> **已由 V1.3.3 替代。** 不要按本文施工。最新可执行规格：`lunitide-agent-hub-三场景-落地规格-v1.3.3.md`（修正了 0153 `source` CHECK、运行中 peek 看不见 inbox、以及会集体红的现网 UI 测试）。

# 月汐「Agent 调度台」场景捷径 + 自由调度 + 传文件 PRD

| 文档信息 | |
|---|---|
| 产品 | 月汐 / Lunitide（Go Engine + WebView2 + React） |
| 范围 | 在调度台 **V1.2 引擎**上：三条固定场景当捷径，保留任选三家做其它事，并**在工作台里把文件交给 Agent** |
| 版本 | **V1.3.2**（在 V1.3.1 上补「传文件」为一等能力） |
| 日期 | 2026-09-12 |
| 状态 | **执行规格。** 批准后按配套实施计划改代码 |
| 前序 | `lunitide-agent-hub-prd.md` V1.2 引擎约束全部继承 |
| 配套计划 | `lunitide-agent-hub-三场景-实施计划.md` |

---

## 0. 需求是否满足（先结论）

### 0.1 用户要的全部能力

| # | 需求 | V1.3 | V1.3.1 | **V1.3.2** |
|---|---|---|---|---|
| 1 | 做 PPT → 固定走 Kimi，用它自己的 skill/MCP/工具，收回 pptx | 有 | 保留 | 保留 |
| 2 | 写新项目 → 先选根 → 固定走 Cursor，按规则建目录写代码 | 有 | 保留 | 保留 |
| 3 | 改现有代码 → 先选根 → 固定走 Codex，读、改、交回 | 有 | 保留 | 保留 |
| 4 | 三家也能做其它事：写文档、总结分析、办事收回产物、做小游戏… | **丢了** | **第四张卡「其它任务」** | 保留 |
| 5 | **把文件传给 Agent**，让它分析、处理或修改，再返回产物和信息 | 无 | 只写了「自己去资源管理器拷进文件夹」 | **工作台「添加文件 / 添加资料夹」：本机多选 → 复制进工作目录 → 三家都能读到 → 改副本或写出新文件 → 时间线+产物收回** |

V1.3.1 把「传资料」做成约定，不是控件。用户必须离开月汐、自己拷文件、再在任务里手写文件名。这**不满足**「一定要能传输文件给他们」。V1.3.2 把传文件做成和工作目录同级的操作。

**一句话：** 三张卡是少点两下的固定套路；第四张卡是自己选 Agent、自己说干什么；**添加文件是把材料交到 Agent 手里的按钮，不是唯一干活方式。**

### 0.2 为什么必须复制进工作目录（不能只传原路径）

三个 CLI 都被钉在工作目录里：

| Agent | 可见范围 |
|---|---|
| Cursor | `--workspace <workDir>`，工作区外文件经常读不到 |
| Codex | `--cd <workDir>` + 沙箱，默认 `workspace-write` 不保证能读盘符其它位置 |
| Kimi | 进程 `Dir=workDir`，习惯只动本目录 |

只把 `C:\Users\...\纪要.pdf` 写进 prompt，Cursor/Kimi 经常找不到，Codex 只读沙箱也会拒。**复制一份到 `workDir/.agenthub-inbox/` 是三家 100% 都能读到的唯一落地法。**

原文件不动。Agent 改的是副本，或在工作目录写出新文件。结果仍走现网时间线 + 产物。

### 0.3 方案取舍（已锁定）

| 方案 | 结论 |
|---|---|
| **本机选文件/资料夹 → 引擎复制进 `.agenthub-inbox/`（采用）** | 三家都能读；原件安全；不进聊天附件总线 |
| 只选工作目录，用户自己去资源管理器拷 | **否。** 上一版。不叫「传输」 |
| 只把原绝对路径写进 prompt，不复制 | **否。** Cursor/Kimi 经常读不到 |
| 复用 `desktop.files.pick` / `people.file.*` / `attachment.*` | **否。** 宿主附件、会话附件、办公总线，隔离不允许焊过来 |
| WebView `<input type=file>` / 拖拽上传 | **否。** WebView2 不稳定；无引擎复制仍落不到三家工作区 |
| 聊天式附件云存储 | **否。** 新存储、新迁移，本迭代不做 |

### 0.4 100% 验收（可测）

**固定场景（捷径）**

| 场景 | 做成什么样 | 不纳入 |
|---|---|---|
| 做 PPT | 点「做 PPT」锁 Kimi → 必须选文件夹 → 可添加参考文件 → 执行。能找到则带 `kimi-slides`。结束能打开 `.pptx`，或诚实提示没有文稿 | 月汐排版；`office.generate`；页内翻页 PPT；保证版式 |
| 写新项目 | 点「写新项目」锁 Cursor → 必须选根 → `--workspace` 钉死该根。可把需求/设计文件添加进 inbox。产物默认本轮新文件 | 内置脚手架；中途问答；保证一次编译过 |
| 改现有代码 | 点「改现有代码」锁 Codex → 必须选仓库根。可把缺陷截图/说明文件添加进 inbox。变更清单不含 `node_modules` | 去掉 `--ignore-user-config`；页内 Git diff |

**自由调度（不能丢）**

| 能力 | 做成什么样 | 不纳入 |
|---|---|---|
| 任选 Agent | 点「其它任务」后出现 C / Cu / K 胶囊，哪个 `available` 就能执行 | 灰卡假装能跑 |
| 任意任务 | 文本框自由写：写文档、总结、分析、小游戏、脚本… 不套场景前缀 | 月汐理解任务类型并换模型 |
| **传文件** | 「添加文件」「添加资料夹」打开本机对话框；副本进入 `.agenthub-inbox/`；chip 可移除；执行时 prompt 自动带文件名 | 对话区上传、办公附件、拖拽、云盘 |
| 收回产物和信息 | 时间线（信息）+ 本轮变更 + inbox 入参 + 预览/本机打开 | 新通知栈；页内 Office 渲染 |

**传文件专项（必须全绿）**

| # | 验收 |
|---|---|
| F1 | 工作台未离开月汐即可多选本机文件并看到 chip |
| F2 | 副本落在当前工作目录 `.agenthub-inbox/`，原路径文件内容与时间戳不被改 |
| F3 | 自由模式未选目录时点「添加文件」：引擎先分配 `agent-hub\日期-序号`，再复制，chip 绑定该目录 |
| F4 | 捷径未选目录时点「添加文件」：先弹出选目录，取消则不复制 |
| F5 | 「添加资料夹」把该夹内普通文件拷进 inbox（跳过依赖目录，封顶），不是把用户资料夹当成项目根 |
| F6 | 执行后三家 prompt 都含 inbox 文件名；Kimi 长文仍在 `.agenthub-prompt.txt` |
| F7 | 任务详情默认能看见 inbox 入参和本轮新产物 |
| F8 | 超过 20 个 / 单文件 100MB / 合计 200MB 的项进 `skipped`，已拷的保留，中文说明 |
| F9 | 已有 hello 真机（Codex/Cursor/Kimi）仍过——证明自由路径没被捷径或传文件拆掉 |

### 0.5 可行性

V1.2 引擎已能自由调度三家。V1.3.2 补：

- 场景捷径 UI
- Kimi 长任务/技能目录
- Cursor `--workspace`
- 扫描跳过依赖目录
- **一个新 Bridge：`agentHub.inbox`（选文件/选资料夹/列出/移除）。这是相对 V1.3.1「不加任何新 Bridge」的唯一放宽。**
- 不改 `task.start` schema（文件已经在磁盘上）
- 不加迁移、不碰办公主链、不焊 `office.generate`

现网漏洞必须顺手修：`agentHub.dir.pick` 前端等 10 分钟，但 `bridge.MaxDeadlineMS` 仍是 30 秒，宿主会拒长截止。选目录和选文件对话框都必须把 Go/前端天花板改成 **600_000**。

---

## 1. 方案（已锁定）

| 方案 | 结论 |
|---|---|
| **四卡：三捷径 + 其它任务（采用）** | 选取仍简单；自由能力不丢 |
| 只留三张卡 | **否。** |
| 办公 PPT 焊 Kimi | **否。** |
| 传文件 = inbox 复制（采用） | 见 §0.3、§3.1 |

模式枚举（仅前端 + localStorage，不入库）：

`ppt | write | fix | free`

Inbox 常量：目录名 **`.agenthub-inbox`**（扫描不跳过它；跳过的是 `.agenthub-prompt.txt`）。

---

## 2. 目标

| # | 目标 | 衡量 |
|---|---|---|
| T1 | 工作台第一眼：四张卡 | 三捷径 +「其它任务」 |
| T2 | 三捷径必须先选目录才能执行 | 前端门闩测试 |
| T3 | 「其它任务」必须先选 Agent；目录可选 | 未选 Agent 不能 start；未选目录且未添加文件时走 V1.2 默认 `agent-hub\日期-序号` |
| T4 | Kimi **无论场景还是自由**：长文进 `.agenthub-prompt.txt`，argv 短；能找到则 `--skills-dir` | 适配器单测不依赖 scene |
| T5 | Cursor 只要有绝对 `workDir` 就带 `--workspace` | 写新项目与自由「做小游戏」共用 |
| T6 | Codex argv 保持 `--ignore-user-config`；**只有捷径「改现有代码」才加场景前缀** | 自由 Codex 写文档/游戏时 stdin 不含「只修 bug」；但若有 inbox 文件，**所有模式**都追加 §4.5 入参段 |
| T7 | 终扫跳过依赖目录；默认展示本轮变更 **和 inbox 入参** | 场景和自由共用 |
| T8 | 结果通道不变：详情时间线 + 产物 + 打开目录 | 写文档得到 md；总结得到 md；游戏得到源码；PPT 得到 pptx；改过的 inbox 副本若 mtime 变了也列出来 |
| **T9** | **工作台能传输文件/资料夹给当前任务的 Agent** | F1–F8 |

### 非目标

与 V1.3 相同：不嵌 GUI、不接 API Key、不动 `token_ledger`、不改办公主链、不加 fsnotify、不加 scene 列、不页内渲染 PPTX、不中途问答。

另加：

- 后端适配器不读 scene。前缀由前端在点捷径时写入 `prompt`。
- **不**把 inbox 做成数据库附件表。
- **不**做拖拽、粘贴、云盘、会话附件。
- **不**移动或删除用户原文件。
- **不**复用 `desktop.files.pick` / `people.file.*` / `attachment.*`。
- 除 `agentHub.inbox` 外不加其它新 Bridge；**不加 0154 迁移**。

---

## 3. 工作台（直观、选取简单）

```
工作台
├── 说明：常用三件事一键开始；其它事点「其它任务」自己选 Agent
├── 四张卡（互斥）
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
│     [添加文件]  [添加资料夹]
│     已添加 chip（文件名 + 大小；× 移除副本）
│     一行说明：原文件不动。Agent 读/改的是工作目录里的副本。
├── 任务说明
├── mode=free 且 agent=codex：沙箱三档（默认 workspace-write）
├── mode=free：快捷句（写文档 / 总结本目录 / 做小游戏 / 写周报 Markdown）
├── [执行]
└── 进行中任务
```

记住：`lunitide:agent-hub-scene` = `ppt|write|fix|free`  
目录：`lunitide:agent-hub-workdir:ppt|write|fix|free`

Inbox 文件列表**不进 localStorage**（避免路径过期）。工作台在 `workDir` 变化时调用 `agentHub.inbox` `action=list`。

### 选取规则

1. 第一次打开：不预选卡，执行禁用。
2. 点捷径：锁 Agent，隐藏胶囊与沙箱与自由快捷句，占位换成该场景，带出该场景上次目录；若该目录存在则 `list` inbox。
3. 点「其它任务」：显示胶囊，默认选**第一个 available** 的 Agent；目录沿用 `…:free` 或空（默认 agent-hub）。
4. 捷径卡对应 CLI 不可用：卡灰，Hint，不能选中。
5. 自由模式某胶囊不可用：点胶囊只弹 Hint，不切换。
6. 超时对用户隐藏，默认 60 分钟。
7. 捷径 Codex 沙箱隐藏，固定 `workspace-write`。
8. 未选卡时，「添加文件 / 添加资料夹」禁用。

### 卡文案

| 卡 | 标题 | 副文案 |
|---|---|---|
| ppt | 做 PPT | 固定交给 Kimi，用它自己的技能做演示文稿 |
| write | 写新项目 | 先选项目根，固定交给 Cursor 建目录并写代码 |
| fix | 改现有代码 | 先选仓库根，固定交给 Codex 阅读并修改 |
| free | 其它任务 | 自己选 Codex / Cursor / Kimi：写文档、总结资料、做小游戏… |

### 3.1 传文件（四卡共用，硬需求）

月汐工作台提供两个按钮，不经过资源管理器手工拷贝（手工拷进工作目录仍然有效，只是不再是唯一办法）。

#### 添加文件

1. 已选四卡之一。
2. **捷径且还没有工作目录：** 先走现网 `agentHub.dir.pick`。取消 → 结束，不打开选文件框。
3. **自由且还没有工作目录：** `agentHub.inbox` `action=files` 且 `workDir=""`，引擎 `allocateDir` 后打开多选框，返回的 `workDir` 写进 chip 与 `localStorage`。
4. **已有工作目录：** `action=files` + 该 `workDir`。
5. 本机多选对话框（所有文件，不过滤扩展名）。取消 → `canceled=true`，已有 chip 不变。
6. 引擎把每个普通文件**复制**到 `<workDir>/.agenthub-inbox/<文件名>`。重名则 `纪要 (2).pdf`。
7. 返回 `files` + `skipped`。前端替换 chip 为完整 inbox 列表（以返回的 `files` 为准，等于 list）。
8. 有 skipped：页内错误文案列出原因，不弹系统 Alert。

#### 添加资料夹

与上面相同，但 `action=folder`：用户选**源资料夹**（不是项目根）。引擎遍历该夹：

- 跳过与扫描相同的依赖目录名（`node_modules` `.git` …）
- 只收普通文件
- 保持相对结构：`.agenthub-inbox/<源夹名>/相对路径`
- 同一套 20 / 100MB / 200MB 封顶，超出进 skipped
- **不**把源资料夹设成 `workDir`

写新项目 / 改现有代码的「项目根」仍然只由「选择文件夹」决定。

#### 移除

chip 上 × → `action=drop` + `workDir` + `name`（inbox 内相对路径，如 `纪要.pdf` 或 `资料/纪要.pdf`）。只删副本。原件不动。`name` 必须在 `.agenthub-inbox` 内，否则「路径不受支持」。

#### 切换 / 恢复工作目录

换目录或自由模式「恢复默认」后重新 `list`。旧目录的 inbox 副本留在旧目录，不跟着搬。

#### 封顶与拒绝

| 规则 | 值 |
|---|---|
| 单次对话框最多收入 | 20 个文件 |
| 单文件 | ≤ 104857600 字节（100MB） |
| 单次合计 | ≤ 209715200 字节（200MB） |
| 源必须 | 存在的普通文件；资料夹必须是目录 |
| 目的必须 | 已存在或可创建的绝对 `workDir`，且通过现网 `forbiddenWorkDir` |
| 不跟随 | 复制时不把 junction/symlink 当目录递归进去；普通文件按内容复制 |
| 不收入 | `.agenthub-prompt.txt`；目的若解析后逃出 `workDir` 则跳过该条 |

超限项进 `skipped`（中文，如 `纪要.pdf 超过 100MB`），已成功的保留。0 个成功且 0 个 skipped 且未取消 → 视为取消。

#### 打开副本

任务尚未开始：不新开「打开文件」Bridge。chip 只展示名字。任务开始后走现网 `agentHub.file.preview` / `file.open`（路径相对于 workDir，例如 `.agenthub-inbox/纪要.pdf`）。

工作台仍可用「选择文件夹」后自己往根目录丢文件；扫描会收到，但默认 UI 只强化 inbox + 本轮变更。文案引导优先用「添加文件」。

### 目录 chip 下文案

- 任何已选卡：`添加文件后，Agent 会在本目录的 .agenthub-inbox 里读副本。原文件不会被改。`
- 捷径写/改另加：`项目根用「选择文件夹」。规则放在根目录（AGENTS.md、.cursor/rules、README）或写在下面。`
- 自由且未选目录且 inbox 空：`不选目录就执行时，会用默认 agent-hub 目录。要用已有项目请先选文件夹。`

### 自由快捷句（只填文本框，不锁 Agent）

| 按钮 | 填入的 prompt |
|---|---|
| 写文档 | 根据本工作目录已有材料写一份 Markdown 说明，只在本目录保存。 |
| 总结本目录 | 阅读本工作目录里的资料（含 .agenthub-inbox 与 pdf/ppt/md/txt），写一份结构化总结 Markdown，只在本目录保存。 |
| 做小游戏 | 在本工作目录做一个可运行的小游戏（说明怎么运行），只在本目录写文件。 |
| 写周报 Markdown | 根据本工作目录材料写一份周报 Markdown，不要生成 Office 文档。 |

用户仍可改字、可换 Agent（例如总结用 Kimi，小游戏用 Cursor）。有 inbox 时执行仍自动追加 §4.5。

### 占位

| 模式 | 占位 |
|---|---|
| ppt | 根据本目录大纲做 12 页介绍 PPT，输出 pptx |
| write | 在本目录按规则建文件夹并写最小可运行代码 |
| fix | 说明缺陷，只改必要文件 |
| free | 写下要做的事。材料用上面的「添加文件」 |

### 结果（场景和自由同一套）

1. 执行后进任务详情。
2. 时间线 = 信息；变更文件 + inbox = 产物区。
3. 默认 `event` ∪ `changed` ∪ `inbox`；开关「显示目录内其它文件」再含 `scan`。永远不展示 `outside`。
4. md/代码/图页内预览；pptx/pdf/docx 本机打开。
5. 写文档/总结：预期 `.md`。小游戏：预期源码 + 说明。PPT：预期 `.pptx`。修改传入文件：预期 inbox 内同名副本 mtime 变化，或工作目录新文件。没有新文件仍可 `success`，提示看时间线。

---

## 4. 捷径详规（前缀只在捷径）

前缀由 **前端**拼进 `prompt` 再 `task.start`。后端只看见完整字符串。适配器不读 scene。

### 4.1 做 PPT（Kimi）

步骤：做 PPT → 选文件夹 →（可选添加参考文件）→ 写要求 → 执行 → 打开 pptx。

前端 prompt：

```
【场景：做 PPT】
工作目录：<workDir>
只在本目录写文件。优先使用 kimi-slides。产出 pptx。参考文件在本目录或 .agenthub-inbox 时先读再做。

用户任务：
<原文>
```

再拼 §4.5（若有文件）。`timeoutMin=60`，`agent=kimi`，`workDir` 必填。

成功：退出码 0。有 pptx 则列出来；没有则提示「没有找到演示文稿」。

### 4.2 写新项目（Cursor）

```
【场景：写新项目】
项目根：<workDir>
把该路径当作唯一项目根。只在该根下创建文件夹和文件。
先读 AGENTS.md、.cursor/rules、README（没有则跳过）。
用户传入的需求/设计在 .agenthub-inbox（没有则跳过）。
用户写的规则优先。做完即结束。

用户任务：
<原文>
```

再拼 §4.5。

### 4.3 改现有代码（Codex）

```
【场景：改现有代码】
项目根：<workDir>
只阅读和修改这个根内的文件。先看 README、AGENTS.md 和相关源码。
用户传入的说明/截图在 .agenthub-inbox（没有则跳过）。
改动保持最小。不要 git 提交或推远程。做完即结束。

用户任务：
<原文>
```

再拼 §4.5。`sandbox=workspace-write`。

### 4.4 其它任务（自由）

前端 **不拼场景前缀**。`prompt` = 文本框原文 + §4.5（若有文件）。`workDir`：有 chip 就传；空且无 inbox 时不传（引擎 `allocateDir`）。若因添加文件已分配目录，必须把该 `workDir` 传给 `task.start`，避免引擎再分配另一个空目录。`sandbox` 仅 Codex 按 chip。

例：

- Agent=Kimi，添加 `纪要.pdf`，「总结这些材料」→ 得到 `总结.md`
- Agent=Cursor，目录=空或新夹，「做小游戏」→ 得到游戏文件
- Agent=Codex，目录=仓库，「按 inbox 里的接口稿写一份说明」→ 得到 md
- Agent=Kimi，添加 `草稿.docx`，「润色并另存为 Markdown」→ 得到 md（不保证改回 docx）

### 4.5 入参段（有 inbox 文件时，四种模式都追加）

```
用户传入的文件已复制到 .agenthub-inbox/（原路径未改）。请先阅读。可以修改这些副本，或在本工作目录写出新文件。不要去改用户原路径上的文件。完成后把结果留在本目录。
- 纪要.pdf
- 资料/大纲.md
```

自由模式 **只有这一段是前端加的**，没有 `【场景：`。测试：自由 prompt 不含 `【场景：`，但含 `.agenthub-inbox` 当且仅当 chip 非空。

---

## 5. 后端

### 5.1 新 Bridge：`agentHub.inbox`

**唯一新增方法。** `x-owner: engine`，`x-enabled: true`。不进 `dataScopedMethod`。

**payload**

| 字段 | 规则 |
|---|---|
| `action` | 必填枚举：`files` \| `folder` \| `list` \| `drop` |
| `workDir` | 字符串，最长 1024。`files`/`folder` 可空（自由模式分配）。`list`/`drop` 必填绝对路径 |
| `name` | 仅 `drop` 必填，最长 512，inbox 内相对路径（正斜杠或反斜杠均可） |

**result**

| 字段 | 规则 |
|---|---|
| `canceled` | 布尔。对话框取消为 true |
| `workDir` | 实际使用的绝对目录；取消且未分配过可为 `""` |
| `files` | 当前 inbox 内全部普通文件，最多 200。`name`（展示名）、`path`（相对 inbox 的相对路径）、`size` |
| `skipped` | 最多 20 条中文原因 |

**截止：** 前端 `capBridgeDeadlineMs` 与 Go `bridge.MaxDeadlineMS` 对 `agentHub.inbox` **和** `agentHub.dir.pick` 均为 **600_000**。`list`/`drop` 也会拿到这个天花板，可接受。

**契约步骤（必须按序，禁止手改生成物）：**

1. 新建 `api/bridge/v1/agentHub.inbox.schema.json`
2. `envelope.schema.json` 方法枚举按字母序插入 `agentHub.inbox`（在 `agentHub.file.preview` 与 `agentHub.task.cancel` 之间）
3. `web/scripts/generate-bridge.mjs` 的 enabled 断言清单同步插入
4. `npm --prefix web run generate:bridge`
5. `handlers_registry.go` 增加 `agentHub.inbox` → `handleAgentHub`
6. `client.ts`：`capBridgeDeadlineMs` 与 `createAgentHubBridge` 对 `agentHub.inbox` 使用 `AGENT_HUB_DIR_PICK_MS`
7. `internal/bridge/deadline.go` + `deadline_test.go`

### 5.2 Inbox 服务（`internal/agenthub`）

```go
const inboxDirName = ".agenthub-inbox"
const maxIngestFiles = 20
const maxIngestFileBytes int64 = 104857600
const maxIngestTotalBytes int64 = 209715200

type InboxFile struct {
    Name string // filepath.Base，或带相对目录的展示名
    Path string // 相对 inboxDirName 的相对路径，正斜杠
    Size int64
}

func (s *Service) Inbox(action, workDir, name string) (canceled bool, dir string, files []InboxFile, skipped []string, err error)
```

可注入：`PickFiles func() ([]string, error)`，`PickFolder func() (string, error)`。空则走 Windows 本机对话框（与 `dir.pick` 同类：隐藏窗体 + `OpenFileDialog` Multiselect / `FolderBrowserDialog`）。非 Windows：`ErrPickCanceled` 或「当前系统不支持选择文件」。

行为：

- `files`/`folder`：`resolveWorkDir(workDir)`（空则 `allocateDir`）→ 对话框 → `os.MkdirAll(inbox)` → 复制 → `listInbox`
- `list`：`workDir` 必须绝对且通过 `forbiddenWorkDir`；inbox 不存在则空列表
- `drop`：`safeInboxPath(workDir, name)` 后 `os.Remove`；禁止删目录树以外

复制用读源写目的，不 `Rename`（跨盘、不移动原件）。

测试必须能在无对话框下完成：单测只设 `PickFiles`/`PickFolder`。

### 5.3 Kimi（所有 Kimi 任务）

```
写 workDir/.agenthub-prompt.txt = req.Prompt（UTF-8 无 BOM）
kimi -p <短指令> --output-format stream-json [--skills-dir DIR]...
Dir=workDir
stdin 空
短指令固定：
请阅读并执行本目录 .agenthub-prompt.txt。只在本目录创建或修改文件。
```

`--skills-dir` 仅当目录下存在 `kimi-slides/SKILL.md`：

1. `%APPDATA%\kimi-desktop\daimon-share\daimon\skills`
2. `%USERPROFILE%\.kimi-code\skills`
3. 环境变量 `KIMI_SKILLS_DIR`

找不到仍执行。开始事件可记「未找到 kimi-slides 技能目录」。不代配 MCP、不碰密钥。

`workDir` 在 Service `resolve` 之后才 `BuildCommand`。

### 5.4 Cursor（所有 Cursor 任务）

```
cursor-agent -p --force --trust --workspace <workDir> --output-format stream-json
Dir=workDir
stdin=req.Prompt
```

自由模式未选目录时，`workDir` 是刚分配的或因添加文件已绑定的目录，`--workspace` 仍等于它。

### 5.5 Codex（所有 Codex 任务）

```
codex exec --json --skip-git-repo-check --ignore-user-config --sandbox <sandbox|workspace-write> --cd <workDir> -o <workDir>/codex-last-message.md
stdin=req.Prompt
```

禁止去掉 `--ignore-user-config`。禁止后端再拼「改现有代码」前缀。stdin 可以含 §4.5，因为那是前端写入的用户可见 prompt，不是适配器私货。

### 5.6 扫描（所有任务）

跳过目录：`node_modules` `.git` `.hg` `.svn` `dist` `build` `out` `coverage` `.venv` `venv` `__pycache__` `.cursor` `.kimi-code` `.codex` `vendor` `.idea` `.vs`

**不跳过** `.agenthub-inbox`。

跳过文件：`.agenthub-prompt.txt`

`source`：

| 值 | 何时 |
|---|---|
| `event` | CLI 事件里出现的路径 |
| `outside` | 事件路径在 workDir 外（UI 永不展示） |
| `inbox` | 文件位于 `.agenthub-inbox/` 下（入参；**优先于** changed/scan） |
| `changed` | 非 inbox、非事件，且 `mtime` 在 `StartedAt−2s` 之后 |
| `scan` | 其余 |

默认 UI：`event` ∪ `changed` ∪ `inbox`，上限 200。

预览：pptx/ppt/doc/docx/xls/xlsx/pdf → `kind=file`。

`ScanWorkDir(workDir, eventPaths, startedAt time.Time)`。`startedAt` 零值：不标 `changed`。

---

## 6. 错误与并发

| 情况 | 行为 |
|---|---|
| 未选卡 | 执行与添加文件禁用 |
| 捷径未选目录 | 执行禁用；添加文件先选目录 |
| 自由未选 Agent | 执行禁用（无 available 时提示安装） |
| 自由未选目录且未添加文件 | 允许执行，用默认 agent-hub 目录 |
| 自由因添加文件已有目录 | start 必须带该 workDir |
| CLI 不可用 | 灰 + Hint |
| 选盘符根/系统目录 | 「工作目录不受支持」 |
| 对话框取消 | `canceled=true`，不报错 |
| 文件超限 | skipped 中文；已拷保留 |
| drop 逃出 inbox | 「路径不受支持」 |
| 未登录 | V1.2 中文映射 |
| 同 Agent 并行 | 排队 |
| 不同 Agent | 可并行 |
| 重启 | 遗留 running 失败 + 新规则终扫 |

---

## 7. 文件地图

**新建**

- `api/bridge/v1/agentHub.inbox.schema.json`
- `internal/agenthub/inbox.go`、`inbox_test.go`
- `internal/agenthub/pick_files_windows.go`、`pick_files_other.go`
- `internal/agenthub/skills.go`、`skills_test.go`

**改**

- `envelope.schema.json`、`generate-bridge.mjs` → 跑生成器（`bridge.ts` / `schema_generated.go` / contract test）
- `handlers_registry.go`、`agenthub_handlers.go`、`agenthub_handlers_test.go`
- `client.ts`、`agentHub.deadline.test.ts`、`deadline.go`、`deadline_test.go`
- `scan.go`、`scan_test.go`、`service.go`
- `adapter.go`、`kimi_test.go`、`cursor_test.go`、`codex_test.go`
- `AgentHubWorkbench.tsx`、`agentHubCopy.ts`、`agentHub.css`、`agentHubApi.ts`、`AgentHubPage.test.tsx`
- `AgentHubDetail.tsx` / `AgentHubArtifacts.tsx`
- `live_windows_test.go`（可选同批：inbox + 总结短任务）

**禁止改：** Office 页、Session 页、token_ledger、commandworker、生成物手改、0154 迁移、`desktop.files.*`、`people.file.*`、`attachment.*`。

---

## 8. 测试清单

### 自动化

| 测试 | 期望 |
|---|---|
| `TestInboxCopiesRegularFileLeavesSource` | 目的有副本；源内容与 size 不变 |
| `TestInboxRenamesCollision` | 第二次 `a.txt` → `a (2).txt` |
| `TestInboxSkipsTooLarge` | 超 100MB 进 skipped，不出现在 files |
| `TestInboxFolderSkipsNodeModules` | 源夹里 `node_modules/x.js` 不入库 |
| `TestInboxDropOnlyInsideInbox` | `name=../secret` 失败 |
| `TestInboxAllocatesWorkDirWhenEmpty` | `workDir=""` + PickFiles 后返回新目录 |
| `TestKimiBuildCommandWritesPromptFile` | 文件=完整 prompt；argv `-p` 不含用户长文 |
| `TestKimiSkillDirsFindsSlides` | 假根含 `kimi-slides/SKILL.md` |
| `TestCursorBuildCommandWorkspace` | `--workspace==WorkDir` |
| `TestCodexBuildCommandNoScenePrefix` | stdin 等于请求原文 |
| `TestCodexKeepsIgnoreUserConfig` | argv 仍含该 flag |
| `TestScanSkipsNodeModules` | 不出现 |
| `TestScanMarksRecentAsChanged` | 新文件 changed |
| `TestScanMarksInboxSource` | `.agenthub-inbox/a.pdf` 为 `inbox` 即使 mtime 很旧 |
| `TestMaxDeadlineMSAgentHubPickers` | `dir.pick` 与 `inbox` = 600000 |
| `TestWorkbenchPptRequiresDirAndLocksKimi` |  |
| `TestWorkbenchFreeStartsWithoutDir` | 其它任务可 start，无 `【场景：` |
| `TestWorkbenchFreeCanPickEachAgent` |  |
| `TestWorkbenchIngestListsFilesInPrompt` | mock inbox 后 start.prompt 含文件名与 `.agenthub-inbox` |
| `TestWorkbenchShortcutIngestPicksDirFirst` | 捷径无目录时先 `dir.pick` |
| `TestDetailShowsInboxByDefault` | `source:inbox` 默认可见；`scan` 默认不可见 |

### 真机

| ID | 内容 |
|---|---|
| L-PPT | 捷径做两页 demo.pptx |
| L-WRITE | 捷径建 README + src/hello.txt |
| L-FIX | 捷径修一个故意写坏的小文件 |
| L-INBOX | 向临时目录 inbox 拷一个 `note.txt`，自由 Kimi「读 inbox 并写 summary.md」；源 `note.txt` 仍在原处 |
| L-FREE | 已有 Codex/Cursor/Kimi hello **保持通过** |

---

## 9. 对照表

| 原话 | 落地 |
|---|---|
| 做 PPT 对接 Kimi 自己的能力 | 捷径卡 + skills-dir + 本机打开 pptx |
| 写项目先定根再 Cursor | 捷径卡 + 必选根 + `--workspace` |
| 改代码先定根再 Codex | 捷径卡 + 必选根 + 变更清单 |
| 也能让三家做别的 | 「其它任务」+ 选 Agent + 自由原文 |
| **传输文件让他们分析/处理/修改并返回** | **添加文件/资料夹 → 复制 inbox → prompt 点名 → 三家读副本 → 时间线+产物** |
| 收回产物和信息 | 同一套时间线 / inbox / 本轮变更 / 预览 / 打开 |

---

## 10. 红线

不焊 `office.generate`；不用 ArtifactInspector；不调用 `commandworker.Run`；不加 fsnotify；不手改生成物；不加 scene 列；不去掉 `--ignore-user-config`；不页内 PPTX；不中途问答；不拆 Office/Session 大页；**不准删掉自由选 Agent**；**不准把传文件做成「请自己去资源管理器拷」**；不移动用户原文件；不复用 people/desktop/attachment 附件总线。

---

## 11. 批准

本文替代 V1.3.1。相对 V1.3.1 的唯一产品加项：工作台传文件（`agentHub.inbox` + `.agenthub-inbox`）。配套实施计划见同目录 `lunitide-agent-hub-三场景-实施计划.md`。批准前不改业务代码。
