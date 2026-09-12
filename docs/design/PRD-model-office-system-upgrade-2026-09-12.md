# Lunitide 模型原生融合与办公交付系统升级 PRD

版本：1.0 · 日期：2026-09-12 · 状态：开发规格，尚未实现本规格新增能力。

代码基线：VERSION `0.4.77`，HEAD `afbcb2c733d4`，并审查当前未提交的 Office Studio 与 Agent Hub 工作区。实施前须记录实际工作区差异摘要。本规格覆盖一个完整发布批次，分阶段集成；不要求一次合并全部代码。

配套文件：

- [逐项实施计划](E:/Trae-Work-Projects/lunitide/docs/superpowers/plans/2026-09-12-model-office-system-upgrade.md)
- [接口与状态合同](E:/Trae-Work-Projects/lunitide/docs/design/model-office-upgrade-2026-09-12/contracts.md)
- [需求追踪矩阵](E:/Trae-Work-Projects/lunitide/docs/design/model-office-upgrade-2026-09-12/traceability.json)
- [24 项固定任务与判定条件](E:/Trae-Work-Projects/lunitide/docs/design/model-office-upgrade-2026-09-12/eval-cases.json)
- [机器可校验的合同 Schema](E:/Trae-Work-Projects/lunitide/docs/design/model-office-upgrade-2026-09-12/contracts.schema.json)
- [办公产品与开源方案研究](E:/Trae-Work-Projects/lunitide/docs/design/PRD-office-quality-commercial-2026-09-11.md)
- [上一轮代码审查](E:/Trae-Work-Projects/lunitide/docs/audits/2026-09-12-deepseek-glm-codex-assessment.md)

## 1. 产品决策与交付边界

**保留现有 Go + React + SQLite 桌面架构，将模型适配、执行预算、成果验证和办公质量接成同一条可追踪的交付链。**

首发围绕 DeepSeek、GLM 和四种办公格式，所有新增功能均通过确定的代码模块、接口、持久化规则和验收任务实现。

“100% 可落地”在本规格中的定义是：全部需求具有负责模块、输入输出、错误处理、测试及实施任务；没有将核心功能留给尚不存在的模型能力、封闭产品接口或后续研究。它不等于承诺随机生成永不失败，也不等于所有 Office 软件、任意复杂原文件都能无损互转。失败、能力不足和依赖缺失均有可实现的产品结果。

本批次交付：

1. DeepSeek、GLM 按型号、端点和 profile 版本编译请求；完整原生历史、兼容恢复与可解释的能力状态。
2. 主 Chat、计划、子代理及后台辅助模型调用共用任务级准入、结算和可信成果验证。
3. 四格式办公工作流统一设计参数、质量决定与原文件证据；能够交付文件、局部修改、保留版本、导出对应 PDF。
4. 新模型从声明到探测、评测、采用、撤回的完整生命周期，以及用户可理解的状态界面。
5. 固定评测任务、回归门槛、迁移与发布步骤。

不纳入本批次：训练基础模型、自动微调、独立云调度集群、重写通用编辑器、任意 PDF 转原生 Office、任意复杂 SmartArt/动画无损导入、重建插件市场、以第三方付费产品私有接口作为关键依赖。外部 Codex 保留独立执行器身份。

## 2. 三个层面的需求交叉校验

| 层面 | 输入 | 必须变成什么 | 检查方法 |
| --- | --- | --- | --- |
| 用户需求 | 美观、大气、可编辑、商用交付、模型持续变强、少打断 | 用户场景、格式支持范围、默认策略、交付状态和质量指标 | 24 项任务、导出文件检查、盲评与用户流程验收 |
| 当前代码 | 实际调用链、类型、数据表、工具与渲染器 | 精确改造点、复用边界、迁移、接口和错误码 | 回归测试、契约生成、故障注入和数据库约束 |
| 审查报告 | 已知缺口、证据不足、与 Codex 的差距 | 每个发现对应需求、工作包和可验证结果 | `traceability.json` 检查无遗漏、无孤立任务 |

需求编号是唯一索引。正文、实施计划和测试按同一编号追踪；后续需求变更必须更新三者，不能只在聊天或代码注释里改变含义。

## 3. 本轮复查对旧报告的补充

| 发现 | 当前证据 | 本规格处理 |
| --- | --- | --- |
| 已有品牌、Spec v2、布局与自动修复 | `internal/officestudio/contract.go`、`theme.go`、`layoutplan.go`、`quality.go` | 复用和补接线，不新建另一套 Office IR 或品牌引擎。 |
| 计划成果摘要来自分页文本 | `internal/app/plan_execution.go:288`，`internal/toolruntime/workspace_read.go:17` | 增加完整原始文件快照；内容提取与文件完整性分开。 |
| 视觉 runner 只检查退出码，且只传第一页 | `internal/officestudio/visual_model.go:33` | 使用逐页摘要绑定的结构化诊断协议；退出 0 不能单独代表通过。 |
| 现有私有状态密钥由公开代次派生 | `internal/app/chat_continuation.go:333` | 使用随机数据密钥和 DPAPI 保护；旧方案仅保留迁移读取。 |
| 工具组 JSON 仍含 reasoning 明文 | `internal/storage/sqlite/protocol.go:17` | V2 完整消息加密持久化，旧记录迁移时清理该字段。 |
| 当前凭据服务在 Engine 内以 LocalClient 装配 | `cmd/engine/main.go:85` | 通过窄接口复用实际部署，不凭 Host-only 注释虚构进程架构。 |
| 预算日志可失败后仍发送，实际超额/终态结算可被拒绝 | `continuity_wire.go:493`、`sqlite/agent_runtime.go:280` | 准入事务必须成功才发送；已发生用量必须记录。 |
| 主循环强制总结可能重复加入旧 tool_calls | `chat_run_stream.go:1340` | 总结只追加指令；所有发送前执行序列校验。 |
| Office 正式交付判定分散 | `officeapp/service.go`、`domain/officestudio/officestudio.go`、前端 | 服务层统一生成 FormalDecision；前端只展示。 |

上一轮 62 分是工程成熟度判断；本规格不把完成代码数量直接换算成更高分。新的能力声明以验收结果为准。

## 4. 架构路线选择

| 方案 | 成本与收益 | 决定 |
| --- | --- | --- |
| 扩展现有模块，统一关键合同 | 复用工具、SQLite、Office 引擎和 UI；可逐步切换，风险可定位 | **采用** |
| 所有任务迁移到新统一 Agent 框架 | 长期接口可能整齐，但同时更换成熟路径、调度和状态，验证面大 | 本批次不采用 |
| 全部交给外部 Codex/Gamma 等生成 | 可快速借用外部能力，但模型、许可、成本和产品行为不可完全控制 | 仅作为可选执行器或对照，不作为核心前提 |

新增代码主要进入已有 `modelfit`、`domain/agentrun`、`agentrunapp`、`officeapp`、`officestudio` 与 `storage/sqlite`；评测调度新增轻量 `modelquality` 包。保持领域包不导入 app、数据库或桌面 UI。

```text
用户需求 + 附件 + 品牌 + 当前模型
                  │
        TaskContract / Brief / GoalRevision
                  │
       ModelBinding → RequestCompiler
                  │
       ProtocolLedger → ContextPreflight
                  │
         BudgetAdmission（提交后才发请求）
                  │
       llmadapter → ModelAttempt → 工具执行
                  │                     │
       UsageSettlement        ToolReceipt + FileSnapshot
                  └──────────┬──────────┘
                       StepVerifier
                            │
      Office Spec v2 → 生成/局部修改 → 实际文件渲染
                            │
                 FormalDecision + TaskOutcome
                            │
          展示 / 导出 / 版本恢复 / 评测证据
```

事务边界与外部副作用不能合成一个原子事务。对未知外部结果保留 `outcome_unknown`，通过回查协调；不声称外部操作严格 exactly-once。

## 5. 全量功能需求

| ID | 功能与必须行为 | 实施任务 |
| --- | --- | --- |
| FR01 | 版本化模型 profile 与显式部署绑定；未知模型不能靠名称自动认证能力 | T02 |
| FR02 | 按型号和端点编译 thinking、effort、工具和结构化输出参数，记录实际生效配置 | T03 |
| FR03 | 普通回复、工具调用、工具结果和终答完整原序持久化与重放 | T04、T05 |
| FR04 | 换模型/端点/profile 时检查兼容，必要时重建上下文并保留任务进度 | T05 |
| FR05 | 协议消息加密、密钥生命周期、旧记录迁移和备份行为明确 | T04、T19 |
| FR06 | 分片工具、断流、400、429、重试和强制总结遵守协议与副作用边界 | T03、T05、T09 |
| FR07 | 每次模型请求前检查最终输入窗口，并在完整消息组边界处理增长 | T07 |
| FR08 | 所有计费模型路径受同一任务总预算约束；并发预留、迟到结算和未知用量正确 | T06、T07、T09 |
| FR09 | 必须步骤覆盖完整，可信回执证明执行，模型正文不能自产验证证据 | T08 |
| FR10 | 成果 SHA 和大小来自完整原文件，快照稳定且受路径权限约束 | T08 |
| FR11 | 中断、重启、未知副作用与继续执行有持久状态，预算不被重置 | T09、T19 |
| FR12 | 流结束、任务结果、用户采纳相互独立；新要求不被旧终态覆盖 | T09、T10 |
| FR13 | 提示词、技能和工具目录按任务/模型编译，保持稳定版本与必要权限 | T03、T07 |
| FR14 | 声明、探测、合格、采用分开；资格绑定实际模型合同和证据 | T11 |
| FR15 | 24 项评测可执行、可复现、可导出；固定集和留出集分开 | T12 |
| FR16 | 候选采用、有效期、回归监测和撤回配置不影响在途任务 | T11、T12 |
| FR17 | Brief、事实与品牌在四格式间统一，复用三套现有品牌 | T13 |
| FR18 | 文字测量、版式适配和有界修复保持事实及锁定元素不变 | T13、T14 |
| FR19 | 首发 PPT 支持矩阵内对象原生可编辑，导出后实际渲染 | T14 |
| FR20 | Word 标题、分页、目录与表格覆盖可验证的正式报告范围 | T15 |
| FR21 | Excel 公式、数据类型、计算、图表及打印范围有独立校验 | T15 |
| FR22 | PDF 解析和页面覆盖真实完成；同源 PDF 绑定源文件版本 | T16 |
| FR23 | 唯一 FormalDecision 决定正式交付，缺检查和陈旧证据不能通过 | T13、T16 |
| FR24 | 局部修改、跨文件指标和版本恢复沿用版本 CAS 与来源绑定 | T17 |
| FR25 | 模型能力、任务进度、质检与导出状态在 UI 中准确呈现 | T10、T18 |
| FR26 | 用量、延迟、配置和证据可追踪；日志与导出不泄露推理私有数据 | T04、T06、T12、T19 |
| FR27 | 迁移、备份、旧客户端和旧会话有明确兼容行为 | T01、T19 |
| FR28 | 模型与办公效果分开比较；模板/字体/工具来源可商用追踪 | T12、T14、T20 |
| FR29 | 外部 Agent 的能力、费用和成果证据独立标识，统一交付检查 | T18 |
| FR30 | 一次发布覆盖所有功能合同，离线和真实依赖验收有完整证据 | T20 |

## 6. 用户场景与默认行为

### 6.1 一句需求交付办公文件

用户输入“用这份季度数据做 12 页经营复盘 PPT，并导出 PDF”。平台读取附件、抽取事实和单位，默认 `ops-clear`、16:9、中文，生成结构，再生成原生 PPTX。实际文件经过解析、版式与目标渲染检查，最终提供 PPTX 与绑定同版本的 PDF。普通偏好缺省不提问；关键事实冲突必须形成一个可回答的问题，并继续处理不依赖冲突的部分。

### 6.2 长任务继续与切换模型

任务进行中用户改为 GLM：当前模型调用完成或取消后切换，不在一个调用中改变目标。兼容则按完整消息账本续接；不兼容则建立新 context epoch，带入目标、已验证事实、回执、文件版本与剩余步骤。TaskID 和累计预算保持，界面提示“已保留进度并切换模型”。

### 6.3 局部修改

用户“第三页换成对比图，其他保持”。解析成目标版本、目标节点、允许的变更范围，按 CAS 创建新版本。非目标对象与锁定事实必须相同；被修改文件的旧质量证据失效，PDF 重建。支持返回上个版本；不覆盖旧文件。

### 6.4 模型升级

用户从供应商列表同步模型后，可看到“基础连接可用”和“办公任务尚未验证”。点击运行验证前显示会使用的模型、任务数与 token 上限；开始验证才产生实际调用。测试通过后用户采用该配置，在途任务仍保持原配置。

### 6.5 渲染组件缺失

能生成文件时仍展示并允许导出副本，状态为“已生成，排版尚未验证”。提供已有渲染器的检测与安装说明；不把结构预览当真实排版。无渲染器时，正式交付请求得到明确的缺项，功能不会卡在无限重试。

## 7. 模型 profile 与请求编译

profile 是随应用发布的不可变 JSON，内容摘要就是 revision。初始配置针对显式选择的 DeepSeek 与 GLM 型号和端点；`ep-...` 等部署别名由用户或管理员绑定，不按字符串包含关系自动确认家族。供应商真实模型修订号不可得时记录 `unknown`。

profile 的必要字段、合法值与 JSON 示例见合同附录。其 key 至少覆盖 family、model matcher、API dialect、规范化 endpoint path、endpoint purpose。凭据的 origin 安全指纹与能力端点指纹分开；前者不能区分同一域名下标准 API 与 Coding Plan 的语义。

编译顺序固定为：绑定目标→选任务提示词版本→选必要技能→稳定排序工具定义→应用能力参数→加入完整协议历史→估算最终输入→预算准入。请求摘要不得包含密钥；提示词、工具定义、profile 分别保存 digest。

| 产品意图 | DeepSeek 思考 profile | GLM-5.3 profile | 未识别模型 |
| --- | --- | --- | --- |
| 快速回应 | low；关闭思考仅在该 profile 明确支持且用户策略允许时 | low，保持 thinking enabled | 不发厂商专属字段 |
| 标准工作 | high | high | 使用已声明的通用参数 |
| 复杂分析/代码/规划 | max | max | 无明确 effort 支持则保持默认并记录 effective=default |

具体型号能力以随包 profile 和上线前探测为准。GLM 保留思考的标准 API 配置显式包含 `clear_thinking:false`；DeepSeek 工具模式保存所需历史 reasoning。官方来源：[DeepSeek 思考模式](https://api-docs.deepseek.com/guides/thinking_mode/)、[GLM-5.3](https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3)、[GLM 思考保留](https://docs.z.ai/guides/capabilities/thinking-mode)。

兼容模式保留现有通用文本/工具路径，但不得将其标为原生能力已验证。前端 `NO_FUNCTION_CALLING_RE` 退为旧数据提示，执行选择由 profile 与探测结果决定。Schema strict、JSON 输出只在 profile 声明且指定端点通过验证时启用；本地参数校验始终保留。不得为了消除任意 400 而悄悄删除关键 schema 约束。

LLM 只生成任务计划、内容与布局意图；不直接控制权限、预算、证据是否合格，也不能从附件或网页中的指令提升工具权限。

## 8. 原生消息账本与恢复协议

新增 `protocol_epochs_v2` 和 `protocol_messages_v2`。每个 epoch 固定 target binding、profile 和 codec；每条消息按 epoch 内单调 sequence 保存。普通用户、普通助手、工具助手、工具结果和最终助手全部记录。字段存在性必须保留：reasoning 缺失、空字符串和非空值不同，不能 TrimSpace、改顺序或拿别轮内容补齐。

完整逻辑消息 JSON 加密保存，包括 multipart、原始工具 arguments 与模型侧工具名称。重放从该账本产生模型历史，禁止再将普通正文历史追加一次。UI 正文、摘要、工具展示是投影，不是原生回放来源。

提交规则：

1. 请求发送前持久化编译后的消息引用和 call intent；失败则不发送。
2. 完整助手响应经协议校验后持久化；工具执行前必须能定位该助手调用。
3. 每个工具操作按 prepared→dispatched→succeeded/failed/unknown 留回执；一个助手组未齐时不得开始下一轮推理。
4. 终答也进入账本，然后提交正文投影和结果事件。丢失确认重试依靠同 call/sequence 的幂等摘要，不重复新增消息。
5. 已发送但未完整收到响应的消息标 incomplete，不可当完整原生状态。已执行工具不因重新生成助手消息而重复执行。

恢复算法：读取 epoch→校验 owner、目标、profile 兼容和 key→校验 sequence 无缺口→检查工具调用/结果一一对应→读取至最后完整边界→追加新用户消息。旧历史缺字段或来源不明时，创建 structured_rebuild epoch；保留任务状态但不宣称原生无损续接。

流式断开分两类：UI 断开不取消已授权后台任务；用户取消必须终止可取消工作并记录未知副作用。重新连接按持久化事件游标补发，旧 revision 事件不能覆盖新状态。

## 9. 协议数据保护与迁移

随机 32 字节数据密钥由密钥服务创建，使用现有 Windows DPAPI 保护。Engine 通过 `ProtocolKeyLeaser` 短期回调使用密钥，不将 key 放进 SQLite、Bridge 或日志；不能继续以 provider ID、凭据代次等公开字符串派生保密密钥。

消息采用 AES-256-GCM，nonce 每次独立随机；AAD 的完整字段与编码以合同第 4 节为唯一规范，包含 owner/session/epoch/sequence、目标、消息身份、complete 与 provenance。解密失败不可回退为未经认证的数据。主 API Key 轮换不应导致所有历史丢失；协议数据密钥使用独立 key ID 和生命周期。

当前生产是 Engine 内 LocalClient；直接实现本地租约即可。已有独立 broker 部署兼容路径采用专用版本化私有 opcode，只传 key 租约，不传大段 reasoning，保留期限、nonce 和进程身份校验。新功能不要求先重构 Host/Engine 进程。

旧数据迁移采用分批、幂等、断点方式：V1 私有 blob 在隔离迁移代码中读取；已知完整工具组加密归档；未知原序不伪造为 V2 完整历史。新记录验证成功后清理旧 JSON 内 reasoning 和旧可推导密钥 blob。旧备份、WAL 和磁盘历史不作物理抹除保证，备份清理遵循已有保留策略。

数据库备份和含 DPAPI 目录备份分开声明恢复能力。仅 DB 或跨 Windows 身份恢复后无 key 时，显示“任务资料可恢复，原生模型状态不可恢复”，从事实重新建立上下文。兼容回滚不能重新启用 V1 私有状态写入。

## 10. 每次调用的预算与上下文准入

统一 `ExecutionScope` 先于 pre-turn compaction 建立。TaskID 稳定，RunID 必须是实际 AgentRun 行；Chat stream 等外部 ID 经 execution_ref 映射，ScopeID 表示有持久策略的执行范围，CallID/AttemptID 表示一次逻辑调用及实际上游请求。继续和重启不重置 TaskID 预算。

调用前满足：

```text
inputUpper + outputCap + safetyMargin <= contextWindow
consumed + reserved + isolated + newReservation <= configuredLimit
```

最终输入包括系统提示词、技能、全部原生 reasoning、工具 schema、工具参数、结果和图片预算。`maxTotalTokens` 包括每个实际 attempt 的输入和输出；`maxOutputTokens` 单独约束输出，reasoning 若计入上游 output 不再另加；缓存命中是 input 的子集，不重复相加。

默认办公任务：总 token 准入额度 262144、累计输出 65536、单次输出至多 32768、最多 48 次模型 attempt、累计模型输出至多 1 MiB、模型和工具活动时长 15 分钟、单助手工具组最多 16 调用、上下文重建最多 3 次、自动版式修复最多 2 轮。收尾输出保留 1024 tokens，包含在总额度内。所有值可在任务策略里降低或扩大，并受用户总额度约束。旧 V1 限制不原地重解释；创建 V2 策略时显式填写单位与范围。

上述 token 是应用准入预算。只有精确或经过验证的 tokenizer 才能作为严格 token 上限的估计依据；供应商计费规则和缺失 usage 不能被假装精确。无法可靠估算时标 estimated 并采用保守余量，严格计费限制下应暂停。实报超预留仍必须入账、标 overrun 并禁止下一次调用，不能拒绝保存实际用量。

并发预留在一个 SQLite 事务中同时约束 task 和 child scope。同一消费仅存一条，父子用聚合视图读取，避免双计。每次 HTTP 重试/字段兼容重试都是新 attempt；发送前失败可释放，发送后未知则隔离未确认额度，迟到回执幂等结算，即使任务已取消也记账但不恢复运行。

主 Chat、council、plan planner/executor/judge、subagent、flash route、压缩和 Office 辅助模型调用必须全部经过同一准入。保留已有字节限制和截断工具防护；新账本成为预算权威，旧 generationBudget 不重复扣 token。

输入增长处理：先分页化尚未发送的大工具结果，保留完整快照引用；已经进入原生历史的消息不被任意改写。仍超窗时，在完整消息组边界使用受支持的原生压缩，或创建新 epoch，以目标、事实、回执、版本和未完成步骤重建。压缩自身也计预算，最多一次压缩后重编译，不能递归压缩。

## 11. 可信任务与成果验收

采用合同附录中的 StepSpec、StepOutcome、TaskOutcome。模型生成 proposal，宿主冻结必要步骤及验收条件。来自模型正文、网页、附件或工具输出中的 `l0` JSON 不具备可信来源。

可信 ToolReceipt 由工具宿主生成，绑定 owner、task、step、attempt、tool/schema version、参数 digest、执行状态、原始结果 snapshot 和时间。Judge 可以指出质量问题，不得把缺文件、失败命令、未知副作用提升为成功。

计划执行要传递前序步骤的已验证结果和文件引用；一个必需步骤没有结果就是未完成，不能从验证集合中省略。重试成功可更新有效结果，但保留失败历史。新增用户要求递增 goalRevision，按受影响节点使旧证据失效，不让旧终态覆盖新目标。

`SnapshotWorkspaceArtifact` 使用现有路径权限解析后读取原始文件，流式计算完整 SHA256、大小并存入不可变快照。读取前后核对文件身份/大小/修改信息；检测变化则 `ARTIFACT_CHANGED` 并重试一次。最终交付验证的是快照摘要；原路径后续变化显示为新版本，不能沿用旧证明。

对代码任务，成功证据包括要求的测试命令、工作目录、目标代码版本和结构化测试结果；`echo ok`、模型说“测试通过”或任意 exit 0 不能替代指定检查。

## 12. 生命周期、恢复与事件

| 概念 | 值 | 用户含义 |
| --- | --- | --- |
| 流状态 | completed / failed / cancelled | 本轮传输结束或中断 |
| TaskOutcome.state | succeeded / incomplete / paused / failed / cancelled / outcome_unknown | 任务完成度与能否继续 |
| completion | verified / not_required / unverified | 是否需要且已完成独立验证 |
| Office 接受 | acceptedVersionID | 用户选定版本，与质量独立 |

普通问候或无需文件的回答可 succeeded + not_required，显示“已回复”。办公合同须 verified 才显示“已验证完成”；只有文件可下载但检查未齐显示“部分完成”。预算不足、等待用户信息等使用 paused 与具体 reasonCode。

保留现有流事件语义，新增 `task_outcome` 事件及 CompletedEvent 可选 outcome 摘要。重连通过当前任务结果查询和游标恢复；数据库提交后再发送业务成功，UI 不从 assistantText 非空推断任务完成。

重启恢复：未发送模型调用释放预留；已发送模型调用等待结算或隔离；prepared 未派发工具可安全取消；dispatched 且未知的变更工具先回查，不能直接重放。对不支持幂等或回查的外部服务保留 outcome_unknown，并显示已知证据。

## 13. 模型持续成长与资格管理

profile 的不可变修订、资格记录和当前采用指针分离。资格记录绑定应用版本、端点合同、profile、prompt/tool revisions、任务集摘要和观测时间。旧资格表保留为 legacy fixture 证据，不能自动晋升为 live qualified。

生命周期：declared→observed→qualified；active 是另外一个指向 qualified revision 的部署选择。blocked/expired 是证据状态，不覆盖历史记录。一次连通性 ping 只产生 connection observation，不产生工具、推理或办公资格。

标准探测包含：非流式回复、流式完成、工具参数、两轮工具、普通回复后工具、重启回放、请求参数编译。实际协议错误将候选标 blocked；任务质量未达门槛时保留 observed，不能“因调用成功”自动通过。

默认资格有效期 7 天；profile、端点 path、工具 schema 或评测规则改变立即失效。过期对已固定配置的在途任务不打断；新任务提示使用当前已验证或兼容模式，并可启动重新验证。没有取得用户对验证批次的授权时，不后台运行付费评测。

采用使用 expectedRevision CAS，同时保存 previousBindingID；候选先作用于新任务，可配置 10% 的已授权测试流量，达到样本门槛再扩大。相同用例在两次连续窗口出现新的协议失败即停用候选新任务；不根据一次视觉评分波动直接自动换用户模型。

回滚恢复本地 profile/绑定，不能恢复已被供应商下线的权重。无法恢复旧模型时选已配置且合格的备用目标；没有备用则保持暂停，不能静默上传到新供应商。

## 14. 办公内容与设计系统升级

直接复用 `Brief`、`Fact`、`BrandProfile`、`Spec v2`、NarrativePlan、LayoutPlan 和现有三套 StarterBrand：`ops-clear`、`brand-pitch`、`editorial-report`。补充 schema 标记、品牌 revision 和设计令牌在各格式中的落地一致性。

首发受控文档族：经营复盘/产品方案/客户提案 PPT；研究报告/项目报告/客户方案 Word；经营台账/项目跟踪/销售管道 Excel；前三者同源 PDF 与现有独立报告 PDF。现有模板缺陷按覆盖清单修复，不复制成另一套模板目录。

默认视觉规则：16:9 PPT，安全边距 0.4 英寸，正文首选 18—24 pt、标题 28—36 pt、注释不得小于 11 pt；A4 Word 正文 11 pt、左右边距 20—25 mm；数字列对齐并标明单位。对现有导入文件不强制套用首发生成版式。

文字超量处理顺序为：合理换行→选择已支持布局→拆页/拆段→保留原始内容的用户可见附录。不得靠缩到不可读字号、截断数据或整页图片化来通过。长中文、英文长词、混排、百分号、负数和缺字必须进入模板用例。

已有 BoundedRepair 保留最多两轮；每轮生成新候选，比较事实锁、目标节点及非目标对象。失败候选不替换 accepted 版本，不无限调用模型“再美化”。

## 15. 四格式质量矩阵

| 格式 | 必须检查 | 默认支持与明确边界 |
| --- | --- | --- |
| PPTX | OOXML 可解析、文字/表格/柱线饼图原生元素、对象边界、重叠白名单、缺字、实际渲染、页数与内容覆盖 | 文本、常规图表、表格和形状；照片允许位图，整页位图不计原生编辑通过。高级动画和任意 SmartArt 不属于首发原生编辑承诺。 |
| DOCX | 样式层级、标题映射、表格列宽、重复表头、分页、目录字段刷新、实际渲染、锁定事实 | 正式报告结构；字段能否刷新以实际目标软件验证为准，未刷新不能显示通过。 |
| XLSX | 表格范围、数据类型、公式引用、完整重算、错误值、图表范围、打印区域、冻结行列、单位与输入区域 | 首发公式 SUM/AVERAGE/IF/SUMIF/COUNTIF 和基本引用；其他公式允许保留，未验证函数不计计算通过。宏和外链默认不执行。 |
| PDF | 真实解析、页数、逐页覆盖、文字层/图片页区分、字体情况、链接、同源 SHA | 不以 `%PDF`/EOF 标记替代解析；扫描 PDF 需要 OCR 才能通过文本完整性，OCR 未配置则明确缺项。PDF/A 仅在专门验证器通过时声明。 |

实际排版沿用当前 LibreOffice worker；已有原生目标软件校验按检测到的版本附加。默认兼容声明只覆盖本次实际验证的 renderer 与 target，不能把 LibreOffice 渲染成功扩展为 PowerPoint/WPS 已验证。

渲染 worker 用已有隔离临时目录、超时、进程树清理和精简环境，不附着或终止用户正在使用的 Office 进程。每次结果记录 renderer/version、字体清单摘要、规则 revision、源文件 digest 和输出 PDF digest。缓存 key 包含这些字段，文件或依赖变化立即失效。

## 16. 唯一正式交付决定与视觉诊断

新增 `officeapp.FormalDecision` 服务，所有 Chat、accept 展示、export、bundle 和任务完成都调用它。决策输入是文件版本、DeliveryPolicy 和质量证据，输出为 allowed、blockingCodes、missingChecks、coverage 和 evidenceRefs。

通过条件：文件摘要一致、策略规定检查全部存在且 passed、证据仍有效、锁定事实与来源一致、目标格式满足可编辑范围。可选项目未配置可以展示 coverage gap；被用户指定为 required 的项目不得因旧 `honestCoverageGap` 特判而放行。规则分只用于诊断，不能通过累加通过项制造“审美 100 分”。

视觉 runner V2 使用 JSON manifest 输入全部页图和各自 SHA，输出 schemaVersion、reviewer/version、输入 digest、已评页索引和 issue 数组。逐页完整覆盖才可记全量；空输出、无效 JSON、摘要不符、只评第一页、超时均为未完成。视觉模型只能给诊断和质量建议，不能覆盖硬性文件/事实失败。

图片必须来自真实文件渲染。已有进程 runner 迁入 `commandworker` 的有界执行，不用裸 `exec.Command` 无期限运行。默认没有视觉模型时依靠确定性检查与受控模板交付，显示“视觉模型未评审”；用户要求视觉审稿时该项成为 required。人工盲评用于品牌/模板发布资格，不假装是每份文件自动获得的人审。

用户采纳与交付模式：`accept` 表示选定文件；`export` 新增 `deliveryMode=copy|formal`。旧 draft=true 映射 copy，draft=false 映射 formal，两字段均省略才默认 copy；冲突字段拒绝，详见合同兼容表。copy 导出当前副本并如实附带检查状态，formal 不满足策略时返回 `OFFICE_FORMAL_REQUIREMENTS_MISSING`。UI 对未验证文件仍提供可用下载操作。

## 17. 局部修改与跨文件一致性

沿用 Office immutable Version、Head revision、PatchRequest、Metric/EvidenceEdge。补齐所有修改入口对 FormalDecision 的失效通知。每次补丁显式列出允许修改的节点/范围、基线 SHA、锁定事实、品牌 revision，CAS 冲突返回最新版本而不覆盖。

跨文件更新采用两阶段：先为所有目标生成候选和校验结果，再原子更新 bundle 的版本引用。任何目标失败时不把一半新、一半旧的组合标为一致交付；已经生成的候选保留供查看。指标携带 value、unit、period、currency、sourceVersion、node digest 和 rounding，数值相同但口径不同仍是冲突。

PDF 永远引用生成它的源版本，不覆盖旧绑定。用户恢复旧 Office 版本时可复用仍有效的同源 PDF 证据；renderer 或规则要求改变则重检。

## 18. UI 与 Bridge 合同

模型详情增加“能力与验证”区域，显示基础连接、原生工具/推理状态、办公验证、最近验证时间、当前采用配置。高级信息折叠展示端点/profile/证据摘要，普通流程不暴露 codec 等实现术语。

任务区分别显示：执行阶段、已产出的文件、检查结果、剩余工作。流结束停止 spinner，但以 TaskOutcome 显示“已回复/已验证完成/部分完成/等待补充/额度不足/待核实”。新 goalRevision 的进展不可被旧消息覆盖。

Office 文件区保持预览、版本、质量、导出四项；真实渲染与结构预览有明确标签。模型切换不强制创建新的用户会话；底层 epoch 更新透明，只有影响用户决策时提示。

新增 Bridge 方法、payload、返回、错误码和 deadline 见合同附录。遵循 `api/bridge/v1/*.schema.json`→`web/scripts/generate-bridge.mjs`→generated TS/Go 的现有流程；更新 runtime registry 和 client guard，不手改生成产物。

## 19. 外部 Agent 集成边界

保留当前 Agent Hub 的 Codex/Cursor/Kimi 适配，不把它们的能力记作自有 DeepSeek/GLM 运行时能力。统一显示 executor、model（已知才填）、外部 session ID、结果状态和费用完整性；缺 usage 为 unknown。

外部文件进入平台交付时依次经过路径校验、原文件快照、格式检查与 FormalDecision。CLI 退出 0 仅表示执行器退出成功。当前重启不能续接的外部任务明确 interrupted；不宣传已实现原生恢复。

Codex App Server 可作为下一版本适配接口，首发不依赖其实验 API。对比参考是 thread/turn/item 与恢复事件协议，不是照搬其模型专属字段。[官方协议](https://learn.chatgpt.com/docs/app-server)

### 19.1 Skills、MCP 与插件的落地位置

此前对 Gamma、Plus AI、Beautiful.ai 和开源组件的研究继续作为体验参照；本批次分别落实为“Brief 到受控初稿”“原生可编辑对象与目标软件验证”“品牌参数与模板约束”。不依赖其未公开内部实现，也不把用户规模或宣传语作为本产品验收依据。

| 能力 | 本批次具体实现 | 接入与负责任务 |
| --- | --- | --- |
| Skills | 编写第一方 office-brief、office-narrative、office-revision、office-delivery 四个流程资源；输出已有 Brief/Spec/Patch/检查请求 | 复用 skillapp 的目录、版本与加载机制；T07 编译和预算，T13/T17 落地生成与修改 |
| MCP | 通过现有 MCP runtime 接入用户已配置的检索、图库或数据工具；输出降为不可信资料或受控素材引用 | T07 统一 tool catalog、权限和用量；T08 验证来源；T20 核对分发清单 |
| 免费/开源组件 | 优先继续用已在代码中的 OOXML、Excelize、PDF、LibreOffice/Typst 路径；按缺口扩展，不并列引入数套生成器 | T14—T16，固定版本、输入输出和可用性检测 |
| 可选第三方生成 | 若以后接 Gamma 等合法 API，作为独立 executor，经同一成果快照与正式决定入库 | 首发无关键依赖；沿用 T18 的边界与证据合同 |

四个第一方 skill 的固定输入/输出：brief 接用户目标与来源引用，产出 Brief；narrative 接 Brief/事实/品牌，产出 Spec v2 的章节/页面意图；revision 接当前版本与允许范围，产出 PatchRequest；delivery 接文件引用与用户交付要求，提出检查和导出请求。最终 permissions、预算、JSON 校验和 FormalDecision 始终由宿主代码执行。skill 不得以文本命令直接标记 verified，也不自动安装工具或取得新权限。

每次调用记录 skill revision、content digest、compiled tool digest；明确缺失技能时使用相同结构合同的基础提示词，不能悄悄换工具或降低 required 检查。免费流程文本可减少内容编排成本，美观仍由模板、字体度量、实际渲染和修复实现。本机可用的 Codex skills 或专有 artifact runtime 不默认拥有随产品再分发的授权。

## 20. 评测与发布指标

固定集 24 项：四格式各 3 项、协议 4 项、长任务 4 项、代码/数据 4 项。同一 case 的正例与故障子场景分别记录，不能混合统计。任务输入和预期结果写在 `eval-cases.json`，全部使用合成资料；首次上线另准备同结构留出集，不参与参数调优。

离线协议回归每次 CI 运行；真实模型任务由有预算的显式批次运行。每个候选至少 3 次重复，保留每次结果而非只挑最好一次。每个样本绑定应用 commit、task/case digest、profile、prompt/tool versions、模型标识、renderer、文件 SHA、用量与耗时。

两个对照分别报告：同模型旧/新运行时用于适配增益；Lunitide/Codex 同目标交付用于产品比较。工具、模型、费用或时间预算不同必须披露。美观由盲评评分，文件可用性由确定性检查；模型自评分不作为独立通过证据。

本批次发布门槛：

- FR01—FR30 全部映射到实现、测试和证据；关键合同与故障回归全部通过。
- 两个首发模型绑定均通过协议探测；若某个目标失败，不能宣称该目标已深度适配。
- 12 个办公正例场景各 3 次（包括 F03 supported-font；missing-glyph 另计故障合同结果）：阻断性文件错误为 0；正式导出策略无绕过；任务未通过时正确交付部分结果并标记。
- 完整任务成功率按实际样本报告，不能用“正确显示失败”充当成功。达到至少 32/36 的办公完整交付，才允许向外宣称该受控任务集已进入商用试运行；同时展示样本规模，不能外推为所有办公任务 89% 成功。
- 三套品牌每套固定 12 页测试，覆盖合同第 12 节的 12 个精确 variant × 3 类内容样本，仅认证 standard 密度：3 名评审在层级、排版、留白、品牌一致和信息表达五项按 1—5 分评分，均值至少 4.0，任何维度均值不低于 3.5；此门槛是模板发布资格，不能替代文件硬检查。
- Windows 指定测试机上，同一离线输入的额外准入/记账 p95 不高于 100 ms；1000 条协议消息恢复 p95 不高于 2 s。记录硬件、DB 和样本，不把网络模型耗时混入。未达标先定位热路径，不删验证。

## 21. 数据迁移、兼容与开关

实施基线当前最高迁移包括未提交 `0153_agent_hub.sql`。本批次预留：0154 模型 profile 与协议账本、0155 执行任务/预算/结果、0156 Office 交付策略与证据索引。T01 冻结时若有占号，只整体顺延并更新映射，不修改已经发布的旧迁移。

旧协议表作为迁移源，不再写 reasoning。旧资格不自动采用；旧 Task 无结果记录显示 unverified。新版字段全部以可选字段加入旧 Bridge 方法，新增方法只由新版 UI 调用；未知版本拒绝读写，不吞字段后假装成功。

开关按依赖启用：`model_profiles_v2`→`protocol_ledger_v2`→`execution_contract_v2`→`office_delivery_v2`。这些是发布配置，不新增多个普通用户开关。开关关闭也不得启用不安全旧私有数据写入或绕过正式交付策略。

数据库升级采用启动互斥、备份、事务迁移、快速一致性检查和版本记录；失败不进入正常 UI。可回滚的兼容版本必须提前发布并能识别新 schema、拒写不支持的状态。若只能恢复备份，应明确恢复点之后的数据不在旧库中；禁止运行旧二进制继续写升级后的未知 schema。

## 22. 实施里程碑与团队计划

建议团队：2 名 Go 工程师、1 名前端工程师、1 名测试/自动化工程师、0.5 名文档设计师；技术负责人由一名 Go 工程师兼任。以下为范围估算，T01 完成基线和依赖核对后更新，不作为已承诺工期。

| 里程碑 | 任务 | 建议时间窗 | 可评审交付 |
| --- | --- | --- | --- |
| M0 冻结与合同 | T01—T02 | 第 1 周 | 基线、接口、profile、迁移编号与用例目录 |
| M1 原生与账本 | T03—T06 | 第 2—4 周 | 请求编译、加密完整历史、兼容恢复和事务预算 |
| M2 执行闭环 | T07—T10 | 第 4—6 周 | 所有调用准入、可信结果、重启恢复和 UI 状态 |
| M3 办公交付 | T13—T18，与 M2 部分并行 | 第 3—8 周 | 统一质量决定、四格式受控交付与局部更新 |
| M4 演进与发布 | T11—T12、T19—T20 | 第 7—10 周 | 资格采用、固定集评测、迁移恢复和候选包 |

总估算 10—12 周，包含集成与兼容缓冲。单人按约 140—180 有效人日规划，不把多模块任务简单承诺为几天完成。尚未配置的目标模型或渲染器由环境准备任务显式列出，不隐藏为最后一刻的阻塞。

代码分支按独立可测试工作包提交，每包可以单独审查；发布仍以本规格全部门槛完成为准。模型与 Office 工作可并行，但不得在共享 DTO、迁移和 Engine wiring 上无协调地同时修改。

## 23. 开发完成定义

每个实施任务必须交付：需求实现、回归用例、失败路径、迁移兼容、生成契约、运行证据。至少一条测试应覆盖原缺陷或真实用户目标，不能只断言新类型字段存在。

发布包包含：应用版本与 commit、全部开关有效值、模型绑定与资格摘要、模板/字体/依赖版本和许可清单、数据库迁移结果、自动化和实测证据、已知支持边界。用户提供模型 key 与执行环境后能够按操作步骤完成任务；环境不可用时有明确降级结果。

本 PRD 的完成不代表这些功能已经实现或验收。开发团队执行配套 T01—T20 后，才能按实际证据更新产品能力与评分。
