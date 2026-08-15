# M0-M6 全链路复核与修复（fullchain-review）

日期：2026-08-15　依据：engineering-delivery-closure 技能流程　范围：M0-M6 全量代码 + M5/M6/M6-M7 衔接

## 审计发现与修复（全部完成）

| # | 严重度 | 位置 | 缺陷 | 修复 |
|---|--------|------|------|------|
| 1 | SEV1 | workspace/changeset.go | Apply 捕获 RollbackRef 仅写库未同步内存副本，compensate() 对 modify/delete 完全失效（失败 apply 无法恢复已写字节） | 两处写路径在 SetM5ChangeSetItemRollback 后同步 it.RollbackRef=ref；新增 TestChangesetApplyCompensation 回归 |
| 2 | SEV2 | workspace/convert.go | Publish 冲突覆盖：dst 已移入 backup 而 src 重命名失败时，目标文件滞留 backup 丢失 | rename 失败分支先 os.Rename(backup,dst) 恢复再 fail() |
| 3 | SEV2 | m6app/skillimport.go | InstallSkill 重复绑定同 (skillVersion,workspace) 返回 ErrSkillInstallNotFound，幂等语义矛盾 | 已存在时直接返回现有安装记录（重放即成功） |
| 4 | SEV2 | app/m7_handlers.go | ErrNotPublished 误占 M7-WF-002；m7flow.ErrStageFixedSet/ErrStageCycle 落入 INTERNAL_ERROR（违反 PRD FR-02 错误契约） | fixed-set->M7-WF-002、cycle->M7-WF-003、未发布->WORKFLOW_VERSION_UNPUBLISHED；新增映射单元测试 |
| 5 | SEV2 | sqlite/governance.go | Policy CRUD 无审计无事务；UpdateReviewStatus/Policy 无 RowsAffected 检查（幽灵更新静默成功并记审计） | 全部改 execWithAudit（policy.created/updated/deactivated）+ RowsAffected→provider.ErrNotFound |
| 6 | SEV2 | sqlite/skill_storage.go | UpdateSkill/UpdateSkillFields 无审计无事务，无 RowsAffected | 同上（skill.updated 审计 + not-found 检查） |
| 7 | SEV2 | migrations/0055 + store.go | 修复5/6所需的 4 个审计 action 未在 audit_events CHECK 目录；expectedSchemaSQL 需同步 | 0055 迁移（沿 0047-0054 重建模式）+ manifest 注册 + expectedSchemaSQL DDL 同步 |

## 门禁结果（全绿）

- go vet ./...：0 警告
- go test ./... -count=1：ok=74 / FAIL=0（含新增 2 个回归测试）
- node web/scripts/generate-bridge.mjs --check：契约零漂移
- web 构建：tsc --noEmit + vite（319 模块）通过
- -race：本机构建无 cgo/gcc，不可用（与既往收口基线一致，未降低门禁）

## 清理

- 删除本轮诊断探针：zz_diag_diff_test.go、zz_dump_full_test.go、zz_sev1_probe_test.go、zz_dump_0054_test.go（持久 dump 测试 m5_schema_dump_test.go / m6s5c_dump_test.go / m7_dump_evidence_test.go 保留）

## 残留披露（未实现/延后，非本轮缺陷）

1. DatabaseContract ETL/OData legacy 延后（既有披露）
2. stdio 传输 Gate 保持关闭（M6-MCP-004 设计要求）
3. M6 服务未在 cmd/engine/main.go 生产装配（返回可重试 STORAGE_UNAVAILABLE，属后续装配切片；既有披露）
4. M7 切片2-5（证据追踪/CR/发行/Promotion/AppUpdate）按既定计划另行实施，本轮仅修 M6-M7 衔接与切片1 缺陷
5. UI 零变更（符合约束）

## 连续性结论

迁移 0001-0055 连续无缺口；M5→M6→M7 衔接链（changeset/merge.finalize finalDigest→StageInputSnapshot、engine 路由表、bridge 契约）复核通过；审计 action 目录与全部 execWithAudit 写路径一致。
