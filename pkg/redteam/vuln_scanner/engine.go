// Package vuln_scanner provides defensive vulnerability scanning capabilities
// for identifying security weaknesses in binaries and source code.
// This module is designed for ethical security assessment and hardening.
package vuln_scanner

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Severity level for vulnerability reports
type Severity string

const (
	Critical Severity = "CRITICAL"
	High     Severity = "HIGH"
	Medium   Severity = "MEDIUM"
	Low      Severity = "LOW"
	Info     Severity = "INFO"
)

// VulnerabilityType categorizes different types of vulnerabilities
type VulnerabilityType string

const (
	TypeBufferOverflow       VulnerabilityType = "BUFFER_OVERFLOW"
	TypeFormatString         VulnerabilityType = "FORMAT_STRING"
	TypeUseAfterFree         VulnerabilityType = "USE_AFTER_FREE"
	TypeDoubleFree           VulnerabilityType = "DOUBLE_FREE"
	TypeIntegerOverflow      VulnerabilityType = "INTEGER_OVERFLOW"
	TypeRaceCondition        VulnerabilityType = "RACE_CONDITION"
	TypeInjection            VulnerabilityType = "INJECTION"
	TypeSecurityMisconfiguration VulnerabilityType = "SECURITY_MISCONFIGURATION"
)

// VulnerabilityReport contains detailed findings from vulnerability scan
type VulnerabilityReport struct {
	ID             string              `json:"id"`
	Type           VulnerabilityType   `json:"type"`
	Severity       Severity            `json:"severity"`
	Function       string              `json:"function,omitempty"`
	File           string              `json:"file"`
	LineNumber     int                 `json:"line_number,omitempty"`
	Description    string              `json:"description"`
	RiskLevel      string              `json:"risk_level"`
	CWE            string              `json:"cwe_id,omitempty"`
	SANVCVE        string              `json:"sancve,omitempty"`
	Recommendation string              `json:"recommendation"`
	Evidence       string              `json:"evidence,omitempty"`
	TriageStatus   string              `json:"triage_status"` // TRIAGED, UNTRIAGED, FALSE_POSITIVE
	Timestamp      string              `json:"timestamp"`
	MITREATTK      string              `json:"mitre_attack,omitempty"`
}

// ScanResult aggregates all vulnerability findings from a scan operation
type ScanResult struct {
	TargetPath      string               `json:"target_path"`
	ScanTime        string               `json:"scan_time"`
	TotalVulnerabilities int             `json:"total_vulnerabilities"`
	CriticalCount   int                  `json:"critical_count"`
	HighCount       int                  `json:"high_count"`
	MediumCount     int                  `json:"medium_count"`
	LowCount        int                  `json:"low_count"`
	Vulnerabilities []VulnerabilityReport `json:"vulnerabilities"`
	Metadata        map[string]string    `json:"metadata,omitempty"`
}

// VulnerabilityScanner provides comprehensive security scanning capabilities
type VulnerabilityScanner struct {
	options *ScannerOptions
	history []*ScanResult
}

// ScannerOptions configures scanner behavior
type ScannerOptions struct {
	ExcludePatterns []string          // File patterns to skip
	IncludePatterns []string          // File patterns to include
	SeverityFilter  []Severity        // Only report severities >= this level
	MaxFindings     int               // Limit findings per type
	EnableCWE       bool              // Add CWE mapping
	EnableMITRE     bool              // Add MITRE ATT&K mapping
}

// DefaultScannerOptions returns safe defaults for production use
func DefaultScannerOptions() *ScannerOptions {
	return &ScannerOptions{
		ExcludePatterns: []string{".git/", "/vendor/", "node_modules/"},
		IncludePatterns: []string{"*.go", "*.c", "*.cpp", "*.h"},
		SeverityFilter:  []Severity{Critical, High, Medium, Low, Info},
		MaxFindings:     1000,
		EnableCWE:       true,
		EnableMITRE:     true,
	}
}

// NewVulnerabilityScanner creates new scanner with specified options
func NewVulnerabilityScanner(options *ScannerOptions) *VulnerabilityScanner {
	if options == nil {
		options = DefaultScannerOptions()
	}
	return &VulnerabilityScanner{
		options: options,
		history: make([]*ScanResult, 0),
	}
}

// DefaultNewVulnerabilityScanner returns legacy-compatible scanner instance
func DefaultNewVulnerabilityScanner() *VulnerabilityScanner {
	return NewVulnerabilityScanner(nil)
}

// GenerateFindingID creates unique ID for each finding
func GenerateFindingID(pattern string, file string, line int) string {
	id := fmt.Sprintf("%s-%s-%d", pattern, filepath.Base(file), line)
	if len(id) > 64 {
		hash := hex.EncodeToString([]byte(id))
		id = fmt.Sprintf("%s-%s", pattern, hash[:16])
	}
	return strings.ToUpper(id)
}

// IsExcluded checks if path should be skipped based on exclude patterns
func (s *VulnerabilityScanner) IsExcluded(path string) bool {
	for _, pattern := range s.options.ExcludePatterns {
		if strings.Contains(path, pattern) {
			return true
		}
	}
	return false
}

// ShouldInclude checks if file matches inclusion criteria
func (s *VulnerabilityScanner) ShouldInclude(filename string) bool {
	if len(s.options.IncludePatterns) == 0 {
		return true
	}
	for _, pattern := range s.options.IncludePatterns {
		if match, _ := filepath.Match(pattern, filename); match {
			return true
		}
	}
	return false
}

// FilterBySeverity filters findings based on severity threshold
func (s *VulnerabilityScanner) FilterBySeverity(findings []VulnerabilityReport) []VulnerabilityReport {
	severities := make(map[Severity]bool)
	for _, sev := range s.options.SeverityFilter {
		severities[sev] = true
	}
	
	filtered := make([]VulnerabilityReport, 0)
	for _, finding := range findings {
		if severities[finding.Severity] {
			filtered = append(filtered, finding)
		}
	}
	return filtered
}

// MapCWE adds Common Weakness Enumeration IDs
func (s *VulnerabilityScanner) MapCWE(vulnType VulnerabilityType, funcName string) string {
	cweMap := map[VulnerabilityType]string{
		TypeBufferOverflow:    "CWE-120",
		TypeFormatString:      "CWE-134",
		TypeUseAfterFree:      "CWE-416",
		TypeDoubleFree:        "CWE-415",
		TypeIntegerOverflow:   "CWE-190",
		TypeRaceCondition:     "CWE-362",
		TypeInjection:         "CWE-89",
		TypeSecurityMisconfiguration: "CWE-1188",
	}
	
	if cwe, ok := cweMap[vulnType]; ok {
		return cwe
	}
	return ""
}

// MapMITREATT&K adds MITRE ATT&K technique IDs
func (s *VulnerabilityScanner) MapMITRE(vulnType VulnerabilityType) string {
	mitreMap := map[VulnerabilityType]string{
		TypeBufferOverflow:    "T1190", // Exploit Public-Facing Application
		TypeFormatString:      "T1190",
		TypeUseAfterFree:      "T1190",
		TypeIntegerOverflow:   "T1190",
		TypeInjection:         "T1190",
		TypeSecurityMisconfiguration: "T1190",
	}
	
	if mitre, ok := mitreMap[vulnType]; ok {
		return mitre
	}
	return ""
}

// EnhanceReport enriches vulnerability report with CWE and MITRE data
func (s *VulnerabilityScanner) EnhanceReport(report *VulnerabilityReport) {
	if s.options.EnableCWE {
		report.CWE = s.MapCWE(report.Type, report.Function)
	}
	if s.options.EnableMITRE {
		report.MITREATTK = s.MapMITRE(report.Type)
	}
	if report.TriageStatus == "" {
		report.TriageStatus = "UNTRIAGED"
	}
	if report.Timestamp == "" {
		report.Timestamp = GetCurrentTimestamp()
	}
}

// ScanBinaryForOverflows identifies buffer overflow vulnerabilities in binary analysis
func (s *VulnerabilityScanner) ScanBinaryForOverflows(binaryPath string) ([]VulnerabilityReport, error) {
	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}
	
	reports := []VulnerabilityReport{}
	
	// Check for dangerous functions in symbol tables
	dangerousFuncs := map[string]VulnerabilityReport{
		"gets": {
			Type:           TypeBufferOverflow,
			Severity:       Critical,
			Function:       "gets",
			File:           binaryPath,
			Description:    "gets() function is inherently unsafe and always causes buffer overflows",
			RiskLevel:      "Critical buffer overflow vulnerability",
			Recommendation: "Immediately replace with fgets(buf, sizeof(buf), stdin)",
		},
		"strcpy": {
			Type:           TypeBufferOverflow,
			Severity:       High,
			Function:       "strcpy",
			File:           binaryPath,
			Description:    "strcpy() does not perform bounds checking",
			RiskLevel:      "Buffer overflow risk",
			Recommendation: "Replace with strncpy() or strlcpy()",
		},
		"strcat": {
			Type:           TypeBufferOverflow,
			Severity:       High,
			Function:       "strcat",
			File:           binaryPath,
			Description:    "strcat() does not perform bounds checking",
			RiskLevel:      "Buffer overflow risk",
			Recommendation: "Replace with strncat() or strlcat()",
		},
		"sprintf": {
			Type:           TypeBufferOverflow,
			Severity:       High,
			Function:       "sprintf",
			File:           binaryPath,
			Description:    "sprintf() does not perform bounds checking",
			RiskLevel:      "Buffer overflow risk",
			Recommendation: "Replace with snprintf()",
		},
		"scanf": {
			Type:           TypeBufferOverflow,
			Severity:       High,
			Function:       "scanf",
			File:           binaryPath,
			Description:    "scanf() without width limits can overflow buffers",
			RiskLevel:      "Buffer overflow risk",
			Recommendation: "Always specify width limit (%19s instead of %s)",
		},
		"vsprintf": {
			Type:           TypeBufferOverflow,
			Severity:       Critical,
			Function:       "vsprintf",
			File:           binaryPath,
			Description:    "vsprintf() is inherently unsafe",
			RiskLevel:      "Critical buffer overflow vulnerability",
			Recommendation: "Replace with vsnprintf()",
		},
	}
	
	dataStr := string(binaryData)
	for funcName, vuln := range dangerousFuncs {
		if strings.Contains(dataStr, funcName) {
			vuln.ID = GenerateFindingID(funcName, binaryPath, 0)
			s.EnhanceReport(&vuln)
			reports = append(reports, vuln)
		}
	}
	
	// Additional heuristic: check for suspicious byte patterns that might indicate stack canaries missing
	checkStackProtection(binaryPath, binaryData, &reports)
	
	return reports, nil
}

// ScanSourceCode performs comprehensive source code vulnerability scanning
func (s *VulnerabilityScanner) ScanSourceCode(sourceFile string) ([]VulnerabilityReport, error) {
	if !s.ShouldInclude(sourceFile) {
		return []VulnerabilityReport{}, nil
	}
	
	file, err := os.Open(sourceFile)
	if err != nil {
		return nil, fmt.Errorf("open failed: %w", err)
	}
	defer file.Close()
	
	reports := []VulnerabilityReport{}
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	
	// Dangerous patterns to detect
	patterns := []struct {
		re          *regexp.Regexp
		vulnType    VulnerabilityType
		description string
		severity    Severity
		riskLevel   string
		recommendation string
	}{
		{
			re:          regexp.MustCompile(`\bgets\s*\(`),
			vulnType:    TypeBufferOverflow,
			description: "gets() is inherently unsafe - always causes buffer overflow",
			severity:    Critical,
			riskLevel:   "Critical vulnerability - must remove immediately",
			recommendation: "Replace with fgets(buffer, size, stdin)",
		},
		{
			re:          regexp.MustCompile(`\bstrcpy\s*\(`),
			vulnType:    TypeBufferOverflow,
			description: "strcpy() lacks bounds checking",
			severity:    High,
			riskLevel:   "Buffer overflow risk",
			recommendation: "Use strncpy(dest, src, dest_size - 1)",
		},
		{
			re:          regexp.MustCompile(`\bstrcat\s*\(`),
			vulnType:    TypeBufferOverflow,
			description: "strcat() lacks bounds checking",
			severity:    High,
			riskLevel:   "Buffer overflow risk",
			recommendation: "Use strncat(dest, src, remaining_space)",
		},
		{
			re:          regexp.MustCompile(`\bsprintf\s*\(`),
			vulnType:    TypeBufferOverflow,
			description: "sprintf() lacks bounds checking",
			severity:    High,
			riskLevel:   "Buffer overflow risk",
			recommendation: "Use snprintf(buffer, size, ...)",
		},
		{
			re:          regexp.MustCompile(`%([^}]*)%[^s]`),
			vulnType:    TypeFormatString,
			description: "Potential format string vulnerability",
			severity:    High,
			riskLevel:   "Format string vulnerability risk",
			recommendation: "Use printf(\"%s\", user_input) instead of printf(user_input)",
		},
		{
			re:          regexp.MustCompile(`\beval\s*\(`),
			vulnType:    TypeInjection,
			description: "eval() allows arbitrary code execution",
			severity:    Critical,
			riskLevel:   "Remote code execution risk",
			recommendation: "Use parameterized commands or allowlist parsing",
		},
		{
			re:          regexp.MustCompile(`\bpopcount\s*\(`),
			vulnType:    TypeIntegerOverflow,
			description: "popcount() may cause integer underflow",
			severity:    Medium,
			riskLevel:   "Integer arithmetic risk",
			recommendation: "Add input validation before arithmetic operations",
		},
		{
			re:          regexp.MustCompile(`malloc.*sizeof\(char\)`),
			vulnType:    TypeBufferOverflow,
			description: "malloc(sizeof(char)) indicates manual memory management",
			severity:    Info,
			riskLevel:   "Manual memory management detected",
			recommendation: "Consider using automatic memory or safer allocations",
		},
	}
	
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		
		// Skip comments and includes
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") ||
			strings.HasPrefix(trimmed, "#include") || strings.HasPrefix(trimmed, "#ifdef") {
			continue
		}
		
		for _, p := range patterns {
			if p.re.MatchString(line) {
				report := VulnerabilityReport{
					ID:             GenerateFindingID(string(p.vulnType), sourceFile, lineNumber),
					Type:           p.vulnType,
					Severity:       p.severity,
					Function:       string(p.vulnType),
					File:           sourceFile,
					LineNumber:     lineNumber,
					Description:    p.description,
					RiskLevel:      p.riskLevel,
					Recommendation: p.recommendation,
					Evidence:       trimLine(line, 100),
				}
				s.EnhanceReport(&report)
				reports = append(reports, report)
				
				// Check max findings limit
				if s.options.MaxFindings > 0 && len(reports) >= s.options.MaxFindings {
					break
				}
			}
		}
	}
	
	if err := scanner.Err(); err != nil {
		return reports, fmt.Errorf("scan failed: %w", err)
	}
	
	return reports, nil
}

// trimLine shortens evidence lines for readability
func trimLine(line string, maxLen int) string {
	if len(line) <= maxLen {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(line[:maxLen]) + "..."
}

// ScanDirectory recursively scans directory for vulnerabilities
func (s *VulnerabilityScanner) ScanDirectory(dirPath string) (*ScanResult, error) {
	if !s.IsExcluded(dirPath) {
		result := &ScanResult{
			TargetPath: dirPath,
			ScanTime:   GetCurrentTimestamp(),
			Metadata:   make(map[string]string),
		}
		
		filesScanned := 0
		walkErr := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: cannot access %s: %v\n", path, err)
				return nil
			}
			
			if s.IsExcluded(path) {
				return nil
			}
			
			if info.IsDir() {
				return nil
			}
			
			if !s.ShouldInclude(info.Name()) {
				return nil
			}
			
			filesScanned++
			
			// Determine file type and scan accordingly
			ext := strings.ToLower(filepath.Ext(info.Name()))
			switch ext {
			case ".go", ".c", ".cpp", ".h":
				findings, err := s.ScanSourceCode(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error scanning %s: %v\n", path, err)
					return nil
				}
				result.Vulnerabilities = append(result.Vulnerabilities, findings...)
			case ".so", ".dll", ".exe", "":
				// Try binary scan if file has no extension but looks like binary
				if isBinaryFile(path) {
					findings, err := s.ScanBinaryForOverflows(path)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error scanning binary %s: %v\n", path, err)
						return nil
					}
					result.Vulnerabilities = append(result.Vulnerabilities, findings...)
				}
			}
			
			// Apply max findings limit
			if s.options.MaxFindings > 0 && len(result.Vulnerabilities) >= s.options.MaxFindings {
				return filepath.SkipAll
			}
			
			return nil
		})
		
		if walkErr != nil && walkErr != filepath.SkipAll {
			return result, walkErr
		}
		
		result.TotalVulnerabilities = len(result.Vulnerabilities)
		result.Vulnerabilities = s.FilterBySeverity(result.Vulnerabilities)
		result.ScanTime = GetCurrentTimestamp()
		result.Metadata = map[string]string{
			"files_scanned": fmt.Sprintf("%d", filesScanned),
			"exclude_patterns": strings.Join(s.options.ExcludePatterns, ";"),
		}
		
		// Count by severity
		countSeverities(result)
		
		s.history = append(s.history, result)
		return result, nil
	}
	
	return &ScanResult{
		TargetPath: dirPath,
		ScanTime:   GetCurrentTimestamp(),
		TotalVulnerabilities: 0,
	}, nil
}

// countSeverities populates Count fields based on vulnerabilities slice
func countSeverities(result *ScanResult) {
	for _, v := range result.Vulnerabilities {
		switch v.Severity {
		case Critical:
			result.CriticalCount++
		case High:
			result.HighCount++
		case Medium:
			result.MediumCount++
		case Low:
			result.LowCount++
		}
	}
}

// isBinaryFile checks if file appears to be a binary (ELF, PE, Mach-O)
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	
	header := make([]byte, 4)
	if _, err := f.Read(header); err != nil {
		return false
	}
	
	// ELF magic: 0x7f 'E' 'L' 'F'
	if header[0] == 0x7f && header[1] == 'E' && header[2] == 'L' && header[3] == 'F' {
		return true
	}
	
	// PE magic: 'M' 'Z'
	if header[0] == 'M' && header[1] == 'Z' {
		return true
	}
	
	// Mach-O magic
	machOSignatures := [][]byte{
		{0xFE, 0xED, 0xFA, 0xCE},
		{0xCE, 0xFA, 0xED, 0xFE},
		{0xFE, 0xED, 0xFA, 0xCF},
		{0xCF, 0xFA, 0xED, 0xFE},
	}
	
	for _, sig := range machOSignatures {
		match := true
		for i := range sig {
			if header[i] != sig[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	
	return false
}

// checkStackProtection looks for signs of stack protection being disabled
func checkStackProtection(binaryPath string, data []byte, reports *[]VulnerabilityReport) {
	dataStr := string(data)
	
	// Check for absence of common stack protector patterns
	hasCanary := strings.Contains(dataStr, "__stack_chk")
	hasProtector := strings.Contains(dataStr, "_FORTIFY_SOURCE")
	
	if !hasCanary && !hasProtector {
		report := VulnerabilityReport{
			ID:             GenerateFindingID("NO_CANARY", binaryPath, 0),
			Type:           TypeSecurityMisconfiguration,
			Severity:       Medium,
			Function:       "",
			File:           binaryPath,
			LineNumber:     0,
			Description:    "No stack canary protection detected",
			RiskLevel:      "Missing runtime buffer overflow protection",
			Recommendation: "Recompile with -fstack-protector-all",
			Evidence:       "Absence of __stack_chk symbols",
		}
		*reports = append(*reports, report)
	}
}

// GetCurrentTimestamp returns current time in ISO 8601 format
func GetCurrentTimestamp() string {
	t := strings.Split(os.Getenv("CURRENT_TIME"), " ")[0] + "T" + strings.Split(os.Getenv("CURRENT_TIME"), " ")[1]
	if t == "T" {
		return "2025-09-06T00:00:00Z"
	}
	return t
}

// GetScanHistory returns historical scan results
func (s *VulnerabilityScanner) GetScanHistory() []*ScanResult {
	historyCopy := make([]*ScanResult, len(s.history))
	copy(historyCopy, s.history)
	return historyCopy
}

// ClearHistory removes all scan history
func (s *VulnerabilityScanner) ClearHistory() {
	s.history = s.history[:0]
}

// Legacy compatibility alias
var SafeNewVulnerabilityScanner = DefaultNewVulnerabilityScanner
