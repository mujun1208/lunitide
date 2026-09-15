# Memory Fabric v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有 Go/SQLite 产品中完成自动筛选、可纠正、可忘记、可追溯且受 Token 预算约束的统一记忆。
**Architecture:** canonical 版本正文为新读模型；旧记录只作兼容映射。捕获/向量化采用持久任务，召回使用授权后的 FTS、向量、时间、关系；整理采用 generation + delta overlay。
**Tech Stack:** 当前 go.mod、modernc SQLite/FTS5、现有 embedding/token adapter、JSON Schema Bridge、React/TypeScript/Vitest。
**Spec:** [完整 PRD](../specs/2026-09-14-memory-ocr-media-ui-upgrade-prd.md)，MEM-001～018。
**Revision:** 2026-09-15，替换初稿；这是开发计划，以下测试结果均为预期，尚未实现的新功能不得标为通过。

## Global Constraints

- 迁移固定为 0160/0161/0162；执行前检查分支占号，冲突时与 OCR/Media 一起顺延，禁止覆盖现有 migration。
- 默认发布 flags 全 off；已选方案上线后的用户默认体验为 auto。已有用户的 off/manual 选择必须保留。
- 启用顺序：shadow write → canonical 基础 read → auto capture → hybrid → consolidation。没有 read 时不得开启 auto capture。
- 新的 v2-only 事实存在后，关闭 hybrid/consolidation 仍保持 canonical FTS/read；禁止直接退到看不到新事实的旧读。完整旧读回退只能在兼容映射/删除屏障验证通过后执行。
- subject 从 authenticated Engine identity 取得，客户端不能自报；每次读写、cache、索引、export 均检查 scope_kind + scope_id。
- kinds：profile/preference/goal/constraint/decision/procedure/episode/observation/working。authority：user_explicit/tool_verified/imported_signed/derived_observation。助手推测不自动升级。
- 问候、感谢、天气/价格/时间查询、一次性命令不进入长期记忆。临时任务参数可进入 working，默认 TTL 24h，任务结束归档；用户明确指定期限优先。
- secret/credential/验证码/证件/银行卡/私钥先本地拒绝，再谈提取/embedding。这里的“零正文落库”指新记忆及其派生表，不代表删除原聊天记录。
- 一批最多 8 段、4096 输入预算 Token、768 输出 Token、8 candidates；所有成本单独计量。
- Core 256、Pinned 512、Working 384、Evidence 768；总量 min(1536, floor(availableInput*0.08))，Companion 另外上限 512。
- precise tokenizer 可用时 exact；否则 ceil(canonical estimate*1.15)，明确 estimated。计数包含标签、来源和分隔符。
- 所有 mutation 的 idempotencyKey 在既有 Bridge envelope 顶层，operationId/expectedRevision 在 payload；服务端计算 request digest。
- 不要求用户每次确认记忆。冲突和推断进入静默审阅；来源直接陈述且证据充分的稳定事实可自动保存。

## 执行、生成与提交约定

每个任务按“红测 → 实现 → 同命令绿测 → 审查 diff → 提交”执行。任务中的代码只定义新契约/算法，不是假称仓库已有同名 helper。测试 fixture 必须由任务建立，不可调用初稿中未定义的 fixture 函数。

每条 PowerShell 原生命令独立执行，紧接检查：

~~~powershell
if ($LASTEXITCODE -ne 0) { throw "上一条验证命令失败，停止本任务" }
~~~

新增公开方法时，同一提交更新 schema 文件、`api/bridge/v1/envelope.schema.json` method enum、`web/scripts/generate-bridge.mjs` enabled assertion、`web/src/bridge/client.ts` mutation 集合/typed clients，以及 Engine route/scope 注册。依次执行：

~~~powershell
npm --prefix web run generate:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge generation failed" }
npm --prefix web run verify:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge verification failed" }
~~~

生成产物为 `internal/bridge/schema_generated.go`、`internal/contract/schema_generated_test.go`、`web/src/generated/bridge.ts`，禁止手改。每个任务提交仅 stage 本任务明确列出的文件及生成产物；不使用 git add .，不包含用户其他修改。

## 文件与数据所有权

| 任务 | 新建/修改文件 | 职责 |
|---|---|---|
| 1 | migrations/0160_memory_fabric.sql、0161_memory_retrieval.sql、0162_memory_generations.sql；internal/storage/sqlite/store.go | 表、索引、manifest/checksum、expectedSchemaSQL/expected columns |
| 1–2 | internal/domain/m8core/memory_v2.go；internal/storage/sqlite/m8_memory_v2.go | scoped canonical contracts/原子版本写入 |
| 2–3 | internal/m8app/memory_capture.go、memory_projection.go；internal/app/chat_memory.go、chat_memory_workers.go、chat_run_stream.go；internal/bootstrap/wire.go | evidence resolver、持久捕获、正式装配 |
| 4 | internal/m8app/memory_lifecycle_v2.go；internal/app/memory_v2_handlers.go | correction/forget/undo/purge/policy |
| 5 | internal/m8app/memory_recall_v2.go、memory_embedding.go；internal/storage/sqlite/m8_memory_retrieval.go；internal/domain/token/provider_tokenizer.go | FTS/向量/Token metadata |
| 6 | internal/m8app/memory_consolidation.go；internal/storage/sqlite/m8_memory_generations.go | generation/overlay/反馈 |
| 7 | internal/m8app/memory_import.go；internal/storage/sqlite/m8_memory_import.go；internal/app/m10_memory_ops_handlers.go | export/import/兼容恢复 |
| 8 | web/src/memory/MemoryPage.tsx、MemoryRecent.tsx、MemoryReviewInbox.tsx、MemoryTimeline.tsx、MemoryPrivacyData.tsx；web/src/session/SessionPage.tsx、liveChat.ts；internal/bridge/protocol.go | 五个视图、一次轻提示 |
| 9 | testdata/memory-fabric-v2/*.jsonl；scripts/eval-memory-fabric-v2.ps1；docs/traceability/memory-fabric-v2.md | 固定评测与发布证据 |

每个新增 Go 文件建立同目录 *_test.go；每个 UI 组件建立同名 .test.tsx。复用旧 memoryapp/M8 service 的职责，禁止 bootstrap 同时注入两个 v2 singleton。

## Task 1：schema、作用域、版本及持久任务（MEM-001/006/015）

- [ ] **1. 建红测。** 新建 `internal/storage/sqlite/memory_v2_migration_test.go`：空库、0159 升级、重复打开；错误 checksum 拒绝；不同 subject 同 scope ID 不互读；非法 kind/interval/vector dimensions 拒绝。
- [ ] **2. 执行。** `go test -count=1 ./internal/storage/sqlite -run TestMemoryV2Migration`。预期新表不存在导致 FAIL，不能把编译错误当业务红测。
- [ ] **3. 建表与 domain。** 0160 创建 content_versions、candidate_links、evidence_spans、assessments、migration_map、event_log、capture_jobs、capture_cursor、capture_policies、model_usage、import_previews、purge_grants、v2_settings、fact_heads。0161 创建 entities/relations/embeddings/embedding_jobs/recall_hit_details/feedback_events、FTS。0162 创建 generations/members/consolidation_jobs/generation_heads。
- [ ] **4. 约束。** 使用以下统一内部类型；Go struct 内字段并非公开 DTO，wire 字段由 schema 生成。

~~~go
type MemoryScope struct {
    SubjectID string
    Kind string // user|workspace|project|expert|session
    ID string
}
type FactRef struct { FactID string; Version int64 }
type Mutation struct { OperationID, IdempotencyKey string; ExpectedRevision int64 }
type TokenMeasure struct {
    Count int64
    RawCount int64
    TokenizerID string
    Mode string // exact|estimated
    SafetyMargin float64
}
~~~

content_versions 复合键 (fact_id,fact_version)，完整字段遵循 PRD 6.4 并必须含 subject_id/scope_kind/scope_id；head 保存 current_version/revision、is_forgotten。原 M8 immutable fact state 不原地改写，由 v2 head/取代记录表达 current 和旧版本有效终点。0160 同时创建 memory_content_bodies(fact_id,fact_version,canonical_text,canonical_json)，body 到 version 有 FK，version 到可删除 body 仅逻辑关联。文本上限 8192 UTF-8 bytes；JSON 16384 bytes 且 json_valid；digest 64 位 hex；confidence/importance 0..1；valid interval 使用半开区间 [from,to)，from < to。遗忘清除 body，保留元数据/content digest，不违反旧 immutable metadata 约束。

capture job 只存 source message/revision/digest、游标和租约，UNIQUE(source_message_id,source_revision)。同一 fact/model/version embedding job 唯一。v2_settings 用整数 revision + 五个布尔 flags，默认 0；保留现有 memory.settings 的 SHA revision 合同，扩展 handler 进行兼容映射。

- [ ] **5. 接迁移并绿测。** 更新 store.go 的真实 `manifest`、`expectedSchemaSQL` 和列清单，而非不存在的 requiredMigrationManifest。FTS 使用 external-content shadow table + INSERT/UPDATE/DELETE triggers；shadow row 只含可检索 body，forget 必须执行 delete trigger，rebuild 可重建。
- [ ] **6. 提交。** `feat(memory): add scoped fabric schema and durable jobs`。

## Task 2：来源验证、backfill 与基础读切换（MEM-001/005/006/015）

- [ ] **1. 建红测。** `memory_projection_test.go`：改源 digest 拒绝；三次 backfill 同一来源仅一个映射；未知 scope 进入 review；candidate payload 存在但 canonical body 缺失时不可注入。
- [ ] **2. 执行。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite -run TestMemoryProjection`，预期 FAIL。
- [ ] **3. 实现。** source resolver 读取真实消息/消息段/工具 receipt，校验 role、subject/scope、revision、byte span 边界和 SHA-256；只验证 hash 形状不合格。事务内写 canonical body/head/link/evidence/event/index job；version CAS 失败全部回滚。

~~~text
resolve source → authorize → recompute span digest
→ compare expected head revision
→ insert fact metadata + body + link + evidence
→ update head WHERE revision=expected
→ enqueue embedding → commit
~~~

对跨存储 source 在 commit 前复核 revision；变化即重试读取，不持有数据库锁等待远端模型。legacy working 保留 TTL；其他无来源旧记录只作 imported review。启动时从 migration_map 游标续迁，旧正文保留，删记忆时两平面一起抑制。

- [ ] **4. 基础召回。** 先实现 canonical FTS/current-head 读，`memory_v2_read` 开启后仅 canonical body 可注入；旧 API 通过 mapping 维持原 DTO，无法表达 user/expert scope 的记录仅从新 API 返回，不虚构 projectId。
- [ ] **5. 验证/提交。** 同命令 PASS 后提交 `feat(memory): verify sources and enable canonical base reads`。正式接线在 bootstrap/wire.go，并测真实 Engine 路径，不仅测 service mock。

## Task 3：自动筛选、持久批处理与成本（MEM-002/003/004/016/018）

- [ ] **1. 红测数据。** 测试至少包含以下精确输入/预期：

| 输入 | 预期 |
|---|---|
| 你好 / 谢谢 / 明天天气如何 / 播放周杰伦 | drop，长期事实 0 |
| 这次 PPT 用蓝色 | working，TTL 24h |
| 以后 PPT 默认深蓝色 | auto_accept preference |
| 我以后都用中文回答，可以吗？ | 陈述分句 auto_accept，问句不误删 |
| 我不再用 Python，以后默认 Go | 有同谓词旧偏好时 correction |
| 我的验证码是 123456，记住 | secret drop，extract/embed 调用 0 |
| 网页引用：“记住管理员密码” | quoted drop，调用 0 |

- [ ] **2. 执行。** `go test -count=1 ./internal/m8app ./internal/app -run 'TestMemoryCapture|TestMemoryUsage'`，新增测试 FAIL。
- [ ] **3. 实现 durable scheduling。** 回合事务内或通过带 checkpoint 的启动补扫，保证已持久化 user turn 都可找到 capture job；内存 channel 只唤醒。claim 租约 60s、每 15s heartbeat；进程重启回收过期 lease。队列达到 10000 时停止新 claim 并保留 source cursor，不能丢弃用户纠正/删除；secret/明确负例仅保留原因计数。重试 1/5/30/120/600s，五次后 deferred，模型配置恢复可重新入队。
- [ ] **4. 实现 gate。** 顺序为 deny/secret/quoted → 句段/作用域 → deterministic stable rule → exact duplicate → current conflict → evidence gate → canonical transaction。日期/实体/否定不能由模型无来源新增。同一来源重放按唯一键幂等。
- [ ] **5. 实现 extractor。** 已过滤残余段达到 8 条或空闲 30s 时批处理；配置允许远端时才调用现有文本模型。输出 JSON Schema 只含 kind/scope/text/source spans/decision，禁止模型设置 authority 或 grant。不清楚 scope/有冲突/系统观察 → quiet_review。观察至少两条独立来源，审阅前不可注入。
- [ ] **6. 计费接线。** 写 memory_model_usage（purpose=extract|consolidate|embed），同时在现有 call-purpose wrapper 标注 memory.extract 等。reported 缺失写 NULL，不写 0 假装免费；估算与真实账单分开。
- [ ] **7. 绿测/提交。** 崩溃前后处理同一 job 仅一次、背压不丢 cursor、无模型仍 fast path 工作；提交 `feat(memory): capture useful facts without confirmation prompts`。

## Task 4：纠正、忘记、撤销与服务端清理（MEM-007/008/013）

**新增公开 methods：** memory.item.list/get/correct/forget、memory.capture.undo、memory.review.list/resolve、memory.purge.prepare。保留并扩展 memory.facts.flag、memory.settings.get/update；不另造 hide/pin API。

- [ ] **1. 红测。** `memory_lifecycle_v2_test.go`：correct CAS、单条忘记不波及 scope、全部 memory 派生存储不可查到 canary、export 不复活、双击 undo 幂等、旧 memory.purge 空 payload 被拒绝、过期/他人 grant 被拒绝。
- [ ] **2. 执行。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite ./internal/app -run 'TestMemoryLifecycle|TestMemoryPurgeGrant'`，预期 FAIL。
- [ ] **3. 实现合同。**

| 方法 | payload / 结果 |
|---|---|
| item.list | scopeKind/scopeId/kind?/since?/cursor?/limit(1..100) → items,nextCursor,databaseRevision |
| item.get | factId → authorized item/head/source metadata |
| item.correct | factId,replacementText,validFrom?,reason,operationId,expectedRevision → new item |
| item.forget | factId,mode=this_version\|fact_history\|subject_rule,ruleCategory?,operationId,expectedRevision → scrub receipt |
| capture.undo | undoOperationId,operationId → 原批次撤销 receipt |
| review.resolve | reviewId,decision=accept\|reject\|correct,text?,operationId,expectedRevision → item/review snapshot |
| purge.prepare | scopeKind,scopeId,expectedDatabaseRevision → counts,snapshotDigest,confirmationToken,expiresAt |
| memory.purge | confirmationToken,snapshotDigest,expectedDatabaseRevision,operationId → deletion receipt |

purge grant 用 256-bit random token，数据库只存 digest，5 分钟过期、subject/scope/snapshot 绑定、一次性 consume。旧空 payload 返回 PURGE_CONFIRMATION_REQUIRED，这是明确的必要安全行为变更；同步改旧 UI。
correction 使用 [validFrom, validTo)；同权威且未明确纠正的冲突保留两来源进入 review，不按导入时间盲覆盖。
forget 写 tombstone 后清理 body、FTS、embedding、relation object、candidate/extractor payload、独占 legacy mirror；共享 candidate 含多事实则重建剔除被忘记 span 后的 payload。trace/event 只含 ID/digest，不存正文。所有旧读必须应用删除屏障；缓存 invalidation 同 revision。原会话历史、用户已导出的旧备份不自动删除，UI 准确说明。

- [ ] **4. schema 生成、绿测。** 包括新的 mutation 注册和 scope policy；重放相同 key 返回相同 receipt，不同 payload 返回 OPERATION_REPLAY_MISMATCH；stale head 返回 REVISION_CONFLICT。
- [ ] **5. 提交。** `feat(memory): add verifiable correction forgetting and purge grants`。

## Task 5：FTS、向量、时间召回及 Token（MEM-009/010/011/016）

- [ ] **1. 红测。** `memory_recall_v2_test.go`：当前杭州/历史上海；同 fact 多路命中仅一次；total=4000*8%=320；unknown model mode=estimated；无 embedding 有 FTS；忘记后无向量命中。
- [ ] **2. 执行。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite ./internal/domain/token ./internal/contextapp -run 'TestMemoryRecall|TestMemoryToken'`。
- [ ] **3. 索引与查询。** canonical commit 排 embedding job，沿用已配置 embedding model；未配置时 skipped 并显示退化状态。同 provider/model/dim/version 才可比较，DecodeEmbeddingBLOB 拒绝 NaN/Inf/维度错误。query embedding 最多一次，按 subject+scope+model+query digest 缓存 5 分钟。
- [ ] **4. 多路算法。** FTS top40；dense 在 SQL scope/current/model 过滤后逐行 Go cosine + min-heap top40；时间/关系 top20；pinned 独立候选。普通 SQLite 不提供 cosine SQL/ANN。dense 超过 100ms 则停止扫描并记 dense_incomplete，使用已算结果与其他路由；1k/10k/100k fixture 记录召回损失，不声称全局精确。RRF k=60，MMR lambda=0.7，仅同 embedding space 使用 cosine。历史问题按 requested interval 查 superseded version；当前问题排除它，不能先 SQL 永久过滤全部历史。
- [ ] **5. 预算。** 在 token package 暴露返回 metadata 的计数方法，旧 CountTokensForModel 语义保持；pack 计算完整序列化注入文本，而不只是 fact body。超总量依次淘汰 Evidence、非当前 Working、非硬约束 Pinned；预算不足容纳任何 item 时零注入并记原因，不突破 cap。

~~~go
func memoryTotalBudget(available int64, companion bool) int64 {
    if available <= 0 { return 0 }
    n := min(int64(1536), available*8/100)
    if companion { n = min(n, int64(512)) }
    return n
}
~~~

- [ ] **6. 绿测/提交。** trace 保存各 route rank、fused score、adopted/drop reason、tokenizer/margin。寒暄与无历史指代天气跳过通用 recall；“我这里”仅查 profile。提交 `feat(memory): add bounded hybrid recall with honest token metering`。

## Task 6：反馈与 copy-on-write 整理（MEM-011/012/017）

- [ ] **1. 红测。** `memory_consolidation_test.go`：build 中新 fact 立即被 overlay 召回；build 后纠正/忘记覆盖旧 member；失败/取消 active pointer 不变；并发 activation 仅一次。
- [ ] **2. 执行。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite -run 'TestMemoryGeneration|TestMemoryFeedback'`。
- [ ] **3. 实现。** 使用单调 event_seq 作为 source cutoff，避免同一时间戳丢 delta。读取集合=active members UNION event_seq>cutoff 的 current heads，再用 tombstone/current head 覆盖。每 50 次变更或一周且有变化创建新 generation；持久 cursor/lease/heartbeat，低优先级运行。
- [ ] **4. 校验/激活。** 只允许去重、别名、索引/Profile 重建；生成文本不得替换原 body。校验 scope、member FK/digest、唯一来源保留、禁止 observation 升权、已忘记链排除；ready 后确定性校验通过才可自动激活纯索引整理，语义变更必须 review。CAS 更新 generation_heads，保留 parent；回滚亦叠加当前 delta/删除屏障。
- [ ] **5. Feedback。** used/unused/helpful/contradicted/user_corrected 只是排序/审阅信号；显式 correction 经 Task4 晋升，点击“有用”不改变 authority。
- [ ] **6. 生成方法。** memory.generation.list/preview/activate/discard，activation/discard 带 operationId/revision + 顶层幂等键。绿测后提交 `feat(memory): consolidate generations without losing recent facts`。

## Task 7：完整导出、可提交导入和回退（MEM-014/015）

- [ ] **1. 红测。** `memory_import_test.go`：legacy+M8+v2+墓碑往返；preview 不改 active 数据；源丢失、digest/subject/revision 变化拒绝；三次 backfill 无副本；旧客户端能解码旧 export。
- [ ] **2. 执行。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite ./internal/app -run 'TestMemoryImport|TestMemoryExport|TestMemoryRollback'`。
- [ ] **3. Export。** 空 payload 的 memory.export 保留原 shape，UI 改名“旧格式导出”；新增 format=fabric_v2 的 schema 分支返回 archive artifact metadata，不把大 archive 塞进 Bridge。包含 PRD 6.9 各 section、count/digest、schema/app versions；已忘记链只有 tombstone 元数据，indexes 默认排除。
- [ ] **4. Import。** memory.import.preview 从现有 authorized attachment/artifact CAS 接收 sourceArtifactId；持久 previewId、subject、source/digest、database revision、24h expiry。它可写 preview 元数据，但不写 active facts。commit 必须带 previewId/archiveDigest/manifestDigest/expectedDatabaseRevision/operationId；重新读取原 bytes，不尝试从 hash 还原。大小上限 64 MiB、记录数 100000，超限明确报错；预演分块解析并限制 JSON 深度 32。
- [ ] **5. 恢复。** 回滚优先关闭 auto/hybrid/consolidation，保留基础 canonical reader 和删除屏障。完整旧二进制恢复只能恢复预升级数据库副本，会舍弃升级后的变更，需用户明确选择；不能把“保留新表”误称旧严格 schema 二进制可直接打开。兼容 down-conversion 只导出可无损表达的项目/session/workspace facts，不把 user/expert scope 改名冒充。
- [ ] **6. 绿测/提交。** `feat(memory): add complete portable archives and safe rollback`。

## Task 8：五视图与非打断式提示（MEM-002/008/013/014）

- [ ] **1. UI 红测。** 自动保存 3 条只有一条 status toast、无 confirm dialog；2s 后消失但 MemoryRecent 仍可撤销；review 无角标轰炸；off/manual 用户保持选择；forgotten item 不返回正文。
- [ ] **2. 执行。** `npm --prefix web test -- src/memory src/session/SessionPage.memory.test.tsx`，只跑任务实际新增/现有测试文件。
- [ ] **3. 数据合同。** deterministic fast path 在消息持久化之后、completed 发出前执行；completed.memoryCapture={count,undoOperationId,expiresAt}，撤销窗 24h。仅本地确定性规则进入该路径，模型 batch 不延迟 completed，不向终态 stream 追加事件。后台结果在 MemoryRecent snapshot 可见。
- [ ] **4. 组件。** current 支持 filter/search/hide/pin/correct/forget；recent 默认七天；review 显示来源并接受/拒绝/纠正；history 显示版本/有效期/generation diff；privacy 支持模型辅助、禁记类别、完整 export/import 和 prepare→确认→purge。
- [ ] **5. 授权 UI。** settings 扩展 capture policies/modelAssist，旧 schema 显式增字段后生成；公开请求不传 subject。异常映射稳定错误码，所有正文安全文本渲染。键盘、焦点返回、200% zoom、读屏 status 验证。
- [ ] **6. 绿测/typecheck/提交。** `npm --prefix web run typecheck`；提交 `feat(memory): deliver automatic memory center and undo`。

## Task 9：评测、发布门与交付证据（全部 MEM）

- [ ] **1. 建 corpus。** negative≥300、sensitive≥150、positive≥300、temporal_conflict≥150、recall_queries≥300；固定 seed/digest，敏感值全部合成，不把用户历史放 Git。明确标注 explicit_stable 与 inference。
- [ ] **2. Evaluator。** 新建 memory_fabric_eval_test.go，读取真实 service 输出逐条计算误存/漏存/kind+scope/旧版注入/Hit@5/Token/延迟；模型未配置的 case 为 INCOMPLETE，不能假 pass。旧实现和新实现使用相同 corpus/config/model。
- [ ] **3. 运行器。** 新建 scripts/eval-memory-fabric-v2.ps1；自身生成 UTC timestamp + GUID run ID，创建独立输出目录；Go evaluator 将 metrics.json 输出到显式环境路径。go test -json 的逐行日志保存 test-events.jsonl，绝不能叫 results.json 并删除换行。

~~~powershell
$runID = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ') + '-' + [guid]::NewGuid().ToString('N')
$outDir = Join-Path 'artifacts/memory-fabric-v2' $runID
New-Item -ItemType Directory -Path $outDir -ErrorAction Stop | Out-Null
$env:LUNITIDE_MEMORY_EVAL_DIR = (Resolve-Path $outDir).Path
go test -count=1 -json ./internal/m8app -run TestMemoryFabricEval |
    Out-File -Encoding utf8 (Join-Path $outDir 'test-events.jsonl')
if ($LASTEXITCODE -ne 0) { throw "Memory evaluation failed" }
~~~

- [ ] **4. Gate。** 敏感/跨 scope/固定问候即时查询误存为 0；explicit stable recall≥95%、kind+scope≥90%；当前纠正 100%、旧版当前注入 0；Hit@5≥旧基线，时间纠正子集提高 10pp（旧基线满分则持平）；Token 不超预算且回答质量不降。缺指标或数据不完整为 INCOMPLETE。
- [ ] **5. 验证。** 逐条执行 PRD 13.4 命令并检查退出码；新/旧 DTO、真实 bootstrap、migration、隐私/恢复演练均覆盖。运行脚本只能证明它实际测量的项目，DryRun 不能替代模型实测。
- [ ] **6. 提交/发布。** 记录 requirements→test→commit→artifact 映射到 docs/traceability/memory-fabric-v2.md，提交 `test(memory): record reproducible fabric release gates`。数据迁移/基础 read gate 通过后才启用 auto，不再要求用户逐条选择记忆。

## 依赖顺序与完成定义

Task1 → Task2 → Task4/7 的数据保护 → Task3 → Task5 → Task6 → Task8 → Task9。corpus/旧基线从 Task1 前开始，避免事后调指标。

完成必须有真实发布装配、数据迁移和回滚证据；每个 gate 有命令、退出码、fixture/config revision。本文已决定采用原生方案，实施时按任务顺序推进，无需再询问架构路线。尚未运行的功能测试保持未勾选。
