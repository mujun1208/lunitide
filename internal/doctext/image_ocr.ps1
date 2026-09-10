param([Parameter(Mandatory=$true)][string]$InputPath, [Parameter(Mandatory=$true)][string]$OutputPath)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$null = [Windows.Storage.StorageFile,Windows.Storage,ContentType=WindowsRuntime]
$null = [Windows.Graphics.Imaging.BitmapDecoder,Windows.Graphics.Imaging,ContentType=WindowsRuntime]
$null = [Windows.Media.Ocr.OcrEngine,Windows.Foundation,ContentType=WindowsRuntime]
$operation = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' } | Select-Object -First 1
function AwaitResult($async, [Type]$type) {
  $task = $operation.MakeGenericMethod($type).Invoke($null, @($async))
  $task.GetAwaiter().GetResult()
}
$engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
if ($null -eq $engine) { throw 'Local OCR language is unavailable. Install a Windows OCR language pack.' }
$file = AwaitResult ([Windows.Storage.StorageFile]::GetFileFromPathAsync($InputPath)) ([Windows.Storage.StorageFile])
$stream = AwaitResult ($file.OpenReadAsync()) ([Windows.Storage.Streams.IRandomAccessStream])
$bitmap = $null
try {
  $decoder = AwaitResult ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
  $text = ''
  $rotation = 'None'
  foreach ($angle in @('None', 'Clockwise90Degrees', 'Clockwise180Degrees', 'Clockwise270Degrees')) {
    $transform = [Windows.Graphics.Imaging.BitmapTransform]::new()
    $transform.Rotation = [Windows.Graphics.Imaging.BitmapRotation]::$angle
    $bitmap = AwaitResult ($decoder.GetSoftwareBitmapAsync([Windows.Graphics.Imaging.BitmapPixelFormat]::Bgra8, [Windows.Graphics.Imaging.BitmapAlphaMode]::Ignore, $transform, [Windows.Graphics.Imaging.ExifOrientationMode]::IgnoreExifOrientation, [Windows.Graphics.Imaging.ColorManagementMode]::DoNotColorManage)) ([Windows.Graphics.Imaging.SoftwareBitmap])
    $result = AwaitResult ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
    $candidate = ($result.Lines | ForEach-Object { $_.Text }) -join "`n"
    if ($candidate.Length -gt $text.Length) { $text = $candidate; $rotation = $angle }
    $bitmap.Dispose()
    $bitmap = $null
  }
  if ($text.Length -gt 500000) { throw 'OCR text budget exceeded.' }
  $pages = @(@{ page = 1; text = $text; rotation = $rotation })
  $json = @{ method = 'windows-ocr'; language = $engine.RecognizerLanguage.LanguageTag; pages = $pages } | ConvertTo-Json -Depth 4 -Compress
  [System.IO.File]::WriteAllText($OutputPath, $json, [System.Text.UTF8Encoding]::new($false))
} finally {
  if ($null -ne $bitmap) { $bitmap.Dispose() }
  $stream.Dispose()
}
