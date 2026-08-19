package deltasync

import "math"

// stats.go provides the statistical machinery required by Task#89: sample
// summaries, Welch's unequal-variance t-test with a real two-sided p-value
// (via the regularized incomplete beta function), and Cohen's d effect size.

// SampleStats summarizes a sample.
type SampleStats struct {
	N      int     `json:"n"`
	Mean   float64 `json:"mean"`
	Var    float64 `json:"var"`  // unbiased (n-1) variance
	StdDev float64 `json:"std"`  // sqrt(Var)
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

// Summarize computes mean/variance/min/max of xs.
func Summarize(xs []float64) SampleStats {
	s := SampleStats{}
	s.N = len(xs)
	if s.N == 0 {
		return s
	}
	s.Min, s.Max = xs[0], xs[0]
	var sum float64
	for _, x := range xs {
		sum += x
		if x < s.Min {
			s.Min = x
		}
		if x > s.Max {
			s.Max = x
		}
	}
	s.Mean = sum / float64(s.N)
	if s.N > 1 {
		var ss float64
		for _, x := range xs {
			d := x - s.Mean
			ss += d * d
		}
		s.Var = ss / float64(s.N-1)
		s.StdDev = math.Sqrt(s.Var)
	}
	return s
}

// TTestResult carries the outcome of Welch's t-test.
type TTestResult struct {
	T       float64 `json:"t"`        // t statistic
	DF      float64 `json:"df"`       // Welch-Satterthwaite degrees of freedom
	PValue  float64 `json:"p_value"`  // two-sided p-value
	CohensD float64 `json:"cohens_d"` // effect size (pooled-SD normalization)
	MeanA   float64 `json:"mean_a"`
	MeanB   float64 `json:"mean_b"`
}

// WelchTTest performs Welch's unequal-variance two-sided t-test comparing a vs b.
func WelchTTest(a, b []float64) TTestResult {
	sa := Summarize(a)
	sb := Summarize(b)
	res := TTestResult{MeanA: sa.Mean, MeanB: sb.Mean}
	if sa.N < 2 || sb.N < 2 {
		res.PValue = 1
		return res
	}
	vaN := sa.Var / float64(sa.N)
	vbN := sb.Var / float64(sb.N)
	se := math.Sqrt(vaN + vbN)
	if se == 0 {
		// identical, zero-variance samples
		res.PValue = 1
		res.CohensD = 0
		return res
	}
	res.T = (sa.Mean - sb.Mean) / se
	// Welch–Satterthwaite degrees of freedom.
	denom := (vaN*vaN)/float64(sa.N-1) + (vbN*vbN)/float64(sb.N-1)
	if denom > 0 {
		res.DF = (vaN + vbN) * (vaN + vbN) / denom
	} else {
		res.DF = float64(sa.N + sb.N - 2)
	}
	res.PValue = studentTTwoSided(res.T, res.DF)
	// Cohen's d with pooled standard deviation.
	pooled := math.Sqrt((sa.Var + sb.Var) / 2)
	if pooled > 0 {
		res.CohensD = (sa.Mean - sb.Mean) / pooled
	}
	return res
}

// ConfidenceInterval95 returns the two-sided 95% confidence interval for the
// population mean of xs, using the Student-t critical value with n-1 degrees of
// freedom: mean ± t*_{0.975,n-1} · s/√n. margin is the half-width. A sample of
// fewer than two points has no dispersion, so a degenerate zero-width interval
// is returned.
func ConfidenceInterval95(xs []float64) (lo, hi, margin float64) {
	s := Summarize(xs)
	if s.N < 2 {
		return s.Mean, s.Mean, 0
	}
	se := s.StdDev / math.Sqrt(float64(s.N))
	tc := tCritical(float64(s.N-1), 0.05)
	margin = tc * se
	return s.Mean - margin, s.Mean + margin, margin
}

// tCritical returns the two-sided critical value t* such that P(|T| > t*) = alpha
// for a Student-t distribution with df degrees of freedom. Because the two-sided
// tail probability studentTTwoSided(·,df) is strictly decreasing in t for t≥0
// (from 1 at t=0 toward 0 as t→∞), the root is found by bisection.
func tCritical(df, alpha float64) float64 {
	if df <= 0 {
		return math.NaN()
	}
	lo, hi := 0.0, 1000.0
	for i := 0; i < 200; i++ {
		mid := (lo + hi) / 2
		if studentTTwoSided(mid, df) > alpha {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// studentTTwoSided returns the two-sided p-value for a t statistic with df
// degrees of freedom, using p = I_{df/(df+t^2)}(df/2, 1/2).
func studentTTwoSided(t, df float64) float64 {
	if df <= 0 {
		return 1
	}
	x := df / (df + t*t)
	return betai(df/2, 0.5, x)
}

// betai is the regularized incomplete beta function I_x(a,b) (Numerical Recipes).
func betai(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	lbeta, _ := math.Lgamma(a + b)
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	bt := math.Exp(lbeta - la - lb + a*math.Log(x) + b*math.Log(1-x))
	if x < (a+1)/(a+b+2) {
		return bt * betacf(a, b, x) / a
	}
	return 1 - bt*betacf(b, a, 1-x)/b
}

// betacf is the continued-fraction expansion used by betai.
func betacf(a, b, x float64) float64 {
	const (
		maxIter = 200
		eps     = 3e-14
		fpmin   = 1e-300
	)
	qab := a + b
	qap := a + 1
	qam := a - 1
	c := 1.0
	d := 1 - qab*x/qap
	if math.Abs(d) < fpmin {
		d = fpmin
	}
	d = 1 / d
	h := d
	for m := 1; m <= maxIter; m++ {
		mf := float64(m)
		m2 := 2 * mf
		aa := mf * (b - mf) * x / ((qam + m2) * (a + m2))
		d = 1 + aa*d
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = 1 + aa/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1 / d
		h *= d * c
		aa = -(a + mf) * (qab + mf) * x / ((a + m2) * (qap + m2))
		d = 1 + aa*d
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = 1 + aa/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < eps {
			break
		}
	}
	return h
}
