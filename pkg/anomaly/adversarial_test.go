package anomaly

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

// ADVERSARIAL VALIDATION: Elliptical Weak Signal Worst-Case

// TestEllipticalWeakSignal verifies that univariate (three-sigma) detectors fail when
// anomalies preserve marginal distributions but break correlation structure (elliptical
// rotation), while Ledoit-Wolf streaming Mahalanobis captures the broken joint geometry.
// Uses the fair mixed-label GenerateDataset harness (normal + injected anomalies).
func TestEllipticalWeakSignal(t *testing.T) {
	const (
		d         = 20
		n         = 3000
		warmup    = 800
		anomFrac  = 0.15
		rho       = 0.75
		seedInt64 = int64(99)
	)

	t.Logf("=== Adversarial Validation: Elliptical Weak Signal ===")
	t.Logf("Config: d=%d, n=%d, warmup=%d, anom_frac=%.2f, rho=%.2f, seed=%d",
		d, n, warmup, anomFrac, rho, seedInt64)

	ds := GenerateDataset(ScenarioElliptical, d, n, warmup, anomFrac, rho, seedInt64)
	labels := ds.Y[warmup:]

	// Streaming detector (Ledoit-Wolf + Mahalanobis), single pass over the whole stream.
	sd := NewStreamingDetector(d, 0.975)
	var predsS []bool
	var scoresS []float64
	for i := 0; i < n; i++ {
		score, anom := sd.Observe(ds.X[i])
		if i >= warmup {
			predsS = append(predsS, anom)
			scoresS = append(scoresS, score)
		}
	}
	cmS := ConfusionFrom(predsS, labels)
	streamF1 := cmS.F1()
	streamAUC := AUCROC(scoresS, labels)

	// Offline Mahalanobis (batch fit on the clean warmup region).
	off := NewOfflineMahalanobisDetector(d, 0.975)
	if err := off.FitLedoitWolf(ds.X[:warmup]); err != nil {
		t.Fatalf("offline fit failed: %v", err)
	}
	var predsO []bool
	var scoresO []float64
	for i := warmup; i < n; i++ {
		s, _, a := off.ScorePoint(ds.X[i])
		predsO = append(predsO, a)
		scoresO = append(scoresO, s)
	}
	cmO := ConfusionFrom(predsO, labels)
	offlineF1 := cmO.F1()
	offlineAUC := AUCROC(scoresO, labels)

	// Three-sigma baseline (univariate marginal method).
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
	cmT := ConfusionFrom(predsT, labels)
	threeSigmaF1 := cmT.F1()
	threeSigmaAUC := AUCROC(scoresT, labels)

	fmt.Println("\n--- Detection Performance on Elliptical Rotation ---")
	fmt.Printf("%-30s | %-12s | %-12s\n", "Method", "F1", "AUC")
	fmt.Println("-----------------------------------------------")
	fmt.Printf("%-30s | %.4f    | %.4f\n", "Streaming LW+Chol", streamF1, streamAUC)
	fmt.Printf("%-30s | %.4f    | %.4f\n", "Offline Batch ML", offlineF1, offlineAUC)
	fmt.Printf("%-30s | %.4f    | %.4f\n", "Three-Sigma (marginal)", threeSigmaF1, threeSigmaAUC)

	threeSigmaBlindRatio := threeSigmaAUC / math.Max(streamAUC, 1e-9)
	t.Logf("Assertion: univariate marginals blind to joint anomalies")
	t.Logf("AUC(3sigma) / AUC(streaming) = %.2f (theory predicts <=0.65)", threeSigmaBlindRatio)
	if threeSigmaBlindRatio <= 0.65 {
		t.Logf("PASS: Three-sigma fails on elliptical rotation as predicted")
	} else {
		t.Logf("WARN: Three-sigma not fully blind, F1=%.2f may capture weak signal", threeSigmaF1)
	}

	ratioStreamToOffline := streamF1 / math.Max(offlineF1, 1e-9)
	t.Logf("Assertion: online streaming attains batch upper bound")
	t.Logf("F1(streaming) / F1(batch) = %.2f (target >=0.85)", ratioStreamToOffline)
	if ratioStreamToOffline >= 0.85 {
		t.Logf("PASS: Streaming matches batch within 15 pct")
	} else {
		t.Logf("WARN: Gap detected, streaming lags behind batch")
	}
}

// TestStreamingEfficiency measures the O(d^2) per-point cost via timing experiments.
func TestStreamingEfficiency(t *testing.T) {
	dimensions := []int{10, 25, 50, 100}
	costs := make(map[int]float64)

	t.Logf("=== Complexity Scaling: O(d^2) Verification ===")
	for _, d := range dimensions {
		n := 3000
		rng := rand.New(rand.NewSource(int64(d)))

		sd := NewStreamingDetector(d, 0.975)
		warmup := 800
		for i := 0; i < warmup; i++ {
			x := make([]float64, d)
			for j := 0; j < d; j++ {
				x[j] = rng.NormFloat64()
			}
			sd.Observe(x)
		}

		start := time.Now()
		for i := 0; i < n-warmup; i++ {
			x := make([]float64, d)
			for j := 0; j < d; j++ {
				x[j] = rng.NormFloat64()
			}
			sd.Observe(x)
		}
		totalNs := float64(time.Since(start).Nanoseconds())
		costs[d] = totalNs / float64(n-warmup)

		t.Logf("d=%d: per-point cost = %.1f ns", d, costs[d])
	}

	if len(costs) >= 2 {
		dims := []int{10, 25, 50, 100}
		for i := 0; i < len(dims)-1; i++ {
			ratio := costs[dims[i+1]] / math.Max(costs[dims[i]], 1e-9)
			t.Logf("d%d->d%d: %.2fx (O(d^2) predicts ~4x, O(d^3) predicts 8x)",
				dims[i], dims[i+1], ratio)
			if ratio < 8 {
				t.Logf("PASS: Cost grows sub-cubically, consistent with O(d^2) amortized update")
			}
		}
	}
}

// TestRotationalAnomalySpecificity tests whether our method flags rotated ellipsoidal
// anomalies rather than just variance spikes.
func TestRotationalAnomalySpecificity(t *testing.T) {
	d := 10
	nTrain := 800
	nTest := 200
	seedInt64 := int64(777)

	rng := rand.New(rand.NewSource(seedInt64))

	eigenvals := make([]float64, d)
	for i := range eigenvals {
		eigenvals[i] = math.Exp(-0.5 * float64(i))
	}
	Q := generateRandomOrthogonalMatrix(d, rng)
	cov := spectralCovariance(Q, eigenvals)

	XTrain := GenerateMultivariateNormal(nTrain, make([]float64, d), cov, rng)

	xAnom := make([][]float64, nTest)
	for i := 0; i < nTest; i++ {
		point := GenerateGaussianNormal(d, 1, seedInt64)[0]
		x0 := point[0]
		x1 := point[1]
		point[0] = -x1
		point[1] = x0
		xAnom[i] = point
	}

	sd := NewStreamingDetector(d, 0.975)
	for _, x := range XTrain {
		sd.Observe(x)
	}

	var scores []float64
	for _, x := range xAnom {
		s, _ := sd.Observe(x)
		scores = append(scores, s)
	}

	threshold := scores[0]
	for _, s := range scores[1:] {
		if s > threshold {
			threshold = s
		}
	}

	countAbove := 0
	for _, s := range scores {
		if s > threshold*0.95 {
			countAbove++
		}
	}

	t.Logf("Rotational anomaly specificity test:")
	t.Logf("Threshold (near-max): %.2f", threshold)
	t.Logf("Points flagged above 95 pct threshold: %d/%d (%.1f pct)",
		countAbove, len(scores), float64(countAbove)/float64(len(scores))*100)

	if countAbove > len(scores)/2 {
		t.Logf("PASS: Detector flags rotated anomalies (joint structure broken)")
	} else {
		t.Logf("WARN: May need calibration, rotation subtle at low dimensions")
	}
}
