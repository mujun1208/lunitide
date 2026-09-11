# Lunitide 产品健康诊断（准确性 / 可靠性 / 速度效率 / 稳定性）

- **日期**：2026-09-11（修订：纳入用户本机未提交 WIP 与本地测试日志）
- **已提交基线**：`feat/prd-v7-s1-continuity` @ `1326fb24`（`VERSION` = **0.4.75**）。云端克隆与本报告源码引用以此为准。
- **用户本机分叉（本克隆不可见）**：同一 commit 上约 **21 个未提交文件（+441/−112）**。业主描述含：Office Studio 导航/引用、`session_artifacts` 绑定 `officeTaskContextID`、Mermaid 引擎路径（`SETTLE` 480→280ms、去掉 `RETRIES=3`、`loadMermaidEngine` 直接渲染）、`internal/diagramrender/handler.go` Timeout 8s→20s、连续性测试。**只当「本机 WIP 声明」写入，不当已合入事实。**
- **范围**：Go Core Engine + Windows WebView2 Host + `web/` React Renderer；不改运行时代码
- **权威已发布说明**：`release/notes-0.4.75.md` 对抖动 / Mermaid 等待 / 专家误装 / 周报试运行序列的口径作准
- **方法**：对照 README、ADR、`docs/implementation/P0-P1-status.md`、`docs/system-analysis-report-2026-09-06.md`、`docs/audits/2026-09-08*` / `2026-09-09*`，独立阅读关键链路；**不把旧审计的 P0 清单当仍未修**
- **不在范围内**：安全渗透清单、文风 nit、付费 live 模型评测；本云环境未跑 120 分钟 `go test -race`，也未跑 `Test-Install.ps1`（业主本机因已有安装被脚本拒绝）

---

## 执行摘要

- **0.4.75 是一批「用户能看见的失败模式」补丁，不是架构转折。** 回合后滚动抖动、Mermaid 永久等待、剧本轮误装航空维修、周报试运行 `CONTEXT_SEQUENCE_INVALID`——这四条在代码与测试里都能对上，属于**真修**而不是只改文案。
- **当前最大产品风险仍是准确性，不是崩溃。** 专家装备、任务路由、办公产物、上下文续轮都建立在多层关键词启发式上。测试能锁住已知误装，锁不住「用户以为挂了两位专家」或「三次 nudge 后生成空 PPT」。
- **可靠性的骨架是硬的（IPC 认证、SQLite WAL、截断不造假文件），边角是软的。** JSON sidecar 写失败被丢掉、工具回执 `_ = FinishToolOperation`、Host 事件队列满即杀流——用户会感到「刚才那一下丢了」，日志里几乎没有收据。
- **速度痛点不在「再少等 50ms 的 token」，而在冷启动与被挡住的首字。** Engine 必须跑完 `WireEngine`（152 条迁移 + 一串 reconcile）才听 Named Pipe；桌面/媒体/月伴工具轮还把模型正文缓冲到工具证据之后。0.4.75 修的是**回合结束后的视觉抖动**，不是 Time-to-first-token。
- **稳定性目前主要被 CI 预算和测试替身「买下来」。** `windows-cgo-race` 从 90 分钟抬到 120 分钟；语音 flake 改用 fake timers；`cmd/desktop` 因 PE relocation 被踢出 `-race`。这能让 Quality 绿，但不能证明长会话 Host/Engine 在真机上不抖。
- **S1 连续性（0.4.74）落地了检查点与 native replay，但「继续」仍会整包回灌 PPT/DOCX 工作流。** 用户改口做别的事时，模型可能接着写上一份幻灯片。
- **`computer.act` 已产品化，但合同明确不承诺「任意 Windows 应用一次成功」。** 指名 `observe`→`name=`/`id=` 是对的；Invoke 失败仍点像素。现场验收只能当抽样，不能当全桌面 SLA。
- **2026-09-06 报告里的两项安全 P0（身份私钥明文、`command.run` 继承全环境）在后续同文档登记为 S-01/S-02 已落地。** 当前树能对上 DPAPI sentinel（`internal/storage/sqlite/identity.go`）和环境白名单（`internal/toolruntime/command_env.go`）。不要再当未修 P0 排期；残留是非 Windows/无 secret store 仍写明文、白名单仍含 `APPDATA`/`USERPROFILE` 等宽项。
- **P0/P1 发布门仍是有条件关闭。** `docs/implementation/P0-P1-status.md`：Windows CGO `-race`、一次性 Win10/11 安装生命周期、Authenticode 仍标外部证据。业主本机未跑 120 分钟 race；`Test-Install.ps1` 因机器已有安装被拒。本云克隆未见单独的 `P0-P1-CLOSEOUT.md`。
- **本机测试日志不能替代发布证据。** `test_all.log` 多为缓存绿；`vitest-all` 74 文件 / 583 项约 26s（远小于 notes 的 288/2176）；`.tts-test.log` **仅 4/21 通过**——语音 flake 在本机仍在。覆盖率闸 58.8% ≥ 51% 与 0.4.75 notes 一致。
- **本机未提交 WIP 会改准确性和稳定性权衡。** 办公产物绑 `officeTaskContextID` 有利于对话卡打开正确任务；Mermaid 若改为主页面 `loadMermaidEngine` 直接渲染，可能回退 B06 隔离 worker。Timeout 8s→20s 让卡死更久。合入前要单独审，不要和 0.4.75 已发布行为混谈。
- **单体正在长大。** `runStream` 1563 行（2026-09-06 时尚记 1138 / gocyclo 332+）。F-06/F-07 仍是设计文档。
- **给一人公司的取舍：** 先修「用户看到的错」，再决定是否合入本机 Mermaid/Office WIP，最后才拆 god-object。不要并行开第三条大重构。

---

## 1. 准确性

### 1.1 界面挂了多名专家，引擎本轮可能一个都不装备

- **现象**：Composer 用 `session.experts.set` 可挂 2+ 专家并显示 chips。若本轮没有 `[引用专家 …]`，引擎对「多名已挂载」返回空集合，本轮只靠意图启发式（或什么都不装）。
- **证据**：`selectedTurnExpertIDs` 仅在 `len(mounted)==1` 时使用会话挂载，否则 `return nil`（`internal/app/chat_expert_council.go`）。`turnEquipmentFor` 直接吃这个结果（`internal/app/chat_turn_equipment.go`）。测试把「两挂载且无 @ 则不启动 council」写成合同（`TestSelectedTurnExpertIDsUsesMountedSubsetOnly`）。前端 `persistMounted` 仍会把多名专家写入会话（`web/src/session/SessionPage.tsx`）。
- **严重度**：**P1**
- **影响面**：普通对话与项目管理的专家 chips、装备条、技能/MCP 绑定、用户对「我已经选了专家」的信任。
- **建议方向**：把 UI 合同写成一种：要么「挂载 = 本轮装备全集」，要么「多名挂载必须再点名，且界面明确说本轮未装备」。不要让 chips 亮着、引擎空转。0.4.75 修的是「弱匹配挤掉已装备专家 / 剧本误装机务」，没有修多挂载语义。

### 1.2 办公产物可被 nudge 次数解锁，正文启发式也能落盘

- **现象**：PPT/Word 流水线在「研究未必充分」时，靠 `PptNudges >= 3` / `DocxNudges >= 3` 或 `pptPipelineReady`（1 次成功 web + 3 次 nudge）放行生成。流结束后 `tryFinishOfficeGen` 还可把助手散文塞进 `*.gen`。
- **证据**：`pptPipelineReady` / `shouldAutoOfficeGen`（`internal/app/chat_ppt_workflow.go`、`chat_office_autogen.go`）；`runStream` 终态调用 `tryFinishOfficeGen`（`chat_run_stream.go`）。0.4.75 补了「周报试运行仍走 `docx.gen`」（`TestTrialWeeklyReportStillMapsToDocxAndIsNotBlocked`），这是**路径**修复，不是**质量门**。
- **严重度**：**P1**
- **影响面**：报告 / 小说 / PPT / 周报试运行；用户拿到结构合法、事实稀薄或格式错位的文件，产品却显示「已完成」。
- **建议方向**：把「能生成」和「该生成」拆开。解锁条件改为可核对的材料收据（已读来源、最低字数/章节、用户明确格式），而不是计数器。自动散文落盘只在用户明确要求文件、且 `officeContentUsable` 之上再加任务合同校验。

### 1.3 长会话上下文有两条真相：折叠历史 vs native 续轮

- **现象**：持久化 `role:tool` 没有 `tool_call_id`。无 native replay 时折成 user 备注；有 S1 native complete 时走 codec 回放。0.4.75 用 `sanitizeProviderReplay` 丢掉中途 system、合并连续 assistant，序列再坏则 `useExplicitChatFallback`。回退成功时模型可能只看见本轮用户话，丢掉工具证据。
- **证据**：`combineProviderMessages` / `sanitizeProviderReplay` / `useExplicitChatFallback` / `isProviderSequenceError`（`internal/app/chat.go`）；`nativeReplayMessages` 读 group 与 private blob 各 3s，失败则静默 `nil`（`internal/app/chat_continuation.go`）。release notes 写明这是为避开 GLM/DeepSeek `CONTEXT_SEQUENCE_INVALID`。
- **严重度**：**P1**
- **影响面**：月伴长会话、工具密集轮、换供应商后续轮、S1 resume。用户表现为「刚才查过/写过的文件模型当没发生」。
- **建议方向**：把「序列消毒」当兼容层，把「工具结果可回放」当主路径。统计 fallback 命中率；native 加载失败应对用户可见（续轮降级），不要只打日志。消息存储最终需要能原样回放 tool 对，而不是永远折成 user note。

### 1.4 模型拒绝工具 schema 时整轮变纯聊天

- **现象**：非流式 HTTP 400 且带工具时，引擎剥掉 `req.Tools`，插入中文系统提示，再以纯对话重试。模型可以自信作答且不再能读文件/跑命令/MCP。
- **证据**：`chat_run_stream.go` 工具 400 降级分支（`toolsFallbackUsed`）。注释承认这是兼容端点的绷带。
- **严重度**：**P1**
- **影响面**：部分 GLM /「OpenAI 兼容」端点；用户已选「能用工具的模型」。
- **建议方向**：降级必须停在可操作状态（强制换模型、或本轮失败可重试），不要继续生成「假装做过」的答案。对常用供应商做 schema 探针（Model Fit PRD 已写 declared/probed/qualified），把 400 从运行时惊喜变成配置期红灯。

### 1.5 「继续」会整包回灌上一轮办公工作流

- **现象**：新目标会清掉上一轮 PPT/DOCX 旗标；`looksLikeResume` / 状态追问则把 Goal、阶段、nudge 计数、`Continuation` 全部灌回。用户说「继续」但心里已经换任务时，格式路由仍看 `turn.PptActive` / `DocxActive`。
- **证据**：`reconcileTurnCheckpointOnStart`（`internal/app/chat_run_stream.go`）；`officeGenToolForTurn` 优先看工作流旗标（`chat_office_autogen.go`）。S1 测了 epoch/model 钉死（`chat_continuation_test.go`），没测「继续 + 改任务」。
- **严重度**：**P2**
- **影响面**：中断后的 PPT/Word、恢复条、自动化无头续跑。
- **建议方向**：Resume 只恢复「同一 Goal 指纹」的工作流；Goal 变化则清旗标并明确提示「已结束上一份文档任务」。

### 1.6 专家 / 任务路由仍是关键词层，0.4.75 只钉住了已知误装

- **现象**：`剧本专家` → `小说编写专家`、本轮文本不继承上一轮「飞机维修」——测试已锁。弱匹配「分析 Excel」**故意不**挤掉已挂的 Database Optimizer。`classifyTaskRoute` 自称「优化而非能力边界」。「打开 Word 写周报」可走到 `docx.gen` 而不是桌面 Word。
- **证据**：`TestTurnEquipmentScriptIntentDoesNotKeepMountedOps` 等（`chat_turn_equipment_test.go`）；`mountedExpertYieldsToIntent` / `expertIntentOverridesMount`；`task_route.go` 注释与 hint 表。
- **严重度**：**P2**
- **影响面**：跨领域追问、办公 vs 桌面、复合「查+写+发」。
- **建议方向**：把「点名 / 单挂载 / 意图」做成一张用户能看懂的优先级表，并在装备条上显示「本轮实际装备」。不要再加第三套平行关键词。复合任务以工具并集为准（代码已 merge），避免再 shrink 到单一 R2。

### 1.7 `computer.act` 指名优先，Invoke 失败仍点像素；不承诺任意应用一次成功

- **现象**：合同要求先 `observe` 再 `name=` / `id=`，禁止空点。控件 Invoke 失败时测试明确期望像素点击（`TestComputerActChainInvokeFailClicksPixels`）。产品化不等于「任意 Windows 应用点一次就成」——`docs/audits/2026-09-09-module-review.md` / closeout 已写明该验收边界。
- **证据**：`internal/ccapp/computer_act.go`、`computer_act_chain_test.go`；工作流文案在 `chat_workflows.go` / `chat_companion_speech.go`。0.4.74 禁止「截月伴前台去点别的软件」。
- **严重度**：**P2**（错点）；**不升级为 P0 缺口**（从未承诺全桌面 SLA）
- **影响面**：桌面自动化、月伴操控、高 DPI / 缩放 / 动画中的按钮。
- **建议方向**：Invoke 失败应重新 observe 或交给用户，而不是默认像素。对外话术保持「抽样能用、不是万能遥控」。

### 1.8 截断 / 过滤 / 不完整流已不再冒充成功

- **现象**：`length` / `content_filter` 会剥掉 ToolCalls，保留部分正文，禁止自动产物。空 finish reason 仍当正常结束。2026-09-08 周报/自动化跟进把缺 `[DONE]` / `message_stop` 收成 `STREAM_INCOMPLETE`，不从残流放工具；无人值守 HTTP 400 不得剥工具后当完成。
- **证据**：`chatModelFinishError`（`chat_model_stream_error.go`）；`turnGenerationBudget.stream`；`docs/audits/2026-09-08-automation-weekly-stream-followup.md`；`chat_run_stream.go` 工具 400 分支带 `!unattended(op)`。
- **严重度**：产物造假路径已从 P0 **降为已缓解**；空 finish reason 与用户可见的 `STREAM_INCOMPLETE` 残留 **P2**。
- **影响面**：全聊天流、办公自动生成、无人值守自动化、周报试运行。
- **建议方向**：对缺 finish reason 且已有未完成 tool_call 的流按不完整处理。保持「部分正文 ≠ 任务完成」。自动化失败要留原失败记录（该审计已示范新闻任务）。

### 1.9 办公工作台导航与产物任务 ID：已提交树已写字段，本机 WIP 在补「打开对的任务」

- **现象**：对话里的 Office 卡片经 `requestOfficeStudio`（35s 超时事件）打开工作台。持久化时已把 `OfficeTaskID = officeTaskContextID(ctx)` 写入会话产物元数据。用户本机 WIP 据称为导航/引用与 `session_artifacts` 收紧绑定；云端未见 diff。
- **证据（已提交）**：`web/src/officeStudio/officeNavigation.ts`；`internal/app/session_artifacts.go`（`OfficeTaskID`、`.message-artifacts.json`）；`chat_run_stream.go` 落盘循环。元数据仍是会话文件夹旁路 JSON，不是 SQLite。
- **严重度**：**P2**（打开错任务 / 卡片与工作台脱节）；WIP 合入前按准确性回归，不按已修复关闭
- **影响面**：Office Studio、聊天附件卡、从对话返回任务列表（2026-09-08 office-navigation 审计同类）。
- **建议方向**：打开工作台必须带可校验的 `officeTaskId`；sidecar 写失败要可见。合入本机 WIP 时用「从卡片进对的任务、返回不丢引用」做合同，而不是只加字段。

---

## 2. 可靠性

### 2.1 一次不确定的 ROLLBACK 会关掉整个 SQLite 存储

- **现象**：写事务回滚失败时，store **直接 `db.Close()`**。之后所有存储调用失败，直到进程重启重新 `initialize`。
- **证据**：`internal/storage/sqlite/uow.go` 注释写明「Never put an uncertain transaction back in the pool」。有回归（`uow_recovery_test.go`）。设计正确（fail-closed），产品表现却是整机「核心不可用」。
- **严重度**：**P1**（低频，杀伤面是全产品）
- **影响面**：聊天、会议、办公、记忆、设置——任何需要 SQLite 的动作。
- **建议方向**：对用户说「数据引擎需要重启」并走已有 Host watchdog；给 Host 一条可观测的 `STORAGE_CLOSED`，不要只让后续 RPC 陆续 500。不要为了「看起来还能写」把脏连接放回池。

### 2.2 Engine 在 Host 未就绪前死亡：不会接管重启

- **现象**：`engineDied` 只有 `hostReady` 之后才 `--takeover` 拉起。启动窗口（连 pipe / `system.health`，约 15s）内 Engine OOM/panic，桌面会停在半启动。
- **证据**：`cmd/desktop/main.go` 中 `select { case <-hostReady: ... default: }`。`cmd/engine/main.go` 在 `WireEngine` 成功前不 listen。
- **严重度**：**P1**
- **影响面**：升级后第一次启动、迁移变慢的机器、磁盘紧张。
- **建议方向**：启动失败要有明确 UI（迁移中 / 引擎退出码），并允许有限次 relaunch。把「听 pipe」与「重活 reconcile」拆开，避免迁移时长直接等于「应用没窗」。

### 2.3 旁路状态与工具回执会静默丢

- **现象**：IM 入站路由、MCP preset 记在内存 `sync.Map` + JSON 文件，`save*` 对 `writePersistJSON` 用 `_ =`。工具操作结束同样 `_ = FinishToolOperation`。会话产物卡元数据写在 `.message-artifacts.json`（`session_artifacts.go`），与 SQLite 消息行是双写。崩溃或磁盘错误后，UI 的操作列表、入站回复路径、办公任务引用、计量账本可能和真实执行不一致。
- **证据**：`internal/app/persist_state.go`；`continuity_wire.go`；`session_artifacts.go`。聊天主路径 `MaxAttempts: 1`，付费生图/视频有 `findReusableMediaOp`——主路径不差，旁路差。本机 WIP 若加强 `officeTaskContextID` 绑定，仍走同一 sidecar。
- **严重度**：**P2**
- **影响面**：IM 回程、MCP 限制、操作中心、用量诚实性（0.4.74 强调「按收据说话」）。
- **建议方向**：这两类状态迁进 SQLite（已有会话/审计模型），或至少把写失败打进 diagnostics 且下次启动可见。回执失败进死信，而不是继续当成功。

### 2.4 Host 事件队列满或 Engine 断连会合成失败终态

- **现象**：`eventQueueCapacity = 1024`。队列不够就 `HOST_EVENT_OVERFLOW` 并取消该流；Engine 事件源关闭则对所有未终态流发 `ENGINE_EVENT_SOURCE_CLOSED`。
- **证据**：`internal/hostbridge/gateway.go`。有溢出测试（`streaming_test.go`）。
- **严重度**：**P2**
- **影响面**：月伴高频 delta、TTS、长工具轮、慢 WebView2。
- **建议方向**：对 delta 做合流/丢中间帧，但必须保住终态与 tool 边界。溢出应降采样，而不是杀整轮——除非已经无法保序。

### 2.5 语音 flake 在单测里被钉住，生产仍是 WebView2 真时钟

- **现象**：0.4.73 后 Quality 曾被 ASR 尾音节 / 静音截止 flake 打红。6a5de76 把 `speech.capture.test.ts` 改成 fake timers，并给 race 作业 120 分钟。业主本机 `.tts-test.log` **4/21 通过**——这是比「CI 用假时钟买绿」更硬的现场信号。
- **证据**：`web/src/session/companion/speech.capture.test.ts`；`.github/workflows/quality.yml`；业主本机 TTS 日志（本克隆无文件，按声明收录）；`docs/audits/2026-09-08-voice-interruption-check.md` 仍标真人打断未 E2E。
- **严重度**：**P2**（产品体感）；发布话术若写「语音已稳」则为 **P1 诚信风险**
- **影响面**：三条语音链路、会议、月伴半双工、TTS。
- **建议方向**：保留 fake-timer 回归。把 4/21 当已知债，不要用 vitest 子集或缓存 `test_all.log` 对冲。另做少量 opt-in 真机脚本。

### 2.6 技能分页一次读完：中途失败仍可能告诉模型「还有下一页」

- **现象**：0.4.75 在首次 `skill.view` 后把后续页拼进同一载荷（上限 40 页 / 约 96KiB），避免十几分钟来回 continue。`assembleSkillViewForModel` 在 invoke/JSON 失败时 `break`，若 `HasMore` 仍为 true，notice 是「已尽量一次读完」。
- **证据**：`internal/app/chat_skill_pages.go`。页回退与 digest 变化有硬错误（好）。
- **严重度**：**P2**
- **影响面**：周报等长 SKILL.md 试运行；正是 0.4.75「序列错误」场景的邻居。
- **建议方向**：组装中断时 `HasMore=false` 并带明确错误，迫使模型停或从 offset=0 重读，而不是继续分页空转。

### 2.7 IPC 断连后处理协程最多再活 5 秒 drain

- **现象**：会话关闭取消 `sessionCtx`，但 `WaitGroup` 只等 `sessionDrainTimeout`。不合作的 handler 会在没有客户端的情况下继续跑（工具/写库）。
- **证据**：`internal/ipc/session.go`。部分写失败会毒化连接（好，有 `partial_event_write_test.go`）。
- **严重度**：**P2**
- **影响面**：用户点停止/关窗后的桌面动作、文件写入、计量。
- **建议方向**：可变操作必须看同一 cancel；drain 超时后的活协程要进 diagnostics。停止按钮的产品合同应是「不再产生新副作用」，而不是「RPC 已经返回」。

### 2.8 付费媒体与流式重试：主路径是克制的

- **现象**：流在 headers 到达后不重试（`internal/llmadapter/chat_stream_error.go`）。图/视频按 receipt 防重提交（`model_catalog.go`）。截断不放工具。
- **证据**：见上；`docs/audits/2026-09-09-module-review.md` 也记录过「结果不明不换模型再付一次」。
- **严重度**：当前视为 **已缓解**（保持回归，勿回退）。
- **影响面**：生图/视频、长流。
- **建议方向**：不要为「提高成功率」加自动换模型。未知结果保持 `OUTCOME_UNKNOWN`。

### 2.9 2026-09-06 安全 P0 多数已落地；不要按旧矩阵重开

- **现象**：`docs/system-analysis-report-2026-09-06.md` 矩阵曾列「身份 Ed25519 私钥明文 hex」「`command.run` 继承完整父进程环境」。同文档后续登记 S-01..S-07 已核验。当前树：库列存 sentinel，真钥进 DPAPI（无 `identitySecrets` 时测试路径仍明文）；`commandEnvAllowlist` 不再 `os.Environ()` 全量，Job Object 在 Windows streaming/batched 路径挂接。
- **证据**：`identity.go` / `identity_privkey_test.go`；`command_env.go`；系统分析「本轮新增完成」S-01..S-07 行。市场签名根是 ADR-017 策略（离线根不参与日常签名），本克隆未见单独「市场私钥明文写库」的现行路径；业主口中的 market key 与 09-06「身份私钥」应对齐理解，避免重复开单。
- **严重度**：原 P0 **视为已缓解**。残留 **P3**：无 DPAPI 的非生产路径；环境白名单含 `APPDATA`/`USERPROFILE`（子进程仍可能读用户配置，但不再继承引擎里的 API key 环境变量——若密钥只在 DPAPI，这是可接受折中）。
- **影响面**：身份签名、People 配对、`command.run` / `run_terminal_cmd`。
- **建议方向**：保持回归。不要为了「再收紧」把 `go test` 所需的 `GOCACHE` 等砍掉。新密钥一律走 `secret.Service`，禁止第三份明文列。

---

## 3. 速度效率

### 3.1 冷启动被 Engine 装配串行挡住

- **现象**：Named Pipe 只在 `bootstrap.WireEngine` 成功后出现：开库、重放迁移、工具/调用恢复、内置种子、MCP 池等。Host 还要 datadir、800ms–3s 重连窗、最多 15s 连 Engine、`system.health`、第二套 browser WebView，然后才是主窗口。
- **证据**：`cmd/engine/main.go`；`cmd/desktop/main.go`；`migrations/` 现 **152** 条 SQL。`quality.yml` 写明 sqlite 测试「每个都开新库重放迁移」，这就是启动成本的放大版。
- **严重度**：**P1**（每次升级、每次 Engine 被 watchdog 拉起）
- **影响面**：打开应用、崩溃恢复、用户以为「卡住了」。
- **建议方向**：先听 pipe 再做可推迟的 seed/reconcile；启动 UI 显示阶段。给 sqlite 测试用模板库（`open_bench_test.go` 已对比「新库 vs 已迁移」），同时缩短 CI 与心理上的「启动有多贵」。

### 3.2 桌面 / 媒体 / 月伴工具轮挡住首字

- **现象**：`bufferReply` 在 `computerTurn`、纯播放、或「月伴 + 有工具且非 lookup」时，把模型正文攒到工具证据之后。用户看着「等待响应…」，上游可能已经在吐字。
- **证据**：`internal/app/chat_run_stream.go`。lookup 快路径有例外与测试（`chat_lookup_latency_test.go`）。这是准确性（先证据后表态）换延迟。
- **严重度**：**P1**（体感）
- **影响面**：电脑控制、媒体、月伴——最常用的「快」场景。
- **建议方向**：先流一句中性进度（不宣称成功），工具后再替换/续写。不要为了 TTFT 重新流「我已经点了播放」。

### 3.3 Mermaid：0.4.75 去掉永久等待；本机 WIP 在换引擎路径

- **现象（已发布 0.4.75）**：仅在 `streaming && mermaidFenceStillOpen` 时 `wait`；结束后不再因布局把滚动钉回底部。`MermaidBlock`：`SETTLE_MS = 480`、`RETRIES = 3`，经 `getDiagramBridge().render` 走隔离 WebView2 worker，`diagramrender` **Timeout = 8s**。`loadMermaidEngine()` 已存在于 `tideMermaid.ts`（动态 `import('mermaid')`），**主路径当前未用它渲染**。
- **现象（业主本机 WIP，本克隆未见）**：SETTLE 480→**280ms**；去掉 `RETRIES=3`；改为 `loadMermaidEngine` **直接渲染**；worker Timeout **8s→20s**。这会缩短「看起来在等」的时间，但把 parser 拉回主页面，并让一次卡死最长多等 12 秒。
- **证据**：已提交 `MarkdownMessage.tsx`、`MermaidBlock.tsx`、`tideMermaid.ts`、`internal/diagramrender/handler.go`；B06 见 `docs/audits/2026-09-06-system-review/implementation/phase2-conversation.md`。0.4.75 notes 只覆盖「围栏未闭合才显示生成中」，不覆盖 WIP 换引擎。
- **严重度**：已发布路径 **P2**（480ms + 冷 worker）。WIP 若合入且破坏隔离，稳定性升为 **P1**（主 WebView 可被恶意/畸形图拖死）。
- **影响面**：架构图、流式气泡、历史重绘、CSP。
- **建议方向**：围栏闭合后跳过 settle。Worker 保活/缓存。**不要为了 200ms 放弃 B06 隔离**；若本机是「失败时才 fallback 到 loadMermaidEngine」，须在报告合入前用测试写清。Timeout 加长只能当诊断，不能当默认。

### 3.4 回合后闪烁已压住，轮询 RPC 没减

- **现象**：`ctxStatus` 5s、`cc.getConfig`+`cc.getAuditLog` 5s、队列在流式时 1.5s。0.4.75 用 JSON 相等跳过 `setState`。会话仍在付「空转 RPC」。
- **证据**：`SessionPage.tsx`、`ccStatus.tsx`、`inputQueue.tsx`。
- **严重度**：**P2**
- **影响面**：长开会话的 Host/Engine 闲时负载、笔记本发热、偶发与流式抢锁。
- **建议方向**：改为事件或带 version 的长轮询；电脑控制条只在最近有 `computer.act` / 审批时才轮询。

### 3.5 热路径仍是两个超大函数 + 一个超大页面

- **现象**：`handleChatStart` 所在 `chat.go` 1700 行；`runStream` 1563 行；`SessionPage.tsx` 401 行但单行密度极高（流、语音、上传、队列、滚动、办公侧栏）。F-06 设计时 Engine 方法 345 / handler 458；现在约 **475 / 509**。
- **证据**：`docs/design/F-06-engine-decomposition.md`、`F-07-frontend-state-management.md`（仍是设计，未实施）。
- **严重度**：**P2**（速度与迭代，也是稳定性放大器）
- **影响面**：每一次聊天/专家/办公改动的回归面；首包解析；开发者「不敢动」。
- **建议方向**：只拆 **StreamEngine**（F-06 耦合最低的一块），不要一次五子系统。前端先把 poll/scroll/composer 挪出 `SessionPage`，不要先上状态库。

### 3.6 迭代被 120 分钟 race 和迁移重放拖住

- **现象**：Quality 普通 Go 测试 25 分钟预算；CGO race **120 分钟**（sqlite 在 0.4.73 撞过 90 分钟天花板）。`cmd/desktop` `//go:build !race`。覆盖率地板 51%，0.4.75 notes 与本机闸均为 **58.8% ≥ 51%**。业主 `test_all.log` **多为缓存绿**；`vitest-all` 只有 **74/583 ~26s**，不是全套 288/2176。本机与本云均**未**跑 120 分钟 `-race`。
- **证据**：`.github/workflows/quality.yml`、`release-candidate.yml`；`6a5de76`；`release/notes-0.4.75.md`；业主本地日志声明。
- **严重度**：**P3**（迭代）；若用缓存日志对外称「全量已验」则为 **P2 发布诚信**
- **影响面**：一人公司的反馈环；「先扩 timeout 再绿」会掩盖真 hang。
- **建议方向**：模板库 + 按包拆 race。对外只用 `-count=1` 非缓存日志。Timeout 只当保险丝。

---

## 4. 稳定性

### 4.1 Handler panic 吃掉现场

- **现象**：`Engine.Handle` 把 panic 变成 `ENGINE_HANDLER_PANIC`，**不记录 `recover()` 值或栈**。对比：`runStream` 和 IPC session 会打栈。
- **证据**：`internal/app/engine.go`。
- **严重度**：**P1**（单次请求可恢复，事后无法定位）
- **影响面**：全部 500+ bridge 方法；「请重试」循环。
- **建议方向**：与 `runStream` 看齐，记类型 + 栈 + method/traceId，仍 fail 单请求、不杀进程。

### 4.2 长会话无界缓存 vs 已修好的 compaction 泄漏

- **现象**：2026-09-06 曾把 `compactionapp.sessionLocks` 标 P3/P2 泄漏。Q-04 已改成有界 LRU（`internal/compactionapp/boundedmap.go`）。`adapterCache` 仍是无逐出 `map[string]Adapter`（`provider_diagnostics.go`）。`inboundRoutes` / `mcpPresetByEP` 是无界 `sync.Map`。
- **证据**：`boundedmap.go` 注释（Q-04）；`provider_diagnostics.go`；`engine.go` 字段。
- **严重度**：**P2**（adapter/sidecar）；compaction 原项 **已缓解**
- **影响面**：多供应商诊断、长开不关的 Engine、大量 IM 会话。
- **建议方向**：adapter 按 provider+model 做小 LRU；sidecar 迁库后自然有界。不要再引入第三份「会话级只增不减」的 map。

### 4.3 Host/Engine 生命周期：就绪后能自愈，就绪前不能

- **现象**：Engine 在认证会话断开后可保活；托盘退出才停引擎；watchdog 先重连 RPC 再整桌面 relaunch。这比「Host 一关全死」稳。缺口见 2.2。
- **证据**：`cmd/desktop/main.go`、`watchdog.go`、`cmd/engine/main.go`。
- **严重度**：就绪后路径 **良好**；启动窗口 **P1**（见 2.2）。
- **影响面**：日常崩溃恢复 vs 升级首启。
- **建议方向**：把启动窗口纳入同一套死亡处理，而不是 `default:` 空分支。

### 4.4 Quality 绿，部分来自放宽而不是根因消失

- **现象**：语音 flake → fake timers；sqlite race → 120m；更早还有 IM timeout / Office remount 不当红（`586b0f6`）。大量真机路径 `t.Skip`（WebView2、SAPI、环回音频、LibreOffice）。业主本机 TTS 4/21、Go 全量多为缓存，与「Quality 绿」可以同时成立。
- **证据**：workflows；`internal/webviewhost/browser_native_windows_test.go`；`internal/meetings/loopback_*`；`internal/officerender/*_integration_test.go`；业主 `.tts-test.log` / `test_all.log` 声明。
- **严重度**：**P2**
- **影响面**：发布信心；回归「绿了但周报试运行仍序列错误」这类 0.4.75 才补上的洞。
- **建议方向**：每放宽一条 CI，登记「被买下来的风险」和复测日期。真机矩阵保持小而重复（装/升/语音/一份办公）。缓存绿不进发布清单。

### 4.5 `runStream` 体量本身就是竞态工厂

- **现象**：一个函数里叠了工具环、bufferReply、视觉/工具 400 回退、办公收尾、预算、companion fallback、checkpoint。0.4.75 又加了技能停轮、序列消毒调用点。单测很多，但交互空间是组合爆炸。
- **证据**：`chat_run_stream.go` 127–1563 行（09-06 报告记 1138，gocyclo 曾 344→332）。`maxToolLoopSteps` 24、硬顶 48（`chat_continue.go`）。本机 WIP「连续性测试」若再往此函数加分支，回归面继续胀。
- **严重度**：**P2**
- **影响面**：任何「再加一个特殊轮」——专家、试运行、办公、电脑控制——都可能复活旧抖动/空转。
- **建议方向**：冻结「再往 runStream 加布尔分支」。新行为走显式 stage + 测试表。这是稳定性工作，不是风格偏好。

### 4.6 WebView2 边角仍然是发布门，不是单测门

- **现象**：README / ADR-001：缺 `ICoreWebView2_3/4` 或 frame2 则关窗；无 Bind/host object。原生关闭、图表 worker、会议采集在审计里有本机证据，CI 默认跳过。`P0-P1-status.md` 仍要求：远程 CGO race、一次性 Win10/11 装/升/留/重装/`/PURGE`、Authenticode 时间戳。业主本机 `Test-Install.ps1` **因已有安装拒绝**；本云克隆无 `P0-P1-CLOSEOUT.md`。
- **证据**：`README.md`；`docs/implementation/P0-P1-status.md` 外部证据行；0.4.75 notes「未跑 Test-Install.ps1」；业主确认。
- **严重度**：**P2**（发布门未关）；不是运行时崩溃
- **影响面**：Evergreen Runtime 缺失、干净机升级、签名可信。
- **建议方向**：保持 fail-closed。用一次性虚拟机跑安装生命周期；不要把「本机已装、脚本拒跑」写成 closeout。Authenticode 缺证书就明确「开发包，非生产」。

### 4.7 实时 talk 仍无发送背压（09-06 仍在）

- **现象**：2026-09-06 标 P1：OpenAI realtime WS 无客户端背压，发送队列可能堆积。当前 `handleTalkAppend` 对 PCM 直接 `session.write(...)`，失败才报会话结束，未见有界队列或对慢连接的拒绝/合流。
- **证据**：`docs/system-analysis-report-2026-09-06.md` §2；`internal/app/talk_handlers.go`（`talk.append`）。
- **严重度**：**P2**（长通话卡顿/假死）；高频全双工升 **P1**
- **影响面**：实时全双工 talk（与半双工月伴/按钮路径不同）。
- **建议方向**：给 write 加有界队列 + 满则丢旧 PCM 或断开并明示。不要在无背压时加大采集帧率。

### 4.8 本机 Mermaid WIP 可能把解析器搬回主 WebView

- **现象**：B06 把 Mermaid 放进独立 worker，主界面只挂 SVG。本机若改 `loadMermaidEngine` 直接 `import('mermaid')` 渲染，等于撤回隔离。Timeout 20s 会让主线程/页面更长时间不可用。
- **证据**：已提交路径仍走 `getDiagramBridge().render`；WIP 为业主声明。
- **严重度**：合入前按 **P1 候选** 审；未合入不记现行缺陷
- **影响面**：畸形图、主题切换、CSP、与 0.4.75「结束等待」补丁的交互。
- **建议方向**：保持 worker 为主路径。直接引擎只允许测试或明确的降级，且须能 kill。

---

## 已做得好的地方

这些不应当成「还可以再优化」的借口，而是**不要回退的合同**：

- **生产架构边界清楚**：Go Host / Engine / React；Electron/Python 已删除（ADR-001）。固定 `https://app.lunitide.local`，只让顶层可信源进网关。
- **IPC 与机密**：Named Pipe + nonce + PID；bootstrap 凭据不进命令行（ADR-002/004）。部分写失败毒化连接，而不是拼脏帧。
- **SQLite 纪律**：WAL、迁移校验和、schema 指纹、不确定事务关库而不是复用。覆盖率与 contract 测试挡回归。
- **诚实失败（近期）**：截断/过滤保留部分正文、不放工具、不造办公文件；`STREAM_INCOMPLETE` 不从残流放工具；无人值守 400 不剥工具装完成（`docs/audits/2026-09-08-automation-weekly-stream-followup.md`）；用量不编节省百分比。
- **电脑控制统一管道**：`computer.act` 映射到现有 `cc.*`。验收边界诚实：不承诺任意 Windows 应用一次成功。
- **S-01/S-02 安全加固**：身份私钥 DPAPI sentinel；`command.run` 环境白名单。09-06 矩阵里的对应 P0 不要当未修重开。
- **Bridge 契约**：schema ↔ `RuntimeHandlers` 双射 + `verify:bridge` + 生成文件 drift 门。
- **0.4.75 针对性修复是真的**（以 `release/notes-0.4.75.md` 为准）：流式才钉底；相同 JSON 不 `setState`；Mermaid 只在未闭合围栏等待；剧本点名不再落到机务；技能分页与序列消毒对准周报试运行。
- **Compaction 泄漏（Q-04）已有界**；流数量封顶 32；付费媒体有 receipt。覆盖率闸 58.8% ≥ 51%。

---

## 建议优先级路线图

按**一人公司能连续做完的厚度**排，不按理想团队并行。

### 第一段（约 30 天量级）——先让「答对 / 交对」可解释

1. **专家装备合同**：多挂载要么全生效要么 UI 禁止多名且文案一致（§1.1）。装备条显示本轮真实 Names/BindKeys。
2. **办公质量门**：去掉「nudge 次数 = 可以生成」；自动散文落盘加收据（§1.2）。
3. **上下文 fallback 可观测**：序列消毒命中、native replay 超时、显式回退打点到 diagnostics（§1.3）。
4. **工具 400**：本轮失败或强制换模型，禁止纯聊天假装有工具（§1.4）。
5. **Handler panic 打栈**（§4.1）——改动小、收益是以后所有稳定性问题。
6. **审本机 Office/Mermaid WIP**：产物 `officeTaskId` 导航可合；`loadMermaidEngine` 回主页面须单独否决或加隔离合同（§1.9 / §3.3 / §4.8）。

成功标准：再出现误装/空 PPT/「模型不认刚才的工具」时，能从装备条或 diagnostics 看出**走了哪条分支**，而不是猜关键词。本机 WIP 要么进独立提交说明，要么不和 0.4.75 已发布行为混为一谈。

### 第二段（约 60 天量级）——失败可恢复，收据一致

1. Engine 启动窗口纳入 watchdog + 可见启动阶段（§2.2 / §3.1）。
2. IM/MCP sidecar 与工具回执进 SQLite 或带失败可见性（§2.3）。
3. 技能一次组装中断时的诚实 `HasMore`（§2.6）。
4. Host 事件溢出改为保终态的降采样（§2.4）。
5. Resume 与 Goal 指纹绑定（§1.5）；`computer.act` Invoke 失败禁止默默像素（§1.7）。

成功标准：杀 Engine、拔网、拒工具 schema、长 SKILL.md，用户看到的是**可重试的原因**，不是静默换一种更笨的行为。

### 第三段（约 90 天量级）——降热路径复杂度，才能继续加能力

1. 抽出 StreamEngine / 冻结 `runStream` 新分支（§3.5 / §4.5）。
2. 月伴/桌面首字进度流，不破坏「先证据后表态」（§3.2）。
3. Mermaid **保持**隔离 worker + 闭合后去 settle（§3.3）；会话轮询改事件（§3.4）。talk 发送背压（§4.7）。
4. CI：模板库、race 拆包、desktop race 子集（§3.6 / §4.4）。用一次性 VM 补 `Test-Install` / Win10/11 生命周期（§4.6）。
5. 再谈 Model Fit Slice 的 probe/qualify（`docs/design/PRD-MULTI-MODEL-CONTINUOUS-FUSION-v3.md`），否则 1.4 会反复出现。

成功标准：加一个专家或一种办公格式时，不必再改 1500 行主循环；Quality 绿来自更短的测试，而不是更长的 timeout。

---

## 调查边界（避免误读本报告）

- 本云环境是 Linux 工作区，工作树干净，**看不到**业主那 21 个未提交文件。WIP 条目全部标成「声明 / 合入前候选」，源码行号只引已提交 0.4.75。
- **未**复跑 120 分钟 Windows CGO `-race`（业主本机也未跑）。**未**跑 `Test-Install.ps1`（业主本机因已有安装被拒）。启动与 TTFT 是结构判断，不是毫秒基准。
- 业主 `test_all.log` 缓存绿、`vitest-all` 74/583、`.tts-test.log` 4/21、覆盖率 58.8%：按声明收录。0.4.75 notes 的 288/2176 是另一份全套口径，两者不要加减。
- `docs/implementation/P0-P1-CLOSEOUT.md` 在本克隆不存在；发布门以 `P0-P1-status.md` 为准（有条件关闭）。
- 2026-09-06 系统分析里的 P0 安全项以同文档「S-01..S-07 已落地」+ 当前源码复核为准，不把旧矩阵当仍打开。
- 未把「邮件 / 共享日历 / 在线 48 场景」列为缺陷——release notes 已声明未通。`computer.act` 未承诺任意应用一次成功。

---

## 关键路径对照（便于下次接着查）

| 用户旅程 | 主要代码 | 本报告条目 |
|---|---|---|
| 打字一轮 → 专家/工具 | `chat.go` → `turnEquipmentFor` → `task_route.go` → `runStream` | 1.1–1.6, 3.2, 4.5 |
| 流式与收尾 | `chat_run_stream.go`、`chat_generation_budget.go`、`SessionPage.tsx` | 1.8, 3.2, 3.4 |
| PPT/PDF/Excel/周报 | `chat_office_autogen.go`、`chat_*_workflow.go`、`chat_skill_trial.go`、`session_artifacts.go` | 1.2, 1.8, 1.9, 2.6 |
| Office Studio 导航 | `web/src/officeStudio/officeNavigation.ts` | 1.9 |
| Mermaid | `MarkdownMessage.tsx`、`MermaidBlock.tsx`、`tideMermaid.ts`、`diagramrender` | 3.3, 4.8 |
| `computer.act` | `internal/ccapp/computer_act.go`、`chat_workflows.go` | 1.7 |
| 实时 talk | `talk_handlers.go` | 4.7 |
| 发布门 | `docs/implementation/P0-P1-status.md`、`quality.yml` | 3.6, 4.4, 4.6 |
| S1 连续 | `chat_continuation.go`、`reconcileTurnCheckpointOnStart` | 1.3, 1.5 |
| Host/Engine 活下来 | `cmd/desktop/main.go`、`ipc/session.go`、`hostbridge/gateway.go`、`sqlite/uow.go` | 2.1–2.4, 2.7, 4.3 |
