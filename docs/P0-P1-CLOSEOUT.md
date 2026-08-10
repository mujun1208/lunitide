# P0/P1 closeout record

Status: **conditional closeout; not formal completion**  
Baseline: `1b3fd1d` (`harden-final-release-acceptance`)

## What is established

- The complete local code-gate command set recorded in `docs/implementation/P0-P1-status.md` passed on baseline `1b3fd1d`: Go test/vet/build, generated Bridge verification, Renderer typecheck/tests/build, authentic Electron migration adoption, native layout/installer checks, and a real WebView2 runtime smoke.
- The locally produced installer is an **unsigned, non-publishable release candidate only**. It is not a production release and is not evidence of publisher identity or trusted timestamping.
- No final release tag is asserted by this record.

## External acceptance still required

| Blocker | Evidence required |
|---|---|
| Authenticode and timestamp | Sign and Windows-policy verify every app-owned PE and the installer with the approved publisher identity and trusted timestamp. |
| Disposable Windows lifecycle | On clean, disposable Windows 10 and Windows 11 x64 clients, verify install, launch, upgrade with data retention, reinstall, uninstall, and `/PURGE`; cover supported Evergreen WebView2 Runtime **present and absent**, including the approved absent-runtime acquisition/failure path and cleanup of processes/staging/backup residue. |
| Remote Windows CGO race | Run the complete Windows CGO `go test -race` workflow against the final candidate commit, including authentic Electron `safeStorage` adoption, and retain CI evidence. |
| True external migration acceptance | On disposable external Windows profiles with authentic legacy data, exercise supported schema variants, valid and damaged credentials/data, interrupted/repeated migration, retention/deletion choice, and byte-preservation/fail-closed behavior. Repository fixtures and same-machine tests do not substitute for this acceptance. |

## Required final sequence

1. Close all four external blockers on one immutable candidate commit.
2. Only after external migration acceptance, delete the isolated legacy Electron/Python executable sources; retain only sanitized, non-executable migration fixtures needed for regression coverage.
3. Run the **entire** local and remote gate set again on the post-deletion final tree, including signed package and clean-machine lifecycle verification.
4. Create the final release tag only after all evidence binds to that exact commit and artifact digests.

Until that sequence succeeds, P0/P1 may be described as having passed local code gates on `1b3fd1d`, but **must not be described as formally complete or released**. The detailed evidence and completion rule remain in `docs/implementation/P0-P1-status.md`.
