package ad_attacks

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// ACLAbuser handles Active Directory ACL/ACE exploitation.
// CRITICAL CEx³ capability for AD penetration testing!
type ACLAbuser struct {
	logger   *logrus.Logger
	AuthGate *AuthorizationGate
	AuditLog *AuditLogger
}

// AccessControlEntry represents a single ACE in AD ACL.
type AccessControlEntry struct {
	Type           string
	AccessMask     uint32
	ObjectGUID     string
	InheritedGUID  string
	PrincipalSID   string
	AllowedFlags   []string
	DeniedFlags    []string
	Priority       int
	IsInherited    bool
	CanExploit     bool
	RiskLevel      string
	Description    string
}

// ACLAbuseVector represents potential privilege escalation path.
type ACLAbuseVector struct {
	TargetUser     string
	TargetObject   string
	GrantedRights  []string
	PrivilegeType  string
	ExploitationMethod string
	SecurityImpact string
	TechnicalDetails string
}

// ACLAbuseResult contains enumeration outcome.
type ACLAbuseResult struct {
	Success        bool
	DangerousACEs  []AccessControlEntry
	AbuseVectors   []ACLAbuseVector
	TotalACEs      int
	HighestRisk    string
	TenantID       string
	Timestamp      time.Time
	Evidence       []byte
	Technique      string
}

// NewACLAbuser creates new ACL abuser instance.
func NewACLAbuser() *ACLAbuser {
	return &ACLAbuser{
		logger:   logrus.WithField("component", "acl_abuser"),
		AuthGate: &AuthorizationGate{},
		AuditLog: &AuditLogger{},
	}
}

// EnumerateACEs performs comprehensive ACE enumeration for target user.
func (a *ACLAbuser) EnumerateACEs(domainController string, targetUser string) (*ACLAbuseResult, error) {
	if a.AuthGate.TenantID != "" {
		a.AuditLog.Log("acl_enum_attempted", fmt.Sprintf("DC=%s User=%s", domainController, targetUser), a.AuthGate.TenantID)
	}

	result := &ACLAbuseResult{
		Timestamp: time.Now(),
		Technique: "T1098.004",
		TenantID:  a.AuthGate.TenantID,
		Success:   false,
	}

	// Simulate enumeration (in production would query LDAP/AD)
	dangerousACEs := a.buildSampleDangerousACEs(targetUser)
	result.DangerousACEs = dangerousACEs
	result.TotalACEs = len(dangerousACEs)

	// Generate exploitation vectors from dangerous ACEs
	result.AbuseVectors = a.generateAbuseVectors(dangerousACEs, targetUser)

	// Determine highest risk level
	highestRisk := calculateHighestRisk(dangerousACEs)
	result.HighestRisk = highestRisk

	if len(result.AbuseVectors) > 0 {
		result.Success = true
	}

	result.Evidence = []byte(fmt.Sprintf("Found %d dangerous ACEs and %v exploitation vectors for %s",
		len(dangerousACEs), len(result.AbuseVectors), targetUser))

	a.logger.Warnf("Enumerated %d dangerous ACEs for %s", len(dangerousACEs), targetUser)
	return result, nil
}

// buildSampleDangerousACEs constructs realistic dangerous ACE examples.
func (a *ACLAbuser) buildSampleDangerousACEs(targetUser string) []AccessControlEntry {
	aceTypes := map[string]uint32{
		"GenericAll":   0xF01FF,
		"GenericWrite": 0x200A0,
		"WriteDAC":     0x40000,
		"WriteOwner":   0x100000,
		"ForceChangePassword": 0x10000,
		"AddAccount":          0x2,
		"CreateChild":         0x10,
		"DeleteTree":          0x100,
	}

	aces := []AccessControlEntry{
		{
			Type:           "Allow",
			AccessMask:     aceTypes["GenericAll"],
			ObjectGUID:     "00000000-0000-0000-0000-000000000000", // All objects
			PrincipalSID:   "S-1-5-21-1234567890-123456789-123456789-512", // Domain Admins
			AllowedFlags:   []string{"DS-Replication-Get-Changes", "DS-Replication-Get-Changes-All"},
			CanExploit:     true,
			RiskLevel:      "CRITICAL",
			Description:    "Full control over any object - can modify permissions on any account",
		},
		{
			Type:           "Allow",
			AccessMask:     aceTypes["GenericWrite"],
			ObjectGUID:     "bf967a86-0de6-492b-9dae-ae0bae3c1e0f", // msDS-Group-Membership
			PrincipalSID:   "S-1-5-21-1234567890-123456789-123456789-1101", // Normal Users
			AllowedFlags:   []string{"GenericWrite", "WriteProperty"},
			CanExploit:     true,
			RiskLevel:      "HIGH",
			Description:    "Can add self to security groups including Domain Admins",
		},
		{
			Type:           "Allow",
			AccessMask:     aceTypes["ForceChangePassword"],
			ObjectGUID:     "00000000-0000-0000-0000-000000000000",
			PrincipalSID:   "S-1-5-21-1234567890-123456789-123456789-519", // Domain Guests
			AllowedFlags:   []string{"ForceChangePassword"},
			CanExploit:     true,
			RiskLevel:      "MEDIUM",
			Description:    "Can change password of any account",
		},
		{
			Type:           "Allow",
			AccessMask:     aceTypes["AddAccount"],
			ObjectGUID:     "f3a646ab-2a98-4804-a6cb-57a63a5b5959", // Computer objects
			PrincipalSID:   "S-1-5-21-1234567890-123456789-123456789-500", // Administrators
			AllowedFlags:   []string{"CreateChild"},
			CanExploit:     true,
			RiskLevel:      "HIGH",
			Description:    "Can join computers to domain for DCE-KST attack",
		},
		{
			Type:           "Allow",
			AccessMask:     aceTypes["WriteDAC"],
			ObjectGUID:     "c8b61aa8-c3e9-44dd-9b7a-5c5a9b8f8a3b", // Service principals
			PrincipalSID:   targetUser + "\\Administrators",
			AllowedFlags:   []string{"WriteDAC", "ModifyPermissions"},
			CanExploit:     true,
			RiskLevel:      "CRITICAL",
			Description:    "Can remove ACL protections from service accounts",
		},
	}

	a.logger.Infof("Built %d sample dangerous ACEs", len(aces))
	return aces
}

// generateAbuseVectors creates exploitation vectors from dangerous ACEs.
func (a *ACLAbuser) generateAbuseVectors(aces []AccessControlEntry, targetUser string) []ACLAbuseVector {
	var vectors []ACLAbuseVector

	for _, ace := range aces {
		vector := ACLAbuseVector{
			TargetUser:   targetUser,
			TargetObject: "Active Directory Object",
			GrantedRights: ace.AllowedFlags,
		}

		switch ace.AccessMask {
		case 0xF01FF: // GenericAll
			vector.PrivilegeType = "Domain Admin Equivalence"
			vector.ExploitationMethod = "BloodHound: Add Self to DA Group | Set SPN | DCSync"
			vector.SecurityImpact = "Complete domain compromise with no additional privileges needed"
			vector.TechnicalDetails = "Use BloodHound data or SharpACLCheck to enumerate and exploit"
		
		case 0x200A0: // GenericWrite
			vector.PrivilegeType = "Account Takeover"
			vector.ExploitationMethod = "PowerView: Set-ADObject -Replace @{msDS-UserAccountControl='ACHT'} | Add-AdObject"
			vector.SecurityImpact = "Takeover user/group/computer accounts, escalate via group membership changes"
			vector.TechnicalDetails = "Can modify attributes, add to groups, trigger Kerberoasting"
		
		case 0x40000: // WriteDAC
			vector.PrivilegeType = "ACL Bypass"
			vector.ExploitationMethod = "Rubeus: dacleditor.exe /principal:%s /rights:WriteDACL /object:target /inheritance:acl"
			vector.SecurityImpact = "Remove ACL restrictions, escalate permissions chain"
			vector.TechnicalDetails = "Can remove DACL entries, grant privileges to malicious SIDs"
		
		case 0x10000: // ForceChangePassword
			vector.PrivilegeType = "Password Reset Attack"
			vector.ExploitationMethod = "SeatBelt: Change-UserPassword -Identity target -NewPassword random"
			vector.SecurityImpact = "Password reset without authentication token, lateral movement enabled"
			vector.TechnicalDetails = "Use mimikatz 'kerberos::pac' or Rubeus to leverage password change"
		
		case 0x2: // AddAccount
			vector.PrivilegeType = "Computer Account Creation"
			vector.ExploitationMethod = "Certify: New-ComputersAccount | PowerView: Add-ComputerAccount"
			vector.SecurityImpact = "Man-in-the-middle attacks via computer account trust relationship"
			vector.TechnicalDetails = "Attacker controls computer account hash for Kerberos AS-REP Roasting"
		}

		vectors = append(vectors, vector)
	}

	a.logger.Debugf("Generated %d abuse vectors", len(vectors))
	return vectors
}

// CalculateAERiskScore computes risk score based on ACE findings.
func (a *ACLAbuser) CalculateAERiskScore(result *ACLAbuseResult) int {
	score := 0

	// Score per dangerous ACE
	for _, ace := range result.DangerousACEs {
		switch ace.RiskLevel {
		case "CRITICAL":
			score += 40
		case "HIGH":
			score += 30
		case "MEDIUM":
			score += 20
		case "LOW":
			score += 10
		}
	}

	// Bonus for multiple abuse vectors
	if len(result.AbuseVectors) >= 3 {
		score += 20
	} else if len(result.AbuseVectors) >= 1 {
		score += 10
	}

	// Cap at 100
	if score > 100 {
		score = 100
	}

	a.logger.Debugf("Calculated ACL abuse risk score: %d/100", score)
	return score
}

// GenerateRecommendations provides remediation guidance for ACL issues.
func (a *ACLAbuser) GenerateRecommendations(result *ACLAbuseResult) []string {
	recommendations := []string{}

	for _, vector := range result.AbuseVectors {
		switch vector.PrivilegeType {
		case "Domain Admin Equivalence":
			recommendations = append(recommendations, 
				fmt.Sprintf("REVIEW: Remove GenericAll permissions from non-administrative users on %s",
					vector.TargetObject))
		
		case "Account Takeover":
			recommendations = append(recommendations,
				"Audit: Review GenericWrite permissions across all OUs, implement controlled delegation")
		
		case "ACL Bypass":
			recommendations = append(recommendations,
				"Restrict: Ensure minimum necessary permissions following principle of least privilege")
		
		case "Password Reset Attack":
			recommendations = append(recommendations,
				"Monitor: Enable auditing for password modification events using Security ID S-1-5-21-xxx")
		
		case "Computer Account Creation":
			recommendations = append(recommendations,
				"Configure: Default computer account creation restrictions via GPO settings")
		}
	}

	return recommendations
}

// Helper function to determine highest risk level.
func calculateHighestRisk(aces []AccessControlEntry) string {
	maxRisk := "NONE"
	riskOrder := map[string]int{"NONE": 0, "LOW": 1, "MEDIUM": 2, "HIGH": 3, "CRITICAL": 4}

	for _, ace := range aces {
		current := riskOrder[ace.RiskLevel]
		max := riskOrder[maxRisk]
		if current > max {
			maxRisk = ace.RiskLevel
		}
	}

	return maxRisk
}

// Helper function to check if string contains substring case-insensitive.
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
