# Office Trial-Ready Pack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Stay on `feat/prd-v7-s1-continuity`. Do not use git worktrees or finishing-a-development-branch. Do not commit unless the user asks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Progress:** TR1–TR6 done. Trial-pack scope 6/6 = 100%. Full HTML PRD product-验收 still ~60%.

**Goal:** Close a trial pack that can reach 100% of its own scope, so testers can walk the Studio path without invented brief defaults or unexplained formal-disabled buttons.

**Architecture:** Keep generate defaults in `NormalizeBrief` for Word/PPT planning only. Persist and snapshot the raw Brief. Studio displays raw fields. Formal stays blockers-only. Do not claim the HTML PRD product-验收 is 100%.

**Tech Stack:** Go `internal/officeapp`, `internal/app`; Vitest `web/src/officeStudio`.

**Spec:** [docs/superpowers/specs/2026-09-12-office-trial-ready.md](../specs/2026-09-12-office-trial-ready.md)

## Global Constraints

- Stay on `feat/prd-v7-s1-continuity`. No commit / push / VERSION / pack unless the user asks.
- Frozen `token_ledger`; no email/shared calendar; `html.gen` stays `penalty-shootout|timer|checklist`.
- Do not invent savings %, Word weekly-report metrics, or “supplier passed”.
- Do not claim beat Gamma / Plus AI / Beautiful.ai; do not flip `designerReviewed` to 36; do not claim calibrated visual 85.
- Formal still requires real blockers-only; skip honest `target-*` / `visual-model` / `pdfa` gaps in `QualityFor`.
- `ValidateFactSet` stays strict (no empty `Fact.Value`).
- Live app stays 0.4.75 until rebuild/restart — never claim 真机已修.
- FR17 and FR18 stay explicit no.
- Do not edit `c:\Users\mujun\.cursor\plans\office_quality_commercial_a4a3b6e5.plan.md`.
- Windows `go test` / `npm` need `required_permissions: ["all"]`.
- Do not hand-edit `web/src/generated/bridge.ts` or `internal/bridge/schema_generated.go`.
- Skip plan commit steps unless the user asks.

---

## Honest scope

This plan is **100% completable**. It is not the whole HTML PRD.

Already landed and must not be redone: Waves 1–10, remaining oneshot Tasks 1–8, production wiring W1–W6, collateral S1–S5, 12-fixture `qa_harness` generate, empty `blind-eval.json`.

---

### Task 1: Persist authored brief without inventing defaults

**Files:**
- Modify: `internal/officeapp/planning.go` (`WithTaskBrief`)
- Test: `internal/officeapp/planning_test.go`

**Interfaces:**
- Consumes: `WithTaskBrief(previous json.RawMessage, brief officestudio.Brief) json.RawMessage`, `BriefFromCheckpoint`
- Produces: persisted Brief equals authored fields; empty audience/purpose/targetLength/confidentiality stay empty; authored confidentiality kept

- [x] **Step 1: Write the failing test**

Append to `internal/officeapp/planning_test.go`:

```go
func TestWithTaskBriefPersistsAuthoredFieldsWithoutInventingDefaults(t *testing.T) {
	got := BriefFromCheckpoint(WithTaskBrief(nil, officestudio.Brief{Confidentiality: "内部"}))
	if got.Audience != "" || got.Purpose != "" || got.Language != "" || got.TargetLength != 0 || len(got.Deliverables) != 0 {
		t.Fatalf("persisted invented defaults: %#v", got)
	}
	if got.Confidentiality != "内部" {
		t.Fatalf("dropped confidentiality: %#v", got)
	}
	empty := BriefFromCheckpoint(WithTaskBrief(nil, officestudio.Brief{}))
	if empty.Audience == "管理层" || empty.Purpose == "经营汇报" || empty.TargetLength == 12 || empty.Confidentiality != "" {
		t.Fatalf("empty brief invented values: %#v", empty)
	}
}
```

Keep `TestNormalizeBriefFillsDefaultsWithoutInventingFacts` unchanged. Generate planning still uses `NormalizeBrief`.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/officeapp -count=1 -timeout 60s -run TestWithTaskBriefPersistsAuthoredFieldsWithoutInventingDefaults`

Expected: FAIL because `WithTaskBrief` currently calls `NormalizeBrief` before encode.

- [x] **Step 3: Write minimal implementation**

In `WithTaskBrief`, persist `brief` as given. Do not call `NormalizeBrief` here. `applyTaskBrief` still uses `NormalizeBrief(raw)` for Word/PPT planning and raw fields for PDF.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/officeapp -count=1 -timeout 60s -run "TestWithTaskBriefPersistsAuthoredFieldsWithoutInventingDefaults|TestNormalizeBriefFillsDefaultsWithoutInventingFacts|TestApplyTaskBrief"`

Expected: PASS

- [ ] **Step 5: Commit**

Skip unless the user asks.

---

### Task 2: Snapshot returns raw brief; confidentiality roundtrips

**Files:**
- Modify: `internal/app/office_studio.go` (`officeTaskDTO`)
- Test: `internal/app/office_context_test.go`

**Interfaces:**
- Consumes: `officeapp.BriefFromCheckpoint`, `office.task.update` already accepts `content.Brief` including `Confidentiality`
- Produces: create/get snapshot audience/purpose/targetLength/confidentiality empty unless authored; update `{confidentiality:"内部"}` returns `内部` and does not invent `管理层`

- [ ] **Step 1: Write the failing tests**

Append to `internal/app/office_context_test.go`:

```go
func TestOfficeTaskSnapshotDoesNotInventBriefDefaults(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "brief-raw")
	r := officeCall(t, e, "office.task.get", "brief-get", map[string]any{"taskId": task.ID})
	if !r.OK {
		t.Fatalf("get: %+v", r.Error)
	}
	var page struct {
		Task struct {
			Brief struct {
				Audience        string `json:"audience"`
				Purpose         string `json:"purpose"`
				TargetLength    int    `json:"targetLength"`
				Confidentiality string `json:"confidentiality"`
			} `json:"brief"`
		} `json:"task"`
	}
	if err := decodeResponsePayload(r.Payload, &page); err != nil {
		t.Fatal(err)
	}
	if page.Task.Brief.Audience != "" || page.Task.Brief.Purpose != "" || page.Task.Brief.TargetLength != 0 || page.Task.Brief.Confidentiality != "" {
		t.Fatalf("invented brief defaults: %+v", page.Task.Brief)
	}
}

func TestOfficeTaskUpdatePersistsConfidentialityWithoutInventingAudience(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "brief-conf")
	r := officeCall(t, e, "office.task.update", "brief-conf-upd", map[string]any{
		"taskId": task.ID, "expectedRevision": task.Revision, "title": task.Title, "goal": task.Goal,
		"brief": map[string]any{"confidentiality": "内部"},
	})
	if !r.OK {
		t.Fatalf("update: %+v", r.Error)
	}
	var page struct {
		Task struct {
			Brief struct {
				Audience        string `json:"audience"`
				Confidentiality string `json:"confidentiality"`
			} `json:"brief"`
		} `json:"task"`
	}
	if err := decodeResponsePayload(r.Payload, &page); err != nil || page.Task.Brief.Confidentiality != "内部" {
		t.Fatalf("confidentiality: %+v %v", page.Task.Brief, err)
	}
	if page.Task.Brief.Audience == "管理层" {
		t.Fatal("update invented audience")
	}
}
```

Do not change `TestOfficeChatEvidenceIncludesBriefFacts`. Chat evidence may still `NormalizeBrief` at read time.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app -count=1 -timeout 90s -run "TestOfficeTaskSnapshotDoesNotInventBriefDefaults|TestOfficeTaskUpdatePersistsConfidentialityWithoutInventingAudience"`

Expected: FAIL — DTO still wraps `NormalizeBrief`.

- [ ] **Step 3: Write minimal implementation**

In `officeTaskDTO`, set `o["brief"] = officeapp.BriefFromCheckpoint(t.Checkpoint)` (raw). Do not call `NormalizeBrief` on the snapshot.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app -count=1 -timeout 90s -run "TestOfficeTaskSnapshotDoesNotInventBriefDefaults|TestOfficeTaskUpdatePersistsConfidentialityWithoutInventingAudience|TestOfficeChatEvidenceIncludesBriefFacts"`

Expected: PASS

- [ ] **Step 5: Commit**

Skip unless the user asks.

---

### Task 3: Studio brief strip and confidentiality field

**Files:**
- Modify: `web/src/officeStudio/officeQualityUi.ts`
- Modify: `web/src/officeStudio/officeStudioApi.ts` (`OfficeBrief`)
- Modify: `web/src/officeStudio/OfficeStudioPage.tsx`
- Test: `web/src/officeStudio/officeQualityUi.test.ts`
- Test: `web/src/officeStudio/OfficeStudioPage.test.tsx`

**Interfaces:**
- Consumes: `OfficeBrief.confidentiality?: string`
- Produces: `briefFieldLabel(value?: string): string`, `briefLengthLabel(targetLength?: number): string`

- [ ] **Step 1: Write the failing tests**

In `officeQualityUi.test.ts`:

```ts
it('labels empty brief fields as unset instead of inventing defaults', () => {
  expect(briefFieldLabel('')).toBe('未填写')
  expect(briefFieldLabel('  客户  ')).toBe('客户')
  expect(briefLengthLabel(undefined)).toBe('页数未填写')
  expect(briefLengthLabel(0)).toBe('页数未填写')
  expect(briefLengthLabel(8)).toBe('约 8 页')
})
```

In `OfficeStudioPage.test.tsx`, change the formal-deliver case so an empty fixture brief shows `未填写` / `页数未填写`, not `管理层`. Add:

```ts
it('saves authored confidentiality without inventing a classification', async () => {
  const api = apiFor()
  await open(api)
  fireEvent.change(screen.getByLabelText('密级'), { target: { value: '内部' } })
  fireEvent.click(screen.getByRole('button', { name: '保存概要' }))
  await waitFor(() =>
    expect(api.update).toHaveBeenCalledWith(
      expect.objectContaining({
        brief: expect.objectContaining({ confidentiality: '内部' }),
      }),
    ),
  )
  expect(api.update).not.toHaveBeenCalledWith(
    expect.objectContaining({
      brief: expect.objectContaining({ confidentiality: '机密' }),
    }),
  )
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run from `web`: `npx vitest run src/officeStudio/officeQualityUi.test.ts src/officeStudio/OfficeStudioPage.test.tsx`

Expected: FAIL — helpers missing; strip still says 管理层; no 密级 field.

- [ ] **Step 3: Write minimal implementation**

Add helpers to `officeQualityUi.ts`. Add `confidentiality?: string` to `OfficeBrief`. Extend `emptyBriefDraft` and the hydrate/save path. Render strip with helpers. Add a 密级 input. Only include `confidentiality` in the update payload when trimmed non-empty.

- [ ] **Step 4: Run tests to verify they pass**

Run from `web`: `npx vitest run src/officeStudio/officeQualityUi.test.ts src/officeStudio/OfficeStudioPage.test.tsx`

Expected: PASS

- [ ] **Step 5: Commit**

Skip unless the user asks.

---

### Task 4: Formal blocked reason and trial scope notice

**Files:**
- Modify: `web/src/officeStudio/officeQualityUi.ts`
- Modify: `web/src/officeStudio/OfficeInspector.tsx`
- Modify: `web/src/officeStudio/OfficeStudioPage.tsx`
- Test: `web/src/officeStudio/officeQualityUi.test.ts`
- Test: `web/src/officeStudio/OfficeStudioPage.test.tsx`

**Interfaces:**
- Produces: `formalDeliverBlockedReason(version?: Pick<OfficeVersion, 'quality' | 'validations'>): string`
- Produces: `trialScopeNotice(): string`

- [ ] **Step 1: Write the failing tests**

```ts
it('explains why formal deliver stays disabled', () => {
  expect(formalDeliverBlockedReason({ quality: 'partial', validations: [] })).toContain('草稿')
  expect(
    formalDeliverBlockedReason({
      quality: 'partial',
      validations: [{ id: 'native_render', label: '渲染', status: 'unavailable', severity: 'blocking', message: '缺组件' }],
    }),
  ).toContain('渲染')
  expect(formalDeliverBlockedReason({ quality: 'passed', validations: [] })).toBe('')
})

it('states the trial scope without competitor claims', () => {
  expect(trialScopeNotice()).toContain('试验范围')
  expect(trialScopeNotice()).toContain('不声称')
  expect(trialScopeNotice()).not.toContain('已超过')
})
```

In the existing formal-deliver Studio test, expect the blocked reason and trial notice to be visible.

- [ ] **Step 2: Run tests to verify they fail**

Run from `web`: `npx vitest run src/officeStudio/officeQualityUi.test.ts src/officeStudio/OfficeStudioPage.test.tsx`

Expected: FAIL — functions missing.

- [ ] **Step 3: Write minimal implementation**

Implement the two functions. Show `formalDeliverBlockedReason(version)` under each version’s formal button when non-empty. Show `trialScopeNotice()` next to the existing deferred notice.

Copy for `formalDeliverBlockedReason`:

- no version → `未选择版本，不能作为正式交付。`
- any failed check or blocking check not passed → `正式交付仍被阻断：{labels}。本机未验证的目标软件、视觉模型和 PDF/A 不单独算正式门槛。`
- quality !== passed → `检查尚未全部通过，只能导出草稿，不能作为正式交付。`
- otherwise empty

Copy for `trialScopeNotice`:

`本期试验范围：可编辑简报、四格式生成、检查、局部修改、草稿或正式导出。不声称设计师已检 36 变体、校准 85 分、PowerPoint/WPS 实机通过、竞品盲评或企业模板审批。`

- [ ] **Step 4: Run tests to verify they pass**

Same vitest command. Expected: PASS

- [ ] **Step 5: Commit**

Skip unless the user asks.

---

### Task 5: Tester protocol

**Files:**
- Create: `docs/qa/office-trial-protocol-2026-09-12.md`

**Interfaces:**
- Consumes: closed scope in the spec
- Produces: a script testers can follow without treating out-of-scope PRD items as product bugs

- [ ] **Step 1: Write the protocol**

Include: rebuild/restart required (live app is 0.4.75 until then); create task; fill brief including optional 密级; generate four formats via chat; inspect checks; try formal (expect disabled without passed quality / LibreOffice); export draft; list results that are **not** failures (target-app unsupported, visual-model unsupported, pdfa unsupported, visual score uncalibrated, no competitor scores).

- [ ] **Step 2: Review against the spec**

Every TR1–TR6 path appears. No invented savings or competitor claims.

- [ ] **Step 3: Commit**

Skip unless the user asks.

---

### Task 6: Gate and progress board

**Files:**
- Modify: `C:/Users/mujun/.cursor/projects/e-Trae-Work-Projects-lunitide/canvases/office-prd-completion.canvas.tsx`

- [ ] **Step 1: Run the engineering gate**

```
go test ./internal/officestudio ./internal/officetools ./internal/officeapp ./internal/officerender ./internal/domain/officestudio
go test ./internal/app -run "TestOffice|TestStudio|TestGenerate|TestChatLanesOff|TestClassifyChatLane"
npx vitest run src/officeStudio
go build ./cmd/engine ./cmd/desktop
```

Working directory for vitest: `web`. Use `required_permissions: ["all"]`.

- [ ] **Step 2: Update the canvas**

Show two progress numbers:

- **试验包范围进度 / 完成进度**：TR1–TR6 done / 6 = 100% when this plan is finished
- **整份 PRD 产品验收**：still about 60%

Do not raise FR17/FR18. Do not claim 真机已修.

- [ ] **Step 3: Commit**

Skip unless the user asks.
