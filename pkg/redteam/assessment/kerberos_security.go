package assessment

import (
	"fmt"
	"time"
)

// EncryptionType represents Kerberos encryption types
type EncryptionType int32

const (
	EncryptionNone      EncryptionType = 0
	EncryptionDES       EncryptionType = 1
	EncryptionRC4_HMAC  EncryptionType = 23
	EncryptionAES128    EncryptionType = 17
	EncryptionAES256    EncryptionType = 18
)

// String returns human-readable name for encryption type
func (e EncryptionType) String() string {
	switch e {
	case EncryptionNone:
		return "NONE"
	case EncryptionDES:
		return "DES-CBC-MD5 (INSECURE)"
	case EncryptionRC4_HMAC:
		return "RC4-HMAC (WEAK)"
	case EncryptionAES128:
		return "AES-128-CTS-HMAC-SHA1-96"
	case EncryptionAES256:
		return "AES-256-CTS-HMAC-SHA1-96 (RECOMMENDED)"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", e)
	}
}

// KERBConfig defines Active Directory Kerberos configuration
type KERBConfig struct {
	DomainName                 string
	DomainController           string
	KDCPort                    int
	KrbtgtPasswordAge          time.Duration
	DefaultEncryptionType      EncryptionType
	RequireStrongCrypto        bool
	ProtectedUsersGroupEnabled bool
	TicketLifetime             time.Duration
}

// KERBDetectionResult captures findings from Kerberos security assessment
type KERBDetectionResult struct {
	FindingID        string
	Title            string
	Description      string
	RiskLevel        string
	SeverityScore    float64
	AffectedSystems  []string
	VulnerableConfigs []map[string]interface{}
	Evidence         []string
	Remediation      string
	MITRETACTechniques []string
	ComplianceMap    map[string]string
}

// KerberosSecurityAssessor performs Kerberos security assessments
type KerberosSecurityAssessor struct {
	config     KERBConfig
	findings   []*KERBDetectionResult
}

// NewKerberosAssessor creates a new Kerberos security assessor
func NewKerberosAssessor(config KERBConfig) *KerberosSecurityAssessor {
	return &KerberosSecurityAssessor{
		config: config,
	}
}

// Assess performs comprehensive Kerberos security assessment
func (a *KerberosSecurityAssessor) Assess() []*KERBDetectionResult {
	a.findings = make([]*KERBDetectionResult, 0)
	a.checkEncryptionTypes()
	return a.findings
}

// checkEncryptionTypes validates Kerberos encryption algorithm usage
func (a *KerberosSecurityAssessor) checkEncryptionTypes() {
	if !a.config.RequireStrongCrypto || a.config.DefaultEncryptionType == EncryptionRC4_HMAC || 
	   a.config.DefaultEncryptionType == EncryptionDES {
		
		riskLevel := "HIGH"
		severity := 7.5
		encryptionType := a.config.DefaultEncryptionType.String()
		
		if encryptionType == "DES-CBC-MD5 (INSECURE)" {
			riskLevel = "CRITICAL"
			severity = 9.2
		}
		
		finding := &KERBDetectionResult{
			FindingID:     fmt.Sprintf("KERB-ENC-%d", encryptionType),
			Title:         fmt.Sprintf("Weak Encryption Type Detected: %s", encryptionType),
			Description: fmt.Sprintf("The domain is configured to use %s encryption, which has known"+
				" cryptographic vulnerabilities.", encryptionType),
			RiskLevel:     riskLevel,
			SeverityScore: severity,
			AffectedSystems: []string{a.config.DomainController},
			VulnerableConfigs: []map[string]interface{}{
				{"encryptionType": encryptionType, "enabled": true},
			},
			Evidence: []string{
				fmt.Sprintf("DefaultEncryptionType: %d", a.config.DefaultEncryptionType),
				fmt.Sprintf("RequireStrongCrypto: %v", a.config.RequireStrongCrypto),
			},
			Remediation: buildSimpleRemediation(encryptionType),
			MITRETACTechniques: []string{
				"T1558.003 - Kerberos Administration: Golden Ticket",
				"T1558.004 - Kerberos Administration: Silver Ticket",
			},
			ComplianceMap: map[string]string{
				"NIST SP 800-53": "SC-11 (Cryptographic Protection)",
				"CIS Controls v8": "4.1 (Secure Configuration)",
			},
		}
		a.findings = append(a.findings, finding)
	}
}

// buildSimpleRemediation provides specific remediation steps
func buildSimpleRemediation(encType string) string {
	return fmt.Sprintf(`### Remediation for %s:
1. Configure Group Policy to enforce AES encryption
2. Run: New-GPO -Name "Disable Weak Kerberos Encryption"
3. Test legacy system compatibility before enforcement
4. Monitor Kerberos authentication logs post-change`, encType)
}

// GenerateReport creates formatted security assessment report
func (a *KerberosSecurityAssessor) GenerateReport() string {
	report := fmt.Sprintf("# Kerberos Security Assessment Report\n")
	report += fmt.Sprintf("Domain: %s\nAssessment Date: %s\n\n", a.config.DomainName, time.Now().Format("2006-01-02"))
	
	totalFindings := len(a.findings)
	criticalCount := 0
	highCount := 0
	
	for _, finding := range a.findings {
		if finding.RiskLevel == "CRITICAL" {
			criticalCount++
		} else if finding.RiskLevel == "HIGH" {
			highCount++
		}
	}
	
	report += fmt.Sprintf("## Summary\nTotal Findings: %d (Critical: %d, High: %d)\n\n", totalFindings, criticalCount, highCount)
	
	for _, finding := range a.findings {
		report += fmt.Sprintf("### %s: %s\nSeverity: %.1f/10 (%s)\n", 
			finding.FindingID, finding.Title, finding.SeverityScore, finding.RiskLevel)
		report += fmt.Sprintf("Description: %s\n\n", finding.Description)
		report += fmt.Sprintf("**Remediation**:\n%s\n\n", finding.Remediation)
	}
	
	report += "---\n*Report generated by CloudAI Fusion OSE3 Module*\n"
	return report
}
