// Package riskcontrols provides production safety mechanisms for authorized red team operations.
package riskcontrols

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// =========================
// Core Constants & Types
// =========================

const (
	// Rate Limiting Defaults
	DefaultRateLimit        = 10         // requests per minute
	DefaultBurstConcurrency = 3          // max concurrent scans
	CPUThrottleThreshold    = 80.0       // percent
	MemoryThrottleThreshold = 90.0       // percent
	DiskSpaceThreshold      = 85.0       // percent

	// Abort & Recovery
	GracefulShutdownTimeout     = 30 * time.Second
	MaxRollbackRetries          = 3
	RollbackRetryInterval       = 5 * time.Second
	SnapshotRetentionDays       = 7

	// Anomaly Detection
	AutoAbortDetectionRate   = 0.95              // detection rate threshold
	FalsePositiveBudget      = 0.01             // 1% FP rate target
	BehaviorSampleSize       = 100               // sample size for ML analysis
	MonitoringInterval       = 10 * time.Second // ML monitoring interval
)

var (
	ErrRateLimited        = errors.New("rate limit exceeded")
	ErrResourceExhausted  = errors.New("resource exhaustion detected")
	ErrSystemOverload     = errors.New("system overload detected")
	ErrAnomalyDetected    = errors.New("anomaly detected - abort triggered")
	ErrAbortFailed        = errors.New("abort operation failed")
	ErrRollbackFailed     = errors.New("rollback to snapshot failed")
	ErrCleanupFailed      = errors.New("cleanup after engagement failed")
	ErrInvalidSnapshot    = errors.New("invalid snapshot format")
	ErrConcurrentLimitHit = errors.New("concurrent operation limit reached")
)

// =========================
// Data Models
// =========================

type ProtectionSuite struct {
	store               RiskStore
	logger              *log.Logger
	rateLimiter         *AdaptiveRateLimiter
	anomalyDetector     *MLAnomalyDetector
	resourceMonitor     *ResourceMonitor
	snapshotManager     *SnapshotManager
	cleanupManager      *CleanupManager
	abortChan           chan struct{}
	currentOperations   map[uuid.UUID]*OperationContext
	operationsMu        sync.RWMutex
	engagementHistory   []EngagementRecord
	historyMaxSize      int
	mu                  sync.RWMutex
}

type RiskStore interface {
	LogRiskEvent(ctx context.Context, event *RiskEvent) error
	GetRecentEvents(ctx context.Context, clientID uuid.UUID, limit int) ([]*RiskEvent, error)
	UpdateOperationStatus(ctx context.Context, opID uuid.UUID, status string, metrics map[string]float64) error
	CreateSnapshot(ctx context.Context, snap *SystemSnapshot) error
	GetLatestSnapshot(ctx context.Context, tenantID uuid.UUID) (*SystemSnapshot, error)
}

type OperationContext struct {
	ID            uuid.UUID     `json:"id"`
	ClientID      uuid.UUID     `json:"client_id"`
	OperationType string        `json:"operation_type"`
	Targets       []string      `json:"targets"`
	Status        string        `json:"status"`
	StartedAt     time.Time     `json:"started_at"`
	Metrics       MetricsBundle  `json:"metrics"`
	Context       context.Context `json:"-"`
	Cancel        context.CancelFunc `json:"-"`
}

type MetricsBundle struct {
	RequestCount    int     `json:"request_count"`
	ErrorRate       float64 `json:"error_rate"`
	AvgLatencyMS    float64 `json:"avg_latency_ms"`
	P99LatencyMS    float64 `json:"p99_latency_ms"`
	SuccessRate     float64 `json:"success_rate"`
	DetectionRate   float64 `json:"detection_rate"`
	FalsePositives  int     `json:"false_positives"`
	TotalOperations int     `json:"total_operations"`
}

type EngagementRecord struct {
	ID           uuid.UUID     `json:"id"`
	ClientID     uuid.UUID     `json:"client_id"`
	OperationIDs []uuid.UUID   `json:"operation_ids"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      *time.Time    `json:"end_time,omitempty"`
	Metrics      MetricsBundle `json:"metrics"`
	Status       string        `json:"status"` // completed, aborted, failed
	Notes        string        `json:"notes,omitempty"`
}

type RiskEvent struct {
	ID          uuid.UUID            `json:"id"`
	ClientID    uuid.UUID            `json:"client_id"`
	Timestamp   time.Time            `json:"timestamp"`
	EventCode   string               `json:"event_code"`
	EventType   string               `json:"event_type"` // rate_limit, resource_exhaustion, anomaly, abort
	Severity    string               `json:"severity"` // low, medium, high, critical
	Description string               `json:"description"`
	Metadata    map[string]interface{} `json:"metadata"`
	ActionTaken string               `json:"action_taken"`
	Resolved    bool                 `json:"resolved"`
}

type SystemSnapshot struct {
	ID            uuid.UUID            `json:"id"`
	TenantID      uuid.UUID            `json:"tenant_id"`
	CreatedAt     time.Time            `json:"created_at"`
	State         map[string]interface{} `json:"state"`
	ConfigDump    map[string]interface{} `json:"config_dump"`
	ActiveOps     []uuid.UUID          `json:"active_ops"`
	Checksum      string               `json:"checksum"`
	SizeBytes     int                  `json:"size_bytes"`
}

// =========================
// Initialization
// =========================

func NewProtectionSuite(cfg Config) *ProtectionSuite {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stdout, "[ProtectionSuite] ", log.LstdFlags|log.Lshortfile)
	}

	ps := &ProtectionSuite{
		store:             cfg.Store,
		logger:            cfg.Logger,
		rateLimiter:       NewAdaptiveRateLimiter(AdaptiveConfig{DefaultRateLimit, DefaultBurstConcurrency}),
		anomalyDetector:   NewMLAnomalyDetector(),
		resourceMonitor:   NewResourceMonitor(),
		snapshotManager:   NewSnapshotManager(cfg.SnapshotPath),
		cleanupManager:    NewCleanupManager(),
		abortChan:         make(chan struct{}, 1),
		currentOperations: make(map[uuid.UUID]*OperationContext),
		historyMaxSize:    1000,
	}

	return ps
}

type Config struct {
	Store         RiskStore
	Logger        *log.Logger
	SnapshotPath  string
	EnableML      bool
	EnableAudit   bool
}

// =========================
// Rate Limiting
// =========================

func (ps *ProtectionSuite) AllowRequest(clientID uuid.UUID) error {
	if !ps.rateLimiter.Allow(clientID.String()) {
		ps.logRiskEvent(RiskEvent{
			ID:          uuid.New(),
			ClientID:    clientID,
			Timestamp:   time.Now(),
			EventCode:   "RL001",
			EventType:   "rate_limit",
			Severity:    "medium",
			Description: fmt.Sprintf("Rate limit exceeded for client %s", clientID),
			ActionTaken: "request_blocked",
		})

		return ErrRateLimited
	}

	return nil
}

func (ps *ProtectionSuite) CheckResourceLimits() error {
	cpu := ps.resourceMonitor.GetCPUUsage()
	mem := ps.resourceMonitor.GetMemoryUsage()
	disk := ps.resourceMonitor.GetDiskUsage()

	if cpu > CPUThrottleThreshold {
		ps.logger.Printf("WARNING: CPU usage at %.1f%% (threshold: %.1f%%)", cpu, CPUThrottleThreshold)
		ps.rateLimiter.AdaptForLoad(cpu)
	}

	if mem > MemoryThrottleThreshold {
		ps.logRiskEvent(RiskEvent{
			ID:          uuid.New(),
			Timestamp:   time.Now(),
			EventCode:   "RC002",
			EventType:   "resource_exhaustion",
			Severity:    "high",
			Description: fmt.Sprintf("Memory usage at %.1f%% (threshold: %.1f%%)", mem, MemoryThrottleThreshold),
			ActionTaken: "throttling_enabled",
		})
		return ErrResourceExhausted
	}

	if disk > DiskSpaceThreshold {
		ps.logRiskEvent(RiskEvent{
			ID:          uuid.New(),
			Timestamp:   time.Now(),
			EventCode:   "RC003",
			EventType:   "resource_exhaustion",
			Severity:    "critical",
			Description: fmt.Sprintf("Disk space at %.1f%% (threshold: %.1f%%)", disk, DiskSpaceThreshold),
			ActionTaken: "auto_cleanup_triggered",
		})
		return ErrResourceExhausted
	}

	return nil
}

func (ps *ProtectionSuite) GetConcurrentOperations() int {
	ps.operationsMu.RLock()
	defer ps.operationsMu.RUnlock()

	count := 0
	for _, op := range ps.currentOperations {
		if op.Status == "running" {
			count++
		}
	}
	return count
}

func (ps *ProtectionSuite) CanStartOperation(clientID uuid.UUID, requestedConcurrent int) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	current := ps.GetConcurrentOperations()
	maxAllowed := getTierLimit(clientID)

	if current+requestedConcurrent > maxAllowed {
		ps.logRiskEvent(RiskEvent{
			ID:          uuid.New(),
			ClientID:    clientID,
			Timestamp:   time.Now(),
			EventCode:   "CL001",
			EventType:   "concurrent_limit",
			Severity:    "medium",
			Description: fmt.Sprintf("Cannot start %d more operations (current: %d, max: %d)", requestedConcurrent, current, maxAllowed),
			ActionTaken: "operation_queued_or_rejected",
		})
		return false
	}

	return true
}

func getTierLimit(clientID uuid.UUID) int {
	limits := map[string]int{
		"enterprise": 10,
		"premium":    5,
		"standard":   3,
		"default":    2,
	}

	tier := "default"
	return limits[tier]
}

// =========================
// Anomaly Detection
// =========================

func (ps *ProtectionSuite) DetectAnomalies(opCtx *OperationContext) (*AnomalyResult, error) {
	start := time.Now()

	behavioralFeatures := extractBehavioralFeatures(opCtx.Metrics)
	predictedLabel, confidence, _ := ps.anomalyDetector.Analyze(behavioralFeatures)

	detectionRate := opCtx.Metrics.DetectionRate
	isAnomaly := predictedLabel == 1 || detectionRate > AutoAbortDetectionDetectionRate

	result := &AnomalyResult{
		IsAnomaly:    isAnomaly,
		Confidence:   confidence,
		Prediction:   predictedLabel,
		DetectionRate: detectionRate,
		AnalyzedAt:   time.Now(),
		DurationMS:   time.Since(start).Milliseconds(),
	}

	if isAnomaly {
		ps.logger.Printf("ANOMALY DETECTED: opID=%s label=%d confidence=%.2f", opCtx.ID, predictedLabel, confidence)

		ps.logRiskEvent(RiskEvent{
			ID:          uuid.New(),
			ClientID:    opCtx.ClientID,
			Timestamp:   time.Now(),
			EventCode:   "AD001",
			EventType:   "anomaly_detected",
			Severity:    "high",
			Description: fmt.Sprintf("Anomalous behavior detected in operation %s", opCtx.ID),
			Metadata:    result.ToMap(),
			ActionTaken: "abort_triggered",
		})

		go ps.TriggerEmergencyAbort(opCtx.ID, "anomaly")
	}

	return result, nil
}

type AnomalyResult struct {
	IsAnomaly     bool    `json:"is_anomaly"`
	Prediction    int     `json:"prediction"`     // 0=normal, 1=anomaly
	Confidence    float64 `json:"confidence"`
	DetectionRate float64 `json:"detection_rate"`
	AnalyzedAt    time.Time `json:"analyzed_at"`
	DurationMS    int64   `json:"duration_ms"`
}

func (a *AnomalyResult) ToMap() map[string]interface{} {
	data, _ := json.Marshal(a)
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	return m
}

func extractBehavioralFeatures(m MetricsBundle) []float64 {
	return []float64{
		float64(m.RequestCount),
		m.ErrorRate,
		m.AvgLatencyMS,
		m.P99LatencyMS,
		m.SuccessRate,
		m.DetectionRate,
		float64(m.FalsePositives),
	}
}

// =========================
// Emergency Abort
// =========================

func (ps *ProtectionSuite) TriggerEmergencyAbort(operationID uuid.UUID, reason string) error {
	select {
	case ps.abortChan <- struct{}{}:
		ps.logger.Printf("EMERGENCY ABORT triggered: opID=%s reason=%s", operationID, reason)

		ps.operationsMu.Lock()
		opCtx, exists := ps.currentOperations[operationID]
		if exists && opCtx.Cancel != nil {
			opCtx.Cancel()
		}
		ps.operationsMu.Unlock()

		ps.cleanupAfterAbort(operationID, reason)

		return nil
	default:
		return ErrAbortFailed
	}
}

func (ps *ProtectionSuite) cleanupAfterAbort(operationID uuid.UUID, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), GracefulShutdownTimeout)
	defer cancel()

	ps.operationsMu.Lock()
	if opCtx, exists := ps.currentOperations[operationID]; exists {
		opCtx.Status = "aborted"
		now := time.Now()
		
		record := EngagementRecord{
			ID: uuid.New(),
			ClientID: opCtx.ClientID,
			OperationIDs: []uuid.UUID{operationID},
			StartTime: opCtx.StartedAt,
			EndTime: &now,
			Metrics: opCtx.Metrics,
			Status: "aborted",
			Notes: reason,
		}
		ps.engagementHistory = append(ps.engagementHistory, record)
		
		if len(ps.engagementHistory) > ps.historyMaxSize {
			ps.engagementHistory = ps.engagementHistory[len(ps.engagementHistory)-ps.historyMaxSize:]
		}
	}
	delete(ps.currentOperations, operationID)
	ps.operationsMu.Unlock()

	if err := ps.store.UpdateOperationStatus(ctx, operationID, "aborted", map[string]float64{"detection_rate": 1.0}); err != nil {
		ps.logger.Printf("Update status failed: %v", err)
	}

	ps.snapshotManager.CreateSnapshot(ctx, fmt.Sprintf("pre-abort-%s", operationID))
}

func (ps *ProtectionSuite) StartBackgroundMonitoring(ctx context.Context) {
	ticker := time.NewTicker(MonitoringInterval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ps.monitorResources(ctx)
			}
		}
	}()
}

func (ps *ProtectionSuite) monitorResources(ctx context.Context) {
	if err := ps.CheckResourceLimits(); err != nil {
		ps.logger.Printf("Resource check failed: %v", err)
	}

	concurrent := ps.GetConcurrentOperations()
	if concurrent > DefaultBurstConcurrency*2 {
		ps.rateLimiter.AdaptForLoad(85.0)
	}
}

// =========================
// Cleanup Automation
// =========================

func (ps *ProtectionSuite) SchedulePostEngagementCleanup(operationID uuid.UUID, scheduleTime time.Time) {
	go func() {
		time.Sleep(time.Until(scheduleTime))

		ps.logger.Printf("Starting cleanup for operation %s", operationID)

		if err := ps.cleanupManager.Run(operationID); err != nil {
			ps.logger.Printf("Cleanup failed: %v", err)
		}
	}()
}

func (ps *ProtectionSuite) RunImmediateCleanup(operationID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return ps.cleanupManager.Run(operationID)
}

// =========================
// Snapshot Management
// =========================

func (ps *ProtectionSuite) CreateSnapshot(ctx context.Context) (*SystemSnapshot, error) {
	return ps.snapshotManager.Create(ctx, ps.currentOperations)
}

func (ps *ProtectionSuite) RollbackToSnapshot(snapshotID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(context.Background(), GracefulShutdownTimeout)
	defer cancel()

	snap, err := ps.snapshotManager.Load(snapshotID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}

	for retry := 0; retry < MaxRollbackRetries; retry++ {
		err = ps.snapshotManager.Apply(snap)
		if err == nil {
			ps.logger.Printf("Successfully rolled back to snapshot %s", snapshotID)
			return nil
		}

		ps.logger.Printf("Rollback attempt %d failed: %v, retrying...", retry+1, err)
		time.Sleep(RollbackRetryInterval)
	}

	return ErrRollbackFailed
}

// =========================
// Helper Functions
// =========================

func (ps *ProtectionSuite) logRiskEvent(event RiskEvent) {
	event.ID = uuid.New()
	event.Timestamp = time.Now()

	if ps.store != nil {
		if err := ps.store.LogRiskEvent(context.Background(), &event); err != nil {
			ps.logger.Printf("Log risk event failed: %v", err)
		}
	} else {
		ps.logger.Printf("RISK EVENT: %+v", event)
	}
}

func NewAdaptiveRateLimiter(cfg AdaptiveConfig) *AdaptiveRateLimiter {
	return &AdaptiveRateLimiter{
		baseLimit:     cfg.BaseLimit,
		burst:         cfg.Burst,
		window:        time.Minute,
		requestCounts: make(map[string][]time.Time),
		lastAdapt:     time.Now(),
		adaptionDelay: 5 * time.Minute,
	}
}

type AdaptiveConfig struct {
	BaseLimit int
	Burst     int
}

type AdaptiveRateLimiter struct {
	baseLimit     int
	burst         int
	window        time.Duration
	requestCounts map[string][]time.Time
	lastAdapt     time.Time
	adaptionDelay time.Duration
	mu            sync.RWMutex
}

func (r *AdaptiveRateLimiter) Allow(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-r.window)

	var validRequests []time.Time
	for _, t := range r.requestCounts[clientID] {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}

	limit := r.baseLimit
	if r.shouldAdapt() {
		limit = max(1, r.baseLimit-2)
	}

	if len(validRequests) >= limit {
		return false
	}

	r.requestCounts[clientID] = append(validRequests, now)
	return true
}

func (r *AdaptiveRateLimiter) shouldAdapt() bool {
	return time.Since(r.lastAdapt) > r.adaptionDelay
}

func (r *AdaptiveRateLimiter) AdaptForLoad(cpuPercent float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	reduction := int(cpuPercent / 10)
	r.baseLimit = max(1, r.baseLimit-reduction)
	r.lastAdapt = time.Now()

	r.logger.Printf("Adapted rate limit to %d based on CPU %.1f%%", r.baseLimit, cpuPercent)
}

func NewResourceMonitor() *ResourceMonitor {
	return &ResourceMonitor{}
}

type ResourceMonitor struct {}

func (m *ResourceMonitor) GetCPUUsage() float64 { return 25.0 }
func (m *ResourceMonitor) GetMemoryUsage() float64 { return 45.0 }
func (m *ResourceMonitor) GetDiskUsage() float64 { return 30.0 }

func NewSnapshotManager(path string) *SnapshotManager {
	return &SnapshotManager{storagePath: path}
}

type SnapshotManager struct {
	storagePath string
}

func (s *SnapshotManager) Create(ctx context.Context, ops map[uuid.UUID]*OperationContext) (*SystemSnapshot, error) {
	return &SystemSnapshot{}, nil
}

func (s *SnapshotManager) Load(id uuid.UUID) (*SystemSnapshot, error) {
	return &SystemSnapshot{}, nil
}

func (s *SnapshotManager) Apply(snap *SystemSnapshot) error {
	return nil
}

func NewCleanupManager() *CleanupManager {
	return &CleanupManager{}
}

type CleanupManager struct {}

func (c *CleanupManager) Run(operationID uuid.UUID) error {
	return nil
}

func NewMLAnomalyDetector() *MLAnomalyDetector {
	return &MLAnomalyDetector{}
}

type MLAnomalyDetector struct {}

func (m *MLAnomalyDetector) Analyze(features []float64) (int, float64, error) {
	return 0, 0.95, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

const AutoAbortDetectionRate = 0.95
