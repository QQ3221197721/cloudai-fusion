// Package blueteam provides AI-enhanced threat detection and automated response
// capabilities for OBCE3 blue team defense operations.
package blueteam

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/redteam_simulation"
)

// AIThreatClassifier uses pattern recognition to classify security threats
type AIThreatClassifier struct {
	ruleBasedPatterns []*ThreatPattern
	machineLearningModel *SimpleMLClassifier
	anomalyDetector    *BehaviorAnomalyDetector
	contextualAnalyzer *ContextualThreatAnalyzer
	knowledgeBase      *ThreatKnowledgeBase
}

// ThreatPattern represents a recognizable attack signature
type ThreatPattern struct {
	ID            string
	Name          string
	Severity      redteam_simulation.FindingSeverity
	TechniqueID   string // MITRE ATT&CK ID
	Patterns      []*regexp.Regexp
	DetectionRule string
	Mitigation    string
	SimilarityThreshold float64
}

// SimpleMLClassifier implements lightweight ML for threat classification
type SimpleMLClassifier struct {
	featureWeights map[string]float64
	classificationRules []ClassificationRule
	learningRate float64
	modelVersion string
	lastTrainingDate time.Time
	history        *TrainingHistory
}

// ClassificationRule defines ML decision rules
type ClassificationRule struct {
	Name         string
	Conditions   []Condition
	Priority     int
	Classification string
	Confidence   float64
}

// Condition defines a single ML condition
type Condition struct {
	Feature     string
	Operator    string // ">", "<", "==", ">=", "<=", "contains"
	Value       interface{}
}

// BehaviorAnomalyDetector identifies anomalous behaviors
type BehaviorAnomalyDetector struct {
	baselineMetrics *BehavioralBaseline
	thresholdConfig AnomalyThresholdConfig
	historicalData  []ObservationWindow
	alertCoalesceTime time.Duration
}

// BehavioralBaseline defines normal behavior metrics
type BehavioralBaseline struct {
	NormalVulnerabilityCount int
	AverageRiskScore float64
	CommonAttackVectors []string
	ExpectedDetectionRate float64
	TypicalResponseTimeMS float64
}

// AnomalyThresholdConfig defines alert thresholds
type AnomalyThresholdConfig struct {
	VulnerabilityCountThreshold int
	RiskScoreThreshold float64
	DeviationStandardDeviations float64
	PatternUniquenessThreshold float64
}

// ObservationWindow represents time-bounded observation data
type ObservationWindow struct {
	StartTime    time.Time
	EndTime      time.Time
	Metrics      ObservationMetrics
	Context      map[string]interface{}
	AnomaliesDetected []string
}

// ObservationMetrics tracks behavioral metrics
type ObservationMetrics struct {
	RequestRatePerSecond float64
	ErrorRate float64
	AuthFailureRate float64
	ResourceUtilization float64
	LatencyP50ms float64
	LatencyP95ms float64
	LatencyP99ms float64
}

// ContextualThreatAnalyzer enhances threat assessment with context
type ContextualThreatAnalyzer struct {
	environmentContext map[string]interface{}
	correlationEngine  *CorrelationEngine
	riskScorer         *DynamicRiskScorer
	threatIntegrator   *ExternalThreatFeedIntegrator
}

// CorrelationEngine correlates related events
type CorrelationEngine struct {
	correlationWindows map[string]time.Duration
	eventGroupingRules []GroupingRule
	temporalAnalyzer  *TemporalEventAnalyzer
	sourceAnalyzer    *SourceCorrelationAnalyzer
}

// GroupingRule defines event grouping logic
type GroupingRule struct {
	Name         string
	Fields       []string
	TimeWindow   time.Duration
	MinEvents    int
	CorrelationType string // "sequential", "concurrent", "similar_pattern"
}

// TemporalEventAnalyzer analyzes event timing patterns
type TemporalEventAnalyzer struct {
	patternCache map[string][]time.Time
	detectionWindows []time.Duration
	spikeThreshold float64
}

// SourceCorrelationAnalyzer correlates by source
type SourceCorrelationAnalyzer struct {
	trustedSources []string
	untrustedSources []string
	ipReputationMap map[string]float64
}

// DynamicRiskScorer calculates dynamic risk scores
type DynamicRiskScorer struct {
	baseScores map[string]float64
	contextMultipliers map[string]float64
	timeDecayFactor float64
	severityAdjustments map[redteam_simulation.FindingSeverity]float64
}

// ExternalThreatFeedIntegrator fetches external threat intelligence
type ExternalThreatFeedIntegrator struct {
	feedURLs []string
	cacheDuration time.Duration
	updateInterval time.Duration
	lastUpdate time.Time
	enableRealtimeUpdates bool
}

// AutomatedResponseOrchestrator coordinates automated defensive responses
type AutomatedResponseOrchestrator struct {
	responseLibrary    []*DefenseResponse
	executionEngine    *ResponseExecutionEngine
	policyController   *ResponsePolicyController
	validationService  *ResponseValidator
	fallbackHandler    *ResponseFallbackHandler
}

// DefenseResponse defines a defensive action
type DefenseResponse struct {
	ID              string
	Name            string
	Type            ResponseType
	SeverityLevel   redteam_simulation.FindingSeverity
	Description     string
	Implementation  string
	TriggerConditions []string
	Effectiveness   float64
	RollbackPlan    string
	TestingRequired bool
}

// ResponseType defines response categories
type ResponseType string

const (
	ResponseIsolate    ResponseType = "ISOLATE"
	ResponseBlock      ResponseType = "BLOCK"
	ResponseMitigate   ResponseType = "MITIGATE"
	ResponseContain    ResponseType = "CONTAIN"
	ResponseRecover    ResponseType = "RECOVER"
	ResponseMonitor    ResponseType = "MONITOR"
)

// ResponseExecutionEngine executes defensive responses
type ResponseExecutionEngine struct {
	executionQueue []PendingResponse
	concurrencyLimit int
	timeoutPerResponse time.Duration
	hookManager      *ExecutionHookManager
	resultCollector  *ResponseResultCollector
}

// PendingResponse defines queued response execution
type PendingResponse struct {
	Response       *DefenseResponse
	Priority       int
	ExecuteAt      time.Time
	RetryCount     int
	MaxRetries     int
	Status         ExecutionStatus
	Context        map[string]interface{}
}

// ExecutionStatus defines execution state
type ExecutionStatus string

const (
	StatusPending ExecutionStatus = "PENDING"
	StatusExecuting ExecutionStatus = "EXECUTING"
	StatusCompleted ExecutionStatus = "COMPLETED"
	StatusFailed ExecutionStatus = "FAILED"
	StatusSkipped ExecutionStatus = "SKIPPED"
)

// ExecutionHookManager manages pre/post execution hooks
type ExecutionHookManager struct {
	preHooks   map[string][]HookFunc
	postHooks  map[string][]HookFunc
	globalHooks []GlobalHookFunc
}

// HookFunc defines hook function signature
type HookFunc func(context map[string]interface{}) (map[string]interface{}, error)

// GlobalHookFunc defines global hook signature
type GlobalHookFunc func(event ExecutionEvent) 

// ResponseResultCollector collects execution results
type ResponseResultCollector struct {
	resultsStore []*ExecutedResponse
	failuresTrack []*ResponseFailure
	successMetrics *PerformanceMetrics
}

// ExecutedResponse records successful execution
type ExecutedResponse struct {
	ResponseID string
	ExecutedAt time.Time
	DurationMs int64
	Outcome string
	Metrics map[string]interface{}
}

// ResponseFailure records failed execution
type ResponseFailure struct {
	ResponseID string
	FailureAt time.Time
	ErrorMessage string
	Retryable bool
	ErrorCode string
}

// PerformanceMetrics tracks performance
type PerformanceMetrics struct {
	TotalExecutions int
	SuccessfulExecutions int
	FailedExecutions int
	SuccessRate float64
	AverageDurationMs float64
	P95DurationMs float64
	P99DurationMs float64
}

// ResponsePolicyController enforces response policies
type ResponsePolicyController struct {
	policies       []*ResponsePolicy
	defaultActions DefaultResponseActions
	approvalWorkflow *ApprovalWorkflowManager
	rateLimiter    *ExecutionRateLimiter
}

// ResponsePolicy defines policy constraints
type ResponsePolicy struct {
	ID           string
	Name         string
	Condition    string
	Action       string
	AllowedResponseTypes []ResponseType
	BlockedScenarios []string
}

// ApprovalWorkflowManager manages approval requirements
type ApprovalWorkflowManager struct {
	requireApprovalForSeverity map[redteam_simulation.FindingSeverity]bool
	autoApproveLowSeverities bool
	escalationRules []EscalationRule
}

// EscalationRule defines escalation conditions
type EscalationRule struct {
	Name         string
	Trigger      string
	EscalateTo   string
	Timeout      time.Duration
	MessageTemplate string
}

// ExecutionRateLimiter prevents response storms
type ExecutionRateLimiter struct {
	maxExecutionsPerMinute int
burstAllowance int
cooldownPeriod time.Duration
executionTimestamps []time.Time
}

// ResponseValidator validates responses before execution
type ResponseValidator struct {
	safetyCheckers []*SafetyChecker
	impactAnalyzer *ImpactAnalyzer
	conflictDetector *ResponseConflictDetector
	willCauseDowntime bool
}

// SafetyChecker performs safety validation
type SafetyChecker struct {
	checkName      string
	checkFunction  func(*DefenseResponse, context.Context) (bool, string)
	isCritical     bool
	priority       int
}

// ImpactAnalyzer assesses response impact
type ImpactAnalyzer struct {
	systemComponents []string
	dependencyGraph map[string][]string
	changeImpactMap map[string]float64
}

// ResponseConflictDetector detects conflicting responses
type ResponseConflictDetector struct {
	incompatiblePairs [][2]ResponseType
	concurrentBlocks []ConcurrentResponseBlock
}

// ConcurrentResponseBlock defines blocked concurrent responses
type ConcurrentResponseBlock struct {
	BlockedTypes []ResponseType
	Reason string
	ExceptionConditions []string
}

// ThreatKnowledgeBase maintains threat intelligence
type ThreatKnowledgeBase struct {
	mitreATT&CKMapping map[string]*MitreTechnique
	vulnerabilityDatabase map[string]*CVEEntry
	tacticTechniqueMatrix [][]string
	updateFrequency time.Duration
	lastUpdateTime time.Time
}

// MitreTechnique maps to MITRE ATT&CK framework
type MitreTechnique struct {
	ID             string
	Name           string
	Technique      string
	Tactics        []string
	Description    string
	DetectionTips  []string
	Mitigations    []string
	DataSources    []string
}

// CVEEntry contains CVE information
type CVEEntry struct {
	CVEID          string
	Severity       redteam_simulation.FindingSeverity
	Description    string
	References     []string
	VulnerableSoftware []string
	PatchesAvailable bool
	ExploitationConfirmed bool
}

// NewAIThreatClassifier creates enhanced threat classifier
func NewAIThreatClassifier() *AIThreatClassifier {
	return &AIThreatClassifier{
		ruleBasedPatterns: initializeThreatPatterns(),
		machineLearningModel: initializeMLClassifier(),
		anomalyDetector: initializeAnomalyDetector(),
		contextualAnalyzer: initializeContextualAnalyzer(),
		knowledgeBase: initializeThreatKnowledgeBase(),
	}
}

// initializeThreatPatterns sets up default threat signatures
func initializeThreatPatterns() []*ThreatPattern {
	return []*ThreatPattern{
		{
			ID:          "TP-001",
			Name:        "Buffer Overflow Attempt",
			Severity:    redteam_simulation.SeverityCritical,
			TechniqueID: "T1190",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`buffer.*overflow|stack.?smash`),
				regexp.MustCompile(`cwe\-?120|cwe120`),
			},
			DetectionRule: "Detect attempts to exploit buffer overflow vulnerabilities",
			Mitigation:    "Implement ASLR, DEP, stack canaries, and bounds checking",
			SimilarityThreshold: 0.85,
		},
		{
			ID:          "TP-002",
			Name:        "Privilege Escalation",
			Severity:    redteam_simulation.SeverityHigh,
			TechniqueID: "T1068",
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`privilege.?escalation|priv_esc`),
				regexp.MustCompile(`sudo.*exploitation|kernel.?exploit`),
			},
			DetectionRule: "Identify privilege escalation attempt patterns",
			Mitigation: "Apply principle of least privilege and minimize sudo access",
			SimilarityThreshold: 0.80,
		},
		// Additional threat patterns would be initialized here
	}
}

// initializeMLClassifier sets up the ML classifier
func initializeMLClassifier() *SimpleMLClassifier {
	return &SimpleMLClassifier{
		featureWeights: map[string]float64{
			"risk_score":            0.25,
			"severity_weight":       0.20,
			"cve_count":             0.15,
			"exposure_vector":       0.15,
			"attack_complexity":     0.10,
			"authentication_required": 0.08,
			"user_interaction":      0.07,
		},
		classificationRules: []ClassificationRule{
			{
				Name:         "Critical Threat Classification",
				Conditions: []Condition{
					{Feature: "risk_score", Operator: ">=", Value: 9.0},
					{Feature: "severity_weight", Operator: "==", Value: redteam_simulation.SeverityCritical},
				},
				Priority: 1,
				Classification: "CRITICAL_THREAT",
				Confidence: 0.95,
			},
			{
				Name:         "High Threat Classification",
				Conditions: []Condition{
					{Feature: "risk_score", Operator: ">=", Value: 7.0},
					{Feature: "risk_score", Operator: "<", Value: 9.0},
				},
				Priority: 2,
				Classification: "HIGH_THREAT",
				Confidence: 0.85,
			},
			{
				Name:         "Medium Threat Classification",
				Conditions: []Condition{
					{Feature: "risk_score", Operator: ">=", Value: 4.0},
					{Feature: "risk_score", Operator: "<", Value: 7.0},
				},
				Priority: 3,
				Classification: "MEDIUM_THREAT",
				Confidence: 0.75,
			},
			{
				Name:         "Low Threat Classification",
				Conditions: []Condition{
					{Feature: "risk_score", Operator: ">=", Value: 0.0},
					{Feature: "risk_score", Operator: "<", Value: 4.0},
				},
				Priority: 4,
				Classification: "LOW_THREAT",
				Confidence: 0.65,
			},
		},
		learningRate: 0.01,
		modelVersion: "v1.0.0",
		history: &TrainingHistory{
		-trainingCount: 0,
		 lastAccuracy: 0.0,
		 modelsTrained: []*ModelSnapshot{},
		},
	}
}

// ThreateKnowledgeBase initialization helper functions
func initializeAnomalyDetector() *BehaviorAnomalyDetector {
	return &BehaviorAnomalyDetector{
		baselineMetrics: &BehavioralBaseline{
			NormalVulnerabilityCount: 0,
			AverageRiskScore: 0.0,
			CommonAttackVectors: []string{},
			ExpectedDetectionRate: 0.95,
			TypicalResponseTimeMS: 500.0,
		},
		thresholdConfig: AnomalyThresholdConfig{
			VulnerabilityCountThreshold: 5,
			RiskScoreThreshold: 7.0,
			DeviationStandardDeviations: 2.0,
			PatternUniquenessThreshold: 0.90,
		},
		alertCoalesceTime: 30 * time.Second,
	}
}

func initializeContextualAnalyzer() *ContextualThreatAnalyzer {
	return &ContextualThreatAnalyzer{
		environmentContext: make(map[string]interface{}),
		correlationEngine: &CorrelationEngine{
			correlationWindows: map[string]time.Duration{
				"rapid_succession": 1 * time.Minute,
				"sustained_attack": 5 * time.Minute,
				"campaign_pattern": 30 * time.Minute,
			},
			eventGroupingRules: []GroupingRule{},
			temporalAnalyzer: &TemporalEventAnalyzer{
				patternCache: make(map[string][]time.Time),
				detectionWindows: []time.Duration{
					1 * time.Minute,
					5 * time.Minute,
					15 * time.Minute,
				},
				spikeThreshold: 3.0,
			},
			sourceAnalyzer: &SourceCorrelationAnalyzer{
				trustedSources: []string{"internal_security_team"},
				untrustedSources: []string{"external_threat_feeds"},
				ipReputationMap: make(map[string]float64),
			},
		},
		riskScorer: &DynamicRiskScorer{
			baseScores: map[string]float64{
				"critical": 9.0,
				"high": 7.0,
				"medium": 5.0,
				"low": 2.5,
				"info": 1.0,
			},
			contextMultipliers: map[string]float64{
				"public_exposure": 1.5,
				"data_sensitive": 1.3,
				"privileged_access": 1.4,
			},
			timeDecayFactor: 0.95,
			severityAdjustments: map[redteam_simulation.FindingSeverity]float64{
				redteam_simulation.SeverityCritical: 1.5,
				redteam_simulation.SeverityHigh: 1.3,
				redteam_simulation.SeverityMedium: 1.1,
				redteam_simulation.SeverityLow: 1.0,
				redteam_simulation.SeverityInfo: 0.9,
			},
		},
		threatIntegrator: &ExternalThreatFeedIntegrator{
			feedURLs: []string{},
			cacheDuration: 1 * time.Hour,
			updateInterval: 6 * time.Hour,
		},
	}
}

func initializeThreatKnowledgeBase() *ThreatKnowledgeBase {
	return &ThreatKnowledgeBase{
		mitreATT&CKMapping: make(map[string]*MitreTechnique),
		vulnerabilityDatabase: make(map[string]*CVEEntry),
		tacticTechniqueMatrix: [][]string{},
		updateFrequency: 24 * time.Hour,
	}
}

// ClassifyThreats classifies findings into threat categories
func (a *AIThreatClassifier) ClassifyThreats(findings []redteam_simulation.Finding) []ClassifiedThreat {
	var classifiedThreats []ClassifiedThreat

	for _, finding := range findings {
		classified := a.classifySingleFinding(finding)
		if classified != nil {
			classifiedThreats = append(classifiedThreats, *classified)
		}
	}

	// Sort by confidence descending
	sort.Slice(classifiedThreats, func(i, j int) bool {
		return classifiedThreats[i].Confidence > classifiedThreats[j].Confidence
	})

	return classifiedThreats
}

// classifySingleFinding performs multi-layer classification on single finding
func (a *AIThreatClassifier) classifySingleFinding(finding redteam_simulation.Finding) *ClassifiedThreat {
	classified := &ClassifiedThreat{
		OriginalFinding: finding,
		ClassificationTimestamp: time.Now(),
	}

	// Layer 1: Rule-based pattern matching
	patternMatch := a.matchThreatPatterns(finding)
	classified.PatternMatches = patternMatch
	if len(patternMatch) > 0 {
		classified.Confidence += 0.2
		classified.TechniqueIDs = append(classified.TechniqueIDs, patternMatch[0].TechniqueID)
	}

	// Layer 2: ML-based classification
	mlPrediction := a.machineLearningModel.predictThreatLevel(finding)
	classified.MLPrediction = mlPrediction
	classified.Confidence += mlPrediction.Confidence * 0.4

	// Layer 3: Anomaly detection
	anomalyScore := a.anomalyDetector.detectAnomaly(finding)
	classified.AnomalyScore = anomalyScore
	if anomalyScore > 0.8 {
		classified.Flags = append(classified.Flags, "anomalous_behavior")
	}

	// Layer 4: Contextual enhancement
	contextualScore := a.contextualAnalyzer.enrichAssessment(finding, classified)
	classified.ContextualRiskScore = contextualScore

	// Normalize confidence to [0, 1]
	if classified.Confidence > 1.0 {
		classified.Confidence = 1.0
	}

	return classified
}

// matchThreatPatterns matches findings against known patterns
func (a *AIThreatClassifier) matchThreatPatterns(finding redteam_simulation.Finding) []*ThreatPattern {
	var matches []*ThreatPattern
	
	evidence := strings.ToLower(finding.Evidence + " " + finding.Title + " " + finding.Description)

	for _, pattern := range a.ruleBasedPatterns {
		for _, regex := range pattern.Patterns {
			if regex.MatchString(evidence) {
				matches = append(matches, pattern)
				break
			}
		}
	}

	return matches
}

// detectAnomaly checks if finding deviates from baseline
func (b *BehaviorAnomalyDetector) detectAnomaly(finding redteam_simulation.Finding) float64 {
	score := 0.0

	// Risk score deviation
	if finding.RiskScore > b.thresholdConfig.RiskScoreThreshold {
		deviation := (finding.RiskScore - b.thresholdConfig.RiskScoreThreshold) / b.thresholdConfig.RiskScoreThreshold
		score += math.Min(deviation, 0.5)
	}

	// Severity weight
	if finding.Severity == redteam_simulation.SeverityCritical {
		score += 0.4
	} else if finding.Severity == redteam_simulation.SeverityHigh {
		score += 0.3
	}

	// Novelty detection (if no similar patterns seen)
	score += 0.1

	return math.Min(score, 1.0)
}

// enrichAssessment adds contextual scoring
func (c *ContextualThreatAnalyzer) enrichAssessment(
	finding redteam_simulation.Finding,
	classified *ClassifiedThreat,
) float64 {
	baseScore := finding.RiskScore
	
	// Apply context multipliers
	for contextKey, multiplier := range c.riskScorer.contextMultipliers {
		if strings.Contains(strings.ToLower(finding.Description), contextKey) {
			baseScore *= multiplier
		}
	}

	// Apply severity adjustment
	severityMult := c.riskScorer.severityAdjustments[finding.Severity]
	finalScore := baseScore * severityMult

	// Clamp to [0, 10]
	if finalScore > 10.0 {
		finalScore = 10.0
	}

	return finalScore
}

// ML Prediction implementation
func (m *SimpleMLClassifier) predictThreatLevel(finding redteam_simulation.Finding) *MLPrediction {
	features := m.extractFeatures(finding)
	
	// Evaluate each rule
	var bestMatch *ClassificationRule
	bestScore := 0.0

	for _, rule := range m.classificationRules {
		score := m.evaluateRule(rule, features)
		if score > bestScore {
			bestMatch = &rule
			bestScore = score
		}
	}

	prediction := &MLPrediction{
		Classification: "UNKNOWN",
		Confidence: 0.5,
		Features: features,
	}

	if bestMatch != nil && bestScore > 0.5 {
		prediction.Classification = bestMatch.Classification
		prediction.Confidence = bestScore * bestMatch.Confidence
	}

	return prediction
}

// extractFeatures extracts relevant features from finding
func (m *SimpleMLClassifier) extractFeatures(finding redteam_simulation.Finding) map[string]interface{} {
	return map[string]interface{}{
		"risk_score": finding.RiskScore,
		"severity_weight": finding.Severity,
		"cve_count": countCVEs(finding.CWE),
		"exposure_vector": calculateExposure(finding.Location),
		"attack_complexity": estimateComplexity(finding.Title),
		"authentication_required": checkAuthNeeded(finding.CWE),
		"user_interaction": estimateUserInteraction(finding.Tags),
	}
}

// evaluateRule scores how well rule matches features
func (m *SimpleMLClassifier) evaluateRule(rule ClassificationRule, features map[string]interface{}) float64 {
	score := 0.0
	matchedConditions := 0

	for _, condition := range rule.Conditions {
		if m.conditionMatches(condition, features) {
			score++
			matchedConditions++
		}
	}

	if matchedConditions == len(rule.Conditions) {
		return rule.Confidence
	}

	return 0.0
}

// conditionMatches evaluates single condition
func (m *SimpleMLClassifier) conditionMatches(condition Condition, features map[string]interface{}) bool {
	featureValue := features[condition.Feature]

	switch condition.Operator {
	case ">":
		return compareValues(featureValue, condition.Value, GT)
	case "<":
		return compareValues(featureValue, condition.Value, LT)
	case "==":
		return featureValue == condition.Value
	case ">=":
		return compareValues(featureValue, condition.Value, GTE)
	case "<=":
		return compareValues(featureValue, condition.Value, LTE)
	case "contains":
		return stringsContains(fmt.Sprintf("%v", featureValue), fmt.Sprintf("%v", condition.Value))
	}

	return false
}

// generateResponses generates appropriate defensive actions
func (b *BlueTeamDetectionEngine) generateResponses(threats []ClassifiedThreat) []*GeneratedResponse {
	var responses []*GeneratedResponse

	for _, threat := range threats {
		response := b.generateSingleResponse(threat)
		if response != nil {
			responses = append(responses, response)
		}
	}

	return responses
}

// generateSingleResponse creates response for single threat
func (b *BlueTeamDetectionEngine) generateSingleResponse(threat ClassifiedThreat) *GeneratedResponse {
	response := &GeneratedResponse{
		ThreatID: threat.ID,
		Classification: threat.Classification,
		Confidence: threat.Confidence,
		CreatedAt: time.Now(),
	}

	// Select appropriate response based on threat level
	var selectedResponse *DefenseResponse
	
	switch threat.Classification {
	case "CRITICAL_THREAT":
		selectedResponse = selectHighestPriorityResponse(
			[]ResponseType{ResponseIsolate, ResponseBlock, ResponseMitigate},
			SeverityCritical,
		)
	case "HIGH_THREAT":
		selectedResponse = selectHighestPriorityResponse(
			[]ResponseType{ResponseBlock, ResponseContain, ResponseMitigate},
			SeverityHigh,
		)
	case "MEDIUM_THREAT":
		selectedResponse = selectHighestPriorityResponse(
			[]ResponseType{ResponseMitigate, ResponseMonitor},
			SeverityMedium,
		)
	default:
		selectedResponse = selectHighestPriorityResponse(
			[]ResponseType{ResponseMonitor},
			SeverityLow,
		)
	}

	if selectedResponse != nil {
		response.SelectedResponse = selectedResponse
		response.EstimatedEffectiveness = selectedResponse.Effectiveness
		
		// Validate response safety
		isSafe, safetyReasons := validateResponseSafety(selectedResponse)
		response.IsSafetyValidated = isSafe
		response.SafetyReasons = safetyReasons

		// Estimate execution impact
		impact := analyzeResponseImpact(selectedResponse)
		response.ImpactAnalysis = impact
	}

	return response
}

// DetectAndRespond performs complete threat detection and response
func (b *BlueTeamDetectionEngine) DetectAndRespond(
	redTeamReport *redteam_simulation.AssessmentReport,
) (*ResponseReport, error) {
	startTime := time.Now()

	report := &ResponseReport{
		ReportID: fmt.Sprintf("BLUETEAM-%d", time.Now().UnixNano()),
		GenerationTime: startTime,
		RedTeamScanID: redTeamReport.ScanID,
	}

	// Phase 1: AI-powered threat classification
	fmt.Println("Performing AI-enhanced threat classification...")
	classifiedThreats := b.aiThreatDetector.ClassifyThreats(redTeamReport.Findings)
	report.DetectedThreats = classifiedThreats

	// Calculate aggregate threat metrics
	report.ThreatMetrics = calculateAggregateThreatMetrics(classifiedThreats)

	// Phase 2: Generate defensive responses
	fmt.Println("Generating automated defense responses...")
	generatedResponses := b.automatedResponse.GenerateResponses(classifiedThreats)
	report.GeneratedResponses = generatedResponses

	// Calculate response effectiveness
	report.ResponseMetrics = calculateResponseMetrics(generatedResponses)

	// Phase 3: Simulate execution and measure effectiveness
	if len(generatedResponses) > 0 {
		effectivenessResults := simulateResponseExecution(generatedResponses, redTeamReport)
		report.ExecutionSimulations = effectivenessResults
	}

	// Calculate overall effectiveness
	report.OverallEffectiveness = report.calculateOverallEffectiveness()

	// Add strategic recommendations
	report.StrategicRecommendations = b.generateStrategicRecommendations(classifiedThreats, generatedResponses)

	// Generate executive summary
	report.ExecutiveSummary = b.generateExecutiveSummary(redTeamReport, report)

	report.Duration = time.Since(startTime)

	return report, nil
}

// calculateAggregateThreatMetrics computes threat aggregation statistics
func calculateAggregateThreatMetrics(threats []ClassifiedThreat) ThreatMetrics {
	if len(threats) == 0 {
		return ThreatMetrics{}
	}

	var totalConfidence, maxConfidence, avgConfidence float64
	highConfidenceCount := 0
	criticalCount := 0
	highCount := 0
	mediumCount := 0
	lowCount := 0

	for _, threat := range threats {
		totalConfidence += threat.Confidence
		if threat.Confidence > maxConfidence {
			maxConfidence = threat.Confidence
		}

		switch threat.Classification {
		case "CRITICAL_THREAT":
			criticalCount++
		case "HIGH_THREAT":
			highCount++
		case "MEDIUM_THREAT":
			mediumCount++
		case "LOW_THREAT":
			lowCount++
		}

		if threat.Confidence > 0.8 {
			highConfidenceCount++
		}
	}

	avgConfidence = totalConfidence / float64(len(threats))

	return ThreatMetrics{
		TotalThreats: len(threats),
		AverageConfidence: avgConfidence,
		MaxConfidence: maxConfidence,
		HighConfidenceThreats: highConfidenceCount,
		ByClassification: map[string]int{
			"CRITICAL_THREAT": criticalCount,
			"HIGH_THREAT": highCount,
			"MEDIUM_THREAT": mediumCount,
			"LOW_THREAT": lowCount,
		},
	}
}

// calculateResponseMetrics measures response quality
func calculateResponseMetrics(responses []*GeneratedResponse) ResponseMetrics {
	if len(responses) == 0 {
		return ResponseMetrics{}
	}

	var totalEffectiveness float64
	validatedCount := 0
	executableCount := 0

	for _, response := range responses {
		totalEffectiveness += response.EstimatedEffectiveness
		if response.IsSafetyValidated {
			validatedCount++
		}
		if response.Executable {
			executableCount++
		}
	}

	return ResponseMetrics{
		TotalResponses: len(responses),
		AverageEffectiveness: totalEffectiveness / float64(len(responses)),
		SafetyValidatedCount: validatedCount,
		ExecutableCount: executableCount,
	}
}

// ResponseReport contains complete blue team response analysis
type ResponseReport struct {
	ReportID                string                `json:"reportId"`
	GenerationTime          time.Time             `json:"generationTime"`
	Duration                time.Duration         `json:"duration,omitempty"`
	RedTeamScanID           string                `json:"redTeamScanId"`
	DetectedThreats         []ClassifiedThreat    `json:"detectedThreats"`
	ThreatMetrics           ThreatMetrics         `json:"threatMetrics"`
	GeneratedResponses      []*GeneratedResponse  `json:"generatedResponses"`
	ResponseMetrics         ResponseMetrics       `json:"responseMetrics"`
	ExecutionSimulations    []*ExecutionSimulation `json:"executionSimulations,omitempty"`
	OverallEffectiveness    float64               `json:"overallEffectiveness"`
	Summary                 ResponseSummary       `json:"summary"`
	StrategicRecommendations []string             `json:"strategicRecommendations"`
	ExecutiveSummary        string                `json:"executiveSummary"`
}

// ThreatMetrics summarizes threat landscape
type ThreatMetrics struct {
	TotalThreats            int                   `json:"totalThreats"`
	AverageConfidence       float64               `json:"averageConfidence"`
	MaxConfidence           float64               `json:"maxConfidence"`
	HighConfidenceThreats   int                   `json:"highConfidenceThreats"`
	ByClassification        map[string]int        `json:"byClassification"`
	TopTechniques           []string              `json:"topTechniques"`
	EstimatedDamagePotential float64              `json:"estimatedDamagePotential"`
}

// ResponseMetrics summarizes defensive capability
type ResponseMetrics struct {
	TotalResponses        int         `json:"totalResponses"`
	AverageEffectiveness  float64     `json:"averageEffectiveness"`
	SafetyValidatedCount  int         `json:"safetyValidatedCount"`
	ExecutableCount       int         `json:"executableCount"`
	FailureRate           float64     `json:"failureRate,omitempty"`
	AvgExecutionTimeMs    float64     `json:"avgExecutionTimeMs,omitempty"`
}

// GeneratedResponse represents an individual defensive action
type GeneratedResponse struct {
	ThreatID            string            `json:"threatId"`
	OriginalThreat      ClassifiedThreat  `json:"originalThreat"`
	SelectedResponse    *DefenseResponse  `json:"selectedResponse"`
	EstimatedEffectiveness float64         `json:"estimatedEffectiveness"`
	IsSafetyValidated   bool              `json:"isSafetyValidated"`
	SafetyReasons       []string          `json:"safetyReasons,omitempty"`
	ImpactAnalysis      *ImpactEstimate   `json:"impactAnalysis,omitempty"`
	Executable          bool              `json:"executable"`
	RequiresManualReview bool             `json:"requiresManualReview"`
	CreatedAt           time.Time         `json:"createdAt"`
}

// ClassifiedThreat contains enriched threat information
type ClassifiedThreat struct {
	ID                    string              `json:"id"`
	OriginalFinding       redteam_simulation.Finding `json:"originalFinding"`
	Classification        string              `json:"classification"`
	Confidence            float64             `json:"confidence"`
	ClassificationTimestamp time.Time           `json:"classificationTimestamp"`
	PatternMatches        []*ThreatPattern    `json:"patternMatches,omitempty"`
	MLPrediction          *MLPrediction       `json:"mlPrediction,omitempty"`
	AnomalyScore          float64             `json:"anomalyScore,omitempty"`
	ContextualRiskScore   float64             `json:"contextualRiskScore,omitempty"`
	Flags                 []string            `json:"flags,omitempty"`
	RecommendedActions    []string            `json:"recommendedActions,omitempty"`
	TechniqueIDs          []string            `json:"techniqueIds,omitempty"`
}

// MLPrediction contains ML model output
type MLPrediction struct {
	Classification string            `json:"classification"`
	Confidence     float64           `json:"confidence"`
	Features       map[string]interface{} `json:"features"`
	ModelVersion   string            `json:"modelVersion"`
}

// ExecutiveSummary provides high-level overview
func (r *ResponseReport) generateExecutiveSummary(redTeamReport *redteam_simulation.AssessmentReport, self *ResponseReport) string {
	bullets := []string{
		fmt.Sprintf("Analized %d red team findings", len(redTeamReport.Findings)),
		fmt.Sprintf("Detected %d potential threats", len(self.DetectedThreats)),
		fmt.Sprintf("Generated %d defensive responses", len(self.GeneratedResponses)),
		fmt.Sprintf("Overall effectiveness score: %.2f%%", self.OverallEffectiveness*100),
	}

	if self.ThreatMetrics.HighConfidenceThreats > 0 {
		bullets = append(bullets, fmt.Sprintf("%d high-confidence threats require immediate attention", 
			self.ThreatMetrics.HighConfidenceThreats))
	}

	if len(self.StrategicRecommendations) > 0 {
		bullets = append(bullets, "Implemented defensive improvements recommended")
	}

	return fmt.Sprintf("Blue Team Analysis Summary:\n\n%s", strings.Join(bullets, "\n"))
}

// ResponseSummary contains condensed report info
type ResponseSummary struct {
	TotalFindingsAnalyzed int `json:"totalFindingsAnalyzed"`
	ThreatsDetected int `json:"threatsDetected"`
	ResponsesGenerated int `json:"responsesGenerated"`
	EffectiveDefensePercentage float64 `json:"effectiveDefensePercentage"`
}

// ExecutionSimulation simulates response execution
type ExecutionSimulation struct {
	ResponseID string `json:"responseId"`
	SimulatedAt time.Time `json:"simulatedAt"`
	ExpectedOutcome string `json:"expectedOutcome"`
	SuccessProbability float64 `json:"successProbability"`
	EstimatedDurationMs int64 `json:"estimatedDurationMs"`
	SideEffects []string `json:"sideEffects,omitempty"`
	rollbackPlan string `json:"rollbackPlan"`
}
