param(
  [Parameter(Mandatory)][string]$Installer,
  [Parameter(Mandatory)][string]$ExpectedVersion,
  [Parameter(Mandatory)][string]$ExpectedInstallerHash,
  [string]$ExpectedSignerThumbprint,
  [string]$EvidenceOut,
  [string]$InstallDirectory,
  [string]$Commit,
  [switch]$AllowUnsignedDevelopment,
  [switch]$ExpectRuntimeAbsent
)
# M3 clean-machine matrix runner. One invocation produces one evidence record
# for the (OS build x WebView2 runtime state) cell it runs on: it captures the
# environment facts, executes the full install/upgrade/retain/reinstall/PURGE
# lifecycle acceptance (Test-Install.ps1), and writes a JSON evidence record
# that binds the result to the commit and artifact digests. Run it once per
# matrix cell, each time on a fresh disposable Windows account/VM:
#   Win10 x64 + Runtime present | Win10 x64 + Runtime absent
#   Win11 x64 + Runtime present | Win11 x64 + Runtime absent
# The script refuses to run when an installation or data root already exists
# (delegated to Test-Install.ps1), so cells stay mutually exclusive.
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot 'Resolve-SignTool.ps1')
$Installer=(Resolve-Path $Installer).Path
function Get-WebView2RuntimeVersion {
  $client='{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
  foreach($view in @('Registry32','Registry64')){
    foreach($hive in @('LocalMachine','CurrentUser')){
      try {
        $key=[Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::$hive,[Microsoft.Win32.RegistryView]::$view)
        $sub=$key.OpenSubKey("SOFTWARE\Microsoft\EdgeUpdate\Clients\$client")
        if($sub){$pv=$sub.GetValue('pv'); if($pv){return [string]$pv}}
      } catch {}
    }
  }
  return $null
}
$os=Get-CimInstance Win32_OperatingSystem
$osArch='other'; if((Get-CimInstance Win32_Processor).Architecture -eq 9){$osArch='x64'}
$cell=[ordered]@{
  osCaption=$os.Caption
  osBuild=$os.BuildNumber
  osArch=$osArch
  webview2Runtime=(Get-WebView2RuntimeVersion)
  powershell=$PSVersionTable.PSVersion.ToString()
  timestampUtc=[DateTime]::UtcNow.ToString('o')
}
if(-not $cell.webview2Runtime){$cell.webview2Runtime='absent'}
$commit=$Commit
if(-not $commit){try { $commit=(& git -C (Join-Path $PSScriptRoot '..') rev-parse HEAD 2>$null) } catch {}}
$sig=Get-AuthenticodeSignature $Installer
$evidence=[ordered]@{
  schemaVersion=1
  cell=$cell
  commit=$commit
  installer=[ordered]@{
    path=$Installer
    sha256=(Get-FileHash $Installer -Algorithm SHA256).Hash.ToLowerInvariant()
    signatureStatus=[string]$sig.Status
    signerThumbprint=if($sig.SignerCertificate){$sig.SignerCertificate.Thumbprint}else{$null}
    timestamped=[bool]$sig.TimeStamperCertificate
  }
  lifecycle='not-run'
  failure=$null
}
Write-Host "Matrix cell: $($cell.osCaption) build $($cell.osBuild), WebView2 Runtime: $($cell.webview2Runtime)"
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class LunitideDialogProbe {
  [DllImport("user32.dll", SetLastError=true, CharSet=CharSet.Unicode)]
  public static extern IntPtr FindWindow(string lpClassName, string lpWindowName);
  [DllImport("user32.dll", SetLastError=true)]
  public static extern IntPtr SendMessage(IntPtr hWnd, uint Msg, IntPtr wParam, IntPtr lParam);
}
'@ -ErrorAction SilentlyContinue
function Assert-AbsentCell {
  # Runtime-absent acceptance (handoff: approved failure path + cleanup):
  # silent install succeeds and logs W140; the desktop then fails closed with
  # the runtime-missing dialog and a non-zero exit, leaving no child
  # processes; purge uninstall removes everything.
  $appid='Lunitide.Desktop.7A565D82-936E-4E06-962D-83B5DD24E53C'
  $uninstallKey="HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$appid"
  if(Test-Path $uninstallKey){throw 'Refusing to run: existing Lunitide registration; use a disposable account'}
  $profileLocalAppData=[Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
  $data=Join-Path $profileLocalAppData 'Lunitide'
  if(Test-Path $data){throw 'Refusing to run: existing Lunitide data root; use a disposable account'}
  $install=if($InstallDirectory){[IO.Path]::GetFullPath($InstallDirectory).TrimEnd('\')}else{Join-Path $profileLocalAppData 'Programs\Lunitide'}
  if(Test-Path $install){throw 'Refusing to run: existing installation directory; use a disposable account'}
  if((Get-FileHash $Installer -Algorithm SHA256).Hash -cne $ExpectedInstallerHash.ToUpperInvariant()){throw 'Installer SHA-256 mismatch'}
  $ownsInstall=$false
  try {
    $p=Start-Process $Installer -ArgumentList @('/S',"/D=$install") -Wait -PassThru
    if($p.ExitCode){throw "silent install failed on a runtime-absent machine: $($p.ExitCode)"}
    $ownsInstall=$true
    $installLog=Join-Path $profileLocalAppData 'LunitideInstaller\Logs\install-latest.log'
    if(-not(Test-Path $installLog) -or (Get-Content $installLog -Raw) -notmatch 'code=W140'){throw 'install log is missing the W140 runtime-absent warning'}
    if(-not(Test-Path (Join-Path $install 'Lunitide.exe'))){throw 'installed desktop missing after silent install'}
    $desktop=Start-Process (Join-Path $install 'Lunitide.exe') -PassThru
    $deadline=[DateTime]::UtcNow.AddSeconds(30)
    $dialog=[IntPtr]::Zero
    # Title must match showRuntimeMissingDialog in internal/webviewhost/runtime_missing_windows.go.
    while([DateTime]::UtcNow -lt $deadline -and -not $desktop.HasExited){
      $dialog=[LunitideDialogProbe]::FindWindow('#32770','Lunitide - WebView2 Runtime missing')
      if($dialog -ne [IntPtr]::Zero){break}
      Start-Sleep -Milliseconds 250
    }
    if($dialog -eq [IntPtr]::Zero){throw 'controlled-failure dialog did not appear on a runtime-absent machine'}
    [void][LunitideDialogProbe]::SendMessage($dialog,0x0010,[IntPtr]::Zero,[IntPtr]::Zero)  # WM_CLOSE
    $desktop.WaitForExit(15000) | Out-Null
    if(-not $desktop.HasExited){Stop-Process -Id $desktop.Id -Force; throw 'desktop did not exit after the dialog was dismissed'}
    if($desktop.ExitCode -eq 0){throw 'desktop exited 0 on a runtime-absent machine; fail-closed violated'}
    Start-Sleep -Milliseconds 1000
    if(Get-CimInstance Win32_Process -Filter "Name='lunitide-engine.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -like "*$install*" }){throw 'engine process survived the controlled failure'}
    $u=Start-Process (Join-Path $install 'Uninstall.exe') -ArgumentList '/S','/PURGE' -Wait -PassThru
    if($u.ExitCode){throw "purge uninstall failed: $($u.ExitCode)"}
    $waitUntil=[DateTime]::UtcNow.AddSeconds(20)
    while((Test-Path $install) -and [DateTime]::UtcNow -lt $waitUntil){Start-Sleep -Milliseconds 250}
    if(Test-Path $install){throw 'purge uninstall retained installation files'}
    if(Test-Path $data){throw 'purge uninstall retained the data root'}
    Write-Host 'Runtime-absent cell passed: guided install warning, controlled launch failure, clean purge'
  } finally {
    if($ownsInstall -and (Test-Path $install)){
      try { & (Join-Path $install 'Uninstall.exe') /S /PURGE | Out-Null } catch {}
      Remove-Item $install -Recurse -Force -ErrorAction SilentlyContinue
    }
    Remove-Item $uninstallKey -Recurse -Force -ErrorAction SilentlyContinue
  }
}
try {
  if(-not $AllowUnsignedDevelopment){
    if($ExpectedSignerThumbprint -notmatch '\A[0-9A-Fa-f]{40}\z'){throw 'Signed matrix acceptance requires -ExpectedSignerThumbprint (or explicit -AllowUnsignedDevelopment for a rehearsal cell)'}
    if($sig.Status -ne 'Valid' -or -not $sig.SignerCertificate -or $sig.SignerCertificate.Thumbprint -cne $ExpectedSignerThumbprint.ToUpperInvariant() -or -not $sig.TimeStamperCertificate){throw 'Installer publisher signature or timestamp is invalid'}
    & (Resolve-SignTool) verify /pa /all /v $Installer; if($LASTEXITCODE){throw 'Windows policy rejected the installer signature or timestamp chain'}
  }
  if($ExpectRuntimeAbsent){
    if($cell.webview2Runtime -ne 'absent'){throw "This machine reports a WebView2 Runtime ($($cell.webview2Runtime)); the absent-cell assertions would not exercise the failure path"}
    Assert-AbsentCell
    $evidence.lifecycle='pass'
    $evidence.absentCell=$true
  } else {
  $args=@{Installer=$Installer;ExpectedVersion=$ExpectedVersion;ExpectedInstallerHash=$ExpectedInstallerHash}
  if($AllowUnsignedDevelopment){$args.AllowUnsignedDevelopment=$true}
  elseif($ExpectedSignerThumbprint){$args.ExpectedSignerThumbprint=$ExpectedSignerThumbprint}
  else{throw 'Signed matrix acceptance requires -ExpectedSignerThumbprint (or explicit -AllowUnsignedDevelopment for a rehearsal cell)'}
  if($InstallDirectory){$args.InstallDirectory=$InstallDirectory}
  & (Join-Path $PSScriptRoot 'Test-Install.ps1') @args
  $evidence.lifecycle='pass'
  }
} catch {
  $evidence.lifecycle='fail'
  $evidence.failure=[string]$_
}
if(-not $EvidenceOut){$EvidenceOut=Join-Path $PSScriptRoot ('matrix-evidence-{0}-wv2-{1}-{2}.json' -f $cell.osBuild,$(if($cell.webview2Runtime -eq 'absent'){'absent'}else{'present'}),[DateTime]::UtcNow.ToString('yyyyMMddHHmmss'))}
$EvidenceOut=[IO.Path]::GetFullPath($EvidenceOut)
[IO.File]::WriteAllText($EvidenceOut,($evidence | ConvertTo-Json -Depth 6),(New-Object Text.UTF8Encoding $false))
Write-Host "Evidence: $EvidenceOut ($($evidence.lifecycle))"
if($evidence.lifecycle -ne 'pass'){exit 1}
