package quantile

// hybrid.go implements TailExact, the "attack structure": a bounded-memory
// hybrid that beats KLL/t-digest exactly where SLOs live — the extreme tail.
//
// Idea. Keep the K largest and K smallest observations EXACTLY, in two fixed-size
// heaps, and summarise the body with a bounded GK summary. Because we also track
// the exact count n, any quantile whose 1-based rank r satisfies r > n−K (or
// r ≤ K) is answered with ZERO error: the element at rank r is simply the
// (n−r+1)-th largest, which we hold verbatim. KLL and t-digest only ever offer an
// eps guarantee there, so an adversary that packs mass against a bucket edge or
// forces centroid over-merging in the tail cannot fool TailExact.
//
// Honest boundary (see docs/algorithm-exact-quantile.md). The exact region is
// [1−K/n, 1] ∪ [0, K/n]. For a FIXED budget K it shrinks as n grows: keeping
// p999 exact forever needs K ≥ n/1000, i.e. memory linear in n. There is no free
// lunch — the Munro-Paterson Ω(n) lower bound still holds. What TailExact buys is
// exactness in the operationally important tail for streams up to ≈ K/(1−q)
// observations, with graceful GK-bounded fallback beyond that.

import (
	"math"
	"sort"
	"strconv"
)

// topKMin retains the K largest values pushed to it, using a binary min-heap so
// the smallest retained value (the eviction candidate) sits at the root.
//
// sortedBuf/dirty implement a lazily-maintained ascending view: a query re-sorts
// only when new values arrived since the last sort, and always into the SAME
// preallocated backing array. Repeated Quantile calls on a settled stream (the
// common dashboard pattern: many p99 reads between rare Adds) therefore run with
// ZERO allocations instead of allocating a fresh K-element slice per query.
type topKMin struct {
	data      []float64
	capK      int
	sortedBuf []float64 // reusable ascending scratch, cap == capK
	dirty     bool      // true when data changed since sortedBuf was last built
}

func newTopKMin(k int) *topKMin {
	if k < 0 {
		k = 0
	}
	return &topKMin{
		data:      make([]float64, 0, k),
		capK:      k,
		sortedBuf: make([]float64, 0, k),
	}
}

func (h *topKMin) Len() int { return len(h.data) }

func (h *topKMin) add(x float64) {
	if h.capK == 0 {
		return
	}
	h.dirty = true
	if len(h.data) < h.capK {
		h.data = append(h.data, x)
		h.up(len(h.data) - 1)
		return
	}
	if x > h.data[0] {
		h.data[0] = x
		h.down(0)
	}
}

func (h *topKMin) up(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if h.data[i] >= h.data[parent] {
			break
		}
		h.data[i], h.data[parent] = h.data[parent], h.data[i]
		i = parent
	}
}

func (h *topKMin) down(i int) {
	n := len(h.data)
	for {
		l, r, smallest := 2*i+1, 2*i+2, i
		if l < n && h.data[l] < h.data[smallest] {
			smallest = l
		}
		if r < n && h.data[r] < h.data[smallest] {
			smallest = r
		}
		if smallest == i {
			break
		}
		h.data[i], h.data[smallest] = h.data[smallest], h.data[i]
		i = smallest
	}
}

// sortedAsc returns an ascending view of the retained values backed by the
// estimator's own reusable buffer. It re-sorts only when the heap changed since
// the previous call, so read-heavy query workloads allocate nothing. The
// returned slice is owned by the estimator and must be treated as read-only.
func (h *topKMin) sortedAsc() []float64 {
	n := len(h.data)
	if !h.dirty && len(h.sortedBuf) == n {
		return h.sortedBuf
	}
	if cap(h.sortedBuf) < n {
		h.sortedBuf = make([]float64, n)
	}
	h.sortedBuf = h.sortedBuf[:n]
	copy(h.sortedBuf, h.data)
	sort.Float64s(h.sortedBuf)
	h.dirty = false
	return h.sortedBuf
}

// TailExact is the bounded-memory tail-exact hybrid estimator.
type TailExact struct {
	k       int
	n       int
	high    *topKMin // K largest values (exact)
	lowNeg  *topKMin // K largest of negated values => K smallest values (exact)
	body    *GKSummary
	bodyEps float64
}

// NewTailExact creates a hybrid keeping the K extreme values exactly on each end
// and summarising the body with a GK(bodyEps) summary. bodyEps ≤ 0 makes the body
// exact too (degenerating to full retention).
func NewTailExact(k int, bodyEps float64) *TailExact {
	if k < 1 {
		k = 1
	}
	return &TailExact{
		k:       k,
		high:    newTopKMin(k),
		lowNeg:  newTopKMin(k),
		body:    NewGKSummary(bodyEps),
		bodyEps: bodyEps,
	}
}

// Name implements Sketch.
func (t *TailExact) Name() string {
	return "TailExact(K=" + strconv.Itoa(t.k) + ",body=" + t.body.Name() + ")"
}

// Count implements Sketch.
func (t *TailExact) Count() int { return t.n }

// Add ingests x into both tails and the body summary.
func (t *TailExact) Add(x float64) {
	if math.IsNaN(x) {
		return
	}
	t.n++
	t.high.add(x)
	t.lowNeg.add(-x)
	t.body.Add(x)
}

// InExactRegion reports whether the q-quantile is answered with zero error given
// the current count, i.e. whether its rank falls inside a retained tail.
func (t *TailExact) InExactRegion(q float64) bool {
	if t.n == 0 {
		return false
	}
	r := t.rank(q)
	return r > t.n-t.high.Len() || r <= t.lowNeg.Len()
}

func (t *TailExact) rank(q float64) int {
	r := int(math.Ceil(q * float64(t.n)))
	if r < 1 {
		r = 1
	}
	if r > t.n {
		r = t.n
	}
	return r
}

// Quantile returns the q-quantile: exact if the rank lands in a retained tail,
// otherwise the GK body estimate.
func (t *TailExact) Quantile(q float64) float64 {
	if t.n == 0 {
		return math.NaN()
	}
	r := t.rank(q)
	highLen := t.high.Len()
	lowLen := t.lowNeg.Len()

	// High tail exact region: rank r corresponds to the (n-r+1)-th largest.
	if r > t.n-highLen {
		desc := t.high.sortedAsc() // ascending
		// element at rank r == (n-r)-th from the top (0-based into descending).
		// descending index d = n - r; ascending index = highLen-1-d.
		d := t.n - r
		ai := highLen - 1 - d
		if ai < 0 {
			ai = 0
		}
		if ai >= highLen {
			ai = highLen - 1
		}
		return desc[ai]
	}

	// Low tail exact region: rank r (1-based) is the r-th smallest.
	if r <= lowLen {
		neg := t.lowNeg.sortedAsc() // ascending in negated space => descending x
		// The r-th smallest x is the r-th from the small end.
		// lowNeg holds -x; ascending -x means descending x; so the largest -x
		// (last) is the smallest x. r-th smallest x => index lowLen-r.
		idx := lowLen - r
		if idx < 0 {
			idx = 0
		}
		if idx >= lowLen {
			idx = lowLen - 1
		}
		return -neg[idx]
	}

	// Body: fall back to the bounded GK estimate.
	return t.body.Quantile(q)
}

// SizeBytes reports both tail heaps plus the GK body summary.
func (t *TailExact) SizeBytes() int {
	return cap(t.high.data)*8 + cap(t.lowNeg.data)*8 + t.body.SizeBytes()
}

// ExactTailFraction returns K/n, the width of each exact tail region under the
// current count. Reported in the tradeoff analysis so the shrinking-tail honesty
// is quantified, not merely asserted.
func (t *TailExact) ExactTailFraction() float64 {
	if t.n == 0 {
		return 1
	}
	return math.Min(1, float64(t.k)/float64(t.n))
}
