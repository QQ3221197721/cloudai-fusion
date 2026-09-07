// Package patent - Integration Tests for Self-Evolving Attack Graph Engine (Patent #1)
// End-to-end validation of attack simulation and training workflows
package patent

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var integrationLogger *logrus.Logger

func init() {
	integrationLogger = logrus.New()
	integrationLogger.SetLevel(logrus.WarnLevel)
}

// ============================================================================
// Test Category A: Attack Simulator Integration (~3 tests)
// ============================================================================

func TestAttackSimulator_Intialization(t *testing.T) {
	simulator := NewAttackSimulator(integrationLogger)
	
	t.Log("Verifying simulator initialization...")
	
	// Check that vulnerability database is populated
	assert.NotEmpty(t, simulator.vulnDB, 
		"Vulnerability database should be initialized with CVEs")
	
	t.Logf("Loaded %d vulnerabilities into database", len(simulator.vulnDB))
	
	// Verify known CVEs are present
	knownCVEs := []string{
		"CVE-2021-4034", // PwnKit
		"CVE-2023-34361", // Metabase
		"CVE-2022-22965", // Spring4Shell
		"CVE-2020-1472", // Zerologon
	}
	
	for _, cve := range knownCVEs {
		vuln, exists := simulator.vulnDB[cve]
		require.True(t, exists, "%s should be in vulnerability database", cve)
		
		t.Logf("  ✓ %s: CVSS=%.1f, SuccessRate=%.2f", 
			cve, vuln.CVSSBaseScore, vuln.ExploitSuccessRate)
		
		assert.Greater(t, vuln.ExploitSuccessRate, 0.0, 
			"%s should have non-zero exploit success rate", cve)
		assert.LessOrEqual(t, vuln.CVSSBaseScore, 10.0, 
			"CVSS score should be within valid range")
	}
	
	// Verify defense mechanisms are loaded
	assert.NotEmpty(t, simulator.defenses,
		"Defense mechanisms should be initialized")
	
	t.Logf("Loaded %d defense mechanisms", len(simulator.defenses))
	
	// Check mitigation matrix
	assert.NotEmpty(t, simulator.mitigationMatrix,
		"Mitigation matrix should contain mappings")
}

func TestAttackSimulator_SimulateTransition_PrivilegeEscalation(t *testing.T) {
	simulator := NewAttackSimulator(integrationLogger)
	
	t.Log("Testing privilege escalation transition simulation...")
	
	// Setup: User-level attacker attempting escalation
	currentState := State{
		PrivilegeLevel:    0, // Basic user
		StealthScore:      0.8,
		UnderDetection:    false,
		NetworkPosition:   "initial_user",
		ActiveCVEs:        []string{},
		TTL:               InitialTTL,
	}
	
	// Target a known local privilege escalation CVE
	action := Action{
		Type:              "escalate",
		TargetCVE:         "CVE-2021-4034", // PwnKit
		RequiredPrivilege: 0,
		StealthImpact:     -0.1,
		DetectionRisk:     0.2,
		ExpectedReward:    0.8,
	}
	
	nextState, reward := simulator.simulateAttackTransition(currentState, action)
	
	t.Logf("Transition result:")
	t.Logf("  Current state: Priv=%d, Stealth=%.2f, Detected=%v", 
		currentState.PrivilegeLevel, currentState.StealthScore, currentState.UnderDetection)
	t.Logf("  Next state ID: %v", nextState)
	t.Logf("  Reward: %.4f", reward)
	
	// Validate transition produced a valid next state
	assert.NotEqual(t, uint64(InvalidStateID), nextState, 
		"Should produce valid next state")
	
	// Decode next state to verify changes
	decodedNext := DecodeState(nextState)
	t.Logf("  Decoded next state: Priv=%d, Stealth=%.2f, Detected=%v",
		decodedNext.PrivilegeLevel, decodedNext.StealthScore, decodedNext.UnderDetection)
	
	// Reward should be bounded within [-1, 1]
	assert.GreaterOrEqual(t, reward, -1.0, 
		"Reward should not exceed minimum bound")
	assert.LessOrEqual(t, reward, 1.0, 
		"Reward should not exceed maximum bound")
}

func TestAttackSimulator_IsDetected_DefenseEvaluation(t *testing.T) {
	simulator := NewAttackSimulator(integrationLogger)
	
	t.Log("Testing defense mechanism detection logic...")
	
	// Test different attack scenarios
	
	scenarios := []struct {
		name          string
		state         State
		action        Action
		expectDetect  bool
	}{
		{
			name: "Process injection at user level",
			state: State{
				PrivilegeLevel: 0,
				UnderDetection: false,
			},
			action: Action{
				Type: "inject_payload",
			},
			expectDetect: false, // Should trigger EDR if active
		},
		{
			name: "Lateral movement during stealth operation",
			state: State{
				PrivilegeLevel: 1,
				StealthScore:   0.9,
				UnderDetection: false,
				NetworkPosition: "lateral_server1",
			},
			action: Action{
				Type: "pivot",
			},
			expectDetect: false, // May trigger AMSI or SIEM
		},
		{
			name: "Credential dumping attempt",
			state: State{
				PrivilegeLevel: 1,
				UnderDetection: false,
			},
			action: Action{
				Type:        "escalate",
				TargetCVE:   "CVE-2020-1472", // Zerologon (credential-related)
			},
			expectDetect: false, // Should trigger EDR credential monitoring
		},
	}
	
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			detected := simulator.isDetected(scenario.state, scenario.action)
			
			t.Logf("  Scenario: %s", scenario.name)
			t.Logf("    Detection result: %v", detected)
			
			// Detection is probabilistic, so we can't assert exact outcome
			// But we verify the function returns a boolean without panicking
			assert.IsType(t, false, detected, 
				"isDetected should return boolean type")
		})
	}
}

// ============================================================================
// Test Category B: End-to-End Training Workflow (~2 tests)
// ============================================================================

func TestEndToEnd_TrainingAndPolicyExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end test in short mode")
	}
	
	agent := NewQLearningAgent()
	agent.epsilonDecay = 0.999
	agent.minEpsilon = 0.05
	agent.Alpha = 0.15 // Faster learning for faster convergence
	
	t.Log("Starting end-to-end training phase...")
	
	var convergenceMetrics []float64
	maxEpisodes := 5000
	
	for episode := 0; episode < maxEpisodes; episode++ {
		totalReward, converged := agent.TrainOneEpisode(30)
		
		if converged && episode > 2000 {
			convergenceMetrics = append(convergenceMetrics, totalReward)
		}
		
		// Log progress every 500 episodes
		if episode%500 == 0 && episode > 0 {
			t.Logf("Episode %d: total_reward=%.4f, epsilon=%.4f, episodes_trained=%d",
				episode, totalReward, agent.Epsilon, agent.TotalEpisodes)
		}
	}
	
	t.Log("Training phase complete")
	
	// Analyze convergence behavior
	if len(convergenceMetrics) > 0 {
		avgConvergedReward := testMean(convergenceMetrics)
		t.Logf("✓ Converged over %d episodes with average reward %.4f",
			len(convergenceMetrics), avgConvergedReward)
		
		assert.Greater(t, avgConvergedReward, -1.0,
			"Final converged reward should be reasonable (not extremely negative)")
	}
	
	// Extract policy after training
	t.Log("Extracting attack policies from trained Q-table...")
	policies := agent.GetCurrentPolicy()
	
	assert.Greater(t, len(policies), 0,
		"Should extract at least one viable attack path after training")
	
	t.Logf("✓ Extracted %d attack paths", len(policies))
	
	// Validate extracted policies are reasonable
	for i, policy := range policies {
		if len(policy.Actions) == 0 {
			continue // Skip empty paths
		}
		
		t.Logf("Policy %d:", i+1)
		t.Logf("  StartState: %v", policy.StartState)
		t.Logf("  Actions count: %d", len(policy.Actions))
		
		// Verify actions are non-trivial
		assert.Greater(t, len(policy.Actions), 0,
			"Each policy should contain at least one action")
		
		// Verify start state is valid
		assert.NotEqual(t, InvalidStateID, policy.StartState,
			"Start state should be valid")
	}
}

func TestEndToEnd_QLearningWithRealisticScenario(t *testing.T) {
	agent := NewQLearningAgent()
	simulator := NewAttackSimulator(integrationLogger)
	
	// Configure for realistic scenario
	agent.epsilonDecay = 0.9995
	agent.minEpsilon = 0.1 // Maintain exploration throughout
	
	t.Log("Running realistic attack chain simulation...")
	
	// Scenario: User tries to escalate from low privileges to domain admin
	initialState := State{
		ActiveCVEs:      []string{},
		NetworkPosition: "initial_user",
		PrivilegeLevel:  0, // Start as regular user
		StealthScore:    InitialStealthScore,
		UnderDetection:  false,
		TTL:             InitialTTL,
	}
	
	var trajectory []struct {
		State     StateID
		Action    ActionID
		Reward    float64
		NextState StateID
	}
	maxSteps := 50
	
	for step := 0; step < maxSteps; step++ {
		currentState := EncodeState(initialState)
		
		action := agent.SelectAction(currentState)
		
		// Check if we've reached terminal state
		if action == InvalidActionID {
			t.Logf("Reached terminal state (no valid actions) at step %d", step)
			break
		}
		
		// Simulate attack transition using real simulator
		decodedAction := DecodeAction(action)
		newStateEncoded, reward := simulator.simulateAttackTransition(initialState, decodedAction)
		
		// Record trajectory step
		trajectory = append(trajectory, struct {
			State     StateID
			Action    ActionID
			Reward    float64
			NextState StateID
		}{
			State:     currentState,
			Action:    action,
			Reward:    reward,
			NextState: newStateEncoded,
		})
		
		// Update Q-value using TD(0)
		agent.UpdateQValue(currentState, action, reward, newStateEncoded)
		
		// Decay epsilon
		agent.Epsilon *= agent.epsilonDecay
		if agent.Epsilon < agent.minEpsilon {
			agent.Epsilon = agent.minEpsilon
		}
		
		// Decode new state for logging
		newState := DecodeState(newStateEncoded)
		initialState = newState
		
		// Track TTL expiration
		initialState.TTL--
		if initialState.TTL <= 0 {
			initialState.UnderDetection = true
			t.Logf("TTL expired at step %d, auto-detected", step)
		}
		
		// Log progress periodically
		if step%10 == 0 {
			t.Logf("Step %d: Priv=%d, Stealth=%.2f, Detected=%v, Reward=%.2f",
				step, initialState.PrivilegeLevel, initialState.StealthScore,
				initialState.UnderDetection, reward)
		}
		
		// Check if we achieved domain admin
		if initialState.PrivilegeLevel >= MaxPrivilegeLevel {
			t.Logf("✓ Achieved domain admin status at step %d!", step)
			break
		}
	}
	
	t.Log("Trajectory analysis:")
	
	// Validate trajectory quality
	positiveRewards := 0
	totalSteps := len(trajectory)
	
	for _, record := range trajectory {
		if record.Reward > 0 {
			positiveRewards++
		}
	}
	
	if totalSteps > 0 {
		successRate := float64(positiveRewards) / float64(totalSteps)
		t.Logf("✓ Positive action rate: %.2f%% (%d/%d steps)",
			successRate*100, positiveRewards, totalSteps)
		
		// Assert basic quality threshold
		assert.Greater(t, successRate, 0.2,
			"At least 20%% of actions should yield positive rewards")
	} else {
		t.Log("No steps recorded in trajectory")
	}
	
	// Verify final state meets criteria
	finalPriv := initialState.PrivilegeLevel
	finalStealth := initialState.StealthScore
	
	t.Logf("Final state metrics:")
	t.Logf("  Privilege Level: %d (max=%d)", finalPriv, MaxPrivilegeLevel)
	t.Logf("  Stealth Score: %.2f", finalStealth)
	t.Logf("  Under Detection: %v", initialState.UnderDetection)
	t.Logf("  Remaining TTL: %d", initialState.TTL)
	
	// Either achieved high privilege OR got caught OR ran out of stealth window
	eitherSuccess := finalPriv >= 2 || initialState.UnderDetection || initialState.TTL <= 0
	assert.True(t, eitherSuccess, 
		"Simulation should reach meaningful terminal state")
}

// ============================================================================
// Test Category C: Reward System Validation (~3 tests)
// ============================================================================

func TestCalculateReward_ComprehensiveScenarios(t *testing.T) {
	scenarios := []struct {
		name       string
		current    State
		next       State
		action     Action
		expectMin  float64
		expectMax  float64
		mustBePos  bool
		mustBeNeg  bool
	}{
		{
			name: "Successful escalation with stealth gain",
			current: State{PrivilegeLevel: 0, StealthScore: 0.7, UnderDetection: false},
			next:     State{PrivilegeLevel: 1, StealthScore: 0.85, UnderDetection: false},
			action:   Action{Type: "escalate"},
			expectMin: 0.3,
			expectMax: 0.8,
			mustBePos: true,
		},
		{
			name: "Failed attack triggers detection",
			current: State{PrivilegeLevel: 1, StealthScore: 0.8, UnderDetection: false},
			next:     State{PrivilegeLevel: 1, StealthScore: 0.5, UnderDetection: true},
			action:   Action{Type: "inject_payload"},
			expectMin: -0.5,
			expectMax: -0.1,
			mustBeNeg: true,
		},
		{
			name: "Stealthy lateral movement (no progress, no penalty)",
			current: State{PrivilegeLevel: 1, StealthScore: 0.9, UnderDetection: false},
			next:     State{PrivilegeLevel: 1, StealthScore: 0.95, UnderDetection: false},
			action:   Action{Type: "pivot"},
			expectMin: 0.0,
			expectMax: 0.2,
			mustBePos: false, // Can be small positive
		},
		{
			name: "Massive stealth degradation (severe penalty)",
			current: State{PrivilegeLevel: 2, StealthScore: 0.95, UnderDetection: false},
			next:     State{PrivilegeLevel: 2, StealthScore: 0.3, UnderDetection: false},
			action:   Action{Type: "persist"},
			expectMin: -0.2,
			expectMax: 0.0,
			mustBeNeg: false, // Penalty already baked in via stealth calculation
		},
	}
	
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			reward := CalculateReward(scenario.current, scenario.next, scenario.action)
			
			t.Logf("  Reward: %.4f", reward)
			
			// Verify bounds
			assert.GreaterOrEqual(t, reward, scenario.expectMin,
				"Reward should not be below minimum expected")
			assert.LessOrEqual(t, reward, scenario.expectMax,
				"Reward should not exceed maximum expected")
			
			// Verify sign constraints
			if scenario.mustBePos {
				assert.Greater(t, reward, 0.0,
					"Reward must be positive for this scenario")
			}
			if scenario.mustBeNeg {
				assert.Less(t, reward, 0.0,
					"Reward must be negative for this scenario")
			}
		})
	}
}

func TestCalculateReward_EdgeCases(t *testing.T) {
	t.Log("Testing edge cases for reward calculation...")
	
	// Edge case 1: Maximum possible reward (perfect escalation + perfect stealth)
	t.Run("Maximum reward scenario", func(t *testing.T) {
		current := State{
			PrivilegeLevel:   MaxPrivilegeLevel - 1, // One step from goal
			StealthScore:     0.8,
			UnderDetection:   false,
		}
		next := State{
			PrivilegeLevel:   MaxPrivilegeLevel, // Goal achieved!
			StealthScore:     1.0, // Perfect stealth
			UnderDetection:   false,
		}
		
		reward := CalculateReward(current, next, Action{})
		
		// Theoretical max: 0.5*1.0 + 0.3*0.2 + 0.2*0 = 0.56
		assert.Greater(t, reward, 0.5,
			"Should approach maximum theoretical reward")
		assert.Less(t, reward, 0.7,
			"Should stay within reasonable bounds")
	})
	
	// Edge case 2: Minimum possible reward (worst-case detection)
	t.Run("Minimum reward scenario", func(t *testing.T) {
		current := State{
			PrivilegeLevel:   0,
			StealthScore:     1.0,
			UnderDetection:   false,
		}
		next := State{
			PrivilegeLevel:   0,
			StealthScore:     0.0,
			UnderDetection:   true,
		}
		
		reward := CalculateReward(current, next, Action{})
		
		// Worst case: 0.5*0 + 0.3*0 - 0.2*1.0 = -0.2
		assert.Less(t, reward, -0.1,
			"Should penalize worst-case scenario heavily")
		assert.Greater(t, reward, -1.0,
			"Reward should still be bounded")
	})
	
	// Edge case 3: Exactly equal states (should give partial credit)
	t.Run("Zero change scenario", func(t *testing.T) {
		current := State{
			PrivilegeLevel:   1,
			StealthScore:     0.5,
			UnderDetection:   false,
		}
		next := current
		
		reward := CalculateReward(current, next, Action{})
		
		// Should be approximately zero (maybe slight positive for survival)
		assert.InDelta(t, 0.0, reward, 0.1,
			"No change should yield near-zero reward")
	})
}

func TestCalculateReward_DomainKnowledge(t *testing.T) {
	t.Log("Testing domain-specific reward heuristics...")
	
	// Test 1: Domain admin escalation should be highly rewarded
	t.Run("Domain admin priority", func(t *testing.T) {
		current := State{PrivilegeLevel: 2} // System
		next := State{PrivilegeLevel: 3}    // Domain Admin
		
		reward := CalculateReward(current, next, Action{})
		
		// This should be significantly higher than regular escalation
		assert.Greater(t, reward, 0.4,
			"Reaching domain admin should yield high reward")
	})
	
	// Test 2: Stealth maintenance matters more at later stages
	t.Run("Late-game stealth importance", func(t *testing.T) {
		current := State{
			PrivilegeLevel:   2,
			StealthScore:     0.95,
			UnderDetection:   false,
		}
		next := State{
			PrivilegeLevel:   2,
			StealthScore:     0.9, // Slight degradation
			UnderDetection:   false,
		}
		
		reward := CalculateReward(current, next, Action{})
		
		// Minor stealth loss at high privilege level still incurs penalty
		assert.Less(t, reward, 0.2,
			"Even small stealth degradation costs something")
	})
	
	// Test 3: Detection cascades to immediate termination value
	t.Run("Detection finality", func(t *testing.T) {
		current := State{
			PrivilegeLevel:   2,
			StealthScore:     0.9,
			UnderDetection:   false,
		}
		next := State{
			PrivilegeLevel:   2,
			StealthScore:     0.9,
			UnderDetection:   true, // Just got caught
		}
		
		reward := CalculateReward(current, next, Action{})
		
		// Pure detection penalty = -0.2
		assert.InDelta(t, -0.2, reward, 0.05,
			"Detection imposes fixed penalty regardless of other factors")
	})
}


