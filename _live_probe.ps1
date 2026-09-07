$ErrorActionPreference = 'SilentlyContinue'
$base = 'http://localhost:8080'

# Force DB OFF (avoid touching the unknown service on :5432) + disable tracing exporter.
$env:CLOUDAI_ENV = 'development'
$env:CLOUDAI_DB_HOST = '127.0.0.1'
$env:CLOUDAI_DB_PORT = '1'
$env:CLOUDAI_JAEGER_ENDPOINT = ''

Write-Host '=== Starting real apiserver.exe (dev mode, no DB) ==='
$p = Start-Process -FilePath '.\bin\apiserver.exe' `
  -ArgumentList '--config','cloudai-fusion.yaml','--log-level','info' `
  -PassThru -NoNewWindow `
  -RedirectStandardOutput '_apiserver_out.log' -RedirectStandardError '_apiserver_err.log'

# Readiness poll on /healthz (up to ~21s)
$ready = $false
for ($i = 0; $i -lt 30; $i++) {
  try { $r = Invoke-WebRequest "$base/healthz" -UseBasicParsing -TimeoutSec 2; if ($r.StatusCode -eq 200) { $ready = $true; break } } catch {}
  Start-Sleep -Milliseconds 700
}
Write-Host ("READY={0}  PID={1}" -f $ready, $p.Id)

function Probe($method, $path, $token, $body) {
  $h = @{}
  if ($token) { $h['Authorization'] = "Bearer $token" }
  try {
    if ($method -eq 'GET') {
      $resp = Invoke-WebRequest "$base$path" -Headers $h -UseBasicParsing -TimeoutSec 6
    } else {
      $resp = Invoke-WebRequest "$base$path" -Method $method -Headers $h -ContentType 'application/json' -Body $body -UseBasicParsing -TimeoutSec 6
    }
    Write-Host ("{0,-6} {1,-38} -> {2}  ({3} bytes)" -f $method, $path, [int]$resp.StatusCode, $resp.Content.Length)
    return $resp.Content
  } catch {
    $code = 'ERR'
    if ($_.Exception.Response) { $code = [int]$_.Exception.Response.StatusCode }
    Write-Host ("{0,-6} {1,-38} -> {2}" -f $method, $path, $code)
    return $null
  }
}

Write-Host "`n--- Public endpoints ---"
Probe GET '/healthz' $null $null | Out-Null
Probe GET '/version' $null $null | Out-Null
Probe GET '/readyz'  $null $null | Out-Null
$feat = Probe GET '/api/v1/features' $null $null
try { $fj = $feat | ConvertFrom-Json; Write-Host ("       -> feature flags: total={0} enabled={1}" -f $fj.total, $fj.enabled) } catch {}

Write-Host "`n--- Auth enforcement / graceful degradation ---"
Probe GET  '/api/v1/clusters' $null $null | Out-Null
Probe POST '/api/v1/auth/login' $null '{"username":"admin","password":"admin123"}' | Out-Null

Write-Host "`n--- Mint viewer token via /auth/refresh (DB-free) ---"
$rt = ('a' * 64)
$token = $null
try {
  $r = Invoke-WebRequest "$base/api/v1/auth/refresh" -Method POST -Headers @{ Authorization = 'Bearer seed' } -ContentType 'application/json' -Body ('{"refresh_token":"' + $rt + '"}') -UseBasicParsing -TimeoutSec 6
  $token = ($r.Content | ConvertFrom-Json).access_token
  Write-Host ("refresh -> {0}, token_len={1}" -f [int]$r.StatusCode, $token.Length)
} catch { Write-Host ("refresh failed: {0}" -f $_.Exception.Message) }

Write-Host "`n--- Authenticated reads (viewer token) ---"
Probe GET '/api/v1/clusters'                $token $null | Out-Null
Probe GET '/api/v1/providers'               $token $null | Out-Null
Probe GET '/api/v1/workloads'               $token $null | Out-Null
Probe GET '/api/v1/security/policies'       $token $null | Out-Null
Probe GET '/api/v1/security/threats'        $token $null | Out-Null
Probe GET '/api/v1/monitoring/alerts/rules' $token $null | Out-Null
Probe GET '/api/v1/monitoring/alerts/events' $token $null | Out-Null
Probe GET '/api/v1/monitoring/dashboard'    $token $null | Out-Null
Probe GET '/api/v1/cost/summary'            $token $null | Out-Null
Probe GET '/api/v1/cost/optimization'       $token $null | Out-Null
Probe GET '/api/v1/mesh/status'             $token $null | Out-Null
Probe GET '/api/v1/edge/topology'           $token $null | Out-Null
Probe GET '/api/v1/wasm/health'             $token $null | Out-Null
Probe GET '/api/v1/resources/summary'       $token $null | Out-Null

Write-Host "`n--- Metrics endpoint (:9100) ---"
try { $m = Invoke-WebRequest 'http://localhost:9100/metrics' -UseBasicParsing -TimeoutSec 4; Write-Host ("GET :9100/metrics -> {0} ({1} bytes)" -f [int]$m.StatusCode, $m.Content.Length) } catch { Write-Host 'metrics endpoint not reachable' }

Stop-Process -Id $p.Id -Force 2>$null
Write-Host "`n=== Stopped apiserver (PID=$($p.Id)) ==="
Write-Host "`n=== apiserver startup log (init lines) ==="
if (Test-Path '_apiserver_out.log') { Get-Content '_apiserver_out.log' -Tail 40 }
if ((Get-Item '_apiserver_err.log' -ErrorAction SilentlyContinue).Length -gt 0) { Write-Host '--- stderr ---'; Get-Content '_apiserver_err.log' -Tail 15 }
