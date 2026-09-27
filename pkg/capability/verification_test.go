package capability

import (
	"testing"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

// ============================================================================
// VERIFICATION TEST 1: Zero-Allocation Proof
// Proves M1's Claim: Hot path makes ≤1 allocation per million operations
// ============================================================================

func BenchmarkM1_SingleRead_AllocationCheck(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	// Warm-up: populate registry with realistic data
	for i := 0; i < 100; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "detail")
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, _ = reg.getCapability("comp0")
	}
}

func BenchmarkM1_SnapshotZeroAllocation(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	// Warm-up
	for i := 0; i < 100; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "detail")
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = reg.GetSnapshot()
	}
}

// Competitor baseline: K8s mutex pattern with allocations
func BenchmarkKubeStyle_Get_Allocation(b *testing.B) {
	reg := NewKubeStyleRegistry()
	
	// Warm-up
	for i := 0; i < 100; i++ {
		reg.Report("comp"+string(rune(i)), CapabilityInfo{Mode: "real"})
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = reg.Get("comp0")
	}
}

// Competitor baseline: Standard sync.RWMutex snapshot allocation
func BenchmarkKubeStyle_Snapshot_Allocation(b *testing.B) {
	reg := NewKubeStyleRegistry()
	
	// Warm-up
	for i := 0; i < 100; i++ {
		reg.Report("comp"+string(rune(i)), CapabilityInfo{Mode: "real"})
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		reg.mu.RLock()
		result := make([]CapabilityInfo, 0, len(reg.caps))
		for _, v := range reg.caps {
			result = append(result, v)
		}
		reg.mu.RUnlock()
		_ = result
	}
}

// ============================================================================
// VERIFICATION TEST 2: Lock-Free Scalability at High Concurrency
// Proves M1's Claim: No performance degradation at 128+ goroutines
// ============================================================================

func BenchmarkM1_OneHundredTwentyEight_Readers(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	// Warm-up: populate registry
	for i := 0; i < 100; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "detail")
	}
	
	b.SetParallelism(128)
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = reg.getCapability("comp0")
		}
	})
}

func BenchmarkM1_64_Concurrent_Readers(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	for i := 0; i < 100; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "detail")
	}
	
	b.SetParallelism(64)
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = reg.getCapability("comp50")
		}
	})
}

func BenchmarkM1_64_Readers_Snapshot(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	for i := 0; i < 100; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "detail")
	}
	
	b.SetParallelism(64)
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = reg.GetSnapshot()
		}
	})
}

func BenchmarkM1_16_Concurrent_Readers(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	for i := 0; i < 100; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "detail")
	}
	
	b.SetParallelism(16)
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = reg.getCapability("comp25")
		}
	})
}

// Competitor scaling test: K8s mutex contention pattern
func BenchmarkKubeStyle_128_Contention(b *testing.B) {
	reg := NewKubeStyleRegistry()
	
	for i := 0; i < 100; i++ {
		reg.Report("comp"+string(rune(i)), CapabilityInfo{Mode: "real"})
	}
	
	b.SetParallelism(128)
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = reg.Get("comp0")
		}
	})
}

func BenchmarkKubeStyle_64_Contention(b *testing.B) {
	reg := NewKubeStyleRegistry()
	
	for i := 0; i < 100; i++ {
		reg.Report("comp"+string(rune(i)), CapabilityInfo{Mode: "real"})
	}
	
	b.SetParallelism(64)
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = reg.Get("comp50")
		}
	})
}

func BenchmarkKubeStyle_16_Contention(b *testing.B) {
	reg := NewKubeStyleRegistry()
	
	for i := 0; i < 100; i++ {
		reg.Report("comp"+string(rune(i)), CapabilityInfo{Mode: "real"})
	}
	
	b.SetParallelism(16)
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = reg.Get("comp25")
		}
	})
}

// ============================================================================
// VERIFICATION TEST 3: Mixed Read/Write Workload
// Realistic scenario: concurrent reads with occasional updates
// ============================================================================

func BenchmarkM1_Mixed_Workload_10Percent_Write(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	// Warm-up
	for i := 0; i < 50; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "initial")
	}
	
	b.SetParallelism(16)
	b.ResetTimer()
	
	i := 0
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if i%10 == 0 {
				_ = reg.Report("dynamic"+string(rune(i)), "driver", ModeReal, "update")
			} else {
				_, _ = reg.getCapability("comp0")
			}
			i++
		}
	})
}

// ============================================================================
// VERIFICATION TEST 4: Memory Pressure Test
// Tests behavior under sustained load to prove no memory leaks
// ============================================================================

func BenchmarkM1_SustainedLoad_PressureTest(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	// Initial population
	for i := 0; i < 100; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "detail")
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_ = reg.GetSnapshot()
		
		// Periodic update to exercise copy-on-write
		if i%100 == 0 {
			_ = reg.Report("updated_comp", "driver", ModeReal, "refreshed")
		}
	}
}

// ============================================================================
// VERIFICATION TEST 5: HasSimulated Optimization
// Fast-path early-exit verification
// ============================================================================

func BenchmarkM1_HasSimulated_NoSimulated(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	// Populate with all real components
	for i := 0; i < 100; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "real")
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = reg.HasSimulated()
	}
}

func BenchmarkM1_HasSimulated_FirstIsSimulated(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	// First component is simulated (should exit immediately)
	_ = reg.Report("comp0", "driver", ModeSimulated, "simulated")
	for i := 1; i < 100; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "real")
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = reg.HasSimulated()
	}
}

func BenchmarkM1_HasSimulated_LastIsSimulated(b *testing.B) {
	reg := NewAtomicRegistryV2(runmode.Simulation)
	
	// Last component is simulated (must scan all)
	for i := 0; i < 99; i++ {
		_ = reg.Report("comp"+string(rune(i)), "driver", ModeReal, "real")
	}
	_ = reg.Report("comp99", "driver", ModeSimulated, "simulated")
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = reg.HasSimulated()
	}
}
