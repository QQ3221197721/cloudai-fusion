package mesh

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TestMesh_PolicyEmitsVerifiableEvidence proves a policy apply emits a signed,
// verifiable receipt honestly tagged with the dataplane mode (in-memory when no
// real Cilium/K8s client is wired).
func TestMesh_PolicyEmitsVerifiableEvidence(t *testing.T) {
	t.Cleanup(capability.Reset)
	signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x71}, 32))
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})

	mgr, _ := NewManager(Config{})
	mgr.SetEvidenceRecorder(ledger)

	if err := mgr.CreatePolicy(context.Background(), &NetworkPolicy{Name: "test-policy"}); err != nil {
		t.Fatalf("create policy: %v", err)
	}

	all, _ := ledger.Store().All(context.Background())
	var rec *evidence.Evidence
	for _, e := range all {
		if e.Action == "mesh.policy.apply" && e.Subject == "test-policy" {
			rec = e
		}
	}
	if rec == nil {
		t.Fatal("expected a mesh.policy.apply receipt")
	}
	var ok bool
	for _, b := range rec.Backends {
		if b.Component == "mesh.dataplane" && b.Mode == "simulated" && b.Driver == "in-memory" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("mesh receipt must honestly tag in-memory dataplane, got %+v", rec.Backends)
	}
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("mesh evidence chain must verify: %+v", rep)
	}
}
