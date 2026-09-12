# Office Usability Pack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans and test-driven-development. Stay on `feat/prd-v7-s1-continuity`. No worktree. No finishing-a-development-branch. Do not commit unless the user asks.

**Goal:** 可用度 100% = 每个可选能力都有可用路径。不是首发工程产品验收 100%，也不是整份 HTML PRD 100%。

**Spec:** [docs/superpowers/specs/2026-09-12-office-usability-pack.md](../specs/2026-09-12-office-usability-pack.md)

## Global Constraints

- Stay on `feat/prd-v7-s1-continuity`. No commit / push / VERSION / pack unless the user asks.
- Frozen `token_ledger`; `html.gen` stays `penalty-shootout|timer|checklist`.
- Do not invent savings %, 机密, 管理层 defaults, or competitor scores.
- `designerReviewed` stays `0`. Do not put Presenton on Generate.
- Formal still skips honest unsupported/missing `target-*` / `visual-model` / `pdfa`.
- Live app stays 0.4.75 until rebuild. FR17/FR18 stay no.
- Do not edit `c:\Users\mujun\.cursor\plans\office_quality_commercial_a4a3b6e5.plan.md`.
- Windows tests need `required_permissions: ["all"]`.

---

### Task 1: U1/U2 Check 按文件实跑

**Files:** `internal/officeapp/native_checks.go`, `service.go`, `internal/officestudio/visual_model.go`

- [x] Failing tests: configured + no PDF keeps `missing` (not remapped to `unsupported`); configured + PDF + injected runner can `passed`/`failed`; visual-model without pages is `missing`
- [x] `applyUsabilityChecks` upserts `pdfa` + `visual-model` from current PDF bytes
- [x] `Check` remaps `missing`→`unsupported` except `pdfa` / `visual-model`
- [x] `RunConfiguredVisualReview` injectable; unit tests do not exec a real model

### Task 2: U3/U4 Studio 可用范围

**Files:** `web/src/officeStudio/officeQualityUi.ts`, `OfficeStudioPage.tsx`, `OfficeInspector.tsx`

- [x] Failing tests: `usabilityScopeNotice` 写明未配置仍可用、外部生成器未进主链、FR17/FR18 不做
- [x] `capabilityUsabilityLabels` 按检查状态；视觉 `passed` 不写 85 认证
- [x] Studio 展示可用范围；检查区展示可用状态

### Task 3: Gate and board ✅

```
go test ./internal/officestudio ./internal/officetools ./internal/officeapp ./internal/officerender ./internal/domain/officestudio
go test ./internal/app -run "TestOffice|TestStudio|TestGenerate"
npx vitest run src/officeStudio
go build ./cmd/engine ./cmd/desktop
```

Update canvas: usability pack U1–U4 100%；首发工程 / 整份 PRD 仍不是 100%。No 真机已修.
