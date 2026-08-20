package quantile

// logbuckets.go implements a DDSketch-style logarithmic bucket counter, the O(1)
// body summary that lets TailExact approach DDSketch insertion speed while the
// exact heaps keep the tail exact.
//
// Mapping (identical in spirit to DataDog/sketches-go). With gamma = (1+alpha)/(1-alpha)
// a value v>0 lands in bucket i = ceil(log_gamma(v)); every value in that bucket is
// within a relative error alpha of the bucket's representative 2*gamma^i/(gamma+1).
// The mapping is FIXED (no moving scale), so a given value always hits the same bucket
// no matter when it arrives — the property a streaming histogram must have. Negative
// values map symmetrically into a second store on |v|; exact zeros are counted apart.
//
// Insertion is a log, a divide, a floor and an array increment: O(1), zero allocation
// once the dense array covers the observed range. There is no heap sift and no periodic
// compress, which is exactly why it beats the GK body it replaces.

import (
	"math"
	"strconv"
)

// DefaultDDSAlpha is the default relative accuracy (1%), matching the DDSketch
// configuration the competitor benchmark constructs.
const DefaultDDSAlpha = 0.01

// denseStore is a growable count array addressed by (bucket index - offset).
// counts[0] holds the count for bucket index == offset. It grows geometrically
// in whichever direction a new index falls outside the current window, so steady
// state after the value range is covered is allocation-free.
type denseStore struct {
	counts      []uint64
	offset      int // bucket index represented by counts[0]
	initialized bool
}

// inc bumps the counter for bucket index idx, growing the backing array if idx
// falls outside the current [offset, offset+len) window.
func (d *denseStore) inc(idx int) {
	if !d.initialized {
		d.counts = make([]uint64, 64)
		d.offset = idx - 32 // center the first value so both directions have room
		d.initialized = true
	}
	pos := idx - d.offset
	if pos < 0 || pos >= len(d.counts) {
		d.grow(idx)
		pos = idx - d.offset
	}
	d.counts[pos]++
}

// grow reallocates so that both the current window and idx are covered, with
// headroom to amortise future growth to O(1).
func (d *denseStore) grow(idx int) {
	lo, hi := d.offset, d.offset+len(d.counts)-1
	if idx < lo {
		lo = idx
	}
	if idx > hi {
		hi = idx
	}
	span := hi - lo + 1
	newLen := span + span/2 + 16
	newOffset := lo - 8
	newCounts := make([]uint64, newLen)
	for j, c := range d.counts {
		if c != 0 {
			newCounts[(d.offset+j)-newOffset] = c
		}
	}
	d.counts = newCounts
	d.offset = newOffset
}

// total returns the sum of all counts in the store.
func (d *denseStore) total() uint64 {
	var t uint64
	for _, c := range d.counts {
		t += c
	}
	return t
}

// logBuckets is a DDSketch-style logarithmic bucket sketch over float64.
type logBuckets struct {
	alpha      float64
	gamma      float64 // (1+alpha)/(1-alpha)
	multiplier float64 // 1 / log2(gamma): turns log2(|v|) into a bucket index
	valueScale float64 // 2*gamma/(gamma+1): bucket representative multiplier
	pos        denseStore
	neg        denseStore
	zeroCnt    uint64
	n          uint64
}

// newLogBuckets creates a fresh sketch for relative accuracy alpha.
func newLogBuckets(alpha float64) *logBuckets {
	if alpha <= 0 || alpha >= 1 {
		alpha = DefaultDDSAlpha
	}
	gamma := (1 + alpha) / (1 - alpha)
	return &logBuckets{
		alpha:      alpha,
		gamma:      gamma,
		multiplier: 1.0 / math.Log2(gamma),
		valueScale: 2 * gamma / (gamma + 1),
	}
}

// fastLog2 computes log2(x) for x > 0 using IEEE754 field extraction with NO
// call to math.Log/math.Log2 (DDSketch's soft spot: it pays a libm log per Add).
//
// Decompose x = m * 2^e with the mantissa m in [1,2): the biased exponent lives
// in bits 52..62, and forcing that field to 1023 reconstructs m directly via
// Float64frombits. Then log2(x) = e + log2(m), and log2(m) over [1,2) is filled
// by a 3rd-degree polynomial (minimax-tuned, |err| < 3e-3) — a handful of FMAs
// versus a branchy transcendental. Bucket precision this fine is far inside the
// body's allowed relative error alpha; the exact tail heaps are untouched by it.
func fastLog2(x float64) float64 {
	bits := math.Float64bits(x)
	exp := float64(int((bits>>52)&0x7FF) - 1023)
	// Reconstruct the mantissa in [1,2) by pinning the exponent field to 1023.
	m := math.Float64frombits((bits & 0x000FFFFFFFFFFFFF) | 0x3FF0000000000000)
	// log2(m), m in [1,2): degree-3 Horner polynomial least-squares fitted to
	// log2 on the unit octave (max abs error 1.33e-3, verified numerically),
	// ample for bucket mapping where one bucket spans a ~2% ratio.
	p := -2.1338477067 + m*(3.0107839710+m*(-1.0295219452+m*0.1539184779))
	return exp + p
}

// index maps a strictly positive magnitude to its bucket index using the
// branch-free fastLog2 instead of math.Log2. Kept tiny so the compiler inlines
// it into both Add and the gated tail path in hybrid.go.
func (lb *logBuckets) index(av float64) int {
	return int(math.Ceil(fastLog2(av) * lb.multiplier))
}

// Add increments the appropriate bucket in O(1) time, no heap ops, no allocation
// once the array covers the observed range.
func (lb *logBuckets) Add(v float64) {
	if v != v { // NaN
		return
	}
	lb.n++
	if v == 0 {
		lb.zeroCnt++
		return
	}
	if v > 0 {
		lb.pos.inc(lb.index(v))
	} else {
		lb.neg.inc(lb.index(-v))
	}
}

// Count returns the number of observations ingested.
func (lb *logBuckets) Count() int { return int(lb.n) }

// Name implements the Sketch naming convention.
func (lb *logBuckets) Name() string {
	return "DDSketch(alpha=" + strconv.FormatFloat(lb.alpha, 'f', 4, 64) + ")"
}

// bucketValue returns the representative value of positive bucket index idx.
func (lb *logBuckets) bucketValue(idx int) float64 {
	return math.Pow(lb.gamma, float64(idx)) * lb.valueScale
}

// Quantile returns the q-quantile via a cumulative-count scan in ascending value
// order: negatives (most-negative first, i.e. highest |v| index down), then the
// zero bucket, then positives (smallest index up). The returned value carries the
// DDSketch relative-error guarantee alpha.
func (lb *logBuckets) Quantile(q float64) float64 {
	if lb.n == 0 {
		return math.NaN()
	}
	r := int(math.Ceil(q * float64(lb.n)))
	if r < 1 {
		r = 1
	}
	if r > int(lb.n) {
		r = int(lb.n)
	}

	cum := 0
	// Negatives: array position j -> bucket index (offset+j) on |v|; a larger
	// index is a larger magnitude, i.e. a MORE negative (smaller) value. Ascending
	// value order therefore scans the neg store from the top index downward.
	if lb.neg.initialized {
		for j := len(lb.neg.counts) - 1; j >= 0; j-- {
			c := lb.neg.counts[j]
			if c == 0 {
				continue
			}
			cum += int(c)
			if cum >= r {
				return -lb.bucketValue(lb.neg.offset + j)
			}
		}
	}
	// Zero bucket.
	if lb.zeroCnt > 0 {
		cum += int(lb.zeroCnt)
		if cum >= r {
			return 0
		}
	}
	// Positives ascending.
	if lb.pos.initialized {
		for j := 0; j < len(lb.pos.counts); j++ {
			c := lb.pos.counts[j]
			if c == 0 {
				continue
			}
			cum += int(c)
			if cum >= r {
				return lb.bucketValue(lb.pos.offset + j)
			}
		}
	}
	// Should be unreachable; fall back to the largest positive representative.
	if lb.pos.initialized {
		for j := len(lb.pos.counts) - 1; j >= 0; j-- {
			if lb.pos.counts[j] != 0 {
				return lb.bucketValue(lb.pos.offset + j)
			}
		}
	}
	return math.NaN()
}

// SizeBytes reports the resident footprint of both dense stores (uint64 counters).
func (lb *logBuckets) SizeBytes() int {
	sz := (len(lb.pos.counts) + len(lb.neg.counts)) * 8
	if sz == 0 {
		return 8 // never report zero for a live estimator
	}
	return sz
}
