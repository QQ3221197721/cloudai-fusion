package billing_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// 2026 Competitive Baseline: Stripe Billing (2026)
//   Usage records aggregated in batch (hourly/daily). Real-time query requires
//   scanning all records in period. Latency: seconds for current-bill query.
//
// Our Innovation: Streaming meter with O(1) current-bill query via atomic counters.

type StreamingBillingMeter struct {
	mu       sync.RWMutex
	counters map[string]*atomic.Int64
}

func NewStreamingBillingMeter() *StreamingBillingMeter {
	return &StreamingBillingMeter{counters: make(map[string]*atomic.Int64, 256)}
}

func (m *StreamingBillingMeter) RecordUsage(tenantID, metric string, amount int64) {
	key := tenantID + ":" + metric
	m.mu.RLock()
	ctr, ok := m.counters[key]
	m.mu.RUnlock()
	if !ok {
		m.mu.Lock()
		ctr, ok = m.counters[key]
		if !ok {
			ctr = &atomic.Int64{}
			m.counters[key] = ctr
		}
		m.mu.Unlock()
	}
	ctr.Add(amount)
}

func (m *StreamingBillingMeter) CurrentBill(tenantID, metric string) int64 {
	key := tenantID + ":" + metric
	m.mu.RLock()
	ctr, ok := m.counters[key]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return ctr.Load()
}

func BenchmarkBilling_StreamRecord(b *testing.B) {
	meter := NewStreamingBillingMeter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		meter.RecordUsage("tenant-1", "gpu-seconds", 1)
	}
}

func BenchmarkBilling_StreamQuery(b *testing.B) {
	meter := NewStreamingBillingMeter()
	for i := 0; i < 100000; i++ {
		meter.RecordUsage("tenant-1", "gpu-seconds", 1)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		meter.CurrentBill("tenant-1", "gpu-seconds")
	}
}

func BenchmarkBilling_BatchQuery_Simulated(b *testing.B) {
	// Baseline: scan all usage records to compute current bill
	records := make([]int64, 100000)
	for i := range records {
		records[i] = 1
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var total int64
		for _, r := range records {
			total += r
		}
		_ = total
	}
}

func BenchmarkBilling_Concurrent(b *testing.B) {
	meter := NewStreamingBillingMeter()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			meter.RecordUsage(fmt.Sprintf("t-%d", i%10), "api-calls", 1)
			i++
		}
	})
}

func TestBilling_StreamVsBatch(t *testing.T) {
	meter := NewStreamingBillingMeter()
	for i := 0; i < 100000; i++ {
		meter.RecordUsage("t1", "gpu", 1)
	}
	streamResult := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			meter.CurrentBill("t1", "gpu")
		}
	})
	records := make([]int64, 100000)
	for i := range records { records[i] = 1 }
	batchResult := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var sum int64
			for _, r := range records { sum += r }
			_ = sum
		}
	})
	t.Logf("Stream O(1): %d ns/op", streamResult.NsPerOp())
	t.Logf("Batch O(N):  %d ns/op", batchResult.NsPerOp())
	t.Logf("Speedup: %.0fx", float64(batchResult.NsPerOp())/float64(streamResult.NsPerOp()))
}
