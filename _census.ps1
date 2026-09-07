# Full compile census: build every package, tally OK/FAIL, and extract unique real broken files
$pkgs = go list -e -f '{{.ImportPath}}' ./... 2>$null
$ok = 0; $fail = 0
$errlines = @()
foreach ($p in $pkgs) {
  $o = go build $p 2>&1
  if ($LASTEXITCODE -eq 0) {
    $ok++
  } else {
    $fail++
    $o | Select-String '\.go:\d+:' | ForEach-Object { $errlines += $_.Line.Trim() }
  }
}
Write-Host "=== FULL CENSUS ==="
Write-Host ("TOTAL packages : " + $pkgs.Count)
Write-Host ("COMPILE-OK     : " + $ok)
Write-Host ("COMPILE-FAIL   : " + $fail)
Write-Host ""
Write-Host "=== UNIQUE REAL BROKEN FILES (root causes, cascades deduped) ==="
$errlines | Sort-Object -Unique | ForEach-Object { Write-Host $_ }
Write-Host "CENSUS COMPLETE"
