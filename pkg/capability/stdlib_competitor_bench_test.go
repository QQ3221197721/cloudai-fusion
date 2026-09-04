package capability

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

// ============================================================================
// FAIR T2 HEAD-TO-HEAD: Real Capability Registry vs Stdlib Baseline (M1 Honesty)
//
// Purpose: Honest head-to-head comparison of run-mode/capability parsing against
// minimal stdlib (`flag` + `os.Getenv`) baselines — same work unit, count=6 medians,
// real verdict even if we lose on raw parse speed.
//
// Competitors:
//   1. REAL REGISTRY (current impl): sync.RWMutex + policy + sorted snapshots + enforce
//   2. PLAIN MAP baseline: raw map + RWMutex without policy/sort (already in compare bench)
//   3. NEW: FLG parser: stdlib `flag` package handling CAF_RUN_MODE / CAF_ENV flags
//   4. NEW: ENV ONLY: bare os.Getenv() calls with manual mode resolution (no flag pkg)
//
// Work Unit A: MODE PARSE (cold resolve of CAF_RUN_MODE / CAF_ENV from args or env)
// Work Unit B: CAPABILITY CHECK (Report + Snapshot + HasSimulated + Enforce path)
//
// Both sides do equal work: parse N env inputs + resolve mode + track components.
// Report latency ns/op, throughput, allocs across count=6 runs → median.
// ==========================================================================

type FlgRunModeParser struct {
	parsed bool
	mu     sync.RWMutex
	result runmode.RunMode
}

func NewFlgRunModeParser() *FlgRunModeParser {
	return &FlgRunModeParser{}
}

// ParseFromArgs uses stdlib flag to parse from command line args
func (f *FlgRunModeParser) ParseFromArgs(args []string) (runmode.RunMode, error) {
	var mode string
	localFlags := flag.NewFlagSet("rm"+strconv.Itoa(randCntr), flag.ContinueOnError)
	randCntr++
	localFlags.StringVar(&mode, "r", "", "run mode")
	_ = localFlags.Parse(args)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.parsed = true
	f.result = runmode.Parse(mode)
	return f.result, nil
}

// ParseEnvironment reads CAF_RUN_MODE then falls back to CAF_ENV (mirrors MinimalEnvParser)
func (f *FlgRunModeParser) ParseEnvironment() runmode.RunMode {
	f.mu.RLock()
	if f.parsed {
		res := f.result
		f.mu.RUnlock()
		return res
	}
	f.mu.RUnlock()

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

	f.mu.Lock()
	f.parsed = true
	f.result = mode
	f.mu.Unlock()
	return mode
}

func (f *FlgRunModeParser) ResetCache() {
	f.mu.Lock()
	f.parsed = false
	f.result = runmode.Simulation
	f.mu.Unlock()
}

func (f *FlgRunModeParser) Warmup() {
	f.ParseEnvironment()
}

// =========================================================================
// BASELINE D: Bare os.Getenv + manual switch (FASTEST POSSIBLE stdlib)
// This baseline uses EnvOnlyParser defined in flag_resolver_bench_test.go
// to avoid duplicate definitions across test files.

var randCntr int

// ============================================================================
// COMPARISON BENCHMARKS — fair, count=6, JSON for automation
// ============================================================================

const benchCompN = 50 // N components tracked (fair N>=20)

// ---- Work Unit 1: MODE PARSE — cold path (all competitors) ----

func BenchmarkStdlibFlgParseCold(b *testing.B) {
	p := NewFlgRunModeParser()
	args := []string{"--r", "production"}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p.ResetCache()
		_, _ = p.ParseFromArgs(args)
	}
}

func BenchmarkStdlibEnvOnlyParseCold(b *testing.B) {
	// Referenced in flag_resolver_bench_test.go - skipping here to avoid duplicate
	b.Skip("See BenchmarkCapabilityMinimalEnvParseCold")
}

func BenchmarkCapabilityMinimalEnvParseCold(b *testing.B) {
	p := NewMinimalEnvParser()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p.ResetCache()
		_ = p.ParseEnvironment()
	}
}

// ---- Work Unit 2: MODE PARSE — hot/warm path (cached, no env IO) ----

func BenchmarkStdlibFlgParseWarm(b *testing.B) {
	p := NewFlgRunModeParser()
	p.Warmup()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = p.ParseEnvironment()
	}
}

func BenchmarkStdlibEnvOnlyParseWarm(b *testing.B) {
	// Referenced in flag_resolver_bench_test.go - skipping here to avoid duplicate
	b.Skip("See BenchmarkCapabilityMinimalEnvParseWarm")
}

func BenchmarkCapabilityMinimalEnvParseWarm(b *testing.B) {
	p := NewMinimalEnvParser()
	p.Warmup()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = p.ParseEnvironment()
	}
}

// ---- Work Unit 3: RAW PARSE PRIMITIVE (no caching, no mutexes) ----

func BenchmarkRawStringToLowerTrimSpace(b *testing.B) {
	inputs := []string{"production", "prod", "staging", "dev", "DEGRADED", ""}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := strings.ToLower(strings.TrimSpace(inputs[i%len(inputs)]))
		switch s {
		case "production", "prod":
			_ = "production"
		case "degraded", "staging":
			_ = "degraded"
		default:
			_ = "simulation"
		}
	}
}

func BenchmarkStdlibFlagParsingOverhead(b *testing.B) {
	inputs := [][]string{
		{"--r", "production"},
		{"--r", "dev"},
		{"--r", "staging"},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f := flag.NewFlagSet("test"+strconv.Itoa(randCntr), flag.ContinueOnError)
		randCntr++
		var val string
		f.StringVar(&val, "x", "", "")
		_ = f.Parse(inputs[i%len(inputs)])
	}
}

// ---- Work Unit 4: CAPABILITY WRITE PATH (report N components) ----

func BenchmarkCapabilityRegistryReportHeavy(b *testing.B) {
	r := NewRegistry(runmode.Degraded)
	setupRegistryWithComponents(r)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		component := "bench-comp-" + strconv.Itoa(i%benchCompN)
		mode := ModeReal
		if i%7 == 0 {
			mode = ModeSimulated
		}
		_ = r.Report(component, "driver-"+strconv.Itoa(i), mode, "detail")
	}
}

func BenchmarkPlainMapReportHeavy(b *testing.B) {
	m := NewPlainCapabilityMap()
	setupPlainMapWithComponents(m)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		component := "bench-comp-" + strconv.Itoa(i%benchCompN)
		mode := ModeReal
		if i%7 == 0 {
			mode = ModeSimulated
		}
		m.Report(component, "driver-"+strconv.Itoa(i), mode, "detail")
	}
}

// ---- Work Unit 5: CAPABILITY SNAPSHOT WITH SORT VS UNSORTED ----

func BenchmarkCapabilityRegistrySnapshotSortedHeavy(b *testing.B) {
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

func BenchmarkPlainMapSnapshotUnsortedHeavy(b *testing.B) {
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

// ---- Work Unit 6: FULL BOOTSTRAP WORKFLOW (real-world startup) ----

func BenchmarkFullRegistryBootWorkflow(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := NewRegistry(runmode.Production)
		_ = r.MustReal("cache.redis.1", "redis", true, "real-cache")
		_ = r.MustReal("messaging.kafka.1", "kafka", false, "sim-messaging")
		_ = r.Report("store.postgres.1", "pg", ModeReal, "db")
		_ = r.Report("scheduler.nodes.1", "k8s", ModeReal, "k8s")
		_ = r.HasSimulated()
		_ = r.Enforce()
		_ = r.Snapshot()
	}
}

func BenchmarkFlatMapNoEnforceBootWorkflow(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m := NewPlainCapabilityMap()
		m.Report("cache.redis.1", "redis", ModeReal, "real-cache")
		m.Report("messaging.kafka.1", "kafka", ModeSimulated, "sim-messaging")
		m.Report("store.postgres.1", "pg", ModeReal, "db")
		m.Report("scheduler.nodes.1", "k8s", ModeReal, "k8s")
		m.HasSimulatedUnsafe()
		m.SnapshotUnsorted()
	}
}

// ==== ENVIRONMENT SETUP FOR CONSISTENT RESULTS ====
func init() {
	os.Setenv("CAF_RUN_MODE", "degraded")
	os.Setenv("CAF_ENV", "staging")
}

// =========================================================================
// INTERPRETATION GUIDELINES (honest T2 verdict rules)
//
// EXPECTED OUTCOMES:
// - Bare stdlib env parse (EnvOnlyParser, MinimalEnvParser) should win raw parse speed
// - Flag-based parse adds overhead vs bare os.Getenv due to flag.Parse machinery
// - Registry vs Plain Map: snapshot sort dominates overhead (~5-15 microseconds per 50 comps)
// - Enforce() adds policy cost only visible in Production mode when simulated exist
//
// EDGE WHERE WE WIN:
// - Production fail-fast enforcement (Enforce) that stdlib lacks entirely
// - Ordered snapshots required for consistent API responses
// - MustReal abstraction for real-backend-required semantics
// - Registry enables honesty audit trail via RegisteredAt timestamps
// =========================================================================
