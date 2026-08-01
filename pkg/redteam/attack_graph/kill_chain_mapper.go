// Package attack_graph - kill_chain_mapper implements MITRE ATT&CK to Kill Chain mapping.
// Maps CVEs and vulnerabilities to Lockheed Martin Cyber Kill Chain phases (7 stages).
package attack_graph

import (
	"fmt"
	"strings"
)

// KillChainPhase represents Lockheed Martin Cyber Kill Chain phase
type KillChainPhase string

const (
	PhaseReconnaissance      KillChainPhase = "reconnaissance"       // 1. Reconnaissance
	PhaseWeaponization       KillChainPhase = "weaponization"        // 2. Weaponization
	PhaseDelivery            KillChainPhase = "delivery"             // 3. Delivery
	PhaseExploitation        KillChainPhase = "exploitation"         // 4. Exploitation
	PhaseInstallation        KillChainPhase = "installation"         // 5. Installation
	PhaseCommandControl      KillChainPhase = "command_and_control"  // 6. Command & Control
	PhaseActionsOnObjectives KillChainPhase = "actions_on_objectives" // 7. Actions on Objectives
)

// KillChainMapping maps vulnerability characteristics to Kill Chain phases
type KillChainMapping struct {
	VulnID           string            `json:"vuln_id"`
	AttackVector     KillChainPhase    `json:"attack_vector"`
	DeliveryMethod   []KillChainPhase  `json:"delivery_methods"`
	ExploitationPath []ExploitPattern  `json:"exploit_patterns"`
	ObjectiveReached []string          `json:"objectives_reached"`
}

// ExploitPattern describes a specific exploitation technique
type ExploitPattern struct {
	PatternName string `json:"pattern_name"`
	Mitigation  string `json:"mitigation,omitempty"`
	Reference   string `json:"reference,omitempty"`
}

// KillChainMapper provides mapping logic from CVE metadata to Kill Chain
type KillChainMapper struct{}

// NewKillChainMapper creates mapper instance
func NewKillChainMapper() *KillChainMapper {
	return &KillChainMapper{}
}

// MapToKillChain determines which Kill Chain phases apply to a CVE
func (kcm *KillChainMapper) MapToKillChain(cve ImpactScore, references []Ref) *KillChainMapping {
	mapping := &KillChainMapping{
		AttackVector: determinePrimaryPhase(cve.AttackVector),
		DeliveryMethod: kcm.determineDeliveryMethods(cve),
		ExploitationPath: kcm.identifyExploitPatterns(cve),
		ObjectiveReached: kcm.detectObjectives(cve),
	}
	
	kcm.logMapping(cve.ID, mapping)
	return mapping
}

// determinePrimaryPhase determines the primary Kill Chain phase based on Attack Vector
func determinePrimaryPhase(av string) KillChainPhase {
	switch av {
	case "NETWORK":
		return PhaseReconnaissance // Network-based reconnaissance
	case "ADJACENT_NETWORK":
		return PhaseDelivery // Limited scope delivery
	case "LOCAL":
		return PhaseExploitation // Local code execution
	case "PHYSICAL":
		return PhaseInstallation // Physical access required
	default:
		return PhaseReconnaissance
	}
}

// determineDeliveryMethods identifies possible delivery methods
func (kcm *KillChainMapper) determineDeliveryMethods(cve ImpactScore) []KillChainPhase {
	deliveryMethods := make([]KillChainPhase, 0)
	
	if cve.AttackVector == "NETWORK" || cve.AttackVector == "ADJACENT_NETWORK" {
		deliveryMethods = append(deliveryMethods, PhaseDelivery)
	}
	
	if cve.AttackComplexity == "LOW" && cve.PrivilegesRequired == "NONE" {
		deliveryMethods = append(deliveryMethods, PhaseDelivery)
	}
	
	if len(deliveryMethods) == 0 {
		deliveryMethods = append(deliveryMethods, PhaseWeaponization)
	}
	
	return deliveryMethods
}

// identifyExploitPatterns identifies common exploit patterns based on CVSS
func (kcm *KillChainMapper) identifyExploitPatterns(cve ImpactScore) []ExploitPattern {
	patterns := make([]ExploitPattern, 0)
	
	// Pattern 1: Remote Code Execution
	if cve.AttackVector == "NETWORK" && 
	   cve.PrivilegesRequired == "NONE" && 
	   cve.UserInteraction == "NONE" {
		patterns = append(patterns, ExploitPattern{
			PatternName: "Remote Code Execution (RCE)",
			Mitigation: "Patch management + Network segmentation",
			Reference:  "MITRE T1190",
		})
	}
	
	// Pattern 2: Privilege Escalation
	if cve.Scope == "CHANGED" || cve.Availability == "HIGH" {
		patterns = append(patterns, ExploitPattern{
			PatternName: "Privilege Escalation",
			Mitigation: "Least privilege principle + RBAC",
			Reference:  "MITRE T1068",
		})
	}
	
	// Pattern 3: Data Exfiltration
	if cve.Confidentiality == "HIGH" && cve.Integrity == "HIGH" {
		patterns = append(patterns, ExploitPattern{
			PatternName: "Data Exfiltration",
			Mitigation: "DLP controls + Encryption at rest",
			Reference:  "MITRE T1041",
		})
	}
	
	return patterns
}

// detectObjectives detects potential attacker objectives
func (kcm *KillChainMapper) detectObjectives(cve ImpactScore) []string {
	objectives := make([]string, 0)
	
	if cve.Confidentiality == "HIGH" {
		objectives = append(objectives, "Data Theft")
	}
	
	if cve.Integrity == "HIGH" {
		objectives = append(objectives, "System Compromise")
	}
	
	if cve.Availability == "HIGH" {
		objectives = append(objectives, "Service Disruption")
	}
	
	return objectives
}

// logMapping records the mapping for audit trail
func (kcm *KillChainMapper) logMapping(vulnID string, mapping *KillChainMapping) {
	// In production, this would log to audit system
	fmt.Printf("Mapped %s to Kill Chain:\n", vulnID)
	fmt.Printf("  Primary Phase: %s\n", mapping.AttackVector)
	fmt.Printf("  Delivery Methods: %v\n", mapping.DeliveryMethod)
	fmt.Printf("  Exploit Patterns: %d detected\n", len(mapping.ExploitationPath))
	fmt.Printf("  Potential Objectives: %v\n", mapping.ObjectiveReached)
}

// GenerateKillChainGraph constructs attack graph nodes/edges from mappings
func (kcm *KillChainMapper) GenerateKillChainGraph(mappings []*KillChainMapping) map[string]interface{} {
	graph := map[string]interface{}{
		"nodes": []Node{},
		"edges": []Edge{},
	}
	
	for _, mapping := range mappings {
		// Add CVE node
		graph["nodes"] = append(graph["nodes"].(map[string]interface{}), map[string]interface{}{
			"id":      mapping.VulnID,
			"type":    "cve",
			"phase":   string(mapping.AttackVector),
			"severity": "", // Would populate from actual data
		})
		
		// Add edges to Kill Chain phases
		for _, phase := range mapping.DeliveryMethod {
			graph["edges"] = append(graph["edges"].(map[string]interface{}), map[string]interface{}{
				"source": mapping.VulnID,
				"target": string(phase),
				"type":   "DELIVERS_TO",
			})
		}
	}
	
	return graph
}

// Node represents a node in the attack graph
type Node struct {
	ID   string                 `json:"id"`
	Type string                 `json:"type"`
	Info map[string]interface{} `json:"info"`
}

// Edge represents an edge in the attack graph
type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}
