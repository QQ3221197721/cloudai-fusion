package tsdb

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Metric — LabelKey
// ============================================================================

func TestMetric_LabelKey(t *testing.T) {
	tests := []struct {
		name   string
		metric Metric
		want   string
	}{
		{
			name: "single label",
			metric: Metric{
				Name:   "cpu_usage",
				Labels: map[string]string{"host": "node1"},
			},
			want: "cpu_usage{host=node1}",
		},
		{
			name: "multiple labels sorted",
			metric: Metric{
				Name:   "gpu_temp",
				Labels: map[string]string{"gpu": "0", "cluster": "prod"},
			},
			want: "gpu_temp{cluster=prod,gpu=0}",
		},
		{
			name: "no labels",
			metric: Metric{
				Name:   "counter",
				Labels: map[string]string{},
			},
			want: "counter{}",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.metric.LabelKey()
			if got != tc.want {
				t.Errorf("LabelKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ============================================================================
// DefaultConfig
// ============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", cfg.Backend)
	}
	if cfg.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d, want 30", cfg.RetentionDays)
	}
	if cfg.MaxSeries != 100000 {
		t.Errorf("MaxSeries = %d, want 100000", cfg.MaxSeries)
	}
}

// ============================================================================
// memoryTSDB — Write & Query
// ============================================================================

func newTestDB() TSDB {
	return NewMemoryTSDB(Config{MaxSeries: 1000, RetentionDays: 7}, logrus.StandardLogger())
}

func TestMemoryTSDB_WriteAndQuery(t *testing.T) {
	db := newTestDB()
	defer db.Close()
	ctx := context.Background()

	now := time.Now()
	metrics := []Metric{{
		Name:   MetricGPUUtilization,
		Labels: map[string]string{"cluster": "prod", "gpu": "0"},
		Samples: []Sample{
			{Timestamp: now.Add(-2 * time.Minute), Value: 75.0},
			{Timestamp: now.Add(-1 * time.Minute), Value: 82.0},
			{Timestamp: now, Value: 90.0},
		},
	}}

	if err := db.Write(ctx, metrics); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, err := db.Query(ctx, Query{
		MetricName: MetricGPUUtilization,
		LabelMatchers: map[string]string{"cluster": "prod"},
		Start: now.Add(-3 * time.Minute),
		End:   now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Series) != 1 {
		t.Fatalf("Series count = %d, want 1", len(result.Series))
	}
	if len(result.Series[0].Samples) != 3 {
		t.Errorf("Samples count = %d, want 3", len(result.Series[0].Samples))
	}
	if result.Stats.SeriesCount != 1 {
		t.Errorf("SeriesCount = %d, want 1", result.Stats.SeriesCount)
	}
}

func TestMemoryTSDB_Query_TimeFilter(t *testing.T) {
	db := newTestDB()
	defer db.Close()
	ctx := context.Background()

	now := time.Now()
	db.Write(ctx, []Metric{{
		Name:   "test_metric",
		Labels: map[string]string{},
		Samples: []Sample{
			{Timestamp: now.Add(-10 * time.Minute), Value: 1},
			{Timestamp: now.Add(-5 * time.Minute), Value: 2},
			{Timestamp: now, Value: 3},
		},
	}})

	result, _ := db.Query(ctx, Query{
		MetricName: "test_metric",
		Start:      now.Add(-6 * time.Minute),
		End:        now.Add(-4 * time.Minute),
	})

	if len(result.Series) != 1 {
		t.Fatalf("Series count = %d, want 1", len(result.Series))
	}
	if len(result.Series[0].Samples) != 1 {
		t.Errorf("filtered samples = %d, want 1", len(result.Series[0].Samples))
	}
}

func TestMemoryTSDB_Query_LabelFilter(t *testing.T) {
	db := newTestDB()
	defer db.Close()
	ctx := context.Background()

	now := time.Now()
	db.Write(ctx, []Metric{
		{Name: "cpu", Labels: map[string]string{"host": "a"}, Samples: []Sample{{Timestamp: now, Value: 10}}},
		{Name: "cpu", Labels: map[string]string{"host": "b"}, Samples: []Sample{{Timestamp: now, Value: 20}}},
	})

	result, _ := db.Query(ctx, Query{
		MetricName:    "cpu",
		LabelMatchers: map[string]string{"host": "b"},
		Start:         now.Add(-time.Minute),
		End:           now.Add(time.Minute),
	})

	if len(result.Series) != 1 {
		t.Fatalf("Series count = %d, want 1", len(result.Series))
	}
	if result.Series[0].Samples[0].Value != 20 {
		t.Errorf("Value = %f, want 20", result.Series[0].Samples[0].Value)
	}
}

func TestMemoryTSDB_Query_NoMatch(t *testing.T) {
	db := newTestDB()
	defer db.Close()

	result, err := db.Query(context.Background(), Query{
		MetricName: "nonexistent",
		Start:      time.Now().Add(-time.Hour),
		End:        time.Now(),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Series) != 0 {
		t.Errorf("Series count = %d, want 0", len(result.Series))
	}
}

// ============================================================================
// QueryInstant
// ============================================================================

func TestMemoryTSDB_QueryInstant(t *testing.T) {
	db := newTestDB()
	defer db.Close()
	ctx := context.Background()

	now := time.Now()
	db.Write(ctx, []Metric{{
		Name:   "temp",
		Labels: map[string]string{"gpu": "0"},
		Samples: []Sample{
			{Timestamp: now.Add(-time.Minute), Value: 60},
			{Timestamp: now, Value: 72},
		},
	}})

	results, err := db.QueryInstant(ctx, "temp", map[string]string{"gpu": "0"})
	if err != nil {
		t.Fatalf("QueryInstant: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	// Should return last sample only
	if len(results[0].Samples) != 1 {
		t.Errorf("samples = %d, want 1", len(results[0].Samples))
	}
	if results[0].Samples[0].Value != 72 {
		t.Errorf("Value = %f, want 72", results[0].Samples[0].Value)
	}
}

func TestMemoryTSDB_QueryInstant_NoMatch(t *testing.T) {
	db := newTestDB()
	defer db.Close()

	results, err := db.QueryInstant(context.Background(), "nothing", nil)
	if err != nil {
		t.Fatalf("QueryInstant: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

// ============================================================================
// DeleteSeries
// ============================================================================

func TestMemoryTSDB_DeleteSeries(t *testing.T) {
	db := newTestDB()
	defer db.Close()
	ctx := context.Background()

	now := time.Now()
	db.Write(ctx, []Metric{{
		Name:   "del_test",
		Labels: map[string]string{},
		Samples: []Sample{
			{Timestamp: now.Add(-2 * time.Hour), Value: 1},
			{Timestamp: now.Add(-1 * time.Hour), Value: 2},
			{Timestamp: now, Value: 3},
		},
	}})

	// Delete middle sample
	err := db.DeleteSeries(ctx, "del_test", map[string]string{},
		now.Add(-90*time.Minute), now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}

	result, _ := db.Query(ctx, Query{
		MetricName: "del_test",
		Start:      now.Add(-3 * time.Hour),
		End:        now.Add(time.Hour),
	})
	if len(result.Series) != 1 {
		t.Fatalf("Series = %d, want 1", len(result.Series))
	}
	if len(result.Series[0].Samples) != 2 {
		t.Errorf("Samples after delete = %d, want 2", len(result.Series[0].Samples))
	}
}

func TestMemoryTSDB_DeleteSeries_AllSamples(t *testing.T) {
	db := newTestDB()
	defer db.Close()
	ctx := context.Background()

	now := time.Now()
	db.Write(ctx, []Metric{{
		Name:    "del_all",
		Labels:  map[string]string{},
		Samples: []Sample{{Timestamp: now, Value: 1}},
	}})

	db.DeleteSeries(ctx, "del_all", map[string]string{},
		now.Add(-time.Hour), now.Add(time.Hour))

	result, _ := db.Query(ctx, Query{
		MetricName: "del_all",
		Start:      now.Add(-2 * time.Hour),
		End:        now.Add(2 * time.Hour),
	})
	if len(result.Series) != 0 {
		t.Errorf("Series = %d, want 0 (all deleted)", len(result.Series))
	}
}

// ============================================================================
// Write — append to existing series
// ============================================================================

func TestMemoryTSDB_Write_Append(t *testing.T) {
	db := newTestDB()
	defer db.Close()
	ctx := context.Background()

	now := time.Now()
	db.Write(ctx, []Metric{{
		Name:    "append_test",
		Labels:  map[string]string{"host": "a"},
		Samples: []Sample{{Timestamp: now, Value: 1}},
	}})
	db.Write(ctx, []Metric{{
		Name:    "append_test",
		Labels:  map[string]string{"host": "a"},
		Samples: []Sample{{Timestamp: now.Add(time.Second), Value: 2}},
	}})

	result, _ := db.Query(ctx, Query{
		MetricName: "append_test",
		Start:      now.Add(-time.Minute),
		End:        now.Add(time.Minute),
	})
	if len(result.Series) != 1 {
		t.Fatalf("Series = %d, want 1", len(result.Series))
	}
	if len(result.Series[0].Samples) != 2 {
		t.Errorf("Samples = %d, want 2", len(result.Series[0].Samples))
	}
}

// ============================================================================
// Aggregation
// ============================================================================

func TestMemoryTSDB_Query_Aggregation(t *testing.T) {
	db := newTestDB()
	defer db.Close()
	ctx := context.Background()

	now := time.Now()
	db.Write(ctx, []Metric{
		{Name: "agg_test", Labels: map[string]string{"host": "a"}, Samples: []Sample{{Timestamp: now, Value: 10}}},
		{Name: "agg_test", Labels: map[string]string{"host": "b"}, Samples: []Sample{{Timestamp: now, Value: 20}}},
	})

	tests := []struct {
		agg  string
		want float64
	}{
		{"sum", 30},
		{"avg", 15},
		{"max", 20},
		{"min", 10},
		{"count", 2},
	}

	for _, tc := range tests {
		t.Run(tc.agg, func(t *testing.T) {
			result, err := db.Query(ctx, Query{
				MetricName:  "agg_test",
				Start:       now.Add(-time.Minute),
				End:         now.Add(time.Minute),
				Aggregation: tc.agg,
			})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(result.Series) != 1 {
				t.Fatalf("Series = %d, want 1", len(result.Series))
			}
			if result.Series[0].Samples[0].Value != tc.want {
				t.Errorf("%s = %f, want %f", tc.agg, result.Series[0].Samples[0].Value, tc.want)
			}
		})
	}
}

// ============================================================================
// Healthy / Close
// ============================================================================

func TestMemoryTSDB_Healthy(t *testing.T) {
	db := newTestDB()
	defer db.Close()
	if !db.Healthy(context.Background()) {
		t.Error("memory TSDB should be healthy")
	}
}

func TestMemoryTSDB_Close(t *testing.T) {
	db := newTestDB()
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// ============================================================================
// VictoriaMetrics TSDB (fallback)
// ============================================================================

func TestVMTSDB_Fallback(t *testing.T) {
	cfg := Config{Backend: "victoriametrics", VMEndpoint: "http://localhost:8428", MaxSeries: 100}
	db := NewVictoriaMetricsTSDB(cfg, logrus.StandardLogger())
	defer db.Close()
	ctx := context.Background()

	now := time.Now()
	err := db.Write(ctx, []Metric{{
		Name:    "vm_test",
		Labels:  map[string]string{},
		Samples: []Sample{{Timestamp: now, Value: 42}},
	}})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, err := db.Query(ctx, Query{
		MetricName: "vm_test",
		Start:      now.Add(-time.Minute),
		End:        now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Series) != 1 {
		t.Errorf("Series = %d, want 1", len(result.Series))
	}

	if !db.Healthy(ctx) {
		t.Error("VM TSDB should be healthy in fallback mode")
	}
}

// ============================================================================
// Factory
// ============================================================================

func TestNew_Memory(t *testing.T) {
	db := New(Config{Backend: "memory", MaxSeries: 10}, logrus.StandardLogger())
	defer db.Close()
	if !db.Healthy(context.Background()) {
		t.Error("should be healthy")
	}
}

func TestNew_VM(t *testing.T) {
	db := New(Config{Backend: "victoriametrics", MaxSeries: 10}, logrus.StandardLogger())
	defer db.Close()
	if !db.Healthy(context.Background()) {
		t.Error("should be healthy")
	}
}

func TestNew_Default(t *testing.T) {
	db := New(Config{MaxSeries: 10}, logrus.StandardLogger())
	defer db.Close()
	if !db.Healthy(context.Background()) {
		t.Error("should be healthy")
	}
}

// ============================================================================
// matchLabels
// ============================================================================

func TestMatchLabels(t *testing.T) {
	tests := []struct {
		name     string
		actual   map[string]string
		matchers map[string]string
		want     bool
	}{
		{"empty matchers", map[string]string{"a": "1"}, map[string]string{}, true},
		{"exact match", map[string]string{"a": "1"}, map[string]string{"a": "1"}, true},
		{"mismatch", map[string]string{"a": "1"}, map[string]string{"a": "2"}, false},
		{"missing key", map[string]string{}, map[string]string{"a": "1"}, false},
		{"nil matchers", map[string]string{"a": "1"}, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchLabels(tc.actual, tc.matchers)
			if got != tc.want {
				t.Errorf("matchLabels = %v, want %v", got, tc.want)
			}
		})
	}
}

// ============================================================================
// Well-known Metric Constants
// ============================================================================

func TestMetricConstants(t *testing.T) {
	constants := []string{
		MetricGPUUtilization, MetricGPUMemoryUsed, MetricGPUTemperature,
		MetricNodeCPUUsage, MetricNodeMemoryUsage,
		MetricWorkloadDuration, MetricClusterNodeCount,
		MetricAPIRequestTotal, MetricAPIRequestDuration,
	}
	for _, c := range constants {
		if c == "" {
			t.Error("metric constant should not be empty")
		}
	}
}
