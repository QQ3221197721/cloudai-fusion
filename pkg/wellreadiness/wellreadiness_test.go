package wellreadiness

import (
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

func validStatus() Status {
	return Status{
		Well: 1, Name: "L1-intel", Claimed: M3FabricConnected,
		Wired: true, BackendMode: BackendReal, FabricConnected: true, EvidenceBacked: true,
	}
}

func TestValidate_HonestRecordPasses(t *testing.T) {
	if err := validStatus().Validate(); err != nil {
		t.Fatalf("honest record must validate: %v", err)
	}
}

func TestValidate_OverclaimsCaught(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Status)
	}{
		{"wired-lie", func(s *Status) { s.Wired = false }},                    // claims M3 but not wired
		{"backend-lie", func(s *Status) { s.BackendMode = BackendSimulated }}, // claims M2+ but simulated
		{"fabric-lie", func(s *Status) { s.FabricConnected = false }},         // claims M3 but not connected
		{"range-lie", func(s *Status) { s.Well = 99 }},                        // out of range
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := validStatus()
			c.mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatalf("overclaim %q must fail Validate", c.name)
			}
		})
	}
}

func TestValidate_LowClaimTolerant(t *testing.T) {
	// A well honestly claiming M0/M1 with a simulated backend is fine.
	s := Status{Well: 4, Name: "L4-network", Claimed: M1Wired, Wired: true, BackendMode: BackendSimulated}
	if err := s.Validate(); err != nil {
		t.Fatalf("honest low claim must validate: %v", err)
	}
}

func TestRegistry_ReportStoresTruthAndSnapshotSorts(t *testing.T) {
	r := NewRegistry(runmode.Simulation)
	_ = r.Report(Status{Well: 3, Name: "L3", Claimed: M1Wired, Wired: true})
	_ = r.Report(Status{Well: 1, Name: "L1", Claimed: M2RealBackend, Wired: true, BackendMode: BackendReal})
	snap := r.Snapshot()
	if len(snap) != 2 || snap[0].Well != 1 || snap[1].Well != 3 {
		t.Fatalf("snapshot must be sorted by well: %+v", snap)
	}
}

func TestReport_ProductionRejectsOverclaim_SimulationTolerates(t *testing.T) {
	bad := validStatus()
	bad.FabricConnected = false // claims M3 but not connected

	// Simulation: stored, visible, but Report does not error (matches capability semantics).
	sim := NewRegistry(runmode.Simulation)
	if err := sim.Report(bad); err != nil {
		t.Fatalf("simulation should tolerate the overclaim at Report time: %v", err)
	}
	if len(sim.Snapshot()) != 1 {
		t.Fatalf("record must still be stored (truth is always visible)")
	}

	// Production: Report returns the overclaim error.
	prod := NewRegistry(runmode.Production)
	if err := prod.Report(bad); err == nil {
		t.Fatalf("production must reject an overclaiming Report")
	}
}

func TestEnforce_ProductionFailsOnOverclaim(t *testing.T) {
	prod := NewRegistry(runmode.Production)
	// Honest well passes.
	_ = prod.Report(validStatus())
	if err := prod.Enforce(); err != nil {
		t.Fatalf("honest wells must pass Enforce: %v", err)
	}
	// Inject an overclaim; Enforce must now fail.
	over := Status{Well: 2, Name: "L2-hunt", Claimed: M3FabricConnected, Wired: false}
	_ = prod.Report(over) // returns err, but also stored
	if err := prod.Enforce(); err == nil {
		t.Fatalf("Enforce must fail when a well overclaims in production")
	}
}

func TestEnforce_NonProductionAlwaysPasses(t *testing.T) {
	sim := NewRegistry(runmode.Simulation)
	_ = sim.Report(Status{Well: 2, Name: "L2", Claimed: M3FabricConnected, Wired: false}) // overclaim
	if err := sim.Enforce(); err != nil {
		t.Fatalf("non-production Enforce must not fail: %v", err)
	}
}

func TestDefaultRegistry_Lifecycle(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	SetPolicy(runmode.Simulation)
	if err := Report(validStatus()); err != nil {
		t.Fatalf("default Report: %v", err)
	}
	if len(Snapshot()) != 1 {
		t.Fatalf("default snapshot should hold 1 record")
	}
	if err := Enforce(); err != nil {
		t.Fatalf("default Enforce (sim): %v", err)
	}
}
