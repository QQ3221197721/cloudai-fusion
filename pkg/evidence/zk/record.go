package zk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ActionZKAttest is the receipt action for a recorded zkEvidence attestation
// (spec §3.3). It is hash-chained and signed like any other receipt, so the
// attestation's public inputs become tamper-evident in the ledger while the
// succinct proof itself stays out-of-band (only its hash + VKID are committed).
const ActionZKAttest = "evidence.zk.attest"

// attestReceipt is the compact ledger payload for a zk attestation: enough to bind
// and re-locate the proof, never the receipts it was computed over.
type attestReceipt struct {
	Statement   string `json:"statement"`
	PublicRoot  string `json:"public_root"`
	ScopeCommit string `json:"scope_commit"`
	Count       int    `json:"count"`
	VKID        string `json:"vk_id"`
	ProofSHA256 string `json:"proof_sha256"`
	Mode        string `json:"mode"`
}

// RecordAttestation records an evidence.zk.attest receipt for namespace, binding
// the attestation into the verifiable chain. Returns the recorded receipt.
func RecordAttestation(ctx context.Context, l *evidence.Ledger, namespace string, att *ZKAttestation) (*evidence.Evidence, error) {
	sum := sha256.Sum256(att.Proof)
	payload := attestReceipt{
		Statement:   string(att.Statement),
		PublicRoot:  att.PublicRoot,
		ScopeCommit: att.ScopeCommit,
		Count:       att.Count,
		VKID:        att.VKID,
		ProofSHA256: hex.EncodeToString(sum[:]),
		Mode:        att.Mode,
	}
	return l.Record(ctx, evidence.RecordInput{
		Actor:   "evidence.zk",
		Action:  ActionZKAttest,
		Subject: namespace,
		Input:   map[string]any{"statement": string(att.Statement), "count": att.Count},
		Output:  map[string]any{"public_root": att.PublicRoot, "vk_id": att.VKID},
		Payload: payload,
		Backends: []evidence.BackendFact{
			{Component: CapabilityComponent, Mode: att.Mode, Driver: "groth16-bn254-poseidon2"},
		},
	})
}
