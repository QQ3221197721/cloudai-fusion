@echo off
REM ============================================================================
REM CloudAI Fusion - Unified Health Check & Diagnostic Tool (Windows)
REM ============================================================================
REM Usage:
REM   scripts\diagnose.bat              Full diagnostic
REM   scripts\diagnose.bat --quick      Quick health check only
REM   scripts\diagnose.bat --logs       Show recent error logs
REM   scripts\diagnose.bat --fix        Attempt auto-fix common issues
REM ============================================================================
setlocal enabledelayedexpansion
title CloudAI Fusion - Diagnostics

set "MODE=full"
set "PASS=0"
set "FAIL=0"
set "WARN=0"

REM Parse arguments
if /i "%~1"=="--quick" set "MODE=quick"
if /i "%~1"=="--logs"  set "MODE=logs"
if /i "%~1"=="--fix"   set "MODE=fix"
if /i "%~1"=="--help"  goto :show_help

echo.
echo  ==============================================================
echo    CloudAI Fusion - Deployment Diagnostics
echo    Mode: %MODE%
echo    Time: %DATE% %TIME%
echo  ==============================================================
echo.

REM ========================================================================
REM Section 1: Prerequisites
REM ========================================================================
echo [1/6] Prerequisites
echo ---------------------------------------------------------------

where docker >nul 2>nul
if %ERRORLEVEL% equ 0 (
    echo   [PASS] Docker installed
    set /a PASS+=1
) else (
    echo   [FAIL] Docker not found
    set /a FAIL+=1
)

docker info >nul 2>nul
if %ERRORLEVEL% equ 0 (
    echo   [PASS] Docker daemon running
    set /a PASS+=1
) else (
    echo   [FAIL] Docker daemon not running
    echo          Fix: Start Docker Desktop
    set /a FAIL+=1
)

docker compose version >nul 2>nul
if %ERRORLEVEL% equ 0 (
    echo   [PASS] Docker Compose V2 available
    set /a PASS+=1
) else (
    echo   [FAIL] Docker Compose V2 not available
    set /a FAIL+=1
)

if exist ".env" (
    echo   [PASS] .env file exists
    set /a PASS+=1
) else (
    echo   [FAIL] .env file missing
    echo          Fix: Run scripts\env-generate.bat
    set /a FAIL+=1
)

if "%MODE%"=="quick" goto :section_services

REM ========================================================================
REM Section 2: Port Availability
REM ========================================================================
echo.
echo [2/6] Port Availability
echo ---------------------------------------------------------------

set "PORTS=8080 8081 8082 8090 5432 6379 9092 4222 9090 3000 16686"
for %%p in (%PORTS%) do (
    netstat -an 2>nul | findstr ":%%p " | findstr "LISTENING" >nul 2>nul
    if !ERRORLEVEL! equ 0 (
        echo   [INFO] Port %%p is in use (expected if services running)
    ) else (
        echo   [    ] Port %%p is free
    )
)

REM ========================================================================
REM Section 3: Container Status
REM ========================================================================
:section_services
echo.
echo [3/6] Container Status
echo ---------------------------------------------------------------

set "SERVICES=postgres redis kafka nats ai-engine apiserver scheduler agent prometheus grafana jaeger"
for %%s in (%SERVICES%) do (
    for /f "tokens=*" %%r in ('docker compose ps --format "{{.State}}" %%s 2^>nul') do set "STATE_%%s=%%r"
    if defined STATE_%%s (
        echo !STATE_%%s! | findstr /i "running" >nul 2>nul
        if !ERRORLEVEL! equ 0 (
            echo   [PASS] %%s: running
            set /a PASS+=1
        ) else (
            echo   [FAIL] %%s: !STATE_%%s!
            set /a FAIL+=1
        )
    ) else (
        echo   [WARN] %%s: not started
        set /a WARN+=1
    )
)

if "%MODE%"=="quick" goto :summary

REM ========================================================================
REM Section 4: Health Endpoints
REM ========================================================================
echo.
echo [4/6] Health Endpoints
echo ---------------------------------------------------------------

REM Infrastructure health
call :check_health "PostgreSQL"   "localhost" "5432" "tcp"
call :check_health "Redis"        "localhost" "6379" "tcp"
call :check_health "Kafka"        "localhost" "9092" "tcp"
call :check_health "NATS"         "http://localhost:8222/healthz" "" "http"
call :check_health "NATS Client"  "localhost" "4222" "tcp"

REM Application health
call :check_health "API Server"   "http://localhost:8080/healthz" "" "http"
call :check_health "Scheduler"    "http://localhost:8081/healthz" "" "http"
call :check_health "Agent"        "http://localhost:8082/healthz" "" "http"
call :check_health "AI Engine"    "http://localhost:8090/healthz" "" "http"

REM Monitoring health
call :check_health "Prometheus"   "http://localhost:9090/-/healthy" "" "http"
call :check_health "Grafana"      "http://localhost:3000/api/health" "" "http"
call :check_health "Jaeger"       "http://localhost:16686/" "" "http"

REM ========================================================================
REM Section 5: Dependency Connectivity (from inside containers)
REM ========================================================================
echo.
echo [5/6] Cross-Service Connectivity
echo ---------------------------------------------------------------

REM Check apiserver can reach postgres
docker compose exec -T apiserver sh -c "curl -sf http://localhost:8080/readyz 2>/dev/null" >nul 2>nul
if %ERRORLEVEL% equ 0 (
    echo   [PASS] apiserver -^> readyz (DB connected)
    set /a PASS+=1
) else (
    echo   [WARN] apiserver readyz check failed (may still be starting)
    set /a WARN+=1
)

REM ========================================================================
REM Section 6: Recent Errors
REM ========================================================================
echo.
echo [6/6] Recent Errors (last 5 lines per service)
echo ---------------------------------------------------------------

for %%s in (apiserver scheduler agent ai-engine) do (
    echo.
    echo   --- %%s ---
    docker compose logs --tail=5 %%s 2>nul | findstr /i "error fatal panic" 2>nul
    if !ERRORLEVEL! neq 0 echo   (no errors found)
)

goto :summary

REM ========================================================================
REM Health check helpers
REM ========================================================================
:check_health
set "SNAME=%~1"
set "ADDR=%~2"
set "PORT=%~3"
set "TYPE=%~4"

if "%TYPE%"=="http" (
    curl -sf --max-time 3 "%ADDR%" >nul 2>nul
    if !ERRORLEVEL! equ 0 (
        echo   [PASS] %SNAME%: healthy
        set /a PASS+=1
    ) else (
        echo   [FAIL] %SNAME%: unreachable (%ADDR%)
        set /a FAIL+=1
    )
) else (
    powershell -NoProfile -Command "try { $c = New-Object System.Net.Sockets.TcpClient('%ADDR%', %PORT%); $c.Close(); exit 0 } catch { exit 1 }" >nul 2>nul
    if !ERRORLEVEL! equ 0 (
        echo   [PASS] %SNAME%: port %PORT% open
        set /a PASS+=1
    ) else (
        echo   [FAIL] %SNAME%: port %PORT% closed
        set /a FAIL+=1
    )
)
exit /b

REM ========================================================================
REM Summary
REM ========================================================================
:summary
echo.
echo  ==============================================================
echo    Diagnostic Summary
echo  ==============================================================
echo.
echo    PASS: %PASS%   FAIL: %FAIL%   WARN: %WARN%
echo.

if %FAIL% equ 0 (
    echo    Status: ALL CHECKS PASSED
    echo.
    echo    Services: http://localhost:8080  (API Server)
    echo              http://localhost:3000  (Grafana, admin/cloudai)
    echo              http://localhost:16686 (Jaeger)
) else (
    echo    Status: %FAIL% ISSUE(S) DETECTED
    echo.
    echo    Common fixes:
    echo      1. Generate .env:   scripts\env-generate.bat
    echo      2. Start services:  docker compose up -d
    echo      3. Check logs:      docker compose logs -f [service]
    echo      4. Restart:         docker compose restart [service]
    echo      5. Full reset:      docker compose down -v ^&^& docker compose up -d
)

echo.
echo  ==============================================================
echo.

if "%MODE%"=="fix" (
    if %FAIL% gtr 0 (
        echo Attempting auto-fix...
        if not exist ".env" (
            echo   Generating .env...
            call scripts\env-generate.bat --non-interactive
        )
        echo   Restarting failed services...
        docker compose up -d
        echo   Waiting 30 seconds for services to stabilize...
        timeout /t 30 /nobreak >nul
        echo   Re-running diagnostics...
        call scripts\diagnose.bat --quick
    )
)

pause
exit /b 0

:show_help
echo Usage: scripts\diagnose.bat [OPTIONS]
echo.
echo Options:
echo   --quick    Quick health check (containers + endpoints only)
echo   --logs     Show recent error logs from all services
echo   --fix      Attempt auto-fix common issues
echo   --help     Show this help
exit /b 0
