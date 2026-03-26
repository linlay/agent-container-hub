[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ReleaseDir = Split-Path -Parent (Split-Path -Parent $ScriptDir)
$PidFile = Join-Path (Join-Path $ReleaseDir '.runtime') 'agent-container-hub.pid'

if (-not (Test-Path -LiteralPath $PidFile)) {
  Write-Host '[stop] pid file not found'
  exit 0
}

$pidValue = (Get-Content -LiteralPath $PidFile -Raw).Trim()
if (-not [string]::IsNullOrWhiteSpace($pidValue)) {
  Stop-Process -Id ([int]$pidValue) -Force -ErrorAction SilentlyContinue
}
Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
Write-Host '[stop] agent-container-hub stopped'
