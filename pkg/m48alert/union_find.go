package m48alert

import "sync"

// UnionFind implements disjoint-set with path compression and union by rank.
// Supports O(α(n)) incremental merges where α is inverse Ackermann function.
type UnionFind struct {
	parent map[int]int
	rank   map[int]int
	mu     sync.RWMutex
}

// NewUnionFind creates a fresh union-find structure.
func NewUnionFind() *UnionFind {
	return &UnionFind{
		parent: make(map[int]int),
		rank:   make(map[int]int),
	}
}

// MakeRoot ensures index exists.
func (uf *UnionFind) ensureIndex(i int) {
	if _, exists := uf.parent[i]; !exists {
		uf.parent[i] = i
		uf.rank[i] = 0
	}
}

// Find returns representative of set containing element i.
// Uses path compression: O(α(n)) amortized. Caller must hold lock.
func (uf *UnionFind) findLocked(i int) int {
	uf.ensureIndex(i)
	if uf.parent[i] != i {
		uf.parent[i] = uf.findLocked(uf.parent[i])
	}
	return uf.parent[i]
}

// Find returns representative of set containing element i.
// Uses path compression: O(α(n)) amortized.
func (uf *UnionFind) Find(i int) int {
	uf.mu.Lock()
	defer uf.mu.Unlock()
	return uf.findLocked(i)
}

// Union merges sets containing elements i and j.
// Returns true if merged, false if already in same set.
func (uf *UnionFind) Union(i, j int) bool {
	uf.mu.Lock()
	defer uf.mu.Unlock()

	rootI := uf.findLocked(i)
	rootJ := uf.findLocked(j)

	if rootI == rootJ {
		return false
	}

	// Union by rank
	if uf.rank[rootI] < uf.rank[rootJ] {
		uf.parent[rootI] = rootJ
	} else if uf.rank[rootI] > uf.rank[rootJ] {
		uf.parent[rootJ] = rootI
	} else {
		uf.parent[rootJ] = rootI
		uf.rank[rootI]++
	}

	return true
}

// GetGroups returns current partitioning as list of groups.
func (uf *UnionFind) GetGroups() [][]int {
	// Acquire exclusive lock (simpler than trying to nest)
	uf.mu.Lock()
	defer uf.mu.Unlock()

	groups := make(map[int][]int)
	for i := range uf.parent {
		root := uf.findLocked(i)
		groups[root] = append(groups[root], i)
	}

	result := make([][]int, 0, len(groups))
	for _, group := range groups {
		result = append(result, group)
	}

	return result
}

// Reset clears all state.
func (uf *UnionFind) Reset() {
	uf.mu.Lock()
	defer uf.mu.Unlock()
	uf.parent = make(map[int]int)
	uf.rank = make(map[int]int)
}
