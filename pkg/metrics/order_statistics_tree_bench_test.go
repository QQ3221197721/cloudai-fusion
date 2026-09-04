package metrics

import (
	"testing"
)

// ============================================================================
// T3 M46: Benchmarks for Order-Statistics AVL Tree
// ============================================================================

// BenchmarkSetup_OurSlidingWindowTree creates a full 10k-sample tree from benchSamples
func setupTreeWindow() *treeSlidingWindow {
	w := newTreeSlidingWindow(len(benchSamples))
	for _, v := range benchSamples {
		w.insert(v)
	}
	return w
}

// -------------------------------------------------------------------------
// Part A: Tree Insert Performance (should be O(log n), ~200-400ns per insert)
// -------------------------------------------------------------------------

// BenchmarkInsertThroughput_TreeSlidingWindow measures the INSERT cost of our
// tree-backed sliding window. This is expected to be SLOWER than the raw ring
// buffer (7ns) because we're paying for O(log n) tree balancing, but still fast
// in absolute terms.
func BenchmarkInsertThroughput_TreeSlidingWindow(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w := newTreeSlidingWindow(10000)
		v := benchSamples[i%len(benchSamples)]
		w.insert(v)
		_ = w
	}
}

// BenchmarkInsertThroughput_TreeCumulative builds a tree incrementally over the
// entire dataset, measuring real-world cumulative insert cost for M samples.
func BenchmarkInsertThroughput_TreeCumulative(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w := newTreeSlidingWindow(10000)
		for _, v := range benchSamples {
			w.insert(v)
		}
		_ = w.tree
	}
}

// -------------------------------------------------------------------------
// Part B: Tree Query Performance (the core fix - should be <200μs!)
// -------------------------------------------------------------------------

// BenchmarkQueryLatency_TreeSlidingWindow measures QUERY latency using the
// order-statistics tree. This is the ACTUAL FIX that replaces the O(n²) sort.
// EXPECTATION: 10-50μs vs 8300000ns (8.3ms) current = 166x-830x improvement!
func BenchmarkQueryLatency_TreeSlidingWindow(b *testing.B) {
	w := setupTreeWindow()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = w.percentile(0.99)
	}
}

// BenchmarkQueryLatency_TreeSlidingWindow_P95 tests p95 which should be same
// complexity as p99 (both O(log n))
func BenchmarkQueryLatency_TreeSlidingWindow_P95(b *testing.B) {
	w := setupTreeWindow()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = w.percentile(0.95)
	}
}

// -------------------------------------------------------------------------
// Part C: Direct Tree Operation Benchmarks (for debugging/profiling)
// -------------------------------------------------------------------------

// BenchmarkInsert_OneToTree measures pure AVL tree insert overhead
func BenchmarkInsert_OneToTree(b *testing.B) {
	var tree *avlNode
	vals := []float64{1.0, 2.0, 3.0, 4.0, 5.0} // small set for cache efficiency
	
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tree = nil
		for _, v := range vals {
			v2 := benchSamples[i%len(benchSamples)] + v*0.01
			tree, _ = tree.insertValue(v2)
		}
		_ = tree.size
	}
}

// BenchmarkDelete_FromTree measures pure AVL tree delete overhead
func BenchmarkDelete_FromTree(b *testing.B) {
	tree := setupTreeWindow().tree
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		v := benchSamples[i%len(benchSamples)]
		tree, _ = tree.deleteValue(v)
		if tree == nil {
			// Rebuild for next iteration
			tree = setupTreeWindow().tree
		}
	}
}

// BenchmarkSelectByRank measures rank-based selection cost directly
func BenchmarkSelectByRank_Direct(b *testing.B) {
	tree := setupTreeWindow().tree
	b.ReportAllocs()
	b.ResetTimer()

	rank := int(0.99 * float64(tree.size)) + 1
	
	for i := 0; i < b.N; i++ {
		val := selectByRank(tree, rank)
		_ = val
	}
}

// -------------------------------------------------------------------------
// Part D: Comparison Baselines (current implementation)
// -------------------------------------------------------------------------

// BenchmarkQuery_Latency_CurlyImplementation is the EXISTING O(n²) benchmark
// (kept here for comparison after integration)
func BenchmarkQuery_Latency_CurrentImplementation(b *testing.B) {
	data := make([]float64, len(benchSamples))
	copy(data, benchSamples)
	
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = percentile(data, 0.99)
	}
}

// -------------------------------------------------------------------------
// Part E: End-to-End Integration Benchmarks
// -------------------------------------------------------------------------

// BenchmarkMixedWorkload_TreeBased simulates realistic load: lots of inserts
// plus periodic queries (e.g., query every 1000th insert). This models actual
// production behavior where Q << M.
func BenchmarkMixedWorkload_TreeBased(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w := newTreeSlidingWindow(10000)
		
		// Simulate mixed workload: 1000 inserts, then 1 query
		for j := 0; j < 1000; j++ {
			idx := (i+j) % len(benchSamples)
			w.insert(benchSamples[idx])
		}
		
		// Periodic query
		_ = w.percentile(0.99)
	}
}

// BenchmarkConcurrentQueries_Tree tests parallel query performance (SLOTracker's
// evaluate() method may run concurrently with other operations)
func BenchmarkConcurrentQueries_Tree(b *testing.B) {
	w := setupTreeWindow()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = w.percentile(0.99)
		}
	})
}
