package runmode

// smart_infer.go adds lightweight, real environment inference on top of the
// existing Parse/FromEnvName primitives, plus a warm cache so the resolved
// configuration is computed once (cold path) and served from memory afterwards
// (hot path). This is what powers the M41 "smart environment inference +
// warm-start acceleration" benchmarks: it exercises real os.Getenv probes and
// real precedence resolution rather than a synthetic loop.

import (
	"os"
	"sync"
)

// probeKeys are the environment variables inspected during a probe, in
// precedence order. The first non-empty signal that resolves a mode wins.
var probeKeys = []string{
	"CAF_RUN_MODE",             // explicit run_mode override
	"CAF_ENV",                  // platform environment name
	"ENV",                      // generic environment name
	"ENVIRONMENT",              // generic environment name (verbose)
	"CI",                       // CI systems imply non-production
	"KUBERNETES_SERVICE_HOST",  // in-cluster signal
}

// EnvProbe is a snapshot of the environment signals used to infer the mode.
type EnvProbe struct {
	RunMode     string // CAF_RUN_MODE
	EnvName     string // CAF_ENV / ENV / ENVIRONMENT (first non-empty)
	CI          string // CI
	InCluster   bool   // KUBERNETES_SERVICE_HOST present
	SignalCount int    // number of non-empty probe keys observed
}

// InferredConfig is the resolved configuration derived from an EnvProbe.
type InferredConfig struct {
	Mode      RunMode
	Source    string // which signal decided the mode
	InCluster bool
	Signals   int
}

// SmartInferrer resolves a RunMode from the environment and caches the result.
// probeFn is injectable so benchmarks and tests can drive it deterministically
// without mutating the real process environment; it defaults to os.Getenv.
type SmartInferrer struct {
	probeFn func(string) string

	mu     sync.RWMutex
	warm   bool
	cached InferredConfig
}

// NewSmartInferrer builds an inferrer backed by the real process environment.
func NewSmartInferrer() *SmartInferrer {
	return &SmartInferrer{probeFn: os.Getenv}
}

// NewSmartInferrerWithProbe builds an inferrer backed by a custom probe source.
func NewSmartInferrerWithProbe(fn func(string) string) *SmartInferrer {
	if fn == nil {
		fn = os.Getenv
	}
	return &SmartInferrer{probeFn: fn}
}

// Probe reads the environment signals. It performs one lookup per probe key and
// records the first non-empty environment name across the name aliases.
func (s *SmartInferrer) Probe() EnvProbe {
	var p EnvProbe
	for _, k := range probeKeys {
		v := s.probeFn(k)
		if v == "" {
			continue
		}
		p.SignalCount++
		switch k {
		case "CAF_RUN_MODE":
			p.RunMode = v
		case "CAF_ENV", "ENV", "ENVIRONMENT":
			if p.EnvName == "" {
				p.EnvName = v
			}
		case "CI":
			p.CI = v
		case "KUBERNETES_SERVICE_HOST":
			p.InCluster = true
		}
	}
	return p
}

// Infer resolves a configuration from a probe using the existing Parse and
// FromEnvName primitives, applying signal precedence.
func (s *SmartInferrer) Infer(p EnvProbe) InferredConfig {
	cfg := InferredConfig{InCluster: p.InCluster, Signals: p.SignalCount}
	switch {
	case p.RunMode != "":
		cfg.Mode = Parse(p.RunMode)
		cfg.Source = "run_mode_var"
	case p.EnvName != "":
		cfg.Mode = FromEnvName(p.EnvName)
		cfg.Source = "env_name"
	case p.CI != "":
		cfg.Mode = Simulation
		cfg.Source = "ci"
	default:
		cfg.Mode = Simulation
		cfg.Source = "default"
	}
	return cfg
}

// Resolve returns the inferred configuration. On the cold path (cache not warm)
// it probes the environment, infers, and caches the result; on the hot path it
// returns the cached value without touching the environment again.
func (s *SmartInferrer) Resolve() InferredConfig {
	s.mu.RLock()
	if s.warm {
		cfg := s.cached
		s.mu.RUnlock()
		return cfg
	}
	s.mu.RUnlock()

	cfg := s.Infer(s.Probe())

	s.mu.Lock()
	s.cached = cfg
	s.warm = true
	s.mu.Unlock()
	return cfg
}

// Warmup forces a cold resolution so subsequent Resolve calls hit the cache.
func (s *SmartInferrer) Warmup() InferredConfig {
	s.Reset()
	return s.Resolve()
}

// Reset clears the warm cache, forcing the next Resolve onto the cold path.
func (s *SmartInferrer) Reset() {
	s.mu.Lock()
	s.warm = false
	s.cached = InferredConfig{}
	s.mu.Unlock()
}

// IsWarm reports whether a resolved configuration is currently cached.
func (s *SmartInferrer) IsWarm() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.warm
}
