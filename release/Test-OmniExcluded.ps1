#requires -Version 5.1
# Packaging contract: Setup must not carry MiniCPM-o / Comni / llama-omni-server.
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
$here=$PSScriptRoot
$build=Get-Content (Join-Path $here 'Build-Release.ps1') -Raw
if($build -match 'Publish-OmniRuntime'){ throw 'Build-Release.ps1 must not invoke Publish-OmniRuntime' }
$layout=Get-Content (Join-Path $here 'Verify-Layout.ps1') -Raw
if($layout -match "omni/llama-omni-runtime\.zip'\)"){ throw 'Verify-Layout.ps1 must not allowlist omni/llama-omni-runtime.zip' }
if($layout -notmatch 'Omni/Comni/MiniCPM-o runtime must not ship in Setup'){ throw 'Verify-Layout.ps1 must reject omni payloads' }

$stage=Join-Path ([IO.Path]::GetTempPath()) ('lunitide-omni-exclude-'+[guid]::NewGuid().ToString('N'))
New-Item $stage,(Join-Path $stage 'web\dist'),(Join-Path $stage 'licenses'),(Join-Path $stage 'omni') -ItemType Directory -Force | Out-Null
try {
  foreach($f in @('Lunitide.exe','lunitide-engine.exe','purge-user-data.exe','WebView2Loader.dll','stop-install-processes.ps1','verify-install-directory.ps1','lunitide-icon.ico')){
    Set-Content (Join-Path $stage $f) 'x' -Encoding ascii
  }
  Set-Content (Join-Path $stage 'web\dist\index.html') '<html></html>' -Encoding ascii
  Set-Content (Join-Path $stage 'licenses\Microsoft.Web.WebView2-LICENSE.txt') 'x' -Encoding ascii
  Set-Content (Join-Path $stage 'licenses\Microsoft.Web.WebView2-NOTICE.txt') 'x' -Encoding ascii
  Set-Content (Join-Path $stage 'omni\llama-omni-runtime.zip') 'not-a-zip' -Encoding ascii
  $failed=$false
  try {
    & (Join-Path $here 'Verify-Layout.ps1') -Stage $stage
  } catch {
    $failed=$true
    if($_.Exception.Message -notmatch 'Omni/Comni/MiniCPM-o'){ throw "Verify-Layout rejected omni for the wrong reason: $($_.Exception.Message)" }
  }
  if(-not $failed){ throw 'Verify-Layout.ps1 accepted omni/llama-omni-runtime.zip' }

  $cache=Join-Path $stage 'cache'
  New-Item $cache -ItemType Directory | Out-Null
  Set-Content (Join-Path $cache 'llama-omni-runtime-v1.0.22.zip') 'leftover' -Encoding ascii
  & (Join-Path $here 'Publish-OmniRuntime.ps1') -Cache $cache -Stage $stage
  $after=Join-Path $stage 'omni\from-publish.zip'
  if(Test-Path $after){ throw 'Publish-OmniRuntime.ps1 wrote a new omni payload' }
} finally {
  Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
}
Write-Host 'Omni/Comni exclusion contract passed'
