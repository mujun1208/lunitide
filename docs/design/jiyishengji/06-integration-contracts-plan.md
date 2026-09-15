# Integration Contracts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐记忆、OCR、媒体之间的真实接线、权限、资源和用户体验合同。

**Architecture:** 保留现有 Go Engine/SQLite/Host Bridge/React。新增应用服务以受授权请求快照为输入，持久状态为真相；不新增外部记忆服务、通用事件数据库或第二套语音引擎。

**Tech Stack:** 当前 go.mod、SQLite、Windows/WebView2、TypeScript/Vitest；不预设尚未验证的 Paddle wheel 组合。

**Spec:** [完整PRD](02-upgrade-prd.md)。本文件是其规范性接口细化，03–05同时适用；不是可选建议。

## Global Constraints

- 本文代码块为待开发合同或测试规格，不是声称当前仓库已有这些新接口。
- 所有新ID授权反查可信subject、scope kind/id；旧接口保留原字段并验证，不能通过省略新字段绕过权限。
- 当前IPC最大4MiB；新增Bridge结果编码后≤512KiB，单个artifact块原始bytes≤65536。
- 先基线/备份，再schema/数据保护，再自动化；模型包和Windows媒体垂直切片独立验证。
- 任何状态改变与外部副作用之间再查权限、取消和fencing；不持有SQLite写事务等待网络/播放/推理。
- 只提交实施任务文件；不得把用户已有dirty代码夹入提交。本文交付不执行这些开发步骤。

## C1. 记忆来源、时序、迁移与遗忘

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
| memory.item.forget | factId,targetVersion?,mode,expectedRevision,operationId | this_version必须targetVersion；fact_history禁targetVersion；subject_rule必须ruleCategory |
| memory.capture.undo | undoOperationId,operationId | 24h；任一项被后续修改则整批MEMORY_UNDO_CONFLICT；不做部分删除 |

新业务mutation需顶层idempotencyKey；targetVersion当前head的删除，恢复仍有效未忘记前版本，否则head为空。批次撤销保存/纠正时恢复该批前的head，但不复活任何墓碑。重复撤销返回首次receipt。

首发“忘记”明确是**删除记忆库及记忆派生数据**，不是删除聊天记录。同步scrub body、FTS shadow、vector、关系文本、candidate/extractor payload、独占legacy mirror与working拷贝，撤销generation引用、缓存；event/trace不存正文。原聊天、compaction/handoff/周归档可能仍包含原信息，UI必须明确显示“不会删除原聊天；当前聊天仍可能引用原文”。所有Memory API不得沿source暴露被忘记正文，也不得从被忘记source/span后台重新提取。

0160增`memory_source_suppressions(subject_id,source_id,source_revision,start_byte,end_byte,fact_id,reason,created_at)`；capturer/companion归档转记忆入口统一查抑制记录。它防重新写记忆，不声称从模型上下文抹除原聊天。若用户要求“删除聊天及摘要”，调用产品相应数据删除流程；当前没有完整覆盖时明确“不支持一键清除全部历史派生”，不能用记忆forget假成功。该更强功能不混入本期满分范围。

### 备份与导入导出

1. 在新迁移之前、旧schema可打开且验证通过的阶段调用真实`Store.CreateBackup(ctx,destination)`；备份在受管备份目录、唯一文件名，不覆盖原库。Store打开流程需新增pre-migration hook，不能等自动迁移后补拍。
2. 迁移后用同一read transaction固定snapshotRevision，流式导出所有section并生成digest/count。64MiB/100000行上限是portable archive上限，不是数据库大小上限；超限明确报EXPORT_TOO_LARGE，引导一致DB备份，禁止称截断结果为完整导出。
3. 0160增`memory_archive_artifacts(artifact_id,subject_id,scope_kind,scope_id,content_ref,sha256,size,state,expires_at,created_at)`和Memory专用CAS root；state=staging/sealed/expired，仅sealed可预演，默认24h保留。上传复用现有附件选择/授权后由Engine登记并流式封存，不接受任意本地路径；导出亦登记此表。读/删采用独立root、授权和读lease（另建memory_archive_leases，字段lease_id/artifact_id/owner/expires_at，60s）；preview引用未过期时不得GC。24h内commit再验digest/revision。
4. 本机已有tombstone/suppression优先于导入旧备份；外来scope不能自动扩权，外来subject需显示绑定当前身份的预演且仅review，不接受原ID直接作为授权。
5. `RestoreBackup`会关闭Store，调用方必须重新打开；恢复旧二进制及旧DB会舍弃升级后数据，必须用户明确选择。功能降级保留基础canonical reader/删除屏障。

## C2. 上下文权限与总成本

自由文本记忆不能成为system指令。working、episode、observation、工具结果和导入均以明确标识的低信任数据块注入。Core仅对白名单属性language/display_name/output_format由产品模板生成偏好；不得允许保存的“忽略审批/泄露密钥”等自由文本升权。最终provider messages需测试，而不只测试数据库字段。

`availableInput`固定采用contextapp.ProviderInfo.EffectiveInputBudget()在加入memory之前的值；记忆计数加入ContextEnvelope并从其余消息预算扣除一次，不能双扣或漏扣。总上限min(1536,floor(availableInput*0.08))，Companion另限512；每槽与序列化标签均计数。未知tokenizer乘1.15是预算估算，不保证实际服务商分词永不超；provider报超长时压缩一次并记录实际usage，不宣传exact。

remote modelAssist默认跟随用户已有“允许文本模型远端处理”策略；未选择/仅本地模式为off。启用auto不等于授权上传。query embedding也遵守相同策略与预算。

新增`memory_budget_days(subject_id,utc_day,limit_tokens,reserved_tokens,settled_tokens,revision)`及`memory_budget_reservations(job_id,attempt_id,maximum_tokens,state,actual_tokens)`。默认每主体UTC日32768预算tokens，用户可设0关闭远端辅助；统一覆盖extract/consolidate/embed/query_embed，额度不足deferred，仍保留fast path/FTS。每次调用前事务预占最坏输入+输出上限，所有重试单独预占；usage缺失不能按0结算，保留预占值。次日新账本，不回写旧记录。此上限是应用预算、非服务商账单绝对保证。

主回答与后台成本分别显示，同时报告端到端总量：answer input/output+extract+query/embed+consolidation+重试。缓存命中要记录，不可仅拿注入减少证明“省钱”。不同模型token不直接当相同货币；无可信价格不计算节省金额。

query embedding不得拖慢前台：总recall deadline150ms（含取query向量）、dense扫描子预算100ms；未拿到向量就FTS/time降级并记录原因。取消网络请求并单独记已产生usage；不得假称HTTP一定100ms返回。所有fallback同样查授权、hidden/tombstone/sensitivity，不能为了快退到不安全legacy缓存。

## C3. OCR：作用域与生产消费者

```go
// 拟加入 internal/ocrapp/request.go；Subject由Engine注入，不是公开payload。
type OCRScope struct { SubjectID, Kind, ID string }
type ResolvedOCRRequest struct {
    Scope OCRScope
    SourceDigest, PolicyRevision, GateRevision string
    ProviderID, ModelID, PackManifestDigest, PipelineKind string
    AllowRemote bool
}
type ScopedRoutingStore interface {
    Get(context.Context, OCRScope) (Routing, error)
    CompareAndSet(context.Context, OCRScope, string, Routing) (Routing, error)
}
```

ResolvedOCRRequest在入口授权后生成一次；provider callback直接消费该快照，禁止ocrProviderCall中再次Routing()取全局绑定。身份切换后旧任务可取消，不能改用新主体凭据继续。旧ocr-routing.json只迁给经确认的本地原主体，无可确定主体则保留文件、禁用绑定并显示“需重新选择”，不得复制给所有组织。

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

- Pack availability：not_installed/ready/quarantined。更新中的旧current仍ready。
- Install operation：requested/preflighting/downloading/verifying/installing/self_testing/succeeded/failed/cancelled。pack ready不是operation终态枚举。
- requested/preflighting/downloading可设置cancel_requested；verifying及原子激活不可取消，返回OCR_OPERATION_NOT_CANCELLABLE。卸载请求等运行lease释放，30s后仍占用则failed/OCR_PACK_BUSY，保留current，不强删正在使用DLL。
- 0163 operation新增lease_owner/lease_until/heartbeat_at/attempt/fence；CAS claim租约60s心跳15s，每次接管fence递增。激活事务必须同fence且目标digest一致；旧worker/runner结果拒绝。
- 新`ocr_version_leases(lease_id,pack_id,version,run_id,owner,fence,expires_at)`保护加载版本；重启先终止/确认孤儿进程再回收旧lease。
- `ocr_artifact_leases(lease_id,artifact_id,owner,expires_at)`保护正在读的结果，60s租约。独立OCR CAS root不与全局workspace CAS做跨域去重/GC；同OCR digest可复用，查询ocr_artifacts有效引用和lease均为0才可删。

新`internal/ocrapp/artifact_store.go`实现受控OpenRange/Verify/DeleteUnreferenced，内部只接受校验后的digest不接受path。返回流式io.ReadCloser；单次原始bytes≤65536，metadata摘要≤4KiB/项，run.get最多20页且总JSON≤512KiB。`ocr.artifact.read`返回base64,nextOffset,eof,sha256,totalBytes；客户端拼接完再UTF-8解码，不逐块破坏汉字。缺失OCR_ARTIFACT_MISSING、损坏OCR_ARTIFACT_CORRUPT，不返回半个结构JSON当成功。

安装包requirements.lock锁所有Python wheel/hash；发布manifest包含它的digest，完整CPU profile还记录CPython ABI、CPU指令集、Windows最低版本、DLL清单及每个依赖许可证。真实组合由W0构建报告确定；没有报告就是profile disabled，不臆造一个版本号称已支持。

## C5. 媒体：Host、epoch、生命周期与音频焦点

所有player attach/next/report设x-owner=host，Host代理向既有私有internalRuntimeHandlers发送带Host生成windowInstanceId/navigationEpoch的请求；这些字段不能来自Renderer payload。内部RPC不加入公共envelope。window销毁、logout、navigation epoch变更撤销lease/ticket。无需新增公开监听端口。

每个session有lease generation；每次实际装载/重播另增playbackEpoch。report必带assetId+playbackEpoch+eventSeq；命令回执再带operationId，自然position/ended不要求虚构操作ID。旧epoch事件直接忽略，不能更新新轨道状态。ended幂等键(session,epoch)只生成一次下一首操作。业务command接受后player.next取绝对目标，重放不能再次计算“下一首”。

新增`media.operation.get/list`返回历史操作与parent/rootOperationId；list按scope+cursor分页。工具调用已映射media operation时Activity仅展示media主记录，保留关联tool ID，不重复计数。

App共同根持久挂MediaProvider/OwnedMediaPlayer/MediaTray；实施选择固定为`web/src/App.tsx`的三个route返回使用同一层级共同MediaProvider包装（不加变化key），不能在key变化的SessionPage中挂载。页面导航持续播放；整页刷新/Engine重启只能恢复暂停快照，不能自动出声。0164增加media_audio_focus singleton表，字段见02/05；CAS约束应用至多一个owned audible session，切换前暂停旧目标并核验，失败则新目标不启动。

| 发起场景 | 权限/控制 |
|---|---|
| 用户点击本地文件播放 | Host选择授权+媒体scope检查；不需要打开电脑控制，不触发额外模型审批 |
| Agent播放用户已授权owned资产 | ToolRuntime共用治理/审计；不得自行扫盘；派发前再查撤销/急停 |
| external播放器控制 | 原computer capability/审批/桌面串行/急停全部保留 |
| owned队列自动下一首 | 首次用户同意队列autoAdvance默认true；仅该队列已授权资产；急停/权限撤销即停止，无隐式下载 |

音频焦点首版取保守策略，不新增语音模型：owned播放遇TTS或麦克风开始，先暂停并等真实pause回执（500ms超时即不开始麦克风，提示手动暂停）；TTS/录音结束后仅在focus token仍有效、资产/epoch未变、用户未手动操作、原本playing时恢复。取消、切设备、退出、急停不自动恢复。麦克风与TTS沿现有互斥/打断策略。

external无可靠暂停能力时提示“请暂停外部媒体或使用耳机”；首版不宣称AEC可以过滤电影对白，不能为语音而绕过审批发媒体键。屏幕/loopback音频不是用户语音来源，不自动进入记忆。

SMTC现DTO没有capabilities/timeline/sessionKey。首版仅开放经实际枚举且唯一匹配的play/pause/stop/next/previous；seek/volume在未增加并验证底层API前禁用。新增字段同时改`media_session.go`、Windows脚本/Go及other stub；多session同app时uncertain，不猜目标。

## C6. 活动中心、视觉与可用性

新增`activity.list {scopeKind,scopeId,domains?,cursor?,limit?}`，limit1..100，响应`items,nextCursor,snapshotAt,hasMore`。保留旧operation.list DTO，不偷偷加必填字段。后端在同一read tx查询已有continuity operation journal、OCR operations/runs和media_operations，不建第四张活动真相表。

首版分页定义为“创建历史”：按(created_at DESC,domain,id) keyset，cursor绑定subject/scope/filter/snapshotAt和签名；后续页只查created_at≤snapshotAt，显示这些条目的最新状态，不承诺跨请求repeatable-read状态快照。前端按domain/id更新去重；刷新重置cursor获得新创建条目。变化中的updated_at不作为翻页键，避免漏行。缺失历史时间返回null/“未知”，不以当前时间冒充。

`ActivitySnapshot`含activityId/domain/kind/phase/terminal/verification?/title/retryable/recoveryAction/createdAt?/updatedAt?/scopeKind/scopeId/rootOperationId?；subject由Engine过滤，UI不据此自行授权。安装只显示本地主体可见操作。media源优先，关联同rootOperationId的tool不重复列项。

视觉使用现有`web/src/styles.css`变量--bg/--bg2/--ink/--muted/--rule/--tide1/--ok/--warn/--err与themeStore，新增局部CSS不建立第二套主题。组件间距8/12/16/24px，按钮最小32px，主要播放按钮40px，焦点环2px。内容区≥960px显示tray完整控件；680–959px折叠次要信息；<680px队列全宽drawer，保证200%缩放不遮挡输入/按钮。长标题两行省略但可聚焦查看全文，错误必须文字+图标，不能仅颜色。

```text
聊天内容 / 办公内容（原布局）
└─ 轻提示：已记住 1 条 · 撤销 · 查看（不抢焦点）
应用底部 MediaTray：封面 标题 | 上一首 播放/暂停 下一首 | 进度 | 队列
右侧 Drawer：当前队列 / 活动详情（二者互斥，Esc关闭回焦点）
设置 → 记忆：现在 / 近期 / 待审阅 / 历史整理 / 隐私数据
设置 → OCR：策略 / Windows状态 / 可选模型包 / 最近运行
```

记忆toast 2s后消失，24h撤销入口保留；不自动移动焦点。导入预演、清空确认可使用dialog并管理焦点。axe serious/critical=0加真实WebView2键盘/读屏，不能把jsdom检查等同所有视觉通过。

## X0. 基线、合同与迁移前备份（先于03 Task1）

**Files:** 修改`internal/storage/sqlite/store.go`、`backup.go`的调用装配；新增`memory_upgrade_backup_test.go`；评测manifest位于各testdata子目录。文档证据落本目录`evidence/`（开发时新建）。

**Consumes:** 当前0159库、CreateBackup。**Produces:** 预迁移备份hook、只读验证报告、冻结数据集digest。

- [ ] 写`TestMemoryPreMigrationBackupContains0159`：打开旧fixture，写唯一canary，升级前生成备份；升级后读取备份schema仍0159且含canary；中途失败不能破坏原库。
- [ ] 执行`go test -count=1 ./internal/storage/sqlite -run 'TestMemoryPreMigrationBackup|TestBackup'`，确认新业务断言失败而非编译错误。
- [ ] 使用已有CreateBackup而不是另写文件拷贝算法；为Open过程加迁移前hook并验证相同连接无写入窗口。source解析/权限/DTO冻结进入schema正反例。
- [ ] 同命令绿测；`git diff --check`只检查本任务修改；提交`test(upgrade): freeze baselines and protect pre-migration database`。

## X1. 记忆源与上下文接线（03 Task2/4/5 同批）

**Files:** `internal/app/chat_run_stream.go`、`chat_memory.go`、`chat_memory_workers.go`、`data_scope.go`、`bootstrap/wire.go`、`contextapp/assemble_envelope.go`、`internal/storage/sqlite/m8_memory_v2.go`；新增同目录测试。

**Consumes:** MemorySource、scoped repository、source suppressions。**Produces:** 真实user evidence、终态轻提示、低信任注入、已授权历史/undo合同。

- [ ] 将07中的M-R01～M-R10编为表驱动fixture，断言数据库与最终provider messages，不仅service mock。
- [ ] 执行`go test -count=1 ./internal/app ./internal/m8app ./internal/contextapp -run 'TestMemorySource|TestMemoryPrompt|TestMemoryUndo|TestMemoryQueue'`确认红测。
- [ ] 按C1/C2接线，默认policy不可用于生产v2；扩展data_scope的新ID解析；claim fence/每日预算同事务验证。
- [ ] 运行同命令及03对应包回归，确认off/manual和原chat归档边界；提交`feat(memory): bind sources scopes and bounded context safely`。

## X2. OCR消费者、页渲染与租约（04 Task1/3/4/7 同批）

**Files:** `internal/ocrapp/request.go`、`artifact_store.go`、`route.go`、`recognize.go`；`internal/doctext/pdf_render_stream_windows.go`/other；`internal/app/ocr_wire.go`、`office_source_text.go`、`model_catalog.go`及实际KB/workspace调用点；0163。

**Consumes:** ResolvedOCRRequest、ScopedRoutingStore、taskDir页引用。**Produces:** 所有入口scoped pipeline、512KiB响应、受租约保护的独立OCR结果库。

- [ ] 建O-R01～O-R08红测；百页fixture以页文件引用消费，统计最大同时存活页数≤2、取消不遗留worker。
- [ ] 执行`go test -count=1 ./internal/ocrapp ./internal/doctext ./internal/app -run 'TestOCRScope|TestOCRSnapshot|TestOCRArtifact|TestPDFRenderBounded|TestOfficeOCRCache'`。
- [ ] 按C3/C4实现，结果被请求取消或fence过期时拒绝提交；旧Result.Text超限明确partial；不得为修测试改全局IPC上限。
- [ ] 同命令绿测、真实pack门另外记录；提交`feat(ocr): integrate scoped bounded document recognition`。

## X3. App媒体焦点与活动分页（05 Task5/6/7 同批）

**Files:** `web/src/App.tsx`、`web/src/media/MediaProvider.tsx`、`audioFocus.ts`、`web/src/session/companion/ttsPlayer.ts`、`speech.ts`；`internal/app/activity_handlers.go`、`media_private_handlers.go`；`internal/storage/sqlite/activity_query.go`、`continuity.go`；相关schema、Host gateway、SMTC DTO/script。

**Consumes:** C5事件epoch与Host身份，C6活动查询。**Produces:** 单一owned发声、跨页面连续播放、TTS/ASR协调、可翻页真实历史。

- [ ] 建A-R01～A-R10及V-R01～V-R04红测，先用可控fake时钟/媒体事件，再实机。
- [ ] 执行`npm --prefix web test -- src/media src/activity src/session/companion`及`go test -count=1 ./internal/app ./internal/hostbridge ./internal/winexec -run 'TestMedia|TestActivity'`，逐条检查退出码。
- [ ] 先落Host/epoch、后App provider、再音频焦点及activity.list，按C5/C6实现；UI不能自己授予成功状态。
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
