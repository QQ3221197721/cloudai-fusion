package anomaly

import (
	"math"
	"math/rand"
)

// ===========================================================================
// DATA GENERATORS FOR JOINT ANOMALIES AND HEAVY-TAILED DISTRIBUTIONS
//
// Reproducible synthetic data (math/rand with explicit seeds is intentional: the
// experiment demands deterministic, reproducible datasets across >=30 seeds).
// ===========================================================================

// Scenario enumerates the injected-anomaly regimes.
type Scenario int

const (
	// ScenarioCorrelationFlip: marginals are identical N(0,1); anomalies flip the
	// sign of the correlation between dims 0 and 1 (rho -> -rho). This is the
	// canonical JOINT anomaly that univariate 3-sigma is provably blind to.
	ScenarioCorrelationFlip Scenario = iota
	// ScenarioElliptical: normal is a correlated Gaussian; anomalies rotate the
	// principal axes 90 degrees, breaking the joint geometry while keeping means fixed.
	ScenarioElliptical
	// ScenarioHeavyTail: normal is correlated Student-t (heavy tails); anomalies are
	// correlation-flipped Student-t. Tests robustness when data is non-Gaussian.
	ScenarioHeavyTail
)

// Dataset bundles a data matrix, its per-point anomaly labels, and the warmup split.
type Dataset struct {
	X      [][]float64 // n x d matrix
	Y      []bool      // true => injected anomaly
	Warmup int         // first Warmup rows are guaranteed anomaly-free (streaming cold start)
	D      int
	Rho    float64
}

// TestRange returns [Warmup, n) — the region on which all detectors are evaluated.
func (ds *Dataset) TestRange() (start, end int) { return ds.Warmup, len(ds.X) }

// GenerateDataset builds a reproducible dataset for the given scenario.
//
// Layout: rows [0, warmup) are pure-normal (clean streaming warmup / offline fit set).
// Rows [warmup, n) are the evaluation region containing normal points plus a fraction
// anomFrac of injected anomalies at pseudo-random positions. Every detector is scored
// on exactly this region against exactly these labels, so the comparison is fair.
func GenerateDataset(scn Scenario, d, n, warmup int, anomFrac, rho float64, seed int64) *Dataset {
	rnd := rand.New(rand.NewSource(seed))
	X := make([][]float64, n)
	Y := make([]bool, n)

	// Decide which test-region rows are anomalies.
	for i := warmup; i < n; i++ {
		if rnd.Float64() < anomFrac {
			Y[i] = true
		}
	}

	for i := 0; i < n; i++ {
		anom := Y[i]
		switch scn {
		case ScenarioCorrelationFlip:
			X[i] = sampleCorrelationFlip(rnd, d, rho, anom, gaussianDraw)
		case ScenarioElliptical:
			X[i] = sampleElliptical(rnd, d, rho, anom)
		case ScenarioHeavyTail:
			X[i] = sampleCorrelationFlip(rnd, d, rho, anom, studentTDraw(4))
		default:
			X[i] = sampleCorrelationFlip(rnd, d, rho, anom, gaussianDraw)
		}
	}

	return &Dataset{X: X, Y: Y, Warmup: warmup, D: d, Rho: rho}
}

// drawFn returns a single scalar innovation from a distribution driven by rnd.
type drawFn func(rnd *rand.Rand) float64

func gaussianDraw(rnd *rand.Rand) float64 { return rnd.NormFloat64() }

// studentTDraw returns a draw function for the standardized Student-t with nu dof.
// Standardization (multiply by sqrt((nu-2)/nu)) keeps unit variance so that the
// marginals match the Gaussian case in scale, isolating the tail-heaviness effect.
func studentTDraw(nu int) drawFn {
	scale := math.Sqrt(float64(nu-2) / float64(nu))
	return func(rnd *rand.Rand) float64 {
		z := rnd.NormFloat64()
		var chi2 float64
		for k := 0; k < nu; k++ {
			g := rnd.NormFloat64()
			chi2 += g * g
		}
		return scale * z / math.Sqrt(chi2/float64(nu))
	}
}

// sampleCorrelationFlip draws one point where consecutive dimension pairs (0,1),(2,3),...
// have correlation +rho (normal) or -rho (anomaly), while every marginal stays exactly unit
// variance and zero mean. Pairing every dimension (rather than only dims 0,1) makes the
// joint signal scale with d instead of being diluted by noise dimensions, which is the
// honest way to test JOINT detectability: 3-sigma still sees identical N(0,1) marginals.
func sampleCorrelationFlip(rnd *rand.Rand, d int, rho float64, anom bool, draw drawFn) []float64 {
	x := make([]float64, d)
	r := rho
	if anom {
		r = -rho
	}
	root := math.Sqrt(1 - r*r)
	j := 0
	for j+1 < d {
		z0 := draw(rnd)
		z1 := draw(rnd)
		x[j] = z0
		// x_{j+1} = r*z0 + sqrt(1-r^2)*z1 => Var=1, Cov(x_j,x_{j+1})=r, marginals identical.
		x[j+1] = r*z0 + root*z1
		j += 2
	}
	if j < d {
		// Odd trailing dimension: independent unit-variance innovation.
		x[j] = draw(rnd)
	}
	return x
}

// sampleElliptical draws a correlated Gaussian (normal) or a 90-degree-rotated variant
// (anomaly). The rotation swaps the stretched/compressed axes so the joint covariance
// is broken while the coordinate-wise means stay at zero.
func sampleElliptical(rnd *rand.Rand, d int, rho float64, anom bool) []float64 {
	x := make([]float64, d)
	z0 := rnd.NormFloat64()
	z1 := rnd.NormFloat64()
	// Normal correlated pair.
	a := z0
	b := rho*z0 + math.Sqrt(1-rho*rho)*z1
	if anom {
		// Rotate the (a,b) pair by 90 degrees: (a,b) -> (-b, a). This maps the
		// principal correlation axis onto the anti-correlation axis.
		a, b = -b, a
	}
	x[0] = a
	x[1] = b
	for j := 2; j < d; j++ {
		x[j] = rnd.NormFloat64()
	}
	return x
}

// GenerateGaussianNormal creates n i.i.d. samples from N(0, I_d) — a plain helper for tests.
func GenerateGaussianNormal(d, n int, seed int64) [][]float64 {
	rnd := rand.New(rand.NewSource(seed))
	X := make([][]float64, n)
	for i := range X {
		X[i] = make([]float64, d)
		for j := 0; j < d; j++ {
			X[i][j] = rnd.NormFloat64()
		}
	}
	return X
}

// GenerateCorrelatedGaussian creates n samples where dims 0,1 have correlation rho and
// the remaining dims are independent standard normal. Marginals are all N(0,1).
func GenerateCorrelatedGaussian(d, n int, rho float64, seed int64) [][]float64 {
	rnd := rand.New(rand.NewSource(seed))
	X := make([][]float64, n)
	for i := range X {
		X[i] = sampleCorrelationFlip(rnd, d, rho, false, gaussianDraw)
	}
	return X
}
