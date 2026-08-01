// Package redteam - Mock Active Directory environment for testing
package redteam_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/ad"
)

// ============================================================================
// MOCK AD ENVIRONMENT FOR TESTING ✅ COMPLETE
// ===========================================================================

// MockDomainController simulates a Windows Domain Controller
type MockDomainController struct {
	domainName      string
	adminUser       string
	adminPassword   string
	computerObjects []string
	userObjects     []map[string]interface{}
	samDatabase     map[string]string // account -> unicodePwd hash
	lsassMemory     []byte            // Simulated LSASS memory dump
	policies        map[string]string // Policy objects
}

// NewMockDomainController creates mock DC instance
func NewMockDomainController(domainName, adminUser, adminPassword string) *MockDomainController {
	mock := &MockDomainController{
		domainName: domainName,
		adminUser: adminUser,
		adminPassword: adminPassword,
		userObjects: make([]map[string]interface{}, 0),
		samDatabase: make(map[string]string),
		policies: make(map[string]string),
	}
	
	// Initialize mock database with admin account
	mock.samDatabase[adminUser] = mock.generateNTLMHash(adminPassword)
	
	return mock
}

// generateNTLMHash generates NTLM hash from password
func (m *MockDomainController) generateNTLMHash(password string) string {
	// Simplified NTLM hash generation for testing
	// In production would use proper LM/NTLM hashing
	hash := fmt.Sprintf("%x", time.Now().UnixNano())[:32]
	for i := 0; i < len(password); i++ {
		hash += fmt.Sprintf("%02x", uint8(password[i]))
	}
	return hash + "aaaaaaaaaaaaaaaaaaaaaaaaaa" // Pad to 32 hex chars
}

// AddUser adds user account to mock domain
func (m *MockDomainController) AddUser(username, password, sid string) error {
	m.userObjects = append(m.userObjects, map[string]interface{}{
		"username": username,
		"password_hash": m.generateNTLMHash(password),
		"sid": sid,
	})
	
	// Add to SAM database
	m.samDatabase[username] = m.generateNTLMHash(password)
	
	return nil
}

// GetUserCredential retrieves user credential data
func (m *MockDomainController) GetUserCredential(username string) (map[string]interface{}, error) {
	for _, user := range m.userObjects {
		if user["username"] == username {
			return user, nil
		}
	}
	return nil, fmt.Errorf("user %s not found", username)
}

// DumpLSASSMemory simulates LSASS memory dump extraction
func (m *MockDomainController) DumpLSASSMemory() ([]byte, error) {
	// Simulate dumping all credentials from LSASS
	var memoryDump []byte
	
	for _, user := range m.userObjects {
		username := user["username"].(string)
		hash := user["password_hash"].(string)
		
		// Format: username:hash
		record := fmt.Sprintf("%s:%s\n", username, hash)
		memoryDump = append(memoryDump, []byte(record)...)
	}
	
	return memoryDump, nil
}

// GetSAMDatabase returns full SAM database dump
func (m *MockDomainController) GetSAMDatabase() (map[string]string, error) {
	return m.samDatabase, nil
}

// ============================================================================
// TEST HELPER FUNCTIONS ✅
// ===========================================================================

// createTestDomainForScenario creates test domain for specific attack scenario
func createTestDomainForScenario(scenario string) (*MockDomainController, error) {
	switch scenario {
	case "kerberoasting":
		return createKerberosTestingDomain()
	case "dcsync":
		return createDCSyncTestingDomain()
	case "golden_ticket":
		return createGoldenTicketTestingDomain()
	case "lateral_movement":
		return createLateralMovementDomain()
	default:
		return createBasicTestingDomain()
	}
}

func createBasicTestingDomain() (*MockDomainController, error) {
	dc := NewMockDomainController("test.local", "Administrator", "P@ssw0rd!")
	
	// Add some test users
	err := dc.AddUser("john.doe", "Password123!", "S-1-5-21-123456789-111")
	if err != nil {
		return nil, err
	}
	
	err = dc.AddUser("jane.smith", "SecurePass456!", "S-1-5-21-123456789-222")
	if err != nil {
		return nil, err
	}
	
	return dc, nil
}

func createKerberosTestingDomain() (*MockDomainController, error) {
	dc := NewMockDomainController("enterprise.local", "krbtgt", "KrbtgtPassword!")
	
	// Add service accounts
	err := dc.AddUser("HOST/dc.enterprise.local", "HostPass123!", "S-1-5-21-555555555-111")
	if err != nil {
		return nil, err
	}
	
	err = dc.AddUser("MSSQLSvc/sql.enterprise.local", "SQLPass456!", "S-1-5-21-555555555-222")
	if err != nil {
		return nil, err
	}
	
	return dc, nil
}

func createDCSyncTestingDomain() (*MockDomainController, error) {
	dc := NewMockDomainController("secure.corp", "Administrator", "Adm!n$ecur3Pa$$")
	
	// Add several privileged accounts
	err := dc.AddUser("ServiceAccount", "SvcAcc0unt!234", "S-1-5-21-777777777-111")
	if err != nil {
		return nil, err
	}
	
	err = dc.AddUser("BackupOperator", "Bakup0perator!", "S-1-5-21-777777777-222")
	if err != nil {
		return nil, err
	}
	
	err = dc.AddUser("DomainAdmin", "D0mainAdrmin!", "S-1-5-21-777777777-333")
	if err != nil {
		return nil, err
	}
	
	return dc, nil
}

func createGoldenTicketTestingDomain() (*MockDomainController, error) {
	dc := NewMockDomainController("golden.test", "krbtgt", "KrbTgt$ecret!")
	
	return dc, nil
}

func createLateralMovementDomain() (*MockDomainController, error) {
	dc := NewMockDomainController("lateral.test", "Administrator", "LateralPass!")
	
	// Create multiple hosts/users for lateral movement
	err := dc.AddUser("jsmith", "JSmithPass!", "S-1-5-21-888888888-111")
	if err != nil {
		return nil, err
	}
	
	err = dc.AddUser("jdoe", "JDoePass!", "S-1-5-21-888888888-222")
	if err != nil {
		return nil, err
	}
	
	err = dc.AddUser("svc_backup", "SvcBackUp!", "S-1-5-21-888888888-333")
	if err != nil {
		return nil, err
	}
	
	return dc, nil
}
