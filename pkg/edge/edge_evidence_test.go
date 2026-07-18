package edge

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TestEdge_RegisterNodeEmitsVerifiableEvidence proves edge node registration
// emits a signed receipt honestly tagged simulated (registration/persistence is
// real, but no live edge-device runtime link is verified).
func TestEdge_RegisterNodeEmitsVerifiableEvidence(t *testing.T) {
	t.Cleanup(capability.Reset)
	signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x73}, 32))
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})

	mgr, _ := NewManager(Config{})
	mgr.SetEvidenceRecorder(ledger)

	node := &EdgeNode{Name: "edge-1", Tier: TierEdge, Region: "cn-east"}
	if err := mgr.RegisterNode(context.Background(), node); err != nil {
		t.Fatalf("register node: %v", err)
	}

	all, _ := ledger.Store().All(context.Background())
	var rec *evidence.Evidence
	for _, e := range all {
		if e.Action == "edge.node.register" {
			rec = e
		}
	}
	if rec == nil {
		t.Fatal("expected an edge.node.register receipt")
	}
	var ok bool
	for _, b := range rec.Backends {
		if b.Component == "edge.runtime" && b.Mode == "simulated" && b.Driver == "rest-stub" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("edge receipt must honestly tag simulated runtime, got %+v", rec.Backends)
	}
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("edge evidence chain must verify: %+v", rep)
	}
}
