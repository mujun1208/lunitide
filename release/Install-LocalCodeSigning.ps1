param(
  [string]$Publisher = 'Yy.MJ',
  [int]$ValidityDays = 1095,
  [switch]$Remove
)
# Installs a CurrentUser code-signing identity so Build-Release.ps1 can emit
# Authenticode signatures with RFC3161 timestamps. The certificate is trusted
# only for this Windows account (Root + TrustedPublisher). Other PCs still see
# an unknown publisher until a CA-issued OV/EV certificate replaces it.
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
$subject = "CN=$Publisher, O=$Publisher"
$signScript = Join-Path $PSScriptRoot 'Sign-Artifact.ps1'
function Get-PublisherCerts([string]$StoreName) {
  @(Get-ChildItem "Cert:\CurrentUser\$StoreName" -ErrorAction SilentlyContinue | Where-Object { $_.Subject -eq $subject })
}
if ($Remove) {
  foreach ($store in 'My','Root','TrustedPublisher') {
    foreach ($c in (Get-PublisherCerts $store)) {
      Remove-Item $c.PSPath -Force
      Write-Host "Removed $($c.Thumbprint) from Cert:\CurrentUser\$store"
    }
  }
  [Environment]::SetEnvironmentVariable('LUNITIDE_SIGNER_THUMBPRINT', $null, 'User')
  [Environment]::SetEnvironmentVariable('LUNITIDE_SIGN_COMMAND', $null, 'User')
  $env:LUNITIDE_SIGNER_THUMBPRINT = $null
  $env:LUNITIDE_SIGN_COMMAND = $null
  Write-Host 'Cleared CurrentUser LUNITIDE_SIGN_* variables'
  return
}
$existing = Get-ChildItem Cert:\CurrentUser\My -ErrorAction SilentlyContinue |
  Where-Object { $_.Subject -eq $subject -and $_.HasPrivateKey -and $_.NotAfter -gt [DateTime]::Now } |
  Select-Object -First 1
if ($existing) {
  $cert = $existing
  Write-Host "Reusing code-signing certificate $($cert.Thumbprint)"
} else {
  $cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject $subject -CertStoreLocation Cert:\CurrentUser\My `
    -NotAfter ([DateTime]::Now.AddDays($ValidityDays)) -KeyUsage DigitalSignature -KeyLength 2048 -KeyAlgorithm RSA `
    -HashAlgorithm sha256 -TextExtension @('2.5.29.37={text}1.3.6.1.5.5.7.3.3')
  Write-Host "Created code-signing certificate $($cert.Thumbprint)"
}
$export = Join-Path ([IO.Path]::GetTempPath()) ('lunitide-publisher-'+[guid]::NewGuid().ToString('N')+'.cer')
try {
  Export-Certificate -Cert $cert.PSPath -FilePath $export -Force | Out-Null
  & certutil.exe -user -addstore -f Root $export | Out-Null
  if ($LASTEXITCODE) { throw 'certutil failed to trust the publisher root (CurrentUser\Root)' }
  & certutil.exe -user -addstore -f TrustedPublisher $export | Out-Null
  if ($LASTEXITCODE) { throw 'certutil failed to trust the publisher (CurrentUser\TrustedPublisher)' }
} finally {
  Remove-Item $export -Force -ErrorAction SilentlyContinue
}
$thumb = $cert.Thumbprint.ToUpperInvariant()
$signCommand = "& '$signScript' -Artifact '{artifact}'"
[Environment]::SetEnvironmentVariable('LUNITIDE_SIGNER_THUMBPRINT', $thumb, 'User')
[Environment]::SetEnvironmentVariable('LUNITIDE_SIGN_COMMAND', $signCommand, 'User')
$env:LUNITIDE_SIGNER_THUMBPRINT = $thumb
$env:LUNITIDE_SIGN_COMMAND = $signCommand
Write-Host "Publisher subject: $subject"
Write-Host 'User environment LUNITIDE_SIGN_COMMAND and LUNITIDE_SIGNER_THUMBPRINT are set for this account.'
Write-Warning 'This identity is trusted on this Windows account only. Other machines need a CA-issued code-signing certificate for SmartScreen / unknown-publisher prompts to go away.'
