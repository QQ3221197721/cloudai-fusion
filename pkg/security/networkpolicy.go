// Package security - networkpolicy.go provides network policy automation.
// Analyzes traffic patterns to generate Cilium/Kubernetes NetworkPolicy resources,
// supports micro-segmentation, traffic flow visualization, and webhook-based
// policy enforcement with automatic policy recommendations.
package security

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Traffic Flow Model
// ============================================================================

// TrafficFlow represents an observed network communication.
type TrafficFlow struct {
	ID          string            `json:"id"`
	SourcePod   string            `json:"source_pod"`
	SourceNS    string            `json:"source_ns"`
	SourceLabels map[string]string `json:"source_labels,omitempty"`
	DestPod     string            `json:"dest_pod"`
	DestNS      string            `json:"dest_ns"`
	DestLabels  map[string]string `json:"dest_labels,omitempty"`
	Port        int               `json:"port"`
	Protocol    string            `json:"protocol"` // TCP, UDP
	BytesTotal  int64             `json:"bytes_total"`
	RequestCount int64            `json:"request_count"`
	LastSeen    time.Time         `json:"last_seen"`
	Encrypted   bool              `json:"encrypted"`
}

// FlowKey returns a unique identifier for this flow direction.
func (f TrafficFlow) FlowKey() string {
	return fmt.Sprintf("%s/%s→%s/%s:%d/%s",
		f.SourceNS, f.SourcePod, f.DestNS, f.DestPod, f.Port, f.Protocol)
}

// ============================================================================
// Network Policy Generator
// ============================================================================

// NetworkPolicySpec represents a generated Kubernetes/Cilium NetworkPolicy.
type NetworkPolicySpec struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Namespace   string             `json:"namespace"`
	Type        string             `json:"type"` // kubernetes, cilium
	Selector    map[string]string  `json:"selector"`
	Ingress     []NetworkRule      `json:"ingress,omitempty"`
	Egress      []NetworkRule      `json:"egress,omitempty"`
	Status      string             `json:"status"` // draft, active, audit
	GeneratedAt time.Time          `json:"generated_at"`
	AppliedAt   *time.Time         `json:"applied_at,omitempty"`
	Source      string             `json:"source"` // auto-generated, manual, webhook
}

// NetworkRule defines an allow rule.
type NetworkRule struct {
	FromLabels map[string]string `json:"from_labels,omitempty"`
	ToLabels   map[string]string `json:"to_labels,omitempty"`
	FromCIDR   []string          `json:"from_cidr,omitempty"`
	ToCIDR     []string          `json:"to_cidr,omitempty"`
	Ports      []PolicyPort      `json:"ports,omitempty"`
}

// PolicyPort defines a port in a network policy.
type PolicyPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// ============================================================================
// Micro-Segmentation Zone
// ============================================================================

// SegmentationZone defines a micro-segmentation boundary.
type SegmentationZone struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Namespaces  []string          `json:"namespaces"`
	Labels      map[string]string `json:"labels"`
	TrustLevel  string            `json:"trust_level"` // untrusted, low, medium, high, critical
	AllowedZones []string         `json:"allowed_zones,omitempty"`
	DenyByDefault bool            `json:"deny_by_default"`
}

// ============================================================================
// Network Policy Engine
// ============================================================================

// NetworkPolicyEngine automates network policy generation and management.
type NetworkPolicyEngine struct {
	flows    []*TrafficFlow
	policies []*NetworkPolicySpec
	zones    []*SegmentationZone
	logger   *logrus.Logger
	mu       sync.RWMutex
}

// NetworkPolicyEngineConfig configures the network policy engine.
type NetworkPolicyEngineConfig struct {
	Logger *logrus.Logger
}

// NewNetworkPolicyEngine creates a new network policy automation engine.
func NewNetworkPolicyEngine(cfg NetworkPolicyEngineConfig) *NetworkPolicyEngine {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	return &NetworkPolicyEngine{
		flows:    make([]*TrafficFlow, 0),
		policies: make([]*NetworkPolicySpec, 0),
		zones:    DefaultSegmentationZones(),
		logger:   cfg.Logger,
	}
}

// IngestFlow records an observed traffic flow for analysis.
func (e *NetworkPolicyEngine) IngestFlow(flow *TrafficFlow) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Update existing flow or add new
	for i, existing := range e.flows {
		if existing.FlowKey() == flow.FlowKey() {
			e.flows[i].BytesTotal += flow.BytesTotal
			e.flows[i].RequestCount += flow.RequestCount
			e.flows[i].LastSeen = flow.LastSeen
			return
		}
	}
	if flow.ID == "" {
		flow.ID = common.NewUUID()
	}
	e.flows = append(e.flows, flow)
}

// GetFlows returns all observed flows.
func (e *NetworkPolicyEngine) GetFlows() []*TrafficFlow {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*TrafficFlow, len(e.flows))
	copy(result, e.flows)
	return result
}

// GeneratePolicies analyzes traffic flows and generates recommended NetworkPolicies.
func (e *NetworkPolicyEngine) GeneratePolicies(ctx context.Context) []*NetworkPolicySpec {
	e.mu.RLock()
	flows := make([]*TrafficFlow, len(e.flows))
	copy(flows, e.flows)
	e.mu.RUnlock()

	if len(flows) == 0 {
		return nil
	}

	// Group flows by destination namespace + labels
	type destKey struct {
		ns     string
		labels string
	}
	groups := make(map[destKey][]*TrafficFlow)
	for _, f := range flows {
		key := destKey{ns: f.DestNS, labels: labelsToString(f.DestLabels)}
		groups[key] = append(groups[key], f)
	}

	var policies []*NetworkPolicySpec
	for key, groupFlows := range groups {
		policy := e.generatePolicyForGroup(key.ns, groupFlows)
		if policy != nil {
			policies = append(policies, policy)
		}
	}

	// Sort by namespace
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Namespace < policies[j].Namespace
	})

	e.mu.Lock()
	e.policies = append(e.policies, policies...)
	e.mu.Unlock()

	e.logger.WithField("generated", len(policies)).Info("Network policies generated from traffic analysis")
	return policies
}

// generatePolicyForGroup creates a NetworkPolicy for a set of flows targeting the same destination.
func (e *NetworkPolicyEngine) generatePolicyForGroup(ns string, flows []*TrafficFlow) *NetworkPolicySpec {
	if len(flows) == 0 {
		return nil
	}

	// Determine selector from first flow's dest labels
	selector := make(map[string]string)
	if flows[0].DestLabels != nil {
		for k, v := range flows[0].DestLabels {
			selector[k] = v
		}
	}
	if len(selector) == 0 {
		selector["app"] = flows[0].DestPod
	}

	// Build ingress rules from unique source → dest combinations
	type ingressKey struct {
		labels string
		port   int
		proto  string
	}
	ingressMap := make(map[ingressKey]*NetworkRule)
	for _, f := range flows {
		key := ingressKey{
			labels: labelsToString(f.SourceLabels),
			port:   f.Port,
			proto:  f.Protocol,
		}
		if _, ok := ingressMap[key]; !ok {
			rule := &NetworkRule{
				Ports: []PolicyPort{{Port: f.Port, Protocol: f.Protocol}},
			}
			if f.SourceLabels != nil && len(f.SourceLabels) > 0 {
				rule.FromLabels = f.SourceLabels
			} else {
				rule.FromLabels = map[string]string{"app": f.SourcePod}
			}
			ingressMap[key] = rule
		}
	}

	var ingress []NetworkRule
	for _, rule := range ingressMap {
		ingress = append(ingress, *rule)
	}

	return &NetworkPolicySpec{
		ID:          common.NewUUID(),
		Name:        fmt.Sprintf("auto-%s-%s", ns, selector["app"]),
		Namespace:   ns,
		Type:        "cilium",
		Selector:    selector,
		Ingress:     ingress,
		Status:      "draft",
		GeneratedAt: time.Now().UTC(),
		Source:      "auto-generated",
	}
}

// ============================================================================
// Zone Management
// ============================================================================

// AddZone adds a micro-segmentation zone.
func (e *NetworkPolicyEngine) AddZone(zone *SegmentationZone) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if zone.ID == "" {
		zone.ID = common.NewUUID()
	}
	e.zones = append(e.zones, zone)
	e.logger.WithFields(logrus.Fields{
		"zone": zone.Name, "trust_level": zone.TrustLevel,
	}).Info("Segmentation zone added")
}

// GetZones returns all segmentation zones.
func (e *NetworkPolicyEngine) GetZones() []*SegmentationZone {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*SegmentationZone, len(e.zones))
	copy(result, e.zones)
	return result
}

// GenerateZonePolicies creates NetworkPolicies to enforce zone boundaries.
func (e *NetworkPolicyEngine) GenerateZonePolicies() []*NetworkPolicySpec {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var policies []*NetworkPolicySpec
	for _, zone := range e.zones {
		if !zone.DenyByDefault {
			continue
		}
		for _, ns := range zone.Namespaces {
			policy := &NetworkPolicySpec{
				ID:          common.NewUUID(),
				Name:        fmt.Sprintf("zone-%s-%s", zone.Name, ns),
				Namespace:   ns,
				Type:        "cilium",
				Selector:    zone.Labels,
				Status:      "draft",
				GeneratedAt: time.Now().UTC(),
				Source:      "zone-enforcement",
			}

			// Allow traffic only from allowed zones
			for _, allowedZoneID := range zone.AllowedZones {
				for _, otherZone := range e.zones {
					if otherZone.ID == allowedZoneID || otherZone.Name == allowedZoneID {
						policy.Ingress = append(policy.Ingress, NetworkRule{
							FromLabels: otherZone.Labels,
						})
					}
				}
			}
			policies = append(policies, policy)
		}
	}
	return policies
}

// ListPolicies returns all generated policies.
func (e *NetworkPolicyEngine) ListPolicies() []*NetworkPolicySpec {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*NetworkPolicySpec, len(e.policies))
	copy(result, e.policies)
	return result
}

// ApprovePoliciy marks a draft policy as active.
func (e *NetworkPolicyEngine) ApprovePolicy(policyID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.policies {
		if p.ID == policyID {
			now := time.Now().UTC()
			p.Status = "active"
			p.AppliedAt = &now
			return nil
		}
	}
	return fmt.Errorf("policy %s not found", policyID)
}

// ============================================================================
// Webhook Extension Point
// ============================================================================

// PolicyWebhook defines a webhook for network policy validation/mutation.
type PolicyWebhook struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Type     string `json:"type"` // validating, mutating
	Enabled  bool   `json:"enabled"`
}

// WebhookResponse is the response from a policy webhook.
type WebhookResponse struct {
	Allowed  bool   `json:"allowed"`
	Reason   string `json:"reason,omitempty"`
	Mutation *NetworkPolicySpec `json:"mutation,omitempty"` // for mutating webhooks
}

// ============================================================================
// Status
// ============================================================================

// NetworkPolicyStatus reports the current state of network policy automation.
type NetworkPolicyStatus struct {
	TotalFlows      int `json:"total_flows"`
	TotalPolicies   int `json:"total_policies"`
	ActivePolicies  int `json:"active_policies"`
	DraftPolicies   int `json:"draft_policies"`
	Zones           int `json:"zones"`
}

// Status returns the current network policy engine status.
func (e *NetworkPolicyEngine) Status() NetworkPolicyStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	active, draft := 0, 0
	for _, p := range e.policies {
		switch p.Status {
		case "active":
			active++
		case "draft":
			draft++
		}
	}

	return NetworkPolicyStatus{
		TotalFlows:     len(e.flows),
		TotalPolicies:  len(e.policies),
		ActivePolicies: active,
		DraftPolicies:  draft,
		Zones:          len(e.zones),
	}
}

// ============================================================================
// Defaults
// ============================================================================

// DefaultSegmentationZones returns standard micro-segmentation zones.
func DefaultSegmentationZones() []*SegmentationZone {
	return []*SegmentationZone{
		{
			ID: "zone-system", Name: "system",
			Description: "Kubernetes system components",
			Namespaces:  []string{"kube-system", "kube-public"},
			Labels:      map[string]string{"zone": "system"},
			TrustLevel:  "critical",
			DenyByDefault: true,
			AllowedZones: []string{"zone-platform"},
		},
		{
			ID: "zone-platform", Name: "platform",
			Description: "CloudAI Fusion platform services",
			Namespaces:  []string{"cloudai-fusion", "monitoring"},
			Labels:      map[string]string{"zone": "platform"},
			TrustLevel:  "high",
			DenyByDefault: true,
			AllowedZones: []string{"zone-system", "zone-workload"},
		},
		{
			ID: "zone-workload", Name: "workload",
			Description: "User workloads and applications",
			Namespaces:  []string{"default", "production", "staging"},
			Labels:      map[string]string{"zone": "workload"},
			TrustLevel:  "medium",
			DenyByDefault: false,
			AllowedZones: []string{"zone-platform"},
		},
		{
			ID: "zone-external", Name: "external",
			Description: "External-facing services",
			Namespaces:  []string{"ingress", "gateway"},
			Labels:      map[string]string{"zone": "external"},
			TrustLevel:  "low",
			DenyByDefault: true,
			AllowedZones: []string{"zone-workload"},
		},
	}
}

// ============================================================================
// Helpers
// ============================================================================

func labelsToString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
