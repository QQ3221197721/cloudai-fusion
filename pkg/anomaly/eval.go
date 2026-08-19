package anomaly

import (
	"math"
	"sort"
)

// ===========================================================================
// EVALUATION METRICS AND STATISTICAL TESTS
// F1/Precision/Recall/AUC-ROC + Welch t-test + Cohen's d for rigorous comparison.
// ===========================================================================

// ConfusionMatrix holds the four outcome counts of a binary classifier.
type ConfusionMatrix struct {
	TP, FP, TN, FN int
}

// Precision returns TP/(TP+FP); 0 if no positive predictions.
func (c ConfusionMatrix) Precision() float64 {
	den := c.TP + c.FP
	if den == 0 {
		return 0
	}
	return float64(c.TP) / float64(den)
}

// Recall returns TP/(TP+FN); 0 if no actual positives.
func (c ConfusionMatrix) Recall() float64 {
	den := c.TP + c.FN
	if den == 0 {
		return 0
	}
	return float64(c.TP) / float64(den)
}

// F1 returns the harmonic mean of precision and recall.
func (c ConfusionMatrix) F1() float64 {
	p, r := c.Precision(), c.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// ConfusionFrom builds a confusion matrix from parallel prediction/label slices.
func ConfusionFrom(preds, labels []bool) ConfusionMatrix {
	var cm ConfusionMatrix
	for i := range preds {
		switch {
		case preds[i] && labels[i]:
			cm.TP++
		case preds[i] && !labels[i]:
			cm.FP++
		case !preds[i] && labels[i]:
			cm.FN++
		default:
			cm.TN++
		}
	}
	return cm
}

// AUCROC computes the area under the ROC curve from continuous scores and binary
// labels using the Mann-Whitney U statistic (rank-based, handles ties by mid-rank).
// Higher score => more anomalous. Returns 0.5 for degenerate single-class input.
func AUCROC(scores []float64, labels []bool) float64 {
	n := len(scores)
	type pair struct {
		s float64
		y bool
	}
	ps := make([]pair, n)
	nPos, nNeg := 0, 0
	for i := 0; i < n; i++ {
		ps[i] = pair{scores[i], labels[i]}
		if labels[i] {
			nPos++
		} else {
			nNeg++
		}
	}
	if nPos == 0 || nNeg == 0 {
		return 0.5
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].s < ps[j].s })

	// Assign mid-ranks (average rank for ties).
	ranks := make([]float64, n)
	i := 0
	for i < n {
		j := i
		for j < n && ps[j].s == ps[i].s {
			j++
		}
		// tie group [i, j): ranks i+1..j, average = (i+1 + j)/2
		avg := float64(i+1+j) / 2.0
		for k := i; k < j; k++ {
			ranks[k] = avg
		}
		i = j
	}

	sumRankPos := 0.0
	for k := 0; k < n; k++ {
		if ps[k].y {
			sumRankPos += ranks[k]
		}
	}
	// U = sumRankPos - nPos*(nPos+1)/2 ; AUC = U / (nPos*nNeg)
	u := sumRankPos - float64(nPos)*float64(nPos+1)/2.0
	return u / (float64(nPos) * float64(nNeg))
}

// ---------------------------------------------------------------------------
// STATISTICAL SIGNIFICANCE
// ---------------------------------------------------------------------------

// SampleStats holds mean, sample variance, and count of a sample.
type SampleStats struct {
	Mean float64
	Var  float64 // unbiased sample variance (n-1)
	N    int
}

// Summarize computes the mean and unbiased variance of x.
func Summarize(x []float64) SampleStats {
	n := len(x)
	if n == 0 {
		return SampleStats{}
	}
	var mean float64
	for _, v := range x {
		mean += v
	}
	mean /= float64(n)
	var ss float64
	for _, v := range x {
		d := v - mean
		ss += d * d
	}
	varc := 0.0
	if n > 1 {
		varc = ss / float64(n-1)
	}
	return SampleStats{Mean: mean, Var: varc, N: n}
}

// WelchTTest performs Welch's two-sample t-test (unequal variances) comparing
// sample a against sample b. Returns the t-statistic, Welch-Satterthwaite degrees
// of freedom, and the two-sided p-value. A positive t means a's mean exceeds b's.
func WelchTTest(a, b []float64) (tStat, df, pValue float64) {
	sa := Summarize(a)
	sb := Summarize(b)
	if sa.N < 2 || sb.N < 2 {
		return 0, 0, math.NaN()
	}
	va := sa.Var / float64(sa.N)
	vb := sb.Var / float64(sb.N)
	denom := va + vb
	if denom <= 0 {
		return 0, 0, 1
	}
	tStat = (sa.Mean - sb.Mean) / math.Sqrt(denom)
	df = denom * denom / (va*va/float64(sa.N-1) + vb*vb/float64(sb.N-1))
	pValue = studentTTwoSidedP(tStat, df)
	return tStat, df, pValue
}

// CohensD returns Cohen's d effect size using the pooled standard deviation of
// samples a and b. |d| >= 0.8 is a large effect, 0.5 medium, 0.2 small.
func CohensD(a, b []float64) float64 {
	sa := Summarize(a)
	sb := Summarize(b)
	if sa.N < 2 || sb.N < 2 {
		return 0
	}
	pooledVar := (float64(sa.N-1)*sa.Var + float64(sb.N-1)*sb.Var) /
		float64(sa.N+sb.N-2)
	if pooledVar <= 0 {
		return 0
	}
	return (sa.Mean - sb.Mean) / math.Sqrt(pooledVar)
}
