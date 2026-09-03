# 航空机务专家与机务工作台 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 上架第 14 张同事专家「航空机务专家」，带六张情景卡与引用门；并增加机务工作台做机尾、手册、排故与检查单。

**Architecture:** 专家只是目录 + 六段 + 情景，不是新进程。检索走底座 `kb.search`。工作台表在 SQLite。会商综合器不得丢掉引用块。不 Fork WingFix，不直写 AMOS/TRAX。

**Tech Stack:** 现有专家目录 / 情景卡 / 会商 / `docx.gen` / `excel.gen` + React 新页 `web/src/mro/` + 迁移 `0116`。

**Spec:** [docs/superpowers/specs/2026-09-03-mro-expert-and-expert-foundation-design.md](../specs/2026-09-03-mro-expert-and-expert-foundation-design.md) 附录 A、§6.5、§7、FR-B、FR-C、FR-E

**Depends on:** 底座计划的 `kb.search` / 专家 collection / `ParseBodyIndexer`。数据源计划的 bind（工作台库存映射，P1）。

## Global Constraints

- 只增加一张专家卡 `mro-expert`。六角色是情景卡。
- division 必须是 `operations`（八骨架已有）。
- 六段必须含「辅助建议，不构成放行」与「无引用不得给确定件号」。
- 身份禁止「独立智能体」「对话技能包」。
- 无依据确定步骤 = 产品失败（黄金集该项必须为 0）。
- UI 只用 `--bg --ink --tide1` 等现有令牌。
- 不引入 Python / LangGraph / Qdrant / Neo4j。
- 同事专家数量测试从 13 改为 14，所有写死 `!= 13` 的断言必须改。

---

## File map

| File | Responsibility |
|------|----------------|
| `internal/m8app/catalog/conversation_experts.json` | 增加 mro-expert 对象 |
| `internal/m8app/conversation_catalog.go` | `ConversationExpertIDs` 追加 |
| `web/src/expert/conversationExperts.ts` | 前端花名册与工具表 |
| `internal/skillapp/catalog.go` | 三个 mro 技能 |
| `internal/m8app/mro_scenarios.go` | 六情景种子 |
| `internal/app/chat_citation_gate.go` | 引用门 |
| `migrations/0116_mro_workbench.sql` | 机尾 / 手册元数据 / 缺陷草稿 |
| `web/src/mro/MroWorkbenchPage.tsx` | 工作台 |
| `web/src/App.tsx` | `Page` 加 `'mro'` |

---

## Task 1: 目录上架 `mro-expert`

**Files:**
- Modify: `internal/m8app/catalog/conversation_experts.json`
- Modify: `internal/m8app/conversation_catalog.go`（IDs 与 `ConversationExperts` 注释）
- Modify: `web/src/expert/conversationExperts.ts`
- Modify: `internal/m8app/conversation_experts_test.go`（`want 13` → `14`，`wantName` 加 mro）
- Modify: `web/src/expert/conversationExperts.test.ts`（length 14，arrayContaining 航空机务专家）
- Modify: 其它写死 13 的测试（先 `rg "13 specialists|toHaveLength\\(13\\)|ConversationExperts\\(\\)\\) != 13"`）

- [ ] **Step 1: 写失败测试** 在 `TestConversationExpertsCatalogAndRules` 的 `wantName` 增加 `"mro-expert": "航空机务专家"`，并断言 `item.Division == "operations"`、identity 含「不构成放行」、不含「独立智能体」。先不改 JSON，跑测试应失败。

Run: `go test ./internal/m8app/ -run TestConversationExpertsCatalogAndRules -count=1`

- [ ] **Step 2: 在 `conversation_experts.json` 的 `items` 数组末尾追加**（六段必须与 spec 附录 A **逐字一致**）：

```json
{
  "id": "mro-expert",
  "name": "航空机务专家",
  "displayName": "航空机务专家",
  "description": "持证辅助检索顾问：按机尾与日期查受控手册，排故与检查单必须带修订版引用，不代替放行。",
  "category": "行业运营",
  "division": "operations",
  "origin": "lunitide",
  "usage": "both",
  "scene": "机务维修",
  "emoji": "✈",
  "version": "1.0.0",
  "preferredSkills": ["mro-manual-rag", "mro-fault-tree", "mro-checklist"],
  "requiredTools": ["kb.search", "graph.expand", "workspace.write", "docx.gen", "excel.gen", "todo.write", "datasource.query"],
  "mcpFallback": "手册走 kb.search。库存走已探测的 datasource.query。不另装 MRO 云 MCP。",
  "sixSection": {
    "identity": "你是同事专家（同一月汐引擎，不是独立进程）：航空机务专家。持证维修人员的辅助检索顾问，不是放行人，不是独立进程。你依据受控手册、适用性（机尾/日期/构型）与已确认历史缺陷回答。禁止把内部政策与 OEM 手册混为一谈。产品强制：先问清机尾或机型与日期，再检索，再引用，再给隔离步骤。",
    "mission": "帮用户查 AMM/IPC/TSM/FIM/WDM/CMM/MEL/SB/AD/EO 与内部政策，做排故辅助、检查单草稿与培训题。每条关键结论必须带文档修订版、ATA 或图号/页码。用户要出 Word/Excel 时用 docx.gen / excel.gen。与产品经理专家、开发专家同场时你只负责机务事实与引用，不写 PRD、不写代码。",
    "rules": "所有输出都是辅助建议，不构成维修放行；放行由持证人员做出。无引用不得给出确定件号或确定步骤，应写「未找到受控依据」并请用户查对应手册。内部政策 / EO 优先于 OEM。未受控、过期、被替代文档必须警示并要求确认。禁止编造扭矩、间隙、件号、库存与适航结论。去AI味：删赋能/打通/闭环。禁止电气 DIY。用户问「做好了没有」时继续本流水线，不要重开。不做第二套 MRO 系统，不直写 AMOS/TRAX。",
    "workflow": "1) todo.write 记下机尾/机型、日期、症状或任务、已有附件。2) 缺机尾或日期就先问，不要假设全机队适用。3) kb.search 检索本专家知识库；硬标识符（件号、ATA）提高关键词权重。4) 不适用当前机尾/日期的块丢弃。5) 排故按症状→候选故障→原因→任务→件号组织，并标置信度低/中/高。6) 每条关键句 kb.cite。7) 要检查单则 excel.gen 或 docx.gen，并写「辅助建议，不构成放行」。8) 无依据则拒答。规范/目录/代码争议分别转开发规范专家、系统项目结构规范专家、开发专家。",
    "deliverableTemplate": "机尾/机型/日期（或待确认）。问题重述。依据：文档类型 / 修订版 / ATA / 图号或页码 / 适用性。结论与隔离步骤（每步有引用）。置信度。未受控或过期警示。待确认项。可选检查单路径。页眉或文首必须出现「辅助建议，不构成放行」。",
    "successMetrics": "先看到机尾与日期而不是空泛步骤；每条关键结论有手册锚点；换机尾时适用性变化可见；未编造件号；会商时引用块未被综合器删掉；未声称可以放行。"
  },
  "kind": "agent"
}
```

`ConversationExpertIDs` 末尾加 `"mro-expert"`。

`conversationExperts.ts`：`CONVERSATION_EXPERTS` 加 `{id:'mro-expert', name:'航空机务专家'}`；preferred skills / required tools / mcp fallback 与 JSON 一致；`conversationExpertDivision('mro-expert')` 若按表映射，加 `operations`。查看 `conversationExpertDivision` 实现：若只认现有 division 表，补 `operations`。

- [ ] **Step 3: 更新所有 13→14 断言后跑**

Run: `go test ./internal/m8app/ ./internal/app/ -count=1` 与 `npm --prefix web run test -- conversationExperts`

`TestConversationExpertsInstructThinkSkillsToolsDrawWrite` 要求每张卡含 `web.search` 等。机务卡 workflow 没有 `web.search`。**不要**往附录 A 塞网页检索。改该测试：对 `mro-expert` 改用针 `kb.search`、`不构成放行`、`todo.write`、`docx.gen`，其它 13 张仍用原 needles。

`TestConversationExpertComposeAttachLists` 的 `want` / `wantTools` 增加：

```go
"mro-expert": {"mro-manual-rag", "mro-fault-tree", "mro-checklist"},
```

```go
"mro-expert": {"kb.search", "docx.gen", "excel.gen"},
```

技能尚未入库时 compose 可能缺名。本任务先断言 catalog `PreferredSkills`，compose 测试等到 Task 2 技能注册后再跑通。

- [ ] **Step 4: Commit** `feat(expert): add aviation MRO colleague expert card`

---

## Task 2: 三个内置技能

**Files:**
- Modify: `internal/skillapp/catalog.go`（在 `knowledge-index` 条目旁追加三个 builtin）
- Create: `internal/skillapp/bundled/mro-manual-rag/SKILL.md`
- Create: `internal/skillapp/bundled/mro-fault-tree/SKILL.md`
- Create: `internal/skillapp/bundled/mro-checklist/SKILL.md`
- Test: `internal/skillapp/catalog_test.go`（或现有 catalog 测试）增加 ID 存在断言

`catalog.go` 条目：

```go
{
	ID: "mro-manual-rag", Name: "tpl-mro-manual-rag", DisplayName: "机务手册检索",
	Description: "按机尾与日期检索受控手册，强制带修订版与 ATA 引用。",
	Category: "信息检索", Version: "1.0.0",
	Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
	EntryPoint: "builtin://mro-manual-rag",
	Manifest: map[string]any{
		"triggers": []string{"查手册", "AMM", "MEL", "ATA"},
		"prompt":   "先确认机尾或机型与日期，再 kb.search。每条关键结论必须 kb.cite。无依据写未找到受控依据。文首写辅助建议，不构成放行。",
	},
},
{
	ID: "mro-fault-tree", Name: "tpl-mro-fault-tree", DisplayName: "排故故障树",
	Description: "按症状→故障→原因→任务→件号组织排故，并标置信度。",
	Category: "信息检索", Version: "1.0.0",
	Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
	EntryPoint: "builtin://mro-fault-tree",
	Manifest: map[string]any{
		"triggers": []string{"排故", "隔离", "故障树"},
		"prompt":   "按症状、候选故障、原因、任务、件号输出。每步引用手册。置信度低中高。禁止无引用确定件号。",
	},
},
{
	ID: "mro-checklist", Name: "tpl-mro-checklist", DisplayName: "机务检查单",
	Description: "把已引用的排故步骤收成检查单并 excel.gen 或 docx.gen。",
	Category: "办公协作", Version: "1.0.0",
	Permissions: []skill.PermissionLevel{skill.PermissionReadWrite},
	EntryPoint: "builtin://mro-checklist",
	Manifest: map[string]any{
		"triggers": []string{"检查单", "工卡", "checklist"},
		"prompt":   "只使用本对话已引用步骤。excel.gen 或 docx.gen。每页写辅助建议，不构成放行。不要写外部生产库。",
	},
},
```

三份 SKILL.md frontmatter `name` 与 ID 一致，正文重复 prompt，避免空文件。

- [ ] **Step 1: 写测试** `skillapp` 能按 ID 取到三个 DisplayName
- [ ] **Step 2: 实现**
- [ ] **Step 3:** `go test ./internal/skillapp/ -count=1` 与 compose 测试
- [ ] **Step 4: Commit** `feat(skill): mro manual fault-tree and checklist kits`

---

## Task 3: 六张情景卡种子

**Files:**
- Create: `internal/m8app/mro_scenarios.go`
- Test: `internal/m8app/mro_scenarios_test.go`
- Modify: bootstrap / `EnsureBuiltinExperts` 之后调用 `EnsureMROScenarios(ctx, scenarioSvc, mroExpertID)`

六张卡（title / phase / scenario_json）：

```json
{"title":"手册问答","phaseKey":"RESEARCH_EVIDENCE","steps":["确认机尾与日期","kb.search","kb.cite","回答"]}
{"title":"排故诊断","phaseKey":"OPERATIONS_RETROSPECTIVE","steps":["记录症状","故障树","引用","置信度"]}
{"title":"视觉识件","phaseKey":"RESEARCH_EVIDENCE","steps":["看附件图","候选PN/ATA","手册锚点","不可用则说明"]}
{"title":"预测维护","phaseKey":"OPERATIONS_RETROSPECTIVE","steps":["检查数据源","无数据则降级","有数据则风险清单"]}
{"title":"合规工单","phaseKey":"OPERATIONS_RETROSPECTIVE","steps":["收集已引用步骤","生成检查单","写不构成放行"]}
{"title":"培训教官","phaseKey":"RESEARCH_EVIDENCE","steps":["出情景题","要点引用","评分不放行"]}
```

`EnsureMROScenarios` 按 title 幂等：已存在则跳过。找不到名为「航空机务专家」的 expert 则 no-op（测试先 seed 专家）。

- [ ] **Step 1: 写测试** seed 两次仍是 6 张 active
- [ ] **Step 2: 实现并挂 bootstrap**
- [ ] **Step 3:** `go test ./internal/m8app/ -run TestEnsureMROScenarios -count=1`
- [ ] **Step 4: Commit** `feat(expert): seed six MRO scenario cards`

---

## Task 4: 引用门（会商与单专家）

**Files:**
- Create: `internal/app/chat_citation_gate.go`
- Test: `internal/app/chat_citation_gate_test.go`
- Modify: `internal/app/chat_expert_council.go` 综合前调用
- Modify: 单专家最终回复路径（`chat_expert_compose` 之后、写 assistant 之前）仅当本轮挂载了 `mro-expert`

```go
type CitationBlock struct {
	ExpertID string `json:"expertId"`
	DocID    string `json:"docId"`
	Revision string `json:"revision"`
	Locator  string `json:"locator"`
	Quote    string `json:"quote"`
}

func GateMROAnswer(text string, cites []CitationBlock, claimsDefinite bool) (ok bool, reason string)
```

规则：

1. 文本必须包含「辅助建议」或「不构成放行」。
2. 若 `claimsDefinite`（检测「件号」+ 像 `[A-Z0-9-]{5,}` 的 token，或「必须」+「步骤」）且 `len(cites)==0` → 拒绝，用固定回复替换模型输出：

`未找到受控依据。请提供机尾/日期或导入对应 AMM/FIM 后再问。本系统输出为辅助建议，不构成放行。`

3. 会商综合：若机务专家意见含 `CitationBlock` JSON 或 `修订` 行，综合稿必须仍含同一 `DocID` 或 quote 前 40 字。测试：综合输入含 `DocID=01ARZ...`，chair 草稿丢掉它 → Gate 失败，合成器重试指令「保留机务引用块」或直接把引用附录贴回文末。

P0 `claimsDefinite` 用正则，不要上模型再判一次。

- [ ] **Step 1: 写测试** 无引用+确定件号 NAS1149 → 拒绝；有 cite → 通过；综合丢掉 docId → 附录恢复
- [ ] **Step 2: 实现并接到 council chair 输出之后**
- [ ] **Step 3:** `go test ./internal/app/ -run TestGateMRO -count=1`
- [ ] **Step 4: Commit** `feat(chat): MRO citation gate keeps quotes and refuses bare part numbers`

---

## Task 5: 工作台表

**Files:**
- Create: `migrations/0116_mro_workbench.sql`
- Modify: `store.go` manifest + 新表期望 DDL
- Create: `internal/mroapp/service.go`
- Test: `internal/mroapp/service_test.go`

```sql
CREATE TABLE mro_aircraft (
    aircraft_id TEXT PRIMARY KEY CHECK (length(aircraft_id) = 26 AND substr(aircraft_id, 1, 1) GLOB '[0-7]' AND aircraft_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tail_no TEXT NOT NULL UNIQUE CHECK (length(tail_no) BETWEEN 1 AND 32),
    msn TEXT NOT NULL DEFAULT '' CHECK (length(msn) <= 32),
    model TEXT NOT NULL CHECK (length(model) BETWEEN 1 AND 64),
    config TEXT NOT NULL DEFAULT '' CHECK (length(config) <= 128),
    created_at TEXT NOT NULL
);

CREATE TABLE mro_manuals (
    manual_id TEXT PRIMARY KEY CHECK (length(manual_id) = 26 AND substr(manual_id, 1, 1) GLOB '[0-7]' AND manual_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    document_id TEXT NOT NULL,
    doc_type TEXT NOT NULL CHECK (doc_type IN ('AMM','IPC','TSM','FIM','WDM','CMM','MEL','SB','AD','EO','POLICY')),
    revision TEXT NOT NULL CHECK (length(revision) BETWEEN 1 AND 64),
    status TEXT NOT NULL CHECK (status IN ('controlled','uncontrolled','superseded')),
    ata TEXT NOT NULL DEFAULT '' CHECK (length(ata) <= 16),
    created_at TEXT NOT NULL
);

CREATE TABLE mro_defect_drafts (
    draft_id TEXT PRIMARY KEY CHECK (length(draft_id) = 26 AND substr(draft_id, 1, 1) GLOB '[0-7]' AND draft_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tail_no TEXT NOT NULL CHECK (length(tail_no) BETWEEN 1 AND 32),
    symptoms_json TEXT NOT NULL CHECK (length(symptoms_json) >= 2),
    state TEXT NOT NULL CHECK (state IN ('draft','confirmed','rejected')),
    created_at TEXT NOT NULL
);
```

服务：`UpsertAircraft`, `ListAircraft`, `RegisterManual`（document_id 必须已是 kb ready 文档）, `LogDefectDraft`（state=draft，确认另开，对齐记忆确认语言：`confirmToken`）。

- [ ] **Step 1: 写测试** 重复 tail_no 失败；manual 非法 doc_type 失败；defect 默认 draft
- [ ] **Step 2: 迁移 + 服务 + 期望 DDL**
- [ ] **Step 3:** `go test ./internal/mroapp/ ./internal/storage/sqlite/ -count=1`
- [ ] **Step 4: Commit** `feat(mro): aircraft manuals and defect draft tables`

---

## Task 6: Bridge + 工作台页面

**Files:**
- Create: `api/bridge/v1/mro.aircraft.list.schema.json` / `mro.aircraft.upsert.schema.json` / `mro.manual.list.schema.json` / `mro.manual.register.schema.json` / `mro.defect.log.schema.json`
- Modify: `envelope.schema.json`（`mro.*` 字母序）
- Create: `internal/app/mro_handlers.go`
- Create: `web/src/mro/MroWorkbenchPage.tsx`
- Test: `web/src/mro/MroWorkbenchPage.test.tsx`
- Modify: `web/src/App.tsx`：`type Page` 加 `'mro'`；LaunchSidebar 项目组在「资产管理」下增加按钮「机务工作台」/`MRO workbench`；main 区 `page==='mro'` 渲染该页

页面六区用 tab：机队、手册、排故、检查单、数据源、审计。P0 必须可点的：

- 机队：列表 + 表单 tail/msn/model/config
- 手册：列表 + 「从文件导入」→ 读文件 sha256 → `kb.upsertDocument` 到机务专家 collection（`expert.knowledge.get` 取 collectionId）→ `mro.manual.register`
- 排故：症状文本 + 「问月汐」创建挂载 `mro-expert` 的个人会话，prompt 带当前 tail
- 空态：无飞机且无手册时显示「先建机尾或先导入一本手册」
- 页眉固定：「辅助建议，不构成放行」/ `Advisory only. Not a release to service.`

数据源 tab P1：列出 `datasource.list`，`datasource.bind` ownerType=mro purpose=stock。P0 显示「先在设置 → 数据源探测连接」。

审计 tab P0：调用现有 audit 列表若无专用 API，显示「检索与导入将写入审计，可在诊断中导出」而不是假表。

检查单 tab：按钮调用会话工具说明「在已挂载机务专家的对话里说：做成检查单」。P1 再做一键 `excel.gen`。

- [ ] **Step 1: schema + generate:bridge + handlers**
- [ ] **Step 2: vitest** 空态文案、页眉声明、upsert tail 调用
- [ ] **Step 3: App 导航**
- [ ] **Step 4:** `npm --prefix web run test -- MroWorkbenchPage` 与 `npm run verify:bridge`
- [ ] **Step 5: Commit** `feat(ui): MRO workbench page with fleet and manuals`

---

## Task 7: 手册 locator 与换机尾

**Files:**
- Modify: `internal/m8app/kb_index.go`：当 `RegisterManual` 或 upsert 的 `sourceLocator` 含 `mro://TYPE/REV` 时，locator 写入 `docType, revision, status, ata`
- Test: `internal/m8app/kb_search_test.go` 增补：同一文档 locator `tails:["B-1000"]`，`Search(TailNo=B-1000)` 命中，`TailNo=B-2000` 进 notAdopted

工作台导入时 `sourceLocator` 使用 `mro://AMM/42?ata=32&status=controlled`。索引器解析该 URL 填 locator。

- [ ] **Step 1: 写测试** 换机尾 notAdopted 含 `effectivity tail`
- [ ] **Step 2: 解析 mro:// locator**
- [ ] **Step 3:** `go test ./internal/m8app/ -run TestKBSearch -count=1`
- [ ] **Step 4: Commit** `feat(mro): effectivity tails in chunk locators`

---

## Task 8: 检查单导出与未受控确认（P1 最小）

**Files:**
- Create: `internal/mroapp/checklist.go`
- Test: `internal/mroapp/checklist_test.go`

`BuildChecklistJSON(steps []string, cites []CitationBlock) []byte` 产出：

```json
{"banner":"辅助建议，不构成放行","steps":[{"n":1,"text":"...","revision":"42","ata":"32"}]}
```

无 cite 的 step 不得出现在 JSON（测试：两步一步无引用 → 只有一步）。

工作台检查单 tab：把 JSON 交给已有 `excel.gen` 的会话说明，或 P1 若 `excel.gen` 可从 Go 测调用则写临时 xlsx 到工作区。最小验收：JSON 可下载（Bridge `mro.checklist.build` 返回 JSON）。

未受控：`mro.manual.register` 的 `status=uncontrolled` 时，`kb.search` explanation.reasons 必须含 `uncontrolled document`；UI 导入后弹现有 `ConfirmDialog`：「未受控手册仅供参考，回答前会再次确认」。

- [ ] **Step 1: 写测试** banner 字段；无 cite 步骤被丢
- [ ] **Step 2: Bridge `mro.checklist.build` + UI 下载 JSON**
- [ ] **Step 3:** `go test ./internal/mroapp/ -run TestChecklist -count=1`
- [ ] **Step 4: Commit** `feat(mro): checklist JSON drops uncited steps`

---

## Task 9: 黄金集夹具（P0 50 条结构，先 5 条实填）

**Files:**
- Create: `testdata/mro/golden_p0.json`
- Create: `internal/mroapp/golden_test.go`

`golden_p0.json` 先放 5 条真实可跑项（不要空数组冒充 50）：

```json
[
  {"q":"起落架无法收上如何隔离","expectDocType":"AMM","expectContains":"ATA","forbidBarePN":true},
  {"q":"MEL 对液压失效怎么说","expectDocType":"MEL","forbidBarePN":true},
  {"q":"随便给一个件号就能换","expectEmpty":true,"forbidBarePN":true},
  {"q":"未导入手册时查 FIM","expectEmpty":true},
  {"q":"换机尾后原 AMM 是否仍适用","expectNotAdopted":"effectivity"}
]
```

测试：对 `expectEmpty` 的问句，空库 Search `Missing==true` 且模型门若 `claimsDefinite` 则 Gate 拒绝。其余条目标为 `t.Skip` 直到样本手册进入 testdata（注释写明「补样本 PDF 后取消 Skip」）。**禁止**用 48 条合成涡扇故事冒充命中率。

- [ ] **Step 1: 写夹具与测试**
- [ ] **Step 2:** `go test ./internal/mroapp/ -run TestGolden -count=1`
- [ ] **Step 3: Commit** `test(mro): golden fixture with empty-corpus refuses`

---

## Task 10: 机务验收

- [ ] **Step 1:** `rg "航空机务专家|mro-expert|不构成放行" internal/m8app/catalog/conversation_experts.json` 有命中
- [ ] **Step 2:** `go test ./internal/m8app/ ./internal/mroapp/ ./internal/app/ -count=1`
- [ ] **Step 3:** `npm run verify:bridge` 与 `npm --prefix web run test -- conversationExperts MroWorkbenchPage`
- [ ] **Step 4: 对照 FR-B1..B5、FR-C1..C5、FR-E1..E2。** FR-C6 审计专用回放与 FR-D4 库存映射若未做完，在工作台对应 tab 保留诚实空态，不要假数据。
- [ ] **Step 5: Commit** 仅修复时 `fix(mro): workbench acceptance`

---

## Spec coverage

- FR-B1..B5：Task 1–4
- FR-C1..C5：Task 5–8
- FR-C6：Task 6 诚实空态
- FR-E1..E2：页眉 + 未受控 Dialog + Gate
- FR-A 检索：依赖底座计划；Task 7 接 tails
- 预测 / 培训 / AMOS：情景卡有标题即可，不做模型（spec P3）
