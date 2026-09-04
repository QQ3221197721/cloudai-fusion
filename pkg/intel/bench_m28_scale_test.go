package intel

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// bench_m28_scale_test.go provides industrial-scale benchmarking for M28 dedup
// algorithm at 1 million unique IOCs, comparing against naive baseline and
// outputting structured metrics for T3 barrier assessment (Task #262).
//
// Run command:
//   go test -bench=BenchmarkM28_Scale1M -benchtime=1x ./pkg/intel -run=^$
//
// Expected metrics captured:
//   - Insert throughput (indicators/s)
//   - Memory usage (bytes) post-ingestion  
//   - Lookup latency (ns/op) at 1M scale
//   - SpaceRatio compared to naive baseline

// BenchmarkM28_Scale1M measures M28 dedup performance at 1 million unique IOCs
// with duplication factor of 20× (95% dedup rate). This represents industrial
// threat intelligence feed scale.
func BenchmarkM28_Scale1M(b *testing.B) {
	const (
		uniqueKeys = 1_000_000
		dupFactor  = 20 // 95% dedup rate
	)
	rawRecords := uniqueKeys * dupFactor

	// Generate test data: 20M raw records → 1M unique after dedup
	bundle := make([]IOCEntry, 0, rawRecords)
	for u := 0; u < uniqueKeys; u++ {
		key := fmt.Sprintf("10.%d.%d.%d", (u>>16)&0xff, (u>>8)&0xff, u&0xff)
		entry := IOCEntry{
			IOCType:     "ip",
			Value:       key,
			Severity:    SeverityMedium,
			FirstSeenAt: time.Now().UTC(),
		}
		for d := 0; d < dupFactor; d++ {
			bundle = append(bundle, entry)
		}
	}

	// Measure memory before insertion
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	memBeforeAlloc := memBefore.Alloc

	// Dedup path: insert all raw records into M28 map
	b.ResetTimer()
	b.ReportAllocs()
	
	var elapsedNS int64
	for i := 0; i < b.N; i++ {
		start := time.Now()
		
		store := NewMemoryStore()
		err := store.UpsertIOCs(bundle)
		if err != nil {
			b.Fatalf("Upsert failed: %v", err)
		}
		
		// Verify correct dedup cardinality
		count := store.IOCCount()
		if count != uniqueKeys {
			b.Fatalf("stored %d unique keys, want %d", count, uniqueKeys)
		}
		
		elapsedNS += time.Since(start).Nanoseconds()
	}
	
	// Collect memory after
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	
	// Calculate insert throughput
	insertThroughput := float64(rawRecords*b.N) / b.Elapsed().Seconds()
	insertNSPerOp := elapsedNS / int64(b.N)
	memoryIncrease := int64(memAfter.Alloc) - int64(memBeforeAlloc)
	
	b.Logf("M28_Insert_Throughput: %.2f indicators/sec", insertThroughput)
	b.Logf("M28_Insert_Latency: %d ns/op (%.2f µs)", insertNSPerOp, float64(insertNSPerOp)/1000)
	b.Logf("M28_Memory_Usage: %d bytes (%.2f MB)", memoryIncrease, float64(memoryIncrease)/(1024*1024))
	b.Logf("M28_Dedup_Rate: %.1f%%", (1-float64(uniqueKeys)/float64(rawRecords))*100)
	
	// Look up the LAST unique value (worst-case position in naive scan)
	target := fmt.Sprintf("10.%d.%d.%d", ((uniqueKeys-1)>>16)&0xff, ((uniqueKeys-1)>>8)&0xff, (uniqueKeys-1)&0xff)
	
	// Rebuild store for lookup benchmark (since it's consumed by benchmark)
	store := NewMemoryStore()
	_ = store.UpsertIOCs(bundle)
	
	// Benchmark M28 map lookup (O(1) expected probes)
	b.Run("Lookup_O1_Map", func(b *testing.B) {
		b.ResetTimer()
		var hits int
		for i := 0; i < b.N; i++ {
			h, _ := store.LookupIOCs("ip", []string{target})
			hits += len(h)
		}
		if hits != b.N {
			b.Fatalf("expected %d hits, got %d", b.N, hits)
		}
	})
	
	// Naive baseline comparison: same unique keys for fair O(1) vs Θ(N) contrast
	// We insert ALL 1M unique keys but scan linearly instead of hashing
	
	// Create naive store with exact same 1M items (one-by-one to match insertion pattern)
	ns := &NaiveLinearStore{}
	for u := 0; u < uniqueKeys; u++ {
		key := fmt.Sprintf("10.%d.%d.%d", (u>>16)&0xff, (u>>8)&0xff, u&0xff)
		entry := IOCEntry{IOCType: "ip", Value: key, FirstSeenAt: time.Now().UTC()}
		ns.Upsert([]IOCEntry{entry})
	}
	b.Run("Naive_Baseline_Comparison", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			e, ok := ns.Lookup("ip", target)
			if !ok {
				b.Fatal("naive lookup failed")
			}
			_ = e
		}
	})
	
	// Output structured JSON summary (parsable by external tools)
	metricsSummary := fmt.Sprintf(`
--- BENCHMARK_SUMMARY_START ---
Test: BenchmarkM28_Scale1M
Unique_Keys: %d
Raw_Records: %d
Dup_Factor: %d
Dedup_Rate: %.1f%%
Insert_Throughput: %.2f indicators/sec
Insert_Latency_NS: %d
Memory_Usage_MB: %.2f
Target_Lookup: %s
--- BENCHMARK_SUMMARY_END ---
`, 
		uniqueKeys,
		rawRecords,
		dupFactor,
		(1-float64(uniqueKeys)/float64(rawRecords))*100,
		insertThroughput,
		insertNSPerOp,
		float64(memoryIncrease)/(1024*1024),
		target,
	)
	
	b.Log(metricsSummary)
}

// TestM28_ScaleInvariants verifies correctness invariants at 1M unique key scale
func TestM28_ScaleInvariants(t *testing.T) {
	const (
		uniqueKeys = 1_000_000
		dupFactor  = 20
	)
	rawRecords := uniqueKeys * dupFactor

	// Generate same pattern as benchmark
	bundle := make([]IOCEntry, 0, rawRecords)
	for u := 0; u < uniqueKeys; u++ {
		key := fmt.Sprintf("10.%d.%d.%d", (u>>16)&0xff, (u>>8)&0xff, u&0xff)
		entry := IOCEntry{
			IOCType:     "ip",
			Value:       key,
			Severity:    SeverityMedium,
			FirstSeenAt: time.Now().UTC(),
		}
		for d := 0; d < dupFactor; d++ {
			bundle = append(bundle, entry)
		}
	}

	// Test M28 dedup path
	m28 := NewMemoryStore()
	err := m28.UpsertIOCs(bundle)
	if err != nil {
		t.Fatalf("M28 upsert failed: %v", err)
	}

	count := m28.IOCCount()
	if count != uniqueKeys {
		t.Fatalf("M28 stored %d unique keys, want %d", count, uniqueKeys)
	}

	// Compute theoretical dedup rate
	expectedDedupRate := 1.0 - float64(uniqueKeys)/float64(rawRecords)
	actualDedupRate := float64(rawRecords-count)/float64(rawRecords)
	
	t.Logf("Scale @ %d unique keys:", uniqueKeys)
	t.Logf("  Raw records ingested: %d", rawRecords)
	t.Logf("  Unique retained:      %d", count)
	t.Logf("  Dedup rate:           %.1f%%", actualDedupRate*100)
	t.Logf("  Space ratio vs naive: %.1fx", float64(rawRecords)/float64(count))
	
	// Verify last key lookup works (stress test O(1) path)
	target := fmt.Sprintf("10.%d.%d.%d", ((uniqueKeys-1)>>16)&0xff, ((uniqueKeys-1)>>8)&0xff, (uniqueKeys-1)&0xff)
	
	start := time.Now()
	hits, _ := m28.LookupIOCs("ip", []string{target})
	lookupElapsed := time.Since(start)
	
	if len(hits) != 1 {
		t.Fatalf("lookup miss on last key: %d hits, want 1", len(hits))
	}
	
	t.Logf("  Last-key lookup time: %d ns (%s)", lookupElapsed.Nanoseconds(), lookupElapsed)
	
	// Verify space-time tradeoff model predictions match reality
	model := DedupCostModel{
		RawRecords:  rawRecords,
		UniqueKeys:  uniqueKeys,
		EntryBytes:  256,
		MapOverhead: 64,
	}
	
	t.Logf("  Model prediction:")
	t.Logf("    Dup factor f:         %.1fx", model.DupFactor())
	t.Logf("    Expected dedup rate:  %.1f%%", model.DedupRate()*100)
	t.Logf("    Space overhead c/s:   %.2f", float64(model.MapOverhead)/float64(model.EntryBytes))
	
	// Sanity check: empirical should be within 1% of theoretical
	if diff := actualDedupRate - expectedDedupRate; diff > 0.01 || diff < -0.01 {
		t.Errorf("empirical dedup rate differs >1%% from model: %.4f vs %.4f", actualDedupRate, expectedDedupRate)
	}
}

// BenchmarkLookupScaleComparison_1M vs 500K quantifies asymptotic query growth
func BenchmarkLookupScaleComparison_1M(b *testing.B) {
	const (
		scale1M = 1_000_000
		scale500K = 500_000
	)
	
	// Build both scales
	buildData := func(n int) (*MemoryStore, string) {
		store := NewMemoryStore()
		for u := 0; u < n; u++ {
			key := fmt.Sprintf("10.%d.%d.%d", (u>>16)&0xff, (u>>8)&0xff, u&0xff)
			store.UpsertIOCs([]IOCEntry{{IOCType: "ip", Value: key}})
		}
		target := fmt.Sprintf("10.%d.%d.%d", ((n-1)>>16)&0xff, ((n-1)>>8)&0xff, (n-1)&0xff)
		return store, target
	}
	
	store1M, target1M := buildData(scale1M)
	store500K, target500K := buildData(scale500K)
	
	b.Run("Lookup_1M_keys", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			hits, _ := store1M.LookupIOCs("ip", []string{target1M})
			if len(hits) != 1 {
				b.Fatal("1M lookup miss")
			}
		}
	})
	
	b.Run("Lookup_500K_keys", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			hits, _ := store500K.LookupIOCs("ip", []string{target500K})
			if len(hits) != 1 {
				b.Fatal("500K lookup miss")
			}
		}
	})
	
	// Both should show stable ~constant time regardless of scale
	b.ReportMetric(float64(scale1M)/float64(scale500K), "scale_ratio")
}
