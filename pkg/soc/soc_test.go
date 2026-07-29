package soc

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

func seededIntel(t *testing.T) *intel.MemoryStore {
	t.Helper()
	s := intel.NewMemoryStore()
	err := s.UpsertIOCs([]intel.IOCEntry{
		{IOCType: "sha256", Value: "deadbeef", Severity: intel.SeverityCritical, ThreatActor: "APT-X"},
		{IOCType: "ip", Value: "203.0.113.9", Severity: intel.SeverityHigh},
		{IOCType: "domain", Value: "evil.example", Severity: intel.SeverityHigh},
	})
	if err != nil {
		t.Fatalf("seed iocs: %v", err)
	}
	return s
}

func TestEndpointDetector_MatchesMaliciousHash(t *testing.T) {
	d := NewEndpointDetector(seededIntel(t))
	f, err := d.Analyze(context.Background(), "host-1", []string{"deadbeef", "cafe"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(f) != 1 || f[0].Technique != "T1204" || f[0].Well != WellEndpoint {
		t.Fatalf("expected one T1204 endpoint finding, got %+v", f)
	}
	if f[0].Severity != intel.SeverityCritical {
		t.Fatalf("severity should inherit the IOC's, got %s", f[0].Severity)
	}
}

func TestNetworkDetector_MatchesIPAndDomain(t *testing.T) {
	d := NewNetworkDetector(seededIntel(t))
	f, err := d.Analyze(context.Background(), "host-2", []string{"203.0.113.9"}, []string{"evil.example", "ok.example"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(f) != 2 {
		t.Fatalf("expected 2 network findings (ip+domain), got %d: %+v", len(f), f)
	}
	for _, x := range f {
		if x.Technique != "T1071" || x.Tactic != "TA0011" {
			t.Fatalf("network finding must map to T1071/TA0011: %+v", x)
		}
	}
}

func TestWorkloadDetector_CISPosture(t *testing.T) {
	d := NewWorkloadDetector()
	f, err := d.Analyze(context.Background(), WorkloadSpec{
		Name: "api", Namespace: "prod", Privileged: true, HostNetwork: true, RunAsRoot: true,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(f) != 3 {
		t.Fatalf("expected 3 posture findings, got %d: %+v", len(f), f)
	}
	// The privileged container must be critical and map to escape (T1611).
	var sawCritical bool
	for _, x := range f {
		if x.Severity == intel.SeverityCritical && x.Technique == "T1611" {
			sawCritical = true
		}
	}
	if !sawCritical {
		t.Fatalf("privileged container must yield a critical T1611 finding: %+v", f)
	}
}

func TestImageDetector_FlagsHighCVEs(t *testing.T) {
	d := NewImageDetector(7.0)
	f, err := d.Analyze(context.Background(), ImageScan{
		Reference: "registry/app:1.0",
		CVEs:      []ImageCVE{{ID: "CVE-2024-1", CVSS: 9.8}, {ID: "CVE-2024-2", CVSS: 5.0}},
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(f) != 1 || f[0].Technique != "T1190" {
		t.Fatalf("only the high CVE should be flagged as T1190, got %+v", f)
	}
	if f[0].Severity != intel.SeverityCritical {
		t.Fatalf("CVSS 9.8 must be critical, got %s", f[0].Severity)
	}
}

func TestIdentityDetector_BruteForceAndImpossibleTravel(t *testing.T) {
	d := NewIdentityDetector(IdentityConfig{FailureThreshold: 3, Window: 10 * time.Minute})
	base := time.Now().UTC()
	events := []AuthEvent{
		{User: "alice", Success: false, Timestamp: base},
		{User: "alice", Success: false, Timestamp: base.Add(1 * time.Minute)},
		{User: "alice", Success: false, Timestamp: base.Add(2 * time.Minute)},
		// bob: successful logins from two countries within the window.
		{User: "bob", Success: true, Country: "US", SourceIP: "1.1.1.1", Timestamp: base},
		{User: "bob", Success: true, Country: "CN", SourceIP: "2.2.2.2", Timestamp: base.Add(3 * time.Minute)},
	}
	f, err := d.Analyze(context.Background(), events)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	var brute, travel bool
	for _, x := range f {
		if x.Technique == "T1110" {
			brute = true
		}
		if x.Technique == "T1078" && x.Severity == intel.SeverityCritical {
			travel = true
		}
	}
	if !brute || !travel {
		t.Fatalf("expected both brute-force (T1110) and impossible-travel (T1078): %+v", f)
	}
}

func TestOrchestrator_MatchesPlaybooks(t *testing.T) {
	o := NewOrchestrator(nil)

	// C2 finding → c2-egress playbook, auto-executed.
	resp := o.Respond(Finding{ID: "1", Technique: "T1071", Severity: intel.SeverityHigh, Asset: "h"})
	if resp.Playbook != "c2-egress" || !resp.Executed {
		t.Fatalf("expected auto-executed c2-egress, got %+v", resp)
	}
	hasBlock := false
	for _, a := range resp.Actions {
		if a.Type == ActionBlockNetwork {
			hasBlock = true
		}
	}
	if !hasBlock {
		t.Fatalf("c2-egress must block network: %+v", resp.Actions)
	}

	// Account takeover requires approval → not auto-executed.
	resp2 := o.Respond(Finding{ID: "2", Technique: "T1078", Severity: intel.SeverityCritical, Asset: "u"})
	if resp2.Playbook != "account-takeover" || resp2.Executed {
		t.Fatalf("account-takeover must require approval (not executed): %+v", resp2)
	}

	// Unknown low-severity → notify-only fallback.
	resp3 := o.Respond(Finding{ID: "3", Technique: "T9999", Severity: intel.SeverityLow, Asset: "x"})
	if resp3.Playbook != "none" {
		t.Fatalf("low-severity unknown should fall through to none, got %+v", resp3)
	}
}

func TestEngine_EndToEnd(t *testing.T) {
	t.Cleanup(capability.Reset)
	ctx := context.Background()
	eng := NewEngine(seededIntel(t), nil)

	f, err := eng.AnalyzeNetwork(ctx, "host-9", []string{"203.0.113.9"}, nil)
	if err != nil {
		t.Fatalf("analyze network: %v", err)
	}
	if len(f) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(f))
	}
	if eng.Findings(0)[0].ID != f[0].ID {
		t.Fatalf("finding should be retained in the store")
	}

	resp, err := eng.Respond(ctx, f[0].ID)
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if resp.Playbook != "c2-egress" {
		t.Fatalf("expected c2-egress response, got %s", resp.Playbook)
	}
	if _, err := eng.Respond(ctx, "nonexistent"); err == nil {
		t.Fatalf("responding to an unknown finding must error")
	}
}

func TestFindingStore_RingEviction(t *testing.T) {
	s := NewFindingStore(2)
	s.Add(Finding{ID: "a", DetectedAt: time.Now()})
	s.Add(Finding{ID: "b", DetectedAt: time.Now().Add(time.Second)})
	s.Add(Finding{ID: "c", DetectedAt: time.Now().Add(2 * time.Second)})
	if s.Count() != 2 {
		t.Fatalf("capacity 2 must retain 2, got %d", s.Count())
	}
	if _, ok := s.Get("a"); ok {
		t.Fatalf("oldest finding 'a' should have been evicted")
	}
	if _, ok := s.Get("c"); !ok {
		t.Fatalf("newest finding 'c' must be present")
	}
}
