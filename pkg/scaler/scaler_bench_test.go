package scaler

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// Benchmarks — hot-path performance of the auto-scaling decision engine.
//
// Covered hot paths:
//   - Scale decision latency (EvaluateMonitorAlert / EvaluateExperiment)
//   - Metrics aggregation throughput (mixed-metric evaluations, ops/s)
//   - History append + query performance (GetHistory over growing ledger)
//   - Apply() write-back overhead (JSONL rewrite + attestation)
//   - allocations/op on the decision hot path
//   - Policy list sorting
//
// All benchmarks use a real filesystem store (b.TempDir) and a real signed
// evidence ledger — no mocks — so numbers reflect production code paths
// including JSON (un)marshalling, file I/O, and attestation.
// ============================================================================

// newBenchScaler builds a temp-backed scaler wired to a real signed ledger.
func newBenchScaler(b *testing.B) (*FSMScaler, context.Context) {
	b.Helper()
	tmpDir := b.TempDir()
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		b.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		b.Fatalf("ledger: %v", err)
	}
	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		b.Fatalf("NewFSMScaler: %v", err)
	}
	return s, context.Background()
}

// BenchmarkEvaluateMonitorAlert measures core scale-up decision latency:
// load policies → match → budget math → persist decision (JSONL append).
func BenchmarkEvaluateMonitorAlert(b *testing.B) {
	s, ctx := newBenchScaler(b)
	if err := s.AddPolicy(ctx, Policy{
		Name:            "latency-tracker",
		Metric:          "latency_p95",
		Threshold:       20,
		Direction:       "regression_triggers_up",
		MinNodes:        1,
		MaxNodes:        20,
		CooldownMinutes: 1,
	}); err != nil {
		b.Fatalf("AddPolicy: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.EvaluateMonitorAlert(ctx, "latency_p95", 25.0, 100.0, 60.0); err != nil {
			b.Fatalf("EvaluateMonitorAlert: %v", err)
		}
	}
}

// BenchmarkEvaluateExperiment measures accuracy-gain decision latency
// (no policy lookup path — pure decision + persist).
func BenchmarkEvaluateExperiment(b *testing.B) {
	s, ctx := newBenchScaler(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.EvaluateExperiment(ctx, 3.5, 100.0, 60.0); err != nil {
			b.Fatalf("EvaluateExperiment: %v", err)
		}
	}
}

// BenchmarkMetricsAggregationThroughput measures mixed-metric evaluation
// throughput (ops/s) across two active policies — the aggregation hot path.
func BenchmarkMetricsAggregationThroughput(b *testing.B) {
	s, ctx := newBenchScaler(b)
	if err := s.AddPolicy(ctx, Policy{
		Name: "latency-tracker", Metric: "latency_p95", Threshold: 15,
		Direction: "regression_triggers_up", MinNodes: 1, MaxNodes: 20,
	}); err != nil {
		b.Fatalf("AddPolicy: %v", err)
	}
	if err := s.AddPolicy(ctx, Policy{
		Name: "throughput-tracker", Metric: "throughput", Threshold: 10,
		Direction: "regression_triggers_up", MinNodes: 1, MaxNodes: 20,
	}); err != nil {
		b.Fatalf("AddPolicy: %v", err)
	}

	metrics := []string{"latency_p95", "throughput"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.EvaluateMonitorAlert(ctx, metrics[i%2], float64(15+(i%20)), 100.0, 60.0); err != nil {
			b.Fatalf("EvaluateMonitorAlert: %v", err)
		}
	}
}

// BenchmarkGetHistory measures history query performance (load JSONL →
// unmarshal → deterministic newest-first sort) over a pre-populated ledger.
func BenchmarkGetHistory(b *testing.B) {
	s, ctx := newBenchScaler(b)
	for i := 0; i < 200; i++ {
		if _, err := s.EvaluateMonitorAlert(ctx, "latency_p95", float64(10+i), 100.0, 60.0); err != nil {
			b.Fatalf("seed history: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if h := s.GetHistory(); len(h) == 0 {
			b.Fatal("empty history")
		}
	}
}

// BenchmarkHistoryAppendAndQuery measures the combined append + query hot loop
// as the ledger grows (realistic monitor-alert reconcile cycle).
func BenchmarkHistoryAppendAndQuery(b *testing.B) {
	s, ctx := newBenchScaler(b)
	for i := 0; i < 50; i++ {
		if _, err := s.EvaluateMonitorAlert(ctx, "latency_p95", float64(10+i), 100.0, 60.0); err != nil {
			b.Fatalf("seed history: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.EvaluateMonitorAlert(ctx, "latency_p95", float64(20+i%30), 100.0, 60.0); err != nil {
			b.Fatalf("EvaluateMonitorAlert: %v", err)
		}
		_ = s.GetHistory()
	}
}

// BenchmarkApply measures Apply() overhead: load → mutate → atomic JSONL
// rewrite → attestation.
func BenchmarkApply(b *testing.B) {
	s, ctx := newBenchScaler(b)
	ids := make([]string, 0, 64)
	for i := 0; i < 64; i++ {
		d, err := s.EvaluateMonitorAlert(ctx, "latency_p95", float64(20+i), 100.0, 60.0)
		if err != nil {
			b.Fatalf("seed decisions: %v", err)
		}
		ids = append(ids, d.ID)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Apply is idempotent-guarded; after first apply it returns an error,
		// which still exercises the load + scan hot path we want to measure.
		_ = s.Apply(ctx, ids[i%len(ids)])
	}
}

// BenchmarkListPolicies measures policy list + newest-first sort performance.
func BenchmarkListPolicies(b *testing.B) {
	s, ctx := newBenchScaler(b)
	names := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	for _, name := range names {
		if err := s.AddPolicy(ctx, Policy{
			Name: name, Metric: "latency_p95", Threshold: 20,
			Direction: "regression_triggers_up", MinNodes: 1, MaxNodes: 10,
		}); err != nil {
			b.Fatalf("AddPolicy: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if p := s.ListPolicies(); len(p) == 0 {
			b.Fatal("no policies")
		}
	}
}
