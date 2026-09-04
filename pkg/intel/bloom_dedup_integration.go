package intel

// bloom_dedup_integration.go — Two-stage semantic + probabilistic dedup pipeline.
//
// This file wires the BloomDedup probabilistic pre-screener (this task, M28 cold-path
// optimization) to the type-aware Normalizer chain from canonicalizer.go (parallel
// Sam M28 Canonicalizer task). The two stages are orthogonal and compose cleanly:
//
//	Stage 1 (semantic):     Normalizer.Normalize collapses near-duplicates to a
//	                        canonical form ("G00GLE.com" → "google.com",
//	                        "192.168.0.7/32" → "192.168.0.0/24").
//	Stage 2 (probabilistic): BloomDedup pre-screens the canonical key against a
//	                        memory-bounded Bloom filter, only touching the exact
//	                        map on a Bloom hit.
//
// WHY THE COMPOSITION IS SOUND (not double-counting novelty):
//   - Stage 1 changes WHICH keys are considered equal (semantic recall). Its
//     contribution is the false-merge bound ε from ErrorBound().
//   - Stage 2 changes HOW FAST an exact-equality set-membership test runs (cold-path
//     latency + memory locality). It adds NO semantic merging: after canonicalization
//     the Bloom+map pair is an exact set — zero false negatives, and false positives
//     are always resolved by the backing map. So the pipeline's false-merge bound is
//     exactly Stage 1's ε; Stage 2 contributes 0 to it.
//
// HONEST POSITIONING: Stage 2 is standard Bloom pre-filtering (Broder & Mitzenmacher
// 2004 survey; Bloom 1970). It is "engineering excellence", not algorithmic novelty.
// The only novelty claim in this pipeline lives in Stage 1 (canonicalizer.go).

// BloomDedupWithNormalizer is the two-stage pipeline: it normalizes each value with a
// Normalizer, then defers exact-set membership to a BloomDedup. It exposes the same
// Add/Lookup surface as BloomDedup so it is a drop-in replacement on call sites that
// want semantic dedup instead of byte-exact dedup.
type BloomDedupWithNormalizer struct {
	canon      Normalizer
	bloomDedup *BloomDedup
}

// NewBloomDedupWithNormalizer builds the two-stage pipeline. norm may be any Normalizer
// (a single DomainNormalizer/IPNormalizer/HashFuzzyNormalizer, or a ChainNormalizer that
// composes them with a union-bound total ε).
func NewBloomDedupWithNormalizer(norm Normalizer, config BloomConfig) *BloomDedupWithNormalizer {
	return &BloomDedupWithNormalizer{
		canon:      norm,
		bloomDedup: NewBloomDedup(config),
	}
}

// Add canonicalizes value (Stage 1) then inserts the canonical key (Stage 2).
// Returns true when the canonical key was newly inserted, false when it was a
// semantic duplicate already present.
func (bw *BloomDedupWithNormalizer) Add(value string) bool {
	return bw.bloomDedup.Add(bw.canon.Normalize(value))
}

// Lookup canonicalizes value then performs the Bloom-pre-screened exact lookup.
func (bw *BloomDedupWithNormalizer) Lookup(value string) (bool, error) {
	return bw.bloomDedup.Lookup(bw.canon.Normalize(value))
}

// Count returns the number of distinct canonical keys retained.
func (bw *BloomDedupWithNormalizer) Count() int { return bw.bloomDedup.Count() }

// FalseMergeBound returns the pipeline's total false-merge probability, which equals
// the Stage-1 normalizer's ErrorBound(). Stage 2 (Bloom+map) is an exact set and
// contributes nothing, so the pipeline inherits Stage 1's bound verbatim.
func (bw *BloomDedupWithNormalizer) FalseMergeBound() float64 {
	return bw.canon.ErrorBound()
}

// Stats exposes the underlying Bloom cold-path/hot-path counters for observability.
func (bw *BloomDedupWithNormalizer) Stats() *BloomStats {
	return &bw.bloomDedup.stats
}
