# P2 domain design — Project create/list slice

Status: **frozen and implemented for the Project slice only**. This document authorizes Project create/list. Session create/list is separately frozen and authorized by [P2-SESSION-DESIGN.md](P2-SESSION-DESIGN.md); all other Session behavior remains excluded.

The product baseline is *Lunitide 月汐：全链路统一产品与系统设计 v1.0*: Project is the top-level business container and P2 later expands into Session, Message, Stage, Artifact, snapshots, and backup. This slice deliberately establishes only the smallest durable container.

## Requirements and boundary

| ID | Requirement |
|---|---|
| `P2-PROJ-001` | A local user can create a named Project and immediately see it in a deterministic Project list. |
| `P2-PROJ-002` | Re-delivery of one create attempt produces one Project; conflicting reuse of its idempotency key fails explicitly. |
| `P2-PROJ-003` | Project creation, its idempotency record, and sanitized audit event commit atomically in SQLite. |
| `P2-PROJ-004` | Renderer access uses only generated `project.create` and `project.list` Bridge methods and gains no SQL, filesystem, shell, URL, or generic invoke authority. |
| `P2-PROJ-005` | Existing Provider, Secret, Gateway, Chat, migration, IPC, and release code gates remain green. |

**In scope:** Project domain; migration/repository; atomic idempotent create; status-filtered list; generated Bridge schemas/types; Project list/create UI; validation, migration, concurrency, contract, and Renderer tests.

**Out of scope:** project rename/update/archive/restore/delete, name uniqueness, pagination, workspace/root path, import/export, search, settings, Session/Message/Stage/Artifact, snapshots, backup/restore, collaboration, and filesystem scanning.

## Frozen Project model

| Field | Rule |
|---|---|
| `id` | Engine-generated immutable canonical Crockford ULID. |
| `name` | Collapse Unicode whitespace runs to one ASCII space; resulting display name is 1–200 Unicode code points. Raw input over 200 code points is rejected before normalization. |
| `status` | Domain/storage reserve `active | archived`; `project.create` accepts no status and always creates `active`. No archive transition is exposed in this slice. |
| `createdAt`, `updatedAt` | Engine-generated UTC timestamps; equal on create. |
| `version` | Starts at 1 and is reserved for later optimistic concurrency. |

Projects are ordered by `created_at ASC, id ASC`. The optional list status filter accepts only `active | archived`. Physical and soft deletion semantics are not defined or exposed. Duplicate display names are currently allowed; uniqueness policy is a later product decision.

## Bridge contracts

Both methods use the existing strict versioned request/response envelope, ULID request/trace IDs, bounded deadline, unknown-field rejection, trusted top-frame ownership checks, and sanitized errors.

### `project.create`

Write operation. `idempotencyKey` is required and must be 1–128 printable ASCII bytes (`U+0021`–`U+007E`).

```json
// payload
{ "name": "My project" }

// success payload
{
  "id": "01J...",
  "name": "My project",
  "status": "active",
  "createdAt": "2026-08-10T12:00:00Z",
  "updatedAt": "2026-08-10T12:00:00Z",
  "version": 1
}
```

IDs, status, timestamps, and version are Engine-owned; forged fields are rejected.

### `project.list`

Read operation; no idempotency key required. The first slice contains at most 100 total Projects in stable `createdAt ASC, id ASC` order. P2 deliberately has no pagination because creation is atomically capped at 100 Projects.

```json
// payload: all projects
{}

// optional status filter
{ "status": "active" }

// success payload
{ "items": [] }
```

There is no pagination or workspace scope in this slice. The response is an explicit private presentation DTO, not a database/domain row.

## Idempotency, audit, and storage

`project.create` atomically writes the Project, a 24-hour idempotency record, and one `project.created` audit event under `BEGIN IMMEDIATE`. Same key plus same canonical request digest replays the original result. Same key plus another digest returns `IDEMPOTENCY_CONFLICT`. Concurrent identical deliveries create exactly one row and one audit event.

Migration `0007_project.sql` adds the constrained Project table and transactionally rebuilds shared idempotency/audit tables to extend their operation enums while preserving existing rows and indexes. Migration bytes are pinned by SHA-256 and startup validates the exact schema fingerprint and data invariants.

Audit metadata contains only the aggregate ID, actor, action, timestamp, and version metadata needed by this slice. It does not contain secrets, credentials, filesystem paths, raw IPC envelopes, SQL errors, or internal causes.

## Renderer security and reliability

- Renderer calls only generated typed methods; no generic object bridge is added.
- Project names render as React text, never HTML.
- The UI prevents busy re-entry and retains the exact immutable mutation attempt only for an explicit retry after a retryable uncertain outcome.
- Non-retryable failures discard the retained attempt.
- A refresh failure preserves already displayed Projects.
- Missing or typed-nil Project service wiring returns sanitized `STORAGE_UNAVAILABLE` instead of panicking.

## Stable errors used by this slice

| Code | Meaning | Retryable |
|---|---|---|
| `BRIDGE_SCHEMA_INVALID` | Invalid/missing/unknown payload field, name, status filter, envelope, or idempotency-key syntax. | No |
| `IDEMPOTENCY_KEY_REQUIRED` | Create omitted a valid idempotency key. | No |
| `PROJECT_CAPACITY_REACHED` | The stable 100-Project quota has been reached. | No |
| `IDEMPOTENCY_CONFLICT` | Key was already used for a different request. | No |
| `STORAGE_UNAVAILABLE` | Storage or Project service is unavailable; internal cause is not exposed. | Yes |

## Verification and acceptance

The slice requires:

- domain tests for canonical ULID, name normalization/limits, status, timestamps, and version;
- migration preservation, immutable checksum, exact schema fingerprint, explicit Crockford alphabet, and startup invariant tests;
- sequential and concurrent replay/conflict tests proving one Project and one audit event;
- strict Bridge tests for null, unknown/forged fields, oversized names, DTO shape, allowlist ownership, and generated-code drift;
- Renderer tests for loading/empty/create, normalization, inert XSS-like names, busy protection, retained retry attempt, stale refresh, and Projects/Providers navigation;
- `go test ./...`, `go vet ./...`, `go build ./...`, Bridge verification, Renderer typecheck/tests/production build;
- P0/P1 release gates remain subject to `docs/P0-P1-CLOSEOUT.md`; this P2 slice does not satisfy external signing, clean-machine, race, or migration acceptance blockers.

## Unresolved later-domain decisions — implementation blocked

| Domain | Decisions still required |
|---|---|
| Project lifecycle/workspace | Rename and name uniqueness; archive/restore/delete state machine; root capability and workspace authorization; pagination/search; import/export. |
| Session | Beyond separately authorized create/list: generated titles, lifecycle/update/archive/delete, Stage association, model-profile pinning, pagination/search. |
| Message | Durable parts, roles/status, sequencing, stream checkpoint/replay, cancellation recovery, usage/attachments, retention. |
| Stage | Exact nine-stage identifiers, transitions, StageRun attempts, parallelism, blocked/stale/cancelled recovery, exit criteria. |
| Artifact | Type registry, storage, versioning, approval immutability, trace links, stale propagation, deletion/export. |
| Snapshot | Boundary/hash, locking, deduplication, retention, impact analysis, restore/replay. |
| Backup/restore | Consistency point, encryption/key recovery, WAL handling, scheduling, retention, integrity, RPO/RTO, rollback UX. |

No migration, API, or UI for these deferred areas may be introduced until its design and acceptance criteria are frozen separately.
