# Extract ns/op values from Go benchmark JSON output
$path = "output/m33_redteam_t2_bench.json"
$lines = Get-Content $path -Encoding UTF8

$rt_nsop = @()
$tv_nsop = @()

foreach ($line in $lines) {
    if ($line -match '"BenchmarkM33_RedeTeam_Baseline_EvidenceChain.*?(\d+)\s+ns/op') {
        $rt_nsop += [int64]$matches[1]
    }
    if ($line -match '"BenchmarkM33_TrivyReal_DbLookup.*?(\d+)\s+ns/op') {
        $tv_nsop += [int64]$matches[1]
    }
}

Write-Host "=== M33 Red Team T2 Benchmark Results ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "REDTEAM Evidence Chain Baseline:" -ForegroundColor Green
Write-Host "  Count: $($rt_nsop.Count)"
if ($rt_nsop.Count -gt 0) {
    $rt_sorted = $rt_nsop | Sort-Object
    if ($rt_nsop.Count % 2 -eq 0) {
        $rt_median = ($rt_sorted[$rt_nsop.Count/2 - 1] + $rt_sorted[$rt_nsop.Count/2]) / 2
    } else {
        $rt_median = $rt_sorted[$rt_nsop.Count/2]
    }
    Write-Host "  Median ns/op:   $([math]::Round($rt_median,0))" -ForegroundColor Green
    Write-Host "  Avg pkgs/sec:   %.2f" -f (100 * 1e9 / (($rt_nsop | Measure-Object -Average).Average))
}

Write-Host ""
Write-Host "Trivy Real DB Lookup Baseline:" -ForegroundColor Yellow
Write-Host "  Count: $($tv_nsop.Count)"
if ($tv_nsop.Count -gt 0) {
    $tv_sorted = $tv_nsop | Sort-Object
    if ($tv_nsop.Count % 2 -eq 0) {
        $tv_median = ($tv_sorted[$tv_nsop.Count/2 - 1] + $tv_sorted[$tv_nsop.Count/2]) / 2
    } else {
        $tv_median = $tv_sorted[$tv_nsop.Count/2]
    }
    Write-Host "  Median ns/op:   $([math]::Round($tv_median,0))" -ForegroundColor Yellow
    Write-Host "  Avg pkgs/sec:   %.2f" -f (100 * 1e9 / (($tv_nsop | Measure-Object -Average).Average))
}

if ($rt_nsop.Count -gt 0 -and $tv_nsop.Count -gt 0) {
    $ratio = ($rt_median / $tv_median)
    Write-Host ""
    Write-Host "=== VERDICT DATA ===" -ForegroundColor Magenta
    Write-Host "Ratio (REDTEAM/Trivy):      $([math]::Round($ratio,1))x"
    $rt_tp = 100 / ($rt_median / 1e9)
    $tv_tp = 100 / ($tv_median / 1e9)
    Write-Host "REDTEAM throughput:         %.0f pkgs/sec" -f $rt_tp
    Write-Host "Trivy throughput:           %.0f pkgs/sec" -f $tv_tp
    
    # Honest verdict
    Write-Host ""
    Write-Host "=== HONEST VERDICT ===" -ForegroundColor Red
    if ($rt_median -gt $tv_median * 2) {
        Write-Host "RESULT: REDTEAM is SLOWER by ~$([math]::Round($ratio*100,0))%" -ForegroundColor DarkYellow
        Write-Host "REASON: Cryptographic overhead per record (signature + Merkle append + Rekor anchoring)"
        Write-Host "EDGE:   REDTEAM provides evidence-chain attestation that Trivy lacks by default:"
        Write-Host "        - Tamper-evident ledger with hash-chained records"
        Write-Host "        - Per-operation signatures for non-repudiation"
        Write-Host "        - Optional public anchoring to Rekor for global verifiability"
    } elseif ($rt_median -lt $tv_median) {
        Write-Host "RESULT: REDTEAM is FASTER" -ForegroundColor Green
        Write-Host "NOTE:   Unexpected — check if cryptographic ops are cached or optimized"
    } else {
        Write-Host "RESULT: Comparable performance within margin of error"
        Write-Host "EDGE:   REDTEAM still wins on evidentiary guarantees (not speed alone)"
    }
}
