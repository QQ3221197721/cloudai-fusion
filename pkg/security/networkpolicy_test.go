package security

import (
	"context"
	"testing"
	"time"
)

// These tests cover the NetworkPolicyEngine control plane — previously only
// exercised indirectly. They pin the observe -> generate -> approve lifecycle,
// the deny-by-default isolation posture, flow de-duplication, and status
// accounting. (npapplier_test.go covers the cluster data-plane side.)

func sampleFlow(srcPod, srcNS string, srcLabels map[string]string, dstPod, dstNS string, dstLabels map[string]string, port int) *TrafficFlow {
	return &TrafficFlow{
		SourcePod: srcPod, SourceNS: srcNS, SourceLabels: srcLabels,
		DestPod: dstPod, DestNS: dstNS, DestLabels: dstLabels,
		Port: port, Protocol: "TCP", BytesTotal: 1024, RequestCount: 5,
		LastSeen: time.Now().UTC(),
	}
}

// TestNetworkPolicyEngine_EnforceIsolation proves isolation yields a deny-all
// (empty ingress AND egress) ACTIVE policy targeting the requested selector.
func TestNetworkPolicyEngine_EnforceIsolation(t *testing.T) {
	e := NewNetworkPolicyEngine(NetworkPolicyEngineConfig{})
	pol := e.EnforceIsolation("prod", "compromised", map[string]string{"app": "victim"})

	if pol == nil {
		t.Fatal("EnforceIsolation returned nil")
	}
	if len(pol.Ingress) != 0 || len(pol.Egress) != 0 {
		t.Fatalf("isolation must be deny-all, got ingress=%d egress=%d", len(pol.Ingress), len(pol.Egress))
	}
	if pol.Status != "active" {
		t.Fatalf("isolation policy status = %q, want active", pol.Status)
	}
	if pol.Namespace != "prod" || pol.Selector["app"] != "victim" {
		t.Fatalf("isolation must target prod/victim, got ns=%s selector=%v", pol.Namespace, pol.Selector)
	}
	// It must be tracked by the engine.
	found := false
	for _, p := range e.ListPolicies() {
		if p.ID == pol.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("isolation policy must be listed by the engine")
	}
}

// TestNetworkPolicyEngine_EnforceIsolation_DefaultsSelector verifies namespace
// and selector defaulting when omitted.
func TestNetworkPolicyEngine_EnforceIsolation_DefaultsSelector(t *testing.T) {
	e := NewNetworkPolicyEngine(NetworkPolicyEngineConfig{})
	pol := e.EnforceIsolation("", "lonely", nil)
	if pol.Namespace != "default" {
		t.Fatalf("empty namespace must default to 'default', got %q", pol.Namespace)
	}
	if pol.Selector["app"] != "lonely" {
		t.Fatalf("empty selector must default to app=name, got %v", pol.Selector)
	}
}

// TestNetworkPolicyEngine_GenerateFromFlows proves observed traffic becomes a
// least-privilege allow policy (draft) with a matching ingress rule.
func TestNetworkPolicyEngine_GenerateFromFlows(t *testing.T) {
	e := NewNetworkPolicyEngine(NetworkPolicyEngineConfig{})
	e.IngestFlow(sampleFlow("frontend", "web", map[string]string{"app": "frontend"},
		"backend", "api", map[string]string{"app": "backend"}, 8080))

	policies := e.GeneratePolicies(context.Background())
	if len(policies) == 0 {
		t.Fatal("expected at least one generated policy")
	}
	var p *NetworkPolicySpec
	for _, cand := range policies {
		if cand.Namespace == "api" {
			p = cand
		}
	}
	if p == nil {
		t.Fatalf("expected a policy in the 'api' namespace, got %+v", policies)
	}
	if p.Status != "draft" {
		t.Fatalf("generated policy must start as draft, got %q", p.Status)
	}
	if p.Selector["app"] != "backend" {
		t.Fatalf("selector must target backend, got %v", p.Selector)
	}
	if len(p.Ingress) == 0 {
		t.Fatal("generated policy must have an ingress allow rule from the observed flow")
	}
}

// TestNetworkPolicyEngine_FlowDeduplication proves repeated identical flows are
// merged (byte/request counters accumulate) rather than duplicated.
func TestNetworkPolicyEngine_FlowDeduplication(t *testing.T) {
	e := NewNetworkPolicyEngine(NetworkPolicyEngineConfig{})
	// Two identical flows (same key), each RequestCount 5.
	e.IngestFlow(sampleFlow("a", "ns", map[string]string{"app": "a"}, "b", "ns", map[string]string{"app": "b"}, 443))
	e.IngestFlow(sampleFlow("a", "ns", map[string]string{"app": "a"}, "b", "ns", map[string]string{"app": "b"}, 443))

	flows := e.GetFlows()
	if len(flows) != 1 {
		t.Fatalf("identical flows must merge into 1, got %d", len(flows))
	}
	// Merged flow must accumulate the two request counts (5 + 5 = 10).
	if flows[0].RequestCount != 10 {
		t.Fatalf("merged flow request count = %d, want 10 (accumulated)", flows[0].RequestCount)
	}
}

// TestNetworkPolicyEngine_ApproveLifecycle proves a draft policy transitions to
// active with an AppliedAt timestamp, and unknown IDs error.
func TestNetworkPolicyEngine_ApproveLifecycle(t *testing.T) {
	e := NewNetworkPolicyEngine(NetworkPolicyEngineConfig{})
	e.IngestFlow(sampleFlow("c", "z", map[string]string{"app": "c"}, "d", "z", map[string]string{"app": "d"}, 9000))
	policies := e.GeneratePolicies(context.Background())
	if len(policies) == 0 {
		t.Fatal("need a generated draft to approve")
	}
	id := policies[0].ID

	if err := e.ApprovePolicy(id); err != nil {
		t.Fatalf("ApprovePolicy: %v", err)
	}
	// Confirm it is now active with a timestamp.
	for _, p := range e.ListPolicies() {
		if p.ID == id {
			if p.Status != "active" {
				t.Fatalf("approved policy status = %q, want active", p.Status)
			}
			if p.AppliedAt == nil {
				t.Fatal("approved policy must set AppliedAt")
			}
		}
	}
	if err := e.ApprovePolicy("does-not-exist"); err == nil {
		t.Fatal("approving an unknown policy must error")
	}
}

// TestNetworkPolicyEngine_Status proves the accounting reflects flows, drafts,
// actives and zones.
func TestNetworkPolicyEngine_Status(t *testing.T) {
	e := NewNetworkPolicyEngine(NetworkPolicyEngineConfig{})
	e.IngestFlow(sampleFlow("s", "n", map[string]string{"app": "s"}, "t", "n", map[string]string{"app": "t"}, 80))
	drafts := e.GeneratePolicies(context.Background())
	if len(drafts) == 0 {
		t.Fatal("need a draft for status accounting")
	}
	_ = e.ApprovePolicy(drafts[0].ID)

	st := e.Status()
	if st.TotalFlows != 1 {
		t.Fatalf("TotalFlows = %d, want 1", st.TotalFlows)
	}
	if st.ActivePolicies < 1 {
		t.Fatalf("ActivePolicies = %d, want >= 1", st.ActivePolicies)
	}
	if st.TotalPolicies < st.ActivePolicies+st.DraftPolicies {
		t.Fatalf("total %d < active %d + draft %d", st.TotalPolicies, st.ActivePolicies, st.DraftPolicies)
	}
	// Default segmentation zones must be present.
	if st.Zones == 0 {
		t.Fatal("expected default segmentation zones")
	}
}

// TestNetworkPolicyEngine_GenerateZonePolicies proves zone-based segmentation
// produces policies for the configured zones.
func TestNetworkPolicyEngine_GenerateZonePolicies(t *testing.T) {
	e := NewNetworkPolicyEngine(NetworkPolicyEngineConfig{})
	if len(e.GetZones()) == 0 {
		t.Skip("no default zones configured")
	}
	zonePolicies := e.GenerateZonePolicies()
	if len(zonePolicies) == 0 {
		t.Fatal("expected zone policies from default zones")
	}
}
