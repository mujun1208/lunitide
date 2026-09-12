# Office Closed-Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or test-driven-development. Stay on `feat/prd-v7-s1-continuity`. No worktree. No finishing-a-development-branch. Do not commit unless the user asks.

**Goal:** Close remaining Studio/chat office dead ends so the closed-loop PRD 1.1 can finish without fake passed states.

**Architecture:** Keep Go generate kernel, chat-only `office.generate`, Studio check/patch/export. Fix honesty and Formal rules; do not add Presenton or refactor `OfficeStudioPage.tsx`.

**Tech Stack:** Go engine, React Studio, Vitest, `go test` with Windows `required_permissions: ["all"]`.

**Spec:** [docs/design/PRD-office-platform-closed-loop-2026-09-12.md](../../design/PRD-office-platform-closed-loop-2026-09-12.md) v1.1（含 1.1 补记 / CL33）

## Global Constraints

- Stay on `feat/prd-v7-s1-continuity`. No commit / push / VERSION / pack unless asked.
- Frozen `token_ledger`; `html.gen` stays `penalty-shootout|timer|checklist`.
- `designerReviewed` stays `0`. Do not put Presenton on Generate.
- Formal still skips honest unsupported/missing `target-*` / `visual-model` / `pdfa`.
- Formal must **not** skip `native_render` / `fields_update` / `full_recalculation` for Word/PPT/Excel.
- gofpdf independent PDF **must** be able to Formal after A1 (rewrite the old Typst-missing Formal block test).
- Do not hand-edit `web/src/generated/bridge.ts` / `schema_generated.go`. If accept grows a `formal` field, run `npm --prefix web run generate:bridge`.
- Do not split `OfficeStudioPage.tsx`.
- Live 0.4.75 is not this pack's pass criterion.

---

### Task 1: A1 Independent PDF is this file's backend

**Files:**
- Modify: `internal/officestudio/adapter.go` (`IndependentPDFCheck`, `RenderIndependentPDFWithTheme`, `ProbeTypst` copy)
- Modify: `internal/officestudio/quality.go` (`EvaluateQuality` must not treat gofpdf fallback as a Formal blocker)
- Modify: `internal/officestudio/patch.go` (`ValidateBrand` must use this version's PDF backend tag, not env probe)
- Test: `internal/officestudio/adapter_test.go`

**Interfaces:**
- Consumes: `TypstExecutable()`, `officetools.GenStablePDFThemed`
- Produces: `Check{ID:"independent_pdf", Status, Message}` where Message names the real backend

- [x] **Step 1: Rewrite the failing/old test first**

Change `TestIndependentPDFMissingWithoutTypstBlocksFormal` so that a gofpdf fallback check does **not** set `FormalOK=false`. Add/keep a case: Typst configured but compile failed then fallback → status is not Typst-passed.

- [x] **Step 2: Run the test and confirm it fails on current code**

```text
go test ./internal/officestudio -count=1 -run TestIndependentPDFMissingWithoutTypstBlocksFormal
```

Expected: FAIL because current `EvaluateQuality` treats `independent_pdf` missing as a blocker.

- [ ] **Step 3: Minimal implementation**

`RenderIndependentPDFWithTheme` must return a check that describes **this file**:
- Typst compiled this file → `passed` + Typst in message
- gofpdf wrote this file → `passed` (or Formal-skippable fallback) + 「稳定独立 PDF，不是 Typst 出版稿」
- Never `passed` just because the Typst binary exists

- [ ] **Step 4: Run**

```text
go test ./internal/officestudio -count=1 -timeout 120s
```

Expected: PASS, including Formal skip tests for target/visual/pdfa.

---

### Task 2: A2 + A9 + A11 Status labels, 排版已检查, promise IDs

**Files:**
- Modify: `web/src/officeStudio/officeQualityUi.ts`
- Test: `web/src/officeStudio/officeQualityUi.test.ts`
- Optionally Inspector if label needs check id

- [ ] **Step 1: Failing vitest (CL12 + CL33 + A9)**

- `qualityPromiseLabels` with only `geometry_bounds`/`layout=passed` must **not** include `排版已检查`
- with `native_render=passed` must include it
- `native_render` missing/unsupported must not use the same optional copy as target-app
- 「内容完整」must light on `package`/`pdf_structure` passed, **not** on fake `structure`
- 「关键数字有来源」must use FactSet / `FindFactRefs`, **not** invented `fact|source|metric` check ids
- Rewrite existing vitest fixtures that currently encode the wrong ids

- [ ] **Step 2:** `npx vitest run src/officeStudio/officeQualityUi.test.ts` — FAIL for the right reason

- [ ] **Step 3:** Minimal label change in `qualityPromiseLabels` / `officeCheckStatusLabel`

- [ ] **Step 4:** `npx vitest run src/officeStudio` — PASS

---

### Task 3: A3 + A5 + A6 Export / same-source / passed≠accepted

**Files:**
- Modify: `internal/app/office_studio.go` export (`ExportNotice(kind, false)` is the bug)
- Modify: bundle export path in `internal/app/office_delivery.go` / `internal/officeapp/bundle.go`
- Test: `internal/officestudio/adapter_test.go`, add app-level export test

- [ ] Pass `sameSource` true only when evidence has a non-stale `SameSourcePDF`
- [ ] Notices must not call `passed` 「已正式接受」
- [ ] Stale source digest → preview/export/bundle all refuse to present the old PDF as current formal reading copy

---

### Task 4: A10 Formal accept is server-gated

**Files:**
- Modify: `internal/app/office_studio.go` `office.artifact.accept`
- Schema: only if a `formal` boolean is required — then `npm --prefix web run generate:bridge` (never hand-edit)
- Test: `internal/app/office_studio_test.go`
- UI already disables the button; keep 「接受为草稿」 working for non-passed

- [ ] Formal accept of `quality!=passed` returns `OFFICE_DRAFT_REQUIRED`
- [ ] Draft accept of the same version succeeds
- [ ] Export without `draft` still requires `quality=passed`

---

### Task 5: A4 A7 A8 Locate / sync / cancel

**Files:**
- `web/src/officeStudio/OfficeStudioPage.tsx` (surgical only)
- `web/src/officeStudio/OfficeStudioPage.test.tsx`
- `internal/app/office_studio_test.go`

- [ ] Locate (checks + metrics): walk preview pages or report 不在此版本; never silent no-op
- [ ] After `office.generate`, `office.task.get` lists the new artifact without chat UI
- [ ] `stopCheck` must not cancel in-flight `office.generate`; check panel shows 检查未完成; last version remains

---

### Task 6: Phase A gate

```text
go test ./internal/officestudio ./internal/officeapp ./internal/officerender -count=1 -timeout 180s
go test ./internal/app -count=1 -timeout 180s -run "TestOffice|TestStudio|TestGenerate"
npx vitest run src/officeStudio
go build ./cmd/engine ./cmd/desktop
```

Do not start Phase B until this is green.

---

### Later phases (do not start in the same breath as Task 1)

- **B:** Measure Generate/Check/Patch; freeze numbers after first printout; keep write-lock tests.
- **C:** Keep existing Word/Excel/PPT/PDF fixtures green; add `office.generate` chat-bridge test for `kind=pdf` (C6).
- **D:** Write **new** `docs/qa/office-closed-loop-protocol-2026-09-12.md` (do not reuse trial-ready protocol as D2 list); run on a **new** desktop binary.
- **E:** Sign PRD 1.1 only after CL01–CL33 + D2.

Plan complete. Execute Phase A task-by-task with TDD. Ask the user before starting if they want inline execution or a pause after Task 1.
