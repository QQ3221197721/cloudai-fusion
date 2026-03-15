package tenant

import (
	"context"
	"testing"
	"time"
)

func newTestManager() *Manager {
	return NewManager(ManagerConfig{
		DefaultTier:       TierFree,
		EnforceQuotas:     true,
		BillingEnabled:    true,
		EncryptionEnabled: true,
	})
}

func createTestTenant(t *testing.T, m *Manager, name string, tier TenantTier) *Tenant {
	t.Helper()
	tenant, err := m.CreateTenant(context.Background(), name, "Display "+name, "owner-1", "owner@test.com", tier)
	if err != nil {
		t.Fatalf("CreateTenant(%s) failed: %v", name, err)
	}
	return tenant
}

// ============================================================================
// Tenant Lifecycle Tests
// ============================================================================

func TestCreateTenant(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "acme", TierPro)

	if tenant.Name != "acme" {
		t.Errorf("expected name 'acme', got %q", tenant.Name)
	}
	if tenant.Tier != TierPro {
		t.Errorf("expected tier 'pro', got %q", tenant.Tier)
	}
	if tenant.Status != TenantStatusActive {
		t.Errorf("expected status 'active', got %q", tenant.Status)
	}
	if !tenant.Network.VPCEnabled {
		t.Error("expected VPC enabled for pro tier")
	}
	if tenant.Quota.MaxGPUCount != 16 {
		t.Errorf("expected 16 GPU for pro tier, got %d", tenant.Quota.MaxGPUCount)
	}
}

func TestCreateDuplicateTenant(t *testing.T) {
	m := newTestManager()
	createTestTenant(t, m, "dup", TierFree)

	_, err := m.CreateTenant(context.Background(), "dup", "Dup", "o2", "o2@t.com", TierFree)
	if err == nil {
		t.Error("expected error for duplicate tenant name")
	}
}

func TestGetTenant(t *testing.T) {
	m := newTestManager()
	created := createTestTenant(t, m, "get-test", TierStarter)

	got, err := m.GetTenant(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTenant failed: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %s, want %s", got.ID, created.ID)
	}
}

func TestGetTenantNotFound(t *testing.T) {
	m := newTestManager()
	_, err := m.GetTenant(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent tenant")
	}
}

func TestListTenants(t *testing.T) {
	m := newTestManager()
	createTestTenant(t, m, "t1", TierFree)
	createTestTenant(t, m, "t2", TierPro)

	list := m.ListTenants(context.Background())
	if len(list) != 2 {
		t.Errorf("expected 2 tenants, got %d", len(list))
	}
}

func TestUpdateTenantTier(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "upgrade", TierFree)

	err := m.UpdateTenantTier(context.Background(), tenant.ID, TierEnterprise)
	if err != nil {
		t.Fatalf("UpdateTenantTier failed: %v", err)
	}

	got, _ := m.GetTenant(context.Background(), tenant.ID)
	if got.Tier != TierEnterprise {
		t.Errorf("expected enterprise tier, got %q", got.Tier)
	}
	if got.Quota.MaxGPUCount != 128 {
		t.Errorf("expected 128 GPU for enterprise, got %d", got.Quota.MaxGPUCount)
	}
	if !got.Network.VPCEnabled {
		t.Error("expected VPC enabled after enterprise upgrade")
	}
}

func TestSuspendAndReactivate(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "suspend-test", TierStarter)

	err := m.SuspendTenant(context.Background(), tenant.ID, "billing issue")
	if err != nil {
		t.Fatalf("SuspendTenant failed: %v", err)
	}

	got, _ := m.GetTenant(context.Background(), tenant.ID)
	if got.Status != TenantStatusSuspended {
		t.Errorf("expected suspended, got %q", got.Status)
	}
	if got.SuspendedAt == nil {
		t.Error("expected SuspendedAt to be set")
	}

	err = m.ReactivateTenant(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("ReactivateTenant failed: %v", err)
	}

	got, _ = m.GetTenant(context.Background(), tenant.ID)
	if got.Status != TenantStatusActive {
		t.Errorf("expected active, got %q", got.Status)
	}
}

func TestReactivateNonSuspended(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "not-suspended", TierFree)

	err := m.ReactivateTenant(context.Background(), tenant.ID)
	if err == nil {
		t.Error("expected error reactivating non-suspended tenant")
	}
}

func TestDeleteTenant(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "del-test", TierFree)

	err := m.DeleteTenant(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("DeleteTenant failed: %v", err)
	}

	_, err = m.GetTenant(context.Background(), tenant.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

// ============================================================================
// Quota Tests
// ============================================================================

func TestCheckQuotaWithinLimit(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "quota-ok", TierFree)

	// Free tier: MaxCPUMillicores = 4000
	err := m.CheckQuota(context.Background(), tenant.ID, "cpu", 2000)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestCheckQuotaExceeded(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "quota-exceed", TierFree)

	// Free tier: MaxCPUMillicores = 4000, but set usage to 3000 first
	m.UpdateUsage(context.Background(), tenant.ID, &QuotaUsage{CPUMillicores: 3000})

	err := m.CheckQuota(context.Background(), tenant.ID, "cpu", 2000)
	if err == nil {
		t.Error("expected quota exceeded error")
	}
}

func TestCheckQuotaGPU(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "quota-gpu", TierFree)

	// Free tier: MaxGPUCount = 0
	err := m.CheckQuota(context.Background(), tenant.ID, "gpu", 1)
	if err == nil {
		t.Error("expected GPU quota exceeded for free tier")
	}
}

func TestCheckQuotaSuspendedTenant(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "quota-suspended", TierStarter)
	m.SuspendTenant(context.Background(), tenant.ID, "test")

	err := m.CheckQuota(context.Background(), tenant.ID, "cpu", 100)
	if err == nil {
		t.Error("expected error for suspended tenant")
	}
}

func TestUpdateUsageAndViolations(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "violations", TierFree)

	// Set usage to 90% of CPU quota (4000 * 0.9 = 3600)
	err := m.UpdateUsage(context.Background(), tenant.ID, &QuotaUsage{CPUMillicores: 3600})
	if err != nil {
		t.Fatalf("UpdateUsage failed: %v", err)
	}

	violations := m.GetViolations(context.Background(), tenant.ID)
	found := false
	for _, v := range violations {
		if v.ResourceType == "cpu" && v.Severity == "warning" {
			found = true
		}
	}
	if !found {
		t.Error("expected CPU warning violation at 90% usage")
	}
}

func TestUpdateUsageCriticalViolation(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "critical-v", TierFree)

	// Set usage to 98% of CPU quota
	err := m.UpdateUsage(context.Background(), tenant.ID, &QuotaUsage{CPUMillicores: 3920})
	if err != nil {
		t.Fatalf("UpdateUsage failed: %v", err)
	}

	violations := m.GetViolations(context.Background(), tenant.ID)
	found := false
	for _, v := range violations {
		if v.ResourceType == "cpu" && v.Severity == "critical" {
			found = true
		}
	}
	if !found {
		t.Error("expected CPU critical violation at 98% usage")
	}
}

// ============================================================================
// Encryption Key Tests
// ============================================================================

func TestEncryptionKeyProvisioned(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "enc-test", TierEnterprise)

	keys := m.GetEncryptionKeys(context.Background(), tenant.ID)
	if len(keys) == 0 {
		t.Fatal("expected encryption key to be provisioned")
	}
	if keys[0].Status != "active" {
		t.Errorf("expected active key, got %q", keys[0].Status)
	}
	if keys[0].Algorithm != "AES-256-GCM" {
		t.Errorf("expected AES-256-GCM, got %q", keys[0].Algorithm)
	}
}

func TestRotateEncryptionKey(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "rotate-test", TierEnterprise)

	newKey, err := m.RotateEncryptionKey(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("RotateEncryptionKey failed: %v", err)
	}

	if newKey.Version != 2 {
		t.Errorf("expected version 2, got %d", newKey.Version)
	}

	keys := m.GetEncryptionKeys(context.Background(), tenant.ID)
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}

	// Old key should be retired
	if keys[0].Status != "retired" {
		t.Errorf("expected old key retired, got %q", keys[0].Status)
	}
}

func TestRotateEncryptionKeyDisabled(t *testing.T) {
	m := NewManager(ManagerConfig{EncryptionEnabled: false, EnforceQuotas: true})
	tenant, _ := m.CreateTenant(context.Background(), "no-enc", "No Enc", "o", "o@t.com", TierFree)

	_, err := m.RotateEncryptionKey(context.Background(), tenant.ID)
	if err == nil {
		t.Error("expected error rotating key for non-encrypted tenant")
	}
}

// ============================================================================
// Billing & Metering Tests
// ============================================================================

func TestRecordUsage(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "billing-test", TierPro)

	record, err := m.RecordUsage(context.Background(), tenant.ID, "gpu", 2.0, "ml-training", "cluster-1")
	if err != nil {
		t.Fatalf("RecordUsage failed: %v", err)
	}

	if record.TotalCost <= 0 {
		t.Error("expected positive total cost")
	}
	if record.Namespace != "ml-training" {
		t.Errorf("expected namespace 'ml-training', got %q", record.Namespace)
	}
}

func TestGetBillingSummary(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "summary-test", TierPro)

	m.RecordUsage(context.Background(), tenant.ID, "gpu", 10.0, "ns1", "c1")
	m.RecordUsage(context.Background(), tenant.ID, "cpu", 100.0, "ns2", "c1")
	m.RecordUsage(context.Background(), tenant.ID, "storage", 500.0, "ns1", "c2")

	summary, err := m.GetBillingSummary(context.Background(), tenant.ID, "2026-03")
	if err != nil {
		t.Fatalf("GetBillingSummary failed: %v", err)
	}

	if summary.TotalCost <= 0 {
		t.Error("expected positive total cost")
	}
	if len(summary.CostByResource) == 0 {
		t.Error("expected cost breakdown by resource")
	}
	if len(summary.CostByNamespace) == 0 {
		t.Error("expected cost breakdown by namespace")
	}
}

func TestGenerateInvoice(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "invoice-test", TierStarter)

	m.RecordUsage(context.Background(), tenant.ID, "gpu", 5.0, "ns1", "c1")
	m.RecordUsage(context.Background(), tenant.ID, "cpu", 50.0, "ns1", "c1")

	invoice, err := m.GenerateInvoice(context.Background(), tenant.ID, "2026-03")
	if err != nil {
		t.Fatalf("GenerateInvoice failed: %v", err)
	}

	if invoice.Status != "issued" {
		t.Errorf("expected issued status, got %q", invoice.Status)
	}
	if invoice.Total <= 0 {
		t.Error("expected positive invoice total")
	}
	if len(invoice.LineItems) == 0 {
		t.Error("expected line items in invoice")
	}

	invoices := m.GetInvoices(context.Background(), tenant.ID)
	if len(invoices) != 1 {
		t.Errorf("expected 1 invoice, got %d", len(invoices))
	}
}

// ============================================================================
// Namespace & Network Tests
// ============================================================================

func TestAddNamespace(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "ns-test", TierStarter)

	err := m.AddNamespace(context.Background(), tenant.ID, "ml-training")
	if err != nil {
		t.Fatalf("AddNamespace failed: %v", err)
	}

	got, _ := m.GetTenant(context.Background(), tenant.ID)
	if len(got.Namespaces) != 2 {
		t.Errorf("expected 2 namespaces, got %d", len(got.Namespaces))
	}
}

func TestAddDuplicateNamespace(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "dup-ns", TierFree)

	err := m.AddNamespace(context.Background(), tenant.ID, "tenant-dup-ns")
	if err == nil {
		t.Error("expected error for duplicate namespace")
	}
}

func TestUpdateNetworkIsolation(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "net-test", TierPro)

	newNet := NetworkIsolation{
		Enabled:         true,
		VPCEnabled:      true,
		VPCID:           "vpc-12345",
		VPCCidr:         "10.99.0.0/16",
		DNSPolicy:       "custom",
		AllowedCIDRs:    []string{"10.0.0.0/8"},
	}

	err := m.UpdateNetworkIsolation(context.Background(), tenant.ID, newNet)
	if err != nil {
		t.Fatalf("UpdateNetworkIsolation failed: %v", err)
	}

	got, _ := m.GetTenant(context.Background(), tenant.ID)
	if got.Network.VPCID != "vpc-12345" {
		t.Errorf("expected vpc-12345, got %q", got.Network.VPCID)
	}
}

// ============================================================================
// Enterprise Tier Features Tests
// ============================================================================

func TestEnterpriseTenantFeatures(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "ent-test", TierEnterprise)

	if !tenant.Encryption.EncryptSecrets {
		t.Error("expected EncryptSecrets for enterprise")
	}
	if !tenant.Encryption.EncryptVolumes {
		t.Error("expected EncryptVolumes for enterprise")
	}
	if !tenant.Encryption.EncryptBackups {
		t.Error("expected EncryptBackups for enterprise")
	}
	if !tenant.Encryption.EncryptTransit {
		t.Error("expected EncryptTransit for enterprise")
	}
	if !tenant.Network.VPCEnabled {
		t.Error("expected VPC enabled for enterprise")
	}
	if !tenant.Network.ServiceMeshIsolation {
		t.Error("expected service mesh isolation for enterprise")
	}
}

func TestDefaultQuotaByTier(t *testing.T) {
	tests := []struct {
		tier     TenantTier
		gpuCount int
	}{
		{TierFree, 0},
		{TierStarter, 2},
		{TierPro, 16},
		{TierEnterprise, 128},
	}

	for _, tt := range tests {
		q := DefaultQuotaByTier(tt.tier)
		if q.MaxGPUCount != tt.gpuCount {
			t.Errorf("tier %s: expected GPU %d, got %d", tt.tier, tt.gpuCount, q.MaxGPUCount)
		}
	}
}

func TestCancelledContext(t *testing.T) {
	m := newTestManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.CreateTenant(ctx, "ctx-test", "Ctx", "o", "o@t.com", TierFree)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// ============================================================================
// Quota Snapshot & Cost Allocation Tests
// ============================================================================

func TestGetQuotaSnapshot(t *testing.T) {
	m := newTestManager()
	tenant := createTestTenant(t, m, "snap-test", TierPro)

	// Set usage
	m.UpdateUsage(context.Background(), tenant.ID, &QuotaUsage{
		CPUMillicores: 8000, MemoryBytes: 16 * 1024 * 1024 * 1024,
		GPUCount: 4, StorageBytes: 100 * 1024 * 1024 * 1024, Pods: 50,
	})

	snap, err := m.GetQuotaSnapshot(tenant.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.TenantName != "snap-test" {
		t.Errorf("expected tenant name 'snap-test', got %q", snap.TenantName)
	}
	if snap.Tier != TierPro {
		t.Errorf("expected tier pro, got %s", snap.Tier)
	}
	if len(snap.Resources) < 4 {
		t.Errorf("expected at least 4 resource entries, got %d", len(snap.Resources))
	}

	cpuEntry := snap.Resources[QuotaCPU]
	if cpuEntry == nil {
		t.Fatal("expected CPU entry")
	}
	if cpuEntry.CurrentUsage != 8000 {
		t.Errorf("expected CPU usage 8000, got %d", cpuEntry.CurrentUsage)
	}
	util := cpuEntry.UtilizationPercent()
	if util < 10 || util > 20 {
		t.Errorf("expected CPU utilization ~12.5%%, got %.1f%%", util)
	}
}

func TestGetQuotaSnapshotNotFound(t *testing.T) {
	m := newTestManager()
	_, err := m.GetQuotaSnapshot("nonexistent")
	if err == nil {
		t.Error("expected error for unknown tenant")
	}
}

func TestQuotaEntryUtilization(t *testing.T) {
	e := QuotaEntry{HardLimit: 100, CurrentUsage: 50, Enforced: true}
	if e.UtilizationPercent() != 50.0 {
		t.Errorf("expected 50%%, got %.1f%%", e.UtilizationPercent())
	}
	if e.IsExceeded() {
		t.Error("should not be exceeded")
	}

	e.CurrentUsage = 110
	if !e.IsExceeded() {
		t.Error("should be exceeded")
	}
}

func TestQuotaEntryWarning(t *testing.T) {
	e := QuotaEntry{HardLimit: 100, SoftLimit: 80, CurrentUsage: 85}
	if !e.IsWarning() {
		t.Error("expected warning at 85/80")
	}
	e.CurrentUsage = 70
	if e.IsWarning() {
		t.Error("should not warn at 70/80")
	}
}

func TestGenerateCostAllocationReport(t *testing.T) {
	m := newTestManager()
	t1 := createTestTenant(t, m, "cost-t1", TierPro)
	t2 := createTestTenant(t, m, "cost-t2", TierStarter)

	m.UpdateUsage(context.Background(), t1.ID, &QuotaUsage{
		CPUMillicores: 16000, MemoryBytes: 32 * 1024 * 1024 * 1024,
		GPUCount: 4, StorageBytes: 500 * 1024 * 1024 * 1024,
	})
	m.UpdateUsage(context.Background(), t2.ID, &QuotaUsage{
		CPUMillicores: 4000, MemoryBytes: 8 * 1024 * 1024 * 1024,
	})

	now := time.Now()
	report := m.GenerateCostAllocationReport(now.Add(-24*time.Hour), now)

	if report.ID == "" {
		t.Error("expected non-empty report ID")
	}
	if len(report.TenantCosts) != 2 {
		t.Errorf("expected 2 tenant costs, got %d", len(report.TenantCosts))
	}
	if report.TotalCost <= 0 {
		t.Errorf("expected positive total cost, got %f", report.TotalCost)
	}
	if report.UnallocatedCost <= 0 {
		t.Error("expected positive unallocated (shared infra) cost")
	}

	// First should be higher cost (t1 has more resources)
	if report.TenantCosts[0].TotalCost < report.TenantCosts[1].TotalCost {
		t.Error("expected first tenant to have higher cost (sorted desc)")
	}

	for _, tc := range report.TenantCosts {
		if tc.CostPercent <= 0 {
			t.Errorf("expected positive cost percent for %s", tc.TenantName)
		}
		if len(tc.ResourceCosts) == 0 {
			t.Errorf("expected resource cost items for %s", tc.TenantName)
		}
	}
}

func TestGetTopConsumers(t *testing.T) {
	m := newTestManager()
	t1 := createTestTenant(t, m, "top-1", TierEnterprise)
	t2 := createTestTenant(t, m, "top-2", TierPro)
	createTestTenant(t, m, "top-3", TierFree)

	m.UpdateUsage(context.Background(), t1.ID, &QuotaUsage{CPUMillicores: 50000})
	m.UpdateUsage(context.Background(), t2.ID, &QuotaUsage{CPUMillicores: 20000})

	top := m.GetTopConsumers(QuotaCPU, 2)
	if len(top) != 2 {
		t.Fatalf("expected 2, got %d", len(top))
	}
	if top[0].Usage != 50000 {
		t.Errorf("expected top consumer with 50000, got %d", top[0].Usage)
	}
	if top[1].Usage != 20000 {
		t.Errorf("expected second consumer with 20000, got %d", top[1].Usage)
	}
}

func TestDefaultCostRates(t *testing.T) {
	rates := DefaultCostRates()
	if len(rates) < 4 {
		t.Errorf("expected at least 4 cost rates, got %d", len(rates))
	}
	for _, r := range rates {
		if r.UnitPrice <= 0 {
			t.Errorf("rate %s: expected positive unit price", r.ResourceType)
		}
	}
}
