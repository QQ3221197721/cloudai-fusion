package scheduler

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// DASPAcceptanceRate tracks the acceptance rate under different demand distributions
	daspAcceptanceRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dasp_acceptance_rate",
			Help: "DASP scheduler acceptance rate (0-1)",
		},
		[]string{"distribution"},
	)

	// DASPFragIndex measures MIG-aware fragmentation in each zone (lower=better, 0-1)
	daspFragmentationMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dasp_fragmentation_index",
			Help: "MIG-aware fragmentation index (lower=better, 0-1)",
		},
		[]string{"zone"},
	)

	// DASPPutPerOpHistogram records placement decision time in nanoseconds
	daspRuntimeOverhead = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dasp_placement_ns_per_op",
			Help:    "Time per placement decision in nanoseconds",
			Buckets: prometheus.ExponentialBuckets(1_000, 2, 14), // 1μs to ~16ms
		},
		[]string{"strategy"}, // dasp vs bestfit vs hamiproxy
	)

	// DASPModeSwitchesTotal counts adaptive mode switches (uniform→skewed transitions)
	daspModeSwitchesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dasp_mode_switches_total",
			Help: "Total number of adaptive mode switches (uniform→skewed)",
		},
	)

	// DASPCascadeEventsTotal tracks cascade events by depth level
	daspCascadeEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dasp_cascade_events_total",
			Help: "Total cascade events by level (level1, level2, ...)",
		},
		[]string{"level"},
	)

	// DASPRatioHistory maintains a rolling window of computed rho values for trend analysis
	daspRatioHistory = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dasp_computed_reservation_ratio",
			Help: "Computed reservation ratio ρ from demand distribution",
		},
		[]string{"scenario"},
	)

	// DASPZoneSizeActual tracks the actual zone size R chosen vs theoretical max
	daspZoneSizeMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dasp_zone_size_gpu_count",
			Help: "Actual large-zone size R (in GPUs)",
		},
		[]string{"distribution"},
	)
)

func init() {
	prometheus.MustRegister(
		daspAcceptanceRate,
		daspFragmentationMetric,
		daspRuntimeOverhead,
		daspModeSwitchesTotal,
		daspCascadeEventsTotal,
		daspRatioHistory,
		daspZoneSizeMetric,
	)
}

// MetricLevel represents structured logging context for DASP decisions
type MetricLevel int

const (
	MetricInfo    MetricLevel = iota // General information
	MetricWarning                   // Warning conditions
	MetricError                     // Error states
)

// LogCascadeEvent records a cascade event with structured fields
func LogCascadeEvent(level MetricLevel, profile MIGSliceProfile, gpuIdx int, reason string) {
	// Increment appropriate counter based on cascade level
	switch level {
	case MetricInfo:
		daspCascadeEventsTotal.WithLabelValues("level1").Inc()
	case MetricWarning, MetricError:
		daspCascadeEventsTotal.WithLabelValues("level2+").Inc()
	default:
		daspCascadeEventsTotal.WithLabelValues("unknown").Inc()
	}
	
	// In production, this would also log structured fields via zap/slog:
	// logger.Info("DASP cascade event",
	// 	"level", level,
	// 	"profile", profile.Name(),
	// 	"gpu_idx", gpuIdx,
	// 	"reason", reason,
	// )
}

// RecordMetrics updates all DASP metrics from current scheduling state
func RecordMetrics(acceptanceRate float64, 
	fragmentation map[string]float64, 
	placementDuration time.Duration,
	strategy string,
	computedRatio float64,
	actualZoneSize int,
	distribution string) {
	
	// Update acceptance rate
	daspAcceptanceRate.WithLabelValues(distribution).Set(acceptanceRate)
	
	// Update fragmentation per zone
	for zone, frag := range fragmentation {
		daspFragmentationMetric.WithLabelValues(zone).Set(frag)
	}
	
	// Record placement duration
	daspRuntimeOverhead.WithLabelValues(strategy).Observe(float64(placementDuration.Nanoseconds()))
	
	// Track computed ratio for trend analysis
	daspRatioHistory.WithLabelValues(distribution).Set(computedRatio)
	
	// Track actual zone size chosen
	daspZoneSizeMetric.WithLabelValues(distribution).Set(float64(actualZoneSize))
}
