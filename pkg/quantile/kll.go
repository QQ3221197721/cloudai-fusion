package quantile

// kll.go implements the Karnin-Lang-Liberty (KLL, 2016) streaming quantile
// sketch: a stack of "compactors". Level h holds weight-2^h items; when a
// compactor fills, it is sorted and every other element (even or odd chosen by a
// fair coin) is promoted to the next level, doubling those items' weight. The
// randomized offset makes the rank estimate unbiased. Memory is O(k) and the
// rank error is O(1/k) with high probability — bounded, never exact.

import (
	"math"
	"math/rand"
	"sort"
	"strconv"
)

// KLL is a bounded-memory quantile sketch with an eps rank-error guarantee.
type KLL struct {
	k          int
	c          float64 // capacity decay per level, classic 2/3
	compactors [][]float64
	n          int
	rng        *rand.Rand
}

// NewKLL creates a KLL sketch with base capacity k (larger k => smaller error,
// more memory). seed makes compaction deterministic for reproducible tests.
func NewKLL(k int, seed int64) *KLL {
	if k < 8 {
		k = 8
	}
	return &KLL{
		k:          k,
		c:          2.0 / 3.0,
		compactors: [][]float64{{}},
		rng:        rand.New(rand.NewSource(seed)),
	}
}

// Name implements Sketch.
func (s *KLL) Name() string { return "KLL(k=" + strconv.Itoa(s.k) + ")" }

// Count implements Sketch.
func (s *KLL) Count() int { return s.n }

// capacity returns the target capacity of the compactor at level h (0 = bottom).
// Lower levels hold more items; capacity decays geometrically going up.
func (s *KLL) capacity(h int) int {
	depth := len(s.compactors)
	cap := int(math.Ceil(float64(s.k) * math.Pow(s.c, float64(depth-1-h))))
	if cap < 2 {
		cap = 2
	}
	return cap
}

// Add ingests x at the bottom compactor and compacts upward as needed.
func (s *KLL) Add(x float64) {
	if math.IsNaN(x) {
		return
	}
	s.compactors[0] = append(s.compactors[0], x)
	s.n++
	s.compress()
}

func (s *KLL) compress() {
	for h := 0; h < len(s.compactors); h++ {
		if len(s.compactors[h]) < s.capacity(h) {
			continue
		}
		if h+1 >= len(s.compactors) {
			s.compactors = append(s.compactors, []float64{})
		}
		buf := s.compactors[h]
		sort.Float64s(buf)
		// Promote every other element; fair coin picks the parity so the
		// estimator is unbiased.
		offset := s.rng.Intn(2)
		for i := offset; i < len(buf); i += 2 {
			s.compactors[h+1] = append(s.compactors[h+1], buf[i])
		}
		s.compactors[h] = buf[:0]
	}
}

// Quantile implements Sketch. It gathers every retained item with its level
// weight (2^h), sorts by value, and walks the cumulative weight to the
// nearest-rank target.
func (s *KLL) Quantile(q float64) float64 {
	if s.n == 0 {
		return math.NaN()
	}
	type wv struct {
		v float64
		w float64
	}
	items := make([]wv, 0, 256)
	var total float64
	for h, comp := range s.compactors {
		w := math.Pow(2, float64(h))
		for _, v := range comp {
			items = append(items, wv{v, w})
			total += w
		}
	}
	if len(items) == 0 {
		return math.NaN()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].v < items[j].v })

	target := math.Ceil(q * total)
	if target < 1 {
		target = 1
	}
	var cum float64
	for _, it := range items {
		cum += it.w
		if cum >= target {
			return it.v
		}
	}
	return items[len(items)-1].v
}

// SizeBytes reports retained float64s across all compactors plus slice headers.
func (s *KLL) SizeBytes() int {
	total := 0
	for _, comp := range s.compactors {
		total += cap(comp) * 8
	}
	total += len(s.compactors) * 24 // slice headers
	return total
}
