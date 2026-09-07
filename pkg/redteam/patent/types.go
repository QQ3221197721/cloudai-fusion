// Package patent - Supporting data structures for Self-Evolving Attack Graph Engine
package patent

// AttackPath represents learned optimal trajectory from initial state to goal
type AttackPath struct {
	StartState StateID `json:"start_state"`
	EndState   StateID `json:"end_state,omitempty"`
	Actions    []Action       `json:"actions"`
	Reward     float64        `json:"total_reward"`
}

// qTableSnapshot serializable checkpoint of entire Q-table
type qTableSnapshot struct {
	QTable map[StateID]map[ActionID]float64 `json:"q_table"`
	
	// Hyperparameters at time of snapshot
	Hyperparameters RLHyperparameters `json:"hyperparameters"`
	
	// Training metrics at snapshot point
	Metrics TrainingMetrics `json:"training_metrics"`
}

// RLHyperparameters captures reinforcement learning configuration
type RLHyperparameters struct {
	Alpha      float64 `json:"alpha"`      // Learning rate η ∈ [0,1]
	Gamma      float64 `json:"gamma"`      // Discount factor γ ∈ [0,1]
	Epsilon    float64 `json:"epsilon"`    // Exploration rate ε ∈ [0,1]
	DecayRate  float64 `json:"decay_rate"` // Epsilon decay per episode
	MinEpsilon float64 `json:"min_epsilon"` // Minimum exploration floor
}

// TrainingMetrics tracks progress during training
type TrainingMetrics struct {
	TotalEpisodes     int       `json:"total_episodes"`
	BestReward        float64   `json:"best_reward"`
	ConvergenceStep   int       `json:"convergence_step"`
	AverageReward     float64   `json:"average_reward"`
	RewardHistorySize int       `json:"reward_history_size"`
}

// Note: FederatedUpdate is defined in poisoning_detector.go to avoid conflicts
