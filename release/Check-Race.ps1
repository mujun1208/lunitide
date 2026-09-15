#requires -Version 5
<#
.SYNOPSIS
    Runs the Go race suite, isolating internal/app so a Go 1.26.6 runtime
    abort can retry without re-running the rest of the tree.

.DESCRIPTION
    Hosted Windows + Go 1.26.6 + CGO race has aborted internal/app with
    runtime.adjustdefers during shrinkstack (Quality 34907144926) and
    "found pointer to free object" during sweep (34907148326). The rest of
    the tree has aborted the same way; Quality 34936008921 failed race on a
    SHA whose coverage job passed because Check-Coverage already retries
    that rest process. Neither dump class contained WARNING: DATA RACE.
    This script serializes internal/app and retries both processes once on
    a runtime abort. Real races and assertion failures still fail the job.
#>
[CmdletBinding()]
param(
    [string]$Timeout = '120m',
    [string]$AppTimeout = '60m',
    [int]$PackageParallel = 4,
    [switch]$SelfTest
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Go-TestHelpers.ps1')

if ($SelfTest) {
    Assert-GoRuntimeAbortClassifier
    Write-Host 'Check-Race self-test passed.'
    exit 0
}

$appImport = 'github.com/lunitide/lunitide/internal/app'
$listed = @(go list ./...)
if ($LASTEXITCODE -ne 0) { throw "go list failed (exit $LASTEXITCODE)" }

$appListed = @($listed | Where-Object { $_ -eq $appImport })
$restListed = @($listed | Where-Object { $_ -ne $appImport })

if ($appListed.Count -gt 0) {
    Invoke-GoLoggedTest -Attempts 2 -GoArgs @(
        'test',
        '-race',
        '-count=1',
        '-timeout', $AppTimeout,
        '-parallel', '1',
        './internal/app'
    )
}

if ($restListed.Count -gt 0) {
    # Same abort retry as coverage (Quality 34933672941 / 34936008921).
    Invoke-GoLoggedTest -Attempts 2 -GoArgs (@(
        'test',
        '-race',
        '-count=1',
        '-timeout', $Timeout,
        '-p', "$PackageParallel"
    ) + $restListed)
}

if ($appListed.Count -eq 0 -and $restListed.Count -eq 0) {
    throw 'go list returned no packages to race-test'
}
Write-Host 'Race suite passed.'
