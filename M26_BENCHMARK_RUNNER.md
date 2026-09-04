# M26 Head-to-Head Benchmark Runner Script

This script reproduces the M26 Remote Provisioning head-to-head benchmarks.

## Prerequisites
- Windows PowerShell
- Go 1.25+ with GOMODCACHE=E:\go\pkg\mod
- Working directory: `d:\IdeaProjects\untitled\cloudai-fusion`

## Quick Run (Full Benchmarks)

```powershell
cd cloudai-fusion
go test -tags=m26headtohead -bench="M26_" -benchtime=2s -count=6 -json ./pkg/edge > m26_bench_results.json
Write-Output "Benchmarks complete. Results in m26_bench_results.json"
```

## Verify Build Before Running

```powershell
cd cloudai-fusion
go build ./pkg/edge 2>&1
if ($LASTEXITCODE -ne 0) { 
    Write-Output "Build failed! Fix errors before proceeding."
    exit $LASTEXITCODE 
}

go vet -tags=m26headtohead ./pkg/edge 2>&1
if ($LASTEXITCODE -ne 0) { 
    Write-Output "Vet found issues!"
    exit $LASTEXITCODE 
}
Write-Output "Build and vet passed. Ready to benchmark."
```

## Run Correctness Tests Only

```powershell
cd cloudai-fusion
go test -tags=m26headtohead -run "TestCorrectness|TestRollback" ./pkg/edge -v
```

Expected output:
- TestCorrectness_NodeManager: PASS
- TestCorrectness_SSHProxy: PASS  
- TestCorrectness_TerraformProxy: PASS
- TestRollbackCapability: PASS

## Parse Results with PowerShell

Extract all benchmark lines:

```powershell
Get-Content m26_bench_results_v2.json | Select-String 'ns/op' | ForEach-Object { $_ }
```

Count runs per benchmark (should be 6):

```powershell
$json = Get-Content m26_bench_results_v2.json -Raw | ConvertFrom-Json
$benchmarks = $json | Where-Object { $_.Action -eq "output" -and $_.Output -like "*ns/op*" }
$benchmarks | Group-Object {$_.Test} | ForEach-Object { 
    Write-Output "$($_.Name): $($_.Count) runs" 
}
```

## Expected Results Summary

Based on our run (median of 6):

### Small Config (1KB, 5 nodes)
- NodeManager: ~8,826 ns/op ✅ WINNER
- SSH Proxy: ~34,160,000 ns/op
- Terraform Proxy: ~100,696,000 ns/op

### Medium Config (10KB, 25 nodes)
- NodeManager: ~45,388 ns/op ✅ WINNER
- SSH Proxy: ~173,650,000 ns/op
- Terraform Proxy: ~100,865,000 ns/op

### Large Config (100KB, 50 nodes)
- NodeManager: ~88,541 ns/op ✅ WINNER
- SSH Proxy: ~348,970,000 ns/op
- Terraform Proxy: ~102,670,000 ns/op

**Verdict:** NodeManager wins by 3-4 orders of magnitude

## Notes

- Competitors documented as **faithful in-memory proxies**, NOT real SSH/Terraform CLI
- Real device status: **N/A** (no physical SSH devices available)
- Simulation parameters pulled from published benchmarks:
  - Local SSH RTT: 5ms
  - Terraform apply delay: 100ms
- Count=6 median provides anti-outlier protection
- Work unit: Push N config blobs → measure latency/throughput/correctness/rollback

## Output Files

- `m26_provision_head_to_head_test.go`: Benchmark implementation
- `m26_bench_results.json`: Raw JSON output
- `M26_REMOTE_PROVISIONING_HEAD_TO_HEAD_BENCHMARK.md`: Full analysis report

## Next Steps After Benchmark

1. Review verdict report for tradeoffs
2. Define precise, defensible claims based on actual data
3. Document honest loss scenarios where SSH/Terraform win
4. Incorporate findings into product positioning
