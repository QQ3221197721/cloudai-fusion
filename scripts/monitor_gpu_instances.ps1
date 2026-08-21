<#
.SYNOPSIS
    GPU (A100/H100) availability monitor for Alibaba Cloud ECS — PowerShell native.

.DESCRIPTION
    Uses `aliyun ecs DescribeAvailableResource` (the CORRECT API for purchasable
    stock, unlike DescribeInstanceStatus which only lists instances you already
    own) to poll target regions for the gn7e 8x A100 SKU (ecs.gn7e-c16g1.32xlarge)
    needed for multi-GPU M2/M3 NVLink topology validation, plus smaller gn7e
    fallbacks.

    DETECTION is fully automatic and loud. When stock flips to available it writes
    a clear alert to logs/gpu_alerts.log and prints a banner.

    PROVISIONING is intentionally NOT automatic. To avoid accidental spend, an
    actual CreateInstance call would only run when the environment variable
    CONFIRM_PROVISION=yes is set. This script only DETECTS + ALERTS; it never
    spends money on its own. (See -ProvisionHook to wire in a gated action.)

.PARAMETER Once
    Run a single probe pass across all regions and exit (used for the live probe).

.PARAMETER IntervalSeconds
    Seconds between probe passes in continuous mode. Default 300 (5 minutes).

.EXAMPLE
    # Single ground-truth probe right now:
    powershell -ExecutionPolicy Bypass -File scripts\monitor_gpu_instances.ps1 -Once

.EXAMPLE
    # Continuous background monitor (every 5 min):
    Start-Process powershell -ArgumentList '-ExecutionPolicy','Bypass','-File','scripts\monitor_gpu_instances.ps1' -WindowStyle Hidden
#>

param(
    [switch]$Once,
    [int]$IntervalSeconds = 300,
    [string]$AliyunExe = "E:\aliyun-cli\aliyun.exe",
    [string[]]$Regions = @(
        "cn-hangzhou",
        "cn-shenzhen",
        "cn-wulanchabu",
        "cn-beijing",
        "cn-shanghai"
    ),
    # gn7e (A100 80GB) family. PRIMARY target is the x8 NVLink SKU needed for
    # multi-GPU M2/M3 topology validation; smaller gn7e sizes are kept as
    # fallbacks so we still learn regional stock signal if the x8 is sold out.
    [string[]]$InstanceTypes = @(
        "ecs.gn7e-c16g1.32xlarge",  # A100 80GB x8 (PRIMARY — 8x NVLink for M2/M3 multi-GPU topology)
        "ecs.gn7e-c16g1.16xlarge",  # A100 80GB x4 (fallback)
        "ecs.gn7e-c16g1.8xlarge",   # A100 80GB x2 (fallback)
        "ecs.gn7e-c16g1.4xlarge"    # A100 80GB x1 (fallback — the single-card SKU just released)
    ),
    [string]$InstanceChargeType = "PostPaid",
    # Optional: path to a script that performs the actual (gated) provisioning.
    # Only invoked when $env:CONFIRM_PROVISION -eq 'yes'.
    [string]$ProvisionHook = ""
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
$ScriptDir   = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir
$LogDir      = Join-Path $ProjectRoot "logs"
$LogFile     = Join-Path $LogDir "gpu_monitor.log"
$AlertFile   = Join-Path $LogDir "gpu_alerts.log"

if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Force -Path $LogDir | Out-Null }

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
function Write-Log {
    param([string]$Level, [string]$Message)
    $ts = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $line = "$ts [$Level] $Message"
    switch ($Level) {
        "INFO"  { Write-Host $line -ForegroundColor Green }
        "WARN"  { Write-Host $line -ForegroundColor Yellow }
        "ERROR" { Write-Host $line -ForegroundColor Red }
        "ALERT" { Write-Host $line -ForegroundColor Magenta }
        default { Write-Host $line }
    }
    Add-Content -Path $LogFile -Value $line -Encoding UTF8
}

function Write-Alert {
    param([string]$Region, [string]$InstanceType, [string]$Status, [string]$ZoneId)
    $ts = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $banner = @"
========================================================================
  !!!  GPU STOCK DETECTED  !!!
  Time        : $ts
  Region      : $Region
  Zone        : $ZoneId
  InstanceType: $InstanceType
  Status      : $Status
  ACTION      : Detection is automatic. Provisioning is GATED.
                To provision, set CONFIRM_PROVISION=yes and run the
                provisioning hook. This monitor does NOT spend on its own.
========================================================================
"@
    Write-Host $banner -ForegroundColor Magenta -BackgroundColor Black
    Add-Content -Path $AlertFile -Value $banner -Encoding UTF8
    Write-Log "ALERT" "STOCK: $Region/$ZoneId $InstanceType -> $Status"

    # Gated provisioning hook (never runs unless explicitly confirmed).
    if ($env:CONFIRM_PROVISION -eq "yes" -and $ProvisionHook -ne "" -and (Test-Path $ProvisionHook)) {
        Write-Log "WARN" "CONFIRM_PROVISION=yes -> invoking provisioning hook: $ProvisionHook"
        try {
            & $ProvisionHook -Region $Region -InstanceType $InstanceType -ZoneId $ZoneId
        } catch {
            Write-Log "ERROR" "Provisioning hook failed: $($_.Exception.Message)"
        }
    } else {
        Write-Log "INFO" "Provisioning NOT triggered (CONFIRM_PROVISION!=yes or no hook)."
    }
}

# ---------------------------------------------------------------------------
# Probe a single region + instance type.
# Returns a PSCustomObject: Region, InstanceType, Status, ZoneId, Detail
# Status values: Available | SoldOut | NotOffered | Error
# ---------------------------------------------------------------------------
function Probe-Region {
    param([string]$Region, [string]$InstanceType)

    $raw = ""
    try {
        $raw = & $AliyunExe ecs DescribeAvailableResource `
            --RegionId $Region `
            --DestinationResource InstanceType `
            --InstanceChargeType $InstanceChargeType `
            --IoOptimized optimized `
            --InstanceType $InstanceType 2>&1 | Out-String
    } catch {
        return [PSCustomObject]@{ Region=$Region; InstanceType=$InstanceType; Status="Error"; ZoneId=""; Detail=$_.Exception.Message }
    }

    # Aliyun CLI prints SDK errors as plain text (not JSON) on failure.
    if ($raw -match "ErrorCode|SDK\.ServerError|InvalidInstanceType|does not exist|Forbidden") {
        $code = "unknown"
        if ($raw -match "ErrorCode:\s*(\S+)") { $code = $Matches[1] }
        # A region simply not offering this SKU is "NotOffered", not a hard error.
        if ($raw -match "not.*(exist|support|offer)|InvalidInstanceType") {
            return [PSCustomObject]@{ Region=$Region; InstanceType=$InstanceType; Status="NotOffered"; ZoneId=""; Detail=$code }
        }
        return [PSCustomObject]@{ Region=$Region; InstanceType=$InstanceType; Status="Error"; ZoneId=""; Detail=$code }
    }

    $json = $null
    try { $json = $raw | ConvertFrom-Json } catch {
        return [PSCustomObject]@{ Region=$Region; InstanceType=$InstanceType; Status="Error"; ZoneId=""; Detail="unparseable response" }
    }

    $zones = @()
    if ($json.AvailableZones -and $json.AvailableZones.AvailableZone) {
        $zones = @($json.AvailableZones.AvailableZone)
    }

    if ($zones.Count -eq 0) {
        return [PSCustomObject]@{ Region=$Region; InstanceType=$InstanceType; Status="NotOffered"; ZoneId=""; Detail="no zones returned" }
    }

    # Zone.Status: "Available" (some stock) vs "SoldOut". Prefer an available zone.
    $availZone = $zones | Where-Object { $_.Status -eq "Available" } | Select-Object -First 1
    if ($availZone) {
        # Drill into supported resource status for extra confidence.
        $srStatus = "Available"
        try {
            $sr = $availZone.AvailableResources.AvailableResource.SupportedResources.SupportedResource `
                | Where-Object { $_.Value -eq $InstanceType } | Select-Object -First 1
            if ($sr -and $sr.Status) { $srStatus = $sr.Status }  # WithStock / Available / NoStock
        } catch {}
        return [PSCustomObject]@{ Region=$Region; InstanceType=$InstanceType; Status=$srStatus; ZoneId=$availZone.ZoneId; Detail="zone available" }
    }

    $firstZone = $zones | Select-Object -First 1
    return [PSCustomObject]@{ Region=$Region; InstanceType=$InstanceType; Status="SoldOut"; ZoneId=$firstZone.ZoneId; Detail="all zones SoldOut" }
}

# ---------------------------------------------------------------------------
# One full pass across all regions x instance types. Returns results array.
# ---------------------------------------------------------------------------
function Invoke-ProbePass {
    $results = @()
    foreach ($region in $Regions) {
        foreach ($it in $InstanceTypes) {
            $r = Probe-Region -Region $region -InstanceType $it
            $results += $r
            $isAvail = ($r.Status -eq "Available" -or $r.Status -eq "WithStock")
            Write-Log "INFO" ("Probe {0,-14} {1,-26} => {2} {3}" -f $r.Region, $r.InstanceType, $r.Status, $(if($r.ZoneId){"($($r.ZoneId))"}else{""}))
            # LOUD alert only for the PRIMARY 8x A100 SKU (the multi-GPU target).
            # Fallback SKUs (x1/x2/x4) are logged at INFO only to avoid spamming
            # gpu_alerts.log — the x1 4xlarge is frequently in stock.
            if ($isAvail -and $it -eq $InstanceTypes[0]) {
                Write-Alert -Region $r.Region -InstanceType $r.InstanceType -Status $r.Status -ZoneId $r.ZoneId
            } elseif ($isAvail) {
                Write-Log "INFO" ("Fallback SKU in stock (not alerting): {0} {1} @ {2}" -f $r.Region, $r.InstanceType, $r.ZoneId)
            }
        }
    }
    return $results
}

# ---------------------------------------------------------------------------
# Prerequisite check
# ---------------------------------------------------------------------------
if (-not (Test-Path $AliyunExe)) {
    Write-Log "ERROR" "aliyun.exe not found at $AliyunExe. Install it or pass -AliyunExe <path>."
    exit 1
}
$verOut = & $AliyunExe version 2>&1 | Out-String
Write-Log "INFO" "aliyun CLI version: $($verOut.Trim())"

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
Write-Log "INFO" "=== GPU availability monitor starting (Once=$Once, Interval=$IntervalSeconds s) ==="
Write-Log "INFO" ("Regions: {0}" -f ($Regions -join ", "))
Write-Log "INFO" ("InstanceTypes: {0}" -f ($InstanceTypes -join ", "))

if ($Once) {
    $res = Invoke-ProbePass
    Write-Log "INFO" "=== Single probe pass complete ==="

    Write-Host ""
    Write-Host "===== LIVE AVAILABILITY TABLE =====" -ForegroundColor Cyan
    $res | Sort-Object Region, InstanceType | Format-Table Region, InstanceType, Status, ZoneId, Detail -AutoSize | Out-String | Write-Host
    exit 0
}

# Continuous mode
while ($true) {
    Write-Log "INFO" "----- probe cycle start -----"
    Invoke-ProbePass | Out-Null
    Write-Log "INFO" "----- cycle complete; sleeping $IntervalSeconds s -----"
    Start-Sleep -Seconds $IntervalSeconds
}
