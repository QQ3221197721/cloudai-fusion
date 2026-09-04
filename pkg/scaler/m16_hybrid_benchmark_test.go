// Package scaler - FLIP M16: Hybrid Predictive+Reactive Controller Benchmark
//
// Benchmarks the new O(1) predictive + PI/PD reactive hybrid controller against both the
// reactive Kubernetes HPA simulator AND the pure predictive baseline. Reuses the shared
// fixtures (patterns, series generator, scorer, median helper) from
// m16_flip_benchmark_test.go so both suites measure the exact same workloads.
//
// Run:
//   go test -run=TestFLIPM16HybridCleanWin -v ./pkg/scaler/
//   go test -bench="M16|Scal" -run=^$ -benchtime=1s -count=6 -json ./pkg/scaler/ > output/m16_hybrid_bench.json
package scaler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// m16HybridAccuracy replays the hybrid controller over the full series, then scores it
// with the shared provisioning-lag model (decision[t] serves load[t+1]).
func m16HybridAccuracy(pat m16SpikePattern, series []float64) m16Accuracy {
	ctrl := NewHybridController(DefaultHybridParams(m16TargetPerPod, m16MinReplicas, m16MaxReplicas))
	base := time.Now()
	provisioned := make([]float64, len(series))
	for i := range series {
		r := ctrl.Decide(series[i], base.Add(time.Duration(i)*m16StepInterval))
		provisioned[i] = float64(r)
	}
	return m16ScoreAccuracy(pat, series, provisioned)
}

// m16HybridReport is the structured artifact for the hybrid audit: it records the hybrid
// controller vs BOTH the reactive HPA and the pure predictive baseline per pattern.
type m16HybridPatternVerdict struct {
	Pattern string `json:"pattern"`

	// Latency (ns/op median across count runs).
	HybridLatencyNs float64 `json:"hybrid_latency_ns_median"`
	PureLatencyNs   float64 `json:"pure_predictive_latency_ns_median"`
	HpaLatencyNs    float64 `json:"hpa_latency_ns_median"`
	SpeedupVsHPA    float64 `json:"latency_speedup_x_vs_hpa"`

	// Accuracy: overshoot %, MAE replicas, convergence ms.
	HybridOvershootPct float64 `json:"hybrid_overshoot_pct"`
	PureOvershootPct   float64 `json:"pure_predictive_overshoot_pct"`
	HpaOvershootPct    float64 `json:"hpa_overshoot_pct"`

	HybridMAE float64 `json:"hybrid_mae_replicas"`
	PureMAE   float64 `json:"pure_predictive_mae_replicas"`
	HpaMAE    float64 `json:"hpa_mae_replicas"`

	HybridConvMS int64 `json:"hybrid_convergence_ms"`
	PureConvMS   int64 `json:"pure_predictive_convergence_ms"`
	HpaConvMS    int64 `json:"hpa_convergence_ms"`

	WinsLatency  bool   `json:"wins_latency_vs_hpa"`
	WinsAccuracy bool   `json:"wins_accuracy_vs_hpa"`
	Verdict      string `json:"verdict"` // clean_win | accuracy_win | latency_win | parity | loss
}

type m16HybridReport struct {
	Module         string                    `json:"module"`
	Competitor     string                    `json:"competitor"`
	Design         string                    `json:"design"`
	Count          int                       `json:"count"`
	GeneratedAt    string                    `json:"generated_at"`
	Patterns       []m16HybridPatternVerdict `json:"patterns"`
	CleanWins      int                       `json:"clean_wins"`
	AccuracyWins   int                       `json:"accuracy_wins"`
	LatencyWins    int                       `json:"latency_wins"`
	OverallVerdict string                    `json:"overall_verdict"`
}

// TestFLIPM16HybridCleanWin runs the count=6 audit for the hybrid controller across all
// 6 workload patterns, comparing it head-to-head with reactive HPA (and reporting the pure
// predictive baseline for context), then writes output/m16_hybrid_bench.json.
func TestFLIPM16HybridCleanWin(t *testing.T) {
	const count = 6
	report := m16HybridReport{
		Module:      "M16 Hybrid Auto-scaling (Feedforward Holt-Winters + PI/PD reactive correction)",
		Competitor:  "k8s.io/kubernetes/pkg/controller/podautoscaler (reactive calculateReplicas + 5m downscale stabilization)",
		Design:      "replicas = ceil( ff + Kp*(reactive-ff) + Kd*loadRate ), Kp=0.55 Kd=0.35, O(1) hot path",
		Count:       count,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, pat := range m16Patterns() {
		series := m16GenerateSeries(pat)
		activeLen := len(series) - m16WarmupSteps

		// --- Latency: Hybrid (O(1) HW forecast + PI/PD feedback) ---
		hybridLat := NewHybridController(DefaultHybridParams(m16TargetPerPod, m16MinReplicas, m16MaxReplicas))
		// Warm the forecaster so the timed hot path exercises the steady-state branch.
		hybridLat.Warm(series[:m16WarmupSteps], m16StepInterval)
		baseH := time.Now()
		hybridNs := m16MedianLatencyNs(count, func(step int) {
			load := series[(step%activeLen)+m16WarmupSteps]
			hybridLat.Decide(load, baseH.Add(time.Duration(step)*m16StepInterval))
		})

		// --- Latency: Pure Predictive baseline (HW + safety buffer) ---
		pureHW := m16NewWarmHW(series)
		baseP := time.Now()
		pureNs := m16MedianLatencyNs(count, func(step int) {
			load := series[(step%activeLen)+m16WarmupSteps]
			pureHW.Update(load, baseP.Add(time.Duration(step)*m16StepInterval))
			_, _ = pureHW.RecommendedNodes()
		})

		// --- Latency: Reactive HPA (competitor) ---
		hpaLat := NewKubeHPAScaler(DefaultKubeHPAConfig(), m16BaseReplicas, m16MinReplicas, m16MaxReplicas, m16TargetPerPod)
		baseR := time.Now()
		hpaNs := m16MedianLatencyNs(count, func(step int) {
			load := series[(step%activeLen)+m16WarmupSteps]
			hpaLat.CalculateReplicas(nil, load, baseR.Add(time.Duration(step)*m16StepInterval))
		})

		// --- Accuracy (deterministic replay) ---
		hybridAcc := m16HybridAccuracy(pat, series)
		pureAcc := m16PredictiveAccuracy(pat, series)
		hpaAcc := m16ReactiveAccuracy(pat, series)

		v := m16HybridPatternVerdict{
			Pattern:            pat.name,
			HybridLatencyNs:    hybridNs,
			PureLatencyNs:      pureNs,
			HpaLatencyNs:       hpaNs,
			HybridOvershootPct: hybridAcc.OvershootPct,
			PureOvershootPct:   pureAcc.OvershootPct,
			HpaOvershootPct:    hpaAcc.OvershootPct,
			HybridMAE:          hybridAcc.MAEReplicas,
			PureMAE:            pureAcc.MAEReplicas,
			HpaMAE:             hpaAcc.MAEReplicas,
			HybridConvMS:       hybridAcc.ConvergenceMS,
			PureConvMS:         pureAcc.ConvergenceMS,
			HpaConvMS:          hpaAcc.ConvergenceMS,
		}
		if hybridNs > 0 {
			v.SpeedupVsHPA = hpaNs / hybridNs
		}
		v.WinsLatency = v.SpeedupVsHPA >= 1.05

		// Accuracy win vs HPA: not-worse MAE AND not-worse convergence, strictly better on one.
		betterMAE := hybridAcc.MAEReplicas <= hpaAcc.MAEReplicas
		betterConv := hybridAcc.ConvergenceMS <= hpaAcc.ConvergenceMS
		strictAny := hybridAcc.MAEReplicas < hpaAcc.MAEReplicas || hybridAcc.ConvergenceMS < hpaAcc.ConvergenceMS
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

		t.Logf("[%s] latency: hybrid=%.1fns pure=%.1fns hpa=%.1fns (%.2fx vs hpa) | overshoot: hybrid=%.1f%% pure=%.1f%% hpa=%.1f%% | MAE: hybrid=%.2f pure=%.2f hpa=%.2f | conv: hybrid=%dms hpa=%dms => %s",
			pat.name, hybridNs, pureNs, hpaNs, v.SpeedupVsHPA,
			hybridAcc.OvershootPct, pureAcc.OvershootPct, hpaAcc.OvershootPct,
			hybridAcc.MAEReplicas, pureAcc.MAEReplicas, hpaAcc.MAEReplicas,
			hybridAcc.ConvergenceMS, hpaAcc.ConvergenceMS, v.Verdict)
	}

	total := len(report.Patterns)
	switch {
	case report.CleanWins == total:
		report.OverallVerdict = "CLEAN_WIN_ALL_PATTERNS"
	case report.CleanWins >= (total+1)/2:
		report.OverallVerdict = "CLEAN_WIN_MAJORITY"
	case report.AccuracyWins >= (total+1)/2 && report.LatencyWins >= (total+1)/2:
		report.OverallVerdict = "WIN_BOTH_DIMENSIONS_MAJORITY"
	case report.LatencyWins == total && report.AccuracyWins > 0:
		report.OverallVerdict = "LATENCY_WIN_ACCURACY_IMPROVED"
	case report.LatencyWins == total:
		report.OverallVerdict = "LATENCY_WIN_ACCURACY_MIXED"
	default:
		report.OverallVerdict = "MIXED_OR_PARITY"
	}

	// Write JSON artifact to output/m16_hybrid_bench.json at the workspace root.
	// From pkg/scaler: .. -> pkg, ../.. -> cloudai-fusion, ../../.. -> workspace root.
	outDir := "../../../output"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	outPath := filepath.Join(outDir, "m16_hybrid_bench.json")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	t.Logf("wrote FLIP M16 HYBRID report: %s", outPath)
	fmt.Printf("\n=== FLIP M16 HYBRID OVERALL VERDICT: %s (clean=%d accuracy=%d latency=%d of %d) ===\n",
		report.OverallVerdict, report.CleanWins, report.AccuracyWins, report.LatencyWins, total)

	if report.LatencyWins == 0 {
		t.Errorf("FLIP M16 regression: hybrid lost latency advantage completely")
	}
}
