// Package websocket - performance benchmarks for Hub sharding implementation.
//
// These benchmarks quantify the benefit of sharded client storage over a single
// global mutex. The old design serialized every Register/Unregister/Broadcast on
// one sync.RWMutex; the sharded design spreads clients across runtime.NumCPU
// shards (min 4) keyed by an FNV-1a hash of the client ID, so operations on
// different shards proceed in parallel.
package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// newBenchClient builds a lightweight client suitable for storage benchmarks.
// conn is intentionally nil: these benchmarks exercise shard storage and the
// broadcast fan-out only, never the underlying network connection.
func newBenchClient(id string) *Client {
	return &Client{
		id:     id,
		send:   make(chan []byte, 64),
		topics: make(map[EventType]bool),
	}
}

// setupHubClients creates n clients and inserts them directly into the hub shards.
func setupHubClients(h *Hub, n int) []*Client {
	clients := make([]*Client, n)
	for i := range clients {
		id := "bench-client-" + strconv.Itoa(i)
		client := newBenchClient(id)
		clients[i] = client
		shard := h.shardFor(id)
		shard.mu.Lock()
		shard.clients[client] = true
		shard.mu.Unlock()
	}
	return clients
}

// singleLockStore models the OLD hub storage: one mutex guarding one map.
type singleLockStore struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

func newSingleLockStore() *singleLockStore {
	return &singleLockStore{clients: make(map[string]*Client)}
}

func (s *singleLockStore) register(c *Client) {
	s.mu.Lock()
	s.clients[c.id] = c
	s.mu.Unlock()
}

// BenchmarkHub_Register_SingleLock measures concurrent registration when every
// goroutine contends on a single global mutex (the pre-sharding baseline).
func BenchmarkHub_Register_SingleLock(b *testing.B) {
	store := newSingleLockStore()
	var counter int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := strconv.FormatInt(atomic.AddInt64(&counter, 1), 10)
			store.register(newBenchClient(id))
		}
	})
}

// BenchmarkHub_Register_Sharded measures the same workload against the sharded
// ShardedHub. Because clients hash to different shards, concurrent goroutines
// mostly avoid lock contention, yielding up to ~numShards× less contention.
func BenchmarkHub_Register_Sharded(b *testing.B) {
	sh := NewShardedHub(context.Background())
	defer sh.Stop()
	var counter int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := strconv.FormatInt(atomic.AddInt64(&counter, 1), 10)
			sh.Register(newBenchClient(id))
		}
	})
}

// benchmarkBroadcast fans a single pre-encoded frame out to n connected clients
// across all shards in parallel. Client send buffers are size 64 and are not
// drained; once full, the non-blocking send falls through to the default case,
// so the benchmark measures pure fan-out/iteration cost, not consumer speed.
func benchmarkBroadcast(b *testing.B, n int) {
	h := NewHub(nil)
	setupHubClients(h, n)

	event := &Event{
		Type: EventTypeSystem,
		Data: map[string]interface{}{"msg": "broadcast test"},
	}
	data, _ := json.Marshal(event)
	frame := encodeTextFrame(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(h.numShards)
		for _, shard := range h.clients {
			go func(s *hubShard) {
				defer wg.Done()
				s.mu.RLock()
				for client := range s.clients {
					select {
					case client.send <- frame:
					default:
						// Send buffer full: skip (matches production drop policy)
					}
				}
				s.mu.RUnlock()
			}(shard)
		}
		wg.Wait()
	}
}

// BenchmarkHub_Broadcast_10K_Clients broadcasts to 10,000 connected clients.
// Target: <10ms per broadcast.
func BenchmarkHub_Broadcast_10K_Clients(b *testing.B) {
	benchmarkBroadcast(b, 10000)
}

// BenchmarkHub_Broadcast_100K_Clients broadcasts to 100,000 connected clients.
// Target: <100ms per broadcast, sustained via parallel shard iteration.
func BenchmarkHub_Broadcast_100K_Clients(b *testing.B) {
	benchmarkBroadcast(b, 100000)
}

// TestShardedHub_Integration validates the standalone ShardedHub lifecycle:
// defaults, Register, ClientCount, and Unregister.
func TestShardedHub_Integration(t *testing.T) {
	ctx := context.Background()
	sh := NewShardedHub(ctx)
	if sh == nil {
		t.Fatal("NewShardedHub should return non-nil")
	}
	if sh.numShards < 4 {
		t.Errorf("numShards should be at least 4, got %d", sh.numShards)
	}
	if sh.ClientCount() != 0 {
		t.Error("new sharded hub should have 0 clients")
	}

	// Register one client per shard using deterministic IDs.
	clients := make([]*Client, sh.numShards)
	for i := 0; i < sh.numShards; i++ {
		c := newBenchClient(fmt.Sprintf("test-%d", i))
		clients[i] = c
		sh.Register(c)
	}
	if sh.ClientCount() != sh.numShards {
		t.Errorf("expected %d clients, got %d", sh.numShards, sh.ClientCount())
	}

	// Unregister the same client pointers and expect an empty hub.
	for _, c := range clients {
		sh.Unregister(c)
	}
	if sh.ClientCount() != 0 {
		t.Errorf("expected 0 clients after unregister, got %d", sh.ClientCount())
	}
	sh.Stop()
}
