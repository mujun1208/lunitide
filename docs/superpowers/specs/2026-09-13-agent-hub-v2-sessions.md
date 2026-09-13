# 月汐 Agent Hub V2 PRD（独立模块 · 会话融合）

| 文档信息 | |
|---|---|
| 产品 | 月汐 / Lunitide（Go Engine + WebView2 + React） |
| 版本 | **V2.1**（废止同日 V2.0 稿：禁止把外接会话焊进 `SessionPage` / `sessions` / `messages`） |
| 日期 | 2026-09-13 |
| 状态 | 可施工。在 **V1.2 Agent Hub 独立模块内部** 融合「多轮会话 + 选目录 + 产物」，UI 只在壳上做两态切换。 |
| 前序 | `lunitide-agent-hub-design-v2/lunitide-agent-hub-prd.md`（V1.2 仍是模块基线） |
| 实施计划 | `docs/superpowers/plans/2026-09-13-agent-hub-v2-sessions.md` |

---

## 0. 先读：上一版方案为什么不能落地

V2.0 稿为了「和月汐对话长得一样」，要求：

- 外接对话走 `SessionPage` + `ChatBridge`；
- `session.create` 挂个人项目，再 `agent_hub_threads.session_id` 外键；
- 侧栏用 `isOrdinarySidebarChat` / session id 集合过滤；
- 产物走会话工作区 / `ChatArtifactCards`。

对照现网，这会**直接搅乱其它产品**：

| 现网事实 | 焊进去会怎样 |
|---|---|
| `App.tsx` 第 91 行：`if (target?.personal)` **整页**变成 `chat-shell` + `SessionPage`，`page` 被写死 `'home'` | 外接态进不了 `page==='agentHub'`，调度台页消失；Ctrl+N / 返回 / 模型管理都按月汐对话走 |
| `SessionPage` 绑 `chat.start`、供应商、专家、月伴、`token` 展示、`liveChat` | 改闸门会动 20+ 个会话测试；Office 还 `subscribeLiveChatRegistry` |
| `session.list` + 个人项目 `⁣月汐·普通对话` | 外接会话出现在月汐「对话」或必须改过滤，两边列表互相污染 |
| `session.metadata.set` 只收 `mroContext` | 不能塞 harness；硬塞会破坏机务 |
| `workspace.artifact.preview` / `sessionFolder` 绑 `tool-workspaces/<session>` | 用户自选项目根读不了，或误把用户盘当会话沙箱 |
| `messages` 表 + FTS + compaction | 外接 JSONL/ACP 事件会进月汐检索和压缩 |
| V1.2 明确：`agentHub.*` 不要进 `dataScopedMethod`，字段用 `taskId` 不用 `runId` | 改成 `sessionId` 当会话主键，容易踩组织鉴权 |

**结论先说：不焊进 SessionPage，也不焊进个人对话列表。UI 像对话，运行时仍是独立模块。**

不是「怕集成」。是现网 `SessionPage` **不能当稳定宿主**。下面是按集成性 / 稳定性 / 一致性 / 健壮 / 复用 / 不乱套 做完的决定，以后按这个执行，不再在两套方案间摇摆。

### 0.1 三种做法

| | A. 焊进个人对话 | B. 借用 SessionPage，数据仍独立 | C. 模块内自建对话页（决定） |
|---|---|---|---|
| 入口 | `setTarget({personal})`，和月汐聊走同一条 `App.tsx` 早退 | `page==='agentHub'` 里 `<SessionPage/>` | `page==='agentHub'` 里 `<AgentHubThread/>` |
| 数据 | `sessions` + `messages` | 适配器伪装成 Session/Message | `agent_hub_threads` / `messages` |
| 引擎 | 极易误走 `chat.start` | 注入 ChatBridge | 只打 `agentHub.thread.*` |

### 0.2 现网事实（决定依据）

`App.tsx`：`if (target?.personal)` **整页**变成 `chat-shell` + `SessionPage`，`page` 被写死 `'home'`。外接一旦变成 personal target，调度台页、Ctrl+N、`onNew=fresh()` 全部按月汐对话走。

`SessionPage` 不是「聊天气泡组件」。外层管 `session.create/list/delete`；内层 `MessagePanel` 在挂载时就会打：

供应商/模型闸门、`liveChat`、专家 `sessionMountGet`、附件 `attachments.list`、技能 `@/`、月伴、`feedback.candidates`、记忆、`sessionFolder`、`inspectTurn`、compaction、办公 `officeTaskId`、草稿键 `projectId+session.id`。

这些默认都认 **月汐 sessionId**。把 threadId 冒充 sessionId，会朝专家/附件/记忆/Office 发脏请求，或把外接事件推进 `liveChat`（办公页在订阅）。

`ChatBridge` 看起来可插，但只换了「发送」。换不掉上面那一串副作用。要安全复用，必须在 `MessagePanel` 加 `lane=harness` 并关掉十几处 `useEffect`。以后每加一个月伴/机务/办公能力，都要记得外接闸门——这正是这个文件已经膨胀的原因。

### 0.3 六维结论

| 维度 | 焊进 SessionPage / 个人对话 | 独立模块 + 套 CSS |
|---|---|---|
| **集成性（产品）** | 入口像「又一个聊天」，但和月汐模型聊混在同一列表，用户分不清额度从哪扣 | 左上「月汐 / 外接 Agent」本来就要分开；集成在**壳**，不在会话运行时 |
| **集成性（工程）** | 假集成：UI 同文件，协议仍是 ACP，还要伪造 SessionDTO | 真边界：`internal/agenthub` + `web/src/agentHub` + `agentHub.*` |
| **稳定性** | 改 SessionPage 闸门会碰现有几十个会话测试；一条漏网 useEffect 就能写脏专家挂载 | V1.2 任务测试可以继续绿；外接坏了不拖垮月汐聊 |
| **一致性** | 短期最像。长期 SessionPage 为办公/月伴改 composer，外接被带着变，或到处 `if (harness)` | 气泡/侧栏/工作区用同一套 CSS class，观感一致；能力条（模型、专家、月伴）故意不一致，因为本来就不是月汐引擎 |
| **健壮** | sessionId 命名空间冲突；FTS/compaction 吃 ACP 碎片；删个人项目是否级联不清晰 | 无 `sessions` 外键；路径只允许 workspace/export；重启策略抄 V1 任务 |
| **复用** | 复用的是 80% 用不上的能力（月伴/专家/账本），复用成本 > 收益 | 复用 Job Object、detect、选目录、inbox、FileInspector、CSS、Dialog。对话页只实现：消息、选项、输入、右栏文件（约 SessionPage 的 20%） |
| **不乱套** | 最差。Ctrl+N、对话搜索、liveChat、token 展示、空会话自动删都会误伤 | 壳只加插槽 + 切外接时 `setTarget(undefined)` |

**「对话 bug 一同修」做不到 100%。** 这是刻意换来的：先保证月汐对话不烂。以后若要抽公共 `ConversationChrome`，必须先给 SessionPage 单测护栏，作为**独立重构任务**，不作为本 PRD 前置。

**不允许的假集成：** 在 AgentHubPage 里渲染 SessionPage，再传入伪造 `SessionDTO` / no-op `session.create`。副作用仍在，只是藏得更深。

V2.1 按 C 落地，满足 §2 的 6 条需求，模块边界见 §3。

---

## 1. 可行性结论

**可以 100% 落地**，当且仅当：

1. Agent Hub 继续是独立模块：后端 `internal/agenthub`，前端 `web/src/agentHub`，Bridge 仍是 `agentHub.*`。
2. **禁止** import / 调用：`SessionPage`、`chat.start`、`message.*`、`session.create/update/delete`（外接数据）、`liveChat`、`ArtifactInspector`、`ChatArtifactCards`、`office.generate`、`agent.run.*`、`commandworker.Run`。
3. UI 与其它产品的唯一接线是 **壳层插槽**（§3.2）。月汐对话列表、项目、办公、机务、专家 **零逻辑改动**。
4. 主路径在模块内从「一把 `task`」升级为「`thread` 多轮会话」；V1.2 的 `agent_hub_tasks` / `agentHub.task.*` **保留且行为不变**。
5. 协议：Cursor `cursor-agent acp`；Kimi `kimi acp`；Codex 先去 `--ignore-user-config` 的 exec，app-server **能探到再开** interactive，探不到就诚实降级（§7）。
6. 不接 API Key，不打月汐模型，不动 `token_ledger`。
7. 新表进 `expectedSchemaSQL`；不手改 `bridge.ts` / `schema_generated.go`。
8. 不嵌三家 GUI / CDP。不实现 Pi / Claude / OpenCode 等（只留注册表位）。
9. 目录规范由对方 Agent 在用户选的根里写文件；月汐不搬家。
10. `officeMenu.agentHub` 默认仍为 false。

---

## 2. 需求对照（能否满足）

| 你的要求 | 满足？ | 落地方式（模块内） | 不满足的上限 |
|---|---|---|---|
| 1. 像打开各自 Agent，有来有往；有产物、能打开 | **能** | 独立 `AgentHubThread` 页：多轮消息 + 右侧文件 + `agentHub.file.preview/open` | 不是三家 GUI 窗口 |
| 2. 指令原样交给他们，尽量用各自 Skills/MCP/工具 | **大部分能** | ACP / 读用户配置；不 `--ignore-user-config`；不 `--skills-dir` 覆盖 | Cursor 插件市场 Skills 官方 CLI 对不齐 |
| 3. 每组对话可指定存储路径 | **能** | thread 上 `workspace_root` + 可选 `export_dir` | 导出只拷贝文稿类，不搬源码 |
| 4. 选目录写项目、按规则建子目录写代码 | **能** | 场景「写项目」强制选根；规则进 inbox 或对话；Agent 自己写文件 | 月汐不强制目录树 |
| 5. 选仓库根改代码，就地改、新文件按规范放 | **能** | 场景「改代码」cwd=仓库根；禁止移动已有路径 | 同上 |
| 6. 对方提示引导，你能选 | **能** | thread 状态 `waiting_user` + 模块内选项条；`agentHub.thread.respond` | 一把 exec 降级的 Codex **不能**提问（卡上写明） |

和 CodexHost 比：你仍是自己的壳 + 独立模块；他是寄生 Desktop。多家集成 = 本模块注册表，不是一次做完他家全部适配器。

---

## 3. 独立模块合同（开发红线）

### 3.1 模块拥有（只许在这些路径长逻辑）

| 层 | 路径 |
|---|---|
| 后端 | `internal/agenthub/**` |
| 桥 | `internal/app/agenthub_handlers.go`（只加 `agentHub.*` case）+ `api/bridge/v1/agentHub.*.schema.json` |
| 库 | `migrations/0154_agent_hub_threads.sql` + `store.go` 的 **新增** `expectedSchemaSQL` 键（禁止改已有表 SQL 文本，除非 0153 三家 CHECK 仍不动） |
| 前端 | `web/src/agentHub/**` |

V1.2 已有：`detect` / `task.*` / `dir.pick` / `inbox` / `file.*` / `artifact.list`、Job Object、`AgentHubFileInspector`。V2 **融合**：在同一模块加 thread 运行时 + 对话 UI，四 Tab 降为「旧版一把任务」。

### 3.2 壳层允许改动（仅此，禁止借机重构）

| 文件 | 允许 | 禁止 |
|---|---|---|
| `App.tsx` | `page==='agentHub'` 仍渲染 `<AgentHubPage/>`（已存在）。切到外接时 **`setTarget(undefined)`** 再 `setPage('agentHub')`。`page==='agentHub'` 时 `onNew` / Ctrl+N **不要**走 `fresh()`（那会建月汐对话）。给 Sidebar 两个可选 slot。 | 不要 `setTarget({personal, harness})`；不要给 SessionPage 加 `externalHarness` |
| `LaunchSidebar.tsx` | 可选 props：`topSlot?: ReactNode`、`replaceMainNav?: ReactNode`。有 `replaceMainNav` 时不渲染办公/对话/项目三段（**三段 JSX 原样保留在另一分支**）。 | **禁止** `import` `../agentHub/**`；禁止改 `isOrdinarySidebarChat`、禁止过滤 `session.list` |
| `sidebarSplit.ts` 或 `web/src/agentHub/shellMode.ts` | `lunitide:shell-mode` 的读写放在 **agentHub** 包；Sidebar 不读这个 key | 不要把 shell mode 写进通用 sidebar 业务 |
| `OfficeMenuPanel.tsx` | 只改 `agentHub` 这一条文案 | 不要改其它开关默认值 |
| `styles.css` | 可加 `.shell-mode-switch` 用现有 button / `--tide1` | 新色板、改 `.message-panel` 语义 |

`topSlot` / `replaceMainNav` 由 `App.tsx` 从 `web/src/agentHub` 传入。这样 **LaunchSidebar 不依赖 Agent Hub**，Agent Hub 也不依赖 Session。

### 3.3 明确禁止的「图方便」

- 把 thread 存进 `sessions` / `messages`。
- 复用 `UserAskWizard`（它认 `user.ask` 工具和会话审批）。在 `agentHub/AgentHubAskBar.tsx` 自写选项条，**可以长得像**，不要 import session。
- 复用 `ArtifactInspector`（`workspace.artifact.preview`）。继续用 `AgentHubFileInspector` + `agentHub.file.preview`。
- 把 thread 事件推进 `liveChat`（Office 在听）。
- 为了流式去改 `handleChat` / `StreamEvent` 联合类型。V2.1 **继续 400ms 轮询** `agentHub.thread.get`（与 V1.2 任务相同，已验证）。
- 扩大 `agent_hub_tasks.agent` 的 SQL CHECK。新 harness 只出现在 `agent_hub_threads.harness_id`。

---

## 4. UI（只改壳 + 模块内页）

### 4.1 两态切换（图 1 只借这一下）

现网菜单在**左**。个人对话打开时 `App` 早退到 `chat-shell`，Sidebar 里 `page` 被写成 `'home'`。因此：

- 切换组件放在 `web/src/agentHub/AgentHubShellSwitch.tsx`，经 `topSlot` 插到 Sidebar 最顶。
- 文案：中文「月汐 / 外接 Agent」；英文 `Lunitide / Agents`。
- 仅 `officeMenu.agentHub===true` 时 App 才传入 `topSlot`。
- **从月汐对话切到外接：** 必须 `setTarget(undefined)` + `setPage('agentHub')`，否则永远出不了 SessionPage。
- **从外接切回月汐：** `setPage(lastPageRef 或 'home')`，不创建会话。
- 办公组「Agent 调度台」按钮保留，行为仍是 `setPage('agentHub')`（现网已有）。不要删这个入口，避免只靠切换、用户找不到。两个入口同一页。

### 4.2 `page==='agentHub'` 主区（模块内换皮，不是新应用）

**删除四 Tab 作为默认主 IA。** `AgentHubPage` 改为：

```
+-- 若 replaceMainNav 未接好：页内仍可显示 Agent 列表（降级，避免 Sidebar slot 失败时空白）
+-- 无 thread：AgentHubHome（选 Agent / 场景 / 目录 / 输入）
+-- 有 thread：AgentHubThread（左消息 + 右文件，class 用现有 workspace-layout / message-panel）
+-- 底或菜单「旧版任务」：现有 AgentHubTasks + Detail（V1.2 一把任务，只读入口）
```

视觉：复用已有 class（`conversation-row`、`message-panel`、`workspace-layout`、`chat-artifacts` 若已是全局）。**不**新建主题。空态不要抄 Kimi 巨标题。

### 4.3 外接态左侧（`replaceMainNav`）

`AgentHubSidebar`：按 detect 分组 + 该组 thread 列表（自己的 list API）。置顶/重命名/删除只打 `agentHub.thread.*`。

搜索：模块内过滤标题，**不要**调用 `messages.search`。

新对话：只创建 thread，不 `session.create`。

---

## 5. 数据（全在 Agent Hub 表）

迁移 `0154_agent_hub_threads.sql`。**不要** `REFERENCES sessions(id)`。

```sql
CREATE TABLE agent_hub_threads (
  id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
  harness_id TEXT NOT NULL CHECK (length(harness_id) BETWEEN 1 AND 64 AND harness_id NOT GLOB '*[^a-z0-9-]*'),
  native_session_id TEXT NOT NULL DEFAULT '' CHECK (length(native_session_id) <= 256),
  title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200 AND title = trim(title)),
  pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
  workspace_root TEXT NOT NULL CHECK (length(workspace_root) BETWEEN 1 AND 1024),
  export_dir TEXT NOT NULL DEFAULT '' CHECK (length(export_dir) <= 1024),
  scene TEXT NOT NULL CHECK (scene IN ('write_project','fix','ppt','free')),
  status TEXT NOT NULL CHECK (status IN ('idle','running','waiting_user','success','failed','cancelled','faulted')),
  access_mode TEXT NOT NULL DEFAULT 'approval' CHECK (access_mode IN ('approval','auto-edit','full-access')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE agent_hub_messages (
  id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
  thread_id TEXT NOT NULL REFERENCES agent_hub_threads(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('user','assistant','system','notice')),
  content TEXT NOT NULL CHECK (length(content) <= 200000),
  created_at TEXT NOT NULL,
  UNIQUE(thread_id, seq)
);
CREATE TABLE agent_hub_thread_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id TEXT NOT NULL REFERENCES agent_hub_threads(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  type TEXT NOT NULL CHECK (length(type) BETWEEN 1 AND 64),
  title TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload_json) AND length(payload_json) <= 1048576),
  ts TEXT NOT NULL,
  UNIQUE(thread_id, seq)
);
CREATE TABLE agent_hub_thread_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id TEXT NOT NULL REFERENCES agent_hub_threads(id) ON DELETE CASCADE,
  rel_path TEXT NOT NULL CHECK (length(rel_path) BETWEEN 1 AND 1024),
  abs_path TEXT NOT NULL CHECK (length(abs_path) BETWEEN 1 AND 1024),
  size INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL CHECK (source IN ('event','scan','export')),
  UNIQUE(thread_id, rel_path)
);
CREATE TABLE agent_hub_prompts (
  thread_id TEXT NOT NULL REFERENCES agent_hub_threads(id) ON DELETE CASCADE,
  call_id TEXT NOT NULL CHECK (length(call_id) BETWEEN 1 AND 128),
  prompt TEXT NOT NULL CHECK (length(prompt) BETWEEN 1 AND 4000),
  options_json TEXT NOT NULL CHECK (json_valid(options_json) AND length(options_json) <= 8192),
  status TEXT NOT NULL CHECK (status IN ('open','answered','cancelled')),
  PRIMARY KEY(thread_id, call_id)
);
```

索引：`ix_agent_hub_threads_harness (harness_id, pinned DESC, updated_at DESC, id)`；`ix_agent_hub_messages_thread (thread_id, seq)`。

`0153` 三张任务表 **一行不改**。Service 上任务方法保持签名。Thread 可以是同一 `Service` 的新方法，或 `Service` 内组合 `ThreadRuntime`，禁止新的全局单例。

默认工作区：`PrepareSubdirectory("agent-hub")/threads/<threadId>/`（与 V1.2 任务目录并列）。用户选的项目根**不**拷进 LocalAppData。

路径允许：`filepath.Abs` 后必须是该 thread 的 `workspace_root` 或 `export_dir` 子路径。沿用 `ErrPathOutside`。用户根不是 SecureRoot——只保证不逃逸，这是产品要求。

---

## 6. Bridge（只扩 `agentHub.*`）

现有方法行为冻结（测试保持绿）。

新增：

| 方法 | 类型 | 作用 |
|---|---|---|
| `agentHub.thread.create` | 请求/响应 | `{harnessId, scene, workspaceRoot, exportDir?, title?, accessMode?}` |
| `agentHub.thread.get` | 请求/响应 | 含 messages、events、files、open prompt |
| `agentHub.thread.list` | 请求/响应 | `{harnessId?}` |
| `agentHub.thread.update` | 请求/响应 | 标题 / pinned |
| `agentHub.thread.cancel` | 请求/响应 | 取消当前 turn |
| `agentHub.thread.prompt` | 请求/响应 | `{threadId, text}` 追加 user 消息并开 turn（非流式） |
| `agentHub.thread.respond` | 请求/响应 | `{threadId, callId, optionId, text?}` |
| `agentHub.workspace.list` | 请求/响应 | `{threadId, relativePath?}` |

`agentHub.detect` **追加字段**（旧前端忽略即可）：`interactive`、`protocol`（`acp`/`exec`/`none`）。

`agentHub.file.preview` / `open`：**增加可选** `threadId`。`taskId` 与 `threadId` 互斥，必须一个。旧测试只传 `taskId` 必须仍绿。

前端 `agentHubApi.ts` 只在本包扩方法。继续用现有 `setAgentHubRequest`，不要改全局 `bridge/client` 业务。

**不加** `agentHub.turn.start` 长流（避免和 chat 流式管线纠缠）。运行中 UI 每 400ms `thread.get`（有 running/waiting_user 时），空闲 4s，与 `AgentHubPage` 现网任务轮询一致。

---

## 7. 适配器（模块内，任务与会话分路径）

```
AgentAdapter          // V1.2 一把任务，禁止改行为除非修 bug
ThreadAdapter         // V2 新接口：Open / Prompt / Respond / Close
```

`registry`：`cursor` / `kimi` / `codex` 实现 ThreadAdapter；未实现的 ID 不进 detect 列表。

| ID | 会话命令 | 任务命令（V1 保持） |
|---|---|---|
| cursor | `cursor-agent acp`（Win 解析原生 node 包，避 `.cmd` 孤儿） | 仍是 `-p --force --trust …` |
| kimi | `kimi acp`（禁止 `--skills-dir`） | 仍是 `-p` + 现网 skills-dir |
| codex | 若 2s 内 app-server 握手成功 → 用它（`interactive=true`）。否则 `exec --json --skip-git-repo-check --sandbox --cd` **无** `--ignore-user-config`（`interactive=false`，Hint 写明不能提问） | 现网仍带 `--ignore-user-config`（旧任务契约，本版不改 task argv，避免绿测和旧任务语义一起炸）。**会话路径**去掉 ignore |

进程：会话用新 `StartPersistent`（stdin 不关），与任务的 `StartProcess`（写完可关 stdin）分开。都挂同一套 Job Object。禁止 `commandworker.Run`。

Loopback：仅 `LUNITIDE_HARNESS_LOOPBACK=1` 时 detect 多一项 `loopback`，供无 CLI 验收多轮+选项。正式 UI 不宣传。

后继家（Claude / Pi / …）：只加 `ThreadAdapter` + detect，**不改 Sidebar/App**。本 PRD 不实现。

---

## 8. 场景（用户可见，不写进适配器 argv）

| 场景 | 默认 harness | 必须选根 | 写入 **system 消息**（`agent_hub_messages.role=system`，界面可见） |
|---|---|---|---|
| 写项目 | cursor | 是 | 「在你选的文件夹里按你的规则创建子目录并写文件。不要把已有文件挪到别处。」 |
| 改代码 | codex | 是 | 「在此仓库根内检索和修改。已有文件保持原路径。新文件按已有结构和你的规则放置。」 |
| 做 PPT | kimi | 否（默认可建 thread 目录） | 「用 Kimi 自己的技能做文稿。pptx 写在工作区；指定了导出目录则完成时复制过去。」 |
| 自由 | 用户选 | 否 | 无 |

用户可改默认 harness（目标须 `available` 且该路径 `interactive` 或用户接受一把 exec）。  
`prompt` 的用户句原样进对方。system 句是否注入由 ThreadAdapter 用「可见的第一条 context」发送——产品上必须已出现在消息列表，禁止只进 argv 不进 UI。

Inbox：继续 `agentHub.inbox`，拷到 `workspaceRoot/.agenthub-inbox`。

导出：turn 成功且 `export_dir` 非空时，拷贝本轮 `.pptx .pdf .docx .xlsx .zip`。不拷 `.go .ts .tsx .js .py`。

权限 chips（模块内，不复用 SessionPage MODE_INFO 也可抄文案）：手动 / 自动 / 完全访问。完全访问才自动过 permission；**业务选项**（`waiting_user`）永远要人点。

---

## 9. 前端模块结构

```
web/src/agentHub/
  AgentHubPage.tsx          // 改：Home | Thread | 折叠旧任务；去掉默认四 Tab
  AgentHubHome.tsx          // 新
  AgentHubThread.tsx        // 新：消息 + 选项 + 右栏
  AgentHubAskBar.tsx        // 新
  AgentHubSidebar.tsx       // 新：给 App 插槽
  AgentHubShellSwitch.tsx   // 新
  AgentHubFileInspector.tsx // 保持
  agentHubApi.ts            // 扩
  agentHubCopy.ts           // 扩文案
  AgentHubTasks.tsx         // 保持，改为「旧版任务」
  AgentHubDetail.tsx        // 保持
```

`AgentHubThread` 自管：本地 messages 状态、400ms get、发送、respond。不要用 `SessionPage` hooks。

---

## 10. 测试（独立模块 + 壳层最小）

**必须绿（回归 V1.2）：**  
`go test ./internal/agenthub/` 现有；`internal/app/agenthub_handlers_test.go`；`web/src/agentHub/*.test.tsx` 里旧任务/PPT 文案。

**新增：**

- PathAllowed / thread CRUD / loopback 两轮 + respond。
- `file.preview` 带 `threadId` 逃逸失败；只带 `taskId` 的旧测试仍过。
- detect 含 `interactive`；loopback 默认不出现。
- Codex **thread** argv 不含 `--ignore-user-config`；**task** argv 仍含（V1 契约）。
- Vitest：ShellSwitch 调用 `onAgents` / `onLunitide`；App 层用假函数测「切外接会清 target」可放 `App` 仅当已有测试方便，否则测 Switch 回调即可。
- Vitest：`LaunchSidebar` 无 `replaceMainNav` 时对话列表与现网一致（现有测试必须仍过）。有 slot 时办公/对话/项目不出现。
- Vitest：AgentHubHome 无目录不能发「写项目」；AskBar 点选项打 `thread.respond`。
- **禁止** 为外接去改 `SessionPage.*.test.tsx`。

本机手工：开 office 菜单 Agent Hub → 切月汐/外接 → 月汐对话列表不被外接标题污染 → loopback 或 Cursor 多轮选项 → 打开产物。

---

## 11. 稳定性与不完整项（写进计划，避免假装做完）

| 项 | 处理 |
|---|---|
| Codex app-server 本仓库未钉死 argv | P2 先交 exec 去 ignore + Hint；握手成功再亮 interactive。不阻塞 P0/P1 |
| ACP 版本漂移 | `initialize` 失败 → thread `faulted`，Hint 用 CLI 原文（截断 200 字） |
| 重启 | 与 V1 任务相同：running thread 标 `faulted`「应用重启后未能继续」 |
| 孤儿进程 | Win Cursor 启动测解析；关页 / `thread.cancel` / 进程杀树 |
| 用户 MCP 风险 | 选项条可见；文案写清完全访问 |
| 轮询延迟 | 与 V1 相同 <1s，不引入 chat 流 |
| 后继 6 家适配器 | 不在本版范围，文档只留注册表规则 |

---

## 12. 发布

- 开关仍是 `officeMenu.agentHub`。
- 不改 VERSION（发布时另定）。
- 用量：thread.get 可带 `tokensUsed`；UI：「消耗的是该 CLI 自己的会员额度」。

---

## 13. 开发顺序

见计划。P0 必须先：表 + loopback + 独立对话页 + 壳插槽 + **清 target**。没有这些就接真 CLI，会再次焊进 SessionPage。
