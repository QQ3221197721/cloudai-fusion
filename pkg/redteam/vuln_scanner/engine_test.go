package vuln_scanner_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/vuln_scanner"
)

func TestScannerCreation(t *testing.T) {
	scanner := vuln_scanner.NewVulnerabilityScanner(nil)
	if scanner == nil {
		t.Fatal("Failed to create vulnerability scanner")
	}
	
	options := vuln_scanner.DefaultScannerOptions()
	if options == nil {
		t.Fatal("DefaultScannerOptions returned nil")
	}
	
	t.Log("✓ Vulnerability scanner created successfully")
}

func TestScanBinaryForDangerousFunctions(t *testing.T) {
	// Create temporary file with dangerous function references
	tmpDir, err := os.MkdirTemp("", "vuln_scan_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	testFile := filepath.Join(tmpDir, "test_binary")
	dangerousContent := []byte(`
// Simulated binary with dangerous functions
#include <string.h>
#include <stdio.h>

void vulnerable_function() {
    char buffer[64];
    gets(buffer);  // DANGEROUS
    strcpy(buffer, "input");  // RISKY
    sprintf(buffer, "value");  // UNSAFE
}`)
	
	if err := os.WriteFile(testFile, dangerousContent, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	
	scanner := vuln_scanner.NewVulnerabilityScanner(nil)
	reports, err := scanner.ScanBinaryForOverflows(testFile)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	
	if len(reports) == 0 {
		t.Error("Expected findings but got none")
	} else {
		t.Logf("✓ Found %d vulnerabilities in test binary", len(reports))
	}
	
	// Verify reports have required fields
	for _, report := range reports {
		if report.File == "" {
			t.Errorf("Report missing file field: %v", report)
		}
		if report.Severity == "" {
			t.Errorf("Report missing severity: %v", report)
		}
	}
}

func TestScanSourceCode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vuln_scan_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	testFile := filepath.Join(tmpDir, "test_source.c")
	sourceCode := []byte(`
#include <stdio.h>
#include <string.h>

void process_input(char *user_input) {
    char buffer[256];
    
    // Vulnerable patterns
    gets(user_input);           // CRITICAL vulnerability
    strcpy(buffer, user_input); // HIGH risk
    sprintf(buffer, "%s", user_input); // MEDIUM risk
}`)
	
	if err := os.WriteFile(testFile, sourceCode, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	
	scanner := vuln_scanner.NewVulnerabilityScanner(nil)
	reports, err := scanner.ScanSourceCode(testFile)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	
	if len(reports) == 0 {
		t.Error("Expected vulnerabilities but got none")
	} else {
		t.Logf("✓ Source code scan found %d issues", len(reports))
		
		// Check critical severity detection
		hasCritical := false
		for _, report := range reports {
			if report.Severity == vuln_scanner.Critical {
				hasCritical = true
				break
			}
		}
		if !hasCritical {
			t.Error("Expected at least one CRITICAL severity finding")
		}
	}
}

func TestSeverityFiltering(t *testing.T) {
	scanner := vuln_scanner.NewVulnerabilityScanner(&vuln_scanner.ScannerOptions{
		SeverityFilter: []vuln_scanner.Severity{vuln_scanner.Critical, vuln_scanner.High},
	})
	
	tests := map[vuln_scanner.Severity]bool{
		vuln_scanner.Critical: true,
		vuln_scanner.High:     true,
		vuln_scanner.Medium:   false,
		vuln_scanner.Low:      false,
	}
	
	allReports := []vuln_scanner.VulnerabilityReport{
		{Severity: vuln_scanner.Critical},
		{Severity: vuln_scanner.High},
		{Severity: vuln_scanner.Medium},
		{Severity: vuln_scanner.Low},
	}
	
	filtered := scanner.FilterBySeverity(allReports)
			
	if len(filtered) != 2 {
		t.Errorf("Expected 2 filtered results, got %d", len(filtered))
	}
			
	for _, report := range filtered {
		if !tests[report.Severity] {
			t.Errorf("Should not include severity: %s", report.Severity)
		}
	}
	
	t.Log("✓ Severity filtering works correctly")
}

func TestCWEAndMITREMapping(t *testing.T) {
	scanner := vuln_scanner.NewVulnerabilityScanner(&vuln_scanner.ScannerOptions{
		EnableCWE:       true,
		EnableMITRE:     true,
	})
	
	report := vuln_scanner.VulnerabilityReport{
		Type: vuln_scanner.TypeBufferOverflow,
	}
	
	scanner.EnhanceReport(&report)
	
	if report.CWE == "" {
		t.Error("CWE mapping failed - empty CWE ID")
	} else {
		t.Logf("✓ CWE mapped: %s", report.CWE)
	}
	
	if report.MITREATTK == "" {
		t.Error("MITRE ATT&K mapping failed - empty technique ID")
	} else {
		t.Logf("✓ MITRE ATT&K mapped: %s", report.MITREATTK)
	}
}

func TestDirectoryScan(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vuln_scan_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	// Create test file with vulnerabilities
	testFile := filepath.Join(tmpDir, "vulnerable.c")
	code := []byte(`
void bad() {
    char buf[10];
    gets(buf);  // vulnerability
}`)
	os.WriteFile(testFile, code, 0644)
	
	// Create excluded file
	excludedDir := filepath.Join(tmpDir, ".git")
	os.MkdirAll(excludedDir, 0755)
	os.WriteFile(filepath.Join(excludedDir, "config"), []byte("gets(x)"), 0644)
	
	scanner := vuln_scanner.NewVulnerabilityScanner(nil)
	result, err := scanner.ScanDirectory(tmpDir)
	if err != nil {
		t.Fatalf("Directory scan failed: %v", err)
	}
	
	t.Logf("✓ Scanned %d files, found %d vulnerabilities", 
		parseMetadata(result.Metadata, "files_scanned"), 
		result.TotalVulnerabilities)
}

func parseMetadata(meta map[string]string, key string) int {
	val, ok := meta[key]
	if !ok {
		return 0
	}
	
	var result int
	for _, ch := range val {
		if ch >= '0' && ch <= '9' {
			result = result*10 + int(ch-'0')
		}
	}
	return result
}

func BenchmarkScanLargeDirectory(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench_scan")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	// Generate test files
	for i := 0; i < 100; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file_%d.go", i))
		content := []byte(fmt.Sprintf(`package test
func F%d() { gets("x"); }`, i))
		os.WriteFile(filename, content, 0644)
	}
	
	scanner := vuln_scanner.NewVulnerabilityScanner(nil)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanner.ScanDirectory(tmpDir)
	}
}
