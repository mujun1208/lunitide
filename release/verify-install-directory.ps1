param(
  [Parameter(Mandatory=$true)][string]$Path,
  [switch]$MustExist,
  [string]$LogPath
)
$ErrorActionPreference = 'Stop'
function Write-InstallLog([string]$Message) {
  if (-not $LogPath) { return }
  try {
    $logParent = Split-Path -Parent $LogPath
    if ($logParent) { New-Item -ItemType Directory -Path $logParent -Force | Out-Null }
    Add-Content -LiteralPath $LogPath -Value "$(Get-Date -Format o) verify-path $Message" -Encoding UTF8
  } catch {}
}
try {
  if (-not [IO.Path]::IsPathRooted($Path) -or $Path -notmatch '^[A-Za-z]:\\') { Write-InstallLog 'exit=36 reason=not-absolute-drive-path'; exit 36 }
  $fullPath = [IO.Path]::GetFullPath($Path).TrimEnd('\')
  $root = [IO.Path]::GetPathRoot($fullPath).TrimEnd('\')
  if ($fullPath -ieq $root) { Write-InstallLog 'exit=37 reason=drive-root'; exit 37 }
  $drive = [IO.DriveInfo]::new([IO.Path]::GetPathRoot($fullPath))
  if (-not $drive.IsReady -or $drive.DriveType -ne [IO.DriveType]::Fixed) { Write-InstallLog 'exit=38 reason=drive-not-fixed'; exit 38 }
  $parent = Split-Path -Parent $fullPath
  if (-not (Test-Path -LiteralPath $parent -PathType Container)) { Write-InstallLog 'exit=30 reason=parent-missing'; exit 30 }
  $current = Get-Item -LiteralPath ([IO.Path]::GetPathRoot($fullPath)) -Force
  foreach ($segment in $parent.Substring($current.FullName.TrimEnd('\').Length).Trim('\').Split('\',[StringSplitOptions]::RemoveEmptyEntries)) {
    $current = Get-Item -LiteralPath (Join-Path $current.FullName $segment) -Force
    if (-not $current.PSIsContainer -or ($current.Attributes -band [IO.FileAttributes]::ReparsePoint)) { Write-InstallLog 'exit=31 reason=unsafe-ancestor'; exit 31 }
  }
  $exists = Test-Path -LiteralPath $fullPath
  if (-not $exists) {
    if ($MustExist) { Write-InstallLog 'exit=32 reason=required-path-missing'; exit 32 }
    Write-InstallLog 'exit=0 result=safe-nonexistent'
    exit 0
  }
  if (-not $MustExist) { Write-InstallLog 'exit=33 reason=path-already-exists'; exit 33 }
  $item = Get-Item -LiteralPath $fullPath -Force
  if (-not $item.PSIsContainer) { Write-InstallLog 'exit=34 reason=not-directory'; exit 34 }
  if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { Write-InstallLog 'exit=35 reason=target-reparse-point'; exit 35 }
  Write-InstallLog 'exit=0 result=safe-existing'
  exit 0
} catch {
  Write-InstallLog "exit=39 reason=validation-exception type=$($_.Exception.GetType().Name)"
  exit 39
}
