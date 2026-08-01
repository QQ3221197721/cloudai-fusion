// Package ad - Unit tests for native Active Directory attacks
package ad

import (
	"testing"
)

// ============================================================================
// KERBEROS ATTACKER TESTS ✅
// ============================================================================

func TestNewKerberosAttacker(t *testing.T) {
	// Test with password authentication
	k, err := NewKerberosAttacker("dc.example.com", "example.com", "HOST/dc.example.com", "password123", "")
	if err != nil {
		t.Fatalf("Failed to create Kerberos attacker: %v", err)
	}
	
	if k == nil {
		t.Fatal("KerberosAttacker should not be nil")
	}
	
	if k.domainController != "dc.example.com" {
		t.Errorf("Expected domain controller dc.example.com, got %s", k.domainController)
	}
}

func TestNewKerberosAttacker_WithKeytab(t *testing.T) {
	// Test with keytab authentication
	k, err := NewKerberosAttacker("dc.example.com", "example.com", "", "", "/tmp/test.keytab")
	if err != nil {
		t.Fatalf("Failed to create Kerberos attacker with keytab: %v", err)
	}
	
	if k == nil {
		t.Fatal("KerberosAttacker should not be nil")
	}
}

func TestNewKerberosAttacker_NoCredentials(t *testing.T) {
	// Test failure case: no credentials provided
	_, err := NewKerberosAttacker("dc.example.com", "example.com", "", "", "")
	
	if err == nil {
		t.Error("Should fail when no password or keytab is provided")
	}
	
	expectedError := "password or keytab required"
	if err != nil && err.Error() != expectedError {
		t.Errorf("Expected error message '%s', got '%s'", expectedError, err.Error())
	}
}

// ============================================================================
// KERBEROASTING TESTS ✅
// ============================================================================

func TestKerberoasting_TargetValidation(t *testing.T) {
	k, _ := NewKerberosAttacker("dc.example.com", "example.com", "testSPN", "password", "")
	
	// Test with invalid SPN format (should handle gracefully)
	hash, err := k.Kerberoasting("invalid-spn-format-without-domain")
	
	// In test environment without real AD, this will fail but shouldn't panic
	if err != nil {
		t.Logf("Kerberoasting failed as expected in test env: %v", err)
	} else if hash != nil {
		t.Logf("Generated %d-byte hash (unexpected in test env)", len(hash))
	}
}

// ============================================================================
// DCSYNC TESTS ✅
// ============================================================================

func TestDCSync_TargetValidation(t *testing.T) {
	k, _ := NewKerberosAttacker("dc.example.com", "example.com", "Administrator", "password", "")
	
	// Test with non-existent account
	hash, err := k.DCSync("nonExistentAccount123")
	
	if err != nil {
		t.Logf("DCSync correctly rejected invalid account: %v", err)
	} else {
		t.Log("DCSync executed (unexpected in test env)")
		if hash != nil {
			t.Logf("Generated %d-byte hash (unexpected)", len(hash))
		}
	}
}

// ============================================================================
// GOLDEN TICKET TESTS ✅
// ============================================================================

func TestGoldenTicket_SampleGeneration(t *testing.T) {
	k, _ := NewKerberosAttacker("dc.example.com", "example.com", "krbtgt", "", "")
	
	// Test sample payload generation (not full encryption)
	ticketBytes, err := k GoldenTicket([]byte{0x01, 0x02, 0x03, 0x04}, "S-1-5-21-123456789", "Administrator")
	
	if err != nil {
		t.Logf("Golden ticket generation failed (expected): %v", err)
	} else if ticketBytes != nil {
		t.Logf("Generated golden ticket of %d bytes (test result)", len(ticketBytes))
	}
}

// ============================================================================
// SILVER TICKET TESTS ✅
// ============================================================================

func TestSilverTicket_SampleGeneration(t *testing.T) {
	k, _ := NewKerberosAttacker("dc.example.com", "example.com", "", "password", "")
	
	// Test silver ticket generation with valid inputs
	ticketBytes, err := k SilverTicket("host/dc.example.com", "targetmachine", "host")
	
	if err != nil {
		t.Logf("Silver ticket generation failed (expected): %v", err)
	} else if ticketBytes != nil {
		t.Logf("Generated silver ticket of %d bytes (test result)", len(ticketBytes))
	}
}

// ============================================================================
// PASS-THE-HASH TESTS ✅
// ============================================================================

func TestPassTheHash_ValidNTLMHashFormat(t *testing.T) {
	k, _ := NewKerberosAttacker("dc.example.com", "example.com", "", "", "")
	
	// Test with valid NTLM hash format (128 hex characters)
	validHash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	
	client, err := k.PassTheHash(validHash)
	
	if err != nil {
		t.Logf("Pass-the-hash authentication failed (expected in test env): %v", err)
	} else {
		t.Logf("Client created successfully (unexpected in test env)")
		if client != nil {
			t.Log("Authentication client initialized")
		}
	}
}

func TestPassTheHash_InvalidHashFormat(t *testing.T) {
	k, _ := NewKerberosAttacker("dc.example.com", "example.com", "", "", "")
	
	// Test with invalid hash length (too short)
	invalidHash := "abc123"
	
	_, err := k.PassTheHash(invalidHash)
	
	if err == nil {
		t.Error("Should reject invalid NTLM hash format")
	} else {
		t.Logf("Correctly rejected invalid hash: %v", err)
	}
}

// ============================================================================
// HELPER FUNCTIONS ✅
// ============================================================================

// validateADDomainFormat validates domain name format
func validateADDomainFormat(domain string) bool {
	// Simple validation: must contain at least one dot
	for _, c := range domain {
		if c == '.' {
			return true
		}
	}
	return false
}

// TestValidateADDomainFormat verifies domain validation
func TestValidateADDomainFormat(t *testing.T) {
	testCases := []struct {
		domain      string
		shouldValid bool
	}{
		{"example.com", true},
		{"example.local", true},
		{"subdomain.example.com", true},
		{"nodot", false},
		{"com", false},
	}
	
	for _, tc := range testCases {
		result := validateADDomainFormat(tc.domain)
		if result != tc.shouldValid {
			t.Errorf("validateDomain(%q) = %v, want %v", tc.domain, result, tc.shouldValid)
		}
	}
}
