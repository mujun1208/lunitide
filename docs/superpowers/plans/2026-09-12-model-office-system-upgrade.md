# Lunitide Model and Office System Upgrade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有桌面产品内完成模型原生融合、任务预算与可信交付、四格式办公质量和模型升级验证的完整闭环。

**Architecture:** 保留 Go Engine、React Renderer、SQLite 和现有 Office Studio；以版本化 profile、加密协议账本、任务合同、调用准入、不可变成果证据连接已有执行路径。不迁移到第二套 Agent 框架。

**Tech Stack:** Go 1.26、SQLite、React 19、TypeScript、Vitest、当前 OOXML/Office 渲染工具链、Windows DPAPI。包版本以 T01 锁定的 go.sum/package-lock 为准。

**Spec:** [主 PRD](E:/Trae-Work-Projects/lunitide/docs/design/PRD-model-office-system-upgrade-2026-09-12.md)；[规范性合同](E:/Trae-Work-Projects/lunitide/docs/design/model-office-upgrade-2026-09-12/contracts.md)；[机器合同与评测目录](E:/Trae-Work-Projects/lunitide/docs/design/model-office-upgrade-2026-09-12/contracts.schema.json)。

本文件为待执行的开发计划，复选框不代表代码已经实现。下文“新建”路径是计划路径，不是当前仓库已有功能。工作区现有修改必须保留；禁止将它们误归为本计划完成项。所有命令从仓库根目录执行，指定 web 工作目录的命令除外。

## 执行约束与依赖图

1. T01 冻结 baseline、迁移编号、fixture 和环境；T02 冻结字段。后续接口更改必须同步主 PRD、合同、Schema 和追踪表。
2. 每个任务先写能暴露原问题的测试，确认失败原因，再实现最小闭环，最后跑任务列出的包级检查。纯样式参数调整只做有意义的渲染对照，不写镜像实现的测试。
3. 不用生产 key、私人会话或生产库做离线测试。需要真实 API 的资格批次单独建立预算，按产品操作启动；离线结果不能伪装 live qualified。
4. 修改 Bridge 源 schema 后运行现有生成器，禁止直接修改 generated 文件。渲染器能力缺失是可验证的产品状态。
5. 每包提交包含代码、契约、测试结果和兼容说明；不要求一次 commit。达到 T20 才是同一发布批次的整体完成。
6. 尽可能复用现有 Store/UoW；本文接口补在现有模块，不用同名新服务替代原实现。

依赖顺序：

```text
T01 → T02 → T03 → T04 → T05
           └────── T06 → T07 → T08 → T09 → T10
T02 + T05 + T06 → T11 → T12
T01 → T13 → T14 / T15 → T16 → T17 → T18
T05 + T09 + T11 + T16 + T17 → T19
T10 + T12 + T18 + T19 → T20
```

T03 的 attempt hook 可以先用测试实现，生产接线必须等 T06；T04 的 key 接口与协议存储可同包实现。T13 的 Office 服务工作可与模型工作并行。SQLite 迁移、Engine 装配、Bridge 主 registry 分别指定一名合并负责人。

| 工作包 | 负责人角色 | 主要交付 | 预计有效人日 |
| --- | --- | --- | ---: |
| T01—T02 | 技术负责人、测试 | 基线与版本化合同 | 8 |
| T03—T05 | Go A | 请求编译、原生存储、连续执行 | 24 |
| T06—T09 | Go B | 准入、可信验收、恢复 | 29 |
| T10—T12 | 前端、Go A、测试 | 任务状态、资格与评测 | 20 |
| T13—T17 | Go B、设计师、测试 | 设计体系与四格式交付 | 37 |
| T18—T19 | 前端、技术负责人、测试 | 产品整合、迁移兼容 | 17 |
| T20 | 全组 | 留出评测、发布与恢复演练 | 10 |
| 合计 | 可并行执行 | 不含环境采购、等待用户反馈 | 145 |

估算是拆解后的工作量，不是完成证明。团队日历安排见主 PRD；具体环境差异在 T01 记录后调整。

## T01 — 冻结代码基线与可复现验收环境

**对应：** FR27。**依赖：** 无。**产出：** 唯一基线清单、合成 fixture 目录、迁移登记。

**文件：**
- 新建：`docs/audits/model-office-upgrade/baseline.json`、`scripts/verify-model-office-upgrade.mjs`。
- 新建：`internal/modelquality/testdata/upgrade-v1/`；从配套 eval-cases.json 导入原始材料与期望值。
- 检查已有：`migrations/`、`go.mod`、`web/package.json`、`api/bridge/v1/`。
- 测试新建：`internal/storage/sqlite/upgrade_compatibility_test.go`。

- [ ] 记录 HEAD、VERSION、git diff 的 SHA、未跟踪文件列表摘要、Go/Node/字体/LibreOffice 版本；只记录路径与摘要，不复制秘密配置。记录每个已修改文件的所有者，避免覆盖已有 Office/Agent Hub 改动。
- [ ] 列出实际最高迁移。默认保留 0154/0155/0156；已占号则整体顺延，在 baseline 中记录逻辑任务到真实文件名的映射。
- [ ] 将 24 项用例写入版本化 testdata；对 generator 输入固定种子、输出字节编码和换行。冻结 case digest，expected 不由被测模型或生成器返回。
- [ ] 建立一个隔离临时目录：独立数据库、workspace、mock HTTP endpoint、fixture keys；同一任务重复运行不会访问用户数据。已有 app/sqlite 测试工厂优先复用，各包不共享全局单例。
- [ ] 新增 `TestUpgradeMigrationBackupCompatibility` 的初始失败用例：旧版 fixture 数据库升级后保留会话和 Office 版本；未知新 schema 的旧写入器拒写。完整恢复断言由 T19 补齐。
- [ ] 开发验证脚本至少检查：Schema、FR/T/test 覆盖、24 case ID 唯一、输入摘要、所有必需证据路径存在；缺证据返回非零，不能仅打印警告。
- [ ] 跑现有 llmadapter/modelfit/contextapp/agentrunapp/officeapp/officestudio 相关回归，保存命令、退出码和版本。已有失败单列，不算本改造通过。

**验收：** 任意开发机能从固定输入重新生成同 digest fixture；baseline 不含 key；没有将未知依赖隐入后续任务。

**恢复：** 本包只添加测试与清单；不运行真实库迁移。

## T02 — Profile、目标身份与用户意图

**对应：** FR01。**依赖：** T01。**输入/输出：** 合同第 2 节 `ModelProfile/ModelIntent/TargetIdentity/TargetDigest`。

**文件：**
- 新建：`internal/modelfit/profile.go`；修改：`internal/modelfit/qualify.go`、`internal/llmadapter/factory.go`、`internal/app/provider_diagnostics.go`、`internal/secret/service_windows.go` 及凭据保存入口。
- 新建：`internal/modelfit/target.go`、`internal/modelfit/profile_v2_test.go`、`internal/modelfit/testdata/profiles-v2.json`。
- 新建：`migrations/0154_model_native_v2.sql` 的模型与协议表部分，后续 T04 补完整后统一合并。
- 新建：`internal/storage/sqlite/model_fit.go` 与测试。

- [ ] 导入 profile-examples 中 DeepSeek 标准端点、GLM 标准端点、GLM Coding 端点声明；初始只为 declared，禁止导入即 qualified。
- [ ] 使用显式 profile 绑定；“名称像 deepseek”只生成建议。ModelContract 对应供应商合同版本，CodecVersion 对应应用序列化版本，二者不能共用一个字符串。
- [ ] 实现 canonical JSON 与 TargetDigest；去 fragment、规范 scheme/host/默认端口，保留有效 path，禁止含 key 的 query/userinfo。
- [ ] `CredentialBindingID` 从实际取得的凭据租约传入。没有该字段时不能声称跨 key 原生兼容。StorageKeyID 单独管理，不混入模型凭据。
- [ ] 添加 `TestModelProfileBindingAlias`：自定义显示名绑定 GLM 能正确识别；同 host 的 Coding 与标准端点不同；别名名称相同但 path/profile 改变 digest 不同；返回模型未知保持 null。
- [ ] 增加 JSON 金样例：对象字段顺序变化不改摘要，数组次序变化改变摘要，组合字符不被归一化，integer 输出稳定。

```powershell
go test ./internal/modelfit ./internal/storage/sqlite -run 'Test(ModelProfileBindingAlias|ProfileCanonical|TargetDigest)' -count=1
```

**验收：** capability 声明、观测、资格是不同字段；未知模型的通用模式可用，但不会获得原生恢复标记。

## T03 — 最终请求编译与实际 attempt 生命周期

**对应：** FR02、FR06、FR13。**依赖：** T02；生产预算实现依赖 T06。

**文件：**
- 新建：`internal/modelfit/parameters.go`、`parameters_test.go`。
- 新建：`internal/llmadapter/prepared.go`、`attempt_test.go`。
- 修改：`internal/llmadapter/openai.go`、`common.go`、`types.go`、`request_efficiency.go`、`internal/app/continuity_wire.go`。
- 测试：`internal/llmadapter/profile_prepared_test.go`。

**合同：** `CompileParameters` 生成 EffectiveParameters；adapter 完成历史规范化、工具名映射和压缩后生成 PreparedRequest。它是准入、审计和真实 HTTP Body 的唯一来源。

- [ ] 表驱动覆盖 quick/standard/deep × 三个显式 profile × strict 开关；不支持的组合必须报错或按 profile 声明有记录地降级，不静默忽略。
- [ ] GLM clear_thinking 的显式 false 必须出现在 JSON，不能因 omitempty 丢失；强制思考 profile 的 quick 使用 low effort，不强行 thinking disabled。
- [ ] 思考与输出参数按端点 profile 白名单编码；DeepSeek strict endpoint 只在明确绑定支持时启用，不将所有网关 URL 自动换成 beta。
- [ ] 增加 PreparedRequest 的冻结点；以最终 body 计算 request digest。所有重试都生成新 attempt，并再次进入 BeforeSend。
- [ ] 将网络发送组织成“准备→准入→标记已派发→发送→完整解析→结算”。BeforeSend/MarkDispatched 失败都不得发送；结算失败写待恢复状态并使任务待核实。
- [ ] 写 `TestCompileParametersMatrix` 与 `TestAttemptErrorMatrix`：覆盖 400 参数错误、429/503、认证错误、发送前取消、半截 SSE、完整文本但 usage 缺失、tool arguments 分片中断。
- [ ] 模型已经流出文本后不自动重开一个不可见的全新响应；半截 arguments 不能进工具执行器。响应 metadata 不进入用户文本或裸日志。

最小运行逻辑（`hooks`、`prepared`、`transport` 均由上述 adapter 实例提供）：

```text
attemptID = hooks.BeforeSend(prepared)
if error: return without HTTP
hooks.MarkDispatched(attemptID)
if error: keep reservation isolated; return without HTTP
result = transport.SendAndParse(prepared.Body)
hooks.AfterAttempt(new bounded settlement context, attemptID, result)
return parsed result only with accurate completion state
```

```powershell
go test ./internal/modelfit ./internal/llmadapter -run 'Test(CompileParametersMatrix|AttemptErrorMatrix|PreparedRequest)' -count=1
```

**验收：** mock server 收到的 body SHA 等于记账摘要；供应商扩展字段不泄入通用 UI；每次实发都有唯一 attempt。

## T04 — 加密完整协议账本与密钥装配

**对应：** FR03、FR05、FR26。**依赖：** T02—T03。

**文件：**
- 新建：`internal/modelfit/transcript.go`、`transcript_test.go`。
- 修改：`internal/modelfit/private.go`、`internal/storage/sqlite/protocol.go`、`internal/app/continuity_wire.go` 中的 MessageGroupStore 端口。
- 新建：`internal/storage/sqlite/protocol_v2.go`、`protocol_v2_test.go`、`internal/secret/protocol_keys.go`、`internal/secretlease/protocol_keys.go`。
- 修改：`cmd/engine/main.go`、现有 secretlease LocalClient/Client/server handler 与协议 schema；完成 0154。
- 新建：`internal/app/protocol_migration.go`、`protocol_migration_test.go`。

- [ ] 为 NativeMessage 保存完整逻辑 message 原值，区分 missing/null/empty；普通助手与工具助手都入账。移除“先 TrimSpace/截断再当完整协议”的路径。
- [ ] 用随机 256-bit 数据密钥，DPAPI 包裹后先耐久写入；engine 通过窄租约使用。测试只注入临时 key。
- [ ] AES-GCM AAD 严格使用合同第4节的完整字段与编码，包括complete/turnID/callID/provenance。复制密文到另一行、会话、目标或修改完整性标志必须解密失败。
- [ ] 实现 NativeTranscriptStore；head、message append、checkpoint、commit idempotency 在一个 UoW 内完成。不得在内部开第二个数据库事务导致部分提交。
- [ ] 同 CommitID 同 digest 重试返回首次结果；不同 digest 冲突。不要只存最后 CommitID 后无法识别更早的重试；使用合同定义的 commit receipt。
- [ ] 运行时迁移按每批 100 条：读旧行→解密旧私有块/提取原 reasoning→V2 加密→事务 CAS 写新行并清理旧字段→提交 cursor。旧记录缺失历史或已被截断时标 legacy/degraded，禁止补造完整消息。
- [ ] 处理超限、key 缺失、跨账户 DPAPI、磁盘满、迁移中断和删除。停用新能力时也不能回退写入明文 reasoning。
- [ ] `TestNativeHistoryExactRoundTrip` 使用 R01 的空格、CRLF、Unicode、空/null/缺字段及 >300 KB 推理；逐字段与精确 string/arguments 对比，不用 TrimSpace 比较。
- [ ] `TestProtocolCipherAADAndLegacyMigration` 覆盖 AAD 换行、密钥错误、批次中断重入、旧字段清理及无法恢复的 legacy 状态。
- [ ] `TestRuntimeEvidenceRedaction` 在 API DTO、结构化日志、备份清单中搜索合成 secret sentinel 与 reasoning sentinel，必须无泄露；加密备份内容可含 ciphertext。

```powershell
go test ./internal/modelfit ./internal/secret ./internal/secretlease ./internal/storage/sqlite ./internal/app -run 'Test(NativeHistoryExactRoundTrip|ProtocolCipherAADAndLegacyMigration|RuntimeEvidenceRedaction|NativeCommit)' -count=1
```

**验收：** 旧会话仍可读业务内容；只有证据完整且目标相容的 V2 epoch 可 native replay。数据库保存 ciphertext，日志和公共 DTO 不暴露私有状态。

## T05 — 跨轮、切模与重试的原生历史校验

**对应：** FR03、FR04、FR06。**依赖：** T03—T04。

**文件：**
- 修改：`internal/app/chat_continuation.go`、`chat_run_stream.go`、`internal/modelfit/messagegroup.go`、`envelope.go`、`internal/llmadapter/openai.go`。
- 新建：`internal/app/native_replay_test.go`、`internal/modelfit/sequence.go`、`sequence_test.go`。

- [ ] 所有后续请求调用 CheckReplay；比较 scope、实际目标、profile digest、codec 与序列，不依据显示名推测兼容。
- [ ] 序列校验维护 pending tool_call_id 集合：助手生成 tool_calls 加入集合，工具结果只能消费一个已存在 ID；集合非空时不允许另发普通 user/assistant 回合；并行工具结果可以按真实完成次序返回。
- [ ] switch model/endpoint/credential/profile、旧历史不完整或 key 不可用时创建新 epoch；将已核验事实、用户目标、工具成果引用重建为结构化上下文。root TaskID 与预算不重置。
- [ ] 修复强制总结分支：不重复追加上一个带 tool_calls 的助手消息，只在已闭合边界增加总结指令。不能通过清空 tool history 掩盖错误。
- [ ] 失败的助手分片保留为 incomplete 证据，不进入可回放完整序列；用户仍能看到已输出部分，重试为显式新 attempt。
- [ ] 加入 `TestNativeTargetMismatchRebuild`：同名换端点、GLM/DeepSeek 互换、备用凭据、profile 升级；均保留任务成果且新建 epoch。
- [ ] 加入 `TestTaskOutcomeForcedSummarySequence`：工具组结束后触发总结，server 看到工具组一次且调用 ID 完整；取消/重启/丢失 commit ack 时无重复工具执行。

```powershell
go test ./internal/modelfit ./internal/llmadapter ./internal/app -run 'Test(NativeTargetMismatchRebuild|TaskOutcomeForcedSummarySequence|RunStreamContinuesAfterPrematureStop|TurnJournalSurvivesRestartAndLostCommitAckWithoutDuplicates)' -count=1
```

**验收：** 完整历史可续接；不相容历史有可解释重建结果；“会话恢复成功”不能替代“原生状态恢复成功”。

## T06 — 任务级原子准入、预留与结算

**对应：** FR08、FR26。**依赖：** T02；接线入口依赖 T03。

**文件：**
- 新建：`internal/domain/agentrun/execution_budget.go`、`internal/agentrunapp/execution_budget.go`、对应测试。
- 修改：`internal/storage/sqlite/agent_runtime.go`、`internal/domain/agentrun/repository.go`、`internal/agentrunapp/service.go` 的 UoW 预算端口。
- 新建：`migrations/0155_execution_contract_v2.sql`；任务三表与 reservation 重建按合同。
- 新建：`internal/storage/sqlite/execution_budget_test.go`。

- [ ] 定义 ExecutionScope 与有效预算策略；nil 未设置、0 禁止。root 任务创建先于任何模型辅助调用。
- [ ] 为每条运行路径绑定稳定 task，run 需要符合已有 AgentRun 外键。Chat/旧 Plan 的外部运行 ID 单独映射，不能随便把 sessionID 填作 runID。最小 accounting run 不改变旧路径的调度权威。
- [ ] 按合同11.2持久化子scope policy、root/child活动区间及heartbeat；reserved_v2_json/settled_v2_json保存分项tokens与输出字节。补并发活动时间不双计、重启不清零、partial→final只记差额的测试。
- [ ] SQLite 写事务内检查 task/child 的 consumed+reserved+isolated，加上本次 input upper+output cap；通过后写 reservation、attempt、call intent。两个并发 Admit 不能都看到同一余额。
- [ ] 父子 scope 聚合读取同一条 reservation，不向父表再加一次消费。取消子代理只释放确定未派发的额度。
- [ ] 重建 reservation CHECK 以允许 isolated；保留旧 reserved_json/committed_json 和已有记录。严格使用幂等 receipt digest，不能终态后一律拒绝结算。
- [ ] 已派发但没有完整 usage 保留已知实际量和未知隔离额；reported 高于预留也记录，并设置 overrun、阻止新调用。cached input 是 input 子集，reasoning 是 output 子集，不再重复相加。
- [ ] 收尾额度为总预算中的保留部分；模型额度不足时产生宿主确定性摘要，不为总结额外绕过预算。
- [ ] `TestExecutionBudgetConcurrentParentChildReservation`：总预算 1000，3 个并发请求各 700，最多一个准入；同一 reservation 在父子查询累计一致；重复 settlement 不加倍。
- [ ] `TestExecutionBudgetLateOverrun`：预留 700，run 已取消，迟到实报 900 必须结算；之后新 200 请求被拒；部分 usage 与 unknown 请求的隔离额度不可被重启清除。

准入事务必须满足：

```text
begin write transaction
load effective task + child policy and current reservations
reject unless all enforced dimensions admit requested upper bound
insert attempt(callID, attemptID, digest)
insert reservation(taskID, valid accounting runID, scopeID, estimate)
insert or update durable call intent
commit
```

```powershell
go test ./internal/agentrunapp ./internal/storage/sqlite -run 'TestExecutionBudget' -count=1
```

**验收：** 无“记账失败但模型请求已经发送”；未知消耗可见；账本能解释每一次实发。不得宣称应用估算可保证供应商账单永远不超额。

## T07 — 所有调用路径的上下文与预算接线

**对应：** FR07、FR08、FR13。**依赖：** T03、T06。

**文件：**
- 修改：`internal/contextapp/assemble_envelope.go`、`internal/app/chat_run_stream.go`、`continuity_wire.go`、`plan_execution.go`、`chat_subagent.go`、`internal/m7app/subagent.go`。
- 修改：`internal/agentrunapp/plan_execution.go`、`internal/llmadapter/request_efficiency.go`。
- 新建：`internal/app/execution_preflight.go`、`execution_preflight_test.go`、`internal/contextapp/compiled_tools_test.go`。

- [ ] 先由 rg 枚举所有 adapter Complete/Stream、council/planner/judge/flash/compaction/Office 辅助入口，在 baseline 附录登记；不能只改主 Chat。
- [ ] 创建 root scope→加载资料→编译最终 tools/skills→组装候选请求→上下文预检→必要压缩→重编译最终请求→预算准入。压缩调用本身消耗同一 task，禁止递归压缩。
- [ ] input 估算覆盖 system、历史、tool schema、用户附件与当前工具返回；使用 profile 的 window、安全余量和最终 max output。没有可靠 tokenizer 时保守估算并记录版本，不用字符数当实测 token。
- [ ] 同一任务显式追踪 prompt、skill revision、compiled tool catalog digest。工具筛选只改变可用集合，不抬高不可信资料的消息角色。
- [ ] 在现有 skillapp 的第一方资源机制中加入 office-brief、office-narrative、office-revision、office-delivery，严格使用主PRD 19.1的输入输出。MCP复用现有runtime，只把已配置且授权工具纳入catalog；缺工具不自动安装、不提升权限。四个流程分别由D01/P01/D03/C04覆盖，不另建技能运行框架。
- [ ] 移除 PlanExecution 调用后 Charge 的独立权威；保留 run.Used 作为账本投影。原 meteredAdapter 变成共同 hook 的观测层。
- [ ] `TestFinalInputPreflightEveryAttempt` 用 L01：大工具返回、下一轮新 tool schema、重试参数变化分别导致重新计算；budget 拒绝时 mock server 请求计数不变。
- [ ] `TestCompiledToolCatalogStable` 覆盖相同工具输入顺序差异产生稳定 digest、版本变化改变 digest、未知工具不被模型文本临时注册、附件始终 untrusted。
- [ ] 逐行覆盖合同中的入口矩阵；测试抓取每个入口的 purpose/task/scope，无 scope 的生产模型请求拒绝。

```powershell
go test ./internal/contextapp ./internal/app ./internal/agentrunapp -run 'Test(FinalInputPreflightEveryAttempt|CompiledToolCatalogStable|AssembleEnvelope|AssembleToolCall)' -count=1
```

**验收：** 主循环、计划、子代理和压缩共享准入；超长输入可暂停或受控压缩，不依赖服务器先报错。

## T08 — 可信步骤验收与原文件快照

**对应：** FR09、FR10。**依赖：** T06—T07。

**文件：**
- 新建：`internal/domain/agentrun/task_outcome.go`、`internal/agentrunapp/task_verifier.go`、对应测试。
- 新建：`internal/toolruntime/artifact_snapshot.go`、`artifact_snapshot_test.go`。
- 修改：`internal/app/plan_execution.go`、现有 tool receipt 存储与 `internal/storage/sqlite/execution_task.go`（新建）。
- 测试：`internal/app/task_evidence_test.go`。

- [ ] 用户目标编译为 StepSpec：required、依赖、允许工具、AcceptanceCheck。模型可提议步骤；required 的建立/修改由宿主规则和用户操作提交新 goal revision。
- [ ] ReceiptResolver 仅从宿主工具运行与已知 validator 读取 immutable receipt；拒绝消息正文、模型 JSON 自报的 L0、伪造 test passed。
- [ ] 修改 `internal/toolruntime/runtime.go` 注入已有 workspace CAS；在 `internal/domain/agentrun/resource.go` 增 Evidence metadata，并更新 AppendEvidence/ListEvidence/GetEvidence。Snapshot方法只写blob，应用UoW将descriptor、receipt、step outcome原子关联，按合同11.3校验scope。
- [ ] SnapshotWorkspaceArtifact 复用路径授权与文件句柄策略，从完整原始 bytes 得 SHA 和 length；不要调用分页 workspace.read 的文本当原文件内容。
- [ ] 读取前后比对文件身份/长度/修改状态，尽量使用受控句柄/临时快照；并发修改重试一次，仍变动则 ARTIFACT_CHANGED。Office blob 直接用不可变 blob SHA。
- [ ] Evidence 将 task/goal/step/attempt、source SHA、validator版本、check结果绑定。跨 revision 复用需显式证明所需输入未变化。
- [ ] EvaluateTask 按依赖图判断所有 required；文件存在只满足 existence，不替代数值/可编辑/渲染检查。
- [ ] `TestArtifactSnapshotRawTail` 写 1 MiB fixture，修改第 900000 字节且前 3200 字节不变，两次 SHA 必须不同；Office 源文件 byte SHA 与提取文本 SHA 不混淆。再破坏同名CAS blob，resolver必须拒绝验证；重启后仍可解析完好的快照。
- [ ] `TestTaskOutcomeRejectsModelAuthoredL0` 与 `TestTaskOutcomeMissingRequiredStep`：模型声称完成但 receipt 缺失、错 scope、过期 digest、漏第二个必需文件时 success=false。

验收判定顺序：

```text
for each required step in topological order:
  require dependencies satisfied
  load trusted receipts matching task + goalRevision + step
  reject missing, unknown, mismatched source/validator evidence
  apply host check predicates
success = every required step verified AND no unresolved required effect
```

```powershell
go test ./internal/toolruntime ./internal/agentrunapp ./internal/app -run 'Test(ArtifactSnapshotRawTail|TaskOutcomeRejectsModelAuthoredL0|TaskOutcomeMissingRequiredStep)' -count=1
```

**验收：** 任务状态可由回执独立重算；不用模型自己的“完成”字段决定成功。

## T09 — 重启、未知副作用与任务结果提交

**对应：** FR06、FR08、FR11、FR12。**依赖：** T05、T08。

**文件：**
- 修改：`internal/app/chat_continuation.go`、`chat_run_stream.go`、`plan_execution.go`、`chat_subagent.go`、`internal/m7app/subagent.go`。
- 新建：`internal/agentrunapp/task_recovery.go`、`internal/storage/sqlite/execution_task.go`、对应测试。
- 修改：现有 turn journal、plan checkpoint 和 completed event 生成位置。

- [ ] 建立 goalRevision/outcomeVersion 的 CAS 提交；TaskID 不随 stream 或 resume 变化。
- [ ] 修改 `internal/agentrunapp/kernel.go` 的 RunRecoveryScanner 与V1预算入口，按binding.execution_mode排除纯accounting运行；V2 task_recovery独占其恢复，owned_runtime保留原工具恢复。按合同11.2处理Budget.policyVersion=2、活动CAS和幂等记录；测试纯计账root重启后保留预算并可继续，含估算区间的计时不能恢复为measured。
- [ ] checkpoint 与原生账本 head、工具回执引用、任务结果在可用的同一 UoW 中提交；大文件先耐久写入再存引用，不把外部命令放 SQL 事务。
- [ ] 启动后识别 prepared/dispatched/settled；已准备未派发可释放，已派发未知不能回放写工具或释放额度。
- [ ] reconcile 仅针对工具声明的幂等查询能力：按 artifact SHA、transaction ID 或原生回执确认。没有确认机制则待核实，保留成果及剩余步骤。
- [ ] 工具调用失败、预算停止、模型 finish、用户取消与成功分别映射 TaskOutcome；stream completion 只结束传输。
- [ ] `TestExecutionResumeUnknownEffect` 在“写入成功、ACK 丢失”“结算前崩溃”“完成事件丢失”三处注入失败；恢复不重复写，迟到 receipt 能推进结果。
- [ ] `TestOutcomeGoalRevisionOrder` 让旧 attempt 在新目标后完成：旧证据入历史但不能覆盖新 outcome；重复 CompletedEvent 幂等。
- [ ] 复跑已有断线继续、准备工具未知不可回放、子代理重启回归。

```powershell
go test ./internal/app ./internal/agentrunapp ./internal/storage/sqlite -run 'Test(ExecutionResumeUnknownEffect|OutcomeGoalRevisionOrder|RunStreamKeepsWorkingWhenUIDisconnects|PlanExecutionRestartMarksPreparedToolUnknownWithoutReplay|SubagentSpawn)' -count=1
```

**验收：** 没有将“进程退出/模型停止/工具退出 0”直接等同任务完成的路径。

## T10 — 任务结果 Bridge 与前端状态

**对应：** FR12、FR25。**依赖：** T09。

**文件：**
- 修改：`api/bridge/v1/` 中 chat/task/event 源 schema、`internal/app` 方法 registry。
- 修改：`web/src/session/liveChat.ts`、`sessionResume.ts`、`SubagentActivityRow.tsx`、`web/src/workspace/CoordinationPlanPanel.tsx`。
- 新建：`web/src/session/taskOutcome.ts`、`TaskOutcomePanel.tsx`、`taskOutcome.test.ts`。

- [ ] 增加 chat.task.get、task_outcome 事件和 chat.start 可选字段；生成 Go/TS 并检查 guard。
- [ ] reducer 先比较 goalRevision，再 outcomeVersion；重复忽略，逆序忽略，sequence 缺口触发 get，不能要求所有事件永不丢失。
- [ ] UI 将“回复结束”与“任务完成”分别显示；可展开剩余步骤、已产出文件、所缺验证。额度不足提供保持 TaskID 的继续入口。
- [ ] 旧会话没有 TaskOutcome 时显示“历史结果未验证”，保留现有阅读/下载，不追溯伪造通过。
- [ ] 在 `TestOutcomeGoalRevisionOrder` 的前端配套用例中测试乱序、重复、恢复查询和旧客户端 payload。
- [ ] `TestModelFitAndOfficeOutcomeUI` 的任务部分先覆盖：spinner 结束但必需文件缺失时仍显示部分完成，不能出现完成绿标。

```powershell
npm --prefix web run generate:bridge
npm --prefix web run verify:bridge
npm --prefix web run typecheck
npm --prefix web test -- src/session/taskOutcome.test.ts src/session/liveChat.test.ts src/session/sessionResume.test.ts
```

**验收：** 网络中断或事件重放后，UI 与持久化 TaskOutcome 一致；不新增普通用户必须理解的底层协议字段。

## T11 — 模型声明、探测、资格与采用

**对应：** FR14、FR16。**依赖：** T02、T05、T06。

**文件：**
- 新建：`internal/modelfit/lifecycle.go`、`internal/modelquality/service.go`、`internal/storage/sqlite/model_fit.go`、对应测试。
- 修改：`internal/modelfit/qualify.go`、`internal/app/model_fit.go`（新建）、Bridge model.fit 源 schema。
- 完成：0154 中 model_fit_runs、model_qualifications_v2、model_active_bindings。

- [ ] 实现 bind/get/probe.start/run.get/run.cancel/activate/rollback，start 在 5 秒内返回 runId；后台工作遵守 task budget、取消和持久状态。
- [ ] declared/observed/qualified 按 scope 独立保存。连接成功不能推出工具调用可靠，工具可靠不能推出 Office 格式正确。
- [ ] fixture 与 live 明确区分。资格绑定 target/profile/suite/app relevant version/renderer；过期或相关组件升级使对应 scope 需要重验。
- [ ] 激活必须指向未过期且覆盖请求 scope 的资格。active CAS 保留 previousBinding，旧绑定不被覆盖。
- [ ] rollback 仅恢复仍兼容当前应用和策略的旧绑定；不可用时返回明确状态，不能关闭验证硬闸。
- [ ] `TestQualificationFixtureCannotPromote`：mock/fixture_pass 不能激活 live；scope 不完整、到期和来源摘要变化不能自动通过。
- [ ] `TestActivationCASAndRollback`：并发采用只成功一个；回撤保留历史；不存在合法 previous 则 unavailable；重复 idempotencyKey 不产生新revision。

```powershell
go test ./internal/modelfit ./internal/modelquality ./internal/storage/sqlite ./internal/app -run 'Test(QualificationFixtureCannotPromote|ActivationCASAndRollback|ModelFitBridge)' -count=1
```

**验收：** 新模型能纳入同一流程，而不是代码里继续增加 if model-name；“跟随成长”意味着可验证地采用新绑定，不是未经测试自动更换。

## T12 — 24 项固定评测、留出集与证据

**对应：** FR15、FR16、FR26、FR28。**依赖：** T11；Office live 验证依赖 T16—T17。

**文件：**
- 新建：`internal/modelquality/suite.go`、`runner.go`、`oracle.go`、`evidence.go`、对应测试。
- 使用：T01 的 `internal/modelquality/testdata/upgrade-v1/`。
- 新建：`docs/audits/model-office-upgrade/evidence.schema.json`，模型 eval Bridge endpoint。

- [ ] 将 24 case 映射为输入、允许工具、预算、独立 oracle、fixtureOnly、期望状态；输入 builder 固定 seed，不读取私人目录。
- [ ] 实现代码/数据 oracle：C01 隔离运行固定测试；C02 独立 CSV parser 算唯一 ID/latest 行；C03 独立只读 SQL expected；C04 从四文件解析事实并交叉比较。
- [ ] Office oracle 分离内容、文件结构、实际渲染和模板盲评；X01 按 integer cents 汇总为 2195700，X02 独立计算 0.25/0.9375，X03 重算得到 110。不能用生成器同一个公式函数验证自己。
- [ ] R02/R04、L02—L04 等 fixture case 由故障注入跑；是否通过 oracle 与模型任务是否 success 分开统计。
- [ ] F03 supported-font 进入12办公正例×3=36次；missing-glyph 为额外故障子场景。R01 精确合成字段与真实观测重放分别运行；C04 正常交付与单验证器失败分别运行。按 scenario 的 statisticGroup 统计，不互相混入分母。
- [ ] 每个 live 候选 3 次独立运行，所有 started task 纳入分母，取消/预算不足/崩溃都留记录；infra 失败另分组并提供含/不含两种口径，不能静默删除。
- [ ] 增加一组同结构留出输入：换数字、长标题、表行数和时区；在版本冻结后运行，不用于调 prompt。用摘要登记，不在模型系统提示中泄露 expected。
- [ ] 同模型旧/新运行时对照与产品间对照分表；外部 Codex 无 usage 则 unknown，不计算虚假性价比。
- [ ] `TestEvalSuiteAllCasesHaveIndependentOracle` 验证每 case 都有独立判定、预算、输入digest，故意改坏一项输入能触发失败。
- [ ] `TestTemplateCertificationAndBenchmarkAccounting` 验证 fixture pass 不增加 live 成功数、F03 missing-glyph 正确阻断不增加交付成功数、重复运行不覆盖首次结果。

```powershell
go test ./internal/modelquality -count=1
node scripts/verify-model-office-upgrade.mjs
```

**验收：** 保存成功率、质量维度、耗时、实报/估计/未知用量和失败分布；评测结果可重算，模型不决定自己的通过结果。

## T13 — Office Brief、品牌、模板资格与唯一交付决定

**对应：** FR17、FR18、FR23。**依赖：** T01；TaskOutcome 关联依赖 T08。

**文件：**
- 修改：`internal/officestudio/contract.go`、`theme.go`、`layoutplan.go`、`quality.go`。
- 修改：`internal/domain/officestudio/officestudio.go`、`internal/officeapp/service.go`。
- 新建：`internal/officeapp/delivery_gate.go`、`delivery_gate_test.go`、`internal/storage/sqlite/office_delivery_decision.go`。
- 新建：`migrations/0156_office_delivery_v2.sql`。

- [ ] 复用 Brief/Spec v2、ops/brand/editorial 三品牌、现有 layout planner/repair。增加 spacing/lineHeight/tableHeader/chartSeries/minBodyPt/fontFallbackPolicy 等 token，不新造平行 IR。
- [ ] Brief 将 audience、purpose、brand revision、页数/格式、事实、锁定项、源引用和编辑要求持久化；四格式引用同一事实源。
- [ ] 加入合同第12节的精确variant、token、证书/样稿/撤销表与失效规则。样稿复用version/validation，保护被证书引用的blob；不另造未被GC追踪的图片存储。
- [ ] 加入模板认证状态。36 枚举做机械验证，先认证 12 核心组合，未审阅组合不进入正式自动选择池。三套品牌的 12 页固定样稿由认证组合构成。
- [ ] 建立 DeliveryPolicy 与 FormalDecision；服务层计算唯一决定，前端、Bridge 和 bundle export 共用。映射旧 check ID，不保留独立的“quality == passed 即放行”分支。
- [ ] 缺检查→needs_review；已知失败→blocked；required 全通过且 source/policy/renderer/fonts 一致→verified。异常不会变成 passed。
- [ ] 用户 accept 与 validation 分离；老 Formal/Draft 参数采用合同中的兼容矩阵，不默认改变旧 export 的保护语义。
- [ ] `TestOfficeBriefBrandFactsAcrossFormats` 用同一事实集生成四种 Spec，核对口径和锁定项；品牌差异应体现在布局/表格/图表，不仅封面颜色。
- [ ] `TestFormalDecisionRequiredCoverage` 覆盖旧 quality=passed 但实际证据缺页、source变更、policy更新、visual-required无结果、目标软件未测。全部不得 formal。

```powershell
go test ./internal/officestudio ./internal/officeapp ./internal/storage/sqlite -run 'Test(OfficeBriefBrandFactsAcrossFormats|FormalDecisionRequiredCoverage)' -count=1
```

**验收：** 同一 version/policy 在 UI、单文件导出、整包导出得到相同决定；所有必检规则可由代码枚举。

## T14 — PPT 可编辑排版、容量控制与视觉检查

**对应：** FR18、FR19、FR28。**依赖：** T13。

**文件：**
- 修改：`internal/officestudio/generate.go`、`internal/officetools/studio_pptx.go`、`internal/officestudio/charts.go`、`layoutplan.go`、`quality.go`、`visual_model.go`。
- 新建：`internal/officestudio/layout_fit_test.go`、`pptx_delivery_test.go`、`visual_manifest.go`、`visual_manifest_test.go`。
- 修改：`internal/officeapp/service.go` 生成后检查编排。

- [ ] 将受控模板定义为槽位、字号下限、边界、最大行/字数、图表类型和信息密度；标题/正文先测量，再决定拆页或换版式。
- [ ] repair 只做允许变更：删除冗余修辞、调整非锁定文案、改合法布局、拆页；不得改数字、单位、来源，不能把正文缩到品牌下限以下。
- [ ] 输出原生文本、shape、table、chart；照片/图标允许 image。保持 shape ID/node digest 用于局部修改，不把整页截图当可编辑 PPTX。
- [ ] 渲染真实 PPTX 为 PDF 后再导出全部 PNG；不能将 PDF bytes 作为 page.bin 冒充图像。manifest 的 pageCount/页 SHA 来自真实输出。
- [ ] visual runner 采用合同第 7 节有界 commandworker 与结构化 stdout，验证每页覆盖/摘要/schema；空 JSON、只第一页、超时、stderr 噪声及退出 0 无诊断均不算评审通过。
- [ ] `TestLayoutFitPreservesLockedFacts` 用 P02 长标题/12项对照，最多两轮 repair；锁定事实字面值不变且字体满足品牌下限。
- [ ] `TestPPTEditableObjectsAndCoverage` 解析 OOXML 统计文本/图表/形状并检查 relations；渲染后页数匹配；视觉结果漏一页必须拒绝全量标记。
- [ ] 人工复核 12 核心组合的原生可编辑性；3套品牌各12页样稿盲评达到主 PRD 门槛后写入 template certificate，保留评审版本。

```powershell
go test ./internal/officestudio ./internal/officeapp -run 'Test(LayoutFitPreservesLockedFacts|PPTEditableObjectsAndCoverage|VisualManifest)' -count=1
```

**验收：** P01—P03 的输出能打开、能编辑、内容正确，真实渲染无重叠/裁切；视觉诊断有输入绑定，模型建议不能放行硬错误。

## T15 — Word 长文与 Excel 可计算交付

**对应：** FR20、FR21。**依赖：** T13；渲染组件可与 T14 共用。

**文件：**
- 修改：`internal/officetools/studio_docx.go`、`internal/officestudio/generate.go`、`internal/officestudio/inspect.go`、`internal/officeapp/service.go`。
- 新建：`internal/officestudio/docx_delivery_test.go`、`xlsx_oracle_test.go`、`document_styles.go`、`workbook_profiles.go`。
- 新建：`internal/officeapp/recalculation.go` 与测试。

- [ ] Word ops 使用结论→指标→行动表，brand 使用观点→证据→建议，editorial 使用摘要→正文→注释/来源；生成语义 heading、页眉页脚和 section，不能仅用三种标题色作为模板差异。
- [ ] 处理长表跨页、重复表头、标题与下段同行、孤行控制、页码和目录 field；设置字段更新标志后还需通过真实渲染器更新并核验页码。无法更新显示 fields-update unknown。
- [ ] DOCX patch 只改指定 paragraph/run/tablecell，保留未涉及 OOXML parts；原文档不支持的复杂特性给出范围限制，不用重写全文掩盖丢失。
- [ ] Excel 使用真实 numeric/date/text 类型：六位订单号为 text，金额为 numeric 并配置样式；区分输入、公式、结果与来源。表名/命名区域稳定，公式引用可解析。
- [ ] workbook profiles 给出业务差异：ops 运营明细/汇总/异常，brand 销售漏斗/预算/ROI，editorial 数据字典/分析/来源。无业务数据时不编造示例收入冒充用户事实。
- [ ] 用隔离 LibreOffice 重算副本，比较公式依赖、结果和 Excel 支持函数范围；输出保留公式，不以缓存值覆盖公式。不支持函数报 unknown 并阻止要求可计算的正式交付。
- [ ] `TestDOCXLongTableAndFieldRefresh` 覆盖 D01—D03，校验65行表头、目录页码及不相关 part digest 不变。
- [ ] `TestXLSXIndependentOracleAndTypes` 覆盖 X01—X03：独立 integer-cents oracle、六位ID、0除、跨表公式、sum110、图表range。向缓存值注入错误仍能被重算/独立判定捕获。

```powershell
go test ./internal/officestudio ./internal/officeapp -run 'Test(DOCXLongTableAndFieldRefresh|XLSXIndependentOracleAndTypes|Recalculation)' -count=1
```

**验收：** 文档结构可编辑、页码正确；表格数据类型与公式真实有效；正确数值与美观分开验证。

## T16 — PDF 全页检查与统一正式导出

**对应：** FR22、FR23。**依赖：** T13—T15。

**文件：**
- 修改：`internal/officestudio/adapter.go`、`internal/officestudio/generate.go`、`internal/officetools/pdf.go`、`internal/officerender/renderer.go`、`internal/officeapp/service.go`、`internal/app/office_studio.go`。
- 复用：`internal/doctext/` PDF 解析与现有 commandworker。
- 新建：`internal/officeapp/pdf_delivery_test.go`、`export_gate_test.go`。
- 修改：Bridge Office export/bundle export schema。

- [ ] F02固定采用DOCX长表→同源PDF；独立PDF只承诺已支持的title/body文本。文本覆盖及单元格校验按合同11.4执行。
- [ ] PDF 检查真实页树、页数、读取错误、文字层、字体覆盖和每页渲染；删除仅凭 %PDF/EOF 可通过的判断。
- [ ] same-source 绑定 source version SHA、renderer/version 和转换参数。用户改 Office 后旧 PDF 标旧源，不自动套新来源。
- [ ] 局部 PDF 修改在受支持的源 Spec/Office 版本上执行并重新渲染；扫描件或任意复杂 PDF 不宣称可以恢复原生布局。
- [ ] AssessDelivery 只读取已有证据；validate 跑耗时检查。导出前再次对不可变版本与 policy 做决定，TOCTOU 变化失败，不用前端传入 allowed。
- [ ] 按兼容矩阵处理 deliveryMode/Draft/Formal：新 UI 显式 copy/formal；旧客户端未给新字段时保留旧安全语义；冲突参数拒绝。
- [ ] copy 可下载并如实说明状态；formal 必须匹配 required checks。对 not_applicable 给机器可判原因，不能以 renderer 缺失作为不适用。
- [ ] `TestPDFParseAllPagesAndSameSource` 覆盖 F01/F02 的多页、损坏中间页、缺字体、源变化；F03 必须报告缺字或不可验证。
- [ ] `TestFormalDecisionRequiredCoverage` 补直接调用 Service.Export、Bridge export、bundle export、旧参数绕过的回归，不能只测按钮隐藏。

```powershell
go test ./internal/officestudio ./internal/officeapp ./internal/app -run 'Test(PDFParseAllPagesAndSameSource|FormalDecisionRequiredCoverage|OfficeExportCompatibility)' -count=1
```

**验收：** 文件副本随时可按策略获取；正式交付有唯一后端决定且覆盖每页；未安装目标软件不会标该软件验证通过。

## T17 — 局部修改与跨文件指标一致性

**对应：** FR24。**依赖：** T16。

**文件：**
- 修改：`internal/officeapp/service.go`、现有 Patch/Metric/EvidenceEdge 服务、`internal/storage/sqlite/office_studio*.go`。
- 新建：`internal/officeapp/bundle_update.go`、`bundle_update_test.go`。
- 修改：`web/src/officeStudio/OfficeStudioPage.tsx` 的修改/版本状态处理。

- [ ] patch 输入固定 baseVersion、expectedRevision、node digest、允许范围；校验 CAS 后才生成候选，变更不得覆盖不相关内容。
- [ ] 对 source fact 记录 value/unit/period/currency/rounding/sourceVersion；同数不同口径视为冲突。所有格式引用共同 metric ID。
- [ ] 跨文件更新先产生全部候选与证据，后用一个 SQLite 事务提交 bundle 引用集合；一项失败保留旧 bundle，候选可查看但不能标整体一致。
- [ ] 所有源修改使原 FormalDecision 失效；未变化部分可复用证据须满足 source范围/validator/policy规则，不能只按文件名复用。
- [ ] undo/restore 创建明确版本关系，恢复旧源时只复用仍有效的同源 PDF；不会重新使用不兼容 renderer 的证据。
- [ ] `TestOfficePatchCASAndBundleAtomicity`：两客户端同revision改同节点只有一个成功；Excel更新成功而PPT失败不改变正式bundle；下一次重试不重复版本；同源PDF关系正确。

```powershell
go test ./internal/officeapp ./internal/storage/sqlite -run 'TestOfficePatchCASAndBundleAtomicity' -count=1
```

**验收：** 用户修改局部内容后，版本、数值口径、PDF来源和验证状态保持一致；不出现半更新的正式文件包。

## T18 — 模型/Office 产品界面与外部 Agent 成果归一

**对应：** FR25、FR29。**依赖：** T10、T11、T17。

**文件：**
- 新建：`web/src/settings/ModelFitPanel.tsx`、`ModelFitPanel.test.tsx`、`web/src/officeStudio/DeliveryStatus.tsx`。
- 修改：`web/src/settings/SettingsPage.tsx`、`web/src/officeStudio/OfficeStudioPage.tsx`、`OfficeStudioRoute.tsx`。
- 修改：`internal/agenthub/adapter.go`、`internal/agenthub/service.go`、`internal/app/agenthub_handlers.go`、`internal/storage/sqlite/agent_hub.go`。
- 新建：`internal/app/external_artifact_verification_test.go`、`web/src/officeStudio/DeliveryStatus.test.tsx`。

- [ ] 模型面板展示连接、原生协议、任务验证、办公资格、最近验证与当前采用；高级折叠展示profile/endpoint/证据，不在主流程强制读技术标识。
- [ ] 一次启动探测/评测后允许关闭面板；后台结果可查询、取消，刷新不重复付费任务。
- [ ] Office 显示预览类型、版本、所缺检查和导出模式；copy按钮不因尚未formal而不可用，formal错误给具体缺项。
- [ ] 外部 Agent 标 executor 与已知 model/session；usage缺失为unknown。退出0只代表外部过程结束，其文件通过原路径权限、快照和Office验证后才能交付。
- [ ] 不在本包实现 Codex App Server。保留当前 CLI 接入的已知恢复边界，重启无法续接显示 interrupted。
- [ ] `TestModelFitAndOfficeOutcomeUI` 覆盖探测进行/失败/过期/激活、任务部分完成、缺渲染器、证据过期、正式导出失败仍能导出副本。
- [ ] `TestExternalExecutorArtifactVerification` 覆盖路径越界、不存在文件、退出0但文档损坏、usage未知、有效产物经同一 FormalDecision 通过。

```powershell
npm --prefix web run generate:bridge
npm --prefix web run verify:bridge
npm --prefix web run typecheck
npm --prefix web test -- src/settings/ModelFitPanel.test.tsx src/officeStudio/DeliveryStatus.test.tsx src/officeStudio/OfficeStudioPage.test.tsx
go test ./internal/app -run 'TestExternalExecutorArtifactVerification' -count=1
```

**验收：** 用户能看懂可做什么、验证了什么、还有什么未完成；外部能力不会算作自有模型适配成绩。

## T19 — 迁移、备份、删除、开关与恢复演练

**对应：** FR05、FR11、FR26、FR27。**依赖：** T05、T09、T11、T16—T17。

**文件：**
- 修改：`internal/storage/sqlite/backup.go`、`maintenance_backup.go`、现有 dump/delete/UoW manifest。
- 修改：`internal/app/protocol_migration.go`、Engine 启动迁移与 feature 配置装配。
- 完成：0154—0156 与 `internal/storage/sqlite/upgrade_compatibility_test.go`。
- 新建：`docs/operations/model-office-upgrade-recovery.md`、`scripts/verify-upgrade-backup.ps1`。

- [ ] 数据库升级前取得启动互斥；一致性备份包含 DB 和协议 key 元数据，DPAPI 文件按现有同账户恢复策略打包。跨账户恢复不可解密时明确失去native状态，不尝试生成同名key。
- [ ] SQLite schema 事务迁移；运行时密文迁移独立可恢复。主界面可读旧内容，但 migration未完成的会话不能获得V2保护完成标记。
- [ ] 确认新表列入备份、导出、dump过滤、session/account删除；V1无FK部分显式清理，V2级联验证owner后删除。备份清单不含裸key/reasoning。
- [ ] 顺序启用 model_profiles_v2、protocol_ledger_v2、execution_contract_v2、office_delivery_v2；关闭高层功能不恢复旧明文或放松formal。
- [ ] 发布前准备兼容二进制，它识别新schema且拒写不支持状态；旧二进制不得直接写新库。恢复备份路径在隔离副本演练，报告恢复点之后数据范围。
- [ ] 补齐 `TestUpgradeMigrationBackupCompatibility`：旧DB→迁移→中断→续迁→校验→备份恢复；保留所有会话/Office版本/已结算预算；缺key/损坏备份/迁移冲突明确失败。
- [ ] `TestRuntimeEvidenceRedaction` 覆盖错误日志、任务导出、模型验证结果、SecretLease失败；sample sentinel 不可出现。
- [ ] 性能跑固定Windows机，1000条协议恢复与1000次准入记录p95；DPAPI在事务外、索引命中、日志有界。未达标查原因，不移除校验。

```powershell
go test ./internal/storage/sqlite ./internal/secret ./internal/secretlease ./internal/app -run 'Test(UpgradeMigrationBackupCompatibility|ProtocolCipherAADAndLegacyMigration|RuntimeEvidenceRedaction|ExecutionResumeUnknownEffect)' -count=1
```

**验收：** 升级失败不进入正常写入；已发生消耗不丢；恢复步骤真实演练，密钥边界和数据恢复点明确。

## T20 — 发布验收、独立审查与交接

**对应：** FR28、FR30。**依赖：** T10、T12、T18、T19。

**文件：**
- 新建：`docs/audits/model-office-upgrade/release-evidence.json`、`release-report.md`。
- 完成：`scripts/verify-model-office-upgrade.mjs`。
- 新建：`internal/modelquality/release_evidence_test.go`。

- [ ] 执行所有本计划包级回归、Bridge生成一致性、前端typecheck与相关UI测试；在隔离CI环境运行更广完整套件，将既有失败与新增失败区分并处理会影响发布的项。
- [ ] 运行两个首发目标的实际协议探测与固定live批次。记录完整requested/returned可知信息、profile、endpoint purpose、app/renderer版本；未验证目标不在release能力清单中。
- [ ] 跑12办公任务×3次并保留所有36份结果；完整交付至少32，硬性错误不能被静默当通过。F03 supported-font参与36次交付；missing-glyph另计故障合同通过率。
- [ ] 跑留出集，模板人工盲评以及12核心组合编辑性检查；36枚举机械检查通过但未人审者仍未认证。
- [ ] 从当前生产可执行版本的隔离副本完成升级/恢复演练；Windows中文路径、长文件名、缺LibreOffice、缺字形、只读输出目录均有可解释状态。
- [ ] 核对依赖/字体/模板/图片许可与分发方式。免费可使用与可随商业产品分发分别登记；本机已有Codex专用skill/runtime不能未经许可直接打包到产品。
- [ ] `TestUpgradeReleaseEvidenceComplete` 要求FR30条都有实现commit、测试命令/结果、运行证据digest，模型资格、模板证书、迁移记录均可解析。删除任一关键证据必须使校验失败。
- [ ] 独立审查者按主PRD第23节验收；对未完成项保持未完成，不通过降低required集合制造完成。
- [ ] 生成用户可读能力表、支持格式范围、已知限制、继续任务/撤回模型配置/恢复备份操作说明。此时才更新产品评分及“深度融合”“商用试运行”等对外描述。

```powershell
go test ./internal/modelquality -run 'TestUpgradeReleaseEvidenceComplete' -count=1
node scripts/verify-model-office-upgrade.mjs --release
npm --prefix web run verify:bridge
npm --prefix web run typecheck
```

**最终产出：** 一个可安装的完整候选发布包、真实验收证据、模型与模板资格、操作手册和迁移恢复包。文档本身的Schema/追踪校验不代替这些开发后的产品测试。

## 开发交接清单

- [ ] T01—T20 的负责人、依赖、提交和证据均已登记。
- [ ] FR01—FR30 无未映射需求；没有只更新PRD不更新合同的字段。
- [ ] 新旧客户端兼容矩阵、状态机和预算语义一致。
- [ ] 24固定任务、留出集、故障注入、人工设计评审采用不同证据类型。
- [ ] 生产不依赖付费设计产品的私有API；skills作为流程资源，MCP作为受控工具接入，核心排版/验证由产品负责。
- [ ] 用户结果由可信证据决定，模型自述不作为完成判定。
- [ ] 发布说明如实列出已验目标与已知支持范围。
