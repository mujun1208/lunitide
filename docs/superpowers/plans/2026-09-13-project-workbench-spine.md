# Project Workbench Spine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or TDD to implement this plan task-by-task.

**Goal:** Land PRD 1.1 so an ITM project has a disk root at create, grows a tree after phase-1 confirm, and 开发/测试 share one checklist chain with 月汐 / Cursor / Codex executors.

**Architecture:** Keep Hub data out of `sessions`/`messages`. Root + tree + executor live on `projects`. Checklists stay JSON deliverable attachments; new `project.task.*` mutate those docs on the server. Phase-session tools and Hub threads with `projectId` use `rootPath` as cwd.

**Tech Stack:** Go engine, SQLite migrations, bridge schemas + `generate-bridge`, React workbench.

**Spec:** `docs/design/PRD-project-workbench-spine-2026-09-13.md` (v1.1)

## Global Constraints

- Do not weld Hub into SessionPage / `sessions` / `messages`.
- Personal chat create (`\u2063月汐·普通对话` name-only) must still succeed without `rootPath`.
- `rootPath` is optional in JSON schema; required in `ValidateCreateBusinessFields` for business projects.
- Unique root via partial index `WHERE root_path != ''` (empty = historical / chat).
- Create writes only `.lunitide/project.json`. No business `mkdir` until phase-1 confirm.
- Tree materialize never deletes dirs. Never auto git commit/push.
- Never auto `dev_done` from model/CLI.
- Do not hand-edit `web/src/generated/bridge.ts` or `internal/bridge/schema_generated.go`.
- New migration is `0155_project_root_tree.sql` (LF only) + manifest checksum + `expectedSchemaSQL` + `expectedColumns`.
- User-visible error codes: `PROJECT_ROOT_*`, `PROJECT_TREE_*`, `PROJECT_TASK_*`, `PROJECT_EXECUTOR_UNAVAILABLE`, `PROJECT_DEV_INCOMPLETE`, `PROJECT_TEST_*`.

## Files

| Unit | Path | Role |
|---|---|---|
| Domain | `internal/domain/project/project.go` | `RootPath`, `TreeStatus`, `TreeDigest`, `TreeGeneratedAt`, `DefaultExecutor` |
| Bind | `internal/projectroot/` | Normalize, FS probe, lock file |
| Tree | `internal/projecttree/` | Parse `ProjectTreeV1`, default trees, mkdir + receipt |
| Tasks | `internal/projecttask/` | Checklist JSON mutate: open/report/complete/returnFromTest |
| Store | `migrations/0155_*.sql`, `store.go`, `uow.go`, `store_project.go` | Columns + scan/insert/update |
| App | `internal/projectapp`, `internal/app/project_handlers.go`, `project_spine_handlers.go` | Create bind + new methods + advance gates |
| Hub | `internal/agenthub` thread create | `projectId` forces cwd |
| Tools | phase-session workspace root | Default write root = `rootPath` |
| Bridge | `api/bridge/v1/project.*.schema.json`, envelope, generate-bridge enabled list | Contract |
| UI | `ProjectPage`, `ProjectTreeEditor`, `ChecklistPanel`, `ProjectWorkbenchShell`, `checklistTypes` | Create root, executor, task, test return |

## Task T01 — Domain + migration + create bind

**Tests first**

- `internal/domain/project/project_test.go`: business create without root fails; empty root still `Validate()` (history/chat); invalid executor rejected; default executor `lunitide`.
- `internal/projectroot/root_test.go`: missing path / file / other lock / write lock + occupied.
- `internal/storage/sqlite/project_root_test.go`: create with root writes lock + columns; duplicate root rejected; empty root still allowed at storage for chat.
- Existing `project.create` business tests must pass a real `rootPath` (helper). Name-only personal chat fixture unchanged.

**Implement**

1. Domain fields + sentinels.
2. `0155` ALTER + partial unique index.
3. Dump sqlite_schema / table_xinfo and update `expectedSchemaSQL` + `expectedColumns`.
4. `CreateProject` bind lock when `RootPath` set; persist new columns.
5. Handler: business create requires root; map `PROJECT_ROOT_*`; DTO includes new fields.
6. Schema: optional `rootPath` on create/update; ProjectDTO extras.

## Task T02 — `project.root.pick` + create form

- Reuse Host folder dialog (same as `agentHub.dir.pick`); do not write `workspace-root.json`.
- `ProjectPage`: required root on create; show path; copy: 根目录现在选定，子目录在需求架构确认后生成。
- Tests: validateForm without root; pick canceled leaves empty.

## Task T03 — ProjectTreeV1

- Pure Go: default implementation/operations trees; reject `..`, drive, UNC; digest; phaseMap/codeRoot rules.

## Task T04 — Phase-1 advance materialize

- Before `CompleteProjectPhase` when phase==1: parse tree or default; mkdir; write `.lunitide/project-tree.json` + receipt; any fail → `PROJECT_TREE_FAILED`, no status change.
- Tests: temp disk ready; read-only / invalid → still chartered/created as before.

## Task T05 — Rebind + materialize retry + history banner

- `project.root.rebind`, `project.tree.get/put/materialize`.
- History empty root: workbench banner 补选项目根.

## Task T06 — Export deliverable copies into `phaseMap`

- On deliverable approve, copy attachment into `{root}/{phaseMap[phase]}/...` without overwrite (add `-v{n}`).

## Task T07 — Auto-import feature list into 开发

- Entering 开发 with empty checklist imports feature_dev_list (dedupe by id/sourceId). Keep manual import.

## Task T08 — `project.task.open` + checklist button + chips

- Server sets `in_progress`, returns one `brief` (id/title/acceptance/target/root + `testReturn`).
- UI: 进入开发; current task; chips include brief. No auto send.

## Task T09 — Phase-session tool root

- If session is phase session and project has root, `workspace.write`/`edit`/`command.run` default to `rootPath`. Personal chat unchanged.

## Task T10 — Hub `projectId` cwd

- `agentHub.thread.create` optional `projectId`; ignore client `workspaceRoot`; use project root.

## Task T11 — Embed Hub thread in workbench

- Executor cursor/codex opens/reuses thread in workbench 外脑 panel. No `setPage('agentHub')`.

## Task T12 — Frontend regression

- ProjectPage / ChecklistPanel / workbench tests + typecheck.

## Task T13 — Executor picker

- `project.executor.set`; DetectAll disables missing Cursor/Codex; `task.open` records executor.

## Task T14 — report / complete

- `report` writes summary only. `complete` → `dev_done`; matching `test_fail` → `pending`. No auto-done.

## Task T15 — returnFromTest + gates

- Reason required. Same `sourceId`. Brief includes reason. Dev advance needs all `dev_done`. Test advance forbids fail/pending/in_progress.

## Verify

- `go test` for touched packages
- `npm --prefix web run generate:bridge` then typecheck / project vitest
- Fix same-class bugs nearby (scan/insert duplication, leftover frontend-only rollback)

## Done when

S1–S9 have automated evidence as PRD §16. Personal chat create still green.
