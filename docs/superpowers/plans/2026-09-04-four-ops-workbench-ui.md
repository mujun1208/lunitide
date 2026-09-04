# 四专家机务工作台 UI/后台 Implementation Plan

> **For agentic workers:** 按任务逐步实施，先红后绿。Steps use checkbox (`- [ ]`) syntax.

**Goal:** 复盘四专家机务扩展，修复"能建卡但填不了数据"的核心漏洞，把工作台重构为六域一级导航 + 列表/详情/录入，并补全全部写路径与低空履历/AOG/PO 缺失域。

**Architecture:** UI 只用现有 Moon&Tide 令牌 + `Dialog`/`usePanelResize`。确定性数字只来自 Go 引擎与 SQLite（0118 表已够，无新迁移）。所有写方法幂等 + 人审 + 落审计。会商保留 cite。

**Tech Stack:** Go Engine + React WebView2 + SQLite + 现有 bridge / vitest / `go test`

**Spec:** [docs/superpowers/specs/2026-09-04-four-ops-workbench-ui-prd.md](../specs/2026-09-04-four-ops-workbench-ui-prd.md)

## Global Constraints

- 复用现有令牌/组件，零新依赖；不改 Cursor 计划文件。
- 内核 SQLite；外部库只走数据源中心只读。
- 所有机务输出辅助建议，不构成放行/采购承诺；无 `sign_rts`。
- 无新迁移（复用 0118）；若字段确缺再单列 0119。
- 不做 RTS/真邮件/飞控 ETL/frePPLe/NL→SQL/AMOS 适配/预测模型。

---

## File map

| File | Responsibility |
|---|---|
| `web/src/mro/MroWorkbenchPage.tsx` | 六域一级导航 + 子页签 + 列表/详情 + 录入 |
| `web/src/mro/mroContext.ts` | scenario/子页签上下文 |
| `web/src/expert/expertIds.ts` | rail 映射 / 机尾过滤 |
| `web/src/bridge/client.ts` | MroBridge 扩展写方法 |
| `web/src/App.tsx` | 接新 bridge props |
| `web/src/styles.css` | 补 `.mro-ops-rail/.mro-domain/.mro-subtabs/.mro-split/.mro-badge*` 等 |
| `api/bridge/v1/*.schema.json` + `envelope.schema.json` | 新方法 schema |
| `web/scripts/generate-bridge.mjs` | enabled 列表 |
| `internal/app/mro_ops_handlers.go` + `handlers_registry.go` | 新 handlers |
| `internal/mroapp/service.go` + `ops_service.go` | OpsStore + Service 新方法 |
| `internal/mroapp/ops_due.go`(重算)/`ops_uas.go`(新)/`ops_parts.go`(扩) | 纯函数 |
| `internal/storage/sqlite/mro_ops.go` | 新 store 方法 |

---

## Task R0: UI 重构骨架（前端 + CSS，零新后端）

**Files:** `MroWorkbenchPage.tsx`(+test), `mroContext.ts`, `expertIds.ts`(+test), `styles.css`, `App.tsx`(+test)

- [ ] **Step 1 红:** vitest 断言六域为一级导航按钮（手册/排故/到期/工具化工品/航材/计划）；checklist/审计/机队在"工具组"；each domain 有子页签。
- [ ] **Step 2 绿:** 重构 `Rail` 类型为六域 + `util`；把 due/tools/parts/plan 提出"更多"；域头 + `skill-status-tabs` 子页签。
- [ ] **Step 3:** 补 CSS：`.mro-ops-rail`、`.mro-domain`、`.mro-domain-head`、`.mro-subtabs`、`.mro-split`、`.mro-badge.is-ok/.is-warn/.is-err`、`.mro-datasource`、`.mro-import-preview`、`.mro-import-hint`。
- [ ] **Step 4:** 机尾/日期过滤打通（loader 传 scope）；各域 loading(`role=status`)/error(`role=alert`)；空态主按钮开录入表单。
- [ ] **Step 5 修 bug:** 串查按当前批次（非 `lotRows[0]`）；套件"转航材待办"暂留占位但标注 P1 落库；`CheckConstraints` 死代码清理放 P1。
- [ ] **Step 6:** `App.tsx` 接线保持防御（partial mock 安全）；`App.test.tsx` 绿。
- [ ] **Step 7:** 相关 vitest 绿。

---

## Task P1: 打通已有写路径（bridge + handlers + 表单 + store）

**Files:** `api/bridge/v1/*.schema.json`, `envelope.schema.json`, `generate-bridge.mjs`, `mro_ops_handlers.go`, `handlers_registry.go`, `service.go`, `ops_service.go`, `ops_due.go`, `mro_ops.go`, `client.ts`, `MroWorkbenchPage.tsx`(+test), 审计源

- [ ] **Step 1 store:** OpsStore 新增 `InsertUtilizationEvent`/`ListUtilizationEvents`、`CloseToolLoan`、`UpsertIntervalRule`、`UpsertScheduleAssignment`、`UpsertCapacitySlot`、`UpsertTaskCardTemplate`/`ListTaskCardTemplates`、`InsertWPTask`/`ListWPTasks`。sqlite 实现 + 往返测试。
- [ ] **Step 2 引擎:** `ops_due.go` 加 `RecomputeDue`(利用率求和→更新 used)；service `RecordUtilization`/`ReturnTool`/`IssueChemical`/`AddPartsTodo`/`UpsertIntervalRule`/`UpsertScheduleAssignment`/`UpsertCapacitySlot`；`BuildWorkPackage` 扩展写 wp_tasks。红→绿测试。
- [ ] **Step 3 bridge:** 新 schema（`additionalProperties:false`, `x-result`）：`mro.due.upsert`/`mro.util.record`/`mro.tool.upsert`/`mro.tool.return`/`mro.lot.upsert`/`mro.chem.issue`/`mro.kit.upsert`/`mro.ops.todo.add`/`mro.parts.stock.upsert`/`mro.parts.alternate.upsert`/`mro.plan.build`/`mro.interval.upsert`/`mro.interval.propose`/`mro.schedule.assign`/`mro.capacity.upsert`/`mro.bulletin.chain`。envelope enum + enabled 列表 → `generate:bridge`。
- [ ] **Step 4 handlers:** 每方法 decode+校验+幂等（写方法）；registry 注册；`mroFailure` 复用；契约测试。
- [ ] **Step 5 审计:** MRO ops 写动作落审计；`mro.audit.list` 合并 KB ∪ MRO ops；测试回放。
- [ ] **Step 6 UI:** 各域录入 `Dialog` 表单调用写方法；套件待办落库；client.ts MroBridge 扩展。
- [ ] **Step 7:** `go test ./internal/...`、`verify:bridge`、相关 vitest 绿。

---

## Task P2: 补全低空 + AOG + PO 缺失域

**Files:** `ops_uas.go`(新), `ops_parts.go`(扩), `service.go`, `ops_service.go`, `mro_ops.go`, schema/handlers/registry, `MroWorkbenchPage.tsx`(+test), `client.ts`

- [ ] **Step 1 store:** `UpsertComponent`/`ListComponents`、`InsertLifeEvent`/`ListLifeEvents`、`InsertPirepDraft`/`ListPirepDrafts`/`UpdatePirepState`、`InsertAOGCase`/`ListAOGCases`/`UpdateAOGState`、`UpsertPODraft`/`ListPODrafts`/`UpdatePOState`。往返测试。
- [ ] **Step 2 引擎:** `ops_uas.go`：`QueryGenealogy`(纯, SN→履历→机尾)、`TriggerStatus`(纯, 五类)；service `UpsertComponent`/`RecordLifeEvent`/`FilePirep`/`ConfirmPirep`/`IntakeAOG`/`CreatePODraft` + 状态流转。测试。
- [ ] **Step 3 bridge:** `mro.component.upsert`/`.list`、`mro.life.record`/`.list`、`mro.genealogy`、`mro.trigger.status`、`mro.pirep.file`/`.list`/`.confirm`、`mro.aog.intake`/`.list`、`mro.po.draft.create`/`.list` → schema + envelope + enabled + `generate:bridge`。
- [ ] **Step 4 handlers + registry + 契约测试。**
- [ ] **Step 5 UI:** 到期与履历子页签(部件履历/PIREP/触发器)；航材子页签(AOG/采购草稿)；client.ts 扩展。
- [ ] **Step 6:** 全套测试绿。

---

## Task P3: 协同与约束收尾

**Files:** `ops_collab.go`, `ops_plan.go`, `ops_service.go`, `MroWorkbenchPage.tsx`(+test)

- [ ] **Step 1:** 清理 `CheckConstraints` 死代码；C1–C7 违反项徽章化。
- [ ] **Step 2:** 质量通报黄金路径：工具→(低空/机务修)→航材→计划，全程 cite、不写生产库；串查预填批次挂载 ≤2 卡。
- [ ] **Step 3:** 发布联动：套件/航材待办跨轨可见（已部分实现，落库版）。
- [ ] **Step 4:** `go test ./internal/...`、`verify:bridge`、相关 vitest 绿。

---

## Self-review

| Spec 条款 | 任务 |
|---|---|
| FR-U1–U5 UI 重构 | R0 |
| FR-W1–W10 写路径闭环 | P1 |
| FR-L1–L5 低空/AOG/PO | P2 |
| FR-C1–C3 协同 | P3 |
| 审计 KB∪ops | P1 Step 5 |
| 无新迁移/无 sign_rts/无 NL→SQL | Global |

无 TBD。类型名 `DueItem`/`DueStatus`/`OpsTodo`/`Component`/`LifeEvent` 跨任务一致。
