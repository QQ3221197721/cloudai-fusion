package eventbus

import (
	"sync"
	"testing"
)

// 2026 Competitive Baseline: NATS pub/sub (single message per publish call)
// Our Innovation: Microbatch + buffer pool (amortize network round-trip over N messages).

type MicrobatchPublisher struct {
	mu       sync.Mutex
	buffer   [][]byte
	batchMax int
	flushes  int
}

func NewMicrobatchPublisher(batchSize int) *MicrobatchPublisher {
	return &MicrobatchPublisher{buffer: make([][]byte, 0, batchSize), batchMax: batchSize}
}

func (mp *MicrobatchPublisher) Publish(msg []byte) {
	mp.mu.Lock()
	mp.buffer = append(mp.buffer, msg)
	if len(mp.buffer) >= mp.batchMax {
		mp.flush()
	}
	mp.mu.Unlock()
}

func (mp *MicrobatchPublisher) flush() {
	// Simulate: single network write of all buffered messages
	_ = mp.buffer
	mp.buffer = mp.buffer[:0]
	mp.flushes++
}

func (mp *MicrobatchPublisher) Flush() {
	mp.mu.Lock()
	if len(mp.buffer) > 0 {
		mp.flush()
	}
	mp.mu.Unlock()
}

// BenchmarkEvent_SinglePublish measures one-by-one publish (NATS baseline).
func BenchmarkEvent_SinglePublish(b *testing.B) {
	msg := []byte(`{"event":"workload.created","ts":1691234567}`)
	// Simulate: each publish = one "network call" (just a slice append here)
	sink := make([][]byte, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = append(sink, msg)
	}
	_ = sink
}

// BenchmarkEvent_Microbatch measures batched publish (our approach).
func BenchmarkEvent_Microbatch(b *testing.B) {
	pub := NewMicrobatchPublisher(64)
	msg := []byte(`{"event":"workload.created","ts":1691234567}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pub.Publish(msg)
	}
	pub.Flush()
}

// BenchmarkEvent_Concurrent measures concurrent publishing with batching.
func BenchmarkEvent_Concurrent(b *testing.B) {
	pub := NewMicrobatchPublisher(128)
	msg := []byte(`{"event":"test"}`)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pub.Publish(msg)
		}
	})
}

func TestEvent_BatchReducesFlushes(t *testing.T) {
	pub := NewMicrobatchPublisher(100)
	for i := 0; i < 10000; i++ {
		pub.Publish([]byte("msg"))
	}
	pub.Flush()
	t.Logf("10000 messages → %d flushes (batch size 100)", pub.flushes)
	if pub.flushes > 110 {
		t.Errorf("expected ~100 flushes, got %d", pub.flushes)
	}
}
