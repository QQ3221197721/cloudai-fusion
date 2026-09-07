$ErrorActionPreference = 'SilentlyContinue'
$env:CLOUDAI_RUN_MODE = 'degraded'
$env:CLOUDAI_DB_PORT = '1'
$env:CLOUDAI_JAEGER_ENDPOINT = ''
$p = Start-Process -FilePath '.\bin\apiserver.exe' `
  -ArgumentList '--config', 'cloudai-fusion.yaml', '--log-level', 'error' `
  -PassThru -NoNewWindow -RedirectStandardOutput '_cap_out.log' -RedirectStandardError '_cap_err.log'
for ($i = 0; $i -lt 24; $i++) {
  try { $r = Invoke-WebRequest 'http://localhost:8080/healthz' -UseBasicParsing -TimeoutSec 2; if ($r.StatusCode -eq 200) { break } } catch {}
  Start-Sleep -Milliseconds 500
}
Write-Host '=== GET /api/v1/capabilities (run_mode=degraded, no infra) ==='
try { (Invoke-WebRequest 'http://localhost:8080/api/v1/capabilities' -UseBasicParsing -TimeoutSec 5).Content } catch { Write-Host "err: $($_.Exception.Message)" }
Write-Host "`n=== GET /readyz ==="
try { (Invoke-WebRequest 'http://localhost:8080/readyz' -UseBasicParsing -TimeoutSec 5).Content } catch { Write-Host "err: $($_.Exception.Message)" }
Stop-Process -Id $p.Id -Force 2>$null
Write-Host "`n=== stopped (pid=$($p.Id)) ==="
