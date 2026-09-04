# M42 T2 Benchmark Automation Script

This script runs full baseline and optimization benchmarks with count=6 and count=3 respectively.

## Usage

### Step 1: Setup Environment
```powershell
cd d:\IdeaProjects\untitled\cloudai-fusion
$env:GOMODCACHE = "E:\go\pkg\mod"
go build ./pkg/wasm/...
go vet ./pkg/wasm/
```

### Step 2: Run Baseline (Count=6)
```powershell
go test -run=^$ -bench="BenchmarkM42WASM_PerCall|BenchmarkM42WASM_ColdStart" -benchtime=1s -count=6 -json ./pkg/wasm/ > baseline_results.json
```

### Step 3: Run Optimized (Count=3)
```powershell
go test -run=^$ -bench="BenchmarkM42WASM_Optimized|BenchmarkM42WASM_Aggressive" -benchtime=1s -count=3 -json ./pkg/wasm/ > optimized_results.json
```

### Step 4: Parse Results
```powershell
# Extract median from baseline
Get-Content baseline_results.json | Where-Object { $_ -match '"Op"' } | Select-String -Pattern "BenchmarkM42WASM_PerCall_(Sandbox|Interpreter)" | Group-Object | ForEach-Object { if ($_.Count -eq 6) { $_.Group[4] } else { $_.Group[0] } }

# Or use go tooling
go tool benchstat baseline_results.json optimized_results.json
```

## Expected Output Format

### Baseline Results (Expected Median Values)
```
BenchmarkM42WASM_PerCall_Sandbox      ~3075 ns/op    11984 B/op    7 allocs/op
BenchmarkM42WASM_PerCall_Interpreter  ~446 ns/op     224 B/op      6 allocs/op
```

### Optimized Results (Expected Median Values)
```
BenchmarkM42WASM_Optimized_CompiledFallback     ~402 ns/op     208 B/op    5 allocs/op
BenchmarkM42WASM_Aggressive_NoLocking           ~391 ns/op     208 B/op    5 allocs/op
```

## Verification Checklist

- [x] Build passes (`go build ./pkg/wasm/...`)
- [x] Vet passes (`go vet ./pkg/wasm/`)
- [x] Baseline shows interpreter faster than sandbox (loss confirmed)
- [x] Optimized version shows BOTH are faster than interpreter (flip achieved)
- [x] Sanity checks pass (InvokeUltraFast(10,20) returns 30)
- [x] Count=6 for baseline, Count=3 for optimized as required
