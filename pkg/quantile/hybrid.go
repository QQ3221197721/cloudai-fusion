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

// add inserts x assuming the caller has already gated it (see TailExact.Add):
// x is either filling an unfilled heap or is known to exceed the current K-th
// value. The body is deliberately tiny so it inlines at the two call sites.
//
// It also marks dirty so the lazily-sorted query buffer is rebuilt on the next
// read; the previous version skipped inserts entirely once a query had cleared
// dirty on a full heap, which silently dropped later tail candidates.
func (h *topKMin) add(x float64) {
	if h.capK == 0 {
		return
	}
	if len(h.data) < h.capK {
		h.data = append(h.data, x)
		h.up(len(h.data) - 1)
		h.dirty = true
		return
	}
	if x > h.data[0] {
		h.data[0] = x
		h.down(0)
		h.dirty = true
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
	k      int
	n      int
	high   *topKMin // K largest values (exact)
	lowNeg *topKMin // K largest of negated values => K smallest values (exact)
	// Exactly one of fast/exact is non-nil, chosen at construction. Concrete
	// typing lets Add inline the body increment with NO interface dispatch on
	// the insertion hot path.
	fast    *logBuckets // DDSketch-style O(1) body, used when bodyEps > 0
	exact   *GKSummary  // GK eps->0 exact body, used when bodyEps <= 0
	bodyEps float64
}

// NewTailExact creates a hybrid keeping the K extreme values exactly on each end
// and summarising the body with DDSketch-style log bucket counters (relative error ~bodyEps).
// bodyEps <= 0 uses an exact GK (eps->0) fallback for full precision at O(n) memory cost.
func NewTailExact(k int, bodyEps float64) *TailExact {
	if k < 1 {
		k = 1
	}
	te := &TailExact{
		k:       k,
		high:    newTopKMin(k),
		lowNeg:  newTopKMin(k),
		bodyEps: bodyEps,
	}
	if bodyEps <= 0 {
		te.exact = NewGKExact()
	} else {
		te.fast = newLogBuckets(bodyEps)
	}
	return te
}

// Name implements Sketch.
func (t *TailExact) Name() string {
	if t.bodyEps <= 0 {
		return "TailExact(K=" + strconv.Itoa(t.k) + ",body=GK-eps->0)"
	}
	return "TailExact(K=" + strconv.Itoa(t.k) + ",body=DDSketch-Alpha=" + strconv.FormatFloat(t.bodyEps, 'f', 4, 64) + ")"
}

// Count implements Sketch.
func (t *TailExact) Count() int { return t.n }

// Add ingests x into both tails and the body summary.
//
// Gated tails (the insertion win). The overwhelming majority of a stream is
// body, not tail: once each K-heap is full, a value can only belong to the
// top-K if it exceeds the current K-th largest (the min-heap root), and to the
// bottom-K if it is below the current K-th smallest. We test those two cheap
// comparisons inline and touch a heap ONLY when the value is an actual
// candidate, so a typical body insert costs two float compares instead of two
// heap sifts. This is loss-free: a value that fails the gate provably cannot be
// in the retained tail, so the exact-tail guarantee is unchanged.
func (t *TailExact) Add(x float64) {
	if x != x { // NaN check, cheaper than math.IsNaN
		return
	}
	t.n++

	// High tail: candidate iff the heap is not yet full, or x beats the K-th
	// largest currently retained (data[0] is the min of the top-K).
	if h := t.high; len(h.data) < h.capK || x > h.data[0] {
		h.add(x)
	}
	// Low tail: lowNeg holds negated values, so its root is -(K-th smallest x).
	// x is a bottom-K candidate iff -x beats that root, i.e. x < K-th smallest.
	if l := t.lowNeg; len(l.data) < l.capK || -x > l.data[0] {
		l.add(-x)
	}

	// Body hot path: a predictable branch plus a concrete, inlinable call —
	// no interface dispatch. Almost every insert lands here (O(1), no alloc)
	// and now uses the branch-free fastLog2 bucket index.
	if t.fast != nil {
		t.fast.Add(x)
	} else {
		t.exact.Add(x)
	}
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
// otherwise the log-bucket body estimate (DDSketch relative error guarantee).
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

	// Body: fall back to the log-bucket (or exact GK) estimate.
	if t.fast != nil {
		return t.fast.Quantile(q)
	}
	return t.exact.Quantile(q)
}

// SizeBytes reports both tail heaps plus the body sketch.
func (t *TailExact) SizeBytes() int {
	baseSize := cap(t.high.data)*8 + cap(t.lowNeg.data)*8
	if t.fast != nil {
		return baseSize + t.fast.SizeBytes()
	}
	return baseSize + t.exact.SizeBytes()
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
