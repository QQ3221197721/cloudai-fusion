// Package purpleteam provides comprehensive Purple Team (Attacker vs Defender) platform
// integrating OBCE3 red team simulation, AISECOPS blue team defense, and DevSecOps security gates.
package purpleteam

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"rand"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/blueteam"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/devsecops"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/integration"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/redteam_simulation"
)

// PurpleTeamPlatform is the complete purple team orchestration system
type PurpleTeamPlatform struct {
	redTeamSimulator *redteam_simulation.RedTeamSimulator
	blueTeamEngine   *blueteam.BlueTeamDetectionEngine
	devSecOpsGates   *devsecops.DevSecOpsGates
	integrationHub   *integration.PurpleTeamIntegrationHub
	config           PlatformConfig
	logger           *PlatformLogger
	metricsCollector *PlatformMetricsCollector
}

// PlatformConfig defines platform-wide configuration
type PlatformConfig struct {
	// Red Team Configuration
	RedTeam redteam_simulation.ScannerConfig `json:"redTeam"`
	
	// Blue Team Configuration
	BlueTeam blueteam.DetectionConfig `json:"blueTeam"`
	
	// DevSecOps Configuration
	DevSecOps devsecops.GatesConfiguration `json:"devSecOps"`
	
	// Integration Configuration
	Integration integration.HubConfig `json:"integration"`
	
	// Operational Settings
	EnableLogging bool `json:"enableLogging"`
	LogLevel string `json:"logLevel"`
	MetricsEnabled bool `json:"metricsEnabled"`
	AuditTrailEnabled bool `json:"auditTrailEnabled"`
}

// DefaultConfig returns standard platform configuration
func DefaultConfig() PlatformConfig {
	return PlatformConfig{
		RedTeam: redteam_simulation.ScannerConfig{
			MaxRecursionDepth: 15,
			IncludeTestFiles: false,
			EnableAdvancedChecks: true,
		},
		BlueTeam: blueteam.DetectionConfig{
			AIModelEnabled: true,
			AnomalyDetectionEnabled: true,
			AutomatedResponseEnabled: true,
		},
		DevSecOps: devsecops.DefaultGatesConfiguration(),
		Integration: integration.DefaultHubConfig(),
		EnableLogging: true,
		LogLevel: "INFO",
		MetricsEnabled: true,
		AuditTrailEnabled: true,
	}
}

// NewPurpleTeamPlatform creates complete purple team platform
func NewPurpleTeamPlatform(config PlatformConfig) (*PurpleTeamPlatform, error) {
	platform := &PurpleTeamPlatform{
		redTeamSimulator: redteam_simulation.NewRedTeamSimulator(config.RedTeam),
		blueTeamEngine: blueteam.NewBlueTeamDetectionEngine(config.BlueTeam),
		devSecOpsGates: devsecops.NewDevSecOpsGates(config.DevSecOps),
		integrationHub: integration.NewPurpleTeamIntegrationHub(),
		config: config,
		logger: NewPlatformLogger(config.LogLevel, config.EnableLogging),
		metricsCollector: NewPlatformMetricsCollector(config.MetricsEnabled),
	}

	if err := platform.validateConfiguration(); err != nil {
		return nil, fmt.Errorf("invalid platform configuration: %w", err)
	}

	platform.logger.Info("Purple Team Platform initialized successfully")
	return platform, nil
}

// validateConfiguration checks platform configuration validity
func (p *PurpleTeamPlatform) validateConfiguration() error {
	if p.config.RedTeam.MaxRecursionDepth <= 0 {
		return fmt.Errorf("red team recursion depth must be positive")
	}

	if p.config.BlueTeam.AutomatedResponseEnabled && !p.config.Integration.BidirectionalFlowEnabled {
		p.logger.Warn("Automated response enabled without bidirectional flow - responses may lack context")
	}

	return nil
}

// RunCompleteEngagement executes full purple team engagement from start to finish
func (p *PurpleTeamPlatform) RunCompleteEngagement(
	ctx context.Context,
	target redteam_simulation.Environment,
	scenario ScenarioSpec,
) (*CompleteEngagementResult, error) {
	startTime := time.Now()
	p.logger.Info("Starting complete purple team engagement", 
		"target", target.TargetPath,
		"scenario", scenario.Name)

	result := &CompleteEngagementResult{
		EngagementID: generateEngagementID(),
		StartTime: startTime,
		TargetEnvironment: target.TargetPath,
		Scenario: scenario.Name,
		Success: false,
	}

	// Phase 1: Red Team Attack Simulation
	p.logger.Info("Phase 1: Initiating Red Team vulnerability assessment...")
	phase1Start := time.Now()
	
	redTeamReport, err := p.redTeamSimulator.IdentifyVulnerabilities(target)
	if err != nil {
		p.logger.Error("Red team assessment failed", "error", err)
		result.Phase1Error = fmt.Sprintf("Red team failed: %v", err)
		return result, fmt.Errorf("red team simulation failed: %w", err)
	}
	
	result.Phase1RedTeam = PhaseResult{
		Name: "Red Team Vulnerability Assessment",
		Status: StatusCompleted,
		Duration: time.Since(phase1Start),
		Output: redTeamReport,
	}
	
	p.logger.Info("Phase 1 completed", 
		"findings_count", len(redTeamReport.Findings),
		"risk_score", redTeamReport.RiskScore,
		"duration_ms", result.Phase1RedTeam.Duration.Milliseconds())

	// Phase 2: Blue Team Detection and Response
	p.logger.Info("Phase 2: Initiating Blue Team threat detection...")
	phase2Start := time.Now()

	blueTeamReport, err := p.blueTeamEngine.DetectAndRespond(redTeamReport)
	if err != nil {
		p.logger.Error("Blue team response failed", "error", err)
		result.Phase2Error = fmt.Sprintf("Blue team failed: %v", err)
		return result, fmt.Errorf("blue team response failed: %w", err)
	}
	
	result.Phase2BlueTeam = PhaseResult{
		Name: "Blue Team Threat Detection",
		Status: StatusCompleted,
		Duration: time.Since(phase2Start),
		Output: blueTeamReport,
	}
	
	p.logger.Info("Phase 2 completed",
		"threats_detected", len(blueTeamReport.DetectedThreats),
		"responses_generated", len(blueTeamReport.GeneratedResponses),
		"detection_rate", blueTeamReport.ThreatMetrics.AverageConfidence,
		"duration_ms", result.Phase2BlueTeam.Duration.Milliseconds())

	// Phase 3: Integration and Intelligence Fusion
	p.logger.Info("Phase 3: Processing intelligence fusion and continuous improvement...")
	phase3Start := time.Now()

	var intelFusionResult *integration.IntelFusionResult
	
	if p.config.Integration.BidirectionalFlowEnabled {
		intelFusionResult = p.integrationHub.ProcessIntelligenceFusion(
			redTeamReport,
			blueTeamReport,
		)
		
		result.Phase3Integration = PhaseResult{
			Name: "Intelligence Fusion",
			Status: StatusCompleted,
			Duration: time.Since(phase3Start),
			Output: intelFusionResult,
		}
		
		p.logger.Info("Phase 3 completed",
			"patterns_stored", intelFusionResult.PatternsStored,
			"training_samples", intelFusionResult.TrainingDataCollected,
			"models_improved", intelFusionResult.ModelsImproved,
			"recommendations_count", len(intelFusionResult.Recommendations))
	} else {
		p.logger.Warn("Bidirectional flow disabled - skipping intelligence fusion")
		result.Phase3Integration = PhaseResult{
			Name: "Intelligence Fusion",
			Status: StatusSkipped,
			Duration: 0,
		}
	}

	// Phase 4: DevSecOps Gate Enforcement (if configured)
	if scenario.EnforceSecurityGates {
		p.logger.Info("Phase 4: Executing DevSecOps security gate validation...")
		phase4Start := time.Now()

		gitCommit := devsecops.NewCommit(
			generateCommitHash(startTime),
			scenario.Branch,
			scenario.Repository,
		)

		securityGateResult, err := p.devSecOpsGates.RunSecurityGate(gitCommit)
		if err != nil {
			p.logger.Error("Security gate check failed", "error", err)
			result.Phase4Error = fmt.Sprintf("Security gate failed: %v", err)
			result.DevSecOpsBlock = true
			result.BlockReason = "Security gates detected critical vulnerabilities"
		} else {
			result.Phase4DevSecOps = PhaseResult{
				Name: "DevSecOps Security Gates",
				Status: mapBoolToStatus(securityGateResult.Passed),
				Duration: time.Since(phase4Start),
				Output: securityGateResult,
			}
			
			if !securityGateResult.Passed {
				result.DevSecOpsBlock = true
				result.BlockReason = securityGateResult.BlockingReason
			}
			
			p.logger.Info("Phase 4 completed",
				"passed", securityGateResult.Passed,
				"risk_score", securityGateResult.RiskScore,
				"issues_found", securityGateResult.Metrics.IssuesFound,
				"duration_ms", result.Phase4DevSecOps.Duration.Milliseconds())
		}
	}

	// Calculate overall effectiveness
	p.logger.Info("Calculating overall engagement effectiveness...")
	result.OverallEffectiveness = p.calculateOverallEffectiveness(
		redTeamReport,
		blueTeamReport,
		intelFusionResult,
	)

	result.EndTime = time.Now()
	result.Duration = time.Since(startTime)
	result.Success = result.isEngagementSuccessful()

	// Generate final summary
	result.Summary = p.generateFinalSummary(result, redTeamReport, blueTeamReport, intelFusionResult)

	// Collect and record metrics
	if p.metricsCollector.Enabled() {
		p.metricsCollector.RecordEngagement(result)
	}

	// Log completion
	if result.Success {
		p.logger.Info("Purple team engagement completed successfully",
			"total_duration_sec", result.Duration.Seconds(),
			"effectiveness_score", result.OverallEffectiveness,
			"phases_completed", countCompletedPhases(result))
	} else {
		p.logger.Warn("Purple team engagement completed with issues",
			"errors", result.errorMessages())
	}

	return result, nil
}

// calculateOverallEffectiveness computes end-to-end effectiveness score
func (p *PurpleTeamPlatform) calculateOverallEffectiveness(
	redReport *redteam_simulation.AssessmentReport,
	blueReport *blueteam.ResponseReport,
	intelResult *integration.IntelFusionResult,
) float64 {
	redScore := min(100.0, float64(len(redReport.Findings))/5.0)
	blueScore := blueReport.OverallEffectiveness * 100.0
	
	var intelScore float64 = 0.0
	if intelResult != nil {
		intelScore = float64(intelResult.UnifiedMetrics.OverallPurpleTeamScore)
	}

	weightedScore := (redScore * 0.3) + (blueScore * 0.5) + (intelScore * 0.2)
	
	return mathMin(weightedScore, 100.0)
}

// generateFinalSummary creates comprehensive summary report
func (p *PurpleTeamPlatform) generateFinalSummary(
	result *CompleteEngagementResult,
	redReport *redteam_simulation.AssessmentReport,
	blueReport *blueteam.ResponseReport,
	intelResult *integration.IntelFusionResult,
) EngagementSummary {
	return EngagementSummary{
		EngagementID: result.EngagementID,
		StartTime: result.StartTime,
		EndTime: result.EndTime,
		Duration: result.Duration,
		Success: result.Success,
		RiskLevel: redReport.RiskLevel,
		TotalFindings: len(redReport.Findings),
		CriticalFindings: countBySeverity(redReport.Findings, redteam_simulation.SeverityCritical),
		HighFindings: countBySeverity(redReport.Findings, redteam_simulation.SeverityHigh),
		ThreatsDetected: len(blueReport.DetectedThreats),
		ResponsesGenerated: len(blueReport.GeneratedResponses),
		DetectionRate: blueReport.ThreatMetrics.AverageConfidence,
		EffectivenessScore: result.OverallEffectiveness,
		BidirectionalFlowEnabled: p.config.Integration.BidirectionalFlowEnabled,
		SecurityGatesPassed: !result.DevSecOpsBlock,
		Recommendations: func() []string {
			if intelResult != nil {
				return intelResult.Recommendations
			}
			return blueReport.StrategicRecommendations
		}(),
	}
}

// CompleteEngagementResult captures entire engagement output
type CompleteEngagementResult struct {
	EngagementID string
	StartTime time.Time
	EndTime time.Time
	Duration time.Duration
	TargetEnvironment string
	Scenario string
	Success bool
	
	Phase1RedTeam PhaseResult
	Phase2BlueTeam PhaseResult
	Phase3Integration PhaseResult
	Phase4DevSecOps PhaseResult
	
	Phase1Error string
	Phase2Error string
	Phase4Error string
	
	OverallEffectiveness float64
	Summary EngagementSummary
	
	DevSecOpsBlock bool
	BlockReason string
	
	Metrics EngagementMetrics
}

// PhaseResult describes individual phase outcome
type PhaseResult struct {
	Name string
	Status PhaseStatus
	Duration time.Duration
	Output interface{}
	Error string
}

// PhaseStatus defines phase execution state
type PhaseStatus string

const (
	StatusPending PhaseStatus = "PENDING"
	StatusInProgress PhaseStatus = "IN_PROGRESS"
	StatusCompleted PhaseStatus = "COMPLETED"
	StatusFailed PhaseStatus = "FAILED"
	StatusSkipped PhaseStatus = "SKIPPED"
)

// EngagementSummary provides high-level overview
type EngagementSummary struct {
	EngagementID string
	StartTime time.Time
	EndTime time.Time
	Duration time.Duration
	Success bool
	RiskLevel redteam_simulation.RiskLevel
	TotalFindings int
	CriticalFindings int
	HighFindings int
	ThreatsDetected int
	ResponsesGenerated int
	DetectionRate float64
	EffectivenessScore float64
	BidirectionalFlowEnabled bool
	SecurityGatesPassed bool
	Recommendations []string
}

// EngagementMetrics tracks performance
type EngagementMetrics struct {
	TotalDurationMs int64
	PhaseDurations map[string]int64
	IssuesFound int
	IssuesResolved int
	MetricsCollected bool
}

// ScenarioSpec defines engagement specification
type ScenarioSpec struct {
	Name string
	Branch string
	Repository string
	EnforceSecurityGates bool
	BidirectionalFlowEnabled bool
	MaximumDuration time.Duration
	RiskTolerance RiskToleranceLevel
}

// RiskToleranceLevel defines acceptable risk
type RiskToleranceLevel string

const (
	ToleranceLow RiskToleranceLevel = "LOW"
	ToleranceMedium RiskToleranceLevel = "MEDIUM"
	ToleranceHigh RiskToleranceLevel = "HIGH"
)

// Helper functions
func mapBoolToStatus(passed bool) PhaseStatus {
	if passed {
		return StatusCompleted
	}
	return StatusFailed
}

func countCompletedPhases(result *CompleteEngagementResult) int {
	count := 0
	if result.Phase1RedTeam.Status == StatusCompleted {
		count++
	}
	if result.Phase2BlueTeam.Status == StatusCompleted {
		count++
	}
	if result.Phase3Integration.Status == StatusCompleted || 
	   result.Phase3Integration.Status == StatusSkipped {
		count++
	}
	if result.Phase4DevSecOps.Status == StatusCompleted || !result.Phase4DevSecOps.Output != nil {
		count++
	}
	return count
}

func (r *CompleteEngagementResult) errorMessages() []string {
	var errors []string
	if r.Phase1Error != "" {
		errors = append(errors, r.Phase1Error)
	}
	if r.Phase2Error != "" {
		errors = append(errors, r.Phase2Error)
	}
	if r.Phase4Error != "" {
		errors = append(errors, r.Phase4Error)
	}
	return errors
}

func (r *CompleteEngagementResult) isEngagementSuccessful() bool {
	if r.Phase1Error != "" || r.Phase2Error != "" {
		return false
	}
	
	if r.DevSecOpsBlock {
		return r.OverallEffectiveness >= 70.0
	}
	
	return r.OverallEffectiveness >= 60.0 && countCompletedPhases(r) >= 2
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func countBySeverity(findings []redteam_simulation.Finding, severity redteam_simulation.FindingSeverity) int {
	count := 0
	for _, f := range findings {
		if f.Severity == severity {
			count++
		}
	}
	return count
}

func generateEngagementID() string {
	return fmt.Sprintf("ENG-%d-%s", 
		time.Now().UnixNano(), 
		randomString(8))
}

func generateCommitHash(t time.Time) string {
	h := sha256.Sum256([]byte(t.String()))
	return fmt.Sprintf("%x", h[:8])
}

func randomString(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

func randomString(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	rand.Seed(time.Now().UnixNano())
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}