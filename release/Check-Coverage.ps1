#requires -Version 5
<#
.SYNOPSIS
    Runs the Go test suite with coverage and fails if total statement
    coverage falls below a floor.

.DESCRIPTION
    A ratchet, not a target: the floor is set just under the current measured
    coverage so a change that deletes tests or lands a large untested feature
    trips the gate, while ordinary noise does not. Raise -Floor as coverage
    improves so the ratchet only ever tightens.

    The floor is deliberately below the live number rather than equal to it —
    the denominator (total statements) grows with every feature, so a healthy
    change can dip the ratio a few tenths without removing a single test.

    internal/app is covered in its own process with -parallel 1. Go 1.26.6 on
    Windows hosted runners has aborted that package under coverage with
    ACCESS_VIOLATION at PC=0x1 while encoding/json populated its sync.Map
    encoder cache (HashTrieMap.Load / golang/go#81189; Quality 34894852667).
    Serializing that package's tests, then retrying once on a runtime abort,
    avoids treating a toolchain crash as a product failure. Assertion
    failures are not retried.
#>
[CmdletBinding()]
param(
    # PRD engineering acceptance preserves the audited 51% baseline.
    [ValidateRange(51,100)][double]$Floor = 51.0,
    [string]$Timeout = '25m',
    [switch]$SelfTest
)

$ErrorActionPreference = 'Stop'

function Test-GoRuntimeAbort {
    param([Parameter(Mandatory = $true)][AllowEmptyString()][string]$Text)
    if ([string]::IsNullOrEmpty($Text)) { return $false }
    return $Text -match 'fatal error: unexpected signal' -or
        $Text -match 'unknown pc 0x' -or
        $Text -match 'tryDeferToSpanScan' -or
        $Text -match 'Exception 0xc0000005' -or
        $Text -match 'fatal error: index out of range'
}

function Merge-CoverProfiles {
    param(
        [Parameter(Mandatory = $true)][string[]]$Paths,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    $mode = $null
    $body = New-Object System.Collections.Generic.List[string]
    foreach ($path in $Paths) {
        if (-not (Test-Path -LiteralPath $path)) { continue }
        foreach ($line in Get-Content -LiteralPath $path) {
            if ($line -like 'mode:*') {
                if (-not $mode) { $mode = $line }
                continue
            }
            if ($line.Trim().Length -gt 0) { [void]$body.Add($line) }
        }
    }
    if (-not $mode) {
        throw ("no coverage mode line in: {0}" -f ($Paths -join ', '))
    }
    @(, $mode) + $body | Set-Content -LiteralPath $Destination
}

function Invoke-GoTestCover {
    param(
        [Parameter(Mandatory = $true)][string[]]$GoArgs,
        [int]$Attempts = 1
    )
    $nativePref = $null
    if (Get-Variable -Name PSNativeCommandUseErrorActionPreference -ErrorAction SilentlyContinue) {
        $nativePref = $PSNativeCommandUseErrorActionPreference
        $PSNativeCommandUseErrorActionPreference = $false
    }
    try {
        $attempt = 0
        while ($true) {
            $attempt++
            $logPath = Join-Path ([IO.Path]::GetTempPath()) ('lunitide-cover-{0}.log' -f [guid]::NewGuid().ToString('N'))
            Write-Host ("go {0} (attempt {1}/{2})" -f ($GoArgs -join ' '), $attempt, $Attempts)
            & go @GoArgs 2>&1 | Tee-Object -FilePath $logPath | ForEach-Object { $_ }
            $code = $LASTEXITCODE
            if ($code -eq 0) { return }
            $text = ''
            if (Test-Path -LiteralPath $logPath) {
                $text = Get-Content -LiteralPath $logPath -Raw
            }
            if ($attempt -lt $Attempts -and (Test-GoRuntimeAbort $text)) {
                Write-Host 'Go runtime aborted coverage (Windows 1.26.6 HashTrieMap / Green Tea). Retrying once; assertion failures are not retried.'
                continue
            }
            throw ("go test failed (exit {0})" -f $code)
        }
    } finally {
        if ($null -ne $nativePref) {
            $PSNativeCommandUseErrorActionPreference = $nativePref
        }
    }
}

if ($SelfTest) {
    if (-not (Test-GoRuntimeAbort "fatal error: unexpected signal during runtime execution`nunknown pc 0x1`nException 0xc0000005")) {
        throw 'expected ACCESS_VIOLATION dump to count as a runtime abort'
    }
    if (-not (Test-GoRuntimeAbort 'fatal error: index out of range in runtime.tryDeferToSpanScan')) {
        throw 'expected Green Tea throw to count as a runtime abort'
    }
    if (Test-GoRuntimeAbort "--- FAIL: TestFoo (1.00s)`n    foo_test.go:1: index out of range") {
        throw 'an assertion failure must not be retried as a runtime abort'
    }
    $scratch = Join-Path ([IO.Path]::GetTempPath()) ('lunitide-cover-selftest-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $scratch | Out-Null
    try {
        $first = Join-Path $scratch 'a.out'
        $second = Join-Path $scratch 'b.out'
        $merged = Join-Path $scratch 'm.out'
        Set-Content -LiteralPath $first "mode: set`ngithub.com/lunitide/lunitide/pkg/a.go:1.1,2.2 1"
        Set-Content -LiteralPath $second "mode: set`ngithub.com/lunitide/lunitide/pkg/b.go:1.1,2.2 1"
        Merge-CoverProfiles -Paths @($first, $second) -Destination $merged
        $got = Get-Content -LiteralPath $merged
        if ($got[0] -cne 'mode: set') { throw 'merged profile lost its mode line' }
        if (@($got | Where-Object { $_ -like '*.go:*' }).Count -ne 2) { throw 'merged profile dropped package lines' }
    } finally {
        Remove-Item -LiteralPath $scratch -Recurse -Force -ErrorAction SilentlyContinue
    }
    Write-Host 'Check-Coverage self-test passed.'
    exit 0
}

# go on Windows mis-parses a relative -coverprofile argument ending in .out as
# a package path ("no required module provides package .out"), so hand it an
# absolute path.
$profilePath = Join-Path (Get-Location) 'coverage.out'
$appProfilePath = Join-Path (Get-Location) 'coverage-app.out'
$restProfilePath = Join-Path (Get-Location) 'coverage-rest.out'
foreach ($path in @($profilePath, $appProfilePath, $restProfilePath)) {
    if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path }
}

$appImport = 'github.com/lunitide/lunitide/internal/app'
$listed = @(go list ./...)
if ($LASTEXITCODE -ne 0) { throw "go list failed (exit $LASTEXITCODE)" }

$appListed = @($listed | Where-Object { $_ -eq $appImport })
$restListed = @($listed | Where-Object { $_ -ne $appImport })
$profiles = New-Object System.Collections.Generic.List[string]

if ($appListed.Count -gt 0) {
    # Isolated so a HashTrieMap abort (Quality 34894852667) can retry without
    # re-running meetings (~9m) and the rest of the tree.
    Invoke-GoTestCover -Attempts 2 -GoArgs @(
        'test',
        '-timeout', $Timeout,
        '-parallel', '1',
        "-coverprofile=$appProfilePath",
        './internal/app'
    )
    $profiles.Add($appProfilePath)
}

if ($restListed.Count -gt 0) {
    Invoke-GoTestCover -Attempts 1 -GoArgs (@(
        'test',
        '-timeout', $Timeout,
        "-coverprofile=$restProfilePath"
    ) + $restListed)
    $profiles.Add($restProfilePath)
}

if ($profiles.Count -eq 0) { throw 'go list returned no packages to cover' }
Merge-CoverProfiles -Paths @($profiles) -Destination $profilePath
if (-not (Test-Path -LiteralPath $profilePath)) { throw "coverage profile was not written to $profilePath" }

$totalLine = (go tool cover "-func=$profilePath" | Select-Object -Last 1)
if ($totalLine -notmatch '([0-9]+(?:\.[0-9]+)?)%') {
    throw "could not parse a coverage total from: $totalLine"
}
$total = [double]$Matches[1]

Write-Host ("Total statement coverage: {0}% (floor {1}%)" -f $total, $Floor)
if ($total -lt $Floor) {
    throw ("Coverage {0}% is below the floor of {1}%. Add meaningful coverage for the changed behavior before accepting this candidate." -f $total, $Floor)
}
Write-Host "Coverage floor satisfied."
