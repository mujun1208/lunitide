# P2 Durable Chat design — assistant durable write slice

Status: **frozen and authorized for assistant durable write only**.

## Authority and scope

This slice follows ADR-005 §1 (immutable archival history) and the development handoff §9.1 (Durable Chat). The frozen P2 Message slice (`docs/P2-MESSAGE-DESIGN.md`) explicitly excluded "assistant streaming durable write"; this document lifts that exclusion for the narrow case of a completed assistant response.

The Renderer remains untrusted: it calls `chat.start` with a `sessionId` and receives stream events. The Engine, not the Renderer, decides prompt input via `contextapp.Assemble` and persists the assistant response. The Renderer never provides assistant text for storage.

**In scope:** `chat.start` with `sessionId` binds to a durable Session; assistant final text is persisted as an immutable `role=assistant` Message; provider-reported usage is persisted to the token ledger; stream terminal states (`completed`, `failed`, `cancelled`) have defined storage semantics.

**Out of scope:** tool calls/results, multi-part assistant messages, assistant message edit/delete, streaming-status persistence (a `streaming` row is not written mid-stream), mid-turn compaction, automatic handoff offer, Renderer-side assistant text submission.

## Frozen model and limits

An assistant Message has canonical uppercase ULID `id`, immutable `sessionId`, `role="assistant"`, `status="completed"`, Engine/SQLite allocated `sequence` (same monotonic per-Session space as user Messages), text, and UTC `createdAt`.

Assistant text is 1..16,384 Unicode code points and at most 65,536 UTF-8 bytes. Input CRLF and bare CR are normalized to LF; no trimming or other normalization. This is wider than the user-message limit (2,048 / 8,192) because model outputs are typically longer; the application layer enforces the role-specific bound while the storage CHECK is widened to the assistant ceiling.

If the assistant response exceeds 16,384 code points, the stream fails with `ASSISTANT_RESPONSE_TOO_LARGE` and no Message is persisted. Splitting a single response into multiple Messages is explicitly deferred.

Quota accounting (Project 64 MiB, Workspace 256 MiB) covers assistant text identically to user text. The same `BEGIN IMMEDIATE` transaction reserves sequence, checks quota, inserts the Message and its single text part, and writes the audit event.

## Migration

Migration `0019_durable_chat.sql` widens CHECK constraints and extends operation/action enums:

- `messages.role`: `CHECK (role IN ('user', 'assistant'))`
- `messages.status`: `CHECK (status IN ('completed', 'failed'))` — `streaming` is not persisted
- `message_parts.text`: `CHECK (length(text) BETWEEN 1 AND 16384 AND length(CAST(text AS BLOB)) <= 65536)` — widened to the assistant ceiling; user-text validation remains enforced by the domain layer at 2,048/8,192
- `idempotency_records.operation`: add `'message.append-assistant'`
- `audit_events.action`: add `'message.assistant.appended'`

Existing rows are preserved. The migration is idempotent and byte-pinned by SHA-256.

## Stream lifecycle and durable write

```text
chat.start(sessionId, providerId, modelId)
→ Engine validates session, provider, model
→ Engine assembles prompt from durable history (contextapp.Assemble)
→ Engine creates streamId, registers stream state
→ Engine calls gateway.Stream with assembled messages
→ deltas are emitted to Renderer via events (not persisted)
→ on stream success:
    → Engine collects full assistant text from deltas
    → Engine calls messageapp.AppendAssistant(streamId, sessionID, text, usage)
    → AppendAssistant atomically: idempotency check → session verify → sequence allocate → quota check → insert message(role=assistant, status=completed) + text part → audit → idempotency record → token ledger entry
    → Engine emits terminal `completed` event (with assistant messageId)
→ on stream failure:
    → Engine emits terminal `failed` event
    → no Message is persisted
→ on stream cancellation:
    → Engine emits terminal `cancelled` event
    → no Message is persisted
```

### Idempotency

The `streamId` serves as the idempotency key for `message.append-assistant`. If the stream completes but the durable write fails (e.g., quota exceeded), a retry with the same `streamId` replays the original result. A different request with the same `streamId` returns `IDEMPOTENCY_CONFLICT`.

### Token ledger

When the gateway reports `Usage.TotalTokens > 0`, the Engine persists a `token.LedgerEntry` with:
- `provider`: the provider protocol string
- `model`: the model ID
- `tokenizerRevision`: `"unknown"` (no tokenizer integration yet)
- `tokenCount`: `Usage.TotalTokens`
- `estimationMethod`: `"provider-reported"`
- `utf8Bytes`: byte length of the assistant text

This is a cache, not message truth. A later tokenizer integration may invalidate and recompute.

## Context assembly integration

When `chat.start` provides a `sessionId` and the Engine has a `messageReader`, the Engine calls `contextapp.Assemble` to build the prompt from durable history. The assembled messages include both `user` and `assistant` Messages, so a multi-turn conversation is reconstructed from storage rather than provided by the Renderer.

Explicitly provided `messages` (e.g., system instructions) are prepended to the assembled history. If no `sessionId` is provided, the legacy path (Renderer-provided messages) is used.

## Stable errors

| Code | Meaning | Retryable |
|---|---|---|
| `BRIDGE_SCHEMA_INVALID` | Invalid/missing payload, providerId, modelId, or sessionId | no |
| `SESSION_NOT_FOUND` | Target Session does not exist | no |
| `MODEL_NOT_FOUND` | Model does not belong to the provider | no |
| `PROVIDER_NOT_READY` | Provider credential is not ready | no |
| `STREAM_UNAVAILABLE` | Event channel not available | yes |
| `STREAM_LIMIT_REACHED` | Concurrent stream cap reached | yes |
| `CONTEXT_ASSEMBLY_FAILED` | Durable context assembly failed | yes |
| `ASSISTANT_RESPONSE_TOO_LARGE` | Assistant text exceeds 16,384 code points | no |
| `MESSAGE_STORAGE_QUOTA_REACHED` | Project or workspace quota would be exceeded | no |
| `UPSTREAM_FAILED` | Provider gateway error | yes |

## Explicit exclusions

This slice does **not** include:
- Tool calls or tool results as durable Messages
- Multi-part assistant Messages (only one text part per Message)
- `streaming` status persistence (only terminal `completed` is stored)
- Assistant message edit, delete, or retry
- Mid-turn compaction (compaction before the stream completes)
- Automatic handoff offer near the context threshold
- Renderer-side assistant text submission or editing
- Prompt-template version tracking (ADR-005 §4 requires it for compaction, not for this slice)

## Acceptance

The slice requires:
- domain tests for assistant Message validation (role, status, text limits, sequence, UTC);
- migration preservation, immutable checksum, exact schema fingerprint, and startup invariant tests;
- application tests for `AppendAssistant` covering success, idempotent replay, idempotency conflict, quota exceeded, session-not-found, and text-too-large;
- Engine integration tests for `chat.start` durable path: assembled prompt includes prior assistant Messages, assistant final text is persisted, usage is written to token ledger, cancelled/failed streams do not persist;
- `go test ./...`, `go vet ./...`, `go build ./...`, Bridge verification, Renderer typecheck/tests/build all green;
- P0/P1 release gates remain subject to `docs/P0-P1-CLOSEOUT.md`.
