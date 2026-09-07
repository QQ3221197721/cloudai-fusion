package patent

import (
	"fmt"
	"math"
	"math/rand"
	"time"
	
	"github.com/sirupsen/logrus"
)

// Vulnerability represents CVE characteristics and exploit properties
type Vulnerability struct {
	ID                  string    `json:"cve_id"`
	Description         string    `json:"description"`
	CVSSBaseScore       float64   `json:"cvss_base_score"` // 0-10 scale
	AttackVector        string    `json:"attack_vector"`   // "NETWORK", "ADJACENT", "LOCAL"
	AttackComplexity    string    `json:"attack_complexity"` // "LOW", "HIGH"
	PrivilegeRequired   string    `json:"privilege_required"` // "NONE", "LOW", "HIGH"
	DisclosureImpact    string    `json:"disclosure_impact"` // impacts on confidentiality/integrity/availability
	ExploitSuccessRate  float64   `json:"exploit_success_rate"` // Base probability of successful exploitation (0-1)
	CovertExecution     bool      `json:"covert_execution"` // Can execute without triggering alerts
	ImpactType          string    `json:"impact_type"`     // "privilege_escalation", "lateral_movement", "persistence"
	TargetPlatform      string    `json:"target_platform"` // "windows", "linux", "macos"
	LastUpdated         time.Time `json:"last_updated"`
}

// DefenseMechanism represents security control that detects/mitigates attacks
type DefenseMechanism struct {
	Name           string            `json:"name"`           // e.g., "EDR实时监控", "AMSI", "AppLocker"
	Type           string            `json:"type"`           // "behavioral", "signature", "heuristic"
	IsActive       bool              `json:"is_active"`      // Whether defense is currently enabled
	DetectionRate  float64           `json:"detection_rate"` // Probability of detecting attack (0-1)
	MitigationRate float64           `json:"mitigation_rate"`// Probability of blocking attack if detected
	TriggerConditions []string         `json:"trigger_conditions"` // Conditions that activate detection
	Latency        time.Duration     `json:"latency"`        // Detection latency (how fast it responds)
}

// AttackSimulator models real-world attack transitions with probabilistic outcomes
// This encapsulates domain knowledge about exploit behavior and defense mechanisms
type AttackSimulator struct {
	logger *logrus.Logger
	
	// Vulnerability database (CVE → exploit characteristics)
	vulnDB map[string]Vulnerability
	
	// Defense mechanism simulations (EDR, AMSI, AppLocker, etc.)
	defenses map[string]DefenseMechanism
	
	// Mitigation effectiveness matrix (which defenses block which vulnerabilities)
	mitigationMatrix map[string][]string // vuln_id -> defense_names that mitigate it
	
	// Statistical profiles for different attack categories
	attackProfiles map[string]AttackProfile
	
	mu interface{} // Thread safety placeholder
}

// AttackProfile contains statistical parameters for different attack types
type AttackProfile struct {
	BaseSuccessRate     float64 // Baseline exploit success rate
	StealthDegradation  float64 // Typical stealth penalty when exploited
	DetectionProbability float64 // Likelihood of triggering defense
}

// NewAttackSimulator creates simulator with default vulnerability database and active defenses
func NewAttackSimulator(logger *logrus.Logger) *AttackSimulator {
	as := &AttackSimulator{
		logger:         logger,
		vulnDB:         make(map[string]Vulnerability),
		defenses:       make(map[string]DefenseMechanism),
		mitigationMatrix: make(map[string][]string),
		attackProfiles: make(map[string]AttackProfile),
	}
	
	// Initialize with comprehensive CVE database
	as.loadCVEDatabase()
	
	// Initialize active defense mechanisms
	as.loadActiveDefenses()
	
	// Build mitigation effectiveness matrix
	as.buildMitigationMatrix()
	
	return as
}

// loadCVEDatabase populates internal database with CVE characteristics
// In production, this would be loaded from external sources like NVD API
func (as *AttackSimulator) loadCVEDatabase() {
	// Example CVEs with realistic exploit characteristics
	
	cves := []Vulnerability{
		{
			ID:               "CVE-2021-4034", // PwnKit - Local Privilege Escalation
			Description:      "Local privilege escalation via pkexec",
			CVSSBaseScore:    7.8,
			AttackVector:     "LOCAL",
			AttackComplexity: "LOW",
			PrivilegeRequired: "NONE",
			DisclosureImpact: "HIGH",
			ExploitSuccessRate: 0.85, // High success rate given low complexity
			CovertExecution: false, // Typically generates logs/alerts
			ImpactType:       "privilege_escalation",
			TargetPlatform:   "linux",
			LastUpdated:      time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC),
		},
		
		{
			ID:               "CVE-2023-34361", // Metabase Authentication Bypass
			Description:      "Authentication bypass in Metabase analytics platform",
			CVSSBaseScore:    9.8,
			AttackVector:     "NETWORK",
			AttackComplexity: "LOW",
			PrivilegeRequired: "NONE",
			DisclosureImpact: "HIGH",
			ExploitSuccessRate: 0.95, // Almost guaranteed success
			CovertExecution: true,  // Stealthy authentication bypass
			ImpactType:       "initial_access",
			TargetPlatform:   "java",
			LastUpdated:      time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC),
		},
		
		{
			ID:               "CVE-2022-22965", // Spring4Shell RCE
			Description:      "Remote code execution in Spring Framework",
			CVSSBaseScore:    9.8,
			AttackVector:     "NETWORK",
			AttackComplexity: "LOW",
			PrivilegeRequired: "NONE",
			DisclosureImpact: "HIGH",
			ExploitSuccessRate: 0.75, // High but depends on specific configuration
			CovertExecution: true,
			ImpactType:       "remote_code_execution",
			TargetPlatform:   "java",
			LastUpdated:      time.Date(2022, 3, 30, 0, 0, 0, 0, time.UTC),
		},
		
		{
			ID:               "CVE-2020-1472",  // Zerologon - NetLogon Protocol Exploit
			Description:      "NetLogon protocol elevation of privilege",
			CVSSBaseScore:    10.0,
			AttackVector:     "NETWORK",
			AttackComplexity: "LOW",
			PrivilegeRequired: "NONE",
			DisclosureImpact: "CRITICAL",
			ExploitSuccessRate: 0.98, // Near-given success
			CovertExecution: false, // Generates significant logs
			ImpactType:       "privilege_escalation",
			TargetPlatform:   "windows",
			LastUpdated:      time.Date(2020, 9, 10, 0, 0, 0, 0, time.UTC),
		},
	}
	
	for _, cve := range cves {
		as.vulnDB[cve.ID] = cve
		
		// Define attack profiles based on impact type
		profile := AttackProfile{
			BaseSuccessRate:     cve.ExploitSuccessRate,
			StealthDegradation:  0.15, // Default stealth penalty
			DetectionProbability: 0.25, // Default detection chance
		}
		
		if !cve.CovertExecution {
			profile.StealthDegradation = 0.3 // Higher penalty for non-stealthy exploits
			profile.DetectionProbability = 0.5 // More likely to trigger detection
		}
		
		as.attackProfiles[cve.ImpactType] = profile
	}
}

// loadActiveDefenses initializes security controls that are typically running
func (as *AttackSimulator) loadActiveDefenses() {
	// Common enterprise defenses
	
	defenses := []DefenseMechanism{
		{
			Name:           "EDR实时行为监控",
			Type:           "behavioral",
			IsActive:       true,
			DetectionRate:  0.75, // Detects 75% of suspicious behaviors
			MitigationRate: 0.60, // Blocks 60% of detected attempts
			TriggerConditions: []string{"process_injection", "credential_dumping", "power_shell_abuse"},
			Latency:        50 * time.Millisecond, // Sub-second response
		},
		
		{
			Name:           "AMSI脚本扫描引擎",
			Type:           "heuristic",
			IsActive:       true,
			DetectionRate:  0.65,
			MitigationRate: 0.70,
			TriggerConditions: []string{"powershell_scripting", "javascript_obfuscation"},
			Latency:        20 * time.Millisecond, // Fast scan
		},
		
		{
			Name:           "AppLocker应用程序白名单",
			Type:           "signature",
			IsActive:       true,
			DetectionRate:  0.90, // High accuracy but only catches known patterns
			MitigationRate: 0.95, // Nearly always blocks if triggered
			TriggerConditions: []string{"unsigned_binary_execution", "suspicious_dll_loading"},
			Latency:        5 * time.Millisecond, // Instant decision
		},
		
		{
			Name:           "SIEM日志关联分析",
			Type:           "behavioral",
			IsActive:       true,
			DetectionRate:  0.50, // Lower immediate detection but catches patterns
			MitigationRate: 0.30, // Alerts rather than blocks
			TriggerConditions: []string{"failed_login_spike", "unusual_time_access", "bulk_data_access"},
			Latency:        5 * time.Second, // Delayed correlation
		},
	}
	
	for _, def := range defenses {
		as.defenses[def.Name] = def
	}
}

// buildMitigationMatrix maps vulnerabilities to defensive controls that can mitigate them
// This represents domain expertise about which defenses work against which attack vectors
func (as *AttackSimulator) buildMitigationMatrix() {
	// Example mappings based on attack vector and complexity
	
	// EDR is effective against behavioral exploits
	as.mitigationMatrix["CVE-2021-4034"] = []string{"EDR 实时行为监控"}
	
	// AMSI catches scripting-based attacks
	as.mitigationMatrix["CVE-2023-34361"] = []string{"AMSI 脚本扫描引擎"}
	
	// AppLocker prevents unauthorized binary execution
	as.mitigationMatrix["CVE-2022-22965"] = []string{"AppLocker 应用程序白名单", "EDR 实时行为监控"}
	
	// Zerologon is network-based and harder to detect in transit
	as.mitigationMatrix["CVE-2020-1472"] = []string{"SIEM 日志关联分析"}
}

// simulateAttackTransition executes one attack action in simulated environment
// Returns next state and immediate reward based on exploit outcome
func (as *AttackSimulator) simulateAttackTransition(currentState State, action Action) (StateID, float64) {
	// Apply action effects based on vulnerability/exploit characteristics
	if vuln, exists := as.vulnDB[action.TargetCVE]; exists {
		// Execute exploit with probabilistic success
		success := as.attemptExploit(vuln, action, currentState)
		
		if success {
			// Update state based on action type
			stateCopy := currentState
			
			// Progress: privilege escalation or lateral movement achieved
			if action.Type == "escalate" || action.Type == "inject_payload" {
				stateCopy.PrivilegeLevel = min(stateCopy.PrivilegeLevel+1, MaxPrivilegeLevel)
			}
			
			if action.Type == "pivot" {
				stateCopy.NetworkPosition = fmt.Sprintf("lateral_%d", time.Now().UnixNano())
			}
			
			// Stealth considerations based on exploit characteristics
			if vuln.CovertExecution {
				stateCopy.StealthScore = math.Min(1.0, stateCopy.StealthScore+0.1)
			} else {
				stateCopy.StealthScore = math.Max(0.0, stateCopy.StealthScore-0.2)
			}
			
			// Check for detection by active defenses
			if as.isDetected(stateCopy, action) {
				stateCopy.UnderDetection = true
				as.logger.WithFields(logrus.Fields{
					"cve":          action.TargetCVE,
					"action":       action.Type,
					"current_priv": stateCopy.PrivilegeLevel,
				}).Warn("Attack detected by defense mechanism")
			}
			
			reward := CalculateReward(currentState, stateCopy, action)
			return EncodeState(stateCopy), reward
		}
	}
	
	// Failed exploit attempt triggers alerts
	stateCopy := currentState
	stateCopy.UnderDetection = true // Failed attempts trigger alerts
	stateCopy.StealthScore = math.Max(0.0, stateCopy.StealthScore-0.3)
	
	as.logger.WithFields(logrus.Fields{
		"cve":      action.TargetCVE,
		"action":   action.Type,
		"stealth":  stateCopy.StealthScore,
	}).Warn("Exploit failed, triggered detection")
	
	// Penalty for failed attacks (negative reward signals poor policy choice)
	return EncodeState(stateCopy), -0.5
}

// isDetected checks if attack triggers any active defense mechanisms
// Returns true if ANY defense catches the attack
func (as *AttackSimulator) isDetected(state State, action Action) bool {
	for name, defense := range as.defenses {
		if !defense.IsActive {
			continue // Skip inactive defenses
		}
		
		// Check if attack matches defense's trigger conditions
		if defense.Catches(action, state) {
			as.logger.WithFields(logrus.Fields{
				"defense":    name,
				"cve":        action.TargetCVE,
				"detection_rate": defense.DetectionRate,
			}).Debug("Defense mechanism evaluated")
			
			// Determined detection based on defense's rate
			if rand.Float64() < defense.DetectionRate {
				return true
			}
		}
	}
	
	return false
}

// attemptExploit simulates specific exploit execution with probabilistic success
// Considers defensive controls and environmental factors
func (as *AttackSimulator) attemptExploit(vuln Vulnerability, action Action, 
	currentState State) bool {
	
	baseSuccessRate := vuln.ExploitSuccessRate
	as.logger.WithFields(logrus.Fields{
		"cve":                    vuln.ID,
		"base_success_rate":      baseSuccessRate,
		"attack_complexity":      vuln.AttackComplexity,
		"privilege_required":     vuln.PrivilegeRequired,
	}).Trace("Evaluating exploit success probability")
	
	// Adjust for defensive controls
	mitigationFactor := 1.0
	
	// Factor 1: Already have required access level (already compromised)
	requiredPrivLevel := as.parsePrivilegeRequirement(vuln.PrivilegeRequired)
	if currentState.PrivilegeLevel >= requiredPrivLevel {
		mitigationFactor -= 0.3 // Already have required access
	}
	
	// Factor 2: Stealth maintained reduces visibility to defenders
	if !currentState.UnderDetection && currentState.StealthScore > 0.5 {
		mitigationFactor -= 0.2
	}
	
	// Factor 3: Defense mitigation against this specific vulnerability
	mitigatingDefenses := as.mitigationMatrix[vuln.ID]
	for _, defName := range mitigatingDefenses {
		if defense, exists := as.defenses[defName]; exists && defense.IsActive {
			mitigationFactor -= defense.MitigationRate * 0.5 // Partial mitigation
		}
	}
	
	finalSuccessRate := baseSuccessRate * mitigationFactor
	finalSuccessRate = math.Max(0.0, math.Min(1.0, finalSuccessRate)) // Clamp [0,1]
	
	as.logger.WithFields(logrus.Fields{
		"cve":                  vuln.ID,
		"mitigation_factor":    mitigationFactor,
		"final_success_rate":   finalSuccessRate,
	}).Debug("Computing final exploit success rate")
	
	// Randomized outcome (simulates real-world unpredictability)
	return rand.Float64() < finalSuccessRate
}

// parsePrivilegeRequirement converts string privilege level to integer
func (as *AttackSimulator) parsePrivilegeRequirement(privReq string) int {
	switch privReq {
	case "HIGH":
		return 2 // Need system-level access
	case "LOW":
		return 1 // Need local admin
	default:
		return 0 // No privileges needed
	}
}

// Catches checks if this defense mechanism catches a specific attack
func (dm *DefenseMechanism) Catches(action Action, state State) bool {
	for _, condition := range dm.TriggerConditions {
		switch condition {
		case "process_injection":
			if action.Type == "inject_payload" {
				return true
			}
		case "credential_dumping":
			// Check if targeting credential-related CVEs
			if action.TargetCVE == "CVE-2020-1472" {
				return true
			}
		case "power_shell_abuse":
			if action.Type == "inject_payload" && state.PrivilegeLevel >= 1 {
				return true
			}
		case "javascript_obfuscation":
			if action.Type == "pivot" {
				return true
			}
		case "unsigned_binary_execution":
			if action.Type == "escalate" {
				return true
			}
		case "suspicious_dll_loading":
			if action.Type == "persist" {
				return true
			}
		case "failed_login_spike":
			if state.UnderDetection && rand.Float64() < 0.3 {
				return true // Some chance of triggering after failure
			}
		case "unusual_time_access":
			if state.NetworkPosition != "initial_user" && time.Now().Hour() > 22 {
				return true // Late-night lateral movement is suspicious
			}
		case "bulk_data_access":
			if len(state.ActiveCVEs) > 3 && state.PrivilegeLevel >= 2 {
				return true // Aggressive exploitation triggers alerts
			}
		}
	}
	
	return false
}

// simulateAttackTransition is a wrapper for use in cex3_self_evolution.go
// This function bridges between the Q-learning agent and the attack simulator
func simulateAttackTransition(currentState State, action Action) (StateID, float64) {
	logger := logrus.New()
	simulator := NewAttackSimulator(logger)
	
	// Execute simulation
	nextState, reward := simulator.simulateAttackTransition(currentState, action)
	
	return nextState, reward
}
