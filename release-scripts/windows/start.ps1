[CmdletBinding()]
param(
  [switch]$Daemon
)

$ErrorActionPreference = 'Stop'
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ReleaseDir = Split-Path -Parent (Split-Path -Parent $ScriptDir)
$RuntimeDir = Join-Path $ReleaseDir '.runtime'
$PidFile = Join-Path $RuntimeDir 'agent-container-hub.pid'
$LogFile = Join-Path $RuntimeDir 'agent-container-hub.log'
$BinaryPath = Join-Path $ReleaseDir 'agent-container-hub.exe'

function Fail([string]$Message) {
  Write-Error "[start] $Message"
  exit 1
}

if (-not (Test-Path -LiteralPath $BinaryPath)) {
  Fail "missing binary: $BinaryPath"
}
if (-not (Test-Path -LiteralPath (Join-Path $ReleaseDir '.env'))) {
  Fail ".env not found; copy from .env.example first"
}

New-Item -ItemType Directory -Force -Path $RuntimeDir, (Join-Path $ReleaseDir 'data/rootfs'), (Join-Path $ReleaseDir 'data/builds') | Out-Null

if (Test-Path -LiteralPath $PidFile) {
  $pidValue = (Get-Content -LiteralPath $PidFile -Raw).Trim()
  if (-not [string]::IsNullOrWhiteSpace($pidValue)) {
    try {
      $proc = Get-Process -Id ([int]$pidValue) -ErrorAction Stop
      Fail "agent-container-hub already running (pid=$($proc.Id))"
    } catch {
      Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
    }
  }
}

$proc = Start-Process -FilePath $BinaryPath -WorkingDirectory $ReleaseDir -RedirectStandardOutput $LogFile -RedirectStandardError $LogFile -PassThru
$proc.Id | Set-Content -LiteralPath $PidFile
Start-Sleep -Seconds 1
if ($proc.HasExited) {
  Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
  Fail "agent-container-hub failed to start; see $LogFile"
}

Write-Host "[start] started agent-container-hub (pid=$($proc.Id))"
Write-Host "[start] log file: $LogFile"
