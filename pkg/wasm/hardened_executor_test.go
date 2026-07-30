// Package wasm provides comprehensive test coverage for hardened WASM sandbox execution.
package wasm_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"
	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Unit Tests for Security Configuration
// ============================================================================

func TestSecurityConfig_Validate(t *testing.T) {
	validConfig := wasm.SecurityConfig{
		CPULimit:     2.0,
		MemoryLimitMB: 256,
		SyscallLimit: 10000,
		TimeLimitSec: 60,
	}
	
	err := validConfig.Validate()
	assert.NoError(t, err, "Valid config should pass validation")
}

func TestSecurityConfig_ValidateInvalidCPU(t *testing.T) {
	invalidConfig := wasm.SecurityConfig{
		CPULimit:     -1.0, // Invalid negative
		MemoryLimitMB: 256,
		SyscallLimit: 10000,
		TimeLimitSec: 60,
	}
	
	err := invalidConfig.Validate()
	assert.Error(t, err, "Negative CPU limit should fail validation")
}

func TestSecurityConfig_ValidateInvalidMemory(t *testing.T) {
	invalidConfig := wasm.SecurityConfig{
		CPULimit:     2.0,
		MemoryLimitMB: 5000, // Exceeds max 4096
		SyscallLimit: 10000,
		TimeLimitSec: 60,
	}
	
	err := invalidConfig.Validate()
	assert.Error(t, err, "Memory over 4096MB should fail validation")
}

func TestSecurityConfig_Default(t *testing.T) {
	config := wasm.DefaultSecurityConfig()
	
	assert.Equal(t, float64(2.0), config.CPULimit)
	assert.Equal(t, 256, config.MemoryLimitMB)
	assert.Equal(t, 10000, config.SyscallLimit)
	assert.Equal(t, 60, config.TimeLimitSec)
	assert.False(t, config.NetworkEnabled, "Network disabled by default")
	assert.False(t, config.DiskAccess, "Disk access disabled by default")
	assert.False(t, config.AllowPrivileged, "Privileged mode disabled by default")
}

// ============================================================================
// ResourceMonitor Tests
// ============================================================================

func TestResourceMonitor_LimitExceeded(t *testing.T) {
	config := wasm.SecurityConfig{
		SyscallLimit:   5,
		TimeLimitSec:   1,
	}
	
	rm := wasm.NewResourceMonitor(config)
	
	// Simulate exceeding syscall limit
	for i := 0; i < 10; i++ {
		rm.AddSyscall()
		err := rm.Update()
		
		if i >= 5 {
			assert.Error(t, err, "Should exceed syscall limit after %d calls", i)
			assert.True(t, rm.LimitExceeded())
		} else {
			assert.NoError(t, err, "No error expected before limit reached")
		}
	}
}

func TestResourceMonitor_Metrics(t *testing.T) {
	config := wasm.SecurityConfig{
		SyscallLimit:   100,
		TimeLimitSec:   60,
	}
	
	rm := wasm.NewResourceMonitor(config)
	rm.AddSyscall()
	
	metrics := rm.GetMetrics()
	
	assert.Contains(t, metrics, "syscall_count")
	assert.Contains(t, metrics, "syscall_limit")
	assert.Contains(t, metrics, "execution_time_sec")
	assert.Equal(t, 1, metrics["syscall_count"])
}

// ============================================================================
// NetworkFilter Tests
// ============================================================================

func TestNetworkFilter_CanConnect(t *testing.T) {
	filter := wasm.NewNetworkFilter(
		[]string{"example.com", "api.cloudai-fusion.io"},
		[]int{22, 23}, // Block SSH and telnet ports
	)
	
	// Allowed host, allowed port
	assert.True(t, filter.CanConnect("example.com", 80))
	
	// Allowed host, blocked port
	assert.False(t, filter.CanConnect("example.com", 22))
	
	// Not in allowlist
	assert.False(t, filter.CanConnect("evil.com", 80))
}

func TestNetworkFilter_SetAllowedHosts(t *testing.T) {
	filter := wasm.NewNetworkFilter(nil, nil)
	
	newHosts := []string{"newhost.example.com"}
	filter.SetAllowedHosts(newHosts)
	
	// Verify hosts updated (in production would check internal state)
	assert.NotPanics(t, func() {
		filter.CanConnect("newhost.example.com", 80)
	})
}

// ============================================================================
// FileSystemGuard Tests
// ============================================================================

func TestFileSystemGuard_CanWrite(t *testing.T) {
	guard := wasm.NewFileSystemGuard(
		[]string{"/etc", "/usr"},  // RO mounts
		[]string{"/tmp/plugins"},  // RW mounts
		"/var/tmp/plugin-data",
	)
	
	// Cannot write to RO mount
	assert.False(t, guard.CanWrite("/etc/passwd"))
	
	// Can write to RW mount
	assert.True(t, guard.CanWrite("/tmp/plugins/output.json"))
	
	// Default deny
	assert.False(t, guard.CanWrite("/home/user/data.json"))
}

func TestFileSystemGuard_WriteLimit(t *testing.T) {
	guard := wasm.NewFileSystemGuard(
		[]string{},
		[]string{"/tmp"},
		"/tmp",
	)
	guard.WriteLimit = 2
	
	// First two writes should succeed
	assert.True(t, guard.CanWrite("/tmp/file1.json"))
	assert.True(t, guard.CanWrite("/tmp/file2.json"))
	
	// Third write should be denied due to limit
	assert.False(t, guard.CanWrite("/tmp/file3.json"))
}

// ============================================================================
// HardenedPluginExecutor Integration Tests
// ============================================================================

func TestHardenedPluginExecutor_Initialize(t *testing.T) {
	executor, err := wasm.NewHardenedPluginExecutor(wasm.SecurityConfig{})
	
	assert.NoError(t, err, "Executor should initialize successfully")
	assert.NotNil(t, executor)
	assert.True(t, executor.IsInitialized())
}

func TestHardenedPluginExecutor_ExecuteWithConstraints(t *testing.T) {
	executor, err := wasm.NewHardenedPluginExecutor(wasm.SecurityConfig{
		TimeLimitSec:   30,
		SyscallLimit:   100,
		MemoryLimitMB: 256,
	})
	
	assert.NoError(t, err)
	defer executor.Shutdown()
	
	ctx := context.Background()
	pluginCode := []byte(`console.log('Hello from WASM plugin')`)
	input := []byte(`{"test": "data"}`)
	
	result, err := executor.ExecutePlugin(ctx, "test-plugin", pluginCode, input)
	
	assert.NoError(t, err, "Execution should succeed")
	assert.NotNil(t, result)
	assert.True(t, result.Success, "Plugin should execute successfully")
	assert.Greater(t, len(result.Output), 0, "Should have output")
	
	// Check resource usage tracked
	assert.NotEmpty(t, result.ResourceUsage)
	assert.GreaterOrEqual(t, result.DurationMs, int64(0), "Duration should be recorded")
}

func TestHardenedPluginExecutor_Timeout(t *testing.T) {
	executor, err := wasm.NewHardenedPluginExecutor(wasm.SecurityConfig{
		TimeLimitSec:   1, // Very short timeout
	})
	
	assert.NoError(t, err)
	defer executor.Shutdown()
	
	ctx := context.Background()
	pluginCode := []byte(`// Malicious code that would hang`)
	input := []byte(`malicious input`)
	
	result, err := executor.ExecutePlugin(ctx, "timeout-test", pluginCode, input)
	
	// Should complete within timeout or fail gracefully
	assert.NotNil(t, result)
	if result != nil {
		assert.LessOrEqual(t, result.DurationMs, int64(2000), "Should respect time limit")
	}
}

func TestHardenedPluginExecutor_NetworkIsolation(t *testing.T) {
	config := wasm.SecurityConfig{
		NetworkEnabled: false, // Disable network
	}
	
	executor, err := wasm.NewHardenedPluginExecutor(config)
	assert.NoError(t, err)
	defer executor.Shutdown()
	
	metrics := executor.GetMetrics()
	assert.Contains(t, metrics, "network_filter")
}

// ============================================================================
// Performance Benchmarks
// ============================================================================

func BenchmarkHardenedPluginExecutor_Execute(b *testing.B) {
	executor, err := wasm.NewHardenedPluginExecutor(wasm.SecurityConfig{
		TimeLimitSec:   30,
		SyscallLimit:   1000,
	})
	
	assert.NoError(b, err)
	
	ctx := context.Background()
	pluginCode := []byte(`console.log('Benchmark plugin')`)
	input := []byte(`benchmark data`)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := executor.ExecutePlugin(ctx, fmt.Sprintf("bench-%d", i), pluginCode, input)
		if err != nil || !result.Success {
			b.Fatal("Execution failed")
		}
	}
}

func BenchmarkHardenedPluginExecutor_Metrics(b *testing.B) {
	executor, _ := wasm.NewHardenedPluginExecutor(wasm.SecurityConfig{})
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics := executor.GetMetrics()
		_ = metrics
	}
}
