package intel

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SQLStore is a driver-agnostic Store backed by a database/sql handle. It is the
// REAL threat-intelligence backend: point it at a production TSDB (ClickHouse is
// the reference target; see docker-compose.intel.yml + scripts/init-db-clickhouse.sql)
// by opening the driver at the composition root and injecting the *sql.DB.
//
// Keeping the dependency on the concrete driver at the call site (rather than
// importing it here) means this package — and therefore the default CI build —
// needs no extra module: the stdlib database/sql interface is the only contract.
// Every query uses placeholders (never string-concatenated values) so untrusted
// feed/query input can never alter query structure.
type SQLStore struct {
	db     *sql.DB
	driver string // e.g. "clickhouse" — reported to pkg/capability
}

// compile-time interface check.
var _ Store = (*SQLStore)(nil)

// NewSQLStore wraps an already-opened *sql.DB. driver names the backend for
// honest capability reporting (e.g. "clickhouse"). A nil db is rejected so a
// misconfigured real backend fails fast rather than masquerading as healthy.
func NewSQLStore(db *sql.DB, driver string) (*SQLStore, error) {
	if db == nil {
		return nil, fmt.Errorf("intel: SQLStore requires a non-nil *sql.DB")
	}
	if driver == "" {
		driver = "sql"
	}
	return &SQLStore{db: db, driver: driver}, nil
}

// Driver returns the configured backend driver name.
func (s *SQLStore) Driver() string { return s.driver }

// IsReal reports true: a SQLStore is a real external backend.
func (s *SQLStore) IsReal() bool { return true }

// UpsertCVE inserts a CVE using placeholders. ClickHouse MergeTree dedups on the
// ORDER BY key at merge time; callers relying on strict uniqueness should use a
// ReplacingMergeTree table (see DDL).
func (s *SQLStore) UpsertCVE(cve CVEEntry) error {
	const q = `INSERT INTO cve_entries
		(cve_id, description, cvss_v3_score, cvss_v3_vector, mitre_tags, published_at, modified_date, vulnerable_software)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(q,
		cve.CVEID, cve.Description, cve.CVSSv3Score, cve.CVSSv3Vector,
		strings.Join(cve.MitreTags, ","), cve.PublishedAt, cve.ModifiedDate,
		strings.Join(cve.VulnerableSoftware, ","),
	)
	if err != nil {
		return fmt.Errorf("intel: sql upsert cve %s: %w", cve.CVEID, err)
	}
	return nil
}

// UpsertIOCs batch-inserts IOCs within a single transaction.
func (s *SQLStore) UpsertIOCs(iocs []IOCEntry) error {
	if len(iocs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("intel: sql begin: %w", err)
	}
	const q = `INSERT INTO ioc_entries (ioc_type, value, threat_actor, severity, first_seen_at, last_seen_at, sources)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	stmt, err := tx.Prepare(q)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("intel: sql prepare ioc: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, i := range iocs {
		if _, err := stmt.Exec(i.IOCType, i.Value, i.ThreatActor, string(i.Severity),
			i.FirstSeenAt, i.LastSeenAt, strings.Join(i.Sources, ",")); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("intel: sql exec ioc %s/%s: %w", i.IOCType, i.Value, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("intel: sql commit iocs: %w", err)
	}
	return nil
}

// PutKnowledgeGraph replaces techniques/tactics via a ReplacingMergeTree table.
func (s *SQLStore) PutKnowledgeGraph(kg KnowledgeGraph) error {
	const q = `INSERT INTO knowledge_graph (type, id, name, description, tactic_ids) VALUES (?, ?, ?, ?, ?)`
	for _, t := range kg.Techniques {
		if _, err := s.db.Exec(q, "technique", t.TechniqueID, t.Name, t.Description, strings.Join(t.TacticIDs, ",")); err != nil {
			return fmt.Errorf("intel: sql put technique %s: %w", t.TechniqueID, err)
		}
	}
	for _, ta := range kg.Tactics {
		if _, err := s.db.Exec(q, "tactic", ta.TacticID, ta.Name, ta.Description, ""); err != nil {
			return fmt.Errorf("intel: sql put tactic %s: %w", ta.TacticID, err)
		}
	}
	return nil
}

// RecentCVEs queries newest-first CVEs at/after since, capped at limit. The
// bound is a placeholder; the fixed ORDER BY column is a constant identifier.
func (s *SQLStore) RecentCVEs(since time.Time, limit int) ([]CVEEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `SELECT cve_id, description, cvss_v3_score, mitre_tags, published_at
		FROM cve_entries WHERE published_at >= ? ORDER BY published_at DESC LIMIT ?`
	rows, err := s.db.Query(q, since, limit)
	if err != nil {
		return nil, fmt.Errorf("intel: sql recent cves: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []CVEEntry
	for rows.Next() {
		var c CVEEntry
		var tags string
		if err := rows.Scan(&c.CVEID, &c.Description, &c.CVSSv3Score, &tags, &c.PublishedAt); err != nil {
			return nil, fmt.Errorf("intel: sql scan cve: %w", err)
		}
		if tags != "" {
			c.MitreTags = strings.Split(tags, ",")
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LookupIOCs returns IOCs of iocType whose value is in values, using an IN clause
// built entirely from placeholders (one "?" per value — never the values inline).
func (s *SQLStore) LookupIOCs(iocType string, values []string) ([]IOCEntry, error) {
	if len(values) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")
	// #nosec G201 -- placeholders is a fixed count of "?" separators, not data.
	q := "SELECT ioc_type, value, threat_actor, severity, first_seen_at FROM ioc_entries WHERE ioc_type = ? AND value IN (" + placeholders + ")"
	args := make([]any, 0, len(values)+1)
	args = append(args, iocType)
	for _, v := range values {
		args = append(args, v)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("intel: sql lookup iocs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []IOCEntry
	for rows.Next() {
		var e IOCEntry
		var sev string
		if err := rows.Scan(&e.IOCType, &e.Value, &e.ThreatActor, &sev, &e.FirstSeenAt); err != nil {
			return nil, fmt.Errorf("intel: sql scan ioc: %w", err)
		}
		e.Severity = Severity(sev)
		out = append(out, e)
	}
	return out, rows.Err()
}

// TechniqueByID looks up a single MITRE technique by ID.
func (s *SQLStore) TechniqueByID(id string) (Technique, bool) {
	const q = `SELECT id, name, description, tactic_ids FROM knowledge_graph WHERE type = 'technique' AND id = ? LIMIT 1`
	var t Technique
	var tactics string
	err := s.db.QueryRow(q, id).Scan(&t.TechniqueID, &t.Name, &t.Description, &tactics)
	if err != nil {
		return Technique{}, false
	}
	if tactics != "" {
		t.TacticIDs = strings.Split(tactics, ",")
	}
	return t, true
}
