package security

import (
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/features"
)

// SeverityLevel represents the severity of an anomaly
type SeverityLevel string

const (
	Low       SeverityLevel = "low"
	Medium    SeverityLevel = "medium"
	High      SeverityLevel = "high"
	Critical  SeverityLevel = "critical"
)

// RecommendedAction represents the suggested response to an abuse pattern
type RecommendedAction string

const (
	BlockFeature           RecommendedAction = "block_feature"
	SuspendAccount         RecommendedAction = "suspend_account"
	Investigate            RecommendedAction = "investigate"
	EmergencyNotify        RecommendedAction = "emergency_notify"
	CustomResponse         RecommendedAction = "custom_response"
)

// PatternType identifies different types of abuse patterns
type PatternType string

const (
	RapidExploitAttempts        PatternType = "rapid_exploit_attempts"
	UnauthorizedTargetScanning  PatternType = "unauthorized_target_scanning"
	CredentialDumpingAtScale    PatternType = "credential_dumping_at_scale"
	DataExfiltrationAttempt     PatternType = "data_exfiltration_attempt"
	CrossTenantAccessAttempt    PatternType = "cross_tenant_access_attempt"
	ResourceAbuse               PatternType = "resource_abuse"
	BruteForceAttack            PatternType = "brute_force_attack"
	NetworkScanning             PatternType = "network_scanning"
	PatternMatchingPatternMatch PatternType = "pattern_matching_pattern_match"
)

// AbusePattern represents a detected abusive behavior
type AbusePattern struct {
	PatternType         PatternType
	Severity            SeverityLevel
	Description         string
	DetectedAt          time.Time
	TenantID            string
	UserID              string // Optional user identifier
	SourceIP            string // Source IP of the request
	Evidence            []string
	RecommendedAction   RecommendedAction
	ConfidenceScore     float64 // 0.0 to 1.0
	RelatedOperations   []string
	CorrelationID       string // Link related patterns
}

// UserAction represents a single user action for analysis
type UserAction struct {
	Timestamp   time.Time
	Operation   string
	TargetIP    string
	TargetHost  string
	TargetPort  int
	Success     bool
	FailureCode string
	Duration    time.Duration
	BytesSent   int
	BytesRecv   int
	UserAgent   string
	Details     map[string]interface{}
}

// ActionWindow represents a time window of actions to analyze
type ActionWindow struct {
	Start     time.Time
	End       time.Time
	Actions   []UserAction
	TenantID  string
}

// AlertNotification represents a security alert notification
type AlertNotification struct {
	Pattern      *AbusePattern
	TimeSent     time.Time
	SentTo       []string // Recipient list
	Channel      string   // email, slack, pagerduty, etc.
	Resolved     bool
	ResolutionNote string
}

// BehaviorDatabase stores historical behavioral data
type BehaviorDatabase struct {
	mutex sync.RWMutex
	actionHistory map[string][]UserAction // tenant_id -> actions
	baselineData  map[string]*BehaviorBaseline // tenant_id -> baseline
	lastSyncTime  time.Time
}

// BehaviorBaseline contains statistical baseline for normal behavior
type BehaviorBaseline struct {
	AvgActionsPerHour    int
	AvgDurationPerAction time.Duration
	CommonTargets        []string
	AllowedTimeWindows   map[string]bool // "hour": true
	CreatedAt            time.Time
	LastUpdated          time.Time
}

// AnomalyDetector monitors user behavior for potential abuse patterns
type AnomalyDetector struct {
	db                 *BehaviorDatabase
	alertSystem        interface{} // Can be Slack, Email, PagerDuty integration
	auditLogger        *AuditLogger
	featureFlags       *features.FeatureFlags
	config             *DetectorConfig
}

// DetectorConfig holds configuration for the anomaly detector
type DetectorConfig struct {
	Enabled                    bool
	CheckInterval              time.Duration
	AutoBlockThreshold         float64 // Confidence score threshold for auto-blocking
	MaxActionsBuffered         int     // Max actions to buffer before processing
	EnableRealTimeAnalysis     bool
	EnableBatchAnalysis        bool
	Allowlist                  map[string]bool // IPs/users to always allow
	Blocklist                  map[string]bool // IPs/users to always block
}

// NewAnomalyDetector creates a new AnomalyDetector instance
func NewAnomalyDetector(config *DetectorConfig, featureFlags *features.FeatureFlags) *AnomalyDetector {
	if config == nil {
		config = getDefaultConfig()
	}

	return &AnomalyDetector{
		db: &BehaviorDatabase{
			actionHistory: make(map[string][]UserAction),
			baselineData:  make(map[string]*BehaviorBaseline),
		},
		auditLogger: &AuditLogger{},
		featureFlags: featureFlags,
		config:       config,
	}
}

// SetAlertSystem sets up the alert notification system
func (ad *AnomalyDetector) SetAlertSystem(system interface{}) {
	ad.alertSystem = system
}

// MonitorBehavior analyzes user actions for potential abuse patterns in real-time
func (ad *AnomalyDetector) MonitorBehavior(tenantID string, action UserAction) []AbusePattern {
	if !ad.config.Enabled {
		return nil
	}

	// Check allowlist/blocklist first
	if ad.isAllowed(action.TargetIP, tenantID) {
		return nil
	}

	if ad.isBlocked(action.TargetIP) {
		return nil // Skip blocked IPs silently
	}

	// Store action
	ad.storeAction(tenantID, action)

	// Get recent actions window
	recentActions := ad.getRecentActions(tenantID, 5*time.Minute)

	// Analyze patterns
	patterns := ad.detectAbusePatterns(tenantID, recentActions)

	for _, pattern := range patterns {
		ad.handleDetectedPattern(pattern)
	}

	return patterns
}

// AnalyzeBulkActions analyzes a batch of actions at once
func (ad *AnomalyDetector) AnalyzeBulkActions(tenantID string, actions []UserAction) []AbusePattern {
	if len(actions) == 0 {
		return nil
	}

	// Store all actions
	for _, action := range actions {
		ad.storeAction(tenantID, action)
	}

	// Get comprehensive action history
	window := ad.getActionWindow(tenantID, 24*time.Hour)

	return ad.detectAbusePatterns(tenantID, window.Actions)
}

// detectAbusePatterns identifies potential abuse scenarios
func (ad *AnomalyDetector) detectAbusePatterns(tenantID string, actions []UserAction) []AbusePattern {
	patterns := []AbusePattern{}

	if len(actions) == 0 {
		return patterns
	}

	// Pattern 1: Rapid-fire exploit attempts
	if pattern := ad.detectRapidExploits(tenantID, actions); pattern != nil {
		patterns = append(patterns, *pattern)
	}

	// Pattern 2: Unauthorized target scanning
	if pattern := ad.detectUnauthorizedScanning(tenantID, actions); pattern != nil {
		patterns = append(patterns, *pattern)
	}

	// Pattern 3: Credential dumping at scale
	if pattern := ad.detectCredentialDumpingAtScale(tenantID, actions); pattern != nil {
		patterns = append(patterns, *pattern)
	}

	// Pattern 4: Data exfiltration attempt
	if pattern := ad.detectDataExfiltration(tenantID, actions); pattern != nil {
		patterns = append(patterns, *pattern)
	}

	// Pattern 5: Resource abuse (excessive quota consumption)
	if pattern := ad.detectResourceAbuse(tenantID, actions); pattern != nil {
		patterns = append(patterns, *pattern)
	}

	// Pattern 6: Brute force attack detection
	if pattern := ad.detectBruteForceAttack(tenantID, actions); pattern != nil {
		patterns = append(patterns, *pattern)
	}

	// Pattern 7: Network scanning patterns
	if pattern := ad.detectNetworkScanning(tenantID, actions); pattern != nil {
		patterns = append(patterns, *pattern)
	}

	return patterns
}

// detectRapidExploits detects rapid exploitation attempts
func (ad *AnomalyDetector) detectRapidExploits(tenantID string, actions []UserAction) *AbusePattern {
	exploitActions := 0
	startTime := time.Time{}

	for i, action := range actions {
		if isExploitOperation(action.Operation) && action.Success {
			exploitActions++

			if i == 0 || startTime.IsZero() {
				startTime = action.Timestamp
			}
		}
	}

	// More than 10 exploits in 1 minute = suspicious
	if exploitActions > 10 && !startTime.IsZero() {
		duration := time.Since(startTime)
		if duration < time.Minute {
			evidence := extractActionEvidence(actions, 10)
			return &AbusePattern{
				PatternType:         RapidExploitAttempts,
				Severity:            High,
				Description:         fmt.Sprintf("Multiple exploitation attempts detected (%d exploits in %.0f seconds)", 
					exploitActions, duration.Seconds()),
				DetectedAt:          time.Now(),
				TenantID:            tenantID,
				Evidence:            evidence,
				RecommendedAction:   BlockFeature,
				ConfidenceScore:     0.85,
				RelatedOperations:   []string{"exploit_execution", "payload_delivery"},
			}
		}
	}

	return nil
}

// detectUnauthorizedScanning detects scanning outside authorized scope
func (ad *AnomalyDetector) detectUnauthorizedScanning(tenantID string, actions []UserAction) *AbusePattern {
	privateRanges := []string{"10.", "172.16.", "192.168."}
	scannedPrivate := 0

	for _, action := range actions {
		if isScanOperation(action.Operation) {
			for _, prefix := range privateRanges {
				if len(action.TargetIP) > len(prefix) && action.TargetIP[:len(prefix)] == prefix {
					scannedPrivate++
					break
				}
			}
		}
	}

	// Scanning more than 5 private addresses without permission
	if scannedPrivate > 5 {
		return &AbusePattern{
			PatternType:         UnauthorizedTargetScanning,
			Severity:            Critical,
			Description:         fmt.Sprintf("Scanning of %d private IP ranges detected - potential unauthorized access", scannedPrivate),
			DetectedAt:          time.Now(),
			TenantID:            tenantID,
			Evidence:            []string{"scanned_private_ranges: " + fmt.Sprintf("%d", scannedPrivate)},
			RecommendedAction:   SuspendAccount,
			ConfidenceScore:     0.9,
			RelatedOperations:   []string{"target_discovery", "vulnerability_scan"},
		}
	}

	return nil
}

// detectCredentialDumpingAtScale detects mass credential extraction
func (ad *AnomalyDetector) detectCredentialDumpingAtScale(tenantID string, actions []UserAction) *AbusePattern {
	dumpAttempts := 0

	for _, action := range actions {
		if operationContains(action.Operation, "credential") || operationContains(action.Operation, "password") {
			dumpAttempts++
		}
	}

	// More than 5 credential extraction attempts in 30 minutes
	if dumpAttempts > 5 {
		evidence := make([]string, 0)
		for _, action := range actions {
			if operationContains(action.Operation, "credential") {
				evidence = append(evidence, fmt.Sprintf("%s on %s", action.Operation, action.TargetHost))
			}
		}

		return &AbusePattern{
			PatternType:         CredentialDumpingAtScale,
			Severity:            Critical,
			Description:         fmt.Sprintf("Mass credential extraction attempted (%d operations in monitoring window)", dumpAttempts),
			DetectedAt:          time.Now(),
			TenantID:            tenantID,
			Evidence:            evidence,
			RecommendedAction:   SuspendAccount,
			ConfidenceScore:     0.95,
			RelatedOperations:   []string{"credential_dumping", "lateral_movement"},
		}
	}

	return nil
}

// detectDataExfiltration detects potential data exfiltration
func (ad *AnomalyDetector) detectDataExfiltration(tenantID string, actions []UserAction) *AbusePattern {
	totalBytesOut := 0

	for _, action := range actions {
		totalBytesOut += action.BytesSent
	}

	// Exfiltrating more than 1GB in short period
	const largeTransferThreshold = 1024 * 1024 * 1024 // 1GB

	if totalBytesOut > largeTransferThreshold {
		return &AbusePattern{
			PatternType:         DataExfiltrationAttempt,
			Severity:            Critical,
			Description:         fmt.Sprintf("Large data transfer detected (%.2f GB potentially exfiltrated)", float64(totalBytesOut)/(1024*1024*1024)),
			DetectedAt:          time.Now(),
			TenantID:            tenantID,
			Evidence:            []string{fmt.Sprintf("bytes_sent: %d", totalBytesOut)},
			RecommendedAction:   SuspendAccount,
			ConfidenceScore:     0.75,
			RelatedOperations:   []string{"data_exfiltration", "file_transfer"},
		}
	}

	return nil
}

// detectResourceAbuse detects excessive resource consumption
func (ad *AnomalyDetector) detectResourceAbuse(tenantID string, actions []UserAction) *AbusePattern {
	actionCount := len(actions)
	shortDuration := time.Duration(0)

	for _, action := range actions {
		shortDuration += action.Duration
	}

	avgDuration := time.Duration(0)
	if actionCount > 0 {
		avgDuration = shortDuration / time.Duration(actionCount)
	}

	// More than 100 actions in 10 minutes with very fast execution = automated abuse
	if actionCount > 100 && shortDuration < 10*time.Minute {
		return &AbusePattern{
			PatternType:         ResourceAbuse,
			Severity:            High,
			Description:         fmt.Sprintf("Excessive API usage detected (%d requests in 10 minutes)", actionCount),
			DetectedAt:          time.Now(),
			TenantID:            tenantID,
			Evidence:            []string{fmt.Sprintf("total_requests: %d, avg_duration: %v", actionCount, avgDuration)},
			RecommendedAction:   BlockFeature,
			ConfidenceScore:     0.8,
			RelatedOperations:   []string{"all"},
		}
	}

	return nil
}

// detectBruteForceAttack detects brute force authentication attempts
func (ad *AnomalyDetector) detectBruteForceAttack(tenantID string, actions []UserAction) *AbusePattern {
	authFailures := 0
	targetUsers := make(map[string]int)

	for _, action := range actions {
		if authFailureOperation(action.Operation) {
			authFailures++
			
			// Extract target user from details if available
			if user, ok := action.Details["user"]; ok {
				if userStr, ok := user.(string); ok {
					targetUsers[userStr]++
				}
			}
		}
	}

	// More than 20 failures or targeted attacks against specific accounts
	if authFailures > 20 {
		highestTarget := ""
		maxAttempts := 0
		
		for user, count := range targetUsers {
			if count > maxAttempts {
				maxAttempts = count
				highestTarget = user
			}
		}

		return &AbusePattern{
			PatternType:         BruteForceAttack,
			Severity:            Critical,
			Description:         fmt.Sprintf("Brute force attack detected (%d auth failures, highest target: %s with %d attempts",
				authFailures, highestTarget, maxAttempts),
			DetectedAt:          time.Now(),
			TenantID:            tenantID,
			Evidence:            []string{fmt.Sprintf("auth_failures: %d", authFailures)},
			RecommendedAction:   EmergencyNotify,
			ConfidenceScore:     0.95,
			RelatedOperations:   []string{"authentication", "login"},
		}
	}

	return nil
}

// detectNetworkScanning detects aggressive network scanning
func (ad *AnomalyDetector) detectNetworkScanning(tenantID string, actions []UserAction) *AbusePattern {
	scanOperations := 0
	uniqueTargets := make(map[string]bool)

	for _, action := range actions {
		if isScanOperation(action.Operation) {
			scanOperations++
			uniqueTargets[action.TargetIP] = true
		}
	}

	// More than 50 scan operations across many unique targets
	if scanOperations > 50 && len(uniqueTargets) > 20 {
		return &AbusePattern{
			PatternType:         NetworkScanning,
			Severity:            High,
			Description:         fmt.Sprintf("Aggressive network scanning detected (%d scans across %d unique targets)", 
				scanOperations, len(uniqueTargets)),
			DetectedAt:          time.Now(),
			TenantID:            tenantID,
			Evidence:            []string{fmt.Sprintf("scan_operations: %d", scanOperations), fmt.Sprintf("unique_targets: %d", len(uniqueTargets))},
			RecommendedAction:   BlockFeature,
			ConfidenceScore:     0.85,
			RelatedOperations:   []string{"system_discovery", "port_scanning"},
		}
	}

	return nil
}

// handleDetectedPattern takes action when abuse is detected
func (ad *AnomalyDetector) handleDetectedPattern(pattern AbusePattern) {
	ad.auditLogger.Log("abuse_detected", fmt.Sprintf("Tenant=%s Pattern=%s Severity=%s",
		pattern.TenantID, pattern.PatternType, pattern.Severity))

	switch pattern.RecommendedAction {
	case BlockFeature:
		ad.blockFeatureAccess(pattern.TenantID)
		
	case SuspendAccount:
		ad.suspendTenantAccess(pattern.TenantID)
		
	case EmergencyNotify:
		ad.emergencyNotifySecurityTeam(pattern)
		
	case Investigate:
		ad.queueForInvestigation(pattern)
	}

	// Send alerts
	ad.sendAlert(pattern)
}

// blockFeatureAccess temporarily blocks a feature for tenant
func (ad *AnomalyDetector) blockFeatureAccess(tenantID string) {
	// Implement feature blocking logic
	ad.auditLogger.Log("feature_blocked", fmt.Sprintf("Tenant=%s Action=automated_block", tenantID))
	
	// Could integrate with feature flags to disable certain features
	if ad.featureFlags != nil {
		// Disable advanced offensive features
		ad.featureFlags.Disable(features.ExploitExecution, "Automated suspension due to suspicious activity")
		ad.featureFlags.Disable(features.LateralMovement, "Automated suspension due to suspicious activity")
	}
}

// suspendTenantAccess suspends entire account access
func (ad *AnomalyDetector) suspendTenantAccess(tenantID string) {
	ad.auditLogger.Log("account_suspended", fmt.Sprintf("Tenant=%s Reason=automated_security", tenantID))
	
	// In production: Set tenant suspended flag in database
	// This would prevent all future API calls
}

// emergencyNotifySecurityTeam sends urgent notifications
func (ad *AnomalyDetector) emergencyNotifySecurityTeam(pattern AbusePattern) {
	notification := AlertNotification{
		Pattern:   &pattern,
		TimeSent:  time.Now(),
		Channel:   "pagerduty",
		Resolved:  false,
	}

	_ = notification
	
	// In production: send to PagerDuty, SMS, phone call
	// ad.alertSystem.NotifyCritical(&notification)
}

// queueForInvestigation adds pattern to investigation queue
func (ad *AnomalyDetector) queueForInvestigation(pattern AbusePattern) {
	ad.auditLogger.Log("investigation_queued", fmt.Sprintf("Tenant=%s Pattern=%s", pattern.TenantID, pattern.PatternType))
	
	// Queue for manual review by security team
}

// sendAlert sends notification about detected pattern
func (ad *AnomalyDetector) sendAlert(pattern AbusePattern) {
	ad.auditLogger.Log("alert_sent", fmt.Sprintf("Pattern=%s Severity=%s", pattern.PatternType, pattern.Severity))
	
	// Send email/slack notification
	// ad.alertSystem.Notify(&AlertNotification{Pattern: &pattern})
}

// storeAction saves action to history
func (ad *AnomalyDetector) storeAction(tenantID string, action UserAction) {
	ad.db.mutex.Lock()
	defer ad.db.mutex.Unlock()

	if _, exists := ad.db.actionHistory[tenantID]; !exists {
		ad.db.actionHistory[tenantID] = make([]UserAction, 0)
	}

	actions := ad.db.actionHistory[tenantID]
	actions = append(actions, action)

	// Limit stored actions to avoid memory issues
	if len(actions) > ad.config.MaxActionsBuffered {
		actions = actions[len(actions)-ad.config.MaxActionsBuffered:]
	}

	ad.db.actionHistory[tenantID] = actions
	ad.db.lastSyncTime = time.Now()
}

// getRecentActions retrieves actions within time window
func (ad *AnomalyDetector) getRecentActions(tenantID string, window time.Duration) []UserAction {
	ad.db.mutex.RLock()
	defer ad.db.mutex.RUnlock()

	cutoff := time.Now().Add(-window)
	recent := make([]UserAction, 0)

	for _, action := range ad.db.actionHistory[tenantID] {
		if action.Timestamp.After(cutoff) {
			recent = append(recent, action)
		}
	}

	return recent
}

// getActionWindow returns comprehensive action history
func (ad *AnomalyDetector) getActionWindow(tenantID string, window time.Duration) ActionWindow {
	ad.db.mutex.RLock()
	defer ad.db.mutex.RUnlock()

	start := time.Now().Add(-window)
	end := time.Now()

	actions := make([]UserAction, 0)
	for _, action := range ad.db.actionHistory[tenantID] {
		if action.Timestamp.After(start) && action.Timestamp.Before(end) {
			actions = append(actions, action)
		}
	}

	return ActionWindow{
		Start:   start,
		End:     end,
		Actions: actions,
		TenantID: tenantID,
	}
}

// isAllowed checks if IP/user is in allowlist
func (ad *AnomalyDetector) isAllowed(ip string, tenantID string) bool {
	if ip != "" && ad.config.Allowlist[ip] {
		return true
	}
	return ad.config.Allowlist[tenantID]
}

// isBlocked checks if IP/user is in blocklist
func (ad *AnomalyDetector) isBlocked(ip string) bool {
	return ad.config.Blocklist[ip]
}

// AuditLogger logs security events
type AuditLogger struct {
	logFile string
}

// Log writes a security event log
func (al *AuditLogger) Log(eventType string, message string) {
	timestamp := time.Now().Format(time.RFC3339)
	logLine := fmt.Sprintf("[%s] %s: %s\n", timestamp, eventType, message)

	// Write to log file
	if al.logFile != "" {
		_ = al.logFile
		// os.AppendFile(al.logFile, []byte(logLine))
	}

	// Also print to stdout for debugging
	fmt.Print(logLine)
}

// extractActionEvidence extracts meaningful evidence from actions
func extractActionEvidence(actions []UserAction, limit int) []string {
	evidence := make([]string, 0, limit)

	for i := 0; i < len(actions) && i < limit; i++ {
		action := actions[i]
		evidence = append(evidence, fmt.Sprintf("%s:%s:%s", 
			action.Operation, 
			action.TargetIP, 
			action.Timestamp.Format("15:04:05")))
	}

	return evidence
}

// Helper functions
func isExploitOperation(operation string) bool {
	exploitOps := []string{"exploit", "attack", "payload"}
	return operationContains(operation, exploitOps...)
}

func isScanOperation(operation string) bool {
	scanOps := []string{"scan", "discovery", "probe"}
	return operationContains(operation, scanOps...)
}

func authFailureOperation(operation string) bool {
	authOps := []string{"login", "auth", "authenticate", "password"}
	return operationContains(operation, authOps...)
}

func operationContains(operation string, substrings ...string) bool {
	opLower := operation
	for _, sub := range substrings {
		if len(opLower) >= len(sub) {
			for i := 0; i <= len(opLower)-len(sub); i++ {
				if opLower[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// getDefaultConfig returns default detector configuration
func getDefaultConfig() *DetectorConfig {
	return &DetectorConfig{
		Enabled:                    true,
		CheckInterval:            30 * time.Second,
		AutoBlockThreshold:       0.8,
		MaxActionsBuffered:       10000,
		EnableRealTimeAnalysis:   true,
		EnableBatchAnalysis:      true,
		Allowlist:                make(map[string]bool),
		Blocklist:                make(map[string]bool),
	}
}
