package matcher

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// WeaponsMatcher implements multi-factor scoring for exploit recommendation
type WeaponsMatcher struct {
	logger       *logrus.Logger
	catalog      *ExploitCatalog
	scoringModel *ScoringModel
}

// TargetProfile describes the target environment for exploit matching
type TargetProfile struct {
	Platforms            []string
	VulnerabilityTypes   []string
	NetworkIsolation     bool
	DataSensitivity      string // High, Medium, Low
	PatchLevel           string // FullyPatched, PartiallyPatched, Unpatched
	BusinessCriticality  int    // 1-10 scale
}

// WeaponMatch represents a matched exploit with scoring details
type WeaponMatch struct {
	Exploit        *CVEEnrichment
	TotalScore     float64
	ScoreBreakdown ScoreBreakdown
	RiskLevel      string // Critical, High, Medium, Low
	Recommendation string
}

// ScoreBreakdown contains individual factor scores
type ScoreBreakdown struct {
	CVSSScore        float64
	MaturityBonus    float64
	PlatformMatch    float64
	TypeMatch        float64
	ContextRelevance float64
	TotalScore       float64
	LastUpdated      time.Time
}

// ScoringModel defines weight configuration
type ScoringModel struct {
	Threshold float64
	Weights   ScoringWeights
}

type ScoringWeights struct {
	CVSS    float64
	Maturity float64
	Platform float64
	Type     float64
	Context  float64
}

// DefaultScoringModel returns standard weighting configuration
func DefaultScoringModel() *ScoringModel {
	return &ScoringModel{
		Threshold: 40.0,
		Weights: ScoringWeights{
			CVSS:     0.30,
			Maturity: 0.25,
			Platform: 0.20,
			Type:     0.15,
			Context:  0.10,
		},
	}
}

// NewWeaponsMatcher creates a new matcher with configurable scoring weights
func NewWeaponsMatcher(logger *logrus.Logger, catalog *ExploitCatalog) *WeaponsMatcher {
	return &WeaponsMatcher{
		logger:       logger,
		catalog:      catalog,
		scoringModel: DefaultScoringModel(),
	}
}

// MatchTargets finds the best exploits for a given target profile
func (wm *WeaponsMatcher) MatchTargets(profile *TargetProfile, topN int) ([]*WeaponMatch, error) {
	allExploits := wm.catalog.GetAll()
	matches := make([]*WeaponMatch, 0, len(allExploits))

	for _, exploit := range allExploits {
		score := wm.calculateScore(&exploit, profile)

		if score.TotalScore > wm.scoringModel.Threshold {
			match := &WeaponMatch{
				Exploit:        &exploit,
				TotalScore:     score.TotalScore,
				ScoreBreakdown: score,
				RiskLevel:      wm.classifyRisk(score.TotalScore),
				Recommendation: wm.generateRecommendation(&exploit, score),
			}
			// Set last updated timestamp
			score.LastUpdated = time.Now()
			matches = append(matches, match)
		}
	}

	// Sort by total score descending
	wm.sortMatches(matches)

	if len(matches) > topN {
		matches = matches[:topN]
	}

	return matches, nil
}

// calculateScore computes a weighted score for an exploit against a target profile
func (wm *WeaponsMatcher) calculateScore(exploit *CVEEnrichment, profile *TargetProfile) ScoreBreakdown {
	breakdown := ScoreBreakdown{}

	// CVSS Base Score (weight: 30%)
	breakdown.CVSSScore = wm.scoreCVSS(exploit) * wm.scoringModel.Weights.CVSS

	// Maturity Level Bonus (weight: 25%)
	breakdown.MaturityBonus = wm.scoreMaturity(exploit) * wm.scoringModel.Weights.Maturity

	// Platform Match (weight: 20%)
	breakdown.PlatformMatch = wm.scorePlatformMatch(exploit, profile) * wm.scoringModel.Weights.Platform

	// Vulnerability Type Match (weight: 15%)
	breakdown.TypeMatch = wm.scoreTypeMatch(exploit, profile) * wm.scoringModel.Weights.Type

	// Context Relevance (weight: 10%)
	breakdown.ContextRelevance = wm.scoreContextRelevance(exploit, profile) * wm.scoringModel.Weights.Context

	// Normalize to 0-100 scale
	totalScore := 0.0
	factors := []float64{
		breakdown.CVSSScore,
		breakdown.MaturityBonus,
		breakdown.PlatformMatch,
		breakdown.TypeMatch,
		breakdown.ContextRelevance,
	}
	for _, f := range factors {
		totalScore += f
	}

	breakdown.TotalScore = totalScore
	return breakdown
}

// Individual scoring methods
func (wm *WeaponsMatcher) scoreCVSS(exploit *CVEEnrichment) float64 {
	// Scale CVSS score (0-10) to 0-100
	return exploit.CVSSBase * 10.0
}

func (wm *WeaponsMatcher) scoreMaturity(exploit *CVEEnrichment) float64 {
	maturityScores := map[string]float64{
		"Weaponized":         100.0,
		"Functional":         85.0,
		"Proof-of-Concept":   70.0,
		"Theoretical":        40.0,
	}

	if score, ok := maturityScores[exploit.Maturity]; ok {
		return score
	}
	return 50.0 // Default for unknown maturity levels
}

func (wm *WeaponsMatcher) scorePlatformMatch(exploit *CVEEnrichment, profile *TargetProfile) float64 {
	if len(profile.Platforms) == 0 {
		return 50.0 // Neutral if no platform constraints
	}

	matches := 0
	for _, exploitPlatform := range exploit.Platforms {
		for _, targetPlatform := range profile.Platforms {
			if strings.EqualFold(exploitPlatform, targetPlatform) {
				matches++
				break
			}
		}
	}

	ratio := float64(matches) / float64(len(profile.Platforms))
	return ratio * 100.0
}

func (wm *WeaponsMatcher) scoreTypeMatch(exploit *CVEEnrichment, profile *TargetProfile) float64 {
	if len(profile.VulnerabilityTypes) == 0 {
		return 50.0 // Neutral if no type constraints
	}

	matches := 0
	for _, targetType := range profile.VulnerabilityTypes {
		if strings.EqualFold(exploit.VulnType, targetType) {
			matches++
			break
		}
	}

	ratio := float64(matches) / float64(len(profile.VulnerabilityTypes))
	return ratio * 100.0
}

func (wm *WeaponsMatcher) scoreContextRelevance(exploit *CVEEnrichment, profile *TargetProfile) float64 {
	score := 50.0 // Start neutral

	// Data sensitivity bonus
	if profile.DataSensitivity == "High" && exploit.RiskRating == "Critical" {
		score += 15.0
	}

	// Business criticality multiplier
	critMultiplier := float64(profile.BusinessCriticality) / 10.0
	score *= critMultiplier

	// Network isolation penalty
	if profile.NetworkIsolation && !wm.isInternalExploit(exploit) {
		score -= 20.0
	}

	return math.Max(0, math.Min(100, score))
}

// Helper functions
func (wm *WeaponsMatcher) classifyRisk(score float64) string {
	switch {
	case score >= 90:
		return "CRITICAL"
	case score >= 75:
		return "HIGH"
	case score >= 50:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

func (wm *WeaponsMatcher) generateRecommendation(exploit *CVEEnrichment, breakdown ScoreBreakdown) string {
	recommendations := []string{}

	if breakdown.MaturityBonus > 80 {
		recommendations = append(recommendations, "High-confidence exploit with proven functionality")
	}

	if breakdown.CVSSScore > 80 {
		recommendations = append(recommendations, "Severe impact potential based on CVSS metrics")
	}

	if len(exploit.References) > 0 {
		recommendations = append(recommendations, "Multiple detection rules available")
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "Moderate risk exploit - verify before use")
	}

	return strings.Join(recommendations, "; ")
}

func (wm *WeaponsMatcher) isInternalExploit(exploit *CVEEnrichment) bool {
	// Heuristic: exploits targeting internal systems only
	internalKeywords := []string{"internal", "intranet", "localhost", "127.0.0.1"}
	for _, keyword := range internalKeywords {
		if strings.Contains(strings.ToLower(exploit.Description), keyword) {
			return true
		}
	}
	return false
}

func (wm *WeaponsMatcher) sortMatches(matches []*WeaponMatch) {
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].TotalScore > matches[j].TotalScore
	})
}

func (wm *WeaponsMatcher) applyTemporalDecay(breakdown ScoreBreakdown) float64 {
	// Apply slight temporal decay based on exploit age
	exploitAge := time.Since(breakdown.LastUpdated).Hours()
	decayFactor := 1.0 - (0.1*math.Min(exploitAge/(24*365), 0.3)) // Max 30% decay after 1 year

	return breakdown.TotalScore * decayFactor
}
