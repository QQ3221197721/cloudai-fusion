// Package wasm — M42 Lite Benchmark (300ms micro-bench)
//
// GOAL: Ultra-fast head-to-head between our sandbox exec vs raw wazero interpreter
// MODULE: minimal add i32,i32->i32 (~16 bytes WAT compiled)
// BENCHTIME: 300ms | COUNT: 3 (NOT 6, NOT 2s) — MUST finish under 60s total
// WORKLOAD: N=1 call per iteration, C=1 only
//
// COMPETEES:
// - Ours: capability-wrapped sandbox execution path (via ExecutableModule)
// - Competitor: raw wazero runtime config Interpreter mode (NEW_RUNTIME_CONFIG_INTERPRETER)
//
// ANTI-FIASCO: If we're slower (capability checks cost), ADMIT IT and claim edge:
// "Capability security + resource limits + snapshot restore (wazero RAW LACKS)"
//
// COMMANDS (PowerShell):
// go build ./pkg/wasm/...; go vet ./pkg/wasm/...
// go test -run=^$ -bench=BenchmarkM42Lite_Exec -benchtime=300ms -count=3 -json ./pkg/wasm/ 2>&1 | Out-String
// Parse ns/op medians across count=3 for verdict.
package wasm

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestM42Lite_Sanity(t *testing.T) {
	// Verify our sandbox can execute minimalAddModule
	cfg := DefaultRuntimeConfig()
	cfg.MaxMemoryPages = 100
	cfg.EnableWASI = false
	
	sandbox, err := NewWazeroInstance(cfg)
	if err != nil {
		t.Fatalf("NewWazeroInstance failed: %v", err)
	}
	defer sandbox.Close()
	
	// Pre-instantiate (includes compile)
	if err := sandbox.Instantiate(minimalAddModule); err != nil {
		t.Fatalf("Instantiate failed: %v", err)
	}
}

func BenchmarkM42Lite_Exec_Ours(b *testing.B) {
	cfg := DefaultRuntimeConfig()
	cfg.MaxMemoryPages = 100
	cfg.EnableWASI = false
	
	sandbox, err := NewWazeroInstance(cfg)
	if err != nil {
		b.Fatalf("NewWazeroInstance failed: %v", err)
	}
	defer sandbox.Close()
	
	// Pre-instantiate (includes compile)
	if err := sandbox.Instantiate(minimalAddModule); err != nil {
		b.Fatalf("Instantiate failed: %v", err)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, _ := sandbox.InvokeFunction("add", 3, 5)
		_ = result
	}
}

func BenchmarkM42Lite_Exec_WazeroInterpreter(b *testing.B) {
	ctx := context.Background()
	
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().
		WithMemoryLimitPages(100).
		WithCloseOnContextDone(true))
	defer runtime.Close(ctx)
	
	compiled, err := runtime.CompileModule(ctx, minimalAddModule)
	if err != nil {
		b.Fatalf("CompileModule failed: %v", err)
	}
	
	mod, err := runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		b.Fatalf("InstantiateModule failed: %v", err)
	}
	
	fn := mod.ExportedFunction("add")
	if fn == nil {
		b.Fatal("add function not exported")
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, _ := fn.Call(ctx, 3, 5)
		_ = result
	}
}
