package tsdb

// evidence_tsdb.go layers two independent barriers over the time-series store:
//
//  1. Evidence-native barrier — each write and query is sealed into a signed,
//     offline-verifiable evidence.Receipt binding the operation to its series
//     and range. We can prove "series S was written/queried at time X".
//
//  2. Independent-innovation barrier — a compaction-efficiency scorer aligns
//     storage layout with the observed query workload. It tracks per-series
//     access frequency and scores any proposed "hot tier" (the series a
//     compaction strategy keeps at fine resolution) by the fraction of real
//     query traffic it captures, and recommends the workload-optimal hot set.

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sort"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TSOpResult is the verifiable result of a single time-series operation.
type TSOpResult struct {
	Op      string            `json:"op"` // "write" | "query"
	Series  string            `json:"series"`
	Receipt *evidence.Receipt `json:"receipt,omitempty"`
}

// CompactionScore reports how well a candidate hot-tier aligns with the
// observed query workload.
type CompactionScore struct {
	HotSeries      []string `json:"hot_series"`
	HotQueryCount  int      `json:"hot_query_count"`
	TotalQueries   int      `json:"total_queries"`
	AlignmentScore float64  `json:"alignment_score"` // 0..1, fraction of query traffic captured
}

// EvidenceTSDBEngine seals TS operations and scores compaction strategies.
type EvidenceTSDBEngine struct {
	receiptBuilder *evidence.ReceiptBuilder

	mu           sync.Mutex
	queryCounts  map[string]int // series → observed query count
	totalQueries int
}

// NewEvidenceTSDBEngine builds an engine with a freshly generated key.
func NewEvidenceTSDBEngine() *EvidenceTSDBEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceTSDBEngine{
		receiptBuilder: evidence.NewReceiptBuilder("tsdb", priv),
		queryCounts:    make(map[string]int),
	}
}

// RecordWrite seals a time-series write into a signed receipt.
func (e *EvidenceTSDBEngine) RecordWrite(series string, timestampUnix int64, value float64) (*TSOpResult, error) {
	if series == "" {
		return nil, fmt.Errorf("tsdb: series must not be empty")
	}
	result := &TSOpResult{Op: "write", Series: series}
	input := struct {
		Series string  `json:"series"`
		TS     int64   `json:"ts"`
		Value  float64 `json:"value"`
	}{series, timestampUnix, value}
	receipt, err := e.receiptBuilder.Build("tsdb.write", input, result)
	if err != nil {
		return nil, fmt.Errorf("tsdb: seal write: %w", err)
	}
	result.Receipt = receipt
	return result, nil
}

// RecordQuery seals a time-series range query into a receipt and records the
// access against the series for compaction scoring.
func (e *EvidenceTSDBEngine) RecordQuery(series string, rangeStartUnix, rangeEndUnix int64) (*TSOpResult, error) {
	if series == "" {
		return nil, fmt.Errorf("tsdb: series must not be empty")
	}
	if rangeEndUnix < rangeStartUnix {
		return nil, fmt.Errorf("tsdb: query range end before start")
	}
	e.mu.Lock()
	e.queryCounts[series]++
	e.totalQueries++
	e.mu.Unlock()

	result := &TSOpResult{Op: "query", Series: series}
	input := struct {
		Series string `json:"series"`
		Start  int64  `json:"start"`
		End    int64  `json:"end"`
	}{series, rangeStartUnix, rangeEndUnix}
	receipt, err := e.receiptBuilder.Build("tsdb.query", input, result)
	if err != nil {
		return nil, fmt.Errorf("tsdb: seal query: %w", err)
	}
	result.Receipt = receipt
	return result, nil
}

// ---------------------------------------------------------------------------
// INNOVATION: compaction-efficiency scoring against the query workload
// ---------------------------------------------------------------------------

// ScoreCompaction scores a candidate hot-tier (series kept at fine resolution)
// by the fraction of observed query traffic it captures. A strategy that keeps
// the most-queried series hot scores near 1.0; one that keeps rarely-queried
// series hot wastes fast storage and scores low.
func (e *EvidenceTSDBEngine) ScoreCompaction(hotSeries []string) CompactionScore {
	e.mu.Lock()
	defer e.mu.Unlock()

	hotSet := make(map[string]bool, len(hotSeries))
	for _, s := range hotSeries {
		hotSet[s] = true
	}
	hotHits := 0
	for series, count := range e.queryCounts {
		if hotSet[series] {
			hotHits += count
		}
	}
	score := 0.0
	if e.totalQueries > 0 {
		score = float64(hotHits) / float64(e.totalQueries)
	}
	return CompactionScore{
		HotSeries:      hotSeries,
		HotQueryCount:  hotHits,
		TotalQueries:   e.totalQueries,
		AlignmentScore: score,
	}
}

// RecommendHotSeries returns the top-N most-queried series — the workload-optimal
// hot tier that maximizes ScoreCompaction's alignment.
func (e *EvidenceTSDBEngine) RecommendHotSeries(topN int) []string {
	if topN <= 0 {
		topN = 1
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	type sc struct {
		series string
		count  int
	}
	ranked := make([]sc, 0, len(e.queryCounts))
	for s, c := range e.queryCounts {
		ranked = append(ranked, sc{s, c})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].series < ranked[j].series
	})
	if topN > len(ranked) {
		topN = len(ranked)
	}
	out := make([]string, 0, topN)
	for i := 0; i < topN; i++ {
		out = append(out, ranked[i].series)
	}
	return out
}
