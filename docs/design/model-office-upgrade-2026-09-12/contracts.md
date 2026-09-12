# 开发合同附录

本文件为 [系统升级 PRD](E:/Trae-Work-Projects/lunitide/docs/design/PRD-model-office-system-upgrade-2026-09-12.md) 的规范性附录。以下名称是拟新增接口，标注“已有”的类型继续复用。JSON 字段以配套 Schema 为准；Go 字段命名按仓库风格映射，所有 public DTO 使用显式 json tag。

## 1. 模块依赖与职责

| 模块 | 负责 | 禁止依赖 |
| --- | --- | --- |
| `internal/modelfit` | profile、目标身份、原生消息、兼容判断、参数编译 | app、SQLite、桌面 UI |
| `internal/llmadapter` | 请求编码、供应商字段、流解析、每次实际 HTTP attempt 的回调 | UI、直接读取数据库 |
| `internal/domain/agentrun` | 任务合同、预算数学、结果与回执类型 | adapter、app、数据库 |
| `internal/agentrunapp` | 预算/验收事务编排、任务状态与权限作用域 | React、供应商专属 JSON |
| `internal/storage/sqlite` | 原子提交、CAS、索引、幂等与数据版本 | LLM 调用、外部工具执行 |
| `internal/secret` / `secretlease` | 随机存储密钥、DPAPI、短期租约 | 模型任务判断、Office 内容 |
| `internal/app` | 将模型、工具、任务、Office 服务接线到现有入口 | 在 handler 中重新实现预算/质量算法 |
| `internal/officeapp` | 不可变文件版本、真实检查、FormalDecision | 前端判定结果作为可信输入 |
| `internal/officestudio` | 现有四格式生成、布局、解析和规则检查 | 新建第二套存储或业务接受状态 |
| `internal/modelquality` | 固定任务执行、独立判定、资格证据 | 私人会话作为默认数据集 |

## 2. Profile 与身份合同

`ModelProfile` 的结构、枚举和完整示例保存在 `contracts.schema.json` 与 `profile-examples.json`。模型部署绑定必须显式选择 profile；名称启发式仅供建议，不作为身份事实。

```go
// internal/modelfit/profile.go；字段在实现时添加显式 json tag。
type ModeParameters struct {
    ThinkingType string  // enabled / disabled / omitted
    Effort       string  // omitted / low / high / max
    ClearThinking *bool
}
type ModelProfile struct {
    SchemaVersion int
    ProfileID, Family, CodecVersion, ModelContract string
    EndpointPurpose, Protocol string
    ModelIDs []string
    Modes map[string]ModeParameters // 必须恰好有 quick/standard/deep
    ReplayPolicy string // exact_assistant_history / tool_groups / none
    StrictTools bool
    StrictEndpointSuffix string
    ContextWindow, MaxOutputTokens int64
    SourceURLs []string
    SourceCheckedAt string
}
type ModelIntent struct {
    Mode string
    StrictTools bool
    OutputTokenCap int64
}
type EffectiveParameters struct {
    ThinkingType, Effort string
    ClearThinking *bool
    StrictTools bool
    MaxTokens int64
}
type TargetIdentity struct {
    ProviderID, Protocol, EndpointURL, EndpointPurpose string
    ModelRequested, Family, ModelContract string
    CredentialBindingID, ProfileDigest, CodecVersion string
}
func CompileParameters(p ModelProfile, in ModelIntent) (EffectiveParameters, error)
func TargetDigest(target TargetIdentity) (string, error)
```

profile digest 不存入被摘要的正文，计算 canonical JSON 的 SHA256。canonical JSON 规定对象 key 按 UTF-8 字典序，数组保留顺序；整数用十进制，禁止 NaN/浮点金额；字符串只做 JSON 转义，不做 Unicode 归一化。初始实现用一个确定性 Go encoder 与金样例校验，JS 不另写一套参与生产决策。

EndpointURL 是最终有效 API URL，含 path。规范化 scheme/host 小写、默认端口、去 fragment；保留 path 语义，拒绝 userinfo、密钥 query 和不允许的重定向。相同 host 不代表相同能力合同。profile 的 API 地址不修改现有网络/凭据授权范围。

`StorageKeyID`、`CredentialBindingID`、`TargetIdentity` 三者独立。凭据绑定在实际租约回调中获得；备用 key 被选中后必须更新本次 attempt 元数据。返回模型或权重版本未知则为 null，不能用 requestedModel 填充伪装观测。

## 3. 原生消息与提交接口

```go
// internal/modelfit/transcript.go
type NativeScope struct { OwnerScope, SessionID string }
type NativeMessage struct {
    ID, TurnID, CallID, Role, Provenance string // adapter_v2 / legacy_import
    Sequence int64
    Complete bool
    WireJSON json.RawMessage // json:"-"，禁止直接进入公共 DTO
}
type SealedMessage struct {
    ID, TurnID, CallID, Role, KeyID, Provenance string
    Sequence int64
    Complete bool
    Ciphertext []byte
}
type NativeEpoch struct {
    ID string
    Scope NativeScope
    Target TargetIdentity
    Profile ModelProfile
    Revision, LastSequence int64
    State string // active / closed / degraded / legacy
}
type NativeCommit struct {
    Scope NativeScope
    EpochID, CommitID string
    ExpectedRevision int64
    Messages []SealedMessage
    Checkpoint json.RawMessage // 仅保存引用和脱敏状态
}
type ReplayDecision struct {
    Mode string // native / structured_rebuild / unavailable
    ReasonCode string
    EpochID string
    Messages []NativeMessage
}
type NativeTranscriptStore interface {
    LoadNativeEpoch(context.Context, NativeScope, string) (NativeEpoch, []SealedMessage, error)
    CreateNativeEpoch(context.Context, NativeEpoch, string) (NativeEpoch, error)
    CommitNativeTurn(context.Context, NativeCommit) (NativeEpoch, error)
}
type NativeSnapshot struct {
    Epoch NativeEpoch
    Messages []NativeMessage // 经过认证解密，保持原顺序
}
func CheckReplay(snapshot NativeSnapshot, target TargetIdentity,
    profile ModelProfile) (ReplayDecision, error)
```

调用 idempotency 规则：同 CommitID、同摘要返回首次结果；同 CommitID 不同摘要返回 `NATIVE_COMMIT_CONFLICT`。CAS 不匹配返回 `NATIVE_REVISION_CONFLICT`。sequence 从 1 开始严格递增；一次批量提交与 checkpoint 更新共用事务。

消息 `Complete` 只描述该条供应商响应的完整性；助手带 tool_calls 后仍需所有对应工具结果才能成为下一次模型调用的完整边界。拒绝重复/陌生 tool_call_id，工具返回顺序可以按原实际提交次序保留，但不能丢失任何匹配项。

`WireJSON` 保存完整逻辑 message；缺字段、null、空字符串、multipart 顺序及 arguments 原值保真。SSE 分片边界不属于持久协议合同。未知扩展字段可加密存档，只有该 codec 的回放白名单才能回传。

当前 adapter 的效率优化、工具命名映射均应在冻结 PreparedRequest 前完成。冻结后不能再次改写历史工具名。每次实际发送的 RequestDigest、EffectiveParameters 必须来自同一 PreparedRequest。

```go
// internal/llmadapter/prepared.go
type PreparedRequest struct {
    Body json.RawMessage
    Digest string
    Target modelfit.TargetIdentity
    Effective modelfit.EffectiveParameters
    NativeMessages []modelfit.NativeMessage
    Stream bool
}
type ResponseMetadata struct {
    ModelReturned *string
    ModelRevision *string
    ProviderRequestID *string
}
type AttemptHooks interface {
    BeforeSend(context.Context, PreparedRequest) (attemptID string, err error)
    MarkDispatched(context.Context, string) error
    AfterAttempt(context.Context, string, AttemptResult) error
}
type AttemptResult struct {
    Dispatched bool
    State string // completed / failed / cancelled / unknown
    Usage Usage // 已有 llmadapter.Usage
    UsageIntegrity string // reported / partial / unknown
    ResponseDigest string
    Metadata ResponseMetadata
}
func PrepareChat(req Request, target modelfit.TargetIdentity,
    replay modelfit.ReplayDecision, profile modelfit.ModelProfile,
    stream bool) (PreparedRequest, error)
// 已有 Response 增加以下字段，不替换已有文本、工具结果、usage：
// Native *modelfit.NativeMessage `json:"-"`
// Metadata ResponseMetadata
```

`BeforeSend` 内完成预算 reservation + call intent 同事务。`MarkDispatched` 提交后才发送，若进程在这两步间退出按 unknown 保守处理。`AfterAttempt` 使用独立的有限结算 context，外层任务取消不跳过结算。流中途异常不得带着截断工具执行重试。

## 4. 存储密钥接口

```go
// internal/secret/protocol_keys.go
type ProtocolKeyService interface {
    EnsureProtocolKey(context.Context) (string, error)
    WithProtocolKey(context.Context, string, func([]byte) error) error
}
// internal/secretlease/protocol_keys.go
type ProtocolKeyRequest struct {
    KeyID string
    Deadline time.Time
    Nonce [32]byte
}
type ProtocolKeyLeaser interface {
    WithProtocolKey(context.Context, ProtocolKeyRequest, func(string, []byte) error) error
}
```

本地 LocalClient 与 RPC Client 共享语义。key 为空取得当前 key，非空只读已有 key；不存在不能生成同名新 key。DPAPI 包裹文件先原子落盘才允许 ciphertext 写数据库。V2 数据 blob 格式：1 字节版本 `2` + 12 字节随机 nonce + GCM ciphertext/tag；AAD 用长度前缀编码 formatVersion、owner、session、epoch、sequence、messageID、role、targetDigest、keyID、complete、turnID、callID、provenance。complete 编为一个字节，整数用 uint64 大端；字符串先写 uint32 字节长度再写 UTF-8。测试 key leaser 只在隔离测试使用。

## 5. 任务预算与结算合同

```go
// internal/domain/agentrun/execution_budget.go
type ExecutionScope struct {
    TaskID, RunID, ScopeID, ParentScopeID string
    GoalRevision int64
    PolicyRevision string
}
type ExecutionBudgetPolicy struct {
    MaxTotalTokens *int64
    MaxOutputTokens *int64
    MaxModelAttempts *int64
    MaxActiveMillis *int64
    MaxOutputBytes *int64
    CloseoutOutputTokens int64
}
type CallEstimate struct {
    CallID, AttemptID, RequestDigest string
    InputTokensUpper, OutputTokenCap, ContextWindow, SafetyMargin int64
    OutputBytesCap int64
    Purpose, TokenizerRevision, ProfileDigest string
}
type CallPermit struct {
    ReservationID, AttemptID string
    OutputTokenCap int64
    Deadline time.Time
}
type UsageTotals struct {
    InputTokens, OutputTokens, CachedInputTokens int64
}
type CallSettlement struct {
    ReservationID, ReceiptID, PayloadDigest string
    SettlementRevision int64
    Usage UsageTotals
    ModelOutputBytes, AttemptElapsedMillis int64
    Integrity string // reported / partial / unknown
    Dispatched bool
    Outcome string // completed / failed / cancelled / unknown
}
type BudgetSnapshot struct {
    ConsumedTotal, ConsumedOutput, ReservedTotal, IsolatedTotal int64
    Attempts, ActiveMillis int64
    ConsumedOutputBytes, ReservedOutputBytes, IsolatedOutputBytes int64
    Overrun bool
    Integrity string
}
type ExecutionBudget interface {
    AdmitCall(context.Context, ExecutionScope, CallEstimate) (CallPermit, error)
    MarkDispatched(context.Context, CallPermit) error
    SettleCall(context.Context, CallSettlement) error
    ReleaseUnsent(context.Context, CallPermit, string) error
    Snapshot(context.Context, ExecutionScope) (BudgetSnapshot, error)
    TransitionActivity(context.Context, ExecutionScope, ActivityTransition) error
}
type ActivityTransition struct {
    RuntimeEpoch, EventID, State string // active / heartbeat / paused / terminal
    ExpectedRevision int64
    At time.Time // 由宿主时钟提供，不接受模型或前端任意时间
}
```

所有限额 pointer 的 nil 表示未设置该维度，0 表示不允许；有效策略必须至少设置 total/output/attempts/activeMillis 四项。安全余量取 `max(1024, ceil(contextWindow*0.02))`，profile 可提高；低窗口模型如该余量使请求不可用则返回明确错误，不取负值。

同一 reservation 记录 root task 与 child scope。父预算和子预算是同记录的聚合，不复制消费。实际 input/output 实报超预留也记账并 overrun；已终态仍收迟到结算。未知消耗不能释放为可用额度。余额缺口由任务暂停和预算扩展解决，不靠重启清零。

收尾额度属于总额度子集，不能额外赠送；没有足够额度则用宿主基于回执生成的确定性摘要。压缩目的固定 `compaction`，每次最多一个活跃压缩调用，防递归。

覆盖矩阵：

| 调用入口 | Scope | 准入位置 | 结算位置 |
| --- | --- | --- | --- |
| Chat Stream | root task | adapter 实际 attempt hook | attempt 完成/取消 |
| council / planner / judge | root 下 purpose scope | 同上 | 同上 |
| plan step / PlanExecution | 绑定同一 task 的 run | 同上，移除调用后才 Charge 的主权威逻辑 | 同上；run.Used 只作投影 |
| subagent | parent scope 下 child | 同上 | 同上，父子不重复 |
| pre-turn / mid-turn compaction | root 下 compaction | 建 root scope 后才能调用 | 同上 |
| flash / Office 辅助模型 | 同任务 purpose scope | 同上 | 同上 |
| external Codex CLI | external 执行器预算范围 | 启动前检查外部任务策略 | 有实际 usage 才结算；缺失显示 unknown，不冒充内部严格 token 控制 |

## 6. 可信步骤、文件与任务结果

```go
// internal/domain/agentrun/task_outcome.go
type AcceptanceCheck struct {
    ID, Kind, ExpectedDigest string
    Required bool
    Predicate json.RawMessage // 由宿主按 Kind 解析的版本化对象，见第 11 节
}
type StepSpec struct {
    ID string
    GoalRevision int64
    Required bool
    DependsOn []string
    Checks []AcceptanceCheck
}
type CheckResult struct { ID, Status, ReasonCode, EvidenceRef string }
type StepOutcome struct {
    StepID, AttemptID string
    GoalRevision int64
    State, ReasonCode string
    ReceiptRefs, EvidenceRefs, ArtifactRefs []string
    CheckResults []CheckResult
    Claim, VerifierRevision string
}
type TaskOutcome struct {
    TaskID string
    GoalRevision, Version int64
    State, Completion, ReasonCode string
    RequiredSteps, PassedSteps, RemainingSteps []string
    EvidenceRefs, ArtifactRefs []string
}
type ArtifactSnapshot struct {
    ID, Path, SHA256, ContentRef string
    Bytes int64
    SourceIdentity string
}
type TrustedReceipt struct {
    ID, OwnerScope, TaskID, RunID, StepID, AttemptID string
    GoalRevision int64
    ToolName, ToolRevision, ArgumentsDigest string
    State, OutputSnapshotRef, CreatedAt string
    ArtifactRefs []string
}
type VerificationScope struct { OwnerScope, TaskID string; GoalRevision int64 }
type CheckEvidence struct {
    ID, CheckID, ValidatorID, ValidatorRevision, SubjectSHA256 string
    OwnerScope, TaskID, ReceiptRef, Status, PredicateDigest string
    GoalRevision int64
}
type ReceiptResolver interface {
    ResolveReceipt(context.Context, VerificationScope, string) (TrustedReceipt, error)
    ResolveArtifact(context.Context, VerificationScope, string) (ArtifactSnapshot, error)
    ResolveCheckEvidence(context.Context, VerificationScope, string) (CheckEvidence, error)
}
// 此含 I/O 的编排函数放 agentrunapp；纯判断函数在 domain 接收已解析证据。
func EvaluateTask(ctx context.Context, scope VerificationScope, specs []StepSpec,
    outcomes []StepOutcome, receipts ReceiptResolver) (TaskOutcome, error)
```

Step state：not_started/running/succeeded/failed/blocked/unknown/skipped。Task state 与 completion 见 Schema。宿主校验 required steps 覆盖、依赖、owner/task/goal revision、回执状态、artifact SHA 和必检结果。必需步骤 skipped 不可成功；可选步骤失败记录警告，不改变必要目标的通过判断。

文件快照由 toolruntime 提供受既有权限控制的方法：

```go
func (r *Runtime) SnapshotWorkspaceArtifact(ctx context.Context,
    mode Mode, sessionID, path string, unconfined bool) (agentrun.ArtifactSnapshot, error)
```

这里 `Mode`、路径策略和 session 解析复用现有 Runtime。大小上限首发沿用现有 8 MiB 文档工具限制；Office 已管理的大文件走 office blob 读取，不绕过其 quota。不能为实现验收而自动将所有工作区路径设 unconfined；传入当前已授权 execution mode。

## 7. FormalDecision 与视觉合同

```go
// internal/domain/officestudio/delivery_policy.go
type DeliveryPolicy struct {
    Revision, TargetRenderer string
    RequiredCheckIDs []string
    RequireEditable, RequireSearchable, RequireVisualReview bool
}
type QualityEvidence struct {
    ID, VersionID, SourceSHA256, PolicyRevision string
    ValidatorID, ValidatorVersion, Renderer, RendererVersion string
    FontDigest, ResultDigest string
    Checks []Check // 已有 domain.officestudio.Check
    PageCount int
    PageDigests []string
    CheckedAt string
}
type FormalDecision struct {
    DecisionID, VersionID, SourceSHA256, PolicyRevision string
    Allowed bool
    State string // draft / checking / needs_review / verified / blocked
    BlockingCodes, MissingChecks, EvidenceRefs []string
}
// internal/officeapp/delivery_gate.go
func (s *Service) AssessDelivery(ctx context.Context, taskID, versionID string,
    policy domain.DeliveryPolicy) (domain.FormalDecision, error)
```

检查规则优先级：缺 required→不通过；required failed→blocked；required unknown/unsupported→needs_review；全部 required passed 且证据有效→verified。not_applicable 只能由规则产生，并给出可核验原因。源 SHA、policy revision、目标 renderer 或 font digest 不匹配的证据不可复用。

初始固定必检集合：

| 范围 | Check IDs |
| --- | --- |
| 所有格式 | `file-integrity`, `source-content`, `locked-facts`, `font-coverage` |
| PPTX | `ooxml-relationships`, `native-editability`, `geometry-bounds`, `text-fit`, `actual-render`, `page-coverage` |
| DOCX | `ooxml-relationships`, `heading-structure`, `table-integrity`, `actual-render`, `page-coverage`；有目录时 `fields-update` |
| XLSX | `cell-types`, `formula-references`, `full-recalculation`, `calculation-oracle`, `chart-source`；请求打印稿时 `actual-render`, `print-area` |
| PDF | `pdf-parse`, `page-coverage`, `page-render`；要求可搜索时 `text-layer`；同源导出时 `same-source` |
| 用户附加要求 | `visual-review`, `target-powerpoint`, `target-wps`, `pdfa` 仅在明确要求时加入 required |

旧 check ID 用一张显式映射表转换，不能同时维护两套放行逻辑。`font-coverage` 对未知字形为 unknown，不用字体名字存在冒充字符覆盖。

视觉进程输入：manifest 路径，UTF-8 JSON，包含 schemaVersion=2、sourceSHA、renderer/version、pageCount、pages[{index,path,sha256,width,height}]。输出 stdout 一个 JSON 对象，最多 1 MiB；日志走 stderr 并截断到 64 KiB。单批最多 20 页，最多 2 并行批次，每批 90 s，总任务最多 5 min。合并必须覆盖 0..pageCount-1，无重复冲突，所有输入页 SHA 一致。结果含 reviewer/version、sourceSHA、reviewedPages、issues[{page,nodeId,code,severity,message,bbox}]；不包含“已验证”可覆盖硬检查的指令。

默认模板发布覆盖 36 个现有品牌×布局枚举，先做全量机械检查；其中 12 个核心组合（具体 ID 见第 12 节）必须通过完整人工设计审阅。其余组合未审阅则标未认证，不在正式模板自动选择池中。不能把 designerReviewed=0 的枚举数量当作设计认证。

## 8. Bridge 与事件

所有新方法复用 v1 envelope、错误包装与 owner/session 校验。以下 payload 不允许客户端指定 ownerScope；由已认证桌面上下文注入。长任务 start 仅建立任务并返回 ID，Bridge deadline 不覆盖整个评测。

| 方法 | Payload 必填；可选 | Result | deadline |
| --- | --- | --- | --- |
| `model.fit.get` | providerId, modelId | binding、declaredCapabilities、observed/qualifiedScopes、active、expiresAt | 5 s |
| `model.fit.bind` | providerId, modelId, profileId, profileDigest, endpointPurpose, expectedRevision, idempotencyKey | 新 bindingRevision | 5 s |
| `model.fit.probe.start` | bindingId, suiteId, budget, idempotencyKey | runId, state | 5 s |
| `model.fit.eval.start` | bindingId, suiteId, repetitions, budget, idempotencyKey | runId, state | 5 s |
| `model.fit.run.get` | runId；afterSequence | run、results、nextSequence | 5 s |
| `model.fit.run.cancel` | runId, expectedRevision | runState | 5 s |
| `model.fit.activate` | bindingId, qualificationId, expectedRevision, idempotencyKey | activeBinding, previousBinding | 5 s |
| `model.fit.rollback` | activeRevision, expectedRevision, idempotencyKey | 恢复的 binding 或明确 unavailable | 5 s |
| `chat.task.get` | taskId | TaskOutcome、budget、remainingSteps、latestSequence | 5 s |
| `office.artifact.assessDelivery` | taskId, versionId, policyRevision | FormalDecision | 5 s；只读已存证据 |

`office.artifact.validate` 复用现有检查任务入口，不新造第二个渲染任务系统。`office.artifact.export` 增加 optional deliveryMode 和 policyRevision，旧 draft 改为 optional 并保留其含义；`office.bundle.export` 同样增加并对包内所有版本执行 FormalDecision。`chat.start` 增加 optional taskId/expectedGoalRevision/modelIntent；旧客户端省略时创建默认合同或继续现有任务。

| 入口与字段 | 固定解释 |
| --- | --- |
| accept：formal=true | 先通过 FormalDecision 再采纳；不通过保留现有采纳指针 |
| accept：formal=false 或缺省 | 只采纳，不修改检查结果 |
| export：省略 deliveryMode，draft=true | copy |
| export：省略 deliveryMode，draft=false | formal，保留旧客户端正式意图 |
| export：两字段均省略 | copy，新 UI 正常情况下仍显式发送 deliveryMode |
| export：显式 deliveryMode，无 draft 或两者一致 | 使用显式 mode |
| export：显式 mode 与 draft 冲突 | INVALID_ARGUMENT；不能以优先级静默降级 |
| bundle：未传 deliveryMode | copy；formal 对每个固定版本检查 |

Go 的 Draft/Formal 使用 *bool 或等效 presence 类型，不能用 bool 的默认 false 区分缺省。单文件与整包 Service 导出入口也接受解析后的模式与策略，避免直接调用绕过 Bridge。

`task_outcome` 事件字段：taskId、goalRevision、outcomeVersion、outcome、lastSequence。消费规则按 goalRevision 再 outcomeVersion 单调更新；重复忽略、逆序忽略、间隙触发查询。CompletedEvent 可附 taskOutcome 摘要，不能与持久化版本不一致。

## 9. 错误码与确定行为

| 错误码 | 是否自动重试 | 产品行为 |
| --- | --- | --- |
| `MODEL_PROFILE_NOT_BOUND` | 否 | 使用明确的兼容模式或选择 profile；不得自动认证 |
| `MODEL_PARAMETER_UNSUPPORTED` | 否 | 显示不支持的意图/字段，保持原配置 |
| `NATIVE_TARGET_MISMATCH` | 仅可重建上下文 | 保留事实和预算，创建新 epoch |
| `NATIVE_SEQUENCE_INVALID` | 否 | 停止原生重放，提供 structured rebuild |
| `NATIVE_KEY_UNAVAILABLE` | 否 | 业务资料可继续，原生状态不可用 |
| `NATIVE_REVISION_CONFLICT` | 重新加载后最多一次 | 不能重复覆盖 sequence |
| `MODEL_CONTEXT_LIMIT` | 最多一次压缩/重建 | 仍不足则 paused，列出需缩减材料 |
| `TASK_BUDGET_EXHAUSTED` | 否 | 返回已完成证据、剩余步骤和额度操作 |
| `MODEL_USAGE_UNKNOWN` | 否 | 隔离未确认额度；显示用量未知 |
| `TOOL_EFFECT_UNKNOWN` | 仅受控 reconcile | 不能盲重放写入或宣称完成 |
| `TASK_EVIDENCE_INVALID` | 否 | 拒绝 verified，指出缺失或失效证据 |
| `ARTIFACT_CHANGED` | 一次重新快照 | 持续变化则要求稳定源文件 |
| `OFFICE_FORMAL_REQUIREMENTS_MISSING` | 否 | 可导出副本，显示缺项 |
| `OFFICE_VISUAL_RESULT_INVALID` | 否 | 视觉评审 unknown，保留其他证据 |
| `MODEL_QUALIFICATION_EXPIRED` | 否 | 不采用新配置；可重新验证 |
| `MODEL_BINDING_CONFLICT` | 否 | 显示最新 revision，不覆盖他人操作 |

400：只有 profile 中明确识别的可兼容错误允许新 attempt，至多一次且重新预算；其他 400 原因原样分类返回。429/503：仅收到明确未执行模型响应且尚未输出时，最多两次额外 attempt，遵守 Retry-After 和总预算；已流出内容/工具结果时不透明重跑。认证错误不跨供应商找 key，备用凭据仅在已有授权规则内使用。

## 10. 数据库变更与事务要求

以下为迁移的字段级定义；实现采用当前仓库 ULID、JSON、digest CHECK 风格。没有列在此处的逻辑状态不可静默加入。

| 表/变更 | 字段与索引 | 写入规则 |
| --- | --- | --- |
| `model_binding_revisions` 新建 | id PK, owner_scope, provider_id, model_id, revision, target_json, target_digest, profile_json, profile_digest, created_at；UNIQUE(owner_scope,provider_id,model_id,revision) | 不可变；完整 endpoint 存储前拒含敏感 query |
| `model_fit_runs` 新建 | id PK, owner_scope, binding_id FK, kind(probe/eval), mode(fixture/live), suite_digest, state, budget_json, result_ref, revision, created_at, finished_at | 状态 queued/running/completed/failed/cancelled；结果文件先耐久落盘才引用 |
| `model_qualifications_v2` 新建 | id PK, owner_scope, binding_id FK, scope, decision(observed/qualified/blocked/expired), evidence_json, expires_at, created_at | 追加；旧 fixture_pass 不迁为 qualified |
| `model_active_bindings` 新建 | owner_scope, provider_id, model_id 复合 PK, binding_id FK, previous_binding_id, qualification_id FK, revision, updated_at | 采用/撤回 CAS 事务；不覆写旧资格 |
| `protocol_epochs_v2` 新建 | id PK, owner_scope, session_id, target_json, target_digest, profile_json, profile_digest, revision, last_sequence, state, last_commit_id, created_at；INDEX(owner_scope,session_id,created_at) | owner+session 校验；消息 head 与 checkpoint 同事务 |
| `protocol_messages_v2` 新建 | id PK, epoch_id FK CASCADE, owner_scope, sequence, turn_id, call_id, role, complete, provenance, key_id, cipher_blob, created_at；UNIQUE(epoch_id,sequence) | 新 sequence 只能 append，原条重试摘要必须一致 |
| `protocol_commit_receipts` 新建 | owner_scope, session_id, epoch_id FK CASCADE, commit_id 复合 PK, sealed_payload_digest, result_revision, result_last_sequence, created_at | 与消息/head/checkpoint 同事务；重试复用首次 sealed payload |
| `protocol_migration_progress` 新建 | migration_id PK, revision, source_kind, last_owner, last_source_key, state, updated_at | 按稳定复合键分页，事务推进 |
| `protocol_legacy_imports` 新建 | source_kind, owner_scope, source_key 复合 PK, source_digest, epoch_id FK CASCADE, message_refs_json, state | 已完成源不因旧正文被清理而重新计算导入摘要 |
| `execution_task_contracts` 新建 | task_id PK, owner_scope, session_id, goal_revision, spec_json, budget_policy_json, policy_revision, revision, outcome_json, active_elapsed_ms, active_since, last_heartbeat_at, runtime_epoch, activity_integrity, created_at, updated_at | 必须步骤与预算变更 CAS；活动时间是墙钟区间并集 |
| `execution_run_bindings` 新建 | run_id PK FK agent_run(id), owner_scope, task_id FK, scope_id, parent_scope_id, run_kind, execution_ref, execution_mode(accounting_only/owned_runtime), scope_policy_json, scope_policy_revision, active_elapsed_ms, active_since, last_heartbeat_at, runtime_epoch, activity_integrity, created_at；UNIQUE(task_id,scope_id), UNIQUE(owner_scope,run_kind,execution_ref)；parent(task_id,parent_scope_id) 组合 FK | run_id 永远真实 AgentRun；子scope不能自引用或成环；外部标识独立 |
| `execution_step_outcomes` 新建 | task_id, goal_revision, step_id, attempt_id 复合 PK, state, outcome_json, revision, created_at | 追加尝试，最新合法结果由 verifier 选择 |
| `run_usage_reservation` 扩展/重建 | 保留旧 id/run_id/reserved_json/committed_json；增 task_id, scope_id, attempt_id, request_digest, dispatched, integrity, reserved_v2_json, settled_v2_json, receipt_id, settlement_revision, settlement_digest, isolated_json；状态增 isolated；INDEX(task_id,status), INDEX(task_id,scope_id,status) | 旧 CHECK 新表复制迁移；attempt_id 非空部分 UNIQUE；V2 task/scope/attempt 全非空或 legacy 全空；每次累计值按差额结算 |
| `office_delivery_policies` 新建 | revision PK, policy_json, digest, created_at | 不可变；固定 required set |
| `office_delivery_decisions` 新建 | id PK, owner_scope, task_id, version_id, source_sha256, policy_revision, evidence_digest, decision_json, created_at；INDEX(version_id,policy_revision,created_at) | 与引用版本 SHA 对应；formal export 前再次校验 |
| `office_template_certifications` 新建 | id PK, scope_kind(builtin/owner), owner_scope, variant_id, density, template_digest, brand_digest, font_digest, renderer_version, policy_revision, review_json, certificate_digest, created_at；INDEX(scope_kind,owner_scope,variant_id,density) | 不可变证书；所有身份匹配且未撤销才有效 |
| `office_template_certification_samples` 新建 | certification_id FK CASCADE, sample_index 复合 PK, version_id FK, validation_id FK, source_sha256, content_case_digest | 每证书固定3个样稿；沿用不可变version/validation的blob引用 |
| `office_template_certification_events` 新建 | id PK, certification_id FK CASCADE, action(revoke), reason, created_at | 追加撤销事件；不能覆写原评审 |

表数量对应已有模块扩展，不引入新数据库服务。新表 JSON 不保存裸 reasoning 或 API key。schema 限制：metadata JSON 256 KiB，task spec 1 MiB，单条协议密文至多 8 MiB，单 epoch 总量受 session 存储 quota 管理。超限拒绝原生继续或重建上下文，不截断后标完整。

0154 放模型四表与协议消息、epoch、commit receipt、迁移进度/映射五表；0155 放 execution 三表、reservation 重建及 evidence.metadata_json；0156 放 Office 交付两表和模板资格三表。实际编号冻结规则见 PRD。所有 UoW/delete/export/dump/backup manifest 一起更新；session 删除需要显式清理缺 FK 的 V1 表，V2 按 owner 验证后 cascade。SQL 事务中不做模型/DPAPI/渲染调用。

运行时加密迁移不放 SQL：每批 100 条，持久化 migration cursor；先加密并验证，再用 CAS 提交新记录并清理旧私有字段。中断重入用 legacy source ID 唯一映射，不能重复导入。数据库迁移与加密迁移各有状态，任一未完成不宣称原生保护升级完成。

## 11. 接线、恢复与证据的固定实现规则

### 11.1 实际凭据、原生捕获与幂等

在 `internal/app/provider_diagnostics.go` 的 `withProviderLeaseRef` 内，以本次选定 ref 构造可信 `ActualCredentialBinding{Ref, BindingRevision}` context 元数据；使用原有回调，无需假设旧 `WithLease` 能返回新 metadata。V2 固定采用不可变 ref：Host 每次保存不同 key 都生成新 ref，BindingRevision=ref，同 ref 仅允许完全相同 key 的幂等保存，否则拒绝覆盖。T02 补齐所有凭据写入口与测试；旧记录没有不可变保证时仅作为 legacy 来源，不认证原生回放。不得用 key 的公开摘要代替身份。

顺序为：选定实际 ref/取得租约→构建 TargetIdentity→CheckReplay→PrepareChat→准入→发送。备用 key 选择改变目标时重做这条链，旧 provider 主 ref 不得继续用于本次元数据。新 epoch 与旧 epoch 使用独立 StorageKeyID 规则，换 API key 不删除旧数据密钥。

Response.Native 在 adapter 解析时捕获。非流式保留字段存在性；流式按字段拼装全部 delta、原始工具名与 arguments 字符串，完成标记仅在协议完整结束时赋值。随后 UI 文本修饰、fallback 文案、日志裁剪不得改 Native。PrepareChat 的 replay 参数来自 CheckReplay 对认证 NativeSnapshot 的校验，或明确的 structured rebuild；req.Messages 只承载本次新增消息。native 模式不能再次附上 UI 历史，已捕获字段不再执行有损效率优化；工具名映射对已编码历史必须幂等。

NativeCommit 的摘要固定为首次 sealed payload 与 checkpoint 的规范编码 SHA256，覆盖全部索引字段、密文、顺序和完整性。**重试必须复用首次密文，不能重新随机加密。**准备提交的 NativeCommit（仅含密文与脱敏 checkpoint）先写现有 durable turn journal 的 prepared payload；崩溃恢复读该 payload。protocol_commit_receipts 保留至会话删除，A、B 提交后重试 A 仍返回 A 的 result_revision/result_last_sequence；摘要冲突返回 NATIVE_COMMIT_CONFLICT。提交完成后按现有 journal 清理规则删除 prepared payload。

旧源 source_kind 限定为 message_group/protocol_private/checkpoint，checkpoint 包括 journal 的历史分片。停用 V1 私有写入后按 source_kind、owner_scope、source_key 稳定分页。事务内再次核对源 digest，写 V2/映射、清理旧私有字段、推进 cursor。已完成映射不重新计算清理后的源摘要；来源不完整的消息认证 provenance=legacy_import，不能因归档成功变成 adapter_v2。

### 11.2 Scope 映射、活动时间与结算

新增 `agentrunapp.EnsureExecutionBinding(ctx, ownerScope, sessionID, taskID, runKind, executionRef, parentScopeID, policy)` 返回 ExecutionScope。它是内部 V2 用例，通过真实 session 创建最小 accounting AgentRun 及 binding，不调用要求伪造正数 cost 上限的 V1 Budget.Validate。V1 创建和计费语义保持原样，V2 用独立有效策略校验。

accounting_only 用于 task_root/chat_stream/m7_subagent/auxiliary 等仅需计账的运行；已有 PlanExecution/AgentRuntime 实执行 run 为 owned_runtime。旧 RunRecoveryScanner、V1 ReplaceBudget/ReserveUsage/CommitUsage 在入口检查 binding，跳过或拒绝接管 accounting_only，由 V2 task_recovery 独占恢复；owned_runtime 的原工具恢复继续生效，预算仍走 V2 准入。accounting 行不在 UI 另显示为一个用户任务。

为使旧 AgentRun 的 Budget JSON 可识别 V2，新增 policyVersion（缺省按1）和 executionTaskId 两个可选字段；纯计账行写 policyVersion=2 与真实 task 引用，领域校验只验证该模式身份，UoW 校验同事务内 task/binding/有效策略存在，不填伪造 cost。旧 V1 路径遇到 policyVersion=2 必须拒绝自行校验或修改；数据库读取、备份与恢复保留该标签。已有 owned_runtime 的 V1 Budget 保留为兼容信息；若其中仍有更严格业务约束，在绑定时显式投影到 V2，不能暗中丢掉用户原有限额。

root 使用 run_kind=task_root、execution_ref=taskID。每个 Chat stream 建 child binding（execution_ref=streamID），逻辑子代理/计划步骤绑定其真实运行 ID；PlanExecution 可复用已有 ex.RunID。继续流可创建新 child，task root 预算不重置；同一逻辑子任务的 resume 复用 scope。chat.start 没有 session 时通过已有 session 创建用例建立受 owner/project 管理的会话，不能填假 FK。parent scope 必须属于同一 task，在事务内验证无环。

活动时间按 root 任务和需要限时的 child scope 分别记录“至少一个受管理模型或工具执行处于 active”的墙钟区间；并发只计区间并集，不相加 attempt 时长。等待用户、暂停、终态不计时。TransitionActivity 使用 revision/EventID 幂等；active 时每秒更新 heartbeat，并在暂停/结束时累计区间。崩溃恢复保守计至 `min(恢复时间,last_heartbeat_at+30秒)` 后暂停，标 activity_integrity=estimated；正常结束为 measured。不能把机器离线数天全部算作执行时间，也不能在重启后抹去已执行区间。

root/child 活动更新统一用 execution_task_contracts.revision CAS；EventID 复用已有 idempotency_records，以 `execution.activity.v2` operation 和 task/scope/runtimeEpoch/eventID 的规范摘要作键，存 payload digest 与返回 revision，并与区间更新同事务。binding 记录 child 的 activity_integrity；只要累计区间曾含估算，整体保持 estimated，后续正常结束不改回 measured。并发活动引用按当前非终态绑定/attempt/工具状态在事务内重算，重复开始/结束事件不能提前暂停仍有活跃工作的 root。

reservation V2 JSON 带 schemaVersion=2，分别存 InputTokensUpper/OutputTokenCap/OutputBytesCap 与累计实报 InputTokens/OutputTokens/CachedInputTokens/ModelOutputBytes。ModelOutputBytes 是模型实际输出 UTF-8 字节，不包括重复显示或工具文件；AttemptElapsedMillis 为观测项，不当 task 墙钟累计量。

固定状态机：

```text
reserved → released    仅确认未派发
reserved → committed   完整权威 usage
reserved → isolated    已派发，用量部分已知或未知
isolated → isolated    更完整但仍不完整的累计 usage
isolated → committed   迟到完整权威 usage
```

新增 SQLite/UoW 方法 `SettleExecutionUsage`、`ReleaseExecutionUsage`、`IsolateExecutionUsage`、`ActiveExecutionUsage`；不放宽旧 V1 CommitUsage。首次未知与后来完整回执有不同 settlement_revision。每次传累计 actual，事务增加 newActual-oldActual，并重算剩余隔离；同 revision/receipt/digest 幂等，异摘要冲突，低 revision 不回退。完整实报不得低于已记可靠实报；出现矛盾则保留较大用量并标 receipt conflict，需显式对账，不能静默负扣。

V2 的 task/scope/attempt 约束必须同时有效；旧行全部为空。ActiveExecutionUsage 同时读 consumed/reserved/isolated，不继续使用只查 status=reserved 的旧函数。task/AgentRun 已终态仍接收结算，不改变任务执行状态。

### 11.3 快照、检查条件与持久化

通用文件快照复用 `internal/workspace/cas.go`。先完整写入、Sync、原子提交 CAS blob，再在已有 evidence 表写 descriptor；0155 为 evidence 加 nullable metadata_json（合法 JSON，最多 256 KiB，旧行为空）。kind=artifact_snapshot_v2，source_uri=`workspace-cas:<sha>`，content_digest 为完整原文件 SHA，run_id 为真实 accounting run。

metadata 固定：schemaVersion=2、ownerScope、taskId、goalRevision、snapshotId、sourcePath、sourceIdentity、contentRef、sha256、bytes、capturedAt。Office 使用 kind=office_snapshot_v2，contentRef=`office-version:<id>` 并引用现有 blob/version，不复制绕过 quota。内部验证快照不等于用户下载许可，不改变原有 executable/MIME 限制。

ResolveArtifact 校验 owner/task/goal 归属后重新确认 blob 的 SHA/size；引用缺失或损坏返回 TASK_EVIDENCE_INVALID。先落盘后提交元数据失败只产生可回收孤立 blob；不得提交指向未落盘文件的成功记录。快照跟随任务证据保留/备份/删除，活动任务引用不可被 sweep；删除元数据后按引用计数和现有保留策略清理。

AcceptanceCheck.Predicate 按 Kind 固定结构：

| Kind | Predicate 必填 |
| --- | --- |
| artifact | schemaVersion、format、requiredCheckIds、expectedFactsDigest、allowedSourceVersionIds |
| command_test | schemaVersion、argv 字符串数组、workingDirectory、codeRevisionDigest、testManifestDigest、expectedExitCode=0 |
| data_value | schemaVersion、selector、value、unit、period、roundingRule |
| delivery_bundle | schemaVersion、requiredFormats、factSetDigest、policyRevision |

ExpectedDigest 为完整 Predicate 的 canonical SHA；用户/模型传来的 CheckResult 只是候选引用，ResolveCheckEvidence 才提供可信结果。CheckEvidence 复用 evidence 表：kind=check_evidence_v2，metadata_json 存合同字段及 schemaVersion；content_digest 是该结构化结果的 SHA，source_uri 为真实工具 receipt 引用。命令验收只接受固定工作目录/代码版本/argv 的独立执行器结果，任意 exit0 不满足 command_test。

### 11.4 评测子场景与 PDF 长表范围

24 个 case ID 保持稳定；scenarios 列出同一 case 内的正例与注入故障，不隐藏增加成功样本。每个场景有 mode、statisticGroup、inputOverrides、checks、expectedTaskSuccess、expectedStates。执行时读取指定 scenario：字段 override 后再计算输入摘要，不能把 fixture 注入当真实供应商返回。

F03 的 supported-font 为办公正例，要求预先确认字体覆盖输入字符；missing-glyph 为 fixture 负例。12 个办公正例各 3 次构成 36 个交付样本，负例另计合同通过率。R01 的 fixture-exact-values 用于空值/超长推理字节保真，live-observed-replay 只核对真实取得的字段；C04 区分 all-formats-success 和 one-validator-fails。

F02 本批次明确采用 **DOCX 结构化长文/长表→同源 PDF**，复用 T15，不扩展独立 PDF 的 title/body 为隐式表格。独立 PDF 保留其已声明文本能力。文本覆盖比较允许移除排版产生的换行/分页、合并连续空白；保留汉字、数字、标点、单位和来源 URL 的有序 token，不得删数字或用 Unicode 归一化掩盖不同字符。表格另校验行/列及每个单元格的逻辑内容，90行拼正文不算表格通过。

## 12. 设计参数、认证组合与样稿

以下是本批次拟固定的默认 token，T13 写入现有 theme；不是声称当前代码已全部采用。所有色值为 sRGB，字号单位 pt，正文行距为倍数，段后为 pt。

| 品牌 | 正文/底色/强调/标题/风险色 | PPT 标题/正文/注释 | Word 正文/行距/段后 |
| --- | --- | --- | --- |
| ops-clear | 1F2937 / F4F6F8 / 0D9488 / 0B1F3A / B45309 | 30 / 20 / 11 | 11 / 1.35 / 6 |
| brand-pitch | 1F1233 / F5F0FF / 7C3AED / 1A0A2E / B45309 | 34 / 20 / 11 | 11 / 1.35 / 7 |
| editorial-report | 2C1810 / F8F4EE / 5B7C5A / 3F2E1E / 9A4D2E | 30 / 19 / 11 | 11 / 1.50 / 8 |

PPT 画布16:9；默认外边距0.5英寸、列间距0.3英寸；内容超过布局容量优先拆页。字体优先已授权且经 glyph 检查的中文字体，实际字体清单与摘要随证据记录；不依靠“系统必然装某字体”。不同品牌最低正文字号取上表正文值；注释只能用于来源/脚注，不能把普通正文标注释来缩小字号。图表系列用同品牌强调色、同色阶及中性灰，至少辅以标签/线型，不能仅靠红绿表达差异。

固定首发资格只覆盖以下 standard 密度：

| 品牌 | 4个实际 variant ID |
| --- | --- |
| ops-clear | ops-clear/cover/standard；ops-clear/two-column/standard；ops-clear/metrics/standard；ops-clear/conclusion/standard |
| brand-pitch | brand-pitch/cover/standard；brand-pitch/two-column/standard；brand-pitch/comparison/standard；brand-pitch/conclusion/standard |
| editorial-report | editorial-report/cover/standard；editorial-report/two-column/standard；editorial-report/comparison/standard；editorial-report/conclusion/standard |

每品牌12页：第1—4页为上述四布局典型内容；第5—8页为相同布局的允许容量边界；第9—12页为相同布局的中英混排、单位、负值/百分比和来源注释。共12个variant×3种输入样本，不代表认证compact/airy。LookupVariant补合法density枚举；记录requested variant、density、resolvedLayout，conclusion映射closing等转换必须可追踪。

证书绑定 variant/density/template/brand/fonts/renderer/policy 和三个 sample version/validation/SHA；任何依赖改变即不能复用。机械检查覆盖所有36工程组合；人工资格仅覆盖上述12个，metrics资格不自动扩展为所有原生图表资格。三名评审提交独立评分与标注，汇总规则按主PRD执行。

样稿保存在专用合成认证任务中，复用 artifact_versions、office_version_metadata、office_validation_runs；证书引用的样稿与validation不可被回收，删除样稿先撤销证书或返回被引用。内置资格仅随受信应用发布清单导入 scope_kind=builtin；用户组织内资格为owner范围，不自动发布全局。证书元数据存在SQLite，样稿/实际渲染复用现有version/validation blob引用，不新增无法被现有blob可达性追踪的独立PNG或证书文件。诊断PNG为可重建临时文件，证据保存其SHA和所依附validation。
