package intel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// clickhouse.go is the REAL L1 threat-intelligence backend. It talks to a
// ClickHouse server over its native HTTP interface (:8123) using only the Go
// standard library — no third-party driver — so the default CI build needs no
// extra module and go.sum stays untouched.
//
// Real-vs-simulated: NewClickHouseStore PINGS the server and creates the schema;
// if the server is unreachable it returns an error and the caller falls back to
// the in-memory (simulated) store. When connected, Driver()="clickhouse" and
// IsReal()=true, so pkg/capability reports L1 as real and a production boot is
// permitted. All queries use ClickHouse HTTP query parameters ({name:Type} +
// param_name=...) or JSONEachRow bodies, never string-concatenated values, so
// untrusted feed/query input cannot alter query structure.

// ClickHouseConfig configures a ClickHouse HTTP connection.
type ClickHouseConfig struct {
	// Endpoint is the base HTTP URL, e.g. "http://localhost:8123".
	Endpoint string
	// Database is the target database (created if missing).
	Database string
	// User / Password authenticate via X-ClickHouse-User / X-ClickHouse-Key.
	User     string
	Password string
	// Timeout bounds each HTTP call (default 15s).
	Timeout time.Duration
}

// ClickHouseStore implements Store against a real ClickHouse server.
type ClickHouseStore struct {
	cfg   ClickHouseConfig
	httpc *http.Client
}

var _ Store = (*ClickHouseStore)(nil)

// NewClickHouseStore connects to ClickHouse, verifies connectivity, and ensures
// the schema exists. A non-nil error means ClickHouse is not usable (the caller
// should fall back to the simulated MemoryStore and report it honestly).
func NewClickHouseStore(cfg ClickHouseConfig) (*ClickHouseStore, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("intel: clickhouse endpoint is required")
	}
	if cfg.Database == "" {
		cfg.Database = "security"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	s := &ClickHouseStore{cfg: cfg, httpc: &http.Client{Timeout: cfg.Timeout}}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	// With the CI health check using curl http://localhost:8123/ping (ci.yml), the
	// HTTP interface is guaranteed ready when job steps start. We keep a small
	// retry loop here only as an extra safety margin; in normal CI runs it will
	// succeed on the first attempt.
	var pingErr error
	for attempt := 0; attempt < 3; attempt++ {
		if pingErr = s.ping(ctx); pingErr == nil {
			break
		}
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
			return nil, fmt.Errorf("intel: clickhouse ping failed after %d attempt(s): %w", attempt+1, pingErr)
		}
	}
	if err := s.ensureSchema(ctx); err != nil {
		return nil, fmt.Errorf("intel: clickhouse schema init failed: %w", err)
	}
	return s, nil
}

// Driver returns "clickhouse".
func (s *ClickHouseStore) Driver() string { return "clickhouse" }

// IsReal reports true: a connected ClickHouse is a real external backend.
func (s *ClickHouseStore) IsReal() bool { return true }

// ping issues a trivial query against the default database (which always exists)
// to verify connectivity; it does NOT depend on the target database existing yet.
func (s *ClickHouseStore) ping(ctx context.Context) error {
	_, err := s.do(ctx, "SELECT 1", nil, "default")
	return err
}

// ensureSchema creates the database and tables if they do not exist. ClickHouse
// dedups CVEs / knowledge-graph rows by ORDER BY key via ReplacingMergeTree.
// CREATE DATABASE runs against 'default' (which always exists); subsequent DDL
// uses s.cfg.Database once it has been created.
func (s *ClickHouseStore) ensureSchema(ctx context.Context) error {
	// First, create the target database (must run in a real DB context).
	if _, err := s.do(ctx, "CREATE DATABASE IF NOT EXISTS "+s.cfg.Database, nil, "default"); err != nil {
		return err
	}
	// Now the target database exists; all following DDL can use it.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS cve_entries (` +
			`cve_id String, description String, cvss_v3_score Float32, cvss_v3_vector String,` +
			`mitre_tags Array(String), published_at DateTime, modified_date DateTime,` +
			`vulnerable_software Array(String)` +
			`) ENGINE = ReplacingMergeTree ORDER BY cve_id`,
		`CREATE TABLE IF NOT EXISTS ioc_entries (` +
			`ioc_type String, value String, threat_actor String, severity String,` +
			`first_seen_at DateTime, last_seen_at DateTime, sources Array(String)` +
			`) ENGINE = ReplacingMergeTree ORDER BY (ioc_type, value)`,
		`CREATE TABLE IF NOT EXISTS knowledge_graph (` +
			`type String, id String, name String, description String,` +
			`tactic_ids Array(String)` +
			`) ENGINE = ReplacingMergeTree ORDER BY (type, id)`,
	}
	for _, q := range stmts {
		if _, err := s.do(ctx, q, nil, ""); err != nil {
			return err
		}
	}
	return nil
}

// UpsertCVE inserts a CVE row (ReplacingMergeTree dedups by cve_id at merge; reads
// use FINAL for a merged view).
func (s *ClickHouseStore) UpsertCVE(cve CVEEntry) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	row := map[string]any{
		"cve_id": cve.CVEID, "description": cve.Description,
		"cvss_v3_score": cve.CVSSv3Score, "cvss_v3_vector": cve.CVSSv3Vector,
		"mitre_tags": nonNil(cve.MitreTags), "published_at": chTime(cve.PublishedAt),
		"modified_date": chTime(cve.ModifiedDate), "vulnerable_software": nonNil(cve.VulnerableSoftware),
	}
	return s.insertJSONEachRow(ctx, "cve_entries", []map[string]any{row})
}

// UpsertIOCs batch-inserts IOC rows.
func (s *ClickHouseStore) UpsertIOCs(iocs []IOCEntry) error {
	if len(iocs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	rows := make([]map[string]any, 0, len(iocs))
	for _, i := range iocs {
		rows = append(rows, map[string]any{
			"ioc_type": i.IOCType, "value": i.Value, "threat_actor": i.ThreatActor,
			"severity": string(i.Severity), "first_seen_at": chTime(i.FirstSeenAt),
			"last_seen_at": chTime(i.LastSeenAt), "sources": nonNil(i.Sources),
		})
	}
	return s.insertJSONEachRow(ctx, "ioc_entries", rows)
}

// PutKnowledgeGraph inserts techniques and tactics.
func (s *ClickHouseStore) PutKnowledgeGraph(kg KnowledgeGraph) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	rows := make([]map[string]any, 0, len(kg.Techniques)+len(kg.Tactics))
	for _, t := range kg.Techniques {
		rows = append(rows, map[string]any{
			"type": "technique", "id": t.TechniqueID, "name": t.Name,
			"description": t.Description, "tactic_ids": nonNil(t.TacticIDs),
		})
	}
	for _, ta := range kg.Tactics {
		rows = append(rows, map[string]any{
			"type": "tactic", "id": ta.TacticID, "name": ta.Name,
			"description": ta.Description, "tactic_ids": []string{},
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return s.insertJSONEachRow(ctx, "knowledge_graph", rows)
}

// chCVERow is the JSON shape returned by SELECT ... FORMAT JSON for CVEs.
type chCVERow struct {
	CVEID       string   `json:"cve_id"`
	Description string   `json:"description"`
	CVSS        float32  `json:"cvss_v3_score"`
	MitreTags   []string `json:"mitre_tags"`
	PublishedAt string   `json:"published_at"`
}

// RecentCVEs returns CVEs published at/after since, newest first, capped at limit.
func (s *ClickHouseStore) RecentCVEs(since time.Time, limit int) ([]CVEEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	q := "SELECT cve_id, description, cvss_v3_score, mitre_tags, toString(published_at) AS published_at " +
		"FROM cve_entries FINAL WHERE published_at >= {since:DateTime} " +
		"ORDER BY published_at DESC LIMIT {lim:UInt32} FORMAT JSON"
	params := map[string]string{"since": chTime(since), "lim": strconv.Itoa(limit)}
	body, err := s.do(ctx, q, params, "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []chCVERow `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("intel: clickhouse decode cves: %w", err)
	}
	out := make([]CVEEntry, 0, len(resp.Data))
	for _, r := range resp.Data {
		out = append(out, CVEEntry{
			CVEID: r.CVEID, Description: r.Description, CVSSv3Score: r.CVSS,
			MitreTags: r.MitreTags, PublishedAt: parseCHTime(r.PublishedAt),
		})
	}
	return out, nil
}

// chIOCRow is the JSON shape returned by SELECT ... FORMAT JSON for IOCs.
type chIOCRow struct {
	IOCType     string `json:"ioc_type"`
	Value       string `json:"value"`
	ThreatActor string `json:"threat_actor"`
	Severity    string `json:"severity"`
	FirstSeenAt string `json:"first_seen_at"`
}

// LookupIOCs returns IOCs of iocType whose value is in values.
func (s *ClickHouseStore) LookupIOCs(iocType string, values []string) ([]IOCEntry, error) {
	if len(values) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	q := "SELECT ioc_type, value, threat_actor, severity, toString(first_seen_at) AS first_seen_at " +
		"FROM ioc_entries FINAL WHERE ioc_type = {t:String} AND value IN {vals:Array(String)} FORMAT JSON"
	params := map[string]string{"t": iocType, "vals": chArrayParam(values)}
	body, err := s.do(ctx, q, params, "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []chIOCRow `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("intel: clickhouse decode iocs: %w", err)
	}
	out := make([]IOCEntry, 0, len(resp.Data))
	for _, r := range resp.Data {
		out = append(out, IOCEntry{
			IOCType: r.IOCType, Value: r.Value, ThreatActor: r.ThreatActor,
			Severity: Severity(r.Severity), FirstSeenAt: parseCHTime(r.FirstSeenAt),
		})
	}
	return out, nil
}

// TechniqueByID returns a MITRE technique by ID.
func (s *ClickHouseStore) TechniqueByID(id string) (Technique, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	q := "SELECT id, name, description, tactic_ids FROM knowledge_graph FINAL " +
		"WHERE type = 'technique' AND id = {id:String} LIMIT 1 FORMAT JSON"
	body, err := s.do(ctx, q, map[string]string{"id": id}, "")
	if err != nil {
		return Technique{}, false
	}
	var resp struct {
		Data []struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			TacticIDs   []string `json:"tactic_ids"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Data) == 0 {
		return Technique{}, false
	}
	r := resp.Data[0]
	return Technique{TechniqueID: r.ID, Name: r.Name, Description: r.Description, TacticIDs: r.TacticIDs}, true
}

// insertJSONEachRow POSTs rows using INSERT ... FORMAT JSONEachRow. The table name
// is a package constant (never user input); values are JSON-encoded, so this is
// injection-safe.
func (s *ClickHouseStore) insertJSONEachRow(ctx context.Context, table string, rows []map[string]any) error {
	var buf bytes.Buffer
	buf.WriteString("INSERT INTO " + table + " FORMAT JSONEachRow\n")
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("intel: clickhouse encode row: %w", err)
		}
	}
	_, err := s.do(ctx, buf.String(), nil, "")
	return err
}

// do performs one ClickHouse HTTP call. dbOverride, when non-empty, overrides
// the configured database (needed for bootstrap calls that run before the target
// database exists). The SQL is sent in the POST body; query parameters are passed
// as param_<name>; auth uses ClickHouse headers.
func (s *ClickHouseStore) do(ctx context.Context, sql string, params map[string]string, dbOverride string) ([]byte, error) {
	u, err := url.Parse(s.cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("intel: clickhouse endpoint: %w", err)
	}
	q := u.Query()
	db := s.cfg.Database
	if dbOverride != "" {
		db = dbOverride
	}
	q.Set("database", db)
	for k, v := range params {
		q.Set("param_"+k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(sql))
	if err != nil {
		return nil, err
	}
	if s.cfg.User != "" {
		req.Header.Set("X-ClickHouse-User", s.cfg.User)
	}
	if s.cfg.Password != "" {
		req.Header.Set("X-ClickHouse-Key", s.cfg.Password)
	}
	resp, err := s.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("intel: clickhouse status %d: %s", resp.StatusCode, bounded(string(body), 300))
	}
	return body, nil
}

// --- helpers ---------------------------------------------------------------

// chTime formats a time for ClickHouse DateTime, clamping zero/pre-epoch values
// to the Unix epoch (ClickHouse DateTime cannot represent times before 1970).
func chTime(t time.Time) string {
	if t.IsZero() || t.Year() < 1970 {
		t = time.Unix(0, 0).UTC()
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

// parseCHTime parses a ClickHouse DateTime string back into time.Time (UTC).
func parseCHTime(s string) time.Time {
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// chArrayParam renders a string slice as a ClickHouse Array(String) parameter
// literal, e.g. ['a','b'] with single quotes escaped.
func chArrayParam(values []string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		esc := strings.ReplaceAll(v, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `'`, `\'`)
		parts = append(parts, "'"+esc+"'")
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// nonNil returns a non-nil slice so JSONEachRow emits [] rather than null for
// ClickHouse Array(String) columns.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// bounded truncates s to at most n bytes for error messages.
func bounded(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
