# Model-office upgrade rebase (0.4.81)

Date: 2026-09-13. Status: execution rebase of the feilunshengj v2 pack. Not a new product line.

## Decision

Keep the existing Go + React + SQLite desktop. Implement FR01–FR30 by extending `modelfit`, `llmadapter`, `agentrun`/`agentrunapp`, `officeapp`/`officestudio`, and `storage/sqlite`. Do not start a second agent framework. Do not treat factory 0155/0156 as unused numbers.

## Frozen baseline (T01)

- Application: `0.4.81`
- Commit: `c93dd3f37bc17f34cb828ded121a14e5c098ed4c`
- Highest applied migration: `0156_project_factory.sql`
- Occupied numbers the v2 pack must not reuse: `0155_project_root_tree.sql`, `0156_project_factory.sql`

## Remapped migrations

| Logical task | Pack name (stale) | Real file |
| --- | --- | --- |
| Model profile + protocol ledger | `0155_model_native_v2.sql` | `0157_model_native_v2.sql` |
| Execution budget / task outcome | `0156_execution_contract_v2.sql` | `0158_execution_contract_v2.sql` |
| Office delivery policy / FormalDecision | `0157_office_delivery_v2.sql` | `0159_office_delivery_v2.sql` |

Do not edit already-released 0154–0156 bodies or checksums.

## What the pack got right

- Split E (engineering) / Q (live model+office) / D (designer certification).
- One FormalDecision, encrypted native ledger, admit-before-send budget, explicit DeepSeek/GLM profiles.
- Reuse Office Spec v2, three brands, Agent Hub as an external executor.

## What is stale or unsafe if executed as printed

- Baseline `0.4.79` / `c6517da0` and reserved 0155–0157 collide with the factory release.
- `FormalDecision` + `0159` exist in the dirty tree (accept/export/bundle share one decision). `Check()` still does not emit `file-integrity` / `source-content` / `locked-facts`. `protocol_epochs_v2`, `CompileParameters`, and the modelquality runner are still absent.
- Current `llmadapter` still strips unknown thinking fields on 400; that is compatibility, not native profile compilation.
- Font `font_actual_substitution` is still Required+unsupported (permanent Basic-formal hole).
- Visual runner still writes only `pages[0]` and treats process exit 0 as review.
- Qualification table `0152_model_fit_qualification` is fixture-only (`untested` / `fixture_pass` / `blocked`) and must stay legacy.
- Pack directory `docs/design/feilunshengj/` contains local Edge profiles; do not commit it. Track copies live under `docs/audits/model-office-upgrade/` and `internal/modelquality/testdata/`.

## Delivery layers

- E can be coded and regression-tested without vendor keys.
- Q requires explicit user-authorized DeepSeek/GLM batches; fixture pass ≠ live qualified.
- D requires human template review. Code cannot mint “高端商用” from tests.

## First executable slice

T01 freeze (this rebase) → T13 Basic/Assured font gate (independent, user-visible) → T02/T03 DeepSeek/GLM profile compiler. Then T04–T10 and T14–T20 in the published dependency graph.
