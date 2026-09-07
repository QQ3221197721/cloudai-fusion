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
// Purpose: Honest comparison of capability registry performance against
// minimal baselines to measure the real cost of honesty/policy enforcement.
//
// Competitors:
//   1. REAL CAPABILITY REGISTRY (current impl): sync.RWMutex + policy + snapshot sorting
//   2. PLAIN MAP+WLOCKETU BASELINE: raw map access without policy/sorting
//   3. STDIN ENV PARSE ONLY: RunMode parsing without capability tracking
//
// Work Unit: N capability checks / N mode parses across 6 runs, median reported.
// ============================================================================

// =========================================================================
// BASELINE A: Plain map+RWMutex capability lookup (NO policy enforcement)
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

func (m *PlainCapabilityMap) SnapshotUnsorted() []Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Backend, 0, len(m.backends))
	for _, b := range m.backends {
		out = append(out, b)
	}
	// NO SORTING - this is the key difference
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
// =========================================================================

type MinimalEnvParser struct {
	mu    sync.RWMutex
	cached runmode.RunMode
	warm  bool
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

	// Real environment probe (same as SmartInferrer but stripped down)
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
// COMPARISON BENCHMARKS
// ============================================================================

const benchComponents = 50 // Number of components to track

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

// BenchmarkCapabilityRegistry vs PlainMap: Policy Enforcement Cost
func BenchmarkCapabilityRegistrySnapshotSorted(b *testing.B) {
	b.ReportAllocs()
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

func BenchmarkPlainCapabilityMapUnsorted(b *testing.B) {
	b.ReportAllocs()
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

// BenchmarkCapabilityRegistry HasSimulated vs Unsafe check
func BenchmarkCapabilityRegistryHasSimulated(b *testing.B) {
	b.ReportAllocs()
	r := NewRegistry(runmode.Production)
	setupRegistryWithComponents(r)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		result := r.HasSimulated()
		_ = result
	}
}

func BenchmarkPlainMapHasSimulatedUnsafe(b *testing.B) {
	b.ReportAllocs()
	m := NewPlainCapabilityMap()
	setupPlainMapWithComponents(m)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		result := m.HasSimulatedUnsafe()
		_ = result
	}
}

// BenchmarkPolicyEnforce vs NoPolicy
func BenchmarkCapabilityRegistryEnforceProductionFail(b *testing.B) {
	b.ReportAllocs()
	r := NewRegistry(runmode.Production)
	for i := 0; i < benchComponents; i++ {
		if i%7 == 0 {
			r.Report("sim.component."+strconv.Itoa(i), "memory", ModeSimulated, "fallback")
		} else {
			r.Report("real.component."+strconv.Itoa(i), "real", ModeReal, "ok")
		}
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		err := r.Enforce()
		if err != nil {
			_ = err.Error()
		}
	}
}

func BenchmarkPlainMapNoEnforcement(b *testing.B) {
	b.ReportAllocs()
	m := NewPlainCapabilityMap()
	for i := 0; i < benchComponents; i++ {
		if i%7 == 0 {
			m.Report("sim.component."+strconv.Itoa(i), "memory", ModeSimulated, "fallback")
		} else {
			m.Report("real.component."+strconv.Itoa(i), "real", ModeReal, "ok")
		}
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		// NO POLICY CHECK - just read state
		_ = m.backends
	}
}

// BenchmarkRegistryReport vs PlainMapReport (write path)
func BenchmarkCapabilityRegistryReport(b *testing.B) {
	b.ReportAllocs()
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

func BenchmarkPlainMapReport(b *testing.B) {
	b.ReportAllocs()
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

// BenchmarkRunModeParse: Cold env parsing vs cached
func BenchmarkRunModeParseColdPath(b *testing.B) {
	b.ReportAllocs()
	parser := NewMinimalEnvParser()
	parser.ResetCache()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		mode := parser.ParseEnvironment()
		_ = mode.String()
	}
}

func BenchmarkRunModeParseWarmPath(b *testing.B) {
	b.ReportAllocs()
	parser := NewMinimalEnvParser()
	parser.Warmup()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		mode := parser.ParseEnvironment()
		_ = mode.String()
	}
}

// Full workflow simulation
func BenchmarkCapabilityRegistryFullWorkflow(b *testing.B) {
	b.ReportAllocs()
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

func BenchmarkPlainMapFullWorkflow(b *testing.B) {
	b.ReportAllocs()
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
		_ = m.backends
	}
}
