package runmode

import "testing"

func TestParse(t *testing.T) {
	cases := map[string]RunMode{
		"production":  Production,
		"PROD":        Production,
		"prod":        Production,
		"degraded":    Degraded,
		"staging":     Degraded,
		"simulation":  Simulation,
		"sim":         Simulation,
		"development": Simulation,
		"dev":         Simulation,
		"test":        Simulation,
		"":            Simulation,
		"nonsense":    Simulation,
	}
	for in, want := range cases {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromEnvName(t *testing.T) {
	cases := map[string]RunMode{
		"production":  Production,
		"prod":        Production,
		"staging":     Degraded,
		"development": Simulation,
		"":            Simulation,
	}
	for in, want := range cases {
		if got := FromEnvName(in); got != want {
			t.Errorf("FromEnvName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModePredicates(t *testing.T) {
	if !Production.IsProduction() {
		t.Error("Production.IsProduction() = false")
	}
	if Production.AllowsSimulation() {
		t.Error("Production must not allow simulation")
	}
	if !Degraded.AllowsSimulation() || !Simulation.AllowsSimulation() {
		t.Error("Degraded/Simulation must allow simulation")
	}
	if Degraded.IsProduction() || Simulation.IsProduction() {
		t.Error("only Production.IsProduction() should be true")
	}
	for _, m := range []RunMode{Simulation, Degraded, Production} {
		if !m.Valid() {
			t.Errorf("%q should be valid", m)
		}
	}
	if RunMode("bogus").Valid() {
		t.Error("bogus mode should be invalid")
	}
}
