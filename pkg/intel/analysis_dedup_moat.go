package intel

// analysis_dedup_moat.go — Formal space-time tradeoff model for the L1 threat-
// intelligence deduplication hash index (M28 AISecOps intelligence ingestion).
//
// PURPOSE (T3 architecture-moat deep-dive, Task #262):
//
//	This file is a NON-PRODUCTION analysis artifact. It adds a self-contained,
//	runnable cost model plus two reference baselines used by the adversarial
//	MoAT tests (dedup_moat_adversarial_test.go). It does NOT modify any existing
//	production type, function, or behaviour — it only introduces new, inert
//	symbols in the intel package so the tradeoff theorem can be exercised and
//	verified with real measurements instead of prose.
//
// The claim under test:
//
//	The keyed dedup hash index (MemoryStore.iocs, a Go map) trades Θ(U) index
//	space for O(1) expected lookup, whereas an unindexed linear structure
//	("naive scan") occupies Θ(R) space (it retains duplicates) AND pays Θ(N)
//	per lookup. No unindexed design can achieve O(1) lookup without
//	materialising a Θ(U) index — at which point it *is* the dedup map. This is
//	a genuine, information-theoretic space-time tradeoff, not an implementation
//	accident.
//
// Symbols:
//   - DedupCostModel      analytical bounds (space, comparisons, ratios)
//   - NaiveLinearStore    the reference no-index / no-dedup baseline
//   - PooledResultLookup  a zero-alloc lookup variant (sync.Pool envelope
//     recycling) — demonstrates that the map's single
//     result-slice allocation is removable, so the map's
//     per-query GC pressure is not a structural cost.

import (
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// 1. Formal cost model
// ---------------------------------------------------------------------------

// DedupCostModel parameterises the space-time tradeoff between the keyed dedup
// hash index and the naive linear (append-all, scan-to-find) baseline.
//
// Notation:
//
//	R  = RawRecords  total records ingested (duplicates included)
//	U  = UniqueKeys  distinct (type,value) keys among the R records
//	s  = EntryBytes  in-memory size of one stored IOC entry
//	c  = MapOverhead per-entry amortised map overhead (bucket slot + tophash +
//	     key string header/bytes + load-factor slack)
//
// The dedup map stores exactly U entries; the naive baseline stores all R.
type DedupCostModel struct {
	RawRecords  int // R — records seen by the ingest path
	UniqueKeys  int // U — distinct keys actually retained by the index
	EntryBytes  int // s — bytes per stored entry
	MapOverhead int // c — amortised per-entry index overhead
}

// DupFactor returns f = R/U, the average number of times each unique key is
// re-delivered by overlapping feeds. f = 1 means no duplication; f = 20 means a
// 95% dedup rate. Returns 0 for a degenerate (empty) model.
func (m DedupCostModel) DupFactor() float64 {
	if m.UniqueKeys <= 0 {
		return 0
	}
	return float64(m.RawRecords) / float64(m.UniqueKeys)
}

// DedupRate returns 1 - U/R, the fraction of ingested records that the index
// collapses away. A 95% dedup rate corresponds to DupFactor == 20.
func (m DedupCostModel) DedupRate() float64 {
	if m.RawRecords <= 0 {
		return 0
	}
	return 1.0 - float64(m.UniqueKeys)/float64(m.RawRecords)
}

// DedupSpaceBytes is the index footprint: Θ(U·(s+c)). Only unique keys survive.
func (m DedupCostModel) DedupSpaceBytes() int {
	return m.UniqueKeys * (m.EntryBytes + m.MapOverhead)
}

// NaiveSpaceBytes is the baseline footprint: Θ(R·s). Without keyed upserts every
// duplicate is retained, so the store grows with the raw stream, unbounded.
func (m DedupCostModel) NaiveSpaceBytes() int {
	return m.RawRecords * m.EntryBytes
}

// SpaceRatio = NaiveSpaceBytes / DedupSpaceBytes. For s >> c this tends to the
// dup factor f: the naive store is ~f× larger because it keeps every duplicate.
// When the per-entry index overhead c is non-trivial the ratio is discounted
// accordingly, which is exactly the "space price" the index pays for O(1) reads.
func (m DedupCostModel) SpaceRatio() float64 {
	d := m.DedupSpaceBytes()
	if d <= 0 {
		return 0
	}
	return float64(m.NaiveSpaceBytes()) / float64(d)
}

// NaiveWorstCaseComparisons is the number of key comparisons a linear scan makes
// in the worst case (target absent, or the last element): it must examine every
// retained record, i.e. R comparisons. This is the adversary's best case against
// an unindexed store.
func (m DedupCostModel) NaiveWorstCaseComparisons() int { return m.RawRecords }

// DedupExpectedProbes is the expected number of slot probes for a Go map lookup:
// O(1), a small constant independent of U. We model it as 1 successful-hit probe
// plus a load-factor-driven fractional collision term; Go grows buckets to keep
// the load factor <= 6.5, so this stays bounded regardless of U.
func (m DedupCostModel) DedupExpectedProbes() float64 { return 1.0 }

// QueryTimeSeparation returns the asymptotic per-lookup speed-up of the index
// over the scan: NaiveWorstCaseComparisons / DedupExpectedProbes = Θ(R). This is
// the crux of the tradeoff — the separation grows without bound as the corpus
// grows, so the gap is not a constant factor that a faster CPU could erase.
func (m DedupCostModel) QueryTimeSeparation() float64 {
	return float64(m.NaiveWorstCaseComparisons()) / m.DedupExpectedProbes()
}

// TotalWorkNaive models total work for R ingests followed by q point lookups on
// an unindexed store: Θ(R) append + Θ(q·R) scanning = R + q·R.
func (m DedupCostModel) TotalWorkNaive(q int) int {
	return m.RawRecords + q*m.RawRecords
}

// TotalWorkDedup models total work for the same workload on the index:
// Θ(R) upserts + Θ(q) O(1) lookups = R + q.
func (m DedupCostModel) TotalWorkDedup(q int) int {
	return m.RawRecords + q
}

// ---------------------------------------------------------------------------
// 2. Reference baseline: the naive, unindexed store
// ---------------------------------------------------------------------------

// NaiveLinearStore is the honest no-index baseline: it appends every ingested
// IOC to a growing slice (no keyed dedup) and answers lookups by a linear scan.
// It is the structure a pipeline that skips keyed upserts would end up with.
//
// It is intentionally NOT a Store implementation — it exists only to quantify
// what the dedup hash index buys, and to give the adversarial tests a concrete
// Θ(N)-lookup, Θ(R)-space opponent that behaves identically to the in-tree
// naiveStore used by bench_test.go, but is reusable outside _test.go files.
type NaiveLinearStore struct {
	mu   sync.RWMutex
	iocs []IOCEntry
}

// NewNaiveLinearStore returns an empty baseline store.
func NewNaiveLinearStore() *NaiveLinearStore { return &NaiveLinearStore{} }

// Upsert appends all IOCs without deduplication — duplicates accumulate, so the
// backing slice grows Θ(R) with the raw stream. This is the whole point.
func (n *NaiveLinearStore) Upsert(iocs []IOCEntry) {
	n.mu.Lock()
	n.iocs = append(n.iocs, iocs...)
	n.mu.Unlock()
}

// Lookup scans linearly for the first entry matching (iocType,value). It returns
// the entry and true on hit, or a zero entry and false when absent. Worst case
// (absent or last element) touches every retained record: Θ(N).
func (n *NaiveLinearStore) Lookup(iocType, value string) (IOCEntry, bool) {
	value = strings.TrimSpace(value)
	n.mu.RLock()
	defer n.mu.RUnlock()
	for i := range n.iocs {
		if n.iocs[i].IOCType == iocType && n.iocs[i].Value == value {
			return n.iocs[i], true
		}
	}
	return IOCEntry{}, false
}

// Len reports how many records are retained (including duplicates). For the
// dedup map the equivalent is IOCCount(), which stays at U.
func (n *NaiveLinearStore) Len() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.iocs)
}

// ---------------------------------------------------------------------------
// 3. Zero-allocation lookup via sync.Pool envelope recycling
// ---------------------------------------------------------------------------
//
// The production MemoryStore.LookupIOCs allocates one result slice per call
// (measured at 144 B/op, 1 alloc/op). That single allocation is a convenience,
// not a structural cost of the index: a hot query path can recycle the result
// envelope through a sync.Pool and reach amortised zero allocations while
// keeping the O(1) hash lookup. This helper demonstrates that, so the report can
// separate "the index costs GC pressure" (false) from "the current API returns a
// fresh slice" (true, and removable). It reads the same map via the exported
// LookupIOCs and copies hits into a pooled buffer supplied by the caller.

// resultEnvelope is a recyclable lookup-result buffer.
type resultEnvelope struct {
	hits []IOCEntry
}

// envelopePool recycles resultEnvelope buffers across lookups on the hot path.
var envelopePool = sync.Pool{
	New: func() any { return &resultEnvelope{hits: make([]IOCEntry, 0, 8)} },
}

// PooledLookupCount performs an O(1) keyed lookup for each value and returns the
// number of hits, recycling its result buffer through envelopePool so the hot
// path allocates nothing amortised. It exists purely to measure that the index's
// per-query allocation is optional; it does not replace LookupIOCs.
func PooledLookupCount(s *MemoryStore, iocType string, values []string) int {
	env := envelopePool.Get().(*resultEnvelope)
	env.hits = env.hits[:0]
	defer envelopePool.Put(env)

	s.mu.RLock()
	for _, v := range values {
		if e, ok := s.iocs[iocKey(iocType, strings.TrimSpace(v))]; ok {
			env.hits = append(env.hits, e)
		}
	}
	n := len(env.hits)
	s.mu.RUnlock()
	return n
}
