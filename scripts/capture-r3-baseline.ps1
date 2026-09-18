#Requires -Version 5.1
param(
    [string]$RepositoryRoot = '',
    [string]$BaselineUtc = '',
    [string[]]$LogicalMigrationNames = @('memory_fabric', 'memory_retrieval', 'memory_generations', 'ocr_model_packs', 'media_sessions'),
    [switch]$SelfTest
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Get-RepoGit {
    param([string]$Root)
    return { param([string[]]$GitArgs) & git -C $Root @GitArgs }
}

function ConvertTo-RepoPath([string]$Path) {
    return (($Path -replace '\\', '/').TrimStart('/'))
}

function Get-FileSha256([string]$Path) {
    $hash = Get-FileHash -LiteralPath $Path -Algorithm SHA256
    return $hash.Hash.ToLowerInvariant()
}

function Write-CompactJson([object]$Value, [string]$Path) {
    if ($Value -is [System.Array] -or ($Value -is [System.Collections.IList] -and -not ($Value -is [System.Collections.IDictionary]))) {
        $parts = @()
        foreach ($item in @($Value)) {
            $parts += ($item | ConvertTo-Json -Compress -Depth 12)
        }
        $json = '[' + ($parts -join ',') + ']'
    } else {
        $json = (ConvertTo-Json -InputObject $Value -Compress -Depth 12)
    }
    [System.IO.File]::WriteAllText($Path, $json + "`n", [System.Text.UTF8Encoding]::new($false))
}

function Get-MigrationDirs([string]$Root) {
    return @(
        (Join-Path $Root 'migrations'),
        (Join-Path $Root 'internal/storage/sqlite/migrations')
    )
}

function Get-MigrationFiles([string]$Root) {
    $files = @()
    foreach ($dir in (Get-MigrationDirs $Root)) {
        if (-not (Test-Path -LiteralPath $dir)) { continue }
        Get-ChildItem -LiteralPath $dir -Filter '*.sql' | ForEach-Object { $files += $_ }
    }
    return @($files)
}

function Get-MaxMigrationNumber([string]$Root) {
    $max = 0
    foreach ($file in (Get-MigrationFiles $Root)) {
        if ($file.Name -match '^(\d{4})_') {
            $n = [int]$Matches[1]
            if ($n -gt $max) { $max = $n }
        }
    }
    return $max
}

function Get-ExistingLogicalMigrations([string]$Root) {
    $map = @{}
    foreach ($file in (Get-MigrationFiles $Root)) {
        if ($file.Name -match '^(\d{4})_(.+)\.sql$') {
            $map[$Matches[2]] = [ordered]@{
                number   = [int]$Matches[1]
                fileName = $file.Name
            }
        }
    }
    return $map
}

function Invoke-GitUTF8([string]$Root, [string[]]$GitArgs) {
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = 'git'
    $quoted = @('-C', $Root, '-c', 'core.quotepath=false') + $GitArgs
    $psi.Arguments = ($quoted | ForEach-Object {
        if ($_ -match '[\s"]') { '"' + ($_ -replace '"', '\"') + '"' } else { $_ }
    }) -join ' '
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.StandardOutputEncoding = [Text.UTF8Encoding]::new($false)
    $p = [Diagnostics.Process]::Start($psi)
    $text = $p.StandardOutput.ReadToEnd()
    $p.WaitForExit()
    if ($p.ExitCode -ne 0) {
        throw "git failed ($($p.ExitCode)): $($p.StandardError.ReadToEnd())"
    }
    return $text
}

function New-UntrackedManifest([string]$Root) {
    $raw = Invoke-GitUTF8 $Root @('ls-files', '--others', '--exclude-standard')
    $items = @()
    if ($raw) {
        $paths = @($raw -split "(`r`n|`n|`r)") | Where-Object { $_ -ne '' }
        foreach ($rel in $paths) {
            $posix = ConvertTo-RepoPath $rel
            $full = Join-Path $Root ($posix -replace '/', [IO.Path]::DirectorySeparatorChar)
            if (-not [IO.File]::Exists($full)) { continue }
            $attr = [IO.File]::GetAttributes($full)
            if (($attr -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "reparse point refused: $posix"
            }
            if (($attr -band [IO.FileAttributes]::Directory) -ne 0) { continue }
            $items += [ordered]@{
                path   = $posix
                bytes  = [int64](Get-Item -LiteralPath $full -Force).Length
                sha256 = Get-FileSha256 $full
            }
        }
    }
    return @($items | Sort-Object { $_.path })
}

function Invoke-Capture {
    param([string]$Root, [string]$Utc, [string[]]$LogicalNames)
    $head1 = (& git -C $Root rev-parse HEAD).Trim()
    $trackedDiffDigest = (& git -C $Root diff --binary --no-ext-diff | git hash-object --stdin).Trim()
    if (-not $trackedDiffDigest) { $trackedDiffDigest = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855' }

    $untracked = New-UntrackedManifest $Root
    $maxObserved = Get-MaxMigrationNumber $Root
    $existing = Get-ExistingLogicalMigrations $Root
    $alloc = @()
    $n = $maxObserved
    $prev = ('{0:D4}' -f $maxObserved)
    foreach ($name in $LogicalNames) {
        if ($existing.ContainsKey($name)) {
            $hit = $existing[$name]
            $alloc += [ordered]@{
                logicalName                   = $name
                number                        = $hit.number
                fileName                      = $hit.fileName
                previous                      = $prev
                maxObserved                   = $maxObserved
                head                          = $head1
                trackedDiffDigest             = $trackedDiffDigest
                untrackedManifestRelativePath = "docs/design/jiyishengji/evidence/baseline/$Utc/untracked-manifest.json"
                allocatedAt                   = $Utc
                existing                      = $true
            }
            $prev = ('{0:D4}' -f $hit.number)
            continue
        }
        $n++
        $fileName = ('{0:D4}_{1}.sql' -f $n, $name)
        $alloc += [ordered]@{
            logicalName                   = $name
            number                        = $n
            fileName                      = $fileName
            previous                      = $prev
            maxObserved                   = $maxObserved
            head                          = $head1
            trackedDiffDigest             = $trackedDiffDigest
            untrackedManifestRelativePath = "docs/design/jiyishengji/evidence/baseline/$Utc/untracked-manifest.json"
            allocatedAt                   = $Utc
            existing                      = $false
        }
        $prev = ('{0:D4}' -f $n)
    }

    $dest = Join-Path $Root "docs/design/jiyishengji/evidence/baseline/$Utc"
    if (Test-Path -LiteralPath $dest) { throw "baseline already exists: $dest" }
    New-Item -ItemType Directory -Path $dest | Out-Null

    $untrackedPath = Join-Path $dest 'untracked-manifest.json'
    Write-CompactJson $untracked $untrackedPath
    $untrackedDigest = Get-FileSha256 $untrackedPath

    foreach ($row in $alloc) {
        $row.untrackedManifestDigest = $untrackedDigest
        $row.baselineManifestRelativePath = "docs/design/jiyishengji/evidence/baseline/$Utc/manifest.json"
    }
    $allocPath = Join-Path $Root 'docs/design/jiyishengji/evidence/migration-allocation.json'
    New-Item -ItemType Directory -Path (Split-Path $allocPath) -Force | Out-Null
    if (Test-Path -LiteralPath $allocPath) { throw "allocation already exists: $allocPath" }
    Write-CompactJson @{ items = $alloc; head = $head1; trackedDiffDigest = $trackedDiffDigest; maxObserved = $maxObserved } $allocPath
    $allocDigest = Get-FileSha256 $allocPath

    $head2 = (& git -C $Root rev-parse HEAD).Trim()
    if ($head1 -ne $head2) { throw "HEAD changed during capture" }

    $manifest = [ordered]@{
        head                          = $head1
        trackedDiffDigest             = $trackedDiffDigest
        untrackedManifestRelativePath = "docs/design/jiyishengji/evidence/baseline/$Utc/untracked-manifest.json"
        untrackedManifestDigest       = $untrackedDigest
        allocationSha256              = $allocDigest
        capturedAt                    = $Utc
        os                            = [Environment]::OSVersion.VersionString
        powershell                    = $PSVersionTable.PSVersion.ToString()
        logicalMigrationNames         = @($LogicalNames)
    }
    Write-CompactJson $manifest (Join-Path $dest 'manifest.json')
    Write-Output "captured $Utc head=$head1 maxObserved=$maxObserved"
}

function Invoke-SelfTest {
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("r3-selftest-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        & git -C $tmp init -q
        & git -C $tmp config user.email 'r3@example.invalid'
        & git -C $tmp config user.name 'r3'
        $mig = Join-Path $tmp 'migrations'
        New-Item -ItemType Directory -Path $mig -Force | Out-Null
        Set-Content -LiteralPath (Join-Path $mig '0161_memory_fabric.sql') -Value '-- fixture' -Encoding ascii
        Set-Content -LiteralPath (Join-Path $tmp 'README.md') -Value 'ok' -Encoding utf8
        & git -C $tmp add README.md migrations/0161_memory_fabric.sql
        & git -C $tmp commit -qm 'init'
        $spaceDir = Join-Path $tmp 'docs/with space'
        New-Item -ItemType Directory -Path $spaceDir -Force | Out-Null
        Set-Content -LiteralPath (Join-Path $spaceDir '中文.txt') -Value "hello`r`n" -Encoding utf8
        $utc = '20260101T000000000Z'
        Invoke-Capture -Root $tmp -Utc $utc -LogicalNames @('memory_fabric', 'ocr_model_packs')
        $allocRows = Get-Content -LiteralPath (Join-Path $tmp 'docs/design/jiyishengji/evidence/migration-allocation.json') -Raw -Encoding utf8 | ConvertFrom-Json
        if ($allocRows.items[0].fileName -ne '0161_memory_fabric.sql' -or $allocRows.items[0].existing -ne $true) {
            throw 'existing memory_fabric must be recorded, not reallocated'
        }
        if ($allocRows.items[1].fileName -ne '0162_ocr_model_packs.sql' -or $allocRows.items[1].existing -ne $false) {
            throw 'ocr_model_packs must take the next free number after maxObserved'
        }
        $untrackedPath = Join-Path $tmp "docs/design/jiyishengji/evidence/baseline/$utc/untracked-manifest.json"
        $untrackedRows = @(Get-Content -LiteralPath $untrackedPath -Raw -Encoding utf8 | ConvertFrom-Json)
        $found = $false
        foreach ($row in $untrackedRows) {
            if ($null -ne $row -and $row.PSObject.Properties['path'] -and $row.path -eq 'docs/with space/中文.txt') {
                $found = $true
            }
        }
        if (-not $found) { throw 'missing unicode path in untracked manifest' }
        $again = $false
        try { Invoke-Capture -Root $tmp -Utc $utc -LogicalNames @('memory_fabric') } catch { $again = $true }
        if (-not $again) { throw 'repeat capture must fail' }
        Write-Output 'selftest ok'
    }
    finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

if ($SelfTest) {
    Invoke-SelfTest
    exit 0
}

if (-not $RepositoryRoot) { $RepositoryRoot = (Resolve-Path '.').Path }
if (-not $BaselineUtc) { $BaselineUtc = [DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssfffZ') }
if (@($LogicalMigrationNames).Count -lt 2) {
    $LogicalMigrationNames = @('memory_fabric', 'memory_retrieval', 'memory_generations', 'ocr_model_packs', 'media_sessions')
}
Invoke-Capture -Root $RepositoryRoot -Utc $BaselineUtc -LogicalNames @($LogicalMigrationNames)
