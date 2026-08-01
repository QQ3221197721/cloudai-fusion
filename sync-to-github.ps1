# CloudAI Fusion - 强制同步到 GitHub
# 此脚本将本地完整版本推到 GitHub，覆盖远程仓库

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "CloudAI Fusion - 强制同步到 GitHub" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Set-Location "d:\IdeaProjects\untitled\cloudai-fusion"

Write-Host "[步骤 1] 检查当前 Git 状态..." -ForegroundColor Yellow
git status
Write-Host ""

Write-Host "[步骤 2] 添加所有更改到暂存区..." -ForegroundColor Yellow
git add .
if ($LASTEXITCODE -ne 0) {
    Write-Host "错误：git add 失败" -ForegroundColor Red
    exit 1
}
Write-Host ""

Write-Host "[步骤 3] 提交本地更改..." -ForegroundColor Yellow
git commit -m "Force push complete local version to cover GitHub repository"
if ($LASTEXITCODE -ne 0) {
    Write-Host "错误：git commit 失败" -ForegroundColor Red
    exit 1
}
Write-Host ""

Write-Host "[步骤 4] ⚠️ 强制推送到 GitHub (这将覆盖远程仓库的所有内容)..." -ForegroundColor Red
Write-Host "警告：此操作不可逆！GitHub 上的版本将被本地版本完全替换！" -ForegroundColor Red
Write-Host ""

$confirm = Read-Host "是否继续？(输入 YES 确认)"
if ($confirm -ne "YES") {
    Write-Host "操作已取消" -ForegroundColor Yellow
    exit 0
}

git push origin main --force
if ($LASTEXITCODE -ne 0) {
    Write-Host "错误：git push 失败" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "✅ 同步成功完成！" -ForegroundColor Green
Write-Host "本地完整版本已推送到 GitHub" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""
