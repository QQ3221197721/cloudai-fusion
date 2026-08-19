package mlops

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// ============================================================================
// M20 Model Performance Monitor — drift detection
// ============================================================================
//
// Two complementary statistics are provided:
//
//   - Population Stability Index (PSI): a binned divergence between a
//     reference (training) distribution and a live (serving) distribution.
//     Industry-standard buckets: <0.1 stable, 0.1–0.25 moderate shift,
//     >0.25 significant drift.
//   - Kolmogorov-Smirnov (KS) statistic: the maximum gap between the two
//     empirical CDFs, sensitive to shape changes PSI can miss.
//
// Thresholds are configurable so each feature can carry its own SLO.

// DriftMethod selects the statistic used to score drift.
type DriftMethod string

const (
	// MethodPSI uses the Population Stability Index.
	MethodPSI DriftMethod = "PSI"
	// MethodKS uses the Kolmogorov-Smirnov statistic.
	MethodKS DriftMethod = "KS"
)

// DriftSeverity classifies a drift score against configured thresholds.
type DriftSeverity string

const (
	// SeverityStable means no material drift.
	SeverityStable DriftSeverity = "STABLE"
	// SeverityWarning means drift exceeds the warning threshold.
	SeverityWarning DriftSeverity = "WARNING"
	// SeverityBreach means drift exceeds the SLO breach threshold.
	SeverityBreach DriftSeverity = "BREACH"
)

// FeatureSLO configures drift thresholds for a single feature.
type FeatureSLO struct {
	// Feature is the feature/column name.
	Feature string
	// Method selects PSI or KS. Defaults to PSI when empty.
	Method DriftMethod
	// WarnThreshold and BreachThreshold are compared against the drift score.
	// For PSI, common defaults are 0.1 (warn) and 0.25 (breach); for KS the
	// score is a [0,1] CDF distance.
	WarnThreshold   float64
	BreachThreshold float64
	// Bins is the number of PSI buckets; ignored for KS. Defaults to 10.
	Bins int
}

func (s FeatureSLO) method() DriftMethod {
	if s.Method == "" {
		return MethodPSI
	}
	return s.Method
}

func (s FeatureSLO) bins() int {
	if s.Bins <= 0 {
		return 10
	}
	return s.Bins
}

// DriftResult is the outcome of scoring a live sample for one feature.
type DriftResult struct {
	Feature      string        `json:"feature"`
	Method       DriftMethod   `json:"method"`
	Score        float64       `json:"score"`
	Severity     DriftSeverity `json:"severity"`
	WarnAt       float64       `json:"warn_at"`
	BreachAt     float64       `json:"breach_at"`
	RefCount     int           `json:"ref_count"`
	LiveCount    int           `json:"live_count"`
	EvaluatedAt  time.Time     `json:"evaluated_at"`
}

// featureBaseline caches the reference distribution and precomputed PSI bin
// edges so repeated scoring avoids re-sorting the reference sample.
type featureBaseline struct {
	slo      FeatureSLO
	ref      []float64 // sorted reference values (for KS)
	binEdges []float64 // PSI upper edges, len = bins-1 (quantile cut points)
	refRates []float64 // PSI reference proportions per bin, len = bins
}

// Monitor scores live feature samples against registered baselines.
type Monitor struct {
	mu        sync.RWMutex
	baselines map[string]*featureBaseline
	now       func() time.Time
}

// NewMonitor returns an empty monitor.
func NewMonitor() *Monitor {
	return &Monitor{
		baselines: make(map[string]*featureBaseline),
		now:       time.Now,
	}
}

// psiEpsilon guards against log(0)/division-by-zero in empty PSI buckets.
const psiEpsilon = 1e-6

// RegisterBaseline records the reference distribution for a feature and
// precomputes the structures needed to score live samples cheaply.
func (m *Monitor) RegisterBaseline(slo FeatureSLO, reference []float64) error {
	if slo.Feature == "" {
		return fmt.Errorf("mlops: baseline requires a feature name")
	}
	if len(reference) == 0 {
		return fmt.Errorf("mlops: baseline for %q needs a non-empty reference sample", slo.Feature)
	}

	ref := make([]float64, len(reference))
	copy(ref, reference)
	sort.Float64s(ref)

	bl := &featureBaseline{slo: slo, ref: ref}

	if slo.method() == MethodPSI {
		bins := slo.bins()
		edges := quantileEdges(ref, bins)
		bl.binEdges = edges
		counts := bucketize(ref, edges)
		rates := make([]float64, len(counts))
		total := float64(len(ref))
		for i, c := range counts {
			rates[i] = float64(c) / total
		}
		bl.refRates = rates
	}

	m.mu.Lock()
	m.baselines[slo.Feature] = bl
	m.mu.Unlock()
	return nil
}

// Score evaluates a live sample against the baseline for the given feature.
func (m *Monitor) Score(feature string, live []float64) (DriftResult, error) {
	m.mu.RLock()
	bl, ok := m.baselines[feature]
	m.mu.RUnlock()
	if !ok {
		return DriftResult{}, fmt.Errorf("mlops: no baseline registered for feature %q", feature)
	}
	if len(live) == 0 {
		return DriftResult{}, fmt.Errorf("mlops: live sample for %q is empty", feature)
	}

	var score float64
	switch bl.slo.method() {
	case MethodKS:
		score = ksStatistic(bl.ref, live)
	default:
		score = psiScore(bl, live)
	}

	res := DriftResult{
		Feature:     feature,
		Method:      bl.slo.method(),
		Score:       score,
		WarnAt:      bl.slo.WarnThreshold,
		BreachAt:    bl.slo.BreachThreshold,
		RefCount:    len(bl.ref),
		LiveCount:   len(live),
		EvaluatedAt: m.now(),
		Severity:    classify(score, bl.slo.WarnThreshold, bl.slo.BreachThreshold),
	}
	return res, nil
}

func classify(score, warn, breach float64) DriftSeverity {
	switch {
	case breach > 0 && score >= breach:
		return SeverityBreach
	case warn > 0 && score >= warn:
		return SeverityWarning
	default:
		return SeverityStable
	}
}

// ============================================================================
// Statistics
// ============================================================================

// quantileEdges returns bins-1 interior cut points at equal quantiles of the
// (already sorted) reference sample. Equal-frequency binning keeps PSI stable
// for skewed distributions.
func quantileEdges(sorted []float64, bins int) []float64 {
	if bins < 2 {
		return nil
	}
	edges := make([]float64, 0, bins-1)
	n := len(sorted)
	for i := 1; i < bins; i++ {
		pos := float64(i) / float64(bins) * float64(n-1)
		lo := int(math.Floor(pos))
		hi := int(math.Ceil(pos))
		if hi >= n {
			hi = n - 1
		}
		frac := pos - float64(lo)
		edge := sorted[lo]*(1-frac) + sorted[hi]*frac
		edges = append(edges, edge)
	}
	return edges
}

// bucketize counts how many values fall into each bin defined by edges.
// A value v goes to bin i if edges[i-1] <= v < edges[i]; the final bin is
// closed on the right. Returns len(edges)+1 counts.
func bucketize(values, edges []float64) []int {
	counts := make([]int, len(edges)+1)
	for _, v := range values {
		idx := sort.SearchFloat64s(edges, v)
		// SearchFloat64s returns the insertion point; values equal to an edge
		// land in the upper bin, which is the conventional PSI treatment.
		if idx > len(edges) {
			idx = len(edges)
		}
		counts[idx]++
	}
	return counts
}

// psiScore computes PSI between the cached reference rates and the live sample.
func psiScore(bl *featureBaseline, live []float64) float64 {
	liveCounts := bucketize(live, bl.binEdges)
	liveTotal := float64(len(live))
	var psi float64
	for i, refRate := range bl.refRates {
		liveRate := float64(liveCounts[i]) / liveTotal
		r := refRate
		l := liveRate
		if r < psiEpsilon {
			r = psiEpsilon
		}
		if l < psiEpsilon {
			l = psiEpsilon
		}
		psi += (l - r) * math.Log(l/r)
	}
	return psi
}

// ksStatistic computes the two-sample Kolmogorov-Smirnov statistic: the
// maximum absolute difference between the empirical CDFs of refSorted (already
// sorted) and live. It runs in O(n log n) dominated by sorting live.
func ksStatistic(refSorted, live []float64) float64 {
	liveSorted := make([]float64, len(live))
	copy(liveSorted, live)
	sort.Float64s(liveSorted)

	n1, n2 := len(refSorted), len(liveSorted)
	i, j := 0, 0
	var d, cdf1, cdf2 float64
	for i < n1 && j < n2 {
		x1, x2 := refSorted[i], liveSorted[j]
		if x1 <= x2 {
			// advance all reference points equal to x1
			v := x1
			for i < n1 && refSorted[i] == v {
				i++
			}
			cdf1 = float64(i) / float64(n1)
		}
		if x2 <= x1 {
			v := x2
			for j < n2 && liveSorted[j] == v {
				j++
			}
			cdf2 = float64(j) / float64(n2)
		}
		if gap := math.Abs(cdf1 - cdf2); gap > d {
			d = gap
		}
	}
	return d
}
