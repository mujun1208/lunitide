param(
  [switch]$WhatIf
)
$ErrorActionPreference = 'Stop'

$webRoot = Split-Path $PSScriptRoot -Parent
$webRootNorm = [IO.Path]::GetFullPath($webRoot).TrimEnd('\') + '\'

$patterns = @('vitest', 'vite-node', 'npm test', 'npm.cmd test', 'pnpm test', 'yarn test')

$targets = @(Get-CimInstance Win32_Process -Filter "name='node.exe'" -ErrorAction SilentlyContinue | Where-Object {
  $cmd = $_.CommandLine
  if (-not $cmd) { return $false }
  if ($cmd -notmatch [regex]::Escape($webRootNorm) -and $cmd -notmatch '\\lunitide\\web\\') { return $false }
  foreach ($needle in $patterns) {
    if ($cmd -like "*$needle*") { return $true }
  }
  $false
})

if ($targets.Count -eq 0) {
  Write-Host "No lunitide/web test zombies found."
  exit 0
}

foreach ($proc in $targets) {
  $line = "pid=$($proc.ProcessId)"
  if ($WhatIf) {
    Write-Host "Would stop $line"
  } else {
    Write-Host "Stopping $line"
    Stop-Process -Id $proc.ProcessId -Force -ErrorAction Stop
  }
}

if (-not $WhatIf) {
  Write-Host "Stopped $($targets.Count) vitest/npm test process(es)."
}
