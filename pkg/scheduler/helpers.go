package scheduler

import (
	"strconv"
	"strings"
)

// ============================================================================
// Utility Functions
// ============================================================================

// parseK8sQuantity parses Kubernetes resource quantities (e.g., "128Gi", "32000Mi").
func parseK8sQuantity(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "Ti"):
		s = strings.TrimSuffix(s, "Ti")
		multiplier = 1024 * 1024 * 1024 * 1024
	case strings.HasSuffix(s, "Gi"):
		s = strings.TrimSuffix(s, "Gi")
		multiplier = 1024 * 1024 * 1024
	case strings.HasSuffix(s, "Mi"):
		s = strings.TrimSuffix(s, "Mi")
		multiplier = 1024 * 1024
	case strings.HasSuffix(s, "Ki"):
		s = strings.TrimSuffix(s, "Ki")
		multiplier = 1024
	}
	val, _ := strconv.ParseInt(s, 10, 64)
	return val * multiplier
}

// estimateNodeCost estimates hourly cost based on GPU type and count.
func estimateNodeCost(gpuCount int, gpuType string) float64 {
	pricePerGPU := 3.0 // base
	switch {
	case strings.Contains(gpuType, "h100"):
		pricePerGPU = 12.0
	case strings.Contains(gpuType, "a100"):
		pricePerGPU = 8.5
	case strings.Contains(gpuType, "a10"):
		pricePerGPU = 2.85
	case strings.Contains(gpuType, "v100"):
		pricePerGPU = 4.5
	case strings.Contains(gpuType, "l40"):
		pricePerGPU = 5.2
	}
	if gpuCount == 0 {
		return 1.0 // CPU-only node
	}
	return pricePerGPU * float64(gpuCount)
}

// generateGPUIndices creates a sequential slice of GPU indices [0, 1, ..., count-1].
func generateGPUIndices(count int) []int {
	indices := make([]int, count)
	for i := 0; i < count; i++ {
		indices[i] = i
	}
	return indices
}
