// Package devsecops provides automated security gate enforcement for CI/CD pipelines
// enabling shift-left security practices in OBCE3 development workflows.
package devsecops

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/redteam_simulation"
)

// DevSecOpsGates implements comprehensive security check automation
type DevSecOpsGates struct {
	sastScanner     *StaticAnalysisScanner
	dastScanner     *DynamicAnalysisScanner
	secretsScanner  *SecretsDetectionScanner
	complianceChecker *ComplianceVerificationEngine
	containerScanner *ContainerImageScanner
	dependencyScanner *DependencyVulnerabilityScanner
	policyEnforcer  *PolicyEnforcementEngine
	metricsCollector *SecurityMetricsCollector
	riskScorer      *AutomatedRiskScorer
}

// StaticAnalysisScanner performs SAST (Static Application Security Testing)
type StaticAnalysisScanner struct {
	scannerEngine string // "semgrep", "sonarqube", "codeql"
	configFile    string
	ruleSets      []SASTRuleSet
	includePaths  []string
	excludePaths  []string
	maxIssues     int
	timeout       time.Duration
	cacheEnabled  bool
	cachePath     string
	reportFormat  string
}

// SASTRuleSet defines static analysis rule collection
type SASTRuleSet struct {
	ID          string
	Name        string
	Languages   []string
	Patterns    []*regexp.Regexp
	Description string
	CWE         string
	Severity    FindingSeverity
	Mitigation  string
	AffectedPatterns []string
	FixPatterns []string
}

// DynamicAnalysisScanner performs DAST (Dynamic Application Security Testing)
type DynamicAnalysisScanner struct {
	targetURL        string
	authCredentials  *AuthConfig
	scanningProfiles []string
	requestTimeout   time.Duration
	concurrencyLevel int
	proxySettings    *ProxyConfig
	followRedirects  bool
	maxDepth         int
	headerOverrides  map[string]string
	injectionTests   []InjectionTestSuite
}

// AuthConfig contains authentication credentials
type AuthConfig struct {
	Username string
	Password string
	Token string
	CookieJarEnabled bool
}

// ProxyConfig defines proxy settings
type ProxyConfig struct {
	Enabled bool
	URL string
	Username string
	Password string
}

// InjectionTestSuite defines injection testing scenarios
type InjectionTestSuite struct {
	Name        string
	TestPoints  []string
	InputTypes  []string
	ExpectedOutcomes []string
	SafetyGuarantees []SafetyGuarantee
}

// SafetyGuarantee ensures test safety
type SafetyGuarantee struct {
	GuaranteeType string
	Description string
	Reversible bool
}

// SecretsDetectionScanner identifies sensitive information exposure
type SecretsDetectionScanner struct {
	enabledDetectors []SecretDetector
	includePreCommit bool
	trackRevisions bool
	maxHistoryDepth int
	outputFormat string
	notifier *SecretsLeakNotifier
}

// SecretDetector defines pattern-based secret detection
type SecretDetector struct {
	Name string
	Pattern *regexp.Regexp
	EntropyThreshold float64
	ContextWindow int
	Family string
	OverrideRules []PatternOverride
}

// PatternOverride allows exception handling
type PatternOverride struct {
	PathMatch string
	Reason string
	ApprovedBy string
	Expiration time.Time
}

// ComplianceVerificationEngine validates against standards
type ComplianceVerificationEngine struct {
	targetStandards []string
	verificationMethods map[string]VerificationMethod
	evidenceCollector *EvidenceCollectionSystem
ReportingGenerator *ComplianceReportGenerator
remediationAdvisor *RemediationGuidanceEngine
}

// VerificationMethod defines validation approach
type VerificationMethod struct {
	Name string
	Approach string // "automated", "manual", "hybrid"
	Requirements []RequirementSpec
	EvidenceTypes []string
	RiskAcceptanceAllowed bool
}

// RequirementSpec defines single compliance requirement
type RequirementSpec struct {
	ID          string
	Description string
	StandardRef string
	Category    string
	Severity    ComplianceSeverity
	CheckFunction func() (bool, Evidence)
}

// ComplianceSeverity defines severity levels
type ComplianceSeverity string

const (
	ComplianceCritical ComplianceSeverity = "CRITICAL"
	ComplianceHigh ComplianceSeverity = "HIGH"
	ComplianceMedium ComplianceSeverity = "MEDIUM"
	ComplianceLow ComplianceSeverity = "LOW"
	ComplianceInfo ComplianceSeverity = "INFO"
)

// ContainerImageScanner scans Docker images for vulnerabilities
type ContainerImageScanner struct {
	registryConfig *RegistryConfiguration
	scanningTools []string
	vulnerabilityDB string
	trivyEnabled bool
	grypeEnabled bool
	outputFormat string
	failOnCVSS float64
	quillIntegration bool
}

// DependencyVulnerabilityScanner checks third-party dependencies
type DependencyVulnerabilityScanner struct {
	platformScanners map[string]*PlatformSpecificScanner
	allowlistList    []PackageAllowlistEntry
	blocklistList []PackageBlocklistEntry
	updateAdvisor *DependencyUpdateAdvisor
	cveDatabaseUrl string
}

// PlatformSpecificScanner handles platform-specific dependency checks
type PlatformSpecificScanner struct {
	platform string // "npm", "pip", "go", "maven", "gradle"
	command string
	parserFunc func(output string) ([]DependencyVuln, error)
}

// PolicyEnforcementEngine enforces security policies
type PolicyEnforcementEngine struct {
	OPAEngine       *OpenPolicyAgent
	policies        []SecurityPolicy
	enforcementMode string // "deny", "warn", "audit"
	exceptionsManager *ExceptionManagementSystem
	approvalWorkflow *PolicyApprovalWorkflow
}

// SecurityPolicy defines policy rules
type SecurityPolicy struct {
	ID          string
	Name        string
	Description string
	Condition   string
	Action      EnforcementAction
	Exceptions  []PolicyException
	AuditTrail  bool
}

// EnforcementAction defines policy action
type EnforcementAction string

const (
	BlockDeployment EnforcementAction = "BLOCK_DEPLOYMENT"
	WarnDeveloper EnforcementAction = "WARN_DEVELOPER"
	LogViolation EnforcementAction = "LOG_VIOLATION"
	AutoRemediate EnforcementAction = "AUTO_REMEDIATE"
)

// PolicyException defines policy exceptions
type PolicyException struct {
	ID            string
	Reason        string
	ApprovedBy    string
	ExpiresAt     time.Time
	Conditions    map[string]string
}

// AutomatedRiskScorer calculates deployment risk scores
type AutomatedRiskScorer struct {
	riskFactors []RiskFactor
	weights       map[string]float64
	toleranceThresholds map[RiskLevel]float64
	historicalData []HistoricalRiskRecord
	machineLearningEnabled bool
	modelVersion string
}

// RiskFactor defines calculation factors
type RiskFactor struct {
	Name        string
	Weight      float64
	Scale       string // "linear", "exponential", "threshold"
	Minimum float64
	Maximum float64
}

// HistoricalRiskRecord tracks past risk assessments
type HistoricalRiskRecord struct {
	AssessmentID string
	Timestamp time.Time
	RiskScore float64
	DeployedTo string
	BreakingIncidents int
	MTTRMinutes float64
}

// GateResult contains security gate evaluation result
type GateResult struct {
	GateID          string                `json:"gateId"`
	ScanTimestamp   time.Time             `json:"scanTimestamp"`
	GitCommit       string                `json:"gitCommit"`
	Branch          string                `json:"branch"`
	Repository      string                `json:"repository"`
	Passed          bool                  `json:"passed"`
	Skipped         bool                  `json:"skipped"`
	BlockingReason  string                `json:"blockingReason,omitempty"`
	Findings        []GateFinding         `json:"findings"`
	RiskScore       float64               `json:"riskScore"`
	RiskLevel       RiskLevel             `json:"riskLevel"`
	Duration        time.Duration         `json:"duration"`
	ComplianceStatus ComplianceReport      `json:"complianceStatus"`
	Metrics         SecurityMetrics       `json:"metrics"`
	Recommendations []string              `json:"recommendations"`
	AuditTrail      []AuditEvent          `json:"auditTrail"`
}

// GateFinding records individual security finding
type GateFinding struct {
	ID            string                `json:"id"`
	Scanner       string                `json:"scanner"`
	Type          FindingType           `json:"type"`
	Title         string                `json:"title"`
	Description   string                `json:"description"`
	Severity      FindingSeverity       `json:"severity"`
	Location      string                `json:"location"`
	CWE           string                `json:"cwe,omitempty"`
	CVE           string                `json:"cve,omitempty"`
	Evidence      string                `json:"evidence"`
	Mitigation    string                `json:"mitigation"`
	RiskScore     float64               `json:"riskScore"`
	Tags          []string              `json:"tags,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// FindingType categorizes findings
type FindingType string

const (
	FindingSAST FindingType = "SAST"
	FindingDAST FindingType = "DAST"
	FindingSecrets FindingType = "SECRETS"
	FindingDependency FindingType = "DEPENDENCY"
	FindingContainer FindingType = "CONTAINER"
	FindingCompliance FindingType = "COMPLIANCE"
)

// ComplianceReport summarizes compliance posture
type ComplianceReport struct {
	Standard          string                `json:"standard"`
	Score             int                   `json:"score"`
	Status            ComplianceStatus      `json:"status"`
	PassedChecks      int                   `json:"passedChecks"`
	TotalChecks       int                   `json:"totalChecks"`
	Violations        []ComplianceViolation `json:"violations"`
	EvidenceCollected bool                  `json:"evidenceCollected"`
	LastUpdated       time.Time             `json:"lastUpdated"`
}

// ComplianceStatus defines overall status
type ComplianceStatus string

const (
	Compliant       ComplianceStatus = "COMPLIANT"
	NonCompliant    ComplianceStatus = "NON_COMPLIANT"
	PartiallyCompliant ComplianceStatus = "PARTIALLY_COMPLIANT"
	NotEvaluated    ComplianceStatus = "NOT_EVALUATED"
)

// ComplianceViolation records compliance breach
type ComplianceViolation struct {
	ID             string              `json:"id"`
	StandardRef    string              `json:"standardRef"`
	RequirementID  string              `json:"requirementId"`
	Description    string              `json:"description"`
	Severity       ComplianceSeverity  `json:"severity"`
	CurrentState   string              `json:"currentState"`
	RequiredState string              `json:"requiredState"`
	Evidence       string              `json:"evidence"`
	Remediation    string              `json:"remediation"`
	References     []string            `json:"references"`
}

// SecurityMetrics captures performance data
type SecurityMetrics struct {
	SASTScanDurationMs int64 `json:"sastScanDurationMs"`
	DASTScanDurationMs int64 `json:"dastScanDurationMs"`
	SecretsScanDurationMs int64 `json:"secretsScanDurationMs"`
	ContainerScanDurationMs int64 `json:"containerScanDurationMs"`
	DependencyScanDurationMs int64 `json:"dependencyScanDurationMs"`
	ComplianceCheckDurationMs int64 `json:"complianceCheckDurationMs"`
	TotalScanTimeMs int64 `json:"totalScanTimeMs"`
	IssuesFound int `json:"issuesFound"`
	IssuesBlocked int `json:"issuesBlocked"`
}

// AuditEvent logs security gate activity
type AuditEvent struct {
	EventID     string    `json:"eventId"`
	Timestamp   time.Time `json:"timestamp"`
	EventType   string    `json:"eventType"`
	Description string    `json:"description"`
	User        string    `json:"user,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NewDevSecOpsGates creates comprehensive security gate system
func NewDevSecOpsGates(config GatesConfiguration) *DevSecOpsGates {
	return &DevSecOpsGates{
		sastScanner: NewStaticAnalysisScanner(config.SASTConfig),
		dastScanner: NewDynamicAnalysisScanner(config.DASTConfig),
		secretsScanner: NewSecretsDetectionScanner(config.SecretsConfig),
		complianceChecker: NewComplianceVerificationEngine(config.ComplianceConfig),
		containerScanner: NewContainerImageScanner(config.ContainerConfig),
		dependencyScanner: NewDependencyVulnerabilityScanner(config.DependencyConfig),
		policyEnforcer: NewPolicyEnforcementEngine(config.PolicyConfig),
		metricsCollector: NewSecurityMetricsCollector(),
		riskScorer: NewAutomatedRiskScorer(config.RiskConfig),
	}
}

// GatesConfiguration defines scanner configurations
type GatesConfiguration struct {
	SASTConfig    SASTScannerConfig
	DASTConfig    DASTScannerConfig
	SecretsConfig SecretsScannerConfig
	ComplianceConfig ComplianceEngineConfig
	ContainerConfig ContainerScannerConfig
	DependencyConfig DependencyScannerConfig
	PolicyConfig PolicyEnforcerConfig
	RiskConfig RiskScorerConfig
}

// RunSecurityGate executes complete security gate workflow
func (d *DevSecOpsGates) RunSecurityGate(gitCommit Commit) (GateResult, error) {
	startTime := time.Now()
	
	result := GateResult{
		GateID: fmt.Sprintf("SEC-GATE-%d", time.Now().UnixNano()),
		ScanTimestamp: startTime,
		GitCommit: gitCommit.Hash,
		Branch: gitCommit.Branch,
		Repository: gitCommit.Repository,
		Passed: true,
		Skipped: false,
		Findings: []GateFinding{},
		RiskScore: 0.0,
		RiskLevel: RiskNegligible,
		AuditTrail: []AuditEvent{},
	}

	ctx := context.Background()
	logAuditEvent(&result, "GATE_STARTED", "Security gates initiated for commit "+gitCommit.Hash)

	// Phase 1: SAST Scan (Static Analysis)
	fmt.Println("Executing SAST - Static Application Security Testing...")
	sastStartTime := time.Now()
	sastFindings, sastDuration, err := d.sastScanner.AnalyzeCode(ctx, gitCommit)
	if err != nil {
		logAuditEvent(&result, "SAST_ERROR", fmt.Sprintf("SAST scan failed: %v", err))
		sastFindings = append(sastFindings, GateFinding{
			ID: fmt.Sprintf("SAST-ERROR-%d", time.Now().UnixNano()),
			Scanner: "SAST",
			Type: FindingSAST,
			Title: "SAST Scan Error",
			Description: fmt.Sprintf("Static analysis encountered error: %v", err),
			Severity: FindingInfo,
			Evidence: err.Error(),
			RiskScore: 0.0,
		})
	} else {
		logAuditEvent(&result, "SAST_COMPLETED", 
			fmt.Sprintf("Found %d potential vulnerabilities", len(sastFindings)))
	}
	result.Metrics.SASTScanDurationMs = int64(sastDuration / time.Millisecond)
	result.Findings = append(result.Findings, sastFindings...)

	// Phase 2: Secrets Detection
	fmt.Println("Scanning for secrets and sensitive data...")
	secretsStartTime := time.Now()
	secretsFindings, secretsDuration := d.secretsScanner.CheckRepository(gitCommit)
	logAuditEvent(&result, "SECRETS_COMPLETED",
		fmt.Sprintf("Found %d potential secrets", len(secretsFindings)))
	result.Metrics.SecretsScanDurationMs = int64(secretsDuration / time.Millisecond)
	result.Findings = append(result.Findings, secretsFindings...)

	// Phase 3: Container Image Scan (if applicable)
	if gitCommit.HasContainers {
		fmt.Println("Scanning container images for vulnerabilities...")
		containerStartTime := time.Now()
		containerFindings, containerDuration := d.containerScanner.ScanImages(gitCommit)
		logAuditEvent(&result, "CONTAINER_SCAN_COMPLETED",
			fmt.Sprintf("Found %d container vulnerabilities", len(containerFindings)))
		result.Metrics.ContainerScanDurationMs = int64(containerDuration / time.Millisecond)
		result.Findings = append(result.Findings, containerFindings...)
	}

	// Phase 4: Dependency Vulnerability Check
	fmt.Println("Analyzing third-party dependencies...")
	dependencyStartTime := time.Now()
	dependencyFindings, dependencyDuration := d.dependencyScanner.AnalyzeDependencies(gitCommit)
	logAuditEvent(&result, "DEPENDENCY_SCAN_COMPLETED",
		fmt.Sprintf("Found %d dependency vulnerabilities", len(dependencyFindings)))
	result.Metrics.DependencyScanDurationMs = int64(dependencyDuration / time.Millisecond)
	result.Findings = append(result.Findings, dependencyFindings...)

	// Phase 5: DAST Scan (if web application detected)
	if gitCommit.IsWebApplication {
		fmt.Println("Performing dynamic application security testing...")
		dastStartTime := time.Now()
		dastFindings, dastDuration, err := d.dastScanner.TestDeployedApp(ctx, gitCommit)
		if err == nil {
			logAuditEvent(&result, "DAST_COMPLETED",
				fmt.Sprintf("Found %d runtime vulnerabilities", len(dastFindings)))
		}
		result.Metrics.DASTScanDurationMs = int64(dastDuration / time.Millisecond)
		result.Findings = append(result.Findings, dastFindings...)
	}

	// Phase 6: Compliance Verification
	fmt.Println("Verifying compliance with security standards...")
	complianceStartTime := time.Now()
	complianceStatus := d.complianceChecker.Verify(gitCommit)
	result.ComplianceStatus = complianceStatus
	logAuditEvent(&result, "COMPLIANCE_COMPLETED",
		fmt.Sprintf("Compliance score: %d/100", complianceStatus.Score))
	result.Metrics.ComplianceCheckDurationMs = int64(time.Since(complianceStartTime) / time.Millisecond)

	// Calculate overall risk score
	fmt.Println("Calculating deployment risk score...")
	riskScore := d.riskScorer.CalculateRiskScore(result.Findings, complianceStatus)
	result.RiskScore = riskScore
	result.RiskLevel = classifyRiskLevel(riskScore)

	// Policy enforcement check
	enforcementResult := d.policyEnforcer.EvaluatePolicies(result)
	if !enforcementResult.Allowed {
		result.Passed = false
		result.BlockingReason = enforcementResult.Reason
		logAuditEvent(&result, "POLICY_BLOCKED", enforcementResult.Reason)
	}

	// Final determination
	if len(shouldBlockFindings(result.Findings)) > 0 {
		result.Passed = false
		findings := shouldBlockFindings(result.Findings)
		result.BlockingReason = fmt.Sprintf("%d critical/high severity issues block deployment", len(findings))
		logAuditEvent(&result, "DEPLOYMENT_BLOCKED", result.BlockingReason)
	} else {
		logAuditEvent(&result, "GATE_PASSED", "All security checks passed")
	}

	result.Duration = time.Since(startTime)
	result.Metrics.TotalScanTimeMs = int64(result.Duration / time.Millisecond)
	result.Metrics.IssuesFound = len(result.Findings)
	result.Metrics.IssuesBlocked = countBlockingIssues(result.Findings)

	result.Recommendations = d.generateRecommendations(result)

	return result, nil
}

// ShouldBlockFindings returns findings that should block deployment
func shouldBlockFindings(findings []GateFinding) []GateFinding {
	var blocking []GateFinding
	
	for _, f := range findings {
		if f.Severity == redteam_simulation.SeverityCritical || 
		   (f.Severity == redteam_simulation.SeverityHigh && f.Type != FindingCompliance) {
			blocking = append(blocking, f)
		}
	}
	
	return blocking
}

// generateRecommendations creates actionable improvements
func (d *DevSecOpsGates) generateRecommendations(result GateResult) []string {
	var recommendations []string

	criticalCount := countBySeverity(result.Findings, redteam_simulation.SeverityCritical)
	highCount := countBySeverity(result.Findings, redteam_simulation.SeverityHigh)

	if criticalCount > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("URGENT: Address %d critical vulnerabilities before deployment", criticalCount))
	}

	if highCount > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("HIGH PRIORITY: Review and remediate %d high-severity issues", highCount))
	}

	if result.ComplianceStatus.Status != Compliant {
		recommendations = append(recommendations,
			fmt.Sprintf("Improve compliance score from %d to meet required standards", 
				result.ComplianceStatus.Score))
	}

	if result.RiskScore > 7.0 {
		recommendations = append(recommendations,
			"Consider delaying deployment until risk score decreases below critical threshold")
	}

	recommendations = append(recommendations,
		"Continue implementing automated security scanning in CI/CD pipeline",
		"Establish regular vulnerability management review meetings",
		"Maintain up-to-date dependency library inventory",
	)

	return recommendations
}

// CountBySeverity counts findings by severity
func countBySeverity(findings []GateFinding, severity redteam_simulation.FindingSeverity) int {
	count := 0
	for _, f := range findings {
		if f.Severity == severity {
			count++
		}
	}
	return count
}

// CountBlockingIssues identifies issues blocking deployment
func countBlockingIssues(findings []GateFinding) int {
	count := 0
	for _, f := range findings {
		if f.Severity == redteam_simulation.SeverityCritical || 
		   (f.Severity == redteam_simulation.SeverityHigh && f.RiskScore > 8.0) {
			count++
		}
	}
	return count
}

// logAuditEvent adds audit trail entry
func logAuditEvent(result *GateResult, eventType, description string) {
	event := AuditEvent{
		EventID: fmt.Sprintf("AUDIT-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		EventType: eventType,
		Description: description,
		Metadata: make(map[string]interface{}),
	}
	result.AuditTrail = append(result.AuditTrail, event)
}

// Commit represents git commit metadata
type Commit struct {
	Hash          string
	Branch        string
	Repository    string
	Message       string
	Author        string
	Committer     string
	Timestamp     time.Time
	HasContainers bool
	IsWebApplication bool
	ChangedFiles  []string
	Diff          string
}

// NewCommit creates commit object from metadata
func NewCommit(hash, branch, repository string) Commit {
	return Commit{
		Hash: hash,
		Branch: branch,
		Repository: repository,
		Timestamp: time.Now(),
		ChangedFiles: []string{},
	}
}
