// Copyright 2026 CloudAI Fusion. All rights reserved.
// Licensed under the Apache License v2.0 (see /LICENSE file).
// IMPORTANT: Mock implementations for OSCE³ validation tests

package osce3_validation

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// MockScannerConfig configuration for mock vulnerability scanner
type MockScannerConfig struct {
	TargetIP        string
	Timeout         time.Duration
	Threads         int
	AuthGateEnabled bool
	AuditLogPath    string
	TicketID        string
}

// MockVulnScanner provides simulated vulnerability scanning results
type MockVulnScanner struct {
	config *MockScannerConfig
	mu     sync.RWMutex
}

// NewMockVulnScanner creates a mock vulnerability scanner for testing
func NewMockVulnScanner(config *MockScannerConfig) *MockVulnScanner {
	return &MockVulnScanner{config: config}
}

// VulnFinding represents a detected vulnerability
type VulnFinding struct {
	CVEID       string
	Description string
	Port        int
	RiskLevel   string
	Evidence    string
	Remediation string
}

// DiscoverVulnerabilities simulates scanning and returns known vulnerabilities
func (m *MockVulnScanner) DiscoverVulnerabilities() ([]*VulnFinding, error) {
	time.Sleep(500 * time.Millisecond) // Simulate scan time
	
	// Return realistic CVE findings based on Metasploitable3 profile
	findings := []*VulnFinding{
		{
			CVEID:       "CVE-2017-0144",
			Description: "EternalBlue SMB Remote Code Execution Vulnerability",
			Port:        445,
			RiskLevel:   "Critical",
			Evidence:    "SMBv1 enabled with NullSession access",
			Remediation: "Disable SMBv1, apply MS17-010 patch",
		},
		{
			CVEID:       "CVE-2017-5638",
			Description: "Apache Struts REST Plugin RCE",
			Port:        80,
			RiskLevel:   "Critical",
			Evidence:    "Apache Struts 2.3.5 detected on port 80",
			Remediation: "Upgrade Struts to latest version",
		},
		{
			CVEID:       "CVE-2011-2523",
			Description: "vsftpd backdoor command execution",
			Port:        21,
			RiskLevel:   "Critical",
			Evidence:    "vsftpd 2.3.4 installed with hidden backdoor",
			Remediation: "Upgrade vsftpd or remove vulnerable version",
		},
		{
			CVEID:       "CVE-2021-40438",
			Description: "Plink SSH Server Integer Overflow",
			Port:        22,
			RiskLevel:   "High",
			Evidence:    "Plink SSH server detected with buffer overflow",
			Remediation: "Update OpenSSH to patched version",
		},
		{
			CVEID:       "CVE-2015-5908",
			Description: "Samba cve_2015_0240 DoS",
			Port:        445,
			RiskLevel:   "High",
			Evidence:    "Samba service listening on port 445",
			Remediation: "Apply Samba security updates",
		},
		{
			CVEID:       "CVE-2018-10533",
			Description: "Apache modproxy AJP insecure proxying",
			Port:        8009,
			RiskLevel:   "Medium",
			Evidence:    "Tomcat AJP connector listening on port 8009",
			Remediation: "Disable AJP connector or restrict access",
		},
	}
	
	// Filter only critical/high by default
	criticalOnly := false
	if m.config.AuthGateEnabled {
		for _, f := range findings {
			if f.RiskLevel == "Critical" || f.RiskLevel == "High" {
				fmt.Printf("🔍 Detected: %s on port %d\n", f.CVEID, f.Port)
			}
		}
	}
	
	return findings, nil
}

// ============================================================================
// USER ENUMERATION MOCK
// ============================================================================

// MockExploitConfig configuration for post-exploitation mock engine
type MockExploitConfig struct {
	TargetHost      string
	PrivilegeLevel  int // 1=user, 2=SYSTEM, 3=root
	AuthGateEnabled bool
	AuditLogPath    string
	TicketID        string
}

// MockPostExploitationEngine simulates post-exploitation operations
type MockPostExploitationEngine struct {
	config *MockExploitConfig
	mu     sync.RWMutex
}

// NewMockPostExploitationEngine creates a mock post-exploitation engine
func NewMockPostExploitationEngine(config *MockExploitConfig) *MockPostExploitationEngine {
	return &MockPostExploitationEngine{config: config}
}

// UserRecord represents an enumerated user account
type UserRecord struct {
	Username string
	UID      int
	IsAdmin  bool
	HomeDir  string
	Status   string
}

// EnumerateUsers simulates user enumeration from target system
func (e *MockPostExploitationEngine) EnumerateUsers() ([]*UserRecord, error) {
	time.Sleep(300 * time.Millisecond) // Simulate enumeration time
	
	// Check privilege level restrictions
	if e.config.PrivilegeLevel < 1 {
		return nil, fmt.Errorf("insufficient privilege for user enumeration")
	}
	
	// Return typical Linux users from Metasploitable3
	users := []*UserRecord{
		{Username: "admin", UID: 1000, IsAdmin: true, HomeDir: "/home/admin", Status: "active"},
		{Username: "guest", UID: 1001, IsAdmin: false, HomeDir: "/home/guest", Status: "active"},
		{Username: "test", UID: 1002, IsAdmin: false, HomeDir: "/home/test", Status: "active"},
		{Username: "user1", UID: 1003, IsAdmin: false, HomeDir: "/home/user1", Status: "active"},
		{Username: "user2", UID: 1004, IsAdmin: false, HomeDir: "/home/user2", Status: "active"},
		{Username: "ftp", UID: 1005, IsAdmin: false, HomeDir: "/var/ftp", Status: "service"},
		{Username: "mysql", UID: 1006, IsAdmin: false, HomeDir: "/var/lib/mysql", Status: "service"},
	}
	
	if e.config.PrivilegeLevel >= 2 {
		// Add root user when SYSTEM privileges achieved
		users = append(users, &UserRecord{Username: "root", UID: 0, IsAdmin: true, HomeDir: "/root", Status: "system"})
	}
	
	fmt.Printf("Enumerated %d user accounts\n", len(users))
	return users, nil
}

// ============================================================================
// EXPLOIT ENGINE MOCK
// ============================================================================

// BufferOverflowExploiter simulates buffer overflow exploitation
type BufferOverflowExploiter struct {
	SandboxMode bool
}

// GenerateReverseShell creates reverse shell shellcode
func (e *BufferOverflowExploiter) GenerateReverseShell(host, port, platform string) ([]byte, error) {
	fmt.Printf("Generating reverse shell for %s...\n", platform)
	
	// Platform-specific mock shellcode sizes
	sizes := map[string]int{
		"linux-x64":  128,
		"windows-x64": 256,
		"linux-x86":  96,
	}
	
	size, ok := sizes[platform]
	if !ok {
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
	
	// Generate random bytes as mock shellcode
	shellcode := make([]byte, size)
	for i := range shellcode {
		shellcode[i] = byte(i%256)
	}
	
	return shellcode, nil
}

// VulnerabilityInfo describes target vulnerability details
type VulnerabilityInfo struct {
	CVEID         string
	BufferOffset  int
	ReturnAddress int
	Severity      string
	Description   string
}

// GeneratePOC creates exploit payload from shellcode and vulnerability info
func (e *BufferOverflowExploiter) GeneratePOC(vuln *VulnerabilityInfo, shellcode []byte) ([]byte, error) {
	payloadSize := vuln.BufferOffset + len(shellcode)
	payload := make([]byte, payloadSize)
	
	// NOP sled
	for i := 0; i < 16; i++ {
		payload[i] = 0x90
	}
	
	// Shellcode injection
	copy(payload[16:], shellcode)
	
	// Overwrite return address
	return payload, nil
}

// PayloadResult contains exploitation result
type PayloadResult struct {
	Success bool
	Output  string
	Memory  string
}

// ExecutePayload executes exploit against target
func (e *BufferOverflowExploiter) ExecutePayload(targetIP string, payload []byte) (*PayloadResult, error) {
	fmt.Printf("Executing payload against %s (%d bytes)\n", targetIP, len(payload))
	
	// Simulate successful exploitation
	if len(payload) < 32 {
		return &PayloadResult{
			Success: false,
			Output:  "payload too small",
		}, nil
	}
	
	result := &PayloadResult{
		Success: true,
		Output:  "whoami\nnt authority\\system",
		Memory:  "0x7ffda1234000-0x7ffda1235000 RWX",
	}
	
	return result, nil
}

// ============================================================================
// CREDENTIAL DUMPING MOCK
// ============================================================================

// PostExploitationEngine simulates credential dumping operations
type PostExploitationEngine struct {
	TargetHost     string
	PrivilegeLevel int
}

// CredentialHash represents extracted credential material
type CredentialHash struct {
	Username string
	NTLMHash string
	Domain   string
	Type     string
	Exists   bool
}

// DumpCredentials extracts NTLM hashes from LSASS/SAM
func (e *PostExploitationEngine) DumpCredentials() ([]*CredentialHash, error) {
	time.Sleep(400 * time.Millisecond) // Simulate credential dump time
	
	fmt.Printf("Dumping credentials from LSASS/SAM on %s\n", e.TargetHost)
	
	creds := []*CredentialHash{
		{Username: "Administrator", NTLMHash: "aad3b435b51404eeaad3b435b51404ee1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d", Domain: "LOCAL", Type: "NTLM", Exists: true},
		{Username: "krbtgt", NTLMHash: "ef2acfd91fec1e14ec2c83ad7fab9e37b1b3c3d8e2f1a4b5c6d7e8f9a0b1c2d3e", Domain: "CORP.LOCAL", Type: "NTLM", Exists: true},
		{Username: "Guest", NTLMHash: "31d6cfe0d16ae931b73c59d7c0c0f2bb1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d", Domain: "LOCAL", Type: "NTLM", Exists: true},
		{Username: "DefaultAccount", NTLMHash: "ffffffffffffffffffffffffffffffff1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d", Domain: "LOCAL", Type: "NTLM", Exists: true},
		{Username: "WDAGUtilityAccount", NTLMHash: "deadbeef1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d", Domain: "LOCAL", Type: "NTLM", Exists: true},
	}
	
	return creds, nil
}

// ============================================================================
// LATERAL MOVEMENT MOCK
// ============================================================================

// NetworkMap represents discovered network topology
type NetworkMap struct {
	Subnets    []*SubnetInfo
	LastScanned time.Time
}

// SubnetInfo describes discovered subnet
type SubnetInfo struct {
	CIDR       string
	ActiveHosts []*HostInfo
}

// HostInfo describes discovered host
type HostInfo struct {
	IP        string
	OpenPorts []int
	OSFamily  string
}

// PivotToNextHop performs lateral movement through network pivoting
func (e *PostExploitationEngine) PivotToNextHop() (*NetworkMap, error) {
	time.Sleep(600 * time.Millisecond) // Simulate pivoting
	
	fmt.Printf("Performing network pivoting from %s\n", e.TargetHost)
	
	// Map internal network structure
	subnets := []*SubnetInfo{
		{
			CIDR: "192.168.100.0/24",
			ActiveHosts: []*HostInfo{
				{IP: "192.168.100.10", OpenPorts: []int{22, 21, 80, 445, 3306}, OSFamily: "Linux"},
				{IP: "192.168.100.20", OpenPorts: []int{445, 88, 389, 53, 135}, OSFamily: "Windows"},
				{IP: "192.168.100.1", OpenPorts: []int{22, 80}, OSFamily: "Router"},
			},
		},
		{
			CIDR: "192.168.101.0/24",
			ActiveHosts: []*HostInfo{
				{IP: "192.168.101.10", OpenPorts: []int{22, 443}, OSFamily: "Linux"},
			},
		},
	}
	
	return &NetworkMap{
		Subnets: subnets,
	}, nil
}

// ============================================================================
// PERSISTENCE INSTALLATION MOCK
// ============================================================================

// PersistenceResult contains installed persistence mechanisms
type PersistenceResult struct {
	TotalCount   int
	Mechnisms    []*PersistenceMechanism
	SurvivedReboot bool
}

// PersistenceMechanism defines a persistence installation method
type PersistenceMechanism struct {
	Type      string
	Name      string
	Command   string
	Path      string
	Interval  string
}

// EstablishPersistence installs persistence mechanisms
func (e *PostExploitationEngine) EstablishPersistence() (*PersistenceResult, error) {
	fmt.Printf("Installing persistence mechanisms on %s\n", e.TargetHost)
	
	mechnisms := []*PersistenceMechanism{
		{Type: "Scheduled Task", Name: "Updater", Command: "cmd.exe /c whoami", Interval: "*/5 * * * *"},
		{Type: "Registry Run Key", Name: "Runtime", Path: "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run", Command: "C:\\temp\\backdoor.exe"},
		{Type: "Service", Name: "SysSvc", Path: "C:\\Windows\\System32\\service.exe", Command: "-p persistent"},
	}
	
	result := &PersistenceResult{
		TotalCount:   len(mechnisms),
		Mechnisms:    mechnisms,
		SurvivedReboot: true,
	}
	
	return result, nil
}

// ============================================================================
// NETWORK HELPER FUNCTIONS
// ============================================================================

// GetLocalIP returns local IP address (mock implementation)
func GetLocalIP() net.IP {
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			return ipNet.IP.To4()
		}
	}
	return net.IPv4(192, 168, 1, 1)
}

// CheckPortOpen tests if a port is open (mock implementation)
func CheckPortOpen(ip string, port int, timeout time.Duration) bool {
	_ = timeout // unused placeholder
	
	// Simulate port check - always return true for common services
	commonPorts := map[int]bool{
		22: true, 21: true, 80: true, 445: true, 389: true, 88: true, 135: true,
	}
	
	if exists, ok := commonPorts[port]; ok {
		return exists
	}
	return false
}
