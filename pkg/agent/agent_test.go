package agent

import (
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Prometheus Text Parser Tests
// ============================================================================

func TestParsePrometheusText_SimpleMetric(t *testing.T) {
	input := strings.NewReader(`node_load1 3.14
node_load5 2.71`)
	metrics, err := parsePrometheusText(input)
	if err != nil {
		t.Fatalf("parsePrometheusText: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if metrics[0].Name != "node_load1" || metrics[0].Value != 3.14 {
		t.Errorf("metric[0] = %+v", metrics[0])
	}
}

func TestParsePrometheusText_WithLabels(t *testing.T) {
	input := strings.NewReader(`DCGM_FI_DEV_GPU_UTIL{gpu="0",UUID="GPU-abc"} 75.5`)
	metrics, err := parsePrometheusText(input)
	if err != nil {
		t.Fatalf("parsePrometheusText: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	m := metrics[0]
	if m.Name != "DCGM_FI_DEV_GPU_UTIL" {
		t.Errorf("name = %q", m.Name)
	}
	if m.Labels["gpu"] != "0" {
		t.Errorf("label gpu = %q", m.Labels["gpu"])
	}
	if m.Labels["UUID"] != "GPU-abc" {
		t.Errorf("label UUID = %q", m.Labels["UUID"])
	}
	if m.Value != 75.5 {
		t.Errorf("value = %f", m.Value)
	}
}

func TestParsePrometheusText_SkipsComments(t *testing.T) {
	input := strings.NewReader(`# HELP node_load1 1m load average
# TYPE node_load1 gauge
node_load1 1.5`)
	metrics, err := parsePrometheusText(input)
	if err != nil {
		t.Fatalf("parsePrometheusText: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric (comments skipped), got %d", len(metrics))
	}
}

func TestParsePrometheusText_SkipsEmptyLines(t *testing.T) {
	input := strings.NewReader(`
node_load1 1.0

node_load5 2.0
`)
	metrics, err := parsePrometheusText(input)
	if err != nil {
		t.Fatalf("parsePrometheusText: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
}

func TestParsePrometheusText_SkipsMalformed(t *testing.T) {
	input := strings.NewReader(`good_metric 42
bad line without value
another_good 99`)
	metrics, err := parsePrometheusText(input)
	if err != nil {
		t.Fatalf("parsePrometheusText: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 valid metrics, got %d", len(metrics))
	}
}

func TestParseMetricLine_Simple(t *testing.T) {
	m, err := parseMetricLine("up 1")
	if err != nil {
		t.Fatalf("parseMetricLine: %v", err)
	}
	if m.Name != "up" || m.Value != 1 {
		t.Errorf("got %+v", m)
	}
	if len(m.Labels) != 0 {
		t.Errorf("simple metric should have no labels")
	}
}

func TestParseMetricLine_WithTimestamp(t *testing.T) {
	m, err := parseMetricLine(`DCGM_FI_DEV_GPU_TEMP{gpu="0"} 65 1709568000`)
	if err != nil {
		t.Fatalf("parseMetricLine: %v", err)
	}
	if m.Value != 65 {
		t.Errorf("value = %f, want 65", m.Value)
	}
}

func TestParseMetricLine_InvalidValue(t *testing.T) {
	_, err := parseMetricLine("metric abc")
	if err == nil {
		t.Error("should fail on non-numeric value")
	}
}

func TestParseMetricLine_TooShort(t *testing.T) {
	_, err := parseMetricLine("onlyname")
	if err == nil {
		t.Error("should fail on single-word line")
	}
}

// ============================================================================
// SplitLabels Tests
// ============================================================================

func TestSplitLabels(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{`gpu="0",UUID="abc"`, 2},
		{`gpu="0"`, 1},
		{`key="value,with,commas"`, 1}, // commas inside quotes
		{`a="1",b="2",c="3"`, 3},
		{``, 0},
	}
	for _, tt := range tests {
		got := splitLabels(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitLabels(%q) = %d items %v, want %d", tt.input, len(got), got, tt.want)
		}
	}
}

// ============================================================================
// Collector Tests
// ============================================================================

func TestNewCollector_Defaults(t *testing.T) {
	c := NewCollector(CollectorConfig{})
	if c.interval != 15*time.Second {
		t.Errorf("default interval = %v, want 15s", c.interval)
	}
	if c.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
}

func TestNewCollector_CustomInterval(t *testing.T) {
	c := NewCollector(CollectorConfig{CollectInterval: 30 * time.Second})
	if c.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", c.interval)
	}
}

func TestCollector_GetLatest_Initially(t *testing.T) {
	c := NewCollector(CollectorConfig{})
	if c.GetLatest() != nil {
		t.Error("initial GetLatest should return nil")
	}
}

func TestCollector_Collect_NoEndpoints(t *testing.T) {
	c := NewCollector(CollectorConfig{})
	// Calling collect with no URLs should produce empty result
	c.collect()
	latest := c.GetLatest()
	if latest == nil {
		t.Fatal("after collect(), GetLatest should not be nil")
	}
	if len(latest.GPUs) != 0 {
		t.Errorf("no DCGM URL → 0 GPUs, got %d", len(latest.GPUs))
	}
	if latest.Node != nil {
		t.Error("no node_exporter URL → nil Node")
	}
	if latest.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestGPUMetrics_Struct(t *testing.T) {
	g := GPUMetrics{
		Index:          0,
		UUID:           "GPU-12345",
		UtilizationPct: 85.5,
		Temperature:    72.0,
	}
	if g.Index != 0 || g.UUID != "GPU-12345" {
		t.Errorf("unexpected GPUMetrics: %+v", g)
	}
}

func TestNodeMetrics_Struct(t *testing.T) {
	n := NodeMetrics{
		CPUUsagePct:      45.0,
		CPUCores:         16,
		MemoryTotalBytes: 64 * 1024 * 1024 * 1024,
		LoadAvg1:         3.5,
	}
	if n.CPUCores != 16 || n.LoadAvg1 != 3.5 {
		t.Errorf("unexpected NodeMetrics: %+v", n)
	}
}
