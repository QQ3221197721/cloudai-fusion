package scheduler

import (
	"strings"
	"testing"
)

// gpu_sharing_edge_cases_test.go covers boundary conditions for GPU memory
// isolation allocation and nvidia-smi MIG output parsing. These tests run in a
// pure CPU environment (no real GPU / nvidia-smi required).

// TestEdgeCases_EmptyWorkloadID verifies that allocating a memory isolation
// group with an empty workload ID never panics and behaves deterministically.
func TestEdgeCases_EmptyWorkloadID(t *testing.T) {
	mgr := NewGPUSharingManager(GPUSharingConfig{})

	// Seed a healthy GPU memory state so we reach the workload-ID handling path.
	mgr.memoryStates[0] = &GPUMemoryState{
		GPUIndex:         0,
		PhysicalTotalMiB: 8192,
		VirtualUsedMiB:   0,
	}

	// Empty workload ID must not panic; per current business rules a healthy GPU
	// with enough memory yields a valid group.
	group, err := mgr.AllocateMemoryIsolationGroup(0, "", 1024, 500, nil)
	if err != nil {
		t.Logf("empty workload ID returned error (acceptable): %v", err)
		return
	}
	if group == nil {
		t.Fatal("expected non-nil group when allocation succeeds")
	}
	if group.WorkloadID != "" {
		t.Errorf("expected empty WorkloadID to be preserved, got %q", group.WorkloadID)
	}
	t.Logf("empty workload ID allocated group id=%s", group.ID)
}

// TestEdgeCases_PhysicalMemoryZero verifies that allocating on a GPU whose
// physical memory is zero returns a clear error and does not divide by zero.
func TestEdgeCases_PhysicalMemoryZero(t *testing.T) {
	mgr := NewGPUSharingManager(GPUSharingConfig{})

	// GPU with zero physical memory.
	mgr.memoryStates[0] = &GPUMemoryState{
		GPUIndex:         0,
		PhysicalTotalMiB: 0,
		VirtualUsedMiB:   0,
	}

	group, err := mgr.AllocateMemoryIsolationGroup(0, "workload-zero", 2048, 500, nil)
	if err == nil {
		t.Fatalf("expected error when PhysicalTotalMiB=0, got success with group=%v", group)
	}
	if !strings.Contains(err.Error(), "physical memory is zero") {
		t.Errorf("error message should contain %q, got: %v", "physical memory is zero", err)
	}

	// State must remain untouched (no partial mutation on the failure path).
	state := mgr.memoryStates[0]
	if state.VirtualUsedMiB != 0 {
		t.Errorf("VirtualUsedMiB should stay 0 on failed allocation, got %d", state.VirtualUsedMiB)
	}
	if len(state.IsolationGroups) != 0 {
		t.Errorf("no isolation group should be recorded on failure, got %d", len(state.IsolationGroups))
	}
	t.Logf("correctly rejected zero-physical-memory allocation: %v", err)
}

// TestEdgeCases_ParseMIGInstances_Malformed feeds parseMIGInstances a range of
// malformed nvidia-smi outputs (empty, whitespace, short lines) and asserts it
// never panics and returns no bogus instances.
func TestEdgeCases_ParseMIGInstances_Malformed(t *testing.T) {
	cases := []struct {
		name   string
		output string
	}{
		{"empty", ""},
		{"whitespace_only", "   \n\n  \t  \n  "},
		{"blank_lines", "\n\n\n\n"},
		{"no_profile_keyword", "GPU 0 : 1g.5gb Instances 1/7"},
		{"profile_but_no_gb_field", "  GPU  0 Profile  19 : g. some text here"},
		{"truncated_fields", "Profile g."},
		{"garbage", "!@#$%^&*()\n\t<<<>>>\nrandom text without structure"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Must not panic on malformed input.
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("parseMIGInstances panicked on %q: %v", tc.name, r)
				}
			}()

			instances := parseMIGInstances(0, tc.output)
			if len(instances) != 0 {
				t.Errorf("expected no MIG instances from malformed output %q, got %d", tc.name, len(instances))
			}
		})
	}
}

// TestEdgeCases_ParseMIGInstances_Valid confirms that well-formed nvidia-smi
// output still parses correctly after the bounds-check hardening.
func TestEdgeCases_ParseMIGInstances_Valid(t *testing.T) {
	output := "" +
		"  GPU  0 Profile  19 : 1g.5gb  Instances: 1/7\n" +
		"  GPU  0 Profile  14 : 2g.10gb Instances: 1/3\n"

	instances := parseMIGInstances(0, output)
	if len(instances) == 0 {
		t.Fatal("expected at least one MIG instance from valid output")
	}
	for i, inst := range instances {
		if inst.GPUIndex != 0 {
			t.Errorf("instance %d: GPUIndex = %d, want 0", i, inst.GPUIndex)
		}
		if inst.Profile == "" {
			t.Errorf("instance %d: empty Profile", i)
		}
		if inst.MemoryMB <= 0 {
			t.Errorf("instance %d: expected positive MemoryMB, got %d", i, inst.MemoryMB)
		}
	}
	t.Logf("parsed %d valid MIG instances", len(instances))
}
