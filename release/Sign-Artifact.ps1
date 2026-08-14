param(
  [Parameter(Mandatory)][string]$Artifact,
  [string]$Thumbprint = $env:LUNITIDE_SIGNER_THUMBPRINT,
  [string]$TimestampUrl = $(if ($env:LUNITIDE_TIMESTAMP_URL) { $env:LUNITIDE_TIMESTAMP_URL } else { 'http://timestamp.digicert.com' })
)
# Standard Authenticode sign command behind LUNITIDE_SIGN_COMMAND. Build-Release.ps1
# substitutes {artifact} with the absolute path and runs this via
# `powershell -NoProfile -Command`; configure it as:
#   $env:LUNITIDE_SIGN_COMMAND = "& '<repo>\release\Sign-Artifact.ps1' -Artifact '{artifact}'"
# The signer certificate (with private key) must already exist in a certificate
# store visible to signtool /sha1 (CurrentUser\My or LocalMachine\My). The
# signature is SHA-256 file digest + SHA-256 RFC3161 timestamp; the trusted
# timestamp is mandatory because the release verifier rejects unsigned timestamps.
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot 'Resolve-SignTool.ps1')
$Artifact=(Resolve-Path $Artifact).Path
if($Thumbprint -notmatch '\A[0-9A-Fa-f]{40}\z'){throw 'Signer thumbprint must be exactly 40 hexadecimal characters (set LUNITIDE_SIGNER_THUMBPRINT or pass -Thumbprint)'}
$Thumbprint=$Thumbprint.ToUpperInvariant()
$signtool=Resolve-SignTool
$cert=Get-ChildItem Cert:\CurrentUser\My,Cert:\LocalMachine\My -ErrorAction SilentlyContinue |
  Where-Object { $_.Thumbprint -ceq $Thumbprint } | Select-Object -First 1
if(-not $cert){throw "Signer certificate $Thumbprint not found in CurrentUser\\My or LocalMachine\\My"}
if(-not $cert.HasPrivateKey){throw "Signer certificate $Thumbprint has no private key in the certificate store"}
if($cert.NotAfter -lt [DateTime]::Now){throw "Signer certificate $Thumbprint expired at $($cert.NotAfter.ToString('u'))"}
& $signtool sign /fd sha256 /td sha256 /tr $TimestampUrl /sha1 $Thumbprint $Artifact
if($LASTEXITCODE){throw "signtool sign failed for $Artifact (exit $LASTEXITCODE); check timestamp server reachability: $TimestampUrl"}
$sig=Get-AuthenticodeSignature $Artifact
if($sig.Status -eq 'NotSigned'){throw "signtool reported success but no signature is present: $Artifact"}
if(-not $sig.TimeStamperCertificate){throw "Signature is missing the RFC3161 countersignature: $Artifact"}
if($sig.SignerCertificate.Thumbprint -cne $Thumbprint){throw "Signed with an unexpected certificate: $Artifact"}
# Chain trust is asserted authoritatively by Build-Release/Verify-Release via
# signtool verify /pa /all /v; here an untrusted chain is only a warning so the
# same script works before the test root is installed.
if($sig.Status -ne 'Valid'){Write-Warning "Signature chain is not trusted on this machine yet ($($sig.Status)): $Artifact"}
Write-Host "Signed: $Artifact"
