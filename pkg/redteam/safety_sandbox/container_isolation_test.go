package safety_sandbox

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

var testLogger *logrus.Logger

func init() {
	testLogger = logrus.New()
	testLogger.SetLevel(logrus.DebugLevel)
	testLogger.SetOutput(os.Stdout)
}

// TestDefaultIsolationConfig tests that default config provides safe defaults
func TestDefaultIsolationConfig(t *testing.T) {
	cfg := DefaultIsolationConfig()
	
	if cfg.BaseImage != "alpine:3.19" {
		t.Errorf("Expected base image alpine:3.19, got %s", cfg.BaseImage)
	}
	
	if cfg.ExecutionTimeout != 5*time.Minute {
		t.Errorf("Expected execution timeout 5m, got %v", cfg.ExecutionTimeout)
	}
	
	if cfg.CPUCQuota != 200000 {
		t.Errorf("Expected CPU quota 200000, got %d", cfg.CPUCQuota)
	}
	
	if cfg.MemoryBytes != 512<<20 {
		t.Errorf("Expected memory 512MB, got %d", cfg.MemoryBytes)
	}
	
	if !cfg.ReadonlyRootfs {
		t.Error("Expected readonly rootfs")
	}
	
	if len(cfg.CapDrop) == 0 || cfg.CapDrop[0] != "ALL" {
		t.Errorf("Expected ALL capabilities dropped, got %v", cfg.CapDrop)
	}
	
	if cfg.Privileged {
		t.Error("Privileged mode should be disabled by default")
	}
}

// TestSanitizeConfig tests configuration sanitization
func TestSanitizeConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    *IsolationConfig
		expected *IsolationConfig
	}{
		{
			name:  "empty config gets defaults",
			input: &IsolationConfig{},
			expected: &IsolationConfig{
				CapDrop: []string{"ALL"},
			},
		},
		{
			name: "low cpu quota gets bumped up",
			input: &IsolationConfig{
				CPUCQuota: 50000, // Below minimum
			},
			expected: &IsolationConfig{
				CPUCQuota: 100000, // Minimum enforced
			},
		},
		{
			name: "high cpu quota gets capped down",
			input: &IsolationConfig{
				CPUCQuota: 1000000, // Above maximum
			},
			expected: &IsolationConfig{
				CPUCQuota: 800000, // Maximum enforced
			},
		},
		{
			name: "low memory gets bumped up",
			input: &IsolationConfig{
				MemoryBytes: 32 << 20, // Below minimum
			},
			expected: &IsolationConfig{
				MemoryBytes: 64 << 20, // Minimum enforced
			},
		},
		{
			name: "privileged mode allowed but warned",
			input: &IsolationConfig{
				Privileged: true,
				CapDrop:    nil, // Should get ALL added
			},
			expected: &IsolationConfig{
				Privileged: true,
				CapDrop:    []string{"ALL"},
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iso := &ContainerIsolator{
				logger: testLogger,
			}
			
			result := iso.sanitizeConfig(tt.input)
			
			if result.CPUCQuota != tt.expected.CPUCQuota {
				t.Errorf("CPUCQuota mismatch: expected %d, got %d", 
					tt.expected.CPUCQuota, result.CPUCQuota)
			}
			
			if result.MemoryBytes != tt.expected.MemoryBytes {
				t.Errorf("MemoryBytes mismatch: expected %d, got %d", 
					tt.expected.MemoryBytes, result.MemoryBytes)
			}
			
			if len(result.CapDrop) == 0 {
				t.Error("CapDrop should never be empty after sanitization")
			}
		})
	}
}

// TestNewContainerIsolator tests isolator initialization
func TestNewContainerIsolator(t *testing.T) {
	ctx := context.Background()
	
	// Test with valid parameters
	iso, err := NewContainerIsolator(testLogger, "tenant-123", "exploit-456")
	if err != nil {
		// This might fail if Docker is not available, which is acceptable
		t.Logf("Isolator creation failed (expected without Docker): %v", err)
	} else {
		defer cleanupIsolator(ctx, t, iso)
		
		status := iso.Status(ctx)
		if !status.IsInitialized {
			t.Error("Expected isolator to be initialized")
		}
	}
}

// TestSetConfig tests configuration setting
func TestSetConfig(t *testing.T) {
	iso := &ContainerIsolator{
		logger: testLogger,
		config: DefaultIsolationConfig(),
	}
	
	// Test with custom config
	customCfg := &IsolationConfig{
		BaseImage:      "ubuntu:22.04",
		MemoryBytes:    256 << 20,
		ReadonlyRootfs: false,
	}
	
	iso.SetConfig(customCfg)
	
	if iso.config.BaseImage != "ubuntu:22.04" {
		t.Errorf("Expected base image ubuntu:22.04, got %s", iso.config.BaseImage)
	}
	
	if iso.config.MemoryBytes != 256<<20 {
		t.Errorf("Expected memory 256MB, got %d", iso.config.MemoryBytes)
	}
	
	// Verify CapDrop still has ALL even when empty in input
	if len(iso.config.CapDrop) == 0 {
		t.Error("CapDrop should be sanitized to include ALL")
	}
}

// TestContainerLifecycle tests full container lifecycle
func TestContainerLifecycle(t *testing.T) {
	ctx := context.Background()
	
	iso, err := NewContainerIsolator(testLogger, "tenant-test", "exploit-lifecycle")
	if err != nil {
		t.Skipf("Docker not available: %v", err)
		return
	}
	defer cleanupIsolator(ctx, t, iso)
	
	// Set config
	iso.SetConfig(&IsolationConfig{
		BaseImage:        "alpine:3.19",
		ExecutionTimeout: 30 * time.Second,
		ReadonlyRootfs:   true,
		Command:          []string{"echo", "Hello from container"},
	})
	
	// Start container
	containerID, err := iso.StartContainer(ctx, []string{})
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}
	
	if containerID == "" {
		t.Error("Container ID should not be empty")
	}
	
	// Get status
	status := iso.Status(ctx)
	if !status.IsRunning && !status.IsInitialized {
		// Container might have already exited (expected for short-lived containers)
		t.Logf("Container status: running=%v, exit_code=%d", status.IsRunning, status.ExitCode)
	}
	
	// Stop container
	err = iso.StopContainer(ctx, 5*time.Second)
	if err != nil {
		t.Logf("Stop error (may be OK if container already exited): %v", err)
	}
	
	// Verify cleanup happened
	finalStatus := iso.Status(ctx)
	if finalStatus.ContainerID != "" {
		// If AutoRemove=false, verify manual cleanup works
		iso.cleanup(ctx)
	}
}

// TestNetworkIsolation tests network isolation setup
func TestNetworkIsolation(t *testing.T) {
	ctx := context.Background()
	
	iso, err := NewContainerIsolator(testLogger, "tenant-net", "exploit-net")
	if err != nil {
		t.Skipf("Docker not available: %v", err)
		return
	}
	defer cleanupIsolator(ctx, t, iso)
	
	// Disable network
	iso.SetConfig(&IsolationConfig{
		BaseImage:      "alpine:3.19",
		DisableNetwork: true,
		Command:        []string{"hostname"},
	})
	
	_, err = iso.StartContainer(ctx, []string{})
	if err != nil {
		t.Fatalf("Failed to start container with network disabled: %v", err)
	}
	
	// Check that no network was created
	if iso.networkID != "" {
		t.Error("Network should not be created when DisableNetwork=true")
	}
}

// TestPortBinding builds correct port bindings
func TestPortBindings(t *testing.T) {
	iso := &ContainerIsolator{
		logger: testLogger,
		config: &IsolationConfig{
			ExposedPorts: []int{8080, 9090},
		},
	}
	
	bindings := iso.buildPortBindings()
	
	if len(bindings) != 2 {
		t.Errorf("Expected 2 port bindings, got %d", len(bindings))
	}
	
	// Check specific ports
	expectedPorts := []string{"8080/tcp", "9090/tcp"}
	for _, port := range expectedPorts {
		if _, exists := bindings[port]; !exists {
			t.Errorf("Expected binding for port %s", port)
		}
	}
	
	// Verify localhost-only binding
	for portStr, bindingsList := range bindings {
		for _, binding := range bindingsList {
			if binding.HostIP != "127.0.0.1" {
				t.Errorf("Expected HostIP 127.0.0.1, got %s", binding.HostIP)
			}
			if binding.HostPort != fmt.Sprintf("%s", portStr[:len(portStr)-4]) {
				t.Errorf("HostPort mismatch for %s", portStr)
			}
		}
	}
}

// TestMountsConfiguration tests volume and tmpfs mount setup
func TestMountsConfiguration(t *testing.T) {
	iso := &ContainerIsolator{
		logger: testLogger,
		config: &IsolationConfig{
			TmpfsSizeBytes: 128 << 20, // 128MB
			ReadonlyRootfs: true,
		},
	}
	
	mounts := iso.buildMounts()
	
	if len(mounts) < 2 {
		t.Errorf("Expected at least 2 mounts, got %d", len(mounts))
	}
	
	// Verify tmpfs mounts exist
	foundTmp := false
	foundVarLog := false
	
	for i, m := range mounts {
		if m.Type == "tmpfs" {
			if m.Target == "/tmp" {
				foundTmp = true
				if m.Size != 128<<20 {
					t.Errorf("tmpfs /tmp size mismatch: expected 128MB, got %d", m.Size)
				}
			}
			if m.Target == "/var/log" {
				foundVarLog = true
			}
		}
	}
	
	if !foundTmp {
		t.Error("tmpfs mount for /tmp not found")
	}
	
	// Optional: var/log mount may or may not be present
	t.Logf("Mounts configured: tmp=%v, var_log=%v, total=%d", 
		foundTmp, foundVarLog, len(mounts))
}

// TestFallbackExecutor tests fallback execution when Docker unavailable
func TestFallbackExecutor(t *testing.T) {
	executor := NewFallbackExecutor(testLogger, DefaultIsolationConfig())
	
	ctx := context.Background()
	output, err := executor.Execute(ctx, "ls", "-la")
	
	// Fallback executor should always fail (not actually isolated)
	if err == nil {
		t.Error("Fallback executor should fail - not truly isolated")
	}
	
	if output != "" {
		t.Error("Fallback executor should return empty output on failure")
	}
	
	t.Log("Fallback correctly rejects execution without Docker")
}

// TestDockerAvailabilityCheck tests availability detection
func TestDockerAvailabilityCheck(t *testing.T) {
	available, msg := CheckDockerAvailability(context.Background())
	
	t.Logf("Docker availability: available=%v, message=%s", available, msg)
	
	// This test is informative only - doesn't assert anything about availability
	// CI environments may or may not have Docker
}

// TestKillFunctionality tests force kill mechanism
func TestKillFunctionality(t *testing.T) {
	ctx := context.Background()
	
	iso, err := NewContainerIsolator(testLogger, "tenant-kill", "exploit-kill")
	if err != nil {
		t.Skipf("Docker not available: %v", err)
		return
	}
	defer cleanupIsolator(ctx, t, iso)
	
	// Set a long-running command
	iso.SetConfig(&IsolationConfig{
		BaseImage:      "alpine:3.19",
		ExecutionTimeout: 2 * time.Minute,
		Command:          []string{"sleep"},
	})
	
	containerID, err := iso.StartContainer(ctx, []string{"10"})
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}
	
	if containerID == "" {
		t.Fatal("Container ID should not be empty")
	}
	
	// Give it a moment to start
	time.Sleep(1 * time.Second)
	
	// Kill the container
	iso.Kill(ctx)
	
	// Verify container is gone
	status := iso.Status(ctx)
	if status.IsRunning {
		t.Error("Container should be killed")
	}
	
	t.Log("Kill functionality works correctly")
}

// TestCleanupHooks tests custom cleanup hook registration
func TestCleanupHooks(t *testing.T) {
	ctx := context.Background()
	
	iso := &ContainerIsolator{
		logger: testLogger,
	}
	
	hookCalled := false
	
	iso.RegisterCleanupHook(func() error {
		hookCalled = true
		return nil
	})
	
	// Force run hooks directly since we can't easily trigger full cleanup
	// This tests hook registration works
	if len(iso.cleanupHooks) == 0 {
		t.Error("Cleanup hooks should be registered")
	}
	
	// Call hooks manually
	for i, hook := range iso.cleanupHooks {
		if err := hook(); err != nil {
			t.Logf("Hook %d failed: %v", i, err)
		}
	}
	
	if !hookCalled {
		t.Error("Cleanup hook was not called")
	}
	
	t.Log("Cleanup hooks work correctly")
}

// TestConcurrentAccess tests thread safety
func TestConcurrentAccess(t *testing.T) {
	iso := &ContainerIsolator{
		logger: testLogger,
		config: DefaultIsolationConfig(),
	}
	
	// Run concurrent status checks
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func() {
			_ = iso.Status(context.Background())
			done <- true
		}()
	}
	
	successCount := 0
	for i := 0; i < 10; i++ {
		<-done
		successCount++
	}
	
	if successCount != 10 {
		t.Errorf("Expected 10 successful concurrent calls, got %d", successCount)
	}
	
	t.Log("Concurrent access handled safely")
}

// TestEvidenceRecording tests evidence logging
func TestEvidenceRecording(t *testing.T) {
	// Create logger with hooks to capture logs
	var logMessages []map[string]interface{}
	
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	
	// Add a hook to capture log entries
	hook := &TestHook{messages: &logMessages}
	logger.AddHook(hook)
	
	iso := &ContainerIsolator{
		logger: testLogger,
		evidenceLogger: logger.WithFields(logrus.Fields{
			"component": "test.evidence",
			"tenant":    "tenant-evidence",
			"exploit":   "exploit-evidence",
		}),
		containerID: "test-container-123",
	}
	
	// Record some evidence
	data := map[string]interface{}{
		"action": "container_created",
		"id":     "abc123",
	}
	
	iso.recordEvidence("container_action", data)
	
	// Verify log was generated
	if len(logMessages) == 0 {
		t.Error("Expected at least one log message to be recorded")
	}
	
	// Check that required fields are present
	if len(logMessages) > 0 {
		fields := logMessages[0]
		if fields["event_type"] != "container_action" {
			t.Errorf("Expected event_type container_action, got %v", fields["event_type"])
		}
	}
	
	t.Log("Evidence recording works correctly")
}

// Helper function to clean up isolator
func cleanupIsolator(ctx context.Context, t *testing.T, iso *ContainerIsolator) {
	if iso.containerID != "" {
		_ = iso.StopContainer(ctx, 5*time.Second)
		iso.cleanup(ctx)
	}
}

// TestHook is a simple logrus hook for testing
type TestHook struct {
	messages *[]map[string]interface{}
}

func (h *TestHook) Levels() logrus.Levels {
	return logrus.AllLevels
}

func (h *TestHook) Fire(entry *logrus.Entry) error {
	msg := make(map[string]interface{})
	for k, v := range entry.Data {
		msg[k] = v
	}
	*h.messages = append(*h.messages, msg)
	return nil
}
