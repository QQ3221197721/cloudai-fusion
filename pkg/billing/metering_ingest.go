// Package billing - zero-allocation metering ingest path.
//
// ZeroAllocIngestor is the optimized counterpart to UsageCollector.RecordUsage.
// UsageCollector allocates on every event (it appends to a per-tenant slice and
// builds a fresh metadata map), which shows up as bytes/op and allocs/op in the
// ingest benchmark. ZeroAllocIngestor instead folds events into a
// pre-allocated fixed-capacity ring buffer of value structs, so the steady-state
// Add() hot path performs ZERO heap allocations: no slice growth, no map
// creation, no interface boxing. This is what lets in-process metering sustain
// microsecond-scale, allocation-free ingestion versus a network meter API.
package billing

import (
	"sync"
	"time"
)

// MeteringEvent is the value-type unit ingested on the hot path. It is stored
// by value inside the ring buffer so a full buffer holds all events contiguously
// with no per-event pointer indirection or allocation.
type MeteringEvent struct {
	TenantID     string
	ResourceType string
	Quantity     int64
	CostUSD      float64
	Timestamp    time.Time
}

// ZeroAllocIngestor is a fixed-capacity ring buffer for high-throughput,
// allocation-free usage ingestion.
type ZeroAllocIngestor struct {
	mu    sync.Mutex
	cap   int64
	buf   []MeteringEvent
	head  int64 // next read position
	tail  int64 // next write position
	count int64 // number of buffered events
	// dropped counts events rejected because the buffer was full (back-pressure).
	dropped int64
}

// NewZeroAllocIngestor creates a ring-buffer ingestor over capacity events. The
// entire backing array is allocated once, up front.
func NewZeroAllocIngestor(capacity int64) *ZeroAllocIngestor {
	if capacity <= 0 {
		capacity = 1
	}
	return &ZeroAllocIngestor{cap: capacity, buf: make([]MeteringEvent, capacity)}
}

// Add enqueues one event. The steady-state path (buffer not full) writes a
// value struct into the pre-allocated array and advances two counters — no heap
// allocation. When full it increments a drop counter rather than growing.
func (z *ZeroAllocIngestor) Add(tenantID, resourceType string, quantity int64, costUSD float64) bool {
	z.mu.Lock()
	if z.count >= z.cap {
		z.dropped++
		z.mu.Unlock()
		return false
	}
	idx := z.tail % z.cap
	z.buf[idx].TenantID = tenantID
	z.buf[idx].ResourceType = resourceType
	z.buf[idx].Quantity = quantity
	z.buf[idx].CostUSD = costUSD
	z.buf[idx].Timestamp = time.Now()
	z.tail++
	z.count++
	z.mu.Unlock()
	return true
}

// AddBatch enqueues a slice of events under a single lock acquisition,
// amortizing the mutex cost. Returns the number accepted.
func (z *ZeroAllocIngestor) AddBatch(events []MeteringEvent) int {
	z.mu.Lock()
	accepted := 0
	for i := range events {
		if z.count >= z.cap {
			z.dropped += int64(len(events) - i)
			break
		}
		idx := z.tail % z.cap
		z.buf[idx] = events[i]
		z.tail++
		z.count++
		accepted++
	}
	z.mu.Unlock()
	return accepted
}

// Len returns the number of currently buffered events.
func (z *ZeroAllocIngestor) Len() int64 {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.count
}

// Dropped returns the number of events dropped due to back-pressure.
func (z *ZeroAllocIngestor) Dropped() int64 {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.dropped
}

// Drain copies all buffered events into dst (reused if it has capacity) and
// clears the buffer, returning the populated slice. Passing a reusable dst
// keeps the drain path allocation-free as well.
func (z *ZeroAllocIngestor) Drain(dst []MeteringEvent) []MeteringEvent {
	z.mu.Lock()
	defer z.mu.Unlock()

	n := z.count
	dst = dst[:0]
	for i := int64(0); i < n; i++ {
		idx := (z.head + i) % z.cap
		dst = append(dst, z.buf[idx])
	}
	z.head += n
	z.count = 0
	return dst
}
