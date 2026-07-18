package evidence

import (
	"bytes"
	"crypto/sha256"
	"math/bits"
)

// merkle.go implements an RFC 6962 (Certificate Transparency) Merkle Tree Hash
// over the evidence leaves. This upgrades the ledger from a purely linear hash
// chain to a real transparency-log data structure — the SAME construction used
// by CT and Rekor — enabling three deeper guarantees:
//
//   - a compact signed checkpoint (tree size + root) committing to the WHOLE log,
//   - inclusion proofs: prove one receipt is in the log without revealing the log,
//   - consistency proofs: prove the log is APPEND-ONLY between two checkpoints
//     (history was never rewritten) — the strongest tamper-evidence guarantee.
//
// Hashing is domain-separated per RFC 6962: leaf = H(0x00 || data),
// node = H(0x01 || left || right). Proof GENERATION follows the recursive RFC
// definitions; proof VERIFICATION uses the equivalent iterative decomposition,
// so an independent verifier need not rebuild the tree.

const (
	rfc6962LeafPrefix = 0x00
	rfc6962NodePrefix = 0x01
)

// mLeafHash returns the RFC 6962 leaf hash H(0x00 || data).
func mLeafHash(data []byte) []byte {
	h := sha256.New()
	h.Write([]byte{rfc6962LeafPrefix})
	h.Write(data)
	return h.Sum(nil)
}

// mNodeHash returns the RFC 6962 interior node hash H(0x01 || left || right).
func mNodeHash(left, right []byte) []byte {
	h := sha256.New()
	h.Write([]byte{rfc6962NodePrefix})
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}

// merkleRoot computes the Merkle Tree Hash over the given leaf hashes.
// The empty tree hashes to SHA-256 of the empty string, per RFC 6962.
func merkleRoot(leaves [][]byte) []byte {
	n := len(leaves)
	if n == 0 {
		s := sha256.Sum256(nil)
		return s[:]
	}
	if n == 1 {
		return leaves[0]
	}
	k := largestPow2LessThan(n)
	return mNodeHash(merkleRoot(leaves[:k]), merkleRoot(leaves[k:]))
}

// largestPow2LessThan returns the largest power of two strictly less than n
// (n >= 2). This is the split point k with k < n <= 2k used throughout RFC 6962.
func largestPow2LessThan(n int) int {
	if n < 2 {
		return 0
	}
	k := 1
	for k<<1 < n {
		k <<= 1
	}
	return k
}

// inclusionProof returns the RFC 6962 audit path proving leaf m is included among
// the given leaf hashes. Returned hashes are ordered leaf-to-root sibling nodes.
func inclusionProof(m int, leaves [][]byte) [][]byte {
	n := len(leaves)
	if n == 1 {
		return [][]byte{}
	}
	k := largestPow2LessThan(n)
	if m < k {
		return append(inclusionProof(m, leaves[:k]), merkleRoot(leaves[k:]))
	}
	return append(inclusionProof(m-k, leaves[k:]), merkleRoot(leaves[:k]))
}

// consistencyProof returns the RFC 6962 proof that the size-m prefix of leaves is
// consistent with (an append-only ancestor of) the full size-n tree.
func consistencyProof(m int, leaves [][]byte) [][]byte {
	if m <= 0 || m >= len(leaves) {
		return [][]byte{}
	}
	return subproof(m, leaves, true)
}

// subproof implements RFC 6962 2.1.2 SUBPROOF.
func subproof(m int, leaves [][]byte, b bool) [][]byte {
	n := len(leaves)
	if m == n {
		if b {
			return [][]byte{}
		}
		return [][]byte{merkleRoot(leaves)}
	}
	k := largestPow2LessThan(n)
	if m <= k {
		return append(subproof(m, leaves[:k], b), merkleRoot(leaves[k:]))
	}
	return append(subproof(m-k, leaves[k:], false), merkleRoot(leaves[:k]))
}

// ============================================================================
// Verification (iterative; no tree rebuild required)
// ============================================================================

// verifyInclusion recomputes the root from a leaf hash + audit path and checks
// it equals root. index is the 0-based leaf position; size is the tree size.
func verifyInclusion(index, size int, leafHash []byte, proof [][]byte, root []byte) bool {
	if index < 0 || size < 0 || index >= size {
		return false
	}
	inner, border := decompInclProof(index, size)
	if len(proof) != inner+border {
		return false
	}
	res := chainInner(leafHash, proof[:inner], index)
	res = chainBorderRight(res, proof[inner:])
	return bytes.Equal(res, root)
}

// verifyConsistency checks that a size1 tree with root1 is an append-only prefix
// of a size2 tree with root2, given the consistency proof.
func verifyConsistency(size1, size2 int, proof [][]byte, root1, root2 []byte) bool {
	if size1 > size2 {
		return false
	}
	if size1 == size2 {
		return len(proof) == 0 && bytes.Equal(root1, root2)
	}
	if size1 == 0 {
		// Any tree is consistent with the empty tree; no proof nodes needed.
		return len(proof) == 0
	}

	inner, border := decompInclProof(size1-1, size2)
	shift := bits.TrailingZeros(uint(size1))
	inner -= shift

	seed := root1
	start := 0
	if size1 != 1<<uint(shift) {
		seed = proof[0]
		start = 1
	}
	if len(proof) != start+inner+border {
		return false
	}
	proof = proof[start:]

	mask := (size1 - 1) >> uint(shift)
	hash1 := chainInnerRight(seed, proof[:inner], mask)
	hash1 = chainBorderRight(hash1, proof[inner:])
	if !bytes.Equal(hash1, root1) {
		return false
	}
	hash2 := chainInner(seed, proof[:inner], mask)
	hash2 = chainBorderRight(hash2, proof[inner:])
	return bytes.Equal(hash2, root2)
}

// decompInclProof splits an inclusion/consistency proof into its inner (path to
// the top of the local subtree) and border (path along the right edge) sizes.
func decompInclProof(index, size int) (inner, border int) {
	inner = innerProofSize(index, size)
	border = bits.OnesCount(uint(index) >> uint(inner))
	return inner, border
}

func innerProofSize(index, size int) int {
	return bits.Len(uint(index ^ (size - 1)))
}

// chainInner folds proof nodes into seed for the inner portion, choosing sibling
// order by the bits of index (0 => sibling on the right, 1 => on the left).
func chainInner(seed []byte, proof [][]byte, index int) []byte {
	for i, p := range proof {
		if (index>>uint(i))&1 == 0 {
			seed = mNodeHash(seed, p)
		} else {
			seed = mNodeHash(p, seed)
		}
	}
	return seed
}

// chainInnerRight folds only the proof nodes that sit on the right edge (used by
// the size1-root reconstruction in a consistency proof).
func chainInnerRight(seed []byte, proof [][]byte, index int) []byte {
	for i, p := range proof {
		if (index>>uint(i))&1 == 1 {
			seed = mNodeHash(p, seed)
		}
	}
	return seed
}

// chainBorderRight folds the border proof nodes (always siblings on the left).
func chainBorderRight(seed []byte, proof [][]byte) []byte {
	for _, p := range proof {
		seed = mNodeHash(p, seed)
	}
	return seed
}
