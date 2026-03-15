@echo off
REM ============================================================================
REM CloudAI Fusion - Environment Variable Generator (Windows)
REM Generates .env with cryptographically secure secrets
REM ============================================================================
REM Usage:
REM   scripts\env-generate.bat              (interactive — dev profile)
REM   scripts\env-generate.bat --profile dev
REM   scripts\env-generate.bat --profile prod
REM   scripts\env-generate.bat --non-interactive
REM ============================================================================
setlocal enabledelayedexpansion
title CloudAI Fusion - Env Generator

set "SCRIPT_DIR=%~dp0"
set "PROJECT_DIR=%SCRIPT_DIR%.."
set "ENV_FILE=%PROJECT_DIR%\.env"
set "PROFILE=dev"
set "INTERACTIVE=1"

REM Parse arguments
:parse_args
if "%~1"=="" goto :done_args
if /i "%~1"=="--profile" (
    set "PROFILE=%~2"
    shift
    shift
    goto :parse_args
)
if /i "%~1"=="--non-interactive" (
    set "INTERACTIVE=0"
    shift
    goto :parse_args
)
if /i "%~1"=="--help" goto :show_help
shift
goto :parse_args
:done_args

echo.
echo  ==============================================================
echo    CloudAI Fusion - Environment Generator
echo    Profile: %PROFILE%
echo  ==============================================================
echo.

REM Check if .env already exists
if exist "%ENV_FILE%" (
    echo [WARN] .env already exists at %ENV_FILE%
    if "%INTERACTIVE%"=="1" (
        set /p "OVERWRITE=Overwrite? [y/N]: "
        if /i not "!OVERWRITE!"=="y" (
            echo Aborted.
            exit /b 0
        )
    ) else (
        echo       Non-interactive mode: creating .env.generated instead
        set "ENV_FILE=%PROJECT_DIR%\.env.generated"
    )
)

REM --------------------------------------------------------------------------
REM Generate cryptographically secure random hex strings via PowerShell
REM --------------------------------------------------------------------------
echo [1/5] Generating secure secrets...

for /f "delims=" %%i in ('powershell -NoProfile -Command "[System.BitConverter]::ToString((1..32 | ForEach-Object { Get-Random -Minimum 0 -Maximum 256 })).Replace('-','')"') do set "JWT_SECRET=%%i"
for /f "delims=" %%i in ('powershell -NoProfile -Command "[System.BitConverter]::ToString((1..16 | ForEach-Object { Get-Random -Minimum 0 -Maximum 256 })).Replace('-','')"') do set "DB_PASSWORD=%%i"
for /f "delims=" %%i in ('powershell -NoProfile -Command "[System.BitConverter]::ToString((1..16 | ForEach-Object { Get-Random -Minimum 0 -Maximum 256 })).Replace('-','')"') do set "GF_PASSWORD=%%i"
for /f "delims=" %%i in ('powershell -NoProfile -Command "[System.BitConverter]::ToString((1..16 | ForEach-Object { Get-Random -Minimum 0 -Maximum 256 })).Replace('-','')"') do set "REDIS_PASSWORD=%%i"

echo       JWT Secret:   %JWT_SECRET:~0,8%...  (64 hex chars)
echo       DB Password:  %DB_PASSWORD:~0,8%...  (32 hex chars)

REM --------------------------------------------------------------------------
REM Profile-specific defaults
REM --------------------------------------------------------------------------
echo.
echo [2/5] Applying profile: %PROFILE%

if /i "%PROFILE%"=="prod" (
    set "LOG_LEVEL=warn"
    set "ENV_NAME=production"
    set "DB_SSLMODE=require"
    set "REDIS_AUTH=true"
) else if /i "%PROFILE%"=="staging" (
    set "LOG_LEVEL=info"
    set "ENV_NAME=staging"
    set "DB_SSLMODE=require"
    set "REDIS_AUTH=false"
) else (
    set "LOG_LEVEL=debug"
    set "ENV_NAME=development"
    set "DB_SSLMODE=disable"
    set "REDIS_AUTH=false"
)

echo       Environment:  %ENV_NAME%
echo       Log Level:    %LOG_LEVEL%
echo       DB SSL Mode:  %DB_SSLMODE%

REM --------------------------------------------------------------------------
REM Optional: LLM API keys (interactive only)
REM --------------------------------------------------------------------------
set "OPENAI_KEY="
set "DASHSCOPE_KEY="
set "OLLAMA_URL="

echo.
echo [3/5] LLM Integration (optional)

if "%INTERACTIVE%"=="1" (
    set /p "OPENAI_KEY=  OpenAI API Key  [skip]: "
    set /p "DASHSCOPE_KEY=  DashScope API Key [skip]: "
    set /p "OLLAMA_URL=  Ollama Base URL  [http://localhost:11434/v1]: "
)
if "!OLLAMA_URL!"=="" set "OLLAMA_URL=http://localhost:11434/v1"

REM --------------------------------------------------------------------------
REM Write .env file
REM --------------------------------------------------------------------------
echo.
echo [4/5] Writing %ENV_FILE%...

(
echo # ============================================================================
echo # CloudAI Fusion - Environment Variables
echo # Generated: %DATE% %TIME%
echo # Profile: %PROFILE%
echo # ============================================================================
echo # SECURITY: Never commit this file to version control!
echo # Regenerate: scripts\env-generate.bat --profile %PROFILE%
echo # ============================================================================
echo.
echo # --------------------------------------------------------------------------
echo # Environment
echo # --------------------------------------------------------------------------
echo CLOUDAI_ENV=%ENV_NAME%
echo CLOUDAI_LOG_LEVEL=%LOG_LEVEL%
echo.
echo # --------------------------------------------------------------------------
echo # Database ^(PostgreSQL^) - REQUIRED
echo # --------------------------------------------------------------------------
echo CLOUDAI_DB_PASSWORD=%DB_PASSWORD%
echo CLOUDAI_DB_SSLMODE=%DB_SSLMODE%
echo.
echo # --------------------------------------------------------------------------
echo # Authentication - REQUIRED
echo # --------------------------------------------------------------------------
echo CLOUDAI_JWT_SECRET=%JWT_SECRET%
echo.
echo # --------------------------------------------------------------------------
echo # Grafana Admin
echo # --------------------------------------------------------------------------
echo GF_ADMIN_PASSWORD=%GF_PASSWORD%
echo.
echo # --------------------------------------------------------------------------
echo # Redis
echo # --------------------------------------------------------------------------
) > "%ENV_FILE%"

if /i "%REDIS_AUTH%"=="true" (
    echo CLOUDAI_REDIS_PASSWORD=%REDIS_PASSWORD%>> "%ENV_FILE%"
) else (
    echo # CLOUDAI_REDIS_PASSWORD=>> "%ENV_FILE%"
)

(
echo.
echo # --------------------------------------------------------------------------
echo # LLM Integration ^(optional^)
echo # --------------------------------------------------------------------------
) >> "%ENV_FILE%"

if not "!OPENAI_KEY!"=="" (
    echo OPENAI_API_KEY=!OPENAI_KEY!>> "%ENV_FILE%"
) else (
    echo # OPENAI_API_KEY=sk-...>> "%ENV_FILE%"
)

if not "!DASHSCOPE_KEY!"=="" (
    echo DASHSCOPE_API_KEY=!DASHSCOPE_KEY!>> "%ENV_FILE%"
) else (
    echo # DASHSCOPE_API_KEY=sk-...>> "%ENV_FILE%"
)

(
echo OLLAMA_BASE_URL=%OLLAMA_URL%
echo # VLLM_BASE_URL=http://localhost:8000/v1
echo.
echo # --------------------------------------------------------------------------
echo # Monitoring
echo # --------------------------------------------------------------------------
echo # OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
) >> "%ENV_FILE%"

echo       Done.

REM --------------------------------------------------------------------------
REM Summary
REM --------------------------------------------------------------------------
echo.
echo [5/5] Summary
echo.
echo  ==============================================================
echo    .env generated successfully!
echo  ==============================================================
echo.
echo    File:      %ENV_FILE%
echo    Profile:   %PROFILE% (%ENV_NAME%)
echo    Secrets:   Auto-generated (cryptographically secure)
echo.
echo    Next steps:
echo      1. Review the .env file
echo      2. Run: docker compose up -d
echo      3. Run: scripts\diagnose.bat  (verify health)
echo  ==============================================================
echo.

if "%INTERACTIVE%"=="1" pause
exit /b 0

:show_help
echo Usage: scripts\env-generate.bat [OPTIONS]
echo.
echo Options:
echo   --profile dev^|staging^|prod   Environment profile (default: dev)
echo   --non-interactive             Skip interactive prompts
echo   --help                        Show this help
echo.
echo Profiles:
echo   dev       Debug logging, no SSL, no Redis auth
echo   staging   Info logging, SSL required, no Redis auth
echo   prod      Warn logging, SSL required, Redis auth enabled
exit /b 0
