package anomaly

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// ===========================================================================
// SKLEARN COMPARISON DATA EXPORTER (Task 91)
//
// Exports the exact synthetic datasets (features + ground-truth labels + the
// streaming evaluation-region flag) that the Go streaming detector is scored on,
// so that Python-side sklearn IsolationForest / LocalOutlierFactor can be run for
// REAL on the SAME data with the SAME labels. It also records the Go detectors'
// metrics per seed into go_metrics.csv, giving a paired basis for Welch t-test /
// Cohen's d / 95% CI against the sklearn numbers.
//
// This test is a data-generation side effect, so it is guarded behind the
// ANOMALY_EXPORT env var and skipped during ordinary `go test` runs:
//
//	$env:ANOMALY_EXPORT="1"; go test ./pkg/anomaly/ -run TestExportSklearnBenchmarkData -v
// ===========================================================================

// sklearnExportConfig fixes the shared experiment geometry. Every detector (Go
// streaming, 3-sigma, offline Mahalanobis, sklearn IF/LOF) is scored on exactly
// the [warmup, n) test region against exactly these labels.
const (
	expD        = 10
	expN        = 3000
	expWarmup   = 800
	expAnomFrac = 0.15
	expRho      = 0.75
	expNumSeeds = 30
)

// TestExportSklearnBenchmarkData writes one CSV per (scenario, seed) plus a
// go_metrics.csv summarizing the Go detectors on the identical evaluation region.
func TestExportSklearnBenchmarkData(t *testing.T) {
	if os.Getenv("ANOMALY_EXPORT") == "" {
		t.Skip("set ANOMALY_EXPORT=1 to (re)generate sklearn comparison datasets")
	}

	scenarios := []Scenario{ScenarioCorrelationFlip, ScenarioElliptical, ScenarioHeavyTail}
	outDir := filepath.Join("testdata", "sklearn")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", outDir, err)
	}

	// go_metrics.csv accumulates one row per (scenario, seed, detector).
	metricsPath := filepath.Join(outDir, "go_metrics.csv")
	mf, err := os.Create(metricsPath)
	if err != nil {
		t.Fatalf("create %s: %v", metricsPath, err)
	}
	defer mf.Close()
	mw := csv.NewWriter(mf)
	_ = mw.Write([]string{"scenario", "d", "rho", "seed", "detector", "precision", "recall", "f1", "auc", "latency_ns"})

	total := 0
	for _, scn := range scenarios {
		for seed := int64(0); seed < expNumSeeds; seed++ {
			ds := GenerateDataset(scn, expD, expN, expWarmup, expAnomFrac, expRho, seed)
			csvName := fmt.Sprintf("%s_d%d_rho%.2f_seed%02d.csv", scenarioToString(scn), expD, expRho, seed)
			if err := writeDatasetCSV(filepath.Join(outDir, csvName), ds); err != nil {
				t.Fatalf("write dataset %s: %v", csvName, err)
			}

			for _, row := range goDetectorMetrics(ds, scn, seed) {
				_ = mw.Write(row)
			}
			total++
		}
	}
	mw.Flush()
	if err := mw.Error(); err != nil {
		t.Fatalf("flush go_metrics.csv: %v", err)
	}
	t.Logf("exported %d datasets + go_metrics.csv to %s", total, outDir)
}

// writeDatasetCSV emits feature_0..feature_{d-1}, label (0/1), is_test (0/1).
// is_test marks the [warmup, n) evaluation region on which every detector is scored.
func writeDatasetCSV(path string, ds *Dataset) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)

	header := make([]string, 0, ds.D+2)
	for j := 0; j < ds.D; j++ {
		header = append(header, "feature_"+strconv.Itoa(j))
	}
	header = append(header, "label", "is_test")
	if err := w.Write(header); err != nil {
		return err
	}

	start, end := ds.TestRange()
	for i := 0; i < len(ds.X); i++ {
		rec := make([]string, 0, ds.D+2)
		for j := 0; j < ds.D; j++ {
			rec = append(rec, strconv.FormatFloat(ds.X[i][j], 'g', 10, 64))
		}
		label := "0"
		if ds.Y[i] {
			label = "1"
		}
		isTest := "0"
		if i >= start && i < end {
			isTest = "1"
		}
		rec = append(rec, label, isTest)
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// goDetectorMetrics runs the streaming, 3-sigma, and offline detectors on ds and
// returns their per-seed metric rows for go_metrics.csv. Latency is the mean
// per-point wall-clock cost measured over the evaluation region.
func goDetectorMetrics(ds *Dataset, scn Scenario, seed int64) [][]string {
	start, end := ds.TestRange()
	var labels []bool
	for i := start; i < end; i++ {
		labels = append(labels, ds.Y[i])
	}

	// ---- Streaming MW+Chol (latency measured on the online Observe path) ----
	sd := NewStreamingDetector(ds.D, 0.975)
	var predsS []bool
	var scoresS []float64
	var streamTestNanos int64
	for i := 0; i < len(ds.X); i++ {
		t0 := time.Now()
		score, anom := sd.Observe(ds.X[i])
		dt := time.Since(t0).Nanoseconds()
		if i >= start {
			predsS = append(predsS, anom)
			scoresS = append(scoresS, score)
			streamTestNanos += dt
		}
	}
	streamLat := float64(streamTestNanos) / float64(len(labels))

	// ---- Adaptive threshold (target quantile 0.85 for top-15% flagging on eval region) ----
	adaptSD := NewStreamingDetectorAdaptive(ds.D, 0.85)
	var adaptPreds []bool
	var adaptScores []float64
	var adaptTestNanos int64
	for i := 0; i < len(ds.X); i++ {
		t0 := time.Now()
		score, anom := adaptSD.Observe(ds.X[i])
		dt := time.Since(t0).Nanoseconds()
		if i >= start {
			adaptPreds = append(adaptPreds, anom)
			adaptScores = append(adaptScores, score)
			adaptTestNanos += dt
		}
	}
	adaptLat := float64(adaptTestNanos) / float64(len(labels))

	// ---- Offline batch Mahalanobis (fit on clean warmup, scores test region) ----
	off := NewOfflineMahalanobisDetector(ds.D, 0.975)
	_ = off.FitLedoitWolf(ds.X[:ds.Warmup])
	var predsO []bool
	var scoresO []float64
	var offTestNanos int64
	for i := start; i < end; i++ {
		t0 := time.Now()
		s, _, a := off.ScorePoint(ds.X[i])
		offTestNanos += time.Since(t0).Nanoseconds()
		predsO = append(predsO, a)
		scoresO = append(scoresO, s)
	}
	offLat := float64(offTestNanos) / float64(len(labels))

	// ---- Univariate 3-sigma ----
	ts := NewThreeSigmaDetector(ds.D, 3.0)
	var predsT []bool
	var scoresT []float64
	var tsTestNanos int64
	for i := 0; i < len(ds.X); i++ {
		t0 := time.Now()
		sf := ts.Observe(ds.X[i], false)
		dt := time.Since(t0).Nanoseconds()
		if i >= start {
			predsT = append(predsT, sf.Anomalous)
			scoresT = append(scoresT, sf.Score)
			tsTestNanos += dt
		}
	}
	tsLat := float64(tsTestNanos) / float64(len(labels))

	mkRow := func(name string, preds []bool, scores []float64, lat float64) []string {
		cm := ConfusionFrom(preds, labels)
		return []string{
			scenarioToString(scn), strconv.Itoa(ds.D), fmt.Sprintf("%.2f", ds.Rho),
			strconv.FormatInt(seed, 10), name,
			fmt.Sprintf("%.6f", cm.Precision()), fmt.Sprintf("%.6f", cm.Recall()),
			fmt.Sprintf("%.6f", cm.F1()), fmt.Sprintf("%.6f", AUCROC(scores, labels)),
			fmt.Sprintf("%.1f", lat),
		}
	}

	return [][]string{
		mkRow("stream", predsS, scoresS, streamLat),
		mkRow("adaptive_0.85", adaptPreds, adaptScores, adaptLat),
		mkRow("offline", predsO, scoresO, offLat),
		mkRow("three_sigma", predsT, scoresT, tsLat),
	}
}
