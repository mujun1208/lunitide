# P2 Session design — create/list slice

Status: **frozen and authorized for Session create/list only**.

## Exact semantics

Session has an engine-generated immutable canonical Crockford ULID and a required immutable `projectId` referring to an existing Project. Its explicit `title` is normalized exactly like Project `name`: reject raw input over 200 Unicode code points, collapse Unicode whitespace runs to one ASCII space, then require 1–200 Unicode code points. Duplicate titles are allowed. Create accepts no lifecycle fields and always produces `status: active`, `createdAt == updatedAt`, and `version: 1`.

At most 100 Sessions may exist per Project. The existence check, per-Project capacity check, Session insert, `session.created` audit insert, and 24-hour `session.create` idempotency insert/replay are one `BEGIN IMMEDIATE` transaction. Same key/same request replays; same key/different request returns `IDEMPOTENCY_CONFLICT`.

`session.list` requires exactly `projectId` and returns that Project's Sessions in `created_at ASC, id ASC` order. Storage uses a foreign key from `sessions.project_id` to `projects.id` with `ON DELETE RESTRICT` and an index supporting the list order.

Stable public errors are `BRIDGE_SCHEMA_INVALID`, `IDEMPOTENCY_KEY_REQUIRED`, `IDEMPOTENCY_CONFLICT`, `PROJECT_NOT_FOUND`, `SESSION_CAPACITY_REACHED`, and `STORAGE_UNAVAILABLE`.

## Bridge contracts

- `session.create`: `{ "projectId": ULID, "title": string }` -> private Session DTO.
- `session.list`: `{ "projectId": ULID }` -> `{ "items": SessionDTO[] }` (maximum 100).
- Session DTO fields are exactly `id`, `projectId`, `title`, `status`, `createdAt`, `updatedAt`, `version`.
- IDs, status, timestamps, and version are engine-owned; unknown or forged fields are rejected.

Project and Session data share one local-user authorization domain in this single-user desktop phase. The trusted top-frame Renderer may list every Project and may use any returned `projectId` with the Session methods; `projectId` is a data-scope selector, not a security capability. A compromised trusted Renderer is therefore inside the local-data trust boundary. Project-scoped capabilities, collaboration roles, and multi-user authorization are explicitly deferred and must be designed before those trust assumptions change.

## Explicit exclusions

This authorization does **not** include Message, Stage/StageRun, model profile, chat behavior, Session update/archive/delete, pagination, or search. It does not alter Project lifecycle semantics.

## Acceptance

Domain, application, SQLite migration/checksum/exact schema/columns/FK/index/startup invariants/UoW/list ordering, engine handler/wiring, schema generation, strict TypeScript response guards (including response `projectId` matching the request), and Project-to-Session/back UI navigation are required. Generated files are regenerated from schemas rather than edited.
