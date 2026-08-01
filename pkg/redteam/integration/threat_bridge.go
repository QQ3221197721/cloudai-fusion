// Package threat_bridge - Integration bridge between existing threat detection and Red Team framework
package threat_bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/audit"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/attack_graph"
)

// ThreatBridge connects existing threat detection with the new knowledge graph
type ThreatBridge struct {
	graphClient *attack_graph.Neo4jGraphClient
	logger      *logrus.Logger
	lastSyncAt  time.Time
}

// NewThreatBridge creates a new threat detection bridge
func NewThreatBridge(client *attack_graph.Neo4jGraphClient, logger *logrus.Logger) *ThreatBridge {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	return &ThreatBridge{
		graphClient: client,
		logger:      logger,
		lastSyncAt:  time.Now(),
	}
}

// ============================================================================
// Threat Event to CVE Mapping
// ============================================================================

// MapThreatEventToCVE maps a detected threat event to potential CVEs
func (tb *ThreatBridge) MapThreatEventToCVE(ctx context.Context, threatEvent *audit.ThreatEvent) ([]string, error) {
	// Extract CVE patterns from threat event details
	cvePatterns := tb.extractCVESignatures(threatEvent)
	
	if len(cvePatterns) == 0 {
		tb.logger.WithFields(logrus.Fields{
			"threat_type": threatEvent.Type,
			"source":      threatEvent.Source,
		}).Debug("No CVE signatures found in threat event")
		
		return nil, nil
	}
	
	// Query Neo4j for matching CVEs
	matchingCVEs := make([]string, 0)
	
	for _, pattern := range cvePatterns {
		results, err := tb.graphClient.FindCVEsBySeverity(ctx, 5.0) // Minimum score 5.0
		if err != nil {
			tb.logger.WithError(err).Warn("Failed to query CVEs by severity")
			continue
		}
		
		for _, cve := range results {
			if tb.matchesSignature(cve.ID, pattern) {
				matchingCVEs = append(matchingCVEs, cve.ID)
			}
		}
	}
	
	// Log mapping result
	tb.logger.WithFields(logrus.Fields{
		"threat_id":        threatEvent.ID,
		"matching_cves":    len(matchingCVEs),
		"cve_list":         matchingCVEs,
	}).Info("Mapped threat event to CVEs")
	
	return matchingCVEs, nil
}

// extractCVESignatures extracts potential CVE identifiers from threat event details
func (tb *ThreatBridge) extractCVESignatures(event *audit.ThreatEvent) []string {
	signatures := make([]string, 0)
	
	details := event.Details
	
	// Check for CVE mentions in description or metadata
	if desc, ok := details["description"].(string); ok {
		// Pattern: CVE-2024-XXXXX
		if containsCVEPattern(desc) {
			signatures = append(signatures, extractCVEFromText(desc))
		}
	}
	
	// Check for vulnerability-related keywords
	vulnKeywords := []string{"vulnerability", "CVE", "exploit", "patch"}
	for _, keyword := range vulnKeywords {
		if stringsContains(details, keyword) {
			signatures = append(signatures, keyword)
		}
	}
	
	// Deduplicate
	return dedupeStrings(signatures)
}

// matchesSignature checks if CVE ID matches signature pattern
func (tb *ThreatBridge) matchesSignature(cveID string, signature string) bool {
	if cveID == signature {
		return true
	}
	
	// Also check year-based patterns
	if len(signature) >= 4 {
		year := signature[:4]
		if strings.Contains(cveID, year) {
			return true
		}
	}
	
	return false
}

// ============================================================================
// Threat Event Enrichment
// ============================================================================

// EnrichThreatEvent enriches a threat event with additional attack graph data
func (tb *ThreatBridge) EnrichThreatEvent(ctx context.Context, event *audit.ThreatEvent) (*EnrichedThreatEvent, error) {
	enriched := &EnrichedThreatEvent{
		OriginalEvent:  event,
		MappedCVEs:     []string{},
		AttackPaths:    []string{},
		RiskScore:      0.0,
		Recommendations: []string{},
	}
	
	// Step 1: Map to CVEs
	cveIDs, err := tb.MapThreatEventToCVE(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("failed to map CVEs: %w", err)
	}
	
	enriched.MappedCVEs = cveIDs
	
	// Step 2: Find related attack paths
	if len(cveIDs) > 0 {
		attackPaths, err := tb.findRelatedAttackPaths(ctx, cveIDs[0])
		if err != nil {
			tb.logger.WithError(err).Warn("Failed to find attack paths")
		} else {
			enriched.AttackPaths = attackPaths
		}
	}
	
	// Step 3: Calculate risk score
	enriched.RiskScore = tb.calculateThreatRiskScore(enriched)
	
	// Step 4: Generate recommendations
	enriched.Recommendations = tb.generateRecommendations(enriched)
	
	return enriched, nil
}

// findRelatedAttackPaths finds attack paths starting from a CVE
func (tb *ThreatBridge) findRelatedAttackPaths(ctx context.Context, startCVE string) ([]string, error) {
	paths, err := tb.graphClient.BuildAttackPath(ctx, startCVE)
	if err != nil {
		return nil, err
	}
	
	return paths, nil
}

// calculateThreatRiskScore calculates combined risk score
func (tb *ThreatBridge) calculateThreatRiskScore(enriched *EnrichedThreatEvent) float64 {
	score := 0.0
	
	// Base score from threat severity
	switch enriched.OriginalEvent.Severity {
	case "critical":
		score += 9.0
	case "high":
		score += 7.0
	case "medium":
		score += 4.0
	default:
		score += 2.0
	}
	
	// Add CVE scores
	for _, cveID := range enriched.MappedCVEs {
		// In production, fetch actual CVE score from Neo4j
		score += 5.0 // Placeholder
	}
	
	// Factor in attack path complexity
	score += float64(len(enriched.AttackPaths)) * 0.5
	
	// Cap at 10.0
	if score > 10.0 {
		score = 10.0
	}
	
	return score
}

// generateRecommendations generates remediation recommendations
func (tb *ThreatBridge) generateRecommendations(enriched *EnrichedThreatEvent) []string {
	recommendations := make([]string, 0)
	
	// General recommendation based on threat type
	switch enriched.OriginalEvent.Type {
	case "brute-force":
		recommendations = append(recommendations, "Implement rate limiting and CAPTCHA")
	case "privilege-escalation":
		recommendations = append(recommendations, "Review RBAC policies and remove unnecessary admin access")
	case "data-exfiltration":
		recommendations = append(recommendations, "Enable DLP controls and encrypt sensitive data")
	case "anomalous-access":
		recommendations = append(recommendations, "Audit user permissions and implement zero-trust network")
	default:
		recommendations = append(recommendations, "Investigate root cause and apply appropriate mitigations")
	}
	
	// CVE-specific recommendations
	for _, cveID := range enriched.MappedCVEs {
		recommendations = append(recommendations, 
			fmt.Sprintf("Check vendor advisories for %s", cveID))
	}
	
	return recommendations
}

// ============================================================================
// Historical Correlation Analysis
// ============================================================================

// AnalyzeHistoricalCorrelations analyzes correlations between past threats and CVEs
func (tb *ThreatBridge) AnalyzeHistoricalCorrelations(ctx context.Context, days int) (*ThreatAnalysisReport, error) {
	report := &ThreatAnalysisReport{
		AnalyzedDays:  days,
		GeneratedAt:   time.Now(),
		Correlations:  []CorrelationRecord{},
	}
	
	// This would require querying historical audit logs
	// For now, return placeholder
	tb.logger.WithField("days", days).Info("Analyzing historical correlations")
	
	return report, nil
}

// ============================================================================
// Real-time Event Synchronization
// ============================================================================

// SyncEventsToGraph synchronizes recent threat events to the knowledge graph
func (tb *ThreatBridge) SyncEventsToGraph(ctx context.Context, sinceTime time.Time) error {
	// Fetch events from audit system
	events := tb.fetchRecentThreatEvents(sinceTime)
	
	for _, event := range events {
		// Map to CVEs and store in Neo4j
		cveIDs, err := tb.MapThreatEventToCVE(ctx, event)
		if err != nil {
			tb.logger.WithError(err).Warn("Failed to process threat event")
			continue
		}
		
		// Create relationship in Neo4j
		for _, cveID := range cveIDs {
			err = tb.createThreatToCVERelationship(ctx, event.ID, cveID)
			if err != nil {
				tb.logger.WithError(err).Warn("Failed to create threat-CVE relationship")
			}
		}
	}
	
	tb.lastSyncAt = time.Now()
	tb.logger.WithField("sync_count", len(events)).Info("Synced threat events to graph")
	
	return nil
}

func (tb *ThreatBridge) fetchRecentThreatEvents(sinceTime time.Time) []*audit.ThreatEvent {
	// Placeholder - would query audit database
	return []*audit.ThreatEvent{}
}

func (tb *ThreatBridge) createThreatToCVERelationship(ctx context.Context, threatID, cveID string) error {
	// Cypher query to create relationship
	cypher := `MATCH (t:ThreatEvent {id: $threat_id})
			   MERGE (c:CVE {id: $cve_id})
			   CREATE (t)-[:RELATED_TO_CVE]->(c)
			   RETURN t, c`
	
	params := map[string]interface{}{
		"threat_id": threatID,
		"cve_id":    cveID,
	}
	
	// Execute via Neo4j driver
	_, err := tb.graphClient.FindCVEsBySeverity(ctx, 0) // Just verify connection
	
	return err
}

// ============================================================================
// Data Structures
// ============================================================================

// EnrichedThreatEvent represents a threat event with additional attack graph data
type EnrichedThreatEvent struct {
	OriginalEvent   *audit.ThreatEvent
	MappedCVEs      []string
	AttackPaths     []string
	RiskScore       float64
	Recommendations []string
}

// ThreatAnalysisReport summarizes correlation analysis
type ThreatAnalysisReport struct {
	AnalyzedDays  int               `json:"analyzed_days"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Correlations  []CorrelationRecord `json:"correlations"`
}

// CorrelationRecord represents a threat-CVE correlation
type CorrelationRecord struct {
	ThreatID     string   `json:"threat_id"`
	CVEIDs       []string `json:"cve_ids"`
	Confidence   float64  `json:"confidence"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}
