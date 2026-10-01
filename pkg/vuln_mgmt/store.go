// Package vuln_mgmt provides vulnerability management for CVE scoring, prioritization and remediation tracking
package vuln_mgmt

import (
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// Severity represents vulnerability severity level
type Severity string

const (
	CriticalSeverity Severity = "critical"
	HighSeverity     Severity = "high"
	MediumSeverity   Severity = "medium"
	LowSeverity      Severity = "low"
)

// ExploitAvailability indicates exploit availability status
type ExploitAvailability string

const (
	NoExploit       ExploitAvailability = "no_exploit"
	ProofOfConcept  ExploitAvailability = "proof_of_concept"
	ActiveExploit   ExploitAvailability = "active_exploit"
	CommercialSpyware ExploitAvailability = "commercial_spyware"
)

// VulnerabilityType categorizes vulnerability sources
type VulnerabilityType string

const (
	TypeOpenSource VulnerabilityType = "open_source"
	TypeProprietary VulnerabilityType = "proprietary"
	TypeCustomApp  VulnerabilityType = "custom_application"
	TypeThirdParty VulnerabilityType = "third_party"
)

// RemediationStatus tracks fix progress
type RemediationStatus string

const (
	StatusNotStarted  RemediationStatus = "not_started"
	StatusInProgress  RemediationStatus = "in_progress"
	StatusPendingReview RemediationStatus = "pending_review"
	StatusCompleted    RemediationStatus = "completed"
	StatusDeferred     RemediationStatus = "deferred"
)

// PriorityLevel indicates remediation priority
type PriorityLevel string

const (
	CriticalPriority PriorityLevel = "critical"
	HighPriority     PriorityLevel = "high"
	MediumPriority   PriorityLevel = "medium"
	LowPriority      PriorityLevel = "low"
)

// Vulnerability represents a security vulnerability with enriched data
type Vulnerability struct {
	ID            string        `json:"id"`
	CVEID         string        `json:"cveId"`
	CVSSv2Score   float64       `json:"cvssv2Score,omitempty"`
	CVSSv3Score   float64       `json:"cvssv3Score"`
	CVSSv3Vector  string        `json:"cvssv3Vector,omitempty"`
	EpssPercentile float64      `json:"epssPercentile"` // Probability of exploitation in next 30 days
	EpssScoredAt  time.Time     `json:"epssScoredAt"`
	PublishedDate time.Time     `json:"publishedDate"`
	UpdatedDate   time.Time     `json:"updatedDate"`
	Description   string        `json:"description"`
	CWEID         string        `json:"cweId,omitempty"`
	References    []string      `json:"references"`
	VulnerabilityType VulnerabilityType `json:"vulnerabilityType"`
	ExploitAvailability ExploitAvailability `json:"exploitAvailability"`
	AffectedPackages []PackageInfo `json:"affectedPackages"`
	
	// Risk scoring
	RiskScore           float64          `json:"riskScore"` // 0-100 AI-calculated risk score
	Priority            PriorityLevel    `json:"priority"`  // critical/high/medium/low
	ContextualRiskFactors []RiskFactor    `json:"contextualRiskFactors"`
}

// PackageInfo describes affected software package
type PackageInfo struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Installed  bool   `json:"installed"`
	FixedIn    string `json:"fixedIn,omitempty"`
	DependsOn  string `json:"dependsOn,omitempty"`
}

// RiskFactor captures contextual elements that influence risk
type RiskFactor struct {
	Type       string `json:"type"`
	Weight     float64 `json:"weight"`
	Justification string `json:"justification"`
}

// VulnerabilityFinding links vulnerability to specific assets
type VulnerabilityFinding struct {
	ID            string        `json:"id"`
	VulnerabilityID string      `json:"vulnerabilityId"`
	AssetID       string        `json:"assetId"`
	AssetName     string        `json:"assetName"`
	AssetType     string        `json:"assetType"`
	DetectedAt    time.Time     `json:"detectedAt"`
	PathToExploit []string      `json:"pathToExploit,omitempty"` // Attack chain analysis
	Status        string        `json:"status"` // active, suppressed, investigated, resolved
	Notes         string        `json:"notes,omitempty"`
}

// RemediationPlan defines fix timeline and approach
type RemediationPlan struct {
	ID             string            `json:"id"`
	VulnerabilityID string          `json:"vulnerabilityId"`
	Title          string            `json:"title"`
	Description    string            `json:"description"`
	Action         string            `json:"action"` // patch/update/block/config_change
	AssignedTo     string            `json:"assignedTo"`
	Deadline       time.Time         `json:"deadline"`
	Status         RemediationStatus `json:"status"`
	CreatedAt      time.Time         `json:"createdAt"`
	CompletedAt    time.Time         `json:"completedAt,omitempty"`
	VerificationEvidence *evidence.Attestation `json:"verificationEvidence,omitempty"`
}

// VulnerabilityStore interface for persistence layer
type VulnerabilityStore interface {
	// Vulnerability CRUD
	CreateVulnerability(v *Vulnerability) error
	GetVulnerability(id string) (*Vulnerability, error)
	UpdateVulnerability(id string, updates map[string]any) error
	DeleteVulnerability(id string) error
	ListVulnerabilities(filters map[string]any, limit, offset int) ([]Vulnerability, error)
	BulkCreate(vulnerabilities []*Vulnerability) error
	
	// Finding operations
	CreateFinding(f *VulnerabilityFinding) error
	UpdateFindingStatus(id string, status string) error
	ListFindings(filters map[string]any, limit, offset int) ([]VulnerabilityFinding, error)
	FindingsByVulnerability(vulnID string) ([]VulnerabilityFinding, error)
	FindingsByAsset(assetID string) ([]VulnerabilityFinding, error)
	
	// Remediation plan operations
	CreateRemediationPlan(rp *RemediationPlan) error
	GetRemediationPlan(id string) (*RemediationPlan, error)
	UpdateRemediationPlan(id string, updates map[string]any) error
	DeleteRemediationPlan(id string) error
	ListRemediationPlans(filters map[string]any, limit, offset int) ([]RemediationPlan, error)
	
	// Analytics and scoring
	GetRiskMetrics() (*RiskMetrics, error)
	GetTopVulnerabilities(limit int) ([]Vulnerability, error)
	GetVulnerabilityTrends(startDate, endDate time.Time) ([]TrendData, error)
}

// RiskMetrics provides aggregated security metrics
type RiskMetrics struct {
	TotalVulnerabilities int                `json:"totalVulnerabilities"`
	BySeverity         map[Severity]int    `json:"bySeverity"`
	ByPriority         map[PriorityLevel]int `json:"byPriority"`
	AverageRiskScore   float64            `json:"averageRiskScore"`
	HighestRiskAssets  []AssetRiskSummary `json:"highestRiskAssets"`
	ExploitableCount   int                `json:"exploitableCount"` // CVSS >= 7 + exploit available
	MeanTimeToRemediate time.Time         `json:"meanTimeToRemediate"` // Stats calculation
}

// AssetRiskSummary ranks assets by cumulative risk
type AssetRiskSummary struct {
	AssetID       string     `json:"assetId"`
	AssetName     string     `json:"assetName"`
	VulnCount     int        `json:"vulnCount"`
	MaxRiskScore  float64    `json:"maxRiskScore"`
	AvgRiskScore  float64    `json:"avgRiskScore"`
	CriticalCount int        `json:"criticalCount"`
}

// TrendData shows vulnerability evolution over time
type TrendData struct {
	Date  time.Time `json:"date"`
	Total int       `json:"total"`
	New   int       `json:"new"`
	Resolved int    `json:"resolved"`
	AvgRiskScore float64 `json:"avgRiskScore"`
}
