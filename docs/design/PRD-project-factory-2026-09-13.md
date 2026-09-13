# 项目管理工厂升级 PRD：生成 → 规范 → 库表 → 工作台 → 发布

**版本：** 1.0  
**日期：** 2026-09-13  
**状态：** 待评审。批准前不改业务代码、不写实施计划。批准后先写 **F1** 实施计划，再按切片开工。  
**产品名：** 月汐项目管理工厂（ITM Project Factory）  
**对照现码：** 工作区已落地的脊椎 PRD（根 / 树 / 开发清单 / 三执行器 / 测试退回开发），以及交付物面板、专家委员会、`user.ask`、Registry、Release。  
**前序合同（继续有效，不得推翻）：** [PRD-project-workbench-spine-2026-09-13.md](./PRD-project-workbench-spine-2026-09-13.md)  
**配套设计指针：** [../superpowers/specs/2026-09-13-project-factory-design.md](../superpowers/specs/2026-09-13-project-factory-design.md)

**本文的 100% ≠ 脊椎已完成。** 脊椎是本厂的地基。本文是地基之上的工厂升级。  
**本文写完 ≠ 产品已 100%。** 工厂 100% = 下文 F1–F5 五片全部按完成定义验收通过。

---

## 0 本文解决什么问题

用户要的「项目管理」不是「聊天 + 九张卡片 + 点确认」，也不是「进工作台再选仓库」。完整主链是：

1. **需求架构规范**：所选专家互相讨论，或像 Claude / Cursor 一样一步一步引导选择；最终产出 **9 份交付物**。有资产库模版则按所选模版生成项目专属内容；没有模版则按内置完整格式生成。三关确认后生成项目根下子目录（已由脊椎完成），并把 **技术 / 开发规范写成后续全局固定规则**（写错可改文档，改后重注入）。
2. **方案和 UI 设计**：消费上一阶段产物，同样用专家讨论或引导选择，产出本阶段全部设计交付物（现网锁定 **10 份**，见 §0.2），三关晋级。
3. **数据库**：按数据库设计文档物化真实表，核齐后三关晋级。
4. **接口**：按接口清单 + 接口详细设计，把接口结构化进接口工作台；清单增删改必须同步结构数据并提示「需要再次处理」；逐条对照详细设计处理并自测通过；展示统计。
5. **开发**：必须先有已核齐的库表，且接口阶段已确认；按功能开发清单 + 开发详细设计结构化进开发工作台（脊椎已有打开任务 / 三执行器 / 回写，本包补统计、变更再处理、详细设计绑定、强制自测）；清单增删改同步结构数据。
6. **测试**：汇总全部已完成的接口任务与开发任务；处理模式与接口 / 开发一致；支持约定测试类型；通过则记记录，不通过退回 **开发或接口** 对应条目，改完再测。
7. **集成**：场景由多条已通过的单元测试组成；场景内单元测试未齐不得做集成测试；失败退回开发 / 接口，修复后必须再过单元测试和集成测试。
8. **打包 / 发布 / 同步**：对整棵项目树与已批准产物打包，本机发布，并同步到用户选定的目标目录。

现码已经有阶段壳、交付物保险柜、三关、专家灌会话、聊天互辩、`user.ask`、清单脊椎、Registry 预览、本地 Release。**缺的是生成链、约束链、物化链、工作台链、验证链。**

---

## 0.1 上一份分析报告复盘（必须改口的地方）

上一份对话分析把覆盖率估在 30%–40%，方向对，但有三处必须在本合同锁死，避免再做成「差不多」。

| 编号 | 分析里的说法 | 复盘 | 本合同 |
|---|---|---|---|
| A1 | 「一次做完整厂会失败，应先问选 A 还是 F1」 | 用户现在要求一份完整可落地总合同，不是再选范围 | 写总 PRD，但 **实施必须按 F1→F5 切片**；每片自己的 100%，禁止一刀写完八步 |
| A2 | 「方案阶段你要 8 份、现码 10 份，待对齐」 | 现网 `PHASE2_DOCS` 与 `RequiredPhaseDocuments` 已是 10 份；砍成 8 会破坏已有项目闸门 | **锁定 10 份**。口头「8 份」视为对设计包的约数，不是砍卡依据 |
| A3 | 「开发是第三步」与「有了库表才能进开发」并列 | 前者是口误或旧口径；后者才是工厂顺序 | **硬顺序：需求 → 方案 → 库表核齐 → 接口确认 → 开发**。软进入后一阶段仍允许看文档，但开发 `task.open` 在库表未 ready 或接口未确认时必须失败 |
| A4 | 「M6 `IntegrationService` 可当接口平台」 | 那是云连接器 / 凭据 / 操作发布，和项目接口清单不是同一产品 | **禁止**把项目管理接口工作台焊进 `m6_integration`。接口工作台是项目内 `WorkBoard(kind=interface)` |
| A5 | 「测试类型要全部自动化」 | 仓库没有压力 / 真机 / 稳定性 runner | 11 类都有 **类型槽位与记录**；其中 5 类自动跑，6 类必须交证据。不装「已自动压测」 |
| A6 | 脊椎 100% 被说成项目管理 100% | 脊椎只覆盖根、树、开发清单、执行器、测试退回开发 | 脊椎 PRD 的 S1–S9 **继续有效**。工厂 100% 另算 F1–F5 |

上一份分析里仍然成立、必须保持的判断：

- 交付物面板今天是证据柜，不是生成工作站。  
- `templateId` 绑定静态文件就能确认，是合规捷径，必须关掉。  
- 专家委员会只出现在聊天流，不写卡片。  
- `dev_standard` / `tech_standard` 不注入后续阶段。  
- `db.query` 只读，不建表。  
- 测试失败只退开发，不退接口。  
- 集成清单不是「已通过单元测试的组合」。  
- Release 是本地制品，不是外部上线。  
- Agent Hub 仍是开发执行器；不焊 `SessionPage`；个人聊天 `\u2063月汐·普通对话` 仍可无根创建。

---

## 0.2 口述需求与现码冲突的锁死项

| 主题 | 口头 | 现码 | 锁死 |
|---|---|---|---|
| 方案交付物数量 | 8 份 | 10 份（含业务蓝图、UI 详细设计、集成测试清单文档） | **10 份** |
| 阶段顺序 | 「开发是第三步」 | 实施/增强：库=3、接口=4、开发=5；运维：库=2、接口=3、开发=4 | 按现码阶段号，**开发不得早于库表核齐与接口确认** |
| 专家怎么工作 | 互辩 **或** 一步一选 | 聊天委员会 + 通用 `user.ask`，无阶段题库，无落盘 | 两种入口都要；生成结果必须写入交付物附件，不能只留在聊天 |
| 模版 | 按所选资产模块生成 | 只存 `templateId`，不实例化 | 生成 = 复制模版字节 + 填空；无模版用内置骨架 |
| 规范 | 全局固定变量 | 两张阶段 1 卡片 | 批准后物化到 `.lunitide/rules/`，并写入 `AGENTS.md` 的托管段；后续阶段会话必注入 |
| 数据库 | 按设计建表并核齐 | 文档 + 只读查询 | SQLite 物化 + 核齐硬闸。MySQL/Postgres **本 PRD 不做** |
| 接口平台 | 清单 CRUD 同步结构 | OpenAPI 预览，不入工作台 | 项目内接口工作台，不接 M6 |
| 测试 | 多类型 + 退回两边 | 手改状态，只退开发 | 类型记录 + 双回退 |
| 发布 | 打包、发布、同步 | 本地包 + dev/stage 晋升 | 整树打包 + 本机晋升 + **同步到用户选的目录**。外部 K8s/云部署 **本 PRD 不做** |

---

## 1 产品定义

### 1.1 一句话

项目管理是本机软件工厂：一个 ITM 项目对应一块已选定的磁盘根；需求与方案由专家讨论或引导选择生成交付物；规范成为后续固定规则；库表按设计建齐；接口、开发、测试、集成共用同一套工作台内核；发布把整棵树打包、本机晋升并同步到选定目录。

### 1.2 不是什么

- 不是推翻脊椎另起炉灶。  
- 不是把八阶段焊成一条 Session。  
- 不是 M6 云集成平台。  
- 不是 grok-build / 官方 IDE 窗口。  
- 不是自动 git。  
- 不是外部生产环境一键上线。  
- 不是全球 `workspace-root.json`。

### 1.3 成功标准（工厂 100% = F1–F5 全真）

| 片 | 覆盖用户步骤 | 完成时必须能看见（不可用「差不多」） |
|---|---|---|
| **F0** | 已落地脊椎 | 根、树、开发清单、月汐/Cursor/Codex、测试退回开发。本 PRD 不重做 |
| **F1** | 步骤 1–2 | 引导选择或专家讨论后，阶段 1 的 9 份、阶段 2 的 10 份以**附件正文**落盘；有模版则按模版生成，无模版则完整骨架；只绑 `templateId` 不能新确认；规范物化并注入后续阶段 |
| **F2** | 步骤 3 | 数据库设计有机器模式；对项目 SQLite `CREATE TABLE`（不删表）；核齐后才能三关；核齐前开发 `task.open` 失败 |
| **F3** | 步骤 4–5 | 接口清单 / 开发清单同步到工作台；统计「总数 / 新增 / 变更 / 未改 / 已做 / 未做」；改清单后提示再处理；开发硬等库表 ready **且** 接口阶段已确认；每条对照详细设计，自测通过才能 complete |
| **F4** | 步骤 6–7 | 测试工作台汇总接口 + 开发已完成项；类型槽位齐全；失败按来源退回开发或接口；集成场景 = 已通过单元测试的组合，未齐不可跑；失败双回退后须再过单元测试和集成 |
| **F5** | 步骤 8 | 整树+已批准产物打不可变包；本机 dev/stage 晋升；同步到用户选定目录并留回执；三关前必须有一次成功同步 |

设计师级脚手架市场、远程 Git、自动 CI、K8s 上线、MySQL/Postgres 建库、真机农场、JMeter 集群，**不是**本成功标准。

### 1.4 目标用户与首发场景（实施/增强）

1. 创建实施项目，根选 `D:\work\商场`（脊椎已有）。  
2. 需求架构规范：点「引导选择」，逐步回答题库；或挂载 ≥2 名专家后点「专家讨论后生成」。  
3. 若资产库有「业务需求分析报告」等模版，先选模版（或「套用本阶段资产模块」）。  
4. 点「生成本阶段交付物」。9 张卡片出现可打开的正文附件，不是空卡。  
5. 人改完三关确认。根下成树（脊椎）。`.lunitide/rules/` 与 `AGENTS.md` 托管段写入开发/技术规范。  
6. 方案阶段同样生成 10 份。功能开发清单、接口清单是结构化 JSON。三关。  
7. 数据库：机器模式来自设计文档或编辑器；物化到 `{root}/.lunitide/data/app.sqlite`；核齐。三关。  
8. 接口工作台从接口清单同步出 I001…；改清单后出现「N 条需要再次处理」。逐条对照接口详细设计，自测通过。统计栏可见。三关。  
9. 进入开发：未核齐库表或接口未确认则「进入开发」禁用。核齐后清单从功能开发清单流入（脊椎）。每条绑定功能详细设计。自测通过才能完成。  
10. 测试工作台出现接口项 + 开发项。T-DEV-001 失败退回 F001；T-API-001 失败退回 I001。  
11. 集成：场景 S01 成员 = 已通过的若干测试条；成员未齐则「开始集成测试」禁用。失败退回对应开发/接口。  
12. 发布：打包整树 → 晋升 stage → 同步到例如 `D:\work\商场-release` → 三关。

运维型：跳过步骤 6 的 10 份方案文档与步骤 11 集成；库/接口/开发阶段号按 `OPERATIONS_PHASES`。

---

## 2 现码事实（对照，不是愿景）

### 2.1 已经有、必须保持

| 能力 | 锚点 | 本包态度 |
|---|---|---|
| 创建必选根、成树、导出交付物 | spine PRD；`projectroot` / `projecttree` / `project_spine_docs.go` | 保持 |
| 九 / 十 / 后继交付物定义与三关 | `deliverableTypes.ts`、`RequiredPhaseDocuments`、`CompleteProjectPhase` | **不改份数**；改的是「确认需要什么证据」 |
| 阶段独立会话 `phase:N:标签` | `projectPhaseSession.ts` | 保持；生成与工作台都挂在当前阶段会话 |
| 专家灌会话 + 委员会互辩 | `phaseExperts.ts`、`chat_expert_council.go` | 保持；**增加**生成落盘 |
| `user.ask` 一步一问 | `UserAskWizard.tsx`、`chat_tool_defs.go` | 保持；**增加**阶段题库，不新发明第二种向导控件 |
| 开发清单 / `task.open|report|complete` / 三执行器 | `projecttask`、`project_spine_handlers.go`、`ProjectDevBar.tsx` | 保持；F3 在其上加统计与闸 |
| 测试退回开发 | `returnFromTest` | 保持；F4 **扩展**退回接口 |
| 阶段 1 `/grill-me` `/to-spec` `/审树` | `devWorkflowChips.ts` | 保持；F1 增加「引导选择 / 生成」按钮，芯片不自动发送 |
| Registry OpenAPI 预览、只读 SQL | `RegistryPanel.tsx`、`m7app.DBQuery` | 保留为辅助；**不再**充当库表物化或接口工作台 |
| 本地 Release 修订 / 打包 / 晋升 | `ReleasePanel.tsx`、`m7_release_handlers.go` | 保持；F5 扩展整树包 + 同步 |
| Hub 硬拆、个人聊天无根 | V2.1；`ValidateCreateBusinessFields` | 保持 |
| `workspaceRepoGuidance` 读 `AGENTS.md` | `repo_guidance.go` | 保持；F1 往托管段写规范，沿这条注入，不另开第二套 prompt 总线 |

### 2.2 和工厂冲突的现码（本包要改）

| 事实 | 锚点 | 后果 |
|---|---|---|
| 确认只要求 `attachmentId` **或** `templateId` | `DeliverablePanel.confirmDoc`、`phaseEvidence` | 空内容也能过关 |
| 聊天要求「请到右侧保存」但无工具可写交付物 | `chat_workflows.go` | 专家产出进不了卡片 |
| 阶段 2 无芯片、无题库 | `chipsForPhase` | 方案阶段不像引导产品 |
| 规范不物化 | 无 `projectrules` | 后续开发看不到阶段 1 规范 |
| `db.query` 只读且常缺连接 | `toolgap.go` `DBQuery` | 建不了表、核不齐 |
| 接口阶段是预览器 | `RegistryPanel` | 无逐条处理、无变更统计 |
| 测试清单只从开发流入 | `seedTestChecklist` / `buildTestItemsFromDev` | 接口工作进不了测试 |
| `lastRunKind` 存执行器名 | `projecttask.Item` | 与测试类型抢字段 |
| 集成清单是普通行 | `PHASE7_DOCS` | 不是场景组合 |
| Release 包只抽部分交付物 | `project_release_content.go` | 不是整树打包，无同步目录 |

### 2.3 两个「接口」必须分名

| 词 | 指什么 | 本 PRD |
|---|---|---|
| 项目接口工作台 | 本项目 `WorkBoard(kind=interface)` | 步骤 4 的唯一主人 |
| M6 集成 | `m6_integration` 云连接器 | 不接线 |
| OpenAPI 预览 | Registry 解析 paths | 可「导入到接口工作台」，不是工作台本身 |

---

## 3 主链（用户可走的唯一幸福路径）

```
创建（必选根）→ 发布 → 工作台
  → ① 需求架构规范
        引导选择 或 专家讨论
        → 套用资产模版（可选）
        → 生成本阶段 9 份附件
        → 人改 + 三关
        → 成树（F0）+ 规范物化（F1）
  → ② 方案和UI设计
        消费阶段 1 正文
        → 同样生成 10 份
        → 三关
  → ③ 数据库
        机器模式 + 物化 SQLite + 核齐
        → 三关（失败不晋级）
  → ④ 接口工作台
        清单同步 → 变更再处理 → 逐条自测
        → 统计可见 → 三关
  → ⑤ 开发工作台
        硬闸：db ready 且接口阶段已确认
        → 清单同步 / 统计 / 绑详细设计 / 自测
        → 三执行器写项目根（F0）
        → 三关
  → ⑥ 测试工作台
        汇总接口 done + 开发 dev_done
        → 类型记录 / 部分自动跑
        → 失败按 sourceKind 退回
        → 三关
  → ⑦ 集成
        场景成员全 test_pass 才能跑
        → 失败双回退 → 再单测再集成
        → 三关
  → ⑧ 发布
        整树打包 → 本机晋升 → 同步到选定目录
        → 三关
```

运维型去掉 ② 与 ⑦，其余同构。

软进入后一阶段（现有衔接警告）**保留**。硬闸只挡：成树失败晋级、库表未核齐的开发打开、接口未确认的开发打开、成员未齐的集成开跑、未同步的发布三关。

---

## 4 领域模型

脊椎已有的 `rootPath` / `ProjectTreeV1` / `ChecklistItem` **不改语义**。本包只增补。

### 4.1 Project 增补字段

| 字段 | 类型 | 约束 |
|---|---|---|
| `rulesDigest` | string | 已物化规范 SHA256，64 hex；未物化为空 |
| `rulesMaterializedAt` | string | RFC3339；未物化为空 |
| `dbStatus` | `none` \| `pending` \| `ready` \| `failed` | 默认 `none` |
| `dbPath` | string | 绝对路径；默认空，物化时写入实际 SQLite 路径 |
| `dbDigest` | string | 已应用 `DatabaseSchemaV1` 的 SHA256；未物化为空 |
| `dbVerifiedAt` | string | 最近一次核齐成功时间 |

关闭/归档不删用户盘上的 SQLite。删除规则仍按脊椎：创建态且无产出才删元数据，不递归删用户文件。

### 4.2 阶段访谈 `PhaseInterviewV1`

路径：`{rootPath}/.lunitide/phase-interview.json`

```json
{
  "version": 1,
  "projectId": "01…",
  "phases": {
    "1": {
      "mode": "guide",
      "completedAt": "2026-09-13T00:00:00Z",
      "answers": [
        { "id": "core_problem", "prompt": "本项目要解决的核心业务问题？", "value": "商场进销存" }
      ]
    }
  }
}
```

`mode`：`guide` \| `council` \| `mixed`。  
每题 `value` 1–2000 字。未完成访谈时允许生成，但卡片顶部必须写「本题库未答完，按已答 + 骨架生成，请人审」。

### 4.3 阶段 1 题库（锁定，实施不得改口另编一套）

一次一题，每题 2–5 个选项 + 界面「其他」（复用 `UserAskWizard`）。

| id | 提示 |
|---|---|
| `core_problem` | 本项目首先要解决什么？ |
| `system_shape` | 系统形态？选项：桌面工具 / Web 应用 / 桌面+本地服务 / 其他 |
| `stack` | 主要技术栈？选项：沿用本仓库习惯 / Go+React / 其他 |
| `data_store` | 数据存在哪？选项：项目内 SQLite / 稍后再定 / 其他 |
| `tree_choice` | 目录树？选项：用默认树 / 我要改树 / 其他 |
| `rule_strictness` | 开发规范？选项：按生成的开发/技术规范严格执行 / 先出草稿我再改 / 其他 |

### 4.4 阶段 2 题库（锁定）

| id | 提示 |
|---|---|
| `main_flows` | 有几条必须先设计的主业务流？选项：1 条 / 2–3 条 / 4 条以上 / 其他 |
| `api_style` | 接口怎么给？选项：OpenAPI REST / 仅内部模块调用 / 两者都要 / 其他 |
| `module_cut` | 功能怎么切？选项：按业务对象 / 按页面 / 按角色 / 其他 |
| `ui_depth` | UI 详细设计要做到哪？选项：线框+状态 / 完整页面说明 / 本阶段先清单 / 其他 |
| `integration_cut` | 集成测试怎么切场景？选项：按主业务流 / 按角色任务 / 本阶段只出清单 / 其他 |

运维型不跑阶段 2 题库。

### 4.5 生成输入与内置骨架

生成一条交付物的输入，按优先级：

1. 用户为本卡选择的资产库模版文件（`templateType=document` 且 `documentType` 对得上 `DELIVERABLE_TEMPLATE_LABEL` 或英文 key）。  
2. 否则用内置骨架 `internal/projectgen/skeletons/{documentType}.md`（清单类为 `.json`）。  
3. 填空：`{{projectName}}` `{{projectCode}}` `{{projectType}}` `{{rootPath}}` `{{date}}` `{{phaseLabel}}` 以及访谈答案 `{{answer.<id>}}`。  
4. 阶段 2 还必须能读到阶段 1 已批准附件的正文（截断到每份 12KiB，总预算 64KiB）。  
5. `mode=council` 时附加最近一次委员会综合意见（截断 8KiB）。

**禁止**只把模版路径写进 `templateId` 而不写附件字节。  
**禁止**生成空文件。骨架最少含：标题、目的、范围、正文小节、修订记录。清单骨架最少含 `version:1` 与 `items:[]`，并在生成说明里写「请补条目」。若访谈已给出可拆的模块，清单必须生成不少于 1 条待办（能拆则拆，不能拆则 1 条「待细化：{core_problem}」）。

阶段级「套用资产模块」：对当前阶段每张还没有 `templateId` 的卡片，按 `documentType` 匹配资产库 **enabled** 模版；一类型多份时取最近更新的一份，并在 UI 列出将套用的名称，人确认后写入各卡 `templateId`，**不**立刻确认交付物。

### 4.6 项目规范 `ProjectRulesV1`

路径：

- `{rootPath}/.lunitide/rules/manifest.json`  
- `{rootPath}/.lunitide/rules/dev-standard.md`  
- `{rootPath}/.lunitide/rules/tech-standard.md`  
- `{rootPath}/.lunitide/rules/biz-standard.md`（有则写）

`manifest.json`：

```json
{
  "version": 1,
  "projectId": "01…",
  "digest": "…64 hex…",
  "source": {
    "dev_standard": { "deliverableId": "01…", "attachmentDigest": "…" },
    "tech_standard": { "deliverableId": "01…", "attachmentDigest": "…" }
  },
  "materializedAt": "2026-09-13T00:00:00Z"
}
```

`AGENTS.md` 托管段（项目根，若文件不存在则创建）：

```
<!-- lunitide:project-rules:start -->
# 本项目规范（月汐托管，勿手改本标记之间的内容；请改阶段 1 交付物后重新物化）

（写入开发规范 + 技术规范的纯文本，合计上限 16KiB，超长从末尾截）
<!-- lunitide:project-rules:end -->
```

规则：

- 只改标记之间。标记外的用户文字必须保留。  
- 阶段 1 三关成功后自动物化一次。  
- 此后用户把 `dev_standard` / `tech_standard` 打回 `review` 并再批准，必须重物化，`rulesDigest` 变化。  
- 后续阶段会话（2–8 / 运维 2–6）在 `chat.start` 时走现有 `workspaceRepoGuidance` 注入 `AGENTS.md`。另加一块 `[项目规范]`，内容来自 `manifest.json` 仍有效的两份 md（各截 4KiB），避免只依赖用户是否打开了仓库根。  
- 个人聊天、无 `projectId` 的会话 **不**注入项目规范。

### 4.7 数据库模式 `DatabaseSchemaV1`

路径：`{rootPath}/.lunitide/schema.json`

```json
{
  "version": 1,
  "dialect": "sqlite",
  "tables": [
    {
      "name": "orders",
      "columns": [
        { "name": "id", "type": "TEXT", "notNull": true, "primaryKey": true },
        { "name": "title", "type": "TEXT", "notNull": true }
      ]
    }
  ]
}
```

规则：

- 表名 / 列名：`^[A-Za-z_][A-Za-z0-9_]*$`，长度 1–64。  
- `type` 仅允许 SQLite 常用声明：`TEXT` `INTEGER` `REAL` `BLOB` `NUMERIC`。  
- 禁止在模式里放 `DROP` / `DELETE` / `ATTACH` / 多语句。  
- 物化只执行 `CREATE TABLE IF NOT EXISTS`（按模式生成，列顺序稳定）。  
- **永不** `DROP TABLE`、永不删列。新增列用 `ALTER TABLE … ADD COLUMN`（列已存在则跳过）。  
- 默认库文件：`{rootPath}/.lunitide/data/app.sqlite`。用户可用 `project.db.bind` 换到根下另一个 `.sqlite` / `.db` 文件，路径必须落在 `rootPath` 内，禁止 `..`。  
- 核齐：模式里每张表都在目标库 `sqlite_master` 中且列名集合 ⊆ 实际列（实际多列允许）。缺表或缺列 → `dbStatus=failed`。  
- 人读的 `db_design` 附件仍要有。机器模式是伴侣，和 `project_structure` + `ProjectTreeV1` 同一模式。

从设计文档提取：若附件中存在 ` ```json ` 且能解析为 `DatabaseSchemaV1`，或存在 ` ```sql ` 且能解析为仅含 `CREATE TABLE` 的语句，则 `schema.put` 可导入。解析失败不晋级，UI 打开模式编辑器。

### 4.8 工作台文档 `WorkBoardV1`

接口 / 开发 / 测试 / 集成 **共用** 此文档。开发清单继续用现有 `ChecklistDoc`（`version:1` + `items`），**兼容读取**：缺省字段当空。写入时允许带上本包新字段。禁止新开四套互不相认的表。

```json
{
  "version": 1,
  "boardKind": "interface",
  "sourceDocumentType": "api_list",
  "sourceDigest": "…",
  "items": []
}
```

`boardKind`：`interface` \| `dev` \| `test` \| `integration`。

条目在现有 `ChecklistItem` 上增补（缺省兼容）：

| 字段 | 含义 |
|---|---|
| `sourceKind` | `dev` \| `interface` \| `manual`。测试 / 集成用来决定退回哪边 |
| `changeKind` | `added` \| `modified` \| `removed` \| `unchanged` |
| `needsReprocess` | 清单同步后是否必须再处理 |
| `sourceFingerprint` | 同步时源条目的标题+关键字段哈希，用于判断 modified |
| `designRef` | `{ "documentType": "api_detail" \| "feature_detail", "hint": "章节或符号，≤200" }` |
| `selfTestPass` | 开发 / 接口 complete 前必须为 true |
| `method` / `path` / `operationId` | 仅接口 |
| `requiredKinds` | 测试条需要哪些类型，默认 `["unit"]` |
| `kindResults` | `{ "unit": { "status": "pass"\|"fail"\|"skipped", "at": "", "summary": "", "evidenceDigest": "" } }` |
| `memberIds` | 仅集成场景：成员测试条 id |
| `returnTargetKind` | 最近一次退回：`dev` \| `interface` |

`removed` 条目保留一行，状态不得当 done；统计计入「变更」侧的删除数，不计入「未做」。人确认删除后可从板中去掉。

**统计（服务端与 UI 同一公式）：**

- `total` = 非 `removed` 的条数  
- `added` / `modified` / `unchanged` / `removed` = 各 `changeKind` 计数（`removed` 按行计）  
- `done` = 接口/开发：`dev_done`；测试/集成：`test_pass`  
- `pending` = `pending`  
- `inProgress` = `in_progress`  
- `returned` = 带 `testReturn` 且未再次 `dev_done`  
- `needsReprocess` = `needsReprocess==true` 的非 removed 条数  

文案固定：「共 {total} 条 · 新增 {added} · 变更 {modified} · 未改 {unchanged} · 已做 {done} · 未做 {total-done} · 待再处理 {needsReprocess}」。

同步算法 `board.sync(boardKind)`：

1. 读源清单（接口：`api_list` 优先，空则 `interface_list` 自身；开发：实施/增强用 `feature_dev_list`，运维用阶段 1 `req_task_list`，与脊椎一致）。  
2. 源条目按 `id` 对齐。源有板无 → 追加 `changeKind=added`，`needsReprocess=true`，`status=pending`。  
3. 源有板有且 fingerprint 变 → `modified` + `needsReprocess=true`；**不**自动改 `dev_done` 为 pending（避免静默丢掉完成态），但 UI 横幅必须出现，三关时若仍有 `needsReprocess` 则失败。  
4. 板有源无 → `removed` + `needsReprocess=true`。  
5. 其余 `unchanged`。  
6. 写回 `sourceDigest`。  
7. 返回统计。  

源清单本身若是 Markdown / OpenAPI 而不是 `ChecklistDoc`：接口允许从 OpenAPI `paths` 生成 id=`I`+三位、title=`METHOD path`。无法解析则 `PROJECT_BOARD_SOURCE_INVALID`，人必须先把清单改成 JSON 或绑定可解析 OpenAPI。

**清单是主人，工作台是投影。** `board.put` 只允许改处理字段（`status`、`notes`、`acceptance`、`designRef`、`selfTestPass`、`executor`、回写、`requiredKinds`、`kindResults`、`memberIds`），**不得**用 put 增删 id。增删改标题/method/path 必须改源清单再 `sync`。OpenAPI 导入必须先写成 `api_list` 或 `interface_list` 附件，再 sync，禁止只把路径画在预览表里冒充已进工作台。

### 4.9 测试类型（锁定名称与实现级别）

| kind | 中文 | v1 实现 |
|---|---|---|
| `unit` | 单元测试记录 | 记录通过/失败（现网主状态） |
| `cli` | CLI 测试 | 在 `rootPath` 跑一条命令，记 exit code；超时默认 60s，最大 300s |
| `link` | 链路测试 | 人工 + 证据附件或日志摘要（≤2000） |
| `stability` | 稳定性测试 | 人工 + 证据 |
| `stress` | 压力测试 | 人工 + 证据 |
| `fluency` | 通畅度测试 | 人工 + 证据 |
| `completeness` | 完整性测试 | 自动：核模式表、接口 path、开发 `targetRelPath` 是否存在 |
| `consistency` | 一致性测试 | 自动：源清单 digest 与 board `sourceDigest` 一致，且无 `needsReprocess` |
| `structured` | 结构化测试 | 自动：接口条目的 method/path 非空；开发条目 title/acceptance 非空 |
| `reuse` | 复用性测试 | 人工 + 证据 |
| `integration` | 集成性测试 | 仅集成板自动/人工；单元板可选记录 |
| `device` | 真机模拟测试 | 人工 + 证据 |

一条测试默认 `requiredKinds=["unit"]`。项目可在测试板头设置默认种类，只影响之后 sync 进来的新条。三关时：每条的 `requiredKinds` 都必须 `pass`（`skipped` 不算）。未列入 required 的种类不挡晋级。

`lastRunKind` **继续表示执行器**（脊椎语义）。测试类型只活在 `kindResults`。禁止再往 `lastRunKind` 里写 `cli`。

### 4.10 集成场景

- 集成板每条是一个场景，`memberIds` 指向测试板条目。  
- 场景 `ready` = 每个 member 存在且 `status=test_pass`。  
- 未 ready 调用开跑 → `PROJECT_INTEGRATION_NOT_READY`。  
- 场景失败：对每个 member 按 `sourceKind` 调用与 `returnFromTest` 相同的退回（同一事务）。  
- 开发 / 接口再 complete 后，对应测试条回 `pending`（脊椎已有）；**同时**包含该测试条的集成场景若已是 `test_pass`，必须打回 `pending`。  
- 阶段 2 的人文档 `integration_test_list` 与阶段 7 的集成板 **不是同一张卡**。阶段 7 可从阶段 2 文档导入场景标题，但成员必须从测试板勾选。

---

## 5 阶段合同

### 5.1 需求架构规范（F1）

入口（阶段 1 交付物面板顶部，芯片旁）：

1. **引导选择**：按 §4.3 弹出 `UserAskWizard`（可预置题，不必等模型调用 `user.ask`）。答完写入 `phase-interview.json`，`mode=guide`。  
2. **专家讨论后生成**：会话已挂载 ≥2 名专家才可点。点后向当前阶段会话发送一条不自动改用户草稿的系统指令：「请就本阶段 9 份交付物互辩并给出可落盘的综合稿」。委员会仍走现有引擎。讨论结束后人再点「生成本阶段交付物」。未满 2 名专家：按钮禁用，文案「请先在专家中心或输入框 @ 至少两名专家」。  
3. **套用资产模块**：§4.5。  
4. **生成本阶段交付物**：对 9 张卡逐张 `generate`。已有 `approved`/`immutable` 的卡跳过。已有附件且人未勾「覆盖草稿」的卡跳过。  

生成后卡片必须能打开正文。确认规则见 §5.9。  
三关成功后：脊椎成树 + 本包规范物化。规范物化失败 → **不晋级**，码 `PROJECT_RULES_FAILED`。

### 5.2 方案和 UI 设计（F1）

与 5.1 同构，题库 §4.4，10 张卡，必须能读阶段 1 已批准正文。  
阶段 2 增加芯片：`/grill-me` `/to-spec` `/to-tickets`（只预填，不自动发送）。  
`api_list`、`feature_dev_list` 必须是可 `parseChecklist` 的 JSON（或 OpenAPI，仅 `api_list`）。  
`feature_detail` / `api_detail` / `db_detail` 必须是完整正文附件。

### 5.3 数据库（F2）

面板不再以 Registry 只读查询当主路径。主路径：

1. 人读 `db_design`（生成或上传）。  
2. `schema.get` / 编辑器（可从附件提取）。  
3. `schema.materialize` → 建库建表。  
4. `schema.verify` → `dbStatus=ready`。  
5. 三关：`db_design` 已批准 **且** `dbStatus=ready`。否则 `PROJECT_DB_INCOMPLETE`。

Registry 的 SQL 框降为「探查」，必须带上 `dbPath`，禁止无目标查询。

### 5.4 接口（F3）

主面板从 Registry 换成 `WorkBoardPanel(kind=interface)`。Registry 仅留「从 OpenAPI 导入到清单」次级入口。

- 进入阶段：若板空，自动 `board.sync`。  
- 清单（`api_list` 或本阶段 `interface_list`）任何 upsert 成功后，前端必须再 `sync`，并弹出「接口清单已更新，请再次处理（待再处理 {n}）」。  
- 打开一条：切到接口阶段（已在则留在）、说明书进输入框（对照 `api_detail` + method/path）、不自动发送。  
- 完成：必须 `selfTestPass=true` 且有 `lastResultSummary`。完成按钮旁默认未勾的「已对照接口详细设计自测通过」；未勾不得调用 complete。  
- 三关：`interface_list` 批准且板内无 `needsReprocess`，且每条非 removed 均为 `dev_done`，或人二次确认「无接口任务」且 `total==0`。

### 5.5 开发（F3，叠在 F0 上）

- 「进入开发」与 `project.task.open`：若 `dbStatus!='ready'` → `PROJECT_DB_REQUIRED`。若接口阶段未完成三关（该 stage 不是 completed/approved）→ `PROJECT_INTERFACE_REQUIRED`。  
- 继续自动导入功能清单（脊椎）。导入后跑一遍 `changeKind` 计算，统计可见。  
- 功能开发清单变更 → 同步开发板 + 横幅「开发清单已更新，请再次处理」。  
- `task.open` 的 brief **必须**附上 `feature_detail` 中与本条 id/title 匹配的一段（找不到则附文档头 2KiB，并写「未精确匹配章节，请对照全文」）。  
- `task.complete` 增加：`selfTestPass` 必须为 true。完成按钮旁默认未勾的「已对照功能详细设计自测通过」。模型自称完成仍不算。  
- 三关：脊椎的全 `dev_done` **加上** 无 `needsReprocess`。

### 5.6 测试（F4）

- 源：接口板所有 `dev_done` + 开发板所有 `dev_done`。id 前缀：`T-Ixxx` / `T-Dxxx`，`sourceId` 指向原 id，`sourceKind` 对应。  
- 处理方式与开发/接口同一套：打开、记录、统计、再处理横幅。  
- `project.test.run`：按 kind 执行 §4.9。  
- `returnFromTest`：若 `sourceKind=interface`，打回接口板对应条（`in_progress` + `testReturn` + 接口交付物若已 approved 则回 review）。`sourceKind=dev` 保持脊椎行为。`manual` 且无 source → `PROJECT_TEST_NO_SOURCE`。  
- 三关：全 `test_pass` 且 required kinds 全 pass，且无 `needsReprocess`。

### 5.7 集成（F4）

- 场景清单；成员从已 `test_pass` 的测试条勾选。  
- 未齐不能跑。  
- 失败双回退 + 场景回 pending。  
- 三关：全部场景 `test_pass`，且测试板此刻仍全 `test_pass`（防止只过集成、单元已被打回）。

### 5.8 发布（F5）

在现有修订 / 打包 / 晋升之上：

1. **打包**：不可变包必须包含（a）已批准交付物字节（现有）；（b）项目树目录清单与 `.lunitide/project-tree.json`、`schema.json`、`rules/manifest.json`、两份工作台 JSON；（c）以 receipt 记录 `rootPath` 下文件的相对路径+SHA256（排除 `node_modules` `.git` 用户可配忽略，默认忽略这两项）。不是把整盘二进制都复制进 SQLite 若单文件 > 4MiB：只记路径和摘要，包内放清单。  
2. **发布**：现有 `release.promote` 到本机 `dev`/`stage`。  
3. **同步**：`project.release.sync`，入参 `destPath`（绝对目录，必须存在且可写，**不得**等于 `rootPath`，不得是 `rootPath` 的祖先）。把包内清单对应的、小于 4MiB 的文件复制到 dest；超大文件只写 receipt「skipped-too-large」。成功写 `{dest}/.lunitide-sync.json`。  
4. 三关：现有 stage 晋升证据 **加上** 至少一条成功 sync receipt。没有 sync → `PROJECT_SYNC_REQUIRED`。

外部部署文案保持：「本地制品，尚未配置外部部署环境。」禁止因本 PRD 把项目状态写成已上线生产。

### 5.9 交付物确认（全阶段，F1 起生效）

新确认（`approved`）必须同时满足：

1. 有 `attachmentId`，且附件字节长度 ≥ 32。  
2. **仅** `templateId` 不得确认。  
3. 清单类附件必须能 `parseChecklist` / `parseWorkBoard`。  
4. `db_design` 确认不代替 `dbStatus`；库阶段三关另查核齐。  
5. 历史已 `approved` 且只有 `templateId` 的卡片 **祖父条款**：保持 approved，直到有人打回重确认。

---

## 6 Bridge / 存储合同

### 6.1 改现有

| 方法 | 变化 |
|---|---|
| `ProjectDTO` | 增加 `rulesDigest` `rulesMaterializedAt` `dbStatus` `dbPath` `dbDigest` `dbVerifiedAt` |
| `project.advanceStatus` / `CompleteProjectPhase` | 阶段 1 成功后物化规范；库阶段加 `dbStatus=ready`；接口/开发/测试/集成加工作台闸；发布加 sync receipt |
| `project.task.open` | 查 `PROJECT_DB_REQUIRED` / `PROJECT_INTERFACE_REQUIRED`；brief 附详细设计摘录 |
| `project.task.complete` | 要求 `selfTestPass` |
| `project.task.returnFromTest` | 按 `sourceKind` 打回开发或接口；集成场景连坐回 pending |
| `deliverable.upsert` | 清单类 upsert 后服务端可返回 `boardDirty:true`（前端必须 sync） |
| `db.query` | 项目工作台调用必须带目标；无目标失败，禁止装成功 |

### 6.2 新增方法（仅这些）

| 方法 | 作用 |
|---|---|
| `project.interview.get` | 读访谈 |
| `project.interview.save` | 写某一阶段 answers + mode |
| `project.deliverable.generate` | 入参 `projectId` `phase` `documentTypes?` `overwriteDrafts`；按 §4.5 写附件并 upsert 为 `review` |
| `project.rules.materialize` | 从已批准规范写磁盘 + DTO |
| `project.rules.get` | 读 manifest + 截断正文 |
| `project.schema.get` / `put` | 机器模式 |
| `project.schema.materialize` | 建目录、建库、CREATE/ADD |
| `project.schema.verify` | 核齐并写 `dbStatus` |
| `project.db.bind` | 绑定根内 sqlite 路径，`dbStatus` 置 `pending` |
| `project.board.get` / `put` | 读写某 kind 工作台 |
| `project.board.sync` | 从源清单对齐 |
| `project.board.stats` | 返回 §4.8 统计 |
| `project.board.item.open` | 接口 / 测试 / 集成打开；开发继续用 `project.task.open`（两者 brief 字段对齐） |
| `project.test.run` | `projectId` `itemId` `kind` 以及 `cli` 时的 `command`（1–500 字） |
| `project.release.sync` | 同步到 `destPath` |

聊天工具（阶段会话、非个人聊天）：

- `deliverable.draft`：入参 `documentType` `title` `markdown`。服务端写成附件并 upsert `review`。这是专家产出进卡片的唯一自动桥。不自动 `approved`。

`agentHub.thread.create` 带 `projectId` 仍锁 cwd（脊椎）。本包不改 Hub 表。

生成方法 **不是** 幂等表新 op；重复生成用 `overwriteDrafts` 显式控制。工作台写入继续复用交付物 upsert 的 attempt，或与 `project.update` 同一策略，**不要**把 `project.board.sync` 登记成新的幂等 op。

### 6.3 库

新迁移 `migrations/0156_project_factory.sql`（LF only）：

- `projects.rules_digest` / `rules_materialized_at`  
- `projects.db_status` / `db_path` / `db_digest` / `db_verified_at`  
- 不新增工作台表。访谈、模式、规则、工作台 JSON 在盘上或交付物附件里。  

`store.go`：manifest checksum + `expectedSchemaSQL` + `expectedColumns`。禁止改旧 `CREATE TABLE projects` 文本冒充已有列。

### 6.4 生成物

改 `api/bridge/v1/*.schema.json` 后跑 `npm --prefix web run generate:bridge`。禁止手改 `web/src/generated/bridge.ts` 与 `internal/bridge/schema_generated.go`。

---

## 7 UI 合同

| 面 | 改动 | 禁止 |
|---|---|---|
| `DeliverablePanel` 阶段 1/2 顶 | 引导选择、专家讨论后生成、套用资产模块、生成本阶段交付物 | 不自动发送聊天；不新造第二套向导皮肤 |
| 交付物卡 | 能打开生成正文；确认检查附件长度 | 只绑模版就亮「已确认」 |
| 阶段 2 | 与阶段 1 相同生成条 + 3 个芯片 |  |
| 库阶段 | `SchemaEditor` + 物化/核齐状态 | 只读 SQL 当主按钮 |
| 接口阶段 | `WorkBoardPanel` + 统计条 + 再处理横幅 | 把 M6 集成页嵌进来 |
| 开发 | 统计条 + 再处理横幅 + 详细设计提示；进入开发受硬闸 | 重写 `SessionPage`；离开工作台进 Hub 首页 |
| 测试 | 同源双列（接口/开发）+ 类型结果 + 跑 CLI | 把 `lastRunKind` 显示成测试类型 |
| 集成 | 场景成员勾选 + ready 指示 | 成员未齐仍显示「测试通过」 |
| 发布 | 增加「同步到目录」与回执 | 宣称已生产上线 |
| `ProjectDTO` 顶栏 | 规范已注入 / 库表 ready 点 |  |

文案：

- 生成中：「正在按模版或完整格式写入交付物，不会自动确认。」  
- 规范：「开发/技术规范已作为本项目固定规则注入后续阶段。改文档后请重新批准并物化。」  
- 开发硬闸：「先完成库表核齐与接口阶段确认，才能开始开发任务。」  
- 再处理：「{平台}清单已更新，{n} 条需要再次处理。」  
- 同步：「同步是复制到你选的目录，不是外部生产发布。」

---

## 8 必须保持 / 明确禁止

### 保持

- 脊椎 S1–S9 与全部错误码。  
- 发布门禁、只读关闭、创建态删除、三关文案。  
- 阶段会话标题格式。  
- Hub V2.1：线程不进 `sessions`/`messages`。  
- 个人聊天精确名 `\u2063月汐·普通对话` 创建不强制 `rootPath`。  
- 不自动 git。  
- 成树只增不删。  
- 模型不得自动 `dev_done` / `approved`。

### 禁止

- 用本 PRD 重做根/树/Hub 熔核。  
- 焊 Agent Hub 进 `SessionPage`，或点开发就 `setPage('agentHub')`。  
- 接口工作台写入 `m6_integration`。  
- 只绑 `templateId` 的新确认。  
- 规范只写在聊天里不当盘上规则。  
- `DROP TABLE` / 按新模式删旧表。  
- 无目标 `db.query` 装成功。  
- 库表未 ready 或接口未确认仍 `task.open` 成功。  
- 测试失败只改备注、或接口失败却打开发。  
- 集成成员未齐当已测。  
- 无同步回执的发布三关。  
- 手改 generated bridge。  
- 一刀实施 F1–F5。

---

## 9 实施切片（可独立验收）

每片先写自己的 writing-plans，再 TDD。禁止把五片写进同一份按小时计的施工单就开工。

| 片 | 刀 | 内容 | 验收 |
|---|---|---|---|
| F1 | G01 | 访谈 get/save + 题库 + UserAskWizard 预置 | 阶段 1 答完 6 题落盘；个人聊天无此条 |
| F1 | G02 | 内置骨架 + generate 写附件 | 无模版时 9 张卡附件 ≥32 字节；已批准卡不覆盖 |
| F1 | G03 | 按 `templateId` 实例化 | 选模版后正文含项目名；确认不再接受裸 templateId |
| F1 | G04 | `deliverable.draft` 工具 + 阶段 2 读阶段 1 | 委员会/聊天可落一张卡为 review |
| F1 | G05 | 规范物化 + AGENTS 托管段 + 阶段 2 会话注入 | 改规范再批准后 digest 变；无项目会话不注入 |
| F1 | G06 | 阶段 2 生成 10 份 + 芯片 | `api_list`/`feature_dev_list` 可 parse |
| F2 | D01 | `DatabaseSchemaV1` 解析/拒绝 | `..`、DROP、非法名失败 |
| F2 | D02 | materialize + verify | 临时盘建表；缺表 verify 失败；二次调用幂等 |
| F2 | D03 | 库阶段三关 + DTO | 未核齐不晋级；`db.query` 带 dbPath |
| F3 | B01 | WorkBoard 同步/统计纯函数 | 增删改未改计数正确 |
| F3 | B02 | 接口板 UI + open/complete + 横幅 | 改清单后 needsReprocess；统计文案固定 |
| F3 | B03 | 开发统计/横幅/designRef/selfTestPass | complete 无自测失败 |
| F3 | B04 | `task.open` 双硬闸 | 无库/无接口确认失败码正确 |
| F4 | V01 | 测试板双源导入 + return 分叉 | 接口失败打接口条 |
| F4 | V02 | `test.run`：cli/completeness/consistency/structured | 命令非 0 → fail；缺表 completeness fail |
| F4 | V03 | 集成 memberIds + 连坐 | 未齐不可跑；失败场景回 pending |
| F5 | R01 | 整树包清单 + 大文件只记摘要 | |
| F5 | R02 | `release.sync` + 发布三关要回执 | dest=root 拒绝；成功写 `.lunitide-sync.json` |

F0 已完成，不列入开工。F2 不得在 F1 规范物化未完成时宣称工厂过半。F3 不得在 F2 未完成时宣称开发平台完成。

---

## 10 测试合同

### 10.1 必须有的自动化

- 访谈：题 id 固定；超长 value 拒绝。  
- 生成：无模版骨架完整；有模版则输出含项目名；不覆盖 approved；空字节失败。  
- 确认：仅 templateId → 失败。  
- 规范：物化写托管段；保留段外用户字；无根失败。  
- 模式：非法 SQL/名拒绝；IF NOT EXISTS 幂等；缺表 verify 失败。  
- 硬闸：db none 时 `task.open` → `PROJECT_DB_REQUIRED`；接口未确认 → `PROJECT_INTERFACE_REQUIRED`。  
- 同步：added/modified/removed/unchanged；三关在 needsReprocess 时失败。  
- 自测：`selfTestPass=false` 不能 complete。  
- 退回：接口源不碰开发条；开发源不碰接口条。  
- 集成：缺 member / member 非 pass → 不可跑。  
- 同步发布：dest 非法；成功 receipt。  
- 个人聊天：无 generate / 无项目规范注入。  
- 前端：统计文案；再处理横幅；生成不自动发送；未探测执行器仍禁用（脊椎）。

### 10.2 不挡本 PRD 100% 的人测

- 真机 Cursor/Codex/真机设备农场。  
- 压力测试是否科学。  
- 专家互辩文笔质量（产品验收「落盘与否」，不验收文采）。  
- 外部生产发布。

---

## 11 错误码（用户可见）

脊椎码全部保留。本包新增：

| 码 | 何时 | 下一步 |
|---|---|---|
| `PROJECT_GENERATE_EMPTY` | 生成结果 < 32 字节 | 换模版或重试生成 |
| `PROJECT_GENERATE_SKIP_APPROVED` | 试图覆盖已批准且未允许 | 先打回再生成 |
| `PROJECT_TEMPLATE_MISSING` | templateId 文件不存在 | 重选模版或改用骨架 |
| `PROJECT_RULES_FAILED` | 规范物化失败 | 检查根可写后重试 |
| `PROJECT_RULES_STALE` | 规范附件 digest 与盘不一致 | 重新物化 |
| `PROJECT_SCHEMA_INVALID` | 模式不合格 | 改编辑器 |
| `PROJECT_DB_BIND_INVALID` | 库路径不在根内 | 重选根内文件 |
| `PROJECT_DB_FAILED` | 建表失败 | 看权限/路径后重试 |
| `PROJECT_DB_INCOMPLETE` | 库三关但未核齐 | 先物化并核齐 |
| `PROJECT_DB_REQUIRED` | 开发打开时库未 ready | 先完成数据库阶段 |
| `PROJECT_INTERFACE_REQUIRED` | 开发打开时接口未确认 | 先完成接口阶段 |
| `PROJECT_BOARD_SOURCE_INVALID` | 源清单无法解析 | 改成 JSON 或可解析 OpenAPI |
| `PROJECT_BOARD_DIRTY` | 三关时仍有待再处理 | 处理完再确认 |
| `PROJECT_SELFTEST_REQUIRED` | 完成条未自测 | 先自测并勾选通过 |
| `PROJECT_TEST_KIND_UNSUPPORTED` | 对该 kind 缺少命令/证据 | 补 command 或证据 |
| `PROJECT_INTEGRATION_NOT_READY` | 场景成员未全过 | 先把单元测试跑齐 |
| `PROJECT_SYNC_INVALID` | 同步目录非法 | 另选目录 |
| `PROJECT_SYNC_REQUIRED` | 发布三关无同步回执 | 先同步 |

---

## 12 与现网文件的落点（禁止借机重构）

| 工作 | 文件 |
|---|---|
| 访谈/生成/骨架 | 新包 `internal/projectgen`（skeletons 内嵌） |
| 规范 | 新包 `internal/projectrules`；`repo_guidance.go` 只加项目块，不改身份逻辑 |
| 模式/物化 | 新包 `internal/projectschema` |
| 工作台同步/统计 | 扩展 `internal/projecttask`（或薄包 `internal/projectboard` 调它），禁止复制一份 checklist |
| 测试 runner | 新包 `internal/projecttestkit`（cli + completeness + consistency + structured） |
| 阶段完成闸 | `project_phase_completion.go`、`project_spine_tx.go` |
| 桥 | `internal/app/project_spine_handlers.go` 旁新 `project_factory_handlers.go`；新 schema；`generate-bridge` |
| 聊天工具 | `chat_tool_defs.go` 加 `deliverable.draft`；`chat.go` 仅阶段会话挂载 |
| UI 生成条 | `DeliverablePanel.tsx`、新 `PhaseGenerateBar.tsx` |
| 题库 | 新 `web/src/project/phaseInterview.ts`，复用 `UserAskWizard` |
| 模式编辑 | 新 `SchemaEditor.tsx`（仿 `ProjectTreeEditor`） |
| 工作台 | 新 `WorkBoardPanel.tsx` 或扩展 `ChecklistPanel.tsx`（优先扩展，文件过大再拆） |
| 统计条 | 新 `boardStats.ts` 与 Go 测同一组样例 |
| 发布同步 | `ReleasePanel.tsx`、`m7_release_handlers.go` 旁新 sync handler |
| 迁移 | `migrations/0156_project_factory.sql` + `store.go` expected |

禁止借本包重写 `SessionPage.tsx`、拆 `OfficeStudioPage`、改 Hub 主键、改个人聊天创建。

---

## 13 非目标

- grok-build 进发布链。  
- ACP 服务端。  
- 嵌 TUI / 官方 IDE。  
- 自动 git。  
- MySQL / PostgreSQL / 远程库建表。  
- 把接口工作台做成 M6 云集成。  
- JMeter / k6 集群、真机设备云。  
- K8s / 客户生产环境发布。  
- 脚手架 zip 市场（仍记后续）。  
- 自进化 SKILL.md。  
- 把办公、月伴、航线迁进项目管理。  
- 统一全球工作区与项目根。

---

## 14 完成定义

### 14.1 本文件作为合同的完成

同时为真：

1. 评审者能按 F1–F5 指出「做什么 / 不做什么 / 用哪条现码 / 失败码」。  
2. 无 TBD / TODO /「视情况」。  
3. 与脊椎 PRD 无冲突：根、树、三执行器、个人聊天、Hub 硬拆均保持。  
4. 方案交付物 10 份、开发顺序库→接口→开发、接口不进 M6、测试双回退、发布要同步，六条锁死无歧义。

### 14.2 产品工厂 100%

F1–F5 的 §9 验收与 §10.1 自动化全绿，且 §1.4 首发场景在测试盘上可复现（外脑可用桩）。  
**缺任何一片，不得对用户说「项目管理需求 100% 落地」。**

本 PRD 完成 ≠ 旧安装包已含此工厂。装机版本以当时签名包为准。

---

## 15 批准后才做的事

1. 评审若改口（例如坚持方案只要 8 份、或必须上 MySQL），先改本文再施工。  
2. 按 writing-plans **只写 F1 计划**（G01–G06）。  
3. F1 验收通过再写 F2 计划，依此类推。  
4. 未改口前禁止按「开发步骤再选仓库」「Hub 另走开发链」「绑模版即确认」「Registry 就算接口平台」施工。
