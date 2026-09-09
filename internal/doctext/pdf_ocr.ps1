param([Parameter(Mandatory=$true)][string]$InputPath, [Parameter(Mandatory=$true)][string]$OutputPath)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$null = [Windows.Storage.StorageFile,Windows.Storage,ContentType=WindowsRuntime]
$null = [Windows.Data.Pdf.PdfDocument,Windows.Data.Pdf,ContentType=WindowsRuntime]
$null = [Windows.Storage.Streams.InMemoryRandomAccessStream,Windows.Storage.Streams,ContentType=WindowsRuntime]
$null = [Windows.Graphics.Imaging.BitmapDecoder,Windows.Graphics.Imaging,ContentType=WindowsRuntime]
$null = [Windows.Media.Ocr.OcrEngine,Windows.Foundation,ContentType=WindowsRuntime]
$operation = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' } | Select-Object -First 1
$action = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and -not $_.IsGenericMethod -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncAction' } | Select-Object -First 1
function AwaitResult($async, [Type]$type) {
  $task = $operation.MakeGenericMethod($type).Invoke($null, @($async))
  $task.GetAwaiter().GetResult()
}
function AwaitAction($async) {
  $task = $action.Invoke($null, @($async))
  $task.GetAwaiter().GetResult()
}
$engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
if ($null -eq $engine) { throw 'Local OCR language is unavailable. Install a Windows OCR language pack.' }
$file = AwaitResult ([Windows.Storage.StorageFile]::GetFileFromPathAsync($InputPath)) ([Windows.Storage.StorageFile])
$pdf = AwaitResult ([Windows.Data.Pdf.PdfDocument]::LoadFromFileAsync($file)) ([Windows.Data.Pdf.PdfDocument])
if ($pdf.PageCount -gt 100) { throw 'OCR page budget exceeded (100 pages).' }
$pages = [System.Collections.Generic.List[object]]::new()
$characters = 0
for ($index = 0; $index -lt $pdf.PageCount; $index++) {
  $page = $pdf.GetPage($index)
  $stream = [Windows.Storage.Streams.InMemoryRandomAccessStream]::new()
  $bitmap = $null
  try {
    $options = [Windows.Data.Pdf.PdfPageRenderOptions]::new()
    $scale = [Math]::Min(2.5, 2400.0 / [Math]::Max($page.Size.Width, $page.Size.Height))
    $options.DestinationWidth = [uint32][Math]::Max(1, [Math]::Round($page.Size.Width * $scale))
    $options.DestinationHeight = [uint32][Math]::Max(1, [Math]::Round($page.Size.Height * $scale))
    AwaitAction ($page.RenderToStreamAsync($stream, $options))
    $stream.Seek(0)
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
    $characters += $text.Length
    if ($characters -gt 500000) { throw 'OCR text budget exceeded.' }
    $pages.Add(@{ page = $index + 1; text = $text; rotation = $rotation })
  } finally {
    if ($null -ne $bitmap) { $bitmap.Dispose() }
    $stream.Dispose()
    $page.Dispose()
  }
}
$json = @{ method = 'windows-ocr'; language = $engine.RecognizerLanguage.LanguageTag; pages = $pages.ToArray() } | ConvertTo-Json -Depth 4 -Compress
[System.IO.File]::WriteAllText($OutputPath, $json, [System.Text.UTF8Encoding]::new($false))
