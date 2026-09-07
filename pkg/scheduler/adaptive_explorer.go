package scheduler

import (
	"math"
	"math/rand"
)

// ============================================================================
// ADAPTIVE EXPLORATION STRATEGY FOR DQN RL SCHEDULER
// Fixes Defect #3: Inadequate exploration without decay or UCB confidence bonus
// ============================================================================

// ExplorationConfig configures adaptive exploration parameters
type ExplorationConfig struct {
	EpsilonStart float64 // Initial exploration rate (default 1.0 = fully exploratory)
	EpsilonEnd   float64 // Final exploration rate (default 0.05 = mostly exploitative)
	EpsilonDecay float64 // Decay factor per episode (default 0.9995)
	UCBAlpha     float64 // UCB confidence coefficient (default 0.1, sqrt(2)*conf_level)
	GreedyWeight float64 // Weight on exploitation in hybrid strategy (default 0.7)
}

// DefaultExplorationConfig returns balanced adaptive exploration settings
func DefaultExplorationConfig() ExplorationConfig {
	return ExplorationConfig{
		EpsilonStart: 1.0,    // Start with full exploration
		EpsilonEnd:   0.05,   // End with 5% exploration (mostly greedy)
		EpsilonDecay: 0.9995, // Decay to reach epsilonEnd in ~1400 episodes
		UCBAlpha:     0.1,    // Moderate confidence bonus for rare states
		GreedyWeight: 0.7,    // 70% greedy, 30% exploration blend
	}
}

// AdaptiveExplorer combines ε-greedy exploration with UCB confidence bonuses
type AdaptiveExplorer struct {
	config           ExplorationConfig
	currentEpsilon   float64
	totalSteps       int64
	actionHistory    map[string]map[int]int // stateHash -> action -> visit count
	maxStateHashLen  int                    // Track most visited states
	topStates        []string               // List of top N most visited state hashes
}

// NewAdaptiveExplorer creates an adaptive explorer with given config
func NewAdaptiveExplorer(cfg ExplorationConfig) *AdaptiveExplorer {
	return &AdaptiveExplorer{
		config:        cfg,
		currentEpsilon: cfg.EpsilonStart,
		actionHistory: make(map[string]map[int]int),
	}
}

// SelectAction implements hybrid ε-greedy + UCB exploration strategy
// For each state-action pair:
//   1. With probability ε: random exploration (ε-greedy)
//   2. With probability (1-ε): select action maximizing Q(s,a) + UCB_bonus
//      where UCB_bonus = α*sqrt(2*ln(N_t)/N_t(a))鼓励探索罕见状态
func (ae *AdaptiveExplorer) SelectAction(stateHash string, qValues []float64, visitCounts []int) int {
	ae.totalSteps++
	
	// Initialize visit counts if needed
	if len(visitCounts) == 0 {
		visitCounts = make([]int, len(qValues))
	}
	
	// Ensure map entry exists
	if ae.actionHistory[stateHash] == nil {
		ae.actionHistory[stateHash] = make(map[int]int)
		for i := range qValues {
			ae.actionHistory[stateHash][i] = 0
		}
	}
	
	// Update global step tracking
	stateVisitCount := ae.getStateVisitCount(stateHash)
	ae.currentEpsilon = ae.computeEpsilonDecay()
	
	// Strategy 1: Pure ε-greedy random exploration
	if rand.Float64() < ae.currentEpsilon {
		action := rand.Intn(len(qValues))
		ae.incrementVisitCount(stateHash, action)
		return action
	}
	
	// Strategy 2: Hybrid greedy + UCB for exploitation
	bestAction := ae.selectMaxUCBAction(stateHash, qValues, visitCounts)
	ae.incrementVisitCount(stateHash, bestAction)
	
	return bestAction
}

// computeEpsilonDecay applies exponential decay to exploration rate
// Formula: ε_t = ε_end + (ε_start - ε_end) * γ^t
// Where γ = ε_decay per episode
func (ae *AdaptiveExplorer) computeEpsilonDecay() float64 {
	if ae.totalSteps == 0 {
		return ae.config.EpsilonStart
	}
	
	// Exponential decay formula
	decayFactor := math.Pow(ae.config.EpsilonDecay, float64(ae.totalSteps))
	newEpsilon := ae.config.EpsilonEnd + (ae.config.EpsilonStart-ae.config.EpsilonEnd)*decayFactor
	
	return math.Max(ae.config.EpsilonEnd, math.Min(ae.config.EpsilonStart, newEpsilon))
}

// getStateVisitCount returns total visits to a state across all actions
func (ae *AdaptiveExplorer) getStateVisitCount(stateHash string) int {
	counts := ae.actionHistory[stateHash]
	total := 0
	for _, c := range counts {
		total += c
	}
	return total
}

// incrementVisitCount increments visit counter for a specific state-action pair
func (ae *AdaptiveExplorer) incrementVisitCount(stateHash string, action int) {
	if ae.actionHistory[stateHash] == nil {
		ae.actionHistory[stateHash] = make(map[int]int)
	}
	ae.actionHistory[stateHash][action]++
}

// selectMaxUCBAction selects action using Upper Confidence Bound heuristic
// UCB1 formula: action = argmax[Q(s,a) + α*sqrt(2*ln(N_t)/N_t(a))]
// where N_t = total visits to state s, N_t(a) = visits to action a
func (ae *AdaptiveExplorer) selectMaxUCBAction(stateHash string, qValues []float64, visitCounts []int) int {
	totalVisits := ae.getStateVisitCount(stateHash)
	if totalVisits == 0 {
		// No experience yet, select uniformly at random
		return rand.Intn(len(qValues))
	}
	
	lnTotalVisits := math.Log(float64(totalVisits))
	
	// Compute UCB scores for all actions
	bestScore := math.Inf(-1)
	bestAction := 0
	
	// Ensure visitCounts has right size
	if len(visitCounts) != len(qValues) {
		visitCounts = make([]int, len(qValues))
	}
	
	for action := range qValues {
		qVal := qValues[action]
		actionVisits := ae.actionHistory[stateHash][action]
		
		// UCB bonus: encourages exploring under-visited actions
		var ucbBonus float64
		if actionVisits > 0 {
			ucbBonus = ae.config.UCBAlpha * math.Sqrt(2.0*lnTotalVisits/float64(actionVisits))
		} else {
			// Infinite bonus for never-visited actions (ensure exploration)
			ucbBonus = math.Inf(1)
		}
		
		score := qVal + ucbBonus
		
		if score > bestScore {
			bestScore = score
			bestAction = action
		}
	}
	
	return bestAction
}

// GetExplorationMetrics returns current exploration statistics for monitoring
func (ae *AdaptiveExplorer) GetExplorationMetrics() map[string]any {
	return map[string]any{
		"total_steps":    ae.totalSteps,
		"current_epsilon": ae.currentEpsilon,
		"unique_states":  len(ae.actionHistory),
		"epsilon_decay_rate": ae.config.EpsilonDecay,
	}
}

// Reset resets explorer state for new training run
func (ae *AdaptiveExplorer) Reset() {
	ae.currentEpsilon = ae.config.EpsilonStart
	ae.totalSteps = 0
	ae.actionHistory = make(map[string]map[int]int)
}

// Simulated Adaptive Explorer for unit testing (no randomness)
type SimulatedExplorer struct {
	greedyStrategy bool // When true, always select max Q-value (pure exploitation)
}

// NewSimulatedExplorer creates explorer for deterministic simulation mode
func NewSimulatedExplorer(greedyStrategy bool) *SimulatedExplorer {
	return &SimulatedExplorer{greedyStrategy: greedyStrategy}
}

// SelectAction in simulation mode either picks max-Q (greedy) or uniform random
func (se *SimulatedExplorer) SelectAction(stateHash string, qValues []float64, visitCounts []int) int {
	if se.greedyStrategy && len(qValues) > 0 {
		// Always pick best known action (no exploration noise)
		bestIdx := 0
		bestVal := qValues[0]
		for i, v := range qValues[1:] {
			if v > bestVal {
				bestVal = v
				bestIdx = i + 1
			}
		}
		return bestIdx
	}
	
	// Random fallback for simulations that do explore
	return rand.Intn(len(qValues))
}
