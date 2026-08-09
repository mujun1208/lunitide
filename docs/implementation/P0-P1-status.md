# P0/P1 acceptance status

Updated: 2026-08-09

## Production architecture

The only production path is:

```text
React Renderer → frame-aware WebView2 Bridge → Go Desktop Host
→ authenticated Windows Named Pipe → Go Core Engine → SQLite / DPAPI
```

Electron/Python `0.2.1` is retained only as a regression baseline and a historical migration input. Native release layout verification rejects Electron, Node Runtime, Python, FastAPI, PyInstaller, and their runtime payloads.

## Acceptance matrix

| Gate | Implementation | Evidence | Release status |
|---|---|---|---|
| Go Host/Engine split, authenticated IPC, PID verification, inherited one-use bootstrap | Complete | Go unit/integration tests and local runtime smoke | Closed |
| Fixed Known Folder data root, owner/DACL, reparse/hardlink/file-identity checks | Complete | Go negative and migration tests | Closed |
| SQLite migrations, checksums, schema fingerprint, WAL, transactions, CAS, idempotency, audit/outbox | Complete | Go unit/integration/concurrency tests | Closed |
| Frame-aware WebView2 origin policy and navigation/popup/download/permission denial | Complete | Go policy tests and local real-runtime smoke | Closed |
| WebView2 privileged-surface hardening | Complete | DevTools, context menus, accelerator keys, dialogs, status UI, host objects, password saving and autofill fail closed before navigation | Closed |
| Provider/model CRUD, model sync and diagnostics | Complete | Engine/SQLite/Renderer tests | Closed |
| Host-only DPAPI secrets, lease broker and crash-safe credential submission/adoption/cleanup | Complete | recovery, replay, wrong-binding, concurrency and integration tests | Closed |
| OpenAI-compatible and Anthropic gateway, pinned DNS/SSRF/TLS policy, streaming/cancel/terminal arbitration | Complete | gateway/network/stream tests, including exactly-once aggregate usage | Closed |
| React Provider UI and generated Bridge contracts | Complete | schema drift gate, TypeScript check, 30 Renderer tests, production Vite build | Closed |
| Electron metadata and authentic Chromium `safeStorage` migration | Complete | fail-fast `npm run test:electron-adoption:e2e` | Closed |
| Native release layout, pinned WebView2Loader, PE/export/manifest checks, NSIS lifecycle scripts | Complete | local stage/installer verification and CI workflow | Closed internally |
| Windows CGO race detector | Workflow complete | `.github/workflows/quality.yml`; no remote run is available in this repository yet | External evidence required |
| Install/upgrade/retain/reinstall/`/PURGE` on disposable Win10 and Win11 | Script complete | `.github/workflows/release-candidate.yml`; current local profile is intentionally rejected because it contains an installation | External evidence required |
| Authenticode publisher signing and timestamp | Build policy complete and fail-closed | Production installer build requires `LUNITIDE_SIGN_COMMAND`; no certificate exists on this machine | External credential required |
| Committed source baseline | Complete | P0/P1 `0.3.0` baseline changeset; release tag intentionally withheld | Closed |
| Matching `v0.3.0` release tag | Not established | Must point at the accepted signed/clean-machine baseline | Required before release |

## Verification commands

```powershell
go test ./...
go vet ./...
go build ./...
npm run verify:bridge
npm --prefix web run typecheck
npm --prefix web test -- --run
npm --prefix web run build
npm run test:electron-adoption:e2e

# Staging does not publish an installer and may run unsigned.
./release/Build-Release.ps1 -SkipInstaller

# Explicitly non-publishable local candidate.
./release/Build-Release.ps1 -AllowUnsignedDevelopment

# Production build: fails unless signing and verification succeed.
./release/Build-Release.ps1
```

## Completion rule

P0/P1 code and local production gates are internally closed only after the complete command set passes on the final tree. Public release is **not complete** until all four external evidence rows above are closed: Windows race, Win10/Win11 disposable lifecycle, valid Authenticode signing/timestamp, and a committed/tagged baseline. No unsigned installer may be called a production release.

Legacy Electron/Python migration sources must remain isolated and non-production until the clean-machine migration matrix is accepted. They may then be removed, retaining only sanitized, non-executable migration fixtures needed for regression coverage.
