package disaster

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func newTestManager() *Manager {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	return NewManager(ManagerConfig{
		DefaultRetentionDays: 30,
		DefaultRegion:        "cn-hangzhou",
		EncryptionEnabled:    true,
		Logger:               logger,
	})
}

func TestNewManager(t *testing.T) {
	m := newTestManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.config.DefaultRetentionDays != 30 {
		t.Errorf("retention = %d, want 30", m.config.DefaultRetentionDays)
	}
	if m.config.DefaultRegion != "cn-hangzhou" {
		t.Errorf("region = %s, want cn-hangzhou", m.config.DefaultRegion)
	}
}

func TestNewManagerDefaults(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if m.config.DefaultRetentionDays != 30 {
		t.Errorf("default retention = %d, want 30", m.config.DefaultRetentionDays)
	}
	if m.config.DefaultRegion != "cn-hangzhou" {
		t.Errorf("default region = %s, want cn-hangzhou", m.config.DefaultRegion)
	}
}

// ============================================================================
// Backup Tests
// ============================================================================

func TestBackupLifecycle(t *testing.T) {
	m := newTestManager()

	// Create
	backup, err := m.CreateBackup("daily-db", BackupTypeFull, TargetDatabase)
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if backup.Status != BackupStatusPending {
		t.Errorf("status = %s, want pending", backup.Status)
	}
	if backup.EncryptionKey == "" {
		t.Error("encryption key should be set when enabled")
	}
	if backup.Region != "cn-hangzhou" {
		t.Errorf("region = %s, want cn-hangzhou", backup.Region)
	}

	// Start
	if err := m.StartBackup(backup.ID); err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	b, _ := m.GetBackup(backup.ID)
	if b.Status != BackupStatusRunning {
		t.Errorf("status = %s, want running", b.Status)
	}

	// Complete
	if err := m.CompleteBackup(backup.ID, 1024*1024*500, "sha256:abc123"); err != nil {
		t.Fatalf("CompleteBackup: %v", err)
	}
	b, _ = m.GetBackup(backup.ID)
	if b.Status != BackupStatusCompleted {
		t.Errorf("status = %s, want completed", b.Status)
	}
	if b.SizeBytes != 1024*1024*500 {
		t.Errorf("size = %d, want %d", b.SizeBytes, 1024*1024*500)
	}
	if b.Checksum != "sha256:abc123" {
		t.Errorf("checksum = %s, want sha256:abc123", b.Checksum)
	}
	if b.CompletedAt == nil {
		t.Error("completed_at should be set")
	}
	if b.StoragePath == "" {
		t.Error("storage_path should be set")
	}
}

func TestBackupStartErrors(t *testing.T) {
	m := newTestManager()

	// Not found
	if err := m.StartBackup("nonexistent"); err == nil {
		t.Error("expected error for nonexistent backup")
	}

	// Already started
	backup, _ := m.CreateBackup("test", BackupTypeFull, TargetDatabase)
	m.StartBackup(backup.ID)
	if err := m.StartBackup(backup.ID); err == nil {
		t.Error("expected error for already running backup")
	}
}

func TestBackupCompleteErrors(t *testing.T) {
	m := newTestManager()

	// Not found
	if err := m.CompleteBackup("nonexistent", 100, "x"); err == nil {
		t.Error("expected error for nonexistent backup")
	}

	// Not running
	backup, _ := m.CreateBackup("test", BackupTypeFull, TargetDatabase)
	if err := m.CompleteBackup(backup.ID, 100, "x"); err == nil {
		t.Error("expected error for non-running backup")
	}
}

func TestFailBackup(t *testing.T) {
	m := newTestManager()
	backup, _ := m.CreateBackup("test", BackupTypeFull, TargetDatabase)
	m.StartBackup(backup.ID)

	if err := m.FailBackup(backup.ID, "disk full"); err != nil {
		t.Fatalf("FailBackup: %v", err)
	}
	b, _ := m.GetBackup(backup.ID)
	if b.Status != BackupStatusFailed {
		t.Errorf("status = %s, want failed", b.Status)
	}
	if b.Metadata["failure_reason"] != "disk full" {
		t.Error("failure reason not recorded")
	}
}

func TestListBackups(t *testing.T) {
	m := newTestManager()
	m.CreateBackup("b1", BackupTypeFull, TargetDatabase)
	m.CreateBackup("b2", BackupTypeIncremental, TargetConfig)
	m.CreateBackup("b3", BackupTypeSnapshot, TargetEtcd)

	list := m.ListBackups()
	if len(list) != 3 {
		t.Errorf("list count = %d, want 3", len(list))
	}
}

func TestListBackupsByTarget(t *testing.T) {
	m := newTestManager()
	m.CreateBackup("b1", BackupTypeFull, TargetDatabase)
	m.CreateBackup("b2", BackupTypeFull, TargetDatabase)
	m.CreateBackup("b3", BackupTypeFull, TargetConfig)

	dbBackups := m.ListBackupsByTarget(TargetDatabase)
	if len(dbBackups) != 2 {
		t.Errorf("db backups = %d, want 2", len(dbBackups))
	}
}

func TestCleanExpiredBackups(t *testing.T) {
	m := NewManager(ManagerConfig{DefaultRetentionDays: 1})

	// Create a completed backup with past expiry
	backup, _ := m.CreateBackup("old", BackupTypeFull, TargetDatabase)
	m.StartBackup(backup.ID)
	m.CompleteBackup(backup.ID, 100, "x")
	// Manually set expiry to the past
	m.mu.Lock()
	m.backups[backup.ID].ExpiresAt = time.Now().UTC().Add(-24 * time.Hour)
	m.mu.Unlock()

	removed := m.CleanExpiredBackups()
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
}

// ============================================================================
// Schedule Tests
// ============================================================================

func TestScheduleLifecycle(t *testing.T) {
	m := newTestManager()

	schedule := &BackupSchedule{
		Name:      "nightly-full",
		Frequency: FrequencyDaily,
		CronExpr:  "0 2 * * *",
		Type:      BackupTypeFull,
		Target:    TargetAll,
		Enabled:   true,
		Region:    "cn-hangzhou",
	}

	if err := m.CreateSchedule(schedule); err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if schedule.ID == "" {
		t.Error("schedule ID should be generated")
	}
	if schedule.RetentionDays != 30 {
		t.Errorf("retention = %d, want 30 (default)", schedule.RetentionDays)
	}

	s, err := m.GetSchedule(schedule.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if s.Name != "nightly-full" {
		t.Errorf("name = %s, want nightly-full", s.Name)
	}

	schedules := m.ListSchedules()
	if len(schedules) != 1 {
		t.Errorf("schedules count = %d, want 1", len(schedules))
	}
}

func TestEnableDisableSchedule(t *testing.T) {
	m := newTestManager()
	schedule := &BackupSchedule{Name: "test", Enabled: true, Target: TargetDatabase, Type: BackupTypeFull}
	m.CreateSchedule(schedule)

	m.EnableSchedule(schedule.ID, false)
	s, _ := m.GetSchedule(schedule.ID)
	if s.Enabled {
		t.Error("schedule should be disabled")
	}

	m.EnableSchedule(schedule.ID, true)
	s, _ = m.GetSchedule(schedule.ID)
	if !s.Enabled {
		t.Error("schedule should be enabled")
	}
}

func TestExecuteSchedule(t *testing.T) {
	m := newTestManager()
	schedule := &BackupSchedule{
		Name: "hourly", Frequency: FrequencyHourly, Enabled: true,
		Target: TargetDatabase, Type: BackupTypeIncremental,
	}
	m.CreateSchedule(schedule)

	backup, err := m.ExecuteSchedule(schedule.ID)
	if err != nil {
		t.Fatalf("ExecuteSchedule: %v", err)
	}
	if backup == nil {
		t.Fatal("backup should not be nil")
	}
	if backup.Metadata["schedule_id"] != schedule.ID {
		t.Error("schedule_id metadata not set")
	}

	s, _ := m.GetSchedule(schedule.ID)
	if s.LastRunAt == nil {
		t.Error("last_run_at should be set")
	}
}

func TestExecuteDisabledSchedule(t *testing.T) {
	m := newTestManager()
	schedule := &BackupSchedule{Name: "test", Enabled: false, Target: TargetDatabase, Type: BackupTypeFull}
	m.CreateSchedule(schedule)

	_, err := m.ExecuteSchedule(schedule.ID)
	if err == nil {
		t.Error("expected error for disabled schedule")
	}
}

// ============================================================================
// Region & Replication Tests
// ============================================================================

func TestRegionManagement(t *testing.T) {
	m := newTestManager()

	primary := &DRRegion{
		Name: "Primary", Provider: "aliyun", Location: "cn-hangzhou",
		Status: RegionStatusActive, IsPrimary: true, RPOMinutes: 5, RTOMinutes: 30,
	}
	standby := &DRRegion{
		Name: "Standby", Provider: "aliyun", Location: "cn-shanghai",
		Status: RegionStatusStandby, IsPrimary: false, RPOMinutes: 5, RTOMinutes: 30,
	}

	m.AddRegion(primary)
	m.AddRegion(standby)

	regions := m.ListRegions()
	if len(regions) != 2 {
		t.Errorf("regions = %d, want 2", len(regions))
	}

	p := m.GetPrimaryRegion()
	if p == nil || p.Name != "Primary" {
		t.Error("primary region should be 'Primary'")
	}
}

func TestReplicationConfig(t *testing.T) {
	m := newTestManager()

	primary := &DRRegion{ID: "r1", Name: "Primary", IsPrimary: true, Status: RegionStatusActive}
	standby := &DRRegion{ID: "r2", Name: "Standby", IsPrimary: false, Status: RegionStatusStandby}
	m.AddRegion(primary)
	m.AddRegion(standby)

	err := m.ConfigureReplication(&ReplicationConfig{
		SourceRegionID: "r1", TargetRegionID: "r2",
		Mode: "async", FailoverMode: FailoverAutomatic,
		HealthCheckSecs: 30, MaxLagSeconds: 60, Enabled: true,
	})
	if err != nil {
		t.Fatalf("ConfigureReplication: %v", err)
	}

	statuses := m.GetReplicationStatus()
	if len(statuses) != 1 {
		t.Fatalf("replication statuses = %d, want 1", len(statuses))
	}
	if statuses[0]["source"] != "Primary" {
		t.Error("source should be Primary")
	}
}

func TestReplicationConfigErrors(t *testing.T) {
	m := newTestManager()
	primary := &DRRegion{ID: "r1", Name: "P", IsPrimary: true}
	m.AddRegion(primary)

	err := m.ConfigureReplication(&ReplicationConfig{
		SourceRegionID: "r1", TargetRegionID: "nonexistent",
	})
	if err == nil {
		t.Error("expected error for nonexistent target region")
	}
}

func TestFailover(t *testing.T) {
	m := newTestManager()
	primary := &DRRegion{ID: "r1", Name: "Primary", IsPrimary: true, Status: RegionStatusActive}
	standby := &DRRegion{ID: "r2", Name: "Standby", IsPrimary: false, Status: RegionStatusStandby}
	m.AddRegion(primary)
	m.AddRegion(standby)

	if err := m.Failover("r2"); err != nil {
		t.Fatalf("Failover: %v", err)
	}

	r1, _ := m.GetRegion("r1")
	if r1.IsPrimary {
		t.Error("r1 should no longer be primary")
	}
	r2, _ := m.GetRegion("r2")
	if !r2.IsPrimary {
		t.Error("r2 should be primary after failover")
	}
	if r2.Status != RegionStatusActive {
		t.Errorf("r2 status = %s, want active", r2.Status)
	}
}

func TestFailoverErrors(t *testing.T) {
	m := newTestManager()
	primary := &DRRegion{ID: "r1", IsPrimary: true, Status: RegionStatusActive}
	m.AddRegion(primary)

	// Failover to self
	if err := m.Failover("r1"); err == nil {
		t.Error("expected error for failover to current primary")
	}

	// Nonexistent
	if err := m.Failover("nonexistent"); err == nil {
		t.Error("expected error for nonexistent region")
	}
}

// ============================================================================
// Drill Tests
// ============================================================================

func TestDrillLifecycle(t *testing.T) {
	m := newTestManager()

	drill, err := m.StartDrill("Q1 DR Drill", "full")
	if err != nil {
		t.Fatalf("StartDrill: %v", err)
	}
	if drill.Status != DrillStatusRunning {
		t.Errorf("status = %s, want running", drill.Status)
	}
	if len(drill.Steps) != 8 {
		t.Errorf("full drill steps = %d, want 8", len(drill.Steps))
	}

	// Update all steps to passed
	for i := range drill.Steps {
		m.UpdateDrillStep(drill.ID, i, "passed", "OK")
	}

	err = m.CompleteDrill(drill.ID, 3*time.Minute, 15*time.Minute)
	if err != nil {
		t.Fatalf("CompleteDrill: %v", err)
	}

	latest := m.GetLatestDrill()
	if latest.Score != 100.0 {
		t.Errorf("score = %f, want 100.0", latest.Score)
	}
	if latest.Status != DrillStatusPassed {
		t.Errorf("status = %s, want passed", latest.Status)
	}
	if latest.RPOAchieved != 3*time.Minute {
		t.Error("RPO not recorded correctly")
	}
}

func TestDrillTypes(t *testing.T) {
	m := newTestManager()

	partial, _ := m.StartDrill("Partial", "partial")
	if len(partial.Steps) != 4 {
		t.Errorf("partial drill steps = %d, want 4", len(partial.Steps))
	}

	tabletop, _ := m.StartDrill("Tabletop", "tabletop")
	if len(tabletop.Steps) != 4 {
		t.Errorf("tabletop drill steps = %d, want 4", len(tabletop.Steps))
	}

	custom, _ := m.StartDrill("Custom", "unknown")
	if len(custom.Steps) != 2 {
		t.Errorf("default drill steps = %d, want 2", len(custom.Steps))
	}
}

func TestDrillWithFailedSteps(t *testing.T) {
	m := newTestManager()

	drill, _ := m.StartDrill("Test", "partial")
	m.UpdateDrillStep(drill.ID, 0, "passed", "OK")
	m.UpdateDrillStep(drill.ID, 1, "failed", "Restore failed: timeout")
	m.UpdateDrillStep(drill.ID, 2, "passed", "OK")
	m.UpdateDrillStep(drill.ID, 3, "passed", "OK")

	m.CompleteDrill(drill.ID, 10*time.Minute, 45*time.Minute)

	latest := m.GetLatestDrill()
	if latest.Status != DrillStatusFailed {
		t.Errorf("status = %s, want failed (has failed step)", latest.Status)
	}
	if latest.Score != 75.0 {
		t.Errorf("score = %f, want 75.0 (3/4 passed)", latest.Score)
	}
}

func TestUpdateDrillStepErrors(t *testing.T) {
	m := newTestManager()

	if err := m.UpdateDrillStep("nonexistent", 0, "passed", ""); err == nil {
		t.Error("expected error for nonexistent drill")
	}

	drill, _ := m.StartDrill("Test", "partial")
	if err := m.UpdateDrillStep(drill.ID, 99, "passed", ""); err == nil {
		t.Error("expected error for out-of-range step index")
	}
}

func TestListDrills(t *testing.T) {
	m := newTestManager()
	m.StartDrill("D1", "full")
	m.StartDrill("D2", "partial")

	drills := m.ListDrills()
	if len(drills) != 2 {
		t.Errorf("drills = %d, want 2", len(drills))
	}
}

func TestGetLatestDrillEmpty(t *testing.T) {
	m := newTestManager()
	if m.GetLatestDrill() != nil {
		t.Error("expected nil for empty drills")
	}
}

// ============================================================================
// Summary Test
// ============================================================================

func TestGetDRSummary(t *testing.T) {
	m := newTestManager()

	// Add some data
	b, _ := m.CreateBackup("b1", BackupTypeFull, TargetDatabase)
	m.StartBackup(b.ID)
	m.CompleteBackup(b.ID, 1024, "sha256:x")

	m.CreateSchedule(&BackupSchedule{Name: "nightly", Enabled: true, Target: TargetAll, Type: BackupTypeFull})

	primary := &DRRegion{ID: "r1", Name: "Primary", IsPrimary: true, Status: RegionStatusActive}
	m.AddRegion(primary)

	drill, _ := m.StartDrill("Test", "tabletop")
	for i := range m.drills[0].Steps {
		m.UpdateDrillStep(drill.ID, i, "passed", "OK")
	}
	m.CompleteDrill(drill.ID, 5*time.Minute, 20*time.Minute)

	summary := m.GetDRSummary()
	if summary["total_backups"].(int) != 1 {
		t.Errorf("total_backups = %v, want 1", summary["total_backups"])
	}
	if summary["completed_backups"].(int) != 1 {
		t.Errorf("completed_backups = %v, want 1", summary["completed_backups"])
	}
	if summary["active_schedules"].(int) != 1 {
		t.Errorf("active_schedules = %v, want 1", summary["active_schedules"])
	}
	if summary["primary_region"].(string) != "Primary" {
		t.Errorf("primary_region = %v, want Primary", summary["primary_region"])
	}
	if summary["last_drill_score"].(float64) != 100.0 {
		t.Errorf("last_drill_score = %v, want 100", summary["last_drill_score"])
	}
}
