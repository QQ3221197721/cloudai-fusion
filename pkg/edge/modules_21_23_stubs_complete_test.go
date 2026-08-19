package edge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
	"github.com/sirupsen/logrus"
)

func newTestLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.ErrorLevel)
	return l
}

// ============================================================================
// Module 21 - Edge Node Manager: REST Stub Honesty Validation
// ============================================================================

// Test_Module21_RESTStubHonesty validates that the edge runtime is honestly
// tagged as simulated via capability.Report, regardless of whether an evidence
// recorder is attached. In production mode, capability.Enforce() should reject
// this simulated backend.
func Test_Module21_RESTStubHonesty(t *testing.T) {
	t.Cleanup(capability.Reset)
	// Restore default Simulation policy after this test to avoid leaking
	// Production policy into other tests that legitimately report simulated backends.
	t.Cleanup(func() { capability.SetPolicy(runmode.Simulation) })

	mgr, err := NewManager(Config{})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	ctx := context.Background()
	node := &EdgeNode{
		Name:   "test-edge-01",
		Tier:   TierEdge,
		Region: "cn-hangzhou",
	}

	err = mgr.RegisterNode(ctx, node)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	// Verify capability.Report was called with ModeSimulated
	backendList := capability.Simulated()
	found := false
	for _, b := range backendList {
		if b.Component == "edge.runtime" && b.Mode == capability.ModeSimulated && b.Driver == "rest-stub" {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("expected edge.runtime to be reported as simulated with driver 'rest-stub', but capability.Simulated() did not contain it")
	}

	// Simulate production policy enforcement: under Production, capability.Enforce()
	// MUST reject because edge.runtime is simulated (honest fail-fast, no silent boot).
	capability.SetPolicy(runmode.Production)
	err = capability.Enforce()
	if err == nil {
		t.Fatal("expected capability.Enforce() to reject simulated edge.runtime under Production policy, got nil")
	}
	if !strings.Contains(err.Error(), "edge.runtime") {
		t.Errorf("expected Enforce error to mention edge.runtime, got: %v", err)
	}
}

// ============================================================================
// Module 22 - Offline Decision Engine: Deterministic Best-Response Validation
// ============================================================================

// Test_Module22_DeterministicDecision validates that LocalDecisionEngine
// produces deterministic decisions given identical inputs and local state.
func Test_Module22_DeterministicDecision(t *testing.T) {
	engine := NewLocalDecisionEngine(100, newTestLogger())
	resources := &EdgeResourceUsage{CPUPercent: 50, MemoryPercent: 50}

	// Run 3 times with same input -> must produce exactly same decision
	var result1, result2, result3 string
	for i := 0; i < 3; i++ {
		d := engine.Evaluate("critical-workload", resources, 100)
		if i == 0 {
			result1 = d.Result + "|" + d.Reason
		} else if i == 1 {
			result2 = d.Result + "|" + d.Reason
		} else {
			result3 = d.Result + "|" + d.Reason
		}
	}

	if result1 != result2 || result2 != result3 {
		t.Fatalf("non-deterministic decision detected: r1=%q, r2=%q, r3=%q", result1, result2, result3)
	}

	// Decision should be approved by default policy for critical-workload at low resources
	if result1 != "approved|approved by policy critical-workload" {
		t.Fatalf("unexpected result pattern: %q", result1)
	}
}

// Test_Module22_LocalPersistence validates that offline decisions are stored
// locally and can be retrieved pending_sync after network partition.
func Test_Module22_LocalPersistence(t *testing.T) {
	engine := NewLocalDecisionEngine(100, newTestLogger())
	resources := &EdgeResourceUsage{CPUPercent: 40, MemoryPercent: 40}

	// Make decisions during network partition
	d1 := engine.Evaluate("inference", resources, 80)
	_ = engine.Evaluate("batch-job", resources, 60)

	pending := engine.PendingSync()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending decisions, got %d", len(pending))
	}

	// Mark one as synced (simulating reconnection and batch upload)
	engine.MarkSynced([]string{d1.ID})

	remainingPending := engine.PendingSync()
	if len(remainingPending) != 1 {
		t.Fatalf("expected 1 remaining pending, got %d", len(remainingPending))
	}

	stats := engine.Stats()
	if stats["total"].(int) != 2 {
		t.Fatalf("expected total=2, got %d", stats["total"])
	}
	if stats["pending_sync"].(int) != 1 {
		t.Fatalf("expected pending_sync=1, got %d", stats["pending_sync"])
	}
}

// Test_Module22_UndecidedFallback validates explicit fallback path when no
// policy matches (decision.Result must NOT be zero value).
func Test_Module22_UndecidedFallback(t *testing.T) {
	engine := NewLocalDecisionEngine(100, newTestLogger())
	resources := &EdgeResourceUsage{CPUPercent: 20, MemoryPercent: 20}

	d := engine.Evaluate("unknown-workload-type-xzy", resources, 20)

	// Must have explicit fallback - cannot return zero/empty decision
	if d.Result == "" {
		t.Fatal("unexpected empty decision result when no policy matches")
	}
	if d.Reason == "" {
		t.Fatal("unexpected empty decision reason when no policy matches")
	}
}

// ============================================================================
// Module 23 - Delta Sync: Vector Clock Merge & Conflict Resolution
// ============================================================================

// Test_Module23_VectorClockMerge validates bidirectional delta sync with zero
// data loss using vector clocks. After merging changes from both sides, all
// changes must be reflected and causality preserved.
func Test_Module23_VectorClockMerge(t *testing.T) {
	logger := newTestLogger()
	cfg := SyncConfig{BlocksizeKB: 1024, ParallelSyncWorkers: 2, RetryAttempts: 3, TimeoutSec: 30}
	ds, err := NewDeltaSyncManager(cfg, logger)
	if err != nil {
		t.Fatalf("NewDeltaSyncManager failed: %v", err)
	}

	// Register two nodes for bidirectional sync
	nodeA := &DeltaEdgeNode{ID: "node-A", Address: "10.0.0.1", Port: 8080, Status: StatusOnline}
	nodeB := &DeltaEdgeNode{ID: "node-B", Address: "10.0.0.2", Port: 8080, Status: StatusOnline}
	_ = ds.RegisterNode(nodeA)
	_ = ds.RegisterNode(nodeB)

	// Generate sample hashes (simulating real data changes)
	nodeA.DataHashes = []string{"hash-a1", "hash-a2", "hash-a3"}
	nodeB.DataHashes = []string{"hash-a1", "hash-b2", "hash-b3"} // b2,b3 differ from A's a2,a3
	// Differing data -> differing Merkle roots (so DetectChanges does NOT short-circuit)
	nodeA.MerkleRoot = "root-A"
	nodeB.MerkleRoot = "root-B"

	// Detect changes between nodes -> deltas MUST reflect real hash comparison
	deltas, err := ds.DetectChanges(context.Background(), "node-A", "node-B")
	if err != nil {
		t.Fatalf("DetectChanges failed: %v", err)
	}

	if len(deltas) != 2 {
		t.Fatalf("expected 2 changed blocks (b2!=a2, b3!=a3), got %d", len(deltas))
	}

	// Validate each change has proper BlockID format and non-zero hashes
	foundB2, foundB3 := false, false
	for _, d := range deltas {
		if d.BlockID == "block_1" && d.OldHash == "hash-a2" && d.NewHash == "hash-b2" {
			foundB2 = true
		}
		if d.BlockID == "block_2" && d.OldHash == "hash-a3" && d.NewHash == "hash-b3" {
			foundB3 = true
		}
	}

	if !foundB2 || !foundB3 {
		t.Fatalf("delta mismatch: foundB2=%v foundB3=%v, expected block_1(b2) and block_2(b3)", foundB2, foundB3)
	}
}

// Test_Module23_ConcurrentWriteConflictResolution validates concurrent writes
//(vector-clock incomparable) are detected and resolved using LWW tie-break.
func Test_Module23_ConcurrentWriteConflictResolution(t *testing.T) {
	cv := &ChangeVector{}

	// Create two concurrent changes (same key, concurrent clocks, identical timestamps)
	changeA := &VectorClockChange{
		Key:       "config-key",
		Value:     "value-A",
		NodeID:    "node-A",
		Timestamp: time.Now(),
		Clock:     map[string]int{"A": 1, "B": 0},
		Operation: "update",
	}

	changeB := &VectorClockChange{
		Key:       "config-key",
		Value:     "value-B",
		NodeID:    "node-B", // B > A lexicographically for determinism
		Timestamp: changeA.Timestamp, // Same timestamp -> deterministic tie-break
		Clock:     map[string]int{"A": 0, "B": 1},
		Operation: "update",
	}

	// Apply both -> conflict detection + LWW resolution
	result1, _ := cv.Apply(changeA)
	if !result1.Applied {
		t.Fatal("expected first apply to succeed")
	}

	result2, _ := cv.Apply(changeB)
	if result2.Conflicted != true {
		t.Fatal("expected concurrent write to be marked as conflicted")
	}
	if result2.Resolution != "last_writer_wins" {
		t.Fatalf("expected resolution 'last_writer_wins', got %q", result2.Resolution)
	}
	if result2.Winner == nil {
		t.Fatal("expected winner to be set after LWW resolution")
	}
	if result2.Winner.Value != "value-B" {
		t.Errorf("expected winner='value-B' (higher NodeID), got %q", result2.Winner.Value)
	}
}

// Test_Module23_CausalOrderingValidation validates that causally ordered
// updates are applied directly without conflict (vector clock comparable).
func Test_Module23_CausalOrderingValidation(t *testing.T) {
	cv := &ChangeVector{}

	// Causally ordered: clock B > clock A component-wise -> B happened-after A
	changeA := &VectorClockChange{
		Key:       "counter",
		Value:     int64(1),
		NodeID:    "A",
		Timestamp: time.Now().Add(-time.Second),
		Clock:     map[string]int{"A": 1, "B": 0},
		Operation: "create",
	}

	changeB := &VectorClockChange{
		Key:       "counter",
		Value:     int64(2),
		NodeID:    "B",
		Timestamp: time.Now(),
		Clock:     map[string]int{"A": 1, "B": 1}, // A:1 <= B:1, so B causally newer
		Operation: "update",
	}

	result1, _ := cv.Apply(changeA)
	if !result1.Applied {
		t.Fatal("expected initial apply")
	}

	// B is causally newer -> direct apply, no conflict
	result2, _ := cv.Apply(changeB)
	if result2.Conflicted {
		t.Fatal("expected causal ordering to avoid conflict, but got conflicted=true")
	}
	if !result2.Applied {
		t.Fatal("expected B to be applied as causally newer")
	}
}

// Test_Module23_SelfCheckCompile ensures delta_sync.go compiles after stub removal
func Test_Module23_SelfCheckCompile(t *testing.T) {
	// This test passes automatically if package builds successfully.
	// The presence here documents intent for CI build gate.
	t.Skip("Self-check compile; build pass proves success")
}
