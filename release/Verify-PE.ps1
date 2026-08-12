param(
  [Parameter(Mandatory)][string]$Path,
  [string[]]$RequiredExports = @(),
  [switch]$RequireWindowsGUI
)
$ErrorActionPreference = 'Stop'
$bytes = [IO.File]::ReadAllBytes((Resolve-Path $Path))
function U16([int]$o) { [BitConverter]::ToUInt16($bytes,$o) }
function U32([int]$o) { [BitConverter]::ToUInt32($bytes,$o) }
if ($bytes.Length -lt 512 -or (U16 0) -ne 0x5A4D) { throw "$Path is not a PE file" }
$pe = U32 0x3c
if ($pe -gt ($bytes.Length-256) -or (U32 $pe) -ne 0x4550) { throw "$Path has an invalid PE header" }
if ((U16 ($pe+4)) -ne 0x8664) { throw "$Path is not AMD64 (PE machine 0x8664)" }
$sections = U16 ($pe+6); $optionalSize = U16 ($pe+20); $optional = $pe+24
if ((U16 $optional) -ne 0x20b) { throw "$Path is not PE32+" }
if ($RequireWindowsGUI -and (U16 ($optional+68)) -ne 2) { throw "$Path is not a Windows GUI subsystem executable" }
$exportRva = U32 ($optional+112)
$sectionTable = $optional+$optionalSize
function RvaToOffset([uint32]$rva) {
  for ($i=0; $i -lt $sections; $i++) {
    $s=$sectionTable+40*$i; $va=U32 ($s+12); $size=[Math]::Max((U32 ($s+8)),(U32 ($s+16)))
    if ($rva -ge $va -and $rva -lt ($va+$size)) { return [int]((U32 ($s+20))+($rva-$va)) }
  }
  throw ('RVA 0x{0:x} is outside PE sections' -f $rva)
}
if ($exportRva -eq 0 -and $RequiredExports.Count -eq 0) { Write-Host "Verified AMD64 PE: $Path"; exit 0 }
if ($exportRva -eq 0) { throw "$Path has no export directory" }
$ed=RvaToOffset $exportRva; $count=U32 ($ed+24); $names=RvaToOffset (U32 ($ed+32)); $found=@{}
for ($i=0; $i -lt $count; $i++) {
  $no=RvaToOffset (U32 ($names+4*$i)); $end=$no
  while ($end -lt $bytes.Length -and $bytes[$end] -ne 0) { $end++ }
  if ($end -eq $bytes.Length) { throw 'Unterminated export name' }
  $found[[Text.Encoding]::ASCII.GetString($bytes,$no,$end-$no)]=$true
}
foreach ($name in $RequiredExports) { if (-not $found.ContainsKey($name)) { throw "$Path lacks required export $name" } }
Write-Host "Verified AMD64 PE and $($RequiredExports.Count) required export(s): $Path"
