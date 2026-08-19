package anomaly

import (
	"math"
	"math/rand"
	"testing"
)

// ===========================================================================
// UNIT TESTS: correctness of the algorithmic building blocks
// ===========================================================================

// TestCholeskyRank1UpdateMatchesBatch verifies the O(d^2) rank-1 Cholesky update
// against a full O(d^3) refactorization of A + w w^T.
func TestCholeskyRank1UpdateMatchesBatch(t *testing.T) {
	rnd := rand.New(rand.NewSource(999))
	d := 6

	A := newMatrix(d)
	for i := 0; i < d; i++ {
		A[i][i] = rnd.Float64()*5 + 2 // dominant diagonal keeps A SPD
		for j := 0; j < i; j++ {
			v := rnd.Float64() * 0.5
			A[i][j] = v
			A[j][i] = v
		}
	}

	L0, ok := CholeskyDecomposition(A)
	if !ok {
		t.Fatal("initial factorization failed")
	}

	w := make([]float64, d)
	for i := range w {
		w[i] = rnd.NormFloat64()
	}

	L1 := matCopy(L0)
	CholeskyRank1Update(L1, w) // w destroyed

	B := matCopy(A)
	for i := 0; i < d; i++ {
		for j := 0; j < d; j++ {
			B[i][j] += (func() float64 { return 0 })() // no-op to keep imports honest
		}
	}
	// rebuild B = A + w0 w0^T using a fresh w0
	w0 := make([]float64, d)
	for i := range w0 {
		w0[i] = 0
	}
	// recompute the original w from L1 vs L0 is hard; instead re-run with a copy
	w2 := make([]float64, d)
	rnd2 := rand.New(rand.NewSource(999))
	// advance rnd2 to the same w draws
	for i := 0; i < d; i++ {
		rnd2.Float64()
		for j := 0; j < i; j++ {
			rnd2.Float64()
		}
	}
	for i := range w2 {
		w2[i] = rnd2.NormFloat64()
	}
	B2 := matCopy(A)
	for i := 0; i < d; i++ {
		for j := 0; j < d; j++ {
			B2[i][j] += w2[i] * w2[j]
		}
	}
	L2, ok := CholeskyDecomposition(B2)
	if !ok {
		t.Fatal("batch factorization failed")
	}

	maxDiff := 0.0
	for i := 0; i < d; i++ {
		for j := 0; j <= i; j++ {
			diff := math.Abs(L1[i][j] - L2[i][j])
			if diff > maxDiff {
				maxDiff = diff
			}
		}
	}
	t.Logf("rank-1 vs batch Cholesky max |diff| = %.3e", maxDiff)
	if maxDiff > 1e-9 {
		t.Errorf("rank-1 Cholesky diverges from batch: max diff %.3e", maxDiff)
	}
}

// TestOnlineShrinkageMatchesBatch checks the streaming Ledoit-Wolf coefficient
// converges to the batch closed form on stationary Gaussian data.
func TestOnlineShrinkageMatchesBatch(t *testing.T) {
	d := 12
	n := 2000
	X := GenerateGaussianNormal(d, n, 555)

	batch := LedoitWolfShrinkage(X)

	stream := NewWelfordEstimator(d)
	for _, x := range X {
		stream.Observe(x)
	}
	rho, mu := stream.OnlineShrinkageCoefficient()

	t.Logf("batch: rho=%.6f mu=%.6f | online: rho=%.6f mu=%.6f", batch.Shrinkage, batch.Mu, rho, mu)
	if batch.Shrinkage > 0.01 {
		if rel := math.Abs(rho-batch.Shrinkage) / batch.Shrinkage; rel > 0.02 {
			t.Errorf("online rho relative error %.2f%% > 2%%", rel*100)
		}
	}
	if rel := math.Abs(mu-batch.Mu) / math.Max(batch.Mu, 1e-9); rel > 0.02 {
		t.Errorf("online mu relative error %.2f%% > 2%%", rel*100)
	}
}

// TestChiSquareQuantileRoundTrip checks quantile/CDF are inverse.
func TestChiSquareQuantileRoundTrip(t *testing.T) {
	for _, df := range []float64{1, 5, 10, 30, 100} {
		for _, p := range []float64{0.5, 0.9, 0.975, 0.99} {
			q := ChiSquareQuantile(df, p)
			back := ChiSquareCDF(q, df)
			if math.Abs(back-p) > 1e-4 {
				t.Errorf("df=%.0f p=%.3f: CDF(Quantile)=%.6f", df, p, back)
			}
		}
	}
}

// ===========================================================================
// CORE CLAIM: 3-sigma is blind to joint anomalies; the streaming detector is not.
// ===========================================================================

// TestThreeSigmaBlindToJointAnomaly proves univariate 3-sigma cannot see a
// correlation-flip anomaly (marginals stay N(0,1)), while the streaming
// Mahalanobis detector achieves high recall on the SAME data.
func TestThreeSigmaBlindStreamingSees(t *testing.T) {
	d := 10
	n := 3000
	warmup := 800
	rho := 0.75

	ds := GenerateDataset(ScenarioCorrelationFlip, d, n, warmup, 0.15, rho, 42)

	// ---- 3-sigma ----
	ts := NewThreeSigmaDetector(d, 3.0)
	var tsPreds, labels []bool
	var tsScores []float64
	for i := 0; i < n; i++ {
		sf := ts.Observe(ds.X[i], ds.Y[i])
		if i >= warmup {
			tsPreds = append(tsPreds, sf.Anomalous)
			tsScores = append(tsScores, sf.Score)
			labels = append(labels, ds.Y[i])
		}
	}
	tsCM := ConfusionFrom(tsPreds, labels)
	tsAUC := AUCROC(tsScores, labels)
	t.Logf("3σ:        P=%.3f R=%.3f F1=%.3f AUC=%.3f (TP=%d FP=%d FN=%d TN=%d)",
		tsCM.Precision(), tsCM.Recall(), tsCM.F1(), tsAUC, tsCM.TP, tsCM.FP, tsCM.FN, tsCM.TN)

	// ---- streaming Mahalanobis ----
	sd := NewStreamingDetector(d, 0.975)
	var sdPreds []bool
	var sdScores []float64
	for i := 0; i < n; i++ {
		score, anom := sd.Observe(ds.X[i])
		if i >= warmup {
			sdPreds = append(sdPreds, anom)
			sdScores = append(sdScores, score)
		}
	}
	sdCM := ConfusionFrom(sdPreds, labels)
	sdAUC := AUCROC(sdScores, labels)
	t.Logf("Streaming: P=%.3f R=%.3f F1=%.3f AUC=%.3f (TP=%d FP=%d FN=%d TN=%d)",
		sdCM.Precision(), sdCM.Recall(), sdCM.F1(), sdAUC, sdCM.TP, sdCM.FP, sdCM.FN, sdCM.TN)

	// 3-sigma recall must be near zero (theoretically blind).
	if tsCM.Recall() > 0.10 {
		t.Errorf("3σ recall %.3f too high — expected blindness to joint anomaly", tsCM.Recall())
	}
	// Streaming AUC must be clearly better than random and than 3σ.
	if sdAUC < 0.75 {
		t.Errorf("streaming AUC %.3f too low; joint anomaly not detected", sdAUC)
	}
	if sdAUC <= tsAUC+0.15 {
		t.Errorf("streaming AUC %.3f not decisively above 3σ AUC %.3f", sdAUC, tsAUC)
	}
}

// TestOfflineUpperBound confirms the offline batch Mahalanobis (clean fit) is a
// strong reference the streaming detector can approach.
func TestOfflineUpperBound(t *testing.T) {
	d := 10
	n := 3000
	warmup := 1000
	ds := GenerateDataset(ScenarioCorrelationFlip, d, n, warmup, 0.15, 0.75, 7)

	off := NewOfflineMahalanobisDetector(d, 0.975)
	if err := off.FitLedoitWolf(ds.X[:warmup]); err != nil {
		t.Fatalf("offline fit failed: %v", err)
	}

	var preds, labels []bool
	var scores []float64
	for i := warmup; i < n; i++ {
		s, _, a := off.ScorePoint(ds.X[i])
		preds = append(preds, a)
		scores = append(scores, s)
		labels = append(labels, ds.Y[i])
	}
	cm := ConfusionFrom(preds, labels)
	auc := AUCROC(scores, labels)
	t.Logf("Offline batch Mahalanobis (upper bound): P=%.3f R=%.3f F1=%.3f AUC=%.3f",
		cm.Precision(), cm.Recall(), cm.F1(), auc)
	if auc < 0.8 {
		t.Errorf("offline upper-bound AUC %.3f unexpectedly low", auc)
	}
}
