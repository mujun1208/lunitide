# Lunitide Memory Fabric v2、PaddleOCR-VL 1.6 与媒体交互升级 PRD（R3）

> 文档状态：R3 代码/PRD/UI融合复核版；文档交付，产品代码/模型包/实机播放尚未实施
> 最终交叉复核：2026-09-15；代码审计 HEAD `b1d58b0d8e4867bd5c5efd7048ce82b5daf07c09`；迁移基线截止 0159
> 决策日期：2026-09-14
> 决策人：产品负责人授权由技术分析代为选择最佳方案
> 适用平台：Windows x64；现有 Go Engine、SQLite、React/WebView2 架构
> 配套实施计划：[记忆升级](03-memory-implementation-plan.md) · [可选 OCR 模型包](04-ocr-implementation-plan.md) · [媒体与活动 UI](05-media-ui-implementation-plan.md) · [总实施计划](11-master-implementation-plan.md) · [需求追踪矩阵](12-requirements-traceability.md)

本版与 [跨模块合同及实施计划](06-integration-contracts-plan.md)、[验收与五分标准](07-acceptance-and-scorecard.md)、[最终复核](10-final-audit-and-five-point-remediation.md) 共同构成完整开发基线。原始问题覆盖见 [需求与竞品复核](08-original-requirements-and-evidence.md)，R2 历史缺陷见 [复核报告](01-review-findings.md)。本目录是唯一现行版本；旧 superpowers 文档只作历史记录。

## 1. 文档用途与证据规则

本文定义三个可以独立交付、但共享治理与 UI 原则的子项目：

1. 将现有两套记忆通路升级为一个本地优先、自动筛选、可追溯、可回滚的 `Memory Fabric v2`。
2. 保留 `Windows.Media.Ocr` 快速路径，新增用户主动安装的 `PaddleOCR-VL-1.6` 复杂文档模型包。
3. 在不绕过现有工具审批和审计的前提下，借鉴白龙马的媒体连续体验与状态可视化，建设统一媒体会话和工具活动 UI。

本文使用以下证据标签，开发和验收不得混用：

| 标签 | 含义 |
|---|---|
| **现码事实** | 已由当前仓库代码、迁移或测试直接证明。 |
| **外部事实** | 来自项目或厂商官方代码、论文、模型卡或文档；厂商自报指标会明确归因。 |
| **产品决策** | 本 PRD 要求实现的行为，不代表现在已有。 |
| **验收目标** | 上线前必须测量并达到的门槛，不描述当前性能。 |

“100% 可落地”在本文中的含义是：范围、状态机、数据归属、接口、失败行为、迁移、回滚、测试和完成定义均明确，开发不需要自行猜产品决策。它不等于在没有本机基准测试前承诺某个 OCR 或记忆算法一定比所有产品准确，也不等于承诺第三方平台内容可被合法抓取或分发。

## 2. 最终决策摘要

### 2.1 选择方案

采用 **方案 B：Lunitide 原生统一 Memory Fabric v2**。

不把 Mem0、Hindsight、Graphiti、Letta 或 Claude Memory 直接变成正式数据真相源。它们的可验证设计思想用于改造现有 Go/SQLite 内核：

- 借鉴 Letta 的“小型常驻核心记忆 + 按需外部检索”，避免把全部历史塞入提示词。[^1]
- 借鉴 Hindsight 的事实、经历、证据支持的观察分离，以及关键词、向量、关系和时间多路召回。[^2]
- 借鉴 Graphiti 的事实有效期、获知时间、历史保留和来源 episode。[^3]
- 借鉴 Claude Dreams 的输入不修改、生成新版本、审阅后切换与可丢弃机制。Claude Dreams 截至本文日期仍被官方标注为 research preview，本文不把它的可用性或效果当作 Lunitide 依赖。[^4]
- 借鉴 Mem0 的提取、去重、整合和 Token 受控思路，但不采用厂商基准数字作为本项目验收结论。[^5]

该选择的理由不是“第三方一定较差”，而是当前产品已经具备本地 SQLite、候选确认、版本、来源叶、审计、上下文预算和压缩底座。直接嵌入第三方服务会制造第二个运行时、第二套删除/导出语义和新的隐私边界，反而扩大系统风险。

外部方案对本项目的价值边界如下：

| 方案 | 可验证的强项 | 不直接照搬的原因 | Lunitide 采用方式 |
|---|---|---|---|
| Claude Memory + Dreams | workspace scoped store、不可变版本、CAS 内容更新；Dreams 以独立输出 store 整理且可审阅/丢弃。 | 托管 API/沙箱能力，不是可嵌入的开源实现；Dreams 仍为 research preview。 | canonical version + CAS；copy-on-write generation；preview/activate/discard。 |
| Hindsight | retain/recall/reflect 分层；事实、经历、观察/mental model 区分；多路检索与 provenance。 | 直接引入会与现有 M8/legacy 构成第三个记忆运行时。 | 采用分类、来源和 keyword/dense/time/relation 多路召回。 |
| Graphiti | 双时间事实、episode 来源、历史关系和 hybrid retrieval。 | 首版引入图数据库会扩大部署、备份和删除面。 | SQLite 有效期/获知时间、受限关系表和索引；达到实测瓶颈后再 ADR。 |
| Letta/MemGPT | 小型 core memory 常驻、完整 history/archival 外置、按需检索。 | 让模型自行直接写核心记忆会扩大 prompt injection 和误存风险。 | 小 Core Profile + 外部检索，但所有写入先过确定性 policy/evidence gate。 |
| Mem0 | 提取、去重、更新和混合记忆层易于独立集成。 | 公开数字跨模型/数据集不可直接等同于本产品；也会增加外部真相源。 | 采用批量 extraction、去重/冲突决策与独立评测，不采用其服务作为权威库。 |

用户提供的专家材料对“分层、混合召回、异步整理、时间关系”判断有帮助；其中二手文章的 star 数、融资、“6 倍提升”“当前最强”等没有统一实验口径，故仅作为待核线索，不进入需求依据或宣传结论。

### 2.2 用户已确认的记忆体验

默认模式为 **智能自动保存**，而不是逐条确认：

- “你好”“谢谢”“今天天气如何”“播放一首歌”“这次临时用红色”等问候、查询、一次性指令和短期状态不进入长期记忆。
- 姓名/称呼、稳定偏好、长期目标、长期项目约束、用户明确纠正、反复出现的工作习惯、可复用流程和经过工具验证的环境事实可以自动进入相应作用域。
- 密码、令牌、验证码、银行卡、证件号、私钥等敏感内容不创建正文候选，只记录不含正文的拒绝原因计数。
- 推断性观察和与现有事实冲突的内容不弹窗打断对话；进入记忆中心的静默审阅箱。
- 自动保存成功只出现可撤销的轻提示，例如“已记住 1 条 · 撤销”，不得出现每轮确认横幅。
- 用户说“忘记这件事”“不要再记我的位置”等明确删除指令时，优先执行删除/禁记规则，不把该句重新保存为偏好正文。

### 2.3 OCR 决策

`PaddleOCR-VL-1.6` 是 **用户自选安装的高级复杂文档包**，不是对 `Windows.Media.Ocr` 的直接替换。

- Windows OCR 继续承担零下载、低启动成本的截图、简单扫描件和灾备路径。
- PaddleOCR-VL 只在安装完成、健康检查通过且路由命中复杂文档时使用。
- 必须运行官方所描述的“版面分析 + VLM 识别”完整链路；只调用 0.9B VLM 组件不得在 UI 中标记为完整 PaddleOCR-VL。官方明确提示二者不等价，单独运行组件可能无法复现完整流水线效果。[^6]
- 官方报告的 OmniDocBench 等分数只作为候选依据，最终默认路由由 Lunitide 自建盲测决定；不得在产品文案中写“必然比 Windows OCR 更准”。[^7]

### 2.4 自动工具、媒体和 UI 决策

- 不复制白龙马整套 Brain UI，也不复制其全局 JavaScript 控制面。
- 借鉴其“选择/生成资产 → 展示 → 播放控制 → 继续对话”的连续闭环，以及队列、进度、封面、歌词和可见播放状态。[^8]
- 新增 Lunitide `MediaSession`，统一自有播放器和外部播放器控制，但能力与可信度分型。
- 只有自有播放器的实际媒体事件，或 Windows SMTC 明确回读确认，才能标记 `verified_playing`；发送媒体键、UIA 点击或打开 URL 只能标记 `command_dispatched` 或 `uncertain`。
- 内置播放器只接收用户本地媒体、用户明确导入的媒体和 Lunitide 合法生成的媒体资产。不开发展示页抓取、第三方音乐/电影下载、破解 DRM、绕过会员或模拟官方曲库。

## 3. 目标、非目标与成功条件

### 3.1 产品目标

| ID | 目标 |
|---|---|
| G-01 | 普通对话中自动保留未来有用的信息，取消逐条确认干扰。 |
| G-02 | 让每条可注入长期记忆都有规范正文、版本、来源、作用域、时间和删除状态。 |
| G-03 | 提升跨会话更新、时间问题、用户纠正和项目续接的召回能力，同时严格控制 Token。 |
| G-04 | 复杂文档 OCR 可由用户按需安装，安装失败不得破坏现有 OCR。 |
| G-05 | 媒体和自动工具向用户持续显示真实阶段、验证证据和恢复动作。 |
| G-06 | 所有新增能力可独立关闭、灰度、回滚，存量数据不可因升级被静默删除或错误升格。 |

### 3.2 明确非目标

- 不通过微调模型权重保存个人记忆。
- 不把聊天摘要当成事实真相，也不把助手推测自动提升为用户事实。
- 不在首版引入 Neo4j、PostgreSQL、外部向量数据库或常驻第三方记忆服务。
- 不承诺离线设备在未配置任何可用推理模型时完成隐式语义提炼；该状态仍应自动保存可由确定性规则识别的显式稳定信息。
- 不将现有项目知识图谱直接当作个人关系图；其 ontology 类型面向文件、模块、需求等项目实体。
- 不把 OCR 的生成式 Markdown 当作原件或精确逐字证据；原始文件、页图和识别引擎信息必须保留。
- 不建立内容聚合平台、第三方曲库或影视库。
- 不降低 `computer.act`、`media.play`、桌面控制现有审批、急停、锁、能力门或审计要求。

### 3.3 成功条件

发布必须同时满足：

1. 存量与新写记忆在 canonical 读模型上无重复注入、无跨主体/跨项目泄漏。
2. 规定的低价值语句集合自动保存为 0 条；规定的稳定偏好/目标集合达到评测门槛。
3. 用户纠正后，旧事实保留历史但不再作为“当前事实”注入。
4. 当前模型 Token 计量能证明记忆注入从固定字节裁剪升级为统一 Token 预算。
5. PaddleOCR 包缺失、下载失败、校验失败、推理超时或崩溃时，Windows OCR 和云端 OCR 原有路径仍可工作。
6. UI 不再把“已发送媒体键”显示成“正在播放”。
7. 全量导出、导入预演、单条删除、整库清除和回滚演练均通过。

## 4. 当前代码基线

### 4.1 记忆是两个生产平面，不是一个

**现码事实：传统平面。** `migrations/0014_memory.sql` 定义 `memories`，包含 `working / episodic / semantic / procedural`、workspace/project/session 作用域、`content`、`embedding_id`、置信度、访问计数和 TTL。`internal/memoryapp/service.go` 提供 CRUD、搜索和 TTL 清理；`internal/storage/sqlite/memory_search.go` 使用 FTS/LIKE。`embedding_id` 存在，但当前个人记忆在线检索没有完成密集向量召回。

**现码事实：治理平面。** `migrations/0061_m8_memory_core.sql` 定义 `memory_candidates`、`memory_facts`、`memory_source_leaves` 和 `recall_traces`。候选保存 payload，事实表保存 ID/版本/敏感度/状态，来源叶保存证据引用和摘要。`internal/m8app/memory.go` 实现候选确认与不可变版本；`migrations/0075_m10_memory_ops.sql` 和 `internal/m8app/memory_ops.go` 增加设置、隐藏、置顶、增长箱、导出与清理。

**现码事实：同一回合同时读取两套。** `internal/app/chat_memory.go:93-148` 从治理平面读取确认偏好和查询相关事实，`151-246` 又从传统平面读取 working、procedural、episodic 和 semantic。两条链路都处于生产注入路径。

**现码事实：治理事实正文关系不完整。** 确认会创建 `memory_facts` 和来源叶，但在线注入仍读取 confirmed candidate payload；candidate 与 fact 没有显式外键映射，当前通过来源叶和 active fact 是否存在间接过滤。这使事实正文、事实身份和候选身份分裂。

**现码事实：现有自动识别过窄。** `internal/domain/m8core/user_memory.go:9-52` 只接受有限的“我喜欢、我偏好、我叫、以后默认”等句式，并整体排除带问号、URL、动作词、时间词的文本。这能防止一部分误记，却会漏掉多句输入中的稳定陈述、纠正、目标和项目决策。

**现码事实：当前召回不是完整混合检索。** `internal/m8app/memory_inject.go:58-163` 仍最多读取 200 条可见候选，以关键词覆盖度 0.8 + 新鲜度 0.2 排序；FTS 命中只作为候选提示。它没有向量召回、有效时间、冲突状态、关系检索或反馈信号。

**现码事实：可复用基础较强。** 当前已有：

- 身份/作用域策略扩展点、敏感度过滤和召回 trace；默认 policy 允许与 evidence shape-only 不等于生产隔离已闭合，新版必须注入真实检查；
- 一次性确认 token、审计、hidden 排除、墓碑和设备向量时钟骨架；
- FTS5 trigram；
- `internal/llmadapter/openai_embed.go` 的 embedding 编解码；
- `internal/m8app/kb_hybrid.go` 的 FTS、dense、recency 与 MMR 模式；
- `internal/contextapp` 的上下文优先级、输出保留和安全余量；
- `internal/compactionapp/executor.go` 的 checkpoint、预览、CAS 提交、失败恢复；
- `internal/app/companion_archives.go` 的异步周归档。

因此本 PRD要求演进现有底座，不新建另一套 Agent Runtime。

### 4.2 当前导出、同步与来源校验边界

- 当前 Memory Ops 导出覆盖治理 facts/leaves/candidates/traces/growth/flags/settings，但不包含传统 `memories`；在统一前不得继续用“完整备份”描述该结果。
- 当前 purge 会涉及传统记忆，而导出缺少对应数据，存在“可删但不可完整恢复”的不对称。
- 当前同步服务能够计算向量时钟与冲突，但没有把合并后的内容写成新的事实版本；不得把它描述成已完成多端内容同步。
- 当前默认 evidence verifier 主要校验引用和 digest 形状，生产接线没有对原始消息重新读取并重算摘要；新版“可追溯”必须补真实 resolver。

### 4.3 OCR 基线

- `internal/doctext/image_ocr_windows.go` 通过受时限、内存和输出大小约束的子进程调用 `Windows.Media.Ocr`，单图超时 15 秒，内存上限 256 MiB。
- `internal/doctext/pdf_ocr_windows.go` 处理 PDF，整份超时 90 秒、内存上限 768 MiB、最多接受 100 页结果。
- `internal/ocrapp/recognize.go` 已有文本层优先、缺页 OCR、云端 provider、云端失败回退本地、页覆盖信息和取消检查。
- `internal/ocrapp/route.go` 以 64 位小写十六进制 CAS revision 保存 `providerId/modelId/preferProvider`；该兼容合同不能被新数字 revision 静默替换。
- `web/src/settings/OCRRouting.tsx` 已有 provider 选择、本地状态和最近失败显示。
- `internal/ocrapp/pack.go` 只有粗粒度 PP-OCR 文件存在探测；当前 `HealthSnapshot` 会读取 routing 中的 `packRoot`，发现 `ppocr.exe/paddleocr.exe/ppocr.onnx` 即返回 `available/ready`，但 `recognize.go` **从不根据 `localEngine=ppocr` 执行该目录**。因此它只是“旧目录已登记”，不是可运行、已安装或已接线的 OCR 引擎。
- `LocalOCRReady` 当前仅以 `runtime.GOOS == windows` 推断图片/PDF 可用，没有执行 WinRT 初始化、语言枚举或固定小图识别；正式 UI 不得把该值渲染为“健康通过”。
- `OCRRouting.tsx` 以 `Promise.all(provider.list(), ocr.get())` 加载，provider 故障会连带遮蔽本地 OCR 状态；并且 `SettingsPage.tsx` 目前仍把 OCR 放在“路由管理”中。两者均是 R3 必修接线，不是现成的“两卡融合页”。

### 4.4 工具、媒体与 UI 基线

- `media.play` 已经经过 `internal/toolruntime/runtime.go` 的统一工具执行、能力门、桌面串行、hook、审批和审计。新增内置播放器不得绕过这条通路。
- 当前 `media.play` 还支持命名歌曲查询、明确 URL 以及 browser/foreground/app 目标；升级必须保留该 external 兼容路径，不能因增加 owned player 而删掉。它只负责打开/控制外部目标，不得把远端内容导入内置播放器。
- `internal/winexec/media_session_windows.go` 可查询 Windows SMTC 会话并返回标题、艺术家和验证状态；`internal/winexec/media_windows.go` 提供全局媒体键兜底。
- `internal/toolruntime/media_foreground.go` 的一条兜底路径在只发送媒体键后仍生成 `passed=true/uncertain=false` 的成功语义；这是 P0 必修真实性缺陷。
- `web/src/session/liveChat.ts` 能在页面离开后保留活动回合并回挂，适合作为媒体事件重连基础。
- `web/src/session/ToolTrajectory.tsx` 当前只显示工具名、状态和短摘要，没有目标、阶段、验证证据、持续时间、重试或恢复入口。
- `web/src/settings/ComputerPanel.tsx` 已有电脑控制启用、风险级别、急停、审计与 CAS 冲突处理；媒体外部控制必须复用这些治理能力。

## 5. 总体架构

```text
用户输入 / 工具回执 / 项目事件
            │
            ├── Memory Capture Gate ──> canonical facts / working state / review inbox
            │                              │
            │                              ├── FTS + dense + temporal + relation indexes
            │                              └── copy-on-write consolidation generations
            │
文件/页图 ──┼── OCR Policy ──> text layer / Windows OCR / optional PaddleOCR-VL pack
            │
媒体意图 ───┴── ToolRuntime ──> Media Operation ──> owned player / external SMTC/UIA
                                             │
                                             └── typed Bridge snapshots + events

所有结果最终进入：Context Assembler、Activity UI、Audit、Token Ledger、Export/Restore
```

共享不变量：

1. **本地 SQLite 是唯一元数据真相源。** 模型文件和媒体大文件保存在受管数据目录，SQLite 保存版本、digest、状态与引用。
2. **模型输出不是授权。** 记忆晋升、OCR 输出展示、工具执行均经过确定性校验与现有权限门。
3. **事件可恢复。** 页面断开后以 snapshot/list 恢复，实时 event 只是加速，不是唯一状态。
4. **失败不得假成功。** 每个子系统显式区分 requested、running、verified、uncertain、failed、cancelled。
5. **所有新表和读写切换均 additive + feature flag。** 禁止需要数据库降级脚本才能回滚的破坏性首发。

当前仓库没有一套可直接复用的、持久化且带 CAS 的通用 feature-flag store。本 PRD 中的 Memory/OCR/Media 开关必须分别落入对应迁移的设置表，并通过 Engine 读写；不能用环境变量或浏览器 `localStorage` 冒充发布控制面。

## 6. Memory Fabric v2 产品设计

### 6.1 统一记忆分类

| kind | 用途 | 默认作用域 | 是否常驻提示词 | 生命周期 |
|---|---|---|---|---|
| `profile` | 姓名、称呼、职业、时区、稳定位置等直接资料 | user | 仅精选快照 | 直到纠正/删除 |
| `preference` | 回复、工具、格式、内容等稳定偏好 | user 或 project | 精选快照；其余按需 | 直到纠正/删除 |
| `goal` | 持续目标、长期计划 | user/project | 按当前任务查询 | 完成后归档 |
| `constraint` | 必须/禁止、合规、环境硬约束 | project/user | 与任务相关时 pinned | 直到被替换 |
| `decision` | 已选方案及理由 | project | 按需 | 被新决策替换但保留历史 |
| `procedure` | 用户教授或工具验证的可复用流程 | project/expert | 按需 | 版本化 |
| `episode` | 发生过的任务、故障、结果和上下文 | session/project | evidence | TTL 或归档 |
| `observation` | 系统由多条证据综合出的模式 | user/project/expert | evidence，绝不冒充直接事实 | 随证据重算 |
| `working` | 当前任务状态、未完成项、临时参数 | session/project | task state | 默认有 TTL |

不得把账单金额、审批状态、截止时间、事项完成状态等有专属结构化业务表的数据仅保存在语言记忆中。记忆只能保存业务记录引用与用户偏好，正式状态仍由业务表负责。

### 6.2 权威等级

从高到低：

1. `user_explicit`：用户原文中的直接陈述或纠正。
2. `tool_verified`：工具实际读取并验证的环境/结果，必须带工具回执。
3. `imported_unverified`：外来导入的低信任数据；hash只证明完整性。所有外来导入先审阅，拒绝archive自报高权威；批准后为user_approved_import，只进evidence。
4. `derived_observation`：由多条来源综合，必须显示“系统观察”。
5. `assistant_unverified`：助手生成内容，只可作为待验证材料，禁止自动成为用户事实。

权威仅表示来源和排序，不授予自动覆盖许可。明确用户纠正且同subject/scope/fact_key唯一匹配才CAS更新；其他冲突进入审阅。用户偏好不能覆盖工具实测环境事实；应分开事实键保留两者。

### 6.3 自动捕获流水线

#### 6.3.0 用户模式、作用域与旧设置兼容的唯一真值表

新 UI 只有一个模式字段和两个作用域字段；不得再把 `memoryEnabled` 与 `captureMode` 同时展示成两个相互矛盾的总开关。公开 wire/Go 字段固定为 `captureMode`、`personalMemoryEnabled`、`projectMemoryEnabled`；SQLite 字段固定为 `capture_mode`、`personal_memory_enabled`、`project_memory_enabled`。

| 模式 | 自动捕获/候选/working/job/index | 用户明确“记住”/主动新增 | 已启用作用域召回与注入 | 管理、纠正、忘记 | 存量数据 |
|---|---|---|---|---|---|
| `auto` | 允许，但还须对应 scope=true 且内部发布 gate 开启 | 允许 | 允许 | 允许 | 保留 |
| `manual` | 禁止；不生成自动 candidate/banner | 通过 `memory.item.create` 直接创建 canonical item | 允许 | 允许 | 保留 |
| `off` | 全部禁止 | 禁止创建；UI 先提示开启 manual/auto | 禁止 | 允许 | 保留且不删除 |

两个作用域开关同时控制该作用域的捕获、显式保存、working 写入、**面向回答的 `memory.search` 检索**、召回和注入；关闭后立即使相关缓存/snapshot 失效，但不删除存量。为了让用户仍能查看和删除已存数据，授权后的 `memory.item.list/get/history`、管理页关键词过滤、export、correct、forget 与 purge 继续可达，不属于回答注入搜索。`personalMemoryEnabled=false` 不影响项目作用域，`projectMemoryEnabled=false` 不影响个人作用域；两者均关闭时显示“当前无启用范围”。workspace/session/expert 等内部 scope 必须由 6.1 的归属规则映射到个人或项目用户开关，不能形成第三个绕过入口。

0160 的确定性迁移规则：`memory_enabled=0 → capture_mode=off`；否则保留合法旧 `capture_mode`，空值按 `auto`；两个新 scope 字段对历史行置 true。为支持用户重新开启时恢复旧偏好，迁移另存 `last_non_off_capture_mode`（仅 `auto|manual`，无值默认 `auto`）。`auto_nominate`、`growth_days` 原值保留在 legacy adapter，一个兼容周期内可由旧客户端读写，但没有生产消费者，不进入新 UI，也不参与 v2 策略。

`memory.settings.get/update` 固定为互斥 `oneOf` 两分支：legacy 分支保留 `version/expectedVersion` 64 位 SHA；R3 分支返回整数 `revision` 并只接受 `expectedRevision`。同一请求不得同时带两个 CAS token。任一分支省略不属于自身的兼容字段时，服务端必须在同一事务内保留数据库当前值，绝不能用零值覆盖；get 结果同时给旧投影 `version` 与 R3 `revision`，具体映射按 03 的兼容矩阵。修改其他既有 canonical 聚合使用对应 `expectedRevision` 与顶层 `idempotencyKey`；`memory.item.create` 是新建操作，只使用 `operationId` 和顶层幂等键，不携带不存在的旧 head revision。

```text
persisted user turn
  → 句段切分
  → 硬拒绝/作用域判断
  → 显式稳定信息 fast path
  → 其余合格句段进入后台批量 extractor
  → JSON Schema 校验
  → 原文 span + digest 重算
  → sensitivity / novelty / conflict gate
  → drop | working | auto_accept | quiet_review
  → append event + canonical projection + index queue
```

#### 6.3.1 输入边界

- 只使用已持久化且 role=user 的真实消息。先持久保存 source job；本地 fast path 在 completed 事件前限时执行，后台提取在终态后运行。取消/失败回合不抹去已持久化用户陈述，但失败工具结果不能标 tool_verified。删除/禁记独立同步执行。源 ID/digest 与顺序见06文档C1。
- 用户原文是个人事实的唯一自动文本来源；助手回复不能作为用户偏好的证据。
- 工具结果走独立 `tool_verified` 入口，必须引用 `callId`、结果 digest 和 verification level。
- 引用块、粘贴文章、角色扮演、系统提示、第三人称陈述默认不属于用户本人。
- 多句输入按句段判断，不能因为末尾有问号而丢弃前面的稳定陈述。例如“我以后都用中文回答，可以吗？”应捕获前半句偏好，而“明天天气如何？”应整句丢弃。
- 回合持久化事务提交后，Engine 以 `source_message_id + source_revision` 幂等写入 `memory_capture_jobs`；现有内存 channel 仅作 wake-up，不是队列真相。worker 每次从数据库 claim 有租约的 job，重新读取原消息、校验 digest、按 cursor 分批，崩溃/队列满/重启均不丢任务；禁止把消息正文复制进 job payload。

#### 6.3.2 硬过滤原因码

服务端必须输出下列稳定原因码；UI 只显示用户友好文字：

```text
greeting
acknowledgement
question_only
lookup_weather_time_news_price
one_off_command
ephemeral_statement
quoted_or_third_party
roleplay_or_instruction_injection
assistant_only
sensitive_secret_or_identifier
empty_or_oversized
duplicate_exact
duplicate_semantic
scope_unresolved
```

硬过滤示例：

| 输入 | 结果 | 原因 |
|---|---|---|
| “你好” | drop | `greeting` |
| “北京明天天气怎么样？” | drop | `lookup_weather_time_news_price` |
| “播放周杰伦” | drop | `one_off_command` |
| “这次 PPT 用蓝色” | `working`，当前 project/session TTL | `ephemeral_statement`，但对当前任务有用 |
| “以后所有 PPT 默认用深蓝色” | auto_accept `preference` | 稳定、未来复用、直接证据 |
| “我的验证码是 123456，记住” | drop 且轻提示未保存 | `sensitive_secret_or_identifier` |
| “我不再用 Python，以后默认 Go” | auto_accept 新版本；旧偏好 superseded | 明确纠正 |

#### 6.3.3 自动保存门槛

候选只有同时满足以下全部条件才可自动成为 active canonical fact：

1. `kind` 属于 profile/preference/goal/constraint/decision/procedure。
2. 存在至少一个可重新读取的用户原文 evidence span，重算 digest 一致。
3. 语句表达未来复用、持续状态或明确身份；仅当前一次任务的信息只能进入 working。
4. sensitivity 不是 secret/credential/government-id/financial-auth/verification-code。
5. 作用域唯一可判定；无法判断 user 还是 project 时进入 quiet review。
6. 不冲突，或同subject/scope/fact_key唯一匹配且有明确纠正语义；后者按head CAS新建版本。含糊冲突进入审阅。完全重复只合并来源，不增加用户访问计数。
7. extractor 返回的 canonical text 不得包含原文 evidence span 中不存在的新实体、数值、否定或时间。

observation必须至少两条独立来源，固定derived_observation；先quiet_review，审阅前任何槽均不注入；用户接受后只能进入evidence，不升为user_explicit。

#### 6.3.4 模型调用与 Token 控制

- 显式 fast path 使用确定性代码，不产生额外模型调用。
- 语义 extractor 是后台可降级能力，复用当前已配置且允许 chat/text 的模型路由，不新增第七种模型角色。
- 单回合不触发独立 extractor；以最多 8 个“硬过滤后仍可能有价值”的用户句段批量处理。
- 批次在会话空闲、会话关闭或累积满 8 句时调度；同一个 source revision 只能处理一次。
- 单批输入上限 4096 tokens、输出上限 768 tokens、最多返回 8 个候选。达到预算后保留未处理游标，不丢数据、不阻塞回答。
- 没有可用模型、预算耗尽或模型失败时，fast path 仍工作；后台任务标记 deferred/failed，可重试，不向用户弹错误对话框。
- 现有 `token_ledger` 的 `subject_type` 闭集且要求 `message_id`，不能凭空写入 purpose。0160 新增 `memory_model_usage`，分别记录 `memory.extract`、`memory.consolidate`、`memory.embed` 的 job/source、provider/model、tokenizer metadata、估算与 provider-reported input/output tokens；主回答用量仍留在现有 ledger，两者可按 source message 关联但不得混算。

### 6.4 Canonical 数据模型

审计快照的最大迁移为 `0159`；下列编号是 06-X0 待核验的连续候选，不在扫描前宣称永久锁定：

- `0160_memory_fabric.sql`
- `0161_memory_retrieval.sql`
- `0162_memory_generations.sql`
- `0163_ocr_model_packs.sql`
- `0164_media_sessions.sql`

X0 必须先扫描当前目标分支的 migration 文件、embed、store manifest 和发布 fixture，生成 `evidence/migration-allocation.json`。若候选号已被占用，只有集成人可在同一合同提交中整体重分配连续区间并同步 00、02–07、11–12、SQL 文件名、测试和 manifest；artifact 合入后号码冻结，子线不得自行顺延。

#### 6.4.1 `memory_content_versions`

一行对应现有 `memory_facts(fact_id, version)` 的规范正文：

```text
fact_id, fact_version                    复合主键及外键
subject_id                               身份隔离键
scope_kind                               user/workspace/project/expert/session
scope_id                                 对应作用域 ID；user scope 固定为 subject_id
kind                                     九类记忆之一
body_ref                                 指向 memory_content_bodies 的相同 fact/version 键
authority                                user_explicit/tool_verified/imported_unverified/user_approved_import/derived_observation
stability                                transient/stable/durable
importance                               0..1；仅用于同类排序，不作为真伪依据
confidence                               0..1；derived 必填，直接事实固定 1
valid_from, valid_to                     事实在现实世界的有效区间，可空
observed_at                              来源事件发生时间
ingested_at                              系统写入时间
origin_plane                             legacy/m8/native/import
origin_id                                原记录 ID
extractor_kind, extractor_model          deterministic/model/import/tool
content_digest                           canonical 内容 SHA-256
created_at                               UTC RFC3339Nano
```

`memory_facts` 的旧不可变元数据不原地修改；`memory_fact_heads` 保存 current_version/revision/is_forgotten，表达当前版本及取代关系。`memory_content_versions` 保存不可变来源与 digest，`memory_content_bodies(fact_id,fact_version,canonical_text,canonical_json)` 保存唯一可注入正文，便于遗忘时清除正文而保留审计元数据。canonical_text 上限 8192 UTF-8 bytes，canonical_json 上限 16384 bytes 且 json_valid。body_ref 是逻辑关联：正文删除后元数据可留存，不能建立阻止遗忘的反向强制 FK；body 到版本的 FK 必须存在。开启 v2 写开关后，缺少 content version/body 的新 fact 必须事务失败。

#### 6.4.2 关系与证据表

- `memory_fact_candidate_links(candidate_id, fact_id, fact_version, relation, created_at)`：显式替代当前隐式关联。
- `memory_evidence_spans(id, fact_id, fact_version, source_kind, source_ref, start_byte, end_byte, quote_digest, created_at)`：`source_kind` 只允许 `user_message|tool_receipt|import_record|user_direct_entry`；定位用户原文、工具回执、导入记录或主动输入，不复制敏感原文。`user_direct_entry` 使用 `source_ref=operation:<operationId>`、start/end 均 NULL、quote_digest 为提交正文 digest。
- `memory_candidate_assessments(candidate_id, kind, decision, reason_codes_json, scope_kind, scope_id, novelty, conflict_fact_id, extractor_json, schema_version, created_at)`：保存自动筛选解释。
- `memory_fact_supersessions(subject_id,scope_kind,scope_id,fact_id,old_version,new_version,effective_at,event_seq)`：同事实版本边与有效终点，FK确保同scope，禁止跨事实链取代。
- `memory_migration_map(origin_plane, origin_id, fact_id, fact_version, state, source_digest, error_code, updated_at)`：支持断点续迁和回滚去重。
- `memory_event_log(event_seq, event_id, subject_id, scope_kind, scope_id, entity_type, entity_id, event_type, payload_json, idempotency_key, occurred_at, recorded_at)`：event_seq 为数据库单调递增整数；append-only 事件仅保存 ID/digest/状态，不保存正文。
- `memory_capture_jobs(job_id, subject_id, source_message_id, source_revision, source_digest, priority, state, attempt, next_attempt_at, cursor_json, lease_owner, lease_until, heartbeat_at, fence, error_code, created_at, updated_at)`：持久捕获队列只保存来源引用/digest；UNIQUE(subject_id,source_message_id,source_revision)，claim/提交需fence校验。
- `memory_model_usage(usage_id, job_id, source_message_id, purpose, provider, model, tokenizer_id, tokenizer_mode, safety_margin, estimated_input_tokens, reported_input_tokens, reported_output_tokens, created_at)`：purpose允许extract/consolidate/embed/query_embed；usage缺失为NULL，累计预占表见06-C2。
- `memory_import_previews(preview_id, subject_id, source_artifact_id, archive_digest, manifest_digest, database_revision, summary_json, state, expires_at, created_at)` 与 `memory_purge_grants(grant_digest, subject_id, scope_kind, scope_id, snapshot_digest, expected_revision, expires_at, consumed_at)`：分别绑定不可变导入源和一次性服务端清理确认。

#### 6.4.3 时间关系与索引表

- `memory_entities(entity_id, subject_id, scope_kind, scope_id, entity_type, canonical_name, aliases_json, created_at, updated_at)`。
- `memory_relations(relation_id, fact_id, fact_version, from_entity_id, predicate, to_entity_id, object_text, valid_from, valid_to, learned_at, invalidated_at, state)`；`to_entity_id` 与 `object_text` 必须且只能有一个。
- `memory_embeddings(fact_id, fact_version, embedding_space_id, model_id, dimensions, vector_blob, vector_digest, state, embedded_at)`；向量维度与 BLOB 长度必须由 `llmadapter.DecodeEmbeddingBLOB` 复核。
- `memory_embedding_jobs(job_id, fact_id, fact_version, embedding_space_id, state, attempt, next_attempt_at, error_code, created_at, updated_at)`：通过敏感/作用域策略后排队，UNIQUE(fact_id,fact_version,embedding_space_id)。历史保留版本可保留已有向量，缺向量先FTS/time降级；空间切换按新space重算，不混算同名模型。
- `memory_recall_hit_details(trace_id, rank, fact_id, fact_version, keyword_score, dense_score, temporal_score, relation_score, feedback_score, fused_score, adopted, reason_code, token_count)`。
- `memory_feedback_events(id, trace_id, fact_id, fact_version, turn_id, outcome, created_at)`；outcome 仅允许 used/unused/helpful/contradicted/user_corrected。

首版不建设通用图数据库。关系查询使用受限 predicate、索引和 SQLite joins/递归 CTE；只有本地规模基准证明不满足要求时，才另立外部图存储 ADR。

embedding_space_id由provider配置ID、endpoint/model、模型revision和dimensions共同派生；密钥不进hash。相同model_id不代表相同向量空间。授权函数处理factId/reviewId/generationId并反查完整scope；禁止继续使用默认always-allow policy。

### 6.5 召回和注入

#### 6.5.1 查询计划

1. 先解析 subject、project/expert/session 作用域、问题时间、实体词和任务类型。
2. SQL 前置过滤 hidden、tombstoned、敏感、错误主体和无效时间版本。当前事实查询只读 current head；显式历史查询按半开有效区间 [valid_from,valid_to) 读取当时有效版本，不能一律排除已被取代的历史版本。遗忘屏障始终优先。
3. 并行取得：FTS top40、dense top40、时间/实体/关系top20。0161的memory_search_documents为external-content shadow，memory_search_fts通过其rowid索引，INSERT/UPDATE/DELETE triggers维护；删除正文同步移除shadow与索引，完整字段见03 Task1。
4. 使用 Reciprocal Rank Fusion 合并不同检索分数，避免把不可比较的 BM25 与 cosine 直接当作同一量纲。
5. 复用 KB 已有的 MMR 思路去重；无 embedding 时按 FTS + temporal 稳定退化。当前 modernc SQLite 没有向量扩展，首版 dense route 不能伪写 SQL cosine：先由 SQL 流式读取已按 subject/scope/请求有效区间/embedding space 过滤的 embedding rows，在 Go 中校验维度并用固定大小 min-heap 保留精确 top 40，内存 O(40×维度数 + 单行向量)。查询 deadline 到时返回已算 best-so-far 并记录 `dense_incomplete`，仍保留 FTS/time 结果；是否引入 ANN 由实测规模另立 ADR。
6. 以当前 provider/model 可解析的 tokenizer 计算候选正文、标签和来源说明；只有 `token.CountTokensForModel` 能解析到内置 tiktoken 编码时才标记 `exact`。未知或不支持的模型使用冻结的 `EstimateTokens` 结果乘 1.15 并向上取整，标记 `estimated`，再按该保守值裁剪。
7. 写入现有 recall trace，并逐条记录采用/未采用原因。

不得再先读取固定 200 条 confirmed candidate 后在 Go 中做词法全量打分。FTS 和时间/关系 SQL 必须真正裁剪候选集；dense 的逐行向量扫描是明确的例外，但只扫描已授权、符合当前或历史查询有效区间、同embedding space的向量，不加载正文且用 O(40×维度数) 有界 heap。索引异常需要记录错误与 fallback，不能静默返回“没有记忆”。

#### 6.5.2 注入权威槽

| 槽 | 内容 | 最大预算 |
|---|---|---|
| Core Profile | 当前用户少量稳定资料和全局偏好，只含 `user_explicit` | 256 tokens |
| Pinned | 与任务直接相关的 constraint/decision/procedure | 512 tokens |
| Working | 当前 session/project 状态 | 384 tokens |
| Evidence | episode/observation/历史版本/工具事实 | 768 tokens |

总记忆注入同时受 `1536 tokens` 和当前可用输入上下文 `8%` 的较小值约束；Companion 总预算为 `512 tokens`。这些是首版产品默认值，必须由设置/配置常量集中定义，不能散落硬编码。超限淘汰顺序为 Evidence → Working 非当前项 → Pinned 非硬约束；Core Profile 中的安全/称呼偏好不得被普通 evidence 挤掉。

每次 trace 必须同时记录 `tokenizerId`、`tokenizerMode=exact|estimated`、`safetyMargin`、原始估算、预算计数、selected/dropped；不得把未知模型的 canonical estimator 描述为“真实 tokenizer”。若 provider 后续提供官方 tokenizer，可在不改变召回合同的前提下注册精确实现并用 golden fixture 校验。

自由记忆文本只能作为低信任数据注入，不能拼进system/developer指令。Core仅允许白名单结构化字段（称呼、语言、输出格式）由模板生成偏好，并服从当前用户及安全规则；working/工具结果/observation/import一律非指令。遗忘边界、最终provider message角色测试见06-C1/C2。

简单寒暄、答案不依赖历史的显式天气/新闻/价格查询，不执行通用记忆召回；若问题使用“我这里”“按我的习惯”等指代，则允许只查 profile/preference。

### 6.6 时间、纠正、冲突和遗忘

- 用户明确纠正创建同一 fact 的新 version，通过 v2 head/取代记录将旧版本标记为 superseded，不修改旧 M8 不可变行。有效区间为半开区间，旧版本有效终点等于新事实 valid_from；如原元数据不可变，由取代记录给出有效终点，不产生人为时间空隙。
- 无法确认是否纠正同一事实时，不关闭旧事实；创建 quiet review 项，UI 并排显示来源和差异。
- “过去我住上海，现在住杭州”必须保留两个有效区间，回答“去年住哪里”和“现在住哪里”可得到不同结果。
- `working` 到期只从在线索引移除，经过保留期后才能物理回收；active 长期事实无自动 TTL。
- 单条“忘记”只处理指定 fact/version 链，不得沿用当前 `fact_id OR scope_id` 的模糊 SQL 删除整个 scope。提交事务必须先写不含正文的 tombstone/digest，再删除或 scrub canonical_text/json、FTS row、embedding、relation object_text、review/extractor payload、仅服务该 fact 的 candidate payload/legacy memory，并从 active generation/overlay/cache 失效；共享 source message 仍属于会话历史，但 Memory 搜索/导出不得再复制或跟随其正文。返回成功前执行读后验证；物理 SQLite page 回收可异步 vacuum，但逻辑和导出不可见必须同步完成。
- “清空项目记忆”和“清空全部个人记忆”是不同高风险操作，均需二次确认、预导出提示、审计和可验证清理结果。
- 禁记规则，例如“不要记位置”，保存为本地 capture policy，不把被禁止的具体位置保存在规则中。

### 6.7 后台整理（Memory Dream / Consolidation）

整理作业必须是 copy-on-write：

```text
active generation G1
  → snapshot source cutoff
  → build G2 in isolation
  → deterministic validation
  → preview diff and metrics
  → activate G2 atomically OR discard G2
  → G1 remains rollbackable
```

触发条件：新增/更正事实累计达到 50 条，或每周一次且存在变化；不得为了定时而处理空变更。任务只在非交互优先级运行。

允许动作：精确重复合并、别名合并、过期 observation 重算、冲突聚类、Core Profile 重建、索引重建。禁止动作：删除唯一来源、把 observation 升为 user fact、改写原始 evidence、自动激活未通过校验的 generation。

`memory_generations` 保存 parent、source cutoff、状态、构建器版本、统计和激活时间；`memory_generation_members` 保存该 generation 采用的 fact/version；`memory_consolidation_jobs` 保存游标、心跳、重试与错误码。状态仅允许 building/ready/active/discarded/failed/archived。任一时刻每个 subject+scope 只能有一个 active generation。

当前事实召回集合为active generation members UNION post-cutoff delta overlay：event_seq>source_cutoff_seq的新事实/更正/墓碑/policy即时覆盖旧member。cutoff用单调序号防同时间戳遗漏。历史查询独立按canonical有效区间取版本，不让current head覆盖历史；两种查询均受当前授权及删除屏障。激活/回退后历史答案必须保持一致。

### 6.8 智能能力入口与记忆中心 UI

设置导航保留稳定 category id `personal`，显示名改为“智能能力”。该 category 的 overview **恰好只有两张同宽摘要卡**：`自动记忆`和`文字识别`；这就是“把现有配置融合到一起”的产品含义，不把两个后端设置揉成一个数据模型。现有聊天“打开记忆”深链直达 memory 详情；普通设置入口先到 overview。OCR 从“路由管理”移除，路由管理只保留模型能力路由。

记忆详情采用**单页极简、低频能力渐进展开**，不做五个常驻页签：

1. 页面头部只有当前状态摘要（例如“自动记忆已开启”）和一个“设置”按钮；三态与两个 scope 开关放进该按钮打开的设置抽屉。抽屉使用现有 Dialog/focus-trap 基础设施，Esc 关闭并把焦点还给按钮。模式与开关的行为只按 6.3.0 真值表，不再显示 `memoryEnabled`、`autoNominate`、`growthDays`。
2. 正文首屏只有搜索与单列有效记忆列表，不放独立筛选按钮。每条只显示正文摘要、作用域和更新时间，右侧“…”提供查看来源、置顶/隐藏、纠正和忘记。类型、时间和来源筛选只在 `高级管理` 的同一抽屉 advanced 视图中按需展开。
3. 冲突、近期变更、历史版本、generation diff、禁记类别、模型辅助/本地隐私选择、完整导出、导入预演和分范围清空统一收进 `高级管理`。只有确有冲突/作用域不明/derived observation 时显示一条克制提示；无待处理项时不显示角标。最近 7 天变更与 24 小时撤销可达，危险清空执行 prepare→明确确认→purge。

overview 不得预加载项目、事实列表、provider 或 OCR 诊断；进入 memory 详情时先加载 identity/settings，只有列表可见时加载 canonical list，展开高级管理后才加载 stats/traces/growth/history。身份解析失败时 fail closed 显示“记忆暂不可用”，不得回退为字符串 `local-user`。个人设置无需先创建项目；只有查看/新增项目记忆时才要求选择项目。

聊天中的轻提示规则：每回合最多一条，2 秒后收起；同一批多个候选合并显示；提供“撤销”与“查看”，不提供逐条“确认”。不调用模型的 deterministic fast path 在回答正文和用户消息都已持久化后、chat terminal event 发出前执行，并通过现有 `completed` event 的 additive 字段 `memoryCapture={count,undoOperationId,expiresAt}` 通知当前页面；它不得延迟首 token。`internal/bridge/protocol.go` 和 `web/src/session/liveChat.ts` 同步扩展并做旧客户端兼容。后台 semantic batch 的结果不尝试向已关闭 stream 追加事件；确定事实进入单列列表，冲突/作用域不明/derived observation 进入高级管理的待确认集合。刷新后不补播旧 toast。撤销调用 `memory.capture.undo`，按 capture operation 原子 tombstone 本批事实。敏感拒绝只有用户明确要求“记住”时才提示“该内容因隐私规则未保存”。

### 6.9 导出、导入和兼容迁移

#### 导出 v2

导出使用单一SQLite read transaction固定snapshotRevision；所有section/count/digest从同一快照流式写独立Memory archive CAS，完成后才返回artifact。默认UTF-8 JSON archive≤64MiB/100000行；超限报EXPORT_TOO_LARGE不截断，整库备份仍由CreateBackup提供。导出必须覆盖：仍有效的 legacy memories、candidates、candidate assessments、facts、content versions/bodies/heads/supersessions、candidate links、source leaves、evidence spans、flags、nominations、recall traces/details、feedback、generations/members、tombstones、capture policies、device clocks/conflicts 和 migration map。对已忘记链只导出 tombstone/digest/time 等无正文元数据，禁止把被 scrub 的 candidate/content/source quote 重新带回。embedding BLOB 默认不导出，可通过 `includeIndexes=true` 显式包含。

导出文件必须包含 `schemaVersion`、`createdAt`、`appVersion`、`subjectId`、每个 section 的行数与 SHA-256。UI 不得再把缺少 legacy 数据的旧格式称为完整备份。

现有 `memory.export` 的 schema/DTO 使用 `additionalProperties:false`，不能无条件换成 v2 manifest。保持空 payload 的 compatibility 响应原样；只在调用方显式传 `format="fabric_v2"` 时走扩展 schema 并返回受授权的 archive artifact metadata。`memory.search/get` 同样通过 compatibility adapter 把 canonical item 映射回既有 `MemoryDTO`，新字段只由 `memory.item.*` 返回。

#### 导入

导入固定两步：用户先把 archive 放入现有受授权、不可变的 attachment/artifact CAS；mutation `memory.import.preview {sourceArtifactId,operationId}` 必须带顶层 idempotencyKey，只写 `memory_import_previews` 元数据而不写 active fact，保存 `previewId + archive/manifest digest + subject + database revision + 24h expiry` 并返回固定 counts/warnings。mutation `memory.import.commit` 必须携带 `previewId/archiveDigest/manifestDigest/expectedDatabaseRevision/operationId` 和新的顶层 idempotencyKey，重新读取同一 immutable bytes；过期、换主体、source 消失或 digest 变化均拒绝。默认不覆盖 active 事实，冲突进入 review；commit 后 preview 标 committed 并释放 source lease，关闭 UI 不等于提交，未提交 preview 由 24h expiry 清理。本期不提供第三个 cancel mutation。

#### 存量迁移

1. 只新增表，不修改/删除旧表。
2. 在旧 schema 验证后、新 migration 执行前创建包含已提交 WAL 的一致数据库副本并验证。当前 `Open/OpenSecure` 会在返回 `Store` 前执行 `initialize/migrate`，所以**不能**先正常 Open 再调用 `Store.CreateBackup`。实现必须增加 `OpenOptions.BeforeMigrate`（同一 raw connection、排他升级锁下调用 backup），或等价拆为 `openRaw → validate legacy → backup → initialize`；失败时不运行迁移并保持原库不变。`maintenance_backup.SnapshotDatabase` 只可作为经测试的实现构件，不得绕过 schema 校验/写窗口合同。迁移后才生成 v2 archive，不要求尚未存在的 v2 导出作为迁移前置。
3. legacy `working` 迁移为 working，保留 TTL；不得自动升为长期事实。
4. legacy semantic/procedural/episodic 生成 migration candidate；只有来源和作用域可验证的低风险记录才能映射，其他进入静默审阅。
5. confirmed M8 candidate 补 content version、显式 link 与 evidence span；digest 不一致标记 migration failed，不猜测修复。
6. 双读阶段按 canonical ID 和 content digest 去重，并在 trace 标出 legacy/canonical 来源。
7. 镜像写通过校验后切 canonical write，再切 canonical read。
8. 至少保留一个兼容发布周期后才允许停止 legacy 写；旧表物理清理由另立迁移决定，不属于本 PRD。

运行时降级优先关闭 auto_capture/hybrid/consolidation，保留 canonical 基础 reader 与删除屏障。已有 v2-only 事实时禁止直接关闭 read 导致新记忆不可见；完整旧读回退须通过无损兼容映射验证。旧二进制可能拒绝新增 schema，不能宣称保留新表即可回滚；完整旧版本恢复需用户明确选择预升级数据库副本，并说明会丢弃升级后的变更。不执行破坏性 down migration。

## 7. PaddleOCR-VL-1.6 可选复杂文档包

### 7.1 定位与路由原则

新增三类本地/远端执行器标识：

| engine id | 定位 | 适用输入 | 是否随主程序可用 |
|---|---|---|---|
| `windows-ocr` | 快速本地文字识别与最终灾备 | 截图、简单扫描、字幕、单栏文字 | Windows 支持条件满足时可用 |
| `paddleocr-vl-1.6` | 高级本地复杂文档解析 | 表格、公式、图表、印章、复杂阅读顺序、畸变页面 | 否，用户主动安装 |
| `provider:<providerId>/<modelId>` | 用户配置的云端视觉/OCR | 按现有 provider 能力 | 取决于配置与健康状态 |

路由不是简单的“首选模型”布尔值。公开 `OCRPolicy` 只有下列 camelCase 字段，schema 必须 `additionalProperties=false`，`fallbackOrder` 必须 `uniqueItems=true,maxItems=3`：

```json
{
  "mode": "auto | local_fast | local_document | provider_first",
  "complexDocumentEngine": "none | paddleocr-vl-1.6",
  "providerId": "optional ULID; 与 modelId 同时出现或同时省略",
  "modelId": "optional non-empty string <= 200 bytes; 与 providerId 同时出现或同时省略",
  "fallbackOrder": ["windows-ocr | paddleocr-vl-1.6 | provider"],
  "sendToCloud": "never | configured_only"
}
```

额外的 schema/handler 约束固定如下：`mode=provider_first` 或 `fallbackOrder` 含 `provider` 时必须同时存在 providerId/modelId 且 `sendToCloud=configured_only`；`fallbackOrder` 含 `paddleocr-vl-1.6` 时 `complexDocumentEngine` 必须为同值。`complexDocumentEngine` 只是复杂页的期望能力，不是 readiness；即使值为 Paddle，resolver 仍须同时检查 pack ready、profile digest 和 `auto_route_enabled`，否则跳过它且绝不下载。只有显式 `mode=local_document` 才要求保存时 Paddle 已 ready。新用户默认值逐字为 `{"mode":"auto","complexDocumentEngine":"paddleocr-vl-1.6","fallbackOrder":["windows-ocr"],"sendToCloud":"never"}`。存量 `preferProvider=false` 迁为该默认；存量 true 且存在完整 provider/model 绑定时迁为 `provider_first + configured_only`，顺序为 `provider,windows-ocr`；绑定缺失时 fail closed 地迁为新用户默认并记录 `LEGACY_PROVIDER_BINDING_REQUIRED`，不得猜测一个 provider。不得因为安装了 Paddle 包而偷偷改变用户的云端隐私选择。

当前 schema 中的 `localEngine=ppocr`、`packRoot` 和 `pack.backend=ppocr-pack` 是 **legacy registration**，不是第四个可执行器。迁移保留其登记信息一个兼容周期，状态固定映射为 `registered_unwired`、`available=false`；旧值的 effective engine 归一化为 `windows-ocr` 并返回 warning，新客户端不得再写 `localEngine=ppocr`。`packRoot` 只在 Engine 内保存，Renderer 不得接收布尔别名 `legacyPackRegistered` 或绝对路径；唯一公开投影是 `ocr.routing.get.legacy`：未登记为 `null`，已登记为 `{engineId:"ppocr",registered:true,state:"registered_unwired",available:false,markerDetected:boolean}`。它绝不自动映射为 `paddleocr-vl-1.6` 的 installed/ready；Paddle 必须通过独立签名 catalog、manifest、probe 和 operation 状态机安装。

`auto` 路由顺序：

1. PDF/Office 有可用文本层：直接使用文本层，不调用 OCR。
2. 简单单图或少量缺字页：`windows-ocr`。
3. 检测到表格、公式、复杂多栏、印章、图表，且 Paddle 包 ready：`paddleocr-vl-1.6`。
4. 用户允许云端且配置健康：按 policy 中的 provider 位置执行。
5. Paddle/provider 失败：逐页回退 Windows；结果明确标记 mixed/partial/uncertain。

复杂度检测器首版只使用确定性信号：页尺寸、文本层覆盖、连通区域/线条密度、列分布、图像占比和用户显式选择。不得调用另一个大模型只为决定是否调用 OCR。

### 7.2 完整流水线要求

官方文档说明 PaddleOCR-VL 完整方案包含版面分析和 VLM 元素识别两个阶段，并警告直接运行 VLM 组件不等于完整流程。[^6] 因此 worker 必须输出：

```text
document
  ├─ page
  │   ├─ page image digest
  │   ├─ orientation/unwarp metadata
  │   └─ ordered blocks
  │       ├─ type: title|text|table|formula|chart|image|seal|...
  │       ├─ polygon/bbox when pipeline supplies it
  │       ├─ literalText
  │       ├─ structuredMarkdown
  │       ├─ confidence when supplied by the responsible stage
  │       └─ engine stage/version
  └─ merged Markdown/JSON
```

`literalText` 与 `structuredMarkdown` 必须分开：生成式结构结果不能覆盖模型识别的纯文本（非原件真值）。任何官方 stage 不提供的 confidence 均返回 null，不得由 Lunitide 伪造。

每个结果至少携带：`engineId`、`engineVersion`、`packDigest`、`page`、`sourceImageDigest`、`complete`、`uncertain`、`warnings[]`、`durationMs`。PDF 混合结果逐页记录实际引擎，不能只给整份文档一个笼统 method。

### 7.3 安装包形态

不把 Python、Paddle 原生库或推理 HTTP 服务加载进 WebView2，也不在用户机器上临时执行 `pip install`。首发只接受一条生产链路：签名 pack 内含固定版本的 Windows x64 CPython、带 hash lock 的 PaddlePaddle/PaddleOCR/PaddleX 依赖、Python JSONL worker、完整 layout + VLM 模型快照、自测样本、LICENSE/NOTICE/SBOM；Go Engine 只负责校验、启动和协议适配。

Engine 从签名 manifest 读取固定相对入口 `runtime/python.exe -I worker/paddleocr_worker.py`，重算入口和关键 DLL digest 后，经 `internal/stdioworker.SpawnIsolatedWithStderr` 启动，使用 stdio JSONL 通信。禁止 pack 自行启动 HTTP 服务。上游首次运行可能自动下载模型，因此 worker 必须显式使用 pack 内本地模型目录并开启离线配置；自测期间发现任何缺失文件或网络下载尝试均判定 pack 不可发布。

在实现下载入口前必须先完成 P09/W0b 的 Windows x64 CPU 可行性门：在干净、无系统 Python、无预热模型缓存的 CI/签名机上构建 pack，断网运行真实 `PaddleOCRVL(pipeline_version="v1.6", ...)` 完整 layout + VLM 冒烟和协议测试，并记录版本、digest、命令、耗时、峰值内存与结果。该门失败时 catalog 中该 runtime profile 保持 disabled，产品只保留 Windows OCR；不得用 fake worker、WSL、Docker 或开发机全局环境宣称“已内置”。

模型包目录：

```text
<LunitideData>/model-packs/ocr/paddleocr-vl-1.6/
  downloads/<operation-id>/          临时下载；失败可清理
  versions/<pack-version>/           不可就地修改
    manifest.json
    LICENSES/
    SBOM.spdx.json
    runtime/python.exe
    requirements.lock
    worker/paddleocr_worker.py
    models/layout/
    models/vlm/
    dictionaries/
    self-test/
  current.json                       由 SQLite 状态重建的启动缓存，不是第二真相源
  quarantine/<pack-version>/         校验/健康失败版本
```

主程序安装包不包含 Paddle 权重。可下载清单必须随 Lunitide 受信任发布渠道签名，并固定：

- pack ID/version/schema/minAppVersion；
- 每个文件 HTTPS URL、精确 byte size、SHA-256；
- 总下载和解压后大小；
- 模型、runtime、字典和 worker 版本；
- 许可证及 attribution；
- 支持的 architecture/device；
- 自测输入/期望结构摘要；
- 清单过期时间和撤销 ID。

签名对象是发布者生成的、与 `manifest.sig` 分离的原始 UTF-8 `manifest.json` bytes；禁止客户端重排 JSON 后再验签。`manifest.sig` 固定 Ed25519、base64url 无填充和 `keyId`。应用内置 active/retired/revoked key 表；未知或 revoked key、过期 manifest、catalog revision/digest 不匹配均 fail closed。发布工具和客户端共享固定 cross-test vectors。

安装器禁止接收用户传入的命令、任意 URL 或任意安装目录。路径必须在受管模型根目录，拒绝绝对路径、`..`、符号链接逃逸、重复文件名和解压炸弹。

### 7.4 安装状态机

```text
安装operation：requested → preflighting → downloading → verifying
             → installing → self_testing → succeeded
允许失败的步骤 → failed；requested/preflighting/downloading可取消 → cancelled
pack availability独立：not_installed / ready / quarantined
更新时旧current仍ready；新版本成功才CAS切current，失败仍用旧current
卸载operation等待lease释放，成功后pack为not_installed；30s仍忙则failed并保留current
```

用户在 UI 看完 manifest 对应体积、硬件要求与 NOTICE 后点击安装，install 请求携带 acceptedManifestDigest 即为该版本确认；不存在无恢复入口的后台 awaiting_consent 状态。同 operationId 重放返回同操作；同 pack 同时只有一个 mutation。版本目录验证并原子 rename 后，事务更新 SQLite current 指针；随后重建 current.json 缓存，缓存更新失败不能反转数据库真相。

### 7.5 进程与资源安全

- worker 由 `internal/stdioworker.SpawnIsolatedWithStderr` 的受限适配器启动；启动前按签名 manifest 重算入口文件 digest，禁止直接 `os/exec`。OCR 专用 quotas 按任务/页 profile 配置，不沿用 MCP 的默认 2 GiB 或整份 Windows PDF 的 90 秒预算。
- 默认并发 1；GPU 版也先保持 1，实测证明安全后才能调整。
- 单页像素、输入 bytes、页数、总任务时间、单页时间、输出 bytes 和临时磁盘均有明确上限。
- `networkPolicy=offline` 的首发含义是应用级离线：worker 协议拒绝 URL，只接收 Engine 创建的本地任务目录；显式设置本地模型路径和离线开关；清空代理变量；不继承 API key、凭据或用户 shell profile；代码、依赖和集成测试不得发起网络调用。模型 pack 只由主 Engine 的受控 downloader 获取。
- 必须明确：现有 Windows Job Object 只能限制进程树/资源并在句柄关闭时回收，**不能阻断 socket，也不是文件系统沙箱**。首发依靠签名的一方 worker、固定依赖和无网络代码路径，不对外宣称 OS 级禁网。若未来把“恶意 worker 也无法联网”列为门槛，须另立 AppContainer/WFP 方案并验证 Paddle 原生 DLL；在那之前 OCR 安全测试只能证明协议、配置和受信任 worker 行为，不能证明内核级 egress 隔离。
- stderr 只允许结构化错误码和脱敏诊断，不输出原文、路径中的用户名、token 或模型输入。
- 进程崩溃、超时、协议版本不符、输出 JSON 超限/非法时立刻终止并将当页标记失败；不得信任部分 JSON 尾部。
- Markdown/HTML 在进入 WebView2 前按允许标签/纯文本策略消毒；公式和表格不允许注入脚本、事件属性、外部资源 URL。

### 7.6 数据表与 Bridge

OCR logical migration `ocr_model_packs`（P00 审计快照下的候选文件名为 `0163_ocr_model_packs.sql`，最终编号只取 `evidence/migration-allocation.json`）新增并由 SQLite 保持唯一状态真相：

- `ocr_pack_state(pack_id, current_version, previous_version, state, active_operation_id, manifest_digest, device_kind, engine_version, installed_at, last_health_at, last_error_code, revision)`：每个 pack 恰好一行；未安装也有状态行；同一 pack 只能有一个 current。
- `ocr_pack_versions(pack_id, version, manifest_digest, install_root_ref, state, installed_at, quarantined_at)`：已校验版本不可变；路径只存 Engine 内部引用。
- `ocr_pack_notices(pack_id, manifest_digest, version, notice_digest, notice_bytes, notice_ref, verified_at, installed_at, uninstalled_at)`：`(pack_id,manifest_digest)` 主键；只有签名 manifest 与 NOTICE bytes 均校验通过后才能插入。`notice_ref` 是 Engine 私有 CAS 引用；catalog 换版或卸载只更新 installed/uninstalled 时间，绝不删除本表或 NOTICE bytes。它是设置/关于页断网查看已受信 NOTICE 的唯一索引。
- `ocr_pack_operations(operation_id, owner_subject_id, pack_id, target_version, accepted_manifest_digest, action, phase, bytes_done, bytes_total, cancel_requested_at, terminal_result_json, error_code, created_at, updated_at, idempotency_key, request_digest, revision, lease_owner, lease_until, heartbeat_at, attempt, fence)`；partial unique index保证同pack至多一个非终态mutation，旧fence不得激活。
- `ocr_version_leases`与`ocr_artifact_leases`按06-C4实现，分别保护加载中的版本与读取中的结果；同属0163，不留给隐式进程变量。
- `ocr_settings(owner_subject_id, scope_kind, scope_id, policy_json, policy_revision, revision, legacy_imported_at, updated_at)`：`(owner_subject_id,scope_kind,scope_id)` 为复合主键；内部 `user` scope 的 `scope_id` 固定等于 `owner_subject_id`，`project` scope 固定等于已授权 project ID，公开响应对 user scope 只返回 `scopeId:null`；主体/范围级路由策略继续使用 SHA `policy_revision` 做 routing CAS，行自身 `revision` 为内部整数版本；`legacy_imported_at` 记录一次性兼容导入。
- `ocr_pack_gates(pack_id, install_enabled, auto_route_enabled, verified_runtime_profile_digest, disabled_reason, revision, updated_at)`：`pack_id` 为主键；本地安装实例级发布开关默认均为 0，digest 默认空，`disabled_reason` 默认 `NO_VERIFIED_RUNTIME_PROFILE`，并使用整数 revision；共享 pack 不随切换主体重复安装，文档结果和策略仍按主体隔离。
- `ocr_legacy_registrations(registration_id, owner_subject_id, scope_kind, scope_id, engine_id, root_ref, marker_detected, state, available, imported_at, revision)`：`engine_id` 固定 `ppocr`、state 固定 `registered_unwired`、available 固定 0；`UNIQUE(owner_subject_id,scope_kind,scope_id,engine_id)`。`root_ref` 仅 Engine 私有，任何 Bridge/result/log/activity 均不序列化。
- `ocr_document_runs(run_id, request_id, owner_subject_id, scope_kind, scope_id, document_digest, policy_revision, gate_revision, mode, provider_id, provider_model_id, pack_manifest_digest, pack_runtime_profile_digest, pipeline_kind, allow_remote, request_snapshot_digest, requested_engine, actual_engines_json, state, pages_total, pages_completed, warnings_json, created_at, updated_at, started_at, finished_at)`；provider 两字段同空或同非空；`request_snapshot_digest` 是上述 immutable request 字段规范序列化后的 SHA-256，`UNIQUE(run_id,request_snapshot_digest)`；created_at 不可变且非空用于活动分页。凭据引用绝不持久化。
- `ocr_page_results(run_id, page, request_snapshot_digest, engine_id, engine_version, actual_pack_manifest_digest, pipeline_kind, source_digest, literal_text_ref, structured_ref, layout_ref, complete, uncertain, warnings_json, duration_ms)`；`(run_id,page)` 主键，并以 `(run_id,request_snapshot_digest)` 外键绑定本次 run 的不可变请求快照；大结构只存 artifact 引用。这样每页通过 join 可证明相同 policy/gate/provider/pack/profile/allowRemote，实际 fallback engine 与 pack digest 仍逐页记录。
- `ocr_artifacts(artifact_id, owner_subject_id, scope_kind, scope_id, run_id, content_ref, sha256, media_type, size, expires_at, created_at)`：正文/结构结果写入独立OCR CAS root的受控存储adapter（不对全局共享CAS做局部GC），本表负责作用域、大小上限、保留期与清理；Bridge 列表只返回有界摘要/引用，不内联整份文档。

迁移实现必须更新 `internal/storage/sqlite/store.go` 的真实 migration `manifest`、`expectedSchemaSQL` 和 expected-column 清单；OCR logical migration 的前置是 allocation 中已冻结的最后一个 Memory logical migration。`current.json` 仅为可删除、可由 SQLite 重建的启动缓存；崩溃恢复以数据库 operation/state/revision 为准。

保留现有 `ocrapp.Result` 和调用者字段，向后兼容地追加 `RunID/PageResults`；需要不同返回形状时新增 `RecognizeDocumentV2`，旧方法只做明确映射，不能直接改坏 KB、模型目录与 Office 调用者。现有 `ocr.routing.get/set` 继续使用其 SHA revision/兼容字段，在此基础上扩展 policy，并新增 Bridge。

所有公开 OCR scope selector 使用同一严格 `oneOf`：`{scopeKind:"user"}` 分支禁止 `scopeId`，Engine 以认证 subject 派生内部 scope ID；`{scopeKind:"project",scopeId:<1..128 bytes>}` 分支必须给出 scopeId 并通过项目授权。`scopeKind` 不得省略，禁止从“当前页面/最后项目”隐式猜测。项目策略行不存在时只读地继承 user policy；`ocr.routing.get/set` 都返回 `requestedScope:{scopeKind,scopeId:null|string}` 与 `policySource:{scopeKind,scopeId:null|string,inherited:boolean}`。继承态 `revision` 是请求 scope、来源 scope、来源 policy revision 的规范摘要；project set 必须在同一事务确认该摘要和“仍无 project row”后创建 override，否则返回 CAS conflict。legacy registration 只读请求 scope 的精确记录，不跨 scope 继承。全局“设置 → 智能能力 → 文字识别”R3 页面固定请求 user scope；项目消费者由 Engine 传入 project scope，不允许 Renderer 自报 subject。

```text
ocr.routing.get    { scopeKind, scopeId?, refreshProbe? }
ocr.routing.set    { scopeKind, scopeId?, policy, expectedRevision }
ocr.pack.get       { packId }
ocr.pack.install   { packId, catalogRevision, acceptedManifestDigest, expectedRevision, operationId }
ocr.pack.cancel    { operationId, expectedRevision }
ocr.pack.uninstall { packId, expectedRevision, confirmed, operationId }
ocr.pack.notice.list { packId, cursor?, limit? } // limit 1..50；列出本机已验证/保留的 NOTICE 摘要
ocr.pack.notice.read { packId, manifestDigest, offset, limit } // 原始byte偏移/长度，limit<=65536
ocr.run.get        { runId, pageCursor?, limit? } // limit<=20，只返回metadata与artifact refs
ocr.run.list       { scopeKind, scopeId?, cursor?, limit? }
ocr.artifact.read  { artifactId, offset, limit } // 原始byte偏移/长度，limit<=65536；返回base64,nextOffset,eof,sha256,totalBytes
```

`ocr.pack.get` 的 result 不得由实现者自行扁平化，固定为下列结构；所有对象（含 preflight/licenseSummary）均 `additionalProperties=false`，字段不得省略，可空字段显式返回 JSON `null`，不存在的 operation/release 也返回 `null`：

```text
pack: {
  packId:string,
  availability: not_installed|ready|quarantined,
  currentVersion:string|null, previousVersion:string|null,
  manifestDigest:string|null, engineVersion:string|null,
  deviceKind:cpu|gpu|null, lastHealthAt:date-time|null,
  lastErrorCode:string|null, revision:int>=1
}
gate: {
  installAllowed:boolean, autoRouteAllowed:boolean,
  reasonCode:string|null, verifiedRuntimeProfileDigest:string|null, revision:int>=1
}
operation: null | {
  operationId:string, action:install|uninstall,
  phase:requested|preflighting|downloading|verifying|installing|self_testing|succeeded|failed|cancelled,
  completedBytes:int>=0, totalBytes:int>=0, cancelRequested:boolean,
  terminal:boolean, cancelAllowed:boolean, errorCode:string|null,
  retryable:boolean, revision:int>=1, createdAt:date-time, updatedAt:date-time
}
release: null | {
  version:string, catalogRevision:string, manifestDigest:string, runtimeProfileDigest:string,
  compressedBytes:int>=0, expandedBytes:int>=0, deviceProfile:cpu|gpu,
  preflight:{state:not_run|compatible|incompatible,reasonCode:string|null,requiredDiskBytes:int>=0,availableDiskBytes:null|int>=0,deviceKind:cpu|gpu},
  licenseSummary:{count:int>=0,spdxIds:string[],nonSpdxLicenseIds:string[]},
  noticeDigest:string, noticeBytes:int>=0
}
```

`terminal` 逐字等于 phase 属于 `succeeded|failed|cancelled`，`cancelAllowed` 逐字等于 phase 属于 `requested|preflighting|downloading` 且尚未请求取消；禁止分别持久化成可漂移真相。preflight schema 必须 `oneOf` 三分支：`not_run` 要求 `reasonCode=null,availableDiskBytes=null`；`compatible` 要求 `reasonCode=null,availableDiskBytes=int>=0`；`incompatible` 要求 `reasonCode=1..128 bytes,availableDiskBytes=int>=0`；三分支的 `deviceKind` 必须等于 release.deviceProfile。`licenseSummary.spdxIds/nonSpdxLicenseIds` 均须 `uniqueItems=true,maxItems=256`，每项 1..128 bytes，count 等于两数组元素总数。`pack.revision` 是 install/uninstall 的 `expectedRevision`；cancel 的 `expectedRevision` 只比较 `operation.revision`；`gate.revision` 只供快照/诊断，handler 每次仍原子重检 gate。没有签名 release/profile 时 `release=null`、`operation=null`、`pack.availability=not_installed`，gate 固定 `installAllowed=false,autoRouteAllowed=false,reasonCode=NO_VERIFIED_RUNTIME_PROFILE`。

NOTICE 生命周期独立于 current pack：catalog verifier 只有在 manifest 签名、manifestDigest、NOTICE digest/bytes 全部通过后，才把 NOTICE bytes 写入受控 CAS 并事务写 `ocr_pack_notices`；安装成功补 `installed_at`，卸载成功补 `uninstalled_at`，均不删除记录。`ocr.pack.notice.list` 按 `verified_at DESC,manifest_digest ASC` 稳定分页，payload 为 `{packId,cursor?,limit?}`、`limit` 默认 20 且范围 1..50，result 固定 `{items,nextCursor}`；item 固定为 `{packId,manifestDigest,version,noticeDigest,noticeBytes,verifiedAt,installedAt,uninstalledAt}`，后两项显式可空，不含路径。`ocr.pack.notice.read` 只接受 list 返回的本机受信 `(packId,manifestDigest)`，result 固定 `{base64,nextOffset,eof,sha256,totalBytes}`，offset/limit 为原始 byte；它不接受路径、URL 或未登记 digest。当前 `release=null` 不影响历史受信 NOTICE 离线读取；只有本机没有该受信记录、NOTICE blob 缺失/损坏或 digest 不一致时才返回 `OCR_PACK_NOTICE_UNAVAILABLE`，不回退网络、不返回旧路径，且数据库、文件和网络 mutation 均为 0。关于页先 list 再 read，因此卸载、catalog 换版和断网后仍能定位所有本机曾验证的许可记录。

下载/解压/校验/自测不能占用普通 Bridge deadline：mutation 仅事务写入 operation 并立即返回 acceptance；Engine 生命周期后台 runner 推进状态，启动时依据 phase 恢复、回滚或隔离 staging。取消只在 requested/preflighting/downloading 阶段设置持久 cancel request；verifying/atomic activation 返回稳定不可取消状态。首版 UI 每 750 ms（后台 3 s，窗口隐藏暂停）轮询 `ocr.pack.get`，终态后停止；不把 `ocr.pack.event` 伪装成普通请求方法。若以后需要推送，另加真实 watch stream 和 reconnect contract。

所有 mutation 沿用 Bridge envelope 的幂等机制；现有 routing 使用 SHA revision，新增 pack 聚合使用整数 expectedRevision。同 key 不同 payload 返回 `OPERATION_REPLAY_MISMATCH`。run/artifact 的读取按 owner_subject_id + scope 授权；共享 pack snapshot 不暴露其他主体的 operation 内容。安装审计使用 m7_audit_events。Windows WinRT 初始化、语言枚举与固定图 self-test 的总 deadline 为 15 秒，结果缓存 5 分钟，refreshProbe 显式刷新，不能只凭操作系统名称返回健康。

新增OCR必须使用ResolvedOCRRequest固定主体/scope/policy/provider/pack快照，所有旧入口与Office/KB/模型目录调用者适配；识别期间禁止再读全局routing。PDF逐页写task dir，独立OCR CAS、lease/fencing和Bridge响应上限512KiB均由06-C3/C4规定，不依赖现有CAS.Get整文件读取。

### 7.7 设置 UI

OCR 入口只存在于“设置 → 智能能力 → 文字识别”详情；“路由管理”不再重复渲染 OCR。详情首屏只呈现两个用户概念：

1. **文字识别 · 自动**：这是当前确定性路由的只读状态摘要，不是总开关，也不能映射为 `preferProvider`。副文案说明系统按文本层、可用本地能力和既有云端授权选择；状态为 `可用 / 需要语言包 / 暂不可用 / 检查失败`，必须来自真实 WinRT 初始化、语言枚举和固定小图 self-test，不能只凭 `runtime.GOOS` 显示勾选。
2. **复杂文档增强 · PaddleOCR-VL-1.6**：展示 `尚未发布可验证运行时 / 未安装 / 安装中 / 可用 / 需处理`。没有签名 catalog/runtime profile 时按钮禁用，原因码 `NO_VERIFIED_RUNTIME_PROFILE`，点击不得发送 mutation。只有 P09/W0b 通过后，安装确认才显示 manifest 的真实下载量、展开磁盘、设备结论、数据流向和许可；ready 必须来自完整 layout+VLM 自测。

“高级与诊断”默认折叠，承载数据是否离开设备、存量 provider/model 选择、Windows 语言、实际 engine/fallback、legacy PP-OCR “已登记但未接线”、签名 manifest/NOTICE、最近运行与故障恢复。`ocr.routing.get`、`ocr.pack.get` 与 `provider.list` 使用独立加载和独立错误；provider 只在展开高级区时请求，失败不得遮蔽 Windows/Paddle 状态。未安装时 policy 可保留 `complexDocumentEngine=paddleocr-vl-1.6` 这一期望值，但 resolver 必须跳过且 UI 不能宣称可用；只有显式 `mode=local_document` 不得在执行器未 ready 时保存。磁盘、CPU/GPU、runtime 不满足时显示准确原因和可行回退。

本期设置融合不新增通用“截图识别/选择文件”工作台。早期视觉探索曾包含这两个按钮，现行 09 原型已移除；正式产品继续由现有文件/Office/附件入口触发识别。若未来增加 OCR 工作台，必须另行定义 Host 文件/屏幕授权、`ocr.run.start` 和结果生命周期，Renderer 不得直接传绝对路径。

### 7.8 OCR 验收基准

建立不少于 200 页的去标识化盲测集，至少覆盖：简中/繁中/英文、UI 小字、低清截图、旋转透视、扫描/弯折/屏摄、票据、字幕、竖排、表格、公式、图表、印章、手写和混合文本层 PDF。

记录但不预先伪造结果：CER/WER、检测 F1/IoU、阅读顺序、表格 TEDS、公式/字段 exact match、虚构字符率、空白页误识率、冷/热 P50/P95、峰值内存、安装体积、失败率和取消时间。

路由发布门槛：

- Windows 简单文本集不得因安装 Paddle 包而回归。
- Paddle 只有在复杂文档质量达到 13.2 节的量化门槛、且资源指标在目标硬件档通过记录的发布评审时，才进入 `auto`。
- 若统计差异不稳定，则 Paddle 保持用户显式选择，不宣称自动更优。
- 每个发布 pack 固定 manifest/dataset revision/engine config；厂商报告分数不能替代该测试。

## 8. 自动工具、媒体会话和 UI 升级

### 8.1 借鉴边界

白龙马 MIT 仓库的媒体界面提供了视频/音乐模式、播放列表、上一首/下一首、seek、音量、封面、LRC 和结束续播等交互参考。[^8] 其 Scene Protocol 的 snapshot、revision、patch、resync、typed intent 思路也适合持续 UI。[^9]

Lunitide 只借鉴产品模式，不复制以下实现：

- `window.*` 全局命令对象；
- 未限定 origin 的 iframe/postMessage；
- 对网络歌词或元数据使用未消毒 `innerHTML`；
- 把平台 iframe、第三方内容、下载能力当作 MIT 代码的一部分；
- 静默 catch 后仍展示成功。

### 8.2 统一工具活动回执

所有有副作用工具逐步采用 `OperationReceipt`：

```text
operationId
toolName
intentSummary
targetDisplay
phase: requested|awaiting_approval|dispatching|verifying|succeeded|uncertain|failed|cancelled
verificationStatus: not_applicable|not_started|pending|confirmed|unconfirmed
verificationSource: none|process|window|uia|smtc|owned_runtime|artifact
evidenceRef: null|string
startedAt/updatedAt/finishedAt
retryPolicy: never|safe_same_id|requires_user
errorCode/userMessage/recoveryActions[]
```

工具 stream 继续用于本回合展示，但最终 receipt 必须持久化并可查询。`ToolTrajectory` 对无专用卡片的工具继续作为回退；媒体使用 `MediaOperationCard`。

自动工具原则：

- 风险分级、审批、急停、桌面串行锁和 capability gate 保持现状。
- “自动”只减少不必要的 UI 操作和提供可靠恢复，不扩大工具权限。
- 可安全重试必须重用相同 idempotency key；play/pause/next 这类非幂等动作在超时未知时不得自动重发。
- 模型只能发 typed intent，不能操纵 Lunitide 自身 DOM。

### 8.3 `MediaSession` 类型

```text
id
ownerSubjectId
scopeKind/scopeId
origin: owned|external
phase: idle|playing|paused|stalled|ended|uncertain|failed|stopped
verificationStatus: none|command_dispatched|verified_playing|verified_paused|verified_ended|verified_stopped
verificationSource: none|smtc|owned_runtime
title/artist/album
artworkArtifactId
positionMs/durationMs
volume/muted
sourceApp/sourceSessionKey
queueRevision
assetId/playbackEpoch/autoAdvance
lastOperationId
revision
createdAt/updatedAt
```

关键不变量：

- `origin=owned` 才允许 Lunitide 承诺 seek、音量、队列、自动下一首和断点续播。
- `origin=external` 的可用命令由 SMTC capability 实时决定；不暴露播放器未声明的 seek/queue。
- `verified_playing` 只能来自自有 `<audio>/<video>` 的实际 `playing` 事件或 SMTC `Verified=true` 且 playback status 为 playing。
- 全局媒体键、打开 URL、启动进程、UIA 点击只能得到 `command_dispatched`；若随后无法回读则终态 `uncertain`。
- 同一 session command 携带 `expectedRevision + operationId + idempotencyKey`；revision 冲突先回读，不盲目覆盖。

状态机：

```text
idle → resolving → awaiting_approval → dispatching → verifying
                                               ├─→ playing ↔ paused → ended
                                               ├─→ uncertain
                                               ├─→ failed
                                               └─→ cancelled
```

### 8.4 内置媒体范围

允许：

- 用户通过文件选择器明确打开的本地音频/视频；
- Lunitide 生成并已登记为 artifact 的音频/视频；
- 用户工作区中经现有路径授权读取的媒体；
- 用户提供的本地歌词文件，或随合法资产生成的歌词。

不允许：

- Agent 自行扫描整盘建立曲库；
- 抓取、下载或转码第三方平台受保护内容；
- 绕过登录、会员、DRM、地区或广告限制；
- 将网页 URL 直接当作可信媒体字节交给 WebView；
- 自动把浏览历史、播放历史写成个人长期偏好。只有重复行为形成的 derived observation 才能进入记忆 evidence，并受静默审阅/关闭控制。

支持格式以 WebView2/Windows 实机能力探测为准；UI 只列出实际 `canPlayType` 或受控后端确认可用的格式，不在 PRD 中虚构全格式支持。

#### 8.4.1 本地大文件登记与播放 URL

现有 `desktop.files.pick/readChunk` 的 100 MiB 与 32 KiB 分块合同适合附件，不适合电影 seek；本功能不得用 Base64 Bridge 循环模拟流媒体。新增 host-owned `media.asset.pick`：公开 payload 同时携带下述严格 MediaScope selector；Host 打开仅音视频类型的系统文件框，验证 regular file、拒绝 symlink/reparse point，读取大小、MIME、mtime 和 Windows volume/file identity；随后通过现有 authenticated private Engine pipe 调用**非 renderer 路由** `internal.media.asset.register`，把 selector 当作未信任业务参数传递。Engine 从认证连接派生 subject，并在登记前校验 project 授权；Host/Renderer 都不能自报 subject。绝对路径只在 Host↔Engine 私有通道和本地数据库出现，公开响应只含 `assetId/title/kind/mime/size/fingerprintRevision/scopeKind/scopeId`，其中 user scope 的 scopeId 固定为 JSON null。

Renderer 通过 engine-owned `media.asset.open {assetId, mediaSessionId}` 请求播放，Engine 做 subject/scope/session/当前 fingerprint 授权后生成随机 opaque ticket，返回：

```text
https://media.lunitide.local/v1/assets/<128-bit-base64url-ticket>
```

该 HTTPS origin 不是网络服务器。`internal/webviewhost` 使用 WebView2 `AddWebResourceRequestedFilter`/`WebResourceRequested` 拦截，Host 经 authenticated private pipe 调用 `internal.media.asset.resolve` 核验 ticket，再从已授权 regular-file handle 返回 `CreateWebResourceResponse`。实现必须支持 `GET`、`HEAD` 和单段 byte Range：200/206/416、`Accept-Ranges`、`Content-Range`、`Content-Length`、受控 `Content-Type`、`X-Content-Type-Options: nosniff`、`Cache-Control: no-store`；拒绝多段 Range、路径/查询拼接、cookie、错误 resource context 和 fingerprint 变化。

ticket 只保存在 Engine 内存，绑定 subject/session/asset；首次使用期限 60 秒，每个成功 Range 续 30 分钟空闲期，硬上限 12 小时，并在 stop、session 删除、身份切换或应用退出时撤销。页面刷新重新 `media.asset.open`；URL、ticket、绝对路径均不得写入 SQLite、聊天、普通日志或 telemetry。Web CSP 只增加 `media-src 'self' blob: https://media.lunitide.local`，不开放 `file:`、通配域、loopback `connect-src` 或 remote frame。

Lunitide 生成/工作区 artifact 先经同一 `media_assets` 注册和作用域检查，再取得 ticket；不能假设现有 artifact preview endpoint 支持 Range。`OwnedMediaPlayer` 按 `kind=audio|video` 渲染 `<audio>` 或 `<video>`，仅实际 `playing/pause/ended/stalled/error/timeupdate` 事件可以推进 owned session 状态。

### 8.5 媒体持久化与 Bridge

`0164_media_sessions.sql` 新增：

- `media_assets`：subject/scope、source_kind=user_selected|artifact|workspace、Engine 内部 source ref、title/kind/MIME/size、volume/file identity 或 artifact digest、state、revision；公开 DTO 永不返回 source ref。
- `media_sessions`：上述 session 快照；播放枚举列唯一命名为 `phase`，含 asset_id/playback_epoch/auto_advance（创建时从默认策略快照）；revision 单调递增。
- `media_operations`：工具意图、阶段、验证、evidence、幂等键、错误与时间。
- `media_queue_items`：`item_id,media_session_id,asset_id,order_index,state,created_at,updated_at`；只引用已登记且同 subject/scope 的 `media_assets.asset_id`，`UNIQUE(media_session_id,order_index)`。不得另存 artifact/path/source_ref 或未经授权的远端 URL；显示元数据从 asset 的安全投影读取。
- `media_bookmarks`：owned 资产的 position/duration/updated_at；播放结束可清理。
- `media_settings`：本地 singleton 发布 gate 与首发 auto_advance=true 策略，整数 revision 并 CAS；不存用户播放历史，历史在按主体隔离的 sessions/bookmarks 中。
- `media_player_leases`：`media_session_id,window_instance_id,navigation_epoch,generation,lease_token_digest,expires_at`；`lease_token_digest` 是 `leaseToken` 的唯一数据库命名，不再使用 nonce_digest/token_digest 同义列；同session只有一个owner；另以应用级audio focus CAS保证至多一个owned audible session。
- `media_player_commands`：operation、session、generation、asset_id/playback_epoch及绝对desired state、claim/ack/expiry；Engine先提交指令再由owner消费。
- `media_audio_focus`：singleton_id、owner_subject_id、media_session_id、playback_epoch、focus_token_digest、reason、revision、updated_at；CAS保护全应用单一owned发声，焦点规则见06-C5。

公开 Renderer Bridge（进入 envelope method enum/schema）。所有下列 MediaScope selector 使用同一 strict `oneOf`：user 分支 `{scopeKind:"user"}` 且禁止 scopeId，Engine 内部以 subject 作为 scope_id；project 分支 `{scopeKind:"project",scopeId:<1..128 bytes>}` 且必须授权。scopeKind 不得省略，也不允许从当前 route 或最后打开项目推断。Media Center 默认使用 user scope；从 project/workspace 的“在媒体中心打开”动作携带该已授权 project ID。所有 get-by-ID 方法仍由 Engine 反查 subject/scope，不接受重复 scope 参数：

```text
media.session.get     { mediaSessionId }
media.session.list    { scopeKind, scopeId?, cursor?, limit? }
media.session.watch   { scopeKind, scopeId? }   // 真实 StreamingHandler
media.session.create  { assetId, queueAssetIds?, scopeKind, scopeId?, operationId }
media.session.command { mediaSessionId, action, positionMs?, volume?, expectedRevision, operationId }
media.queue.command   { mediaSessionId, action, itemId?, beforeItemId?, expectedQueueRevision, operationId }
media.asset.pick      { multiple?, scopeKind, scopeId? }   // x-owner=host；Host透传selector，Engine鉴权登记；不返回路径
media.asset.list      { scopeKind, scopeId?, mediaSessionId?, sourceKind?, cursor?, limit? } // sourceKind=user_selected|artifact|workspace
media.asset.open      { assetId, mediaSessionId }
media.operation.get   { operationId }
media.operation.list  { scopeKind, scopeId?, cursor?, limit? }
activity.list         { scopeKind, scopeId?, domains?, cursor?, limit? }
media event payload   { mediaSessionId, revision, phase, verificationStatus, verificationSource, nowPlaying?, operation? }
```

Host-private internal RPC（只进入 authenticated private pipe allowlist，不生成公开 schema，不得由 Renderer 调用）：

```text
internal.media.player.attach   { mediaSessionId, operationId, windowInstanceId, navigationEpoch }
internal.media.player.next     { mediaSessionId, leaseToken, generation, windowInstanceId, navigationEpoch }
internal.media.player.report   { mediaSessionId, leaseToken, generation, windowInstanceId, navigationEpoch, assetId, playbackEpoch, eventSeq, operationId?, event, positionMs?, durationMs?, errorCode? }
```

`action` 限定为 play/pause/toggle/stop/previous/next/seek/set_volume/mute/unmute。参数不适用于动作时 schema fail-closed，例如 `seek` 必须有非负 `positionMs`，`play` 不接受 position。

`media.queue.command.action` 单独限定为 move/remove/clear/jump：move 要求 `itemId`，`beforeItemId` 可空表示末尾；remove/jump 要求 `itemId`；clear 禁止 item 字段。它按 `expectedQueueRevision` CAS，不把队列动作混入播放 action 枚举。`media.session.create` 创建 owned idle snapshot；`media.asset.open` 只为已存在且已授权的 session 签发 ticket，不隐式创建会话。

`media.session.watch` 必须走现有 StreamingHandler/streamId/sequence 机制；media event 是 `bridge.Event` 的 typed payload，不创建带 `x-method=media.event` 的普通请求 schema。event 只携带已提交 revision 的 invalidation/有界 patch，不能触发任何播放命令；断线、sequence gap 或 revision 跳号立即用 `media.session.list/get` 替换本地 snapshot。

owned 播放闭环固定为 Engine command 事务 → player.next 领取绝对状态指令 → owner 执行 → player.report 回执 → 更新 session/watch。`internal.media.player.attach/next/report` 由 Host 经既有 `internalRuntimeHandlers` 私有调用并附加 Host 生成的 windowInstanceId/navigationEpoch；它们不使用公开 schema/x-owner 路由，Renderer 不得指定 owner。attach从可信上下文取得window ID，lease 30 秒、每 10 秒续租；同 owner 再次 attach 续租，换 owner 必须等过期并递增 generation。next 在未 ack 前可重放同指令，客户端按 operationId 去重；next/previous 的目标队列项由 Engine CAS 只计算一次，不能重放相对跳转。report只接受当前lease/generation、assetId/playbackEpoch、递增eventSeq；每次装载/重播增epoch，自然ended按session+epoch去重，命令回执operationId必填；客户端不得自报 verified=true。播放 Promise resolve 不构成成功证据，必须有实际 playing 事件；autoplay 被阻止显示手动播放按钮。timeupdate 最多每秒上报一次。刷新恢复快照但不自动出声。

操作状态与播放状态分离：`media_operations.phase` 使用 requested/awaiting_approval/dispatching/verifying/succeeded/uncertain/failed/cancelled；`media_sessions.phase` 使用 idle/playing/paused/stalled/ended/uncertain/failed/stopped。上方时序图中的 resolving 表示 requested 阶段内的解析步骤，不是额外数据库枚举；两组状态不能合并，SQL、Go domain、Bridge DTO 与 React snapshot 都使用同名 `phase` 字段。

现有 `media.play` 保持 Agent tool 名称和参数兼容：已登记 `assetId/sessionId` 走 owned/typed session；命名查询、用户明确 URL 和 browser/foreground/app 目标继续作为 `origin=external` 经过原 ToolRuntime/审批/急停路径。外部路径可以打开搜索页或外部播放器，但不下载、不抓取、不加入 owned queue；在匹配 SMTC 证据出现前最多为 `command_dispatched/uncertain`。外部调用方不需要同时理解两套工具。

播放器挂在App共同根的持久MediaProvider，`MediaCenterPage`与条件式`MediaMiniPlayer`仅订阅同一 snapshot；切会话/设置/项目不卸载audio/video。整页刷新恢复暂停。TTS/麦克风音频焦点、同应用单一发声及外部媒体提示见06-C5，属于本期P0/P1而非后续优化。

### 8.6 UI 组件

#### `MediaCenterPage`、`MusicPlayerSurface` 与 `VideoPlayerSurface`

左侧“办公”分组新增一级入口“媒体中心”，不藏在设置或聊天工具轨迹中。媒体中心是完整播放界面：音乐与视频使用两个独立表面但共享同一 Engine snapshot、队列和权限合同。音乐表面突出封面、标题、播放控制、进度和队列；视频表面提供真实 16:9 画面、字幕/音量/进度及全屏入口。切换表面不得复制播放状态或启动第二个 audible session。

无播放会话时，媒体中心显示选择本地合法资产/已登记 artifact 的空状态，应用其他页面不渲染任何播放器。创建 idle session 本身不等于开始播放；只有用户在媒体中心明确开始播放并得到 playing/paused 等有效 snapshot 后，才进入“当前播放会话”。

#### `MediaMiniPlayer`

用户从媒体中心开始播放后，离开媒体中心时在内容区右下显示约 60–66 px 高的迷你播放器；返回媒体中心时隐藏迷你层，避免同一控件重复。首发迷你播放器只保留封面、两行内标题、只读紧凑进度、播放/暂停和关闭；上一首、下一首、可拖动 seek、队列、音量、倍速、字幕等操作回到媒体中心完成。它不能横跨整个应用底部，也不能遮挡聊天输入框、审批或系统提示。

暂停只改变 session 为 paused，保留会话、位置和迷你播放器；用户点击关闭才执行既有 `stop` command、释放当前 ticket/播放焦点并从活动 UI 中移除当前会话，历史 operation/bookmark 仍按数据策略保留。stopped/ended 且无可继续队列、failed 或不存在 active session 时不显示迷你播放器。整页刷新只恢复 paused snapshot，不自动出声。

#### `MediaQueueDrawer`

只对 owned session 开放。支持拖动排序、移除、清空、跳转；每次变更带 queue revision。外部播放器显示“队列由外部应用管理”，不展示假队列。

#### `MediaOperationCard`

在聊天工具轨迹中显示“准备目标 → 等待授权 → 已发送 → 正在核验 → 已确认/无法确认/失败”，并提供依据，例如“Windows 媒体会话报告正在播放”。不显示底层脚本、密钥或用户绝对路径。

#### `PlaybackStatus`

verified 与 uncertain 的文字+图标双编码只在媒体中心、迷你播放器和相关操作卡片内复用，不在 SessionPage 再常驻一个重复播放组件。

#### `ActivityStatusButton` 与 `ActivityCenter`

把长任务、OCR 安装、模型下载和媒体操作统一为可恢复活动列表；入口固定在应用顶部状态区，以按钮打开 ActivityCenter 弹层/抽屉，不占左侧主导航。无运行、失败或待处理活动时保持安静，不用持续数字角标制造负担；有进行中或需要恢复的活动时显示文字/图标状态，不能只靠颜色。媒体播放控件由媒体中心和条件式迷你播放器负责，不把所有状态强塞进 ToolTrajectory。

首发不再造第四套活动数据库。现有operation.list没有跨scope/cursor/时间DTO，不能承担完整活动中心。新增 `activity.list` 聚合已有工具 journal、OCR operations/runs、media_operations；request 的 `domains` 是可选 unique array，元素只允许 `tool|ocr|media`、1..3 项，省略表示全部，空数组拒绝；scope 使用 8.5 的 strict oneOf，limit 默认 50、范围 1..100。前端只通过下列 exact DTO 展示，稳定分页、授权、去重及真实后端文件在06文档C6/X3定义：

```text
ActivitySnapshot {
  activityId, domain: tool|ocr|media, kind,
  phase: queued|awaiting_approval|running|verifying|succeeded|uncertain|failed|cancelled,
  terminal,
  verificationStatus: not_applicable|not_started|pending|confirmed|unconfirmed,
  verificationSource: none|process|window|uia|smtc|owned_runtime|artifact,
  title, completedUnits: null|integer, totalUnits: null|integer,
  errorCode: null|string, retryable,
  recoveryAction: none|retry|cancel|open_settings|open_player|select_asset,
  scopeKind, scopeId: null|string,
  createdAt: null|date-time, updatedAt: null|date-time, rootOperationId: null|string
}
```

DTO 的上述字段全部 required；nullability 不得改成省略。`terminal` 只由 normalized phase 是否属于 `succeeded|uncertain|failed|cancelled` 派生。completedUnits/totalUnits 必须同为 null 或满足 `0<=completed<=total` 且 total>0。DTO不含正文、OCR文本、路径、URL、歌词或底层异常。OCR安装靠snapshot轮询；media/tool事件触发刷新activity.list及详情get。活动分页与权限由后端聚合负责。web/src/activity/activitySnapshot.ts为唯一DTO归属；缺失旧时间返回 null、UI 显示“未知”，不编造时间。

### 8.7 视觉与交互规范

- 沿用 Lunitide 现有颜色、圆角、字体和层级 token，不复制白龙马品牌视觉。
- 媒体中心可使用现有薄荷绿—蓝灰—电光紫品牌光场作为单一主视觉；普通设置和列表保持纯黑/近黑背景，不把渐变铺满所有卡片。
- 迷你播放器桌面高度约 60 px、右下悬浮，宽度随标题有界；680–959 px 折叠次要信息，<680 px 可退化为带安全边距的底部浮层，但不得在桌面端横跨全宽或遮挡输入/审批。
- 进度条需键盘可操作；按钮具备中文 aria-label、focus-visible、disabled 和 loading 状态。
- 动画只用于迷你播放器出现/退出、面板展开和状态过渡；遵守 `prefers-reduced-motion`。
- 封面缺失使用统一占位，不从未知域加载远程图片。
- 歌词使用纯文本节点或严格消毒后的结构；搜索高亮不得使用未消毒 HTML。
- 每个失败状态都给出一个真实恢复动作：重试核验、打开外部播放器、切换本地资产、查看设置或取消；没有可恢复动作时明确结束。

### 8.8 媒体和工具验收

- 发送全局 play 键但无 SMTC 回读：必须为 uncertain，不得出现“正在播放某歌曲”。
- SMTC 标题与目标不匹配：不得把另一个应用的媒体会话算成功。
- owned `<audio>/<video>` 触发 `playing` 后才能 verified；`error/stalled` 正确进入 failed/uncertain；大文件 seek 必须产生正确单段 Range 响应而不是 Base64 全量读入。
- 页面刷新/离开/回到会话后，以 `media.session.get/list` 恢复同一 revision 和进度，不重复执行 play/next。
- 无当前播放会话时，聊天/办公/设置/项目/AgentHub 页不渲染播放器；从媒体中心开始播放后离开该页显示右下约 60 px MiniPlayer，暂停后仍显示并保持位置，关闭后 stop、释放 ticket/焦点并隐藏。音乐/视频表面及 MiniPlayer 使用同一 snapshot，页面间不产生第二路声音；AgentHub 的替换侧栏与 44px shell header 不得遮挡 MiniPlayer、顶部 Activity 或焦点环。
- 活动中心只能从顶部状态入口打开，不占左侧“办公”一级导航；长任务失败后从该入口仍能定位证据和真实恢复动作。
- revision 冲突、重复 operationId、快速连点、急停、播放器退出、文件被移走、格式不支持均有确定终态。
- 恶意标题、歌词、封面 URL、媒体路径不能执行脚本或越过工作区/用户选择授权。

## 9. 对外契约与失败语义

本节给出产品层不可再由开发自行改意的契约。Go 结构、Bridge schema 和 TypeScript 生成类型必须来自同一个 schema 源；前端不得用 `any` 接住新增响应。

### 9.1 通用写操作信封

所有会改变记忆、模型包或媒体会话的 Bridge 调用统一使用现有 Request envelope 的顶层 `idempotencyKey`。创建用户可见操作的方法在 payload 携带 `operationId`；只有修改已存在、具有 CAS 版本的目标聚合时才携带该方法定义的 `expectedRevision`。例如 `memory.item.create` 没有旧 head 可比较，明确不带 `expectedRevision`；每个方法仍以 9.2～9.4 的精确表为准。典型“修改既有聚合”信封为：

```json
{
  "idempotencyKey": "client-generated-stable-key",
  "payload": {
    "operationId": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
    "expectedRevision": 7
  }
}
```

- `operationId` 是一次用户可见操作的 ULID，用于审计与活动中心关联。
- 异步 mutation 的 `activityId` 不生成第二个业务 ID，固定逐字等于已接受的 `operationId`；`ActivitySnapshot` 的稳定键是 `(domain,activityId)`。非 mutation 的 OCR run/工具 journal 投影使用各自稳定 run/journal ID 作为 `activityId`，并始终带 `domain`。
- 同一个 `idempotencyKey`、同一主体、同一方法重复调用，返回第一次提交的终态；不得重复保存、重复安装或重复跳到下一首。
- 有 revision 的聚合必须执行 CAS。Memory、pack 和 media 新聚合使用正整数 revision；兼容的 `ocr.routing.*` 与 `memory.settings.*` legacy 分支继续使用当前 64 位小写十六进制 revision，`memory.settings.*` R3 分支使用整数 revision，二者由 adapter 映射但不得在同一请求混用。冲突返回最新 snapshot；新方法用 `REVISION_CONFLICT`，现有接口保留其当前冲突码，不自动覆盖。
- 每个响应都包含 `requestId`；异步操作另含 `activityId`。
- 删除、取消、卸载等操作只有落库后才返回成功；排队成功使用 `accepted`，不能写 `completed`。

### 9.2 Memory Bridge

保留现有 `memory.search/get/export/purge`，但其实现切到 canonical 读模型；新增：

```text
memory.item.list
memory.item.get
memory.item.history
memory.item.create
memory.item.correct
memory.item.forget
memory.capture.undo
memory.review.list
memory.review.resolve
memory.generation.list
memory.generation.preview
memory.generation.activate
memory.generation.discard
memory.import.preview
memory.import.commit
memory.purge.prepare
```

generation/import 的公开合同逐字如下；scopeId 对 generation.list 仍使用 user 禁止/project 必填的 strict oneOf，所有对象 `additionalProperties=false`，完整 DTO 和错误码以 06-C1 为唯一技术合同：

| 方法 | payload | result |
|---|---|---|
| `memory.generation.list` | `scopeKind,scopeId?,cursor?,limit?` | `items,nextCursor,databaseRevision` |
| `memory.generation.preview` | `generationId,cursor?,limit?` | `generation,changes,nextCursor` |
| `memory.generation.activate` | `generationId,expectedRevision,operationId` + 顶层 idempotencyKey | `generation,databaseRevision,operationId` |
| `memory.generation.discard` | `generationId,expectedRevision,operationId` + 顶层 idempotencyKey | `generation,databaseRevision,operationId` |
| `memory.import.preview` | `sourceArtifactId,operationId` + 顶层 idempotencyKey | `previewId,sourceArtifactId,archiveDigest,manifestDigest,databaseRevision,expiresAt,counts,warnings,operationId` |
| `memory.import.commit` | `previewId,archiveDigest,manifestDigest,expectedDatabaseRevision,operationId` + 顶层 idempotencyKey | `previewId,state,importedCount,reviewCount,skippedCount,conflictCount,databaseRevision,operationId` |

关键请求：

`memory.item.create` 是 manual 模式和记忆中心“主动新增”的唯一 canonical 写入口。公开 payload 不接受 `subjectId`、`expectedRevision`、kind、authority、confidence、validFrom 或 expiresAt；公共字段仅为 `scopeKind` (`user|project`)、项目作用域时的 `scopeId`、`operationId`，正文来源再做禁止额外字段的严格 `oneOf`：高级抽屉“手动新增”只传 `text` 且禁止 `sourceRef`；用户消息“保存为记忆”只传 `sourceRef` 且禁止 `text`。`sourceRef` 自身也是严格 `oneOf`：`{messageId}` 表示整条已持久化 user message，Engine 重读原文并解析为 `[0,len(UTF-8 bytes))`；`{messageId,startByte,endByte}` 表示该消息内显式半开 UTF-8 byte span。两个偏移必须同时出现，只出现一个或落在非 UTF-8 边界均以 `MEMORY_SOURCE_INVALID` 拒绝。sourceRef 分支的 canonical 正文只能由 Engine 解码该原始 span 得到，客户端无权另传正文，因此不存在“无关消息冒充证据”；当前用户消息旁入口固定使用整条消息形式，局部文本入口今后只能使用三字段形式，不得靠客户端显示文本反推偏移。Engine 从认证身份和真实 source receipt 解析 subject/evidence；直接 text 分支的 evidence `source_kind` 固定为 `user_direct_entry`，不得伪造聊天引用。实现计划中的 `explicit_ui` 是 operation receipt 的 `receipt_kind`，不是第二个 evidence source_kind，也不得写入 `memory_evidence_spans.source_kind`。服务端用本地确定性分类选择 kind，不能唯一判断时固定回退 `episode`，并以提交时间作为 `valid_from`、`expires_at=NULL`，不得依赖远端模型才能保存。服务端执行敏感过滤、scope 授权、去重和幂等后固定返回 `{item,databaseRevision,undoOperationId,undoExpiresAt}`；undo 窗口为 24 小时并遵守后续修改冲突规则。`off` 返回 `MEMORY_MODE_OFF`，对应 scope=false 返回 `MEMORY_SCOPE_DISABLED`，不得静默保存。

```json
{
  "method": "memory.item.correct",
  "payload": {
    "factId": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
    "replacementText": "以后默认使用简体中文回答",
    "validFrom": "2026-09-14T00:00:00Z",
    "reason": "user_correction",
    "operationId": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
    "expectedRevision": 3
  }
}
```

`memory.item.forget` 必须区分：

- `this_version`：必须带targetVersion与expectedRevision；删除该版本正文，若其为current则恢复未忘记的有效前版本，否则head为空；
- `fact_history`：墓碑化该事实的全部版本；
- `subject_rule`：删除命中项，并增加“此类信息不再保存”的 deny rule；
- `scope_all`：不允许 `memory.item.forget` 执行；先调用 mutation `memory.purge.prepare {scopeKind,scopeId?,expectedDatabaseRevision,operationId}` 并携带顶层 `idempotencyKey`，scopeId 遵循 user 禁止/project 必填规则。服务端持久化 grant，返回 `{counts,snapshotDigest,confirmationToken,expiresAt,operationId}`；confirmation token 5 分钟、一次性且数据库只存 digest。同 key 同 payload 重放首次 receipt，同 key 变参返回 `OPERATION_REPLAY_MISMATCH`。随后现有 `memory.purge {confirmationToken,snapshotDigest,expectedDatabaseRevision,operationId}` 必须携带新的顶层 `idempotencyKey`，原子 consume grant 后清理并返回 deletion receipt。prepare 与 purge 使用两个不同的 operationId；空 payload 或只通过浏览器 `window.confirm` 的请求一律返回 `PURGE_CONFIRMATION_REQUIRED`，不得直接删除。

memory.capture.undo在24小时内撤销capture operation仍拥有的版本；任一项已被后续修改则整批MEMORY_UNDO_CONFLICT、零删除。撤销纠正可恢复未被遗忘的前版本head；同幂等键重放原结果。

列表项至少返回：

```text
factId, currentVersionId, kind, text, authority, confidence,
scopeKind, scopeId, validFrom, validTo, state, sensitivity,
sourceCount, lastRecalledAt, createdAt, updatedAt, revision
```

`text` 只有通过主体、作用域和敏感度策略后才返回；被墓碑化条目只返回元数据和删除时间。

### 9.3 OCR Bridge

保留 `ocr.routing.get/set`，新增：

```text
ocr.pack.get
ocr.pack.install
ocr.pack.cancel
ocr.pack.uninstall
ocr.pack.notice.list
ocr.pack.notice.read
ocr.run.get
ocr.run.list
ocr.artifact.read
```

`ocr.pack.install` 请求仅接受 catalog 中的 `packId=paddleocr-vl-1.6`、`catalogRevision`、`acceptedManifestDigest`、`expectedRevision` 和 `operationId`，服务端从白名单选择版本；不接受任意 URL、任意 pip 参数或任意本地命令。通过 gate 后的接受响应固定为（通用 envelope 外层仍按现有 Bridge 编码）：

```json
{
  "requestId": "d70133df-f677-4c14-9ea7-b46b8df92c78",
  "operationId": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "activityId": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "accepted": true,
  "packId": "paddleocr-vl-1.6",
  "phase": "requested",
  "packRevision": 8,
  "operationRevision": 1
}
```

这里 `activityId === operationId` 是强约束。uninstall 采用同形 receipt；cancel 不创建第二个 operation，固定返回 `{requestId,operationId,activityId,cancelRequested,phase,operationRevision}`，其中 activityId 仍等于目标 operationId。gate 未通过时返回稳定错误而非 receipt，并且 operation、activity、下载、staging 与 runner job 的新增数均为 0。

首发安装进度由 7.6 节冻结的 `ocr.pack.get` 四段 snapshot 轮询恢复，UI 只读 `pack.availability`、`gate.*`、`operation.phase/completedBytes/totalBytes` 与 `release.*`，展示百分比只能由两个 byte 字段派生，不得再造扁平 `state/stage/progress` DTO。设置/关于页的 NOTICE 只能经有界 `ocr.pack.notice.list→ocr.pack.notice.read` 发现并读取；网络错误正文或本地绝对路径不得送往 UI。`ocr.run.list` 必须分页、有 scope，并且只返回有界摘要；大正文经授权 artifact 读取。

OCR 识别结果统一返回：

```text
engineId, engineVersion, pipelineKind, pageCount, coveredPages,
plainText, markdownArtifactId, layoutArtifactId, warnings,
fallbackChain, startedAt, finishedAt, confidenceAvailable
```

`confidenceAvailable=false` 时不得伪造 0–1 置信度；具体 block 可没有 confidence。

### 9.4 Media Bridge

采用 8.5 节的方法名。`media.session.command` 的响应不是简单布尔值，而是：

```json
{
  "requestId": "d70133df-f677-4c14-9ea7-b46b8df92c78",
  "operationId": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "activityId": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "mediaSessionId": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "phase": "verifying",
  "verificationStatus": "pending",
  "verificationSource": "none",
  "revision": 12,
  "acceptedAt": "2026-09-14T00:00:00Z"
}
```

异步媒体 command 同样满足 `activityId === operationId`。`phase=verifying` 已表达命令完成派发并等待证据；接受 receipt 不能把“已派发”塞进核验来源，所以固定为 `verificationStatus=pending,verificationSource=none`。终态经 `media.event` 或重新读取 snapshot 获得：有实际证据时 operation 变 `confirmed` 并填写来源；超时无证据时 operation 变 `unconfirmed` 且 phase=`uncertain`。外部播放器无法回读不是失败，也不是成功播放。MediaSession 自身使用 8.3 的播放专用 verificationStatus 闭集，不能与 operation/activity 的通用闭集互换。

### 9.5 稳定错误码

| 子系统 | 错误码 | 是否重试 | UI 行为 |
|---|---|---:|---|
| 通用 | `REVISION_CONFLICT` | 先回读 | 刷新最新状态，保留用户尚未提交的输入。 |
| 通用 | `SCOPE_DENIED` | 否 | 不显示受限正文；说明作用域不允许。 |
| 通用 | `OPERATION_REPLAY_MISMATCH` | 否 | 同一幂等键参数不一致，要求重新发起。 |
| 通用 | `PRE_MIGRATION_BACKUP_REQUIRED` | 修复配置后重试 | 已有库待升级但没有一致备份 hook；停止启动，不执行 migration。 |
| 通用 | `SETTINGS_VERSION_CONFLICT` | 先回读 | 保留旧 OCR routing 的 SHA CAS 语义，刷新后让用户重试。 |
| Memory | `MEMORY_EVIDENCE_MISMATCH` | 否 | 不晋升；进入隔离并显示“来源校验失败”。 |
| Memory | `MEMORY_SOURCE_INVALID` | 否 | 不保存；来源不存在、角色错误、被修改或 span 不是合法 UTF-8 边界。 |
| Memory | `MEMORY_SCOPE_DENIED` | 否 | 不泄露事实是否存在；客户端来源或目标 scope 未获授权。 |
| Memory | `MEMORY_SCOPE_DISABLED` | 用户开启后可重试 | 对应个人/项目范围已关闭；零写入、零索引。 |
| Memory | `MEMORY_MODE_OFF` | 用户开启后可重试 | 记忆模式关闭；显式保存和自动捕获均为零。 |
| Memory | `MEMORY_SETTINGS_CONFLICT` | 先回读 | 设置 revision/version 冲突；保留本地草稿，不静默覆盖。 |
| Memory | `MEMORY_UNDO_CONFLICT` | 否 | 批次中已有后续修改；整批不撤销，不做部分删除。 |
| Memory | `PURGE_CONFIRMATION_REQUIRED` | 完成 prepare 后重试 | 必须使用未过期、同主体/范围/revision 的一次性确认 grant。 |
| Memory | `EXPORT_TOO_LARGE` | 改用备份 | 超过 64 MiB 或 100000 行 portable archive 上限；不返回截断导出。 |
| Memory | `MEMORY_CONFLICT_REVIEW_REQUIRED` | 否 | 对话不中断；送静默审阅箱。 |
| Memory | `MEMORY_INDEX_UNAVAILABLE` | 是 | 回退 canonical recent/FTS；记录 degraded，不阻断聊天。 |
| Memory | `MEMORY_IMPORT_DIGEST_MISMATCH` | 否 | 禁止 commit，列出受影响记录数。 |
| OCR | `OCR_PACK_NOT_INSTALLED` | 否 | 路由到 Windows/云端并提示可选安装。 |
| OCR | `OCR_PROBE_REQUIRED` | 是 | 仅用于 P01→P07 兼容窗口：显示“尚未检查”，绝不标 ready；P07 接线后以六态 probe 取代。 |
| OCR | `NO_VERIFIED_RUNTIME_PROFILE` | 否 | 禁用 Paddle 安装；不得创建 operation、下载目录或 worker。 |
| OCR | `OCR_LEGACY_ENGINE_UNWIRED` | 否 | 拒绝把 PP-OCR 登记保存为执行偏好；保留 Windows 可用路径。 |
| OCR | `OCR_SCOPE_REQUIRED` | 否 | 内部旧调用缺可信作用域；不借用全局身份。 |
| OCR | `OCR_SCOPE_FORBIDDEN` | 否 | run/artifact 不属于当前主体或范围；不泄露存在性。 |
| OCR | `OCR_OPERATION_NOT_CANCELLABLE` | 否 | 正在校验/原子激活；显示当前阶段并等待终态。 |
| OCR | `OCR_PACK_BUSY` | 是 | 当前版本仍有运行租约；不强删 DLL，稍后重试。 |
| OCR | `OCR_PACK_NOTICE_UNAVAILABLE` | 否 | 本机无该受信 NOTICE 或保留 blob 校验失败；刷新 NOTICE 列表并保持该条许可入口禁用，不联网补取。 |
| OCR | `OCR_ARTIFACT_MISSING` | 条件式 | 显示结果已过期/不存在，不返回半个正文。 |
| OCR | `OCR_ARTIFACT_CORRUPT` | 否 | 隔离损坏结果，不进入上下文；允许重新识别源文档。 |
| OCR | `OCR_MANIFEST_UNTRUSTED` | 否 | 删除 staging 引用，禁止启动 worker。 |
| OCR | `OCR_HASH_MISMATCH` | 是 | 丢弃本次 staging；允许从零重试。 |
| OCR | `OCR_DISK_INSUFFICIENT` | 条件式 | 显示所需与可用字节，不启动下载。 |
| OCR | `OCR_RUNTIME_UNSUPPORTED` | 否 | 保留 Windows OCR；显示实际缺失能力。 |
| OCR | `OCR_WORKER_TIMEOUT` | 是 | 终止 Job Object；本次按路由策略回退。 |
| OCR | `OCR_OUTPUT_INVALID` | 是 | 隔离原输出，不进入上下文；回退并记录版本。 |
| Media | `MEDIA_SESSION_NOT_FOUND` | 条件式 | 刷新会话；可重新选择目标。 |
| Media | `MEDIA_CAPABILITY_UNAVAILABLE` | 否 | 禁用对应控件，不尝试模拟。 |
| Media | `MEDIA_TARGET_MISMATCH` | 是 | 不报播放成功；重新解析目标。 |
| Media | `MEDIA_UNVERIFIED` | 是 | 显示“命令已发送，无法确认”，提供重新核验。 |
| Media | `MEDIA_AUTOPLAY_BLOCKED` | 用户手势后重试 | 不假报播放；显示明确“点击播放”。 |
| Media | `MEDIA_SOURCE_GONE` | 否 | 标记队列项不可用并让用户重新选择。 |

低价值记忆被硬过滤是正常决策，不是用户错误；只写聚合指标 `capture_drop_total{reason}`，不创建包含原文的错误记录。

## 10. 完整功能需求与验收编号

优先级定义：P0 为对应子项目首发阻断项；P1 为首个稳定版必须交付；P2 进入后续增强，不阻断前三个实施计划完成。

### 10.1 Memory Fabric v2

| ID | P | 需求 | 可验证验收 |
|---|---:|---|---|
| MEM-001 | P0 | 建立 canonical 正文版本和 candidate/fact 显式映射。 | 任一注入项可从 fact → current version → source leaf → 原始消息/工具回执单向追溯；无 candidate payload 旁路注入。 |
| MEM-002 | P0 | 默认启用静默智能自动保存，移除逐条确认。 | 连续普通聊天不出现确认弹窗；保存后最多一个可撤销 toast。 |
| MEM-003 | P0 | 硬过滤问候、感谢、天气/价格等即时查询、一次性命令、纯助手文本和敏感正文。 | 固定负例集逐类通过；secret canary 0 条正文/embedding/事件 payload 落库。 |
| MEM-004 | P0 | 明确稳定资料、偏好、目标、约束、纠正和可复用流程可自动保存。 | 固定正例集输出正确 kind、scope、authority 和来源；重复输入不制造当前事实副本。 |
| MEM-005 | P0 | 每次晋升前真实读取来源并重算 digest。 | 修改/缺失源消息的测试必须失败为 `MEMORY_EVIDENCE_MISMATCH`。 |
| MEM-006 | P0 | 用户/工作区/项目/会话作用域在查询、索引、导出和 UI 一致执行；个人/项目用户开关同时门控该归属范围的捕获、显式保存、working、搜索、召回和注入。 | 跨主体/全部 scope 隔离测试 100% 拒绝；个人关时个人全链调用为0且项目仍可用，项目关反之，两者关时捕获/注入均为0；缓存键含主体/scope/settings revision 并在切换后失效。 |
| MEM-007 | P0 | 用户纠正创建新版本并关闭旧版本有效期。 | “我不再用 A，改用 B”后只注入 B；历史页可见 A 的有效期和替代关系。 |
| MEM-008 | P0 | 单条遗忘、主题禁记与整库清理均可验证。 | 删除后 canonical、FTS、dense、缓存和当前生成均不可召回；导出只含墓碑元数据。 |
| MEM-009 | P1 | 查询采用 FTS、dense、时间、关系与 pinned 多路候选，再统一重排和去冗余。 | 关闭任一路由时仍有可预期降级；同一 fact 不因多路命中重复注入。 |
| MEM-010 | P1 | 使用模型可解析的精确 tokenizer；不支持时使用加 15% 裕量的 canonical estimator。 | 每次 trace 记录 tokenizerId/mode/margin、available/selected/dropped；unknown model 不标 exact，最终注入不越过 6.5.2 上限。 |
| MEM-011 | P1 | 写入与召回反馈闭环但不自证。 | recall/citation/correction/dismiss 只影响排序或审阅；不能独立把 observation 晋升为直接事实。 |
| MEM-012 | P1 | 后台整理为 copy-on-write generation。 | 取消/崩溃/校验失败不改变 active generation；激活是单事务 CAS。 |
| MEM-013 | P1 | “智能能力”两卡入口与记忆详情提供三态/双 scope 设置、主动新增、最近自动保存、搜索、来源、时间线、审阅、纠正、撤销和隐私设置。 | overview 恰好两卡；设置抽屉与单列列表按需加载；页面刷新可由 snapshot 恢复；关键操作键盘和读屏可完成。 |
| MEM-014 | P0 | 完整 v2 导出、导入预演和提交。 | 存量 `memories` 和治理平面都被 manifest 覆盖；round-trip 后有效/current/删除语义一致。 |
| MEM-015 | P0 | 存量迁移可重入、可审计、可安全降级。 | 同一迁移重复三次无新增副本；降级 hybrid 后 canonical 基础读取仍可见 v2-only 事实；旧接口不能复活已忘记数据。 |
| MEM-016 | P1 | 明确离线退化行为。 | 无提取模型时确定性显式规则仍工作；其余进入不含敏感正文的 pending 计数，不阻断回答。 |
| MEM-017 | P1 | 生成式总结不得改写权威来源。 | generation 表只引用输入版本；discard 后无残留 active 指针。 |
| MEM-018 | P1 | 禁记策略先于模型调用。 | deny rule 命中时 extraction 调用计数为 0，且不生成 embedding。 |

### 10.2 OCR

| ID | P | 需求 | 可验证验收 |
|---|---:|---|---|
| OCR-001 | P0 | Windows OCR 保持零额外模型下载的默认本地路径，但可用性必须实测；Paddle 包默认未安装。 | 全新安装不下载模型；真实 WinRT probe 覆盖 ready/缺语言包/初始化失败/超时/非Windows，UI 不凭平台名称假报 ready；现有识别回归全过。 |
| OCR-002 | P0 | 模型包只能从签名 catalog 安装，并校验每个文件 digest。 | catalog/manifest/文件任一被篡改均不能进入 ready。 |
| OCR-003 | P0 | 安装支持预检、可见进度、取消、失败清理和重试。 | 每个状态转移有持久化 snapshot；重启后不会把半包标记 ready。 |
| OCR-004 | P0 | 运行完整 layout + VLM pipeline，并准确报告 pipelineKind/version。 | 缺少 layout 组件时健康检查失败；不得报告 `paddleocr_vl_full`。 |
| OCR-005 | P0 | 签名一方 worker 采用应用级 offline policy，受超时/内存/进程/输出限制且协议只接受显式任务引用；不得把 Job Object 描述为网络/文件沙箱。 | URL/越权引用被协议拒绝，worker 不继承凭据/代理，断网干净机完整 pipeline 可运行；输出炸弹/挂死被终止；测试报告明确 OS egress 不在首发保证内。 |
| OCR-006 | P0 | 路由按主体请求快照、文本层、任务类型、安装健康和策略确定性选择；旧 PP-OCR 登记永不成为执行器。 | 同一输入与配置得到同一路由解释；识别中切主体/设置不改变本次快照；legacy marker 的执行调用数为0；结果包含完整 fallbackChain。 |
| OCR-007 | P0 | 任一 Paddle 故障不得破坏 Windows/云端路径。 | 缺包、校验失败、启动失败、超时、崩溃分别通过故障注入。 |
| OCR-008 | P1 | 输出区分纯文本、结构化 Markdown、layout blocks 和警告。 | 表格/公式/印章等无法可靠识别时显式 warning，不伪造置信度。 |
| OCR-009 | P1 | “智能能力→文字识别”以只读自动状态+可选增强呈现；高级区说明云端授权、下载、磁盘、设备要求、legacy 登记、隐私和卸载影响。 | OCR 不再重复出现在路由页；provider 失败不遮蔽本地状态；数值来自签名 manifest/实机 preflight，不使用硬编码宣传数。 |
| OCR-010 | P0 | 盲测决定自动路由，不以厂商指标代替。 | 7.8 节数据集和报告入库；未过 gate 时只提供手动使用。 |
| OCR-011 | P1 | 卸载安全处理活动作业和历史结果。 | 有运行任务时先取消并等待终态；删除模型文件但保留结果元数据和 license notice。 |
| OCR-012 | P1 | 安装包许可证/NOTICE 可离线查看。 | catalog、安装目录和关于页均能定位版本、来源、许可证与修改声明。 |

### 10.3 自动工具与媒体

| ID | P | 需求 | 可验证验收 |
|---|---:|---|---|
| TOOL-001 | P0 | 工具回执区分接收、执行、核验和终态。 | 任一 UI 成功文案都能定位对应 verification evidence。 |
| TOOL-002 | P0 | 延续现有审批、能力门、急停、串行锁和审计。 | 新媒体入口不能绕过 ToolRuntime；急停后未提交操作取消。 |
| TOOL-003 | P1 | 活动 snapshot 与 event 可重连。 | WebView 刷新后不重复执行，恢复同一 operationId/终态。 |
| TOOL-004 | P1 | 用户可从失败卡片执行真实恢复动作。 | 恢复操作创建新 operation 并关联父 operation，不重写历史。 |
| MEDIA-001 | P0 | `MediaSession` 统一 owned 与 external，但能力分型。 | external 未声明的 seek/queue 控件不出现或 disabled。 |
| MEDIA-002 | P0 | 修复“只发送媒体键即报成功”。 | 无 SMTC 回读时终态 `uncertain/MEDIA_UNVERIFIED`。 |
| MEDIA-003 | P0 | owned 播放器仅处理被授权本地/生成资产。 | 路径越权、未知远端 URL、脚本型元数据和失效 artifact 被拒绝。 |
| MEDIA-004 | P1 | owned 支持播放、暂停、关闭、seek、音量、队列、自动下一首和断点。 | 浏览器事件驱动状态；暂停保留 active session/位置，关闭映射 stop 并释放资源；刷新后恢复但不未经手势自动出声。 |
| MEDIA-005 | P1 | external 控制使用 SMTC 优先，全局键/UIA 是低置信兜底。 | 目标元数据不匹配不得 verified；播放器退出得到确定终态。 |
| MEDIA-006 | P1 | 快速操作 CAS + 幂等。 | 双击 next、乱序 event、旧 revision command 不造成两次跳转。 |
| MEDIA-007 | P1 | MediaCenter/MiniPlayer/QueueDrawer/OperationCard 共享同一 snapshot。 | 音乐与视频为独立表面但 title/phase/revision 一致；无 session 时其他页面不显示播放器，paused 显示、close 后隐藏且无各自本地真相。 |
| MEDIA-008 | P1 | 播放历史默认不写长期偏好。 | 单次播放不产生日志正文记忆；只有满足 MEM derived observation 规则才进入审阅。 |
| MEDIA-009 | P0 | 不实现第三方受保护内容抓取/下载/绕过。 | 工具 schema 不存在相应动作；URL allowlist 测试 fail-closed。 |
| MEDIA-010 | P0 | 本地歌曲/电影使用 host 私有登记和 WebView2 Range resource broker，不经 Base64 Bridge 暴露大文件。 | Renderer/API/日志拿不到绝对路径或 ticket 映射；GET/HEAD/单 Range 通过，multi-range、过期 ticket、换文件、跨 scope 和 traversal 得到确定拒绝。 |
| UI-001 | P1 | ActivityCenter 汇总 OCR 安装、长工具和媒体操作，并从顶部状态入口打开。 | 不占左侧主导航；支持 running/failed/uncertain 筛选，页面重开可恢复。 |
| UI-002 | P1 | 状态不能只靠颜色。 | 图标、文字、ARIA live 与焦点行为通过 axe/键盘测试。 |
| UI-003 | P1 | 沿用现有纯黑/近黑 design tokens 和中文文案规范；智能能力 overview 恰好两卡，窄屏设置导航收进抽屉。 | 不引入第二套全局 reset、字体或白龙马品牌资产；390/1024/1440 三视口及 100/150/200% 缩放无横向滚动或主操作遮挡。 |
| UI-004 | P1 | 不展示密钥、完整绝对路径和未消毒 HTML。 | 注入测试、快照测试和日志审计均通过。 |

## 11. 数据迁移、发布顺序与回滚

### 11.1 迁移分配与冻结

下表是审计 HEAD（当前最大 migration 为 0159）上的**候选分配**。X0 必须扫描 `migrations/`、`migrations/embed.go`、`store.go` manifest 和已发布数据库，生成并提交唯一 `docs/design/jiyishengji/evidence/migration-allocation.json`；只有该文件与 schema 测试同批通过后编号才冻结。若开工前已被占用，由集成人一次性重分配整个连续区间并机械更新 00、02–07、11–12、文件名、测试名和 manifest；子线不得自行顺延。

| Migration | 所属 | 内容 |
|---|---|---|
| `0160_memory_fabric.sql` | Memory | canonical versions/bodies/heads、links、evidence、assessment、migration map、capture jobs/cursor/policy、usage、import preview/purge grant、settings、事件。 |
| `0161_memory_retrieval.sql` | Memory | entities、relations、embeddings、recall hit detail、feedback。 |
| `0162_memory_generations.sql` | Memory | consolidation generations、members、validation、active pointer。 |
| `0163_ocr_model_packs.sql` | OCR | pack state/versions/operations、受信 NOTICE 索引、OCR settings gate、scoped runs/pages/artifacts。 |
| `0164_media_sessions.sql` | Media | authorized assets、sessions、operations、queue items、bookmarks、media gates、player leases/commands。 |

迁移只新增表、索引和可空列；首版不 drop/rename 旧表。每个 migration 必须加入 schema dump/upgrade/全新建库三类测试。分配文件至少含 `logicalName/number/fileName/previous/maxObserved/head/trackedDiffDigest/untrackedManifestRelativePath/untrackedManifestDigest/baselineManifestRelativePath/allocatedAt`；`worktreeDiffDigest` 为废弃别名，新 artifact 禁止写入；CI 校验文档和 manifest 不存在第二套编号。

### 11.2 Feature flags

| Flag | 默认 | 作用 |
|---|---:|---|
| `memory_v2_write` | off | 在旧写入成功后写 canonical v2；对用户不可见。 |
| `memory_v2_read` | off | canonical基础读取；v2-only内容存在时禁止关闭，降级关hybrid/consolidation。 |
| `memory_v2_auto_capture` | off | 启用静默自动筛选与 toast。 |
| `memory_v2_hybrid_recall` | off | 启用 dense/time/relation 与重排。 |
| `memory_v2_consolidation` | off | 启用后台 generation。 |
| `ocr_paddle_pack_install` | off | 展示安装入口。 |
| `ocr_paddle_auto_route` | off | 允许策略自动选 Paddle；盲测前保持 off。 |
| `media_session_v2` | off | 创建持久媒体 session/operation。 |
| `activity_center_v2` | off | 展示统一活动中心。 |

这些发布开关没有可直接复用的通用持久化 store：Memory 开关写入 0160 的 memory_v2_settings，通过现有 M8 settings 服务兼容访问；OCR 安装/自动路由 gate 写入 0163 的 ocr_pack_gates，主体策略写入 ocr_settings；Media/Activity 开关写入 0164 的 media_settings。全部由 Engine CAS 管理；禁止只放环境变量、进程内 gauge 或前端 localStorage。

### 11.3 依赖波次

| 波次 | 工作 | 入口条件 | 退出门禁 |
|---|---|---|---|
| W0a 通用真实性与冻结基线 | 生成 P00 的 HEAD/diff/untracked/migration allocation/升级前备份基线；冻结评测集；完成 P01 的媒体 false-success 与 OCR legacy 假 ready 修复；记录旧召回基线。 | 当前主干测试可运行。 | 通用基线可复现；allocation/备份证据落盘；真实性修复测试通过。此门不依赖 Paddle。 |
| W1 数据底座与 Windows OCR 诚实基线 | 按 allocation 执行五个 logical migrations；实现 typed storage、Bridge schema、scoped OCR snapshot 与真实 Windows probe；Paddle gate 默认 disabled。 | W0a。 | 新建/升级/重启/并发 CAS 通过；Windows 六态真实；无 profile 的 Paddle mutation=0。 |
| W0b Paddle 独立可行性门 | 在干净断网 Windows x64 CPU 上构建并运行完整 Paddle layout+VLM pack，形成签名、许可、资源与故障证据。 | W0a；W1 的 OCR scoped/gate 基线。可与 Memory/Media 后续波次并行。 | 通过则产生 `verifiedRuntimeProfileDigest` 并允许 W5；失败则记录 `BLOCKED/INCOMPLETE`，仅阻止 Paddle 专属工作，Windows OCR、Memory、Media 继续。 |
| W2 Memory 影子写与基础读 | canonical link、真实 evidence resolver、存量 backfill；先 shadow，再启用 canonical 基础 reader/删除屏障。 | allocation 中 Memory Fabric/Retrieval logical migrations。 | 迁移可重入；影子差异报告无 scope 泄漏；v2-only 事实可读。 |
| W3 Memory 自动捕获 | 硬过滤、批量提取、去重、审阅箱、撤销。 | W2；安全集完成。 | MEM-002～008、014～016 通过。 |
| W4 Memory 新召回 | hybrid、token ledger、时间/关系、generation。 | W3；allocation 中 Memory Retrieval/Generations logical migrations。 | 离线评测过 gate，灰度 shadow recall 无越权。 |
| W5 OCR 可选包 | catalog、异步安装器、Python worker launcher、手动路由、盲测。 | W0b/P09 通过并产生匹配 digest；allocation 中 OCR logical migration；发布签名与 NOTICE 流程可用。 | OCR-002～005/007/010～012 的 Paddle 专属门通过；自动路由仍 off。 |
| W6 媒体与活动 UI | MediaSession、owned player、SMTC 验证、独立媒体中心、条件式 MiniPlayer 与顶部 Activity 入口。 | allocation 中 Media logical migration；TOOL-001/002。 | MEDIA/UI 全部 P0/P1 验收。 |
| W7 灰度与稳定 | 按主体灰度新读、自动路由、后台整理。 | 各子项目 gate 独立通过。 | 回滚演练、资源/隐私/长稳测试通过。 |

W3 必须在 W2 基础读通过后开始；W0b、W5、W6 可按各自前置条件与 Memory 工作并行。W0b 失败只阻止 W5/P10 和 Paddle 自动路由，不阻止 W1 的 Windows OCR 诚实基线或其他模块。W4 使用 W2/W3 的 canonical 数据和反馈，不能让自动记忆先于可读取的数据层上线。Paddle 自动路由只能在对应硬件档的盲测通过后单独开启。

### 11.4 回滚原则

- **代码回滚**：先在支持新 schema 的当前二进制关闭自动捕获/高级召回/自动路由/UI；不承诺旧严格 schema 二进制能直接打开新增表数据库。完整旧版本恢复使用用户明确选择的预升级备份，并告知升级后变更损失。
- **Memory 写回滚**：关闭 `memory_v2_write` 后停止新 v2 写；已写内容只读保留。只有 v2 产生、且用户已在 v2 UI 修改的记录必须经显式 down-conversion 工具后才能回旧写，不静默丢失。
- **Memory 读降级**：关闭 hybrid/consolidation 后保留 canonical 基础 read 与删除屏障。关闭 v2 read 前必须验证全部存量无损兼容映射；有 v2-only 事实则拒绝该切换。遗忘所需的旧正文清理不可逆转或复活。
- **OCR 回滚**：关闭 auto-route，停止 worker；模型包可保留或由用户卸载，Windows OCR 不受影响。
- **Media 回滚**：停止创建 v2 session；现有 `media.play` 回原路径，但 W0a/P01 的 truthfulness 修复不得回滚。
- **数据库恢复**：上线前对用户数据库做现有机制支持的备份；恢复演练验证 schema version、foreign key、FTS rebuild 和 digest。

## 12. 文件级实施边界

以下是目标文件边界；三份配套实施计划会把它们拆成逐测试任务。若开发发现现有同职责文件，应在计划评审中合并职责，不能额外再造平行 service。

### 12.1 Memory

修改：

- `internal/domain/m8core/memory.go`、`memory_ops.go`、`user_memory.go`：扩充 canonical 类型、捕获决策和版本/时间语义。
- `internal/m8app/memory.go`、`memory_autoaccept.go`、`memory_inject.go`、`memory_ops.go`：切断 candidate payload 旁路，接入 capture/recall/generation。
- `internal/app/chat_memory.go`、`chat_memory_workers.go`：回合批量捕获、注入预算和异步队列。
- `internal/app/m8_memory_handlers.go`、`m10_memory_handlers.go`、`m10_memory_ops_handlers.go`：新增 typed Bridge。
- `internal/storage/sqlite/m8_memory.go`、`m10_memory_ops.go`、`upgrade_schema.go`：canonical 存取、CAS、迁移。
- `internal/contextapp/*`：接入 tokenizer 预算，不改变已有输出保留不变量。
- `internal/domain/token/provider_tokenizer.go`：在现有 exact/fallback 行为上返回 tokenizer metadata；未知模型应用 1.15 保守裕量且不标 exact。
- `web/src/memory/MemoryPage.tsx`、`MemoryOpsPanel.tsx`、`web/src/m8/PrivacyConsole.tsx`：收敛为单页记忆中心入口；低频管理能力由高级抽屉按需承载。
- `web/src/settings/settingsNav.ts`、`SettingsPage.tsx`、`web/src/m8/PersonalIntelligencePage.tsx`、`web/src/app/navStore.ts`、`web/src/App.tsx`：保留 `personal` category/深链，落地“智能能力”两卡 overview 与 `overview|memory|ocr` 详情；OCR 从 routing 去重。修改前先隔离并合并用户当前 dirty 变更。

新增：

- `migrations/0160_memory_fabric.sql`
- `migrations/0161_memory_retrieval.sql`
- `migrations/0162_memory_generations.sql`
- `internal/m8app/memory_capture.go`
- `internal/m8app/memory_recall_v2.go`
- `internal/m8app/memory_consolidation.go`
- `internal/m8app/memory_import.go`
- `internal/storage/sqlite/m8_memory_v2.go`
- `internal/storage/sqlite/m8_memory_retrieval.go`
- `internal/storage/sqlite/m8_memory_generations.go`
- `web/src/memory/MemoryList.tsx`
- `web/src/memory/MemoryItemMenu.tsx`
- `web/src/memory/MemoryAdvancedPanel.tsx`
- `web/src/memory/MemorySettingsPanel.tsx`
- `web/src/memory/memorySettings.ts`
- `web/src/settings/SmartCapabilities.css`（组件作用域样式；不另建全局主题）

### 12.2 OCR

修改：

- `internal/ocrapp/recognize.go`、`route.go`、`health.go`、`pack.go`：完整路由、真实 Windows health、legacy PP-OCR 只登记语义、结果和回退。
- `internal/storage/sqlite/store.go`：把 0163 加入真实 migration manifest、`expectedSchemaSQL` 和 expected-column 校验。
- `internal/bootstrap/wire.go`：装配 SQLite repository、catalog verifier、installer、后台 runner、CAS artifact store 和 worker launcher。
- `internal/app/s1_bridge_handlers.go`、新增 `ocr_wire.go`：兼容 routing 合同并新增 pack/NOTICE/run Bridge。
- `web/src/settings/OCRRouting.tsx`：先作为兼容 adapter，随后收敛到 `OCRSettingsPanel`；核心状态独立加载，provider 只在高级区按需加载。
- `internal/storage/sqlite/upgrade_schema.go`：注册 0163。

新增：

- `migrations/0163_ocr_model_packs.sql`
- `internal/storage/sqlite/ocr_model_packs.go`
- `internal/ocrapp/catalog.go`
- `internal/ocrapp/installer.go`
- `internal/ocrapp/worker.go`
- `internal/ocrapp/result.go`
- `internal/doctext/ocr_probe_windows.go`、`internal/doctext/ocr_probe_other.go`
- `workers/paddleocr-vl-1.6/paddleocr_worker.py`
- `workers/paddleocr-vl-1.6/protocol.schema.json`
- `workers/paddleocr-vl-1.6/requirements.lock`（发布构建时转为逐 wheel hash lock）
- `scripts/build-paddleocr-vl-pack.ps1`、`scripts/test-paddleocr-vl-pack.ps1`
- `web/src/settings/OCRModelPackCard.tsx`
- `web/src/settings/OCRSettingsPanel.tsx`
- `web/src/settings/OCRAdvancedPanel.tsx`
- `web/src/settings/OCRRecentRuns.tsx`
- `api/bridge/v1/ocr.pack.notice.list.schema.json`、`api/bridge/v1/ocr.pack.notice.read.schema.json`
- `testdata/ocr-eval/README.md` 及不含版权受限内容的固定评测 manifest。

Git 保存可审查的 worker 源、协议和构建锁；受管 CPython、wheel、DLL 与模型资产只进入签名 pack，不提交临时虚拟环境。若 P09/W0b 官方运行时验证证明所选 Windows 后端不可用，安装入口保持 flag off，并更换 catalog runtime profile；不得用未声明的 WSL/Docker 依赖冒充内置安装成功。

### 12.3 工具、媒体和 UI

修改：

- `internal/toolruntime/media.go`、`media_foreground.go`、`runtime.go`：MediaSession adapter 和验证语义。
- `internal/winexec/media_session_windows.go`、`media_windows.go`：能力与 SMTC snapshot，不改变低置信 fallback 的事实等级。
- `cmd/desktop/main.go`、`internal/hostbridge/gateway.go`：装配 host media picker/resource broker 与 authenticated private Engine RPC；公开 Gateway 不暴露 internal media 方法。
- `internal/webviewhost/*`、`web/index.html`：注册 `media.lunitide.local` resource interception、Range 响应和窄化 CSP。
- `internal/app/media_receipt_test.go` 对应的生产 handler：注册 Media Bridge 和 event。
- `web/src/session/liveChat.ts`、`ToolTrajectory.tsx`、`SessionPage.tsx`：活动恢复和 OperationCard。
- `web/src/settings/ComputerPanel.tsx`：显示共用权限和急停状态，不复制设置。
- `web/src/app/appTypes.ts`、`web/src/app/navStore.ts`：把 `media` 加入唯一 `Page` union/route state，不另建媒体导航 store。
- `web/src/app/LaunchSidebar.tsx`、`LaunchSidebar.test.tsx`：在现有“办公”折叠组加入常驻一级“媒体中心”，纳入 `officeOpen`/active 计算；不加入 `officeMenuSettings` 开关，也不把 AgentHub shell switch 放回办公组。
- `web/src/App.tsx`、`App.test.tsx`、`web/src/styles.css`：在当前三条 early-return/root 装配外层接唯一 Media runtime，在主 route ternary 接 `page==='media'`，接入顶部 Activity/MiniPlayer 并验证 AgentHub replaceMainNav/header 避让。
- `internal/storage/sqlite/upgrade_schema.go`：注册 0164。

新增：

- `migrations/0164_media_sessions.sql`
- `internal/domain/media/session.go`
- `internal/mediaapp/service.go`
- `internal/storage/sqlite/media_sessions.go`
- `internal/app/media_handlers.go`
- `internal/desktopmedia/handler_windows.go`、`handler_other.go`
- `internal/webviewhost/media_resource_windows.go`、`media_resource_other.go`
- `web/src/media/MediaCenterPage.tsx`
- `web/src/media/MusicPlayerSurface.tsx`
- `web/src/media/VideoPlayerSurface.tsx`
- `web/src/media/MediaMiniPlayer.tsx`
- `web/src/media/MediaQueueDrawer.tsx`
- `web/src/media/MediaOperationCard.tsx`
- `web/src/media/OwnedMediaPlayer.tsx`
- `web/src/activity/ActivityStatusButton.tsx`
- `web/src/activity/ActivityCenter.tsx`

所有新增公开 Bridge 方法都必须：添加 `api/bridge/v1/*.schema.json`；同步 `api/bridge/v1/envelope.schema.json` 的 method enum；同步 `web/scripts/generate-bridge.mjs` 的 hard-coded enabled-method assertion；运行 `npm --prefix web run generate:bridge`，由它生成 `internal/bridge/schema_generated.go`、`web/src/generated/bridge.ts`、`internal/contract/schema_generated_test.go`；最后运行 `npm --prefix web run verify:bridge`。不得手改三个生成产物。`internal.*` Host↔Engine 方法不进入 renderer envelope，沿用 authenticated private pipe 并单独做 allowlist/协议测试。

## 13. 测试、评测与发布门禁

### 13.1 记忆固定评测集

在不提交真实用户对话的前提下，建立合成和经授权脱敏数据：

| 文件 | 最低条数 | 覆盖 |
|---|---:|---|
| `capture_negative.jsonl` | 300 | 问候、感谢、即时天气/价格、问句、一次性命令、临时状态、引用文本、助手推测。 |
| `capture_sensitive.jsonl` | 150 | token、密码、验证码、证件、银行卡、私钥、连接串及混淆形式。 |
| `capture_positive.jsonl` | 300 | 资料、偏好、目标、约束、决定、流程、明确纠正，多句/中英混合。 |
| `temporal_conflict.jsonl` | 150 | 生效/失效、先后纠正、同权威冲突、完成目标、否定和撤回。 |
| `recall_queries.jsonl` | 300 | 单回合、跨会话、时间、更新、项目限定、拒答和无答案。 |

每条记录含稳定 `caseId`、输入 turns、主体/scope、期望 capture、禁止 capture、期望当前事实、可接受 recall IDs 和 rationale；测试不依赖自由文本完全相等。

Memory 首发 gate：

- 敏感集：正文、embedding、trace payload 落库为 0；该项必须 100%。
- 负例集：问候/感谢/即时查询/纯命令的长期记忆误存率为 0；边界型临时陈述总体误存率不高于 1%。
- 明确正例：capture recall 不低于 95%，kind + scope 同时正确不低于 90%。这只约束标注为 `explicit_stable` 的集合，不把隐式推断混入。
- 纠正集：当前版本选择准确率 100%；旧版本注入率 0。
- 召回：scope violation为0；Hit@5不低于基线，时间纠正子集达到min(100%,旧基线+10个百分点)，避免基线95%时要求不可能的105%。
- Token：每个样本的实际注入不超过配置上限；同等答复质量样本报告 p50/p95 注入 Token，首发不得高于旧实现 p95。

LongMemEval、LongMemEval-V2、LoCoMo 和 MemoryAgentBench 可作为外部补充报告，但其版本、模型、提示词、评分脚本和成本必须固定；不能拿不同模型/上下文设置的分数做产品宣传。[^10][^11][^12][^13]

### 13.2 OCR 盲测

本地固定集合至少 200 页，按设备与文档类别分层：

- 40 页简单中英截图/打印体；
- 40 页普通扫描件，包括倾斜、低对比度和噪声；
- 60 页复杂双栏、表格、标题层级、脚注和混排；
- 30 页公式/图表/印章等高难结构；
- 30 页应失败或应警告的极端输入。

样本只允许：(a) 仓库脚本生成并随 seed/生成器版本固定的合成页；(b) 明确允许再分发的公共数据子集并保存原始许可证/来源；(c) 获书面授权且完成去标识化的内部样本。`testdata/ocr-eval/manifest.json` 固定每个源文件 SHA-256、licenseRef、category、language、annotationRevision 和 split；受限制原件不入 Git，只保存不可逆 digest，并使 CI 对缺失授权数据明确 skip 而不是伪装 pass。每页至少两人独立标注，分歧仲裁后冻结 golden revision。

标注保存逐页规范化纯文本、阅读顺序 block、表格单元格网格和应有 warning。核心指标：中文/混合文本 CER、英文 WER、block detection F1、reading-order pair accuracy、table cell exact/F1、空白页误报、失败可解释率，以及冷/热启动 p50/p95、峰值 RSS、CPU 时间和输出体积。runner 必须输出 engine/pack/manifest/dataset/config digest、逐页 raw metric 和汇总置信区间；缺 corpus、缺许可、缺 engine 或缺指标均为 `INCOMPLETE`，不能通过发布 gate。

自动路由 gate 按硬件 profile 分开判断：

1. 简单页默认仍走 Windows，除非 Paddle 在该 profile 上 CER 相对改善至少 5%，且 p95 延迟不超过 Windows 的 3 倍。
2. 复杂页只有在结构 F1 提升至少 5 个百分点或 CER 相对改善至少 10%，同时任务成功率不下降，才允许自动路由 Paddle。
3. 未达到门槛不代表安装功能失败：保留“手动高级识别”，`ocr_paddle_auto_route` 保持关闭。
4. 厂商 benchmark 只在报告背景页引用，结论以 Lunitide 固定集合为准。

指标边界：Windows不提供可比较结构时不得虚构其F1=0；改用Paddle结构F1≥0.85且CER不劣于Windows作为首发目标，明确不是直接结构对比。基线CER=0不做相对除法，要求保持0并按其他适用指标判断。完整统计口径见07。

### 13.3 媒体、工具与 UI

测试矩阵必须覆盖：

- owned 本地音频、视频、生成 artifact；正常/暂停/seek/结束/stalled/error/文件移走；
- Host 私有登记不把 path 送给 Renderer；resource broker 覆盖 GET/HEAD、首/中/尾单 Range、416、multi-range 拒绝、ticket 过期/撤销、跨 scope、文件 identity 改变和 4 GiB 以上稀疏测试文件，验证不整文件载入内存；
- external 有 SMTC、无 SMTC、多个会话、元数据延迟、目标不匹配、应用退出；
- 全局键/UIA fallback 的 unverified 终态；
- command CAS、重复幂等键、参数变更复用幂等键、乱序 event、快速连点和急停；
- 页面刷新、窗口重建、Engine 重连与事件丢失；
- 恶意媒体名、歌词、封面、路径和 URL；
- 键盘、读屏、高对比度、200% 缩放、窄窗和 reduced motion。

发布 gate：false verified success 为 0；越权资产读取为 0；Renderer/日志绝对路径泄漏为 0；重复操作副作用为 0；Range 峰值内存与文件大小无正比增长；所有运行中状态可由数据库 snapshot 恢复；axe 自动检查无 serious/critical；核心播放与取消流程可全键盘完成。

### 13.4 必跑命令

开发计划中的每个任务先跑最小测试，合并前至少执行：

```powershell
go test -count=1 ./internal/domain/m8core ./internal/memoryapp ./internal/m8app ./internal/contextapp ./internal/compactionapp
go test -count=1 ./internal/ocrapp ./internal/doctext
go test -count=1 ./internal/toolruntime ./internal/winexec ./internal/mediaapp
go test -count=1 ./internal/storage/sqlite ./internal/app ./internal/contract
npm --prefix web test
npm --prefix web run typecheck
npm --prefix web run build
go test -count=1 ./...
```

`internal/mediaapp` 在该包创建前不加入 W0a 命令，创建后必须加入。仓库若已有统一 lint/typecheck 命令，开发以现有 `package.json`/CI 为准并在实施计划中写出实际命令，不臆造新脚本。

## 14. 安全、隐私、合规与可观测性

### 14.1 记忆威胁模型

- 把用户粘贴的网页、邮件、文档和工具输出视为不可信内容；其中的“请记住”“忽略规则”不具备用户授权。
- capture 只读取用户亲自输入的允许段和结构化、已验证工具回执；quoted/attachment/tool-output 段默认不可自动晋升为 `user_explicit`。
- 先做本地敏感硬过滤，再做任何远端 extraction/embedding；配置为本地模式时正文不得出设备。
- 对 memory poisoning 建立来源权威、证据数量、冲突隔离和用户纠正通道；模型不能自行解除 deny rule。
- 每次召回再执行读时授权，不因索引时曾获授权就永久可见。Agent 记忆污染与跨会话持久化攻击是明确的安全边界，不作为普通相关性问题处理。[^14]

### 14.2 OCR 与模型包供应链

- catalog 使用应用内置公钥校验；manifest 固定 packId、版本、平台、架构、所有文件 digest、下载/展开字节、runtime profile、entrypoint、license refs。
- 下载只写 staging；校验通过后原子切换 current pointer。启动时再次校验关键 entrypoint digest。
- worker 使用 Job Object 做进程树/内存/超时回收，使用固定环境变量 allowlist、固定工作目录和本地模型路径，不继承 API key、代理和用户 shell profile；首发是受信任签名 worker 的应用级 offline policy，不把它宣传为 OS 网络或文件隔离。
- 模型输出按不可信数据处理；Markdown/HTML 永不直接 `dangerouslySetInnerHTML`。
- PaddleOCR 和模型卡使用 Apache-2.0，但每个实际打包依赖仍需生成完整 SBOM/NOTICE；本 PRD 不把上游主项目许可证自动外推到所有第三方 wheel。[^15]

### 14.3 媒体与内容权利

- BaiLongma 仓库代码许可证允许借鉴其代码设计，但不授予任何歌曲、电影、封面、歌词或第三方服务内容权利；Lunitide 只处理用户有权访问的资产。[^16]
- 不在 telemetry 上传媒体内容、歌词、完整文件名或绝对路径；以类别、扩展名、时长区间和匿名错误码统计。
- 外部应用控制始终受现有电脑控制设置与急停约束；关闭电脑控制时 external session 为只读状态展示。

### 14.4 指标与日志

指标不含正文、用户查询、OCR 文本或媒体标题：

```text
memory_capture_total{decision,reason,kind,scope_kind}
memory_recall_total{route,outcome}
memory_recall_tokens{slot}
memory_conflict_total{resolution}
memory_generation_total{state}
ocr_pack_activity_total{stage,outcome,error_code}
ocr_run_total{engine_id,pipeline_kind,outcome,fallback}
ocr_run_duration_ms{engine_id,device_profile}
media_operation_total{origin,action,verification_status,verification_source,outcome}
media_verification_duration_ms{origin,action}
activity_recovery_total{kind,outcome}
```

高基数字段（factId、operationId、文件名、模型响应）只进入本地受控 debug/audit，不作为 metric label。默认日志对路径、token、证件和正文做结构化脱敏；debug 导出需用户明确操作并展示包含范围。

## 15. 性能与稳定性预算

这些是验收预算，不是对当前设备性能的宣称：

- capture不阻塞首token。队列10000时停止物化新的普通job、保留source cursor；继续claim/消费排空，不能停止消费者。删除/禁记/明确纠正独立高优先通路。
- fast path只做本地规则、授权和事务，追加终态耗时目标p95≤20ms；超过单次20ms预算转持久后台job，不等待模型。该值是验收目标，不是任意磁盘负载下硬实时保证。
- recall 在本地索引健康时 p95 不超过 150 ms；超时立即以 pinned + recent canonical 降级，不等待所有召回路由。
- Memory 后台任务单主体串行，使用 checkpoint 和批次上限；前台有活动时降低优先级并可暂停。
- OCR worker 的并发默认 1；具体内存/超时来自已签名 runtime profile 和用户资源设置，不能把官方模型大小当作实际峰值内存。
- media command 本地接受后 100 ms 内返回 accepted snapshot；verified 时限按 origin 配置，超时转 uncertain，不让 UI 无限旋转。
- 所有队列、event buffer、文本字段和 artifact 都有显式上限；超限返回稳定错误或持久化分页，不无界保存在 Go/React 内存。

长稳门禁：模拟 24 小时事件/重连/后台任务时，活动表无永久 `running`、worker 无孤儿进程、队列长度回落、SQLite foreign key/quick_check 正常；此项由自动加速时钟与故障注入完成，不要求测试真实 sleep 24 小时。

## 16. 主要风险与处置

| 风险 | 触发信号 | 处置 |
|---|---|---|
| 自动记忆误存导致信任下降 | dismiss/correct 比例升高；负例回归 | 收紧 deterministic gate；按 kind 关闭 auto-promote；保留自动候选但不注入。 |
| 两套旧平面迁移产生重复 | shadow diff 同一语义多 current facts | 使用 migration map + normalized digest + scope 去重；人工抽样后才切读。 |
| 摘要固化错误 | generation validation/correction 增加 | 不改输入；回退上一 generation；derived 内容降权。 |
| dense 模型不可用或变更 | embedding version mismatch/health fail | FTS+time 降级；按 model/version 重建索引，不混算向量。 |
| Paddle 在部分 Windows 设备不可运行 | preflight/runtime crash/RSS 超预算 | 对应 profile 不发布/不自动路由；Windows OCR 常驻兜底。 |
| 模型包供应链或体积失控 | 签名失败、catalog 字节异常、磁盘不足 | fail-closed；展示 manifest 实值；支持取消与安全清理。 |
| 外部媒体状态不可验证 | SMTC 缺失/元数据不匹配 | 明确 uncertain；不宣称播放内容；提供打开外部播放器。 |
| UI 状态源分裂 | MediaCenter/MiniPlayer/卡片/后端 revision 不一致 | 后端 snapshot 唯一真相；event 仅 invalidation/增量；冲突强制回读。 |
| 第三方内容版权风险 | 需求出现曲库、抓取、下载、DRM | 拒绝进入产品范围；只支持用户资产和合法生成 artifact。 |
| Token 降低反而损害回答 | recall hit 提升但任务质量下降 | 将 answer correctness 与 token 同时评测；保留 pinned/constraint 槽，不只优化长度。 |

## 17. Definition of Done 与交付物

每个子项目只有同时满足下列条件才可标记完成：

1. 本 PRD 对应的全部 P0/P1 requirement 在测试或验收报告中有证据链接；不能用“代码已写”代替行为证据。
2. 数据 migration 可从空库和 0159 生产形态升级；可重启、可重入、可 rollback flag。
3. Bridge schema、Go handler、TypeScript client 和 UI error mapping 同步生成并通过契约测试。
4. 权限、作用域、敏感信息、路径、HTML、模型包签名和幂等/CAS 负向测试通过。
5. 评测数据版本、运行环境、模型/引擎版本、命令、原始机器可读结果与摘要报告入库；不只提供截图。
6. 用户文档包含默认行为、如何关闭自动记忆、查看/纠正/忘记、安装/卸载 OCR 包、媒体允许范围与 uncertain 含义。
7. telemetry/log 审计确认不含正文、密钥、OCR 内容、媒体标题和绝对路径。
8. feature flag 灰度、关闭、进程崩溃、Engine 重启和数据库恢复演练完成。
9. 现有相关测试与 13.4 节全量门禁通过；任何已知失败必须有与本改造无关的基线证据，不能直接忽略。
10. 更新 release note 和第三方 NOTICE；未通过盲测的 Paddle profile 不启用自动路由。

最终交付物：

- 本 PRD；
- 三份逐任务实施计划；
- schema/migration/代码/UI/文档；
- Memory、OCR、媒体三份机器可复现评测报告；
- threat model 与 privacy review；
- migration/rollback/runbook；
- 第三方 SBOM/NOTICE；
- requirement → commit/test/report 的 traceability 表。

实施计划的唯一职责映射：

| 计划 | 负责 requirement | 不得顺带扩张 |
|---|---|---|
| 03-memory-implementation-plan.md | MEM-001～MEM-018，以及Memory通用契约/安全项 | 不安装Paddle，不实现播放器。 |
| 04-ocr-implementation-plan.md | OCR-001～OCR-012及OCR活动合同 | 不改变用户凭据和隐私选择。 |
| 05-media-ui-implementation-plan.md | TOOL-001～004、MEDIA-001～010、UI-001～004 | 不建内容服务，不降低审批。 |
| 06-integration-contracts-plan.md | C1–C6、X0–X3，补齐前三项生产接线 | 不新增外部记忆运行时或重写语音模型。 |

跨计划共享migration、Bridge、身份/授权、App音频焦点、活动聚合与全量门，由集成人维护06合同并重放schema生成，不复制同名不兼容类型。

## 18. 本文不作出的承诺

- 不宣称某一记忆框架是“世界唯一最好”；当前公开 benchmark 的任务、上下文、模型与评分不同，不能诚实地产生单一总冠军。
- 不宣称 Claude 的记忆一定比本方案先进。Claude 的官方方案展示了 workspace store、版本与 Dreams 整理能力，但其托管实现不是本仓库可直接复制的源代码。[^4][^17]
- 不宣称 PaddleOCR-VL-1.6 在 Lunitide 支持的每台 Windows 机器都比 Windows OCR 准或快；是否自动选择由 13.2 节实测决定。
- 不宣称发送媒体键就完成播放，也不宣称 Lunitide 有权提供互联网上任意歌曲、电影或歌词。
- 不把预估下载大小、内存或延迟写死为宣传数字；UI 从签名 manifest 与本机 preflight 展示实际值。

## 19. 文档交付复核与开发起点（2026-09-15）

R3 交付以本目录 README、11 总实施计划和 12 追踪矩阵为入口，未实施新增产品功能。开发按 W0a、W1～W7 及独立条件门 W0b 与 11 的依赖推进；任务编号不等于可以绕过依赖。候选迁移编号基于审计时最大 0159，只有 X0 生成 allocation artifact 后才冻结。

R2 曾在旧 HEAD `5970012d` 运行以下现有功能检查；当前 HEAD 已变化，因此状态均为 `STALE`：

| 实际命令 | 结果 | 能证明什么 |
|---|---|---|
| `go test -count=1 ./internal/memoryapp ./internal/m8app ./internal/contextapp ./internal/compactionapp ./internal/ocrapp ./internal/doctext ./internal/toolruntime ./internal/winexec ./internal/tts ./internal/voice` | 旧基线通过；STALE | 只能说明当时相关包，不证明当前或新增功能。 |
| `npm --prefix web run verify:bridge` | 旧基线通过；STALE | 只能说明当时生成产物；新增接口仍须重新生成。 |

本次仍只改设计交付物，未安装模型或变动用户数据；当前工作树其他源码修改不属于本 PRD 成果。X0 必须重新运行基线并保存 `HEAD/trackedDiffDigest/untrackedManifestRelativePath/untrackedManifestDigest/baselineManifestRelativePath/command/start/end/exitCode/outputDigest/environment` 到 `evidence/baseline/`；不再生成 `worktreeDiffDigest`。新功能测试、OCR 盲测、真实离线包、媒体实机验证、性能及 Token 收益均未完成。

特别保留的发布前验证条件：Windows CPU 完整 Paddle runtime 能否发布由真实构建/断网测试决定，不凭包名推定兼容；记忆与 OCR 的准确率提升必须由固定样本实测；用户粘贴纯文本若没有来源标注，系统无法完美识别其是否引用，必须保守进入静默审阅并通过误存集验收，不能声称绝对识别意图。

## 20. 主要来源与固定证据

外部来源均优先链接上游官方仓库、官方文档、论文或模型卡；引用只支持本文所述设计事实，不代替本仓库实测。

[^1]: Letta, “Memory Architecture”，core memory 与 archival/conversation memory 的分层设计：[github.com/letta-ai/skills](https://github.com/letta-ai/skills/blob/main/letta/letta-api-client/memory-architecture.md)。
[^2]: Hindsight 官方仓库及 retain/recall/reflect、事实/经历/观察/mental models 和多路检索说明：[github.com/vectorize-io/hindsight](https://github.com/vectorize-io/hindsight)。
[^3]: Graphiti 官方仓库，双时间、episode provenance 与 hybrid retrieval：[github.com/getzep/graphiti](https://github.com/getzep/graphiti)。
[^4]: Anthropic Claude Dreams 官方文档；research preview、读取 store/session、写入独立 output store、输入保持不变与 review/discard：[platform.claude.com/docs](https://platform.claude.com/docs/en/managed-agents/dreams)。
[^5]: Mem0 论文与公开 benchmark 仓库；本文只借鉴架构并要求自行复测：[arXiv:2504.19413](https://arxiv.org/abs/2504.19413)、[github.com/mem0ai/memory-benchmarks](https://github.com/mem0ai/memory-benchmarks)。
[^6]: PaddleOCR-VL 官方 pipeline 文档，完整 pipeline 与 VLM 模块的边界、安装和硬件支持矩阵：[PaddleOCR-VL pipeline](https://github.com/PaddlePaddle/PaddleOCR/blob/main/docs/version3.x/pipeline_usage/PaddleOCR-VL.md)。
[^7]: PaddleOCR-VL-1.6 官方算法文档和模型卡；0.9B 与 OmniDocBench 数字均按上游自报处理：[算法文档](https://github.com/PaddlePaddle/PaddleOCR/blob/main/docs/version3.x/algorithm/PaddleOCR-VL/PaddleOCR-VL-1.6.md)、[Hugging Face 模型卡](https://huggingface.co/PaddlePaddle/PaddleOCR-VL-1.6)。
[^8]: BaiLongma 固定提交 `b20f4ad5d16425be0520745d69048af11439d617` 的媒体 UI 实现：[media-modes.js](https://github.com/xiaoyuanda666-ship-it/BaiLongma/blob/b20f4ad5d16425be0520745d69048af11439d617/src/ui/brain-ui/media-modes.js)。
[^9]: BaiLongma Scene Protocol 固定提交，用于理解其 scene/widget 消息闭环，不作为 Lunitide 安全协议直接复制：[SCENE-PROTOCOL.md](https://github.com/xiaoyuanda666-ship-it/BaiLongma/blob/b20f4ad5d16425be0520745d69048af11439d617/SCENE-PROTOCOL.md)。
[^10]: LongMemEval 官方仓库：[github.com/xiaowu0162/LongMemEval](https://github.com/xiaowu0162/LongMemEval)。
[^11]: LongMemEval-V2 官方仓库：[github.com/xiaowu0162/LongMemEval-V2](https://github.com/xiaowu0162/LongMemEval-V2)。
[^12]: LoCoMo 官方仓库：[github.com/snap-research/locomo](https://github.com/snap-research/locomo)。
[^13]: MemoryAgentBench 论文：[OpenReview](https://openreview.net/pdf?id=DT7JyQC3MR)。
[^14]: NIST 关于 Agentic AI threats/mitigations 的公开材料，用于持久化污染、权限与可信边界威胁建模：[NIST CSRC](https://csrc.nist.gov/csrc/media/presentations/2026/agentic-ai-emerging-threats%2C-mitigations%2C-and-cha/1.3-agentic_ai-sotiropoulos.pdf)。
[^15]: PaddleOCR-VL-1.6 模型卡列出的 Apache-2.0 许可证；实际发行包仍需逐依赖 NOTICE：[模型卡](https://huggingface.co/PaddlePaddle/PaddleOCR-VL-1.6)。
[^16]: BaiLongma 仓库许可证和固定源码只覆盖相应代码权利，不代表第三方媒体内容授权：[LICENSE](https://github.com/xiaoyuanda666-ship-it/BaiLongma/blob/b20f4ad5d16425be0520745d69048af11439d617/LICENSE)。
[^17]: Anthropic Managed Agent Memory 官方文档，workspace 范围存储、版本和容量边界：[platform.claude.com/docs](https://platform.claude.com/docs/en/managed-agents/memory)。

## 21. R3 规范性补充与执行入口

06 的 C1–C6 是技术合同唯一来源，03–05 是子系统任务，11 是唯一总依赖顺序，12 是唯一需求状态源。新增 activity.list、media.operation.get/list、memory.item.create/history、完整 scoped OCR、source revision/抑制、音频焦点、生产 Host 身份、页渲染与 CAS 租约都属于本期。07 给出五分制与阻断门。任何尚未有 artifact 的模型兼容性、质量、性能或用户评价不得标 `VERIFIED`。
