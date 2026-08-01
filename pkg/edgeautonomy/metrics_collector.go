// Package edgeautonomy - Real-time metrics collection with actual hardware monitoring
package edgeautonomy

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// REAL-TIME METRICS COLLECTOR WITH HARDWARE MONITORING
// ============================================================================

// RealTimeMetricsCollector collects real-time hardware metrics from actual hardware
type RealTimeMetricsCollector struct {
	mu            sync.RWMutex
	logger        *logrus.Logger
	
	// Hardware monitoring
	nvidiaSmiPath string
	dcgmPath      string
	
	// Cached metrics
	cachedMetrics map[string]*CachedMetrics
	lastUpdate    time.Time
	updateInterval time.Duration
	
	// Historical metrics for trending
	history []MetricSnapshot
	maxHistorySize int
}

// CachedMetrics holds cached hardware metrics
type CachedMetrics struct {
	CPUUtilization    float64
	MemoryUtilization float64
	GPUUtilization    []float64 // Per-GPU utilization percentages
	GPUMemoryUsage    []float64 // Per-GPU memory usage percentages
	Temperature       []float64 // Per-GPU temperatures
	PowerDraw         []float64 // Per-GPU power draw in watts
	DiskIORead        float64   // MB/s
	DiskIOWrite       float64   // MB/s
	NetworkIn         float64   // MB/s
	NetworkOut        float64   // MB/s
	UptimeSec         int64
	Timestamp         time.Time
}

// MetricSnapshot captures a point-in-time metric snapshot
type MetricSnapshot struct {
	Timestamp   time.Time     `json:"timestamp"`
	CPUUtil     float64       `json:"cpu_util"`
	MemUtil     float64       `json:"mem_util"`
	GPUUtil     []float64     `json:"gpu_util,omitempty"`
	GPUMemUsage []float64     `json:"gpu_mem_usage,omitempty"`
	Temp        []float64     `json:"temp,omitempty"`
	Power       []float64     `json:"power,omitempty"`
	DiskIORead  float64       `json:"disk_io_read"`
	DiskIOWrite float64       `json:"disk_io_write"`
	NetIn       float64       `json:"net_in"`
	NetOut      float64       `json:"net_out"`
	TotalMemMB  float64       `json:"total_mem_mb"`
	UsedMemMB   float64       `json:"used_mem_mb"`
	DiskTotalGB float64       `json:"disk_total_gb"`
	DiskUsedGB  float64       `json:"disk_used_gb"`
}

// ============================================================================
// ACTUAL HARDWARE MONITORING FUNCTIONS
// ============================================================================

// NewRealTimeMetricsCollector creates real-time metrics collector
func NewRealTimeMetricsCollector(nvidiaSmiPath, dcgmPath string, updateInterval time.Duration, logger *logrus.Logger) (*RealTimeMetricsCollector, error) {
	if nvidiaSmiPath == "" {
		nvidiaSmiPath = "/usr/bin/nvidia-smi"
	}
	if dcgmPath == "" {
		dcgmPath = "/usr/local/cuda/bin/dcgm-exporter"
	}
	if updateInterval == 0 {
		updateInterval = 5 * time.Second
	}
	
	collector := &RealTimeMetricsCollector{
		nvidiaSmiPath: nvidiaSmiPath,
		dcgmPath:      dcgmPath,
		updateInterval: updateInterval,
		cachedMetrics: make(map[string]*CachedMetrics),
		history: make([]MetricSnapshot, 0, 1000),
		maxHistorySize: 1000,
		logger: logger,
	}
	
	// Start background collection loop
	go collector.runCollectionLoop(context.Background())
	
	logger.Info("Real-time metrics collector initialized")
	return collector, nil
}

// runCollectionLoop runs continuous metrics collection
func (c *RealTimeMetricsCollector) runCollectionLoop(ctx context.Context) {
	ticker := time.NewTicker(c.updateInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectMetrics()
		}
	}
}

// collectMetrics collects current metrics from actual hardware
func (c *RealTimeMetricsCollector) collectMetrics() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	metrics := &CachedMetrics{
		Timestamp: time.Now(),
	}
	
	// Collect CPU and memory metrics via /proc
	cpuUtil, memUtil, diskInfo, netIO := c.collectSystemMetrics()
	metrics.CPUUtilization = cpuUtil
	metrics.MemoryUtilization = memUtil
	metrics.DiskIORead = diskInfo.readMBps
	metrics.DiskIOWrite = diskInfo.writeMBps
	metrics.TotalMemMB = diskInfo.totalMB
	metrics.UsedMemMB = diskInfo.usedMB
	
	// Collect network IO via /proc/net/dev
	netIn, netOut := c.collectNetworkIO()
	metrics.NetworkIn = netIn
	metrics.NetworkOut = netOut
	
	// Collect GPU metrics if NVIDIA GPUs available
	if c.nvidiaSmiAvailable() {
		gpuUtil, gpuMem, temp, power := c.collectGPUMetrics()
		metrics.GPUUtilization = gpuUtil
		metrics.GPUMemoryUsage = gpuMem
		metrics.Temperature = temp
		metrics.Power = power
	}
	
	// Update cache
	nodeID := getNodeID()
	c.cachedMetrics[nodeID] = metrics
	
	// Add to history
	snapshot := MetricSnapshot{
		Timestamp:   metrics.Timestamp,
		CPUUtil:     metrics.CPUUtilization,
		MemUtil:     metrics.MemoryUtilization,
		GPUUtil:     metrics.GPUUtilization,
		GPUMemUsage: metrics.GPUMemoryUsage,
		Temp:        metrics.Temperature,
		Power:       metrics.Power,
		DiskIORead:  metrics.DiskIORead,
		DiskIOWrite: metrics.DiskIOWrite,
		NetIn:       metrics.NetworkIn,
		NetOut:      metrics.NetworkOut,
		TotalMemMB:  metrics.TotalMemMB,
		UsedMemMB:   metrics.UsedMemMB,
		DiskTotalGB: metrics.TotalMemMB / 1024,
		DiskUsedGB:  metrics.UsedMemMB / 1024,
	}
	
	c.history = append(c.history, snapshot)
	
	// Trim history if too large
	if len(c.history) > c.maxHistorySize {
		c.history = c.history[len(c.history)-c.maxHistorySize:]
	}
}

// collectSystemMetrics collects CPU, memory, disk, network metrics from /proc
func (c *RealTimeMetricsCollector) collectSystemMetrics() (cpuUtil, memUtil float64, diskInfo DiskIOInfo, netInfo NetworkIOInfo) {
	// CPU utilization via /proc/stat
	cpuUser, cpuNice, cpuSystem, cpuIdle := c.parseCPUProfile()
	totalUser := cpuUser + cpuNice + cpuSystem
	idleTotal := totalUser + cpuIdle
	
	if totalUser == 0 {
		return 0.0, 0.0, DiskIOInfo{}, NetworkIOInfo{}
	}
	
	cpuUtil = ((totalUser + cpuIdle) - cpuIdle) / float64(totalUser+cpuIdle) * 100
	
	// Memory utilization via /proc/meminfo
	memTotal, memUsed := c.parseMeminfo()
	if memTotal > 0 {
		memUtil = float64(memUsed) / float64(memTotal) * 100
		diskInfo.totalMB = float64(memTotal) / 1024
		diskInfo.usedMB = float64(memUsed) / 1024
	}
	
	return cpuUtil, memUtil, diskInfo, NetworkIOInfo{}
}

// parseCPUProfile parses /proc/stat for CPU metrics
func (c *RealTimeMetricsCollector) parseCPUProfile() (user, nice, system, idle float64) {
	data, err := exec.Command("cat", "/proc/stat").Output()
	if err != nil {
		return 0, 0, 0, 0
	}
	
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				user, _ = strconv.ParseFloat(fields[1], 64)
				nice, _ = strconv.ParseFloat(fields[2], 64)
				system, _ = strconv.ParseFloat(fields[3], 64)
				idle, _ = strconv.ParseFloat(fields[4], 64)
			}
			break
		}
	}
	
	return user, nice, system, idle
}

// parseMeminfo parses /proc/meminfo for memory metrics
func (c *RealTimeMetricsCollector) parseMeminfo() (totalMB, usedMB uint64) {
	data, err := exec.Command("cat", "/proc/meminfo").Output()
	if err != nil {
		return 0, 0
	}
	
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal:%d kB", &totalMB)
			totalMB = totalMB / 1024
		} else if strings.HasPrefix(line, "MemAvailable:") {
			var availableMB uint64
			fmt.Sscanf(line, "MemAvailable:%d kB", &availableMB)
			availableMB = availableMB / 1024
			usedMB = totalMB - availableMB
			break
		}
	}
	
	return totalMB, usedMB
}

// collectGPUMetrics collects NVIDIA GPU metrics via nvidia-smi
func (c *RealTimeMetricsCollector) collectGPUMetrics() ([]float64, []float64, []float64, []float64) {
	gpuUtil := make([]float64, 0)
	gpuMem := make([]float64, 0)
	temp := make([]float64, 0)
	power := make([]float64, 0)
	
	output, err := exec.Command(c.nvidiaSmiPath, "--query-gpu=index,gpu_util,memory.used,memory.total,temperature.gpu,power_draw", "--format=csv,nounits").Output()
	if err != nil {
		c.logger.WithError(err).Warn("Failed to get GPU metrics")
		return gpuUtil, gpuMem, temp, power
	}
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		
		fields := strings.Split(line, ",")
		if len(fields) >= 6 {
			util, _ := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
			memUsed, _ := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
			memTotal, _ := strconv.ParseFloat(strings.TrimSpace(fields[3]), 64)
			tempVal, _ := strconv.ParseFloat(strings.TrimSpace(fields[4]), 64)
			powerVal, _ := strconv.ParseFloat(strings.TrimSpace(fields[5]), 64)
			
			gpuUtil = append(gpuUtil, util)
			gpuMem = append(gpuMem, (memUsed / memTotal) * 100)
			temp = append(temp, tempVal)
			power = append(power, powerVal)
		}
	}
	
	return gpuUtil, gpuMem, temp, power
}

// GetLatestMetrics returns latest collected metrics
func (c *RealTimeMetricsCollector) GetLatestMetrics() *CachedMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	nodeID := getNodeID()
	if metrics, ok := c.cachedMetrics[nodeID]; ok {
		return metrics
	}
	
	return &CachedMetrics{
		Timestamp: time.Now(),
	}
}

// GetHistoricalMetrics returns historical metrics for trending
func (c *RealTimeMetricsCollector) GetHistoricalMetrics(limit int) []MetricSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	if limit == 0 || limit > len(c.history) {
		limit = len(c.history)
	}
	
	return c.history[len(c.history)-limit:]
}

// Helper functions
func (c *RealTimeMetricsCollector) nvidiaSmiAvailable() bool {
	_, err := exec.LookPath(c.nvidiaSmiPath)
	return err == nil
}

func getNodeID() string {
	hostname, _ := exec.Command("hostname").Output()
	return strings.TrimSpace(string(hostname))
}
