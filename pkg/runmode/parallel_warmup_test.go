package runmode

import (
	"context"
	"errors"
	"testing"
	"time"
)

// --- Unit Tests ---

func TestParallelWarmer_HappyPath_FastestSourceWins(t *testing.T) {
	// Source 0: slow (50ms)
	// Source 1: fast (immediate) — should win
	// Source 2: slow (100ms)
	// Source 3: errors out
	sources := []Source{
		{Name: "slow_env", ProbeFn: func(ctx context.Context) (RunMode, error) {
			select {
			case <-time.After(50 * time.Millisecond):
				return Degraded, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}},
		{Name: "fast_file", ProbeFn: func(ctx context.Context) (RunMode, error) {
			return Production, nil
		}},
		{Name: "slow_k8s", ProbeFn: func(ctx context.Context) (RunMode, error) {
			select {
			case <-time.After(100 * time.Millisecond):
				return Simulation, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}},
		{Name: "error_src", ProbeFn: func(ctx context.Context) (RunMode, error) {
			return "", errors.New("unavailable")
		}},
	}

	pw := NewParallelWarmer(sources...)
	ctx := context.Background()

	result, err := pw.Resolve(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Mode != Production {
		t.Errorf("mode=%q, want %q", result.Mode, Production)
	}
	if result.Source != "fast_file" {
		t.Errorf("source=%q, want %q", result.Source, "fast_file")
	}
}

func TestParallelWarmer_AllTimeout(t *testing.T) {
	sources := []Source{
		{Name: "timeout1", ProbeFn: func(ctx context.Context) (RunMode, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}},
		{Name: "timeout2", ProbeFn: func(ctx context.Context) (RunMode, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}},
	}

	pw := NewParallelWarmer(sources...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := pw.Resolve(ctx)
	if err == nil {
		t.Fatal("expected error when all sources timeout")
	}
}

func TestParallelWarmer_AllError(t *testing.T) {
	sources := []Source{
		{Name: "err1", ProbeFn: func(ctx context.Context) (RunMode, error) {
			return "", errors.New("fail1")
		}},
		{Name: "err2", ProbeFn: func(ctx context.Context) (RunMode, error) {
			return "", errors.New("fail2")
		}},
		{Name: "err3", ProbeFn: func(ctx context.Context) (RunMode, error) {
			return "", errors.New("fail3")
		}},
	}

	pw := NewParallelWarmer(sources...)
	ctx := context.Background()

	_, err := pw.Resolve(ctx)
	if err == nil {
		t.Fatal("expected error when all sources fail")
	}
	if err.Error() != "runmode: all sources failed" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParallelWarmer_Determinism(t *testing.T) {
	// All sources are instantaneous, but only one returns Production.
	// Run multiple times to verify consistent behavior with caching.
	sources := []Source{
		{Name: "env", ProbeFn: func(ctx context.Context) (RunMode, error) {
			return Production, nil
		}},
		DefaultSource(Simulation),
	}

	for i := 0; i < 100; i++ {
		pw := NewParallelWarmer(sources...)
		ctx := context.Background()
		r, err := pw.Resolve(ctx)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if !r.Mode.Valid() {
			t.Fatalf("iteration %d: invalid mode %q", i, r.Mode)
		}
	}
}

func TestParallelWarmer_CacheHotPath(t *testing.T) {
	calls := 0
	sources := []Source{
		{Name: "counter", ProbeFn: func(ctx context.Context) (RunMode, error) {
			calls++
			return Degraded, nil
		}},
	}

	pw := NewParallelWarmer(sources...)
	ctx := context.Background()

	// Cold path
	r1, err := pw.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !pw.IsWarm() {
		t.Fatal("expected warm after first Resolve")
	}
	coldCalls := calls

	// Hot path — should not call source again
	r2, err := pw.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if calls != coldCalls {
		t.Errorf("hot path re-probed: calls went %d -> %d", coldCalls, calls)
	}
	if r1.Mode != r2.Mode || r1.Source != r2.Source {
		t.Error("hot path returned different result")
	}

	// Reset → cold again
	pw.Reset()
	if pw.IsWarm() {
		t.Fatal("expected cold after Reset")
	}
	_, _ = pw.Resolve(ctx)
	if calls <= coldCalls {
		t.Error("expected re-probe after Reset")
	}
}

func TestParallelWarmer_NoSources(t *testing.T) {
	pw := NewParallelWarmer()
	ctx := context.Background()
	r, err := pw.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != Simulation {
		t.Errorf("mode=%q, want simulation for no sources", r.Mode)
	}
}

func TestParallelWarmer_ConvenienceSources(t *testing.T) {
	env := map[string]string{"CAF_RUN_MODE": "production"}
	src := EnvVarSource(func(k string) string { return env[k] })

	pw := NewParallelWarmer(src)
	ctx := context.Background()
	r, err := pw.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != Production {
		t.Errorf("mode=%q, want production", r.Mode)
	}
}

// --- Benchmarks ---

// benchSource creates a zero-alloc source that returns immediately.
func benchSource(name string, mode RunMode) Source {
	return Source{
		Name: name,
		ProbeFn: func(ctx context.Context) (RunMode, error) {
			return mode, nil
		},
	}
}

// benchSlowSource creates a source that always errors (simulates unreachable backend).
func benchSlowSource(name string) Source {
	return Source{
		Name: name,
		ProbeFn: func(ctx context.Context) (RunMode, error) {
			return "", errors.New("unreachable")
		},
	}
}

// BenchmarkParallelResolve_4Sources_Cold measures the cold path with 4 sources,
// where one responds instantly (env_var) and others error out or are slow.
// Target: ≤2µs per cold resolve.
func BenchmarkParallelResolve_4Sources_Cold(b *testing.B) {
	sources := []Source{
		benchSlowSource("slow_file"),
		benchSource("env_var", Production),
		benchSlowSource("slow_k8s"),
		benchSource("default", Simulation),
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pw := NewParallelWarmer(sources...)
		_, _ = pw.Resolve(ctx)
	}
}

// BenchmarkParallelResolve_4Sources_Hot measures the hot path (cached result).
// This should be extremely fast — just an atomic load.
func BenchmarkParallelResolve_4Sources_Hot(b *testing.B) {
	sources := []Source{
		benchSource("env_var", Production),
		benchSource("file", Degraded),
		benchSource("k8s", Simulation),
		benchSource("default", Simulation),
	}
	pw := NewParallelWarmer(sources...)
	ctx := context.Background()
	// Warm up
	_, _ = pw.Resolve(ctx)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pw.Resolve(ctx)
	}
}

// BenchmarkParallelResolve_AllInstant measures cold path when all 4 sources
// respond instantly (best-case scenario for parallel resolution).
func BenchmarkParallelResolve_AllInstant(b *testing.B) {
	sources := []Source{
		benchSource("env", Production),
		benchSource("file", Degraded),
		benchSource("k8s", Simulation),
		benchSource("default", Simulation),
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pw := NewParallelWarmer(sources...)
		_, _ = pw.Resolve(ctx)
	}
}

// BenchmarkParallelResolve_vs_Serial provides a direct comparison benchmark
// using the same deterministic probe environment as BenchmarkColdResolve.
func BenchmarkParallelResolve_vs_Serial(b *testing.B) {
	// Serial baseline (same as BenchmarkColdResolve)
	b.Run("Serial_SmartInferrer", func(b *testing.B) {
		s := NewSmartInferrerWithProbe(benchProbeFn)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			s.Reset()
			_ = s.Resolve()
		}
	})

	// Parallel: wrap the same probe logic into 4 sources
	b.Run("Parallel_4Sources", func(b *testing.B) {
		sources := []Source{
			EnvVarSource(benchProbeFn),
			{Name: "file_config", ProbeFn: func(ctx context.Context) (RunMode, error) {
				return "", errors.New("no file")
			}},
			{Name: "k8s_configmap", ProbeFn: func(ctx context.Context) (RunMode, error) {
				if benchProbeFn("KUBERNETES_SERVICE_HOST") != "" {
					return Degraded, nil
				}
				return "", errors.New("not in cluster")
			}},
			DefaultSource(Simulation),
		}
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pw := NewParallelWarmer(sources...)
			_, _ = pw.Resolve(ctx)
		}
	})
}
