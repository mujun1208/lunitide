# Office Capability Pack 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Stay on `feat/prd-v7-s1-continuity`. No worktree. No finishing-a-development-branch. Do not commit unless the user asks.

**Progress:** G1–G3 done. Capability pack 1 = 100% of its own scope. First-ship FR01–FR16 still ~83%. Full HTML PRD still not 100%.

**Goal:** Land glyph measurement, fail-closed visual-model wiring, and honest PDF/A validation so this pack is 100% of its own scope.

**Architecture:** Keep generate → backends → Check/Patch. Glyph is a `TextMeasure`. Visual-model and PDF/A are checks. Unsupported stays skippable for Formal; passed/failed only after a real run.

**Tech Stack:** Go `internal/officestudio`, `internal/officeapp`.

**Spec:** [docs/superpowers/specs/2026-09-12-office-capability-pack1.md](../specs/2026-09-12-office-capability-pack1.md)

## Global Constraints

- Stay on `feat/prd-v7-s1-continuity`. No commit / push / VERSION / pack unless the user asks.
- Frozen `token_ledger`; `html.gen` stays `penalty-shootout|timer|checklist`.
- Do not invent savings %, 机密, 管理层 defaults, or competitor scores.
- `designerReviewed` stays `0`. Do not put Presenton on Generate.
- Formal still skips honest unsupported `target-*` / `visual-model` / `pdfa`.
- Live app stays 0.4.75 until rebuild. FR17/FR18 stay no.
- Do not edit `c:\Users\mujun\.cursor\plans\office_quality_commercial_a4a3b6e5.plan.md`.
- Windows tests need `required_permissions: ["all"]`.

---

### Task 1: Glyph measure

**Files:** `internal/officestudio/glyph.go`, `glyph_windows.go`, `glyph_test.go`, `layoutplan.go`, `prepare.go`

- [ ] Failing tests: injected extent → `measure=glyph`; failed extent → EstimateMeasure / `not glyph`; never `glyph-verified` without extent
- [ ] `go test ./internal/officestudio -count=1 -timeout 60s -run "TestGlyphMeasure|TestEstimateMeasure"`
- [ ] Implement `GlyphMeasure`, `DefaultTextMeasure`; `applyLayoutPlanning` uses `DefaultTextMeasure(brand.Fonts.East)`
- [ ] Pass same tests plus existing estimate test

### Task 2: Visual model check

**Files:** `internal/officestudio/visual_model.go`, `visual_model_test.go`, `internal/officeapp/native_checks.go`

- [ ] Failing tests: unconfigured unsupported; configured without pages not passed; review success passed + uncalibrated; review issues failed
- [ ] Implement `VisualModelCheck`; coverage check uses it (`LUNITIDE_OFFICE_VISION` means configured)
- [ ] `go test ./internal/officestudio ./internal/officeapp -count=1 -timeout 90s -run "TestVisualModel|TestTargetAppCoverage"`

### Task 3: PDF/A validator

**Files:** `internal/officestudio/adapter.go`, `patch.go`, `wave11_test.go` or new test

- [ ] Failing tests: no env unsupported; env + no PDF missing; fake runner fail/pass
- [ ] `IndependentPDFACheck` reads `LUNITIDE_PDFA_VALIDATOR`; `Validate` passes PDF bytes
- [ ] Existing Typst/PDF tests still refuse “已符合 PDF/A” without validator
- [ ] `go test ./internal/officestudio -count=1 -timeout 60s -run "TestIndependentPDF|TestValidatePDF|TestPDFA"`

### Task 4: Gate and board

```
go test ./internal/officestudio ./internal/officetools ./internal/officeapp ./internal/officerender ./internal/domain/officestudio
go test ./internal/app -run "TestOffice|TestStudio|TestGenerate"
npx vitest run src/officeStudio
go build ./cmd/engine ./cmd/desktop
```

Update canvas: pack G1–G3 100%; first-ship still ~91–94%; full PRD still not 100%. No 真机已修.
