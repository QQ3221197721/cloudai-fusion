package observability

// anomaly_test.go verifies Module 45. All randomness uses explicitly seeded
// math/rand sources so every assertion below is deterministic and reproducible
// in CI — no flaky thresholds.

import (
	"errors"
	"math"
	"math/rand"
	"testing"
)

// makeCluster returns n points drawn from an isotropic Gaussian centred at
// (cx,cy) with the given spread, using a caller-supplied seeded RNG.
func makeCluster(rng *rand.Rand, n int, cx, cy, spread float64) [][]float64 {
	out := make([][]float64, n)
	for i := range out {
		out[i] = []float64{cx + rng.NormFloat64()*spread, cy + rng.NormFloat64()*spread}
	}
	return out
}

// TestIForestFitValidation covers the input-validation contract of Fit.
func TestIForestFitValidation(t *testing.T) {
	f := NewIForest(10, 16)

	if err := f.Fit(nil); !errors.Is(err, ErrNoTrainingData) {
		t.Errorf("Fit(nil) error = %v, want ErrNoTrainingData", err)
	}
	if err := f.Fit([][]float64{{}}); !errors.Is(err, ErrNoFeatures) {
		t.Errorf("Fit(empty row) error = %v, want ErrNoFeatures", err)
	}

	ragged := [][]float64{{1, 2}, {3}}
	var dimErr *DimensionMismatchError
	if err := f.Fit(ragged); !errors.As(err, &dimErr) {
		t.Errorf("Fit(ragged) error = %v, want *DimensionMismatchError", err)
	} else if dimErr.Index != 1 || dimErr.Want != 2 || dimErr.Got != 1 {
		t.Errorf("DimensionMismatchError = %+v, want {Index:1 Want:2 Got:1}", *dimErr)
	}

	if f.Fitted() {
		t.Error("Fitted() = true after only failed Fit calls, want false")
	}
	if f.Score([]float64{1, 2}) != 0 {
		t.Error("Score on unfitted forest should be 0")
	}
}

// TestIForestOutliersScoreHigher is the core Module 45 requirement: injected
// known outliers must score significantly above points from the training
// distribution.
func TestIForestOutliersScoreHigher(t *testing.T) {
	rng := rand.New(rand.NewSource(98765))
	normal := makeCluster(rng, 300, 0, 0, 1)

	f := NewIForest(100, 128)
	if err := f.Fit(normal); err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if !f.Fitted() {
		t.Fatal("Fitted() = false after successful Fit")
	}

	// Outliers placed far outside the ~3-sigma envelope of the training cluster.
	outliers := [][]float64{
		{10, 10}, {-15, 12}, {8, -12}, {-20, -8}, {25, 0},
	}

	outScores := f.ScoreBatch(outliers)
	inScores := f.ScoreBatch(normal[:50])

	meanOut, meanIn := meanOf(outScores), meanOf(inScores)
	if gap := meanOut - meanIn; gap < 0.10 {
		t.Errorf("mean outlier score %.4f vs mean inlier %.4f: gap %.4f < 0.10",
			meanOut, meanIn, gap)
	}

	// Strict separation: the weakest outlier must still beat the strongest
	// inlier in this sample.
	if minOut, maxIn := minOf(outScores), maxOf(inScores); minOut <= maxIn {
		t.Errorf("separation failed: weakest outlier %.4f <= strongest inlier %.4f",
			minOut, maxIn)
	}

	t.Logf("mean outlier=%.4f mean inlier=%.4f gap=%.4f", meanOut, meanIn, meanOut-meanIn)
}

// TestIForestDeterministic asserts that a fixed seed yields identical trees, so
// scores are byte-identical across independently constructed forests.
func TestIForestDeterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(12345))
	data := makeCluster(rng, 150, 2, 3, 1.5)

	a := NewIForestWithSeed(40, 64, DefaultIForestSeed)
	b := NewIForestWithSeed(40, 64, DefaultIForestSeed)
	if err := a.Fit(data); err != nil {
		t.Fatalf("Fit a: %v", err)
	}
	if err := b.Fit(data); err != nil {
		t.Fatalf("Fit b: %v", err)
	}

	for i, x := range data[:25] {
		if sa, sb := a.Score(x), b.Score(x); sa != sb {
			t.Fatalf("sample %d: same-seed scores diverge: %.17g vs %.17g", i, sa, sb)
		}
	}

	// A different seed should produce a different forest (sanity check that the
	// seed is actually threaded through tree construction).
	c := NewIForestWithSeed(40, 64, DefaultIForestSeed+1)
	if err := c.Fit(data); err != nil {
		t.Fatalf("Fit c: %v", err)
	}
	differs := false
	for _, x := range data[:25] {
		if a.Score(x) != c.Score(x) {
			differs = true
			break
		}
	}
	if !differs {
		t.Error("different seeds produced identical scores; seed is not being used")
	}
}

// TestIForestScoreRange checks the score stays inside [0,1] for inliers,
// outliers, and degenerate constant input.
func TestIForestScoreRange(t *testing.T) {
	rng := rand.New(rand.NewSource(555))
	data := makeCluster(rng, 200, 0, 0, 1)
	data = append(data, []float64{50, 50})

	f := NewIForest(60, 64)
	if err := f.Fit(data); err != nil {
		t.Fatalf("Fit: %v", err)
	}
	for i, s := range f.ScoreBatch(data) {
		if s < 0 || s > 1 || math.IsNaN(s) {
			t.Fatalf("score[%d] = %v, outside [0,1]", i, s)
		}
	}

	// Wrong dimensionality must be rejected rather than panicking.
	if got := f.Score([]float64{1, 2, 3}); got != 0 {
		t.Errorf("Score with wrong dim = %v, want 0", got)
	}

	// A constant dataset has no splittable feature; scoring must not panic.
	constant := [][]float64{{1, 1}, {1, 1}, {1, 1}, {1, 1}}
	cf := NewIForest(10, 4)
	if err := cf.Fit(constant); err != nil {
		t.Fatalf("Fit constant: %v", err)
	}
	if s := cf.Score([]float64{1, 1}); s < 0 || s > 1 || math.IsNaN(s) {
		t.Errorf("constant-data score = %v, want a finite value in [0,1]", s)
	}
}

// TestIForestThreshold verifies the empirical contamination threshold splits the
// score distribution at roughly the requested quantile.
func TestIForestThreshold(t *testing.T) {
	rng := rand.New(rand.NewSource(777))
	data := makeCluster(rng, 200, 0, 0, 1)

	f := NewIForest(60, 64)
	if err := f.Fit(data); err != nil {
		t.Fatalf("Fit: %v", err)
	}

	const contamination = 0.10
	th := f.Threshold(data, contamination)
	if th <= 0 || th >= 1 {
		t.Fatalf("threshold = %v, want value inside (0,1)", th)
	}

	above := 0
	for _, s := range f.ScoreBatch(data) {
		if s > th {
			above++
		}
	}
	frac := float64(above) / float64(len(data))
	// Ties in the score distribution keep this approximate; allow a wide band
	// rather than asserting an exact quantile.
	if frac > contamination*2.5 {
		t.Errorf("%.1f%% of points exceed the %.0f%% threshold; too many",
			frac*100, contamination*100)
	}
	t.Logf("threshold=%.4f flags %.1f%% of training data", th, frac*100)
}

// TestStatisticalBaselineDetectsShift checks the EWMA/3-sigma control detector
// stays quiet on stationary input and fires on a large step.
func TestStatisticalBaselineDetectsShift(t *testing.T) {
	rng := rand.New(rand.NewSource(2024))
	b := NewStatisticalBaseline(0.3)

	// Stationary phase: N(100, 1).
	falsePositives := 0
	for i := 0; i < 200; i++ {
		if b.IsAnomaly(100 + rng.NormFloat64()) {
			falsePositives++
		}
	}
	if rate := float64(falsePositives) / 200; rate > 0.05 {
		t.Errorf("false-positive rate on stationary input = %.1f%%, want <= 5%%", rate*100)
	}

	level, sd, n := b.Level()
	if math.Abs(level-100) > 2 {
		t.Errorf("EWMA level = %.3f, want approximately 100", level)
	}
	if sd <= 0 {
		t.Errorf("stddev = %v, want > 0", sd)
	}
	if n != 200 {
		t.Errorf("observation count = %d, want 200", n)
	}

	lo, hi, ok := b.Bounds()
	if !ok {
		t.Fatal("Bounds not available after warmup")
	}
	if !(lo < level && level < hi) {
		t.Errorf("band [%.3f,%.3f] does not bracket level %.3f", lo, hi, level)
	}

	// A large step change must be flagged.
	if z := b.Observe(160); z <= 3 {
		t.Errorf("z-score for a +60 sigma-scale step = %.3f, want > 3", z)
	}
}

// TestStatisticalBaselineWarmupAndReset covers the warmup gate and Reset.
func TestStatisticalBaselineWarmupAndReset(t *testing.T) {
	b := NewStatisticalBaselineWithBand(0.3, 3.0, 5)

	if _, _, ok := b.Bounds(); ok {
		t.Error("Bounds available before any observation, want not-ready")
	}
	// During warmup nothing should be flagged, however extreme.
	b.Observe(10)
	for i := 0; i < 3; i++ {
		if b.IsAnomaly(1000) {
			t.Fatalf("observation %d flagged during warmup", i)
		}
	}

	b.Reset()
	if _, _, n := b.Level(); n != 0 {
		t.Errorf("count after Reset = %d, want 0", n)
	}
	if _, _, ok := b.Bounds(); ok {
		t.Error("Bounds available immediately after Reset")
	}
}

// TestStatisticalBaselineScoreMonotonic checks the z-to-[0,1] mapping is bounded
// and that a bigger deviation scores higher.
func TestStatisticalBaselineScoreMonotonic(t *testing.T) {
	build := func() *StatisticalBaseline {
		rng := rand.New(rand.NewSource(31337))
		b := NewStatisticalBaseline(0.3)
		for i := 0; i < 100; i++ {
			b.Observe(50 + rng.NormFloat64())
		}
		return b
	}

	small := build().Score(52)
	large := build().Score(120)

	for name, s := range map[string]float64{"small": small, "large": large} {
		if s < 0 || s > 1 || math.IsNaN(s) {
			t.Fatalf("%s deviation score = %v, outside [0,1]", name, s)
		}
	}
	if large <= small {
		t.Errorf("score not monotonic in deviation: small=%.4f large=%.4f", small, large)
	}
}

// TestCrossValidateAgreement exercises the two-detector cross-check on a clear
// inlier and a clear outlier.
func TestCrossValidateAgreement(t *testing.T) {
	rng := rand.New(rand.NewSource(4242))
	train := makeCluster(rng, 250, 0, 0, 1)

	f := NewIForest(100, 128)
	if err := f.Fit(train); err != nil {
		t.Fatalf("Fit: %v", err)
	}

	baseline := NewStatisticalBaseline(0.3)
	for _, x := range train {
		baseline.Observe(x[0])
	}

	th := f.Threshold(train, 0.05)

	inlier := CrossValidate(f, baseline, []float64{0.1, -0.1}, 0, th)
	if inlier.Anomaly {
		t.Errorf("inlier flagged: forest=%.4f baseline=%.4f", inlier.ForestScore, inlier.BaselineScore)
	}
	if !inlier.Agreement {
		t.Error("detectors disagree on a clear inlier")
	}

	outlier := CrossValidate(f, baseline, []float64{40, -35}, 0, th)
	if !outlier.Anomaly {
		t.Errorf("outlier not flagged: forest=%.4f baseline=%.4f", outlier.ForestScore, outlier.BaselineScore)
	}
	if !outlier.Agreement {
		t.Logf("detectors disagree on outlier (forest=%.4f baseline=%.4f); "+
			"recorded, not fatal", outlier.ForestScore, outlier.BaselineScore)
	}

	// Nil detectors must be tolerated.
	if v := CrossValidate(nil, nil, []float64{1, 2}, 0, 0.5); v.Anomaly {
		t.Error("CrossValidate with nil detectors reported an anomaly")
	}
}

// TestExpectedPathLength pins the c(n) normalisation constant at its known
// boundary values.
func TestExpectedPathLength(t *testing.T) {
	cases := []struct {
		n    float64
		want float64
	}{
		{0, 0},
		{1, 0},
		{2, 1},
	}
	for _, c := range cases {
		if got := expectedPathLength(c.n); got != c.want {
			t.Errorf("expectedPathLength(%v) = %v, want %v", c.n, got, c.want)
		}
	}
	// c(n) must grow with n.
	if expectedPathLength(256) <= expectedPathLength(64) {
		t.Error("expectedPathLength is not increasing in n")
	}
}

// ---------------------------------------------------------------------------
// Benchmarks — numbers reported in docs/verification-modules-45-49.md
// ---------------------------------------------------------------------------

func benchData(seed int64, n, dim int) [][]float64 {
	rng := rand.New(rand.NewSource(seed))
	out := make([][]float64, n)
	for i := range out {
		row := make([]float64, dim)
		for j := range row {
			row[j] = rng.NormFloat64()
		}
		out[i] = row
	}
	return out
}

// BenchmarkIForestFit measures training throughput: 100 trees, 256-sample
// subsamples, 5000x10 dataset.
func BenchmarkIForestFit(b *testing.B) {
	data := benchData(1, 5000, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f := NewIForest(100, 256)
		if err := f.Fit(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIForestScore measures single-sample scoring throughput.
func BenchmarkIForestScore(b *testing.B) {
	data := benchData(2, 2000, 10)
	f := NewIForest(100, 256)
	if err := f.Fit(data); err != nil {
		b.Fatal(err)
	}
	x := data[0]

	b.ReportAllocs()
	b.ResetTimer()
	var sink float64
	for i := 0; i < b.N; i++ {
		sink += f.Score(x)
	}
	if sink < 0 {
		b.Fatal("unreachable: prevents dead-code elimination")
	}
}

// BenchmarkStatisticalBaselineObserve measures the O(1) online update.
func BenchmarkStatisticalBaselineObserve(b *testing.B) {
	base := NewStatisticalBaseline(0.3)
	b.ReportAllocs()
	b.ResetTimer()
	var sink float64
	for i := 0; i < b.N; i++ {
		sink += base.Observe(float64(i % 100))
	}
	if sink < 0 {
		b.Fatal("unreachable")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func meanOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func maxOf(xs []float64) float64 {
	m := math.Inf(-1)
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}

func minOf(xs []float64) float64 {
	m := math.Inf(1)
	for _, x := range xs {
		if x < m {
			m = x
		}
	}
	return m
}
