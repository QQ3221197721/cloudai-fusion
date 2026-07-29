// Package security - npapplier.go closes the L8 response loop at the data plane.
// NetworkPolicyApplier converts the engine's NetworkPolicySpec control-plane
// objects into real networkingv1.NetworkPolicy resources and applies them to a
// live Kubernetes cluster through client-go. This is the "cluster reconciler"
// the SOAR actuator needs so isolate-host / harden-workload responses are REAL
// enforcement (kube-proxy/CNI drops the traffic), not just recorded intent.
package security

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// appliedByLabel marks policies created by the SOAR response path so operators
// can list (and clean up) automated mitigations with a single selector.
const appliedByLabel = "cloudai.fusion/applied-by"

// appliedByValue is the label value identifying SOAR-applied policies.
const appliedByValue = "soar-actuator"

// NetworkPolicyApplier applies NetworkPolicySpec objects to a real cluster.
// A nil *NetworkPolicyApplier is safe to use and reports itself unavailable,
// so callers can hold an optional applier without nil-guarding every call.
type NetworkPolicyApplier struct {
	client  kubernetes.Interface
	timeout time.Duration
}

// NewNetworkPolicyApplier wraps a Kubernetes clientset. The clientset may come
// from in-cluster config, a kubeconfig, or (in tests) k8s.io/client-go/kubernetes/fake.
func NewNetworkPolicyApplier(client kubernetes.Interface) *NetworkPolicyApplier {
	if client == nil {
		return nil
	}
	return &NetworkPolicyApplier{client: client, timeout: 15 * time.Second}
}

// Available reports whether a cluster client is attached. It is nil-safe:
// a nil applier simply means "no data-plane path configured".
func (a *NetworkPolicyApplier) Available() bool {
	return a != nil && a.client != nil
}

// Apply converts the spec to a networkingv1.NetworkPolicy and creates it in the
// cluster (update-on-conflict). Returns the applied object's name.
func (a *NetworkPolicyApplier) Apply(ctx context.Context, spec *NetworkPolicySpec) (string, error) {
	if !a.Available() {
		return "", fmt.Errorf("networkpolicy applier: no cluster client attached")
	}
	if spec == nil {
		return "", fmt.Errorf("networkpolicy applier: nil spec")
	}
	ns := spec.Namespace
	if ns == "" {
		ns = "default"
	}
	np := toNetworkingV1(spec, ns)

	cctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	created, err := a.client.NetworkingV1().NetworkPolicies(ns).Create(cctx, np, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		// Update in place: automated re-response to the same target must converge,
		// not fail. Fetch to preserve resourceVersion for the update.
		existing, getErr := a.client.NetworkingV1().NetworkPolicies(ns).Get(cctx, np.Name, metav1.GetOptions{})
		if getErr != nil {
			return "", fmt.Errorf("networkpolicy applier: get existing %q: %w", np.Name, getErr)
		}
		np.ResourceVersion = existing.ResourceVersion
		updated, updErr := a.client.NetworkingV1().NetworkPolicies(ns).Update(cctx, np, metav1.UpdateOptions{})
		if updErr != nil {
			return "", fmt.Errorf("networkpolicy applier: update %q: %w", np.Name, updErr)
		}
		return updated.Name, nil
	}
	if err != nil {
		return "", fmt.Errorf("networkpolicy applier: create %q: %w", np.Name, err)
	}
	return created.Name, nil
}

// Remove deletes a previously applied policy (mitigation rollback).
func (a *NetworkPolicyApplier) Remove(ctx context.Context, namespace, name string) error {
	if !a.Available() {
		return fmt.Errorf("networkpolicy applier: no cluster client attached")
	}
	if namespace == "" {
		namespace = "default"
	}
	cctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	err := a.client.NetworkingV1().NetworkPolicies(namespace).Delete(cctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("networkpolicy applier: delete %q: %w", name, err)
	}
	return nil
}

// ListApplied returns the names of policies this applier (or a previous run of
// it) created, identified by the applied-by label.
func (a *NetworkPolicyApplier) ListApplied(ctx context.Context, namespace string) ([]string, error) {
	if !a.Available() {
		return nil, fmt.Errorf("networkpolicy applier: no cluster client attached")
	}
	if namespace == "" {
		namespace = "default"
	}
	cctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	list, err := a.client.NetworkingV1().NetworkPolicies(namespace).List(cctx, metav1.ListOptions{
		LabelSelector: appliedByLabel + "=" + appliedByValue,
	})
	if err != nil {
		return nil, fmt.Errorf("networkpolicy applier: list: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].Name)
	}
	return names, nil
}

// Connected probes the cluster with a cheap read so IsReal claims are backed by
// an actual reachability check, not just "a client object exists".
func (a *NetworkPolicyApplier) Connected(ctx context.Context) bool {
	if !a.Available() {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := a.client.Discovery().ServerVersion()
	if err != nil {
		// Fallback: fake clientsets in tests have no discovery server; a
		// namespaced list against the API is an equivalent liveness probe.
		_, lerr := a.client.NetworkingV1().NetworkPolicies("default").List(cctx, metav1.ListOptions{Limit: 1})
		return lerr == nil
	}
	return true
}

// toNetworkingV1 converts the platform's NetworkPolicySpec into the official
// networkingv1.NetworkPolicy object. Empty Ingress AND Egress means deny-all
// in both directions (the isolation posture EnforceIsolation produces).
func toNetworkingV1(spec *NetworkPolicySpec, namespace string) *networkingv1.NetworkPolicy {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sanitizePolicyName(spec.Name),
			Namespace: namespace,
			Labels: map[string]string{
				appliedByLabel:               appliedByValue,
				"cloudai.fusion/policy-id":   spec.ID,
				"cloudai.fusion/policy-type": spec.Source,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: spec.Selector},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}

	// Ingress: no rules with PolicyTypes=[Ingress] = deny-all ingress; each
	// NetworkRule becomes one allow rule.
	for _, r := range spec.Ingress {
		np.Spec.Ingress = append(np.Spec.Ingress, networkingv1.NetworkPolicyIngressRule{
			From:  ruleToPeers(r.FromLabels, r.FromCIDR),
			Ports: ruleToPorts(r.Ports),
		})
	}
	for _, r := range spec.Egress {
		np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
			To:    ruleToPeers(r.ToLabels, r.ToCIDR),
			Ports: ruleToPorts(r.Ports),
		})
	}
	return np
}

func ruleToPeers(labels map[string]string, cidrs []string) []networkingv1.NetworkPolicyPeer {
	var peers []networkingv1.NetworkPolicyPeer
	if len(labels) > 0 {
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			PodSelector: &metav1.LabelSelector{MatchLabels: labels},
		})
	}
	for _, cidr := range cidrs {
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr},
		})
	}
	return peers
}

func ruleToPorts(ports []PolicyPort) []networkingv1.NetworkPolicyPort {
	var out []networkingv1.NetworkPolicyPort
	for _, p := range ports {
		proto := corev1.ProtocolTCP
		if p.Protocol == "UDP" || p.Protocol == "udp" {
			proto = corev1.ProtocolUDP
		}
		port := intstr.FromInt(p.Port)
		out = append(out, networkingv1.NetworkPolicyPort{Protocol: &proto, Port: &port})
	}
	return out
}

// sanitizePolicyName makes a spec name a valid RFC 1123 subdomain (K8s object
// name): lowercase alphanumerics and '-', max 253 chars, must start/end with
// an alphanumeric.
func sanitizePolicyName(name string) string {
	if name == "" {
		return "soar-policy"
	}
	b := make([]byte, 0, len(name))
	for i := 0; i < len(name) && len(b) < 253; i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b = append(b, c)
		case c >= 'A' && c <= 'Z':
			b = append(b, c+('a'-'A'))
		case c == '-', c == '.', c == '_', c == ' ', c == '/':
			if len(b) > 0 && b[len(b)-1] != '-' {
				b = append(b, '-')
			}
		}
	}
	// Trim trailing '-'
	for len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	if len(b) == 0 {
		return "soar-policy"
	}
	return string(b)
}
