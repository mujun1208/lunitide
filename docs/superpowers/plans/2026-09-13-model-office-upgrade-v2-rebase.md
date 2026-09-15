# Model-office upgrade v2 rebase plan

> **T01 freeze notes only.** Execute E-layer work from `docs/superpowers/plans/2026-09-13-model-office-e-wave.md`. Sequencing: `docs/superpowers/plans/2026-09-13-full-dev-closeout.md`.

> **For agentic workers:** Use executing-plans or subagent-driven-development. TDD. Do not reuse migrations 0155/0156.

**Goal:** Land the feilunshengj v2 pack on the real 0.4.81 tree without colliding with the project factory.

**Spec:** `docs/superpowers/specs/2026-09-13-model-office-upgrade-rebase-design.md` plus `docs/design/feilunshengj/PRD.md` / `CONTRACTS.md` (read, do not execute stale 0155–0157 names).

## Task 1: T01 freeze

**Files:**
- `docs/audits/model-office-upgrade/baseline.json`
- `scripts/verify-model-office-upgrade.mjs`
- `internal/modelquality/testdata/upgrade-v1/`
- `internal/modelquality/regression_selection.go`
- `internal/storage/sqlite/upgrade_schema.go`
- `internal/storage/sqlite/upgrade_compatibility_test.go`

- [ ] Record HEAD `c93dd3f3`, VERSION `0.4.81`, remapped 0157–0159.
- [ ] Copy eval-cases and profile-examples into testdata (24 unique case IDs).
- [ ] Verify script fails on empty `--run` and on missing FR/case coverage.
- [ ] `TestRegressionSelectionRejectsZeroMatches` and `TestUpgradeMigrationBackupCompatibility` pass against the current store.
- [ ] Do not create 0157 SQL in this task.

```powershell
go test ./internal/modelquality ./internal/storage/sqlite -run 'Test(RegressionSelectionRejectsZeroMatches|UpgradeMigrationBackupCompatibility)' -count=1
node scripts/verify-model-office-upgrade.mjs
```

## Later (do not start until Task 1 is green)

2. T13 font Basic/Assured — `internal/officeapp/font_checks.go`
3. T02/T03 profile + request compiler — `internal/modelfit/profile.go`, `internal/llmadapter`
4. T04+ follow the pack graph with remapped SQL names
