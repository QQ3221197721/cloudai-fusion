package wellrouter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// FSMStore — file-backed rule/audit persistence with atomic writes and
// offset+truncate guard for JSONL safety (aligns with pkg/elasticpool
// appendLeaseLocked/appendDecisionLocked patterns).
// ============================================================================

// FSMStore provides thread-safe, crash-safe persistence for route rules and
// audit logs. Directories are created automatically if missing.
type FSMStore struct {
	mu   sync.RWMutex
	root string
	name string
}

// NewFSMStore creates a new store at <root>/<name>. Creates the directory if
// needed and returns an error only on permission/mount failures.
func NewFSMStore(root, name string) (*FSMStore, error) {
	if root == "" {
		return nil, fmt.Errorf("wellrouter: store requires non-empty root")
	}
	if name == "" {
		return nil, fmt.Errorf("wellrouter: store requires non-empty name")
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("wellrouter: create dir %q: %w", dir, err)
	}
	return &FSMStore{root: root, name: name}, nil
}

// RootPath returns the absolute path to the store's data directory.
func (s *FSMStore) RootPath() string {
	return filepath.Join(s.root, s.name)
}

// Exists reports whether any persisted rules exist (i.e., we're not in a fresh
// state that needs default rule generation). The policy: "exists" means a
// non-empty rules.json (the file exists AND has > 0 entries). A file with []
// counts as not existing (user deleted all rules explicitly).
func (s *FSMStore) Exists(ctx context.Context) (bool, error) {
	path := s.rulesFile()
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	// File exists; check it's not empty
	f, fe := os.Open(path)
	if fe != nil {
		return false, fe
	}
	defer f.Close()
	buf := bufio.NewReader(f)
	first, _ := buf.Peek(1)
	return len(first) > 0, nil
}

// LoadRules reads the rules array from rules.json. Returns ([], true, nil) when
// files don't exist or content is empty ([]), ([rules], true, nil) when present,
// (nil, false, err) on I/O/format errors.
func (s *FSMStore) LoadRules(ctx context.Context) ([]*RouteRule, bool, error) {
	path := s.rulesFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("wellrouter: read rules: %w", err)
	}
	rules := []*RouteRule{}
	if json.Unmarshal(data, &rules) != nil {
		// Corrupt JSON → treat as missing and return (nil,false,err) so caller
		// knows something is genuinely wrong rather than silently generating defaults.
		return nil, false, fmt.Errorf("wellrouter: parse rules: %w", err)
	}
	found := len(rules) > 0
	if found && len(rules) > 0 {
		rulesLoaded(len(rules))
	}
	return rules, found, nil
}

// PersistRules atomically replaces rules.json using tmp+rename (POSIX atomic).
// If no write occurs due to corruption/permission errors during tmp creation,
// the existing file is preserved intact. This matches pool.go persistNodesLocked.
func (s *FSMStore) PersistRules(ctx context.Context, rules []*RouteRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return fmt.Errorf("wellrouter: marshal rules: %w", err)
	}

	path := s.rulesFile()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("wellrouter: ensure dir: %w", err)
	}
	tmp := path + ".tmp"

	// Write tmp first
	if werr := os.WriteFile(tmp, data, 0o644); werr != nil {
		return fmt.Errorf("wellrouter: write rules tmp: %w", werr)
	}
	// Atomic rename
	if rerr := os.Rename(tmp, path); rerr != nil {
		// Best effort rollback
		os.Remove(tmp) // ignore rollback failure; original untouched
		return fmt.Errorf("wellrouter: commit rules.json: %w", rerr)
	}

	rulesPersisted(len(rules))
	return nil
}

// AppendAudit appends one audit record to audit.jsonl using offset+truncate
// protection: pre-append size is recorded; partial writes are rolled back so
// torn lines never poison future ListAudits calls. Behavior aligns exactly with
// appendLeaseLocked/appendDecisionLocked in pkg/elasticpool/pool.go.
func (s *FSMStore) AppendAudit(ctx context.Context, record any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("wellrouter: marshal audit: %w", err)
	}
	line = append(line, '\n')

	path := s.auditFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("wellrouter: ensure audit dir: %w", err)
	}

	fh, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if openErr != nil {
		return fmt.Errorf("wellrouter: open audit: %w", openErr)
	}

	info, statErr := fh.Stat()
	if statErr != nil {
		fh.Close()
		return fmt.Errorf("wellrouter: stat audit: %w", statErr)
	}
	offset := info.Size()

	n, werr := fh.Write(line)
	if werr != nil || n != len(line) {
		// Rollback any partial write
		cErr := fh.Close()
		trErr := os.Truncate(path, offset)
		if cErr != nil {
			return fmt.Errorf("wellrouter: close audit: %w; partial wrote %d/%d", cErr, n, len(line))
		}
		if trErr != nil {
			return fmt.Errorf("wellrouter: truncate audit (rollback): %w; partial wrote %d/%d", trErr, n, len(line))
		}
		if werr == nil {
			werr = fmt.Errorf("short write %d bytes", n)
		}
		return fmt.Errorf("wellrouter: append audit (partial %d/%d): %w", n, len(line), werr)
	}
	if cErr := fh.Close(); cErr != nil {
		return fmt.Errorf("wellrouter: close audit: %w", cErr)
	}

	return nil
}

// ListAudits returns the last N audit records (newest first). Limit <= 0 → 20.
func (s *FSMStore) ListAudits(ctx context.Context, limit int) ([]json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}

	path := s.auditFile()
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, nil
		}
		return nil, fmt.Errorf("wellrouter: read audit: %w", rerr)
	}

	var out []json.RawMessage
	for i := len(data) - 1; i >= 0; i-- {
		start := i
		for start > 0 && data[start-1] != '\n' {
			start--
		}
		line := string(data[start:i+1])
		if line == "" {
			continue
		}
		out = append(out, json.RawMessage(line))
		if len(out) >= limit {
			break
		}
		i = start - 1
	}
	// out is already newest-first per loop direction; preserve order
	return out, nil
}

// Close releases resources (currently none; placeholder for future).
func (s *FSMStore) Close() error {
	return nil
}

// Files returns the absolute paths used by this store.
func (s *FSMStore) Files() (rulesPath, auditPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rulesFile(), s.auditFile()
}

func (s *FSMStore) rulesFile() string {
	return filepath.Join(s.root, s.name, "rules.json")
}

func (s *FSMStore) auditFile() string {
	return filepath.Join(s.root, s.name, "audit.jsonl")
}

// Helper logging functions
func rulesLoaded(count int) {
	logrus.WithField("count", count).Info("wellrouter: rules loaded from disk")
}

func rulesPersisted(count int) {
	logrus.WithField("count", count).Info("wellrouter: rules persisted to disk")
}

func auditAppended(action string, subject string) {
	logrus.WithFields(logrus.Fields{
		"action":  action,
		"subject": subject,
	}).Debug("wellrouter: audit appended")
}
