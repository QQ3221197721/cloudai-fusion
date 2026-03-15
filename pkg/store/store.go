// Package store provides the database access layer for CloudAI Fusion,
// using GORM with PostgreSQL. Handles schema migration, CRUD operations,
// and connection lifecycle management.
package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config holds database connection configuration
type Config struct {
	DSN            string
	MaxOpenConns   int
	MaxIdleConns   int
	ConnMaxLifetime time.Duration
	LogLevel       string // silent, error, warn, info
}

// Store wraps GORM DB and provides typed query methods
type Store struct {
	db     *gorm.DB
	logger *logrus.Logger
}

// New creates a new Store instance, opens the DB connection, and runs AutoMigrate.
func New(cfg Config) (*Store, error) {
	gormLogLevel := logger.Silent
	switch cfg.LogLevel {
	case "info":
		gormLogLevel = logger.Info
	case "warn":
		gormLogLevel = logger.Warn
	case "error":
		gormLogLevel = logger.Error
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	maxOpen := cfg.MaxOpenConns
	if maxOpen == 0 {
		maxOpen = 25
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle == 0 {
		maxIdle = 10
	}
	connLifetime := cfg.ConnMaxLifetime
	if connLifetime == 0 {
		connLifetime = 5 * time.Minute
	}

	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxLifetime(connLifetime)

	// AutoMigrate all models — migrate one by one to tolerate pre-existing tables
	// created by init-db.sql (which may lack GORM-specific columns like deleted_at).
	models := []interface{}{
		&User{}, &AuditLog{},
		&ClusterModel{}, &WorkloadModel{}, &WorkloadEvent{},
		&SecurityPolicyModel{}, &VulnerabilityScanModel{},
		&MeshPolicyModel{},
		&WasmModuleModel{}, &WasmInstanceModel{},
		&EdgeNodeModel{},
		&AlertRuleModel{}, &AlertEventModel{},
		&SchedulerSnapshotModel{},
	}
	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			// SQLSTATE 42P07 = duplicate_table — table created by init-db.sql, safe to skip
			if strings.Contains(err.Error(), "42P07") || strings.Contains(err.Error(), "already exists") {
				logrus.WithError(err).Debug("Table already exists (from init-db.sql), skipping")
				continue
			}
			return nil, fmt.Errorf("failed to auto-migrate: %w", err)
		}
	}

	return &Store{
		db:     db,
		logger: logrus.StandardLogger(),
	}, nil
}

// DB returns the underlying GORM DB (for advanced queries)
func (s *Store) DB() *gorm.DB {
	return s.db
}

// Close closes the database connection
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Ping checks database connectivity
func (s *Store) Ping() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

// ============================================================================
// User CRUD
// ============================================================================

// CreateUser inserts a new user record
func (s *Store) CreateUser(user *User) error {
	return s.db.Create(user).Error
}

// GetUserByID finds a user by primary key
func (s *Store) GetUserByID(id string) (*User, error) {
	var user User
	if err := s.db.Where("id = ?", id).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByUsername finds a user by username
func (s *Store) GetUserByUsername(username string) (*User, error) {
	var user User
	if err := s.db.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByEmail finds a user by email
func (s *Store) GetUserByEmail(email string) (*User, error) {
	var user User
	if err := s.db.Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// ListUsers returns paginated user list
func (s *Store) ListUsers(offset, limit int) ([]User, int64, error) {
	var users []User
	var total int64
	s.db.Model(&User{}).Count(&total)
	if err := s.db.Offset(offset).Limit(limit).Order("created_at DESC").Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// UpdateUser updates user fields
func (s *Store) UpdateUser(user *User) error {
	return s.db.Save(user).Error
}

// DeleteUser soft-deletes a user
func (s *Store) DeleteUser(id string) error {
	return s.db.Where("id = ?", id).Delete(&User{}).Error
}

// ============================================================================
// Audit Log
// ============================================================================

// CreateAuditLog inserts an audit log entry
func (s *Store) CreateAuditLog(log *AuditLog) error {
	return s.db.Create(log).Error
}

// ListAuditLogs returns recent audit logs
func (s *Store) ListAuditLogs(limit int) ([]AuditLog, error) {
	var logs []AuditLog
	if err := s.db.Order("created_at DESC").Limit(limit).Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// ============================================================================
// Cluster CRUD
// ============================================================================

// CreateCluster inserts a new cluster record
func (s *Store) CreateCluster(cluster *ClusterModel) error {
	return s.db.Create(cluster).Error
}

// GetClusterByID finds a cluster by primary key
func (s *Store) GetClusterByID(id string) (*ClusterModel, error) {
	var c ClusterModel
	if err := s.db.Where("id = ?", id).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ListClusters returns paginated cluster list
func (s *Store) ListClusters(offset, limit int) ([]ClusterModel, int64, error) {
	var clusters []ClusterModel
	var total int64
	s.db.Model(&ClusterModel{}).Count(&total)
	if err := s.db.Offset(offset).Limit(limit).Order("created_at DESC").Find(&clusters).Error; err != nil {
		return nil, 0, err
	}
	return clusters, total, nil
}

// UpdateCluster updates cluster fields
func (s *Store) UpdateCluster(cluster *ClusterModel) error {
	return s.db.Save(cluster).Error
}

// DeleteCluster soft-deletes a cluster
func (s *Store) DeleteCluster(id string) error {
	return s.db.Where("id = ?", id).Delete(&ClusterModel{}).Error
}

// ============================================================================
// Workload CRUD
// ============================================================================

// CreateWorkload inserts a new workload record
func (s *Store) CreateWorkload(w *WorkloadModel) error {
	return s.db.Create(w).Error
}

// GetWorkloadByID finds a workload by primary key
func (s *Store) GetWorkloadByID(id string) (*WorkloadModel, error) {
	var w WorkloadModel
	if err := s.db.Where("id = ?", id).First(&w).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// ListWorkloads returns paginated workload list with optional filters
func (s *Store) ListWorkloads(clusterID, status string, offset, limit int) ([]WorkloadModel, int64, error) {
	var workloads []WorkloadModel
	var total int64

	q := s.db.Model(&WorkloadModel{})
	if clusterID != "" {
		q = q.Where("cluster_id = ?", clusterID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	q.Count(&total)

	if err := q.Offset(offset).Limit(limit).Order("created_at DESC").Find(&workloads).Error; err != nil {
		return nil, 0, err
	}
	return workloads, total, nil
}

// UpdateWorkload updates workload fields
func (s *Store) UpdateWorkload(w *WorkloadModel) error {
	return s.db.Save(w).Error
}

// DeleteWorkload soft-deletes a workload
func (s *Store) DeleteWorkload(id string) error {
	return s.db.Where("id = ?", id).Delete(&WorkloadModel{}).Error
}

// UpdateWorkloadStatus updates only the status field and records an event
func (s *Store) UpdateWorkloadStatus(id, fromStatus, toStatus, reason string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// Update status
		result := tx.Model(&WorkloadModel{}).Where("id = ? AND status = ?", id, fromStatus).
			Updates(map[string]interface{}{"status": toStatus, "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("workload %s not in expected status %s", id, fromStatus)
		}

		// Record state transition event
		event := &WorkloadEvent{
			ID:         newUUID(),
			WorkloadID: id,
			FromStatus: fromStatus,
			ToStatus:   toStatus,
			Reason:     reason,
			CreatedAt:  time.Now().UTC(),
		}
		return tx.Create(event).Error
	})
}

// ListWorkloadEvents returns events for a specific workload
func (s *Store) ListWorkloadEvents(workloadID string, limit int) ([]WorkloadEvent, error) {
	var events []WorkloadEvent
	if err := s.db.Where("workload_id = ?", workloadID).Order("created_at DESC").Limit(limit).Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// ============================================================================
// Security Policy CRUD
// ============================================================================

// CreateSecurityPolicy inserts a new security policy
func (s *Store) CreateSecurityPolicy(p *SecurityPolicyModel) error {
	return s.db.Create(p).Error
}

// GetSecurityPolicyByID finds a security policy by ID
func (s *Store) GetSecurityPolicyByID(id string) (*SecurityPolicyModel, error) {
	var p SecurityPolicyModel
	if err := s.db.Where("id = ?", id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// ListSecurityPolicies returns all active security policies
func (s *Store) ListSecurityPolicies(offset, limit int) ([]SecurityPolicyModel, int64, error) {
	var policies []SecurityPolicyModel
	var total int64
	s.db.Model(&SecurityPolicyModel{}).Count(&total)
	if err := s.db.Offset(offset).Limit(limit).Order("created_at DESC").Find(&policies).Error; err != nil {
		return nil, 0, err
	}
	return policies, total, nil
}

// DeleteSecurityPolicy soft-deletes a security policy
func (s *Store) DeleteSecurityPolicy(id string) error {
	return s.db.Where("id = ?", id).Delete(&SecurityPolicyModel{}).Error
}

// newUUID generates a UUID (avoiding circular import with common)
func newUUID() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		time.Now().UnixNano()&0xFFFFFFFF,
		time.Now().UnixNano()>>32&0xFFFF,
		0x4000|time.Now().UnixNano()>>48&0x0FFF,
		0x8000|time.Now().UnixNano()>>60&0x3FFF,
		time.Now().UnixNano()&0xFFFFFFFFFFFF)
}

// ============================================================================
// Scheduler Snapshot — Queue crash recovery
// ============================================================================

// SaveSchedulerSnapshot persists the scheduler queue state for crash recovery.
// Uses upsert semantics: only one snapshot (key="scheduler") is kept.
func (s *Store) SaveSchedulerSnapshot(data string) error {
	snap := SchedulerSnapshotModel{
		Key:       "scheduler",
		Data:      data,
		UpdatedAt: time.Now().UTC(),
	}
	// Upsert: ON CONFLICT(key) DO UPDATE
	return s.db.Where("key = ?", "scheduler").Assign(snap).FirstOrCreate(&snap).Error
}

// LoadSchedulerSnapshot retrieves the last saved scheduler queue state.
// Returns empty string if no snapshot exists.
func (s *Store) LoadSchedulerSnapshot() (string, error) {
	var snap SchedulerSnapshotModel
	result := s.db.Where("key = ?", "scheduler").First(&snap)
	if result.Error != nil {
		if result.RowsAffected == 0 {
			return "", nil // no snapshot yet
		}
		return "", result.Error
	}
	return snap.Data, nil
}
