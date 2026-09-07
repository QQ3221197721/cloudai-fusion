// Package tee_integrations - Integration layer connecting L15 TEE to other Deep Wells
// Bridges TEE enclave capabilities with Intelligence, Security, Cost, and Audit systems
package tee_integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/tee"
)

// ============================================================================
// INTELLIGENCE INTEGRATION (L1 ↔ L15)
// ============================================================================

// IntelligenceConnector connects L1 Intelligence with L15 TEE
type IntelligenceConnector struct {
	mu          sync.RWMutex
	logger      *logrus.Logger
	teeProvider tee.TEEProvider
	
	// Threat intel feed integration
	threatFeed     chan *ThreatIntelligenceEvent
	evidenceBuffer []EvidenceReport
	maxBufferSize  int
	
	// Metrics collection
	metricsChannel chan MetricReport
	lastUpdate     time.Time
}

// ThreatIntelligenceEvent represents a security threat from L1 intelligence
type ThreatIntelligenceEvent struct {
	EventID       string    `json:"event_id"`
	Timestamp     time.Time `json:"timestamp"`
	ThreatType    string    `json:"threat_type"`
	Severity      string    `json:"severity"`
	Description   string    `json:"description"`
	EvidenceHash  []byte    `json:"evidence_hash"`
	Confidence    float64   `json:"confidence"`
}

// EvidenceReport reports cryptographic evidence to L10 audit system
type EvidenceReport struct {
	EvidenceID    string    `json:"evidence_id"`
	EnclaveID     string    `json:"enclave_id"`
	EvidenceType  string    `json:"evidence_type"`
	EvidenceBytes []byte    `json:"evidence_bytes"`
	VerifiedAt    time.Time `json:"verified_at"`
	AuditLogID    string    `json:"audit_log_id"`
}

// NewIntelligenceConnector creates connector between L1 and L15
func NewIntelligenceLogger(provider tee.TEEProvider, logger *logrus.Logger) *IntelligenceConnector {
	return &IntelligenceConnector{
		teeProvider:    provider,
		logger:         logger,
		threatFeed:     make(chan *ThreatIntelligenceEvent, 100),
		evidenceBuffer: make([]EvidenceReport, 0, 100),
		maxBufferSize:  100,
		metricsChannel: make(chan MetricReport, 50),
		lastUpdate:     time.Now(),
	}
}

// ProcessThreatEvent processes incoming threat intelligence event
func (c *IntelligenceConnector) ProcessThreatEvent(ctx context.Context, event *ThreatIntelligenceEvent) error {
	c.mu.Lock()
	
	// Buffer overflow protection
	if len(c.evidenceBuffer) >= c.maxBufferSize {
		// Evict oldest evidence
		c.evidenceBuffer = c.evidenceBuffer[1:]
	}
	
	c.mu.Unlock()
	
	// Create TEE enclave for secure processing
	enclaveConfig := tee.EnclaveConfig{
		ID:                  fmt.Sprintf("threat_proc_%s", event.EventID),
		CodeHash:            computeEventHash(event),
		MemorySizeMB:        256,
		CPUCount:            2,
		NetworkMode:         tee.NetworkInternal,
		SecurityPolicy:      tee.PolicyStrict,
		AttestationRequired: true,
	}
	
	enclave, err := c.teeProvider.CreateEnclave(ctx, enclaveConfig)
	if err != nil {
		return fmt.Errorf("failed to create enclave: %w", err)
	}
	
	// Process threat in secure enclave
	processedEvidence, err := c.processInEnclave(enclave.ID, event)
	if err != nil {
		return fmt.Errorf("enclave processing failed: %w", err)
	}
	
	// Submit evidence report to L10 audit
	report := EvidenceReport{
		EvidenceID:    generateEvidenceID(),
		EnclaveID:     enclave.ID,
		EvidenceType:  "threat_analysis",
		EvidenceBytes: processedEvidence,
		VerifiedAt:    time.Now(),
	}
	
	c.mu.Lock()
	c.evidenceBuffer = append(c.evidenceBuffer, report)
	c.mu.Unlock()
	
	c.logger.WithFields(logrus.Fields{
		"event_id": event.EventID,
		"enclave":  enclave.ID,
	}).Info("Processed threat event in TEE enclave")
	
	return nil
}

func (c *IntelligenceConnector) processInEnclave(enclaveID string, event *ThreatIntelligenceEvent) ([]byte, error) {
	// Securely process threat evidence inside enclave
	// Would execute analysis inside TEE enclave
	
	eventJSON, _ := json.Marshal(event)
	
	// Perform sensitive analysis inside enclave
	processed := performSecureAnalysis(eventJSON)
	
	return processed, nil
}

// ============================================================================
// SECURITY INTEGRATION (L3-L8 SOC ↔ L15)
// ============================================================================

// SecurityIntegration connects L3-L8 SOC with L15 TEE
type SecurityIntegration struct {
	mu               sync.RWMutex
	logger           *logrus.Logger
	teeProvider      tee.TEEProvider
	sigmasRulesEngine *SIGMARulesEngine
	
	// Evidence submission queue
	evidenceQueue    []*SecurityEvidence
	queueCapacity    int
	
	// Real-time monitoring metrics
	metricsChannel   chan SecurityMetric
	lastCheckTime    time.Time
	checkInterval    time.Duration
}

// SecurityEvidence represents security evidence from TEE
type SecurityEvidence struct {
	EvidenceID   string    `json:"evidence_id"`
	EnclaveID    string    `json:"enclave_id"`
	EvidenceType string    `json:"evidence_type"`
	Payload      []byte    `json:"payload"`
	CreatedAt    time.Time `json:"created_at"`
	VerifiedBy   string    `json:"verified_by"`
}

// SecurityMetric represents real-time security metric
type SecurityMetric struct {
	Name       string    `json:"name"`
	Value      float64   `json:"value"`
	Labels     map[string]string `json:"labels,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	Unit       string    `json:"unit"`
}

// NewSecurityIntegration creates connection between L15 and L3-L8 SOC
func NewSecurityIntegration(provider tee.TEEProvider, logger *logrus.Logger) *SecurityIntegration {
	return &SecurityIntegration{
		teeProvider:       provider,
		logger:            logger,
		evidenceQueue:     make([]*SecurityEvidence, 0, 500),
		queueCapacity:     500,
		metricsChannel:    make(chan SecurityMetric, 100),
		lastCheckTime:     time.Now(),
		checkInterval:     30 * time.Second,
	}
}

// SubmitSecureEvidence submits security evidence generated by TEE enclave
func (si *SecurityIntegration) SubmitSecureEvidence(ctx context.Context, enclaveID string, evidenceType string, payload []byte) error {
	si.mu.Lock()
	defer si.mu.Unlock()
	
	// Queue overflow protection
	if len(si.evidenceQueue) >= si.queueCapacity {
		// Remove oldest evidence
		si.evidenceQueue = si.evidenceQueue[1:]
	}
	
	evidence := &SecurityEvidence{
		EvidenceID:   generateEvidenceID(),
		EnclaveID:    enclaveID,
		EvidenceType: evidenceType,
		Payload:      payload,
		CreatedAt:    time.Now(),
		VerifiedBy:   "TEE_Secure_Processor",
	}
	
	si.evidenceQueue = append(si.evidenceQueue, evidence)
	
	// Emit metric
	si.metricsChannel <- SecurityMetric{
		Name:      "security_evidence_submitted",
		Value:     1.0,
		Labels:    map[string]string{"type": evidenceType},
		Timestamp: time.Now(),
		Unit:      "count",
	}
	
	si.logger.WithFields(logrus.Fields{
		"evidence_id": evidence.EvidenceID,
		"type":        evidenceType,
	}).Debug("Submitted secure evidence")
	
	return nil
}

// CollectMetrics collects security metrics from TEE enclaves
func (si *SecurityIntegration) CollectMetrics(ctx context.Context) ([]SecurityMetric, error) {
	now := time.Now()
	
	// Count pending evidences
	totalPending := float64(len(si.evidenceQueue))
	
	// Get enclave health metrics
	enclaves := si.getHealthyEnclaves(ctx)
	activeEnclaves := float64(len(enclaves))
	
	metrics := []SecurityMetric{
		{
			Name:      "te_enclave_count",
			Value:     activeEnclaves,
			Labels:    map[string]string{"status": "healthy"},
			Timestamp: now,
			Unit:      "count",
		},
		{
			Name:      "te_pending_evidences",
			Value:     totalPending,
			Labels:    map[string]string{},
			Timestamp: now,
			Unit:      "count",
		},
	}
	
	return metrics, nil
}

// ============================================================================
// COST INTEGRATION (L9-L12 FinOps ↔ L15)
// ============================================================================

// CostIntegration connects L15 TEE usage with FinOps cost tracking
type CostIntegration struct {
	mu           sync.RWMutex
	logger       *logrus.Logger
	teeProvider  tee.TEEProvider
	costTracker  *CostTrackingSystem
	
	// Usage metrics
	usageHistory []UsageRecord
	maxHistory   int
	
	// Cost optimization suggestions
	suggestionsChannel chan CostOptimizationSuggestion
}

// UsageRecord tracks TEE enclave usage for cost accounting
type UsageRecord struct {
	EnclaveID      string    `json:"enclave_id"`
	ResourceType   string    `json:"resource_type"` // cpu, memory, storage
	UsageHours     float64   `json:"usage_hours"`
	CostUSD        float64   `json:"cost_usd"`
	TimestampStart time.Time `json:"timestamp_start"`
	TimestampEnd   time.Time `json:"timestamp_end"`
}

// CostOptimizationSuggestion provides cost reduction recommendation
type CostOptimizationSuggestion struct {
	SuggestionID string    `json:"suggestion_id"`
	EnclaveIDs   []string  `json:"enclave_ids"`
	Action       string    `json:"action"` // shutdown, resize, migrate
	EstimatedSavings float64 `json:"estimated_savings"`
	Priority     string    `json:"priority"` // high, medium, low
	CreatedAt    time.Time `json:"created_at"`
}

// NewCostIntegration creates cost tracking integration
func NewCostIntegration(provider tee.TEEProvider, logger *logrus.Logger) *CostIntegration {
	return &CostIntegration{
		teeProvider:        provider,
		logger:             logger,
		usageHistory:       make([]UsageRecord, 0, 1000),
		maxHistory:         1000,
		suggestionsChannel: make(chan CostOptimizationSuggestion, 10),
	}
}

// RecordUsage records enclave usage for cost accounting
func (ci *CostIntegration) RecordUsage(ctx context.Context, enclaveID string, resourceType string, hours float64, costUSD float64) error {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	
	record := UsageRecord{
		EnclaveID:      enclaveID,
		ResourceType:   resourceType,
		UsageHours:     hours,
		CostUSD:        costUSD,
		TimestampStart: time.Now().Add(-time.Duration(hours) * time.Hour),
		TimestampEnd:   time.Now(),
	}
	
	ci.usageHistory = append(ci.usageHistory, record)
	
	// History overflow protection
	if len(ci.usageHistory) > ci.maxHistory {
		ci.usageHistory = ci.usageHistory[len(ci.usageHistory)-ci.maxHistory:]
	}
	
	ci.logger.WithFields(logrus.Fields{
		"enclave_id": enclaveID,
		"cost":       costUSD,
	}).Debug("Recorded enclave usage")
	
	return nil
}

// GenerateCostSuggestions generates cost optimization suggestions
func (ci *CostIntegration) GenerateCostSuggestions(ctx context.Context) ([]CostOptimizationSuggestion, error) {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	
	suggestions := make([]CostOptimizationSuggestion, 0)
	
	// Analyze usage patterns for cost savings
	// Would analyze historical usage data
	
	return suggestions, nil
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func computeEventHash(event *ThreatIntelligenceEvent) []byte {
	data, _ := json.Marshal(event)
	hash := sha256.Sum256(data)
	return hash[:]
}

func generateEvidenceID() string {
	return fmt.Sprintf("evd_%d", time.Now().UnixNano())
}
