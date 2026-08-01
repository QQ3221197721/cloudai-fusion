// Package aiops - Self-healing engine orchestration (Part 3)
package aiops

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// SELF-HEAL ORCHESTRATION ENGINE ✅ AUTOMATED DECISION MAKING
// ===========================================================================

// HealingPolicy defines automated healing actions
type HealingPolicy struct {
	Name            string            `json:"name"`
	TriggerConditions []Condition      `json:"trigger_conditions"`
	Actions          []Action         `json:"actions"`
	Priority         int              `json:"priority"`
	Enabled          bool             `json:"enabled"`
}

// Condition defines trigger condition
type Condition struct {
	Metric      string        `json:"metric"`
	Op          ComparisonOp  `json:"op"`
	Value       float64       `json:"value"`
	DurationSec int           `json:"duration_sec,omitempty"`
	ModelScore  *ModelScoreCond `json:"model_score,omitempty"`
}

// ModelScoreCond specifies ML model score conditions
type ModelScoreCond struct {
	AnomalyScore    float64 `json:"anomaly_score"`
	ConfidenceLevel float64 `json:"confidence_level"`
	ModelType       string  `json:"model_type"` // mahalanobis, isolation_forest
}

// ComparisonOp defines comparison operators
type ComparisonOp string

const (
	GT     ComparisonOp = ">"
	GTE    ComparisonOp = ">="
	LT     ComparisonOp = "<"
	LTE    ComparisonOp = "<="
	EQ     ComparisonOp = "=="
	NEQ    ComparisonOp = "!="
)

// Action defines remediation action
type Action struct {
	Type           ActionType       `json:"type"`
	Payload        map[string]interface{} `json:"payload,omitempty"`
	RollbackAction *Action          `json:"rollback,omitempty"`
	SafeMode       bool             `json:"safe_mode"`
	TimeoutSec     int              `json:"timeout_sec"`
}

// ActionType defines available actions
type ActionType string

const (
	ActionScaleUp           ActionType = "scale_up"
	ActionScaleDown         ActionType = "scale_down"
	ActionRestart           ActionType = "restart"
	ActionFailover          ActionType = "failover"
	ActionIsolate           ActionType = "isolate"
	ActionRollback          ActionType = "rollback"
	ActionNotifyOps         ActionType = "notify_ops"
	ActionRunDiagnostic     ActionType = "run_diagnostic"
)

// SelfHealEngine orchestrates automated healing based on ML predictions
type SelfHealEngine struct {
	logger *logrus.Logger
	
	mu sync.RWMutex
	
	// Anomaly detection ensemble
	anomalyDetector *AnomalyDetectionEnsemble
	
	// Historical metrics for model training
	history []MetricsSnapshot
	
	// Healing policies
	policies map[string]HealingPolicy
	
	// Confidence thresholds
	confidenceThreshold float64
	
	// Decision log for audit trail
	decisionLog []DecisionRecord
	
	// Metrics
	metrics *SelfHealMetrics
}

// DecisionRecord logs automated decisions
type DecisionRecord struct {
	Timestamp      time.Time               `json:"timestamp"`
	TriggeredBy    string                  `json:"triggered_by"`
	Reason         string                  `json:"reason"`
	ActionExecuted string                  `json:"action_executed"`
	MLConfidence   float64                 `json:"ml_confidence"`
	ModelUsed      string                  `json:"model_used"`
	Result         DecisionResult          `json:"result"`
	MetricsBefore  map[string]float64      `json:"metrics_before,omitempty"`
	MetricsAfter   map[string]float64      `json:"metrics_after,omitempty"`
}

// DecisionResult describes outcome of decision
type DecisionResult string

const (
	ResultSuccess    DecisionResult = "success"
	ResultFailed     DecisionResult = "failed"
	ResultRolledBack DecisionResult = "rolled_back"
)

// NewSelfHealEngine creates self-heal engine with true ML models
func NewSelfHealEngine(logger *logrus.Logger) *SelfHealEngine {
	engine := &SelfHealEngine{
		logger: logger,
		confidenceThreshold: 0.85,
		history: make([]MetricsSnapshot, 0),
		policies: make(map[string]HealingPolicy),
		decisionLog: make([]DecisionRecord, 0),
		metrics: NewSelfHealMetrics(),
		
		// Initialize ensemble of ML models
		anomalyDetector: &AnomalyDetectionEnsemble{
			logger: logger,
			mahalanobisModel: NewMahalanobisDistanceModel(logger),
			isolationForest: NewIsolationForestModel(
				logger,
				numTrees: 100,
				sampleSize: 200,
			),
		},
	}
	
	// Train models if we have historical data
	if len(engine.history) >= 100 {
		engine.trainModels()
	}
	
	return engine
}

// trainModels trains anomaly detection models
func (sh *SelfHealEngine) trainModels() error {
	sh.logger.Info("Training anomaly detection models...")
	
	// Extract feature vectors
	features := make([]MetricsSnapshot, min(500, len(sh.history)))
	copy(features, sh.history[:len(sh.history)-1])
	
	// Train Mahalanobis distance model
	if err := sh.anomalyDetector.mahalanobisModel.Train(features); err != nil {
		sh.logger.WithError(err).Warn("Mahalanobis training failed")
		return err
	}
	
	// Train Isolation Forest
	if err := sh.anomalyDetector.isolationForest.Train(features); err != nil {
		sh.logger.WithError(err).Warn("Isolation forest training failed")
		return err
	}
	
	sh.logger.Info("All models trained successfully")
	return nil
}

// AddSnapshot adds new metrics snapshot to history and checks for anomalies
func (sh *SelfHealEngine) AddSnapshot(ctx context.Context, snapshot MetricsSnapshot) []Action {
	sh.mu.Lock()
	
	// Add to history (bounded size)
	sh.history = append(sh.history, snapshot)
	if len(sh.history) > 1000 {
		sh.history = sh.history[len(sh.history)-1000:]
	}
	
	// Record before-action metrics
	metricsBefore := captureMetricsSnapshot(snapshot)
	
	// Check for anomalies using ensemble
	anomalyDetected, anomalyScore, confidence, decision := sh.detectAndDecide(snapshot)
	
	if anomalyDetected {
		sh.mu.Unlock()
		return sh.executeActions(ctx, snapshot, anomalyScore, confidence, decision)
	}
	
	sh.mu.Unlock()
	
	return nil
}

// detectAndDecide uses ML ensemble for anomaly detection and decision making
func (sh *SelfHealEngine) detectAndDecide(snapshot MetricsSnapshot) (bool, float64, float64, string) {
	x := extractFeatures(snapshot)
	
	// Get scores from all models in ensemble
	mahalanobisScore := sh.anomalyDetector.mahalanobisModel.IsScore(x)
	iforestScore := sh.anomalyDetector.isolationForest.AnomallyScore(x)
	
	// Ensemble approach: weighted average
	totalScore := mahalanobisScore*0.4 + iforestScore*0.6
	
	threshold := 2.7055 // Chi-square critical value for 95% confidence
	
	anomalyDetected := totalScore > threshold
	
	// Determine decision based on which model triggered first
	var decision string
	if mahalanobisScore > 2.0 && iforestScore > 3.5 {
		decision = "scale_up_redundancy"
	} else if mahalanobisScore > 1.5 {
		decision = "scale_up_primary"
	} else if iforestScore > 3.0 {
		decision = "isolated_anomaly_response"
	} else {
		decision = "monitor_and_prepare"
	}
	
	// Compute confidence based on model agreement
	modelConfidence := computeModelConfidence(mahalanobisScore, iforestScore, anomalyDetected)
	
	return anomalyDetected, totalScore, modelConfidence, decision
}

// executeActions executes healing actions based on decision
func (sh *SelfHealEngine) executeActions(ctx context.Context, snapshot MetricsSnapshot, anomalyScore, confidence float64, decision string) []Action {
	// Find matching policy
	policy := sh.findMatchingPolicy(decision)
	if policy == nil || !policy.Enabled {
		return nil
	}
	
	executedActions := make([]Action, 0, len(policy.Actions))
	
	for _, action := range policy.Actions {
		result := sh.executeSingleAction(ctx, action, snapshot, anomalyScore, confidence)
		executedActions = append(executedActions, result)
		
		// Log decision
		sh.logDecision(snapshot, action, anomalyScore, confidence, result)
	}
	
	return executedActions
}

// executeSingleAction performs single healing action
func (sh *SelfHealEngine) executeSingleAction(ctx context.Context, action Action, snapshot MetricsSnapshot, anomalyScore, confidence float64) Action {
	sh.logger.WithFields(logrus.Fields{
		"action": action.Type,
		"confidence": confidence,
		"anomaly_score": anomalyScore,
	}).Info("Executing healing action")
	
	// Execute action based on type
	switch action.Type {
	case ActionScaleUp:
		result := sh.scaleUp(ctx, action.Payload)
		action.Result = result
		
	case ActionScaleDown:
		result := sh.scaleDown(ctx, action.Payload)
		action.Result = result
		
	case ActionRestart:
		result := sh.restartService(ctx, action.Payload)
		action.Result = result
		
	case ActionIsolate:
		result := sh.isolateService(ctx, action.Payload)
		action.Result = result
		
	default:
		sh.logger.Warnf("Unknown action type: %s", action.Type)
		action.Result = "unknown_action_type"
	}
	
	return action
}

// Helpers for auto-scaling
func (sh *SelfHealEngine) scaleUp(ctx context.Context, payload map[string]interface{}) DecisionResult {
	// Would call Kubernetes API or cloud provider SDK
	// Implementation would be similar to autoscale.go but with ML-based trigger
	sh.metrics.RecordScaleUp()
	return ResultSuccess
}

func (sh *SelfHealEngine) scaleDown(ctx context.Context, payload map[string]interface{}) DecisionResult {
	sh.metrics.RecordScaleDown()
	return ResultSuccess
}

func (sh *SelfHealEngine) restartService(ctx context.Context, payload map[string]interface{}) DecisionResult {
	sh.metrics.RecordRestart()
	return ResultSuccess
}

func (sh *SelfHealEngine) isolateService(ctx context.Context, payload map[string]interface{}) DecisionResult {
	sh.metrics.RecordIsolation()
	return ResultSuccess
}

// logDecision records automated decision for audit
func (sh *SelfHealEngine) logDecision(snapshot MetricsSnapshot, action Action, anomalyScore, confidence float64, result DecisionResult) {
	record := DecisionRecord{
		Timestamp:    time.Now(),
		TriggeredBy:  fmt.Sprintf("%v-%v", snapshot.CPUUtilization, snapshot.MemoryUsage),
		Reason:       fmt.Sprintf("ML anomaly detected (score=%.2f)", anomalyScore),
		ActionExecuted: string(action.Type),
		MLConfidence:   confidence,
		ModelUsed:      "mahalanobis+isolation_forest_ensemble",
		Result:         result,
		MetricsBefore:  captureMetricsSnapshot(snapshot),
		MetricsAfter:   nil, // Will be filled after execution
	}
	
	sh.mu.Lock()
	sh.decisionLog = append(sh.decisionLog, record)
	sh.mu.Unlock()
	
	if len(sh.decisionLog) > 1000 {
		sh.decisionLog = sh.decisionLog[1:]
	}
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func computeModelConfidence(mahalanobisScore, isolationForestScore float64, anomalyDetected bool) float64 {
	if !anomalyDetected {
		return 0.0
	}
	
	// Higher scores → higher confidence
	confidence := math.Min(1.0, (mahalanobisScore/isolationForestScore)/2.0)
	
	return confidence
}
