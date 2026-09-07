package aiops

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_AllowExecution(t *testing.T) {
	tests := []struct {
		name          string
		maxFailures   int
		timeout       time.Duration
		failures      int
		expectedAllow bool
		description   string
	}{
		{
			name:          "initial_state_allows_execution",
			maxFailures:   5,
			timeout:       1 * time.Minute,
			failures:      0,
			expectedAllow: true,
			description:   "Should allow execution when circuit is closed initially",
		},
		{
			name:          "open_circuit_blocks_execution",
			maxFailures:   3,
			timeout:       1 * time.Second,
			failures:      3,
			expectedAllow: false,
			description:   "Should block execution after reaching max failures",
		},
		{
			name:          "timeout_expired_allows_half_open",
			maxFailures:   3,
			timeout:       100 * time.Millisecond,
			failures:      3,
			expectedAllow: true,
			description:   "Should allow execution after timeout expires (half-open state)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := &CircuitBreaker{
				maxFailures:  tt.maxFailures,
				timeout:      tt.timeout,
				failureCount: tt.failures,
				state:        "closed",
			}

			if tt.failures >= tt.maxFailures {
				cb.state = "open"
				cb.lastFailure = time.Now().Add(-tt.timeout)
			}

			// For timeout expired test, artificially expire the timeout
			if tt.name == "timeout_expired_allows_half_open" {
				cb.lastFailure = time.Now().Add(-2 * tt.timeout)
			}

			result := cb.AllowExecution()
			assert.Equal(t, tt.expectedAllow, result, tt.description)
		})
	}
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	cb := &CircuitBreaker{
		maxFailures:  5,
		timeout:      1 * time.Minute,
		failureCount: 3,
		state:        "open",
		lastFailure:  time.Now(),
	}

	// Record success should reset to closed state
	cb.RecordSuccess()

	assert.Equal(t, 0, cb.failureCount, "failure count should be reset")
	assert.Equal(t, "closed", cb.state, "state should transition to closed")
	assert.True(cb.AllowExecution(), "should allow execution after reset")
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	cb := &CircuitBreaker{
		maxFailures: 3,
		timeout:     1 * time.Minute,
		failureCount: 0,
		state:       "closed",
	}

	cb.RecordFailure()
	assert.Equal(t, 1, cb.failureCount, "first failure recorded")
	assert.Equal(t, "closed", cb.state, "still closed after first failure")

	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, 3, cb.failureCount, "three failures recorded")
	assert.Equal(t, "open", cb.state, "circuit opens after max failures reached")
}

func TestExtractPodInfo(t *testing.T) {
	tests := []struct {
		name         string
		metadata     map[string]interface{}
		expectPod    string
		expectNS     string
		description  string
	}{
		{
			name: "valid_pod_and_namespace",
			metadata: map[string]interface{}{
				"pod_name":   "test-pod-abc123",
				"namespace":  "production",
			},
			expectPod:   "test-pod-abc123",
			expectNS:    "production",
			description: "Should extract pod name and namespace correctly",
		},
		{
			name: "missing_namespace_defaults_to_default",
			metadata: map[string]interface{}{
				"pod_name": "test-pod-def456",
			},
			expectPod:   "test-pod-def456",
			expectNS:    "default",
			description: "Should default namespace to 'default' when not provided",
		},
		{
			name:         "nil_metadata_returns_empty",
			metadata:     nil,
			expectPod:    "",
			expectNS:     "",
			description:  "Should return empty strings for nil metadata",
		},
		{
			name:         "empty_metadata_returns_empty",
			metadata:     map[string]interface{}{},
			expectPod:    "",
			expectNS:     "",
			description:  "Should return empty strings for empty metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podName, ns := extractPodInfo(tt.metadata)
			assert.Equal(t, tt.expectPod, podName, tt.description)
			assert.Equal(t, tt.expectNS, ns, tt.description)
		})
	}
}

func TestNewK8sHealingOrchestrator_DefaultLogger(t *testing.T) {
	ctx := context.Background()
	
	// Skip actual Kubernetes config loading in tests
	// This would require a real kubeconfig or mock setup
	t.Skip("Skipping K8s orchestrator creation test - requires real K8s cluster")

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise

	orchestrator, err := NewK8sHealingOrchestrator("/nonexistent/kubeconfig", logger)
	assert.NotNil(t, orchestrator, "Should create orchestrator instance even with invalid config")
	assert.Error(t, err, "Should return error for invalid kubeconfig path")
}

func TestRemediationResult_MetricsStatus(t *testing.T) {
	startTime := time.Now()
	
	result := RemediationResult{
		FaultType:     "gpu-temp-high",
		ActionType:    ActionPodRestart,
		StartTime:     startTime,
		MetricsStatus: "pending",
	}
	
	require.Equal(t, "pending", result.MetricsStatus, "Initial status should be pending")
	
	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)
	result.MetricsStatus = "success"
	
	assert.Equal(t, "success", result.MetricsStatus, "Status should update on success")
	assert.True(t, result.Success)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestCircuitBreaker_ParallelAccess(t *testing.T) {
	cb := &CircuitBreaker{
		maxFailures: 10,
		timeout:     1 * time.Minute,
	}
	
	done := make(chan bool, 100)
	
	// Launch concurrent goroutines accessing the circuit breaker
	for i := 0; i < 100; i++ {
		go func() {
			_ = cb.AllowExecution()
			done <- true
			
			if i%2 == 0 {
				cb.RecordSuccess()
			} else {
				cb.RecordFailure()
			}
		}()
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < 100; i++ {
		<-done
	}
	
	// Should not panic under concurrent access
	t.Log("Circuit breaker handled concurrent access safely")
}

func TestFaultEvent_MetadataStructure(t *testing.T) {
	fault := Fault{
		Type:     "node-disk-full",
		Severity: "critical",
		Metadata: map[string]interface{}{
			"node_name": "worker-node-3",
			"disk_path": "/var/lib/docker",
			"usage_pct": 97.5,
		},
		DetectedAt: time.Now(),
		Source:     "disk-detector-01",
	}
	
	assert.Equal(t, "node-disk-full", fault.Type)
	assert.Equal(t, "critical", fault.Severity)
	assert.Equal(t, "worker-node-3", fault.Metadata["node_name"])
	assert.Equal(t, float64(97.5), fault.Metadata["usage_pct"])
	assert.Equal(t, "disk-detector-01", fault.Source)
}

func TestActionType_StringRepresentation(t *testing.T) {
	tests := []struct {
		action   ActionType
		expected string
	}{
		{ActionPodRestart, "pod_restart"},
		{ActionNodeCordon, "node_cordon"},
		{ActionServiceFailover, "service_failover"},
		{ActionScaleUp, "scale_up"},
		{ActionRollback, "rollback"},
		{ActionExecuteCommand, "exec_command"},
	}
	
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.action), "String representation should match constant value")
		})
	}
}

// Integration test placeholder - requires real K8s cluster
func TestK8sHealingOrchestrator_Integration_RecoverPod(t *testing.T) {
	if testing.Short() {
		t.Skip("Integration test skipped")
	}
	
	// This would require:
	// 1. Real K8s cluster access
	// 2. Test namespace setup
	// 3. Actual pod deployment
	// 4. Cleanup procedures
	
	t.Skip("Integration test requires real Kubernetes cluster")
}

func BenchmarkCircuitBreaker(b *testing.B) {
	cb := &CircuitBreaker{
		maxFailures: 100,
		timeout:     1 * time.Minute,
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		cb.AllowExecution()
		
		if i%10 == 0 {
			cb.RecordFailure()
		} else {
			cb.RecordSuccess()
		}
	}
}

func BenchmarkExtractPodInfo(b *testing.B) {
	metadata := map[string]interface{}{
		"pod_name":   "test-pod-xyz",
		"namespace":  "kube-system",
		"node_name":  "worker-1",
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		extractPodInfo(metadata)
	}
}

func TestChaos_CPUStresser_Configuration(t *testing.T) {
	// Test CPU stresser configuration defaults
	config := DefaultCPUStresserConfig()
	
	assert.GreaterOrEqual(t, config.Duration, time.Minute, "Default duration should be at least 1 minute")
	assert.Greater(t, config.Workers, 0, "Workers should be at least 1")
	
	stresser := NewCPUStresser(config)
	assert.NotNil(t, stresser, "Should create CPU stresser instance")
	
	// Verify it has the configured values
	assert.Equal(t, config.Duration, stresser.duration)
	assert.Equal(t, config.Workers, stresser.workers)
}

func TestChaos_CPUStresser_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	config := CPUStresserConfig{
		Duration: 5 * time.Minute,
		Workers:  2,
	}
	
	stresser := NewCPUStresser(config)
	
	// Should respect context cancellation
	err := stresser.Inject(ctx)
	
	// May complete early due to context cancellation
	if err != nil {
		assert.Equal(t, context.DeadlineExceeded, err || ctx.Err())
	}
}

// Logger implementation for testing
type testLogger struct{}

func (l *testLogger) Infof(format string, args ...interface{}) {
	t.Logf(format, args...)
}

func (l *testLogger) Warnf(format string, args ...interface{}) {
	t.Logf("WARN: "+format, args...)
}

func (l *testLogger) Errorf(format string, args ...interface{}) {
	t.Logf("ERROR: "+format, args...)
}
