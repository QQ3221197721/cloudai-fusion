package quantile

// prombuckets.go implements Prometheus' histogram_quantile() bucket interpolation,
// as documented in PromQL. It bins observations into fixed le upper bounds, then
// linearly interpolates within a bucket assuming uniform density. This is the
// classic baseline that suffers tens of percent error on heavy-tailed data when
// the quantile falls inside a wide/sparse bucket.

import "math"

// PrometheusHistogramQuantile computes q-quantile over buckets that capture the
// observation stream using finite-le upper bounds. The implementation matches the
// published PromQL algorithm: bin into cumulative counts, find the first bucket
// whose count exceeds rank = q * total, then interpolate linearly between lower
// and upper within that bucket. If the +Inf bucket is reached, return the highest
// finite bound.
func PrometheusHistogramQuantile(bucketsLe []float64, values []float64, q float64) float64 {
	if len(bucketsLe) == 0 {
		return math.NaN()
	}
	counts := make([]int64, len(bucketsLe))

	for _, v := range values {
		idx := len(bucketsLe)
		for i, le := range bucketsLe {
			if v <= le {
				idx = i
				break
			}
		}
		if idx < len(bucketsLe) {
			counts[idx]++
		}
	}

	cum := make([]int64, len(counts)+1) // last slot = implicit +Inf bucket
	var acc int64
	for i, c := range counts {
		acc += c
		cum[i+1] = acc
	}
	total := acc
	if total == 0 {
		return math.NaN()
	}

	rank := int64(q * float64(total))
	b := -1
	for b < len(counts) && cum[b+1] < rank {
		b++
	}
	// b is now the index of the bucket containing the quantile (or -1 if all >= rank are beyond tail)
	if b == -1 || b >= len(counts) {
		// Quantile fell in the +Inf bucket: Prometheus returns highest finite bound.
		return bucketsLe[len(bucketsLe)-1]
	}

	upper := bucketsLe[b]
	lower := 0.0
	if b > 0 {
		lower = bucketsLe[b-1]
	}
	countBefore := cum[b]
	inBucket := cum[b+1] - countBefore
	if inBucket == 0 {
		return upper
	}
	return lower + (upper-lower)*(float64(rank-countBefore)/float64(inBucket))
}

// PrometheusHistogramEstimator wraps the above into Sketch so it can be driven by
// the same harness as other methods. Note that it requires a known bucket layout
// at construction time, just like production histograms.
type PrometheusHistogramEstimator struct {
	name    string
	buckets []float64 // finite le upper bounds, ascending
	counts  []int64   // per-bucket counts (finite buckets only)
	cum     []int64   // cum[i+1] = count of values <= buckets[i]; cum[0]=0
	n       int64
}

// NewPrometheusHistogram creates an estimator for the given ascending upper
// bounds. The layout is fixed at construction, exactly like a production
// histogram whose buckets are chosen before any data is seen.
func NewPrometheusHistogram(name string, buckets []float64) *PrometheusHistogramEstimator {
	b := make([]float64, len(buckets))
	copy(b, buckets)
	return &PrometheusHistogramEstimator{
		name:    name,
		buckets: b,
		counts:  make([]int64, len(b)),
		cum:     make([]int64, len(b)+1),
	}
}

func (h *PrometheusHistogramEstimator) resetCum() {
	h.cum = make([]int64, len(h.counts)+1)
	var acc int64
	for i, c := range h.counts {
		acc += c
		h.cum[i+1] = acc
	}
}

// Name implements Sketch.
func (h *PrometheusHistogramEstimator) Name() string { return h.name }

// Count implements Sketch.
func (h *PrometheusHistogramEstimator) Count() int { return int(h.n) }

// Add bins x into one of the finite buckets; anything larger lands in the +Inf
// bin but does not grow the size (we never store it).
func (h *PrometheusHistogramEstimator) Add(x float64) {
	if math.IsNaN(x) {
		return
	}
	h.n++
	idx := len(h.counts)
	for i, le := range h.buckets {
		if x <= le {
			idx = i
			break
		}
	}
	if idx < len(h.counts) {
		h.counts[idx]++
		h.resetCum()
	} else {
		h.resetCum()
	}
}

// Quantile implements Sketch.
func (h *PrometheusHistogramEstimator) Quantile(q float64) float64 {
	total := h.cum[len(h.cum)-1]
	if total == 0 {
		return math.NaN()
	}
	rank := int64(q * float64(total))
	b := -1
	for b < len(h.counts) && h.cum[b+1] < rank {
		b++
	}
	if b == -1 || b >= len(h.counts) {
		return h.buckets[len(h.buckets)-1]
	}
	upper := h.buckets[b]
	lower := 0.0
	if b > 0 {
		lower = h.buckets[b-1]
	}
	countBefore := h.cum[b]
	inBucket := h.cum[b+1] - countBefore
	if inBucket == 0 {
		return upper
	}
	return lower + (upper-lower)*(float64(rank-countBefore)/float64(inBucket))
}

// SizeBytes uses O(#buckets) fixed memory independent of n.
func (h *PrometheusHistogramEstimator) SizeBytes() int {
	return cap(h.buckets)*8 + cap(h.counts)*8 + cap(h.cum)*8
}
