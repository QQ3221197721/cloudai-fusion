// Package cost provides resource cost calculation, budget alerts, and
// optimization recommendations for cloud infrastructure (GPU/CPU instances).
package cost

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// PricingModel defines per-unit rates for a specific provider/instance type.
type PricingModel struct {
	Provider    string // "aws", "azure", "gcp"
	InstanceID  string // e.g. "nvidia-a100-80gb", "h100-80gb", "l40s"
	InstanceType string // machine-readable identifier like "a100-80gb-aws"
	CostPerGPUPerHour   float64
	CostPerCPUPerHour   float64
	CostPerGBStoragePerHour float64
	CostPerNetworkEgressPerGB float64 // outbound GB charges
}

// CostReport aggregates costs over a time range.
type CostReport struct {
	ClusterID        string
	TimeRangeStart   time.Time
	TimeRangeEnd     time.Time
	GPUCost          float64
	VCpuCost         float64
	StorageCost      float64
	NetworkCost      float64
	TotalCost        float64
	BudgetStatus     BudgetStatus
	Recommendations  []OptimizationRecommendation

	// Receipt optionally carries a signed, offline-verifiable attestation of
	// this cost claim (populated by EvidenceCostEngine.CalculateCost).
	Receipt          *evidence.Receipt `json:"receipt,omitempty"`
}

// BudgetStatus classifies current spend relative to configured thresholds.
type BudgetStatus int

const (
	BudgetNormal BudgetStatus = iota
	BudgetNearThreshold
	BudgetExceeded
	BudgetCritical
)

func (b BudgetStatus) String() string {
	switch b {
	case BudgetNormal:
		return "normal"
	case BudgetNearThreshold:
		return "near-threshold"
	case BudgetExceeded:
		return "exceeded"
	case BudgetCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// OptimizationRecommendation suggests cost-saving actions.
type OptimizationRecommendation struct {
	Type       string // "right-sizing", "spot", "reserved", "storage-tiering"
	Priority   int    // 1=highest
	SavingsUSD float64 // estimated monthly savings
	Desc       string // human description
}

// CostCalculator performs cost computations given resources and pricing.
type CostCalculator struct {
	mu            sync.RWMutex
	pricingModels []PricingModel
	budgets       []BudgetAlert
	repo          PricingRepository
}

// PricingRepository supplies real pricing data from external sources.
type PricingRepository interface {
	FetchPricing() ([]PricingModel, error)
}

// InMemoryPricingRepo is an in-memory pricing repository populated with real
// public instance rates (approximate as of late 2025).
type InMemoryPricingRepo struct {
	models []PricingModel
}

// NewInMemoryPricingRepo returns a pricing repository with realistic GPU prices.
func NewInMemoryPricingRepo() *InMemoryPricingRepo {
	// Prices are approximated from public lists (AWS/Azure/GCP on-demand hourly per unit).
	return &InMemoryPricingRepo{models: []PricingModel{
		// AWS H100/NVIDIA H100 S8: ~$39/hr GPU; H100 per hour ~$39
		{Provider: "aws", InstanceID: "nvidia-h100-80gb", InstanceType: "h100-80gb-aws",
			CostPerGPUPerHour:   39.0,
			CostPerCPUPerHour:   0.08,
			CostPerGBStoragePerHour: 0.0001,
			CostPerNetworkEgressPerGB: 0.09},
		// NVIDIA A100 80GB AWS: ~$7.6/hr GPU
		{Provider: "aws", InstanceID: "nvidia-a100-80gb", InstanceType: "a100-80gb-aws",
			CostPerGPUPerHour:   7.56,
			CostPerCPUPerHour:   0.08,
			CostPerGBStoragePerHour: 0.0001,
			CostPerNetworkEgressPerGB: 0.09},
		// NVIDIA L40S: ~$6.4/hr GPU
		{Provider: "aws", InstanceID: "nvidia-l40s", InstanceType: "l40s-aws",
			CostPerGPUPerHour:   6.36,
			CostPerCPUPerHour:   0.08,
			CostPerGBStoragePerHour: 0.0001,
			CostPerNetworkEgressPerGB: 0.09},
		// Azure H100/NC40DS v5: ~$36/hr GPU
		{Provider: "azure", InstanceID: "nvidia-h100-80gb", InstanceType: "h100-80gb-azure",
			CostPerGPUPerHour:   36.0,
			CostPerCPUPerHour:   0.08,
			CostPerGBStoragePerHour: 0.00012,
			CostPerNetworkEgressPerGB: 0.09},
		// Azure A100: ~$7.5/hr GPU
		{Provider: "azure", InstanceID: "nvidia-a100-80gb", InstanceType: "a100-80gb-azure",
			CostPerGPUPerHour:   7.50,
			CostPerCPUPerHour:   0.07,
			CostPerGBStoragePerHour: 0.00012,
			CostPerNetworkEgressPerGB: 0.09},
		// GCP H100 VM: ~$36/hr GPU
		{Provider: "gcp", InstanceID: "nvidia-h100-80gb", InstanceType: "h100-80gb-gcp",
			CostPerGPUPerHour:   36.0,
			CostPerCPUPerHour:   0.08,
			CostPerGBStoragePerHour: 0.0001,
			CostPerNetworkEgressPerGB: 0.12},
		// GCP A100: ~$6/hr GPU
		{Provider: "gcp", InstanceID: "nvidia-a100-80gb", InstanceType: "a100-80gb-gcp",
			CostPerGPUPerHour:   6.00,
			CostPerCPUPerHour:   0.08,
			CostPerGBStoragePerHour: 0.0001,
			CostPerNetworkEgressPerGB: 0.12},
	}}
}

// FetchPricing implements PricingRepository.
func (r *InMemoryPricingRepo) FetchPricing() ([]PricingModel, error) {
	return r.models, nil
}

// NewCostCalculator creates a calculator with optional pricing repository.
func NewCostCalculator(repo PricingRepository) *CostCalculator {
	c := &CostCalculator{repo: repo}
	if repo != nil {
		if models, err := repo.FetchPricing(); err == nil && len(models) > 0 {
			c.mu.Lock()
			c.pricingModels = models
			c.mu.Unlock()
		}
	}
	return c
}

// AddPricingModel registers a pricing model explicitly.
func (c *CostCalculator) AddPricingModel(m PricingModel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pricingModels = append(c.pricingModels, m)
}

// SetBudgets configures one or more budget alert thresholds.
func (c *CostCalculator) SetBudgets(budgets []BudgetAlert) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.budgets = budgets
}

// CalculateClusterCost computes the cost report for a cluster over a time range.
// It assumes resources remain constant; pass ResourceSnapshot to override per
// instance counts.
func (c *CostCalculator) CalculateClusterCost(clusterID string, tr TimeRange) CostReport {
	c.mu.RLock()
	defer c.mu.RUnlock()

	report := CostReport{
		ClusterID:      clusterID,
		TimeRangeStart: tr.Start,
		TimeRangeEnd:   tr.End,
	}

	hours := tr.DurationHours()
	totalGPUs := 0
	totalVCPUs := 0
	tags := make(map[string]string)

	for _, r := range tr.Resources {
		hoursForResource := hours
		if !r.Start.IsZero() && !r.End.IsZero() && r.End.Sub(r.Start).Seconds() < hours*3600 {
			hoursForResource = max(0.0, r.End.Sub(r.Start).Hours())
		}
		if hoursForResource <= 0 {
			continue
		}
		for _, inst := range r.Instances {
			if inst.GPUCount == 0 {
				inst.GPUCount = 1
			}
			gpuCost := float64(inst.GPUCount) * inst.HoursFraction * hoursForResource * c.gpuPrice(inst.InstanceID, inst.Provider)
			vcpuCost := float64(inst.VCPUCount) * inst.HoursFraction * hoursForResource * c.cpuPrice(inst.Provider)
			storageCost := inst.StorageGB * inst.HoursFraction * hoursForResource * storagePrice
			totalGPUs += int(float64(inst.GPUCount) * inst.HoursFraction)
			totalVCPUs += int(float64(inst.VCPUCount) * inst.HoursFraction)
			report.GPUCost += gpuCost
			report.VCpuCost += vcpuCost
			report.StorageCost += storageCost
			for k, v := range inst.Tags {
				tags[k] = v
			}
		}
	}
	egress := tr.EgressGB / 100 // approximate division factor for simplicity
	report.NetworkCost = egress * networkEgressAvg

	report.TotalCost = report.GPUCost + report.VCpuCost + report.StorageCost + report.NetworkCost

	report.BudgetStatus = c.evaluateBudget(report.TotalCost)
	report.Recommendations = c.generateRecommendations(&tr, totalGPUs, totalVCPUs)

	return report
}

func (c *CostCalculator) gpuPrice(instanceID, provider string) float64 {
	for _, p := range c.pricingModels {
		if p.InstanceID == instanceID || p.InstanceType == instanceID {
			return p.CostPerGPUPerHour
		}
	}
	// Fallback heuristic based on instance prefix if not exact match
	if len(instanceID) >= 2 {
		id := instanceID[:2]
		switch id {
		case "H1":
			return 39.0
		case "A1", "A0":
			return 7.56
		case "L4":
			return 6.36
		default:
			return 7.0
		}
	}
	return 7.0
}

func (c *CostCalculator) cpuPrice(provider string) float64 {
	var fallback = 0.08
	for _, p := range c.pricingModels {
		if p.Provider == provider {
			return p.CostPerCPUPerHour
		}
	}
	return fallback
}

var storagePrice = 0.0001
var networkEgressAvg = 0.09

func (c *CostCalculator) evaluateBudget(total float64) BudgetStatus {
	// Caller (CalculateClusterCost) already holds c.mu.RLock; do not re-lock.
	var closest float64
	minDiff := math.MaxFloat64
	found := false
	for _, b := range c.budgets {
		if !b.Enabled {
			continue
		}
		diff := abs(b.Threshold - total)
		if diff < minDiff {
			minDiff = diff
			closest = b.Threshold
			found = true
		}
	}
	if !found {
		return BudgetNormal
	}
	ratio := total / max(1e-6, closest)
	switch {
	case ratio < 0.8:
		return BudgetNormal
	case ratio < 0.95:
		return BudgetNearThreshold
	case ratio < 1.2:
		return BudgetExceeded
	default:
		return BudgetCritical
	}
}

func (c *CostCalculator) generateRecommendations(tr *TimeRange, totalGPUs, totalVCPUs int) []OptimizationRecommendation {
	if totalGPUs == 0 {
		return nil
	}
	recs := make([]OptimizationRecommendation, 0, 3)
	// Spot replacement recommendation
	if spotSavings := float64(totalGPUs) * 360 * 0.6; spotSavings > 0 {
		recs = append(recs, OptimizationRecommendation{
			Type:       "spot",
			Priority:   1,
			SavingsUSD: spotSavings,
			Desc:       fmt.Sprintf("Consider using preemptible/spot instances for fault-tolerant workloads. Estimated savings: $%.0f/mo", spotSavings),
		})
	}
	// Reserved capacity recommendation
	if reservedSavings := float64(totalGPUs) * 360 * 0.3; reservedSavings > 0 {
		recs = append(recs, OptimizationRecommendation{
			Type:       "reserved",
			Priority:   2,
			SavingsUSD: reservedSavings,
			Desc:       fmt.Sprintf("Reserved capacity for stable workloads can save ~30%%. Savings estimate: $%.0f/mo", reservedSavings),
		})
	}
	// Right-sizing suggestion for idle GPUs
	if idleSavings := float64(totalGPUs) * 10.0; idleSavings > 0 {
		recs = append(recs, OptimizationRecommendation{
			Type:       "right-sizing",
			Priority:   3,
			SavingsUSD: idleSavings,
			Desc:       "Right-size underutilized GPU instances to reduce waste.",
		})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Priority < recs[j].Priority })
	return recs
}

// BudgetAlert wraps an alert threshold that triggers notifications when exceeded.
type BudgetAlert struct {
	Name         string
	Threshold    float64
	Channels     []string // email/slack etc.
	Enabled      bool
}

// TimeRange specifies a time window for calculations.
type TimeRange struct {
	Start         time.Time
	End           time.Time
	Resources     []ResourceSnapshot
	EgressGB      float64 // total outbound network GB over this period
}

// DurationHours returns the span in hours.
func (tr TimeRange) DurationHours() float64 {
	d := tr.End.Sub(tr.Start)
	if d < 0 {
		d = 0
	}
	return d.Hours()
}

// ResourceSnapshot captures the state of resources at a point in time.
type ResourceSnapshot struct {
	Instances    []InstanceUsage
	Start        time.Time
	End          time.Time
	ComputeHours float64 // pre-computed compute hours
}

// InstanceUsage describes running workloads on a single machine.
type InstanceUsage struct {
	InstanceID     string
	Provider       string
	GPUCount       int
	VCPUCount      int
	StorageGB      float64
	HoursFraction  float64 // fraction of full hours used (0–1)
	Tags           map[string]string
}

// abs returns the absolute value of a float64.
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
