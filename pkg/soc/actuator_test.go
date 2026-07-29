package soc

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// activeCount returns how many active mitigations of a given type the engine's
// recording actuator holds.
func activeCount(t *testing.T, eng *Engine, action ActionType) int {
	t.Helper()
	ra, ok := eng.Actuator().(*RecordingActuator)
	if !ok {
		t.Fatalf("expected default RecordingActuator")
	}
	n := 0
	for _, m := range ra.Active() {
		if m.Action == action {
			n++
		}
	}
	return n
}

func TestRespond_AutomatedPlaybookActuates(t *testing.T) {
	t.Cleanup(capability.Reset)
	ctx := context.Background()
	store := intel.NewMemoryStore()
	_ = store.UpsertIOCs([]intel.IOCEntry{{IOCType: "ip", Value: "203.0.113.9", Severity: intel.SeverityHigh}})
	eng := NewEngine(store, nil)

	f, err := eng.AnalyzeNetwork(ctx, "node-1", []string{"203.0.113.9"}, nil)
	if err != nil || len(f) != 1 {
		t.Fatalf("seed finding: %v (%d)", err, len(f))
	}

	resp, err := eng.Respond(ctx, f[0].ID)
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if resp.Playbook != "c2-egress" || !resp.Executed {
		t.Fatalf("expected auto-executed c2-egress, got %+v", resp)
	}
	// The automated actions must have been actuated (executed steps recorded).
	if len(resp.Actuations) == 0 {
		t.Fatalf("automated response must produce actuations")
	}
	for _, a := range resp.Actuations {
		if !a.Executed {
			t.Fatalf("actuation not executed: %+v", a)
		}
	}
	// block-network is a lasting mitigation; it must now be active.
	if activeCount(t, eng, ActionBlockNetwork) != 1 {
		t.Fatalf("expected an active block-network mitigation")
	}
	// notify creates no lasting mitigation.
	if activeCount(t, eng, ActionNotify) != 0 {
		t.Fatalf("notify must not create a lasting mitigation")
	}
}

func TestRespond_ApprovalRequiredDoesNotActuateBlocking(t *testing.T) {
	t.Cleanup(capability.Reset)
	eng := NewEngine(intel.NewMemoryStore(), nil)
	// Inject an account-takeover finding (T1078) directly; its playbook requires
	// approval, so blocking actions must NOT auto-actuate.
	f := newFinding(WellIdentity, "T1078", "alice", "impossible travel", intel.SeverityCritical, nil)
	eng.store.Add(f)

	resp, err := eng.Respond(context.Background(), f.ID)
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if resp.Playbook != "account-takeover" || resp.Executed {
		t.Fatalf("account-takeover must require approval (not executed): %+v", resp)
	}
	// No revoke-credential / isolate-host mitigation may be active (approval gate).
	if activeCount(t, eng, ActionRevokeCredential) != 0 || activeCount(t, eng, ActionIsolateHost) != 0 {
		t.Fatalf("approval-required playbook must not auto-actuate blocking actions")
	}
}

// stubRealActuator lets us assert the real-vs-simulated reporting path.
type stubRealActuator struct{ acted int }

func (*stubRealActuator) Name() string { return "stub-real" }
func (*stubRealActuator) IsReal() bool { return true }
func (s *stubRealActuator) Actuate(_ context.Context, action ActionType, target string) ActuationResult {
	s.acted++
	return ActuationResult{Action: action, Target: target, Mode: "real", Executed: true}
}

func TestSetActuator_RealBackendUsed(t *testing.T) {
	t.Cleanup(capability.Reset)
	ctx := context.Background()
	store := intel.NewMemoryStore()
	_ = store.UpsertIOCs([]intel.IOCEntry{{IOCType: "ip", Value: "203.0.113.9", Severity: intel.SeverityHigh}})
	eng := NewEngine(store, nil)
	real := &stubRealActuator{}
	eng.SetActuator(real)

	f, _ := eng.AnalyzeNetwork(ctx, "n", []string{"203.0.113.9"}, nil)
	resp, err := eng.Respond(ctx, f[0].ID)
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if real.acted == 0 {
		t.Fatalf("injected real actuator must be used")
	}
	sawReal := false
	for _, a := range resp.Actuations {
		if a.Mode == "real" {
			sawReal = true
		}
	}
	if !sawReal {
		t.Fatalf("actuations must report real mode when a real actuator is wired")
	}
}
