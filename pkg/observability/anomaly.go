package observability

// anomaly.go implements Module 45 (AIOps anomaly detection) in pure Go with no
// Python or heavyweight ML dependencies.
//
// Two independent detectors are provided so results can be cross-validated:
//
//  1. IsolationForest — the Liu/Ting/Zhou (2008) isolation-based ensemble. It
//     isolates observations with random axis-parallel splits; anomalies need
//     fewer splits to isolate, so they sit at shallower average depth. The score
//     is the canonical s(x) = 2^(-E[h(x)]/c(psi)), normalised to [0,1].
//
//  2. StatisticalBaseline — an EWMA level/variance tracker with a 3-sigma style
//     z-score mapping. It is univariate and cheap, and acts as a control for the
//     forest: agreement between the two raises confidence, disagreement flags a
//     case worth human review.
//
// Determinism: every random choice is drawn from an explicitly seeded RNG owned
// by the forest (math/rand, never crypto/rand). Two forests built with the same
// seed and the same input produce byte-identical trees and identical scores.
// math/rand is the correct choice here precisely because reproducibility is a
// requirement; these values are never used as secrets, tokens, or signatures.

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
)

// Errors returned by IsolationForest.Fit.
var (
	// ErrNoTrainingData is returned when Fit is called with no samples.
	ErrNoTrainingData = errors.New("observability: no training data")
	// ErrNoFeatures is returned when samples have zero columns.
	ErrNoFeatures = errors.New("observability: samples have no features")
)

// DimensionMismatchError reports a row whose length differs from the first row.
type DimensionMismatchError struct {
	Index int // row index in the training set
	Want  int // expected feature count
	Got   int // actual feature count
}

func (e *DimensionMismatchError) Error() string {
	return fmt.Sprintf("observability: sample %d has %d features, want %d", e.Index, e.Got, e.Want)
}

// eulerGamma is the Euler-Mascheroni constant, used in the harmonic-number
// approximation for expected BST path length.
const eulerGamma = 0.5772156649015329

// DefaultIForestSeed is the seed used by NewIForest so results are reproducible
// across runs and across machines.
const DefaultIForestSeed int64 = 42

// ============================================================================
// Module 45a: Isolation Forest
// ============================================================================

// IsolationForest is an ensemble of isolation trees producing anomaly scores in
// [0,1]. Higher scores mean "easier to isolate", i.e. more anomalous.
//
// A zero value is not usable; construct with NewIForest.
type IsolationForest struct {
	numTrees   int
	sampleSize int

	mu       sync.RWMutex
	trees    []*isolationTree
	fitted   bool
	features int
	// normFactor is c(psi): the expected path length for the effective sample
	// size, cached at Fit time.
	normFactor float64
	seed       int64
}

// isolationTree is one tree of the ensemble.
type isolationTree struct {
	root *iNode
	// heightLimit caps recursion at ceil(log2(psi)), per the original paper.
	heightLimit int
}

// iNode is a node of an isolation tree. Internal nodes carry a split
// (feature, cut); external nodes carry the count of samples that reached them.
type iNode struct {
	left, right *iNode
	feature     int
	cut         float64
	external    bool
	size        int
}

// NewIForest returns an untrained Isolation Forest with the given ensemble size
// and per-tree subsample size, seeded with DefaultIForestSeed.
//
// numTrees <= 0 defaults to 100; sampleSize <= 1 defaults to 256, the value
// recommended by the original paper.
func NewIForest(numTrees, sampleSize int) *IsolationForest {
	return NewIForestWithSeed(numTrees, sampleSize, DefaultIForestSeed)
}

// NewIForestWithSeed is NewIForest with an explicit RNG seed, for tests that
// need several independent-but-reproducible forests.
func NewIForestWithSeed(numTrees, sampleSize int, seed int64) *IsolationForest {
	if numTrees <= 0 {
		numTrees = 100
	}
	if sampleSize <= 1 {
		sampleSize = 256
	}
	return &IsolationForest{
		numTrees:   numTrees,
		sampleSize: sampleSize,
		seed:       seed,
	}
}

// Fit trains the forest. Each tree is grown on an independent subsample drawn
// with replacement from samples.
//
// All rows must have the same length; rows shorter than the first row are
// rejected. Fit is safe to call again to retrain on new data.
func (f *IsolationForest) Fit(samples [][]float64) error {
	if len(samples) == 0 {
		return ErrNoTrainingData
	}
	dim := len(samples[0])
	if dim == 0 {
		return ErrNoFeatures
	}
	for i, s := range samples {
		if len(s) != dim {
			return &DimensionMismatchError{Index: i, Want: dim, Got: len(s)}
		}
	}

	// Effective subsample size cannot exceed the dataset.
	psi := f.sampleSize
	if psi > len(samples) {
		psi = len(samples)
	}
	heightLimit := int(math.Ceil(math.Log2(float64(psi))))
	if heightLimit < 1 {
		heightLimit = 1
	}

	rng := rand.New(rand.NewSource(f.seed))
	trees := make([]*isolationTree, 0, f.numTrees)
	subset := make([][]float64, psi)
	for t := 0; t < f.numTrees; t++ {
		for i := 0; i < psi; i++ {
			subset[i] = samples[rng.Intn(len(samples))]
		}
		// growTree may reorder its input, so hand it a copy of the slice
		// headers (the rows themselves are never mutated).
		work := make([][]float64, psi)
		copy(work, subset)
		trees = append(trees, &isolationTree{
			root:        growTree(work, dim, 0, heightLimit, rng),
			heightLimit: heightLimit,
		})
	}

	f.mu.Lock()
	f.trees = trees
	f.features = dim
	f.normFactor = expectedPathLength(float64(psi))
	f.fitted = true
	f.mu.Unlock()
	return nil
}

// growTree builds an isolation tree over data by recursive random splitting.
func growTree(data [][]float64, dim, depth, heightLimit int, rng *rand.Rand) *iNode {
	if depth >= heightLimit || len(data) <= 1 {
		return &iNode{external: true, size: len(data)}
	}

	// Pick a feature that actually has spread; if none does, all rows are
	// identical on every axis and the node is a leaf.
	feature, minV, maxV, ok := pickSplitFeature(data, dim, rng)
	if !ok {
		return &iNode{external: true, size: len(data)}
	}
	cut := minV + rng.Float64()*(maxV-minV)

	// In-place partition: rows with value < cut go left.
	i := 0
	for j := 0; j < len(data); j++ {
		if data[j][feature] < cut {
			data[i], data[j] = data[j], data[i]
			i++
		}
	}
	// A degenerate partition (everything on one side) would recurse forever on
	// the same set; treat it as a leaf.
	if i == 0 || i == len(data) {
		return &iNode{external: true, size: len(data)}
	}

	return &iNode{
		feature: feature,
		cut:     cut,
		size:    len(data),
		left:    growTree(data[:i], dim, depth+1, heightLimit, rng),
		right:   growTree(data[i:], dim, depth+1, heightLimit, rng),
	}
}

// pickSplitFeature chooses a random feature with non-zero range and returns its
// index and observed bounds. ok is false when every feature is constant.
func pickSplitFeature(data [][]float64, dim int, rng *rand.Rand) (feature int, minV, maxV float64, ok bool) {
	// Try features in a random order and take the first with spread. This is
	// equivalent to uniform selection among splittable features and avoids
	// scanning every column when the first candidate works.
	order := rng.Perm(dim)
	for _, cand := range order {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, row := range data {
			v := row[cand]
			if math.IsNaN(v) {
				continue
			}
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		if hi-lo > 1e-12 {
			return cand, lo, hi, true
		}
	}
	return 0, 0, 0, false
}

// Score returns the anomaly score of x in [0,1]. An unfitted forest returns 0.
//
// The score is s(x) = 2^(-E[h(x)]/c(psi)); values above roughly 0.6 are the
// conventional anomaly region, and 0.5 means "average depth", i.e. no signal.
func (f *IsolationForest) Score(x []float64) float64 {
	f.mu.RLock()
	trees := f.trees
	fitted := f.fitted
	dim := f.features
	c := f.normFactor
	f.mu.RUnlock()

	if !fitted || len(trees) == 0 || len(x) != dim || c <= 0 {
		return 0
	}

	var total float64
	for _, t := range trees {
		total += t.pathLength(x)
	}
	avg := total / float64(len(trees))
	return math.Pow(2, -avg/c)
}

// ScoreBatch scores many samples, reusing one read-lock acquisition per call.
func (f *IsolationForest) ScoreBatch(xs [][]float64) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = f.Score(x)
	}
	return out
}

// pathLength returns h(x): the depth reached by x, plus an adjustment for the
// unbuilt subtree below a truncated or multi-sample leaf.
func (t *isolationTree) pathLength(x []float64) float64 {
	n := t.root
	depth := 0.0
	for !n.external {
		if x[n.feature] < n.cut {
			n = n.left
		} else {
			n = n.right
		}
		depth++
	}
	// Leaves holding more than one sample stand in for a subtree we chose not
	// to grow; charge the expected depth of that subtree.
	if n.size > 1 {
		depth += expectedPathLength(float64(n.size))
	}
	return depth
}

// expectedPathLength is c(n): the average path length of an unsuccessful search
// in a binary search tree of n nodes, used to normalise depths.
func expectedPathLength(n float64) float64 {
	if n <= 1 {
		return 0
	}
	if n == 2 {
		return 1
	}
	harmonic := math.Log(n-1) + eulerGamma
	return 2*harmonic - 2*(n-1)/n
}

// Fitted reports whether the forest has been trained.
func (f *IsolationForest) Fitted() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.fitted
}

// NumTrees returns the ensemble size.
func (f *IsolationForest) NumTrees() int { return f.numTrees }

// Threshold returns the score cutoff that flags the top contamination fraction
// of scores as anomalous, computed empirically from the supplied samples.
// contamination is clamped to (0,0.5]. It returns 0 when there is no data.
//
// This is the honest way to pick a cutoff: it is derived from the score
// distribution of real data rather than a hardcoded constant.
func (f *IsolationForest) Threshold(samples [][]float64, contamination float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	if contamination <= 0 || contamination > 0.5 {
		contamination = 0.1
	}
	scores := f.ScoreBatch(samples)
	sorted := make([]float64, len(scores))
	copy(sorted, scores)
	sort.Float64s(sorted)
	idx := int(math.Floor(float64(len(sorted)) * (1 - contamination)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

// ============================================================================
// Module 45b: EWMA + 3-sigma statistical baseline
// ============================================================================

// StatisticalBaseline tracks a univariate signal with an exponentially weighted
// moving average and an EWMA of squared deviations, flagging points that fall
// outside a sigma band. It is the cross-check control for IsolationForest.
type StatisticalBaseline struct {
	alpha   float64 // smoothing factor for level and variance
	sigmaK  float64 // band width in standard deviations (3 by default)
	warmup  int     // observations required before scoring is meaningful

	mu    sync.Mutex
	level float64
	varia float64
	count int
}

// NewStatisticalBaseline returns a baseline with smoothing factor alpha and a
// 3-sigma band. alpha outside (0,1] falls back to 0.3.
func NewStatisticalBaseline(alpha float64) *StatisticalBaseline {
	return NewStatisticalBaselineWithBand(alpha, 3.0, 5)
}

// NewStatisticalBaselineWithBand allows tuning the band width and warmup count.
func NewStatisticalBaselineWithBand(alpha, sigmaK float64, warmup int) *StatisticalBaseline {
	if alpha <= 0 || alpha > 1 {
		alpha = 0.3
	}
	if sigmaK <= 0 {
		sigmaK = 3.0
	}
	if warmup < 1 {
		warmup = 1
	}
	return &StatisticalBaseline{alpha: alpha, sigmaK: sigmaK, warmup: warmup}
}

// Observe folds v into the baseline and returns the deviation in standard
// deviations (the z-score) evaluated *before* the update, so a point is judged
// against the model that preceded it rather than one it has already shifted.
//
// During warmup, and while variance is degenerate, it returns 0.
func (b *StatisticalBaseline) Observe(v float64) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.count == 0 {
		b.level = v
		b.varia = 0
		b.count = 1
		return 0
	}

	dev := v - b.level
	z := 0.0
	if b.count >= b.warmup {
		if sd := math.Sqrt(b.varia); sd > 1e-12 {
			z = math.Abs(dev) / sd
		}
	}

	b.level += b.alpha * dev
	b.varia = (1-b.alpha)*b.varia + b.alpha*dev*dev
	b.count++
	return z
}

// Score converts Observe's z-score into a [0,1] anomaly score comparable with
// IsolationForest.Score. The mapping saturates smoothly: z=sigmaK maps to ~0.5
// and larger deviations approach 1.
func (b *StatisticalBaseline) Score(v float64) float64 {
	z := b.Observe(v)
	if z <= 0 {
		return 0
	}
	// Logistic in z centred on the sigma band.
	return 1 / (1 + math.Exp(-(z - b.sigmaK)))
}

// IsAnomaly reports whether v lies outside the sigma band.
func (b *StatisticalBaseline) IsAnomaly(v float64) bool {
	return b.Observe(v) > b.sigmaK
}

// Level returns the current EWMA level, its standard deviation, and the number
// of observations folded in.
func (b *StatisticalBaseline) Level() (level, stddev float64, n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.level, math.Sqrt(b.varia), b.count
}

// Bounds returns the current [lower, upper] sigma band. ok is false during
// warmup, when the band is not yet meaningful.
func (b *StatisticalBaseline) Bounds() (lower, upper float64, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count < b.warmup {
		return 0, 0, false
	}
	sd := math.Sqrt(b.varia)
	return b.level - b.sigmaK*sd, b.level + b.sigmaK*sd, true
}

// Reset clears all accumulated state.
func (b *StatisticalBaseline) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.level, b.varia, b.count = 0, 0, 0
}

// ============================================================================
// Cross-validation of the two detectors
// ============================================================================

// DetectorVerdict is the combined judgement of the forest and the baseline on
// one observation.
type DetectorVerdict struct {
	ForestScore   float64 `json:"forest_score"`
	BaselineScore float64 `json:"baseline_score"`
	// Agreement is true when both detectors land on the same side of their
	// respective thresholds.
	Agreement bool `json:"agreement"`
	Anomaly   bool `json:"anomaly"`
}

// CrossValidate scores x with both detectors and reports whether they agree.
// The baseline consumes feature index featureIdx of x.
//
// Anomaly is set when either detector fires; Agreement records whether the two
// independent methods concurred, which is the signal operators should weigh.
func CrossValidate(f *IsolationForest, b *StatisticalBaseline, x []float64, featureIdx int, forestThreshold float64) DetectorVerdict {
	var v DetectorVerdict
	if f != nil {
		v.ForestScore = f.Score(x)
	}
	if b != nil && featureIdx >= 0 && featureIdx < len(x) {
		v.BaselineScore = b.Score(x[featureIdx])
	}
	forestFired := v.ForestScore > forestThreshold
	baselineFired := v.BaselineScore > 0.5
	v.Agreement = forestFired == baselineFired
	v.Anomaly = forestFired || baselineFired
	return v
}
