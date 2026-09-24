#requires -Version 5
# Shared helpers for Windows hosted-runner Go 1.26.6 runtime aborts.
# Sourced by Check-Coverage.ps1 and Check-Race.ps1. Not invoked directly.

function Test-GoRuntimeAbort {
    param([Parameter(Mandatory = $true)][AllowEmptyString()][string]$Text)
    if ([string]::IsNullOrEmpty($Text)) { return $false }
    return $Text -match 'fatal error: unexpected signal' -or
        $Text -match 'fatal error: fault' -or
        $Text -match 'unknown pc 0x' -or
        $Text -match 'tryDeferToSpanScan' -or
        $Text -match 'Exception 0xc0000005' -or
        $Text -match 'signal 0xc0000005' -or
        $Text -match 'fatal error: index out of range' -or
        $Text -match 'fatal error: found pointer to free object' -or
        $Text -match 'fatal error: sync: unlock of unlocked mutex' -or
        $Text -match '\.test\.exe: Access is denied'
}

function Invoke-GoLoggedTest {
    param(
        [Parameter(Mandatory = $true)][string[]]$GoArgs,
        [int]$Attempts = 1,
        [string]$RetryReason = 'Go runtime aborted the test process (Windows 1.26.6). Retrying once; assertion and DATA RACE failures are not retried.'
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
            $logPath = Join-Path ([IO.Path]::GetTempPath()) ('lunitide-go-test-{0}.log' -f [guid]::NewGuid().ToString('N'))
            Write-Host ("go {0} (attempt {1}/{2})" -f ($GoArgs -join ' '), $attempt, $Attempts)
            & go @GoArgs 2>&1 | Tee-Object -FilePath $logPath | ForEach-Object { $_ }
            $code = $LASTEXITCODE
            if ($code -eq 0) { return }
            $text = ''
            if (Test-Path -LiteralPath $logPath) {
                $text = Get-Content -LiteralPath $logPath -Raw
            }
            if ($attempt -lt $Attempts -and (Test-GoRuntimeAbort $text)) {
                Write-Host $RetryReason
                if ($text -match '\.test\.exe: Access is denied') {
                    Start-Sleep -Seconds 3
                }
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

function Assert-GoRuntimeAbortClassifier {
    if (-not (Test-GoRuntimeAbort "fatal error: unexpected signal during runtime execution`nunknown pc 0x1`nException 0xc0000005")) {
        throw 'expected ACCESS_VIOLATION dump to count as a runtime abort'
    }
    if (-not (Test-GoRuntimeAbort "unexpected fault address 0x7ff600000000`nfatal error: fault`n[signal 0xc0000005 code=0x8 addr=0x7ff600000000 pc=0x1]")) {
        throw 'expected Go 1.26 fault dump to count as a runtime abort'
    }
    if (-not (Test-GoRuntimeAbort "fatal error: sync: unlock of unlocked mutex")) {
        throw 'expected unlocked-mutex throw to count as a runtime abort'
    }
    if (-not (Test-GoRuntimeAbort 'fatal error: index out of range in runtime.tryDeferToSpanScan')) {
        throw 'expected Green Tea throw to count as a runtime abort'
    }
    if (-not (Test-GoRuntimeAbort "fatal error: found pointer to free object`nruntime.(*mspan).reportZombies")) {
        throw 'expected sweep zombie throw to count as a runtime abort'
    }
    if (Test-GoRuntimeAbort "--- FAIL: TestFoo (1.00s)`n    foo_test.go:1: index out of range") {
        throw 'an assertion failure must not be retried as a runtime abort'
    }
    if (Test-GoRuntimeAbort "WARNING: DATA RACE`n--- FAIL: TestFoo") {
        throw 'a DATA RACE must not be retried as a runtime abort'
    }
    if (-not (Test-GoRuntimeAbort "fork/exec C:\Users\runner\AppData\Local\Temp\go-build1\b853\stdioworker.test.exe: Access is denied.`nFAIL`tgithub.com/lunitide/lunitide/internal/stdioworker`t0.305s")) {
        throw 'expected Windows test-binary lock to count as a runtime abort'
    }
}
