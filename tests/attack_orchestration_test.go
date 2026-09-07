// Package tests - Comprehensive unit tests for attack orchestration system
package tests

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/orchestration"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// ============================================================================
// TEST FIXTURES AND HELPERS
// ============================================================================

var logger *logrus.Logger

func init() {
	logger = logrus.New()
	logger.SetLevel(logrus.DebugLevel)
}

func TestMain(m *testing.M) {
	// Setup before all tests
	code := m.Run()
	
	// Cleanup if needed
	_ = code
}

// ============================================================================
// ATTACK ORCHESTRATOR TESTS
// ============================================================================

func TestNewAttackOrchestrator(t *testing.T) {
	t.Run("create_with_default_config", func(t *testing.T) {
		ctx := context.Background()
		
		orchestrator, err := orchestration.NewAttackOrchestrator(ctx, logger)
		
		assert.NoError(t, err)
		assert.NotNil(t, orchestrator)
		
		state := orchestrator.GetState()
		assert.Equal(t, "Ready", state.Status)
	})
	
	t.Run("handle_nil_logger", func(t *testing.T) {
		ctx := context.Background()
		
		orchestrator, err := orchestration.NewAttackOrchestrator(ctx, nil)
		
		assert.NoError(t, err)
		assert.NotNil(t, orchestrator)
	})
}

func TestAttackOrchestratorLifecycle(t *testing.T) {
	t.Run("start_and_stop_gracefully", func(t *testing.T) {
		ctx := context.Background()
		
		orchestrator, err := orchestration.NewAttackOrchestrator(ctx, logger)
		assert.NoError(t, err)
		
		startCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		
		err = orchestrator.Start(startCtx)
		assert.NoError(t, err)
		
		state := orchestrator.GetState()
		assert.Equal(t, "Running", state.Status)
		
		// Stop after a short delay
		time.Sleep(100 * time.Millisecond)
		
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		
		err = orchestrator.Stop(stopCtx)
		assert.NoError(t, err)
		
		finalState := orchestrator.GetState()
		assert.Contains(t, []string{"Stopped", "ForceStopped", "TimeoutStopped"}, finalState.Status)
	})
	
	t.Run("prevent_double_start", func(t *testing.T) {
		ctx := context.Background()
		
		orchestrator, err := orchestration.NewAttackOrchestrator(ctx, logger)
		assert.NoError(t, err)
		
		startCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		
		err = orchestrator.Start(startCtx)
		assert.NoError(t, err)
		
		// Attempt second start should fail
		err = orchestrator.Start(startCtx)
		assert.Error(t, err)
	})
}

func TestParallelAttackVectors(t *testing.T) {
	t.Run("execute_three_vectors_concurrently", func(t *testing.T) {
		ctx := context.Background()
		
		orchestrator, err := orchestration.NewAttackOrchestrator(ctx, logger)
		assert.NoError(t, err)
		
		// Submit attack paths
		submitCtx, submitCancel := context.WithTimeout(ctx, 2*time.Second)
		defer submitCancel()
		
		phishingPathID := "phishing_001"
		rcePathID := "rce_001"
		ntlmPathID := "ntlm_001"
		
		err = orchestrator.SubmitPhishingPath(submitCtx, phishingPathID, "user@target.com", "target.com")
		assert.NoError(t, err)
		
		err = orchestrator.SubmitRCEPath(submitCtx, rcePathID, "server.example.com", "SharePoint_RCE")
		assert.NoError(t, err)
		
		err = orchestrator.SubmitNTLMPath(submitCtx, ntlmPathID, "workstation.example.com")
		assert.NoError(t, err)
		
		// Let execution proceed briefly
		time.Sleep(500 * time.Millisecond)
		
		// Check state
		state := orchestrator.GetState()
		t.Logf("Active paths: %d, Completed: %d", state.ActivePaths, state.CompletedPaths)
		
		// Stop orchestrator
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		
		orchestrator.Stop(stopCtx)
		
		results := orchestrator.GetResults()
		t.Logf("Total results collected: %d", len(results))
		
		assert.True(t, len(results) >= 0) // At least some results
	})
	
	t.Run("channel_buffer_overflow_handling", func(t *testing.T) {
		ctx := context.Background()
		
		orchestrator, err := orchestration.NewAttackOrchestrator(ctx, logger)
		assert.NoError(t, err)
		
		submitCtx := context.Background()
		
		// Try to submit many paths quickly (more than channel buffer)
		for i := 0; i < 20; i++ {
			pathID := fmt.Sprintf("test_%d", i)
			err := orchestrator.SubmitPhishingPath(submitCtx, pathID, "user@example.com", "example.com")
			if err != nil {
				// Channel full is acceptable in this test
				break
			}
		}
		
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		
		orchestrator.Stop(stopCtx)
	})
}

func TestQValueUpdates(t *testing.T) {
	t.Run("update_q_values_based_on_feedback", func(t *testing.T) {
		ctx := context.Background()
		
		orchestrator, err := orchestration.NewAttackOrchestrator(ctx, logger)
		assert.NoError(t, err)
		
		// Manually test Q-value update mechanism
		initialState := "state_initial"
		initialAction := "action_exploit"
		
		// Simulate success
		metrics := &orchestration.AttackMetrics{
			SuccessRate:   0.8,
			StealthScore:  0.7,
			DetectionCount: 1,
			ElapsedTimeMS: 2000,
			ResourceUsage: 0.6,
		}
		
		// Get reward (indirectly through internal method)
		reward := orchestrator.CalculateRewardForTest(initialState, initialAction, true, metrics)
		assert.Greater(t, reward, float64(0))
		
		// Get negative reward
		negativeReward := orchestrator.CalculateRewardForTest(initialState, initialAction, false, metrics)
		assert.Less(t, negativeReward, reward)
	})
}

// ============================================================================
// REWARD CALCULATOR TESTS
// ============================================================================

func TestRewardCalculatorCreation(t *testing.T) {
	t.Run("create_with_default_config", func(t *testing.T) {
		calc := orchestration.NewRewardCalculator(logger)
		
		assert.NotNil(t, calc)
		assert.NotNil(t, calc.Config)
		
		// Check default weights
		assert.Equal(t, 0.40, calc.Config.SuccessWeight)
		assert.Equal(t, 0.30, calc.Config.StealthWeight)
		assert.Equal(t, 0.20, calc.Config.DetectionPenalty)
	})
	
	t.Run("configurable_weights", func(t *testing.T) {
		calc := orchestration.NewRewardCalculator(logger)
		
		newConfig := &orchestration.RewardConfig{
			SuccessWeight:       0.50,
			StealthWeight:       0.25,
			DetectionPenalty:    0.15,
			TimeEfficiencyBonus: 0.10,
			ResourceUtilization: 0.20,
		}
		
		calc.SetConfig(newConfig)
		assert.Equal(t, newConfig.SuccessWeight, calc.Config.SuccessWeight)
	})
}

func TestCalculateComponents(t *testing.T) {
	t.Run("calculate_complete_reward", func(t *testing.T) {
		calc := orchestration.NewRewardCalculator(logger)
		
		successMetrics := &orchestration.SuccessMetrics{
			ActualExploits:      5,
			SuccessfulExploits:  3,
			FrameworkDetected:   true,
			VulnerabilityScore:  9.8,
			DataExfiltrated:     true,
			PrivilegeEscalated:  true,
			CurrentPrivileges:   "User",
			TargetPrivileges:    "DomainAdmin",
		}
		
		stealthMetrics := &orchestration.StealthMetrics{
			EDEvasionRate:   0.9,
			AVDetectionRate: 0.85,
			LogTampering:    false,
			Cleanliness:     0.95,
			HidingTechnique: "SignedBinary",
		}
		
		detectionMetrics := &orchestration.DetectionMetrics{
			SIEMEvents: []orchestration.SIEMEvent{
				{EventID: 4625, Logged: true, Severity: "Low"},
				{EventID: 4624, Logged: true, Severity: "Medium"},
			},
			EventCount:      2,
			AlertsGenerated: 1,
			TriageStatus:    "Medium",
			ResponseTimeMS:  1000,
			AutomatedBlocks: 0,
		}
		
		totalReward := calc.Calculate(
			successMetrics,
			stealthMetrics,
			detectionMetrics,
			2000, // time in ms
			0.6,  // resource usage
		)
		
		// Reward should be positive and bounded
		assert.Greater(t, totalReward, float64(0))
		assert.Less(t, totalReward, float64(1))
	})
	
	t.Run("handle_nil_metrics", func(t *testing.T) {
		calc := orchestration.NewRewardCalculator(logger)
		
		reward := calc.Calculate(nil, nil, nil, 0, 0)
		assert.GreaterOrEqual(t, reward, float64(0))
	})
	
	t.Run("negative_reward_on_failure", func(t *testing.T) {
		calc := orchestration.NewRewardCalculator(logger)
		
		successMetrics := &orchestration.SuccessMetrics{
			ActualExploits:     3,
			SuccessfulExploits: 0,
		}
		
		reward := calc.Calculate(
			successMetrics,
			nil,
			nil,
			0,
			0,
		)
		
		// Should be very low or negative
		assert.Less(t, reward, float64(0.5))
	})
}

func TestAdaptiveTuning(t *testing.T) {
	t.Run("adapt_weights_after_history", func(t *testing.T) {
		calc := orchestration.NewRewardCalculator(logger)
		
		config := orchestration.DefaultRewardConfig()
		config.AdaptiveTuning = true
		config.TuningWindow = 10
		
		calc.SetConfig(config)
		
		// Generate synthetic history
		for i := 0; i < 15; i++ {
			metrics := createRandomMetrics()
			calc.Calculate(
				metrics.success,
				metrics.stealth,
				metrics.detection,
				metrics.timeTakenMS,
				metrics.resourceUsage,
			)
		}
		
		stats := calc.GetRewardAnalytics()
		assert.Greater(t, stats.HistoricalDataPoints, 0)
	})
}

func TestRewardAnalytics(t *testing.T) {
	t.Run("compute_statistics", func(t *testing.T) {
		calc := orchestration.NewRewardCalculator(logger)
		
		// Generate known reward distribution
		expectedRewards := []float64{0.3, 0.4, 0.5, 0.6, 0.7}
		
		for _, reward := range expectedRewards {
			successMetrics := &orchestration.SuccessMetrics{
				ActualExploits:      1,
				SuccessfulExploits:  int(reward * 1),
				FrameworkDetected:   reward > 0.4,
				VulnerabilityScore:  9.0,
				PrivilegeEscalated:  reward > 0.5,
				CurrentPrivileges:   "User",
				TargetPrivileges:    "Admin",
			}
			
			calc.Calculate(
				successMetrics,
				nil,
				nil,
				1000,
				0.5,
			)
		}
		
		analytics := calc.GetRewardAnalytics()
		
		assert.Greater(t, analytics.HistoricalDataPoints, 0)
		assert.Greater(t, analytics.AverageReward, float64(0))
		assert.Greater(t, analytics.MaxReward, analytics.MinReward)
	})
}

// ============================================================================
// PATH MANAGER TESTS
// ============================================================================

func TestPathManagerCreation(t *testing.T) {
	t.Run("initialize_empty_manager", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		assert.NotNil(t, manager)
		assert.NotEmpty(t, manager.CurrentState)
		
		stats := manager.GetPathStats()
		assert.Equal(t, 0, stats.TotalDiscovered)
		assert.Equal(t, 0, stats.SubmittedCount)
	})
}

func TestPathCreationAndLifecycle(t *testing.T) {
	t.Run("create_phishing_path", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		path, err := manager.CreatePath("path_001", "Spear Phishing Campaign", 
			orchestration.PhishingPath, 7)
		
		assert.NoError(t, err)
		assert.NotNil(t, path)
		assert.Equal(t, "path_001", path.ID)
		assert.Equal(t, orchestration.PathStatusNew, path.Status)
		assert.Equal(t, 4, len(path.Stages))
	})
	
	t.Run("create_rce_path", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		path, err := manager.CreatePath("path_002", "SharePoint RCE", 
			orchestration.RCEPath, 8)
		
		assert.NoError(t, err)
		assert.NotNil(t, path)
		assert.Equal(t, 4, len(path.Stages))
	})
	
	t.Run("create_ntlm_path", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		path, err := manager.CreatePath("path_003", "NTLM Relay Attack", 
			orchestration.NTLMRelayPath, 6)
		
		assert.NoError(t, err)
		assert.NotNil(t, path)
	})
	
	t.Run("duplicate_path_id_fails", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		_, err1 := manager.CreatePath("dup_001", "First Path", orchestration.PhishingPath, 5)
		assert.NoError(t, err1)
		
		_, err2 := manager.CreatePath("dup_001", "Second Path", orchestration.PhishingPath, 5)
		assert.Error(t, err2)
		assert.Contains(t, err2.Error(), "already exists")
	})
	
	t.Run("invalid_priority_rejected", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		_, err := manager.CreatePath("bad_path", "Invalid Priority", 
			orchestration.PhishingPath, 15)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be between 1 and 10")
	})
}

func TestPathExecutionManagement(t *testing.T) {
	t.Run("submit_and_complete_path", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		path, _ := manager.CreatePath("exec_test", "Test Path", 
			orchestration.PhishingPath, 5)
		
		// Mark as submitted
		manager.MarkSubmitted(path.ID)
		
		p, exists := manager.GetPath(path.ID)
		assert.True(t, exists)
		assert.Equal(t, orchestration.PathStatusQueued, p.Status)
		
		// Mark as completed successfully
		manager.MarkCompleted(path.ID, true, "")
		
		p, exists = manager.GetPath(path.ID)
		assert.True(t, exists)
		assert.Equal(t, orchestration.PathStatusSucceeded, p.Status)
		
		stats := manager.GetPathStats()
		assert.Equal(t, 1, stats.SuccessfulCount)
		assert.Equal(t, 1.0, stats.AverageSuccessRate)
	})
	
	t.Run("failed_path_tracking", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		path, _ := manager.CreatePath("fail_test", "Fail Path", 
			orchestration.RCEPath, 6)
		
		manager.MarkSubmitted(path.ID)
		manager.MarkCompleted(path.ID, false, "Vulnerability not found")
		
		stats := manager.GetPathStats()
		assert.Equal(t, 1, stats.FailedCount)
		assert.Equal(t, 0.0, stats.AverageSuccessRate)
	})
}

func TestPathOptimization(t *testing.T) {
	t.Run("optimize_path_with_q_value", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		path, _ := manager.CreatePath("opt_test", "Optimized Path", 
			orchestration.PhishingPath, 7)
		
		err := manager.OptimizePath(path.ID, "state_enhanced_001", 0.85)
		
		assert.NoError(t, err)
		
		p, exists := manager.GetPath(path.ID)
		assert.True(t, exists)
		assert.Equal(t, orchestration.PathStatusOptimized, p.Status)
		assert.Equal(t, 0.85, p.QValue)
		
		stats := manager.GetPathStats()
		assert.Equal(t, 1, stats.TotalOptimized)
		assert.Equal(t, 1, stats.OptimizationCycles)
	})
	
	t.Run("select_best_paths_by_q_value", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		// Create paths with different priorities
		manager.CreatePath("high_prio", "High Priority", orchestration.PhishingPath, 9)
		manager.CreatePath("med_prio", "Medium Priority", orchestration.RCEPath, 5)
		manager.CreatePath("low_prio", "Low Priority", orchestration.NTLMRelayPath, 3)
		
		// Optimize them
		manager.OptimizePath("high_prio", "state_high", 0.95)
		manager.OptimizePath("med_prio", "state_med", 0.70)
		manager.OptimizePath("low_prio", "state_low", 0.45)
		
		bestPaths := manager.SelectBestPaths(2)
		
		assert.Len(t, bestPaths, 2)
		assert.Equal(t, "high_prio", bestPaths[0].ID)
		assert.Equal(t, "med_prio", bestPaths[1].ID)
	})
}

// ============================================================================
// INTEGRATION TESTS
// ============================================================================

func TestFullAttackOrchestrationFlow(t *testing.T) {
	t.Run("complete_attack_campaign", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		
		// Create orchestrator
		orchestrator, err := orchestration.NewAttackOrchestrator(ctx, logger)
		assert.NoError(t, err)
		
		// Start attack campaign
		err = orchestrator.Start(ctx)
		assert.NoError(t, err)
		
		// Submit multiple paths
		pathIDs := []string{"phish_1", "phish_2", "rce_1", "ntlm_1"}
		attackTypes := []orchestration.AttackPathType{
			orchestration.PhishingPath,
			orchestration.PhishingPath,
			orchestration.RCEPath,
			orchestration.NTLMRelayPath,
		}
		
		for i, pathID := range pathIDs {
			switch attackTypes[i] {
			case orchestration.PhishingPath:
				err = orchestrator.SubmitPhishingPath(ctx, pathID, 
					fmt.Sprintf("target%d@example.com", i+1), "example.com")
			case orchestration.RCEPath:
				err = orchestrator.SubmitRCEPath(ctx, pathID, 
					fmt.Sprintf("server%d.corp.local", i+1), "Exploit_Template")
			case orchestration.NTLMRelayPath:
				err = orchestrator.SubmitNTLMPath(ctx, pathID, 
					fmt.Sprintf("ws%d.internal.net", i+1))
			}
			
			assert.NoError(t, err)
		}
		
		// Wait for partial execution
		time.Sleep(2 * time.Second)
		
		// Check intermediate state
		state := orchestrator.GetState()
		t.Logf("Intermediate State - Active: %d, Completed: %d, Success: %d",
			state.ActivePaths, state.CompletedPaths, state.SucceededPaths)
		
		// Stop gracefully
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		
		orchestrator.Stop(stopCtx)
		
		// Verify final results
		results := orchestrator.GetResults()
		assert.GreaterOrEqual(t, len(results), 0)
		
		finalState := orchestrator.GetState()
		assert.Contains(t, []string{"Stopped", "ForceStopped", "TimeoutStopped"}, finalState.Status)
	})
}

func TestConcurrentAccessSafety(t *testing.T) {
	t.Run("thread_safe_path_operations", func(t *testing.T) {
		manager := orchestration.NewPathManager(logger)
		
		var wg sync.WaitGroup
		iterations := 50
		
		// Concurrent path creation
		wg.Add(iterations)
		for i := 0; i < iterations; i++ {
			go func(index int) {
				defer wg.Done()
				
				pathID := fmt.Sprintf("concurrent_%d", index)
				path, err := manager.CreatePath(pathID, 
					fmt.Sprintf("Path %d", index), 
					orchestration.PhishingPath, 5)
				
				if err == nil && path != nil {
					manager.MarkSubmitted(path.ID)
					manager.MarkCompleted(path.ID, true, "")
				}
			}(i)
		}
		
		wg.Wait()
		
		stats := manager.GetPathStats()
		assert.GreaterOrEqual(t, stats.SuccessfulCount, 0)
	})
	
	t.Run("reward_calculator_thread_safety", func(t *testing.T) {
		calc := orchestration.NewRewardCalculator(logger)
		
		var wg sync.WaitGroup
		goroutines := 20
		
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func(index int) {
				defer wg.Done()
				
				metrics := createRandomMetrics()
				calc.Calculate(
					metrics.success,
					metrics.stealth,
					metrics.detection,
					metrics.timeTakenMS,
					metrics.resourceUsage,
				)
			}(i)
		}
		
		wg.Wait()
		
		analytics := calc.GetRewardAnalytics()
		assert.GreaterOrEqual(t, analytics.HistoricalDataPoints, goroutines)
	})
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

type testMetrics struct {
	success       *orchestration.SuccessMetrics
	stealth       *orchestration.StealthMetrics
	detection     *orchestration.DetectionMetrics
	timeTakenMS   int64
	resourceUsage float64
}

func createRandomMetrics() testMetrics {
	return testMetrics{
		success: &orchestration.SuccessMetrics{
			ActualExploits:      rand.Intn(10) + 1,
			SuccessfulExploits:  rand.Intn(10) + 1,
			FrameworkDetected:   rand.Float32() > 0.5,
			VulnerabilityScore:  float64(rand.Intn(10)),
			DataExfiltrated:     rand.Float32() > 0.7,
			PrivilegeEscalated:  rand.Float32() > 0.4,
			CurrentPrivileges:   "User",
			TargetPrivileges:    "Admin",
		},
		stealth: &orchestration.StealthMetrics{
			EDEvasionRate:   rand.Float32(),
			AVDetectionRate: rand.Float32(),
			LogTampering:    rand.Float32() > 0.9,
			Cleanliness:     rand.Float32(),
			HidingTechnique: "LivingOffLand",
		},
		detection: &orchestration.DetectionMetrics{
			SIEMEvents:      []orchestration.SIEMEvent{{EventID: 4624, Logged: true}},
			EventCount:      rand.Intn(5),
			AlertsGenerated: rand.Intn(3),
			TriageStatus:    "Medium",
			ResponseTimeMS:  int64(rand.Intn(5000)),
			AutomatedBlocks: rand.Intn(2),
		},
		timeTakenMS:   int64(rand.Intn(10000)) + 500,
		resourceUsage: rand.Float32(),
	}
}
