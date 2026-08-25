# P0/P1 acceptance status

Updated: 2026-08-10

## Production architecture

The only production path is:

```text
React Renderer → frame-aware WebView2 Bridge → Go Desktop Host
→ authenticated Windows Named Pipe → Go Core Engine → SQLite / DPAPI
```

The Electron/Python `0.2.1` prototype has been deleted. Its one job — carrying legacy `safeStorage` credentials into DPAPI — finished on 2026-08-11, and the Python engine it fronted had already been superseded by the Go engine over a named pipe. Native release layout verification rejects Electron, Node Runtime, Python, FastAPI, PyInstaller, and their runtime payloads.

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
| React Provider UI and generated Bridge contracts | Complete | schema drift gate, TypeScript check, 32 Renderer tests, production Vite build | Closed |
| Electron metadata and authentic Chromium `safeStorage` migration | Removed in 0.4.01 | The prototype was never released; the one machine that ran it recorded `credential=adopted` on 2026-08-11, so the import had no remaining reader. Prototype, migration code and npm toolchain deleted; migration 0088 drops the bookkeeping tables | Closed |
| Native release layout, pinned WebView2Loader, PE/export/manifest/content checks, exact binary version binding, NSIS lifecycle scripts | Complete | local positive/negative stage and installer verification plus CI workflow | Closed internally |
| WebView2 profile isolation and failure-safe installer replacement | Complete | secured `%LOCALAPPDATA%\\Lunitide\\WebView2` profile; sibling stage/backup/restore installer flow | Closed internally |
| Windows CGO race detector | Workflow complete | `.github/workflows/quality.yml` and `.github/workflows/release-candidate.yml`; no remote run is available in this repository yet | External evidence required |
| Install/upgrade/retain/reinstall/`/PURGE` on disposable Win10 and Win11 | Script complete | `.github/workflows/release-candidate.yml`; current local profile is intentionally rejected because it contains an installation; client matrix must cover Evergreen WebView2 present and absent | External evidence required |
| Authenticode publisher signing and timestamp | Build policy complete and fail-closed | Production build signs/verifies all three app-owned PE files before the manifest, then signs/verifies the installer; no certificate exists on this machine | External credential required |
| Committed source baseline | Complete | P0/P1 `0.3.0` baseline changeset; release tag intentionally withheld | Closed |
| Matching `v0.3.0` release tag | Not established | Must point at the accepted signed/clean-machine baseline | Required before release |

## Final local hardening evidence (2026-08-10)

The latest local hardening closes additional release-safety gaps:

- tagged/manual release packaging now depends on the complete quality and Windows CGO race jobs in the same workflow; pull requests cannot package or upload unsigned release candidates;
- legacy Electron ProviderStore initialization never rewrites, renames, or partially normalizes a migration source on load; unsupported versions, invalid entries, and duplicate provider IDs fail closed without changing source bytes;
- NSIS activation commits only after shortcut and uninstall registration writes succeed and the installed version is read back; failed upgrades restore the prior release and distinguish incomplete metadata restoration with a dedicated exit code;
- disposable lifecycle acceptance rejects leftover `Lunitide.backup.*` and `Lunitide.installing.*` directories after upgrade;
- two native stage builds produced identical payload manifests, and the final staged WebView2 runtime loaded the trusted document, closed through `WM_CLOSE`, exited with code 0, and left no tracked child process.
- authenticated IPC now clears the handshake deadline without imposing an idle-session timeout, while bounding each response/event write and client request write independently;
- Host-to-Engine writes and handshake I/O honor context cancellation, poison partially written connections, and reject failed post-handshake deadline resets;
- synchronous stream events are bounded and buffered until the initial response is committed, preserving response-before-event ordering without deadlocking handlers;
- client shutdown immediately interrupts stalled event delivery, and session shutdown bounds draining of non-cooperative request handlers.
- Desktop and Engine expose side-effect-free `--version`; staged and installed layout acceptance requires exact ordinal equality with `VERSION`.
- production signing now signs and verifies each app-owned PE before manifest generation and signs/verifies the NSIS container last; every signature requires the pinned publisher, trusted timestamp, and Windows policy validation.
- release assets use positive extension allowlists and reject PE, ZIP, and CAB magic even when payloads are renamed; negative acceptance proves disguised PE and wrong-version rejection.
- both remote CGO race jobs generate a real Electron `safeStorage` corpus and run the integration-tagged credential adoption test under `-race`.

The complete local Go, legacy migration TypeScript, Bridge, Renderer (32 tests), authentic Electron safeStorage adoption, native layout, NSIS compilation, and real WebView2 runtime command set passes on this tree. The latest explicitly non-publishable unsigned development installer is:

```text
release/out/Lunitide-Setup-0.3.0-x64.exe
SHA-256 f7bd26f95bf06bf9700c5cf15805e3d91b0e167ff8a67b4c4cff6edcd5967a6c
```

This evidence does not replace the external Win10/Win11 lifecycle, final-commit remote race, Authenticode/timestamp, or release-tag requirements below.

`WebView2Loader.dll` is not the Evergreen WebView2 Runtime. The Host detects a missing runtime and fails closed; clean-client acceptance must explicitly exercise both a supported preinstalled Runtime and a Runtime-absent machine. The product must choose and validate an approved Runtime acquisition policy before calling the Runtime-absent case releasable.

## Verification commands

```powershell
go test ./...
go vet ./...
go build ./...
npm --prefix web run verify:bridge
npm --prefix web run typecheck
npm --prefix web test -- --run
npm --prefix web run build

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

The installer staging flow preserves or restores the prior release for ordinary extraction, file-lock, disk, and activation failures. As with the local-storage boundary in ADR-003/ADR-004, it does not claim isolation from malicious code already running as the same user and racing user-writable filesystem paths with equivalent authority.
