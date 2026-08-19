package anomaly

import "math"

// ===========================================================================
// BASELINE ALGORITHMS
// sklearn IsolationForest / LOF are run for real via python-engine (sklearn_baseline.py);
// this file provides the pure-Go baselines: univariate 3-sigma and offline batch Mahalanobis.
// ===========================================================================

// scoreAndFlag is the common return shape for streaming baseline observers.
type scoreAndFlag struct {
	Score     float64
	Anomalous bool
}

func newScoreAndFlag(s float64, a bool) scoreAndFlag {
	return scoreAndFlag{Score: s, Anomalous: a}
}

// maxFloat returns max(a, b).
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// BASELINE 1: UNIVARIATE 3-SIGMA (per-feature Welford)
// Provably blind to JOINT anomalies whose marginals stay N(0,1): the whole point
// of the experiment is that this baseline cannot see a broken correlation structure.
// ---------------------------------------------------------------------------

// ThreeSigmaDetector maintains a running mean/variance per dimension and flags a point
// when any coordinate deviates more than k standard deviations. The continuous score is
// the maximum absolute z-score across coordinates (used for AUC).
type ThreeSigmaDetector struct {
	d     int
	k     float64
	count float64
	mean  []float64
	m2    []float64 // sum of squared deviations per dimension.
}

// NewThreeSigmaDetector creates a per-feature 3-sigma detector with threshold multiplier k.
func NewThreeSigmaDetector(d int, k float64) *ThreeSigmaDetector {
	return &ThreeSigmaDetector{
		d:    d,
		k:    k,
		mean: make([]float64, d),
		m2:   make([]float64, d),
	}
}

// Observe updates the per-dimension statistics and returns the max z-score and flag.
// The isAnomaly argument is accepted for interface symmetry but not used (unsupervised).
func (t *ThreeSigmaDetector) Observe(x []float64, _ bool) scoreAndFlag {
	t.count++
	// Per-dimension Welford update.
	for i := 0; i < t.d; i++ {
		delta := x[i] - t.mean[i]
		t.mean[i] += delta / t.count
		t.m2[i] += delta * (x[i] - t.mean[i])
	}
	if t.count < 2 {
		return newScoreAndFlag(0, false)
	}

	maxZ := 0.0
	flagged := false
	for i := 0; i < t.d; i++ {
		std := math.Sqrt(maxFloat(t.m2[i]/(t.count-1), 1e-12))
		z := math.Abs(x[i]-t.mean[i]) / std
		if z > maxZ {
			maxZ = z
		}
		if z > t.k {
			flagged = true
		}
	}
	return newScoreAndFlag(maxZ, flagged)
}

// ---------------------------------------------------------------------------
// BASELINE 2: OFFLINE BATCH MAHALANOBIS (upper-bound reference)
// Fits mean + covariance on a clean batch and scores by Mahalanobis distance. With a
// clean fit set this is the best a Gaussian model can do, so it serves as an upper bound.
// ---------------------------------------------------------------------------

// OfflineMahalanobisDetector fits a global mean and (regularized) covariance on a batch,
// then scores points by their Mahalanobis distance against that fixed model.
type OfflineMahalanobisDetector struct {
	d         int
	mean      []float64
	L         [][]float64 // Cholesky of the regularized covariance.
	lambda    float64     // ridge added to the diagonal for conditioning.
	pthresh   float64
	threshold float64
	fitted    bool
}

// NewOfflineMahalanobisDetector creates an unfitted offline detector.
func NewOfflineMahalanobisDetector(d int, pth float64) *OfflineMahalanobisDetector {
	return &OfflineMahalanobisDetector{
		d:         d,
		lambda:    1e-6,
		pthresh:   pth,
		threshold: math.Sqrt(ChiSquareQuantile(float64(d), pth)),
	}
}

// FitLedoitWolf fits the detector using the Ledoit-Wolf shrunk covariance of X. This is
// the strongest offline reference (well-conditioned even for small n / large d).
func (o *OfflineMahalanobisDetector) FitLedoitWolf(X [][]float64) error {
	n := len(X)
	if n == 0 {
		o.fitted = false
		return &detectorError{"empty fit set"}
	}
	o.mean = make([]float64, o.d)
	for _, row := range X {
		for j := 0; j < o.d; j++ {
			o.mean[j] += row[j]
		}
	}
	for j := 0; j < o.d; j++ {
		o.mean[j] /= float64(n)
	}

	lw := LedoitWolfShrinkage(X)
	L, ok := choleskyOfRegularizedCov(lw.Sigma, o.lambda)
	if !ok {
		L, ok = choleskyOfRegularizedCov(lw.Sigma, o.lambda*1e3)
		if !ok {
			o.fitted = false
			return &detectorError{"covariance not positive definite"}
		}
	}
	o.L = L
	o.fitted = true
	return nil
}

// detectorError is a small error type for the baseline detectors.
type detectorError struct{ msg string }

func (e *detectorError) Error() string { return e.msg }

// ScorePoint returns the Mahalanobis distance (score), its square (D2), and the flag.
func (o *OfflineMahalanobisDetector) ScorePoint(x []float64) (score, d2 float64, anomalous bool) {
	if !o.fitted {
		return 0, 0, false
	}
	v := subVectors(x, o.mean)
	d2 = mahalanobisSqFromChol(o.L, v)
	if d2 < 0 {
		d2 = 0
	}
	score = math.Sqrt(d2)
	return score, d2, score > o.threshold
}
