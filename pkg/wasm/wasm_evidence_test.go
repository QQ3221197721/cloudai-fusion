package wasm

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TestWasm_DeployEmitsVerifiableEvidence proves an in-memory Wasm deploy emits a
// signed receipt honestly tagged simulated (no real Spin/containerd endpoint).
func TestWasm_DeployEmitsVerifiableEvidence(t *testing.T) {
	t.Cleanup(capability.Reset)
	signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x72}, 32))
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})

	mgr, _ := NewManager(Config{})
	mgr.SetEvidenceRecorder(ledger)

	if _, err := mgr.Deploy(context.Background(), &WasmDeployRequest{ModuleID: "mod-1", ClusterID: "c1", Replicas: 2}); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	all, _ := ledger.Store().All(context.Background())
	var rec *evidence.Evidence
	for _, e := range all {
		if e.Action == "wasm.deploy" && e.Subject == "mod-1" {
			rec = e
		}
	}
	if rec == nil {
		t.Fatal("expected a wasm.deploy receipt")
	}
	var ok bool
	for _, b := range rec.Backends {
		if b.Component == "wasm.runtime" && b.Mode == "simulated" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("wasm receipt must honestly tag simulated runtime, got %+v", rec.Backends)
	}
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("wasm evidence chain must verify: %+v", rep)
	}
}
