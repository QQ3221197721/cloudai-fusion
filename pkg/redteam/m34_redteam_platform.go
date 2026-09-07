// Package redteam implements the unified M34 Red Team Security Platform integrating all three OBCE3 patents.
// This platform provides end-to-end vulnerability assessment by combining:
//   - Patent #1: Self-Evolving Attack Graph Engine for optimal attack path discovery
//   - Patent #2: Quantum-Resistant Vulnerability Predictor using lattice-based cryptography
//   - Patent #3: Adversarial ML Defense System with GAN-driven augmentation
//
// The integration creates a cohesive security assessment tool that exceeds the sum of individual parts.
package redteam

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/patent"
)

// TargetInfo represents the target system being assessed
type TargetInfo struct {
	IP       string     `json:"ip"`
	Hostname string     `json:"hostname,omitempty"`
	Ports    []int      `json:"ports"`
	Services []Service  `json:"services,omitempty"`
	KnownCVEs []string  `json:"known_cves,omitempty"`
}

// Service represents a network service running on target
type Service struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Port    int    `json:"port"`
}

// AssessmentResult contains the complete vulnerability assessment output
type AssessmentResult struct {
	AttacksDiscovered       int               `json:"attacks_discovered"`
	CriticalVulnerabilities int               `json:"critical_vulnerabilities"`
	TimeToExploit           time.Duration     `json:"time_to_exploit"`
	ConfidenceScore         float64           `json:"confidence_score"` // 0-1 scale
	Recommendations         []Recommendation  `json:"recommendations"`
	AttackPaths             []OrchestratedPath `json:"attack_paths,omitempty"`
}

// Recommendation provides actionable remediation guidance
type Recommendation struct {
	Priority      string            `json:"priority"` // critical, high, medium, low
	CVE           string            `json:"cve,omitempty"`
	Description   string            `json:"description"`
	Mitigation    string            `json:"mitigation"`
	RiskReduction float64           `json:"risk_reduction"` // Percentage reduction
	Impact        RecommendationImpact `json:"impact"`
}

// RecommendationImpact quantifies the business impact of vulnerability
type RecommendationImpact struct {
	Confidentiality int // 0-3 scale
	Integrity       int // 0-3 scale
	Availability    int // 0-3 scale
}

// ============================================================================
// M34 RED TEAM PLATFORM - UNIFIED ORCHESTRATION LAYER
// ============================================================================

// M34RedTeamPlatform integrates all three OBCE3 patents into production-ready security platform
type M34RedTeamPlatform struct {
	mu sync.RWMutex
	
	// Patent #1: Self-Evolving Attack Graph Engine
	attackGraphEngine  *patent.QLearningAgent
	
	// Patent #2: Quantum-Resistant Vulnerability Predictor  
	vulnPredictor *patent.QuantumResistantPredictor
	
	// Patent #3: Adversarial ML Defense System
	adversarialDefense *patent.AdversarialMLDefenseSystem
	
	// Cross-patent coordination components
	attackChainOrchestrator *AttackChainOrchestrator
	vulnerabilityAssessor   *VulnerabilityAssessor
	defenseEvaluator        *DefenseEvaluator
	
	// Infrastructure
	logger *logrus.Logger
	ctx    context.Context
	cancel context.CancelFunc
	
	// Performance metrics
	stats *PlatformStats
}

// PlatformStats tracks operational metrics
type PlatformStats struct {
	mu                sync.RWMutex
	assessmentsRun    int
	totalLatencyMs    int64
	criticalFinds     int
	averageConfidence float64
}

// NewM34RedTeamPlatform creates unified platform instance with all three patents
func NewM34RedTeamPlatform(ctx context.Context, logger *logrus.Logger) (*M34RedTeamPlatform, error) {
	if logger == nil {
		logger = logrus.New()
		logger.SetLevel(logrus.WarnLevel)
	}
	
	// Create cancellation context
	innerCtx, cancel := context.WithCancel(ctx)
	
	platform := &M34RedTeamPlatform{
		logger: logger.WithField("component", "m34-platform"),
		ctx:    innerCtx,
		cancel: cancel,
		stats:  &PlatformStats{},
	}
	
	// Initialize Patent #1: Self-Evolving Attack Graph Engine
	agLogger := logger.WithField("patent", "self-evolving-graph")
	agEngine := patent.NewQLearningAgent()
	platform.attackGraphEngine = agEngine
	
	// Initialize Patent #2: Quantum-Resistant Vulnerability Predictor
	qpLogger := logger.WithField("patent", "quantum-resistant-predictor")
	qpPredictor, err := patent.NewQuantumResistantPredictor(qpLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize quantum predictor: %w", err)
	}
	platform.vulnPredictor = qpPredictor
	
	// Initialize Patent #3: Adversarial ML Defense System
	adLogger := logger.WithField("patent", "adversarial-ml-defense")
	adDefense, err := patent.NewAdversarialMLDefenseSystem(adLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize adversarial defense: %w", err)
	}
	platform.adversarialDefense = adDefense
	
	// Initialize cross-patent coordinators
	platform.attackChainOrchestrator = NewAttackChainOrchestrator(
		platform.attackGraphEngine,
		platform.vulnPredictor,
		platform.adversarialDefense,
	)
	platform.vulnerabilityAssessor = NewVulnerabilityAssessor(platform.logger)
	platform.defenseEvaluator = NewDefenseEvaluator(platform.logger)
	
	platform.logger.Info("✅ M34 Red Team Platform initialized with all three OBCE3 patents")
	
	return platform, nil
}

// AssessVulnerabilities runs comprehensive end-to-end assessment using all three patents
func (p *M34RedTeamPlatform) AssessVulnerabilities(target TargetInfo, ctx context.Context) (AssessmentResult, error) {
	startTime := time.Now()
	p.mu.Lock()
	p.stats.assessmentsRun++
	p.mu.Unlock()
	
	defer func() {
		elapsed := time.Since(startTime)
		p.mu.Lock()
		p.stats.totalLatencyMs += elapsed.Milliseconds()
		p.mu.Unlock()
	}()
	
	// Step 1: Use Patent #1 for attack path discovery and optimization
	p.logger.WithFields(logrus.Fields{
		"target_ip": target.IP,
		"ports":     len(target.Ports),
	}).Debug("Starting attack path discovery (Patent #1)")
	
	attackPaths := p.attackGraphEngine.DiscoverOptimizedPaths(ctx)
	
	if len(attackPaths) == 0 {
		p.logger.Warn("No attack paths discovered")
		return AssessmentResult{
			AttacksDiscovered: 0,
			ConfidenceScore:   0.0,
		}, nil
	}
	
	// Step 2: Use Patent #2 for quantum-resistant vulnerability prediction
	p.logger.WithField("paths_count", len(attackPaths)).Debug("Running quantum threat analysis (Patent #2)")
	
	quantumThreatMatrix := p.vulnPredictor.AssessThreats(attackPaths, ctx)
	
	// Step 3: Use Patent #3 for defensive capability evaluation
	p.logger.Debug("Evaluating adversarial defenses (Patent #3)")
	
	defenseReport := p.adversarialDefense.EvaluateDefenses(target, attackPaths, quantumThreatMatrix)
	
	// Step 4: Cross-patent synthesis and orchestration
	p.logger.Debug("Orchestrating multi-patent results")
	
	result := p.attackChainOrchestrator.Orchestrate(attackPaths, quantumThreatMatrix, defenseReport)
	
	elapsed := time.Since(startTime)
	
	// Update stats
	p.mu.Lock()
	if len(result.CriticalPaths) > 0 {
		p.stats.criticalFinds += len(result.CriticalPaths)
	}
	p.averageConfidence = (p.averageConfidence*float64(p.stats.assessmentsRun-1) + result.ConfidenceScore) / float64(p.stats.assessmentsRun)
	p.mu.Unlock()
	
	return AssessmentResult{
		AttacksDiscovered:       len(attackPaths),
		CriticalVulnerabilities: len(result.CriticalPaths),
		TimeToExploit:           elapsed,
		ConfidenceScore:         result.ConfidenceScore,
		Recommendations:         p.generateRecommendations(result),
		AttackPaths:             result.Paths,
	}, nil
}

// generateRecommendations converts orchestrated results into actionable remediation guidance
func (p *M34RedTeamPlatform) generateRecommendations(result OrchestratedResult) []Recommendation {
	recommendations := make([]Recommendation, 0, len(result.Recommendations))
	
	for i, rec := range result.Recommendations {
		// Deduplicate recommendations based on CVE
		isDuplicate := false
		for j := 0; j < i; j++ {
			if recommendations[j].CVE == rec.CVE {
				isDuplicate = true
				break
			}
		}
		if isDuplicate {
			continue
		}
		
		recPriority := convertPriority(rec.Priority)
		recImpact := convertImpact(rec.Impact)
		
		recommendations = append(recommendations, Recommendation{
			Priority:      recPriority,
			CVE:           rec.CVE,
			Description:   rec.Description,
			Mitigation:    rec.Mitigation,
			RiskReduction: rec.RiskReduction,
			Impact:        recImpact,
		})
	}
	
	return recommendations
}

// GetStats returns current platform operational statistics
func (p *M34RedTeamPlatform) GetStats() PlatformStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	return PlatformStats{
		assessmentsRun:    p.stats.assessmentsRun,
		totalLatencyMs:    p.stats.totalLatencyMs,
		criticalFinds:     p.stats.criticalFinds,
		averageConfidence: p.stats.averageConfidence,
	}
}

// Stop gracefully shuts down the platform
func (p *M34RedTeamPlatform) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.cancel()
	p.logger.Info("M34 Red Team Platform stopped")
}

// convertPriority maps priority enum to string
func convertPriority(priority patent.Priority) string {
	switch priority {
	case patent.PriorityCritical:
		return "critical"
	case patent.PriorityHigh:
		return "high"
	case patent.PriorityMedium:
		return "medium"
	case patent.PriorityLow:
		return "low"
	default:
		return "unknown"
	}
}

// convertImpact converts patent recommendation impact to platform impact
func convertImpact(pi patent.RecommendationImpact) RecommendationImpact {
	return RecommendationImpact{
		Confidentiality: pi.Confidentiality,
		Integrity:       pi.Integrity,
		Availability:    pi.Availability,
	}
}
