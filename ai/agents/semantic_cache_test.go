package agents

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// ============================================================================
// AI Agent Semantic Cache Performance Benchmarks
//
// 2026 Competitive Baseline: LangChain + GPTCache
//   - Every cache lookup requires embedding API call: ~100-500ms
//   - Even on "hit", total latency = embedding_time + vector_search = 150-600ms
//   - Full LLM call: 1000-3000ms
//
// Our Two-Level Cache:
//   - L1 exact hash: ~30ns (no embedding call needed)
//   - L2 semantic: ~100ms (embedding + cosine search)
//   - Miss (full LLM): ~2000ms
//
// Run: go test -bench=BenchmarkAgent -benchmem ./ai/agents/
// ============================================================================

func randomEmbedding(dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rand.Float32()*2 - 1
	}
	return v
}

// BenchmarkAgent_L1Hit measures exact hash cache hit (the fast path).
// This is our key advantage over GPTCache: zero embedding computation.
func BenchmarkAgent_L1Hit(b *testing.B) {
	cache := NewSemanticCache(10000, 1000, 0.95, 30*time.Minute)

	// Pre-populate with a known query
	query := "What is the GPU utilization of node-3?"
	cache.Store(query, "Current GPU utilization of node-3 is 78%.", randomEmbedding(384))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.LookupL1(query)
	}
}

// BenchmarkAgent_L1Hit_Variant measures L1 hit with minor whitespace variants.
// Tests that normalization handles common variations.
func BenchmarkAgent_L1Hit_Variant(b *testing.B) {
	cache := NewSemanticCache(10000, 1000, 0.95, 30*time.Minute)

	cache.Store("what is the gpu utilization of node-3?",
		"Current GPU utilization of node-3 is 78%.", randomEmbedding(384))

	// Same semantic query with different casing/spacing
	variants := []string{
		"What is the GPU utilization of node-3?",
		"WHAT IS THE GPU UTILIZATION OF NODE-3?",
		"  what  is  the  gpu  utilization  of  node-3?  ",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.LookupL1(variants[i%len(variants)])
	}
}

// BenchmarkAgent_L2Hit measures semantic similarity lookup.
// Simulates: embedding already computed, search N cached vectors.
func BenchmarkAgent_L2Hit(b *testing.B) {
	cache := NewSemanticCache(10000, 1000, 0.95, 30*time.Minute)

	// Populate with 100 cached embeddings
	for i := 0; i < 100; i++ {
		emb := randomEmbedding(384)
		cache.Store(fmt.Sprintf("query-%d", i),
			fmt.Sprintf("response-%d", i), emb)
	}

	// Create a query embedding similar to one of the cached ones
	// (just slightly perturbed version of the first cached embedding)
	cache.mu.RLock()
	targetEmb := make([]float32, 384)
	if len(cache.embeddings) > 0 {
		copy(targetEmb, cache.embeddings[0].vector)
		// Add tiny noise to make it slightly different
		for i := range targetEmb {
			targetEmb[i] += 0.001 * (rand.Float32() - 0.5)
		}
	}
	cache.mu.RUnlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.LookupL2(targetEmb)
	}
}

// BenchmarkAgent_L2_Search_1000 measures vector search with 1000 cached entries.
// This tests scaling of brute-force cosine similarity.
func BenchmarkAgent_L2_Search_1000(b *testing.B) {
	cache := NewSemanticCache(10000, 1000, 0.95, 30*time.Minute)

	for i := 0; i < 1000; i++ {
		cache.Store(fmt.Sprintf("query-%d", i), "resp", randomEmbedding(384))
	}

	queryEmb := randomEmbedding(384)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.LookupL2(queryEmb)
	}
}

// BenchmarkAgent_CosineSimilarity measures raw cosine computation (384 dims).
func BenchmarkAgent_CosineSimilarity(b *testing.B) {
	a := randomEmbedding(384)
	vec := randomEmbedding(384)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cosineSimilarity(a, vec)
	}
}

// BenchmarkAgent_LLMCall_Simulated simulates full LLM call latency (baseline).
func BenchmarkAgent_LLMCall_Simulated(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate: 2000ms LLM inference (we just sleep 1ms as proxy)
		time.Sleep(1 * time.Millisecond)
	}
}

// TestAgent_SemanticCache_E2E validates full cache flow.
func TestAgent_SemanticCache_E2E(t *testing.T) {
	cache := NewSemanticCache(1000, 100, 0.95, 30*time.Minute)

	// Simulate real usage pattern
	queries := []string{
		"What is the GPU utilization?",
		"How much memory is free on node-1?",
		"Show me recent alerts",
		"What is the GPU utilization?", // repeat
		"what is the gpu utilization?", // variant
		"  WHAT IS THE GPU UTILIZATION?  ", // another variant
	}

	// First pass: all miss, store results
	for i, q := range queries[:3] {
		_, hit := cache.LookupL1(q)
		if hit {
			t.Errorf("query %d should miss on first call", i)
		}
		cache.Store(q, fmt.Sprintf("answer-%d", i), randomEmbedding(384))
	}

	// Second pass: should hit L1 for exact and normalized matches
	for _, q := range queries[3:] {
		_, hit := cache.LookupL1(q)
		if !hit {
			t.Errorf("query %q should hit L1 cache (normalized)", q)
		}
	}

	stats := cache.Stats()
	t.Logf("L1 Hits: %d, L2 Hits: %d, Misses: %d", stats.L1Hits, stats.L2Hits, stats.Misses)
	t.Logf("L1 Rate: %.1f%%, Miss Rate: %.1f%%", stats.L1Rate*100, stats.MissRate*100)

	if stats.L1Rate < 0.3 {
		t.Errorf("expected >30%% L1 hit rate, got %.1f%%", stats.L1Rate*100)
	}
}
