# 项目管理主链 PRD：根目录 → 阶段成树 → 清单入开发

**版本：** 1.1（追加：开发执行器三选一 + 开发/测试同一条清单闭环）  
**日期：** 2026-09-13  
**状态：** 现行改造合同。完成本文件全部必须条目后，项目管理按**这条主链**完整可使用。  
**对照现码：** 工作区 `0.4.80` 前后（`internal/projectapp`、`internal/domain/project`、`web/src/project/**`、`internal/agenthub`）。  
**前序纠偏：** 同日 grok-build 对照、以及「把编码融进开发步骤」两份分析有岗位偏差；偏差与改口见 §0。  
**配套计划：** 本 PRD 批准后再写 `docs/superpowers/plans/2026-09-13-project-workbench-spine.md`。未批准不改代码。

**1.1 相对 1.0 追加：** S8 开发执行器三选一（月汐 / Cursor / Codex）；S9 开发↔测试同一条清单闭环（回写结果 → 确认完成 → 测试 → 带原因退回再改）。桥增加 `project.executor.set`、`project.task.report`、`project.task.returnFromTest`。实施刀 T13–T15 为 S8/S9 必做。

---

## 0 本文解决什么问题

用户要的项目管理不是「进工作台随便聊，开发步骤再选一个仓库」。主链是：

1. **先建项目，根目录当时就定死。**
2. **第一阶段「需求架构规范」三关确认后，按结构规范自动生成子目录。**
3. **后面几步在这棵树上写文档、做设计、出清单。**
4. **开发检查清单里的每一条任务，对接到「开发」步骤去实现。**
5. **开发步骤里选用执行器：月汐平台、或已对接的 Cursor、或已对接的 Codex。** 无论谁写，结果都回写同一条清单，再确认后进入「测试」。
6. **测试不通过必须退回同一条开发清单（带原因），开发按反馈再改，反复直到通过。** 禁止外脑另走一条互不相认的链路。

现码已经有项目生命周期、八/六阶段、交付物、三关、清单导入、开发芯片和 Agent Hub，但**没有文件系统脊椎，也没有把 Hub 收成开发步骤的执行器**：建项目不绑根，确认第一阶段不建树，清单条目不能打开一轮开发；Cursor/Codex 在办公 Hub 自选文件夹；测试退回只改备注、不重开任务。

因此本文只收两类条目：

1. 已经做成、必须保持的能力。  
2. 还能用工程收口、收口后用户就能沿主链走完的缺口。

**本 PRD 的 100% ≠ 做成 grok-build，≠ 焊 Agent Hub 进 SessionPage，≠ 嵌官方窗口。**

---

### 0.1 前两份方案的偏差（必须改口）

| 前序说法 | 错在哪 | 本合同 |
|---|---|---|
| 开发步骤再选/绑定代码根 | 根目录是**建项目**时定的，不是开发时才选 | 创建必填 `rootPath`；开发步骤只消费它 |
| 项目管理缺的是「外脑编码岗」 | 缺的是树 + 清单对接；外脑必须是开发步骤上的**执行器**，不是另一条产品 | 主链不依赖外脑也能走完；选了 Cursor/Codex 必须回写同一清单 |
| Hub「写项目」自选文件夹 | 和 ITM 项目抢「项目」这个词，会写到另一棵树 | 带 `projectId` 的 Hub 线程**禁止另选根** |
| 会话沙箱 ≈ 项目仓库 | `conversations-root` / `tool-workspaces/<session>` 是对话产物；项目根是工程盘 | 阶段会话写文件默认落到项目根 |
| 功能开发清单和开发清单是两份独立表 | 产品上后者由前者流入 | 进入开发阶段空清单则自动导入，禁止只靠手点「导入」 |

保留仍然成立的前序结论：

- grok-build / Cursor / Kimi / Codex 是编码 Agent 标准件，月汐是工作台；**挂载，不熔核。**
- Agent Hub 数据不得写入 `sessions` / `messages`（V2.1 硬拆仍有效）。
- 不嵌 TUI、不点官方 GUI、不从 grok-build 本树编 Windows 依赖。

---

## 1 产品定义

### 1.1 一句话

项目管理是本机软件工程的**阶段工厂**：一个 ITM 项目对应一块已选定的磁盘根；需求架构确认后长出目录树；各阶段把文档和清单写进对应目录；开发清单的每一条在「开发」步骤里被做完——执行器可选月汐、Cursor 或 Codex——结果回写同一条清单；确认开发完成后进入测试；测试不通过带着原因退回同一条目再改，直到通过。

### 1.2 不是什么

- 不是全球 `workspace-root.json` 的别名。  
- 不是 Agent Hub 的「选个文件夹写项目」。  
- 不是会话工具目录里的临时沙箱。  
- 不是 git 产品（不自动 init / commit / push）。  
- 不是把八阶段对话焊成一条 Session。  
- 不是 M7 `devTask.*` 证据链的替换品（那条链保持独立；本 PRD 不强制接线）。

### 1.3 成功标准（本 PRD 的 100%）

同时满足下面九条，才算完整可使用：

| # | 标准 | 不可用「差不多」替代 |
|---|---|---|
| S1 | 建项目就有根 | 创建表单必须选出存在的本地目录；未选不能保存；`ProjectDTO.rootPath` 非空 |
| S2 | 确认需求架构才成树 | 第一阶段三关成功后，根下出现合同目录；失败则项目状态**不晋级** |
| S3 | 后继阶段写在树上 | 交付物确认后，对应阶段目录里能看到导出副本；不是只存在 SQLite 附件库 |
| S4 | 清单自动流入开发 | 进入开发步骤时，开发清单来自功能开发清单（实施/增强）或本阶段维护（运维）；条目可点 |
| S5 | 一条任务对上开发 | 点清单条目会切到开发步骤、带上该条标题/验收/目标路径，并能写回 `in_progress` / `dev_done` |
| S6 | 写代码落在项目根 | 开发步骤里月汐改文件、Cursor/Codex 改文件，cwd 都是 `rootPath`；禁止默默写到 Hub 默认目录或会话沙箱还对用户说「已在本项目开发」 |
| S7 | 无死胡同 | 根被删、树部分存在、清单为空、所选执行器未装，都有可见原因和下一步；禁止空成功 |
| S8 | 开发执行器三选一 | 开发步骤可选用月汐 / Cursor / Codex；未探测到的选项禁用并说明原因；选定后打开任务即走该执行器 |
| S9 | 开发↔测试同一条链 | 确认开发完成才能进测试；测试不通过带着原因退回**同一**开发条目；再开发 prompt 含测试反馈；再完成后对应测试条回到待测。无论谁执行，回写的都是这份清单 |

设计师级脚手架市场、远程 Git 托管、自动 CI、路径寻址子代理树、Kimi/Grok 一等入口，**不是**本成功标准。Kimi/Grok 若已 `available`，可走与 Cursor 相同插座，但不出现在默认三选一文案里。

### 1.4 目标用户与首发场景

本机做实施/增强/运维项目的人：先立项，再按阶段出规范，最后按清单改这块盘上的代码。

首发走通一条：

1. 创建实施项目，根选 `D:\work\商场`。  
2. 发布 → 进入工作台 → 需求架构规范：九份交付物齐，其中「项目结构规范」给出树（或用默认树）。  
3. 三关确认。根下出现 `docs/01-需求架构规范` … `src` 等目录，以及 `.lunitide/project-tree.json`。  
4. 方案和 UI 设计维护「功能开发清单」F001…。  
5. 库表、接口阶段出设计文档，导出进对应目录。  
6. 进入开发：开发清单自动出现 F001…。顶栏选「月汐 / Cursor / Codex」。点 F001 → 按所选执行器打开；写在 `D:\work\商场`；回写本条结果后标 `dev_done`。  
7. 全部开发条完成 → 三关确认进入测试。测试清单从开发清单生成。T001 不通过 → F001 回到进行中并带上失败原因。  
8. 切回开发，打开 F001：prompt 含测试反馈；仍可用原执行器或另选。改完再标 `dev_done`，T001 回到待测。反复直到测试通过。

---

## 2 现码事实（对照，不是愿景）

### 2.1 已经有、必须保持

| 能力 | 锚点 | 保持 |
|---|---|---|
| 创建 / 发布 / 关闭 / 重开 / 删除门禁 | `ProjectPage.tsx`、`project_fsm.go` | 创建态不能进工作台；发布后才进 |
| 实施/增强八阶段、运维六阶段 | `projectPhases.ts` | 阶段标签与序号不改 |
| 阶段独立会话 `phase:N:标签` | `projectPhaseSession.ts` | 不把各阶段焊成一条会话 |
| 阶段交付物 + 三关晋级 | `DeliverablePanel.tsx`、`CompleteProjectPhase` | 证据门禁保持；晋级仍走 `project.advanceStatus` |
| 第一阶段九份文档（含「项目结构规范」） | `deliverableTypes.ts` `PHASE1_DOCS`、`RequiredPhaseDocuments` | 文档还在；**额外**变成可执行树 |
| 功能开发清单 → 开发清单手导 | `ChecklistPanel` `importFrom` | 升级为自动导入，按钮留作补导 |
| 测试失败退回开发 | `rollbackTestFailToDev` | **升级**：服务端回写 + 重开任务 + 反馈进 prompt；禁止只改备注 |
| 开发芯片 / 阶段技能注入 | `devWorkflowChips.ts`、`chat_workflows.go` | 芯片改为对**当前任务**发送，不只填框 |
| 工作区代码页 | `Workspace.tsx` `code` | 开发步骤默认打开，且根是项目根 |
| Hub 独立模块 | `2026-09-13-agent-hub-v2-sessions.md` | 数据硬拆保持 |

### 2.2 和主链冲突的现码

| 事实 | 锚点 | 后果 |
|---|---|---|
| `Project` / `project.create` 没有根路径 | `project.go`、`project.create.schema.json` | 建项目只建了元数据 |
| 确认第一阶段只改状态 + 冻交付物 | `CompleteProjectPhase`、`DeliverablePanel.finalize` | 磁盘上不长树 |
| 「项目结构规范」只是可上传文档 | `PHASE1_DOCS.project_structure` | 规范不驱动 `mkdir` |
| 资产有 `scaffold` 模版，项目不用 | `internal/domain/asset/template.go` | 脚手架和阶段脱节 |
| 阶段对话写在会话工具目录 | `conversationsapp`、`toolruntime` | 「开发」改不到用户工程 |
| 开发清单与任务无执行外键 | `ChecklistItem` 只有 title/status | 点条目不能开一轮开发 |
| Hub `write_project` 自选 `workspaceRoot` | `thread_service.go` | 和 ITM 项目不是同一棵树；开发步骤必须锁死项目根 |
| `plan.run` 明确「独立 session 工作区，不写回用户源码」 | `phase2-projects.md` | 协调计划不能冒充本 PRD 的开发对接 |
| 全局 `workspace-root.json` | `internal/config` | 不能当项目根 |

### 2.3 两个「项目」必须分名

| 词 | 指什么 | 本 PRD |
|---|---|---|
| ITM 项目 | `projects` 行，编号 ITMxxxxx | 唯一主人：根目录 + 阶段 + 清单 |
| 项目根 | `rootPath` 这块盘 | 建项时选定，此后只换不另建（换根是显式操作） |
| Hub 写项目 | 外脑在某个 cwd 里写文件 | 仅当 `projectId` 为空时才自选文件夹 |
| 会话工作区 | 对话附件 / 工具沙箱 | 个人聊天继续用；工作台阶段会话让位给项目根 |

---

## 3 主链（用户可走的唯一幸福路径）

```
创建（必选根目录）
  → 发布（立项，根已锁定）
  → 进入工作台
  → ① 需求架构规范：九份交付物 + 结构树（默认可生成）
  → 三关确认 → 引擎 mkdir 子目录（失败不晋级）
  → ② 方案和UI设计：文档 + 功能开发清单
  → ③ 数据库 / ④ 接口：设计落入对应目录
  → ⑤ 开发：自动接过功能开发清单；选月汐 / Cursor / Codex；逐条打开、回写、确认完成
  → ⑥ 测试：从开发清单生成测试项；不通过带着原因退回同一开发条 → 再开发 → 再测（轮次不限）
  → ⑦ 集成 / ⑧ 发布：保持现有门禁
```

运维型跳过方案/UI 与集成；开发是第 4 步。目录默认树同步缩短（§5.3）。

阶段仍允许「软进入后一阶段」（现有衔接警告）。**但**：未确认第一阶段，不得声称树已生成；未生成树，开发任务不得对用户显示「已在本项目根开发」。

---

## 4 领域模型

### 4.1 Project 增补字段

| 字段 | 类型 | 约束 |
|---|---|---|
| `rootPath` | string | 绝对本地路径；创建后到关闭前不可空；长度 1–1024 |
| `treeStatus` | `none` \| `pending` \| `ready` \| `partial` \| `failed` | 默认 `none` |
| `treeDigest` | string | 树清单规范 SHA256，64 hex；未成树为空 |
| `treeGeneratedAt` | string | RFC3339；未成树为空 |
| `defaultExecutor` | `lunitide` \| `cursor` \| `codex` | 开发步骤默认执行器；默认 `lunitide` |

关闭/归档不删盘。删除仅限创建态且无产出：**若根下已有 `.lunitide/project.json` 且仅含本项目锁，删除元数据，不递归删用户文件。**

### 4.2 树清单 `ProjectTreeV1`

机器可执行，和给人看的「项目结构规范」文档成对出现。

```json
{
  "version": 1,
  "dirs": [
    "docs/01-需求架构规范",
    "docs/02-方案和UI设计",
    "docs/03-数据库",
    "docs/04-接口",
    "docs/05-开发",
    "docs/06-测试",
    "docs/07-集成",
    "docs/08-发布",
    "src",
    "tests",
    "deploy"
  ],
  "phaseMap": {
    "1": "docs/01-需求架构规范",
    "2": "docs/02-方案和UI设计",
    "3": "docs/03-数据库",
    "4": "docs/04-接口",
    "5": "src",
    "6": "docs/06-测试",
    "7": "docs/07-集成",
    "8": "docs/08-发布"
  },
  "codeRoot": "src"
}
```

规则：

- 所有路径相对 `rootPath`，禁止 `..`、盘符、UNC。  
- `dirs` 只建目录，不建文件（除 `.lunitide/*` 管理文件）。  
- `phaseMap` 的值必须是 `dirs` 中某一项或子路径。  
- `codeRoot` 默认 `src`，必须在 `dirs` 里。

### 4.3 默认树（结构规范未给出机器树时）

实施/增强：§4.2 示例。  
运维：去掉 `docs/02-方案和UI设计`、`docs/07-集成`；`phaseMap` 按六阶段重编号（阶段 2→数据库，3→接口，4→`src`，5→测试，6→发布）。

第一阶段聊天必须提示：可改默认树；不改则确认时按默认生成。引擎在晋级前把实际采用的树写入 `.lunitide/project-tree.json`。

### 4.4 根锁文件

`{rootPath}/.lunitide/project.json`：

```json
{
  "projectId": "01…",
  "projectCode": "ITM00001",
  "name": "商场",
  "boundAt": "2026-09-13T00:00:00Z"
}
```

创建时写入。另一 ITM 项目不得绑同一 `rootPath`（比锁、比库唯一索引）。用户手删锁文件但库仍占用该路径 → 进入工作台时重建锁。

### 4.5 清单条目增补（兼容旧 JSON）

`ChecklistItem` 现有字段保持。新增可选字段，缺省视为未对接：

| 字段 | 含义 |
|---|---|
| `acceptance` | 验收标准（纯文本，≤ 2000） |
| `targetRelPath` | 相对项目根的主要工作目录或文件，默认 `codeRoot` |
| `executor` | 本条最近选用的执行器：`lunitide` \| `cursor` \| `codex`；缺省用项目 `defaultExecutor` |
| `workSessionId` | 月汐阶段会话中打开过该任务（可空） |
| `hubThreadId` | Cursor/Codex 线程（可空） |
| `lastRunKind` | `lunitide` \| `cursor` \| `codex` \| `none` |
| `lastResultSummary` | 最近一次回写的结果摘要（≤ 2000），月汐/Cursor/Codex 共用 |
| `lastResultAt` | 最近回写时间 |
| `testReturnId` | 最近一次把它打回的测试条目 id |
| `testReturn` | `{ id, reason, at }`；`task.open` 的 brief 读这个对象，不靠解析 notes |
| `openedAt` | 最近一次「进入开发」 |

旧清单无这些字段仍能显示；打开任务时补默认值并保存。测试清单条继续用 `sourceId` 指向开发条 id，这是闭环主键。

---

## 5 阶段合同

### 5.1 创建

`project.create` **增加必填** `rootPath`（前端创建对话框必选文件夹）。

服务端：

1. 规范化为绝对路径；必须已存在且为目录。  
2. 可写（探测 `.lunitide` 能否创建）。  
3. 无其它项目占用；无外项目锁文件。  
4. 同一事务：插 `projects` 行 + 写锁文件。写锁失败则回滚创建。  
5. **此时不生成业务子目录。** `treeStatus=none`。

前端：创建表单在「C 项目名称」旁增加「项目根目录 *」，按钮复用本机选文件夹对话框。文案：「根目录现在选定，第一阶段确认后才会生成子目录。」

发布（`project.publish`）不改根、不成树。

### 5.2 需求架构规范（阶段 1）

保持九份交付物。新增：

- 「项目结构规范」卡片增加「编辑目录树」：表格增删 `dirs`，预览将创建的路径。保存为附件 `project-tree.v1.json`，并挂到该交付物（可与给人看的 docx/md **同时**存在：`attachmentId` 仍是人文档；机器树另存 `projectAttachment` category=`project_tree`，或交付物增加可选 `treeAttachmentId`）。  
- **落地简化（本 PRD 采用）：** 交付物 `project_structure` 若绑定的附件 MIME 为 `application/json` 且能解析为 `ProjectTreeV1`，用之；否则晋级时用默认树。人文档仍可另传。  
- 阶段聊天注入：除现有 grill-me / to-spec 外，明确「确认本阶段会按结构规范在项目根生成目录，请先审树」。

### 5.3 三关确认 → 成树（硬闸）

`project.advanceStatus` 且 `phase===1` 时，在 `CompleteProjectPhase` **之前**：

1. 解析树（附件或默认）。  
2. `mkdir` 全部 `dirs`（已存在则记 `existed`，不删、不清空）。  
3. 写 `.lunitide/project-tree.json` 与 `.lunitide/tree-receipt.json`（created / existed / failed）。  
4. 任一目失败 → 返回 `PROJECT_TREE_FAILED`，**不晋级**。  
5. 成功 → `treeStatus=ready`，再走现有 `CompleteProjectPhase`。

幂等：同一树摘要再确认，只补缺失目录。改树后的再确认只增不删。

**禁止**用脚手架 zip 覆盖已有文件。可选：资产库已启用的 `scaffold` 模版在成树**之后**解压到根，冲突文件跳过并写入 receipt.`skipped`。本 PRD **第一刀不做 zip 解压**（§16）；默认树已够 S2。

### 5.4 方案 / 库 / 接口（阶段 2–4）

- 确认某份文件交付物时：把附件导出一份到 `phaseMap[phase]/<documentType>-<safeTitle>.<ext>`。已有同名则加 `-v{n}`，不覆盖。  
- 功能开发清单（阶段 2，实施/增强）继续 JSON 清单。保存时同步导出 `docs/02-方案和UI设计/feature_dev_list.json`。  
- 运维无阶段 2 功能清单：开发清单在开发阶段手工维护或从需求任务清单导入（`req_task_list` 若是清单 JSON；否则允许空清单 + 手增）。

### 5.5 开发（实施 5 / 运维 4）

进入该阶段（`selectPhase` 或任务对接）时：

1. 若 `treeStatus≠ready`：横幅「尚未生成项目目录」，提供「仅补生成目录」（不改阶段状态）；未成树时禁用「进入开发」写盘。  
2. 若开发清单 0 条且存在上游功能开发清单：自动导入（与现 `mapItems` 相同，状态一律 `pending`），保存一次。已有条目不重复导入（按 `sourceId` 或 `id` 去重）。  
3. 顶栏固定「用谁开发」三选一：月汐平台 / Cursor / Codex。见 §6.4。  
4. 右侧默认：阶段交付物（清单）+ 工作区代码页，根 = `rootPath`。选 Cursor/Codex 且已打开任务时，可切「外脑」面板。  
5. 清单每一行：「进入开发」按当前执行器打开；行上显示最近执行器与结果摘要；被测试退回的条有醒目标记。  

芯片 `/implement` 等：若有当前任务，prompt 带上该条 id/标题/验收/`targetRelPath`/测试反馈；没有当前任务则提示「请先点清单里的一条」。

本阶段三关晋级 = **确认开发完成、进入测试**，硬闸见 §6.7。

### 5.6 测试 / 集成 / 发布

进入测试（实施 6 / 运维 5）：

1. 若测试清单 0 条：按现 `buildTestItemsFromDev` **自动**从所有 `dev_done` 条生成（`sourceId` = 开发条 id）。  
2. 导出测试清单到 `phaseMap` 对应目录。  
3. 标 `test_fail` **必须**填原因，并调用 `project.task.returnFromTest`（§6.8）。禁止只改前端备注。  
4. 测试阶段三关：任一条 `test_fail` / `pending` / `in_progress` → **不准晋级**。  
5. 集成门禁继续拒绝「测试不通过尚未退回」；退回后开发条未再 `dev_done` 也拒绝。  

发布阶段仍不把本地制品说成正式 live。

---

## 6 任务对接到「开发」

### 6.1 一条任务的工作单元

用户点开发清单条目 `D001`（或导入后的 `F001`）：

1. 切到开发阶段（若还在别的阶段）。  
2. 该条 `status`：`pending` → `in_progress`（已是 `dev_done` 且无新的测试退回则只打开，不改状态；有 `testReturnId` 且测试条仍为 `test_fail` 则保持/回到 `in_progress`）。  
3. 执行器 = 本条 `executor` 或项目 `defaultExecutor`（§6.4）。  
4. 记下 `openedAt`、`executor`、`lastRunKind`。  
5. 拼装同一套任务说明书（§6.5），按执行器投递（§6.6）。  
6. 右侧代码页定位到 `targetRelPath`。  
7. 本条成为「当前任务」。**任何执行器都不得自动 `dev_done`。** 人按「回写结果并完成」走 `project.task.complete`（§6.6）。

不新建第三条任务表。不强制 `plan.run`。

### 6.2 外脑不再是旁路

Cursor / Codex **就是**开发步骤的执行器，不是办公里另选文件夹的第二条产品。办公 Hub 自由线程仍可自选目录；**从项目管理打开的线程必须带 `projectId`。**

### 6.3 阶段会话的写盘范围

当 `session` 是项目阶段会话且项目 `rootPath` 已绑：

- `workspace.write` / `workspace.edit` / `command.run` 的默认根是 `rootPath`。  
- 仍走审批档（approval / auto-edit / full-access）。  
- 个人聊天（`personal`）行为不变。  
- 禁止把项目根写进全局 `workspace-root.json`。

### 6.4 执行器三选一（1.1）

开发步骤顶栏（阶段条下方或清单表头）固定一组互斥选择，文案：

| 选项 | 值 | 何时可选 | 打开任务时做什么 |
|---|---|---|---|
| 月汐平台 | `lunitide` | 始终 | 中栏阶段会话 + 任务说明书进输入框，不自动发送 |
| Cursor 对接 | `cursor` | `DetectAll` 中 cursor 为 `available` | 复用或创建 Hub 线程，`harnessId=cursor`，`projectId` + 强制 `rootPath` |
| Codex 对接 | `codex` | Codex 为 `available` | 同上，`harnessId=codex`（app-server 或 exec 由 Hub 现有降级处理，结果仍回写本条） |

规则：

- 选择写入 `project.executor.set` → `defaultExecutor`。本条若尚未跑过，用这个默认值。  
- 已跑过的条保留自己的 `executor`；用户可在行内改选后再「进入开发」。  
- 未 `available`：选项禁用，短句「未检测到，装好并登录后可选」。**禁止**假装已连通。  
- 本步不把 Kimi/Grok 画进三选一。若日后 `available`，用与 Cursor 相同插座加第四项，不改清单主键。  
- 换执行器不换条目 id、不换 `sourceId`、不换项目根。换的只是谁改磁盘。

### 6.5 任务说明书（三种执行器同一份）

`project.task.open` 返回的 `brief` 必须含且仅按此顺序：

1. 条目 id、标题、验收、`targetRelPath`、项目根绝对路径。  
2. 若有 `testReturnId`：测试条 id、失败原因、退回时间（从开发条 notes / `lastResultSummary` 解析不够；用结构化 `testReturn` 对象）。  
3. 约束：只改本任务范围；不要 git commit/push；不要把文件挪出项目根。

月汐：brief 填进阶段会话输入框。  
Cursor/Codex：brief 作为该线程的首条 `thread.prompt`（或续上同一 `hubThreadId` 时作为新一轮 prompt）。禁止另写一套「Hub 场景话术」把清单字段丢掉。

### 6.6 回写结果（一条链）

新增 `project.task.report`：

- 入参：`projectId`、`itemId`、`executor`、`summary`（必填，1–2000）、可选 `hubThreadId`。  
- 写出：`lastResultSummary`、`lastResultAt`、`lastRunKind`、`executor`。  
- 不改 `status`。

`project.task.complete`：

- 要求该条已是 `in_progress` 或已有 `lastResultSummary`。  
- 置 `dev_done`。  
- 若存在 `sourceId` 反指的测试条（测试清单里 `sourceId===本条 id`）且状态为 `test_fail`：自动改回 `pending`（待重测），并在测试条 notes 追加「开发已再交付 @ 时间」。  
- 导出两份清单到阶段目录。

UI：

- 月汐：当前任务旁「回写结果并完成」——摘要默认截取本轮最后一条助手回复（可改），确认后先 report 再 complete。  
- Cursor/Codex：线程进入空闲/结束且未 fault 时，外脑面板出现同一按钮；摘要默认截取该线程最后一条助手/notice。fault 时只允许「回写失败原因」，条目标 `in_progress`，不得 complete。  
- 模型或 CLI 自称「已完成」不算数。

### 6.7 确认开发完成 → 进入测试

开发阶段点「三关确认晋级」时，在现有 `CompleteProjectPhase` 之外增加清单硬闸（失败码 `PROJECT_DEV_INCOMPLETE`）：

1. 开发清单至少 1 条（实施/增强且上游功能清单非空时）；否则允许空清单但必须二次确认「无开发任务」。  
2. 每条状态均为 `dev_done`。  
3. 无 `in_progress` / `pending`。  
4. 通过后：若测试清单为空，服务端按 `buildTestItemsFromDev` 生成并落盘。  
5. 然后才晋级项目状态 / 完成开发 stage。

软进入测试步骤（没点三关）仍允许，与现「衔接警告」一致；但测试清单在未晋级时也可预生成，方便人先看。**不能**在开发未完成时把测试清单标 approved。

### 6.8 测试不通过 → 退回同一条开发

`project.task.returnFromTest`（取代只在前端改 notes 的 `rollbackTestFailToDev` 作为主路径；前端旧函数改为调这个桥）：

入参：`projectId`、`testItemId`、`reason`（必填，1–2000）。

服务端：

1. 测试条必须有 `sourceId`，且开发清单存在该 id。  
2. 测试条 → `test_fail`，notes 追加原因。  
3. 开发条 → `in_progress`，`testReturnId=testItemId`，写入结构化 `testReturn={id,reason,at}`（可序列化进 notes 前的保留字段；实现上建议清单 JSON 增加可选 `testReturn` 对象）。  
4. 开发清单交付物若已 `approved`，打回 `review`（否则无法再改条目）。  
5. 同一事务保存两份清单。

UI：测试行「不通过」必须弹原因；提交后提示「已退回开发清单 {id}」，并提供「去开发改这一条」（切到开发阶段并 `task.open`，brief 含原因）。

禁止：没有 `sourceId` 的测试条标 `test_fail` 还声称已退回。手增测试条若无来源，必须先绑开发条或禁止 fail。

### 6.9 反复直到完成

状态机（同一 `item.id` / `sourceId` 对）：

```
开发 pending → open → in_progress → report → complete → dev_done
                                                    ↓
测试 pending → 测 → test_pass（本条闭环）
              → test_fail → returnFromTest → 开发 in_progress（带原因）
                    ↑                              ↓
                    └── 再 complete 后测试条回到 pending ──┘
```

- 全部测试条 `test_pass` 且清单已确认，才允许测试阶段三关。  
- 轮次不限。  
- 换执行器不新开清单 id。  
- 集成阶段继续看「开发清单全 `dev_done` + 测试无 fail」（现 `fetchIntegrationGateReady` 升级为读服务端同一事实）。

---

## 7 和 Agent Hub / grok-build 的关系（收口）

| 问题 | 本合同 |
|---|---|
| Agent Hub 的 Cursor/Codex 是干什么的 | **开发步骤的执行器**，为项目管理写代码，不是旁路玩具 |
| 要不要融 grok-build 内核 | 不要 |
| grok / kimi | 同一插座可接，不进默认三选一文案，不挡 S8 |
| Hub 还要不要独立页 | 要；办公自由线程仍可自选目录 |
| 从项目管理进去 | 必须 `projectId`，cwd 锁死 `rootPath`，结果必须 `task.report` |
| 会话硬拆 | 仍禁止把线程写入 `sessions` / `messages` |

---

## 8 Bridge / 存储合同

### 8.1 改现有

| 方法 | 变化 |
|---|---|
| `project.create` | 必填 `rootPath` |
| `project.update` | 创建态可改根（换根：改锁文件，旧锁删除）；立项后改根走 `project.root.rebind` |
| `ProjectDTO` | 增加 `rootPath`、`treeStatus`、`treeDigest`、`treeGeneratedAt`、`defaultExecutor` |
| `project.advanceStatus` | `phase===1` 先成树；开发阶段三关加清单全 `dev_done`；测试阶段三关加无 fail/未测 |

### 8.2 新增（仅这些）

| 方法 | 作用 |
|---|---|
| `project.root.pick` | Host 选目录，不写全局 workspace-root；返回 path 或 canceled |
| `project.root.rebind` | 立项后换根：校验空闲、迁锁、`treeStatus` 置 `pending`，提示重新生成树 |
| `project.tree.get` | 读树清单 + receipt |
| `project.tree.put` | 保存机器树（未晋级第一阶段时可改） |
| `project.tree.materialize` | 仅补生成目录（已晋级或失败重试） |
| `project.executor.set` | 写 `defaultExecutor`：`lunitide` / `cursor` / `codex` |
| `project.task.open` | 打开清单条目到开发：改状态、按当前执行器返回同一份 `brief`（含 `testReturn`） |
| `project.task.report` | 只写开发结果摘要 / 执行器 / 可选 `hubThreadId`，不改完成态 |
| `project.task.complete` | 人确认后 `dev_done`；同源测试 `test_fail` 条回到 `pending`；导出清单 |
| `project.task.returnFromTest` | 测试不通过：原因必填，打回同一开发条 |

`agentHub.thread.create`：可选 `projectId`；有则强制 cwd，忽略客户端另传的 `workspaceRoot`。

### 8.3 库

新迁移（下一号，禁止改旧 `CREATE TABLE projects` 文本冒充已有列）：

- `projects.root_path TEXT` + 唯一索引（空串当未绑，历史行允许空）。  
- `projects.tree_status` / `tree_digest` / `tree_generated_at`。  
- `projects.default_executor TEXT`（默认 `lunitide`）。  
- 不新增任务表。执行器、结果、测试退回都写在现有清单 JSON 字段上。

历史项目：`rootPath` 空。进入工作台顶栏「补选项目根」；未补选时阶段可看文档，不能成树、不能任务写盘。

### 8.4 生成物

改 `api/bridge/v1/*.schema.json` 后跑 `generate-bridge.mjs`。禁止手改 `web/src/generated/bridge.ts`。

---

## 9 UI 合同

| 面 | 改动 | 禁止 |
|---|---|---|
| `ProjectPage` 创建/编辑 | 根目录选择、显示路径 | 不把 Hub 首页嵌进来 |
| 工作台顶/阶段条 | 显示根路径短名；树状态点 | 不改阶段个数 |
| 阶段 1 交付物 | 目录树编辑 + 预览 | 不删九份人文档 |
| `ChecklistPanel` | 行内「进入开发」；当前任务高亮；显示执行器与测试退回标记 | 不在阶段 1 显示该按钮 |
| `ProjectWorkbenchShell` | 接收 `taskOpen` 切到开发；开发顶栏三选一执行器 | 不整页 `setPage('agentHub')` |
| `SessionPage` 开发芯片 | 带当前任务发送 | 不自动 `chat.start` 打开任务 |
| 开发步骤右侧 | 可切「外脑」面板（Cursor/Codex 当前任务） | 不渲染 `SessionPage` 进 Hub |
| 测试清单 | 「不通过」必填原因；「去开发改这一条」 | 禁止只改前端备注冒充退回 |
| 代码页 | `filesFocus` / 根来自项目 | 不 `clear()` 成未绑定 |

文案：创建页「根目录现在选定，子目录在需求架构确认后生成。」

---

## 10 必须保持 / 明确禁止

### 保持

- 发布门禁、只读关闭、创建态删除规则。  
- 三关文案与不可逆提示。  
- 阶段会话标题格式。  
- Hub V2.1 模块边界。  
- 测试退回开发（升级为服务端同一条链，不是只改备注）。  
- 不自动 git commit/push。

### 禁止

- 建项目不选根却允许发布。  
- 第一阶段未确认就批量 `mkdir`（创建时只准 `.lunitide/project.json`）。  
- 成树失败仍 `req_architecture` / 运维等价晋级。  
- `rm -rf` 或按新树删旧目录。  
- 把项目根写进 `workspace-root.json`。  
- 开发任务写进 `sessions` 冒充外脑。  
- 点开发步骤就离开工作台进办公 Hub。  
- 未绑根时对用户说「已在本项目开发」。  
- 借本 PRD 重写 `SessionPage.tsx` 或拆 `OfficeStudioPage`。  
- 把 M7 `devTask.*` 改成清单主键（可后续映射，本包不做）。  
- 模型或 CLI 自称完成就自动 `dev_done`。  
- 未 `available` 的 Cursor/Codex 仍可点选并假装已连通。  
- 测试不填原因就标 `test_fail`，或没有 `sourceId` 还声称已退回开发。  
- 换执行器就新开一条清单 id / 拆成另一条互不相认的任务。

---

## 11 实施切片（可独立验收）

每刀有失败测试再写实现。禁止一刀做完全部 UI。

| 刀 | 内容 | 验收 |
|---|---|---|
| T01 | 领域 + 迁移 + DTO：`rootPath` / 树状态 | `go test` 创建无根失败；有根写锁；重复根拒绝 |
| T02 | `project.root.pick` + 创建表单 | 不选根不能提交；选出的路径出现在清单 |
| T03 | `ProjectTreeV1` 解析、默认树、路径拒绝 `..` | 纯 Go 表测 |
| T04 | 阶段 1 `advanceStatus` 成树；失败不晋级 | 临时盘：确认后目录在；只读盘：状态仍 chartered |
| T05 | 历史项目补选根 + `tree.materialize` | 已晋级项目补树，状态不变 |
| T06 | 交付物导出到 `phaseMap` 目录 | 确认一份 docx 后阶段目录可见副本 |
| T07 | 进入开发自动导入功能清单 | 空开发清单变出 F 条；再进不重复 |
| T08 | `project.task.open` + 清单按钮 + 芯片带任务 | 点 F001 到开发步骤，输入框含编号，代码页指向目标 |
| T09 | 阶段会话工具根 = `rootPath` | 集成测试：`workspace.write` 落在项目根不是 session 沙箱 |
| T10 | Hub `projectId` 强制 cwd | 带项目 id 时忽略客户端 workspaceRoot |
| T11 | 开发步骤内嵌外脑面板（已有 Thread UI） | 不改 Hub 表主键；工作台不丢阶段条 |
| T12 | 前端回归 + 文案 | `ProjectPage` / `ChecklistPanel` / workbench 单测；typecheck |
| T13 | `project.executor.set` + 开发顶栏三选一 | 未探测到的 Cursor/Codex 禁用；可选中后 `task.open` 带对应 `executor` |
| T14 | `project.task.report` / `complete` | 无确认不得 `dev_done`；complete 后同源 `test_fail` 回 `pending` |
| T15 | `project.task.returnFromTest` + 开发/测试三关硬闸 | 无原因失败；退回后开发条 `in_progress` 且 brief 含原因；开发未全 `dev_done` / 测试有 fail 不得晋级 |

T01–T09 覆盖 S1–S7。T10–T15 覆盖 S6/S8/S9（外脑挂载 + 一条链），与 T09 可并行，但**不得**在 T13–T15 未完成时宣布 S8/S9 或「开发对接完成」。S1–S5 仍可先单独验收。

---

## 12 测试合同

### 12.1 必须有的自动化

- 创建：缺根、根不存在、根为文件、根被占用、锁文件冲突。  
- 成树：默认树；JSON 树；`..` 拒绝；部分目录只读 → 不晋级；二次确认幂等。  
- 清单：自动导入；去重；打开任务状态机；`dev_done` 不自动打开改状态。  
- 执行器：`executor.set` 非法值拒绝；`task.open` 的 brief 三种执行器字段顺序相同；有 `testReturn` 时必含失败原因。  
- 回写：`report` 不改 status；`complete` 无确认路径不存在；同源测试 fail → pending。  
- 退回：无 reason / 无 `sourceId` 失败；开发条重开且 `testReturn` 结构化。  
- 三关：开发未全 `dev_done` → `PROJECT_DEV_INCOMPLETE`；测试有 fail/未测 → `PROJECT_TEST_OPEN`。  
- 工具根：阶段会话写 `src/a.txt` 的绝对路径前缀等于 `rootPath`。  
- Hub：`projectId` 覆盖 workspaceRoot。  
- 前端：创建无根禁用提交；清单行按钮；开发阶段自动导入只触发一次；未探测执行器禁用；测试不通过必须填原因。

### 12.2 本包不做的人测（不挡 100%）

- 真机 Cursor / Codex 写真实客户仓库（自动化用探测桩与线程桩即可）。  
- 外脑写完后的代码正确性（产品只验收回写与状态机）。  
- 默认树是否符合某家公司规范（可在阶段 1 改树）。

---

## 13 错误码（用户可见）

| 码 | 何时 | 下一步 |
|---|---|---|
| `PROJECT_ROOT_REQUIRED` | 创建/写盘无根 | 选择根目录 |
| `PROJECT_ROOT_INVALID` | 路径不存在或非目录 | 重选 |
| `PROJECT_ROOT_BUSY` | 已被其它 ITM 占用 | 换目录或打开原项目 |
| `PROJECT_ROOT_READONLY` | 不能写 `.lunitide` | 换可写盘 |
| `PROJECT_TREE_INVALID` | JSON 不合格 | 改树或清空用默认 |
| `PROJECT_TREE_FAILED` | mkdir 失败 | 看 receipt；修权限后「补生成」 |
| `PROJECT_TREE_REQUIRED` | 未成树就要任务写盘 | 先确认阶段 1 或补生成 |
| `PROJECT_TASK_NOT_FOUND` | 条目 id 不在清单 | 刷新清单 |
| `PROJECT_TASK_PHASE` | 在非开发清单上调用 open | 只对开发清单 |
| `PROJECT_EXECUTOR_UNAVAILABLE` | 选用了未探测到的 Cursor/Codex | 安装并登录后重试，或改选月汐 |
| `PROJECT_DEV_INCOMPLETE` | 开发三关时清单未全 `dev_done` | 先完成或退回后的条目 |
| `PROJECT_TEST_OPEN` | 测试三关时仍有 fail / pending / 进行中 | 测完或退回开发 |
| `PROJECT_TEST_REASON_REQUIRED` | 测试不通过未填原因 | 填写失败原因 |
| `PROJECT_TEST_NO_SOURCE` | 测试条无 `sourceId` 却要退回开发 | 先绑定开发条 |

---

## 14 与现网文件的落点（禁止借机重构）

| 工作 | 文件 |
|---|---|
| 领域字段 | `internal/domain/project/project.go` |
| 成树 | 新包 `internal/projecttree`（解析 + mkdir + receipt），由 `projectapp.Mutate` 阶段 1 调用 |
| 迁移 | `migrations/0xxx_project_root_tree.sql` + `store.go` **新增** expected 键策略按仓库惯例 |
| 桥 | `internal/app/project_handlers.go`、新 schema、`generate-bridge` |
| 创建页 | `web/src/project/ProjectPage.tsx` |
| 树编辑 | `web/src/project/ProjectTreeEditor.tsx`（新，阶段 1 面板引用） |
| 清单/任务 | `checklistTypes.ts`、`ChecklistPanel.tsx`、`DeliverablePanel.tsx`、`checklistStore.ts`（旧 `rollbackTestFailToDev` 改调桥） |
| 工作台 | `ProjectWorkbenchShell.tsx`（切阶段 + 当前任务 + 执行器三选一） |
| 工具根 | `internal/app` 组 prompt / `toolruntime` 仅在「有项目根的阶段会话」改默认根 |
| Hub | `thread_service.go` 认 `projectId`；`web/src/agentHub` 只加可选入口与探测态，不改硬拆 |

---

## 15 非目标

- grok-build 源码进发布链。  
- ACP 服务端（让别人嵌月汐）。  
- 嵌 TUI / 官方 IDE 窗口。  
- 自动 git。  
- 自进化 SKILL.md。  
- 把办公、月伴、航线迁进项目管理。  
- 统一全球工作区与项目根。  
- 脚手架 zip 一键展开（记为后续 PRD）。  
- 重启后续上 Hub 原生线程（仍按 Hub 规格 fault；本包不抢）。

---

## 16 完成定义

同时为真：

1. S1–S9 各有自动化证据（T01–T09 对 S1–S7；T10–T15 对 S6/S8/S9）。  
2. 首发场景 §1.4 步骤 1–8 在测试盘上可复现（步骤 6–8 可用探测桩 / 线程桩，不必真机 Cursor）。  
3. 创建未选根、成树失败、未成树写盘、Hub 带 projectId 另传 cwd、未探测执行器、开发未完成晋级、测试无原因退回，七条失败路径有测试。  
4. `npm run typecheck` 与本包相关 `go test` 通过。  
5. 本文 §0.1 的旧说法不再出现在用户可见文案里。

本 PRD 完成 ≠ 旧安装包已含此主链。装机版本以当时签名包为准。

---

## 17 批准后才做的事

1. 按 writing-plans 写实施计划（一刀一测）。  
2. 从 T01 开始改代码。  
3. 不在本文件未改口前继续按「开发步骤再选仓库」或「Hub 另走一条开发链」施工。
