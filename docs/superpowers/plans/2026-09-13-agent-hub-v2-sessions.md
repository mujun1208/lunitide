# Agent Hub V2.1 Implementation Plan（独立模块）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evolve the existing Agent Hub module into multi-turn harness conversations (pick folder, ask/respond, open artifacts) without mounting them on SessionPage, sessions, or messages.

**Architecture:** All thread state lives in new `agent_hub_*` tables. UI stays in `web/src/agentHub`. The product shell only grows two optional sidebar slots and must `setTarget(undefined)` before `setPage('agentHub')`. Turns are polled via `agentHub.thread.get` every 400ms. V1.2 `task.*` stays unchanged.

**Tech Stack:** Go `internal/agenthub`, SQLite 0154, `agentHub.*` Bridge schemas, React in `web/src/agentHub`, existing Job Object helper.

**Spec:** `docs/superpowers/specs/2026-09-13-agent-hub-v2-sessions.md`

## Global Constraints

- Do not import SessionPage, liveChat, ChatArtifactCards, ArtifactInspector, UserAskWizard, message.*, session.create, chat.start, office.generate, agent.run, commandworker.Run.
- Do not FK `sessions`. Do not put harness rows in the personal chat list.
- Do not change `agent_hub_tasks` CHECK or V1 task argv (Codex task still has `--ignore-user-config`).
- Do not hand-edit generated bridge files; add schemas then generate-bridge.
- Register new SQL in `expectedSchemaSQL` (dump test first).
- LaunchSidebar must not import `../agentHub`.
- Do not implement Pi / Claude / OpenCode / OMP / Grok / DeepSeek.
- Do not add a streaming `turn.start` in this plan.
- PowerShell: use `;` not `&&`.
- Touch only files listed in each task.

## File map

| File | Role |
|---|---|
| `migrations/0154_agent_hub_threads.sql` | threads, messages, events, files, prompts |
| `internal/storage/sqlite/store.go` | new expectedSchemaSQL keys only |
| `internal/agenthub/thread.go` | CRUD + PathAllowed |
| `internal/agenthub/thread_runtime.go` | turns, waiting_user |
| `internal/agenthub/loopback.go` | env-gated adapter |
| `internal/agenthub/capability.go` | add Interactive, Protocol on detect JSON |
| `internal/app/agenthub_handlers.go` | new cases; keep old cases identical |
| `api/bridge/v1/agentHub.thread.*.schema.json` | create/get/list/update/cancel/prompt/respond |
| `api/bridge/v1/agentHub.workspace.list.schema.json` | |
| extend detect + file.preview + file.open schemas | extra fields optional |
| `web/src/agentHub/*` | Home, Thread, AskBar, Sidebar, ShellSwitch |
| `web/src/app/LaunchSidebar.tsx` | `topSlot?` `replaceMainNav?` only |
| `web/src/App.tsx` | slots + clear target + Ctrl+N guard |
| `web/src/settings/OfficeMenuPanel.tsx` | one copy string |

---

## Task 0: Schema

SQL (verbatim in migration **and** `expectedSchemaSQL` after dumping the live CREATE text):

Use the five `CREATE TABLE` / indexes from spec §5.

- [ ] Add a failing dump assertion for `table:agent_hub_threads` if missing.
- [ ] Run the sqlite schema test; confirm fail.
- [ ] Add 0154; paste exact dumped SQL into `expectedSchemaSQL`.
- [ ] `go test` the sqlite schema package until green.
- [ ] Commit `0154 agent hub threads (no sessions FK)`.

---

## Task 1: Thread store

**Files:** `internal/agenthub/thread.go`, `thread_test.go`

```go
func PathAllowed(workspace, export, candidate string) bool
func DefaultThreadDir(root, threadID string) string // filepath.Join(root, "threads", threadID)
```

- [ ] Tests: inside workspace true; `..\Windows\win.ini` false; export child true.
- [ ] Insert/Get/List/Update title+pinned; delete cascade messages (if using same DB in test).
- [ ] `go test ./internal/agenthub/ -count=1 -run Thread`
- [ ] Commit `thread store`.

---

## Task 2: Loopback runtime

**Files:** `internal/agenthub/thread_runtime.go`, `loopback.go`, `thread_runtime_test.go`

Rules from spec: `?` or `选择` → open prompt two options 是/否; respond writes `loopback.txt` and assistant text; busy second prompt errors.

Detect: `loopback` only if `os.Getenv("LUNITIDE_HARNESS_LOOPBACK")=="1"`.

- [ ] Test two-step without CLI.
- [ ] Test detect omits loopback by default.
- [ ] Commit `loopback thread runtime`.

---

## Task 3: Bridge + handlers

**Files:** new/extended schemas; `agenthub_handlers.go`; `agenthub_handlers_test.go`; generate-bridge.

Keep every existing `task.*` test byte-compatible.

New handler tests:

- `thread.create` empty workspace → path under Root/threads/id
- `file.preview` `{threadId, path:"..\\Windows\\win.ini"}` fails
- existing preview with `taskId` still fails escape (unchanged)
- loopback env: create → prompt `选哪个?` → get status `waiting_user` → respond → get assistant + file

`detect` JSON includes `interactive` / `protocol` for cursor/kimi/codex (false/`exec` until later tasks).

- [ ] generate-bridge
- [ ] `go test ./internal/app/ -count=1 -run AgentHub`
- [ ] Commit `agentHub.thread bridge`.

---

## Task 4: AgentHub API + AskBar + Home + Thread (no App yet)

**Files:** `agentHubApi.ts` (add types/methods); `AgentHubAskBar.tsx` + test; `AgentHubHome.tsx` + test; `AgentHubThread.tsx` + test; `agentHubCopy.ts` scene blurbs from spec §8.

`AgentHubThread`: poll `thread.get` 400ms when status is `running` or `waiting_user`. Render messages. If open prompt, show AskBar. Composer calls `thread.prompt`. Right pane: `workspace.list` + existing `AgentHubFileInspector`.

Do not import session/*.

- [ ] Home: 写项目 without folder shows「请先选择项目目录」.
- [ ] AskBar click calls respond.
- [ ] `npx vitest run src/agentHub`
- [ ] Commit `agent hub conversation UI`.

---

## Task 5: Sidebar slots (no agentHub import)

**Files:** `LaunchSidebar.tsx`, `LaunchSidebar.test.tsx`

```tsx
topSlot?: React.ReactNode
replaceMainNav?: React.ReactNode
```

Render `topSlot` above the 新对话 button. If `replaceMainNav`, skip office/chats/projects sections (leave that JSX in the else branch unchanged).

- [ ] Existing tests still find 对话 / 办公 when slots omitted.
- [ ] New test: `replaceMainNav={<div>slot</div>}` → 对话 heading absent, slot present.
- [ ] Commit `sidebar slots without agentHub import`.

---

## Task 6: App shell wiring

**Files:** `App.tsx`; `AgentHubShellSwitch.tsx`; `AgentHubSidebar.tsx`; `AgentHubPage.tsx`; `OfficeMenuPanel.tsx` copy.

In the **non-personal** shell (the return that already has `page==='agentHub'`):

- If `officeMenu.agentHub`, pass `topSlot={<AgentHubShellSwitch mode={page==='agentHub'?'agentHub':'lunitide'} onLunitide={()=>setPage(lastOrHome)} onAgents={()=>{ setTarget(undefined); setPage('agentHub') }} />}`
- If `page==='agentHub'`, pass `replaceMainNav={<AgentHubSidebar onOpenThread={...} />}` and `onNew` that starts AgentHubHome new-thread flow (do not `fresh()`).
- Ctrl+N `useEffect`: if `page==='agentHub'` or you are about to, do not `fresh()`. Read `page` from a ref updated every render.

**Personal chat shell (line ~91):** also pass `topSlot` with `onAgents={()=>{ setTarget(undefined); setPage('agentHub') }}` so the switch works while a Lunitide chat is open. Do **not** pass `replaceMainNav` here (user still sees their chats until they switch; switching clears target and remounts the other shell).

`AgentHubPage`: default view Home or Thread from internal state / URL-less selectedThreadId. Four tabs not default. Link「旧版任务」toggles the old Tasks+Detail.

Office menu description (zh): 「在左侧显示月汐 / 外接 Agent 切换，用本机 Cursor、Codex、Kimi 对话。产物在本页打开，不进入月汐对话列表。」

- [ ] Update officeMenu copy test if it asserts old sentence.
- [ ] Update AgentHubPage tests that required 工作台 Tab — expect Home / 旧版任务 instead.
- [ ] Commit `shell switch clears personal target`.

---

## Task 7: Cursor ACP (module only)

**Files:** `internal/agenthub/acpframe.go`, `cursor_acp.go`, `cursor_acp_test.go`, `testdata/cursor-acp-hello.jsonl`

Windows `.cmd` → `versions/*/node.exe` + `index.js` + `acp`. Detect `interactive=true`, `protocol=acp` when exe exists (handshake can be lazy on first thread).

Do not change `cursorAdapter.BuildCommand` (V1 task).

- [ ] Fixture framing tests.
- [ ] Commit `cursor ACP thread adapter`.

---

## Task 8: Codex thread exec (no ignore) + optional app-server

**Files:** `internal/agenthub/codex_thread.go`, tests

Thread path argv **must not** include `--ignore-user-config`.  
`TestCodexBuildCommand` for **tasks** must still expect `--ignore-user-config`.

If a 2s app-server probe is not reliable, ship `interactive=false`, Hint「当前只能一把跑完，不能中途提问」. Do not block.

- [ ] Two tests: task argv has ignore; thread argv has not.
- [ ] Commit `codex thread without ignore-user-config`.

---

## Task 9: Kimi ACP

**Files:** `internal/agenthub/kimi_acp.go`, tests, fixture

`kimi acp` only. Do not add `--skills-dir` on this path. Leave `kimiAdapter.BuildCommand` as V1.

- [ ] Framing tests.
- [ ] Commit `kimi ACP thread adapter`.

---

## Task 10: Export copy

**Files:** `internal/agenthub/export.go`, `export_test.go`

On thread success, copy allowlisted extensions to `export_dir`. Skip source code extensions (spec §8).

- [ ] Tests for allow and skip.
- [ ] Commit `thread export copy`.

---

## Task 11: Verification

- [ ] `go test ./internal/agenthub/ ./internal/app/ -count=1 -run "AgentHub|Thread|Codex|Kimi|Cursor|PathAllowed|Loopback|Export"`
- [ ] `npx vitest run src/agentHub src/app/LaunchSidebar.test.tsx src/settings/officeMenuSettings.test.tsx`
- [ ] Do **not** run/fix SessionPage tests unless they failed from an accidental import (revert that import).
- [ ] Manual: enable Agent Hub menu; open a Lunitide chat; click 外接 Agent; confirm you leave SessionPage; Lunitide 对话 list has no thread titles; loopback or Cursor can ask and open a file.
- [ ] Commit only if the above is green.

---

## Spec coverage

| Spec | Task |
|---|---|
| Isolation / no SessionPage | 4–6 (enforced) |
| Clear target trap | 6 |
| Sidebar slots | 5 |
| 0154 no sessions FK | 0–1 |
| Poll 400ms | 4 |
| Requirements 1–6 | 4, 7–10 |
| V1 task unchanged | 3, 8, 9 |
| Extra harnesses | out of plan |

## Placeholder scan

Codex app-server handshake is explicitly optional in Task 8. No other TBD.
