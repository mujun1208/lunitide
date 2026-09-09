# Expert Lifecycle Follow-Up

Scope: the Experts: Not Repaired section of
`2026-09-08-meetings-workflow-status.md`. This is a source and isolated-test
follow-up, not an installed-build or live-model acceptance claim.

## Implemented

- Both manual-form and model-tool creation persist `creation_origin=manual`
  and start disabled. Responses include the saved name, ID and state.
- Internal bootstrap and catalog installation pass explicit origins and retain
  their enabled creation behavior. No existing expert states are rewritten.
- Migration 0148 marks pre-existing rows `legacy`, preserving their names,
  source, catalog references, timestamps and enabled/disabled/archived states.
  Local source alone never proves manual provenance.
- List/detail expose creation origin and ownership. The manual list filter is
  restricted to the service's subject; the UI offers "我创建的" and clears
  conflicting filters after creation, selecting the new expert's name/card.
- Bootstrap recipe, catalog-ID and skill-binding refreshes skip explicitly
  manual profiles, including profiles sharing a factory name.
- `expert.try` validates ownership and the reviewed version, snapshots all six
  sections, and performs a bounded text completion through the configured model
  adapter. It neither enables nor mounts the expert. Empty answers and upstream
  errors produce a retryable failure, with no fabricated successful response.
- Explicit enable uses `expert.toggle`. `expert.delete` requires manual origin,
  ownership, current version and a confirmation bound to expert/version. Project
  and session reference checks run with the tombstone in one writer transaction.
  Retrying the same completed deletion is harmless. Version history and audit
  evidence remain; deleted profiles are hidden and cannot be re-enabled or tried.
- Session mounting rejects disabled manual or deleted experts inside its own
  transaction, closing the mount/delete race.

## Verification

- Frontend: `npm test -- src/expert --maxWorkers=2`, 8 files / 47 tests passed.
  Includes 6 new cases covering creation selection, provenance/ownership filter,
  trial failure/retry, explicit enable, guarded delete/retry, and protected origins.
- `npm run typecheck` and `npm run verify:bridge` passed.
- Full `go test ./internal/m8app ./internal/contract -count=1` passed.
- Full `go test ./internal/app ./internal/storage/sqlite -count=1` passed
  (app: 65.686s; sqlite: 136.884s).
- Focused app/storage tests passed for expert bridge creation, model-tool
  creation, disabled trial success/failure, migration/reopen persistence,
  session references, and existing expert contracts.
- The final foreign-version-pointer rejection in trial preparation passed its
  focused regression tests together with trial, delete and bootstrap guards.
- All databases used for verification are temporary test fixtures. Model
  completion tests use an adapter stub; no real model credentials are used.

## Remaining Boundaries

Subsequent handoff: see `2026-09-08-short-drama-live-check.md` for reconciliation
of Halley's earlier test binary failure and ready-to-run real short-drama inputs.
The fixture is now independent of the attachment test provider/adapter. A fresh
full app suite after that change passed in 58.675s. The post-deploy helper and
profile validation tests passed without connecting to a live engine.

- Trial is a text/persona trial. It does not execute bound skills, MCP, tools,
  browsing, file generation or a full shared-chat turn.
- Existing ambiguous local profiles stay `legacy`; they are not silently
  reclassified as user-owned manual profiles or made eligible for deletion.
- Logical deletion retains the name under the existing unique constraint;
  creating another expert with the same historical name remains disallowed.
- No deployment, application start/restart, live database migration or live
  model call was performed. Installed-build acceptance remains unverified.

## Files

Expert domain and service:

- `internal/domain/m8core/expert.go`
- `internal/m8app/expert.go`
- `internal/m8app/expert_lifecycle.go`
- `internal/m8app/bootstrap.go`
- `internal/m8app/catalog_install.go`
- `internal/m8app/expert_test.go`
- `internal/m8app/expert_lifecycle_test.go`
- `internal/m8app/conversation_experts_test.go`

Persistence:

- `migrations/0148_expert_lifecycle.sql`
- `internal/storage/sqlite/m8_expert.go`
- `internal/storage/sqlite/session_experts.go`
- `internal/storage/sqlite/store.go` (migration manifest and expected schema only)
- `internal/storage/sqlite/expert_lifecycle_migration_test.go`

App and creation workflow:

- `internal/app/expert_lifecycle_handlers.go`
- `internal/app/expert_lifecycle_handlers_test.go`
- `internal/app/m8_expert_handlers.go`
- `internal/app/handlers_registry.go` (two expert routes only)
- `internal/app/chat_skill_tools.go` (expert-create comment and result text only)
- `internal/skillapp/bundled/expert-manager/SKILL.md` (post-create lifecycle text)

Contracts and frontend:

- `api/bridge/v1/expert.create.schema.json`
- `api/bridge/v1/expert.list.schema.json`
- `api/bridge/v1/expert.delete.schema.json`
- `api/bridge/v1/expert.try.schema.json`
- `api/bridge/v1/envelope.schema.json` (two expert methods only)
- `web/scripts/generate-bridge.mjs` (two expert methods only)
- `internal/bridge/schema_generated.go` (generated)
- `internal/contract/schema_generated_test.go` (generated)
- `web/src/generated/bridge.ts` (generated)
- `web/src/bridge/client.ts` (expert trial/delete bindings and mutation metadata)
- `web/src/expert/ExpertCenterPage.tsx`
- `web/src/expert/ExpertLifecycleActions.tsx`
- `web/src/expert/ExpertCenterPage.test.tsx`

The workspace was already dirty. Pre-existing changes were preserved, including
those in files shared with this task. No edits were made to `chat.go`,
`chat_run_stream.go`, provider modules or meeting modules.
