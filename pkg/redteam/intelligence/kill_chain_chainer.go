package redteam

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

// KillChainChainer orchestrates multi-step attack path construction from CVEs
type KillChainChainer struct {
	logger      *logrus.Logger
	ruleset     *AttackRuleset
	policy      ScoringPolicy
}

// NewKillChainChainer creates a chainer with default rules
func NewKillChainChainer(logger *logrus.Logger) *KillChainChainer {
	return &KillChainChainer{
		logger: logger,
		ruleset: NewAttackRuleset(),
		policy: ScoringPolicy{
			PreferShorter:         true,
			PreferVerifiedPoC:     true,
			AvoidNoisyVectors:     true,
			MinimizeDetectionRisk: true,
		},
	}
}

// FindOptimalAttackPath discovers the shortest/most reliable attack chain
func (kcc *KillChainChainer) FindOptimalAttackPath(
	ctx context.Context,
	targetCVEs []string,
	goalPhases []string,
	constraints AttackConstraints,
) (*AttackChainResult, error) {
	
	kcc.logger.WithFields(logrus.Fields{
		"target_cves": len(targetCVEs),
		"goal_phases": goalPhases,
	}).Info("Starting optimal attack path search")

	// Build candidate paths from all combinations
	var candidatePaths []*AttackChain

	for _, cve := range targetCVEs {
		path := kcc.buildSingleStepChain(ctx, cve, constraints)
		if path != nil {
			candidatePaths = append(candidatePaths, path)
		}
	}

	// Try to extend paths with follow-up exploits
	extendedPaths := kcc.extendAttackPaths(ctx, candidatePaths, 3, constraints)

	// Score and rank by preference policy
	scoredResults := kcc.scoreAttackPaths(extendedPaths)

	if len(scoredResults) == 0 {
		return nil, fmt.Errorf("no valid attack paths found")
	}

	// Return top result
	bestResult := scoredResults[0]

	return &AttackChainResult{
		Path:            bestResult.Path,
		Score:           bestResult.Score,
		Rationale:       bestResult.Rationale,
		EstimatedTime:   kcc.estimateExecutionTime(bestResult.Path),
		DetectionRisk:   kcc.calculateDetectionRisk(bestResult.Path),
		ExploitReliability: kcc.calculateExploitReliability(bestResult.Path),
	}, nil
}

// buildSingleStepChain constructs a simple single-attack CVE exploit chain
func (kcc *KillChainChainer) buildSingleStepChain(ctx context.Context, targetCVE string, constraints AttackConstraints) *AttackChain {
	
	step := &AttackStep{
		ID:              fmt.Sprintf("step-%s", targetCVE),
		Type:            StepCVEExploit,
		CVEID:           targetCVE,
		Phase:           "Initial Access",
		Prerequisite:    nil,
		SuccessCondition: func(result ExploitResult) bool {
			return result.Success && !result.Errored
		},
		PostAction:      nil,
		RequiredPrivileges: PrivilegeLevelUser,
		EvasionTechniques: []EvasionTech{},
		DetectionLikelihood: DetectionHigh,
		RiskScore:       7.5,
	}

	return &AttackChain{
		ID:             fmt.Sprintf("chain-single-%s", targetCVE),
		Name:           fmt.Sprintf("Single CVE: %s", targetCVE),
		Description:    fmt.Sprintf("Direct exploitation of %s", targetCVE),
		Steps:          []*AttackStep{step},
		StartPhase:     "Initial Access",
		EndPhase:       "Actions on Objectives",
		TotalDuration:  time.Hour,
		RiskLevel:      RiskHigh,
		Status:         StatusReady,
	}
}

// extendAttackPaths attempts to add follow-up steps to existing chains
func (kcc *KillChainChainer) extendAttackPaths(ctx context.Context, initialPaths []*AttackChain, maxDepth int, constraints AttackConstraints) []*AttackChain {
	extended := make([]*AttackChain, len(initialPaths))

	for i, path := range initialPaths {
		extended[i] = kcc.recursivelyExtendPath(ctx, path, 0, maxDepth, constraints)
	}

	return extended
}

// recursivelyExtendPath extends a single attack path with additional steps
func (kcc *KillChainChainer) recursivelyExtendPath(ctx context.Context, currentPath *AttackChain, depth int, maxDepth int, constraints AttackConstraints) *AttackChain {
	if depth >= maxDepth {
		return currentPath
	}

	// Get possible next phases based on MITRE ATT&CK mapping
	lastStep := currentPath.Steps[len(currentPath)-1]
	nextPhases := kcc.ruleset.GetFollowingPhases(lastStep.Phase)

	var bestNextStep *AttackStep
	
	for _, phase := range nextPhases {
		step := kcc.generateNextStep(ctx, phase, lastStep, constraints)
		if step != nil && kcc.isValidTransition(lastStep, step) {
			bestNextStep = step
			break
		}
	}

	if bestNextStep == nil {
		return currentPath
	}

	// Add step and continue extending
	newPath := currentPath.DeepCopy()
	newPath.Steps = append(newPath.Steps, bestNextStep)
	newPath.EndPhase = bestNextStep.Phase
	
	return kcc.recursivelyExtendPath(ctx, newPath, depth+1, maxDepth, constraints)
}

// generateNextStep creates a new attack step for the specified phase
func (kcc *KillChainChainer) generateNextStep(ctx context.Context, targetPhase string, prevStep *AttackStep, constraints AttackConstraints) *AttackStep {
	// Map phase to likely techniques/phases
	phaseMapping := map[string]string{
		"Initial Access":        "T1566.001", // Spearphishing
		"Execution":             "T1059",     // Command Line
		"Persistence":           "T1053",     // Scheduled Task
		"Privilege Escalation":  "T1068",     // Exploitation for Privilege Escalation
		"Defense Evasion":       "T1027",     // Obfuscated Files or Information
		"Credential Access":     "T1003",     // OS Credential Dumping
		"Discovery":             "T1082",     // System Information Discovery
		"Lateral Movement":      "T1021",     // Remote Services
		"Collection":            "T1005",     // Data from Local System
		"Command and Control":   "T1071",     // Application Layer Protocol
		"Exfiltration":          "T1041",     "Exfiltration Over C2 Channel",
	}

	techniqueID := phaseMapping[targetPhase]
	if techniqueID == "" {
		return nil
	}

	return &AttackStep{
		ID:              fmt.Sprintf("step-%s-%s", prevStep.ID, techniqueID),
		Type:            StepMITRETechnique,
		TechniqueID:     techniqueID,
		Phase:           targetPhase,
		Prerequisite:    prevStep,
		SuccessCondition: func(result ExploitResult) bool {
			return result.PhaseTransitioned
		},
		PostAction:      func(env AttackEnvironment) {},
		RequiredPrivileges: PrivilegeLevelSystem,
		EvasionTechniques: kcc.selectEvasionForPhase(targetPhase),
		DetectionLikelihood: DetectionMedium,
		RiskScore:       6.5,
	}
}

// isValidTransition checks if the transition between steps is valid
func (kcc *KillChainChainer) isValidTransition(prev, next *AttackStep) bool {
	// Check privilege escalation requirement
	if next.RequiredPrivileges > prev.RequiredPrivileges {
		return prev.StepType == StepPrivilegeEscalation || prev.StepType == StepCVEExploit
	}
	
	// Check prerequisite fulfillment
	if next.Prerequisite != nil && next.Prerequisite.ID != prev.ID {
		return false
	}
	
	return true
}

// selectEvasionForPhase chooses appropriate evasion techniques for each kill chain phase
func (kcc *KillChainChainer) selectEvasionForPhase(phase string) []EvasionTech {
	switch phase {
	case "Defense Evasion":
		return []EvasionTech{
			EvasionObfuscatePayload,
			E evasionUseStagedPayload,
			EvasionEncodeDataStreams,
		}
	case "Privilege Escalation":
		return []EvasionTech{
			EvasionDisableAntivirus,
			EvasionBypassAMERestrictions,
			EvasionProcessInjection,
		}
	case "Lateral Movement":
		return []EvasionTech{
			EvasionLivingOffTheLand,
			EvasionSignedBinaryProxyExecution,
			EvasionPassTheHash,
		}
	default:
		return []EvasionTech{EvasionBasicEvasion}
	}
}

// scoreAttackPaths ranks attack paths by multiple criteria
func (kcc *KillChainChainer) scoreAttackPaths(paths []*AttackChain) []*ScoredAttackChain {
	scored := make([]*ScoredAttackChain, len(paths))

	for i, path := range paths {
		score := kcc.calculatePathScore(path)
		
		rationale := kcc.generateScoringRationale(path, score)
		
		scored[i] = &ScoredAttackChain{
			Path:      path,
			Score:     score,
			Rationale: rationale,
		}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored
}

// calculatePathScore computes a composite score for an attack path
func (kcc *KillChainChainer) calculatePathScore(path *AttackChain) float64 {
	baseScore := 0.0
	
	// Length factor (shorter is better)
	if kcc.policy.PreferShorter {
		lengthBonus := float64(maxPathLength-len(path.Steps))/float64(maxPathLength) * 20
		baseScore += lengthBonus
	}

	// PoC verification bonus
	if kcc.policy.PreferVerifiedPoC {
		verifiedCount := kcc.countVerifiedPoCs(path)
		pocBonus := float64(verifiedCount)/float64(len(path.Steps))*30
		baseScore += pocBonus
	}

	// Risk penalty (lower risk is better)
	if kcc.policy.MinimizeDetectionRisk {
		detectionRisk := kcc.calculateDetectionRisk(path)
		riskPenalty := -float64(detectionRisk) * 20
		baseScore += riskPenalty
	}

	// Reliability factor
	reliabilityFactor := kcc.calculateExploitReliability(path) * 30
	baseScore += reliabilityFactor

	// Coverage bonus (more kill chain phases covered)
	phaseCoverage := float64(kcc.calculateUniquePhases(path)) / float64(totalKillChainPhases) * 20
	baseScore += phaseCoverage

	return baseScore
}

// countVerifiedPoCs counts how many steps have verified PoCs
func (kcc *KillChainChainer) countVerifiedPoCs(path *AttackChain) int {
	count := 0
	for _, step := range path.Steps {
		if step.ExploitMetadata != nil && step.ExploitMetadata.Verified {
			count++
		}
	}
	return count
}

// calculateDetectionRisk estimates the likelihood of detection
func (kcc *KillChainChainer) calculateDetectionRisk(path *AttackChain) float64 {
	totalRisk := 0.0
	for _, step := range path.Steps {
		switch step.DetectionLikelihood {
		case DetectionVeryHigh:
			totalRisk += 1.0
		case DetectionHigh:
			totalRisk += 0.8
		case DetectionMedium:
			totalRisk += 0.5
		case DetectionLow:
			totalRisk += 0.2
		case DetectionVeryLow:
			totalRisk += 0.1
		}
	}
	return totalRisk / float64(len(path.Steps))
}

// calculateExploitReliability estimates overall exploit success probability
func (kcc *KillChainChainer) calculateExploitReliability(path *AttackChain) float64 {
	totalReliability := 0.0
	for _, step := range path.Steps {
		if step.ExploitMetadata != nil {
			if step.ExploitMetadata.ProofOfConcept {
				totalReliability += 0.8
			} else {
				totalReliability += 0.5
			}
		} else {
			totalReliability += 0.3
		}
	}
	return totalReliability / float64(len(path.Steps))
}

// estimateExecutionTime estimates total time needed to complete the chain
func (kcc *KillChainChainer) estimateExecutionTime(path *AttackChain) time.Duration {
	totalTime := time.Duration(0)
	
	for _, step := range path.Steps {
		duration := kcc.estimateStepDuration(step)
		totalTime += duration
	}
	
	return totalTime
}

// estimateStepDuration estimates execution time for a single step
func (kcc *KillChainChainer) estimateStepDuration(step *AttackStep) time.Duration {
	switch step.Type {
	case StepCVEExploit:
		return 5 * time.Minute
	case StepPrivilegeEscalation:
		return 10 * time.Minute
	case StepLateralMovement:
		return 15 * time.Minute
	case StepMITRETechnique:
		return 8 * time.Minute
	default:
		return 5 * time.Minute
	}
}

// calculateUniquePhases counts how many unique kill chain phases are covered
func (kcc *KillChainChainer) calculateUniquePhases(path *AttackChain) int {
	phases := make(map[string]bool)
	for _, step := range path.Steps {
		phases[step.Phase] = true
	}
	return len(phases)
}

// generateScoringRationale provides explanation for scoring decisions
func (kcc *KillChainChainer) generateScoringRationale(path *AttackChain, score float64) string {
	var parts []string
	
	// Length analysis
	if len(path.Steps) <= 3 {
		parts = append(parts, "Short chain (good)")
	} else {
		parts = append(parts, fmt.Sprintf("Long chain with %d steps (slower)", len(path.Steps)))
	}
	
	// PoC status
	verifiedCount := kcc.countVerifiedPoCs(path)
	if verifiedCount == len(path.Steps) {
		parts = append(parts, "All exploits have verified PoCs")
	} else if verifiedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d exploits verified", verifiedCount, len(path.Steps)))
	}
	
	// Detection risk
	risk := kcc.calculateDetectionRisk(path)
	if risk < 0.3 {
		parts = append(parts, "Low detection risk")
	} else if risk < 0.6 {
		parts = append(parts, "Medium detection risk")
	} else {
		parts = append(parts, "High detection risk")
	}
	
	return fmt.Sprintf("Attack chain score %.2f: %v", score, parts)
}

// DeepCopy creates a copy of the attack chain
func (ac *AttackChain) DeepCopy() *AttackChain {
	stepsCopy := make([]*AttackStep, len(ac.Steps))
	copy(stepsCopy, ac.Steps)
	
	return &AttackChain{
		ID:            ac.ID,
		Name:          ac.Name,
		Description:   ac.Description,
		Steps:         stepsCopy,
		StartPhase:    ac.StartPhase,
		EndPhase:      ac.EndPhase,
		TotalDuration: ac.TotalDuration,
		RiskLevel:     ac.RiskLevel,
		Status:        ac.Status,
	}
}

// ============================================================================
// Supporting Structures
// ============================================================================

// AttackChain represents a sequence of attack steps
type AttackChain struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	Steps          []*AttackStep `json:"steps"`
	StartPhase     string        `json:"start_phase"`
	EndPhase       string        `json:"end_phase"`
	TotalDuration  time.Duration `json:"total_duration"`
	RiskLevel      RiskLevel     `json:"risk_level"`
	Status         ChainStatus   `json:"status"`
}

// AttackStep represents a single action in the attack chain
type AttackStep struct {
	ID                 string              `json:"id"`
	Type               StepType            `json:"step_type"`
	CVEID              string              `json:"cve_id,omitempty"`
	TechniqueID        string              `json:"technique_id,omitempty"`
	Phase              string              `json:"phase"`
	Prerequisite       *AttackStep         `json:"prerequisite,omitempty"`
	SuccessCondition   func(ExploitResult) bool `json:"-"`
	PostAction         func(AttackEnvironment) `json:"-"`
	RequiredPrivileges PrivilegeLevel      `json:"required_privileges"`
	EvasionTechniques  []EvasionTech       `json:"evasion_techniques"`
	DetectionLikelihood DetectionLevel      `json:"detection_likeliness"`
	RiskScore          float64             `json:"risk_score"`
	ExploitMetadata    *ExploitInfo        `json:"exploit_metadata,omitempty"`
}

// ExploitResult represents the outcome of an exploit execution
type ExploitResult struct {
	Success          bool                `json:"success"`
	Errored          bool                `json:"errored"`
	PhaseTransitioned bool               `json:"phase_transitioned"`
	ExitCode         int                 `json:"exit_code"`
	PayloadInstalled bool                `json:"payload_installed"`
	Metadata         map[string]any      `json:"metadata"`
}

// AttackEnvironment represents the target environment during execution
type AttackEnvironment struct {
	TargetOS         string
	TargetVersion    string
	NetworkSegment   string
	ProtectedByEDR   bool
	ActiveThreats    []string
	CurrentPrivileges PrivilegeLevel
}

// ScoredAttackChain wraps a chain with its scoring metrics
type ScoredAttackChain struct {
	Path      *AttackChain `json:"path"`
	Score     float64      `json:"score"`
	Rationale string       `json:"rationale"`
}

// AttackChainResult contains the final attack chain analysis
type AttackChainResult struct {
	Path               *AttackChain `json:"path"`
	Score              float64      `json:"score"`
	Rationale          string       `json:"rationale"`
	EstimatedTime      time.Duration `json:"estimated_time"`
	DetectionRisk      float64      `json:"detection_risk"`
	ExploitReliability float64      `json:"exploit_reliability"`
}

// Constants
const (
	maxPathLength         = 10
	totalKillChainPhases  = 11
)

type ChainStatus string
const (
	StatusReady     ChainStatus = "ready"
	StatusRunning   ChainStatus = "running"
	StatusCompleted ChainStatus = "completed"
	StatusFailed    ChainStatus = "failed"
)

type RiskLevel string
const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type StepType string
const (
	StepCVEExploit         StepType = "cve_exploit"
	StepPrivilegeEscalation StepType = "privilege_escalation"
	StepLateralMovement    StepType = "lateral_movement"
	StepMITRETechnique     StepType = "mitre_technique"
)

type PrivilegeLevel int
const (
	PrivilegeLevelNone PrivilegeLevel = iota
	PrivilegeLevelUser
	PrivilegeLevelSystem
)

type EvasionTech string
const (
	EvasionBasicEvasion               EvasionTech = "basic_evasion"
	E evasionObfuscatePayload         EvasionTech = "obfuscate_payload"
	E evasionUseStagedPayload         EvasionTech = "use_staged_payload"
	E evasionEncodeDataStreams        E evasionTech = "encode_data_streams"
	E evasionDisableAntivirus         E evasionTech = "disable_antivirus"
	E evasionBypassAMERestrictions    E evasionTech = "bypass_amerestrictions"
	E evasionProcessInjection         E evasionTech = "process_injection"
	E evasionLivingOffTheLand         E evasionTech = "living_off_the_land"
	E evasionSignedBinaryProxyExecution E evasionTech = "signed_binary_proxy_execution"
	E evasionPassTheHash              E evasionTech = "pass_the_hash"
)

type DetectionLevel float64
const (
	DetectionVeryLow DetectionLevel = 0.1
	DetectionLow     DetectionLevel = 0.2
	DetectionMedium  DetectionLevel = 0.5
	DetectionHigh    DetectionLevel = 0.8
	DetectionVeryHigh DetectionLevel = 1.0
)
