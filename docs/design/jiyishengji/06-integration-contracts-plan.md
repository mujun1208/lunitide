# Integration Contracts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐记忆、OCR、媒体之间的真实接线、权限、资源和用户体验合同。

**Architecture:** 保留现有 Go Engine/SQLite/Host Bridge/React。新增应用服务以受授权请求快照为输入，持久状态为真相；不新增外部记忆服务、通用事件数据库或第二套语音引擎。

**Tech Stack:** 当前 go.mod、SQLite、Windows/WebView2、TypeScript/Vitest；不预设尚未验证的 Paddle wheel 组合。

**Spec:** [完整PRD](02-upgrade-prd.md)。本文件是其规范性接口细化，03–05同时适用；执行顺序以 [11 总实施计划](11-master-implementation-plan.md) 为准，映射以 [12 需求追踪矩阵](12-requirements-traceability.md) 为准。

## Global Constraints

- 本文代码块为待开发合同或测试规格，不是声称当前仓库已有这些新接口。
- 所有新ID授权反查可信subject、scope kind/id；旧接口保留原字段并验证，不能通过省略新字段绕过权限。
- 当前IPC最大4MiB；新增Bridge结果编码后≤512KiB，单个artifact块原始bytes≤65536。
- 先基线/备份，再schema/数据保护，再自动化；模型包和Windows媒体垂直切片独立验证。
- 任何状态改变与外部副作用之间再查权限、取消和fencing；不持有SQLite写事务等待网络/播放/推理。
- 只提交实施任务文件；不得把用户已有dirty代码夹入提交。本文交付不执行这些开发步骤。
- `0160–0164` 在本文是 logical candidate。X0 生成 `evidence/migration-allocation.json` 后才冻结实际编号；任何子线不得自行顺延。

## C1. 记忆来源、时序、迁移与遗忘

### 模式与作用域唯一合同

wire/Go 固定使用 `captureMode`、`personalMemoryEnabled`、`projectMemoryEnabled`；DB 固定使用 `capture_mode`、`personal_memory_enabled`、`project_memory_enabled`。`auto` 允许已启用 scope 的自动捕获与召回；`manual` 禁止自动 candidate/banner，只允许 `memory.item.create` 显式保存，但可召回已启用 scope 的存量；`off` 禁止所有 capture/working/candidate/job/index/面向回答的 `memory.search`/recall/inject，保留管理和存量数据。scope=false 同时阻止该归属范围的自动捕获、显式保存、working、回答检索、召回和注入，并立即失效缓存，不删除数据。授权后的 `memory.item.list/get/history`、管理页过滤、export/correct/forget/purge 始终可达，以便关闭后仍能检查和删除存量；它们不得复用会注入回答的 search path。

legacy `memoryEnabled` 不再进入新 UI。0160 backfill 为 `memory_enabled=0 → off`，否则保留合法旧 `capture_mode`；新 scope 列默认 true，并保存 `last_non_off_capture_mode` 以支持重开。`autoNominate/growthDays` 保留兼容读写但不参与 v2 行为。`memory.settings.get/update` 使用 schema `oneOf`：legacy 分支继续用 `version/expectedVersion` SHA，R3 分支只用整数 `revision/expectedRevision`；同一请求不得混用。任一分支省略另一分支字段时必须在事务内保留现值，不能把缺失解码为零值覆盖。内部 rollout flags 与 R3 设置共用整数行 revision，但不进入公开 settings payload。

### 来源合同

`chat_run_stream.go` 的现有 messageID 是助手消息ID，不能继续用于 chat-user 来源。新捕获输入必须通过当前turn的持久user receipt得到用户消息；多条用户消息按receipt逐条处理，不能用最后一条助手ID代替。

```go
// 拟加入 internal/domain/m8core/memory_v2.go；内部合同，不是Bridge参数。
type MemorySource struct {
    SubjectID, ScopeKind, ScopeID string
    SessionID, UserMessageID string
    SourceRevision string // sha256，按下述规范计算；不是现有Message字段
    PartID string
    StartByte, EndByte int64 // UTF-8半开区间
    QuoteDigest string
}
```

SourceRevision = SHA256(UTF-8 canonical JSON)：字段固定顺序为sessionId/messageId/role/sequence/text/parts；text使用数据库已规范化且未裁剪的原文，parts按持久顺序，包含part ID、kind和内容digest；无parts使用空数组，不包含随机时间。quote digest直接取原文[start:end] bytes，不先去标点/trim。验证role=user、所属session与subject、scope可达、UTF-8边界、span原文和digest；不通过则`MEMORY_SOURCE_INVALID`。tool来源使用独立call receipt及实际verification字段，不冒充user。

`memory_evidence_spans.source_kind` 的完整枚举固定为 `user_message|tool_receipt|import_record|user_direct_entry`。`memory.item.create` 没有 `sourceRef` 时写 `source_kind=user_direct_entry`、`source_ref=operation:<operationId>`、`start_byte/end_byte=NULL`、`quote_digest=SHA256(提交正文 UTF-8 bytes)`；有合法 `sourceRef` 时才写 `user_message`。`explicit_ui` 只允许作为 operation receipt 的 `receipt_kind`，不得成为 evidence source kind，也不得伪造聊天消息来源。

历史confirmed记录：只允许沿turn关联唯一定位原user receipt并找到原始span后修复；无法唯一确定进入review，记录`legacy_source_unresolved`，不能用助手输出反推用户说过什么。

### 时序与背压

```text
用户消息持久化 → 原子写source outbox或启动补扫cursor
→ 回合执行 → 回合最终化：本地fast path最多20ms
→ 已提交事实计数进入completed.memoryCapture；否则留job
→ 后台worker读取真实source → policy/evidence/CAS → commit
```

20ms是超时退后台边界与p95目标，不是磁盘硬实时承诺。取消/失败回答不影响已持久化用户直接陈述的候选资格；off模式不创建working、candidate、后台提取或新索引，正常聊天日志仍按现有策略保存。manual只允许用户明确保存动作创建记忆，不自动生成待确认横幅。已有off/manual不改；新用户/未明确选择的历史默认策略在发布gate通过后auto。

capture job唯一键(subject,userMessageID,SourceRevision)。租约60s、心跳15s、claim增加attempt/fence；旧fence结果禁止commit。10000高水位停止普通job物化，保留source扫描游标，继续消费者排空。扫描每次≤100条；低水位8000恢复物化。明确删除/禁记同步执行，不排在提取队列后。

### 自动更新与使用门

- fact_key=(subject,scope_kind,scope_id,entity_key,predicate_key)，只能由白名单解析或人工审阅确定；语义相似不足以自动覆盖。
- 明确“以后改用/不再/现在改为”且唯一同键匹配→head CAS correction；其他冲突review。
- observation至少两条独立来源，review前零注入；接受后仍derived/evidence。
- imported_unverified先review；SHA256不是签名。禁止archive自报user_explicit。接受后user_approved_import/evidence。
- `memory_fact_supersessions`保存版本边与effective_at；旧metadata不改正文。历史查询按有效区间读取，不用current head覆盖全部历史。当前查询才应用current head；tombstone对两种查询都有效。

### 查询、撤销、遗忘合同

| 方法 | payload | 行为 |
|---|---|---|
| memory.item.get | factId,version?,asOf? | version/asOf互斥；反查授权；默认当前 |
| memory.item.history | factId,cursor?,limit? | limit1..100，按版本倒序，含无正文墓碑 |
| memory.item.create | scopeKind,scopeId?,operationId + 严格 oneOf：text-only 或 sourceRef-only；sourceRef 再严格 oneOf=`{messageId}`（整条持久化 user text）或 `{messageId,startByte,endByte}`（显式半开 UTF-8 byte span）；无 expectedRevision/kind/时间字段 → item,databaseRevision,undoOperationId,undoExpiresAt | manual/主动新增唯一 canonical 入口；sourceRef 分支禁止 text，fact body/evidence 都由 Engine 重读 span 唯一派生；text 分支禁止 sourceRef 并记 `user_direct_entry`；整条形式解析 `0..len(bytes)`，两偏移必须同时出现，非法来源统一 `MEMORY_SOURCE_INVALID`；Engine 注入 subject，本地确定性分类，歧义固定 kind=episode，valid_from=提交时间、expires_at=NULL；敏感过滤/授权/去重/幂等；undo=24h；off=`MEMORY_MODE_OFF`，scope=false=`MEMORY_SCOPE_DISABLED` |
| memory.item.forget | factId,targetVersion?,mode,expectedRevision,operationId | this_version必须targetVersion；fact_history禁targetVersion；subject_rule必须ruleCategory |
| memory.capture.undo | undoOperationId,operationId | 24h；任一项被后续修改则整批MEMORY_UNDO_CONFLICT；不做部分删除 |
| memory.purge.prepare | scopeKind,scopeId?,expectedDatabaseRevision,operationId | user禁scopeId、project必填；它会持久化一次性 grant，故属于 mutation 且顶层 idempotencyKey 必填；返回 counts,snapshotDigest,confirmationToken,expiresAt,operationId；同key变参拒绝；prepare 与最终 purge 使用不同 operationId |
| memory.purge | confirmationToken,snapshotDigest,expectedDatabaseRevision,operationId | mutation；使用与 prepare 不同的新顶层 idempotencyKey；原子消费未过期的一次性 grant 后清理并返回 deletion receipt；字段缺失、摘要/修订不匹配或仅有浏览器确认时拒绝 |

generation/import 六个方法的 exact schema 固定如下，字段名不得用 `id/revision/databaseVersion` 等别名；所有对象 `additionalProperties=false`，所有 cursor 为 `null` 或不透明 1..512 bytes，limit 默认 50、范围 1..100：

| 方法 | envelope / payload | exact result 与状态规则 |
|---|---|---|
| `memory.generation.list` | read；`scopeKind,scopeId?,cursor?,limit?`，scope strict oneOf | `{items:GenerationSummary[],nextCursor:null|string,databaseRevision:int>=1}`；按 `createdAt DESC,generationId ASC` 稳定分页 |
| `memory.generation.preview` | read；`generationId,cursor?,limit?` | `{generation:GenerationSummary,changes:GenerationChange[],nextCursor:null|string}`；Engine 由 generationId 反查 subject/scope |
| `memory.generation.activate` | mutation；顶层 idempotencyKey；`generationId,expectedRevision:int>=1,operationId` | `{generation:GenerationSummary,databaseRevision:int>=1,operationId}`；只允许 ready/archived，CAS 切 active；旧 active 原子变 archived |
| `memory.generation.discard` | mutation；顶层 idempotencyKey；`generationId,expectedRevision:int>=1,operationId` | 同上；只允许 building/ready/failed，先 fence worker 再变 discarded；active/archived 拒绝 |
| `memory.import.preview` | mutation；顶层 idempotencyKey；`sourceArtifactId,operationId` | `{previewId,sourceArtifactId,archiveDigest,manifestDigest,databaseRevision:int>=1,expiresAt,counts:ImportCounts,warnings:string[],operationId}`；只写 preview/lease，active fact 写入数为 0 |
| `memory.import.commit` | mutation；新的顶层 idempotencyKey；`previewId,archiveDigest,manifestDigest,expectedDatabaseRevision:int>=1,operationId` | `{previewId,state:"committed",importedCount,reviewCount,skippedCount,conflictCount,databaseRevision:int>=1,operationId}`；重读并校验原 artifact 后单事务提交 |

`GenerationSummary` exact fields 为 `{generationId,parentGenerationId:null|string,scopeKind:"user"|"project",scopeId:null|string,state:"building"|"ready"|"active"|"discarded"|"failed"|"archived",sourceCutoffSeq:int>=0,builderVersion:string,memberCount:int>=0,revision:int>=1,createdAt,readyAt:null|date-time,activatedAt:null|date-time,errorCode:null|string}`；user scopeId 必须 null，project 必须非空。`GenerationChange` exact fields 为 `{change:"add"|"remove"|"replace",factId,fromVersion:null|int>=1,toVersion:null|int>=1,beforeText:null|string,afterText:null|string,reasonCodes:string[]}`，文本来自已授权 canonical body 且各≤8192 UTF-8 bytes，已忘记正文固定 null。`ImportCounts` exact fields 为 `{total,accepted,review,conflicts,sensitive,tombstones}`，均为非负整数；warnings 最多 100 项、每项≤256 bytes。

generation 稳定错误为 `MEMORY_GENERATION_NOT_FOUND|MEMORY_GENERATION_NOT_READY|MEMORY_GENERATION_ACTIVE|MEMORY_GENERATION_STATE_INVALID|REVISION_CONFLICT`；import 稳定错误为 `MEMORY_IMPORT_SOURCE_MISSING|MEMORY_IMPORT_TOO_LARGE|MEMORY_IMPORT_SCHEMA_UNSUPPORTED|MEMORY_IMPORT_DIGEST_MISMATCH|MEMORY_IMPORT_PREVIEW_EXPIRED|MEMORY_IMPORT_SCOPE_DENIED|REVISION_CONFLICT`。同 idempotencyKey 同 request digest 重放首次 result，同 key 变参统一 `OPERATION_REPLAY_MISMATCH`。preview 是 mutation 因为会落 preview/lease；generation.preview 只是 read。关闭抽屉不发送 commit/cancel，未提交 preview 只由 24h expiry 回收。

新业务mutation需顶层idempotencyKey；targetVersion当前head的删除，恢复仍有效未忘记前版本，否则head为空。批次撤销保存/纠正时恢复该批前的head，但不复活任何墓碑。重复撤销返回首次receipt。

首发“忘记”明确是**删除记忆库及记忆派生数据**，不是删除聊天记录。同步scrub body、FTS shadow、vector、关系文本、candidate/extractor payload、独占legacy mirror与working拷贝，撤销generation引用、缓存；event/trace不存正文。原聊天、compaction/handoff/周归档可能仍包含原信息，UI必须明确显示“不会删除原聊天；当前聊天仍可能引用原文”。所有Memory API不得沿source暴露被忘记正文，也不得从被忘记source/span后台重新提取。

0160增`memory_source_suppressions(subject_id,source_id,source_revision,start_byte,end_byte,fact_id,reason,created_at)`；capturer/companion归档转记忆入口统一查抑制记录。它防重新写记忆，不声称从模型上下文抹除原聊天。若用户要求“删除聊天及摘要”，调用产品相应数据删除流程；当前没有完整覆盖时明确“不支持一键清除全部历史派生”，不能用记忆forget假成功。该更强功能不混入本期满分范围。

### 备份与导入导出

1. 在新迁移之前、旧 schema 可验证的阶段创建一致备份。当前 `Open/OpenSecure` 返回前已经 initialize/migrate，不能拿到 Store 后才备份；实现固定为新增 `OpenOptions.BeforeMigrate`，或同等 `openRaw→validate→backup→initialize` 拆分，在同一升级锁/连接窗口调用现有 backup primitive。备份失败则不迁移；受管目录使用唯一文件名，不覆盖原库。
2. 迁移后用同一read transaction固定snapshotRevision，流式导出所有section并生成digest/count。64MiB/100000行上限是portable archive上限，不是数据库大小上限；超限明确报EXPORT_TOO_LARGE，引导一致DB备份，禁止称截断结果为完整导出。
3. 0160增`memory_archive_artifacts(artifact_id,subject_id,scope_kind,scope_id,content_ref,sha256,size,state,expires_at,created_at)`和Memory专用CAS root；state=staging/sealed/expired，仅sealed可预演，默认24h保留。上传复用现有附件选择/授权后由Engine登记并流式封存，不接受任意本地路径；导出亦登记此表。读/删采用独立root、授权和读lease（另建memory_archive_leases，字段lease_id/artifact_id/owner/expires_at，60s）；preview引用未过期时不得GC。24h内commit再验digest/revision。
4. 本机已有tombstone/suppression优先于导入旧备份；外来scope不能自动扩权，外来subject需显示绑定当前身份的预演且仅review，不接受原ID直接作为授权。
5. `RestoreBackup`会关闭Store，调用方必须重新打开；恢复旧二进制及旧DB会舍弃升级后数据，必须用户明确选择。功能降级保留基础canonical reader/删除屏障。

### 记忆状态数据字典

下表是 0160–0162 所有关键 `state` 列的完整闭集；SQL `CHECK`、Go typed constants、Bridge 投影和测试必须逐字一致。`terminal` 只表示该记录不再自动推进，不代表其业务对象不可被新的 operation 重新创建。

| 实体 | 完整枚举 | terminal 集合与合法推进 |
|---|---|---|
| `memory_capture_jobs` / `memory_embedding_jobs` / `memory_consolidation_jobs` | `queued|running|deferred|succeeded|failed|cancelled` | terminal=`succeeded|failed|cancelled`；`queued→running`，`running→succeeded|failed|cancelled|deferred`，`deferred→queued|cancelled`；租约接管只增加 fence，不引入新状态 |
| `memory_budget_reservations` | `reserved|settled|released|expired` | 全部除 `reserved` 外均 terminal；只允许 `reserved→settled|released|expired` |
| `memory_migration_map` | `pending|migrated|review|failed` | terminal=`migrated|review|failed`；只允许 `pending→migrated|review|failed`，重试通过新 attempt/幂等更新实现，不把 terminal 行退回 pending |
| `memory_import_previews` | `staging|ready|committed|discarded|expired|failed` | terminal=`committed|discarded|expired|failed`；`staging→ready|failed|discarded|expired`，`ready→committed|discarded|expired|failed` |
| `memory_archive_artifacts` | `staging|sealed|expired` | terminal=`expired`；`staging→sealed|expired`，`sealed→expired`；只有 sealed 可被 preview/read lease 引用 |
| `memory_relations` | `active|invalidated` | terminal=`invalidated`；只允许 `active→invalidated` |
| `memory_embeddings` | `ready|invalidated` | terminal=`invalidated`；只允许 `ready→invalidated`，推理失败记在 embedding job，不写伪 ready 向量 |
| `memory_generations` | `building|ready|active|discarded|failed|archived` | terminal=`discarded|failed|archived`；`building→ready|failed|discarded`，`ready→active|discarded`，`active→archived`；同 subject/scope 至多一个 active |

## C2. 上下文权限与总成本

自由文本记忆不能成为system指令。working、episode、observation、工具结果和导入均以明确标识的低信任数据块注入。Core仅对白名单属性language/display_name/output_format由产品模板生成偏好；不得允许保存的“忽略审批/泄露密钥”等自由文本升权。最终provider messages需测试，而不只测试数据库字段。

`availableInput`固定采用contextapp.ProviderInfo.EffectiveInputBudget()在加入memory之前的值；记忆计数加入ContextEnvelope并从其余消息预算扣除一次，不能双扣或漏扣。总上限min(1536,floor(availableInput*0.08))，Companion另限512；每槽与序列化标签均计数。未知tokenizer乘1.15是预算估算，不保证实际服务商分词永不超；provider报超长时压缩一次并记录实际usage，不宣传exact。

remote modelAssist默认跟随用户已有“允许文本模型远端处理”策略；未选择/仅本地模式为off。启用auto不等于授权上传。query embedding也遵守相同策略与预算。

新增`memory_budget_days(subject_id,utc_day,limit_tokens,reserved_tokens,settled_tokens,revision)`及`memory_budget_reservations(job_id,attempt_id,maximum_tokens,state,actual_tokens)`。默认每主体UTC日32768预算tokens，用户可设0关闭远端辅助；统一覆盖extract/consolidate/embed/query_embed，额度不足deferred，仍保留fast path/FTS。每次调用前事务预占最坏输入+输出上限，所有重试单独预占；usage缺失不能按0结算，保留预占值。次日新账本，不回写旧记录。此上限是应用预算、非服务商账单绝对保证。

主回答与后台成本分别显示，同时报告端到端总量：answer input/output+extract+query/embed+consolidation+重试。缓存命中要记录，不可仅拿注入减少证明“省钱”。不同模型token不直接当相同货币；无可信价格不计算节省金额。

query embedding不得拖慢前台：总recall deadline150ms（含取query向量）、dense扫描子预算100ms；未拿到向量就FTS/time降级并记录原因。取消网络请求并单独记已产生usage；不得假称HTTP一定100ms返回。所有fallback同样查授权、hidden/tombstone/sensitivity，不能为了快退到不安全legacy缓存。

## C3. OCR：作用域与生产消费者

```go
// 拟加入 internal/ocrapp/request.go；OwnerSubjectID由Engine注入，不是公开payload。
type OCRScope struct {
    OwnerSubjectID string
    ScopeKind string
    ScopeID string
}
type ProviderBinding struct {
    ProviderID string
    ModelID string
    CredentialRef string // 仅Engine内部，不进入Bridge/result
}
type ResolvedOCRRequest struct {
    RequestID string
    Scope OCRScope
    SourceDigest string
    PolicyRevision string
    GateRevision int64
    Mode string
    Provider *ProviderBinding
    PackManifestDigest string
    PackRuntimeProfileDigest string
    PipelineKind string
    AllowRemote bool
}
type ScopedRoutingStore interface {
    Get(context.Context, OCRScope) (Routing, error)
    CompareAndSet(context.Context, OCRScope, string, Routing) (Routing, error)
}
```

公开 `OCRPolicy` 只允许以下字段，所有 schema 均须 `additionalProperties=false`：

```text
mode: auto|local_fast|local_document|provider_first
complexDocumentEngine: none|paddleocr-vl-1.6
providerId/modelId: 必须同时出现或同时省略；providerId=ULID，modelId 为 1..200 bytes
fallbackOrder: windows-ocr|paddleocr-vl-1.6|provider 的唯一数组，maxItems=3
sendToCloud: never|configured_only
```

条件合同固定为：`mode=provider_first` 或 fallback 含 `provider` 时 providerId/modelId 必填且 `sendToCloud=configured_only`；fallback 含 Paddle 时 `complexDocumentEngine=paddleocr-vl-1.6`；只有显式 `mode=local_document` 在保存时要求 Paddle ready。默认值逐字为 `{"mode":"auto","complexDocumentEngine":"paddleocr-vl-1.6","fallbackOrder":["windows-ocr"],"sendToCloud":"never"}`。schema 用 `oneOf` 表达 provider pair 与上述条件，不能只在 UI 校验。

公开 OCR scope selector 固定为严格 `oneOf`：user 分支 `{scopeKind:"user"}` 禁止 `scopeId`，project 分支 `{scopeKind:"project",scopeId:string(1..128 bytes)}` 必须 scopeId；两分支均 `additionalProperties=false` 并可按方法加入其余已列字段。Engine 从认证 identity 注入 `OwnerSubjectID`，user 的内部 `ScopeID=OwnerSubjectID`，project 先做授权；缺 scopeKind、user 带 scopeId、project 缺 scopeId 或越权均在读库/探测前拒绝。项目没有精确 policy row 时继承 user policy，但 legacy registration 不继承。

`ocr.routing.get {scopeKind,scopeId?,refreshProbe?}` result 固定为 `{requestedScope,policySource,policy,revision,windowsProbe,legacy}`；requestedScope/policySource 都是 `{scopeKind,scopeId:null|string}`，policySource 另有 `inherited:boolean`。`legacy` 必须是 `null` 或 `{engineId:"ppocr",registered:true,state:"registered_unwired",available:false,markerDetected:boolean}`，不得降级成 boolean。`ocr.routing.set` 的新 payload 固定为 `{scopeKind,scopeId?,policy,expectedRevision}`，路由 CAS 使用 SHA revision；继承态 revision 绑定请求 scope、来源 scope 和来源 policy revision，创建 project override 时在同一事务重检。顶层 envelope `idempotencyKey` 必填且本方法不接受 `operationId`。一个兼容发布周期内旧 decoder 可接受 `preferProvider/localEngine/packRoot`，但旧 payload 也只能作用于显式请求 scope，result 永远使用上述新形状。

`ocr_legacy_registrations` 固定字段为 `(registration_id,owner_subject_id,scope_kind,scope_id,engine_id,root_ref,marker_detected,state,available,imported_at,revision)`，并强制 `UNIQUE(owner_subject_id,scope_kind,scope_id,engine_id)`；`engine_id=ppocr`、`state=registered_unwired`、`available=0`。未能确定 owner/scope 的旧文件不落库，只保留并提示重新选择。

ResolvedOCRRequest在入口授权后生成一次；provider callback直接消费该快照，禁止ocrProviderCall中再次Routing()取全局绑定。身份切换后旧任务可取消，不能改用新主体凭据继续。旧ocr-routing.json只迁给经确认的本地原主体，无可确定主体则保留文件、禁用绑定并显示“需重新选择”，不得复制给所有组织。

该快照必须可审计而不泄露凭据：`ocr_document_runs` 持久化 `request_id/owner_subject_id/scope_kind/scope_id/document_digest/policy_revision/gate_revision/mode/provider_id/provider_model_id/pack_manifest_digest/pack_runtime_profile_digest/pipeline_kind/allow_remote/request_snapshot_digest`，其中 provider 两字段同空或同非空，绝不写 `CredentialRef`。`request_snapshot_digest` 是这些字段按固定字段顺序做 UTF-8 canonical JSON 后的 SHA-256，并与 `(run_id,request_snapshot_digest)` 建唯一键。每个 `ocr_page_results` 必须保存同一 `request_snapshot_digest` 外键，另记实际 `engine_id/engine_version/actual_pack_manifest_digest/pipeline_kind/source_digest/warnings`；中途 fallback 只能改变实际执行证据，不能改请求快照。

当前 `localEngine=ppocr/packRoot` 只表示 legacy 目录登记。即使探测到 marker，也必须返回 `registered_unwired`、`available=false`，识别调用数为0；旧值 effective engine 为 Windows 并带 warning。旧字段兼容解码一个发布周期，新客户端不得写 `ppocr`；Renderer 不得得到 `packRoot` 绝对路径。该登记绝不迁为 PaddleOCR-VL installed/ready。

必须同步适配以下入口，不只新增RecognizeDocumentV2：RecognizeDocument、RecognizePDF、RecognizeImage及仓库ReadDocument调用点；Office `office_source_text.go`、模型目录`model_catalog.go`、KB/工作区读文档入口全部传可信scope。旧签名可留compat adapter，但缺作用域只能返回OCR_SCOPE_REQUIRED，不静默借全局身份。

Office OCR缓存键=(subject,scope kind/id,source SHA,policy SHA,gate revision,resolved pipeline,pack manifest digest,provider/model)。变更任一项不能命中旧结果。legacy Text上限1MiB；超过时返回有界摘要并Complete=false+结果artifact提示，不把截断识别当完整。结构化结果不得倒灌为精确原文证据。

### 逐页渲染

新增`internal/doctext/pdf_render_stream_windows.go`及other stub，提供以下内部接口：

```go
type RenderedPageRef struct {
    Page, Width, Height int
    InternalPath, SHA256 string // 只供Engine/worker，不进入Bridge
    Size int64
}
type PDFRenderLimits struct { MaxPages, MaxPixels int; MaxPageBytes, MaxTempBytes int64 }
// callback同步消费；成功后释放当页临时资源，再渲染下一页。
func RenderPDFPagesToTaskDir(ctx context.Context, sourcePath, taskDir string,
    limits PDFRenderLimits, consume func(RenderedPageRef) error) error
```

输入必须是Engine授权并复制/封存到私有taskDir的文件。≤100页、20MP/页、PNG≤32MiB/页、临时目录≤512MiB；同一时刻≤1页渲染+1页识别，逐页stat/尺寸检查在读取/分配前，禁全部PNG累积内存。taskDir监测配额不是内核磁盘沙箱。页失败保留已完成页；cancel终止渲染子进程与worker，清理无lease临时文件。

## C4. OCR：包状态、租约、结果文件

- Windows probe：`ready|unsupported_os|initialization_failed|language_unavailable|sample_failed|timed_out`；这是完整闭集，仅 `ready` 对应 `available=true`。
- Pack availability：`not_installed|ready|quarantined`；更新中的旧current仍ready。pack version state：`verified|quarantined`，其中 quarantined 为 terminal，不允许重新激活相同 digest。
- Install/uninstall operation：`requested|preflighting|downloading|verifying|installing|self_testing|succeeded|failed|cancelled`；terminal=`succeeded|failed|cancelled`，pack ready不是operation phase。uninstall 不经过 downloading，但复用同一闭集并只走实际步骤。
- Document run：`requested|running|succeeded|partial|failed|cancelled`；terminal=`succeeded|partial|failed|cancelled`，仅 `requested→running|cancelled`、`running→succeeded|partial|failed|cancelled`。
- Legacy registration：state 只允许 `registered_unwired` 且 `available=false`，不存在 terminal 转换或 ready 分支。
- requested/preflighting/downloading可设置cancel_requested；verifying及原子激活不可取消，返回OCR_OPERATION_NOT_CANCELLABLE。卸载请求等运行lease释放，30s后仍占用则failed/OCR_PACK_BUSY，保留current，不强删正在使用DLL。
- 0163 operation新增lease_owner/lease_until/heartbeat_at/attempt/fence；CAS claim租约60s心跳15s，每次接管fence递增。激活事务必须同fence且目标digest一致；旧worker/runner结果拒绝。
- 新`ocr_version_leases(lease_id,pack_id,version,run_id,owner,fence,expires_at)`保护加载版本；重启先终止/确认孤儿进程再回收旧lease。
- `ocr_artifact_leases(lease_id,artifact_id,owner,expires_at)`保护正在读的结果，60s租约。独立OCR CAS root不与全局workspace CAS做跨域去重/GC；同OCR digest可复用，查询ocr_artifacts有效引用和lease均为0才可删。
- `ocr_pack_notices(pack_id,manifest_digest,version,notice_digest,notice_bytes,notice_ref,verified_at,installed_at,uninstalled_at)` 以 `(pack_id,manifest_digest)` 为主键，只收录签名 manifest 与 NOTICE bytes 均验证通过的记录。NOTICE 使用独立受控 CAS ref；卸载和 catalog 换版不删除记录/blob，只更新安装生命周期时间。
- 没有签名 catalog 和已验证 runtime profile 时，`ocr.pack.get` 返回 `gate.installAllowed=false, gate.autoRouteAllowed=false, gate.reasonCode=NO_VERIFIED_RUNTIME_PROFILE`；`ocr.pack.install` 返回稳定拒绝且不得创建 operation。legacy PP-OCR marker 不能满足此 gate。
- Windows 健康由 15 秒内真实 WinRT 初始化、语言枚举与固定小图识别得出 `ready|unsupported_os|initialization_failed|language_unavailable|sample_failed|timed_out`，仅 `ready` 的 `available=true`，缓存5分钟；`runtime.GOOS` 只能决定是否尝试 probe，不能决定 ready。UI 分别映射为“可用 / 暂不支持 / 初始化失败 / 需要语言包 / 自检失败 / 检查超时”。

`ocr.pack.get {packId}` 是唯一包快照方法，result 固定为四段；所有对象（含 preflight/licenseSummary）均 `additionalProperties=false`，字段不得省略，可空字段显式为 JSON `null`：

```text
pack:{packId:string,availability:not_installed|ready|quarantined,currentVersion:string|null,previousVersion:string|null,manifestDigest:string|null,engineVersion:string|null,deviceKind:cpu|gpu|null,lastHealthAt:date-time|null,lastErrorCode:string|null,revision:int>=1}
gate:{installAllowed:boolean,autoRouteAllowed:boolean,reasonCode:string|null,verifiedRuntimeProfileDigest:string|null,revision:int>=1}
operation:null|{operationId:string,action:install|uninstall,phase:requested|preflighting|downloading|verifying|installing|self_testing|succeeded|failed|cancelled,completedBytes:int>=0,totalBytes:int>=0,cancelRequested:boolean,terminal:boolean,cancelAllowed:boolean,errorCode:string|null,retryable:boolean,revision:int>=1,createdAt:date-time,updatedAt:date-time}
release:null|{version:string,catalogRevision:string,manifestDigest:string,runtimeProfileDigest:string,compressedBytes:int>=0,expandedBytes:int>=0,deviceProfile:cpu|gpu,preflight:{state:not_run|compatible|incompatible,reasonCode:string|null,requiredDiskBytes:int>=0,availableDiskBytes:null|int>=0,deviceKind:cpu|gpu},licenseSummary:{count:int>=0,spdxIds:string[],nonSpdxLicenseIds:string[]},noticeDigest:string,noticeBytes:int>=0}
```

`terminal` 由 phase 是否属于 `succeeded|failed|cancelled` 唯一派生；`cancelAllowed` 仅在 phase 属于 `requested|preflighting|downloading` 且 `cancelRequested=false` 时为 true，两者不得作为独立数据库真相。preflight schema 使用 `oneOf`：`not_run` 固定 `reasonCode=null,availableDiskBytes=null`；`compatible` 固定 `reasonCode=null,availableDiskBytes=int>=0`；`incompatible` 要求 `reasonCode=1..128 bytes,availableDiskBytes=int>=0`；`deviceKind===release.deviceProfile`。`licenseSummary.spdxIds/nonSpdxLicenseIds` 均 `uniqueItems=true,maxItems=256`，每项 1..128 bytes，count 等于两数组总数。`pack.revision` 只用于 install/uninstall 的 payload `expectedRevision`；cancel 比较目标 `operation.revision`；`gate.revision` 仅用于诊断，handler 每次仍原子重检 gate。四个 OCR mutation 逐方法冻结，禁止用“所有 mutation 都带 operationId/expectedRevision”生成第二套 DTO：

| 方法 | 顶层 envelope | payload | 接受结果/拒绝边界 |
|---|---|---|---|
| `ocr.routing.set` | `idempotencyKey` 必填 | `scopeKind,scopeId?,policy,expectedRevision`（SHA；scope 严格 oneOf） | 返回含 requestedScope/policySource 的新 routing snapshot；无 `operationId` |
| `ocr.pack.install` | `idempotencyKey` 必填 | `packId,catalogRevision,acceptedManifestDigest,expectedRevision,operationId` | `{requestId,operationId,activityId,accepted:true,packId,phase:"requested",packRevision,operationRevision}`；`activityId===operationId` |
| `ocr.pack.cancel` | `idempotencyKey` 必填 | `operationId,expectedRevision`（operation integer） | 不新建 operation；`{requestId,operationId,activityId,cancelRequested,phase,operationRevision}`；`activityId===operationId` |
| `ocr.pack.uninstall` | `idempotencyKey` 必填 | `packId,expectedRevision,confirmed,operationId` | 与 install 同形 acceptance receipt；`confirmed` 必须为 true |

`ocr.pack.notice.list {packId,cursor?,limit?}` 与 `ocr.pack.notice.read {packId,manifestDigest,offset,limit}` 都是只读有界方法，不要求 idempotencyKey/operationId/expectedRevision。list 的 limit 默认20、范围1..50，按 `verified_at DESC,manifest_digest ASC` 稳定分页，返回 `{items,nextCursor}`；item exact shape 为 `{packId,manifestDigest,version,noticeDigest,noticeBytes,verifiedAt,installedAt:null|date-time,uninstalledAt:null|date-time}`，无路径。read 要求 `offset>=0`、`1<=limit<=65536`，只读取 list 可发现的本机 retained trusted NOTICE，result 固定为 `{base64,nextOffset,eof,sha256,totalBytes}`；不接受本地路径、URL 或未登记 digest。`release=null` 但 retained row/blob 完整时仍须离线成功；只有无受信记录、blob 缺失/损坏或 digest 不一致时返回非重试错误 `OCR_PACK_NOTICE_UNAVAILABLE`，数据库、文件和网络 mutation=0。run/list/artifact 同样是只读方法。

新`internal/ocrapp/artifact_store.go`实现受控OpenRange/Verify/DeleteUnreferenced，内部只接受校验后的digest不接受path。返回流式io.ReadCloser；单次原始bytes≤65536，metadata摘要≤4KiB/项，run.get最多20页且总JSON≤512KiB。`ocr.artifact.read`返回base64,nextOffset,eof,sha256,totalBytes；客户端拼接完再UTF-8解码，不逐块破坏汉字。缺失OCR_ARTIFACT_MISSING、损坏OCR_ARTIFACT_CORRUPT，不返回半个结构JSON当成功。

安装包requirements.lock锁所有Python wheel/hash；发布manifest包含它的digest，完整CPU profile还记录CPython ABI、CPU指令集、Windows最低版本、DLL清单及每个依赖许可证。真实组合由W0构建报告确定；没有报告就是profile disabled，不臆造一个版本号称已支持。

## C5. 媒体：Host、epoch、生命周期与音频焦点

媒体持久状态的完整闭集固定如下，SQL/Go/Bridge/React 不得另造同义值：

| 实体 | 完整枚举 | terminal 集合/约束 |
|---|---|---|
| `media_assets.state` | `ready|missing|changed|revoked` | terminal=`changed|revoked`；`missing` 在同一文件 identity 重新可达后可回 `ready`，内容 fingerprint 改变必须新建 asset 或进入 changed |
| `media_sessions.phase` | `idle|playing|paused|stalled|ended|uncertain|failed|stopped` | `stopped` 为 release 后 terminal；`ended/uncertain/failed` 是可由新 operation 恢复的静止态，不得映射成 operation 成功 |
| `media_operations.phase` | `requested|awaiting_approval|dispatching|verifying|succeeded|uncertain|failed|cancelled` | terminal=`succeeded|uncertain|failed|cancelled` |
| `media_queue_items.state` | `queued|current|played|removed|failed` | terminal=`played|removed|failed`；每 session 至多一个 current，queue item 只引用 `asset_id` |
| `media_player_commands.state` | `pending|claimed|acknowledged|expired|cancelled|failed` | terminal=`acknowledged|expired|cancelled|failed`；claim/ack 必须匹配 lease generation、playbackEpoch 与 fence |

核验必须拆成两个字段。`media_sessions.verification_status` 的完整闭集为 `none|command_dispatched|verified_playing|verified_paused|verified_ended|verified_stopped`，`media_sessions.verification_source` 只允许 `none|smtc|owned_runtime`。`media_operations.verification_status` 与通用 OperationReceipt/Activity 使用 `not_applicable|not_started|pending|confirmed|unconfirmed`，`verification_source` 使用 `none|process|window|uia|smtc|owned_runtime|artifact`。不得再出现含义不明的单字段 `verification`。`command_dispatched` 只属于 session verification_status；operation receipt 派发后为 phase=verifying/status=pending/source=none，证据到达才 confirmed+source，超时则 phase=uncertain/status=unconfirmed。

`media_player_leases` 的数据库字段固定为 `lease_token_digest`；Bridge/Host 临时值叫 `leaseToken`，不得再出现 `nonce_digest` 或 `token_digest` 同义列。公开方法只包含媒体业务 intent/query；`internal.media.player.attach`、`internal.media.player.next`、`internal.media.player.report` 及 lease/command claim/ack 均为 Host-private internal RPC，不进入 Renderer envelope method enum/schema，Renderer 不得直接调用，也不得把 `windowInstanceId/navigationEpoch/leaseToken` 放进公共 payload。

MediaScope selector 与 C3 的公开 OCR scope 判别同形：`{scopeKind:"user"}` 禁止 scopeId，`{scopeKind:"project",scopeId:string(1..128 bytes)}` 必须 scopeId，scopeKind 永远必填。Engine 从认证连接派生 subject；user 的内部 scope_id=subject，project 先授权；公开 DTO 对 user 返回 `scopeId:null`。`media.session.list/watch/create`、`media.asset.pick/list`、`media.operation.list` 与 `activity.list` 都使用该 selector；get-by-ID 方法只接收对象 ID 并由 Engine 反查授权。`media.asset.pick` 虽是 `x-owner=host`，scope selector 仍是不可信业务输入：Host 仅连同 window identity 和私有 file handle/path 传给 `internal.media.asset.register`，最终 subject 派生和 project 授权必须在 Engine 完成。缺 scopeKind、user 带 scopeId、project 缺 scopeId、asset/session scope 不匹配均在副作用前拒绝。

`media.asset.list` 的可选过滤名固定为 `sourceKind`，枚举 `user_selected|artifact|workspace`，逐字对应 `media_assets.source_kind`；不得使用属于 session 的 `origin=owned|external` 过滤 asset。

上述三个 `internal.media.player.*` 方法只由 Host 调用；Host 向既有私有 `internalRuntimeHandlers` 发送带 Host 生成 `windowInstanceId/navigationEpoch` 的请求，这些字段不能来自 Renderer payload。内部 RPC 不加入公共 envelope，也不使用公开 schema 的 `x-owner` 机制。window销毁、logout、navigation epoch变更撤销lease/ticket。无需新增公开监听端口。

每个session有lease generation；每次实际装载/重播另增playbackEpoch。report必带assetId+playbackEpoch+eventSeq；命令回执再带operationId，自然position/ended不要求虚构操作ID。旧epoch事件直接忽略，不能更新新轨道状态。ended幂等键(session,epoch)只生成一次下一首操作。业务command接受后player.next取绝对目标，重放不能再次计算“下一首”。

新增`media.operation.get/list`返回历史操作与parent/rootOperationId；list按scope+cursor分页。工具调用已映射media operation时Activity仅展示media主记录，保留关联tool ID，不重复计数。

App共同根只持久挂载无视觉的`MediaStore`与`OwnedMediaPlayer`（或承担同等职责的runtime/provider），不得在根节点常驻完整播放UI。`MediaCenter`与条件式`MiniPlayer`订阅同一个只读`PlaybackSnapshot`，命令统一回到store/player；两套UI不得各建audio/video元素、队列或进度真相。实施选择固定为`web/src/App.tsx`的三个route返回共享同一无变化key的runtime层，不能在key变化的`SessionPage`中挂载。页面导航持续播放；整页刷新/Engine重启只能恢复暂停快照，不能自动出声。0164增加media_audio_focus singleton表，字段见02/05；CAS约束应用至多一个owned audible session，切换前暂停旧目标并核验，失败则新目标不启动。

`MiniPlayer` 不持久化第二份播放状态；UI 派生 `miniPlayerPhase` 的完整闭集固定为 `hidden|active|closing|close_error`，避免与后端 `media_sessions.phase` 混名。当前位于 `MediaCenter`、无 current asset，或 session `phase` 不是 `playing|paused` 时 `miniPlayerPhase=hidden`；离开媒体中心且 session `phase=playing|paused` 时为 `active`，所以 pause 仍保留进度/队列与 MiniPlayer。用户 close 后，在 stop/release operation 非终态期间为 `closing`；该 stop operation 到 `failed|uncertain` 时为 `close_error` 并保留原 MiniPlayer、文字错误与重试/强制结束入口，即使 session snapshot 已暂时进入非 playing/paused；只有 stop operation `succeeded` 且 session `phase=stopped` 后转 `hidden`。`ended/stalled/failed/uncertain` 的普通会话（不是未解决的 close operation）不单独弹出 MiniPlayer，恢复入口在媒体中心/Activity。媒体中心返回时复用同一 snapshot 和焦点位置，不重新装载资产或重复发送 play。

| 发起场景 | 权限/控制 |
|---|---|
| 用户点击本地文件播放 | Host选择授权+媒体scope检查；不需要打开电脑控制，不触发额外模型审批 |
| Agent播放用户已授权owned资产 | ToolRuntime共用治理/审计；不得自行扫盘；派发前再查撤销/急停 |
| external播放器控制 | 原computer capability/审批/桌面串行/急停全部保留 |
| owned队列自动下一首 | 首次用户同意队列autoAdvance默认true；仅该队列已授权资产；急停/权限撤销即停止，无隐式下载 |

音频焦点首版取保守策略，不新增语音模型：owned播放遇TTS或麦克风开始，先暂停并等真实pause回执（500ms超时即不开始麦克风，提示手动暂停）；TTS/录音结束后仅在focus token仍有效、资产/epoch未变、用户未手动操作、原本playing时恢复。取消、切设备、退出、急停不自动恢复。麦克风与TTS沿现有互斥/打断策略。

external无可靠暂停能力时提示“请暂停外部媒体或使用耳机”；首版不宣称AEC可以过滤电影对白，不能为语音而绕过审批发媒体键。屏幕/loopback音频不是用户语音来源，不自动进入记忆。

SMTC现DTO没有capabilities/timeline/sessionKey。首版仅开放经实际枚举且唯一匹配的play/pause/stop/next/previous；seek/volume在未增加并验证底层API前禁用。新增字段同时改`media_session.go`、Windows脚本/Go及other stub；多session同app时uncertain，不猜目标。

## C6. 顶部活动状态、视觉与可用性

新增 `activity.list {scopeKind,scopeId?,domains?,cursor?,limit?}`；scope 采用 C5 strict oneOf，domains 只能省略或是 unique array，元素枚举 `tool|ocr|media`、minItems=1、maxItems=3，省略表示全部，空数组/未知值拒绝。limit 默认50、范围1..100，cursor 最长512 bytes。exact response 为 `{items:ActivitySnapshot[],nextCursor:null|string,snapshotAt:date-time,hasMore:boolean}`，四字段全 required，`hasMore === (nextCursor !== null)`。保留旧operation.list DTO，不偷偷加必填字段。后端在同一read tx查询已有continuity operation journal、OCR operations/runs和media_operations，不建第四张活动真相表。

首版分页定义为“创建历史”：按(created_at DESC,domain,id) keyset，cursor绑定subject/scope/domains/snapshotAt和签名；后续页必须复用同一 selector/filter，只查created_at≤snapshotAt，显示这些条目的最新状态，不承诺跨请求repeatable-read状态快照。前端按domain/id更新去重；刷新重置cursor获得新创建条目。变化中的updated_at不作为翻页键，避免漏行。缺失历史时间返回null/“未知”，不以当前时间冒充。

`ActivitySnapshot` 所有字段 required，exact shape 为 `{activityId:string,domain:"tool"|"ocr"|"media",kind:string,phase:"queued"|"awaiting_approval"|"running"|"verifying"|"succeeded"|"uncertain"|"failed"|"cancelled",terminal:boolean,verificationStatus:"not_applicable"|"not_started"|"pending"|"confirmed"|"unconfirmed",verificationSource:"none"|"process"|"window"|"uia"|"smtc"|"owned_runtime"|"artifact",title:string,completedUnits:null|int,totalUnits:null|int,errorCode:null|string,retryable:boolean,recoveryAction:"none"|"retry"|"cancel"|"open_settings"|"open_player"|"select_asset",scopeKind:"user"|"project",scopeId:null|string,createdAt:null|date-time,updatedAt:null|date-time,rootOperationId:null|string}`。terminal 仅由 normalized phase 的 `succeeded|uncertain|failed|cancelled` 派生；进度两字段同为 null 或满足 `0<=completed<=total` 且 total>0；confirmed 必须有非 none source，unconfirmed 可以 source=none。user scopeId 必须 null、project 必须 1..128 bytes；subject由Engine过滤，UI不据此自行授权。异步 mutation 的 activityId 固定逐字等于 accepted operationId，列表稳定键用 `(domain,activityId)`；OCR run/工具 journal 等非 mutation 投影使用各自稳定源 ID。源状态到 normalized phase/status/source 的映射表与 adapter tests 属于 X3；安装只显示本地主体可见操作。media源优先，关联同rootOperationId的tool不重复列项。

视觉使用现有`web/src/styles.css`变量--bg/--bg2/--ink/--muted/--rule/--tide1/--ok/--warn/--err与themeStore，新增局部CSS不建立第二套主题。默认主内容背景保持产品现有纯黑，侧栏只做接近黑色的轻分层；薄荷绿—蓝灰—电光紫极光仅用于首页与媒体中心主视觉，不能铺到记忆/OCR设置卡或每个选中态。普通卡片仅比背景轻微提亮，依赖留白、字号和少量边框建立层级，不用大面积深蓝灰模拟另一套后台主题。组件间距8/12/16/24px，按钮最小32px，主要播放按钮40px，焦点环2px。

布局断点合同：≥960px显示完整侧栏、媒体中心主控制与右下MiniPlayer；680–959px压缩侧栏并折叠媒体次要信息；<680px使用单列内容，队列/活动详情全宽呈现，MiniPlayer不得遮挡聊天输入、OCR主按钮或系统导航。必须覆盖390×844、1024×768、1440×900及100%/150%/200%缩放，无横向滚动。长标题两行省略但可聚焦查看全文，错误必须文字+图标，不能仅颜色；`prefers-reduced-motion`下极光与进度装饰停止或显著减弱。

```text
聊天内容 / 办公内容（原布局）
└─ 轻提示：已记住 1 条 · 撤销 · 查看（不抢焦点）
办公 → 媒体中心（与办公工作台同级）：完整音乐/视频主视觉与播放控制
非媒体页 + 活动会话 → 右下 MediaMiniPlayer：封面 标题 | 只读进度 | 播放/暂停 | 关闭
顶部状态按钮 → 活动popover/详情；活动不占左侧主导航
设置 → 智能能力 overview：恰好“自动记忆”“文字识别”两张同宽卡
智能能力 → 记忆：状态 + 搜索 + 单列记忆；设置抽屉承载三态和两个scope开关
智能能力 → OCR：文字识别·自动（只读状态）+ 复杂文档增强唯一主操作；高级信息默认折叠
```

设置 category id 保持 `personal`，显示名改“智能能力”；内部详情状态固定 `overview|memory|ocr`。普通设置入口进入 overview，现有聊天 `onOpenMemory` 直达 memory。overview 不挂载项目、事实列表、provider 或 OCR 请求。OCR 从 routing 页移除，routing 只保留 CapabilityRouting。记忆页面不暴露召回阈值、Token、去重、时间整理等内部参数；三态/双scope放现有 Dialog 型设置抽屉。来源、纠正、忘记收进单条更多菜单；无冲突时不显示待审阅模块。身份失败 fail closed，不回退 `local-user`；个人设置不依赖项目存在。

OCR首屏默认文案为“文字识别 · 自动”，这是只读路由摘要，不是开关、也不等于 `preferProvider`。核心 `ocr.routing.get` 与 `ocr.pack.get` 独立加载；`provider.list` 只在高级区展开时加载，失败不遮蔽本地状态。Windows 显示真实 probe 状态；Paddle 未过 W0 时按钮以 `NO_VERIFIED_RUNTIME_PROFILE` 禁用且 mutation=0。语言、provider、legacy PP-OCR、pipeline、版本、缓存和回退收进高级区。早期原型曾有截图/选文件探索；现行 09 Demo 与本期正式产品均没有 OCR 工作台。

Canonical 组件名冻结为：`SmartCapabilitiesPanel`、`MemoryPage`、`MemoryStatusHeader`、`MemoryDrawer`、`MemorySettingsPanel`、`MemoryList`、`MemoryItemMenu`、`MemoryAdvancedPanel`、`OCRSettingsPanel`、`OCRModelPackCard`、`OCRAdvancedPanel`、`MediaCenterPage`、`MusicPlayerSurface`、`VideoPlayerSurface`、`MediaMiniPlayer`、`MediaQueueDrawer`、`MediaOperationCard`、`ActivityStatusButton`、`ActivityCenter`。`MemoryDrawer` 是记忆设置、item 详情和高级管理唯一抽屉容器；`MemorySettingsPanel` 只是其 settings 内容。现有 `MemoryOpsPanel`、`OCRRouting` 在迁移测试覆盖后删除，不得与 canonical 组件并存；03–05 出现旧名必须在本轮文档先机械更新。

顶部状态按钮显示进行中/需处理数量并打开活动popover或详情；侧栏不得新增“活动中心”主导航项。队列、活动详情等浮层遵循互斥规则，`Esc`关闭并回到触发按钮。axe serious/critical=0加真实WebView2键盘/读屏，不能把jsdom检查等同所有视觉通过。

## X0. 基线、合同与迁移前备份（先于03 Task1）

**Files:** 修改`internal/storage/sqlite/store.go`、`backup.go`的调用装配；新增`memory_upgrade_backup_test.go`；新增 `docs/design/jiyishengji/evidence/migration-allocation.json` 与 `evidence/baseline/<timestamp>/manifest.json`；评测 manifest 位于各 testdata 子目录。

**Consumes:** 当前最大 migration、raw/open 初始化顺序、backup primitive。**Produces:** 冻结 migration allocation、预迁移备份 hook、带 HEAD/trackedDiffDigest/untrackedManifestDigest 的只读验证报告、冻结数据集 digest。

- [ ] 写`TestMemoryPreMigrationBackupContains0159`：打开旧fixture，写唯一canary，升级前生成备份；升级后读取备份schema仍0159且含canary；中途失败不能破坏原库。
- [ ] 执行`go test -count=1 ./internal/storage/sqlite -run 'TestMemoryPreMigrationBackup|TestBackup'`，确认新业务断言失败而非编译错误。
- [ ] 扫描 migration 文件/embed/manifest/已发布 fixture，一次性写 allocation artifact 并由测试校验连续前置关系；记录 `git rev-parse HEAD` 与 `git diff --binary | git hash-object --stdin` 为 `trackedDiffDigest`，另记录 `untrackedManifestRelativePath/untrackedManifestDigest/baselineManifestRelativePath`。`worktreeDiffDigest` 是废弃别名，不得再写入新 artifact；报告不包含 dirty 文件正文。
- [ ] 使用已有 backup primitive 而不是另写裸文件复制；为 Open 增加 `BeforeMigrate` 或同等 raw 阶段，验证相同升级锁下无写入窗口。source 解析、三态/双scope、PP-OCR legacy、UI route/组件名进入合同正反例。
- [ ] 同命令绿测；`git diff --check`只检查本任务修改；提交`test(upgrade): freeze baselines and protect pre-migration database`。

## X1. 记忆源与上下文接线（03 Task2/4/5 同批）

**Files:** `internal/app/chat_run_stream.go`、`chat_memory.go`、`chat_memory_workers.go`、`data_scope.go`、`bootstrap/wire.go`、`contextapp/assemble_envelope.go`、`internal/storage/sqlite/m8_memory_v2.go`；新增同目录测试。

**Consumes:** MemorySource、scoped repository、source suppressions。**Produces:** 真实user evidence、终态轻提示、低信任注入、已授权历史/undo合同。

- [ ] 将07中的 M-R01～M-R15 编为表驱动 fixture，断言数据库与最终 provider messages，不仅 service mock；M-R13 必须逐一覆盖 auto/manual/off 与个人/项目两范围的全部生产路径，M-R14/15 固化 legacy omission 与显式保存。
- [ ] 执行`go test -count=1 ./internal/app ./internal/m8app ./internal/contextapp -run 'TestMemorySource|TestMemoryPrompt|TestMemoryUndo|TestMemoryQueue'`确认红测。
- [ ] 按C1/C2接线，默认policy不可用于生产v2；扩展data_scope的新ID解析；claim fence/每日预算同事务验证。
- [ ] 运行同命令及03对应包回归，确认off/manual和原chat归档边界；提交`feat(memory): bind sources scopes and bounded context safely`。

## X2. OCR消费者、页渲染与租约（04 Task1/3/4/7 同批）

**Files:** `internal/ocrapp/request.go`、`artifact_store.go`、`route.go`、`recognize.go`、`health.go`、`pack.go`；`internal/doctext/ocr_probe_windows.go`/other、`pdf_render_stream_windows.go`/other；`internal/app/ocr_wire.go`、`office_source_text.go`、`model_catalog.go`及实际KB/workspace调用点；OCR logical migration。

**Consumes:** ResolvedOCRRequest、ScopedRoutingStore、taskDir页引用。**Produces:** 所有入口scoped pipeline、512KiB响应、受租约保护的独立OCR结果库。

- [ ] 建 O-R01～O-R12 红测：含百页 bounded render、真实 Windows probe 状态、legacy marker 永不执行、provider 不二次读 routing、provider UI 故障隔离、无 runtime profile 安装拒绝。
- [ ] 执行`go test -count=1 ./internal/ocrapp ./internal/doctext ./internal/app -run 'TestOCRScope|TestOCRSnapshot|TestOCRArtifact|TestPDFRenderBounded|TestOfficeOCRCache'`。
- [ ] 按C3/C4实现，结果被请求取消或fence过期时拒绝提交；旧Result.Text超限明确partial；不得为修测试改全局IPC上限。
- [ ] 同命令绿测、真实pack门另外记录；提交`feat(ocr): integrate scoped bounded document recognition`。

## X3. App媒体焦点与活动分页（05 Task5/6/7 同批）

**Files:** `web/src/App.tsx`、App Shell/顶部状态入口、`web/src/media/MediaStore.ts`（或等价现有runtime）、`MediaCenterPage.tsx`、`MediaMiniPlayer.tsx`、`MediaQueueDrawer.tsx`、`web/src/activity/ActivityStatusButton.tsx`、`ActivityCenter.tsx`、`audioFocus.ts`、`web/src/session/companion/ttsPlayer.ts`、`speech.ts`；`internal/app/activity_handlers.go`、`media_private_handlers.go`；`internal/storage/sqlite/activity_query.go`、`continuity.go`；相关schema、Host gateway、SMTC DTO/script。

**Consumes:** C5事件epoch与Host身份，C6活动查询。**Produces:** 根级单一player/store、MediaCenter与条件式MiniPlayer共享snapshot、单一owned发声、跨页面连续播放、TTS/ASR协调、顶部可翻页真实活动历史。

- [ ] 建 A-R01～A-R12、V-R01～V-R04 及 U-R01～U-R15 红测，先用可控 fake 时钟/媒体事件，再实机。
- [ ] 执行`npm --prefix web test -- src/media src/activity src/session/companion`及`go test -count=1 ./internal/app ./internal/hostbridge ./internal/winexec -run 'TestMedia|TestActivity'`，逐条检查退出码。
- [ ] 先落Host/epoch、后App根级player/store与共享snapshot、再MediaCenter/条件式MiniPlayer、音频焦点及activity.list，按C5/C6实现；UI不能自己授予成功状态。
- [ ] 正式生成Bridge，测试旧operation.list仍可解码；Windows走真实合法视频seek/焦点/导航；提交`feat(media): preserve playback across routes and coordinate audio focus`。

## 关键测试代码范例（需随上述任务新增）

以下测试可直接作为03 Task5新增函数memoryTotalBudget的同包测试；该函数已在03定义，测试不依赖虚构fixture：

```go
func TestMemoryTotalBudgetR2(t *testing.T) {
    cases := []struct { available int64; companion bool; want int64 }{
        {0,false,0}, {4000,false,320}, {100000,false,1536}, {100000,true,512},
    }
    for _, c := range cases {
        if got := memoryTotalBudget(c.available,c.companion); got != c.want {
            t.Fatalf("available=%d companion=%v got=%d want=%d",c.available,c.companion,got,c.want)
        }
    }
}
```

测试文件使用与被测函数相同package并导入testing。其余集成测试输入/操作/精确预期见07；测试框架必须先建真实repo/Engine fixture，禁止把尚未定义的helper当已存在接口。全部开发命令是待执行步骤，不是本次已通过记录。
