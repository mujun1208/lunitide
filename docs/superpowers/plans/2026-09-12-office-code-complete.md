# Office Code-Complete Remainder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Stay on `feat/prd-v7-s1-continuity`. No worktree. No finishing-a-development-branch. Do not commit unless the user asks.

**Progress:** C1–C5 done. Code-complete pack 5/5 = 100%. Full HTML PRD product-验收 still ~60%.

**Goal:** Land the remaining engineer-completable Office gaps, excluding human trial, so this remainder is 100% of its own scope.

**Architecture:** Keep generate → backends → Check/Patch. Confidentiality and brief evidence stay fail-closed. Quality chips only light from existing validations.

**Tech Stack:** Go `internal/officeapp`, `internal/app`, `internal/officestudio`; Vitest `web/src/officeStudio`.

**Spec:** [docs/superpowers/specs/2026-09-12-office-code-complete.md](../specs/2026-09-12-office-code-complete.md)

## Global Constraints

- Stay on `feat/prd-v7-s1-continuity`. No commit / push / VERSION / pack unless the user asks.
- Frozen `token_ledger`; `html.gen` stays `penalty-shootout|timer|checklist`.
- Do not invent savings %, 机密, 管理层 defaults in snapshot/evidence, or competitor scores.
- `designerReviewed` stays `0`. Formal stays blockers-only.
- Live app stays 0.4.75 until rebuild. FR17/FR18 stay no.
- Do not edit `c:\Users\mujun\.cursor\plans\office_quality_commercial_a4a3b6e5.plan.md`.
- Windows tests need `required_permissions: ["all"]`.

---

### Task 1: Four-format confidentiality

**Files:**
- Modify: `internal/officeapp/planning.go`
- Test: `internal/officeapp/planning_test.go`

- [ ] **Step 1: Write failing tests** `TestApplyTaskBriefWritesPPTAndExcelConfidentialityWithoutInventing`

PPT one slide: authored `内部` appears in `Notes`; empty brief does not write `机密`.  
Excel `说明` first cell gets `密级：内部`; `原始数据` header stays free of `机密`.

- [ ] **Step 2: Run to fail** `go test ./internal/officeapp -count=1 -timeout 60s -run TestApplyTaskBriefWritesPPTAndExcelConfidentialityWithoutInventing`

- [ ] **Step 3: Minimal impl** After narrative plan, use **raw** confidentiality. PPT: append `密级：…` to each slide Notes if missing. Excel: only the `说明` sheet first text cell. Word/PDF paths unchanged.

- [ ] **Step 4: Pass** `go test ./internal/officeapp -count=1 -timeout 60s -run "TestApplyTaskBrief"`

---

### Task 2: Chat evidence uses raw brief

**Files:**
- Modify: `internal/officeapp/planning.go` (`FormatBriefEvidence`)
- Modify: `internal/app/office_context.go`
- Test: `internal/officeapp/planning_test.go`, `internal/app/office_context_test.go`

- [ ] **Step 1: Failing tests** Empty brief → evidence contains `audience unset`, not `audience=管理层`. Authored `内部` appears; `机密` does not.

- [ ] **Step 2: Fail** `go test ./internal/officeapp ./internal/app -count=1 -timeout 90s -run "TestFormatBriefEvidence|TestOfficeChatEvidenceDoesNotInventBriefDefaults|TestOfficeChatEvidenceIncludesBriefFacts"`

- [ ] **Step 3: Impl** `FormatBriefEvidence(raw)` lists only authored fields; unset written as unset. `officeChatEvidence` uses raw BriefFromCheckpoint, not NormalizeBrief.

- [ ] **Step 4: Pass** same command plus existing brief-facts test.

---

### Task 3: License pack load validates

**Files:**
- Modify: `internal/officestudio/license_pack.go`
- Test: `internal/officestudio/wave11_test.go`

- [ ] **Step 1: Failing test** Temp JSON `reviewed:true, items:[]` → `LoadLicensePackChecklist` errors.

- [ ] **Step 2: Fail** `go test ./internal/officestudio -count=1 -timeout 60s -run TestLoadLicensePackChecklistRejectsReviewedEmpty`

- [ ] **Step 3: Impl** After unmarshal, `pack.Validate()`.

- [ ] **Step 4: Pass** `go test ./internal/officestudio -count=1 -timeout 60s -run "TestLoadLicensePackChecklist|TestLicensePackChecklistIsUnreviewed"`

---

### Task 4: §7.3 quality promise chips

**Files:**
- Modify: `web/src/officeStudio/officeQualityUi.ts`, `OfficeInspector.tsx`
- Test: `officeQualityUi.test.ts`, `OfficeStudioPage.test.tsx`

- [ ] **Step 1: Failing tests** `qualityPromiseLabels` on fixture-like version: includes `内容完整` `可继续编辑` `存在需处理的问题`; excludes `排版已检查` when layout is unavailable. Studio 检查 tab shows those strings.

- [ ] **Step 2: Fail** vitest those two files.

- [ ] **Step 3: Impl** Conservative chips from existing validations only. Render under check summary.

- [ ] **Step 4: Pass** same vitest.

---

### Task 5: Brief contract includes confidentiality and outline

**Files:**
- Modify: `api/bridge/v1/public.dto.schema.json`
- Test: `internal/contract/office_brief_schema_test.go`

- [x] **Step 1:** Failing test that `OfficeBriefDTO` must declare `confidentiality` and `outline`
- [x] **Step 2:** Add `OfficeNarrativeNodeDTO` plus those Brief fields; regenerate bridge
- [x] **Step 3:** `go test ./internal/contract -run TestOfficeBriefDTOAcceptsAuthoredConfidentialityAndOutline`

---

### Task 6: Gate and board

Run office/app/vitest/build. Update canvas: remainder C1–C5 100%; full PRD still ~60%. No 真机已修.
