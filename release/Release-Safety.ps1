# Shared read-only path and source checks. Dot-source before release cleanup.
function Assert-NoReleaseReparsePoint([string]$Path, [switch]$Tree) {
  $full=[IO.Path]::GetFullPath($Path)
  $current=$full
  while($current){
    if(Test-Path -LiteralPath $current){
      $item=Get-Item -LiteralPath $current -Force
      if($item.Attributes -band [IO.FileAttributes]::ReparsePoint){throw "Reparse point is not allowed in a release path: $current"}
    }
    $next=Split-Path -Parent $current
    if($next -eq $current){break}; $current=$next
  }
  if($Tree -and (Test-Path -LiteralPath $full -PathType Container)){
    $pending=New-Object 'Collections.Generic.Stack[string]'; $pending.Push($full)
    while($pending.Count){
      foreach($item in Get-ChildItem -LiteralPath $pending.Pop() -Force){
        if($item.Attributes -band [IO.FileAttributes]::ReparsePoint){throw "Reparse point is not allowed inside a release tree: $($item.FullName)"}
        if($item.PSIsContainer){$pending.Push($item.FullName)}
      }
    }
  }
}
function Assert-ReleaseChildPath([string]$Path,[string]$Parent) {
  $full=[IO.Path]::GetFullPath($Path).TrimEnd('\')
  $base=[IO.Path]::GetFullPath($Parent).TrimEnd('\')
  if(-not $full.StartsWith($base+'\',[StringComparison]::OrdinalIgnoreCase)){throw "Release path must be a child of its explicit root: $full"}
  Assert-NoReleaseReparsePoint $full -Tree
  return $full
}
function Get-ReleaseSourceSnapshot([string]$Root,[string]$ExcludedRoot) {
  $rootFull=[IO.Path]::GetFullPath($Root).TrimEnd('\')
  $exclude=if($ExcludedRoot){[IO.Path]::GetFullPath($ExcludedRoot).TrimEnd('\')+'\'}else{''}
  $commit=(& git -C $rootFull rev-parse HEAD | Out-String).Trim()
  if($LASTEXITCODE -or $commit -notmatch '^[a-f0-9]{40,64}$'){throw 'Source commit could not be determined'}
  $raw=(& git -C $rootFull -c core.quotepath=false ls-files --cached --others --exclude-standard -z | Out-String).TrimEnd("`r","`n")
  if($LASTEXITCODE){throw 'Source input list could not be determined'}
  $deletedRaw=(& git -C $rootFull -c core.quotepath=false ls-files --deleted -z | Out-String).TrimEnd("`r","`n")
  if($LASTEXITCODE){throw 'Deleted source inputs could not be determined'}
  $deleted=@{}
  foreach($relative in $deletedRaw.Split([char[]]@([char]0),[StringSplitOptions]::RemoveEmptyEntries)){$deleted[$relative]=$true}
  $files=@()
  # The explicit char[] selects the same Split overload on .NET Framework 4.x
  # and modern .NET; a scalar char otherwise leaves an empty trailing path on 5.1.
  foreach($relative in ($raw.Split([char[]]@([char]0),[StringSplitOptions]::RemoveEmptyEntries) | Sort-Object -Unique)){
    if($relative -match '[\r\n]'){throw 'Source filename contains a line break'}
    $full=Assert-ReleaseChildPath (Join-Path $rootFull $relative) $rootFull
    if($exclude -and $full.StartsWith($exclude,[StringComparison]::OrdinalIgnoreCase)){continue}
    if(-not(Test-Path -LiteralPath $full -PathType Leaf)){
      # ls-files --cached includes intentionally deleted tracked files. The
      # candidate represents the actual file set; restoring one changes its
      # digest. Unexpected disappearance while hashing still fails closed.
      if($deleted.ContainsKey($relative)){continue}
      throw "Source input missing: $relative"
    }
    $files += [ordered]@{path=$relative.Replace('\','/');sha256=(Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash.ToLowerInvariant()}
  }
  if(-not $files.Count){throw 'Source fingerprint has no inputs'}
  $lines=($files | ForEach-Object {$_.sha256+'  '+$_.path}) -join "`n"
  $hasher=[Security.Cryptography.SHA256]::Create()
  try{$digest=([BitConverter]::ToString($hasher.ComputeHash([Text.Encoding]::UTF8.GetBytes($lines)))).Replace('-','').ToLowerInvariant()}finally{$hasher.Dispose()}
  return [ordered]@{commit=$commit;treeSha256=$digest;fileCount=$files.Count;files=$files}
}
function Assert-ReleaseSourceUnchanged($Before,$After) {
  if($Before.commit -cne $After.commit -or $Before.treeSha256 -cne $After.treeSha256){throw 'Source inputs changed during the build; discard this mixed candidate and build again from a stable checkout'}
}
function Assert-ReleaseCandidate($Candidate) {
  if($Candidate.treeSha256 -notmatch '^[a-f0-9]{64}$' -or $Candidate.commit -notmatch '^[a-f0-9]{40,64}$' -or $Candidate.fileCount -ne @($Candidate.files).Count -or $Candidate.fileCount -lt 1 -or $Candidate.fileCount -gt 50000){throw 'Candidate source evidence is malformed'}
  $seen=@{}; $lines=@()
  foreach($file in $Candidate.files){
    if($file.path -match '(^/|\\|(^|/)\.\.(/|$)|[\r\n:]|^$)' -or $file.sha256 -notmatch '^[a-f0-9]{64}$' -or $seen.ContainsKey($file.path)){throw 'Candidate source input is unsafe or duplicated'}
    $seen[$file.path]=$true; $lines += $file.sha256+'  '+$file.path
  }
  $hasher=[Security.Cryptography.SHA256]::Create()
  try{
    $hashBytes=$hasher.ComputeHash([Text.Encoding]::UTF8.GetBytes(($lines -join "`n")))
    $digest=([BitConverter]::ToString($hashBytes)).Replace('-','').ToLowerInvariant()
  }finally{$hasher.Dispose()}
  if($digest -cne $Candidate.treeSha256){throw 'Candidate source digest does not match its input list'}
}
