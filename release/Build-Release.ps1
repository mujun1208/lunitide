[CmdletBinding()]
param(
  [string]$OutputRoot = 'release/out',
  [switch]$SkipInstaller,
  [switch]$RequireSignature,
  [switch]$AllowUnsignedDevelopment,
  [string]$SignCommand = $env:LUNITIDE_SIGN_COMMAND,
  [string]$ExpectedSignerThumbprint = $env:LUNITIDE_SIGNER_THUMBPRINT
)
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
Add-Type -AssemblyName System.IO.Compression.FileSystem
$root=(Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$version=(Get-Content (Join-Path $root 'VERSION') -Raw).Trim()
if ($version -notmatch '^\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$') { throw 'VERSION is invalid' }
if (-not $SkipInstaller -and -not $AllowUnsignedDevelopment) { $RequireSignature=$true }
$out=Join-Path $root $OutputRoot; $stage=Join-Path $out "Lunitide-$version-x64"; $cache=Join-Path $root '.release-cache'
function Invoke-ArtifactSigning([string]$Artifact) {
  if ($SignCommand) { & powershell -NoProfile -Command ($SignCommand.Replace('{artifact}',$Artifact)); if ($LASTEXITCODE) { throw "signature command failed for $Artifact" } }
  elseif ($RequireSignature) { throw 'Production signing is required; set LUNITIDE_SIGN_COMMAND with an {artifact} token, or use -AllowUnsignedDevelopment only for a non-publishable test candidate' }
}
function Assert-PublisherSignature([string]$Artifact) {
  if (-not $RequireSignature) { return }
  if (-not $ExpectedSignerThumbprint) { throw 'Production signing requires LUNITIDE_SIGNER_THUMBPRINT' }
  $expected=$ExpectedSignerThumbprint.Trim()
  if ($expected -notmatch '\A[0-9A-Fa-f]{40}\z') { throw 'LUNITIDE_SIGNER_THUMBPRINT must be exactly 40 hexadecimal characters' }
  $sig=Get-AuthenticodeSignature $Artifact
  if ($sig.Status -ne 'Valid') { throw "Invalid signature for $Artifact`: $($sig.Status)" }
  if (-not $sig.SignerCertificate -or $sig.SignerCertificate.Thumbprint.ToUpperInvariant() -cne $expected.ToUpperInvariant()) { throw "Signer does not match the pinned publisher certificate: $Artifact" }
  if (-not $sig.TimeStamperCertificate) { throw "Signature is missing a trusted timestamp: $Artifact" }
  $signtool=Get-Command signtool.exe -ErrorAction SilentlyContinue
  if (-not $signtool) { throw 'Production verification requires Windows SDK signtool.exe' }
  & $signtool.Source verify /pa /all /v $Artifact
  if ($LASTEXITCODE) { throw "Windows Authenticode policy rejected the signature or timestamp chain: $Artifact" }
}
Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
New-Item $stage,$cache,(Join-Path $stage 'web\dist'),(Join-Path $stage 'licenses') -ItemType Directory -Force | Out-Null
$oldCgo=$env:CGO_ENABLED; $oldOs=$env:GOOS; $oldArch=$env:GOARCH
Push-Location $root
try {
  $env:CGO_ENABLED='0'; $env:GOOS='windows'; $env:GOARCH='amd64'
  $ld="-s -w -X github.com/lunitide/lunitide/internal/buildinfo.Version=$version"
  & go build -trimpath -buildvcs=false -ldflags $ld -o (Join-Path $stage 'Lunitide.exe') ./cmd/desktop
  if ($LASTEXITCODE) { throw 'desktop build failed' }
  & go build -trimpath -buildvcs=false -ldflags $ld -o (Join-Path $stage 'lunitide-engine.exe') ./cmd/engine
  if ($LASTEXITCODE) { throw 'engine build failed' }
  & go build -trimpath -buildvcs=false -ldflags '-s -w' -o (Join-Path $stage 'purge-user-data.exe') ./cmd/purge-user-data
  if ($LASTEXITCODE) { throw 'purge helper build failed' }
} finally { $env:CGO_ENABLED=$oldCgo; $env:GOOS=$oldOs; $env:GOARCH=$oldArch; Pop-Location }
& npm --prefix web run build; if ($LASTEXITCODE) { throw 'renderer build failed' }
Copy-Item (Join-Path $root 'web\dist\*') (Join-Path $stage 'web\dist') -Recurse -Force
Copy-Item (Join-Path $root 'resources\lunitide-icon.ico') $stage -Force

$wvVersion='1.0.3537.50'; $wvHash='5ea526bbd728adda0da4d31219267e96460494a427e4894c4e09d9f320f4b9aa'
$wvLoaderX64Hash='2f965e10aed3b356a408978a0e6d74eb86e3e722dd008fa9ad39f68884479e85'
$wv=Join-Path $cache "Microsoft.Web.WebView2.$wvVersion.nupkg"
if (-not (Test-Path $wv)) { Invoke-WebRequest -UseBasicParsing "https://www.nuget.org/api/v2/package/Microsoft.Web.WebView2/$wvVersion" -OutFile $wv }
if ((Get-FileHash $wv -Algorithm SHA256).Hash.ToLowerInvariant() -ne $wvHash) { Remove-Item $wv -Force; throw 'WebView2 NuGet SHA-256 mismatch' }
$signatureVerified=$false
$nuget=Get-Command nuget.exe -ErrorAction SilentlyContinue
$dotnet=Get-Command dotnet.exe -ErrorAction SilentlyContinue
if ($nuget) {
  & $nuget.Source verify -Signatures $wv; if ($LASTEXITCODE) { throw 'WebView2 NuGet signature verification failed' }; $signatureVerified=$true
} elseif ($dotnet) {
  $oldPreference=$ErrorActionPreference
  try {
    $ErrorActionPreference='Continue'
    $help=(& $dotnet.Source nuget verify --help 2>&1 | Out-String)
    $verifySupported=($LASTEXITCODE -eq 0 -and $help -notmatch 'could not be loaded|Unrecognized command')
  } finally {
    $ErrorActionPreference=$oldPreference
  }
  if ($verifySupported) { & $dotnet.Source nuget verify $wv --all; if ($LASTEXITCODE) { throw 'WebView2 NuGet signature verification failed' }; $signatureVerified=$true }
}
if (-not $signatureVerified) {
  if ($RequireSignature) { throw 'A trustworthy nuget/dotnet package signature verifier is required for -RequireSignature builds' }
  Write-Warning 'No NuGet signature verifier is available; continuing only because this is a non-RequireSignature build. Pinned package and loader hashes remain enforced.'
}
$wvExtract=Join-Path $cache "webview2-$wvVersion"; Remove-Item $wvExtract -Recurse -Force -ErrorAction SilentlyContinue
[IO.Compression.ZipFile]::ExtractToDirectory($wv,$wvExtract)
$loader=Join-Path $wvExtract 'build\native\x64\WebView2Loader.dll'
if ((Get-FileHash $loader -Algorithm SHA256).Hash.ToLowerInvariant() -ne $wvLoaderX64Hash) { throw 'Pinned x64 WebView2Loader.dll SHA-256 mismatch' }
Copy-Item $loader $stage
Copy-Item (Join-Path $wvExtract 'LICENSE.txt') (Join-Path $stage 'licenses\Microsoft.Web.WebView2-LICENSE.txt')
Copy-Item (Join-Path $wvExtract 'NOTICE.txt') (Join-Path $stage 'licenses\Microsoft.Web.WebView2-NOTICE.txt')
Copy-Item (Join-Path $PSScriptRoot 'stop-install-processes.ps1') $stage
Copy-Item (Join-Path $PSScriptRoot 'verify-install-directory.ps1') $stage
& (Join-Path $PSScriptRoot 'Verify-PE.ps1') (Join-Path $stage 'WebView2Loader.dll') -RequiredExports @('CreateCoreWebView2EnvironmentWithOptions','GetAvailableCoreWebView2BrowserVersionString','CompareBrowserVersions')
foreach($binary in @('Lunitide.exe','lunitide-engine.exe','purge-user-data.exe')){
  $artifact=Join-Path $stage $binary
  Invoke-ArtifactSigning $artifact
  Assert-PublisherSignature $artifact
}
& (Join-Path $PSScriptRoot 'Verify-Layout.ps1') -Stage $stage -Version $version

$manifest=Join-Path $stage 'SHA256SUMS.txt'
Get-ChildItem $stage -File -Recurse | Where-Object FullName -ne $manifest | ForEach-Object {
  $rel=$_.FullName.Substring($stage.Length+1).Replace('\','/'); "{0}  {1}" -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(),$rel
} | Sort-Object | Set-Content $manifest -Encoding ascii
& (Join-Path $PSScriptRoot 'Verify-Layout.ps1') -Stage $stage -Version $version -VerifyManifest

if (-not $SkipInstaller) {
  $nsisVersion='3.11'; $nsisHash='c7d27f780ddb6cffb4730138cd1591e841f4b7edb155856901cdf5f214394fa1'
  $nsisZip=Join-Path $cache "nsis-$nsisVersion.zip"
  if (-not (Test-Path $nsisZip)) {
    & curl.exe --fail --location --silent --show-error --output $nsisZip "https://downloads.sourceforge.net/project/nsis/NSIS%203/$nsisVersion/nsis-$nsisVersion.zip"
    if ($LASTEXITCODE) { throw 'NSIS archive download failed' }
  }
  if ((Get-FileHash $nsisZip -Algorithm SHA256).Hash.ToLowerInvariant() -ne $nsisHash) { Remove-Item $nsisZip -Force; throw 'NSIS archive SHA-256 mismatch' }
  $pathComponent=Get-Item $cache -Force
  while ($pathComponent) {
    if ($pathComponent.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "NSIS extraction path contains a reparse point: $($pathComponent.FullName)" }
    $pathComponent=$pathComponent.Parent
  }
  $nsis=Join-Path $cache ("nsis-build-"+[guid]::NewGuid().ToString('N'))
  try {
    Remove-Item $nsis -Recurse -Force -ErrorAction SilentlyContinue
    New-Item $nsis -ItemType Directory | Out-Null
    if ((Get-Item $nsis -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "NSIS extraction root is a reparse point: $nsis" }
    [IO.Compression.ZipFile]::ExtractToDirectory($nsisZip,$nsis)
    $reparse=Get-ChildItem $nsis -Force -Recurse | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint } | Select-Object -First 1
    if ($reparse) { throw "NSIS archive extracted a reparse point: $($reparse.FullName)" }
    $makeNsis=Join-Path $nsis "nsis-$nsisVersion\makensis.exe"
    if (-not (Test-Path $makeNsis -PathType Leaf)) { throw 'Fresh NSIS extraction is missing makensis.exe' }
    $installer=Join-Path $out "Lunitide-Setup-$version-x64.exe"
    & $makeNsis "/DVERSION=$version" "/DSTAGE=$stage" "/DOUTFILE=$installer" (Join-Path $PSScriptRoot 'installer.nsi')
    if ($LASTEXITCODE) { throw 'NSIS compilation failed' }
  } finally {
    Remove-Item $nsis -Recurse -Force -ErrorAction SilentlyContinue
  }
  Invoke-ArtifactSigning $installer
  Assert-PublisherSignature $installer
  $releaseManifest=Join-Path $out 'SHA256SUMS.txt'
  $installerName=Split-Path $installer -Leaf
  $stageManifestName=(Split-Path $stage -Leaf)+'/SHA256SUMS.txt'
  @(
    "{0}  {1}" -f (Get-FileHash $installer -Algorithm SHA256).Hash.ToLowerInvariant(),$installerName
    "{0}  {1}" -f (Get-FileHash $manifest -Algorithm SHA256).Hash.ToLowerInvariant(),$stageManifestName
  ) | Set-Content $releaseManifest -Encoding ascii
}
Write-Host "Release stage: $stage"; if (-not $SkipInstaller) { Write-Host "Installer: $installer" }
