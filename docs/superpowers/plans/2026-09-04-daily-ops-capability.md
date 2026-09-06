# Daily Ops P0 Implementation Plan

> **For agentic workers:** Execute inline in this session. TDD per package. Do not commit unless the user asks. Spec is the unique fact source.

**Goal:** Ship P0 of the daily-ops executable PRD: route the tool surface, attach L0, force observe-before-pixel, and keep companion/C9 regressions green — without new kinds, embeddings, or a key pool.

**Architecture:** Pure `classifyTaskRoute` + `applyTaskRoute` wrap the existing `autoToolProfile` / companion deny / `ccToolDefinitions` stack. Computer-control closures stay inside `ccapp` (`rememberHits`, `verifyAfter`, Acc unnamed). Plan verify reuses a Go mirror of `pickCompanionFlashModel` and requires `l0` in the verifier prompt.

**Tech Stack:** Go engine tests (`go test`), existing `gateway.ToolDefinition`, `ccapp.Service`, vitest only if `userAsk.ts` changes.

**Spec:** [docs/superpowers/specs/2026-09-04-daily-ops-executable-prd.md](../specs/2026-09-04-daily-ops-executable-prd.md)

## Global Constraints

- No new daemon; SQLite kernel; do not move `v0.4.62`
- Do not rewrite `AnnotateCapture` / `assignNodeIDs`
- Do not touch poison / companion TTS / `CompanionStage` visuals
- `user.ask` only gains optional `reason`; reuse UAC parking
- P0: rules-only router (no flash classify call)
- `media.play` foreground still requires CC
- SoM ids are `B1`/`E1` strings
- Unchanged pixels: one internal 700ms wait, then fail; observe unchanged stays success

---

## Task 1: `classifyTaskRoute` + `applyTaskRoute`

**Files:** create `internal/app/task_route.go`, `internal/app/task_route_test.go`

**Steps:**

- [x] Write `TestClassifyTaskRoute` covering D-A1–A7 (table-driven).
- [x] Implement `TaskRoute`, `classifyTaskRoute`, `applyTaskRoute`, `routeAllow`.
- [x] `go test ./internal/app` green with route + companion/specialist regressions

`classifyTaskRoute(goal, companion, ccEnabled)` returns `("", nil)` when unmatched (keep today's full surface). `ccEnabled` only adds `computer.act` to R2. `companion` does not change the route. `applyTaskRoute` always keeps `user.ask`; if `kb.search` / `kb.cite` / `graph.expand` are already on `defs`, keep them.

## Task 2: Wire `chat.start`

**Files:** `internal/app/chat.go`

- [x] After `filterCompanionDefaultTools`, call `applyTaskRoute` using `classifyTaskRoute`.
- [x] `computerControlEnabled()` mirrors `ccToolDefinitions` (`Enabled && !EmergencyStopped`).
- [x] Put `taskRoute` on `streamState` for Task 4.

## Task 3: Flash id Go mirror

**Files:** create `internal/app/flash_model.go`, `internal/app/flash_model_test.go`

- [x] Mirror TS: `(?:flash|air|lite|mini|haiku)` and exclude realtime/live. `pickFlashModelID` / `pickJudgeModelID`.

## Task 4: `plan.run` verify uses flash + `l0`

**Files:** `internal/app/chat_plan.go`, `internal/app/chat_plan_test.go`

- [x] Collect per-step `l0` JSON from tool summaries (`attachPlanStepL0`).
- [x] If every mutating step `passed`, skip verifier (`verified=true`). If no `l0` at all, `verified=false`. Else `Complete` with flash model id when present (D-C1).
- [x] Steps inherit parent `taskRoute` via `applyTaskRoute` on the step tool set.

## Task 5: Observe gate + truncated

**Files:** `internal/ccapp/runhost.go`, `internal/ccapp/service.go`, tests

- [x] `observe` JSON adds `truncated`, `maxNodes`, `returned`. Bare `click`/`drag` x,y without this-turn observe → error. `click id=` requires `rememberHits`.

## Task 6: Unnamed nodes only by id

**Files:** `internal/ccapp/host_windows_ui.go`, `host_windows_uia.go`, `service.go`

- [x] Acc unnamed kept as `role (unnamed)`. Name match >1 → fail with candidate ids; `id=B2` succeeds. `InvokeUI("button (unnamed)")` is not a success path.

## Task 7: `verifyAfter` auto-wait

**Files:** `internal/ccapp/service.go`, `internal/storage/sqlite/cc_control_test.go`

- [x] Mutating: same hash → wait 700ms → recapture → still same → error. Observe/list/screenshot unchanged remains success (D-B4/D-L2).

## Task 8: L0 on media / type / browser

**Files:** `desktop_open_verify.go` (reuse hit), `media_foreground.go`, `runtime.go` `desktop.type`, browser execute

- [x] Player window missing → `ok:false`. CC missing → existing error, copy points at settings. Type reread must contain text. MCP not ready / stale ref not `ok:true`. Success paths attach `{"l0":...}`.

## Task 9: `user.ask` reason

**Files:** `chat_tool_defs.go`, `user_ask.go`, `web/src/session/userAsk.ts`

- [x] Optional `reason` enum. Login/pay/captcha from browser runtime park as `user.ask`. UAC tests stay green.

## Task 10: CJK → paste

**Files:** `runtime.go` desktop.type, `runhost.go` keyboard_type

- [x] Han or rune count >16 → existing paste path.

## Task 11: Regression

- [x] `go test ./internal/app ./internal/toolruntime ./internal/ccapp ./internal/domain/provider -count=1`

Must keep C7-6, C8-2, C9-8, companion UAC, soda alias.

---

## Spec coverage

| Spec | Task |
|---|---|
| §5.A D-A1–A7 | 1–2 |
| §5.C D-C1–C3 | 3–4 |
| §5.I §5.K D-I D-K | 5 |
| §5.J | 6 |
| §5.L | 7 |
| §5.B | 8 |
| §5.G | 9 |
| §5.O §5.N | 10–11 |
| P1 kinds/embed/keys | out of this plan |

P1 is a later plan after P0 is green.
