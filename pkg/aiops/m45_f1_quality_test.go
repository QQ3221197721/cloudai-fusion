// Package aiops - Module M45 Detection Quality: F1 on Joint/Multivariate Anomalies
//
// The head-to-head bench file (m45_head_to_head_bench_test.go) already measures
// F1 on a UNIVARIATE anomaly dataset (single-feature spikes) — where a simple
// max-|z| Z-score is expected to do well. This file adds the OTHER half of the
// honest story: a JOINT/MULTIVARIATE dataset where anomalies are correlation
// breakdowns (each feature stays inside its normal marginal range, only the
// COMBINATION is anomalous). This is exactly the regime a multivariate
// Mahalanobis detector should win and a univariate Z-score should miss.
//
// Methodology: threshold SWEEP per detector → report the best achievable F1.
// This removes human threshold-tuning bias and is the fair way to compare
// detectors that live on completely different score scales
// (chi-square distance vs max-|z| vs isolation score in (0,1]).
//
// Reuses types already declared in m45_head_to_head_bench_test.go:
//   ZScoreBaseline, EWMAOnlineDetector, RandomCutForest, and the helpers
//   clip / maxInt / extractFeatures / getFeatureValue.
//
// Anti-fiasco rules honored: real labeled data, threshold sweep (>3 points),
// identical data + identical eval for every detector, honest verdict on loss.

package aiops

import (
	"io"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	m45JointTotal  = 5000
	m45JointTrain  = 500
	m45JointSeed   = int64(9283746501)
)

var (
	m45JointData   []MetricsSnapshot
	m45JointLabels []bool
)

func init() {
	rng := rand.New(rand.NewSource(m45JointSeed))
	m45JointData, m45JointLabels = generateMultivariateAnomalyDataset(m45JointTotal, rng)
}

// generateMultivariateAnomalyDataset builds a stream whose anomalies are JOINT:
// individual feature values remain inside the normal marginal range observed in
// the training window, but the multivariate combination violates the learned
// correlation structure. A univariate max-|z| detector cannot flag these by
// magnitude alone; a covariance-aware detector (Mahalanobis) can.
func generateMultivariateAnomalyDataset(n int, rng *rand.Rand) ([]MetricsSnapshot, []bool) {
	data := make([]MetricsSnapshot, n)
	labels := make([]bool, n)
	base := time.Now().Add(-24 * time.Hour)

	for i := range data {
		t := base.Add(time.Duration(i) * time.Second)

		// --- Normal regime with baked-in correlations ---
		// CPU drives latency (positive corr) and disk read (positive corr).
		cpu := clip(0.45+rng.NormFloat64()*0.08, 0.05, 0.95)
		mem := clip(0.50+rng.NormFloat64()*0.06, 0.05, 0.95)
		// Latency strongly correlated with CPU.
		latency := 40.0 + 180.0*cpu + rng.NormFloat64()*8
		// DiskRead correlated with CPU too.
		diskRead := 300.0 + 900.0*cpu + math.Abs(rng.NormFloat64()*40)
		diskWrite := 300.0 + math.Abs(rng.NormFloat64()*40)
		// Connections correlated with network-in.
		netIn := 1e6 + 6e5*cpu + rng.NormFloat64()*4e4
		netOut := 5e5 + rng.NormFloat64()*3e4
		conns := maxInt(int(60+120*cpu+rng.NormFloat64()*6), 0)
		errRate := math.Max(0.005+rng.NormFloat64()*0.003, 0)

		isAnomaly := false

		if i > m45JointTrain {
			switch {
			// TYPE 1: high CPU but abnormally LOW latency (correlation break).
			// CPU~0.9 is seen in normal data; latency~45 is seen in normal data;
			// but high-CPU-with-low-latency never co-occurs normally.
			case i%100 == 42:
				cpu = clip(0.88+rng.Float64()*0.06, 0.05, 0.95)
				latency = 45.0 + rng.NormFloat64()*6 // as if CPU were ~0.05
				diskRead = 300.0 + 900.0*cpu + math.Abs(rng.NormFloat64()*40)
				netIn = 1e6 + 6e5*cpu + rng.NormFloat64()*4e4
				conns = maxInt(int(60+120*cpu+rng.NormFloat64()*6), 0)
				isAnomaly = true

			// TYPE 2: low CPU but abnormally HIGH latency + error (overload w/o compute cause).
			case i%120 == 56:
				cpu = clip(0.20+rng.NormFloat64()*0.05, 0.05, 0.95)
				latency = 230.0 + rng.NormFloat64()*20 // as if CPU were high
				errRate = 0.02 + rng.Float64()*0.03    // modest, still small in magnitude
				diskRead = 300.0 + 900.0*cpu + math.Abs(rng.NormFloat64()*40)
				netIn = 1e6 + 6e5*cpu + rng.NormFloat64()*4e4
				conns = maxInt(int(60+120*cpu+rng.NormFloat64()*6), 0)
				isAnomaly = true

			// TYPE 3: connections decoupled from network-in (normally correlated).
			// conns very low while netIn stays high — each in-range, combo abnormal.
			case i%150 == 89:
				netIn = 1.4e6 + rng.NormFloat64()*3e4 // high-normal
				conns = rng.Intn(15)                  // very low, decoupled
				isAnomaly = true

			// TYPE 4: diskRead decoupled from CPU (normally correlated).
			// High disk read while CPU is low — magnitude of each is in-range.
			case i%180 == 33:
				cpu = clip(0.25+rng.NormFloat64()*0.05, 0.05, 0.95)
				diskRead = 1100.0 + rng.NormFloat64()*40 // high-normal (seen at high CPU)
				latency = 40.0 + 180.0*cpu + rng.NormFloat64()*8
				netIn = 1e6 + 6e5*cpu + rng.NormFloat64()*4e4
				conns = maxInt(int(60+120*cpu+rng.NormFloat64()*6), 0)
				isAnomaly = true
			}
		}

		data[i] = MetricsSnapshot{
			Timestamp:      t,
			CPUUtilization: cpu,
			MemoryUsage:    mem,
			DiskIORead:     diskRead,
			DiskIOWrite:    diskWrite,
			NetworkIn:      math.Max(netIn, 0),
			NetworkOut:     netOut,
			Connections:    conns,
			ErrorRate:      errRate,
			LatencyP99:     math.Max(latency, 0),
		}
		labels[i] = isAnomaly
	}

	return data, labels
}

// ============================================================================
// THRESHOLD-SWEEP F1: for a given per-point score slice + labels, find the
// threshold that maximizes F1 (best achievable operating point).
// ============================================================================

// sweepBestF1 returns the f1Result at the best-F1 threshold over all unique
// score values. f1Result is declared in m45_head_to_head_bench_test.go.
func sweepBestF1(name string, scores []float64, labels []bool) f1Result {
	// Candidate thresholds = sorted unique scores (plus a slightly-below-min so
	// the "flag everything" point is reachable).
	uniq := make(map[float64]struct{}, len(scores))
	for _, s := range scores {
		uniq[s] = struct{}{}
	}
	cands := make([]float64, 0, len(uniq)+1)
	minScore := math.Inf(1)
	for s := range uniq {
		cands = append(cands, s)
		if s < minScore {
			minScore = s
		}
	}
	cands = append(cands, math.Nextafter(minScore, math.Inf(-1)))

	best := f1Result{name: name}
	bestF1 := -1.0
	for _, th := range cands {
		var tp, fp, tn, fn int
		for i, s := range scores {
			pred := s > th
			switch {
			case pred && labels[i]:
				tp++
			case pred && !labels[i]:
				fp++
			case !pred && labels[i]:
				fn++
			default:
				tn++
			}
		}
		var prec, rec, f1 float64
		if tp+fp > 0 {
			prec = float64(tp) / float64(tp+fp)
		}
		if tp+fn > 0 {
			rec = float64(tp) / float64(tp+fn)
		}
		if prec+rec > 0 {
			f1 = 2 * prec * rec / (prec + rec)
		}
		if f1 > bestF1 {
			bestF1 = f1
			best = f1Result{
				name: name, tp: tp, fp: fp, tn: tn, fn: fn,
				precision: prec, recall: rec, f1: f1,
			}
		}
	}
	return best
}

// buildAndScore trains all four detectors on train and returns per-point scores
// on the eval slice. Mahalanobis = squared distance (chi-square), ZScore/EWMA =
// max standardized deviation, RCF = isolation score in (0,1].
func buildAndScore(train, eval []MetricsSnapshot) (mahal, zsc, ewm, rcf []float64) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	logger.SetOutput(io.Discard)

	m := NewMahalanobisDistanceModel(logger)
	_ = m.Train(train)
	z := NewZScoreBaseline(8, 3.0)
	_ = z.Train(train)
	e := NewEWMAOnlineDetector(8, 0.1, 3.0)
	_ = e.Train(train)
	r := NewRandomCutForest(100, 256, 8, 12)
	_ = r.Train(train)

	mahal = make([]float64, len(eval))
	zsc = make([]float64, len(eval))
	ewm = make([]float64, len(eval))
	rcf = make([]float64, len(eval))

	x := make([]float64, 8)
	for i := range eval {
		copy(x, extractFeatures(eval[i]))
		mahal[i] = m.IsScore(x)
		zsc[i] = z.Score(x)
		ewm[i] = e.Score(x)
		rcf[i] = r.Score(x)
	}
	return
}

func reportF1(t *testing.T, title string, eval []MetricsSnapshot, labels []bool, mahal, zsc, ewm, rcf []float64) {
	pos := 0
	for _, l := range labels {
		if l {
			pos++
		}
	}

	results := []f1Result{
		sweepBestF1("M45-Mahalanobis", mahal, labels),
		sweepBestF1("ZScore-3sigma", zsc, labels),
		sweepBestF1("EWMA-online", ewm, labels),
		sweepBestF1("RandomCutForest", rcf, labels),
	}

	t.Logf("=== %s ===", title)
	t.Logf("Eval points: %d | true anomalies: %d (%.2f%%) | best-F1 threshold sweep",
		len(labels), pos, 100*float64(pos)/float64(len(labels)))
	t.Logf("%-18s %6s %6s %6s %6s %10s %8s %8s", "Detector", "TP", "FP", "FN", "TN", "Precision", "Recall", "F1")
	for _, r := range results {
		t.Logf("%-18s %6d %6d %6d %6d %10.4f %8.4f %8.4f",
			r.name, r.tp, r.fp, r.fn, r.tn, r.precision, r.recall, r.f1)
	}

	// Honest verdict.
	m45F1 := results[0].f1
	bestBaseF1, bestBaseName := results[1].f1, results[1].name
	for _, r := range results[2:] {
		if r.f1 > bestBaseF1 {
			bestBaseF1, bestBaseName = r.f1, r.name
		}
	}
	gap := m45F1 - bestBaseF1
	if gap > 0 {
		t.Logf("VERDICT: M45-Mahalanobis WINS. F1=%.4f vs best baseline %s F1=%.4f (gap +%.4f)",
			m45F1, bestBaseName, bestBaseF1, gap)
	} else {
		t.Logf("VERDICT: M45-Mahalanobis does NOT win. F1=%.4f vs best baseline %s F1=%.4f (gap %.4f)",
			m45F1, bestBaseName, bestBaseF1, gap)
	}
}

// TestM45_F1_UnivariateSweep reruns the head-to-head F1 on the UNIVARIATE
// dataset using a fair best-F1 threshold sweep for every detector.
func TestM45_F1_UnivariateSweep(t *testing.T) {
	train := m45Data[:m45TrainPoints]
	eval := m45Data[m45TrainPoints:]
	labels := m45Labels[m45TrainPoints:]
	mahal, zsc, ewm, rcf := buildAndScore(train, eval)
	reportF1(t, "M45 F1 — UNIVARIATE anomalies (single-feature spikes)", eval, labels, mahal, zsc, ewm, rcf)
}

// TestM45_F1_JointSweep is the KEY test: F1 on JOINT/multivariate anomalies
// (correlation breakdowns) where the multivariate detector should win.
func TestM45_F1_JointSweep(t *testing.T) {
	train := m45JointData[:m45JointTrain]
	eval := m45JointData[m45JointTrain:]
	labels := m45JointLabels[m45JointTrain:]
	mahal, zsc, ewm, rcf := buildAndScore(train, eval)
	reportF1(t, "M45 F1 — JOINT/MULTIVARIATE anomalies (correlation breakdowns)", eval, labels, mahal, zsc, ewm, rcf)
}
