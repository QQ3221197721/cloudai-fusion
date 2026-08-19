package observability

// metrics_test.go verifies Module 46: collector fan-out, bidirectional
// Prometheus exposition support, and the aggregation pipeline.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func staticCollector(name string, samples ...Sample) Collector {
	return CollectorFunc{
		CollectorName: name,
		Fn:            func(context.Context) ([]Sample, error) { return samples, nil },
	}
}

// TestMultiCollectorFanOut checks that samples from several sources are merged
// and that common labels are stamped without overwriting source labels.
func TestMultiCollectorFanOut(t *testing.T) {
	mc := NewMultiCollector(map[string]string{"cluster": "prod", "region": "eu-west-1"})
	mc.Register(
		staticCollector("cpu", Sample{Name: "cpu_usage", Value: 0.4, Labels: map[string]string{"node": "n1"}}),
		staticCollector("mem", Sample{Name: "mem_bytes", Value: 2048, Labels: map[string]string{"node": "n2"}}),
		// This source sets cluster itself; the common label must not clobber it.
		staticCollector("edge", Sample{Name: "edge_rtt", Value: 12, Labels: map[string]string{"cluster": "edge-1"}}),
	)

	if mc.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", mc.Len())
	}

	samples, err := mc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("collected %d samples, want 3", len(samples))
	}

	byName := make(map[string]Sample)
	for _, s := range samples {
		byName[s.Name] = s
	}
	if got := byName["cpu_usage"].Labels["cluster"]; got != "prod" {
		t.Errorf("cpu_usage cluster label = %q, want %q", got, "prod")
	}
	if got := byName["cpu_usage"].Labels["region"]; got != "eu-west-1" {
		t.Errorf("cpu_usage region label = %q, want %q", got, "eu-west-1")
	}
	if got := byName["edge_rtt"].Labels["cluster"]; got != "edge-1" {
		t.Errorf("source-provided cluster label was overwritten: got %q, want %q", got, "edge-1")
	}
}

// TestMultiCollectorPartialFailure asserts a broken source is reported but does
// not suppress the healthy sources' samples.
func TestMultiCollectorPartialFailure(t *testing.T) {
	sentinel := errors.New("scrape target unreachable")
	mc := NewMultiCollector(nil)
	mc.Register(
		staticCollector("good", Sample{Name: "up", Value: 1}),
		CollectorFunc{
			CollectorName: "broken",
			Fn:            func(context.Context) ([]Sample, error) { return nil, sentinel },
		},
	)

	samples, err := mc.Collect(context.Background())
	if err == nil {
		t.Fatal("Collect returned nil error despite a failing collector")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error does not wrap the source cause: %v", err)
	}
	var ce *CollectError
	if !errors.As(err, &ce) {
		t.Errorf("error is not a *CollectError: %v", err)
	} else if ce.Collector != "broken" {
		t.Errorf("CollectError.Collector = %q, want %q", ce.Collector, "broken")
	}
	if len(samples) != 1 {
		t.Errorf("healthy samples lost on partial failure: got %d, want 1", len(samples))
	}
}

// TestMultiCollectorConcurrent stresses Collect from many goroutines while
// registration happens, guarding the RWMutex discipline.
//
// Note: -race is unavailable on this Windows/no-CGO toolchain, so this is a
// WaitGroup concurrency stress test, not a race-detector run.
func TestMultiCollectorConcurrent(t *testing.T) {
	mc := NewMultiCollector(map[string]string{"env": "test"})
	for i := 0; i < 8; i++ {
		mc.Register(staticCollector(fmt.Sprintf("src%d", i),
			Sample{Name: fmt.Sprintf("metric_%d", i), Value: float64(i)}))
	}

	const goroutines = 32
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				s, err := mc.Collect(context.Background())
				if err != nil {
					errCh <- err
					return
				}
				if len(s) != 8 {
					errCh <- fmt.Errorf("got %d samples, want 8", len(s))
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// TestMultiCollectorContextCancel verifies a cancelled context is surfaced.
func TestMultiCollectorContextCancel(t *testing.T) {
	mc := NewMultiCollector(nil)
	mc.Register(staticCollector("cpu", Sample{Name: "cpu", Value: 1}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := mc.Collect(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Collect with cancelled ctx: error = %v, want context.Canceled", err)
	}
}

// TestPrometheusRoundTrip is the core Module 46 requirement: write then parse
// must preserve names, labels, values, and timestamps.
func TestPrometheusRoundTrip(t *testing.T) {
	ts := time.UnixMilli(1_700_000_000_000).UTC()
	in := []Sample{
		{
			Name: "http_requests_total", Type: MetricCounter, Help: "Total HTTP requests.",
			Labels: map[string]string{"method": "GET", "code": "200"}, Value: 1027, Timestamp: ts,
		},
		{
			Name: "http_requests_total", Type: MetricCounter,
			Labels: map[string]string{"method": "POST", "code": "500"}, Value: 3, Timestamp: ts,
		},
		{Name: "process_open_fds", Type: MetricGauge, Help: "Open file descriptors.", Value: 17},
	}

	var buf bytes.Buffer
	if err := WritePrometheus(&buf, in); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	text := buf.String()

	for _, want := range []string{
		"# HELP http_requests_total Total HTTP requests.",
		"# TYPE http_requests_total counter",
		`http_requests_total{code="200",method="GET"} 1027 1700000000000`,
		"# TYPE process_open_fds gauge",
		"process_open_fds 17",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("exposition output missing %q\n--- got ---\n%s", want, text)
		}
	}

	out, err := ParsePrometheus(strings.NewReader(text))
	if err != nil {
		t.Fatalf("ParsePrometheus: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("round-trip sample count = %d, want %d", len(out), len(in))
	}

	// Locate the GET series and compare field by field.
	var got *Sample
	for i := range out {
		if out[i].Name == "http_requests_total" && out[i].Labels["method"] == "GET" {
			got = &out[i]
			break
		}
	}
	if got == nil {
		t.Fatal("GET series missing after round trip")
	}
	if got.Value != 1027 {
		t.Errorf("value = %v, want 1027", got.Value)
	}
	if !got.Timestamp.Equal(ts) {
		t.Errorf("timestamp = %v, want %v", got.Timestamp, ts)
	}
	if got.Type != MetricCounter {
		t.Errorf("type = %q, want %q", got.Type, MetricCounter)
	}
	if got.Help != "Total HTTP requests." {
		t.Errorf("help = %q, want %q", got.Help, "Total HTTP requests.")
	}
	if got.Labels["code"] != "200" {
		t.Errorf("code label = %q, want 200", got.Labels["code"])
	}
}

// TestPrometheusWriteDeterministic checks byte-stable output for equal input.
func TestPrometheusWriteDeterministic(t *testing.T) {
	samples := []Sample{
		{Name: "b_metric", Value: 2, Labels: map[string]string{"z": "1", "a": "2", "m": "3"}},
		{Name: "a_metric", Value: 1, Labels: map[string]string{"q": "9", "b": "8"}},
	}
	var first bytes.Buffer
	if err := WritePrometheus(&first, samples); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	for i := 0; i < 20; i++ {
		var again bytes.Buffer
		if err := WritePrometheus(&again, samples); err != nil {
			t.Fatalf("WritePrometheus: %v", err)
		}
		if first.String() != again.String() {
			t.Fatalf("output not deterministic on iteration %d", i)
		}
	}
	// Metric names sorted, labels sorted within a series.
	if !strings.Contains(first.String(), `a_metric{b="8",q="9"} 1`) {
		t.Errorf("labels not sorted:\n%s", first.String())
	}
	if idx1, idx2 := strings.Index(first.String(), "a_metric"), strings.Index(first.String(), "b_metric"); idx1 > idx2 {
		t.Error("metric families not emitted in sorted name order")
	}
}

// TestPrometheusSpecialValues covers NaN/+Inf/-Inf tokens in both directions.
func TestPrometheusSpecialValues(t *testing.T) {
	in := []Sample{
		{Name: "m_nan", Value: math.NaN(), Type: MetricGauge},
		{Name: "m_pinf", Value: math.Inf(1), Type: MetricGauge},
		{Name: "m_ninf", Value: math.Inf(-1), Type: MetricGauge},
	}
	var buf bytes.Buffer
	if err := WritePrometheus(&buf, in); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	for _, tok := range []string{"m_nan NaN", "m_pinf +Inf", "m_ninf -Inf"} {
		if !strings.Contains(buf.String(), tok) {
			t.Errorf("output missing %q\n%s", tok, buf.String())
		}
	}

	out, err := ParsePrometheus(&buf)
	if err != nil {
		t.Fatalf("ParsePrometheus: %v", err)
	}
	byName := map[string]float64{}
	for _, s := range out {
		byName[s.Name] = s.Value
	}
	if !math.IsNaN(byName["m_nan"]) {
		t.Errorf("m_nan = %v, want NaN", byName["m_nan"])
	}
	if !math.IsInf(byName["m_pinf"], 1) {
		t.Errorf("m_pinf = %v, want +Inf", byName["m_pinf"])
	}
	if !math.IsInf(byName["m_ninf"], -1) {
		t.Errorf("m_ninf = %v, want -Inf", byName["m_ninf"])
	}
}

// TestPrometheusEscaping round-trips label values containing quotes,
// backslashes, and newlines.
func TestPrometheusEscaping(t *testing.T) {
	in := []Sample{{
		Name:   "log_lines",
		Type:   MetricCounter,
		Labels: map[string]string{"msg": `he said "hi"`, "path": `C:\temp\x`, "multi": "a\nb"},
		Value:  5,
	}}
	var buf bytes.Buffer
	if err := WritePrometheus(&buf, in); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	out, err := ParsePrometheus(&buf)
	if err != nil {
		t.Fatalf("ParsePrometheus: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("parsed %d samples, want 1", len(out))
	}
	for k, want := range in[0].Labels {
		if got := out[0].Labels[k]; got != want {
			t.Errorf("label %q round-tripped as %q, want %q", k, got, want)
		}
	}
}

// TestPrometheusParseErrors asserts malformed input is rejected with a located
// error rather than silently skipped.
func TestPrometheusParseErrors(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"unterminated label block", `metric{a="1" 5`},
		{"unquoted label value", `metric{a=1} 5`},
		{"label missing equals", `metric{abc} 5`},
		{"missing value", `metric_without_value`},
		{"bad value", `metric abc`},
		{"bad timestamp", `metric 1 notanumber`},
		{"too many fields", `metric 1 2 3`},
		{"unterminated label value", `metric{a="1} 5`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParsePrometheus(strings.NewReader(c.text))
			if err == nil {
				t.Fatalf("ParsePrometheus(%q) = nil error, want a ParseError", c.text)
			}
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("error type = %T (%v), want *ParseError", err, err)
			}
			if pe.Line != 1 {
				t.Errorf("ParseError.Line = %d, want 1", pe.Line)
			}
		})
	}
}

// TestPrometheusParseIgnoresComments confirms free-form comments and blank lines
// are skipped while HELP/TYPE metadata is retained.
func TestPrometheusParseIgnoresComments(t *testing.T) {
	text := "" +
		"# this is a plain comment\n" +
		"\n" +
		"# HELP my_metric Some help text.\n" +
		"# TYPE my_metric gauge\n" +
		"my_metric{a=\"1\"} 42\n"

	out, err := ParsePrometheus(strings.NewReader(text))
	if err != nil {
		t.Fatalf("ParsePrometheus: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("parsed %d samples, want 1", len(out))
	}
	if out[0].Help != "Some help text." {
		t.Errorf("help = %q", out[0].Help)
	}
	if out[0].Type != MetricGauge {
		t.Errorf("type = %q, want gauge", out[0].Type)
	}
	if out[0].Value != 42 {
		t.Errorf("value = %v, want 42", out[0].Value)
	}
}

// TestAggregateGrouping validates grouping and every aggregation function
// against hand-computed expected values.
func TestAggregateGrouping(t *testing.T) {
	samples := []Sample{
		{Name: "latency", Labels: map[string]string{"svc": "api"}, Value: 10},
		{Name: "latency", Labels: map[string]string{"svc": "api"}, Value: 20},
		{Name: "latency", Labels: map[string]string{"svc": "api"}, Value: 30},
		{Name: "latency", Labels: map[string]string{"svc": "db"}, Value: 100},
		{Name: "latency", Labels: map[string]string{"svc": "db"}, Value: 200},
	}

	groups := Aggregate(samples, []string{"svc"}, AggCount, AggSum, AggAvg, AggMin, AggMax)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}

	// Deterministic order: api before db.
	if groups[0].GroupLabels["svc"] != "api" || groups[1].GroupLabels["svc"] != "db" {
		t.Fatalf("groups not sorted by label value: %q, %q",
			groups[0].GroupLabels["svc"], groups[1].GroupLabels["svc"])
	}

	api := groups[0]
	for f, want := range map[AggFunc]float64{
		AggCount: 3, AggSum: 60, AggAvg: 20, AggMin: 10, AggMax: 30,
	} {
		if got := api.Results[f]; got != want {
			t.Errorf("api %s = %v, want %v", f, got, want)
		}
	}

	db := groups[1]
	if db.Count != 2 || db.Results[AggSum] != 300 || db.Results[AggAvg] != 150 {
		t.Errorf("db group = %+v", db)
	}
}

// TestAggregateMissingLabel confirms samples lacking the group label fall into
// the empty-value bucket rather than being dropped.
func TestAggregateMissingLabel(t *testing.T) {
	samples := []Sample{
		{Name: "m", Labels: map[string]string{"zone": "a"}, Value: 1},
		{Name: "m", Value: 5}, // no labels at all
	}
	groups := Aggregate(samples, []string{"zone"}, AggCount, AggSum)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	var total float64
	for _, g := range groups {
		total += g.Results[AggSum]
	}
	if total != 6 {
		t.Errorf("total across groups = %v, want 6 (no samples dropped)", total)
	}
}

// TestAggregateLabelCollision guards the length-prefixed grouping key against
// label values that would collide under naive concatenation.
func TestAggregateLabelCollision(t *testing.T) {
	samples := []Sample{
		{Name: "m", Labels: map[string]string{"a": "x", "b": "yz"}, Value: 1},
		{Name: "m", Labels: map[string]string{"a": "xy", "b": "z"}, Value: 2},
	}
	groups := Aggregate(samples, []string{"a", "b"}, AggCount)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (label values collided)", len(groups))
	}
}

// TestAggregateNaNHandling verifies NaN samples are excluded from statistics.
func TestAggregateNaNHandling(t *testing.T) {
	samples := []Sample{
		{Name: "m", Labels: map[string]string{"k": "v"}, Value: 10},
		{Name: "m", Labels: map[string]string{"k": "v"}, Value: math.NaN()},
		{Name: "m", Labels: map[string]string{"k": "v"}, Value: 20},
	}
	g := Aggregate(samples, []string{"k"}, AggCount, AggSum, AggAvg)[0]
	if g.Results[AggCount] != 2 {
		t.Errorf("count = %v, want 2 (NaN excluded)", g.Results[AggCount])
	}
	if g.Results[AggSum] != 30 {
		t.Errorf("sum = %v, want 30", g.Results[AggSum])
	}
	if g.Results[AggAvg] != 15 {
		t.Errorf("avg = %v, want 15", g.Results[AggAvg])
	}

	// All-NaN group must not produce NaN statistics.
	allNaN := []Sample{{Name: "m", Value: math.NaN()}}
	gn := Aggregate(allNaN, nil, AggCount, AggAvg)[0]
	if gn.Results[AggCount] != 0 {
		t.Errorf("all-NaN count = %v, want 0", gn.Results[AggCount])
	}
	if math.IsNaN(gn.Results[AggAvg]) {
		t.Error("all-NaN avg is NaN; want a defined 0")
	}
}

// TestQuantileExact pins p95/p99 against hand-computed interpolation on 1..100.
func TestQuantileExact(t *testing.T) {
	sorted := make([]float64, 100)
	for i := range sorted {
		sorted[i] = float64(i + 1)
	}

	// pos = q*(n-1); for n=100, q=.95 -> 94.05 -> between ranks 94 and 95
	// (values 95 and 96) => 95 + 0.05 = 95.05.
	if got, want := Quantile(sorted, 0.95), 95.05; math.Abs(got-want) > 1e-9 {
		t.Errorf("Quantile(0.95) = %v, want %v", got, want)
	}
	if got, want := Quantile(sorted, 0.99), 99.01; math.Abs(got-want) > 1e-9 {
		t.Errorf("Quantile(0.99) = %v, want %v", got, want)
	}
	if got := Quantile(sorted, 0.5); math.Abs(got-50.5) > 1e-9 {
		t.Errorf("Quantile(0.50) = %v, want 50.5", got)
	}

	// Edges and degenerate inputs.
	if got := Quantile(nil, 0.5); got != 0 {
		t.Errorf("Quantile(nil) = %v, want 0", got)
	}
	if got := Quantile([]float64{7}, 0.99); got != 7 {
		t.Errorf("single-element quantile = %v, want 7", got)
	}
	if got := Quantile(sorted, 0); got != 1 {
		t.Errorf("Quantile(0) = %v, want 1", got)
	}
	if got := Quantile(sorted, 1); got != 100 {
		t.Errorf("Quantile(1) = %v, want 100", got)
	}
	if got := Quantile(sorted, 2); got != 100 {
		t.Errorf("Quantile(q>1) = %v, want clamped to 100", got)
	}
}

// TestAggregatePercentiles checks p95/p99 flow through the aggregation pipeline.
func TestAggregatePercentiles(t *testing.T) {
	samples := make([]Sample, 0, 100)
	for i := 1; i <= 100; i++ {
		samples = append(samples, Sample{
			Name:   "req_ms",
			Labels: map[string]string{"svc": "api"},
			Value:  float64(i),
		})
	}
	g := Aggregate(samples, []string{"svc"}, AggP95, AggP99)[0]
	if got, want := g.Results[AggP95], 95.05; math.Abs(got-want) > 1e-9 {
		t.Errorf("p95 = %v, want %v", got, want)
	}
	if got, want := g.Results[AggP99], 99.01; math.Abs(got-want) > 1e-9 {
		t.Errorf("p99 = %v, want %v", got, want)
	}
	if PercentileMethod == "" {
		t.Error("PercentileMethod must document how percentiles are derived")
	}
}

// TestToSamplesReExport confirms aggregated results can be re-exported and
// re-parsed through the Prometheus path.
func TestToSamplesReExport(t *testing.T) {
	samples := []Sample{
		{Name: "latency", Labels: map[string]string{"svc": "api"}, Value: 10},
		{Name: "latency", Labels: map[string]string{"svc": "api"}, Value: 30},
	}
	groups := Aggregate(samples, []string{"svc"}, AggAvg, AggP95)
	out := ToSamples(groups, time.UnixMilli(1_700_000_000_000).UTC())
	if len(out) != 2 {
		t.Fatalf("ToSamples returned %d samples, want 2", len(out))
	}

	var buf bytes.Buffer
	if err := WritePrometheus(&buf, out); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	reparsed, err := ParsePrometheus(&buf)
	if err != nil {
		t.Fatalf("ParsePrometheus: %v", err)
	}
	if len(reparsed) != 2 {
		t.Fatalf("re-parsed %d samples, want 2", len(reparsed))
	}

	found := false
	for _, s := range reparsed {
		if s.Name == "latency:avg" {
			found = true
			if s.Value != 20 {
				t.Errorf("latency:avg = %v, want 20", s.Value)
			}
			if s.Labels["svc"] != "api" {
				t.Errorf("group label lost: %+v", s.Labels)
			}
		}
		if s.Name == "latency:p95" && s.Labels["quantile_method"] != "exact" {
			t.Errorf("p95 sample missing quantile_method=exact: %+v", s.Labels)
		}
	}
	if !found {
		t.Error("latency:avg missing after re-export")
	}
}

// TestScrapeMeasuresDuration verifies Scrape reports a real elapsed time. It
// asserts only that the measurement is plausible; no latency target is claimed
// here, since a target must come from a benchmark on real hardware.
func TestScrapeMeasuresDuration(t *testing.T) {
	c := CollectorFunc{
		CollectorName: "slow",
		Fn: func(context.Context) ([]Sample, error) {
			time.Sleep(5 * time.Millisecond)
			return []Sample{{Name: "x", Value: 1}}, nil
		},
	}
	res := Scrape(context.Background(), c)
	if res.Err != nil {
		t.Fatalf("Scrape error: %v", res.Err)
	}
	if res.Duration < 5*time.Millisecond {
		t.Errorf("Duration = %v, want >= 5ms", res.Duration)
	}
	if len(res.Samples) != 1 {
		t.Errorf("got %d samples, want 1", len(res.Samples))
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func benchSamples(n int) []Sample {
	svcs := []string{"api", "db", "cache", "queue", "edge"}
	out := make([]Sample, n)
	for i := range out {
		out[i] = Sample{
			Name: "request_duration_ms",
			Type: MetricGauge,
			Labels: map[string]string{
				"svc":    svcs[i%len(svcs)],
				"method": []string{"GET", "POST"}[i%2],
			},
			Value: float64(i % 500),
		}
	}
	return out
}

// BenchmarkAggregate measures aggregation throughput over 10k samples with
// p95/p99 enabled (the sorting path).
func BenchmarkAggregate(b *testing.B) {
	samples := benchSamples(10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Aggregate(samples, []string{"svc", "method"}, AggCount, AggSum, AggAvg, AggMax, AggP95, AggP99)
	}
}

// BenchmarkWritePrometheus measures exposition-format export throughput.
func BenchmarkWritePrometheus(b *testing.B) {
	samples := benchSamples(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := WritePrometheus(&buf, samples); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParsePrometheus measures exposition-format parse throughput.
func BenchmarkParsePrometheus(b *testing.B) {
	var buf bytes.Buffer
	if err := WritePrometheus(&buf, benchSamples(5000)); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParsePrometheus(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMultiCollectorScrape measures fan-out cost across 10 collectors.
func BenchmarkMultiCollectorScrape(b *testing.B) {
	mc := NewMultiCollector(map[string]string{"cluster": "bench"})
	for i := 0; i < 10; i++ {
		mc.Register(staticCollector(fmt.Sprintf("src%d", i), benchSamples(100)...))
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mc.Collect(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
