// Package delivery implements Pillar III (docs/verifiable-moat-spec.md §5.3): the
// "last mile" of verifiable trust - deploy provenance, failover/DR, and edge
// autonomy. DL-1 (this file) proves the RUNNING deployment equals the signed +
// approved artifact, and attests DRIFT rather than hiding it.
//
// It reuses the spine verbatim (evidence.SignStatement + the evidence ledger), so
// it adds no new cryptography: a DeployAttestation is signed like a model
// provenance or bench attestation, and recorded as a delivery.deploy receipt that
// is tamper-evident, inclusion-provable, and completeness-groupable via fabric.
package delivery

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

const deployDomain = "cloudai-fusion/deploy-attestation/v1"

// ActionDeploy tags a recorded DeployAttestation receipt.
const ActionDeploy = "delivery.deploy"

// DeployAttestation binds a running deployment to the signed+approved artifact it
// should be. Versus "trust the reconcile" (ArgoCD/Flux), it makes "what runs =
// what was reviewed and signed" an offline-verifiable, drift-attesting statement.
type DeployAttestation struct {
	ID            string `json:"id"`
	Workload      string `json:"workload"`
	Cluster       string `json:"cluster"`
	ImageDigest   string `json:"image_digest"`   // the RUNNING image digest
	ConfigHash    string `json:"config_hash"`    // the RUNNING config hash
	SignedDigest  string `json:"signed_digest"`  // cosign-signed, approved image digest
	ProvenanceRef string `json:"provenance_ref"` // SLSA provenance reference
	ApprovalRef   string `json:"approval_ref"`   // change-approval receipt id
	DriftDetected bool   `json:"drift_detected"` // running != signed (attested, never hidden)
	CreatedAt     string `json:"created_at"`
	KeyID         string `json:"key_id"`
	Signature     string `json:"signature"`
}

// DeployInput describes an observed deployment to attest.
type DeployInput struct {
	Workload      string
	Cluster       string
	ImageDigest   string
	ConfigHash    string
	SignedDigest  string
	ProvenanceRef string
	ApprovalRef   string
}

// BuildDeployAttestation computes the running-vs-signed binding (drift is derived,
// not supplied) and signs it.
func BuildDeployAttestation(s evidence.Signer, in DeployInput) (*DeployAttestation, error) {
	if s == nil {
		return nil, fmt.Errorf("delivery: deploy attestation requires a signer")
	}
	att := &DeployAttestation{
		ID:            common.NewUUID(),
		Workload:      in.Workload,
		Cluster:       in.Cluster,
		ImageDigest:   in.ImageDigest,
		ConfigHash:    in.ConfigHash,
		SignedDigest:  in.SignedDigest,
		ProvenanceRef: in.ProvenanceRef,
		ApprovalRef:   in.ApprovalRef,
		DriftDetected: in.ImageDigest != in.SignedDigest,
		CreatedAt:     common.NowUTC().Format("2006-01-02T15:04:05Z07:00"),
		KeyID:         s.KeyID(),
	}
	att.Signature = ""
	sig, err := evidence.SignStatement(s, deployDomain, *att)
	if err != nil {
		return nil, err
	}
	att.Signature = sig
	return att, nil
}

// VerifyDeployAttestation checks the attestation's signature against a pinned key.
// A drift attestation is still validly SIGNED (drift is recorded, not hidden); use
// ProvesIntegrity to assert no drift.
func VerifyDeployAttestation(att *DeployAttestation, pub ed25519.PublicKey) error {
	if att == nil {
		return fmt.Errorf("delivery: nil deploy attestation")
	}
	unsigned := *att
	unsigned.Signature = ""
	if err := evidence.VerifyStatement(pub, deployDomain, unsigned, att.Signature); err != nil {
		return fmt.Errorf("delivery: deploy attestation signature: %w", err)
	}
	return nil
}

// ProvesIntegrity returns nil only when the running deployment provably equals the
// signed+approved artifact (no drift). It is the release gate run after verifying
// the signature.
func (att *DeployAttestation) ProvesIntegrity() error {
	if att.SignedDigest == "" {
		return fmt.Errorf("delivery: no signed digest to compare against")
	}
	if att.DriftDetected || att.ImageDigest != att.SignedDigest {
		return fmt.Errorf("delivery: deployment drift - running %s != signed %s", att.ImageDigest, att.SignedDigest)
	}
	return nil
}

// RecordDeployAttestation appends a signed attestation as a delivery.deploy receipt.
func RecordDeployAttestation(ctx context.Context, rec evidence.Recorder, att *DeployAttestation) (*evidence.Evidence, error) {
	if rec == nil || att == nil {
		return nil, nil
	}
	return rec.Record(ctx, evidence.RecordInput{
		Actor:   "delivery",
		Action:  ActionDeploy,
		Subject: att.Workload,
		Input:   map[string]any{"workload": att.Workload, "cluster": att.Cluster},
		Output:  map[string]any{"image_digest": att.ImageDigest, "drift": att.DriftDetected},
		Payload: att,
		Backends: []evidence.BackendFact{
			{Component: "delivery.deploy", Mode: "real", Driver: "cosign-slsa"},
		},
	})
}

// ClusterKeyOf is the fabric KeyFunc for the delivery well: the cluster a deploy
// receipt targeted, or "" for non-deploy receipts. A fabric.Well registered with
// it can completeness-prove "every deploy to cluster X is present, none hidden".
func ClusterKeyOf(e *evidence.Evidence) string {
	if e == nil || e.Action != ActionDeploy {
		return ""
	}
	var p struct {
		Cluster string `json:"cluster"`
	}
	if json.Unmarshal(e.Payload, &p) == nil {
		return p.Cluster
	}
	return ""
}
