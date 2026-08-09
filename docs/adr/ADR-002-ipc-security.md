# ADR-002: Host/Engine process and IPC boundary

- Status: Accepted, implementation in progress
- Date: 2026-08-09

The Host and Engine are separate Go processes connected by a per-launch, cryptographically random Windows Named Pipe. The Engine has no predictable default pipe name; `--pipe` is required, while the Desktop's explicit override is development-only and its default remains random. The Pipe DACL grants only the current user. Both ends verify the peer process ID through Windows named-pipe APIs: the Host accepts only the Engine PID it spawned, and the Engine checks the expected Host PID immediately after `Accept` (before the session gate) and again in the handshake. RPC uses a 4-byte length prefix and bounded frames.

The one-use nonce is delivered through an anonymous pipe whose read end is deliberately inherited as Engine standard input. Controlled inheritance prevents accidental handle leakage; it does **not** isolate the nonce from an attacker capable of reading another same-user process's memory. The authenticator has explicit unused, reserved, and committed states. Reservation is atomic and irreversible; after a correct nonce is reserved, ACK failure shuts down the Engine listener rather than reopening the nonce or leaving an alive-but-unauthenticatable Engine. No privileged Bridge method is exposed by this mechanism.
