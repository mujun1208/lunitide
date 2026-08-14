# Locates the Windows SDK signtool.exe: PATH first, then the newest versioned
# Windows Kits x64 installation. Used by every release gate script so signed
# builds/verifications work on machines where the SDK bin directory is not on
# PATH (a default SDK install never adds it).
function Resolve-SignTool {
  $signtool=Get-Command signtool.exe -ErrorAction SilentlyContinue
  if($signtool){return $signtool.Source}
  $candidate=Get-ChildItem 'C:\Program Files (x86)\Windows Kits\10\bin' -Directory -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -match '^\d+\.\d+\.\d+\.\d+$' } |
    Sort-Object { [version]$_.Name } -Descending |
    ForEach-Object { Join-Path $_.FullName 'x64\signtool.exe' } |
    Where-Object { Test-Path $_ -PathType Leaf } | Select-Object -First 1
  if(-not $candidate){throw 'signtool.exe not found; install the Windows 10/11 SDK signing tools'}
  return $candidate
}
