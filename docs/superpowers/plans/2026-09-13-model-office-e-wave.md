# Model-office E-layer Wave Implementation Plan

> **Progress (dirty tree, 2026-09-13 night):** Task 1 (T01 gates) and Task 2 (T13 font) are landed. FormalDecision persist / `0159` is landed. Do **not** re-dispatch those tasks. Remaining T13 production (`Check()` IDs + recompute) is Wave C0 in `docs/superpowers/plans/2026-09-13-all-dev-100-closeout.md`. Resume this file at **Task 3 (T02)**.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. TDD. Do not reuse migrations 0155/0156. Do not start T02 until Task 1 is green. T13 may run in parallel with T02 after Task 1. Binding T01 acceptance: `docs/design/feilunshengj/IMPLEMENTATION-AUDIT-2026-09-13.html` §2 + CONTRACTS §13.7 (empty `--run` is not enough; execute `go test -json`; backup test must restore session/office rows).

**Goal:** Land the feilunshengj v2 **engineering** layer on the real 0.4.81 tree so FR01–FR30 have code + offline tests, without colliding with the project factory.

**Architecture:** Extend `modelfit`, `llmadapter`, `agentrun`/`agentrunapp`, `officeapp`/`officestudio`, and `storage/sqlite`. One FormalDecision, encrypted native ledger, admit-before-send budget, explicit DeepSeek/GLM profiles. Do not start a second agent framework. Do not treat factory 0155/0156 as unused numbers.

**Tech Stack:** Go 1.26, SQLite, React 19, Vitest, current OOXML/Office renderer, Windows DPAPI.

**Spec:** `docs/superpowers/specs/2026-09-13-model-office-upgrade-rebase-design.md` plus pack `docs/design/feilunshengj/PRD.md` / `CONTRACTS.md` / `IMPLEMENTATION-PLAN.md` (read; do not execute stale 0155–0157 names). Program: `docs/superpowers/plans/2026-09-13-full-dev-closeout.md`.

## Global Constraints

- Remap: model native → `0157_model_native_v2.sql`; execution → `0158_execution_contract_v2.sql`; office → `0159_office_delivery_v2.sql`.
- Do not edit released 0154–0156 bodies or checksums.
- Do not commit `docs/design/feilunshengj/` (gitignored; may contain Edge profiles).
- Copy fixtures only into `internal/modelquality/testdata/` and `docs/audits/model-office-upgrade/`.
- `0152_model_fit_qualification` stays fixture-only (`untested` / `fixture_pass` / `blocked`). Fixture pass ≠ live qualified.
- Current `llmadapter` stripping unknown thinking fields on 400 is compatibility, not GLM native compile. T03 replaces that path.
- Code cannot mint “高端商用”. Designer certificates are Wave E (human).
- Do not change poison / 月伴 TTS / 玉盘像素 / 星尘配方.
- After Bridge schema edits: `npm --prefix web run generate:bridge`.
- Do not commit unless the user asks.
- Windows `go test` / npm need unrestricted permissions.

## File map (first executable slices)

| File | Responsibility |
| --- | --- |
| `docs/audits/model-office-upgrade/baseline.json` | T01 freeze record |
| `scripts/verify-model-office-upgrade.mjs` | T01/T20 evidence gate |
| `internal/modelquality/testdata/upgrade-v1/` | 24 unique case IDs + profile examples |
| `internal/storage/sqlite/upgrade_schema.go` | Refuse unknown / stale pack numbers |
| `internal/officeapp/font_checks.go` | Basic vs Assured required flags |
| `internal/domain/officestudio/delivery_policy.go` | `DeliveryPolicy`, `FormalDecision` |
| `internal/officeapp/delivery_gate.go` | `AssessDelivery` |
| `internal/modelfit/profile.go` | `ModelProfile`, `CompileParameters`, `TargetDigest` |
| `migrations/0157_model_native_v2.sql` | Created in T02/T04, **not** T01 |

## Graph

```text
T01 → T02 → T03 → T04 → T05
           └────── T06 → T07 → T08 → T09 → T10
T02 + T05 + T06 → T11 → T12
T01 → T13 → T14 / T15 → T16 → T17 → T18
T05 + T09 + T11 + T16 + T17 → T19
T10 + T12 + T18 + T19 → T20
```

---

### Task 1: Close T01 gates (PRD audit 2026-09-13)

T01 is **partially done**, not accepted. Binding review: `docs/design/feilunshengj/IMPLEMENTATION-AUDIT-2026-09-13.html` §2 and CONTRACTS §13.7. Do **not** create `0157` SQL. Do **not** implement FormalDecision / CompileParameters. Do **not** count `store.validateJournal` as a new T01 feature.

**Files:**
- Modify: `scripts/verify-model-office-upgrade.mjs`
- Modify: `internal/modelquality/regression_selection.go` (keep `SelectRegressionTests` as the only matcher)
- Create if needed: `internal/modelquality/cmd/selectreg/main.go` — prints selected names or exits 2/1
- Modify: `internal/storage/sqlite/upgrade_compatibility_test.go`
- Modify: `docs/audits/model-office-upgrade/baseline.json`
- Modify: `docs/audits/model-office-upgrade/traceability.json` (`baselineCommit` must equal 0.4.81 HEAD)

**Interfaces:**
- Consumes: `SelectRegressionTests(names []string, pattern string) ([]string, error)`
- Consumes: `(*Store).CreateBackup`, `(*Store).RestoreBackup`
- Produces: verify script inventory vs execute. Execute parses `go test -json` and requires named tests `run`+`pass`.
- Exit codes: empty `--run` → **2**; illegal regex / zero matches / skip-only / package fail / test fail / missing expected Test → **1**; `--inventory` success prints `mode: "inventory"` and must not claim tests ran.

- [ ] **Step 1: Prove today’s fake success, then write the failing backup test**

From repo root with a real argv array (do not let the shell drop empty `--run`):

```powershell
node --input-type=module -e "import {spawnSync} from 'node:child_process'; const r=spawnSync('node',['scripts/verify-model-office-upgrade.mjs','--run','^TestCertainlyDoesNotExist_20260913$'],{encoding:'utf8'}); console.log('exit',r.status); console.log(r.stdout)"
node --input-type=module -e "import {spawnSync} from 'node:child_process'; const r=spawnSync('node',['scripts/verify-model-office-upgrade.mjs','--run','['],{encoding:'utf8'}); console.log('exit',r.status); console.log(r.stdout)"
```

Expected RED: both currently exit **0** and print `ok: true`.

Replace `TestUpgradeMigrationBackupCompatibility` so it uses production backup/restore and keeps journal-name asserts. Seed a session row and at least one `office_version_metadata` row through the same API `office_studio_test.go` uses. Record title + office count (and one content digest if a blob exists) before backup, mutate live, restore, reopen, assert the old title and office count return. Keep: factory 0155/0156 present, no `0157_model_native_v2.sql`, stale pack names absent, `RefuseUnknownUpgradeSchema([]string{"0160_future.sql"})` → `ErrUnknownSchema`. Full V2 key recovery stays T19.

```go
backup := filepath.Join(dir, "snap.db")
if err = store.CreateBackup(ctx, backup); err != nil {
	t.Fatal(err)
}
if _, err = store.db.ExecContext(ctx, `UPDATE sessions SET title='mutated-after-backup' WHERE id=?`, sessionID); err != nil {
	t.Fatal(err)
}
if err = store.RestoreBackup(ctx, backup); err != nil {
	t.Fatal(err)
}
```

`CreateBackup` requires an absolute `.db` destination. `RestoreBackup` closes the store; reopen with `OpenTemplated`.

- [ ] **Step 2: Confirm the backup test is RED before implementing**

```powershell
go test ./internal/storage/sqlite -run TestUpgradeMigrationBackupCompatibility -count=1
```

Expected: FAIL until CreateBackup/RestoreBackup and restored session title are in the test body and passing.

- [ ] **Step 3: Implement the gate**

Verify script, in order:

1. Inventory (always): reserved 0157–0159; no stale pack filenames under `migrations/`; no `0157_model_native_v2.sql` yet; 24 unique case IDs; FR01–FR30 present; `traceability.json` `baselineCommit` equals `baseline.json` `commit` (`c93dd3f37bc17f34cb828ded121a14e5c098ed4c`); each eval case sha256 is listed in `baseline.caseDigests`; profile-examples has ≥3 profiles (DeepSeek + GLM standard + GLM Coding).
2. `--inventory`: print `{ "ok": true, "mode": "inventory" }` and exit 0. Do not claim tests ran.
3. `--run` present and `""`: exit **2**.
4. Else execute: `go test <pkgs> -list .` → **Go** `SelectRegressionTests` (via `go run ./internal/modelquality/cmd/selectreg`, do not reimplement the matcher only in Node) → `go test -json <pkgs> -run <pattern> -count=1`. Parse events: every selected name must `run` and `pass`; skip-only / missing / fail → exit **1**. Invalid pattern or zero matches → non-zero, never `ok: true`.
5. Allowed packages: `./internal/modelquality` `./internal/storage/sqlite`.

Default execute `--run`: `Test(RegressionSelectionRejectsZeroMatches|UpgradeMigrationBackupCompatibility)`.

`selectreg`: `-pattern` + names on stdin or `-names-file`; print selected names; exit 2 on empty pattern; exit 1 on invalid regex or zero matches.

baseline.json additions (paths + digests only, no secrets): `workspace.dirtyPathsDigest` (sha256 of `git status --porcelain`), `toolchain.fonts` / `toolchain.libreOffice` (version or `"missing"`), `caseDigests`, `traceabilityCommit` == `commit`, `layers.E` stays `"pending"`. Do not claim a user production DB is on 0156.

- [ ] **Step 4: GREEN + four script probes**

```powershell
go test ./internal/modelquality ./internal/storage/sqlite -run 'Test(RegressionSelectionRejectsZeroMatches|UpgradeMigrationBackupCompatibility)' -count=1
node --input-type=module -e "import {spawnSync} from 'node:child_process'; for (const args of [['--run',''],['--run','^TestCertainlyDoesNotExist_20260913$'],['--run','['],['--inventory'],[]]) { const r=spawnSync('node',['scripts/verify-model-office-upgrade.mjs',...args],{encoding:'utf8'}); console.log(JSON.stringify({args,status:r.status,out:(r.stdout||'').slice(0,240),err:(r.stderr||'').slice(0,240)})) }"
```

| args | exit |
| --- | ---: |
| `--run` `""` | 2 |
| `--run` `^TestCertainlyDoesNotExist_20260913$` | 1 |
| `--run` `[` | 1 or 2 |
| `--inventory` | 0 and `mode` is `inventory` |
| default | 0 and JSON lists passed Go tests, not inventory-only |

- [ ] **Step 5: Do not commit** unless the user asked. Leave the tree dirty.

---

### Task 2: T13 font Basic / Assured (user-visible hole)

**Files:**
- Create: `internal/domain/officestudio/delivery_policy.go`
- Modify: `internal/officeapp/font_checks.go`, `internal/officeapp/font_checks_test.go`
- Create: `internal/officeapp/delivery_gate.go`, `internal/officeapp/delivery_gate_test.go` (AssessDelivery can be a stub that only applies font required-flags in this task)
- Do **not** create `0159` until Task 3 of this plan (FormalDecision persist). This task is the independent font gate.

**Interfaces:**
- Consumes: existing `domain.Check`, `officerender.FontReport`
- Produces:

```go
// internal/domain/officestudio/delivery_policy.go
package officestudio

type DeliveryPolicy struct {
    Revision       string
    TargetRenderer string
    Tier           string // basic / assured
    RequiredCheckIDs []string
    RequireEditable, RequireSearchable, RequireVisualReview bool
}

func FontActualRequired(tier string) bool { return tier == "assured" }
```

`fontChecks` must set `font_actual_substitution.Required` from the policy tier, **not** from the legacy hardcoded `Required: true`. Status stays `unsupported` until a real substitution report exists. Basic formal path must not inherit legacy Required.

- [ ] **Step 1: Write the failing tests**

```go
func TestFontPolicyDoesNotInheritLegacyRequired(t *testing.T) {
	svc, _, _ := studioServiceFixture(t)
	basic := domain.DeliveryPolicy{Tier: "basic", Revision: "v2-basic"}
	checks, _ := svc.fontChecksForPolicy(context.Background(), "docx", []byte("PK"), basic)
	var actual domain.Check
	for _, c := range checks {
		if c.ID == "font_actual_substitution" {
			actual = c
		}
	}
	if actual.ID == "" {
		t.Fatal("missing font_actual_substitution")
	}
	if actual.Required {
		t.Fatal("basic must not require font_actual_substitution")
	}
	if actual.Status == "passed" {
		t.Fatal("inventory must not mint substitution proof")
	}

	assured := domain.DeliveryPolicy{Tier: "assured", Revision: "v2-assured"}
	achecks, _ := svc.fontChecksForPolicy(context.Background(), "docx", []byte("PK"), assured)
	for _, c := range achecks {
		if c.ID == "font_actual_substitution" && !c.Required {
			t.Fatal("assured must require font_actual_substitution")
		}
		if c.ID == "font_actual_substitution" && c.Status == "passed" {
			t.Fatal("assured unsupported must not become passed")
		}
	}
}
```

Keep `TestOfficeFontEvidencePersistsWithoutOverclaimingNativeLayout` — it must still fail if status is `passed`.

- [ ] **Step 2: Run tests to verify they fail**

```powershell
go test ./internal/officeapp -run 'TestFontPolicyDoesNotInheritLegacyRequired|TestOfficeFontEvidencePersistsWithoutOverclaimingNativeLayout' -count=1
```

Expected: FAIL — `fontChecksForPolicy` undefined; current `font_actual_substitution` is `Required: true` for every call.

- [ ] **Step 3: Write minimal implementation**

```go
func (s *Service) fontChecksForPolicy(ctx context.Context, kind string, data []byte, policy domain.DeliveryPolicy) ([]domain.Check, *officerender.FontReport) {
	checks, report := s.fontChecks(ctx, kind, data)
	required := domain.FontActualRequired(policy.Tier)
	for i := range checks {
		if checks[i].ID == "font_actual_substitution" {
			checks[i].Required = required
			if checks[i].Status == "passed" {
				checks[i].Status = "unsupported"
			}
		}
	}
	return checks, report
}
```

Change `fontChecks` so the actual-substitution check is created with `Required: false` by default (Basic). Callers that still use `fontChecks` without a policy stay Basic. Do not mark substitution `passed` from inventory alone.

Wire existing generate/validate paths that persist office checks to use `fontChecksForPolicy` with the task’s stored tier if present; if no policy exists yet, use `Tier: "basic"`.

- [ ] **Step 4: Run tests**

```powershell
go test ./internal/officeapp -run 'TestFont|TestOfficeFont' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit (only if the user asked)**

```powershell
git add internal/domain/officestudio/delivery_policy.go internal/officeapp/font_checks.go internal/officeapp/font_checks_test.go
git commit -m "fix(office): Basic formal no longer inherits legacy font-actual Required."
```

---

### Task 3: T02 profile + TargetDigest (start `0157`)

**Files:**
- Create: `internal/modelfit/profile.go`, `internal/modelfit/profile_v2_test.go`
- Create: `internal/modelfit/testdata/profiles-v2.json` (copy from `internal/modelquality/testdata/upgrade-v1/profile-examples.json`, do not import as qualified)
- Create: `migrations/0157_model_native_v2.sql` — **profile/target tables only**; T04 adds protocol ledger columns to the same file or a follow-up statement in 0157 if the journal is still unreleased. If 0157 is already applied in any user DB, add columns in a later unused number — **never** rewrite a released checksum. On this branch 0157 does not exist yet, so one file is OK.
- Modify: `internal/storage/sqlite/store.go` expected migration list + checksum
- Modify: `internal/storage/sqlite/upgrade_compatibility_test.go` — 0157 may now exist; keep refusing stale `0155_model_native*`
- Test: `internal/storage/sqlite/model_fit_v2_test.go`

**Interfaces** (from CONTRACTS §2; add json tags):

```go
type ModeParameters struct {
    ThinkingType  string `json:"thinkingType"`
    Effort        string `json:"effort"`
    ClearThinking *bool  `json:"clearThinking"`
}
type ModelProfile struct {
    SchemaVersion int `json:"schemaVersion"`
    ProfileID, Family, CodecVersion, ModelContract string
    EndpointPurpose, Protocol string
    ModelIDs []string
    Modes map[string]ModeParameters
    ReplayPolicy string
    StrictTools bool
    StrictEndpointSuffix string
    ContextWindow, MaxOutputTokens int64
    SourceURLs []string
    SourceCheckedAt string
    AllowedReturnedModels []string
    RevisionPolicy string
    IdentityEvidenceTTLHours int64
}
type TargetIdentity struct {
    ProviderID, Protocol, EndpointURL, EndpointPurpose string
    ModelRequested, Family, ModelContract string
    CredentialBindingID, ProfileDigest, CodecVersion string
    ExpectedModelRevision *string
}
func TargetDigest(target TargetIdentity) (string, error)
func ProfileDigest(p ModelProfile) (string, error)
```

`CompileParameters` is Task 4. Do not implement attempt lifecycle here.

- [ ] **Step 1: Write the failing tests**

```go
func TestTargetDigestCanonicalAndRejectsSecrets(t *testing.T) {
	a := TargetIdentity{Protocol: "openai_compatible", EndpointURL: "https://API.example.com:443/v1#frag", EndpointPurpose: "standard", ModelRequested: "glm-5", Family: "glm"}
	b := TargetIdentity{Protocol: "openai_compatible", EndpointURL: "https://api.example.com/v1", EndpointPurpose: "standard", ModelRequested: "glm-5", Family: "glm"}
	da, err := TargetDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := TargetDigest(b)
	if err != nil || da != db {
		t.Fatalf("canonical mismatch %s %s %v", da, db, err)
	}
	if _, err = TargetDigest(TargetIdentity{EndpointURL: "https://user:pass@api.example.com/v1"}); err == nil {
		t.Fatal("userinfo must be rejected")
	}
	if _, err = TargetDigest(TargetIdentity{EndpointURL: "https://api.example.com/v1?api_key=secret"}); err == nil {
		t.Fatal("key query must be rejected")
	}
	coding := TargetIdentity{Protocol: "openai_compatible", EndpointURL: "https://api.example.com/v1", EndpointPurpose: "coding", ModelRequested: "glm-5", Family: "glm"}
	dc, err := TargetDigest(coding)
	if err != nil || dc == da {
		t.Fatal("coding purpose must not share standard digest")
	}
}

func TestModelProfileBindingAlias(t *testing.T) {
	raw, err := os.ReadFile("testdata/profiles-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Profiles []ModelProfile `json:"profiles"`
	}
	if json.Unmarshal(raw, &doc) != nil || len(doc.Profiles) < 3 {
		t.Fatal(doc)
	}
	glm := FindProfile(doc.Profiles, "glm", "standard")
	coding := FindProfile(doc.Profiles, "glm", "coding")
	if glm.ProfileID == "" || coding.ProfileID == "" || glm.ProfileID == coding.ProfileID {
		t.Fatal("glm standard and coding must be distinct declared profiles")
	}
	if glm.Modes["quick"].ClearThinking == nil {
		t.Fatal("glm clearThinking false must be present, not omitted")
	}
	d1, _ := ProfileDigest(glm)
	glm2 := glm
	// field order must not matter — marshal via canonical encoder
	d2, _ := ProfileDigest(glm2)
	if d1 != d2 {
		t.Fatal("canonical profile digest unstable")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```powershell
go test ./internal/modelfit -run 'Test(TargetDigestCanonicalAndRejectsSecrets|ModelProfileBindingAlias)' -count=1
```

Expected: FAIL — types / functions undefined.

- [ ] **Step 3: Write minimal implementation**

- Canonical JSON: object keys UTF-8 sorted, arrays keep order, integers decimal, no Unicode NFC.
- EndpointURL: lowercase scheme/host, drop default port, drop fragment, keep path, reject userinfo and query keys that look like secrets (`api_key`, `key`, `token`, `secret`).
- Profiles load as `declared`, never `qualified`.
- `0157` tables: `model_profiles_v2` (profile_id PK, family, codec_version, model_contract, endpoint_purpose, body_json, profile_digest), `model_targets_v2` (digest PK, provider_id, endpoint_url, purpose, model_requested, credential_binding_id). No protocol ciphertext columns yet.
- Update `store.go` manifest + `TestUpgradeMigrationBackupCompatibility` to allow 0157 **after** it exists; still refuse `0155_model_native`.

- [ ] **Step 4: Run tests**

```powershell
go test ./internal/modelfit ./internal/storage/sqlite -run 'Test(TargetDigestCanonicalAndRejectsSecrets|ModelProfileBindingAlias|UpgradeMigrationBackupCompatibility)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit (only if the user asked)**

```powershell
git add internal/modelfit migrations/0157_model_native_v2.sql internal/storage/sqlite
git commit -m "feat(model-office): declare DeepSeek/GLM profiles and canonical target digests."
```

---

### Task 4: T03 CompileParameters + attempt freeze point

**Files:**
- Create: `internal/modelfit/parameters.go`, `parameters_test.go`
- Create: `internal/llmadapter/prepared.go`, `internal/llmadapter/profile_prepared_test.go`
- Modify: `internal/llmadapter/openai.go`, `common.go`, `types.go` — compile through `PreparedRequest`; do not keep “strip unknown thinking on 400” as the success path

**Interfaces:**

```go
type ModelIntent struct {
    Mode string
    StrictTools bool
    OutputTokenCap int64
}
type EffectiveParameters struct {
    ThinkingType, Effort string
    ClearThinking *bool
    StrictTools bool
    MaxTokens int64
}
func CompileParameters(p ModelProfile, in ModelIntent) (EffectiveParameters, error)
```

- [ ] **Step 1: Write the failing test**

```go
func TestCompileParametersMatrix(t *testing.T) {
	glm := mustLoadProfile(t, "glm", "standard")
	eff, err := CompileParameters(glm, ModelIntent{Mode: "quick"})
	if err != nil {
		t.Fatal(err)
	}
	if eff.ClearThinking == nil || *eff.ClearThinking != false {
		t.Fatal("glm explicit false clearThinking must survive")
	}
	body, err := EncodePrepared(glm, eff)
	if err != nil || !bytes.Contains(body, []byte(`"clear_thinking":false`)) && !bytes.Contains(body, []byte(`"clearThinking":false`)) {
		t.Fatalf("omitempty dropped clearThinking: %s", body)
	}
	if _, err = CompileParameters(glm, ModelIntent{Mode: "nope"}); err == nil {
		t.Fatal("unknown mode must error")
	}
}
```

Use the real vendor field name from CONTRACTS / profile-examples (GLM `clear_thinking`). Do not invent a third name.

- [ ] **Step 2: Run to see FAIL** (`CompileParameters` undefined or omitempty drops false).

- [ ] **Step 3: Implement compile + PreparedRequest digest.** BeforeSend/MarkDispatched hooks may be test fakes until T06. Do not send HTTP in this task.

- [ ] **Step 4:**

```powershell
go test ./internal/modelfit ./internal/llmadapter -run 'TestCompileParametersMatrix' -count=1
```

- [ ] **Step 5: Commit (only if asked)** `feat(model-office): compile profile parameters into a frozen PreparedRequest.`

---

### Tasks 5–20: remaining E graph (execute pack section, remapped names)

For each task below: read the named pack section in `docs/design/feilunshengj/IMPLEMENTATION-PLAN.md` and CONTRACTS section, write the listed first failing test **before** production code, use the remapped SQL name, then run the listed command. Do not copy pack Edge profiles. Do not mark Q live. Do not write designer certificates.

| Task | Pack § | Remap / files | First failing test | Run |
| --- | --- | --- | --- | --- |
| T04 ledger | T04 | `0157` add `protocol_epochs_v2` encrypted blobs; `internal/storage/sqlite/protocol_ledger.go`; DPAPI via `internal/secret` | `TestProtocolCipherAADAndLegacyMigration` | `go test ./internal/storage/sqlite ./internal/modelfit -run TestProtocolCipherAADAndLegacyMigration -count=1` |
| T05 history | T05 | `internal/modelfit/transcript.go`; replay exact bytes | `TestNativeHistoryExactRoundTrip` | `go test ./internal/modelfit -run TestNativeHistoryExactRoundTrip -count=1` |
| T06 budget | T06 | **`0158_execution_contract_v2.sql`** (not 0156); `internal/domain/agentrun` budget types; `internal/agentrunapp` | `TestExecutionBudgetConcurrentParentChildReservation` | `go test ./internal/agentrunapp ./internal/storage/sqlite -run TestExecutionBudgetConcurrent -count=1` |
| T07 preflight | T07 | Wire every chat/office/hub send through Compile + budget BeforeSend | `TestFinalInputPreflightEveryAttempt` | `go test ./internal/app ./internal/llmadapter -run TestFinalInputPreflightEveryAttempt -count=1` |
| T08 snapshot | T08 | `Runtime.SnapshotWorkspaceArtifact`; Office blob SHA ≠ extract SHA | `TestArtifactSnapshotRawTail` | `go test ./internal/agentrunapp ./internal/officeapp -run TestArtifactSnapshotRawTail -count=1` |
| T09 resume | T09 | Unknown-effect resume; no double write | `TestExecutionResumeUnknownEffect` | `go test ./internal/agentrunapp -run TestExecutionResumeUnknownEffect -count=1` |
| T10 UI outcome | T10 | Task UI: spinner end + missing file ≠ green complete | `TestModelFitAndOfficeOutcomeUI` (task half) | `npx --prefix web vitest run` + matching Go test |
| T11 qualify | T11 | Keep 0152 fixture table legacy; new live activation CAS | `TestQualificationFixtureCannotPromote` | `go test ./internal/modelfit ./internal/storage/sqlite -run TestQualificationFixtureCannotPromote -count=1` |
| T12 eval | T12 | `internal/modelquality` oracles on the 24 cases | `TestEvalSuiteAllCasesHaveIndependentOracle` | `go test ./internal/modelquality -run TestEvalSuiteAllCasesHaveIndependentOracle -count=1` |
| T13 persist | T13 rest | **`0159_office_delivery_v2.sql`**; `AssessDelivery`; `TestFormalDecisionRequiredCoverage` | `go test ./internal/officeapp ./internal/officestudio ./internal/storage/sqlite -run 'Test(OfficeBriefBrandFactsAcrossFormats\|FormalDecisionRequiredCoverage)' -count=1` |
| T14 PPT | T14 | `visual_model.go` must cover **all pages**; exit 0 without JSON is fail | `TestPPTEditableObjectsAndCoverage` + `TestLayoutTraceSurvivesGenerate` | `go test ./internal/officestudio ./internal/officeapp -run 'Test(LayoutFitPreservesLockedFacts\|PPTEditableObjectsAndCoverage\|VisualManifest\|LayoutTraceSurvivesGenerate)' -count=1` |
| T15 Word/Excel | T15 | DOCX fields + LibreOffice recalc; no invented revenue | `TestDOCXLongTableAndFieldRefresh`, `TestXLSXIndependentOracleAndTypes` | `go test ./internal/officestudio ./internal/officeapp -run 'Test(DOCXLongTableAndFieldRefresh\|XLSXIndependentOracleAndTypes)' -count=1` |
| T16 PDF | T16 | All pages; F03 missing-glyph ≠ success | `TestPDFParseAllPagesAndSameSource` | `go test ./internal/officeapp -run TestPDFParseAllPagesAndSameSource -count=1` |
| T17 patch/bundle | T17 | CAS patch; Excel ok + PPT fail does not publish bundle | `TestOfficePatchCASAndBundleAtomicity` | `go test ./internal/officeapp -run TestOfficePatchCASAndBundleAtomicity -count=1` |
| T18 product UI | T18 | FormalDecision shared by UI / single export / bundle | `TestModelFitAndOfficeOutcomeUI`, `TestExternalExecutorArtifactVerification` | Go + vitest |
| T19 migrate | T19 | Complete `TestUpgradeMigrationBackupCompatibility` restore loop | `TestProtocolMigrationCannotResurrectDeletedSession` | `go test ./internal/storage/sqlite -run 'Test(UpgradeMigrationBackupCompatibility\|ProtocolMigration\|ProtocolBackupKeySet)' -count=1` |
| T20 release | T20 | `scripts/verify-model-office-upgrade.mjs` requires FR01–FR30 evidence; `baseline.json` `layers.E=done` | `TestUpgradeReleaseEvidenceComplete` | `node scripts/verify-model-office-upgrade.mjs` + Go |

**T13 persist first test (do not skip — this is the FormalDecision lock):**

```go
func TestFormalDecisionRequiredCoverage(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	policy := domain.DeliveryPolicy{Tier: "assured", Revision: "v2", RequiredCheckIDs: []string{"file-integrity", "font-actual", "page-coverage"}}
	dec, err := svc.AssessDelivery(context.Background(), task.ID, "missing-version", policy)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed || dec.State == "verified" {
		t.Fatalf("missing evidence must not formal: %+v", dec)
	}
	_ = store
}
```

**T14 visual first test (current bug: `pages[0]` + exit 0):**

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

After each task: run **that** command, then `go test ./internal/storage/sqlite -run TestUpgradeMigrationBackupCompatibility -count=1` if you touched migrations.

E-layer 100% is Task 20 green **and** `layers.E=done`. Then stop. Q/D are Waves D/E in the program, not this file.

---

## Self-review

- Stale pack 0155–0157 never appear as create-paths.
- T01 does not create 0157; T02 does.
- Font hole has its own task before FormalDecision persist.
- T14 explicitly forbids first-page-only visual pass.
- Q live batches and designer certificates are named and excluded.
- Later tasks list first test names and remapped files so an executor is not told “similar to T03”.
