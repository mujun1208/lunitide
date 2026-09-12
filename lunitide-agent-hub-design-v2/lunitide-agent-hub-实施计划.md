# 月汐「Agent 调度台」实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现网壳里新增一级页「Agent 调度台」，调度本机已装 CLI；不能跑的适配器保持灰卡，不假装三家齐。

**Architecture:** 新包 `internal/agenthub` 自写进程（抄 Job Object，**禁止** `commandworker.Run`）。新 Bridge `agentHub.*` + 400ms 轮询。SQLite `agent_hub_*`。前端 `Page='agentHub'` + 页内 Tab。默认目录钉在 `PrepareSubdirectory("agent-hub")`。

**Tech Stack:** 现网 Go Engine + WebView2 + React + `api/bridge/v1` + `npm --prefix web run generate:bridge`。不加 fsnotify。

**Spec:** `lunitide-agent-hub-design-v2/lunitide-agent-hub-prd.md` **V1.2**

## Global Constraints

- 不是 Wails；无 React Router；`decodePayload` 保持 `DisallowUnknownFields`。
- 禁止手改 `web/src/generated/bridge.ts`、`internal/bridge/schema_generated.go`。
- 禁止 `command.run` / `command.start` / `commandworker.Run`；禁止把任务写入 `agent.run` / 聊天 `messages`。
- `token_ledger` 冻结；CLI token 只写 `agent_hub_tasks.tokens_used`。
- 不加 `fsnotify` 或其它新 Go 依赖。
- 不拆 `OfficeStudioPage.tsx` / `SessionPage.tsx`；不改办公 `office.generate` 主链。
- 主题只用 `--tide1` + 现有 ☀/☾；无第二颗主题钮、无全站青柠。
- `agentHub.*` 不要加入 `dataScopedMethod`；字段用 `taskId` 不用 `runId`。
- `App.tsx` 必须有 `page==='agentHub'`，否则 fallback 是供应商页。
- 没有 M0 fixture 的适配器 Detect 不得返回 `available`。
- 用户没要求不要 commit；不要开新 worktree。

---

## 文件地图（只动这些）

**新建**

- `internal/agenthub/types.go` — `AgentStatus` / `TaskRequest` / `AgentEvent` / `TaskResult` / `Artifact`
- `internal/agenthub/detect.go` + `detect_test.go`
- `internal/agenthub/adapter.go`
- `internal/agenthub/codex.go` + `codex_test.go`
- `internal/agenthub/cursor.go` + `cursor_test.go`
- `internal/agenthub/kimi.go` + `kimi_test.go`
- `internal/agenthub/parser.go` + `parser_test.go`
- `internal/agenthub/runner.go` + `runner_test.go`
- `internal/agenthub/proc_windows.go` — 抄 Job Object + **可写 stdin**（不要调用 `commandworker.Run`）
- `internal/agenthub/proc_other.go` — 非 Windows 返回 unsupported（产品只发 Windows）
- `internal/agenthub/scan.go` + `scan_test.go` — 终扫 Walk，无 fsnotify
- `internal/agenthub/store.go` — 经现有 sqlite 连接
- `internal/agenthub/testdata/*.jsonl`
- `internal/agenthub/capability-matrix.md` — M0 产出
- `migrations/0153_agent_hub.sql`（若当时最新号已变，用下一个号）
- `api/bridge/v1/agentHub.detect.schema.json`
- `api/bridge/v1/agentHub.task.start.schema.json`
- `api/bridge/v1/agentHub.task.get.schema.json`
- `api/bridge/v1/agentHub.task.cancel.schema.json`
- `api/bridge/v1/agentHub.task.list.schema.json`
- `api/bridge/v1/agentHub.artifact.list.schema.json`
- `api/bridge/v1/agentHub.file.preview.schema.json`
- `api/bridge/v1/agentHub.file.open.schema.json`
- `internal/app/agenthub_handlers.go` + `agenthub_handlers_test.go`
- `web/src/agentHub/AgentHubPage.tsx`
- `web/src/agentHub/AgentHubWorkbench.tsx`
- `web/src/agentHub/AgentHubTasks.tsx`
- `web/src/agentHub/AgentHubDetail.tsx`
- `web/src/agentHub/AgentHubArtifacts.tsx`
- `web/src/agentHub/AgentHubFileInspector.tsx` — **不要** import `ArtifactInspector`
- `web/src/agentHub/agentHub.css`
- `web/src/agentHub/agentHubApi.ts`
- `web/src/agentHub/*.test.tsx`

**修改**

- `internal/storage/sqlite/store.go` — `manifest` **和** `expectedSchemaSQL`
- `api/bridge/v1/envelope.schema.json` — 按字母序插入 `agentHub.*`（在 `agent.run.*` 之后，因为 `H` > `.`）
- `internal/app/handlers_registry.go`
- `internal/bootstrap/wire.go` — `dataRoot.PrepareSubdirectory("agent-hub")` 交给 Engine
- `internal/app/engine.go` — 挂 `agenthub` 服务 + 工作根，**不要**误加进 `dataScopedMethod`
- `web/src/app/appTypes.ts` — `Page` 加 `'agentHub'`
- `web/src/app/LaunchSidebar.tsx` + `LaunchSidebar.test.tsx`
- `web/src/App.tsx` + 现有 App 测试（若有按页断言）
- `web/src/styles.css` — 仅当页内 CSS 需要共享变量时，优先写在 `agentHub.css`

**不改**

- `token_ledger` 迁移与写入
- 生成物（只允许 `generate:bridge` 改）
- `OfficeStudioPage.tsx` / `SessionPage.tsx`
- `index.html`（气质参考；可加「非生产壳」注释）
- `internal/app/org_lifecycle.go` 的 `dataScopedMethod`（保持 `agentHub.` 不在列表里）

---

## 对照现网的硬陷阱（先读再写）

| 陷阱 | 证据 | 正确做法 |
|---|---|---|
| `commandworker.Run` 立刻关 stdin | `worker_windows.go`：「stdin sees immediate EOF」 | 自写 `proc_windows.go`：先 `Write` prompt 再关写端 |
| 15 分钟 / 4MiB 硬帽 | `TimeoutHardCap=15m`，`OutputHardCap=4<<20` | Hub 超时 30–120min；按行落库，不丢后续 JSONL |
| 无 fsnotify | `go.mod` 无此依赖 | 终扫 Walk + 事件路径 |
| `ArtifactInspector` 绑会话 | props=`sessionId`，调 `workspace.artifact.preview` | 新组件 + `agentHub.file.preview` |
| 漏 `expectedSchemaSQL` | `store.go` `unknown schema object` | dump 测试贴原文 |
| `App.tsx` fallback | 最后一分支是 `ProviderApp` | 显式 `page==='agentHub'` |
| 子目录必须钉住 | `datadir.PrepareSubdirectory` | `agent-hub` 与 `tool-workspaces` 同级 |
| Toast 长度 | `Notify` title≤120 body≤500 | 截断；失败不挡横幅 |

---

## 第一部分：与设计稿差异（已拍板）

| # | 稿子 | V1.2 |
|---|---|---|
| D1 | 顶栏四链 + ◐ | 侧栏一级页 + 页内 Tab；主题只用底栏 ☀/☾ |
| D2 | 「Pro 已登录」 | 四态灯 + 版本 |
| D3–D6 | 无筛选/取消/退出码 | 必须有 |
| D7 | 无额度说明 | 「消耗的是该 CLI 自己的会员额度」 |
| D8 | 假 6 秒 Toast | `task.get` 400ms + 横幅 + `NewPlatformNotifier` |
| D9 | 稿内 `--acc` 现为近白，旧稿曾是青柠 | 产品 `--acc: var(--tide1)`（深 `#3bd6ff` / 浅 `#008ac5`） |
| D10 | 「生成周报」 | 「写周报 Markdown」 |
| D11 | 三家都能跑 | 能力矩阵；Kimi 无非交互则灰 |
| D12 | `~/workspace/0911-03` | `%LocalAppData%\Lunitide\agent-hub\YYYYMMDD-NN\` |

---

## 第二部分：M0 Spike（0.5 周，适配器门闩）

在开发者 Windows 上逐项做，fixture 放 `internal/agenthub/testdata/`：

```
□ 1. where.exe / LookPath：codex、cursor-agent、kimi（及常见安装路径）
□ 2. codex exec --json --sandbox workspace-write --cd <空目录>
     stdin：「只在本目录创建一个 hello.txt」
     保存完整 stdout、exit code、usage 字段是否存在
□ 3. 三档 sandbox 能否写出目录外文件（Windows 上多半无效 → UI 要写明）
□ 4. cursor-agent -p --force --output-format stream-json
     Dir=测试目录；存 stream-json
□ 5. 未登录时 stderr 特征（中文映射用）
□ 6. kimi --help；有无稳定非交互 argv
     有 → 再跑一条并存样例；无 → 矩阵 NO，V1 只 Detect
□ 7. 长 prompt（>32K）走 stdin 是否被接受
□ 8. 自写 Job Object 杀树：取消后无孤儿（不要用 commandworker.Run 做这条）
```

**门闩：** 矩阵 `nonInteractive=NO` 的适配器，生产 `Detect` 不得 `available`。壳（页+灰卡）可先做。

---

## 第三部分：分期任务

### Task 1: 迁移 + expectedSchemaSQL

**Files:**
- Create: `migrations/0153_agent_hub.sql`（或下一号）
- Create: `internal/storage/sqlite/agent_hub_dump_test.go`
- Modify: `internal/storage/sqlite/store.go`（`manifest` 末尾 + `expectedSchemaSQL`）

**Interfaces:**
- Produces: 表 `agent_hub_tasks` / `agent_hub_events` / `agent_hub_artifacts`，status 枚举见 PRD §5.6，artifact `source IN ('event','scan','outside')`
- ULID CHECK 必须与 `0152` 相同 glob

- [ ] **Step 1: 写 dump 测试（会失败）**

仿 `internal/storage/sqlite/m9_dump_evidence_test.go`：内存库只执行新 SQL，打印 `sqlite_schema`。另写：

```go
func TestAgentHubMigrationOpens(t *testing.T) {
    store := openTestStore(t) // 用现网测试 helper
    _, err := store.DB().Exec(`INSERT INTO agent_hub_tasks(id,agent,prompt,work_dir,status,created_at,idempotency_key)
        VALUES('01ARZ3NDEKTSV4RRFFQ69G5FAE','codex','hi',?, 'queued', datetime('now'), 'k1')`, t.TempDir())
    if err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: 跑测试确认失败**（表不存在或 manifest 条数不够）
- [ ] **Step 3: 写 SQL + 把 dump 输出贴进 `expectedSchemaSQL` + manifest SHA256**
- [ ] **Step 4: `go test ./internal/storage/sqlite/ -count=1` 通过**
- [ ] **Step 5: 仅当用户要求时再 commit**

---

### Task 2: 类型 + Detect

**Files:**
- Create: `internal/agenthub/types.go`, `detect.go`, `detect_test.go`

**Interfaces:**

```go
type AgentStatus struct {
    Name            string `json:"name"` // codex|cursor|kimi
    State           string `json:"state"` // available|not_installed|not_logged_in|unknown
    Version         string `json:"version"`
    NonInteractive  bool   `json:"nonInteractive"`
    StreamJSON      bool   `json:"streamJSON"`
    Hint            string `json:"hint"` // 中文
}

func DetectAll(ctx context.Context, look LookPath, runVersion VersionRunner) []AgentStatus
```

`available` **仅当**：exe 找到 **且** 该适配器能力矩阵 `nonInteractive=YES` **且** `--version` 在 2s 内成功。登录未证伪时不要标绿当「Pro」。

- [ ] **Step 1: 失败测试**

```go
func TestDetectMissing(t *testing.T) {
    st := DetectAll(ctx, func(string) (string, error) { return "", exec.ErrNotFound }, nil)
    if st[0].State != "not_installed" { t.Fatal(st[0]) }
}
func TestDetectTimeoutIsUnknown(t *testing.T) { /* VersionRunner 阻塞 3s → unknown */ }
func TestDetectNeverAvailableWithoutMatrix(t *testing.T) {
    // kimi 矩阵 NO：即使 LookPath 成功也不得 available
}
```

- [ ] **Step 2: 跑红 → 实现 `--version` 2s timeout → 跑绿**

---

### Task 3: Parser

**Files:** `parser.go`, `parser_test.go`, `testdata/codex-hello.jsonl`（M0 或最小伪造行）

**Interfaces:**

```go
func ParseLine(agent, line string) (AgentEvent, bool) // ok=false 表示空行丢弃
```

- [ ] 已知 fixture 行 → 标准 `AgentEvent`
- [ ] 垃圾行 → `type=message`，不 panic
- [ ] 缺字段不 panic
- [ ] `usage` 抽出非负 `tokens`；没有则 0（UI 显示「CLI 未回报」）

---

### Task 4: 自写进程 + Runner（假 CLI）

**Files:** `proc_windows.go`, `proc_other.go`, `runner.go`, `runner_test.go`

**Interfaces:**

```go
type ProcSpec struct {
    Exe, Dir string
    Args     []string
    Stdin    []byte
    Timeout  time.Duration // 默认 30m，上限 120m
}
func StartProcess(ctx context.Context, spec ProcSpec, onLine func(string)) (exit int64, timedOut bool, err error)
```

**禁止** `import` 后调用 `commandworker.Run`。Windows 实现对照 `worker_windows.go` 抄 Job Object，但 stdin 路径相反。

- [ ] **假 exe 测试（Windows）：** 写一个 `go test` 子进程：从 stdin 读文本，向 stdout 打两行 JSON，在 `Dir` 写 `hello.txt`，exit 0。Runner 应 `success`，事件 ≥2，终扫含 `hello.txt`。
- [ ] **取消测试：** 子进程 `sleep`；cancel 后 2s 内进程消失（`syscall` / `FindProcess`）。
- [ ] **超时测试：** Timeout=200ms，子进程睡 5s → `timeout`，无孤儿。
- [ ] **stdin 测试：** 断言子进程读到完整 prompt（证明没走 commandworker 的 EOF-stdin）。

Codex `BuildCommand` 仅在 M0 通过后实现真实 argv；本 Task 可用假 exe。

---

### Task 5: 终扫

**Files:** `scan.go`, `scan_test.go`

- [ ] 只收录 `work_dir` 内常规文件
- [ ] 事件给出的外部路径 → `source=outside`
- [ ] 忽略 `.agenthub-prompt.txt`（若仍在）
- [ ] 不引用 fsnotify

---

### Task 6: Bridge schema + handlers

**Files:** 8 个 `api/bridge/v1/agentHub.*.schema.json`；`envelope.schema.json`；`internal/app/agenthub_handlers.go` + test；`handlers_registry.go`；`engine.go` / `wire.go`

仿 `api/bridge/v1/people.file.open.schema.json`：`additionalProperties: false`，`x-method` / `x-result` / 正负例。

`task.start` payload 字段（禁止多字段，否则 `decodePayload` 炸）：

```json
{ "taskId", "agent", "prompt", "workDir", "sandbox", "timeoutMin", "idempotencyKey" }
```

均为 schema 声明的可选/必选；Go 结构体 json tag 必须同名。

- [ ] 加 schema → `npm --prefix web run generate:bridge`
- [ ] 生成文件只允许生成器改动
- [ ] handler 测试：
  - `not_installed` / `nonInteractive=false` → `AGENT_NOT_AVAILABLE`，中文
  - `file.preview` `..\Windows\win.ini` → 失败，不读盘
  - `file.open` 路径逃逸失败
  - start 成功后 `task.get` 含 events
- [ ] `RuntimeHandlers` 登记 8 个方法
- [ ] `dataScopedMethod` **不要**加 `agentHub.`

预览实现抄 `handleWorkspaceArtifactPreview` 的 kind 分支，但根是任务 `work_dir`，调用 `openArtifactTarget`。

---

### Task 7: 导航

**Files:** `appTypes.ts`, `LaunchSidebar.tsx`, `LaunchSidebar.test.tsx`, `App.tsx`

侧栏现序（不要打乱）：

```
办公组 → 对话 → 项目组（项目管理/技能/专家/MCP/能力包/资产）
→【在此插入】Agent 调度台
→ launch-bottom：设置 + 头像 + ☀/☾ + 中/EN
```

插入位置：`</nav>` 之后、`<div className="launch-bottom">` 的设置按钮之前。

- [ ] 测试：点击「Agent 调度台」`setPage` 收到 `'agentHub'`
- [ ] 测试：`page==='agentHub'` 时该按钮 `active`，设置按钮不 active
- [ ] `App.tsx` 在 `page==='mro'` 与 fallback **之间**加：

```tsx
:page==='agentHub'?<PageErrorBoundary label="agentHub"><Suspense fallback={<p role="status">正在打开 Agent 调度台…</p>}><AgentHubPage/></Suspense></PageErrorBoundary>
```

- [ ] 组件测试或 App 测试：`setPage('agentHub')` 后屏幕有「Agent 调度台」/工作台 Tab，**没有**「模型与供应商」主标题

---

### Task 8: 工作台 + 详情

**Files:** `web/src/agentHub/*`

- [ ] 三态/四态胶囊；`available` 才能执行
- [ ] 模板含「写周报 Markdown」，**没有**「生成周报」
- [ ] Hero：「一个入口，调度本机已安装的 CLI」—— 不要「AI 军团」
- [ ] 额度一句
- [ ] 详情：`vi.useFakeTimers` + 400ms poll `task.get`；`seq` 去重
- [ ] `agentHub.css`：`--acc: var(--tide1)`；无 `#themeBtn`
- [ ] 进行中条可点进详情；取消调 `task.cancel`

---

### Task 9: 通知

- [ ] 完成态页内横幅可点进详情（主路径）
- [ ] `scheduler.NewPlatformNotifier().Notify("Lunitide", body)`；超长截断；error 只记日志
- [ ] 不新建 Toast 组件栈；设计稿右下角 Toast 动效可做页内横幅

---

### Task 10: Cursor / Kimi 适配器

- [ ] Cursor：M0 fixture → `BuildCommand` + `ParseLine`
- [ ] Kimi：矩阵 NO → 单测 `Detect` 永不 `available`，`task.start` 被拒
- [ ] 矩阵 YES 才写 Execute

---

### Task 11: 任务中心 / 产物中心

- [ ] 筛选：状态 / Agent / 日期
- [ ] 排队文案、取消、重跑（**新 taskId + 同 prompt/agent**，不要 upsert 旧行）
- [ ] 产物网格 + 扩展名筛选
- [ ] `AgentHubFileInspector` 预览/打开；路径测试已在 Task 6

---

### Task 12: 回归门

- [ ] `go test ./internal/agenthub/ ./internal/app/ -count=1`
- [ ] `go test ./internal/storage/sqlite/ -count=1`
- [ ] `npm --prefix web test -- --run agentHub`
- [ ] `npm --prefix web test -- --run src/app/LaunchSidebar`
- [ ] `go.mod` 无 fsnotify
- [ ] 现网 Quality 同款命令（对话/办公相关包）全绿
- [ ] **不要**为此任务改 VERSION / 打包，除非用户另令

---

## 第四部分：验收映射

| PRD §10 | 怎么测 |
|---|---|
| 探测三态 | 拔 exe / 超时 / 真安装 |
| 不离壳闭环 | 仅 available 适配器 |
| 延迟 <1s | fake timer + 400ms |
| 产物无漏 | Walk vs 磁盘 |
| 通知可定位 | 横幅点击 |
| 排队/取消 | 同 Agent 连点 + cancel 无孤儿 |
| 零月汐模型 / ledger 不动 | handler 不碰 provider；ledger count 断言 |
| 生成文件无手改 | `generate:bridge` + diff |
| 无 fsnotify / 无 commandworker.Run | grep |
| 不是供应商页 | App 分支测试 |
| 回归 | 对话/办公/技能现有测试 |

---

## 第五部分：纪律

1. **没有 fixture，不准 available。** 壳可以先上。
2. **Parser 先容错再精确。**
3. **不扩 scope：** 不做交互 Kimi、不做 API Key、不做第二套主题、不把调度台并进办公生成、不加新依赖。
4. **用户没要求就不要 commit。**

---

## 执行方式（方案写完后，现在不要开工）

实现阶段二选一（到时候再选）：

1. **Inline** — 本会话按 Task 1→12 做（推荐，文件边界清楚）
2. **Subagent-Driven** — 每 Task 开子代理，Task 之间人工过门

**现在不要写业务代码。** 先确认 V1.2 PRD。确认后 M0 可立刻在本机跑。
