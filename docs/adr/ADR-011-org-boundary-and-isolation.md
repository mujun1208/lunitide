# ADR-011: Organization 边界与多租户隔离根（D-1）

## Status
Accepted（v1.0.0 · 2026-08-16 · 决策人：项目业主 · 接受方式：会话确认 2026-08-16）

## Context
M9 全部生产能力（schema migration / 生产 Bridge API / Registry 注册 / Runner 派发）被 D-1 阻断。
威胁模型 `docs/threat/m9-01-org-isolation.md`（T-01/T-02 跨组织隔离与存在性侧信道）已评审，
要求在解封前冻结多租户隔离根、org_id 语义与「停用≠清除」边界。

## Decision
1. **不可变 org_id 隔离根**：除平台全局只读目录外，所有控制面/数据面资源创建时写入不可变
   `org_id`，禁止后续 UPDATE 改绑组织。该值只能从已验证会话或签名能力票据导出，
   不接受请求正文、查询参数或 Runner 自报覆盖。
2. **Repository 强制谓词**：每条读/写/关联/聚合/删除查询强制注入 `org_id = :verified_org_id`；
   缺少组织上下文即 fail closed（M9-003 语义：跨组织访问 100% 拒绝）。
3. **键与路径规则**：主键引用使用 `(org_id, id)` 复合外键；业务唯一性使用含 `org_id` 的复合唯一键；
   对象存储路径、blob 引用、缓存 key、幂等/去重 key、队列 topic 与死信队列均带组织前缀。
4. **无旁路**：后台消费者、定时任务、索引重建、备份恢复、数据迁移、支持工具与管理员操作
   不得使用 super-tenant 或关闭谓词绕过隔离。
5. **Organization 状态机**：`draft→active→suspended→closed`；suspended 拒绝新运行但保留
   Hold 与审计；closed ≠ 物理删除（停用≠清除，数据仅进入只读封存态）。
6. **ResidencyPolicySnapshot**：绑定 org/data_class/purpose、允许的 storage/processing/backup
   regions、runner kinds、key region、support access regions、跨区复制/迁移状态与 policy digest；
   检查覆盖 DB/blob/index/cache/queue/DLQ/audit/backup/evaluation/Runner 临时盘/诊断导出。

## Alternatives considered
- **行级安全由应用层自觉注入**：被否决——单点遗漏即跨组织泄漏，必须 Repository 层统一强制。
- **org_id 可迁移（改绑组织）**：被否决——历史审计链与存储前缀将失去一致根。
- **closed 即物理删除**：被否决——与 Legal Hold、审计保全、合规取证冲突。

## Consequences
- 解锁切片 1 全部任务卡（T-9.1.1 / T-9.1.2 / T-9.1.3）及 T-9.4.1。
- 首个迁移为 `migrations/0069_m9_org_foundation.sql`（仓库迁移目录为根 `migrations/`，
  起编 N=68 已实测；编号永不回收、禁止修改已应用迁移）。
- 隔离中间件须提供 `go test ./internal/org/ -run TestIsolation` 全负测。

## 评审记录
- 威胁模型评审：`docs/threat/m9-01-org-isolation.md`（T-9.0.2 证据 2026-08-16）。
- 接受签字：项目业主（待签）。
