package capability

import (
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

// ============================================================================
// FAIR HEAD-TO-HEAD T2 BENCHMARK: Capability Registry vs Plain Baselines
//
// Purpose: Honest comparison of the M1 Run-mode Honesty capability registry
// against minimal baselines to measure the REAL cost of honesty/policy
// enforcement — no strawmen, same work unit, honest verdict even if we lose.
//
// Competitors:
//   1. REAL CAPABILITY REGISTRY (current impl): sync.RWMutex + policy + sorted snapshot
//   2. PLAIN MAP+RWMUTEX BASELINE: raw map access without policy/sorting
//   3. MINIMAL ENV PARSER: stdlib flag/env RunMode parse without capability tracking
//
// Work Unit: N=50 components; N capability checks / N mode parses.
// Report latency ns/op, throughput (b.N), allocs across count=6, median.
// ============================================================================

// =========================================================================
// BASELINE A: Plain map+RWMutex capability lookup (NO policy enforcement)
//
// This is the honest "what you'd write if you didn't care about run-mode
// honesty": a concurrent map keyed by component name. It uses the SAME
// sync.RWMutex + map[string]Backend data structure as the real Registry,
// but drops (a) run-mode policy enforcement, (b) sorted snapshots. Any
// speedup it shows is precisely the price of those two honesty features.
// =========================================================================

type PlainCapabilityMap struct {
	mu       sync.RWMutex
	backends map[string]Backend
}

func NewPlainCapabilityMap() *PlainCapabilityMap {
	return &PlainCapabilityMap{backends: make(map[string]Backend)}
}

func (m *PlainCapabilityMap) Report(component, driver string, mode Mode, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[component] = Backend{
		Component:    component,
		Mode:         mode,
		Driver:       driver,
		Detail:       detail,
		RegisteredAt: time.Now().UTC(),
	}
}

// SnapshotUnsorted returns records WITHOUT the sort.Slice the real Registry
// performs — this isolates the sorting cost of the honest snapshot.
func (m *PlainCapabilityMap) SnapshotUnsorted() []Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Backend, 0, len(m.backends))
	for _, b := range m.backends {
		out = append(out, b)
	}
	return out
}

func (m *PlainCapabilityMap) HasSimulatedUnsafe() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, b := range m.backends {
		if b.Mode == ModeSimulated {
			return true
		}
	}
	return false
}

// =========================================================================
// BASELINE B: Stdlib flag/env parse only (RunMode without capability registry)
//
// This is the "just parse an env var" competitor: it reads CAF_RUN_MODE /
// CAF_ENV with os.Getenv and delegates to the same runmode.Parse primitives,
// caching the result behind an RWMutex. It has no capability registry at all
// — it is the honest baseline for the mode-inference work unit.
// =========================================================================

type MinimalEnvParser struct {
	mu     sync.RWMutex
	cached runmode.RunMode
	warm   bool
}

func NewMinimalEnvParser() *MinimalEnvParser {
	return &MinimalEnvParser{}
}

func (p *MinimalEnvParser) ParseEnvironment() runmode.RunMode {
	p.mu.RLock()
	if p.warm {
		mode := p.cached
		p.mu.RUnlock()
		return mode
	}
	p.mu.RUnlock()

	var mode runmode.RunMode
	runMode := os.Getenv("CAF_RUN_MODE")
	envName := os.Getenv("CAF_ENV")

	switch {
	case runMode != "":
		mode = runmode.Parse(runMode)
	case envName != "":
		mode = runmode.FromEnvName(envName)
	default:
		mode = runmode.Simulation
	}

	p.mu.Lock()
	p.cached = mode
	p.warm = true
	p.mu.Unlock()
	return mode
}

func (p *MinimalEnvParser) Warmup() {
	p.ParseEnvironment()
}

// ResetCache forces the next ParseEnvironment onto the cold path.
func (p *MinimalEnvParser) ResetCache() {
	p.mu.Lock()
	p.warm = false
	p.mu.Unlock()
}

// ============================================================================
// COMPARISON BENCHMARKS — same work unit for both competitors
// ============================================================================

const benchComponents = 50 // Number of components to track (fair, N>=20)

func setupRegistryWithComponents(r *Registry) {
	for i := 0; i < benchComponents; i++ {
		component := "component-"
		switch {
		case i < 15:
			component += "cache.redis." + strconv.Itoa(i)
		case i < 30:
			component += "messaging.kafka." + strconv.Itoa(i)
		case i < 40:
			component += "store.postgres." + strconv.Itoa(i)
		default:
			component += "scheduler.nodes." + strconv.Itoa(i)
		}

		mode := ModeReal
		if i%7 == 0 {
			mode = ModeSimulated
		}

		r.Report(component, "driver-"+strconv.Itoa(i), mode, "test-detail")
	}
}

func setupPlainMapWithComponents(m *PlainCapabilityMap) {
	for i := 0; i < benchComponents; i++ {
		component := "component-"
		switch {
		case i < 15:
			component += "cache.redis." + strconv.Itoa(i)
		case i < 30:
			component += "messaging.kafka." + strconv.Itoa(i)
		case i < 40:
			component += "store.postgres." + strconv.Itoa(i)
		default:
			component += "scheduler.nodes." + strconv.Itoa(i)
		}

		mode := ModeReal
		if i%7 == 0 {
			mode = ModeSimulated
		}

		m.Report(component, "driver-"+strconv.Itoa(i), mode, "test-detail")
	}
}

// ---- Work unit 1: SNAPSHOT (sorted honesty vs unsorted plain) ----

func BenchmarkCmpRegistrySnapshotSorted(b *testing.B) {
	r := NewRegistry(runmode.Degraded)
	setupRegistryWithComponents(r)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		snap := r.Snapshot()
		if len(snap) > 0 {
			_ = snap[0].Component
		}
	}
}

func BenchmarkCmpPlainMapUnsorted(b *testing.B) {
	m := NewPlainCapabilityMap()
	setupPlainMapWithComponents(m)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		snap := m.SnapshotUnsorted()
		if len(snap) > 0 {
			_ = snap[0].Component
		}
	}
}

// ---- Work unit 2: HasSimulated scan (identical work both sides) ----

func BenchmarkCmpRegistryHasSimulated(b *testing.B) {
	r := NewRegistry(runmode.Production)
	setupRegistryWithComponents(r)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.HasSimulated()
	}
}

func BenchmarkCmpPlainMapHasSimulated(b *testing.B) {
	m := NewPlainCapabilityMap()
	setupPlainMapWithComponents(m)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.HasSimulatedUnsafe()
	}
}

// ---- Work unit 3: WRITE path (Report) ----

func BenchmarkCmpRegistryReport(b *testing.B) {
	r := NewRegistry(runmode.Degraded)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		component := "component-" + strconv.Itoa(i%benchComponents)
		mode := ModeReal
		if i%7 == 0 {
			mode = ModeSimulated
		}
		_ = r.Report(component, "driver-"+strconv.Itoa(i), mode, "detail")
	}
}

func BenchmarkCmpPlainMapReport(b *testing.B) {
	m := NewPlainCapabilityMap()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		component := "component-" + strconv.Itoa(i%benchComponents)
		mode := ModeReal
		if i%7 == 0 {
			mode = ModeSimulated
		}
		m.Report(component, "driver-"+strconv.Itoa(i), mode, "detail")
	}
}

// ---- Work unit 4: MODE PARSE (warm cached path, both sides) ----
// Registry side: SmartInferrer warm resolve is not visible from this package,
// so we compare the minimal parser cold vs warm to characterize the parse cost.

func BenchmarkCmpEnvParseColdPath(b *testing.B) {
	parser := NewMinimalEnvParser()
	parser.ResetCache()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mode := parser.ParseEnvironment()
		_ = mode.String()
	}
}

func BenchmarkCmpEnvParseWarmPath(b *testing.B) {
	parser := NewMinimalEnvParser()
	parser.Warmup()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mode := parser.ParseEnvironment()
		_ = mode.String()
	}
}

// ---- Work unit 5: FULL WORKFLOW (Report -> Snapshot -> Check -> Enforce) ----

func BenchmarkCmpRegistryFullWorkflow(b *testing.B) {
	r := NewRegistry(runmode.Production)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		component := "workflow-" + strconv.Itoa(i%benchComponents)
		mode := ModeReal
		if i%7 == 0 {
			mode = ModeSimulated
		}
		_ = r.Report(component, "driver", mode, "workflow")
		snap := r.Snapshot()
		_ = snap
		_ = r.HasSimulated()
		_ = r.Policy()
		_ = r.Enforce()
	}
}

func BenchmarkCmpPlainMapFullWorkflow(b *testing.B) {
	m := NewPlainCapabilityMap()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		component := "workflow-" + strconv.Itoa(i%benchComponents)
		mode := ModeReal
		if i%7 == 0 {
			mode = ModeSimulated
		}
		m.Report(component, "driver", mode, "workflow")
		snap := m.SnapshotUnsorted()
		_ = snap
		_ = m.HasSimulatedUnsafe()
	}
}
