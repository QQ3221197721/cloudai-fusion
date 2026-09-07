package features

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/license"
)

// FeatureID represents a unique feature identifier
type FeatureID string

const (
	// Core scanning features
	BasicVulnerabilityScan  FeatureID = "basic_vulnerability_scan"
	TargetDiscovery         FeatureID = "target_discovery"
	Reporting               FeatureID = "reporting"
	UserEnumeration         FeatureID = "user_enumeration"
	AssetManagement         FeatureID = "asset_management"

	// Advanced offensive security features
	CredentialDumping       FeatureID = "credential_dumping"
	AutomatedExploitation   FeatureID = "automated_exploitation"
	PayloadDelivery         FeatureID = "payload_delivery"
	PersistenceMechanisms   FeatureID = "persistent_mechanisms"
	AutomatedPivoting       FeatureID = "automated_pivoting"

	// Enterprise features
	MultiTenantSupport      FeatureID = "multi_tenant_support"
	SingleSignOn            FeatureID = "single_sign_on"
	AuditLogging            FeatureID = "audit_logging"
	ComplianceReports       FeatureID = "compliance_reports"
	WhiteLabeling           FeatureID = "white_labeling"
	APIAccess               FeatureID = "api_access"

	// AI-powered features
	AIAnalyst               FeatureID = "ai_analyst"
	PredictiveAnalysis      FeatureID = "predictive_analysis"
	BehavioralAnalysis      FeatureID = "behavioral_analysis"
	HumanBehaviorSimulation FeatureID = "human_behavior_simulation"

	// Red team capabilities
	RedTeamOperations       FeatureID = "red_team_operations"
	ExploitExecution        FeatureID = "exploit_execution"
	LateralMovement         FeatureID = "lateral_movement"
	DataExfiltration        FeatureID = "data_exfiltration"
	DomainTraversal         FeatureID = "domain_traversal"

	// Integration features
	WebhookNotifications    FeatureID = "webhook_notifications"
	IntegrationHub          FeatureID = "integration_hub"
	ThirdPartyIntegrations  FeatureID = "third_party_integrations"
)

// FeatureInfo contains metadata about a feature
type FeatureInfo struct {
	ID              FeatureID
	Name            string
	Description     string
	DefaultEnabled  bool
	LicenseRequired license.LicenseType
	Hidden          bool // Don't show in UI if not enabled
}

// FeatureFlags manages feature availability based on license and configuration
type FeatureFlags struct {
	mutex           sync.RWMutex
	featureRegistry map[FeatureID]FeatureInfo
	enabledFeatures map[string]bool
	disabledReasons map[string]string
	hooks           map[FeatureID][]func()
	configSource    string // config source: "file", "db", "env"
}

// NewFeatureFlags creates a new initialized FeatureFlags system
func NewFeatureFlags() *FeatureFlags {
	ff := &FeatureFlags{
		featureRegistry: make(map[FeatureID]FeatureInfo),
		enabledFeatures: make(map[string]bool),
		disabledReasons: make(map[string]string),
		hooks:           make(map[FeatureID][]func()),
	}

	// Register all known features with their metadata
	ff.registerAllFeatures()

	return ff
}

// registerAllFeatures defines the complete feature catalog
func (ff *FeatureFlags) registerAllFeatures() {
	defaultFeatures := []FeatureInfo{
		// Core scanning (community)
		{BasicVulnerabilityScan, "基本漏洞扫描", "Core vulnerability scanning with basic checks", true, license.Community, false},
		{TargetDiscovery, "目标发现", "Automated target discovery and inventory", true, license.Community, false},
		{Reporting, "报告生成", "Generate detailed scan reports", true, license.Community, false},
		{UserEnumeration, "用户枚举", "Enumerate users on discovered targets", true, license.Community, false},
		{AssetManagement, "资产管理", "Manage and organize assets", true, license.Community, false},

		// Advanced offensive (professional+)
		{CredentialDumping, "凭证dumping", "Extract credentials from systems", false, license.Professional, false},
		{AutomatedExploitation, "自动化利用", "Automated exploit execution", false, license.Professional, false},
		{PayloadDelivery, "载荷投递", "Deliver payloads to targets", false, license.Professional, false},
		{PersistenceMechanisms, "持久化机制", "Establish persistence mechanisms", false, license.Enterprise, false},
		{AutomatedPivoting, "自动跳板", "Automatically pivot through network", false, license.Enterprise, false},

		// Enterprise features (enterprise only)
		{MultiTenantSupport, "多租户支持", "Multi-tenant architecture support", false, license.Enterprise, false},
		{SingleSignOn, "单点登录", "SSO integration", false, license.Enterprise, false},
		{AuditLogging, "审计日志", "Enhanced audit logging", false, license.Enterprise, false},
		{ComplianceReports, "合规报告", "Regulatory compliance reports", false, license.Enterprise, false},
		{WhiteLabeling, "白标功能", "Custom branding and white-labeling", false, license.Enterprise, false},
		{APIAccess, "API 访问", "Full API access", false, license.Enterprise, false},

		// AI features (professional+)
		{AIAnalyst, "AI 分析师", "AI-powered analysis assistant", false, license.Professional, false},
		{PredictiveAnalysis, "预测分析", "Predictive threat analysis", false, license.Professional, false},
		{BehavioralAnalysis, "行为分析", "User behavior analysis", false, license.Enterprise, false},
		{HumanBehaviorSimulation, "人类行为模拟", "Human-like behavior simulation", false, license.Enterprise, false},

		// Red team capabilities (enterprise only)
		{RedTeamOperations, "红队操作", "Full red team operations suite", false, license.Enterprise, false},
		{ExploitExecution, "漏洞利用执行", "Execute exploits against targets", false, license.Enterprise, false},
		{LateralMovement, "横向移动", "Network lateral movement techniques", false, license.Enterprise, false},
		{DataExfiltration, "数据外传", "Data exfiltration simulations", false, license.Enterprise, false},
		{DomainTraversal, "域遍历", "Active Directory traversal", false, license.Enterprise, false},

		// Integration features
		{WebhookNotifications, "Webhook 通知", "Configure webhook notifications", false, license.Professional, false},
		{IntegrationHub, "集成中心", "Central hub for integrations", false, license.Enterprise, false},
		{ThirdPartyIntegrations, "第三方集成", "Connect to third-party tools", false, license.Enterprise, false},
	}

	for _, f := range defaultFeatures {
		ff.featureRegistry[f.ID] = f
		if f.DefaultEnabled {
			ff.enabledFeatures[string(f.ID)] = true
		} else {
			reason := fmt.Sprintf("Requires %s license", f.LicenseRequired)
			ff.disabledReasons[string(f.ID)] = reason
		}
	}
}

// GetFeatureByID retrieves feature metadata by ID
func (ff *FeatureFlags) GetFeatureByID(id FeatureID) (FeatureInfo, bool) {
	ff.mutex.RLock()
	defer ff.mutex.RUnlock()

	info, ok := ff.featureRegistry[id]
	return info, ok
}

// GetAllFeatures returns all registered features
func (ff *FeatureFlags) GetAllFeatures() []FeatureInfo {
	ff.mutex.RLock()
	defer ff.mutex.RUnlock()

	features := make([]FeatureInfo, 0, len(ff.featureRegistry))
	for _, info := range ff.featureRegistry {
		features = append(features, info)
	}

	return features
}

// Enable marks a feature as available
func (ff *FeatureFlags) Enable(featureID FeatureID, customReason string) {
	ff.mutex.Lock()
	defer ff.mutex.Unlock()

	if _, exists := ff.featureRegistry[featureID]; !exists {
		return
	}

	ff.enabledFeatures[string(featureID)] = true
	
	if customReason != "" {
		delete(ff.disabledReasons, string(featureID))
	}
	
	// Trigger hooks
	for _, hook := range ff.hooks[featureID] {
		hook()
	}
}

// Disable marks a feature as unavailable
func (ff *FeatureFlags) Disable(featureID FeatureID, customReason string) {
	ff.mutex.Lock()
	defer ff.mutex.Unlock()

	if _, exists := ff.featureRegistry[featureID]; !exists {
		return
	}

	ff.enabledFeatures[string(featureID)] = false
	
	if customReason != "" {
		ff.disabledReasons[string(featureID)] = customReason
	} else {
		// Use default reason based on license requirement
		if info, ok := ff.featureRegistry[featureID]; ok {
			ff.disabledReasons[string(featureID)] = fmt.Sprintf("Requires %s license", info.LicenseRequired)
		}
	}

	// Remove from disabled reasons if being enabled
	if customReason == "" && customReason != "" {
		delete(ff.disabledReasons, string(featureID))
	}
}

// IsEnabled checks if feature is currently available
func (ff *FeatureFlags) IsEnabled(featureID FeatureID) bool {
	ff.mutex.RLock()
	defer ff.mutex.RUnlock()

	if info, ok := ff.featureRegistry[featureID]; ok {
		// Check if feature requires specific license tier
		return ff.checkFeatureAccess(featureID, nil, info)
	}

	return false
}

// checkFeatureAccess evaluates feature access with license constraints
func (ff *FeatureFlags) checkFeatureAccess(featureID FeatureID, licenseInfo *license.LicenseInfo, info FeatureInfo) bool {
	if licenseInfo == nil {
		// No license - only allow community features
		return ff.enabledFeatures[string(featureID)] && info.LicenseRequired == license.Community
	}

	// License exists - check if tier is sufficient
	requiredTier := info.LicenseRequired
	actualTier := license.GetLicenseTier(licenseInfo.LicenseType)
	requiredTierNum := license.GetLicenseTier(requiredTier)

	// If tenant's tier >= required tier, and feature is enabled, allow access
	return actualTier >= requiredTierNum && ff.enabledFeatures[string(featureID)]
}

// IsLicensedFeature returns if this is an enterprise feature requiring upgrade
func (ff *FeatureFlags) IsLicensedFeature(featureID FeatureID) bool {
	_, ok := ff.featureRegistry[featureID]
	if !ok {
		return false
	}

	info, _ := ff.featureRegistry[featureID]
	return info.LicenseRequired != license.Community
}

// GetDisableReason returns why a feature is disabled or the licensing requirement
func (ff *FeatureFlags) GetDisableReason(featureID FeatureID) string {
	ff.mutex.RLock()
	defer ff.mutex.RUnlock()

	// First check if explicitly disabled
	if reason, ok := ff.disabledReasons[string(featureID)]; ok {
		return reason
	}

	// Return license requirement as reason
	if info, ok := ff.featureRegistry[featureID]; ok {
		if info.LicenseRequired != license.Community {
			return fmt.Sprintf("Requires %s license to access", info.LicenseRequired)
		}
	}

	return ""
}

// CanAccessWithLicense checks if specific license grants access
func (ff *FeatureFlags) CanAccessWithLicense(featureID FeatureID, licenseInfo *license.LicenseInfo) bool {
	info, ok := ff.featureRegistry[featureID]
	if !ok {
		return false
	}

	return ff.checkFeatureAccess(featureID, licenseInfo, info)
}

// GetAvailableFeatures returns all features accessible with given license
func (ff *FeatureFlags) GetAvailableFeatures(licenseInfo *license.LicenseInfo) []FeatureID {
	ff.mutex.RLock()
	defer ff.mutex.RUnlock()

	available := make([]FeatureID, 0)

	for id, info := range ff.featureRegistry {
		if ff.checkFeatureAccess(id, licenseInfo, info) {
			available = append(available, id)
		}
	}

	return available
}

// RegisterHook allows registration of custom logic when feature is accessed
func (ff *FeatureFlags) RegisterHook(featureID FeatureID, hookFunc func()) {
	ff.mutex.Lock()
	defer ff.mutex.Unlock()

	ff.hooks[featureID] = append(ff.hooks[featureID], hookFunc)
}

// SetConfigSource updates the configuration source
func (ff *FeatureFlags) SetConfigSource(source string) {
	ff.mutex.Lock()
	defer ff.mutex.Unlock()

	ff.configSource = source
}

// ExportConfiguration exports current feature state as JSON
func (ff *FeatureFlags) ExportConfiguration() ([]byte, error) {
	ff.mutex.RLock()
	defer ff.mutex.RUnlock()

	config := map[string]interface{}{
		"config_source":  ff.configSource,
		"enabled":        ff.enabledFeatures,
		"disabled_reasons": ff.disabledReasons,
		"features":       make(map[string]map[string]interface{}),
	}

	for id, info := range ff.featureRegistry {
		config["features"].(map[string]map[string]interface{})[string(id)] = map[string]interface{}{
			"name":              info.Name,
			"description":       info.Description,
			"default_enabled":   info.DefaultEnabled,
			"license_required":  info.LicenseRequired,
			"hidden":            info.Hidden,
		}
	}

	return json.MarshalIndent(config, "", "  ")
}

// ImportConfiguration imports feature configuration from JSON
func (ff *FeatureFlags) ImportConfiguration(configJSON []byte) error {
	var config map[string]interface{}
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	ff.mutex.Lock()
	defer ff.mutex.Unlock()

	// Update enabled features
	if enabled, ok := config["enabled"].(map[string]interface{}); ok {
		ff.enabledFeatures = make(map[string]bool)
		for key, val := range enabled {
			if b, ok := val.(bool); ok {
				ff.enabledFeatures[key] = b
			}
		}
	}

	// Update disabled reasons
	if reasons, ok := config["disabled_reasons"].(map[string]interface{}); ok {
		ff.disabledReasons = make(map[string]string)
		for key, val := range reasons {
			if reason, ok := val.(string); ok {
				ff.disabledReasons[key] = reason
			}
		}
	}

	// Update config source
	if source, ok := config["config_source"].(string); ok {
		ff.configSource = source
	}

	return nil
}

// UpgradeFeatures upgrades feature availability based on license upgrade
func (ff *FeatureFlags) UpgradeFeatures(oldLicense, newLicense *license.LicenseInfo) {
	oldTier := license.GetLicenseTier(oldLicense.LicenseType)
	newTier := license.GetLicenseTier(newLicense.LicenseType)

	if newTier <= oldTier {
		return
	}

	// Auto-enable features that are now available
	for id, info := range ff.featureRegistry {
		requiredTier := license.GetLicenseTier(info.LicenseRequired)
		
		if newTier >= requiredTier && !ff.enabledFeatures[string(id)] {
			ff.enabledFeatures[string(id)] = true
			delete(ff.disabledReasons, string(id))
		}
	}
}

// GetUsageStatistics calculates feature usage statistics
func (ff *FeatureFlags) GetUsageStatistics() map[string]int {
	ff.mutex.RLock()
	defer ff.mutex.RUnlock()

	stats := make(map[string]int)
	
	for id := range ff.featureRegistry {
		count := 0
		if ff.enabledFeatures[string(id)] {
			count = 1
		}
		stats[string(id)] = count
	}

	return stats
}

// SyncWithDatabase syncs feature flags from database (for multi-instance deployments)
func (ff *FeatureFlags) SyncWithDatabase(db interface{}) error {
	// In production: query DB for feature configuration
	// This is a placeholder for distributed synchronization
	_ = db
	return nil
}
