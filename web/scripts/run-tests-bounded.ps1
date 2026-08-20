param(
  [int]$OuterTimeoutMinutes = 20,
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$VitestArgs
)
$ErrorActionPreference = 'Stop'

$webRoot = Split-Path $PSScriptRoot -Parent
Push-Location $webRoot
try {
  $npmCmd = Get-Command npm.cmd -ErrorAction SilentlyContinue
  if ($npmCmd) { $npm = $npmCmd.Source } else { $npm = (Get-Command npm -ErrorAction Stop).Source }

  $args = @('test', '--')
  if ($VitestArgs.Count -gt 0) { $args += $VitestArgs } else { $args += 'run' }

  $proc = Start-Process -FilePath $npm -ArgumentList $args -PassThru -NoNewWindow -Wait:$false
  $timeoutMs = [Math]::Max(1, $OuterTimeoutMinutes) * 60 * 1000
  if (-not $proc.WaitForExit($timeoutMs)) {
    Write-Error "Vitest exceeded ${OuterTimeoutMinutes}m; killing orphaned test workers."
    try { $proc.Kill($true) } catch {}
    & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'stop-test-zombies.ps1')
    exit 124
  }
  exit $proc.ExitCode
} finally {
  Pop-Location
}
