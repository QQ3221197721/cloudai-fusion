package quantile

// exact.go implements the zero-error reference oracle: an augmented,
// randomized balanced BST (a treap) whose nodes carry subtree sizes so that a
// select-by-rank query is O(log n) with no approximation whatsoever. This is
// the ground-truth structure the whole comparison is scored against.
//
// Why a treap rather than a sorted slice? A sorted slice supports O(1) select
// but O(n) insert (element shifting), so a streaming ingest of n items costs
// O(n^2). The task asks specifically for an O(log n)-insert exact baseline; a
// treap delivers expected O(log n) insert AND O(log n) select, matching the
// bound a skip list or a red-black tree would give, with far less code.
//
// Memory is unavoidably O(n): keeping every distinct value (with a multiplicity
// count) is exactly what the Munro-Paterson lower bound says an exact single
// pass must pay. We do not hide that cost; SizeBytes reports it honestly.

import (
	"math"
	"math/rand"
)

// treapNode is one BST node. Equal values are coalesced via cnt (multiplicity)
// so a stream of many duplicates stays compact; size is the total multiplicity
// of the whole subtree, which is what makes rank queries exact and O(height).
type treapNode struct {
	value       float64
	priority    uint64
	cnt         int64 // multiplicity of value at this node
	size        int64 // total count in subtree (sum of cnt)
	left, right *treapNode
}

// Exact is a streaming exact-quantile structure. It is the reference against
// which every approximate estimator is measured. It has zero approximation
// error by construction and O(n) memory.
type Exact struct {
	root  *treapNode
	rng   *rand.Rand
	n     int
	nodes int // distinct values retained, for SizeBytes
}

// NewExact returns an empty exact estimator. seed makes the internal treap
// priorities deterministic so tests are reproducible; balance is unaffected by
// the choice of seed.
func NewExact(seed int64) *Exact {
	return &Exact{rng: rand.New(rand.NewSource(seed))}
}

// Name implements Sketch.
func (e *Exact) Name() string { return "Exact(treap)" }

// Count implements Sketch.
func (e *Exact) Count() int { return e.n }

// Add ingests x in expected O(log n) time.
func (e *Exact) Add(x float64) {
	if math.IsNaN(x) {
		return
	}
	e.root = e.insert(e.root, x)
	e.n++
}

func size(t *treapNode) int64 {
	if t == nil {
		return 0
	}
	return t.size
}

func (t *treapNode) update() {
	t.size = t.cnt + size(t.left) + size(t.right)
}

// rotateRight/rotateLeft are the standard treap rotations that restore the
// max-heap property on priority while preserving BST order on value.
func rotateRight(y *treapNode) *treapNode {
	x := y.left
	y.left = x.right
	x.right = y
	y.update()
	x.update()
	return x
}

func rotateLeft(x *treapNode) *treapNode {
	y := x.right
	x.right = y.left
	y.left = x
	x.update()
	y.update()
	return y
}

func (e *Exact) insert(t *treapNode, x float64) *treapNode {
	if t == nil {
		e.nodes++
		return &treapNode{value: x, priority: e.rng.Uint64(), cnt: 1, size: 1}
	}
	switch {
	case x == t.value:
		t.cnt++
		t.size++
		return t
	case x < t.value:
		t.left = e.insert(t.left, x)
		if t.left.priority > t.priority {
			t = rotateRight(t)
		} else {
			t.update()
		}
	default:
		t.right = e.insert(t.right, x)
		if t.right.priority > t.priority {
			t = rotateLeft(t)
		} else {
			t.update()
		}
	}
	return t
}

// selectRank returns the value at 0-based rank r (0 <= r < n) in sorted order,
// walking the subtree-size annotations in O(height) = expected O(log n).
func (e *Exact) selectRank(r int64) float64 {
	t := e.root
	for t != nil {
		leftSize := size(t.left)
		switch {
		case r < leftSize:
			t = t.left
		case r < leftSize+t.cnt:
			return t.value
		default:
			r -= leftSize + t.cnt
			t = t.right
		}
	}
	return math.NaN()
}

// Quantile returns the exact nearest-rank q-quantile with zero error.
func (e *Exact) Quantile(q float64) float64 {
	if e.n == 0 {
		return math.NaN()
	}
	if q <= 0 {
		return e.selectRank(0)
	}
	if q >= 1 {
		return e.selectRank(int64(e.n) - 1)
	}
	rank := int64(math.Ceil(q*float64(e.n))) - 1 // 0-based
	if rank < 0 {
		rank = 0
	}
	if rank > int64(e.n)-1 {
		rank = int64(e.n) - 1
	}
	return e.selectRank(rank)
}

// SizeBytes reports the retained payload: one treapNode per distinct value.
// A node is value(8) + priority(8) + cnt(8) + size(8) + two pointers(16) = 48
// bytes on a 64-bit target. This is a real O(distinct) figure, deliberately not
// flattered — it is the price of exactness.
func (e *Exact) SizeBytes() int {
	const nodeBytes = 48
	return e.nodes * nodeBytes
}

// Rank returns the exact number of ingested observations strictly less than x.
// Exposed for tests that verify the treap's order statistics directly.
func (e *Exact) Rank(x float64) int64 {
	var r int64
	t := e.root
	for t != nil {
		if x <= t.value {
			t = t.left
		} else {
			r += size(t.left) + t.cnt
			t = t.right
		}
	}
	return r
}
