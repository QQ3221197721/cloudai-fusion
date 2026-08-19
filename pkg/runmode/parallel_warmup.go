package runmode

// parallel_warmup.go implements concurrent multi-source probing for RunMode
// resolution. Instead of serially checking env vars → file config → K8s
// ConfigMap → default, it fires goroutines for all sources simultaneously and
// takes the first successful response (race-to-first with context cancellation).
//
// Design:
//   - Each Source is a lightweight function that returns (RunMode, error).
//   - ParallelWarmer fans out all sources, collects the first non-error result.
//   - After resolution, the result is cached (same warm/cold semantics as
//     SmartInferrer) so hot-path access is a single atomic load.
//
// Target: 4-source cold resolve ≤2µs (vs serial SmartInferrer cold ~8µs).

import (
	"context"
	"errors"
	"sync/atomic"
)

// Source is a probe function that resolves a RunMode from a specific origin.
// Implementations must be safe for concurrent invocation and should respect
// context cancellation to avoid lingering goroutines.
type Source struct {
	Name    string
	ProbeFn func(ctx context.Context) (RunMode, error)
}

// ParallelResult holds the resolved mode plus metadata about which source won.
type ParallelResult struct {
	Mode   RunMode
	Source string // Name of the winning source
}

// ParallelWarmer resolves RunMode by racing multiple sources concurrently.
// The first source to return a valid (non-error) result wins; all others are
// cancelled. Results are cached for hot-path access.
type ParallelWarmer struct {
	sources []Source

	// warm cache (lock-free hot path via atomic)
	resolved atomic.Value // stores *ParallelResult
}

// NewParallelWarmer creates a warmer that will race the provided sources.
// At least one source should be provided; if none are given, Resolve returns
// the default Simulation mode.
func NewParallelWarmer(sources ...Source) *ParallelWarmer {
	pw := &ParallelWarmer{
		sources: sources,
	}
	return pw
}

// Resolve returns the RunMode by racing all sources concurrently. On first
// call (cold path) it fans out goroutines and caches the result. Subsequent
// calls (hot path) return the cached value with zero allocation.
func (pw *ParallelWarmer) Resolve(ctx context.Context) (ParallelResult, error) {
	// Hot path: check cache first (atomic load).
	v := pw.resolved.Load()
	if r, ok := v.(*ParallelResult); ok && r != nil {
		return *r, nil
	}

	// Cold path: race sources.
	result, err := pw.resolve(ctx)
	if err != nil {
		return ParallelResult{}, err
	}

	// Cache result for future hot-path access.
	pw.resolved.Store(&result)
	return result, nil
}

// resolve performs the actual parallel probe. It creates a child context that
// is cancelled as soon as the first source responds successfully.
func (pw *ParallelWarmer) resolve(ctx context.Context) (ParallelResult, error) {
	if len(pw.sources) == 0 {
		return ParallelResult{Mode: Simulation, Source: "default"}, nil
	}

	type probeResult struct {
		mode   RunMode
		source string
		ok     bool
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered channel: every source sends exactly one result (success or failure).
	ch := make(chan probeResult, len(pw.sources))

	for i := range pw.sources {
		src := pw.sources[i]
		go func() {
			mode, err := src.ProbeFn(ctx)
			if err == nil {
				ch <- probeResult{mode: mode, source: src.Name, ok: true}
			} else {
				ch <- probeResult{ok: false}
			}
		}()
	}

	// Wait for first success or all sources to fail / context to expire.
	remaining := len(pw.sources)
	for remaining > 0 {
		select {
		case r := <-ch:
			if r.ok {
				cancel()
				return ParallelResult{Mode: r.mode, Source: r.source}, nil
			}
			remaining--
		case <-ctx.Done():
			return ParallelResult{}, ctx.Err()
		}
	}
	return ParallelResult{}, errors.New("runmode: all sources failed")
}

// Reset clears the warm cache, forcing the next Resolve onto the cold path.
func (pw *ParallelWarmer) Reset() {
	pw.resolved.Store((*ParallelResult)(nil))
}

// IsWarm reports whether a resolved configuration is currently cached.
func (pw *ParallelWarmer) IsWarm() bool {
	v := pw.resolved.Load()
	if r, ok := v.(*ParallelResult); ok {
		return r != nil
	}
	return false
}

// --- Convenience source constructors ---

// EnvVarSource creates a Source that resolves from an environment variable.
// probeFn is the env-lookup function (e.g. os.Getenv); injectable for testing.
func EnvVarSource(probeFn func(string) string) Source {
	return Source{
		Name: "env_var",
		ProbeFn: func(ctx context.Context) (RunMode, error) {
			if v := probeFn("CAF_RUN_MODE"); v != "" {
				return Parse(v), nil
			}
			if v := probeFn("CAF_ENV"); v != "" {
				return FromEnvName(v), nil
			}
			return "", errors.New("no env var set")
		},
	}
}

// FileConfigSource creates a Source that resolves from a config file lookup.
// readFn simulates reading a file; in production this would read a YAML/JSON.
func FileConfigSource(readFn func(ctx context.Context) (string, error)) Source {
	return Source{
		Name: "file_config",
		ProbeFn: func(ctx context.Context) (RunMode, error) {
			val, err := readFn(ctx)
			if err != nil {
				return "", err
			}
			mode := Parse(val)
			if !mode.Valid() {
				return "", errors.New("invalid mode in file config")
			}
			return mode, nil
		},
	}
}

// K8sConfigMapSource creates a Source that resolves from a Kubernetes ConfigMap.
// lookupFn simulates the ConfigMap lookup.
func K8sConfigMapSource(lookupFn func(ctx context.Context) (string, error)) Source {
	return Source{
		Name: "k8s_configmap",
		ProbeFn: func(ctx context.Context) (RunMode, error) {
			val, err := lookupFn(ctx)
			if err != nil {
				return "", err
			}
			return Parse(val), nil
		},
	}
}

// DefaultSource creates a Source that always returns the given fallback mode.
func DefaultSource(fallback RunMode) Source {
	return Source{
		Name: "default",
		ProbeFn: func(ctx context.Context) (RunMode, error) {
			return fallback, nil
		},
	}
}
