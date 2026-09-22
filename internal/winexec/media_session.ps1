$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Console]::InputEncoding = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$request = [Console]::In.ReadToEnd() | ConvertFrom-Json
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$managerType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager,Windows.Media.Control,ContentType=WindowsRuntime]
$propertiesType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties,Windows.Media.Control,ContentType=WindowsRuntime]
$asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' } | Select-Object -First 1
function Await($operation, $type) {
    $task = $asTask.MakeGenericMethod($type).Invoke($null, @($operation))
    if (-not $task.Wait(2500)) { throw 'Media operation timed out' }
    return $task.Result
}
$manager = Await ($managerType::RequestAsync()) $managerType
function Test-MediaAlias([string]$aumid, $aliases) {
    if ([string]::IsNullOrWhiteSpace($aumid)) { return $false }
    foreach ($alias in @($aliases)) {
        $name = [string]$alias
        if ([string]::IsNullOrWhiteSpace($name)) { continue }
        if ($aumid.IndexOf($name, [StringComparison]::OrdinalIgnoreCase) -ge 0) { return $true }
        $compact = ($name -replace '\s', '')
        if ($compact.Length -ge 3 -and $aumid.IndexOf($compact, [StringComparison]::OrdinalIgnoreCase) -ge 0) { return $true }
        $stem = [IO.Path]::GetFileNameWithoutExtension($name)
        if ($stem -and $stem -ne $name -and $stem.Length -ge 3 -and $aumid.IndexOf($stem, [StringComparison]::OrdinalIgnoreCase) -ge 0) { return $true }
    }
    return $false
}
$currentId = ''
try { $currentId = [string]$manager.GetCurrentSession().SourceAppUserModelId } catch {}
$sessions = @($manager.GetSessions() | Where-Object { Test-MediaAlias $_.SourceAppUserModelId $request.aliases })
if ($sessions.Count -gt 1 -and $currentId) {
    $preferred = @($sessions | Where-Object { $_.SourceAppUserModelId -eq $currentId })
    if ($preferred.Count -eq 1) { $sessions = $preferred }
}
if ($sessions.Count -ne 1) { throw 'No unique matching media session' }
$session = $sessions[0]
$beforeTitle = ''
try { $beforeTitle = (Await ($session.TryGetMediaPropertiesAsync()) $propertiesType).Title } catch {}
$accepted = $false
if ($request.action -eq 'play' -and $request.shuffle -and $session.GetPlaybackInfo().Controls.IsShuffleEnabled) {
    try { $null = Await ($session.TryChangeShuffleActiveAsync($true)) ([bool]) } catch {}
}
try {
switch ($request.action) {
    'status' { $accepted = $true }
    'play' { $accepted = Await ($session.TryPlayAsync()) ([bool]) }
    'pause' { $accepted = Await ($session.TryPauseAsync()) ([bool]) }
    'next' { $accepted = Await ($session.TrySkipNextAsync()) ([bool]) }
    'prev' { $accepted = Await ($session.TrySkipPreviousAsync()) ([bool]) }
    'stop' { $accepted = Await ($session.TryStopAsync()) ([bool]) }
    default { throw 'Unsupported media action' }
}
} catch { $accepted = $false }
$verified = $false
$properties = $null
for ($i = 0; $i -lt 8; $i++) {
    $info = $session.GetPlaybackInfo()
    $status = $info.PlaybackStatus.ToString()
    try { $properties = Await ($session.TryGetMediaPropertiesAsync()) $propertiesType } catch {}
    switch ($request.action) {
        'status' { $verified = $true }
        'play' { $verified = $accepted -and $status -eq 'Playing' }
        'pause' { $verified = $accepted -and $status -eq 'Paused' }
        'stop' { $verified = $accepted -and $status -eq 'Stopped' }
        { $_ -in 'next','prev' } { $verified = $accepted -and $status -eq 'Playing' -and $properties.Title -and $beforeTitle -and $properties.Title -ne $beforeTitle }
    }
    if ($verified -or -not $accepted) { break }
    Start-Sleep -Milliseconds 250
}
$capabilities = @()
try {
    $controls = $info.Controls
    if ($controls.IsPlayEnabled) { $capabilities += 'play' }
    if ($controls.IsPauseEnabled) { $capabilities += 'pause' }
    if ($controls.IsStopEnabled) { $capabilities += 'stop' }
    if ($controls.IsSkipNextEnabled) { $capabilities += 'next' }
    if ($controls.IsSkipPreviousEnabled) { $capabilities += 'previous' }
} catch {}
$positionMs = 0
$durationMs = 0
try {
    $timeline = $session.GetTimelineProperties()
    if ($timeline.Position) { $positionMs = [int64]$timeline.Position.TotalMilliseconds }
    if ($timeline.EndTime) { $durationMs = [int64]$timeline.EndTime.TotalMilliseconds }
} catch {}
[ordered]@{
    app = $session.SourceAppUserModelId
    status = $status
    title = $properties.Title
    artist = $properties.Artist
    verified = [bool]$verified
    shuffle = ($info.IsShuffleActive -eq $true)
    sessionKey = [string]$session.SourceAppUserModelId
    capabilities = @($capabilities)
    positionMs = $positionMs
    durationMs = $durationMs
} | ConvertTo-Json -Compress
