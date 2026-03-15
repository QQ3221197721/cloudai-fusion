// Package metrics provides a unified metrics registry for CloudAI Fusion,
// implementing the Four Golden Signals (latency, traffic, errors, saturation),
// business metrics, resource utilization, cost tracking, and SLI/SLO indicators.
//
// All metrics use the "cloudai" namespace and are automatically registered
// with Prometheus default registry on package initialization.
package metrics

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ============================================================================
// Four Golden Signals — Latency, Traffic, Errors, Saturation
// ============================================================================

var (
	// --- Latency ---

	// HTTPRequestDuration tracks the latency distribution of HTTP requests.
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "cloudai",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds (golden signal: latency)",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path", "status_code"},
	)

	// GRPCRequestDuration tracks the latency of gRPC calls.
	GRPCRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "cloudai",
			Subsystem: "grpc",
			Name:      "request_duration_seconds",
			Help:      "gRPC request duration in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 5},
		},
		[]string{"service", "method", "status"},
	)

	// --- Traffic ---

	// HTTPRequestsTotal counts total HTTP requests (golden signal: traffic).
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests processed (golden signal: traffic)",
		},
		[]string{"method", "path", "status_code"},
	)

	// GRPCRequestsTotal counts total gRPC calls.
	GRPCRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "grpc",
			Name:      "requests_total",
			Help:      "Total gRPC requests processed",
		},
		[]string{"service", "method", "status"},
	)

	// HTTPRequestsInFlight tracks currently in-progress HTTP requests.
	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Number of HTTP requests currently being processed",
		},
	)

	// --- Errors ---

	// HTTPErrorsTotal counts HTTP responses with 4xx/5xx status codes.
	HTTPErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "http",
			Name:      "errors_total",
			Help:      "Total HTTP error responses (golden signal: errors)",
		},
		[]string{"method", "path", "status_code", "error_type"},
	)

	// PanicsRecoveredTotal counts recovered panics.
	PanicsRecoveredTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "http",
			Name:      "panics_recovered_total",
			Help:      "Total panics recovered by middleware",
		},
	)

	// --- Saturation ---

	// HTTPResponseSizeBytes tracks response payload sizes.
	HTTPResponseSizeBytes = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "cloudai",
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "HTTP response size in bytes (golden signal: saturation)",
			Buckets:   prometheus.ExponentialBuckets(100, 10, 8), // 100B..1GB
		},
		[]string{"method", "path"},
	)

	// GoroutinesCount tracks active goroutines (runtime saturation).
	GoroutinesCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "runtime",
			Name:      "goroutines_count",
			Help:      "Number of active goroutines",
		},
	)

	// ConnectionPoolUtilization tracks DB/cache connection pool saturation.
	ConnectionPoolUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "pool",
			Name:      "utilization_ratio",
			Help:      "Connection pool utilization ratio (0-1)",
		},
		[]string{"pool_name"},
	)
)

// ============================================================================
// Business Metrics
// ============================================================================

var (
	// TaskSuccessTotal counts successfully completed AI tasks.
	TaskSuccessTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "task",
			Name:      "success_total",
			Help:      "Total successful AI task completions",
		},
		[]string{"task_type", "cluster_id"},
	)

	// TaskFailureTotal counts failed AI tasks.
	TaskFailureTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "task",
			Name:      "failure_total",
			Help:      "Total failed AI tasks",
		},
		[]string{"task_type", "cluster_id", "failure_reason"},
	)

	// TaskDuration tracks AI task execution duration.
	TaskDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "cloudai",
			Subsystem: "task",
			Name:      "duration_seconds",
			Help:      "AI task execution duration in seconds",
			Buckets:   []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600},
		},
		[]string{"task_type"},
	)

	// SchedulingLatency tracks how long a workload waits before being scheduled.
	SchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "cloudai",
			Subsystem: "scheduler",
			Name:      "scheduling_latency_seconds",
			Help:      "Time from workload submission to scheduling decision",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		},
	)

	// SchedulerDecisionsTotal counts scheduling decisions by outcome.
	SchedulerDecisionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "scheduler",
			Name:      "decisions_total",
			Help:      "Total scheduling decisions",
		},
		[]string{"result"}, // placed, preempted, rejected, deferred
	)

	// AIInferenceLatency tracks AI model inference latency.
	AIInferenceLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "cloudai",
			Subsystem: "ai",
			Name:      "inference_latency_seconds",
			Help:      "AI model inference latency in seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		},
		[]string{"model_name", "model_version"},
	)

	// AIInferenceThroughput tracks inference throughput (tokens/sec or samples/sec).
	AIInferenceThroughput = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "ai",
			Name:      "inference_throughput",
			Help:      "AI inference throughput (samples or tokens per second)",
		},
		[]string{"model_name", "unit"}, // unit: tokens_per_sec, samples_per_sec
	)

	// EventBusPublishedTotal counts events published to the event bus.
	EventBusPublishedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "eventbus",
			Name:      "published_total",
			Help:      "Total events published to event bus",
		},
		[]string{"topic"},
	)

	// EventBusDeliveredTotal counts events delivered to subscribers.
	EventBusDeliveredTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "eventbus",
			Name:      "delivered_total",
			Help:      "Total events delivered to subscribers",
		},
		[]string{"topic"},
	)

	// EventBusErrorsTotal counts event delivery failures.
	EventBusErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "eventbus",
			Name:      "errors_total",
			Help:      "Total event delivery errors",
		},
		[]string{"topic", "error_type"},
	)
)

// ============================================================================
// Resource Utilization Metrics
// ============================================================================

var (
	// GPUUtilization tracks GPU compute utilization percentage.
	GPUUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "gpu_utilization_percent",
			Help:      "GPU compute utilization percentage (0-100)",
		},
		[]string{"cluster_id", "node", "gpu_index", "gpu_model"},
	)

	// GPUMemoryUsedBytes tracks GPU memory usage.
	GPUMemoryUsedBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "gpu_memory_used_bytes",
			Help:      "GPU memory used in bytes",
		},
		[]string{"cluster_id", "node", "gpu_index"},
	)

	// GPUMemoryTotalBytes tracks total GPU memory.
	GPUMemoryTotalBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "gpu_memory_total_bytes",
			Help:      "Total GPU memory in bytes",
		},
		[]string{"cluster_id", "node", "gpu_index"},
	)

	// GPUTemperatureCelsius tracks GPU temperature.
	GPUTemperatureCelsius = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "gpu_temperature_celsius",
			Help:      "GPU temperature in Celsius",
		},
		[]string{"cluster_id", "node", "gpu_index"},
	)

	// GPUPowerWatts tracks GPU power consumption.
	GPUPowerWatts = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "gpu_power_watts",
			Help:      "GPU power consumption in watts",
		},
		[]string{"cluster_id", "node", "gpu_index"},
	)

	// CPUUtilization tracks CPU utilization per node.
	CPUUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "cpu_utilization_percent",
			Help:      "CPU utilization percentage (0-100)",
		},
		[]string{"cluster_id", "node"},
	)

	// MemoryUsedBytes tracks memory usage per node.
	MemoryUsedBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "memory_used_bytes",
			Help:      "Memory used in bytes",
		},
		[]string{"cluster_id", "node"},
	)

	// MemoryTotalBytes tracks total memory per node.
	MemoryTotalBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "memory_total_bytes",
			Help:      "Total memory in bytes",
		},
		[]string{"cluster_id", "node"},
	)

	// DiskUsedBytes tracks disk usage per node.
	DiskUsedBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "disk_used_bytes",
			Help:      "Disk used in bytes",
		},
		[]string{"cluster_id", "node", "mount_point"},
	)

	// NetworkBandwidthBytes tracks network I/O.
	NetworkBandwidthBytes = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "resource",
			Name:      "network_bytes_total",
			Help:      "Total network bytes transferred",
		},
		[]string{"cluster_id", "node", "direction"}, // rx, tx
	)
)

// ============================================================================
// Cost Metrics
// ============================================================================

var (
	// CloudResourceCostHourly tracks hourly cloud resource cost.
	CloudResourceCostHourly = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "cost",
			Name:      "resource_hourly_usd",
			Help:      "Hourly cloud resource cost in USD",
		},
		[]string{"cluster_id", "provider", "resource_type"}, // gpu, cpu, storage, network
	)

	// ComputeUnitCost tracks cost per compute unit (e.g., $/GPU-hour).
	ComputeUnitCost = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "cost",
			Name:      "compute_unit_usd",
			Help:      "Cost per compute unit in USD",
		},
		[]string{"provider", "instance_type", "unit"}, // gpu_hour, cpu_hour
	)

	// CostSavingsTotal tracks accumulated cost savings.
	CostSavingsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "cost",
			Name:      "savings_total_usd",
			Help:      "Total cost savings achieved in USD",
		},
		[]string{"strategy"}, // spot, preemptible, right-sizing, scheduling
	)

	// BudgetUtilizationRatio tracks budget consumption.
	BudgetUtilizationRatio = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "cost",
			Name:      "budget_utilization_ratio",
			Help:      "Budget utilization ratio (0-1, 1 = fully consumed)",
		},
		[]string{"team", "project"},
	)
)

// ============================================================================
// Registry — centralized metric access
// ============================================================================

// Registry provides thread-safe access to custom metric collectors.
type Registry struct {
	mu       sync.RWMutex
	customs  map[string]prometheus.Collector
	registry *prometheus.Registry
}

// NewRegistry creates a metrics registry wrapping the Prometheus default registry.
func NewRegistry() *Registry {
	return &Registry{
		customs:  make(map[string]prometheus.Collector),
		registry: prometheus.DefaultRegisterer.(*prometheus.Registry),
	}
}

// Register adds a custom collector to the registry.
func (r *Registry) Register(name string, c prometheus.Collector) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.customs[name]; exists {
		return fmt.Errorf("metric %q already registered", name)
	}
	if err := prometheus.Register(c); err != nil {
		return err
	}
	r.customs[name] = c
	return nil
}

// Unregister removes a custom collector.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.customs[name]
	if !ok {
		return false
	}
	prometheus.Unregister(c)
	delete(r.customs, name)
	return true
}

// Get retrieves a custom collector by name.
func (r *Registry) Get(name string) (prometheus.Collector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.customs[name]
	return c, ok
}
