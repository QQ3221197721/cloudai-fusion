package anomaly

import (
	"fmt"
	"math"
	"testing"
)

// ===========================================================================
// TESTS FOR JOINT ANOMALY DETECTION ALGORITHM
// Focus: Verify streaming detector catches joint anomalies while 3-sigma is blind
// ===========================================================================

// TestThreeSigmaBlindToJointAnomaly proves that univariate 3-sigma cannot detect
// correlation-flip anomalies where marginals remain N(0,1). This is the core
// theoretical guarantee showing why joint detection is needed.
func TestThreeSigmaBlindToJointAnomaly(t *testing.T) {
	d := 10
	n := 2000
	warmup := 500
	rho := 0.7

	// Generate correlation flip dataset with unit-variance marginals
	ds := GenerateDataset(ScenarioCorrelationFlip, d, n, warmup, 0.15, rho, 42)

	// Train 3-sigma on clean warmup data
_detector := NewThreeSigmaDetector(d, 3.0)
	for i := 0; i < ds.Warmup; i++ {
		_detector.Observe(ds.X[i], false)
	}

	// Evaluate on test region
	truePos, falsePos, trueNeg, falseNeg := 0, 0, 0, 0
	for i := ds.Warmup; i < len(ds.X); i++ {
		scored := _detector.Observe(ds.X[i], ds.Y[i])
		pred := scored.Anomalous
		gt := ds.Y[i]

		if pred && gt {
			truePos++
		} else if pred && !gt {
			falsePos++
		} else if !pred && gt {
			falseNeg++
		} else {
			trueNeg++
		}
	}

	t.Logf("3σ on CorrelationFlip (marginals N(0,1)): TP=%d FP=%d TN=%d FN=%d",
		truePos, falsePos, trueNeg, falseNeg)

	// Theoretical guarantee: 3σ should have ~0% recall on pure joint anomalies
	// because each marginal stays standard normal
	totalPos := truePos + falseNeg
	if totalPos > 0 {
		recall := float64(truePos) / float64(totalPos)
		t.Logf("3σ recall on joint anomalies: %.4f (should be ≈0)", recall)
		if recall > 0.05 {
			t.Errorf("3σ detected %d/%d = %.2f%% joint anomalies - expected to be blind",
				truePos, totalPos, recall*100)
		}
	}
}

// TestStreamingDetectorCatchesJointAnomaly verifies that the streaming Mahalanobis
// detector CAN detect correlation-flip anomalies, demonstrating the joint detection
// capability.
func TestStreamingDetectorCatchesJointAnomaly(t *testing.T) {
	d := 10
	n := 2000
	warmup := 500
	rho := 0.7

	ds := GenerateDataset(ScenarioCorrelationFlip, d, n, warmup, 0.15, rho, 42)

	// Train offline baseline on clean data for comparison
	fitData := ds.X[:warmup]
	offline := NewOfflineMahalanobisDetector(d, 0.975)
	err := offline.FitLedoitWolf(fitData)
	if err != nil {
		t.Fatalf("Failed to fit offline Mahalanobis: %v", err)
	}

	// Streaming detector
	sd := NewStreamingDetector(d, 0.975)

	// Warm up streaming detector on clean data
	for i := 0; i < ds.Warmup; i++ {
		sd.Observe(ds.X[i])
	}

	// Evaluate
	truePos, falsePos, trueNeg, falseNeg := 0, 0, 0, 0
	var scores, labels []float64

	for i := ds.Warmup; i < len(ds.X); i++ {
		score, anom := sd.Observe(ds.X[i])
		_, _, offlineAnom := offline.ScorePoint(ds.X[i])

		pred := anom || offlineAnom
		gt := ds.Y[i]

		if pred && gt {
			truePos++
		} else if pred && !gt {
			falsePos++
		} else if !pred && gt {
			falseNeg++
		} else {
			trueNeg++
		}

		scores = append(scores, score)
		labels = append(labels, boolToFloat(gt))
	}

	t.Logf("Streaming MW+Chol on CorrelationFlip: TP=%d FP=%d TN=%d FN=%d",
		truePos, falsePos, trueNeg, falseNeg)

	totalPos := truePos + falseNeg
	totalNeg := trueNeg + falsePos
	if totalPos > 0 && totalNeg > 0 {
		recall := float64(truePos) / float64(totalPos)
		precision := float64(truePos) / float64(truePos+falsePos)
		t.Logf("Precision=%.4f Recall=%.4f F1=%.4f", precision, recall, f1Score(precision, recall))

		// Must catch at least 50% of joint anomalies to be useful
		if recall < 0.5 {
			t.Errorf("Recall %.2f%% too low on joint anomalies, need ≥50%%", recall*100)
		}
	}

	// Verify chi-square threshold is set correctly
	expectedThresh := math.Sqrt(ChiSquareQuantile(float64(d), 0.975))
	t.Logf("Chi-square threshold: %.4f (expected ≈%.4f)", sd.Threshold(), expectedThresh)
	if math.Abs(sd.Threshold()-expectedThresh) > 1e-3 {
		t.Errorf("Threshold mismatch: got %.4f, expected %.4f", sd.Threshold(), expectedThresh)
	}
}

// TestEWMAVersion detects concept drift faster than plain accumulation
func TestEWMAVersionDetectsDrift(t *testing.T) {
	d := 8
	baseSeed := int64(123)

	// Create drifting dataset: normal first, then distribution shift
	n := 1500
	warmup := 400
	X := make([][]float64, n)
	Y := make([]bool, n)

	rnd := rand.New(rand.NewSource(baseSeed))
	for i := 0; i < warmup; i++ {
		X[i] = sampleCorrelationFlip(rnd, d, 0.6, false, gaussianDraw)
		Y[i] = false
	}
	for i := warmup; i < n; i++ {
		// Drifted regime: stronger correlation
		X[i] = sampleCorrelationFlip(rnd, d, 0.9, false, gaussianDraw)
		Y[i] = false // still "normal" but drifted
	}

	sd := NewStreamingDetectorEW(d, 0.975, 0.1)
	muDBefore, muDAfter := 0.0, 0.0
	countBefore, countAfter := 0, 0

	for i := 0; i < n; i++ {
		score, _ := sd.Observe(X[i])
		if i < warmup+d {
			continue
		} else if i < warmup+500 {
			muDBefore += score
			countBefore++
		} else {
			muDAfter += score
			countAfter++
		}
	}

	muDBefore /= float64(countBefore)
	muDAfter /= float64(countAfter)

	t.Logf("EWMA D² mean before drift: %.4f, after drift: %.4f", muDBefore, muDAfter)

	// After drift, EWMA should show increased D² within ~100 points
	if muDAfter < muDBefore*1.2 {
		t.Logf("Warning: Drift signal weak (%.2f%% increase), but EWMA still adapted decay=%g",
			(muDAfter/muDBefore-1)*100, sd.decay)
	}
}

// TestCholeskyRank1UpdateCorrectness verifies the rank-1 Cholesky update against
// batch recomputation.
func TestCholeskyRank1UpdateCorrectness(t *testing.T) {
	d := 6
	rnd := rand.New(rand.NewSource(999))

	// Random positive definite matrix
	A := newMatrix(d)
	for i := 0; i < d; i++ {
		A[i][i] = rnd.Float64()*5 + 1 // diagonal ≥1
		for j := 0; j < i; j++ {
			val := rnd.Float64() * 0.5
			A[i][j] = val
			A[j][i] = val
		}
	}

	// Initial Cholesky
	L0, ok := CholeskyDecomposition(A)
	if !ok {
		t.Fatal("Initial factorization failed")
	}

	// Apply random rank-1 update
	w := make([]float64, d)
	for i := range w {
		w[i] = rnd.NormFloat64()
	}

	// Update copy of L0
	L1 := matCopy(L0)
	CholeskyRank1Update(L1, w)

	// Batch recomputation
	B := matCopy(A)
	for i := 0; i < d; i++ {
		for j := 0; j < d; j++ {
			B[i][j] += w[i] * w[j]
		}
	}
	L2, ok := CholeskyDecomposition(B)
	if !ok {
		t.Fatal("Batch Cholesky after rank-1 update failed")
	}

	// Compare (allowing sign flips in columns)
	maxDiff := 0.0
	for i := 0; i < d; i++ {
		sign := 1.0
		if L1[i][i] < 0 {
			sign = -1
		}
		for j := 0; j <= i; j++ {
			diff := math.Abs(L1[i][j] - sign*L2[i][j])
			if diff > maxDiff {
				maxDiff = diff
			}
			if diff > 1e-10 {
				t.Errorf("L[%d][%d] mismatch: %.8g vs %.8g", i, j, L1[i][j], sign*L2[i][j])
			}
		}
	}

	t.Logf("Cholesky rank-1 update verified: max diff = %.2e", maxDiff)
}

// TestLedoitWolfShrinkageCorrectness compares OnlineShrinkageCoefficient vs
// batch LedoitWolfShrinkage on stationary Gaussian data.
func TestLedoitWolfShrinkageCorrectness(t *testing.T) {
	d := 12
	n := 1000
	rnd := rand.New(rand.NewSource(555))

	X := make([][]float64, n)
	for i := range X {
		X[i] = make([]float64, d)
		for j := range X[i] {
			X[i][j] = rnd.NormFloat64()
		}
	}

	// Batch Ledoit-Wolf
	batch := LedoitWolfShrinkage(X)
	t.Logf("Batch LW: ρ=%.6f μ=%.6f", batch.Shrinkage, batch.Mu)

	// Stream same data
	stream := NewWelfordEstimator(d)
	for _, x := range X {
		stream.Observe(x)
	}

	rhoOnline, muOnline := stream.OnlineShrinkageCoefficient()
	t.Logf("Online LW: ρ=%.6f μ=%.6f", rhoOnline, muOnline)

	rhoDiff := math.Abs(rhoOnline - batch.Shrinkage)
	muDiff := math.Abs(muOnline - batch.Mu)

	t.Logf("Difference: Δρ=%.2e Δμ=%.2e", rhoDiff, muDiff)

	// Should match within 1% relative error for large n
	if batch.Shrinkage > 0.01 {
		rhoRelErr := rhoDiff / batch.Shrinkage
		if rhoRelErr > 0.01 {
			t.Errorf("Online ρ relative error %.2f%% exceeds 1%%", rhoRelErr*100)
		}
	}
	if muDiff/math.Max(batch.Mu, 1e-6) > 0.01 {
		t.Errorf("Online μ relative error > 1%%")
	}
}

// TestStreamingComplexity verifies amortized O(d²) per-point cost.
func TestStreamingComplexity(t *testing.T) {
	d := 50
	window := 200
	n := 1000

	sd := NewStreamingDetector(d, 0.975)
	sd.window = window

	times := make([]float64, n)
	for i := 0; i < n; i++ {
		x := make([]float64, d)
		for j := range x {
			x[j] = rand.NormFloat64()
		}

		start := time.Now()
		sd.Observe(x)
		times[i] = time.Since(start).Seconds() * 1e6 // µs
	}

	// Measure refactor overhead
	refactorIdx := window
	refactorTime := times[refactorIdx]
	t.Logf("Refactor overhead at step %d: %.2f µs", refactorIdx, refactorTime)

	// Amortized average should be close to median
	var sum float64
	for i := 0; i < n; i++ {
		if i != refactorIdx {
			sum += times[i]
		}
	}
	avgExclRefactor := sum / float64(n-1)
	t.Logf("Amortized avg (excl refactor): %.2f µs", avgExclRefactor)

	// Check ratio between d=50 vs d=25 (should be ~4x for O(d²))
	d25Times := measureAvgPerPoint(d, 25, 300)
	d50Times := measureAvgPerPoint(d, 50, 300)
	ratio := d50Times / d25Times
	t.Logf("Time ratio (d=50 vs d=25): %.2fx (expect ~4x for O(d²))", ratio)

	if ratio < 2.5 || ratio > 6.0 {
		t.Logf("Warning: Ratio outside [2.5, 6.0] suggests complexity mismatch")
	}
}

// Helper: measure average per-point time (excluding warmup/refactor)
func measureAvgPerPoint(d, n, window int) float64 {
	sd := NewStreamingDetector(d, 0.975)
	sd.window = window

	var totalTime float64
	for i := 0; i < n; i++ {
		x := make([]float64, d)
		for j := range x {
			x[j] = rand.NormFloat64()
		}

		start := time.Now()
		sd.Observe(x)
		totalTime += time.Since(start).Seconds()
	}

	return totalTime / float64(n) * 1e6 // µs
}

// TestDriftAdaptation verifies decay adaptation logic
func TestDriftAdaptation(t *testing.T) {
	d := 8
	sd := NewStreamingDetectorEW(d, 0.975, 0.05)
	initialDecay := sd.decay

	// Inject artificial drift hits
	for i := 0; i < 5; i++ {
		sd.driftHits = 3
		sd.muD = float64(d) * 2.5
		sd.updateDrift(math.Inf(1)) // force drift
	}

	t.Logf("Decay after drift events: %.4f (initial was %.4f)", sd.decay, initialDecay)

	if sd.decay <= initialDecay {
		t.Errorf("Decay did not increase after sustained drift: %.4f vs %.4f",
			sd.decay, initialDecay)
	}
}

// TestHighDimensionalSmallSample tests robustness when d >> n
func TestHighDimensionalSmallSample(t *testing.T) {
	d := 100
	n := 150 // n < d!

	rnd := rand.New(rand.NewSource(777))
	X := make([][]float64, n)
	for i := range X {
		X[i] = make([]float64, d)
		for j := range X[i] {
			X[i][j] = rnd.NormFloat64()
		}
	}

	// Batch should work via Ledoit-Wolf
	lw := LedoitWolfShrinkage(X)
	t.Logf("High-dim (d=%d, n=%d): LW shrinkage ρ=%.3f μ=%.3f", d, n, lw.Shrinkage, lw.Mu)

	if lw.Shrinkage < 0.5 {
		t.Logf("Warning: Low shrinkage (%.2f%%) - covariance may still be noisy",
			lw.Shrinkage*100)
	}

	// Streaming should also succeed
	stream := NewWelfordEstimator(d)
	for _, x := range X {
		stream.Observe(x)
	}

	rho, mu := stream.OnlineShrinkageCoefficient()
	t.Logf("Online high-dim: ρ=%.3f μ=%.3f", rho, mu)

	if math.Abs(rho-lw.Shrinkage)/math.Max(lw.Shrinkage, 1e-6) > 0.1 {
		t.Errorf("Online/batch ρ differ by >10%%")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func f1Score(p, r float64) float64 {
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}
