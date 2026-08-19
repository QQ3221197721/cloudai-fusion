package observability

import (
	"math"
	"testing"
	"time"
)

// ============================================================================
// M18 Tests — Tracing Optimizer
// ============================================================================

func TestOptimizerTailSamplingError(t *testing.T) {
	cfg := TraceOptimizerConfig{BaseSampleRate: 0} // drop all normal traces
	o := NewTraceOptimizer(cfg)
	spans := []*Span{
		{SpanID: "1", TraceID: "t1", ParentID: "", Name: "run", StartTime: time.Now(), Status: StatusOk},
		{SpanID: "2", TraceID: "t1", ParentID: "1", Name: "step 1", StartTime: time.Now(), Status: StatusOk},
		{SpanID: "3", TraceID: "t1", ParentID: "2", Name: "train", StartTime: time.Now(), Duration: 1*time.Second, Status: StatusError, ErrorMsg: "OOM"},
	}
	res := o.Optimize("t1", spans)
	if !res.Retained {
		t.Fatal("trace with error span should be retained")
	}
	if res.Policy != PolicyError {
		t.Errorf("expected policy %s, got %s", PolicyError, res.Policy)
	}
	if len(res.OptimizedSpans) != 3 {
		t.Fatalf("expected 3 optimized spans, got %d", len(res.OptimizedSpans))
	}
}

func TestOptimizerTailSamplingLatency(t *testing.T) {
	cfg := TraceOptimizerConfig{BaseSampleRate: 0, LatencyThreshold: 500 * time.Millisecond}
	o := NewTraceOptimizer(cfg)
	start := time.Now()
	spans := []*Span{
		{SpanID: "1", TraceID: "t2", ParentID: "", Name: "run", StartTime: start, EndTime: start.Add(2 * time.Second), Status: StatusOk},
		{SpanID: "2", TraceID: "t2", ParentID: "1", Name: "slow step", StartTime: start, EndTime: start.Add(2 * time.Second), Status: StatusOk},
	}
	res := o.Optimize("t2", spans)
	if !res.Retained {
		t.Fatal("long trace should be retained")
	}
	if res.Policy != PolicyLatency {
		t.Errorf("expected latency retention, got %s", res.Policy)
	}
}

func TestOptimizerTailSamplingImportantAttr(t *testing.T) {
	cfg := TraceOptimizerConfig{
		BaseSampleRate:         0,
		ImportantAttributes:    []string{"diverged", "gpu.oom", "checkpoint.corrupt"},
		LatencyThreshold:       0,
		AggregateThreshold:     100, // disable aggregation
	}
	o := NewTraceOptimizer(cfg)
	spans := []*Span{
		{SpanID: "1", TraceID: "t3", ParentID: "", Name: "run", Status: StatusOk, Attributes: map[string]interface{}{"diverged": true}},
		{SpanID: "2", TraceID: "t3", ParentID: "1", Name: "step", Status: StatusOk},
	}
	res := o.Optimize("t3", spans)
	if !res.Retained || res.Policy != PolicyImportantAttr {
		t.Errorf("trace with important attribute should be retained as important_attr, got retained=%v policy=%s", res.Retained, res.Policy)
	}
}

func TestOptimizerDropNormalTraces(t *testing.T) {
	cfg := TraceOptimizerConfig{
		BaseSampleRate:   0,
		LatencyThreshold: 0,
	}
	o := NewTraceOptimizer(cfg)
	spans := []*Span{
		{SpanID: "1", TraceID: "drop_all_trace", ParentID: "", Name: "normal", Status: StatusOk},
	}
	res := o.Optimize("drop_all_trace", spans)
	if res.Retained {
		t.Error("normal trace should be dropped when BaseSampleRate=0")
	}
}

func TestOptimizerProbabilisticKeep(t *testing.T) {
	cfg := TraceOptimizerConfig{BaseSampleRate: 1.0} // keep everything
	o := NewTraceOptimizer(cfg)
	spans := []*Span{{SpanID: "1", TraceID: "t_keep", ParentID: "", Name: "always", Status: StatusOk}}
	res := o.Optimize("t_keep", spans)
	if !res.Retained || res.Policy != PolicyProbabilistic {
		t.Errorf("kept trace should have probabilistic policy, got retained=%v policy=%s", res.Retained, res.Policy)
	}
}

func TestOptimizerEmptyTrace(t *testing.T) {
	o := NewTraceOptimizer(TraceOptimizerConfig{})
	res := o.Optimize("", nil)
	if res.Retained {
		t.Error("empty trace should not be retained")
	}
	if res.Policy != PolicyEmpty {
		t.Errorf("expected empty policy, got %s", res.Policy)
	}
}

func TestOptimizerAggregation(t *testing.T) {
	cfg := TraceOptimizerConfig{
		BaseSampleRate:     1.0,
		AggregateThreshold: 3, // collapse groups >= 3 identical OK spans
	}
	o := NewTraceOptimizer(cfg)
	spans := []*Span{
		{SpanID: "a", TraceID: "agg_t1", ParentID: "root", Name: "epoch_step", Status: StatusOk, Duration: 100 * time.Millisecond},
		{SpanID: "b", TraceID: "agg_t1", ParentID: "root", Name: "epoch_step", Status: StatusOk, Duration: 105 * time.Millisecond},
		{SpanID: "c", TraceID: "agg_t1", ParentID: "root", Name: "epoch_step", Status: StatusOk, Duration: 95 * time.Millisecond},
		{SpanID: "err", TraceID: "agg_t1", ParentID: "root", Name: "final_check", Status: StatusError, ErrorMsg: "validation fail"},
	}
	res := o.Optimize("agg_t1", spans)
	if !res.Retained {
		t.Fatal("trace should be kept")
	}
	// Expect 2 output spans: one aggregate + the error span
	if len(res.OptimizedSpans) != 2 {
		t.Fatalf("expected 2 optimized spans (aggregate + error), got %d: %v", len(res.OptimizedSpans), res.OptimizedSpans)
	}
	if res.AggregatedGroups != 1 || res.CollapsedSpans != 2 {
		t.Errorf("expected 1 group collapsed by 2 spans, got aggGroups=%d collapsed=%d", res.AggregatedGroups, res.CollapsedSpans)
	}
}

func TestOptimizerNeverCollapseErrors(t *testing.T) {
	// Multiple sibling error spans must NOT be collapsed even if they are structurally identical.
	cfg := TraceOptimizerConfig{BaseSampleRate: 1.0, AggregateThreshold: 3}
	o := NewTraceOptimizer(cfg)
	spans := []*Span{
		{SpanID: "e1", TraceID: "t_err_agg", ParentID: "p", Name: "failing_op", Status: StatusError, ErrorMsg: "fail 1"},
		{SpanID: "e2", TraceID: "t_err_agg", ParentID: "p", Name: "failing_op", Status: StatusError, ErrorMsg: "fail 2"},
		{SpanID: "e3", TraceID: "t_err_agg", ParentID: "p", Name: "failing_op", Status: StatusError, ErrorMsg: "fail 3"},
	}
	res := o.Optimize("t_err_agg", spans)
	if len(res.OptimizedSpans) != 3 {
		t.Errorf("error spans must never be collapsed; expected 3 spans, got %d", len(res.OptimizedSpans))
	}
}

func TestOptimizerStatsConsistency(t *testing.T) {
	cfg := TraceOptimizerConfig{BaseSampleRate: 1.0}
	o := NewTraceOptimizer(cfg)
	for i := 0; i < 10; i++ {
		traces := make(map[string][]*Span)
		traces["trace_"+string(rune('0'+i))] = []*Span{{SpanID: string(rune('0' + i)), TraceID: "trace_" + string(rune('0' + i)), Name: "span", Status: StatusOk}}
		o.OptimizeBatch(traces)
	}
	s := o.Stats()
	if s.TracesSeen != 10 || s.TracesKept != 10 || s.TracesDropped != 0 {
		t.Errorf("stats mismatch: seen=%d kept=%d dropped=%d", s.TracesSeen, s.TracesKept, s.TracesDropped)
	}
}

func TestComputeAdjustedRate(t *testing.T) {
	tests := []struct {
		name string
		current, observed, target, min, max float64
		want float64
	}{
		{"overshoot", 0.1, 150, 100, 0.01, 1.0, 0.1 * 0.8},
		{"undershoot", 0.1, 50, 100, 0.01, 1.0, 0.1 * 1.25},
		{"ontarget", 0.1, 100, 100, 0.01, 1.0, 0.1},
		{"minbound", 0.01, 150, 100, 0.01, 1.0, 0.01},
		{"maxbound", 1.0, 50, 100, 0.01, 1.0, 1.0},
		{"zeroobserved", 0.1, 0, 100, 0.01, 1.0, 0.1 * 1.25},
	}
	for _, tc := range tests {
		got := computeAdjustedRate(tc.current, tc.observed, tc.target, tc.min, tc.max)
		diff := math.Abs(got - tc.want)
		if diff > 1e-9 {
			t.Errorf("test %s: expected %.4f, got %.4f", tc.name, tc.want, got)
		}
	}
}

// Benchmark tail-sampling decisions without aggregation cost.
func BenchmarkOptimizeNoAgg(b *testing.B) {
	baseSpans := []*Span{
		{SpanID: "1", TraceID: "base", ParentID: "", Name: "init", Status: StatusOk},
		{SpanID: "2", TraceID: "base", ParentID: "1", Name: "train", Status: StatusOk, Duration: 10 * time.Millisecond},
	}
	cfg := TraceOptimizerConfig{BaseSampleRate: 0.1, AggregateThreshold: 1000} // disable aggregation
	o := NewTraceOptimizer(cfg)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = o.Optimize(string(rune('A'+(i%26))), append([]*Span{}, baseSpans...))
	}
}

// Benchmark aggregation denoising with many sibling spans.
func BenchmarkAggregateManySpans(b *testing.B) {
	parentID := "root"
	spans := make([]*Span, 100)
	for i := 0; i < 100; i++ {
		spans[i] = &Span{SpanID: string(rune('0' + i)), TraceID: "agg", ParentID: parentID, Name: "epoch_step", Status: StatusOk, Duration: time.Duration(i+1) * time.Millisecond}
	}
	cfg := TraceOptimizerConfig{BaseSampleRate: 1.0, AggregateThreshold: 10}
	o := NewTraceOptimizer(cfg)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = o.Optimize("agg", append([]*Span{}, spans...))
	}
}

// Benchmark full pipeline: tail sampling + aggregation across multiple traces.
func BenchmarkOptimizeBatch(b *testing.B) {
	makeTrace := func(tid string, n int) []*Span {
		spans := make([]*Span, n)
		for i := 0; i < n; i++ {
			spans[i] = &Span{SpanID: string(rune('0'+i)), TraceID: tid, ParentID: "root", Name: "rep", Status: StatusOk}
		}
		return spans
	}
	traces := make(map[string][]*Span)
	for i := 0; i < 100; i++ {
		traces["t"+string(rune('0'+i))] = makeTrace(string(rune('0'+i)), 20)
	}
	cfg := TraceOptimizerConfig{BaseSampleRate: 0.2, AggregateThreshold: 5}
	o := NewTraceOptimizer(cfg)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = o.OptimizeBatch(traces)
	}
}
