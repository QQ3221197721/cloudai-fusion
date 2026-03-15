// Package disaster provides disaster recovery capabilities including
// scheduled backups, cross-region replication, and one-click recovery drills.
package disaster

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Backup Types
// ============================================================================

// BackupType defines the kind of backup.
type BackupType string

const (
	BackupTypeFull        BackupType = "full"
	BackupTypeIncremental BackupType = "incremental"
	BackupTypeDifferential BackupType = "differential"
	BackupTypeSnapshot    BackupType = "snapshot"
)

// BackupStatus defines the lifecycle state of a backup.
type BackupStatus string

const (
	BackupStatusPending    BackupStatus = "pending"
	BackupStatusRunning    BackupStatus = "running"
	BackupStatusCompleted  BackupStatus = "completed"
	BackupStatusFailed     BackupStatus = "failed"
	BackupStatusExpired    BackupStatus = "expired"
	BackupStatusDeleted    BackupStatus = "deleted"
)

// BackupTarget specifies what to back up.
type BackupTarget string

const (
	TargetDatabase    BackupTarget = "database"
	TargetObjectStore BackupTarget = "object_store"
	TargetConfig      BackupTarget = "config"
	TargetSecrets     BackupTarget = "secrets"
	TargetEtcd        BackupTarget = "etcd"
	TargetVolumes     BackupTarget = "volumes"
	TargetAll         BackupTarget = "all"
)

// Backup represents a single backup record.
type Backup struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Type          BackupType    `json:"type"`
	Target        BackupTarget  `json:"target"`
	Status        BackupStatus  `json:"status"`
	SizeBytes     int64         `json:"size_bytes"`
	Region        string        `json:"region"`
	StoragePath   string        `json:"storage_path"`
	Checksum      string        `json:"checksum"` // SHA-256
	EncryptionKey string        `json:"encryption_key,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	CompletedAt   *time.Time    `json:"completed_at,omitempty"`
	ExpiresAt     time.Time     `json:"expires_at"`
	Duration      time.Duration `json:"duration,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// ============================================================================
// Backup Schedule
// ============================================================================

// ScheduleFrequency defines how often backups run.
type ScheduleFrequency string

const (
	FrequencyHourly  ScheduleFrequency = "hourly"
	FrequencyDaily   ScheduleFrequency = "daily"
	FrequencyWeekly  ScheduleFrequency = "weekly"
	FrequencyMonthly ScheduleFrequency = "monthly"
)

// BackupSchedule defines a recurring backup job.
type BackupSchedule struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Frequency     ScheduleFrequency `json:"frequency"`
	CronExpr      string            `json:"cron_expr,omitempty"` // e.g. "0 2 * * *"
	Type          BackupType        `json:"type"`
	Target        BackupTarget      `json:"target"`
	RetentionDays int               `json:"retention_days"`
	Enabled       bool              `json:"enabled"`
	Region        string            `json:"region"`
	Encrypted     bool              `json:"encrypted"`
	LastRunAt     *time.Time        `json:"last_run_at,omitempty"`
	NextRunAt     *time.Time        `json:"next_run_at,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

// ============================================================================
// Cross-Region Disaster Recovery
// ============================================================================

// RegionStatus classifies the DR state of a region.
type RegionStatus string

const (
	RegionStatusActive   RegionStatus = "active"
	RegionStatusStandby  RegionStatus = "standby"
	RegionStatusDraining RegionStatus = "draining"
	RegionStatusFailed   RegionStatus = "failed"
)

// FailoverMode defines how failover is triggered.
type FailoverMode string

const (
	FailoverAutomatic FailoverMode = "automatic"
	FailoverManual    FailoverMode = "manual"
)

// DRRegion represents a disaster-recovery region configuration.
type DRRegion struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Provider        string       `json:"provider"` // aws, aliyun, etc.
	Location        string       `json:"location"` // us-east-1, cn-hangzhou, etc.
	Status          RegionStatus `json:"status"`
	IsPrimary       bool         `json:"is_primary"`
	RPOMinutes      int          `json:"rpo_minutes"` // Recovery Point Objective
	RTOMinutes      int          `json:"rto_minutes"` // Recovery Time Objective
	ReplicationLag  time.Duration `json:"replication_lag,omitempty"`
	LastSyncAt      *time.Time   `json:"last_sync_at,omitempty"`
	Endpoint        string       `json:"endpoint,omitempty"`
}

// ReplicationConfig defines cross-region replication settings.
type ReplicationConfig struct {
	SourceRegionID   string       `json:"source_region_id"`
	TargetRegionID   string       `json:"target_region_id"`
	Mode             string       `json:"mode"` // async, semi_sync, sync
	FailoverMode     FailoverMode `json:"failover_mode"`
	HealthCheckSecs  int          `json:"health_check_seconds"`
	MaxLagSeconds    int          `json:"max_lag_seconds"`
	Enabled          bool         `json:"enabled"`
}

// ============================================================================
// Recovery Drill
// ============================================================================

// DrillStatus defines the state of a recovery drill.
type DrillStatus string

const (
	DrillStatusScheduled DrillStatus = "scheduled"
	DrillStatusRunning   DrillStatus = "running"
	DrillStatusPassed    DrillStatus = "passed"
	DrillStatusFailed    DrillStatus = "failed"
	DrillStatusAborted   DrillStatus = "aborted"
)

// DrillStep represents a single step in a recovery drill.
type DrillStep struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Status      string        `json:"status"` // pending, running, passed, failed, skipped
	Duration    time.Duration `json:"duration,omitempty"`
	Output      string        `json:"output,omitempty"`
	Error       string        `json:"error,omitempty"`
}

// RecoveryDrill records one execution of a DR drill.
type RecoveryDrill struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Type        string        `json:"type"` // full, partial, tabletop
	Status      DrillStatus   `json:"status"`
	BackupID    string        `json:"backup_id,omitempty"`
	RegionID    string        `json:"region_id,omitempty"`
	Steps       []DrillStep   `json:"steps"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
	RPOAchieved time.Duration `json:"rpo_achieved,omitempty"`
	RTOAchieved time.Duration `json:"rto_achieved,omitempty"`
	Score       float64       `json:"score"` // 0-100 drill success score
	Notes       string        `json:"notes,omitempty"`
}

// ============================================================================
// Disaster Recovery Manager
// ============================================================================

// ManagerConfig configures the DR manager.
type ManagerConfig struct {
	DefaultRetentionDays int    `json:"default_retention_days"`
	DefaultRegion        string `json:"default_region"`
	EncryptionEnabled    bool   `json:"encryption_enabled"`
	Logger               *logrus.Logger
}

// Manager provides disaster recovery operations.
type Manager struct {
	config      ManagerConfig
	backups     map[string]*Backup
	schedules   map[string]*BackupSchedule
	regions     map[string]*DRRegion
	replications []*ReplicationConfig
	drills      []*RecoveryDrill
	logger      *logrus.Logger
	mu          sync.RWMutex
}

// NewManager creates a new disaster recovery manager.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.DefaultRetentionDays == 0 {
		cfg.DefaultRetentionDays = 30
	}
	if cfg.DefaultRegion == "" {
		cfg.DefaultRegion = "cn-hangzhou"
	}

	cfg.Logger.WithFields(logrus.Fields{
		"retention_days": cfg.DefaultRetentionDays,
		"region":         cfg.DefaultRegion,
		"encryption":     cfg.EncryptionEnabled,
	}).Info("Disaster recovery manager initialized")

	return &Manager{
		config:    cfg,
		backups:   make(map[string]*Backup),
		schedules: make(map[string]*BackupSchedule),
		regions:   make(map[string]*DRRegion),
		logger:    cfg.Logger,
	}
}

// ============================================================================
// Backup Operations
// ============================================================================

// CreateBackup initiates a new backup.
func (m *Manager) CreateBackup(name string, backupType BackupType, target BackupTarget) (*Backup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := common.NowUTC()
	backup := &Backup{
		ID:        common.NewUUID(),
		Name:      name,
		Type:      backupType,
		Target:    target,
		Status:    BackupStatusPending,
		Region:    m.config.DefaultRegion,
		CreatedAt: now,
		ExpiresAt: now.AddDate(0, 0, m.config.DefaultRetentionDays),
		Metadata:  make(map[string]string),
	}

	if m.config.EncryptionEnabled {
		backup.EncryptionKey = fmt.Sprintf("enc-%s", common.NewUUID()[:8])
	}

	m.backups[backup.ID] = backup
	m.logger.WithFields(logrus.Fields{
		"backup_id": backup.ID, "type": backupType, "target": target,
	}).Info("Backup created")

	return backup, nil
}

// StartBackup transitions a backup to running state (simulates execution).
func (m *Manager) StartBackup(backupID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	backup, ok := m.backups[backupID]
	if !ok {
		return fmt.Errorf("backup %s not found", backupID)
	}
	if backup.Status != BackupStatusPending {
		return fmt.Errorf("backup %s is not pending (status: %s)", backupID, backup.Status)
	}
	backup.Status = BackupStatusRunning
	return nil
}

// CompleteBackup marks a backup as completed with size and checksum.
func (m *Manager) CompleteBackup(backupID string, sizeBytes int64, checksum string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	backup, ok := m.backups[backupID]
	if !ok {
		return fmt.Errorf("backup %s not found", backupID)
	}
	if backup.Status != BackupStatusRunning {
		return fmt.Errorf("backup %s is not running", backupID)
	}

	now := common.NowUTC()
	backup.Status = BackupStatusCompleted
	backup.SizeBytes = sizeBytes
	backup.Checksum = checksum
	backup.CompletedAt = &now
	backup.Duration = now.Sub(backup.CreatedAt)
	backup.StoragePath = fmt.Sprintf("s3://%s/backups/%s/%s", m.config.DefaultRegion, backup.Target, backup.ID)

	m.logger.WithFields(logrus.Fields{
		"backup_id": backupID, "size_bytes": sizeBytes, "duration": backup.Duration,
	}).Info("Backup completed")

	return nil
}

// FailBackup marks a backup as failed.
func (m *Manager) FailBackup(backupID string, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	backup, ok := m.backups[backupID]
	if !ok {
		return fmt.Errorf("backup %s not found", backupID)
	}
	backup.Status = BackupStatusFailed
	backup.Metadata["failure_reason"] = reason
	return nil
}

// GetBackup returns a specific backup.
func (m *Manager) GetBackup(backupID string) (*Backup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	backup, ok := m.backups[backupID]
	if !ok {
		return nil, fmt.Errorf("backup %s not found", backupID)
	}
	return backup, nil
}

// ListBackups returns all backups sorted by creation time (newest first).
func (m *Manager) ListBackups() []*Backup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*Backup, 0, len(m.backups))
	for _, b := range m.backups {
		list = append(list, b)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list
}

// ListBackupsByTarget returns backups of a specific target type.
func (m *Manager) ListBackupsByTarget(target BackupTarget) []*Backup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []*Backup
	for _, b := range m.backups {
		if b.Target == target {
			list = append(list, b)
		}
	}
	return list
}

// CleanExpiredBackups removes backups past their expiration date.
func (m *Manager) CleanExpiredBackups() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := common.NowUTC()
	removed := 0
	for id, b := range m.backups {
		if now.After(b.ExpiresAt) && b.Status == BackupStatusCompleted {
			b.Status = BackupStatusExpired
			delete(m.backups, id)
			removed++
		}
	}
	if removed > 0 {
		m.logger.WithField("removed", removed).Info("Expired backups cleaned")
	}
	return removed
}

// ============================================================================
// Schedule Operations
// ============================================================================

// CreateSchedule adds a new backup schedule.
func (m *Manager) CreateSchedule(schedule *BackupSchedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if schedule.ID == "" {
		schedule.ID = common.NewUUID()
	}
	if schedule.RetentionDays == 0 {
		schedule.RetentionDays = m.config.DefaultRetentionDays
	}
	schedule.CreatedAt = common.NowUTC()

	m.schedules[schedule.ID] = schedule
	m.logger.WithFields(logrus.Fields{
		"schedule_id": schedule.ID, "frequency": schedule.Frequency, "target": schedule.Target,
	}).Info("Backup schedule created")

	return nil
}

// GetSchedule returns a specific schedule.
func (m *Manager) GetSchedule(scheduleID string) (*BackupSchedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.schedules[scheduleID]
	if !ok {
		return nil, fmt.Errorf("schedule %s not found", scheduleID)
	}
	return s, nil
}

// ListSchedules returns all backup schedules.
func (m *Manager) ListSchedules() []*BackupSchedule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*BackupSchedule, 0, len(m.schedules))
	for _, s := range m.schedules {
		list = append(list, s)
	}
	return list
}

// EnableSchedule enables or disables a schedule.
func (m *Manager) EnableSchedule(scheduleID string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.schedules[scheduleID]
	if !ok {
		return fmt.Errorf("schedule %s not found", scheduleID)
	}
	s.Enabled = enabled
	return nil
}

// ExecuteSchedule runs a backup according to a schedule (for cron triggers).
func (m *Manager) ExecuteSchedule(scheduleID string) (*Backup, error) {
	m.mu.Lock()
	schedule, ok := m.schedules[scheduleID]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("schedule %s not found", scheduleID)
	}
	if !schedule.Enabled {
		m.mu.Unlock()
		return nil, fmt.Errorf("schedule %s is disabled", scheduleID)
	}
	now := common.NowUTC()
	schedule.LastRunAt = &now
	m.mu.Unlock()

	name := fmt.Sprintf("scheduled-%s-%s", schedule.Name, now.Format("20060102-150405"))
	backup, err := m.CreateBackup(name, schedule.Type, schedule.Target)
	if err != nil {
		return nil, err
	}
	backup.Metadata["schedule_id"] = scheduleID
	return backup, nil
}

// ============================================================================
// Cross-Region DR
// ============================================================================

// AddRegion registers a DR region.
func (m *Manager) AddRegion(region *DRRegion) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if region.ID == "" {
		region.ID = common.NewUUID()
	}

	m.regions[region.ID] = region
	m.logger.WithFields(logrus.Fields{
		"region_id": region.ID, "name": region.Name,
		"location": region.Location, "primary": region.IsPrimary,
	}).Info("DR region registered")

	return nil
}

// GetRegion returns a specific region.
func (m *Manager) GetRegion(regionID string) (*DRRegion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	r, ok := m.regions[regionID]
	if !ok {
		return nil, fmt.Errorf("region %s not found", regionID)
	}
	return r, nil
}

// ListRegions returns all DR regions.
func (m *Manager) ListRegions() []*DRRegion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*DRRegion, 0, len(m.regions))
	for _, r := range m.regions {
		list = append(list, r)
	}
	return list
}

// GetPrimaryRegion returns the current primary region.
func (m *Manager) GetPrimaryRegion() *DRRegion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, r := range m.regions {
		if r.IsPrimary {
			return r
		}
	}
	return nil
}

// ConfigureReplication sets up cross-region replication.
func (m *Manager) ConfigureReplication(config *ReplicationConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.regions[config.SourceRegionID]; !ok {
		return fmt.Errorf("source region %s not found", config.SourceRegionID)
	}
	if _, ok := m.regions[config.TargetRegionID]; !ok {
		return fmt.Errorf("target region %s not found", config.TargetRegionID)
	}

	m.replications = append(m.replications, config)
	m.logger.WithFields(logrus.Fields{
		"source": config.SourceRegionID, "target": config.TargetRegionID,
		"mode": config.Mode, "failover": config.FailoverMode,
	}).Info("Replication configured")

	return nil
}

// Failover performs a failover to the specified target region.
func (m *Manager) Failover(targetRegionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	target, ok := m.regions[targetRegionID]
	if !ok {
		return fmt.Errorf("target region %s not found", targetRegionID)
	}
	if target.IsPrimary {
		return fmt.Errorf("region %s is already primary", targetRegionID)
	}

	// Demote current primary
	for _, r := range m.regions {
		if r.IsPrimary {
			r.IsPrimary = false
			r.Status = RegionStatusStandby
		}
	}

	// Promote target
	target.IsPrimary = true
	target.Status = RegionStatusActive

	m.logger.WithField("new_primary", targetRegionID).Warn("Failover executed")
	return nil
}

// GetReplicationStatus returns replication health info.
func (m *Manager) GetReplicationStatus() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []map[string]interface{}
	for _, r := range m.replications {
		source := m.regions[r.SourceRegionID]
		target := m.regions[r.TargetRegionID]
		status := map[string]interface{}{
			"source":        source.Name,
			"target":        target.Name,
			"mode":          r.Mode,
			"failover_mode": r.FailoverMode,
			"enabled":       r.Enabled,
			"source_status": source.Status,
			"target_status": target.Status,
		}
		if target.LastSyncAt != nil {
			status["last_sync"] = *target.LastSyncAt
			status["lag"] = time.Since(*target.LastSyncAt).String()
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// ============================================================================
// Recovery Drills
// ============================================================================

// StartDrill begins a one-click recovery drill.
func (m *Manager) StartDrill(name, drillType string) (*RecoveryDrill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	drill := &RecoveryDrill{
		ID:        common.NewUUID(),
		Name:      name,
		Type:      drillType,
		Status:    DrillStatusRunning,
		StartedAt: common.NowUTC(),
		Steps:     m.generateDrillSteps(drillType),
	}

	m.drills = append(m.drills, drill)
	m.logger.WithFields(logrus.Fields{
		"drill_id": drill.ID, "type": drillType, "steps": len(drill.Steps),
	}).Info("Recovery drill started")

	return drill, nil
}

// generateDrillSteps creates the steps for a given drill type.
func (m *Manager) generateDrillSteps(drillType string) []DrillStep {
	switch drillType {
	case "full":
		return []DrillStep{
			{Name: "validate_backup", Description: "Verify latest backup integrity", Status: "pending"},
			{Name: "provision_standby", Description: "Provision standby infrastructure", Status: "pending"},
			{Name: "restore_database", Description: "Restore database from backup", Status: "pending"},
			{Name: "restore_config", Description: "Restore configuration and secrets", Status: "pending"},
			{Name: "verify_data", Description: "Verify data integrity post-restore", Status: "pending"},
			{Name: "switch_traffic", Description: "Switch traffic to DR region", Status: "pending"},
			{Name: "validate_services", Description: "Validate all services are healthy", Status: "pending"},
			{Name: "measure_rpo_rto", Description: "Measure achieved RPO/RTO", Status: "pending"},
		}
	case "partial":
		return []DrillStep{
			{Name: "validate_backup", Description: "Verify latest backup integrity", Status: "pending"},
			{Name: "restore_database", Description: "Restore database from backup", Status: "pending"},
			{Name: "verify_data", Description: "Verify data integrity post-restore", Status: "pending"},
			{Name: "measure_rpo_rto", Description: "Measure achieved RPO/RTO", Status: "pending"},
		}
	case "tabletop":
		return []DrillStep{
			{Name: "review_runbook", Description: "Review DR runbook procedures", Status: "pending"},
			{Name: "verify_contacts", Description: "Verify on-call contacts and escalation", Status: "pending"},
			{Name: "check_backups", Description: "Verify recent backup availability", Status: "pending"},
			{Name: "review_rpo_rto", Description: "Review RPO/RTO targets", Status: "pending"},
		}
	default:
		return []DrillStep{
			{Name: "validate_backup", Description: "Verify latest backup integrity", Status: "pending"},
			{Name: "verify_data", Description: "Verify data integrity", Status: "pending"},
		}
	}
}

// UpdateDrillStep updates the status of a specific drill step.
func (m *Manager) UpdateDrillStep(drillID string, stepIndex int, status, output string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	drill := m.findDrill(drillID)
	if drill == nil {
		return fmt.Errorf("drill %s not found", drillID)
	}
	if stepIndex < 0 || stepIndex >= len(drill.Steps) {
		return fmt.Errorf("step index %d out of range (0-%d)", stepIndex, len(drill.Steps)-1)
	}

	drill.Steps[stepIndex].Status = status
	drill.Steps[stepIndex].Output = output
	return nil
}

// CompleteDrill finishes a drill and calculates the score.
func (m *Manager) CompleteDrill(drillID string, rpoAchieved, rtoAchieved time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	drill := m.findDrill(drillID)
	if drill == nil {
		return fmt.Errorf("drill %s not found", drillID)
	}

	now := common.NowUTC()
	drill.Status = DrillStatusPassed
	drill.CompletedAt = &now
	drill.Duration = now.Sub(drill.StartedAt)
	drill.RPOAchieved = rpoAchieved
	drill.RTOAchieved = rtoAchieved

	// Score based on step pass rate
	passed := 0
	for _, s := range drill.Steps {
		if s.Status == "passed" {
			passed++
		} else if s.Status == "failed" {
			drill.Status = DrillStatusFailed
		}
	}
	if len(drill.Steps) > 0 {
		drill.Score = float64(passed) / float64(len(drill.Steps)) * 100.0
	}

	m.logger.WithFields(logrus.Fields{
		"drill_id": drillID, "score": drill.Score,
		"rpo": rpoAchieved, "rto": rtoAchieved,
	}).Info("Recovery drill completed")

	return nil
}

// findDrill finds a drill by ID (caller must hold lock).
func (m *Manager) findDrill(drillID string) *RecoveryDrill {
	for _, d := range m.drills {
		if d.ID == drillID {
			return d
		}
	}
	return nil
}

// ListDrills returns all recovery drills.
func (m *Manager) ListDrills() []*RecoveryDrill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*RecoveryDrill, len(m.drills))
	copy(result, m.drills)
	return result
}

// GetLatestDrill returns the most recent drill.
func (m *Manager) GetLatestDrill() *RecoveryDrill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.drills) == 0 {
		return nil
	}
	return m.drills[len(m.drills)-1]
}

// ============================================================================
// Summary & Status
// ============================================================================

// GetDRSummary returns an overview of the disaster recovery posture.
func (m *Manager) GetDRSummary() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalBackups := len(m.backups)
	completedBackups := 0
	failedBackups := 0
	var latestBackupTime *time.Time
	var totalBackupSize int64

	for _, b := range m.backups {
		if b.Status == BackupStatusCompleted {
			completedBackups++
			totalBackupSize += b.SizeBytes
			if latestBackupTime == nil || b.CreatedAt.After(*latestBackupTime) {
				t := b.CreatedAt
				latestBackupTime = &t
			}
		} else if b.Status == BackupStatusFailed {
			failedBackups++
		}
	}

	activeSchedules := 0
	for _, s := range m.schedules {
		if s.Enabled {
			activeSchedules++
		}
	}

	primary := ""
	for _, r := range m.regions {
		if r.IsPrimary {
			primary = r.Name
		}
	}

	var lastDrillScore float64
	var lastDrillTime *time.Time
	if len(m.drills) > 0 {
		last := m.drills[len(m.drills)-1]
		lastDrillScore = last.Score
		if last.CompletedAt != nil {
			lastDrillTime = last.CompletedAt
		}
	}

	return map[string]interface{}{
		"total_backups":     totalBackups,
		"completed_backups": completedBackups,
		"failed_backups":    failedBackups,
		"total_backup_size": totalBackupSize,
		"latest_backup":     latestBackupTime,
		"active_schedules":  activeSchedules,
		"total_regions":     len(m.regions),
		"primary_region":    primary,
		"replications":      len(m.replications),
		"total_drills":      len(m.drills),
		"last_drill_score":  lastDrillScore,
		"last_drill_time":   lastDrillTime,
	}
}
