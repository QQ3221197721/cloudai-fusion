// Package common provides shared types, utilities, and interfaces
// used across all CloudAI Fusion components.
package common

import (
	"fmt"
	"time"
)

// ============================================================================
// Workload Generation Utilities
// ============================================================================

// GenerateSequentialWorkloads creates N sequential workloads for trace chain testing
func GenerateSequentialWorkloads(baseID string, count int) ([]BenchmarkWorkload, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be positive, got %d", count)
	}
	
	workloads := make([]BenchmarkWorkload, count)
	
	for i := 0; i < count; i++ {
		wl := NewSimpleBenchmarkWorkload(
			fmt.Sprintf("%s-%d", baseID, i),
			"sequential-chain",
		)
		
		if i > 0 {
			parentID := workloads[i-1].ID()
			wl.Metadata()["parent_id"] = parentID
			wl.Metadata()["chain_index"] = i
		}
		
		workloads[i] = wl
	}
	
	return workloads, nil
}

// GenerateParallelWorkloads creates N parallel workloads sharing same parent
func GenerateParallelWorkloads(parentID string, count int) ([]BenchmarkWorkload, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be positive, got %d", count)
	}
	
	workloads := make([]BenchmarkWorkload, count)
	
	for i := 0; i < count; i++ {
		wl := NewSimpleBenchmarkWorkload(
			fmt.Sprintf("%s-parallel-%d", parentID, i),
			"parallel-batch",
		)
		
		wl.Metadata()["parent_id"] = parentID
		wl.Metadata()["group_index"] = i
		
		workloads[i] = wl
	}
	
	return workloads, nil
}

// GenerateFaultChain simulates cascading faults through dependent workloads
func GenerateFaultChain(workloads []BenchmarkWorkload, propagate bool) error {
	if len(workloads) == 0 {
		return fmt.Errorf("cannot generate fault chain from empty workload list")
	}
	
	// Inject fault into first workload
	err := workloads[0].InjectFault(FaultCPUOverload, 0.8)
	if err != nil {
		return err
	}
	
	if !propagate {
		return nil
	}
	
	// Propagate cascade through dependency chain
	for i := 1; i < len(workloads); i++ {
		meta := workloads[i].Metadata()
		if parentID, ok := meta["parent_id"].(string); ok {
			// Find parent in list and inject fault there too
			for _, parent := range workloads[:i] {
				if parent.ID() == parentID {
					// Inject fault into parent with escalating severity
					newSeverity := 0.8 + float64(i)*0.05
					if newSeverity > 1.0 {
						newSeverity = 1.0
					}
					
					err = parent.InjectFault(FaultCPUOverload, newSeverity)
					if err != nil {
						break
					}
				}
			}
		}
		// Also inject fault into the current workload itself
		err = workloads[i].InjectFault(FaultCPUOverload, 0.8)
		if err != nil {
			break
		}
	}
	
	return nil
}

// ============================================================================
// Workload Analysis Functions
// ============================================================================

// CountWorkloadsWithFault returns number of workloads with injected faults
func CountWorkloadsWithFault(workloads []BenchmarkWorkload) int {
	count := 0
	for _, wl := range workloads {
		meta := wl.Metadata()
		if val, ok := meta["fault_injected"]; ok && val == true {
			count++
		}
	}
	return count
}

// ExtractWorkloadIDs returns slice of all workload IDs
func ExtractWorkloadIDs(workloads []BenchmarkWorkload) []string {
	ids := make([]string, len(workloads))
	for i, wl := range workloads {
		ids[i] = wl.ID()
	}
	return ids
}

// FilterWorkloadsByProfile returns workloads matching specific profile
func FilterWorkloadsByProfile(workloads []BenchmarkWorkload, profile string) []BenchmarkWorkload {
	var filtered []BenchmarkWorkload
	
	for _, wl := range workloads {
		if wl.Profile() == profile {
			filtered = append(filtered, wl)
		}
	}
	
	return filtered
}

// GetWorkloadWithParent returns first workload that matches the given parent ID
func GetWorkloadWithParent(workloads []BenchmarkWorkload, parentID string) BenchmarkWorkload {
	for _, wl := range workloads {
		meta := wl.Metadata()
		if parent, ok := meta["parent_id"].(string); ok && parent == parentID {
			return wl
		}
	}
	return nil
}

// CalculateTotalDuration computes expected total duration for a batch of workloads
func CalculateTotalDuration(workloads []BenchmarkWorkload) time.Duration {
	total := time.Duration(0)
	for _, wl := range workloads {
		total += wl.ExpectedDuration()
	}
	return total
}

// ValidateAllWorkloads checks if all workloads in the list are valid
func ValidateAllWorkloads(workloads []BenchmarkWorkload) error {
	for _, wl := range workloads {
		if err := wl.Validate(); err != nil {
			return fmt.Errorf("workload %s validation failed: %v", wl.ID(), err)
		}
	}
	return nil
}
