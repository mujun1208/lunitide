param([Parameter(Mandatory=$true)][string]$InputPath, [Parameter(Mandatory=$true)][string]$OutputPath)
$ErrorActionPreference = 'Stop'
function Write-Probe([string]$State, [object]$Languages, [string]$Text, [string]$ErrorCode) {
  if ($null -eq $Languages) { $Languages = @() }
  $payload = @{
    state = $State
    languages = @($Languages)
    text = $Text
    errorCode = $ErrorCode
  } | ConvertTo-Json -Compress -Depth 4
  [System.IO.File]::WriteAllText($OutputPath, $payload, [System.Text.UTF8Encoding]::new($false))
}
try {
  Add-Type -AssemblyName System.Runtime.WindowsRuntime
  $null = [Windows.Storage.StorageFile,Windows.Storage,ContentType=WindowsRuntime]
  $null = [Windows.Graphics.Imaging.BitmapDecoder,Windows.Graphics.Imaging,ContentType=WindowsRuntime]
  $null = [Windows.Media.Ocr.OcrEngine,Windows.Foundation,ContentType=WindowsRuntime]
} catch {
  Write-Probe 'initialization_failed' @() '' 'OCR_WINRT_INIT'
  exit 0
}
$operation = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' } | Select-Object -First 1
if ($null -eq $operation) {
  Write-Probe 'initialization_failed' @() '' 'OCR_WINRT_ASYNC'
  exit 0
}
function AwaitResult($async, [Type]$type) {
  $task = $operation.MakeGenericMethod($type).Invoke($null, @($async))
  $task.GetAwaiter().GetResult()
}
$languages = @()
try {
  $available = [Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages
  foreach ($lang in $available) { $languages += $lang.LanguageTag }
} catch {
  Write-Probe 'initialization_failed' @() '' 'OCR_LANGUAGE_ENUM'
  exit 0
}
$engine = $null
try {
  $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
} catch {
  Write-Probe 'initialization_failed' $languages '' 'OCR_ENGINE_CREATE'
  exit 0
}
if ($null -eq $engine -or $languages.Count -eq 0) {
  Write-Probe 'language_unavailable' $languages '' 'OCR_LANGUAGE_MISSING'
  exit 0
}
try {
  $file = AwaitResult ([Windows.Storage.StorageFile]::GetFileFromPathAsync($InputPath)) ([Windows.Storage.StorageFile])
  $stream = AwaitResult ($file.OpenReadAsync()) ([Windows.Storage.Streams.IRandomAccessStream])
  $bitmap = $null
  try {
    $decoder = AwaitResult ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
    $transform = [Windows.Graphics.Imaging.BitmapTransform]::new()
    $bitmap = AwaitResult ($decoder.GetSoftwareBitmapAsync([Windows.Graphics.Imaging.BitmapPixelFormat]::Bgra8, [Windows.Graphics.Imaging.BitmapAlphaMode]::Ignore, $transform, [Windows.Graphics.Imaging.ExifOrientationMode]::IgnoreExifOrientation, [Windows.Graphics.Imaging.ColorManagementMode]::DoNotColorManage)) ([Windows.Graphics.Imaging.SoftwareBitmap])
    $result = AwaitResult ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
    $text = (($result.Lines | ForEach-Object { $_.Text }) -join ' ').Trim()
  } finally {
    if ($null -ne $bitmap) { $bitmap.Dispose() }
    $stream.Dispose()
  }
} catch {
  Write-Probe 'sample_failed' $languages '' 'OCR_SAMPLE_FAILED'
  exit 0
}
if ([string]::IsNullOrWhiteSpace($text)) {
  Write-Probe 'sample_failed' $languages '' 'OCR_SAMPLE_EMPTY'
  exit 0
}
Write-Probe 'ready' $languages $text ''
