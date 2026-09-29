# Codex pipeline corrections

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:test-driven-development while implementing. Do not commit unless the user asks.

**Goal:** Make a typed turn survive optional context failures, and make an approval continuation skip the pre-turn memory flush, without widening which files the page can read.

**Architecture:** Two pure functions own the policy. `preturnHardStop` aborts only on cancel or deadline. `settleTurnStatus` records an approval wait as interrupted so the existing `liveTaskSkipsSessionSummary("继续")` path applies. Call sites stay in `chat.go` and `chat_run_stream.go`.

**Tech Stack:** Go engine tests (`go test` from the repo root), existing chat checkpoint helpers.

**Spec:** Canvas `codex-pipeline-review.canvas.tsx` after the 2026-09-29 correction. Codex comparison is public product behavior, not a source reading.

## Global Constraints

- Do not install over a running `Lunitide.exe`.
- Do not commit, tag, or release unless the user asks.
- Do not let the renderer import an arbitrary path. `attachment.importLocal` stays behind `desktopfiles.Allowed`.
- Do not raise the 500MB store cap or the 32MB text-extraction cap.
- `SessionPage.tsx` and `chat_run_stream.go` may be split only by moving existing code into new files.
- A missing explicit attachment still fails the turn.
- Cancel and deadline still fail the turn.

## Review Focus

- Optional summary, handoff, or compaction errors used to return `CONTEXT_SUMMARY_READ_FAILED`, `HANDOFF_CONTEXT_READ_FAILED`, or `COMPACTION_TRIGGER_FAILED` and the user could not send. They now log and continue. A person expects the answer to start.
- Approval used to save the checkpoint as completed, so the following「继续」ran memory hygiene and a watermark check again. It now saves interrupted. A person expects confirm to continue the same task without a second warmup. A new sentence still runs the warmup.
- A dropped file is allowlisted by the desktop host, then imported by path. A path the page invents is still refused. Clipboard images stay on the byte upload.

## Report corrections (do not reintroduce)

- Parallel read-only tools are 6: `workspace.list`, `workspace.read`, `workspace.search`, `web.fetch`, `web.search`, `excel.parse`. Cap is 6. The earlier report said 5.
- `TriggerPreTurnCompaction` calls the compaction model only after `CheckAndTrigger` says the high watermark was crossed. Every non-companion typed turn still runs `flushMemoryBeforeCompaction` and the watermark check first.
- Default profile starts from `chat_tool_defs.go` (38 named tools) and then adds plan, MCP, computer-control, skill, expert, plugin, and settings tools.
- `maxToolLoopSteps` and `maxToolLoopStepsHard` are both 240, so the extension chunk cannot raise the ceiling.

## Landed after the first slice

- [x] **Host drop.** The desktop host records paths from `WM_DROPFILES` with `GrantPaths`. The page calls `attachment.importLocal` for those paths. A path the page invents is still refused. Without the host webview, the page keeps the byte upload.
- [x] **Same-stream approval.** Tools other than `user.ask` wait on the open stream until `chat.tool.approve` delivers the result. The page does not send another「继续」while that stream is open. `user.ask` still ends the stream so the typed answer is a new message.
- [x] **File split.** Session helpers live in `sessionPageShared.tsx`, the chat surface in `SessionMessagePanel.tsx`, and `runStream` in `chat_run_loop.go`. `SessionPage` still exports the execution-mode helpers.

## Landed in the working tree after Task 1

- [x] **Read profile.** An attachment turn with no action hint uses `toolProfileRead` (`workspace.list/read/search`, `web.search`, `web.fetch`, `memory.search`, `memory.get`, `user.ask`). 「运行」「生成」「打开」and the other action hints stay on the full surface. Explicit profiles and companion turns are unchanged.
- [x] **One advertised shell.** `chatTurnToolDefinitions` omits `run_terminal_cmd`. `engineToolDefinitions` still lists it, and both executors stay.
- [x] **Bounded path import.** Files larger than `importExtractMax` (`doctext.MaxInputBytes`, 32MB) are streamed into storage. Text excerpts are a UTF-8 prefix. Office, PDF, and zip files are stored whole with a note, and are not parsed from a truncated archive. `MaxFileSize` stays 500MB.

Verification: `go test -count=1 -timeout 180s -run "TestImportPath|TestInternalImportPathCopiesLocalText|TestAttachmentReadKeepsANarrowToolSurface|TestAdvertisedToolsHideTheAliasShell|TestChatTurnHidesDeliverableDraftOutsideProjectPhase|TestApplyToolProfileKeepsDefaultAndFilters|TestAutoToolProfile|TestPreturnHardStopOnlyForCancel|TestApprovalWaitKeepsTheTurnUnfinished|TestSettleTurnStatusMatchesTheStreamTerminal|TestLiveTaskSkipsTheSessionSummary" ./internal/attachmentapp/ ./internal/app/` exited 0. `go test -count=1 -timeout 180s ./internal/attachmentapp/` exited 0.

### Task 1: Preturn policy

**Files:**
- Create: `internal/app/chat_preturn_policy_test.go`
- Create: `internal/app/chat_preturn_policy.go`
- Modify: `internal/app/chat.go` (compaction, summary, handoff error branches)
- Modify: `internal/app/chat_run_stream.go` (status switch around the terminal event)

**Interfaces:**
- Produces: `func preturnHardStop(err error) bool`
- Produces: `func settleTurnStatus(current string, terminal bridge.EventType, waitingForApproval, hadTools bool, err error) string`

- [x] **Step 1–5:** Policy tests passed. `preturnHardStop` and `settleTurnStatus` are wired. `go test -count=1 -run "TestPreturnHardStopOnlyForCancel|TestApprovalWaitKeepsTheTurnUnfinished|TestSettleTurnStatusMatchesTheStreamTerminal|TestLiveTaskSkipsTheSessionSummary" ./internal/app/` exited 0.
