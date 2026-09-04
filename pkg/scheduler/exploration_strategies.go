package scheduler

import (
	"math"
	"math/rand"
)

// ============================================================================
// ADAPTIVE EXPLORATION STRATEGIES FOR RL SCHEDULER
// Fixes Defect #3: Static epsilon-greedy without confidence-based adaptation
// ============================================================================

// ExplorationConfig configures exploration behavior
type ExplorationConfig struct {
	EpsilonStart    float64 // Initial exploration rate (default: 1.0)
	EpsilonEnd      float64 // Final exploitation rate (default: 0.05)
	EpsilonDecay    float64 // Decay rate per step (default: 0.9995)
	UCBAlpha        float64 // UCB confidence bonus factor (default: 0.1)
	MinSamplesPerAction int // Minimum samples before exploiting (default: 10)
}

// DefaultExplorationConfig returns balanced exploration settings
func DefaultExplorationConfig() ExplorationConfig {
	return ExplorationConfig{
		EpsilonStart:          1.0,
		EpsilonEnd:            0.05,
		EpsilonDecay:          0.9995,
		UCBAlpha:              0.1,
		MinSamplesPerAction:   10,
	}
}

// AdaptiveExplorer implements adaptive exploration with epsilon-greedy + UCB fallback
type AdaptiveExplorer struct {
	cfg              ExplorationConfig
	globalStep       int64
	actionCounts     map[int]int64      // (state_hash -> action) -> count
	stateVisitCounts map[string]int64   // state_hash -> total visits
	bestRewardByState map[string]float64 // best reward observed per state
	mu               interface{}        // Mutex placeholder
}

// NewAdaptiveExplorer creates adaptive explorer with config
func NewAdaptiveExplorer(cfg ExplorationConfig) *AdaptiveExplorer {
	return &AdaptiveExplorer{
		cfg:              cfg,
		actionCounts:     make(map[int]int64),
		stateVisitCounts: make(map[string]int64),
		bestRewardByState: make(map[string]float64),
	}
}

// SelectAction implements adaptive epsilon-greedy with UCB fallback
// Returns action index based on:
// 1. Early phase: pure random exploration
// 2. Middle phase: epsilon-greedy with decaying epsilon
// 3. Late phase: UCB-based confidence bonuses when exploration low
func (e *AdaptiveExplorer) SelectAction(stateHash string, qValues []float64, numActions int) int {
	e.globalStep++
	
	// Update visit counts
	e.stateVisitCounts[stateHash]++
	
	// Calculate current epsilon with decay
	currentEpsilon := math.Max(e.cfg.EpsilonEnd,
		e.cfg.EpsilonStart*(e.cfg.EpsilonDecay*math.Pow(float64(e.globalStep), -0.1)))
	
	// Phase 1: Pure exploration for first N steps
	if e.globalStep < int64(1000) {
		return rand.Intn(numActions)
	}
	
	// Phase 2: Epsilon-greedy exploration
	if rand.Float64() < currentEpsilon {
		action := rand.Intn(numActions)
		e.actionCounts[action]++
		return action
	}
	
	// Phase 3: Exploitation with UCB confidence bonuses
	return e.ucbSelectAction(stateHash, qValues, numActions)
}

// ucbSelectAction selects action using Upper Confidence Bound formula
// Q(s,a) + c*sqrt(ln(N(s)) / N(s,a)) where c is exploration constant
func (e *AdaptiveExplorer) ucbSelectAction(stateHash string, qValues []float64, numActions int) int {
	maxVisits := int64(1)
	for _, count := range e.actionCounts {
		if count > maxVisits {
			maxVisits = count
		}
	}
	
	selectedAction := 0
	maxScore := math.Inf(-1)
	
	for action := 0; action < numActions; action++ {
		qValue := qValues[action]
		
		actionCount := e.actionCounts[action]
		if actionCount == 0 {
			// Never tried this action before – always explore unknown actions first
			score := math.Inf(1)
			if score > maxScore {
				maxScore = score
				selectedAction = action
			}
			continue
		}
		
		// UCB formula: Q(s,a) + alpha * sqrt(ln(N(s)) / N(s,a))
		totalVisits := float64(e.stateVisitCounts[stateHash])
		bonus := e.cfg.UCBAlpha * math.Sqrt(math.Log(totalVisits)/float64(actionCount))
		score := qValue + bonus
		
		if score > maxScore {
			maxScore = score
			selectedAction = action
		}
	}
	
	e.actionCounts[selectedAction]++
	return selectedAction
}

// UpdateBestReward tracks best observed reward per state for convergence detection
func (e *AdaptiveExplorer) UpdateBestReward(stateHash string, reward float64) {
	bestReward, ok := e.bestRewardByState[stateHash]
	if !ok || reward > bestReward {
		e.bestRewardByState[stateHash] = reward
	}
}

// CheckConvergence detects if training has plateaued across all states
// Returns true if average reward change < threshold over last N episodes
func (e *AdaptiveExplorer) CheckConvergence(rewards []float64, windowSize int, threshold float64) bool {
	if len(rewards) < windowSize {
		return false
	}
	
	var totalChange float64
	count := 0
	
	for i := len(rewards) - windowSize; i < len(rewards)-1; i++ {
		change := math.Abs(rewards[i+1] - rewards[i])
		totalChange += change
		count++
	}
	
	avgChange := totalChange / float64(count)
	return avgChange < threshold
}
