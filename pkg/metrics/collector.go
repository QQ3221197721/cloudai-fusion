package metrics

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Resource Utilization Collector
// ============================================================================

// ResourceCollectorConfig configures the resource utilization collector.
type ResourceCollectorConfig struct {
	// CollectInterval controls how often resource metrics are sampled.
	CollectInterval time.Duration

	// Logger for collector operations.
	Logger *logrus.Logger

	// NodeName identifies this node in metrics labels.
	NodeName string

	// ClusterID identifies the cluster in metrics labels.
	ClusterID string

	// GPUCollector is an optional function that returns GPU metrics.
	// If nil, GPU metrics are not collected.
	GPUCollector func() []GPUMetrics

	// EnableGoRuntime enables Go runtime metrics (goroutines, heap, GC).
	EnableGoRuntime bool
}

// GPUMetrics represents GPU device metrics returned by the collector function.
type GPUMetrics struct {
	Index         string
	Model         string
	Utilization   float64 // 0-100
	MemoryUsed    float64 // bytes
	MemoryTotal   float64 // bytes
	Temperature   float64 // celsius
	PowerUsage    float64 // watts
}

// ResourceCollector periodically collects resource utilization metrics
// and updates Prometheus gauges.
type ResourceCollector struct {
	config ResourceCollectorConfig
	cancel context.CancelFunc
	mu     sync.Mutex
}

// NewResourceCollector creates a new resource collector.
func NewResourceCollector(cfg ResourceCollectorConfig) *ResourceCollector {
	if cfg.CollectInterval == 0 {
		cfg.CollectInterval = 15 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.NodeName == "" {
		cfg.NodeName = "unknown"
	}

	return &ResourceCollector{
		config: cfg,
	}
}

// Start begins periodic resource collection.
func (c *ResourceCollector) Start(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx, c.cancel = context.WithCancel(ctx)
	go c.collectLoop(ctx)
	c.config.Logger.WithFields(logrus.Fields{
		"interval":  c.config.CollectInterval,
		"node":      c.config.NodeName,
		"gpu":       c.config.GPUCollector != nil,
		"goruntime": c.config.EnableGoRuntime,
	}).Info("Resource collector started")
}

// Stop halts the collection loop.
func (c *ResourceCollector) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *ResourceCollector) collectLoop(ctx context.Context) {
	// Collect immediately on start
	c.collect()

	ticker := time.NewTicker(c.config.CollectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect()
		}
	}
}

func (c *ResourceCollector) collect() {
	if c.config.EnableGoRuntime {
		c.collectGoRuntime()
	}
	if c.config.GPUCollector != nil {
		c.collectGPU()
	}
}

func (c *ResourceCollector) collectGoRuntime() {
	// Goroutines
	GoroutinesCount.Set(float64(runtime.NumGoroutine()))

	// Memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	MemoryUsedBytes.WithLabelValues(c.config.ClusterID, c.config.NodeName).Set(float64(m.Alloc))
	MemoryTotalBytes.WithLabelValues(c.config.ClusterID, c.config.NodeName).Set(float64(m.Sys))
}

func (c *ResourceCollector) collectGPU() {
	gpus := c.config.GPUCollector()
	for _, gpu := range gpus {
		GPUUtilization.WithLabelValues(
			c.config.ClusterID, c.config.NodeName, gpu.Index, gpu.Model,
		).Set(gpu.Utilization)

		GPUMemoryUsedBytes.WithLabelValues(
			c.config.ClusterID, c.config.NodeName, gpu.Index,
		).Set(gpu.MemoryUsed)

		GPUMemoryTotalBytes.WithLabelValues(
			c.config.ClusterID, c.config.NodeName, gpu.Index,
		).Set(gpu.MemoryTotal)

		GPUTemperatureCelsius.WithLabelValues(
			c.config.ClusterID, c.config.NodeName, gpu.Index,
		).Set(gpu.Temperature)

		GPUPowerWatts.WithLabelValues(
			c.config.ClusterID, c.config.NodeName, gpu.Index,
		).Set(gpu.PowerUsage)
	}
}

// ============================================================================
// Database Pool Collector
// ============================================================================

// DBPoolStats represents database connection pool statistics.
type DBPoolStats struct {
	MaxOpen     int
	Open        int
	InUse       int
	Idle        int
	WaitCount   int64
	WaitTime    time.Duration
}

// CollectDBPoolStats updates connection pool utilization metrics.
func CollectDBPoolStats(poolName string, stats DBPoolStats) {
	if stats.MaxOpen > 0 {
		utilization := float64(stats.InUse) / float64(stats.MaxOpen)
		ConnectionPoolUtilization.WithLabelValues(poolName).Set(utilization)
	}
}
