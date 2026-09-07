package common

import (
	"strconv"
	"testing"
	"time"
)

// ============================================================================
// SimpleBenchmarkWorkload Tests
// ============================================================================

func TestSimpleBenchmarkWorkload(t *testing.T) {
	wl := NewSimpleBenchmarkWorkload("test-1", "cpu-heavy")
	
	if wl.ID() != "test-1" {
		t.Errorf("ID mismatch: expected 'test-1', got '%s'", wl.ID())
	}
	
	if wl.Profile() != "cpu-heavy" {
		t.Errorf("Profile mismatch: expected 'cpu-heavy', got '%s'", wl.Profile())
	}
	
	if wl.Priority() != 50 {
		t.Errorf("Default priority wrong: expected 50, got %d", wl.Priority())
	}
	
	// Test validation
	if err := wl.Validate(); err != nil {
		t.Fatalf("Unexpected validation error: %v", err)
	}
	
	// Test invalid ID
	invalidWl := &SimpleBenchmarkWorkload{id: "", profile: "test"}
	if err := invalidWl.Validate(); err == nil {
		t.Error("Expected validation error for empty ID")
	}
	
	// Test fault injection
	err := wl.InjectFault(FaultCPUOverload, 0.75)
	if err != nil {
		t.Fatalf("Failed to inject fault: %v", err)
	}
	
	if val, ok := wl.Metadata()["fault_injected"]; !ok || val != true {
		t.Error("Expected fault_injected flag in metadata")
	}
	
	// Test invalid severity
	err = wl.InjectFault(FaultCPUOverload, 1.5)
	if err == nil {
		t.Error("Expected error for severity > 1.0")
	}
}

func TestSimpleBenchmarkWorkload_MultipleFaults(t *testing.T) {
	wl := NewSimpleBenchmarkWorkload("multi-fault", "memory-heavy")
	
	// Inject multiple faults
	err := wl.InjectFault(FaultMemoryLeak, 0.6)
	if err != nil {
		t.Fatalf("First fault injection failed: %v", err)
	}
	
	err = wl.InjectFault(FaultNetworkLatency, 0.9)
	if err != nil {
		t.Fatalf("Second fault injection failed: %v", err)
	}
	
	// Verify both faults can be injected
	faultCount := CountWorkloadsWithFault([]BenchmarkWorkload{wl})
	if faultCount != 1 {
		t.Errorf("Expected 1 workload with faults, got %d", faultCount)
	}
}

// ============================================================================
// BatchBenchmarkWorkload Tests
// ============================================================================

func TestBatchBenchmarkWorkload(t *testing.T) {
	bw := NewBatchBenchmarkWorkload("parent-1", 10, "batch-processing")
	
	expectedID := "parent-1-batch-10"
	if bw.ID() != expectedID {
		t.Errorf("ID mismatch: expected '%s', got '%s'", expectedID, bw.ID())
	}
	
	if bw.Profile() != "batch-processing" {
		t.Errorf("Profile mismatch: expected 'batch-processing', got '%s'", bw.Profile())
	}
	
	if bw.Priority() != 100 {
		t.Errorf("Priority should be 100 for batch workloads, got %d", bw.Priority())
	}
	
	// Validate batch metadata
	meta := bw.Metadata()
	if meta["parent_id"] != "parent-1" {
		t.Error("Expected parent_id in metadata")
	}
	if meta["batch_size"] != 10 {
		t.Error("Expected batch_size of 10 in metadata")
	}
	
	// Test validation
	invalidBW := NewBatchBenchmarkWorkload("", 5, "test")
	if err := invalidBW.Validate(); err == nil {
		t.Error("Expected validation error for empty parent ID")
	}
	
	invalidBW2 := NewBatchBenchmarkWorkload("valid-parent", 0, "test")
	if err := invalidBW2.Validate(); err == nil {
		t.Error("Expected validation error for zero count")
	}
	
	validBW := NewBatchBenchmarkWorkload("valid-parent", 1, "test")
	if err := validBW.Validate(); err != nil {
		t.Fatalf("Unexpected validation error: %v", err)
	}
}

// ============================================================================
// GenerateSequentialWorkloads Tests
// ============================================================================

func TestGenerateSequentialWorkloads(t *testing.T) {
	workloads, err := GenerateSequentialWorkloads("chain", 5)
	
	if err != nil {
		t.Fatalf("Unexpected error generating workloads: %v", err)
	}
	
	if len(workloads) != 5 {
		t.Fatalf("Expected 5 workloads, got %d", len(workloads))
	}
	
	// Verify ID format
	expectedPrefix := "chain"
	for i, wl := range workloads {
		expectedID := expectedPrefix + "-" + strconv.Itoa(i)
		if wl.ID() != expectedID {
			t.Errorf("Workload %d ID mismatch: expected '%s', got '%s'", 
				i, expectedID, wl.ID())
		}
	}
	
	// Verify parent-child relationships
	for i := 1; i < len(workloads); i++ {
		parentID := workloads[i].Metadata()["parent_id"]
		if parentID != workloads[i-1].ID() {
			t.Errorf("Parent-child link broken at index %d: parent=%v, actual_parent=%s", 
				i, parentID, workloads[i-1].ID())
		}
	}
}

func TestGenerateSequentialWorkloads_EmptyList(t *testing.T) {
	workloads, err := GenerateSequentialWorkloads("empty", 0)
	if err == nil {
		t.Error("Expected error for count=0")
	}
	if workloads != nil {
		t.Error("Expected nil workloads for count=0")
	}
}

func TestGenerateSequentialWorkloads_NegativeCount(t *testing.T) {
	workloads, err := GenerateSequentialWorkloads("negative", -5)
	if err == nil {
		t.Error("Expected error for negative count")
	}
	if workloads != nil {
		t.Error("Expected nil workloads for negative count")
	}
}

// ============================================================================
// GenerateParallelWorkloads Tests
// ============================================================================

func TestGenerateParallelWorkloads(t *testing.T) {
	workloads, err := GenerateParallelWorkloads("parallel-root", 3)
	
	if err != nil {
		t.Fatalf("Unexpected error generating parallel workloads: %v", err)
	}
	
	if len(workloads) != 3 {
		t.Fatalf("Expected 3 workloads, got %d", len(workloads))
	}
	
	// All should share the same parent
	parentID := "parallel-root"
	for i, wl := range workloads {
		meta := wl.Metadata()
		if meta == nil {
			t.Errorf("Workload %d metadata is nil!", i)
			continue
		}
		if meta["parent_id"] != parentID {
			t.Errorf("Workload %d has wrong parent_id: %v", i, meta["parent_id"])
		}
		
		if _, ok := meta["group_index"]; !ok {
			t.Errorf("Workload %d missing group_index", i)
		}
	}
}

// ============================================================================
// Fault Chain Generation Tests
// ============================================================================

func TestGenerateFaultChain(t *testing.T) {
	workloads, _ := GenerateSequentialWorkloads("fault-test", 3)
	
	err := GenerateFaultChain(workloads, true)
	if err != nil {
		t.Fatalf("Failed to inject fault chain: %v", err)
	}
	
	faultyCount := CountWorkloadsWithFault(workloads)
	if faultyCount != 3 {
		t.Errorf("Expected 3 workloads with faults, got %d", faultyCount)
	}
}

func TestGenerateFaultChain_NoPropagation(t *testing.T) {
	workloads, _ := GenerateSequentialWorkloads("no-propagate", 5)
	
	err := GenerateFaultChain(workloads, false)
	if err != nil {
		t.Fatalf("Failed to inject single fault: %v", err)
	}
	
	faultyCount := CountWorkloadsWithFault(workloads)
	if faultyCount != 1 {
		t.Errorf("Expected 1 workload with fault (no propagation), got %d", faultyCount)
	}
}

func TestGenerateFaultChain_EmptyList(t *testing.T) {
	err := GenerateFaultChain([]BenchmarkWorkload{}, true)
	if err == nil {
		t.Error("Expected error for empty workload list")
	}
}

// ============================================================================
// Workload Analysis Function Tests
// ============================================================================

func TestExtractWorkloadIDs(t *testing.T) {
	workloads, _ := GenerateSequentialWorkloads("ids-test", 3)
	
	ids := ExtractWorkloadIDs(workloads)
	
	if len(ids) != 3 {
		t.Errorf("Expected 3 IDs, got %d", len(ids))
	}
	
	expectedIDs := []string{"ids-test-0", "ids-test-1", "ids-test-2"}
	for i, expected := range expectedIDs {
		if ids[i] != expected {
			t.Errorf("ID[%d] mismatch: expected '%s', got '%s'", i, expected, ids[i])
		}
	}
}

func TestFilterWorkloadsByProfile(t *testing.T) {
	// Create mixed workloads
	wl1 := NewSimpleBenchmarkWorkload("wl-1", "cpu-heavy")
	wl2 := NewSimpleBenchmarkWorkload("wl-2", "gpu-intensive")
	wl3 := NewSimpleBenchmarkWorkload("wl-3", "cpu-heavy")
	wl4 := NewSimpleBenchmarkWorkload("wl-4", "memory-heavy")
	
	allWorkloads := []BenchmarkWorkload{wl1, wl2, wl3, wl4}
	
	// Filter by CPU heavy
	cpuWorkloads := FilterWorkloadsByProfile(allWorkloads, "cpu-heavy")
	if len(cpuWorkloads) != 2 {
		t.Errorf("Expected 2 cpu-heavy workloads, got %d", len(cpuWorkloads))
	}
	
	// Filter by GPU intensive
	gpuWorkloads := FilterWorkloadsByProfile(allWorkloads, "gpu-intensive")
	if len(gpuWorkloads) != 1 {
		t.Errorf("Expected 1 gpu-intensive workload, got %d", len(gpuWorkloads))
	}
	
	// Filter non-existent profile
	noneWorkloads := FilterWorkloadsByProfile(allWorkloads, "non-existent")
	if len(noneWorkloads) != 0 {
		t.Errorf("Expected 0 workloads for non-existent profile, got %d", len(noneWorkloads))
	}
}

func TestGetWorkloadWithParent(t *testing.T) {
	parentWl := NewSimpleBenchmarkWorkload("parent-1", "root")
	childWl := NewSimpleBenchmarkWorkload("child-1", "child")
	childWl.Metadata()["parent_id"] = "parent-1"
	grandChildWl := NewSimpleBenchmarkWorkload("grandchild-1", "grandchild")
	grandChildWl.Metadata()["parent_id"] = "child-1"
	
	workloads := []BenchmarkWorkload{parentWl, childWl, grandChildWl}
	
	// Find child by parent
	found := GetWorkloadWithParent(workloads, "parent-1")
	if found == nil {
		t.Error("Expected to find child workload with parent 'parent-1'")
	} else if found.ID() != "child-1" {
		t.Errorf("Expected to find 'child-1', got '%s'", found.ID())
	}
	
	// Find grandchild
	found = GetWorkloadWithParent(workloads, "child-1")
	if found == nil {
		t.Error("Expected to find grandchild workload with parent 'child-1'")
	} else if found.ID() != "grandchild-1" {
		t.Errorf("Expected to find 'grandchild-1', got '%s'", found.ID())
	}
	
	// Non-existent parent
	noneFound := GetWorkloadWithParent(workloads, "non-existent")
	if noneFound != nil {
		t.Error("Expected nil for non-existent parent")
	}
}

func TestCalculateTotalDuration(t *testing.T) {
	wl1 := NewSimpleBenchmarkWorkload("wl-1", "test1")
	wl1.duration = time.Second * 30
	
	wl2 := NewSimpleBenchmarkWorkload("wl-2", "test2")
	wl2.duration = time.Minute * 2 // 120 seconds
	
	wl3 := NewSimpleBenchmarkWorkload("wl-3", "test3")
	wl3.duration = time.Minute * 1 // 60 seconds
	
	totalDuration := CalculateTotalDuration([]BenchmarkWorkload{wl1, wl2, wl3})
	expectedDuration := time.Second * 30 + time.Minute*2 + time.Minute*1
	
	if totalDuration != expectedDuration {
		t.Errorf("Total duration mismatch: expected %v, got %v", 
			expectedDuration, totalDuration)
	}
}

func TestValidateAllWorkloads(t *testing.T) {
	validWl1 := NewSimpleBenchmarkWorkload("valid-1", "test")
	validWl2 := NewSimpleBenchmarkWorkload("valid-2", "test")
	
	// All valid
	err := ValidateAllWorkloads([]BenchmarkWorkload{validWl1, validWl2})
	if err != nil {
		t.Fatalf("Unexpected validation error for valid workloads: %v", err)
	}
	
	// One invalid
	invalidWl := &SimpleBenchmarkWorkload{id: "", profile: "invalid"}
	err = ValidateAllWorkloads([]BenchmarkWorkload{validWl1, invalidWl})
	if err == nil {
		t.Error("Expected validation error when one workload is invalid")
	}
}
