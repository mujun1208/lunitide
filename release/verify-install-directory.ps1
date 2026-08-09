param(
  [Parameter(Mandatory=$true)][string]$Path,
  [switch]$MustExist
)
$ErrorActionPreference = 'Stop'
$parent = Split-Path -Parent $Path
if (-not (Test-Path -LiteralPath $parent -PathType Container)) { exit 30 }
$parentItem = Get-Item -LiteralPath $parent -Force
if ($parentItem.Attributes -band [IO.FileAttributes]::ReparsePoint) { exit 31 }
$exists = Test-Path -LiteralPath $Path
if (-not $exists) {
  if ($MustExist) { exit 32 }
  exit 0
}
if (-not $MustExist) { exit 33 }
$item = Get-Item -LiteralPath $Path -Force
if (-not $item.PSIsContainer) { exit 34 }
if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { exit 35 }
exit 0
