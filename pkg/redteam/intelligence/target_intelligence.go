package intelligence

import (
	"encoding/json"
	"fmt"
	"time"
	
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/exploit"
)

// ============================================================================
// DEEP TARGET INTELLIGENCE SYSTEM (知彼) - Environment Reconnaissance Core
// ============================================================================

// OSProfile 操作系统详细配置
type OSProfile struct {
	Name        string    `json:"name"`          // "Windows Server 2019" / "Ubuntu 20.04"
	Version     string    `json:"version"`       // "1803" / "20.04"
	Build       string    `json:"build"`         // "17763.4374"
	PatchLevel  float64   `json:"patchLevel"`    // 0-1, percentage of patches applied
	Kernel      string    `json:"kernel,omitempty"`
	CVEs        []string  `json:"knownCVEs"`
	Hardening   float64   `json:"hardeningScore"` // 0-1, security hardening level
}

// NetworkTopologyMap 网络拓扑映射
type NetworkTopology struct {
	Segments    []NetworkSegment `json:"segments"`
	Firewalls   []FirewallConfig `json:"firewalls"`
	Routers     []RouterConfig   `json:"routers"`
	LoadBalancers []LoadBalancerConfig `json:"loadBalancers"`
	VLANMappings map[string]int   `json:"vlanMappings,omitempty"`
}

type NetworkSegment struct {
	Name        string   `json:"name"`
	CIDR        string   `json:"cidr"`
	Gateway     string   `json:"gateway"`
	SecurityEnv string   `json:"securityEnvironment"` // "dmz" / "internal" / "restricted"
	SensitiveData bool   `json:"containsSensitiveData"`
}

type FirewallConfig struct {
	Model       string    `json:"model"`            // "Palo Alto PA-3200 series"
	Version     string    `json:"version"`          // "10.1.0-h10"
	RulesCount  int       `json:"rulesCount"`
	LastUpdated time.Time `json:"lastUpdated"`
}

type RouterConfig struct {
	Model     string `json:"model"`      // "CISCO ISR 4000 series"
	Firmware  string `json:"firmware"`
	Routing   string `json:"routingProtocol"` // "BGP", "OSPF"
}

type LoadBalancerConfig struct {
	Model    string   `json:"model"`       // "F5 BIG-IP"
	Version  string   `json:"version"`
	Backend  []string `json:"backendServers"`
	SSLOff   bool     `json:"sslTermination"`
}

// SecurityProducts 安全产品组合
type SecurityProducts struct {
	EDRs           []EDRConfig           `json:"edr,omitempty"`
	WAFs           []WAFConfig           `json:"waf,omitempty"`
	IPSSystems     []IPSSystem           `json:"ips,omitempty"`
	SIEMs          []SIEMConfig          `json:"siem,omitempty"`
	LogManagement  []LogManagementSystem `json:"logManagement,omitempty"`
}

type EDRConfig struct {
	Name         string    `json:"name"`         // "CrowdStrike Falcon"
	Version      string    `json:"version"`      // "v6.29"
	Status       string    `json:"status"`       // "active" / "inactive"
	Monitoring   bool      `json:"monitoring"`
	LastSeen     time.Time `json:"lastSeen,omitempty"`
	Capabilities []string  `json:"capabilities"`
}

type WAFConfig struct {
	Name         string    `json:"name"`         // "ModSecurity"
	Version      string    `json:"version"`      // "v3.0"
	OWASPRules   int       `json:"owaspRules"`
	CloudFlare   bool      `json:"cloudflare"`
	RulesCount   int       `json:"rulesCount"`
}

type IPSSystem struct {
	Name     string    `json:"name"`       // "Snort" / "Suricata"
	Version  string    `json:"version"`    // "3.1" / "6.0"
	Ruleset  string    `json:"ruleset"`    // "Oinkcode"
	Updates  time.Time `json:"updates,omitempty"` // Last signature update
}

type SIEMConfig struct {
	Name         string    `json:"name"`         // "Splunk Enterprise"
	Version      string    `json:"version"`      // "9.1"
	Indexes      []string  `json:"indexes"`
	Correlation  int       `json:"correlationRules"` // Number of active rules
}

type LogManagementSystem struct {
	Name        string    `json:"name"`         // "Graylog"
	Version     string    `json:"version"`      // "4.4"
	Retention   int       `json:"retentionDays"` // Days of log retention
	RealTime    bool      `json:"realTimeAlerting"`
}

// VulnerabilitySurface 漏洞表面分析
type VulnerabilitySurface struct {
	CVEs              map[string]VulnProfile  `json:"knownCVEs"`
	AttackVectors     []AttackVector          `json:"exploitPathways,omitempty"`
	HardeningScore    HardeningScore          `json:"hardeningScore"`
	ExposedServices   []ServiceExposure       `json:"exposedServices,omitempty"`
}

// VulnProfile CVE 详细配置文件
type VulnProfile struct {
	CVE               string    `json:"cve"`
	CVSS              float64   `json:"cvss"`                // 0-10
	Description       string    `json:"description"`
	ExploitAvailable  bool      `json:"exploitAvailable"`
	PublicPOC         string    `json:"publicPOC,omitempty"` // GitHub URL or repository
	AffectedVersions  []string  `json:"affectedVersions"`
	Remediation       string    `json:"remediation"`
	DiscoveredAt      time.Time `json:"discoveredAt"`
}

// AttackVector 攻击向量
type AttackVector struct {
	Type            string             `json:"type"` // "web-entrypoint" / "network-service"
	Endpoints       []WebEndpoint      `json:"endpoints,omitempty"`
	OpenPorts       []PortExposure     `json:"ports,omitempty"`
}

type WebEndpoint struct {
	URL         string  `json:"url"`
	Method      string  `json:"method"` // "GET" / "POST"
	Vulnerable    bool    `json:"vulnerable"`
	SQLiRisk    string  `json:"sqliRisk,omitempty"` // "low" / "medium" / "high"
	XSSRisk     string  `json:"xssRisk,omitempty"`
	UploadRisk  string  `json:"uploadRisk,omitempty"`
}

type PortExposure struct {
	Port        int    `json:"port"`
	Service     string `json:"service"`
	Version     string `json:"version"`
	Vulnerable    bool   `json:"vulnerable"`
	RiskLevel   string `json:"riskLevel"` // "low" / "medium" / "high"
}

type ServiceExposure struct {
	Name        string      `json:"name"`
	Port        int         `json:"port"`
	Version     string      `json:"version"`
	Banner      string      `json:"banner,omitempty"`
	Authentication bool      `json:"authenticationRequired"`
}

type HardeningScore struct {
	OSHardening        float64 `json:"osHardening"`         // 0-1
	NetworkHardening   float64 `json:"networkHardening"`    // 0-1
	ApplicationHardening float64 `json:"applicationHardening"` // 0-1
	OverallSecurityPosture float64 `json:"overallSecurityPosture"` // 0-1
}

// ActiveDefenseStack 主动防御体系
type ActiveDefenseStack struct {
	RealTimeMonitoring []DetectionSystem `json:"realTimeMonitoring"`
	LogAggregation     LogAggregationCapabilities `json:"logAggregation"`
	IncidentResponse   IncidentResponseCapability `json:"incidentResponse"`
	ComplianceRequirements []string `json:"complianceRequirements"` // ["PCI-DSS", "HIPAA", "SOC2"]
}

// DetectionSystem 检测系统配置
type DetectionSystem struct {
	Name              string    `json:"name"`
	Type              string    `json:"type"` // "EDR" / "SIEM" / "NIDS"
	MonitoringEnabled bool      `json:"monitoringEnabled"`
	AlertThreshold    string    `json:"alertThreshold"` // "low" / "medium" / "high"
	AutomatedResponse bool      `json:"automatedResponse"`
	ResponseTime      string    `json:"responseTime"`
	Capabilities      []string  `json:"capabilities"`
}

// LogAggregationCapabilities 日志聚合能力
type LogAggregationCapabilities struct {
	SIEMPlatforms     []string `json:"siemPlatforms"`
	RetentionPeriod   int      `json:"retentionDays"` // days
	RealTimeAlerting  bool     `json:"realTimeAlerting"`
	CorrelationRules  int      `json:"correlationRules"`
	DataSources       []string `json:"dataSources"`
}

// IncidentResponseCapability 应急响应能力
type IncidentResponseCapability struct {
	AutomationLevel     string   `json:"automationLevel"` // "manual" / "partially-automated" / "fully-automated"
	SOARIntegration     bool     `json:"soarIntegration"`
	ResponsePlaybooks   int      `json:"responsePlaybooks"`
	MeanTimeToRespond   string   `json:"meanTimeToRespond"`
	ContainmentStrategy []string `json:"containmentStrategies"`
}

// DeepTargetIntelligence 完整的目标情报结构
type DeepTargetIntelligence struct {
	// Basic Information
	TargetID        string   `json:"targetId"`
	TargetName      string   `json:"targetName"`
	ReconTimestamp  time.Time `json:"reconTimestamp"`
	IntelligenceLevel string  `json:"intelligenceLevel"` // "full" / "partial" / "minimal"
	
	// Environment Reconnaissance - 敌情环境侦察
	Environment EnvironmentProfile `json:"environment"`
	
	// Vulnerability Surface Analysis - 弱点点位测绘
	VulnerabilitySurface *VulnerabilitySurface `json:"vulnerabilitySurface,omitempty"`
	
	// Active Defense Detection - 主动防御监测
	ActiveDefense *ActiveDefenseStack `json:"activeDefense,omitempty"`
	
	// Intelligence Metadata
	DataSources []string `json:"dataSources"` // How we collected this info
	Confidence  float64  `json:"confidence"` // 0-1, data confidence score
}

// EnvironmentProfile 环境档案
type EnvironmentProfile struct {
	OperatingSystems   map[string]OSProfile  `json:"operatingSystems"`
	NetworkTopology    *NetworkTopology      `json:"networkTopology,omitempty"`
	SecurityProducts   *SecurityProducts     `json:"securityProducts,omitempty"`
}

// JSON Serialization Methods

// ToJSON 序列化到 JSON
func (ti *DeepTargetIntelligence) ToJSON() ([]byte, error) {
	return json.MarshalIndent(ti, "", "  ")
}

// String 字符串表示
func (ti *DeepTargetIntelligence) String() string {
	b, _ := ti.ToJSON()
	return string(b)
}

// GetTargetID 获取目标 ID
func (ti *DeepTargetIntelligence) GetTargetID() string {
	return ti.TargetID
}

// HasEDR 检查是否有 EDR 防护
func (ti *DeepTargetIntelligence) HasEDR() bool {
	if ti.ActiveDefense == nil || ti.Environment.SecurityProducts == nil {
		return false
	}
	for _, edr := range ti.Environment.SecurityProducts.EDRs {
		if edr.Monitoring && edr.Status == "active" {
			return true
		}
	}
	return false
}

// HasSIEM 检查是否有 SIEM 日志平台
func (ti *DeepTargetIntelligence) HasSIEM() bool {
	if ti.ActiveDefense == nil || ti.ActiveDefense.LogAggregation.SIEMPlatforms == nil {
		return false
	}
	return len(ti.ActiveDefense.LogAggregation.SIEMPlatforms) > 0
}

// GetKnownCVEs 获取已知 CVE 列表
func (ti *DeepTargetIntelligence) GetKnownCVEs() []string {
	if ti.VulnerabilitySurface == nil {
		return nil
	}
	var cves []string
	for cve := range ti.VulnerabilitySurface.CVEs {
		cves = append(cves, cve)
	}
	return cves
}

// RiskAssessment 风险评估
func (ti *DeepTargetIntelligence) RiskAssessment() string {
	var severity string
	var recommendations []string
	
	// 综合评分
	riskScore := ti.calculateRiskScore()
	
	switch {
	case riskScore >= 0.8:
		severity = "CRITICAL"
	case riskScore >= 0.6:
		severity = "HIGH"
	case riskScore >= 0.4:
		severity = "MEDIUM"
	default:
		severity = "LOW"
	}
	
	// 生成建议
	if ti.HasEDR() {
		recommendations = append(recommendations, "EDR monitoring increases detection probability")
	}
	if ti.HasSIEM() {
		recommendations = append(recommendations, "Active SIEM logging creates forensic trail")
	}
	if ti.VulnerabilitySurface != nil && len(ti.VulnerabilitySurface.CVEs) > 10 {
		recommendations = append(recommendations, fmt.Sprintf("%d known vulnerabilities require patching", len(ti.VulnerabilitySurface.CVEs)))
	}
	
	return fmt.Sprintf("Risk Level: %s (Score: %.2f)\nRecommendations: %v", 
		severity, riskScore, recommendations)
}

// calculateRiskScore 计算风险评分 (内部方法)
func (ti *DeepTargetIntelligence) calculateRiskScore() float64 {
	score := 0.0
	
	// Vulnerability weight: 40%
	if ti.VulnerabilitySurface != nil {
		vulnWeight := float64(len(ti.VulnerabilitySurface.CVEs)) / 50.0
		if vulnWeight > 1.0 {
			vulnWeight = 1.0
		}
		score += vulnWeight * 0.4
	}
	
	// Defense weight: 30%
	if ti.ActiveDefense != nil {
		defenseWeight := 0.0
		count := 0
		
		for _, sys := range ti.ActiveDefense.RealTimeMonitoring {
			if sys.MonitoringEnabled {
				count++
			}
		}
		if count > 0 {
			defenseWeight = float64(count) / 5.0
			if defenseWeight > 1.0 {
				defenseWeight = 1.0
			}
		}
		
		if ti.ActiveDefense.LogAggregation.RealTimeAlerting {
			defenseWeight += 0.2
		}
		
		score += (1.0 - defenseWeight) * 0.3
	} else {
		score += 0.3 // No defense = high risk
	}
	
	// Network exposure weight: 30%
	if ti.Environment.NetworkTopology != nil {
		segmentCount := float64(len(ti.Environment.NetworkTopology.Segments))
		exposure := segmentCount / 10.0
		if exposure > 1.0 {
			exposure = 1.0
		}
		score += exposure * 0.3
	}
	
	return score
}

// CreateSampleIntel 创建示例情报数据
func CreateSampleIntel() *DeepTargetIntelligence {
	now := time.Now()
	
	intel := &DeepTargetIntelligence{
		TargetID:          "TARGET-001",
		TargetName:        "Production Database Cluster",
		ReconTimestamp:    now,
		IntelligenceLevel: "full",
		DataSources:       []string{"Nmap scan", "Vulnerability scanner", "Network topology discovery"},
		Confidence:        0.92,
	}
	
	// Setup environment profile
	intel.Environment = EnvironmentProfile{
		OperatingSystems: map[string]OSProfile{
			"Windows Server 2019": {
				Name:       "Windows Server 2019",
				Version:    "1803",
				Build:      "17763.4374",
				PatchLevel: 0.85,
				CVEs: []string{
					"CVE-2021-4034", "CVE-2022-21907", "CVE-2023-1234",
				},
				Hardening: 0.72,
			},
			"Ubuntu 20.04": {
				Name:       "Ubuntu 20.04",
				Version:    "20.04",
				Kernel:     "5.4.0-91-generic",
				PatchLevel: 0.78,
				CVEs: []string{
					"CVE-2021-4034", "CVE-2021-41559",
				},
				Hardening: 0.65,
			},
		},
		NetworkTopology: &NetworkTopology{
			Segments: []NetworkSegment{
				{
					Name:         "DMZ",
					CIDR:         "10.0.1.0/24",
					Gateway:      "10.0.1.1",
					SecurityEnv:  "dmz",
					SensitiveData: false,
				},
				{
					Name:         "Internal",
					CIDR:         "10.0.2.0/24",
					Gateway:      "10.0.2.1",
					SecurityEnv:  "internal",
					SensitiveData: true,
				},
				{
					Name:         "Restricted",
					CIDR:         "10.0.3.0/24",
					Gateway:      "10.0.3.1",
					SecurityEnv:  "restricted",
					SensitiveData: true,
				},
			},
			Firewalls: []FirewallConfig{
				{
					Model:       "Palo Alto PA-3200 series",
					Version:     "10.1.0-h10",
					RulesCount:  47,
					LastUpdated: now.AddDate(0, -2, 0), // 2 months ago
				},
			},
			Routers: []RouterConfig{
				{
					Model:     "CISCO ISR 4000 series",
					Firmware:  "16.12.4",
					Routing:   "BGP",
				},
			},
			VLANMappings: map[string]int{
				"DMZ":   10,
				"INT":   20,
				"REST":  30,
			},
		},
		SecurityProducts: &SecurityProducts{
			EDRs: []EDRConfig{
				{
					Name:         "CrowdStrike Falcon",
					Version:      "v6.29",
					Status:       "active",
					Monitoring:   true,
					Capabilities: []string{"EDR", "Threat Hunting", "Incident Response"},
				},
				{
					Name:         "SentinelOne",
					Version:      "v8.5",
					Status:       "inactive",
					Monitoring:   false,
					Capabilities: []string{"EDR"},
				},
			},
			WAFs: []WAFConfig{
				{
					Name:       "ModSecurity",
					Version:    "v3.0",
					OWASPRules: 234,
					RulesCount: 189,
				},
				{
					Name:       "Cloudflare Enterprise",
					Version:    "Enterprise",
					CloudFlare: true,
					RulesCount: 156,
				},
			},
			IPSSystems: []IPSSystem{
				{
					Name:     "Snort",
					Version:  "3.1",
					Ruleset:  "SNORT_2.9.17.0",
				},
				{
					Name:     "Suricata",
					Version:  "6.0",
					Ruleset:  "ET_Open",
				},
			},
			SIEMs: []SIEMConfig{
				{
					Name:         "Splunk Enterprise",
					Version:      "9.1",
					Indexes:      []string{"main", "auth", "network"},
					Correlation:  234,
				},
				{
					Name:         "ELK Stack",
					Version:      "7.16",
					Indexes:      []string{"logs-*"},
					Correlation:  89,
				},
			},
			LogManagement: []LogManagementSystem{
				{
					Name:       "Graylog",
					Version:    "4.4",
					Retention:  90,
					RealTime:   true,
				},
			},
		},
	}
	
	// Setup vulnerability surface
	intel.VulnerabilitySurface = &VulnerabilitySurface{
		CVEs: map[string]VulnProfile{
			"CVE-2021-4034": {
				CVE:              "CVE-2021-4034",
				CVSS:             7.8,
				Description:      "Sudo Local Privilege Escalation",
				ExploitAvailable: true,
				PublicPOC:        "https://github.com/taviso/suid",
				AffectedVersions: []string{"4.1 <= sudo < 1.9.5p1"},
				Remediation:      "Upgrade sudo to >= 1.9.5p1",
				DiscoveredAt:     time.Date(2021, 12, 10, 0, 0, 0, 0, time.UTC),
			},
			"CVE-2022-21907": {
				CVE:              "CVE-2022-21907",
				CVSS:             9.8,
				Description:      "Windows Kernel Local Privilege Escalation",
				ExploitAvailable: true,
				PublicPOC:        "https://github.com/stevemullins/kernel-exploit",
				AffectedVersions: []string{"Windows Server 2019"},
				Remediation:      "Apply MS22-086 cumulative update",
				DiscoveredAt:     time.Date(2022, 9, 13, 0, 0, 0, 0, time.UTC),
			},
		},
		AttackVectors: []AttackVector{
			{
				Type: "web-entrypoint",
				Endpoints: []WebEndpoint{
					{
						URL:         "/api/v1/users",
						Method:      "GET",
						Vulnerable:  true,
						SQLiRisk:    "high",
						XSSRisk:     "medium",
					},
					{
						URL:         "/api/v1/upload",
						Method:      "POST",
						Vulnerable:  true,
						UploadRisk:  "high",
					},
				},
			},
			{
				Type: "network-service",
				OpenPorts: []PortExposure{
					{
						Port:      22,
						Service:   "SSH",
						Version:   "OpenSSH 8.2p1",
						Vulnerable: false,
						RiskLevel: "low",
					},
					{
						Port:      80,
						Service:   "HTTP",
						Vulnerable: true,
						RiskLevel: "high",
					},
					{
						Port:      443,
						Service:   "HTTPS",
						Vulnerable: true,
						RiskLevel: "medium",
					},
				},
			},
		},
		HardeningScore: HardeningScore{
			OSHardening: 0.72,
			NetworkHardening: 0.65,
			ApplicationHardening: 0.58,
			OverallSecurityPosture: 0.65,
		},
	}
	
	// Setup active defense
	intel.ActiveDefense = &ActiveDefenseStack{
		RealTimeMonitoring: []DetectionSystem{
			{
				Name:              "CrowdStrike Falcon",
				Type:              "EDR",
				MonitoringEnabled: true,
				AlertThreshold:    "high",
				AutomatedResponse: true,
				ResponseTime:      "< 5 seconds",
				Capabilities:      []string{"EDR", "Threat Hunting", "Incident Response"},
			},
			{
				Name:              "Splunk Enterprise",
				Type:              "SIEM",
				MonitoringEnabled: true,
				AlertThreshold:    "medium",
				AutomatedResponse: false,
				ResponseTime:      "manual (< 5 minutes)",
				Capabilities:      []string{"Log Aggregation", "SIEM", "Correlation Rules"},
			},
		},
		LogAggregation: LogAggregationCapabilities{
			SIEMPlatforms:    []string{"Splunk Enterprise 9.1", "ELK Stack 7.16"},
			RetentionPeriod:  90,
			RealTimeAlerting: true,
			CorrelationRules: 234,
			DataSources:      []string{"syslog", "windows-event-log", "netflow", "proxy-logs"},
		},
		IncidentResponse: IncidentResponseCapability{
			AutomationLevel:   "partially-automated",
			SOARIntegration:   true,
			ResponsePlaybooks: 47,
			MeanTimeToRespond: "< 5 minutes",
			ContainmentStrategy: []string{"isolate-host", "terminate-processes", "block-ip"},
		},
		ComplianceRequirements: []string{"PCI-DSS", "HIPAA", "SOC2 Type II"},
	}
	
	return intel
}

// IsUnderDefense 检查是否受到有效监控
func (ti *DeepTargetIntelligence) IsUnderDefense() bool {
	if ti.ActiveDefense == nil {
		return false
	}
	
	// Check if any monitoring is active
	for _, sys := range ti.ActiveDefense.RealTimeMonitoring {
		if sys.MonitoringEnabled {
			return true
		}
	}
	
	return false
}
