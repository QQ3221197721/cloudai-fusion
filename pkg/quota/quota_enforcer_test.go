package quota

import (
	"fmt"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/license"
)

func TestQuotaEnforcer_New(t *testing.T) {
	qe := NewQuotaEnforcer()

	if qe == nil {
		t.Error("QuotaEnforcer should not be nil")
	}

	if qe.LocalCache == nil {
		t.Error("LocalCache should not be nil")
	}
}

func TestQuotaEnforcer_EnforceCommunityQuota(t *testing.T) {
	qe := NewQuotaEnforcer()

	// Create community license with limited quota
	communityLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"community-tenant",
		license.Community,
		7,
		[]string{"basic_vulnerability_scan", "user_enumeration"},
		100,
	)

	tests := []struct {
		name      string
		operation OperationType
		targets   int
		shouldErr bool
	}{
		{
			name:      "valid-scan-under-limit",
			operation: VulnerabilityScan,
			targets:   50,
			shouldErr: false,
		},
		{
			name:      "exceed-hourly-limit",
			operation: VulnerabilityScan,
			targets:   60, // Exceed hourly limit
			shouldErr: true,
		},
		{
			name:      "credential-dump-forbidden",
			operation: CredentialDump,
			targets:   1,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := qe.EnforceQuota("community-tenant", tt.operation, tt.targets, communityLicense)

			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}

			if !tt.shouldErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestQuotaEnforcer_EnforceProfessionalQuota(t *testing.T) {
	qe := NewQuotaEnforcer()

	professionalLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"pro-tenant",
		license.Professional,
		30,
		[]string{"exploit_execution", "credential_dumping"},
		1000,
	)

	tests := []struct {
		name           string
		operation      OperationType
		targets        int
		shouldErr      bool
		expectedError  string
	}{
		{
			name:          "valid-exploit-operation",
			operation:     ExploitExecution,
			targets:       10,
			shouldErr:     false,
		},
		{
			name:          "exceed-monthly-quota",
			operation:     PayloadUpload,
			targets:       200, // More than monthly limit for professional
			shouldErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := qe.EnforceQuota("pro-tenant", tt.operation, tt.targets, professionalLicense)

			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}

			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestQuotaEnforcer_EnforceEnterpriseUnlimited(t *testing.T) {
	qe := NewQuotaEnforcer()

	enterpriseLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"enterprise-tenant",
		license.Enterprise,
		365,
		[]string{"all"},
		-1, // unlimited
	)

	// Enterprise should never hit quota limits
	for i := 0; i < 10; i++ {
		err := qe.EnforceQuota("enterprise-tenant", VulnerabilityScan, 1000, enterpriseLicense)
		if err != nil {
			t.Errorf("Enterprise tenant should have unlimited quota: %v", err)
		}
	}
}

func TestQuotaEnforcer_GetQuotaStatus(t *testing.T) {
	qe := NewQuotaEnforcer()

	communityLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"status-tenant",
		license.Community,
		7,
		nil,
		100,
	)

	// Make some quota calls first
	qe.EnforceQuota("status-tenant", VulnerabilityScan, 10, communityLicense)
	qe.EnforceQuota("status-tenant", VulnerabilityScan, 20, communityLicense)

	status := qe.GetQuotaStatus("status-tenant", VulnerabilityScan, communityLicense)

	if status.Operation != VulnerabilityScan {
		t.Errorf("Wrong operation in status")
	}

	if status.UsedThisMonth != 30 {
		t.Errorf("Expected UsedThisMonth=30, got %d", status.UsedThisMonth)
	}

	if status.IsUnlimited {
		t.Error("Community tier should not be unlimited")
	}

	fmt.Printf("Quota Status: %+v\n", status)
}

func TestQuotaEnforcer_BulkCheckMultiple(t *testing.T) {
	qe := NewQuotaEnforcer()

	proLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"bulk-tenant",
		license.Professional,
		30,
		nil,
		1000,
	)

	operations := []OperationType{VulnerabilityScan, UserEnumeration, SystemDiscovery}
	counts := map[OperationType]int{
		VulnerabilityScan:  50,
		UserEnumeration:    20,
		SystemDiscovery:    30,
	}

	err := qe.BulkCheckMultiple("bulk-tenant", operations, counts, proLicense)
	if err != nil {
		t.Errorf("Bulk check should succeed: %v", err)
	}
}

func TestQuotaEnforcer_LocalCache(t *testing.T) {
	cache := &LocalCacheStore{
		data: make(map[string]int),
	}

	// Set a value
	cache.Increment("test-key", 10)
	
	// Get the value
	value := cache.Get("test-key")
	if value != 10 {
		t.Errorf("Expected 10, got %d", value)
	}

	// Increment again
	cache.Increment("test-key", 5)
	value = cache.Get("test-key")
	if value != 15 {
		t.Errorf("Expected 15 after increment, got %d", value)
	}

	// Clear
	cache.Clear("test-key")
	value = cache.Get("test-key")
	if value != 0 {
		t.Errorf("Expected 0 after clear, got %d", value)
	}
}

func TestQuotaEnforcer_IsWithinQuota(t *testing.T) {
	qe := NewQuotaEnforcer()

	communityLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"iswithin-tenant",
		license.Community,
		7,
		nil,
		100,
	)

	// Check quota without consuming
	within := qe.IsWithinQuota("iswithin-tenant", VulnerabilityScan, 50, communityLicense)
	if !within {
		t.Error("Should be within quota")
	}

	// Consume most of quota
	for i := 0; i < 99; i++ {
		qe.EnforceQuota("iswithin-tenant", VulnerabilityScan, 1, communityLicense)
	}

	// Now should be at limit
	within = qe.IsWithinQuota("iswithin-tenant", VulnerabilityScan, 1, communityLicense)
	if !within {
		t.Error("Should still be within quota (1 more)")
	}

	// This should exceed
	within = qe.IsWithinQuota("iswithin-tenant", VulnerabilityScan, 2, communityLicense)
	if within {
		t.Error("Should exceed quota with 2 more")
	}
}

// Benchmark performance
func BenchmarkQuotaEnforce(b *testing.B) {
	qe := NewQuotaEnforcer()

	proLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"bench-tenant",
		license.Professional,
		30,
		nil,
		10000,
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		qe.EnforceQuota("bench-tenant", VulnerabilityScan, 1, proLicense)
	}
}

func BenchmarkQuotaGetStatus(b *testing.B) {
	qe := NewQuotaEnforcer()

	proLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"status-bench-tenant",
		license.Professional,
		30,
		nil,
		1000,
	)

	// Initialize usage
	for i := 0; i < 100; i++ {
		qe.EnforceQuota("status-bench-tenant", VulnerabilityScan, 1, proLicense)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = qe.GetQuotaStatus("status-bench-tenant", VulnerabilityScan, proLicense)
	}
}

// Example usage
func ExampleNewQuotaEnforcer() {
	qe := NewQuotaEnforcer()

	// Load or validate license
	lm, _ := license.NewLicenseManager(false)
	licenseInfo, key, _ := lm.CreateLicense(
		"example-tenant",
		license.Professional,
		30,
		nil,
		1000,
	)

	fmt.Printf("Generated License Key: %s\n", key)

	// Enforce quota for an operation
	err := qe.EnforceQuota("example-tenant", VulnerabilityScan, 100, licenseInfo)
	if err != nil {
		panic(err)
	}

	// Check current quota status
	status := qe.GetQuotaStatus("example-tenant", VulnerabilityScan, licenseInfo)
	fmt.Printf("Remaining quota: %d\n", status.Remaining)
}

func ExampleQuotaEnforcer_SetRedisClient() {
	qe := NewQuotaEnforcer()
	
	// In production, set up Redis client for distributed caching
	var redisClient interface{} // Replace with actual Redis client
	
	qe.SetRedisClient(redisClient)
	
	_ = qe
}
