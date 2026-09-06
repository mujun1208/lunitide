#requires -Version 5.1
<#
.SYNOPSIS
    Runs reproducible engineering gates against one stable source tree.
.DESCRIPTION
    Groups may run concurrently into the same evidence directory. Each group
    refuses to overwrite its receipt, records real exit codes, and rejects
    source changes during verification. Documentation/evidence is excluded
    from the code fingerprint; release candidates additionally bind docs.
    Uses only isolated tests. Does not install, sign, or operate user data.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('go','race','web','static','security','release-tools')][string]$Group,
    [Parameter(Mandatory)][string]$EvidenceDirectory,
    [ValidateRange(51,100)][double]$CoverageFloor = 51,
    [ValidateRange(1,16)][int]$ParallelPackages = 4
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$PSDefaultParameterValues = @{'Out-File:Encoding'='utf8'}
. (Join-Path $PSScriptRoot 'Release-Safety.ps1')
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$docs = Join-Path $repo 'docs'
$evidence = Assert-ReleaseChildPath $EvidenceDirectory $docs
if (-not (Test-Path -LiteralPath $evidence)) { New-Item -ItemType Directory -Path $evidence | Out-Null }
$receiptPath = Join-Path $evidence ($Group + '-receipt.json')
if (Test-Path -LiteralPath $receiptPath) { throw 'Use a new evidence directory; existing receipts are immutable' }
$utf8 = New-Object Text.UTF8Encoding($false)
$receipt = [ordered]@{
    group = $Group; startedAt = [datetime]::UtcNow.ToString('o'); finishedAt = $null
    status = 'running'; source = $null; sourceUnchanged = $false; checks = @(); error = $null
    scope = 'code-and-automated-tests; docs excluded; no physical or release acceptance'
}
function Save-Receipt {
    [IO.File]::WriteAllText($receiptPath, ($receipt | ConvertTo-Json -Depth 8), $utf8)
}
function Invoke-Gate([string]$Name, [string]$Command, [string[]]$Arguments, [string]$OutputFile = '') {
    if (-not $OutputFile) { $OutputFile = $Name + '.log' }
    $started = [datetime]::UtcNow
    $resolved = Get-Command -Name $Command -CommandType Application -ErrorAction Stop
    $global:LASTEXITCODE = -1
    $previousPreference = $ErrorActionPreference
    try {
        # Windows PowerShell 5.1 models native stderr as ErrorRecords, even
        # for successful tools. Exit status, not stderr presence, is the gate.
        $ErrorActionPreference = 'Continue'
        & $resolved.Source @Arguments 1> (Join-Path $evidence $OutputFile) 2> (Join-Path $evidence ($Name + '.stderr.log'))
        $code = $LASTEXITCODE
    } finally { $ErrorActionPreference = $previousPreference }
    $receipt.checks += [ordered]@{
        name = $Name; command = $Command; arguments = $Arguments; exitCode = $code
        startedAt = $started.ToString('o'); finishedAt = [datetime]::UtcNow.ToString('o')
        stdout = $OutputFile; stderr = $Name + '.stderr.log'
    }
    Save-Receipt
    if ($code -ne 0) { throw "$Name failed (exit $code); inspect its recorded output" }
    Write-Host "$Name passed"
}
$oldCgo = $env:CGO_ENABLED
Push-Location $repo
try {
    $before = Get-ReleaseSourceSnapshot $repo $docs
    $sourceFile = $Group + '-source.json'
    [IO.File]::WriteAllText((Join-Path $evidence $sourceFile), ($before | ConvertTo-Json -Depth 6), $utf8)
    $receipt.source = [ordered]@{commit=$before.commit; treeSha256=$before.treeSha256; fileCount=$before.fileCount; manifest=$sourceFile}
    Save-Receipt
    switch ($Group) {
        'go' {
            $env:CGO_ENABLED = '0'
            $profile = Join-Path $evidence 'coverage.out'
            Invoke-Gate 'go-tests' 'go' @('test','-json','-count=1',"-p=$ParallelPackages",'-timeout=25m',"-coverprofile=$profile",'./...') 'go-tests.jsonl'
            Invoke-Gate 'coverage' 'go' @('tool','cover',"-func=$profile") 'coverage.txt'
            $total = Get-Content -LiteralPath (Join-Path $evidence 'coverage.txt') | Select-Object -Last 1
            if ($total -notmatch '([0-9]+(?:\.[0-9]+)?)%') { throw 'Coverage total is missing' }
            $percent = [double]::Parse($Matches[1], [cultureinfo]::InvariantCulture)
            $receipt['coveragePercent'] = $percent
            $receipt['coverageFloor'] = $CoverageFloor
            if ($percent -lt $CoverageFloor) { throw "Coverage $percent is below $CoverageFloor" }
        }
        'race' {
            $env:CGO_ENABLED = '1'
            Invoke-Gate 'go-race' 'go' @('test','-race','-json','-count=1',"-p=$ParallelPackages",'-timeout=90m','./...') 'go-race.jsonl'
        }
        'web' {
            Invoke-Gate 'bridge' 'npm.cmd' @('--prefix','web','run','verify:bridge')
            Invoke-Gate 'typecheck' 'npm.cmd' @('--prefix','web','run','typecheck')
            Push-Location (Join-Path $repo 'web')
            try {
                Invoke-Gate 'web-tests' 'npx.cmd' @('--no-install','vitest','run','--reporter=json',('--outputFile=' + (Join-Path $evidence 'web-tests.json')))
                Invoke-Gate 'web-build' 'npx.cmd' @('--no-install','vite','build')
            } finally { Pop-Location }
        }
        'static' {
            $env:CGO_ENABLED = '0'
            Invoke-Gate 'go-vet' 'go' @('vet','./...')
            Invoke-Gate 'go-build' 'go' @('build','./...')
            Invoke-Gate 'go-lint' 'golangci-lint.exe' @('run','./...')
        }
        'security' {
            $env:CGO_ENABLED = '0'
            Invoke-Gate 'govulncheck' 'govulncheck.exe' @('-json','./...') 'govulncheck.jsonl'
            Invoke-Gate 'npm-audit' 'npm.cmd' @('--prefix','web','audit','--json','--audit-level=moderate') 'npm-audit.json'
        }
        'release-tools' {
            Invoke-Gate 'release-tools-ps5' 'powershell.exe' @('-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',(Join-Path $PSScriptRoot 'Test-ReleaseTools.ps1'))
            Invoke-Gate 'release-tools-ps7' 'pwsh.exe' @('-NoProfile','-NonInteractive','-File',(Join-Path $PSScriptRoot 'Test-ReleaseTools.ps1'))
            Invoke-Gate 'release-exclusions' 'powershell.exe' @('-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',(Join-Path $PSScriptRoot 'Test-OmniExcluded.ps1'))
        }
    }
    Assert-ReleaseSourceUnchanged $before (Get-ReleaseSourceSnapshot $repo $docs)
    $receipt.sourceUnchanged = $true
    $receipt.status = 'passed'
} catch {
    $receipt.status = 'failed'
    $receipt.error = $_.Exception.Message
    throw
} finally {
    $receipt.finishedAt = [datetime]::UtcNow.ToString('o')
    Save-Receipt
    $env:CGO_ENABLED = $oldCgo
    Pop-Location
}
