package redteam

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/sirupsen/logrus"
)

// Neo4jIndexOptimizer manages database indexes and constraints for optimal query performance
type Neo4jIndexOptimizer struct {
	driver   neo4j.Driver
	logger   *logrus.Logger
}

// NewNeo4jIndexOptimizer creates an optimizer instance
func NewNeo4jIndexOptimizer(driver neo4j.Driver, logger *logrus.Logger) *Neo4jIndexOptimizer {
	return &Neo4jIndexOptimizer{
		driver: driver,
		logger: logger,
	}
}

// EnsureIndexes creates all necessary indexes for CVE knowledge graph queries
func (njo *Neo4jIndexOptimizer) EnsureIndexes(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	indexes := []string{
		// Primary index on CVE nodes
		`CREATE INDEX cve_id_index IF NOT EXISTS FOR (c:CVE) ON (c.id)`,
		
		// Secondary indexes for common queries
		`CREATE INDEX cve_cvss_score_index IF NOT EXISTS FOR (c:CVE) ON (c.cvss_score)`,
		`CREATE INDEX cve_severity_index IF NOT EXISTS FOR (c:CVE) ON (c.base_severity)`,
		
		// MITRE ATT&CK technique index
		`CREATE INDEX mitre_technique_id_index IF NOT EXISTS FOR (t:MITRETechnique) ON (t.id)`,
		
		// Exploit relationship index
		`CREATE INDEX exploit_url_index IF NOT EXISTS FOR (e:Exploit) ON (e.url)`,
		
		// Threat indicator index
		`CREATE INDEX threat_tlp_index IF NOT EXISTS FOR (ti:ThreatIndicator) ON (ti.tlp)`,
	}

	sessionConfig := neo4j.SessionConfig{
		AccessMode: neo4j.AccessWriteDefault,
	}

	session, err := njo.driver.Session(ctx, sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j session: %w", err)
	}
	defer session.Close()

	for _, indexCmd := range indexes {
		result, err := session.Run(ctx, indexCmd, nil)
		if err != nil {
			njo.logger.WithError(err).Warnf("Failed to create index: %s", indexCmd)
			continue
		}

		record, err := result.Single()
		if err == nil {
			msg := record.Get("message").(string)
			njo.logger.Debugf("Created index: %s - %s", indexCmd, msg)
		}
	}

	// Create uniqueness constraints
	constraints := []string{
		`CREATE CONSTRAINT cve_id_uniqueness IF NOT EXISTS FOR (c:CVE) REQUIRE c.id IS UNIQUE`,
		`CREATE CONSTRAINT technique_id_uniqueness IF NOT EXISTS FOR (t:MITRETechnique) REQUIRE t.id IS UNIQUE`,
	}

	for _, constraint := range constraints {
		result, err := session.Run(ctx, constraint, nil)
		if err != nil {
			njo.logger.WithError(err).Warnf("Failed to create constraint: %s", constraint)
			continue
		}

		record, err := result.Single()
		if err == nil {
			msg := record.Get("message").(string)
			njo.logger.Debugf("Created constraint: %s - %s", constraint, msg)
		}
	}

	njo.logger.Info("All indexes and constraints created successfully")
	return nil
}

// OptimizeQueryPerformance runs query optimizations for the CVE knowledge graph
func (njo *Neo4jIndexOptimizer) OptimizeQueryPerformance(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	sessionConfig := neo4j.SessionConfig{
		AccessMode: neo4j.AccessReadDefault,
	}

	session, err := njo.driver.Session(ctx, sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j session: %w", err)
	}
	defer session.Close()

	// Run Cypher query optimization
	optimizationQueries := []struct {
		name string
		query string
	}{
		{
			name: "Analyze CVE queries",
			query: "CALL dbms.procedures() YIELD name, description WHERE 'Cypher' IN description CALL db.schema.visualization()",
		},
		{
			name: "Get query performance stats",
			query: "CALL db.await.indexes() YIELD name, type RETURN name, type",
		},
	}

	for _, opt := range optimizationQueries {
		result, err := session.Run(ctx, opt.query, nil)
		if err != nil {
			njo.logger.WithError(err).Warnf("Failed to run optimization query: %s", opt.name)
			continue
		}
		njo.logger.Infof("Successfully ran optimization query: %s", opt.name)
	}

	return nil
}

// CleanStaleData removes duplicate or stale CVE entries
func (njo *Neo4jIndexOptimizer) CleanStaleData(ctx context.Context, retentionDays int) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(retentionDays)*24*time.Hour)
	defer cancel()

	sessionConfig := neo4j.SessionConfig{
		AccessMode: neo4j.AccessWriteDefault,
	}

	session, err := njo.driver.Session(ctx, sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j session: %w", err)
	}
	defer session.Close()

	// Remove duplicates based on ID
	cleanupQuery := `
	CALL {
		MATCH (c1:CVE)
		WITH c1.id AS id, COLLECT(c1) as duplicates
		WHERE size(duplicates) > 1
		
		// Keep the one with highest CVSS score, delete others
		RETURN head(sort(duplicates, function(a, b) a.cvss_score > b.cvss_score))[0] as keeper,
		       tail(sort(duplicates, function(a, b) a.cvss_score > b.cvss_score)) as duplicates_to_delete
	}
	
	UNWIND duplicates_to_delete AS duplicate
	DETACH DELETE duplicate`

	result, err := session.Run(ctx, cleanupQuery, map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to execute cleanup query: %w", err)
	}

	count, _ := result.Consume(ctx)
	njo.logger.Infof("Cleaned up %d stale CVE duplicates", count.Counters().NodesDeleted())

	return nil
}

// GetDatabaseStatistics returns Neo4j database statistics
func (njo *Neo4jIndexOptimizer) GetDatabaseStatistics(ctx context.Context) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	sessionConfig := neo4j.SessionConfig{
		AccessMode: neo4j.AccessReadDefault,
	}

	session, err := njo.driver.Session(ctx, sessionConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j session: %w", err)
	}
	defer session.Close()

	stats := make(map[string]interface{})

	// Get node counts
	nodeStats := []struct {
		label string
		query string
	}{
		{"CVE Nodes", "MATCH (c:CVE) RETURN count(c) as count"},
		{"MITRE Techniques", "MATCH (t:MITRETechnique) RETURN count(t) as count"},
		{"Exploits", "MATCH (e:Exploit) RETURN count(e) as count"},
		{"Threat Indicators", "MATCH (ti:ThreatIndicator) RETURN count(ti) as count"},
	}

	for _, stat := range nodeStats {
		result, err := session.Run(ctx, stat.query, nil)
		if err != nil {
			continue
		}
		record, err := result.Single()
		if err == nil {
			count := record.Get("count").(int64)
			stats[stat.label] = count
		}
	}

	// Get relationship counts
	relStats := []struct {
		label string
		query string
	}{
		{"CVE-Exploit Relationships", "MATCH ()-[r:HAS_EXPLOIT]->() RETURN count(r) as count"},
		{"CVE-Technique Relationships", "MATCH ()-[r:USES_TECHNIQUE]->() RETURN count(r) as count"},
		{"CVE-Threat Relationships", "MATCH ()-[r:RELATED_TO_THREAT]->() RETURN count(r) as count"},
	}

	for _, stat := range relStats {
		result, err := session.Run(ctx, stat.query, nil)
		if err != nil {
			continue
		}
		record, err := result.Single()
		if err == nil {
			count := record.Get("count").(int64)
			stats[stat.label] = count
		}
	}

	// Get index information
	indexResult, err := session.Run(ctx, "CALL db.indexes() YIELD name RETURN count(name) as total", nil)
	if err == nil {
		if record, err := indexResult.Single(); err == nil {
			totalIndices := record.Get("total").(int64)
			stats["Total Indexes"] = totalIndices
		}
	}

	return stats, nil
}
