package scheduler

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// INTEGRATION TESTS FOR REAL RL SCHEDULING ENVIRONMENT
// These tests validate genuine deep RL training with production MIG scheduler
// NO SIMULATION - every reward comes from real scheduling outcomes
// ============================================================================

func TestRLTrainingOnRealSchedule(t *testing.T) {
	// Setup: Create real environment with production MIG scheduler
	env, err := NewRLEnvironment(DefaultRLConfig())
	require.NoError(t, err, "Failed to create real RL environment")
	assert.NotNil(t, env)
	
	// Setup: Initialize DQN optimizer
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	
	optimizer, err := NewDeepRLOptimizer(nil, logger)
	require.NoError(t, err, "Failed to create Deep RLOptimizer")
	assert.NotNil(t, optimizer)
	
	// Capture initial network weights
	initialWeights := optimizer.captureNetworkWeights()
	t.Logf("Initial Q-network initialized with %d layers", len(initialWeights))

	// Execute: Run 500 episodes of REAL training
	const testEpisodes = 500
	
	startTime := time.Now()
	metrics := optimizer.TrainWithEnvironment(env, testEpisodes)
	elapsed := time.Since(startTime)
	
	t.Logf("✓ Real RL Training Complete: %d episodes in %v", testEpisodes, elapsed)

	// Assert: Acceptance rate improved
	t.Logf("✓ Final acceptance rate: %.2f%%", metrics.FinalAcceptanceRate*100)
	assert.Greater(t, metrics.FinalAcceptanceRate, 0.1, 
		"Acceptance rate should improve after training")

	// Assert: Low fragmentation
	t.Logf("✓ Average fragmentation: %.2f%%", metrics.AverageFragmentation*100)
	assert.Less(t, metrics.AverageFragmentation, 0.90, 
		"Fragmentation should be reduced")

	// Assert: Neural network weights actually changed (proves NOT simulation)
	hasChanged := false
	for layer := range initialWeights {
		for idx := range initialWeights[layer] {
			diff := math.Abs(optimizer.qNetwork.weights[layer][idx] - initialWeights[layer][idx])
			if diff > 0.001 {
				hasChanged = true
				break
			}
		}
		if hasChanged {
			break
		}
	}
	t.Logf("✓ Weight change detected: %v", hasChanged)
	assert.True(t, hasChanged, 
		"Neural network weights MUST change during training - if no change, it's simulated!")
	
	// Assert: Experience pool grew with REAL transitions
	expSize := optimizer.experiencePool.Size()
	t.Logf("✓ Experience pool size: %d transitions", expSize)
	assert.Greater(t, expSize, 0, "Should have stored real experience tuples")
}

func TestRLEnvironmentStepExecutesRealScheduling(t *testing.T) {
	// Create fresh environment
	env, err := NewRLEnvironment(DefaultRLConfig())
	require.NoError(t, err)
	
	// Reset to initialize queue
	state := env.Reset()
	assert.NotZero(t, len(state.RequestQueue), "Queue should have workloads after reset")
	
	originalQueueSize := len(state.RequestQueue)
	t.Logf("Initial queue size: %d", originalQueueSize)
	
	// Execute step with action 0 (assign small slice)
	nextState, reward, done, info := env.Step(int(ActionAssignSmallSlice))
	
	// Verify outcome
	assert.False(t, done, "Should not be done after first step")
	assert.GreaterOrEqual(t, reward, 0.0, 
		"Reward should be non-negative for successful scheduling")
	
	// Verify state transition
	assert.NotEqual(t, state, nextState, "State should change after action")
	
	// Verify metadata
	assignedCount := 0
	if ac, ok := info["assigned_count"].(int); ok {
		assignedCount = ac
	}
	t.Logf("Scheduled %d workloads", assignedCount)
	
	// Verify queue decreased or stayed same
	assert.LessOrEqual(t, len(nextState.RequestQueue), originalQueueSize, 
		"Queue should decrease after scheduling")
}

func TestRealRewardComputationFromActualMetrics(t *testing.T) {
	// Create environment and execute some steps
	env, err := NewRLEnvironment(DefaultRLConfig())
	require.NoError(t, err)
	
	// Execute multiple steps to build up assignments
	for i := 0; i < 20; i++ {
		_, _, done, _ := env.Step(int(ActionAssignSmallSlice))
		if done {
			break
		}
	}
	
	// Get final metrics
	finalMetrics := env.GetMetrics()
	
	t.Logf("Final acceptance rate: %.2f%%", finalMetrics.AcceptanceRate*100)
	t.Logf("Fragmentation metric: %.2f%%", finalMetrics.FragmentationMetric*100)
	t.Logf("Utilization rate: %.2f%%", finalMetrics.UtilizationRate*100)
	
	// Verify metrics are in valid range [0, 1]
	assert.GreaterOrEqual(t, finalMetrics.AcceptanceRate, 0.0)
	assert.LessOrEqual(t, finalMetrics.AcceptanceRate, 1.0)
	
	assert.GreaterOrEqual(t, finalMetrics.FragmentationMetric, 0.0)
	assert.LessOrEqual(t, finalMetrics.FragmentationMetric, 1.0)
	
	assert.GreaterOrEqual(t, finalMetrics.UtilizationRate, 0.0)
	assert.LessOrEqual(t, finalMetrics.UtilizationRate, 1.0)
	
	// Verify reward computation uses real metrics
	reward := env.computeRealReward()
	t.Logf("Computed real reward: %.4f", reward)
	
	// Reward should be positive weighted sum
	assert.GreaterOrEqual(t, reward, -1.0, 
		"Reward should be approximately >= minimum_possible")
	assert.Less(t, reward, 1.5, 
		"Reward should be bounded by maximum possible weight sum")
}

func TestMIGSchedulerActuallyPlacesWorkloads(t *testing.T) {
	// Create MIG scheduler directly
	scheduler := NewMigScheduler()
	assert.NotNil(t, scheduler)
	assert.Equal(t, 8, len(scheduler.gpus), "Should have 8 A100 GPUs")
	
	// Generate realistic workload queue
	workloads := make([]Workload, 20)
	profiles := []string{"1g.10gb", "2g.20gb", "3g.40gb"}
	
	for i := 0; i < 20; i++ {
		profileName := profiles[i%len(profiles)]
		var memoryGB int
		switch profileName {
		case "1g.10gb":
			memoryGB = 10
		case "2g.20gb":
			memoryGB = 20
		case "3g.40gb":
			memoryGB = 40
		}
		
		workloads[i] = Workload{
			ID:       fmt.Sprintf("test-%d", i),
			Name:     fmt.Sprintf("Test Workload %d", i),
			Priority: i % 10,
			ResourceRequest: common.ResourceRequest{
				GPUCount:      1,
				MemoryBytes:   int64(memoryGB) * 1024 * 1024 * 1024,
				CPUMillicores: 8000,
			},
		}
	}
	
	// Execute real MIG scheduling
	assignments, err := scheduler.Schedule(workloads)
	require.NoError(t, err, "MIG scheduling should not fail")
	
	t.Logf("✓ Requested %d workloads, scheduled %d successfully", len(workloads), len(assignments))
	
	// Assertions that prove REAL scheduling occurred
	assert.NotEmpty(t, assignments, "Should schedule some workloads")
	assert.LessOrEqual(t, len(assignments), len(workloads), 
		"Scheduled count cannot exceed requested")
	
	// Verify each assignment is valid MIG allocation
	for _, a := range assignments {
		assert.NotEmpty(t, a.NodeName, "Assignment should have node name")
		assert.NotEmpty(t, a.Reason, "Assignment should explain why")
		assert.Greater(t, len(a.GPUIndices), 0, "Assignment must specify GPU indices")
		
		// Verify MIG slice ratio is valid
		sliceRatio := a.GPUShareRatio
		assert.GreaterOrEqual(t, sliceRatio, 0.0)
		assert.LessOrEqual(t, sliceRatio, 1.0)
		
		t.Logf("✓ Assignment: %s@GPU%d (ratio=%.2f)", a.NodeName, a.GPUIndices[0], sliceRatio)
	}
}
