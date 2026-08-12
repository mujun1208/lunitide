param([Parameter(Mandatory=$true)][string]$Path,[string]$LogPath)
$ErrorActionPreference = 'Stop'
function Write-InstallLog([string]$Message) {
  if (-not $LogPath) { return }
  try { Add-Content -LiteralPath $LogPath -Value "$(Get-Date -Format o) stop-processes $Message" -Encoding UTF8 } catch {}
}
$root = [IO.Path]::GetFullPath($Path).TrimEnd('\') + '\'
$names = @('Lunitide.exe', 'lunitide-engine.exe')

for($attempt=0; $attempt -lt 20; $attempt++) {
  $matching = @(Get-CimInstance Win32_Process | Where-Object {
    $_.Name -in $names -and
    $_.ExecutablePath -and
    [IO.Path]::GetFullPath($_.ExecutablePath).StartsWith($root, [StringComparison]::OrdinalIgnoreCase)
  })
  if($matching.Count -eq 0){Write-InstallLog 'result=stopped'; exit 0}
  $matching | ForEach-Object {Write-InstallLog "action=stop pid=$($_.ProcessId) name=$($_.Name)"; Stop-Process -Id $_.ProcessId -Force -ErrorAction Stop}
  Start-Sleep -Milliseconds 250
}
Write-InstallLog 'result=timeout'
throw 'Lunitide processes are still running after the stop timeout'
