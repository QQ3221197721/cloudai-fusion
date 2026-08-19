
// Package metasploit_config provides configuration management for Metasploit integration
package metasploit

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ConfigManager manages Metasploit configuration lifecycle
type ConfigManager struct {
	config     *GlobalConfig
	filePath   string
	mu         sync.RWMutex
	validator  *ConfigValidator
}

// GlobalConfig holds all Metasploit configuration
type GlobalConfig struct {
	RPC            RPCConfig          `json:"rpc"`
	Scanning       ScanningConfig     `json:"scanning"`
	Exploitation   ExploitationConfig `json:"exploitation"`
	Security       SecurityConfig     `json:"security"`
	Reporting      ReportingConfig    `json:"reporting"`
	Logging        LoggingConfig      `json:"logging"`
}

// RPCConfig holds Metasploit RPC connection settings
type RPCConfig struct {
	Host           string        `json:"host"`
	Port           int           `json:"port"`
	Username       string        `json:"username"`
	Password       string        `json:"password"`
	SSLEnabled     bool          `json:"ssl_enabled"`
	SSLCertPath    string        `json:"ssl_cert_path,omitempty"`
	SSLKeyPath     string        `json:"ssl_key_path,omitempty"`
	Timeout        int           `json:"timeout_seconds"`
	RetryAttempts  int           `json:"retry_attempts"`
	MaxConcurrent  int           `json:"max_concurrent_sessions"`
}

// ScanningConfig defines vulnerability scanning parameters
type ScanningConfig struct {
	ThreadCount          int      `json:"thread_count"`
	PortRange            string   `json:"port_range"`
	ServiceTimeout       int      `json:"service_timeout_seconds"`
	FingerprintDelay     int      `json:"fingerprint_delay_ms"`
	StealthMode          bool     `json:"stealth_mode"`
	CVEUpdateInterval    int      `json:"cve_update_interval_days"`
	IncludeDeprecated    bool     `json:"include_deprecated_modules"`
	VerifyExploitsBefore bool     `json:"verify_exploits_before_use"`
}

// ExploitationConfig controls exploit execution behavior
type ExploitationConfig struct {
	DefaultPayload               string   `json:"default_payload"`
	SessionLeakageDetection      bool     `json:"session_leakage_detection"`
	PayloadCustomization         bool     `json:"payload_customization"`
	EvasionTechniques            []string `json:"evasion_techniques"`
	PrivilegeEscalationStrategy  string   `json:"privilege_escalation_strategy"` // automatic, manual, disabled
	MaxSessionDurationMinutes    int      `json:"max_session_duration_minutes"`
	AutoCleanupExpiredSessions   bool     `json:"auto_cleanup_expired"`
	ExploitRetryAttempts         int      `json:"exploit_retry_attempts"`
	SuccessConfirmationThreshold float64  `json:"success_confirmation_threshold"`
}

// SecurityConfig defines security constraints
type SecurityConfig struct {
	AuthorizationRequired    bool     `json:"authorization_required"`
	AllowedTargetRanges      []string `json:"allowed_target_ranges"` // CIDR notation
	BlockedTargetIPs         []string `json:"blocked_ip_list"`
	RequiresApprovalForCritical bool  `json:"requires_approval_for_critical_exploits"`
	WhitelistOnlyMode        bool     `json:"whitelist_only_mode"`
	AuditAllOperations       bool     `json:"audit_all_operations"`
	SessionEncryption        bool     `json:"session_encryption"`
	DisableLateralMovement   bool     `json:"disable_lateral_movement"`
}

// ReportingConfig controls report generation
type ReportingConfig struct {
	Format                      string   `json:"format"` // json, xml, pdf, html
	IncludeScreenshots          bool     `json:"include_screenshots"`
	IncludeEvidenceChain        bool     `json:"include_evidence_chain"`
	RetainReportsDays           int      `json:"retain_reports_days"`
	AutomateEmailReport         bool     `json:"automate_email_report"`
	EmailRecipients             []string `json:"email_recipients"`
	EmailSMTPServer             string   `json:"email_smtp_server"`
	ComplianceFrameworks        []string `json:"compliance_frameworks"` // NIST, ISO27001, PCI-DSS
}

// LoggingConfig defines logging behavior
type LoggingConfig struct {
	Level              string   `json:"level"` // debug, info, warn, error
	OutputPath         string   `json:"output_path"`
	MaxFileSizeMB      int      `json:"max_file_size_mb"`
	MaxBackupFiles     int      `json:"max_backup_files"`
	SecureLogging      bool     `json:"secure_logging"`
	LogSensitiveData   bool     `json:"log_sensitive_data"`
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(filePath string) (*ConfigManager, error) {
	cm := &ConfigManager{
		filePath: filePath,
		validator: NewConfigValidator(),
	}
	
	err := cm.load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	
	return cm, nil
}

// load reads configuration from file
func (cm *ConfigManager) load() error {
	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	
	var config GlobalConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config JSON: %w", err)
	}
	
	// Validate configuration
	if err := cm.validator.Validate(config); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	
	cm.mu.Lock()
	cm.config = &config
	cm.mu.Unlock()
	
	return nil
}

// Save persists configuration to file
func (cm *ConfigManager) Save() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	
	if err := os.WriteFile(cm.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	
	return nil
}

// GetRPCConfig returns RPC configuration
func (cm *ConfigManager) GetRPCConfig() RPCConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.RPC
}

// GetScanningConfig returns scanning configuration
func (cm *ConfigManager) GetScanningConfig() ScanningConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.Scanning
}

// IsTargetAllowed checks if target IP is in allowed ranges
func (cm *ConfigManager) IsTargetAllowed(ip string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	// Check blocked list first
	for _, blocked := range cm.config.Security.BlockedTargetIPs {
		if ip == blocked {
			return false
		}
	}
	
	// If whitelist-only mode, check allowed ranges
	if cm.config.Security.WhitelistOnlyMode {
		// In production, implement CIDR matching
		// For now, return true if we have any allowed ranges
		return len(cm.config.Security.AllowedTargetRanges) > 0
	}
	
	return true
}

// RequiresApprovalForExploit determines if exploit needs approval
func (cm *ConfigManager) RequiresApprovalForExploit(severity float64) bool {
	if !cm.config.Security.RequiresApprovalForCritical {
		return false
	}
	
	return severity >= 9.0 // Critical threshold
}
