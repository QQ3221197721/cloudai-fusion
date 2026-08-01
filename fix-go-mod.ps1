# CloudAI Fusion - 修复 go.mod 和 go.sum
# 用于解决 Go Lint 和依赖解析问题

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "修复 Go 模块配置" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Set-Location "d:\IdeaProjects\untitled\cloudai-fusion"

Write-Host "[步骤 1] 备份当前的 go.mod..." -ForegroundColor Yellow
Copy-Item go.mod go.mod.backup -ErrorAction SilentlyContinue

Write-Host "[步骤 2] 修正 go.mod 中的 module path..." -ForegroundColor Yellow
$goModContent = Get-Content go.mod -Raw
$correctedContent = $goModContent -replace 'module github\.com/cloudai-fusion/cloudai-fusion', 'module github.com/QQ3221197721/cloudai-fusion'
$correctedContent | Set-Content go.mod -Encoding UTF8
Write-Host "✅ Module path 已修正" -ForegroundColor Green

Write-Host "[步骤 3] 删除旧的 go.sum 并重新生成..." -ForegroundColor Yellow
if (Test-Path go.sum) {
    Remove-Item go.sum -Force
}
Write-Host "[步骤 4] 运行 go mod tidy 重新计算依赖..." -ForegroundColor Yellow
Start-Process -FilePath "go" -ArgumentList "mod", "tidy" -Wait -RedirectStandardOutput "go_mod_tidy.log" -RedirectStandardError "go_mod_tidy_error.log"
Write-Host ""

Write-Host "[步骤 5] 检查是否成功..." -ForegroundColor Yellow
if (Test-Path go.sum) {
    Write-Host "✅ go.sum 已成功生成" -ForegroundColor Green
    $lineCount = (Get-Content go.sum).Count
    Write-Host "   包含 $lineCount 行依赖信息" -ForegroundColor White
} else {
    Write-Host "⚠️ 注意：go.sum 未生成（可能有未解决的依赖问题）" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "[步骤 6] 提交修复..." -ForegroundColor Yellow
git add go.mod go.sum
git commit -m "fix(go.mod): correct module path and regenerate dependencies"

Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "修复完成！请手动执行:" -ForegroundColor Green
Write-Host "  git push origin main --force" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
