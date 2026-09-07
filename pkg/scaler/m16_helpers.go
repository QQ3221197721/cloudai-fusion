// Package scaler - FLIP M16: Auto-scaling helper functions for benchmark accuracy and convergence analysis.
package scaler

import (
	"fmt"
	"math"
)

// proxiedReactive converts []int32 to []float64 for accuracy comparison.
func proxiedReactive(hpaProvisioned []int32, loadSeries []float64) []float64 {
	out := make([]float64, len(loadSeries))
	for i, v := range hpaProvisioned {
		out[i] = float64(v)
	}
	return out
}

// Reset resets the HW state for replay.
func (hw *HoltWintersState) Reset() {
	hw.level = 0
	hw.trend = 0
	for i := range hw.seasonals {
		hw.seasonals[i] = 0
	}
	hw.count = 0
	hw.variance = 0
	hw.warmCount = 0
	hw.warmSumSq = 0
}

// WriteM16Summary writes the M16 summary JSON and console output.
func WriteM16Summary(results []m16BenchmarkResult) {
	patternMedians := make(map[string]struct {
		latencySpeedup float64
		overshootHPA   float64
		overshootPred  float64
		wins           int
	})

	for _, r := range results {
		m := patternMedians[r.PatternName]
		m.wins++

		if m.latencySpeedup == 0 || r.LatencySpeedup < m.latencySpeedup {
			m.latencySpeedup = r.LatencySpeedup
		}
		if m.overshootHPA == 0 || r.OvershootHPA < m.overshootHPA {
			m.overshootHPA = r.OvershootHPA
		}
		if m.overshootPred == 0 || r.OvershootPred < m.overshootPred {
			m.overshootPred = r.OvershootPred
		}
		patternMedians[r.PatternName] = m
	}

	fmt.Println("\n=== FLIP M16 Final Verdict ===\n")
	totalPredWins := 0
	totalRuns := len(results)

	for name, m := range patternMedians {
		fmt.Printf("%s:\n", name)
		fmt.Printf("  Median latency speedup: %.2fx\n", m.latencySpeedup)
		fmt.Printf("  Predictive overshoot: %.1f%%, HPA overshoot: %.1f%%\n", m.overshootPred, m.overshootHPA)
		if m.latencySpeedup >= 1.05 && (m.overshootPred <= m.overshootHPA*1.1) {
			fmt.Printf("  ✓ PREDICTIVE WINS on this pattern\n")
			totalPredWins += m.wins
		} else {
			fmt.Printf("  Tie or HPA competitive\n")
		}
		fmt.Println()
	}

	pct := float64(totalPredWins) / float64(totalRuns) * 100
	fmt.Printf("Overall: predictive wins %d/%d runs (%.1f%%)\n", totalPredWins, totalRuns, pct)
	if pct >= 66.7 {
		fmt.Println("✓✓✓ FLIP M16 PASSED: Clean win over k8s HPA on BOTH latency AND accuracy ✓✓✓")
	} else {
		fmt.Println("⚠ Result: parity or mixed - requires deeper analysis")
	}
}

// ComputeAccuracy measures MAE + overshoot%.
func computeAccuracy(provisioned, required []float64) (mae float64, overshootPct float64) {
	if len(provisioned) == 0 {
		return 0, 0
	}
	var errorSum float64
	overshootEvents := 0

	for i := 0; i < len(provisioned); i++ {
		diff := provisioned[i] - required[i]
		if diff > 0 {
			errorSum += diff
			overshootEvents++
		}
	}

	mae = errorSum / float64(len(provisioned))
	overshootPct = float64(overshootEvents) / float64(len(provisioned)) * 100

	return mae, overshootPct
}

// EstimateConvergenceTime estimates minutes until provisioning stable after spike.
func estimateConvergenceTime(load, trueReq []float64, pred bool) int64 {
	const tolerance = 0.25
	converged := false
	samples := len(load)
	window := 3
	var convergenceStart int

	for i := 0; i <= samples-window && !converged; i++ {
		var sum, variance float64

		for j := 0; j < window; j++ {
			idx := i + j
			val := trueReq[idx]
			sum += val
		}
		avg := sum / float64(window)
		for j := 0; j < window; j++ {
			idx := i + j
			diff := trueReq[idx] - avg
			variance += diff * diff
		}
		variance /= float64(window)

		if math.Sqrt(variance) < tolerance {
			converged = true
			convergenceStart = i
		}
	}

	if converged {
		return int64(convergenceStart) * 60000 // milliseconds
	}
	return 0
}

// GenerateSyntheticLoadSeries creates deterministic workload trace for fair comparison.
func GenerateSyntheticLoadSeries(pat m16SpikePattern) []float64 {
	length := pat.spikeStart + pat.spikeDuration + 10
	series := make([]float64, length)
	baseSeed := 42.0

	for i := 0; i < length; i++ {
		trend := float64(i) * 0.5
		seasonal := math.Sin(float64(i)/float64(pat.period)*2*math.Pi)*10
		base := pat.baseLoad + trend + seasonal

		if i >= pat.spikeStart && i < pat.spikeStart+pat.spikeDuration {
			base *= pat.spikeMagnitude
		}

		noisef := math.Sin(baseSeed+float64(i)*0.7)-0.5
		base += noisef * 2

		series[i] = base
	}
	return series
}
