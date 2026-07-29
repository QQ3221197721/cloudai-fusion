package main

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/soc"
)

// TestNetworkPolicyActuator_RealBlockEnforced verifies that when the gateway IP
// ACL is enabled, block-network is a REAL enforcement action: the blocked IP is
// actually rejected by the gateway afterwards, and the actuator reports Mode=real.
func TestNetworkPolicyActuator_RealBlockEnforced(t *testing.T) {
	acl := security.NewIPAccessList(nil, nil)
	gw := security.NewGateway(security.GatewayConfig{IPAccessList: acl, EnableIPACL: true})
	npe := security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{})
	act := newNetworkPolicyActuator(gw, npe)

	if !act.IsReal() {
		t.Fatalf("actuator must report real when gateway IP ACL is enabled")
	}
	if !acl.IsAllowed("198.51.100.7") {
		t.Fatalf("precondition: IP should be allowed before block")
	}

	res := act.Actuate(context.Background(), soc.ActionBlockNetwork, "198.51.100.7")
	if res.Mode != "real" || !res.Executed {
		t.Fatalf("expected real+executed block, got %+v", res)
	}
	// The block is genuinely enforced now.
	if acl.IsAllowed("198.51.100.7") {
		t.Fatalf("blocked IP must be rejected by the gateway ACL")
	}
	// And it shows up as an active mitigation.
	if len(act.Active()) != 1 {
		t.Fatalf("expected 1 active mitigation, got %d", len(act.Active()))
	}
}

// TestNetworkPolicyActuator_IsolateCreatesActivePolicy verifies isolate-host
// creates an ACTIVE deny-by-default network policy in the real policy engine.
func TestNetworkPolicyActuator_IsolateCreatesActivePolicy(t *testing.T) {
	gw := security.NewGateway(security.GatewayConfig{EnableIPACL: false})
	npe := security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{})
	act := newNetworkPolicyActuator(gw, npe)

	res := act.Actuate(context.Background(), soc.ActionIsolateHost, "pod-xyz")
	if !res.Executed {
		t.Fatalf("isolate must execute, got %+v", res)
	}
	active := 0
	for _, p := range npe.ListPolicies() {
		if p.Status == "active" && p.Source == "soar-isolation" {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("expected 1 active isolation policy, got %d", active)
	}
}

// TestNetworkPolicyActuator_HonestSimulatedWhenDisabled verifies that with IP ACL
// disabled the actuator honestly reports simulated (no real data-plane block).
func TestNetworkPolicyActuator_HonestSimulatedWhenDisabled(t *testing.T) {
	gw := security.NewGateway(security.GatewayConfig{EnableIPACL: false})
	npe := security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{})
	act := newNetworkPolicyActuator(gw, npe)

	if act.IsReal() {
		t.Fatalf("actuator must not claim real when IP ACL is disabled")
	}
	res := act.Actuate(context.Background(), soc.ActionBlockNetwork, "203.0.113.5")
	if res.Mode != "simulated" {
		t.Fatalf("block must be simulated when IP ACL disabled, got %q", res.Mode)
	}
}

// TestNetworkPolicyActuator_UnsupportedActionHonest verifies non-network actions
// are honestly reported as not executed by this actuator.
func TestNetworkPolicyActuator_UnsupportedActionHonest(t *testing.T) {
	act := newNetworkPolicyActuator(
		security.NewGateway(security.GatewayConfig{}),
		security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{}),
	)
	res := act.Actuate(context.Background(), soc.ActionRebuildImage, "img:tag")
	if res.Executed {
		t.Fatalf("rebuild-image is not a network-policy action; must not report executed")
	}
}
