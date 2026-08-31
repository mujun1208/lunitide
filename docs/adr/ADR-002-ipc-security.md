# ADR-002: Host/Engine process and IPC boundary

- Status: Accepted
- Date: 2026-08-09
- Updated: 2026-08-31

The Host and Engine are separate Go processes connected by a Windows Named Pipe. The production pipe name is stable per user (`\\.\pipe\lunitide-gateway-<user>`) so a second client can reconnect; `--pipe` remains required on the engine and may still be overridden for development. The Pipe DACL grants only the current user. Non-loopback and `0.0.0.0` binds stay forbidden.

The engine checks the peer process ID after `Accept` and in the handshake. The first host PID is the tray/owner. After a successful handshake the same persisted bootstrap secret may open another session from that owner (stay-alive) or from another current-user process (auto-pair: pipe DACL already excludes other users). Handshake ACK failure still poisons the secret and shuts down the engine. A second desktop instance still prefers activating the existing window; if it attaches, it reconnects to the stable pipe with the persisted nonce and does not spawn a second engine.

`sameUserPairedPID` is intentionally any positive PID pair. Isolation is the per-user pipe DACL plus the persisted handshake nonce, not a child-PID allowlist. A same-user process that can read `gateway-session.nonce` can connect; that is the documented residual. Tightening to tray/desktop children only would break `--rpc-health` and extra workbench clients. Public bind / `0.0.0.0` stay forbidden.

The nonce is delivered through an anonymous inherited stdin pipe on first launch and persisted under the data root as `gateway-session.nonce`. It does **not** isolate the nonce from an attacker who can read another same-user process's memory. Secret leases run in-process on the engine. RPC uses a 4-byte length prefix and bounded frames. No privileged Bridge method is exposed by this mechanism. Public bind / `0.0.0.0` stay forbidden.
