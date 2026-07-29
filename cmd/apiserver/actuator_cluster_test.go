package main

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/soc"
)

// These tests close the SOAR loop at the data plane: with a live-cluster
// NetworkPolicy applier attached, IsReal() flips from false to true and
// isolate/harden actions land as REAL networkingv1.NetworkPolicy objects.

// TestNetworkPolicyActuator_IsRealWithClusterApplier proves the core promise:
// attaching a cluster applier alone (IP ACL disabled) turns IsReal() true.
func TestNetworkPolicyActuator_IsRealWithClusterApplier(t *testing.T) {
	gw := security.NewGateway(security.GatewayConfig{EnableIPACL: false})
	npe := security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{})
	act := newNetworkPolicyActuator(gw, npe)

	if act.IsReal() {
		t.Fatal("precondition: no ACL + no applier must be simulated")
	}

	act.SetClusterApplier(security.NewNetworkPolicyApplier(fake.NewSimpleClientset()))

	if !act.IsReal() {
		t.Fatal("IsReal must become true once a cluster applier is attached")
	}
}

// TestNetworkPolicyActuator_IsolateAppliedToCluster verifies isolate-host with
// an applier produces Mode="real" and a deny-all policy actually lands in the
// (fake) cluster with the SOAR applied-by label.
func TestNetworkPolicyActuator_IsolateAppliedToCluster(t *testing.T) {
	client := fake.NewSimpleClientset()
	gw := security.NewGateway(security.GatewayConfig{EnableIPACL: false})
	npe := security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{})
	act := newNetworkPolicyActuator(gw, npe)
	act.SetClusterApplier(security.NewNetworkPolicyApplier(client))

	res := act.Actuate(context.Background(), soc.ActionIsolateHost, "victim-pod")
	if res.Mode != "real" || !res.Executed {
		t.Fatalf("isolate with applier must be real+executed, got %+v", res)
	}

	// The policy is REALLY in the cluster.
	list, err := client.NetworkingV1().NetworkPolicies("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list cluster policies: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 NetworkPolicy in cluster, got %d", len(list.Items))
	}
	np := list.Items[0]
	// Deny-all semantics: no rules, both policy types.
	if len(np.Spec.Ingress) != 0 || len(np.Spec.Egress) != 0 {
		t.Fatalf("expected deny-all, got ingress=%d egress=%d", len(np.Spec.Ingress), len(np.Spec.Egress))
	}
	if len(np.Spec.PolicyTypes) != 2 {
		t.Fatalf("expected both PolicyTypes, got %v", np.Spec.PolicyTypes)
	}
	if np.Labels["cloudai.fusion/applied-by"] != "soar-actuator" {
		t.Fatalf("applied-by label missing: %v", np.Labels)
	}
	if np.Spec.PodSelector.MatchLabels["app"] != "victim-pod" {
		t.Fatalf("selector must target the victim, got %v", np.Spec.PodSelector.MatchLabels)
	}
}

// TestNetworkPolicyActuator_HardenAppliedToCluster verifies harden-workload
// also reaches the data plane through the same applier path.
func TestNetworkPolicyActuator_HardenAppliedToCluster(t *testing.T) {
	client := fake.NewSimpleClientset()
	act := newNetworkPolicyActuator(
		security.NewGateway(security.GatewayConfig{EnableIPACL: false}),
		security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{}),
	)
	act.SetClusterApplier(security.NewNetworkPolicyApplier(client))

	res := act.Actuate(context.Background(), soc.ActionHardenWorkload, "api-server")
	if res.Mode != "real" || !res.Executed {
		t.Fatalf("harden with applier must be real+executed, got %+v", res)
	}
	list, _ := client.NetworkingV1().NetworkPolicies("default").List(context.Background(), metav1.ListOptions{})
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 NetworkPolicy in cluster, got %d", len(list.Items))
	}
}

// TestNetworkPolicyActuator_NilClientStaysHonest: attaching an applier built
// from a nil client must NOT flip IsReal (NewNetworkPolicyApplier(nil) is nil).
func TestNetworkPolicyActuator_NilClientStaysHonest(t *testing.T) {
	act := newNetworkPolicyActuator(
		security.NewGateway(security.GatewayConfig{EnableIPACL: false}),
		security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{}),
	)
	act.SetClusterApplier(security.NewNetworkPolicyApplier(nil))
	if act.IsReal() {
		t.Fatal("nil-client applier must not make the actuator claim real")
	}
	res := act.Actuate(context.Background(), soc.ActionIsolateHost, "pod-x")
	if res.Mode != "simulated" {
		t.Fatalf("without a working cluster the mode must stay simulated, got %q", res.Mode)
	}
}

// TestSOARChain_EndToEnd_RealEnforcement wires the FULL production chain:
// intel IOC → SOC engine detection → c2-egress playbook → networkPolicyActuator
// with IP ACL enabled AND a cluster applier attached. Every automated action
// must execute, block-network and isolate-host must both be Mode="real", and
// the NetworkPolicy must exist in the cluster afterwards.
func TestSOARChain_EndToEnd_RealEnforcement(t *testing.T) {
	t.Cleanup(capability.Reset)
	ctx := context.Background()

	// Threat intel: 203.0.113.66 is a known C2 endpoint.
	store := intel.NewMemoryStore()
	_ = store.UpsertIOCs([]intel.IOCEntry{{IOCType: "ip", Value: "203.0.113.66", Severity: intel.SeverityHigh}})

	// Real actuator: gateway ACL on + fake-cluster applier attached.
	acl := security.NewIPAccessList(nil, nil)
	gw := security.NewGateway(security.GatewayConfig{IPAccessList: acl, EnableIPACL: true})
	npe := security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{})
	client := fake.NewSimpleClientset()
	act := newNetworkPolicyActuator(gw, npe)
	act.SetClusterApplier(security.NewNetworkPolicyApplier(client))

	eng := soc.NewEngine(store, nil)
	eng.SetActuator(act)

	// Detection: outbound connection to the C2 IP yields a T1071 finding.
	findings, err := eng.AnalyzeNetwork(ctx, "edge-node-7", []string{"203.0.113.66"}, nil)
	if err != nil || len(findings) != 1 {
		t.Fatalf("seed detection: %v (%d findings)", err, len(findings))
	}

	resp, err := eng.Respond(ctx, findings[0].ID)
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if resp.Playbook != "c2-egress" || !resp.Executed {
		t.Fatalf("expected auto-executed c2-egress, got %+v", resp)
	}

	// Every automated action executed; block + isolate both real.
	realModes := map[soc.ActionType]bool{}
	blockTarget := ""
	for _, a := range resp.Actuations {
		if !a.Executed {
			t.Fatalf("actuation not executed: %+v", a)
		}
		if a.Mode == "real" {
			realModes[a.Action] = true
		}
		if a.Action == soc.ActionBlockNetwork {
			blockTarget = a.Target
		}
	}
	if !realModes[soc.ActionBlockNetwork] {
		t.Fatal("block-network must be real (IP ACL enabled)")
	}
	if !realModes[soc.ActionIsolateHost] {
		t.Fatal("isolate-host must be real (cluster applier attached)")
	}

	// Data-plane proof 1: block-network genuinely acted. A network finding's
	// asset is the internal host that reached out to the C2 endpoint (the C2 IP
	// itself is carried in finding.Evidence), so the SOAR block targets that
	// host and the actuator now holds it as an ACTIVE mitigation.
	if blockTarget != "edge-node-7" {
		t.Fatalf("block-network should target the finding asset, got %q", blockTarget)
	}
	blockedActive := false
	for _, m := range act.Active() {
		if m.Action == soc.ActionBlockNetwork && m.Target == blockTarget {
			blockedActive = true
		}
	}
	if !blockedActive {
		t.Fatal("block-network must be recorded as an active gateway mitigation")
	}
	// Data-plane proof 2: the isolation NetworkPolicy is really in the cluster.
	list, _ := client.NetworkingV1().NetworkPolicies("default").List(ctx, metav1.ListOptions{})
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 isolation policy in cluster, got %d", len(list.Items))
	}
	// End-to-end honesty: the wired actuator reports real enforcement, which is
	// exactly what Orchestrator.IsReal() delegates to after SetActuator.
	if !act.IsReal() {
		t.Fatal("actuator must report IsReal=true for this chain")
	}
}
