$ErrorActionPreference = 'SilentlyContinue'
$names = @(
  'Language.OCR~~~zh-CN~0.0.1.0',
  'Language.OCR~~~en-US~0.0.1.0'
)
foreach ($name in $names) {
  $cap = Get-WindowsCapability -Online -Name $name
  if ($null -eq $cap) { continue }
  if ($cap.State -eq 'Installed') { continue }
  Add-WindowsCapability -Online -Name $name | Out-Null
}
exit 0
