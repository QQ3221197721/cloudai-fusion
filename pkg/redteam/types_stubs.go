// Package redteam - Common type definitions for Red Team operations
package redteam

import (
	"context"
	"time"
)

// Credentials represents authentication credentials for AD/Kerberos attacks
type Credentials struct {
	Username     string
	Password     string
	Domain       string
	KDC          string
	Certificate  []byte
	Service      string
}

// TicketFlag represents Kerberos ticket flag
type TicketFlag int

const (
	TicketFlagRenewable TicketFlag = 1 << iota
	TicketFlagForwardable
	TicketFlagPreAuthenticationRequired
)

// GoldenTicketOptions represents configuration for golden ticket creation
type GoldenTicketOptions struct {
	DomainSID              string
	 krb5Hash               string
	Policy                 string
	ValidityDuration       time.Duration
	CreateAdminUser        bool
	AdditionalProperties   map[string]interface{}
}

// TGSOptions represents Configuration for TGS ticket creation  
type TGSOptions struct {
	ServicePrincipal string
	KeyVersionNumber uint32
	ValidityDuration time.Duration
	CustomClaims     map[string]interface{}
}

// ExploitationResult represents the outcome of an exploit attempt
type ExploitationResult struct {
	Status        ExploitStatus
	Vulnerability string
	Evidence      []byte
	ErrorMsg      string
	Metrics       ImpactMetrics
}

// Status represents exploit execution status
type ExploitStatus string

const (
	ExploitStatusPending           ExploitStatus = "pending"
	ExploitStatusSuccessful        ExploitStatus = "successful"
	ExploitStatusFailed            ExploitStatus = "failed"
	ExploitStatusPartiallySuccessful ExploitStatus = "partially_successful"
)

// Evidence represents technical evidence from exploit or attack
type Evidence struct {
	Type        string
	Data        []byte
	Timestamp   time.Time
	Context     context.Context
	Verifiable  bool
	MerkleProof []byte
}

// ConfigValidator validates metasploit configurations
type ConfigValidator struct {
	rpcEndpoint string
	apiKey      string
	timeout     time.Duration
	validated   bool
}

// NewConfigValidator creates a new configuration validator
func NewConfigValidator(rpcEndpoint string, apiKey string, timeout time.Duration) *ConfigValidator {
	return &ConfigValidator{
		rpcEndpoint: rpcEndpoint,
		apiKey:      apiKey,
		timeout:     timeout,
		validated:   false,
	}
}

// ImpactMetrics contains quantitative metrics about exploit impact
type ImpactMetrics struct {
	AchievementRate float64
	Confidence      float64
	BlastRadius     float64
	DetectionRate   float64
	TimeToExploit   float64
}

// BaseMetric represents base metric calculation method
type BaseMetric interface {
	Calculate() float64
	Validate() bool
	Serialize() ([]byte, error)
}

// ExploitMetrics contains aggregated metrics collection
type ExploitMetrics struct {
	Exploits    []string
	Targets     []string
	Impacts     []ImpactMetrics
	GeneratedAt time.Time
}

// Configuration represents exploit framework configuration
type Configuration struct {
	Scanners   []string
	Exploits   []string
	Payloads   []string
	Targets    []TargetSystem
	Timeout    time.Duration
	RetryCount int
}

// TargetSystem represents a target system description
type TargetSystem struct {
	IP             string
	Port           int
	OS             string
	Services       []ServiceInfo
	Flags          []string
	IsCloud        bool
	EmailList      []string
	Name           string
	URL            string
	SourceCodePath string
}

// ServiceInfo represents information about a service
type ServiceInfo struct {
	Name    string
	Version string
	Port    int
	Protocol string
}

// NetworkPacket represents a network packet structure
type NetworkPacket struct {
	Header    []byte
	Payload   []byte
	Timestamp time.Time
	Source    string
	Destination string
}

// VulnerabilityReport represents a vulnerability finding
type VulnerabilityReport struct {
	ID             string
	Score          float64
	Severity       string
	Description    string
	CVE            string
	MetasploitModule string
	Remediation    string
	References     []string
	Tactics        []string
	Techniques     []string
	KillChain      []string
}

// ExploitFinding represents an exploit finding with detailed information
type ExploitFinding struct {
	VulnerabilityReport
	ExploitDetails struct {
		AvailableExploits []string
		ProofOfConcept    string
		PayloadOptions    []string
	}
}
