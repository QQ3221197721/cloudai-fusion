package security

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// isolationSpec builds the deny-all posture EnforceIsolation produces.
func isolationSpec(name string) *NetworkPolicySpec {
	return &NetworkPolicySpec{
		ID:        "pol-123",
		Name:      name,
		Namespace: "default",
		Type:      "cilium",
		Selector:  map[string]string{"app": "victim"},
		Ingress:   []NetworkRule{},
		Egress:    []NetworkRule{},
		Status:    "active",
		Source:    "soar-isolation",
	}
}

// TestNetworkPolicyApplier_ApplyIsolation verifies a REAL networkingv1 object
// lands in the cluster with the exact deny-all semantics of the spec.
func TestNetworkPolicyApplier_ApplyIsolation(t *testing.T) {
	client := fake.NewSimpleClientset()
	applier := NewNetworkPolicyApplier(client)
	if !applier.Available() {
		t.Fatal("applier with client must be available")
	}

	name, err := applier.Apply(context.Background(), isolationSpec("isolate-host-victim"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := client.NetworkingV1().NetworkPolicies("default").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("policy not found in cluster: %v", err)
	}
	// Deny-all: no rules + both policy types declared.
	if len(got.Spec.Ingress) != 0 || len(got.Spec.Egress) != 0 {
		t.Fatalf("expected deny-all (no rules), got ingress=%d egress=%d", len(got.Spec.Ingress), len(got.Spec.Egress))
	}
	if len(got.Spec.PolicyTypes) != 2 {
		t.Fatalf("expected both PolicyTypes, got %v", got.Spec.PolicyTypes)
	}
	if got.Spec.PodSelector.MatchLabels["app"] != "victim" {
		t.Fatalf("selector mismatch: %v", got.Spec.PodSelector.MatchLabels)
	}
	if got.Labels[appliedByLabel] != appliedByValue {
		t.Fatalf("applied-by label missing: %v", got.Labels)
	}
}

// TestNetworkPolicyApplier_ReapplyConverges verifies repeated automated
// responses to the same target update in place instead of failing.
func TestNetworkPolicyApplier_ReapplyConverges(t *testing.T) {
	client := fake.NewSimpleClientset()
	applier := NewNetworkPolicyApplier(client)

	spec := isolationSpec("isolate-host-victim")
	if _, err := applier.Apply(context.Background(), spec); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	// Second response to the same finding must converge, not error.
	spec.ID = "pol-456"
	name, err := applier.Apply(context.Background(), spec)
	if err != nil {
		t.Fatalf("re-Apply: %v", err)
	}
	got, err := client.NetworkingV1().NetworkPolicies("default").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("policy missing after re-apply: %v", err)
	}
	if got.Labels["cloudai.fusion/policy-id"] != "pol-456" {
		t.Fatalf("update did not take effect, policy-id=%s", got.Labels["cloudai.fusion/policy-id"])
	}
	// Still exactly one policy object.
	list, _ := client.NetworkingV1().NetworkPolicies("default").List(context.Background(), metav1.ListOptions{})
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 policy after converging re-apply, got %d", len(list.Items))
	}
}

// TestNetworkPolicyApplier_RuleConversion verifies allow rules translate to
// real peers/ports (labels, CIDRs, protocols).
func TestNetworkPolicyApplier_RuleConversion(t *testing.T) {
	client := fake.NewSimpleClientset()
	applier := NewNetworkPolicyApplier(client)

	spec := isolationSpec("allow-frontend")
	spec.Ingress = []NetworkRule{{
		FromLabels: map[string]string{"app": "frontend"},
		FromCIDR:   []string{"10.1.0.0/16"},
		Ports:      []PolicyPort{{Port: 8443, Protocol: "TCP"}, {Port: 53, Protocol: "UDP"}},
	}}
	name, err := applier.Apply(context.Background(), spec)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, _ := client.NetworkingV1().NetworkPolicies("default").Get(context.Background(), name, metav1.GetOptions{})
	if len(got.Spec.Ingress) != 1 {
		t.Fatalf("expected 1 ingress rule, got %d", len(got.Spec.Ingress))
	}
	rule := got.Spec.Ingress[0]
	if len(rule.From) != 2 { // pod selector peer + CIDR peer
		t.Fatalf("expected 2 peers, got %d", len(rule.From))
	}
	if rule.From[1].IPBlock == nil || rule.From[1].IPBlock.CIDR != "10.1.0.0/16" {
		t.Fatalf("CIDR peer mismatch: %+v", rule.From[1])
	}
	if len(rule.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(rule.Ports))
	}
	if *rule.Ports[1].Protocol != "UDP" {
		t.Fatalf("expected UDP for port 2, got %s", *rule.Ports[1].Protocol)
	}
}

// TestNetworkPolicyApplier_RemoveAndList covers mitigation rollback and the
// applied-by inventory.
func TestNetworkPolicyApplier_RemoveAndList(t *testing.T) {
	client := fake.NewSimpleClientset()
	applier := NewNetworkPolicyApplier(client)

	name, err := applier.Apply(context.Background(), isolationSpec("isolate-a"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	names, err := applier.ListApplied(context.Background(), "default")
	if err != nil || len(names) != 1 || names[0] != name {
		t.Fatalf("ListApplied = %v, %v; want [%s]", names, err, name)
	}
	if err := applier.Remove(context.Background(), "default", name); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// Removing again must be idempotent (NotFound tolerated).
	if err := applier.Remove(context.Background(), "default", name); err != nil {
		t.Fatalf("second Remove not idempotent: %v", err)
	}
	names, _ = applier.ListApplied(context.Background(), "default")
	if len(names) != 0 {
		t.Fatalf("expected empty inventory after remove, got %v", names)
	}
}

// TestNetworkPolicyApplier_NilSafety: a nil applier must be safe and honest.
func TestNetworkPolicyApplier_NilSafety(t *testing.T) {
	var applier *NetworkPolicyApplier
	if applier.Available() {
		t.Fatal("nil applier must not report available")
	}
	if _, err := applier.Apply(context.Background(), isolationSpec("x")); err == nil {
		t.Fatal("nil applier Apply must error")
	}
	if NewNetworkPolicyApplier(nil) != nil {
		t.Fatal("NewNetworkPolicyApplier(nil) must return nil")
	}
	if applier.Connected(context.Background()) {
		t.Fatal("nil applier must not report connected")
	}
}

// TestNetworkPolicyApplier_Connected: a fake cluster must count as reachable
// (list probe path), backing IsReal claims with an actual round-trip.
func TestNetworkPolicyApplier_Connected(t *testing.T) {
	applier := NewNetworkPolicyApplier(fake.NewSimpleClientset())
	if !applier.Connected(context.Background()) {
		t.Fatal("fake cluster should be reachable via the list probe")
	}
}

// TestSanitizePolicyName ensures arbitrary action/target strings become valid
// K8s object names.
func TestSanitizePolicyName(t *testing.T) {
	cases := map[string]string{
		"isolate-host-victim":  "isolate-host-victim",
		"Harden_Workload/Pod":  "harden-workload-pod",
		"UPPER case  name":     "upper-case-name",
		"":                     "soar-policy",
		"###":                  "soar-policy",
		"trailing-separators—": "trailing-separators",
	}
	for in, want := range cases {
		if got := sanitizePolicyName(in); got != want {
			t.Errorf("sanitizePolicyName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestToNetworkingV1_DefaultNamespace covers namespace defaulting.
func TestToNetworkingV1_DefaultNamespace(t *testing.T) {
	spec := isolationSpec("x")
	spec.Namespace = ""
	np := toNetworkingV1(spec, "default")
	if np.Namespace != "default" {
		t.Fatalf("namespace = %q, want default", np.Namespace)
	}
	var _ *networkingv1.NetworkPolicy = np // type sanity
}
