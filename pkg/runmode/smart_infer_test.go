package runmode

import "testing"

func TestSmartInferrer_Precedence(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantMod RunMode
		wantSrc string
	}{
		{"explicit_run_mode", map[string]string{"CAF_RUN_MODE": "production", "CAF_ENV": "dev"}, Production, "run_mode_var"},
		{"env_name_staging", map[string]string{"CAF_ENV": "staging"}, Degraded, "env_name"},
		{"ci_only", map[string]string{"CI": "true"}, Simulation, "ci"},
		{"empty_default", map[string]string{}, Simulation, "default"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewSmartInferrerWithProbe(func(k string) string { return c.env[k] })
			cfg := s.Resolve()
			if cfg.Mode != c.wantMod {
				t.Errorf("mode=%q, want %q", cfg.Mode, c.wantMod)
			}
			if cfg.Source != c.wantSrc {
				t.Errorf("source=%q, want %q", cfg.Source, c.wantSrc)
			}
		})
	}
}

func TestSmartInferrer_WarmCache(t *testing.T) {
	calls := 0
	s := NewSmartInferrerWithProbe(func(k string) string {
		calls++
		if k == "CAF_ENV" {
			return "production"
		}
		return ""
	})

	if s.IsWarm() {
		t.Fatal("inferrer must start cold")
	}
	first := s.Resolve()
	if !s.IsWarm() {
		t.Fatal("inferrer must be warm after Resolve")
	}
	afterCold := calls

	// Hot path must not re-probe the environment.
	_ = s.Resolve()
	if calls != afterCold {
		t.Errorf("hot Resolve re-probed env: calls went %d -> %d", afterCold, calls)
	}
	if first.Mode != Production {
		t.Errorf("mode=%q, want production", first.Mode)
	}

	// Reset returns to the cold path.
	s.Reset()
	if s.IsWarm() {
		t.Fatal("Reset must clear the warm cache")
	}
	_ = s.Resolve()
	if calls <= afterCold {
		t.Error("cold Resolve after Reset should re-probe env")
	}
}

func TestSmartInferrer_InClusterSignal(t *testing.T) {
	s := NewSmartInferrerWithProbe(func(k string) string {
		if k == "KUBERNETES_SERVICE_HOST" {
			return "10.0.0.1"
		}
		return ""
	})
	cfg := s.Resolve()
	if !cfg.InCluster {
		t.Error("expected InCluster=true when KUBERNETES_SERVICE_HOST is set")
	}
	if cfg.Signals != 1 {
		t.Errorf("Signals=%d, want 1", cfg.Signals)
	}
}
