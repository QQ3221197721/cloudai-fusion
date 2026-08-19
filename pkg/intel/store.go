package intel

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is the pluggable persistence backend for threat intelligence.
//
// The interface is deliberately small and backend-agnostic so the Hub can run on
// a simulated in-memory store (default, honestly reported) or a real TSDB such as
// ClickHouse (registered as a real backend) without changing call sites — mirroring
// the platform's real-vs-simulated capability model.
type Store interface {
	// Driver returns the backend driver name (e.g. "memory", "clickhouse").
	Driver() string
	// IsReal reports whether this store is backed by a real external dependency.
	IsReal() bool

	// UpsertCVE inserts or updates a single CVE by CVEID.
	UpsertCVE(cve CVEEntry) error
	// UpsertIOCs inserts or updates a batch of IOCs keyed by (type,value).
	UpsertIOCs(iocs []IOCEntry) error
	// PutKnowledgeGraph replaces the stored MITRE ATT&CK knowledge graph.
	PutKnowledgeGraph(kg KnowledgeGraph) error

	// RecentCVEs returns CVEs published at/after since, newest first, capped at limit.
	RecentCVEs(since time.Time, limit int) ([]CVEEntry, error)
	// LookupIOCs returns stored IOCs of iocType whose value is in values.
	LookupIOCs(iocType string, values []string) ([]IOCEntry, error)
	// TechniqueByID returns a MITRE technique by ID and whether it was found.
	TechniqueByID(id string) (Technique, bool)
}

// MemoryStore is an in-memory Store used as the honest simulated default.
// It is concurrency-safe and has no external dependency, so it always compiles
// and runs in CI. It is reported to pkg/capability as a simulated backend.
type MemoryStore struct {
	mu         sync.RWMutex
	cves       map[string]CVEEntry
	iocs       map[string]IOCEntry // key: type + "\x00" + value
	techniques map[string]Technique
	tactics    map[string]Tactic
}

// NewMemoryStore builds an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		cves:       make(map[string]CVEEntry),
		iocs:       make(map[string]IOCEntry),
		techniques: make(map[string]Technique),
		tactics:    make(map[string]Tactic),
	}
}

// Driver returns "memory".
func (s *MemoryStore) Driver() string { return "memory" }

// IsReal reports false: the in-memory store is a simulation.
func (s *MemoryStore) IsReal() bool { return false }

func iocKey(iocType, value string) string { return iocType + "\x00" + value }

// UpsertCVE stores a CVE keyed by its ID.
func (s *MemoryStore) UpsertCVE(cve CVEEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cves[cve.CVEID] = cve
	return nil
}

// UpsertIOCs stores a batch of IOCs keyed by (type,value).
func (s *MemoryStore) UpsertIOCs(iocs []IOCEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, i := range iocs {
		s.iocs[iocKey(i.IOCType, i.Value)] = i
	}
	return nil
}

// PutKnowledgeGraph replaces the stored techniques and tactics.
func (s *MemoryStore) PutKnowledgeGraph(kg KnowledgeGraph) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range kg.Techniques {
		s.techniques[t.TechniqueID] = t
	}
	for _, ta := range kg.Tactics {
		s.tactics[ta.TacticID] = ta
	}
	return nil
}

// RecentCVEs returns CVEs published at/after since, newest first, capped at limit.
func (s *MemoryStore) RecentCVEs(since time.Time, limit int) ([]CVEEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CVEEntry, 0, len(s.cves))
	for _, c := range s.cves {
		if !c.PublishedAt.Before(since) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PublishedAt.After(out[j].PublishedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// LookupIOCs returns stored IOCs of iocType whose value matches one of values.
func (s *MemoryStore) LookupIOCs(iocType string, values []string) ([]IOCEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IOCEntry, 0, len(values))
	for _, v := range values {
		if i, ok := s.iocs[iocKey(iocType, strings.TrimSpace(v))]; ok {
			out = append(out, i)
		}
	}
	return out, nil
}

// TechniqueByID returns a MITRE technique by ID.
func (s *MemoryStore) TechniqueByID(id string) (Technique, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.techniques[id]
	return t, ok
}

// CVECount returns the number of stored CVEs (used by tests and metrics).
func (s *MemoryStore) CVECount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cves)
}

// IOCCount returns the number of stored IOCs (used by tests and metrics).
func (s *MemoryStore) IOCCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.iocs)
}

// iocFreshness returns the reference time used for TTL aging of an IOC: its
// LastSeenAt when set (an indicator re-observed by a later feed), else its
// FirstSeenAt. Indicators go stale, so L1 ages them out from the most recent
// sighting rather than from first ingestion.
func iocFreshness(i IOCEntry) time.Time {
	if !i.LastSeenAt.IsZero() {
		return i.LastSeenAt
	}
	return i.FirstSeenAt
}

// EvictExpired removes IOCs whose freshness time is older than now-ttl and
// returns the number evicted. A non-positive ttl disables eviction (returns 0),
// so callers must opt in explicitly. This is the in-memory (simulated) backend's
// TTL: a real TSDB backend (ClickHouse) instead relies on the engine's native
// TTL clause on ioc_entries, so TTL is not part of the backend-agnostic Store
// interface. IOCs with a zero freshness time (no timestamp in the feed) are
// never evicted, since their age is unknown.
func (s *MemoryStore) EvictExpired(now time.Time, ttl time.Duration) int {
	if ttl <= 0 {
		return 0
	}
	cutoff := now.Add(-ttl)
	s.mu.Lock()
	defer s.mu.Unlock()
	evicted := 0
	for k, i := range s.iocs {
		ref := iocFreshness(i)
		if ref.IsZero() {
			continue
		}
		if ref.Before(cutoff) {
			delete(s.iocs, k)
			evicted++
		}
	}
	return evicted
}
