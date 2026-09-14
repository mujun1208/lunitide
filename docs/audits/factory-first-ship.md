# Factory first-ship walk (implementation)

Date: 2026-09-14
Install / data dir: throwaway `C:\Users\mujun\AppData\Local\Temp\lunitide-walk-data-2026-09-14` (`LUNITIDE_DATA_ROOT`; dirty-tree `go build ./cmd/engine`, not installed 0.4.77, not `%LOCALAPPDATA%\Lunitide`)
Result: pass

Out of scope (do not block factory 100%): live Cursor/Codex, device farm, K8s.

Live connect: throwaway nonce at `%TEMP%\lunitide-walk-data-2026-09-14\gateway-session.nonce` + `ipc.GatewayPipeName(USERNAME)` → `\\.\pipe\lunitide-gateway-mujun`. `system.health` = `{"engine":"ready","protocol":"1.0","version":"0.0.0-dev"}`. Did not OpenSecure the production DB. Did not `--quit` production. Throwaway host left running after the walk. B5 `factoryEngine` output was not copied as this walk.

Project: `factory-walk-2026-09-14` / `01M2ERW78C7VNRAE3SGV1W2QSG` / `ITM00005` / status `go_live_prep` / version 12.
Root: `C:\Users\mujun\AppData\Local\Temp\lunitide-factory-walk-2026-09-14-093531` (not this repo).
Release dest: `C:\Users\mujun\AppData\Local\Temp\lunitide-factory-walk-2026-09-14-release-093531`.

Product bug found on the first throwaway binary (9/12): workbench order pack → promote → `project.release.sync` → 三关 wrote `.lunitide/sync-receipt.json` after the CR snapshot, so `InventoryTree` changed and `project.advanceStatus` phase 8 returned `PROJECT_INVALID_TRANSITION` (`release sources changed since revision`). Regression: `TestProjectReleasePhaseAllowsSyncReceiptAfterRevision` + inventory skip of `.lunitide/sync-receipt.json`. Walk below is the 12/12 rerun against the rebuilt dirty-tree engine.

1. pass — `project.create` / `project.publish` / `stage.create` phases 1–8. Implementation project on the throwaway disk root above.
2. pass — `project.interview.get` / `project.deliverable.generate` / `deliverable.list` / `projectAttachment.get`. Phase 1: 9 openable attachments. Incomplete-interview banner present (allowed).
3. pass — `projectAttachment.ingest` / `deliverable.upsert` / `project.advanceStatus`. Human edit + 三关. `treeStatus=ready`, `rulesDigest` set; `.lunitide/rules/` + AGENTS.md hosted segment written.
4. pass — `project.deliverable.generate` / `deliverable.list` / `projectAttachment.get` / `deliverable.upsert` / `project.advanceStatus`. Phase 2: 10 cards; `api_list` / `feature_dev_list` JSON; 三关.
5. pass — `project.schema.put` / `materialize` / `verify` / ingest / upsert / 三关. sqlite at `{root}/.lunitide/data/app.sqlite`; `dbStatus=ready`.
6. pass — ingest I001 onto `api_list` before phase-2 lock, then `project.board.sync` / `board.get` / `board.put` / 三关. First sync: I001; stats `共 1 条 · 新增 1 · … · 待再处理 1`.
7. pass — `project.task.open` blocked `PROJECT_DB_REQUIRED` then `PROJECT_INTERFACE_REQUIRED`. After gates: F001 from feature list; self-test via `board.put`; open succeeded.
8. pass — `project.board.sync` / `project.test.run` / `project.task.returnFromTest`. Test board has T-I001 + T-F001. T-API fail returned I001; T-DEV fail returned F001.
9. pass — ingest / upsert / `project.test.run` / 三关. Integration start blocked `PROJECT_INTEGRATION_NOT_READY`; fail returns members by source; then pass + 三关.
10. pass — `release.createRevision` / `release.buildPackage` / `release.promote` (dev then stage) / `project.release.sync` to sibling dest / `project.advanceStatus` 三关. Dest has `keep.txt`.
11. pass — `project.list` / `session.create` / `project.deliverable.generate` / `project.rules.get`. Personal generate and `rules.get` both `PROJECT_ROOT_REQUIRED` (no generate bar / no `[项目规范]`). Session `01M2ERW84616N077H5TM88WHMW`.
12. pass — `project.list` / `project.schema.get`. Walk project `rulesDigest` set (规范已注入) and `dbStatus===ready` (库表 ready).

项目管理工厂 100% 落地: Tasks 1–5 stay green (`TestFactoryFirstShipImplementationWalk` re-run pass) and this walk is 12/12 pass. Do not mark the whole product or model-office 100% from this file.
