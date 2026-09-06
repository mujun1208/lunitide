#requires -Version 5.1
# Read-only observation of an already running user-selected release. Does not
# launch workloads, close windows, invoke providers, or read application data.
param(
  [Parameter(Mandatory)][string]$InstallDirectory,
  [Parameter(Mandatory)][string]$ExpectedVersion,
  [Parameter(Mandatory)][string]$ExpectedManifestHash,
  [Parameter(Mandatory)][string]$EvidenceDirectory,
  [ValidateRange(0.01,168)][double]$DurationHours=8,
  [ValidateRange(1,60)][int]$SampleSeconds=15,
  [ValidateRange(1,65536)][int]$MemoryGrowthLimitMB=256,
  [ValidateRange(1,100000)][int]$HandleGrowthLimit=256,
  [string]$ExpectedSignerThumbprint,
  [switch]$AllowUnsignedDevelopment
)
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot 'Release-Safety.ps1')
$install=(Resolve-Path -LiteralPath $InstallDirectory).Path.TrimEnd('\')
Assert-NoReleaseReparsePoint $install -Tree
$manifest=Join-Path $install 'SHA256SUMS.txt'
if($ExpectedManifestHash -notmatch '^[0-9A-Fa-f]{64}$' -or (Get-FileHash -LiteralPath $manifest -Algorithm SHA256).Hash -ine $ExpectedManifestHash){throw 'Installed manifest differs from the accepted candidate'}
if(-not $AllowUnsignedDevelopment -and $ExpectedSignerThumbprint -notmatch '^[0-9A-Fa-f]{40}$'){throw 'Supply the publisher thumbprint or explicitly use unsigned rehearsal mode'}
& (Join-Path $PSScriptRoot 'Verify-Layout.ps1') -Stage $install -Version $ExpectedVersion -VerifyManifest -Installed -ExpectedSignerThumbprint $ExpectedSignerThumbprint
$source=Get-Content -LiteralPath (Join-Path $install 'SOURCE-CANDIDATE.json') -Raw | ConvertFrom-Json
$evidence=[IO.Path]::GetFullPath($EvidenceDirectory)
Assert-NoReleaseReparsePoint $evidence
if(Test-Path -LiteralPath $evidence){throw 'EvidenceDirectory must be new; previous observations are retained'}
New-Item -ItemType Directory -Path $evidence | Out-Null
$started=[DateTime]::UtcNow; $until=$started.AddHours($DurationHours)
$baseline=@{}; $last=@{}; $ids=@{}; $peak=@{}; $samples=0; $issues=New-Object 'Collections.Generic.List[string]'
$result=[ordered]@{schemaVersion=1;version=$ExpectedVersion;commit=$source.commit;sourceTreeSha256=$source.treeSha256;manifestSha256=$ExpectedManifestHash.ToLowerInvariant();startedAtUtc=$started.ToString('o');requestedHours=$DurationHours;completed=$false;observationStatus='incomplete';workloadAcceptance='pending-manual-evidence';signatureMode=$(if($AllowUnsignedDevelopment){'rehearsal'}else{'publisher-verified'});failure=$null}
try{
  do{
    $at=[DateTime]::UtcNow
    $observed=@(Get-CimInstance Win32_Process | Where-Object {
      $_.Name -in @('Lunitide.exe','lunitide-engine.exe') -and $_.ExecutablePath -and
      [IO.Path]::GetFullPath($_.ExecutablePath).StartsWith($install+'\',[StringComparison]::OrdinalIgnoreCase)
    })
    $rows=@()
    foreach($name in @('Lunitide.exe','lunitide-engine.exe')){
      $matching=@($observed | Where-Object Name -eq $name)
      if($matching.Count -lt 1){$issues.Add("$($at.ToString('o')) $name is absent");continue}
      $processIDs=(@($matching | ForEach-Object {[int]$_.ProcessId} | Sort-Object) -join ',')
      $row=[ordered]@{name=$name;pid=$processIDs;processCount=$matching.Count;workingSetBytes=[long]($matching | Measure-Object WorkingSetSize -Sum).Sum;privateBytes=[long]($matching | Measure-Object PrivatePageCount -Sum).Sum;handles=[int]($matching | Measure-Object HandleCount -Sum).Sum;threads=[int]($matching | Measure-Object ThreadCount -Sum).Sum}
      if(-not $baseline.ContainsKey($name)){$baseline[$name]=$row;$ids[$name]=$row.pid;$peak[$name]=$row.privateBytes}
      if($ids[$name] -ne $row.pid){$issues.Add("$($at.ToString('o')) $name process identity changed")}
      $peak[$name]=[Math]::Max($peak[$name],$row.privateBytes);$last[$name]=$row;$rows += $row
    }
    [IO.File]::AppendAllText((Join-Path $evidence 'samples.jsonl'),(([ordered]@{atUtc=$at.ToString('o');processes=$rows} | ConvertTo-Json -Depth 5 -Compress)+"`n"),(New-Object Text.UTF8Encoding $false))
    $samples++
    if($at -ge $until){break}
    Start-Sleep -Seconds $SampleSeconds
  }while($true)
  $result.completed=$true
  $growth=@()
  foreach($name in $baseline.Keys){
    $memoryDelta=$last[$name].privateBytes-$baseline[$name].privateBytes
    $handleDelta=$last[$name].handles-$baseline[$name].handles
    $review=$memoryDelta -gt ($MemoryGrowthLimitMB*1MB) -or $handleDelta -gt $HandleGrowthLimit
    if($review){$issues.Add("$name resource growth exceeded the configured observation threshold; review samples and workload")}
    $growth += [ordered]@{name=$name;privateBytesGrowth=$memoryDelta;handlesGrowth=$handleDelta;peakPrivateBytes=$peak[$name];reviewRequired=$review}
  }
  $result.resourceGrowth=$growth
  $result.observationStatus=if($issues.Count){'review-required'}else{'observed-stable'}
}catch{$result.failure=[string]$_.Exception.Message;$result.observationStatus='failed'}
finally{
  $result.finishedAtUtc=[DateTime]::UtcNow.ToString('o');$result.observedHours=([DateTime]::UtcNow-$started).TotalHours;$result.sampleCount=$samples;$result.issues=@($issues.ToArray())
  [IO.File]::WriteAllText((Join-Path $evidence 'result.json'),($result | ConvertTo-Json -Depth 8),(New-Object Text.UTF8Encoding $false))
}
Write-Host "Observation evidence: $evidence ($($result.observationStatus)); workload and machine acceptance remain separate"
if(-not $result.completed -or $issues.Count){exit 1}
