// Package redteam - Enhanced 10-factor scoring algorithm for vulnerability prioritization
package matcher

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// ============================================================================
// EXPLOIT CATALOG DATA STRUCTURES
// ============================================================================

// CVEEnrichment represents a CVE entry with full metadata
type CVEEnrichment struct {
	ID                  string    `json:"id"`
	Title               string    `json:"title"`
	Description         string    `json:"description"`
	CVE                 string    `json:"cve"`
	CWE                 string    `json:"cwe"`
	PublishedDate       time.Time `json:"published_date"`
	LastUpdated         time.Time `json:"last_updated"`
	VulnType            string    `json:"vuln_type"`
	AttackVector        string    `json:"attack_vector"` // Network/Adjacent/Local/Physical
	Authentication      string    `json:"authentication"` // None/Single/Multiple/Required
	Scope               string    `json:"scope"` // Unchanged/Changed
	ImpactTypes         []string  `json:"impact_type"`
	CVSSBase            float64   `json:"cvss_base"`
	CVSSVector          string    `json:"cvss_vectors"`
	RiskRating          string    `json:"risk_rating"`
	Exploitability      string    `json:"exploitability"`
	Maturity            string    `json:"maturity"`
	DetectionEvasion    []string  `json:"detection_evasion"`
	Platforms           []string  `json:"platforms"`
	Languages           []string  `json:"languages"`
	Dependencies        []string  `json:"dependencies"`
	MemoryMB            int       `json:"memory_mb"`
	CPUThreads          int       `json:"cpu_threads"`
	SuccessProbability  float64   `json:"success_probability"`
	EvidenceChain       []Evidence `json:"evidence_chain"`
	Reproducibility     string    `json:"reproducibility"`
	UseCases            []string    `json:"use_cases"`
	BestPractices       []string    `json:"best_practices"`
	RisksCaveats        []string    `json:"risks_caveats"`
	Tags                []string    `json:"tags"`
	Source              string    `json:"source"`
	Reliability         string    `json:"reliability"`
	Confidence          float64   `json:"confidence"`

	// NEW FACTORS FOR 10-FACTOR MODEL
	TemporalScore       float64   `json:"temporal_score"`             // CVSS Temporal Score
	VulnerabilityAge    float64   `json:"vulnerability_age"`          // Days since disclosure
	TargetPrevalence    float64   `json:"target_prevalence"`          // Target system market share
	DetectionAvoidance  float64   `json:"detection_avoidance"`        // Evasion capability score
	LateralMovement     float64   `json:"lateral_movement"`           // Ability to spread laterally
	PersistenceFeasible float64   `json:"persistence_feasible"`       // Persistence creation capability
	EaseOfExecution     float64   `json:"ease_of_execution"`          // Execution complexity (inverted)
}

// Evidence represents validation evidence chain item
type Evidence struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Date        time.Time `json:"date"`
	Source      string    `json:"source"`
	SuccessRate float64   `json:"success_rate"`
	Environment string    `json:"environment"`
	ValidationHash string `json:"validation_hash"`
}

// ExploitCatalog represents the enriched exploit database
type ExploitCatalog struct {
	Metadata   CatalogMetadata `json:"metadata"`
	Exploits   []CVEEnrichment `json:"exploits"`
}

// CatalogMetadata holds catalog statistics and configuration
type CatalogMetadata struct {
	GeneratedAt     string        `json:"generated_at"`
	Version         string        `json:"version"`
	TotalCount      int           `json:"total_count"`
	MinimumRequired int           `json:"minimum_required"`
	MeetsRequirement bool         `json:"meets_requirement"`
	Statistics      Statistics    `json:"statistics"`
}

// Statistics holds breakdown metrics
type Statistics struct {
	BySeverity   map[string]int    `json:"by_severity"`
	ByMaturity   map[string]int    `json:"by_maturity"`
	ByVulnType   map[string]int    `json:"by_vuln_type"`
	ByPlatform   map[string]int    `json:"by_platform,omitempty"`
	ByDomain     map[string]int    `json:"by_domain,omitempty"`
}

// ============================================================================
// 10-FACTOR WEIGHTED SCORING ENGINE
// ============================================================================

// TenFactorScorer implements weighted scoring using 10 factors
type TenFactorScorer struct {
	weights *ScoringWeights
}

// ScoringWeights defines the weight for each factor
type ScoringWeights struct {
	CVSSBaseWeight           float64 // 30%
	TemporalWeight           float64 // 10%
	ExploitabilityWeight     float64 // 8%
	ImpactWeight             float64 // 12%
	EvidenceWeight           float64 // 10%
	TrendWeight              float64 // 5%
	ContextualWeight         float64 // 5%
	VulnerabilityAgeWeight   float64 // 10%
	DetectionAvoidanceWeight float64 // 10%
}

const (
	DefaultCVSSWeight           = 0.30
	DefaultTemporalWeight       = 0.10
	DefaultExploitabilityWeight = 0.08
	DefaultImpactWeight         = 0.12
	DefaultEvidenceWeight       = 0.10
	DefaultTrendWeight          = 0.05
	DefaultContextualWeight     = 0.05
	DefaultVulnerabilityAgeWeight = 0.10
	DefaultDetectionAvoidanceWeight = 0.10
)

// NewTenFactorScorer creates a new scorer with optimal weights
func NewTenFactorScorer() *TenFactorScorer {
	return &TenFactorScorer{
		weights: &ScoringWeights{
			CVSSBaseWeight:           DefaultCVSSWeight,
			TemporalWeight:           DefaultTemporalWeight,
			ExploitabilityWeight:     DefaultExploitabilityWeight,
			ImpactWeight:             DefaultImpactWeight,
			EvidenceWeight:           DefaultEvidenceWeight,
			TrendWeight:              DefaultTrendWeight,
			ContextualWeight:         DefaultContextualWeight,
			VulnerabilityAgeWeight:   DefaultVulnerabilityAgeWeight,
			DetectionAvoidanceWeight: DefaultDetectionAvoidanceWeight,
		},
	}
}

// CalculateEnhancedScore computes a comprehensive threat score using 10 factors
func (t *TenFactorScorer) CalculateEnhancedScore(cve *CVEEnrichment) (float64, *ScoreBreakdown) {
	cvssScore := t.calculateCVSSComponent(cve)
	temporalScore := t.calculateTemporalComponent(cve)
	exploitabilityScore := t.calculateExploitabilityComponent(cve)
	impactScore := t.calculateImpactComponent(cve)
	evidenceScore := t.calculateEvidenceComponent(cve)
	trendScore := t.calculateTrendComponent(cve)
	contextualScore := t.calculateContextualComponent(cve)
	vulnerabilityAgeScore := t.calculateVulnerabilityAgeComponent(cve)
	detectionAvoidanceScore := t.calculateDetectionAvoidanceComponent(cve)

	totalScore := cvssScore*weights.CVSSBaseWeight +
		temporalScore*t.weights.TemporalWeight +
		exploitabilityScore*t.weights.ExploitabilityWeight +
		impactScore*t.weights.ImpactWeight +
		evidenceScore*t.weights.EvidenceWeight +
		trendScore*t.weights.TrendWeight +
		contextualScore*t.weights.ContextualWeight +
		vulnerabilityAgeScore*t.weights.VulnerabilityAgeWeight +
		detectionAvoidanceScore*t.weights.DetectionAvoidanceWeight

	breakdown := &ScoreBreakdown{
		CVSSScore:                    cvssScore,
		TemporalScore:                temporalScore,
		ExploitabilityScore:          exploitabilityScore,
		ImpactScore:                  impactScore,
		EvidenceScore:                evidenceScore,
		TrendScore:                   trendScore,
		ContextualScore:              contextualScore,
		VulnerabilityAgeScore:        vulnerabilityAgeScore,
		DetectionAvoidanceScore:      detectionAvoidanceScore,
		TotalScore:                   totalScore,
	}

	return math.Min(100, totalScore), breakdown
}

// ScoreBreakdown contains individual component scores
type ScoreBreakdown struct {
	CVSSScore                    float64 `json:"cvss_score"`
	TemporalScore                float64 `json:"temporal_score"`
	ExploitabilityScore          float64 `json:"exploitability_score"`
	ImpactScore                  float64 `json:"impact_score"`
	EvidenceScore                float64 `json:"evidence_score"`
	TrendScore                   float64 `json:"trend_score"`
	ContextualScore              float64 `json:"contextual_score"`
	VulnerabilityAgeScore        float64 `json:"vulnerability_age_score"`
	DetectionAvoidanceScore      float64 `json:"detection_avoidance_score"`
	TotalScore                   float64 `json:"total_score"`
}

// calculateCVSSComponent calculates CVSS base score contribution (30%)
func (t *TenFactorScorer) calculateCVSSComponent(cve *CVEEnrichment) float64 {
	if cve.CVSSBase == 0 {
		return 0
	}
	// Normalize to 0-100 scale
	return cve.CVSSBase * 10
}

// calculateTemporalComponent calculates CVSS Temporal Score contribution (10%)
func (t *TenFactorScorer) calculateTemporalComponent(cve *CVEEnrichment) float64 {
	if cve.TemporalScore > 0 {
		return cve.TemporalScore * 10
	}

	// Derive from maturity if temporal not available
	maturityMultiplier := t.getTemporalMultiplier(cve.Maturity)
	return cve.CVSSBase * maturityMultiplier * 10
}

// getTemporalMultiplier maps maturity level to temporal multiplier
func (t *TenFactorScorer) getTemporalMultiplier(maturity string) float64 {
	switch strings.ToLower(maturity) {
	case "weaponized", "active-exploitation":
		return 0.9
	case "functional", "proof-of-concept":
		return 0.85
	case "theoretical":
		return 0.7
	default:
		return 0.8 // default uncertainty
	}
}

// calculateExploitabilityComponent calculates exploitability score (8%)
func (t *TenFactorScorer) calculateExploitabilityComponent(cve *CVEEnrichment) float64 {
	baseProb := cve.SuccessProbability
	if baseProb == 0 {
		baseProb = 0.5
	}
	
	// Boost for weaponized exploits
	if cve.Exploitability == "Weaponized" || cve.Exploitability == "Active-Exploitation" {
		baseProb = math.Max(baseProb, 0.9)
	} else if cve.Exploitability == "Proof-of-Concept" {
		baseProb = math.Max(baseProb, 0.7)
	}
	
	return baseProb * 10
}

// calculateImpactComponent calculates impact score (12%)
func (t *TenFactorScorer) calculateImpactComponent(cve *CVEEnrichment) float64 {
	impactCount := len(cve.ImpactTypes)
	if impactCount == 0 {
		impactCount = 1
	}

	// Weight confidentiality/integrity/availability equally
	var impactScore float64
	for _, it := range cve.ImpactTypes {
		switch strings.ToUpper(it) {
		case "CONFIDENTIALITY":
			impactScore += 4
		case "INTEGRITY":
			impactScore += 4
		case "AVAILABILITY":
			impactScore += 3
		}
	}

	return math.Min(10, impactScore)
}

// calculateEvidenceComponent calculates evidence validation score (10%)
func (t *TenFactorScorer) calculateEvidenceComponent(cve *CVEEnrichment) float64 {
	if len(cve.EvidenceChain) == 0 {
		return 5 // neutral baseline
	}

	var totalScore float64
	for _, ev := range cve.EvidenceChain {
		totalScore += ev.SuccessRate
	}

	avgScore := totalScore / float64(len(cve.EvidenceChain))
	return avgScore
}

// calculateTrendComponent calculates trend momentum score (5%)
func (t *TenFactorScorer) calculateTrendComponent(cve *CVEEnrichment) float64 {
	// Count recent activity indicators
	activeSignals := 0
	
	if cve.Exploitability == "Active-Exploitation" {
		activeSignals++
	}
	if cve.Reliability == "Verified" {
		activeSignals++
	}
	if len(cve.Tags) > 0 && strings.Contains(strings.Join(cve.Tags, ""), "critical") {
		activeSignals++
	}

	return float64(activeSignals) * 3.33
}

// calculateContextualComponent calculates context-specific score (5%)
func (t *TenFactorScorer) calculateContextualComponent(cve *CVEEnrichment) float64 {
	contextBoost := 0.0

	// Check for targeted environments
	for _, platform := range cve.Platforms {
		if strings.EqualFold(platform, "windows") || strings.EqualFold(platform, "linux") {
			contextBoost += 2
		}
	}

	// Check target prevalence
	if cve.TargetPrevalence > 0.7 {
		contextBoost += 3
	}

	return math.Min(10, contextBoost)
}

// calculateVulnerabilityAgeComponent calculates vulnerability age score (10%)
func (t *TenFactorScorer) calculateVulnerabilityAgeComponent(cve *CVEEnrichment) float64 {
	now := time.Now()
	daysSinceDisclosure := now.Sub(cve.PublishedDate).Hours() / 24

	// Recent vulnerabilities are more dangerous (patch not yet widespread)
	var ageScore float64
	if daysSinceDisclosure < 30 {
		ageScore = 10 // brand new
	} else if daysSinceDisclosure < 90 {
		ageScore = 8
	} else if daysSinceDisclosure < 180 {
		ageScore = 6
	} else if daysSinceDisclosure < 365 {
		ageScore = 4
	} else {
		ageScore = 2 // older, likely patched
	}

	return ageScore
}

// calculateDetectionAvoidanceComponent calculates evasion score (10%)
func (t *TenFactorScorer) calculateDetectionAvoidanceComponent(cve *CVEEnrichment) float64 {
	// Score based on anti-detection capabilities
	evasionFactors := 0
	
	evasionTerms := []string{"bypass", "anti-virus", "edr", "detection", "signature", 
		"sandstone", "defensive", "obfuscation", "stealth", "undetectable"}
	
	combinedTags := strings.Join(append(cve.Tags, cve.DetectionEvasion...), " ")
	lowerTags := strings.ToLower(combinedTags)
	
	for _, term := range evasionTerms {
		if strings.Contains(lowerTags, term) {
			evasionFactors++
		}
	}

	// Check platforms for native capabilities
	for _, platform := range cve.Platforms {
		if strings.Contains(strings.ToLower(platform), "windows") {
			if cve.Exploitability == "Weaponized" || cve.Exploitability == "Active-Exploitation" {
				evasionFactors++
			}
		}
	}

	minEvade := math.Max(float64(1), float64(evasionFactors))
	return math.Min(10, minEvade*2.5)
}

// GetScoreBreakdown returns detailed analysis of why an exploit scored high
func (t *TenFactorScorer) GetScoreBreakdown(cve *CVEEnrichment) string {
	score, breakdown := t.CalculateEnhancedScore(cve)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Overall Threat Score: %.1f/100\n", score))
	sb.WriteString(fmt.Sprintf("CVSS Base: %.1f × %.2f = %.1f\n", 
		breakdown.CVSSScore, t.weights.CVSSBaseWeight, breakdown.CVSSScore*t.weights.CVSSBaseWeight))
	sb.WriteString(fmt.Sprintf("Temporal: %.1f × %.2f = %.1f\n",
		breakdown.TemporalScore, t.weights.TemporalWeight, breakdown.TemporalScore*t.weights.TemporalWeight))
	sb.WriteString(fmt.Sprintf("Exploitability: %.1f × %.2f = %.1f\n",
		breakdown.ExploitabilityScore, t.weights.ExploitabilityWeight, breakdown.ExploitabilityScore*t.weights.ExploitabilityWeight))
	sb.WriteString(fmt.Sprintf("Impact: %.1f × %.2f = %.1f\n",
		breakdown.ImpactScore, t.weights.ImpactWeight, breakdown.ImpactScore*t.weights.ImpactWeight))
	sb.WriteString(fmt.Sprintf("Evidence: %.1f × %.2f = %.1f\n",
		breakdown.EvidenceScore, t.weights.EvidenceWeight, breakdown.EvidenceScore*t.weights.EvidenceWeight))
	sb.WriteString(fmt.Sprintf("Trend: %.1f × %.2f = %.1f\n",
		breakdown.TrendScore, t.weights.TrendWeight, breakdown.TrendScore*t.weights.TrendWeight))
	sb.WriteString(fmt.Sprintf("Contextual: %.1f × %.2f = %.1f\n",
		breakdown.ContextualScore, t.weights.ContextualWeight, breakdown.ContextualScore*t.weights.ContextualWeight))
	sb.WriteString(fmt.Sprintf("Vulnerability Age: %.1f × %.2f = %.1f\n",
		breakdown.VulnerabilityAgeScore, t.weights.VulnerabilityAgeWeight, breakdown.VulnerabilityAgeScore*t.weights.VulnerabilityAgeWeight))
	sb.WriteString(fmt.Sprintf("Detection Avoidance: %.1f × %.2f = %.1f\n",
		breakdown.DetectionAvoidanceScore, t.weights.DetectionAvoidanceWeight, breakdown.DetectionAvoidanceScore*t.weights.DetectionAvoidanceWeight))

	return sb.String()
}

// LoadExploitCatalog loads and parses the exploit database
func LoadExploitCatalog(path string) (*ExploitCatalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read catalog file: %w", err)
	}

	catalog := &ExploitCatalog{}
	if err := json.Unmarshal(data, catalog); err != nil {
		return nil, fmt.Errorf("failed to parse catalog JSON: %w", err)
	}

	return catalog, nil
}

// SaveExploitCatalog writes the catalog to disk
func SaveExploitCatalog(catalog *ExploitCatalog, path string) error {
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal catalog: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write catalog file: %w", err)
	}

	return nil
}

// FilterByDomain filters exploits by target domain
func (c *ExploitCatalog) FilterByDomain(domain string) []CVEEnrichment {
	var results []CVEEnrichment
	domainLower := strings.ToLower(domain)

	for _, exploit := range c.Exploits {
		// Check tags and descriptions
		combined := strings.ToLower(exploit.Description + " " + strings.Join(exploit.Tags, " "))
		if strings.Contains(combined, domainLower) {
			results = append(results, exploit)
		}
	}

	return results
}

// RankByScore sorts exploits by threat score
func (c *ExploitCatalog) RankByScore(scorer *TenFactorScorer) []RankedExploit {
	ranked := make([]RankedExploit, len(c.Exploits))

	for i, exploit := range c.Exploits {
		score, _ := scorer.CalculateEnhancedScore(&exploit)
		ranked[i] = RankedExploit{
			CVE:       exploit,
			Score:     score,
			Rank:      i + 1,
		}
	}

	// Sort descending by score
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})

	// Update ranks after sorting
	for i := range ranked {
		ranked[i].Rank = i + 1
	}

	return ranked
}

// ============================================================================
// SCORING METADATA EXTRACTION UTILITIES
// ============================================================================

// ExtractCVSSTemporalScore derives temporal score from CVSS vector string
func ExtractCVSSTemporalScore(vectorStr string) float64 {
	// Parse from CVSS vector format: AV:N/AC:L/PR:L/UI:N/S:C/C:H/I:H/A:H/E:P
	// E=X=E (Experimental), F(F)=Functional, P(P)=Proof-of-Concept, T(T)=Theoretical, H(H)=Verified
	
	elementMapping := map[string]string{
		"E": "exploitability",
		"RL": "remediation-level",
		"RC": "report-confidence",
	}
	
	var temporalMultiplier float64 = 0.7 // default uncertainty
	
	for key, field := range elementMapping {
		if strings.Contains(vectorStr, key+":") {
			value := getFieldFromVector(vectorStr, key)
			switch strings.ToLower(value) {
			case "p", "proof-of-concept":
				temporalMultiplier = 0.85
			case "f", "functional":
				temporalMultiplier = 0.9
			case "v", "verified":
				temporalMultiplier = 0.95
			case "t", "theoretical":
				temporalMultiplier = 0.6
			default:
				temporalMultiplier = 0.8 // unknown
			}
		}
	}
	
	return temporalMultiplier * 10
}

// ExtractTargetPrevalence calculates target system market share
func ExtractTargetPrevalence(platforms []string) float64 {
	weightMap := map[string]float64{
		"windows":    0.30,
		"linux":      0.25,
		"macos":      0.08,
		"kubernetes": 0.20,
		"docker":     0.12,
		"iot":        0.05,
		"embedded":   0.03,
	}
	
	totalPrevalence := 0.0
	platformLowerCount := make(map[string]int)
	
	for _, platform := range platforms {
		lowerPlatform := strings.ToLower(platform)
		
		// Check for exact matches first
		if weight, ok := weightMap[lowerPlatform]; ok {
			totalPrevalence += weight
			continue
		}
		
		// Check for substring matches
		for knownPlatform, weight := range weightMap {
			if strings.Contains(lowerPlatform, knownPlatform) {
				platformLowerCount[knownPlatform]++
				totalPrevalence += weight
				break
			}
		}
	}
	
	return math.Min(1.0, totalPrevalence)
}

// ExtractLateralMovementCapability evaluates spread potential
func ExtractLateralMovementCapability(cve *CVEEnrichment) float64 {
	score := 0.0
	
	// Check authentication bypasses
	if strings.Contains(strings.ToLower(cve.VulnType), "authentication") ||
		strings.Contains(strings.ToLower(cve.VulnType), "bypass") {
		score += 3.0
	}
	
	// Check network access vector
	if cve.AttackVector == "Network" {
		score += 2.0
	}
	
	// Check for Windows AD context
	for _, platform := range cve.Platforms {
		if strings.Contains(strings.ToLower(platform), "windows") ||
			strings.Contains(strings.ToLower(platform), "active directory") {
			score += 2.0
			break
		}
	}
	
	// Check exploit maturity (weaponized = more likely to spread)
	if cve.Maturity == "Weaponized" || cve.Maturity == "Active-Exploitation" {
		score += 2.0
	}
	
	// Check dependencies (can indicate supply chain)
	if len(cve.Dependencies) > 0 {
		score += 1.0
	}
	
	return math.Min(10.0, score)
}

// ExtractPersistenceCreationFeasibility assesses persistence capability
func ExtractPersistenceCreationFeasibility(cve *CVEEnrichment) float64 {
	score := 0.0
	
	// Check vulnerability type
	vulnType := strings.ToLower(cve.VulnType)
	if strings.Contains(vulnType, "privilege") || strings.Contains(vulnType, "escalation") {
		score += 3.0
	}
	
	if strings.Contains(vulnType, "rce") || strings.Contains(vulnType, "code injection") {
		score += 2.5
	}
	
	// Check attack complexity
	if cve.Authentication == "None" && cve.AttackVector == "Network" {
		score += 2.0
	}
	
	// Check platforms for native persistence mechanisms
	for _, platform := range cve.Platforms {
		lowerPlatform := strings.ToLower(platform)
		if strings.Contains(lowerPlatform, "windows") {
			score += 1.5 // Registry, scheduled tasks, services
		} else if strings.Contains(lowerPlatform, "linux") {
			score += 1.0 // Crontab, systemd, init scripts
		}
	}
	
	// Check tags for persistence indicators
	combinedTags := strings.ToLower(strings.Join(cve.Tags, " "))
	if strings.Contains(combinedTags, "rootkit") ||
		strings.Contains(combinedTags, "backdoor") ||
		strings.Contains(combinedTags, "ptid") ||
		strings.Contains(combinedTags, "persistence") {
		score += 2.0
	}
	
	return math.Min(10.0, score)
}

// ExtractEaseOfExecution evaluates execution complexity (inverted - higher is easier)
func ExtractEaseOfExecution(cve *CVEEnrichment) float64 {
	score := 5.0 // neutral baseline
	
	// Lower accessibility complexity = easier execution
	if cve.Authentication == "None" {
		score += 2.0
	} else if cve.Authentication == "Single" {
		score += 1.0
	}
	
	// Simple attack vectors are easier
	vectorComplexity := map[string]float64{
		"Network":  2.0,
		"Adjacent": 1.0,
		"Local":    0.0,
		"Physical": -1.0,
	}
	
	if complexity, ok := vectorComplexity[cve.AttackVector]; ok {
		score += complexity
	}
	
	// Proof-of-concept exploits are generally easier to run than theoretical ones
	maturityEase := map[string]float64{
		"Weaponized":          2.0,
		"Active-Exploitation": 2.0,
		"Functional":          1.5,
		"Proof-of-Concept":    1.0,
		"Theoretical":         -1.0,
	}
	
	if ease, ok := maturityEase[cve.Maturity]; ok {
		score += ease
	}
	
	// Check detection evasion requirements (easier exploits often less sophisticated)
	if len(cve.DetectionEvasion) == 0 {
		score += 1.0 // no evasion needed
	}
	
	// Check if it requires special tools or capabilities
	if len(cve.Languages) <= 2 && len(cve.Dependencies) <= 1 {
		score += 1.0 // simple requirements
	}
	
	return math.Max(0, math.Min(10, score))
}

// CalculateVulnerabilityAge computes age since disclosure in days
func CalculateVulnerabilityAge(publishedDate time.Time) float64 {
	now := time.Now()
	daysSinceDisclosure := now.Sub(publishedDate).Hours() / 24
	return daysSinceDisclosure
}

// ExtractDetectionAvoidancePotential scores anti-detection capability
func ExtractDetectionAvoidancePotential(cve *CVEEnrichment) float64 {
	score := 0.0
	
	// Count evasion techniques mentioned
	evasionTerms := map[string]float64{
		"bypass":           1.5,
		"anti-virus":       2.0,
		"edr":              2.0,
		"antimalware":      1.5,
		"signature":        1.0,
		"obfuscation":      1.5,
		"stealth":          1.5,
		"undetectable":     2.0,
		"polymorphic":      2.5,
		"metamorphic":      2.5,
		"fileless":         2.0,
		"living-off-the-land": 2.0,
		"lolbin":           2.0,
		"sandbox-evasion":  2.0,
		"debugger-evasion": 1.5,
		"vm-detection":     1.5,
	}
	
	combinedText := strings.ToLower(cve.Description + " " + strings.Join(cve.Tags, " ") + " " + strings.Join(cve.DetectionEvasion, " "))
	
	for term, points := range evasionTerms {
		if strings.Contains(combinedText, term) {
			score += points
		}
	}
	
	// Bonus for weaponized/active exploits (typically mature enough to evade detection)
	if cve.Maturity == "Weaponized" || cve.Maturity == "Active-Exploitation" {
		score += 1.0
	}
	
	// Check if exploit targets multiple OS types (harder to detect)
	if len(cve.Platforms) >= 2 {
		score += 1.0
	}
	
	return math.Min(10.0, score)
}

// Helper function to extract field from CVSS vector
func getFieldFromVector(vector string, key string) string {
	prefix := key + ":"
	idx := strings.Index(vector, prefix)
	if idx == -1 {
		return "X" // default
	}
	
	startIndex := idx + len(prefix)
	endIndex := strings.IndexAny(vector[startIndex:], "/|")
	
	if endIndex == -1 {
		return vector[startIndex:]
	}
	
	return vector[startIndex : startIndex+endIndex]
}

// RankedExploit represents a scored and ranked exploit
type RankedExploit struct {
	CVE   CVEEnrichment `json:"cve"`
	Score float64       `json:"score"`
	Rank  int           `json:"rank"`
}
