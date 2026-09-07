package features

import (
	"fmt"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/license"
)

func TestFeatureFlags_New(t *testing.T) {
	ff := NewFeatureFlags()

	if ff == nil {
		t.Error("FeatureFlags should not be nil")
	}

	// Check that default community features are enabled
	if !ff.IsEnabled(BasicVulnerabilityScan) {
		t.Error("Basic vulnerability scanning should be enabled by default")
	}
}

func TestFeatureFlags_AllFeatures(t *testing.T) {
	ff := NewFeatureFlags()

	allFeatures := ff.GetAllFeatures()

	if len(allFeatures) == 0 {
		t.Error("Should have registered features")
	}

	fmt.Printf("Total registered features: %d\n", len(allFeatures))
}

func TestFeatureFlags_LicenseGating(t *testing.T) {
	ff := NewFeatureFlags()

	communityLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"tenant-community",
		license.Community,
		7,
		nil,
		100,
	)

	proLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"tenant-pro",
		license.Professional,
		30,
		[]string{"exploit_execution"},
		1000,
	)

	enterpriseLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"tenant-enterprise",
		license.Enterprise,
		365,
		[]string{"all"},
		-1,
	)

	tests := []struct {
		name        string
		featureID   FeatureID
		licenses    []*license.LicenseInfo
		expectations []bool // Expected access result for each license
	}{
		{
			name:      "community-basic-access",
			featureID: BasicVulnerabilityScan,
			licenses:  []*license.LicenseInfo{communityLicense, proLicense, enterpriseLicense},
			expectations: []bool{true, true, true}, // All tiers can access
		},
		{
			name:      "credential-dumping-tier-gating",
			featureID: CredentialDumping,
			licenses:  []*license.LicenseInfo{communityLicense, proLicense, enterpriseLicense},
			expectations: []bool{false, true, true}, // Community no, Pro and Enterprise yes
		},
		{
			name:      "persistence-enterprise-only",
			featureID: PersistenceMechanisms,
			licenses:  []*license.LicenseInfo{communityLicense, proLicense, enterpriseLicense},
			expectations: []bool{false, false, true}, // Only Enterprise
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := ff.GetFeatureByID(tt.featureID)

			for i, lic := range tt.licenses {
				canAccess := ff.CanAccessWithLicense(tt.featureID, lic)
				expected := tt.expectations[i]

				if canAccess != expected {
					t.Errorf("[%s]%s - license=%s, expected=%v, got=%v",
						info.ID, info.Name, lic.LicenseType, expected, canAccess)
				}
			}
		})
	}
}

func TestFeatureFlags_EnableDisable(t *testing.T) {
	ff := NewFeatureFlags()

	// Disable a feature
	ff.Disable(BasicVulnerabilityScan, "Maintenance mode")

	if ff.IsEnabled(BasicVulnerabilityScan) {
		t.Error("Feature should be disabled after calling Disable")
	}

	reason := ff.GetDisableReason(BasicVulnerabilityScan)
	if reason != "Maintenance mode" {
		t.Errorf("Expected disable reason 'Maintenance mode', got '%s'", reason)
	}

	// Re-enable the feature
	ff.Enable(BasicVulnerabilityScan, "")

	if !ff.IsEnabled(BasicVulnerabilityScan) {
		t.Error("Feature should be enabled after calling Enable")
	}
}

func TestFeatureFlags_GetAvailableFeatures(t *testing.T) {
	ff := NewFeatureFlags()

	communityLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"available-tenant",
		license.Community,
		7,
		nil,
		100,
	)

	availableFeatures := ff.GetAvailableFeatures(communityLicense)

	if len(availableFeatures) == 0 {
		t.Error("Community tenant should have some features available")
	}

	fmt.Printf("Community tier has %d features available\n", len(availableFeatures))

	// Verify basic features are in the list
	hasBasic := false
	for _, f := range availableFeatures {
		if f == BasicVulnerabilityScan {
			hasBasic = true
			break
		}
	}

	if !hasBasic {
		t.Error("BasicVulnerabilityScan should be available to community tier")
	}
}

func TestFeatureFlags_Hooks(t *testing.T) {
	ff := NewFeatureFlags()

	hookCalled := false
	ff.RegisterHook(BasicVulnerabilityScan, func() {
		hookCalled = true
	})

	ff.Enable(BasicVulnerabilityScan, "Testing hooks")

	if !hookCalled {
		t.Error("Hook should have been called when enabling feature")
	}
}

func TestFeatureFlags_ExportImportConfiguration(t *testing.T) {
	ff := NewFeatureFlags()

	configJSON, err := ff.ExportConfiguration()
	if err != nil {
		t.Fatalf("Failed to export configuration: %v", err)
	}

	if len(configJSON) == 0 {
		t.Error("Exported configuration should not be empty")
	}

	// Create new instance and import
	newFF := NewFeatureFlags()
	err = newFF.ImportConfiguration(configJSON)
	if err != nil {
		t.Fatalf("Failed to import configuration: %v", err)
	}

	// Verify imported state matches
	if len(newFF.enabledFeatures) != len(ff.enabledFeatures) {
		t.Error("Imported features count doesn't match original")
	}
}

func TestFeatureFlags_IsLicensedFeature(t *testing.T) {
	ff := NewFeatureFlags()

	tests := []struct {
		featureID FeatureID
		isLicensed bool
	}{
		{BasicVulnerabilityScan, false}, // Community feature
		{CredentialDumping, true},       // Requires upgrade
		{PersistenceMechanisms, true},   // Enterprise only
		{RedTeamOperations, true},       // Enterprise only
	}

	for _, tt := range tests {
		t.Run(string(tt.featureID), func(t *testing.T) {
			isLicensing := ff.IsLicensedFeature(tt.featureID)
			if isLicensing != tt.isLicensed {
				t.Errorf("IsLicensedFeature(%s) = %v, want %v", 
					tt.featureID, isLicensing, tt.isLicensed)
			}
		})
	}
}

func TestFeatureFlags_Upgrade(t *testing.T) {
	ff := NewFeatureFlags()

	oldLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"upgrade-tenant",
		license.Community,
		7,
		nil,
		100,
	)

	newLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"upgrade-tenant",
		license.Professional,
		30,
		[]string{"all"},
		1000,
	)

	// Get available features before upgrade
	beforeCount := len(ff.GetAvailableFeatures(oldLicense))

	// Upgrade features
	ff.UpgradeFeatures(oldLicense, newLicense)

	afterCount := len(ff.GetAvailableFeatures(newLicense))

	if afterCount <= beforeCount {
		t.Error("Number of available features should increase after upgrade")
	}

	fmt.Printf("Before: %d features, After: %d features\n", beforeCount, afterCount)
}

func BenchmarkFeatureIsEnabled(b *testing.B) {
	ff := NewFeatureFlags()
	proLicense, _, _ := license.NewLicenseManager(false).CreateLicense(
		"bench-tenant",
		license.Professional,
		30,
		nil,
		1000,
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ff.CanAccessWithLicense(BasicVulnerabilityScan, proLicense)
	}
}

func BenchmarkFeatureGetAll(b *testing.B) {
	ff := NewFeatureFlags()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ff.GetAllFeatures()
	}
}

// Example usage
func ExampleNewFeatureFlags() {
	ff := NewFeatureFlags()

	// Check if a feature is available
	if ff.IsEnabled(BasicVulnerabilityScan) {
		fmt.Println("Basic vulnerability scanning is available")
	}

	// Check licensing requirements
	if ff.IsLicensedFeature(CredentialDumping) {
		fmt.Println("Credential dumping requires license upgrade")
		
		reason := ff.GetDisableReason(CredentialDumping)
		fmt.Printf("Requirement: %s\n", reason)
	}
}

func ExampleFeatureFlags_CustomConfig() {
	ff := NewFeatureFlags()

	// Load custom configuration from database
	customConfig := `{
		"config_source": "database",
		"enabled": {
			"basic_vulnerability_scan": true,
			"custom_feature": true
		}
	}`

	_ = ff.ImportConfiguration([]byte(customConfig))
	_ = ff
}
