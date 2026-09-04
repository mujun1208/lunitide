# 日常任务能力整改 · 可执行落地 PRD

日期：2026-09-04  
版本：落地版（复核后重写）  
状态：已批准，P0 已完成；P1 未开工。实施计划：[../plans/2026-09-04-daily-ops-capability.md](../plans/2026-09-04-daily-ops-capability.md)。  
产品：月汐（Lunitide）Go Engine + React WebView2 + SQLite  

**本文是唯一事实源。** 前稿 [2026-09-04-daily-ops-capability-prd.md](./2026-09-04-daily-ops-capability-prd.md) **superseded**（方向保留，锁死项以下文为准）。  
能力补齐旧文 [2026-09-04-capability-slots-design.md](./2026-09-04-capability-slots-design.md) 同样只作对照，不开工。

沿用冻结：不新 daemon；内核 SQLite；不改 poison / 说完再答 / 月伴 TTS / 专家表；`desktop.open` 成功=前置后前台命中；电脑控制不自开；保护会话不可删改名；不假成功 `ENGINE_UNAVAILABLE`；不移动 `v0.4.62`。

---

## 0. 复核勘误（上一稿不能直接开工的点）

| ID | 上一稿问题 | 落地锁死 |
|---|---|---|
| R1 | R1 allow 写了 `kb.search` / `kb.cite` | **默认对话工具表没有这两项**（只在机务专家挂载时出现）。R1 默认只留 `web.search` `web.fetch` `memory.search` `memory.get` `user.ask`。专家已在工具表里则保留，不新加。 |
| R2 | 「CC 关仍可播放」 | `media.play` foreground **现码要求** `ccInvoker`（`media_foreground.go`）。CC 关：`desktop.open` 可以，foreground 播放按现网失败，文案指向启用电脑控制。不装成功。 |
| SoM | 视觉输出 `{"markId":n}` 整数 | 现码 ID 是 **`B1` `E1` `L2`**（`assignNodeIDs`）。契约改为 `{"markId":"B1"}`，必须落在本帧 `nodes[].id`。 |
| 空树 | 树空仍要视觉读号 | 编号表为空则 **无法读号**。空树 + 有 `kind=gui` → 允许 GUI 输出经 `frameId` 校验的归一化坐标；空树 + 无 GUI → `无法执行` + 观察。 |
| #L | 画面哈希不变立刻 `ok:false` | 会误杀未重绘的按钮，并打现网 `screen unchanged` 测试。改为：突变后哈希相同 = **L0 不确定** → 运行时自动一次 `wait until=change`（最长 700ms，不另调模型）→ 仍相同才 `ok:false`。`observe`/`list`/`screenshot` 不变仍成功。 |
| #I | 无 observe 禁止一切 xy | 空树必须给 GUI 留坐标口。锁：裸 xy 仅当（本轮已 observe **且** `count==0` **且** 执行器是 `kind=gui`）。chat/vision 永不输出 xy。 |
| Embed | 当已有能力 | `gateway.Adapter` **只有** Complete/Stream/Discover，无 `/embeddings`。P1 才加可选接口 `Embedder`，仅 `openai_compatible`。P0 不做向量。 |
| Key 池 | P0 改 `CredentialRef` | 凭据提交链（Windows + `secretlease` + submissionId）不能在 P0 重写。P0 不做 Key 池。P1：`CredentialRef` 仍是第一把；另加有序备份列表。 |
| Judge | P0 就要角色表 + 新 Bridge | P0：`plan.run` 验证模型用现成 `pickCompanionFlashModel` 启发式（同供应商 flash/lite），验证消息必须带 `l0`。P1 才落「能力路由」表与 Bridge。 |
| user.ask | 新状态机 `awaiting_human` | 会改 `SessionPage`/`CompanionStage`，和冻结冲突。**复用现网 UAC 停车**（已有 companion 测试）。只给工具参数加可选 `reason`。 |
| 画手 | #D 文件写「改 SoM 编号」 | **禁止重写** `AnnotateCapture` / `assignNodeIDs`。 |
| 分期 | P0 七条并行 | P0 只做「不配新模型也变准」且每条可单测。新 kind / 向量 / Key 池 / 能力路由 UI 全在 P1。 |

---

## 1. 目标与非目标

**目标：** 日常任务先走对的工具面，再观察世界，再允许视觉兜底。GUI 是可配的独立 kind，今天没有模型也能用 SoM 读号。

**非目标（写进测试反向用例）：**

- 不新开 UI-TARS Desktop / vLLM / 向量库进程；Setup 不夹带 GUI/embedding 权重。  
- 不为 OS-Atlas / GUI-Actor 写 transformers 加载器。  
- 不做个人微信/QQ 入站、手机节点、Home Assistant、远程桌面、天气商业 API。  
- 不恢复 Office COM 生成。  
- 不接线 `companionOpeningAck`，不改 poison / TTS 音色。  
- 不让子代理 / 同事画像 / 机务专家拿到 `computer.act` 或 GUI。  
- 不把月汐改成 Codex「截屏即主干」。  
- 不按 TRAE 缺口去外挂 Computer-Use MCP。

---

## 2. 现码锚点（实施时对照，禁止平行实现）

| 能力 | 锚点 |
|---|---|
| Kind 目录 | `internal/domain/provider/kind.go`：`NormalizeKind` 未知→`llm`；`ValidKind`；`CatalogForKind`；`VisionDescribeCatalog` |
| 前端页签 | `web/src/provider/modelKind.ts` `MODEL_KINDS`；`ProviderApp.tsx` |
| Bridge 枚举 | `api/bridge/v1/public.dto.schema.json` → `ModelDTO.kind` |
| 工具组装 | `internal/app/chat.go` 约 637–663：`autoToolProfile` → 默认再挂 plan/mcp/cc/skill/expert → `filterCompanionDefaultTools` |
| 寒暄收面 | `internal/app/chat_tool_profile.go` `autoToolProfile` / `applyToolProfile` |
| 桌面统一手 | `internal/ccapp/computer_act.go` `MapComputerAct`；模型只见 `computer.act` |
| 观察+角标 | `internal/ccapp/runhost.go` `ToolObserveUI`；`assignNodeIDs`；`AnnotateCapture`；`rememberHits` |
| 具名点击 | `clickNamedLadder`：InvokeUI → Win32 → 像素+hit-test；`verifyAfter` 再截图 |
| 打开 L0 | `internal/toolruntime/desktop_open_verify.go` `openedWindowConfirmed` |
| 纯打开 settle | `internal/app/chat_continue.go` `companionGoalIsOpenOnly` |
| 播放+CC | `media_foreground.go`：`invoke==nil` 则失败 |
| 计划验证 | `internal/app/chat_plan.go` `planVerifyPrompt`，同一 `model` |
| 决策 | `internal/toolruntime/user_ask.go`；schema `additionalProperties:false` |
| 月伴 flash | `web/src/provider/modelKind.ts` `pickCompanionFlashModel`；引擎侧需镜像同规则（P0 judge / 分类） |
| KB 向量列 | `kb_chunks.embedding` 写入现为 `NULL`（`m8_kb.go` `PutKBChunks`） |
| Gateway | `Adapter` = Complete/Stream/Discover；生图生视频是另接口 |

**已有、禁止当缺口重做：** 前台验窗、UIA 填表、observe 角标、`id=B1` 记忆、UAC/另存为停车、`frameId`+DPI、急停审计、五通道 `im.send`、三家入站、月伴语音、cron、Office `*.gen`。

---

## 3. 目标架构（落地形状）

```
用户句
  → classifyTaskRoute（规则；必要时 flash JSON）
  → 收工具表（叠在 autoToolProfile / companion deny / cc 门之上）
  → 模型只在该面里选工具
  → 每个突变工具返回附 l0
       L0 过 → 结算
       L0 不确定 → 自动 wait 一次或 P0 用 flash 看观察（plan.run 验证）
       L0 败 → ok:false + 一句原因
  → R2/R3 结构化失败
       observe（已有角标）
       nodes>0：gui 目录？gui : vision 读 {"markId":"B1"} → click id=
       nodes=0：仅 gui 可给归一化坐标；否则停
  → 再 L0
```

角色（P1 可配，P0 用缺省）：

| Role | Kind | P0 缺省 | P1 |
|---|---|---|---|
| chat | llm | 会话模型 | 下拉 |
| flash | llm | `pickCompanionFlashModel` | 下拉，空=自动 |
| vision | vision / llm+视力 | `VisionDescribeCatalog` | 下拉 |
| embed | embedding | 不调用 | 下拉，空=只 FTS |
| judge | llm | 同 flash 启发式 | 下拉；禁止默认=chat，除非勾选 |
| gui | gui | 空目录 | 下拉，空=SoM 读号 |

新 kind 仅 `embedding`、`gui`。

---

## 4. 数据与 API 契约

### 4.1 Model kind（P1，但 schema 必须一次改对）

`ModelDTO.kind` 枚举增加 `embedding`、`gui`（`voice` 保留给页签聚合）。

同步改：

- `api/bridge/v1/public.dto.schema.json`  
- 生成：`internal/bridge/schema_generated.go`、`internal/contract/schema_generated_test.go`、`web/src/generated/bridge.ts`（走现网 `verify:bridge`）  
- `internal/domain/provider/kind.go`：`KindEmbedding` `KindGUI`；`NormalizeKind`/`ValidKind`/`kindMatchesCatalog`  
- `web/src/provider/modelKind.ts`：`MODEL_KINDS` 加 `embedding` `gui`；`persistKind` 原样透传；`modelKind()` 页签映射  
- `ProviderApp.tsx` 页签文案：向量模型 / GUI 模型  

`NormalizeKind("gui")` 必须是 `gui`，**禁止**再掉进 llm。D-D1 先写失败再改。

### 4.2 任务路由（P0）

```go
type TaskRoute string // R0 R1 R2 R3 R4

func classifyTaskRoute(goal string, companion, ccEnabled bool) (TaskRoute, map[string]bool)
```

- 纯函数，`internal/app/task_route.go`。  
- `allow==nil` 表示不收面（非法 flash 回落）。  
- 规则表见 §5.A。不确定才 `classifyTaskRouteWithFlash`（P0 可先只规则；规则打尽测试语料后再加 flash）。  
- **P0 建议只规则**：测试语料全覆盖后，flash 分类放到 P0 末或 P1，避免 P0 依赖模型。

接入：`chat.go` 在 `filterCompanionDefaultTools` **之后**、`runStream` **之前**：

```
req.Tools = applyTaskRoute(req.Tools, route, allow)
```

`applyTaskRoute`：allow 为 nil 则原样；否则只留 `allow[name]`。`user.ask` 任何路由都保留。

`plan.run`：把父轮 `route` 放进 `runPlanCycle`，每步工具表再 `applyTaskRoute`。禁止步骤把 R1 扩成 `computer.act`。

### 4.3 L0 返回形状（P0）

工具 `Result` 增加可选 JSON 字段（给模型看的 summary 里嵌一段，或独立 `l0` 键，选现网 Result 已有扩展位，**不改 Bridge 事件名**）：

```json
{"ok":true,"l0":{"kind":"foreground|field|url|pixels|file|cite","passed":true,"uncertain":false,"detail":"…"}}
```

`ok` 以 L0 为准：`passed=false && !uncertain` → `ok:false`。

### 4.4 SoM 读号（P1 运行时；P0 只强制 observe+id）

```json
{"markId":"B1"}
```

`markId` 必须等于本帧 `nodes[].id`。非法/越界 → 失败，不回退 chat。

### 4.5 user.ask.reason（P0）

`chat_tool_defs.go` schema 增加可选：

```json
"reason":{"type":"string","enum":["login","2fa","captcha","pay","uac","file_picker","decision"]}
```

`validateUserAsk` 允许该字段。`web/src/session/userAsk.ts` `UserAskPack` 加 `reason?:`。UI 只换标题文案，**不新页面**。UAC/另存为继续现网停车。

### 4.6 能力路由表（P1）

```sql
CREATE TABLE capability_role_bindings (
  role TEXT PRIMARY KEY CHECK (role IN ('chat','flash','vision','embed','judge','gui')),
  provider_id TEXT,
  model_id TEXT,
  allow_judge_eq_chat INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
```

Bridge：`capability.roles.get` / `capability.roles.set`（新 schema + 生成）。空 `model_id` = 自动。  
设置页并入现设置，不新窗口路由。

### 4.7 Embedder（P1）

```go
type Embedder interface {
    Embed(ctx context.Context, secret []byte, model string, texts []string) ([][]float32, error)
}
```

仅 OpenAI 兼容实现 `POST {base}/embeddings`。Anthropic / volc_speech 不实现；配了类型不对 → 写入跳过，FTS 仍可用。

`PutKBChunks` 在有 embed 目录时写 `embedding` BLOB（float32 little-endian + 维数头）。失败留 NULL。

### 4.8 Key 备份（P1）

`providers` 增加 `credential_ref_backups TEXT`（JSON 字符串数组，最多 4）。`CredentialRef` 仍是当前租赁的第一把。  
轮转仅当错误正文/码匹配 `429` / `insufficient_quota` / `quota`。`401` 不轮。同一次 `chat.start` 最多换 3 把。  
UI：供应商编辑「添加备用 Key」，每把走现网 `submitCredential`，禁止明文进 SQLite。

---

## 5. 工作包（可执行，按此拆计划）

### 5.A 任务路由 — P0

**现状：** `autoToolProfile` 只把寒暄收成 minimal（`web.search/fetch` `memory.*` `user.ask`）。其余全套含 `computer.act`。

**改：** `classifyTaskRoute` + `applyTaskRoute`。

**规则（零模型，先写表驱动测试）：**

| 输入（含） | Route | allow 关键 |
|---|---|---|
| 仅寒暄（沿用 `autoProfileChatHints`，且无任务词，且 ≤40 字） | R0 | 同 minimal |
| 天气/气温/股价/金价/新闻/几点/汇率 且无「用汽水/用网易云」 | R1 | search/fetch/memory/user.ask |
| 打开浏览器查天气 / 上网查天气 | R1 | 同上，**无** browser.act |
| 打开/启动/把开 + 本机应用名 | R2 | desktop.open/type、media.play、office gen/parse、computer.act（仅 ccEnabled） |
| 播放/暂停/下一首 + 歌名或播放器 | R2 | 同上 |
| 打开/登录/点 + http(s) 或「网站/网页」 | R3 | browser.act、web.fetch、user.ask |
| 写一份/生成 PPT/表格/报告/画一张/做视频 | R4 | office gen、workspace.*、image/video.generate、web.search/fetch、user.ask |
| 冲突：查询词 + 打开浏览器 | R1 | |
| 冲突：查询词 + 点名已装 App | R2 | |
| 空或未匹配 | 不收面（nil allow） | 与今天全套相同 |

**不改：** `autoToolProfile` 仍先跑（R0 与 minimal 应对齐）。companion deny、`ccToolDefinitions` 门不动。

**验收：** D-A1–A6。  
**文件：** 新 `internal/app/task_route.go` `_test.go`；改 `chat.go` `chat_plan.go` `chat_tool_profile.go`（只调用，不把规则写进 chat.go）。

### 5.B L0 推广 — P0

| 工具 | 过线 | 失败 | 不确定（不装成功、不立刻判死） |
|---|---|---|---|
| desktop.open | 已有前台命中 | 假前台 Lunitide | — |
| media.play foreground | 播放器窗在前台（启动查询/CanonicalMusicApp） | 窗都没有 | 有窗但读不到 Now Playing → `uncertain`，对用户说「已打开播放器」，不编「正在播 X」 |
| media.play 无 invoke | 现网错误，不改 | CC 未开 | — |
| desktop.type | UIA 再读值包含提交文本 | 读到空或旧值 | — |
| browser.act navigate | final URL 非空 | BROWSER_MCP_NOT_READY 当成功（现网应已失败，补测试 D-B3） | — |
| browser.act click/type | 返回新鲜 snapshot | stale ref 当成功 | snapshot 相同 → 一次 wait 再 snapshot |
| computer.act 突变 | 见 5.L | 前台是 Lunitide | 哈希相同 → 自动 wait |
| *.gen | 文件存在且 size>0 | 现网已接近 | — |

**禁止** 新 EnumWindows。复用 `ForegroundWindow` / `ActivateWindowMatching`。

**验收：** D-B1–B5。  
**文件：** `desktop_open_verify.go`（抽出命中给 media 复用）、`media.go`/`media_foreground.go`、`runtime.go` desktop.type 回读、browser 执行返回、`ccapp/service.go` verifyAfter。

### 5.C plan.verify — P0（无新表）

**改：** `runPlanCycle` 每步收集 `l0`；验证 user 消息必须含这些 JSON。验证 `Complete` 的 model = `pickCompanionFlashModel` 的 Go 镜像（同供应商、id 匹配 flash/air/lite/mini/haiku，否则用当前 model 但测试 D-C1 在「供应商同时有 plus 与 flash」夹具下断言 ≠ chat）。

无 L0 观察 → `verified` 强制 false。各步 L0.passed 全 true → **不调用**验证模型，`verified=true`。

**验收：** D-C1–C3。  
**文件：** `chat_plan.go`；把 `isCompanionFlashModelId` 抽到 `internal/domain/provider` 或 `internal/app/flash_model.go` 供 Go/测试与 TS 对齐（TS 已有，Go 新镜像 + 单测对照几条 id）。

### 5.G user.ask reason — P0

见 §4.5。登录墙：`browser.act` 已有文案「Login walls… stop and ask」——落地为：检测登录墙/支付（URL 或 snapshot 含 password/captcha/pay）时 **运行时**改写为本轮 `user.ask`（reason=login|pay|captcha），不让模型再调 `computer.act`。UAC 回归 D-G2。

**不改** SessionPage 停车骨架。  
**文件：** `user_ask.go`、`chat_tool_defs.go`、`userAsk.ts`、browser 执行分支、现网 companion UAC 测试。

### 5.I 本轮先 observe — P0

在 `ccapp.Service` 增加本 session 轮次记忆（已有 `rememberHits` / `CurrentFrameID`）：

- `observedFrameID`：本轮最近一次成功 `action=observe` 的 frameId  
- 裸 `click/drag` 带 x,y：若 `observedFrameID==""` → `ok:false`「先 observe」  
- 例外：`observedFrameID!=""` 且该帧 `count==0` 且调用方是 GUI 兜底（内部 flag，模型调不到）  
- `click id=B1`：必须 `lookupHit` 命中，否则失败（现网接近，补 D-J4）  
- 纯打开 settle 后禁止本轮 `computer.act`（现网保持，回归 D-H1）

**不重写** AnnotateCapture。  
**验收：** D-I1–I3。  
**文件：** `runhost.go` `service.go`。

### 5.J 无名只点 ID — P0

1. Acc `collectIfActionable`：无名不再 `return false`；Name=`role (unnamed)`。  
2. `InvokeUI`：若 query 匹配 `^[A-Z]\d+$`，走 `rememberHits` 坐标/AutomationId，**禁止**把 `button (unnamed)` 当唯一键 Invoke。  
3. `name=` 模糊命中 >1 → `ok:false`，summary 列出候选 id。  
4. 不先 observe 的 `id=B1` → 失败。

**验收：** D-J1–J3。  
**文件：** `host_windows_ui.go` `host_windows_uia.go` `service.go`。

### 5.K truncated — P0

`observe` JSON 增加 `truncated` `maxNodes` `returned`。`len==maxNodes` → truncated=true，summary 提示 scroll 后再 observe。不改 80/120 上限。

**验收：** D-K1–K2。  
**文件：** `runhost.go`。

### 5.L verifyAfter — P0（修正）

`verifyAfter` 对突变工具：哈希相同 → 内部 `wait until=change` 700ms → 再截；仍相同 → 返回 error（`ok:false`），summary 保留两帧说明。  
观察类工具：保持现网「screen unchanged」成功（`cc_control_test.go` 那条回归）。

**验收：** D-L1–L2。  
**文件：** `service.go`；改/补 `cc_control_test.go`。

### 5.O CJK paste — P0

`desktop.type` 与 `computer.act type`：文本含汉字 **或** rune>16 → 走现有 `paste` 路径（`ToolPaste`），不逐键。拉丁短词仍 type。  
游戏只收按键：诚实边界，不特殊做。

**验收：** D-O1。  
**文件：** `runtime.go`（type 分派）、`runhost.go` ToolKeyboardType。

### 5.N 门 — P0（文档+测试，少改代码）

CC 关闭：`ccToolDefinitions` 已返回空。补测试：SoM 读号函数在 `!Enabled` 直接 return。失败句若模型仍说「无法执行」且无向导，只改 **工具错误字符串**（computer.act 不可见时不会被调；media.play foreground 错误改为「请在设置打开电脑控制后再播放」）。

**验收：** D-N1。

### 5.D kind=gui + 读号 — P1

顺序：先 D-D1 schema/kind.go，再页签，再 `gui_fallback.go`。

`pickGUIFallback(ccOn, nodes, guiCatalog, visionCatalog) executor`：

| 条件 | 执行 |
|---|---|
| !ccOn | none |
| R1/R4/R0 | none |
| nodes>0 && guiCatalog | gui 模型，只许输出 markId |
| nodes>0 && !gui && vision | vision + `guiSomPickPrompt` |
| nodes==0 && gui | gui 归一化坐标 + frameId |
| else | none → 无法执行 |

Vision/GUI 调用走现有 Complete+Images，不新协议。  
页签空态文案见前稿，保留 UI-TARS 接入草稿（不下载）。

**验收：** D-D1–D8。  
**文件：** kind.go、modelKind.ts、ProviderApp、schema 生成、`gui_fallback.go`、`chat_tool_defs.go` 描述补「先 observe」。

### 5.E embedding — P1

见 §4.7。查询：FTS 与向量并行，融合 `0.5*fts + 0.4*cos + 0.1*recency`，MMR λ=0.7。未配目录：现网 FTS 基线测试必须绿。机务关键词问 FTS 分不得被丢掉。

**验收：** D-E1–E3。  
**文件：** `gateway/openai_embed.go`、`m8_kb.go`、`kb_search_handlers.go`、memory 写入点。

### 5.F Key 备份 — P1

见 §4.8。D-F1–F2。先测 429 轮转，再改 lease。

### 5.P 能力路由 UI — P1

§4.6。六个下拉，空=自动。judge 勾选「允许与主模型相同」才允许 D-C1 例外。

### 5.Q 拖 id / ScrollPattern — P1 末

`drag` 可 `id` 起点。`scroll` 后允许再 observe（现网已可以，补测试即可）。不新工具名。

### 5.M 提权 — P1

UIPI/拒绝附加 → 现网 `ErrCcRiskBlocked` 映射 `user.ask` reason=uac。不点。D-M1。

### P2（默认关，本文只留门，不写实现步骤）

天气适配器、rerank、扫描件 OCR、文本 Guard。另批。

---

## 6. 分期与依赖（可拆 PR，禁止夹带）

```
P0-1 task_route 纯函数+测试          ──无依赖
P0-2 chat.go 收工具表                ← P0-1
P0-3 flash id Go 镜像                ──无依赖
P0-4 plan.verify 带 l0 + flash       ← P0-3
P0-5 observe truncated + 先 observe   ──无依赖（ccapp）
P0-6 无名 ID / Acc                   ← P0-5
P0-7 verifyAfter 自动 wait           ──无依赖（ccapp）
P0-8 media/type/browser L0           ──复用 desktop_open_verify
P0-9 user.ask reason + 登录墙         ──少碰 SessionPage
P0-10 CJK paste                      ──ccapp + desktop.type
P0-11 companion 回归 C7–C9 / D-H     ← P0-2、P0-5

P1-1 kind+schema 生成
P1-2 Provider 页签
P1-3 gui_fallback
P1-4 Embedder + 混合检索
P1-5 Key 备份
P1-6 capability.roles + 设置 UI
P1-7 drag id / UIPI ask
```

P0 可单独发布，不升大版本也可。P1 才出现 GUI/向量页签。

同一人不要同时改 `SessionPage.tsx` 与 `CompanionStage.tsx`。舞台未提交视觉改动禁止夹带。

---

## 7. 验收全表

先写失败测试再改。前缀 `D-`。现网 C7–C9 / C9-8 必须保持绿。

| ID | 阶段 | 断言 |
|---|---|---|
| D-A1 | P0 | 「北京明天天气」→R1，allow 无 desktop/browser/computer.act |
| D-A2 | P0 | 「打开汽水」→R2，allow 有 desktop.open；ccEnabled=false 时无 computer.act |
| D-A3 | P0 | 「打开浏览器查天气」→R1 |
| D-A4 | P0 | 「写一份半月财报到桌面」→R4，有 excel.gen/docx.gen，无 computer.act |
| D-A5 | P0 | 「你好」→R0，与 minimal 工具集相等 |
| D-A6 | P0 | 分类不得把 CC 打开；ccEnabled 只影响 allow |
| D-A7 | P0 | R1 allow **不含** kb.search（默认夹具） |
| D-B1 | P0 | media.play 前台无播放器窗 → ok:false |
| D-B1b | P0 | invoke==nil → 现网错误（CC 关），不装播放成功 |
| D-B2 | P0 | desktop.type 回读不含文本 → ok:false |
| D-B3 | P0 | browser click 在 MCP 未就绪或 stale → 不得 ok:true |
| D-B4 | P0 | action=list 不得 verified |
| D-B5 | P0 | 假前台 Lunitide 像素动作失败 |
| D-C1 | P0 | 夹具含 flash 时 verify model ≠ chat model |
| D-C2 | P0 | 无 l0 则 verified=false |
| D-C3 | P0 | 各步 L0.passed → 不调用验证 Complete |
| D-G1 | P0 | 登录墙 → user.ask reason=login，无 computer.act |
| D-G2 | P0 | UAC 现网回归 |
| D-G3 | P0 | 支付 URL → reason=pay |
| D-H1 | P0 | 月伴纯打开前台成功 settle，无 GUI/computer.act |
| D-H2 | P0 | 月伴工具表无 command.run / 裸 cc.* |
| D-I1 | P0 | 无 observe 的 click x,y → ok:false |
| D-I2 | P0 | 同轮 observe 后 click id=B1 走 rememberHits |
| D-I3 | P0 | AnnotateCapture 仍是唯一画手（不新增画号函数） |
| D-J1 | P0 | 两个 unnamed：name= 失败列 B1/B2；id=B2 成功 |
| D-J2 | P0 | Acc 无名观察后有 ID |
| D-J3 | P0 | InvokeUI("button (unnamed)") 不得当唯一成功 |
| D-K1 | P0 | 打满 maxNodes → truncated=true |
| D-K2 | P0 | 未满 → false |
| D-L1 | P0 | 突变后两帧哈希同（含自动 wait）→ ok:false |
| D-L2 | P0 | observe 后哈希同 → 仍 ok:true（现网回归） |
| D-N1 | P0 | CC 关：工具表无 computer.act；gui_fallback 为 none |
| D-O1 | P0 | 含汉字的 type → paste 分派 |
| D-D1 | P1 | ValidKind/NormalizeKind(gui|embedding) |
| D-D2 | P1 | CatalogForKind(gui) 不含 vision/llm；VisionDescribe 不含 gui |
| D-D3 | P1 | 页签文案 GUI 模型 / 向量模型 |
| D-D4 | P1 | 会话选择器仍只有 llm |
| D-D5 | P1 | R1 调 gui_fallback → none |
| D-D6 | P1 | desktop.type 已能命中 → 不进 gui_fallback |
| D-D7 | P1 | 无 gui 有 vision 有节点 → 只收 markId 字符串 ID |
| D-D8 | P1 | 无 gui 无 vision 或空树无 gui → 无法执行 + 观察 |
| D-D9 | P1 | 空树 + 有 gui → 允许归一化坐标且必须带 frameId |
| D-E1–E3 | P1 | 见上 |
| D-F1–F2 | P1 | 见上 |
| D-M1 | P1 | 提权附加失败 → ask/uac，无像素 |
| D-H3 | P1 | 若启用 flash 分类，只用 flash 绑定 |

**现网回归（P0 每 PR 必跑）：**  
`go test ./internal/app ./internal/toolruntime ./internal/ccapp ./internal/domain/provider`  
C7-6、C8-2、C9-8、companion UAC park、`openedWindowConfirmed` 汽水别名。  
Web：`modelKind` `userAsk` `approvalProfile` companion a11y。  
`npx tsc --noEmit`；改 schema 则 `npm run verify:bridge`。

---

## 8. 手工 20 分钟（P1 齐套；P0 做前 6 条即可）

1. 不配 GUI：查「北京明天天气」只出现 web.search，不开浏览器、不点桌面。  
2. 「打开汽水」前台是播放器，不说无法执行。  
3. CC 开：无名按钮先 observe 见角标，再点 B1；不 observe 就 xy 失败。  
4. 点完画面完全不变（点已选中的同一项）→ 失败或诚实不确定，不报「已完成」。  
5. 汉字填记事本走粘贴，框里能读回。  
6. 登录页 → 决策卡 reason 登录，不盲点。  
7. （P1）只配视觉：无名按钮读号能点。  
8. （P1）视觉也关、空树：失败带观察。  
9. （P1）GUI 页草稿指向 127.0.0.1，安装包体积不增。  
10. 主模型 429 + 备份 Key（P1）同轮能续。

---

## 9. 文件所有权（禁止超范围）

```
P0
  新  internal/app/task_route.go
  新  internal/app/flash_model.go          （与 TS 对齐的 flash 判定）
  改  internal/app/chat.go                 （只接 applyTaskRoute）
  改  internal/app/chat_plan.go
  改  internal/app/chat_tool_defs.go       （user.ask reason）
  改  internal/app/chat_tool_profile.go    （如需导出 allow 合并）
  改  internal/toolruntime/user_ask.go
  改  internal/toolruntime/desktop_open_verify.go
  改  internal/toolruntime/media.go media_foreground.go
  改  internal/toolruntime/runtime.go      （type 回读 + CJK）
  改  internal/ccapp/runhost.go service.go
  改  internal/ccapp/host_windows_ui.go host_windows_uia.go
  改  web/src/session/userAsk.ts
  回归 internal/ccapp 现网 screen unchanged
  回归 companion C7–C9

P1
  改  api/bridge/v1/public.dto.schema.json
  新  api/bridge/v1/capability.roles.get.schema.json
  新  api/bridge/v1/capability.roles.set.schema.json
  生成 bridge / contract / web generated
  改  internal/domain/provider/kind.go provider.go
  改  web/src/provider/modelKind.ts ProviderApp.tsx
  新  internal/app/gui_fallback.go
  新  internal/gateway/openai_embed.go
  改  internal/storage/sqlite/m8_kb.go + 新 migration 角色表/备份列
  改  internal/secretlease （仅 P1-5）
  新  web/src/settings/CapabilityRouting.tsx（并入现设置）
```

不碰：`CompanionStage.tsx` 视觉、`poison`、omni 安装器、VERSION、`v0.4.62` tag、`.release-cache`。

---

## 10. 风险与回滚

| 风险 | 落地对策 |
|---|---|
| 路由误收，用户真要开网页查 | 「打开浏览器查天气」走 R1；用户说「打开 Chrome 上 12306」走 R3（点名站点/Chrome+URL） |
| 自动 wait 拖慢点选 | 上限 700ms；L0.passed 的不 wait |
| schema 生成漏跑 | P1-1 独立 PR，Quality 的 schema 岗必须绿 |
| Key 池搞坏凭据 | P1 先备份列只读测试，再接线；失败回退只租 CredentialRef |
| 视觉读号乱点 | schema 只收 markId；越界失败；R1/R4 不调用 |
| P0 范围膨胀 | 上表以外的 kind/UI/向量一律拒收 |

单工作包不合：回退该包文件，不一次 revert 全 P0。

---

## 11. 批准

请确认：

1. 本文替换前稿为唯一事实源。  
2. P0 不配 GUI/向量/Key 池也能合。  
3. 画面不变先自动 wait 再失败（不是立刻失败）。  
4. SoM 读的是 `B1` 不是整数。  
5. 空树只能 GUI 给坐标，否则停。

确认后写 `docs/superpowers/plans/2026-09-04-daily-ops-capability.md`，按 §6 一条一测。未批准不改 `kind.go`、不接路由。
