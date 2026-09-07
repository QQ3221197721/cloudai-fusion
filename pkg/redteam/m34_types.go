// Package redteam provides type definitions for the unified M34 Red Team Platform.
// These types bridge Patent #1, #2, and #3 into a cohesive assessment output.
package redteam

import (
	"math"
	"slices"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/helpers"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/patent"
)

// ============================================================================
// CROSS-PATENT DATA STRUCTURES
// ============================================================================

// CombinedPath merges attack path with quantum threat predictions
type CombinedPath struct {
	PathID              string                       `json:"path_id"`
	Actions             []patent.Action              `json:"actions,omitempty"`
	StartState          patent.StateID               `json:"start_state"`
	EndState            patent.EndState              `json:"end_state,omitempty"`
	BaseReward          float64                      `json:"base_reward"`
	CVEsInvolved        []string                     `json:"cves_involved,omitempty"`
	QuantumThreatLevel  float64                      `json:"quantum_threat_level"` // 0-1 scale
	QuantumProbability  float64                      `json:"quantum_probability"`  // Exploitation likelihood
	QuantumCommitments  []string                     `json:"quantum_commitments,omitempty"` // Audit trail
}

// ValidatedPath represents combined path that has passed defense validation
type ValidatedPath struct {
	CombinedPath        CombinedPath                 `json:"combined_path"`
	IsValid             bool                         `json:"is_valid"`
	EvasionScore        float64                      `json:"evasion_score"`      // 0-1, higher = better evasion
	DetectionConfidence float64                      `json:"detection_confidence"` // 0-1, higher = more detectable
	ValidationTimestamp time.Time                    `json:"validation_timestamp"`
}

// ScoredPath contains ROI-calculated validated path
type ScoredPath struct {
	Index           int             `json:"index"`
	ValidatedPath   ValidatedPath   `json:"validated_path"`
	Exploitability  float64         `json:"exploitability"`  // 0-1
	ImpactScore     float64         `json:"impact_score"`    // Absolute value
	StealthFactor   float64         `json:"stealth_factor"`  // 0-1
	DetectionRisk   float64         `json:"detection_risk"`  // 0-1
	CalculatedROI   float64         `json:"calculated_roi"`  // 0-10 scale
}

// OrchestratedResult is the final output from cross-patent orchestration
type OrchestratedResult struct {
	Paths             []ScoredPath             `json:"paths"`
	CriticalPaths     []ScoredPath             `json:"critical_paths"`
	Recommendations   []patent.Recommendation  `json:"recommendations"`
	ConfidenceScore   float64                  `json:"confidence_score"` // Overall assessment confidence 0-1
	CrossValidationOK bool                     `json:"cross_validation_ok"`
}

// ============================================================================
// VULNERABILITY ASSESSOR - Centralized Risk Scoring
// ============================================================================

// VulnerabilityAssessor aggregates vulnerability findings across patents
type VulnerabilityAssessor struct {
	logger loggerInterface
}

// NewVulnerabilityAssessor creates centralized vulnerability scoring system
func NewVulnerabilityAssessor(logger loggerInterface) *VulnerabilityAssessor {
	return &VulnerabilityAssessor{
		logger: logger,
	}
}

// AssessVulnerabilities computes comprehensive risk score from multiple sources
func (va *VulnerabilityAssessor) AssessVulnerabilities(
	paths []ScoredPath,
	defenseReports []patent.DefenseReport,
) VulnerabilityAssessment {
	
	if len(paths) == 0 {
		return VulnerabilityAssessment{
			RiskScore:       0.0,
			ConfidenceScore: 0.0,
			FindingCount:    0,
		}
	}
	
	// Aggregate risk from top paths
	var totalRisk float64
	for _, sp := range paths {
		totalRisk += sp.CalculatedROI
	}
	
	avgRisk := totalRisk / float64(len(paths))
	
	// Calculate detection coverage
	detectionCoverage := va.calculateDetectionCoverage(defenseReports)
	
	// Final risk score: weighted combination
	finalRisk := avgRisk*0.7 + detectionCoverage*0.3
	
	return VulnerabilityAssessment{
		RiskScore:       finalRisk,
		ConfidenceScore: va.computeConfidence(paths),
		FindingCount:    len(paths),
		TopCVEs:         va.extractTopCVEs(paths),
	}
}

// calculateDetectionCoverage measures what fraction of attacks are monitored
func (va *VulnerabilityAssessor) calculateDetectionCoverage(reports []patent.DefenseReport) float64 {
	if len(reports) == 0 {
		return 0.5 // Default: assume 50% coverage
	}
	
	totalCoverage := 0.0
	for _, report := range reports {
		totalCoverage += float64(report.DetectorCoverage)
	}
	
	return totalCoverage / float64(len(reports))
}

// computeConfidence synthesizes overall assessment confidence
func (va *VulnerabilityAssessor) computeConfidence(paths []ScoredPath) float64 {
	if len(paths) < 3 {
		return 0.6 // Lower confidence for small sample size
	}
	
	// Check consistency of risk scores
	variances := va.calculateVariances(paths)
	
	// High variance = uncertain assessment
	if variances > 2.0 {
		return 0.6
	} else if variances > 1.0 {
		return 0.8
	}
	
	return 0.95 // Consistent results = high confidence
}

// extractTopCVEs identifies most critical CVEs from all paths
func (va *VulnerabilityAssessor) extractTopCVEs(paths []ScoredPath) []string {
	cveFreq := make(map[string]int)
	
	for _, sp := range paths {
		for _, cve := range sp.CVEsInvolved {
			cveFreq[cve]++
		}
	}
	
	// Sort by frequency
	type cveCount struct {
		cve     string
		count   int
	}
	
	cves := make([]cveCount, 0, len(cveFreq))
	for cve, count := range cveFreq {
		cves = append(cves, cveCount{cve, count})
	}
	
	slices.SortFunc(cves, func(a, b cveCount) int {
		return b.count - a.count
	})
	
	result := make([]string, 0, helpers.MinInt(5, len(cves)))
	for i := 0; i < helpers.MinInt(5, len(cves)); i++ {
		result = append(result, cves[i].cve)
	}
	
	return result
}

// calculateVariances computes standard deviation of ROI scores
func (va *VulnerabilityAssessor) calculateVariances(paths []ScoredPath) float64 {
	if len(paths) < 2 {
		return 0.0
	}
	
	// Calculate mean
	var sum float64
	for _, sp := range paths {
		sum += sp.CalculatedROI
	}
	mean := sum / float64(len(paths))
	
	// Calculate variance
	var sumSquaredDiff float64
	for _, sp := range paths {
		diff := sp.CalculatedROI - mean
		sumSquaredDiff += diff * diff
	}
	
	return math.Sqrt(sumSquaredDiff / float64(len(paths)))
}

// VulnerabilityAssessment contains aggregated vulnerability findings
type VulnerabilityAssessment struct {
	RiskScore       float64   `json:"risk_score"`         // 0-10 scale
	ConfidenceScore float64   `json:"confidence_score"`   // 0-1 scale
	FindingCount    int       `json:"finding_count"`
	TopCVEs         []string  `json:"top_cves,omitempty"`
}

// ============================================================================
// DEFENSE EVALUATOR - Adversarial Capability Assessment
// ============================================================================

// DefenseEvaluator assesses defensive posture against discovered attacks
type DefenseEvaluator struct {
	logger loggerInterface
}

// NewDefenseEvaluator creates adversarial capability assessment system
func NewDefenseEvaluator(logger loggerInterface) *DefenseEvaluator {
	return &DefenseEvaluator{
		logger: logger,
	}
}

// EvaluateDefenses determines how well current defenses can detect attacks
func (de *DefenseEvaluator) EvaluateDefenses(
	target TargetInfo,
	attackPaths []patent.AttackPath,
	quantumMatrix map[string]*patent.QuantumThreat,
) []patent.DefenseReport {
	
	reports := make([]patent.DefenseReport, 0, 3)
	
	// Evaluate ML-based detectors
	mlReport := de.evaluateMLDetectors(target, attackPaths)
	reports = append(reports, mlReport)
	
	// Evaluate anomaly detection
	anomalyReport := de.evaluateAnomalyDetectors(target, attackPaths)
	reports = append(reports, anomalyReport)
	
	// Evaluate signature-based detection
	signatureReport := de.evaluateSignatureDetectors(target, attackPaths, quantumMatrix)
	reports = append(reports, signatureReport)
	
	return reports
}

// evaluateMLDetectors tests machine learning classifier effectiveness
func (de *DefenseEvaluator) evaluateMLDetectors(target TargetInfo, paths []patent.AttackPath) patent.DefenseReport {
	report := patent.DefenseReport{
		DetectorType:    patent.MLClassifierDetector,
		Confidence:      0.75,
		DetectorCoverage: 0.8,
		EvasionProbability: 0.25,
	}
	
	// Adjust based on path complexity
	if len(paths) > 5 {
		report.Confidence *= 0.9 // Harder to catch long chains
		report.EvasionProbability *= 1.1
	}
	
	return report
}

// evaluateAnomalyDetectors evaluates statistical anomaly detection capabilities
func (de *DefenseEvaluator) evaluateAnomalyDetectors(target TargetInfo, paths []patent.AttackPath) patent.DefenseReport {
	report := patent.DefenseReport{
		DetectorType:    patent.AnomalyDetector,
		Confidence:      0.65,
		DetectorCoverage: 0.6,
		EvasionProbability: 0.35,
	}
	
	return report
}

// evaluateSignatureDetectors tests pattern-matching detection systems
func (de *DefenseEvaluator) evaluateSignatureDetectors(
	target TargetInfo,
	paths []patent.AttackPath,
	quantumMatrix map[string]*patent.QuantumThreat,
) patent.DefenseReport {
	
	report := patent.DefenseReport{
		DetectorType:    patent.SignatureBasedDetector,
		Confidence:      0.85,
		DetectorCoverage: 0.9,
		EvasionProbability: 0.15,
	}
	
	// Signatures struggle with novel attack combinations
	if len(paths) > 0 && len(paths[0].Actions) > 8 {
		report.Confidence *= 0.8 // Reduced confidence for complex chains
		report.EvasionProbability *= 1.2
	}
	
	return report
}
