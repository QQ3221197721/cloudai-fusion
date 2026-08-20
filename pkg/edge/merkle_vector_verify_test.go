package edge

import (
	"fmt"
	"testing"
	"time"
)

// ============================================================================
// M24-26 Moat Verification: measured bandwidth savings & causal-ordering
// correctness. These tests emit the concrete numbers claimed by the modules
// (Merkle diff bandwidth-savings multiple; vector-clock causal order) so the
// T3 barrier can be certified against real data rather than assertions.
// ============================================================================

// TestMerkleDiff_MeasuredBandwidthSavings constructs a realistic edge-cloud
// sync scenario (1000 resources, 5% changed) and reports the actual bytes a
// Merkle delta-sync transfers versus a full re-sync.
func TestMerkleDiff_MeasuredBandwidthSavings(t *testing.T) {
	const total = 1000
	const changed = 50 // 5%

	base := map[string][]byte{}
	for i := 0; i < total; i++ {
		base[fmt.Sprintf("resource-%d", i)] = []byte(
			fmt.Sprintf(`{"id":%d,"apiVersion":"v1","spec":{"replicas":3,"image":"app:1.0"}}`, i))
	}
	self := NewMerkleTree(base)

	other := make(map[string][]byte, len(base))
	for k, v := range base {
		other[k] = v
	}
	for i := 0; i < changed; i++ {
		key := fmt.Sprintf("resource-%d", i*(total/changed))
		other[key] = []byte(
			fmt.Sprintf(`{"id":%d,"apiVersion":"v2","spec":{"replicas":5,"image":"app:2.0"}}`, i))
	}
	otherTree := NewMerkleTree(other)

	diff := self.ComputeDiff(otherTree)
	if diff == nil {
		t.Fatal("nil diff")
	}

	// Bytes a delta sync would transfer: only added + modified leaf payloads.
	var deltaBytes int64
	for _, a := range diff.Added {
		deltaBytes += a.Size
	}
	for _, m := range diff.Modified {
		deltaBytes += m.Size
	}
	// Bytes a full sync would transfer: every leaf in the target tree.
	var fullBytes int64
	for _, v := range other {
		fullBytes += int64(len(v))
	}

	if deltaBytes == 0 {
		t.Fatal("delta produced 0 bytes for a changed scenario (diff broken)")
	}
	savings := float64(fullBytes) / float64(deltaBytes)

	t.Logf("changed=%d/%d resources", len(diff.Modified), total)
	t.Logf("full-sync bytes    = %d", fullBytes)
	t.Logf("delta-sync bytes   = %d", deltaBytes)
	t.Logf("bandwidth savings  = %.2fx (full/delta)", savings)
	t.Logf("engine CompressionRatio = %.2fx", diff.Stats.CompressionRatio)
	t.Logf("engine BytesSaved       = %d", diff.Stats.BytesSaved)

	// Honest gate: with only 5% churn, delta sync must beat full sync by a wide
	// margin. If this ever fails, the moat claim is false and must be retracted.
	if savings < 2.0 {
		t.Errorf("bandwidth savings %.2fx below 2x — Merkle diff advantage not demonstrated", savings)
	}
}

// TestVectorClock_CausalOrderingCorrectness verifies the three causal
// relationships a vector clock must distinguish: before (-1), after (+1),
// concurrent (2), and equal (0).
func TestVectorClock_CausalOrderingCorrectness(t *testing.T) {
	// Two independent replicas over the same process set.
	a := NewCausalVectorClock([]string{"p1", "p2"}, nil)
	b := NewCausalVectorClock([]string{"p1", "p2"}, nil)

	// Equal at start.
	if got := a.CompareFromMaps(a.GetTimestamp(), b.GetTimestamp()); got != 0 {
		t.Errorf("fresh clocks should be equal (0), got %d", got)
	}

	// a advances p1 twice → a happens-after b on p1.
	clockAfter := map[string]int{"p1": 2, "p2": 0}
	clockBase := map[string]int{"p1": 0, "p2": 0}
	if got := a.CompareFromMaps(clockAfter, clockBase); got != 1 {
		t.Errorf("expected AFTER (1), got %d", got)
	}
	if got := a.CompareFromMaps(clockBase, clockAfter); got != -1 {
		t.Errorf("expected BEFORE (-1), got %d", got)
	}

	// Concurrent: a ahead on p1, b ahead on p2 — neither dominates.
	clockX := map[string]int{"p1": 2, "p2": 0}
	clockY := map[string]int{"p1": 0, "p2": 2}
	if got := a.CompareFromMaps(clockX, clockY); got != 2 {
		t.Errorf("expected CONCURRENT (2), got %d", got)
	}

	// Merge (element-wise max) should dominate both operands afterwards.
	m := NewCausalVectorClock([]string{"p1", "p2"}, nil)
	m.processes = map[string]int{"p1": 2, "p2": 0}
	other := NewCausalVectorClock([]string{"p1", "p2"}, nil)
	other.processes = map[string]int{"p1": 0, "p2": 2}
	m.Merge(other)
	merged := m.GetTimestamp()
	if merged["p1"] != 2 || merged["p2"] != 2 {
		t.Errorf("merge should take element-wise max, got %v", merged)
	}
	if got := m.CompareFromMaps(merged, clockX); got != 1 {
		t.Errorf("merged clock should be AFTER operand X (1), got %d", got)
	}
	t.Logf("causal relations verified: equal=0, after=1, before=-1, concurrent=2, merge=max")
}

// TestDeltaSync_ConflictResolutionDeterminism confirms concurrent writes are
// resolved deterministically (LWW with NodeID tie-break), a prerequisite for
// convergent edge replicas.
func TestDeltaSync_ConflictResolutionDeterminism(t *testing.T) {
	ts := time.Now()
	c1 := &VectorClockChange{Key: "k", Value: "v1", Timestamp: ts, NodeID: "node-a", Clock: map[string]int{"node-a": 1}}
	c2 := &VectorClockChange{Key: "k", Value: "v2", Timestamp: ts, NodeID: "node-b", Clock: map[string]int{"node-b": 1}}

	r1 := resolveConflict(c1, c2)
	r2 := resolveConflict(c1, c2)
	if r1.Winner.NodeID != r2.Winner.NodeID {
		t.Fatalf("non-deterministic resolution: %s vs %s", r1.Winner.NodeID, r2.Winner.NodeID)
	}
	// Equal timestamps → higher NodeID wins ("node-b" > "node-a").
	if r1.Winner.NodeID != "node-b" {
		t.Errorf("expected deterministic tie-break to node-b, got %s", r1.Winner.NodeID)
	}
	t.Logf("conflict resolution deterministic: winner=%s (%s)", r1.Winner.NodeID, r1.Resolution)
}
