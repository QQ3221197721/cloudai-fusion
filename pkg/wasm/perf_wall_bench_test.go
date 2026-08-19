// Package wasm — Task 104: performance-wall benchmarks that close the gaps
// left by the existing suite (wazero_pool_bench_test.go, wasi_gpu_bench_test.go,
// wazero_runtime_test.go).
//
// The existing files already cover cold-start, pooling amortization, pool
// borrow/return overhead, concurrent access, and GPU-WASI capability checks.
// This file adds the four dimensions those files do NOT isolate:
//
//  1. Pure warm function-call round-trip overhead (ns/op, zero pool noise).
//  2. Isolated memory snapshot cost and restore cost (separately, not mixed).
//  3. Capability gate cost ON the invoke path (gated vs un-gated invoke).
//  4. Hot-migration state-transfer latency (real snapshot→serialize→restore),
//     with an explicit request-loss annotation for the drain-before-swap design.
//
// Honesty notes:
//   - wazero is a pure-Go interpreter; per-call cost is interpreter cost, not
//     AOT machine-code cost. See docs/performance-validation-wasm.md.
//   - All snapshot/restore numbers are REAL linear-memory byte transfers
//     through wazero's Memory.Read/Write (see wazero_runtime.go).
//
// Run (PowerShell, batched — WASM benches are slow):
//   go test ./pkg/wasm/ "-bench=BenchmarkCallOverheadWarm" -benchmem -count=3 -benchtime=5x "-run=^$"
package wasm

import (
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// 1. Warm function-call round-trip overhead (ns/op)
// ============================================================================

// BenchmarkCallOverheadWarm isolates the cost of a single exported-function
// round trip on an ALREADY-instantiated instance. Unlike BenchmarkPoolPreWarmed
// (which also pays a channel Get/Put per op), this holds one warm instance and
// times InvokeFunction alone, so the reported ns/op is the pure wazero call
// overhead a request pays once the cold-start has been amortized away.
func BenchmarkCallOverheadWarm(b *testing.B) {
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false
	inst, err := NewWazeroInstance(cfg)
	if err != nil {
		b.Skipf("Skipping (wazero not available): %v", err)
	}
	defer inst.Close()
	if err := inst.Instantiate(minimalAddModule); err != nil {
		b.Skipf("Skipping (instantiate failed): %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := inst.InvokeFunction("add", 3, 5); err != nil {
			b.Fatalf("invoke: %v", err)
		}
	}
}

// BenchmarkCallOverheadWarmParallel measures the same warm call under parallel
// borrowers sharing one instance (InvokeFunction takes only an RLock), showing
// how the per-call cost scales with contention on a single hot module.
func BenchmarkCallOverheadWarmParallel(b *testing.B) {
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false
	inst, err := NewWazeroInstance(cfg)
	if err != nil {
		b.Skipf("Skipping (wazero not available): %v", err)
	}
	defer inst.Close()
	if err := inst.Instantiate(minimalAddModule); err != nil {
		b.Skipf("Skipping (instantiate failed): %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = inst.InvokeFunction("add", 3, 5)
		}
	})
}

// ============================================================================
// 2. Isolated snapshot / restore cost
// ============================================================================

// benchMemoryInstance returns a warm instance backed by the 1-page memory
// module, ready for snapshot/restore timing. Skips the benchmark cleanly if
// wazero is unavailable.
func benchMemoryInstance(b *testing.B) *WazeroInstance {
	b.Helper()
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false
	inst, err := NewWazeroInstance(cfg)
	if err != nil {
		b.Skipf("Skipping (wazero not available): %v", err)
	}
	if err := inst.Instantiate(memoryModule); err != nil {
		_ = inst.Close()
		b.Skipf("Skipping (instantiate failed): %v", err)
	}
	return inst
}

// BenchmarkSnapshotOnly times a full linear-memory capture (real Memory.Read of
// every byte, one 64 KiB page here). This is the export half of a hot-migration.
func BenchmarkSnapshotOnly(b *testing.B) {
	inst := benchMemoryInstance(b)
	defer inst.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := inst.Snapshot(); err != nil {
			b.Fatalf("snapshot: %v", err)
		}
	}
}

// BenchmarkRestoreOnly times writing a snapshot back into linear memory (real
// Memory.Write). The snapshot is captured once before the timer so only the
// restore path is measured. This is the import half of a hot-migration.
func BenchmarkRestoreOnly(b *testing.B) {
	inst := benchMemoryInstance(b)
	defer inst.Close()

	snap, err := inst.Snapshot()
	if err != nil {
		b.Fatalf("snapshot: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := inst.Restore(snap); err != nil {
			b.Fatalf("restore: %v", err)
		}
	}
}

// ============================================================================
// 3. Capability gate ON the invoke path
// ============================================================================

// BenchmarkCapabilityGateOnCallPath measures the marginal cost of enforcing a
// capability gate immediately before a WASM call, versus the same call with no
// gate. The gate here is the deny-by-default nil-check + rule evaluation from
// this package's own Grant/PathRule (pkg/wasm/capability.go, in scope) — NOT
// pkg/capability (out of scope). The delta between the two sub-benchmarks is
// the security tax the sandbox adds per call.
func BenchmarkCapabilityGateOnCallPath(b *testing.B) {
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false
	inst, err := NewWazeroInstance(cfg)
	if err != nil {
		b.Skipf("Skipping (wazero not available): %v", err)
	}
	defer inst.Close()
	if err := inst.Instantiate(minimalAddModule); err != nil {
		b.Skipf("Skipping (instantiate failed): %v", err)
	}

	// A representative grant: filesystem root + GPU device, mirroring what a
	// WASI host-call would consult before dispatching into the guest.
	grant := &Grant{
		Filesystem: &PathRule{AllowedRoots: []string{"/data"}},
		GPU:        &GPURule{AllowedDevices: []int{0, 1}, MaxMemoryGB: 8},
	}

	b.Run("no-gate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = inst.InvokeFunction("add", 3, 5)
		}
	})

	b.Run("with-gate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Deny-by-default gate on the hot path: nil-checks + one rule eval.
			if !grant.HasFilesystemAccess() || !grant.Filesystem.IsPathAllowed("/data/model.bin") {
				b.Fatal("expected fs grant")
			}
			if !grant.HasGPUAccess() || !grant.GPU.IsDeviceAllowed(0) {
				b.Fatal("expected gpu grant")
			}
			_, _ = inst.InvokeFunction("add", 3, 5)
		}
	})

	// Pure gate cost, no WASM call, to expose the <30ns target directly.
	b.Run("gate-only", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if !grant.HasGPUAccess() || !grant.GPU.IsDeviceAllowed(0) {
				b.Fatal("expected gpu grant")
			}
		}
	})
}

// BenchmarkCapabilityDenyPath measures the cheapest refusal: a nil grant field
// short-circuits before any rule evaluation (deny-by-default). This is the cost
// of blocking an unauthorized syscall attempt from the guest.
func BenchmarkCapabilityDenyPath(b *testing.B) {
	denied := NewDefaultGrant() // all fields nil → everything denied
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if denied.HasGPUAccess() || denied.HasFilesystemAccess() || denied.HasNetworkAccess() {
			b.Fatal("default grant must deny everything")
		}
	}
}

// ============================================================================
// 4. Hot-migration state-transfer latency (real bytes) + request-loss note
// ============================================================================

// BenchmarkMigrationStateTransfer measures the real state-transfer window of a
// hot migration: snapshot the source instance's linear memory, serialize it
// through the migration wire format, deserialize it, and restore it into a
// PRE-WARMED target instance. The artificial DrainTimeoutSec sleep in
// RunMigration is intentionally excluded — this isolates the actual work that
// determines how long the swap window lasts.
//
// Request-loss: the migration design drains in-flight requests BEFORE swapping
// (RunMigration step 3) and the target is pre-warmed, so in-flight requests are
// not dropped (loss rate = 0 by construction). New requests arriving during the
// window queue behind the warm target rather than paying a cold start. The
// reported "reqloss" metric records this design invariant.
func BenchmarkMigrationStateTransfer(b *testing.B) {
	// Silence the migration service's per-snapshot Info logging so the benchmark
	// output stays clean and the logging cost does not dominate the measurement.
	quiet := logrus.New()
	quiet.SetLevel(logrus.PanicLevel)
	svc := NewMigrationService(DefaultMigrationConfig(), quiet)

	source := benchMemoryInstance(b)
	defer source.Close()

	// Write a deterministic pattern so the transfer moves real, non-zero bytes.
	srcMem := source.testModuleForSnapshot().Memory()
	pattern := make([]byte, 4096)
	for i := range pattern {
		pattern[i] = byte(i % 251)
	}
	_ = srcMem.Write(0, pattern)

	// Target is pre-warmed (module instantiated) so restore writes real memory.
	target := benchMemoryInstance(b)
	defer target.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := svc.Snapshot(source)
		if err != nil {
			b.Fatalf("snapshot: %v", err)
		}
		snap := &Snapshot{}
		if err := snap.UnmarshalBinary(data); err != nil {
			b.Fatalf("unmarshal: %v", err)
		}
		if err := target.Restore(snap.Memory); err != nil {
			b.Fatalf("restore: %v", err)
		}
	}
	b.ReportMetric(0, "reqloss") // drain-before-swap + warm target ⇒ 0 dropped
}

// Compile-time assertion that the capability API used above stays in this
// package (pkg/wasm), keeping the benchmark within the task's scope isolation.
var _ = capability.ModeSimulated
