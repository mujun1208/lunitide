param([switch]$Race)
$ErrorActionPreference = 'Stop'
$root = Join-Path ([IO.Path]::GetTempPath()) ('lunitide-electron-corpus-' + [guid]::NewGuid().ToString('N'))
$previousCorpus = $env:LUNITIDE_ELECTRON_CORPUS
$previousCanary = $env:LUNITIDE_ELECTRON_CORPUS_CANARY
try {
  New-Item $root -ItemType Directory -Force | Out-Null
  $env:LUNITIDE_ELECTRON_CORPUS = Join-Path $root 'providers.json'
  $env:LUNITIDE_ELECTRON_CORPUS_CANARY = 'electron-safe-storage-e2e-canary'
  & npm exec electron -- test/electron-safe-storage/generate-corpus.cjs
  if ($LASTEXITCODE) { throw "Electron corpus generation failed: $LASTEXITCODE" }
  foreach ($name in @('providers.json', 'Local State')) {
    if (-not (Test-Path (Join-Path $root $name) -PathType Leaf)) { throw "Electron corpus is missing $name" }
  }
  $raceArg = if ($Race) { @('-race') } else { @() }
  & go test @raceArg -tags=integration ./internal/credentialsubmission -run '^TestElectronSafeStorageAdoptionE2E$' -count=1
  if ($LASTEXITCODE) { throw "Electron credential adoption E2E failed: $LASTEXITCODE" }
} finally {
  $env:LUNITIDE_ELECTRON_CORPUS = $previousCorpus
  $env:LUNITIDE_ELECTRON_CORPUS_CANARY = $previousCanary
  Remove-Item $root -Recurse -Force -ErrorAction SilentlyContinue
}
