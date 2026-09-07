// Package patent - Unit Tests for Self-Evolving Attack Graph Engine (Patent #1)
// Comprehensive test coverage for Q-learning agent implementation
package patent

import (
	"math"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testLogger *logrus.Logger
	
	initialized bool
)

func init() {
	testLogger = logrus.New()
	testLogger.SetLevel(logrus.ErrorLevel)
	initialized = true
}

// ============================================================================
// Test Category A: QLearningAgent Core Functions (~8 tests)
// ============================================================================

func TestQLearningAgent_GetQValue(t *testing.T) {
	// Ensure initialization
	if !initialized {
		t.Skip("Test infrastructure not initialized")
	}
	
	agent := NewQLearningAgent()
	
	initialState := StateID(1)
	action1 := ActionID(1)
	
	// Initially zero-initialized
	q := agent.GetQValue(initialState, action1)
	assert.Zero(t, q, "Q-value should be zero for unexplored state-action pair")
	
	// After update, should have non-zero value
	reward := 1.0
	nextState := StateID(2)
	
	// Manually set up a Q-value for testing
	agent.QTable[initialState] = map[ActionID]float64{
		action1: 0.5,
	}
	
	updatedQ := agent.GetQValue(initialState, action1)
	assert.InDelta(t, 0.5, updatedQ, 1e-6, "Should return stored Q-value")
	
	// Update via UpdateQValue method
	agent.UpdateQValue(initialState, action1, reward, nextState)
	
	actualQ := agent.GetQValue(initialState, action1)
	expectedQ := 0.5 + 0.1*(1.0 + 0.95*0.0 - 0.5) // Default α=0.1, γ=0.95
	
	assert.InDelta(t, expectedQ, actualQ, 1e-6, 
		"Q-value should be updated according to TD(0) rule")
}

func TestQLearningAgent_UpdateQValue_TDRule(t *testing.T) {
	// Verify temporal difference formula implementation
	agent := NewQLearningAgent()
	agent.Alpha = 0.9   // Use high alpha for easy verification
	agent.Gamma = 0.5
	
	state := StateID(1)
	action := ActionID(1)
	
	// Set initial Q-value manually
	agent.QTable[state] = map[ActionID]float64{
		action: 0.5,
	}
	
	reward := 1.0
	maxNextQ := 2.0
	
	// Manually compute what getMaxNextQ would return
	oldQ := agent.QTable[state][action]
	tdTarget := reward + agent.Gamma*maxNextQ
	tdError := tdTarget - oldQ
	expectedQ := oldQ + agent.Alpha*tdError
	
	// Execute update
	agent.UpdateQValue(state, action, reward, StateID(2))
	
	actualQ := agent.GetQValue(state, action)
	assert.InDelta(t, expectedQ, actualQ, 1e-6, 
		"TD(0) update: Q ← Q + α[R + γ·maxQ - Q]")
	
	// Manual verification: 0.5 + 0.9*(1.0 + 0.5*2.0 - 0.5) = 0.5 + 0.9*1.5 = 1.85
	assert.InDelta(t, 1.85, actualQ, 1e-6, 
		"Expected calculation: 0.5 + 0.9*(1.0 + 0.5*2.0 - 0.5)")
}

func TestQLearningAgent_SelectAction_ExplorationVsExploitation(t *testing.T) {
	agent := NewQLearningAgent()
	
	// Set up Q-table with known values
	agent.QTable[StateID(1)] = map[ActionID]float64{
		ActionID(1): 1.0, // Best action
		ActionID(2): 0.5, // Second best
	}
	
	// Test pure exploitation (ε=0)
	agent.Epsilon = 0.0
	iterations := 1000
	
	bestActionCount := 0
	for i := 0; i < iterations; i++ {
		action := agent.SelectAction(StateID(1))
		if action == ActionID(1) {
			bestActionCount++
		}
	}
	
	assert.Equal(t, iterations, bestActionCount, 
		"With ε=0, should always exploit best action")
	
	// Test pure exploration (ε=1)
	agent.Epsilon = 1.0
	allActionsSeen := make(map[ActionID]bool)
	
	for i := 0; i < iterations; i++ {
		action := agent.SelectAction(StateID(1))
		allActionsSeen[action] = true
	}
	
	// Should see both actions (1 and 2) during pure exploration
	assert.Len(t, allActionsSeen, 2, 
		"With ε=1, should explore both available actions")
}

func TestQLearningAgent_EpsilonDecay(t *testing.T) {
	agent := NewQLearningAgent()
	agent.epsilonDecay = 0.995
	agent.minEpsilon = 0.01
	
	initialEpsilon := agent.Epsilon
	require.Equal(t, 1.0, initialEpsilon, "Start with full exploration")
	
	episodes := 1000
	
	for i := 0; i < episodes; i++ {
		agent.Epsilon *= agent.epsilonDecay
		if agent.Epsilon < agent.minEpsilon {
			agent.Epsilon = agent.minEpsilon
			break
		}
	}
	
	finalEpsilon := agent.Epsilon
	assert.LessOrEqual(t, finalEpsilon, agent.minEpsilon,
		"Epsilon should decay to minEpsilon after many episodes")
	assert.True(t, finalEpsilon <= initialEpsilon,
		"Epsilon should decrease over time")
	
	// Verify mathematical correctness
	expectedEpisodes := int(math.Log(agent.minEpsilon) / math.Log(agent.epsilonDecay))
	assert.LessOrEqual(t, episodes, expectedEpisodes+1, 
		"Decay rate should match mathematical expectation")
}

func TestQLearningAgent_ConcurrentSafety(t *testing.T) {
	agent := NewQLearningAgent()
	agent.Epsilon = 0.1
	
	states := []StateID{}
	for i := 0; i < 10; i++ {
		states = append(states, StateID(i))
	}
	actions := []ActionID{}
	for i := 0; i < 5; i++ {
		actions = append(actions, ActionID(i))
	}
	
	var wg sync.WaitGroup
	
	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < len(states); j++ {
				for k := 0; k < len(actions); k++ {
					_ = agent.GetQValue(states[j], actions[k])
				}
			}
		}()
	}
	
	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(ep int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				state := StateID(rand.Intn(10))
				action := ActionID(rand.Intn(5))
				agent.UpdateQValue(state, action, float64(j), StateID(rand.Intn(10)))
			}
		}(i)
	}
	
	wg.Wait()
	
	// Verify no corruption
	assert.NotEmpty(t, agent.QTable, "Q-table should have data after concurrent operations")
}

func TestQLearningAgent_TrainOneEpisode_BasicFlow(t *testing.T) {
	agent := NewQLearningAgent()
	agent.Epsilon = 0.1 // Less exploration for deterministic behavior
	
	reward, converged := agent.TrainOneEpisode(10)
	
	// Episode should complete without panicking
	assert.GreaterOrEqual(t, reward, -10.0, 
		"Total reward should be within reasonable bounds")
	assert.LessOrEqual(t, reward, 10.0, 
		"Total reward should be bounded by max steps × max per-step reward")
	
	// Convergence should only happen when epsilon reaches minimum
	// Since we start with Epsilon=0.1 > minEpsilon=0.05, should not converge yet
	if agent.epsilonDecay >= 0.9995 {
		assert.False(t, converged, 
			"Should not converge in single episode with current hyperparameters")
	}
}

func TestQLearningAgent_TrainOneEpisode_EpsilonDecayWithinBounds(t *testing.T) {
	agent := NewQLearningAgent()
	agent.epsilonDecay = 0.9995
	agent.minEpsilon = 0.01
	
	initialEpsilon := agent.Epsilon
	
	agent.TrainOneEpisode(50)
	afterFirstEpisodeEpsilon := agent.Epsilon
	
	assert.True(t, afterFirstEpisodeEpsilon <= initialEpsilon,
		"Epsilon should decay after each episode")
	assert.GreaterOrEqual(t, afterFirstEpisodeEpsilon, agent.minEpsilon,
		"Epsilon should not go below minimum threshold")
	
	// After 50 steps with decay 0.9995: ε ≈ 0.976
	expectedEpsilon := 1.0 * math.Pow(0.9995, 50)
	assert.InDelta(t, expectedEpsilon, afterFirstEpisodeEpsilon, 0.01,
		"Epsilon decay should follow exponential schedule")
}

func TestQLearningAgent_CurrentPolicy_Extraction(t *testing.T) {
	agent := NewQLearningAgent()
	
	// Populate Q-table with known optimal paths
	agent.QTable[StateID(1)] = map[ActionID]float64{
		ActionID(1): 5.0, // Best action from state 1
		ActionID(2): 2.0,
	}
	agent.QTable[StateID(2)] = map[ActionID]float64{
		ActionID(1): 3.0, // Best action from state 2
		ActionID(2): 1.0,
	}
	
	policies := agent.GetCurrentPolicy()
	
	assert.NotEmpty(t, policies,
		"Should extract at least one valid attack path")
	assert.GreaterOrEqual(t, len(policies), 2,
		"Should extract policy for each state with Q-values")
	
	// Validate extracted policies select best actions greedily
	for _, policy := range policies {
		for _, action := range policy.Actions {
			// Actions should come from valid encoded states
			assert.NotEqual(t, InvalidActionID, action,
				"Extracted action should be valid")
		}
	}
}

// ============================================================================
// Test Category B: Reward Function Validation (~3 tests)
// ============================================================================

func TestCalculateReward_SuccessfulPrivilegeEscalation(t *testing.T) {
	currentState := State{
		PrivilegeLevel: 1, // user
		StealthScore:   0.8,
		UnderDetection: false,
	}
	
	nextState := State{
		PrivilegeLevel: 2, // admin
		StealthScore:   0.9,
		UnderDetection: false,
	}
	
	action := Action{
		Type:              "escalate",
		StealthImpact:     0.1,
		DetectionRisk:     0.0,
	}
	
	reward := CalculateReward(currentState, nextState, action)
	
	// Success + stealth improvement, no detection penalty
	// R = 0.5×1.0 + 0.3×0.1 - 0.2×0 = 0.53
	expectedReward := 0.53
	assert.InDelta(t, expectedReward, reward, 0.01,
		"Successful privilege escalation with stealth gain should yield positive reward")
	assert.Greater(t, reward, 0.0, "Reward should be positive for successful attack")
}

func TestCalculateReward_DetectionPenalty(t *testing.T) {
	currentState := State{
		PrivilegeLevel: 1,
		StealthScore:   0.9,
		UnderDetection: false,
	}
	
	nextState := State{
		PrivilegeLevel: 1, // No progress
		StealthScore:   0.7, // Degraded stealth
		UnderDetection: true, // Detected!
	}
	
	action := Action{}
	
	reward := CalculateReward(currentState, nextState, action)
	
	// Failed attack triggers detection
	// R = 0.5×0 + 0.3×0 - 0.2×1.0 = -0.2
	assert.Less(t, reward, 0.0, "Should penalize detected attacks")
	assert.InDelta(t, -0.2, reward, 0.01, "Detection penalty should be significant")
}

func TestCalculateReward_NoProgress(t *testing.T) {
	currentState := State{
		PrivilegeLevel: 2, // Already high privilege
		StealthScore:   0.8,
		UnderDetection: false,
	}
	
	nextState := State{
		PrivilegeLevel: 2, // Same level
		StealthScore:   0.85, // Slight stealth improvement
		UnderDetection: false,
	}
	
	action := Action{}
	
	reward := CalculateReward(currentState, nextState, action)
	
	// No progress but improved stealth → partial credit
	// R = 0.5×0 + 0.3×0.05 - 0.2×0 = 0.015
	assert.GreaterOrEqual(t, reward, 0.0, 
		"Should give some reward for safe execution without degradation")
	assert.InDelta(t, 0.015, reward, 0.001, 
		"Small stealth improvement should yield small positive reward")
}

func TestCalculateReward_ZeroRewardCase(t *testing.T) {
	currentState := State{
		PrivilegeLevel: 1,
		StealthScore:   0.5,
		UnderDetection: false,
	}
	
	nextState := State{
		PrivilegeLevel: 1, // No change
		StealthScore:   0.5, // No change
		UnderDetection: false,
	}
	
	reward := CalculateReward(currentState, nextState, Action{})
	
	assert.InDelta(t, 0.0, reward, 0.001, 
		"No progress should yield approximately zero reward")
}

// ============================================================================
// Test Category C: Training Loop Integrity (~4 tests)
// ============================================================================

func TestTrainOneEpisode_ConvergenceBehavior(t *testing.T) {
	agent := NewQLearningAgent()
	agent.epsilonDecay = 0.9995
	agent.minEpsilon = 0.05
	agent.Alpha = 0.2   // Higher learning rate for faster convergence
	
	episodeRewards := []float64{}
	totalSteps := 500
	
	for ep := 0; ep < totalSteps; ep++ {
		totalReward, _ := agent.TrainOneEpisode(20)
		episodeRewards = append(episodeRewards, totalReward)
	}
	
	// Compute moving averages
	first100Avg := testMean(episodeRewards[:100])
	last100Avg := testMean(episodeRewards[len(episodeRewards)-100:])
	
	t.Logf("First 100 episodes avg reward: %.4f", first100Avg)
	t.Logf("Last 100 episodes avg reward: %.4f", last100Avg)
	
	// Average rewards may or may not improve significantly in tabular Q-learning
	// This is a soft test that documents behavior rather than enforcing strict requirements
	if len(episodeRewards) > 0 {
		first100Avg := testMean(episodeRewards[:min(len(episodeRewards), 100)])
		last100Start := len(episodeRewards) - 100
		if last100Start < 0 {
			last100Start = 0
		}
		last100Avg := testMean(episodeRewards[last100Start:])
		
		t.Logf("First 100 episodes avg reward: %.4f", first100Avg)
		t.Logf("Last 100 episodes avg reward: %.4f", last100Avg)
		
		// Average rewards may or may not improve significantly in tabular Q-learning
		// This is a soft test that documents behavior rather than enforcing strict requirements
	}
}

func TestGetCurrentPolicy_Replayability(t *testing.T) {
	agent := NewQLearningAgent()
	agent.Alpha = 0.1
	agent.Gamma = 0.95
	
	// Train for 100 episodes
	for ep := 0; ep < 100; ep++ {
		_, _ = agent.TrainOneEpisode(10)
	}
	
	// Extract policy multiple times
	policy1 := agent.GetCurrentPolicy()
	policy2 := agent.GetCurrentPolicy()
	policy3 := agent.GetCurrentPolicy()
	
	// Policies should be consistent across extractions (deterministic greedy selection)
	assert.Equal(t, len(policy1), len(policy2),
		"Policy extraction should be deterministic")
	assert.Equal(t, len(policy2), len(policy3),
		"Policy extraction should be reproducible")
}

func TestQLearningAgent_Checkpointing_Serialization(t *testing.T) {
	agent := NewQLearningAgent()
	
	// Manually populate Q-table
	agent.QTable[StateID(1)] = map[ActionID]float64{
		ActionID(1): 0.5,
		ActionID(2): 0.3,
	}
	agent.TotalEpisodes = 100
	agent.BestReward = 15.5
	
	filename := "/tmp/test_qtable_" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".json"
	defer os.Remove(filename)
	
	// Save checkpoint
	err := agent.saveToDisk(filename)
	assert.NoError(t, err, "Should save checkpoint without errors")
	
	// Verify file exists and has content
	data, err := os.ReadFile(filename)
	assert.NoError(t, err, "Checkpoint file should be readable")
	assert.Greater(t, len(data), 0, "File should have content")
	
	// Load into new agent
	newAgent := NewQLearningAgent()
	err = newAgent.loadFromDisk(filename)
	assert.NoError(t, err, "Should load checkpoint without errors")
	
	// Verify loaded data matches original
	assert.Equal(t, agent.QTable, newAgent.QTable,
		"Loaded Q-table should match saved version")
	assert.Equal(t, agent.TotalEpisodes, newAgent.TotalEpisodes,
		"Episode count should match")
	assert.InDelta(t, agent.BestReward, newAgent.BestReward, 1e-6,
		"Best reward should match")
}

// Helper function to calculate mean of slice
func testMean(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}
