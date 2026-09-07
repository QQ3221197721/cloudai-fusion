package auto_remediation

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Finding represents a single vulnerability finding
type Finding struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`           // e.g., "CWE-89", "SQL Injection"
	Severity    Severity  `json:"severity"`       // Low/Medium/High/Critical
	CVSSScore   float64   `json:"cvss_score,omitempty"`
	CVE         string    `json:"cve,omitempty"`
	Location    string    `json:"location"`         // File/Service/Component
	Evidence    string    `json:"evidence"`         // Proof/snapshot
	Timestamp   time.Time `json:"timestamp"`
	Source      string    `json:"source"`           // Scanner tool name
	MitreTactic string    `json:"mitre_tactic,omitempty"`
	MitreTechnique string  `json:"mitre_technique,omitempty"`
}

// FindingsAggregator groups and analyzes findings from multiple sources
type FindingsAggregator struct {
	mu            sync.RWMutex
	findings      []Finding
	grouped       map[GroupKey][]Finding
	cveIndex      map[string][]Finding
	typeIndex     map[string][]Finding
	lastRun       time.Time
	context       context.Context
}

// GroupKey defines how findings can be grouped
type GroupKey struct {
	CVE        string
	Type       string
	Severity   Severity
	Attachment AttachmentLevel
}

// AttachmentLevel defines integration points
type AttachmentLevel string

const (
	LevelNetwork  AttachmentLevel = "network"
	LevelApplication                 = "application"
	LevelSystem                      = "system"
	LevelData                        = "data"
)

// AggregationResult contains aggregated analysis
type AggregationResult struct {
	TotalFindings     int                     `json:"total_findings"`
	BySeverity        map[Severity]int        `json:"by_severity"`
	ByType           map[string]int           `json:"by_type"`
	ByAttachment     map[AttachmentLevel]int  `json:"by_attachment"`
	GroupedFindings  []GroupedFinding        `json:"grouped_findings"`
	CVERiskSummary   []CVERiskSummary        `json:"cve_risk_summary"`
	RiskScore        float64                  `json:"risk_score"`
	TopPriorities   []PrioritizedFinding     `json:"top_priorities"`
	GeneratedAt      time.Time                `json:"generated_at"`
}

// GroupedFinding represents a group of related findings
type GroupedFinding struct {
	Key         GroupKey              `json:"key"`
	Count       int                   `json:"count"`
	FindingIDs  []string              `json:"finding_ids"`
	Locations   []string              `json:"locations"`
	AvgCVSS     float64               `json:"avg_cvss"`
	Description string                `json:"description"`
	Sample      Finding                 `json:"sample"` // Representative example
}

// CVERiskSummary provides per-CVE risk analysis
type CVERiskSummary struct {
	CVE           string        `json:"cve"`
	Count         int           `json:"count"`
	MaxCVSS       float64       `json:"max_cvss"`
	AffectedAssets []string      `json:"affected_assets"`
	PriorityRank  int           `json:"priority_rank"`
}

// PrioritizedFinding represents top-priority remediation targets
type PrioritizedFinding struct {
	FindingID  string    `json:"finding_id"`
	CVE        string    `json:"cve,omitempty"`
	Reason     string    `json:"reason"`          // Why this is high priority
	RiskScore  float64   `json:"risk_score"`
	Action     string    `json:"action"`          // Recommended action
	Urgency    UrgencyLevel `json:"urgency"`
}

// NewFindingsAggregator creates a new aggregator
func NewFindingsAggregator(ctx context.Context) *FindingsAggregator {
	return &FindingsAggregator{
		findings:   make([]Finding, 0),
		grouped:    make(map[GroupKey][]Finding),
		cveIndex:   make(map[string][]Finding),
		typeIndex:  make(map[string][]Finding),
		context:    ctx,
	}
}

// AddFinding adds a finding to the aggregator
func (ag *FindingsAggregator) AddFinding(finding Finding) {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	
	finding.ID = fmt.Sprintf("F-%d", len(ag.findings)+1)
	finding.Timestamp = time.Now()
	
	ag.findings = append(ag.findings, finding)
	
	// Index by CVE
	if finding.CVE != "" {
		ag.cveIndex[finding.CVE] = append(ag.cveIndex[finding.CVE], finding)
	}
	
	// Index by type
	ag.typeIndex[finding.Type] = append(ag.typeIndex[finding.Type], finding)
}

// AddMultiple finds appends multiple findings at once
func (ag *FindingsAggregator) AddMultiple(findings []Finding) {
	for _, f := range findings {
		ag.AddFinding(f)
	}
}

// Clear clears all findings
func (ag *FindingsAggregator) Clear() {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	
	ag.findings = ag.findings[:0]
	ag.grouped = make(map[GroupKey][]Finding)
	ag.cveIndex = make(map[string][]Finding)
	ag.typeIndex = make(map[string][]Finding)
}

// Count returns total number of findings
func (ag *FindingsAggregator) Count() int {
	ag.mu.RLock()
	defer ag.mu.RUnlock()
	return len(ag.findings)
}

// GetFindings returns all findings
func (ag *FindingsAggregator) GetFindings() []Finding {
	ag.mu.RLock()
	defer ag.mu.RUnlock()
	result := make([]Finding, len(ag.findings))
	copy(result, ag.findings)
	return result
}

// GroupByCVE groups findings by CVE ID
func (ag *FindingsAggregator) GroupByCVE() map[string][]Finding {
	ag.mu.RLock()
	defer ag.mu.RUnlock()
	
	result := make(map[string][]Finding)
	for cve, findings := range ag.cveIndex {
		result[cve] = make([]Finding, len(findings))
		copy(result[cve], findings)
	}
	return result
}

// GroupByType groups findings by vulnerability type
func (ag *FindingsAggregator) GroupByType() map[string][]Finding {
	ag.mu.RLock()
	defer ag.mu.RUnlock()
	
	result := make(map[string][]Finding)
	for vtype, findings := range ag.typeIndex {
		result[vtype] = make([]Finding, len(findings))
		copy(result[vtype], findings)
	}
	return result
}

// Aggregate performs comprehensive analysis of findings
func (ag *FindingsAggregator) Aggregate(ctx context.Context) *AggregationResult {
	ag.mu.RLock()
	defer ag.mu.RUnlock()
	
	result := &AggregationResult{
		TotalFindings: len(ag.findings),
		BySeverity:    make(map[Severity]int),
		ByType:       make(map[string]int),
		ByAttachment: make(map[AttachmentLevel]int),
		GeneratedAt:  time.Now(),
	}
	
	// Count by severity/type/attachment
	for _, finding := range ag.findings {
		result.BySeverity[finding.Severity]++
		result.ByType[finding.Type]++
		
		// Infer attachment level from location
		level := ag.inferAttachmentLevel(finding.Location)
		result.ByAttachment[level]++
		
		// Group findings
		key := GroupKey{
			CVE:        finding.CVE,
			Type:       finding.Type,
			Severity:   finding.Severity,
			Attachment: level,
		}
		ag.grouped[key] = append(ag.grouped[key], finding)
	}
	
	// Build grouped findings
	for key, findings := range ag.grouped {
		avgCVSS := ag.calculateAvgCVSS(findings)
		
		locations := make([]string, len(findings))
		ids := make([]string, len(findings))
		for i, f := range findings {
			locations[i] = f.Location
			ids[i] = f.ID
		}
		
		description := ag.generateGroupDescription(key, findings)
		
		groupedFinding := GroupedFinding{
			Key:         key,
			Count:       len(findings),
			FindingIDs:  ids,
			Locations:   locations,
			AvgCVSS:     avgCVSS,
			Description: description,
			Sample:      findings[0],
		}
		result.GroupedFindings = append(result.GroupedFindings, groupedFinding)
	}
	
	// Calculate CVE risk summaries
	for cve, findings := range ag.cveIndex {
		maxCVSS := ag.maxCVSS(findings)
		assets := make([]string, len(findings))
		for i, f := range findings {
			assets[i] = f.Location
		}
		
		summary := CVERiskSummary{
			CVE:            cve,
			Count:          len(findings),
			MaxCVSS:        maxCVSS,
			AffectedAssets: assets,
		}
		result.CVERiskSummary = append(result.CVERiskSummary, summary)
	}
	
	// Sort CVE summaries by CVSS score
	result.sortCVESummaries()
	
	// Calculate overall risk score
	result.RiskScore = ag.calculateOverallRiskScore()
	
	// Identify top priorities
	result.TopPriorities = ag.identifyTopPriorities()
	
	// Update last run timestamp
	ag.lastRun = time.Now()
	
	return result
}

// calculateAvgCVSS calculates average CVSS for a group
func (ag *FindingsAggregator) calculateAvgCVSS(findings []Finding) float64 {
	if len(findings) == 0 {
		return 0
	}
	
	var sum float64
	for _, f := range findings {
		sum += f.CVSSScore
	}
	return sum / float64(len(findings))
}

// maxCVSS finds maximum CVSS in a list
func (ag *FindingsAggregator) maxCVSS(findings []Finding) float64 {
	var max float64
	for _, f := range findings {
		if f.CVSSScore > max {
			max = f.CVSSScore
		}
	}
	return max
}

// inferAttachmentLevel infers attachment from location string
func (ag *FindingsAggregator) inferAttachmentLevel(location string) AttachmentLevel {
	switch {
	case strings.Contains(location, "api") || strings.Contains(location, "endpoint") || strings.Contains(location, "port"):
		return LevelNetwork
	case strings.Contains(location, ".go") || strings.Contains(location, ".py") || strings.Contains(location, ".js"):
		return LevelApplication
	case strings.Contains(location, "database") || strings.Contains(location, "storage") || strings.Contains(location, "data"):
		return LevelData
	default:
		return LevelSystem
	}
}

// generateGroupDescription generates human-readable group description
func (ag *FindingsAggregator) generateGroupDescription(key GroupKey, findings []Finding) string {
	parts := []string{}
	
	if key.CVE != "" {
		parts = append(parts, fmt.Sprintf("%s vulnerability", key.CVE))
	} else {
		parts = append(parts, key.Type)
	}
	
	parts = append(parts, fmt.Sprintf("affecting %d locations", len(findings)))
	
	if key.Severity == Critical || key.Severity == High {
		parts = append(parts, fmt.Sprintf("(CVSS: %.1f avg)", key.Severity.Score()))
	}
	
	return strings.Join(parts, " - ")
}

// sortCVESummaries sorts CVE summaries by risk
func (result *AggregationResult) sortCVESummaries() {
	sort.Slice(result.CVERiskSummary, func(i, j int) bool {
		// Higher CVSS first, then more occurrences
		if result.CVERiskSummary[i].MaxCVSS != result.CVERiskSummary[j].MaxCVSS {
			return result.CVERiskSummary[i].MaxCVSS > result.CVERiskSummary[j].MaxCVSS
		}
		return result.CVERiskSummary[i].Count > result.CVERiskSummary[j].Count
	})
	
	// Assign priority ranks
	for i := range result.CVERiskSummary {
		result.CVERiskSummary[i].PriorityRank = i + 1
	}
}

// calculateOverallRiskScore computes 0-100 risk score
func (ag *FindingsAggregator) calculateOverallRiskScore() float64 {
	if len(ag.findings) == 0 {
		return 0
	}
	
	var weightedSum float64
	count := 0
	
	for _, finding := range ag.findings {
		weight := finding.Severity.Weight()
		weightedSum += weight * finding.CVSSScore
		count++
	}
	
	avgScore := weightedSum / float64(count)
	
	// Scale to 0-100
	riskScore := min(avgScore*10, 100.0)
	return math.Round(riskScore*10) / 10
}

// identifyTopPriorities identifies highest priority items for remediation
func (ag *FindingsAggregator) identifyTopPriorities() []PrioritizedFinding {
	const MaxPriorities = 5
	
	var priorities []PrioritizedFinding
	
	// Sort findings by risk score
	sorted := make([]Finding, len(ag.findings))
	copy(sorted, ag.findings)
	sort.Slice(sorted, func(i, j int) bool {
		scoreI := sorted[i].Severity.Weight() * sorted[i].CVSSScore
		scoreJ := sorted[j].Severity.Weight() * sorted[j].CVSSScore
		return scoreI > scoreJ
	})
	
	for _, finding := range sorted[:min(len(sorted), MaxPriorities)] {
		urgency := determineUrgency(finding)
		reason := ag.buildPriorityReason(finding)
		
		priority := PrioritizedFinding{
			FindingID: finding.ID,
			CVE:       finding.CVE,
			Reason:    reason,
			RiskScore: finding.Severity.Weight() * finding.CVSSScore,
			Action:    ag.suggestAction(finding),
			Urgency:   urgency,
		}
		priorities = append(priorities, priority)
	}
	
	return priorities
}

// buildPriorityReason explains why this finding is high priority
func (ag *FindingsAggregator) buildPriorityReason(finding Finding) string {
	reasons := []string{}
	
	if finding.Severity == Critical {
		reasons = append(reasons, "Critical severity")
	} else if finding.Severity == High {
		reasons = append(reasons, "High severity")
	}
	
	if finding.CVSSScore >= 9.0 {
		reasons = append(reasons, "CVSS score ≥ 9.0")
	}
	
	if finding.MitreTactic != "" {
		reasons = append(reasons, fmt.Sprintf("MITRE ATT&CK: %s", finding.MitreTactic))
	}
	
	if finding.Source == "Trivy" || finding.Source == "Grype" {
		reasons = append(reasons, "Verified by automated scanner")
	}
	
	return strings.Join(reasons, ", ")
}

// suggestAction recommends specific remediation action
func (ag *FindingsAggregator) suggestAction(finding Finding) string {
	switch {
	case strings.Contains(finding.Type, "dependency") || strings.Contains(finding.Type, "library"):
		return "Update vulnerable package to patched version"
	case finding.Type == "hardcoded_secret" || finding.Type == "credential_exposure":
		return "Remove credentials and use secret management"
	case finding.Type == "sql_injection" || finding.Type == "injection":
		return "Implement parameterized queries or input sanitization"
	case finding.Type == "insecure_direct_object_reference":
		return "Add authorization checks for object access"
	default:
		return "Review and apply vendor security patches"
	}
}

// determineUrgency determines fix urgency
func determineUrgency(finding Finding) UrgencyLevel {
	if finding.Severity == Critical && finding.CVSSScore >= 9.5 {
		return UrgencyImmediate
	}
	if finding.Severity == Critical || finding.Severity == High {
		return UrgencySoon
	}
	if finding.Severity == Medium {
		return UrgencyScheduled
	}
	return UrgencyMonitor
}

// GetGroupedFindings returns grouped findings
func (ag *FindingsAggregator) GetGroupedFindings() []GroupedFinding {
	ag.mu.RLock()
	defer ag.mu.RUnlock()
	
	result := make([]GroupedFinding, 0, len(ag.grouped))
	for _, findings := range ag.grouped {
		if len(findings) > 0 {
			group := findings[0]
			result = append(result, GroupedFinding{
				Key: GroupKey{
					CVE:      group.CVE,
					Type:     group.Type,
					Severity: group.Severity,
				},
				Count:       len(findings),
				FindingIDs:  nil, // Will be filled
				Locations:   nil,
				AvgCVSS:     ag.calculateAvgCVSS(findings),
				Description: "",
				Sample:      group,
			})
		}
	}
	return result
}

// Helper functions (would need imports in real code)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Additional implementation would include proper logging, metrics, etc.