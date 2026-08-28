#requires -Version 5.1
param(
  [Parameter(Mandatory)][string]$Cache,
  [Parameter(Mandatory)][string]$Stage
)
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
# Unpack llama-omni-server.exe + DLLs from the pinned Comni NSIS installer into
# stage/omni/llama-omni-runtime.zip. MiniCPM-o GGUF weights stay out — they
# download through OmniInstallRow. Never run a user-facing Comni GUI install.

$RuntimeRevision='v1.0.22'
$RuntimeSetupFile='Comni-Setup-1.0.22-win64.exe'
$RuntimeSHA256='72cefacba846920c3063479bc4bbfcdc268bb494623ee84f5f1e57464202a514'
$RuntimeBytes=564212994
$ZipName='llama-omni-runtime.zip'

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem

function Test-OmniRuntimeMember([string]$Rel) {
  $base=[IO.Path]::GetFileName($Rel).ToLowerInvariant()
  $slash=$Rel.Replace('\','/').ToLowerInvariant()
  if ($base.EndsWith('.gguf') -or $base.EndsWith('.pdb')) { return $false }
  if ($base -eq 'comni.exe' -or $base -eq 'uninstall.exe') { return $false }
  if ($slash.Contains('node_modules') -or $slash.Contains('/electron') -or $slash.Contains('resources.pak')) { return $false }
  if ($base.EndsWith('.asar') -or $base.EndsWith('.pyc')) { return $false }
  if ($base -eq 'llama-omni-server.exe' -or $base -eq 'llama-omni-server') { return $true }
  if ($base.EndsWith('.dll')) { return $true }
  return $false
}

function Assert-OmniRuntimeZip([string]$ZipPath) {
  if (-not (Test-Path $ZipPath -PathType Leaf)) { throw "Missing omni runtime zip: $ZipPath" }
  $archive=[IO.Compression.ZipFile]::OpenRead($ZipPath)
  try {
    $names=@($archive.Entries | ForEach-Object { $_.FullName.Replace('\','/') })
    if (-not ($names | Where-Object { $_ -match '(^|/)llama-omni-server\.exe$' })) {
      throw 'omni runtime zip is missing llama-omni-server.exe'
    }
    if ($names | Where-Object { $_.ToLowerInvariant().EndsWith('.gguf') }) {
      throw 'omni runtime zip must not contain GGUF weights'
    }
  } finally {
    $archive.Dispose()
  }
}

function Expand-ComniSetup([string]$Setup, [string]$Dest) {
  $seven=@(
    (Get-Command 7z.exe -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -ErrorAction SilentlyContinue),
    (Join-Path $env:ProgramFiles '7-Zip\7z.exe'),
    (Join-Path ${env:ProgramFiles(x86)} '7-Zip\7z.exe')
  ) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
  if ($seven) {
    & $seven x -y "-o$Dest" $Setup
    if ($LASTEXITCODE -ne 0) { throw '7-Zip extraction of Comni-Setup failed' }
    return
  }
  Write-Warning '7-Zip not found; silently unpacking Comni-Setup into the release cache (not Program Files)'
  $p=Start-Process -FilePath $Setup -ArgumentList @('/S', "/D=$Dest") -Wait -PassThru
  if ($p.ExitCode -ne 0) { throw "Comni silent unpack failed: $($p.ExitCode)" }
}

$stageOmni=Join-Path $Stage 'omni'
$stageZip=Join-Path $stageOmni $ZipName
$stageNotice=Join-Path $Stage 'licenses\llama.cpp-omni-NOTICE.txt'
New-Item $stageOmni,(Join-Path $Stage 'licenses') -ItemType Directory -Force | Out-Null
@'
llama-omni-server is built from llama.cpp-omni / Comni (MIT), pinned at v1.0.22.
https://github.com/tc-mb/llama.cpp-omni
月汐 ships only the inference process and its shared libraries. MiniCPM-o GGUF
weights are downloaded on demand into the product data directory.
'@ | Set-Content $stageNotice -Encoding utf8

$override=$env:LUNITIDE_OMNI_RUNTIME_ZIP
if ($override -and (Test-Path $override -PathType Leaf)) {
  try {
    Copy-Item $override $stageZip -Force
    Assert-OmniRuntimeZip $stageZip
    return
  } catch {
    Remove-Item $stageZip -Force -ErrorAction SilentlyContinue
    Write-Warning "LUNITIDE_OMNI_RUNTIME_ZIP was unusable ($($_.Exception.Message)); omitting omni runtime zip rather than fetching Comni."
  }
}

New-Item $Cache -ItemType Directory -Force | Out-Null
$cachedZip=Join-Path $Cache ("llama-omni-runtime-$RuntimeRevision.zip")
if (Test-Path $cachedZip -PathType Leaf) {
  try {
    Copy-Item $cachedZip $stageZip -Force
    Assert-OmniRuntimeZip $stageZip
    return
  } catch {
    Remove-Item $stageZip -Force -ErrorAction SilentlyContinue
    Write-Warning "Cached llama-omni-runtime zip was unusable ($($_.Exception.Message)); will try a prior stage or omit."
  }
}

# Prefer a zip already staged by a prior release (e.g. 0.4.19). Never fetch Comni-Setup.
$outRoot=Split-Path $Stage -Parent
if (Test-Path $outRoot) {
  $prior=@(Get-ChildItem $outRoot -Directory -ErrorAction SilentlyContinue | ForEach-Object { Join-Path $_.FullName "omni\$ZipName" } | Where-Object { Test-Path $_ -PathType Leaf } | Select-Object -First 1)
  if ($prior) {
    try {
      Copy-Item $prior[0] $stageZip -Force
      Assert-OmniRuntimeZip $stageZip
      Copy-Item $stageZip $cachedZip -Force
      return
    } catch {
      Remove-Item $stageZip -Force -ErrorAction SilentlyContinue
      Write-Warning "Prior staged omni zip was unusable: $($_.Exception.Message)"
    }
  }
}

$setup=Join-Path $Cache $RuntimeSetupFile
if (-not (Test-Path $setup -PathType Leaf)) {
  Write-Warning "llama-omni-runtime.zip is not cached and Comni-Setup will not be downloaded. MiniCPM-o runtime is omitted from this Setup; 云端 and 本地 voice paths still work."
  return
}
if ((Get-Item $setup).Length -ne $RuntimeBytes) {
  Write-Warning "Cached Comni-Setup size mismatch; omitting omni runtime zip rather than re-downloading."
  return
}
if ((Get-FileHash $setup -Algorithm SHA256).Hash.ToLowerInvariant() -ne $RuntimeSHA256) {
  Write-Warning "Cached Comni-Setup SHA-256 mismatch; omitting omni runtime zip rather than re-downloading."
  return
}

$extract=Join-Path $Cache "comni-$RuntimeRevision-extract"
$server=Get-ChildItem $extract -Recurse -File -Filter 'llama-omni-server.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $server) {
  Remove-Item $extract -Recurse -Force -ErrorAction SilentlyContinue
  New-Item $extract -ItemType Directory | Out-Null
  try {
    Expand-ComniSetup -Setup $setup -Dest $extract
  } catch {
    Write-Warning "Could not extract cached Comni-Setup ($($_.Exception.Message)); omitting omni runtime zip rather than re-downloading."
    return
  }
  $server=Get-ChildItem $extract -Recurse -File -Filter 'llama-omni-server.exe' | Select-Object -First 1
}
if (-not $server) {
  Write-Warning 'Cached Comni extract is missing llama-omni-server.exe; omitting omni runtime zip.'
  return
}

$binDir=$server.Directory.FullName
$packRoot=$binDir
if ((Split-Path $binDir -Leaf) -eq 'bin') {
  $parent=Split-Path $binDir -Parent
  if (Test-Path (Join-Path $parent 'lib')) { $packRoot=$parent }
}

$zipTmp=Join-Path $Cache ("llama-omni-runtime-$RuntimeRevision.building.zip")
Remove-Item $zipTmp -Force -ErrorAction SilentlyContinue
$zip=[IO.Compression.ZipFile]::Open($zipTmp, [IO.Compression.ZipArchiveMode]::Create)
try {
  foreach ($item in @(Get-ChildItem $packRoot -Recurse -File)) {
    $rel=$item.FullName.Substring($packRoot.Length+1).Replace('\','/')
    if (-not (Test-OmniRuntimeMember $rel)) { continue }
    [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $item.FullName, $rel, [IO.Compression.CompressionLevel]::Optimal)
  }
} finally {
  $zip.Dispose()
}
Assert-OmniRuntimeZip $zipTmp
Copy-Item $zipTmp $cachedZip -Force
Copy-Item $cachedZip $stageZip -Force
