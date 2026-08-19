package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// newTestLedger builds a real MemoryStore+EphemeralSigner ledger (golden pattern).
func newTestLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("generate signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		t.Fatalf("build ledger: %v", err)
	}
	return l
}

// stubProvider is a minimal credential-less Provider for FSM tests.
type stubProvider struct {
	name string
	ptype common.CloudProviderType
}

func (p stubProvider) Name() string                   { return p.name }
func (p stubProvider) Type() common.CloudProviderType { return p.ptype }
func (p stubProvider) Region() string                 { return "test-region" }
func (p stubProvider) ListClusters(ctx context.Context) ([]*ClusterInfo, error) {
	return []*ClusterInfo{}, nil
}
func (p stubProvider) GetCluster(ctx context.Context, clusterID string) (*ClusterInfo, error) {
	return nil, errors.New("stub")
}
func (p stubProvider) CreateCluster(ctx context.Context, req *CreateClusterRequest) (*ClusterInfo, error) {
	return nil, errors.New("stub")
}
func (p stubProvider) DeleteCluster(ctx context.Context, clusterID string) error {
	return errors.New("stub")
}
func (p stubProvider) ScaleCluster(ctx context.Context, clusterID string, nodeCount int) error {
	return errors.New("stub")
}
func (p stubProvider) GetKubeConfig(ctx context.Context, clusterID string) (string, error) {
	return "", errors.New("stub")
}
func (p stubProvider) ListNodes(ctx context.Context, clusterID string) ([]*NodeInfo, error) {
	return []*NodeInfo{}, nil
}
func (p stubProvider) GetNodeMetrics(ctx context.Context, clusterID, nodeID string) (*NodeMetrics, error) {
	return nil, errors.New("stub")
}
func (p stubProvider) ListGPUInstances(ctx context.Context) ([]*GPUInstanceInfo, error) {
	return []*GPUInstanceInfo{}, nil
}
func (p stubProvider) GetGPUPricing(ctx context.Context, gpuType string) (*GPUPricing, error) {
	return &GPUPricing{GPUType: gpuType, OnDemandPrice: 1.0, Currency: "USD"}, nil
}
func (p stubProvider) GetCostSummary(ctx context.Context, startTime, endTime string) (*CostSummary, error) {
	return &CostSummary{}, nil
}
func (p stubProvider) Ping(ctx context.Context) error { return nil }

// ============================================================================
// FSM table tests
// ============================================================================

func TestValidateTransitionAllLegalPaths(t *testing.T) {
	legal := []struct{ from, to ClusterLifecycleState }{
		{StatePending, StateProvisioning},
		{StatePending, StateFailed},
		{StateProvisioning, StateReady},
		{StateProvisioning, StateFailed},
		{StateProvisioning, StateDeleting},
		{StateReady, StateDeleting},
		{StateDeleting, StateDeleted},
		{StateDeleting, StateFailed},
		{StateFailed, StatePending}, // retry path
		{StateFailed, StateDeleting},
	}
	for _, tt := range legal {
		if err := ValidateTransition(tt.from, tt.to); err != nil {
			t.Errorf("legal transition %s→%s rejected: %v", tt.from, tt.to, err)
		}
	}
}

func TestValidateTransitionIllegal(t *testing.T) {
	illegal := []struct{ from, to ClusterLifecycleState }{
		{StateReady, StatePending},        // ready cannot restart
		{StateReady, StateProvisioning},   // ready cannot re-provision
		{StateReady, StateReady},          // self-loop
		{StatePending, StateReady},        // skip provisioning
		{StatePending, StateDeleted},      // skip everything
		{StateProvisioning, StatePending}, // no rewind
		{StateProvisioning, StateDeleted}, // skip deleting
		{StateDeleting, StateReady},       // no resurrection mid-delete
		{StateDeleting, StateProvisioning},
		{StateFailed, StateReady},   // failed must retry via pending
		{StateFailed, StateDeleted}, // must pass deleting
		{StateFailed, StateFailed},
	}
	for _, tt := range illegal {
		err := ValidateTransition(tt.from, tt.to)
		if err == nil {
			t.Errorf("illegal transition %s→%s accepted", tt.from, tt.to)
			continue
		}
		if !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("transition %s→%s: error does not wrap ErrInvalidTransition: %v", tt.from, tt.to, err)
		}
	}
}

func TestValidateTransitionTerminalResurrection(t *testing.T) {
	// deleted is terminal: EVERY successor must be rejected, with a message
	// that names the terminality.
	for _, to := range []ClusterLifecycleState{StatePending, StateProvisioning, StateReady, StateDeleting, StateFailed, StateDeleted} {
		err := ValidateTransition(StateDeleted, to)
		if err == nil {
			t.Fatalf("terminal resurrection deleted→%s accepted", to)
		}
		if !strings.Contains(err.Error(), "terminal") {
			t.Errorf("deleted→%s error should mention terminality: %v", to, err)
		}
	}
}

func TestValidateTransitionErrorListsAllowedStates(t *testing.T) {
	err := ValidateTransition(StateReady, StateProvisioning)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "deleting") {
		t.Errorf("error should list allowed successor (deleting): %v", msg)
	}
}

// ============================================================================
// OperationTracker journeys
// ============================================================================

func TestOperationTrackerHappyPath(t *testing.T) {
	root := t.TempDir()
	tracker, err := NewOperationTracker(root, newTestLedger(t))
	if err != nil {
		t.Fatalf("new tracker: %v", err)
	}
	ctx := context.Background()
	spec := &CreateClusterRequest{Name: "gpu-1", NodeCount: 4, NodeType: "a100", GPUNodeCount: 4, GPUNodeType: "nvidia-a100"}

	op, err := tracker.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, spec)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.HasPrefix(op.ID, "op-") || len(op.ID) != len("op-")+12 {
		t.Fatalf("ID format op-<hex12> violated: %q", op.ID)
	}
	if op.State != StatePending {
		t.Fatalf("fresh op state = %s, want pending", op.State)
	}
	if op.EvidenceHash == "" {
		t.Error("start attestation missing (ledger present)")
	}

	if err := tracker.MarkProvisioning(ctx, op.ID); err != nil {
		t.Fatalf("mark provisioning: %v", err)
	}
	if err := tracker.MarkReady(ctx, op.ID, "cluster-abc123"); err != nil {
		t.Fatalf("mark ready: %v", err)
	}

	got, err := tracker.Get(op.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != StateReady {
		t.Errorf("final state = %s, want ready", got.State)
	}
	if got.ClusterID != "cluster-abc123" {
		t.Errorf("cluster id = %q, want cluster-abc123", got.ClusterID)
	}
	if got.EvidenceHash == "" {
		t.Error("transition attestation missing")
	}
}

func TestOperationTrackerDeleteJourney(t *testing.T) {
	root := t.TempDir()
	tracker, _ := NewOperationTracker(root, newTestLedger(t))
	ctx := context.Background()

	op, _ := tracker.Start(ctx, stubProvider{name: "gcp", ptype: common.CloudProviderGCP}, &CreateClusterRequest{Name: "d", NodeCount: 1, NodeType: "t"})
	_ = tracker.MarkProvisioning(ctx, op.ID)
	_ = tracker.MarkReady(ctx, op.ID, "c1")
	if err := tracker.MarkDeleting(ctx, op.ID); err != nil {
		t.Fatalf("mark deleting: %v", err)
	}
	if err := tracker.MarkDeleted(ctx, op.ID); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}
	got, _ := tracker.Get(op.ID)
	if got.State != StateDeleted {
		t.Fatalf("state = %s, want deleted", got.State)
	}

	// Terminal: every further transition must fail.
	if err := tracker.MarkProvisioning(ctx, op.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal op accepted provisioning: %v", err)
	}
	if err := tracker.MarkDeleting(ctx, op.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal op accepted deleting: %v", err)
	}
	if err := tracker.Retry(ctx, op.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal op accepted retry: %v", err)
	}
}

func TestOperationTrackerFailureAndRetry(t *testing.T) {
	root := t.TempDir()
	tracker, _ := NewOperationTracker(root, newTestLedger(t))
	ctx := context.Background()

	op, _ := tracker.Start(ctx, stubProvider{name: "azure", ptype: common.CloudProviderAzure}, &CreateClusterRequest{Name: "f", NodeCount: 1, NodeType: "t"})
	if err := tracker.MarkFailed(ctx, op.ID, "quota exceeded"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	got, _ := tracker.Get(op.ID)
	if got.State != StateFailed || got.ErrorMessage != "quota exceeded" {
		t.Fatalf("got %+v", got)
	}

	if err := tracker.Retry(ctx, op.ID); err != nil {
		t.Fatalf("retry failed→pending: %v", err)
	}
	got, _ = tracker.Get(op.ID)
	if got.State != StatePending {
		t.Fatalf("after retry state = %s, want pending", got.State)
	}
	if got.ErrorMessage != "" {
		t.Errorf("retry should clear error message, got %q", got.ErrorMessage)
	}

	// Full second attempt succeeds.
	if err := tracker.MarkProvisioning(ctx, op.ID); err != nil {
		t.Fatalf("re-provision: %v", err)
	}
	if err := tracker.MarkReady(ctx, op.ID, "c2"); err != nil {
		t.Fatalf("re-ready: %v", err)
	}
}

func TestOperationTrackerIllegalTransitionsViaAPI(t *testing.T) {
	root := t.TempDir()
	tracker, _ := NewOperationTracker(root, newTestLedger(t))
	ctx := context.Background()
	spec := &CreateClusterRequest{Name: "x", NodeCount: 1, NodeType: "t"}

	t.Run("pending_cannot_ready", func(t *testing.T) {
		op, _ := tracker.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, spec)
		err := tracker.MarkReady(ctx, op.ID, "c")
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("pending→ready should fail, got %v", err)
		}
	})

	t.Run("ready_cannot_provisioning", func(t *testing.T) {
		op, _ := tracker.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, spec)
		_ = tracker.MarkProvisioning(ctx, op.ID)
		_ = tracker.MarkReady(ctx, op.ID, "c")
		if err := tracker.MarkProvisioning(ctx, op.ID); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("ready→provisioning should fail, got %v", err)
		}
	})

	t.Run("provisioning_cannot_pending", func(t *testing.T) {
		op, _ := tracker.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, spec)
		_ = tracker.MarkProvisioning(ctx, op.ID)
		if err := tracker.Retry(ctx, op.ID); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("provisioning→pending should fail, got %v", err)
		}
	})

	t.Run("deleting_cannot_ready", func(t *testing.T) {
		op, _ := tracker.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, spec)
		_ = tracker.MarkProvisioning(ctx, op.ID)
		_ = tracker.MarkReady(ctx, op.ID, "c")
		_ = tracker.MarkDeleting(ctx, op.ID)
		if err := tracker.MarkReady(ctx, op.ID, "c"); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("deleting→ready should fail, got %v", err)
		}
	})

	t.Run("unknown_op", func(t *testing.T) {
		if _, err := tracker.Get("op-doesnotexist"); !errors.Is(err, ErrOperationNotFound) {
			t.Fatalf("expected ErrOperationNotFound, got %v", err)
		}
		if err := tracker.MarkProvisioning(ctx, "op-doesnotexist"); !errors.Is(err, ErrOperationNotFound) {
			t.Fatalf("expected ErrOperationNotFound from Mark, got %v", err)
		}
	})
}

func TestOperationTrackerPersistenceRoundTrip(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	tracker1, _ := NewOperationTracker(root, newTestLedger(t))
	op1, _ := tracker1.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, &CreateClusterRequest{Name: "p1", NodeCount: 2, NodeType: "t"})
	_ = tracker1.MarkProvisioning(ctx, op1.ID)
	_ = tracker1.MarkReady(ctx, op1.ID, "cluster-p1")

	// Fresh tracker over the same root must see the LWW-merged history.
	tracker2, err := NewOperationTracker(root, nil)
	if err != nil {
		t.Fatalf("reopen tracker: %v", err)
	}
	got, err := tracker2.Get(op1.ID)
	if err != nil {
		t.Fatalf("round-trip get: %v", err)
	}
	if got.State != StateReady {
		t.Errorf("round-trip state = %s, want ready", got.State)
	}
	if got.ClusterID != "cluster-p1" {
		t.Errorf("round-trip cluster id = %q", got.ClusterID)
	}
	if got.RequestedSpec == nil || got.RequestedSpec.Name != "p1" {
		t.Errorf("round-trip spec lost: %+v", got.RequestedSpec)
	}

	// The JSONL file exists under <root>/cloud/ and every line parses.
	data, err := os.ReadFile(filepath.Join(root, "cloud", "operations.jsonl"))
	if err != nil {
		t.Fatalf("read operations.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 { // start + provisioning + ready rows
		t.Errorf("expected 3 rows (one per transition), got %d", len(lines))
	}
	for i, ln := range lines {
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(ln), &probe); err != nil {
			t.Errorf("row %d is not valid JSON (torn write?): %v", i+1, err)
		}
		if probe.ID != op1.ID {
			t.Errorf("row %d id = %q, want %q", i+1, probe.ID, op1.ID)
		}
	}
}

func TestOperationTrackerTornWriteTruncateGuard(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	tracker, _ := NewOperationTracker(root, newTestLedger(t))
	op, _ := tracker.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, &CreateClusterRequest{Name: "torn", NodeCount: 1, NodeType: "t"})

	path := filepath.Join(root, "cloud", "operations.jsonl")

	// Simulate an externally torn trailing write: bytes with NO terminating LF.
	// (The offset+truncate rollback prevents the tracker itself from ever
	// producing such a tail; this injection mimics an outside crash.)
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := fh.WriteString(`{"id":"op-tornpartial`); err != nil {
		t.Fatalf("append torn bytes: %v", err)
	}
	fh.Close()

	// The next append must self-heal: repair the torn tail to the last LF,
	// then write the new row atomically — leaving every line parseable.
	if err := tracker.MarkProvisioning(ctx, op.ID); err != nil {
		t.Fatalf("append after torn tail: %v", err)
	}
	data, _ := os.ReadFile(path)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		t.Fatalf("log does not end with LF after append")
	}
	for i, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(ln), &probe); err != nil {
			t.Fatalf("line %d still torn after self-heal: %v\nline: %s", i+1, err, ln)
		}
	}

	// The injected partial bytes are gone and the state read is intact.
	if strings.Contains(string(data), "op-tornpartial") {
		t.Error("torn tail was not truncated away")
	}
	got, err := tracker.Get(op.ID)
	if err != nil {
		t.Fatalf("get after heal: %v", err)
	}
	if got.State != StateProvisioning {
		t.Fatalf("post-heal state = %s, want provisioning", got.State)
	}
}

func TestOperationTrackerListNewestFirstAndLimit(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	tracker, _ := NewOperationTracker(root, nil)

	var ids []string
	for i := 0; i < 5; i++ {
		op, err := tracker.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, &CreateClusterRequest{Name: "n", NodeCount: 1, NodeType: "t"})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		ids = append(ids, op.ID)
	}

	all, err := tracker.List(0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("list len = %d, want 5", len(all))
	}
	// Newest first = reverse creation order.
	for i := 0; i < len(all); i++ {
		want := ids[len(ids)-1-i]
		if all[i].ID != want {
			t.Errorf("list[%d] = %s, want %s (newest-first violated)", i, all[i].ID, want)
		}
	}

	two, _ := tracker.List(2)
	if len(two) != 2 || two[0].ID != ids[4] || two[1].ID != ids[3] {
		t.Errorf("limit=2 returned wrong slice: %+v", two)
	}
}

func TestOperationTrackerNilLedgerDegrades(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	tracker, err := NewOperationTracker(root, nil) // nil ledger
	if err != nil {
		t.Fatalf("new tracker with nil ledger: %v", err)
	}

	op, err := tracker.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, &CreateClusterRequest{Name: "nil", NodeCount: 1, NodeType: "t"})
	if err != nil {
		t.Fatalf("start with nil ledger must not fail: %v", err)
	}
	if op.EvidenceHash != "" {
		t.Errorf("nil-ledger op must have empty EvidenceHash, got %q", op.EvidenceHash)
	}
	if err := tracker.MarkProvisioning(ctx, op.ID); err != nil {
		t.Fatalf("transition with nil ledger: %v", err)
	}
	if err := tracker.MarkReady(ctx, op.ID, "c-nil"); err != nil {
		t.Fatalf("ready with nil ledger: %v", err)
	}
	got, _ := tracker.Get(op.ID)
	if got.State != StateReady || got.EvidenceHash != "" {
		t.Errorf("nil-ledger degradation violated: %+v", got)
	}

	// Attest() with nil ledger is a documented no-op.
	if h, err := tracker.Attest(ctx, "cloud.plan", "s", nil, nil); err != nil || h != "" {
		t.Errorf("nil-ledger Attest = (%q, %v), want (\"\", nil)", h, err)
	}
}

func TestOperationTrackerEveryTransitionAttested(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	lgr := newTestLedger(t)
	tracker, _ := NewOperationTracker(root, lgr)

	op, _ := tracker.Start(ctx, stubProvider{name: "aws", ptype: common.CloudProviderAWS}, &CreateClusterRequest{Name: "att", NodeCount: 1, NodeType: "t"})
	seqAfterStart := lgrLastSeq(t, lgr)

	_ = tracker.MarkProvisioning(ctx, op.ID)
	seqAfterProv := lgrLastSeq(t, lgr)
	if seqAfterProv <= seqAfterStart {
		t.Errorf("provisioning attestation missing: seq %d → %d", seqAfterStart, seqAfterProv)
	}

	_ = tracker.MarkReady(ctx, op.ID, "c-att")
	if seq := lgrLastSeq(t, lgr); seq <= seqAfterProv {
		t.Errorf("ready attestation missing: seq %d → %d", seqAfterProv, seq)
	}

	_ = tracker.MarkDeleting(ctx, op.ID)
	_ = tracker.MarkDeleted(ctx, op.ID)
	if seq := lgrLastSeq(t, lgr); seq != seqAfterProv+3 {
		t.Errorf("expected 3 more receipts after provisioning, last seq %d", seq)
	}

	// Start itself must have emitted exactly one receipt (cloud.op.start).
	evs, err := lgr.Store().All(context.Background())
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	actions := map[string]int{}
	for _, ev := range evs {
		actions[ev.Action]++
	}
	for _, want := range []string{"cloud.op.start", "cloud.op.provisioning", "cloud.op.ready", "cloud.op.deleting", "cloud.op.deleted"} {
		if actions[want] == 0 {
			t.Errorf("missing attestation action %q (have %v)", want, actions)
		}
	}
}

// lgrLastSeq reads the highest seq currently in the ledger's memory store.
func lgrLastSeq(t *testing.T, l *evidence.Ledger) uint64 {
	t.Helper()
	last, err := l.Store().Last(context.Background())
	if err != nil || last == nil {
		return 0
	}
	return last.Seq
}
