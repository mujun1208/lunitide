# ADR-001: Go + WebView2 + React production architecture

- Status: Accepted
- Date: 2026-08-09

Lunitide production builds use a Go WebView2 Host, a separate Go Engine, and a React/TypeScript Renderer. Electron and Python remain a read-only functional prototype and migration source until P1 parity is reached; no new production capability may depend on them.

The first implementation slice establishes typed Bridge/RPC contracts, a current-user-only Named Pipe, and SQLite-backed Provider queries before adding the WebView2 window.

## Amendment, 2026-08-25 (0.4.01): the prototype is gone

Parity was reached, and the Electron prototype and its migration path have been
deleted rather than left as a baseline. It was never released, so the only
machine that ever held its data was a development one, and there its credential
had already been adopted into the DPAPI store; the import had no reader left.
Removed with it: the npm toolchain that existed to keep the prototype
installable, and the end-to-end test that drove real Electron to prove the
`safeStorage` decrypt. Migration 0088 drops the bookkeeping tables, while 0005
and 0006 stay embedded because an applied migration is history.

The prohibition in the paragraph above is now unconditional and has no
expiry: no production capability may depend on Electron or Python.
