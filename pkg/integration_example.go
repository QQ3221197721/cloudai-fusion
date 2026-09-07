package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/features"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/license"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/quota"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
)

// IntegrationDemo demonstrates how all 4 layers work together
func main() {
	fmt.Println("=== CloudAI Fusion 多层访问控制系统演示 ===\n")

	// =====================================================================
	// STEP 1: Initialize Systems
	// =====================================================================
	fmt.Println("📦 Initializing security systems...")

	lm, err := license.NewLicenseManager(false) // Production mode
	if err != nil {
		panic(err)
	}

	ff := features.NewFeatureFlags()

	qe := quota.NewQuotaEnforcer()

	detectorConfig := &security.DetectorConfig{
		Enabled:                    true,
		AutoBlockThreshold:         0.8,
		MaxActionsBuffered:         10000,
		EnableRealTimeAnalysis:     true,
		EnableBatchAnalysis:        true,
		Allowlist:                  make(map[string]bool),
		Blocklist:                  make(map[string]bool),
	}

	anomalyDetector := security.NewAnomalyDetector(detectorConfig, ff)

	fmt.Println("✅ All systems initialized\n")

	// =====================================================================
	// STEP 2: Create Different License Tiers
	// =====================================================================
	fmt.Println("👤 Creating sample tenants with different licenses...\n")

	tenants := map[string]*license.LicenseInfo{}

	// Community Tenant (Free)
	communityLicense, communityKey, _ := lm.CreateLicense(
		"tenant-community",
		license.Community,
		7, // 7 days trial
		nil,
		100, // max targets
	)
	fmt.Printf("🔸 Community Tenant:\n")
	fmt.Printf("   Key: %s\n", communityKey)
	fmt.Printf("   Type: %s\n", communityLicense.LicenseType)
	fmt.Printf("   Max Targets: %d\n\n", communityLicense.MaxTargets)

	tenants["community"] = communityLicense

	// Professional Tenant ($99/month)
	proLicense, proKey, _ := lm.CreateLicense(
		"tenant-pro",
		license.Professional,
		30,
		[]string{"exploit_execution", "credential_dumping"},
		1000,
	)
	fmt.Printf("💎 Professional Tenant:\n")
	fmt.Printf("   Key: %s\n", proKey)
	fmt.Printf("   Type: %s\n", proLicense.LicenseType)
	fmt.Printf("   Max Targets: %d\n\n", proLicense.MaxTargets)

	tenants["pro"] = proLicense

	// Enterprise Tenant ($499/month)
	enterpriseLicense, enterpriseKey, _ := lm.CreateLicense(
		"tenant-enterprise",
		license.Enterprise,
		365,
		[]string{"all"},
		-1, // unlimited
	)
	fmt.Printf("👑 Enterprise Tenant:\n")
	fmt.Printf("   Key: %s\n", enterpriseKey)
	fmt.Printf("   Type: %s\n", enterpriseLicense.LicenseType)
	fmt.Printf("   Max Targets: %d (unlimited)\n\n", enterpriseLicense.MaxTargets)

	tenants["enterprise"] = enterpriseLicense

	// =====================================================================
	// STEP 3: Test Feature Access Control
	// =====================================================================
	fmt.Println("🔒 Testing feature access control...\n")

	featuresToTest := []features.FeatureID{
		features.BasicVulnerabilityScan,
		features.CredentialDumping,
		features.ExploitExecution,
		features.PersistenceMechanisms,
		features.RedTeamOperations,
	}

	for tenantName, lic := range tenants {
		fmt.Printf("🏢 %s tier (%s):\n", tenantName, lic.LicenseType)
		
		for _, feature := range featuresToTest {
			info := ff.GetFeatureByID(feature)
			canAccess := ff.CanAccessWithLicense(feature, lic)

			status := "❌"
			if canAccess {
				status = "✅"
			}

			fmt.Printf("   %s %-30s (%s)\n", status, info.Name, info.ID)
		}
		fmt.Println()
	}

	// =====================================================================
	// STEP 4: Test Quota Enforcement
	// =====================================================================
	fmt.Println("📊 Testing quota enforcement...\n")

	testOperations := []quota.OperationType{
		quota.VulnerabilityScan,
		quota.UserEnumeration,
		quota.PayloadUpload,
	}

	for tenantName, lic := range tenants {
		fmt.Printf("🏢 %s - Quota Status:\n", tenantName)

		for _, op := range testOperations {
			err := qe.EnforceQuota(tenantName, op, 1, lic)
			status := "✅ OK"
			if err != nil {
				status = fmt.Sprintf("❌ %v", err)
			}
			fmt.Printf("   %-25s: %s\n", op, status)
		}
		fmt.Println()
	}

	// =====================================================================
	// STEP 5: Test Anomaly Detection
	// =====================================================================
	fmt.Println("🚨 Testing anomaly detection...\n")

	// Simulate normal behavior for community tenant
	fmt.Println("📝 Testing normal behavior patterns:")
	normalActions := []security.UserAction{
		{
			Timestamp:   time.Now(),
			Operation:   "vulnerability_scan",
			TargetIP:    "192.168.1.100",
			TargetHost:  "web-server-01",
			Success:     true,
			Duration:    time.Second * 5,
			BytesSent:   1024,
		},
		{
			Timestamp:   time.Now().Add(time.Second * 2),
			Operation:   "user_enumeration",
			TargetIP:    "192.168.1.100",
			TargetHost:  "web-server-01",
			Success:     true,
			Duration:    time.Second * 3,
			BytesSent:   512,
		},
	}

	patterns := anomalyDetector.MonitorBehavior("tenant-community", normalActions[0])
	if len(patterns) == 0 {
		fmt.Println("   ✅ Normal scan - No issues detected")
	}

	// Simulate suspicious rapid exploit attempts
	fmt.Println("\n📝 Testing suspicious rapid-fire exploits:")
	suspiciousActions := make([]security.UserAction, 15)
	baseTime := time.Now()
	for i := 0; i < 15; i++ {
		suspiciousActions[i] = security.UserAction{
			Timestamp:   baseTime.Add(time.Duration(i*2) * time.Millisecond),
			Operation:   "exploit_execution",
			TargetIP:    "10.0.0." + fmt.Sprintf("%d", 100+i),
			Success:     true,
			Duration:    time.Millisecond * 500,
			BytesSent:   2048,
		}
	}

	patterns = anomalyDetector.MonitorBehavior("tenant-suspicious", suspiciousActions[0])
	for _, action := range suspiciousActions[1:] {
		anomalyDetector.MonitorBehavior("tenant-suspicious", action)
	}

	if len(patterns) > 0 {
		fmt.Println("   ⚠️ Suspicious activity DETECTED!")
		for _, pattern := range patterns {
			fmt.Printf("      Pattern: %s\n", pattern.PatternType)
			fmt.Printf("      Severity: %s\n", pattern.Severity)
			fmt.Printf("      Action: %s\n", pattern.RecommendedAction)
		}
	} else {
		fmt.Println("   ✅ No patterns detected")
	}

	// =====================================================================
	// STEP 6: Demonstrate Upgrade Path
	// =====================================================================
	fmt.Println("\n🔄 Demonstrating upgrade flow...\n")

	oldLicense := tenants["community"]
	newLicense, newKey, _ := lm.CreateLicense(
		"tenant-upgraded",
		license.Professional,
		30,
		nil,
		1000,
	)

	fmt.Printf("📈 Before upgrade (Community):\n")
	availableBefore := ff.GetAvailableFeatures(oldLicense)
	fmt.Printf("   Available features: %d\n", len(availableBefore))

	fmt.Printf("\n📈 After upgrade to Professional:\n")
	fmt.Printf("   New Key: %s\n", newKey)
	ff.UpgradeFeatures(oldLicense, newLicense)
	availableAfter := ff.GetAvailableFeatures(newLicense)
	fmt.Printf("   Available features: %d (+%d)\n", len(availableAfter), len(availableAfter)-len(availableBefore))

	// Show newly unlocked features
	fmt.Printf("\n🔓 Newly unlocked features:\n")
	for _, feature := range featuresToTest {
		info := ff.GetFeatureByID(feature)
		oldCanAccess := ff.CanAccessWithLicense(feature, oldLicense)
		newCanAccess := ff.CanAccessWithLicense(feature, newLicense)

		if !oldCanAccess && newCanAccess {
			fmt.Printf("   ✨ %s\n", info.Name)
		}
	}

	// =====================================================================
	// STEP 7: Summary
	// =====================================================================
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("🎉 INTEGRATION DEMO COMPLETE")
	fmt.Println(strings.Repeat("=", 50))

	fmt.Println("\n📋 Summary of Protected Layers:")
	fmt.Println("   1️⃣ License Manager - Digital signature validation")
	fmt.Println("   2️⃣ Quota Enforcer - Rate limiting per tier")
	fmt.Println("   3️⃣ Feature Flags - Smart access control")
	fmt.Println("   4️⃣ Anomaly Detector - Abuse prevention")

	fmt.Println("\n💡 Benefits:")
	fmt.Println("   ✅ Code stays fully open source (Apache 2.0)")
	fmt.Println("   ✅ Prevents commercial abuse")
	fmt.Println("   ✅ Clear upgrade path")
	fmt.Println("   ✅ Production-ready architecture")
	fmt.Println("   ✅ Audit trail for compliance")

	fmt.Println(strings.Repeat("=", 50))
