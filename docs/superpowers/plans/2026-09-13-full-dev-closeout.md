# Full Development Close-out Program

> **Superseded as the scoreboard.** Current 100% close-out (includes FormalDecision leftovers and tonight’s dirty-tree facts) is `docs/superpowers/plans/2026-09-13-all-dev-100-closeout.md`. This file remains the older sequencer.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Execute **one wave at a time**. Do not start Wave C until Wave B factory code tasks are green, unless the user explicitly parallelizes T13 after T01. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Close every in-scope development gap so factory F1–F5 and model-office E-layer each reach their own 100%, without mixing those definitions or promising Q/D from code.

**Architecture:** Two tracks, two completion contracts. Factory remaining work is a short TDD close-out on the current dirty `feat/prd-v7-s1-continuity` tree. Model-office is the remapped feilunshengj v2 graph (`0157`/`0158`/`0159`). Do not open a new worktree. Do not merge to `main`. Do not rewrite released `0154`–`0156`.

**Tech Stack:** Go engine, SQLite, React 19 / Vitest, `generate-bridge`, Windows desktop installer.

**Spec:**
- Factory: `docs/design/PRD-project-factory-2026-09-13.md` + spine `docs/design/PRD-project-workbench-spine-2026-09-13.md`
- Factory remaining tasks: `docs/superpowers/plans/2026-09-13-factory-100-closeout.md`
- Model-office rebase: `docs/superpowers/specs/2026-09-13-model-office-upgrade-rebase-design.md`
- Model-office E tasks: `docs/superpowers/plans/2026-09-13-model-office-e-wave.md`
- Pack (read only, gitignored): `docs/design/feilunshengj/PRD.md`, `CONTRACTS.md`, `IMPLEMENTATION-PLAN.md`

## Global Constraints

- Stay on this dirty tree. Do not revert uncommitted factory leftover + T01 freeze.
- Do not commit unless the user asks. If a task says Commit, stage only the files that task touched and wait if the user has not asked.
- After any Bridge schema edit: `npm --prefix web run generate:bridge`. Never hand-edit `web/src/generated/bridge.ts` or `internal/bridge/schema_generated.go`.
- Windows `go test` / npm need unrestricted permissions. PowerShell: no `&&`; use `;`.
- Do not commit `docs/design/feilunshengj/`, secrets, `web/dist`, `.release-cache`.
- Do not change poison / 月伴 TTS / 玉盘像素 / 星尘配方.
- Do not weld Hub into SessionPage. Do not use M6 as the interface platform. No auto git. SQLite only.
- Factory 100% ≠ model-office 100%. Do not tell the user “产品 100%” until the matching contract below is green.

---

## Two 100% definitions (do not mix)

### Factory 100% (Track A)

All of these are true:

1. F1–F5 §9 acceptance from `PRD-project-factory-2026-09-13.md` is met in code.
2. §10.1 automation is green (including personal chat: no generate / no project-rules injection).
3. One §1.4 first-ship walk reproduces on a **temp disk** with stubbed executors (this program’s Wave B e2e test).
4. A human live walk on a real workbench (`%LocalAppData%\Lunitide` or a test install) checks the same 12 steps. The Go e2e does **not** replace that walk.

### Model-office E 100% (Track B, engineering layer only)

T01–T20 engineering gates in the remapped pack are green:

| Logical migration | Pack name (stale) | Real file |
| --- | --- | --- |
| Model profile + protocol ledger | `0155_model_native_v2.sql` | `0157_model_native_v2.sql` |
| Execution budget / task outcome | `0156_execution_contract_v2.sql` | `0158_execution_contract_v2.sql` |
| Office delivery / FormalDecision | `0157_office_delivery_v2.sql` | `0159_office_delivery_v2.sql` |

Fixture pass ≠ live qualified. Code cannot mint “高端商用”.

### Not in either 100%

K8s / customer production deploy, live Cursor/Codex as factory success, MySQL/Postgres, device farm, designer market, remote Git, auto CI, grok-build, ACP, nested TUI, auto git, Hub-in-SessionPage, M6 as interface platform, rewriting 0154–0156, committing the gitignored pack.

### Q and D (required for “所有功能正常” in the office sense, not code-mintable)

- **Q:** user-authorized DeepSeek/GLM live batches after E. Separate budget. `layers.Q` in `docs/audits/model-office-upgrade/baseline.json` stays `not_run` until those batches exist.
- **D:** human designer review of the 12 core template combinations. Tests must not write a designer certificate.

---

## Honest calendar

| Wave | What | Size | Done when |
| --- | --- | --- | --- |
| A | Stabilize current dirty tree (factory leftover + T01 freeze). Re-verify. Commit only if asked. | hours | targeted Go + vitest + `generate-bridge --check` green |
| B | Factory remaining close-out | 2–4 days | `2026-09-13-factory-100-closeout.md` tasks 1–6 green + human walk recorded |
| C | Model-office E | weeks (pack ~145 person-days; T01 mostly done) | T20 engineering gates; `layers.E=done` |
| D | Q live batches | depends on keys | user-authorized batch evidence |
| E | D designer review | human | certificates, not tests |

Do not promise Wave C–E in one session.

---

## Current tree (Wave A facts)

Pushed HEAD at freeze: `c93dd3f3` / `0.4.81` (`Release 0.4.81: land the project factory workbench and close review gaps.`).

Uncommitted (keep; do not revert): F1 generate/interview UI, F3 `emptyBoardAck`, F5 whole-tree inventory, `writeChecklistTx` writer gate, `deliverable.draft` filter, dirty-board gates, integration fail-by-source, WorkBoardPanel empty auto-sync, shell badges, T01 `baseline.json` + `scripts/verify-model-office-upgrade.mjs` + `internal/modelquality/*` + reserved 0157–0159 names.

T01 freeze is **partially landed, not fully accepted**: 24 unique case IDs are in testdata; verify script exists; `0157` SQL must still not exist.

---

## Wave sequence

- [ ] **Wave A — Stabilize.** Re-run the Wave A commands below. If anything red, fix only the regression. Do not start new features. Commit only if the user asks.

```powershell
go test ./internal/projectgen ./internal/projectrules ./internal/projectschema ./internal/projectboard ./internal/projecttestkit ./internal/projectsync ./internal/modelquality -count=1
go test ./internal/app -count=1
go test ./internal/storage/sqlite -count=1
npm --prefix web run generate:bridge -- --check
npx --prefix web vitest run src/project/WorkBoardPanel.test.tsx src/project/DeliverablePanel.test.tsx src/project/PhaseGenerateBar.test.tsx src/project/ProjectWorkbenchShell.test.tsx
npx --prefix web tsc --noEmit
node scripts/verify-model-office-upgrade.mjs
```

- [ ] **Wave B — Factory 100% remaining.** Execute `docs/superpowers/plans/2026-09-13-factory-100-closeout.md` task-by-task. Then record the human walk in `docs/audits/factory-first-ship.md`.

- [ ] **Wave C — Model-office E.** Execute `docs/superpowers/plans/2026-09-13-model-office-e-wave.md`. Graph: `T01 → T02 → T03 → T04 → T05` then `T06–T10`; `T01 → T13 → T14–T18`; T19/T20 last. T13 may run in parallel with T02 after T01 is accepted.

- [ ] **Wave D — Q.** Only after the user provides keys and a budget. Do not fake `live qualified`.

- [ ] **Wave E — D.** Human designers. Code keeps `unsupported` / `needs_review` when evidence is missing.

---

## Why this shape

Three approaches were considered:

1. **One “just finish it” session** — rejects itself. Factory remaining is days; E-layer is weeks; Q/D are not code.
2. **One combined 145-day plan file** — unreadable; factory and office do not share a 100% gate.
3. **This program + two TDD plans** (chosen) — each track has bite-sized tasks and a hard done-when. Executors read the slice plan, not this page, for file-level work.

---

## Self-review

- Factory §10.1 leftover items (personal chat, first-ship e2e, `boardDirty`, `db.query` no-target) each have a Wave B task.
- Pack T02–T20 are not silently dropped; they live in the E-wave plan with remapped SQL.
- Out-of-scope list matches factory PRD §13 and the rebase spec.
- No task in this program writes product code; it only sequences the other two plans.
