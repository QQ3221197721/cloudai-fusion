@echo off
REM ============================================================================
REM CloudAI Fusion - GPU Environment Setup (Windows)
REM ============================================================================
REM
REM This script checks GPU support for Docker containers on Windows.
REM
REM Requirements:
REM   1. Windows 10/11 (build 19041+) with WSL2
REM   2. NVIDIA GPU with Game Ready / Studio driver (v525.60+)
REM   3. Docker Desktop with WSL2 backend enabled
REM
REM Usage:
REM   scripts\setup-gpu.bat
REM
REM ============================================================================
title CloudAI Fusion - GPU Setup
setlocal enabledelayedexpansion

set "ERRORS=0"
set "WARNINGS=0"

echo.
echo  ========================================================
echo    CloudAI Fusion - GPU Environment Setup (Windows)
echo  ========================================================
echo.

REM --------------------------------------------------------------------------
REM [1/5] Check Windows version
REM --------------------------------------------------------------------------
echo [1/5] Checking Windows version...

for /f "tokens=2 delims==" %%a in ('wmic os get BuildNumber /value 2^>nul') do set "BUILD=%%a"
set "BUILD=%BUILD: =%"
if "%BUILD%"=="" (
    echo   [WARN] Could not detect Windows build number.
    set /a WARNINGS+=1
) else (
    if %BUILD% GEQ 19041 (
        echo   [OK]   Windows Build %BUILD% ^(WSL2 requires 19041+^)
    ) else (
        echo   [FAIL] Windows Build %BUILD% is too old for WSL2 ^(need 19041+^)
        echo          Update Windows: Settings ^> Update ^& Security ^> Windows Update
        set /a ERRORS+=1
    )
)
echo.

REM --------------------------------------------------------------------------
REM [2/5] Check WSL2
REM --------------------------------------------------------------------------
echo [2/5] Checking WSL2...

wsl --status >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo   [FAIL] WSL2 is not installed or not enabled.
    echo.
    echo          To install WSL2:
    echo            wsl --install
    echo            ^(then restart your computer^)
    echo.
    echo          To set WSL2 as default:
    echo            wsl --set-default-version 2
    echo.
    set /a ERRORS+=1
    goto :check_nvidia
)

REM Check WSL version
for /f "tokens=*" %%w in ('wsl -l -v 2^>nul') do (
    echo %%w | findstr /i "2" >nul 2>nul && set "WSL2_FOUND=1"
)

if defined WSL2_FOUND (
    echo   [OK]   WSL2 is available
) else (
    echo   [WARN] WSL is installed but no WSL2 distro found
    echo          Install Ubuntu: wsl --install -d Ubuntu
    echo          Convert existing: wsl --set-version ^<distro^> 2
    set /a WARNINGS+=1
)

REM Check if Docker WSL integration is likely active
wsl -e docker info >nul 2>nul
if %ERRORLEVEL% equ 0 (
    echo   [OK]   Docker is accessible from WSL2
) else (
    echo   [INFO] Docker not accessible from WSL2 ^(may need Docker Desktop WSL integration^)
)
echo.

REM --------------------------------------------------------------------------
REM [3/5] Check NVIDIA driver
REM --------------------------------------------------------------------------
:check_nvidia
echo [3/5] Checking NVIDIA driver...

where nvidia-smi >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo   [FAIL] nvidia-smi not found — NVIDIA driver not installed
    echo.
    echo          Install the latest NVIDIA driver:
    echo            https://www.nvidia.com/download/index.aspx
    echo.
    echo          For GPU in Docker, you need:
    echo            - Game Ready driver ^>= 525.60
    echo            - OR Studio driver ^>= 528.24
    echo.
    set /a ERRORS+=1
    goto :check_docker
)

for /f "tokens=*" %%g in ('nvidia-smi --query-gpu=driver_version --format^=csv^,noheader 2^>nul') do set "DRIVER_VER=%%g"
for /f "tokens=*" %%g in ('nvidia-smi --query-gpu=name --format^=csv^,noheader 2^>nul') do set "GPU_NAME=%%g"
for /f "tokens=*" %%g in ('nvidia-smi --query-gpu=memory.total --format^=csv^,noheader 2^>nul') do set "GPU_MEM=%%g"

echo   [OK]   NVIDIA Driver %DRIVER_VER%
echo          GPU:    %GPU_NAME%
echo          Memory: %GPU_MEM%

REM Check minimum driver version
for /f "tokens=1 delims=." %%m in ("%DRIVER_VER%") do set "DRV_MAJOR=%%m"
if %DRV_MAJOR% LSS 525 (
    echo   [WARN] Driver %DRIVER_VER% ^< 525.60 ^(minimum for CUDA 12.x^)
    echo          Please update your NVIDIA driver.
    set /a WARNINGS+=1
)
echo.

REM --------------------------------------------------------------------------
REM [4/5] Check Docker Desktop
REM --------------------------------------------------------------------------
:check_docker
echo [4/5] Checking Docker Desktop...

where docker >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo   [FAIL] Docker is not installed
    echo          Install: https://www.docker.com/products/docker-desktop
    set /a ERRORS+=1
    goto :check_gpu_docker
)

docker info >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo   [FAIL] Docker daemon is not running. Start Docker Desktop first.
    set /a ERRORS+=1
    goto :check_gpu_docker
)

echo   [OK]   Docker Desktop is running

REM Check if WSL2 backend is used
docker info 2>nul | findstr /i "WSL" >nul 2>nul
if %ERRORLEVEL% equ 0 (
    echo   [OK]   Docker Desktop is using WSL2 backend
) else (
    echo   [WARN] Docker Desktop may not be using WSL2 backend
    echo          Enable in: Docker Desktop ^> Settings ^> General ^> Use the WSL2 based engine
    set /a WARNINGS+=1
)
echo.

REM --------------------------------------------------------------------------
REM [5/5] Test GPU in Docker
REM --------------------------------------------------------------------------
:check_gpu_docker
echo [5/5] Testing GPU access in Docker...

if %ERRORS% GTR 0 (
    echo   [SKIP] Cannot test GPU in Docker — fix errors above first
    goto :summary
)

echo   Running: docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi
echo.

docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi 2>nul
if %ERRORLEVEL% equ 0 (
    echo.
    echo   [OK]   GPU is accessible inside Docker containers!
) else (
    echo.
    echo   [FAIL] GPU not accessible inside Docker containers
    echo.
    echo          Common fixes:
    echo            1. Make sure Docker Desktop uses WSL2 backend
    echo               Docker Desktop ^> Settings ^> General ^> Use the WSL2 based engine
    echo            2. Update NVIDIA driver to latest
    echo               https://www.nvidia.com/download/index.aspx
    echo            3. Restart Docker Desktop after driver update
    echo            4. Restart computer if WSL2 was just installed
    echo.
    set /a ERRORS+=1
)
echo.

REM --------------------------------------------------------------------------
REM Summary
REM --------------------------------------------------------------------------
:summary
echo  ========================================================
echo    Summary
echo  ========================================================
echo.

if %ERRORS% equ 0 (
    echo   All checks passed! GPU is ready for Docker.
    echo.
    echo   Start with GPU:
    echo     start.bat --gpu
    echo     :: or:
    echo     docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d
    echo.
    if %WARNINGS% GTR 0 (
        echo   %WARNINGS% warning^(s^) — review the output above.
    )
) else (
    echo   %ERRORS% error^(s^) found. Fix them and re-run this script.
    echo.
    echo   After fixing:
    echo     scripts\setup-gpu.bat
    echo.
    echo   Or use CPU mode ^(no GPU required^):
    echo     start.bat
)

echo  ========================================================
echo.
pause
exit /b %ERRORS%
