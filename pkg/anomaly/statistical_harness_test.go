package anomaly

import (
	"encoding/csv"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// BenchmarkStreamingVsBaseline runs a full statistical comparison across many seeds,
// comparing streaming detector performance against univariate 3-sigma and offline batch
// Mahalanobis (upper bound).
func BenchmarkStreamingVsBaseline(b *testing.B) {
	scenarios := []Scenario{ScenarioCorrelationFlip, ScenarioElliptical, ScenarioHeavyTail}
	dims := []int{10, 20}
	rhos := []float64{0.5, 0.75}
	frac := 0.15
	numSeeds := int64(30)

	var results []BenchmarkResult

	for _, scn := range scenarios {
		for _, d := range dims {
			for _, rho := range rhos {
				for seed := int64(0); seed < numSeeds; seed++ {
					res := runSeed(scn, d, nTest, warmupDefault, frac, rho, seed)
					results = append(results, res)
				}
			}
		}
	}

	// Write combined results to CSV for external analysis/sklearn integration.
	csvPath := filepath.Join("testdata", "benchmark_streaming_vs_baseline.csv")
	os.MkdirAll(filepath.Dir(csvPath), 0o755)
	f, err := os.Create(csvPath)
	if err != nil {
		b.Fatalf("create csv failed: %v", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	_ = w.Write([]string{"scenario", "d", "rho", "seed", "stream_f1", "stream_auc", "offline_f1", "offline_auc", "three_sigma_f1", "three_sigma_auc"})
	for _, r := range results {
		_ = w.Write([]string{
			scenarioToString(r.scn), strconv.Itoa(r.d), fmt.Sprintf("%.2f", r.rho), strconv.FormatInt(r.seed, 10),
			fmt.Sprintf("%.4f", r.streamF1), fmt.Sprintf("%.4f", r.streamAUC),
			fmt.Sprintf("%.4f", r.offlineF1), fmt.Sprintf("%.4f", r.offlineAUC),
			fmt.Sprintf("%.4f", r.threeSigmaF1), fmt.Sprintf("%.4f", r.threeSigmaAUC),
		})
	}
	w.Flush()
	b.Logf("Wrote %d seeds to %s", len(results), csvPath)

	// Compute p-values and effect sizes for key comparisons.
	compareGroupsByMetric(results, b)

	b.SetBytes(int64(nTest))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// No-op loop to measure baseline overhead; real work done above.
	}
}

const (
	nTest         = 3000
	warmupDefault = 800
)

// BenchmarkResult holds per-seed metrics for one configuration.
type BenchmarkResult struct {
	scn            Scenario
	d              int
	rho            float64
	seed           int64
	streamF1       float64
	streamAUC      float64
	offlineF1      float64
	offlineAUC     float64
	threeSigmaF1   float64
	threeSigmaAUC  float64
}

func scenarioToString(s Scenario) string {
	switch s {
	case ScenarioCorrelationFlip:
		return "correlation_flip"
	case ScenarioElliptical:
		return "elliptical"
	case ScenarioHeavyTail:
		return "heavy_tail"
	default:
		return "unknown"
	}
}

// compareGroupsByMetric computes Welch t-test and Cohen's d between groups.
func compareGroupsByMetric(results []BenchmarkResult, b *testing.B) {
	// Compare streaming vs offline on F1 for correlation-flip only.
	var strF1s, offF1s []float64
	for _, r := range results {
		if r.scn == ScenarioCorrelationFlip {
			strF1s = append(strF1s, r.streamF1)
			offF1s = append(offF1s, r.offlineF1)
		}
	}
	if len(strF1s) >= 2 && len(offF1s) >= 2 {
		tStat, df, pVal := WelchTTest(strF1s, offF1s)
		coh := CohensD(strF1s, offF1s)
		b.Logf("CFLP: streaming vs offline F1: t=%.3f df=%.1f p=%.4f CohensD=%.3f", tStat, df, pVal, coh)
		if !math.IsNaN(pVal) && pVal < 0.05 {
			b.Logf("Difference statistically significant at α=0.05")
		}
	}
}

// runSeed executes one complete experiment with a given random seed.
func runSeed(scn Scenario, d, n, warmup int, anomFrac, rho float64, seed int64) BenchmarkResult {
	rs := rand.New(rand.NewSource(seed))
	_ = rs // keep rnd alive for potential expansion

	ds := GenerateDataset(scn, d, n, warmup, anomFrac, rho, seed)

	// ---- Streaming MW+Chol ----
	sd := NewStreamingDetector(d, 0.975)
	var predsS []bool
	var scoresS []float64
	var labelsY []bool
	for i := 0; i < n; i++ {
		score, anom := sd.Observe(ds.X[i])
		if i >= warmup {
			predsS = append(predsS, anom)
			scoresS = append(scoresS, score)
			labelsY = append(labelsY, ds.Y[i])
		}
	}
	cmS := ConfusionFrom(predsS, labelsY)
	aucS := AUCROC(scoresS, labelsY)

	// Offline batch Mahalanobis (upper bound)
	off := NewOfflineMahalanobisDetector(d, 0.975)
	_ = off.FitLedoitWolf(ds.X[:warmup])
	var predsO []bool
	var scoresO []float64
	for i := warmup; i < n; i++ {
		s, _, a := off.ScorePoint(ds.X[i])
		predsO = append(predsO, a)
		scoresO = append(scoresO, s)
	}
	cmO := ConfusionFrom(predsO, labelsY)
	aucO := AUCROC(scoresO, labelsY)

	// Three sigma
	ts := NewThreeSigmaDetector(d, 3.0)
	var predsT []bool
	var scoresT []float64
	for i := 0; i < n; i++ {
		sf := ts.Observe(ds.X[i], false)
		if i >= warmup {
			predsT = append(predsT, sf.Anomalous)
			scoresT = append(scoresT, sf.Score)
		}
	}
	cmT := ConfusionFrom(predsT, labelsY)
	aucT := AUCROC(scoresT, labelsY)

	return BenchmarkResult{
		scn: scn, d: d, rho: rho, seed: seed,
		streamF1: cmS.F1(), streamAUC: aucS,
		offlineF1: cmO.F1(), offlineAUC: aucO,
		threeSigmaF1: cmT.F1(), threeSigmaAUC: aucT,
	}
}
