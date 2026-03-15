// Package agent provides real metrics collection for CloudAI Fusion agents.
// Scrapes NVIDIA DCGM exporter and Prometheus node_exporter for real-time
// GPU and system metrics. Parses Prometheus text exposition format.
package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Collector Configuration
// ============================================================================

// CollectorConfig configures the metrics collector endpoints
type CollectorConfig struct {
	// DCGMExporterURL is the NVIDIA DCGM exporter endpoint (e.g., http://localhost:9400/metrics)
	DCGMExporterURL string
	// NodeExporterURL is the Prometheus node_exporter endpoint (e.g., http://localhost:9100/metrics)
	NodeExporterURL string
	// CollectInterval is the time between collection cycles
	CollectInterval time.Duration
	// Logger is the structured logger
	Logger *logrus.Logger
}

// ============================================================================
// Collected Metric Types
// ============================================================================

// GPUMetrics holds metrics for a single GPU device
type GPUMetrics struct {
	Index            int     `json:"index"`
	UUID             string  `json:"uuid,omitempty"`
	Name             string  `json:"name,omitempty"`
	UtilizationPct   float64 `json:"utilization_percent"`
	MemoryUsedBytes  float64 `json:"memory_used_bytes"`
	MemoryTotalBytes float64 `json:"memory_total_bytes"`
	MemoryUsagePct   float64 `json:"memory_usage_percent"`
	Temperature      float64 `json:"temperature_celsius"`
	PowerUsageWatts  float64 `json:"power_usage_watts"`
	PowerLimitWatts  float64 `json:"power_limit_watts"`
	FanSpeedPct      float64 `json:"fan_speed_percent"`
	SMClockMHz       float64 `json:"sm_clock_mhz"`
	MemClockMHz      float64 `json:"mem_clock_mhz"`
}

// NodeMetrics holds system-level metrics from node_exporter
type NodeMetrics struct {
	CPUUsagePct      float64 `json:"cpu_usage_percent"`
	CPUCores         int     `json:"cpu_cores"`
	MemoryTotalBytes float64 `json:"memory_total_bytes"`
	MemoryFreeBytes  float64 `json:"memory_free_bytes"`
	MemoryUsagePct   float64 `json:"memory_usage_percent"`
	DiskTotalBytes   float64 `json:"disk_total_bytes"`
	DiskFreeBytes    float64 `json:"disk_free_bytes"`
	DiskUsagePct     float64 `json:"disk_usage_percent"`
	NetworkRxBytes   float64 `json:"network_rx_bytes"`
	NetworkTxBytes   float64 `json:"network_tx_bytes"`
	LoadAvg1         float64 `json:"load_avg_1min"`
	LoadAvg5         float64 `json:"load_avg_5min"`
	LoadAvg15        float64 `json:"load_avg_15min"`
	UptimeSeconds    float64 `json:"uptime_seconds"`
}

// CollectedMetrics holds all metrics from a single collection cycle
type CollectedMetrics struct {
	Timestamp  time.Time    `json:"timestamp"`
	GPUs       []GPUMetrics `json:"gpus"`
	Node       *NodeMetrics `json:"node,omitempty"`
	Errors     []string     `json:"errors,omitempty"`
}

// ============================================================================
// Collector
// ============================================================================

// Collector scrapes NVIDIA DCGM and node_exporter for real metrics
type Collector struct {
	dcgmURL         string
	nodeExporterURL string
	interval        time.Duration
	logger          *logrus.Logger
	httpClient      *http.Client

	latest *CollectedMetrics
	mu     sync.RWMutex
}

// NewCollector creates a new metrics collector
func NewCollector(cfg CollectorConfig) *Collector {
	interval := cfg.CollectInterval
	if interval == 0 {
		interval = 15 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &Collector{
		dcgmURL:         cfg.DCGMExporterURL,
		nodeExporterURL: cfg.NodeExporterURL,
		interval:        interval,
		logger:          logger,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Start begins the periodic metrics collection loop
func (c *Collector) Start(ctx context.Context) {
	c.logger.WithFields(logrus.Fields{
		"dcgm_url":         c.dcgmURL,
		"node_exporter_url": c.nodeExporterURL,
		"interval":         c.interval.String(),
	}).Info("Metrics collector started")

	// Collect immediately
	c.collect()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Metrics collector stopped")
			return
		case <-ticker.C:
			c.collect()
		}
	}
}

// GetLatest returns the most recent collected metrics
func (c *Collector) GetLatest() *CollectedMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// collect performs one metrics collection cycle
func (c *Collector) collect() {
	result := &CollectedMetrics{
		Timestamp: time.Now().UTC(),
		GPUs:      make([]GPUMetrics, 0),
		Errors:    make([]string, 0),
	}

	// Collect GPU metrics from DCGM exporter
	if c.dcgmURL != "" {
		gpus, err := c.collectDCGM()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("DCGM: %s", err.Error()))
			c.logger.WithError(err).Debug("Failed to collect DCGM metrics")
		} else {
			result.GPUs = gpus
		}
	}

	// Collect node metrics from node_exporter
	if c.nodeExporterURL != "" {
		node, err := c.collectNodeExporter()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("node_exporter: %s", err.Error()))
			c.logger.WithError(err).Debug("Failed to collect node_exporter metrics")
		} else {
			result.Node = node
		}
	}

	c.mu.Lock()
	c.latest = result
	c.mu.Unlock()

	gpuCount := len(result.GPUs)
	if gpuCount > 0 {
		c.logger.WithField("gpu_count", gpuCount).Debug("GPU metrics collected")
	}
}

// ============================================================================
// NVIDIA DCGM Exporter Scraping
// ============================================================================

// collectDCGM scrapes the NVIDIA DCGM exporter and parses GPU metrics
func (c *Collector) collectDCGM() ([]GPUMetrics, error) {
	metrics, err := c.scrapePrometheus(c.dcgmURL)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape DCGM exporter: %w", err)
	}

	// Build GPU map by index
	gpuMap := make(map[int]*GPUMetrics)

	for _, m := range metrics {
		gpuIdx := -1
		if idx, ok := m.Labels["gpu"]; ok {
			gpuIdx, _ = strconv.Atoi(idx)
		}
		if gpuIdx < 0 {
			continue
		}

		gpu, exists := gpuMap[gpuIdx]
		if !exists {
			gpu = &GPUMetrics{Index: gpuIdx}
			gpuMap[gpuIdx] = gpu
		}

		// Map DCGM metric names to our struct
		// DCGM exporter metric names: https://docs.nvidia.com/datacenter/dcgm/latest/user-guide/feature-overview.html
		switch m.Name {
		case "DCGM_FI_DEV_GPU_UTIL":
			gpu.UtilizationPct = m.Value
		case "DCGM_FI_DEV_FB_USED":
			gpu.MemoryUsedBytes = m.Value * 1024 * 1024 // DCGM reports in MiB
		case "DCGM_FI_DEV_FB_FREE":
			freeBytes := m.Value * 1024 * 1024
			if gpu.MemoryTotalBytes == 0 && gpu.MemoryUsedBytes > 0 {
				gpu.MemoryTotalBytes = gpu.MemoryUsedBytes + freeBytes
			}
		case "DCGM_FI_DEV_FB_TOTAL":
			gpu.MemoryTotalBytes = m.Value * 1024 * 1024
		case "DCGM_FI_DEV_GPU_TEMP":
			gpu.Temperature = m.Value
		case "DCGM_FI_DEV_POWER_USAGE":
			gpu.PowerUsageWatts = m.Value
		case "DCGM_FI_DEV_ENFORCED_POWER_LIMIT":
			gpu.PowerLimitWatts = m.Value
		case "DCGM_FI_DEV_FAN_SPEED":
			gpu.FanSpeedPct = m.Value
		case "DCGM_FI_DEV_SM_CLOCK":
			gpu.SMClockMHz = m.Value
		case "DCGM_FI_DEV_MEM_CLOCK":
			gpu.MemClockMHz = m.Value
		}

		// Extract GPU name/UUID from labels
		if uuid, ok := m.Labels["UUID"]; ok && gpu.UUID == "" {
			gpu.UUID = uuid
		}
		if name, ok := m.Labels["modelName"]; ok && gpu.Name == "" {
			gpu.Name = name
		}
	}

	// Convert map to sorted slice and compute derived metrics
	result := make([]GPUMetrics, 0, len(gpuMap))
	for _, gpu := range gpuMap {
		if gpu.MemoryTotalBytes > 0 {
			gpu.MemoryUsagePct = (gpu.MemoryUsedBytes / gpu.MemoryTotalBytes) * 100
		}
		result = append(result, *gpu)
	}

	return result, nil
}

// ============================================================================
// Prometheus node_exporter Scraping
// ============================================================================

// collectNodeExporter scrapes node_exporter and parses system metrics
func (c *Collector) collectNodeExporter() (*NodeMetrics, error) {
	metrics, err := c.scrapePrometheus(c.nodeExporterURL)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape node_exporter: %w", err)
	}

	node := &NodeMetrics{}

	// Aggregate CPU seconds to compute usage
	var cpuIdleTotal float64
	var cpuTotal float64
	var cpuCount int

	for _, m := range metrics {
		switch m.Name {
		case "node_cpu_seconds_total":
			mode := m.Labels["mode"]
			cpuTotal += m.Value
			if mode == "idle" {
				cpuIdleTotal += m.Value
			}
			if mode == "user" {
				// Count CPUs by distinct "cpu" label
				if _, ok := m.Labels["cpu"]; ok {
					cpuCount++
				}
			}
		case "node_memory_MemTotal_bytes":
			node.MemoryTotalBytes = m.Value
		case "node_memory_MemFree_bytes":
			node.MemoryFreeBytes = m.Value
		case "node_memory_MemAvailable_bytes":
			// Prefer MemAvailable over MemFree
			if node.MemoryFreeBytes == 0 {
				node.MemoryFreeBytes = m.Value
			}
		case "node_filesystem_size_bytes":
			// Sum root filesystem
			if m.Labels["mountpoint"] == "/" {
				node.DiskTotalBytes = m.Value
			}
		case "node_filesystem_avail_bytes":
			if m.Labels["mountpoint"] == "/" {
				node.DiskFreeBytes = m.Value
			}
		case "node_network_receive_bytes_total":
			if m.Labels["device"] != "lo" {
				node.NetworkRxBytes += m.Value
			}
		case "node_network_transmit_bytes_total":
			if m.Labels["device"] != "lo" {
				node.NetworkTxBytes += m.Value
			}
		case "node_load1":
			node.LoadAvg1 = m.Value
		case "node_load5":
			node.LoadAvg5 = m.Value
		case "node_load15":
			node.LoadAvg15 = m.Value
		case "node_boot_time_seconds":
			node.UptimeSeconds = float64(time.Now().Unix()) - m.Value
		}
	}

	// Calculate derived metrics
	if cpuTotal > 0 {
		node.CPUUsagePct = (1.0 - cpuIdleTotal/cpuTotal) * 100
	}
	node.CPUCores = cpuCount
	if node.MemoryTotalBytes > 0 {
		node.MemoryUsagePct = (1.0 - node.MemoryFreeBytes/node.MemoryTotalBytes) * 100
	}
	if node.DiskTotalBytes > 0 {
		node.DiskUsagePct = (1.0 - node.DiskFreeBytes/node.DiskTotalBytes) * 100
	}

	return node, nil
}

// ============================================================================
// Prometheus Text Format Parser
// ============================================================================

// prometheusMetric represents a single parsed Prometheus metric line
type prometheusMetric struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// scrapePrometheus fetches and parses a Prometheus text exposition endpoint
func (c *Collector) scrapePrometheus(url string) ([]prometheusMetric, error) {
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return parsePrometheusText(resp.Body)
}

// parsePrometheusText parses Prometheus text exposition format
func parsePrometheusText(r io.Reader) ([]prometheusMetric, error) {
	var metrics []prometheusMetric
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		m, err := parseMetricLine(line)
		if err != nil {
			continue // skip malformed lines
		}
		metrics = append(metrics, *m)
	}

	return metrics, scanner.Err()
}

// parseMetricLine parses a single Prometheus metric line
// Format: metric_name{label1="val1",label2="val2"} value [timestamp]
func parseMetricLine(line string) (*prometheusMetric, error) {
	m := &prometheusMetric{
		Labels: make(map[string]string),
	}

	// Check for labels
	braceStart := strings.IndexByte(line, '{')
	braceEnd := strings.IndexByte(line, '}')

	if braceStart > 0 && braceEnd > braceStart {
		m.Name = line[:braceStart]

		// Parse labels
		labelStr := line[braceStart+1 : braceEnd]
		for _, pair := range splitLabels(labelStr) {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				m.Labels[key] = val
			}
		}

		// Parse value (after closing brace)
		valueStr := strings.TrimSpace(line[braceEnd+1:])
		// Remove optional timestamp
		if spaceIdx := strings.IndexByte(valueStr, ' '); spaceIdx > 0 {
			valueStr = valueStr[:spaceIdx]
		}
		v, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid value: %s", valueStr)
		}
		m.Value = v
	} else {
		// No labels
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid metric line")
		}
		m.Name = parts[0]
		v, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid value: %s", parts[1])
		}
		m.Value = v
	}

	return m, nil
}

// splitLabels splits a Prometheus label string, handling quoted values with commas
func splitLabels(s string) []string {
	var result []string
	var current strings.Builder
	inQuote := false

	for _, c := range s {
		switch c {
		case '"':
			inQuote = !inQuote
			current.WriteRune(c)
		case ',':
			if inQuote {
				current.WriteRune(c)
			} else {
				result = append(result, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(c)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}
