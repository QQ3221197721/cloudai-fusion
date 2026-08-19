package deltasync

import "crypto/sha256"

// MerkleTree is a binary hash tree over an ordered list of chunk IDs. Unpaired
// nodes at a level are promoted unchanged to the next level (rather than
// duplicated) to avoid the classic duplicate-leaf second-preimage ambiguity.
type MerkleTree struct {
	levels [][][32]byte // levels[0] = leaves, last level = single root
}

// internalHash domain-separates internal nodes (prefix 0x01) from leaves so an
// attacker cannot pass an internal digest off as a leaf.
func internalHash(l, r [32]byte) [32]byte {
	buf := make([]byte, 1+32+32)
	buf[0] = 0x01
	copy(buf[1:], l[:])
	copy(buf[33:], r[:])
	return sha256.Sum256(buf)
}

// BuildMerkleTree constructs a tree from ordered leaf hashes (chunk IDs).
func BuildMerkleTree(leaves [][32]byte) (*MerkleTree, error) {
	if len(leaves) == 0 {
		return nil, ErrEmptyTree
	}
	level := make([][32]byte, len(leaves))
	copy(level, leaves)
	levels := [][][32]byte{level}
	for len(level) > 1 {
		next := make([][32]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				next = append(next, internalHash(level[i], level[i+1]))
			} else {
				next = append(next, level[i]) // promote unpaired node
			}
		}
		levels = append(levels, next)
		level = next
	}
	return &MerkleTree{levels: levels}, nil
}

// MerkleTreeFromChunks builds a tree from a chunk list using each chunk's ID.
func MerkleTreeFromChunks(chunks []Chunk) (*MerkleTree, error) {
	leaves := make([][32]byte, len(chunks))
	for i, c := range chunks {
		leaves[i] = c.ID
	}
	return BuildMerkleTree(leaves)
}

// Root returns the Merkle root digest.
func (t *MerkleTree) Root() [32]byte { return t.levels[len(t.levels)-1][0] }

// LeafCount returns the number of leaves.
func (t *MerkleTree) LeafCount() int { return len(t.levels[0]) }

// Height returns the number of levels above the leaves (root-to-leaf edges).
func (t *MerkleTree) Height() int { return len(t.levels) - 1 }

// DiffResult reports the outcome of a structural Merkle diff.
type DiffResult struct {
	ChangedLeaves []int `json:"changed_leaves"` // indices of differing leaves
	Comparisons   int   `json:"comparisons"`    // node-hash equality checks performed
	RoundTrips    int   `json:"round_trips"`    // network rounds in a level-synchronous protocol
}

// Diff locates the differing leaves between two equal-shaped trees in
// O(k·log n) comparisons (k = number of changed leaves) rather than O(n): each
// level compares only the children of nodes already known to differ. Identical
// subtrees are pruned in a single comparison, which is the O(log n)
// localization property.
//
// RoundTrips models a level-synchronous reconciliation protocol: each tree
// level that still contains a differing node costs one network round trip. For
// a balanced tree this is bounded by Height()+1 = O(log n) regardless of how
// many leaves changed.
func (t *MerkleTree) Diff(other *MerkleTree) (*DiffResult, error) {
	if t.LeafCount() != other.LeafCount() {
		return nil, ErrShapeMismatch
	}
	res := &DiffResult{}
	top := len(t.levels) - 1
	// Node indices (within their level) currently known to differ.
	diffNodes := []int{0}
	res.Comparisons++
	res.RoundTrips++ // root exchange
	if t.levels[top][0] == other.levels[top][0] {
		return res, nil // roots match => identical
	}
	// Descend level by level from just below the root to the leaves.
	for lvl := top - 1; lvl >= 0; lvl-- {
		var nextDiff []int
		levelHadDiff := false
		for _, parent := range diffNodes {
			for _, child := range []int{parent * 2, parent*2 + 1} {
				if child >= len(t.levels[lvl]) {
					continue
				}
				res.Comparisons++
				if t.levels[lvl][child] != other.levels[lvl][child] {
					levelHadDiff = true
					nextDiff = append(nextDiff, child)
				}
			}
		}
		if levelHadDiff {
			res.RoundTrips++
		}
		diffNodes = nextDiff
		if lvl == 0 {
			res.ChangedLeaves = append(res.ChangedLeaves, diffNodes...)
		}
	}
	return res, nil
}
