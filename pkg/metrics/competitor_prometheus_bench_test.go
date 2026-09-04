package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
)

// ============================================================================
// T2 M46 Head-to-Head: Our Exact Quantile vs Prometheus Histogram/Summary
// ============================================================================
//
// This file adds a real head-to-head benchmark of the pkg/metrics in-memory
// exact-quantile path (ring-buffer sliding window from slo.go + the exact
// sort-based percentile() query) against the real Prometheus client_golang
// v1.19.0 competitor (prometheus.Histogram / prometheus.Summary).
//
// Two dimensions are covered:
//   Part A — Observe/Insert throughput.
//   Part B — Query accuracy + latency. Prometheus does NOT expose a client-side
//            quantile method; histogram_quantile is computed server-side in
//            PromQL. We therefore Gather() the metric and apply the exact
//            histogram_quantile linear-interpolation algorithm on the exported
//            buckets (the same math Prometheus server uses), and read the
//            Summary's estimated objective quantiles from the exported dto.
//
// This is a NEW file. It does not modify any production or existing test code.

// -------------------------------------------------------------------------
// Deterministic sample data (skewed latency-like distribution)
// -------------------------------------------------------------------------

var benchSamples []float64

func init() {
	const n = 10000
	benchSamples = make([]float64, n)
	// Deterministic xorshift* PRNG so results are reproducible across runs.
	x := uint64(0x9E3779B97F4A7C15)
	for i := 0; i < n; i++ {
		x ^= x >> 12
		x ^= x << 25
		x ^= x >> 27
		r := (x * 0x2545F4914F6CDD1D) >> 11 // 53-bit mantissa
		u := float64(r) / float64(1<<53)    // uniform in [0,1)
		// Skewed toward small values, long tail — like HTTP latencies (seconds).
		benchSamples[i] = u*u*u*2.0 + u*0.05 + 0.001
	}
}

// -------------------------------------------------------------------------
// Part A: Observe / Insert throughput
// -------------------------------------------------------------------------

// BenchmarkInsertThroughput_OurSlidingWindow measures the pure insert cost of
// our exact-quantile data structure: the ring-buffer sliding window used by
// SLOTracker (slo.go). This mirrors exactly what RecordRequest does to store a
// latency sample, isolated from the Prometheus counters it also updates.
func BenchmarkInsertThroughput_OurSlidingWindow(b *testing.B) {
	w := newSlidingWindow(10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := benchSamples[i%len(benchSamples)]
		w.latencies[w.latencyIdx] = v
		w.latencyIdx = (w.latencyIdx + 1) % w.windowSize
		if w.latencyIdx == 0 {
			w.latencyFull = true
		}
	}
}

// BenchmarkInsertThroughput_PrometheusHistogram uses the real
// prometheus.Histogram.Observe (competitor).
func BenchmarkInsertThroughput_PrometheusHistogram(b *testing.B) {
	reg := prometheus.NewRegistry()
	h := promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Name:    "bench_insert_hist_seconds",
		Help:    "insert throughput histogram",
		Buckets: prometheus.DefBuckets,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Observe(benchSamples[i%len(benchSamples)])
	}
}

// BenchmarkInsertThroughput_PrometheusSummary uses the real
// prometheus.Summary.Observe (competitor). Summary maintains a streaming
// quantile estimator (CKMS) with configured objective error bounds.
func BenchmarkInsertThroughput_PrometheusSummary(b *testing.B) {
	reg := prometheus.NewRegistry()
	s := promauto.With(reg).NewSummary(prometheus.SummaryOpts{
		Name:       "bench_insert_summary_seconds",
		Help:       "insert throughput summary",
		Objectives: map[float64]float64{0.5: 0.05, 0.95: 0.01, 0.99: 0.001},
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Observe(benchSamples[i%len(benchSamples)])
	}
}

// -------------------------------------------------------------------------
// Part B: Query latency
// -------------------------------------------------------------------------

// BenchmarkQueryLatency_OurExactPercentile measures the cost of computing an
// EXACT p99 via our percentile() over a full 10k-sample window.
func BenchmarkQueryLatency_OurExactPercentile(b *testing.B) {
	data := make([]float64, len(benchSamples))
	copy(data, benchSamples)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = percentile(data, 0.99)
	}
}

// BenchmarkQueryLatency_PrometheusHistogram measures the cost of Gather()+the
// histogram_quantile bucket interpolation (the real PromQL server-side path).
func BenchmarkQueryLatency_PrometheusHistogram(b *testing.B) {
	reg := prometheus.NewRegistry()
	h := promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Name:    "bench_query_hist_seconds",
		Help:    "query latency histogram",
		Buckets: prometheus.DefBuckets,
	})
	for _, v := range benchSamples {
		h.Observe(v)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mfs, _ := reg.Gather()
		_ = histogramQuantileFromGather(mfs, "bench_query_hist_seconds", 0.99)
	}
}

// BenchmarkQueryLatency_PrometheusSummary measures the cost of Gather()+reading
// the pre-computed streaming objective quantile from the exported dto.
func BenchmarkQueryLatency_PrometheusSummary(b *testing.B) {
	reg := prometheus.NewRegistry()
	s := promauto.With(reg).NewSummary(prometheus.SummaryOpts{
		Name:       "bench_query_summary_seconds",
		Help:       "query latency summary",
		Objectives: map[float64]float64{0.5: 0.05, 0.95: 0.01, 0.99: 0.001},
	})
	for _, v := range benchSamples {
		s.Observe(v)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mfs, _ := reg.Gather()
		_ = summaryQuantileFromGather(mfs, "bench_query_summary_seconds", 0.99)
	}
}

// -------------------------------------------------------------------------
// Part B: Query accuracy (the T2 differentiator)
// -------------------------------------------------------------------------

// TestAccuracy_OurExactPercentile asserts our percentile() is exact (only
// floating-point rounding error) against the ground truth over the same data.
func TestAccuracy_OurExactPercentile(t *testing.T) {
	trueP95 := groundTruthPercentile(benchSamples, 0.95)
	trueP99 := groundTruthPercentile(benchSamples, 0.99)

	// Feed the same data through the production ring buffer, then query.
	w := newSlidingWindow(len(benchSamples))
	for _, v := range benchSamples {
		w.latencies[w.latencyIdx] = v
		w.latencyIdx = (w.latencyIdx + 1) % w.windowSize
		if w.latencyIdx == 0 {
			w.latencyFull = true
		}
	}
	p95 := percentile(w.latencies[:len(benchSamples)], 0.95)
	p99 := percentile(w.latencies[:len(benchSamples)], 0.99)

	errP95 := absFloat(p95 - trueP95)
	errP99 := absFloat(p99 - trueP99)
	t.Logf("OUR EXACT: p95=%.8f (truth=%.8f, abs_err=%.3e) p99=%.8f (truth=%.8f, abs_err=%.3e)",
		p95, trueP95, errP95, p99, trueP99, errP99)

	const tol = 1e-9
	if errP95 > tol || errP99 > tol {
		t.Errorf("exact percentile deviated beyond fp tolerance: p95_err=%.3e p99_err=%.3e", errP95, errP99)
	}
}

// TestAccuracy_PrometheusHistogramBucketApprox measures the quantization error
// of the Prometheus histogram bucket approximation vs ground truth.
func TestAccuracy_PrometheusHistogramBucketApprox(t *testing.T) {
	trueP95 := groundTruthPercentile(benchSamples, 0.95)
	trueP99 := groundTruthPercentile(benchSamples, 0.99)

	reg := prometheus.NewRegistry()
	h := promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Name:    "bench_acc_hist_seconds",
		Help:    "accuracy histogram",
		Buckets: prometheus.DefBuckets,
	})
	for _, v := range benchSamples {
		h.Observe(v)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}
	approxP95 := histogramQuantileFromGather(mfs, "bench_acc_hist_seconds", 0.95)
	approxP99 := histogramQuantileFromGather(mfs, "bench_acc_hist_seconds", 0.99)

	relP95 := absFloat(approxP95-trueP95) / trueP95 * 100
	relP99 := absFloat(approxP99-trueP99) / trueP99 * 100
	t.Logf("PROM HISTOGRAM APPROX: p95=%.8f (truth=%.8f, rel_err=%.2f%%) p99=%.8f (truth=%.8f, rel_err=%.2f%%)",
		approxP95, trueP95, relP95, approxP99, trueP99, relP99)
	t.Logf("Histogram bucket quantization error is inherent to fixed-bucket design (documented T2 tradeoff, not a failure)")
}

// TestAccuracy_PrometheusSummaryObjective measures the streaming-estimator error
// of the Prometheus summary objective quantiles vs ground truth.
func TestAccuracy_PrometheusSummaryObjective(t *testing.T) {
	trueP95 := groundTruthPercentile(benchSamples, 0.95)
	trueP99 := groundTruthPercentile(benchSamples, 0.99)

	reg := prometheus.NewRegistry()
	s := promauto.With(reg).NewSummary(prometheus.SummaryOpts{
		Name:       "bench_acc_summary_seconds",
		Help:       "accuracy summary",
		Objectives: map[float64]float64{0.95: 0.01, 0.99: 0.001},
	})
	for _, v := range benchSamples {
		s.Observe(v)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}
	approxP95 := summaryQuantileFromGather(mfs, "bench_acc_summary_seconds", 0.95)
	approxP99 := summaryQuantileFromGather(mfs, "bench_acc_summary_seconds", 0.99)

	relP95 := absFloat(approxP95-trueP95) / trueP95 * 100
	relP99 := absFloat(approxP99-trueP99) / trueP99 * 100
	t.Logf("PROM SUMMARY OBJECTIVE: p95=%.8f (truth=%.8f, rel_err=%.2f%%) p99=%.8f (truth=%.8f, rel_err=%.2f%%)",
		approxP95, trueP95, relP95, approxP99, trueP99, relP99)
	t.Logf("Summary objectives guarantee only a bounded rank error (alpha); estimation error is inherent (documented T2 tradeoff)")
}

// -------------------------------------------------------------------------
// Helpers (test-only)
// -------------------------------------------------------------------------

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// groundTruthPercentile computes the exact percentile with a full O(n log n)
// sort, independent of the production percentile() implementation, so it is a
// trustworthy reference for the accuracy comparison.
func groundTruthPercentile(data []float64, p float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sorted := make([]float64, len(data))
	copy(sorted, data)
	// Simple heap-free stdlib-style sort via insertion is O(n^2); use quicksort.
	quicksort(sorted, 0, len(sorted)-1)
	idx := p * float64(len(sorted)-1)
	lo := int(idx)
	if float64(lo) == idx || lo+1 >= len(sorted) {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[lo+1]*frac
}

func quicksort(a []float64, lo, hi int) {
	for lo < hi {
		p := a[(lo+hi)/2]
		i, j := lo, hi
		for i <= j {
			for a[i] < p {
				i++
			}
			for a[j] > p {
				j--
			}
			if i <= j {
				a[i], a[j] = a[j], a[i]
				i++
				j--
			}
		}
		if j-lo < hi-i {
			quicksort(a, lo, j)
			lo = i
		} else {
			quicksort(a, i, hi)
			hi = j
		}
	}
}

// histogramQuantileFromGather replicates PromQL histogram_quantile: it reads the
// cumulative bucket counts exported by the given metric family and performs the
// standard linear interpolation within the bucket that contains the rank.
func histogramQuantileFromGather(mfs []*dto.MetricFamily, name string, q float64) float64 {
	var hist *dto.Histogram
	for _, mf := range mfs {
		if mf.GetName() == name && mf.GetType() == dto.MetricType_HISTOGRAM {
			ms := mf.GetMetric()
			if len(ms) > 0 {
				hist = ms[0].GetHistogram()
			}
			break
		}
	}
	if hist == nil {
		return 0
	}
	total := float64(hist.GetSampleCount())
	if total == 0 {
		return 0
	}
	rank := q * total
	buckets := hist.GetBucket()
	var prevCount float64
	var prevBound float64
	for _, bkt := range buckets {
		cum := float64(bkt.GetCumulativeCount())
		ub := bkt.GetUpperBound()
		if cum >= rank {
			if cum == prevCount {
				return ub
			}
			// Linear interpolation between prevBound and ub.
			frac := (rank - prevCount) / (cum - prevCount)
			return prevBound + frac*(ub-prevBound)
		}
		prevCount = cum
		prevBound = ub
	}
	// Above the last finite bucket: return the highest finite bound.
	if len(buckets) > 0 {
		return buckets[len(buckets)-1].GetUpperBound()
	}
	return 0
}

// summaryQuantileFromGather reads the pre-computed objective quantile value that
// the Prometheus summary exported for the requested quantile.
func summaryQuantileFromGather(mfs []*dto.MetricFamily, name string, q float64) float64 {
	for _, mf := range mfs {
		if mf.GetName() == name && mf.GetType() == dto.MetricType_SUMMARY {
			ms := mf.GetMetric()
			if len(ms) == 0 {
				return 0
			}
			for _, qq := range ms[0].GetSummary().GetQuantile() {
				if absFloat(qq.GetQuantile()-q) < 1e-9 {
					return qq.GetValue()
				}
			}
		}
	}
	return 0
}
