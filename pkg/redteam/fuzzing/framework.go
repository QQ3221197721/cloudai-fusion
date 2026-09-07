// Package fuzzing provides automated fuzzing capabilities using AFL++ for discovering
// security vulnerabilities through systematic input mutation and execution monitoring.
// This module implements ethical security testing methodologies.
package fuzzing

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Severity represents vulnerability severity levels
type Severity string

const (
	Critical Severity = "CRITICAL"
	High     Severity = "HIGH"
	Medium   Severity = "MEDIUM"
	Low      Severity = "LOW"
	Info     Severity = "INFO"
)

// CrashType categorizes different types of crashes discovered during fuzzing
type CrashType string

const (
	TypeSegmentationFault  CrashType = "SEGFAULT"
	TypeStackSmashing      CrashType = "STACK_SMASHING"
	TypeOutOfMemory        CrashType = "OOM"
	TypeTimeout            CrashType = "TIMEOUT"
	TypeAssertionFailure   CrashType = "ASSERTION_FAILURE"
	TypeUndefinedBehavior  CrashType = "UB"
	TypeUnknown            CrashType = "UNKNOWN"
)

// FuzzingStatus tracks current fuzzing campaign state
type FuzzingStatus string

const (
	StatusIdle     FuzzingStatus = "IDLE"
	StatusRunning  FuzzingStatus = "RUNNING"
	StatusPaused   FuzzingStatus = "PAUSED"
	StatusComplete FuzzingStatus = "COMPLETE"
	StatusFailed   FuzzingStatus = "FAILED"
)

// VulnerabilityReport contains findings from fuzzing campaigns
type VulnerabilityReport struct {
	ID          string        `json:"id"`
	CrashType   CrashType     `json:"crash_type"`
	Severity    Severity      `json:"severity"`
	InputPath   string        `json:"input_path"`
	InputData   string        `json:"input_data,omitempty"`
	TerminalCmd string        `json:"terminal_cmd,omitempty"`
	Description string        `json:"description"`
	RiskLevel   string        `json:"risk_level"`
	Recommendation string      `json:"recommendation"`
	FindingTime string        `json:"finding_time"`
	StackTrace  string        `json:"stack_trace,omitempty"`
}

// FuzzingConfig defines fuzzing campaign parameters
type FuzzingConfig struct {
	InputDir     string        // Directory containing seed inputs
	OutputDir    string        // Directory for fuzzing results
	BinaryPath   string        // Target binary to fuzz
	Timeout      time.Duration // Execution timeout per sample
	Qualities    []string      // Quality modes: dumb, explore, cache, eureka
	Workers      int           // Number of fuzzing workers
	TimeLimit    time.Duration // Total time limit
	Dictionaries map[string][]string // Input dictionaries
	Energy       string        // Fuzzing energy schedule
	Planner      string        // Planner algorithm: fifo, queue, fast
}

// DefaultFuzzingConfig returns sensible defaults for most fuzzing scenarios
func DefaultFuzzingConfig(binaryPath string) *FuzzingConfig {
	return &FuzzingConfig{
		InputDir:  "./seed_inputs",
		OutputDir: "./fuzz_output",
		BinaryPath: binaryPath,
		Timeout:   5 * time.Second,
		Qualities: []string{"explore"},
		Workers:   4,
		TimeLimit: 24 * time.Hour,
		Dictionaries: map[string][]string{},
		Energy:   "default",
		Planner:  "fast",
	}
}

// FuzzingResult captures metrics and statistics from a completed fuzzing campaign
type FuzzingResult struct {
	CampaignName    string                `json:"campaign_name"`
	StartTime       time.Time             `json:"start_time"`
	EndTime         time.Time             `json:"end_time"`
	Status          FuzzingStatus         `json:"status"`
	TotalExecutions uint64                `json:"total_executions"`
	AverageCPS      float64               `json:"average_cps"`
	PendingInputs   uint64                `json:"pending_inputs"`
	FoundCrashes    uint64                `json:"found_crashes"`
	CrashReports    []VulnerabilityReport `json:"crash_reports,omitempty"`
	Hangs           uint64                `json:"hangs"`
	Config          *FuzzingConfig        `json:"config,omitempty"`
	Metrics         map[string]interface{}`json:"metrics,omitempty"`
}

// FuzzingFramework orchestrates AFL++ fuzzing campaigns and crash analysis
type FuzzingFramework struct {
	config *FuzzingConfig
	campaigns []*FuzzingResult
	currentStatus FuzzingStatus
	aflPath string
}

// NewFuzzingFramework creates new framework with specified configuration
func NewFuzzingFramework(config *FuzzingConfig) *FuzzingFramework {
	if config == nil {
		config = DefaultFuzzingConfig("")
	}
	
	fw := &FuzzingFramework{
		config: config,
		campaigns: make([]*FuzzingResult, 0),
		currentStatus: StatusIdle,
		aflPath: getAFLPath(),
	}
	
	return fw
}

// DefaultNewFuzzingFramework returns legacy-compatible instance
func DefaultNewFuzzingFramework() *FuzzingFramework {
	return NewFuzzingFramework(nil)
}

// CheckAFLDependency verifies AFL++ is installed and accessible
func (fw *FuzzingFramework) CheckAFLDependency() error {
	cmd := exec.Command("which", "afl-fuzz")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("afl-fuzz not found in PATH, please install afl++: %w", err)
	}
	return nil
}

// setAFLPath locates AFL++ installation directory
func getAFLPath() string {
	aflPaths := []string{
		os.Getenv("AFL_PATH"),
		"/usr/local/bin",
		"/usr/bin",
		"C:\\Program Files\\AFLplusplus",
		"$HOME/.local/bin",
	}
	
	for _, path := range aflPaths {
		if path == "" || path[0] == '$' {
			continue
		}
		
		path = strings.ReplaceAll(path, "$HOME", os.Getenv("HOME"))
		path = strings.TrimSuffix(path, "/")
		
		if _, err := os.Stat(filepath.Join(path, "afl-fuzz")); err == nil {
			return path
		}
	}
	
	return ""
}

// CreateFuzzTarget compiles target source code with AFL instrumentation
func (fw *FuzzingFramework) CreateFuzzTarget(sourceFile string, outputBinary string) error {
	if outputBinary == "" {
		basename := strings.TrimSuffix(filepath.Base(sourceFile), filepath.Ext(sourceFile))
		outputBinary = basename + "_fuzz"
	}
	
	cmd := exec.Command(
		filepath.Join(fw.aflPath, "afl-gcc"),
		"-O0",
		"-g",
		"-fno-omit-frame-pointer",
		sourceFile,
		"-o", outputBinary,
	)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compilation failed: %w, output: %s", err, output)
	}
	
	fmt.Printf("✓ Compiled instrumented binary: %s\n", outputBinary)
	return nil
}

// CreateSeedInput creates initial seed corpus entry
func (fw *FuzzingFramework) CreateSeedInput(data []byte, filename string) error {
	inputPath := filepath.Join(fw.config.InputDir, filename)
	
	if err := os.WriteFile(inputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to create seed input: %w", err)
	}
	
	fmt.Printf("✓ Created seed input: %s (%d bytes)\n", filename, len(data))
	return nil
}

// ValidateSeedCorpus checks if seed inputs are valid and suitable for fuzzing
func (fw *FuzzingFramework) ValidateSeedCorpus() error {
	files, err := os.ReadDir(fw.config.InputDir)
	if err != nil {
		return fmt.Errorf("read input directory: %w", err)
	}
	
	if len(files) == 0 {
		return fmt.Errorf("no seed inputs found in %s", fw.config.InputDir)
	}
	
	validCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		
		info, err := file.Info()
		if err != nil {
			continue
		}
		
		if info.Size() > 0 {
			validCount++
		}
	}
	
	if validCount == 0 {
		return fmt.Errorf("all seed inputs are empty")
	}
	
	fmt.Printf("✓ Validated %d seed inputs\n", validCount)
	return nil
}

// RunFuzzing executes comprehensive AFL++ fuzzing campaign
func (fw *FuzzingFramework) RunFuzzing(durationMinutes int) (*FuzzingResult, error) {
	if durationMinutes <= 0 {
		durationMinutes = 60 // Default 1 hour
	}
	
	if err := fw.CheckAFLDependency(); err != nil {
		return nil, err
	}
	
	if err := fw.ValidateSeedCorpus(); err != nil {
		return nil, err
	}
	
	if err := fw.CreateFuzzTargetFromBinary(fw.config.BinaryPath); err != nil {
		fmt.Printf("Warning: %v, skipping binary compilation\n", err)
	}
	
	result := &FuzzingResult{
		CampaignName: fmt.Sprintf("campaign_%s", time.Now().Format("20060102_150405")),
		StartTime:    time.Now(),
		Status:       StatusRunning,
		Config:       fw.config,
		Metrics:      make(map[string]interface{}),
	}
	
	fw.currentStatus = StatusRunning
	
	cmd := exec.Command(
		filepath.Join(fw.aflPath, "afl-fuzz"),
		"-i", fw.config.InputDir,
		"-o", fw.config.OutputDir,
		"-t", fmt.Sprintf("%dm", durationMinutes),
		"-T", result.CampaignName,
		"-W", "30s", // Warning if no new paths in 30s
		"-m", "memlimit", "1024", // Memory limit 1GB
		"-Q", // Queue mode for stability
		"./target_fuzz",
		"@input",
	)
	
	cmd.Dir = "."
	outputChan := make(chan string, 100)
	
	go func() {
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		
		scannerOut := bufio.NewScanner(stdout)
		scannerErr := bufio.NewScanner(stderr)
		
		go func() {
			for scannerOut.Scan() {
				outputChan <- scannerOut.Text()
			}
		}()
		
		go func() {
			for scannerErr.Scan() {
				outputChan <- scannerErr.Text()
			}
		}()
	}()
	
	err := cmd.Start()
	if err != nil {
		fw.currentStatus = StatusFailed
		result.Status = StatusFailed
		return result, fmt.Errorf("failed to start fuzzing: %w", err)
	}
	
	doneChan := make(chan error, 1)
	go func() {
		doneChan <- cmd.Wait()
	}()
	
	timeout := time.Duration(durationMinutes) * time.Minute
	select {
	case err := <-doneChan:
		close(outputChan)
		result.EndTime = time.Now()
		result.Status = StatusComplete
		
		if err != nil && err.Error() != "exit status 1" {
			result.Status = StatusFailed
		}
		
	case <-time.After(timeout):
		close(outputChan)
		result.EndTime = time.Now()
		result.Status = StatusComplete
		cmd.Process.Kill()
	}
	
	fw.currentStatus = StatusIdle
	fw.campaigns = append(fw.campaigns, result)
	return result, nil
}

// CreateFuzzTargetFromBinary compiles existing binary with AFL instrumentation
func (fw *FuzzingFramework) CreateFuzzTargetFromBinary(binaryPath string) error {
	if !fileExists(binaryPath) {
		return fmt.Errorf("binary not found: %s", binaryPath)
	}
	
	outputPath := "target_fuzz"
	cmd := exec.Command(
		filepath.Join(fw.aflPath, "afl-clang-fast"),
		"-O0",
		"-g",
		"-fno-omit-frame-pointer",
		"-lm",
		binaryPath,
		"-o", outputPath,
	)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("instrumentation failed: %w, output: %s", err, output)
	}
	
	fmt.Printf("✓ Instrumented binary: %s\n", outputPath)
	return nil
}

// AnalyzeCrashes scans fuzzing output directory for discovered crashes
func (fw *FuzzingFramework) AnalyzeCrashes(campaignDir string) ([]VulnerabilityReport, error) {
	crashDir := filepath.Join(campaignDir, "crashes")
	
	if _, err := os.Stat(crashDir); os.IsNotExist(err) {
		return []VulnerabilityReport{}, nil
	}
	
	reports := []VulnerabilityReport{}
	files, err := os.ReadDir(crashDir)
	if err != nil {
		return nil, fmt.Errorf("read crash directory: %w", err)
	}
	
	for i, file := range files {
		report := fw.analyzeSingleCrash(crashDir, file, i)
		if report != nil {
			reports = append(reports, *report)
		}
	}
	
	return reports, nil
}

// analyzeSingleCrash extracts details from individual crash input
func (fw *FuzzingFramework) analyzeSingleCrash(crashDir string, file os.DirEntry, index int) *VulnerabilityReport {
	crashPath := filepath.Join(crashDir, file.Name())
	
	data, err := os.ReadFile(crashPath)
	if err != nil {
		return nil
	}
	
	// Determine crash type by examining symptoms
	crashType := fw.classifyCrashType(data)
	severity := fw.estimateSeverity(crashType)
	
	report := &VulnerabilityReport{
		ID:          fmt.Sprintf("crash_%d_%s", index, file.Name()),
		CrashType:   crashType,
		Severity:    severity,
		InputPath:   crashPath,
		InputData:   base64Encode(string(data)),
		Description: fmt.Sprintf("Discovered %s via AFL++ fuzzing", crashType),
		RiskLevel:   getRiskLevel(severity),
		Recommendation: recommendMitigation(crashType),
		FindingTime: time.Now().Format(time.RFC3339),
	}
	
	return report
}

// classifyCrashType determines the nature of crash based on behavior patterns
func (fw *FuzzingFramework) classifyCrashType(inputData []byte) CrashType {
	dataStr := string(inputData)
	
	// Heuristic-based classification
	if containsSensitiveString(dataStr, "segfault|segmentation fault|core dumped") {
		return TypeSegmentationFault
	}
	if containsSensitiveString(dataStr, "stack smashing detected") {
		return TypeStackSmashing
	}
	if containsSensitiveString(dataStr, "out of memory|oom|malloc failed") {
		return TypeOutOfMemory
	}
	if containsSensitiveString(dataStr, "timeout|timed out") {
		return TypeTimeout
	}
	if containsSensitiveString(dataStr, "assertion failed|abort") {
		return TypeAssertionFailure
	}
	
	return TypeUnknown
}

// estimateSeverity estimates vulnerability severity from crash type
func (fw *FuzzingFramework) estimateSeverity(crashType CrashType) Severity {
	severityMap := map[CrashType]Severity{
		TypeSegmentationFault:  Critical,
		TypeStackSmashing:      Critical,
		TypeAssertionFailure:   High,
		TypeOutOfMemory:        Medium,
		TypeTimeout:            Low,
		TypeUndefinedBehavior:  Medium,
		TypeUnknown:            Medium,
	}
	
	if sev, ok := severityMap[crashType]; ok {
		return sev
	}
	return Medium
}

// analyzeCoverageMetrics collects coverage information from fuzzing campaign
func (fw *FuzzingFramework) analyzeCoverageMetrics(campaignDir string) map[string]interface{} {
	metrics := make(map[string]interface{})
	
	queuePath := filepath.Join(campaignDir, "queue")
	favPath := filepath.Join(campaignDir, "fav")
	
	if entries, err := os.ReadDir(queuePath); err == nil {
		metrics["queued_inputs"] = len(entries)
	}
	
	if entries, err := os.ReadDir(favPath); err == nil {
		metrics["favorite_inputs"] = len(entries)
	}
	
	cyclePath := filepath.Join(campaignDir, "cycycles")
	if stats, err := os.ReadFile(cyclePath); err == nil {
		metrics["cycles_complete"] = parseNumericContent(string(stats))
	}
	
	return metrics
}

// generateCoverageReport produces human-readable coverage analysis
func (fw *FuzzingFramework) GenerateCoverageReport(campaignDir string, reportPath string) error {
	metrics := fw.analyzeCoverageMetrics(campaignDir)
	
	content := fmt.Sprintf("=== Fuzzing Coverage Report ===\n\n")
	content += fmt.Sprintf("Campaign: %s\n", filepath.Base(campaignDir))
	content += fmt.Sprintf("Timestamp: %s\n", time.Now().Format(time.RFC3339))
	content += "\n--- Metrics ---\n"
	
	for key, value := range metrics {
		content += fmt.Sprintf("%s: %v\n", key, value)
	}
	
	content += "\n--- Recommendations ---\n"
	if queued, ok := metrics["queued_inputs"].(int); ok && queued < 10 {
		content += "• Few queued inputs - consider adding more diverse seeds\n"
	}
	if fav, ok := metrics["favorite_inputs"].(int); ok && fav < 5 {
		content += "• Low favorite count - may indicate poor coverage diversity\n"
	}
	
	if err := os.WriteFile(reportPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	
	fmt.Printf("✓ Generated coverage report: %s\n", reportPath)
	return nil
}

// GetFuzzingHistory returns historical campaign results
func (fw *FuzzingFramework) GetFuzzingHistory() []*FuzzingResult {
	return copyFuzzingResults(fw.campaigns)
}

// ClearCampaigns removes all campaign history
func (fw *FuzzingFramework) ClearCampaigns() {
	fw.campaigns = fw.campaigns[:0]
}

// GetCurrentStatus returns current fuzzing status
func (fw *FuzzingFramework) GetCurrentStatus() FuzzingStatus {
	return fw.currentStatus
}

// Helper functions

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func containsSensitiveString(text, pattern string) bool {
	re, _ := regexp.Compile(pattern)
	return re.MatchString(text)
}

func base64Encode(data string) string {
	// Simplified - would use encoding/base64 in real implementation
	if len(data) > 100 {
		return data[:100] + "... [truncated]"
	}
	return data
}

func parseNumericContent(content string) int {
	pattern := regexp.MustCompile(`\d+`)
	matches := pattern.FindStringSubmatch(content)
	if len(matches) > 0 {
		if num, err := strconv.Atoi(matches[0]); err == nil {
			return num
		}
	}
	return 0
}

func getRiskLevel(severity Severity) string {
	riskMap := map[Severity]string{
		Critical: "Critical vulnerability requires immediate remediation",
		High:     "High severity issue should be prioritized",
		Medium:   "Medium severity warrants investigation",
		Low:      "Low severity may require attention but non-urgent",
		Info:     "Informational finding for awareness",
	}
	if risk, ok := riskMap[severity]; ok {
		return risk
	}
	return "Assessment pending"
}

func recommendMitigation(crashType CrashType) string {
	recMap := map[CrashType]string{
		TypeSegmentationFault:  "Add bounds checking and validate input lengths before memory access",
		TypeStackSmashing:      "Enable stack canaries (-fstack-protector) and fix buffer overflow root cause",
		TypeOutOfMemory:        "Implement proper memory limits and graceful degradation on allocation failure",
		TypeTimeout:            "Add request timeouts and ensure efficient processing of all inputs",
		TypeAssertionFailure:   "Replace assertions with proper error handling for production code",
		TypeUndefinedBehavior:  "Eliminate undefined behavior by following C/C++ language specifications",
		TypeUnknown:            "Investigate crash symptoms and add targeted defensive measures",
	}
	if rec, ok := recMap[crashType]; ok {
		return rec
	}
	return "Perform detailed crash analysis and implement appropriate safeguards"
}

// Deep cloning helper functions
func copyFuzzingResults(src []*FuzzingResult) []*FuzzingResult {
	dst := make([]*FuzzingResult, len(src))
	copy(dst, src)
	return dst
}
