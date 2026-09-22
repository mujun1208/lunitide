# Replay already-committed local commits onto a remote branch through the GitHub
# Git Data API, for the case where github.com:443 is unreachable from this host
# while api.github.com answers normally.
#
# Every object is rebuilt from the exact bytes, tree, parent and author/committer
# timestamps of the local commit, and its SHA is compared with the local one at
# each layer. A mismatch means this would create a history different from the one
# that was tested, so the script stops instead of pushing it.
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File Push-ViaGitDataApi.ps1 -Branch trae/docs/Design
#
# Requires: gh (logged in) for the token, git, network to api.github.com.

[CmdletBinding()]
param(
  [string]$Repo = 'mujun1208/lunitide',
  [Parameter(Mandatory)][string]$Branch,
  [string]$Commit = 'HEAD',
  [switch]$WhatIf
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Fail([string]$Message) { Write-Error $Message; exit 1 }

function Invoke-GitText([string[]]$GitArgs) {
  $text = (& git @GitArgs 2>&1 | Out-String)
  if ($LASTEXITCODE) { Fail ("git {0} failed: {1}" -f ($GitArgs -join ' '), $text.Trim()) }
  return $text.Trim()
}

# Binary-safe read of a git object. A PowerShell pipeline rewrites LF as CRLF,
# which changes the bytes and therefore the SHA — redirect through cmd instead.
function Get-GitObjectBytes([string]$Kind, [string]$Sha) {
  $tmp = [IO.Path]::GetTempFileName()
  try {
    & cmd /c "git cat-file $Kind $Sha > `"$tmp`"" | Out-Null
    if ($LASTEXITCODE) { Fail "git cat-file $Kind $Sha failed" }
    return [IO.File]::ReadAllBytes($tmp)
  } finally { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
}

$token = (& gh auth token 2>&1 | Out-String).Trim()
if ($LASTEXITCODE -or -not $token) { Fail 'gh auth token failed; run: gh auth login' }
$headers = @{
  Authorization          = "Bearer $token"
  Accept                 = 'application/vnd.github+json'
  'X-GitHub-Api-Version' = '2022-11-28'
  'User-Agent'           = 'lunitide-ship'
}
$api = "https://api.github.com/repos/$Repo"

function Invoke-Api([string]$Method, [string]$Path, $Body) {
  $uri = "$api$Path"
  if ($null -eq $Body) { return Invoke-RestMethod -Method $Method -Uri $uri -Headers $headers }
  $json = $Body | ConvertTo-Json -Depth 12 -Compress
  # Send explicit UTF-8 bytes: PowerShell 5.1 would otherwise encode a string
  # body with the ANSI code page and corrupt any non-ASCII commit message.
  $bytes = [Text.UTF8Encoding]::new($false).GetBytes($json)
  return Invoke-RestMethod -Method $Method -Uri $uri -Headers $headers -Body $bytes -ContentType 'application/json; charset=utf-8'
}

# --- resolve local and remote endpoints -------------------------------------

$target = Invoke-GitText @('rev-parse', $Commit)
$remote = (Invoke-Api 'GET' "/git/ref/heads/$Branch" $null).object.sha
Write-Host "repo   : $Repo"
Write-Host "branch : $Branch"
Write-Host "remote : $remote"
Write-Host "local  : $target"

if ($remote -ceq $target) { Write-Host 'Remote already matches the local commit; nothing to replay.'; exit 0 }

$ancestor = & git merge-base --is-ancestor $remote $target
if ($LASTEXITCODE) { Fail "Remote $remote is not an ancestor of $target; this would diverge. Fetch and rebase first." }

$queue = @(Invoke-GitText @('rev-list', '--reverse', "$remote..$target") -split "`r?`n" | Where-Object { $_ })
if (-not $queue.Count) { Fail 'No commits to replay.' }
Write-Host ("replay : {0} commit(s)" -f $queue.Count)

# --- replay one commit ------------------------------------------------------

function Invoke-ReplayCommit([string]$Sha) {
  Write-Host ""
  Write-Host ("=== {0}  {1}" -f $Sha.Substring(0, 8), (Invoke-GitText @('log', '-1', '--format=%s', $Sha)))

  $wantTree = Invoke-GitText @('rev-parse', ($Sha + '^{tree}'))
  $parents = @(Invoke-GitText @('log', '-1', '--format=%P', $Sha) -split '\s+' | Where-Object { $_ })
  if ($parents.Count -ne 1) { Fail "Only single-parent commits are supported; $Sha has $($parents.Count)." }
  $parent = $parents[0]
  $baseTree = Invoke-GitText @('rev-parse', ($parent + '^{tree}'))

  # :<srcmode> <dstmode> <srcsha> <dstsha> <status>\t<path>
  $raw = @(Invoke-GitText @('diff-tree', '-r', '--no-commit-id', '--raw', $Sha) -split "`r?`n" | Where-Object { $_ })
  if (-not $raw.Count) { Fail "$Sha changes no files." }

  $entries = @()
  foreach ($line in $raw) {
    if ($line -notmatch '^:(\d{6}) (\d{6}) ([0-9a-f]+) ([0-9a-f]+) ([A-Z])\d*\t(.+)$') { Fail "Unparsed diff-tree line: $line" }
    $dstMode = $Matches[2]; $dstSha = $Matches[4]; $status = $Matches[5]; $path = $Matches[6]

    if ($status -eq 'D') {
      Write-Host "  delete  $path"
      $entries += [ordered]@{ path = $path; mode = $Matches[1]; type = 'blob'; sha = $null }
      continue
    }
    if ($dstMode -notin @('100644', '100755')) { Fail "Unsupported mode $dstMode for $path (symlink or submodule); replay by hand." }

    $bytes = Get-GitObjectBytes 'blob' $dstSha
    $created = Invoke-Api 'POST' '/git/blobs' @{
      content  = [Convert]::ToBase64String($bytes)
      encoding = 'base64'
    }
    if ($created.sha -cne $dstSha) { Fail "blob SHA mismatch for $path : local $dstSha, remote $($created.sha)" }
    Write-Host ("  blob ok $path  ({0} bytes)" -f $bytes.Length)
    $entries += [ordered]@{ path = $path; mode = $dstMode; type = 'blob'; sha = $dstSha }
  }

  $tree = Invoke-Api 'POST' '/git/trees' @{ base_tree = $baseTree; tree = $entries }
  if ($tree.sha -cne $wantTree) { Fail "tree SHA mismatch: local $wantTree, remote $($tree.sha)" }
  Write-Host "  tree ok $wantTree"

  # Take the message straight out of the commit object's bytes: reading it via
  # `git log --format=%B` through a pipeline turns LF into CRLF, which changes
  # the object and so the commit SHA.
  $rawBytes = Get-GitObjectBytes 'commit' $Sha
  $split = -1
  for ($i = 0; $i -lt $rawBytes.Length - 1; $i++) {
    if ($rawBytes[$i] -eq 10 -and $rawBytes[$i + 1] -eq 10) { $split = $i + 2; break }
  }
  if ($split -lt 0) { Fail "Could not find the header/message boundary in commit $Sha." }
  $message = [Text.UTF8Encoding]::new($false).GetString($rawBytes, $split, $rawBytes.Length - $split)

  $body = [ordered]@{
    message   = $message
    tree      = $wantTree
    parents   = @($parent)
    author    = [ordered]@{
      name  = Invoke-GitText @('log', '-1', '--format=%an', $Sha)
      email = Invoke-GitText @('log', '-1', '--format=%ae', $Sha)
      date  = Invoke-GitText @('log', '-1', '--format=%aI', $Sha)
    }
    committer = [ordered]@{
      name  = Invoke-GitText @('log', '-1', '--format=%cn', $Sha)
      email = Invoke-GitText @('log', '-1', '--format=%ce', $Sha)
      date  = Invoke-GitText @('log', '-1', '--format=%cI', $Sha)
    }
  }

  if ($WhatIf) { Write-Host "  [WhatIf] would create the commit and move the ref"; return $Sha }

  $made = Invoke-Api 'POST' '/git/commits' $body
  if ($made.sha -cne $Sha) { Fail "commit SHA mismatch: local $Sha, remote $($made.sha). Do not move the ref." }
  Write-Host "  commit ok $Sha"

  $moved = Invoke-Api 'PATCH' "/git/refs/heads/$Branch" @{ sha = $Sha; force = $false }
  if ($moved.object.sha -cne $Sha) { Fail "ref did not move to $Sha (now $($moved.object.sha))" }
  Write-Host "  ref -> $Sha"
  return $Sha
}

foreach ($sha in $queue) { Invoke-ReplayCommit $sha | Out-Null }

Write-Host ""
$final = (Invoke-Api 'GET' "/git/ref/heads/$Branch" $null).object.sha
Write-Host "remote $Branch is now $final"
if (-not $WhatIf -and $final -cne $target) { Fail "Final remote SHA $final does not match local $target." }
Write-Host 'Replay complete; remote matches the tested local history.'
