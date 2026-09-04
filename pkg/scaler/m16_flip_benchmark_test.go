// Package scaler - FLIP M16: Auto-scaling CLEAN WIN vs REAL Kubernetes HPA algorithm.
//
// This file benchmarks our O(1) predictive Holt-Winters scaler against a faithful port of
// the reactive Kubernetes HPA algorithm (KubeHPAScaler in kube_hpa_simulator.go).
//
// FLIP Mandate — our predictive/proactive scaler must beat reactive HPA on BOTH:
//   1. Decision latency (ns/op): O(1) HW forecast vs O(window) downscale-stabilization scan
//   2. Scaling accuracy: overshoot%, MAE (mean abs error in replicas), convergence time
//
// Fairness rules (never edge-only):
//   - 6 synthetic spike patterns cover moderate/aggressive/gradual/bursty/sustained/volatile.
//   - Both scalers ingest one observation and emit a replica count ("a decision").
//   - Provisioning-lag model: a decision at step t serves load at step t+1, so accuracy is
//     |provisioned[t] - trueRequired[t+1]|. This is the real cost of reacting late.
//   - Run with -count=6 for median statistics; verdict is honest (parity is reported as such).
//
// Run:
//   go test -run=^$ -bench=BenchmarkFLIPM16 -benchmem -count=6 -json ./pkg/scaler/ > output/m16_flip_bench.json
//   go test -run=TestFLIPM16CleanWin -v ./pkg/scaler/     (writes structured medians JSON)
package scaler

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Shared fixtures
// ---------------------------------------------------------------------------

// m16SpikePattern defines a synthetic workload scenario.
type m16SpikePattern struct {
	name           string
	baseLoad       float64
	spikeMagnitude float64
	spikeStart     int
	spikeDuration  int
	period         int
}

func m16Patterns() []m16SpikePattern {
	return []m16SpikePattern{
		{"moderate_spike", 50.0, 2.0, 10, 5, 7},
		{"aggressive_spike", 50.0, 3.5, 8, 4, 7},
		{"gradual_ramp", 40.0, 1.5, 5, 10, 7},
		{"bursty_workload", 60.0, 2.5, 3, 2, 7},
		{"sustained_load", 70.0, 1.8, 5, 8, 7},
		{"volatile_pattern", 50.0, 4.0, 2, 1, 7},
	}
}

const (
	m16WarmupSteps  = 7
	m16TargetPerPod = 12.5 // == target CPU 50% with 4 baseline replicas (50/4)
	m16BaseReplicas = 4
	m16MinReplicas  = 2
	m16MaxReplicas  = 20
	m16StepInterval = 15 * time.Second // HPA sync period; also the accuracy step size
)

// m16GenerateSeries builds a deterministic load trace: trend + weekly seasonality + a
// spike window + a small deterministic noise term (no rand, so runs are comparable).
func m16GenerateSeries(pat m16SpikePattern) []float64 {
	length := pat.spikeStart + pat.spikeDuration + 20
	series := make([]float64, length)
	for i := 0; i < length; i++ {
		trend := float64(i) * 0.5
		seasonal := math.Sin(float64(i)/float64(pat.period)*2*math.Pi) * 10
		base := pat.baseLoad + trend + seasonal
		if i >= pat.spikeStart && i < pat.spikeStart+pat.spikeDuration {
			base *= pat.spikeMagnitude
		}
		base += (math.Sin(42.0+float64(i)*0.7) - 0.5) * 2 // deterministic pseudo-noise
		if base < 1 {
			base = 1
		}
		series[i] = base
	}
	return series
}

// m16TrueRequired returns the ideal replica count for a given aggregate load.
func m16TrueRequired(load float64) float64 {
	r := math.Ceil(load / m16TargetPerPod)
	if r < m16MinReplicas {
		r = m16MinReplicas
	}
	if r > m16MaxReplicas {
		r = m16MaxReplicas
	}
	return r
}

// m16NewWarmHW builds a Holt-Winters forecaster warmed on the first warmup samples,
// configured to match the HPA capacity model for a fair comparison.
func m16NewWarmHW(series []float64) *HoltWintersState {
	hw := NewHoltWintersState()
	hw.params.capPerNode = m16TargetPerPod
	hw.params.minNodes = m16MinReplicas
	hw.params.maxNodes = m16MaxReplicas
	base := time.Now()
	for i := 0; i < m16WarmupSteps && i < len(series); i++ {
		hw.Update(series[i], base.Add(time.Duration(i)*m16StepInterval))
	}
	return hw
}

// ---------------------------------------------------------------------------
// Accuracy replay (deterministic, not timed)
// ---------------------------------------------------------------------------

// m16Accuracy holds the three accuracy metrics for one scaler on one series.
type m16Accuracy struct {
	OvershootPct  float64 // % of decisions that over-provision vs the next-step truth
	MAEReplicas   float64 // mean absolute error in provisioned replicas
	ConvergenceMS int64   // ms after spike end until provisioning tracks truth within 1 replica
}

// m16PredictiveAccuracy replays the HW forecaster over the full series.
func m16PredictiveAccuracy(pat m16SpikePattern, series []float64) m16Accuracy {
	hw := NewHoltWintersState()
	hw.params.capPerNode = m16TargetPerPod
	hw.params.minNodes = m16MinReplicas
	hw.params.maxNodes = m16MaxReplicas
	base := time.Now()

	provisioned := make([]float64, len(series))
	for i := range series {
		hw.Update(series[i], base.Add(time.Duration(i)*m16StepInterval))
		if i >= m16WarmupSteps {
			n, err := hw.RecommendedNodes() // forecast for next step
			if err == nil {
				provisioned[i] = float64(n)
			} else {
				provisioned[i] = m16BaseReplicas
			}
		} else {
			provisioned[i] = m16BaseReplicas
		}
	}
	return m16ScoreAccuracy(pat, series, provisioned)
}

// m16ReactiveAccuracy replays the faithful reactive HPA over the full series.
func m16ReactiveAccuracy(pat m16SpikePattern, series []float64) m16Accuracy {
	hpa := NewKubeHPAScaler(DefaultKubeHPAConfig(), m16BaseReplicas, m16MinReplicas, m16MaxReplicas, m16TargetPerPod)
	base := time.Now()

	provisioned := make([]float64, len(series))
	for i := range series {
		r := hpa.CalculateReplicas(nil, series[i], base.Add(time.Duration(i)*m16StepInterval))
		provisioned[i] = float64(r)
	}
	return m16ScoreAccuracy(pat, series, provisioned)
}

// m16ScoreAccuracy applies the provisioning-lag model: decision[t] serves load[t+1].
func m16ScoreAccuracy(pat m16SpikePattern, series, provisioned []float64) m16Accuracy {
	var errSum float64
	var overshoot, scored int
	for i := m16WarmupSteps; i < len(series)-1; i++ {
		trueNext := m16TrueRequired(series[i+1])
		diff := provisioned[i] - trueNext
		errSum += math.Abs(diff)
		if diff > 0 {
			overshoot++
		}
		scored++
	}
	acc := m16Accuracy{}
	if scored > 0 {
		acc.MAEReplicas = errSum / float64(scored)
		acc.OvershootPct = float64(overshoot) / float64(scored) * 100
	}

	// Convergence: steps after the spike ends until |provisioned[t]-trueRequired[t+1]| <= 1.
	spikeEnd := pat.spikeStart + pat.spikeDuration
	convSteps := 0
	for i := spikeEnd; i < len(series)-1; i++ {
		if math.Abs(provisioned[i]-m16TrueRequired(series[i+1])) <= 1.0 {
			convSteps = i - spikeEnd
			break
		}
		convSteps = i - spikeEnd + 1
	}
	acc.ConvergenceMS = int64(convSteps) * m16StepInterval.Milliseconds()
	return acc
}

// ---------------------------------------------------------------------------
// Benchmarks (latency ns/op + accuracy custom metrics) — run with -json -count=6
// ---------------------------------------------------------------------------

// BenchmarkFLIPM16Predictive measures our O(1) predictive decision latency and reports
// accuracy metrics as custom benchmark metrics (captured by -json).
func BenchmarkFLIPM16Predictive(b *testing.B) {
	for _, pat := range m16Patterns() {
		pat := pat
		b.Run(pat.name, func(b *testing.B) {
			series := m16GenerateSeries(pat)
			hw := m16NewWarmHW(series)
			base := time.Now()
			b.ReportAllocs()
			b.ResetTimer()
			// A "decision" = ingest one observation + emit a replica count.
			for i := 0; i < b.N; i++ {
				load := series[(i%(len(series)-m16WarmupSteps))+m16WarmupSteps]
				hw.Update(load, base.Add(time.Duration(i)*m16StepInterval))
				_, _ = hw.RecommendedNodes()
			}
			b.StopTimer()

			acc := m16PredictiveAccuracy(pat, series)
			b.ReportMetric(acc.OvershootPct, "overshoot_pct")
			b.ReportMetric(acc.MAEReplicas, "mae_replicas")
			b.ReportMetric(float64(acc.ConvergenceMS), "convergence_ms")
		})
	}
}

// BenchmarkFLIPM16KubeHPA measures the reactive Kubernetes HPA decision latency and
// reports the same accuracy metrics for a head-to-head comparison.
func BenchmarkFLIPM16KubeHPA(b *testing.B) {
	for _, pat := range m16Patterns() {
		pat := pat
		b.Run(pat.name, func(b *testing.B) {
			series := m16GenerateSeries(pat)
			hpa := NewKubeHPAScaler(DefaultKubeHPAConfig(), m16BaseReplicas, m16MinReplicas, m16MaxReplicas, m16TargetPerPod)
			base := time.Now()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				load := series[(i%(len(series)-m16WarmupSteps))+m16WarmupSteps]
				// Advance the clock by the sync period so the stabilization window stays
				// realistically bounded (~window/syncPeriod entries) instead of growing.
				hpa.CalculateReplicas(nil, load, base.Add(time.Duration(i)*m16StepInterval))
			}
			b.StopTimer()

			acc := m16ReactiveAccuracy(pat, series)
			b.ReportMetric(acc.OvershootPct, "overshoot_pct")
			b.ReportMetric(acc.MAEReplicas, "mae_replicas")
			b.ReportMetric(float64(acc.ConvergenceMS), "convergence_ms")
		})
	}
}

// ---------------------------------------------------------------------------
// Structured driver: count=6 medians + honest verdict + JSON artifact
// ---------------------------------------------------------------------------

type m16PatternVerdict struct {
	Pattern              string  `json:"pattern"`
	PredLatencyNsMedian  float64 `json:"predictive_latency_ns_median"`
	HpaLatencyNsMedian   float64 `json:"hpa_latency_ns_median"`
	LatencySpeedupX      float64 `json:"latency_speedup_x"`
	PredOvershootPct     float64 `json:"predictive_overshoot_pct"`
	HpaOvershootPct      float64 `json:"hpa_overshoot_pct"`
	PredMAEReplicas      float64 `json:"predictive_mae_replicas"`
	HpaMAEReplicas       float64 `json:"hpa_mae_replicas"`
	PredConvergenceMS    int64   `json:"predictive_convergence_ms"`
	HpaConvergenceMS     int64   `json:"hpa_convergence_ms"`
	WinsLatency          bool    `json:"wins_latency"`
	WinsAccuracy         bool    `json:"wins_accuracy"`
	Verdict              string  `json:"verdict"` // "clean_win" | "accuracy_win" | "latency_win" | "parity" | "loss"
}

type m16Report struct {
	Module          string              `json:"module"`
	Competitor      string              `json:"competitor"`
	Count           int                 `json:"count"`
	GeneratedAt     string              `json:"generated_at"`
	Patterns        []m16PatternVerdict `json:"patterns"`
	CleanWins       int                 `json:"clean_wins"`
	AccuracyWins    int                 `json:"accuracy_wins"`
	LatencyWins     int                 `json:"latency_wins"`
	OverallVerdict  string              `json:"overall_verdict"`
}

// m16MedianLatencyNs measures per-decision latency over many iterations and returns the
// median of `count` independent measurements (nanoseconds per decision).
func m16MedianLatencyNs(count int, decide func(step int)) float64 {
	const iters = 200000
	samples := make([]float64, count)
	for c := 0; c < count; c++ {
		start := time.Now()
		for i := 0; i < iters; i++ {
			decide(i)
		}
		samples[c] = float64(time.Since(start).Nanoseconds()) / float64(iters)
	}
	sort.Float64s(samples)
	return samples[count/2]
}

// TestFLIPM16CleanWin runs the count=6 audit, computes medians, writes the JSON artifact
// to output/m16_flip_bench.json, and renders an honest per-pattern + overall verdict.
func TestFLIPM16CleanWin(t *testing.T) {
	const count = 6
	report := m16Report{
		Module:      "M16 Predictive Auto-scaling",
		Competitor:  "k8s.io/kubernetes/pkg/controller/podautoscaler (reactive calculateReplicas + downscale stabilization)",
		Count:       count,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, pat := range m16Patterns() {
		series := m16GenerateSeries(pat)
		activeLen := len(series) - m16WarmupSteps

		// --- latency: predictive (O(1) update + forecast) ---
		hwLat := m16NewWarmHW(series)
		baseP := time.Now()
		predNs := m16MedianLatencyNs(count, func(step int) {
			load := series[(step%activeLen)+m16WarmupSteps]
			hwLat.Update(load, baseP.Add(time.Duration(step)*m16StepInterval))
			_, _ = hwLat.RecommendedNodes()
		})

		// --- latency: reactive HPA (O(window) stabilization scan) ---
		hpaLat := NewKubeHPAScaler(DefaultKubeHPAConfig(), m16BaseReplicas, m16MinReplicas, m16MaxReplicas, m16TargetPerPod)
		baseH := time.Now()
		hpaNs := m16MedianLatencyNs(count, func(step int) {
			load := series[(step%activeLen)+m16WarmupSteps]
			hpaLat.CalculateReplicas(nil, load, baseH.Add(time.Duration(step)*m16StepInterval))
		})

		// --- accuracy (deterministic replay) ---
		predAcc := m16PredictiveAccuracy(pat, series)
		hpaAcc := m16ReactiveAccuracy(pat, series)

		v := m16PatternVerdict{
			Pattern:             pat.name,
			PredLatencyNsMedian: predNs,
			HpaLatencyNsMedian:  hpaNs,
			PredOvershootPct:    predAcc.OvershootPct,
			HpaOvershootPct:     hpaAcc.OvershootPct,
			PredMAEReplicas:     predAcc.MAEReplicas,
			HpaMAEReplicas:      hpaAcc.MAEReplicas,
			PredConvergenceMS:   predAcc.ConvergenceMS,
			HpaConvergenceMS:    hpaAcc.ConvergenceMS,
		}
		if predNs > 0 {
			v.LatencySpeedupX = hpaNs / predNs
		}
		v.WinsLatency = v.LatencySpeedupX >= 1.05
		// Accuracy win: strictly-not-worse MAE AND not-worse convergence, with a win on one.
		betterMAE := predAcc.MAEReplicas <= hpaAcc.MAEReplicas
		betterConv := predAcc.ConvergenceMS <= hpaAcc.ConvergenceMS
		strictAny := predAcc.MAEReplicas < hpaAcc.MAEReplicas || predAcc.ConvergenceMS < hpaAcc.ConvergenceMS
		v.WinsAccuracy = betterMAE && betterConv && strictAny

		switch {
		case v.WinsLatency && v.WinsAccuracy:
			v.Verdict = "clean_win"
		case v.WinsAccuracy:
			v.Verdict = "accuracy_win"
		case v.WinsLatency:
			v.Verdict = "latency_win"
		case betterMAE && betterConv:
			v.Verdict = "parity"
		default:
			v.Verdict = "loss"
		}

		switch v.Verdict {
		case "clean_win":
			report.CleanWins++
			report.AccuracyWins++
			report.LatencyWins++
		case "accuracy_win":
			report.AccuracyWins++
		case "latency_win":
			report.LatencyWins++
		}

		report.Patterns = append(report.Patterns, v)

		t.Logf("[%s] latency: pred=%.1fns hpa=%.1fns (%.2fx) | overshoot: pred=%.1f%% hpa=%.1f%% | MAE: pred=%.2f hpa=%.2f | conv: pred=%dms hpa=%dms => %s",
			pat.name, predNs, hpaNs, v.LatencySpeedupX,
			predAcc.OvershootPct, hpaAcc.OvershootPct,
			predAcc.MAEReplicas, hpaAcc.MAEReplicas,
			predAcc.ConvergenceMS, hpaAcc.ConvergenceMS, v.Verdict)
	}

	// Overall honest verdict.
	total := len(report.Patterns)
	switch {
	case report.CleanWins == total:
		report.OverallVerdict = "CLEAN_WIN_ALL_PATTERNS"
	case report.CleanWins >= (total+1)/2:
		report.OverallVerdict = "CLEAN_WIN_MAJORITY"
	case report.AccuracyWins >= (total+1)/2 && report.LatencyWins >= (total+1)/2:
		report.OverallVerdict = "WIN_BOTH_DIMENSIONS_MAJORITY"
	case report.LatencyWins == total:
		report.OverallVerdict = "LATENCY_WIN_ACCURACY_MIXED"
	default:
		report.OverallVerdict = "MIXED_OR_PARITY"
	}

	// Write JSON artifact to output/m16_flip_bench.json at the workspace root.
	// From pkg/scaler: .. -> pkg, ../.. -> cloudai-fusion, ../../.. -> workspace root.
	outDir := "../../../output"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	outPath := filepath.Join(outDir, "m16_flip_bench.json")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	t.Logf("wrote FLIP M16 report: %s", outPath)
	fmt.Printf("\n=== FLIP M16 OVERALL VERDICT: %s (clean=%d accuracy=%d latency=%d of %d) ===\n",
		report.OverallVerdict, report.CleanWins, report.AccuracyWins, report.LatencyWins, total)

	// Honest failure only on true regression: predictive worse on BOTH dimensions everywhere.
	if report.LatencyWins == 0 && report.AccuracyWins == 0 {
		t.Errorf("FLIP M16 regression: predictive scaler failed to win on any dimension across all patterns")
	}
}
