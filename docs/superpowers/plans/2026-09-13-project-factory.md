# Project Factory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land F1–F5 of `docs/design/PRD-project-factory-2026-09-13.md` on the existing spine (root/tree/executors stay).

**Architecture:** Pure packages (`projectgen`, `projectrules`, `projectschema`, `projectboard`, `projecttestkit`) plus thin bridge handlers. Work boards stay JSON attachments. New project columns only for rules/db status. No M6 weld, no SessionPage rewrite.

**Tech Stack:** Go engine, SQLite, React/Vitest, `generate-bridge`.

**Spec:** `docs/design/PRD-project-factory-2026-09-13.md`

## Global Constraints

- Spine PRD remains in force (root, tree, three executors, personal chat, Hub hard-split).
- Phase 2 keeps 10 deliverables. Dev hard-gates: db ready + interface stage completed.
- SQLite only. Sync = copy to a chosen folder, not cloud deploy.
- Schema change → `npm --prefix web run generate:bridge`. Never hand-edit generated files.
- Migration `0156` LF only + store.go checksum + expected columns.
- Windows `go test` needs unrestricted permissions.
- Do not commit unless the user asks.

## Tasks

- [ ] F1 domain: interview + generate + rules (tests first)
- [ ] F2 domain: schema parse/materialize/verify
- [ ] F3 domain: board sync/stats; complete requires selfTestPass
- [ ] F4 domain: test kinds + dual return + integration ready
- [ ] F5 domain: sync dest validation + receipt
- [ ] Storage: migration 0156 + DTO columns + phase gates
- [ ] Bridge schemas + generate-bridge + handlers
- [ ] Frontend: generate bar, schema editor, board stats, hard gates, release sync
- [ ] Verify: targeted go test + vitest + typecheck + generate-bridge --check
