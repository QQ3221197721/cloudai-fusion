// Package billing - incremental multi-dimensional cost allocation.
//
// CostAllocator attributes usage cost across dimensions (namespace / tenant /
// GPU / resource) and — crucially — maintains those attributions INCREMENTALLY.
// Each Allocate() folds one usage event into the running per-dimension totals in
// O(1), so the current allocation is always up to date without a periodic full
// recompute over the historical dataset.
//
// This is the architectural moat versus batch cost tools: OpenCost and Kubecost
// recompute allocations on a fixed ETL/evaluation interval (minutes), whereas
// here the allocation reflects the most recent event the instant it is folded in.
package billing

import (
	"sort"
	"sync"
)

// AllocationKey identifies a cost bucket across the tracked dimensions.
type AllocationKey struct {
	Namespace string
	Tenant    string
	GPUModel  string
	Resource  string
}

// AllocationEntry is the running attribution for a single key.
type AllocationEntry struct {
	Key      AllocationKey `json:"key"`
	Quantity int64         `json:"quantity"`
	CostUSD  float64       `json:"cost_usd"`
	Events   int64         `json:"events"`
	// Share is the fraction of the grand-total cost this key represents,
	// recomputed lazily on Snapshot (not on every Allocate).
	Share float64 `json:"share"`
}

// CostAllocator maintains incremental cost attribution across dimensions.
type CostAllocator struct {
	mu        sync.RWMutex
	entries   map[AllocationKey]*AllocationEntry
	totalCost float64
	totalQty  int64
}

// NewCostAllocator creates an empty incremental cost allocator.
func NewCostAllocator() *CostAllocator {
	return &CostAllocator{entries: make(map[AllocationKey]*AllocationEntry, 256)}
}

// Allocate folds one usage event into the running attribution. This is the hot
// path: a single map lookup plus a few additions, independent of history size.
func (c *CostAllocator) Allocate(key AllocationKey, quantity int64, costUSD float64) {
	c.mu.Lock()
	e := c.entries[key]
	if e == nil {
		e = &AllocationEntry{Key: key}
		c.entries[key] = e
	}
	e.Quantity += quantity
	e.CostUSD += costUSD
	e.Events++

	c.totalQty += quantity
	c.totalCost += costUSD
	c.mu.Unlock()
}

// AllocateBatch folds a slice of events under one lock acquisition.
func (c *CostAllocator) AllocateBatch(keys []AllocationKey, quantities []int64, costs []float64) {
	c.mu.Lock()
	n := len(keys)
	if len(quantities) < n {
		n = len(quantities)
	}
	if len(costs) < n {
		n = len(costs)
	}
	for i := 0; i < n; i++ {
		e := c.entries[keys[i]]
		if e == nil {
			e = &AllocationEntry{Key: keys[i]}
			c.entries[keys[i]] = e
		}
		e.Quantity += quantities[i]
		e.CostUSD += costs[i]
		e.Events++
		c.totalQty += quantities[i]
		c.totalCost += costs[i]
	}
	c.mu.Unlock()
}

// CostFor returns the currently attributed cost for a specific key in O(1).
func (c *CostAllocator) CostFor(key AllocationKey) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if e := c.entries[key]; e != nil {
		return e.CostUSD
	}
	return 0
}

// TotalCost returns the grand-total attributed cost in O(1) (it is maintained
// incrementally rather than summed on demand).
func (c *CostAllocator) TotalCost() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalCost
}

// Snapshot returns a sorted (by cost descending) copy of all allocation entries
// with their share of total cost filled in. This is the read path and does not
// disturb the incremental state.
func (c *CostAllocator) Snapshot() []AllocationEntry {
	c.mu.RLock()
	total := c.totalCost
	out := make([]AllocationEntry, 0, len(c.entries))
	for _, e := range c.entries {
		entry := *e
		if total > 0 {
			entry.Share = entry.CostUSD / total
		}
		out = append(out, entry)
	}
	c.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].CostUSD != out[j].CostUSD {
			return out[i].CostUSD > out[j].CostUSD
		}
		return out[i].Key.Namespace < out[j].Key.Namespace
	})
	return out
}

// KeyCount returns the number of distinct allocation buckets tracked.
func (c *CostAllocator) KeyCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
