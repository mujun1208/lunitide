param(
  [switch]$Remove,
  [int]$ValidityDays = 90
)
# Creates (or removes) a self-signed code-signing certificate used ONLY to
# rehearse the signing pipeline before the approved publisher certificate
# exists. Signatures made with this identity prove pipeline mechanics
# (sign command, thumbprint pinning, RFC3161 timestamp, signtool /pa policy
# verification); they are never evidence of publisher identity, and any
# artifact signed with it remains non-publishable.
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
$subject='CN=Lunitide Rehearsal Publisher (TEST ONLY, NOT A RELEASE IDENTITY)'
if($Remove){
  $found=@(Get-ChildItem Cert:\CurrentUser\My,Cert:\CurrentUser\Root,Cert:\CurrentUser\TrustedPublisher -ErrorAction SilentlyContinue | Where-Object { $_.Subject -eq $subject })
  foreach($store in 'My','Root','TrustedPublisher'){
    foreach($c in @(Get-ChildItem "Cert:\CurrentUser\$store" -ErrorAction SilentlyContinue | Where-Object { $_.Subject -eq $subject })){
      Remove-Item $c.PSPath -Force
      Write-Host "Removed $($c.Thumbprint) from Cert:\CurrentUser\$store"
    }
  }
  if(-not $found){Write-Host 'No rehearsal certificate present'}
  return
}
$existing=Get-ChildItem Cert:\CurrentUser\My -ErrorAction SilentlyContinue | Where-Object { $_.Subject -eq $subject -and $_.NotAfter -gt [DateTime]::Now } | Select-Object -First 1
if($existing){$cert=$existing; Write-Host "Reusing existing rehearsal certificate $($cert.Thumbprint)"}
else{
  $cert=New-SelfSignedCertificate -Type CodeSigningCert -Subject $subject -CertStoreLocation Cert:\CurrentUser\My `
    -NotAfter ([DateTime]::Now.AddDays($ValidityDays)) -KeyUsage DigitalSignature -KeyLength 2048 -KeyAlgorithm RSA `
    -HashAlgorithm sha256 -TextExtension @('2.5.29.37={text}1.3.6.1.5.5.7.3.3')
  Write-Host "Created rehearsal certificate $($cert.Thumbprint)"
}
# signtool verify /pa requires the chain to root in a trusted store. CurrentUser
# scope needs no elevation and never affects other accounts.
$export=Join-Path ([IO.Path]::GetTempPath()) ('lunitide-rehearsal-'+[guid]::NewGuid().ToString('N')+'.cer')
try {
  Export-Certificate -Cert $cert.PSPath -FilePath $export -Force | Out-Null
  & certutil.exe -user -addstore -f Root $export | Out-Null; if($LASTEXITCODE){throw 'certutil failed to trust the rehearsal root (CurrentUser\Root)'}
  & certutil.exe -user -addstore -f TrustedPublisher $export | Out-Null; if($LASTEXITCODE){throw 'certutil failed to trust the rehearsal publisher (CurrentUser\TrustedPublisher)'}
} finally { Remove-Item $export -Force -ErrorAction SilentlyContinue }
Write-Warning 'This is a rehearsal identity. It is NOT an approved publisher; artifacts signed with it stay non-publishable. Remove it with: New-TestPublisherCertificate.ps1 -Remove'
Write-Host ''
Write-Host 'Pipeline rehearsal configuration:'
Write-Host ('  $env:LUNITIDE_SIGNER_THUMBPRINT = "{0}"' -f $cert.Thumbprint)
Write-Host ('  $env:LUNITIDE_SIGN_COMMAND = "& ''{0}'' -Artifact ''{{artifact}}''"' -f (Join-Path $PSScriptRoot 'Sign-Artifact.ps1'))
