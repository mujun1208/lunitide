$ErrorActionPreference = 'Stop'
$localAppData = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
if (-not $localAppData) { throw 'Windows LocalApplicationData Known Folder is unavailable' }
$root = [IO.Path]::GetFullPath((Join-Path $localAppData 'Programs\Lunitide')).TrimEnd('\') + '\'
$names = @('Lunitide.exe', 'lunitide-engine.exe')

for($attempt=0; $attempt -lt 20; $attempt++) {
  $matching = @(Get-CimInstance Win32_Process | Where-Object {
    $_.Name -in $names -and
    $_.ExecutablePath -and
    [IO.Path]::GetFullPath($_.ExecutablePath).StartsWith($root, [StringComparison]::OrdinalIgnoreCase)
  })
  if($matching.Count -eq 0){exit 0}
  $matching | ForEach-Object {Stop-Process -Id $_.ProcessId -Force -ErrorAction Stop}
  Start-Sleep -Milliseconds 250
}
throw 'Lunitide processes are still running after the stop timeout'
