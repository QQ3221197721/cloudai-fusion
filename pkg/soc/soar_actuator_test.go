package soc

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// These tests pin the SOAR orchestrator's end-to-end honesty ladder: IsReal()
// must reflect the TRUE enforcement backend, never overclaim. They complement
// actuator_test.go (which drives Engine.Respond) by exercising the orchestrator
// directly through BindActuator.

// realStubActuator reports real enforcement and counts invocations.
type realStubActuator struct{ calls int }

func (*realStubActuator) Name() string { return "real-stub" }
func (*realStubActuator) IsReal() bool { return true }
func (s *realStubActuator) Actuate(_ context.Context, action ActionType, target string) ActuationResult {
	s.calls++
	return ActuationResult{Action: action, Target: target, Mode: "real", Executed: true}
}

// TestOrchestrator_IsReal_HonestyLadder walks the three truthful states:
// unbound -> false, RecordingActuator -> false, real backend -> true.
func TestOrchestrator_IsReal_HonestyLadder(t *testing.T) {
	o := NewOrchestrator(nil)

	// 1. No actuator bound yet: must be honest (not real).
	if o.IsReal() {
		t.Fatal("unbound orchestrator must report IsReal=false")
	}

	// 2. Default RecordingActuator: records intent only, still not real.
	o.BindActuator(NewRecordingActuator())
	if o.IsReal() {
		t.Fatal("RecordingActuator must report IsReal=false (records only)")
	}

	// 3. A real backend: now the orchestrator may truthfully claim real.
	o.BindActuator(&realStubActuator{})
	if !o.IsReal() {
		t.Fatal("real backend must make orchestrator report IsReal=true")
	}
}

// TestOrchestrator_C2Egress_ActionSequence verifies the exact automated action
// ordering for a C2 finding: block-network -> isolate-host -> notify, all
// automated (c2-egress does not require approval).
func TestOrchestrator_C2Egress_ActionSequence(t *testing.T) {
	o := NewOrchestrator(nil)
	f := newFinding(WellNetwork, "T1071", "host-9", "C2 beacon", intel.SeverityHigh, nil)

	resp := o.Respond(f)
	if resp.Playbook != "c2-egress" {
		t.Fatalf("playbook = %q, want c2-egress", resp.Playbook)
	}
	want := []ActionType{ActionBlockNetwork, ActionIsolateHost, ActionNotify}
	if len(resp.Actions) != len(want) {
		t.Fatalf("action count = %d, want %d (%+v)", len(resp.Actions), len(want), resp.Actions)
	}
	for i, a := range resp.Actions {
		if a.Type != want[i] {
			t.Fatalf("action[%d] = %q, want %q", i, a.Type, want[i])
		}
		if !a.Automated {
			t.Fatalf("c2-egress action %q must be automated", a.Type)
		}
		if a.Target != "host-9" {
			t.Fatalf("action %q target = %q, want host-9", a.Type, a.Target)
		}
	}
}

// TestOrchestrator_ContainerEscape_ApprovalGate verifies that an
// approval-required playbook (container-escape / T1611) marks its non-notify
// actions as NOT automated, so blocking steps do not fire without a human.
func TestOrchestrator_ContainerEscape_ApprovalGate(t *testing.T) {
	o := NewOrchestrator(nil)
	f := newFinding(WellCloudWorkload, "T1611", "pod-escape", "container escape", intel.SeverityHigh, nil)

	resp := o.Respond(f)
	if resp.Playbook != "container-escape" {
		t.Fatalf("playbook = %q, want container-escape", resp.Playbook)
	}
	for _, a := range resp.Actions {
		switch a.Type {
		case ActionNotify:
			if !a.Automated {
				t.Fatal("notify must always be automated")
			}
		case ActionHardenWorkload:
			if a.Automated {
				t.Fatal("harden-workload must require approval (not automated)")
			}
		}
	}
}

// TestOrchestrator_UnmatchedFinding_NotifyFallback verifies a finding with no
// matching playbook and below the fallback floor still yields an honest notify
// response rather than silently doing nothing.
func TestOrchestrator_UnmatchedFinding_NotifyFallback(t *testing.T) {
	o := NewOrchestrator(nil)
	// Low severity, unknown technique: no specific playbook, below High floor.
	f := newFinding(WellEndpoint, "T9999", "asset-x", "unknown", intel.SeverityLow, nil)

	resp := o.Respond(f)
	if len(resp.Actions) == 0 {
		t.Fatal("even an unmatched finding must produce a notify action")
	}
	if resp.Actions[0].Type != ActionNotify {
		t.Fatalf("fallback action = %q, want notify", resp.Actions[0].Type)
	}
}
