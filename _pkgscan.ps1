# Detect directories containing conflicting Go package names (ignoring _test.go xxx_test packages)
$root = Get-Location
$dirs = Get-ChildItem -Recurse -Directory | Where-Object { $_.FullName -notmatch '\.quarantine' -and $_.FullName -notmatch '\\vendor\\' }
$dirs += Get-Item $root
foreach ($d in $dirs) {
  $gofiles = Get-ChildItem -Path $d.FullName -Filter *.go -File -ErrorAction SilentlyContinue
  if (-not $gofiles) { continue }
  $pkgset = @{}
  foreach ($g in $gofiles) {
    $line = Select-String -Path $g.FullName -Pattern '^package\s+(\w+)' -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($line) {
      $name = ($line.Matches[0].Groups[1].Value)
      # normalize: strip trailing _test so external test pkgs don't count as conflict
      $norm = $name -replace '_test$',''
      if (-not $pkgset.ContainsKey($norm)) { $pkgset[$norm] = @() }
      $pkgset[$norm] += $g.Name
    }
  }
  if ($pkgset.Keys.Count -gt 1) {
    $rel = $d.FullName.Replace($root.Path + '\','')
    Write-Host "CONFLICT: $rel  ->  packages: $($pkgset.Keys -join ', ')"
    foreach ($k in $pkgset.Keys) { Write-Host ("    [{0}] {1}" -f $k, ($pkgset[$k] -join ', ')) }
  }
}
Write-Host "SCAN COMPLETE"
