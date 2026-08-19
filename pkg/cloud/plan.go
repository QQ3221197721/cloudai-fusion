// plan.go — Module 2 cost planning: the PlanEngine aggregates the hardcoded
// GPU pricing tables exposed by every registered provider (GetGPUPricing) into
// a cost-ascending, multi-cloud comparison. Because the tables are static,
// planning works with zero credentials — the developer-experience core: you
// can price a 4×A100 cluster across 6 clouds before owning a single API key.
// Every option is honestly marked "plan-only, no cloud API calls".
package cloud

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ============================================================================
// Resource Spec & Plan Types
// ============================================================================

// ResourceSpec describes the GPU cluster shape being planned.
type ResourceSpec struct {
	GPUType           string `json:"gpu_type"`             // e.g. "nvidia-a100"
	GPUNodes          int    `json:"gpu_nodes"`            // number of GPU nodes (>0)
	Region            string `json:"region,omitempty"`     // preferred region ("" = any)
	DurationHours     int    `json:"duration_hours"`       // rental window for TotalCost (>0)
	KubernetesVersion string `json:"k8s_version,omitempty"` // e.g. "1.28"
}

// Validate rejects meaningless specs up front (fail loud, never coerce).
func (s *ResourceSpec) Validate() error {
	if strings.TrimSpace(s.GPUType) == "" {
		return fmt.Errorf("cloud: gpu_type is required")
	}
	if s.GPUNodes <= 0 {
		return fmt.Errorf("cloud: gpu_nodes must be positive (got %d)", s.GPUNodes)
	}
	if s.DurationHours <= 0 {
		return fmt.Errorf("cloud: duration_hours must be positive (got %d)", s.DurationHours)
	}
	return nil
}

// PlanOption is one provider's offer for a ResourceSpec.
type PlanOption struct {
	Provider     string `json:"provider"`               // provider name
	ProviderType string `json:"provider_type"`          // provider type enum string
	Region       string `json:"region"`
	InstanceType string `json:"instance_type,omitempty"` // best-effort match from ListGPUInstances
	GPUType      string `json:"gpu_type"`
	HourlyCost   float64 `json:"hourly_cost"`            // OnDemandPrice × nodes
	MonthlyCost  float64 `json:"monthly_cost"`           // HourlyCost × 730 (avg month)
	TotalCost    float64 `json:"total_cost"`             // HourlyCost × DurationHours
	Currency     string `json:"currency"`                // native currency of the price table
	Availability string `json:"availability"`            // "plan-only" | "credential-required"
	Pros         []string `json:"pros,omitempty"`
}

// PlanAvailability values. Planning never touches cloud APIs — every option is
// plan-only. credential-required marks providers whose live verification
// (actual provisioning) would need real credentials.
const (
	PlanAvailabilityPlanOnly  = "plan-only"
	PlanAvailabilityCredReqd  = "credential-required"
)

// hoursPerMonth is the standard cloud-billing convention for monthly estimates.
const hoursPerMonth = 730.0

// ============================================================================
// PlanEngine
// ============================================================================

// PlanEngine generates multi-cloud cost plans from the static pricing tables.
// It performs no cloud API calls that require credentials.
type PlanEngine struct{}

// NewPlanEngine returns a ready engine.
func NewPlanEngine() *PlanEngine { return &PlanEngine{} }

// Generate queries every registered provider's hardcoded pricing table and
// returns options sorted by TotalCost ascending (native-currency values; each
// option carries its Currency). Providers are never skipped for missing
// credentials — the tables are static, so all registered providers appear.
func (e *PlanEngine) Generate(ctx context.Context, m *Manager, spec ResourceSpec) ([]*PlanOption, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	providers := m.ListProviders()
	options := make([]*PlanOption, 0, len(providers))

	for _, p := range providers {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		pricing, err := p.GetGPUPricing(pctx, spec.GPUType)
		cancel()
		if err != nil || pricing == nil {
			// Static table unreachable is a provider defect; plan around it.
			continue
		}

		opt := &PlanOption{
			Provider:     p.Name(),
			ProviderType: string(p.Type()),
			Region:       p.Region(),
			GPUType:      spec.GPUType,
			HourlyCost:   pricing.OnDemandPrice * float64(spec.GPUNodes),
			MonthlyCost:  pricing.OnDemandPrice * float64(spec.GPUNodes) * hoursPerMonth,
			TotalCost:    pricing.OnDemandPrice * float64(spec.GPUNodes) * float64(spec.DurationHours),
			Currency:     pricing.Currency,
			Availability: PlanAvailabilityPlanOnly,
			InstanceType: e.bestInstanceFor(p, ctx, spec),
			Pros:         providerPros(string(p.Type()), pricing),
		}
		options = append(options, opt)
	}

	// Cost ascending, stable so equal-cost providers keep registration order.
	sort.SliceStable(options, func(i, j int) bool {
		return options[i].TotalCost < options[j].TotalCost
	})

	// Region preference: when the caller pinned a region, hoist exact matches
	// above the rest (cost still decides within each group).
	if spec.Region != "" {
		sort.SliceStable(options, func(i, j int) bool {
			iMatch := strings.EqualFold(options[i].Region, spec.Region)
			jMatch := strings.EqualFold(options[j].Region, spec.Region)
			if iMatch != jMatch {
				return iMatch
			}
			return options[i].TotalCost < options[j].TotalCost
		})
	}

	return options, nil
}

// bestInstanceFor finds a concrete instance type from the provider's static
// catalog whose GPUType matches the spec — cosmetic context for the plan row.
func (e *PlanEngine) bestInstanceFor(p Provider, ctx context.Context, spec ResourceSpec) string {
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	instances, err := p.ListGPUInstances(pctx)
	if err != nil {
		return ""
	}
	for _, inst := range instances {
		if strings.EqualFold(inst.GPUType, spec.GPUType) {
			return inst.InstanceType
		}
	}
	return ""
}

// providerPros returns a short static pitch per provider type plus spot hints.
func providerPros(providerType string, pricing *GPUPricing) []string {
	var pros []string
	switch providerType {
	case "aliyun":
		pros = append(pros, "China-mainland optimized", "ACK deep integration")
	case "aws":
		pros = append(pros, "Broadest global regions", "EKS mature ecosystem")
	case "azure":
		pros = append(pros, "Enterprise Microsoft stack", "AKS hybrid networking")
	case "gcp":
		pros = append(pros, "TPU/GPU hybrid catalog", "GKE autopilot")
	case "huawei":
		pros = append(pros, "Ascend domestic option", "CCE compliance focus")
	case "tencent":
		pros = append(pros, "Greater-China latency", "TKE gaming pedigree")
	default:
		pros = append(pros, "Unified cafctl interface")
	}
	if pricing.SpotPrice > 0 {
		pros = append(pros, fmt.Sprintf("spot from %.2f/hr", pricing.SpotPrice))
	}
	if pricing.ReservedPrice > 0 {
		pros = append(pros, fmt.Sprintf("reserved %.2f/hr", pricing.ReservedPrice))
	}
	return pros
}
