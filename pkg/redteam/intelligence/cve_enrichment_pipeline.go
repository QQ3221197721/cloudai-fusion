
package redteam

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// CVEEnrichmentPipeline orchestrates multi-source data enrichment and Neo4j storage
type CVEEnrichmentPipeline struct {
	feedManager *MultiSourceFeedManager
	logger      *logrus.Logger
	
	// Worker pool configuration
	numWorkers int
	batchSize  int
	
	// Metrics
	metrics struct {
		totalIngested    int64
		totalProcessed   int64
		totalFailed      int64
		startTime        time.Time
		duration         time.Duration
		mu               sync.RWMutex
	}
}

// NewCVEEnrichmentPipeline creates a new enrichment pipeline
func NewCVEEnrichmentPipeline(feedManager *MultiSourceFeedManager, logger *logrus.Logger, workers, batchSize int) *CVEEnrichmentPipeline {
	if workers == 0 {
		workers = 5 // Default concurrent workers
	}
	if batchSize == 0 {
		batchSize = 100 // Process 100 CVEs per batch
	}

	return &CVEEnrichmentPipeline{
		feedManager: feedManager,
		logger:      logger,
		numWorkers:  workers,
		batchSize:   batchSize,
	}
}

// Run executes the full enrichment pipeline
func (cep *CVEEnrichmentPipeline) Run(ctx context.Context, limit int) error {
	cep.metrics.startTime = time.Now()
	cep.logger.WithFields(logrus.Fields{
		"limit":           limit,
		"num_workers":     cep.numWorkers,
		"batch_size":      cep.batchSize,
	}).Info("Starting CVE enrichment pipeline")

	// Step 1: Fetch data from all sources
	rawData, err := cep.feedManager.FetchAllCVEs(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to fetch CVE data: %w", err)
	}

	cep.logger.WithField("fetched_count", len(rawData)).Info("Fetched enriched CVE data from multiple sources")

	// Step 2: Process each CVE through worker pool
	cveChan := make(chan CVEItemWithEnrichment, len(rawData))
	errorsChan := make(chan error, 100)

	// Distribute data to workers
	for i := 0; i < cep.numWorkers; i++ {
		go cep.processWorker(ctx, cveChan, errorsChan)
	}

	// Feed data into channel
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, item := range rawData {
			select {
			case <-ctx.Done():
				return
			case cveChan <- item:
			}
		}
	}()

	// Wait for feeder to complete, then close channel
	go func() {
		wg.Wait()
		close(cveChan)
	}()

	// Process results
	var processedCount int
	for err := range errorsChan {
		cep.metrics.totalFailed++
		cep.logger.WithError(err).Warn("Failed to process CVE")
	}

	processedCount = int(cep.metrics.totalProcessed)
	cep.metrics.duration = time.Since(cep.metrics.startTime)

	cep.logger.WithFields(logrus.Fields{
		"total_fetched":          len(rawData),
		"total_processed":        processedCount,
		"total_failed":           cep.metrics.totalFailed,
		"processing_duration_ms": cep.metrics.duration.Milliseconds(),
		"throughput_cves_per_sec": float64(processedCount) / cep.metrics.duration.Seconds(),
	}).Info("CVE enrichment pipeline completed")

	return nil
}

// processWorker handles individual CVE processing
func (cep *CVEEnrichmentPipeline) processWorker(ctx context.Context, input <-chan CVEItemWithEnrichment, output chan<- error) {
	for item := range input {
		select {
		case <-ctx.Done():
			return
		default:
			err := cep.storeCVEInNeo4j(item)
			if err != nil {
				output <- fmt.Errorf("failed to store CVE %s: %w", item.CVE.ID, err)
			} else {
				cep.incrementProcessed()
			}
		}
	}
}

// storeCVEInNeo4j stores a single CVE with all enrichments in Neo4j
func (cep *CVEEnrichmentPipeline) storeCVEInNeo4j(item CVEItemWithEnrichment) error {
	_ = context.Background()

	// Create CVE node with all metadata
	_ = `
	MERGE (cve:CVE {id: $id})
	ON CREATE SET 
		cve.created = $created,
		cve.published = $published,
		cve.cvss_score = $score,
		cve.base_severity = $severity,
		cve.vector_string = $vector,
		cve.attack_vector = $av,
		cve.attack_complexity = $ac,
		cve.privileges_required = $pr,
		cve.user_interaction = $ui,
		cve.scope = $scope,
		cve.confidentiality = $conf,
		cve.integrity = $int,
		cve.availability = $avail
	SET cve.description = $description,
	    cve.last_modified = $modified
	RETURN cve.id`

	description := ""
	if len(item.CVE.CVE.Description) > 0 {
		description = item.CVE.CVE.Description[0].Value
	}

	params := map[string]interface{}{
		"id":              item.CVE.ID,
		"description":     description,
		"score":           item.CVE.Impact.BaseScore,
		"severity":        item.CVE.Impact.BaseSeverity,
		"vector":          item.CVE.Impact.VectorString,
		"av":              "", // Need to parse from vector string
		"ac":              "",
		"pr":              "",
		"ui":              "",
		"scope":           "",
		"conf":            "",
		"int":             "",
		"avail":           "",
		"created":         time.Now().UTC(),
		"modified":        time.Now().UTC(),
		"published":       time.Now().UTC(),
	}

	// TODO: Execute Cypher query against Neo4j graph client
	// For now, just log what would happen
	cep.logger.WithFields(logrus.Fields{
		"cve_id":     item.CVE.ID,
		"cvss_score": params["score"],
		"severity":   params["severity"],
	}).Debug("Would create CVE node in Neo4j")

	// Store exploit metadata if available
	if item.ExploitMetadata != nil {
		_ = `
		MATCH (cve:CVE {id: $cveId})
		CREATE (e:Exploit {
			url: $url,
			platform: $platform,
			author: $author,
			proof_of_concept: $poc
		})
		CREATE (cve)-[:HAS_EXPLOIT]->(e)
		RETURN e.url`

		exploitParams := map[string]interface{}{
			"cveId":        item.CVE.ID,
			"url":          item.ExploitMetadata.URL,
			"platform":     item.ExploitMetadata.Platform,
			"author":       item.ExploitMetadata.Author,
			"poc":          item.ExploitMetadata.ProofOfConcept,
		}

		cep.logger.WithFields(logrus.Fields{
			"cve_id": item.CVE.ID,
			"url":    exploitParams["url"],
		}).Debug("Would store Exploit relationship in Neo4j")
	}

	// Map MITRE ATT&CK techniques
	for _, technique := range item.Techniques {
		_ = `
		MATCH (cve:CVE {id: $cveId})
		MERGE (tech:MITRETechnique {id: $techniqueId})
		ON CREATE SET 
			tech.name = $techniqueName,
			tech.tactic = $tactic,
			tech.tactic_name = $tacticName
		CREATE (cve)-[:USES_TECHNIQUE]->(tech)
		RETURN tech.id`

		techniqueParams := map[string]interface{}{
			"cveId":         item.CVE.ID,
			"techniqueId":   technique.TechniqueID,
			"techniqueName": technique.TechniqueName,
			"tactic":        technique.TacticID,
			"tacticName":    technique.TacticName,
		}

		cep.logger.WithFields(logrus.Fields{
			"cve_id":     item.CVE.ID,
			"technique":  techniqueParams["techniqueId"],
			"tactic":     techniqueParams["tactic"],
		}).Debug("Would map MITRE ATT&CK technique in Neo4j")
	}

	// Store threat indicators
	for _, indicator := range item.ThreatIntel {
		_ = `
		MATCH (cve:CVE {id: $cveId})
		MERGE (threat:ThreatIndicator {
			tlp: $tlp,
			active_campaign: $activeCampaign
		})
		ON CREATE SET
			threat.apt_groups = $aptGroups,
			threat.exploit_type = $exploitType
		CREATE (cve)-[:RELATED_TO_THREAT]->(threat)
		RETURN threat.tlp`

		threatParams := map[string]interface{}{
			"cveId":          item.CVE.ID,
			"tlp":            indicator.TLP,
			"activeCampaign": indicator.ActiveCampaign,
			"aptGroups":      indicator.APTGroups,
			"exploitType":    indicator.ExploitType,
		}

		cep.logger.WithFields(logrus.Fields{
			"cve_id":        item.CVE.ID,
			"tlp":           threatParams["tlp"],
			"active":        threatParams["activeCampaign"],
		}).Debug("Would store threat indicator in Neo4j")
	}

	return nil
}

// incrementProcessed updates processed counter atomically
func (cep *CVEEnrichmentPipeline) incrementProcessed() {
	cep.metrics.mu.Lock()
	defer cep.metrics.mu.Unlock()
	cep.metrics.totalProcessed++
}

// GetMetrics returns current pipeline metrics
func (cep *CVEEnrichmentPipeline) GetMetrics() PipelineMetrics {
	cep.metrics.mu.RLock()
	defer cep.metrics.mu.RUnlock()

	return PipelineMetrics{
		TotalIngested:   cep.metrics.totalIngested,
		TotalProcessed:  cep.metrics.totalProcessed,
		TotalFailed:     cep.metrics.totalFailed,
		StartTime:       cep.metrics.startTime,
		Duration:        cep.metrics.duration,
		Throughput:      float64(cep.metrics.totalProcessed) / cep.metrics.duration.Seconds(),
	}
}

// PipelineMetrics provides performance metrics
type PipelineMetrics struct {
	TotalIngested   int64         `json:"total_ingested"`
	TotalProcessed  int64         `json:"total_processed"`
	TotalFailed     int64         `json:"total_failed"`
	StartTime       time.Time     `json:"start_time"`
	Duration        time.Duration `json:"duration_ms"`
	Throughput      float64       `json:"throughput_cves_per_sec"`
}
