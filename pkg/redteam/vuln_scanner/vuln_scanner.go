package vuln_scanner

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

// VulnScanner performs automated vulnerability discovery with authorization
type VulnScanner struct {
	TargetIP      string
	Timeout       time.Duration
	Threads       int
	CVEDatabase   map[string]CVEInfo
	AuditLog      *AuditLogger
	AuthGate      *AuthorizationGate
	TicketID      string // Work order ID for authorization tracking
}

// CVEInfo represents known vulnerability data
type CVEInfo struct {
	ID               string
	Description      string
	CVSS             float64
	AffectedVersions []string
	ExploitURL       string
	ProofOfConcept   string
	Remediation      string
}

// VulnFinding represents discovered vulnerability finding
type VulnFinding struct {
	CVE            CVEInfo
	Port           uint16
	Service        string
	Version        string
	Evidence       string
	RiskLevel      string // Critical, High, Medium, Low
	ExploitReady   bool   // Whether public exploits exist
	Timestamp      string // RFC3339 formatted timestamp
}

// DiscoverVulnerabilities scans target for known vulnerabilities (AFTER authorization)
func (v *VulnScanner) DiscoverVulnerabilities() ([]VulnFinding, error) {
	// Check authorization first - CRITICAL SECURITY GATE
	if err := v.AuthGate.ValidateBeforeExploit("vulnerability_scan", "Execute"); err != nil {
		return nil, fmt.Errorf("scan denied: %w", err)
	}

	v.AuditLog.Log(AuditEvent{
		Timestamp:    time.Now().UTC(),
		EventType:    "scan_initiated",
		ExploitType:  "vulnerability_discovery",
		TenantID:     v.AuthGate.TenantID,
		EngagementID: v.AuthGate.EngagementID,
	})

	if v.Timeout == 0 {
		v.Timeout = 10 * time.Second
	}
	if v.Threads == 0 {
		v.Threads = 100
	}

	// Scan common ports
	openPorts := v.scanPorts(1, 1024)
	v.AuditLog.Log(AuditEvent{
		Timestamp:   time.Now().UTC(),
		EventType:   "port_scan_complete",
		ExploitType: "tcp_port_scanning",
		Reason:      fmt.Sprintf("Found %d open ports", len(openPorts)),
	})

	// Fingerprint services
	services := v.fingerprintServices(openPorts)

	// Match against CVE database
	findings := v.matchCVEs(services)

	v.AuditLog.Log(AuditEvent{
		Timestamp:   time.Now().UTC(),
		EventType:   "scan_complete",
		ExploitType: "vulnerability_discovery",
		Reason:      fmt.Sprintf("Found %d vulnerabilities", len(findings)),
	})

	return findings, nil
}

// scanPorts performs parallel TCP port scanning
func (v *VulnScanner) scanPorts(startPort, endPort int) map[uint16]bool {
	openPorts := make(map[uint16]bool)

	var mu sync.Mutex
	var wg sync.WaitGroup

	for port := startPort; port <= endPort; port++ {
		wg.Add(1)

		go func(p uint16) {
			defer wg.Done()

			conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", v.TargetIP, p), v.Timeout)
			if err == nil {
				mu.Lock()
				openPorts[p] = true
				mu.Unlock()
				conn.Close()
			}
		}(uint16(port))
	}

	wg.Wait()

	return openPorts
}

// ServiceInfo contains service identification information
type ServiceInfo struct {
	Port     uint16
	Name     string
	Version  string
	Banner   string
	Protocol string
}

// fingerprintServices identifies service types and versions from banners
func (v *VulnScanner) fingerprintServices(openPorts map[uint16]bool) map[uint16]ServiceInfo {
	services := make(map[uint16]ServiceInfo)

	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile(`(?i)^SSH-\d\.\d`):                    "SSH",
		regexp.MustCompile(`(?i)^220 .* ESMTP`):                  "Postfix SMTP",
		regexp.MustCompile(`(?i)^220.*Microsoft ESMTP`):          "Exchange SMTP",
		regexp.MustCompile(`(?i)^220.*vsftpd`):                   "vsftpd FTP",
		regexp.MustCompile(`(?i)^3306.*MySQL`):                   "MySQL DB",
		regexp.MustCompile(`(?i)^445.*SMB`):                      "SMB/CIFS",
		regexp.MustCompile(`(?i)^HTTP/1\.\d`):                    "HTTP Server",
		regexp.MustCompile(`(?i)^OracleDB`):      "Oracle Database",
		regexp.MustCompile(`(?i)^FTP `):           "FTP Server",
		regexp.MustCompile(`(?i)^Telnet`):         "Telnet Service",
	}

	for port := range openPorts {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", v.TargetIP, port), 5*time.Second)
		if err != nil {
			continue
		}

		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buffer := make([]byte, 512)
		n, _ := conn.Read(buffer)

		banner := string(buffer[:n])

		svc := ServiceInfo{
			Port:   port,
			Banner: banner,
		}

		// Match banner against patterns
		for pattern, name := range patterns {
			if pattern.MatchString(banner) {
				svc.Name = name

				// Extract version if present
				versionPattern := regexp.MustCompile(`(\d+\.\d+(\.\d+)?)`)
				matches := versionPattern.FindString(banner)
				if matches != "" {
					svc.Version = matches
				}

				break
			}
		}

		services[port] = svc
		conn.Close()
	}

	return services
}

// matchCVEs matches services against known CVE database
func (v *VulnScanner) matchCVEs(services map[uint16]ServiceInfo) []VulnFinding {
	findings := []VulnFinding{}

	for port, svc := range services {
		// EternalBlue (MS17-010) - SMB
		if port == 445 && strings.Contains(svc.Banner, "SMB") {
			findings = append(findings, VulnFinding{
				CVE: CVEInfo{
					ID:               "CVE-2017-0144",
					Description:      "EternalBlue SMB Remote Code Execution",
					CVSS:             9.8,
					AffectedVersions: []string{"< 6.1.7601"},
					ExploitURL:       "https://www.exploit-db.com/exploits/41784",
					Remediation:      "Apply MS17-010 security update immediately",
				},
				Port:         port,
				Service:      svc.Name,
				Version:      svc.Version,
				Evidence:     "SMBv1 enabled on Windows host",
				RiskLevel:    "Critical",
				ExploitReady: true,
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
			})
		}

		// Apache Struts RCE
		if strings.Contains(svc.Name, "Apache") && (port == 80 || port == 443) {
			if compareVersions(svc.Version, "2.0.0", "2.5.25") {
				findings = append(findings, VulnFinding{
					CVE: CVEInfo{
						ID:               "CVE-2017-5638",
						Description:      "Apache Struts Jakarta MultiPart Parser RCE",
						CVSS:             10.0,
						AffectedVersions: []string{"2.3.0 - 2.3.31", "2.5.0 - 2.5.10"},
						ExploitURL:       "https://www.exploit-db.com/exploits/42135",
						Remediation:      "Upgrade to Apache Struts 2.5.26+",
					},
					Port:         port,
					Service:      svc.Name,
					Version:      svc.Version,
					Evidence:     "Apache Struts version vulnerable",
					RiskLevel:    "Critical",
					ExploitReady: true,
					Timestamp:    time.Now().UTC().Format(time.RFC3339),
				})
			}
		}

		// vsftpd backdoor
		if strings.Contains(svc.Name, "vsftpd") {
			if compareVersions(svc.Version, "2.0.0", "2.3.4") {
				findings = append(findings, VulnFinding{
					CVE: CVEInfo{
						ID:               "CVE-2011-2523",
						Description:      "vsftpd 2.3.4 Backdoor",
						CVSS:             10.0,
						AffectedVersions: []string{"2.3.4"},
						ExploitURL:       "https://www.exploit-db.com/exploits/18233",
						Remediation:      "Upgrade to vsftpd 2.3.5+",
					},
					Port:         port,
					Service:      svc.Name,
					Version:      svc.Version,
					Evidence:     "vsftpd 2.3.4 detected",
					RiskLevel:    "Critical",
					ExploitReady: true,
					Timestamp:    time.Now().UTC().Format(time.RFC3339),
				})
			}
		}

		// Web servers with common vulnerabilities
		if port == 80 || port == 443 {
			if strings.Contains(svc.Banner, "IIS/") {
				findings = append(findings, VulnFinding{
					CVE: CVEInfo{
						ID:               "CVE-2015-1635",
						Description:      "IIS HTTP.sys Remote Code Execution",
						CVSS:             10.0,
						Remediation:      "Apply May 2015 Security Bulletin Update",
					},
					Port:         port,
					Service:      svc.Name,
					Version:      svc.Version,
					Evidence:     "IIS HTTP.sys potentially vulnerable",
					RiskLevel:    "Critical",
					ExploitReady: true,
					Timestamp:    time.Now().UTC().Format(time.RFC3339),
				})
			}

			if strings.Contains(svc.Banner, "nginx") && compareVersions(svc.Version, "1.0.0", "1.16.1") {
				findings = append(findings, VulnFinding{
					CVE: CVEInfo{
						ID:               "CVE-2019-20372",
						Description:      "Nginx Lua Module Remote Code Execution",
						CVSS:             9.8,
						Remediation:      "Upgrade to Nginx 1.16.1 or 1.15.14",
					},
					Port:         port,
					Service:      svc.Name,
					Version:      svc.Version,
					Evidence:     "Nginx Lua Module vulnerable version detected",
					RiskLevel:    "High",
					ExploitReady: true,
					Timestamp:    time.Now().UTC().Format(time.RFC3339),
				})
			}

			if strings.Contains(svc.Banner, "Apache") && compareVersions(svc.Version, "2.0.0", "2.4.49") {
				findings = append(findings, VulnFinding{
					CVE: CVEInfo{
						ID:               "CVE-2021-41773",
						Description:      "Apache Path Traversal and RCE",
						CVSS:             9.8,
						Remediation:      "Upgrade to Apache 2.4.50",
					},
					Port:         port,
					Service:      svc.Name,
					Version:      svc.Version,
					Evidence:     "Apache HTTP Server vulnerable to path traversal",
					RiskLevel:    "Critical",
					ExploitReady: true,
					Timestamp:    time.Now().UTC().Format(time.RFC3339),
				})
			}
		}

		// MySQL authentication bypass
		if strings.Contains(svc.Name, "MySQL") && compareVersions(svc.Version, "5.0.0", "8.0.23") {
			findings = append(findings, VulnFinding{
				CVE: CVEInfo{
					ID:               "CVE-2021-2471",
					Description:      "MySQL Community Server Authentication Bypass",
					CVSS:             9.8,
					Remediation:      "Upgrade to MySQL 8.0.24 or later",
				},
				Port:         port,
				Service:      svc.Name,
				Version:      svc.Version,
				Evidence:     "MySQL version susceptible to auth bypass",
				RiskLevel:    "Critical",
				ExploitReady: true,
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
			})
		}

		// SSH vulnerabilities
		if strings.Contains(svc.Name, "SSH") && compareVersions(svc.Version, "3.0", "7.8") {
			findings = append(findings, VulnFinding{
				CVE: CVEInfo{
					ID:               "CVE-2020-15778",
					Description:      "OpenSSH Host Key Confirmation Side Channel",
					CVSS:             7.5,
					Remediation:      "Upgrade to OpenSSH 8.0+",
				},
				Port:         port,
				Service:      svc.Name,
				Version:      svc.Version,
				Evidence:     "OpenSSH version may have side channel vulnerability",
				RiskLevel:    "Medium",
				ExploitReady: false,
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
			})
		}
	}

	return findings
}

// compareVersions compares semver versions
func compareVersions(current, min, max string) bool {
	currentNum := parseVersion(current)
	minNum := parseVersion(min)
	maxNum := parseVersion(max)

	return currentNum >= minNum && currentNum < maxNum
}

// parseVersion converts version string to integer for comparison
func parseVersion(version string) int {
	parts := strings.Split(version, ".")
	result := 0
	multiplier := 1000

	for _, part := range parts {
		num := 0
		fmt.Sscanf(part, "%d", &num)
		result += num * multiplier
		multiplier /= 1000
	}

	return result
}

// CreateNewScanner initializes a new vulnerability scanner
func CreateNewScanner(targetIP string, tenantID, engagementID string) *VulnScanner {
	return &VulnScanner{
		TargetIP:  targetIP,
		Timeout:   10 * time.Second,
		Threads:   100,
		CVEDatabase: make(map[string]CVEInfo),
		AuditLog:  &AuditLogger{},
		AuthGate:  CreateAuthorizationForTenant(tenantID, engagementID, "Execute"),
	}
}

// GetCVEDatabase returns the internal CVE database snapshot
func (v *VulnScanner) GetCVEDatabase() map[string]CVEInfo {
	return v.CVEDatabase
}

// AddCVEToDatabase adds a new vulnerability entry
func (v *VulnScanner) AddCVEToDatabase(cve CVEInfo) {
	v.CVEDatabase[cve.ID] = cve
}

// AuthorizationGate validates before every exploit operation
type AuthorizationGate struct {
	Authorized      bool
	TenantID        string
	PermissionLevel string // Read, Write, Execute, Admin
	EngagementID    string
}

// PermissionType defines required permission levels
type PermissionType string

const (
	PermRead     PermissionType = "Read"
	PermWrite    PermissionType = "Write"
	PermExecute  PermissionType = "Execute"
	PermAdmin    PermissionType = "Admin"
)

// ValidateBeforeExploit rigorously checks authorization per OSEP/PEN-300 standards
func (a *AuthorizationGate) ValidateBeforeExploit(exploitType string, requiredPermission PermissionType) error {
	// Validation 1: Is tenant properly authorized?
	if !a.Authorized {
		event := AuditEvent{
			Timestamp:     time.Now().UTC(),
			EventType:     "authorization_denied",
			ExploitType:   exploitType,
			TenantID:      a.TenantID,
			EngagementID:  a.EngagementID,
			Reason:        "unauthorized_tenant - proof of authorization required",
			RequiredPerms: string(requiredPermission),
		}
		globalAuditLogger.Log(event)
		return fmt.Errorf("unauthorized tenant %s - no active engagement found for exploitation of %s", a.TenantID, exploitType)
	}

	// Validation 2: Does tenant have sufficient permission level?
	if !a.hasRequiredPermission(requiredPermission) {
		event := AuditEvent{
			Timestamp:     time.Now().UTC(),
			EventType:     "permission_denied",
			ExploitType:   exploitType,
			TenantID:      a.TenantID,
			EngagementID:  a.EngagementID,
			RequiredPerms: string(requiredPermission),
			ProvidedPerms: a.PermissionLevel,
		}
		globalAuditLogger.Log(event)
		return fmt.Errorf("insufficient permissions: tenant has [%s] but requires [%s] for %s exploitation", 
			a.PermissionLevel, requiredPermission, exploitType)
	}

	// Validation 3: Log authorization GRANT before allowing execution
	event := AuditEvent{
		Timestamp:     time.Now().UTC(),
		EventType:     "authorization_granted",
		ExploitType:   exploitType,
		TenantID:      a.TenantID,
		EngagementID:  a.EngagementID,
		RequiredPerms: string(requiredPermission),
	}
	globalAuditLogger.Log(event)

	return nil
}

// hasRequiredPermission verifies permission hierarchy based on OSCE³ requirements
// Hierarchy: Admin > Execute > Write > Read
func (a *AuthorizationGate) hasRequiredPermission(required PermissionType) bool {
	permissionLevels := map[PermissionType]int{
		PermRead:    1,
		PermWrite:   2,
		PermExecute: 3,
		PermAdmin:   4,
	}

	myLevel := permissionLevels[PermissionType(a.PermissionLevel)]
	requiredLevel := permissionLevels[required]

	return myLevel >= requiredLevel
}

// CreateAuthorizationForTenant creates authorized gate for specific tenant
func CreateAuthorizationForTenant(tenantID, engagementID, permissionLevel string) *AuthorizationGate {
	return &AuthorizationGate{
		Authorized:      true,
		TenantID:        tenantID,
		PermissionLevel: permissionLevel,
		EngagementID:    engagementID,
	}
}

// AuditEvent represents an audit log entry per compliance requirements
type AuditEvent struct {
	Timestamp     time.Time
	EventType     string
	ExploitType   string
	TenantID      string
	EngagementID  string
	Reason        string
	RequiredPerms string
	ProvidedPerms string
}

// Global audit logger for compliance tracking
type AuditLogger struct{}

// Log records audit event with ISO 8601 timestamp for compliance
func (a *AuditLogger) Log(event AuditEvent) {
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Format details based on event type
	details := fmt.Sprintf("[%s] %s", event.EventType, event.ExploitType)
	if event.Reason != "" {
		details += fmt.Sprintf(" Reason: %s", event.Reason)
	}
	if event.RequiredPerms != "" {
		details += fmt.Sprintf(" RequiredPerms: %s", event.RequiredPerms)
	}

	fmt.Printf("[AUDIT] %s | Tenant:%s | Engagement:%s | %s\n", 
		timestamp, event.TenantID, event.EngagementID, details)
}

var globalAuditLogger *AuditLogger

// InitializeGlobalAuditLogger sets up global audit logging system
func InitializeGlobalAuditLogger() {
	if globalAuditLogger == nil {
		globalAuditLogger = &AuditLogger{}
	}
}
