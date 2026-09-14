# All Development 100% Close-out Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Execute **one wave at a time**. Do not start Wave C until Wave B factory code tasks 1–5 are green, unless the user explicitly parallelizes model-office after T01. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every in-scope development gap so the project factory and the model-office engineering layer each reach their own 100%, then run Q/D as separate human/live gates.

**Architecture:** Two tracks, three completion contracts (Factory / E / Q+D). Stay on the current dirty `feat/prd-v7-s1-continuity` tree. Do not open a new worktree. Do not merge to `main`. Do not rewrite released `0154`–`0156`. Factory leftover and already-landed T01 / T13 / `0159` stay; do not revert them.

**Tech Stack:** Go 1.26, SQLite, React 19 / Vitest, `generate-bridge`, Windows desktop installer, Windows DPAPI.

**Spec:**
- Factory: `docs/design/PRD-project-factory-2026-09-13.md` + spine `docs/design/PRD-project-workbench-spine-2026-09-13.md`
- Factory remaining TDD: `docs/superpowers/plans/2026-09-13-factory-100-closeout.md`
- Model-office rebase: `docs/superpowers/specs/2026-09-13-model-office-upgrade-rebase-design.md`
- Model-office E TDD (T02+): `docs/superpowers/plans/2026-09-13-model-office-e-wave.md`
- Pack (read only, gitignored): `docs/design/feilunshengj/PRD.md`, `CONTRACTS.md`, `IMPLEMENTATION-PLAN.md`
- Binding audit: `docs/design/feilunshengj/IMPLEMENTATION-AUDIT-2026-09-13.md` §5

## Global Constraints

- Stay on this dirty tree. Do not revert uncommitted factory leftover, T01 freeze, T13 font policy, or `0159` FormalDecision.
- Do not commit unless the user asks. If a task says Commit, stage only that task’s files and wait if the user has not asked.
- After any Bridge schema edit: `npm --prefix web run generate:bridge`. Never hand-edit `web/src/generated/bridge.ts` or `internal/bridge/schema_generated.go`.
- Windows `go test` / npm need unrestricted permissions. PowerShell: no `&&`; use `;`.
- Do not commit `docs/design/feilunshengj/`, secrets, `web/dist`, `.release-cache`.
- Do not change poison / 月伴 TTS / 玉盘像素 / 星尘配方.
- Do not weld Hub into SessionPage. Do not use M6 as the interface platform. No auto git. SQLite only.
- Remap: model native → `0157_model_native_v2.sql`; execution → `0158_execution_contract_v2.sql`; office → `0159_office_delivery_v2.sql` (already created in the dirty tree).
- `0152_model_fit_qualification` stays fixture-only. Fixture pass ≠ live qualified. Code cannot mint “高端商用”.
- Factory 100% ≠ model-office 100% ≠ product 100%. Do not tell the user a mixed “产品 100%”.

---

## What “100%” means (do not mix)

| Contract | 100% is true only when | Code cannot claim |
| --- | --- | --- |
| **Factory** | F1–F5 §9 visible; §10.1 automation green; one §1.4 first-ship e2e on a **temp disk**; human walk recorded pass in `docs/audits/factory-first-ship.md` | Live Cursor/Codex, device farm, K8s, MySQL/Postgres, designer market, remote Git, auto CI |
| **Model-office E** | T01–T20 engineering gates green **and** `docs/audits/model-office-upgrade/baseline.json` `layers.E=done` | Live DeepSeek/GLM qualified; designer certificates; “高端商用” |
| **Q** | User-authorized live batches exist; `layers.Q` is no longer `not_run` | Any fixture or mock HTTP as live qualified |
| **D** | Human designers review the 12 core template combinations; certificates written by people | Tests writing `designerReviewed=1` |

“所有功能正常、所有缺口闭环” = Factory 100% **and** E 100% **and** Q run **and** D recorded. Waves C–E cannot finish in one sitting. Pack estimate remains ~145 person-days for the upgrade graph plus 2–4 days factory remaining plus human Q/D.

Out of all four contracts: K8s / customer production deploy, MySQL/Postgres, device farm, designer market, remote Git, auto CI, grok-build, ACP, nested TUI, auto git, Hub-in-SessionPage, M6 as interface platform, rewriting 0154–0156, committing the gitignored pack.

---

## Approaches considered

1. **One “just finish it” session** — rejected. Factory remaining is days; E is weeks; Q/D are not code.
2. **One 2000-line file that inlines every factory and T02–T20 TDD step** — rejected. Unreadable; two 100% gates would be mixed.
3. **This program + two executor plans** (chosen) — this file is the scoreboard and remaining-gap order. Factory file-level TDD stays in `2026-09-13-factory-100-closeout.md`. Model compile/ledger TDD stays in `2026-09-13-model-office-e-wave.md` starting at its Task 3 (T02). FormalDecision **production** leftovers are in this file as Wave C0 because they are not in those older files.

---

## Current tree (2026-09-13 night)

Pushed HEAD: `c93dd3f3` / `0.4.81`. Working tree is dirty. No commit unless asked.

### Factory — code mostly slice-complete, product not accepted

Landed in 0.4.81 + dirty leftover (keep): F1 generate/interview UI, F3 `emptyBoardAck` / `ErrEmptyBoardAck`, F5 `projectsync.InventoryTree` + flattened pack, `writeChecklistTx` writer gate, `deliverable.draft` filtered at turn assembly, `ErrBoardDirty` on advance, integration fail-by-source, WorkBoardPanel empty-board auto-sync, shell badges (`rulesDigest` → 规范已注入, `dbStatus==='ready'` → 库表 ready).

**Still missing for factory 100%:**

| Gap | Evidence | Close with |
| --- | --- | --- |
| `boardDirty` on checklist upsert | no `boardDirty` in app tests or DTO | factory-100 Task 1 |
| Personal-chat isolation lock | no `TestPersonalChatGenerateRequiresRoot` / `TestProjectFactoryGuidanceSkippedWithoutPhase` | factory-100 Task 2 |
| `db.query` no-target lock | no `TestDbQueryWithoutTargetFails` | factory-100 Task 3 |
| Portable CLI (`sh -c` on Unix) | `cmd /c` still Windows-shaped | factory-100 Task 4 |
| One §1.4 e2e | no `TestFactoryFirstShipImplementationWalk` | factory-100 Task 5 |
| Human live walk | no `docs/audits/factory-first-ship.md` | factory-100 Task 6 |

### Model-office — T01 / T13 min loop in the dirty tree; spine missing

| Landed (dirty tree, not “work package accepted”) | Still missing |
| --- | --- |
| T01 verify script: empty `--run`→2; missing name / `[` →1; inventory vs execute; Go `selectreg` | Full freeze labels (fonts version vs `"windows-fonts"`); T19 V2 key restore |
| T01 backup test: CreateBackup → mutate → RestoreBackup with session + office rows | 0157 still correctly absent |
| T13 font: Basic `Required:false`; Assured requires `font-actual`; inventory never `passed` | `QualityFor` still marks optional unsupported substitution `partial` |
| T13 persist: `0159` policies + decisions; accept / single export / bundle formal share `AssessDelivery` | `Check()` does **not** emit `file-integrity` / `source-content` / `locked-facts` — live Generate cannot Basic-verify without seeded rows |
| Historical Required no longer copied in `AddOfficeValidation` | First persist for `(version,SHA,policy)` wins; later evidence does not recompute |
| Alias collapse: `actual-render=failed` beats `native_render=unsupported` | `BlockingCodes` unused; no `office.artifact.assessDelivery` Bridge method |

**Not present in production code:** `CompileParameters`, `TargetDigest`, `0157_model_native_v2.sql`, `protocol_epochs_v2`, `0158_execution_contract_v2.sql`, admit-before-send (`continuity_wire.go` still `return run(emit)` after `PutCallAttemptIntent` fails), visual all-pages (`visual_model.go` still writes `pages[0]` and treats exit 0 as success), T14–T17 four-format oracles, T11 live qualification, T12 runner/oracles, Q, D.

Honest upgrade progress (this PRD as denominator, not the whole product): still **0/20 fully accepted**. T01 and T13 have real but incomplete loops. Do not convert “a few tests green” into a completion percentage. Management midpoint after tonight is still single-digit; the model/budget/quality spine has not started.

---

## File map (remaining work)

| File | Responsibility |
| --- | --- |
| `docs/audits/factory-first-ship.md` | Human factory walk |
| `api/bridge/v1/deliverable.upsert.schema.json` | `boardDirty` |
| `internal/app/project_factory_firstship_test.go` | §1.4 temp-disk walk |
| `internal/officeapp/service.go` `Check()` | Emit CONTRACTS Basic IDs |
| `internal/officeapp/delivery_gate.go` | Recompute FormalDecision when `evidence_digest` changes |
| `internal/modelfit/profile.go` | `ModelProfile`, `TargetDigest` |
| `internal/modelfit/parameters.go` | `CompileParameters` |
| `migrations/0157_model_native_v2.sql` | Profiles + later protocol ledger |
| `migrations/0158_execution_contract_v2.sql` | Task budget / outcome |
| `internal/app/continuity_wire.go` | Intent fail → **zero** HTTP |
| `internal/officestudio/visual_model.go` | All pages; exit 0 + empty JSON = fail |
| `scripts/verify-model-office-upgrade.mjs` | T20 FR01–FR30 evidence |
| `docs/audits/model-office-upgrade/baseline.json` | `layers.E/Q/D` |

---

## Graph

```text
Wave A stabilize
    → Wave B factory remaining (Tasks 1–5 code, Task 6 human)
    → Wave C0 FormalDecision production (Check IDs + recompute)
    → Wave C1  T02 → T03 → T04 → T05
                 └────── T06 → T07 → T08 → T09 → T10
    → Wave C2  T14 → T15 → T16 → T17 → T18
    → Wave C3  T11 → T12 ; T19 → T20 (needs C1+C2)
    → Wave D Q (keys + budget)
    → Wave E D (designers)
```

C0 may run in parallel with Wave B. T02 must not start until C0 keeps T01 backup + FormalDecision tests green. T13 font/persist already landed — do not re-dispatch them.

---

## Wave A — Stabilize the dirty tree

No new features. If anything is red, fix only the regression.

- [ ] **Step 1: Run the verification set**

```powershell
go test ./internal/projectgen ./internal/projectrules ./internal/projectschema ./internal/projectboard ./internal/projecttestkit ./internal/projectsync ./internal/modelquality -count=1
go test ./internal/officeapp -run "TestFormalDecision|TestFont|TestOfficeFont|TestAssessDelivery" -count=1
go test ./internal/app -run "TestOfficeFormalAcceptRequiresPassedQuality|TestFormalDecision" -count=1
go test ./internal/storage/sqlite -run TestUpgradeMigrationBackupCompatibility -count=1
npm --prefix web run generate:bridge -- --check
npx --prefix web vitest run src/project/WorkBoardPanel.test.tsx src/project/DeliverablePanel.test.tsx src/project/PhaseGenerateBar.test.tsx src/project/ProjectWorkbenchShell.test.tsx
npx --prefix web tsc --noEmit
node scripts/verify-model-office-upgrade.mjs --inventory
```

Expected: PASS. `0157_model_native_v2.sql` still must not exist. `0159_office_delivery_v2.sql` may exist.

- [ ] **Step 2: If red, fix only that package. Do not start Wave B/C.**
- [ ] **Step 3: Commit only if the user asked.**

---

## Wave B — Factory remaining (executor plan)

Execute `docs/superpowers/plans/2026-09-13-factory-100-closeout.md` Tasks 1–6 in order. Do not weaken `PROJECT_BOARD_DIRTY`, `PROJECT_DB_INCOMPLETE`, `PROJECT_SYNC_REQUIRED`, or `PROJECT_ROOT_REQUIRED`.

| Factory-100 task | First failing test | Done when |
| --- | --- | --- |
| 1 `boardDirty` | `TestDeliverableUpsertChecklistReportsBoardDirty` | upsert result has `boardDirty`; DeliverablePanel calls `board.sync` |
| 2 personal chat | `TestPersonalChatGenerateRequiresRoot`, `TestProjectFactoryGuidanceSkippedWithoutPhase`, `TestPersonalChatToolsOmitDeliverableDraft` | no generate without root; no `[项目规范]` at phase 0 / empty projectId |
| 3 `db.query` | `TestDbQueryWithoutTargetFails` | no target → `BRIDGE_SCHEMA_INVALID` |
| 4 CLI | `TestCLICommandUsesPortableShell` | Windows `cmd /c`, else `sh -c` |
| 5 first-ship e2e | `TestFactoryFirstShipImplementationWalk` | temp-disk create → generate 9+10 → approve → schema → boards → sync dest ≠ root |
| 6 human walk | `docs/audits/factory-first-ship.md` 12 steps recorded pass | only then say “项目管理工厂 100% 落地” |

Factory 100% is Tasks 1–5 green **and** Task 6 recorded pass. The Go e2e does not replace the live walk.

---

## Wave C0 — FormalDecision production (not yet in older plans)

Live Generate still cannot Basic-verify because `Check()` never writes `file-integrity` / `source-content` / `locked-facts`. First persist also freezes `needs_review` after evidence improves. This wave closes those holes so a user can generate a Word file and get a real Basic decision without test seeding.

**Files:**
- Modify: `internal/officeapp/service.go` (`Check`)
- Modify: `internal/officeapp/delivery_gate.go` (`AssessDelivery`, `persistFormalDecision`)
- Modify: `internal/storage/sqlite/office_delivery_v2.go` (`FindOfficeDeliveryDecision` — latest matching `evidence_digest`, or recompute-always)
- Modify: `internal/officeapp/delivery_gate_test.go`
- Keep: `TestFormalDecisionRequiredCoverage`, `TestHistoricalRequiredDoesNotPolluteCurrentPolicy`, `TestFormalDecisionAcceptExportBundleShareDecision`, `TestFormalDecisionFailedActualRenderNotHiddenByNativeRenderAlias`

**Interfaces:**
- Consumes: existing `Check` local issues, version SHA, `RegisteredRequiredCheckIDs`
- Produces: every persisted validation from `Check()` includes canonical IDs `file-integrity`, `source-content`, `locked-facts` (plus existing font / render IDs). `AssessDelivery` recomputes from latest SHA-matching reports; if `evidence_digest` changed, insert a new decision row and return that row (do not return the oldest stale `needs_review`). Failed required IDs go in `BlockingCodes`; `MissingChecks` stays for missing/unknown/unsupported/pending.

- [ ] **Step 1: Write the failing tests**

```go
func TestCheckEmitsFormalRequiredIDs(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "formal-ids")
	reports, err := store.ListOfficeValidations(context.Background(), v.ID)
	if err != nil || len(reports) == 0 {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range reports[0].Checks {
		seen[domain.CanonicalCheckID(c.ID)] = true
	}
	for _, id := range []string{"file-integrity", "source-content", "locked-facts", "font-availability"} {
		if !seen[id] {
			t.Fatalf("Check() missing %s in %+v", id, reports[0].Checks)
		}
	}
}

func TestFormalDecisionRecomputesWhenEvidenceImproves(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "recompute")
	first, err := svc.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
	if err != nil || first.Allowed {
		t.Fatalf("first assess should not allow incomplete evidence: %+v %v", first, err)
	}
	if _, err = store.AddOfficeValidation(context.Background(), domain.Validation{
		VersionID: v.ID, SHA256: v.SHA256, Validator: "later-complete",
		Checks: []domain.Check{
			{ID: "file-integrity", Status: "passed"},
			{ID: "source-content", Status: "passed"},
			{ID: "locked-facts", Status: "passed"},
			{ID: "font-availability", Status: "passed"},
			{ID: "actual-render", Status: "passed"},
			{ID: "font_actual_substitution", Status: "unsupported", Required: false},
		},
	}); err != nil {
		t.Fatal(err)
	}
	second, err := svc.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Allowed || second.State != "verified" {
		t.Fatalf("improved evidence must recompute, not reuse first persist: first=%+v second=%+v", first, second)
	}
	if second.DecisionID == "" || second.DecisionID == first.DecisionID {
		t.Fatalf("new evidence_digest must mint a new DecisionID: %q %q", first.DecisionID, second.DecisionID)
	}
}

func TestFormalDecisionFailedRequiredSetsBlockingCodes(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "blocked-codes")
	if _, err := store.AddOfficeValidation(context.Background(), domain.Validation{
		VersionID: v.ID, SHA256: v.SHA256, Validator: "fail-render",
		Checks: []domain.Check{
			{ID: "file-integrity", Status: "passed"},
			{ID: "source-content", Status: "passed"},
			{ID: "locked-facts", Status: "passed"},
			{ID: "font-availability", Status: "passed"},
			{ID: "actual-render", Status: "failed"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	dec, err := svc.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
	if err != nil || dec.State != "blocked" || dec.Allowed {
		t.Fatalf("%+v %v", dec, err)
	}
	found := false
	for _, id := range dec.BlockingCodes {
		if id == "actual-render" {
			found = true
		}
	}
	if !found {
		t.Fatalf("failed required must be BlockingCodes, not only MissingChecks: %+v", dec)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```powershell
go test ./internal/officeapp -run "TestCheckEmitsFormalRequiredIDs|TestFormalDecisionRecomputesWhenEvidenceImproves|TestFormalDecisionFailedRequiredSetsBlockingCodes" -count=1
```

Expected: FAIL — `Check()` lacks those IDs; second assess returns the first `DecisionID` / `needs_review`; `BlockingCodes` empty.

- [ ] **Step 3: Minimal implementation**

In `Check()`, after local/file scans, append (do not hide `unsupported` as `passed`):

```go
checks = append(checks,
	domain.Check{ID: "file-integrity", Label: "文件完整性", Required: true, Status: fileIntegrityStatus(v, b), Detail: fileIntegrityDetail(v, b)},
	domain.Check{ID: "source-content", Label: "源内容", Required: true, Status: sourceContentStatus(local), Detail: sourceContentDetail(local)},
	domain.Check{ID: "locked-facts", Label: "锁定事实", Required: true, Status: lockedFactsStatus(v, local), Detail: lockedFactsDetail(v, local)},
)
```

`file-integrity` passes only when SHA256 of `b` equals `v.SHA256` and size > 0. `source-content` passes when the existing local content scan did not fail required source predicates; otherwise `failed` or `needs_review`/`unsupported` honestly. `locked-facts` passes when every locked fact on the version is present in the extracted content; if the version has no locked facts, status is `passed` with detail “无锁定事实”. Do not mark `passed` from an empty scan.

`persistFormalDecision`: compute `evidenceDigest`; `Find` by `(version_id, source_sha256, policy_revision, evidence_digest)`; if a row exists return it; else insert. Never return an older digest for the same policy.

Map required `failed` → `BlockingCodes`; required missing/unsupported/pending → `MissingChecks`.

- [ ] **Step 4: Run tests**

```powershell
go test ./internal/officeapp -run "TestCheckEmits|TestFormalDecision|TestFont|TestOfficeFont|TestAssessDelivery" -count=1
go test ./internal/app -run "TestOfficeFormalAcceptRequiresPassedQuality|TestFormalDecision" -count=1
go test ./internal/storage/sqlite -run TestUpgradeMigrationBackupCompatibility -count=1
```

Expected: PASS. Do not flip `unsupported` to `passed`. Do not create `0157`/`0158`.

- [ ] **Step 5: Commit only if the user asked.**

```powershell
git add internal/officeapp/service.go internal/officeapp/delivery_gate.go internal/officeapp/delivery_gate_test.go internal/storage/sqlite/office_delivery_v2.go
git commit -m "fix(office): emit FormalDecision required checks and recompute when evidence changes."
```

---

## Wave C1 — Model compile + ledger + budget (T02–T10)

Execute `docs/superpowers/plans/2026-09-13-model-office-e-wave.md` from **Task 3 (T02)** onward. Do not re-run that file’s Task 1 (T01) or Task 2 (T13 font). Treat that file’s “T13 persist” row as **already landed**; remaining T13 work is Wave C0 above plus T14–T16 quality.

### C1.1 T02 Profile + TargetDigest (creates `0157`)

First tests (verbatim from the E-wave plan): `TestTargetDigestCanonicalAndRejectsSecrets`, `TestModelProfileBindingAlias`.

```powershell
go test ./internal/modelfit -run "Test(TargetDigestCanonicalAndRejectsSecrets|ModelProfileBindingAlias)" -count=1
```

Expected first run: FAIL — types undefined.

`0157` this slice = `model_profiles_v2` + `model_targets_v2` only. Update `store.go` manifest + `expectedSchemaSQL`. `TestUpgradeMigrationBackupCompatibility` may see 0157; must still refuse `0155_model_native*`. Profiles load as `declared`, never `qualified`. Reject endpoint userinfo and `api_key`/`key`/`token`/`secret` query keys.

### C1.2 T03 CompileParameters

First test: `TestCompileParametersMatrix`. GLM `clear_thinking: false` must survive encode (not `omitempty`). Unknown mode errors. Replace “strip thinking on 400” as the success path. No HTTP in this task.

```powershell
go test ./internal/modelfit ./internal/llmadapter -run TestCompileParametersMatrix -count=1
```

### C1.3 T04–T10 first locks

Read the matching pack section, write the named test **before** production code, remapped SQL only.

| ID | First test | Done when | Forbidden substitute |
| --- | --- | --- | --- |
| T04 | `TestProtocolCipherAADAndLegacyMigration` | `protocol_epochs_v2` encrypted; DPAPI via `internal/secret`; AAD binds owner+epoch | Re-label existing `protocol.go` AES as V2 complete |
| T05 | `TestNativeHistoryExactRoundTrip` | Replay exact bytes; incompatible model → new epoch, keep task/budget | Name-only model switch |
| T06 | `TestExecutionBudgetConcurrentParentChildReservation` | `0158_execution_contract_v2.sql`; parent/child reserve in one transaction | Old `generationBudget` only |
| T07 | `TestFinalInputPreflightEveryAttempt` **and** `TestCallAttemptIntentFailureDoesNotSendHTTP` | Chat/office/hub send Compile + budget BeforeSend; intent fail → HTTP count 0 | Log-and-`return run(emit)` |
| T08 | `TestArtifactSnapshotRawTail` | Workspace snapshot SHA is the raw file; Office blob SHA ≠ extract SHA | Paginated text as the file |
| T09 | `TestExecutionResumeUnknownEffect` | Unknown effect stays unknown; no double write; budget not reset | “Looks resumed” from `native_session_id` |
| T10 | `TestModelFitAndOfficeOutcomeUI` (task half) | Spinner ends; missing file ≠ green complete | Bridge success with empty path |

T07 HTTP-zero test (write this even if the pack names only preflight):

```go
func TestCallAttemptIntentFailureDoesNotSendHTTP(t *testing.T) {
	var sends int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[]}`))}, nil
	})
	// wire adapter with transport; store.PutCallAttemptIntent returns error
	_, err := startChatTurnThatWouldCallModel(t, transport)
	if sends != 0 {
		t.Fatalf("intent persist failed but HTTP sent %d times", sends)
	}
	if err == nil {
		t.Fatal("expected admit error, not a silent generate")
	}
}
```

Put the helper next to the existing continuity / chat generation tests. If the current `continuity_wire.go` path is the production sender, that is the function under test — after `PutCallAttemptIntent` error it must **not** call `run(emit)`.

After any migration: `go test ./internal/storage/sqlite -run TestUpgradeMigrationBackupCompatibility -count=1`.

---

## Wave C2 — Four-format quality + UI (T14–T18)

T13 font + persist already exist. This wave makes FormalDecision honest for real files.

| ID | First test | Done when | Forbidden substitute |
| --- | --- | --- | --- |
| T14 | `TestVisualManifestRejectsFirstPageOnly` + `TestPPTEditableObjectsAndCoverage` | Manifest covers `0..pageCount-1`; exit 0 + empty JSON = fail; locked facts survive fit | Loop `pages[0]` only; treat process exit 0 as pass |
| T15 | `TestDOCXLongTableAndFieldRefresh`, `TestXLSXIndependentOracleAndTypes` | TOC/fields refresh; Excel types + independent oracle; no invented revenue | Screenshot-only |
| T16 | `TestPDFParseAllPagesAndSameSource` | All pages parsed; F03 missing-glyph ≠ success; same-source PDF binds version SHA | First page only |
| T17 | `TestOfficePatchCASAndBundleAtomicity` | CAS patch; Excel ok + PPT fail does **not** publish the bundle | Partial bundle as success |
| T18 | `TestModelFitAndOfficeOutcomeUI`, `TestExternalExecutorArtifactVerification` | UI / single export / bundle show the same FormalDecision; Hub copy error is not silent success | Hide buttons only |

T14 first test (current bug: `pages[0]` + exit 0):

```go
func TestVisualManifestRejectsFirstPageOnly(t *testing.T) {
	man := VisualManifest{PageCount: 3, PageDigests: []string{"aaa"}}
	if err := ValidateVisualManifest(man, 3); err == nil {
		t.Fatal("one digest for three pages must fail")
	}
	if err := ValidateVisualProcess(0, "", nil); err == nil {
		t.Fatal("exit 0 with empty JSON must fail")
	}
}
```

Change `internal/officestudio/visual_model.go` so it writes every page and parses stdout JSON (max 1 MiB). Caller in `native_checks.go` must pass per-page images when it claims page coverage — fixing the loop alone is not enough.

---

## Wave C3 — Qualify, eval, migrate, release (T11–T12, T19–T20)

| ID | First test | Done when | Forbidden substitute |
| --- | --- | --- | --- |
| T11 | `TestQualificationFixtureCannotPromote` | 0152 fixture rows cannot become live `qualified`; activate is CAS | `fixture_pass` → qualified |
| T12 | `TestEvalSuiteAllCasesHaveIndependentOracle` | 24 case IDs each have an oracle; hold-out listed | Case list printed as “eval complete” |
| T19 | `TestProtocolMigrationCannotResurrectDeletedSession` | Backup/restore of V2 keys + deleted session stays deleted | T01 office-row restore as T19 done |
| T20 | `TestUpgradeReleaseEvidenceComplete` | verify script requires FR01–FR30 evidence; `layers.E=done` | Inventory-only `ok: true` |

E 100% = every C0–C3 first test green **and** `layers.E=done`. Then stop coding the upgrade graph.

---

## Wave D — Q live batches

Only after the user provides keys and a budget.

- [ ] Confirm `layers.E=done`. *(correctly left open: Q ran without writing E=done)*
- [x] Run the 24-case live suite against user-authorized DeepSeek / GLM endpoints. *(1 rep; DeepSeek `deepseek-v4-pro` LiveSuccess=4; GLM `glm-5.3` LiveSuccess=2; office mostly fail)*
- [x] Write evidence under `docs/audits/model-office-upgrade/` (no secrets). (`q-live-evidence.json`)
- [x] Set `layers.Q` from `not_run` to the real result. Never write `live qualified` from fixtures. (`layers.Q=ran`, `liveQualified=false`)

---

## Wave E — D designer review

Human only.

- [x] 12 semantic layouts generated locally (`ops-clear` / `standard`) as uncertified samples. Not CONTRACTS 4×3 first-ship IDs; not human D.
- [ ] Three independent scores. `designerReviewed=0` stays uncertified.
- [x] Tests must not insert certificates. Missing visual evidence stays `unsupported` / `needs_review`. (`d-designer-evidence.json`: `designerReviewed=0`, `layers.D=not_run`)

---

## Scoreboard (update after each accepted wave)

| Package | Status now | Next evidence |
| --- | --- | --- |
| Factory F1–F5 code | Wave B Tasks 1–5 reviewed | — |
| Factory product 100% | Yes (2026-09-14 walk pass) | factory contract only |
| T01–T20 first locks + missing FR named tests | Reviewed on dirty tree | `layers.E` stays pending |
| Q | `ran` (not live_qualified) | C01 harness fixed; no 3× re-run |
| D | `not_run` (12 samples only) | Human 3-score review |

---

## Self-review

| Spec requirement | Task |
| --- | --- |
| Factory §10.1 leftover (personal chat, first-ship, boardDirty, db.query, CLI) | Wave B → factory-100 Tasks 1–6 |
| Factory §1.4 12-step scene | Wave B Task 5 + 6 |
| Audit §5 row 1 T01 gates | Landed; Wave A re-verify |
| Audit §5 row 2 FormalDecision user loop | Landed persist; Wave C0 makes it true on Generate |
| Audit §5 row 3 T02–T05 | Wave C1 |
| Audit §5 row 4 T06–T10 + intent-fail HTTP=0 | Wave C1 T06–T10 |
| Audit §5 row 5 T14–T17 all pages / oracles | Wave C2 |
| Audit §5 row 6 T11–T12 T19–T20 Q/D | Wave C3 + D + E |
| FR01–FR30 | T02–T20 rows above |
| CONTRACTS §7 FormalDecision rules | Wave C0 + existing persist tests |
| CONTRACTS §8 export mode | Already in dirty tree; keep `TestOfficeFormalAcceptRequiresPassedQuality` |
| No 0154–0156 rewrite | Global constraint |
| Q/D not code-mintable | Waves D/E |

Placeholder scan: none. “Similar to Task N” not used for implementation steps. Factory file-level code lives in the factory-100 plan so this file stays the scoreboard.

Type names used later match CONTRACTS / E-wave: `ModelProfile`, `TargetIdentity`, `TargetDigest`, `CompileParameters`, `PreparedRequest`, `FormalDecision`, `DeliveryPolicy`.
