// Package orchestration - Q-Learning driven attack path orchestration engine
// Patent-protected: Reinforcement learning for cyber attack optimization
package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// ATTACK ORCHESTRATOR CORE
// ============================================================================

// AttackOrchestrator coordinates multiple parallel attack vectors using Q-Learning
type AttackOrchestrator struct {
	mu sync.RWMutex
	
	logger *logrus.Logger
	config *OrchestratorConfig
	
	// Q-Learning components
	qTable       *SparseQTable
	qAgent       *QAgent
	attackSimulator *AttackSimulator
	
	// Path management
	pathManager *PathManager
	
	// Evidence collection
	evidenceCollector *EvidenceCollector
	
	// Execution state
	state           OrchestratorState
	startTime       time.Time
	cancelFunc      context.CancelFunc
	wg              sync.WaitGroup
	
	// Parallel execution channels
	phishingChan    chan *AttackAttempt
	rceChan         chan *AttackAttempt
	ntlmChan        chan *AttackAttempt
	
	// Results
	results         []AttackResult
	successTracker  map[string]bool // path_id -> success
}

// OrchestratorConfig defines the orchestrator behavior parameters
type OrchestratorConfig struct {
	MaxConcurrentAttacks int
	RewardWeights       *RewardWeights
	QLearningParams     *QLearningParams
	TimeBudget          time.Duration
	EpsilonDecay        float64
	EarlyTermination    bool
}

// QLearningParams defines Q-Learning hyperparameters (Patent #13)
type QLearningParams struct {
	LearningRate      float64 // η
	DiscountFactor    float64 // γ
	ExplorationRate   float64 // ε
	ExplorationDecay  float64 // ε decay per episode
	MinExplorationRate float64
}

// RewardWeights defines multi-component reward scaling
type RewardWeights struct {
	SuccessWeight       float64
	StealthWeight       float64
	DetectionPenalty    float64
	TimeEfficiencyBonus float64
	ResourceUtilization float64
}

// OrchestratorState tracks the current state of attack orchestration
type OrchestratorState struct {
	Status            string
	ActivePaths       int
	CompletedPaths    int
	FailedPaths       int
	SucceededPaths    int
	TotalExploits     int
	SuccessfulExploits int
	AverageReward     float64
	FinalAchieved     string // e.g., "DomainAdmin", "DAC"
	ElapsedTime       time.Duration
}

// AttackResult represents a single attack attempt result
type AttackResult struct {
	PathID        string
	AttackType    string
	Timestamp     time.Time
	StartTime     time.Time
	EndTime       time.Time
	Success       bool
	RewardScore   float64
	Metrics       *AttackMetrics
	Credentials   []CredentialDump
	EvidenceChain []EvidenceEntry
}

// AttackMetrics captures exploit performance metrics
type AttackMetrics struct {
	SuccessRate    float64
	StealthScore   float64
	DetectionCount int
	ElapsedTimeMS  int64
	ResourceUsage  float64 // CPU/memory/utilization score
}

// ============================================================================
// INITIALIZATION AND LIFECYCLE
// ============================================================================

// NewAttackOrchestrator creates a new Q-Learning attack orchestrator
func NewAttackOrchestrator(ctx context.Context, logger *logrus.Logger) (*AttackOrchestrator, error) {
	if logger == nil {
		logger = logrus.New()
	}
	
	// Initialize with default configurations
	config := &OrchestratorConfig{
		MaxConcurrentAttacks: 5,
		RewardWeights: &RewardWeights{
			SuccessWeight:       0.40,
			StealthWeight:       0.30,
			DetectionPenalty:    0.20,
			TimeEfficiencyBonus: 0.10,
			ResourceUtilization: 0.20,
		},
		QLearningParams: &QLearningParams{
			LearningRate:      0.1,
			DiscountFactor:    0.95,
			ExplorationRate:   0.1,
			ExplorationDecay:  0.995,
			MinExplorationRate: 0.01,
		},
		EarlyTermination: true,
		TimeBudget:       10 * time.Minute,
	}
	
	// Initialize Q-table with sparse storage (optimized for large state spaces)
	qTable := NewSparseQTable(logger)
	
	// Create epsilon-greedy agent
	qAgent := &QAgent{
		QTable:         qTable,
		ExplorationRate: config.QLearningParams.ExplorationRate,
		Gamma:          config.QLearningParams.DiscountFactor,
		LR:             config.QLearningParams.LearningRate,
		History:        make([]ActionHistoryItem, 0),
	}
	
	// Initialize simulator
	simulator := NewAttackSimulator(config.MaxConcurrentAttacks, logger)
	
	// Create path manager
	pathMgr := NewPathManager(logger)
	
	// Create evidence collector
	evidenceCollector := NewEvidenceCollector(logger)
	
	orchestrator := &AttackOrchestrator{
		logger:            logger,
		config:            config,
		qTable:            qTable,
		qAgent:            qAgent,
		attackSimulator:   simulator,
		pathManager:       pathMgr,
		evidenceCollector: evidenceCollector,
		
		state: OrchestratorState{
			Status:            "Initialized",
			ActivePaths:       0,
			CompletedPaths:    0,
			FailedPaths:       0,
			SucceededPaths:    0,
			TotalExploits:     0,
			SuccessfulExploits: 0,
			AverageReward:     0.0,
			FinalAchieved:     "",
		},
		
		successTracker: make(map[string]bool),
		
		// Channel buffers for parallel execution
		phishingChan:    make(chan *AttackAttempt, 10),
		rceChan:         make(chan *AttackAttempt, 10),
		ntlmChan:        make(chan *AttackAttempt, 10),
		
		results:         make([]AttackResult, 0),
	}
	
	orchestrator.state.Status = "Ready"
	
	return orchestrator, nil
}

// Start launches the attack orchestration with all three attack paths
func (o *AttackOrchestrator) Start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	
	// Check if already running
	if o.state.Status == "Running" {
		return fmt.Errorf("orchestrator already running")
	}
	
	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	o.cancelFunc = cancel
	o.startTime = time.Now()
	o.state.Status = "Running"
	
	o.logger.Info("Attack orchestration started")
	
	// Initialize parallel attack vectors
	o.wg.Add(3)
	
	go o.runPhishingAttackVector(ctx)
	go o.runRCEAttackVector(ctx)
	go o.runNTLMRelayVector(ctx)
	
	o.logger.Info("All three attack vectors launched in parallel")
	
	return nil
}

// Stop gracefully terminates all attack vectors
func (o *AttackOrchestrator) Stop(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	
	if o.cancelFunc != nil {
		o.cancelFunc()
	}
	
	o.logger.Info("Stopping attack orchestration...")
	
	// Wait for all goroutines to finish
	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		o.state.Status = "Stopped"
		o.logger.Info("All attack vectors stopped successfully")
		return nil
	case <-ctx.Done():
		o.state.Status = "ForceStopped"
		o.logger.Warn("Attack orchestration force stopped")
		return ctx.Err()
	case <-time.After(5 * time.Second):
		o.state.Status = "TimeoutStopped"
		o.logger.Error("Attack orchestration timeout during stop")
		return fmt.Errorf("stop timeout")
	}
}

// GetState returns current orchestrator state
func (o *AttackOrchestrator) GetState() OrchestratorState {
	o.mu.RLock()
	defer o.mu.RUnlock()
	
	// Calculate elapsed time
	elapsed := time.Since(o.startTime)
	
	return OrchestratorState{
		Status:            o.state.Status,
		ActivePaths:       o.state.ActivePaths,
		CompletedPaths:    o.state.CompletedPaths,
		FailedPaths:       o.state.FailedPaths,
		SucceededPaths:    o.state.SucceededPaths,
		TotalExploits:     o.state.TotalExploits,
		SuccessfulExploits: o.state.SuccessfulExploits,
		AverageReward:     o.state.AverageReward,
		FinalAchieved:     o.state.FinalAchieved,
		ElapsedTime:       elapsed,
	}
}

// GetResults returns all attack results collected
func (o *AttackOrchestrator) GetResults() []AttackResult {
	o.mu.RLock()
	defer o.mu.RUnlock()
	
	// Return copy of results
	resultsCopy := make([]AttackResult, len(o.results))
	copy(resultsCopy, o.results)
	
	return resultsCopy
}

// ============================================================================
// PARALLEL ATTACK VECTORS
// ============================================================================

// runPhishingAttackVector executes phishing-based initial access attacks
func (o *AttackOrchestrator) runPhishingAttackVector(ctx context.Context) {
	defer o.wg.Done()
	
	o.logger.Info("Starting Phishing attack vector...")
	
	for {
		select {
		case <-ctx.Done():
			o.logger.Info("Phishing vector stopped by context cancellation")
			return
		case attempt, more := <-o.phishingChan:
			if !more {
				o.logger.Info("Phishing channel closed")
				return
			}
			
			result := o.executePhishingAttack(ctx, attempt)
			o.processResult(ctx, result)
			
			// Update Q-value based on feedback
			o.updateQValue(attempt.State, attempt.Action, result.Success, result.Metrics)
		}
	}
}

// runRCEAttackVector executes SharePoint/Office RCE exploitation
func (o *AttackOrchestrator) runRCEAttackVector(ctx context.Context) {
	defer o.wg.Done()
	
	o.logger.Info("Starting RCE attack vector...")
	
	for {
		select {
		case <-ctx.Done():
			o.logger.Info("RCE vector stopped by context cancellation")
			return
		case attempt, more := <-o.rceChan:
			if !more {
				o.logger.Info("RCE channel closed")
				return
			}
			
			result := o.executeRCEAttack(ctx, attempt)
			o.processResult(ctx, result)
			
			o.updateQValue(attempt.State, attempt.Action, result.Success, result.Metrics)
		}
	}
}

// runNTLMRelayVector executes NTLM relay attacks
func (o *AttackOrchestrator) runNTLMRelayVector(ctx context.Context) {
	defer o.wg.Done()
	
	o.logger.Info("Starting NTLM Relay attack vector...")
	
	for {
		select {
		case <-ctx.Done():
			o.logger.Info("NTLM Relay vector stopped by context cancellation")
			return
		case attempt, more := <-o.ntlmChan:
			if !more {
				o.logger.Info("NTLM channel closed")
				return
			}
			
			result := o.executeNTLMRelayAttack(ctx, attempt)
			o.processResult(ctx, result)
			
			o.updateQValue(attempt.State, attempt.Action, result.Success, result.Metrics)
		}
	}
}

// ============================================================================
// ATTACK EXECUTION FUNCTIONS
// ============================================================================

// executePhishingAttack performs phishing-based initial access
func (o *AttackOrchestrator) executePhishingAttack(ctx context.Context, attempt *AttackAttempt) AttackResult {
	startTime := time.Now()
	
	result := AttackResult{
		PathID:      attempt.PathID,
		AttackType:  "Phishing",
		StartTime:   startTime,
		Timestamp:   time.Now(),
		Success:     false,
		Metrics:     &AttackMetrics{},
	}
	
	o.logger.WithFields(logrus.Fields{
		"path_id":  attempt.PathID,
		"target":  attempt.Target,
	}).Info("Executing phishing attack")
	
	// Simulate phishing campaign execution
	// In production, this would integrate with real phishing frameworks
	
	simulatedDelay := 2000 + (int64(attempt.TargetHash) % 3000) // 2-5 seconds simulation
	time.Sleep(time.Duration(simulatedDelay) * time.Millisecond)
	
	// Calculate success probability based on Q-values
	successProb := o.qAgent.SelectAction(attempt.State, attempt.Action)
	isSuccess := simulatedRandom(successProb)
	
	endTime := time.Now()
	
	result.EndTime = endTime
	result.Success = isSuccess
	result.EvidenceChain = o.generatePhishingEvidence(attempt, isSuccess)
	
	// Calculate metrics
	result.Metrics.SuccessRate = successProb
	result.Metrics.StealthScore = 0.7
	result.Metrics.DetectionCount = simulatedIntRange(0, 3)
	result.Metrics.ElapsedTimeMS = endTime.Sub(startTime).Milliseconds()
	result.Metrics.ResourceUsage = 0.6 + simulatedIntRange(0, 3)/10.0
	
	// Collect credentials if successful
	if isSuccess {
		result.Credentials = []CredentialDump{
			{
				Type:        "UserCredentials",
				Username:    fmt.Sprintf("victim@%s", attempt.Domain),
				Password:    simulateCredentialHash(),
				Source:      "PhishingLanding",
				Timestamp:   endTime,
				Verified:    true,
			},
		}
		
		// Progress toward domain admin via lateral movement
		o.progressTowardsDAC(ctx, attempt.PathID)
	}
	
	o.logger.WithFields(logrus.Fields{
		"path_id":  attempt.PathID,
		"success":  isSuccess,
		"reward":   result.RewardScore,
		"metrics":  result.Metrics,
	}).Info("Phishing attack completed")
	
	return result
}

// executeRCEAttack performs RCE exploitation
func (o *AttackOrchestrator) executeRCEAttack(ctx context.Context, attempt *AttackAttempt) AttackResult {
	startTime := time.Now()
	
	result := AttackResult{
		PathID:      attempt.PathID,
		AttackType:  "RCE",
		StartTime:   startTime,
		Timestamp:   time.Now(),
		Success:     false,
		Metrics:     &AttackMetrics{},
	}
	
	o.logger.WithFields(logrus.Fields{
		"path_id": attempt.PathID,
		"target":  attempt.Target,
		"exploit": attempt.ExploitType,
	}).Info("Executing RCE attack")
	
	// Simulate RCE exploit execution
	simulatedDelay := 1500 + (int64(attempt.TargetHash) % 2000)
	time.Sleep(time.Duration(simulatedDelay) * time.Millisecond)
	
	// Calculate success probability
	successProb := o.qAgent.SelectAction(attempt.State, attempt.Action)
	isSuccess := simulatedRandom(successProb)
	
	endTime := time.Now()
	
	result.EndTime = endTime
	result.Success = isSuccess
	result.EvidenceChain = o.generateRCEEvidence(attempt, isSuccess)
	
	// Calculate metrics
	result.Metrics.SuccessRate = successProb
	result.Metrics.StealthScore = 0.8
	result.Metrics.DetectionCount = simulatedIntRange(0, 2)
	result.Metrics.ElapsedTimeMS = endTime.Sub(startTime).Milliseconds()
	result.Metrics.ResourceUsage = 0.5 + simulatedIntRange(0, 4)/10.0
	
	if isSuccess {
		result.Credentials = []CredentialDump{
			{
				Type:        "SystemShell",
				Username:    "SYSTEM",
				Password:    "NTLMv2_Hash_Simulated",
				Source:      "RCE_Execution",
				Timestamp:   endTime,
				Verified:    true,
			},
		}
		
		o.progressTowardsDAC(ctx, attempt.PathID)
	}
	
	o.logger.WithFields(logrus.Fields{
		"path_id":  attempt.PathID,
		"success":  isSuccess,
	}).Info("RCE attack completed")
	
	return result
}

// executeNTLMRelayAttack performs NTLM relay attacks
func (o *AttackOrchestrator) executeNTLMRelayAttack(ctx context.Context, attempt *AttackAttempt) AttackResult {
	startTime := time.Now()
	
	result := AttackResult{
		PathID:      attempt.PathID,
		AttackType:  "NTLM_Relay",
		StartTime:   startTime,
		Timestamp:   time.Now(),
		Success:     false,
		Metrics:     &AttackMetrics{},
	}
	
	o.logger.WithFields(logrus.Fields{
		"path_id": attempt.PathID,
		"target":  attempt.Target,
	}).Info("Executing NTLM relay attack")
	
	// Simulate NTLM relay
	simulatedDelay := 800 + (int64(attempt.TargetHash) % 1200)
	time.Sleep(time.Duration(simulatedDelay) * time.Millisecond)
	
	successProb := o.qAgent.SelectAction(attempt.State, attempt.Action)
	isSuccess := simulatedRandom(successProb)
	
	endTime := time.Now()
	
	result.EndTime = endTime
	result.Success = isSuccess
	result.EvidenceChain = o.generateNTLMLEvidence(attempt, isSuccess)
	
	result.Metrics.SuccessRate = successProb
	result.Metrics.StealthScore = 0.6
	result.Metrics.DetectionCount = simulatedIntRange(1, 5) // NTLM relay is noisy
	result.Metrics.ElapsedTimeMS = endTime.Sub(startTime).Milliseconds()
	result.Metrics.ResourceUsage = 0.4 + simulatedIntRange(0, 3)/10.0
	
	if isSuccess {
		result.Credentials = []CredentialDump{
			{
				Type:        "NTLM_Hash",
				Username:    attempt.Target,
				Password:    "NTLMv2_Response_Captured",
				Source:      "NTLMChallengeResponse",
				Timestamp:   endTime,
				Verified:    true,
			},
		}
		
		o.progressTowardsDAC(ctx, attempt.PathID)
	}
	
	o.logger.WithFields(logrus.Fields{
		"path_id":  attempt.PathID,
		"success":  isSuccess,
	}).Info("NTLM relay attack completed")
	
	return result
}

// ============================================================================
// RESULT PROCESSING AND Q-VALUE UPDATE
// ============================================================================

// processResult handles attack completion and updates global state
func (o *AttackOrchestrator) processResult(ctx context.Context, result AttackResult) {
	o.mu.Lock()
	defer o.mu.Unlock()
	
	o.state.TotalExploits++
	
	if result.Success {
		o.state.SuccessfulExploits++
		o.state.SucceededPaths++
		o.successTracker[result.PathID] = true
		
		o.logger.WithFields(logrus.Fields{
			"path_id": result.PathID,
			"type":    result.AttackType,
			"reward":  result.RewardScore,
		}).Info("Attack succeeded")
		
		// Early termination check
		if o.config.EarlyTermination && result.HasDAC() {
			o.logger.Info("Domain Admin achieved! Terminating other vectors.")
			o.state.FinalAchieved = "DomainAdmin"
		}
	} else {
		o.state.FailedPaths++
	}
	
	o.state.CompletedPaths++
	o.state.ActivePaths--
	
	o.results = append(o.results, result)
	
	// Recalculate average reward
	totalReward := 0.0
	for _, r := range o.results {
		totalReward += r.RewardScore
	}
	o.state.AverageReward = totalReward / float64(len(o.results))
	
	// Record in evidence ledger
	o.evidenceCollector.Record(result)
}

// ============================================================================
// SIMULATION UTILITIES
// ============================================================================

func simulatedRandom(probability float64) bool {
	// Simplified random generation for simulation
	return rand.Float64() < probability
}

func simulatedIntRange(min, max int) int {
	return min + int(rand.Float64()*float64(max-min))
}

func simulateCredentialHash() string {
	return fmt.Sprintf("NTLMv2-%x", rand.Int63())
}

// ============================================================================
// CALCULATION METHODS
// ============================================================================

// calculateReward implements multi-component reward function (Patent #13)
func (o *AttackOrchestrator) calculateReward(state, action string, success bool, metrics *AttackMetrics) float64 {
	// Reward = α*SuccessRate + β*StealthScore - γ*DetectionProb + δ*TimeBonus + ε*ResourceUtilization
	successComponent := o.config.RewardWeights.SuccessWeight
	if !success {
		successComponent = 0
	}
	
	stealthComponent := metrics.StealthScore * o.config.RewardWeights.StealthWeight
	detectionPenalty := float64(metrics.DetectionCount) * 0.1 * o.config.RewardWeights.DetectionPenalty
	
	timeBonus := 0.0
	if metrics.ElapsedTimeMS > 0 {
		// Faster attacks get bonus (inverse of elapsed time normalized)
		normalizedTime := 1.0 / (1.0 + float64(metrics.ElapsedTimeMS)/10000.0)
		timeBonus = normalizedTime * o.config.RewardWeights.TimeEfficiencyBonus
	}
	
	resourceComponent := metrics.ResourceUsage * o.config.RewardWeights.ResourceUtilization
	
	reward := successComponent + stealthComponent - detectionPenalty + timeBonus + resourceComponent
	
	return reward
}

func (o *AttackOrchestrator) updateQValue(state string, action string, success bool, metrics *AttackMetrics) {
	// Calculate immediate reward
	immediateReward := o.calculateReward(state, action, success, metrics)
	
	// Get max Q-value for next state (Bellman equation)
	maxNextQ := o.qAgent.GetMaxQValue(state)
	
	// Q-learning update: Q(s,a) <- Q(s,a) + η[r + γ*max_a'Q(s',a') - Q(s,a)]
	currentQ := o.qAgent.GetQValue(state, action)
	
	newQ := currentQ + o.config.QLearningParams.LearningRate * 
		(immediateReward + o.config.QLearningParams.DiscountFactor*maxNextQ - currentQ)
	
	o.qAgent.UpdateQValue(state, action, newQ)
	
	// Decay exploration rate
	if o.qAgent.ExplorationRate > o.config.QLearningParams.MinExplorationRate {
		o.qAgent.ExplorationRate *= o.config.QLearningParams.ExplorationDecay
	}
	
	o.logger.WithFields(logrus.Fields{
		"state":   state,
		"action":  action,
		"success": success,
		"reward":  immediateReward,
		"old_q":   currentQ,
		"new_q":   newQ,
	}).Debug("Q-value updated")
}

// ============================================================================
// PATH SUBMISSION HELPERS
// ============================================================================

// SubmitPhishingPath submits a new phishing attack path for execution
func (o *AttackOrchestrator) SubmitPhishingPath(ctx context.Context, pathID, target, domain string) error {
	attempt := &AttackAttempt{
		PathID:     pathID,
		Target:     target,
		Domain:     domain,
		Action:     "launch_phishing",
		State:      o.pathManager.GetCurrentState(),
		ExploitType: "SpearPhishing",
		TargetHash: hashString(pathID),
	}
	
	o.pathManager.MarkSubmitted(pathID)
	
	select {
	case o.phishingChan <- attempt:
		o.state.ActivePaths++
		o.logger.WithFields(logrus.Fields{
			"path_id": pathID,
			"target":  target,
		}).Info("Phishing path submitted")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("phishing channel full, retry later")
	}
}

// SubmitRCEPath submits RCE attack path
func (o *AttackOrchestrator) SubmitRCEPath(ctx context.Context, pathID, target, exploitType string) error {
	attempt := &AttackAttempt{
		PathID:     pathID,
		Target:     target,
		Action:     "execute_rce",
		State:      o.pathManager.GetCurrentState(),
		ExploitType: exploitType,
		TargetHash: hashString(pathID),
	}
	
	o.pathManager.MarkSubmitted(pathID)
	
	select {
	case o.rceChan <- attempt:
		o.state.ActivePaths++
		o.logger.WithFields(logrus.Fields{
			"path_id": pathID,
			"exploit": exploitType,
		}).Info("RCE path submitted")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("RCE channel full, retry later")
	}
}

// SubmitNTLMPath submits NTLM relay attack path
func (o *AttackOrchestrator) SubmitNTLMPath(ctx context.Context, pathID, target string) error {
	attempt := &AttackAttempt{
		PathID:     pathID,
		Target:     target,
		Action:     "relay_ntlm",
		State:      o.pathManager.GetCurrentState(),
		TargetHash: hashString(pathID),
	}
	
	o.pathManager.MarkSubmitted(pathID)
	
	select {
	case o.ntlmChan <- attempt:
		o.state.ActivePaths++
		o.logger.WithFields(logrus.Fields{
			"path_id": pathID,
			"target":  target,
		}).Info("NTLM relay path submitted")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("NTLM channel full, retry later")
	}
}

// ============================================================================
// PROGRESS TOWARDS DOMAIN ADMIN
// ============================================================================

// progressTowardsDAC advances the attack chain toward DAC
func (o *AttackOrchestrator) progressTowardsDAC(ctx context.Context, successfulPathID string) {
	// In a real implementation, this would trigger lateral movement
	// For simulation, we mark progress
	
	o.logger.WithFields(logrus.Fields{
		"path_id": successfulPathID,
	}).Info("Advancing towards Domain Admin compromise")
	
	// Track achievement of critical milestones
	if !o.checkDACAchieved() {
		o.triggerLateralMovement(ctx, successfulPathID)
	}
}

// triggerLateralMovement simulates lateral movement after initial compromise
func (o *AttackOrchestrator) triggerLateralMovement(ctx context.Context, fromPathID string) {
	o.logger.Info("Initiating lateral movement protocol...")
	
	// This would integrate with actual lateral movement tools
	// For now, mark as progressed
	time.Sleep(500 * time.Millisecond)
	
	o.logger.Info("Lateral movement completed successfully")
}

// checkDACAchieved checks if domain admin has been achieved
func (o *AttackOrchestrator) checkDACAchieved() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	
	return o.state.FinalAchieved == "DomainAdmin"
}

// ============================================================================
// EVIDENCE GENERATION
// ============================================================================

func (o *AttackOrchestrator) generatePhishingEvidence(attempt *AttackAttempt, success bool) []EvidenceEntry {
	entries := make([]EvidenceEntry, 0)
	
	timestamp := time.Now()
	
	// Email sending event
	entries = append(entries, EvidenceEntry{
		Timestamp:   timestamp,
		EventType:   "EmailSent",
		Description: fmt.Sprintf("Phishing email sent to %s", attempt.Target),
		Details: map[string]interface{}{
			"subject":  "Urgent: Account Verification Required",
			"link":     "http://fake-login.example.com",
			"success":  success,
		},
	})
	
	if success {
		// Credential harvest
		entries = append(entries, EvidenceEntry{
			Timestamp:   timestamp.Add(1 * time.Second),
			EventType:   "CredentialHarvest",
			Description: "User credentials harvested from fake login page",
			Details: map[string]interface{}{
				"username": fmt.Sprintf("%s@%s", attempt.Target, attempt.Domain),
			},
		})
		
		// Session establishment
		entries = append(entries, EvidenceEntry{
			Timestamp:   timestamp.Add(2 * time.Second),
			EventType:   "SessionEstablished",
			Description: "Authenticated session established",
		})
	}
	
	return entries
}

func (o *AttackOrchestrator) generateRCEEvidence(attempt *AttackAttempt, success bool) []EvidenceEntry {
	entries := make([]EvidenceEntry, 0)
	
	timestamp := time.Now()
	
	entries = append(entries, EvidenceEntry{
		Timestamp:   timestamp,
		EventType:   "VulnScanned",
		Description: fmt.Sprintf("Scanning target %s for RCE vulnerabilities", attempt.Target),
	})
	
	if success {
		entries = append(entries, EvidenceEntry{
			Timestamp:   timestamp.Add(500*time.Millisecond),
			EventType:   "VulnerabilityDetected",
			Description: fmt.Sprintf("Found vulnerable service: %s", attempt.ExploitType),
			Details: map[string]interface{}{
				"vulnerability": attempt.ExploitType,
				"cve":           "CVE-SIMULATED-2024-XXXX",
			},
		})
		
		entries = append(entries, EvidenceEntry{
			Timestamp:   timestamp.Add(1000*time.Millisecond),
			EventType:   "ExploitExecuted",
			Description: "Remote code execution achieved",
			Details: map[string]interface{}{
				"shell":     "cmd.exe",
				"user":      "SYSTEM",
				"working_dir": "/tmp",
			},
		})
		
		entries = append(entries, EvidenceEntry{
			Timestamp:   timestamp.Add(1500*time.Millisecond),
			EventType:   "CommandExecuted",
			Description: "Arbitrary command executed with elevated privileges",
		})
	}
	
	return entries
}

func (o *AttackOrchestrator) generateNTLMLEvidence(attempt *AttackAttempt, success bool) []EvidenceEntry {
	entries := make([]EvidenceEntry, 0)
	
	timestamp := time.Now()
	
	entries = append(entries, EvidenceEntry{
		Timestamp:   timestamp,
		EventType:   "NTLMCapture",
		Description: "Capturing NTLM challenge-response",
	})
	
	if success {
		entries = append(entries, EvidenceEntry{
			Timestamp:   timestamp.Add(500*time.Millisecond),
			EventType:   "NTLMRelayAttempt",
			Description: fmt.Sprintf("Attempting NTLM relay to %s", attempt.Target),
		})
		
		entries = append(entries, EvidenceEntry{
			Timestamp:   timestamp.Add(1000*time.Millisecond),
			EventType:   "AuthenticationBypass",
			Description: "NTLM relay bypassed authentication",
		})
		
		entries = append(entries, EvidenceEntry{
			Timestamp:   timestamp.Add(1500*time.Millisecond),
			EventType:   "PrivilegeEscalation",
			Description: "Gained elevated privileges via relay",
		})
	}
	
	return entries
}

// ============================================================================
// HELPER TYPES AND STRUCTURES
// ============================================================================

// AttackAttempt represents a single attack attempt
type AttackAttempt struct {
	PathID      string
	Target      string
	Domain      string
	ExploitType string
	Action      string
	State       string
	TargetHash  uint64
}

// CredentialDump captured credential material
type CredentialDump struct {
	Type        string    `json:"type"`
	Username    string    `json:"username"`
	Password    string    `json:"password"`
	Source      string    `json:"source"`
	Timestamp   time.Time `json:"timestamp"`
	Verified    bool      `json:"verified"`
}

// HasDAC checks if result includes domain admin compromise
func (a *AttackResult) HasDAC() bool {
	for _, cred := range a.Credentials {
		if cred.Username == "SYSTEM" || cred.Type == "DomainAdmin" {
			return true
		}
	}
	return false
}

// EvidenceEntry represents a single piece of attack evidence
type EvidenceEntry struct {
	Timestamp   time.Time            `json:"timestamp"`
	EventType   string               `json:"event_type"`
	Description string               `json:"description"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func hashString(s string) uint64 {
	h := sha256.New()
	h.Write([]byte(s))
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8])
}

func simulatedRandom(probability float64) bool {
	// Simplified random generation for simulation
	return rand.Float64() < probability
}

func simulatedIntRange(min, max int) int {
	return min + int(rand.Float64()*float64(max-min))
}

func simulateCredentialHash() string {
	return fmt.Sprintf("NTLMv2-%x", rand.Int63())
}

// ============================================================================
// TEST EXPORTED METHODS (FOR UNIT TESTS)
// ============================================================================

// CalculateRewardForTest exposes calculateReward for testing
func (o *AttackOrchestrator) CalculateRewardForTest(state, action string, success bool, metrics *AttackMetrics) float64 {
	return o.calculateReward(state, action, success, metrics)
}

// Config returns config for testing
func (o *AttackOrchestrator) Config() *OrchestratorConfig {
	return o.config
}
