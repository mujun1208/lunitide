# ADR-004: Host secrets and Engine leases

- Status: Accepted
- Date: 2026-08-21

## Boundary

Only the Windows Host owns `SecretService`. Credentials are DPAPI CurrentUser assets under the protected LocalAppData/Lunitide root. The asset name is a digest; DPAPI optional entropy binds credential reference, provider ID, normalized origin, and protocol. Plaintext exists only transiently in the single Host-owned `provider.credential.submit` Bridge request and the DPAPI `Put` buffer; it is never forwarded to Engine, logged, persisted in SQLite, returned publicly, or retained in stable Renderer state. Ciphertext is likewise excluded from SQLite, logs, Bridge responses, Provider APIs, and Renderer state. The submit method returns only an opaque, short-lived submission token and coordinator-bound provider ID.

The Engine may obtain a credential only over a fresh per-launch Named Pipe broker. The broker address is random and transferred with the bootstrap seed through the inherited anonymous handle, not command-line arguments. Broker HMAC keys are domain-separated from main RPC authentication. Both ends verify the kernel-reported peer PID.

Lease requests bind provider, credential reference, normalized origin, protocol, operation, deadline, and a 256-bit nonce. Frames are limited to 64 KiB, TTL to five seconds, and nonces are atomically consumed before decryption, so replay, expiry, wrong binding, and concurrent duplicate consumption fail. `Operation` is authenticated, auditable request metadata; it is not an enforceable in-Engine capability boundary once plaintext has been delivered. Host cancellation closes the listener; Engine process exit closes its client and the Host tears down the child.

The main JSON RPC handshake retains its string `sessionNonce` for compatibility. It is a one-time, in-memory credential with reservation/consumption semantics; erasure of all immutable JSON/string copies cannot be guaranteed and is therefore explicitly best-effort. This does not change the broker's binary authenticated lease protocol.

## Plaintext lifetime

Plaintext exists only inside `WithSecret`, a broker response buffer, and the connector callback stack. Mutable buffers are zeroed on return. Code must not convert credentials to strings, serialize them, log them, place them in errors, databases, crash metadata, metrics, or Bridge DTOs. Go and Windows may retain unavoidable allocator/kernel copies; this is best-effort erasure, not locked-memory protection.

## Threat model and non-goals

The design blocks accidental persistence/disclosure, Renderer compromise crossing the allow-list, pipe squatting, unauthenticated peers, stale/replayed leases, and file redirection through reparse points or hard links. Protected owner/DACL and atomic replacement reduce local file attacks.

It does **not** defend against malware already executing as the same user that can inspect Host/Engine memory, inject code, steal inherited handles, call DPAPI as that user, or race filesystem operations with equivalent authority. DPAPI CurrentUser does not provide machine-wide administrator isolation. The filesystem checks are defense in depth and do not claim elimination of every path-reopen race.

Legacy Chromium `v10`/`v11` safeStorage migration additionally unwraps the Electron AES key with CurrentUser DPAPI and decrypts one AES-GCM value in the Host. Raw key and plaintext buffers are wiped, but Go's `crypto/aes` does not expose destruction of its expanded key schedule; same-user memory inspection remains outside the stated protection boundary.
