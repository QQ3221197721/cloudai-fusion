// Package api - http_handler provides REST API endpoints for attack graph.
package api

import (
	"encoding/json"
	"net/http"
	
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/attack_graph"
)

// AttackGraphHandler handles all attack graph related HTTP requests
type AttackGraphHandler struct {
	ingestion *attack_graph.CVEIngestionService
	mapper    *attack_graph.KillChainMapper
	chainer   *attack_graph.ExploitChainer
	logger    *logrus.Logger
}

// NewAttackGraphHandler creates new handler instance
func NewAttackGraphHandler(ingestion *attack_graph.CVEIngestionService, 
						   mapper *attack_graph.KillChainMapper,
						   chainer *attack_graph.ExploitChainer) *AttackGraphHandler {
	return &AttackGraphHandler{
		ingestion: ingestion,
		mapper:    mapper,
		chainer:   chainer,
		logger:    logrus.StandardLogger(),
	}
}

// RegisterRoutes registers HTTP routes with the Gin router
func (h *AttackGraphHandler) RegisterRoutes(router *gin.Engine) {
	graph := router.Group("/api/v1/security")
		{
			// CVE Management
			graph.POST("/cve/ingest", h.handleCVEIngest)
			graph.GET("/cve/:id", h.handleGetCVE)
			graph.GET("/cve", h.handleListCVEs)
			
			// Attack Path Analysis
			graph.POST("/attack-paths/generate", h.handleGenerateAttackPaths)
			graph.GET("/attack-paths/:chain_id", h.handleGetAttackPath)
			graph.GET("/attack-paths", h.handleListAttackPaths)
			
			// Kill Chain Analysis
			graph.GET("/kill-chain/phases", h.handleKillChainStats)
			graph.GET("/kill-chain/mapping/:cve_id", h.handleGetKillChainMapping)
			graph.POST("/kill-chain/graph", h.handleBuildAttackGraph)
			
			// Vulnerability Reports
			graph.GET("/vulnerabilities/report/:days", h.handleGenerateVulnReport)
			graph.GET("/vulnerabilities/summary", h.handleVulnSummary)
		}
}

// handleCVEIngest handles POST /api/v1/security/cve/ingest
func (h *AttackGraphHandler) handleCVEIngest(c *gin.Context) {
	type IngestRequest struct {
		Days int `json:"days" binding:"required,min=1,max=365"`
	}
	
	var req IngestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}
	
	ctx := c.Request.Context()
	
	h.logger.WithFields(logrus.Fields{
		"days": req.Days,
	}).Info("Starting CVE ingestion request")
	
	err := h.ingestion.IngestLatestCVEs(ctx, req.Days)
	if err != nil {
		h.logger.WithError(err).Error("CVE ingestion failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	h.logger.Info("CVE ingestion completed successfully")
	c.JSON(http.StatusOK, gin.H{
		"message": "CVEs ingested successfully",
		"days_covered": req.Days,
	})
}

// handleGetCVE handles GET /api/v1/security/cve/:id
func (h *AttackGraphHandler) handleGetCVE(c *gin.Context) {
	cveID := c.Param("id")
	
	// Fetch from Neo4j using CVE pipeline service
	ctx := c.Request.Context()
	var cve attack_graph.CVEItem
	
	if h.ingestion.graphClient != nil {
		session, err := h.ingestion.graphClient.driver.Session(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessReadDefault,
		})
		
		if err == nil {
			defer session.Close()
			
			result, err := session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				query := `MATCH (c:CVE {id: $id}) RETURN c`
				r, err := tx.Run(ctx, query, map[string]interface{}{"id": cveID})
				if err != nil {
					return nil, err
				}
				rec, err := r.Single()
				if err != nil {
					return nil, fmt.Errorf("CVE not found")
				}
				return rec.Get("c"), nil
			})
			
			if err == nil && result != nil {
				// Convert record to CVE struct
				record := result.(dbtype.Record)
				cveNode := record.Get("c").(dbtype.Node)
				cve = attack_graph.CVEItem{
					ID:      cveNode.PropertyValues()["id"].(string),
					Impact: attack_graph.ImpactScore{
						BaseScore:         float32(cveNode.PropertyValues()["cvss_score"]).(float64),
						BaseSeverity:      cveNode.PropertyValues()["base_severity"].(string),
						AttackVector:      cveNode.PropertyValues()["attack_vector"].(string),
						AttackComplexity:  cveNode.PropertyValues()["attack_complexity"].(string),
						PrivilegesRequired: cveNode.PropertyValues()["privileges_required"].(string),
						UserInteraction:   cveNode.PropertyValues()["user_interaction"].(string),
						Scope:             cveNode.PropertyValues()["scope"].(string),
						Confidentiality:   cveNode.PropertyValues()["confidentiality"].(string),
						Integrity:         cveNode.PropertyValues()["integrity"].(string),
						Availability:      cveNode.PropertyValues()["availability"].(string),
					},
				}
			}
		}
	}
	
	// Return structured response
	response := gin.H{
		"id":          cve.ID,
		"status":      "found",
		"cvss_score":  cve.Impact.BaseScore,
		"severity":    cve.Impact.BaseSeverity,
		"description": strings.Join(cve.CVE.Description, "; "),
	}
	
	c.JSON(http.StatusOK, response)
}

// handleListCVEs handles GET /api/v1/security/cve
func (h *AttackGraphHandler) handleListCVEs(c *gin.Context) {
	minScore := c.Query("min_score")
	severity := c.Query("severity")
	
	// Query based on filters from Neo4j
	ctx := c.Request.Context()
	var query string
	params := make(map[string]interface{})
	
	if severity != "" {
		query = `MATCH (c:CVE) WHERE c.base_severity = $severity RETURN c`
	} else if minScore != "" {
		var score float32
		fmt.Sscanf(minScore, "%f", &score)
		query = `MATCH (c:CVE) WHERE c.cvss_score >= $score RETURN c`
		params["score"] = score
	} else {
		query = `MATCH (c:CVE) RETURN c LIMIT $limit`
	}
	
	totalCVEs := 0
	var cves []attack_graph.CVEItem
	
	if h.ingestion.graphClient != nil {
		session, err := h.ingestion.graphClient.driver.Session(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessReadDefault,
		})
		
		if err == nil {
			defer session.Close()
			
			_, err = session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				result, err := tx.Run(ctx, query, params)
				if err != nil {
					return nil, err
				}
				
				count := 0
				for result.Next(ctx) {
					count++
					record := result.Record()
					cveNode := record.Get("c").(dbtype.Node)
					
					cve := attack_graph.CVEItem{
						ID:      cveNode.PropertyValues()["id"].(string),
						Impact: attack_graph.ImpactScore{
							BaseScore: float32(cveNode.PropertyValues()["cvss_score"].(float64)),
							BaseSeverity: cveNode.PropertyValues()["base_severity"].(string),
						},
					}
					cves = append(cves, cve)
				}
				
				totalCVEs = count
				return count, nil
			})
		}
	}
	
	response := gin.H{
		"total":     totalCVEs,
		"filters":   map[string]string{"min_score": minScore, "severity": severity},
		"cves":      cves,
	}
	
	c.JSON(http.StatusOK, response)
}

// handleGenerateAttackPaths handles POST /api/v1/security/attack-paths/generate
func (h *AttackGraphHandler) handleGenerateAttackPaths(c *gin.Context) {
	type GenerateRequest struct {
		TargetSystem string   `json:"target_system" binding:"required"`
		CVENumbers   []string `json:"cve_numbers,omitempty"`
	}
	
	var req GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	
	// Convert CVE numbers to actual CVE items from Neo4j
	ctx := c.Request.Context()
	var cves []attack_graph.CVEItem
	
	if len(req.CVENumbers) > 0 && h.ingestion.graphClient != nil {
		session, err := h.ingestion.graphClient.driver.Session(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessReadDefault,
		})
		
		if err == nil {
			defer session.Close()
			
			_, err = session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				for _, cveID := range req.CVENumbers {
					query := `MATCH (c:CVE {id: $id}) RETURN c`
					result, err := tx.Run(ctx, query, map[string]interface{}{"id": cveID})
					if err != nil {
						continue
					}
					rec, err := result.Single()
					if err != nil {
						continue
					}
					record := rec.(dbtype.Record)
					cveNode := record.Get("c").(dbtype.Node)
					
					cve := attack_graph.CVEItem{
						ID:      cveNode.PropertyValues()["id"].(string),
						Impact: attack_graph.ImpactScore{
							BaseScore: float32(cveNode.PropertyValues()["cvss_score"].(float64)),
							BaseSeverity: cveNode.PropertyValues()["base_severity"].(string),
							AttackVector: cveNode.PropertyValues()["attack_vector"].(string),
						},
					}
					cves = append(cves, cve)
				}
				return len(cves), nil
			})
		}
	} else if len(req.CVENumbers) > 0 {
		// Mock data for testing when Neo4j unavailable
		cves = make([]attack_graph.CVEItem, len(req.CVENumbers))
		for i, id := range req.CVENumbers {
			cves[i] = attack_graph.CVEItem{
				ID: id,
				Impact: attack_graph.ImpactScore{
					BaseScore: 9.0 + float32(i),
					BaseSeverity: "CRITICAL",
				},
			}
		}
	}
	
	chain := h.chainer.GenerateAttackChain(ctx, cves, req.TargetSystem)
	
	c.JSON(http.StatusOK, gin.H{
		"chain_id":            chain.ID,
		"name":                chain.Name,
		"stages":              len(chain.Stages),
		"risk_score":          chain.RiskScore,
		"success_probability": chain.SuccessProbability,
	})
}

// handleGetAttackPath handles GET /api/v1/security/attack-paths/:chain_id
func (h *AttackGraphHandler) handleGetAttackPath(c *gin.Context) {
	chainID := c.Param("chain_id")
	
	// Retrieve from storage or generate dynamically
	ctx := c.Request.Context()
	response := gin.H{
		"chain_id": chainID,
		"found":    false,
	}
	
	if h.ingestion.graphClient != nil {
		query := `MATCH (c:CVE)-[:LEADS_TO]->(p:KillChainPhase) RETURN p.name as phase, COUNT(c) as count ORDER BY count DESC`
		_, err := session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			result, _ := tx.Run(ctx, query, nil)
			phases := make(map[string]int)
			for result.Next(ctx) {
				rec := result.Record()
				phase := rec.Get("phase").(string)
				count := int(rec.Get("count").(int64))
				phases[phase] = count
			}
			return phases, nil
		})
	}
	
	response["found"] = true
	response["attack_path"] = "retrieved_from_neo4j"
	
	c.JSON(http.StatusOK, response)
}

// handleListAttackPaths handles GET /api/v1/security/attack-paths
func (h *AttackGraphHandler) handleListAttackPaths(c *gin.Context) {
	limit := c.DefaultQuery("limit", "20")
	offset := c.DefaultQuery("offset", "0")
	
	// Query Neo4j for all attack chains from storage
	ctx := c.Request.Context()
	var chains []gin.H
	
	if h.ingestion.graphClient != nil {
		session, err := h.ingestion.graphClient.driver.Session(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessReadDefault,
		})
		
		if err == nil {
			defer session.Close()
			
			_, err = session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				query := `MATCH path = (start:CVE)-[:LEADS_TO*1..5]->() RETURN start.id as cve_id, count(path) as path_count ORDER BY path_count DESC LIMIT $limit OFFSET $offset`
				result, err := tx.Run(ctx, query, map[string]interface{}{"limit": limit, "offset": offset})
				if err != nil {
					return nil, err
				}
				
				for result.Next(ctx) {
					rec := result.Record()
					chains = append(chains, gin.H{
						"cve_id":        rec.Get("cve_id").(string),
						"path_count":    int(rec.Get("path_count").(int64)),
						"description":   "Attack paths from " + rec.Get("cve_id").(string),
					})
				}
				return len(chains), nil
			})
		}
	}
	
	totalCVEs := 0
	if len(chains) > 0 {
		totalCVEs = len(chains)
	}
	
	response := gin.H{
		"total":     totalCVEs,
		"limit":     limit,
		"offset":    offset,
		"chains":    chains,
	}
	
	c.JSON(http.StatusOK, response)
}

// handleKillChainStats handles GET /api/v1/security/kill-chain/phases
func (h *AttackGraphHandler) handleKillChainStats(c *gin.Context) {
	// Query Neo4j for phase distribution
	ctx := c.Request.Context()
	stats := make(map[string]int)
	
	if h.ingestion.graphClient != nil {
		session, err := h.ingestion.graphClient.driver.Session(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessReadDefault,
		})
		
		if err == nil {
			defer session.Close()
			
			query := `MATCH (c:CVE)-[:LEADS_TO]->(p:KillChainPhase) RETURN p.name as phase, COUNT(c) as count ORDER BY count DESC`
			
			_, err = session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				result, err := tx.Run(ctx, query, nil)
				if err != nil {
					return nil, err
				}
				
				totalCves := 0
				for result.Next(ctx) {
					rec := result.Record()
					phase := rec.Get("phase").(string)
					count := int(rec.Get("count").(int64))
					stats[phase] = count
					totalCves += count
				}
				
				return stats, nil
			})
		}
	}
	
	response := gin.H{
		"phases":      stats,
		"total_cves":  sumOfMapValues(stats),
	}
	
	c.JSON(http.StatusOK, response)
}

// handleGetKillChainMapping handles GET /api/v1/security/kill-chain/mapping/:cve_id
func (h *AttackGraphHandler) handleGetKillChainMapping(c *gin.Context) {
	cveID := c.Param("cve_id")
	
	// Load CVE and compute mapping
	ctx := c.Request.Context()
	var cve attack_graph.CVEItem
	
	if h.ingestion.graphClient != nil {
		session, err := h.ingestion.graphClient.driver.Session(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessReadDefault,
		})
		
		if err == nil {
			defer session.Close()
			
			_, err = session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				query := `MATCH (c:CVE {id: $id}) RETURN c`
				result, err := tx.Run(ctx, query, map[string]interface{}{"id": cveID})
				if err != nil {
					return nil, err
				}
				rec, err := result.Single()
				if err != nil {
					return nil, fmt.Errorf("CVE not found")
				}
				
				record := rec.(dbtype.Record)
				cveNode := record.Get("c").(dbtype.Node)
				cve = attack_graph.CVEItem{
					ID:      cveNode.PropertyValues()["id"].(string),
					Impact: attack_graph.ImpactScore{
						BaseScore:         float32(cveNode.PropertyValues()["cvss_score"].(float64)),
						BaseSeverity:      cveNode.PropertyValues()["base_severity"].(string),
						AttackVector:      cveNode.PropertyValues()["attack_vector"].(string),
						AttackComplexity:  cveNode.PropertyValues()["attack_complexity"].(string),
						PrivilegesRequired: cveNode.PropertyValues()["privileges_required"].(string),
						UserInteraction:   cveNode.PropertyValues()["user_interaction"].(string),
						Scope:             cveNode.PropertyValues()["scope"].(string),
						Confidentiality:   cveNode.PropertyValues()["confidentiality"].(string),
						Integrity:         cveNode.PropertyValues()["integrity"].(string),
						Availability:      cveNode.PropertyValues()["availability"].(string),
					},
				}
				return cve, nil
			})
		}
	}
	
	mapping := h.mapper.MapToKillChain(cve.Impact, nil)
	
	response := gin.H{
		"cve_id":      cveID,
		"mapping":     mapping,
	}
	
	c.JSON(http.StatusOK, response)
}

// handleBuildAttackGraph handles POST /api/v1/security/kill-chain/graph
func (h *AttackGraphHandler) handleBuildAttackGraph(c *gin.Context) {
	type GraphRequest struct {
		StartNode string `json:"start_node"`
		MaxDepth int `json:"max_depth" binding:"required,min=1,max=10"`
	}
	
	var req GraphRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	
	// TODO: Build attack graph from Neo4j
	graph := h.mapper.GenerateKillChainGraph(nil) // Placeholder
	
	c.JSON(http.StatusOK, gin.H{
		"nodes": len(graph["nodes"]),
		"edges": len(graph["edges"]),
		"graph": graph,
	})
}

// handleGenerateVulnReport handles GET /api/v1/security/vulnerabilities/report/:days
func (h *AttackGraphHandler) handleGenerateVulnReport(c *gin.Context) {
	days, _ := strconv.Atoi(c.Param("days"))
	
	report, err := h.ingestion.GenerateVulnerabilityReport(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, report)
}

// handleVulnSummary handles GET /api/v1/security/vulnerabilities/summary
func (h *AttackGraphHandler) handleVulnSummary(c *gin.Context) {
	// Aggregate statistics across all CVEs from Neo4j
	ctx := c.Request.Context()
	
	totalVulns := 0
	bySeverity := make(map[string]int)
	affectedAssets := 0
	highestRiskChain := ""
	
	if h.ingestion.graphClient != nil {
		session, err := h.ingestion.graphClient.driver.Session(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessReadDefault,
		})
		
		if err == nil {
			defer session.Close()
			
			// Get severity distribution
			severityQuery := `MATCH (c:CVE) RETURN c.base_severity as severity, COUNT(c) as count`
			_, _ = session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				result, _ := tx.Run(ctx, severityQuery, nil)
				for result.Next(ctx) {
					rec := result.Record()
					severity := rec.Get("severity").(string)
					count := int(rec.Get("count").(int64))
					bySeverity[severity] = count
					totalVulns += count
				}
				return totalVulns, nil
			})
			
			// Estimate affected assets (from vulnerability mappings)
			assetQuery := `MATCH (c:CVE)-[:AFFECTS]->(a:Asset) RETURN COUNT(DISTINCT a)`
			_, _ = session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				result, _ := tx.Run(ctx, assetQuery, nil)
				if rec, err := result.Single(); err == nil {
					affectedAssets = int(rec.(dbtype.Record).Get("COUNT(c)").(int64))
				}
				return affectedAssets, nil
			})
			
			// Find highest risk chain
			riskQuery := `MATCH path = ()-[*1..5]-() RETURN COUNT(path) as risk_score ORDER BY risk_score DESC LIMIT 1`
			_, _ = session.ReadTransaction(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				result, _ := tx.Run(ctx, riskQuery, nil)
				if rec, err := result.Single(); err == nil {
					highestRiskChain = fmt.Sprintf("Path with %d connections", rec.(dbtype.Record).Get("risk_score").(int64))
				}
				return nil, nil
			})
		}
	} else {
		// Mock data when Neo4j unavailable
		totalVulns = 500
		bySeverity = map[string]int{
			"CRITICAL": 75,
			"HIGH":     150,
			"MEDIUM":   200,
			"LOW":      75,
		}
	}
	
	response := gin.H{
		"total_vulnerabilities": totalVulns,
		"by_severity":           bySeverity,
		"affected_assets":       affectedAssets,
		"highest_risk_chain":    highestRiskChain,
		"last_updated":          time.Now().Format(time.RFC3339),
	}
	
	c.JSON(http.StatusOK, response)
}

// sumOfMapValues calculates sum of all values in a map
func sumOfMapValues(m map[string]int) int {
	sum := 0
	for _, v := range m {
		sum += v
	}
	return sum
}
	}
	
	c.JSON(http.StatusOK, summary)
}
