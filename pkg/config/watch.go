package config

// watch.go wires the hot-reload protocol end to end:
//
//	Source (file / etcd / consul) --emits--> raw bytes
//	     --parse--> map[string]string
//	     --CRDT merge--> converged ConfigState   (multi-node reconciliation)
//	     --seal + atomic swap--> HotStore         (zero-downtime publish)
//
// The Source interface is deliberately tiny so that a file watcher (implemented
// here with fsnotify) and a distributed KV watcher (etcd/consul) are drop-in
// interchangeable. Only the file source is compiled in by default because it is
// the one with a real, vendored dependency (fsnotify, already used by viper).
// EtcdSource/ConsulSource can be added by satisfying the same interface without
// touching the Reloader.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Update is one config revision delivered by a Source.
type Update struct {
	// Values is the full desired config as key/value pairs.
	Values map[string]string
	// Origin identifies where the update came from (path, etcd key prefix, ...).
	Origin string
}

// Source produces config updates. Watch streams updates on the returned channel
// until ctx is cancelled or the source is closed; the channel is closed on exit.
type Source interface {
	Watch(ctx context.Context) (<-chan Update, error)
	Close() error
}

// ---------------------------------------------------------------------------
// FileSource — real fsnotify-backed watcher
// ---------------------------------------------------------------------------

// FileSource watches a single key=value config file and emits an Update on every
// write. It emits once immediately so the store is populated at startup, then on
// each fsnotify write/create/rename event (editors often replace-and-rename).
type FileSource struct {
	path    string
	watcher *fsnotify.Watcher
	once    sync.Once
}

// NewFileSource creates a watcher for path. The file need not exist yet; the
// watcher observes the containing directory so create/rename are caught too.
func NewFileSource(path string) (*FileSource, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("config: create fsnotify watcher: %w", err)
	}
	dir := filepath.Dir(path)
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("config: watch dir %q: %w", dir, err)
	}
	return &FileSource{path: filepath.Clean(path), watcher: w}, nil
}

// Watch streams updates. The first Update is the current file contents (empty if
// the file is missing); subsequent updates follow relevant fsnotify events.
func (f *FileSource) Watch(ctx context.Context) (<-chan Update, error) {
	out := make(chan Update, 1)
	go func() {
		defer close(out)
		// Prime with the current contents.
		if vals, err := readKV(f.path); err == nil {
			select {
			case out <- Update{Values: vals, Origin: f.path}:
			case <-ctx.Done():
				return
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-f.watcher.Events:
				if !ok {
					return
				}
				if filepath.Clean(ev.Name) != f.path {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				vals, err := readKV(f.path)
				if err != nil {
					continue
				}
				select {
				case out <- Update{Values: vals, Origin: f.path}:
				case <-ctx.Done():
					return
				}
			case _, ok := <-f.watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return out, nil
}

// Close releases the underlying fsnotify watcher.
func (f *FileSource) Close() error {
	var err error
	f.once.Do(func() { err = f.watcher.Close() })
	return err
}

// readKV parses a simple "key=value" file (ignoring blank lines and #comments).
// This intentionally matches .env-style config so the file source has no schema
// coupling; richer formats (YAML) go through the existing viper Load path.
func readKV(path string) (map[string]string, error) {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, sc.Err()
}

// ---------------------------------------------------------------------------
// MemSource — programmatic source for tests, etcd/consul adapters, and gossip
// ---------------------------------------------------------------------------

// MemSource is an in-memory Source driven by Push. It is the adapter point for a
// distributed KV store: an etcd/consul watch loop simply calls Push on each
// delivered revision. It is also what the benchmarks and tests drive directly.
type MemSource struct {
	ch     chan Update
	origin string
	once   sync.Once
}

// NewMemSource returns an in-memory source tagged with origin.
func NewMemSource(origin string) *MemSource {
	return &MemSource{ch: make(chan Update, 16), origin: origin}
}

// Push submits a new revision. Non-blocking up to the buffer; drops to the
// caller's discretion via the returned bool (false => buffer full).
func (m *MemSource) Push(values map[string]string) bool {
	select {
	case m.ch <- Update{Values: values, Origin: m.origin}:
		return true
	default:
		return false
	}
}

// Watch returns the update stream. ctx cancellation stops forwarding.
func (m *MemSource) Watch(ctx context.Context) (<-chan Update, error) {
	out := make(chan Update)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case u, ok := <-m.ch:
				if !ok {
					return
				}
				select {
				case out <- u:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// Close closes the input channel so Watch drains and exits.
func (m *MemSource) Close() error {
	m.once.Do(func() { close(m.ch) })
	return nil
}

// ---------------------------------------------------------------------------
// Reloader — glue: Source -> CRDT merge -> sealed atomic swap
// ---------------------------------------------------------------------------

// Reloader binds a Source to a HotStore. Each incoming Update is merged into the
// node's CRDT ConfigState (so concurrent updates from multiple sources/nodes
// converge deterministically) and the merged result is sealed and published via
// copy-on-write swap. OnPublish, if set, is invoked after every successful swap.
type Reloader struct {
	store  *HotStore
	state  *ConfigState
	signer *BundleSigner

	// OnPublish is called with the newly published snapshot (nil-safe).
	OnPublish func(*Snapshot)
}

// NewReloader constructs a reloader for nodeID, generating an Ed25519 signer so
// every published version is sealed (the moat). Pass a signer via WithSigner to
// reuse a KMS/seed-derived key instead.
func NewReloader(nodeID string) (*Reloader, error) {
	signer, err := NewBundleSigner()
	if err != nil {
		return nil, err
	}
	return &Reloader{
		store:  NewHotStore(nodeID),
		state:  NewConfigState(nodeID),
		signer: signer,
	}, nil
}

// WithSigner overrides the auto-generated signer (e.g. one built from
// EvidenceKeyPath). Call before Run.
func (r *Reloader) WithSigner(s *BundleSigner) *Reloader {
	if s != nil {
		r.signer = s
	}
	return r
}

// Store exposes the HotStore so callers can serve lock-free reads/flags.
func (r *Reloader) Store() *HotStore { return r.store }

// State exposes the CRDT ConfigState for peer reconciliation (see MergePeer).
func (r *Reloader) State() *ConfigState { return r.state }

// Run consumes updates from src until ctx is cancelled or the stream closes. It
// merges each update into the CRDT state and publishes the converged, sealed
// snapshot. Run blocks; start it in its own goroutine.
func (r *Reloader) Run(ctx context.Context, src Source) error {
	ch, err := src.Watch(ctx)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case u, ok := <-ch:
			if !ok {
				return nil
			}
			r.apply(u.Values)
		}
	}
}

// apply merges values into the CRDT state (local writes) and publishes.
func (r *Reloader) apply(values map[string]string) {
	for k, v := range values {
		r.state.Set(k, v)
	}
	r.publish()
}

// MergePeer folds a peer node's registers into local state (multi-node
// convergence) and republishes if anything changed. Returns the number of keys
// that changed. This is the reconciliation entry point a gossip/anti-entropy
// loop calls with registers pulled from other nodes.
func (r *Reloader) MergePeer(peer map[string]LWWRegister) int {
	changed := r.state.Merge(peer)
	if changed > 0 {
		r.publish()
	}
	return changed
}

// publish snapshots the CRDT state, seals it, and swaps it into the store.
func (r *Reloader) publish() {
	snap, swapped, err := r.store.Publish(r.state.Snapshot(), r.signer)
	if err != nil || !swapped {
		return
	}
	if r.OnPublish != nil {
		r.OnPublish(snap)
	}
}
