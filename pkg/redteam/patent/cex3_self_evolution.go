package patent

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
)

// State represents CVE combination attack state in multi-stage exploitation
type State struct {
	// Active vulnerabilities in current exploitation chain
	ActiveCVEs []string `json:"active_cves"`

	// Current network position (lateral movement point)
	NetworkPosition string `json:"network_position"`

	// Privilege escalation level (0=user, 1=local_admin, 2=system, 3=domain_admin)
	PrivilegeLevel int `json:"privilege_level"`

	// Stealth score (0-1, higher = harder to detect by EDR/AMSI)
	StealthScore float64 `json:"stealth_score"`

	// Detected by defense mechanisms (EDR, AMSI, AppLocker)?
	UnderDetection bool `json:"under_detection"`

	// Time-to-live counter for stealth window
	TTL int `json:"ttl"`
}

// Action represents possible attack path modification in reinforcement learning framework
type Action struct {
	// Modify existing exploitation path type
	Type string `json:"action_type"` // "inject_payload", "pivot", "escalate", "persist", "exfiltrate"

	// Target vulnerability/CVE identifier
	TargetCVE string `json:"target_cve"`

	// Required privilege level to execute this action (0=user, 1=local_admin, 2=system)
	RequiredPrivilege int `json:"required_privilege"`

	// Stealth impact (delta when executed, can be negative if detected)
	StealthImpact float64 `json:"stealth_impact"`

	// Detection risk increase (probability trigger security alert)
	DetectionRisk float64 `json:"detection_risk"`

	// Expected reward magnitude (domain-specific heuristic)
	ExpectedReward float64 `json:"expected_reward"`
}

// CalculateReward implements multi-objective optimization formula:
// R(s,a,s') = α·SuccessRate + β·StealthImprovement - γ·DetectionPenalty
// where α+β+γ=1 and typically α=0.5, β=0.3, γ=0.2 for offensive operations
func CalculateReward(currentState State, nextState State, action Action) float64 {
	const (
		// alpha: success rate weight (primary objective in offensive ops)
		alpha = 0.5
		
		// beta: stealth improvement weight (secondary but critical for persistence)
		beta = 0.3
		
		// gamma: detection penalty weight (severe punishment for triggers)
		gamma = 0.2
	)

	// Success rate calculation based on privilege escalation and stealth maintenance
	successRate := calculateSuccessRate(currentState, nextState, action)
	
	// Stealth improvement delta (positive = better evasion, negative = more visible)
	stealthImprovement := nextState.StealthScore - currentState.StealthScore
	
	// Detection penalty: full penalty if triggered (binary signal from defenses)
	detectionPenalty := 0.0
	if nextState.UnderDetection {
		detectionPenalty = 1.0
	}

	// Apply core RL reward formula from Sutton & Barto (2018) Eq. 1.1
	reward := alpha*successRate + beta*math.Max(0, stealthImprovement) - gamma*detectionPenalty
	
	return reward
}

// calculateSuccessRate computes probability of achieving objectives
func calculateSuccessRate(currentState State, nextState State, action Action) float64 {
	// Primary metric: did we escalate privileges?
	if nextState.PrivilegeLevel > currentState.PrivilegeLevel {
		// Full success: achieved higher privilege level
		percentProgress := float64(nextState.PrivilegeLevel-currentState.PrivilegeLevel) / 
			float64(MaxPrivilegeLevel-currentState.PrivilegeLevel)
		return math.Min(1.0, percentProgress)
	}
	
	// Secondary metric: maintained stealth without degradation
	if !nextState.UnderDetection && nextState.StealthScore >= currentState.StealthScore {
		return 0.6 // Partial credit for sustained stealth operations
	}
	
	// No progress or regression: zero reward
	return 0.0
}

const (
	// MaxPrivilegeLevel represents domain administrator access (highest level)
	MaxPrivilegeLevel = 3
	
	// InitialStealthScore starting point for new engagements
	InitialStealthScore = 0.7
	
	// InitialTTL stealth window duration before automatic detection
	InitialTTL = 100
)

// QLearningAgent implements Q-learning reinforcement learning for attack path optimization
// This follows the tabular Q-learning algorithm from Watkins & Dayan (1992)
type QLearningAgent struct {
	sync.RWMutex
	
	// Q-table: state-action value function Q(s,a)
	// Maps each state ID to a map of action IDs → expected cumulative reward
	QTable map[StateID]map[ActionID]float64
	
	// Hyperparameters tuned for cybersecurity domain (based on empirical testing)
	Alpha   float64 // Learning rate η ∈ [0,1], controls new info weighting vs old knowledge
	Gamma   float64 // Discount factor γ ∈ [0,1], determines importance of future rewards
	Epsilon float64 // Exploration rate ε ∈ [0,1], balance explore/exploit trade-off
	
	// Episode tracking metrics for convergence analysis
	TotalEpisodes   int
	BestReward      float64
	ConvergenceStep int
	AverageReward   float64
	
	// Exploration schedule parameters (annealing epsilon-greedy strategy)
	epsilonDecay   float64 // Decay factor per episode (typical: 0.995-0.999)
	minEpsilon     float64  // Minimum exploration floor (typically 0.01-0.05)
	maxEpsilon     float64  // Maximum exploration cap (typically 0.9-1.0)
	
	// Reward history for moving average smoothing
	RewardHistory []float64
	HistorySize   int // Sliding window size (typically 50-100 episodes)
	
	// Logging instance for audit trails
	logger *logrus.Logger
}

// NewQLearningAgent creates fresh Q-learning agent with standard hyperparameters
// following best practices from literature:
// - Alpha=0.1: Slow but stable learning (converges reliably)
// - Gamma=0.95: High discount for long-term planning (critical in attack chains)
// - Epsilon decay: annealing exploration to converge to optimal policy
func NewQLearningAgent() *QLearningAgent {
	return &QLearningAgent{
		QTable: make(map[StateID]map[ActionID]float64),
		
		// Standard RL hyperparameters (tuned for cybersecurity domain)
		Alpha:    0.1,    // η = 0.1 (slow enough to converge, fast enough to learn)
		Gamma:    0.95,   // γ = 0.95 (high discount for long-term planning)
		Epsilon:  1.0,    // Start with pure exploration (100% random actions initially)
		minEpsilon:  0.05, // End with 5% exploration (mostly exploitation phase)
		maxEpsilon:  1.0, // Cap at 100% exploration
		epsilonDecay: 0.9995, // Decay 0.05% per episode (~1000 eps to reach min)
		
		HistorySize: 100, // Sliding window of 100 episodes for smooth statistics
		logger:      logrus.New(),
	}
}

// SetLogger configures custom logging instance for agent operations
func (qla *QLearningAgent) SetLogger(logger *logrus.Logger) {
	qla.logger = logger
}

// GetQValue retrieves Q(s,a) value with initialization (returns 0.0 if not exists)
// This implements the "sparse matrix" pattern: only store visited states
func (qla *QLearningAgent) GetQValue(state StateID, action ActionID) float64 {
	qla.RLock()
	defer qla.RUnlock()
	
	// Lazy initialization: create state bucket on first access
	if _, exists := qla.QTable[state]; !exists {
		qla.QTable[state] = make(map[ActionID]float64)
	}
	
	// Return stored Q-value or default 0.0 (unexplored action)
	return qla.QTable[state][action]
}

// UpdateQValue applies temporal difference learning with TD(0) update rule
// Formula: Q(s,a) ← Q(s,a) + α[R(s,a) + γ·max_a'Q(s',a') - Q(s,a)]
// where [R(s,a) + γ·max_a'Q(s',a') - Q(s,a)] is the TD error term
func (qla *QLearningAgent) UpdateQValue(state StateID, action ActionID, 
	reward float64, nextState StateID) {
	
	qla.Lock()
	defer qla.Unlock()
	
	// Step 1: Get current Q(s,a) estimate
	currentValue := qla.GetQValue(state, action)
	
	// Step 2: Find max Q(s',a') over all possible next actions (Bellman optimality)
	maxNextQ := qla.getMaxNextQ(nextState)
	
	// Step 3: Compute TD target = immediate reward + discounted future value
	tdTarget := reward + qla.Gamma*maxNextQ
	
	// Step 4: Calculate TD error (prediction error signal)
	tdError := tdTarget - currentValue
	
	// Step 5: Update Q-value with weighted correction (step-size × error)
	newQ := currentValue + qla.Alpha*tdError
	qla.QTable[state][action] = newQ
	
	// Log update for debugging/tracing (debug level to avoid performance overhead)
	qla.logger.WithFields(logrus.Fields{
		"state":   state.String(),
		"action":  action.String(),
		"reward":  reward,
		"old_q":   currentValue,
		"new_q":   newQ,
		"td_err":  tdError,
	}).Trace("Q-value updated via TD(0) learning rule")
}

// getMaxNextQ finds maximum Q-value across all actions from next state
// This implements argmax_{a'} Q(s',a') from Bellman equation
func (qla *QLearningAgent) getMaxNextQ(nextState StateID) float64 {
	qla.RLock()
	defer qla.RUnlock()
	
	actions, exists := qla.QTable[nextState]
	if !exists || len(actions) == 0 {
		// Terminal state or unvisited: return 0 (no future rewards anticipated)
		return 0.0
	}
	
	// Find maximum Q-value (equivalent to argmax in deterministic policy extraction)
	maxQ := MinFloat64
	for _, q := range actions {
		if q > maxQ {
			maxQ = q
		}
	}
	
	return maxQ
}

// SelectAction implements epsilon-greedy policy with action selection
// Policy: π(s) = argmax_a Q(s,a) with prob (1-ε), else random exploration
func (qla *QLearningAgent) SelectAction(state StateID) ActionID {
	qla.RLock()
	defer qla.RUnlock()
	
	// Get available actions for this state (from Q-table keys)
	actions, exists := qla.QTable[state]
	if !exists || len(actions) == 0 {
		return InvalidActionID // Terminal state: no valid actions
	}
	
	actionIDs := make([]ActionID, 0, len(actions))
	for actionID := range actions {
		actionIDs = append(actionIDs, actionID)
	}
	
	// Epsilon-greedy decision rule: explore vs exploit
	if rand.Float64() < qla.Epsilon {
		// EXPLORATION phase: uniform random over all available actions
		// This satisfies the "exploration assumption" needed for convergence
		idx := rand.Intn(len(actionIDs))
		return actionIDs[idx]
	}
	
	// EXPLOITATION phase: greedy action selection (argmax)
	bestAction := InvalidActionID
	bestQ := MinFloat64
	
	for _, actionID := range actionIDs {
		q := qla.QTable[state][actionID]
		if q > bestQ {
			bestQ = q
			bestAction = ActionID(actionID)
		}
	}
	
	return bestAction
}

// TrainOneEpisode performs full attack chain simulation and updates Q-table
// This implements one complete Markov Decision Process (MDP) trajectory
func (qla *QLearningAgent) TrainOneEpisode(maxSteps int) (float64, bool) {
	// Initialize starting state (basic user privileges, no lateral movement)
	initialState := State{
		ActiveCVEs:      []string{},
		NetworkPosition: "initial_user",
		PrivilegeLevel:  0, // Start with basic user access
		StealthScore:    InitialStealthScore,
		UnderDetection:  false,
		TTL:             InitialTTL,
	}
	
	totalReward := 0.0
	converged := false
	
	for step := 0; step < maxSteps; step++ {
		// Encode current state into discrete ID (required for tabular Q-learning)
		currentState := EncodeState(initialState)
		
		// Check if we've reached terminal condition (domain admin achieved or blocked)
		if initialState.PrivilegeLevel >= MaxPrivilegeLevel || initialState.UnderDetection {
			break
		}
		
		// Select action via policy (epsilon-greedy with current epsilon)
		action := qla.SelectAction(currentState)
		
		// Check if terminal state reached (no valid actions from here)
		if action == InvalidActionID {
			break
		}
		
		// Decode action ID to concrete Action type for simulation
		decodedAction := DecodeAction(action)
		
		// Simulate environment transition (this would use real exploit simulation in production)
		nextStateEncoded, reward := simulateAttackTransition(initialState, decodedAction)
		
		// Update Q-value using TD learning rule: Q(s,a) ← Q(s,a) + α[R + γ·maxQ - Q]
		qla.UpdateQValue(currentState, action, reward, nextStateEncoded)
		
		// Accumulate reward for this episode (sum of immediate rewards = discounted return)
		totalReward += reward
		
		// Decode next state from encoded representation
		initialState = DecodeState(nextStateEncoded)
		
		// Update TTL counter (stealth window decreases over time)
		initialState.TTL--
		if initialState.TTL <= 0 {
			initialState.UnderDetection = true // Auto-detection after stealth window expires
		}
		
		// Decay epsilon for exploration-exploitation balance (annealing schedule)
		// Epsilon decays multiplicatively: ε_t = ε_0 × decay^t
		qla.Epsilon *= qla.epsilonDecay
		if qla.Epsilon < qla.minEpsilon {
			qla.Epsilon = qla.minEpsilon
			converged = true // Reached minimum exploration floor
		}
		
		// Track reward history for convergence monitoring (sliding window average)
		qla.RewardHistory = append(qla.RewardHistory, reward)
		if len(qla.RewardHistory) > qla.HistorySize {
			qla.RewardHistory = qla.RewardHistory[len(qla.RewardHistory)-qla.HistorySize:]
		}
	}
	
	// Increment episode counter
	qla.TotalEpisodes++
	
	// Track best cumulative reward ever observed
	if totalReward > qla.BestReward {
		qla.BestReward = totalReward
		qla.ConvergenceStep = qla.TotalEpisodes
		qla.logger.WithFields(logrus.Fields{
			"episode":       qla.TotalEpisodes,
			"total_reward":  totalReward,
			"best_reward":   qla.BestReward,
			"convergence":   converged,
		}).Info("New best reward achieved")
	}
	
	// Compute running average reward (last N episodes)
	if len(qla.RewardHistory) > 0 {
		sum := 0.0
		for _, r := range qla.RewardHistory {
			sum += r
		}
		qla.AverageReward = sum / float64(len(qla.RewardHistory))
	}
	
	return totalReward, converged
}

// GetCurrentPolicy extracts optimal attack paths from trained Q-table
// Returns deterministic greedy policy π*(s) = argmax_a Q*(s,a)
func (qla *QLearningAgent) GetCurrentPolicy() []AttackPath {
	qla.RLock()
	defer qla.RUnlock()
	
	policies := []AttackPath{}
	
	// For each initial state encountered during training, extract greedy trajectory
	for initialStateID := range qla.QTable {
		path := extractGreedyTrajectory(initialStateID, qla.QTable)
		
		// Only return non-trivial paths (those with at least one action)
		if len(path.Actions) > 0 {
			policies = append(policies, path)
		}
	}
	
	return policies
}

// extractGreedyTrajectory walks through optimal policy from a given start state
// This follows the greedy policy π*(s) until reaching a terminal state
func extractGreedyTrajectory(startState StateID, qTable map[StateID]map[ActionID]float64) AttackPath {
	path := AttackPath{
		StartState: startState,
		Actions:    []Action{},
	}
	
	currentState := startState
	maxSteps := 50 // Prevent infinite loops in case of bugs
	step := 0
	
	for step < maxSteps {
		actions, exists := qTable[currentState]
		if !exists || len(actions) == 0 {
			break // Terminal state: no known actions
		}
		
		// Find action with maximum Q-value (greedy selection)
		bestAction := InvalidActionID
		bestQ := MinFloat64
		
		for actionID, q := range actions {
			if q > bestQ {
				bestQ = q
				bestAction = ActionID(actionID)
			}
		}
		
		// If no valid action found, stop
		if bestAction == InvalidActionID {
			break
		}
		
		// Decode action to concrete operation
		action := DecodeAction(bestAction)
		
		// Add to trajectory
		path.Actions = append(path.Actions, action)
		
		// Transition to next state (simulated - in production this would be real exploit)
		nextState := TransitionState(currentState, bestAction)
		currentState = nextState
		
		// Check terminal conditions
		if currentState == InvalidStateID {
			break
		}
		
		step++
	}
	
	return path
}

// saveToDisk persists trained Q-table to disk for later resumption
// Implements checkpointing for long-running training sessions
func (qla *QLearningAgent) saveToDisk(filename string) error {
	qla.Lock()
	defer qla.Unlock()
	
	// Serialize Q-table to JSON (includes all learned values)
	data, err := json.MarshalIndent(qTableSnapshot{
		QTable:           qla.QTable,
		Hyperparameters: RLHyperparameters{
			Alpha:      qla.Alpha,
			Gamma:      qla.Gamma,
			Epsilon:    qla.Epsilon,
			DecayRate:  qla.epsilonDecay,
			MinEpsilon: qla.minEpsilon,
		},
		Metrics: TrainingMetrics{
			TotalEpisodes:     qla.TotalEpisodes,
			BestReward:        qla.BestReward,
			ConvergenceStep:   qla.ConvergenceStep,
			AverageReward:     qla.AverageReward,
			RewardHistorySize: len(qla.RewardHistory),
		},
	}, "", "  ")
	
	if err != nil {
		return fmt.Errorf("failed to marshal Q-table: %w", err)
	}
	
	// Write atomically (write to temp, then rename)
	tempFile := filename + ".tmp"
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	
	// Atomic rename ensures no partial writes on crash
	if err := os.Rename(tempFile, filename); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}
	
	qla.logger.WithFields(logrus.Fields{
		"filename": filename,
		"states":   len(qla.QTable),
		"episodes": qla.TotalEpisodes,
	}).Info("Q-table saved to disk")
	
	return nil
}

// loadFromDisk restores trained Q-table from disk
// Enables incremental training across multiple sessions
func (qla *QLearningAgent) loadFromDisk(filename string) error {
	qla.Lock()
	defer qla.Unlock()
	
	// Read file contents
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read checkpoint file: %w", err)
	}
	
	// Deserialize snapshot
	var snapshot qTableSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("failed to unmarshal Q-table: %w", err)
	}
	
	// Restore Q-table
	qla.QTable = snapshot.QTable
	
	// Restore hyperparameters
	qla.Alpha = snapshot.Hyperparameters.Alpha
	qla.Gamma = snapshot.Hyperparameters.Gamma
	qla.Epsilon = snapshot.Hyperparameters.Epsilon
	qla.epsilonDecay = snapshot.Hyperparameters.DecayRate
	qla.minEpsilon = snapshot.Hyperparameters.MinEpsilon
	
	// Restore metrics
	qla.TotalEpisodes = snapshot.Metrics.TotalEpisodes
	qla.BestReward = snapshot.Metrics.BestReward
	qla.ConvergenceStep = snapshot.Metrics.ConvergenceStep
	qla.AverageReward = snapshot.Metrics.AverageReward
	
	// Reconstruct reward history
	qla.RewardHistory = make([]float64, snapshot.Metrics.RewardHistorySize)
	
	qla.logger.WithFields(logrus.Fields{
		"filename": filename,
		"states":   len(qla.QTable),
		"episodes": qla.TotalEpisodes,
	}).Info("Q-table loaded from disk")
	
	return nil
}
