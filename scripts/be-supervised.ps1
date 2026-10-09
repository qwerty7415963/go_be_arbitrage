# be-supervised.ps1 — run ONE arbitrage-be instance under supervision.
#
# Why: `make run` occupies a terminal and dies silently on Ctrl+C / terminal
# close / double-start (bind conflict). This wrapper guarantees a single
# supervised instance with timestamped logs and automatic restart on crash.
#
# Usage (from the arbitrage_be repo root):
#   powershell -File scripts/be-supervised.ps1 [-Port 8080] [-SkipBuild]
#   powershell -File scripts/be-supervised.ps1 -Stop [-Port 8080]
#
# Behavior:
# - Refuses to start if <Port> is already listening (prints holder PID).
#   Never double-starts into a bind conflict.
# - Builds bin/arbitrage-be.exe once at start (unless -SkipBuild).
# - Restarts the child on abnormal exit with backoff 1s,2s,4s..30s.
# - Gives up after 3 consecutive fast failures (<8s each): that means a
#   broken binary or environment, not a flaky crash. Exit code 4.
# - Child exit code 0 (clean shutdown, e.g. shared-console Ctrl+C) stops
#   supervision without restart.
# - -Stop asks a running supervisor to stop the child and exit (via flag
#   file; safe even if PIDs were recycled).
#
# Files (repo root, all git-ignored):
#   logs/be-supervised-<port>.log       BE stdout (rotated to .prev on restart)
#   logs/be-supervised-<port>.err.log   BE stderr incl. app logs (rotated too)
#   .be-supervised-<port>.pid           supervisor + child PIDs
#   .be-supervised-<port>.stop          stop-request flag (removed on stop)
#
# Requires PowerShell 5.1+. No admin rights needed.

param(
    [int]$Port = 8080,
    [switch]$SkipBuild,
    [switch]$Stop
)

$ErrorActionPreference = 'Stop'

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = Split-Path -Parent $ScriptDir
# Resolve to absolute: -File with a relative path can leave these relative,
# which breaks child startup once the working directory shifts.
$ScriptDir = Convert-Path -LiteralPath $ScriptDir
$RepoRoot = Convert-Path -LiteralPath $RepoRoot
$Binary = Join-Path $RepoRoot 'bin\arbitrage-be.exe'
$LogsDir = Join-Path $RepoRoot 'logs'
$LogFile = Join-Path $LogsDir ("be-supervised-{0}.log" -f $Port)
$ErrFile = Join-Path $LogsDir ("be-supervised-{0}.err.log" -f $Port)
$PidFile = Join-Path $RepoRoot (".be-supervised-{0}.pid" -f $Port)
$StopFlag = Join-Path $RepoRoot (".be-supervised-{0}.stop" -f $Port)

# Last-resort error trap: without this, a startup failure in a hidden window
# dies silently (this exact blindness cost a debug round). Records the fatal
# error to the log file, then exits 5.
trap {
    try {
        $detail = ($_ | Out-String).Trim()
        $msg = "{0:yyyy-MM-dd HH:mm:ss} [supervisor] FATAL: {1}" -f (Get-Date), $detail
        Write-Host $msg
        Add-Content -LiteralPath $LogFile -Value $msg -ErrorAction SilentlyContinue
    } catch { }
    exit 5
}

function Write-Log([string]$Message) {
    $line = "{0:yyyy-MM-dd HH:mm:ss} [supervisor] {1}" -f (Get-Date), $Message
    Write-Host $line
    Add-Content -LiteralPath $LogFile -Value $line -ErrorAction SilentlyContinue
}

function Get-PortOwner([int]$CheckPort) {
    $conn = Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
        Where-Object { $_.LocalPort -eq $CheckPort } |
        Select-Object -First 1
    if ($conn) { return $conn.OwningProcess } else { return $null }
}

function Test-PidAlive([int]$ProcessId) {
    # NOTE: param must NOT be named $Pid ($PID is a read-only automatic var).
    if ($ProcessId -le 0) { return $false }
    return $null -ne (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)
}

function Read-PidFile {
    if (-not (Test-Path -LiteralPath $PidFile)) { return $null }
    $info = @{}
    foreach ($line in (Get-Content -LiteralPath $PidFile -ErrorAction SilentlyContinue)) {
        $parts = $line -split '=', 2
        if ($parts.Count -eq 2) { $info[$parts[0].Trim()] = $parts[1].Trim() }
    }
    return $info
}

# --- Stop mode: ask the supervisor to stop, don't start anything. ---
if ($Stop) {
    $info = Read-PidFile
    if (-not $info -or -not $info['supervisor']) {
        Write-Host "be-supervised: not running (no pidfile for port $Port)."
        exit 0
    }
    $supPid = [int]$info['supervisor']
    $childPid = 0
    if ($info['child']) { $childPid = [int]$info['child'] }
    if (-not (Test-PidAlive $supPid)) {
        if (Test-PidAlive $childPid) {
            Stop-Process -Id $childPid -Force
            Write-Host "be-supervised: supervisor dead, orphaned child $childPid killed."
        } else {
            Write-Host "be-supervised: stale pidfile, nothing running."
        }
        Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $StopFlag -Force -ErrorAction SilentlyContinue
        exit 0
    }
    Set-Content -LiteralPath $StopFlag -Value ("stop requested {0:yyyy-MM-dd HH:mm:ss}" -f (Get-Date)) -NoNewline
    if (Test-PidAlive $childPid) {
        Stop-Process -Id $childPid -Force
        Write-Host "be-supervised: stop requested (flag set, child $childPid killed)."
    } else {
        Write-Host "be-supervised: stop requested (flag set, child already gone)."
    }
    $deadline = (Get-Date).AddSeconds(25)
    while ((Test-Path -LiteralPath $PidFile) -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 500
    }
    if (Test-Path -LiteralPath $PidFile) {
        Write-Host "be-supervised: supervisor $supPid did not exit in 25s; kill it manually if needed."
        exit 1
    }
    Write-Host "be-supervised: stopped."
    exit 0
}

# --- Start mode ---
$existing = Read-PidFile
if ($existing -and $existing['supervisor'] -and (Test-PidAlive ([int]$existing['supervisor']))) {
    Write-Host ("be-supervised: already supervised (supervisor PID {0}, port {1}). Use -Stop first." -f $existing['supervisor'], $Port)
    exit 2
}
if ($existing) {
    # Stale pidfile from a dead supervisor; drop it (port check below still guards).
    Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $StopFlag -Force -ErrorAction SilentlyContinue
}

$owner = Get-PortOwner $Port
if ($owner) {
    Write-Host ("be-supervised: REFUSING to start, port {0} already held by PID {1}." -f $Port, $owner)
    Write-Host "If that process is stale, stop it first, then retry. No loop started."
    exit 2
}

if (-not (Test-Path -LiteralPath $LogsDir)) {
    New-Item -ItemType Directory -Path $LogsDir | Out-Null
}

if (-not $SkipBuild) {
    Write-Host "be-supervised: building bin/arbitrage-be.exe ..."
    & go build -o $Binary ./cmd/server
    if ($LASTEXITCODE -ne 0) {
        Write-Host "be-supervised: build failed, not starting."
        exit 3
    }
} elseif (-not (Test-Path -LiteralPath $Binary)) {
    Write-Host ("be-supervised: -SkipBuild but no binary at {0}. Build first." -f $Binary)
    exit 3
}

Set-Content -LiteralPath $PidFile -Value ("supervisor={0}" -f $PID) -NoNewline
Write-Log ("supervising port {0}, binary {1}" -f $Port, $Binary)

function Rotate-Logs {
    # Start-Process redirection truncates, so rotate one generation first:
    # current run always lands in a fresh file, previous run stays in .prev.
    foreach ($f in @($LogFile, $ErrFile)) {
        if (Test-Path -LiteralPath $f) {
            Move-Item -LiteralPath $f -Destination ($f + ".prev") -Force
        }
    }
}

function Start-Child([string]$Exe, [string]$WorkDir, [string]$OutLog, [string]$ErrLog) {
    # NOTE: Start-Process has no -Environment on PS 5.1; ARBITRAGE_PORT is set
    # process-wide before the loop and inherited by the child.
    Rotate-Logs
    $p = Start-Process -FilePath $Exe -WorkingDirectory $WorkDir -PassThru `
        -WindowStyle Hidden `
        -RedirectStandardOutput $OutLog -RedirectStandardError $ErrLog
    return $p
}

$env:ARBITRAGE_PORT = "$Port"
$backoff = 1
$fastFails = 0
try {
    while ($true) {
        $started = Get-Date
        $proc = Start-Child $Binary $RepoRoot $LogFile $ErrFile
        Set-Content -LiteralPath $PidFile -Value ("supervisor={0}`nchild={1}" -f $PID, $proc.Id) -NoNewline
        Write-Log ("child started PID {0}" -f $proc.Id)
        $proc.WaitForExit()
        $code = $proc.ExitCode
        $uptime = ((Get-Date) - $started).TotalSeconds
        Write-Log ("child PID exited code={0} after {1:N0}s" -f $code, $uptime)

        if (Test-Path -LiteralPath $StopFlag) {
            Remove-Item -LiteralPath $StopFlag -Force -ErrorAction SilentlyContinue
            Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
            Write-Log "stop requested, supervisor exiting."
            exit 0
        }
        if ($code -eq 0) {
            Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
            Write-Log "clean exit, not restarting."
            exit 0
        }
        if ($uptime -lt 8) { $fastFails++ } else { $fastFails = 0 }
        if ($fastFails -ge 3) {
            Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
            Write-Log "GIVING UP: 3 consecutive fast failures (<8s). Fix port/build (see log), then restart. Not looping."
            exit 4
        }
        Write-Log ("restarting in {0}s (backoff, fastFails={1})" -f $backoff, $fastFails)
        Start-Sleep -Seconds $backoff
        if ($backoff -lt 30) { $backoff = $backoff * 2 }
    }
} finally {
    # Best effort: don't leave an orphaned child behind.
    $info = Read-PidFile
    if ($info -and $info['child'] -and (Test-PidAlive ([int]$info['child']))) {
        Stop-Process -Id ([int]$info['child']) -Force -ErrorAction SilentlyContinue
    }
}
