// Package scheduler - rl_optimizer.go implements reinforcement learning-based
// scheduling optimization using a tabular Q-learning approach.
// The RL agent learns optimal node selection strategies from scheduling outcomes
// (GPU utilization, job completion time, cost) to improve scheduling decisions over time.
package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Q-Learning Scheduler Optimizer
// ============================================================================

// RLOptimizerConfig holds RL optimizer configuration
type RLOptimizerConfig struct {
	LearningRate    float64 // alpha: how fast Q-values update (0.01-0.5)
	DiscountFactor  float64 // gamma: importance of future rewards (0.8-0.99)
	ExplorationRate float64 // epsilon: probability of random action (0.05-0.3)
	ExplorationDecay float64 // epsilon decay per episode (0.995-0.9999)
	MinExploration  float64 // minimum epsilon floor (0.01-0.05)
}

// RLOptimizer implements Q-learning for scheduling optimization
type RLOptimizer struct {
	config    RLOptimizerConfig
	qTable    map[string]map[string]float64 // state → action → Q-value
	episodes  int                            // total training episodes
	rewards   []float64                      // reward history (sliding window)
	avgReward float64                        // running average reward
	logger    *logrus.Logger
	mu        sync.RWMutex
}

// SchedulingState represents the discrete state observed by the RL agent
type SchedulingState struct {
	WorkloadType     string  // training, inference, fine-tuning
	GPUCountBucket   string  // "1", "2-4", "5-8", "8+"
	PriorityBucket   string  // "low", "medium", "high", "critical"
	ClusterLoadLevel string  // "low" (<30%), "medium" (30-70%), "high" (>70%)
	TimeOfDay        string  // "off-peak", "standard", "peak"
}

// SchedulingAction represents a discrete action the RL agent can take
type SchedulingAction struct {
	NodePreference   string // "cheapest", "fastest", "balanced", "topology-optimal"
	GPUShareMode     string // "exclusive", "mps", "mig"
	PreemptionChoice string // "allow", "avoid"
}

// SchedulingReward captures the outcome of a scheduling decision
type SchedulingReward struct {
	GPUUtilization    float64 // 0-100: higher is better
	JobCompletionTime float64 // seconds: lower is better
	CostEfficiency    float64 // 0-1: cost / performance ratio
	QueueWaitTime     float64 // seconds: lower is better
	Preemptions       int     // number of preemptions caused: lower is better
}

// NewRLOptimizer creates a new Q-learning scheduling optimizer
func NewRLOptimizer(cfg RLOptimizerConfig) *RLOptimizer {
	if cfg.LearningRate == 0 {
		cfg.LearningRate = 0.1
	}
	if cfg.DiscountFactor == 0 {
		cfg.DiscountFactor = 0.95
	}
	if cfg.ExplorationRate == 0 {
		cfg.ExplorationRate = 0.2
	}
	if cfg.ExplorationDecay == 0 {
		cfg.ExplorationDecay = 0.999
	}
	if cfg.MinExploration == 0 {
		cfg.MinExploration = 0.02
	}

	return &RLOptimizer{
		config:  cfg,
		qTable:  make(map[string]map[string]float64),
		rewards: make([]float64, 0, 1000),
		logger:  logrus.StandardLogger(),
	}
}

// ============================================================================
// State / Action Encoding
// ============================================================================

// EncodeState converts workload properties to a discrete RL state
func EncodeState(workloadType string, gpuCount int, priority int, clusterUtilization float64) SchedulingState {
	state := SchedulingState{
		WorkloadType: workloadType,
	}

	// Discretize GPU count
	switch {
	case gpuCount <= 1:
		state.GPUCountBucket = "1"
	case gpuCount <= 4:
		state.GPUCountBucket = "2-4"
	case gpuCount <= 8:
		state.GPUCountBucket = "5-8"
	default:
		state.GPUCountBucket = "8+"
	}

	// Discretize priority
	switch {
	case priority >= 9:
		state.PriorityBucket = "critical"
	case priority >= 6:
		state.PriorityBucket = "high"
	case priority >= 3:
		state.PriorityBucket = "medium"
	default:
		state.PriorityBucket = "low"
	}

	// Discretize cluster load
	switch {
	case clusterUtilization > 70:
		state.ClusterLoadLevel = "high"
	case clusterUtilization > 30:
		state.ClusterLoadLevel = "medium"
	default:
		state.ClusterLoadLevel = "low"
	}

	// Time of day (simplified)
	hour := time.Now().Hour()
	switch {
	case hour >= 22 || hour < 6:
		state.TimeOfDay = "off-peak"
	case hour >= 9 && hour < 18:
		state.TimeOfDay = "peak"
	default:
		state.TimeOfDay = "standard"
	}

	return state
}

func (s SchedulingState) Key() string {
	return fmt.Sprintf("%s|%s|%s|%s|%s", s.WorkloadType, s.GPUCountBucket, s.PriorityBucket, s.ClusterLoadLevel, s.TimeOfDay)
}

// AllActions returns all possible scheduling actions
func AllActions() []SchedulingAction {
	preferences := []string{"cheapest", "fastest", "balanced", "topology-optimal"}
	shareModes := []string{"exclusive", "mps", "mig"}
	preemptChoices := []string{"allow", "avoid"}

	var actions []SchedulingAction
	for _, p := range preferences {
		for _, s := range shareModes {
			for _, pr := range preemptChoices {
				actions = append(actions, SchedulingAction{
					NodePreference:   p,
					GPUShareMode:     s,
					PreemptionChoice: pr,
				})
			}
		}
	}
	return actions
}

func (a SchedulingAction) Key() string {
	return fmt.Sprintf("%s|%s|%s", a.NodePreference, a.GPUShareMode, a.PreemptionChoice)
}

// ============================================================================
// Q-Learning Core
// ============================================================================

// SelectAction chooses the best action for a state using epsilon-greedy policy
func (rl *RLOptimizer) SelectAction(state SchedulingState) SchedulingAction {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	allActs := AllActions()

	// Epsilon-greedy exploration
	if rand.Float64() < rl.config.ExplorationRate {
		return allActs[rand.Intn(len(allActs))]
	}

	// Exploitation: choose action with highest Q-value
	stateKey := state.Key()
	qValues, exists := rl.qTable[stateKey]
	if !exists {
		// No experience for this state → random
		return allActs[rand.Intn(len(allActs))]
	}

	bestAction := allActs[0]
	bestQ := math.Inf(-1)
	for _, act := range allActs {
		q, ok := qValues[act.Key()]
		if ok && q > bestQ {
			bestQ = q
			bestAction = act
		}
	}

	return bestAction
}

// UpdateQValue updates the Q-table based on observed reward (Q-learning update rule)
// Q(s,a) ← Q(s,a) + α * [r + γ * max_a'(Q(s',a')) - Q(s,a)]
func (rl *RLOptimizer) UpdateQValue(state SchedulingState, action SchedulingAction, reward float64, nextState SchedulingState) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	stateKey := state.Key()
	actionKey := action.Key()
	nextStateKey := nextState.Key()

	// Initialize state entry if needed
	if _, ok := rl.qTable[stateKey]; !ok {
		rl.qTable[stateKey] = make(map[string]float64)
	}

	// Current Q-value
	currentQ := rl.qTable[stateKey][actionKey]

	// Max Q-value for next state
	maxNextQ := 0.0
	if nextQValues, ok := rl.qTable[nextStateKey]; ok {
		for _, q := range nextQValues {
			if q > maxNextQ {
				maxNextQ = q
			}
		}
	}

	// Q-learning update
	newQ := currentQ + rl.config.LearningRate*(reward+rl.config.DiscountFactor*maxNextQ-currentQ)
	rl.qTable[stateKey][actionKey] = newQ

	// Update statistics
	rl.episodes++
	rl.rewards = append(rl.rewards, reward)
	if len(rl.rewards) > 1000 {
		rl.rewards = rl.rewards[len(rl.rewards)-1000:]
	}

	// Calculate running average
	sum := 0.0
	for _, r := range rl.rewards {
		sum += r
	}
	rl.avgReward = sum / float64(len(rl.rewards))

	// Decay exploration rate
	rl.config.ExplorationRate *= rl.config.ExplorationDecay
	if rl.config.ExplorationRate < rl.config.MinExploration {
		rl.config.ExplorationRate = rl.config.MinExploration
	}
}

// CalculateReward computes a scalar reward from scheduling outcome
func CalculateReward(outcome SchedulingReward) float64 {
	// Normalized reward components (each in 0-1 range, higher = better)

	// GPU utilization: target is ~80%. Too low or too high is penalized
	utilReward := 1.0 - math.Abs(outcome.GPUUtilization-80.0)/80.0
	if utilReward < 0 {
		utilReward = 0
	}

	// Job completion: faster is better (normalize against 1 hour baseline)
	completionReward := 1.0 - math.Min(outcome.JobCompletionTime/3600.0, 1.0)

	// Cost: better cost efficiency = higher reward
	costReward := outcome.CostEfficiency

	// Queue wait: shorter wait = higher reward (normalize against 5 min baseline)
	waitReward := 1.0 - math.Min(outcome.QueueWaitTime/300.0, 1.0)

	// Preemption penalty
	preemptPenalty := math.Min(float64(outcome.Preemptions)*0.2, 0.5)

	// Weighted composite reward
	reward := (utilReward*0.35 + completionReward*0.25 + costReward*0.2 + waitReward*0.15) - preemptPenalty

	// Scale to [-1, 1]
	return math.Max(-1.0, math.Min(1.0, reward*2-1))
}

// ============================================================================
// Score Adjustment
// ============================================================================

// AdjustNodeScore applies RL-learned preferences to a node's score
func (rl *RLOptimizer) AdjustNodeScore(baseScore float64, action SchedulingAction, node *NodeScore, workload *Workload) float64 {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	adjustedScore := baseScore

	// Apply node preference from RL action
	switch action.NodePreference {
	case "cheapest":
		// Boost score for cheaper nodes
		costBonus := (50.0 - node.CostPerHour) / 50.0 * 15.0
		adjustedScore += costBonus
	case "fastest":
		// Boost score for nodes with more free GPUs and lower utilization
		speedBonus := float64(node.GPUFreeCount) * 3.0
		if node.GPUUtilization < 50 {
			speedBonus += 10.0
		}
		adjustedScore += speedBonus
	case "topology-optimal":
		// Boost topology score weight
		adjustedScore += node.TopologyScore * 0.2
	case "balanced":
		// No adjustment, use base score
	}

	// Clamp
	if adjustedScore < 0 {
		adjustedScore = 0
	}
	if adjustedScore > 100 {
		adjustedScore = 100
	}

	return adjustedScore
}

// GetStatistics returns RL optimizer statistics
func (rl *RLOptimizer) GetStatistics() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	stateCount := len(rl.qTable)
	totalQEntries := 0
	for _, actions := range rl.qTable {
		totalQEntries += len(actions)
	}

	return map[string]interface{}{
		"episodes":          rl.episodes,
		"q_table_states":    stateCount,
		"q_table_entries":   totalQEntries,
		"exploration_rate":  rl.config.ExplorationRate,
		"avg_reward":        rl.avgReward,
		"learning_rate":     rl.config.LearningRate,
		"discount_factor":   rl.config.DiscountFactor,
	}
}

// ============================================================================
// Neural Policy Bridge — ONNX-based PPO/SAC inference
// ============================================================================

// NeuralPolicyConfig configures the neural network policy bridge.
type NeuralPolicyConfig struct {
	ONNXModelPath   string  // Path to exported ONNX model (from Python PPO/SAC trainer)
	FallbackToQLearning bool // Fall back to Q-learning if ONNX loading fails
	AIEngineAddr    string  // Python AI engine HTTP endpoint for remote inference
	ConfidenceThreshold float64 // Min confidence to use neural policy (0-1)
}

// NeuralPolicyBridge provides a unified interface for neural network-based
// scheduling decisions. It supports two modes:
//   1. Remote inference via AI engine HTTP API (PPO/SAC model served by Python)
//   2. Fallback to tabular Q-learning when neural model is unavailable
//
// The ONNX Runtime integration point is defined but actual ONNX loading
// requires the onnxruntime-go CGo binding (github.com/yalue/onnxruntime_go),
// which is optional. In production, the recommended path is remote inference
// via the AI engine's /api/v1/rl/predict endpoint.
type NeuralPolicyBridge struct {
	config    NeuralPolicyConfig
	qLearning *RLOptimizer
	logger    *logrus.Logger
	mu        sync.RWMutex
	modelLoaded bool
}

// NewNeuralPolicyBridge creates a policy bridge with Q-learning fallback.
func NewNeuralPolicyBridge(cfg NeuralPolicyConfig, qlCfg RLOptimizerConfig) *NeuralPolicyBridge {
	bridge := &NeuralPolicyBridge{
		config:    cfg,
		qLearning: NewRLOptimizer(qlCfg),
		logger:    logrus.StandardLogger(),
	}

	// Attempt to validate ONNX model path
	if cfg.ONNXModelPath != "" {
		bridge.logger.WithField("model", cfg.ONNXModelPath).Info(
			"Neural policy ONNX model configured (remote inference preferred)")
	}

	if cfg.AIEngineAddr != "" {
		bridge.logger.WithField("endpoint", cfg.AIEngineAddr).Info(
			"Neural policy remote inference endpoint configured")
	}

	return bridge
}

// ContinuousAction represents the output of a neural policy (PPO/SAC).
// Maps to the 3-dimensional continuous action space defined in the Python Gym env.
type ContinuousAction struct {
	NodePreference      float64 `json:"node_preference"`       // [0, 1] → node ranking selection
	GPUShareRatio       float64 `json:"gpu_share_ratio"`        // [0, 1] → 0.25-1.0
	PreemptionWillingness float64 `json:"preemption_willingness"` // [0, 1] → probability
}

// Predict produces a scheduling action. It tries neural policy first,
// then falls back to Q-learning.
func (b *NeuralPolicyBridge) Predict(state SchedulingState, observation []float64) (*ContinuousAction, string) {
	// Try remote neural inference
	if b.config.AIEngineAddr != "" {
		action, err := b.remoteInference(observation)
		if err == nil {
			return action, "neural_remote"
		}
		b.logger.WithError(err).Debug("Remote neural inference failed, falling back")
	}

	// Fallback: convert Q-learning discrete action to continuous
	qAction := b.qLearning.SelectAction(state)
	continuous := discreteToContinuous(qAction)
	return continuous, "q_learning_fallback"
}

// remoteInference calls the Python AI engine for neural policy prediction.
func (b *NeuralPolicyBridge) remoteInference(observation []float64) (*ContinuousAction, error) {
	if b.config.AIEngineAddr == "" {
		return nil, fmt.Errorf("no AI engine address configured")
	}

	// Build request payload
	payload := map[string]interface{}{
		"observation": observation,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/rl/predict", b.config.AIEngineAddr)

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non-200 response: %d", resp.StatusCode)
	}

	var result struct {
		Action []float64 `json:"action"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Action) < 3 {
		return nil, fmt.Errorf("unexpected action dim: %d", len(result.Action))
	}

	return &ContinuousAction{
		NodePreference:        clamp(result.Action[0], 0, 1),
		GPUShareRatio:         clamp(result.Action[1], 0, 1),
		PreemptionWillingness: clamp(result.Action[2], 0, 1),
	}, nil
}

// UpdateFromOutcome feeds scheduling outcome back to Q-learning (online learning).
func (b *NeuralPolicyBridge) UpdateFromOutcome(
	state SchedulingState,
	action SchedulingAction,
	reward float64,
	nextState SchedulingState,
) {
	b.qLearning.UpdateQValue(state, action, reward, nextState)
}

// GetStatistics returns combined statistics from both neural and Q-learning.
func (b *NeuralPolicyBridge) GetBridgeStatistics() map[string]interface{} {
	qlStats := b.qLearning.GetStatistics()
	qlStats["neural_model_path"] = b.config.ONNXModelPath
	qlStats["neural_endpoint"] = b.config.AIEngineAddr
	qlStats["model_loaded"] = b.modelLoaded
	qlStats["mode"] = "hybrid_q_learning_neural"
	return qlStats
}

// discreteToContinuous maps a discrete Q-learning action to continuous space.
func discreteToContinuous(action SchedulingAction) *ContinuousAction {
	ca := &ContinuousAction{}

	// Map node preference
	switch action.NodePreference {
	case "cheapest":
		ca.NodePreference = 0.0  // pick top-ranked (cheapest)
	case "fastest":
		ca.NodePreference = 0.25
	case "balanced":
		ca.NodePreference = 0.5
	case "topology-optimal":
		ca.NodePreference = 0.75
	default:
		ca.NodePreference = 0.5
	}

	// Map GPU share mode
	switch action.GPUShareMode {
	case "exclusive":
		ca.GPUShareRatio = 1.0
	case "mps":
		ca.GPUShareRatio = 0.5
	case "mig":
		ca.GPUShareRatio = 0.3
	default:
		ca.GPUShareRatio = 1.0
	}

	// Map preemption
	switch action.PreemptionChoice {
	case "allow":
		ca.PreemptionWillingness = 0.8
	case "avoid":
		ca.PreemptionWillingness = 0.1
	default:
		ca.PreemptionWillingness = 0.5
	}

	return ca
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
