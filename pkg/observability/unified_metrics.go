package observability

import (
	"context"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// ============================================================================
// Unified Metrics Collector for CloudAI Fusion
// ============================================================================
// 
// This module integrates metrics from four completed algorithm modules:
// - M47: Distributed tracing with cross-service correlation
// - M29/M31: ML pipeline security with drift detection  
// - M49: Self-healing benchmark with K8s integration
// - M9: Quantile P² sketch for streaming metrics
//
// The unified collector provides a single source of truth for observability
// across all modules, enabling cross-module correlation and consolidated alerting.

// ModuleStats bundles statistics exported by individual module collectors
type ModuleStats struct {
	Tracing    TracingStatistics
	MLSecurity MLSecurityStatistics
	SelfHeal   SelfHealStatistics
	Quantile   QuantileStatistics
}

// TracingStatistics captures M47 distributed tracing metrics
type TracingStatistics struct {
	TotalSpans   int64
	ErrorCount   int64
	LatencyP99   float64 // milliseconds
	ServiceCount int
	ActiveTraces int
	AvgLatencyMs float64
}

// MLSecurityStatistics captures M29/M31 ML security metrics
type MLSecurityStatistics struct {
	MaxPSI            float64
	AveragePSI        float64
	MinPSI            float64
	ActiveModels      int
	BlockedListEvents int64
	DriftFeatures     []string
	AnomalyScore      float64
	ModelVersions     []string
}

// SelfHealStatistics captures M49 self-healing metrics
type SelfHealStatistics struct {
	AvgMTTR           float64
	P95MTTR           float64
	P99MTTR           float64
	SuccessCount      int64
	FailureCount      int64
	InRecovery        int
	RecoveredLastHour int64
	FaultTypes        []string
}

// QuantileStatistics captures M9 quantile P² sketch metrics
type QuantileStatistics struct {
	P50          float64
	P90          float64
	P99          float64
	MemoryUsage  int64
	SampleCount  int64
	DriftPercent float64
}

// TracingCollector interfaces with M47 distributed tracing module
type TracingCollector interface {
	ExportStats() TracingStatistics
}

// MLSecurityCollector interfaces with M29/M31 ML security module
type MLSecurityCollector interface {
	ExportStats() MLSecurityStatistics
}

// SelfHealCollector interfaces with M49 self-healing module
type SelfHealCollector interface {
	ExportStats() SelfHealStatistics
}

// QuantileCollector interfaces with M9 quantile sketch module
type QuantileCollector interface {
	ExportStats() QuantileStatistics
}

// UnifiedCollector aggregates metrics from all four algorithm modules
type UnifiedCollector struct {
	mu sync.RWMutex
	
	tracing    TracingCollector
	mlSecurity MLSecurityCollector
	healing    SelfHealCollector
	quantiles  QuantileCollector
	
	totalSpans        prometheus.Gauge
	errorRate         prometheus.Gauge
	avgDriftPSI       prometheus.Gauge
	errorCount        prometheus.Counter
	tracesTotal       prometheus.Counter
	mttrSeconds       prometheus.Histogram
	remediationsTotal prometheus.Counter
	remediationsFailed prometheus.Counter
	quantileValues    map[float64]prometheus.Gauge
	
	skipTracing    bool
	skipMLSecurity bool
	skipSelfHeal   bool
	skipQuantile   bool
}

// UnifiedCollectorConfig configures which modules to include
type UnifiedCollectorConfig struct {
	EnableTracing    bool
	EnableMLSecurity bool
	EnableSelfHeal   bool
	EnableQuantile   bool
}

// DefaultUnifiedCollectorConfig returns default configuration
func DefaultUnifiedCollectorConfig() UnifiedCollectorConfig {
	return UnifiedCollectorConfig{
		EnableTracing:    true,
		EnableMLSecurity: true,
		EnableSelfHeal:   true,
		EnableQuantile:   true,
	}
}

// NewUnifiedCollector creates a new unified metrics collector
func NewUnifiedCollector() *UnifiedCollector {
	return NewUnifiedCollectorWithConfig(DefaultUnifiedCollectorConfig())
}

// NewUnifiedCollectorWithConfig creates collector with custom module selection
func NewUnifiedCollectorWithConfig(cfg UnifiedCollectorConfig) *UnifiedCollector {
	
	defaultTracing := &dummyTracingCollector{}
	defaultMLOps := &dummyMLSCollector{}
	defaultHealing := &dummySelfHealCollector{}
	defaultQuantile := &dummyQuantileCollector{}
	
	uc := &UnifiedCollector{
		tracing:    defaultTracing,
		mlSecurity: defaultMLOps,
		healing:    defaultHealing,
		quantiles:  defaultQuantile,
		
		skipTracing:    !cfg.EnableTracing,
		skipMLSecurity: !cfg.EnableMLSecurity,
		skipSelfHeal:   !cfg.EnableSelfHeal,
		skipQuantile:   !cfg.EnableQuantile,
		
		totalSpans: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cloudai_total_traces",
			Help: "Total trace count across all services (M47)",
		}),
		
		errorRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cloudai_error_rate",
			Help: "Overall HTTP error rate derived from traces (0-1)",
		}),
		
		avgDriftPSI: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cloudai_avg_drift_psi",
			Help: "Average PSI drift metric across ML features (M29/M31)",
		}),
		
		errorCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cloudai_trace_errors_total",
			Help: "Total number of traced errors (M47)",
		}),
		
		tracesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cloudai_spans_total",
			Help: "Total number of spans recorded (M47)",
		}),
		
		mttrSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "cloudai_mttr_seconds",
			Help:    "Mean Time To Recovery in seconds (M49)",
			Buckets: []float64{5, 10, 30, 60, 120, 300, 600},
		}),
		
		remediationsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cloudai_remediations_success_total",
			Help: "Successful remediation actions taken (M49)",
		}),
		
		remediationsFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cloudai_remediations_failed_total",
			Help: "Failed remediation actions (M49)",
		}),
		
		quantileValues: make(map[float64]prometheus.Gauge),
	}
	
	if !uc.skipQuantile {
		for _, q := range []float64{0.5, 0.9, 0.99} {
			uc.quantileValues[q] = prometheus.NewGauge(prometheus.GaugeOpts{
				Name: fmt.Sprintf("cloudai_quantile_p%d", int(q*100)),
				Help: fmt.Sprintf("Estimated %dth percentile from streaming data (M9)", int(q*100)),
			})
		}
	}
	
	return uc
}

// SetTracingCollector allows injecting real M47 tracing collector
func (uc *UnifiedCollector) SetTracingCollector(c TracingCollector) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.tracing = c
}

// SetMLSecurityCollector allows injecting real M29/M31 ML security collector
func (uc *UnifiedCollector) SetMLSecurityCollector(c MLSecurityCollector) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.mlSecurity = c
}

// SetSelfHealCollector allows injecting real M49 self-healing collector
func (uc *UnifiedCollector) SetSelfHealCollector(c SelfHealCollector) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.healing = c
}

// SetQuantileCollector allows injecting real M9 quantile collector
func (uc *UnifiedCollector) SetQuantileCollector(c QuantileCollector) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.quantiles = c
}

// CollectMetrics aggregates all module metrics into unified time series
func (uc *UnifiedCollector) CollectMetrics(ctx context.Context) map[string]float64 {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	
	results := make(map[string]float64)
	
	// Gather M47 distributed tracing metrics
	if !uc.skipTracing && uc.tracing != nil {
		tracingStats := uc.tracing.ExportStats()
		results["traces_total"] = float64(tracingStats.TotalSpans)
		results["traces_errors"] = float64(tracingStats.ErrorCount)
		results["trace_latency_p99"] = tracingStats.LatencyP99
		results["trace_services_active"] = float64(tracingStats.ServiceCount)
		results["trace_active_count"] = float64(tracingStats.ActiveTraces)
		
		uc.totalSpans.Set(float64(tracingStats.TotalSpans))
		uc.errorCount.Add(float64(tracingStats.ErrorCount))
		
		if tracingStats.TotalSpans > 0 {
			errorRate := float64(tracingStats.ErrorCount) / float64(tracingStats.TotalSpans)
			uc.errorRate.Set(errorRate)
			results["composite_error_rate"] = errorRate
			results["error_count"] = float64(tracingStats.ErrorCount)
		}
	}
	
	// Gather M29/M31 ML security metrics
	if !uc.skipMLSecurity && uc.mlSecurity != nil {
		mlStats := uc.mlSecurity.ExportStats()
		results["drift_psi_max"] = mlStats.MaxPSI
		results["drift_psi_avg"] = mlStats.AveragePSI
		results["drift_psi_min"] = mlStats.MinPSI
		results["model_versions_active"] = float64(mlStats.ActiveModels)
		results["security_events_blocked"] = float64(mlStats.BlockedListEvents)
		results["anomaly_score"] = mlStats.AnomalyScore
		results["drift_features_count"] = float64(len(mlStats.DriftFeatures))
		
		uc.avgDriftPSI.Set(mlStats.AveragePSI)
	}
	
	// Gather M49 self-healing metrics
	if !uc.skipSelfHeal && uc.healing != nil {
		healStats := uc.healing.ExportStats()
		results["mttr_avg"] = healStats.AvgMTTR
		results["mttr_p95"] = healStats.P95MTTR
		results["mttr_p99"] = healStats.P99MTTR
		results["remediations_success"] = float64(healStats.SuccessCount)
		results["remediations_failed"] = float64(healStats.FailureCount)
		results["in_recovery_count"] = float64(healStats.InRecovery)
		results["recovered_last_hour"] = float64(healStats.RecoveredLastHour)
		
		if healStats.AvgMTTR > 0 {
			uc.mttrSeconds.Observe(healStats.AvgMTTR)
		}
		uc.remediationsTotal.Add(float64(healStats.SuccessCount))
		uc.remediationsFailed.Add(float64(healStats.FailureCount))
		
		results["recovery_success_rate"] = calculateSuccessRate(healStats.SuccessCount, healStats.FailureCount)
	}
	
	// Gather M9 quantile metrics
	if !uc.skipQuantile && uc.quantiles != nil {
		quantStats := uc.quantiles.ExportStats()
		results["quantile_p50"] = quantStats.P50
		results["quantile_p90"] = quantStats.P90
		results["quantile_p99"] = quantStats.P99
		results["quantile_memory_bytes"] = float64(quantStats.MemoryUsage)
		results["quantile_sample_count"] = float64(quantStats.SampleCount)
		results["quantile_drift_percent"] = quantStats.DriftPercent
		
		if val, ok := uc.quantileValues[0.5]; ok {
			val.Set(quantStats.P50)
		}
		if val, ok := uc.quantileValues[0.9]; ok {
			val.Set(quantStats.P90)
		}
		if val, ok := uc.quantileValues[0.99]; ok {
			val.Set(quantStats.P99)
		}
	}
	
	return results
}

// Describe implements prometheus.Collector interface
func (uc *UnifiedCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- uc.totalSpans.Desc()
	ch <- uc.errorRate.Desc()
	ch <- uc.avgDriftPSI.Desc()
	ch <- uc.errorCount.Desc()
	ch <- uc.tracesTotal.Desc()
	ch <- uc.mttrSeconds.Desc()
	ch <- uc.remediationsTotal.Desc()
	ch <- uc.remediationsFailed.Desc()
	
	for _, g := range uc.quantileValues {
		ch <- g.Desc()
	}
}

// Collect implements prometheus.Collector interface
func (uc *UnifiedCollector) Collect(ch chan<- prometheus.Metric) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	
	ch <- uc.totalSpans
	ch <- uc.errorRate
	ch <- uc.avgDriftPSI
	ch <- uc.errorCount
	ch <- uc.tracesTotal
	ch <- uc.mttrSeconds
	ch <- uc.remediationsTotal
	ch <- uc.remediationsFailed
	
	for _, g := range uc.quantileValues {
		ch <- g
	}
}

// calculateSuccessRate computes success rate from counts
func calculateSuccessRate(success, failure int64) float64 {
	total := success + failure
	if total == 0 {
		return 1.0
	}
	return float64(success) / float64(total)
}

// Dummy collector implementations for testing
type dummyTracingCollector struct{}

func (d *dummyTracingCollector) ExportStats() TracingStatistics {
	return TracingStatistics{TotalSpans: 0, ErrorCount: 0, LatencyP99: 0, ServiceCount: 0, ActiveTraces: 0, AvgLatencyMs: 0}
}

type dummyMLSCollector struct{}

func (d *dummyMLSCollector) ExportStats() MLSecurityStatistics {
	return MLSecurityStatistics{MaxPSI: 0, AveragePSI: 0, MinPSI: 0, ActiveModels: 0, BlockedListEvents: 0, DriftFeatures: nil, AnomalyScore: 0, ModelVersions: nil}
}

type dummySelfHealCollector struct{}

func (d *dummySelfHealCollector) ExportStats() SelfHealStatistics {
	return SelfHealStatistics{AvgMTTR: 0, P95MTTR: 0, P99MTTR: 0, SuccessCount: 0, FailureCount: 0, InRecovery: 0, RecoveredLastHour: 0, FaultTypes: nil}
}

type dummyQuantileCollector struct{}

func (d *dummyQuantileCollector) ExportStats() QuantileStatistics {
	return QuantileStatistics{P50: 0, P90: 0, P99: 0, MemoryUsage: 0, SampleCount: 0, DriftPercent: 0}
}