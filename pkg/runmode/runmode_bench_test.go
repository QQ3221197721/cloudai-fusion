package runmode

import "testing"

// benchProbeEnv is a deterministic environment used to drive the smart-inference
// benchmarks without depending on (or mutating) the real process environment.
var benchProbeEnv = map[string]string{
	"CAF_ENV":                 "staging",
	"CI":                      "true",
	"KUBERNETES_SERVICE_HOST": "10.0.0.1",
}

func benchProbeFn(key string) string { return benchProbeEnv[key] }

// BenchmarkParse measures the raw config-string inference primitive.
func BenchmarkParse(b *testing.B) {
	inputs := []string{"production", "prod", "staging", "degraded", "dev", "", "nonsense"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Parse(inputs[i%len(inputs)])
	}
}

// BenchmarkFromEnvName measures env-name→mode inference.
func BenchmarkFromEnvName(b *testing.B) {
	inputs := []string{"production", "staging", "development", "prod", ""}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FromEnvName(inputs[i%len(inputs)])
	}
}

// BenchmarkEnvProbe measures the environment-probe latency (one lookup per
// probe key across the deterministic bench environment).
func BenchmarkEnvProbe(b *testing.B) {
	s := NewSmartInferrerWithProbe(benchProbeFn)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Probe()
	}
}

// BenchmarkConfigInference measures precedence resolution from an already
// captured probe (isolating inference cost from probe cost).
func BenchmarkConfigInference(b *testing.B) {
	s := NewSmartInferrerWithProbe(benchProbeFn)
	p := s.Probe()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Infer(p)
	}
}

// BenchmarkColdResolve measures the cold-start path: every iteration re-probes
// the environment and recomputes the configuration (cache reset each time).
func BenchmarkColdResolve(b *testing.B) {
	s := NewSmartInferrerWithProbe(benchProbeFn)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Reset()
		_ = s.Resolve()
	}
}

// BenchmarkHotResolve measures the warm-start path: the configuration is
// resolved once, then every iteration serves the cached result.
func BenchmarkHotResolve(b *testing.B) {
	s := NewSmartInferrerWithProbe(benchProbeFn)
	s.Warmup()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Resolve()
	}
}
