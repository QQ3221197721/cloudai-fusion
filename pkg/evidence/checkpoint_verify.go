package evidence

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
)

// checkpoint_verify.go holds the third-party-facing verification of the Merkle
// artifacts: a signed checkpoint, an inclusion proof, and an append-only
// consistency proof. These have ZERO dependency on the running platform — an
// auditor recomputes everything from the published public key and the proof
// bytes, which is the whole point of a transparency log.

// VerifyCheckpoint checks a checkpoint's Ed25519 signature against pub.
func VerifyCheckpoint(cp *Checkpoint, pub ed25519.PublicKey) error {
	if cp == nil {
		return errors.New("evidence: nil checkpoint")
	}
	return VerifyLeaf(pub, checkpointSigningBytes(cp.TreeSize, cp.RootHash), cp.Signature)
}

// MerkleRootHexOfRecords recomputes the RFC 6962 Merkle root over records, which
// must be in ascending Seq order (Store.All / bundle order).
func MerkleRootHexOfRecords(records []*Evidence) (string, error) {
	leaves, err := leafHashesFromRecords(records)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(merkleRoot(leaves)), nil
}

// VerifyInclusionResponse verifies (1) the checkpoint signature and (2) that the
// audit path + leaf hash reconstruct the checkpoint's signed root. On success the
// caller has cryptographic proof the receipt is committed by that checkpoint.
func VerifyInclusionResponse(resp *InclusionProofResponse, pub ed25519.PublicKey) error {
	if resp == nil || resp.Checkpoint == nil {
		return errors.New("evidence: nil inclusion proof or checkpoint")
	}
	if err := VerifyCheckpoint(resp.Checkpoint, pub); err != nil {
		return fmt.Errorf("evidence: checkpoint signature: %w", err)
	}
	if resp.TreeSize != resp.Checkpoint.TreeSize {
		return fmt.Errorf("evidence: proof tree_size %d != checkpoint size %d", resp.TreeSize, resp.Checkpoint.TreeSize)
	}
	leaf, err := hex.DecodeString(resp.LeafHash)
	if err != nil {
		return fmt.Errorf("evidence: decode leaf hash: %w", err)
	}
	root, err := hex.DecodeString(resp.Checkpoint.RootHash)
	if err != nil {
		return fmt.Errorf("evidence: decode root: %w", err)
	}
	path, err := decodeHexNodes(resp.AuditPath)
	if err != nil {
		return err
	}
	if !verifyInclusion(resp.LeafIndex, resp.TreeSize, leaf, path, root) {
		return errors.New("evidence: inclusion proof does not reconstruct the signed root")
	}
	return nil
}

// VerifyConsistencyResponse verifies the checkpoint signature and that the proof
// establishes the size-First tree is an append-only prefix of the size-Second
// tree (no historical receipt altered or removed).
func VerifyConsistencyResponse(resp *ConsistencyProofResponse, pub ed25519.PublicKey) error {
	if resp == nil || resp.Checkpoint == nil {
		return errors.New("evidence: nil consistency proof or checkpoint")
	}
	if err := VerifyCheckpoint(resp.Checkpoint, pub); err != nil {
		return fmt.Errorf("evidence: checkpoint signature: %w", err)
	}
	// The checkpoint must commit to exactly the second (larger) tree.
	if resp.Checkpoint.TreeSize != resp.Second || resp.Checkpoint.RootHash != resp.SecondRoot {
		return errors.New("evidence: checkpoint does not match the second tree of the proof")
	}
	r1, err := hex.DecodeString(resp.FirstRoot)
	if err != nil {
		return fmt.Errorf("evidence: decode first root: %w", err)
	}
	r2, err := hex.DecodeString(resp.SecondRoot)
	if err != nil {
		return fmt.Errorf("evidence: decode second root: %w", err)
	}
	nodes, err := decodeHexNodes(resp.Proof)
	if err != nil {
		return err
	}
	if !verifyConsistency(resp.First, resp.Second, nodes, r1, r2) {
		return errors.New("evidence: consistency proof failed (log is not an append-only extension)")
	}
	return nil
}

// decodeHexNodes decodes a list of hex proof nodes.
func decodeHexNodes(hexes []string) ([][]byte, error) {
	out := make([][]byte, len(hexes))
	for i, h := range hexes {
		b, err := hex.DecodeString(h)
		if err != nil {
			return nil, fmt.Errorf("evidence: decode proof node %d: %w", i, err)
		}
		out[i] = b
	}
	return out, nil
}
