param([Parameter(Mandatory)][string]$OutputRoot,[Parameter(Mandatory)][string]$Version,[Parameter(Mandatory)][string]$ExpectedInstallerHash,[Parameter(Mandatory)][string]$ExpectedSourceTreeSha256,[string]$ExpectedSignerThumbprint,[switch]$AllowUnsignedDevelopment)
$ErrorActionPreference='Stop'; . (Join-Path $PSScriptRoot 'Resolve-SignTool.ps1'); . (Join-Path $PSScriptRoot 'Release-Safety.ps1'); $OutputRoot=(Resolve-Path $OutputRoot).Path
if($Version -notmatch '^\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$'){throw 'Invalid release version'}
$stage=Join-Path $OutputRoot "Lunitide-$Version-x64"; $installer=Join-Path $OutputRoot "Lunitide-Setup-$Version-x64.exe"; $manifest=Join-Path $OutputRoot 'SHA256SUMS.txt'
foreach($path in @($stage,$installer,$manifest)){if(-not(Test-Path $path)){throw "Missing release artifact: $path"}}
if($ExpectedInstallerHash -notmatch '^[a-fA-F0-9]{64}$' -or (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash -ine $ExpectedInstallerHash){throw 'Installer differs from the independently recorded candidate SHA-256'}
$source=Read-ReleaseJson (Join-Path $stage 'SOURCE-CANDIDATE.json')
if($ExpectedSourceTreeSha256 -notmatch '^[a-fA-F0-9]{64}$' -or $source.treeSha256 -ine $ExpectedSourceTreeSha256){throw 'Source inputs differ from the independently recorded candidate SHA-256'}
$installerName=Split-Path $installer -Leaf; $stageManifestName=(Split-Path $stage -Leaf)+'/SHA256SUMS.txt'; $expected=@($installerName,$stageManifestName); $entries=@{}
foreach($line in Get-Content $manifest){if($line -notmatch '^([0-9a-f]{64})  ([^\\].*)$'){throw "Malformed release manifest line: $line"}; if($entries.ContainsKey($Matches[2])){throw "Duplicate release manifest path: $($Matches[2])"}; $entries[$Matches[2]]=$Matches[1]}
if($entries.Count -ne 2 -or @($expected|Where-Object{-not $entries.ContainsKey($_)}).Count){throw 'Release manifest must contain exactly the installer and staged manifest'}
foreach($entry in @(@($installerName,$installer),@($stageManifestName,(Join-Path $stage 'SHA256SUMS.txt')))){if((Get-FileHash $entry[1] -Algorithm SHA256).Hash.ToLowerInvariant() -cne $entries[$entry[0]]){throw "Release manifest hash mismatch: $($entry[0])"}}
if($AllowUnsignedDevelopment){$ExpectedSignerThumbprint=''}
& (Join-Path $PSScriptRoot 'Verify-Layout.ps1') -Stage $stage -Version $Version -VerifyManifest -ExpectedSignerThumbprint $ExpectedSignerThumbprint
if(-not $AllowUnsignedDevelopment){
  if($ExpectedSignerThumbprint -notmatch '\A[0-9A-Fa-f]{40}\z'){throw 'Signed release verification requires an exact publisher thumbprint'}
  $sig=Get-AuthenticodeSignature $installer; if($sig.Status -ne 'Valid' -or -not $sig.SignerCertificate -or $sig.SignerCertificate.Thumbprint -cne $ExpectedSignerThumbprint.ToUpperInvariant() -or -not $sig.TimeStamperCertificate){throw 'Installer publisher signature or timestamp is invalid'}
  & (Resolve-SignTool) verify /pa /all /v $installer; if($LASTEXITCODE){throw 'Windows policy rejected the installer signature or timestamp chain'}
}
if($AllowUnsignedDevelopment){Write-Host "Development artifact checks passed (publisher and machine acceptance remain pending): $OutputRoot"}
else{Write-Host "Signed release artifact checks passed (machine acceptance is separate): $OutputRoot"}
