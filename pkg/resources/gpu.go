// Package resources provides hardware resource management, primarily GPU visibility
package resources

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var ErrGPUUnavailable = errors.New("no GPUs found")

// GPUMetric contains real-time GPU telemetry
type GPUMetric struct {
	ID           int     `json:"gpu_id"`
	Name         string  `json:"name"`
	Utility      float64 `json:"utilization_percent"` // 0-100
	MemoryUsed   uint64  `json:"memory_used_mb"`
	MemoryTotal  uint64  `json:"memory_total_mb"`
	MemoryFree   uint64  `json:"memory_free_mb"`
	Temperature  int     `json:"temperature_celsius"`
	PowerWatts   float64 `json:"power_watts"`
	FanSpeed     int     `json:"fan_speed_percent"`
	State        string  `json:"state"`
}

// MIGTopology describes NVIDIA MIG (Multi-Instance GPU) configuration
type MIGTopology struct {
	GPUID          int      `json:"gpu_id"`
	MemoryMB       int      `json:"total_memory_mb"`
	Enabled        bool     `json:"mig_enabled"`
	Slices         []MIGSlice `json:"slices,omitempty"`
}

// MIGSlice represents a single MIG slice partition
type MIGSlice struct {
	ID            int    `json:"slice_id"`
	Name          string `json:"name"`
	MemoryMB      int    `json:"memory_mb"`
	CUDACompute   int    `json:"cuda_compute"`
	MemoryLocked  bool   `json:"memory_locked"`
	AllowedPIDs   []int  `json:"allowed_pids,omitempty"`
}

// GPUCollector queries and aggregates GPU metrics
type GPUCollector struct {
	logger func(format string, args ...interface{})
}

// NewGPUCollector creates a new GPU collector
func NewGPUCollector() *GPUCollector {
	return &GPUCollector{
		logger: func(format string, args ...interface{}) {},
	}
}

// SetLogger sets custom logging function
func (c *GPUCollector) SetLogger(log func(format string, args ...interface{})) {
	c.logger = log
}

// CollectGPUMetrics retrieves current metrics from all available GPUs
func (c *GPUCollector) CollectGPUMetrics(ctx context.Context) ([]GPUMetric, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=index,name,memory.total,memory.used,memory.free,utilization.gpu,utilization.memory,power.usage,temperature.gpu,fan.speed", "--format=csv,noheader,nounits").Output()
	if err != nil {
		c.logger("Failed to query nvidia-smi: %v", err)
		return nil, err
	}

	return c.ParseNvidiaSMI(string(output))
}

// ParseNvidiaSMI parses raw nvidia-smi output into structured GPUMetrics
func (c *GPUCollector) ParseNvidiaSMI(output string) ([]GPUMetric, error) {
	var metrics []GPUMetric

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return nil, ErrGPUUnavailable
	}

	for _, line := range lines {
		parts := strings.Split(line, ", ")
		if len(parts) < 10 {
			continue
		}

		// Skip the CSV header row: the first field of a data row is a numeric index.
		idx, idxErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		if idxErr != nil {
			continue
		}

		// Handle case with or without fan speed (sometimes nvidia-smi omits fan)
		fanSpeed := ""
		if len(parts) > 10 {
			fanSpeed = parts[10]
		} else {
			// Use previous field or default if missing
			fanSpeed = "0"
		}

		metric := GPUMetric{
			ID:         idx,
			Name:       strings.TrimSpace(parts[1]),
			Utility:    parseFloat64(parts[5]),
			MemoryUsed: parseMB(parts[3]),
			MemoryTotal: parseMB(parts[2]),
			MemoryFree: parseMB(parts[4]),
			Temperature: parseInt(parts[8]),
			PowerWatts: parseFloat64(parts[9]),
			FanSpeed:   parseInt(fanSpeed),
			State:      "ready",
		}

		metrics = append(metrics, metric)
	}

	c.logger("Collected metrics for %d GPUs", len(metrics))
	return metrics, nil
}

// DiscoverMIGTopology checks if MIG is enabled on GPUs
func (c *GPUCollector) DiscoverMIGTopology(ctx context.Context, gpuIDs []int) ([]MIGTopology, error) {
	topologies := make([]MIGTopology, 0)

	for _, gpuID := range gpuIDs {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		
		// Query MIG status
		statusOut, _ := exec.CommandContext(ctx, "nvidia-smi", "-i", fmt.Sprintf("%d", gpuID), "-L").Output()
		cancel()

		t := MIGTopology{
			GPUID: gpuID,
		}

		if strings.Contains(string(statusOut), "compute mode has been changed to") {
			t.Enabled = true
			t.MemoryMB = 12288 // Default assumption for testing
		} else {
			t.Enabled = false
		}

		topologies = append(topologies, t)
	}

	return topologies, nil
}

// parseMB parses a size string like "12345 MB" into megabytes
func parseMB(s string) uint64 {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 4 {
		return 0
	}
	var mb int
	_, _ = fmt.Sscanf(trimmed[:len(trimmed)-3], "%d", &mb)
	return uint64(mb * 1024 * 1024)
}

// parseFloat64 parses a float string
func parseFloat64(s string) float64 {
	var f float64
	fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f
}

// parseInt parses an integer string
func parseInt(s string) int {
	var i int
	fmt.Sscanf(strings.TrimSpace(s), "%d", &i)
	return i
}
