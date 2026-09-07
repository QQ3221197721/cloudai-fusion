package redteam

import (
	"time"
)

// VulnerabilityType categorizes different vulnerability classes
type VulnerabilityType int

const (
	None VulnerabilityType = iota
	SQLInjection
	XSSReflected
	XSSStored
	XSSDOMBased
	CSRF
	SSRF
	CommandInjection
	PathTraversal
	InsecureDeserialization
	SensitiveDataExposure
	BrokenAuthentication
	VulnerableComponent
	XMLExternalEntities
	FailedAuthentication
	InsufficientLogging
	
	// Network-specific
	GoldenTicket
	SilverTicket
	PassTheHash
	Kerberoasting
	ASREPRoasting
	DNSRebinding
	NFSManipulation
	SMBRelay
	
	// Binary-specific
	BufferOverflow
	UseAfterFree
	IntegerOverflow
	DataRace
	ROPChain
	SyscallInjection
	
	// Web-specific
	OWASPTop10_1 // Broken Access Control
	OWASPTop10_2 // Cryptographic Failures
	OWASPTop10_3 // Injection
	OWASPTop10_4 // Insecure Design
	OWASPTop10_5 // Security Misconfiguration
	OWASPTop10_6 // Vulnerable Components
	OWASPTop10_7 // Authentication Failures
	OWASPTop10_8 // Software Integrity
	OWASPTop10_9 // Logging Failures
	OWASPTop10_10 // Server Side Request Forgery
)

func (vt VulnerabilityType) String() string {
	names := map[VulnerabilityType]string{
		None: "none",
		SQLInjection: "sql-injection",
		XSSReflected: "xss-reflected",
		XSSStored: "xss-stored",
		XSSDOMBased: "xss-dom",
		CSRF: "csrf",
		SSRF: "ssrf",
		CommandInjection: "command-injection",
		PathTraversal: "path-traversal",
		InsecureDeserialization: "insecure-deserialization",
		SensitiveDataExposure: "sensitive-data-exposure",
		BrokenAuthentication: "broken-authentication",
		VulnerableComponent: "vulnerable-component",
		XMLExternalEntities: "xxe",
		FailedAuthentication: "failed-authentication",
		InsufficientLogging: "insufficient-logging",
		
		GoldenTicket: "golden-ticket",
		SilverTicket: "silver-ticket",
		PassTheHash: "pass-the-hash",
		Kerberoasting: "kerberoasting",
		ASREPRoasting: "asreprotating",
		DNSRebinding: "dns-rebinding",
		NFSManipulation: "nfs-manipulation",
		SMBRelay: "smb-relay",
		
		BufferOverflow: "buffer-overflow",
		UseAfterFree: "use-after-free",
		IntegerOverflow: "integer-overflow",
		DataRace: "data-race",
		ROPChain: "rop-chain",
		SyscallInjection: "syscall-injection",
		
		OWASPTop10_1: "owasp-top-10-1-broken-access-control",
		OWASPTop10_2: "owasp-top-10-2-cryptographic-failures",
		OWASPTop10_3: "owasp-top-10-3-injection",
		OWASPTop10_4: "owasp-top-10-4-insecure-design",
		OWASPTop10_5: "owasp-top-10-5-security-misconfiguration",
		OWASPTop10_6: "owasp-top-10-6-vulnerable-components",
		OWASPTop10_7: "owasp-top-10-7-authentication-failures",
		OWASPTop10_8: "owasp-top-10-8-software-integrity-failures",
		OWASPTop10_9: "owasp-top-10-9-logging-failures",
		OWASPTop10_10: "owasp-top-10-10-serverside-request-forgery",
	}
	
	if name, ok := names[vt]; ok {
		return name
	}
	return "unknown"
}

// Severity defines vulnerability criticality levels
type Severity int

const (
	Info Severity = iota + 1
	Low
	Medium
	High
	Critical
	Highest
)

func (s Severity) String() string {
	names := map[Severity]string{
		Info:    "info",
		Low:     "low",
		Medium:  "medium",
		High:    "high",
		Critical: "critical",
		Highest: "highest",
	}
	
	if name, ok := names[s]; ok {
		return name
	}
	return "unknown"
}

func (s Severity) Level() int {
	return int(s)
}

// EngagementScope defines the boundaries of a red team engagement
type EngagementScope struct {
	ID               EngagementID
	TargetNetworks   []string
	AllowedTargets   []string
	ProhibitedActs   []AttackTechnique
	RiskTier         RiskTier
	IsolationMode    IsolationLevel
	MaxExecutionTime time.Duration
	ApprovalRequired bool
	Approvers        []string
}

// EngagementStatus tracks engagement lifecycle state
type EngagementStatus int

const (
	Planned EngagementStatus = iota
	Authorized
	Running
	Paused
	Completed
	Aborted
)

func (es EngagementStatus) String() string {
	names := map[EngagementStatus]string{
		Planned:   "planned",
		Authorized: "authorized",
		Running:   "running",
		Paused:    "paused",
		Completed: "completed",
		Aborted:   "aborted",
	}
	
	if name, ok := names[es]; ok {
		return name
	}
	return "unknown"
}

// AttackPhase represents distinct phases in attack chain execution
type AttackPhase int

const (
	PhaseUnspecified AttackPhase = iota
	PhaseReconnaissance
	PhaseInitialAccess
	PhaseNetworkPenetration
	PhaseWebApplicationAttack
	PhaseBinaryExploitation
	PhasePostExploitation
	PhaseMultiVectorChain
	PhaseReporting
)

func (ap AttackPhase) String() string {
	names := map[AttackPhase]string{
		PhaseUnspecified:          "unspecified",
		PhaseReconnaissance:       "reconnaissance",
		PhaseInitialAccess:        "initial-access",
		PhaseNetworkPenetration:   "network-penetration",
		PhaseWebApplicationAttack: "web-application-attack",
		PhaseBinaryExploitation:   "binary-exploitation",
		PhasePostExploitation:     "post-exploitation",
		PhaseMultiVectorChain:     "multi-vector-chain",
		PhaseReporting:            "reporting",
	}
	
	if name, ok := names[ap]; ok {
		return name
	}
	return "unknown"
}

// AttackStageType defines stage classifications in attack chains
type AttackStageType int

const (
	UnknownStage AttackStageType = iota
	ReconStage
	NetworkPenetrationStage
	WebApplicationStage
	BinaryExploitationStage
	PostExploitationStage
	MultiVectorStage
)

func (ast AttackStageType) String() string {
	names := map[AttackStageType]string{
		UnknownStage:           "unknown",
		ReconStage:             "reconnaissance",
		NetworkPenetrationStage: "network-penetration",
		WebApplicationStage:    "web-application",
		BinaryExploitationStage: "binary-exploitation",
		PostExploitationStage:  "post-exploitation",
		MultiVectorStage:       "multi-vector",
	}
	
	if name, ok := names[ast]; ok {
		return name
	}
	return "unknown"
}

// VulnerabilityFinding represents discovered security issue
type VulnerabilityFinding struct {
	Type           VulnerabilityType
	Severity       Severity
	Confidence     float64 // 0.0 to 1.0
	URL            string
	Parameter      string
	Method         string
	Payload        string
	Request        string
	Response       string
	Description    string
	Impact         string
	Mitigation     string
	Remediation    string
	Evidence       map[string]interface{}
	CVE            string
	CWE            string
	Reference      []string
	Temporality    string // "current", "historic", "future"
	TenantID       string
	DiscoveryTime  time.Time
	FirstSeen      time.Time
	LastSeen       time.Time
	Active         bool
	Verified       bool
	Exploitable    bool
	DemoPoC        string
	
	BinaryPath     string
	FunctionName   string
	Line           int
	Context        map[string]interface{}
	
	// Multi-stage context
	ChainedFrom    []string
	ImpactsNextStages bool
	
	SuccessRate    float64    // For evasion techniques
	BypassedMitigations []string
}

// PhaseResult captures results from executing attack phase
type PhaseResult struct {
	ID              string
	EngagementID    EngagementID
	Phase           AttackPhase
	Status          string
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	Findings        []VulnerabilityFinding
	TargetsTested   int
	VectorsAttempted int
	SuccessCount    int
	FailureCount    int
	ScopeCoverage   float64
	EvasionRate     float64
	CVECoverage     float64
	AverageImpact   float64
	Chained         bool
	Stages          int
	BinariesTested  int
	PivotPointsFound int
	CredentialsDumped int
	PrivilegeEscalated int
	
	// Context extraction for chaining
	extractedContext map[string]interface{}
}

// ExtractContext prepares context for next attack stage
func (pr *PhaseResult) ExtractContext() map[string]interface{} {
	if pr.extractedContext != nil {
		return pr.extractedContext
	}
	
	pr.extractedContext = map[string]interface{}{
		"findings": pr.Findings,
		"severity_breakdown": countBySeverity(pr.Findings),
		"exploited_vectors": pr.VectorsAttempted,
		"accessible_targets": pr.TargetsTested,
		"mitigations_bypassed": collectBypassedMitigations(pr.Findings),
		"credentials_compromised": pr.CredentialsDumped,
	}
	
	return pr.extractedContext
}

// Merge combines two phase results
func (pr *PhaseResult) Merge(other *PhaseResult) {
	pr.Findings = append(pr.Findings, other.Findings...)
	pr.TargetsTested += other.TargetsTested
	pr.VectorsAttempted += other.VectorsAttempted
	pr.SuccessCount += other.SuccessCount
	pr.FailureCount += other.FailureCount
	
	if other.Duration > pr.Duration {
		pr.Duration = other.Duration
	}
}

// StageResult wraps single attack stage outcome
type StageResult struct {
	StageID     string
	Type        AttackStageType
	Input       map[string]interface{}
	PhaseResult *PhaseResult
	ErrorMessage string
	Success     bool
	Duration    time.Duration
}

// ExtractionContext holds data passed between attack stages
type StageContext struct {
	DiscoveredCredentials []Credential
	CompromisedHosts      []string
	ValidTickets          []TicketBlob
	ObtainedShellcodes    []byte
	ConstructedROPChains  []ROPChain
	PivotedHosts          []PivotPoint
	MitigationsBypassed   []string
	ExploitTemplates      []string
}

// Merge updates context with findings from another result
func (sc *StageContext) Merge(result *PhaseResult) {
	for _, f := range result.Findings {
		if f.Type == GoldenTicket || f.Type == SilverTicket {
			sc.ValidTickets = append(sc.ValidTickets, TicketBlob{})
		}
		
		if f.BypassedMitigations != nil {
			sc.MitigationsBypassed = append(sc.MitigationsBypassed, f.BypassedMitigations...)
		}
		
		if f.URL != "" {
			sc.CompromisedHosts = append(sc.CompromisedHosts, f.URL)
		}
	}
}

// ExecutionOutcome summarizes multi-stage attack chain execution
type ExecutionOutcome struct {
	ChainID         string
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	Success         bool
	StagesCompleted int
	TotalStages     int
	Findings        []VulnerabilityFinding
	TotalFindings   int
	ErrorMessage    string
	FinalStage      int
	AverageImpact   float64
	EvasionRate     float64
}

// TargetAnalysis aggregates reconnaissance findings
type TargetAnalysis struct {
	CriticalAssets     []CriticalAsset
	VulnerableServices []VulnerableService
	PossiblePaths      []AttackPath
	ServiceCount       int
	AssetCount         int
	OSDistribution     map[string]int
	ProtocolBreakdown  map[string]int
	TopCVEs            []string
	HighestRiskScore   float64
}

// EngagementReport contains comprehensive assessment results
type EngagementReport struct {
	ID                  EngagementID
	Scope               EngagementScope
	StartTime           time.Time
	EndTime             time.Time
	TotalDuration       time.Duration
	PhasesExecuted      int
	TotalFindings       int
	SeverityBreakdown   map[Severity]int
	Cex3Score           float64
	FLIPBenchmark       FLIPBenchmarkData
	ExecutiveSummary    string
	DetailedFindings    []VulnerabilityFinding
	Recommendations     []string
	EvidenceChain       []EvidenceRecord
	ComplianceStatus    string
	NextSteps           []string
}

// FLIPBenchmarkData aligns with FLIP benchmark standard
type FLIPBenchmarkData struct {
	FindingDensity       float64 // Findings per KLOC
	AverageSeverity      float64 // 0.0 to 1.0 scale
	EvasionSuccess       float64 // Percentage
	RemediationAccuracy  float64 // Percentage
	FalsePositiveRate    float64 // Percentage
	ExecutionSpeed       float64 // Vulns/second
	AttackChainEfficiency float64 // Success rate across chains
	CrossLayerCoordination float64 // Multi-vector effectiveness
}

// EngagementMetrics provides comprehensive metrics
type EngagementMetrics struct {
	EngagementID      EngagementID
	TotalTime         time.Duration
	TotalFindings     int
	SeverityBreakdown map[Severity]int
	CEX3Score         float64
	FLIPBenchmark     FLIPBenchmarkData
	PhaseBreakdown    map[AttackPhase]PhaseMetrics
}

type PhaseMetrics struct {
	Duration   time.Duration
	Findings   int
	Targets    int
	SuccessRate float64
}

// CriticalAsset identifies high-value targets
type CriticalAsset struct {
	ID           string
	Name         string
	Criticality  Severity
	Type         string
	Owner        string
	Location     string
	ExposedPorts []int
	DataSensitivity string
	ComplianceRequirements []string
}

// VulnerableService describes vulnerable endpoint
type VulnerableService struct {
	Target       string
	Port         int
	Protocol     string
	Application  string
	Version      string
	CVEs         []string
	Exploitable  bool
	Risk         float64
	LastScanned  time.Time
}

// AttackPath represents potential exploitation route
type AttackPath struct {
	ID         string
	Stages     []AttackStage
	SuccessProb float64
	AvgImpact  float64
	DurationEst time.Duration
}

// EvidenceRecord logs audit trail entry
type EvidenceRecord struct {
	Timestamp   time.Time
	Action      string
	Description string
	Input       map[string]interface{}
	Output      map[string]interface{}
	Backends    []BackendFact
	Signature   string
	ChainHash   string
}

type BackendFact struct {
	Component string
	Mode      string
	Driver    string
}

// Credential stores compromised credentials
type Credential struct {
	Username string
	Hash     string
	Plaintext string
	Source   string
	HashType string
	Effective bool
}

// TicketBlob represents Kerberos ticket
type TicketBlob struct {
	Realm      string
	TicketName AccountName
	Flags      uint32
	Expiration time.Time
	Key        CryptoKey
	PAC        []byte
}

// ROPChain represents reduced instruction sequence
type ROPChain struct {
	Gadgets []ROPGadget
	MemoryLayout []uintptr
	BypassedMitigations []string
	Payload []byte
}

// ROPGadget is single gadget instruction sequence
type ROPGadget struct {
	Address      uintptr
	Instructions []byte
	Destinations []RegisterDestination
}

// RegisterDestination defines target register state
type RegisterDestination struct {
	Register string
	Value    uint64
}

// PivotPoint represents network pivot capability
type PivotPoint struct {
	Hostname   string
	IP         string
	Port       int
	Credential Credential
	TrustLevel float64
}

// AttackGraph manages attack relationships
type AttackGraph struct {
	Nodes    map[string]*AttackNode
	Edges    []*AttackEdge
	RootNodes []string
}

// AttackNode represents attack step
type AttackNode struct {
	ID        string
	Type      AttackStageType
	State     string
	Prereqs   []string
	Consequents []string
	Risk      float64
	Impact    float64
}

// AttackEdge represents transition between nodes
type AttackEdge struct {
	Source string
	Target string
	Weight float64
	Type   string
}

// AttackChain defines complete exploitation sequence
type AttackChain struct {
	ID       string
	Name     string
	Goal     string
	Stages   []AttackStage
	SuccessProb float64
	DurationEst time.Duration
}

// AttackStage represents single step in chain
type AttackStage struct {
	ID          string
	Type        AttackStageType
	Order       int
	Description string
	Input       map[string]interface{}
	ExpectedOutput interface{}
	Prerequisites []string
	RiskLevel   Severity
	Tool        string
	Technique   string
}

// AccountName represents Kerberos principal
type AccountName struct {
	NameType  uint32
	NameString []string
	Realm     string
}

// CryptoKey represents encryption key material
type CryptoKey struct {
	KeyMaterial []byte
	KeyType     uint32
	Derivation string
}
