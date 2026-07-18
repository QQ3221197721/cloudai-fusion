package capability

import (
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

func TestReport_RealNeverErrors(t *testing.T) {
	r := NewRegistry(runmode.Production)
	if err := r.Report("cache", "redis", ModeReal, "connected"); err != nil {
		t.Fatalf("reporting a real backend must never error, got: %v", err)
	}
	if r.HasSimulated() {
		t.Error("registry should have no simulated backends")
	}
	if err := r.Enforce(); err != nil {
		t.Errorf("Enforce() with only real backends must pass, got: %v", err)
	}
}

func TestReport_SimulatedUnderProductionErrors(t *testing.T) {
	r := NewRegistry(runmode.Production)
	err := r.Report("messaging", "memory", ModeSimulated, "no nats driver")
	if err == nil {
		t.Fatal("reporting simulated under production must return an error")
	}
	// Record is stored regardless so it remains visible.
	if !r.HasSimulated() {
		t.Error("simulated backend should be recorded even though it errored")
	}
	if len(r.Simulated()) != 1 {
		t.Errorf("expected 1 simulated backend, got %d", len(r.Simulated()))
	}
}

func TestReport_SimulatedAllowedInNonProduction(t *testing.T) {
	for _, mode := range []runmode.RunMode{runmode.Simulation, runmode.Degraded} {
		r := NewRegistry(mode)
		if err := r.Report("messaging", "memory", ModeSimulated, "dev"); err != nil {
			t.Errorf("mode=%s: simulated must be allowed, got error: %v", mode, err)
		}
		if err := r.Enforce(); err != nil {
			t.Errorf("mode=%s: Enforce must pass outside production, got: %v", mode, err)
		}
	}
}

func TestEnforce_AggregatesViolations(t *testing.T) {
	r := NewRegistry(runmode.Production)
	_ = r.Report("cache", "memory", ModeSimulated, "")
	_ = r.Report("messaging", "memory", ModeSimulated, "")
	_ = r.Report("scheduler.nodes", "k8s", ModeReal, "")
	err := r.Enforce()
	if err == nil {
		t.Fatal("Enforce must fail when simulated backends exist under production")
	}
	// Snapshot is sorted and includes all three.
	if got := len(r.Snapshot()); got != 3 {
		t.Errorf("Snapshot len = %d, want 3", got)
	}
	if got := len(r.Simulated()); got != 2 {
		t.Errorf("Simulated len = %d, want 2", got)
	}
}

func TestMustReal(t *testing.T) {
	r := NewRegistry(runmode.Production)
	if err := r.MustReal("cache", "redis", true, "ok"); err != nil {
		t.Errorf("MustReal(real=true) under production must pass, got: %v", err)
	}
	if err := r.MustReal("lock", "memory", false, "fallback"); err == nil {
		t.Error("MustReal(real=false) under production must error")
	}

	r2 := NewRegistry(runmode.Degraded)
	if err := r2.MustReal("lock", "memory", false, "fallback"); err != nil {
		t.Errorf("MustReal(real=false) in degraded must not error, got: %v", err)
	}
}

func TestDefaultRegistry(t *testing.T) {
	Reset()
	SetPolicy(runmode.Production)
	if Policy() != runmode.Production {
		t.Fatalf("Policy() = %q, want production", Policy())
	}
	if err := Report("x", "sim", ModeSimulated, ""); err == nil {
		t.Error("default Report simulated under production must error")
	}
	if !HasSimulated() {
		t.Error("default registry should report simulated")
	}
	if err := Enforce(); err == nil {
		t.Error("default Enforce must fail under production with a simulated backend")
	}
	Reset()
	if len(Snapshot()) != 0 {
		t.Error("Reset should clear the default registry")
	}
}
