@echo off
REM ============================================================================
REM CloudAI Fusion - Quick Start Script (Windows)
REM ============================================================================
REM Modes:
REM   start.bat              -- Standard profile, CPU mode (~60-90s)
REM   start.bat --minimal    -- Minimal profile, core only, fastest startup
REM   start.bat --full       -- Full profile + monitoring stack (~2-3 min)
REM   start.bat --gpu        -- GPU mode (NVIDIA CUDA via WSL2)
REM   start.bat --fast       -- Skip health wait (~30-45s)
REM   start.bat --stop       -- Stop all services
REM
REM Feature profiles:
REM   --minimal   Core only (API/Scheduler/Agent + DB/Redis). Fastest startup.
REM   (default)   Standard -- monitoring, edge, WASM, security, cost.
REM   --full      Everything including experimental (RL, LLM ops, multi-cluster).
REM
REM GPU support:
REM   Requires: WSL2 + NVIDIA driver + Docker Desktop WSL2 backend
REM   Setup:    scripts\setup-gpu.bat
REM ============================================================================
title CloudAI Fusion Platform
setlocal enabledelayedexpansion

set "MODE=core"
set "GPU_MODE=0"
set "SKIP_HEALTH=0"
set "STEP_TOTAL=5"
set "COMPOSE_FILES=-f docker-compose.yml"
set "DEVICE_LABEL=CPU"
set "FEATURE_PROFILE=standard"

REM Parse arguments
for %%A in (%*) do (
    if /I "%%A"=="--fast" (
        set "MODE=fast"
        set "SKIP_HEALTH=1"
    )
    if /I "%%A"=="--full" (
        set "MODE=full"
        set "FEATURE_PROFILE=full"
        set /a STEP_TOTAL+=1
    )
    if /I "%%A"=="--minimal" (
        set "MODE=minimal"
        set "FEATURE_PROFILE=minimal"
    )
    if /I "%%A"=="--gpu" (
        set "GPU_MODE=1"
        set /a STEP_TOTAL+=1
    )
    if /I "%%A"=="--stop" (
        echo Stopping all services...
        docker compose --profile monitoring down 2>nul
        if !ERRORLEVEL! neq 0 docker compose down
        echo All services stopped.
        exit /b 0
    )
    if /I "%%A"=="--help" (
        echo Usage: start.bat [--minimal^|--fast^|--full^|--gpu^|--stop]
        echo   (default)    Standard profile, CPU mode ~60-90s
        echo   --minimal    Minimal profile -- core services only, fastest startup
        echo   --gpu        Enable NVIDIA GPU for AI Engine ^(WSL2 required^)
        echo   --fast       Minimal health wait, ~30-45s
        echo   --full       Full profile + monitoring ^(Prometheus/Grafana/Jaeger^)
        echo   --stop       Stop all services
        echo.
        echo Feature profiles:
        echo   minimal   = Core only ^(no edge/wasm/mesh/monitoring^)
        echo   standard  = Production-ready features ^(default^)
        echo   full      = Everything enabled ^(experimental included^)
        echo.
        echo GPU setup:    scripts\setup-gpu.bat
        echo Diagnostics:  scripts\diagnose.bat
        exit /b 0
    )
)

REM Export feature profile for containers
set "CLOUDAI_FEATURE_PROFILE=!FEATURE_PROFILE!"

REM --------------------------------------------------------------------------
REM GPU mode: WSL2 + NVIDIA check
REM --------------------------------------------------------------------------
if !GPU_MODE! equ 1 (
    echo.
    echo   Checking GPU prerequisites...

    set "GPU_OK=1"

    REM Check WSL2
    wsl --status >nul 2>nul
    if !ERRORLEVEL! neq 0 (
        echo   [FAIL] WSL2 is not installed or not enabled.
        echo          GPU passthrough on Windows requires WSL2.
        echo          Install: wsl --install
        set "GPU_OK=0"
    ) else (
        echo   [OK]   WSL2 available
    )

    REM Check NVIDIA driver
    where nvidia-smi >nul 2>nul
    if !ERRORLEVEL! neq 0 (
        echo   [FAIL] NVIDIA driver not found.
        echo          Install: https://www.nvidia.com/download/index.aspx
        set "GPU_OK=0"
    ) else (
        for /f "tokens=*" %%g in ('nvidia-smi --query-gpu=name --format^=csv^,noheader 2^>nul') do set "GPU_NAME=%%g"
        for /f "tokens=*" %%g in ('nvidia-smi --query-gpu=memory.total --format^=csv^,noheader 2^>nul') do set "GPU_MEM=%%g"
        echo   [OK]   NVIDIA GPU: !GPU_NAME! ^(!GPU_MEM!^)
    )

    REM Check Docker WSL2 backend
    docker info 2>nul | findstr /i "WSL" >nul 2>nul
    if !ERRORLEVEL! neq 0 (
        echo   [WARN] Docker Desktop may not be using WSL2 backend.
        echo          Enable: Docker Desktop ^> Settings ^> General ^> Use the WSL2 based engine
    )

    if !GPU_OK! equ 0 (
        echo.
        echo   GPU prerequisites not met. Falling back to CPU mode.
        echo   Run: scripts\setup-gpu.bat for setup instructions.
        set "GPU_MODE=0"
        set /a STEP_TOTAL-=1
    ) else (
        set "COMPOSE_FILES=-f docker-compose.yml -f docker-compose.gpu.yml"
        set "DEVICE_LABEL=GPU (NVIDIA CUDA via WSL2)"
    )
)

echo.
echo  ========================================================
echo    CloudAI Fusion - Cloud-Native AI Unified Management
echo    Mode: %MODE%  ^|  Profile: !FEATURE_PROFILE!  ^|  Device: !DEVICE_LABEL!
echo  ========================================================
echo.

REM --------------------------------------------------------------------------
REM Step 1: Check Docker
REM --------------------------------------------------------------------------
echo [1/%STEP_TOTAL%] Checking prerequisites...

where docker >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo   [ERROR] Docker is not installed or not in PATH.
    echo   Install: https://www.docker.com/products/docker-desktop
    pause
    exit /b 1
)

docker compose version >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo   [ERROR] Docker Compose V2 is not available.
    pause
    exit /b 1
)

docker info >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo   [ERROR] Docker daemon is not running. Start Docker Desktop first.
    pause
    exit /b 1
)

echo   Docker is running.

REM --------------------------------------------------------------------------
REM Step 2: Generate .env if missing
REM --------------------------------------------------------------------------
echo.
echo [2/%STEP_TOTAL%] Checking environment configuration...

if not exist .env (
    echo   .env not found. Generating with secure defaults...
    echo.
    call scripts\env-generate.bat --non-interactive
    echo.
) else (
    echo   .env already exists, using existing configuration.
)

REM --------------------------------------------------------------------------
REM Step 3: Start infrastructure
REM --------------------------------------------------------------------------
echo.
echo [3/%STEP_TOTAL%] Starting infrastructure services...
echo   (PostgreSQL, Redis, Kafka, NATS)

docker compose up -d postgres redis kafka nats

echo   Waiting for infrastructure to become healthy...
call :wait_healthy "postgres" 40
call :wait_healthy "redis" 20
call :wait_healthy "kafka" 60
call :wait_healthy "nats" 20

echo   Infrastructure ready.

REM --------------------------------------------------------------------------
REM Step 4: Start core services
REM --------------------------------------------------------------------------
echo.
echo [4/%STEP_TOTAL%] Starting core services... (device: !DEVICE_LABEL!)

set "AI_TIMEOUT=45"
if !GPU_MODE! equ 1 set "AI_TIMEOUT=90"

docker compose !COMPOSE_FILES! up -d ai-engine
call :wait_healthy "ai-engine" !AI_TIMEOUT!

docker compose !COMPOSE_FILES! up -d apiserver scheduler
call :wait_healthy "apiserver" 45
call :wait_healthy "scheduler" 45

docker compose !COMPOSE_FILES! up -d agent
call :wait_healthy "agent" 30

echo   Core services ready.

REM --------------------------------------------------------------------------
REM Step 5 (full mode): Start monitoring
REM --------------------------------------------------------------------------
if /I "%MODE%"=="full" (
    echo.
    echo [5/%STEP_TOTAL%] Starting monitoring stack...
    echo   (Prometheus, Grafana, Jaeger, Exporters)
    docker compose !COMPOSE_FILES! --profile monitoring up -d
    echo   Monitoring stack starting.
)

REM --------------------------------------------------------------------------
REM Summary
REM --------------------------------------------------------------------------
echo.
echo [%STEP_TOTAL%/%STEP_TOTAL%] Startup complete!
echo.
echo  ========================================================
echo    CloudAI Fusion is running!
echo    Mode: %MODE%  ^|  Profile: !FEATURE_PROFILE!  ^|  Device: !DEVICE_LABEL!
echo  ========================================================
echo.
echo    Services:
echo      API Server:   http://localhost:8080
echo      Scheduler:    http://localhost:8081
echo      Agent:        http://localhost:8082
echo      AI Engine:    http://localhost:8090  [!DEVICE_LABEL!]

if /I "%MODE%"=="full" (
echo.
echo    Monitoring:
echo      Prometheus:   http://localhost:9090
echo      Grafana:      http://localhost:3000  (admin/cloudai)
echo      Jaeger:       http://localhost:16686
echo      NATS Monitor: http://localhost:8222
)

echo.
echo    Quick Test:  curl http://localhost:8080/healthz
if /I NOT "%MODE%"=="full" (
echo    Add monitoring: start.bat --full
)
if /I NOT "!FEATURE_PROFILE!"=="full" (
echo    Full features:  start.bat --full
)
if /I "!FEATURE_PROFILE!"=="minimal" (
echo    Standard:       start.bat
)
if !GPU_MODE! equ 0 (
echo    Enable GPU:  start.bat --gpu ^(requires WSL2, run: scripts\setup-gpu.bat^)
)
echo    Feature flags:  curl http://localhost:8080/api/v1/features
echo    GPU setup:   scripts\setup-gpu.bat
echo    Diagnostics: scripts\diagnose.bat
echo    Stop:        docker compose --profile monitoring down
echo  ========================================================
echo.

pause
exit /b 0

REM --------------------------------------------------------------------------
REM Helper: wait for a container to become healthy
REM --------------------------------------------------------------------------
:wait_healthy
set "SVC=%~1"
set "TIMEOUT=%~2"
set "ELAPSED=0"

if %SKIP_HEALTH% equ 1 (
    echo   [SKIP] %SVC%: health wait skipped (--fast mode)
    exit /b 0
)

:wait_loop
if %ELAPSED% geq %TIMEOUT% (
    echo   [WARN] %SVC% did not become healthy within %TIMEOUT%s
    echo          Run: docker compose logs %SVC%
    exit /b 0
)

for /f "tokens=*" %%h in ('docker compose ps --format "{{.Health}}" %SVC% 2^>nul') do set "HEALTH=%%h"
echo !HEALTH! | findstr /i "healthy" >nul 2>nul
if !ERRORLEVEL! equ 0 (
    echo   [OK] %SVC%: healthy (%ELAPSED%s)
    exit /b 0
)

timeout /t 2 /nobreak >nul
set /a ELAPSED+=2
goto :wait_loop
