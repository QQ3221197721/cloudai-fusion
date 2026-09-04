# M29 FLIP Benchmark Runner - Simple PowerShell wrapper
$ErrorActionPreference = "Stop"

cd d:\IdeaProjects\untitled\cloudai-fusion

Write-Host "=== Running M29 FLIP Benchmark ===" -ForegroundColor Cyan

# Run benchmark with JSON output
go test ./pkg/hunt `
  -bench="M29Flip(UEBA|Tdigest)" `
  -run=^$ `
  -benchtime=1s `
  -count=6 `
  -json 2>&1 | Out-File m29_bench_output.json -Encoding UTF8

Write-Host "`nBenchmark complete. Output saved to m29_bench_output.json" -ForegroundColor Green

# Check results
if (Test-Path m29_bench_output.json) {
    Write-Host "`n--- Parsing Results ---" -ForegroundColor Yellow
    
    $totalLines = (Get-Content m29_bench_output.json | Measure-Object).Count
    Write-Host "Total benchmark records: $totalLines" -ForegroundColor White
    
    # Find M29 benchmarks
    $uebaBench = Select-String -Path m29_bench_output.json -Pattern "BenchmarkM29FlipUEBAAAnalyzer" | Select-Object -First 6
    $tdigestBench = Select-String -Path m29_bench_output.json -Pattern "BenchmarkM29FlipTdigestDetector" | Select-Object -First 6
    
    Write-Host "`nUEBA Benchmarks found: $($uebaBench.Count)" -ForegroundColor Green
    Write-Host "go-tdigest Benchmarks found: $($tdigestBench.Count)" -ForegroundColor Green
    
    # Try to extract NS/op values
    $nsOps = Select-String -Path m29_bench_output.json -Pattern '"NS/op"\s*:\s*(\d+)' 
    if ($nsOps.Count -gt 0) {
        Write-Host "`nSample NS/op values:" -ForegroundColor Cyan
        $nsOps | Select-Object -First 3 | ForEach-Object { 
            Write-Host $_.Line -ForegroundColor Gray 
        }
    }
    
} else {
    Write-Host "Error: No output generated!" -ForegroundColor Red
    exit 1
}

Write-Host "`n=== Complete ===" -ForegroundColor Cyan
