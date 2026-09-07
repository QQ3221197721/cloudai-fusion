// Package orchestration - Core Q-Learning structures (Patent #13)
package orchestration

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// SPARSE Q-TABLE IMPLEMENTATION
// ============================================================================

// SparseQTable implements memory-efficient Q-value storage using sparse representation
type SparseQTable struct {
	mu     sync.RWMutex
	table  map[string]map[string]float64 // state -> action -> q-value
	logger *logrus.Logger
}

// NewSparseQTable creates a new sparse Q-table
func NewSparseQTable(logger *logrus.Logger) *SparseQTable {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &SparseQTable{
		table:  make(map[string]map[string]float64),
		logger: logger,
	}
}

// GetQValue retrieves Q-value for state-action pair
func (qt *SparseQTable) GetQValue(state, action string) float64 {
	qt.mu.RLock()
	defer qt.mu.RUnlock()
	
	if actions, exists := qt.table[state]; exists {
		if qvalue, exists := actions[action]; exists {
			return qvalue
		}
	}
	
	// Return initial value (0.5) if not found
	return 0.5
}

// UpdateQValue sets Q-value for state-action pair
func (qt *SparseQTable) UpdateQValue(state, action string, qvalue float64) {
	qt.mu.Lock()
	defer qt.mu.Unlock()
	
	if _, exists := qt.table[state]; !exists {
		qt.table[state] = make(map[string]float64)
	}
	
	qt.table[state][action] = qvalue
	
	qt.logger.WithFields(logrus.Fields{
		"state":  state,
		"action": action,
		"value":  qvalue,
	}).Debug("Q-table updated")
}

// GetMaxQValue finds maximum Q-value across all actions in given state
func (qt *SparseQTable) GetMaxQValue(state string) float64 {
	qt.mu.RLock()
	defer qt.mu.RUnlock()
	
	if actions, exists := qt.table[state]; exists {
		maxQ := -999999.0
		for _, qvalue := range actions {
			if qvalue > maxQ {
				maxQ = qvalue
			}
		}
		
		if maxQ != -999999.0 {
			return maxQ
		}
	}
	
	// Return default initial value
	return 0.5
}

// GetAllActions returns all actions for a given state
func (qt *SparseQTable) GetAllActions(state string) []string {
	qt.mu.RLock()
	defer qt.mu.RUnlock()
	
	if actions, exists := qt.table[state]; exists {
		result := make([]string, 0, len(actions))
		for action := range actions {
			result = append(result, action)
		}
		return result
	}
	
	return make([]string, 0)
}

// ============================================================================
// Q-LEARNING AGENT WITH EPSILON-GREEDY POLICY
// ============================================================================

// QAgent implements epsilon-greedy policy for action selection with history tracking
type QAgent struct {
	mu              sync.RWMutex
	QTable          *SparseQTable
	ExplorationRate float64
	Gamma           float64
	LR              float64
	History         []ActionHistoryItem
	TotalSteps      int
	TotalRewards    float64
}

// ActionHistoryItem records a single step in the learning process
type ActionHistoryItem struct {
	State       string
	Action      string
	Reward      float64
	NextState   string
	Done        bool
	Timestamp   time.Time
}

// SelectAction chooses action using epsilon-greedy strategy
func (qa *QAgent) SelectAction(state string, availableActions []string) string {
	qa.mu.Lock()
	defer qa.mu.Unlock()
	
	qa.TotalSteps++
	
	// Epsilon-greedy: explore with probability ε, exploit otherwise
	if randFloat64() < qa.ExplorationRate {
		// Exploration: select random action
		if len(availableActions) > 0 {
			randomIndex := int(randFloat64() * float64(len(availableActions)))
			return availableActions[randomIndex]
		}
		return ""
	}
	
	// Exploitation: select best action
	bestAction := ""
	bestQ := -999999.0
	
	for _, action := range availableActions {
		qvalue := qa.QTable.GetQValue(state, action)
		if qvalue > bestQ {
			bestQ = qvalue
			bestAction = action
		}
	}
	
	if bestAction != "" {
		return bestAction
	}
	
	// Fallback to random if no actions evaluated yet
	if len(availableActions) > 0 {
		return availableActions[int(randFloat64()*float64(len(availableActions)))]
	}
	
	return ""
}

// RecordExperience stores transition in replay buffer/history
func (qa *QAgent) RecordExperience(state, action string, reward float64, nextState string, done bool) {
	qa.mu.Lock()
	defer qa.mu.Unlock()
	
	item := ActionHistoryItem{
		State:     state,
		Action:    action,
		Reward:    reward,
		NextState: nextState,
		Done:      done,
		Timestamp: time.Now(),
	}
	
	qa.History = append(qa.History, item)
	qa.TotalRewards += reward
	
	// Limit history size to prevent unbounded growth
	maxHistorySize := 10000
	if len(qa.History) > maxHistorySize {
		qa.History = qa.History[len(qa.History)-maxHistorySize:]
	}
}

// GetAverageReward calculates average reward over recent experience
func (qa *QAgent) GetAverageReward(window int) float64 {
	qa.mu.RLock()
	defer qa.mu.RUnlock()
	
	if len(qa.History) == 0 {
		return 0.0
	}
	
	start := len(qa.History) - window
	if start < 0 {
		start = 0
	}
	
	sum := 0.0
	for i := start; i < len(qa.History); i++ {
		sum += qa.History[i].Reward
	}
	
	return sum / float64(window)
}

// DecayExplorationRate reduces exploration rate over time (epsilon decay)
func (qa *QAgent) DecayExplorationRate(decayFactor, minRate float64) {
	qa.mu.Lock()
	defer qa.mu.Unlock()
	
	if qa.ExplorationRate > minRate {
		qa.ExplorationRate *= decayFactor
	}
}

// ============================================================================
// ATTACK SIMULATOR ENVIRONMENT
// ============================================================================

// AttackSimulator simulates attack environment for training and evaluation
type AttackSimulator struct {
	mu              sync.RWMutex
	maxConcurrent   int
	activeAttacks   int
	completedAttacks int
	failedAttacks   int
	logger          *logrus.Logger
	
	// Simulation configuration
	defaultSuccessProb  float64
	defaultStealthScore float64
	defaultDetectionCount int
	
	// Performance tracking
	totalExploitTimeMS int64
	successRate        float64
}

// NewAttackSimulator creates a new attack simulator
func NewAttackSimulator(maxConcurrent int, logger *logrus.Logger) *AttackSimulator {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &AttackSimulator{
		maxConcurrent:       maxConcurrent,
		defaultSuccessProb:  0.5,
		defaultStealthScore: 0.7,
		defaultDetectionCount: 1,
		logger:              logger,
	}
}

// ExecuteAttack simulates a single attack attempt
func (as *AttackSimulator) ExecuteAttack(attempt *AttackAttempt) (*AttackResult, error) {
	as.mu.Lock()
	if as.activeAttacks >= as.maxConcurrent {
		as.mu.Unlock()
		return nil, ErrTooManyActiveAttacks
	}
	as.activeAttacks++
	as.mu.Unlock()
	
	defer func() {
		as.mu.Lock()
		as.activeAttacks--
		as.completedAttacks++
		as.mu.Unlock()
	}()
	
	// Simulate delay based on attack type
	delay := simulateDelayMs(attempt.Action)
	time.Sleep(time.Duration(delay) * time.Millisecond)
	
	// Determine success based on Q-values (simulated)
	successProb := as.calculateSuccessProbability(attempt.State, attempt.Action)
	isSuccess := simulatedRandom(successProb)
	
	// Calculate metrics
	metrics := &AttackMetrics{
		SuccessRate:   successProb,
		StealthScore:  as.defaultStealthScore,
		DetectionCount: as.defaultDetectionCount + int(randFloat64()*2),
		ElapsedTimeMS: int64(delay),
		ResourceUsage: 0.4 + randFloat64()*0.5,
	}
	
	// Record outcome
	if isSuccess {
		as.mu.Lock()
		as.successRate = float64(as.completedAttacks-as.failedAttacks) / float64(as.completedAttacks)
		as.totalExploitTimeMS += delay
		as.mu.Unlock()
	} else {
		as.mu.Lock()
		as.failedAttacks++
		as.mu.Unlock()
	}
	
	result := &AttackResult{
		PathID:    attempt.PathID,
		AttackType: extractAttackType(attempt.Action),
		Timestamp: time.Now(),
		StartTime: time.Now().Add(-time.Duration(delay)*time.Millisecond),
		EndTime:   time.Now(),
		Success:   isSuccess,
		Metrics:   metrics,
	}
	
	if isSuccess {
		result.Credentials = []CredentialDump{
			{
				Type:        "SimulatedCredentials",
				Username:    attempt.Target,
				Password:    simulateCredentialHash(),
				Source:      "Simulation",
				Timestamp:   time.Now(),
				Verified:    true,
			},
		}
	}
	
	return result, nil
}

// calculateSuccessProbability combines multiple factors to estimate exploit success
func (as *AttackSimulator) calculateSuccessProbability(state, action string) float64 {
	// Base probability from Q-learning simulation
	baseProb := 0.5 + (randFloat64()*0.4)
	
	// Add complexity factor based on attack type
	switch action {
	case "launch_phishing":
		baseProb *= 0.8 // Phishing has lower success rate
	case "execute_rce":
		baseProb *= 1.2 // RCE can have higher success if vuln exists
	case "relay_ntlm":
		baseProb *= 0.9 // NTLM relay is moderate difficulty
	}
	
	// Ensure bounds
	if baseProb > 1.0 {
		baseProb = 1.0
	}
	if baseProb < 0.1 {
		baseProb = 0.1
	}
	
	return baseProb
}

// GetPerformanceStats returns simulator performance statistics
func (as *AttackSimulator) GetPerformanceStats() SimulatorStats {
	as.mu.RLock()
	defer as.mu.RUnlock()
	
	avgExploitTime := int64(0)
	if as.completedAttacks > 0 {
		avgExploitTime = as.totalExploitTimeMS / int64(as.completedAttacks)
	}
	
	return SimulatorStats{
		MaxConcurrent:   as.maxConcurrent,
		ActiveAttacks:   as.activeAttacks,
		CompletedAttacks: as.completedAttacks,
		FailedAttacks:   as.failedAttacks,
		AverageSuccessRate: as.successRate,
		AverageExploitTimeMS: avgExploitTime,
	}
}

// ============================================================================
// EVIDENCE COLLECTOR
// ============================================================================

// EvidenceCollector collects and organizes attack evidence
type EvidenceCollector struct {
	mu            sync.RWMutex
	logger        *logrus.Logger
	evidenceChain []EvidenceEntry
	attemptRecords map[string][]EvidenceEntry // path_id -> evidence
}

// NewEvidenceCollector creates a new evidence collector
func NewEvidenceCollector(logger *logrus.Logger) *EvidenceCollector {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &EvidenceCollector{
		logger:         logger,
		evidenceChain:  make([]EvidenceEntry, 0),
		attemptRecords: make(map[string][]EvidenceEntry),
	}
}

// Record stores an attack result's evidence
func (ec *EvidenceCollector) Record(result AttackResult) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	
	// Add result-level evidence
	for _, entry := range result.EvidenceChain {
		entry.Timestamp = result.StartTime
		ec.evidenceChain = append(ec.evidenceChain, entry)
	}
	
	// Store by path ID
	if _, exists := ec.attemptRecords[result.PathID]; !exists {
		ec.attemptRecords[result.PathID] = make([]EvidenceEntry, 0)
	}
	ec.attemptRecords[result.PathID] = append(ec.attemptRecords[result.PathID], result.EvidenceChain...)
	
	ec.logger.WithFields(logrus.Fields{
		"path_id": result.PathID,
		"type":    result.AttackType,
		"evidence_count": len(result.EvidenceChain),
		"success": result.Success,
	}).Debug("Evidence recorded")
}

// GetFullEvidenceChain returns complete attack timeline
func (ec *EvidenceCollector) GetFullEvidenceChain() []EvidenceEntry {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	
	chainCopy := make([]EvidenceEntry, len(ec.evidenceChain))
	copy(chainCopy, ec.evidenceChain)
	
	return chainCopy
}

// GetPathEvidence returns evidence for specific attack path
func (ec *EvidenceCollector) GetPathEvidence(pathID string) []EvidenceEntry {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	
	if evidence, exists := ec.attemptRecords[pathID]; exists {
		evidenceCopy := make([]EvidenceEntry, len(evidence))
		copy(evidenceCopy, evidence)
		return evidenceCopy
	}
	
	return make([]EvidenceEntry, 0)
}

// GenerateDAGVisualization creates attack graph visualization data
func (ec *EvidenceCollector) GenerateDAGVisualization() DAGNode {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	
	root := DAGNode{
		ID:   "root",
		Name: "Initial Access",
		Type: "Start",
	}
	
	currentNodes := []*DAGNode{&root}
	
	for _, pathID := range ec.getPathIDs() {
		evidence := ec.attemptRecords[pathID]
		if len(evidence) > 0 {
			node := &DAGNode{
				ID:   pathID,
				Name: pathID,
				Type: "Path",
				Parent: "root",
			}
			
			root.Children = append(root.Children, node)
			currentNodes = append(currentNodes, node)
		}
	}
	
	return root
}

// ============================================================================
// DATA STRUCTURES FOR VISUALIZATION
// ============================================================================

// DAGNode represents a node in the attack graph
type DAGNode struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Parent   string      `json:"parent,omitempty"`
	Children []*DAGNode  `json:"children,omitempty"`
	Evidence []EvidenceEntry `json:"evidence,omitempty"`
}

// ============================================================================
// STATISTICS AND REPORTING
// ============================================================================

// SimulatorStats contains simulator performance metrics
type SimulatorStats struct {
	MaxConcurrent      int
	ActiveAttacks      int
	CompletedAttacks   int
	FailedAttacks      int
	AverageSuccessRate float64
	AverageExploitTimeMS int64
}

// Error definitions
var ErrTooManyActiveAttacks = fmt.Errorf("maximum concurrent attacks reached")

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func simulateDelayMs(action string) int64 {
	baseDelays := map[string]int64{
		"launch_phishing": 3000,
		"execute_rce":     2000,
		"relay_ntlm":      1500,
	}
	
	if delay, exists := baseDelays[action]; exists {
		return delay + randInt64()%2000
	}
	
	return 2000 + randInt64()%2000
}

func extractAttackType(action string) string {
	switch action {
	case "launch_phishing":
		return "Phishing"
	case "execute_rce":
		return "RCE"
	case "relay_ntlm":
		return "NTLM_Relay"
	default:
		return "Unknown"
	}
}

func (ec *EvidenceCollector) getPathIDs() []string {
	ids := make([]string, 0, len(ec.attemptRecords))
	for id := range ec.attemptRecords {
		ids = append(ids, id)
	}
	return ids
}
