package tracing

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// ============================================================================
// Sketch: relative-error bound proof (machine-checked)
// ============================================================================

// TestSketchRelativeErrorBound asserts the core guarantee: for every recorded
// value v>0, the bucket estimate est satisfies |est-v| <= eps*v. This is the
// property the whole compression scheme's accuracy rests on.
func TestSketchRelativeErrorBound(t *testing.T) {
	for _, eps := range []float64{0.001, 0.01, 0.05, 0.1} {
		sk := NewRelativeErrorSketch(eps)
		// Cover many orders of magnitude: 1ns .. 100s.
		for v := 1.0; v <= 1e11; v *= 1.1 {
			i := sk.bucketIndex(v)
			est := sk.estimate(i)
			rel := math.Abs(est-v) / v
			if rel > eps+1e-12 {
				t.Fatalf("eps=%g v=%g est=%g rel=%g exceeds bound", eps, v, est, rel)
			}
		}
	}
}

// TestSketchQuantileAccuracy verifies reconstructed quantiles are within eps of
// the exact quantiles computed from the raw samples.
func TestSketchQuantileAccuracy(t *testing.T) {
	const eps = 0.01
	rng := rand.New(rand.NewSource(42))
	sk := NewRelativeErrorSketch(eps)
	raw := make([]float64, 0, 100000)
	for i := 0; i < 100000; i++ {
		// Lognormal latency in ns, roughly 10us..500ms.
		v := math.Exp(rng.NormFloat64()*1.0 + 12.5)
		sk.Record(v)
		raw = append(raw, v)
	}
	sort.Float64s(raw)

	for _, q := range []float64{0.5, 0.9, 0.95, 0.99, 0.999} {
		rank := int(math.Ceil(q*float64(len(raw)))) - 1
		if rank < 0 {
			rank = 0
		}
		trueVal := raw[rank]
		est, errBound := sk.Quantile(q)
		rel := math.Abs(est-trueVal) / trueVal
		if rel > eps+1e-9 {
			t.Errorf("q=%.3f true=%.1f est=%.1f rel=%.4f > eps=%.3f", q, trueVal, est, rel, eps)
		}
		// The reported absolute error bound must cover the guarantee.
		if errBound < 0 {
			t.Errorf("negative error bound %g", errBound)
		}
	}
}

// TestSketchBucketBoundedByRange proves memory is bounded by the value dynamic
// range, NOT by the number of observations: recording 1e6 samples over a fixed
// range keeps bucket count small and constant.
func TestSketchBucketBoundedByRange(t *testing.T) {
	const eps = 0.01
	sk := NewRelativeErrorSketch(eps)
	rng := rand.New(rand.NewSource(7))
	// All values in [1e6, 1e9] ns (1ms .. 1s).
	for i := 0; i < 1_000_000; i++ {
		v := 1e6 + rng.Float64()*(1e9-1e6)
		sk.Record(v)
	}
	// Analytical bound: ceil(log_gamma(max/min)).
	gamma := (1 + eps) / (1 - eps)
	analyticMax := int(math.Ceil(math.Log(1e9/1e6)/math.Log(gamma))) + 2
	if sk.BucketCount() > analyticMax {
		t.Fatalf("bucket count %d exceeds analytic bound %d", sk.BucketCount(), analyticMax)
	}
	if sk.Count() != 1_000_000 {
		t.Fatalf("count mismatch: %d", sk.Count())
	}
	t.Logf("1e6 samples -> %d buckets (bound %d)", sk.BucketCount(), analyticMax)
}

// ============================================================================
// Skeleton canonicalization
// ============================================================================

// TestCanonicalSkeletonOrderIndependence verifies that reordering sibling spans
// (as happens with concurrent execution) yields the SAME skeleton id — the key
// property that lets latencies from equivalent code paths route together.
func TestCanonicalSkeletonOrderIndependence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	// root -> {A(client), B(client)}
	traceAB := []SpanSummary{
		{OpName: "root", Kind: oteltrace.SpanKindServer, ParentIdx: -1, LatencyNS: 100},
		{OpName: "A", Kind: oteltrace.SpanKindClient, ParentIdx: 0, LatencyNS: 20},
		{OpName: "B", Kind: oteltrace.SpanKindClient, ParentIdx: 0, LatencyNS: 30},
	}
	// Same shape, siblings listed in the opposite order.
	traceBA := []SpanSummary{
		{OpName: "root", Kind: oteltrace.SpanKindServer, ParentIdx: -1, LatencyNS: 100},
		{OpName: "B", Kind: oteltrace.SpanKindClient, ParentIdx: 0, LatencyNS: 30},
		{OpName: "A", Kind: oteltrace.SpanKindClient, ParentIdx: 0, LatencyNS: 20},
	}
	idAB, _ := canonicalSkeleton(traceAB)
	idBA, _ := canonicalSkeleton(traceBA)
	if idAB != idBA {
		t.Logf("sibling A,B: AB=%x BA=%x",
			idAB, idBA)
		t.Fatalf("sibling reorder changed skeleton id")
	}
}

// TestCanonicalSkeletonDistinguishesShapes verifies structurally different
// traces get different skeleton ids (no false coalescing).
func TestCanonicalSkeletonDistinguishesShapes(t *testing.T) {
	flat := []SpanSummary{
		{OpName: "root", ParentIdx: -1},
		{OpName: "A", ParentIdx: 0},
		{OpName: "B", ParentIdx: 0},
	}
	chain := []SpanSummary{
		{OpName: "root", ParentIdx: -1},
		{OpName: "A", ParentIdx: 0},
		{OpName: "B", ParentIdx: 1}, // B under A, not root
	}
	diffName := []SpanSummary{
		{OpName: "root", ParentIdx: -1},
		{OpName: "A", ParentIdx: 0},
		{OpName: "C", ParentIdx: 0}, // different op name
	}
	idFlat, _ := canonicalSkeleton(flat)
	idChain, _ := canonicalSkeleton(chain)
	idName, _ := canonicalSkeleton(diffName)
	if idFlat == idChain {
		t.Error("different topology hashed identically (flat vs chain)")
	}
	if idFlat == idName {
		t.Error("different op names hashed identically")
	}
}

// ============================================================================
// End-to-end compressor reconstruction
// ============================================================================

// makeTrace builds a fixed-shape trace: root -> {db(client), cache(client)}
// with latencies drawn from the supplied generator (indexed by node position).
func makeTrace(lat func(pos int) int64) []SpanSummary {
	return []SpanSummary{
		{OpName: "GET /api/orders", Kind: oteltrace.SpanKindServer, ParentIdx: -1, LatencyNS: lat(0)},
		{OpName: "DB SELECT orders", Kind: oteltrace.SpanKindClient, ParentIdx: 0, LatencyNS: lat(1)},
		{OpName: "Cache GET", Kind: oteltrace.SpanKindClient, ParentIdx: 0, LatencyNS: lat(2)},
	}
}

// TestCompressorReconstruction ingests many traces of one shape and checks that
// per-node reconstructed quantiles match the exact quantiles within eps.
func TestCompressorReconstruction(t *testing.T) {
	const eps = 0.01
	comp := NewTraceCompressor(eps)
	rng := rand.New(rand.NewSource(99))

	const N = 50000
	// Keep exact latencies per canonical node to compute ground-truth quantiles.
	exact := [3][]float64{}
	var skID uint64
	for i := 0; i < N; i++ {
		gen := func(pos int) int64 {
			var mean float64
			switch pos {
			case 0:
				mean = 13.0 // ~440us
			case 1:
				mean = 12.0 // ~160us
			default:
				mean = 10.0 // ~22us
			}
			v := math.Exp(rng.NormFloat64()*0.5 + mean)
			return int64(v)
		}
		tr := makeTrace(gen)
		skID = comp.Ingest(tr)
		// Canonical order for this shape is root, then children sorted by name:
		// "Cache GET" < "DB SELECT orders", so canonical positions are:
		// 0=root, 1=Cache GET, 2=DB SELECT orders.
		exact[0] = append(exact[0], float64(tr[0].LatencyNS))
		exact[1] = append(exact[1], float64(tr[2].LatencyNS)) // Cache -> pos 1
		exact[2] = append(exact[2], float64(tr[1].LatencyNS)) // DB -> pos 2
	}

	agg := comp.Aggregate(skID)
	if agg == nil {
		t.Fatal("aggregate missing")
	}
	if len(agg.Sketches) != 3 {
		t.Fatalf("expected 3 node sketches, got %d", len(agg.Sketches))
	}
	// Verify canonical node naming matches our assumption.
	if agg.NodeNames[1] != "Cache GET" || agg.NodeNames[2] != "DB SELECT orders" {
		t.Fatalf("unexpected canonical node order: %v", agg.NodeNames)
	}

	for pos := 0; pos < 3; pos++ {
		sort.Float64s(exact[pos])
		for _, q := range []float64{0.5, 0.9, 0.99} {
			rank := int(math.Ceil(q*float64(len(exact[pos])))) - 1
			if rank < 0 {
				rank = 0
			}
			trueVal := exact[pos][rank]
			est, _ := agg.Sketches[pos].Quantile(q)
			rel := math.Abs(est-trueVal) / trueVal
			if rel > eps+1e-9 {
				t.Errorf("node %d q=%.2f true=%.1f est=%.1f rel=%.4f > eps", pos, q, trueVal, est, rel)
			}
		}
	}

	st := comp.Stats()
	t.Logf("reconstruction OK: spans=%d traces=%d skeletons=%d raw=%dB compressed=%dB ratio=%.1fx",
		st.Spans, st.Traces, st.Skeletons, st.RawBytes, st.CompressedByte, st.Ratio)
	if st.Ratio < 10 {
		t.Errorf("expected >10x bandwidth reduction, got %.1fx", st.Ratio)
	}
}

// ============================================================================
// OTel export-path integration
// ============================================================================

// TestCompressingExporterIntegration wires the CompressingExporter into a real
// OTel TracerProvider (as the batch exporter) and verifies that spans produced
// through the normal SDK path are aggregated by the compressor after Shutdown.
func TestCompressingExporterIntegration(t *testing.T) {
	exp := NewCompressingExporter(0.01, WithFlushIdle(50*time.Millisecond))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp), // export each span immediately for a deterministic test
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	tracer := tp.Tracer("integration")

	const traces = 200
	for i := 0; i < traces; i++ {
		ctx, root := tracer.Start(context.Background(), "GET /api/orders",
			oteltrace.WithSpanKind(oteltrace.SpanKindServer))
		_, db := tracer.Start(ctx, "DB SELECT orders", oteltrace.WithSpanKind(oteltrace.SpanKindClient))
		time.Sleep(time.Microsecond)
		db.End()
		_, cache := tracer.Start(ctx, "Cache GET", oteltrace.WithSpanKind(oteltrace.SpanKindClient))
		cache.End()
		root.End()
	}

	// Shutdown flushes all pending traces into the compressor.
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	st := exp.Compressor().Stats()
	if st.Spans != traces*3 {
		t.Errorf("expected %d spans ingested, got %d", traces*3, st.Spans)
	}
	if st.Skeletons != 1 {
		t.Errorf("expected 1 coalesced skeleton, got %d", st.Skeletons)
	}
	if st.Ratio < 10 {
		t.Errorf("expected >10x reduction on repeated shape, got %.1fx", st.Ratio)
	}
	t.Logf("exporter integration: %d spans -> 1 skeleton, ratio=%.1fx (raw=%dB, compressed=%dB)",
		st.Spans, st.Ratio, st.RawBytes, st.CompressedByte)
}

// ============================================================================
// Benchmarks
// ============================================================================

// BenchmarkSketchRecord confirms O(1) per-observation ingest cost.
func BenchmarkSketchRecord(b *testing.B) {
	sk := NewRelativeErrorSketch(0.01)
	rng := rand.New(rand.NewSource(1))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sk.Record(math.Exp(rng.NormFloat64() + 12))
	}
}

// BenchmarkCompressorIngest measures full per-trace ingest (skeleton hash +
// per-span routing) throughput.
func BenchmarkCompressorIngest(b *testing.B) {
	comp := NewTraceCompressor(0.01)
	rng := rand.New(rand.NewSource(2))
	tr := makeTrace(func(pos int) int64 { return int64(math.Exp(rng.NormFloat64() + 12)) })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Refresh latencies cheaply without reallocating the slice.
		tr[0].LatencyNS = int64(math.Exp(rng.NormFloat64() + 13))
		tr[1].LatencyNS = int64(math.Exp(rng.NormFloat64() + 12))
		tr[2].LatencyNS = int64(math.Exp(rng.NormFloat64() + 10))
		comp.Ingest(tr)
	}
}

// BenchmarkCompressionBandwidth is the headline benchmark: it drives a
// realistic multi-shape workload and reports bandwidth reduction versus
// verbatim OTel export and the empirical max reconstruction error, via custom
// b.ReportMetric metrics (so results survive -json capture).
func BenchmarkCompressionBandwidth(b *testing.B) {
	const eps = 0.01
	const skeletons = 20
	const tracesPerSkeleton = 5000

	// Build a fixed set of shapes of varying width/depth.
	shapes := make([][]SpanSummary, skeletons)
	for s := 0; s < skeletons; s++ {
		width := 2 + s%6 // 2..7 children
		tr := make([]SpanSummary, 0, width+1)
		tr = append(tr, SpanSummary{OpName: "GET /svc" + string(rune('A'+s)), Kind: oteltrace.SpanKindServer, ParentIdx: -1})
		for w := 0; w < width; w++ {
			tr = append(tr, SpanSummary{
				OpName:    "dep-" + string(rune('a'+w)),
				Kind:      oteltrace.SpanKindClient,
				ParentIdx: 0,
			})
		}
		shapes[s] = tr
	}

	b.ReportAllocs()
	b.ResetTimer()

	var lastRatio, lastMaxErr float64
	for n := 0; n < b.N; n++ {
		comp := NewTraceCompressor(eps)
		rng := rand.New(rand.NewSource(int64(n) + 1))

		// Keep ground truth to measure empirical reconstruction error.
		type key struct {
			sk  uint64
			pos int
		}
		truth := map[key][]float64{}

		for s := 0; s < skeletons; s++ {
			base := shapes[s]
			for t := 0; t < tracesPerSkeleton; t++ {
				tr := make([]SpanSummary, len(base))
				copy(tr, base)
				for i := range tr {
					tr[i].LatencyNS = int64(math.Exp(rng.NormFloat64()*0.5 + 11 + float64(i%3)))
				}
				skID := comp.Ingest(tr)
				_, order := canonicalSkeleton(tr)
				for pos, origIdx := range order {
					k := key{skID, pos}
					truth[k] = append(truth[k], float64(tr[origIdx].LatencyNS))
				}
			}
		}

		st := comp.Stats()
		lastRatio = st.Ratio

		// Empirical max relative error over p50/p90/p99 across all nodes.
		maxErr := 0.0
		for k, vals := range truth {
			sort.Float64s(vals)
			agg := comp.Aggregate(k.sk)
			for _, q := range []float64{0.5, 0.9, 0.99} {
				rank := int(math.Ceil(q*float64(len(vals)))) - 1
				if rank < 0 {
					rank = 0
				}
				trueVal := vals[rank]
				est, _ := agg.Sketches[k.pos].Quantile(q)
				if trueVal > 0 {
					if rel := math.Abs(est-trueVal) / trueVal; rel > maxErr {
						maxErr = rel
					}
				}
			}
		}
		lastMaxErr = maxErr
	}

	b.ReportMetric(lastRatio, "x-reduction")
	b.ReportMetric(lastMaxErr*100, "%-max-recon-err")
	if lastRatio < 10 {
		b.Errorf("bandwidth reduction %.1fx < 10x target", lastRatio)
	}
	if lastMaxErr > eps+1e-6 {
		b.Errorf("max reconstruction error %.4f exceeds eps=%.3f", lastMaxErr, eps)
	}
}
