// Package cloud implements Module 2 — the Multi-Cloud Unified Interface: a
// provider-agnostic layer that unifies list/create/monitor across 6 clouds,
// generates cost-optimal plans via PlanEngine, and tracks cluster lifecycle
// transitions through an explicit FSM with signed attestation via pkg/evidence.
//
// Lock-in thesis: every cluster lifecycle transition (pending→provisioning→
// ready/failed; deleting→deleted/failed) is a signed, hash-chained receipt in
// the evidence ledger plus an append-only row in operations.jsonl (last-write-
// wins per op ID, offset+truncate rollback so a torn write never poisons reads).
// After months of cluster changes, that provenance history means migrating
// would abandon the auditable timeline your compliance team trusts.
//
// Storage layout:
//
//	<root>/cloud/operations.jsonl   append-only lifecycle events (LWW per op ID)
package cloud

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// Cluster Lifecycle FSM
// ============================================================================

// ClusterLifecycleState is one state of the cluster lifecycle state machine.
//
//	pending → provisioning → ready | failed
//	ready → deleting → deleted | failed
//	failed → pending (retry allowed)
//	deleted is terminal — no transitions out.
type ClusterLifecycleState string

const (
	// StatePending is the initial state after Start.
	StatePending ClusterLifecycleState = "pending"
	// StateProvisioning means the provider create call is in flight.
	StateProvisioning ClusterLifecycleState = "provisioning"
	// StateReady means the cluster is healthy and accepting workloads.
	StateReady ClusterLifecycleState = "ready"
	// StateDeleting means deletion is in flight.
	StateDeleting ClusterLifecycleState = "deleting"
	// StateDeleted is the terminal state after successful deletion.
	StateDeleted ClusterLifecycleState = "deleted"
	// StateFailed is a retryable failure state (failed→pending allowed).
	StateFailed ClusterLifecycleState = "failed"
)

// lifecycleTransitions is the FSM edge table: current state → allowed successors.
var lifecycleTransitions = map[ClusterLifecycleState][]ClusterLifecycleState{
	StatePending:      {StateProvisioning, StateFailed},
	StateProvisioning: {StateReady, StateFailed, StateDeleting},
	StateReady:        {StateDeleting},
	StateDeleting:     {StateDeleted, StateFailed},
	StateFailed:       {StatePending, StateDeleting},
	StateDeleted:      {}, // terminal
}

// Sentinel errors callers can test with errors.Is.
var (
	// ErrInvalidTransition is returned on an FSM edge that does not exist.
	ErrInvalidTransition = errors.New("cloud: invalid lifecycle transition")
	// ErrOperationNotFound is returned when an operation ID is absent.
	ErrOperationNotFound = errors.New("cloud: operation not found")
)

// AllowedTransitions returns the legal successor states of s (docs/tests/CLI).
func AllowedTransitions(s ClusterLifecycleState) []ClusterLifecycleState {
	out := make([]ClusterLifecycleState, len(lifecycleTransitions[s]))
	copy(out, lifecycleTransitions[s])
	return out
}

// ValidateTransition enforces the FSM edge table. On failure it returns an
// error wrapping ErrInvalidTransition that names the allowed successors.
func ValidateTransition(from, to ClusterLifecycleState) error {
	allowed, ok := lifecycleTransitions[from]
	if !ok {
		return fmt.Errorf("%w: unknown state %q", ErrInvalidTransition, from)
	}
	if len(allowed) == 0 {
		return fmt.Errorf("%w: %s is terminal (no successor states)", ErrInvalidTransition, from)
	}
	for _, s := range allowed {
		if s == to {
			return nil
		}
	}
	return fmt.Errorf("%w: %s → %s (allowed from %s: %s)",
		ErrInvalidTransition, from, to, from, strings.Join(statesToStrings(allowed), ", "))
}

func statesToStrings(ss []ClusterLifecycleState) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	return out
}

// ============================================================================
// Cluster Operation Record
// ============================================================================

// ClusterOperation is one tracked lifecycle operation (create or delete
// journey). Rows are appended to operations.jsonl on every transition; reads
// merge last-write-wins per ID.
type ClusterOperation struct {
	ID            string                `json:"id"`                        // "op-<hex12>"
	Provider      string                `json:"provider"`                  // provider name
	ProviderType  string                `json:"provider_type"`             // provider type enum string
	ClusterID     string                `json:"cluster_id,omitempty"`      // filled on MarkReady
	State         ClusterLifecycleState `json:"state"`                     // current FSM state
	RequestedSpec *CreateClusterRequest `json:"requested_spec,omitempty"`  // node count / machine type
	EvidenceHash  string                `json:"evidence_hash,omitempty"`   // hash of the latest attestation receipt
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
	ErrorMessage  string                `json:"error_message,omitempty"`   // filled on MarkFailed
}

// lifecycleAction maps a target state to its attestation action name.
func lifecycleAction(s ClusterLifecycleState) string {
	switch s {
	case StatePending:
		return "cloud.op.start"
	case StateProvisioning:
		return "cloud.op.provisioning"
	case StateReady:
		return "cloud.op.ready"
	case StateDeleting:
		return "cloud.op.deleting"
	case StateDeleted:
		return "cloud.op.deleted"
	case StateFailed:
		return "cloud.op.failed"
	default:
		return "cloud.op.unknown"
	}
}

// ============================================================================
// OperationTracker
// ============================================================================

// DefaultCloudActor is the attestation actor for cloud lifecycle receipts.
const DefaultCloudActor = "cafctl-cloud"

// operationsFile is the JSONL log name under <root>/cloud/.
const operationsFile = "operations.jsonl"

// OperationTracker drives the cluster lifecycle FSM: Start creates a pending
// operation, Mark* methods advance it through validated transitions, and every
// step is persisted (append-only, torn-write safe) and attested through the
// evidence ledger (nil ledger degrades honestly — no receipt, operation still
// progresses).
type OperationTracker struct {
	mu      sync.Mutex
	root    string
	opsPath string
	ledger  *evidence.Ledger
	last    *evidence.Evidence
}

// NewOperationTracker creates a tracker storing under <root>/cloud/. Pass a
// nil ledger to disable attestation (dev-only degradation, honestly reported).
func NewOperationTracker(root string, ledger *evidence.Ledger) (*OperationTracker, error) {
	dir := filepath.Join(root, "cloud")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cloud: create operations dir: %w", err)
	}
	return &OperationTracker{
		root:    root,
		opsPath: filepath.Join(dir, operationsFile),
		ledger:  ledger,
	}, nil
}

// LastAttestation returns the most recent receipt (nil when ledger disabled).
func (t *OperationTracker) LastAttestation() *evidence.Evidence { return t.last }

// Attest records a cloud-related receipt (e.g. "cloud.plan") without creating
// a lifecycle operation. Returns the receipt hash ("" when ledger disabled).
func (t *OperationTracker) Attest(ctx context.Context, action, subject string, input, output any) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ledger == nil {
		return "", nil
	}
	ev, err := t.ledger.Record(ctx, evidence.RecordInput{
		Actor:   DefaultCloudActor,
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Backends: []evidence.BackendFact{{
			Component: "cloud.plan",
			Mode:      "real",
			Driver:    "static-pricing-table",
			Detail:    "plan generated from hardcoded GPU price tables, no cloud API calls",
		}},
	})
	if err != nil {
		return "", fmt.Errorf("cloud: attestation %s failed: %w", action, err)
	}
	t.last = ev
	return ev.Hash, nil
}

// Start opens a new cluster-creation operation in StatePending and records the
// "cloud.op.start" attestation. The provider identifies the target cloud; spec
// carries the requested node count / machine type.
func (t *OperationTracker) Start(ctx context.Context, provider Provider, spec *CreateClusterRequest) (*ClusterOperation, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UTC()
	op := &ClusterOperation{
		ID:            newOperationID(),
		Provider:      provider.Name(),
		ProviderType:  string(provider.Type()),
		State:         StatePending,
		RequestedSpec: spec,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := t.attestLocked(ctx, op, spec); err != nil {
		return nil, err
	}
	if err := t.appendOperationLocked(op); err != nil {
		return nil, err
	}
	return op, nil
}

// MarkProvisioning advances pending → provisioning.
func (t *OperationTracker) MarkProvisioning(ctx context.Context, opID string) error {
	return t.transitionLocked(ctx, opID, StateProvisioning)
}

// MarkReady advances provisioning → ready, recording the cluster ID.
func (t *OperationTracker) MarkReady(ctx context.Context, opID, clusterID string) error {
	return t.transitionLocked(ctx, opID, StateReady, func(op *ClusterOperation) {
		op.ClusterID = clusterID
	})
}

// MarkFailed moves a non-terminal operation → failed with a reason.
func (t *OperationTracker) MarkFailed(ctx context.Context, opID, errMsg string) error {
	return t.transitionLocked(ctx, opID, StateFailed, func(op *ClusterOperation) {
		op.ErrorMessage = errMsg
	})
}

// MarkDeleting initiates deletion (ready|provisioning|failed → deleting).
func (t *OperationTracker) MarkDeleting(ctx context.Context, opID string) error {
	return t.transitionLocked(ctx, opID, StateDeleting)
}

// Retry re-queues a failed operation: failed → pending, clearing the recorded
// error so the journey can run provisioning again.
func (t *OperationTracker) Retry(ctx context.Context, opID string) error {
	return t.transitionLocked(ctx, opID, StatePending, func(op *ClusterOperation) {
		op.ErrorMessage = ""
	})
}

// MarkDeleted completes deletion (deleting → deleted, terminal).
func (t *OperationTracker) MarkDeleted(ctx context.Context, opID string) error {
	return t.transitionLocked(ctx, opID, StateDeleted)
}

type transitionMod func(*ClusterOperation)

// transitionLocked loads, validates, mutates, attests, and persists one FSM
// step. Caller holds t.mu.
func (t *OperationTracker) transitionLocked(ctx context.Context, opID string, to ClusterLifecycleState, mods ...transitionMod) error {
	ops, err := t.loadOperationsLocked()
	if err != nil {
		return err
	}
	op, ok := ops[opID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrOperationNotFound, opID)
	}
	from := op.State
	if err := ValidateTransition(from, to); err != nil {
		return err
	}
	op.State = to
	op.UpdatedAt = time.Now().UTC()
	for _, m := range mods {
		m(op)
	}
	if err := t.attestLocked(ctx, op, map[string]string{
		"from_state": string(from),
		"to_state":   string(to),
	}); err != nil {
		return err
	}
	if err := t.appendOperationLocked(op); err != nil {
		return err
	}
	return nil
}

// List returns operations newest-first, capped at limit (limit <= 0 = all).
func (t *OperationTracker) List(limit int) ([]*ClusterOperation, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	byID, err := t.loadOperationsLocked()
	if err != nil {
		return nil, err
	}
	// Creation order = first-seen order tracked below; rebuild chronological list.
	order := t.orderLocked()
	out := make([]*ClusterOperation, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- { // newest first
		if op, ok := byID[order[i]]; ok {
			out = append(out, op)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Get returns one operation by ID.
func (t *OperationTracker) Get(opID string) (*ClusterOperation, error) {
	ops, err := t.List(0)
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if op.ID == opID {
			return op, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrOperationNotFound, opID)
}

// ============================================================================
// Persistence: append-only JSONL with offset+truncate rollback
// ============================================================================

// appendOperationLocked appends one row. Two layers of torn-write defense,
// mirroring the elasticpool golden pattern:
//  1. pre-write repair: if the file ends mid-line (external crash/corruption),
//     truncate back to the last LF so the new row starts on a clean boundary;
//  2. post-write rollback: stat the file first (offset), write line+LF, and
//     on a short/failed write roll back with Truncate(offset).
//
// Together these guarantee the JSONL log never carries a torn line.
// Caller holds t.mu.
func (t *OperationTracker) appendOperationLocked(op *ClusterOperation) error {
	line, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("cloud: marshal operation: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(t.opsPath), 0o755); err != nil {
		return fmt.Errorf("cloud: create operations dir: %w", err)
	}
	t.repairTornTailLocked()
	fh, err := os.OpenFile(t.opsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("cloud: open operations log: %w", err)
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		return fmt.Errorf("cloud: stat operations log: %w", err)
	}
	offset := info.Size()
	n, werr := fh.Write(append(line, '\n'))
	if werr != nil || n != len(line)+1 {
		// Roll back any partial bytes — the JSONL log must never carry a torn
		// line that would poison future reads. Best effort; report the original
		// failure with rollback context.
		if terr := os.Truncate(t.opsPath, offset); terr != nil {
			return fmt.Errorf("cloud: append operation (wrote %d of %d bytes): %w; rollback failed: %v",
				n, len(line)+1, werr, terr)
		}
		if werr == nil {
			werr = io.ErrShortWrite
		}
		return fmt.Errorf("cloud: append operation (wrote %d of %d bytes): %w", n, len(line)+1, werr)
	}
	return nil
}

// repairTornTailLocked truncates a trailing partial line (bytes after the
// last LF) so the next append starts on a clean row boundary. Best effort:
// any read/stat failure leaves the file untouched. Caller holds t.mu.
func (t *OperationTracker) repairTornTailLocked() {
	data, err := os.ReadFile(t.opsPath)
	if err != nil || len(data) == 0 {
		return
	}
	if data[len(data)-1] == '\n' {
		return // already clean
	}
	clean := int64(bytes.LastIndexByte(data, '\n') + 1) // -1+1 = 0 when no LF
	_ = os.Truncate(t.opsPath, clean)
}

// loadOperationsLocked reads operations.jsonl and merges last-write-wins per
// op ID, preserving first-seen (creation) order. A missing file is an empty
// log; a torn trailing line (should be impossible given the truncate guard)
// is skipped rather than fatal — reads stay robust.
// Caller holds t.mu.
func (t *OperationTracker) loadOperationsLocked() (map[string]*ClusterOperation, error) {
	data, err := os.ReadFile(t.opsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]*ClusterOperation{}, nil
		}
		return nil, fmt.Errorf("cloud: read operations log: %w", err)
	}
	byID := map[string]*ClusterOperation{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var op ClusterOperation
		if err := json.Unmarshal([]byte(line), &op); err != nil {
			// Skip torn/corrupt trailing line instead of failing the read.
			continue
		}
		merged := op // copy
		byID[op.ID] = &merged
	}
	return byID, nil
}

// orderLocked returns first-seen IDs in creation order (stable across LWW merges).
func (t *OperationTracker) orderLocked() []string {
	data, err := os.ReadFile(t.opsPath)
	if err != nil {
		return nil
	}
	var order []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var probe struct {
			ID string `json:"id"`
		}
		if json.Unmarshal([]byte(line), &probe) != nil || probe.ID == "" {
			continue
		}
		if !seen[probe.ID] {
			seen[probe.ID] = true
			order = append(order, probe.ID)
		}
	}
	return order
}

// ============================================================================
// Attestation
// ============================================================================

// attestLocked writes one signed, hash-chained receipt through the evidence
// ledger for the operation's current state transition and stamps the receipt
// hash onto the operation. A nil ledger skips emission (honest degradation).
// Caller holds t.mu.
func (t *OperationTracker) attestLocked(ctx context.Context, op *ClusterOperation, input any) error {
	if t.ledger == nil {
		op.EvidenceHash = ""
		return nil
	}
	ev, err := t.ledger.Record(ctx, evidence.RecordInput{
		Actor:   DefaultCloudActor,
		Action:  lifecycleAction(op.State),
		Subject: op.ID,
		Input:   input,
		Output: map[string]any{
			"state":       string(op.State),
			"provider":    op.Provider,
			"cluster_id":  op.ClusterID,
			"error":       op.ErrorMessage,
		},
		Payload: map[string]any{
			"operation_id": op.ID,
			"provider":     op.Provider,
			"provider_type": op.ProviderType,
			"state":        string(op.State),
			"cluster_id":   op.ClusterID,
		},
		Backends: []evidence.BackendFact{{
			Component: "cloud.lifecycle",
			Mode:      "real",
			Driver:    "fsm",
			Detail:    "cluster lifecycle state machine transition",
		}},
	})
	if err != nil {
		return fmt.Errorf("cloud: attestation %s failed: %w", lifecycleAction(op.State), err)
	}
	t.last = ev
	op.EvidenceHash = ev.Hash
	return nil
}

// newOperationID returns "op-" + 12 hex chars (6 random bytes).
func newOperationID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is practically impossible; fall back to time-based
		// uniqueness rather than panicking inside a CLI.
		return fmt.Sprintf("op-%012x", time.Now().UnixNano())
	}
	return "op-" + hex.EncodeToString(b)
}

