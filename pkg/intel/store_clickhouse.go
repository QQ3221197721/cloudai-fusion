//go:build ignore

// Package store provides a ClickHouse-backed threat intelligence storage implementation.
package intel

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// ClickHouseStore implements intel.StoreInterface using ClickHouse TSDB.
type ClickHouseStore struct {
	db *sql.DB
}

// New creates a new ClickHouseStore connected to the specified DSN.
func New(dsn string) (*ClickHouseStore, error) {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open clickhouse connection: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	store := &ClickHouseStore{db: db}
	if err := store.ensureTables(); err != nil {
		return nil, fmt.Errorf("failed to ensure tables: %w", err)
	}

	return store, nil
}

// ensureTables creates necessary tables if they don't exist.
func (s *ClickHouseStore) ensureTables() error {
	schemas := []string{
		createCVETableSQL,
		createIOCTableSQL,
		createKnowledgeGraphTableSQL,
	}

	for _, schema := range schemas {
		if _, err := s.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to exec schema %q: %w", schema[:50], err)
		}
	}

	return nil
}

const createCVETableSQL = `
CREATE TABLE IF NOT EXISTS cve_entries (
    cve_id String,
    description String,
    cvss_v3_score Float32,
    cvss_v3_vector String,
    mitre_tags Array(String),
    references Array(String),
    published_at DateTime,
    modified_date DateTime,
    vulnerable_software Array(String),
    index idx_cve_id cve_id TYPE bloom_filter(0.01) GRANULARITY 1,
    index idx_cvss cvss_v3_score TYPE minmax GRANULARITY 1
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(published_at)
ORDER BY (published_at, cve_id)
SETTINGS index_granularity = 8192;
`

const createIOCTableSQL = `
CREATE TABLE IF NOT EXISTS ioc_entries (
    ioc_id UUID DEFAULT generateUUID4(),
    ioc_type String,
    value String,
    threat_actor Nullable(String),
    severity String,
    first_seen_at DateTime,
    last_seen_at DateTime,
    sources Array(String),
    inserted_at DateTime DEFAULT now(),
    index idx_type ioc_type TYPE hash(32) GRANULARITY 1,
    index idx_value value TYPE ngrambf_v1(3, 256, 10000, 0) GRANULARITY 1
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(first_seen_at)
ORDER BY (ioc_type, first_seen_at, value)
SETTINGS index_granularity = 8192;
`

const createKnowledgeGraphTableSQL = `
CREATE TABLE IF NOT EXISTS knowledge_graph (
    type String,
    id String,
    name String,
    description String,
    tactic_ids Array(String),
    sub_techniques Array(String),
    attack_patterns Array(String),
    updated_at DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (type, id);
`

// InsertCVERows inserts multiple CVE entries in bulk.
func (s *ClickHouseStore) InsertCVEEntries(ctx context.Context, entries []intel.CVEEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO cve_entries 
		(cve_id, description, cvss_v3_score, cvss_v3_vector, mitre_tags, references, 
		 published_at, modified_date, vulnerable_software)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare stmt: %w", err)
	}

	defer stmt.Close()

	for _, e := range entries {
		_, err = stmt.ExecContext(ctx,
			e.CVEID, e.Description, e.CVSSv3Score, "", e.MitreTags,
			nil, e.PublishedAt, e.ModifiedDate, e.VulnerableSoftware,
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("exec insert: %w", err)
		}
	}

	return tx.Commit()
}

// GetRecentCVEs returns newly published CVEs within the specified time window.
func (s *ClickHouseStore) GetRecentCVEs(ctx context.Context, since time.Time, limit int) ([]intel.CVEEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cve_id, description, cvss_v3_score, mitre_tags, published_at, modified_date, vulnerable_software
		FROM cve_entries
		WHERE published_at >= ?
		ORDER BY published_at DESC
		LIMIT ?
	`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent cves: %w", err)
	}
	defer rows.Close()

	var result []intel.CVEEntry
	for rows.Next() {
		var cve intel.CVEEntry
		var tags, vulnList []string
		if err := rows.Scan(&cve.CVEID, &cve.Description, &cve.CVSSv3Score, &tags, &cve.PublishedAt, &cve.ModifiedDate, &vulnList); err != nil {
			return nil, err
		}
		cve.MitreTags = tags
		cve.VulnerableSoftware = vulnList
		result = append(result, cve)
	}

	return result, rows.Err()
}

// Close closes the database connection.
func (s *ClickHouseStore) Close() error {
	return s.db.Close()
}
