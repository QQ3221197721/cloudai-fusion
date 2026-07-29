package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/soc"
)

// networkPolicyActuator is the REAL L8 response executor, backed by the platform's
// existing security subsystems rather than a placeholder:
//   - the API gateway's IP access-control list: BlockIP is genuine in-process
//     enforcement — when IP ACL is enabled, GatewayMiddleware rejects the source;
//   - the network-policy engine: isolate/harden create ACTIVE deny-by-default
//     NetworkPolicy control-plane objects;
//   - the OPTIONAL cluster applier: when a Kubernetes client is attached, those
//     policies are ALSO applied to the live cluster as networkingv1.NetworkPolicy
//     objects, making isolate/harden real data-plane enforcement (CNI drops the
//     traffic), not just recorded intent.
//
// It satisfies soc.Actuator and exposes Active() so GET /api/v1/soc/mitigations
// reports what automated responses actually did. Per-action Mode is honest:
// "real" only when enforcement genuinely takes effect now.
type networkPolicyActuator struct {
	gw      *security.Gateway
	npe     *security.NetworkPolicyEngine
	applier *security.NetworkPolicyApplier // optional; nil-safe (Available()==false)

	mu     sync.Mutex
	active map[string]soc.Mitigation
}

func newNetworkPolicyActuator(gw *security.Gateway, npe *security.NetworkPolicyEngine) *networkPolicyActuator {
	return &networkPolicyActuator{gw: gw, npe: npe, active: make(map[string]soc.Mitigation)}
}

// SetClusterApplier attaches a live-cluster NetworkPolicy applier. From then on
// isolate-host / harden-workload responses are applied to the data plane and
// honestly reported as Mode="real".
func (a *networkPolicyActuator) SetClusterApplier(applier *security.NetworkPolicyApplier) {
	a.mu.Lock()
	a.applier = applier
	a.mu.Unlock()
}

func (a *networkPolicyActuator) clusterApplier() *security.NetworkPolicyApplier {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applier
}

func (a *networkPolicyActuator) Name() string { return "network-policy" }

// IsReal reports whether a real data-plane enforcement path is active: either
// the gateway IP ACL genuinely rejects blocked sources, or a live-cluster
// NetworkPolicy applier is attached (isolate/harden reach the CNI).
func (a *networkPolicyActuator) IsReal() bool {
	if a.gw != nil && a.gw.IPACLEnabled() {
		return true
	}
	return a.clusterApplier().Available()
}

func (a *networkPolicyActuator) record(action soc.ActionType, target string) {
	a.mu.Lock()
	a.active[string(action)+"\x00"+target] = soc.Mitigation{Action: action, Target: target, Since: time.Now().UTC()}
	a.mu.Unlock()
}

// Actuate executes one response action through the real subsystems.
func (a *networkPolicyActuator) Actuate(ctx context.Context, action soc.ActionType, target string) soc.ActuationResult {
	switch action {
	case soc.ActionBlockNetwork:
		enforced := false
		if a.gw != nil {
			enforced = a.gw.BlockIP(target)
		}
		a.record(action, target)
		if enforced {
			return soc.ActuationResult{Action: action, Target: target, Mode: "real", Executed: true,
				Detail: "IP blocked at gateway; subsequent requests from it are rejected"}
		}
		return soc.ActuationResult{Action: action, Target: target, Mode: "simulated", Executed: true,
			Detail: "IP added to gateway block list (enforcement inactive: IP ACL disabled)"}
	case soc.ActionIsolateHost, soc.ActionHardenWorkload:
		var policy *security.NetworkPolicySpec
		if a.npe != nil {
			policy = a.npe.EnforceIsolation("default", string(action)+"-"+target, map[string]string{"app": target})
		}
		a.record(action, target)
		// Close the loop at the data plane: apply the deny-by-default policy to
		// the live cluster when an applier is attached. Only a successful apply
		// earns Mode="real" — an attach without a working cluster stays honest.
		if applier := a.clusterApplier(); applier.Available() && policy != nil {
			applied, err := applier.Apply(ctx, policy)
			if err == nil {
				return soc.ActuationResult{Action: action, Target: target, Mode: "real", Executed: true,
					Detail: fmt.Sprintf("deny-by-default NetworkPolicy %q applied to cluster (id=%s); CNI enforces isolation", applied, policy.ID)}
			}
			return soc.ActuationResult{Action: action, Target: target, Mode: "simulated", Executed: true,
				Detail: fmt.Sprintf("active NetworkPolicy created (id=%s) but cluster apply failed: %v", policy.ID, err)}
		}
		id := ""
		if policy != nil {
			id = policy.ID
		}
		return soc.ActuationResult{Action: action, Target: target, Mode: "simulated", Executed: true,
			Detail: fmt.Sprintf("active deny-by-default NetworkPolicy created (id=%s); data-plane apply needs a cluster client", id)}
	case soc.ActionNotify:
		return soc.ActuationResult{Action: action, Target: target, Mode: "simulated", Executed: true, Detail: "operator notified"}
	default:
		return soc.ActuationResult{Action: action, Target: target, Mode: "simulated", Executed: false,
			Detail: "no network-policy executor for this action (needs endpoint/identity/registry backend)"}
	}
}

// Active returns the mitigations this actuator currently holds.
func (a *networkPolicyActuator) Active() []soc.Mitigation {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]soc.Mitigation, 0, len(a.active))
	for _, m := range a.active {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target == out[j].Target {
			return out[i].Action < out[j].Action
		}
		return out[i].Target < out[j].Target
	})
	return out
}
