param(
  [Parameter(Mandatory)][string]$Executable,
  [int]$StartupTimeoutSeconds=30,
  [int]$ShutdownTimeoutSeconds=15
)
$ErrorActionPreference='Stop'
$Executable=(Resolve-Path $Executable).Path
$runRoot=Join-Path ([IO.Path]::GetTempPath()) ('lunitide-runtime-'+[guid]::NewGuid().ToString('N'))
$stdout=Join-Path $runRoot 'stdout.log'; $stderr=Join-Path $runRoot 'stderr.log'
New-Item $runRoot -ItemType Directory -Force|Out-Null
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class LunitideProcessStatus {
  [DllImport("kernel32.dll", SetLastError=true)]
  public static extern bool GetExitCodeProcess(IntPtr process, out uint exitCode);
}
'@
$desktop=$null
$tracked=@{}
function Update-TrackedDescendants([int]$ParentId) {
  foreach($child in @(Get-CimInstance Win32_Process -Filter "ParentProcessId=$ParentId" -ErrorAction SilentlyContinue)) {
    $key="$($child.ProcessId)|$($child.CreationDate)"
    $tracked[$key]=[pscustomobject]@{Id=[int]$child.ProcessId;CreationDate=$child.CreationDate;ExecutablePath=$child.ExecutablePath;Name=$child.Name}
    Update-TrackedDescendants -ParentId $child.ProcessId
  }
}
function Get-TrackedSurvivors {
  $result=@()
  foreach($expected in $tracked.Values) {
    $actual=Get-CimInstance Win32_Process -Filter "ProcessId=$($expected.Id)" -ErrorAction SilentlyContinue
    if($actual -and $actual.CreationDate -eq $expected.CreationDate -and (!$expected.ExecutablePath -or $actual.ExecutablePath -eq $expected.ExecutablePath)){$result+=$expected}
  }
  return @($result)
}
try {
  $desktop=Start-Process $Executable -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
  $processHandle=$desktop.Handle
  $deadline=(Get-Date).AddSeconds($StartupTimeoutSeconds)
  $loaded=$false
  while((Get-Date) -lt $deadline) {
    Update-TrackedDescendants -ParentId $desktop.Id
    if($desktop.HasExited){throw "Lunitide exited during startup: $(Get-Content $stderr -Raw -ErrorAction SilentlyContinue)"}
    $logs=(Get-Content $stdout -Raw -ErrorAction SilentlyContinue)+(Get-Content $stderr -Raw -ErrorAction SilentlyContinue)
    if($logs -match 'Lunitide WebView2 initial document loaded'){$loaded=$true;break}
    Start-Sleep -Milliseconds 200
  }
  if(-not $loaded){throw "WebView2 initial document did not load within $StartupTimeoutSeconds seconds. stdout=$(Get-Content $stdout -Raw -ErrorAction SilentlyContinue) stderr=$(Get-Content $stderr -Raw -ErrorAction SilentlyContinue)"}
  Update-TrackedDescendants -ParentId $desktop.Id
  $engines=@($tracked.Values|Where-Object{$_.Name -eq 'lunitide-engine.exe'})
  if($engines.Count -ne 1){throw "expected exactly one Engine child, found $($engines.Count)"}
  $desktop.Refresh()
  if($desktop.MainWindowHandle -eq 0){throw 'WebView2 desktop window handle is unavailable after initial navigation'}
  if(-not $desktop.CloseMainWindow()){throw 'failed to send WM_CLOSE to the WebView2 desktop window'}
  $shutdownDeadline=(Get-Date).AddSeconds($ShutdownTimeoutSeconds)
  while(-not $desktop.HasExited -and (Get-Date) -lt $shutdownDeadline) {
    Update-TrackedDescendants -ParentId $desktop.Id
    Start-Sleep -Milliseconds 200
  }
  if(-not $desktop.HasExited){throw "Lunitide did not exit within $ShutdownTimeoutSeconds seconds after WM_CLOSE"}
  $desktop.WaitForExit()
  [uint32]$exitCode=0
  if(-not [LunitideProcessStatus]::GetExitCodeProcess($processHandle,[ref]$exitCode)){throw "GetExitCodeProcess failed: $([Runtime.InteropServices.Marshal]::GetLastWin32Error())"}
  if($exitCode -ne 0){throw "Lunitide exited with code $exitCode after WM_CLOSE"}
  do {
    $survivors=@(Get-TrackedSurvivors)
    if($survivors.Count -eq 0){break}
    Start-Sleep -Milliseconds 200
  } while((Get-Date) -lt $shutdownDeadline)
  if($survivors.Count){throw "child processes survived Desktop shutdown: $(($survivors.Id|Sort-Object)-join ', ')"}
  Write-Host 'Installed WebView2 runtime startup and clean shutdown acceptance passed'
} finally {
  if($desktop -and -not $desktop.HasExited){Update-TrackedDescendants -ParentId $desktop.Id; Stop-Process -Id $desktop.Id -Force -ErrorAction SilentlyContinue}
  foreach($survivor in @(Get-TrackedSurvivors)){Stop-Process -Id $survivor.Id -Force -ErrorAction SilentlyContinue}
  Remove-Item $runRoot -Recurse -Force -ErrorAction SilentlyContinue
}
