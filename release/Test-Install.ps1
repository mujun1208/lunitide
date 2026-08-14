param([Parameter(Mandatory)][string]$Installer,[Parameter(Mandatory)][string]$ExpectedVersion,[Parameter(Mandatory)][string]$ExpectedInstallerHash,[string]$TestRoot,[string]$InstallDirectory,[string]$ExpectedSignerThumbprint,[switch]$AllowUnsignedDevelopment)
$ErrorActionPreference='Stop'; . (Join-Path $PSScriptRoot 'Resolve-SignTool.ps1'); $Installer=(Resolve-Path $Installer).Path
if($ExpectedInstallerHash -notmatch '\A[0-9A-Fa-f]{64}\z'){throw 'Expected installer SHA-256 must be exactly 64 hexadecimal characters'}
if(-not $AllowUnsignedDevelopment){
  if($ExpectedSignerThumbprint -notmatch '\A[0-9A-Fa-f]{40}\z'){throw 'Signed acceptance requires an exact publisher thumbprint'}
  $sig=Get-AuthenticodeSignature $Installer
  if($sig.Status -ne 'Valid' -or -not $sig.SignerCertificate -or $sig.SignerCertificate.Thumbprint -cne $ExpectedSignerThumbprint.ToUpperInvariant() -or -not $sig.TimeStamperCertificate){throw 'Installer publisher signature or timestamp is invalid'}
  & (Resolve-SignTool) verify /pa /all /v $Installer; if($LASTEXITCODE){throw 'Windows policy rejected the installer signature or timestamp chain'}
}
function Assert-InstallerHash { if((Get-FileHash $Installer -Algorithm SHA256).Hash -cne $ExpectedInstallerHash.ToUpperInvariant()){throw 'Installer SHA-256 changed before execution'} }
function Wait-Until([scriptblock]$Condition,[string]$Failure,[int]$TimeoutSeconds=20) {
  $deadline=[DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
  do { if(& $Condition){return}; Start-Sleep -Milliseconds 250 } while([DateTime]::UtcNow -lt $deadline)
  throw $Failure
}
if(-not $TestRoot){$TestRoot=Join-Path ([IO.Path]::GetTempPath()) ('lunitide-install-test-'+[guid]::NewGuid().ToString('N'))}
$appid='Lunitide.Desktop.7A565D82-936E-4E06-962D-83B5DD24E53C'
$uninstallKey="HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$appid"
if(Test-Path $uninstallKey){throw 'Refusing to overwrite an existing Lunitide uninstall registration; use a disposable Windows account'}
$profileLocalAppData=[Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
$profileAppData=[Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)
$data=Join-Path $profileLocalAppData 'Lunitide'
if(Test-Path $data){throw 'Refusing to touch existing Lunitide user data; run acceptance under a disposable Windows account/profile'}
$install=if($InstallDirectory){[IO.Path]::GetFullPath($InstallDirectory).TrimEnd('\')}else{Join-Path $profileLocalAppData 'Programs\Lunitide'}
if(-not [IO.Path]::IsPathRooted($install) -or $install -notmatch '^[A-Za-z]:\\' -or (New-Object IO.DriveInfo ([IO.Path]::GetPathRoot($install))).DriveType -ne [IO.DriveType]::Fixed){throw 'InstallDirectory must be an absolute path on a local fixed drive'}
if(Test-Path $install){throw 'Refusing to overwrite an existing Lunitide installation; use a disposable Windows account'}
$ownsData=$false; $ownsInstall=$false
try {
  New-Item $TestRoot -ItemType Directory -Force|Out-Null
	$owner=Join-Path $install '.lunitide-install-owner'; $marker=Join-Path $data 'acceptance-retain.txt'
	New-Item $data -ItemType Directory -Force|Out-Null; $ownsData=$true
	# The interactive directory picker commonly creates the selected folder.
	# An existing empty target must be accepted as a fresh installation.
	New-Item $install -ItemType Directory -Force|Out-Null; $ownsInstall=$true
	Assert-InstallerHash; $p=Start-Process $Installer -ArgumentList @('/S',"/D=$install") -Wait -PassThru; if($p.ExitCode){throw "empty-directory install failed: $($p.ExitCode); see $profileLocalAppData\LunitideInstaller\Logs\install-latest.log"}; $p.Dispose()
	$p=Start-Process (Join-Path $install 'Uninstall.exe') -ArgumentList '/S' -Wait -PassThru; if($p.ExitCode){throw "empty-directory setup cleanup failed: $($p.ExitCode)"}; $p.Dispose()
	Wait-Until { -not(Test-Path $install) } 'empty-directory setup cleanup did not finish within the timeout'
	New-Item $install -ItemType Directory -Force|Out-Null; $ownsInstall=$true; Set-Content $marker 'retain'
  # Seed an owned legacy layout with a stale canary and no modern ownership file.
  Set-Content (Join-Path $install 'stale-electron-canary.dll') 'must disappear'
  New-Item $uninstallKey -Force|Out-Null
  Set-ItemProperty $uninstallKey InstallLocation $install
  Set-ItemProperty $uninstallKey DisplayVersion '0.0.1'
  Assert-InstallerHash; $p=Start-Process $Installer -ArgumentList @('/S',"/D=$install") -Wait -PassThru; if($p.ExitCode){throw "install failed: $($p.ExitCode); see $profileLocalAppData\LunitideInstaller\Logs\install-latest.log"}; $p.Dispose()
  if(Test-Path (Join-Path $install 'stale-electron-canary.dll')){throw 'upgrade retained a stale old-release file'}
  $installParent=Split-Path $install -Parent
  if(Get-ChildItem $installParent -Directory -Filter 'Lunitide.backup.*' -ErrorAction SilentlyContinue){throw 'upgrade retained a backup release directory'}
  if(Get-ChildItem $installParent -Directory -Filter 'Lunitide.installing.*' -ErrorAction SilentlyContinue){throw 'upgrade retained a staging directory'}
  if(-not(Test-Path (Join-Path $install 'Lunitide.exe'))){throw 'installed desktop missing'}
  if(-not(Test-Path (Join-Path $install 'purge-user-data.exe'))){throw 'installed purge helper missing'}
  if((Get-Content $owner -Raw) -cne $appid){throw 'installation ownership marker is missing or invalid'}
  if((Get-ItemProperty $uninstallKey).DisplayVersion -cne $ExpectedVersion){throw 'installed DisplayVersion does not match the expected release version'}
  & (Join-Path $PSScriptRoot 'Verify-Layout.ps1') -Stage $install -Version $ExpectedVersion -VerifyManifest -Installed -ExpectedSignerThumbprint $ExpectedSignerThumbprint
  & (Join-Path $PSScriptRoot 'Test-Runtime.ps1') -Executable (Join-Path $install 'Lunitide.exe')
  & (Join-Path $PSScriptRoot 'Verify-Layout.ps1') -Stage $install -Version $ExpectedVersion -VerifyManifest -Installed -ExpectedSignerThumbprint $ExpectedSignerThumbprint
  if((Get-ItemProperty $uninstallKey).DisplayName -cne 'Lunitide 月汐'){throw 'registered display name is missing or incorrectly encoded'}
  if((Get-ItemProperty $uninstallKey).InstallLocation -ne $install){throw 'registered installation path does not match the selected directory'}
  if((Get-ItemProperty $uninstallKey).UninstallString -ne ('"'+(Join-Path $install 'Uninstall.exe')+'"')){throw 'uninstall registration does not match the selected directory'}
  if(-not(Test-Path (Join-Path $profileAppData 'Microsoft\Windows\Start Menu\Programs\Lunitide\Lunitide.lnk'))){throw 'Start Menu shortcut missing'}
  Set-Content $owner 'not-the-appid' -NoNewline
  $p=Start-Process (Join-Path $install 'Uninstall.exe') -ArgumentList '/S' -Wait -PassThru
  $p.Dispose()
  $uninstallLog=Join-Path $profileLocalAppData 'LunitideInstaller\Logs\uninstall-latest.log'
  if(-not(Test-Path $uninstallLog) -or (Get-Content $uninstallLog -Raw) -notmatch 'code=U110 phase=ownership'){throw 'invalid-owner uninstall did not record the expected U110 refusal'}
  if(-not(Test-Path (Join-Path $install 'Lunitide.exe'))){throw 'uninstaller deleted files despite an invalid ownership marker'}
  Set-Content $owner $appid -NoNewline
  $p=Start-Process (Join-Path $install 'Uninstall.exe') -ArgumentList '/S' -Wait -PassThru; if($p.ExitCode){throw "retain uninstall failed: $($p.ExitCode)"}; $p.Dispose()
  Wait-Until { -not(Test-Path $install) } 'retain uninstall did not finish within the timeout'
  if(-not(Test-Path $marker)){throw 'default uninstall did not retain data'}
  if(Test-Path $uninstallKey){throw 'uninstall registry key survived'}
  if(Test-Path $install){throw 'default uninstall retained installation files'}
  if(Test-Path (Join-Path $profileAppData 'Microsoft\Windows\Start Menu\Programs\Lunitide')){throw 'default uninstall retained Start Menu shortcuts'}
  [GC]::Collect(); [GC]::WaitForPendingFinalizers()
  Assert-InstallerHash; $p=Start-Process $Installer -ArgumentList @('/S',"/D=$install") -Wait -PassThru; if($p.ExitCode){throw "reinstall failed: $($p.ExitCode)"}; $p.Dispose()
  $p=Start-Process (Join-Path $install 'Uninstall.exe') -ArgumentList '/S /PURGE' -Wait -PassThru; if($p.ExitCode){throw "purge uninstall failed: $($p.ExitCode)"}; $p.Dispose()
  Wait-Until { -not(Test-Path $install) -and -not(Test-Path $marker) } 'purge uninstall did not finish within the timeout'
  if(Test-Path $marker){throw 'explicit purge retained data'}
  if(Test-Path $install){throw 'purge uninstall retained installation files'}
  Write-Host "Install/uninstall retain and purge acceptance passed: $install"
} finally {
  try {
    if($ownsInstall -and (Test-Path (Join-Path $install 'stop-install-processes.ps1'))){& (Join-Path $install 'stop-install-processes.ps1') -Path $install 2>$null}
  } catch {
    Write-Warning "Failed to stop installed processes during acceptance cleanup: $_"
  }
  if($ownsInstall -and (Test-Path $install)){
    $ownedMarker=Join-Path $install '.lunitide-install-owner'
    $legacyOwned=(Test-Path $uninstallKey) -and ((Get-ItemProperty $uninstallKey -ErrorAction SilentlyContinue).InstallLocation -eq $install)
    $modernOwned=(Test-Path $ownedMarker) -and ((Get-Content $ownedMarker -Raw).Trim() -ceq $appid)
    if($legacyOwned -or $modernOwned){Remove-Item $install -Recurse -Force -ErrorAction SilentlyContinue}
  }
  Remove-Item $uninstallKey -Recurse -Force -ErrorAction SilentlyContinue
  if($ownsData){Remove-Item $data -Recurse -Force -ErrorAction SilentlyContinue}
  Remove-Item $TestRoot -Recurse -Force -ErrorAction SilentlyContinue
}
