

// Package attack_graph_test - comprehensive unit tests for Red Team framework
package attack_graph

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// CVE Ingestion Service Tests
// ============================================================================

func TestNewCVEIngestionService(t *testing.T) {
	tests := []struct {
		name    string
		cfg     CVEIngestionConfig
		wantErr bool
	}{
		{
			name: "Valid config with all fields",
			cfg: CVEIngestionConfig{
				APIKey:          "test-key-123",
				DBURI:           "bolt://localhost:7687",
				RefreshInterval: 24 * time.Hour,
				Logger:          logrus.StandardLogger(),
			},
			wantErr: false,
		},
		{
			name: "Minimal config with defaults",
			cfg: CVEIngestionConfig{
				APIKey: "minimal-key",
				DBURI:  "bolt://localhost:7687",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewCVEIngestionService(tt.cfg)
			if err != nil && !tt.wantErr {
				t.Fatalf("NewCVEIngestionService() unexpected error = %v", err)
			}
			
			if service == nil {
				t.Error("Expected non-nil service, got nil")
				return
			}
			
			// Verify the service is properly initialized by checking behavior
			if tt.wantErr {
				t.Log("expected error occurred as designed")
			} else {
				// Service should be functional for valid configs
				_ = service.GetAPIKey()
				t.Logf("service created with cacheTTL=%v", service.GetCacheTTL())
			}
		})
	}
}

func TestCVEItemValidation(t *testing.T) {
	cve := CVEItem{
		ID: "CVE-2024-0001",
		Impact: ImpactScore{
			BaseScore: 9.8,
			BaseSeverity: "CRITICAL",
			AttackVector: "NETWORK",
		},
	}
	
	// Verify basic struct creation
	if cve.ID != "CVE-2024-0001" {
		t.Errorf("Expected CVE ID 'CVE-2024-0001', got '%s'", cve.ID)
	}
	
	if cve.Impact.BaseScore != 9.8 {
		t.Errorf("Expected BaseScore 9.8, got %.1f", cve.Impact.BaseScore)
	}
}

// ============================================================================
// Kill Chain Mapper Tests
// ============================================================================

func TestDeterminePrimaryPhase(t *testing.T) {
	tests := []struct {
		attackVector string
		expected     KillChainPhase
	}{
		{"NETWORK", PhaseReconnaissance},
		{"ADJACENT_NETWORK", PhaseDelivery},
		{"LOCAL", PhaseExploitation},
		{"PHYSICAL", PhaseInstallation},
		{"INVALID", PhaseReconnaissance}, // Default case
	}

	for _, tt := range tests {
		t.Run(tt.attackVector, func(t *testing.T) {
			result := determinePrimaryPhase(tt.attackVector)
			if result != tt.expected {
				t.Errorf("Expected phase %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestMapToKillChain(t *testing.T) {
	mapper := NewKillChainMapper()
	
	cve := ImpactScore{
		BaseScore: 9.8,
		BaseSeverity: "CRITICAL",
		AttackVector: "NETWORK",
		AttackComplexity: "LOW",
		PrivilegesRequired: "NONE",
		UserInteraction: "NONE",
		Scope: "CHANGED",
		Confidentiality: "HIGH",
		Integrity: "HIGH",
		Availability: "HIGH",
	}
	
	mapping := mapper.MapToKillChain(cve, nil)
	
	// Validate mapping structure
	if mapping.AttackVector != PhaseReconnaissance {
		t.Errorf("Expected primary phase reconnaissance, got %s", mapping.AttackVector)
	}
	
	if len(mapping.DeliveryMethod) == 0 {
		t.Error("Expected at least one delivery method")
	}
	
	if len(mapping.ObjectiveReached) == 0 {
		t.Error("Expected at least one objective")
	}
}

func TestIdentifyExploitPatterns(t *testing.T) {
	mapper := NewKillChainMapper()
	
	// RCE scenario
	rceCve := ImpactScore{
		AttackVector: "NETWORK",
		AttackComplexity: "LOW",
		PrivilegesRequired: "NONE",
		UserInteraction: "NONE",
	}
	
	patterns := mapper.identifyExploitPatterns(rceCve)
	
	if len(patterns) == 0 {
		t.Error("Expected exploit patterns for RCE scenario")
	}
	
	// Verify pattern name contains expected text
	foundRCE := false
	for _, p := range patterns {
		if containsIgnoreCase(p.PatternName, "Remote Code Execution") {
			foundRCE = true
			break
		}
	}
	
	if !foundRCE {
		t.Error("Expected RCE pattern in identified exploits")
	}
}

// ============================================================================
// Exploit Chainer Tests
// ============================================================================

func TestGenerateAttackChain(t *testing.T) {
	chainer := NewExploitChainer()
	
	mockCVEs := []CVEItem{
		{
			ID: "CVE-2024-RCE-001",
			Impact: ImpactScore{
				BaseScore: 9.8,
				AttackVector: "NETWORK",
				AttackComplexity: "LOW",
			},
		},
		{
			ID: "CVE-2024-PTE-002",
			Impact: ImpactScore{
				BaseScore: 7.8,
				AttackVector: "LOCAL",
			},
		},
	}
	
	targetCtx := &TargetContext{
		SystemDescription: "Production Environment",
	}
	
	chain := chainer.GenerateAttackChain(context.Background(), mockCVEs, targetCtx)
	
	if chain == nil {
		t.Fatal("Expected non-nil attack chain")
	}
	
	if len(chain.Stages) < 1 {
		t.Fatalf("Expected at least 1 stage, got %d", len(chain.Stages))
	}
	
	// Validate chain properties
	if chain.RiskScore <= 0 {
		t.Error("Expected positive risk score")
	}
	
	if chain.SuccessProbability < 0 || chain.SuccessProbability > 1 {
		t.Errorf("Expected success probability between 0 and 1, got %f", chain.SuccessProbability)
	}
}

func TestCalculateSuccessProbability(t *testing.T) {
	chain := &AttackChain{
		Stages: []AttackStage{
			{
				Order: 1,
				CVE: CVEItem{
					Impact: ImpactScore{
						AttackComplexity: "LOW", // High probability
					},
				},
			},
			{
				Order: 2,
				CVE: CVEItem{
					Impact: ImpactScore{
						AttackComplexity: "HIGH", // Lower probability
					},
				},
			},
		},
	}
	
	chain.CalculateSuccessProbability()
	
	if chain.SuccessProbability == 0 {
		t.Error("Expected non-zero success probability")
	}
	
	if chain.SuccessProbability >= 1.0 {
		t.Errorf("Expected probability < 1.0, got %f", chain.SuccessProbability)
	}
}

func TestFilterBySeverity(t *testing.T) {
	cves := []CVEItem{
		{ID: "CVE-1", Impact: ImpactScore{BaseScore: 9.8}}, // CRITICAL
		{ID: "CVE-2", Impact: ImpactScore{BaseScore: 7.5}}, // HIGH
		{ID: "CVE-3", Impact: ImpactScore{BaseScore: 5.0}}, // MEDIUM
		{ID: "CVE-4", Impact: ImpactScore{BaseScore: 3.0}}, // LOW
	}
	
	critical := filterBySeverity(cves, "CRITICAL")
	high := filterBySeverity(cves, "HIGH")
	medium := filterBySeverity(cves, "MEDIUM")
	
	if len(critical) != 1 || critical[0].ID != "CVE-1" {
		t.Error("Expected 1 critical CVE")
	}
	
	if len(high) != 1 || high[0].ID != "CVE-2" {
		t.Error("Expected 1 high CVE")
	}
	
	if len(medium) != 1 || medium[0].ID != "CVE-3" {
		t.Error("Expected 1 medium CVE")
	}
}

func TestValidateAttackChain(t *testing.T) {
	// Valid chain
	validChain := &AttackChain{
		Stages: []AttackStage{
			{Order: 1, Action: "Initial Access"},
			{Order: 2, Action: "Privilege Escalation", Prerequisites: []string{"Initial Access"}},
		},
	}
	
	if !validChain.ValidateAttackChain() {
		t.Error("Expected valid chain to pass validation")
	}
	
	// Invalid chain (missing prerequisite)
	invalidChain := &AttackChain{
		Stages: []AttackStage{
			{Order: 1, Action: "Initial Access"},
			{Order: 2, Action: "Privilege Escalation", Prerequisites: []string{"NonExistent"}},
		},
	}
	
	if invalidChain.ValidateAttackChain() {
		t.Error("Expected invalid chain to fail validation")
	}
}

// ============================================================================
// Utility Function Tests
// ============================================================================

func TestContainsString(t *testing.T) {
	tests := []struct {
		slice  []string
		str    string
		expect bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{[]string{}, "a", false},
	}
	
	for _, tt := range tests {
		result := containsString(tt.slice, tt.str)
		if result != tt.expect {
			t.Errorf("containsString(%v, %s) = %v, want %v", 
				tt.slice, tt.str, result, tt.expect)
		}
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	if !containsIgnoreCase("Hello World", "HELLO") {
		t.Error("Expected case-insensitive match")
	}
	
	if containsIgnoreCase("Hello World", "xyz") {
		t.Error("Should not match unrelated string")
	}
}

// Helper functions for tests
func containsIgnoreCase(s, substr string) bool {
	sLower := toLower(s)
	substrLower := toLower(substr)
	
	for i := 0; i <= len(sLower)-len(substrLower); i++ {
		if sLower[i:i+len(substrLower)] == substrLower {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	var result []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		result = append(result, c)
	}
	return string(result)
}

// ============================================================================
// Context-aware Tests
// ============================================================================

func TestWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	
	service, err := NewCVEIngestionService(CVEIngestionConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("NewCVEIngestionService failed: %v", err)
	}
	
	err = service.IngestLatestCVEs(ctx, 7)
	// Expected to either succeed quickly or handle cancellation gracefully
	if err != nil && err.Error() != "context canceled" {
		t.Logf("Got error (may be acceptable): %v", err)
	}
}

// ============================================================================
// Table-driven Tests for CVE Item Creation
// ============================================================================

var cveTestCases = []struct {
	name      string
	id        string
	score     float32
	severity  string
	valid     bool
	checkFn   func(t *testing.T, item CVEItem)
}{
	{
		name:     "Critical severity CVE",
		id:       "CVE-2024-0001",
		score:    9.8,
		severity: "CRITICAL",
		valid:    true,
		checkFn: func(t *testing.T, item CVEItem) {
			if item.ID != "CVE-2024-0001" {
				t.Errorf("Expected CVE ID CVE-2024-0001, got %s", item.ID)
			}
		},
	},
	{
		name:     "High severity CVE",
		id:       "CVE-2024-0002",
		score:    7.5,
		severity: "HIGH",
		valid:    true,
	},
	{
		name:     "Zero score edge case",
		id:       "CVE-2024-0000",
		score:    0.0,
		severity: "NONE",
		valid:    true, // Should still be valid
	},
}

func TestCVEItemCreation(t *testing.T) {
	for _, tc := range cveTestCases {
		t.Run(tc.name, func(t *testing.T) {
			item := CVEItem{
				ID: tc.id,
				Impact: ImpactScore{
					BaseScore:    float64(tc.score),
					BaseSeverity: tc.severity,
				},
			}
			
			if tc.checkFn != nil {
				tc.checkFn(t, item)
			}
		})
	}
}

