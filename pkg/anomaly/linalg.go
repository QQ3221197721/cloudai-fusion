// Package anomaly implements streaming joint anomaly detection with Ledoit-Wolf shrinkage
// and rank-1 Cholesky updates (Task 88 - Algorithm Fortress against sklearn IsolationForest/LOF).
//
// The core idea: IsolationForest and LOF are weak against JOINT anomalies where every marginal
// distribution looks normal but the correlation structure is broken. A single-pass streaming
// Mahalanobis detector, with Ledoit-Wolf shrinkage to condition the covariance and rank-1
// Cholesky updates to keep per-point cost at O(d^2), can beat those offline batch models.
package anomaly

import "math"

// ===========================================================================
// LINEAR ALGEBRA UTILITIES
// ===========================================================================

// dotProduct computes the inner product of two vectors a and b.
func dotProduct(a, b []float64) float64 {
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// copyVector returns a deep copy of vector v.
func copyVector(v []float64) []float64 {
	res := make([]float64, len(v))
	copy(res, v)
	return res
}

// subVectors returns v1 - v2.
func subVectors(v1, v2 []float64) []float64 {
	res := make([]float64, len(v1))
	for i := range res {
		res[i] = v1[i] - v2[i]
	}
	return res
}

// newMatrix allocates a d x d zero matrix.
func newMatrix(d int) [][]float64 {
	m := make([][]float64, d)
	for i := range m {
		m[i] = make([]float64, d)
	}
	return m
}

// matCopy returns a deep copy of matrix A.
func matCopy(A [][]float64) [][]float64 {
	res := make([][]float64, len(A))
	for i := range A {
		res[i] = make([]float64, len(A[i]))
		copy(res[i], A[i])
	}
	return res
}

// ---------------------------------------------------------------------------
// CHOLESKY FACTORIZATION
// ---------------------------------------------------------------------------

// CholeskyDecomposition computes lower-triangular L such that A = L * L^T.
// Returns (L, true) on success; (nil, false) if A is not positive definite.
// Complexity: O(d^3). Used for the offline baseline and periodic refactorization.
func CholeskyDecomposition(A [][]float64) ([][]float64, bool) {
	n := len(A)
	L := newMatrix(n)

	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			sum := A[i][j]
			for k := 0; k < j; k++ {
				sum -= L[i][k] * L[j][k]
			}
			if i == j {
				if sum <= 0 {
					return nil, false
				}
				L[i][j] = math.Sqrt(sum)
			} else {
				L[i][j] = sum / L[j][j]
			}
		}
	}
	return L, true
}

// choleskyOfRegularizedCov returns the Cholesky factor of (S + gamma*I).
// gamma is the diagonal loading (Ledoit-Wolf / Tikhonov style) that guarantees
// positive-definiteness even for high-dimensional small-sample covariances.
func choleskyOfRegularizedCov(S [][]float64, gamma float64) ([][]float64, bool) {
	n := len(S)
	M := matCopy(S)
	for i := 0; i < n; i++ {
		M[i][i] += gamma
	}
	return CholeskyDecomposition(M)
}

// CholeskyRank1Update performs the in-place rank-1 update L*L^T := L*L^T + w*w^T
// in O(d^2), where L is lower triangular. The input slice w is used as scratch and
// is destroyed on return. This is the classic Gill-Golub-Murray-Saunders update.
func CholeskyRank1Update(L [][]float64, w []float64) {
	d := len(w)
	for k := 0; k < d; k++ {
		lkk := L[k][k]
		r := math.Hypot(lkk, w[k])
		c := r / lkk
		s := w[k] / lkk
		L[k][k] = r
		for i := k + 1; i < d; i++ {
			L[i][k] = (L[i][k] + s*w[i]) / c
			w[i] = c*w[i] - s*L[i][k]
		}
	}
}

// scaleCholesky multiplies the Cholesky factor L by factor f, which is equivalent to
// scaling the underlying matrix L*L^T by f^2. Used for exponential-weight decay.
func scaleCholesky(L [][]float64, f float64) {
	for i := range L {
		for j := 0; j <= i; j++ {
			L[i][j] *= f
		}
	}
}

// ---------------------------------------------------------------------------
// TRIANGULAR SOLVES
// ---------------------------------------------------------------------------

// forwardSolve solves L*x = b (L lower triangular) by forward substitution. O(d^2).
func forwardSolve(L [][]float64, b []float64) []float64 {
	n := len(L)
	x := make([]float64, n)
	for i := 0; i < n; i++ {
		sum := b[i]
		for j := 0; j < i; j++ {
			sum -= L[i][j] * x[j]
		}
		x[i] = sum / L[i][i]
	}
	return x
}

// mahalanobisSqFromChol computes the squared Mahalanobis form v^T (L*L^T)^{-1} v
// via a single forward solve: z = L^{-1} v, result = ||z||^2. O(d^2).
func mahalanobisSqFromChol(L [][]float64, v []float64) float64 {
	z := forwardSolve(L, v)
	return dotProduct(z, z)
}

// ---------------------------------------------------------------------------
// MATRIX NORMS AND STATISTICS
// ---------------------------------------------------------------------------

// frobeniusNormSq returns ||A||_F^2 (sum of squared entries).
func frobeniusNormSq(A [][]float64) float64 {
	var sum float64
	for _, row := range A {
		for _, v := range row {
			sum += v * v
		}
	}
	return sum
}

// trace returns the sum of diagonal elements of A.
func trace(A [][]float64) float64 {
	var t float64
	for i := range A {
		t += A[i][i]
	}
	return t
}
