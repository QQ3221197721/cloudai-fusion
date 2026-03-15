package tenant

import (
	"fmt"
	"sort"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Enhanced Quota Tracking
// ============================================================================

// QuotaResourceType enumerates trackable resource types.
type QuotaResourceType string

const (
	QuotaCPU         QuotaResourceType = "cpu"
	QuotaMemory      QuotaResourceType = "memory"
	QuotaGPU         QuotaResourceType = "gpu"
	QuotaGPUMemory   QuotaResourceType = "gpu_memory"
	QuotaStorage     QuotaResourceType = "storage"
	QuotaPods        QuotaResourceType = "pods"
	QuotaServices    QuotaResourceType = "services"
	QuotaNamespaces  QuotaResourceType = "namespaces"
	QuotaClusters    QuotaResourceType = "clusters"
	QuotaBandwidth   QuotaResourceType = "bandwidth"
)

// QuotaEntry tracks a single resource quota with usage history.
type QuotaEntry struct {
	ResourceType QuotaResourceType `json:"resource_type"`
	HardLimit    int64             `json:"hard_limit"`
	SoftLimit    int64             `json:"soft_limit"`    // warning threshold
	CurrentUsage int64             `json:"current_usage"`
	PeakUsage    int64             `json:"peak_usage"`
	Unit         string            `json:"unit"` // millicores, bytes, count, mbps
	Enforced     bool              `json:"enforced"`
	LastUpdated  time.Time         `json:"last_updated"`
}

// UtilizationPercent returns the current usage as a percentage of hard limit.
func (q *QuotaEntry) UtilizationPercent() float64 {
	if q.HardLimit <= 0 {
		return 0
	}
	return float64(q.CurrentUsage) / float64(q.HardLimit) * 100.0
}

// IsExceeded returns true if current usage exceeds the hard limit.
func (q *QuotaEntry) IsExceeded() bool {
	return q.Enforced && q.CurrentUsage > q.HardLimit
}

// IsWarning returns true if current usage exceeds the soft limit.
func (q *QuotaEntry) IsWarning() bool {
	return q.SoftLimit > 0 && q.CurrentUsage > q.SoftLimit
}

// QuotaSnapshot captures the full quota state for a tenant at a point in time.
type QuotaSnapshot struct {
	TenantID    string                          `json:"tenant_id"`
	TenantName  string                          `json:"tenant_name"`
	Tier        TenantTier                      `json:"tier"`
	Resources   map[QuotaResourceType]*QuotaEntry `json:"resources"`
	CapturedAt  time.Time                       `json:"captured_at"`
}

// ============================================================================
// Cost Allocation
// ============================================================================

// CostAllocationReport represents a cost breakdown per tenant.
type CostAllocationReport struct {
	ID              string                `json:"id"`
	PeriodStart     time.Time             `json:"period_start"`
	PeriodEnd       time.Time             `json:"period_end"`
	GeneratedAt     time.Time             `json:"generated_at"`
	Currency        string                `json:"currency"`
	TenantCosts     []TenantCostSummary   `json:"tenant_costs"`
	TotalCost       float64               `json:"total_cost"`
	UnallocatedCost float64               `json:"unallocated_cost"` // shared infra
}

// TenantCostSummary summarizes costs for a single tenant.
type TenantCostSummary struct {
	TenantID     string              `json:"tenant_id"`
	TenantName   string              `json:"tenant_name"`
	Tier         TenantTier          `json:"tier"`
	ResourceCosts []ResourceCostItem `json:"resource_costs"`
	TotalCost    float64             `json:"total_cost"`
	CostPercent  float64             `json:"cost_percent"` // % of total platform cost
}

// ResourceCostItem breaks down cost for a single resource type.
type ResourceCostItem struct {
	ResourceType  QuotaResourceType `json:"resource_type"`
	UsageQuantity float64           `json:"usage_quantity"`
	Unit          string            `json:"unit"`
	UnitPrice     float64           `json:"unit_price"`
	TotalCost     float64           `json:"total_cost"`
}

// CostRate defines the billing rate for a resource type.
type CostRate struct {
	ResourceType QuotaResourceType `json:"resource_type"`
	UnitPrice    float64           `json:"unit_price"` // per unit per hour
	Unit         string            `json:"unit"`
	Currency     string            `json:"currency"`
}

// DefaultCostRates returns standard per-unit-per-hour pricing.
func DefaultCostRates() []CostRate {
	return []CostRate{
		{ResourceType: QuotaCPU, UnitPrice: 0.034, Unit: "core-hour", Currency: "USD"},
		{ResourceType: QuotaMemory, UnitPrice: 0.004, Unit: "GiB-hour", Currency: "USD"},
		{ResourceType: QuotaGPU, UnitPrice: 2.10, Unit: "gpu-hour", Currency: "USD"},
		{ResourceType: QuotaGPUMemory, UnitPrice: 0.14, Unit: "GiB-hour", Currency: "USD"},
		{ResourceType: QuotaStorage, UnitPrice: 0.00014, Unit: "GiB-hour", Currency: "USD"},
		{ResourceType: QuotaBandwidth, UnitPrice: 0.085, Unit: "GB", Currency: "USD"},
	}
}

// ============================================================================
// Quota Manager Methods (extends Manager)
// ============================================================================

// GetQuotaSnapshot returns a detailed quota snapshot for a tenant.
func (m *Manager) GetQuotaSnapshot(tenantID string) (*QuotaSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return nil, fmt.Errorf("tenant %q not found", tenantID)
	}

	usage, ok := m.usage[tenantID]
	if !ok {
		usage = &QuotaUsage{TenantID: tenantID}
	}

	snap := &QuotaSnapshot{
		TenantID:   tenantID,
		TenantName: tenant.Name,
		Tier:       tenant.Tier,
		CapturedAt: common.NowUTC(),
		Resources:  make(map[QuotaResourceType]*QuotaEntry),
	}

	// CPU
	snap.Resources[QuotaCPU] = &QuotaEntry{
		ResourceType: QuotaCPU,
		HardLimit:    tenant.Quota.MaxCPUMillicores,
		SoftLimit:    int64(float64(tenant.Quota.MaxCPUMillicores) * 0.8),
		CurrentUsage: usage.CPUMillicores,
		PeakUsage:    usage.CPUMillicores, // simplified
		Unit:         "millicores",
		Enforced:     true,
		LastUpdated:  usage.CollectedAt,
	}

	// Memory
	snap.Resources[QuotaMemory] = &QuotaEntry{
		ResourceType: QuotaMemory,
		HardLimit:    tenant.Quota.MaxMemoryBytes,
		SoftLimit:    int64(float64(tenant.Quota.MaxMemoryBytes) * 0.8),
		CurrentUsage: usage.MemoryBytes,
		PeakUsage:    usage.MemoryBytes,
		Unit:         "bytes",
		Enforced:     true,
		LastUpdated:  usage.CollectedAt,
	}

	// GPU
	snap.Resources[QuotaGPU] = &QuotaEntry{
		ResourceType: QuotaGPU,
		HardLimit:    int64(tenant.Quota.MaxGPUCount),
		SoftLimit:    int64(float64(tenant.Quota.MaxGPUCount) * 0.8),
		CurrentUsage: int64(usage.GPUCount),
		PeakUsage:    int64(usage.GPUCount),
		Unit:         "count",
		Enforced:     true,
		LastUpdated:  usage.CollectedAt,
	}

	// Storage
	snap.Resources[QuotaStorage] = &QuotaEntry{
		ResourceType: QuotaStorage,
		HardLimit:    tenant.Quota.MaxStorageBytes,
		SoftLimit:    int64(float64(tenant.Quota.MaxStorageBytes) * 0.8),
		CurrentUsage: usage.StorageBytes,
		PeakUsage:    usage.StorageBytes,
		Unit:         "bytes",
		Enforced:     true,
		LastUpdated:  usage.CollectedAt,
	}

	// Pods
	snap.Resources[QuotaPods] = &QuotaEntry{
		ResourceType: QuotaPods,
		HardLimit:    int64(tenant.Quota.MaxPods),
		SoftLimit:    int64(float64(tenant.Quota.MaxPods) * 0.8),
		CurrentUsage: int64(usage.Pods),
		Unit:         "count",
		Enforced:     true,
		LastUpdated:  usage.CollectedAt,
	}

	return snap, nil
}

// GenerateCostAllocationReport generates a cost allocation report for all tenants.
func (m *Manager) GenerateCostAllocationReport(periodStart, periodEnd time.Time) *CostAllocationReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rates := DefaultCostRates()
	rateMap := make(map[QuotaResourceType]CostRate)
	for _, r := range rates {
		rateMap[r.ResourceType] = r
	}

	hours := periodEnd.Sub(periodStart).Hours()
	if hours <= 0 {
		hours = 1
	}

	report := &CostAllocationReport{
		ID:          common.NewUUID(),
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		GeneratedAt: common.NowUTC(),
		Currency:    "USD",
	}

	totalPlatformCost := 0.0

	for _, tenant := range m.tenants {
		usage, ok := m.usage[tenant.ID]
		if !ok {
			continue
		}

		summary := TenantCostSummary{
			TenantID:   tenant.ID,
			TenantName: tenant.Name,
			Tier:       tenant.Tier,
		}

		// CPU cost: millicores → cores
		if rate, ok := rateMap[QuotaCPU]; ok {
			cores := float64(usage.CPUMillicores) / 1000.0
			cost := cores * rate.UnitPrice * hours
			summary.ResourceCosts = append(summary.ResourceCosts, ResourceCostItem{
				ResourceType: QuotaCPU, UsageQuantity: cores,
				Unit: rate.Unit, UnitPrice: rate.UnitPrice, TotalCost: cost,
			})
			summary.TotalCost += cost
		}

		// Memory cost: bytes → GiB
		if rate, ok := rateMap[QuotaMemory]; ok {
			gib := float64(usage.MemoryBytes) / (1024 * 1024 * 1024)
			cost := gib * rate.UnitPrice * hours
			summary.ResourceCosts = append(summary.ResourceCosts, ResourceCostItem{
				ResourceType: QuotaMemory, UsageQuantity: gib,
				Unit: rate.Unit, UnitPrice: rate.UnitPrice, TotalCost: cost,
			})
			summary.TotalCost += cost
		}

		// GPU cost
		if rate, ok := rateMap[QuotaGPU]; ok && usage.GPUCount > 0 {
			cost := float64(usage.GPUCount) * rate.UnitPrice * hours
			summary.ResourceCosts = append(summary.ResourceCosts, ResourceCostItem{
				ResourceType: QuotaGPU, UsageQuantity: float64(usage.GPUCount),
				Unit: rate.Unit, UnitPrice: rate.UnitPrice, TotalCost: cost,
			})
			summary.TotalCost += cost
		}

		// Storage cost: bytes → GiB
		if rate, ok := rateMap[QuotaStorage]; ok {
			gib := float64(usage.StorageBytes) / (1024 * 1024 * 1024)
			cost := gib * rate.UnitPrice * hours
			summary.ResourceCosts = append(summary.ResourceCosts, ResourceCostItem{
				ResourceType: QuotaStorage, UsageQuantity: gib,
				Unit: rate.Unit, UnitPrice: rate.UnitPrice, TotalCost: cost,
			})
			summary.TotalCost += cost
		}

		totalPlatformCost += summary.TotalCost
		report.TenantCosts = append(report.TenantCosts, summary)
	}

	// Calculate percentages
	report.TotalCost = totalPlatformCost
	for i := range report.TenantCosts {
		if totalPlatformCost > 0 {
			report.TenantCosts[i].CostPercent = report.TenantCosts[i].TotalCost / totalPlatformCost * 100
		}
	}

	// Sort by cost descending
	sort.Slice(report.TenantCosts, func(i, j int) bool {
		return report.TenantCosts[i].TotalCost > report.TenantCosts[j].TotalCost
	})

	// Add shared infrastructure overhead (15%)
	report.UnallocatedCost = totalPlatformCost * 0.15
	report.TotalCost += report.UnallocatedCost

	return report
}

// GetTopConsumers returns the top N tenants by resource usage.
func (m *Manager) GetTopConsumers(resourceType QuotaResourceType, limit int) []TenantResourceRank {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var ranks []TenantResourceRank
	for _, tenant := range m.tenants {
		usage, ok := m.usage[tenant.ID]
		if !ok {
			continue
		}

		var val int64
		switch resourceType {
		case QuotaCPU:
			val = usage.CPUMillicores
		case QuotaMemory:
			val = usage.MemoryBytes
		case QuotaGPU:
			val = int64(usage.GPUCount)
		case QuotaStorage:
			val = usage.StorageBytes
		case QuotaPods:
			val = int64(usage.Pods)
		default:
			continue
		}

		ranks = append(ranks, TenantResourceRank{
			TenantID:   tenant.ID,
			TenantName: tenant.Name,
			Tier:       tenant.Tier,
			Usage:      val,
		})
	}

	sort.Slice(ranks, func(i, j int) bool {
		return ranks[i].Usage > ranks[j].Usage
	})

	if limit > 0 && len(ranks) > limit {
		ranks = ranks[:limit]
	}

	return ranks
}

// TenantResourceRank represents a tenant's ranking for a resource type.
type TenantResourceRank struct {
	TenantID   string     `json:"tenant_id"`
	TenantName string     `json:"tenant_name"`
	Tier       TenantTier `json:"tier"`
	Usage      int64      `json:"usage"`
}
