package hunt

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// newSeededStore builds a MemoryStore with representative L1 data.
func newSeededStore(t *testing.T) *intel.MemoryStore {
	t.Helper()
	s := intel.NewMemoryStore()
	now := time.Now().UTC()
	if err := s.UpsertCVE(intel.CVEEntry{
		CVEID: "CVE-2024-0001", CVSSv3Score: 9.8, MitreTags: []string{"T1190"}, PublishedAt: now,
	}); err != nil {
		t.Fatalf("seed cve: %v", err)
	}
	if err := s.UpsertCVE(intel.CVEEntry{
		CVEID: "CVE-2024-0002", CVSSv3Score: 3.0, PublishedAt: now,
	}); err != nil {
		t.Fatalf("seed cve: %v", err)
	}
	if err := s.UpsertIOCs([]intel.IOCEntry{
		{IOCType: "ip", Value: "203.0.113.9", Severity: intel.SeverityCritical, FirstSeenAt: now},
	}); err != nil {
		t.Fatalf("seed ioc: %v", err)
	}
	if err := s.PutKnowledgeGraph(intel.KnowledgeGraph{
		Techniques: []intel.Technique{{TechniqueID: "T1190", Name: "Exploit Public-Facing Application", TacticIDs: []string{"TA0001"}}},
	}); err != nil {
		t.Fatalf("seed kg: %v", err)
	}
	return s
}

func TestEngine_Hunt_CorrelatesAndEnriches(t *testing.T) {
	store := newSeededStore(t)
	eng := NewEngine(store, nil, nil) // default heuristic reasoner

	findings, err := eng.Hunt(context.Background(), Query{
		Name:      "daily-critical-hunt",
		MinCVSS:   7.0,
		IOCType:   "ip",
		IOCValues: []string{"203.0.113.9", "10.0.0.1"},
	})
	if err != nil {
		t.Fatalf("Hunt: %v", err)
	}

	// Expect: 1 CVE finding (9.8 passes MinCVSS 7.0; 3.0 filtered) + 1 IOC hit.
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(findings), findings)
	}

	// Sorted by confidence desc — the critical IOC (0.95) leads the CVE (0.98)?
	// CVE 9.8 → 0.98 confidence > IOC 0.95, so CVE leads.
	if findings[0].Confidence < findings[1].Confidence {
		t.Fatalf("findings not sorted by confidence desc: %+v", findings)
	}

	// The CVE finding must be enriched with the technique name from L1.
	var enriched bool
	for _, f := range findings {
		if f.Technique == "T1190" {
			if f.TechniqueName != "Exploit Public-Facing Application" {
				t.Fatalf("technique not enriched from L1: %+v", f)
			}
			if f.Tactic != "TA0001" {
				t.Fatalf("tactic mapping wrong: %+v", f)
			}
			enriched = true
		}
	}
	if !enriched {
		t.Fatalf("expected a T1190 finding enriched from the knowledge graph")
	}
}

func TestEngine_Hunt_MinCVSSFilters(t *testing.T) {
	store := newSeededStore(t)
	eng := NewEngine(store, nil, nil)

	findings, err := eng.Hunt(context.Background(), Query{Name: "high-only", MinCVSS: 9.5})
	if err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	// Only CVE-2024-0001 (9.8) passes; no IOC query provided.
	if len(findings) != 1 || findings[0].ID != "cve:CVE-2024-0001" {
		t.Fatalf("MinCVSS filter failed: %+v", findings)
	}
	if findings[0].Severity != intel.SeverityCritical {
		t.Fatalf("severity mapping wrong: %+v", findings[0])
	}
}

func TestHeuristicReasoner_Contract(t *testing.T) {
	r := HeuristicReasoner{}
	if r.IsLLM() {
		t.Fatalf("heuristic reasoner must report IsLLM()=false")
	}
	if r.Name() != "heuristic" {
		t.Fatalf("Name()=%q, want heuristic", r.Name())
	}
	var _ Reasoner = r
}

func TestTechniqueMapping(t *testing.T) {
	cases := []struct {
		iocType string
		want    string
	}{
		{"ip", "T1071"},
		{"domain", "T1071"},
		{"sha256", "T1204"},
		{"unknown", "T1190"},
	}
	for _, c := range cases {
		if got := techniqueForIOC(c.iocType); got != c.want {
			t.Fatalf("techniqueForIOC(%q)=%q, want %q", c.iocType, got, c.want)
		}
	}
	if got := primaryTechnique([]string{"CWE-79", "T1059"}); got != "T1059" {
		t.Fatalf("primaryTechnique should pick the T-prefixed tag, got %q", got)
	}
	if got := primaryTechnique(nil); got != "T1190" {
		t.Fatalf("primaryTechnique(nil) should default to T1190, got %q", got)
	}
}

// stubReasoner lets us assert the LLM/real reporting path without a real endpoint.
type stubReasoner struct{ llm bool }

func (s stubReasoner) Name() string { return "stub" }
func (s stubReasoner) IsLLM() bool  { return s.llm }
func (s stubReasoner) Reason(context.Context, Query, Signals) ([]Finding, error) {
	return []Finding{{ID: "stub", Technique: "T1190", Confidence: 0.5}}, nil
}

func TestEngine_CustomReasoner(t *testing.T) {
	store := intel.NewMemoryStore()
	eng := NewEngine(store, stubReasoner{llm: true}, nil)
	findings, err := eng.Hunt(context.Background(), Query{Name: "stub-hunt"})
	if err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if len(findings) != 1 || findings[0].ID != "stub" {
		t.Fatalf("custom reasoner not used: %+v", findings)
	}
}
