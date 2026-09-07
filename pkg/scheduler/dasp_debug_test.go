package scheduler

import (
	"fmt"
	"testing"
)

// Debug DASP's handling of OnesThenSevens workload
func TestDASPOnesThenSevensDebug(t *testing.T) {
	N := 8
	
	workload := OnesThenSevens(N)
	t.Logf("=== OnesThenSevens(N=%d) Workload ===", N)
	t.Logf("Total jobs: %d", len(workload))
	for i, job := range workload {
		if i < 10 || i >= len(workload)-5 {
			t.Logf("  Job[%d]: %s (size=%d)", i, job.Name, job.Size)
		}
	}
	
	// Check distribution
	distro := map[string]float64{"1g.10gb": 0.5, "7g.80gb": 0.5}
	largeFraction := computeLargeRequestFraction(distro)
	rhoRaw := computeReservationRatio(distro)
	
	t.Logf("\nDistribution Analysis:")
	t.Logf("  Large fraction (count-based): %.4f", largeFraction)
	t.Logf("  Reservation ratio (slice-weighted): %.4f", rhoRaw)
	t.Logf("  Tau threshold: 0.50")
	
	const tau = 0.50
	if largeFraction <= tau {
		t.Log("→ DASP will use HAMi-style spreading (largeFraction ≤ τ)")
	} else {
		cappedRho := rhoRaw
		if cappedRho > 0.625 {
			cappedRho = 0.625
		}
		R := int(rounded(cappedRho * float64(8))) // 8 GPUs
		t.Logf("→ DASP will use segregation: R=%d GPUs for large zone", R)
		t.Logf("  Capped rho: %.4f → R=%d", cappedRho, R)
	}
	
	// Now run actual scheduling with DASP
	gpus := NewGPUTopology(N)
	sched := NewMIGScheduler(gpus, distro)
	
	var accepted int
	for i, job := range workload {
		_, err := sched.Schedule(fmt.Sprintf("w-%d", i), job.Name, DemandAwareSegregationPlacement{})
		if err == nil {
			accepted++
			t.Logf("✓ Job[%d] %s ACCEPTED", i, job.Name)
		} else {
			t.Logf("✗ Job[%d] %s FAILED: %v", i, job.Name, err)
		}
	}
	
	t.Logf("\nFinal: Accepted=%d/%d", accepted, len(workload))
	if accepted == 0 {
		t.Fatal("BUG CONFIRMED: DASP rejected ALL jobs!")
	}
}

// Helper function
func rounded(x float64) int {
	if x < 0 {
		return int(x - 0.5)
	}
	return int(x + 0.5)
}
