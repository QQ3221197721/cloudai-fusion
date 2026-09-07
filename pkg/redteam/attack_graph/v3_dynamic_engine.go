package attack_graph

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Self-Evolving Attack Graph Engine - OBCE3 Original Algorithm Patent #1
// ============================================================================
// This engine implements a Q-Learning based dynamic attack path optimization system.
// The algorithm discovers optimal attack sequences through trial-and-error exploration,
// learning from historical engagement data to minimize detection probability while
// maximizing exploitation success rate.
//
// Reference: Sutton & Barto (2018) Reinforcement Learning: An Introduction, Theorem 6.1
// Convergence Proof: Under standard RL assumptions (bounded rewards, exploring starts),
// Q-learning converges to optimal policy with probability 1.
//
// State Space S = All possible CVE combination subgraphs
// Action Space A = {AddEdge, RemoveEdge, ReRoute, SkipNode, Parallelize, Chain, Terminate}
// Reward Function R(path) = alpha*SuccessRate + beta*StealthScore - gamma*DetectionProbability
// where � =0.4, � =0.3, � =0.3 (normalized weights summing to 1.0)
//
// Implementation Priority: P0 (Core capability, must reach production-grade quality)
// Coverage Requirement:   ?0% unit test coverage
// Performance Target: Query latency <1ms for state lookup

// DynamicAttackGraph is the core self-evolving attack planning engine
type DynamicAttackGraph struct {
	logger *logrus.Logger

	// State space representation
	// Each state is a directed acyclic graph (DAG) of exploited vulnerabilities
	stateGraph *StateGraph

	// Q-Learning agent components
	qTable       *QTable              // State�  Action value table
	epsilon      float64              // Exploration rate (epsilon-greedy)
	learningRate float64              // �  parameter (typically 0.1)
	discountFactor float64             // �  parameter (typically 0.9)
	rewardWeight SuccessRateWeight    // �  for success rate component
	stealthWeight StealthWeight       // �  for stealth component
	detectionWeight DetectionWeight   // �  for detection penalty

	// Knowledge base (in-memory, replaces Neo4j TODO)
	knowledgeBase *VulnerabilityKnowledgeBase

	// MITRE ATT&CK mapping index
	mitreIndex *MitreAttacksIndex

	// Experience replay buffer for offline learning
	experienceBuffer *ExperienceReplayBuffer

	// Engagement history and telemetry
	engagementHistory map[EngagementID][]Finding
	telemetryChannel  chan Finding

	// Thread safety
	mu sync.RWMutex
}

// State represents an attack graph state (DAG of exploited CVEs)
type State struct {
	// Exploited nodes (CVE IDs)
	exploitedNodes []string

	// Current position in attack chain
	currentNode string

	// Access level achieved
	accessLevel AccessLevel

	// Network topology reached
	networkTopology NetworkTopology
}

// Hash computes a unique identifier for this state
func (s *State) Hash() string {
	return fmt.Sprintf("%v-%v-%v-%v", s.exploitedNodes, s.currentNode, s.accessLevel, s.networkTopology)
}

// Action represents an atomic attack graph modification operation
type Action int

const (
	ActionNone            Action = iota // No operation
	ActionAddEdge                       // Add new vulnerability to chain
	ActionRemoveEdge                    // Prune ineffective path
	ActionReRoute                       // Change attack sequence
	ActionSkipNode                      // Bypass detected vulnerability
	ActionParallelize                   // Launch concurrent attacks
	ActionChain                         // Execute full exploit chain
	ActionTerminate                     // End engagement
)

func (a Action) String() string {
	names := [...]string{
		"None", "AddEdge", "RemoveEdge", "ReRoute",
		"SkipNode", "Parallelize", "Chain", "Terminate",
	}
	if a < Action(len(names)) {
		return names[a]
	}
	return fmt.Sprintf("Unknown(%d)", a)
}

// AllActions is the complete action space for the Q-learning agent
var AllActions = []Action{ActionAddEdge, ActionRemoveEdge, ActionReRoute, ActionSkipNode, ActionParallelize, ActionChain}

// ============================================================================
// Q-Learning Core Implementation
// ============================================================================

// NewDynamicAttackGraph creates a new self-evolving attack graph engine
func NewDynamicAttackGraph(logger *logrus.Logger, rec evidence.Recorder) *DynamicAttackGraph {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	engine := &DynamicAttackGraph{
		logger: logger.WithField("component", "dynamic_attack_graph"),

		// Initialize Q-learning parameters (standard RL hyperparameters)
		epsilon:      0.1,    // 10% exploration rate
		learningRate: 0.1,    // �  = 0.1 (convergence guarantee per Sutton&Barto Thm 6.1)
		discountFactor: 0.9,  // �  = 0.9 (future reward weighting)
		rewardWeight: SuccessRateWeight(0.4), // �  = 0.4
		stealthWeight: StealthWeight(0.3),    // �  = 0.3
		detectionWeight: DetectionWeight(0.3),// �  = 0.3

		// Initialize components
		stateGraph:         NewStateGraph(),
		knowledgeBase:      NewVulnerabilityKnowledgeBase(),
		mitreIndex:         NewMitreAttacksIndex(),
		experienceBuffer:   NewExperienceReplayBuffer(10000),
		engagementHistory:  make(map[EngagementID][]Finding),
		telemetryChannel:   make(chan Finding, 100),
		qTable:             NewQTable(),
	}

	// Load vulnerability knowledge base from NVD API
	if err := engine.knowledgeBase.Initialize(context.Background()); err != nil {
		logger.WithError(err).Warn("Failed to load NVD database, using empty KB")
	}

	logger.Info("Self-Evolving Attack Graph engine initialized successfully")
	return engine
}

// Q-Learning Update Rule: Q(s,a)   ?Q(s,a) + � [R + � � max Q(s',a') - Q(s,a)]
// This is the core Bellman optimality equation update step
func (dag *DynamicAttackGraph) UpdateQValue(state State, action Action, reward float64, nextState State) {
	dag.mu.Lock()
	defer dag.mu.Unlock()

	// Get current Q-value
	currentQ := dag.qTable.Get(state, action)

	// Compute max Q-value for next state across all actions
	var maxNextQ float64 = 0.0
	for _, nextAction := range AllActions {
		nextQ := dag.qTable.Get(nextState, nextAction)
		if nextQ > maxNextQ {
			maxNextQ = nextQ
		}
	}

	// Bellman update: Q(s,a)   ?Q(s,a) + � [R + � � max Q(s',a') - Q(s,a)]
	target := reward + dag.discountFactor*maxNextQ
	delta := target - currentQ
	newQ := currentQ + dag.learningRate*delta

	// Store updated Q-value
	dag.qTable.Set(state, action, newQ)

	// Record experience to replay buffer
	dag.experienceBuffer.Add(Experience{
		State:      state,
		Action:     action,
		Reward:     reward,
		NextState:  nextState,
		Timestamp:  time.Now(),
	})

	dag.logger.WithFields(logrus.Fields{
		"state_hash":  state.Hash(),
		"action":      action.String(),
		"current_q":   currentQ,
		"reward":      reward,
		"max_next_q":  maxNextQ,
		"new_q":       newQ,
		"delta":       delta,
	}).Trace("Q-value updated")
}

// SelectAction implements epsilon-greedy policy selection
// With probability � : explore random action
// With probability 1-� : exploit best known action
func (dag *DynamicAttackGraph) SelectAction(state State) Action {
	dag.mu.RLock()
	defer dag.mu.RUnlock()

	// Epsilon-greedy exploration
	if dag.randFloat64() < dag.epsilon {
		// Random exploration
		actionIdx := dag.randInt(len(AllActions))
		return AllActions[actionIdx]
	}

	// Greedy exploitation: argmax_a Q(s,a)
	var bestAction Action
	var bestQ float64 = math.MinFloat64

	for _, action := range AllActions {
		qValue := dag.qTable.Get(state, action)
		if qValue > bestQ {
			bestQ = qValue
			bestAction = action
		}
	}

	return bestAction
}

// ============================================================================
// Reward Function Design
// ============================================================================
// R(path) = � � SuccessRate + � � StealthScore   ?� � DetectionProbability
//
// SuccessRate: Historical exploitation success rate for this CVE (0-100%)
// StealthScore: Fraction of EDR/AV bypassed successfully (0-1)
// DetectionProbability: Probability of triggering security alerts (0-1)
//
// Weight normalization: � +� +� =1.0 ensures reward stays in bounded range [-0.3, 0.7]

// SuccessRateWeight is the �  coefficient for success rate component
type SuccessRateWeight float64

func (w SuccessRateWeight) Value() float64 {
	return float64(w)
}

// StealthWeight is the �  coefficient for stealth component
type StealthWeight float64

func (w StealthWeight) Value() float64 {
	return float64(w)
}

// DetectionWeight is the �  coefficient for detection penalty
type DetectionWeight float64

func (w DetectionWeight) Value() float64 {
	return float64(w)
}

// CalculateReward computes the multi-component reward signal
func (dag *DynamicAttackGraph) CalculateReward(path AttackPath) float64 {
	// Component 1: Success Rate (positive contribution)
	successRate := float64(path.SuccessRate) / 100.0
	successContribution := dag.rewardWeight.Value() * successRate

	// Component 2: Stealth Score (positive contribution)
	// Stealth = avoidedEDR / totalStages
	stealthScore := float64(path.AvoidedEDR) / float64(len(path.Stages))
	if len(path.Stages) == 0 {
		stealthScore = 1.0 // Perfect stealth if no stages executed
	}
	stealthContribution := dag.stealthWeight.Value() * stealthScore

	// Component 3: Detection Probability (negative penalty)
	detectionProb := float64(path.TriggeredAlerts) / float64(len(path.Stages))
	if len(path.Stages) == 0 {
		detectionProb = 0.0 // No detections if nothing executed
	}
	detectionPenalty := dag.detectionWeight.Value() * detectionProb

	// Final reward = � � Success + � � Stealth   ?� � Detection
	reward := successContribution + stealthContribution - detectionPenalty

	dag.logger.WithFields(logrus.Fields{
		"success_contrib":  successContribution,
		"stealth_contrib":  stealthContribution,
		"detection_penalty": detectionPenalty,
		"total_reward":     reward,
	}).Debug("Reward calculated")

	return reward
}

// ============================================================================
// Path Optimization Interface
// ============================================================================

// OptimizeAttackPath finds the optimal attack sequence given initial reconnaissance
func (dag *DynamicAttackGraph) OptimizeAttackPath(ctx context.Context, reconData ReconnaissanceData) (*OptimizedAttackPath, error) {
	dag.mu.Lock()
	defer dag.mu.Unlock()

	// Step 1: Construct initial state from reconnaissance
	initialState := dag.buildInitialState(reconData)

	// Step 2: Plan multiple candidate paths in parallel
	candidatePaths := dag.generateCandidatePaths(initialState, reconData.Targets)

	// Step 3: Evaluate each path using Q-table values
	var bestPath *OptimizedAttackPath
	var bestScore float64 = math.MinFloat64

	for _, path := range candidatePaths {
		score := dag.evaluatePathQuality(path)
		if score > bestScore {
			bestScore = score
			bestPath = path
		}
	}

	if bestPath == nil {
		return nil, fmt.Errorf("no valid attack path found")
	}

	dag.logger.WithFields(logrus.Fields{
		"path_length": len(bestPath.Stages),
		"expected_success_rate": bestPath.ExpectedSuccessRate,
		"expected_detection_prob": bestPath.ExpectedDetectionProbability,
	}).Info("Optimal attack path selected")

	return bestPath, nil
}

// buildInitialState constructs the starting state from reconnaissance data
func (dag *DynamicAttackGraph) buildInitialState(recon ReconnaissanceData) State {
	state := State{
		exploitedNodes: []string{},
		currentNode:    recon.InitialAccessPoint,
		accessLevel:    AccessLevelNone,
		networkTopology: NetworkTopology{
			Workstations: recon.WorkstationCount,
			Servers:      recon.ServerCount,
			DomainControllers: recon.DomainControllerCount,
		},
	}

	// Pre-populate with known vulnerable CVEs from KB
	for _, target := range recon.Targets {
		if cves := dag.knowledgeBase.GetVulnerableCVEs(target.IP); len(cves) > 0 {
			state.exploitedNodes = append(state.exploitedNodes, cves...)
		}
	}

	return state
}

// generateCandidatePaths generates multiple attack sequences for evaluation
func (dag *DynamicAttackGraph) generateCandidatePaths initialState State, targets []TargetDiscovery) []*AttackPath {
	paths := []*AttackPath{}

	// Strategy 1: Direct exploitation (high risk, high reward)
	paths = append(paths, dag.buildDirectExploitPath(initialState, targets))

	// Strategy 2: Lateral movement first (lower risk)
	paths = append(paths, dag.buildLateralMovementPath(initialState, targets))

	// Strategy 3: Privilege escalation focus (domain admin goal)
	paths = append(paths, dag.buildPrivEscPath(initialState, targets))

	// Strategy 4: Multi-vector parallel attack (advanced)
	paths = append(paths, dag.buildMultiVectorPath(initialState, targets))

	return paths
}

// evaluatePathQuality scores a candidate path using Q-values
func (dag *DynamicAttackGraph) evaluatePathQuality(path *AttackPath) float64 {
	var totalQ float64

	for stageIdx, stage := range path.Stages {
		// Construct state at this stage
		state := dag.constructStageState(path, stageIdx)

		// Get Q-value for this stage's action
		action := dag.mapStageToAction(stage)
		qValue := dag.qTable.Get(state, action)
		totalQ += qValue
	}

	// Normalize by path length
	avgQ := totalQ / float64(len(path.Stages))

	// Combine with reward function
	reward := dag.CalculateReward(*path)

	// Final score = weighted average of Q-value and reward
	return 0.5*avgQ + 0.5*reward
}

// constructStageState builds the state representation at a specific path stage
func (dag *DynamicAttackGraph) constructStageState(path *AttackPath, stageIdx int) State {
	state := State{
		exploitedNodes: path.Stages[:stageIdx].CVEIDs,
		currentNode:    path.Stages[stageIdx].Target.IP,
		accessLevel:    path.Stages[stageIdx].AchievedPrivilege,
		networkTopology: path.NetworkTopology,
	}
	return state
}

// mapStageToAction converts an attack stage to Q-learning action
func (dag *DynamicAttackGraph) mapStageToAction(stage AttackStage) Action {
	switch stage.Type {
	case StageInitialAccess:
		return ActionAddEdge
	case StageExecution:
		return ActionChain
	case StagePersistence:
		return ActionAddEdge
	case StagePrivilegeEscalation:
		return ActionAddEdge
	case StageDefenseEvasion:
		return ActionSkipNode
	case StageCredentialAccess:
		return ActionParallelize
	case StageDiscovery:
		return ActionAddEdge
	case StageLateralMovement:
		return ActionReRoute
	case StageCollection:
		return ActionChain
	case StageExfiltration:
		return ActionTerminate
	default:
		return ActionNone
	}
}

// ============================================================================
// Offline Learning from History
// ============================================================================

// TrainFromHistory performs offline Q-learning on past engagement data
func (dag *DynamicAttackGraph) TrainFromHistory(ctx context.Context, engagementIDs []EngagementID) error {
	dag.mu.Lock()
	defer dag.mu.Unlock()

	dag.logger.Infof("Starting offline training on %d engagements", len(engagementIDs))

	// Collect all experiences from history
	allExperiences := []Experience{}
	for _, engID := range engagementIDs {
		findings := dag.engagementHistory[engID]
		exps := dag.extractExperiencesFromFindings(findings)
		allExperiences = append(allExperiences, exps...)
	}

	// Shuffle experiences for SGD-like updates
	dag.shuffleExperiences(allExperiences)

	// Mini-batch gradient descent on Q-values
	batchSize := 32
	for epoch := 0; epoch < 10; epoch++ {
		for i := 0; i < len(allExperiences); i += batchSize {
			end := i + batchSize
			if end > len(allExperiences) {
				end = len(allExperiences)
			}

			batch := allExperiences[i:end]
			dag.updateQValuesFromBatch(batch)
		}
	}

	dag.logger.Info("Offline training completed")
	return nil
}

// extractExperiencesFromFindings converts historical findings into Q-learning experiences
func (dag *DynamicAttackGraph) extractExperiencesFromFindings(findings []Finding) []Experience {
	experiences := []Experience{}

	for i := 0; i < len(findings)-1; i++ {
		curr := findings[i]
		next := findings[i+1]

		state := State{
			exploitedNodes: []string{curr.CVEID},
			currentNode:    curr.TargetIP,
			accessLevel:    curr.AccessLevel,
		}

		nextState := State{
			exploitedNodes: []string{next.CVEID},
			currentNode:    next.TargetIP,
			accessLevel:    next.AccessLevel,
		}

		action := ActionAddEdge // Simplified mapping

		// Compute reward based on whether next finding advanced the attack
		reward := 0.0
		if next.AccessLevel > curr.AccessLevel {
			reward = 1.0 // Positive reward for privilege escalation
		} else if next.DetectedByEDR {
			reward = -0.5 // Negative reward for detection
		}

		experiences = append(experiences, Experience{
			State:     state,
			Action:    action,
			Reward:    reward,
			NextState: nextState,
		})
	}

	return experiences
}

// updateQValuesFromBatch performs Q-learning update on a mini-batch
func (dag *DynamicAttackGraph) updateQValuesFromBatch(batch []Experience) {
	for _, exp := range batch {
		dag.UpdateQValue(exp.State, exp.Action, exp.Reward, exp.NextState)
	}
}

// shuffleExperiences randomizes experience order (Fisher-Yates shuffle)
func (dag *DynamicAttackGraph) shuffleExperiences(exps []Experience) {
	for i := len(exps) - 1; i > 0; i-- {
		j := dag.randInt(i + 1)
		exps[i], exps[j] = exps[j], exps[i]
	}
}

// ============================================================================
// Utility Methods
// ============================================================================

// SaveQTable persists the learned Q-table to disk for reuse
func (dag *DynamicAttackGraph) SaveQTable(filepath string) error {
	dag.mu.RLock()
	defer dag.mu.RUnlock()

	return dag.qTable.SaveToFile(filepath)
}

// LoadQTable restores a previously saved Q-table
func (dag *DynamicAttackGraph) LoadQTable(filepath string) error {
	dag.mu.Lock()
	defer dag.mu.Unlock()

	if err := dag.qTable.LoadFromFile(filepath); err != nil {
		return fmt.Errorf("failed to load Q-table: %w", err)
	}

	dag.logger.Info("Q-table loaded from disk")
	return nil
}

// GetQValue returns the current Q-value for a state-action pair
func (dag *DynamicAttackGraph) GetQValue(state State, action Action) float64 {
	dag.mu.RLock()
	defer dag.mu.RUnlock()
	return dag.qTable.Get(state, action)
}

// DecayEpsilon reduces exploration rate over time (annealing)
func (dag *DynamicAttackGraph) DecayEpsilon(decayRate float64, minEpsilon float64) {
	dag.mu.Lock()
	defer dag.mu.Unlock()

	dag.epsilon *= decayRate
	if dag.epsilon < minEpsilon {
		dag.epsilon = minEpsilon
	}

	dag.logger.WithFields(logrus.Fields{
		"old_epsilon": dag.epsilon / decayRate,
		"new_epsilon": dag.epsilon,
	}).Debug("Epsilon decayed")
}

// randFloat64 returns a random float in [0, 1)
func (dag *DynamicAttackGraph) randFloat64() float64 {
	return math.Float64frombits((math.MaxUint64 >> 1) & dag.randomBits())
}

// randInt returns a random integer in [0, n)
func (dag *DynamicAttackGraph) randInt(n int) int {
	return int(math.Float64frombits((math.MaxUint64 >> 1) & dag.randomBits()) * float64(n))
}

// randomBits generates pseudo-random bits (placeholder for crypto/rand)
func (dag *DynamicAttackGraph) randomBits() uint64 {
	// TODO: Replace with cryptographically secure random source
	return uint64(time.Now().UnixNano())
}
