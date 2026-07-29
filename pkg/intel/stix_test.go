package intel

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// realisticBundle is a STIX 2.1 bundle in the shape MISP/OTX export: indicators
// with STIX patterns (ip/domain/hash/url, single '=' and 'IN'), a vulnerability,
// and a mitre-attack attack-pattern.
const realisticBundle = `{
  "type": "bundle",
  "id": "bundle--aa",
  "objects": [
    {"type":"indicator","spec_version":"2.1","id":"indicator--1","pattern_type":"stix",
     "pattern":"[ipv4-addr:value = '198.51.100.23']","valid_from":"2026-01-02T03:04:05Z",
     "x_severity":"high","indicator_types":["malicious-activity"],"x_threat_actor":"APT-X"},
    {"type":"indicator","spec_version":"2.1","id":"indicator--2","pattern_type":"stix",
     "pattern":"[domain-name:value = 'evil.example.com']","confidence":95},
    {"type":"indicator","spec_version":"2.1","id":"indicator--3","pattern_type":"stix",
     "pattern":"[file:hashes.'SHA-256' = 'aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899']"},
    {"type":"indicator","spec_version":"2.1","id":"indicator--4","pattern_type":"stix",
     "pattern":"[url:value = 'http://evil.example/payload']","confidence":50},
    {"type":"indicator","spec_version":"2.1","id":"indicator--5","pattern_type":"stix",
     "pattern":"[ipv4-addr:value IN ('203.0.113.7', '203.0.113.8')]","x_severity":"critical"},
    {"type":"vulnerability","spec_version":"2.1","id":"vulnerability--1","name":"CVE-2026-9999",
     "description":"Example RCE","created":"2026-01-01T00:00:00Z","x_cvss_v3_score":9.8,
     "external_references":[{"source_name":"cve","external_id":"CVE-2026-9999"}]},
    {"type":"attack-pattern","spec_version":"2.1","id":"attack-pattern--1","name":"Exploit Public-Facing Application",
     "external_references":[{"source_name":"mitre-attack","external_id":"T1190"}],
     "kill_chain_phases":[{"kill_chain_name":"mitre-attack","phase_name":"initial-access"}]},
    {"type":"relationship","spec_version":"2.1","id":"relationship--1","relationship_type":"indicates"}
  ]
}`

func TestParseSTIXBundle_Realistic(t *testing.T) {
	imp, err := ParseSTIXBundle([]byte(realisticBundle))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 4 single-value indicators + 2 from the IN list = 6 IOCs.
	if len(imp.IOCs) != 6 {
		t.Fatalf("expected 6 IOCs, got %d: %+v", len(imp.IOCs), imp.IOCs)
	}
	byVal := map[string]IOCEntry{}
	for _, i := range imp.IOCs {
		byVal[i.Value] = i
	}
	if e := byVal["198.51.100.23"]; e.IOCType != "ip" || e.Severity != SeverityHigh || e.ThreatActor != "APT-X" {
		t.Errorf("ip indicator not parsed correctly: %+v", e)
	}
	if e := byVal["evil.example.com"]; e.IOCType != "domain" || e.Severity != SeverityCritical { // conf 95
		t.Errorf("domain indicator/severity wrong: %+v", e)
	}
	if e, ok := byVal["aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"]; !ok || e.IOCType != "sha256" {
		t.Errorf("sha256 indicator missing/wrong: %+v", e)
	}
	if e := byVal["http://evil.example/payload"]; e.IOCType != "url" || e.Severity != SeverityMedium { // conf 50
		t.Errorf("url indicator/severity wrong: %+v", e)
	}
	if _, ok := byVal["203.0.113.8"]; !ok {
		t.Errorf("IN-list second value not parsed")
	}

	if len(imp.CVEs) != 1 || imp.CVEs[0].CVEID != "CVE-2026-9999" || imp.CVEs[0].CVSSv3Score != 9.8 {
		t.Fatalf("vulnerability not parsed: %+v", imp.CVEs)
	}
	if len(imp.Techniques) != 1 || imp.Techniques[0].TechniqueID != "T1190" {
		t.Fatalf("attack-pattern not parsed: %+v", imp.Techniques)
	}
}

func TestParseSTIXBundle_Errors(t *testing.T) {
	if _, err := ParseSTIXBundle([]byte(`{"not":"json`)); err == nil {
		t.Errorf("expected error on malformed json")
	}
	if _, err := ParseSTIXBundle([]byte(`{"type":"other"}`)); err == nil {
		t.Errorf("expected error on non-bundle with no objects")
	}
}

// TestHub_ImportSTIXBundle_EndToEnd proves a pushed STIX bundle lands in the
// store and is queryable by the operations wells (LookupIOCs / RecentCVEs).
func TestHub_ImportSTIXBundle_EndToEnd(t *testing.T) {
	t.Cleanup(capability.Reset)
	hub := NewHub(nil, NewMemoryStore(), nil)
	res, err := hub.ImportSTIXBundle(context.Background(), []byte(realisticBundle))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.IOCAdded != 6 || res.CVEAdded != 1 {
		t.Fatalf("unexpected sync result: %+v", res)
	}
	// The IOC is now queryable exactly as L4 network detection would query it.
	hits, err := hub.MatchIOCs(context.Background(), "ip", []string{"198.51.100.23", "8.8.8.8"})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(hits) != 1 || hits[0].Value != "198.51.100.23" {
		t.Fatalf("expected the STIX IP to be queryable, got %+v", hits)
	}
	// The technique enriched the knowledge graph.
	if tech, ok := hub.Store().TechniqueByID("T1190"); !ok || tech.Name == "" {
		t.Fatalf("STIX attack-pattern did not enrich the knowledge graph")
	}
}
