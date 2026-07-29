// Package soc implements the Operations-layer deep wells (L3-L8) of CloudAI
// Fusion's AISecOps platform: a Security Operations Center that turns raw
// telemetry into MITRE ATT&CK-mapped findings and orchestrates response.
//
// Wells:
//
//	L3 Endpoint   — file/process indicators matched against L1 IOC hashes.
//	L4 Network    — connections matched against L1 IOC IPs/domains.
//	L5 Workload   — CIS-style Kubernetes posture checks.
//	L6 Identity   — brute-force and impossible-travel auth anomalies.
//	L7 Image      — container image CVE triage.
//	L8 Response   — SOAR playbooks that act on findings (isolate/block/quarantine).
//
// Design (consistent with the platform's honesty model):
//   - Detectors are deterministic and rule-based, so they run in CI with no
//     external dependency; each is reported to pkg/capability (rule-based =
//     simulated until a real analytics backend is wired).
//   - Every analysis and response records a signed receipt in pkg/evidence.
//   - Findings and responses are optionally emitted onto the EventBus deep-well
//     fabric (pkg/eventbus) so L3-L7 escalate to L8 and L8 records into L13.
//
// Cross-deep-well integration:
//
//	L3-L7 ⇐ L1  (Intelligence): IOC/CVE lookups drive detection.
//	L3-L7 ⇒ L8  (Response):     findings feed SOAR playbooks.
//	L8    ⇒ L13 (Evidence):     responses are signed and verifiable.
package soc

import (
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// Severity reuses the L1 intelligence severity scale so findings compose with
// CVE/IOC criticality without a second, divergent enum.
type Severity = intel.Severity

// Well identifies the operations-layer deep well that produced a finding.
type Well int

const (
	WellEndpoint      Well = 3
	WellNetwork       Well = 4
	WellCloudWorkload Well = 5
	WellIdentity      Well = 6
	WellImage         Well = 7
	WellResponse      Well = 8
)

// String returns the stable well label (e.g. "L3-endpoint").
func (w Well) String() string {
	switch w {
	case WellEndpoint:
		return "L3-endpoint"
	case WellNetwork:
		return "L4-network"
	case WellCloudWorkload:
		return "L5-workload"
	case WellIdentity:
		return "L6-identity"
	case WellImage:
		return "L7-image"
	case WellResponse:
		return "L8-response"
	default:
		return "L?-unknown"
	}
}

// Finding is one detection produced by an operations-layer well, mapped to MITRE
// ATT&CK for consistent triage and correlation with L2 hunting.
type Finding struct {
	ID         string         `json:"id"`
	Well       Well           `json:"well"`
	WellName   string         `json:"well_name"`
	Technique  string         `json:"technique"` // e.g. "T1204"
	Tactic     string         `json:"tactic"`    // e.g. "TA0002"
	Severity   Severity       `json:"severity"`
	Asset      string         `json:"asset"`
	Title      string         `json:"title"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	DetectedAt time.Time      `json:"detected_at"`
}

// IntelReader is the minimal L1 surface the detectors consume. It is satisfied by
// intel.MemoryStore and intel.SQLStore.
type IntelReader interface {
	LookupIOCs(iocType string, values []string) ([]intel.IOCEntry, error)
	TechniqueByID(id string) (intel.Technique, bool)
}

// FindingStore is a bounded, concurrency-safe in-memory store of recent findings.
// It is the query surface for GET /soc/findings and the input roster for SOAR.
type FindingStore struct {
	mu       sync.RWMutex
	findings []Finding
	byID     map[string]Finding
	capacity int
}

// NewFindingStore builds a store retaining up to capacity findings (<=0 => 1000).
func NewFindingStore(capacity int) *FindingStore {
	if capacity <= 0 {
		capacity = 1000
	}
	return &FindingStore{byID: make(map[string]Finding), capacity: capacity}
}

// Add stores findings, evicting oldest entries beyond capacity (ring buffer).
func (s *FindingStore) Add(findings ...Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range findings {
		s.findings = append(s.findings, f)
		s.byID[f.ID] = f
	}
	if len(s.findings) > s.capacity {
		drop := s.findings[:len(s.findings)-s.capacity]
		for _, f := range drop {
			delete(s.byID, f.ID)
		}
		s.findings = s.findings[len(s.findings)-s.capacity:]
	}
}

// Get returns a finding by ID.
func (s *FindingStore) Get(id string) (Finding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.byID[id]
	return f, ok
}

// List returns findings newest-first, capped at limit (<=0 => all).
func (s *FindingStore) List(limit int) []Finding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Finding, len(s.findings))
	copy(out, s.findings)
	sort.SliceStable(out, func(i, j int) bool { return out[i].DetectedAt.After(out[j].DetectedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Count returns the number of stored findings.
func (s *FindingStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.findings)
}

// severityFromCVSS maps a CVSS v3 score to a severity band.
func severityFromCVSS(score float32) Severity {
	switch {
	case score >= 9.0:
		return intel.SeverityCritical
	case score >= 7.0:
		return intel.SeverityHigh
	case score >= 4.0:
		return intel.SeverityMedium
	default:
		return intel.SeverityLow
	}
}

// tacticForTechnique maps a technique to its primary tactic (best-effort default
// when the L1 knowledge graph has not enriched it).
func tacticForTechnique(technique string) string {
	switch technique {
	case "T1190", "T1133", "T1566", "T1078":
		return "TA0001" // Initial Access
	case "T1204", "T1059":
		return "TA0002" // Execution
	case "T1071", "T1105", "T1571":
		return "TA0011" // Command and Control
	case "T1110":
		return "TA0006" // Credential Access
	case "T1610", "T1611":
		return "TA0004" // Privilege Escalation
	default:
		return "TA0001"
	}
}
