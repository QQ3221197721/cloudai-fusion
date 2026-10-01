// Package container_sec provides container security for image scanning, runtime protection, k8s policy enforcement
package container_sec

import (
	"time"
)

// VulnSeverity represents CVE severity level
type VulnSeverity string

const (
	Critical VulnSeverity = "critical"
	High     VulnSeverity = "high"
	Medium   VulnSeverity = "medium"
	Low      VulnSeverity = "low"
	Unknown  VulnSeverity = "unknown"
)

// SecretType identifies sensitive data found in images
type SecretType string

const (
	SecretAPIKey    SecretType = "api_key"
	SecretPassword  SecretType = "password"
	SecretToken     SecretType = "token"
	SecretCertificate SecretType = "certificate"
)

// ComplianceStandard represents compliance frameworks
type ComplianceStandard string

const (
	StandardOCP           ComplianceStandard = "ocp"
	StandardCIS           ComplianceStandard = "cis"
	StandardNIST          ComplianceStandard = "nist"
	StandardPCI           ComplianceStandard = "pci_dss"
)

// ImageScanResult contains vulnerability analysis
type ImageScanResult struct {
	ID              string                `json:"id"`
	ImageDigest     string                `json:"imageDigest"`
	ImageTag        string                `json:"imageTag"`
	ScannerVersion  string                `json:"scannerVersion"`
	ScanDate        time.Time             `json:"scanDate"`
	Vulnerabilities []VulnSummary         `json:"vulnerabilities"`
	Secrets         []SecretFinding       `json:"secrets"`
	Compliance      map[ComplianceStandard]bool `json:"compliance"`
	Rating          string                `json:"rating"` // A-F grade
}

type VulnSummary struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Severity  VulnSeverity      `json:"severity"`
	Package   string            `json:"package"`
	Version   string            `json:"version"`
	FixedIn   string            `json:"fixedIn"`
	CVEID     string            `json:"cveId,omitempty"`
}

type SecretFinding struct {
	Type     SecretType `json:"type"`
	File     string     `json:"file"`
	Line     int        `json:"line"`
	SecretID string     `json:"secretId"`
}

// RuntimeProtectionConfig defines runtime behavior rules
type RuntimeProtectionConfig struct {
	ID                string   `json:"id"`
	ClusterName       string   `json:"clusterName"`
	Namespace         string   `json:"namespace"`
	AlertsEnabled     bool     `json:"alertsEnabled"`
	BlockLevel        string   `json:"blockLevel"` // none/audit/block
	DroppedSyscalls   []string `json:"droppedSyscalls"`
	AllowedCapabilties []string `json:"allowedCapabilities"`
}

// K8sPolicy defines pod security standards
type K8sPolicy struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Standard       string   `json:"standard"` // baseline/enforced
	PodSecurityEnv string   `json:"podSecurityEnv"` // privileged/disabled/restricted
	MetaTags       []string `json:"metaTags"`
}

// ContainerSecStore interface for persistence
type ContainerSecStore interface {
	// Image scanning
	CreateScanResult(result *ImageScanResult) error
	GetScanResult(id string) (*ImageScanResult, error)
	ListScanResults(filters map[string]any, limit int) ([]ImageScanResult, error)
	
	// Runtime protection
	CreateRuntimeConfig(config *RuntimeProtectionConfig) error
	UpdateRuntimeConfig(id string, updates map[string]any) error
	GetRuntimeConfig(clusterName string) (*RuntimeProtectionConfig, error)
	ListRuntimeConfigs() ([]RuntimeProtectionConfig, error)
	
	// K8s policies
	CreateK8sPolicy(policy *K8sPolicy) error
	UpdateK8sPolicy(id string, updates map[string]any) error
	DeleteK8sPolicy(id string) error
	ListK8sPolicies(limit int) ([]K8sPolicy, error)
	
	// Analytics
	GetImageMetrics() (*ImageMetrics, error)
	GetComplianceReports() ([]ComplianceReport, error)
}

// ImageMetrics tracks container security metrics
type ImageMetrics struct {
	TotalImages     int       `json:"totalImages"`
	ScannedToday    int       `json:"scannedToday"`
	AvgScanTimeSec  float64   `json:"avgScanTimeSec"`
	HighRiskImages  int       `json:"highRiskImages"` // rating D-F
	PassCompliance  int       `json:"passCompliance"` // OC P standard
}

// ComplianceReport summarizes compliance status
type ComplianceReport struct {
	Standard       ComplianceStandard `json:"standard"`
	Status         string             `json:"status"` // pass/fail/unknown
	Checkpoints    int                `json:"checkpoints"`
	Passed         int                `json:"passed"`
	Failed         int                `json:"failed"`
	LastUpdated    time.Time          `json:"lastUpdated"`
}
