// Package aiops - Module M45 Anomaly Detection Head-to-Head Benchmark
// T2 Protocol: REAL competitor vs. AIOps ensemble (Mahalanobis + IsolationForest)
//
// Competitors (all REAL, implemented from published algorithms — documented):
//   1. Z-score baseline     — classic univariate 3-sigma statistical detector
//   2. EWMA online detector — exponentially-weighted moving average control chart
//   3. Random Cut Forest    — Go port of AWS/Microsoft RCF streaming detector
//
// Same work unit for every contender: score ONE 8-feature data point.
// Metrics reported:
//   1) Detection latency ns/op per point (go test -bench)
//   2) Throughput points/sec (derived from ns/op)
//   3) Detection F1 on a labeled synthetic anomaly dataset (TestF1Report)
//
// Anti-fiasco rules honored:
//   - Real competitors (Z-score/EWMA/RCF), documented above.
//   - count=6 median taken at the reporting layer (benchstat / manual median).
//   - Identical work unit + identical dataset for all detectors.
//   - Honest verdict even if we lose (see TestF1Report + the delivery notes).
//   - build + vet clean before any bench run.
//
// NOTE: reuses testLogger and sampleSnapshot already declared in
// selfheal_bench_test.go (same package) — do NOT redeclare them here.

package aiops

import (
	"io"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	// Shared synthetic workload + ground-truth labels, built once in init().
	m45Data   []MetricsSnapshot
	m45Labels []bool

	// Detectors under test.
	m45Engine      *SelfHealEngine
	zscoreBaseline *ZScoreBaseline
	ewmaBaseline   *EWMAOnlineDetector
	rcfBaseline    *RandomCutForest
)

const (
	m45TotalPoints = 5000
	m45TrainPoints = 500 // first 500 = "normal" regime used for training
	m45Seed        = int64(1724538900)
)

func init() {
	rng := rand.New(rand.NewSource(m45Seed))

	m45Data, m45Labels = generateSyntheticWorkload(m45TotalPoints, rng)

	train := m45Data[:m45TrainPoints]

	// M45 ensemble.
	engineLogger := logrus.New()
	engineLogger.SetLevel(logrus.PanicLevel)
	engineLogger.SetOutput(io.Discard)
	m45Engine = NewSelfHealEngine(engineLogger)
	_ = m45Engine.anomalyDetector.mahalanobisModel.Train(train)
	_ = m45Engine.anomalyDetector.isolationForest.Train(train)

	// Baselines.
	zscoreBaseline = NewZScoreBaseline(8, 3.0) // 3-sigma classic rule
	_ = zscoreBaseline.Train(train)

	ewmaBaseline = NewEWMAOnlineDetector(8, 0.1, 3.0)
	_ = ewmaBaseline.Train(train)

	rcfBaseline = NewRandomCutForest(100, 256, 8, 12)
	_ = rcfBaseline.Train(train)
}

// ============================================================================
// SYNTHETIC WORKLOAD (realistic multivariate metrics with injected anomalies)
// ============================================================================

// generateSyntheticWorkload builds n snapshots plus a parallel ground-truth
// label slice. Labels are set ONLY where an anomaly is actually injected —
// this keeps the F1 evaluation honest and detector-agnostic.
func generateSyntheticWorkload(n int, rng *rand.Rand) ([]MetricsSnapshot, []bool) {
	data := make([]MetricsSnapshot, n)
	labels := make([]bool, n)
	base := time.Now().Add(-24 * time.Hour)

	const (
		cpuBase       = 0.35
		memBase       = 0.50
		diskReadBase  = 500.0
		diskWriteBase = 300.0
		netInBase     = 1e6
		netOutBase    = 5e5
		connBase      = 80.0
		errBase       = 0.005
		latencyBase   = 50.0
	)

	for i := range data {
		t := base.Add(time.Duration(i) * time.Second)

		// Daily seasonality: gentle sinusoidal load boost.
		phase := math.Sin(float64(i) / 3600.0 * 2 * math.Pi)
		peak := 1.0 + 0.3*math.Max(0, phase)

		cpu := clip(cpuBase*peak+rng.NormFloat64()*0.05, 0, 1)
		mem := clip(memBase*peak+rng.NormFloat64()*0.04, 0, 1)
		netIn := netInBase + rng.NormFloat64()*5e4
		diskRead := diskReadBase + math.Abs(rng.NormFloat64()*80)
		errRate := math.Max(errBase+rng.NormFloat64()*0.005, 0)

		isAnomaly := false

		// Pattern 1: CPU spike (runaway process).
		if i > m45TrainPoints && rng.Float64() < 0.02 {
			cpu = clip(0.95+rng.Float64()*0.04, 0, 1)
			isAnomaly = true
		}
		// Pattern 2: memory crash (level shift down).
		if i >= 1500 && i < 1550 {
			mem = clip(0.10+rng.Float64()*0.05, 0, 1)
			isAnomaly = true
		}
		// Pattern 3: network DDoS burst.
		if i > 2000 && rng.Float64() < 0.01 {
			netIn = netInBase * (5 + rng.Float64()*10)
			isAnomaly = true
		}
		// Pattern 4: disk I/O saturation.
		if i > 3000 && rng.Float64() < 0.015 {
			diskRead = 5000 + rng.Float64()*3000
			isAnomaly = true
		}
		// Pattern 5: error-rate storm.
		if i > 3500 && rng.Float64() < 0.02 {
			errRate = 0.15 + rng.Float64()*0.1
			isAnomaly = true
		}

		latency := latencyBase*(1+cpu*2) + math.Abs(rng.NormFloat64()*10)

		data[i] = MetricsSnapshot{
			Timestamp:      t,
			CPUUtilization: cpu,
			MemoryUsage:    mem,
			DiskIORead:     diskRead,
			DiskIOWrite:    diskWriteBase + math.Abs(rng.NormFloat64()*40),
			NetworkIn:      math.Max(netIn, 0),
			NetworkOut:     netOutBase + rng.NormFloat64()*3e4,
			Connections:    maxInt(int(connBase*peak+rng.NormFloat64()*10), 0),
			ErrorRate:      errRate,
			LatencyP99:     math.Max(latency, 0),
		}
		labels[i] = isAnomaly
	}

	return data, labels
}

func clip(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ============================================================================
// BASELINE 1: Z-Score (univariate 3-sigma statistical detector)
// Reference: classic Shewhart / Grubbs outlier test, per-feature max |z|.
// ============================================================================

type ZScoreBaseline struct {
	means     []float64
	stds      []float64
	nFeatures int
	threshold float64
	trained   bool
}

func NewZScoreBaseline(nFeatures int, threshold float64) *ZScoreBaseline {
	return &ZScoreBaseline{
		means:     make([]float64, nFeatures),
		stds:      make([]float64, nFeatures),
		nFeatures: nFeatures,
		threshold: threshold,
	}
}

func (z *ZScoreBaseline) Train(data []MetricsSnapshot) error {
	if len(data) < 10 {
		return nil
	}
	n := float64(len(data))
	for i := 0; i < z.nFeatures; i++ {
		sum := 0.0
		for _, s := range data {
			sum += getFeatureValue(s, i)
		}
		z.means[i] = sum / n

		variance := 0.0
		for _, s := range data {
			d := getFeatureValue(s, i) - z.means[i]
			variance += d * d
		}
		z.stds[i] = math.Sqrt(variance / n)
		if z.stds[i] < 1e-10 {
			z.stds[i] = 1.0
		}
	}
	z.trained = true
	return nil
}

// Score returns the maximum absolute z-score across features.
func (z *ZScoreBaseline) Score(x []float64) float64 {
	if !z.trained {
		return 0.0
	}
	maxZ := 0.0
	for i := 0; i < z.nFeatures && i < len(x); i++ {
		zs := math.Abs((x[i] - z.means[i]) / z.stds[i])
		if zs > maxZ {
			maxZ = zs
		}
	}
	return maxZ
}

func (z *ZScoreBaseline) IsAnomaly(x []float64) bool {
	return z.Score(x) > z.threshold
}

// ============================================================================
// BASELINE 2: EWMA online detector (exponentially-weighted control chart)
// Reference: Roberts (1959) EWMA; online mean+variance tracking, per-feature.
// ============================================================================

type EWMAOnlineDetector struct {
	ewmas     []float64
	ewmaVars  []float64
	alpha     float64
	nFeatures int
	threshold float64
	trained   bool
}

func NewEWMAOnlineDetector(nFeatures int, alpha, threshold float64) *EWMAOnlineDetector {
	return &EWMAOnlineDetector{
		ewmas:     make([]float64, nFeatures),
		ewmaVars:  make([]float64, nFeatures),
		alpha:     alpha,
		nFeatures: nFeatures,
		threshold: threshold,
	}
}

func (e *EWMAOnlineDetector) Train(data []MetricsSnapshot) error {
	if len(data) == 0 {
		return nil
	}
	init := minInt(50, len(data))
	for i := 0; i < e.nFeatures; i++ {
		sum := 0.0
		for j := 0; j < init; j++ {
			sum += getFeatureValue(data[j], i)
		}
		e.ewmas[i] = sum / float64(init)

		variance := 0.0
		for j := 0; j < init; j++ {
			d := getFeatureValue(data[j], i) - e.ewmas[i]
			variance += d * d
		}
		e.ewmaVars[i] = variance / float64(init)
		if e.ewmaVars[i] < 1e-10 {
			e.ewmaVars[i] = 1.0
		}
	}
	e.trained = true
	return nil
}

// Score computes the max standardized deviation, then updates EWMA state
// (online learning: score-then-adapt).
func (e *EWMAOnlineDetector) Score(x []float64) float64 {
	if !e.trained {
		return 0.0
	}
	maxStat := 0.0
	for i := 0; i < e.nFeatures && i < len(x); i++ {
		stat := math.Abs(x[i]-e.ewmas[i]) / math.Sqrt(e.ewmaVars[i])
		if stat > maxStat {
			maxStat = stat
		}
	}
	// Adapt after scoring.
	for i := 0; i < e.nFeatures && i < len(x); i++ {
		prev := e.ewmas[i]
		e.ewmas[i] = e.alpha*x[i] + (1-e.alpha)*prev
		d := x[i] - e.ewmas[i]
		e.ewmaVars[i] = e.alpha*d*d + (1-e.alpha)*e.ewmaVars[i]
		if e.ewmaVars[i] < 1e-10 {
			e.ewmaVars[i] = 1e-10
		}
	}
	return maxStat
}

func (e *EWMAOnlineDetector) IsAnomaly(x []float64) bool {
	return e.Score(x) > e.threshold
}

// ============================================================================
// BASELINE 3: Random Cut Forest (Go port for streaming anomaly detection)
// Reference: Guha et al. "Robust Random Cut Forest Based Anomaly Detection
// On Streams" (ICML 2016). Simplified proxy: dimension-proportional random
// cuts + expected-path-length normalization (isolation-style scoring).
// Documented as a PROXY: this is a faithful RCF-family scorer, not the full
// AWS displacement/CoDisp implementation.
// ============================================================================

type RandomCutForest struct {
	trees      []*rcfTree
	sampleSize int
	maxDepth   int
	nFeatures  int
	threshold  float64
	trained    bool
}

type rcfTree struct {
	root *rcfNode
}

type rcfNode struct {
	left       *rcfNode
	right      *rcfNode
	cutDim     int
	cutValue   float64
	size       int
	isExternal bool
}

func NewRandomCutForest(numTrees, sampleSize, nFeatures, maxDepth int) *RandomCutForest {
	return &RandomCutForest{
		trees:      make([]*rcfTree, numTrees),
		sampleSize: sampleSize,
		maxDepth:   maxDepth,
		nFeatures:  nFeatures,
		threshold:  0.55,
	}
}

func (r *RandomCutForest) Train(data []MetricsSnapshot) error {
	if len(data) == 0 {
		return nil
	}
	// Deterministic RNG per Train call for reproducibility.
	trng := rand.New(rand.NewSource(m45Seed + 7))
	for i := range r.trees {
		sample := r.subsample(data, trng)
		r.trees[i] = &rcfTree{root: r.buildTree(sample, 0, trng)}
	}
	r.trained = true
	return nil
}

func (r *RandomCutForest) subsample(data []MetricsSnapshot, trng *rand.Rand) []MetricsSnapshot {
	n := minInt(r.sampleSize, len(data))
	out := make([]MetricsSnapshot, n)
	for i := 0; i < n; i++ {
		out[i] = data[trng.Intn(len(data))]
	}
	return out
}

// buildTree performs random-cut partitioning. Cut dimension is chosen with
// probability proportional to its range (the RCF distinguishing property vs
// vanilla isolation forest which picks dimensions uniformly).
func (r *RandomCutForest) buildTree(data []MetricsSnapshot, depth int, trng *rand.Rand) *rcfNode {
	node := &rcfNode{size: len(data)}
	if depth >= r.maxDepth || len(data) <= 1 {
		node.isExternal = true
		return node
	}

	mins := make([]float64, r.nFeatures)
	maxs := make([]float64, r.nFeatures)
	for j := 0; j < r.nFeatures; j++ {
		mins[j] = math.Inf(1)
		maxs[j] = math.Inf(-1)
	}
	for _, s := range data {
		for j := 0; j < r.nFeatures; j++ {
			v := getFeatureValue(s, j)
			if v < mins[j] {
				mins[j] = v
			}
			if v > maxs[j] {
				maxs[j] = v
			}
		}
	}

	// Range-proportional dimension selection.
	totalRange := 0.0
	ranges := make([]float64, r.nFeatures)
	for j := 0; j < r.nFeatures; j++ {
		ranges[j] = maxs[j] - mins[j]
		if ranges[j] < 0 {
			ranges[j] = 0
		}
		totalRange += ranges[j]
	}
	if totalRange < 1e-12 {
		node.isExternal = true
		return node
	}

	target := trng.Float64() * totalRange
	cutDim := 0
	acc := 0.0
	for j := 0; j < r.nFeatures; j++ {
		acc += ranges[j]
		if target <= acc {
			cutDim = j
			break
		}
	}

	cutValue := mins[cutDim] + trng.Float64()*ranges[cutDim]
	node.cutDim = cutDim
	node.cutValue = cutValue

	var left, right []MetricsSnapshot
	for _, s := range data {
		if getFeatureValue(s, cutDim) < cutValue {
			left = append(left, s)
		} else {
			right = append(right, s)
		}
	}
	node.left = r.buildTree(left, depth+1, trng)
	node.right = r.buildTree(right, depth+1, trng)
	return node
}

// Score returns a normalized isolation-style anomaly score in (0,1].
func (r *RandomCutForest) Score(x []float64) float64 {
	if !r.trained || len(r.trees) == 0 {
		return 0.0
	}
	total := 0.0
	for _, tree := range r.trees {
		total += rcfPathLength(tree.root, x, 0)
	}
	avg := total / float64(len(r.trees))
	c := rcfExpectedC(r.sampleSize)
	if c == 0 {
		return 0.0
	}
	return math.Pow(2, -avg/c)
}

func rcfPathLength(node *rcfNode, x []float64, depth int) float64 {
	if node == nil {
		return float64(depth)
	}
	if node.isExternal {
		return float64(depth) + rcfExpectedC(node.size)
	}
	if node.cutDim < len(x) && x[node.cutDim] < node.cutValue {
		return rcfPathLength(node.left, x, depth+1)
	}
	return rcfPathLength(node.right, x, depth+1)
}

func rcfExpectedC(n int) float64 {
	if n <= 1 {
		return 0
	}
	return 2.0*(math.Log2(float64(n-1))+0.5772156649) - (2.0 * float64(n-1) / float64(n))
}

func (r *RandomCutForest) IsAnomaly(x []float64) bool {
	return r.Score(x) > r.threshold
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// LATENCY / THROUGHPUT BENCHMARKS — identical work unit: score 1 data point
// Run with: go test -run '^$' -bench 'M45|ZScore|EWMA|RCF' -benchtime=2s -count=6 -json
// ============================================================================

// BenchmarkM45_ScorePoint measures the M45 ensemble scoring latency per point.
func BenchmarkM45_ScorePoint(b *testing.B) {
	x := extractFeatures(sampleSnapshot)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mScore := m45Engine.anomalyDetector.mahalanobisModel.IsScore(x)
		fScore := m45Engine.anomalyDetector.isolationForest.AnomallyScore(x)
		_ = mScore*0.4 + fScore*0.6
	}
}

// BenchmarkZScore_ScorePoint measures the Z-score baseline scoring latency.
func BenchmarkZScore_ScorePoint(b *testing.B) {
	x := extractFeatures(sampleSnapshot)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = zscoreBaseline.Score(x)
	}
}

// BenchmarkEWMA_ScorePoint measures the EWMA baseline scoring latency.
func BenchmarkEWMA_ScorePoint(b *testing.B) {
	x := extractFeatures(sampleSnapshot)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ewmaBaseline.Score(x)
	}
}

// BenchmarkRCF_ScorePoint measures the Random Cut Forest scoring latency.
func BenchmarkRCF_ScorePoint(b *testing.B) {
	x := extractFeatures(sampleSnapshot)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rcfBaseline.Score(x)
	}
}

// ============================================================================
// DETECTION QUALITY — F1 on the labeled dataset (fair, same data + threshold)
// Reported as a normal test so numbers print with `go test -run TestF1Report -v`.
// ============================================================================

type f1Result struct {
	name              string
	tp, fp, tn, fn    int
	precision, recall float64
	f1                float64
}

func evalDetector(name string, predict func(x []float64) bool) f1Result {
	r := f1Result{name: name}
	x := make([]float64, 8)
	for i := m45TrainPoints; i < len(m45Data); i++ {
		copy(x, extractFeatures(m45Data[i]))
		pred := predict(x)
		actual := m45Labels[i]
		switch {
		case pred && actual:
			r.tp++
		case pred && !actual:
			r.fp++
		case !pred && actual:
			r.fn++
		default:
			r.tn++
		}
	}
	if r.tp+r.fp > 0 {
		r.precision = float64(r.tp) / float64(r.tp+r.fp)
	}
	if r.tp+r.fn > 0 {
		r.recall = float64(r.tp) / float64(r.tp+r.fn)
	}
	if r.precision+r.recall > 0 {
		r.f1 = 2 * r.precision * r.recall / (r.precision + r.recall)
	}
	return r
}

// TestF1Report prints precision/recall/F1 for every detector on the identical
// labeled dataset. This is the detection-quality half of the head-to-head.
func TestF1Report(t *testing.T) {
	// Fresh EWMA so its online state is not polluted by earlier bench runs.
	ewma := NewEWMAOnlineDetector(8, 0.1, 3.0)
	_ = ewma.Train(m45Data[:m45TrainPoints])

	results := []f1Result{
		evalDetector("M45-Ensemble", func(x []float64) bool {
			return m45Engine.anomalyDetector.mahalanobisModel.IsScore(x) > 2.7055 ||
				m45Engine.anomalyDetector.isolationForest.AnomallyScore(x) > 3.5
		}),
		evalDetector("ZScore-3sigma", func(x []float64) bool { return zscoreBaseline.IsAnomaly(x) }),
		evalDetector("EWMA-online", func(x []float64) bool { return ewma.IsAnomaly(x) }),
		evalDetector("RandomCutForest", func(x []float64) bool { return rcfBaseline.IsAnomaly(x) }),
	}

	total := len(m45Data) - m45TrainPoints
	positives := 0
	for i := m45TrainPoints; i < len(m45Data); i++ {
		if m45Labels[i] {
			positives++
		}
	}

	t.Logf("=== M45 Head-to-Head F1 Report ===")
	t.Logf("Eval points: %d | true anomalies: %d (%.2f%%)", total, positives, 100*float64(positives)/float64(total))
	t.Logf("%-18s %6s %6s %6s %6s %10s %8s %8s", "Detector", "TP", "FP", "FN", "TN", "Precision", "Recall", "F1")
	for _, r := range results {
		t.Logf("%-18s %6d %6d %6d %6d %10.4f %8.4f %8.4f",
			r.name, r.tp, r.fp, r.fn, r.tn, r.precision, r.recall, r.f1)
	}
}
