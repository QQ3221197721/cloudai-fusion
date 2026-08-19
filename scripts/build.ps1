#Requires -Version 5.0
<#
.SYNOPSIS
    Build the cafctl control-plane CLI on Windows / PowerShell.

.DESCRIPTION
    Compiles cmd/cafctl into cafctl.exe at the repository root, injecting version
    metadata via -ldflags (matching the Makefile's LDFLAGS). Run it from the
    repository root:

        cd cloudai-fusion
        .\scripts\build.ps1
        .\cafctl.exe zk-demo generate --help

.PARAMETER Output
    Output binary path. Defaults to .\cafctl.exe in the repository root.
#>
[CmdletBinding()]
param(
    [string]$Output = ""
)

$ErrorActionPreference = "Stop"

# Resolve the repository root (parent of this scripts/ directory) so the script
# works regardless of the caller's current directory.
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot  = Split-Path -Parent $scriptDir

if ([string]::IsNullOrWhiteSpace($Output)) {
    $Output = Join-Path $repoRoot "cafctl.exe"
}

Push-Location $repoRoot
try {
    # Best-effort version metadata; fall back to sane defaults if git is absent.
    $version   = "0.1.0-dev"
    $gitCommit = "unknown"
    $treeState = "unknown"
    try { $version   = (git describe --tags --always --dirty 2>$null); if (-not $version)   { $version   = "0.1.0-dev" } } catch {}
    try { $gitCommit = (git rev-parse --short HEAD 2>$null);           if (-not $gitCommit) { $gitCommit = "unknown" } } catch {}
    try {
        $porcelain = (git status --porcelain 2>$null)
        $treeState = if ([string]::IsNullOrWhiteSpace($porcelain)) { "clean" } else { "dirty" }
    } catch {}
    $buildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    $versionPkg = "github.com/cloudai-fusion/cloudai-fusion/pkg/version"
    $ldflags = "-X $versionPkg.Version=$version " +
               "-X $versionPkg.GitCommit=$gitCommit " +
               "-X $versionPkg.GitTreeState=$treeState " +
               "-X $versionPkg.BuildTime=$buildTime"

    Write-Host "Building cafctl.exe ..." -ForegroundColor Cyan
    Write-Host "  version=$version commit=$gitCommit tree=$treeState" -ForegroundColor DarkGray
    Write-Host "  output=$Output" -ForegroundColor DarkGray

    & go build -trimpath -ldflags $ldflags -o $Output ./cmd/cafctl
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }

    Write-Host "Build succeeded: $Output" -ForegroundColor Green
    Write-Host "Try: .\cafctl.exe zk-demo generate --help" -ForegroundColor Yellow
}
finally {
    Pop-Location
}
