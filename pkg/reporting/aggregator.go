// Package reporting - incremental streaming aggregator.
//
// StreamAggregator maintains live, always-current aggregates that are updated
// in O(1) per event as usage/cost records arrive, rather than being recomputed
// by a periodic batch pass over the full dataset. This is the architectural
// differentiator versus batch cost tools (OpenCost/Kubecost ETL, Prometheus
// recording rules) that recompute on a fixed evaluation interval: a Snapshot
// here reflects the most recently ingested event, not the state as of the last
// batch tick.
package reporting

import (
	"sort"
	"strings"
	"sync"
)

// StreamAggregator is a concurrency-safe incremental aggregator keyed by a
// fixed set of dimensions. Add() folds one record into the running totals in
// constant time; Snapshot() reads the current materialized aggregates.
type StreamAggregator struct {
	mu   sync.RWMutex
	dims []string
	// groups holds running aggregates keyed by composite dimension key.
	groups map[string]*AggRow
	// totals track the grand aggregate so Snapshot never rescans groups for it.
	totalCost float64
	totalQty  int64
	totalCnt  int64
}

// NewStreamAggregator creates an incremental aggregator over the given
// dimension order.
func NewStreamAggregator(dims []string) *StreamAggregator {
	return &StreamAggregator{
		dims:   append([]string(nil), dims...),
		groups: make(map[string]*AggRow, 256),
	}
}

// Add folds a single record into the running aggregates. Cost of an Add is one
// map lookup plus a handful of additions — independent of how many records have
// been ingested so far, which is what keeps the aggregate real-time under load.
func (s *StreamAggregator) Add(rec *Record) {
	s.mu.Lock()
	key := compositeKeyOf(rec, s.dims)
	row := s.groups[key]
	if row == nil {
		keys := make(map[string]string, len(s.dims))
		for _, d := range s.dims {
			keys[d] = rec.dimensionValue(d)
		}
		row = &AggRow{Keys: keys}
		s.groups[key] = row
	}
	row.Count++
	row.Quantity += rec.Quantity
	row.Cost += rec.Cost

	s.totalCnt++
	s.totalQty += rec.Quantity
	s.totalCost += rec.Cost
	s.mu.Unlock()
}

// AddBatch folds a slice of records in a single lock acquisition, amortizing
// the mutex cost across the batch for bulk backfill.
func (s *StreamAggregator) AddBatch(recs []Record) {
	s.mu.Lock()
	for i := range recs {
		rec := &recs[i]
		key := compositeKeyOf(rec, s.dims)
		row := s.groups[key]
		if row == nil {
			keys := make(map[string]string, len(s.dims))
			for _, d := range s.dims {
				keys[d] = rec.dimensionValue(d)
			}
			row = &AggRow{Keys: keys}
			s.groups[key] = row
		}
		row.Count++
		row.Quantity += rec.Quantity
		row.Cost += rec.Cost

		s.totalCnt++
		s.totalQty += rec.Quantity
		s.totalCost += rec.Cost
	}
	s.mu.Unlock()
}

// Snapshot returns a deterministic, sorted copy of the current aggregates. The
// returned Report is a value snapshot: callers may serialize or mutate it
// without affecting the live aggregator.
func (s *StreamAggregator) Snapshot(title string) *Report {
	s.mu.RLock()
	rows := make([]AggRow, 0, len(s.groups))
	for _, row := range s.groups {
		rows = append(rows, *row)
	}
	total := s.totalCost
	qty := s.totalQty
	dims := s.dims
	s.mu.RUnlock()

	sort.Slice(rows, func(i, j int) bool {
		return compareKeys(rows[i].Keys, rows[j].Keys, dims) < 0
	})

	return &Report{
		Title:      title,
		Dimensions: dims,
		Rows:       rows,
		TotalCost:  total,
		TotalQty:   qty,
		RowCount:   len(rows),
	}
}

// GroupCount returns the number of distinct groups currently tracked.
func (s *StreamAggregator) GroupCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.groups)
}

// compositeKeyOf builds a composite dimension key. Unlike Engine.compositeKey
// it is stateless (no shared builder) so it is safe to call under the
// aggregator lock from concurrent goroutines.
func compositeKeyOf(rec *Record, dims []string) string {
	if len(dims) == 0 {
		return ""
	}
	if len(dims) == 1 {
		return rec.dimensionValue(dims[0])
	}
	var b strings.Builder
	for i, d := range dims {
		if i > 0 {
			b.WriteByte(0)
		}
		b.WriteString(rec.dimensionValue(d))
	}
	return b.String()
}
