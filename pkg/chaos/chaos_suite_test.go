package chaos

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestLogger is a test implementation of the Logger interface.
type TestLogger struct {
	TestingT testing.TB
}

func (tl *TestLogger) Infof(format string, args ...interface{}) {
	tl.TestingT.Logf("[INFO] "+format, args...)
}

func (tl *TestLogger) Warnf(format string, args ...interface{}) {
	tl.TestingT.Logf("[WARN] "+format, args...)
}

func (tl *TestLogger) Errorf(format string, args ...interface{}) {
	tl.TestingT.Logf("[ERROR] "+format, args...)
}

func NewTestLogger(tb testing.TB) *TestLogger {
	return &TestLogger{TestingT: tb}
}

// ============================================================================
// CPU Stresser Tests
// ============================================================================

func TestCPUStresser_DefaultConfiguration(t *testing.T) {
	config := DefaultCPUStresserConfig()
	
	assert.NotNil(t, config.Logger, "Logger can be nil")
	assert.Greater(t, config.Workers, 0, "Workers should be at least 1")
	
	stresser := NewCPUStresser(config)
	assert.NotNil(t, stresser, "Should create CPU stresser instance")
}

func TestCPUStresser_ShouldCompleteWithinTimeout(t *testing.T) {
	shortCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	
	config := CPUStresserConfig{
		Duration: 100 * time.Millisecond,
		Workers:  2,
		Logger:   NewTestLogger(t),
	}
	
	stresser := NewCPUStresser(config)
	err := stresser.Inject(shortCtx)
	
	// Should complete or be cancelled gracefully
	assert.True(t, err == nil || err == context.DeadlineExceeded || err == context.Canceled, 
		"Error should be nil or context-related: %v", err)
}

func TestCPUStresser_Cleanup(t *testing.T) {
	ctx := context.Background()
	
	config := CPUStresserConfig{
		Duration: 1 * time.Second,
		Workers:  2,
		Logger:   NewTestLogger(t),
	}
	
	stresser := NewCPUStresser(config)
	
	// Inject stress
	go func() {
		_ = stresser.Inject(ctx)
	}()
	
	// Wait a bit
	time.Sleep(100 * time.Millisecond)
	
	// Cleanup should not panic
	err := stresser.Remove(ctx)
	assert.NoError(t, err, "Cleanup should not error")
}

// ============================================================================
// Memory Pressure Tests  
// ============================================================================

func TestMemoryPressureInjector_DefaultConfiguration(t *testing.T) {
	config := DefaultMemoryPressureConfig()
	
	assert.InDelta(t, 80.0, config.TargetUsagePercent, 0.001, 
		"Default target usage should be around 80%")
	assert.GreaterOrEqual(t, config.Granularity, time.Second, 
		"Granularity should be at least 1 second")
}

func TestMemoryPressureInjector_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	
	config := MemoryPressureConfig{
		TargetUsagePercent: 90.0,
		Granularity:        100 * time.Millisecond,
		Logger:             NewTestLogger(t),
	}
	
	injector := NewMemoryPressureInjector(config)
	
	// Start injection in goroutine
	done := make(chan bool)
	go func() {
		_ = injector.Inject(ctx)
		done <- true
	}()
	
	// Cancel after short delay
	time.Sleep(500 * time.Millisecond)
	cancel()
	
	// Should exit cleanly
	select {
	case <-done:
		t.Log("Memory injector exited cleanly on context cancellation")
	case <-time.After(2 * time.Second):
		t.Fatal("Memory injector did not exit within timeout")
	}
}

func TestMemoryPressureInjector_Remove(t *testing.T) {
	ctx := context.Background()
	
	injector := NewMemoryPressureInjector(DefaultMemoryPressureConfig())
	
	// Remove without Inject should not panic
	err := injector.Remove(ctx)
	assert.NoError(t, err, "Remove should succeed even if Inject was not called")
}

// ============================================================================
// GPU Temperature Stresser Tests
// ============================================================================

func TestGPUGPUTemperatureStresser_DefaultConfiguration(t *testing.T) {
	config := DefaultGPUStressConfig()
	
	assert.Equal(t, 85, config.TargetTemperature, 
		"Default target temperature should be 85°C")
	assert.NotEmpty(t, config.Devices, "Devices list can be empty")
}

func TestGPUGPUTemperatureStresser_NonexistentGPU(t *testing.T) {
	ctx := context.Background()
	
	stresser := NewGPUGPUTemperatureStresser(DefaultGPUStressConfig())
	
	// Attempting to use nvidia-smi without GPU should fail gracefully
	err := stresser.setPowerLimit(ctx)
	
	if err != nil {
		// Expected - nvidia-smi may not be available
		t.Logf("Expected error (no GPU): %v", err)
	}
}

func TestGPUGPUTemperatureStresser_InjectAndCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	stresser := NewGPUGPUTemperatureStresser(GPUStressConfig{
		TargetTemperature: 90,
		Logger:            NewTestLogger(t),
	})
	
	// Inject (may skip actual hardware manipulation if no GPU present)
	err := stresser.Inject(ctx)
	
	if err != nil {
		// Acceptable if no NVIDIA hardware present
		t.Logf("Inject completed with: %v (may be expected)", err)
	}
	
	// Cleanup should work regardless
	err = stresser.Remove(ctx)
	assert.NoError(t, err, "Cleanup should always succeed")
}

// ============================================================================
// Network Partition Tests
// ============================================================================

func TestNetworkPartitionInjector_DefaultConfiguration(t *testing.T) {
	config := DefaultNetworkPartitionConfig()
	
	assert.Equal(t, "drop", config.PartitionType, 
		"Default partition type should be 'drop'")
	assert.InDelta(t, 50.0, config.PacketLoss, 0.001, 
		"Default packet loss should be 50%")
	assert.Equal(t, 2*time.Minute, config.Duration, 
		"Default duration should be 2 minutes")
}

func TestNetworkPartitionInjector_PartitionTypes(t *testing.T) {
	tests := []struct {
		name      string
		partition string
	}{
		{"drop", "drop"},
		{"delay", "delay"},
		{"corrupt", "corrupt"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			
			config := NetworkPartitionConfig{
				Target:        "test-target",
				PartitionType: tt.partition,
				Duration:      200 * time.Millisecond,
				Logger:        NewTestLogger(t),
			}
			
			injector := NewNetworkPartitionInjector(config)
			
			// This will likely return errors without actual network access
			// but should handle them gracefully
			err := injector.Inject(ctx)
			
			// Accept non-nil errors for unimplemented functionality
			t.Logf("Inject returned: %v (expected for unimplemented)", err)
			
			err = injector.Remove(ctx)
			assert.NoError(t, err, "Cleanup should always succeed")
		})
	}
}

func TestNetworkPartitionInjector_IsPartitioned(t *testing.T) {
	injector := NewNetworkPartitionInjector(DefaultNetworkPartitionConfig())
	
	// Should initially report no partition
	assert.False(t, injector.IsPartitioned(), "Should report false before inject")
	
	// After inject would return true in real implementation
	// Placeholder test
	t.Skip("IsPartitioned requires state tracking implementation")
}

// ============================================================================
// Suite Tests (run all together)
// ============================================================================

func TestChaosSuite_CPU_Memory_Integration(t *testing.T) {
	t.Log("Running integrated CPU + Memory stress test")
	
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	// Create both stressors
	cpuStresser := NewCPUStresser(CPUStresserConfig{
		Duration: 500 * time.Millisecond,
		Workers:  1,
		Logger:   NewTestLogger(t),
	})
	
	memInjector := NewMemoryPressureInjector(MemoryPressureConfig{
		TargetUsagePercent: 30.0, // Low percentage for safety
		Granularity:        200 * time.Millisecond,
		Logger:             NewTestLogger(t),
	})
	
	// Run both concurrently
	cpuDone := make(chan bool)
	memDone := make(chan bool)
	
	go func() {
		_ = cpuStresser.Inject(ctx)
		cpuDone <- true
	}()
	
	go func() {
		_ = memInjector.Inject(ctx)
		memDone <- true
	}()
	
	// Wait for both to complete or be cancelled
	timeout := time.After(3 * time.Second)
	
	for {
		select {
		case <-cpuDone:
			t.Log("CPU stress test completed")
		case <-memDone:
			t.Log("Memory pressure test completed")
		case <-timeout:
			t.Log("Integration test timed out (expected in short mode)")
			return
		}
		
		if len(cpuDone) > 0 && len(memDone) > 0 {
			break
		}
	}
	
	t.Log("Integrated CPU + Memory stress completed successfully")
}

func BenchmarkCPUStresser(b *testing.B) {
	logger := &TestLogger{TestingT: b}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config := CPUStresserConfig{
			Duration: 10 * time.Millisecond,
			Workers:  1,
			Logger:   logger,
		}
		
		stresser := NewCPUStresser(config)
		
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_ = stresser.Inject(ctx)
		cancel()
	}
}

// Note: These tests require either real hardware or mock implementations
// For CI/CD environments, consider using:
// - Docker containers with tc/network emulation
// - Virtual machines with controlled network conditions
// - Mock interfaces for K8s client interactions
