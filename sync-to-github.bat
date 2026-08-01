@echo off
setlocal enabledelayedexpansion

echo ========================================
echo CloudAI Fusion - 强制同步到 GitHub
echo ========================================
echo.

cd /d "d:\IdeaProjects\untitled\cloudai-fusion"

echo 步骤 1: 检查 Git 状态
git status
echo.

echo 步骤 2: 添加所有更改
git add .
if %errorlevel% neq 0 (
    echo 错误：git add 失败
    exit /b 1
)
echo.

echo 步骤 3: 提交更改
git commit -m "Force push complete local version to cover GitHub repository"
if %errorlevel% neq 0 (
    echo 错误：git commit 失败
    exit /b 1
)
echo.

echo 步骤 4: 强制推送到 GitHub
echo 警告：这将覆盖 GitHub 上的所有内容！
git push origin main --force
if %errorlevel% neq 0 (
    echo 错误：git push 失败
    exit /b 1
)
echo.

echo ========================================
echo 同步成功完成！
echo ========================================
pause
