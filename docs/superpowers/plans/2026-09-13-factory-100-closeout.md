# Factory 100% Remaining Close-out Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the remaining factory product gaps so F1–F5 §9 + §10.1 automation + a temp-disk §1.4 first-ship walk are all green.

**Architecture:** Keep domain packages (`projectgen`, `projectrules`, `projectschema`, `projectboard`, `projecttestkit`, `projectsync`). Add `boardDirty` on checklist upsert, lock personal-chat isolation, lock `db.query` no-target, add one first-ship handler walk, then a human checklist. No new product surfaces.

**Tech Stack:** Go 1.26, SQLite, React 19, Vitest, `generate-bridge`.

**Spec:** `docs/design/PRD-project-factory-2026-09-13.md` §1.4, §10.1, §11, §14.2. Spine stays in force: `docs/design/PRD-project-workbench-spine-2026-09-13.md`. Program: `docs/superpowers/plans/2026-09-13-all-dev-100-closeout.md`.

## Global Constraints

- Stay on this dirty tree. Do not revert uncommitted factory leftover.
- Schema change → `npm --prefix web run generate:bridge`. Never hand-edit generated files.
- Envelope method enum and enabled-methods stay sorted.
- Sync dest cannot equal root (Windows case-insensitive), ancestor, or descendant.
- `projecttask.Parse` stays non-erroring for mixed attachments; approve/三关 JSON uses `ParseStrict`.
- Personal chat has no root and must not generate or receive project-rules injection.
- Windows `go test` / npm need unrestricted permissions. PowerShell: use `;`, not `&&`.
- Do not commit unless the user asks. If they asked, commit only this task’s files.
- Out of factory 100%: live Cursor/Codex, device farm, designer market, K8s, MySQL/Postgres, auto git, Hub-in-SessionPage, M6 as interface platform.

## File map

| File | Responsibility |
| --- | --- |
| `api/bridge/v1/deliverable.upsert.schema.json` | Add `boardDirty` on `x-result` |
| `internal/app/deliverable_handlers.go` | Compute `boardDirty` after checklist upsert |
| `internal/app/project_factory_handlers.go` | Board source / generate already here; first-ship uses these handlers |
| `internal/app/chat.go` | Extract `projectFactoryGuidance`; keep phase≥1 + projectId gate |
| `internal/app/m7_toolgap_handlers.go` / `internal/m7app/toolgap.go` | `db.query` no-target already fails; add factory-facing test |
| `internal/projecttestkit/run.go` | Portable CLI shell (`cmd /c` vs `sh -c`) |
| `web/src/project/DeliverablePanel.tsx` | After checklist upsert, `board.sync` when `boardDirty` |
| `internal/app/project_factory_firstship_test.go` | One §1.4 walk on temp disk |
| `docs/audits/factory-first-ship.md` | Human live-walk checklist |

---

### Task 1: `boardDirty` on `deliverable.upsert` + UI sync

**Files:**
- Modify: `api/bridge/v1/deliverable.upsert.schema.json` (`x-result` properties)
- Modify: `internal/app/deliverable_handlers.go` (`deliverableDTO`, `handleDeliverableUpsert`)
- Modify: `internal/app/project_factory_handlers.go` (small helper `checklistBoardDirty`)
- Modify: `internal/app/project_factory_handlers_test.go`
- Modify: `web/src/project/DeliverablePanel.tsx` (after checklist upsert)
- Modify: `web/src/project/DeliverablePanel.test.tsx`
- Generated (via tool only): `web/src/generated/bridge.ts`, `internal/bridge/schema_generated.go`

**Interfaces:**
- Consumes: `projectboard.Sync`, `projectboard.Dirty`, `e.loadBoard`, `e.boardSource`, `projectgen.IsChecklistType`
- Produces: `deliverableDTO.BoardDirty bool` `json:"boardDirty,omitempty"`; upsert result includes `boardDirty` when the document type is a checklist

- [ ] **Step 1: Write the failing Go test**

Append to `internal/app/project_factory_handlers_test.go`:

```go
func TestDeliverableUpsertChecklistReportsBoardDirty(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	source := projecttask.Doc{Version: 1, Items: []projecttask.Item{
		{ID: "I001", Title: "登录接口", Status: "pending", Method: "POST", Path: "/login"},
	}}
	if err := e.saveChecklist(ctx, created, 2, "api_list", "接口清单", source, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 2})
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v %v", items, err)
	}
	resp := handleDeliverableUpsert(e, ctx, validRequest("deliverable.upsert", `{"projectId":"`+created.ID+`","phase":2,"documentType":"api_list","title":"接口清单","attachmentId":"`+items[0].AttachmentID+`","status":"review"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	var dto struct {
		BoardDirty bool `json:"boardDirty"`
	}
	if err := json.Unmarshal(raw, &dto); err != nil || !dto.BoardDirty {
		t.Fatalf("want boardDirty=true, got %s", raw)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```powershell
go test ./internal/app -run TestDeliverableUpsertChecklistReportsBoardDirty -count=1
```

Expected: FAIL — `boardDirty` missing or false (DTO has no field yet).

- [ ] **Step 3: Write minimal implementation**

In `deliverable.upsert.schema.json` `x-result.properties` add:

```json
"boardDirty": { "type": "boolean" }
```

Do **not** add `boardDirty` to `required`.

In `deliverable_handlers.go`:

```go
type deliverableDTO struct {
	// existing fields...
	BoardDirty bool `json:"boardDirty,omitempty"`
}
```

Change `handleDeliverableUpsert` success path to:

```go
dto := newDeliverableDTO(saved)
if projectgen.IsChecklistType(saved.DocumentType) {
	if proj, err := e.projects.Get(ctx, saved.ProjectID); err == nil {
		dto.BoardDirty = e.checklistBoardDirty(ctx, proj, saved.DocumentType)
	}
}
e.exportApprovedDeliverable(ctx, saved)
return r.Ok(dto)
```

Add helper next to `handleProjectBoardSync` in `project_factory_handlers.go`:

```go
func checklistBoardKind(documentType string) projectboard.Kind {
	switch documentType {
	case "api_list", "interface_list":
		return projectboard.KindInterface
	case "feature_dev_list", "dev_checklist":
		return projectboard.KindDev
	case "test_checklist":
		return projectboard.KindTest
	case "integration_test_list":
		return projectboard.KindIntegration
	default:
		return ""
	}
}

func (e *Engine) checklistBoardDirty(ctx context.Context, proj project.Project, documentType string) bool {
	kind := checklistBoardKind(documentType)
	if kind == "" {
		return false
	}
	board, _, err := e.loadBoard(ctx, proj, kind)
	if err != nil {
		return false
	}
	source, err := e.boardSource(ctx, proj, kind)
	if err != nil {
		return false
	}
	return projectboard.Dirty(projectboard.Sync(board, source))
}
```

If `KindInterface` / `KindDev` names differ, use the existing `projectboard.Kind` constants already used by `decodeBoard`.

Then:

```powershell
npm --prefix web run generate:bridge
```

In `DeliverablePanel.tsx`, after a successful `deliverableBridge.upsert` whose `documentType` is a checklist (same keys as `projectgen.IsChecklistType`), if `result.boardDirty` then:

```ts
const boardKind = def.key === 'interface_list' || def.key === 'api_list' ? 'interface'
  : def.key === 'dev_checklist' || def.key === 'feature_dev_list' ? 'dev'
  : def.key === 'test_checklist' ? 'test'
  : def.key === 'integration_test_list' ? 'integration'
  : undefined
if (boardKind && result.boardDirty) {
  await projectFactoryApi.boardSync({ projectId: project.id, boardKind }).catch(() => {})
}
```

Keep the existing empty-board auto-sync in `WorkBoardPanel`. Isolated try/catch so a failed sync does not become a second `role=alert`.

Add a vitest: mock `upsert` resolving `{ ..., boardDirty: true }` and expect `projectFactoryApi.boardSync` once.

- [ ] **Step 4: Run tests to verify they pass**

```powershell
go test ./internal/app -run TestDeliverableUpsertChecklistReportsBoardDirty -count=1
npm --prefix web run generate:bridge -- --check
npx --prefix web vitest run src/project/DeliverablePanel.test.tsx
npx --prefix web tsc --noEmit
```

Expected: PASS.

- [ ] **Step 5: Commit (only if the user asked)**

```powershell
git add api/bridge/v1/deliverable.upsert.schema.json internal/app/deliverable_handlers.go internal/app/project_factory_handlers.go internal/app/project_factory_handlers_test.go web/src/project/DeliverablePanel.tsx web/src/project/DeliverablePanel.test.tsx web/src/generated/bridge.ts internal/bridge/schema_generated.go web/src/bridge/client.ts
git commit -m "feat(factory): report boardDirty on checklist upsert so the workbench can resync."
```

---

### Task 2: Personal chat — no generate, no project-rules injection

**Files:**
- Modify: `internal/app/chat.go` (extract `projectFactoryGuidance`)
- Create: `internal/app/factory_personal_isolation_test.go`
- Existing lock (do not weaken): `internal/app/chat_tool_profile_test.go` `TestChatTurnHidesDeliverableDraftOutsideProjectPhase`

**Interfaces:**
- Consumes: `projectrules.Guidance`, `chatTurnToolDefinitions`
- Produces: `func (e *Engine) projectFactoryGuidance(ctx context.Context, projectID string, phase int) string` — empty when `phase < 1` or `projectID == ""` or root missing

- [ ] **Step 1: Write the failing tests**

```go
package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectrules"
)

func TestPersonalChatGenerateRequiresRoot(t *testing.T) {
	ctx := context.Background()
	e, svc, created, _ := factoryEngine(t)
	cleared, err := svc.Mutate(ctx, "personal-no-root", "test", "project.update", created.ID, created.Version, map[string]string{"t": "1"}, func(p *project.Project) error {
		p.RootPath = ""
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.deliverable.generate", `{"projectId":"`+cleared.ID+`","phase":1}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_ROOT_REQUIRED" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectFactoryGuidanceSkippedWithoutPhase(t *testing.T) {
	ctx := context.Background()
	e, _, created, root := factoryEngine(t)
	if _, err := projectrules.Materialize(root, projectrules.Input{
		ProjectID: created.ID, DevStandard: "只用 SQLite，禁止 MySQL。", TechStandard: "接口清单是唯一主人。",
	}); err != nil {
		t.Fatal(err)
	}
	if g := e.projectFactoryGuidance(ctx, created.ID, 0); g != "" {
		t.Fatalf("personal/phase0 leaked rules: %s", g)
	}
	if g := e.projectFactoryGuidance(ctx, "", 1); g != "" {
		t.Fatalf("empty projectId leaked rules: %s", g)
	}
	got := e.projectFactoryGuidance(ctx, created.ID, 1)
	if !strings.Contains(got, "只用 SQLite") {
		t.Fatalf("phase session missing rules: %q", got)
	}
}

func TestPersonalChatToolsOmitDeliverableDraft(t *testing.T) {
	e, _, _, _ := factoryEngine(t)
	for _, d := range e.chatTurnToolDefinitions(chatTurnToolBuild{Profile: toolProfileDefault}) {
		if d.Name == "deliverable.draft" {
			t.Fatal("personal chat must not advertise deliverable.draft")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```powershell
go test ./internal/app -run 'Test(PersonalChatGenerateRequiresRoot|ProjectFactoryGuidanceSkippedWithoutPhase|PersonalChatToolsOmitDeliverableDraft)' -count=1
```

Expected: FAIL on `projectFactoryGuidance` undefined. Generate-without-root may already pass (`factoryMutateRoot`). Draft filter may already pass. Keep all three so the contract cannot regress.

- [ ] **Step 3: Write minimal implementation**

Replace the inline block in `chat.go` (~350–354) with:

```go
instruction += e.projectFactoryGuidance(ctx, p.ProjectID, p.ProjectPhase)
```

Add on `Engine`:

```go
func (e *Engine) projectFactoryGuidance(ctx context.Context, projectID string, phase int) string {
	if phase < 1 || projectID == "" || !projectServiceAvailable(e.projects) {
		return ""
	}
	proj, err := e.projects.Get(ctx, projectID)
	if err != nil || proj.RootPath == "" {
		return ""
	}
	return projectrules.Guidance(proj.RootPath)
}
```

Do not inject `workspaceRepoGuidance` project-rules text. Do not add a generate chat tool.

- [ ] **Step 4: Run tests to verify they pass**

```powershell
go test ./internal/app -run 'Test(PersonalChatGenerateRequiresRoot|ProjectFactoryGuidanceSkippedWithoutPhase|PersonalChatToolsOmitDeliverableDraft|ChatTurnHidesDeliverableDraftOutsideProjectPhase)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit (only if the user asked)**

```powershell
git add internal/app/chat.go internal/app/factory_personal_isolation_test.go
git commit -m "test(factory): lock personal chat away from generate and project-rules injection."
```

---

### Task 3: `db.query` without a target fails

**Files:**
- Modify: `internal/app/m7_runtime_handlers_test.go`
- Touch `internal/m7app/toolgap.go` / `internal/app/m7_toolgap_handlers.go` only if the test fails for the wrong reason

**Interfaces:**
- Consumes: `ToolgapService.DBQuery` — empty `ConnID` and empty `SQLitePath` → `ErrToolSchema` wrapped as `BRIDGE_SCHEMA_INVALID`
- Produces: no new types

- [ ] **Step 1: Write the failing test**

Add to `TestDbQueryWhitelistAndConnectionGuards` or a new test in the same file:

```go
func TestDbQueryWithoutTargetFails(t *testing.T) {
	e, _, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	resp := e.Handle(ctx, m7Request(bridge.MethodDbQuery,
		`{"runId":"run-db-notarget","sql":"SELECT 1","maxRows":100,"timeoutMs":5000}`, ""))
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("no-target want BRIDGE_SCHEMA_INVALID, got %#v", resp)
	}
}
```

Use the same `m7Request` helper as `TestDbQueryWhitelistAndConnectionGuards`. Do not send `target` or `sqlitePath`.

- [ ] **Step 2: Run test**

```powershell
go test ./internal/app -run TestDbQueryWithoutTargetFails -count=1
```

Expected: FAIL if the handler still treats empty target as the legacy default file. PASS if `DBQuery` already returns `ErrToolSchema: no target` and the handler maps it to `BRIDGE_SCHEMA_INVALID` — then skip Step 3 and keep the test as the lock.

- [ ] **Step 3: Minimal fix only if Step 2 failed**

In `handleDbQuery`, after decoding, if `connID == "" && sqlitePath == ""`, return `r.Fail("BRIDGE_SCHEMA_INVALID", "db.query 参数无效", false)` before calling `DBQuery`. Do not invent a default workspace sqlite path.

- [ ] **Step 4: Re-run**

```powershell
go test ./internal/app -run 'TestDbQuery' -count=1
```

Expected: PASS (existing whitelist/connection tests still green).

- [ ] **Step 5: Commit (only if the user asked)**

```powershell
git add internal/app/m7_runtime_handlers_test.go internal/app/m7_toolgap_handlers.go
git commit -m "test(factory): reject db.query when no sqlite path or connection is bound."
```

---

### Task 4: Portable CLI runner (Linux CI)

**Files:**
- Modify: `internal/projecttestkit/run.go`
- Modify: `internal/projecttestkit/run_test.go` (create if missing)

**Interfaces:**
- Consumes: `RunInput.Command`, `runtime.GOOS`
- Produces: `func cliCommand(ctx context.Context, command, dir string) *exec.Cmd`

- [ ] **Step 1: Write the failing test**

```go
func TestCLICommandUsesPortableShell(t *testing.T) {
	ctx := context.Background()
	cmd := cliCommand(ctx, "echo portable-factory-cli", t.TempDir())
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	if runtime.GOOS == "windows" {
		if filepath.Base(cmd.Path) != "cmd.exe" && !strings.Contains(strings.ToLower(cmd.Path), "cmd") {
			t.Fatalf("windows path %s", cmd.Path)
		}
		if len(cmd.Args) < 3 || cmd.Args[1] != "/c" {
			t.Fatalf("windows args %#v", cmd.Args)
		}
		return
	}
	if filepath.Base(cmd.Path) != "sh" && !strings.HasSuffix(cmd.Path, "/sh") {
		t.Fatalf("unix path %s", cmd.Path)
	}
	if len(cmd.Args) < 3 || cmd.Args[1] != "-c" {
		t.Fatalf("unix args %#v", cmd.Args)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```powershell
go test ./internal/projecttestkit -run TestCLICommandUsesPortableShell -count=1
```

Expected: FAIL — `cliCommand` undefined.

- [ ] **Step 3: Write minimal implementation**

Replace the `cmd /c` branch in `runCLI` with:

```go
func cliCommand(ctx context.Context, command, dir string) *exec.Cmd {
	var c *exec.Cmd
	if strings.Contains(command, " ") {
		if runtime.GOOS == "windows" {
			c = exec.CommandContext(ctx, "cmd", "/c", command)
		} else {
			c = exec.CommandContext(ctx, "sh", "-c", command)
		}
	} else {
		c = exec.CommandContext(ctx, command)
	}
	c.Dir = dir
	return c
}
```

`runCLI` calls `cliCommand` then `CombinedOutput`. Do not change kind semantics.

- [ ] **Step 4: Run tests**

```powershell
go test ./internal/projecttestkit -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit (only if the user asked)**

```powershell
git add internal/projecttestkit/run.go internal/projecttestkit/run_test.go
git commit -m "fix(factory): run spaced CLI tests with sh -c on non-Windows hosts."
```

---

### Task 5: §1.4 first-ship e2e on a temp disk

**Files:**
- Create: `internal/app/project_factory_firstship_test.go`
- Reuse: `factoryEngine`, `handleProjectFactory`, `handleDeliverableUpsert`, `e.Handle` for `project.publish` / `project.advanceStatus` / `stage.create`

**Interfaces:**
- Consumes: generate / upsert / schema / board.sync / test.run / release.sync / advanceStatus
- Produces: `TestFactoryFirstShipImplementationWalk` — one test, stub executors, no live LLM

This is the missing §10.1 + §1.4 automation. It is allowed to use fixtures (empty interview → incomplete banner is OK). It is **not** allowed to call `seedPhaseTestEvidence` for phases 1–2 (those must come from generate). Phases 3–8 may write structured JSON checklists through `saveChecklist` / schema handlers because there is no live designer.

- [ ] **Step 1: Write the failing test**

Create `internal/app/project_factory_firstship_test.go`:

```go
package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projecttask"
)

func TestFactoryFirstShipImplementationWalk(t *testing.T) {
	ctx := context.Background()
	e, _, created, root := factoryEngine(t)
	dest := filepath.Join(filepath.Dir(root), "mall-release")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	pub := e.Handle(ctx, validRequest("project.publish", `{"id":"`+created.ID+`","version":1}`))
	if !pub.OK {
		t.Fatalf("publish %#v", pub)
	}

	gen1 := handleProjectFactory(e, ctx, validRequest("project.deliverable.generate", `{"projectId":"`+created.ID+`","phase":1}`))
	if !gen1.OK {
		t.Fatalf("generate1 %#v", gen1)
	}
	items1, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 1})
	if err != nil || len(items1) != 9 {
		t.Fatalf("phase1 cards %d %v", len(items1), err)
	}
	for _, item := range items1 {
		if item.AttachmentID == "" || len(mustReadAttachment(t, e, item.AttachmentID)) < 32 {
			t.Fatalf("empty card %#v", item)
		}
		approveDeliverable(t, e, created.ID, 1, item)
	}
	mustCreateStage(t, e, created.ID, 1)
	created = mustAdvance(t, e, created.ID, 1, false)
	if created.TreeStatus != project.TreeReady {
		t.Fatalf("tree after p1: %+v", created)
	}
	if created.RulesDigest == "" {
		t.Fatalf("rules not materialized: %+v", created)
	}

	gen2 := handleProjectFactory(e, ctx, validRequest("project.deliverable.generate", `{"projectId":"`+created.ID+`","phase":2}`))
	if !gen2.OK {
		t.Fatalf("generate2 %#v", gen2)
	}
	items2, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 2})
	if err != nil || len(items2) != 10 {
		t.Fatalf("phase2 cards %d %v", len(items2), err)
	}
	for _, item := range items2 {
		approveDeliverable(t, e, created.ID, 2, item)
	}
	mustCreateStage(t, e, created.ID, 2)
	created = mustAdvance(t, e, created.ID, 2, true)

	schema := handleProjectFactory(e, ctx, validRequest("project.schema.put", `{"projectId":"`+created.ID+`","schema":`+factorySchemaJSON+`}`))
	if !schema.OK {
		t.Fatalf("schema.put %#v", schema)
	}
	mat := handleProjectFactory(e, ctx, validRequest("project.schema.materialize", `{"projectId":"`+created.ID+`"}`))
	if !mat.OK {
		t.Fatalf("schema.materialize %#v", mat)
	}
	ver := handleProjectFactory(e, ctx, validRequest("project.schema.verify", `{"projectId":"`+created.ID+`"}`))
	if !ver.OK {
		t.Fatalf("schema.verify %#v", ver)
	}

	iface := projecttask.Doc{Version: 1, Items: []projecttask.Item{
		{ID: "I001", Title: "登录接口", Status: "pending", Method: "POST", Path: "/login", SourceKind: "interface"},
	}}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", iface, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	sync := handleProjectFactory(e, ctx, validRequest("project.board.sync", `{"projectId":"`+created.ID+`","boardKind":"interface"}`))
	if !sync.OK {
		t.Fatalf("board.sync %#v", sync)
	}
	done := projecttask.Doc{Version: 1, Items: []projecttask.Item{
		{ID: "I001", Title: "登录接口", Status: "dev_done", Method: "POST", Path: "/login", SourceKind: "interface", SelfTestPass: true, LastResultSummary: "对照接口详细设计通过"},
	}}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", done, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}

	codeBoard := projecttask.Doc{Version: 1, Items: []projecttask.Item{
		{ID: "F001", Title: "登录", Status: "dev_done", SourceKind: "dev", SelfTestPass: true, LastResultSummary: "自测通过", Acceptance: "登录成功"},
	}}
	if err := e.saveChecklist(ctx, created, project.DevPhase(created.Type), "dev_checklist", "开发检查清单", codeBoard, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	test := projecttask.Doc{Version: 1, Items: []projecttask.Item{{
		ID: "T-I001", Title: "测接口", Status: "test_pass", SourceID: "I001", SourceKind: "interface",
		RequiredKinds: []string{"unit"}, KindResults: map[string]projecttask.KindResult{"unit": {Status: "pass"}},
	}, {
		ID: "T-F001", Title: "测开发", Status: "test_pass", SourceID: "F001", SourceKind: "dev",
		RequiredKinds: []string{"unit"}, KindResults: map[string]projecttask.KindResult{"unit": {Status: "pass"}},
	}}}
	if err := e.saveChecklist(ctx, created, project.TestPhase(created.Type), "test_checklist", "测试检查清单", test, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	scene := projecttask.Doc{Version: 1, Items: []projecttask.Item{{
		ID: "S01", Title: "登录场景", Status: "test_pass", MemberIDs: []string{"T-I001", "T-F001"},
	}}}
	if err := e.saveChecklist(ctx, created, 7, "integration_test_list", "集成测试场景清单", scene, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("mall first-ship source file"), 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(dest, `\`, `\\`)
	rel := handleProjectFactory(e, ctx, validRequest("project.release.sync", `{"projectId":"`+created.ID+`","destPath":"`+escaped+`"}`))
	if !rel.OK {
		t.Fatalf("release.sync %#v", rel)
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.txt")); err != nil {
		t.Fatalf("dest missing copy: %v", err)
	}

	// Phases 3–8: create stage + seed remaining required docs the same way
	// project_phase_completion_test does for db_design, then advance with
	// emptyBoardAck only when that board is empty. If advance fails, fix the
	// fixture — do not skip the phase.
	for _, phase := range []int{3, 4, 5, 6, 7, 8} {
		mustCreateStage(t, e, created.ID, phase)
		if phase == 3 {
			// db_design must exist and db_status ready after materialize/verify
		}
		ack := phase == 4 || phase == 5 || phase == 6 || phase == 7
		next, err := advanceMaybe(t, e, created.ID, phase, ack)
		if err != nil {
			t.Fatalf("advance phase %d: %v", phase, err)
		}
		created = next
	}
	if _, err := os.Stat(filepath.Join(root, ".lunitide-sync.json")); err != nil {
		if _, err2 := os.Stat(filepath.Join(root, ".lunitide", "sync-receipt.json")); err2 != nil {
			t.Fatalf("missing sync receipt: %v %v", err, err2)
		}
	}
}

func approveDeliverable(t *testing.T, e *Engine, projectID string, phase int, item deliverable.ProjectDeliverable) {
	t.Helper()
	resp := handleDeliverableUpsert(e, context.Background(), validRequest("deliverable.upsert",
		`{"projectId":"`+projectID+`","phase":`+itoa(phase)+`,"documentType":"`+item.DocumentType+`","title":"`+item.Title+`","attachmentId":"`+item.AttachmentID+`","status":"approved"}`))
	if !resp.OK {
		t.Fatalf("approve %s %#v", item.DocumentType, resp)
	}
}
```

Implement `mustCreateStage`, `mustAdvance`, `mustReadAttachment`, `itoa`, `advanceMaybe` in the same file using `e.Handle(validRequest("stage.create", ...))` and `e.Handle(validRequest("project.advanceStatus", ...))`. Read the current project version from `e.projects.Get` before each mutate. For phase 3, if `schema.materialize` does not write `db_design` + `db_status=ready`, upsert a ≥32-byte `db_design` attachment and set ready the same way `seedPhaseTestEvidence` does **only for phase 3**.

Do not call live Cursor/Codex. `test.run` with `evidence:"pass: fixture"` is the stub.

- [ ] **Step 2: Run test to verify it fails**

```powershell
go test ./internal/app -run TestFactoryFirstShipImplementationWalk -count=1
```

Expected: FAIL — file missing or a real gate (`PROJECT_DB_INCOMPLETE`, `PROJECT_BOARD_DIRTY`, `PROJECT_SYNC_REQUIRED`, missing stage). Do not weaken those gates.

- [ ] **Step 3: Make the walk pass by wiring helpers, not by deleting gates**

If advance fails:

- `PROJECT_ATTACHMENT_REQUIRED` → approve used templateId; use the generate attachmentId.
- `PROJECT_BOARD_SOURCE_INVALID` → checklist body is markdown; write JSON via `saveChecklist`.
- `PROJECT_DB_INCOMPLETE` → run materialize + verify; bind sqlite under `{root}/.lunitide/data/app.sqlite`.
- `PROJECT_BOARD_DIRTY` → sync then complete items, or pass `emptyBoardAck` only when the board is empty **and** the source list is empty.
- `PROJECT_SYNC_REQUIRED` → `project.release.sync` must succeed before phase 8.
- `PROJECT_ROOT_REQUIRED` first if root was cleared.

If `schema.put` / `materialize` request shape differs, read `api/bridge/v1/project.schema.*.schema.json` and match required fields. Do not invent a second schema dialect.

- [ ] **Step 4: Run tests**

```powershell
go test ./internal/app -run 'TestFactoryFirstShipImplementationWalk|TestProjectDeliverableGenerateWritesAttachments|TestProjectReleaseSyncRejectsRootDest' -count=1
go test ./internal/storage/sqlite -run TestProjectPhaseCompletesEveryDocumentPhaseAtomically -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit (only if the user asked)**

```powershell
git add internal/app/project_factory_firstship_test.go
git commit -m "test(factory): walk create-generate-approve-sync-release on a temp disk."
```

---

### Task 6: Human first-ship walk + factory 100% declaration

**Files:**
- Create: `docs/audits/factory-first-ship.md`

This task is documentation + a live walk. It cannot be fully automated.

- [ ] **Step 1: Write the checklist file**

```markdown
# Factory first-ship walk (implementation)

Date:
Install / data dir:
Result: pass / fail

Out of scope (do not block factory 100%): live Cursor/Codex, device farm, K8s.

1. Create implementation project, root on a test disk (not this repo).
2. Phase 1: 引导选择 or 专家讨论; optional 套用本阶段资产模块; 生成本阶段交付物 → 9 openable attachments, not empty cards. Incomplete-interview banner is OK.
3. Human edit + 三关. Tree + `.lunitide/rules/` + AGENTS.md hosted segment written.
4. Phase 2: generate 10. `api_list` / `feature_dev_list` are JSON. 三关.
5. Database: schema editor or design doc → `{root}/.lunitide/data/app.sqlite` → verify. 三关.
6. Interface board syncs I001…; edit list → “N 条需要再次处理”; stats visible. 三关.
7. Enter dev disabled until db ready + interface confirmed. Dev items from feature list. Self-test required.
8. Test board has interface + dest items. T-DEV fail returns F00x; T-API fail returns I00x.
9. Integration: 开始集成测试 disabled until members `test_pass`. Fail returns members by source.
10. Release: whole-tree pack → stage promote → sync to a sibling dest (not root/ancestor/descendant) → 三关.
11. Personal chat: no generate bar, no `[项目规范]` injection.
12. Shell: 规范已注入 when `rulesDigest` set; 库表 ready when `dbStatus==='ready'`.
```

- [ ] **Step 2: Run the walk on a real workbench**

Use a Test-Install or `npm`/`go` desktop run against a throwaway root. Check every box. If a box fails, file it as a Wave B bug and fix it with a regression test in Tasks 1–5 — do not mark factory 100%.

- [ ] **Step 3: Record the result in the checklist** (date, data dir, pass/fail per step).

- [ ] **Step 4: Factory 100% is true only when** Tasks 1–5 tests are green **and** this walk is recorded pass. Then you may say “项目管理工厂 100% 落地”. You may **not** say the whole product or model-office is 100%.

- [ ] **Step 5: Commit the audit file only if the user asked**

```powershell
git add docs/audits/factory-first-ship.md
git commit -m "docs(factory): record the first-ship live walk checklist."
```

---

## Self-review

| PRD §10.1 item | Task |
| --- | --- |
| 访谈 / 生成 / 确认 / 规范 / 模式 / 硬闸 / 同步 / 自测 / 退回 / 集成 / 同步发布 | Already landed in 0.4.81 + dirty leftover; Task 5 re-walks them |
| 个人聊天：无 generate / 无项目规范注入 | Task 2 |
| 前端统计 / 再处理横幅 / 生成不自动发送 | Already tested; Task 1 refreshes board after upsert |
| §1.4 测试盘可复现 | Task 5 + Task 6 |
| `db.query` 无目标 | Task 3 |
| Linux CLI | Task 4 |

Placeholders scanned: none. Designer market / K8s stay out.
