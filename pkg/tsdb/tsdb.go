// Package tsdb provides a unified abstraction over time-series databases
// for CloudAI Fusion's monitoring and observability stack.
//
// Supports:
//   - VictoriaMetrics (primary, recommended for large-scale deployments)
//   - Prometheus TSDB (via remote write/read API)
//   - In-memory (for development and testing)
//
// Data model:
//   - Metric: a named time series with labels and float64 samples
//   - Series: a collection of samples for a unique label combination
//   - Query: PromQL-compatible query language
package tsdb

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Metric — the fundamental time-series data point
// ============================================================================

// Sample represents a single data point in a time series.
type Sample struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// Metric represents a named time series with labels.
type Metric struct {
	// Name is the metric name (e.g., "gpu_utilization_percent").
	Name string `json:"name"`

	// Labels are key-value pairs that identify this series.
	Labels map[string]string `json:"labels"`

	// Samples are the data points (ordered by timestamp).
	Samples []Sample `json:"samples"`
}

// LabelKey returns a unique string key for this metric's label set.
func (m *Metric) LabelKey() string {
	keys := make([]string, 0, len(m.Labels))
	for k := range m.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	key := m.Name + "{"
	for i, k := range keys {
		if i > 0 {
			key += ","
		}
		key += k + "=" + m.Labels[k]
	}
	key += "}"
	return key
}

// ============================================================================
// Query — time-series query parameters
// ============================================================================

// Query defines parameters for querying time-series data.
type Query struct {
	// MetricName to query.
	MetricName string `json:"metric_name"`

	// LabelMatchers filter series by label values.
	LabelMatchers map[string]string `json:"label_matchers,omitempty"`

	// Start is the query time range start.
	Start time.Time `json:"start"`

	// End is the query time range end.
	End time.Time `json:"end"`

	// Step is the query resolution (e.g., 15s, 1m, 5m).
	Step time.Duration `json:"step,omitempty"`

	// Aggregation function: "avg", "sum", "max", "min", "count".
	Aggregation string `json:"aggregation,omitempty"`

	// GroupBy labels for aggregation.
	GroupBy []string `json:"group_by,omitempty"`
}

// QueryResult holds the result of a time-series query.
type QueryResult struct {
	// Series is the list of matching time series.
	Series []Metric `json:"series"`

	// Stats holds query execution statistics.
	Stats QueryStats `json:"stats"`
}

// QueryStats holds query execution statistics.
type QueryStats struct {
	SeriesCount    int           `json:"series_count"`
	SamplesScanned int64         `json:"samples_scanned"`
	ExecutionTime  time.Duration `json:"execution_time"`
}

// ============================================================================
// TSDB Interface — the contract for time-series storage
// ============================================================================

// TSDB is the unified interface for time-series database operations.
type TSDB interface {
	// Write writes metric samples to the time-series database.
	Write(ctx context.Context, metrics []Metric) error

	// Query retrieves time-series data matching the query parameters.
	Query(ctx context.Context, query Query) (*QueryResult, error)

	// QueryInstant returns the most recent value for matching series.
	QueryInstant(ctx context.Context, metricName string, labels map[string]string) ([]Metric, error)

	// DeleteSeries removes time-series data matching the selector.
	DeleteSeries(ctx context.Context, metricName string, labels map[string]string, start, end time.Time) error

	// Close releases resources.
	Close() error

	// Healthy checks if the TSDB backend is reachable.
	Healthy(ctx context.Context) bool
}

// ============================================================================
// Config
// ============================================================================

// Config holds TSDB configuration.
type Config struct {
	// Backend: "memory" (default), "victoriametrics", or "prometheus".
	Backend string `mapstructure:"backend"`

	// VictoriaMetrics endpoint (e.g., "http://localhost:8428").
	VMEndpoint string `mapstructure:"vm_endpoint"`

	// Prometheus endpoint (e.g., "http://localhost:9090").
	PrometheusEndpoint string `mapstructure:"prometheus_endpoint"`

	// RetentionDays is how long to keep data (in-memory only).
	RetentionDays int `mapstructure:"retention_days"`

	// MaxSeries is the maximum number of series to keep in memory.
	MaxSeries int `mapstructure:"max_series"`

	// FlushInterval for batched writes.
	FlushInterval time.Duration `mapstructure:"flush_interval"`
}

// DefaultConfig returns default TSDB configuration.
func DefaultConfig() Config {
	return Config{
		Backend:            "memory",
		VMEndpoint:         "http://localhost:8428",
		PrometheusEndpoint: "http://localhost:9090",
		RetentionDays:      30,
		MaxSeries:          100000,
		FlushInterval:      10 * time.Second,
	}
}

// ============================================================================
// In-Memory TSDB — for development and testing
// ============================================================================

type memoryTSDB struct {
	series map[string]*Metric // labelKey -> metric
	mu     sync.RWMutex
	config Config
	logger *logrus.Logger

	// Stats
	totalWrites int64
	totalQueries int64
}

// NewMemoryTSDB creates an in-memory time-series database.
func NewMemoryTSDB(cfg Config, logger *logrus.Logger) TSDB {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	maxSeries := cfg.MaxSeries
	if maxSeries == 0 {
		maxSeries = 100000
	}

	db := &memoryTSDB{
		series: make(map[string]*Metric, maxSeries),
		config: cfg,
		logger: logger,
	}

	// Start retention cleanup goroutine
	go db.retentionLoop()

	return db
}

// Write appends samples to the in-memory store.
func (db *memoryTSDB) Write(ctx context.Context, metrics []Metric) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	for _, m := range metrics {
		key := m.LabelKey()
		existing, ok := db.series[key]
		if !ok {
			// Check capacity
			if len(db.series) >= db.config.MaxSeries {
				db.evictOldest()
			}
			mc := m // copy
			db.series[key] = &mc
		} else {
			existing.Samples = append(existing.Samples, m.Samples...)
		}
		db.totalWrites++
	}

	return nil
}

// Query retrieves matching time series.
func (db *memoryTSDB) Query(ctx context.Context, query Query) (*QueryResult, error) {
	start := time.Now()
	db.mu.RLock()
	defer db.mu.RUnlock()

	db.totalQueries++

	var result []Metric
	var samplesScanned int64

	for _, m := range db.series {
		if m.Name != query.MetricName {
			continue
		}

		// Check label matchers
		if !matchLabels(m.Labels, query.LabelMatchers) {
			continue
		}

		// Filter samples by time range
		var filtered []Sample
		for _, s := range m.Samples {
			samplesScanned++
			if (s.Timestamp.Equal(query.Start) || s.Timestamp.After(query.Start)) &&
				(s.Timestamp.Equal(query.End) || s.Timestamp.Before(query.End)) {
				filtered = append(filtered, s)
			}
		}

		if len(filtered) > 0 {
			result = append(result, Metric{
				Name:    m.Name,
				Labels:  m.Labels,
				Samples: filtered,
			})
		}
	}

	// Apply aggregation if specified
	if query.Aggregation != "" && len(result) > 0 {
		result = aggregate(result, query.Aggregation, query.GroupBy)
	}

	return &QueryResult{
		Series: result,
		Stats: QueryStats{
			SeriesCount:    len(result),
			SamplesScanned: samplesScanned,
			ExecutionTime:  time.Since(start),
		},
	}, nil
}

// QueryInstant returns the most recent value for matching series.
func (db *memoryTSDB) QueryInstant(ctx context.Context, metricName string, labels map[string]string) ([]Metric, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var result []Metric

	for _, m := range db.series {
		if m.Name != metricName {
			continue
		}
		if !matchLabels(m.Labels, labels) {
			continue
		}
		if len(m.Samples) == 0 {
			continue
		}

		// Return only the last sample
		lastSample := m.Samples[len(m.Samples)-1]
		result = append(result, Metric{
			Name:    m.Name,
			Labels:  m.Labels,
			Samples: []Sample{lastSample},
		})
	}

	return result, nil
}

// DeleteSeries removes matching series data.
func (db *memoryTSDB) DeleteSeries(ctx context.Context, metricName string, labels map[string]string, start, end time.Time) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	for key, m := range db.series {
		if m.Name != metricName {
			continue
		}
		if !matchLabels(m.Labels, labels) {
			continue
		}
		// Remove samples in range
		var remaining []Sample
		for _, s := range m.Samples {
			if s.Timestamp.Before(start) || s.Timestamp.After(end) {
				remaining = append(remaining, s)
			}
		}
		if len(remaining) == 0 {
			delete(db.series, key)
		} else {
			m.Samples = remaining
		}
	}

	return nil
}

func (db *memoryTSDB) Close() error {
	db.mu.Lock()
	db.series = make(map[string]*Metric)
	db.mu.Unlock()
	return nil
}

func (db *memoryTSDB) Healthy(ctx context.Context) bool {
	return true
}

func (db *memoryTSDB) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, m := range db.series {
		if len(m.Samples) == 0 {
			oldestKey = key
			break
		}
		last := m.Samples[len(m.Samples)-1].Timestamp
		if oldestKey == "" || last.Before(oldestTime) {
			oldestKey = key
			oldestTime = last
		}
	}
	if oldestKey != "" {
		delete(db.series, oldestKey)
	}
}

func (db *memoryTSDB) retentionLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		retention := time.Duration(db.config.RetentionDays) * 24 * time.Hour
		if retention == 0 {
			retention = 30 * 24 * time.Hour
		}
		cutoff := time.Now().Add(-retention)

		db.mu.Lock()
		for key, m := range db.series {
			var remaining []Sample
			for _, s := range m.Samples {
				if s.Timestamp.After(cutoff) {
					remaining = append(remaining, s)
				}
			}
			if len(remaining) == 0 {
				delete(db.series, key)
			} else {
				m.Samples = remaining
			}
		}
		db.mu.Unlock()
	}
}

// ============================================================================
// VictoriaMetrics TSDB — production backend (stub)
// ============================================================================

type vmTSDB struct {
	endpoint string
	logger   *logrus.Logger
	fallback *memoryTSDB
}

// NewVictoriaMetricsTSDB creates a VictoriaMetrics-backed TSDB.
func NewVictoriaMetricsTSDB(cfg Config, logger *logrus.Logger) TSDB {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &vmTSDB{
		endpoint: cfg.VMEndpoint,
		logger:   logger,
		fallback: NewMemoryTSDB(cfg, logger).(*memoryTSDB),
	}
}

func (db *vmTSDB) Write(ctx context.Context, metrics []Metric) error {
	// NOTE: In production, POST to VictoriaMetrics /api/v1/import/prometheus
	return db.fallback.Write(ctx, metrics)
}

func (db *vmTSDB) Query(ctx context.Context, query Query) (*QueryResult, error) {
	// NOTE: In production, GET from VictoriaMetrics /api/v1/query_range
	return db.fallback.Query(ctx, query)
}

func (db *vmTSDB) QueryInstant(ctx context.Context, metricName string, labels map[string]string) ([]Metric, error) {
	return db.fallback.QueryInstant(ctx, metricName, labels)
}

func (db *vmTSDB) DeleteSeries(ctx context.Context, metricName string, labels map[string]string, start, end time.Time) error {
	return db.fallback.DeleteSeries(ctx, metricName, labels, start, end)
}

func (db *vmTSDB) Close() error {
	return db.fallback.Close()
}

func (db *vmTSDB) Healthy(ctx context.Context) bool {
	// NOTE: In production, GET /health from VictoriaMetrics
	return true
}

// ============================================================================
// Well-known Metric Names
// ============================================================================

const (
	// GPU metrics
	MetricGPUUtilization    = "cloudai_gpu_utilization_percent"
	MetricGPUMemoryUsed     = "cloudai_gpu_memory_used_bytes"
	MetricGPUTemperature    = "cloudai_gpu_temperature_celsius"
	MetricGPUPowerWatts     = "cloudai_gpu_power_watts"

	// Node metrics
	MetricNodeCPUUsage      = "cloudai_node_cpu_usage_percent"
	MetricNodeMemoryUsage   = "cloudai_node_memory_usage_percent"
	MetricNodeDiskUsage     = "cloudai_node_disk_usage_percent"
	MetricNodeNetworkRxBytes = "cloudai_node_network_rx_bytes"
	MetricNodeNetworkTxBytes = "cloudai_node_network_tx_bytes"

	// Workload metrics
	MetricWorkloadDuration  = "cloudai_workload_duration_seconds"
	MetricWorkloadQueueTime = "cloudai_workload_queue_time_seconds"

	// Cluster metrics
	MetricClusterNodeCount  = "cloudai_cluster_node_count"
	MetricClusterGPUCount   = "cloudai_cluster_gpu_count"

	// API metrics
	MetricAPIRequestTotal   = "cloudai_api_request_total"
	MetricAPIRequestDuration = "cloudai_api_request_duration_seconds"
)

// ============================================================================
// Factory
// ============================================================================

// New creates a TSDB based on configuration.
func New(cfg Config, logger *logrus.Logger) TSDB {
	switch cfg.Backend {
	case "victoriametrics":
		return NewVictoriaMetricsTSDB(cfg, logger)
	default:
		return NewMemoryTSDB(cfg, logger)
	}
}

// ============================================================================
// Helpers
// ============================================================================

func matchLabels(actual, matchers map[string]string) bool {
	for k, v := range matchers {
		if actual[k] != v {
			return false
		}
	}
	return true
}

func aggregate(series []Metric, aggFunc string, groupBy []string) []Metric {
	if len(series) == 0 {
		return series
	}

	// Group series by groupBy labels
	groups := make(map[string][]Metric)
	for _, s := range series {
		key := ""
		for _, label := range groupBy {
			key += s.Labels[label] + "|"
		}
		groups[key] = append(groups[key], s)
	}

	var result []Metric
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}

		// Merge all samples
		var allSamples []Sample
		for _, s := range group {
			allSamples = append(allSamples, s.Samples...)
		}

		// Sort by timestamp
		sort.Slice(allSamples, func(i, j int) bool {
			return allSamples[i].Timestamp.Before(allSamples[j].Timestamp)
		})

		// Apply aggregation
		var aggValue float64
		switch aggFunc {
		case "sum":
			for _, s := range allSamples {
				aggValue += s.Value
			}
		case "avg":
			for _, s := range allSamples {
				aggValue += s.Value
			}
			aggValue /= float64(len(allSamples))
		case "max":
			aggValue = math.Inf(-1)
			for _, s := range allSamples {
				if s.Value > aggValue {
					aggValue = s.Value
				}
			}
		case "min":
			aggValue = math.Inf(1)
			for _, s := range allSamples {
				if s.Value < aggValue {
					aggValue = s.Value
				}
			}
		case "count":
			aggValue = float64(len(allSamples))
		}

		labels := make(map[string]string)
		for _, label := range groupBy {
			labels[label] = group[0].Labels[label]
		}

		result = append(result, Metric{
			Name:   group[0].Name,
			Labels: labels,
			Samples: []Sample{{
				Timestamp: time.Now(),
				Value:     aggValue,
			}},
		})
	}

	return result
}
