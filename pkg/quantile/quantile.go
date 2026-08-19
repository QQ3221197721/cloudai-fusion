// Package quantile implements streaming quantile estimators together with an
// exact order-statistics baseline, and provides the honest theoretical framing
// for why bounded-memory zero-error quantiles are impossible on an arbitrary
// stream.
//
// # The problem
//
// Prometheus' histogram_quantile() derives a percentile by linearly
// interpolating inside a fixed, pre-declared bucket, assuming a uniform density
// across that bucket. On heavy-tailed or multi-modal latency the assumption is
// wrong and the estimate can be off by tens of percent (see the Prometheus
// baseline in prombuckets.go and the measured comparison in the tests).
//
// The mergeable sketches — GK, KLL, t-digest — are genuinely bounded in memory,
// but each carries an epsilon rank-error guarantee: the returned element's true
// rank is within +/- eps*n of the requested rank. They cannot be exact on an
// adversarial stream.
//
// # What is and is not possible
//
// A single-pass exact quantile requires Omega(n) memory in the comparison model
// (Munro & Paterson, 1980). Therefore "bounded memory AND zero error on an
// arbitrary stream" is provably impossible. This package does not pretend
// otherwise. Instead it offers:
//
//   - Exact: an augmented treap. O(log n) expected insert, O(log n) exact
//     rank/select. Zero error, O(n) memory. This is the reference oracle.
//
//   - TailExact (the "attack structure"): a bounded-memory hybrid that keeps the
//     extreme tail EXACTLY in a fixed-size ordered buffer and summarises the body
//     with a GK summary. Its guarantee is strictly stronger than KLL/t-digest in
//     the region operators actually alert on: for any quantile whose rank falls
//     inside the retained tail it returns the true value with zero error, while
//     the sketches retain only an eps guarantee there. The honest cost, spelled
//     out in docs/algorithm-exact-quantile.md, is that the exact-tail region
//     [1 - K/n, 1] shrinks as n grows for a fixed budget K.
//
// All estimators implement Sketch so the comparison harness treats them
// uniformly.
package quantile

import (
	"math"
	"sort"
)

// Sketch is a streaming quantile estimator over float64 observations.
//
// Implementations are not required to be safe for concurrent use; callers that
// need concurrency must synchronise externally. The interface is intentionally
// minimal so the exact baseline and every approximate competitor can be driven
// by one comparison harness.
type Sketch interface {
	// Name identifies the estimator in reports and benchmark output.
	Name() string
	// Add ingests one observation.
	Add(x float64)
	// Quantile returns the estimated q-quantile for q in [0,1]. The rank
	// convention is nearest-rank (see NearestRank): q maps to the element at
	// 1-based rank ceil(q*n). All estimators share this convention so the only
	// difference measured is approximation error, not a definitional offset.
	Quantile(q float64) float64
	// Count returns the number of observations ingested.
	Count() int
	// SizeBytes returns the estimator's current resident memory footprint in
	// bytes, counting the live payload (values/centroids/tuples) plus the fixed
	// per-object overhead. It is a real measurement of retained state, used for
	// the bytes/stream metric.
	SizeBytes() int
}

// NearestRank returns the element of sorted at 1-based rank ceil(q*n), the
// canonical "nearest rank" quantile. sorted must be ascending. This is the
// definition every estimator in this package targets, so that GK/KLL/t-digest
// rank error is measured against the same yardstick as the exact structure.
//
// q is clamped to [0,1]; an empty slice yields NaN.
func NearestRank(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return math.NaN()
	}
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[n-1]
	}
	rank := int(math.Ceil(q * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}

// ExactQuantileSorted is NearestRank over an already-sorted slice; kept as a
// readable alias for call sites that compute the per-dataset ground truth.
func ExactQuantileSorted(sorted []float64, q float64) float64 {
	return NearestRank(sorted, q)
}

// SortedCopyQuantile sorts a copy of values and returns its nearest-rank
// q-quantile. This is the unambiguous ground truth for a finite sample set and
// is used by tests to score every estimator's approximation error.
func SortedCopyQuantile(values []float64, q float64) float64 {
	s := make([]float64, len(values))
	copy(s, values)
	sort.Float64s(s)
	return NearestRank(s, q)
}

// AbsError returns |estimate - truth|.
func AbsError(estimate, truth float64) float64 {
	return math.Abs(estimate - truth)
}

// RelErrorPct returns the relative error of estimate against truth as a
// percentage. When truth is zero it returns NaN, because a percentage relative
// to zero is undefined; callers should fall back to absolute error there.
func RelErrorPct(estimate, truth float64) float64 {
	if truth == 0 {
		return math.NaN()
	}
	return math.Abs(estimate-truth) / math.Abs(truth) * 100
}

// rankOf returns the 0-based number of elements in sorted strictly less than x,
// i.e. the rank at which x would be inserted keeping sorted order. Used to score
// an estimate by the true rank of the value it returned.
func rankOf(sorted []float64, x float64) int {
	return sort.SearchFloat64s(sorted, x)
}

// RankErrorFraction scores an estimate by rank rather than by value: it returns
// |rank(estimate) - targetRank| / n, the quantity GK/KLL bound by eps. sorted is
// the full ascending dataset. This is the fairest way to compare rank-error
// sketches, because two very different values can share almost the same rank in a
// flat region of the distribution.
func RankErrorFraction(sorted []float64, estimate float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return math.NaN()
	}
	targetRank := int(math.Ceil(q*float64(n))) - 1 // 0-based
	if targetRank < 0 {
		targetRank = 0
	}
	if targetRank > n-1 {
		targetRank = n - 1
	}
	got := rankOf(sorted, estimate)
	// rankOf gives the count strictly less than estimate; clamp into range.
	if got > n {
		got = n
	}
	diff := got - targetRank
	if diff < 0 {
		diff = -diff
	}
	return float64(diff) / float64(n)
}
