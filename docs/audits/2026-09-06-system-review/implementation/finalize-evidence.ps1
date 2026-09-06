$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../../..'))
$evidence = Join-Path $PSScriptRoot 'evidence'
Set-Location -LiteralPath $repoRoot

function Read-GoEvents([string]$name) {
    @(Get-Content -LiteralPath (Join-Path $evidence $name) | ForEach-Object { $_ | ConvertFrom-Json })
}
function Count-TestEvents($events, [string]$action) {
    @($events | Where-Object { $_.Test -and $_.Action -eq $action }).Count
}

$goEvents = Read-GoEvents 'go-test-release-check.jsonl'
$webResult = Get-Content -LiteralPath (Join-Path $evidence 'web-test-verified.json') -Raw | ConvertFrom-Json
$npmResult = Get-Content -LiteralPath (Join-Path $evidence 'npm-audit-final.json') -Raw | ConvertFrom-Json
$coverageLine = Get-Content -LiteralPath (Join-Path $evidence 'coverage-release-summary.txt') | Select-Object -Last 1
if ($coverageLine -notmatch '([0-9]+(?:\.[0-9]+)?)%') { throw 'Missing coverage total' }
$coverage = [double]::Parse($Matches[1], [cultureinfo]::InvariantCulture)
if ($coverage -lt 49) { throw 'CI coverage floor failed' }
if (@($goEvents | Where-Object { $_.Action -in @('fail','build-fail') }).Count -ne 0) { throw 'Go failures remain' }
if (-not $webResult.success) { throw 'Renderer failures remain' }
if ($npmResult.metadata.vulnerabilities.total -ne 0) { throw 'Renderer dependency findings remain' }

$raceSummaries = foreach ($name in @('root-race-final.jsonl','recovery-race-final.jsonl')) {
    $events = Read-GoEvents $name
    [ordered]@{
        file = $name
        passed = Count-TestEvents $events 'pass'
        failed = @($events | Where-Object { $_.Action -eq 'fail' }).Count
        skipped = Count-TestEvents $events 'skip'
        packages = @($events | Where-Object Test | Select-Object -ExpandProperty Package -Unique)
    }
}
$vulnRaw = Get-Content -LiteralPath (Join-Path $evidence 'govulncheck-after-update.jsonl') -Raw
$vulnObjects = ('[' + ($vulnRaw -replace '(?m)^\}\r?\n\{', "},`n{") + ']') | ConvertFrom-Json
$findings = @($vulnObjects | Where-Object finding | ForEach-Object { $_.finding })
$reachable = @($findings | Where-Object { @($_.trace | Where-Object function).Count -gt 0 })
if ($reachable.Count -ne 0) { throw 'Reachable Go dependency findings remain' }

$result = [ordered]@{
    base_commit = '09978597dc026bfa40ab8d690d64f9bcaef476f3'
    branch = 'codex/system-upgrade-20260906'
    date = '2026-09-06'
    scope = '本轮实现与隔离集成，非完整发布验收'
    go = [ordered]@{
        log = 'go-test-release-check.jsonl'
        pass_packages = @($goEvents | Where-Object { -not $_.Test -and $_.Action -eq 'pass' }).Count
        packages_with_test_events = @($goEvents | Where-Object Test | Select-Object -ExpandProperty Package -Unique).Count
        passed_tests_including_subtests = Count-TestEvents $goEvents 'pass'
        passed_top_level_tests = @($goEvents | Where-Object { $_.Test -and $_.Test -notmatch '/' -and $_.Action -eq 'pass' }).Count
        failed = 0
        skipped_tests = @($goEvents | Where-Object { $_.Test -and $_.Action -eq 'skip' } | Select-Object Package,Test)
        coverage_percent = $coverage
        ci_floor_percent = 49
    }
    renderer = [ordered]@{
        log = 'web-test-verified.json'; files = $webResult.testResults.Count
        passed = $webResult.numPassedTests; failed = $webResult.numFailedTests
        skipped = $webResult.numPendingTests; success = $webResult.success
    }
    checks = [ordered]@{
        golangci = 'pass: 0 issues'; go_vet = 'pass'; go_build_cgo0 = 'pass'
        bridge_verify = 'pass'; renderer_typecheck = 'pass'
        renderer_build = 'pass; existing large-chunk warnings remain'
        npm_audit = $npmResult.metadata.vulnerabilities
        govulncheck_reachable = $reachable.Count
        govulncheck_module_only = @($findings | Select-Object osv, fixed_version, trace)
    }
    selected_race = @($raceSummaries)
    race_scope_note = '选择性race，不是全库；上述root日志在末次x/crypto及相关Go模块升级前。末次依赖集已重跑全量普通测试、coverage、lint、vet、CGO=0构建及govulncheck。其余专项race见各progress文件。'
    scores = [ordered]@{
        baseline = 2.733142857142857; current_provisional = 3.5594285714285707
        display_current = 3.6; target = 4.9; release_acceptance = $false
    }
    unverified = @('真实三语音链/音频设备','真实外部模型、MCP、MySQL/PostgreSQL','真实键鼠/UIA与多机同事','完整安装/更新/签名','全库race','备份恢复与故障演练','72h连续运行','14天候选观察','PRD明确未完成的执行/权限/状态闭环')
}
$result | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $evidence 'verification-results.json') -Encoding utf8

$modified = @(git diff --name-only 2>$null)
if ($LASTEXITCODE -ne 0) { throw 'Cannot read modified source paths' }
$newFiles = @(git ls-files --others --exclude-standard)
if ($LASTEXITCODE -ne 0) { throw 'Cannot read new source paths' }
$sourcePaths = @($modified + $newFiles | Where-Object { $_ -and $_ -notmatch '^docs/' } | Sort-Object -Unique)
$sourceFiles = foreach ($path in $sourcePaths) {
    $absolute = Join-Path $repoRoot $path
    if (Test-Path -LiteralPath $absolute -PathType Leaf) {
        $item = Get-Item -LiteralPath $absolute
        [ordered]@{path=$path;bytes=$item.Length;sha256=(Get-FileHash -LiteralPath $absolute -Algorithm SHA256).Hash.ToLowerInvariant()}
    } else {
        [ordered]@{path=$path;deleted=$true}
    }
}
[ordered]@{
    base_commit = $result.base_commit; branch = $result.branch
    captured_at = [datetime]::UtcNow.ToString('o')
    scope = '本轮变更的已跟踪和未跟踪源码；排除docs，非提交或发布产物'
    files = @($sourceFiles)
} | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $evidence 'source-snapshot.json') -Encoding utf8
[pscustomobject]$result.go | Select-Object pass_packages,packages_with_test_events,passed_tests_including_subtests,passed_top_level_tests,failed,coverage_percent | ConvertTo-Json
Write-Output "Source snapshot files: $($sourceFiles.Count)"
