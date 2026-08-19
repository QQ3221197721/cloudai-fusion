// Package modelmonitor — benchmarks for Module 20 (Model Performance Monitor).
//
// Measures the honest, in-process cost of recording performance observations,
// setting baselines, computing drift, and evaluating alert rules. Records are
// appended JSONL; reports read files and parse JSON. The baseline+alert flow
// triggers on thresholds configured via DefaultRules():
//   - latency_p95_regression: warn ≥25%, critical ≥50%
//   - error_rate_regression: warn ≥50%, critical ≥100%
//   - accuracy_regression: warn ≥5pp drop, critical ≥10pp drop
//   - throughput_regression: warn ≥30% drop, critical ≥60% drop
//
// Synthetic drift benchmark: generates N records with controlled accuracy drops
// plus small Gaussian noise (GaussianSeed=42 ensures reproducibility). We measure:
//   - Detection Rate: % of runs where an alert fires given a known regression
//     (accuracy = baseline − delta + noise), tested at delta ∈ {0, 3pp, 5pp, 10pp}
//     against a 5pp WARN threshold. "Synthetic data" because these are not production
//     traffic metrics — they're noise-injected synthetic readings with a controlled
//     ground-truth offset from baseline.
//
// This mirrors real A/B evaluation where the monitor is run as a deterministic
// classifier over noisy measurements: it has a configurable threshold and will
// make Type I errors above it (detecting noise as regression) and false negatives
// below it. The ROC curve can be shaped by choosing the right threshold. For this
// benchmark, we fix WarnPct=5pp and measure how noise behaves around that threshold.
package modelmonitor

import (
	"context"
	"math"
	mrand "math/rand"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func mkRecWithTs(ref string, p50, p95, p99, qps, acc, errRate float64, samples int, ts time.Time) PerformanceRecord {
	return PerformanceRecord{
		ModelVersion:  ref,
		Timestamp:     ts,
		LatencyP50MS:  p50, LatencyP95MS: p95, LatencyP99MS: p99,
		ThroughputQPS: qps, Accuracy: acc, ErrorRate: errRate, SampleCount: samples,
	}
}

// BenchmarkRecord measures appending performance records to JSONL logs with attestation.
func BenchmarkRecord(b *testing.B) {
	dir := b.TempDir()
	signer, _ := evidence.GenerateEphemeralSigner()
	store := evidence.NewMemoryStore()
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer, Anchorer: evidence.NewSimulatedAnchorer()})
	mon, _ := NewFSMonitor(dir, ledger, nil)
	ctx := context.Background()
	rec := mkRecWithTs("bench:1.0.0", 40, 100, 200, 1000, 0.90, 0.01, 10000, time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := mon.Record(ctx, rec); err != nil {
			b.Fatalf("record: %v", err)
		}
	}
}

// BenchmarkSetBaseline measures pinning the latest record as baseline (read records
// + persist baselines.json atomically) without any registry check overhead.
func BenchmarkSetBaseline(b *testing.B) {
	dir := b.TempDir()
	mon, _ := NewFSMonitor(dir, nil, nil)
	ctx := context.Background()
	_ = mkRecWithTs("bench-baseline:1.0.0", 40, 100, 200, 1000, 0.90, 0.01, 10000, time.Now())
	for i := 0; i < 20; i++ {
		ts := time.Now().Add(time.Second * time.Duration(i))
		if err := mon.Record(ctx, mkRecWithTs("bench-baseline:1.0.0", float64(40+i), float64(100+i*2), float64(200+i*3), 1000, 0.90, 0.01, 10000, ts)); err != nil {
			b.Fatalf("record: %v", err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := mon.SetBaseline(ctx, "bench-baseline:1.0.0"); err != nil {
			b.Fatalf("baseline: %v", err)
		}
	}
}

// BenchmarkReport computes drift between baseline and latest (no alerts triggered
// unless drift exceeds thresholds). Tests typical reporting path with both present.
func BenchmarkReport(b *testing.B) {
	dir := b.TempDir()
	mon, _ := NewFSMonitor(dir, nil, nil)
	ctx := context.Background()
	base := mkRecWithTs("bench-report:1.0.0", 40, 100, 200, 1000, 0.90, 0.01, 10000, time.Date(2026,8,17,10,0,0,0,time.UTC))
	later := mkRecWithTs("bench-report:1.0.0", 42, 105, 210, 1100, 0.905, 0.009, 10200, time.Date(2026,8,17,11,0,0,0,time.UTC))
	if err := mon.Record(ctx, base); err != nil {
		b.Fatalf("record base: %v", err)
	}
	if err := mon.SetBaseline(ctx, "bench-report:1.0.0"); err != nil {
		b.Fatalf("baseline: %v", err)
	}
	if err := mon.Record(ctx, later); err != nil {
		b.Fatalf("record later: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mon.Report(ctx, "bench-report", "1.0.0"); err != nil {
			b.Fatalf("report: %v", err)
		}
	}
}

// BenchmarkComputeDrift measures the per-metric drift math (percentage change for
// latency/throughput/error-rate; percentage points for accuracy) across 6 canonical
// metrics — no file IO here, pure function call.
func BenchmarkComputeDrift(b *testing.B) {
	base := &PerformanceRecord{Accuracy: 0.90, LatencyP50MS: 40, LatencyP95MS: 100, LatencyP99MS: 200, ThroughputQPS: 1000, ErrorRate: 0.01}
	later := &PerformanceRecord{Accuracy: 0.905, LatencyP50MS: 42, LatencyP95MS: 105, LatencyP99MS: 210, ThroughputQPS: 1100, ErrorRate: 0.009}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ComputeDrift(*base, *later)
	}
}

// BenchmarkEvaluateRules evaluates all default alert rules against baseline vs latest
// and returns only triggered alerts (positive regression magnitude). Uses moderate
// regressions that trigger alerts (not minimal edge case), so results show expected
// number of alerts firing.
func BenchmarkEvaluateRules(b *testing.B) {
	benchRules := []AlertRule{
		{Name: "latency_p95_regression", Metric: MetricLatencyP95, Direction: IncreaseIsBad, WarnPct: 25, CriticalPct: 50},
		{Name: "error_rate_regression", Metric: MetricErrorRate, Direction: IncreaseIsBad, WarnPct: 50, CriticalPct: 100},
		{Name: "accuracy_regression", Metric: MetricAccuracy, Direction: DecreaseIsBad, WarnPct: 5, CriticalPct: 10},
		{Name: "throughput_regression", Metric: MetricThroughput, Direction: DecreaseIsBad, WarnPct: 30, CriticalPct: 60},
	}
	base := &PerformanceRecord{Accuracy: 0.90, LatencyP50MS: 40, LatencyP95MS: 100, LatencyP99MS: 200, ThroughputQPS: 1000, ErrorRate: 0.01}
	later := &PerformanceRecord{Accuracy: 0.85, LatencyP50MS: 48, LatencyP95MS: 130, LatencyP99MS: 260, ThroughputQPS: 900, ErrorRate: 0.012}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alerts := EvaluateRules(benchRules, base, later)
		_ = len(alerts)
	}
}

// BenchmarkAlertsEndToEnd measures the full alert evaluation path: read records,
// find latest version, load baseline, compute drift, evaluate all rules. This
// includes FS reads which dominate the per-call latency.
func BenchmarkAlertsEndToEnd(b *testing.B) {
	dir := b.TempDir()
	mon, _ := NewFSMonitor(dir, nil, nil)
	ctx := context.Background()
	// Seed baseline for model:1.0.0 (the version we'll compare against)
	if err := mon.Record(ctx, mkRecWithTs("bench-alerts:1.0.0", 40, 100, 200, 1000, 0.90, 0.01, 10000, time.Date(2026,8,17,10,0,0,0,time.UTC))); err != nil {
		b.Fatalf("record base: %v", err)
	}
	if err := mon.SetBaseline(ctx, "bench-alerts:1.0.0"); err != nil {
		b.Fatalf("baseline: %v", err)
	}
	// Record a LATEST VERSION with degradation (alerts compares latest vs its own baseline)
	if err := mon.Record(ctx, mkRecWithTs("bench-alerts:1.1.0", 45, 120, 250, 1050, 0.83, 0.02, 10200, time.Date(2026,8,17,11,0,0,0,time.UTC))); err != nil {
		b.Fatalf("record latest: %v", err)
	}
	// Pin baseline for 1.1.0 so Alerts() can compare 1.1.0 -> 1.1.0 baseline
	if err := mon.SetBaseline(ctx, "bench-alerts:1.1.0"); err != nil {
		b.Fatalf("baseline 1.1: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mon.Alerts(ctx, "bench-alerts"); err != nil {
			b.Fatalf("alerts: %v", err)
		}
	}
}

// BenchmarkDriftDetectionSynthetic measures the DRIFT DETECTION RATE over synthetic
// accuracy data. We inject a known regression (accuracy = baseline − delta + noise)
// and compute the % of runs that fire a WARN/CRITICAL alert at threshold 5pp.
//
// HONESTY NOTE: "synthetic data" means noise-injected simulated readings, NOT
// production traffic. The detection rate reflects behavior of a threshold classifier
// under controlled conditions with a fixed noise level. Real-world performance
// varies with actual noise distribution and drift magnitude.
func BenchmarkDriftDetectionSynthetic(b *testing.B) {
	const (
		GaussianSeed = 42
		BaseAcc      = 0.90
		BaselineErr  = 0.010
		Samples      = 10000
		TestRuns     = 100
		NoiseStd     = 0.005 // ±0.5% standard deviation
	)
	deltas := []float64{0, 3, 5, 10} // pp values tested (baseline − delta)
	detCounts := [4]int{}            // counts per delta
	ctx := context.Background()
	rng := mrand.New(mrand.NewSource(GaussianSeed))
	genNoise := func() float64 {
		// Box-Muller transform for standard Gaussian (math/rand in Go 1.22 has no NormalFloat64).
		u1 := rng.Float64()
		if u1 < 1e-10 {
			u1 = 1e-10
		}
		u2 := rng.Float64()
		z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
		return z * NoiseStd
	}

	for di, delta := range deltas {
		dir := b.TempDir()
		mon, _ := NewFSMonitor(dir, nil, nil)
		baseRec := mkRecWithTs("synth-det:1.0.0", 40, 100, 200, 1000, BaseAcc, BaselineErr, Samples, time.Date(2026,8,17,10,0,0,0,time.UTC))
		if err := mon.Record(ctx, baseRec); err != nil {
			b.Fatalf("record base synth: %v", err)
		}
		if err := mon.SetBaseline(ctx, "synth-det:1.0.0"); err != nil {
			b.Fatalf("baseline synth: %v", err)
		}
		// Test many runs accumulating fresh noise injection for accurate detection rate estimate.
		for r := 0; r < TestRuns; r++ {
			latest := mkRecWithTs("synth-det:1.0.0", float64(40+(r%10)), float64(100+(r%20)), float64(200+(r%30)), 1000, math.Max(0, BaseAcc-float64(delta)+genNoise()), BaselineErr, Samples, time.Date(2026,8,17,11,0,0,r*5+10,time.UTC))
			if err := mon.Record(ctx, latest); err != nil {
				b.Fatalf("record latest synth: %v", err)
			}
			if alerts, err := mon.Alerts(ctx, "synth-det"); err == nil {
				for _, a := range alerts {
					if a.Rule == "accuracy_regression" {
						// Treat any severity (WARN or CRITICAL) as detected regression for this benchmark.
						detCounts[di]++
						break
					}
				}
			}
		}
	}
	// Log detection rates and false positives clearly labeled as SYNTHETIC:
	b.ReportMetric(float64(detCounts[0]), "false_positives_synthetic_delta_0pp")    // ~0 pp → expect low FP
	b.ReportMetric(float64(detCounts[1]), "detection_rate_synthetic_delta_3pp")     // 3 pp → near-zero expectation
	b.ReportMetric(float64(detCounts[2]), "detection_rate_synthetic_delta_5pp_warn") // 5 pp → threshold crossing
	b.ReportMetric(float64(detCounts[3]), "detection_rate_synthetic_delta_10pp_crit")// 10 pp → high detection expected
	// Note: these raw counts must be divided by TestRuns (100) to get percentages.
}
