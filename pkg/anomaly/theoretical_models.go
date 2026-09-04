// Package anomaly - adversarial validation generators (Task #261).
// This file provides theoretically-motivated synthetic datasets that expose the
// weaknesses of Isolation Forest and LOF while favoring Ledoit-Wolf streaming
// Mahalanobis detection.
//
// Scenarios:
//  1. Elliptical rotation - joint anomalies with normal marginals (IF blind, LOF collapse)
//  2. Heavy-tailed shell perturbations - outliers requiring quadratic-form scoring
//
// All generators are additive (new file only); no production code is modified.
package anomaly

import (
	"math"
	"math/rand"
)

// ============================================================================
// ADVERSARIAL SYNTHETIC DATASETS FOR STATISTICAL BENCHMARKING
// ============================================================================

// GenerateEllipticalRotation creates a worst-case scenario for Isolation Forest / LOF:
//   - Training data: Gaussian N(0, Sigma) with correlated features (elliptical shell)
//   - Test anomalies: points drawn from the INVERSE-spectrum covariance, preserving
//     marginal scale but rotating the principal axes so the joint correlation breaks.
//
// Why IF/LOF fail: each dimension remains roughly standard-normal marginally; only the
// joint correlation is broken. Mahalanobis scoring captures this via v^T S^{-1} v.
func GenerateEllipticalRotation(d int, nTrain, nTest int, seed int64) (XTrain, XTest [][]float64, YTest []bool) {
	rng := rand.New(rand.NewSource(seed))

	// Eigenvalues decay exponentially: lambda_1 ~ 1, lambda_d ~ 0.08.
	eigenvals := make([]float64, d)
	for i := range eigenvals {
		eigenvals[i] = math.Exp(-0.5 * float64(i))
	}

	// Random orthogonal basis Q (QR of a random Gaussian matrix).
	Q := generateRandomOrthogonalMatrix(d, rng)

	// Training covariance = Q diag(lambda) Q^T.
	cov := spectralCovariance(Q, eigenvals)

	// Anomaly covariance = Q diag(1/lambda) Q^T (inverse spectrum => rotated ellipsoid).
	invEigen := make([]float64, d)
	for i := range eigenvals {
		invEigen[i] = 1.0 / math.Max(eigenvals[i], 1e-6)
	}
	covAnom := spectralCovariance(Q, invEigen)

	XTrain = GenerateMultivariateNormal(nTrain, make([]float64, d), cov, rng)
	XTest = GenerateMultivariateNormal(nTest, make([]float64, d), covAnom, rng)
	YTest = make([]bool, nTest)
	for i := range YTest {
		YTest[i] = true // all test points are anomalous (rotated ellipsoid)
	}
	return XTrain, XTest, YTest
}

// GenerateHeavyTailAnomalies constructs outliers via a Student-t mixture on an
// elliptical shell. Training is light-tailed Gaussian; anomalies are t(3)-distributed
// scaled to comparable marginal variance, so IF over/under-flags on local density
// while Mahalanobis (with LW shrinkage) scores them robustly.
func GenerateHeavyTailAnomalies(d int, nTrain, nTest int, seed int64) (XTrain, XTest [][]float64, YTest []bool) {
	rng := rand.New(rand.NewSource(seed))

	XTrain = GenerateGaussianNormal(d, nTrain, seed)
	XTest = make([][]float64, nTest)
	YTest = make([]bool, nTest)

	tScale := math.Sqrt(float64(d) / 3.0) // approximate normalization
	for i := 0; i < nTest; i++ {
		x := make([]float64, d)
		for j := 0; j < d; j++ {
			x[j] = studentT(rng, 3) * tScale
		}
		XTest[i] = x
		YTest[i] = true
	}
	return XTrain, XTest, YTest
}

// ============================================================================
// UTILITY GENERATORS
// ============================================================================

// spectralCovariance reconstructs Q diag(vals) Q^T for orthogonal Q.
func spectralCovariance(Q [][]float64, vals []float64) [][]float64 {
	d := len(vals)
	cov := newMatrix(d)
	for i := 0; i < d; i++ {
		for j := 0; j < d; j++ {
			sum := 0.0
			for k := 0; k < d; k++ {
				sum += Q[i][k] * vals[k] * Q[j][k]
			}
			cov[i][j] = sum
		}
	}
	return cov
}

// generateRandomOrthogonalMatrix produces a d x d orthogonal matrix via modified
// Gram-Schmidt on a random Gaussian matrix. math/rand is used only for synthetic
// test data generation, not for any security-sensitive purpose.
func generateRandomOrthogonalMatrix(d int, rng *rand.Rand) [][]float64 {
	A := newMatrix(d)
	for i := 0; i < d; i++ {
		for j := 0; j < d; j++ {
			A[i][j] = rng.NormFloat64()
		}
	}
	// Rows of A are orthonormalized in place (treat each row as a vector).
	for j := 0; j < d; j++ {
		for i := 0; i < j; i++ {
			dot := dotProduct(A[i], A[j])
			for k := 0; k < d; k++ {
				A[j][k] -= dot * A[i][k]
			}
		}
		normSq := dotProduct(A[j], A[j])
		if normSq > 1e-30 {
			scale := 1.0 / math.Sqrt(normSq)
			for k := 0; k < d; k++ {
				A[j][k] *= scale
			}
		}
	}
	return A
}

// GenerateMultivariateNormal draws n samples from N(mu, Sigma) via Cholesky.
// Sigma is assumed symmetric positive-(semi)definite; a small ridge is applied on
// numerical indefiniteness.
func GenerateMultivariateNormal(n int, mu []float64, Sigma [][]float64, rng *rand.Rand) [][]float64 {
	d := len(mu)
	L, ok := CholeskyDecomposition(Sigma)
	if !ok {
		reg := matCopy(Sigma)
		for i := 0; i < d; i++ {
			reg[i][i] += 1e-6
		}
		L, _ = CholeskyDecomposition(reg)
	}

	X := make([][]float64, n)
	z := make([]float64, d)
	for i := 0; i < n; i++ {
		for j := 0; j < d; j++ {
			z[j] = rng.NormFloat64()
		}
		row := make([]float64, d)
		copy(row, mu)
		for r := 0; r < d; r++ {
			sum := 0.0
			for c := 0; c <= r; c++ { // L lower-triangular
				sum += L[r][c] * z[c]
			}
			row[r] += sum
		}
		X[i] = row
	}
	return X
}

// studentT returns a Student-t(df) variate via the Normal / Chi-square ratio:
// T = Z / sqrt(V/df), V ~ Chi2(df). math/rand suffices for synthetic data.
func studentT(rng *rand.Rand, df int) float64 {
	z := rng.NormFloat64()
	v := 0.0
	for i := 0; i < df; i++ {
		g := rng.NormFloat64()
		v += g * g
	}
	v /= float64(df)
	if v <= 1e-12 {
		return z
	}
	return z / math.Sqrt(v)
}
