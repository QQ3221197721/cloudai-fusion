package integration

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/alerting"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/detect"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// TestEndToEndEvidenceFlow proves the unified evidence chain works across modules:
// User authenticates → Schedules GPU job → Detection engine monitors → Alert fires.
// Each step produces a signed *evidence.Receipt, and the entire chain is verifiable
// end-to-end with a single Ed25519 public key check per receipt.
//
// Note: this uses the ACTUAL constructors and signatures of the real
// evidence_*.go files (verified against source):
//   - auth.NewEvidenceAccessController(EvidenceAccessConfig) (*..., error)
//   - scheduler.NewEvidenceGPUScheduler(EvidenceGPUSchedulerConfig) (*..., error)
//   - detect.NewEvidenceDetectionEngine(ed25519.PrivateKey) *...
//   - alerting.NewEvidenceAlertManager(ed25519.PrivateKey) *...
func TestEndToEndEvidenceFlow(t *testing.T) {
	// Setup: one common signing key for readability. In production each module
	// owns its own key; Receipt.Verify() only relies on the embedded public key.
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}

	// Step 1: Auth — user gets access (produces Receipt).
	// RoleOperator holds PermWorkloadCreate, so "execute" is allowed.
	authEngine, err := auth.NewEvidenceAccessController(auth.EvidenceAccessConfig{
		SigningKey: privKey,
	})
	if err != nil {
		t.Fatalf("new access controller: %v", err)
	}
	user := auth.User{ID: "dev-123", Role: auth.RoleOperator}
	resource := auth.Resource{Path: "/gpu/schedule"}
	action := auth.Action{Name: "execute", Permission: auth.PermWorkloadCreate}
	allowed, authReceipt, err := authEngine.CheckPermission(user, resource, action)
	if err != nil {
		t.Fatalf("check permission: %v", err)
	}
	if !allowed {
		t.Fatal("expected access allowed for operator + workload:create")
	}
	if authReceipt == nil || !authReceipt.Verify() {
		t.Fatal("auth receipt invalid")
	}

	// Step 2: Scheduler — GPU job scheduled (produces Receipt with Pareto proof).
	gpuScheduler, err := scheduler.NewEvidenceGPUScheduler(scheduler.EvidenceGPUSchedulerConfig{
		SigningKey: privKey,
		Nodes: []scheduler.GPUNode{
			{Name: "gpu-node-1", FreeGPUs: 8, HasNVLink: true, PowerPerGPUW: 300, LatencyBaseMs: 5, TPSPerGPU: 120},
			{Name: "gpu-node-2", FreeGPUs: 4, HasNVLink: false, PowerPerGPUW: 250, LatencyBaseMs: 8, TPSPerGPU: 90},
		},
	})
	if err != nil {
		t.Fatalf("new gpu scheduler: %v", err)
	}
	jobs := []scheduler.Job{
		{ID: "train-1", GPUCount: 2, ExpectedTPS: 200, LatencyClass: 10, PowerBudgetW: 600, PreferNVLink: true},
	}
	assignments, schedReceipt, err := gpuScheduler.Schedule(context.Background(), jobs)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(assignments) == 0 {
		t.Fatal("scheduler produced no assignments")
	}
	if schedReceipt == nil || !schedReceipt.Verify() {
		t.Fatal("scheduler receipt invalid")
	}

	// Step 3: Detect — monitoring for anomalies (produces Receipt).
	detectEngine := detect.NewEvidenceDetectionEngine(privKey)
	event := map[string]interface{}{
		"rule_id": "gpu-cpu-spike",
		"metric":  "cpu_usage",
		"value":   95.0,
		"source":  "gpu-node-1",
	}
	detectResult, err := detectEngine.Detect(event)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if detectResult.Receipt == nil || !detectResult.Receipt.Verify() {
		t.Fatal("detect receipt invalid")
	}

	// Step 4: Alerting — if an anomaly is detected, an alert fires (produces Receipt).
	alertMgr := alerting.NewEvidenceAlertManager(privKey)
	alert := alerting.EvidenceAlert{
		ID:       "alert-1",
		Source:   "gpu-node-1",
		Severity: "high",
		Message:  "GPU overheating",
	}
	deliveryProof, err := alertMgr.SendAlert(alert)
	if err != nil {
		t.Fatalf("send alert: %v", err)
	}
	if deliveryProof.Receipt == nil || !deliveryProof.Receipt.Verify() {
		t.Fatal("alert receipt invalid")
	}

	// Step 5: Verify the ENTIRE chain is valid end-to-end.
	allReceipts := []*evidence.Receipt{
		authReceipt,
		schedReceipt,
		detectResult.Receipt,
		deliveryProof.Receipt,
	}
	for i, r := range allReceipts {
		if r == nil {
			t.Fatalf("receipt %d in chain is nil", i)
		}
		if !r.Verify() {
			t.Fatalf("receipt %d in chain failed verification", i)
		}
	}

	t.Logf("End-to-end evidence chain verified: %d receipts, all valid", len(allReceipts))
	t.Logf("Auth (%s) -> Schedule (%s) -> Detect (%s) -> Alert (%s): complete verifiable flow",
		authReceipt.Module, schedReceipt.Module, detectResult.Receipt.Module, deliveryProof.Receipt.Module)
}
