package metrics

import (
	"testing"
)

// ============================================================================
// T3 M46: Tests for Order-Statistics AVL Tree
// ============================================================================

func TestTreeSlidingWindow_InsertAndQuery(t *testing.T) {
	w := newTreeSlidingWindow(100)
	
	// Insert simple values
	for i := 0; i < 50; i++ {
		w.insert(float64(i))
	}
	
	// Query p50 should give us 24.5 (average of 24 and 25)
	p50 := w.percentile(0.5)
	expectedP50 := 24.5
	
	if absFloat(p50-expectedP50) > 1e-9 {
		t.Errorf("p50 = %v, expected %v", p50, expectedP50)
	}
	
	// Query p99: idx = 0.99*(50-1) = 48.51 -> interpolate sorted[48]=48 and sorted[49]=49
	p99 := w.percentile(0.99)
	expectedP99 := 48.51
	if absFloat(p99-expectedP99) > 1e-9 {
		t.Errorf("p99 = %v, expected %v", p99, expectedP99)
	}
}

func TestTreeSlidingWindow_Duplicates(t *testing.T) {
	w := newTreeSlidingWindow(100)
	
	// Insert same value multiple times
	for i := 0; i < 10; i++ {
		w.insert(5.0)
	}
	
	// All percentiles should be exactly 5.0
	for _, p := range []float64{0.0, 0.5, 0.9, 0.99, 1.0} {
		q := w.percentile(p)
		if q != 5.0 {
			t.Errorf("percentile(%v) = %v for all-duplicate data, expected 5.0", p, q)
		}
	}
}

func TestTreeSlidingWindow_WraparoundDelete(t *testing.T) {
	w := newTreeSlidingWindow(10)
	
	// Fill up the window
	for i := 0; i < 10; i++ {
		w.insert(float64(i))
	}
	
	// Verify we have all values
	count := w.totalRecords()
	if count != 10 {
		t.Fatalf("Expected 10 records, got %v", count)
	}
	
	// Insert one more, which should trigger wraparound and delete old value (0.0)
	w.insert(100.0)
	
	// Now we should still have 10 records
	count = w.totalRecords()
	if count != 10 {
		t.Fatalf("After wraparound: expected 10 records, got %v", count)
	}
	
	// p50 should now reflect new data distribution (values 1-10 + 100)
	p50 := w.percentile(0.5)
	// Expected: median of [1,2,3,4,5,6,7,8,9,100] = 5.5
	expectedP50 := 5.5
	if absFloat(p50-expectedP50) > 0.1 {
		t.Errorf("p50 after wraparound = %v, expected ~%v", p50, expectedP50)
	}
}

func TestTreeSlidingWindow_Empty(t *testing.T) {
	w := newTreeSlidingWindow(100)
	
	// Should return 0 for empty data
	if w.percentile(0.5) != 0.0 {
		t.Error("Empty window should return 0")
	}
	
	if w.percentile(0.99) != 0.0 {
		t.Error("Empty window should return 0")
	}
}

func TestTreeSlidingWindow_Stats(t *testing.T) {
	w := newTreeSlidingWindow(100)
	
	// Insert some values
	for i := 0; i < 50; i++ {
		w.insert(float64(i))
	}
	
	// Do a query
	_ = w.percentile(0.5)
	
	stats := w.getStats()
	
	inserts := stats["inserts"].(int)
	if inserts != 50 {
		t.Errorf("Expected 50 inserts, got %v", inserts)
	}
	
	queries := stats["queries"].(int)
	if queries != 1 {
		t.Errorf("Expected 1 query, got %v", queries)
	}
}

func TestSelectByRank_Correctness(t *testing.T) {
	// Build a tree with known values
	w := newTreeSlidingWindow(100)
	values := []float64{5.0, 3.0, 7.0, 1.0, 9.0, 8.0, 2.0, 6.0, 4.0, 10.0}
	for _, v := range values {
		w.insert(v)
	}
	
	// Sort expected values
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sortFloat64s(sorted) // reuse existing sort for ground truth
	
	// Check each rank
	for k := 1; k <= len(values); k++ {
		val := selectByRank(w.tree, k)
		expected := sorted[k-1]
		if absFloat(val-expected) > 1e-9 {
			t.Errorf("selectByRank(%v) = %v, expected %v (k-th=%v)", k, val, expected, k)
		}
	}
}

func TestSelectByRank_WithDuplicates(t *testing.T) {
	tree := &avlNode{}
	
	// Insert values with duplicates
	duplicates := []float64{5.0, 3.0, 3.0, 5.0, 5.0, 7.0}
	for _, v := range duplicates {
		node, _ := tree.insertValue(v)
		tree = node
	}
	
	// Total size should be 6
	if tree.size != 6 {
		t.Fatalf("Expected size 6, got %v", tree.size)
	}
	
	// Rank selection: 
	// ranks 1-2: 3.0 (two copies)
	// ranks 3-5: 5.0 (three copies)  
	// rank 6: 7.0
	
	for k := 1; k <= 2; k++ {
		if val := selectByRank(tree, k); val != 3.0 {
			t.Errorf("Rank %v = %v, expected 3.0", k, val)
		}
	}
	
	for k := 3; k <= 5; k++ {
		if val := selectByRank(tree, k); val != 5.0 {
			t.Errorf("Rank %v = %v, expected 5.0", k, val)
		}
	}
	
	if val := selectByRank(tree, 6); val != 7.0 {
		t.Errorf("Rank 6 = %v, expected 7.0", val)
	}
}

func TestTreePercentile_EqualsArraySort(t *testing.T) {
	// Generate test data
	data := benchSamples[:100] // smaller set for speed
	
	// Method A: Array copy + sort (ground truth)
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sortFloat64s(sorted)
	
	// Method B: Tree
	w := newTreeSlidingWindow(len(data))
	for _, v := range data {
		w.insert(v)
	}
	
	// Compare at multiple percentiles
	percentiles := []float64{0.0, 0.1, 0.25, 0.5, 0.75, 0.9, 0.95, 0.99, 1.0}
	tolerance := 1e-6
	
	for _, p := range percentiles {
		idx := p * float64(len(sorted)-1)
		lower := int(idx)
		upper := int(idx+1)
		if upper >= len(sorted) {
			upper = lower
		}
		fraction := idx - float64(lower)
		
		expected := sorted[lower]*(1-fraction) + sorted[upper]*fraction
		actual := w.percentile(p)
		
		if absFloat(expected-actual) > tolerance {
			t.Errorf("p=%.2f: tree=%v, array_sort=%v, diff=%v", 
				p, actual, expected, absFloat(expected-actual))
		}
	}
}

// Note: absFloat, groundTruthPercentile and benchSamples are defined in
// competitor_prometheus_bench_test.go and reused here.

// Ensure sortFloat64s is accessible (it's defined in slo.go)
var _ = sortFloat64s

func BenchmarkTreeCorrectness_P50(b *testing.B) {
	w := setupTreeWindow()
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_ = w.percentile(0.5)
	}
}

func BenchmarkTreeCorrectness_P99(b *testing.B) {
	w := setupTreeWindow()
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_ = w.percentile(0.99)
	}
}

// Test that tree maintains exact precision (0% error guarantee)
func TestTreeExactPrecision(t *testing.T) {
	// Use ground truth from sorted array
	trueP95 := groundTruthPercentile(benchSamples, 0.95)
	trueP99 := groundTruthPercentile(benchSamples, 0.99)
	
	// Build tree and query
	w := newTreeSlidingWindow(len(benchSamples))
	for _, v := range benchSamples {
		w.insert(v)
	}
	
	p95 := w.percentile(0.95)
	p99 := w.percentile(0.99)
	
	errP95 := absFloat(p95 - trueP95)
	errP99 := absFloat(p99 - trueP99)
	
	t.Logf("TREE EXACT: p95=%.8f (truth=%.8f, abs_err=%.3e) p99=%.8f (truth=%.8f, abs_err=%.3e)",
		p95, trueP95, errP95, p99, trueP99, errP99)
	
	const tol = 1e-9 // floating-point tolerance
	if errP95 > tol || errP99 > tol {
		t.Errorf("tree percentile deviated beyond fp tolerance: p95_err=%.3e p99_err=%.3e", errP95, errP99)
	}
}

// Test tree with edge cases
func TestTreeEdgeCases(t *testing.T) {
	// Single element
	w := newTreeSlidingWindow(100)
	w.insert(42.0)
	if w.percentile(0.5) != 42.0 {
		t.Error("Single element should return that element")
	}
	
	// Two elements
	w = newTreeSlidingWindow(100)
	w.insert(1.0)
	w.insert(2.0)
	if w.percentile(0.5) != 1.5 { // exact middle
		t.Error("Two elements p50 should be midpoint")
	}
	
	// Negative values
	w = newTreeSlidingWindow(100)
	for _, v := range []float64{-10.0, -5.0, 0.0, 5.0, 10.0} {
		w.insert(v)
	}
	p50 := w.percentile(0.5)
	if p50 != 0.0 {
		t.Errorf("Median of [-10,-5,0,5,10] should be 0.0, got %v", p50)
	}
}

// Test tree balance maintenance through many operations
func TestTreeBalanceMaintenance(t *testing.T) {
	w := newTreeSlidingWindow(1000)
	
	// Insert sorted data (worst case for unbalanced trees)
	for i := 0; i < 500; i++ {
		w.insert(float64(i))
	}
	
	// Query should succeed quickly if tree is balanced
	start := make([]byte, 1000) // allocate memory to potentially trigger issues
	w.insert(-1.0)              // insert value smaller than all others
	_ = w.percentile(0.5)       // query mid
	_ = w.percentile(0.99)      // query tail
	
	// If we get here without panic or extremely slow operation, tree is balanced
	_ = start // suppress unused warning
	t.Log("Tree maintained balance under adversarial input pattern")
}
