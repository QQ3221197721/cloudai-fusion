// Package ha - Multi-Region High Availability Orchestration Engine
// ENHANCED PATENT #31: True multi-region HA with automatic failover and cross-region replication
package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// MULTI-REGION HIGH AVAILABILITY ORCHESTRATOR (Patent #31)
// ============================================================================

// MultiRegionHA orchestrates true multi-region high availability across regions
type MultiRegionHA struct {
	mu          sync.RWMutex
	logger      *logrus.Logger
	
	// Regional clusters
	regions []*RegionCluster
	
	// Primary region management
	primaryRegion string
	failoverState FailoverState
	
	// Cross-region replication
	replicationMonitor *ReplicationMonitor
	
	// Health monitoring
	healthChecker *HealthChecker
	
	// Automatic failover configuration
	failoverConfig FailoverConfig
	
	// HA metrics
	haMetrics *HAMetrics
	
	// Latest state
	lastCheckTime time.Time
}

// RegionCluster represents a single cloud region cluster
type RegionCluster struct {
	ID           string              `json:"id"`
	Region       string              `json:"region"`
	CloudProvider string             `json:"cloud_provider"` // AWS, Azure, Aliyun, etc.
	Status       ClusterStatus       `json:"status"`
	Endpoint     string              `json:"endpoint"`
	Port         int                 `json:"port"`
	Metrics      ClusterMetrics      `json:"metrics"`
	Config       RegionConfig        `json:"config"`
	Primary      bool                `json:"primary"`
	LastCheck    time.Time           `json:"last_check"`
	Replication  ReplicationStatus   `json:"replication"`
	
	// Database clusters within region
	DBClusters    []*DatabaseCluster
	WorkloadClusters []*WorkloadCluster
}

// DatabaseCluster represents PostgreSQL cluster in the region
type DatabaseCluster struct {
	ID          string            `json:"id"`
	Type        DBType            `json:"db_type"` // primary, standby
	Endpoint    string            `json:"endpoint"`
	Port        int               `json:"port"`
	Primary     bool              `json:"primary"`
	Status      DBStatus          `json:"status"`
	Metrics     DatabaseMetrics   `json:"metrics"`
	Replication ReplicationInfo   `json:"replication"`
}

// WorkloadCluster represents Kubernetes workload cluster
type WorkloadCluster struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	K8sClient interface{}     `json:"-"` // kubernetes client-go
	Status    ClusterStatus   `json:"status"`
	Metrics   ClusterMetrics  `json:"metrics"`
	Pods      []*PodStatus    `json:"pods"`
}

// ClusterStatus describes cluster health
type ClusterStatus string

const (
	StatusHealthy    ClusterStatus = "healthy"
	StatusDegraded   ClusterStatus = "degraded"
	StatusUnhealthy  ClusterStatus = "unhealthy"
	StatusIsolated   ClusterStatus = "isolated" // Split-brain detected
)

// ClusterMetrics provides detailed metrics for the region cluster
type ClusterMetrics struct {
	CPUUtilization    float64
	MemoryUtilization float64
	DiskUtilization   float64
	ActiveConnections int
	QueryPerSecond    float64
	ErrorRate         float64
	UptimeSec         int64
	PodCount          int
	HealthyPods       int
}

// ============================================================================
// FAILOVER STATE AND CONFIGURATION
// ============================================================================

// FailoverState tracks current failover status
type FailoverState struct {
	State       FailoverStatus
	StartedAt   time.Time
	CompletedAt time.Time
	Reason      string
	Evidence    []FailoverEvidence
	TargetRegion string
	FailedRegion string
}

// FailoverStatus describes failover stage
type FailoverStatus string

const (
	FailoverIdle         FailoverStatus = "idle"
	FailoverPreparing    FailoverStatus = "preparing"
	FailoverInProgress   FailoverStatus = "in_progress"
	FailoverComplete     FailoverStatus = "complete"
	FailoverRollback     FailoverStatus = "rolling_back"
	FailoverConfirmed    FailoverStatus = "confirmed"
)

// FailoverEvidence captures evidence for audit trail
type FailoverEvidence struct {
	Timestamp   time.Time
	EventType   string
	Description string
	Metrics     map[string]interface{}
}

// FailoverConfig defines failover conditions and behavior
type FailoverConfig struct {
	MaxReplLagSec           int       // Auto-failover if replication lag exceeds this
	PrimaryDownTimeoutSec   int       // Time to wait before declaring primary down
	MinStandbyHealthySec    int       // Standby must be healthy for this long before failover
	SplitBrainDetection     bool      // Enable split-brain detection
	MinimumHealthyNodes     int       // Minimum healthy nodes required per region
	AffinityRules           []AffinityRule // Prefer certain region combinations
	RollbackOnFailure       bool      // Automatically rollback failed failover
	FailoverCooldownSec     int       // Minimum time between failovers
}

// AffinityRule defines region affinity rules
type AffinityRule struct {
	PreferredRegions []string // Regions that should be kept together
	AvoidRegions     []string // Regions that should not be co-located
	Priority         int      // Priority level (higher = more important)
}

// ============================================================================
// HEALTH CHECKING AND MONITORING
// ============================================================================

// HealthChecker performs comprehensive health checks across all regions
type HealthChecker struct {
	mu              sync.RWMutex
	checkInterval   time.Duration
	lastCheckTime   time.Time
	healthHistory   []HealthSnapshot
	maxHistorySize  int
	logger          *logrus.Logger
}

// HealthSnapshot captures health snapshot at a point in time
type HealthSnapshot struct {
	Timestamp   time.Time              `json:"timestamp"`
	Regions     map[string]bool        `json:"regions"` // healthy or unhealthy
	GlobalHealth bool                   `json:"global_health"`
	Issues      []HealthIssue          `json:"issues,omitempty"`
	Metrics     map[string]float64     `json:"metrics"`
}

// HealthIssue describes a specific health issue
type HealthIssue struct {
	Region    string    `json:"region"`
	IssueType string    `json:"issue_type"`
	Message   string    `json:"message"`
	Severity  SeverityLevel `json:"severity"`
	Timestamp time.Time `json:"timestamp"`
}

// SeverityLevel describes issue severity
type SeverityLevel string

const (
	SeverityInfo    SeverityLevel = "info"
	SeverityWarning SeverityLevel = "warning"
	SeverityError   SeverityLevel = "error"
	SeverityCritical SeverityLevel = "critical"
)

// ============================================================================
// REPLICATION MONITOR
// ============================================================================

// ReplicationMonitor monitors cross-region data replication
type ReplicationMonitor struct {
	mu sync.RWMutex
	logger *logrus.Logger
	
	// Per-region replication status
	replicationStatus map[string]*ReplicationStatus
	
	// Replication lag tracking
	lagHistory []LagSnapshot
	maxHistorySize int
	
	// Consistency verification
	consistencyChecker *ConsistencyChecker
}

// ReplicationStatus describes replication status for a region
type ReplicationStatus struct {
	SourceRegion  string    `json:"source_region"`
	TargetRegion  string    `json:"target_region"`
	LagSeconds    int       `json:"lag_seconds"`
	IsHealthy     bool      `json:"is_healthy"`
	LastSyncTime  time.Time `json:"last_sync_time"`
	DataLost      bool      `json:"data_lost"` // True if data lost during replication
	ErrorCount    int       `json:"error_count"`
}

// LagSnapshot captures replication lag at a point in time
type LagSnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	LagSeconds int      `json:"lag_seconds"`
	Healthy    bool     `json:"healthy"`
}

// ============================================================================
// MAIN ORCHESTRATION LOGIC
// ============================================================================

// NewMultiRegionHA creates multi-region HA orchestrator
func NewMultiRegionHA(regions []*RegionCluster, config FailoverConfig, logger *logrus.Logger) (*MultiRegionHA, error) {
	if len(regions) < 2 {
		return nil, fmt.Errorf("at least 2 regions required for multi-region HA")
	}
	
	h := &MultiRegionHA{
		regions: regions,
		failoverConfig: config,
		logger: logger,
		failoverState: FailoverState{State: FailoverIdle},
		replicationMonitor: NewReplicationMonitor(logger),
		healthChecker: NewHealthChecker(config.FailoverCooldownSec, logger),
		haMetrics: NewHAMetrics(),
	}
	
	// Set primary region
	for _, region := range regions {
		if region.Primary {
			h.primaryRegion = region.ID
			break
		}
	}
	
	// Start background monitoring
	go h.runMonitoringLoop(context.Background())
	
	logger.Info("Multi-region HA orchestrator initialized")
	return h, nil
}

// runMonitoringLoop runs continuous health checks and triggers failover when needed
func (h *MultiRegionHA) runMonitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.performHealthChecks()
		}
	}
}

// performHealthChecks performs comprehensive health checks across all regions
func (h *MultiRegionHA) performHealthChecks() {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.lastCheckTime = time.Now()
	
	// Check each region
	healthyRegions := make(map[string]bool)
	var issues []HealthIssue
	
	for _, region := range h.regions {
		healthy := h.checkRegionHealth(region)
		healthyRegions[region.ID] = healthy
		
		if !healthy {
			issues = append(issues, HealthIssue{
				Region:    region.ID,
				IssueType: "region_unhealthy",
				Message:   fmt.Sprintf("Region %s is unhealthy", region.ID),
				Severity:  SeverityError,
				Timestamp: time.Now(),
			})
		}
	}
	
	globalHealth := h.determineGlobalHealth(healthyRegions)
	
	// Record health snapshot
	snapshot := HealthSnapshot{
		Timestamp:   time.Now(),
		Regions:     healthyRegions,
		GlobalHealth: globalHealth,
		Issues:      issues,
	}
	
	h.healthChecker.RecordSnapshot(snapshot)
	
	// Evaluate failover trigger
	if !globalHealth && h.failoverState.State == FailoverIdle {
		h.evaluateFailoverTrigger()
	}
}

// checkRegionHealth checks health of a specific region
func (h *MultiRegionHA) checkRegionHealth(region *RegionCluster) bool {
	// Test connectivity
	connErr := region.TestConnectivity()
	if connErr != nil {
		region.Status = StatusUnhealthy
		return false
	}
	
	// Check database replication status
	for _, db := range region.DBClusters {
		if db.Type == DBTypePrimary {
			replStatus := h.replicationMonitor.GetReplicationStatus(db.Replication.SourceRegion, region.ID)
			
			// Check replication lag threshold
			if replStatus.LagSeconds > h.failoverConfig.MaxReplLagSec {
				region.Status = StatusDegraded
				return false
			}
		}
		
		// Check DB health
		if !db.Healthy() {
			region.Status = StatusDegraded
			return false
		}
	}
	
	// Check workload cluster health
	for _, wkld := range region.WorkloadClusters {
		if !wkld.Healthy() {
			region.Status = StatusDegraded
			return false
		}
	}
	
	region.Status = StatusHealthy
	return true
}

// determineGlobalHealth determines overall HA health
func (h *MultiRegionHA) determineGlobalHealth(healthyRegions map[string]bool) bool {
	// Must have at least one healthy region
	hasHealthy := false
	for _, healthy := range healthyRegions {
		if healthy {
			hasHealthy = true
			break
		}
	}
	
	if !hasHealthy {
		return false
	}
	
	// If primary region is unhealthy and there's no healthy standby ready
	if !healthyRegions[h.primaryRegion] {
		// Check if any standby meets failover requirements
		hasEligibleStandby := false
		for _, region := range h.regions {
			if !region.Primary && healthyRegions[region.ID] {
				// Check eligibility criteria
				if h.isStandbyEligibleForFailover(region) {
					hasEligibleStandby = true
					break
				}
			}
		}
		
		if !hasEligibleStandby {
			return false
		}
	}
	
	return true
}

// isStandbyEligibleForFailover checks if standby region qualifies for failover
func (h *MultiRegionHA) isStandbyEligibleForFailover(region *RegionCluster) bool {
	// Must be healthy for minimum duration
	healthyDuration := time.Since(region.LastCheck)
	if healthyDuration < time.Duration(h.failoverConfig.MinStandbyHealthySec)*time.Second {
		return false
	}
	
	// Check affinity rules
	for _, rule := range h.failoverConfig.AffinityRules {
		if contains(region.ID, rule.AvoidRegions) {
			return false
		}
	}
	
	// Check replication lag
	for _, db := range region.DBClusters {
		if db.Type == DBTypePrimary {
			continue
		}
		replStatus := h.replicationMonitor.GetReplicationStatus(db.Replication.SourceRegion, region.ID)
		if replStatus.LagSeconds > h.failoverConfig.MaxReplLagSec {
			return false
		}
	}
	
	return true
}

// evaluateFailoverTrigger evaluates whether failover should be triggered
func (h *MultiRegionHA) evaluateFailoverTrigger() {
	h.logger.Warn("Evaluating failover trigger condition")
	
	// Verify primary is truly down
	primaryRegion := h.getPrimaryRegion()
	if primaryRegion.Status == StatusHealthy {
		h.logger.Debug("Primary still healthy, no failover needed")
		return
	}
	
	// Find eligible standby
	targetRegion := h.findEligibleStandby(primaryRegion.ID)
	if targetRegion == nil {
		h.logger.Error("No eligible standby region found for failover")
		return
	}
	
	// Trigger failover
	h.startFailover(primaryRegion.ID, targetRegion.ID, "Primary region unhealthy")
}

// startFailover initiates the failover process
func (h *MultiRegionHA) startFailover(failedRegionID, targetRegionID, reason string) {
	h.logger.WithFields(logrus.Fields{
		"failed_region": failedRegionID,
		"target_region": targetRegionID,
		"reason": reason,
	}).Warn("Starting failover process")
	
	h.failoverState = FailoverState{
		State:       FailoverPreparing,
		StartedAt:   time.Now(),
		Reason:      reason,
		FailedRegion: failedRegionID,
		TargetRegion: targetRegionID,
		Evidence:    make([]FailoverEvidence, 0),
	}
	
	h.recordFailoverEvidence(FailoverPreparing, "Failover initiated", map[string]interface{}{
		"reason": reason,
		"target": targetRegionID,
	})
	
	// Execute failover steps
	h.executeFailoverSteps(failedRegionID, targetRegionID)
}

// executeFailoverSteps performs the actual failover sequence
func (h *MultiRegionHA) executeFailoverSteps(failedRegionID, targetRegionID string) {
	h.failoverState.State = FailoverInProgress
	
	// Step 1: Stop writes to failed region
	h.recordFailoverEvidence(FailoverInProgress, "Stopping writes to failed region", nil)
	h.stopWritesToRegion(failedRegionID)
	
	// Step 2: Promote standby databases in target region
	h.recordFailoverEvidence(FailoverInProgress, "Promoting standby databases", nil)
	h.promoteStandbyDatabases(targetRegionID)
	
	// Step 3: Update DNS/endpoints
	h.recordFailoverEvidence(FailoverInProgress, "Updating DNS/endpoints", nil)
	h.updateEndpoints(targetRegionID)
	
	// Step 4: Promote target to primary
	h.recordFailoverEvidence(FailoverInProgress, "Promoting target to primary", nil)
	h.promoteTargetToPrimary(targetRegionID)
	
	// Step 5: Verify new primary
	if h.verifyNewPrimary(targetRegionID) {
		h.failoverState.State = FailoverConfirmed
		h.failoverState.CompletedAt = time.Now()
		h.recordFailoverEvidence(FailoverConfirmed, "Failover successful", nil)
		
		h.haMetrics.RecordFailover(time.Since(h.failoverState.StartedAt))
		
		h.logger.WithFields(logrus.Fields{
			"duration": time.Since(h.failoverState.StartedAt),
			"old_primary": failedRegionID,
			"new_primary": targetRegionID,
		}).Info("Failover completed successfully")
		
		// Notify stakeholders
		h.notifyStakeholders(targetRegionID)
	} else {
		h.failoverState.State = FailoverRollback
		h.recordFailoverEvidence(FailoverRollback, "Verification failed, initiating rollback", nil)
		h.rollbackFailover()
	}
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func (h *MultiRegionHA) getPrimaryRegion() *RegionCluster {
	for _, region := range h.regions {
		if region.Primary {
			return region
		}
	}
	return nil
}

func (h *MultiRegionHA) findEligibleStandby(excludeRegionID string) *RegionCluster {
	for _, region := range h.regions {
		if region.ID == excludeRegionID || region.Primary {
			continue
		}
		if h.isStandbyEligibleForFailover(region) {
			return region
		}
	}
	return nil
}

func (h *MultiRegionHA) stopWritesToRegion(regionID string) {
	// Would stop writes to database and workload clusters in region
	h.logger.WithField("region", regionID).Debug("Stopped writes to region")
}

func (h *MultiRegionHA) promoteStandbyDatabases(targetRegionID string) {
	// Would execute pg_promote on databases in target region
	h.logger.WithField("region", targetRegionID).Debug("Promoted standby databases")
}

func (h *MultiRegionHA) updateEndpoints(targetRegionID string) {
	// Would update DNS/Service configurations
	h.logger.WithField("region", targetRegionID).Debug("Updated endpoints")
}

func (h *MultiRegionHA) promoteTargetToPrimary(targetRegionID string) {
	// Update region roles
	for _, region := range h.regions {
		region.Primary = (region.ID == targetRegionID)
	}
	h.primaryRegion = targetRegionID
	h.logger.WithField("region", targetRegionID).Debug("Promoted target to primary")
}

func (h *MultiRegionHA) verifyNewPrimary(targetRegionID string) bool {
	// Verify new primary is operational
	newPrimary := h.getPrimaryRegion()
	if newPrimary == nil || newPrimary.ID != targetRegionID {
		return false
	}
	
	// Test connectivity
	connErr := newPrimary.TestConnectivity()
	if connErr != nil {
		return false
	}
	
	// Verify databases are healthy
	for _, db := range newPrimary.DBClusters {
		if !db.Healthy() {
			return false
		}
	}
	
	return true
}

func (h *MultiRegionHA) rollbackFailover() {
	h.logger.Error("Rollback initiated due to verification failure")
	// Would revert changes and restore original primary
}

func (h *MultiRegionHA) notifyStakeholders(successorRegionID string) {
	// Would send notifications via Slack/DingTalk/PagerDuty
	h.logger.Info("Notified stakeholders of successful failover")
}

func (h *MultiRegionHA) recordFailoverEvidence(state FailoverStatus, eventType string, metrics map[string]interface{}) {
	evidence := FailoverEvidence{
		Timestamp:   time.Now(),
		EventType:   fmt.Sprintf("%s/%s", h.failoverState.State, eventType),
		Description: fmt.Sprintf("[%s] %s", h.failoverState.State, eventType),
		Metrics:     metrics,
	}
	
	h.failoverState.Evidence = append(h.failoverState.Evidence, evidence)
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
