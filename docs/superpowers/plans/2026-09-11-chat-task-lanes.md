# Typed Chat Task Lanes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land PRD v1.4 §0–§11 typed-chat lanes (T01–T21) so ordinary conversation is no longer the old report-pipeline / must-@-to-council path.

**Architecture:** Pure `classifyChatLane` + `buildLaneContract` + `applyLaneTools` shrink tools after `applyTaskRoute`. Council overlay is mount∪chips, not a second @ door. Do not open a second chat engine. Do not change companion §12 except `DisableReasoning` stays on for voice.

**Tech Stack:** Go `internal/app`, existing `go test ./internal/app`.

**Spec:** [docs/design/PRD-chat-task-lanes-v1.md](docs/design/PRD-chat-task-lanes-v1.md) §0–§11 (T01–T21). Not §12.

## Global Constraints

- No commit / push / VERSION / pack unless the user asks.
- Frozen `token_ledger`; no email/shared calendar; `html.gen` stays three templates.
- Do not invent savings %, Word weekly-report metrics, or “supplier passed”.
- `LUNITIDE_CHAT_LANES=off` restores the old pipeline only (not Token Efficiency).
- Windows `go test` needs `all` permissions.
- Companion voice contracts in §12 stay; lanes do not reopen the voice state machine.
- R4 label for 写周报 stays; shrink search in `applyLaneTools`, not `detectTaskRoute`.

---

### Task 1: Lane pure functions (T01, T03–T06, T18, T20)

**Files:**
- Create: `internal/app/chat_lane.go`
- Create: `internal/app/chat_lane_test.go`

**Interfaces:**
- Produces: `ChatLane`, `LaneInput`, `LaneContract`, `CouncilOverlay`, `classifyChatLane`, `hasTurnMaterials`, `applyLaneOverrides`, `buildLaneContract`, `applyLaneTools`, `chatLanesEnabled`

- [ ] Write failing tests for T01/T03/T04/T05/T06/T18/T20 classify + materials + overrides
- [ ] `go test ./internal/app -count=1 -timeout 60s -run TestClassifyChatLane`
- [ ] Implement minimal classifiers and contract builders
- [ ] Re-run until green

### Task 2: applyLaneTools shrink (T17)

**Files:**
- Modify: `internal/app/chat_lane.go`
- Test: `internal/app/chat_lane_test.go`

- [ ] Failing test: L2-ask + R4 defs → no `web.search`/`web.fetch`/`docx.gen`; keeps `user.ask`
- [ ] Failing test: L2 + R4 defs → no search/fetch; keeps `docx.gen`
- [ ] Implement `applyLaneTools` (subtract only)
- [ ] Green

### Task 3: Council mount ∪ chips (T11, T14, T15, T21)

**Files:**
- Modify: `internal/app/chat_expert_council.go` `selectedTurnExpertIDs`
- Modify: `internal/app/chat_expert_council_test.go`
- Modify: `internal/app/chat_lane.go` `councilShouldRun`

- [ ] Change `TestSelectedTurnExpertIDsUsesMountedSubsetOnly` to expect two mounts → two IDs
- [ ] Change chip test: chips must not drop still-mounted IDs (union, cap 8)
- [ ] T21: L0 + two mounts → council Run=false
- [ ] T15: 「请两位一起评」+ 0 mounts → Run=false
- [ ] Implement union roster; wire `councilShouldRun(lane, roster, goal, companion)`
- [ ] Green council + lane tests

### Task 4: Wire start + stream (T02, T03, T16, T17)

**Files:**
- Modify: `internal/app/chat.go` after `classifyTaskRoute`
- Modify: `internal/app/chat_run_stream.go` skip `startDocx/startPpt` when `SkipOfficeResearchPipeline`
- Modify: `internal/app/chat_docx_workflow.go` `docxGenBlocked` respects Skip
- Modify: `internal/app/chat_workflows.go` L2/L2-ask drop research clause

- [ ] Tests: applyLaneTools after route; Skip prevents startDocx; L2-ask no auto web.search
- [ ] Wire contract onto stream state
- [ ] Green targeted tests + existing office/400 tests (T07–T10 regression)

### Task 5: Gate

- [ ] `go test ./internal/app -count=1 -timeout 180s -run "TestClassifyChatLane|TestApplyLaneTools|TestSelectedTurnExpert|TestCouncil|TestInventory|TestCompanionGoal|TestDocx|TestOffice|TestCapability"`
- [ ] Do not claim live 0.4.75 typed chat is fixed until rebuild

---

P1 (not this plan): context path logs, panic stack, Mermaid WIP.  
P2 (not this plan): on-screen lane chip.
