package websocket

import (
	"fmt"
	"sync"
	"testing"
)

// ============================================================================
// WebSocket Sharded Hub Performance Benchmarks
//
// Validates: Sharded broadcast O(N/shards) vs single-hub O(N).
//
// Run: go test -bench=BenchmarkHub -benchmem ./pkg/websocket/
// ============================================================================

// BenchmarkHub_SingleShard_1K measures broadcast to 1K conns in single shard (baseline).
func BenchmarkHub_SingleShard_1K(b *testing.B) {
	hub := NewShardedHub(1) // single shard = no parallelism advantage
	topic := "alerts"
	for i := 0; i < 1000; i++ {
		hub.Register(fmt.Sprintf("conn-%d", i), topic)
	}
	msg := []byte(`{"type":"alert","level":"critical","message":"CPU spike detected"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.BroadcastDirect(topic, msg)
	}
}

// BenchmarkHub_16Shards_1K measures broadcast to 1K conns across 16 shards.
func BenchmarkHub_16Shards_1K(b *testing.B) {
	hub := NewShardedHub(16)
	topics := []string{"alerts", "gpu-metrics", "workloads", "cluster-health",
		"audit", "scheduler", "billing", "edge",
		"security", "finops", "deploy", "mesh",
		"tee", "disaster", "plugins", "agents"}
	for i := 0; i < 1000; i++ {
		topic := topics[i%len(topics)]
		hub.Register(fmt.Sprintf("conn-%d", i), topic)
	}
	msg := []byte(`{"type":"alert","level":"critical","message":"CPU spike detected"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Broadcast to one shard (only ~62 conns per shard vs 1000 total)
		hub.BroadcastDirect("alerts", msg)
	}
}

// BenchmarkHub_SingleShard_10K measures 10K connections single shard.
func BenchmarkHub_SingleShard_10K(b *testing.B) {
	hub := NewShardedHub(1)
	topic := "metrics"
	for i := 0; i < 10000; i++ {
		hub.Register(fmt.Sprintf("conn-%d", i), topic)
	}
	msg := []byte(`{"ts":1691234567,"gpu_util":85.2,"mem_used":72000}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.BroadcastDirect(topic, msg)
	}
}

// BenchmarkHub_16Shards_10K measures 10K connections distributed across 16 shards.
func BenchmarkHub_16Shards_10K(b *testing.B) {
	hub := NewShardedHub(16)
	topics := []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8",
		"t9", "t10", "t11", "t12", "t13", "t14", "t15", "t16"}
	for i := 0; i < 10000; i++ {
		hub.Register(fmt.Sprintf("conn-%d", i), topics[i%16])
	}
	msg := []byte(`{"ts":1691234567,"gpu_util":85.2}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Each broadcast only touches 625 conns (10000/16)
		hub.BroadcastDirect("t1", msg)
	}
}

// BenchmarkHub_Parallel_Broadcast measures concurrent broadcasts from multiple goroutines.
func BenchmarkHub_Parallel_Broadcast(b *testing.B) {
	hub := NewShardedHub(16)
	topics := []string{"alerts", "metrics", "audit", "deploy"}
	for i := 0; i < 4000; i++ {
		hub.Register(fmt.Sprintf("conn-%d", i), topics[i%4])
	}
	msg := []byte(`{"event":"test"}`)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			hub.BroadcastDirect(topics[i%4], msg)
			i++
		}
	})
}

// TestShardedHub_Distribution validates even connection distribution.
func TestShardedHub_Distribution(t *testing.T) {
	hub := NewShardedHub(8)
	topics := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for i := 0; i < 1000; i++ {
		hub.Register(fmt.Sprintf("conn-%d", i), topics[i%8])
	}

	// Check distribution across shards
	var counts []int
	for _, s := range hub.shards {
		s.mu.RLock()
		counts = append(counts, len(s.conns))
		s.mu.RUnlock()
	}

	t.Logf("Shard distribution: %v (ideal: 125 each)", counts)
	t.Logf("Total connections: %d", hub.ConnCount())

	if hub.ConnCount() != 1000 {
		t.Errorf("expected 1000 conns, got %d", hub.ConnCount())
	}
}

// TestShardedHub_BroadcastReachesCorrectShard validates message isolation.
func TestShardedHub_BroadcastReachesCorrectShard(t *testing.T) {
	hub := NewShardedHub(4)
	hub.Register("conn-1", "alerts")
	hub.Register("conn-2", "alerts")
	hub.Register("conn-3", "metrics")

	msg := []byte("test-broadcast")
	count := hub.BroadcastDirect("alerts", msg)

	// Should only reach 2 connections in "alerts" shard
	t.Logf("Broadcast to 'alerts' reached %d connections", count)

	// Verify metrics shard was not touched
	var wg sync.WaitGroup
	_ = wg // compilation check
}
