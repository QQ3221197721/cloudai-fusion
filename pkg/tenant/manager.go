package tenant

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Configuration
// ============================================================================

// ManagerConfig holds configuration for the tenant manager
type ManagerConfig struct {
	DefaultTier       TenantTier    `json:"default_tier"`
	EnforceQuotas     bool          `json:"enforce_quotas"`
	BillingEnabled    bool          `json:"billing_enabled"`
	EncryptionEnabled bool          `json:"encryption_enabled"`
	MeteringInterval  time.Duration `json:"metering_interval"`
}

// ============================================================================
// Manager
// ============================================================================

// Manager provides multi-tenant isolation management
type Manager struct {
	tenants    map[string]*Tenant
	usage      map[string]*QuotaUsage
	records    map[string][]*UsageRecord
	invoices   map[string][]*Invoice
	violations map[string][]*QuotaViolation
	keys       map[string][]*EncryptionKey
	prices     []ResourcePrice

	config ManagerConfig
	logger *logrus.Logger
	mu     sync.RWMutex
}

// NewManager creates a new tenant manager
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.MeteringInterval == 0 {
		cfg.MeteringInterval = 1 * time.Minute
	}
	if cfg.DefaultTier == "" {
		cfg.DefaultTier = TierFree
	}

	return &Manager{
		tenants:    make(map[string]*Tenant),
		usage:      make(map[string]*QuotaUsage),
		records:    make(map[string][]*UsageRecord),
		invoices:   make(map[string][]*Invoice),
		violations: make(map[string][]*QuotaViolation),
		keys:       make(map[string][]*EncryptionKey),
		prices:     defaultPrices(),
		config:     cfg,
		logger:     logrus.StandardLogger(),
	}
}

// ============================================================================
// Tenant Lifecycle
// ============================================================================

// CreateTenant creates a new tenant with default quotas based on tier
func (m *Manager) CreateTenant(ctx context.Context, name, displayName, ownerID, ownerEmail string, tier TenantTier) (*Tenant, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.tenants {
		if t.Name == name {
			return nil, fmt.Errorf("tenant with name %q already exists", name)
		}
	}

	if tier == "" {
		tier = m.config.DefaultTier
	}

	now := common.NowUTC()
	tenant := &Tenant{
		ID:          common.NewUUID(),
		Name:        name,
		DisplayName: displayName,
		Tier:        tier,
		Status:      TenantStatusProvisioning,
		OwnerID:     ownerID,
		OwnerEmail:  ownerEmail,
		Namespaces:  []string{fmt.Sprintf("tenant-%s", name)},
		Labels:      map[string]string{"tier": string(tier)},
		Quota:       DefaultQuotaByTier(tier),
		Network: NetworkIsolation{
			Enabled:              true,
			NetworkPolicyEnabled: true,
			DNSPolicy:            "isolated",
		},
		Encryption: EncryptionConfig{
			Enabled:     m.config.EncryptionEnabled,
			Algorithm:   "AES-256-GCM",
			KeyProvider: "local",
			KeyVersion:  1,
		},
		Billing: BillingConfig{
			PlanID:             string(tier),
			BillingCycle:       "monthly",
			Currency:           "USD",
			AlertThresholds:    []float64{0.5, 0.8, 0.95},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if tier == TierPro || tier == TierEnterprise {
		tenant.Network.VPCEnabled = true
		tenant.Network.VPCCidr = fmt.Sprintf("10.%d.0.0/16", len(m.tenants)+1)
		tenant.Network.ServiceMeshIsolation = true
	}

	if tier == TierEnterprise {
		tenant.Encryption.EncryptSecrets = true
		tenant.Encryption.EncryptVolumes = true
		tenant.Encryption.EncryptBackups = true
		tenant.Encryption.EncryptTransit = true
		tenant.Encryption.RotationInterval = "90d"
	}

	m.tenants[tenant.ID] = tenant
	m.usage[tenant.ID] = &QuotaUsage{TenantID: tenant.ID, CollectedAt: now}

	if tenant.Encryption.Enabled {
		key := &EncryptionKey{
			ID:        common.NewUUID(),
			TenantID:  tenant.ID,
			Algorithm: tenant.Encryption.Algorithm,
			Provider:  tenant.Encryption.KeyProvider,
			Version:   1,
			Status:    "active",
			CreatedAt: now,
			ExpiresAt: now.AddDate(0, 3, 0),
		}
		m.keys[tenant.ID] = []*EncryptionKey{key}
		tenant.Encryption.KeyID = key.ID
	}

	tenant.Status = TenantStatusActive

	m.logger.WithFields(logrus.Fields{
		"tenant_id": tenant.ID, "name": tenant.Name,
		"tier": tier, "owner": ownerEmail,
	}).Info("Tenant created")

	return tenant, nil
}

// GetTenant returns a tenant by ID
func (m *Manager) GetTenant(_ context.Context, tenantID string) (*Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return nil, fmt.Errorf("tenant %q not found", tenantID)
	}
	return tenant, nil
}

// ListTenants returns all tenants
func (m *Manager) ListTenants(_ context.Context) []*Tenant {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Tenant, 0, len(m.tenants))
	for _, t := range m.tenants {
		result = append(result, t)
	}
	return result
}

// UpdateTenantTier upgrades or downgrades a tenant's tier
func (m *Manager) UpdateTenantTier(ctx context.Context, tenantID string, newTier TenantTier) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return fmt.Errorf("tenant %q not found", tenantID)
	}

	oldTier := tenant.Tier
	tenant.Tier = newTier
	tenant.Quota = DefaultQuotaByTier(newTier)
	tenant.Labels["tier"] = string(newTier)
	tenant.UpdatedAt = common.NowUTC()

	if newTier == TierPro || newTier == TierEnterprise {
		tenant.Network.VPCEnabled = true
		tenant.Network.ServiceMeshIsolation = true
	}

	m.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID, "old_tier": oldTier, "new_tier": newTier,
	}).Info("Tenant tier updated")

	return nil
}

// SuspendTenant suspends a tenant
func (m *Manager) SuspendTenant(_ context.Context, tenantID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return fmt.Errorf("tenant %q not found", tenantID)
	}

	now := common.NowUTC()
	tenant.Status = TenantStatusSuspended
	tenant.SuspendedAt = &now
	tenant.UpdatedAt = now

	m.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID, "reason": reason,
	}).Warn("Tenant suspended")

	return nil
}

// ReactivateTenant reactivates a suspended tenant
func (m *Manager) ReactivateTenant(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return fmt.Errorf("tenant %q not found", tenantID)
	}

	if tenant.Status != TenantStatusSuspended {
		return fmt.Errorf("tenant %q is not suspended (current: %s)", tenantID, tenant.Status)
	}

	tenant.Status = TenantStatusActive
	tenant.SuspendedAt = nil
	tenant.UpdatedAt = common.NowUTC()

	m.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID,
	}).Info("Tenant reactivated")

	return nil
}

// DeleteTenant removes a tenant
func (m *Manager) DeleteTenant(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.tenants[tenantID]; !ok {
		return fmt.Errorf("tenant %q not found", tenantID)
	}

	delete(m.tenants, tenantID)
	delete(m.usage, tenantID)
	delete(m.keys, tenantID)

	m.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID,
	}).Info("Tenant deleted")

	return nil
}

// ============================================================================
// Quota Management
// ============================================================================

// CheckQuota validates whether a resource request fits within the tenant's quota
func (m *Manager) CheckQuota(_ context.Context, tenantID string, resourceType string, requestAmount float64) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return fmt.Errorf("tenant %q not found", tenantID)
	}

	if tenant.Status != TenantStatusActive {
		return fmt.Errorf("tenant %q is not active (status: %s)", tenantID, tenant.Status)
	}

	if !m.config.EnforceQuotas {
		return nil
	}

	usage, ok := m.usage[tenantID]
	if !ok {
		return nil
	}

	switch resourceType {
	case "cpu":
		if int64(requestAmount)+usage.CPUMillicores > tenant.Quota.MaxCPUMillicores {
			return fmt.Errorf("CPU quota exceeded: requested %v + current %d > limit %d",
				requestAmount, usage.CPUMillicores, tenant.Quota.MaxCPUMillicores)
		}
	case "memory":
		if int64(requestAmount)+usage.MemoryBytes > tenant.Quota.MaxMemoryBytes {
			return fmt.Errorf("memory quota exceeded: requested %v + current %d > limit %d",
				requestAmount, usage.MemoryBytes, tenant.Quota.MaxMemoryBytes)
		}
	case "gpu":
		if int(requestAmount)+usage.GPUCount > tenant.Quota.MaxGPUCount {
			return fmt.Errorf("GPU quota exceeded: requested %v + current %d > limit %d",
				requestAmount, usage.GPUCount, tenant.Quota.MaxGPUCount)
		}
	case "storage":
		if int64(requestAmount)+usage.StorageBytes > tenant.Quota.MaxStorageBytes {
			return fmt.Errorf("storage quota exceeded")
		}
	case "pods":
		if int(requestAmount)+usage.Pods > tenant.Quota.MaxPods {
			return fmt.Errorf("pod quota exceeded: requested %v + current %d > limit %d",
				requestAmount, usage.Pods, tenant.Quota.MaxPods)
		}
	}

	return nil
}

// UpdateUsage updates the current resource usage for a tenant
func (m *Manager) UpdateUsage(_ context.Context, tenantID string, usage *QuotaUsage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.tenants[tenantID]; !ok {
		return fmt.Errorf("tenant %q not found", tenantID)
	}

	usage.TenantID = tenantID
	usage.CollectedAt = common.NowUTC()
	m.usage[tenantID] = usage

	m.checkViolations(tenantID)

	return nil
}

// GetUsage returns current resource usage for a tenant
func (m *Manager) GetUsage(_ context.Context, tenantID string) (*QuotaUsage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	usage, ok := m.usage[tenantID]
	if !ok {
		return nil, fmt.Errorf("no usage data for tenant %q", tenantID)
	}
	return usage, nil
}

// GetViolations returns quota violations for a tenant
func (m *Manager) GetViolations(_ context.Context, tenantID string) []*QuotaViolation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.violations[tenantID]
}

func (m *Manager) checkViolations(tenantID string) {
	tenant := m.tenants[tenantID]
	usage := m.usage[tenantID]
	if tenant == nil || usage == nil {
		return
	}

	now := common.NowUTC()

	checkResource := func(resType string, current float64, limit float64) {
		if limit <= 0 {
			return
		}
		ratio := current / limit
		if ratio >= 0.95 {
			v := &QuotaViolation{
				ID: common.NewUUID(), TenantID: tenantID,
				ResourceType: resType, Current: current, Limit: limit,
				Severity: "critical",
				Message:  fmt.Sprintf("%s usage at %.1f%% of quota", resType, ratio*100),
				DetectedAt: now,
			}
			if ratio >= 1.0 {
				v.Message = fmt.Sprintf("%s quota exceeded: %.0f / %.0f", resType, current, limit)
			}
			m.violations[tenantID] = append(m.violations[tenantID], v)
		} else if ratio >= 0.8 {
			v := &QuotaViolation{
				ID: common.NewUUID(), TenantID: tenantID,
				ResourceType: resType, Current: current, Limit: limit,
				Severity: "warning",
				Message:  fmt.Sprintf("%s usage at %.1f%% of quota", resType, ratio*100),
				DetectedAt: now,
			}
			m.violations[tenantID] = append(m.violations[tenantID], v)
		}
	}

	checkResource("cpu", float64(usage.CPUMillicores), float64(tenant.Quota.MaxCPUMillicores))
	checkResource("memory", float64(usage.MemoryBytes), float64(tenant.Quota.MaxMemoryBytes))
	checkResource("gpu", float64(usage.GPUCount), float64(tenant.Quota.MaxGPUCount))
	checkResource("pods", float64(usage.Pods), float64(tenant.Quota.MaxPods))
}

// ============================================================================
// Encryption Key Management
// ============================================================================

// RotateEncryptionKey creates a new encryption key version for a tenant
func (m *Manager) RotateEncryptionKey(_ context.Context, tenantID string) (*EncryptionKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return nil, fmt.Errorf("tenant %q not found", tenantID)
	}

	if !tenant.Encryption.Enabled {
		return nil, fmt.Errorf("encryption is not enabled for tenant %q", tenantID)
	}

	now := common.NowUTC()

	// Retire old active keys
	for _, k := range m.keys[tenantID] {
		if k.Status == "active" {
			k.Status = "retired"
		}
	}

	newKey := &EncryptionKey{
		ID:        common.NewUUID(),
		TenantID:  tenantID,
		Algorithm: tenant.Encryption.Algorithm,
		Provider:  tenant.Encryption.KeyProvider,
		Version:   tenant.Encryption.KeyVersion + 1,
		Status:    "active",
		CreatedAt: now,
		ExpiresAt: now.AddDate(0, 3, 0),
	}

	m.keys[tenantID] = append(m.keys[tenantID], newKey)
	tenant.Encryption.KeyID = newKey.ID
	tenant.Encryption.KeyVersion = newKey.Version
	tenant.Encryption.LastRotatedAt = &now
	tenant.UpdatedAt = now

	m.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID, "key_version": newKey.Version,
	}).Info("Encryption key rotated")

	return newKey, nil
}

// GetEncryptionKeys returns all encryption keys for a tenant
func (m *Manager) GetEncryptionKeys(_ context.Context, tenantID string) []*EncryptionKey {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.keys[tenantID]
}

// ============================================================================
// Billing & Metering
// ============================================================================

// RecordUsage records a usage metering event
func (m *Manager) RecordUsage(_ context.Context, tenantID string, resourceType string, quantity float64, namespace, clusterID string) (*UsageRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return nil, fmt.Errorf("tenant %q not found", tenantID)
	}

	price := m.findPrice(resourceType, tenant.Tier)
	now := common.NowUTC()

	record := &UsageRecord{
		ID:           common.NewUUID(),
		TenantID:     tenantID,
		ResourceType: resourceType,
		Quantity:     quantity,
		Unit:         price.Unit,
		UnitPrice:    price.UnitPrice,
		TotalCost:    quantity * price.UnitPrice,
		Namespace:    namespace,
		ClusterID:    clusterID,
		StartTime:    now,
		EndTime:      now.Add(1 * time.Hour),
		RecordedAt:   now,
	}

	m.records[tenantID] = append(m.records[tenantID], record)

	// Check budget alerts
	m.checkBudgetAlerts(tenantID)

	return record, nil
}

// GetBillingSummary returns billing summary for a tenant and period
func (m *Manager) GetBillingSummary(_ context.Context, tenantID, period string) (*BillingSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return nil, fmt.Errorf("tenant %q not found", tenantID)
	}

	records := m.records[tenantID]
	summary := &BillingSummary{
		TenantID:        tenantID,
		Period:          period,
		BudgetLimit:     tenant.Billing.BudgetLimit,
		CostByResource:  make(map[string]float64),
		CostByNamespace: make(map[string]float64),
		CostByCluster:   make(map[string]float64),
	}

	for _, r := range records {
		summary.TotalCost += r.TotalCost
		summary.CostByResource[r.ResourceType] += r.TotalCost
		if r.Namespace != "" {
			summary.CostByNamespace[r.Namespace] += r.TotalCost
		}
		if r.ClusterID != "" {
			summary.CostByCluster[r.ClusterID] += r.TotalCost
		}
	}

	if summary.BudgetLimit > 0 {
		summary.BudgetUsedPercent = (summary.TotalCost / summary.BudgetLimit) * 100
	}

	// Simple projection: assume linear cost growth
	now := common.NowUTC()
	dayOfMonth := float64(now.Day())
	if dayOfMonth > 0 {
		daysInMonth := 30.0
		summary.ProjectedMonthlyCost = (summary.TotalCost / dayOfMonth) * daysInMonth
	}

	return summary, nil
}

// GenerateInvoice generates an invoice for a tenant
func (m *Manager) GenerateInvoice(_ context.Context, tenantID, period string) (*Invoice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.tenants[tenantID]; !ok {
		return nil, fmt.Errorf("tenant %q not found", tenantID)
	}

	// Aggregate records by resource type
	costByType := make(map[string]float64)
	qtyByType := make(map[string]float64)
	for _, r := range m.records[tenantID] {
		costByType[r.ResourceType] += r.TotalCost
		qtyByType[r.ResourceType] += r.Quantity
	}

	var lineItems []InvoiceItem
	var subtotal float64
	for resType, cost := range costByType {
		item := InvoiceItem{
			Description:  fmt.Sprintf("%s usage", resType),
			ResourceType: resType,
			Quantity:     qtyByType[resType],
			UnitPrice:    cost / qtyByType[resType],
			Amount:       cost,
		}
		lineItems = append(lineItems, item)
		subtotal += cost
	}

	now := common.NowUTC()
	tax := subtotal * 0.0 // tax calculation placeholder
	invoice := &Invoice{
		ID:        common.NewUUID(),
		TenantID:  tenantID,
		Period:    period,
		Status:    "issued",
		LineItems: lineItems,
		Subtotal:  subtotal,
		Tax:       tax,
		Total:     subtotal + tax,
		Currency:  "USD",
		IssuedAt:  now,
		DueAt:     now.AddDate(0, 0, 30),
	}

	m.invoices[tenantID] = append(m.invoices[tenantID], invoice)

	m.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID, "invoice_id": invoice.ID,
		"period": period, "total": invoice.Total,
	}).Info("Invoice generated")

	return invoice, nil
}

// GetInvoices returns invoices for a tenant
func (m *Manager) GetInvoices(_ context.Context, tenantID string) []*Invoice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.invoices[tenantID]
}

func (m *Manager) checkBudgetAlerts(tenantID string) {
	tenant := m.tenants[tenantID]
	if tenant == nil || tenant.Billing.BudgetLimit <= 0 {
		return
	}

	var totalCost float64
	for _, r := range m.records[tenantID] {
		totalCost += r.TotalCost
	}

	ratio := totalCost / tenant.Billing.BudgetLimit
	for _, threshold := range tenant.Billing.AlertThresholds {
		if ratio >= threshold {
			m.logger.WithFields(logrus.Fields{
				"tenant_id": tenantID, "threshold": threshold,
				"current_cost": totalCost, "budget": tenant.Billing.BudgetLimit,
			}).Warn("Budget alert threshold reached")
		}
	}

	// Auto-suspend if over limit
	if ratio >= 1.0 && tenant.Billing.AutoSuspendOnLimit {
		tenant.Status = TenantStatusSuspended
		now := common.NowUTC()
		tenant.SuspendedAt = &now
		m.logger.WithFields(logrus.Fields{
			"tenant_id": tenantID,
		}).Warn("Tenant auto-suspended due to budget limit exceeded")
	}
}

// AddNamespace adds a namespace to a tenant
func (m *Manager) AddNamespace(_ context.Context, tenantID, namespace string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return fmt.Errorf("tenant %q not found", tenantID)
	}

	if len(tenant.Namespaces) >= tenant.Quota.MaxNamespaces {
		return fmt.Errorf("namespace quota exceeded: current %d >= limit %d",
			len(tenant.Namespaces), tenant.Quota.MaxNamespaces)
	}

	for _, ns := range tenant.Namespaces {
		if ns == namespace {
			return fmt.Errorf("namespace %q already exists for tenant", namespace)
		}
	}

	tenant.Namespaces = append(tenant.Namespaces, namespace)
	tenant.UpdatedAt = common.NowUTC()
	return nil
}

// UpdateNetworkIsolation updates the network isolation config for a tenant
func (m *Manager) UpdateNetworkIsolation(_ context.Context, tenantID string, network NetworkIsolation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tenant, ok := m.tenants[tenantID]
	if !ok {
		return fmt.Errorf("tenant %q not found", tenantID)
	}

	tenant.Network = network
	tenant.UpdatedAt = common.NowUTC()

	m.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID, "vpc_enabled": network.VPCEnabled,
	}).Info("Network isolation updated")

	return nil
}

// StartMeteringLoop starts the periodic metering collection loop
func (m *Manager) StartMeteringLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(m.config.MeteringInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.collectMetering()
			}
		}
	}()
}

func (m *Manager) collectMetering() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for tenantID, usage := range m.usage {
		tenant := m.tenants[tenantID]
		if tenant == nil || tenant.Status != TenantStatusActive {
			continue
		}

		// Record CPU usage
		if usage.CPUMillicores > 0 {
			cpuHours := float64(usage.CPUMillicores) / 1000.0
			price := m.findPrice("cpu", tenant.Tier)
			record := &UsageRecord{
				ID:           common.NewUUID(),
				TenantID:     tenantID,
				ResourceType: "cpu",
				Quantity:     cpuHours,
				Unit:         price.Unit,
				UnitPrice:    price.UnitPrice,
				TotalCost:    cpuHours * price.UnitPrice,
				RecordedAt:   common.NowUTC(),
			}
			m.records[tenantID] = append(m.records[tenantID], record)
		}
	}
}

func (m *Manager) findPrice(resourceType string, tier TenantTier) ResourcePrice {
	for _, p := range m.prices {
		if p.ResourceType == resourceType && p.Tier == tier {
			return p
		}
	}
	// fallback to default
	for _, p := range m.prices {
		if p.ResourceType == resourceType {
			return p
		}
	}
	return ResourcePrice{UnitPrice: 0.01, Unit: "per-hour"}
}

func defaultPrices() []ResourcePrice {
	return []ResourcePrice{
		{ResourceType: "cpu", Unit: "per-vcpu-hour", UnitPrice: 0.05, Currency: "USD", Tier: TierFree},
		{ResourceType: "cpu", Unit: "per-vcpu-hour", UnitPrice: 0.04, Currency: "USD", Tier: TierStarter},
		{ResourceType: "cpu", Unit: "per-vcpu-hour", UnitPrice: 0.03, Currency: "USD", Tier: TierPro},
		{ResourceType: "cpu", Unit: "per-vcpu-hour", UnitPrice: 0.02, Currency: "USD", Tier: TierEnterprise},
		{ResourceType: "memory", Unit: "per-gb-hour", UnitPrice: 0.01, Currency: "USD", Tier: TierFree},
		{ResourceType: "memory", Unit: "per-gb-hour", UnitPrice: 0.008, Currency: "USD", Tier: TierPro},
		{ResourceType: "gpu", Unit: "per-gpu-hour", UnitPrice: 3.00, Currency: "USD", Tier: TierFree},
		{ResourceType: "gpu", Unit: "per-gpu-hour", UnitPrice: 2.50, Currency: "USD", Tier: TierStarter},
		{ResourceType: "gpu", Unit: "per-gpu-hour", UnitPrice: 2.00, Currency: "USD", Tier: TierPro},
		{ResourceType: "gpu", Unit: "per-gpu-hour", UnitPrice: 1.50, Currency: "USD", Tier: TierEnterprise},
		{ResourceType: "storage", Unit: "per-gb-month", UnitPrice: 0.10, Currency: "USD", Tier: TierFree},
		{ResourceType: "network", Unit: "per-gb", UnitPrice: 0.12, Currency: "USD", Tier: TierFree},
		{ResourceType: "api-call", Unit: "per-10k-requests", UnitPrice: 0.01, Currency: "USD", Tier: TierFree},
	}
}
