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
#>
[CmdletBinding()]
param(
    # Minimum acceptable total statement coverage, in percent. Baseline at
    # introduction was 49.9%; the floor sits just below it.
    [double]$Floor = 49.0,
    [string]$Timeout = '25m'
)

$ErrorActionPreference = 'Stop'

# go on Windows mis-parses a relative -coverprofile argument ending in .out as
# a package path ("no required module provides package .out"), so hand it an
# absolute path.
$profilePath = Join-Path (Get-Location) 'coverage.out'
if (Test-Path $profilePath) { Remove-Item $profilePath }

go test -timeout $Timeout "-coverprofile=$profilePath" ./...
if ($LASTEXITCODE -ne 0) { throw "go test failed (exit $LASTEXITCODE)" }
if (-not (Test-Path $profilePath)) { throw "coverage profile was not written to $profilePath" }

$totalLine = (go tool cover "-func=$profilePath" | Select-Object -Last 1)
if ($totalLine -notmatch '([0-9]+(?:\.[0-9]+)?)%') {
    throw "could not parse a coverage total from: $totalLine"
}
$total = [double]$Matches[1]

Write-Host ("Total statement coverage: {0}% (floor {1}%)" -f $total, $Floor)
if ($total -lt $Floor) {
    throw ("Coverage {0}% is below the floor of {1}%. Add tests, or — if this is a deliberate baseline change — lower -Floor in the workflow on purpose." -f $total, $Floor)
}
Write-Host "Coverage floor satisfied."
