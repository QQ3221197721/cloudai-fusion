package delivery

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// failover.go is DL-2 (docs/verifiable-moat-spec.md §5.3): a signed, offline-
// verifiable record of a cross-cluster failover that proves measured RTO/RPO
// against their SLO targets and that EXACTLY ONE promotion won (no split-brain).
// Versus "trust the restore ran" (Velero), it is the artifact a DORA/BCP auditor
// signs off on. It reuses the spine (evidence.SignStatement) - no new crypto.

const failoverDomain = "cloudai-fusion/failover-attestation/v1"

// ActionFailover tags a recorded FailoverAttestation receipt.
const ActionFailover = "delivery.failover"

// FailoverAttestation is the signed DR record.
type FailoverAttestation struct {
	ID             string  `json:"id"`
	Service        string  `json:"service"`
	FromCluster    string  `json:"from_cluster"`
	ToCluster      string  `json:"to_cluster"`
	Trigger        string  `json:"trigger"`
	Promotions     int     `json:"promotions"`       // must be 1 (no split-brain)
	RTOSeconds     float64 `json:"rto_seconds"`      // measured recovery time
	RPOSeconds     float64 `json:"rpo_seconds"`      // measured recovery point
	RTOTargetSec   float64 `json:"rto_target_sec"`   // SLO target
	RPOTargetSec   float64 `json:"rpo_target_sec"`   // SLO target
	HealthProbeRef string  `json:"health_probe_ref"` // reference to the probe evidence
	CreatedAt      string  `json:"created_at"`
	KeyID          string  `json:"key_id"`
	Signature      string  `json:"signature"`
}

// FailoverInput describes an observed failover to attest.
type FailoverInput struct {
	Service        string
	FromCluster    string
	ToCluster      string
	Trigger        string
	Promotions     int
	RTOSeconds     float64
	RPOSeconds     float64
	RTOTargetSec   float64
	RPOTargetSec   float64
	HealthProbeRef string
}

// BuildFailoverAttestation signs an observed failover.
func BuildFailoverAttestation(s evidence.Signer, in FailoverInput) (*FailoverAttestation, error) {
	if s == nil {
		return nil, fmt.Errorf("delivery: failover attestation requires a signer")
	}
	att := &FailoverAttestation{
		ID:             common.NewUUID(),
		Service:        in.Service,
		FromCluster:    in.FromCluster,
		ToCluster:      in.ToCluster,
		Trigger:        in.Trigger,
		Promotions:     in.Promotions,
		RTOSeconds:     in.RTOSeconds,
		RPOSeconds:     in.RPOSeconds,
		RTOTargetSec:   in.RTOTargetSec,
		RPOTargetSec:   in.RPOTargetSec,
		HealthProbeRef: in.HealthProbeRef,
		CreatedAt:      common.NowUTC().Format("2006-01-02T15:04:05Z07:00"),
		KeyID:          s.KeyID(),
	}
	att.Signature = ""
	sig, err := evidence.SignStatement(s, failoverDomain, *att)
	if err != nil {
		return nil, err
	}
	att.Signature = sig
	return att, nil
}

// VerifyFailoverAttestation checks the attestation's signature against a pinned key.
func VerifyFailoverAttestation(att *FailoverAttestation, pub ed25519.PublicKey) error {
	if att == nil {
		return fmt.Errorf("delivery: nil failover attestation")
	}
	unsigned := *att
	unsigned.Signature = ""
	if err := evidence.VerifyStatement(pub, failoverDomain, unsigned, att.Signature); err != nil {
		return fmt.Errorf("delivery: failover attestation signature: %w", err)
	}
	return nil
}

// MeetsSLO returns nil only when exactly one promotion won (no split-brain) and the
// measured RTO/RPO are within their targets. It is the BCP/DR gate a release or
// audit check runs after verifying the signature.
func (att *FailoverAttestation) MeetsSLO() error {
	if att.Promotions != 1 {
		return fmt.Errorf("delivery: split-brain - %d promotions won (must be exactly 1)", att.Promotions)
	}
	if att.RTOTargetSec > 0 && att.RTOSeconds > att.RTOTargetSec {
		return fmt.Errorf("delivery: RTO breach - %.1fs > target %.1fs", att.RTOSeconds, att.RTOTargetSec)
	}
	if att.RPOTargetSec > 0 && att.RPOSeconds > att.RPOTargetSec {
		return fmt.Errorf("delivery: RPO breach - %.1fs > target %.1fs", att.RPOSeconds, att.RPOTargetSec)
	}
	return nil
}

// RecordFailoverAttestation appends a signed attestation as a delivery.failover receipt.
func RecordFailoverAttestation(ctx context.Context, rec evidence.Recorder, att *FailoverAttestation) (*evidence.Evidence, error) {
	if rec == nil || att == nil {
		return nil, nil
	}
	return rec.Record(ctx, evidence.RecordInput{
		Actor:   "delivery",
		Action:  ActionFailover,
		Subject: att.Service,
		Input:   map[string]any{"service": att.Service, "from": att.FromCluster, "to": att.ToCluster},
		Output:  map[string]any{"rto_seconds": att.RTOSeconds, "rpo_seconds": att.RPOSeconds, "promotions": att.Promotions},
		Payload: att,
		Backends: []evidence.BackendFact{
			{Component: "delivery.failover", Mode: "real", Driver: "client-go-probes"},
		},
	})
}

// FailoverServiceKeyOf is the fabric KeyFunc for the failover well: the service a
// failover receipt covered, or "" for non-failover receipts. A fabric.Well with it
// can completeness-prove "every failover of service S is present, none hidden".
func FailoverServiceKeyOf(e *evidence.Evidence) string {
	if e == nil || e.Action != ActionFailover {
		return ""
	}
	var p struct {
		Service string `json:"service"`
	}
	if json.Unmarshal(e.Payload, &p) == nil {
		return p.Service
	}
	return ""
}
