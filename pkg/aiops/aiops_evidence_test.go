package aiops

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func aiopsLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x3c}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func findReceipt(all []*evidence.Evidence, action, subject string) *evidence.Evidence {
	for _, e := range all {
		if e.Action == action && e.Subject == subject {
			return e
		}
	}
	return nil
}

func assertHonestMethod(t *testing.T, rec *evidence.Evidence, wantMethod, comp, driver string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["method"] != wantMethod {
		t.Fatalf("method must be honestly labeled %q, got %v", wantMethod, payload["method"])
	}
	if payload["applied"] != false {
		t.Fatalf("advisory decision must record applied=false, got %v", payload["applied"])
	}
	var ok bool
	for _, b := range rec.Backends {
		if b.Component == comp && b.Mode == "real" && b.Driver == driver {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("receipt must carry backend %s/real/%s, got %+v", comp, driver, rec.Backends)
	}
}

func TestAutoscale_EmitsVerifiableHonestEvidence(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := aiopsLedger(t)
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	a.SetEvidenceRecorder(ledger)
	a.RegisterWorkload(&ScalableWorkload{
		ID: "wl-1", Name: "test", CurrentReplicas: 3, MinReplicas: 1, MaxReplicas: 50,
		TargetMetrics: []TargetMetric{{Name: "cpu", TargetValue: 50, Weight: 1.0}},
	})
	now := time.Now()
	for i := 0; i < 10; i++ {
		a.IngestMetrics([]MetricSample{
			{Timestamp: now.Add(-time.Duration(i) * 10 * time.Second), WorkloadID: "wl-1", MetricName: "cpu", Value: 90, Replicas: 3},
		})
	}
	if _, err := a.Evaluate(context.Background(), "wl-1"); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	all, _ := ledger.Store().All(context.Background())
	rec := findReceipt(all, "aiops.autoscale", "wl-1")
	if rec == nil {
		t.Fatal("expected an aiops.autoscale receipt")
	}
	assertHonestMethod(t, rec, "heuristic", "aiops.autoscale", "heuristic-ema")
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("autoscale evidence chain must verify: %+v", rep)
	}
}

func TestSelfHeal_EmitsVerifiableHonestEvidence(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := aiopsLedger(t)
	engine := NewSelfHealingEngine(SelfHealConfig{}, nil)
	engine.SetEvidenceRecorder(ledger)

	engine.AnalyzeRootCause(&Incident{ID: "inc-1", Category: "pod"})

	all, _ := ledger.Store().All(context.Background())
	rec := findReceipt(all, "aiops.selfheal", "inc-1")
	if rec == nil {
		t.Fatal("expected an aiops.selfheal receipt")
	}
	assertHonestMethod(t, rec, "heuristic-rules", "aiops.selfheal", "heuristic-rules")
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("self-heal evidence chain must verify: %+v", rep)
	}
}
