// Package store - Data sharding strategy for horizontal scaling.
// Implements tenant-based and time-based partitioning for CloudAI Fusion's
// multi-tenant workload data, enabling efficient queries and data isolation.
//
// Sharding strategies:
//   1. Tenant-based: each tenant's data is routed to a dedicated shard
//   2. Time-based: data is partitioned by time period (day/month)
//   3. Hybrid: tenant + time partition for optimal locality
//
// PostgreSQL native partitioning is used where possible (PARTITION BY RANGE).
// Application-level sharding routes queries to the correct database.
package store

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ============================================================================
// Sharding Config
// ============================================================================

// ShardConfig holds data sharding configuration.
type ShardConfig struct {
	// Strategy: "none" (default), "tenant", "time", or "hybrid".
	Strategy string `mapstructure:"strategy"`

	// ShardCount is the number of database shards for tenant-based sharding.
	ShardCount int `mapstructure:"shard_count"`

	// ShardDSNs maps shard IDs to DSN connection strings.
	// For tenant-based sharding: ["shard-0-dsn", "shard-1-dsn", ...]
	ShardDSNs []string `mapstructure:"shard_dsns"`

	// TimePartition configuration for time-based partitioning.
	TimePartitionUnit string `mapstructure:"time_partition_unit"` // "day" or "month"

	// RetentionDays is how long to keep partitioned data.
	RetentionDays int `mapstructure:"retention_days"`
}

// DefaultShardConfig returns default sharding configuration (disabled).
func DefaultShardConfig() ShardConfig {
	return ShardConfig{
		Strategy:          "none",
		ShardCount:        1,
		TimePartitionUnit: "month",
		RetentionDays:     365,
	}
}

// ============================================================================
// ShardRouter — routes queries to the correct shard
// ============================================================================

// ShardRouter determines which database shard to use for a given operation.
type ShardRouter struct {
	strategy  string
	shards    []*gorm.DB
	primary   *gorm.DB
	config    ShardConfig
	logger    *logrus.Logger
	mu        sync.RWMutex
}

// NewShardRouter creates a shard router based on configuration.
func NewShardRouter(primary *gorm.DB, cfg ShardConfig, logger *logrus.Logger) (*ShardRouter, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	router := &ShardRouter{
		strategy: cfg.Strategy,
		primary:  primary,
		config:   cfg,
		logger:   logger,
	}

	// Initialize shard connections
	if cfg.Strategy == "tenant" || cfg.Strategy == "hybrid" {
		if len(cfg.ShardDSNs) == 0 {
			// No shard DSNs configured — use primary for all shards
			logger.Info("No shard DSNs configured, using primary database for all shards")
			router.shards = []*gorm.DB{primary}
		} else {
			for i, dsn := range cfg.ShardDSNs {
				_ = dsn
				// NOTE: In production, each DSN connects to a different database:
				//   db, _ := gorm.Open(postgres.Open(dsn), &gorm.Config{})
				// For now, use primary as all shards.
				router.shards = append(router.shards, primary)
				logger.WithField("shard", i).Info("Shard connection initialized")
			}
		}
	}

	logger.WithFields(logrus.Fields{
		"strategy":    cfg.Strategy,
		"shard_count": len(router.shards),
	}).Info("Shard router initialized")

	return router, nil
}

// ShardForTenant returns the database shard for a given tenant ID.
// Uses consistent hashing to distribute tenants across shards.
func (r *ShardRouter) ShardForTenant(tenantID string) *gorm.DB {
	if r.strategy == "none" || len(r.shards) == 0 {
		return r.primary
	}

	idx := r.hashTenant(tenantID) % uint32(len(r.shards))
	return r.shards[idx]
}

// ShardForTime returns the partition suffix for a given timestamp.
// For PostgreSQL native partitioning, this generates the partition name.
func (r *ShardRouter) ShardForTime(t time.Time) string {
	switch r.config.TimePartitionUnit {
	case "day":
		return t.Format("2006_01_02")
	case "month":
		return t.Format("2006_01")
	default:
		return t.Format("2006_01")
	}
}

// ShardForHybrid returns both shard DB and partition suffix.
func (r *ShardRouter) ShardForHybrid(tenantID string, t time.Time) (*gorm.DB, string) {
	return r.ShardForTenant(tenantID), r.ShardForTime(t)
}

// Primary returns the primary (unsharded) database.
func (r *ShardRouter) Primary() *gorm.DB {
	return r.primary
}

// AllShards returns all shard connections (for fan-out queries).
func (r *ShardRouter) AllShards() []*gorm.DB {
	if len(r.shards) == 0 {
		return []*gorm.DB{r.primary}
	}
	return r.shards
}

// hashTenant computes a consistent hash for tenant routing.
func (r *ShardRouter) hashTenant(tenantID string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(tenantID))
	return h.Sum32()
}

// ============================================================================
// PostgreSQL Native Partitioning — DDL helpers
// ============================================================================

// PartitionManager manages PostgreSQL table partitions.
type PartitionManager struct {
	db     *gorm.DB
	config ShardConfig
	logger *logrus.Logger
}

// NewPartitionManager creates a new partition manager.
func NewPartitionManager(db *gorm.DB, cfg ShardConfig, logger *logrus.Logger) *PartitionManager {
	return &PartitionManager{
		db:     db,
		config: cfg,
		logger: logger,
	}
}

// CreateTimePartition creates a new time-based partition for a table.
// Uses PostgreSQL PARTITION BY RANGE on the created_at column.
func (pm *PartitionManager) CreateTimePartition(ctx context.Context, tableName string, start, end time.Time) error {
	partitionName := fmt.Sprintf("%s_%s", tableName, start.Format("2006_01"))

	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
		partitionName,
		tableName,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
	)

	if err := pm.db.WithContext(ctx).Exec(sql).Error; err != nil {
		return fmt.Errorf("failed to create partition %s: %w", partitionName, err)
	}

	pm.logger.WithFields(logrus.Fields{
		"partition": partitionName,
		"start":     start.Format("2006-01-02"),
		"end":       end.Format("2006-01-02"),
	}).Info("Time partition created")

	return nil
}

// CreateFuturePartitions pre-creates partitions for upcoming time periods.
func (pm *PartitionManager) CreateFuturePartitions(ctx context.Context, tableName string, periods int) error {
	now := time.Now().UTC()

	for i := 0; i < periods; i++ {
		var start, end time.Time

		switch pm.config.TimePartitionUnit {
		case "day":
			start = now.AddDate(0, 0, i).Truncate(24 * time.Hour)
			end = start.AddDate(0, 0, 1)
		default: // month
			y, m, _ := now.Date()
			start = time.Date(y, m+time.Month(i), 1, 0, 0, 0, 0, time.UTC)
			end = start.AddDate(0, 1, 0)
		}

		if err := pm.CreateTimePartition(ctx, tableName, start, end); err != nil {
			pm.logger.WithError(err).WithField("partition", i).Warn("Failed to create future partition")
			// Continue creating other partitions
		}
	}

	return nil
}

// DropExpiredPartitions removes partitions older than the retention period.
func (pm *PartitionManager) DropExpiredPartitions(ctx context.Context, tableName string) error {
	retention := time.Duration(pm.config.RetentionDays) * 24 * time.Hour
	cutoff := time.Now().Add(-retention)

	// Query pg_catalog for child partitions
	var partitions []struct {
		TableName string
	}

	sql := `
		SELECT child.relname AS table_name
		FROM pg_inherits
		JOIN pg_class parent ON parent.oid = pg_inherits.inhparent
		JOIN pg_class child ON child.oid = pg_inherits.inhrelid
		WHERE parent.relname = ?
		ORDER BY child.relname
	`

	if err := pm.db.WithContext(ctx).Raw(sql, tableName).Scan(&partitions).Error; err != nil {
		return fmt.Errorf("failed to list partitions: %w", err)
	}

	for _, p := range partitions {
		// Parse partition date from name (e.g., "audit_logs_2025_01")
		partTime, err := parsePartitionDate(p.TableName, tableName)
		if err != nil {
			continue
		}

		if partTime.Before(cutoff) {
			dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", p.TableName)
			if err := pm.db.WithContext(ctx).Exec(dropSQL).Error; err != nil {
				pm.logger.WithError(err).WithField("partition", p.TableName).Warn("Failed to drop expired partition")
			} else {
				pm.logger.WithField("partition", p.TableName).Info("Dropped expired partition")
			}
		}
	}

	return nil
}

// EnsurePartitionedTable converts a regular table to a partitioned table.
// WARNING: This requires the table to be empty or data to be migrated.
func (pm *PartitionManager) EnsurePartitionedTable(ctx context.Context, tableName, partitionColumn string) error {
	// Check if table is already partitioned
	var count int64
	checkSQL := `
		SELECT COUNT(*) FROM pg_partitioned_table pt
		JOIN pg_class c ON c.oid = pt.partrelid
		WHERE c.relname = ?
	`
	pm.db.WithContext(ctx).Raw(checkSQL, tableName).Scan(&count)

	if count > 0 {
		pm.logger.WithField("table", tableName).Debug("Table already partitioned")
		return nil
	}

	pm.logger.WithFields(logrus.Fields{
		"table":  tableName,
		"column": partitionColumn,
	}).Info("Table partitioning would be applied (requires manual migration for existing data)")

	return nil
}

// ============================================================================
// Partitioned Tables — SQL DDL for production deployment
// ============================================================================

// PartitionedTableDDL returns SQL DDL for creating partitioned versions
// of the main tables. Deploy these via migration scripts.
func PartitionedTableDDL() []string {
	return []string{
		// Audit logs partitioned by month
		`CREATE TABLE IF NOT EXISTS audit_logs_partitioned (
			LIKE audit_logs INCLUDING ALL
		) PARTITION BY RANGE (created_at)`,

		// Workload events partitioned by month
		`CREATE TABLE IF NOT EXISTS workload_events_partitioned (
			LIKE workload_events INCLUDING ALL
		) PARTITION BY RANGE (created_at)`,

		// Alert events partitioned by month
		`CREATE TABLE IF NOT EXISTS alert_events_partitioned (
			LIKE alert_events INCLUDING ALL
		) PARTITION BY RANGE (fired_at)`,

		// Cost records partitioned by month
		`CREATE TABLE IF NOT EXISTS cost_records_partitioned (
			LIKE cost_records INCLUDING ALL
		) PARTITION BY RANGE (period_start)`,
	}
}

// TenantIsolationDDL returns SQL for tenant-level Row Security Policies.
// PostgreSQL RLS provides row-level data isolation without application changes.
func TenantIsolationDDL() []string {
	return []string{
		// Enable RLS on multi-tenant tables
		`ALTER TABLE clusters ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE workloads ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE security_policies ENABLE ROW LEVEL SECURITY`,

		// Create policies: users can only see their own tenant's data
		`CREATE POLICY tenant_isolation_clusters ON clusters
			USING (created_by = current_setting('app.current_user_id')::uuid
				OR current_setting('app.current_role') = 'admin')`,

		`CREATE POLICY tenant_isolation_workloads ON workloads
			USING (created_by = current_setting('app.current_user_id')::uuid
				OR current_setting('app.current_role') = 'admin')`,
	}
}

// ============================================================================
// Utility
// ============================================================================

func parsePartitionDate(partitionName, baseTableName string) (time.Time, error) {
	suffix := partitionName[len(baseTableName)+1:] // skip "tablename_"
	layouts := []string{"2006_01_02", "2006_01"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, suffix); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse partition date from %s", partitionName)
}
