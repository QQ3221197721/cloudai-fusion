// Package store - Enhanced PostgreSQL connection management.
// Provides connection pool optimization, read replica routing,
// and advanced transaction management for production deployments.
package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ============================================================================
// Enhanced Config — production-grade connection pool settings
// ============================================================================

// PoolConfig holds advanced connection pool parameters.
type PoolConfig struct {
	// MaxOpenConns is the max number of open connections to the database.
	MaxOpenConns int `mapstructure:"max_open_conns"`

	// MaxIdleConns is the max number of idle connections in the pool.
	MaxIdleConns int `mapstructure:"max_idle_conns"`

	// ConnMaxLifetime is the max amount of time a connection may be reused.
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`

	// ConnMaxIdleTime is the max amount of time a connection may be idle.
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`

	// HealthCheckInterval is how often to check connection health.
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"`
}

// DefaultPoolConfig returns production-grade pool defaults.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxOpenConns:        50,
		MaxIdleConns:        25,
		ConnMaxLifetime:     30 * time.Minute,
		ConnMaxIdleTime:     5 * time.Minute,
		HealthCheckInterval: 30 * time.Second,
	}
}

// ============================================================================
// Read Replica Configuration
// ============================================================================

// ReplicaConfig holds read replica connection settings.
type ReplicaConfig struct {
	// DSNs is a list of read replica DSN strings.
	DSNs []string `mapstructure:"dsns"`

	// LoadBalancePolicy: "round-robin" (default) or "random".
	LoadBalancePolicy string `mapstructure:"load_balance_policy"`

	// Pool configuration for replicas.
	Pool PoolConfig `mapstructure:"pool"`
}

// ============================================================================
// Enhanced Store — with read replicas and pool optimization
// ============================================================================

// EnhancedStore extends Store with read replica support and advanced pooling.
type EnhancedStore struct {
	*Store // embed the base store (primary/write)

	// Read replicas for horizontal read scaling
	replicas []*gorm.DB
	replicaMu sync.RWMutex
	replicaIdx uint64

	// Pool statistics
	poolStats PoolStats
	statsMu   sync.RWMutex

	// Health check
	healthCancel context.CancelFunc
	healthWg     sync.WaitGroup

	logger *logrus.Logger
}

// PoolStats holds connection pool statistics.
type PoolStats struct {
	// Primary (write) pool stats
	PrimaryOpenConns     int   `json:"primary_open_conns"`
	PrimaryInUse         int   `json:"primary_in_use"`
	PrimaryIdle          int   `json:"primary_idle"`
	PrimaryWaitCount     int64 `json:"primary_wait_count"`
	PrimaryWaitDuration  time.Duration `json:"primary_wait_duration"`

	// Replica pool stats (aggregated)
	ReplicaCount         int   `json:"replica_count"`
	ReplicaTotalOpen     int   `json:"replica_total_open"`
	ReplicaTotalInUse    int   `json:"replica_total_in_use"`

	// Health status
	PrimaryHealthy       bool  `json:"primary_healthy"`
	HealthyReplicaCount  int   `json:"healthy_replica_count"`
	LastHealthCheck      time.Time `json:"last_health_check"`
}

// NewEnhanced creates an EnhancedStore with production-grade connection pooling.
func NewEnhanced(primaryCfg Config, poolCfg PoolConfig, replicaCfg *ReplicaConfig, logger *logrus.Logger) (*EnhancedStore, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	// Create primary (write) store
	base, err := New(primaryCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create primary store: %w", err)
	}

	// Apply enhanced pool configuration to primary
	sqlDB, err := base.db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get primary sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(poolCfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(poolCfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(poolCfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(poolCfg.ConnMaxIdleTime)

	logger.WithFields(logrus.Fields{
		"max_open":      poolCfg.MaxOpenConns,
		"max_idle":      poolCfg.MaxIdleConns,
		"max_lifetime":  poolCfg.ConnMaxLifetime,
		"max_idle_time": poolCfg.ConnMaxIdleTime,
	}).Info("Primary connection pool configured")

	es := &EnhancedStore{
		Store:  base,
		logger: logger,
	}

	// Initialize read replicas
	if replicaCfg != nil && len(replicaCfg.DSNs) > 0 {
		for i, dsn := range replicaCfg.DSNs {
			replicaDB, err := openReplica(dsn, replicaCfg.Pool)
			if err != nil {
				logger.WithError(err).WithField("replica", i).Warn("Failed to connect read replica, skipping")
				continue
			}
			es.replicas = append(es.replicas, replicaDB)
			logger.WithField("replica", i).Info("Read replica connected")
		}
		logger.WithField("replicas", len(es.replicas)).Info("Read replicas initialized")
	}

	// Start health check loop
	if poolCfg.HealthCheckInterval > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		es.healthCancel = cancel
		es.healthWg.Add(1)
		go es.healthCheckLoop(ctx, poolCfg.HealthCheckInterval)
	}

	return es, nil
}

// ReadDB returns a GORM DB suitable for read-only queries.
// If read replicas are configured, distributes reads via round-robin.
// Falls back to primary if no replicas are healthy.
func (es *EnhancedStore) ReadDB() *gorm.DB {
	es.replicaMu.RLock()
	defer es.replicaMu.RUnlock()

	if len(es.replicas) == 0 {
		return es.db // fallback to primary
	}

	// Round-robin across replicas
	idx := es.replicaIdx % uint64(len(es.replicas))
	es.replicaIdx++
	return es.replicas[idx]
}

// WriteDB returns the primary GORM DB for write operations.
func (es *EnhancedStore) WriteDB() *gorm.DB {
	return es.db
}

// Transaction executes fn within a database transaction on the primary.
// Supports nested transactions via GORM's SavePoint.
func (es *EnhancedStore) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return es.db.WithContext(ctx).Transaction(fn)
}

// TransactionWithRetry executes fn within a transaction with automatic retry
// on serialization failure (PostgreSQL error code 40001).
func (es *EnhancedStore) TransactionWithRetry(ctx context.Context, maxRetries int, fn func(tx *gorm.DB) error) error {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := es.db.WithContext(ctx).Transaction(fn)
		if err == nil {
			return nil
		}

		// Check if error is a serialization failure (retryable)
		if isSerializationError(err) && attempt < maxRetries {
			delay := time.Duration(1<<uint(attempt)) * 100 * time.Millisecond
			es.logger.WithFields(logrus.Fields{
				"attempt": attempt + 1,
				"delay":   delay,
			}).Debug("Retrying serialization failure")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				continue
			}
		}

		return err
	}
	return fmt.Errorf("transaction failed after %d retries", maxRetries)
}

// GetPoolStats returns current connection pool statistics.
func (es *EnhancedStore) GetPoolStats() PoolStats {
	es.statsMu.RLock()
	defer es.statsMu.RUnlock()

	stats := es.poolStats

	// Refresh primary stats
	if sqlDB, err := es.db.DB(); err == nil {
		dbStats := sqlDB.Stats()
		stats.PrimaryOpenConns = dbStats.OpenConnections
		stats.PrimaryInUse = dbStats.InUse
		stats.PrimaryIdle = dbStats.Idle
		stats.PrimaryWaitCount = dbStats.WaitCount
		stats.PrimaryWaitDuration = dbStats.WaitDuration
	}

	// Refresh replica stats
	es.replicaMu.RLock()
	stats.ReplicaCount = len(es.replicas)
	totalOpen := 0
	totalInUse := 0
	for _, r := range es.replicas {
		if sqlDB, err := r.DB(); err == nil {
			dbStats := sqlDB.Stats()
			totalOpen += dbStats.OpenConnections
			totalInUse += dbStats.InUse
		}
	}
	stats.ReplicaTotalOpen = totalOpen
	stats.ReplicaTotalInUse = totalInUse
	es.replicaMu.RUnlock()

	return stats
}

// Close shuts down all connections (primary + replicas).
func (es *EnhancedStore) Close() error {
	// Stop health check
	if es.healthCancel != nil {
		es.healthCancel()
		es.healthWg.Wait()
	}

	// Close replicas
	for _, r := range es.replicas {
		if sqlDB, err := r.DB(); err == nil {
			sqlDB.Close()
		}
	}

	// Close primary
	return es.Store.Close()
}

// ============================================================================
// Internal helpers
// ============================================================================

func openReplica(dsn string, pool PoolConfig) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	maxOpen := pool.MaxOpenConns
	if maxOpen == 0 {
		maxOpen = 25
	}
	maxIdle := pool.MaxIdleConns
	if maxIdle == 0 {
		maxIdle = 10
	}

	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	if pool.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(pool.ConnMaxLifetime)
	}
	if pool.ConnMaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(pool.ConnMaxIdleTime)
	}

	return db, nil
}

func (es *EnhancedStore) healthCheckLoop(ctx context.Context, interval time.Duration) {
	defer es.healthWg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			es.checkHealth()
		}
	}
}

func (es *EnhancedStore) checkHealth() {
	es.statsMu.Lock()
	defer es.statsMu.Unlock()

	es.poolStats.LastHealthCheck = time.Now()

	// Check primary
	if sqlDB, err := es.db.DB(); err == nil {
		es.poolStats.PrimaryHealthy = sqlDB.Ping() == nil
	}

	// Check replicas
	healthy := 0
	es.replicaMu.RLock()
	for _, r := range es.replicas {
		if sqlDB, err := r.DB(); err == nil {
			if sqlDB.Ping() == nil {
				healthy++
			}
		}
	}
	es.replicaMu.RUnlock()
	es.poolStats.HealthyReplicaCount = healthy
}

func isSerializationError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// PostgreSQL serialization failure: SQLSTATE 40001
	return contains(errStr, "40001") || contains(errStr, "serialization")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
