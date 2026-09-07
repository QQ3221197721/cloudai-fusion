package license

import (
	"fmt"
	"testing"
)

func TestLicenseManager_New(t *testing.T) {
	lm, err := NewLicenseManager(true) // Debug mode for testing
	if err != nil {
		t.Fatalf("Failed to create LicenseManager: %v", err)
	}

	if lm == nil {
		t.Error("LicenseManager should not be nil")
	}

	if !lm.DebugMode {
		t.Error("DebugMode should be true")
	}
}

func TestLicenseManager_ValidateDebugMode(t *testing.T) {
	lm, err := NewLicenseManager(true)
	if err != nil {
		t.Fatalf("Failed to create LicenseManager: %v", err)
	}

	// Empty key should work in debug mode
	license, err := lm.ValidateLicense("")
	if err != nil {
		t.Fatalf("Debug mode should accept empty key: %v", err)
	}

	if license.LicenseType != Enterprise {
		t.Errorf("Expected enterprise license in debug mode, got %s", license.LicenseType)
	}
}

func TestLicense_CreateAndValidate(t *testing.T) {
	lm, err := NewLicenseManager(false) // Non-debug mode
	if err != nil {
		t.Fatalf("Failed to create LicenseManager: %v", err)
	}

	// Create a professional license
	createdLicense, key, err := lm.CreateLicense(
		"tenant-12345",
		Professional,
		30, // 30 days
		[]string{"exploit_execution", "credential_dumping"},
		500, // max targets
	)

	if err != nil {
		t.Fatalf("Failed to create license: %v", err)
	}

	if key == "" {
		t.Error("License key should not be empty")
	}

	if createdLicense.TenantID != "tenant-12345" {
		t.Errorf("Expected tenant ID 'tenant-12345', got '%s'", createdLicense.TenantID)
	}

	if createdLicense.LicenseType != Professional {
		t.Errorf("Expected professional license, got %s", createdLicense.LicenseType)
	}

	// Validate the license we just created
	validated, err := lm.ValidateLicense(key)
	if err != nil {
		t.Fatalf("Failed to validate created license: %v", err)
	}

	if validated.TenantID != "tenant-12345" {
		t.Errorf("Tenant mismatch after validation")
	}
}

func TestLicense_FeatureAccess(t *testing.T) {
	lm, _ := NewLicenseManager(false)

	// Create community license
	communityLicense, _, _ := lm.CreateLicense(
		"tenant-community",
		Community,
		7,
		[]string{"basic_vulnerability_scan", "user_enumeration"},
		100,
	)

	// Create enterprise license
	enterpriseLicense, _, _ := lm.CreateLicense(
		"tenant-enterprise",
		Enterprise,
		365,
		[]string{"all"},
		-1, // unlimited
	)

	tests := []struct {
		name          string
		license       *LicenseInfo
		feature       string
		expected      bool
		description   string
	}{
		{
			name:      "community-basic-feature",
			license:   communityLicense,
			feature:   "basic_vulnerability_scan",
			expected:  true,
			description: "Community tier should have basic features",
		},
		{
			name:      "community-exploit-feature",
			license:   communityLicense,
			feature:   "exploit_execution",
			expected:  false,
			description: "Community tier should NOT have exploit features",
		},
		{
			name:      "enterprise-all-features",
			license:   enterpriseLicense,
			feature:   "exploit_execution",
			expected:  true,
			description: "Enterprise tier should have all features",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canAccess := lm.FeatureAccess(tt.license, tt.feature)
			if canAccess != tt.expected {
				t.Errorf("%s: FeatureAccess returned %v, expected %v (%s)",
					tt.name, canAccess, tt.expected, tt.description)
			}
		})
	}
}

func TestLicense_TierComparison(t *testing.T) {
	tests := []struct {
		licenseType LicenseType
		expected    int
	}{
		{Community, 1},
		{Professional, 2},
		{Enterprise, 3},
		{"invalid", 0},
	}

	for _, tt := range tests {
		got := GetLicenseTier(tt.licenseType)
		if got != tt.expected {
			t.Errorf("GetLicenseTier(%s) = %d, want %d", tt.licenseType, got, tt.expected)
		}
	}
}

func TestLicense_RemainingTargets(t *testing.T) {
	lm, _ := NewLicenseManager(false)

	unlimitedLicense, _, _ := lm.CreateLicense(
		"tenant-unlimited",
		Enterprise,
		365,
		[]string{"all"},
		-1, // unlimited
	)

	limitedLicense, _, _ := lm.CreateLicense(
		"tenant-limited",
		Professional,
		30,
		[]string{"all"},
		500, // limited
	)

	tests := []struct {
		name           string
		license        *LicenseInfo
		used           int
		expectedResult int
		description    string
	}{
		{
			name:           "unlimited-license",
			license:        unlimitedLicense,
			used:           999999,
			expectedResult: -1,
			description:    "Unlimited should always return -1",
		},
		{
			name:           "limited-license-with-quota",
			license:        limitedLicense,
			used:           200,
			expectedResult: 300,
			description:    "Should calculate remaining quota correctly",
		},
		{
			name:           "limited-license-exhausted",
			license:        limitedLicense,
			used:           500,
			expectedResult: 0,
			description:    "Should return 0 when exhausted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := lm.GetRemainingTargets(tt.license, tt.used)
			if result != tt.expectedResult {
				t.Errorf("GetRemainingTargets(%d) = %d, want %d (%s)",
					tt.used, result, tt.expectedResult, tt.description)
			}
		})
	}
}

func TestGenerateDemoLicense(t *testing.T) {
	key := GenerateDemoLicense("test-tenant", 14)
	
	if key == "" {
		t.Error("Generated demo license key should not be empty")
	}

	fmt.Printf("Demo license key: %s\n", key)
}

// Example usage
func ExampleNewLicenseManager() {
	lm, err := NewLicenseManager(true) // Enable debug mode for development
	if err != nil {
		panic(err)
	}

	license, err := lm.ValidateLicense("")
	if err != nil {
		panic(err)
	}

	fmt.Printf("License Type: %s\n", license.LicenseType)
	fmt.Printf("Features: %v\n", license.Features)
}

// Benchmark performance
func BenchmarkLicenseCreate(b *testing.B) {
	lm, _ := NewLicenseManager(false)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = lm.CreateLicense(
			fmt.Sprintf("tenant-%d", i),
			Professional,
			30,
			[]string{"feature1", "feature2"},
			1000,
		)
	}
}

func BenchmarkLicenseValidate(b *testing.B) {
	lm, _ := NewLicenseManager(false)
	
	// Pre-create licenses for benchmark
	keys := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		_, key, _ := lm.CreateLicense(
			fmt.Sprintf("tenant-%d", i),
			Professional,
			30,
			nil,
			-1,
		)
		keys[i] = key
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lm.ValidateLicense(keys[i])
	}
}
