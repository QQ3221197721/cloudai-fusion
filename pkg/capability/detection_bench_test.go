package capability

import (
	"context"
	"crypto/ed25519"
	"strconv"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

// ============================================================================
// Capability gate decision latency - hot path optimization
// ============================================================================

func BenchmarkPolicyCheckProduction(b *testing.B) {
	b.ReportAllocs()
	r := NewRegistry(runmode.Production)
	var backends []Backend
	for i := 0; i < 20; i++ {
		r.Report("cache.redis", "redis", ModeReal, "prod-cluster:6379")
		r.Report("messaging.kafka", "kafka", ModeReal, "broker-1:9092")
		r.Report("store.pg", "postgres", ModeReal, "primary-db")
	}
	backends = r.Snapshot()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		err := r.Enforce()
		_ = err // consume result
		if len(backends) > 0 {
			_ = backends[i%len(backends)].Component
		}
	}
}

func BenchmarkPolicyCheckSimulated(b *testing.B) {
	b.ReportAllocs()
	r := NewRegistry(runmode.Simulation)
	for i := 0; i < 20; i++ {
		r.Report("cache.sim", "memory", ModeSimulated, "in-memory-fallback")
		r.Report("messaging.sim", "mem", ModeSimulated, "local-queue")
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = r.HasSimulated()
		_ = r.Policy()
	}
}

func BenchmarkEnforceFailFast(b *testing.B) {
	b.ReportAllocs()
	r := NewRegistry(runmode.Production)
	r.Report("cache.redis", "redis", ModeReal, "ok")
	r.Report("scheduler.nodes", "sim", ModeSimulated, "no-k8s-detected")
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		err := r.Enforce()
		if err != nil {
			_ = err.Error()
		}
	}
}

// ============================================================================
// Edge-autonomy T3 directional gate — 三维能力模型 + deny-by-default
// ============================================================================

func BenchmarkThreeDimensionalGate(b *testing.B) {
	b.ReportAllocs()
	
	// Simulate the 3D capability matrix check (GPU, TEE, DataLayer)
	type CapMatrix [3][3]bool
	matrix := CapMatrix{
		{true, false, true}, {false, true, false}, {true, true, true},
	}
	
	r := NewRegistry(runmode.Degraded)
	r.SetPolicy(runmode.Production)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		// Gate 1: deny-by-default when any required cap missing
		denied := !matrix[i%3][i%3]
		
		// Gate 2: cross-cap dependency validation
		for dim := 0; dim < 3; dim++ {
			if denied && !matrix[dim][dim] {
				continue
			}
			_ = matrix[dim][dim]
		}
		
		_ = denied
	}
}

func BenchmarkDenyByDefaultPolicyCheck(b *testing.B) {
	b.ReportAllocs()
	
	type RequiredCapability string
	const (
		CapGPU        RequiredCapability = "gpu"
		CapTEE        RequiredCapability = "tee"
		CapDataLayer  RequiredCapability = "data-layer"
	)
	
	requiredCaps := []RequiredCapability{CapGPU, CapTEE, CapDataLayer}
	availableCaps := map[RequiredCapability]bool{
		CapGPU:   true,
		CapTEE:   false, // missing → deny
		CapDataLayer: true,
	}
	
	policyDeny := func(req []RequiredCapability, avail map[RequiredCapability]bool) bool {
		for _, c := range req {
			if !avail[c] {
				return true
			}
		}
		return false
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		denied := policyDeny(requiredCaps, availableCaps)
		_ = denied
	}
}

func BenchmarkEvidenceCapReceiptSignVerify(b *testing.B) {
	engine := NewEvidenceCapabilityEngine()
	privKey := engine.GetPrivKey()

	receipt := &EvidenceCapReceipt{
		Timestamp: time.Now().UnixNano(),
		Module:    "capability",
		Operation: "cap.detect",
		Input:     []byte(`{"available":["high-throughput","gpu-accelerated"],"needed":["high-throughput","low-latency"]}`),
		Output:    []byte(`{"detected":["high-throughput"],"missing":["low-latency"],"current_tier":"cpu-only"}`),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		receipt.Signature = ed25519.Sign(privKey, receipt.signingPayload())
		if !receipt.Verify(privKey) {
			b.Fatal("verification failed")
		}
	}
}

func BenchmarkEvidenceCapDetect(b *testing.B) {
	engine := NewEvidenceCapabilityEngine()
	available := []string{"high-throughput", "gpu-accelerated"}
	needed := []string{"high-throughput", "low-latency"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := engine.Detect(available, needed)
		if err != nil {
			b.Fatal(err)
		}
		_ = res.CurrentTier
	}
}

// ============================================================================
// Capability detection path latency (avoid shell in bench, use pure Go)
// ============================================================================

func BenchmarkDetectionHotPath(b *testing.B) {
	b.ReportAllocs()
	
	// Pre-build feature flag patterns to simulate detection results
	patterns := []FeatureFlags{
		{SGX: SGXCapability{Available: true}, GPU: GPUCapability{Available: true}, EBPF: EBPFCapability{Available: true}},
		{SGX: SGXCapability{Available: false}, GPU: GPUCapability{Available: false}, EBPF: EBPFCapability{Available: true}},
		{SGX: SGXCapability{Available: false}, GPU: GPUCapability{NvidiaPresent: true}, EBPF: EBPFCapability{SupportLevel: 1}},
	}
	
	detector := NewDetector()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		flags := detector.DetectAll(ctx)
		cancel()
		
		_ = flags.Committed
		_ = detector.Flags()
		_ = patterns[i%len(patterns)].GPU.VRAMMB
	}
}

func BenchmarkGracefulDegradationPlanning(b *testing.B) {
	b.ReportAllocs()
	
	detector := NewDetector()
	detector.flags.SGX.Available = false
	detector.flags.GPU.Available = false
	detector.flags.EBPF.Available = false
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		policy := detector.GracefulDegradation()
		_ = len(policy)
		_ = policy["tee"]
		_ = policy["gpu-scheduling"]
	}
}

// ============================================================================
// Registry snapshot performance (frequently accessed in health check endpoints)
// ============================================================================

func BenchmarkRegistrySnapshotSorted(b *testing.B) {
	b.ReportAllocs()
	
	r := NewRegistry(runmode.Degraded)
	for i := 0; i < 100; i++ {
		componentName := "component-"
		switch {
		case i < 30:
			componentName += "cache."
		case i < 60:
			componentName += "messaging."
		case i < 80:
			componentName += "store."
		default:
			componentName += "scheduler."
		}
		componentName += strconv.Itoa(i)

		r.Report(componentName, "driver-"+strconv.Itoa(i), ModeReal, "")
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		snap := r.Snapshot()
		if len(snap) > 0 {
			_ = snap[0].Component
		}
	}
}

func BenchmarkRegistryHasSimulatedOptimization(b *testing.B) {
	b.ReportAllocs()
	
	r := NewRegistry(runmode.Production)
	for i := 0; i < 50; i++ {
		r.Report("real.component"+strconv.Itoa(i), "real", ModeReal, "")
	}
	r.Report("sim.fallback", "memory", ModeSimulated, "")
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		result := r.HasSimulated()
		_ = result
	}
}
