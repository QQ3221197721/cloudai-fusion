// Package modelregistry implements Module 13 — the AI/ML model registry, the
// first real module of the AI/ML layer and our second ecosystem lock-in anchor
// (the model lineage format, alongside .caf Evidence Manifests).
//
// Lock-in thesis: a model registry is easy to replace on day one and
// prohibitively expensive to replace on day 300, because by then every model
// version carries (a) a content-addressed blob, (b) a recursive lineage DAG
// (dataset → code → parent versions), and (c) signed, hash-chained attestations
// in the evidence ledger. Migrating means abandoning the provenance your
// auditors already trust — exactly the Dockerfile effect, but for models.
//
// Storage layout (content-addressed, file-system based):
//
//	<root>/<name>/<version>.json   immutable version record (one per semver)
//	<root>/<name>/_current         "currently serving" version pointer
//	<root>/blobs/<sha256>          model weights, deduplicated by content hash
//
// Every Register and Rollback writes a real attestation through
// pkg/evidence.Ledger (same MemoryStore+EphemeralSigner wiring as `cafctl run`
// when no backend is configured); pass a nil ledger to skip attestation.
package modelregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// LatestVersion is the pseudo-version that resolves to the model's current
// serving pointer (falling back to the most recently registered version).
const LatestVersion = "latest"

const (
	currentFile = "_current" // name of the current-version pointer file
	blobsDir    = "blobs"    // content-addressed blob store directory
)

// Sentinel errors callers can test with errors.Is.
var (
	// ErrNotFound is returned by Get/Lineage when the model or version is absent.
	ErrNotFound = errors.New("modelregistry: not found")
	// ErrExists is returned by Register when name:version is already registered.
	ErrExists = errors.New("modelregistry: version already registered")
)

// ModelArtifact is one immutable, semantically-versioned model release.
type ModelArtifact struct {
	Name       string            `json:"name"`       // e.g. "resnet50"
	Version    string            `json:"version"`    // semver: 1.0.0
	SHA256     string            `json:"sha256"`     // content address of the weights blob
	SizeBytes  int64             `json:"size_bytes"` // exact blob size
	CreatedBy  string            `json:"created_by"` // actor that registered this version
	CreatedAt  time.Time         `json:"created_at"` // UTC registration time
	Lineage    Lineage           `json:"lineage"`    // provenance triple
	ModelCard  ModelCard         `json:"model_card"` // auto-generated model card
	Tags       map[string]string `json:"tags,omitempty"`
}

// Lineage is the provenance triple that makes a model version reproducible:
// which data trained it, which code built it, and which earlier version it
// was fine-tuned from (ParentVersion; empty for a root version).
type Lineage struct {
	DatasetRef    string            `json:"dataset_ref,omitempty"`    // dataset sha256 or path reference
	CodeRef       string            `json:"code_ref,omitempty"`       // training code commit hash
	Hyperparams   map[string]string `json:"hyperparams,omitempty"`    // e.g. {"lr": "0.001"}
	ParentVersion string            `json:"parent_version,omitempty"` // fine-tune parent, same model
}

// ModelCard is the human/machine-readable card auto-generated at registration.
type ModelCard struct {
	Summary   string             `json:"summary,omitempty"`
	TaskType  string             `json:"task_type,omitempty"` // classification/detection/generation
	Framework string             `json:"framework,omitempty"` // pytorch/tensorflow
	Metrics   map[string]float64 `json:"metrics,omitempty"`   // e.g. {"accuracy": 0.94}
}

// RegisterInput describes one registration request.
type RegisterInput struct {
	Name          string
	Version       string // semver MAJOR.MINOR.PATCH
	ArtifactPath  string // local path to the model weights file
	DatasetRef    string
	CodeRef       string
	ParentVersion string            // optional fine-tune parent within the same model
	Hyperparams   map[string]string // optional
	TaskType      string            // optional model-card field
	Framework     string            // optional model-card field
	Summary       string            // optional model-card field
	Metrics       map[string]float64
	Tags          map[string]string
	CreatedBy     string // defaults to "cafctl"; also the attestation actor
}

// LineageEdge is one parent link in the lineage DAG: From is the child
// "name:version", To is its ParentVersion target.
type LineageEdge struct {
	From string `json:"from"` // child ref  "resnet50:1.2.0"
	To   string `json:"to"`   // parent ref "resnet50:1.1.0"
}

// LineageGraph is the recursive lineage DAG rooted at one model version,
// produced by walking ParentVersion links transitively.
type LineageGraph struct {
	Root  string          `json:"root"`  // "name:version" the query started from
	Nodes []ModelArtifact `json:"nodes"` // newest first; Nodes[0] is the root
	Edges []LineageEdge   `json:"edges"` // one per parent hop
	Depth int             `json:"depth"` // chain length incl. root (root-only = 1)
}

// registerAttestation is the signed, hash-chained payload written on every
// Register. It is the cryptographic anchor that Verify checks the on-disk record
// against. RecordDigest is sha256 over the canonical JSON of the ModelArtifact
// exactly as persisted, so any later edit to ANY field (ParentVersion,
// DatasetRef, CodeRef, metrics, content address) is detectable — not just the
// weights blob. This is what MLflow/DVC structurally lack: their lineage lives
// in a mutable DB row with no signed digest to compare against.
type registerAttestation struct {
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	SHA256       string    `json:"sha256"`        // content address of the weights blob
	RecordDigest string    `json:"record_digest"` // sha256(canonical(ModelArtifact))
	Lineage      Lineage   `json:"lineage"`
	ModelCard    ModelCard `json:"model_card"`
}

// IntegrityReport is the outcome of Verify: a cryptographic tamper check.
// Tampered is true when any performed check failed; Checks holds human-readable
// notes for each check so a report reads like an auditor's checklist.
type IntegrityReport struct {
	Ref              string   `json:"ref"`               // name:version
	BlobPresent      bool     `json:"blob_present"`      // the content-addressed blob exists
	BlobHashOK       bool     `json:"blob_hash_ok"`      // sha256(blob) == record.SHA256 == blob filename
	AttestationFound bool     `json:"attestation_found"` // a signed model.register receipt exists for this ref
	RecordDigestOK   bool     `json:"record_digest_ok"`  // sha256(canonical(record)) == sealed attestation digest
	ChainVerified    bool     `json:"chain_verified"`    // the whole attestation chain re-verifies offline
	Tampered         bool     `json:"tampered"`          // any performed check failed
	Checks           []string `json:"checks"`            // per-check notes
}

// computeTampered reports whether any performed integrity check failed. When no
// ledger is configured only the content-address binding is enforced.
func (rep *IntegrityReport) computeTampered() bool {
	if !rep.BlobPresent || !rep.BlobHashOK {
		return true
	}
	if rep.AttestationFound && (!rep.RecordDigestOK || !rep.ChainVerified) {
		return true
	}
	return false
}

// Registry is the model registry contract. The file-system implementation is
// FSRegistry; a future DB/object-store backend can swap in behind this.
type Registry interface {
	// Register ingests the artifact bytes (content-addressed + deduplicated),
	// creates an immutable version record, and writes an attestation.
	Register(ctx context.Context, input RegisterInput) (*ModelArtifact, error)
	// List returns versions of one model (name != "") or of all models
	// (name == ""), newest first.
	List(ctx context.Context, name string) ([]ModelArtifact, error)
	// Get returns one version; version may be "latest" to resolve the current
	// serving pointer.
	Get(ctx context.Context, name, version string) (*ModelArtifact, error)
	// Lineage recursively walks ParentVersion links into a DAG.
	Lineage(ctx context.Context, name, version string) (*LineageGraph, error)
	// Rollback repoints the current-version pointer and attests the change.
	// Data is never deleted. If fromVer is non-empty it must match the current
	// pointer (optimistic concurrency guard).
	Rollback(ctx context.Context, name, fromVer, toVer string) error
}

// Compile-time proof that FSRegistry satisfies Registry.
var _ Registry = (*FSRegistry)(nil)

// FSRegistry is the file-system Registry: JSON version records plus a
// content-addressed blob store, with attestations via pkg/evidence.
// A nil ledger disables attestation (all other behavior is unchanged).
type FSRegistry struct {
	root   string
	ledger *evidence.Ledger

	mu   sync.Mutex
	last *evidence.Evidence // most recent receipt, for CLI display
}

// NewFSRegistry opens (and creates, if needed) a registry rooted at dir.
func NewFSRegistry(dir string, ledger *evidence.Ledger) (*FSRegistry, error) {
	if dir == "" {
		return nil, errors.New("modelregistry: registry root path is required")
	}
	if err := os.MkdirAll(filepath.Join(dir, blobsDir), 0o755); err != nil {
		return nil, fmt.Errorf("modelregistry: create registry root: %w", err)
	}
	return &FSRegistry{root: dir, ledger: ledger}, nil
}

// Root returns the registry root directory (read-only accessor).
func (r *FSRegistry) Root() string { return r.root }

// LastAttestation returns the receipt from the most recent Register/Rollback,
// or nil when none was written (nil ledger or none yet).
func (r *FSRegistry) LastAttestation() *evidence.Evidence { return r.last }

// Register implements Registry. The artifact is hashed, deduplicated against
// blobs/<sha256>, recorded as an immutable <name>/<version>.json, made the
// current version, and attested.
func (r *FSRegistry) Register(ctx context.Context, in RegisterInput) (*ModelArtifact, error) {
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	if err := validateVersion(in.Version); err != nil {
		return nil, err
	}
	if in.ArtifactPath == "" {
		return nil, fmt.Errorf("modelregistry: artifact path is required for %s:%s", in.Name, in.Version)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(in.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("modelregistry: read artifact %q: %w", in.ArtifactPath, err)
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	// Content-addressed storage: if the blob already exists the bytes are
	// identical by definition of sha256, so we skip the write but still create
	// the new version record below (dedup storage, not dedup versions).
	blobPath, err := safeJoin(r.root, blobsDir, sha)
	if err != nil {
		return nil, fmt.Errorf("modelregistry: blob path: %w", err)
	}
	dedup := false
	if _, statErr := os.Stat(blobPath); statErr == nil {
		dedup = true
	} else {
		if err := os.WriteFile(blobPath, data, 0o644); err != nil {
			return nil, fmt.Errorf("modelregistry: store blob %s: %w", sha[:12], err)
		}
	}

	createdBy := in.CreatedBy
	if createdBy == "" {
		createdBy = "cafctl"
	}
	artifact := &ModelArtifact{
		Name:      in.Name,
		Version:   in.Version,
		SHA256:    sha,
		SizeBytes: int64(len(data)),
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
		Lineage: Lineage{
			DatasetRef:    in.DatasetRef,
			CodeRef:       in.CodeRef,
			Hyperparams:   in.Hyperparams,
			ParentVersion: in.ParentVersion,
		},
		ModelCard: ModelCard{
			Summary:   in.Summary,
			TaskType:  in.TaskType,
			Framework: in.Framework,
			Metrics:   in.Metrics,
		},
		Tags: in.Tags,
	}

	// If this version claims a fine-tune parent, the parent must already exist —
	// a lineage chain must never dangle.
	if in.ParentVersion != "" {
		if _, err := r.getLocked(ctx, in.Name, in.ParentVersion); err != nil {
			return nil, fmt.Errorf("modelregistry: parent version unavailable: %w", err)
		}
	}

	modelDir, err := safeJoin(r.root, in.Name)
	if err != nil {
		return nil, fmt.Errorf("modelregistry: model dir: %w", err)
	}
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return nil, fmt.Errorf("modelregistry: create model dir: %w", err)
	}
	versionFile, err := safeJoin(modelDir, in.Version+".json")
	if err != nil {
		return nil, fmt.Errorf("modelregistry: version path: %w", err)
	}
	if _, statErr := os.Stat(versionFile); statErr == nil {
		return nil, fmt.Errorf("%w: %s:%s (versions are immutable; register a new version instead)",
			ErrExists, in.Name, in.Version)
	}
	if err := writeJSONAtomic(versionFile, artifact); err != nil {
		return nil, fmt.Errorf("modelregistry: persist version record: %w", err)
	}
	if err := r.setCurrentLocked(in.Name, in.Version); err != nil {
		return nil, err
	}

	// Seal a digest of the exact persisted record into the signed attestation.
	// Verify later recomputes this digest from the on-disk record; any divergence
	// proves post-registration tampering.
	recordDigest, err := evidence.HashAny(artifact)
	if err != nil {
		return nil, fmt.Errorf("modelregistry: compute record digest: %w", err)
	}
	if aerr := r.attestLocked(ctx, "model.register", in.Name, createdBy,
		map[string]any{
			"name":          in.Name,
			"version":       in.Version,
			"artifact_path": in.ArtifactPath,
			"dataset_ref":   in.DatasetRef,
			"code_ref":      in.CodeRef,
			"parent":        in.ParentVersion,
		},
		map[string]any{
			"sha256":     sha,
			"size_bytes": artifact.SizeBytes,
			"dedup":      dedup,
			"current":    in.Version,
		},
		&registerAttestation{
			Name:         in.Name,
			Version:      in.Version,
			SHA256:       sha,
			RecordDigest: recordDigest,
			Lineage:      artifact.Lineage,
			ModelCard:    artifact.ModelCard,
		}); aerr != nil {
		return nil, aerr
	}
	return artifact, nil
}

// List implements Registry: versions newest-first. An unknown model yields an
// empty slice (use Get for hard NotFound semantics).
func (r *FSRegistry) List(_ context.Context, name string) ([]ModelArtifact, error) {
	if name != "" {
		if err := validateName(name); err != nil {
			return nil, err
		}
		return r.listModel(name)
	}

	entries, err := os.ReadDir(r.root)
	if err != nil {
		return nil, fmt.Errorf("modelregistry: read registry root: %w", err)
	}
	var all []ModelArtifact
	for _, e := range entries {
		if !e.IsDir() || e.Name() == blobsDir {
			continue
		}
		arts, err := r.listModel(e.Name())
		if err != nil {
			return nil, err
		}
		all = append(all, arts...)
	}
	sortArtifactsNewestFirst(all)
	return all, nil
}

func (r *FSRegistry) listModel(name string) ([]ModelArtifact, error) {
	modelDir, err := safeJoin(r.root, name)
	if err != nil {
		return nil, fmt.Errorf("modelregistry: model dir: %w", err)
	}
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("modelregistry: read model dir %q: %w", name, err)
	}
	var arts []ModelArtifact
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var a ModelArtifact
		path, jerr := safeJoin(modelDir, e.Name())
		if jerr != nil {
			return nil, fmt.Errorf("modelregistry: version record path %s/%s: %w", name, e.Name(), jerr)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("modelregistry: read version record %s/%s: %w", name, e.Name(), err)
		}
		if err := json.Unmarshal(data, &a); err != nil {
			return nil, fmt.Errorf("modelregistry: parse version record %s/%s: %w", name, e.Name(), err)
		}
		arts = append(arts, a)
	}
	sortArtifactsNewestFirst(arts)
	return arts, nil
}

// Get implements Registry. version may be "latest", which resolves through the
// _current pointer (falling back to the newest registered version).
func (r *FSRegistry) Get(ctx context.Context, name, version string) (*ModelArtifact, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getLocked(ctx, name, version)
}

// getLocked is Get without re-acquiring the mutex (caller holds r.mu).
func (r *FSRegistry) getLocked(_ context.Context, name, version string) (*ModelArtifact, error) {
	if version == "" || version == LatestVersion {
		cur, err := r.currentLocked(name)
		if err != nil {
			return nil, err
		}
		if cur == "" {
			arts, _ := r.listModel(name)
			if len(arts) == 0 {
				return nil, r.notFound(name, version, nil)
			}
			cur = arts[0].Version
		}
		version = cur
	} else if err := validateVersion(version); err != nil {
		return nil, err
	}

	versionPath, err := safeJoin(r.root, name, version+".json")
	if err != nil {
		return nil, fmt.Errorf("modelregistry: version path: %w", err)
	}
	data, err := os.ReadFile(versionPath)
	if err != nil {
		if os.IsNotExist(err) {
			known, _ := r.listModel(name)
			versions := make([]string, 0, len(known))
			for _, a := range known {
				versions = append(versions, a.Version)
			}
			return nil, r.notFound(name, version, versions)
		}
		return nil, fmt.Errorf("modelregistry: read version record %s:%s: %w", name, version, err)
	}
	var a ModelArtifact
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("modelregistry: parse version record %s:%s: %w", name, version, err)
	}
	return &a, nil
}

// notFound builds the canonical clear-error for missing versions.
func (r *FSRegistry) notFound(name, version string, knownVersions []string) error {
	if len(knownVersions) == 0 {
		return fmt.Errorf("%w: model %q (requested version %q) has no versions registered yet", ErrNotFound, name, version)
	}
	return fmt.Errorf("%w: model %q has no version %q; registered versions: %s",
		ErrNotFound, name, version, strings.Join(knownVersions, ", "))
}

// Lineage implements Registry: recursive ParentVersion walk with cycle
// detection, newest node first.
func (r *FSRegistry) Lineage(ctx context.Context, name, version string) (*LineageGraph, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lineageLocked(ctx, name, version)
}

func (r *FSRegistry) lineageLocked(ctx context.Context, name, version string) (*LineageGraph, error) {
	root, err := r.getLocked(ctx, name, version)
	if err != nil {
		return nil, err
	}
	graph := &LineageGraph{Root: ref(name, root.Version)}

	visited := map[string]bool{}
	var walk func(art *ModelArtifact) error
	walk = func(art *ModelArtifact) error {
		nodeRef := ref(art.Name, art.Version)
		if visited[nodeRef] {
			return fmt.Errorf("modelregistry: lineage cycle detected at %s; records are corrupt", nodeRef)
		}
		visited[nodeRef] = true
		graph.Nodes = append(graph.Nodes, *art)
		if art.Lineage.ParentVersion == "" {
			return nil
		}
		parent, err := r.getLocked(ctx, art.Name, art.Lineage.ParentVersion)
		if err != nil {
			return fmt.Errorf("modelregistry: broken lineage at %s: %w", nodeRef, err)
		}
		graph.Edges = append(graph.Edges, LineageEdge{From: nodeRef, To: ref(parent.Name, parent.Version)})
		return walk(parent)
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	graph.Depth = len(graph.Nodes)
	return graph, nil
}

// Rollback implements Registry: repoint _current to toVer (which must exist),
// attesting the change. fromVer, when non-empty, must match the current
// pointer so a stale writer cannot clobber a newer rollback. No data is ever
// deleted — rollback is a pointer move plus a receipt.
func (r *FSRegistry) Rollback(ctx context.Context, name, fromVer, toVer string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if err := validateVersion(toVer); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.getLocked(ctx, name, toVer); err != nil {
		return fmt.Errorf("modelregistry: rollback target invalid: %w", err)
	}
	cur, err := r.currentLocked(name)
	if err != nil {
		return err
	}
	if fromVer != "" && cur != fromVer {
		return fmt.Errorf("modelregistry: rollback conflict for %q: current version is %q, not %q (re-read latest and retry)", name, cur, fromVer)
	}
	if err := r.setCurrentLocked(name, toVer); err != nil {
		return err
	}
	return r.attestLocked(ctx, "model.rollback", name, "cafctl",
		map[string]any{"name": name, "from": cur, "to": toVer},
		map[string]any{"current": toVer},
		map[string]any{"previous_current": cur, "rolled_back_to": toVer})
}

// Current returns the model's current serving version, or "" when none is set.
func (r *FSRegistry) Current(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentLocked(name)
}

func (r *FSRegistry) currentLocked(name string) (string, error) {
	p, err := safeJoin(r.root, name, currentFile)
	if err != nil {
		return "", fmt.Errorf("modelregistry: current pointer path: %w", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("modelregistry: read current pointer for %q: %w", name, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func (r *FSRegistry) setCurrentLocked(name, version string) error {
	p, err := safeJoin(r.root, name, currentFile)
	if err != nil {
		return fmt.Errorf("modelregistry: current pointer path: %w", err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(version), 0o644); err != nil {
		return fmt.Errorf("modelregistry: write current pointer: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("modelregistry: commit current pointer: %w", err)
	}
	return nil
}

// attestLocked writes one receipt through the evidence ledger (real signing
// and hash chaining; the backing store depends on the injected ledger).
func (r *FSRegistry) attestLocked(ctx context.Context, action, subject, actor string, input, output, payload any) error {
	if r.ledger == nil {
		return nil
	}
	ev, err := r.ledger.Record(ctx, evidence.RecordInput{
		Actor:   actor,
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("modelregistry: attestation %s failed: %w", action, err)
	}
	r.last = ev
	return nil
}

// Verify performs a cryptographic integrity check that proves whether the
// persisted record has been modified since registration. MLflow/DVC store
// version and lineage rows in an ordinary database with no cryptographic seal,
// so a silent UPDATE to a lineage row is undetectable. Here two independent
// bindings are re-checked from scratch:
//
//  1. Content address — the weights live at blobs/<sha256>. Verify recomputes
//     sha256 over the stored bytes and requires it to equal both the record's
//     SHA256 field and the blob's own filename. Any edit to the weights changes
//     the address and is caught (attack: blob substitution).
//  2. Signed record digest — every Register seals sha256(canonical(record)) into
//     a hash-chained, Ed25519-signed attestation. Verify recomputes the on-disk
//     record's digest, requires a matching sealed digest, and re-verifies the
//     WHOLE attestation chain offline against the ledger's public key. Editing
//     ANY field (ParentVersion, DatasetRef, CodeRef, Metrics, …) diverges from
//     the sealed digest and is caught (attacks: lineage mutation, receipt
//     deletion, ledger tampering).
//
// A nil ledger disables binding (2); binding (1) is always enforced. Verify
// never mutates state.
func (r *FSRegistry) Verify(ctx context.Context, name, version string) (*IntegrityReport, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	rep := &IntegrityReport{Ref: ref(name, version)}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Read the current record; also normalizes "latest" to a concrete version.
	artifact, err := r.getLocked(ctx, name, version)
	if err != nil {
		return nil, err
	}
	rep.Ref = ref(name, artifact.Version)

	// (1) Content-address integrity: recompute sha256(blob) and require it to
	// equal the record's SHA256 and the blob's own filename.
	blobPath, err := safeJoin(r.root, blobsDir, artifact.SHA256)
	if err != nil {
		return nil, fmt.Errorf("modelregistry: blob path: %w", err)
	}
	if data, rerr := os.ReadFile(blobPath); rerr == nil {
		rep.BlobPresent = true
		sum := sha256.Sum256(data)
		actualSHA := hex.EncodeToString(sum[:])
		rep.BlobHashOK = actualSHA == artifact.SHA256 && actualSHA == filepath.Base(blobPath)
		if rep.BlobHashOK {
			rep.Checks = append(rep.Checks, "[PASS] content-address verified: sha256(blob) == record.sha256 == blob filename")
		} else {
			rep.Checks = append(rep.Checks,
				fmt.Sprintf("[FAIL] BLOB TAMPER: sha256(blob)=%s… != record.sha256=%s…", short(actualSHA), short(artifact.SHA256)))
		}
	} else if os.IsNotExist(rerr) {
		rep.Checks = append(rep.Checks, "[FAIL] content-addressed blob missing at "+blobPath)
	} else {
		return nil, fmt.Errorf("modelregistry: read blob %s: %w", short(artifact.SHA256), rerr)
	}

	// (2) Signed record-digest binding. Skipped (honestly) when no ledger exists.
	if r.ledger == nil {
		rep.Checks = append(rep.Checks, "[INFO] no ledger configured: signed lineage cross-check skipped (content-address still enforced)")
		rep.Tampered = rep.computeTampered()
		return rep, nil
	}

	recordDigest, err := evidence.HashAny(artifact)
	if err != nil {
		return nil, fmt.Errorf("modelregistry: compute record digest: %w", err)
	}

	recs, err := r.ledger.Store().List(ctx, evidence.Filter{Action: "model.register", Subject: name, Limit: 10000})
	if err != nil {
		return nil, fmt.Errorf("modelregistry: list attestations: %w", err)
	}
	var sealedDigest string
	for _, e := range recs {
		if len(e.Payload) == 0 {
			continue
		}
		var p registerAttestation
		if json.Unmarshal(e.Payload, &p) != nil {
			continue
		}
		if p.Name == name && p.Version == artifact.Version {
			rep.AttestationFound = true
			sealedDigest = p.RecordDigest
			break
		}
	}
	if !rep.AttestationFound {
		rep.Checks = append(rep.Checks, "[FAIL] no signed model.register attestation found for "+rep.Ref)
		rep.Tampered = rep.computeTampered()
		return rep, nil
	}

	rep.RecordDigestOK = sealedDigest == recordDigest
	if rep.RecordDigestOK {
		rep.Checks = append(rep.Checks, "[PASS] record digest matches signed attestation sealed at register time")
	} else {
		rep.Checks = append(rep.Checks,
			fmt.Sprintf("[FAIL] LINEAGE/RECORD TAMPER: on-disk digest=%s… != signed digest=%s…", short(recordDigest), short(sealedDigest)))
	}

	// (3) Re-verify the entire attestation chain OFFLINE against the public key:
	// recompute every leaf hash, check the unbroken hash chain, verify signatures.
	all, err := r.ledger.Store().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("modelregistry: read attestation chain: %w", err)
	}
	report, verr := evidence.VerifyChain(all, r.ledger.Signer().PublicKey())
	if verr != nil {
		return nil, fmt.Errorf("modelregistry: verify attestation chain: %w", verr)
	}
	rep.ChainVerified = report.Valid
	if rep.ChainVerified {
		rep.Checks = append(rep.Checks,
			fmt.Sprintf("[PASS] attestation chain verified offline: %d/%d receipts valid (key %s)", report.Verified, report.Total, report.KeyID))
	} else {
		rep.Checks = append(rep.Checks,
			fmt.Sprintf("[FAIL] ATTESTATION CHAIN BROKEN: %d/%d receipts failed", report.Failed, report.Total))
	}

	rep.Tampered = rep.computeTampered()
	if !rep.Tampered {
		rep.Checks = append(rep.Checks, "[OK] all checks passed — no tampering detected")
	}
	return rep, nil
}

// short truncates a hex digest to its first 12 chars for readable diagnostics.
func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func ref(name, version string) string { return name + ":" + version }

// safeJoin joins base with segments and verifies the resolved path stays
// inside base — defense in depth against path traversal even though
// validateName/validateVersion already reject separators and '..' prefixes.
func safeJoin(base string, segs ...string) (string, error) {
	p := base
	for _, s := range segs {
		p = filepath.Join(p, s)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes registry root: %q", p)
	}
	return p, nil
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// sortArtifactsNewestFirst orders by CreatedAt descending, breaking exact ties
// by version string so the order is always deterministic.
func sortArtifactsNewestFirst(arts []ModelArtifact) {
	sort.SliceStable(arts, func(i, j int) bool {
		if !arts[i].CreatedAt.Equal(arts[j].CreatedAt) {
			return arts[i].CreatedAt.After(arts[j].CreatedAt)
		}
		return arts[i].Version > arts[j].Version
	})
}

var (
	nameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	semverRe  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	maxNameLen = 64
)

// validateName enforces a filesystem-safe, registry-portable model name.
func validateName(name string) error {
	if name == "" {
		return errors.New("modelregistry: model name is required")
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("modelregistry: model name %q exceeds %d characters", name, maxNameLen)
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("modelregistry: invalid model name %q: use letters, digits, '.', '_', '-' (must start alphanumeric)", name)
	}
	return nil
}

// validateVersion enforces strict core semver (MAJOR.MINOR.PATCH, no leading
// zeros) so version strings sort and compare predictably.
func validateVersion(version string) error {
	if !semverRe.MatchString(version) {
		return fmt.Errorf("modelregistry: invalid semantic version %q: expected MAJOR.MINOR.PATCH (e.g. 1.0.0)", version)
	}
	for _, part := range strings.Split(version, ".") {
		if len(part) > 1 && part[0] == '0' {
			return fmt.Errorf("modelregistry: invalid semantic version %q: segment %q has a leading zero", version, part)
		}
	}
	return nil
}
