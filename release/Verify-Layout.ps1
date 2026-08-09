param([Parameter(Mandatory)][string]$Stage,[string]$Version,[switch]$VerifyManifest,[switch]$Installed)
$ErrorActionPreference='Stop'; $Stage=(Resolve-Path $Stage).Path
$required=@('Lunitide.exe','lunitide-engine.exe','purge-user-data.exe','WebView2Loader.dll','web\dist\index.html','licenses\Microsoft.Web.WebView2-LICENSE.txt','licenses\Microsoft.Web.WebView2-NOTICE.txt')
foreach($f in $required){if(-not(Test-Path (Join-Path $Stage $f)-PathType Leaf)){throw "Missing staged file: $f"}}
$allowedRootFiles=@('Lunitide.exe','lunitide-engine.exe','purge-user-data.exe','WebView2Loader.dll','SHA256SUMS.txt')
if($Installed){$allowedRootFiles += @('Uninstall.exe','.lunitide-install-owner','stop-install-processes.ps1')}
$files=Get-ChildItem $Stage -File -Recurse
foreach($item in $files){
  $rel=$item.FullName.Substring($Stage.Length+1).Replace('\','/')
  $allowed=($rel -in $allowedRootFiles) -or $rel.StartsWith('web/dist/',[StringComparison]::Ordinal) -or $rel.StartsWith('licenses/',[StringComparison]::Ordinal)
  if(-not $allowed){throw "Staged file is not on the exact release allowlist: $rel"}
  if(($rel.StartsWith('web/dist/',[StringComparison]::Ordinal) -or $rel.StartsWith('licenses/',[StringComparison]::Ordinal)) -and $rel -match '(?i)\.(exe|dll|ps1|cmd|bat|com|msi)$'){
    throw "Executable or script payload is forbidden below asset directories: $rel"
  }
  if($rel -match '(?i)(^|/)(electron[^/]*|node(\.exe|\.dll)?|python(\d+(\.\d+)?)?(\.exe|\.dll)?|resources\.pak|app\.asar|chrome_[^/]*\.pak|ffmpeg\.dll|locales|_internal|site-packages|node_modules)(/|$)' -or $rel -match '(?i)\.(pyd|pyc)$'){
    throw "Legacy Electron/Python/Node runtime contamination rejected: $rel"
  }
}
& (Join-Path $PSScriptRoot 'Verify-PE.ps1') (Join-Path $Stage 'Lunitide.exe')
& (Join-Path $PSScriptRoot 'Verify-PE.ps1') (Join-Path $Stage 'lunitide-engine.exe')
& (Join-Path $PSScriptRoot 'Verify-PE.ps1') (Join-Path $Stage 'purge-user-data.exe')
if($Version -and $Version -notmatch '^\d+\.\d+\.\d+'){throw 'Invalid acceptance version'}
if($VerifyManifest){
  $manifest=Join-Path $Stage 'SHA256SUMS.txt'; if(-not(Test-Path $manifest -PathType Leaf)){throw 'Missing SHA256SUMS.txt'}
  $entries=@{}; foreach($line in Get-Content $manifest){
    if($line -notmatch '^([0-9a-f]{64})  ([^\\].*)$'){throw "Malformed SHA256SUMS line: $line"}
    $rel=$Matches[2]; if($rel.Contains('..') -or $entries.ContainsKey($rel)){throw "Unsafe or duplicate manifest path: $rel"}; $entries[$rel]=$Matches[1]
  }
  $manifestExclusions=@('SHA256SUMS.txt'); if($Installed){$manifestExclusions += @('Uninstall.exe','.lunitide-install-owner','stop-install-processes.ps1')}
  $actual=@($files | ForEach-Object {$_.FullName.Substring($Stage.Length+1).Replace('\','/')} | Where-Object {$_ -notin $manifestExclusions})
  foreach($rel in $actual){if(-not $entries.ContainsKey($rel)){throw "Manifest missing file: $rel"}; if((Get-FileHash (Join-Path $Stage $rel) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $entries[$rel]){throw "Manifest hash mismatch: $rel"}}
  foreach($rel in $entries.Keys){if($rel -notin $actual){throw "Manifest contains extra file: $rel"}}
}
Write-Host "Layout acceptance passed: $Stage"
