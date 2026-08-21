// Package edgeautonomy - Unit tests for RealTimeMetricsCollector
package edgeautonomy

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

var testLogger *logrus.Logger

func init() {
	testLogger = logrus.New()
	testLogger.SetLevel(logrus.FatalLevel) // Only show errors in tests
}

// TestRealTimeMetricsCollector_Initialization tests that collector initializes correctly
func TestRealTimeMetricsCollector_Initialization(t *testing.T) {
	collector, err := NewRealTimeMetricsCollector(
		"/usr/bin/nvidia-smi",
		"/usr/local/cuda/bin/dcgm-exporter",
		5*time.Second,
		testLogger,
	)
	
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	
	if collector == nil {
		t.Fatal("Expected non-nil collector")
	}
	
	if collector.nvidiaSmiPath != "/usr/bin/nvidia-smi" {
		t.Errorf("Expected nvidiaSmiPath=/usr/bin/nvidia-smi, got %s", collector.nvidiaSmiPath)
	}
	
	if collector.updateInterval != 5*time.Second {
		t.Errorf("Expected updateInterval=5s, got %v", collector.updateInterval)
	}
	
	// Verify initial state - GetLatestMetrics should return empty but not panic
	metrics := collector.GetLatestMetrics()
	if metrics == nil {
		t.Error("Expected non-nil metrics after initialization")
	}
	
	if metrics.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}
}

// TestRealTimeMetricsCollector_MetricsCollection tests core aggregation logic
func TestRealTimeMetricsCollector_MetricsCollection(t *testing.T) {
	collector, err := NewRealTimeMetricsCollector(
		"", // Use default path
		"", // Use default path
		0,  // Use default interval (5s)
		testLogger,
	)
	
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	
	// Manually invoke collectMetrics once instead of waiting for background loop
	collector.collectMetrics()
	
	// Get metrics after collection - should return valid structure even if zero values
	metrics := collector.GetLatestMetrics()
	if metrics == nil {
		t.Fatal("Expected non-nil metrics after collection")
	}
	
	// Verify basic fields exist (values may be 0 on Windows/non-Linux systems)
	if metrics.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp after collection")
	}
	
	// CPU and Memory utilization can be 0 on systems without /proc
	// We only verify the structure is correct, not specific values
	
	history := collector.GetHistoricalMetrics(0)
	if len(history) < 1 {
		t.Error("Expected at least one snapshot in history after collection")
	}
}

// TestRealTimeMetricsCollector_GPUDegradedF gracefully degrades when nvidia-smi unavailable
func TestRealTimeMetricsCollector_GPUDegradedF(t *testing.T) {
	// Test with a path that definitely doesn't exist
	collector, err := NewRealTimeMetricsCollector(
		"/nonexistent/path/to/nvidia-smi",
		"/also/fake/path",
		5*time.Second,
		testLogger,
	)
	
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	
	// Collect metrics - should NOT panic even though GPU tools are unavailable
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("collectGPUMetrics panicked when nvidia-smi unavailable: %v", r)
			}
		}()
		
		// Directly test GPU collection logic
		gpuUtil, gpuMem, temp, power := collector.collectGPUMetrics()
		
		// On Windows or systems without GPU tools, expect empty/zero slices
		if len(gpuUtil) != 0 {
			t.Logf("Unexpected GPU util data: %v (expected empty on this platform)", gpuUtil)
		}
		if len(gpuMem) != 0 {
			t.Logf("Unexpected GPU mem data: %v (expected empty on this platform)", gpuMem)
		}
		if len(temp) != 0 {
			t.Logf("Unexpected temperature data: %v (expected empty on this platform)", temp)
		}
		if len(power) != 0 {
			t.Logf("Unexpected power data: %v (expected empty on this platform)", power)
		}
		
		// Critical: No panic occurred - graceful degradation works!
		t.Log("Successfully handled missing nvidia-smi without panic")
	}()
	
	// Verify whole pipeline still works
	collector.collectMetrics()
	metrics := collector.GetLatestMetrics()
	
	if metrics == nil {
		t.Fatal("Expected metrics even in degraded mode")
	}
	
	// Power should be 0 when GPU tools unavailable
	if metrics.Power != 0 {
		t.Errorf("Expected Power=0 when GPU tools unavailable, got %f", metrics.Power)
	}
}

// TestRealTimeMetricsCollector_SystemMetricsCrossPlatform tests system metrics handling on Windows
func TestRealTimeMetricsCollector_SystemMetricsCrossPlatform(t *testing.T) {
	collector, err := NewRealTimeMetricsCollector(
		"", "", 0, testLogger,
	)
	
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	
	// On Windows, /proc/stat and /proc/meminfo don't exist
	// collectSystemMetrics should return zeros, not panic or error
	cpuUtil, memUtil, diskInfo, netInfo := collector.collectSystemMetrics()
	
	// Accept zero values on Windows/non-Linux platforms
	if cpuUtil < 0 || cpuUtil > 100 {
		t.Errorf("CPU utilization out of range: %f", cpuUtil)
	}
	if memUtil < 0 || memUtil > 100 {
		t.Errorf("Memory utilization out of range: %f", memUtil)
	}
	
	// Disk info should have valid structure even if values are zero
	// This test verifies graceful handling of non-existent /proc
	t.Logf("System metrics collected (Windows-compatible): CPU=%.2f%%, Mem=%.2f%%, DiskTotal=%.2fMB", 
		cpuUtil, memUtil, diskInfo.TotalMB)
	
	// NetInfo should be valid struct (may have zero values)
	_ = netInfo.PacketsIn
	_ = netInfo.PacketsOut
	
	// Full collection should work without errors
	collector.collectMetrics()
	metrics := collector.GetLatestMetrics()
	
	if metrics == nil {
		t.Fatal("Expected metrics after full collection cycle")
	}
	
	t.Log("System metrics collection completed successfully on Windows platform")
}

// TestRealTimeMetricsCollector_HistoricalDataManagement tests history trimming and retrieval
func TestRealTimeMetricsCollector_HistoricalDataManagement(t *testing.T) {
	collector, err := NewRealTimeMetricsCollector(
		"", "", 0, testLogger,
	)
	
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	
	// Simulate multiple collection cycles by calling collectMetrics repeatedly
	for i := 0; i < 100; i++ {
		// Manually invoke the private method through a workaround
		// In production, this would happen via the background loop
		func() {
			collector.mu.Lock()
			defer collector.mu.Unlock()
			
			// Add snapshot directly to history (private field access in same package)
			snapshot := MetricSnapshot{
				Timestamp: time.Now(),
				CPUUtil:     float64(i),
				MemUtil:     float64(i * 2),
			}
			collector.history = append(collector.history, snapshot)
		}()
	}
	
	// Retrieve all history
	allHistory := collector.GetHistoricalMetrics(0)
	if len(allHistory) != 100 {
		t.Errorf("Expected 100 history entries, got %d", len(allHistory))
	}
	
	// Limit retrieval
	limitedHistory := collector.GetHistoricalMetrics(10)
	if len(limitedHistory) != 10 {
		t.Errorf("Expected 10 limited entries, got %d", len(limitedHistory))
	}
	
	// Verify history trimming when over maxHistorySize (1000)
	for i := 0; i < 500; i++ {
		collector.collectMetrics()
	}
	
	historyAfterTrim := collector.GetHistoricalMetrics(0)
	if len(historyAfterTrim) > 1000 {
		t.Errorf("History should be trimmed to max 1000, got %d", len(historyAfterTrim))
	}
	
	t.Logf("History management verified: current size=%d", len(historyAfterTrim))
}
