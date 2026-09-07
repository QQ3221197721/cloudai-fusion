// Package wasm — Module 50: Honest performance benchmarks for the wazero-backed
// WASM execution engine.
//
// These benchmarks measure REAL numbers on the local machine so we can position
// our pure-Go wazero runtime honestly against WasmEdge (AOT, CGO) and Firecracker
// (microVM). We deliberately reuse the inline WASM bytecode already vetted in
// wazero_runtime_test.go (minimalAddModule / memoryModule) so nothing is
// downloaded and everything is reproducible with:
//
//	go test ./pkg/wasm/... -bench=. -benchmem -count=1
//
// HONESTY NOTES baked into the benchmark design:
//   - wazero v1.12 executes via an optimizing interpreter on most platforms
//     (no CGO). We do NOT claim AOT-level throughput.
//   - The WASM-vs-native "overhead multiple" is dominated by the host<->guest
//     call boundary. Native Go add is near-free (~1ns even with //go:noinline),
//     so the ratio is large by construction. We report both absolute ns/op so
//     the reader can judge the boundary cost fairly rather than a scary ratio.
package wasm

import (
	"testing"
)

// nativeAdd is an equivalent pure-Go implementation of the WASM `add` export.
// //go:noinline prevents the compiler from inlining it to zero cost, giving a
// fairer (still cheap) native call baseline to compare the FFI boundary against.
//
//go:noinline
func nativeAdd(a, b uint64) uint64 {
	return a + b
}

// ----------------------------------------------------------------------------
// (1) Instantiation latency: compile + instantiate a module (ms/instance).
// ----------------------------------------------------------------------------

// BenchmarkInstantiate_CompilePlusInstantiate measures the full cost of turning
// raw WASM bytes into a callable instance (runtime + compile + instantiate).
// This is the "cold start per module" number.
func BenchmarkInstantiate_CompilePlusInstantiate(b *testing.B) {
	cfg := RuntimeConfig{MaxMemoryPages: 10, TimeoutPerInvoke: 5e9, EnableWASI: false}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := NewWazeroInstance(cfg)
		if err != nil {
			b.Fatalf("new instance: %v", err)
		}
		if err := inst.Instantiate(minimalAddModule); err != nil {
			b.Fatalf("instantiate: %v", err)
		}
		_ = inst.Close()
	}
}

// BenchmarkInstantiate_RuntimeOnly isolates the wazero runtime construction cost
// (no module compiled). Subtract this from the number above to get the pure
// compile+instantiate delta.
func BenchmarkInstantiate_RuntimeOnly(b *testing.B) {
	cfg := RuntimeConfig{MaxMemoryPages: 10, TimeoutPerInvoke: 5e9, EnableWASI: false}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := NewWazeroInstance(cfg)
		if err != nil {
			b.Fatalf("new instance: %v", err)
		}
		_ = inst.Close()
	}
}

// ----------------------------------------------------------------------------
// (2) Call overhead: WASM fn.Call vs equivalent native Go call.
// ----------------------------------------------------------------------------

// BenchmarkInvoke_WASMAdd measures the steady-state cost of invoking an already
// instantiated WASM `add` export via the low-level InvokeFunction path
// (this is the real wazero fn.Call boundary Tim wired up, no stubbing).
func BenchmarkInvoke_WASMAdd(b *testing.B) {
	cfg := RuntimeConfig{MaxMemoryPages: 10, TimeoutPerInvoke: 5e9, EnableWASI: false}
	inst, err := NewWazeroInstance(cfg)
	if err != nil {
		b.Fatalf("new instance: %v", err)
	}
	defer inst.Close()
	if err := inst.Instantiate(minimalAddModule); err != nil {
		b.Fatalf("instantiate: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := inst.InvokeFunction("add", 3, 5); err != nil {
			b.Fatalf("invoke: %v", err)
		}
	}
}

// NOTE ON THE HIGH-LEVEL Invoke() PATH: Invoke(ctx, fnName, input) calls
// fn.Call(ctx) WITHOUT numeric arguments (it is designed to pass I/O via linear
// memory, not via wasm params). It therefore cannot drive the param-based `add`
// export, so we benchmark the call boundary via the low-level InvokeFunction
// path above, which is exactly the real wazero fn.Call(ctx, args...) Tim wired up.

// BenchmarkInvoke_NativeGoAdd is the native Go baseline for the SAME operation.
// The ratio (WASMAdd ns/op) / (NativeGoAdd ns/op) is the honest call-boundary
// overhead multiple. Expect a large ratio because native add is ~1ns; the
// absolute WASM ns/op is the number that matters for real workloads.
var sinkAdd uint64

func BenchmarkInvoke_NativeGoAdd(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var acc uint64
	for i := 0; i < b.N; i++ {
		acc = nativeAdd(3, 5)
	}
	sinkAdd = acc
}

// ----------------------------------------------------------------------------
// (3) Memory footprint per live instance.
// ----------------------------------------------------------------------------

// BenchmarkMemory_LiveInstanceFootprint keeps each instantiated module alive
// (module holds its linear memory) so -benchmem B/op reflects the retained
// per-instance footprint rather than transient allocations that are freed
// within the loop iteration.
func BenchmarkMemory_LiveInstanceFootprint(b *testing.B) {
	cfg := RuntimeConfig{MaxMemoryPages: 10, TimeoutPerInvoke: 5e9, EnableWASI: false}
	insts := make([]*WazeroInstance, 0, b.N)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := NewWazeroInstance(cfg)
		if err != nil {
			b.Fatalf("new instance: %v", err)
		}
		if err := inst.Instantiate(memoryModule); err != nil {
			b.Fatalf("instantiate: %v", err)
		}
		insts = append(insts, inst)
	}
	b.StopTimer()
	for _, inst := range insts {
		_ = inst.Close()
	}
}
