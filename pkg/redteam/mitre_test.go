// Package redteam - Unit tests for MITRE ATT&CK coverage
package redteam

import (
	"testing"
)

// ============================================================================
// INITIAL ACCESS TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreInitAccess_Phishing(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add phishing technique
	t1566 := &Technique{
		ID:          "T1566",
		Name:        "Phishing",
		Tactic:      "Initial Access",
		Description: "Email-based attacks",
	}
	
	m.addTechnique(t1566)
	
	// Verify technique added correctly
	found, exists := m.GetTechniqueByID("T1566")
	if !exists {
		t.Fatal("Phishing technique should exist")
	}
	
	if found.ID != "T1566" || found.Name != "Phishing" {
		t.Errorf("Technique mismatch: got %s/%s", found.ID, found.Name)
	}
}

func TestMitreInitAccess_DriveBy(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add drive-by compromise technique
	t1189 := &Technique{
		ID:          "T1189",
		Name:        "Drive-by Compromise",
		Tactic:      "Initial Access",
		Description: "Web exploitation",
	}
	
	m.addTechnique(t1189)
	
	found, exists := m.GetTechniqueByID("T1189")
	if !exists {
		t.Fatal("Drive-by technique should exist")
	}
	
	if found.Tactic != "Initial Access" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// EXECUTION TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreExecution_CommandInterpreter(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add command interpreter technique with subtechniques
	t1059 := &Technique{
		ID:          "T1059",
		Name:        "Command and Scripting Interpreter",
		Tactic:      "Execution",
		Subtechniques: []string{"T1059.001 (PowerShell)", "T1059.004 (Python)"},
	}
	
	m.addTechnique(t1059)
	
	found, exists := m.GetTechniqueByID("T1059")
	if !exists {
		t.Fatal("Command interpreter technique should exist")
	}
	
	if len(found.Subtechniques) != 2 {
		t.Errorf("Expected 2 subtechniques, got %d", len(found.Subtechniques))
	}
}

// ============================================================================
// PERSISTENCE TECHNIQUE TESTS ✅
// ============================================================================

func TestMitrePersistence_RegistryRunKeys(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add registry run keys persistence technique
	t1547 := &Technique{
		ID:          "T1547.001",
		Name:        "Registry Run Keys / Startup Folder",
		Tactic:      "Persistence",
	}
	
	m.addTechnique(t1547)
	
	found, exists := m.GetTechniqueByID("T1547.001")
	if !exists {
		t.Fatal("Registry persistence technique should exist")
	}
	
	if found.Tactic != "Persistence" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

func TestMitrePersistence_Cron(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add cron persistence technique
	t1053 := &Technique{
		ID:          "T1053.005",
		Name:        "Cron",
		Tactic:      "Persistence",
	}
	
	m.addTechnique(t1053)
	
	_, exists := m.GetTechniqueByID("T1053.005")
	if !exists {
		t.Fatal("Cron persistence technique should exist")
	}
}

// ============================================================================
// PRIVILEGE ESCALATION TECHNIQUE TESTS ✅
// ============================================================================

func TestMitrePrivEsc_Exploitation(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add privilege escalation technique
	t1068 := &Technique{
		ID:          "T1068",
		Name:        "Exploitation for Privilege Escalation",
		Tactic:      "Privilege Escalation",
	}
	
	m.addTechnique(t1068)
	
	found, exists := m.GetTechniqueByID("T1068")
	if !exists {
		t.Fatal("Privilege escalation technique should exist")
	}
	
	if found.Tactic != "Privilege Escalation" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// CREDENTIAL ACCESS TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreCredentialAccess_BruteForce(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add brute force credential access technique
	t1110 := &Technique{
		ID:          "T1110",
		Name:        "Brute Force",
		Tactic:      "Credential Access",
	}
	
	m.addTechnique(t1110)
	
	found, exists := m.GetTechniqueByID("T1110")
	if !exists {
		t.Fatal("Brute force technique should exist")
	}
	
	if found.Tactic != "Credential Access" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// DEFENSE EVASION TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreDefenseEvasion_Obfuscation(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add file obfuscation defense evasion technique
	t1027 := &Technique{
		ID:          "T1027",
		Name:        "Obfuscated Files or Information",
		Tactic:      "Defense Evasion",
	}
	
	m.addTechnique(t1027)
	
	found, exists := m.GetTechniqueByID("T1027")
	if !exists {
		t.Fatal("Obfuscation technique should exist")
	}
	
	if found.Tactic != "Defense Evasion" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// DISCOVERY TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreDiscovery_SystemInfo(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add system information discovery technique
	t1082 := &Technique{
		ID:          "T1082",
		Name:        "System Information Discovery",
		Tactic:      "Discovery",
	}
	
	m.addTechnique(t1082)
	
	found, exists := m.GetTechniqueByID("T1082")
	if !exists {
		t.Fatal("System info technique should exist")
	}
	
	if found.Tactic != "Discovery" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// LATERAL MOVEMENT TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreLateralMovement_RDP(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add RDP lateral movement technique
	t1021 := &Technique{
		ID:          "T1021.001",
		Name:        "Remote Desktop Protocol",
		Tactic:      "Lateral Movement",
	}
	
	m.addTechnique(t1021)
	
	found, exists := m.GetTechniqueByID("T1021.001")
	if !exists {
		t.Fatal("RDP technique should exist")
	}
	
	if found.Tactic != "Lateral Movement" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// COLLECTION TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreCollection_ScreenCapture(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add screen capture collection technique
	t1113 := &Technique{
		ID:          "T1113",
		Name:        "Screen Capture",
		Tactic:      "Collection",
	}
	
	m.addTechnique(t1113)
	
	found, exists := m.GetTechniqueByID("T1113")
	if !exists {
		t.Fatal("Screen capture technique should exist")
	}
	
	if found.Tactic != "Collection" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// COMMAND AND CONTROL TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreCommandAndControl_WebProtocol(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add web protocol C2 technique
	t1071 := &Technique{
		ID:          "T1071.001",
		Name:        "Web Protocols",
		Tactic:      "Command and Control",
	}
	
	m.addTechnique(t1071)
	
	found, exists := m.GetTechniqueByID("T1071.001")
	if !exists {
		t.Fatal("Web protocol technique should exist")
	}
	
	if found.Tactic != "Command and Control" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// EXFILTRATION TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreExfiltration_C2Channel(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add exfiltration over C2 channel technique
	t1041 := &Technique{
		ID:          "T1041",
		Name:        "Exfiltration Over C2 Channel",
		Tactic:      "Exfiltration",
	}
	
	m.addTechnique(t1041)
	
	found, exists := m.GetTechniqueByID("T1041")
	if !exists {
		t.Fatal("Exfil technique should exist")
	}
	
	if found.Tactic != "Exfiltration" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// IMPACT TECHNIQUE TESTS ✅
// ============================================================================

func TestMitreImpact_DataDestruction(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add data destruction impact technique
	t1485 := &Technique{
		ID:          "T1485",
		Name:        "Data Destruction",
		Tactic:      "Impact",
	}
	
	m.addTechnique(t1485)
	
	found, exists := m.GetTechniqueByID("T1485")
	if !exists {
		t.Fatal("Data destruction technique should exist")
	}
	
	if found.Tactic != "Impact" {
		t.Errorf("Wrong tactic: %s", found.Tactic)
	}
}

// ============================================================================
// TACTIC-BASED RETRIEVAL TESTS ✅
// ============================================================================

func TestGetTechniquesByTactic_InitialAccess(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add multiple techniques to Initial Access tactic
	for _, id := range []string{"T1566", "T1189", "T1190"} {
		m.addTechnique(&Technique{ID: id, Name: "Test", Tactic: "Initial Access"})
	}
	
	techniques := m.GetTechniquesByTactic("Initial Access")
	
	if len(techniques) != 3 {
		t.Errorf("Expected 3 initial access techniques, got %d", len(techniques))
	}
	
	// Verify all are indeed Initial Access
	for _, tech := range techniques {
		if tech.Tactic != "Initial Access" {
			t.Errorf("Technique %s has wrong tactic: %s", tech.ID, tech.Tactic)
		}
	}
}

func TestGetTechniquesByTactic_EmptyResult(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Try to get technique from non-existent tactic
	techniques := m.GetTechniquesByTactic("NonExistentTactic")
	
	if len(techniques) != 0 {
		t.Errorf("Expected 0 techniques for non-existent tactic, got %d", len(techniques))
	}
}

// ============================================================================
// COVERAGE CALCULATION TESTS ✅
// ============================================================================

func TestCalculateCoverage_EmptyDatabase(t *testing.T) {
	m := NewMITREATTandCK(nil)
	m.calculateCoverage()
	
	expectedPercent := 0.0
	if m.coveragePercent != expectedPercent {
		t.Errorf("Expected 0%% coverage for empty database, got %.1f%%", m.coveragePercent)
	}
}

func TestCalculateCoverage_WithTechniques(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add sample technique
	m.addTechnique(&Technique{ID: "T1234", Name: "Test", Tactic: "Initial Access"})
	
	m.calculateCoverage()
	
	if m.coveragePercent == 0 {
		t.Error("Coverage should be > 0 after adding technique")
	} else if m.coveragePercent < 0.1 {
		t.Logf("Coverage %.3f%% seems low but acceptable", m.coveragePercent)
	}
}

// ============================================================================
// HELPER FUNCTIONS ✅
// ============================================================================

// countTechniquesByTactic helps count techniques per tactic
func countTechniquesByTactic(m *MITREATTandCK) map[string]int {
	counts := make(map[string]int)
	
	for _, tactic := range []string{"Initial Access", "Execution", "Persistence", "Privilege Escalation", 
		"Credential Access", "Defense Evasion", "Discovery", "Lateral Movement", 
		"Collection", "Command and Control", "Exfiltration", "Impact"} {
		
		counts[tactic] = len(m.GetTechniquesByTactic(tactic))
	}
	
	return counts
}

func TestCountTechniquesByTactic(t *testing.T) {
	m := NewMITREATTandCK(nil)
	
	// Add techniques across different tactics
	m.addTechnique(&Technique{ID: "T1566", Tactic: "Initial Access"})
	m.addTechnique(&Technique{ID: "T1059", Tactic: "Execution"})
	m.addTechnique(&Technique{ID: "T1547", Tactic: "Persistence"})
	m.addTechnique(&Technique{ID: "T1068", Tactic: "Privilege Escalation"})
	m.addTechnique(&Technique{ID: "T1110", Tactic: "Credential Access"})
	m.addTechnique(&Technique{ID: "T1027", Tactic: "Defense Evasion"})
	
	counts := countTechniquesByTactic(m)
	
	// Verify counts match additions
	if counts["Initial Access"] != 1 {
		t.Errorf("Expected 1 initial access technique, got %d", counts["Initial Access"])
	}
	if counts["Execution"] != 1 {
		t.Errorf("Expected 1 execution technique, got %d", counts["Execution"])
	}
	if counts["Persistence"] != 1 {
		t.Errorf("Expected 1 persistence technique, got %d", counts["Persistence"])
	}
	if counts["Privilege Escalation"] != 1 {
		t.Errorf("Expected 1 priv esc technique, got %d", counts["Privilege Escalation"])
	}
	if counts["Credential Access"] != 1 {
		t.Errorf("Expected 1 credential access technique, got %d", counts["Credential Access"])
	}
	if counts["Defense Evasion"] != 1 {
		t.Errorf("Expected 1 defense evasion technique, got %d", counts["Defense Evasion"])
	}
	
	t.Logf("Technique distribution: %+v", counts)
}
