// Package intel implements the Threat Intelligence Well (L1) of CloudAI Fusion's
// AISecOps platform.
//
// It provides offline-first threat-intelligence ingestion, storage, and query:
//   - CVE/NVD feeds (JSONL)
//   - IOC (Indicators of Compromise) feeds (TSV)
//   - MITRE ATT&CK knowledge graph (TTP mapping)
//
// Design principles (consistent with the platform's run-mode honesty framework):
//   - Offline-first: works with no internet (USB / mirror-server sync).
//   - Pluggable storage: a Store interface backs the Hub; the default MemoryStore
//     is honestly reported as a SIMULATED backend via pkg/capability, while a real
//     TSDB (e.g. ClickHouse) backend registers as REAL. Production forbids the
//     simulated fallback via capability.Enforce().
//   - Verifiable: every sync action records a signed receipt into pkg/evidence.
//
// Cross-deep-well integration:
//
//	L1 ⇒ L2  (Threat Hunting): a new CVE triggers hunting queries.
//	L1 ⇒ L3-L8 (Operations):   IOC updates feed endpoint / network rules.
//	L1 ⇐ L13 (Evidence Ledger): each ingest is cryptographically logged.
package intel

import "time"

// Severity classifies the criticality of an intelligence item.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// CVEEntry is a normalized CVE record from NVD or an equivalent feed.
type CVEEntry struct {
	CVEID              string    `json:"cve_id"` // e.g. "CVE-2024-1234"
	Description        string    `json:"description"`
	CVSSv3Score        float32   `json:"cvss_v3_score"`
	CVSSv3Vector       string    `json:"cvss_v3_vector,omitempty"`
	MitreTags          []string  `json:"mitre_tags,omitempty"` // e.g. ["T1190"]
	References         []string  `json:"references,omitempty"`
	PublishedAt        time.Time `json:"published_at"`
	ModifiedDate       time.Time `json:"modified_date,omitempty"`
	VulnerableSoftware []string  `json:"vulnerable_software,omitempty"`
}

// IsCritical reports whether the CVE meets the critical CVSS threshold (>= 9.0).
func (c CVEEntry) IsCritical() bool { return c.CVSSv3Score >= 9.0 }

// IOCEntry is an Indicator of Compromise (IP, domain, hash, URL, ...).
type IOCEntry struct {
	IOCType     string    `json:"ioc_type"` // "ip" | "domain" | "sha256" | "md5" | "url"
	Value       string    `json:"value"`
	ThreatActor string    `json:"threat_actor,omitempty"`
	Severity    Severity  `json:"severity"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at,omitempty"`
	Sources     []string  `json:"sources,omitempty"`
}

// KnowledgeGraph is a subset of the MITRE ATT&CK model used for TTP mapping.
type KnowledgeGraph struct {
	Tactics    []Tactic    `json:"tactics"`
	Techniques []Technique `json:"techniques"`
}

// Tactic is a MITRE ATT&CK tactic (the "why" of an attack step).
type Tactic struct {
	TacticID    string `json:"tactic_id"` // e.g. "TA0001"
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Technique is a MITRE ATT&CK technique (the "how"), mapped to one or more tactics.
type Technique struct {
	TechniqueID string   `json:"technique_id"` // e.g. "T1190"
	Name        string   `json:"name"`
	TacticIDs   []string `json:"tactic_ids,omitempty"`
	Description string   `json:"description,omitempty"`
}

// FeedSource describes one threat-intelligence feed and where to load it from.
// LocalPath is tried first (offline); URL is the online fallback.
type FeedSource struct {
	Name       string    `json:"name"`
	URL        string    `json:"url,omitempty"`
	LocalPath  string    `json:"local_path,omitempty"`
	PubKeyPath string    `json:"pubkey_path,omitempty"`
	LastSyncAt time.Time `json:"last_sync_at,omitempty"`
	Status     string    `json:"status,omitempty"` // "active" | "stale" | "error"
}

// SyncResult accumulates the outcome of a synchronization run.
type SyncResult struct {
	CVEAdded int      `json:"cve_added"`
	IOCAdded int      `json:"ioc_added"`
	Errors   []string `json:"errors,omitempty"`
}

// AddCVE increments the CVE-added counter.
func (r *SyncResult) AddCVE() { r.CVEAdded++ }

// AddIOC increments the IOC-added counter.
func (r *SyncResult) AddIOC() { r.IOCAdded++ }

// RecordError appends an error message.
func (r *SyncResult) RecordError(msg string) { r.Errors = append(r.Errors, msg) }

// HasErrors reports whether any error was recorded.
func (r *SyncResult) HasErrors() bool { return len(r.Errors) > 0 }
