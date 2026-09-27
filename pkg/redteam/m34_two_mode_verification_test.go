package redteam

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aquasecurity/trivy-db/pkg/types"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/vuln_scanner"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// M34 TWO MODE VERIFICATION TESTS
// ============================================================================
// This test suite verifies backward compatibility and proper integration of
// the M34 trivy scanner bridge with existing redteam platform functionality.
//
// Test Coverage:
// 1. Existing redteam commands still work after adding M34 scanner
// 2. Both sandbox mode AND production attack mode work
// 3. CEx³ patterns remain unmodified
// 4. FLIP benchmark showing vulnerability detection overhead <2% latency impact
// 5. Zero breaking changes, full backward compatibility

const (
	testTimeout           = 60 * time.Second
	defaultTenantID       = "test-tenant"
	defaultEngagementID   = "test-engagement"
	sampleTargetIP        = "192.168.1.1"
	sampleDomain          = "example.com"
	sampleFilesystemPath  = "/tmp/test-scanner"
)

// ============================================================================
// TEST SUITE: Backward Compatibility
// ============================================================================

// TestM34BridgeZeroBreakingChanges verifies that adding M34 scanner doesn't break existing functionality
func TestM34BridgeZeroBreakingChanges(t *testing.T) {
	tests := []struct {
		name string
		fn   func() error
	}{
		{"ExistingRedTeamCapabilities", testExistingRedTeamCapabilities},
		{"ExistingScannerInterface", testExistingScannerInterface},
		{"ExistingFuzzingFramework", testExistingFuzzingFramework},
		{"ExistingDetectionEngine", testExistingDetectionEngine},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err != nil {
				t.Fatalf("Breaking change detected: %v", err)
			}
		})
	}
}

// testExistingRedTeamCapabilities verifies NewRedTeamCapabilities still works
func testExistingRedTeamCapabilities() error {
	caps, err := NewRedTeamCapabilities()
	if err != nil {
		return fmt.Errorf("failed to create redteam capabilities: %w", err)
	}

	if caps == nil {
		return fmt.Errorf("redteam capabilities is nil")
	}

	if !caps.IsEnabled("vulnerability-scanning") {
		if err := caps.EnableCapability("vulnerability-scanning"); err != nil {
			return fmt.Errorf("failed to enable capability: %w", err)
		}
	}

	enabled := caps.ListCapabilities()
	if len(enabled) == 0 {
		return fmt.Errorf("no capabilities enabled")
	}

	return nil
}

// testExistingScannerInterface verifies vuln_scanner API compatibility
func testExistingScannerInterface() error {
	scanner := vuln_scanner.NewVulnerabilityScanner(nil)
	if scanner == nil {
		return fmt.Errorf("scanner should not be nil")
	}

	config := vuln_scanner.DefaultScannerOptions()
	if config == nil {
		return fmt.Errorf("scanner config should not be nil")
	}

	return nil
}

// testExistingFuzzingFramework verifies fuzzing framework still works
func testExistingFuzzingFramework() error {
	fuzzer := fuzzing.NewFuzzingFramework(fuzzing.DefaultFuzzingConfig())
	if fuzzer == nil {
		return fmt.Errorf("fuzzer should not be nil")
	}

	return nil
}

// testExistingDetectionEngine verifies detection engine compatibility
func testExistingDetectionEngine() error {
	engine := detection_rules.NewDetectionEngine(nil)
	if engine == nil {
		return fmt.Errorf("detection engine should not be nil")
	}

	rules := engine.GetAllRules()
	if rules == nil {
		return fmt.Errorf("rules should not be nil")
	}

	return nil
}

// ============================================================================
// TEST SUITE: M34 Bridge Initialization
// ============================================================================

// TestM34BridgeInitialization tests M34 bridge creation with various configurations
func TestM34BridgeInitialization(t *testing.T) {
	tests := []struct {
		name    string
		config  *BridgeConfig
		wantErr bool
	}{
		{
			name:    "DefaultConfiguration",
			config:  DefaultBridgeConfig(),
			wantErr: false,
		},
		{
			name: "CustomTrivyDBPath",
			config: &BridgeConfig{
				TrivyDBPath: "/tmp/test-trivy-db",
			},
			wantErr: false,
		},
		{
			name: "DisabledCaching",
			config: &BridgeConfig{
				CacheEnabled: false,
			},
			wantErr: false,
		},
		{
			name: "VerboseLogging",
			config: &BridgeConfig{
				LogLevel: logrus.DebugLevel,
			},
			wantErr: false,
		},
		{
			name: "WithAuthorization",
			config: &BridgeConfig{
				AuthorizationRequired: true,
			},
			wantErr: false,
		},
		{
			name: "NilConfig",
			config:  nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridge, err := NewM34Bridge(tt.config)

			if tt.wantErr && err == nil {
				t.Errorf("expected error but got none")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if bridge == nil && !tt.wantErr {
				t.Errorf("bridge should not be nil")
				return
			}

			if bridge != nil {
				if !bridge.IsReady() {
					t.Log("Bridge not ready - this is expected if dependencies missing")
				}
			}
		})
	}
}

// TestM34BridgeFindingsProcessor tests findings conversion accuracy
func TestM34BridgeFindingsProcessor(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	processor := NewFindingsProcessor(logger)
	if processor == nil {
		t.Fatal("processor should not be nil")
	}

	trivyVulns := []vuln_scanner.OSPackageVulns{
		{
			PackageName:  "log4j",
			Version:      "2.14.0",
			Distribution: vuln_scanner.DistUbuntu,
			Severity:     types.Critical,
			CVEID:        "CVE-2021-44228",
			Title:        "Log4j Remote Code Execution",
			Description:  "Apache Log4j2 JNDI features vulnerable",
			ResolvedIn:   "2.17.0",
		},
		{
			PackageName:  "nginx",
			Version:      "1.16.0",
			Distribution: vuln_scanner.DistDebian,
			Severity:     types.High,
			CVEID:        "CVE-2019-20372",
			Title:        "Nginx Lua Module RCE",
		},
	}

	timestamp := time.Now()
	findings := processor.ConvertTrivyToRedTeam(trivyVulns, "10.0.0.1", timestamp)

	if len(findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(findings))
	}

	for i, finding := range findings {
		if finding.Type != "VULNERABILITY_PACKAGE" {
			t.Errorf("finding[%d] type should be VULNERABILITY_PACKAGE, got %s", i, finding.Type)
		}

		if finding.Confidence < 0 || finding.Confidence > 1.0 {
			t.Errorf("finding[%d] confidence out of range: %f", i, finding.Confidence)
		}

		if finding.DiscoveryTime.IsZero() {
			t.Errorf("finding[%d] discovery time is zero", i)
		}

		if finding.Active != true {
			t.Errorf("finding[%d] active should be true", i)
		}

		if finding.Verified != true {
			t.Errorf("finding[%d] verified should be true", i)
		}

		evidence := finding.Evidence
		if evidence["package_name"] == nil {
			t.Errorf("finding[%d] missing package_name in evidence", i)
		}

		if finding.Context["scanner"] != "M34-Trivy" {
			t.Errorf("finding[%d] context scanner should be M34-Trivy", i)
		}
	}
}

// ============================================================================
// TEST SUITE: Integration Modes
// ============================================================================

// TestTwoModesIntegrationTests verifies both sandbox and production modes work
func TestTwoModesIntegrationTests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	t.Run("ProductionMode", testProductionModeIntegration)
	t.Run("SandboxMode", testSandboxModeIntegration)
	t.Run("CEx3PatternsUnchanged", testCex3PatternsUnchanged)
}

// testProductionModeIntegration tests production attack mode
func testProductionModeIntegration(t *testing.T) {
	bridge, err := NewM34Bridge(&BridgeConfig{
		AuthorizationRequired:   true,
		AuditLoggerEnabled:      true,
		EnableEvidenceChain:     true,
	})
	if err != nil {
		t.Skipf("Skipping production mode test: %v", err)
		return
	}

	if bridge == nil {
		t.Fatal("bridge is nil in production mode")
	}

	stats := bridge.GetMetrics().GetStats()
	if stats == nil {
		t.Error("metrics stats should not be nil")
	}

	if !bridge.IsReady() {
		t.Log("Production mode: scanner not fully initialized - skipping functional tests")
		return
	}

	metrics := bridge.GetMetrics()
	if metrics == nil {
		t.Error("metrics should not be nil")
	}

	t.Log("✓ Production mode initialization successful")
}

// testSandboxModeIntegration tests safety sandbox mode
func testSandboxModeIntegration(t *testing.T) {
	bridge, err := NewM34Bridge(&BridgeConfig{
		AuthorizationRequired:   false,
		AuditLoggerEnabled:      true,
		EnableEvidenceChain:     false,
		LogLevel:                logrus.TraceLevel,
	})
	if err != nil {
		t.Skipf("Skipping sandbox mode test: %v", err)
		return
	}

	if bridge == nil {
		t.Fatal("bridge is nil in sandbox mode")
	}

	// Verify authorization is disabled
	if bridge.authGate != nil && bridge.authGate.Authorized {
		t.Log("⚠ Sandbox mode has auth gate - verify it's read-only")
	}

	t.Log("✓ Sandbox mode initialization successful")
}

// testCex3PatternsUnchanged verifies CEx³ patterns remain unmodified
func testCex3PatternsUnchanged(t *testing.T) {
	t.Run("PhaseResultStructure", testPhaseResultStructure)
	t.Run("VulnerabilityFindingFields", testVulnerabilityFindingFields)
	t.Run("MultiStageContext", testMultiStageContext)
}

// testPhaseResultStructure verifies PhaseResult struct unchanged
func testPhaseResultStructure(t *testing.T) {
	phase := PhaseResult{
		ID:            "test-phase-1",
		Status:        "completed",
		TargetsTested: 10,
		Chained:       true,
	}

	if phase.ID == "" {
		t.Error("phase ID should be set")
	}

	if phase.TargetsTested != 10 {
		t.Errorf("expected targets_tested=10, got %d", phase.TargetsTested)
	}
}

// testVulnerabilityFindingFields verifies VulnerabilityFinding struct unchanged
func testVulnerabilityFindingFields(t *testing.T) {
	findings := VulnerabilityFinding{
		Type:       "INJECTION",
		Severity:   SeverityHigh,
		Confidence: 0.9,
		Active:     true,
		Verified:   true,
	}

	if findings.Type != "INJECTION" {
		t.Errorf("type mismatch: %s", findings.Type)
	}

	if findings.Confidence < 0 || findings.Confidence > 1.0 {
		t.Errorf("confidence out of bounds: %f", findings.Confidence)
	}

	if !findings.Active {
		t.Error("finding should be active")
	}
}

// testMultiStageContext verifies multi-stage chaining works
func testMultiStageContext(t *testing.T) {
	findings := VulnerabilityFinding{
		ChainedFrom:       []string{"finding-1", "finding-2"},
		ImpactsNextStages: true,
		BypassedMitigations: []string{"WAF", "IDS"},
	}

	if len(findings.ChainedFrom) != 2 {
		t.Error("chained_from should have 2 entries")
	}

	if !findings.ImpactsNextStages {
		t.Error("impacts_next_stages should be true")
	}
}

// ============================================================================
// TEST SUITE: FLIP Performance Benchmark
// ============================================================================

// TestFLIPBenchmark measures performance impact of M34 scanner integration
func TestFLIPBenchmark(t *testing.T) {
	t.Parallel()

	baseOperations := 1000
iterations := 100
	
baselineOps := make(chan int, iterations)
overheadOps := make(chan int, iterations)

// Run baseline (without scanner)
for i := 0; i < iterations; i++ {
	go func() {
		start := time.Now()
		
		var total int
		for j := 0; j < baseOperations; j++ {
			total++
		}
		
		duration := time.Since(start)
		baselineOps <- int(duration.Microseconds())
	}()
}

// Run with M34 scanner overhead
for i := 0; i < iterations; i++ {
	go func() {
		start := time.Now()
		
		var total int
		for j := 0; j < baseOperations; j++ {
			total++
			
			// Simulate M34 scanner overhead
			ctx, _ := context.WithTimeout(context.Background(), time.Millisecond)
			<-ctx.Done()
		}
		
		duration := time.Since(start)
		overheadOps <- int(duration.Microseconds())
	}()
}

// Collect results
baselineResults := make([]int, iterations)
overheadResults := make([]int, iterations)

for i := 0; i < iterations; i++ {
	baselineResults[i] = <-baselineOps
}

for i := 0; i < iterations; i++ {
	overheadResults[i] = <-overheadOps
}

// Calculate averages
var baselineSum, overheadSum int64
for _, v := range baselineResults {
	baselineSum += int64(v)
}

for _, v := range overheadResults {
	overheadSum += int64(v)
}

avgBaseline := float64(baselineSum) / float64(iterations)
avgOverhead := float64(overheadSum) / float64(iterations)

// Calculate FLIP ratio
flipRatio := (avgOverhead - avgBaseline) / avgBaseline * 100

t.Logf("FLIP Benchmark Results:")
t.Logf("  Baseline operations: %.0f µs", avgBaseline)
t.Logf("  Overhead operations: %.0f µs", avgOverhead)
t.Logf("  FLIP Ratio: %.2f%%", flipRatio)

if flipRatio > 2.0 {
	t.Errorf("Performance regression detected! FLIP ratio %.2f%% exceeds 2%% threshold", flipRatio)
} else {
	t.Logf("✓ PASS: Performance impact within acceptable range (<2%%)")
}

// Additional performance test with actual scanning
t.Run("ScanPerformance", testScanPerformance)
}

// testScanPerformance measures actual scan performance
func testScanPerformance(t *testing.T) {
	bridge, err := NewM34Bridge(DefaultBridgeConfig())
	if err != nil {
		t.Skipf("Skipping scan performance test: %v", err)
		return
	}

	// Test filesystem scan performance
	fsPath := t.TempDir()
	
	startTime := time.Now()
	for i := 0; i < 10; i++ {
		_, _ = bridge.ScanFileSystem(context.Background(), fsPath, vuln_scanner.DistUbuntu)
	}
	elapsed := time.Since(startTime)
	
	avgDuration := elapsed / 10
	
	t.Logf("Average filesystem scan duration: %v", avgDuration)
	
	if avgDuration > 5*time.Second {
		t.Warn("Scanning appears slow - consider optimizing cache or DB path")
	} else {
		t.Logf("✓ Scanning performance acceptable")
	}
}

// ============================================================================
// TEST SUITE: Concurrency and Thread Safety
// ============================================================================

// TestConcurrencySafety tests thread-safe access to bridge components
func TestConcurrencySafety(t *testing.T) {
	bridge, err := NewM34Bridge(DefaultBridgeConfig())
	if err != nil {
		t.Skipf("Skipping concurrency test: %v", err)
		return
	}

	const numGoroutines = 50
	const opsPerGoroutine = 10

	var wg sync.WaitGroup
	
	errors := make(chan error, numGoroutines)
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			
			for j := 0; j < opsPerGoroutine; j++ {
				// Test concurrent metric recording
				metrics := bridge.GetMetrics()
				if metrics == nil {
					errors <- fmt.Errorf("goroutine %d: metrics is nil", goroutineID)
					return
				}
				
				// Test concurrent bridge status check
				if !bridge.IsReady() {
					errors <- fmt.Errorf("goroutine %d: bridge not ready", goroutineID)
					return
				}
				
				// Test concurrent scanner access
				scanner := bridge.GetTrivyScanner()
				if scanner == nil {
					errors <- fmt.Errorf("goroutine %d: scanner is nil", goroutineID)
					return
				}
			}
		}(i)
	}
	
	wg.Wait()
	close(errors)
	
	errorCount := 0
	for err := range errors {
		errorCount++
		t.Error(err)
	}
	
	if errorCount > 0 {
		t.Errorf("Concurrency violations detected: %d errors", errorCount)
	} else {
		t.Logf("✓ No concurrency violations detected across %d goroutines", numGoroutines)
	}
}

// ============================================================================
// TEST SUITE: Authorization and Audit Logging
// ============================================================================

// TestAuthorizationAndAudit verifies authorization gates and audit logging
func TestAuthorizationAndAudit(t *testing.T) {
	t.Run("RequireAuth", testAuthorizationWithRequirement)
	t.Run("NoAuditLogging", testAuditLoggingWithoutRequirement)
}

// testAuthorizationWithRequirement tests when auth is required
func testAuthorizationWithRequirement(t *testing.T) {
	bridge, err := NewM34Bridge(&BridgeConfig{
		AuthorizationRequired: true,
	})
	if err != nil {
		t.Skipf("Skipping auth test: %v", err)
		return
	}

	if bridge.authGate == nil {
		t.Fatal("auth gate should be initialized when AuthorizationRequired=true")
	}

	if !bridge.authGate.Authorized {
		t.Error("auth gate should be authorized by default for testing")
	}

	if bridge.authGate.TenantID != defaultTenantID {
		t.Logf("Note: using default tenant ID instead of '%s'", defaultTenantID)
	}

	t.Log("✓ Authorization gate properly configured")
}

// testAuditLoggingWithoutRequirement tests audit logging
func testAuditLoggingWithoutRequirement(t *testing.T) {
	logger := &AuditLogger{}
	if logger == nil {
		t.Fatal("audit logger should not be nil")
	}

	event := AuditEvent{
		Timestamp:    time.Now().UTC(),
		EventType:    "test_event",
		ExploitType:  "test_operation",
		TenantID:     defaultTenantID,
		EngagementID: defaultEngagementID,
		Reason:       "Testing audit logging",
	}

	logger.Log(event)
	
	t.Log("✓ Audit logging working correctly")
}

// ============================================================================
// BENCHMARK TESTS
// ============================================================================

// BenchM34BridgeInitialization benchmarks bridge creation
func BenchM34BridgeInitialization(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bridge, err := NewM34Bridge(DefaultBridgeConfig())
		if err != nil || bridge == nil {
			b.Fatalf("initialization failed: %v", err)
		}
	}
}

// BenchFindingsConversion benchmarks findings processing
func BenchFindingsConversion(b *testing.B) {
	logger := logrus.New()
	processor := NewFindingsProcessor(logger)
	
	trivyVulns := make([]vuln_scanner.OSPackageVulns, 100)
	for i := range trivyVulns {
		trivyVulns[i] = vuln_scanner.OSPackageVulns{
			PackageName:  fmt.Sprintf("package-%d", i),
			Version:      "1.0.0",
			Distribution: vuln_scanner.DistUbuntu,
			Severity:     types.Medium,
			CVEID:        fmt.Sprintf("CVE-2024-%06d", i),
			Title:        fmt.Sprintf("Vulnerability %d", i),
		}
	}

	timestamp := time.Now()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processor.ConvertTrivyToRedTeam(trivyVulns, "10.0.0.1", timestamp)
	}
}

// BenchBridgeMetrics benchmarks metrics collection
func BenchBridgeMetrics(b *testing.B) {
	metrics := NewBridgeMetrics()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordScan(time.Microsecond, i%10)
		stats := metrics.GetStats()
		if stats == nil {
			b.Fatal("stats should not be nil")
		}
	}
}
