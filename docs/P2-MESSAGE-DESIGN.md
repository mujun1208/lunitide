# P2 Message append/list frozen design

## Authority and scope

This slice follows the product design §12.1: a `message` belongs to a Session and ordered content is stored in `message_part`. It exposes only user `message.append` and paginated `message.list`; there is no assistant role, streaming, parts API, attachment, usage, edit, delete, compaction, or retention behavior.

## Frozen model and limits

A public Message has canonical uppercase ULID `id`, immutable canonical ULID `sessionId`, `role="user"`, `status="completed"`, Engine/SQLite allocated JSON-safe `int64` sequence (1..9,007,199,254,740,991, contiguous and monotonically increasing per Session), text, and UTC `createdAt`. Text remains 1..2,048 Unicode code points and at most 8,192 UTF-8 bytes. Input CRLF and bare CR are normalized to LF; no trimming or other whitespace/Unicode normalization occurs. Session history has no item-count cap; per-page limits are independent of total history.

To bound cumulative disk use, stored Message text is limited to 64 MiB per Project and 256 MiB across the local workspace. Append checks both UTF-8 byte totals inside the same `BEGIN IMMEDIATE` transaction and rejects an append that would exceed either limit. The quota covers authoritative `message_parts.text`; SQLite/WAL overhead is handled by operational free-space checks in the later backup/storage slice and is not advertised as exact quota usage.

SQLite uses the authoritative split: `messages` stores identity/lifecycle/order and `message_parts` stores exactly one `type="text"`, `ordinal=1` row for each message. Public append/list flatten that one part to `text`.

## Append transaction and idempotency

`message.append` requires a valid 1..128 character idempotency key. Its entire operation executes under `BEGIN IMMEDIATE`: reclaim/check the 24-hour record; strictly replay the authoritative response or reject conflict/corruption; verify the Session; allocate `max(sequence)+1` and fail closed before the JSON-safe ceiling; check project/workspace quotas; insert message and text part, sanitized audit, and idempotency record. Any failure rolls back all effects. The writer lock prevents sequence/quota races; unique `(session_id,sequence)` is defense in depth.

## List and invariants

`message.list` requires `sessionId`; optional `cursor`, `direction` (`forward` default or `backward`), `limit` (1..256, default 64), and `byteBudget` (16,384..245,760, default 131,072). The opaque versioned cursor binds session, direction, snapshot high-water, and boundary with deterministic SHA-256 corruption detection; strict decoding rejects unknown/trailing content and tampering. SQLite uses sequence keyset reads bounded by the snapshot, so later appends never enter an existing traversal. Each page verifies contiguous order and exactly one text part via authoritative `messages LEFT JOIN message_parts`. Both item count and the actual marshaled success envelope must be strictly below the requested budget (and therefore the 256 KiB Host and 4 MiB IPC limits). If one item cannot fit, `MESSAGE_PAGE_BUDGET_TOO_SMALL` prevents an empty looping page.

## Stable errors

| Code | Meaning | Retryable |
|---|---|---|
| `BRIDGE_SCHEMA_INVALID` | Strict DTO, ULID, or text bound invalid | no |
| `IDEMPOTENCY_KEY_REQUIRED` | Missing/invalid mutation key | no |
| `IDEMPOTENCY_CONFLICT` | Same live key used for another request | no |
| `SESSION_NOT_FOUND` | Target Session does not exist | no |
| `MESSAGE_CURSOR_INVALID` | Cursor malformed, corrupted, tampered, or bound to another request | no |
| `MESSAGE_PAGE_BUDGET_TOO_SMALL` | One complete item cannot fit requested envelope budget | no |
| `MESSAGE_STORAGE_QUOTA_REACHED` | Project or workspace Message text quota would be exceeded | no |
| `MESSAGE_DATA_INVARIANT_VIOLATION` | Durable message data violates frozen invariants | no |
| `STORAGE_UNAVAILABLE` | Other storage failure | yes |


## Cursor authentication and transactional state

Pagination cursors use HMAC-SHA-256 with an Engine-process-only, domain-separated 32-byte key derived from the launch bootstrap secret. It is distinct from the broker key, copied into the Message service, and never persisted. Engine restart intentionally invalidates outstanding UI cursors; clients restart pagination.

Session sequence/count/text bytes and Project/workspace text-byte usage are explicit tables maintained by application code in the same `BEGIN IMMEDIATE` append transaction. Conditional updates reserve quota and sequence before insertion, and every later failure rolls them back. Startup fully reconciles counters, contiguous authoritative messages, and their single text parts and fails closed. Initial pagination reads persistent session state rather than scanning message history.
