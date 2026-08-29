#requires -Version 5.1
param(
  [Parameter(Mandatory)][string]$Cache,
  [Parameter(Mandatory)][string]$Stage
)
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
# Retired. 0.4.24 staged llama-omni-runtime.zip from .release-cache (unpacked
# from Comni-Setup-1.0.22-win64.exe, ~538 MB) so Setup carried llama-omni-server.
# Setup must not contain that zip, GGUF, or Comni-Setup. MiniCPM-o remains an
# optional post-install download into the data directory. Cache leftovers,
# prior stages, and LUNITIDE_OMNI_RUNTIME_ZIP are ignored on purpose.
Write-Host 'Publish-OmniRuntime: omni runtime is excluded from Setup (no copy).'
