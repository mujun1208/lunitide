# Remaining Engineering Close-out Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Execute **one wave at a time**. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every remaining **engineering** FR first-lock that is still missing, then refresh offline release evidence. Human live experiments and designer scores are out of scope.

**Architecture:** Named tests from `docs/audits/model-office-upgrade/traceability.json`. Minimal production only where a lock fails. Do not mint live qualification or designer certificates. Do not write `layers.E=done`.

**Tech Stack:** Go 1.26, SQLite, existing `modelfit` / `agentrun` / `officestudio` / `modelquality`.

**Spec:** `docs/audits/model-office-upgrade/traceability.json` FR01–FR30; `docs/superpowers/plans/2026-09-13-all-dev-100-closeout.md` honesty contract.

## Global Constraints

- Stay dirty. No commit unless the user asks.
- Do not write `baseline.json` `layers.E=done`.
- Do not write `live_qualified` or `designerReviewed=1`.
- Do not re-run paid live Q. Do not OpenSecure production DB.
- `--release` must stay fail-closed (`modelQualification` / `templateCertificates` not live/certified).
- Windows: `;` not `&&`. `go test` needs unrestricted permissions.
- Do not revert factory leftover, 0157–0159, or T01 freeze (`latestAppliedMigration` stays `0156_project_factory.sql` in baseline).

## Out of scope (user: 真人实验可不算)

- Human designer 3-score review
- 3× office live 36
- Installed-app human click-through (factory walk already recorded)
- Claiming product 100% / E 100% / 高端商用

## Wave R1 — missing FR first locks *(complete, reviewed)*

Write the named test first. Then minimal production.

| FR | First test | Done when |
| --- | --- | --- |
| FR04 | `TestNativeTargetMismatchRebuild` | Incompatible model/endpoint/profile rebuilds epoch; task/budget kept |
| FR06 | `TestAttemptErrorMatrix` **and** `TestTaskOutcomeForcedSummarySequence` | 400/429/disconnect/retry/forced-summary honor protocol + no extra side effect |
| FR09 | `TestTaskOutcomeRejectsModelAuthoredL0` **and** `TestTaskOutcomeMissingRequiredStep` | Model body cannot self-author L0 evidence; missing required step stays incomplete |
| FR12 | `TestOutcomeGoalRevisionOrder` | Stream end / task outcome / user adopt stay independent; new goal revision not overwritten by old terminal |
| FR13 | `TestCompiledToolCatalogStable` | Tool catalog compiles per task/model; digest stable |
| FR16 | `TestActivationCASAndRollback` | Activate CAS; rollback restores previous binding; in-flight task untouched |
| FR17 | `TestOfficeBriefBrandFactsAcrossFormats` | Same brief+facts+brand across docx/pptx/xlsx/pdf; reuse ops/brand/editorial |
| FR26 | `TestRuntimeEvidenceRedaction` | Logs/export evidence have no protocol private / secrets |
| FR28 | `TestTemplateCertificationAndBenchmarkAccounting` | Model vs office scored separately; template/font/tool source tracked; `designerReviewed=0` |

## Wave R2 — evidence refresh *(complete, reviewed)*

- Run each FR `testCommand` (existing + new). Fill `testResult=pass`, `implementationCommit` = `c93dd3f37bc17f34cb828ded121a14e5c098ed4c`, `runEvidenceDigest` = sha256 of that test’s stdout.
- FR status may become `passed` when the named test is green. That is engineering pass, not live release.
- `release-evidence.json` `layers.Q` = `ran` (match baseline). `layers.E` = `pending`. `layers.D` = `not_run`.
- `modelQualification.status` stays `missing` (not live).
- `templateCertificates.status` stays `missing` (not certified).
- `migrationRecord` stays honest (`0156` freeze in baseline; do not claim production DB migrated).
- `node scripts/verify-model-office-upgrade.mjs --inventory` still ok; `--release` still fail-closed.
