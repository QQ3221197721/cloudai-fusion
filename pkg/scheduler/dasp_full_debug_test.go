package scheduler

import (
	"fmt"
	"testing"
)

// TestDASPOnesThenSevensFullDebug - full debug like the moat test
func TestDASPOnesThenSevensFullDebug(t *testing.T) {
	N := 8
	
	workload := OnesThenSevens(N)
	distro := map[string]float64{"1g.10gb": 0.5, "7g.80gb": 0.5}
	
	// Check distribution
	largeFraction := computeLargeRequestFraction(distro)
	t.Logf("largeFraction = %.4f, tau = 0.50, will use segregation: %v", largeFraction, largeFraction >= 0.50)
	
	// Now run exactly like the moat test does
	gpus := deepCopyCluster(NewGPUTopology(N))
	m := runSingleSimulation(gpus, workload, DemandAwareSegregationPlacement{}, distro)
	
	t.Logf("\n=== DASP Results ===")
	t.Logf("Accept Count: %d / %d total jobs", m.acceptCount, len(workload))
	for profileName, count := range m.profileAccepts {
		t.Logf("  %s: %d accepts", profileName, count)
	}
	
	if m.acceptCount == 0 {
		t.Log("DEBUG: All jobs failed - checking if this is a capacity issue...")
		sched := NewMIGScheduler(deepCopyCluster(NewGPUTopology(N)), distro)
		
		for i, job := range workload {
			_, err := sched.Schedule(fmt.Sprintf("w-%d", i), job.Name, DemandAwareSegregationPlacement{})
			if err == nil {
				t.Logf("  Job[%d] %s ACCEPTED", i, job.Name)
			} else {
				t.Logf("  Job[%d] %s FAILED: %v", i, job.Name, err)
			}
		}
	} else {
		t.Logf("\n✓ SUCCESS: DASP accepted %d jobs!", m.acceptCount)
	}
}
