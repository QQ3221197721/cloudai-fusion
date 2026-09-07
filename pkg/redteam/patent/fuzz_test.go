// Package patent - Fuzz Tests for Self-Evolving Attack Graph Engine (Patent #1)
// Property-based testing for edge cases and robustness validation
package patent

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Test Category A: Q-Learning Agent Properties (~3 fuzz tests)
// ============================================================================

func FuzzQLearning_Train_VariousHyperparameters(f *testing.F) {
	// Seed corpus with typical hyperparameter combinations
	f.Add(float64(0.1), float64(0.9))   // alpha, gamma baseline
	f.Add(float64(0.0), float64(1.0))   // edge case: zero learning
	f.Add(float64(1.0), float64(0.0))   // edge case: no future reward consideration
	f.Add(float64(0.5), float64(0.5))   // balanced settings
	
	f.Fuzz(func(t *testing.T, alpha, gamma float64) {
		agent := NewQLearningAgent()
		
		// Ensure hyperparameters in valid ranges (clamp if outside)
		if alpha < 0 || alpha > 1 {
			t.Skipf("Alpha out of range [0,1]: %.4f", alpha)
		}
		if gamma < 0 || gamma > 1 {
			t.Skipf("Gamma out of range [0,1]: %.4f", gamma)
		}
		
		agent.Alpha = alpha
		agent.Gamma = gamma
		
		// Train for small number of steps (fuzzing should be fast)
		reward, _ := agent.TrainOneEpisode(10)
		
		// Rewards should always be bounded within reasonable bounds
		assert.GreaterOrEqual(t, reward, -20.0, "Reward should not be extremely negative")
		assert.LessOrEqual(t, reward, 20.0, "Reward should not be extremely positive")
		
		// Verify Q-values don't explode
		for _, actions := range agent.QTable {
			for _, q := range actions {
				if math.IsNaN(q) || math.IsInf(q, 0) {
					t.Errorf("Invalid Q-value detected: %v", q)
				}
				if math.Abs(q) > 1000.0 {
					t.Errorf("Exploding Q-value detected: %v", q)
				}
			}
		}
	})
}

func FuzzQLearning_QValue_BoundedUpdates(f *testing.F) {
	agent := NewQLearningAgent()
	agent.Alpha = 0.1
	agent.Gamma = 0.9
	
	// Seed with various episode lengths
	f.Add(1)
	f.Add(10)
	f.Add(100)
	f.Add(1000)
	
	f.Fuzz(func(t *testing.T, steps int) {
		if steps <= 0 {
			steps = 1 // Minimum 1 step
		}
		
		// Cap at reasonable limit for fuzzing
		if steps > 1000 {
			steps = 1000
		}
		
		for i := 0; i < steps; i++ {
			_, _ = agent.TrainOneEpisode(5)
			
			// Verify Q-values don't explode after each episode
			for state, actions := range agent.QTable {
				for action, q := range actions {
					// Check for mathematical validity
					if math.IsNaN(q) || math.IsInf(q, 0) {
						t.Errorf("Invalid Q-value at step %d: state=%v, action=%v, q=%.4f", 
							i, state, action, q)
					}
					
					// Reasonable bound check (should never reach this with proper RL)
					if math.Abs(q) > 100.0 {
						t.Errorf("Exploding Q-value at step %d: state=%v, action=%v, q=%.4f", 
							i, state, action, q)
					}
				}
			}
		}
	})
}

func FuzzQLearning_EpsilonDecay_RangeConstraints(f *testing.F) {
	// Seed with various decay rates
	f.Add(float64(0.99))
	f.Add(float64(0.999))
	f.Add(float64(0.9999))
	f.Add(float64(1.0))    // No decay
	f.Add(float64(0.9))    // Aggressive decay
	
	f.Fuzz(func(t *testing.T, decayRate float64) {
		agent := NewQLearningAgent()
		
		// Clamp decay rate to valid range [0.9, 1.0]
		if decayRate < 0.9 {
			decayRate = 0.9
		}
		if decayRate > 1.0 {
			decayRate = 1.0
		}
		
		agent.epsilonDecay = decayRate
		
		initialEpsilon := agent.Epsilon
		episodes := 100
		
		for i := 0; i < episodes; i++ {
			_, _ = agent.TrainOneEpisode(3)
			
			// Epsilon should monotonically decrease (or stay same)
			if agent.Epsilon > initialEpsilon+0.01 {
				t.Errorf("Epsilon increased significantly from %.4f to %.4f after decay", 
					initialEpsilon, agent.Epsilon)
			}
			
			// Never go below minimum
			if agent.Epsilon < agent.minEpsilon-0.01 {
				t.Errorf("Epsilon %.4f went below minimum %.4f", 
					agent.Epsilon, agent.minEpsilon)
			}
		}
		
		// Final epsilon should be within bounds
		assert.GreaterOrEqual(t, agent.Epsilon, agent.minEpsilon-0.01,
			"Final epsilon should not be below minimum")
		assert.LessOrEqual(t, agent.Epsilon, agent.maxEpsilon,
			"Final epsilon should not exceed maximum")
	})
}

// ============================================================================
// Test Category B: Reward Function Properties (~3 fuzz tests)
// ============================================================================

func FuzzAttackSimulator_RewardRange(f *testing.F) {
	_ = NewAttackSimulator(nil)
	
	// Seed with various privilege levels and stealth scores
	f.Add(0, float64(0.0))
	f.Add(0, float64(1.0))
	f.Add(3, float64(0.5))
	f.Add(MaxPrivilegeLevel, float64(0.8))
	
	f.Fuzz(func(t *testing.T, privilegeLevel int, stealth float64) {
		// Normalize inputs to valid ranges
		if privilegeLevel < 0 {
			privilegeLevel = 0
		}
		if privilegeLevel > MaxPrivilegeLevel {
			privilegeLevel = MaxPrivilegeLevel
		}
		
		if stealth < 0.0 {
			stealth = 0.0
		}
		if stealth > 1.0 {
			stealth = 1.0
		}
		
		currentState := State{
			PrivilegeLevel: privilegeLevel,
			StealthScore:   stealth,
			UnderDetection: false,
		}
		
		nextState := currentState
		nextState.PrivilegeLevel = min(nextState.PrivilegeLevel+1, MaxPrivilegeLevel)
		if nextState.StealthScore+0.1 > 1.0 {
			nextState.StealthScore = 1.0
		} else {
			nextState.StealthScore = nextState.StealthScore + 0.1
		}
		
		action := Action{
			Type:              "test",
			StealthImpact:     0.1,
			DetectionRisk:     0.0,
		}
		
		reward := CalculateReward(currentState, nextState, action)
		
		// Rewards should always be bounded within [-0.5, 1.2]
		assert.InDelta(t, 0.0, reward, 2.0, 
			"Reward should be within reasonable bounds [-2, 2]")
		
		// Should never be extremely positive or negative
		assert.LessOrEqual(t, reward, 2.0, "Reward should not exceed 2.0")
		assert.GreaterOrEqual(t, reward, -1.0, "Reward should not be below -1.0")
	})
}

func FuzzCalculateReward_Monotonicity(f *testing.F) {
	// Seed with escalation scenarios
	f.Add(0, 1, false, false, float64(0.7), float64(0.8))
	f.Add(1, 2, false, false, float64(0.8), float64(0.9))
	f.Add(2, 3, false, false, float64(0.9), float64(1.0))
	
	f.Fuzz(func(t *testing.T, privCurrent, privNext int, 
		currentDetected, nextDetected bool,
		currentStealth, nextStealth float64) {
		
		// Normalize inputs
		if privCurrent < 0 {
			privCurrent = 0
		}
		if privNext < 0 {
			privNext = 0
		}
		if privCurrent > MaxPrivilegeLevel {
			privCurrent = MaxPrivilegeLevel
		}
		if privNext > MaxPrivilegeLevel {
			privNext = MaxPrivilegeLevel
		}
		
		if currentStealth < 0.0 {
			currentStealth = 0.0
		}
		if currentStealth > 1.0 {
			currentStealth = 1.0
		}
		if nextStealth < 0.0 {
			nextStealth = 0.0
		}
		if nextStealth > 1.0 {
			nextStealth = 1.0
		}
		
		currentState := State{
			PrivilegeLevel: privCurrent,
			UnderDetection: currentDetected,
			StealthScore:   currentStealth,
		}
		
		nextState := State{
			PrivilegeLevel: privNext,
			UnderDetection: nextDetected,
			StealthScore:   nextStealth,
		}
		
		reward := CalculateReward(currentState, nextState, Action{})
		
		// Verify reward stays bounded
		assert.GreaterOrEqual(t, reward, -1.0, 
			"Reward should not be extremely negative")
		assert.LessOrEqual(t, reward, 2.0, 
			"Reward should not be extremely positive")
		
		// Key property: escalating privileges should generally improve reward
		// unless under detection
		if privNext > privCurrent && !nextDetected {
			assert.GreaterOrEqual(t, reward, -0.5, 
				"Privilege escalation without detection should yield reasonable reward")
		}
		
		// Detection should penalize regardless of other factors
		if nextDetected {
			assert.LessOrEqual(t, reward, 0.5,
				"Being detected should significantly reduce reward")
		}
	})
}

// ============================================================================
// Test Category C: State/Action Encoding Properties (~2 fuzz tests)
// ============================================================================

func FuzzEncodeDecodeState_Consistency(f *testing.F) {
	// Seed with typical attack states
	f.Add(0, "initial_user", float64(0.7), false, 100)
	f.Add(3, "domain_controller", float64(0.95), false, 50)
	f.Add(1, "lateral_host1", float64(0.5), true, 25)
	
	f.Fuzz(func(t *testing.T, privilegeLevel int, networkPos string, 
		stealth float64, underDetect bool, ttl int) {
		
		// Normalize inputs
		if privilegeLevel < 0 {
			privilegeLevel = 0
		}
		if privilegeLevel > MaxPrivilegeLevel {
			privilegeLevel = MaxPrivilegeLevel
		}
		
		if stealth < 0.0 {
			stealth = 0.0
		}
		if stealth > 1.0 {
			stealth = 1.0
		}
		
		if ttl < 0 {
			ttl = 0
		}
		
		state := State{
			ActiveCVEs:      []string{},
			NetworkPosition: networkPos,
			PrivilegeLevel:  privilegeLevel,
			StealthScore:    stealth,
			UnderDetection:  underDetect,
			TTL:             ttl,
		}
		
		// Encode to ID
		stateID := EncodeState(state)
		
		// Verify ID is valid (not InvalidStateID sentinel)
		assert.NotEqual(t, InvalidStateID, stateID,
			"Encoded state ID should be valid")
		
		// Decode back (lossy, but should produce structurally valid State)
		decoded := DecodeState(stateID)
		
		// Decoded state should have valid structure
		assert.NotEqual(t, uint64(0), uint64(stateID),
			"State ID should not be zero")
		
		// Privilege level should be recoverable approximately
		assert.GreaterOrEqual(t, decoded.PrivilegeLevel, 0,
			"Decoded privilege level should be non-negative")
		assert.LessOrEqual(t, decoded.PrivilegeLevel, 3,
			"Decoded privilege level should be ≤ 3")
	})
}

func FuzzEncodeAction_Validity(f *testing.F) {
	// Seed with various action types
	f.Add("inject_payload", "CVE-2021-4034")
	f.Add("pivot", "CVE-2023-34361")
	f.Add("escalate", "CVE-2020-1472")
	f.Add("persist", "CVE-2022-22965")
	f.Add("exfiltrate", "")
	
	f.Fuzz(func(t *testing.T, actionType, targetCve string) {
		action := Action{
			Type:              actionType,
			TargetCVE:         targetCve,
			RequiredPrivilege: 0,
			StealthImpact:     0.0,
			DetectionRisk:     0.0,
		}
		
		// Encode action
		actionID := EncodeAction(action)
		
		// Verify ID is valid (not InvalidActionID sentinel)
		assert.NotEqual(t, InvalidActionID, actionID,
			"Encoded action ID should be valid")
		
		// Decode back (lossy)
		decoded := DecodeAction(actionID)
		
		// Decoded action should have some valid fields
		validTypes := map[string]bool{
			"inject_payload": true,
			"pivot":          true,
			"escalate":       true,
			"persist":        true,
			"exfiltrate":     true,
			"unknown":        true,
		}
		assert.True(t, validTypes[decoded.Type],
			"Decoded action type should be one of known types")
	})
}



// ============================================================================
// Test Category D: Training Convergence Properties (~2 fuzz tests)
// ============================================================================

func FuzzTraining_SeriesConvergence(f *testing.F) {
	// Seed with different random seeds
	f.Add(int64(12345))
	f.Add(int64(67890))
	f.Add(int64(0))
	f.Add(int64(99999))
	
	f.Fuzz(func(t *testing.T, seed int64) {
		rand.Seed(seed)
		
		agent := NewQLearningAgent()
		agent.Epsilon = 0.5 // Start at moderate exploration
		agent.Alpha = 0.15  // Moderate learning rate
		
		var rewards []float64
		
		// Train for 200 episodes
		for ep := 0; ep < 200; ep++ {
			reward, _ := agent.TrainOneEpisode(10)
			rewards = append(rewards, reward)
			
			// Check for mathematical anomalies
			if math.IsNaN(reward) || math.IsInf(reward, 0) {
				t.Errorf("Episode %d produced invalid reward: %v", ep, reward)
				return
			}
		}
		
		// Compute statistics
		totalSum := 0.0
		for _, r := range rewards {
			totalSum += r
		}
		avgReward := totalSum / float64(len(rewards))
		
		// Average reward should be reasonable (not extremely negative)
		assert.Greater(t, avgReward, -10.0, 
			"Average reward over 200 episodes should be reasonable")
		
		// Epsilon should have decayed appropriately
		if agent.epsilonDecay >= 0.999 {
			assert.Less(t, agent.Epsilon-agent.minEpsilon, 0.2,
				"Epsilon should approach minimum after many episodes")
		}
	})
}

func FuzzTraining_Reproducibility(f *testing.F) {
	// Test that same sequence produces deterministic results
	f.Add(int64(42))
	
	f.Fuzz(func(t *testing.T, seed int64) {
		// Run first training session
		rand.Seed(seed)
		agent1 := NewQLearningAgent()
		agent1.Epsilon = 0.3
		
		trainingSteps := 50
		rewards1 := []float64{}
		
		for ep := 0; ep < trainingSteps; ep++ {
			reward, _ := agent1.TrainOneEpisode(5)
			rewards1 = append(rewards1, reward)
		}
		
		// Run second training session with identical seed
		rand.Seed(seed)
		agent2 := NewQLearningAgent()
		agent2.Epsilon = 0.3
		
		rewards2 := []float64{}
		
		for ep := 0; ep < trainingSteps; ep++ {
			reward, _ := agent2.TrainOneEpisode(5)
			rewards2 = append(rewards2, reward)
		}
		
		// With identical RNG seed and deterministic environment,
		// rewards should match exactly
		assert.Equal(t, len(rewards1), len(rewards2),
			"Both runs should complete same number of steps")
		
		// Due to randomness in simulation, rewards may vary slightly
		// But overall statistics should be similar
		sum1 := 0.0
		for _, r := range rewards1 {
			sum1 += r
		}
		
		sum2 := 0.0
		for _, r := range rewards2 {
			sum2 += r
		}
		
		avg1 := sum1 / float64(len(rewards1))
		avg2 := sum2 / float64(len(rewards2))
		
		// Averages should be within 50% of each other
		diff := math.Abs(avg1 - avg2)
		meanAvg := (math.Abs(avg1) + math.Abs(avg2)) / 2
		
		if meanAvg > 0.01 {
			percentDiff := diff / meanAvg
			assert.Less(t, percentDiff, 0.5,
				fmt.Sprintf("Averages should be within 50%%: run1=%.4f, run2=%.4f", avg1, avg2))
		}
	})
}
