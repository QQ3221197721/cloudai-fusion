package gitops

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TestSyncEmitsVerifiableEvidence proves the Phase 6 pattern: a breadth
// subsystem (GitOps) emits a signed, verifiable receipt for its action and is
// honest about whether the sync ran on a real backend. Here no ArgoCD server is
// configured, so the sync is simulated and the receipt must say so.
func TestSyncEmitsVerifiableEvidence(t *testing.T) {
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x66}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	mgr := NewManager(DefaultManagerConfig())
	mgr.SetEvidenceRecorder(ledger)
	ctx := context.Background()

	app, _ := mgr.CreateApplication(ctx, &Application{
		Name: "evidence-sync", Environment: "dev", Engine: EngineFlux,
		RepoURL: "https://example.com/repo", TargetRevision: "v1.0.0",
	})
	if _, err := mgr.SyncApplication(ctx, app.ID, "abc123"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	all, _ := ledger.Store().All(ctx)
	var rec *evidence.Evidence
	for _, e := range all {
		if e.Action == "gitops.sync" && e.Subject == app.ID {
			rec = e
			break
		}
	}
	if rec == nil {
		t.Fatalf("expected a gitops.sync receipt for %s", app.ID)
	}

	// Honesty: with no ArgoCD configured, the receipt must flag the sync as
	// simulated (not a real backend) in its per-action backends.
	var sawSimulated bool
	for _, b := range rec.Backends {
		if b.Component == "gitops.sync" && b.Mode == "simulated" {
			sawSimulated = true
		}
	}
	if !sawSimulated {
		t.Fatalf("simulated sync must be reported as simulated, backends=%+v", rec.Backends)
	}

	// Payload must record which engine/revision and that it was not real.
	var payload map[string]any
	if err := json.Unmarshal(rec.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["sync_real"] != false {
		t.Fatalf("payload sync_real must be false for a simulated sync, got %v", payload["sync_real"])
	}

	// And it must verify cryptographically.
	rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey())
	if !rep.Valid {
		t.Fatalf("gitops evidence must verify, got %+v", rep)
	}
}
