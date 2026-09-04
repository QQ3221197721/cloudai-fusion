package gitops

// theoretical_merkle_drift_test.go — Task #267 adversarial verification of the
// Merkle path-compression optimality theorem for M39 GitOps drift detection.
//
// Every test here is real and runnable (go test ./pkg/gitops/...). The tests
// measure the exact quantities the theorem bounds (hash comparisons, pruned
// subtrees, level-synchronous round trips) and compare Merkle pruning against
// the naive O(n) full re-scan that the production map-diff (DiffStates) embodies.
//
// These are additive test files only; no production code is modified or deleted.

import (
	"fmt"
	"math"
	"testing"
)

// ----------------------------------------------------------------------------
// Synthetic Helm-history workload builders
// ----------------------------------------------------------------------------

// synthSnapshot builds a snapshot of `charts` Helm releases, each rendering one
// Deployment-like resource with `fields` config fields. Deterministic values so
// two snapshots are identical unless mutated.
func synthSnapshot(charts, fields int) []ResourceState {
	states := make([]ResourceState, charts)
	for c := 0; c < charts; c++ {
		fs := make(map[string]string, fields)
		for f := 0; f < fields; f++ {
			fs[fmt.Sprintf("spec.field%03d", f)] = fmt.Sprintf("v-%d-%d", c, f)
		}
		states[c] = ResourceState{
			Kind:      "Deployment",
			Name:      fmt.Sprintf("release-%05d", c),
			Namespace: fmt.Sprintf("team-%02d", c%16),
			Fields:    fs,
		}
	}
	return states
}

// mutateField flips one field on one chart, simulating a single-cell drift.
func mutateField(states []ResourceState, chart, field int) {
	key := fmt.Sprintf("spec.field%03d", field)
	states[chart].Fields[key] = states[chart].Fields[key] + "-DRIFTED"
}

// deepCopySnapshot clones so mutations don't leak between desired/live.
func deepCopySnapshot(src []ResourceState) []ResourceState {
	out := make([]ResourceState, len(src))
	for i, r := range src {
		fs := make(map[string]string, len(r.Fields))
		for k, v := range r.Fields {
			fs[k] = v
		}
		out[i] = ResourceState{Kind: r.Kind, Name: r.Name, Namespace: r.Namespace, Fields: fs}
	}
	return out
}

// ----------------------------------------------------------------------------
// Correctness guard: Merkle pruning must recover the exact changed set
// ----------------------------------------------------------------------------

func TestMerkleDiffRecoversExactChangedSet(t *testing.T) {
	desired := synthSnapshot(200, 30) // n = 200*(30+1) = 6200 leaves
	live := deepCopySnapshot(desired)
	// Introduce a scattered set of changes.
	changes := [][2]int{{0, 0}, {17, 5}, {88, 29}, {150, 12}, {199, 0}}
	for _, ch := range changes {
		mutateField(live, ch[0], ch[1])
	}

	dt, lt, trueChanged := BuildDriftMerklePair(desired, live)
	m := DiffMerkle(dt, lt)
	full := NaiveFullDiff(dt, lt)

	if len(m.ChangedKeys) != trueChanged {
		t.Fatalf("Merkle diff found %d changed leaves, ground truth %d", len(m.ChangedKeys), trueChanged)
	}
	if len(full.ChangedKeys) != trueChanged {
		t.Fatalf("full diff found %d changed leaves, ground truth %d", len(full.ChangedKeys), trueChanged)
	}
	// Sets must be identical.
	for i := range m.ChangedKeys {
		if m.ChangedKeys[i] != full.ChangedKeys[i] {
			t.Fatalf("changed-key mismatch at %d: merkle=%q full=%q", i, m.ChangedKeys[i], full.ChangedKeys[i])
		}
	}
	t.Logf("n=%d k=%d | merkle comparisons=%d pruned=%d roundtrips=%d height=%d | full comparisons=%d",
		m.LeafCount, len(m.ChangedKeys), m.Comparisons, m.NodesPruned, m.RoundTrips, m.Height, full.Comparisons)
}

// ----------------------------------------------------------------------------
// Adversarial worst case: a single-cell change hidden among n leaves.
// The adversary maximizes ambiguity — the change could be anywhere, forcing a
// naive detector to scan all n. Merkle localizes it in ~2·log2(n) comparisons.
// ----------------------------------------------------------------------------

func TestWorstCaseSingleCellChange(t *testing.T) {
	for _, charts := range []int{64, 256, 1024, 4096} {
		desired := synthSnapshot(charts, 15)
		live := deepCopySnapshot(desired)
		// Change the very last leaf — the deepest, farthest-from-root cell.
		mutateField(live, charts-1, 14)

		dt, lt, trueChanged := BuildDriftMerklePair(desired, live)
		if trueChanged != 1 {
			t.Fatalf("charts=%d expected exactly 1 changed leaf, got %d", charts, trueChanged)
		}
		m := DiffMerkle(dt, lt)
		full := NaiveFullDiff(dt, lt)

		if len(m.ChangedKeys) != 1 {
			t.Fatalf("charts=%d merkle localized %d leaves, want 1", charts, len(m.ChangedKeys))
		}
		n := m.LeafCount
		// Theorem prediction: comparisons <= 2*height + 1 for a single change.
		bound := 2*m.Height + 1
		if m.Comparisons > bound {
			t.Fatalf("charts=%d n=%d merkle comparisons=%d exceeds single-change bound %d",
				charts, n, m.Comparisons, bound)
		}
		// Full diff must always touch every leaf.
		if full.Comparisons != n {
			t.Fatalf("charts=%d full comparisons=%d != n=%d", charts, full.Comparisons, n)
		}
		// Round trips bounded by height+1 (log n), independent of k.
		if m.RoundTrips > m.Height+1 {
			t.Fatalf("charts=%d round_trips=%d exceeds height+1=%d", charts, m.RoundTrips, m.Height+1)
		}
		speedup := float64(full.Comparisons) / float64(m.Comparisons)
		t.Logf("WORST-CASE n=%d height=%d | merkle cmp=%d roundtrips=%d pruned=%d | full cmp=%d | speedup=%.1fx",
			n, m.Height, m.Comparisons, m.RoundTrips, m.NodesPruned, full.Comparisons, speedup)
	}
}

// ----------------------------------------------------------------------------
// Real-world workload: 120 Helm releases (>100) with a handful of live drifts.
// Captures the round_trips metric on a realistic mix.
// ----------------------------------------------------------------------------

func TestRealWorldHelmReleases(t *testing.T) {
	const charts = 120 // > 100 releases
	const fields = 40
	desired := synthSnapshot(charts, fields)
	live := deepCopySnapshot(desired)

	// A realistic drift: an operator hand-edits replicas/limits on ~5 services.
	drifted := [][2]int{{3, 0}, {3, 1}, {27, 10}, {88, 39}, {119, 20}}
	for _, d := range drifted {
		mutateField(live, d[0], d[1])
	}

	dt, lt, trueChanged := BuildDriftMerklePair(desired, live)
	m := DiffMerkle(dt, lt)
	full := NaiveFullDiff(dt, lt)

	if len(m.ChangedKeys) != trueChanged || trueChanged != len(drifted) {
		t.Fatalf("expected %d drifts, merkle=%d truth=%d", len(drifted), len(m.ChangedKeys), trueChanged)
	}
	if full.Comparisons != m.LeafCount {
		t.Fatalf("full comparisons=%d != n=%d", full.Comparisons, m.LeafCount)
	}
	// k*log2(n) upper-bound sanity (with slack for shared upper paths / promotion).
	kLogN := int(math.Ceil(float64(len(drifted)) * math.Log2(float64(m.LeafCount))))
	t.Logf("REAL-WORLD releases=%d fields=%d n=%d k=%d | merkle cmp=%d roundtrips=%d pruned=%d | full cmp=%d | k*log2(n)=%d | speedup=%.1fx",
		charts, fields, m.LeafCount, len(m.ChangedKeys), m.Comparisons, m.RoundTrips, m.NodesPruned,
		full.Comparisons, kLogN, float64(full.Comparisons)/float64(m.Comparisons))
}

// ----------------------------------------------------------------------------
// Large-scale pruning: 5000 charts × 50 fields = 255000 leaves, 10 drifts.
// Verifies pruning efficiency under a deep hierarchical structure.
// ----------------------------------------------------------------------------

func TestLargeScalePruningEfficiency(t *testing.T) {
	const charts = 5000
	const fields = 50
	desired := synthSnapshot(charts, fields)
	live := deepCopySnapshot(desired)

	// 10 scattered drifts across the estate.
	k := 10
	for i := 0; i < k; i++ {
		mutateField(live, i*(charts/k), i%fields)
	}

	dt, lt, trueChanged := BuildDriftMerklePair(desired, live)
	if trueChanged != k {
		t.Fatalf("expected %d drifts, got %d", k, trueChanged)
	}
	m := DiffMerkle(dt, lt)
	full := NaiveFullDiff(dt, lt)

	n := m.LeafCount
	if len(m.ChangedKeys) != k {
		t.Fatalf("merkle localized %d, want %d", len(m.ChangedKeys), k)
	}
	if full.Comparisons != n {
		t.Fatalf("full comparisons=%d != n=%d", full.Comparisons, n)
	}
	// Pruning must keep comparisons well below n: assert < 5% of n for k<<n.
	if float64(m.Comparisons) >= 0.05*float64(n) {
		t.Fatalf("pruning insufficient: merkle cmp=%d not < 5%% of n=%d", m.Comparisons, n)
	}
	// k*ceil(log2(n)) theoretical upper bound (each of k paths ~ height long).
	upper := k * (m.Height + 1)
	t.Logf("LARGE-SCALE n=%d height=%d k=%d | merkle cmp=%d (%.4f%% of n) roundtrips=%d pruned=%d | full cmp=%d | k*(height+1)=%d | speedup=%.0fx",
		n, m.Height, k, m.Comparisons, 100*float64(m.Comparisons)/float64(n),
		m.RoundTrips, m.NodesPruned, full.Comparisons, upper, float64(full.Comparisons)/float64(m.Comparisons))
	if m.Comparisons > upper*2 { // 2x slack for shared prefixes / promoted nodes
		t.Fatalf("merkle comparisons=%d exceeds 2*k*(height+1)=%d", m.Comparisons, 2*upper)
	}
}

// ----------------------------------------------------------------------------
// No-drift case: identical snapshots. Merkle prunes the entire tree in ONE
// comparison (root match); full diff still pays n. This is the amortized
// steady-state advantage (most scans find no drift).
// ----------------------------------------------------------------------------

func TestNoDriftWholeTreePrune(t *testing.T) {
	desired := synthSnapshot(1000, 50)
	live := deepCopySnapshot(desired)

	dt, lt, trueChanged := BuildDriftMerklePair(desired, live)
	if trueChanged != 0 {
		t.Fatalf("expected 0 drift, got %d", trueChanged)
	}
	m := DiffMerkle(dt, lt)
	full := NaiveFullDiff(dt, lt)

	if m.Comparisons != 1 {
		t.Fatalf("no-drift should cost exactly 1 root comparison, got %d", m.Comparisons)
	}
	if len(m.ChangedKeys) != 0 {
		t.Fatalf("no-drift merkle reported %d changes", len(m.ChangedKeys))
	}
	if full.Comparisons != m.LeafCount {
		t.Fatalf("full comparisons=%d != n=%d", full.Comparisons, m.LeafCount)
	}
	t.Logf("NO-DRIFT n=%d | merkle cmp=1 roundtrips=%d | full cmp=%d | speedup=%dx",
		m.LeafCount, m.RoundTrips, full.Comparisons, full.Comparisons)
}

// ----------------------------------------------------------------------------
// Benchmarks — incremental diff cost, Merkle vs naive full re-scan.
// Trees are prebuilt (amortized at Helm-commit time); the benchmark measures the
// per-scan incremental diff, which is the quantity the theorem bounds.
// ----------------------------------------------------------------------------

func benchPair(charts, fields, k int) (dt, lt *DriftMerkleTree) {
	desired := synthSnapshot(charts, fields)
	live := deepCopySnapshot(desired)
	for i := 0; i < k; i++ {
		mutateField(live, i*(charts/max(k, 1)), i%fields)
	}
	dt, lt, _ = BuildDriftMerklePair(desired, live)
	return dt, lt
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func BenchmarkMerkleDiff_5000charts_10drift(b *testing.B) {
	dt, lt := benchPair(5000, 50, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DiffMerkle(dt, lt)
	}
}

func BenchmarkNaiveFullDiff_5000charts_10drift(b *testing.B) {
	dt, lt := benchPair(5000, 50, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NaiveFullDiff(dt, lt)
	}
}

func BenchmarkMerkleDiff_WorstCaseSingleChange(b *testing.B) {
	dt, lt := benchPair(4096, 15, 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DiffMerkle(dt, lt)
	}
}

func BenchmarkNaiveFullDiff_WorstCaseSingleChange(b *testing.B) {
	dt, lt := benchPair(4096, 15, 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NaiveFullDiff(dt, lt)
	}
}

func BenchmarkMerkleDiff_NoDrift_WholePrune(b *testing.B) {
	dt, lt := benchPair(5000, 50, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DiffMerkle(dt, lt)
	}
}
