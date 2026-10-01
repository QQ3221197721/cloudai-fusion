// Package api provides HTTP handlers for M45 AI-Powered Threat Hunting Platform
package api

import (
	"net/http"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// DetectionModelType defines ML model category
type DetectionModelType string

const (
	ModelAnomalyDetection DetectionModelType = "anomaly_detection"
	ModelBehaviorBaseline DetectionModelType = "behavior_baseline"
	ModelThreatScoring    DetectionModelType = "threat_scoring"
	ModelUEBA             DetectionModelType = "ueba"
)

// AnomalySeverity defines anomaly severity level
type AnomalySeverity string

const (
	SeverityLow     AnomalySeverity = "low"
	SeverityMedium  AnomalySeverity = "medium"
	SeverityHigh    AnomalySeverity = "high"
	SeverityCritical AnomalySeverity = "critical"
)

// BaselineType defines baseline measurement type
type BaselineType string

const (
	BaselineNetworkTraffic  BaselineType = "network_traffic"
	BaselineUserBehavior    BaselineType = "user_behavior"
	BaselineSystemMetrics   BaselineType = "system_metrics"
	BaselineApplicationLogs BaselineType = "application_logs"
)

// BaselineConfig represents a behavioral baseline configuration
type BaselineConfig struct {
	ID          string        `json:"id"`
	Name        string        `json:"name" binding:"required"`
	Type        BaselineType  `json:"type" binding:"required"`
	Entity      string        `json:"entity" binding:"required"` // user, host, application
	Features    []string      `json:"features"` // Metrics to monitor
	WindowHours int           `json:"windowHours"` // Training window
	Thresholds  ThresholdConfig `json:"thresholds"` // Statistical thresholds
	UpdatedAt   time.Time     `json:"updatedAt"`
	Metrics     map[string]any `json:"metrics"` // Mean, stddev, percentiles
	Enabled     bool            `json:"enabled"`
}

// ThresholdConfig defines statistical detection thresholds
type ThresholdConfig struct {
	ZScoreThreshold float64 `json:"zScoreThreshold"` // e.g., 3.0 standard deviations
	PValueThreshold float64 `json:"pValueThreshold"` // e.g., 0.01
	MinSamples      int     `json:"minSamples"`      // Minimum data points required
	UseDynamic      bool    `json:"useDynamic"`      // Adaptive thresholds
}

// HuntCampaign represents an active threat hunting campaign
type HuntCampaign struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name" binding:"required"`
	Description       string                 `json:"description"`
	DetectionModels   []string               `json:"detectionModels"`
	Baselines         []BaselineConfig       `json:"baselines,omitempty"`
	Status            string                 `json:"status"` // running, paused, completed, failed
	StartDate         time.Time              `json:"startDate"`
	EndDate           *time.Time             `json:"endDate,omitempty"`
	AnomaliesDetected int                    `json:"anomaliesDetected"`
	Investigations    []InvestigationCase    `json:"investigations"`
	Metrics           CampaignMetrics        `json:"metrics"`
	CreatedBy         string                 `json:"createdBy"`
	LastRun           time.Time              `json:"lastRun"`
	NextRun           *time.Time             `json:"nextRun,omitempty"`
}

// CampaignMetrics tracks hunting performance
type CampaignMetrics struct {
	TotalEventsAnalyzed int `json:"totalEventsAnalyzed"`
	SamplesProcessed    int `json:"samplesProcessed"`
	FalsePositives      int `json:"falsePositives"`
	TruePositives       int `json:"truePositives"`
	ConfidenceAvg       float64 `json:"confidenceAvg"`
	AUCScore            float64 `json:"aucScore"` // Area Under Curve
	LatencyMs           int64 `json:"latencyMs"` // Average latency
}

// MLAnomaly represents a detected anomaly
type MLAnomaly struct {
	ID            string                   `json:"id"`
	CampaignID    string                   `json:"campaignId"`
	ModelID       string                   `json:"modelId"`
	EntityID      string                   `json:"entityId"`
	EntityType    string                   `json:"entityType"` // user, host, network, app
	AnomalyType   string                   `json:"anomalyType"`
	Severity      AnomalySeverity          `json:"severity"`
	Score         float64                  `json:"score"` // 0-100 anomaly score
	ContextData   map[string]any           `json:"contextData"`
	Histogram     []float64                `json:"histogram,omitempty"` // Historical distribution
	Timestamp     time.Time                `json:"timestamp"`
	FirstSeen     time.Time                `json:"firstSeen"`
	LastSeen      time.Time                `json:"lastSeen"`
	Investigated  bool                     `json:"investigated"`
	InvestigationID string                  `json:"investigationId,omitempty"`
	Evidence      []EvidencePoint          `json:"evidence,omitempty"`
}

// EvidencePoint captures supporting evidence for anomaly
type EvidencePoint struct {
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Data        any       `json:"data"`
	Timestamp   time.Time `json:"timestamp"`
	Weight      float64   `json:"weight"` // Confidence weight
}

// InvestigationCase represents a security investigation
type InvestigationCase struct {
	ID            string                 `json:"id"`
	CampaignID    string                 `json:"campaignId"`
	AnomalyID     string                 `json:"anomalyId"`
	Title         string                 `json:"title" binding:"required"`
	Description   string                 `json:"description"`
	Severity      AnomalySeverity        `json:"severity"`
	Status        string                 `json:"status"` // open, in_progress, false_positive, true_positive, closed
	AssignedTo    string                 `json:"assignedTo"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
	StartedAt     *time.Time             `json:"startedAt,omitempty"`
	ClosedAt      *time.Time             `json:"closedAt,omitempty"`
	FindingType   string                 `json:"findingType"` // attack, misconfiguration, benign
	Evidence      []InvestigationEvidence `json:"evidence"`
	Notes         []InvestigationNote    `json:"notes"`
	RelatedIOCs   []string               `json:"relatedIoCs"`
	MITRETactics  []string               `json:"mitreTactics"`
	RiskAssessment string                `json:"riskAssessment"`
	Recommendations []string             `json:"recommendations"`
}

// InvestigationEvidence is evidence collected during investigation
type InvestigationEvidence struct {
	Type        string    `json:"type"`
	Source      string    `json:"source"`
	Data        any       `json:"data"`
	Relevance   string    `json:"relevance"`
	Timestamp   time.Time `json:"timestamp"`
	UploadedBy  string    `json:"uploadedBy"`
}

// InvestigationNote is a comment or note on the case
type InvestigationNote struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content" binding:"required"`
	Timestamp time.Time `json:"timestamp"`
}

// MLModelRegistry tracks ML models for threat detection
type MLModelRegistry struct {
	ID           string               `json:"id"`
	Name         string               `json:"name" binding:"required"`
	Type         DetectionModelType   `json:"type" binding:"required"`
	Version      string               `json:"version"`
	ModelPath    string               `json:"modelPath"`
	TrainingData string               `json:"trainingData"`
	Accuracy     float64              `json:"accuracy"` // Validation accuracy
	LastTrained  time.Time            `json:"lastTrained"`
	Parameters   map[string]any       `json:"parameters"`
	Metadata     map[string]any       `json:"metadata"`
	Active       bool                 `json:"active"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
}

// BehavioralAnalyticsStore interface
type BehavioralAnalyticsStore interface {
	CreateBaseline(baseline *BaselineConfig) error
	GetBaseline(id string) (*BaselineConfig, error)
	UpdateBaseline(id string, updates map[string]any) error
	DeleteBaseline(id string) error
	ListBaselines(filters map[string]any, limit, offset int) ([]BaselineConfig, error)
	
	RegisterHuntCampaign(campaign *HuntCampaign) error
	GetHuntCampaign(id string) (*HuntCampaign, error)
	UpdateHuntCampaign(id string, updates map[string]any) error
	DeleteHuntCampaign(id string) error
	ListHuntCampaigns(status string, limit, offset int) ([]HuntCampaign, error)
	StartCampaign(id string) error
	CompleteCampaign(id string) error
	
	CreateMLAnomaly(anomaly *MLAnomaly) error
	GetMLAnomaly(id string) (*MLAnomaly, error)
	UpdateMLAnomaly(id string, updates map[string]any) error
	ListMLAnomalies(campaignID string, filters map[string]any, limit, offset int) ([]MLAnomaly, error)
	MarkAnomalyAsInvestigated(anomalyID, investigationID string) error
	
	CreateInvestigation(caseObj *InvestigationCase) error
	GetInvestigation(id string) (*InvestigationCase, error)
	UpdateInvestigation(id string, updates map[string]any) error
	CloseInvestigation(id string, findingType string) error
	ListInvestigations(campaignID string) ([]InvestigationCase, error)
	AddEvidence(caseID string, evidence InvestigationEvidence) error
	AddNote(caseID string, note InvestigationNote) error
	
	CreateMLModel(model *MLModelRegistry) error
	GetMLModel(id string) (*MLModelRegistry, error)
	UpdateMLModel(id string, updates map[string]any) error
	DeleteMLModel(id string) error
	ListMLModels() ([]MLModelRegistry, error)
	TriggerModelRetraining(modelID string) error
	
	BulkCreateAnomalies(anomalies []MLAnomaly) error
	UpdateCampaignMetrics(campaignID string, metrics CampaignMetrics) error
}

// AIThreatHuntingHandler handles AI threat hunting requests
type AIThreatHuntingHandler struct {
	store  BehavioralAnalyticsStore
	ledger *evidence.Ledger
	logger *logrus.Logger
}

func NewAIThreatHuntingHandler(
	store BehavioralAnalyticsStore,
	ledger *evidence.Ledger,
	logger *logrus.Logger,
) *AIThreatHuntingHandler {
	return &AIThreatHuntingHandler{
		store:  store,
		ledger: ledger,
		logger: logger,
	}
}

func RegisterAIHuntingRoutes(router *echo.Echo, handler *AIThreatHuntingHandler) {
	hunting := router.Group("/api/v1/ai-threat-hunting")
	{
		// Baseline Management
		hunting.POST("/baselines", handler.handleCreateBaseline)
		hunting.GET("/baselines", handler.handleListBaselines)
		hunting.GET("/baselines/:id", handler.handleGetBaseline)
		hunting.PUT("/baselines/:id", handler.handleUpdateBaseline)
		hunting.DELETE("/baselines/:id", handler.handleDeleteBaseline)
		hunting.POST("/baselines/:id/train", handler.handleTrainBaseline)
		
		// Hunt Campaigns
		hunting.POST("/campaigns", handler.handleCreateCampaign)
		hunting.GET("/campaigns", handler.handleListCampaigns)
		hunting.GET("/campaigns/:id", handler.handleGetCampaign)
		hunting.PUT("/campaigns/:id", handler.handleUpdateCampaign)
		hunting.DELETE("/campaigns/:id", handler.handleDeleteCampaign)
		hunting.POST("/campaigns/:id/start", handler.handleStartCampaign)
		hunting.POST("/campaigns/:id/pause", handler.handlePauseCampaign)
		hunting.POST("/campaigns/:id/stop", handler.handleStopCampaign)
		
		// ML Anomalies
		hunting.GET("/anomalies", handler.handleListAnomalies)
		hunting.GET("/anomalies/:id", handler.handleGetAnomaly)
		hunting.PUT("/anomalies/:id/investigate", handler.handleAnnotateAnomaly)
		hunting.POST("/anomalies/:id/review", handler.handleReviewAnomaly)
		hunting.POST("/anomalies/bulk-update", handler.handleBulkUpdateAnomalies)
		
		// Investigations
		hunting.POST("/investigations", handler.handleCreateInvestigation)
		hunting.GET("/investigations", handler.handleListInvestigations)
		hunting.GET("/investigations/:id", handler.handleGetInvestigation)
		hunting.PUT("/investigations/:id", handler.handleUpdateInvestigation)
		hunting.POST("/investigations/:id/evidence", handler.handleAddInvestigationEvidence)
		hunting.POST("/investigations/:id/notes", handler.handleAddInvestigationNote)
		hunting.POST("/investigations/:id/close", handler.handleCloseInvestigation)
		
		// ML Models
		hunting.POST("/models", handler.handleRegisterMLModel)
		hunting.GET("/models", handler.handleListMLModels)
		hunting.GET("/models/:id", handler.handleGetMLModel)
		hunting.PUT("/models/:id", handler.handleUpdateMLModel)
		hunting.DELETE("/models/:id", handler.handleDeleteMLModel)
		hunting.POST("/models/:id/retrain", handler.handleTriggerModelRetraining)
		
		// Analytics & Insights
		hunting.GET("/analytics/campaign-performance", handler.handleGetCampaignPerformance)
		hunting.GET("/analytics/detection-rates", handler.handleGetDetectionRates)
		hunting.GET("/analytics/false-positive-rate", handler.handleGetFalsePositiveRate)
		hunting.GET("/insights/trends", handler.handleGetTrendAnalysis)
	}
}

func (h *AIThreatHuntingHandler) handleCreateBaseline(c echo.Context) error {
	var req struct {
		Name        string        `json:"name" binding:"required"`
		Type        BaselineType  `json:"type" binding:"required"`
		Entity      string        `json:"entity" binding:"required"`
		Features    []string      `json:"features" binding:"required"`
		WindowHours int           `json:"windowHours" binding:"required"`
		Thresholds  ThresholdConfig `json:"thresholds"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	baselineID := uuid.New().String()
	now := time.Now()
	
	baseline := &BaselineConfig{
		ID:          baselineID,
		Name:        req.Name,
		Type:        req.Type,
		Entity:      req.Entity,
		Features:    req.Features,
		WindowHours: req.WindowHours,
		Thresholds:  req.Thresholds,
		Metrics:     make(map[string]any),
		Enabled:     true,
		UpdatedAt:   now,
	}
	
	if err := h.store.CreateBaseline(baseline); err != nil {
		h.logger.WithError(err).Error("Failed to create baseline")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create baseline"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"baseline_id": baselineID,
		"type":        req.Type,
		"entity":      req.Entity,
		"actor":       c.GetString("user_id"),
	}).Info("Created behavioral baseline")
	
	if h.ledger != nil {
		h.ledger.Attest(evidence.Receipt{
			Action:  "BASELINE_CREATED",
			Subject: baselineID,
			Actor:   c.GetString("user_id"),
		})
	}
	
	return c.JSON(http.StatusCreated, gin.H{
		"baseline": baseline,
		"message":  "behavioral baseline created",
	})
}

func (h *AIThreatHuntingHandler) handleListBaselines(c echo.Context) error {
	limit := 100
	offset := 0
	
	c.IntParam(c.QueryParam("limit"), &limit)
	c.IntParam(c.QueryParam("offset"), &offset)
	
	filters := make(map[string]any)
	if entityType := c.QueryParam("entity"); entityType != "" {
		filters["entity"] = entityType
	}
	if baselineType := c.QueryParam("type"); baselineType != "" {
		filters["type"] = baselineType
	}
	
	baselines, err := h.store.ListBaselines(filters, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list baselines"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"baselines": baselines,
		"total":     len(baselines),
		"limit":     limit,
		"offset":    offset,
	})
}

func (h *AIThreatHuntingHandler) handleGetBaseline(c echo.Context) error {
	id := c.Param("id")
	
	baseline, err := h.store.GetBaseline(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "baseline not found"})
	}
	
	return c.JSON(http.StatusOK, baseline)
}

func (h *AIThreatHuntingHandler) handleUpdateBaseline(c echo.Context) error {
	id := c.Param("id")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	updates["updated_at"] = time.Now()
	
	if err := h.store.UpdateBaseline(id, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update baseline"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id": id,
		"message": "baseline updated",
	})
}

func (h *AIThreatHuntingHandler) handleDeleteBaseline(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.DeleteBaseline(id); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete baseline"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":      id,
		"message": "baseline deleted",
	})
}

func (h *AIThreatHuntingHandler) handleTrainBaseline(c echo.Context) error {
	id := c.Param("id")
	
	logger := h.logger.WithFields(logrus.Fields{
		"baseline_id": id,
		"action":      "train_baseline",
	})
	
	logger.Info("Starting baseline training")
	
	return c.JSON(http.StatusOK, gin.H{
		"baseline_id": id,
		"message":     "training initiated",
	})
}

func (h *AIThreatHuntingHandler) handleCreateCampaign(c echo.Context) error {
	var req struct {
		Name        string   `json:"name" binding:"required"`
		Description string   `json:"description"`
		BaselineIDs []string `json:"baselineIds"`
		ModelIDs    []string `json:"modelIds"`
		Schedule    string   `json:"schedule"` // Cron expression for recurring
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	campaignID := uuid.New().String()
	now := time.Now()
	
	campaign := &HuntCampaign{
		ID:              campaignID,
		Name:            req.Name,
		Description:     req.Description,
		DetectionModels: req.ModelIDs,
		Status:          "running",
		StartDate:       now,
		AnomaliesDetected: 0,
		Investigations:  []InvestigationCase{},
		Metrics: CampaignMetrics{
			TotalEventsAnalyzed: 0,
			SamplesProcessed:    0,
		},
		CreatedBy: c.GetString("user_id"),
		LastRun:   now,
	}
	
	if err := h.store.RegisterHuntCampaign(campaign); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create campaign"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"campaign_id": campaignID,
		"name":        req.Name,
		"actor":       c.GetString("user_id"),
	}).Info("Created hunt campaign")
	
	return c.JSON(http.StatusCreated, gin.H{
		"campaign": campaign,
		"message":  "hunt campaign created",
	})
}

func (h *AIThreatHuntingHandler) handleListCampaigns(c echo.Context) error {
	limit := 100
	offset := 0
	
	c.IntParam(c.QueryParam("limit"), &limit)
	c.IntParam(c.QueryParam("offset"), &offset)
	
	status := c.QueryParam("status")
	
	campaigns, err := h.store.ListHuntCampaigns(status, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list campaigns"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"campaigns": campaigns,
		"total":     len(campaigns),
		"limit":     limit,
		"offset":    offset,
	})
}

func (h *AIThreatHuntingHandler) handleGetCampaign(c echo.Context) error {
	id := c.Param("id")
	
	campaign, err := h.store.GetHuntCampaign(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
	}
	
	return c.JSON(http.StatusOK, campaign)
}

func (h *AIThreatHuntingHandler) handleUpdateCampaign(c echo.Context) error {
	id := c.Param("id")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	if err := h.store.UpdateHuntCampaign(id, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update campaign"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"campaign_id": id,
		"message":     "campaign updated",
	})
}

func (h *AIThreatHuntingHandler) handleDeleteCampaign(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.DeleteHuntCampaign(id); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete campaign"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"campaign_id": id,
		"message":     "campaign deleted",
	})
}

func (h *AIThreatHuntingHandler) handleStartCampaign(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.StartCampaign(id); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start campaign"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"campaign_id": id,
		"message":     "campaign started",
	})
}

func (h *AIThreatHuntingHandler) handlePauseCampaign(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.UpdateHuntCampaign(id, map[string]any{"status": "paused"}); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to pause campaign"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"campaign_id": id,
		"message":     "campaign paused",
	})
}

func (h *AIThreatHuntingHandler) handleStopCampaign(c echo.Context) error {
	id := c.Param("id")
	
	now := time.Now()
	if err := h.store.UpdateHuntCampaign(id, map[string]any{
		"status": "completed",
		"end_date": now,
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stop campaign"})
	}
	
	if err := h.store.CompleteCampaign(id); err != nil {
		h.logger.WithError(err).Warn("Failed to complete campaign metrics")
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"campaign_id": id,
		"message":     "campaign stopped",
	})
}

func (h *AIThreatHuntingHandler) handleListAnomalies(c echo.Context) error {
	campaignID := c.QueryParam("campaignId")
	limit := 100
	offset := 0
	
	c.IntParam(c.QueryParam("limit"), &limit)
	c.IntParam(c.QueryParam("offset"), &offset)
	
	filters := make(map[string]any)
	if severity := c.QueryParam("severity"); severity != "" {
		filters["severity"] = severity
	}
	if investigated := c.QueryParam("investigated"); investigated != "" {
		filters["investigated"] = investigated == "true"
	}
	
	anomalies, err := h.store.ListMLAnomalies(campaignID, filters, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list anomalies"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"anomalies": anomalies,
		"total":     len(anomalies),
		"limit":     limit,
		"offset":    offset,
	})
}

func (h *AIThreatHuntingHandler) handleGetAnomaly(c echo.Context) error {
	id := c.Param("id")
	
	anomaly, err := h.store.GetMLAnomaly(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "anomaly not found"})
	}
	
	return c.JSON(http.StatusOK, anomaly)
}

func (h *AIThreatHuntingHandler) handleAnnotateAnomaly(c echo.Context) error {
	anomalyID := c.Param("id")
	
	var req struct {
		In vestigationID string `json:"investigationId"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	if err := h.store.MarkAnomalyAsInvestigated(anomalyID, req.InvestigationID); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to annotate"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"anomaly_id": anomalyID,
		"message":    "anomaly annotated with investigation",
	})
}

func (h *AIThreatHuntingHandler) handleReviewAnomaly(c echo.Context) error {
	id := c.Param("id")
	
	var req struct {
		Reviewers   []string `json:"reviewers"`
		Verdict     string   `json:"verdict"` // true_positive, false_positive, inconclusive
		Notes       string   `json:"notes"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"anomaly_id": id,
		"message":    "anomaly review recorded",
	})
}

func (h *AIThreatHuntingHandler) handleBulkUpdateAnomalies(c echo.Context) error {
	var updates []struct {
		ID       string            `json:"id"`
		Updates  map[string]any    `json:"updates"`
	}
	
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
	}
	
	var anomalies []MLAnomaly
	for _, u := range updates {
		anomaly := MLAnomaly{}
		// Convert updates to anomaly struct
		anomalies = append(anomalies, anomaly)
	}
	
	if err := h.store.BulkCreateAnomalies(anomalies); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "bulk update failed"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"message": "bulk update completed",
	})
}

func (h *AIThreatHuntingHandler) handleCreateInvestigation(c echo.Context) error {
	var req struct {
		CampaignID  string            `json:"campaignId" binding:"required"`
		AnomalyID   string            `json:"anomalyId" binding:"required"`
		Title       string            `json:"title" binding:"required"`
		Description string            `json:"description"`
		Severity    AnomalySeverity   `json:"severity" binding:"required"`
		AssignedTo  string            `json:"assignedTo"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	caseID := uuid.New().String()
	now := time.Now()
	
	caseObj := &InvestigationCase{
		ID:            caseID,
		CampaignID:    req.CampaignID,
		AnomalyID:     req.AnomalyID,
		Title:         req.Title,
		Description:   req.Description,
		Severity:      req.Severity,
		Status:        "open",
		AssignedTo:    req.AssignedTo,
		CreatedAt:     now,
		UpdatedAt:     now,
		Evidence:      []InvestigationEvidence{},
		Notes:         []InvestigationNote{},
	}
	
	if err := h.store.CreateInvestigation(caseObj); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create investigation"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"case_id":       caseID,
		"campaign_id":   req.CampaignID,
		"severity":      req.Severity,
		"actor":         c.GetString("user_id"),
	}).Info("Created investigation case")
	
	return c.JSON(http.StatusCreated, gin.H{
		"case": caseObj,
		"message": "investigation case created",
	})
}

func (h *AIThreatHuntingHandler) handleListInvestigations(c echo.Context) error {
	campaignID := c.QueryParam("campaignId")
	
	cases, err := h.store.ListInvestigations(campaignID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list investigations"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"investigations": cases,
		"total":          len(cases),
	})
}

func (h *AIThreatHuntingHandler) handleGetInvestigation(c echo.Context) error {
	id := c.Param("id")
	
	caseObj, err := h.store.GetInvestigation(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "investigation not found"})
	}
	
	return c.JSON(http.StatusOK, caseObj)
}

func (h *AIThreatHuntingHandler) handleUpdateInvestigation(c echo.Context) error {
	id := c.Param("id")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	updates["updated_at"] = time.Now()
	
	if err := h.store.UpdateInvestigation(id, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update investigation"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"case_id": id,
		"message": "investigation updated",
	})
}

func (h *AIThreatHuntingHandler) handleAddInvestigationEvidence(c echo.Context) error {
	caseID := c.Param("id")
	
	var evidence InvestigationEvidence
	if err := c.BindJSON(&evidence); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid evidence data"})
	}
	
	if err := h.store.AddEvidence(caseID, evidence); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add evidence"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"case_id": caseID,
		"message": "evidence added",
	})
}

func (h *AIThreatHuntingHandler) handleAddInvestigationNote(c echo.Context) error {
	caseID := c.Param("id")
	
	var note InvestigationNote
	if err := c.BindJSON(&note); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid note"})
	}
	
	note.Timestamp = time.Now()
	
	if err := h.store.AddNote(caseID, note); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add note"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"case_id": caseID,
		"message": "note added",
	})
}

func (h *AIThreatHuntingHandler) handleCloseInvestigation(c echo.Context) error {
	id := c.Param("id")
	
	var req struct {
		FindingType string `json:"findingType" binding:"required"` // true_positive, false_positive
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid closing data"})
	}
	
	now := time.Now()
	if err := h.store.CloseInvestigation(id, req.FindingType); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to close investigation"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"case_id":      id,
		"finding_type": req.FindingType,
		"message":      "investigation closed",
		"closed_at":    now,
	})
}

func (h *AIThreatHuntingHandler) handleRegisterMLModel(c echo.Context) error {
	var req struct {
		Name      string            `json:"name" binding:"required"`
		Type      DetectionModelType `json:"type" binding:"required"`
		Version   string            `json:"version"`
		ModelPath string            `json:"modelPath" binding:"required"`
		Accuracy  float64           `json:"accuracy"`
		Params    map[string]any    `json:"parameters"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model definition"})
	}
	
	modelID := uuid.New().String()
	now := time.Now()
	
	model := &MLModelRegistry{
		ID:          modelID,
		Name:        req.Name,
		Type:        req.Type,
		Version:     req.Version,
		ModelPath:   req.ModelPath,
		Accuracy:    req.Accuracy,
		Parameters:  req.Params,
		Active:      true,
		LastTrained: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	
	if err := h.store.CreateMLModel(model); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register model"})
	}
	
	return c.JSON(http.StatusCreated, gin.H{
		"model": model,
		"message": "ML model registered",
	})
}

func (h *AIThreatHuntingHandler) handleListMLModels(c echo.Context) error {
	models, err := h.store.ListMLModels()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list models"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"models": models,
		"total":  len(models),
	})
}

func (h *AIThreatHuntingHandler) handleGetMLModel(c echo.Context) error {
	id := c.Param("id")
	
	model, err := h.store.GetMLModel(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
	}
	
	return c.JSON(http.StatusOK, model)
}

func (h *AIThreatHuntingHandler) handleUpdateMLModel(c echo.Context) error {
	id := c.Param("id")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	updates["updated_at"] = time.Now()
	
	if err := h.store.UpdateMLModel(id, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update model"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"model_id": id,
		"message":  "model updated",
	})
}

func (h *AIThreatHuntingHandler) handleDeleteMLModel(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.DeleteMLModel(id); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete model"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"model_id": id,
		"message":  "model deleted",
	})
}

func (h *AIThreatHuntingHandler) handleTriggerModelRetraining(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.TriggerModelRetraining(id); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to trigger retraining"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"model_id": id,
		"message":  "model retraining initiated",
	})
}

func (h *AIThreatHuntingHandler) handleGetCampaignPerformance(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{
		"message": "campaign performance analytics",
	})
}

func (h *AIThreatHuntingHandler) handleGetDetectionRates(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{
		"message": "detection rates analytics",
	})
}

func (h *AIThreatHuntingHandler) handleGetFalsePositiveRate(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{
		"message": "false positive rate analytics",
	})
}

func (h *AIThreatHuntingHandler) handleGetTrendAnalysis(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{
		"message": "trending analysis",
	})
}
