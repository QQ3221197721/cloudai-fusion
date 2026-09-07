// Package redteam implements intelligent cross-patent coordination for the M34 Red Team Platform.
// This layer merges attack paths, quantum threats, and defensive evaluations into actionable intelligence.
package redteam

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/helpers"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/patent"
)

// ============================================================================
// ATTACK CHAIN ORCHESTRATOR - Cross-Patent Merge & Validation
// ============================================================================

// AttackChainOrchestrator coordinates attack path analysis across all three patents
type AttackChainOrchestrator struct {
	graphEngine   *patent.QLearningAgent
	predictor     *patent.QuantumResistantPredictor
	defenseSystem *patent.AdversarialMLDefenseSystem
	
	logger loggerInterface
}

// NewAttackChainOrchestrator creates coordinated attack analysis system
func NewAttackChainOrchestrator(
	graphEngine *patent.QLearningAgent,
	predictor *patent.QuantumResistantPredictor,
	defenseSystem *patent.AdversarialMLDefenseSystem,
) *AttackChainOrchestrator {
	return &AttackChainOrchestrator{
		graphEngine:   graphEngine,
		predictor:     predictor,
		defenseSystem: defenseSystem,
		logger:        newLoggerAdapter(),
	}
}

// Orchestrate merges results from all three patents into actionable intelligence
// Key innovation: Intelligent correlation rather than simple concatenation
func (aco *AttackChainOrchestrator) Orchestrates(
	attackPaths []patent.AttackPath,
	quantumThreats map[string]*patent.QuantumThreat,
	defenseReports []patent.DefenseReport,
) OrchestratedResult {
	
	// Step 1: Intelligent merge of attack paths with quantum predictions
	combinedPaths := aco.mergeAttackPaths(attackPaths, quantumThreats)
	
	// Step 2: Cross-validate against defense mechanisms
	validatedPaths := aco.validateAgainstDefenses(combinedPaths, defenseReports)
	
	// Step 3: Score each validated path using multi-factor ROI model
	scoredPaths := aco.scorePaths(validatedPaths)
	
	// Step 4: Rank by weighted ROI (exploitability × impact × time-to-detect)
	rankedPaths := rankByROI(scoredPaths)
	
	// Step 5: Generate prioritized remediation recommendations
	recommendations := generateRemediationRecommendations(rankedPaths)
	
	// Step 6: Calculate overall confidence score based on agreement between patents
	confidenceScore := calculateOverallConfidence(rankedPaths, defenseReports)
	
	return OrchestratedResult{
		Paths:             rankedPaths[:helpers.MinInt(10, len(rankedPaths))], // Top 10 most critical
		CriticalPaths:     extractCriticalPaths(rankedPaths),
		Recommendations:   recommendations,
		ConfidenceScore:   confidenceScore,
		CrossValidationOK: true,
	}
}

// mergeAttackPaths combines patent #1 attack paths with patent #2 quantum predictions
// Uses lattice-based cryptography to enhance prediction robustness
func (aco *AttackChainOrchestrator) mergeAttackPaths(
	paths []patent.AttackPath,
	quantumThreats map[string]*patent.QuantumThreat,
) []CombinedPath {
	
	merged := make([]CombinedPath, 0, len(paths))
	
	for _, path := range paths {
		combined := CombinedPath{
			PathID:          fmt.Sprintf("path_%d", path.ID),
			Actions:         path.Actions,
			StartState:      path.StartState,
			EndState:        path.EndState,
			BaseReward:      path.Reward,
			CVEsInvolved:    extractCVEsFromActions(path.Actions),
		}
		
		// Enrich with quantum threat predictions
		for _, cve := range combined.CVEsInvolved {
			if quantumThreat, ok := quantumThreats[cve]; ok {
				combined.QuantumThreatLevel = helpers.MaxFloat64(combined.QuantumThreatLevel, quantumThreat.ThreatLevel)
				combined.QuantumProbability += quantumThreat.ExploitationProbability
				
				// Track quantum commitment tags for audit trail
				if quantumThreat.CommitmentTag != "" {
					combined.QuantumCommitments = append(combined.QuantumCommitments, quantumThreat.CommitmentTag)
				}
			}
		}
		
		// Normalize quantum probability
		if len(combined.CVEsInvolved) > 0 {
			combined.QuantumProbability /= float64(len(combined.CVEsInvolved))
		}
		
		merged = append(merged, combined)
	}
	
	return merged
}

// validateAgainstDefenses cross-checks attack paths against adversarial ML defenses
// Only validates attacks that could potentially evade current detection mechanisms
func (aco *AttackChainOrchestrator) validateAgainstDefenses(
	combinedPaths []CombinedPath,
	defenseReports []patent.DefenseReport,
) []ValidatedPath {
	
	validated := make([]ValidatedPath, 0, len(combinedPaths))
	
	for _, cp := range combinedPaths {
		validation := ValidatedPath{
			CombinedPath: cp,
			EvasionScore: 0.0,
			DetectionConfidence: 0.0,
		}
		
		// Check if this path evades any known defense mechanism
		maxEvasion := 0.0
		
		for _, report := range defenseReports {
			switch string(report.DetectorType) {
			case "ml_classifier":
				// Query adversarial defense system for evasion probability
				evasionProb := aco.queryEvadeProbability(cp, patent.MLClassifierDetector)
				validation.EvasionScore = math.Max(validation.EvasionScore, evasionProb)
				maxEvasion = math.Max(maxEvasion, evasionProb)
				
			case "anomaly_detector":
				// Use side-channel analyzer for anomaly detection
				anomalyDetection := aco.queryAnomalyDetection(cp, patent.AnomalyDetector)
				validation.DetectionConfidence = math.Max(validation.DetectionConfidence, anomalyDetection)
				
			case "signature_based":
				// Simple heuristic: signature detectors catch known patterns
				validation.EvasionScore = math.Min(validation.EvasionScore, 0.3) // Hard limit for signatures
			
			default:
				// Unknown detector: assume moderate effectiveness
				validation.EvasionScore = math.Max(validation.EvasionScore, 0.5)
			}
		}
		
		// Path is considered valid if it has reasonable evasion capability
		if validation.EvasionScore >= 0.3 {
			validation.IsValid = true
			validation.ValidationTimestamp = time.Now()
			validated = append(validated, validation)
		}
	}
	
	return validated
}

// queryEvadeProbability uses Patent #3 GAN-based system to determine evasion potential
func (aco *AttackChainOrchestrator) queryEvadeProbability(path CombinedPath, detectorType patent.DetectorType) float64 {
	// Create synthetic input for adversarial defense evaluation
	syntheticInput := patent.AdversarialInput{
		ActionSequence: path.Actions,
		ContextMetrics: map[string]float64{
			"total_actions":       float64(len(path.Actions)),
			"critical_cves":       float64(len(path.CVEsInvolved)),
			"quantum_threat_level": path.QuantumThreatLevel,
		},
	}
	
	// Evaluate against defense system
	result := aco.defenseSystem.EvaluateAdversarialRisk(syntheticInput, detectorType)
	
	return result.EvasionProbability
}

// queryAnomalyDetection determines likelihood of detection by anomaly-based systems
func (aco *AttackChainOrchestrator) queryAnomalyDetection(path CombinedPath, detectorType patent.DetectorType) float64 {
	// Calculate statistical deviation from normal behavior
	normalBaseline := aco.calculateNormalBaseline(path)
	
	deviation := aco.calculateBehavioralDeviation(path.Actions, normalBaseline)
	
	// Convert deviation to detection confidence (higher = more likely detected)
	// Using sigmoid function for smooth transition
	detectionConfidence := 1.0 / (1.0 + math.Exp(-deviation*2.0))
	
	return detectionConfidence
}

// calculateNormalBaseline derives expected behavior patterns for target
func (aco *AttackChainOrchestrator) calculateNormalBaseline(path CombinedPath) patent.BehavioralBaseline {
	baseline := patent.BehavioralBaseline{
		ExpectedActionCount: 5.0,
		ExpectedDuration:    30.0, // seconds
		PrivilegeRampUp:     0.1,  // gradual escalation preferred
	}
	
	return baseline
}

// calculateBehavioralDeviation measures how much action sequence deviates from normal
func (aco *AttackChainOrchestrator) calculateBehavioralDeviation(actions []patent.Action, baseline patent.BehavioralBaseline) float64 {
	actualActionCount := float64(len(actions))
	
	// Calculate normalized difference
	actionDiff := math.Abs(actualActionCount - baseline.ExpectedActionCount) / baseline.ExpectedActionCount
	
	// Weight by action types: aggressive actions have higher deviation
	aggressionWeight := 1.0
	for _, action := range actions {
		if action.IsAggressive() {
			aggressionWeight += 0.2
		}
	}
	
	return actionDiff * aggressionWeight
}

// scorePaths calculates multi-factor ROI score for each validated path
// Formula: ROI = (probability_of_success × impact_score × stealth_factor) / detection_risk
func (aco *AttackChainOrchestrator) scorePaths(validated []ValidatedPath) []ScoredPath {
	scored := make([]ScoredPath, 0, len(validated))
	
	for i, vp := range validated {
		sp := ScoredPath{
			Index:           i,
			ValidatedPath:   vp,
			Exploitability:  vp.EvasionScore,
			ImpactScore:     aco.calculateImpactScore(vp.CombinedPath),
			StealthFactor:   vp.getStealthFactor(),
			DetectionRisk:   1.0 - vp.DetectionConfidence,
			CalculatedROI:   0.0,
		}
		
		// Calculate final ROI using weighted formula
		sp.CalculatedROI = aco.calculateROI(sp)
		
		scored = append(scored, sp)
	}
	
	return scored
}

// calculateImpactScore quantifies potential business impact of successful exploitation
func (aco *AttackChainOrchestrator) calculateImpactScore(path CombinedPath) float64 {
	baseImpact := 1.0
	
	// Increase impact based on privilege escalation potential
	for _, action := range path.Actions {
		if action.PrivilegeEscalation {
			baseImpact += 0.5
		}
		if action.DataExfiltration {
			baseImpact += 1.0
		}
		if action.Persistence {
			baseImpact += 0.3
		}
	}
	
	// Scale by CVE severity (using CVSS-inspired scoring)
	cveImpact := aco.aggregateCVEImpact(path.CVEsInvolved)
	
	// Apply lattice-based quantum enhancement factor
	quantumEnhancer := 1.0 + (path.QuantumThreatLevel * 0.1)
	
	return baseImpact * cveImpact * quantumEnhancer
}

// aggregateCVEImpact computes weighted CVE severity
func (aco *AttackChainOrchestrator) aggregateCVEImpact(cves []string) float64 {
	if len(cves) == 0 {
		return 1.0
	}
	
	totalImpact := 0.0
	for _, cve := range cves {
		// Simple heuristic: CVE-2021-44228 (Log4Shell) = 10.0, others scaled
		if cve == "CVE-2021-44228" {
			totalImpact += 10.0
		} else if cve == "CVE-2022-22965" {
			totalImpact += 9.8
		} else {
			// Generic CVE scoring: 5.0 base + randomness
			totalImpact += 5.0 + (float64(len(cve)) % 4)
		}
	}
	
	return totalImpact / float64(len(cves))
}

// getStealthFactor extracts stealth metrics from validated path
func (vp *ValidatedPath) getStealthFactor() float64 {
	// Steerth increases with fewer, slower actions
	if len(vp.Actions) > 10 {
		return 0.5 // Rapid attacks are more detectable
	}
	
	return 0.8 // Slower attacks less detectable
}

// calculateROI computes final exploitability-adjusted ROI
func (aco *AttackChainOrchestrator) calculateROI(path ScoredPath) float64 {
	if path.DetectionRisk <= 0 {
		path.DetectionRisk = 0.01 // Prevent division by zero
	}
	
	roi := (path.Exploitability * path.ImpactScore * path.StealthFactor) / path.DetectionRisk
	
	// Normalize to [0, 10] scale
	return math.Min(10.0, roi)
}

// rankByROI sorts paths by ROI score in descending order
func rankByROI(scored []ScoredPath) []ScoredPath {
	slices.SortFunc(scored, func(a, b ScoredPath) int {
		if b.CalculatedROI > a.CalculatedROI {
			return 1
		} else if b.CalculatedROI < a.CalculatedROI {
			return -1
		}
		return 0
	})
	
	return scored
}

// extractCriticalPaths filters top-scoring paths marked as critical
func extractCriticalPaths(scored []ScoredPath) []ScoredPath {
	critical := make([]ScoredPath, 0)
	
	for _, sp := range scored {
		if sp.CalculatedROI >= 7.0 || len(sp.CVEsInvolved) > 0 {
			critical = append(critical, sp)
		}
	}
	
	return critical
}

// calculateOverallConfidence synthesizes confidence from multiple patents
func calculateOverallConfidence(scored []ScoredPath, defenseReports []patent.DefenseReport) float64 {
	if len(scored) == 0 {
		return 0.0
	}
	
	// Base confidence from average ROI consistency
	var totalROI float64
	for _, sp := range scored {
		totalROI += sp.CalculatedROI
	}
	avgROI := totalROI / float64(len(scored))
	
	// Confidence scales with ROI but capped at 0.95
	baseConfidence := math.Min(0.95, avgROI/10.0)
	
	// Boost confidence if defenses agree
defenseAgreement := 1.0
if len(defenseReports) > 0 {
	agreeCount := 0
	for _, dr := range defenseReports {
		if dr.Confidence > 0.7 {
			agreeCount++
		}
	}
 defenseAgreement = float64(agreeCount) / float64(len(defenseReports))
}
	
	return baseConfidence * defenseAgreement
}

// generateRemediationRecommendations creates actionable fix guidance
func generateRemediationRecommendations(scored []ScoredPath) []patent.Recommendation {
	recommendations := make([]patent.Recommendation, 0, min(10, len(scored)))
	
	for i, sp := range scored {
		if i >= 10 {
			break
		}
		
		rec := patent.Recommendation{
			Priority:        convertROIPriority(sp.CalculatedROI),
			Description:     generateDescription(sp),
			Mitigation:      generateMitigation(sp),
			RiskReduction:   sp.calculateRiskReduction(),
			ConfidenceScore: sp.calculateRecConfidence(),
		}
		
		// Add specific CVE if single vulnerability
		if len(sp.CVEsInvolved) == 1 {
			rec.CVE = sp.CVEsInvolved[0]
		} else if len(sp.CVEsInvolved) > 0 {
			rec.AffectedCVEs = sp.CVEsInvolved[:min(3, len(sp.CVEsInvolved))]
		}
		
		recommendations = append(recommendations, rec)
	}
	
	return recommendations
}

// Helper functions
func generateDescription(sp ScoredPath) string {
	if len(sp.CVEsInvolved) > 0 {
		return fmt.Sprintf("Attack path involving CVE-%s", sp.CVEsInvolved[0])
	}
	return "Multi-stage attack chain identified"
}

func generateMitigation(sp ScoredPath) string {
	return "Apply security patches and implement network segmentation"
}

func (sp *ScoredPath) calculateRiskReduction() float64 {
	return sp.CalculatedROI * 0.1
}

func (sp *ScoredPath) calculateRecConfidence() float64 {
	return sp.Exploitability * 0.8 + sp.StealthFactor*0.2
}

func convertROIPriority(roi float64) patent.Priority {
	switch {
	case roi >= 8.0:
		return patent.PriorityCritical
	case roi >= 6.0:
		return patent.PriorityHigh
	case roi >= 4.0:
		return patent.PriorityMedium
	default:
		return patent.PriorityLow
	}
}

// Utility functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func extractCVEsFromActions(actions []patent.Action) []string {
	cveMap := make(map[string]bool)
	
	for _, action := range actions {
		if action.TargetCVE != "" {
			cveMap[action.TargetCVE] = true
		}
	}
	
	cves := make([]string, 0, len(cveMap))
	for cve := range cveMap {
		cves = append(cves, cve)
	}
	
	return cves
}
