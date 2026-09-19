# 月汐本职 9.5–10 分升级 PRD

日期：2026-09-19  
状态：`0.4.94` 已签名安装。本机场验大半绿（见 `2026-09-19-score-95-field-matrix.md`）。已装本职约 **9.0**，综合均权约 **8.7**。本职 9.5 **未到**：已装 Host 仍钉 Chat 迷你条（源码已隐藏）、打开记事本未专跑、Cursor Hub 新建要登录。P3 14 天 soak / 本机 CGO race 不能报 10。复核纠正仍有效：WP3 只对窗口溢出重试；WP8 不重复 reconcile、不自动解封 MCP。  
产品：月汐（Lunitide）Go Engine + Windows WebView2 Host + React renderer + SQLite  
对照报告：Cursor 画布 `lunitide-peer-10-score.canvas.tsx`（2026-09-19 十三维打分）  
现码基线：`VERSION` = `0.4.95`（源码已藏 Chat 迷你条；已装仍是 `0.4.94` Host `5acd8668…4140`，装新包前红框还在）  
冻结原则：**不换核、不扩面、不抄五家岗位。** 继续 ADR-001 三层；内核永远 SQLite；记忆晋升永远要人确认；Agent Hub 永远是 PATH 子代理，不是第二套大脑。

本文件是「把 13 维全部做到本职 9.5–10」的**唯一事实源**：尺子、每维 9.5/10 定义、工作包、文件、失败行为、验收、禁做项。画布里的 wave-1 `AFTER`（均权约 8.4、本职 8.8–9.2）是第一波可达值；**本 PRD 是收口程序**，每维按下面的本职定义验收到 ≥9.5。

---

## 0. 分类与目标

这是**架构级**升级：改的是主链路自救、压缩、调度回退、设置健康面、Hub 投影，不是加一个开关。

**对外一句话（产品清晰锁死，全文只许这句）：**  
月汐是这台 Windows 电脑上的本地优先办公助手——对话、工具、记忆、办公、语音、审批，在这一台机器上做完。

**分数目标：** 13 维全部达到**本职尺** 9.5；10 分是同一尺子上的场验年限，不另开功能清单。  
**禁止：** 用综合尺（跨平台 + 20 通道 + 无人自写技能）报 9.5–10。那把尺的 10 不是月汐的岗位。

### 0.1 三把尺子（继续禁止混报）

| 尺子 | 现码 | 本 PRD 结束后 | 10 分长什么样 | 本 PRD 是否追求这把尺的 10 |
|---|---|---|---|---|
| 综合尺（13 维均权，含跨岗位项） | ~6.7 | 约 8.6–8.9 | 面小、全绿、跨平台、自进化 | **否。禁止用这把尺对外报满分** |
| **本职尺（Windows 办公助手）** | 7.4 | **9.5** | 关窗不丢、点播放不崩、设置一眼见灯、长会话不爆窗、该动手时会动手 | **是。这是验收尺** |
| 核链路尺（装配/记忆/融合/压缩/调度） | 7.4 | **9.5** | 听懂该调什么；溢出/漏召会救当前步，不靠加词表 | **是。并入本职验收** |

### 0.2 本职 10 分总验收句（不要功能清单）

做完本 PRD，下面六句必须同时为真。任一句假，对应维不得报 9.5。

1. 点「设置」立刻看到绿 / 黄 / 红，总览取最差域；Chat / 月伴 / 主页不再出现「运行状态」。
2. 选本地文件能播；杀 Host 再开，引擎还在，不会因一张媒体票或误 `wantPlay` 把窗打死。
3. 文字聊一百轮还能接着干：超窗则检查点 + 重试**当前步**，源消息不删。
4. 「把它弄好」这类无动词目标也会动手：收面漏召则**放宽工具面再试一次**，不靠再加关键词。
5. 模型只带着**已确认**的长期记忆；整理、压缩前 flush 只产生提名，人点确认才注入。
6. 对外只说 §0 那一句；未配能力、未场验入口降级或隐藏，不装成功。

### 0.3 明确不做（抄了就把产品做散或把合同做坏）

| 禁抄 | 原因 |
|---|---|
| OpenClaw 公网 Gateway、20 通道、节点摄像头 | 岗位是本机 Windows，不是全球网关 |
| 把 Codex-rs 沙箱树搬进引擎 | Hub 继续 spawn PATH 上的 Codex/Cursor/Kimi；本机审批留在月汐 |
| Hermes 无人批准的 `skill_manage` / 自写 SKILL.md | 与「确认后才进长期记忆」对着干 |
| DeepSeek 式一切皆插件微核 / 13 个独立专家进程 | 拆掉现网 Engine 单体合同，本职不加分 |
| 未确认记忆写入系统指令 | 合同已在 `prepareChatMemory`，本 PRD 只加整理，不加私写 |
| 浏览器/媒体空成功、`MEDIA_UNVERIFIED` 说成已播放 | 现合同保留 |
| 月伴为了首字永远不压缩、长聊爆窗 | 热路径仍跳过 LLM 压缩；超水位走 §WP3 冷路径 |
| 用「嗯」假出字、用加词表冒充理解 | 漏召自救是放宽再试，不是 `infoQueryHints` 再加一行 |

---

## 1. 13 维：现码 → 本职 9.5 / 10 定义 → 工作包

现分来自 2026-09-19 现码审阅（画布 `LUNI`），不是装包 soak。  
**9.5** = 本职定义下的合同 + 单测 + 本机场验清单全绿。  
**10** = 同一合同在真实日用里不再回归；本 PRD 交付 9.5，10 靠 P3 场验窗，不靠新子系统。

| # | 维度 | 现码 | 本职 9.5 定义（验收语言） | 本职 10 | 工作包 |
|---|---|---|---|---|---|
| 1 | 架构底座 | 7.6 | Host 崩不影响 Engine；Engine 崩 Host 能拉起；健康合同可查询；媒体 COM 无 UAF | 同上合同 soak ≥14 天无回归 | WP0, WP1 |
| 2 | 主链路 | 7.2 | Chat / 月伴 / Hub 投影共用 `message.append`→`chat.start`→`turnEquipmentFor`；Hub 不另起大脑 | 中途换模型不拆会话 | WP2, WP5 |
| 3 | 事务处理 | 7.0 | 工具/OCR/媒体三域灯；失败可恢复；空成功禁止；播放/打开有 `verified` | 真机恢复面无「看起来成功」 | WP1, WP6 |
| 4 | 记忆 | 7.4 | 压缩前 flush 只提名；定时整理只提名；注入仍 confirm-only；专家 gist 不重复提名 | 整理环日用可见、不漏确认 | WP4 |
| 5 | 模型融合 | 7.6 | 全部入口走 `chat/flash/vision/embed/judge/gui`；Hub/BYOA 同一适配器；会话内可换模型 | 换模型后压缩检查点仍可用 | WP5 |
| 6 | 省 Token / 压缩 | 7.0 | 水位压缩 + 溢出重试当前步；权威指令冻结吃前缀缓存；月伴超水位有冷路径 | 溢出重试成功率场验 ≥95% | WP3 |
| 7 | 上下文理解 | 7.2 | 七层信封不丢权威层；检查点带 epoch；长聊月伴不靠「永远浅窗」装懂 | 百轮后约束/待办仍在信封 | WP3, WP4 |
| 8 | 语义调度 | 7.2 | 漏召：放宽面再试 1 次；误收面不靠加词；技能仍目录+`skill.view`；MCP 仍 12 直挂否则网关 | 无动词目标场验会动手 | WP6 |
| 9 | 功能可用 | 5.6 | 看见的入口能用或灰掉并写原因；媒体播放场验过；未配能力不装成功 | 办公菜单项场验矩阵全绿 | WP0, WP7 |
| 10 | 稳定健壮 | 6.2 | COM Complete+Release 在 UI wait 内；`wantPlay` 门闩；race 进 CI；关窗任务跟引擎 | 无 Host 自杀、无启动即崩 | WP0, WP1 |
| 11 | 高集成 | 7.0 | 一个运行时：装备层 + 入口层合成；Work→Chat、Hub→同一会话投影 | 用户感觉不到第二套产品 | WP2, WP5 |
| 12 | 自我净化 | 5.0 | 灯 + 失败对账自动再跑 + 记忆整理 + MCP 检疫 + 压缩 Recover，全部人看得见 | 无人值守只整理不改权威 | WP1, WP4, WP8 |
| 13 | 产品清晰 | 5.6 | 文案只许 §0 一句；未完成入口隐藏/降级；设置即健康 | 新用户 10 秒说得出月汐是什么 | WP7 |

Wave-1 只把多数维推到 8.2–8.8、净化 7.6。本表把缺口补到 9.5：**净化可见且自动（仍不写权威）、Hub 投影、前缀冻结、能力门闩导航、场验矩阵。**

---

## 2. 现码锚点（改这些，不新开平台）

| 层 | 现码事实 | 本 PRD 怎么用 |
|---|---|---|
| 生产架构 | ADR-001：Engine / Host / renderer | 不改三层 |
| 长上下文 | ADR-005 七层信封 + 检查点；`TriggerPreTurnCompaction` | 补溢出重试；月伴热路径仍跳过 LLM 压缩 |
| 装备 | `turnEquipmentFor`（`chat_turn_equipment.go`） | Hub 投影也走这里 |
| 收面 | `classifyTaskRoute` R0–R4；未匹配不收面 | 漏召回退到 `RouteUnspecified` 再试 1 次 |
| 月伴工具门 | `companionWantsTools` | 保留闲聊不带工具；漏召走 WP6，不加词 |
| 技能 | `skillCatalogInjection` + `skill.view` / `skill.invoke` | 已对齐 Hermes 目录层，不改成整包塞 |
| MCP | `mcpDirectToolCap = 12`，否则 `mcp.search` / `mcp.call` | 保持 |
| 记忆 | `prepareChatMemory`；`memory_nominations`；confirm 才注入 | 只加 flush / 定时整理 |
| 车道 | `applyLaneTools` L0–L2 | 放宽再试时允许升到未指定 |
| 完成因 | `llmadapter.FinishReasonLength` 已归一 | 溢出重试挂这里 |
| 活动 | `ActivitySnapshotDTO` 域 tool/ocr/media；前台按钮仍在 `MediaRuntime` | 搬到设置灯 |
| 媒体崩溃 | 工作树：`deliverMediaResponse` wait 内 Complete+Release；`needsPlaybackOpen` 看 `wantPlay` | WP0 入库并装包（须另令） |
| Hub | `agenthub_handlers.go` harness 仅 loopback/cursor/kimi/codex | 不增 harness；投影进 Chat 会话 |

---

## 3. 工作包

每个工作包必须独立可测、可回滚。禁止「顺便重构 Engine」。

### WP0 · 场验止血（稳定 6.2→9.5 的必要前提）

**问题：** 已装 0.4.93 选本地媒体播放可杀 Host；再开会因误 `wantPlay` / 自动挂 src 再崩。引擎往往还在。

**锁死：**

- 媒体 `WebResourceRequested` 用 `MediaResourceContextAll`；`ICoreWebView2Deferral` 的 Complete 与 COM Release **必须在 UI 线程 wait 返回之前**完成。禁止 `dispatch` 出去后立刻 Release。
- `needsPlaybackOpen` / `MediaRuntime` / `OwnedMediaPlayer` 只有 `wantPlay === true` 才挂播放 URL 或调 `play()`。
- 启动恢复不得把上次 `playing` 当成本访要播。
- 本包**不**改产品版本号、不签名、不推 GitHub，除非用户另下一句「发 0.4.94」。

**已有改动（工作树，视为本包实现稿，入库前只许修测试）：**

- `internal/webviewhost/media_origin.go`
- `internal/webviewhost/media_origin_test.go`
- `internal/webviewhost/media_resource_windows.go`
- `internal/webviewhost/host_windows.go`（`closeSTA` 摘 ALL filter）
- `web/src/media/mediaSnapshot.ts`
- `web/src/media/MediaRuntime.tsx`
- `web/src/media/OwnedMediaPlayer.tsx`

**验收：**

- `TestDeliverMediaResponse`：`[release]` 不得出现在 wait 结束前。
- 本机：选 `ringing_short.mp3` → 播 → 停 → 再播；杀 `lunitide.exe` 再开，不自动出声、不秒退。
- 未装含本修复的 Host 之前，**稳定维不得报 9.5**。

---

### WP1 · 设置运行状态三色灯（事务 + 净化 + 清晰）

**问题：** 「运行状态」在 Chat/主页抢注意力；`activityNeedsAttention` 把 running 当成要处理。

**锁死（沿用已批准的灯语义，本包才许改代码）：**

- 从 `MediaRuntime` **删除** `ActivityStatusButton` / `ActivityCenter`。Chat、月伴、媒体中心、Agent Hub 前台都不再出现「运行状态」。
- 设置左轨：在「返回」下方、搜索上方，**钉死** `SettingsRunStatus`。点侧栏「设置」默认 `general`，灯已经在左轨，不必先找诊断页。
- **不**给侧栏「设置」按钮本身加灯。
- 总览一盏 + tool / ocr / media 三盏。
- 绿：该域无 `failed`、无 `uncertain`。`queued` / `running` / `verifying` 仍绿。
- 黄：有 `uncertain` 且无 `failed`。
- 红：任一 `failed`。
- 未使用的域 = 绿（不是灰、不是灭）。`off` 只表示「本机构造里没有这个域」，当前三域都有，默认不当 off。
- 总览 = 三域最差（红 > 黄 > 绿）。
- 黄/红可就地展开恢复动作（沿用现 Activity 恢复入口），不新开第三套中心。
- 诊断页可重复同一组灯，不是唯一入口。

**文件：**

- 新增 `web/src/settings/SettingsRunStatus.tsx`（或同等名）
- `web/src/settings/SettingsPage.tsx`、`web/src/settings/settingsNav.ts`（不必新 category）
- `web/src/activity/activitySnapshot.ts`：新增 `lampTone(domain)`，**不要**复用 `activityNeedsAttention` 当灯
- `web/src/media/MediaRuntime.tsx`：去掉前台状态按钮
- 改测：`web/src/App.r3-ui.test.tsx`、`web/src/accessibility/r3_accessibility.test.tsx`、`ActivityStatusButton.test.tsx` —— 断言从「主界面有运行状态」改为「设置左轨有灯、主界面没有」

**验收：**

- 无活动：四盏全绿。
- 仅 running：仍绿。
- 媒体 `uncertain`：媒体黄、总览黄。
- 工具 `failed`：工具红、总览红。
- 打开设置 < 1s 能看见灯（不经过搜索、不经过诊断）。

---

### WP2 · 一条河：Hub / 月伴投影进同一会话模型（主链路 + 高集成）

**问题：** 主干是 `message.append` + `chat.start`。月伴抢跑且跳过压缩；Hub 是另一套 thread/harness。

**锁死：**

- **不**把 Hub 重写成 Chat。Harness 仍只允许 `loopback | cursor | kimi | codex`。
- 每个 Hub thread 必须绑定一个普通 `session_id`（已有则复用，没有则创建）。Hub 的用户可见文字、工具摘要、失败原因，作为该会话的消息投影（只读卡片可留在 Hub 页）。
- Hub 的「再试 / 继续」走同一 `chat.start` 装备层，禁止第二套 tool loop。
- 月伴：身份/语音约束仍只在 `chat_companion_speech.go` 覆盖层。装备、记忆、MCP/技能目录仍走 `turnEquipmentFor` / `prepareChatMemory` / `skillCatalogInjection`。
- 中途换模型：同一 `session_id`，失效旧 tokenizer 计数，下一轮按新窗口做水位检查。禁止「换模型 = 新会话」。

**文件：**

- `internal/app/agenthub_handlers.go` 及 `agenthub_handlers_test.go`
- `internal/app/chat.go`（session 绑定，不把 companion 特例扩到 Hub）
- `internal/app/chat_turn_equipment.go`
- Hub UI 只加「在对话中打开」；不把 Chat 做成 Hub。

**验收：**

- 单测：`agentHub.thread.create` 返回非空 `sessionId`；对该 session `message.list` 看得到 harness 终态摘要。
- 换模型后 `session_id` 不变，压缩检查点仍按源消息区间有效。
- 月伴闲聊仍不带工具（`companionWantsTools` 假）；月伴任务仍走装备。

---

### WP3 · 溢出则检查点并重试当前步（压缩 7.0→9.5 + 上下文理解）

**问题：** 水位压缩在文字聊已有；供应商仍可能回 `FinishReasonLength` / context window exceeded。月伴为 TTFT 整段跳过压缩。权威指令每轮重排会打掉前缀缓存。

**锁死：**

1. **溢出重试（OpenCode V2 语义，自研实现，不引进 OpenCode 运行时）**  
   - 触发：适配器归一后 `FinishReasonLength`，或错误分类为上下文溢出（须在 `llmadapter` 加**封闭**错误类，禁止用英文子串满天飞）。  
   - 动作：对**当前 session** 同步跑现成 `compactionExecutor`（生成候选 → 校验 → CAS 激活）。源 `messages` **不删不改**。  
   - 然后用**同一条用户消息、同一装备、同一工具面**重试当前步，最多 **1** 次。  
   - 仍溢出：fail-closed，用户可见「上下文已满，请开新话题或先压缩」，不得截断装成功。  
   - 检查点必须带 `epoch`（单调整数，会话内 +1），信封选中检查点时写入 selection trace。

2. **压缩前 memory flush（OpenClaw 语义，合同仍是月汐的）**  
   - 在生成摘要**之前**，对即将被检查点覆盖的区间跑候选抽取 → `memory.nominate`。  
   - 不自动 `confirm`。未确认条目不得进 `prepareChatMemory` 注入集。

3. **月伴压缩冷路径**  
   - 热路径：`p.Companion == true` 仍跳过 LLM `TriggerPreTurnCompaction`（现注释保留：压缩会吃掉 TTFT）。  
   - 超高水位：复用**上一个已激活检查点**（若有）+ 收紧 `MaxMessages`；本轮不新开 LLM 压缩。  
   - 本轮结束后（流结束、用户已听到收尾）：异步跑压缩 + flush。下一轮月伴用新检查点。  
   - 若无检查点且已超窗：口头一句「这段我先记下，紧接着继续」，然后异步压缩；禁止静默丢约束。

4. **权威指令冻结（Hermes 缓存语义）**  
   - `AssembleEnvelope` 第 1 层（系统/安全/产品指令）字节在「确认集 / 人设 / 安全策略」不变时必须稳定。  
   - 禁止为省 token 把动态任务指令提升到第 1 层。  
   - 确认集变更才允许第 1 层变化（已有 token efficiency 前缀缓存继续用）。

**文件：**

- `internal/app/chat.go`、`internal/app/chat_run_stream.go`、`internal/app/engine.go`
- `internal/compactionapp/*`（epoch 字段、Recover 已有则复用）
- `internal/contextapp/assemble_envelope.go`
- `internal/llmadapter/completion_finish.go` + 新溢出错误类型
- `internal/app/chat_memory.go`（flush 调用点）
- 测试：`internal/app` 溢出重试；`contextapp` epoch；companion 热路径仍 0 次 LLM 压缩、冷路径 1 次

**验收：**

- 人造超窗：第一次 length → 检查点激活 → 第二次同用户消息成功；`messages` 行数不减。
- 第二次仍 length → 明确失败码，无助手假完成。
- companion 单测：首轮 `TriggerPreTurnCompaction` 调用次数为 0。
- 第 1 层 hash：只改最近用户消息时保持不变。

---

### WP4 · 记忆整理环（记忆 7.4→9.5 + 净化）

**问题：** nominate/confirm 合同在，缺「压缩前必 flush」和「空闲时整理」。不实施 2026-09-14 Memory Fabric v2 全文（那是另一份未开工的大 PRD）。本包只取让本职记忆到 9.5 的薄切片。

**锁死：**

- 新增引擎内 `MemoryHygiene` 任务（可用现有 scheduler / idle 钩子，禁止新进程）：  
  - 触发：设置打开时若距上次 > 6h；或引擎空闲 30min；或压缩即将跑。  
  - 动作：去重提名、撤回过期 nominated、把重复专家 gist 压成一条提名。  
  - **永不** `confirm`。  
- 设置「智能能力 / 记忆」列出待确认提名（已有则接上，没有就用现 `memory.nomination.list`）。
- `auto_nominate` 默认保持现库默认；本包不改成自动写入长期记忆。
- 不引入 Mem0/Graphiti/Letta 运行时。

**文件：**

- `internal/app/chat_memory.go`
- `internal/m8app` nomination 服务
- `internal/app/m10_nomination_handlers.go`（若已有 list/confirm，只加 hygiene RPC）
- 设置记忆页只读列表 + 确认/撤回按钮

**验收：**

- 同一 gist 连续两轮不会产生两条 `nominated`。
- hygiene 跑完，`confirmed` 行数不变。
- 压缩前 flush 至少产生 0 条提名（无候选也成功），不得阻塞压缩。

---

### WP5 · 能力位打穿（融合 7.6→9.5）

**问题：** 能力位 `chat/flash/vision/embed/judge/gui` 已在路由页；Hub / 部分办公入口仍可能绕开。

**锁死：**

- `chat.start`、月伴、会议摘要、办公生成、Hub 投影，选模型只通过现有 capability 解析。未配置的能力：入口灰掉，文案「未配置 ×× 能力」，禁止进一半失败。
- BYOA / 自定义 endpoint：必须登记能力位，才能出现在该入口。
- `judge` 本 PRD **只**用于溢出后「是否值得重试」的内部开关（可先规则：length → 值得）。**禁止**用 judge 模型做路由关键词替代（那是另一项研究，不加分）。
- 不新增第四家 Hub harness。

**文件：**

- `internal/app` 能力检查已有 `CheckCapability` —— 入口补齐
- `web/src` 办公菜单 / Hub / 会议入口：未配置则 disabled + 原因
- 测试：关掉 vision 后会议截图入口不可点

**验收：**

- 未配 `gui` 时电脑控制入口不可点。
- 未配 `chat` 时 `chat.start` 返回既有能力错误，不空转。

---

### WP6 · 漏召放宽再试（语义调度 7.2→9.5）

**问题：** `classifyTaskRoute` / `companionWantsTools` / `ConversationExpertsMatchingIntent` 是词面。无动词目标（「把这个弄好」）会收成空面或闲聊。加词表会永远落后。

**锁死：**

- 新增 `widenAndRetry`，**每用户轮最多 1 次**。
- 触发（任一）：  
  1. 本轮 `TaskRoute != ""`（已收面）且模型 **0 次** 成功工具调用，且助手文本像拒绝/做不到/「我没有这个工具」；或  
  2. 用户上一轮是工具目标、本轮是短催促（「你没做」「继续」「再试」）且上一轮 0 工具。  
- 动作：丢掉本轮收面，按 `RouteUnspecified` 重装 `turnEquipmentFor` + `applyLaneTools`，**同一用户消息**再跑当前步。已产生的助手碎句作废，不得留给用户两段矛盾话。
- **不**对纯问候（「你好」「在吗」）触发。
- **不**新增 `infoQueryHints` 行来「修」漏召。
- 技能：仍先目录后 `skill.view`。放宽再试不得改成整包 SKILL.md。
- MCP：仍 12 直挂否则网关。放宽再试不得把未 ready 的 MCP 直接塞进模型。

**文件：**

- `internal/app/task_route.go`（只加「是否像催促」的小函数，不加业务词）
- `internal/app/chat.go` / `chat_run_stream.go`（重试编排）
- `internal/app/chat_companion_speech.go`：月伴漏召同样 1 次；闲聊门闩保持
- 测试：收成 R1 的「把桌面上的报告弄好」0 工具 → 第二次带 office/desktop 工具；「你好」不重试

**验收：**

- 单测表格：问候 / 天气 / 无动词办公 / 催促，触发次数分别为 0 / 0 / 1 / 1。
- 二次仍 0 工具：正常结束，不第三次。

---

### WP7 · 看见的都能用（可用 5.6→9.5 + 清晰 5.6→9.5）

**问题：** 面大于场验；未配能力的入口仍可点进失败。

**锁死：**

- 办公菜单、侧栏、Hub：能力或 harness 不可用则 **隐藏或 disabled**，禁用理由一句话。禁止进页再「核心引擎暂时不可用」当主路径。
- 文案审计：设置关于页、空状态、README 首段，产品句只许 §0。删除「办公 OS / 全能工作台」并列主语。
- 未场验表面（本 PRD 不新做）：保持隐藏或「实验」标记，**不得**算进 9.5 可用维。
- 可用维 9.5 的场验矩阵（Windows x64，本机）：

| 表面 | 必须绿 |
|---|---|
| 文字 Chat 一轮 | 出字、可停 |
| 月伴问候 | 立刻出声，不带工具 |
| 月伴「打开记事本」 | `desktop.open` 验窗成功才说打开 |
| 媒体中心选本地音频播放/暂停 | Host 不退 |
| 设置灯 | 点设置即见 |
| 记忆确认 | 未确认不出现在下一轮系统注入 |
| 未配电脑控制 | 入口不可用 |
| Hub loopback | 能建 thread，投影进 session |

**文件：** 导航 / 办公菜单显隐（已有 office-menu 设置则复用）、文案字符串、上述场验手册 `docs/superpowers/specs/2026-09-19-score-95-field-matrix.md`（批准本 PRD 后与实施计划一起写，不提前空文件）。

---

### WP8 · 失败对账自动再跑（净化 5.0→9.5）

**问题：** `diagnostics`、audit、MCP quarantined、`compaction Recover`、`agent.run.reconcile` 已在引擎里，失败常常只写日志。

**锁死：**

- 引擎启动后与每次设置打开：跑一次 **只读扫描** + 对**已知幂等**恢复项自动再跑：  
  - `run/recovery.go` 扫描与 quarantine  
  - compaction Recover（已有则调，没有禁止新写一套）  
  - MCP 仍 quarantined 的不得自动解封  
- 扫描结果写入活动快照的系统域或复用 diagnostics，**反映到 WP1 的灯**（红 = 有未恢复失败；黄 = 已 quarantine 待人处理；绿 = 扫描通过）。
- 自动再跑必须幂等。禁止自动删用户数据、禁止自动 confirm 记忆、禁止自动重装 MCP。

**文件：**

- `internal/run/recovery.go`
- `internal/app` 启动钩子
- 灯数据源与 `ActivitySnapshotDTO` 对齐（可增 `diagnostics` 域；若增域，总览仍取最差；未使用=绿）

**验收：**

- 损坏 run chain：quarantine 一次，第二次扫描不重复制造事件。
- 灯：quarantine 存在 → 非绿。

---

## 4. 分期与依赖

| 期 | 工作包 | 本职分数怎么动 | 依赖 |
|---|---|---|---|
| **P0 止血** | WP0 入库（另令才签名发版）+ WP1 灯 + WP7 文案/门闩 | 稳定 ≥9.5 的合同侧；可用 ≥8.5；清晰 ≥8.5。未装新 Host 则稳定场验仍红 | 用户批准本 PRD；发版另令 |
| **P1 一条河** | WP3 溢出重试 + flush + 月伴冷路径 + 指令冻结；WP6 放宽再试；WP2 Hub 投影 | 压缩/理解/调度/链路 ≥9.5 | P0 灯不堵 P1，可并行；溢出重试不依赖灯 |
| **P2 自净** | WP4 整理环；WP5 能力打穿；WP8 对账再跑 | 记忆/融合/净化/集成 ≥9.5 | WP3 flush 与 WP4 共享 nominate；WP8 依赖 WP1 灯 |
| **P3 场验窗** | 场验矩阵 14 天；race 进 CI；关窗 Engine 仍活的 watchdog 核对 | 各维从 9.5 → 趋近 10 | 须已装含 WP0 的 Host |

并行规则：WP0 与 WP1 可同一迭代；WP3 与 WP6 可并行（都改 `chat.go` 时由同一计划串行提交，避免双写 tool loop）。  
**本 PRD 批准 ≠ 实施批准。** 实施前按子系统拆计划：`docs/superpowers/plans/2026-09-19-p0-field-health.md` 等，一事一计划。

---

## 5. 全局约束（写进每一份实施计划的 Global Constraints）

- 平台：Windows x64；WebView2 Host；不在本程序做 macOS/Linux 宿主。
- 栈：Go Engine + SQLite + 命名管道 + DPAPI（Host）/ leases（Engine）。
- 版本：未听到「发版 / 签名 / 0.4.94」不得改 `VERSION`、不得打 NSIS、不得推 Latest。
- 记忆：未确认不得进系统指令。
- 技能：目录元信息 + `skill.view`；禁止无人自写技能。
- Hub：harness 白名单不扩大。
- 调度：禁止靠加 `task_route.go` 词表宣称「理解提升」。
- 测试：先失败测试再改；不跳过 hooks；Windows 宿主测试要 `all` 权限时按现网习惯。
- CGO race：P3 在 CI 或文档化的本机 job 跑，不再「只在注释里遗憾」。
- 不覆盖已发 digest：`v0.4.92` 及更早 GitHub 对象不得删。

---

## 6. 风险与诚实边界

| 风险 | 处理 |
|---|---|
| 综合尺永远到不了 10 | 对外只报本职尺。综合 10 需要跨平台与自进化，本 PRD 拒绝 |
| 放宽再试增加一次模型调用 | 每轮最多 1 次；问候不触发；比加词表便宜且可测 |
| 月伴异步压缩与下一轮竞态 | 检查点 CAS 已有；冷路径只在流结束后启动 |
| Hub 投影被理解成「Hub 废了」 | Hub 页保留；只是 thread 必须有 session |
| 9.5 ≠ 装包 100% | 未安装含 WP0 的 Host，稳定/可用场验不得报完成 |
| Memory Fabric v2 全文 | 不在范围；WP4 是薄切片。以后要做另开 PRD |
| 设置灯设计已在对话批准、代码未动 | WP1 是第一份改 UI 的包；未再批准不得改前台 |

---

## 7. 自检

| 检查 | 结果 |
|---|---|
| 13 维是否都有 9.5 定义和工作包 | 是，§1 表 |
| 是否用综合 10 冒充目标 | 否，§0.1 |
| 是否要求抄 Gateway / Codex 核 / 自写技能 | 否，§0.3 |
| 文件是否落到现码 | 是，§2–§3 |
| 占位符 TBD/「适当处理」 | 无 |
| 与未入库崩溃修复的关系 | WP0 明确 |
| 与设置灯设计的关系 | WP1 明确 |
| 是否授权现在写代码 | 用户已令执行落地；实施按本文件纠正后的包推进 |

---

## 8. 批准后怎么拆计划

不要用一份 200 步计划覆盖四个子系统。批准后按写作计划技能各出一份：

1. P0：WP0（若尚未 commit）+ WP1 + WP7 门闩/文案  
2. P1：WP3 + WP6（必要时 WP2 后置若要减冲突）  
3. P2：WP2（若未进 P1）+ WP4 + WP5 + WP8  
4. P3：场验手册 + race CI  

每份计划必须引用本文件路径，Global Constraints 原文抄 §5。
