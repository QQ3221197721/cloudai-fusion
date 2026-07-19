package scheduler

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// isolation.go is the CN-3 well (docs/verifiable-moat-spec.md §5.1, §5.4b): a
// signed, offline-verifiable attestation that a MIG/MPS GPU co-placement preserved
// hardware isolation (no cross-tenant memory sharing). It is exactly the claim the
// red team (RT-1) attacks: attempt cross-tenant GPU-memory exfiltration, prove the
// claim holds (or a finding + remediation proof when it doesn't).
//
// Honesty (real-vs-simulated): the isolation check is a pluggable IsolationProbe.
// The real one measures via nvidia-smi MIG/MPS; the SimulatedIsolationProbe returns
// a scripted verdict and reports Real()==false, so an attestation never pretends a
// real probe ran. It reuses the spine (evidence.SignStatement) - no new crypto.

const isolationDomain = "cloudai-fusion/isolation-attestation/v1"

// ActionIsolation tags a recorded IsolationAttestation receipt.
const ActionIsolation = "schedule.isolation"

// IsolationAttestation is the signed hardware-isolation record for one GPU.
type IsolationAttestation struct {
	ID        string   `json:"id"`
	Node      string   `json:"node"`
	GPUIndex  int      `json:"gpu_index"`
	ShareMode string   `json:"share_mode"` // mig | mps | exclusive
	Tenants   []string `json:"tenants"`    // tenants co-placed on this GPU
	Isolated  bool     `json:"isolated"`   // probe verdict: no cross-tenant memory sharing
	ProbeReal bool     `json:"probe_real"` // was the probe a real nvidia-smi check?
	Backend   string   `json:"backend"`
	CreatedAt string   `json:"created_at"`
	KeyID     string   `json:"key_id"`
	Signature string   `json:"signature"`
}

// IsolationProbe measures whether a GPU co-placement preserves hardware isolation.
// Real backends query nvidia-smi MIG/MPS; the simulated one returns a scripted
// verdict (Real()==false).
type IsolationProbe interface {
	Probe(ctx context.Context, node string, gpu int, mode GPUShareMode, tenants []string) (isolated bool, err error)
	Real() bool
	Backend() string
}

// SimulatedIsolationProbe is the honest no-hardware probe: it returns a scripted
// verdict and reports Real()==false.
type SimulatedIsolationProbe struct{ Verdict bool }

// Probe returns the scripted verdict.
func (p SimulatedIsolationProbe) Probe(context.Context, string, int, GPUShareMode, []string) (bool, error) {
	return p.Verdict, nil
}

// Real reports false: no real hardware probe ran.
func (SimulatedIsolationProbe) Real() bool { return false }

// Backend identifies this probe as simulated.
func (SimulatedIsolationProbe) Backend() string { return "simulated" }

// BuildIsolationAttestation probes the co-placement and signs the resulting record.
func BuildIsolationAttestation(ctx context.Context, s evidence.Signer, probe IsolationProbe, node string, gpu int, mode GPUShareMode, tenants []string) (*IsolationAttestation, error) {
	if s == nil {
		return nil, fmt.Errorf("scheduler: isolation attestation requires a signer")
	}
	if probe == nil {
		return nil, fmt.Errorf("scheduler: isolation attestation requires a probe")
	}
	isolated, err := probe.Probe(ctx, node, gpu, mode, tenants)
	if err != nil {
		return nil, fmt.Errorf("scheduler: isolation probe failed: %w", err)
	}
	att := &IsolationAttestation{
		ID:        common.NewUUID(),
		Node:      node,
		GPUIndex:  gpu,
		ShareMode: string(mode),
		Tenants:   tenants,
		Isolated:  isolated,
		ProbeReal: probe.Real(),
		Backend:   probe.Backend(),
		CreatedAt: common.NowUTC().Format("2006-01-02T15:04:05Z07:00"),
		KeyID:     s.KeyID(),
	}
	att.Signature = ""
	sig, err := evidence.SignStatement(s, isolationDomain, *att)
	if err != nil {
		return nil, err
	}
	att.Signature = sig
	return att, nil
}

// VerifyIsolationAttestation checks the attestation's signature against a pinned key.
func VerifyIsolationAttestation(att *IsolationAttestation, pub ed25519.PublicKey) error {
	if att == nil {
		return fmt.Errorf("scheduler: nil isolation attestation")
	}
	unsigned := *att
	unsigned.Signature = ""
	if err := evidence.VerifyStatement(pub, isolationDomain, unsigned, att.Signature); err != nil {
		return fmt.Errorf("scheduler: isolation attestation signature: %w", err)
	}
	return nil
}

// ProvesIsolation returns nil only when the probe confirmed no cross-tenant memory
// sharing. It is the gate a multi-tenant SLA / red-team validation runs after
// verifying the signature.
func (att *IsolationAttestation) ProvesIsolation() error {
	if !att.Isolated {
		return fmt.Errorf("scheduler: GPU %s/%d (%s) is NOT isolated across tenants %v", att.Node, att.GPUIndex, att.ShareMode, att.Tenants)
	}
	return nil
}

// RecordIsolationAttestation appends a signed attestation as a schedule.isolation receipt.
func RecordIsolationAttestation(ctx context.Context, rec evidence.Recorder, att *IsolationAttestation) (*evidence.Evidence, error) {
	if rec == nil || att == nil {
		return nil, nil
	}
	mode := "simulated"
	if att.ProbeReal {
		mode = "real"
	}
	return rec.Record(ctx, evidence.RecordInput{
		Actor:   "scheduler",
		Action:  ActionIsolation,
		Subject: att.Node,
		Input:   map[string]any{"node": att.Node, "gpu": att.GPUIndex, "share_mode": att.ShareMode, "tenants": att.Tenants},
		Output:  map[string]any{"isolated": att.Isolated, "probe_real": att.ProbeReal},
		Payload: att,
		Backends: []evidence.BackendFact{
			{Component: "scheduler.isolation", Mode: mode, Driver: att.Backend},
		},
	})
}

// IsolationNodeKeyOf is the fabric KeyFunc for the isolation well: the node an
// isolation receipt covered, or "" for non-isolation receipts.
func IsolationNodeKeyOf(e *evidence.Evidence) string {
	if e == nil || e.Action != ActionIsolation {
		return ""
	}
	var p struct {
		Node string `json:"node"`
	}
	if json.Unmarshal(e.Payload, &p) == nil {
		return p.Node
	}
	return ""
}
