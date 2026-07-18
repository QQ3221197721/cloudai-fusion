package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// makeLeaves builds n distinct RFC 6962 leaf hashes for property tests.
func makeLeaves(n int) [][]byte {
	leaves := make([][]byte, n)
	for i := 0; i < n; i++ {
		leaves[i] = mLeafHash([]byte{byte(i), byte(i >> 8)})
	}
	return leaves
}

func TestMerkleRoot_EmptyTreeKnownAnswer(t *testing.T) {
	// RFC 6962: the empty tree hashes to SHA-256 of the empty string.
	got := hex.EncodeToString(merkleRoot(nil))
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Fatalf("empty root = %s, want %s", got, want)
	}
}

func TestMerkleRoot_SmallTreesStructure(t *testing.T) {
	l := makeLeaves(2)
	// Two-leaf root must be node(leaf0, leaf1) with domain separation.
	want := mNodeHash(l[0], l[1])
	if !bytes.Equal(merkleRoot(l), want) {
		t.Fatal("2-leaf root does not match node(leaf0,leaf1)")
	}
	// Single-leaf root is the leaf hash itself.
	if !bytes.Equal(merkleRoot(l[:1]), l[0]) {
		t.Fatal("1-leaf root must equal the leaf hash")
	}
	// Leaf and node hashing must be domain-separated (different prefixes).
	if bytes.Equal(mLeafHash([]byte("x")), func() []byte { s := sha256.Sum256([]byte("x")); return s[:] }()) {
		t.Fatal("leaf hash must be domain-separated from a plain sha256")
	}
}

func TestInclusionProof_GenerateAndVerifyAllPositions(t *testing.T) {
	for n := 1; n <= 33; n++ {
		leaves := makeLeaves(n)
		root := merkleRoot(leaves)
		for idx := 0; idx < n; idx++ {
			proof := inclusionProof(idx, leaves)
			if !verifyInclusion(idx, n, leaves[idx], proof, root) {
				t.Fatalf("inclusion proof failed for idx=%d size=%d", idx, n)
			}
			// A wrong leaf hash must NOT verify.
			bad := append([]byte(nil), leaves[idx]...)
			bad[0] ^= 0xff
			if verifyInclusion(idx, n, bad, proof, root) {
				t.Fatalf("tampered leaf unexpectedly verified idx=%d size=%d", idx, n)
			}
			// A wrong root must NOT verify.
			badRoot := append([]byte(nil), root...)
			badRoot[0] ^= 0xff
			if verifyInclusion(idx, n, leaves[idx], proof, badRoot) {
				t.Fatalf("wrong root unexpectedly verified idx=%d size=%d", idx, n)
			}
		}
	}
}

func TestConsistencyProof_GenerateAndVerifyAllPrefixes(t *testing.T) {
	for n := 2; n <= 33; n++ {
		leaves := makeLeaves(n)
		fullRoot := merkleRoot(leaves)
		for m := 1; m < n; m++ {
			prefixRoot := merkleRoot(leaves[:m])
			proof := consistencyProof(m, leaves)
			if !verifyConsistency(m, n, proof, prefixRoot, fullRoot) {
				t.Fatalf("consistency proof failed m=%d n=%d", m, n)
			}
			// A forged "old root" must break consistency: this is what proves the
			// log is append-only and history was not rewritten.
			forged := append([]byte(nil), prefixRoot...)
			forged[0] ^= 0xff
			if verifyConsistency(m, n, proof, forged, fullRoot) {
				t.Fatalf("consistency verified against a forged old root m=%d n=%d", m, n)
			}
		}
	}
}

func TestConsistencyProof_EqualAndEmptyEdges(t *testing.T) {
	leaves := makeLeaves(5)
	root := merkleRoot(leaves)
	// Equal sizes: empty proof, roots must match.
	if !verifyConsistency(5, 5, nil, root, root) {
		t.Fatal("equal-size consistency with matching roots must hold")
	}
	if verifyConsistency(5, 5, nil, root, makeLeaves(1)[0]) {
		t.Fatal("equal-size consistency with different roots must fail")
	}
	// Empty prefix is consistent with anything, no proof nodes.
	if !verifyConsistency(0, 5, nil, merkleRoot(nil), root) {
		t.Fatal("empty prefix must be consistent with any tree")
	}
}
