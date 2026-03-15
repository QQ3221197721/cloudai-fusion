// Package migrate provides a SQL-based database migration system for CloudAI Fusion.
// It tracks applied migrations in a schema_migrations table and executes pending
// migrations in version order. Compatible with CI/CD pipelines and supports
// both up and down migrations.
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Direction indicates the migration direction.
type Direction int

const (
	// Up applies the migration.
	Up Direction = iota
	// Down rolls back the migration.
	Down
)

// Migration represents a single database migration step.
type Migration struct {
	Version     string // Unique version identifier, e.g. "0001", "20260301120000"
	Description string // Human-readable description
	UpSQL       string // SQL to apply migration
	DownSQL     string // SQL to rollback migration
}

// AppliedMigration is a record of a migration that has been applied.
type AppliedMigration struct {
	Version   string    `json:"version"`
	Checksum  string    `json:"checksum"`
	AppliedAt time.Time `json:"applied_at"`
}

// Migrator manages database schema migrations.
type Migrator struct {
	db         *sql.DB
	migrations []Migration
	tableName  string
	logger     *logrus.Logger
	mu         sync.Mutex
}

// Config holds migrator configuration.
type Config struct {
	DB         *sql.DB
	TableName  string // defaults to "schema_migrations"
	Logger     *logrus.Logger
}

// New creates a new Migrator with the given config and migrations.
func New(cfg Config, migrations []Migration) *Migrator {
	tableName := cfg.TableName
	if tableName == "" {
		tableName = "schema_migrations"
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	// Sort migrations by version
	sorted := make([]Migration, len(migrations))
	copy(sorted, migrations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Version < sorted[j].Version
	})

	return &Migrator{
		db:         cfg.DB,
		migrations: sorted,
		tableName:  tableName,
		logger:     logger,
	}
}

// Up applies all pending migrations in version order.
func (m *Migrator) Up(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureTable(ctx); err != nil {
		return 0, fmt.Errorf("failed to create migrations table: %w", err)
	}

	applied, err := m.getApplied(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	appliedMap := make(map[string]bool, len(applied))
	for _, a := range applied {
		appliedMap[a.Version] = true
	}

	count := 0
	for _, mig := range m.migrations {
		if appliedMap[mig.Version] {
			continue
		}

		if err := ctx.Err(); err != nil {
			return count, fmt.Errorf("context cancelled during migration: %w", err)
		}

		m.logger.WithFields(logrus.Fields{
			"version":     mig.Version,
			"description": mig.Description,
		}).Info("Applying migration")

		if err := m.applyMigration(ctx, mig, Up); err != nil {
			return count, fmt.Errorf("migration %s failed: %w", mig.Version, err)
		}
		count++
	}

	m.logger.WithField("applied", count).Info("Migration complete")
	return count, nil
}

// Down rolls back the last N migrations. If n <= 0, rolls back all.
func (m *Migrator) Down(ctx context.Context, n int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureTable(ctx); err != nil {
		return 0, fmt.Errorf("failed to create migrations table: %w", err)
	}

	applied, err := m.getApplied(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Reverse order for rollback
	sort.Slice(applied, func(i, j int) bool {
		return applied[i].Version > applied[j].Version
	})

	migMap := make(map[string]Migration, len(m.migrations))
	for _, mig := range m.migrations {
		migMap[mig.Version] = mig
	}

	if n <= 0 {
		n = len(applied)
	}
	if n > len(applied) {
		n = len(applied)
	}

	count := 0
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return count, fmt.Errorf("context cancelled during rollback: %w", err)
		}

		ver := applied[i].Version
		mig, ok := migMap[ver]
		if !ok {
			m.logger.WithField("version", ver).Warn("Migration definition not found, skipping rollback")
			continue
		}

		if mig.DownSQL == "" {
			m.logger.WithField("version", ver).Warn("No down SQL defined, skipping")
			continue
		}

		m.logger.WithFields(logrus.Fields{
			"version":     ver,
			"description": mig.Description,
		}).Info("Rolling back migration")

		if err := m.applyMigration(ctx, mig, Down); err != nil {
			return count, fmt.Errorf("rollback %s failed: %w", ver, err)
		}
		count++
	}

	m.logger.WithField("rolled_back", count).Info("Rollback complete")
	return count, nil
}

// Status returns the list of applied migrations and pending migrations.
func (m *Migrator) Status(ctx context.Context) (applied []AppliedMigration, pending []Migration, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureTable(ctx); err != nil {
		return nil, nil, err
	}

	applied, err = m.getApplied(ctx)
	if err != nil {
		return nil, nil, err
	}

	appliedMap := make(map[string]bool, len(applied))
	for _, a := range applied {
		appliedMap[a.Version] = true
	}

	for _, mig := range m.migrations {
		if !appliedMap[mig.Version] {
			pending = append(pending, mig)
		}
	}
	return applied, pending, nil
}

// ensureTable creates the migrations tracking table if it doesn't exist.
func (m *Migrator) ensureTable(ctx context.Context) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		version     VARCHAR(64) PRIMARY KEY,
		checksum    VARCHAR(64) NOT NULL,
		description TEXT,
		applied_at  TIMESTAMP NOT NULL DEFAULT NOW()
	)`, m.tableName)
	_, err := m.db.ExecContext(ctx, query)
	return err
}

// getApplied returns all applied migrations from the database.
func (m *Migrator) getApplied(ctx context.Context) ([]AppliedMigration, error) {
	query := fmt.Sprintf("SELECT version, checksum, applied_at FROM %s ORDER BY version ASC", m.tableName)
	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AppliedMigration
	for rows.Next() {
		var a AppliedMigration
		if err := rows.Scan(&a.Version, &a.Checksum, &a.AppliedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// applyMigration executes a migration within a transaction and records/removes the version.
func (m *Migrator) applyMigration(ctx context.Context, mig Migration, dir Direction) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	sqlText := mig.UpSQL
	if dir == Down {
		sqlText = mig.DownSQL
	}

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}

	if dir == Up {
		checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(mig.UpSQL)))
		insertQuery := fmt.Sprintf(
			"INSERT INTO %s (version, checksum, description, applied_at) VALUES ($1, $2, $3, $4)",
			m.tableName,
		)
		if _, err := tx.ExecContext(ctx, insertQuery, mig.Version, checksum, mig.Description, time.Now().UTC()); err != nil {
			return fmt.Errorf("record migration: %w", err)
		}
	} else {
		deleteQuery := fmt.Sprintf("DELETE FROM %s WHERE version = $1", m.tableName)
		if _, err := tx.ExecContext(ctx, deleteQuery, mig.Version); err != nil {
			return fmt.Errorf("remove migration record: %w", err)
		}
	}

	return tx.Commit()
}

// Checksum computes SHA-256 of a migration's up SQL.
func Checksum(sqlText string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(sqlText)))
}
