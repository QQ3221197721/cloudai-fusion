// Package tenant provides multi-tenant isolation for CloudAI Fusion,
// including namespace-level resource quotas, network isolation (VPC per tenant),
// data encryption with tenant-specific keys, and usage-based billing metering.
package tenant

import (
	"time"
)

// ============================================================================
// Tenant Core
// ============================================================================

// TenantStatus represents the lifecycle state of a tenant
type TenantStatus string

const (
	TenantStatusActive     TenantStatus = "active"
	TenantStatusSuspended  TenantStatus = "suspended"
	TenantStatusDeactivated TenantStatus = "deactivated"
	TenantStatusProvisioning TenantStatus = "provisioning"
	TenantStatusDeleting   TenantStatus = "deleting"
)

// TenantTier defines the subscription tier
type TenantTier string

const (
	TierFree       TenantTier = "free"
	TierStarter    TenantTier = "starter"
	TierPro        TenantTier = "pro"
	TierEnterprise TenantTier = "enterprise"
)

// Tenant represents a tenant organization in the platform
type Tenant struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	DisplayName   string            `json:"display_name"`
	Tier          TenantTier        `json:"tier"`
	Status        TenantStatus      `json:"status"`
	OwnerID       string            `json:"owner_id"`
	OwnerEmail    string            `json:"owner_email"`
	Namespaces    []string          `json:"namespaces"`
	Labels        map[string]string `json:"labels,omitempty"`
	Quota         ResourceQuota     `json:"quota"`
	Network       NetworkIsolation  `json:"network"`
	Encryption    EncryptionConfig  `json:"encryption"`
	Billing       BillingConfig     `json:"billing"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	SuspendedAt   *time.Time        `json:"suspended_at,omitempty"`
}

// ============================================================================
// Resource Quotas (Namespace-Level)
// ============================================================================

// ResourceQuota defines resource limits for a tenant
type ResourceQuota struct {
	// Compute
	MaxCPUMillicores  int64 `json:"max_cpu_millicores"`
	MaxMemoryBytes    int64 `json:"max_memory_bytes"`
	MaxGPUCount       int   `json:"max_gpu_count"`
	MaxStorageBytes   int64 `json:"max_storage_bytes"`

	// Kubernetes objects
	MaxPods           int `json:"max_pods"`
	MaxServices       int `json:"max_services"`
	MaxSecrets        int `json:"max_secrets"`
	MaxConfigMaps     int `json:"max_configmaps"`
	MaxPVCs           int `json:"max_pvcs"`

	// Workloads
	MaxConcurrentJobs   int `json:"max_concurrent_jobs"`
	MaxNamespaces       int `json:"max_namespaces"`
	MaxClusters         int `json:"max_clusters"`

	// Network
	MaxIngressBandwidthMbps int `json:"max_ingress_bandwidth_mbps"`
	MaxEgressBandwidthMbps  int `json:"max_egress_bandwidth_mbps"`
}

// QuotaUsage tracks current resource usage against the quota
type QuotaUsage struct {
	TenantID          string    `json:"tenant_id"`
	CPUMillicores     int64     `json:"cpu_millicores"`
	MemoryBytes       int64     `json:"memory_bytes"`
	GPUCount          int       `json:"gpu_count"`
	StorageBytes      int64     `json:"storage_bytes"`
	Pods              int       `json:"pods"`
	Services          int       `json:"services"`
	Secrets           int       `json:"secrets"`
	ConfigMaps        int       `json:"configmaps"`
	PVCs              int       `json:"pvcs"`
	ConcurrentJobs    int       `json:"concurrent_jobs"`
	Namespaces        int       `json:"namespaces"`
	Clusters          int       `json:"clusters"`
	CollectedAt       time.Time `json:"collected_at"`
}

// DefaultQuotaByTier returns default resource quotas for a tier
func DefaultQuotaByTier(tier TenantTier) ResourceQuota {
	switch tier {
	case TierFree:
		return ResourceQuota{
			MaxCPUMillicores: 4000, MaxMemoryBytes: 8 * 1024 * 1024 * 1024,
			MaxGPUCount: 0, MaxStorageBytes: 10 * 1024 * 1024 * 1024,
			MaxPods: 20, MaxServices: 5, MaxSecrets: 10, MaxConfigMaps: 10, MaxPVCs: 2,
			MaxConcurrentJobs: 1, MaxNamespaces: 1, MaxClusters: 1,
			MaxIngressBandwidthMbps: 100, MaxEgressBandwidthMbps: 100,
		}
	case TierStarter:
		return ResourceQuota{
			MaxCPUMillicores: 16000, MaxMemoryBytes: 32 * 1024 * 1024 * 1024,
			MaxGPUCount: 2, MaxStorageBytes: 100 * 1024 * 1024 * 1024,
			MaxPods: 100, MaxServices: 20, MaxSecrets: 50, MaxConfigMaps: 50, MaxPVCs: 10,
			MaxConcurrentJobs: 5, MaxNamespaces: 5, MaxClusters: 2,
			MaxIngressBandwidthMbps: 500, MaxEgressBandwidthMbps: 500,
		}
	case TierPro:
		return ResourceQuota{
			MaxCPUMillicores: 64000, MaxMemoryBytes: 128 * 1024 * 1024 * 1024,
			MaxGPUCount: 16, MaxStorageBytes: 1024 * 1024 * 1024 * 1024,
			MaxPods: 500, MaxServices: 100, MaxSecrets: 200, MaxConfigMaps: 200, MaxPVCs: 50,
			MaxConcurrentJobs: 20, MaxNamespaces: 20, MaxClusters: 10,
			MaxIngressBandwidthMbps: 2000, MaxEgressBandwidthMbps: 2000,
		}
	case TierEnterprise:
		return ResourceQuota{
			MaxCPUMillicores: 256000, MaxMemoryBytes: 512 * 1024 * 1024 * 1024,
			MaxGPUCount: 128, MaxStorageBytes: 10 * 1024 * 1024 * 1024 * 1024,
			MaxPods: 5000, MaxServices: 500, MaxSecrets: 1000, MaxConfigMaps: 1000, MaxPVCs: 200,
			MaxConcurrentJobs: 100, MaxNamespaces: 100, MaxClusters: 50,
			MaxIngressBandwidthMbps: 10000, MaxEgressBandwidthMbps: 10000,
		}
	default:
		return DefaultQuotaByTier(TierFree)
	}
}

// ============================================================================
// Network Isolation (VPC per Tenant)
// ============================================================================

// NetworkIsolation defines network isolation configuration
type NetworkIsolation struct {
	Enabled         bool              `json:"enabled"`
	VPCEnabled      bool              `json:"vpc_enabled"`
	VPCID           string            `json:"vpc_id,omitempty"`
	VPCCidr         string            `json:"vpc_cidr,omitempty"`
	SubnetCidrs     []string          `json:"subnet_cidrs,omitempty"`
	NetworkPolicyEnabled bool         `json:"network_policy_enabled"`
	AllowedCIDRs    []string          `json:"allowed_cidrs,omitempty"`
	DeniedCIDRs     []string          `json:"denied_cidrs,omitempty"`
	IngressRules    []NetworkRule     `json:"ingress_rules,omitempty"`
	EgressRules     []NetworkRule     `json:"egress_rules,omitempty"`
	DNSPolicy       string            `json:"dns_policy"` // isolated, shared, custom
	ServiceMeshIsolation bool         `json:"service_mesh_isolation"`
}

// NetworkRule defines a network access rule
type NetworkRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Protocol    string `json:"protocol"` // TCP, UDP, ICMP
	PortRange   string `json:"port_range"` // e.g., "80", "8080-8090"
	Source      string `json:"source"`     // CIDR or namespace selector
	Destination string `json:"destination"`
	Action      string `json:"action"`   // allow, deny
}

// ============================================================================
// Data Encryption (Tenant-Specific Keys)
// ============================================================================

// EncryptionConfig defines tenant-specific encryption settings
type EncryptionConfig struct {
	Enabled          bool       `json:"enabled"`
	Algorithm        string     `json:"algorithm"` // AES-256-GCM, AES-256-CBC, ChaCha20-Poly1305
	KeyProvider      string     `json:"key_provider"` // vault, aws-kms, azure-keyvault, gcp-kms, local
	KeyID            string     `json:"key_id,omitempty"`
	KeyVersion       int        `json:"key_version"`
	RotationInterval string     `json:"rotation_interval"` // e.g., "90d", "30d"
	LastRotatedAt    *time.Time `json:"last_rotated_at,omitempty"`
	EncryptSecrets   bool       `json:"encrypt_secrets"`     // encrypt K8s secrets at rest
	EncryptVolumes   bool       `json:"encrypt_volumes"`     // encrypt PVs
	EncryptBackups   bool       `json:"encrypt_backups"`     // encrypt backup data
	EncryptTransit   bool       `json:"encrypt_transit"`     // enforce mTLS between services
}

// EncryptionKey represents a tenant's encryption key
type EncryptionKey struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Algorithm   string    `json:"algorithm"`
	Provider    string    `json:"provider"`
	KeyMaterial []byte    `json:"-"` // never serialized
	Version     int       `json:"version"`
	Status      string    `json:"status"` // active, rotating, retired
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ============================================================================
// Billing & Metering (Usage Billing)
// ============================================================================

// BillingConfig defines billing settings for a tenant
type BillingConfig struct {
	PlanID            string          `json:"plan_id"`
	PaymentMethod     string          `json:"payment_method"` // credit-card, invoice, prepaid
	BillingCycle      string          `json:"billing_cycle"`  // monthly, annual
	Currency          string          `json:"currency"`       // USD, EUR, CNY
	BudgetLimit       float64         `json:"budget_limit"`
	AlertThresholds   []float64       `json:"alert_thresholds"` // e.g., [0.5, 0.8, 0.95]
	AutoSuspendOnLimit bool           `json:"auto_suspend_on_limit"`
	CostCenter        string          `json:"cost_center,omitempty"`
	InvoiceEmail      string          `json:"invoice_email"`
}

// ResourcePrice defines pricing for a resource type
type ResourcePrice struct {
	ResourceType string  `json:"resource_type"` // cpu, memory, gpu, storage, network, api-call
	Unit         string  `json:"unit"`          // per-hour, per-gb-month, per-request
	UnitPrice    float64 `json:"unit_price"`
	Currency     string  `json:"currency"`
	Tier         TenantTier `json:"tier"`
}

// UsageRecord represents a single metering record
type UsageRecord struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	ResourceType string    `json:"resource_type"`
	Quantity     float64   `json:"quantity"`
	Unit         string    `json:"unit"`
	UnitPrice    float64   `json:"unit_price"`
	TotalCost    float64   `json:"total_cost"`
	Namespace    string    `json:"namespace,omitempty"`
	ClusterID    string    `json:"cluster_id,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	RecordedAt   time.Time `json:"recorded_at"`
}

// Invoice represents a billing invoice for a tenant
type Invoice struct {
	ID          string        `json:"id"`
	TenantID    string        `json:"tenant_id"`
	Period      string        `json:"period"` // e.g., "2026-03"
	Status      string        `json:"status"` // draft, issued, paid, overdue
	LineItems   []InvoiceItem `json:"line_items"`
	Subtotal    float64       `json:"subtotal"`
	Tax         float64       `json:"tax"`
	Total       float64       `json:"total"`
	Currency    string        `json:"currency"`
	IssuedAt    time.Time     `json:"issued_at"`
	DueAt       time.Time     `json:"due_at"`
	PaidAt      *time.Time    `json:"paid_at,omitempty"`
}

// InvoiceItem represents a line item in an invoice
type InvoiceItem struct {
	Description  string  `json:"description"`
	ResourceType string  `json:"resource_type"`
	Quantity     float64 `json:"quantity"`
	UnitPrice    float64 `json:"unit_price"`
	Amount       float64 `json:"amount"`
}

// BillingSummary provides an overview of a tenant's billing
type BillingSummary struct {
	TenantID           string             `json:"tenant_id"`
	Period             string             `json:"period"`
	TotalCost          float64            `json:"total_cost"`
	BudgetLimit        float64            `json:"budget_limit"`
	BudgetUsedPercent  float64            `json:"budget_used_percent"`
	CostByResource     map[string]float64 `json:"cost_by_resource"`
	CostByNamespace    map[string]float64 `json:"cost_by_namespace"`
	CostByCluster      map[string]float64 `json:"cost_by_cluster"`
	DailyTrend         []DailyCost        `json:"daily_trend"`
	ProjectedMonthlyCost float64          `json:"projected_monthly_cost"`
}

// DailyCost tracks cost for a single day
type DailyCost struct {
	Date string  `json:"date"`
	Cost float64 `json:"cost"`
}

// QuotaViolation records when a tenant exceeds their quota
type QuotaViolation struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	ResourceType string    `json:"resource_type"`
	Current      float64   `json:"current"`
	Limit        float64   `json:"limit"`
	Severity     string    `json:"severity"` // warning, critical
	Message      string    `json:"message"`
	DetectedAt   time.Time `json:"detected_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
}
