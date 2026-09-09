[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$output = Join-Path $PSScriptRoot 'out\e2e-binaries'
New-Item -ItemType Directory -Path $output -Force | Out-Null
Push-Location $repo
try {
    # Host and engine must use the same profile; mixing tags hides saved tasks.
    $engine = Join-Path $output 'lunitide-engine.exe'
    $desktop = Join-Path $output 'lunitide-desktop-e2e.exe'
    & go build -tags lunitide_e2e -o $engine ./cmd/engine
    if ($LASTEXITCODE -ne 0) { throw 'E2E engine build failed' }
    & go build -tags lunitide_e2e -ldflags '-H=windowsgui' -o $desktop ./cmd/desktop
    if ($LASTEXITCODE -ne 0) { throw 'E2E desktop build failed' }
    foreach ($binary in @($engine, $desktop)) {
        $info = & go version -m $binary
        if ($LASTEXITCODE -ne 0 -or -not ($info -match '-tags=lunitide_e2e')) {
            throw "E2E profile tag missing: $binary"
        }
        Get-FileHash -LiteralPath $binary -Algorithm SHA256
    }
} finally {
    Pop-Location
}
