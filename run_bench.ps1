# Run SDK benchmark and capture output
Push-Location "d:\IdeaProjects\untitled\cloudai-fusion"

Write-Host "=== M38 SDK Benchmark ===" -ForegroundColor Cyan
Write-Host "Working directory: $(Get-Location)"
Write-Host "Go version:"
go version

Write-Host "`nRunning: go test -v -bench=. -benchmem -count=3 ./pkg/sdk/`n"
$script = 'go test -v -bench=. -benchmem -count=3 ./pkg/sdk/ 2>&1'
Set-Content -Path "d:\IdeaProjects\untitled\cloudai-fusion\_temp_cmd.txt" -Value $script

Start-Process powershell -ArgumentList "-Command", "& { cd '$PWD'; `n$script }" -NoNewWindow -Wait -PassThru | Out-Null

Pop-Location
