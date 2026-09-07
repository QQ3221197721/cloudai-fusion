// Package integration provides purple team intelligence fusion and continuous
// improvement mechanisms for OBCE3攻防协同作战平台.
package integration

import (
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/blueteam"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/redteam_simulation"
)

// PurpleTeamIntegrationHub coordinates bidirectional intelligence flow
type PurpleTeamIntegrationHub struct {
	sharedIntelDB *SharedIntelligenceDatabase
	metricDashboard *UnifiedMetricsDashboard
	improvementEngine *ContinuousImprovementEngine
	patternLibrary *AttackDefensePatternLibrary
	knowledgeGraph *SecurityKnowledgeGraph
	mlTrainingPipeline *MLModelTrainingPipeline
	simulationController *PurpleTeamSimulationController
	eventBus *PurpleTeamEventBus
	configManagement *ConfigurationManagementSystem
}

// SharedIntelligenceDatabase maintains shared knowledge repository
type SharedIntelligenceDatabase struct {
	dbPath string
	tables map[string]*IntelTable
	indexes []IndexDefinition
	cache TTLCache
	lock sync.RWMutex
	accessLog *AccessLogManager
}

// IntelTable defines intelligence storage table
type IntelTable struct {
	Name string
	Schema map[string]FieldType
	PartitionStrategy string
	ReplicationFactor int
	CompressionEnabled bool
}

// FieldType defines data types
type FieldType string

const (
	FieldTypeString FieldType = "STRING"
	FieldTypeInt FieldType = "INTEGER"
	FieldTypeFloat FieldType = "FLOAT"
	FieldTypeBool FieldType = "BOOLEAN"
	FieldTypeTimestamp FieldType = "TIMESTAMP"
	FieldTypeJSON FieldType = "JSON"
	FieldTypeBytes FieldType = "BYTES"
)

// IndexDefinition defines database indexes
type IndexDefinition struct {
	Name      string
	TableName string
	Columns   []string
	Unique    bool
	Type      string // "btree", "hash", "fulltext"
}

// TTLCache implements time-to-live caching
type TTLCache struct {
	items map[string]cacheEntry
	defaultTTL time.Duration
	evictionPolicy string
	maxSize int
}

// cacheEntry stores cached value with expiration
type cacheEntry struct {
	Value interface{}
	ExpiresAt time.Time
	CreatedAt time.Time
	AccessCount int
}

// AccessLogManager tracks database access patterns
type AccessLogManager struct {
	logFile *os.File
	retentionPeriod time.Duration
	anonymizeQueries bool
	exportFormats []string
}

// ContinuousImprovementEngine drives iterative enhancement
type ContinuousImprovementEngine struct {
	redTeamEnhancer *RedTeamEnhancementModule
	blueTeamEnhancer *BlueTeamEnhancementModule
	crossLearnings *CrossDomainLearner
	trackingSystem *ImprovementTrackingSystem
	evaluationFramework *ImprovementEvaluationFramework
	feedbackLoop *ClosedFeedbackLoop
}

// RedTeamEnhancementModule improves red team capabilities
type RedTeamEnhancementModule struct {
	detectionAvoidanceTechniques []DetectionAvoidanceTechnique
timingOptimization timingOptimizer
evasionPatternLibrary *EvasionPatternRepository
successRateAnalyzer *SuccessRateAnalytics
adaptivenessController *AdaptivenessController
}

// DetectionAvoidanceTechnique enables stealth techniques
type DetectionAvoidanceTechnique struct {
	ID              string
	Name            string
	Description     string
	Effectiveness   float64
	DetectionRisk   float64
	ExecutionTimeMs float64
	Prerequisites   []string
	FailureScenarios []string
}

// timingOptimizer optimizes attack timing
type timingOptimizer struct {
	patterns []TimingPattern
	adaptiveAlgorithm bool
	learningRate float64
	historicalData []TimingRecord
}

// TimingPattern defines optimal timing pattern
type TimingPattern struct {
	Name        string
	Intervals   []time.Duration
	JitterRange float64
	ContextConditions []string
	ExpectedEffectiveness float64
}

// TimingRecord tracks timing performance
type TimingRecord struct {
	Timestamp time.Time
	TechniqueID string
	Success boolean
	ExecutionDurationMs int64
	DetectionLatencyMs int64
	Context map[string]interface{}
}

// EvasionPatternRepository stores evasion patterns
type EvasionPatternRepository struct {
	patterns map[string]*EvasionPattern
	learnedPatterns []string
	blacklistPatterns []string
	updateFrequency time.Duration
}

// EvasionPattern defines single evasion technique
type EvasionPattern struct {
	PatternID   string
	Description string
	TriggerCondition string
	Action string
	RetryAttempts int
	BackoffStrategy string
	Effectiveness float64
}

// SuccessRateAnalytics analyzes success metrics
type SuccessRateAnalytics struct {
	techniqueMetrics map[string][]TechniquePerformanceRecord
	aggregator *MetricAggregator
	trendAnalyzer *TrendAnalysisEngine
	baselineCalculator *BaselineCalculator
	reportGenerator *PerformanceReportGenerator
}

// TechniquePerformanceRecord captures single attempt outcome
type TechniquePerformanceRecord struct {
	TechniqueID string
	AttemptNumber int
	Success boolean
	DurationMs int64
	Detected boolean
	DetectionLatencyMs int64
	Environment string
	Context map[string]interface{}
}

// BlueTeamEnhancementModule improves blue team detection
type BlueTeamEnhancementModule struct {
	signatureLibrary *ThreatSignatureRepository
	anomalyRuleset *AnomalyDetectionRuleset
	machineLearningModels []*TrainedModel
	detectionOptimization *DetectionOptimizationEngine
	falsePositiveReducer *FalsePositiveReducer
	correlationEnhancer *CorrelationRuleOptimizer
}

// ThreatSignatureRepository stores threat signatures
type ThreatSignatureRepository struct {
	signatures map[string]*ThreatSignature
	updatesSyncInterval time.Duration
	externalFeedURLs []string
	localOverrides map[string]string
	deprecationMap map[string]string
}

// ThreatSignature defines detection signature
type ThreatSignature struct {
	SignatureID   string
	Name          string
	Description   string
	TechnicalDetails string
	Patterns      []*regexp.Regexp
	MITRETechniqueIDs []string
	CVEs          []string
	DetectionConfidence float64
	FalsePositiveRate float64
	EvidenceRequirements []string
	RemediationGuidance string
}

// AnomalyDetectionRuleset defines behavioral rules
type AnomalyDetectionRuleset struct {
	rules         []AnomalyRule
	baselines     map[string]BehavioralBaseline
	dynamicThresholds bool
	learningRate float64
}

// AnomalyRule defines anomaly detection rule
type AnomalyRule struct {
	RuleID string
	Name string
	Description string
	Metrics []string
	Thresholds map[string]ThresholdConfig
	Weight float64
	Actions []string
}

// ThresholdConfig defines threshold parameters
type ThresholdConfig {
	Minimum float64
	Maximum float64
	AlertThreshold float64
	CriticalThreshold float64
	DeviationType string // "absolute", "percentage", "standard_deviation"
	HysteresisMargin float64
}

// TrainedModel represents ML model
type TrainedModel struct {
	ModelID string
	Type string
	Version string
	Accuracy float64
	F1Score float64
	Precision float64
	Recall float64
	TrainingDataSize int
	LastTrainedAt time.Time
	Status ModelStatus
}

// ModelStatus defines training status
type ModelStatus string

const (
	ModelReady ModelStatus = "READY"
	ModelTraining ModelStatus = "TRAINING"
	ModelDeprecated ModelStatus = "DEPRECATED"
	ModelExpired ModelStatus = "EXPIRED"
)

// CrossDomainLearner facilitates cross-domain learning
type CrossDomainLearner struct {
	knowledgeTransferPatterns []KnowledgeTransferPattern
	correlationDiscoverer *PatternCorrelationDiscoverer
	generalizationEngine *GeneralizationEngine
	abstractionLayer *AbstractionMapper
	transfers history *TransferHistoryTracker
}

// KnowledgeTransferPattern defines how knowledge transfers
type KnowledgeTransferPattern struct {
	PatternID string
	SourceDomain string
	TargetDomain string
	TransferMethod string
	Effectiveness float64
	Constraints []string
	Prerequisites []string
}

// PatternCorrelationDiscoverer finds correlations
type PatternCorrelationDiscoverer struct {
	correlationAlgorithms []string
	minConfidence float64
	maxCorrelations int
	timeout time.Duration
	outputFormat string
}

// GeneralizationEngine generalizes domain-specific knowledge
type GeneralizationEngine struct {
	abstractionLevels []int
	generalizationStrategies []string
	validityChecker *GeneralizationValidator
	constraintsEnforcer *ConstraintEnforcementEngine
}

// TransferHistoryTracker records transfer history
type TransferHistoryTracker struct {
	transfers []KnowledgeTransferRecord
	maxHistorySize int
	retentionPolicy string
	analysisFrequency time.Duration
}

// KnowledgeTransferRecord captures single transfer
type KnowledgeTransferRecord struct {
	TransferID string
	Timestamp time.Time
	SourceDomain string
	TargetDomain string
	PatternID string
	Success boolean
	Effectiveness float64
	LearningCurve []float64
	Challenges []string
	Insights []string
}

// SecurityKnowledgeGraph builds knowledge graph
type SecurityKnowledgeGraph struct {
	nodes map[string]*GraphNode
	edges map[string][]*GraphEdge
	inferenceEngine *LogicalInferenceEngine
	ruleProcessor *RuleProcessingEngine
	visualizationRenderer *GraphVisualizationRenderer
	updater *IncrementalUpdater
}

// GraphNode represents knowledge node
type GraphNode struct {
	NodeID string
	NodeType string // "technique", "tool", "vulnerability", "mitigation", "indicator"
	Properties map[string]interface{}
	ConnectivityDegree int
	CentralityScore float64
	Betweenness float64
	PageRank float64
}

// GraphEdge represents connection
type GraphEdge struct {
	EdgeID string
	SourceNodeID string
	TargetNodeID string
	EdgeType string
	Strength float64
	Weight float64
	Directed bool
	Properties map[string]interface{}
}

// LogicalInferenceEngine performs reasoning on graph
type LogicalInferenceEngine struct {
	 inferenceRules []LogicalRule
	propagationAlgorithms []string
	confidenceCalculator *ConfidencePropagationCalculator
	contradictionDetector *ContradictionResolutionEngine
	explanationGenerator *InferenceExplanationGenerator
}

// LogicalRule defines inference rule
type LogicalRule struct {
	RuleID string
	Pattern string
	Consequence string
	Confidence float64
	ApplicabilityConditions []string
}

// AttackDefensePatternLibrary catalogs patterns
type AttackDefensePatternLibrary struct {
	patterns map[string]*PatternCatalog
	searchIndex map[string][]string
	ratingSystem *PatternRatingSystem
	usageTracker *PatternUsageTracker
	communityContributions []*CommunitySubmission
}

// PatternCatalog contains pattern definition
type PatternCatalog struct {
	PatternID string
	Name string
	Description string
	AttackType string
	DefenseType string
	EffectivenessRating float64
	ComplexityRating float64
	Prerequisites []string
	ImplementationSteps []string
	CodeExamples []string
	CustomersReferences []string
	Risks []string
	Mitigations []string
	Variants []string
	TestingGuidance []string
	PerformanceMetrics map[string]float64
	History []VersionRecord
}

// VersionRecord tracks pattern evolution
type VersionRecord struct {
	Version string
	Changes string
	ChangedBy string
	ChangedAt time.Time
	Rationale string
}

// UnifiedMetricsDashboard provides consolidated metrics view
type UnifiedMetricsDashboard struct {
	redux metricsCollector
	blueTeamMetricsCollector
	devsecOpsMetricsCollector
	integrationMetricsCollector
	reportGenerator *ExecutiveReportGenerator
	alertingSystem *UnifiedAlertingSystem
	visualizationRenderer *DashboardVisualizationEngine
	refreshScheduler *MetricsRefreshScheduler
}

// MetricsCollector collects purple team metrics
type PurpleTeamMetricsCollector struct {
	metrics map[string]MetricDefinition
	collectionSchedule time.Duration
	stores []MetricsStore
	transformations []MetricTransformation
	aggregationRules map[string]AggregationRule
	alertThresholds map[string]AlertThreshold
}

// MetricDefinition defines metric specification
type MetricDefinition struct {
	Name string
	Description string
	Unit string
	AggregationFunction string // "sum", "avg", "max", "min", "count"
	CollectionInterval time.Duration
	RetentionPeriod time.Duration
	Dimensions []string
	CalculatedFrom string
}

// AggregationRule defines aggregation logic
type AggregationRule struct {
	RuleID string
	MetricName string
	GroupBy []string
	Function string
	TimeWindow time.Duration
	OutputName string
}

// AlertThreshold defines alert conditions
type AlertThreshold struct {
	ThresholdID string
	MetricName string
	Operator string // ">", "<", ">=", "<=", "==", "between"
	ThresholdValue float64
	MessageTemplate string
	Severity AlertSeverity
	NotificationChannels []string
	CooldownPeriod time.Duration
}

// AlertSeverity defines severity level
type AlertSeverity string

const (
	AlertCritical AlertSeverity = "CRITICAL"
	AlertHigh AlertSeverity = "HIGH"
	AlertMedium AlertSeverity = "MEDIUM"
	AlertLow AlertSeverity = "LOW"
	AlertInfo AlertSeverity = "INFO"
)

// PurpleTeamSimulationController orchestrates simulations
type PurpleTeamSimulationController struct {
	scenarioEngine *ScenarioOrchestrationEngine
	executionMonitor *SimulationExecutionMonitor
	resourceAllocator *ResourceAllocationManager
	cancellationController *GracefulCancellationHandler
	resultAggregator *SimulationResultAggregator
	recorder *SimulationRecorder
}

// ScenarioOrchestrationEngine runs scenarios
type ScenarioOrchestrationEngine struct {
	scenarioLibrary []PurpleTeamScenario
	executionPlanner *ScenarioExecutionPlanner
	runtimeEnv *SimulationRuntimeEnvironment
	progressTracker *ScenarioProgressTracker
	failureHandler *ScenarioFailureHandler
}

// PurpleTeamScenario defines simulation scenario
type PurpleTeamScenario struct {
	ScenarioID string
	Name string
	Description string
	Objectives []string
	Attacks []AttackSequence
	Defenses []DefenseResponse
	Metrics []ScenarioMetric
	SuccessCriteria []SuccessCriterion
	Constraints []ScenarioConstraint
	Duration time.Duration
	RiskLevel RiskLevel
	RequiredResources []string
	Prerequisites []string
	Dependencies []string
}

// AttackSequence defines attack chain
type AttackSequence struct {
	SequenceID string
	Techniques []string
	TimingPattern string
	SuccessProbability float64
	DetectionProbability float64
	EstimatedDurationMs int64
}

// DefenseResponse defines defensive response
type DefenseResponse struct {
	ResponseID string
	Type string
	TriggerCondition string
	ExpectedOutcome string
	ExecutionTimeMs int64
	FalsePositiveRate float64
	ImpactOnOperations float64
}

// ScenarioMetric defines measurement
type ScenarioMetric struct {
	MetricName string
	Description string
	Unit string
	TargetValue float64
	WarningThreshold float64
	CriticalThreshold float64
}

// SuccessCriterion defines pass criteria
type SuccessCriterion struct {
	CriterionID string
	Description string
	Type string // "metric", "behavioral", "temporal"
	Threshold float64
	Operator string
}

// PurpleTeamEventBus handles event communication
type PurpleTeamEventBus struct {
	topics map[string][]EventHandler
	broadcastTimeout time.Duration
	orderGuarantee bool
	acknowledgmentRequired bool
	deadLetterQueue *DeadLetterQueue
	metricsCollector *EventMetricsCollector
}

// EventHandler processes events
type EventHandler struct {
	HandlerID string
	HandlerFunc func(event Event) error
	Priority int
	MaxRetries int
	BackoffStrategy string
}

// Event defines event structure
type Event struct {
	EventID string
	Timestamp time.Time
	EventType string
	Source string
	Topic string
	Payload map[string]interface{}
	Attributes map[string]string
	DeliveryAttempts int
	Status EventStatus
}

// EventStatus defines delivery status
type EventStatus string

const (
	EventPending EventStatus = "PENDING"
	EventDelivered EventStatus = "DELIVERED"
	EventAcknowledged EventStatus = "ACKNOWLEDGED"
	EventFailed EventStatus = "FAILED"
	EventExpired EventStatus = "EXPIRED"
)

// ConfigurationManagementSystem manages settings
type ConfigurationManagementSystem struct {
	configStore *ConfigStore
	versionManager *ConfigurationVersionManager
	changeTracker *ChangeTrackingSystem
	validator *ConfigurationValidator
	migrator *ConfigurationMigrator
}

// ProcessIntelligenceFusion executes intelligence fusion cycle
func (p *PurpleTeamIntegrationHub) ProcessIntelligenceFusion(
	redTeamResults *redteam_simulation.AssessmentReport,
	blueTeamResults *blueteam.ResponseReport,
) (*IntelFusionResult, error) {
	startTime := time.Now()

	fusionResult := &IntelFusionResult{
		FusionID: fmt.Sprintf("INTEL-FUSION-%d", time.Now().UnixNano()),
		StartTime: startTime,
		RedTeamScanID: redTeamResults.ScanID,
		BlueTeamScanID: blueTeamResults.ReportID,
	}

	ctx := context.Background()

	// Step 1: Store findings in shared intelligence database
	fmt.Println("Storing findings in shared intelligence database...")
	dbUpdateResult := p.sharedIntelDB.IndexFindings(redTeamResults, blueTeamResults)
	fusionResult.DatabaseUpdates = dbUpdateResult

	// Step 2: Update knowledge graph
	fmt.Println("Updating security knowledge graph...")
	kgUpdateResult := p.knowledgeGraph.UpdateWithNewFindings(
		redTeamResults.Findings, 
		blueTeamResults.DetectedThreats,
	)
	fusionResult.KnowledgeGraphUpdates = kgUpdateResult

	// Step 3: Extract and store patterns
	fmt.Println("Extracting attack-defense patterns...")
	patternExtractionResult := p.patternLibrary.ExtractAndStorePatterns(
		redTeamResults, 
		blueTeamResults,
	)
	fusionResult.PatternsStored = patternExtractionResult.NumPatternsStored

	// Step 4: Improve red team from blue team feedback
	fmt.Println("Enhancing red team techniques based on detection results...")
	redTeamImprovements := p.improvementEngine.ImproveRedTeamFromBlueFeedback(blueTeamResults)
	fusionResult.RedTeamImprovements = redTeamImprovements

	// Step 5: Improve blue team from red team attacks
	fmt.Println("Enhancing blue team detection from red team techniques...")
	blueTeamImprovements := p.improvementEngine.ImproveBlueTeamFromRedAttacks(redTeamResults)
	fusionResult.BlueTeamImprovements = blueTeamImprovements

	// Step 6: Collect training data for ML models
	fmt.Println("Collecting training data for machine learning...")
	trainingData := p.collectTrainingData(ctx, redTeamResults, blueTeamResults)
	fusionResult.TrainingDataCollected = len(trainingData)

	// Step 7: Train/improve ML models
	if len(trainingData) > minimumTrainingSamples {
		fmt.Println("Training improved ML models...")
		modelTrainingResult := p.mlTrainingPipeline.TrainImprovedModels(
			trainingData,
			fusionResult.BlueTeamImprovements.Metrics,
		)
		fusionResult.ModelsImproved = modelTrainingResult
	}

	// Step 8: Calculate effectiveness metrics
	fmt.Println("Calculating unified effectiveness metrics...")
	metrics := p.metricDashboard.CalculateUnifiedMetrics(
		redTeamResults, 
		blueTeamResults,
		fusionResult,
	)
	fusionResult.UnifiedMetrics = metrics

	// Step 9: Generate improvement recommendations
	fmt.Println("Generating strategic improvement recommendations...")
	recommendations := p.generateStrategicRecommendations(
		redTeamResults, 
		blueTeamResults, 
		fusionResult,
	)
	fusionResult.Recommendations = recommendations

	// Step 10: Schedule follow-up simulation
	if f.configManagement.ShouldScheduleFollowUp() {
		fmt.Println("Scheduling next purple team engagement...")
		nextScenario := p.simulationController.ScheduleNextScenario(
			fusionResult,
		)
		fusionResult.NextSimulationScheduled = nextScenario
	}

	fusionResult.EndTime = time.Now()
	fusionResult.Duration = time.Since(startTime)
	fusionResult.Success = true

	return fusionResult, nil
}

// collectTrainingData gathers combined dataset
func (p *PurpleTeamIntegrationHub) collectTrainingData(
	ctx context.Context,
	redResults *redteam_simulation.AssessmentReport,
	blueResults *blueteam.ResponseReport,
) []TrainingSample {
	var samples []TrainingSample

	for _, redFinding := range redResults.Findings {
		// Find corresponding blue team detection
		var matchedThreat *blueteam.ClassifiedThreat
		
		for _, threat := range blueResults.DetectedThreats {
			if threat.ID == redFinding.ID ||
			   strings.Contains(threat.OriginalFinding.Title, redFinding.Title) {
				matchedThreat = &threat
				break
			}
		}

		sample := TrainingSample{
			SampleID: fmt.Sprintf("SAMPLE-%d-%d", time.Now().UnixNano(), len(samples)),
			RedTeamFinding: redFinding,
			BlueTeamClassification: matchedThreat,
			IsDetected: matchedThreat != nil,
			DetectionConfidence: func() float64 {
				if matchedThreat != nil {
					return matchedThreat.Confidence
				}
				return 0.0
			}(),
			Timestamp: time.Now(),
			Features: extractTrainingFeatures(redFinding, matchedThreat),
		}

		samples = append(samples, sample)
	}

	return samples
}

// extractTrainingFeatures creates feature vector
func extractTrainingFeatures(
	redFinding redteam_simulation.Finding,
	blueThreat *blueteam.ClassifiedThreat,
) map[string]interface{} {
	features := make(map[string]interface{})

	features["risk_score"] = redFinding.RiskScore
	features["severity_level"] = severityToInteger(redFinding.Severity)
	features["cwe_category"] = cweToCategory(redFinding.CWE)
	features["has_cve"] = redFinding.CVE != "" && redFinding.CVE != "N/A"
	features["line_number_present"] := func() bool {
		return redFinding.Line > 0
	}()
	
	if blueThreat != nil {
		features["detection_confidence"] = blueThreat.Confidence
		features["classification"] = blueThreat.Classification
		features["anomaly_score"] = blueThreat.AnomalyScore
		features["contextual_risk"] = blueThreat.ContextualRiskScore
	} else {
		features["detection_confidence"] = 0.0
		features["classification"] = "NOT_DETECTED"
		features["anomaly_score"] = 0.0
		features["contextual_risk"] = redFinding.RiskScore
	}

	// Extract textual features
	text := redFinding.Title + " " + redFinding.Description
	features["title_length"] = len(text)
	features["has_sensitive_words"] = containsSensitiveWords(text)

	return features
}

// Intelligence Fusion Result structure
type IntelFusionResult struct {
	FusionID string `json:"fusionId"`
	StartTime time.Time `json:"startTime"`
	EndTime time.Time `json:"endTime,omitempty"`
	Duration time.Duration `json:"duration"`
	RedTeamScanID string `json:"redTeamScanId"`
	BlueTeamScanID string `json:"blueTeamScanId"`
	Success bool `json:"success"`
	Error string `json:"error,omitempty"`
	
	DatabaseUpdates *DatabaseUpdateSummary `json:"databaseUpdates"`
	KnowledgeGraphUpdates *KGUpdateSummary `json:"knowledgeGraphUpdates"`
	PatternsStored int `json:"patternsStored"`
	
	RedTeamImprovements *ImprovementResult `json:"redTeamImprovements"`
	BlueTeamImprovements *ImprovementResult `json:"blueTeamImprovements"`
	
	TrainingDataCollected int `json:"trainingDataCollected"`
	ModelsImproved *ModelTrainingSummary `json:"modelsImproved,omitempty"`
	
	UnifiedMetrics DashboardMetrics `json:"unifiedMetrics"`
	Recommendations []string `json:"recommendations"`
	NextSimulationScheduled *ScheduledScenario `json:"nextSimulationScheduled,omitempty"`
}

// DatabaseUpdateSummary tracks database operations
type DatabaseUpdateSummary struct {
	RecordsInserted int `json:"recordsInserted"`
	RecordsUpdated int `json:"recordsUpdated"`
	IndexesRebuilt int `json:"indexesRebuilt"`
	QueryPerformance Improvement `json:"queryPerformance,omitempty"`
}

// KGUpdateSummary tracks knowledge graph updates
type KGUpdateSummary struct {
	NodesAdded int `json:"nodesAdded"`
	EdgesAdded int `json:"edgesAdded"`
	PatternsDiscovered int `json:"patternsDiscovered"`
	InferencesMade int `json:"inferencesMade"`
}

// ImprovementResult captures improvement stats
type ImprovementResult struct {
	TechniquesEnhanced int `json:"techniquesEnhanced"`
	RulesUpdated int `json:"rulesUpdated"`
	AccuracyImprovement float64 `json:"accuracyImprovement"`
	FalsePositiveReduction float64 `json:"falsePositiveReduction"`
	EffectivenessGain float64 `json:"effectivenessGain"`
	NewPatternsAdded int `json:"newPatternsAdded"`
	Metrics map[string]float64 `json:"metrics"`
}

// ScheduledScenario defines next simulation
type ScheduledScenario struct {
	ScenarioID string `json:"scenarioId"`
	ScheduledAt time.Time `json:"scheduledAt"`
	EstimatedDuration time.Duration `json:"estimatedDuration"`
	Confidence float64 `json:"confidence"`
	Priority int `json:"priority"`
}

// ModelTrainingSummary tracks ML improvements
type ModelTrainingSummary struct {
	ModelsTrained int `json:"modelsTrained"`
	AverageAccuracy float64 `json:"averageAccuracy"`
	AccuracyImprovement float64 `json:"accuracyImprovement"`
	F1ScoreImprovement float64 `json:"f1ScoreImprovement"`
	TrainingDurationMs int64 `json:"trainingDurationMs"`
	SamplesUsed int `json:"samplesUsed"`
	ModelVersions []string `json:"modelVersions"`
}

// DashboardMetrics contains unified dashboard data
type DashboardMetrics struct {
	OverallPurpleTeamScore float64 `json:"overallPurpleTeamScore"`
	RedTeamScore float64 `json:"redTeamScore"`
	BlueTeamScore float64 `json:"blueTeamScore"`
	AttackCoverage float64 `json:"attackCoverage"`
	DefenseCoverage float64 `json:"defenseCoverage"`
	DetectionRate float64 `json:"detectionRate"`
	ResponseTimeAvgMs float64 `json:"responseTimeAvgMs"`
	FalsePositiveRate float64 `json:"falsePositiveRate"`
	ContinuousImprovementRate float64 `json:"continuousImprovementRate"`
	TrendDirection string `json:"trendDirection"`
	Hotspots []string `json:"hotspots"`
	RecentEvents []DashboardEvent `json:"recentEvents"`
}

// DashboardEvent represents recent activity
type DashboardEvent struct {
	EventID string `json:"eventId"`
	Timestamp time.Time `json:"timestamp"`
	EventType string `json:"eventType"`
	Description string `json:"description"`
	Severity AlertSeverity `json:"severity"`
	MetricValue float64 `json:"metricValue"`
}

// TrainingSample captures ML training example
type TrainingSample struct {
	SampleID string `json:"sampleId"`
	RedTeamFinding redteam_simulation.Finding `json:"redTeamFinding"`
	BlueTeamClassification *blueteam.ClassifiedThreat `json:"blueTeamClassification,omitempty"`
	IsDetected bool `json:"isDetected"`
	DetectionConfidence float64 `json:"detectionConfidence"`
	Timestamp time.Time `json:"timestamp"`
	Features map[string]interface{} `json:"features"`
	Label string `json:"label"`
}

// Improvement tracking helper functions
func calculateOverallScore(redTeamScore, blueTeamScore float64) float64 {
	// Weighted average favoring blue team slightly
	return (redTeamScore * 0.4 + blueTeamScore * 0.6)
}

func severityToInteger(severity redteam_simulation.FindingSeverity) int {
	switch severity {
	case redteam_simulation.SeverityCritical:
		return 5
	case redteam_simulation.SeverityHigh:
		return 4
	case redteam_simulation.SeverityMedium:
		return 3
	case redteam_simulation.SeverityLow:
		return 2
	default:
		return 1
	}
}

func cweToCategory(cwe string) string {
	if cwe == "" || cwe == "N/A" {
		return "unknown"
	}
	
	if strings.Contains(cwe, "120") {
		return "buffer_overflow"
	}
	if strings.Contains(cwe, "787") {
		return "out_of_bounds_write"
	}
	if strings.Contains(cwe, "134") {
		return "format_string"
	}
	
	return "other"
}

func containsSensitiveWords(text string) bool {
	sensitive := []string{"password", "secret", "token", "credential", "apikey"}
	textLower := strings.ToLower(text)
	
	for _, word := range sensitive {
		if strings.Contains(textLower, word) {
			return true
		}
	}
	return false
}

// generateStrategicRecommendations provides high-level guidance
func (p *PurpleTeamIntegrationHub) generateStrategicRecommendations(
	redResults *redteam_simulation.AssessmentReport,
	blueResults *blueteam.ResponseReport,
	fusionResult *IntelFusionResult,
) []string {
	var recommendations []string

	// Priority recommendations based on gaps
	if len(fusionResult.RedTeamImprovements.TechniquesEnhanced) == 0 {
		recommendations = append(recommendations,
			"Consider additional red team training to improve attack simulation realism")
	}

	if fusionResult.BlueTeamImprovements.AccuracyImprovement < 0.1 {
		recommendations = append(recommendations,
			"Blue team detection accuracy improvement below target - review detection rules")
	}

	// Coverage recommendations
	redCoverage := float64(len(redResults.Findings)) / float64(maximumPossibleVulns) * 100
	if redCoverage < 70 {
		recommendations = append(recommendations,
			fmt.Sprintf("Expand red team vulnerability coverage from %.0f%% to at least 90%%", redCoverage))
	}

	// Strategic long-term recommendations
	recommendations = append(recommendations,
		"Establish quarterly purple team engagement cadence",
		"Implement automated knowledge base updates post-engagement",
		"Create dedicated purple team excellence center of innovation",
		"Develop custom attack simulations based on unique infrastructure",
		"Build internal red/blue team talent through certification programs",
	)

	return recommendations
}

const (
	minimumTrainingSamples = 100
	maximumPossibleVulns = 500
)

// NewPurpleTeamIntegrationHub creates integration hub instance
func NewPurpleTeamIntegrationHub() *PurpleTeamIntegrationHub {
	return &PurpleTeamIntegrationHub{
		sharedIntelDB: NewSharedIntelligenceDatabase(),
		metricDashboard: NewUnifiedMetricsDashboard(),
		improvementEngine: NewContinuousImprovementEngine(),
		patternLibrary: NewAttackDefensePatternLibrary(),
		knowledgeGraph: NewSecurityKnowledgeGraph(),
		mlTrainingPipeline: NewMLModelTrainingPipeline(),
		simulationController: NewPurpleTeamSimulationController(),
		eventBus: NewPurpleTeamEventBus(),
		configManagement: NewConfigurationManagementSystem(),
	}
}
