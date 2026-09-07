package attack_graph

import (
	"encoding/gob"
	"fmt"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Q-Table Implementation - Stateâ†’Action Value Storage
// ============================================================================
// This component stores the learned Q-values for all state-action pairs.
// Uses string hashing of states for efficient O(1) lookup.
// Thread-safe implementation with RWMutex for concurrent access.
//
// Memory Efficiency: Uses sparse storage - only stores visited states
// Convergence Guarantee: Per Sutton&Barto Theorem 6.1, under standard RL assumptions
//                       (bounded rewards, exploring starts), Q-learning converges to optimal policy

type QTable struct {
	logger *logrus.Logger

	// Q-values: state_hash â†?action â†?value
	values map[string]map[Action]float64

	// Visit counts: state_hash â†?count
	visits map[string]int

	// Epsilon-greedy exploration parameters
	epsilon float64

	// Thread safety
	mu sync.RWMutex
}

// NewQTable creates a new Q-table instance
func NewQTable() *QTable {
	return &QTable{
		logger: logrus.WithField("component", "q_table"),
		values: make(map[string]map[Action]float64),
		visits: make(map[string]int),
		epsilon: 0.1,
	}
}

// Get returns the Q-value for a state-action pair
func (qt *QTable) Get(state State, action Action) float64 {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	stateHash := state.Hash()
	if _, exists := qt.values[stateHash]; !exists {
		return 0.0 // Initialize new states with zero Q-values
	}

	return qt.values[stateHash][action]
}

// Set updates the Q-value for a state-action pair
func (qt *QTable) Set(state State, action Action, value float64) {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	stateHash := state.Hash()

	// Initialize action map for new states
	if qt.values[stateHash] == nil {
		qt.values[stateHash] = make(map[Action]float64)
	}

	qt.values[stateHash][action] = value

	// Increment visit count
	qt.visits[stateHash]++
}

// GetAllActions returns all possible actions
func (qt *QTable) GetAllActions() []Action {
	return []Action{ActionAddEdge, ActionRemoveEdge, ActionReRoute, ActionSkipNode, ActionParallelize, ActionChain}
}

// GetBestAction finds the action with maximum Q-value for a state
func (qt *QTable) GetBestAction(state State) (Action, float64) {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	stateHash := state.Hash()
	if _, exists := qt.values[stateHash]; !exists {
		return ActionNone, 0.0 // Unexplored state
	}

	var bestAction Action
	var bestValue float64 = -1e9 // Negative infinity

	for action, value := range qt.values[stateHash] {
		if value > bestValue {
			bestValue = value
			bestAction = action
		}
	}

	return bestAction, bestValue
}

// DecayEpsilon reduces exploration rate over time
func (qt *QTable) DecayEpsilon(decayRate float64, minEpsilon float64) {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	qt.epsilon *= decayRate
	if qt.epsilon < minEpsilon {
		qt.epsilon = minEpsilon
	}
}

// GetVisitCount returns how many times a state has been visited
func (qt *QTable) GetVisitCount(state State) int {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	return qt.visits[state.Hash()]
}

// SaveToFile persists the Q-table to disk in binary format
func (qt *QTable) SaveToFile(filepath string) error {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)

	if err := encoder.Encode(qt.values); err != nil {
		return fmt.Errorf("failed to encode Q-table: %w", err)
	}

	if err := encoder.Encode(qt.visits); err != nil {
		return fmt.Errorf("failed to encode visit counts: %w", err)
	}

	if err := encoder.Encode(qt.epsilon); err != nil {
		return fmt.Errorf("failed to encode epsilon: %w", err)
	}

	return nil
}

// LoadFromFile restores a previously saved Q-table from disk
func (qt *QTable) LoadFromFile(filepath string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)

	if err := decoder.Decode(&qt.values); err != nil {
		return fmt.Errorf("failed to decode values: %w", err)
	}

	if err := decoder.Decode(&qt.visits); err != nil {
		return fmt.Errorf("failed to decode visits: %w", err)
	}

	if err := decoder.Decode(&qt.epsilon); err != nil {
		return fmt.Errorf("failed to decode epsilon: %w", err)
	}

	return nil
}

// Clear resets all Q-values to zero
func (qt *QTable) Clear() {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	qt.values = make(map[string]map[Action]float64)
	qt.visits = make(map[string]int)
}

// Stats returns summary statistics about the Q-table
func (qt *QTable) Stats() map[string]interface{} {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	totalStates := len(qt.values)
	totalActions := 0
	for _, actions := range qt.values {
		totalActions += len(actions)
	}

	return map[string]interface{}{
		"total_states":     totalStates,
		"total_actions":    totalActions,
		"avg_actions_per_state": float64(totalActions) / float64(totalStates),
		"epsilon":          qt.epsilon,
	}
}
