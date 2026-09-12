# 月汐「Agent 调度台」功能 PRD

| 文档信息 | |
|---|---|
| 产品 | 月汐 / Lunitide（Go Engine + WebView2 + React） |
| 新功能 | Agent 调度台（侧栏一级页，调度本机已装 CLI） |
| 版本 | **V1.2（二次对齐现网，可落地）** |
| 日期 | 2026-09-12 |
| 状态 | 替代 V1.0 / V1.1。V1.0 按 Wails 绿场写；V1.1 仍误写「直接调用 commandworker / 加 fsnotify / 复用 ArtifactInspector」 |
| 视觉稿 | `index.html` 只作页面气质参考，不替换产品壳 |

---

## 0. 可行性结论（先读）

**原 V1.0 不能 100% 落地。** 它假设 Wails 绑定、`agent:event` 总线、`~/lunitide-workspace`、三家 CLI 同等 JSON 流、厂商 Logo、「Pro 已登录」可测、以及全站电光青柠主题。这些和现网都不成立。

**V1.2 可以 100% 落地**，前提是按下面锁定的范围做：只新增、不改现有主链语义；每个能力都有现成代码可接或明确「抄原语、不调用现成入口」；CLI 能力按探测结果 gated，不假装三家一样强。

**V1.1 二次审计仍不能按字面施工（已并入本版）：**

| V1.1 误写 | 现网事实 | V1.2 |
|---|---|---|
| Runner「复用 `commandworker.Run`」 | `Run` 把 **stdin 立刻关掉**（EOF）；`TimeoutHardCap=15m`；`OutputHardCap=4MiB` 截断后丢字节 | **禁止调用 `commandworker.Run`。** 在 `internal/agenthub` 自写进程：抄 `worker_windows.go` 的 Job Object（kill-on-close / 杀树 / CREATE_NO_WINDOW），但 **stdin 先写 prompt 再关**、超时 **30–120min**、stdout **按行流式落库**（不设 4MiB 丢弃帽） |
| 产物用 fsnotify 去抖 | `go.mod` **没有** `fsnotify` | V1 **不加新依赖**。清单 = 解析到的 `file_write` ∪ **结束时 `filepath.Walk` `work_dir`**。运行中如需提示，在 `task.get` 里做廉价 `ReadDir` |
| 「复用 ArtifactInspector」 | 该组件写死 `sessionId` + `workspace.artifact.preview`（会话沙箱） | **禁止 import。** 新建 `AgentHubFileInspector`，只抄展示分类（text/md/image），数据走 `agentHub.file.preview` |
| 迁移只改 manifest checksum | `store.go` 的 `expectedSchemaSQL` 字节级比对，漏登记会直接 `unknown schema object` | 新表/索引必须进 `expectedSchemaSQL`；先写 dump 测试再贴 SQL 原文 |
| `App.tsx` 加页即可 | `page` 无匹配时 **fallback 是供应商页** | 必须写 `page==='agentHub'` 分支，否则点进调度台会打开 ProviderApp |
| 工作目录 `filepath.Join(dataRoot, …)` | 生产子目录必须 `datadir.SecureRoot.PrepareSubdirectory`（禁 reparse、ACL） | 启动时 `PrepareSubdirectory("agent-hub")`，任务目录挂在这棵钉住的根下 |

| 原 V1.0 假设 | 现网事实 | V1.1 落地法 |
|---|---|---|
| Wails 方法绑定 + 事件总线 | `api/bridge/v1/*.schema.json` → `generate-bridge` → `handlers_registry.go`；聊天才有长流 | 新 `agentHub.*` Bridge 方法；运行中 **400ms 轮询** `task.get`（延迟仍 <1s）。不手改 `bridge.ts` / `schema_generated.go` |
| 绿场 Go 骨架 + 新 `tasks` 表名 | 已有 `agent_run` / `command_job` / `tool_operations` | 新表必须叫 `agent_hub_*`，禁止复用 `agent.run.*`（那是月汐内部编排，不是外接 CLI） |
| `~/lunitide-workspace` | 数据根是 `%LocalAppData%/Lunitide`，会话目录是 `tool-workspaces/<ULID>` | 默认工作目录：`<dataRoot>/agent-hub/<YYYYMMDD>-<seq>/` |
| 三家同等 `--json` | Codex/Cursor/Kimi 能力未在本机产品里验证；Kimi 非交互未证实 | 适配器 **能力矩阵 gated**：未探测到非交互就不给「执行」，只给安装/登录引导。不引入 xterm 交互当 V1 主路径（产品 V1 本就禁止中途对话） |
| 侧栏被顶部导航替换 | `LaunchSidebar` + `Page` 联合类型 + `App.tsx` 条件渲染，无 React Router | 新增 `Page='agentHub'`；侧栏「项目」组与「设置」之间加一项。稿子里的顶栏变成 **页内 Tab** |
| 电光青柠 `#c8f04b` 当全站强调色 | `--tide1/#3bd6ff` 月潮色，☀/☾ 已切主题 | 调度台 **页内** 把稿子的 `--acc` 映射到 `--tide1`，沿用现有 `html[data-theme]`。禁止再做一颗主题按钮 |
| UI 写死「Pro 已登录 / 3 Agent 在线」 | 无法读各家会员接口 | 只展示探测态：`available` / `not_installed` / `not_logged_in` / `unknown` + 版本号。禁止写 Pro/会员 |
| 走 `command.run` 或用户白名单放行 | `command.run` 默认只允许 git/go；`commandworker.Run` 不能喂 stdin、15 分钟封顶 | **禁止** `command.run` / `command.start` / `commandworker.Run`。自写 Runner，只抄 Job Object 杀树 |
| 预览复用会话产物 API | `workspace.artifact.preview` 绑会话沙箱 | 新 `agentHub.file.preview` / `agentHub.file.open`，路径必须落在该任务 `work_dir` 内 |
| 完成通知用新 Toast 体系 | 自动化已有 Windows toast；主壳没有全局 Toast | 复用 `internal/scheduler` 的 toast 通道 + 页内横幅（对齐 `SessionPage` 的短通知）。双通道，不新建通知栈 |
| 用量写入 `token_ledger` | `token_ledger` **冻结** | CLI 回报的 token **只存 `agent_hub_tasks.tokens_used`，只展示**。文案：「消耗的是该 CLI 自己的会员额度」。零月汐模型调用 |
| 厂商 Logo /「AI 军团」营销 | 合规：不拿厂商标做宣传物料 | 字母徽标（C / Cu / K）+「调用本机已安装的 XX」。Hero 改为中性文案 |
| Wails build / 三台干净机当发布流程 | 现网是 `Build-Release.ps1` + Quality CI | 验收走现有 `go test` / `vitest` / `Build-Release.ps1`，不另起打包栈 |
| 「生成周报」快捷模板 | 月汐周报走 `office.generate`，不是外接 CLI | 模板改成「写周报 Markdown」。Word/Excel 仍走办公工作台 |

**V1 不做（保持可落地）：**

- 不嵌 Cursor/Codex/Kimi 的 GUI，不做 Tab 补全；
- 不做任务中途人机问答（非交互一把跑完；不能非交互的适配器保持不可执行）；
- 不接各家 API Key，不代打模型；
- 不改 `token_ledger`、邮件、共享日历、`html.gen` 三模板、办公 `office.generate` 主链；
- 不把调度台任务伪装成月汐对话消息或 `agent.run`；
- V1 不把 xterm 交互会话当 Kimi 主路径（`web/src/terminal/TerminalPanel.tsx` 已存在，留给 V2）；
- V1 **不加** `fsnotify` 或其它新 Go 依赖；不调用 `commandworker.Run`。

---

## 1. 背景与问题

用户本机可能装了 Codex CLI、Cursor CLI（`cursor-agent`）、Kimi CLI，并各自有会员。痛点仍成立：入口分散、过程要人盯、产物难找回。

**核心做法不变：** 月汐做进程调度 + 输出解析 + 产物归拢。账号留在用户和厂商之间。

**成本模型不变：** 调度台自己不打月汐模型；解析是纯代码。展示的 token 来自 CLI 事件（没有就显示「CLI 未回报」），不进冻结的 `token_ledger`。

---

## 2. 目标与非目标

### 2.1 目标（V1.2）

| # | 目标 | 衡量标准 |
|---|---|---|
| G1 | 办公菜单一级页「Agent 调度台」（默认隐藏，与办公工作台同一开关），展示最多三个适配器及探测态 | 冷启动后尽快返回 `DetectAll`（PATH + 常见安装目录）。矩阵允许非交互且找到 exe 即可执行；`--version` 是展示，超时不得把已装 CLI 判死。登录探测未证伪时不要写 Pro |
| G2 | 对 **当前探测为 available** 的适配器下发任务并自动执行 | 用户不离开月汐完成下发；不可用适配器按钮禁用 |
| G3 | 展示执行过程（步骤流） | 轮询间隔 400ms，UI 延迟 <1s |
| G4 | 完成后系统 toast + 页内横幅 + 产物清单 + 路径 | 至少一条通道到达；点击定位任务详情 |
| G5 | 任务工作目录内的代码/Markdown/图片可预览，其余系统打开 | 预览 API 拒绝目录外路径 |

### 2.2 能力 gated（不是失败，是诚实）

| 适配器 | V1 主路径 | 探测失败时 |
|---|---|---|
| Codex | `codex exec --json …`（M0 固化 fixture 后才标 available） | 灰卡 + 安装引导，不能点执行 |
| Cursor | `cursor-agent -p --output-format stream-json …`（同样要 M0 fixture） | 同上 |
| Kimi | `kimi -p … --output-format stream-json`（官方非交互已签出） | 未安装则灰卡 + 安装引导，不能点执行 |

三张卡可以同时展示。**「三家都能跑」不是 V1 验收项**；「不能跑的不能假装能跑」才是。

---

## 3. 用户与场景

**目标用户：** 本机至少装了一款上述 CLI 的开发者。

- S1：选可用 Agent → 写任务 → 用默认工作目录 → 执行 → 看时间线 → 通知 → 预览文件。
- S2：Codex 与 Cursor **同时**各跑一个（同 Agent 串行排队）。
- S3：按 Agent/日期找回历史任务目录。

若三家都未安装：工作台仍可打开，三张卡全灰，给安装说明。这算验收通过，不算功能缺失。

---

## 4. 菜单与信息架构

### 4.1 入口（对齐现网导航）

改这些现网点，不要新开壳：

- `web/src/app/appTypes.ts` 的 `Page` 增加 `'agentHub'`
- `web/src/app/LaunchSidebar.tsx`：在 **办公组** 增加「Agent 调度台」，由 `officeMenu.agentHub` 控制显示，**默认关闭**（与办公工作台同一套开关）
- `web/src/App.tsx`：必须写 `page==='agentHub'` 分支（懒加载仿 `OfficeStudioRoute`）。**漏写会掉进最后的 ProviderApp fallback**
- 侧栏落点：办公组内，不进项目组。用户覆盖优先于「项目组与设置之间单独一项」的旧写法
- 月伴入口保持首页按钮，不进调度台
- `agentHub.*` **不要**加入 `dataScopedMethod` 前缀（现网只扫 `agent.run.` / `workspace.` 等）。字段用 `taskId`，禁止用 `runId`（否则一旦误入 scope 会当 `agent-run` 鉴权失败）

页内四个 Tab（对应设计稿顶栏，不搬到全局顶栏）：**工作台 / 任务中心 / 任务详情 / 产物中心**。

### 4.2 页内信息架构

```
Agent 调度台
├── 工作台（默认）
│   ├── Hero（中性文案，不用「AI 军团」）
│   ├── Agent 胶囊：字母徽标、名称、探测态、版本（无则 —）
│   └── 命令台：任务、工作目录 chip、权限 chip（仅 Codex 有沙箱三档）、超时、执行
├── 任务中心：统计 + 筛选 + 列表（取消 / 重跑）
├── 任务详情：时间线 + 产物 + 打开目录 / 预览 / 取消
└── 产物中心：按任务归档，按 Agent / 日期 / 扩展名筛选
```

视觉：把 `index.html` 的玻璃拟态、胶囊、时间线、统计卡、产物网格做成 **页内 CSS**（`web/src/agentHub/agentHub.css`），`--acc` → `var(--tide1)`。主题跟月汐 ☀/☾。

---

## 5. 技术方案（对齐现网）

### 5.1 总体架构

```
React 页 AgentHubPage
    │  Bridge client（generate-bridge 生成）
    ▼
agentHub.detect / dir.pick / task.start / task.get / task.cancel / task.list /
agentHub.artifact.list / file.preview / file.open
    │
internal/agenthub  （新包，不进 chat lane）
    ├─ Detect
    ├─ Adapter（Codex / Cursor / Kimi）
    ├─ Runner（自写；抄 Job Object，不调用 commandworker.Run）
    ├─ Parser（JSONL 容错；非 JSON 行 → type=message）
    ├─ Scan（结束 Walk + 事件路径；不加 fsnotify）
    └─ Store（SQLite agent_hub_* + expectedSchemaSQL）
         └─ 完成 → scheduler.NewPlatformNotifier() + 页内横幅；下次 poll 看到 terminal
```

**禁止：** Wails、`command.run`、往 `token_ledger` 记账、把任务写进聊天 `messages`。

### 5.2 Bridge 方法（新 schema，禁止手改生成物）

在 `api/bridge/v1/` 增加方法并写入 `envelope.schema.json` 枚举，然后 `npm --prefix web run generate:bridge`，在 `handlers_registry.go` 注册。

| 方法 | 作用 |
|---|---|
| `agentHub.detect` | 返回三适配器状态（安装/版本/登录探测/能力：`streamJSON` / `nonInteractive`） |
| `agentHub.dir.pick` | 系统选目录对话框；取消返回 `{canceled:true}`。前端 deadline 须放宽到 10 分钟 |
| `agentHub.task.start` | 幂等键；仅 `available && nonInteractive` 可启动 |
| `agentHub.task.get` | 状态 + 已持久化事件 + 产物（供 400ms 轮询） |
| `agentHub.task.cancel` | Job Object 杀树；已落盘文件保留 |
| `agentHub.task.list` | 筛选 agent/status/日期 |
| `agentHub.artifact.list` | 跨任务产物 |
| `agentHub.file.preview` | 仅 `work_dir` 内；文本/md/图片；其他 `kind=file` + `notice` 提示用本机打开 |
| `agentHub.file.open` | 打开文件或所在目录（复用现有宿主 Reveal/ShellExecute 模式） |

`task.start` 请求：

```go
type TaskRequest struct {
    TaskID     string `json:"taskId"`     // 可选，服务端 ULID
    Agent      string `json:"agent"`      // codex | cursor | kimi
    Prompt     string `json:"prompt"`
    WorkDir    string `json:"workDir"`    // 空则自动分配；必须在 agent-hub 根下或用户确认后的目录
    Sandbox    string `json:"sandbox"`    // read-only | workspace-write | full-access（仅 Codex 有意义）
    TimeoutMin int    `json:"timeoutMin"` // 默认 30，最大 120
}
```

标准事件 / 结果结构沿用 V1.0 的 `AgentEvent` / `TaskResult` / `Artifact`（字段名不要再改，前端时间线只认这一套）。

适配器接口：

```go
type AgentAdapter interface {
    Name() string
    Detect(look LookPath, version VersionRunner) AgentStatus
    BuildCommand(req TaskRequest) (exe string, args []string, stdin []byte, err error)
    ParseLine(line string) (AgentEvent, bool)
}
```

`AgentStatus` 必须带：`State`、`Version`、`NonInteractive`、`StreamJSON`、`Hint`（中文，给卡片用）。

### 5.3 命令映射（M0 通过后才标 available）

**Codex**

```
codex exec --json --skip-git-repo-check --ignore-user-config --sandbox <sandbox> --cd <workDir> -o <workDir>/codex-last-message.md
```

prompt 走 **stdin**（避开 Win 32K 命令行上限）。`--skip-git-repo-check` 让默认 `agent-hub` 目录能跑；`--ignore-user-config` 不继承桌面端钉死的模型/MCP（登录仍走 `CODEX_HOME`）；`-o` 保证成功时至少有一份可预览产物。`--sandbox` 三档；Windows 上沙箱不是安全边界，`full-access` 仍要二次确认。

**Cursor**

```
cursor-agent -p --force --output-format stream-json
```

工作目录用进程 `Dir`；`--force` 只允许默认自动目录或用户确认过的目录。UI 写明：将自动改工作目录内文件。

**Kimi**

```
kimi -p <prompt> --output-format stream-json
```

工作目录用进程 `Dir`。`-p` 模式官方按 `auto` 权限、不弹 TUI。禁止为了凑数去嵌交互终端。

### 5.4 Runner（必须自写）

禁止调用：`commandworker.Run`、`command.start`、`command.run`、对话 `terminals`。

自写要求（对照 `internal/commandworker/worker_windows.go` 抄，不要 import 其 `Run`）：

- 子进程 `CREATE_SUSPENDED` → 加入 **新 Job Object**（`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` + ActiveProcess）→ Resume。
- `CREATE_NO_WINDOW`，不弹控制台。
- **stdin：写入完整 prompt 后关闭写端**（与 commandworker「立刻关 stdin」相反）。
- stdout/stderr 合并，**按行**交给 `ParseLine`，每行入 `agent_hub_events`；不在内存攒整份输出。
- 超时默认 30 分钟、最大 120 分钟（`commandworker.TimeoutHardCap` 是 15 分钟，所以不能走它）。
- 同 Agent 同时最多 1 个 `running`，其余 `queued`；不同 Agent 可并行。
- 取消/超时：`TerminateJobObject`，已落盘文件保留。
- exe 必须 `LookPath` 后的绝对路径；`Dir` 必须绝对路径。
- 环境：继承当前用户环境（不要用 commandworker「整表替换且可空」的默认）。
- 超长 prompt：stdin **写入失败则整单失败**（不默默继续）。V1 不实现 `.agenthub-prompt.txt` 回退，除非本机证实该 CLI 只吃文件。
- 默认工作目录：`dataRoot.PrepareSubdirectory("agent-hub")` 下 `<YYYYMMDD>-<seq>/`，用 `Mkdir` 原子占号。用户自选目录：拒绝盘符根与 Windows 系统目录；选目录时前端二次确认（可能改文件）。
- 进程重启：`Recover()` 把遗留 `running` 标失败「应用重启后未能继续该任务」，终扫已落盘文件；内存里的自定义超时不恢复，排队任务按 30 分钟再跑。

### 5.5 产物

1. 解析到的 `file_write` 路径（规范化后仍在 `work_dir` 内才标 `source=event`；目录外标 `outside`）。
2. 结束时 `filepath.Walk(work_dir)` 并集（权威清单）。
3. 运行中可选：`task.get` 对 `work_dir` 做一层 `ReadDir` 提示，**不加 fsnotify 依赖**。

### 5.6 SQLite（新迁移，不改旧表）

下一号迁移（现网最新为 `0152_model_fit_qualification.sql`，落地时用当时下一个号，例如 `0153_agent_hub.sql`）。必须同时改：

1. `internal/storage/sqlite/store.go` 的 `manifest` + SHA256
2. 同文件 `expectedSchemaSQL`（`table:` / `index:` 的 sqlite_schema 原文必须字节一致）
3. `migrations` embed 文件数与 manifest 条数一致

id 用现网 ULID 约束（不要只写 `length=26`）：

```sql
id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')
```

```sql
CREATE TABLE agent_hub_tasks (
  id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
  agent TEXT NOT NULL CHECK (agent IN ('codex','cursor','kimi')),
  prompt TEXT NOT NULL CHECK (length(prompt) BETWEEN 1 AND 100000),
  work_dir TEXT NOT NULL,
  sandbox TEXT,
  status TEXT NOT NULL CHECK (status IN ('queued','running','success','failed','timeout','cancelled')),
  exit_code INTEGER,
  tokens_used INTEGER NOT NULL DEFAULT 0,
  error_msg TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
  UNIQUE(idempotency_key)
);
CREATE TABLE agent_hub_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL REFERENCES agent_hub_tasks(id),
  seq INTEGER NOT NULL,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  ts TEXT NOT NULL,
  UNIQUE(task_id, seq)
);
CREATE TABLE agent_hub_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL REFERENCES agent_hub_tasks(id),
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  mime TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL CHECK (source IN ('event','scan','outside')),
  UNIQUE(task_id, path)
);
CREATE INDEX ix_agent_hub_tasks_agent_status ON agent_hub_tasks(agent, status, created_at);
CREATE INDEX ix_agent_hub_events_task_seq ON agent_hub_events(task_id, seq);
CREATE INDEX ix_agent_hub_artifacts_task ON agent_hub_artifacts(task_id);
```

标准事件（前端时间线只认这些 `type`，Parser 未知 JSON 也落 `message`）：

`started` / `step` / `message` / `file_write` / `usage` / `failed` / `finished`

`AgentEvent`：`seq int`, `type`, `title`, `detail`, `ts`（RFC3339）。`usage` 的 token 写入 `tokens_used`，**不写 `token_ledger`**。

### 5.7 前后端通信

| 方向 | 通道 | 内容 |
|---|---|---|
| 前端→引擎 | Bridge 方法 | 见 §5.2 |
| 运行中 | `setInterval(400ms)` → `agentHub.task.get` | 事件/状态/产物增量用 `seq` 去重 |
| 完成 | 下次 poll 看到 terminal + toast/横幅 | 不依赖新的全局 EventBus |

Toast 调用 `scheduler.NewPlatformNotifier().Notify`：title 1–120 字、body 1–500 字（超长截断）。PowerShell 失败不挡页内横幅。

---

## 6. 前端详设（在现网壳里长）

### 6.1 工作台

- 胶囊三态灯：绿 `available`、灰 `not_installed`、黄 `not_logged_in`、虚线 `unknown`。
- 未安装：点击只弹中文安装说明（官方文档链接用纯文本，不内嵌下载器）。
- 快捷模板：写工具脚本 / 重构模块 / 写技术文档 / 修复 Bug / **写周报 Markdown**。
- 「执行」禁用：未选可用 Agent、prompt 空、该 Agent 已有 running（可排队时按钮仍可用，文案改为「加入排队」）。

### 6.2 任务详情

- 左时间线，右产物；状态条：耗时、token 或「CLI 未回报」、退出码、错误摘要。
- 预览：新建 `web/src/agentHub/AgentHubFileInspector.tsx`。只抄 `ArtifactInspector` 的展示分类（text / markdown / image /「请用本机打开」），**禁止 import 该组件**（它绑 `sessionId`）。预览大小/截断对齐 `handleWorkspaceArtifactPreview`（8MiB 读、非图 256KiB 展示）。打开目录走同包 `openArtifactTarget`。

### 6.3 任务中心 / 产物中心

- 筛选条必须有（设计稿缺的要补）：状态、Agent、日期。
- 运行中/排队中显示「取消」。

---

## 7. 异常与边界

| 场景 | 处理 |
|---|---|
| CLI 未安装 | 灰卡 + 引导；start 返回 `AGENT_NOT_AVAILABLE` |
| 未登录/会员过期 | 黄灯或运行失败解析 stderr；中文错误，不把 raw English 甩给用户（沿用现网 `UserError` 规则） |
| 限流 | 状态 `failed`，`error_msg` 写「订阅限流，请稍后重试」 |
| 超时/取消 | 杀树，保留产物 |
| 写到 work_dir 外 | 清单标 `outside`，默认目录任务应尽量不出现 |
| 用户指定 `C:\`、用户主目录根 | **拒绝盘符根与 Windows 系统目录**；自选其它目录前端二次确认 |
| 非 JSON 行 | `message` 原样入时间线 |
| M0 未通过的适配器 | 代码可存在，Detect 永不标 `available` |

---

## 8. 合规与安全

- 不存储各家凭证；
- 文案「调用本机已安装的 XX」；字母徽标，不用官方 Logo 文件；
- `--force` / `full-access` 风险明示；
- 产物不上传；
- Runner 不走对话 `command.run`，避免和办公「禁止 command.run 拼 OOXML」规则缠在一起；
- 调度台任务不得调用 `office.generate` 冒充月汐办公交付。

---

## 9. 里程碑（仍约 4 周，但依赖现网，不是绿场）

| 里程碑 | 内容 | 工期 |
|---|---|---|
| M0 | 本机实测三家 CLI；写出 fixture 与能力矩阵。**没有 fixture 的适配器不得标 available** | 0.5 周 |
| M1 | `internal/agenthub` + 迁移（manifest **和** expectedSchemaSQL）+ Detect + 自写 Runner + Store + 单测；Codex 仅 M0 过才 Execute | 1 周 |
| M2 | Bridge schema + 侧栏页 + 工作台/详情轮询 + toast/横幅 + 页内视觉 | 1 周 |
| M3 | Cursor（若 M0 过）+ 任务中心/产物中心 + 预览/打开；Kimi 仅 Detect 或完整适配（按矩阵） | 1 周 |
| M4 | 异常、排队、高危路径、Quality 门 + 签名包（走现有 Release） | 0.5 周 |

---

## 10. 验收标准

1. 未安装 / 探测超时 / 可用 三种卡片态与 `DetectAll` 一致；禁止出现「Pro 已登录」除非未来真能量到（V1 不做）。
2. 对 **available** 适配器：发起 → 轮询时间线 → 完成横幅/toast → 详情，无需离开月汐。
3. 时间线 `seq` 单调；UI 延迟 <1s（400ms poll）。
4. 终扫产物与磁盘一致（`outside` 单独列出）。
5. 通知或页内横幅可点进详情；预览拒目录外路径。
6. 同 Agent 排队；取消/超时后无孤儿进程（Job Object）。
7. 抓包：调度台路径不打月汐模型 API；`token_ledger` 行数不因调度台任务增加。
8. `npm run generate:bridge` 后生成文件无手改漂移。
9. 现有对话 / 办公 / 技能 / 专家 / 月伴回归：`vitest` + 相关 Go 包测试全绿。
10. `go.mod` 不出现 `fsnotify`；`internal/agenthub` 不 import `commandworker` 的 `Run`（可抄 Job Object 源码进本包）。
11. 点「Agent 调度台」渲染的是 Hub 页，不是供应商页。

---

## 11. 设计稿怎么用

`index.html` 是交互气质样板（Hero、胶囊、命令台、统计卡、时间线、产物网格、完成条）。落地时：

- **保留：** 居中命令台、胶囊选择、页内四态、玻璃面板、呼吸灯/进度环（用 `--tide1`）。
- **丢掉：** 独立顶栏品牌、◐ 主题钮、「3 Agent 在线」、厂商营销句、写死的 Pro 文案、青柠强调色、`~/workspace/0911-03` 假路径。
- **补上：** 状态灯、筛选、取消、退出码、额度说明、探测失败引导。
