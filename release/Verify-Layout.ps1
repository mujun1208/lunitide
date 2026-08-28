param([Parameter(Mandatory)][string]$Stage,[string]$Version,[switch]$VerifyManifest,[switch]$Installed,[string]$ExpectedSignerThumbprint)
$ErrorActionPreference='Stop'; $Stage=(Resolve-Path $Stage).Path
$required=@('Lunitide.exe','lunitide-engine.exe','purge-user-data.exe','WebView2Loader.dll','stop-install-processes.ps1','verify-install-directory.ps1','lunitide-icon.ico','web\dist\index.html','licenses\Microsoft.Web.WebView2-LICENSE.txt','licenses\Microsoft.Web.WebView2-NOTICE.txt','licenses\llama.cpp-omni-NOTICE.txt','omni\llama-omni-runtime.zip')
foreach($f in $required){if(-not(Test-Path (Join-Path $Stage $f)-PathType Leaf)){throw "Missing staged file: $f"}}
$allowedRootFiles=@('Lunitide.exe','lunitide-engine.exe','purge-user-data.exe','WebView2Loader.dll','stop-install-processes.ps1','verify-install-directory.ps1','lunitide-icon.ico','SHA256SUMS.txt')
if($Installed){$allowedRootFiles += @('Uninstall.exe','.lunitide-install-owner')}
$files=Get-ChildItem $Stage -File -Recurse
foreach($item in $files){
  $rel=$item.FullName.Substring($Stage.Length+1).Replace('\','/')
  $allowed=($rel -in $allowedRootFiles) -or $rel.StartsWith('web/dist/',[StringComparison]::Ordinal) -or $rel.StartsWith('licenses/',[StringComparison]::Ordinal) -or ($rel -eq 'omni/llama-omni-runtime.zip')
  if(-not $allowed){throw "Staged file is not on the exact release allowlist: $rel"}
	if($rel -eq 'omni/llama-omni-runtime.zip'){
	  Add-Type -AssemblyName System.IO.Compression.FileSystem
	  $archive=[IO.Compression.ZipFile]::OpenRead($item.FullName)
	  try {
	    $names=@($archive.Entries | ForEach-Object { $_.FullName.Replace('\','/') })
	    if(-not ($names | Where-Object { $_ -match '(^|/)llama-omni-server\.exe$' })){throw 'omni/llama-omni-runtime.zip is missing llama-omni-server.exe'}
	    if($names | Where-Object { $_.ToLowerInvariant().EndsWith('.gguf') }){throw 'omni/llama-omni-runtime.zip must not contain GGUF weights'}
	  } finally { $archive.Dispose() }
	}
	if($rel.StartsWith('web/dist/',[StringComparison]::Ordinal) -and $rel -notmatch '(?i)\.(html|js|css|map|json|png|jpe?g|gif|webp|svg|ico|woff2?|ttf)$'){throw "Renderer asset type is not allowed: $rel"}
	if($rel.StartsWith('licenses/',[StringComparison]::Ordinal) -and $rel -notmatch '(?i)\.(txt|md)$'){throw "License asset type is not allowed: $rel"}
	if($rel.StartsWith('web/dist/',[StringComparison]::Ordinal) -or $rel.StartsWith('licenses/',[StringComparison]::Ordinal)){
	  $bytes=[IO.File]::ReadAllBytes($item.FullName)
	  $leadingPatterns=@([byte[]](0x4d,0x5a),[byte[]](0x4d,0x53,0x43,0x46),[byte[]](0x1f,0x8b),[byte[]](0x37,0x7a,0xbc,0xaf,0x27,0x1c),[byte[]](0xfd,0x37,0x7a,0x58,0x5a,0x00),[byte[]](0x42,0x5a,0x68),[byte[]](0x52,0x61,0x72,0x21,0x1a,0x07),[byte[]](0x50,0x4b,0x03,0x04))
	  $forbidden=$false; foreach($pattern in $leadingPatterns){if($bytes.Length -ge $pattern.Length){$match=$true; for($j=0;$j -lt $pattern.Length;$j++){if($bytes[$j] -ne $pattern[$j]){$match=$false;break}}; if($match){$forbidden=$true;break}}}
	  if(-not $forbidden -and $rel -match '(?i)\.(html|js|css|map|json|svg|txt|md)$'){$embeddedPatterns=@([byte[]](0x4d,0x53,0x43,0x46),[byte[]](0x37,0x7a,0xbc,0xaf,0x27,0x1c),[byte[]](0xfd,0x37,0x7a,0x58,0x5a,0x00),[byte[]](0x52,0x61,0x72,0x21,0x1a,0x07),[byte[]](0x50,0x4b,0x03,0x04),[byte[]](0x50,0x4b,0x01,0x02),[byte[]](0x50,0x4b,0x05,0x06),[Text.Encoding]::ASCII.GetBytes('ustar')); foreach($pattern in $embeddedPatterns){for($i=0; -not $forbidden -and $i -le $bytes.Length-$pattern.Length; $i++){$match=$true; for($j=0;$j -lt $pattern.Length;$j++){if($bytes[$i+$j] -ne $pattern[$j]){$match=$false;break}}; if($match){$forbidden=$true}}; if($forbidden){break}}}
	  if($forbidden){throw "Executable or archive content is forbidden below asset directories: $rel"}
	}
  if($rel -match '(?i)(^|/)(electron[^/]*|node(\.exe|\.dll)?|python(\d+(\.\d+)?)?(\.exe|\.dll)?|resources\.pak|app\.asar|chrome_[^/]*\.pak|ffmpeg\.dll|locales|_internal|site-packages|node_modules)(/|$)' -or $rel -match '(?i)\.(pyd|pyc)$'){
    throw "Legacy Electron/Python/Node runtime contamination rejected: $rel"
  }
}
& (Join-Path $PSScriptRoot 'Verify-PE.ps1') (Join-Path $Stage 'Lunitide.exe') -RequireWindowsGUI
& (Join-Path $PSScriptRoot 'Verify-PE.ps1') (Join-Path $Stage 'lunitide-engine.exe')
& (Join-Path $PSScriptRoot 'Verify-PE.ps1') (Join-Path $Stage 'purge-user-data.exe')
if($VerifyManifest){
  $manifest=Join-Path $Stage 'SHA256SUMS.txt'; if(-not(Test-Path $manifest -PathType Leaf)){throw 'Missing SHA256SUMS.txt'}
  $entries=@{}; foreach($line in Get-Content $manifest){
    if($line -notmatch '^([0-9a-f]{64})  ([^\\].*)$'){throw "Malformed SHA256SUMS line: $line"}
    $rel=$Matches[2]; if($rel.Contains('..') -or $entries.ContainsKey($rel)){throw "Unsafe or duplicate manifest path: $rel"}; $entries[$rel]=$Matches[1]
  }
  $manifestExclusions=@('SHA256SUMS.txt'); if($Installed){$manifestExclusions += @('Uninstall.exe','.lunitide-install-owner')}
  $actual=@($files | ForEach-Object {$_.FullName.Substring($Stage.Length+1).Replace('\','/')} | Where-Object {$_ -notin $manifestExclusions})
  foreach($rel in $actual){if(-not $entries.ContainsKey($rel)){throw "Manifest missing file: $rel"}; if((Get-FileHash (Join-Path $Stage $rel) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $entries[$rel]){throw "Manifest hash mismatch: $rel"}}
  foreach($rel in $entries.Keys){if($rel -notin $actual){throw "Manifest contains extra file: $rel"}}
}
if($ExpectedSignerThumbprint){
  if($ExpectedSignerThumbprint -notmatch '\A[0-9A-Fa-f]{40}\z'){throw 'Expected signer thumbprint must be exactly 40 hexadecimal characters'}
  foreach($binary in @('Lunitide.exe','lunitide-engine.exe','purge-user-data.exe')){$sig=Get-AuthenticodeSignature (Join-Path $Stage $binary); if($sig.Status -ne 'Valid' -or -not $sig.SignerCertificate -or $sig.SignerCertificate.Thumbprint -cne $ExpectedSignerThumbprint.ToUpperInvariant() -or -not $sig.TimeStamperCertificate){throw "Installed publisher signature is invalid: $binary"}}
}
if($Version){
  if($Version -notmatch '^\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$'){throw 'Invalid acceptance version'}
  foreach($binary in @('Lunitide.exe','lunitide-engine.exe')){$actual=(& (Join-Path $Stage $binary) --version | Out-String).Trim(); if($LASTEXITCODE -ne 0 -or $actual -cne $Version){throw "$binary version '$actual' does not exactly match VERSION '$Version'"}}
}
Write-Host "Layout acceptance passed: $Stage"
