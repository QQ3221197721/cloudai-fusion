package anomaly

import "math"

// ===========================================================================
// SPECIAL FUNCTIONS
// Self-contained implementations (stdlib math only) so this package does not
// depend on any project quantile package (explicitly out of scope for Task 88).
// ===========================================================================

// ---------------------------------------------------------------------------
// STANDARD NORMAL
// ---------------------------------------------------------------------------

// normalCDF returns the CDF of the standard normal distribution at x.
func normalCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// inverseNormalCDF returns the inverse CDF (probit) of the standard normal.
// Uses the Acklam rational approximation (relative error < 1.15e-9).
func inverseNormalCDF(p float64) float64 {
	if p <= 0 {
		return math.Inf(-1)
	}
	if p >= 1 {
		return math.Inf(+1)
	}

	a := [6]float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := [5]float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := [6]float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := [4]float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}

	pLow := 0.02425
	pHigh := 1 - pLow

	var x float64
	switch {
	case p < pLow:
		q := math.Sqrt(-2 * math.Log(p))
		x = (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= pHigh:
		q := p - 0.5
		r := q * q
		x = (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q := math.Sqrt(-2 * math.Log(1-p))
		x = -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}

	// One step of Halley refinement using the true CDF for higher accuracy.
	e := normalCDF(x) - p
	u := e * math.Sqrt(2*math.Pi) * math.Exp(x*x/2)
	x = x - u/(1+x*u/2)
	return x
}

// ---------------------------------------------------------------------------
// GAMMA / INCOMPLETE GAMMA (for chi-square)
// ---------------------------------------------------------------------------

// lowerRegularizedGamma returns P(a, x) = γ(a, x) / Γ(a), the regularized lower
// incomplete gamma function. Uses series expansion for x < a+1 and the continued
// fraction for the complement otherwise (Numerical Recipes).
func lowerRegularizedGamma(a, x float64) float64 {
	if x <= 0 || a <= 0 {
		return 0
	}
	if x < a+1 {
		// Series representation.
		ap := a
		sum := 1.0 / a
		del := sum
		for n := 0; n < 300; n++ {
			ap++
			del *= x / ap
			sum += del
			if math.Abs(del) < math.Abs(sum)*1e-15 {
				break
			}
		}
		return sum * math.Exp(-x+a*math.Log(x)-logGamma(a))
	}
	// Continued fraction for the upper incomplete gamma Q(a,x); return 1 - Q.
	return 1 - upperRegularizedGammaCF(a, x)
}

// upperRegularizedGammaCF evaluates Q(a,x) via the Lentz continued fraction.
func upperRegularizedGammaCF(a, x float64) float64 {
	const tiny = 1e-30
	b := x + 1 - a
	c := 1 / tiny
	d := 1 / b
	h := d
	for i := 1; i < 300; i++ {
		an := -float64(i) * (float64(i) - a)
		b += 2
		d = an*d + b
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = b + an/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < 1e-15 {
			break
		}
	}
	return math.Exp(-x+a*math.Log(x)-logGamma(a)) * h
}

// logGamma returns ln(Γ(x)) via the Lanczos approximation.
func logGamma(x float64) float64 {
	lg, _ := math.Lgamma(x)
	return lg
}

// ---------------------------------------------------------------------------
// CHI-SQUARE QUANTILE
// ---------------------------------------------------------------------------

// ChiSquareCDF returns the CDF of the chi-square distribution with df degrees
// of freedom evaluated at x.
func ChiSquareCDF(x, df float64) float64 {
	if x <= 0 {
		return 0
	}
	return lowerRegularizedGamma(df/2, x/2)
}

// ChiSquareQuantile returns the p-th quantile of the chi-square distribution with
// df degrees of freedom. It seeds with the Wilson-Hilferty approximation, then
// refines with a few bounded Newton/bisection steps against the exact CDF, so it
// is accurate across the full range (not just the WH regime).
func ChiSquareQuantile(df, p float64) float64 {
	if df <= 0 || p <= 0 {
		return 0
	}
	if p >= 1 {
		return math.Inf(+1)
	}

	// Wilson-Hilferty seed.
	z := inverseNormalCDF(p)
	seed := df * math.Pow(z*math.Sqrt(2/(9*df))+1-2/(9*df), 3)
	if seed <= 0 || math.IsNaN(seed) {
		seed = df
	}

	// Bracket the root and refine with bisection (robust, monotone CDF).
	lo, hi := 0.0, seed
	for ChiSquareCDF(hi, df) < p {
		hi *= 2
		if hi > 1e12 {
			break
		}
	}
	for i := 0; i < 100; i++ {
		mid := 0.5 * (lo + hi)
		if ChiSquareCDF(mid, df) < p {
			lo = mid
		} else {
			hi = mid
		}
		if hi-lo < 1e-10*(1+hi) {
			break
		}
	}
	return 0.5 * (lo + hi)
}

// ---------------------------------------------------------------------------
// INCOMPLETE BETA / STUDENT-T (for Welch t-test p-values)
// ---------------------------------------------------------------------------

// regularizedIncompleteBeta returns I_x(a, b), the regularized incomplete beta
// function, via the Lentz continued fraction (Numerical Recipes).
func regularizedIncompleteBeta(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	lbeta := logGamma(a+b) - logGamma(a) - logGamma(b)
	front := math.Exp(lbeta + a*math.Log(x) + b*math.Log(1-x))
	if x < (a+1)/(a+b+2) {
		return front * betaContinuedFraction(a, b, x) / a
	}
	return 1 - front*betaContinuedFraction(b, a, 1-x)/b
}

func betaContinuedFraction(a, b, x float64) float64 {
	const tiny = 1e-30
	qab := a + b
	qap := a + 1
	qam := a - 1
	c := 1.0
	d := 1 - qab*x/qap
	if math.Abs(d) < tiny {
		d = tiny
	}
	d = 1 / d
	h := d
	for m := 1; m <= 300; m++ {
		fm := float64(m)
		m2 := 2 * fm
		aa := fm * (b - fm) * x / ((qam + m2) * (a + m2))
		d = 1 + aa*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		h *= d * c
		aa = -(a + fm) * (qab + fm) * x / ((a + m2) * (qap + m2))
		d = 1 + aa*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < 1e-15 {
			break
		}
	}
	return h
}

// studentTTwoSidedP returns the two-sided p-value of Student's t distribution with
// df degrees of freedom for statistic t.
func studentTTwoSidedP(t, df float64) float64 {
	if df <= 0 {
		return math.NaN()
	}
	x := df / (df + t*t)
	// P(|T| > |t|) = I_x(df/2, 1/2)
	return regularizedIncompleteBeta(df/2, 0.5, x)
}
