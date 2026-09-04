# 四专家机务扩展 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有月汐机务底座上新增四张运营同事卡，并把 due/tools/parts/plan 能力加进同一机务工作台，不另开独立平台。

**Architecture:** 四张卡走与 PPT/开发相同的 catalog + bootstrap + KB/记忆/成长路径。引用门与工作台识别一次泛化为 `IsOpsColleague`（五张运营卡）。确定性数字（到期、校准、库存、间隔）只来自 Go 引擎与 SQLite 台账。会商用现有综合器保留 cite；`publish_schedule` 只写本机待办，不直写 AMOS。

**Tech Stack:** Go Engine + React WebView2 + SQLite + 现有 bridge / vitest / `go test`

**Spec:** [docs/superpowers/specs/2026-09-04-four-ops-experts-prd.md](../specs/2026-09-04-four-ops-experts-prd.md)

## Global Constraints

- 要材料里的能力，不要 WingFix / LangGraph / PostgreSQL+Qdrant+Neo4j 骨架。
- 产品内核永远 SQLite；外部库只走现有数据源中心只读。
- 所有机务输出是辅助建议，不构成放行 / 局方批准 / 采购承诺。无 `sign_rts`。
- 六情景继续挂在 `mro-expert`；本计划只新增四张领域同事卡。
- `intentEquipMaxNames=2`；释义句（「CCAR-92 是什么意思」）不装备。
- UI 只用现有令牌；新轨进 more 抽屉，不把顶栏做成六平级 Tab。
- 迁移编号 **0118**；不重写 `graph_nodes.node_type` CHECK。
- 低空/计划人设禁止整份拷贝 `aircraft-maintenance-engineer`。
- 不做 RTS 签字、真邮件、飞控 ETL、frePPLe、NL→SQL、AMOS 适配、预测模型。

---

## File map

| File | Responsibility |
|---|---|
| `internal/m8app/ops_colleague.go` | `OpsColleagueIDs` + `IsOpsColleague` |
| `internal/m8app/catalog/conversation_experts.json` | 四张卡六段正文 |
| `internal/m8app/conversation_catalog.go` | ID 列表 14→18 |
| `internal/m8app/conversation_compose.go` | ByName 短名 + 意图别名 |
| `internal/m8app/ops_scenarios.go` | 每卡 3 张情景种子 |
| `internal/skillapp/bundled/{uas,tooling,parts,mx}-*/SKILL.md` | 四份新人设 |
| `web/src/expert/conversationExperts.ts` | 前端镜像 |
| `web/src/expert/expertIds.ts` | 工作台启用 / 按轨解析 ULID |
| `web/src/mro/MroAskButton.tsx` + `mroContext.ts` | 按轨挂载 |
| `web/src/mro/MroWorkbenchPage.tsx` | more 抽屉新轨 |
| `migrations/0118_mro_ops_ledgers.sql` | 台账表 |
| `internal/mroapp/ops_*.go` | 到期/校准/批次/替代/工作包/约束/发布/串查纯函数 |
| `internal/storage/sqlite/mro_ops.go` | 台账持久化 |
| `internal/m8app/ontologypacks/mro.v1.json` | kinds/links/actions 增量 |

---

## Task 1: Catalog + IsOpsColleague + 四份 SKILL + 情景（P0）

**Files:**
- Create: `internal/m8app/ops_colleague.go`, `internal/m8app/ops_scenarios.go`, `internal/m8app/ops_scenarios_test.go`, `internal/skillapp/bundled/uas-airworthiness-advisor/SKILL.md`, `internal/skillapp/bundled/tooling-chemical-advisor/SKILL.md`, `internal/skillapp/bundled/parts-supply-advisor/SKILL.md`, `internal/skillapp/bundled/mx-planning-advisor/SKILL.md`
- Modify: `conversation_experts.json`, `conversation_catalog.go`, `conversation_compose.go`, `conversation_experts_test.go`, `cmd/engine/main.go`, `skillapp/catalog.go`, `skillapp/catalog_test.go`, `internal/app/chat_kb_inject.go`, `internal/app/chat_citation_gate.go`, `internal/app/chat_office_autogen_test.go`, `internal/app/chat_expert_compose_test.go`

- [ ] **Step 1: 红灯** — 扩展 `TestConversationExpertsCatalogAndRules`：四张新卡必须存在，division=`operations`，identity 含「同事专家」「不是独立进程」，禁止「独立智能体/LangGraph」。`TestConversationExpertsInstructThinkSkillsToolsDrawWrite` 对五张运营卡改用 `kb.search` 针而不是通用 `web.search` 针。长度断言 14→18。
- [ ] **Step 2: 绿灯** — 写入四张完整六段 JSON；`ConversationExpertIDs` 追加四 id；`ConversationExpertByName("工具化工品专家")` 解析到 `tooling-chemical-expert`；意图别名按规格。
- [ ] **Step 3: IsOpsColleague** — `m8app.IsOpsColleague(name, catalogID)` 覆盖五张 id + 显示名 + 短名 + `航空机务专家`。`isMROColleague` / `turnHasMROName` 改为调用它。测试：`低空适航专家` 与 `uas-airworthiness-expert` 为真；`PPT专家` 为假。
- [ ] **Step 4: SKILL** — 四份 SKILL.md（CCAR-92 / SDS·校准 / 替代件三重过滤 / interval_rules）。`catalog.go` embed + Catalog 条目。`TestMROCatalogSkillsPresent` 扩成运营技能表。禁止低空/计划拷贝 AME 原文。
- [ ] **Step 5: 情景** — `EnsureOpsExpertScenarios` 每卡 3 张、幂等、缺专家 no-op。`main.go` 在 `EnsureMROScenarios` 后调用。
- [ ] **Step 6: 意图** — 「查 CCAR-92 运行分类」装备低空；「C 检缺密封剂」最多装备计划+工具两张；「CCAR-92 是什么意思」不装备。
- [ ] **Step 7:** `go test ./internal/m8app/ ./internal/skillapp/ ./internal/app/ -count=1` 绿。

```go
var OpsColleagueIDs = []string{
	"mro-expert", "uas-airworthiness-expert", "tooling-chemical-expert",
	"parts-expert", "mx-planning-expert",
}

func IsOpsColleague(name, catalogID string) bool {
	switch strings.TrimSpace(catalogID) {
	case "mro-expert", "uas-airworthiness-expert", "tooling-chemical-expert",
		"parts-expert", "mx-planning-expert":
		return true
	}
	switch strings.TrimSpace(name) {
	case "航空机务维修专家", "航空机务专家", "低空适航专家",
		"航空工具化工品专家", "工具化工品专家",
		"航空航材专家", "航空维修计划专家",
		"mro-expert", "uas-airworthiness-expert", "tooling-chemical-expert",
		"parts-expert", "mx-planning-expert":
		return true
	}
	if item, ok := ConversationExpertByName(name); ok {
		return IsOpsColleague("", item.ID)
	}
	return false
}
```

---

## Task 2: 前端镜像 + 问月汐按轨挂载 + 工作台空态（P0）

**Files:**
- Modify: `web/src/expert/conversationExperts.ts`, `conversationExperts.test.ts`, `conversationExperts.center.test.tsx`, `expertIds.ts`, `expertIds.test.ts`, `ExpertCenterPage.tsx`, `ExpertCenterPage.test.tsx`, `App.tsx`, `MroAskButton.tsx`, `MroAskButton.test.tsx`, `mroContext.ts`, `MroWorkbenchPage.tsx`, `MroWorkbenchPage.test.tsx`, `web/src/project/phaseExperts.test.ts`

- [ ] **Step 1: 红灯** — `CONVERSATION_EXPERTS` 长度 18；ByName 认「工具化工品专家」；四张 division=`operations`。
- [ ] **Step 2: 绿灯** — 追加四张卡的 skills/tools/mcp/emoji；`conversationExpertDivision` 把四 id 归 operations。
- [ ] **Step 3: expertIds** — `OPS_COLLEAGUE_IDS`、`isEnabledOpsWorkbench`（五张任一启用）、`installedOpsExpertIds`、`workbenchRailForCatalog`、`askCatalogForRail`、`isUASModel`。
- [ ] **Step 4: 专家中心** — `isMroColleague` 改为 `isOpsColleague`；五张卡都出「打开工作台」。测试低空卡同样出按钮。
- [ ] **Step 5: App** — 列出五张 ULID；`onOpenWorkbench(item)` 写入 `initialRail`；侧栏在任一运营卡启用时显示工作台。
- [ ] **Step 6: 问月汐** — `mroContext.scenario` 增加 `due|tools|parts|plan`；`openMroChat` 仍要求 26 位 ULID，但该 ULID 可以是任一运营卡。缺专家文案改为「先启用对应机务专家」。
- [ ] **Step 7: 工作台 more** — 增加 due/tools/parts/plan 空态 + 问月汐。顶栏仍只 manuals/fault/more。
- [ ] **Step 8:** 相关 vitest 绿。

```ts
export function askCatalogForRail(rail: string, model?: string): string {
  switch (rail) {
    case 'due':
      return isUASModel(model) ? 'uas-airworthiness-expert' : 'mx-planning-expert'
    case 'tools': return 'tooling-chemical-expert'
    case 'parts': return 'parts-expert'
    case 'plan': return 'mx-planning-expert'
    default: return 'mro-expert'
  }
}
```

---

## Task 3: 到期引擎 + 校准门 + 批次/替代/工作包（P1 纯函数先红后绿）

**Files:**
- Create: `internal/mroapp/ops_due.go`, `ops_due_test.go`, `ops_tools.go`, `ops_tools_test.go`, `ops_lots.go`, `ops_lots_test.go`, `ops_parts.go`, `ops_parts_test.go`, `ops_plan.go`, `ops_plan_test.go`

- [ ] **Step 1: due** — `EvaluateDue(item, today)`：缺 `used` 或缺 `due_at`/`limit` → `state=missing`，展示「未录入」，**不是 0**。`used/limit` 与 `due_date-today` 超限 → `overdue`。禁止估算。
- [ ] **Step 2: checkout** — `CanCheckoutTool(tool, today)`：`calib_due < today` 返回不可借出原因；未过期允许。
- [ ] **Step 3: lot** — `TraceLot(lots, uses, lotID)`：母批 → 子批 → 使用记录 → 机尾。
- [ ] **Step 4: kit** — `KitShortage(required, onHand)` 列出缺件。
- [ ] **Step 5: alternates** — `FilterAlternates` 必须同时 `certOK && effectivityHit && qty>0`，否则降级询价。
- [ ] **Step 6: work package** — `AssembleWorkPackage` 四源可见：标准卡 + AD/SB due + MEL 时限 + 未关闭项。
- [ ] **Step 7: interval** — `ProposeIntervalChange` 缺 MPD cite 或本队数据则拒绝。
- [ ] **Step 8:** `go test ./internal/mroapp/ -count=1` 绿。

```go
func EvaluateDue(item DueItem, today time.Time) DueStatus {
	if item.UsedMissing || (item.LimitValue <= 0 && item.DueAt == "") {
		return DueStatus{State: "missing", Label: "未录入"}
	}
	// calendar: days = due_at - today; usage: remain = limit - used
}
```

---

## Task 4: SQLite 0118 + store + 工作台轨数据（P1）

**Files:**
- Create: `migrations/0118_mro_ops_ledgers.sql`, `internal/storage/sqlite/mro_ops.go`, `mro_ops_test.go`
- Modify: `internal/storage/sqlite/store.go`（manifest + `expectedSchemaSQL`）, `internal/mroapp/service.go`（Store 接口扩展）, `MroWorkbenchPage.tsx` + 测试

表（同一产品库）：`mro_due_items`, `mro_utilization_events`, `mro_components`, `mro_life_events`, `mro_pirep_drafts`, `mro_tools`, `mro_tool_loans`, `mro_chem_lots`, `mro_chem_uses`, `mro_kits`, `mro_kit_items`, `mro_parts_stock`, `mro_alternates`, `mro_aog_cases`, `mro_po_drafts`, `mro_interval_rules`, `mro_task_card_templates`, `mro_work_packages`, `mro_wp_tasks`, `mro_schedule_assignments`, `mro_capacity_slots`, `mro_interval_change_drafts`, `mro_ops_todos`。

主键一律 26 位 ULID CHECK，风格对齐 `0116_mro_workbench.sql`。

- [ ] **Step 1:** 写 migration；`sha256sum` 登记 `store.go` manifest。
- [ ] **Step 2:** 把 sqlite_schema 原文写入 `expectedSchemaSQL`（先 apply 再 dump，禁止手猜空格）。
- [ ] **Step 3:** store 方法：list/upsert due、list tools、checkout（走 `CanCheckoutTool`）、trace lot、kit staging、list stock/alternates、assemble WP、constraint check、publish（写 todos）。
- [ ] **Step 4:** 工作台 due/tools/parts/plan 渲染台账；过期工具借出按钮 disabled + 原因；缺利用率显示「未录入」。
- [ ] **Step 5:** `go test ./internal/storage/sqlite/ ./internal/mroapp/ -count=1` 与工作台 vitest 绿。

---

## Task 5: 发布联动 + 质量通报串查 + 约束检查（P2）

**Files:**
- Create: `internal/mroapp/ops_collab.go`, `ops_collab_test.go`
- Modify: `ops_plan.go`（C1–C7 检查）、工作台 tools/parts/plan 轨、`mro.v1.json`

约束检查（P1 已有骨架，P2 补齐 C1–C7 列出违反项，**不是求解器**）：

1. C1 窗口与 due 冲突（窗口晚于 overdue）
2. C2 机位/技能组工时超出 `capacity_slots`
3. C3 机尾已标记 AOG/停场
4. C4 同一机尾窗口重叠
5. C5 套件缺件（kit shortage）
6. C6 长周期件无库存且无替代
7. C7 间隔规则缺 source_cite

- [ ] **Step 1:** `CheckScheduleConstraints` 返回违反列表；无违反则 `ok`。
- [ ] **Step 2:** `PublishSchedule(wpID)` 人审后写入 `mro_ops_todos`：`kit_staging` + `parts_request`；工作台 tools/parts 轨可见待办。
- [ ] **Step 3:** `QualityBulletinChain(lotID)`：机尾清单（cite 使用记录）→ 适航影响占位（需法规 cite）→ 航材冻结/询价草稿字段 → due 重算草稿。全程不自动写生产库。
- [ ] **Step 4:** 本体包增加 spec §6.6 kinds/links；`TestOntologyPacksLoadSoftwareAndMRO` 断言新 kind。
- [ ] **Step 5:** 工作台「串查」按钮预填批次号（打开问月汐，挂载工具+低空或机务修，最多 2）。
- [ ] **Step 6:** `go test ./internal/mroapp/ ./internal/m8app/ -count=1` 与相关 vitest 绿。

```go
func PublishScheduleTodos(pkg WorkPackage) []OpsTodo {
	return []OpsTodo{
		{Kind: "kit_staging", Ref: pkg.ID, Status: "open"},
		{Kind: "parts_request", Ref: pkg.ID, Status: "open"},
	}
}
```

---

## Task 6: Bridge 对齐（能接线则接线，不阻塞引擎验收）

**Files:** `api/bridge/v1/mro.due.list.schema.json` 等、`internal/app/mro_handlers.go`、`handlers_registry.go`、`npm run generate:bridge`、`npm run verify:bridge`

优先方法：`mro.due.list`、`mro.tool.list`、`mro.tool.checkout`、`mro.lot.trace`、`mro.kit.staging`、`mro.parts.stock.list`、`mro.plan.constraint.check`、`mro.plan.publish`、`mro.ops.todo.list`。

- [ ] **Step 1:** 每个方法一份 schema（`additionalProperties: false`，带 `x-result` / `x-examples`）。
- [ ] **Step 2:** handler 只调 `mroapp`，不写 SQL。
- [ ] **Step 3:** `npm run generate:bridge` 后 `verify:bridge` 绿。
- [ ] **Step 4:** `App.tsx` 把新方法接到工作台 props。

若 generate 与现有 schema 冻结冲突，先保证 Go 引擎 + 工作台注入 props 的 vitest 绿，再补 bridge；不得为了接线引入独立 page。

---

## Self-review

| Spec 条款 | 任务 |
|---|---|
| FR-F1–F7 四卡/SKILL/情景/别名 | Task 1–2 |
| §5.1 引用门泛化 | Task 1 Step 3 |
| FR-G1 空态 + 问月汐 | Task 2 |
| FR-G2–G7 台账/校准/批次/套件/AOG/工作包 | Task 3–4 |
| FR-H5 间隔双源 | Task 3 Step 7 |
| P2 发布联动 / 串查 / C1–C7 | Task 5 |
| 无 sign_rts / NL→SQL / 独立 page | Global Constraints |

无 TBD。类型名 `DueItem` / `DueStatus` / `OpsTodo` 在 Task 3 与 Task 5 一致。
