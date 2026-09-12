# 月汐「Agent 调度台」V1.3.3 落地规格

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development（推荐）或 executing-plans，按 Task 勾选执行。每步先红后绿。  
> **本文替代** `lunitide-agent-hub-三场景-prd.md` V1.3.2 与 `lunitide-agent-hub-三场景-实施计划.md`。V1.2 引擎约束（`lunitide-agent-hub-prd.md`）全部继承。  
> **批准后只改业务代码。** 未批准前不要施工。

| 字段 | 值 |
|---|---|
| 产品 | 月汐 / Lunitide（Go Engine + WebView2 + React） |
| 版本 | **V1.3.3** |
| 日期 | 2026-09-12 |
| 状态 | **可一次性执行的落地规格**（复盘 + 已锁定产品 + 逐步计划） |
| 现网基线 | Agent 调度台 **V1.2 已落地**；V1.3.2 **设计已写、代码未动** |
| Spec + Plan | 本文一份；不要再按 V1.3.2 计划施工 |

**Goal:** 四张卡（做 PPT / 写新项目 / 改现有代码 / 其它任务）+ 工作台把本机文件/资料夹复制进 `.agenthub-inbox` 交给三家 CLI；捷径锁 Agent；自由模式仍可任选三家做任意事。任务结束能在详情里看见 inbox 入参与本轮变更，且 **不会因为新 source 值写爆 SQLite**。

**Architecture:** 场景前缀只在前端拼。后端适配器不读 scene。传文件走新方法 `agentHub.inbox`，不改 `task.start` schema。扫描在内存标 `inbox`/`changed`；**入库前压成 `scan`，读出时按路径+mtime 还原**（0153 CHECK 不能扩，且 SQLite 改 CHECK 必须重建表，本迭代不加 0154）。顺手把 `dir.pick`/`inbox` 的 Go 截止改成 600s。

**Tech Stack:** 现网 `internal/agenthub` + `web/src/agentHub` + `npm --prefix web run generate:bridge`。

---

## Global Constraints

- 禁止 `commandworker.Run` / `command.run` / `fsnotify` / 手改生成物。
- 禁止改 `OfficeStudioPage.tsx` / `SessionPage.tsx` / `office.generate` / `token_ledger`。
- 禁止去掉 Codex `--ignore-user-config`。
- 禁止删掉自由选 Agent。
- 禁止把传文件做成「请自己去资源管理器拷」而不做按钮。
- 禁止复用 `desktop.files.pick` / `people.file.*` / `attachment.*`。
- 禁止移动或删除用户原文件。
- 禁止加 `0154`、禁止改 `0153` 的 CHECK、禁止加 scene 列。
- 字段 `taskId` 不是 `runId`；所有 `agentHub.*` 不进 `dataScopedMethod`。
- 新 schema 必须 `npm --prefix web run generate:bridge`，禁止手改 `web/src/generated/bridge.ts` / `internal/bridge/schema_generated.go`。
- 用户没要求不要 commit、不要改 VERSION、不要打包、不要推送。
- `go test` / vitest 通常需要完整权限；vitest 必须在 `web/` 下跑。
- PowerShell 用 `;` 不连接 `&&`。
- 不跑生产 `%LocalAppData%\Lunitide` 引擎。
- 运行中任务的 inbox 必须能被 `task.get` 看见（修 `peekWorkDir`）。

---

## 0. 先结论

**V1.3.2 文档不能按原文一次性落地。** 产品方向对，但有 3 个会让「100% 真实实现」失败的规格洞，加上现网仍停在 V1.2。

| 维度 | 现状 | 能否按 V1.3.2 原文一次做完 |
|---|---|---|
| 用户需求 | 三捷径 + 自由调度 + **工作台传文件** | 需求本身可落地 |
| V1.3.2 PRD | 产品锁定正确；**漏了 0153 `source` CHECK**；漏了运行中 peek 看不到子目录 inbox | 按原文写库会失败；运行中看不到入参 |
| 现网代码 | V1.2 引擎/九个 Bridge/自由工作台已绿 | **四卡、inbox、Kimi 短 argv、Cursor `--workspace`、Go 600s 截止全部未做** |

V1.3.3 把那 3 个洞锁死，并给出可勾选的 Task。按本文做完，需求可以 100% 在现网语义下落地。  
**不能保证的只有 CLI 自己的能力**（Kimi 未必每次交出 pptx；Cursor/Codex 未必一次编译过）。产品验收是「诚实调度 + 文件交到工作区 + 时间线/产物收回」，不是「代三家保证质量」。

---

## 1. 三维度复盘

### 1.1 用户需求（要什么）

原话落地为五条，缺一条都不算做完：

| # | 需求 | 做成什么样才算真的 |
|---|---|---|
| 1 | 做 PPT | 点卡 → 锁 Kimi → 必须选文件夹 → 可添加参考文件 → 执行。能找到则带 `kimi-slides`。结束能打开 `.pptx`，或诚实说没有文稿 |
| 2 | 写新项目 | 点卡 → 锁 Cursor → 必须选项目根 → `--workspace` 钉死该根。可把需求/设计拷进 inbox |
| 3 | 改现有代码 | 点卡 → 锁 Codex → 必须选仓库根。可把缺陷说明/截图拷进 inbox。变更清单不含 `node_modules` |
| 4 | 三家也能做别的 | **第四张卡「其它任务」**：自己选 C / Cu / K，自由写任务。写文档、总结、小游戏、办事收回产物 |
| 5 | **把文件传给 Agent** | 工作台「添加文件 / 添加资料夹」：本机多选 → **复制**进 `workDir/.agenthub-inbox/` → 三家都能读到 → 改副本或写出新文件 → 时间线 + 产物收回。原件不动 |

「自己去资源管理器拷」不是传文件。只把绝对路径写进 prompt、不复制，Cursor/Kimi 经常读不到、Codex 沙箱也会拒。

### 1.2 V1.3.2 PRD（写了什么、对不对）

**对的（V1.3.3 全盘继承）：**

- 四卡互斥；捷径锁 Agent；自由不丢。
- 前缀只在前端拼；适配器不读 scene；不加 scene 列。
- inbox 复制方案；不复用办公/会话/桌面附件总线。
- 唯一新 Bridge：`agentHub.inbox`。不改 `task.start` schema。
- Kimi 长文进 `.agenthub-prompt.txt`；Cursor 带 `--workspace`；Codex 保留 `--ignore-user-config`。
- 扫描跳过依赖目录；默认 UI：`event ∪ changed ∪ inbox`；永不展示 `outside`。
- `dir.pick` / `inbox` 截止 600_000。
- 红线：不焊 `office.generate`、不用 ArtifactInspector、不调用 `commandworker.Run`。

**错的 / 不完整的（按原文施工会翻车）：**

| # | V1.3.2 写法 | 现网事实 | V1.3.3 锁定 |
|---|---|---|---|
| D1 | 「不加 0154」且 API `source` 直接用 `inbox`/`changed` | `migrations/0153_agent_hub.sql`：`source CHECK (event\|scan\|outside)`。`UpsertArtifact` 原样写入。SQLite 改 CHECK 必须重建表 | **不加迁移。** 内存/API 用 `inbox`/`changed`；**入库压成 `scan`；读出按路径+mtime 还原** |
| D2 | 「任务详情默认看见 inbox」只改结束扫描 | `GetTask` 在 running/queued 时用 `peekWorkDir`，**只读工作目录顶层**。inbox 在子目录，运行中永远看不见 | `peekWorkDir` 额外 `ReadDir(.agenthub-inbox)`，标 `inbox` |
| D3 | `BuildCommand` 写 `.agenthub-prompt.txt`，测试用 `t.TempDir()` | 现网 `TestKimiBuildCommand` 用 `D:\work` 且断言 argv 含长文。新测试必须改现网旧断言，且 **禁止再用 `D:\work` 当真目录** | 用 `t.TempDir()`；先红后改适配器 |
| D4 | 工作台测试「沿用 stubLists」后直接点执行 | 现网 `AgentHubPage.test.tsx` 假定：无卡、胶囊常显、`HUB_TEMPLATES`、超时 chip、沙箱常显。四卡后这些测试会集体红 | Task 6 **重写**这些用例，**不得删**自由路径（其它任务 + 任选 Agent） |
| D5 | 计划写了 `TestInboxAllocates` / `TestInboxSkipsTooLarge` 在 PRD 清单，实施计划测试代码漏了 | 漏测则 F3/F8 会空心 | Task 2 补全这两个测试 |
| D6 | 截止只改 Go + 前端 `dir.pick` | 前端 `capBridgeDeadlineMs` / `createAgentHubBridge` 已给 `dir.pick` 600s；**Go `MaxDeadlineMS` 仍 30s**。宿主会拒长截止，选目录对话框是现网已存在的坑 | Task 3 必须同时改 Go 与 inbox 前端 |

### 1.3 现网代码（实际有什么）

**V1.2 已落地、必须保住：**

- 9 个 Bridge：`detect` / `dir.pick` / `task.start|get|cancel|list` / `artifact.list` / `file.preview` / `file.open`
- `internal/agenthub`：探测、排队、Job Object Runner、JSONL 解析、默认 `agent-hub\日期-序号`、0153 表
- Codex argv 已含 `--ignore-user-config`；适配器无 scene（正确）
- 前端：自由胶囊 + 可选目录 + 模板 + 400ms 轮询 + `AgentHubFileInspector`（没有 ArtifactInspector）
- 前端 `AGENT_HUB_DIR_PICK_MS = 600_000` 仅对 `agentHub.dir.pick`
- `promptFileName = ".agenthub-prompt.txt"` 扫描时跳过，**但没有适配器写它**
- `agentHub.*` 不在 `dataScopedMethod` 前缀里（正确）

**V1.3.2 未落地（文件确认不存在）：**

- `api/bridge/v1/agentHub.inbox.schema.json`
- `internal/agenthub/inbox.go` / `skills.go` / `pick_files_windows.go` / `pick_files_other.go`
- `ScanWorkDir` 仍是 2 参；不跳过 `node_modules`；无 `inbox`/`changed`
- Kimi：`[]string{"-p", req.Prompt, "--output-format", "stream-json"}`
- Cursor：无 `--workspace`
- Go `MaxDeadlineMS` 无 agentHub 特例
- 工作台无四卡、无添加文件、无 `lunitide:agent-hub-scene`

关键现网站点：

```13:13:internal/agenthub/scan.go
func ScanWorkDir(workDir string, eventPaths []string) []Artifact {
```

```62:64:internal/agenthub/adapter.go
func (kimiAdapter) BuildCommand(req TaskRequest) (string, []string, []byte, error) {
	return "kimi", []string{"-p", req.Prompt, "--output-format", "stream-json"}, nil, nil
}
```

```48:50:internal/agenthub/adapter.go
func (cursorAdapter) BuildCommand(req TaskRequest) (string, []string, []byte, error) {
	return "cursor-agent", []string{"-p", "--force", "--trust", "--output-format", "stream-json"}, []byte(req.Prompt), nil
}
```

```34:34:migrations/0153_agent_hub.sql
  source TEXT NOT NULL CHECK (source IN ('event','scan','outside')),
```

```43:45:internal/bridge/deadline.go
	default:
		return DefaultMaxDeadlineMS
	}
```

```472:490:internal/agenthub/service.go
func peekWorkDir(workDir string) []Artifact {
	// 只 ReadDir 顶层；.agenthub-inbox 内的入参运行中不可见
}
```

### 1.4 对照矩阵（需求 × PRD × 代码）

| 能力 | 需求 | V1.3.2 文档 | V1.2 代码 | V1.3.3 |
|---|---|---|---|---|
| 做 PPT → Kimi | 要 | 有 | 无卡；可手选 Kimi 自由跑 | Task 4+6 |
| 写新项目 → Cursor + 根 | 要 | 有 | 无卡；Cursor 无 `--workspace` | Task 5+6 |
| 改代码 → Codex + 根 | 要 | 有 | 无卡；有 `--ignore-user-config` | Task 6（适配器已够） |
| 其它任务任选三家 | 要 | 有 | **现网就是这个**，必须保住 | Task 6 改走第四张卡，不删测试 |
| 工作台传文件 | 要 | 有 | **无** | Task 2+3+6 |
| 原件不被改 | 要 | 有 | — | Task 2 |
| 详情看见 inbox | 要 | 写了结束扫描 | peek 看不见子目录 | Task 1+7 |
| 跳过 node_modules | 要 | 有 | **会扫进去** | Task 1 |
| 选目录对话框 | 要 | 修 600s | 前端 600s / Go 30s **互斥** | Task 3 |
| 页内 PPTX / office.generate | 不要 | 不要 | 未焊（正确） | 继续不要 |

---

## 2. V1.3.3 相对 V1.3.2 的锁定修正

### 2.1 source 三层（D1）— 必须这样，否则任务结束 Upsert 失败

```
ScanWorkDir / peekWorkDir     →  内存 Source = event|outside|inbox|changed|scan
persistableSource             →  入库 Source = event|outside|scan
                                 inbox、changed 压成 scan
GetTask / Artifacts           →  rematerializeSource
                                 路径落在 .agenthub-inbox → inbox
                                 原 stored=event 且非 inbox → event
                                 stored=outside → outside
                                 否则若 startedAt 非零且 mtime > startedAt-2s → changed
                                 否则 scan
```

禁止：往 SQLite 写 `inbox` 或 `changed`。  
禁止：为扩 CHECK 加 0154 / 改 0153 / 改 `expectedSchemaSQL`。

`MemoryStore` 不执行 CHECK，单测仍以 **API 看见的 Source** 为准（GetTask 还原后）。另写 `TestPersistableSourceMapsInboxAndChanged` 锁映射函数。

### 2.2 运行中 peek 必须看见 inbox（D2）

`peekWorkDir`：

1. 保持现网：列出工作目录**顶层普通文件**（跳过 `.agenthub-prompt.txt`）。
2. 追加：若存在 `<workDir>/.agenthub-inbox/`，再 `ReadDir` 该目录（含一层子目录即可覆盖「资料夹/相对结构」；实现用 Walk 但 **只走 inbox 树**，跳过与扫描相同的依赖目录名）。
3. inbox 树内文件 `Source=inbox`。
4. `GetTask` 在 merge 之后调用 `decorateArtifacts(task.WorkDir, task.StartedAt, arts)`。

这样 F7 在 running 与 success 都成立，且不引入 fsnotify。

### 2.3 其它锁定（与 V1.3.2 相同，写死避免再争）

- Inbox 目录名：`.agenthub-inbox`。扫描**不**跳过它。跳过文件：`.agenthub-prompt.txt`。
- 捷径必选目录才能执行；自由未选目录且 inbox 空 → 引擎 `allocateDir`。
- 自由因添加文件已分配目录 → `task.start` **必须**带该 `workDir`。
- 捷径无目录点「添加文件」→ 先 `agentHub.dir.pick`；取消则不调 inbox。
- 自由无目录点「添加文件」→ `inbox action=files` 且 `workDir=""`。
- 对话框取消：`canceled=true`；前端不改 chip / 不改 workDir。
- 超时对用户隐藏，默认 60 分钟。
- 捷径 Codex 沙箱隐藏，固定 `workspace-write`。
- 第一次打开不预选卡（即使 localStorage 有上次 scene）。点卡后再带出该 scene 的目录。
- Inbox 列表不进 localStorage。`workDir` 变化时 `action=list`。
- 不移动原件；复制用读源写目的，不 `Rename`。
- 重名：`纪要 (2).pdf`。
- 封顶：20 个 / 100MB / 200MB；超限进 `skipped` 中文。
- 资料夹：保持 `.agenthub-inbox/<源夹名>/相对路径`；**不**把源夹设成 workDir。
- `drop` 的 `name` 必须落在 inbox 内，否则「路径不受支持」。
- 后端不读 scene。自由 prompt 不含 `【场景：`。有 inbox 时四种模式都追加入参段。

---

## 3. 产品规格（执行时以本节为准）

### 3.1 工作台

```
工作台
├── 说明：常用三件事一键开始；其它事点「其它任务」自己选 Agent
├── 四张卡（互斥，第一次打开都不选）
│     做 PPT        → mode=ppt   agent=kimi    目录必选
│     写新项目      → mode=write agent=cursor  目录必选
│     改现有代码    → mode=fix   agent=codex   目录必选
│     其它任务      → mode=free  agent=用户选  目录可选
├── mode=free 才显示：三枚 Agent 胶囊（灯 + 版本）
├── 工作目录 chip
│     捷径未选目录：执行禁用
│     自由未选目录且 inbox 为空：显示「默认目录 agent-hub」，仍可执行
│     [选择文件夹]  [恢复默认]（仅自由）
├── 传给 Agent 的文件
│     [添加文件]  [添加资料夹]   （未选卡则禁用）
│     已添加 chip（文件名 + 大小；× 移除副本）
│     一行说明：原文件不动。Agent 读/改的是工作目录里的副本。
├── 任务说明
├── mode=free 且 agent=codex：沙箱三档（默认 workspace-write）
├── mode=free：快捷句（写文档 / 总结本目录 / 做小游戏 / 写周报 Markdown）
├── [执行]
└── 进行中任务
```

localStorage：

- `lunitide:agent-hub-scene` = 上次点过的卡（只写不预选）
- `lunitide:agent-hub-workdir:ppt|write|fix|free` 分场景记目录
- **删除**对旧键 `lunitide:agent-hub-workdir` 的依赖（Task 6 测试 afterEach 同时清新旧键）

卡文案：

| 卡 | 标题 | 副文案 |
|---|---|---|
| ppt | 做 PPT | 固定交给 Kimi，用它自己的技能做演示文稿 |
| write | 写新项目 | 先选项目根，固定交给 Cursor 建目录并写代码 |
| fix | 改现有代码 | 先选仓库根，固定交给 Codex 阅读并修改 |
| free | 其它任务 | 自己选 Codex / Cursor / Kimi：写文档、总结资料、做小游戏… |

选取规则：

1. 第一次打开：不预选卡，执行与添加文件禁用。
2. 点捷径：锁 Agent，隐藏胶囊/沙箱/自由快捷句，占位换成该场景，带出该场景上次目录；目录存在则 `list` inbox。
3. 点「其它任务」：显示胶囊，默认选**第一个 available** 的 Agent；目录沿用 `…:free` 或空。
4. 捷径对应 CLI 不可用：卡灰，Hint，不能选中。
5. 自由某胶囊不可用：点胶囊只弹 Hint，不切换。
6. 超时隐藏，默认 60。
7. 捷径 Codex 沙箱隐藏，固定 `workspace-write`。

占位：

| 模式 | 占位 |
|---|---|
| ppt | 根据本目录大纲做 12 页介绍 PPT，输出 pptx |
| write | 在本目录按规则建文件夹并写最小可运行代码 |
| fix | 说明缺陷，只改必要文件 |
| free | 写下要做的事。材料用上面的「添加文件」 |

自由快捷句（只填文本框，不锁 Agent）：

| 按钮 | prompt |
|---|---|
| 写文档 | 根据本工作目录已有材料写一份 Markdown 说明，只在本目录保存。 |
| 总结本目录 | 阅读本工作目录里的资料（含 .agenthub-inbox 与 pdf/ppt/md/txt），写一份结构化总结 Markdown，只在本目录保存。 |
| 做小游戏 | 在本工作目录做一个可运行的小游戏（说明怎么运行），只在本目录写文件。 |
| 写周报 Markdown | 根据本工作目录材料写一份周报 Markdown，不要生成 Office 文档。 |

目录下文案：

- 任何已选卡：`添加文件后，Agent 会在本目录的 .agenthub-inbox 里读副本。原文件不会被改。`
- 捷径写/改另加：`项目根用「选择文件夹」。规则放在根目录（AGENTS.md、.cursor/rules、README）或写在下面。`
- 自由且未选目录且 inbox 空：`不选目录就执行时，会用默认 agent-hub 目录。要用已有项目请先选文件夹。`

### 3.2 传文件

**添加文件**

1. 已选四卡之一。
2. 捷径且无 workDir：先 `agentHub.dir.pick`。取消 → 结束，不调 inbox。
3. 自由且无 workDir：`agentHub.inbox` `action=files` `workDir=""`。
4. 已有 workDir：`action=files` + 该目录。
5. 本机多选，不过滤扩展名。取消 → `canceled=true`。
6. 引擎复制到 `<workDir>/.agenthub-inbox/<文件名>`。重名 `a (2).txt`。
7. 前端以返回的 `files` 整表替换 chip。
8. `skipped` 页内错误列出，不 `alert`。

**添加资料夹：** 同上，`action=folder`。跳过依赖目录名；只收普通文件；保持 `.agenthub-inbox/<源夹名>/相对路径`；同一套封顶；不把源夹设成 workDir。

**移除：** `action=drop` + `workDir` + `name`（inbox 内相对路径）。只删副本。

**换目录 / 自由恢复默认：** 重新 `list`。旧 inbox 不搬。

**打开副本：** 任务未开始 chip 只展示名字。任务开始后走现网 `file.preview` / `file.open`，路径如 `.agenthub-inbox/纪要.pdf`（`ResolveFile` 已接受相对路径）。

封顶与拒绝见 V1.3.2 §3.1；0 个成功且 0 个 skipped 且未取消 → 视为取消。

### 3.3 前端拼 prompt

`scenePrefix` / `inboxPrefix` 见 Task 6 原文。自由不加 `【场景：`。有 chip 时四种模式都追加入参段。`timeoutMin: 60`。

### 3.4 后端适配器

**Kimi（所有 Kimi 任务，不论场景）：**

```
写 workDir/.agenthub-prompt.txt = req.Prompt（UTF-8 无 BOM）
kimi -p <短指令> --output-format stream-json [--skills-dir DIR]...
Dir=workDir
stdin 空
短指令固定：
请阅读并执行本目录 .agenthub-prompt.txt。只在本目录创建或修改文件。
```

`--skills-dir` 仅当该根下存在 `kimi-slides/SKILL.md`：

1. `%APPDATA%\kimi-desktop\daimon-share\daimon\skills`
2. `%USERPROFILE%\.kimi-code\skills`
3. `KIMI_SKILLS_DIR`

找不到仍执行。`execute` 里若 kimi 且 argv 无 `--skills-dir`，可记一条 started 之后的事件：「未找到 kimi-slides 技能目录」。不代配 MCP、不碰密钥。`WorkDir` 必须已是 `resolve` 后的绝对路径。

**Cursor（所有 Cursor 任务）：**

```
cursor-agent -p --force --trust --workspace <workDir> --output-format stream-json
Dir=workDir
stdin=req.Prompt
```

**Codex：** 保持现网。禁止去掉 `--ignore-user-config`。禁止后端再拼「改现有代码」前缀。

### 3.5 扫描跳过目录

`node_modules` `.git` `.hg` `.svn` `dist` `build` `out` `coverage` `.venv` `venv` `__pycache__` `.cursor` `.kimi-code` `.codex` `vendor` `.idea` `.vs`  
比较用 `strings.EqualFold`。不跳过 `.agenthub-inbox`。

### 3.6 详情 / 产物中心过滤

默认：`event|changed|inbox`。开关「显示目录内其它文件」再含 `scan`。永远不展示 `outside`。  
实现放 `visibleHubArtifacts`（`agentHubCopy.ts`），详情和产物中心共用。

### 3.7 新 Bridge：`agentHub.inbox`

唯一新增方法。`x-owner: engine`，`x-enabled: true`。不进 `dataScopedMethod`。

payload：`action` 必填 `files|folder|list|drop`；`workDir` 最长 1024；`name` 仅 drop 必填，最长 512。

result：`canceled`、`workDir`、`files`（最多 200，字段 `name`/`path`/`size`）、`skipped`（最多 20 条中文）。`list`/`drop` 的 `workDir` 必须是已存在的绝对路径。

截止：Go + 前端对 `agentHub.inbox` **和** `agentHub.dir.pick` 均为 **600_000**。

### 3.8 错误

| 情况 | 行为 |
|---|---|
| 未选卡 | 执行与添加文件禁用 |
| 捷径未选目录 | 执行禁用；添加文件先选目录 |
| 自由未选 Agent | 执行禁用 |
| 自由未选目录且未添加文件 | 允许执行，默认 agent-hub |
| 对话框取消 | `canceled=true`，不报错 |
| 超限 | skipped 中文；已拷保留 |
| drop 逃出 inbox | 「路径不受支持」 |
| 选盘符根/系统目录 | 「工作目录不受支持」 |
| 同 Agent 并行 | 排队；不同 Agent 可并行 |

### 3.9 非目标 / 红线

不嵌 GUI；不接 API Key；不动 `token_ledger`；不改办公主链；不加 fsnotify；不加 scene 列；不页内渲染 PPTX；不中途问答；不把 inbox 做成附件表；不做拖拽/粘贴/云盘/会话附件；不移动用户原文件；不复用 people/desktop/attachment；除 `agentHub.inbox` 外不加新 Bridge；不加 0154。

---

## 4. 文件地图

**新建**

- `api/bridge/v1/agentHub.inbox.schema.json`
- `internal/agenthub/inbox.go`
- `internal/agenthub/inbox_test.go`
- `internal/agenthub/pick_files_windows.go`
- `internal/agenthub/pick_files_other.go`
- `internal/agenthub/skills.go`
- `internal/agenthub/skills_test.go`

**改**

- `api/bridge/v1/envelope.schema.json`
- `web/scripts/generate-bridge.mjs`
- 生成物（只通过 generate:bridge）
- `internal/bridge/deadline.go`、`deadline_test.go`
- `web/src/bridge/client.ts`、`web/src/bridge/agentHub.deadline.test.ts`
- `internal/app/handlers_registry.go`、`agenthub_handlers.go`、`agenthub_handlers_test.go`
- `internal/agenthub/scan.go`、`scan_test.go`、`service.go`、`adapter.go`
- `internal/agenthub/kimi_test.go`、`cursor_test.go`、`codex_test.go`
- `web/src/agentHub/agentHubApi.ts`、`agentHubCopy.ts`、`agentHubCopy.test.ts`
- `web/src/agentHub/AgentHubWorkbench.tsx`、`agentHub.css`、`AgentHubPage.test.tsx`
- `web/src/agentHub/AgentHubDetail.tsx`、`AgentHubDetail.test.tsx`
- `web/src/agentHub/AgentHubArtifacts.tsx`

**不改**

- `0153` / 不加 `0154`、Office/Session 大页、token_ledger、`expectedSchemaSQL`、`desktop.files.*`、`people.file.*`、`attachment.*`。

---

## Task 1: 扫描跳过依赖 + changed + inbox + 入库映射 + peek inbox

**Files:** `internal/agenthub/scan.go`、`scan_test.go`、`service.go`

**接口：**

```go
const inboxDirName = ".agenthub-inbox"

func ScanWorkDir(workDir string, eventPaths []string, startedAt time.Time) []Artifact
func persistableSource(source string) string
func rematerializeSource(workDir, path, stored string, startedAt time.Time) string
func decorateArtifacts(workDir, startedAt string, arts []Artifact) []Artifact
func parseRFC3339(value string) time.Time // 空或解析失败 → 零值
```

`startedAt` 零值：不标 `changed`。路径相对 `workDir` 第一段是 `inboxDirName` → 内存 `Source=inbox`，优先于 changed/scan；**不**覆盖已经是 `event`/`outside` 的项。

### 步骤

- [ ] 把现有 `TestScanWorkDirCollectsInsideAndOutside` 的调用改成 `ScanWorkDir(root, []string{"hello.txt", outside}, time.Time{})`。
- [ ] 在 `scan_test.go` **追加**（先红）：

```go
func TestScanSkipsNodeModules(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "x.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, nil, time.Time{})
	for _, art := range arts {
		if strings.Contains(filepath.ToSlash(art.Path), "node_modules") {
			t.Fatalf("leaked %s", art.Path)
		}
	}
}

func TestScanMarksRecentAsChanged(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old.txt")
	if err := os.WriteFile(old, []byte("o"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, nil, started)
	got := map[string]string{}
	for _, art := range arts {
		got[art.Name] = art.Source
	}
	if got["hello.txt"] != "changed" {
		t.Fatalf("%v", got)
	}
	if got["old.txt"] != "scan" {
		t.Fatalf("old should stay scan: %v", got)
	}
}

func TestScanMarksInboxSource(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, inboxDirName)
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	target := filepath.Join(inbox, "note.pdf")
	if err := os.WriteFile(target, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, past, past); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, nil, time.Now())
	for _, art := range arts {
		if art.Name == "note.pdf" && art.Source == "inbox" {
			return
		}
	}
	t.Fatalf("%+v", arts)
}

func TestPersistableSourceMapsInboxAndChanged(t *testing.T) {
	if persistableSource("inbox") != "scan" || persistableSource("changed") != "scan" {
		t.Fatal("inbox/changed must persist as scan")
	}
	if persistableSource("event") != "event" || persistableSource("outside") != "outside" || persistableSource("scan") != "scan" {
		t.Fatal("legacy sources stay")
	}
}

func TestRematerializeInboxFromPersistedScan(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, inboxDirName, "note.pdf")
	if rematerializeSource(root, path, "scan", time.Time{}) != "inbox" {
		t.Fatal("inbox path must come back")
	}
	if rematerializeSource(root, filepath.Join(root, "old.txt"), "event", time.Time{}) != "event" {
		t.Fatal("event stays")
	}
}

func TestPeekWorkDirListsInbox(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, inboxDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, inboxDirName, "note.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	arts := peekWorkDir(root)
	for _, art := range arts {
		if art.Name == "note.txt" && art.Source == "inbox" {
			return
		}
	}
	t.Fatalf("%+v", arts)
}
```

- [ ] `go test ./internal/agenthub/ -count=1 -run "TestScanSkipsNodeModules|TestScanMarksRecentAsChanged|TestScanMarksInboxSource|TestPersistableSourceMapsInboxAndChanged|TestRematerializeInboxFromPersistedScan|TestPeekWorkDirListsInbox"` — 必须红。
- [ ] 改 `scan.go`：
  - `skipScanDir(name)`：名称 EqualFold 属于 §3.5 列表则 true。
  - `WalkDir`：若 `d.IsDir()` 且 `path != workDir` 且 `skipScanDir(d.Name())` → `fs.SkipDir`。**不要**跳过 `.agenthub-inbox`。
  - 符号链接目录：`d.Type()&os.ModeSymlink != 0` 且是目录 → `fs.SkipDir`（不跟随 junction 递归）。
  - 非事件文件：`inboxRel(workDir, abs)` → `inbox`；否则若 `!startedAt.IsZero()` 且 `info.ModTime().After(startedAt.Add(-2*time.Second))` → `changed`；否则 `scan`。
  - 实现 `persistableSource` / `rematerializeSource` / `inboxRel` / `decorateArtifacts` / `parseRFC3339`（可放 `scan.go` 或 `service.go`，名称必须一致）。
- [ ] `service.go`：
  - 两处 `ScanWorkDir(...)` 改为 `ScanWorkDir(dir, paths, parseRFC3339(task.StartedAt))`。Recover 那处传入 `parseRFC3339(task.StartedAt)`（重启失败任务仍能标 changed）。
  - Upsert 前：`art.Source = persistableSource(art.Source)`。
  - `peekWorkDir`：顶层文件保持 `scan`；再 Walk `filepath.Join(workDir, inboxDirName)`，文件标 `inbox`，跳过 prompt 文件与 skip 目录。
  - `GetTask`：merge 之后 `arts = decorateArtifacts(task.WorkDir, task.StartedAt, arts)`。
  - `Artifacts`：对每条 `art.Source = rematerializeSource("", art.Path, art.Source, time.Time{})`（无 workDir 时 `inboxRel` 用路径是否含 `/.agenthub-inbox/` 或前缀 `.agenthub-inbox/`）。
- [ ] `go test ./internal/agenthub/ -count=1` 全绿（含现网 `TestGetTaskListsLiveFilesWhileRunning`）。

---

## Task 2: Inbox 复制 / 列出 / 删除（无对话框）

**Files:** `inbox.go`、`inbox_test.go`；`service.go` 增加字段：

```go
PickFiles  func() ([]string, error)
PickFolder func() (string, error)
```

```go
type InboxFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

const (
	maxIngestFiles     = 20
	maxIngestFileBytes int64 = 104857600
	maxIngestTotalBytes int64 = 209715200
)

func (s *Service) Inbox(action, workDir, name string) (canceled bool, dir string, files []InboxFile, skipped []string, err error)
```

`PickFiles`/`PickFolder` 为空时走 Task 8 的 OS 函数；本 Task 单测必须注入，不弹窗。

行为：

- `files`/`folder`：`resolveWorkDir(workDir)`（空则 `allocateDir`）→ 对话框 → `os.MkdirAll(inbox)` → `io.Copy` → `listInbox`。取消（`errors.Is(err, ErrPickCanceled)` 或空选择）→ `canceled=true`，`files=[]`。前端取消时不改 chip，所以取消后即使已 allocate 也可以返回该 dir；**前端以 `canceled` 为准忽略**。
- `list`：`workDir` 必须绝对且通过 `forbiddenWorkDir`；inbox 不存在则空列表。
- `drop`：`safeInboxPath(workDir, name)` 后 `os.Remove`；`insideDir(filepath.Join(workDir, inboxDirName), abs)` 失败 → `fmt.Errorf("路径不受支持")`。
- 重名：`base (n).ext`（`n` 从 2 起）。
- 单文件 > 100MB / 累计 > 200MB / 超过 20 个 → 该条进 skipped，中文：`<name> 超过 100MB` / `合计超过 200MB` / `超过 20 个文件`。
- folder：Walk 源夹，`skipScanDir` 的目录 `fs.SkipDir`；只收普通文件；目的 `filepath.Join(inbox, filepath.Base(srcFolder), rel)`；逃出 inbox 的解析结果跳过。
- 不 Copy `.agenthub-prompt.txt`。
- 不 `Rename`。不跟随 symlink 目录。

### 步骤

- [ ] 写 `inbox_test.go`（先红）。必须包含下列测试，不要漏 Allocates / SkipsTooLarge：

```go
func TestInboxCopiesRegularFileLeavesSource(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "note.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	canceled, dir, files, skipped, err := s.Inbox("files", "", "")
	if err != nil || canceled || len(skipped) != 0 || len(files) != 1 {
		t.Fatalf("%v %v %v %v %v", canceled, dir, files, skipped, err)
	}
	body, err := os.ReadFile(src)
	if err != nil || string(body) != "hello" {
		t.Fatalf("source mutated: %q %v", body, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, inboxDirName, "note.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("copy: %q %v", got, err)
	}
}

func TestInboxRenamesCollision(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(src, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, dir, _, _, err := s.Inbox("files", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, files, _, err := s.Inbox("files", dir, "")
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %v", files, err)
	}
	names := files[0].Path + "," + files[1].Path
	if !strings.Contains(names, "a.txt") || !strings.Contains(names, "a (2).txt") {
		t.Fatal(names)
	}
}

func TestInboxSkipsTooLarge(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "big.bin")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxIngestFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, _, files, skipped, err := s.Inbox("files", "", "")
	if err != nil || len(files) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0], "100MB") {
		t.Fatalf("%v %v %v", files, skipped, err)
	}
}

func TestInboxFolderSkipsNodeModules(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "node_modules", "x.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "keep.md"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFolder = func() (string, error) { return src, nil }
	_, _, files, _, err := s.Inbox("folder", "", "")
	if err != nil || len(files) != 1 || files[0].Name != "keep.md" {
		t.Fatalf("%v %v", files, err)
	}
}

func TestInboxDropOnlyInsideInbox(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, dir, _, _, err := s.Inbox("files", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := s.Inbox("drop", dir, "../note.txt"); err == nil {
		t.Fatal("expected escape to fail")
	}
	_, _, files, _, err := s.Inbox("drop", dir, "note.txt")
	if err != nil || len(files) != 0 {
		t.Fatalf("%v %v", files, err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source must remain")
	}
}

func TestInboxAllocatesWorkDirWhenEmpty(t *testing.T) {
	root := t.TempDir()
	s := New(NewMemoryStore(), root, nil)
	src := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, dir, files, _, err := s.Inbox("files", "", "")
	if err != nil || len(files) != 1 || !strings.HasPrefix(filepath.Clean(dir), filepath.Clean(root)) {
		t.Fatalf("%s %v %v", dir, files, err)
	}
}

func TestInboxCancelEmptySelection(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	s.PickFiles = func() ([]string, error) { return nil, ErrPickCanceled }
	canceled, _, files, _, err := s.Inbox("files", "", "")
	if err != nil || !canceled || len(files) != 0 {
		t.Fatalf("%v %v %v", canceled, files, err)
	}
}
```

- [ ] `go test ./internal/agenthub/ -count=1 -run TestInbox` — 必须红。
- [ ] 实现 `Inbox`。`listInbox` 返回相对 inbox 的正斜杠 `Path`，`Name` 用展示名（可含子目录）。
- [ ] `go test ./internal/agenthub/ -count=1 -run TestInbox` 绿。
- [ ] `go test ./internal/agenthub/ -count=1` 全绿。

---

## Task 3: `agentHub.inbox` 契约 + handler + 10 分钟截止

**Files:** schema、envelope、generate-bridge.mjs、handlers、client、deadline。

### Schema 原文（新建 `api/bridge/v1/agentHub.inbox.schema.json`，LF）

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lunitide.local/schema/bridge/v1/agentHub.inbox.schema.json",
  "title": "agentHub.inbox payload",
  "x-method": "agentHub.inbox",
  "x-owner": "engine",
  "x-enabled": true,
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "action": { "type": "string", "enum": ["files", "folder", "list", "drop"] },
    "workDir": { "type": "string", "maxLength": 1024 },
    "name": { "type": "string", "maxLength": 512 }
  },
  "required": ["action"],
  "x-result": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "canceled": { "type": "boolean" },
      "workDir": { "type": "string", "maxLength": 1024 },
      "files": {
        "type": "array",
        "maxItems": 200,
        "items": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "name": { "type": "string", "minLength": 1, "maxLength": 256 },
            "path": { "type": "string", "minLength": 1, "maxLength": 1024 },
            "size": { "type": "integer", "minimum": 0 }
          },
          "required": ["name", "path", "size"]
        }
      },
      "skipped": {
        "type": "array",
        "maxItems": 20,
        "items": { "type": "string", "minLength": 1, "maxLength": 256 }
      }
    },
    "required": ["canceled", "workDir", "files"]
  },
  "x-examples": {
    "positive": [
      { "action": "list", "workDir": "D:/work" },
      { "action": "files" },
      { "action": "drop", "workDir": "D:/work", "name": "note.txt" }
    ],
    "negative": [{}, { "action": "upload" }, { "extra": true }]
  }
}
```

### 步骤

- [ ] 写入上述 schema（LF）。
- [ ] `envelope.schema.json` 方法枚举在 `agentHub.file.preview` 后插入 `"agentHub.inbox",`（字母序：preview 与 task.cancel 之间）。
- [ ] `generate-bridge.mjs` enabled 断言数组在 `'agentHub.file.preview',` 后插入 `'agentHub.inbox',`。
- [ ] `npm --prefix web run generate:bridge`（需要完整权限）。生成后确认出现 `MethodAgentHubInbox Method = "agentHub.inbox"`。**禁止手改生成物。**
- [ ] `internal/bridge/deadline.go`：

```go
AgentHubPickDeadlineMS = 600_000
```

`MaxDeadlineMS` 增加：

```go
case MethodAgentHubDirPick, MethodAgentHubInbox:
	return AgentHubPickDeadlineMS
```

若生成器尚未跑完，先生成再改 deadline（常量名以生成物为准：`MethodAgentHubInbox`）。

- [ ] `deadline_test.go` 追加（可并进现有测试函数或新建 `TestMaxDeadlineMSAgentHubPickers`）：

```go
func TestMaxDeadlineMSAgentHubPickers(t *testing.T) {
	if MaxDeadlineMS("agentHub.dir.pick") != 600_000 {
		t.Fatalf("dir.pick cap = %d", MaxDeadlineMS("agentHub.dir.pick"))
	}
	if MaxDeadlineMS("agentHub.inbox") != 600_000 {
		t.Fatalf("inbox cap = %d", MaxDeadlineMS("agentHub.inbox"))
	}
	if MaxDeadlineMS("agentHub.detect") != DefaultMaxDeadlineMS {
		t.Fatalf("detect must stay 30s: %d", MaxDeadlineMS("agentHub.detect"))
	}
}
```

- [ ] `client.ts`：
  - `capBridgeDeadlineMs`：`method === 'agentHub.dir.pick' || method === 'agentHub.inbox'` → `AGENT_HUB_DIR_PICK_MS`。
  - `createAgentHubBridge`：`method === 'agentHub.dir.pick' || method === 'agentHub.inbox' ? AGENT_HUB_DIR_PICK_MS : deadlineMs`。
- [ ] `agentHub.deadline.test.ts` 增加 inbox 断言：

```ts
it('lets agentHub.dir.pick and agentHub.inbox wait for native dialogs', () => {
  expect(capBridgeDeadlineMs('agentHub.dir.pick', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
  expect(capBridgeDeadlineMs('agentHub.inbox', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
  expect(capBridgeDeadlineMs('agentHub.detect', AGENT_HUB_DIR_PICK_MS)).toBe(30_000)
})
```

可删掉旧的只测 dir.pick 的用例，或改名保留一条，不要两条互相打架。

- [ ] `handlers_registry.go` 增加 `bridge.Method("agentHub.inbox"): handleAgentHub,`。
- [ ] `handleAgentHub` 增加：

```go
case "agentHub.inbox":
	var p struct {
		Action  string `json:"action"`
		WorkDir string `json:"workDir"`
		Name    string `json:"name"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.Action != "files" && p.Action != "folder" && p.Action != "list" && p.Action != "drop") {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.inbox 参数无效", false)
	}
	canceled, dir, files, skipped, err := e.agentHub.Inbox(p.Action, p.WorkDir, p.Name)
	if errors.Is(err, agenthub.ErrPickCanceled) {
		canceled, err = true, nil
		files = []agenthub.InboxFile{}
	}
	if err != nil {
		return agentHubFailure(r, err)
	}
	if files == nil {
		files = []agenthub.InboxFile{}
	}
	payload := map[string]any{"canceled": canceled, "workDir": dir, "files": files}
	if len(skipped) > 0 {
		payload["skipped"] = skipped
	}
	return r.Ok(payload)
```

`agentHubFailure` 对「路径不受支持」「工作目录不受支持」应走 default 分支（现网已按中文关键字放行）。不要把它们映射成 PATH_OUTSIDE，除非 `errors.Is(ErrPathOutside)`。

- [ ] `agenthub_handlers_test.go`：

```go
func TestAgentHubInboxCopiesWithInjectedPicker(t *testing.T) {
	s := agenthub.New(agenthub.NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	e := &Engine{agentHub: s}
	resp := handleAgentHub(e, context.Background(), validRequest("agentHub.inbox", `{"action":"files"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw := string(hubJSON(resp.Payload))
	if !strings.Contains(raw, `"canceled":false`) || !strings.Contains(raw, "note.txt") {
		t.Fatalf("%s", raw)
	}
}
```

并把 `"agentHub.inbox"` 加入 `TestAgentHubMethodsAreNotDataScoped` 的切片。

- [ ] `go test ./internal/bridge/ ./internal/app/ ./internal/agenthub/ -count=1`
- [ ] 在 `web/`：`npx vitest run src/bridge/agentHub.deadline.test.ts`

---

## Task 4: Kimi 短 argv + prompt 文件 + skills-dir

**Files:** `skills.go`、`skills_test.go`、`adapter.go`、`kimi_test.go`；可选 `service.go` 记一条「未找到技能目录」。

短指令常量（必须一字不差）：

```go
const kimiPromptArg = "请阅读并执行本目录 .agenthub-prompt.txt。只在本目录创建或修改文件。"
```

### 步骤

- [ ] **改掉**现网 `TestKimiBuildCommand`（它现在断言 argv 含「写 hello.txt」）。新原文：

```go
func TestKimiBuildCommand(t *testing.T) {
	dir := t.TempDir()
	exe, args, stdin, err := (kimiAdapter{}).BuildCommand(TaskRequest{Prompt: "写 hello.txt", WorkDir: dir})
	if err != nil || exe != "kimi" || len(stdin) != 0 {
		t.Fatalf("%s %v %q %v", exe, args, stdin, err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "写 hello.txt") {
		t.Fatalf("long prompt must not sit on argv: %v", args)
	}
	if !strings.Contains(joined, "-p") || !strings.Contains(joined, "stream-json") {
		t.Fatalf("%v", args)
	}
	body, err := os.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil || !strings.Contains(string(body), "写 hello.txt") {
		t.Fatalf("%q %v", body, err)
	}
}

func TestKimiBuildCommandRequiresWorkDir(t *testing.T) {
	_, _, _, err := (kimiAdapter{}).BuildCommand(TaskRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("expected work dir error")
	}
}
```

- [ ] `skills_test.go`：

```go
func TestKimiSkillDirsFindsSlides(t *testing.T) {
	root := t.TempDir()
	slides := filepath.Join(root, "kimi-slides")
	if err := os.MkdirAll(slides, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slides, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := filterKimiSkillRoots([]string{root, filepath.Join(root, "missing")})
	if len(got) != 1 || got[0] != root {
		t.Fatalf("%v", got)
	}
}
```

- [ ] `go test ./internal/agenthub/ -count=1 -run "TestKimiBuildCommand|TestKimiSkillDirsFindsSlides"` 先红。
- [ ] `skills.go`：

```go
func filterKimiSkillRoots(candidates []string) []string {
	var out []string
	for _, root := range candidates {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "kimi-slides", "SKILL.md")); err == nil {
			out = append(out, root)
		}
	}
	return out
}

func kimiSkillDirs() []string {
	return filterKimiSkillRoots([]string{
		filepath.Join(os.Getenv("APPDATA"), "kimi-desktop", "daimon-share", "daimon", "skills"),
		filepath.Join(os.Getenv("USERPROFILE"), ".kimi-code", "skills"),
		os.Getenv("KIMI_SKILLS_DIR"),
	})
}
```

- [ ] `kimiAdapter.BuildCommand`：`WorkDir` 空 → `fmt.Errorf("工作目录无效")`；`os.WriteFile(filepath.Join(req.WorkDir, promptFileName), []byte(req.Prompt), 0o644)`；argv `-p` + `kimiPromptArg` + `--output-format` + `stream-json`；每个 skill 根追加 `--skills-dir`、dir。`adapter.go` 增加 `"os"` import。
- [ ] （可选但推荐）`execute` 在 BuildCommand 成功后：若 `req.Agent=="kimi"` 且 args 不含 `--skills-dir`，`appendEvent` 一条 `Type=message` `Title=未找到 kimi-slides 技能目录`。
- [ ] `go test ./internal/agenthub/ -count=1 -run "TestKimi|TestCursorBuildCommand|TestCodexBuildCommand"` 绿。

---

## Task 5: Cursor `--workspace`

**Files:** `adapter.go`、`cursor_test.go`

```go
func hasPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
```

### 步骤

- [ ] `TestCursorBuildCommand` 增加 `hasPair(args, "--workspace", `D:\work`)`，先红。现网其它断言保留（stdin=`hi`，含 `-p` `--force` `--trust` `stream-json`）。
- [ ] `BuildCommand`：

```go
return "cursor-agent", []string{"-p", "--force", "--trust", "--workspace", req.WorkDir, "--output-format", "stream-json"}, []byte(req.Prompt), nil
```

- [ ] 绿。`TestCodexBuildCommand` 必须仍是 `string(stdin)=="hi"` 且 argv 含 `--ignore-user-config`。**不要改 Codex 适配器。**

---

## Task 6: 工作台四卡 + 传文件 chips

**Files:** `agentHubCopy.ts`、`agentHubCopy.test.ts`、`agentHubApi.ts`、`AgentHubWorkbench.tsx`、`agentHub.css`、`AgentHubPage.test.tsx`

```ts
export type HubScene = 'ppt' | 'write' | 'fix' | 'free'
export type InboxFile = { name: string; path: string; size: number }

export const SCENE_KEY = 'lunitide:agent-hub-scene'
export const workDirKey = (scene: HubScene) => `lunitide:agent-hub-workdir:${scene}`

export function scenePrefix(scene: HubScene, workDir: string, userText: string): string {
  if (scene === 'free') return userText
  if (scene === 'ppt') {
    return `【场景：做 PPT】\n工作目录：${workDir}\n只在本目录写文件。优先使用 kimi-slides。产出 pptx。参考文件在本目录或 .agenthub-inbox 时先读再做。\n\n用户任务：\n${userText}`
  }
  if (scene === 'write') {
    return `【场景：写新项目】\n项目根：${workDir}\n把该路径当作唯一项目根。只在该根下创建文件夹和文件。\n先读 AGENTS.md、.cursor/rules、README（没有则跳过）。\n用户传入的需求/设计在 .agenthub-inbox（没有则跳过）。\n用户写的规则优先。做完即结束。\n\n用户任务：\n${userText}`
  }
  return `【场景：改现有代码】\n项目根：${workDir}\n只阅读和修改这个根内的文件。先看 README、AGENTS.md 和相关源码。\n用户传入的说明/截图在 .agenthub-inbox（没有则跳过）。\n改动保持最小。不要 git 提交或推远程。做完即结束。\n\n用户任务：\n${userText}`
}

export function inboxPrefix(files: InboxFile[]): string {
  if (files.length === 0) return ''
  const lines = files.map(item => `- ${item.path}`)
  return `\n\n用户传入的文件已复制到 .agenthub-inbox/（原路径未改）。请先阅读。可以修改这些副本，或在本工作目录写出新文件。不要去改用户原路径上的文件。完成后把结果留在本目录。\n${lines.join('\n')}`
}

export function composeHubPrompt(scene: HubScene, workDir: string, userText: string, files: InboxFile[]): string {
  return `${scenePrefix(scene, workDir, userText)}${inboxPrefix(files)}`
}

export const FREE_TEMPLATES = [
  { zh: '写文档', en: 'Write docs', prompt: '根据本工作目录已有材料写一份 Markdown 说明，只在本目录保存。' },
  { zh: '总结本目录', en: 'Summarize this folder', prompt: '阅读本工作目录里的资料（含 .agenthub-inbox 与 pdf/ppt/md/txt），写一份结构化总结 Markdown，只在本目录保存。' },
  { zh: '做小游戏', en: 'Make a small game', prompt: '在本工作目录做一个可运行的小游戏（说明怎么运行），只在本目录写文件。' },
  { zh: '写周报 Markdown', en: 'Write weekly-report Markdown', prompt: '根据本工作目录材料写一份周报 Markdown，不要生成 Office 文档。' },
] as const

export const HUB_SCENES = [
  { id: 'ppt' as const, agent: 'kimi' as const, zh: '做 PPT', en: 'Make a PPT', subZh: '固定交给 Kimi，用它自己的技能做演示文稿', subEn: 'Always Kimi, using its own slides skill' },
  { id: 'write' as const, agent: 'cursor' as const, zh: '写新项目', en: 'New project', subZh: '先选项目根，固定交给 Cursor 建目录并写代码', subEn: 'Pick a root, then Cursor writes the project' },
  { id: 'fix' as const, agent: 'codex' as const, zh: '改现有代码', en: 'Fix code', subZh: '先选仓库根，固定交给 Codex 阅读并修改', subEn: 'Pick a repo root, then Codex edits' },
  { id: 'free' as const, agent: '' as const, zh: '其它任务', en: 'Other task', subZh: '自己选 Codex / Cursor / Kimi：写文档、总结资料、做小游戏…', subEn: 'Pick Codex, Cursor or Kimi yourself' },
] as const

export function visibleHubArtifacts<T extends { source: string }>(items: T[], showScan: boolean): T[] {
  return items.filter(item => {
    if (item.source === 'outside') return false
    if (item.source === 'scan') return showScan
    return item.source === 'event' || item.source === 'changed' || item.source === 'inbox'
  })
}
```

`HUB_TEMPLATES`：改成 `FREE_TEMPLATES` 的别名（`export const HUB_TEMPLATES = FREE_TEMPLATES`），避免旧 import 编译失败。`agentHubCopy.test.ts` 改为断言 `FREE_TEMPLATES` 含「写周报 Markdown」且不含「生成周报」，并加：

```ts
it('prefixes shortcuts but leaves free prompts clean until inbox files exist', () => {
  expect(scenePrefix('free', 'E:/hub', '总结这些材料')).toBe('总结这些材料')
  expect(scenePrefix('ppt', 'E:/hub', '做两页')).toContain('【场景：做 PPT】')
  expect(composeHubPrompt('free', 'E:/hub', '总结这些材料', [{ name: '纪要.pdf', path: '纪要.pdf', size: 12 }])).toContain('.agenthub-inbox')
  expect(composeHubPrompt('free', 'E:/hub', '总结这些材料', [{ name: '纪要.pdf', path: '纪要.pdf', size: 12 }])).not.toContain('【场景：')
})

it('hides scan and outside artifacts until asked', () => {
  const items = [
    { source: 'inbox' },
    { source: 'changed' },
    { source: 'scan' },
    { source: 'outside' },
  ]
  expect(visibleHubArtifacts(items, false).map(item => item.source)).toEqual(['inbox', 'changed'])
  expect(visibleHubArtifacts(items, true).map(item => item.source)).toEqual(['inbox', 'changed', 'scan'])
})
```

`agentHubApi.inbox`：

```ts
inbox: (payload: { action: 'files' | 'folder' | 'list' | 'drop'; workDir?: string; name?: string }) =>
  request<{ canceled: boolean; workDir: string; files: InboxFile[]; skipped?: string[] }>('agentHub.inbox', payload),
```

`start` payload：

- 捷径：`agent` 锁死；`workDir` 必有；`prompt=composeHubPrompt(...)`；`timeoutMin: 60`；fix 时 `sandbox: 'workspace-write'`
- 自由：`agent` 为选中胶囊；`workDir` 有则传（含因添加文件分配的）；空且 files 空则 `undefined`；`prompt=composeHubPrompt('free', ...)`；`sandbox` 仅 codex；`timeoutMin: 60`

Workbench 状态：`scene: HubScene | null` 初值 `null`（不读 SCENE_KEY 预选）。点卡后 `localStorage.setItem(SCENE_KEY, scene)` 并读 `workDirKey(scene)`。

添加文件伪代码：

```ts
async function ingest(action: 'files' | 'folder') {
  if (!scene) return
  let dir = workDir
  if (scene !== 'free' && !dir) {
    const picked = await agentHubApi.pickDir()
    if (picked.canceled || !picked.path) return
    if (!window.confirm(zh ? '将在此目录执行 CLI，可能改文件。确定继续？' : 'The CLI may edit files in this folder. Continue?')) return
    dir = picked.path
    setWorkDir(dir)
    localStorage.setItem(workDirKey(scene), dir)
  }
  const got = await agentHubApi.inbox({ action, workDir: dir || undefined })
  if (got.canceled) return
  if (got.workDir) {
    setWorkDir(got.workDir)
    localStorage.setItem(workDirKey(scene), got.workDir)
  }
  setFiles(got.files ?? [])
  setError((got.skipped ?? []).join('；'))
}
```

`useEffect`：当 `workDir` 变为非空，`inbox({action:'list', workDir})` 刷新 files；变为空则 `setFiles([])`。

CSS：`.agent-hub-scene` / `.agent-hub-file` 选中用 `var(--tide1)`，不要新颜色。按钮保持现网小芯片气质，不要做回大按钮。

### 步骤

- [ ] `AgentHubPage.test.tsx` 的 mock 增加 `inbox: vi.fn()`。`afterEach` 清 `lunitide:agent-hub-scene` 与 `lunitide:agent-hub-workdir:ppt|write|fix|free` 以及旧键 `lunitide:agent-hub-workdir`。
- [ ] **重写**会因四卡而红的现网用例，**不要删自由路径**：

  1. `renders the hub shell...`：不再要求一上来就有「写周报 Markdown」和「本机沙箱不可用」。改成：看见四张卡标题；未选卡时「执行」disabled；无「生成周报」/「Pro 已登录」/「AI 军团」。点「其它任务」后才出现「写周报 Markdown」。
  2. `starts a task from an available adapter`：先点「其它任务」，再填「写周报 Markdown」，再执行。`start.agent==='codex'`（stubLists 里第一个 available 是 Codex），prompt 不含 `【场景：`。
  3. `starts a kimi...`：先点「其它任务」，再点 Kimi 胶囊，再执行。
  4. `sends a picked work directory...`：先点「其它任务」，再选目录，再执行。

- [ ] **追加**（先红）：

```tsx
it('other-task starts without a directory and without a scene prefix', async () => {
  stubLists()
  vi.mocked(agentHubApi.start).mockResolvedValue(startedFixture('codex', '写说明'))
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: /其它任务/ }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '写说明' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({
    agent: 'codex',
    prompt: expect.not.stringContaining('【场景：'),
  })))
})

it('ppt shortcut does not start without a work directory', async () => {
  stubLists()
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'codex', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
      { name: 'kimi', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' },
    ],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: /做 PPT/ }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '做两页' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  expect(agentHubApi.start).not.toHaveBeenCalled()
})

it('ppt shortcut locks kimi and prefixes the prompt when a directory is remembered', async () => {
  stubLists()
  localStorage.setItem('lunitide:agent-hub-workdir:ppt', 'E:/slides')
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'codex', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: 'x' },
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: 'x' },
      { name: 'kimi', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' },
    ],
  })
  vi.mocked(agentHubApi.inbox).mockResolvedValue({ canceled: false, workDir: 'E:/slides', files: [] })
  vi.mocked(agentHubApi.start).mockResolvedValue(startedFixture('kimi', '做两页'))
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: /做 PPT/ }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '做两页' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({
    agent: 'kimi',
    workDir: 'E:/slides',
    prompt: expect.stringContaining('【场景：做 PPT】'),
  })))
})

it('free ingest lists files in the start prompt and binds the allocated directory', async () => {
  stubLists()
  vi.mocked(agentHubApi.inbox).mockResolvedValue({
    canceled: false,
    workDir: 'E:/hub',
    files: [{ name: '纪要.pdf', path: '纪要.pdf', size: 12 }],
  })
  vi.mocked(agentHubApi.start).mockResolvedValue(startedFixture('codex', '总结'))
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: /其它任务/ }))
  fireEvent.click(screen.getByRole('button', { name: '添加文件' }))
  await waitFor(() => expect(agentHubApi.inbox).toHaveBeenCalledWith(expect.objectContaining({ action: 'files' })))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '总结这些材料' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({
    workDir: 'E:/hub',
    prompt: expect.stringMatching(/纪要\.pdf/),
  })))
  const payload = vi.mocked(agentHubApi.start).mock.calls.at(-1)?.[0] as { prompt: string }
  expect(payload.prompt).toContain('.agenthub-inbox')
  expect(payload.prompt).not.toContain('【场景：')
})

it('ppt ingest picks a directory before opening the file dialog', async () => {
  stubLists()
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'codex', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: 'x' },
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: 'x' },
      { name: 'kimi', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' },
    ],
  })
  vi.mocked(agentHubApi.pickDir).mockResolvedValue({ canceled: true, path: '' })
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: /做 PPT/ }))
  fireEvent.click(screen.getByRole('button', { name: '添加文件' }))
  await waitFor(() => expect(agentHubApi.pickDir).toHaveBeenCalled())
  expect(agentHubApi.inbox).not.toHaveBeenCalled()
})
```

`startedFixture` 就地写一个小 helper，返回现网 start mock 同形状。四张卡的 accessible name 用 `/做 PPT/` 等，因为按钮里还有副文案。

- [ ] `npx vitest run src/agentHub/AgentHubPage.test.tsx src/agentHub/agentHubCopy.test.ts`（`web/`）先红。
- [ ] 实现四卡、chips、添加文件/资料夹、× drop、`list` on workDir 变化、隐藏超时 chip。
- [ ] 同一 vitest 命令全绿。

---

## Task 7: 详情默认看见 inbox

**Files:** `AgentHubDetail.tsx`、`AgentHubDetail.test.tsx`、`AgentHubArtifacts.tsx`

默认：`visibleHubArtifacts(items, showScan)`。开关 aria-label：`显示目录内其它文件`。永不展示 `outside`。

### 步骤

- [ ] `AgentHubDetail.test.tsx` 追加：

```tsx
it('shows inbox and changed artifacts by default and hides scan', async () => {
  vi.mocked(agentHubApi.get).mockResolvedValue({
    task: {
      taskId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      agent: 'kimi',
      prompt: '总结',
      workDir: 'C:/tmp',
      sandbox: '',
      status: 'success',
      tokensUsed: 0,
      errorMsg: '',
      createdAt: '2026-09-12T00:00:00Z',
    },
    events: [],
    artifacts: [
      { name: '纪要.pdf', path: '.agenthub-inbox/纪要.pdf', size: 12, mime: 'application/pdf', source: 'inbox' },
      { name: 'hello.txt', path: 'hello.txt', size: 2, mime: 'text/plain', source: 'changed' },
      { name: 'lib.js', path: 'lib.js', size: 3, mime: 'text/plain', source: 'scan' },
      { name: 'away.txt', path: 'C:/away.txt', size: 1, mime: 'text/plain', source: 'outside' },
    ],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubDetail taskId="01ARZ3NDEKTSV4RRFFQ69G5FAE" /></LanguageProvider>)
  expect(await screen.findByText('纪要.pdf')).toBeInTheDocument()
  expect(screen.getByText('hello.txt')).toBeInTheDocument()
  expect(screen.queryByText('lib.js')).toBeNull()
  expect(screen.queryByText('away.txt')).toBeNull()
})
```

现网 400ms 轮询两条测试保持。

- [ ] 详情产物列表改走 `visibleHubArtifacts`；加 checkbox/按钮「显示目录内其它文件」。
- [ ] 产物中心同样过滤（默认隐藏 scan/outside）。产物中心不必做开关（详情开关是 F7）；若加开关也可以，默认仍隐藏。
- [ ] `npx vitest run src/agentHub`（`web/`）绿。

---

## Task 8: Windows 选文件对话框 + 回归

**Files:** `pick_files_windows.go`、`pick_files_other.go`；回归现网包测试与 livehub。

`pick_files_other.go`：

```go
//go:build !windows

package agenthub

func pickFilesOS() ([]string, error) { return nil, ErrPickCanceled }
func pickFolderOS() (string, error)  { return "", ErrPickCanceled }
```

`pick_files_windows.go`：抄 `pick_windows.go` 的隐藏 TopMost 窗体。files 用 `OpenFileDialog`，`Multiselect=true`，`Filter='所有文件|*.*'`，标题「选择要交给 Agent 的文件」，输出以换行拼接的绝对路径。folder 用 `FolderBrowserDialog`，说明「选择要交给 Agent 的资料夹」，`ShowNewFolderButton=false`。取消或空 → `ErrPickCanceled`。超时 10 分钟，与 `pick_windows.go` 一致。

`Service.Inbox`：`PickFiles`/`PickFolder` 为 nil 时分别调用 `pickFilesOS` / `pickFolderOS`。

完整 Windows 脚本（写入常量，不要现场拼）：

```go
//go:build windows

package agenthub

import (
	"context"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/winexec"
)

func pickFilesOS() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", pickFilesScript).Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrPickCanceled
		}
		return nil, err
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, ErrPickCanceled
	}
	var files []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	if len(files) == 0 {
		return nil, ErrPickCanceled
	}
	return files, nil
}

func pickFolderOS() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", pickInboxFolderScript).Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrPickCanceled
		}
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrPickCanceled
	}
	return path, nil
}

const pickFilesScript = `
Add-Type -AssemblyName System.Windows.Forms
$form = New-Object System.Windows.Forms.Form
$form.TopMost = $true
$form.ShowInTaskbar = $false
$form.StartPosition = 'Manual'
$form.Location = New-Object System.Drawing.Point(-32000, -32000)
$form.Size = New-Object System.Drawing.Size(1, 1)
$form.Show()
$form.Activate()
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Title = '选择要交给 Agent 的文件'
$d.Filter = '所有文件|*.*'
$d.Multiselect = $true
$ok = $d.ShowDialog($form) -eq 'OK'
$names = $d.FileNames
$form.Close()
if ($ok) {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $names -join [Environment]::NewLine
}
`

const pickInboxFolderScript = `
Add-Type -AssemblyName System.Windows.Forms
$form = New-Object System.Windows.Forms.Form
$form.TopMost = $true
$form.ShowInTaskbar = $false
$form.StartPosition = 'Manual'
$form.Location = New-Object System.Drawing.Point(-32000, -32000)
$form.Size = New-Object System.Drawing.Size(1, 1)
$form.Show()
$form.Activate()
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = '选择要交给 Agent 的资料夹'
$d.ShowNewFolderButton = $false
$ok = $d.ShowDialog($form) -eq 'OK'
$path = $d.SelectedPath
$form.Close()
if ($ok) {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $path
}
`
```

### 步骤

- [ ] 写入上述两个 pick 文件。`Inbox` 在注入为空时调用它们。
- [ ] `go test ./internal/agenthub/ ./internal/app/ ./internal/bridge/ -count=1`
- [ ] 在 `web/`：`npx vitest run src/agentHub src/bridge/agentHub.deadline.test.ts`
- [ ] livehub hello 三条必须仍过（现网 `//go:build livehub`）：

```
go test ./internal/agenthub/ -tags livehub -count=1 -run "TestLiveCodexDetectStartArtifactPreview|TestLiveCursorDetectStartArtifactPreview|TestLiveKimiDetectStartArtifactPreview" -timeout 20m
```

不要改这三条的 prompt 语义。自由路径没被捷径拆掉的证据就是它们仍绿。

- [ ] 可选同批：`TestLiveKimiInboxSummary` — 注入 `PickFiles` 指向临时 `note.txt`，`Inbox("files")` 后 `StartTask` prompt 含 inbox 段 +「写 summary.md」；断言源文件仍在；产物有 `summary.md` 或时间线有中文。没有 Kimi 则 `t.Skip`。

---

## 施工顺序

1. Task 1 扫描 + 入库映射 + peek inbox  
2. Task 2 Inbox 服务  
3. Task 3 Bridge 契约（含 dir.pick 截止修复）  
4. Task 4 Kimi  
5. Task 5 Cursor  
6. Task 6 工作台四卡 + 传文件  
7. Task 7 产物过滤  
8. Task 8 对话框 + 回归  

每 Task：红 → 最小实现 → 绿。不要先做四卡再补测试。不要按 V1.3.2 计划施工（它会把 `inbox` 直接写入 SQLite）。

---

## 5. 100% 验收（可测）

**固定场景**

| 场景 | 做成什么样 | 不纳入 |
|---|---|---|
| 做 PPT | 点「做 PPT」锁 Kimi → 必须选文件夹 → 可添加参考文件 → 执行。能找到则带 `kimi-slides`。结束能打开 `.pptx`，或诚实提示没有文稿 | 月汐排版；`office.generate`；页内翻页 PPT；保证版式 |
| 写新项目 | 点「写新项目」锁 Cursor → 必须选根 → `--workspace` 钉死该根。可把需求/设计添加进 inbox | 内置脚手架；中途问答；保证一次编译过 |
| 改现有代码 | 点「改现有代码」锁 Codex → 必须选仓库根。可把缺陷截图/说明添加进 inbox。变更清单不含 `node_modules` | 去掉 `--ignore-user-config`；页内 Git diff |

**自由调度（不能丢）**

| 能力 | 做成什么样 | 不纳入 |
|---|---|---|
| 任选 Agent | 点「其它任务」后出现 C / Cu / K，哪个 `available` 就能执行 | 灰卡假装能跑 |
| 任意任务 | 文本框自由写，不套场景前缀 | 月汐理解任务类型并换模型 |
| 传文件 | 「添加文件」「添加资料夹」；副本进 `.agenthub-inbox/`；chip 可移除；执行时 prompt 自动带文件名 | 对话区上传、办公附件、拖拽、云盘 |
| 收回产物和信息 | 时间线 + 本轮变更 + inbox 入参 + 预览/本机打开 | 新通知栈；页内 Office 渲染 |

**传文件专项（必须全绿）**

| # | 验收 |
|---|---|
| F1 | 工作台未离开月汐即可多选本机文件并看到 chip |
| F2 | 副本落在当前工作目录 `.agenthub-inbox/`，原路径文件内容与时间戳不被改 |
| F3 | 自由模式未选目录时点「添加文件」：引擎先分配 `agent-hub\日期-序号`，再复制，chip 绑定该目录 |
| F4 | 捷径未选目录时点「添加文件」：先弹出选目录，取消则不复制 |
| F5 | 「添加资料夹」把该夹内普通文件拷进 inbox（跳过依赖目录，封顶），不是把用户资料夹当成项目根 |
| F6 | 执行后三家 prompt 都含 inbox 文件名；Kimi 长文仍在 `.agenthub-prompt.txt` |
| F7 | 任务详情默认能看见 inbox 入参和本轮新产物（running 与 success 都要） |
| F8 | 超过 20 个 / 单文件 100MB / 合计 200MB 的项进 `skipped`，已拷的保留，中文说明 |
| F9 | 已有 hello 真机（Codex/Cursor/Kimi）仍过——证明自由路径没被捷径或传文件拆掉 |
| F10 | 任务结束 `UpsertArtifact` 不因 `source=inbox/changed` 失败（入库为 `scan`，读出还原） |

**真机（可选，不阻塞自动化）**

| ID | 内容 |
|---|---|
| L-PPT | 捷径做两页 demo.pptx |
| L-WRITE | 捷径建 README + src/hello.txt |
| L-FIX | 捷径修一个故意写坏的小文件 |
| L-INBOX | 向临时目录 inbox 拷一个 `note.txt`，自由 Kimi「读 inbox 并写 summary.md」；源 `note.txt` 仍在原处 |
| L-FREE | 已有 Codex/Cursor/Kimi hello **保持通过** |

---

## 6. Spec 覆盖自检

| 条款 | 任务 |
|---|---|
| 四卡 / 自由不丢 | Task 6 |
| 捷径必选目录 | Task 6 |
| 自由可选目录 | Task 6 |
| 添加文件 / 资料夹 / 移除 | Task 2 + 3 + 6 |
| 原件不被改 | Task 2 |
| 自由无目录先分配再拷 | Task 2 + 6 |
| 捷径无目录先 dir.pick | Task 6 |
| inbox 入参段 | Task 6 |
| 扫描 inbox / skip / changed | Task 1 |
| 入库压 source + 读出还原 | Task 1（F10） |
| 运行中 peek 看见 inbox | Task 1（F7） |
| Kimi 文件 + skills | Task 4 |
| Cursor workspace | Task 5 |
| Codex 无强制改代码前缀 | Task 5 + 6 |
| 10 分钟选文件/选目录 | Task 3 + 8 |
| UI 默认 inbox+变更 | Task 7 |
| 不拆自由真机 | Task 8 |
| 不改 Office / 不手改生成物 / 不加 0154 | 全局约束 |

占位扫描：本文无 TBD/TODO/「类似 Task N」。V1.3.2 漏写的 Allocates / SkipsTooLarge / persistableSource / peek inbox / 现网 UI 测试重写，均已写入对应 Task。

---

## 7. 残留风险（做完 V1.3.3 仍不是产品能担保的）

这些**不是漏做**，不要为它们加范围：

| 风险 | 为什么接受 |
|---|---|
| Kimi 不一定交出 pptx / 版式一般 | 月汐不代排版；验收是「调度 + 打开或诚实提示」 |
| Cursor `--workspace` 若该机 CLI 过旧不认 flag | 时间线会有 CLI 原文；不回退去掉 flag |
| 用户项目根会出现 `.agenthub-inbox/` 与 `.agenthub-prompt.txt` | 这是三家都能读到的代价；不自动改 `.gitignore` |
| 自由模式取消选文件后可能留下空的 `日期-序号` 目录 | allocate 必须在对话框前；取消由前端忽略 dir |
| peek / 资料夹 Walk 不是全盘实时监视 | V1 不加 fsnotify；400ms `task.get` 足够 |
| 产物中心跨任务列表的 `changed` 还原弱于详情 | 跨任务查询没有 `startedAt`；inbox 仍按路径还原。F7 以详情为准 |
| 本机没装某家 CLI | 灰卡 + Hint，不算功能缺失（V1.2 已锁定） |

---

## 8. 对照表

| 原话 | 落地 |
|---|---|
| 做 PPT 对接 Kimi 自己的能力 | 捷径卡 + skills-dir + 本机打开 pptx |
| 写项目先定根再 Cursor | 捷径卡 + 必选根 + `--workspace` |
| 改代码先定根再 Codex | 捷径卡 + 必选根 + 变更清单 |
| 也能让三家做别的 | 「其它任务」+ 选 Agent + 自由原文 |
| **传输文件让他们分析/处理/修改并返回** | **添加文件/资料夹 → 复制 inbox → prompt 点名 → 三家读副本 → 时间线+产物** |
| 收回产物和信息 | 同一套时间线 / inbox / 本轮变更 / 预览 / 打开 |

---

## 9. 批准

本文替代 V1.3.2。相对 V1.3.2 的工程修正只有三条：入库 source 映射、运行中 peek inbox、补全/重写会翻车的测试。产品能力与 V1.3.2 相同。

批准前不改业务代码。批准后按 Task 1→8 一次性执行。