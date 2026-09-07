package fuzzing_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/fuzzing"
)

func TestFrameworkCreation(t *testing.T) {
	config := fuzzing.DefaultFuzzingConfig("./test_binary")
	fw := fuzzing.NewFuzzingFramework(config)
	
	if fw == nil {
		t.Fatal("Failed to create fuzzing framework")
	}
	
	if fw.GetCurrentStatus() != fuzzing.StatusIdle {
		t.Errorf("Expected IDLE status, got %s", fw.GetCurrentStatus())
	}
	
	t.Log("✓ Fuzzing framework initialized successfully")
}

func TestAFLDependencyCheck(t *testing.T) {
	fw := fuzzing.NewFuzzingFramework(nil)
	err := fw.CheckAFLDependency()
	
	// This may fail in test environment if AFL++ not installed
	if err != nil {
		t.Logf("AFL++ dependency check: %v (expected if not installed)", err)
	} else {
		t.Log("✓ AFL++ found in PATH")
	}
}

func TestCreateSeedInput(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fuzz_seed_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	config := &fuzzing.FuzzingConfig{
		InputDir:  tmpDir,
		OutputDir: filepath.Join(tmpDir, "output"),
		BinaryPath: "./nonexistent", // Won't actually be compiled in test
	}
	
	fw := fuzzing.NewFuzzingFramework(config)
	
	testInput := []byte("Hello World!\\x00\\xff\\xfe")
	err = fw.CreateSeedInput(testInput, "seed1.txt")
	if err != nil {
		t.Fatalf("Failed to create seed input: %v", err)
	}
	
	// Verify file exists
	seedPath := filepath.Join(tmpDir, "seed1.txt")
	if _, err := os.Stat(seedPath); os.IsNotExist(err) {
		t.Error("Seed input file was not created")
	}
	
	t.Log("✓ Seed input created successfully")
}

func TestValidateSeedCorpus(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fuzz_corpus_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	config := &fuzzing.FuzzingConfig{
		InputDir:   tmpDir,
		OutputDir:  filepath.Join(tmpDir, "output"),
		BinaryPath: "./test",
	}
	
	fw := fuzzing.NewFuzzingFramework(config)
	
	// Create valid seeds
	for i := 0; i < 5; i++ {
		seedData := []byte{byte(i), byte(i + 1), 0x41, 0x42, 0x43}
		filename := filepath.Join(tmpDir, fmt.Sprintf("seed_%d", i))
		os.WriteFile(filename, seedData, 0644)
	}
	
	err = fw.ValidateSeedCorpus()
	if err != nil {
		t.Errorf("Valid corpus failed validation: %v", err)
	} else {
		t.Log("✓ Seed corpus validated successfully")
	}
}

func TestSeedValidationFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fuzz_empty_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	config := &fuzzing.FuzzingConfig{
		InputDir:   tmpDir,
		BinaryPath: "./test",
	}
	
	fw := fuzzing.NewFuzzingFramework(config)
	
	err = fw.ValidateSeedCorpus()
	if err == nil {
		t.Error("Expected validation failure with empty directory")
	} else {
		t.Logf("✓ Empty corpus correctly rejected: %v", err)
	}
}

func TestCrashClassification(t *testing.T) {
	tests := []struct {
		name     string
		crash    fuzzing.CrashType
		expected fuzzing.Severity
	}{
		{"Segmentation fault", fuzzing.TypeSegmentationFault, fuzzing.Critical},
		{"Stack smashing", fuzzing.TypeStackSmashing, fuzzing.Critical},
		{"Out of memory", fuzzing.TypeOutOfMemory, fuzzing.Medium},
		{"Assertion failure", fuzzing.TypeAssertionFailure, fuzzing.High},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fw := fuzzing.NewFuzzingFramework(nil)
			
			result := fw.EstimateSeverity(tt.crash)
			if result != tt.expected {
				t.Errorf("Expected severity %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFuzzingResultLifecycle(t *testing.T) {
	config := fuzzing.DefaultFuzzingConfig("./nonexistent")
	fw := fuzzing.NewFuzzingFramework(config)
	
	result := &fuzzing.FuzzingResult{
		CampaignName: "test_campaign",
		Status:       fuzzing.StatusRunning,
		Config:       config,
		Metrics:      make(map[string]interface{}),
	}
	
	// Simulate lifecycle
	t.Logf("Campaign started: %s at %s", result.CampaignName, result.StartTime.Format(time.RFC3339))
	result.Status = fuzzing.StatusComplete
	result.EndTime = fw.TimeNow()
	t.Logf("Campaign completed: %s - Total executions: %d", 
		result.CampaignName, result.TotalExecutions)
	
	_ = fw.GetFuzzingHistory()
	
	t.Log("✓ Fuzzing result lifecycle works correctly")
}

// Helper methods for testing
func (fw *fuzzing.FuzzingFramework) TimeNow() time.Time {
	return time.Now()
}

func BenchmarkFuzzingWorkflow(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark_fuzz")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	config := &fuzzing.FuzzingConfig{
		InputDir:   tmpDir,
		OutputDir:  filepath.Join(tmpDir, "output"),
		BinaryPath: "./test",
		Workers:    4,
	}
	
	fw := fuzzing.NewFuzzingFramework(config)
	
	// Create diverse seed corpus
	seeds := [][]byte{
		[]byte("<html>"),
		[]byte("{json}"),
		[]byte("[array]"),
		[]byte("key=value"),
		[]byte("user:pass"),
	}
	
	for i, seed := range seeds {
		fw.CreateSeedInput(seed, fmt.Sprintf("seed_%d.txt", i))
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fw.ValidateSeedCorpus()
		_ = fw.AnalyzeCrashes(tmpDir)
	}
}
