# ADR-001: Go + WebView2 + React production architecture

- Status: Accepted
- Date: 2026-08-09

Lunitide production builds use a Go WebView2 Host, a separate Go Engine, and a React/TypeScript Renderer. Electron and Python remain a read-only functional prototype and migration source until P1 parity is reached; no new production capability may depend on them.

The first implementation slice establishes typed Bridge/RPC contracts, a current-user-only Named Pipe, and SQLite-backed Provider queries before adding the WebView2 window.
