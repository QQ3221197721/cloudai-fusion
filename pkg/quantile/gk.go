package quantile

// gk.go implements the Greenwald-Khanna (2001) streaming quantile sketch, plus
// its eps->0 specialization. GK keeps a summary of tuples <v, g, delta>:
//
//	g_i     = rmin(i) - rmin(i-1)   (min-rank gap to the previous tuple)
//	delta_i = rmax(i) - rmin(i)     (uncertainty in v_i's true rank)
//
// where rmin(i)=sum_{j<=i} g_j and rmax(i)=rmin(i)+delta_i. The compression
// invariant g_i+delta_i <= floor(2*eps*n) guarantees that a query for rank r
// returns a value whose true rank is within eps*n of r. Memory is
// O((1/eps) log(eps*n)).
//
// eps -> 0 specialization: as eps shrinks, floor(2*eps*n) stays below 1 until n
// grows past 1/(2*eps), so every inserted tuple gets delta=0 and NOTHING is ever
// merged — the summary degenerates into storing the entire sorted stream and
// becomes exact. This is not a trick: it is the theory made visible. Zero error
// costs O(n) memory. NewGKExact() exposes that mode explicitly.

import (
	"math"
	"strconv"
)

// gkTuple is one GK summary entry.
type gkTuple struct {
	v     float64
	g     int   // rmin gap
	delta int   // rank uncertainty
}

// GKSummary is the classic Greenwald-Khanna estimator: bounded memory with a
// guaranteed eps rank-error, but no zero-error guarantee on adversarial streams.
// For exactness in a region use TailExact instead.
type GKSummary struct {
	eps    float64
	exact  bool // eps==0: retain everything, never compress
	n      int
	tuples []gkTuple
}

// NewGKSummary creates a GK estimator for the requested epsilon (rank error
// fraction). A standard operational choice is eps ~ 0.01.
func NewGKSummary(eps float64) *GKSummary {
	if eps <= 0 {
		return NewGKExact()
	}
	return &GKSummary{eps: eps}
}

// NewGKExact is the eps->0 specialization: GK configured to retain every value,
// yielding zero error at O(n) memory. It exists to demonstrate, in running code,
// the space-error boundary that makes bounded-memory exactness impossible.
func NewGKExact() *GKSummary {
	return &GKSummary{eps: 0, exact: true}
}

// Name implements Sketch.
func (g *GKSummary) Name() string {
	if g.exact {
		return "GK(eps->0,exact)"
	}
	return "GK(eps=" + strconv.FormatFloat(g.eps, 'f', 3, 64) + ")"
}

// Count implements Sketch.
func (g *GKSummary) Count() int { return g.n }

// Add inserts x into the summary in O(len(tuples)) time (linear scan for the
// insertion slot, matching the reference GK implementation).
func (g *GKSummary) Add(x float64) {
	if x != x { // NaN check, faster than math.IsNaN
		return
	}

	// Inline sort.Search to avoid function call overhead
	idx := len(g.tuples)
	lo, hi := 0, idx
	for lo < hi {
		m := lo + (hi-lo)/2
		if x >= g.tuples[m].v {
			lo = m + 1
		} else {
			hi = m
		}
	}
	idx = lo

	t := gkTuple{v: x, g: 1, delta: 0}
	if !g.exact && idx != 0 && idx != len(g.tuples) {
		t.delta = int(2*g.eps*float64(g.n))
	}

	g.tuples = append(g.tuples, gkTuple{})
	copy(g.tuples[idx+1:], g.tuples[idx:])
	g.tuples[idx] = t
	g.n++

	if g.exact {
		return
	}
	period := int(1 / (2 * g.eps))
	if period < 1 {
		period = 1
	}
	if g.n%period == 0 {
		g.compress()
	}
}

// compress merges adjacent tuples while the GK invariant permits, bounding the
// summary size to O((1/eps) log(eps*n)).
func (g *GKSummary) compress() {
	threshold := int(2 * g.eps * float64(g.n))
	for i := len(g.tuples) - 2; i >= 1; i-- {
		if g.tuples[i].g+g.tuples[i+1].g+g.tuples[i+1].delta <= threshold {
			g.tuples[i+1].g += g.tuples[i].g
			g.tuples = append(g.tuples[:i], g.tuples[i+1:]...)
		}
	}
}

// Quantile implements Sketch using the GK query rule: scan tuples accumulating
// rmin (inclusive of the current tuple) and return the first value whose rank
// interval [rmin, rmax] brackets the target rank r within eps*n. In exact mode
// (eps=0, all delta=0, every value its own g=1 tuple) rmin hits r exactly at
// index r-1, so the returned value is the true r-th order statistic — zero error.
func (g *GKSummary) Quantile(q float64) float64 {
	if g.n == 0 {
		return math.NaN()
	}
	if len(g.tuples) == 1 {
		return g.tuples[0].v
	}
	r := int(math.Ceil(q * float64(g.n)))
	if r < 1 {
		r = 1
	}
	if r > g.n {
		r = g.n
	}
	epsN := int(g.eps * float64(g.n))
	rmin := 0
	for i := 0; i < len(g.tuples); i++ {
		rmin += g.tuples[i].g
		rmax := rmin + g.tuples[i].delta
		if r-rmin <= epsN && rmax-r <= epsN {
			return g.tuples[i].v
		}
	}
	return g.tuples[len(g.tuples)-1].v
}

// SizeBytes reports retained state. Each tuple is float64(8) + two int(16) = 24
// bytes on a 64-bit target. In exact mode this grows as O(n); in bounded mode it
// plateaus at O((1/eps) log(eps*n)).
func (g *GKSummary) SizeBytes() int {
	const tupleBytes = 24
	return cap(g.tuples) * tupleBytes
}
