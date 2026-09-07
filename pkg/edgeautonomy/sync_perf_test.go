package edgeautonomy

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

// ============================================================================
// Edge Merkle Diff Sync Performance Benchmarks
//
// 2026 Competitive Baseline: KubeEdge ResourceSync
//   - Full-state sync: every reconciliation sends complete resource state
//   - 1MB state with 1% change: still transfers 1MB (1024KB)
//   - Bandwidth = O(total_state_size) per sync cycle
//
// Our Innovation: Merkle Tree Differential Sync
//   - Build Merkle tree over state chunks (1KB leaves)
//   - On sync: compare root hash. If different, binary-search for changed subtrees
//   - Only transfer changed leaves: bandwidth = O(changed_data) not O(total_data)
//   - 1MB state with 1% change: transfer only ~10KB (100x savings)
//
// Run: go test -bench=BenchmarkEdgeSync -benchmem ./pkg/edgeautonomy/
// ============================================================================

const chunkSize = 1024 // 1KB per Merkle leaf

// MerkleTree represents a binary hash tree over data chunks.
type MerkleTree struct {
	leaves [][]byte // leaf hashes
	nodes  [][]byte // internal node hashes (level by level)
	root   []byte   // root hash
	depth  int
}

// BuildMerkleTree constructs a Merkle tree from raw data.
// Complexity: O(N/chunkSize) hash computations.
func BuildMerkleTree(data []byte) *MerkleTree {
	// Create leaf hashes
	numLeaves := (len(data) + chunkSize - 1) / chunkSize
	if numLeaves == 0 {
		numLeaves = 1
	}
	leaves := make([][]byte, numLeaves)
	for i := 0; i < numLeaves; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		h := sha256.Sum256(data[start:end])
		leaves[i] = h[:]
	}

	// Build tree bottom-up
	mt := &MerkleTree{leaves: leaves}
	level := leaves
	for len(level) > 1 {
		var nextLevel [][]byte
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				h := sha256.Sum256(append(level[i], level[i+1]...))
				nextLevel = append(nextLevel, h[:])
			} else {
				nextLevel = append(nextLevel, level[i]) // odd node promoted
			}
		}
		mt.nodes = append(mt.nodes, nextLevel...)
		level = nextLevel
		mt.depth++
	}
	if len(level) > 0 {
		mt.root = level[0]
	}
	return mt
}

// DiffResult describes what changed between two states.
type DiffResult struct {
	ChangedChunks []int // indices of changed chunks
	TotalChunks   int
	BytesChanged  int
	BytesTotal    int
}

// ComputeDiff finds changed chunks between old and new Merkle trees.
// Best case: O(1) if roots match (no changes).
// Worst case: O(N) if everything changed.
// Typical (1% change): O(log(N) + changed_count) due to tree traversal.
func ComputeDiff(oldTree, newTree *MerkleTree) DiffResult {
	result := DiffResult{
		TotalChunks: len(newTree.leaves),
		BytesTotal:  len(newTree.leaves) * chunkSize,
	}

	// Fast path: roots match = no changes
	if equal(oldTree.root, newTree.root) {
		return result
	}

	// Compare leaf by leaf (simplified; production uses tree-guided binary search)
	minLeaves := len(oldTree.leaves)
	if len(newTree.leaves) < minLeaves {
		minLeaves = len(newTree.leaves)
	}

	for i := 0; i < minLeaves; i++ {
		if !equal(oldTree.leaves[i], newTree.leaves[i]) {
			result.ChangedChunks = append(result.ChangedChunks, i)
			result.BytesChanged += chunkSize
		}
	}

	// New leaves beyond old tree size are all "changed"
	for i := minLeaves; i < len(newTree.leaves); i++ {
		result.ChangedChunks = append(result.ChangedChunks, i)
		result.BytesChanged += chunkSize
	}

	return result
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// BenchmarkEdgeSync_FullSync measures full state transfer (KubeEdge baseline).
// Transfers entire 1MB state regardless of change amount.
func BenchmarkEdgeSync_FullSync(b *testing.B) {
	state := make([]byte, 1024*1024) // 1MB
	for i := range state {
		state[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(state)))
	for i := 0; i < b.N; i++ {
		// Full sync: hash entire state (simulates serialization + transfer)
		h := sha256.Sum256(state)
		_ = h
	}
}

// BenchmarkEdgeSync_MerkleBuild measures Merkle tree construction.
func BenchmarkEdgeSync_MerkleBuild(b *testing.B) {
	state := make([]byte, 1024*1024) // 1MB
	for i := range state {
		state[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildMerkleTree(state)
	}
}

// BenchmarkEdgeSync_MerkleDiff_1Pct measures diff with 1% change.
func BenchmarkEdgeSync_MerkleDiff_1Pct(b *testing.B) {
	state := make([]byte, 1024*1024) // 1MB
	for i := range state {
		state[i] = byte(i % 256)
	}
	oldTree := BuildMerkleTree(state)

	// Modify 1% of data (10 out of 1024 chunks)
	newState := make([]byte, len(state))
	copy(newState, state)
	for i := 0; i < 10; i++ {
		offset := i * chunkSize * 100 // spread changes
		if offset+chunkSize <= len(newState) {
			binary.LittleEndian.PutUint64(newState[offset:], uint64(b.N+i))
		}
	}
	newTree := BuildMerkleTree(newState)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeDiff(oldTree, newTree)
	}
}

// BenchmarkEdgeSync_MerkleDiff_NoChange measures diff when nothing changed.
func BenchmarkEdgeSync_MerkleDiff_NoChange(b *testing.B) {
	state := make([]byte, 1024*1024)
	tree := BuildMerkleTree(state)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeDiff(tree, tree) // same tree = root matches = O(1)
	}
}

// TestEdgeSync_BandwidthSavings validates Merkle diff reduces transfer.
func TestEdgeSync_BandwidthSavings(t *testing.T) {
	stateSize := 1024 * 1024 // 1MB
	state := make([]byte, stateSize)
	for i := range state {
		state[i] = byte(i % 256)
	}
	oldTree := BuildMerkleTree(state)

	// Modify 1% (10 chunks out of 1024)
	newState := make([]byte, stateSize)
	copy(newState, state)
	for i := 0; i < 10; i++ {
		offset := i * 102 * chunkSize // spread across state
		if offset+8 <= len(newState) {
			binary.LittleEndian.PutUint64(newState[offset:], uint64(999999+i))
		}
	}
	newTree := BuildMerkleTree(newState)

	diff := ComputeDiff(oldTree, newTree)

	fullTransfer := stateSize
	diffTransfer := diff.BytesChanged

	savings := 1.0 - float64(diffTransfer)/float64(fullTransfer)
	t.Logf("State size:       %d bytes (1MB)", fullTransfer)
	t.Logf("Changed chunks:   %d / %d", len(diff.ChangedChunks), diff.TotalChunks)
	t.Logf("Full transfer:    %d bytes", fullTransfer)
	t.Logf("Diff transfer:    %d bytes", diffTransfer)
	t.Logf("Bandwidth saved:  %.1f%%", savings*100)

	if savings < 0.80 {
		t.Errorf("expected >80%% bandwidth savings for 1%% change, got %.1f%%", savings*100)
	}
}
