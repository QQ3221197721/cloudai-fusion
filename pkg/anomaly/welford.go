package anomaly

import "math"

// ===========================================================================
// STREAMING WELFORD MEAN / COVARIANCE with LEDOIT-WOLF SHRINKAGE
// ===========================================================================

// WelfordEstimator maintains a running mean vector and co-moment matrix in a single
// pass using Welford's numerically stable algorithm generalized to covariance
// (Pébay 2008). It optionally supports exponential weighting (decay) for concept
// drift, and it accumulates the scalar fourth-moment statistic needed to compute
// the Ledoit-Wolf shrinkage coefficient online.
//
// The symmetric rank-1 form of the co-moment update is used:
//
//	delta  = x - mean_old
//	mean  += delta / n
//	C     += ((n-1)/n) * delta * delta^T        (plain accumulation)
//
// which is a genuine symmetric rank-1 update C += w*w^T with w = sqrt((n-1)/n)*delta,
// enabling O(d^2) rank-1 Cholesky maintenance downstream.
type WelfordEstimator struct {
	d     int
	count float64
	mean  []float64
	// C is the co-moment matrix: sum_k (x_k - mean)(x_k - mean)^T (plain), or the
	// exponentially weighted analogue. Sample covariance S = C / (count - 1).
	C [][]float64

	// fourthMoment accumulates sum_k ||x_k - mean||^4 (running, against the current
	// mean at observation time). Used by the online Ledoit-Wolf coefficient.
	fourthMoment float64

	// Exponential weighting parameters. When decay > 0 the estimator uses West's
	// exponentially weighted covariance instead of plain accumulation.
	decay float64 // forgetting factor alpha in (0,1); 0 => plain Welford

	// lastW holds the symmetric rank-1 update vector applied on the most recent
	// Observe call (w such that C changed by +w*w^T), for downstream Cholesky sync.
	lastW []float64
	// lastDecayScale is the multiplicative scale applied to C before the rank-1 add
	// on the most recent Observe (1.0 for plain Welford, (1-decay) for EW).
	lastDecayScale float64
}

// NewWelfordEstimator creates a plain (unweighted) streaming estimator for d dimensions.
func NewWelfordEstimator(d int) *WelfordEstimator {
	return &WelfordEstimator{
		d:     d,
		mean:  make([]float64, d),
		C:     newMatrix(d),
		lastW: make([]float64, d),
	}
}

// NewEWWelfordEstimator creates an exponentially weighted estimator with forgetting
// factor decay (alpha) in (0,1). Larger decay forgets faster (shorter memory ~ 1/decay).
func NewEWWelfordEstimator(d int, decay float64) *WelfordEstimator {
	e := NewWelfordEstimator(d)
	e.decay = decay
	return e
}

// Count returns the number of observed points (or effective count under EW).
func (w *WelfordEstimator) Count() float64 { return w.count }

// Mean returns a copy of the current running mean.
func (w *WelfordEstimator) Mean() []float64 { return copyVector(w.mean) }

// SetDecay changes the forgetting factor at runtime (used by the drift-adaptive path).
func (w *WelfordEstimator) SetDecay(decay float64) { w.decay = decay }

// Observe incorporates a single sample x into the running estimates.
func (w *WelfordEstimator) Observe(x []float64) {
	w.count++
	delta := subVectors(x, w.mean)

	if w.decay > 0 {
		// West (1979) exponentially weighted covariance:
		//   mean <- mean + alpha*delta
		//   C    <- (1-alpha) * (C + alpha * delta delta^T)
		// The uniform (1-alpha) scale on C maps to sqrt(1-alpha) scale on Cholesky,
		// and the rank-1 add uses w = sqrt((1-alpha)*alpha) * delta.
		alpha := w.decay
		for i := 0; i < w.d; i++ {
			w.mean[i] += alpha * delta[i]
		}
		scale := 1 - alpha
		for i := 0; i < w.d; i++ {
			for j := 0; j < w.d; j++ {
				w.C[i][j] = scale * (w.C[i][j] + alpha*delta[i]*delta[j])
			}
		}
		coef := math.Sqrt(scale * alpha)
		for i := 0; i < w.d; i++ {
			w.lastW[i] = coef * delta[i]
		}
		w.lastDecayScale = math.Sqrt(scale)
	} else {
		// Plain Welford.
		invN := 1.0 / w.count
		for i := 0; i < w.d; i++ {
			w.mean[i] += delta[i] * invN
		}
		coef := (w.count - 1) / w.count
		for i := 0; i < w.d; i++ {
			ci := w.C[i]
			ci0 := coef * delta[i]
			for j := 0; j < w.d; j++ {
				ci[j] += ci0 * delta[j]
			}
		}
		sq := math.Sqrt(coef)
		for i := 0; i < w.d; i++ {
			w.lastW[i] = sq * delta[i]
		}
		w.lastDecayScale = 1.0
	}

	// Fourth-moment accumulator (against post-update mean, converges for stationary streams).
	dev := subVectors(x, w.mean)
	nrm2 := dotProduct(dev, dev)
	if w.decay > 0 {
		w.fourthMoment = (1-w.decay)*w.fourthMoment + w.decay*nrm2*nrm2
	} else {
		w.fourthMoment += nrm2 * nrm2
	}
}

// SampleCovariance returns the current sample covariance S = C / (count - 1) for plain
// accumulation, or the normalized EW covariance C when exponentially weighted.
func (w *WelfordEstimator) SampleCovariance() [][]float64 {
	S := matCopy(w.C)
	var denom float64
	if w.decay > 0 {
		denom = 1.0 // C is already the normalized EW covariance
	} else {
		denom = w.count - 1
		if denom < 1 {
			denom = 1
		}
	}
	if denom != 1 {
		inv := 1.0 / denom
		for i := range S {
			for j := range S[i] {
				S[i][j] *= inv
			}
		}
	}
	return S
}

// LastRankUpdate returns the decay scale and rank-1 vector applied on the most recent
// Observe. Downstream code applies: scaleCholesky(L, scale); CholeskyRank1Update(L, w).
func (w *WelfordEstimator) LastRankUpdate() (scale float64, wvec []float64) {
	return w.lastDecayScale, copyVector(w.lastW)
}

// CoMoment returns a copy of the current co-moment matrix C = sum_k (x_k-mean)(x_k-mean)^T
// (or the EW analogue). This is the matrix whose Cholesky the detector rank-1-updates.
func (w *WelfordEstimator) CoMoment() [][]float64 {
	return matCopy(w.C)
}

// ---------------------------------------------------------------------------
// LEDOIT-WOLF SHRINKAGE
// ---------------------------------------------------------------------------

// LedoitWolfResult holds the shrunk covariance estimate and the diagnostic quantities.
type LedoitWolfResult struct {
	// Shrinkage is the optimal shrinkage intensity rho in [0,1].
	Shrinkage float64
	// Mu is the shrinkage target scale = trace(S)/d (average variance).
	Mu float64
	// Sigma is the shrunk covariance: (1-rho)*S + rho*mu*I.
	Sigma [][]float64
}

// LedoitWolfShrinkage computes the Ledoit-Wolf (2004) linear shrinkage estimator toward
// the scaled-identity target mu*I from a data matrix X (rows = samples, cols = dims).
//
// Closed-form derivation (see docs/algorithm-streaming-joint-anomaly.md):
//
//	mu     = trace(S)/d                         (target scale)
//	d2     = ||S - mu*I||_F^2                    (dispersion of S from target)
//	bbar2  = (1/n^2) * sum_k ||x_k x_k^T - S||_F^2   (estimation error of S)
//	b2     = min(bbar2, d2)
//	rho    = b2 / d2                             (optimal shrinkage intensity)
//	Sigma  = (1-rho)*S + rho*mu*I
//
// S here is the population covariance (divide by n), matching sklearn.covariance.ledoit_wolf.
func LedoitWolfShrinkage(X [][]float64) LedoitWolfResult {
	n := len(X)
	if n == 0 {
		return LedoitWolfResult{}
	}
	d := len(X[0])

	// Column means.
	mean := make([]float64, d)
	for _, row := range X {
		for j := 0; j < d; j++ {
			mean[j] += row[j]
		}
	}
	for j := 0; j < d; j++ {
		mean[j] /= float64(n)
	}

	// Population sample covariance S = (1/n) sum (x-mean)(x-mean)^T.
	S := newMatrix(d)
	centered := make([][]float64, n)
	for k, row := range X {
		c := make([]float64, d)
		for j := 0; j < d; j++ {
			c[j] = row[j] - mean[j]
		}
		centered[k] = c
		for i := 0; i < d; i++ {
			ci := c[i]
			Si := S[i]
			for j := 0; j < d; j++ {
				Si[j] += ci * c[j]
			}
		}
	}
	invN := 1.0 / float64(n)
	for i := 0; i < d; i++ {
		for j := 0; j < d; j++ {
			S[i][j] *= invN
		}
	}

	mu := trace(S) / float64(d)

	// d2 = ||S - mu*I||_F^2
	d2 := 0.0
	for i := 0; i < d; i++ {
		for j := 0; j < d; j++ {
			v := S[i][j]
			if i == j {
				v -= mu
			}
			d2 += v * v
		}
	}

	// bbar2 = (1/n^2) sum_k ||x_k x_k^T - S||_F^2
	// For centered x_k: ||x_k x_k^T - S||_F^2 = ||x_k||^4 - 2 x_k^T S x_k + ||S||_F^2.
	sumF := 0.0
	sNormSq := frobeniusNormSq(S)
	for _, c := range centered {
		nrm2 := dotProduct(c, c)
		// x^T S x
		xsx := 0.0
		for i := 0; i < d; i++ {
			var acc float64
			Si := S[i]
			for j := 0; j < d; j++ {
				acc += Si[j] * c[j]
			}
			xsx += c[i] * acc
		}
		sumF += nrm2*nrm2 - 2*xsx + sNormSq
	}
	bbar2 := sumF / (float64(n) * float64(n))

	b2 := math.Min(bbar2, d2)
	rho := 0.0
	if d2 > 0 {
		rho = b2 / d2
	}
	if rho < 0 {
		rho = 0
	}
	if rho > 1 {
		rho = 1
	}

	sigma := matCopy(S)
	for i := 0; i < d; i++ {
		for j := 0; j < d; j++ {
			sigma[i][j] *= (1 - rho)
			if i == j {
				sigma[i][j] += rho * mu
			}
		}
	}

	return LedoitWolfResult{Shrinkage: rho, Mu: mu, Sigma: sigma}
}

// OnlineShrinkageCoefficient computes the Ledoit-Wolf shrinkage intensity from the
// streaming sufficient statistics accumulated by the WelfordEstimator, without a
// second pass over data. It uses:
//
//	S       = C / n                              (population covariance)
//	mu      = trace(S)/d
//	d2      = ||S - mu*I||_F^2
//	bbar2   = (1/n^2) * (Q - n*||S||_F^2)         where Q = sum_k ||x_k - mean||^4
//
// The identity sum_k x_k^T S x_k = n*||S||_F^2 (for centered x_k) yields the compact
// bbar2 above; Q is the accumulated fourthMoment. This is an online approximation
// because the running mean drifts, but it converges to the batch value on stationary
// streams (validated in tests against LedoitWolfShrinkage / sklearn).
func (w *WelfordEstimator) OnlineShrinkageCoefficient() (rho, mu float64) {
	n := w.count
	if n < 2 {
		return 1, 0
	}
	// Population covariance S = C / n.
	S := newMatrix(w.d)
	inv := 1.0 / n
	for i := 0; i < w.d; i++ {
		for j := 0; j < w.d; j++ {
			S[i][j] = w.C[i][j] * inv
		}
	}
	mu = trace(S) / float64(w.d)
	d2 := 0.0
	for i := 0; i < w.d; i++ {
		for j := 0; j < w.d; j++ {
			v := S[i][j]
			if i == j {
				v -= mu
			}
			d2 += v * v
		}
	}
	sNormSq := frobeniusNormSq(S)
	bbar2 := (w.fourthMoment - n*sNormSq) / (n * n)
	if bbar2 < 0 {
		bbar2 = 0
	}
	b2 := math.Min(bbar2, d2)
	if d2 > 0 {
		rho = b2 / d2
	} else {
		rho = 1
	}
	if rho < 0 {
		rho = 0
	}
	if rho > 1 {
		rho = 1
	}
	return rho, mu
}
