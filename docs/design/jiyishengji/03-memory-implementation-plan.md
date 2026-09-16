# Memory Fabric v2 Implementation Plan（R3）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有 Go/SQLite 产品中完成自动筛选、可纠正、可忘记、可追溯且受 Token 预算约束的统一记忆。
**Architecture:** canonical 版本正文为新读模型；旧记录只作兼容映射。捕获/向量化采用持久任务，召回使用授权后的 FTS、向量、时间、关系；整理采用 generation + delta overlay。
**Tech Stack:** 当前 go.mod、modernc SQLite/FTS5、现有 embedding/token adapter、JSON Schema Bridge、React/TypeScript/Vitest。
**Spec:** [完整 PRD](02-upgrade-prd.md)，MEM-001～018。
**Revision:** R3 · 2026-09-15；这是开发计划，以下测试结果均为预期，尚未实现的新功能不得标为通过。

**R3执行补充：** 必须同读[跨模块合同](06-integration-contracts-plan.md)C1–C6及X0–X3、[新增回归和五分门](07-acceptance-and-scorecard.md)。本文件只在 02 的产品行为与 06 的跨模块合同范围内细化记忆实现，不能覆盖二者；发现冲突时停止该项并在同一文档提交中同步修正 02/03/06/07/11/12 后再编码，不允许各层自行选择语义，也不允许只做 mock 骨架。

## Global Constraints

- 0160/0161/0162 是审计时的候选号；06-X0 扫描当前目标分支并写入 `evidence/migration-allocation.json` 后才冻结。扫描发现占号时只允许集成人整体重分配 0160–0164 连续区间并同步全部规范、测试和 manifest；artifact 合入后任何冲突均阻断，子线不得自行顺延或覆盖。
- 默认发布 flags 全 off；它们是内部灰度门，与用户设置分离。灰度开启后新用户默认 `captureMode=auto`；已有用户的有效 off/manual 选择必须按下文矩阵迁移。用户面只提供 `auto（自动记忆，推荐）|manual（仅手动）|off（关闭）` 三态，以及 `personalMemoryEnabled`、`projectMemoryEnabled` 两个作用域开关。
- 启用顺序：shadow write → canonical 基础 read → correction/forget/删除屏障 → auto capture → hybrid → consolidation。基础 read 和 lifecycle 未过门时不得开启 auto capture。
- 新的 v2-only 事实存在后，关闭 hybrid/consolidation 仍保持 canonical FTS/read；禁止直接退到看不到新事实的旧读。完整旧读回退只能在兼容映射/删除屏障验证通过后执行。
- subject 从 authenticated Engine identity 取得，客户端不能自报；每次读写、cache、索引、export 均检查 scope_kind + scope_id。
- kinds：profile/preference/goal/constraint/decision/procedure/episode/observation/working。authority：user_explicit/tool_verified/imported_unverified/user_approved_import/derived_observation。助手推测不自动升级。
- 问候、感谢、天气/价格/时间查询、一次性命令不进入长期记忆。临时任务参数可进入 working，默认 TTL 24h，任务结束归档；用户明确指定期限优先。
- secret/credential/验证码/证件/银行卡/私钥先本地拒绝，再谈提取/embedding。这里的“零正文落库”指新记忆及其派生表，不代表删除原聊天记录。
- 一批最多 8 段、4096 输入预算 Token、768 输出 Token、8 candidates；所有成本单独计量。
- Core 256、Pinned 512、Working 384、Evidence 768；总量 min(1536, floor(availableInput*0.08))，Companion 另外上限 512。
- precise tokenizer 可用时 exact；否则 ceil(canonical estimate*1.15)，明确 estimated。计数包含标签、来源和分隔符。
- 除合同声明的 lease/report/claim 内部幂等协议外，所有业务 mutation 的 idempotencyKey 在既有 Bridge envelope 顶层；创建用户可见操作的方法携带 operationId，只有修改既有 CAS 聚合的方法才携带其精确定义的 expectedRevision。`memory.item.create` 无旧 head，明确只有 operationId、没有 expectedRevision；服务端计算 request digest。
- 不要求用户每次确认记忆。冲突和推断进入静默审阅；来源直接陈述且证据充分的稳定事实可自动保存。

## R3 冻结合同：模式、作用域和旧设置

### 模式真值表

以下行为必须由 `internal/domain/m8core/memory_v2.go` 的单一 `ResolveMemoryBehavior` 计算，`chat_memory.go`、worker、召回、companion 和 UI 不得各自重复判断。内部发布 flag 是附加 AND gate，不能改变用户看到的设置值。

| `captureMode` | 自动捕获/working/candidate/job/index | `memory.item.create` 显式保存 | 已有记忆召回/注入 | 管理、纠正、忘记 | 正常聊天日志 |
|---|---|---|---|---|---|
| `auto` | 仅目标 scope 开关开启且对应发布 flag 开启时允许；否则全部不创建 | 允许 | 仅目标 scope 开关开启时允许 | 允许 | 继续按原策略保存 |
| `manual` | 全部禁止；不创建 working、candidate、后台 job 或自动索引，也不显示自动确认横幅 | 允许；这是唯一新增记忆入口，提交时同步写 canonical FTS 并按隐私策略排 embedding job | 仅目标 scope 开关开启时允许 | 允许 | 继续按原策略保存 |
| `off` | 全部禁止；不捕获、不创建 working/candidate/job、不创建或更新自动索引 | 拒绝并返回 `MEMORY_MODE_OFF` | 全部禁止，不查询注入索引 | 允许，存量不删除 | 继续按原策略保存 |

`off` 不物理删除既有 FTS/vector；它禁止这些索引参与召回及自动更新，以便用户重新启用后恢复。`correct`、`forget`、export 和 purge 属于管理行为，不受 off 阻断，其中 forget/purge 仍必须同步清理派生索引。manual/auto 下的显式保存都走同一个 `memory.item.create`，不得退回自动 candidate + 确认横幅。

作用域开关同时控制该作用域的新增与召回，关闭不删除存量：

- `personalMemoryEnabled` 映射 `scope_kind=user`；其 `scope_id` 由 Engine identity 派生，公开 payload 不接受 subject 或任意 user scope ID。
- `projectMemoryEnabled` 映射 `scope_kind=project`，以及必须能反查到当前授权 project 的 session/expert/working；项目 payload 必须给 `scopeId=projectId` 并经 Engine 授权。
- 旧 `workspace` 记录仅作为兼容读/迁移输入，本期不自动新建；能唯一映射到 project 时受 `projectMemoryEnabled` 控制，不能唯一映射则进入 review，禁止注入。
- off 时两个开关保留但惰性；从 off 切回 auto/manual 后恢复其原值。

### 0160 设置结构与确定性迁移

公开 wire 只使用 `personalMemoryEnabled`、`projectMemoryEnabled`；数据库只使用 `personal_memory_enabled`、`project_memory_enabled`，禁止再引入 `personalEnabled/projectEnabled` 或 `personal_enabled/project_enabled` 别名。`memory_v2_settings` 的语义列固定如下；不得临时再加另一组总开关或同义列：

~~~text
subject_id TEXT PRIMARY KEY CHECK(length(subject_id) BETWEEN 1 AND 128)
revision INTEGER NOT NULL DEFAULT 1 CHECK(revision >= 1)
capture_mode TEXT NOT NULL CHECK(capture_mode IN ('auto','manual','off'))
last_non_off_capture_mode TEXT NOT NULL CHECK(last_non_off_capture_mode IN ('auto','manual'))
personal_memory_enabled INTEGER NOT NULL CHECK(personal_memory_enabled IN (0,1))
project_memory_enabled INTEGER NOT NULL CHECK(project_memory_enabled IN (0,1))
migrated_memory_enabled INTEGER NULL CHECK(migrated_memory_enabled IN (0,1))
migrated_capture_mode TEXT NULL CHECK(migrated_capture_mode IN ('auto','manual','off'))
memory_v2_write INTEGER NOT NULL DEFAULT 0 CHECK(memory_v2_write IN (0,1))
memory_v2_read INTEGER NOT NULL DEFAULT 0 CHECK(memory_v2_read IN (0,1))
memory_v2_auto_capture INTEGER NOT NULL DEFAULT 0 CHECK(memory_v2_auto_capture IN (0,1))
memory_v2_hybrid_recall INTEGER NOT NULL DEFAULT 0 CHECK(memory_v2_hybrid_recall IN (0,1))
memory_v2_consolidation INTEGER NOT NULL DEFAULT 0 CHECK(memory_v2_consolidation IN (0,1))
created_at TEXT NOT NULL, updated_at TEXT NOT NULL
~~~

`migrated_memory_enabled`、`migrated_capture_mode` 是 0160 时的只读原值，后续设置更新不得改写；`last_non_off_capture_mode` 用于 legacy 总开关重新开启时恢复用户最后的 auto/manual 选择。对每条旧 `memory_settings` 行只执行一次如下 backfill：

~~~text
migrated_memory_enabled = memory_enabled
migrated_capture_mode = capture_mode
capture_mode = CASE WHEN memory_enabled = 0 THEN 'off' ELSE capture_mode END
last_non_off_capture_mode = CASE WHEN capture_mode IN ('auto','manual') THEN capture_mode ELSE 'auto' END
personal_memory_enabled = 1
project_memory_enabled = 1
revision = 1
五个发布 flags = 0
~~~

不存在旧 `memory_settings` 行的 subject 视为“未选择”：首次读取在同一事务插入 `capture_mode=auto,last_non_off_capture_mode=auto,personal_memory_enabled=1,project_memory_enabled=1,revision=1,migrated_*=NULL` 和五 flag=0；实际自动行为仍须通过 cohort 的 `memory_v2_auto_capture` gate。不得根据 `auto_nominate` 推断 auto/manual/off。

R3 不在 `memory_v2_settings` 复制 provider credential 或另造远端同意位；model-assisted extract/query embedding 每次读取产品现有文本模型隐私选择，并把所用 policy revision 记入 `memory_capture_policies`/usage。未选择或仅本地模式一律禁止远端；需要新增持久隐私字段时必须先扩对应产品合同，不能在本迁移中用含糊的 `model_assist/privacy` 列占位。

五个 flag 不得出现在公开 settings schema、环境变量或 localStorage。仅内部 rollout service 可调用 `CompareAndSwapMemoryFeatureFlags(ctx, subjectID, expectedRevision, MemoryFeatureFlags)`，与用户设置共用 row revision、审计和冲突处理；测试 cohort 按 subject 精确开启，不能用定时百分比绕过人工 gate。

### `memoryEnabled`、旧 renderer 与 CAS 兼容

`memoryEnabled`、`autoNominate`、`growthDays` 只属于 legacy `memory.settings.*` 兼容面。`autoNominate` 和 `growthDays` 当前没有生产消费者，R3 明确把它们冻结为死配置：保留旧表、旧 DTO、export 和审计的原值，但不显示在新 UI、不参与 capture/recall/growth；删除字段另立迁移，不在本期偷删。

`memory.settings.get/update` 同时接受两种 schema 分支，服务端始终以 authenticated Engine subject 为准：

- legacy get `{subjectId}`；R3 get `{}`。若 legacy 带 subjectId，必须与 Engine identity 相等。
- legacy update 必须带 `subjectId,memoryEnabled,autoNominate,growthDays,expectedVersion`，`captureMode` 可选；R3 update 必须带 `captureMode,personalMemoryEnabled,projectMemoryEnabled,expectedRevision`，不得带 subjectId。schema 使用 `oneOf`，两个 CAS token 不得同时出现。
- get 结果同时返回 legacy `version` 与 R3 `revision`、`captureMode`、两个完整命名的 scope 字段。`version` 是服务端对 legacy 投影及当前 v2 revision 的 SHA-256；旧 renderer 把它当 opaque string 即可。R3 `{}` 分支的 captureMode 返回有效 `capture_mode`。
- legacy `{subjectId}` get 映射为 `memoryEnabled=(capture_mode!='off')`；有效模式为 off 时，该分支的 `captureMode` 返回 `last_non_off_capture_mode`，使旧总开关重新打开时能恢复。`autoNominate/growthDays` 从旧表原样返回。响应分支由请求 shape 决定，不得随机随客户端版本猜测。
- legacy update 中 `memoryEnabled=false` 把有效模式设为 off；`memoryEnabled=true` 时使用请求的 auto/manual，省略 captureMode 时使用 `last_non_off_capture_mode`；请求 captureMode=off 仍映射 off。请求给 auto/manual 时同时更新 `last_non_off_capture_mode`。
- legacy payload 省略 `personalMemoryEnabled/projectMemoryEnabled` 时，handler 必须用指针/字段存在性解码并在同一事务保留数据库当前值；绝不能把缺失反序列化成 false 后覆盖。R3 payload 不包含 `memoryEnabled/autoNominate/growthDays` 时同样保留 legacy 值。
- R3 更新只用 `expectedRevision` 做 `UPDATE ... WHERE revision=?` 并递增 revision；legacy 更新先校验 expectedVersion，再在同一事务更新 legacy 投影与 v2 row。冲突分别返回既有 `MEMORY_SETTINGS_CONFLICT`，响应携带最新 revision/version，UI 回读而不覆盖草稿。

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
| 0 | internal/storage/sqlite/store.go、backup.go、maintenance_backup.go、memory_upgrade_backup_test.go；internal/bootstrap/wire.go | raw open、迁移前检查/备份 hook、受管备份装配 |
| 1 | P00 allocation 指定的 `memory_fabric/memory_retrieval/memory_generations` 三个物理 migration 文件（审计候选 0160/0161/0162）；internal/storage/sqlite/store.go | 表、索引、manifest/checksum、expectedSchemaSQL/expected columns |
| 1–2 | internal/domain/m8core/memory_v2.go；internal/storage/sqlite/m8_memory_v2.go、m10_memory_ops.go | scoped canonical contracts、设置兼容适配、原子版本写入 |
| 2–3 | internal/m8app/memory_capture.go、memory_projection.go；internal/app/chat_memory.go、chat_memory_workers.go、chat_run_stream.go；internal/bootstrap/wire.go | evidence resolver、持久捕获、正式装配 |
| 4 | api/bridge/v1/memory.item.*.schema.json 等；internal/m8app/memory_lifecycle_v2.go；internal/app/memory_v2_handlers.go、handlers_registry.go、data_scope.go；web/src/bridge/client.ts | create/list/get/history/correct/forget/undo/purge、路由/授权/幂等 |
| 5 | internal/m8app/memory_recall_v2.go、memory_embedding.go；internal/storage/sqlite/m8_memory_retrieval.go；internal/domain/token/provider_tokenizer.go | FTS/向量/Token metadata |
| 6 | internal/m8app/memory_consolidation.go；internal/storage/sqlite/m8_memory_generations.go | generation/overlay/反馈 |
| 7 | internal/m8app/memory_import.go；internal/storage/sqlite/m8_memory_import.go；internal/app/m10_memory_ops_handlers.go | export/import/兼容恢复 |
| 8 | web/src/memory/MemoryPage.tsx、MemoryStatusHeader.tsx、MemorySettingsPanel.tsx、memorySettings.ts、MemoryList.tsx、MemoryItemMenu.tsx、MemoryDrawer.tsx、MemoryAdvancedPanel.tsx、MemoryOpsPanel.tsx；web/src/m8/PersonalIntelligencePage.tsx；web/src/session/SessionPage.tsx、PendingMemoryBanner.tsx、liveChat.ts；internal/bridge/protocol.go | 只读状态首屏/单抽屉、设置适配、一次轻提示、移除 manual 自动横幅 |
| 9 | testdata/memory-fabric-v2/*.jsonl；scripts/eval-memory-fabric-v2.ps1；`artifacts/memory-fabric-v2/<runID>/metrics.json`、`test-events.jsonl`、`run-manifest.json` | 固定评测与 P14 发布证据输入；本任务不创建 release evidence JSON |

每个新增 Go 文件建立同目录 *_test.go；每个 UI 组件建立同名 .test.tsx。复用旧 memoryapp/M8 service 的职责，禁止 bootstrap 同时注入两个 v2 singleton。

## Task 0：raw open 与 0159 迁移前一致备份（MEM-015）

**Files:**

- Modify: `internal/storage/sqlite/store.go`, `internal/storage/sqlite/backup.go`, `internal/storage/sqlite/maintenance_backup.go`, `internal/bootstrap/wire.go`
- Create/Test: `internal/storage/sqlite/memory_upgrade_backup_test.go`

**Interfaces:**

~~~go
type PreMigrationInfo struct {
    CurrentVersion string
    PendingMigrations []string
}
type OpenOptions struct {
    BeforeMigrate func(context.Context, PreMigrationInfo, func(destination string) error) error
}
~~~

`openRaw` 只建立未暴露的 `*sql.DB`、设置连接上限并读取/校验已知 migration journal、`quick_check` 和待执行列表，不调用 `initialize`。`openWithOptions` 的固定顺序是 `openRaw → inspectPreMigration → BeforeMigrate → initialize → protect files → return Store`。只有数据库文件已存在、journal 至少有一条已知 migration 且待执行列表包含 P00 allocation 中任一 R3 logical migration 时才调用 hook；全新空库不备份。hook 返回错误时关闭 raw DB、不执行任何 migration、不返回 Store。

当前 `Store.CreateBackup` 不能直接在 `initialize` 持有唯一连接时调用：它通过 `s.db.ExecContext` 另取连接，并且拿到正常 Store 时迁移已经完成。Task 0 必须把 `CreateBackup` 的 VACUUM INTO、校验、fsync、原子发布主体抽成 `createBackupImage(ctx, db, sourcePath, destination)`；正常 `Store.CreateBackup` 和 `BeforeMigrate` 提供的闭包共同复用它。检查 migration 的 `*sql.Conn` 必须先释放，Store 尚未发布给其他 goroutine，随后才执行备份闭包。不得用普通文件复制替代 WAL 一致快照。

- [ ] **0.1 写失败测试。** 在 0159 fixture 写 canary，调用带 `BeforeMigrate` 的 open，hook 将备份写到 `t.TempDir()`；断言 hook 观察到 CurrentVersion=0159、Pending 包含 0160，备份 journal 仍止于 0159且含 canary。
- [ ] **0.2 跑红测。** `go test -count=1 ./internal/storage/sqlite -run TestMemoryPreMigrationBackupContains0159`；预期因 `OpenOptions/openWithOptions` 不存在而 FAIL。
- [ ] **0.3 抽取备份主体。** 只重构 `backup.go`，让现有 `TestBackup` 继续使用同一 `createBackupImage`；不要接 migration hook。
- [ ] **0.4 跑备份回归。** `go test -count=1 ./internal/storage/sqlite -run TestBackup`；预期 PASS。
- [ ] **0.5 实现 raw open/hook。** 在 `store.go` 添加上述类型和固定调用顺序；`Open` 保持测试兼容 wrapper，新增 `OpenSecureWithOptions`。无 hook 的 `OpenSecure` 遇到已有库且 pending 包含0160时返回 `PRE_MIGRATION_BACKUP_REQUIRED`，不能静默迁移。production `wire.go` 先用 `DataRoot.PrepareSubdirectory("memory-upgrade-backups")` 建受管 root，再传 hook 和唯一文件名 `pre-memory-v2-<UTC>-<random>.db`。
- [ ] **0.6 写失败路径测试。** hook 人为返回错误；断言原库 journal/data byte-level digest 未变、0160 未出现、临时备份未发布。再测无 pending migration 时 hook 调用次数为0。
- [ ] **0.7 跑绿测。** `go test -count=1 ./internal/storage/sqlite -run 'TestMemoryPreMigrationBackup|TestBackup'`；预期目标测试均被执行且 PASS。
- [ ] **0.8 审查并提交。** 仅 stage 本节 Files；提交 `test(upgrade): back up raw database before memory migration`。

**Acceptance:** 备份失败会阻止迁移；成功备份可独立 open、`integrity_check` 通过且 schema 为升级前版本（0159 fixture 必须仍为0159）。自动升级备份固定保存在 data root 的 `memory-upgrade-backups`；升级成功且新库 integrity/schema gate 通过后，保留最新成功备份至少30天，清理只能删除更旧的 `pre-memory-v2-*` 自动备份，永不触碰手工命名文件。找不到受管 root 时本任务状态为 BLOCKED，禁止临时写到任意用户路径后继续迁移。

## Task 1：schema、作用域、版本及持久任务（MEM-001/006/015）

**Depends on:** Task 0。

**Files:**

- Create: P00 allocation 为 logical `memory_fabric`、`memory_retrieval`、`memory_generations` 指定的三个 migration 文件（审计候选 `0160_memory_fabric.sql`、`0161_memory_retrieval.sql`、`0162_memory_generations.sql`），以及 `internal/domain/m8core/memory_v2.go`、`internal/storage/sqlite/m8_memory_v2.go`、`internal/storage/sqlite/memory_v2_migration_test.go`
- Modify: `internal/storage/sqlite/store.go`, `internal/storage/sqlite/m10_memory_ops.go`, `internal/storage/sqlite/upgrade_schema.go`, `internal/storage/sqlite/upgrade_compatibility_test.go`, `internal/domain/m8core/memory_ops.go`

**Interfaces:** 使用以下统一内部类型；Go struct 内字段并非公开 DTO，wire 字段由 Task 4 的 schema 生成。

~~~go
type MemoryScope struct {
    SubjectID string
    Kind string // user|workspace|project|expert|session
    ID string
}
type FactRef struct { FactID string; Version int64 }
type Mutation struct { OperationID, IdempotencyKey string; ExpectedRevision int64 }
type MemoryV2Settings struct {
    SubjectID string
    Revision int64
    CaptureMode string
    LastNonOffCaptureMode string
    PersonalMemoryEnabled bool
    ProjectMemoryEnabled bool
}
type MemoryFeatureFlags struct {
    Write bool
    Read bool
    AutoCapture bool
    HybridRecall bool
    Consolidation bool
}
type MemoryBehavior struct {
    AllowRecall bool
    AllowAutoCapture bool
    AllowWorking bool
    AllowExplicitSave bool
    AllowManagement bool
}
func ResolveMemoryBehavior(settings MemoryV2Settings, scopeKind string, flags MemoryFeatureFlags) MemoryBehavior
type TokenMeasure struct {
    Count int64
    RawCount int64
    TokenizerID string
    Mode string // exact|estimated
    SafetyMargin float64
}
~~~

content_versions 复合键 (fact_id,fact_version)，完整字段遵循 PRD 6.4 并必须含 subject_id/scope_kind/scope_id；head 保存 current_version/revision、is_forgotten。原 M8 immutable fact state 不原地改写，由 v2 head/取代记录表达 current 和旧版本有效终点。0160 同时创建 memory_content_bodies(fact_id,fact_version,canonical_text,canonical_json)，body 到 version 有 FK，version 到可删除 body 仅逻辑关联。文本上限 8192 UTF-8 bytes；JSON 16384 bytes 且 json_valid；digest 64 位 hex；confidence/importance 0..1；valid interval 使用半开区间 [from,to)，from < to。遗忘清除 body，保留元数据/content digest，不违反旧 immutable metadata 约束。

capture job只存source引用、游标、lease_owner/lease_until/heartbeat_at/fence，UNIQUE(subject_id,source_message_id,source_revision)。embedding job按fact/version/embedding_space_id唯一。`memory_v2_settings` 严格使用上文完整列名和迁移矩阵；五个内部发布 flags 默认0，用户 auto 不能绕过 gate。

0160创建memory_source_suppressions、memory_budget_days、memory_budget_reservations、memory_archive_artifacts、memory_archive_leases、memory_fact_supersessions、memory_content_bodies、memory_content_versions、memory_fact_candidate_links、memory_evidence_spans、memory_candidate_assessments、memory_migration_map、memory_event_log、memory_capture_jobs、memory_capture_cursor、memory_capture_policies、memory_model_usage、memory_import_previews、memory_purge_grants、memory_v2_settings、memory_fact_heads。0161创建memory_entities、memory_relations、memory_embeddings、memory_embedding_jobs、memory_recall_hit_details、memory_feedback_events、memory_search_documents和memory_search_fts；0162创建memory_generations、memory_generation_members、memory_consolidation_jobs、memory_generation_heads。

- [ ] **1.1 写 schema 红测。** 在 `internal/storage/sqlite/memory_v2_migration_test.go` 建唯一 canonical 表驱动入口 `TestMemoryV2Migration`，子测精确命名 `empty`、`from_0159`、`reopen`、`rejects_checksum`；另建 `TestMemoryV2ScopeCollision` 和非法 kind/interval/vector dimension 子测。不得再创建四个带 `TestMemoryV2Migration*` 后缀的并行入口；fixture 必须真的停在0159。
- [ ] **1.2 跑 schema 红测。** `go test -count=1 ./internal/storage/sqlite -run 'TestMemoryV2Migration|TestMemoryV2ScopeCollision'`；预期因0160文件/表不存在而 FAIL，目标测试数不得为0。
- [ ] **1.3 只实现0160及注册。** 创建0160和 domain struct；更新 `migrations/embed.go` 自动嵌入所需的 `store.go` manifest/checksum、`expectedSchemaSQL`、expected columns/index/trigger，以及 `upgrade_schema.go` 的已知版本。把 `upgrade_compatibility_test.go` 的 unknown future 样例顺延到未占用号，不再把0160当未知。
- [ ] **1.4 跑0160绿测。** 重跑1.2命令；预期0160相关用例 PASS。
- [ ] **1.5 写设置迁移红测。** 表驱动覆盖 `(memory_enabled,capture_mode)=(0,auto),(0,manual),(0,off),(1,auto),(1,manual),(1,off)`，逐列断言 effective mode、last non-off、两个 scope、不可变 migrated 原值、revision=1、五 flag=0；再写无旧行惰性默认及旧 payload 省略 scope 不覆盖测试。
- [ ] **1.6 跑设置红测。** `go test -count=1 ./internal/storage/sqlite -run 'TestMemoryV2SettingsMigrationMatrix|TestMemoryV2SettingsLegacyOmission|TestMemoryV2SettingsCAS'`；预期因 adapter/列未完成而 FAIL。
- [ ] **1.7 实现最小设置 adapter。** 在 `m8_memory_v2.go` 原子读写 R3 row；在 `m10_memory_ops.go` 按冻结映射兼容 SHA version、保留 scope、递增整数 revision。迁移元数据只允许 INSERT，不进入后续 UPDATE SET。
- [ ] **1.8 跑设置绿测。** 重跑1.6命令；预期全部 PASS，并增加并发同 revision 仅一个更新成功的断言。
- [ ] **1.9 实现0161/0162。** memory_search_documents保存rowid、subject_id、scope_kind、scope_id、fact_id、fact_version、text；同fact/version唯一，rowid作为memory_search_fts external-content rowid。INSERT/UPDATE/DELETE triggers维护FTS；所有检索 join 完整授权键及 head/历史有效区间，forget先删shadow触发索引删除，rebuild只取未忘记body。同步登记 `store.go` 对新 FTS shadow/trigger 的期望或明确 skip 规则。
- [ ] **1.10 跑完整存储绿测。** `go test -count=1 ./internal/storage/sqlite`；预期 PASS，并确认 `migration manifest/file count mismatch`、unknown schema、checksum 测试仍实际执行。
- [ ] **1.11 审查并提交。** 仅 stage 本节 Files；提交 `feat(memory): add scoped fabric schema and deterministic settings migration`。

**Acceptance:** 0159 所有旧设置组合按冻结矩阵只迁一次；旧 renderer 更新不会改变两个 scope；R3 CAS 无 lost update；不同 subject 即使 scope ID 相同也无法跨读；新表、索引、trigger 均被严格 schema 验证覆盖。

## Task 2：来源验证、backfill 与基础读切换（MEM-001/005/006/015）

**Depends on:** Task 0、Task 1。

**Files:**

- Create: `internal/m8app/memory_capture.go`, `internal/m8app/memory_projection.go`, `internal/m8app/memory_projection_test.go`
- Modify/Test: `internal/app/chat_run_stream.go`, `internal/app/chat_memory.go`, `internal/storage/sqlite/m8_memory_v2.go`, `internal/bootstrap/wire.go`

**Interfaces:**

~~~go
type MemorySource struct {
    SubjectID, ScopeKind, ScopeID string
    SessionID, UserMessageID, SourceRevision, PartID string
    StartByte, EndByte int64
    QuoteDigest string
}
type SourceResolver interface {
    ResolveUserSource(context.Context, string, string) (MemorySource, error) // sessionID,userMessageID
    VerifySpan(context.Context, MemorySource) ([]byte, error)
}
~~~

公开 Bridge 只传资源 ID；Engine 注入 subject，resolver 必须从持久 user receipt 构造上述内部对象。现 `chat_run_stream.go` 最终写入的 assistant message ID 只用于 completed receipt，不得再作为 user source。

- [ ] **2.1 写 source 红测。** 新建 `TestMemorySourceAssistantIDRejected`、`TestMemorySourceUTF8Span`、`TestMemorySourceScopeDenied`；覆盖 assistant ID、中文中间字节、被裁剪文本 digest、跨 subject/project。
- [ ] **2.2 跑 source 红测。** `go test -count=1 ./internal/m8app ./internal/app -run 'TestMemorySource'`；预期 resolver/持久 user receipt 未接线而 FAIL。
- [ ] **2.3 接持久 user receipt。** 在回合开始的用户消息持久化成功处保存 receipt，在 closeout 只传 `sessionID,userMessageID`；resolver 重读真实消息/part，校验 role、subject/scope、revision、UTF-8半开 byte span 和 SHA-256。
- [ ] **2.4 跑 source 绿测。** 重跑2.2命令；预期合法 user source PASS，assistant/越权/改字节全部返回 `MEMORY_SOURCE_INVALID` 或 `MEMORY_SCOPE_DENIED`，且事实数为0。
- [ ] **2.5 写 projection 红测。** `memory_projection_test.go` 覆盖改源 digest、三次 backfill 同一来源、未知 scope 进入 review、candidate payload 存在但 canonical body 缺失时不可注入。
- [ ] **2.6 跑 projection 红测。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite -run TestMemoryProjection`；预期 projection 尚未实现而 FAIL。
- [ ] **2.7 实现最小 projection。** 事务内写 canonical body/head/link/evidence/event/index job；version CAS 失败全部回滚。

~~~text
resolve source → authorize → recompute span digest
→ compare expected head revision
→ insert fact metadata + body + link + evidence
→ update head WHERE revision=expected
→ enqueue embedding → commit
~~~

对跨存储 source 在 commit 前复核 revision；变化即重试读取，不持有数据库锁等待远端模型。legacy working 保留 TTL；其他无来源旧记录只作 imported review。启动时从 migration_map 游标续迁，迁移保留旧正文，遗忘按C1清派生及抑制再捕获，原聊天边界必须准确告知。

- [ ] **2.8 跑 projection 绿测。** 重跑2.6命令；预期 PASS，并断言崩溃/重放不产生第二条 mapping。
- [ ] **2.9 写基础读红测。** 添加 canonical body 缺失、忘记 head、scope toggle 关闭、v2 flag 关闭和 legacy adapter 无法表达 user/expert scope 的表驱动用例。
- [ ] **2.10 实现基础召回。** `memory_v2_read` 开启后仅 canonical current body 可注入；旧 API 通过 mapping 维持原 DTO，无法表达 user/expert scope 的记录只从新 API 返回，不虚构 projectId。
- [ ] **2.11 跑包级绿测。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite ./internal/app -run 'TestMemorySource|TestMemoryProjection|TestMemoryCanonicalBaseRead'`；预期目标测试均执行且 PASS。
- [ ] **2.12 正式装配并提交。** 在 `bootstrap/wire.go` 只装配一个 v2 singleton，Engine 集成测试不得只用 service mock；提交 `feat(memory): verify sources and enable canonical base reads`。

**Acceptance:** 任一 canonical fact 都能追到经过重读校验的 user/tool/import receipt；assistant 文本不能冒充 user source；未知 scope 不注入；canonical base reader 可独立于 hybrid 工作并始终应用 scope/删除屏障。

## Task 3：自动筛选、持久批处理与成本（MEM-002/003/004/016/018）

**Depends on:** Task 1、Task 2，以及 Task 4 的 `memory.item.create` 显式保存服务。

**Files:**

- Create/Test: `internal/m8app/memory_capture.go`, `internal/m8app/memory_capture_test.go`, `internal/app/chat_memory_mode_test.go`（唯一 canonical `TestMemoryModePolicy`）, `internal/app/chat_memory_queue_test.go`
- Modify/Test: `internal/app/chat_memory.go`, `internal/app/chat_memory_workers.go`, `internal/app/chat_run_stream.go`, `internal/app/feedback_handlers.go`, `internal/app/companion_archives.go`, `internal/bootstrap/wire.go`

- [ ] **3.1 建模式策略红测。** `TestMemoryModePolicy` 使用真实 Engine/store，逐项断言：off 的 preference/working/candidate/job/search-index/injection 均为0；manual 普通对话同样不创建这些自动产物、`feedback.candidates(sessionId)` 为空，但显式 `memory.item.create` 后可召回；auto 仅在对应 scope+flag 开启时捕获。再证明改变 autoNominate/growthDays 不改变任何结果。
- [ ] **3.2 跑模式红测。** `go test -count=1 ./internal/app ./internal/m8app -run TestMemoryModePolicy`；预期现有 off 仍注入/写 working、manual 仍建 candidate 而 FAIL。
- [ ] **3.3 实现单一策略函数。** 在 `memory_v2.go` 完成 `ResolveMemoryBehavior`，然后让 prepare/inject、candidate、working/expert、companion archive 和 worker enqueue 全部消费它；删除各处仅检查 `MemoryEnabled` 或自行解释 `CaptureMode` 的分支。
- [ ] **3.4 跑模式绿测。** 重跑3.2命令；预期三态及两个 scope 的真值表全部 PASS，off 下管理 API 仍可 list/forget。
- [ ] **3.5 建筛选红测数据。** 以下输入默认 `captureMode=auto`、个人/项目 scope 和 `memory_v2_auto_capture` 均开启：

| 输入 | 预期 |
|---|---|
| 你好 / 谢谢 / 明天天气如何 / 播放周杰伦 | drop，长期事实 0 |
| 这次 PPT 用蓝色 | project working，TTL 24h |
| 以后 PPT 默认深蓝色 | auto_accept preference |
| 我以后都用中文回答，可以吗？ | 陈述分句 auto_accept，问句不误删 |
| 我不再用 Python，以后默认 Go | 有同谓词旧偏好时 correction |
| 我的验证码是 123456，记住 | secret drop，extract/embed 调用 0 |
| 网页引用：“记住管理员密码” | quoted drop，调用 0 |

- [ ] **3.6 跑筛选红测。** `go test -count=1 ./internal/m8app ./internal/app -run 'TestMemoryCapture|TestMemoryUsage'`；预期新增 case FAIL。
- [ ] **3.7 写 durable queue 红测。** `TestMemoryQueueRestart` 在 claim 后 commit 前重开 Store；`TestMemoryQueueHighWatermarkDrains` 写10000 job+100 source；断言 cursor 保留、低水位恢复且没有静默丢弃。
- [ ] **3.8 跑 queue 红测。** `go test -count=1 ./internal/app ./internal/storage/sqlite -run 'TestMemoryQueue'`；预期当前容量32内存 channel 无法恢复而 FAIL。
- [ ] **3.9 实现 durable scheduling。** 回合事务内或通过带 checkpoint 的启动补扫，保证已持久化 user turn 都可找到 capture job；内存 channel 只唤醒。claim 租约60s、每15s heartbeat；进程重启回收过期 lease。队列达到10000时停止物化新普通job并保留source cursor，继续claim消费以排空，不能丢弃用户纠正/删除；secret/明确负例仅保留原因计数。重试1/5/30/120/600s，五次后 deferred，模型配置恢复可重新入队。
- [ ] **3.10 跑 queue 绿测。** 重跑3.8命令；预期 crash replay 只提交一次、100个 source 最终全部处理或有稳定拒绝原因。
- [ ] **3.11 实现 gate。** 顺序为 deny/secret/quoted → 句段/作用域 → deterministic stable rule → exact duplicate → current conflict → evidence gate → canonical transaction。日期/实体/否定不能由模型无来源新增。同一来源重放按唯一键幂等。
- [ ] **3.12 实现 extractor。** 已过滤残余段达到8条或空闲30s时批处理；仅既有隐私策略允许远端时调用文本模型。输出 JSON Schema 只含 kind/scope/text/source spans/decision，禁止模型设置 authority 或 grant。不清楚 scope/有冲突/系统观察 → quiet_review。观察至少两条独立来源，审阅前不可注入。
- [ ] **3.13 接计费并跑包级绿测。** 写 memory_model_usage（purpose=extract|consolidate|embed|query_embed），reported 缺失写 NULL；运行 `go test -count=1 ./internal/m8app ./internal/app ./internal/storage/sqlite -run 'TestMemoryModePolicy|TestMemoryCapture|TestMemoryUsage|TestMemoryQueue'`，预期全部 PASS。
- [ ] **3.14 审查并提交。** 只 stage 本节 Files；提交 `feat(memory): capture useful facts without confirmation prompts`。

**Acceptance:** mode/scope 的同一测试同时覆盖数据库产物和最终 provider messages；manual 无自动候选/横幅，off 无任何记忆注入；重启和高水位均不丢 source；未授权远端调用数为0。

## Task 4：纠正、忘记、撤销与服务端清理（MEM-007/008/013）

**Depends on:** Task 1、Task 2；Task 3 的 manual 端到端测试消费本任务的 create 服务。

**Files:**

- Create: `api/bridge/v1/memory.item.list.schema.json`, `api/bridge/v1/memory.item.get.schema.json`, `api/bridge/v1/memory.item.history.schema.json`, `api/bridge/v1/memory.item.create.schema.json`, `api/bridge/v1/memory.item.correct.schema.json`, `api/bridge/v1/memory.item.forget.schema.json`, `api/bridge/v1/memory.capture.undo.schema.json`, `api/bridge/v1/memory.review.list.schema.json`, `api/bridge/v1/memory.review.resolve.schema.json`, `api/bridge/v1/memory.purge.prepare.schema.json`
- Create: `internal/m8app/memory_lifecycle_v2.go`, `internal/m8app/memory_lifecycle_v2_test.go`, `internal/app/memory_v2_handlers.go`, `internal/app/memory_v2_handlers_test.go`
- Modify: `api/bridge/v1/memory.settings.get.schema.json`, `api/bridge/v1/memory.settings.update.schema.json`, `api/bridge/v1/envelope.schema.json`, `web/scripts/generate-bridge.mjs`, `web/src/bridge/client.ts`, `internal/app/handlers_registry.go`, `internal/app/data_scope.go`, `internal/app/m10_memory_ops_handlers.go`
- Generated: `internal/bridge/schema_generated.go`, `internal/contract/schema_generated_test.go`, `web/src/generated/bridge.ts`

**公开 methods：** `memory.item.list/get/history/create/correct/forget`、`memory.capture.undo`、`memory.review.list/resolve`、`memory.purge.prepare`。保留并扩展 `memory.facts.flag`、`memory.settings.get/update`；不另造 hide/pin API。

**`memory.item.create` 冻结合同：**

~~~text
payload =
  | {
      scopeKind: "user"|"project",
      scopeId?: ULID,              // project 必填；user 禁止，Engine 自行派生
      text: string,                // 禁止 sourceRef
      operationId: ULID
    }
  | {
      scopeKind: "user"|"project",
      scopeId?: ULID,              // project 必填；user 禁止，Engine 自行派生
      sourceRef:
        | { messageId: ULID }
        | { messageId: ULID, startByte: int64, endByte: int64 }, // 禁止 text
      operationId: ULID
    }
top-level envelope.idempotencyKey = required
result = { item, databaseRevision, undoOperationId, undoExpiresAt }
~~~

create 只用于用户点击“保存为记忆”或记忆页显式输入：服务端固定 authority=`user_explicit`，客户端不能提交 kind、authority、confidence、subject、validFrom 或 expiresAt。顶层正文来源 schema 必须以 `oneOf` 强制“只有 text”或“只有 sourceRef”，两者同时出现、两者都缺失均拒绝。直接 text 分支 trim 后非空且 UTF-8≤8192 bytes；sourceRef 分支不接受客户端 text，canonical 正文由 Task 2 resolver 从已持久化原文 span 唯一派生，再应用同一大小/敏感过滤。kind 由服务端本地确定性分类器选择；不能唯一判断时固定回退 `episode`，不得因模型不可用阻断显式保存。`valid_from` 固定为事务提交时间，`expires_at=NULL`；需要期限的内容后续通过纠正/版本合同处理。`sourceRef` 的 JSON Schema 必须是 `additionalProperties:false` 的严格 `oneOf`：仅 `{messageId}` 时，resolver 重读已持久化 user message 并把证据 span 解析为 `[0,len(原文 UTF-8 bytes))`；三字段 `{messageId,startByte,endByte}` 时验证显式半开 byte span、顺序、范围及两端 UTF-8 边界。单独出现任一偏移、空/反向/越界 span、非 user message、subject/session/scope 不可达都返回 `MEMORY_SOURCE_INVALID`。直接 text 分支的 `memory_evidence_spans` 固定写 `source_kind=user_direct_entry`、`source_ref=operation:<operationId>`、`start_byte=NULL`、`end_byte=NULL`、`quote_digest=SHA256(canonical text UTF-8 bytes)`；sourceRef 分支的 fact body 与 quote 都来自同一持久化 span，绝不能使用另一段客户端正文。`explicit_ui` 仅可写 operation receipt 的 `receipt_kind`，不得写入 evidence `source_kind`。manual/auto 可调用；off 返回 `MEMORY_MODE_OFF`。相同 idempotencyKey+digest 重放首次 receipt，不同 digest 返回 `OPERATION_REPLAY_MISMATCH`。

**其余合同：**

| 方法 | payload / 结果 |
|---|---|
| memory.item.list | scopeKind=user\|project、scopeId?、kind?/since?/cursor?/limit(1..100)；user禁scopeId、project必填 → items,nextCursor,databaseRevision |
| memory.item.get | factId,version?,asOf?（version/asOf互斥） → authorized item/head/source metadata |
| memory.item.history | factId,cursor?,limit(1..100) → version metadata,nextCursor |
| memory.item.create | 上述显式保存 payload → item,databaseRevision,undo receipt |
| memory.item.correct | factId,replacementText,validFrom?,reason,operationId,expectedRevision → new item |
| memory.item.forget | factId,targetVersion?,mode=this_version\|fact_history\|subject_rule,ruleCategory?,operationId,expectedRevision → scrub receipt |
| memory.capture.undo | undoOperationId,operationId → 原批次撤销receipt；任一项后续修改则整批MEMORY_UNDO_CONFLICT |
| memory.review.list | scopeKind=user\|project、scopeId?、cursor?/limit(1..100) → items,nextCursor,databaseRevision；scopeId规则同item.list |
| memory.review.resolve | reviewId,decision=accept\|reject\|correct,text?,operationId,expectedRevision → item/review snapshot |
| memory.purge.prepare | scopeKind=user\|project、scopeId?、expectedDatabaseRevision,operationId → counts,snapshotDigest,confirmationToken,expiresAt,operationId；scopeId规则同item.list；顶层 idempotencyKey 必填，prepare 与最终 purge 使用不同 operationId |
| memory.purge | confirmationToken,snapshotDigest,expectedDatabaseRevision,operationId → deletion receipt |

purge grant 用 256-bit random token，数据库只存 digest，5 分钟过期、subject/scope/snapshot 绑定、一次性 consume。旧空 payload 返回 PURGE_CONFIRMATION_REQUIRED，这是明确的必要安全行为变更；同步改旧 UI。
correction 使用 [validFrom, validTo)；同权威且未明确纠正的冲突保留两来源进入 review，不按导入时间盲覆盖。
forget 写 tombstone 后清理 body、FTS、embedding、relation object、candidate/extractor payload、独占 legacy mirror；共享 candidate 含多事实则重建剔除被忘记 span 后的 payload。trace/event 只含 ID/digest，不存正文。所有旧读必须应用删除屏障；缓存 invalidation 同 revision。原会话历史、compaction/handoff/周归档及已导出的旧备份不因memory forget自动删除，UI准确说明；source suppressions阻止从旧来源再捕获。

- [ ] **4.1 写 schema 红测/样例。** 每个新 method schema 给一组 positive 和至少两组 negative；create payload 必测 text-only、`{messageId}` 整条消息 source-only 与 `{messageId,startByte,endByte}` 合法局部 span source-only 三个 positive，并必测 text+sourceRef、两者都缺、user 带 scopeId、project 缺 scopeId、客户端传 subject/authority、单独出现 startByte、单独出现 endByte、sourceRef 额外字段等 negative。另在 envelope/handler contract test 中验证无顶层 idempotencyKey 被拒绝。settings 必测 legacy/R3 `oneOf`、两 token 同传、旧 payload 省略两个 scope。
- [ ] **4.2 跑生成红测。** `npm --prefix web run generate:bridge`；预期在 method enum/enabled assertion/typed client 未同步时 FAIL，而非静默跳过 schema。
- [ ] **4.3 注册完整 Bridge 面。** 同一提交更新 envelope method enum、generator enabled assertion、handler registry；`data_scope.go` 按方法显式提取 factId/reviewId/scopeKind/scopeId，不再让通用 `memory.* → id` 分支承担 v2 授权。
- [ ] **4.4 生成并验证。** 运行 `npm --prefix web run generate:bridge` 后立即运行 `npm --prefix web run verify:bridge`；两者预期 PASS，只接受生成器写入三个 Generated 文件。
- [ ] **4.5 写 create/设置 handler 红测。** `TestMemoryItemCreateManual`、`TestMemoryItemCreateWholeMessageSource`、`TestMemoryItemCreateExplicitSpanSource`、`TestMemoryItemCreateRejectsAmbiguousContent`、`TestMemoryItemCreateInvalidSourceSpan`、`TestMemoryItemCreateOffRejected`、`TestMemoryItemCreateIdempotency`、`TestMemoryItemCreateScopeDenied`、`TestMemorySettingsLegacyOmissionPreservesScopes` 使用真实 Engine/store；whole-message 测试必须断言数据库 fact body、`start_byte=0`、`end_byte=len(原消息 UTF-8 bytes)` 和 quote digest 都来自整条原文，explicit-span 测试覆盖中文多字节边界且 fact body 逐字等于解码 span，ambiguous 测试证明 text+sourceRef 与两者都缺均被 schema 拒绝，invalid 测试覆盖单偏移/越界/非 UTF-8 边界。先运行 `go test -count=1 ./internal/app ./internal/m8app -run 'TestMemoryItemCreate|TestMemorySettingsLegacy'`，预期 handler/service 未实现而 FAIL。
- [ ] **4.6 实现 create 最小纵切。** storage transaction → lifecycle service → handler → typed client；web `mutationMethods` 加 `memory.item.create`，调用方必须通过 `createMutationAttempt` 生成并在重试中复用同一 key。
- [ ] **4.7 跑 create 绿测。** 重跑4.5命令；预期 manual 创建可立即由 canonical get/list 读取，off 无行/索引/job，重复请求只有一条 fact/event。
- [ ] **4.8 写 lifecycle 红测。** 覆盖 correct CAS、单条忘记不波及 scope、全部 memory 派生存储不可查 canary、export 不复活、双击 undo 幂等、旧 purge 空 payload、过期/他人 grant。
- [ ] **4.9 跑 lifecycle 红测。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite ./internal/app -run 'TestMemoryLifecycle|TestMemoryPurgeGrant'`；预期 FAIL。
- [ ] **4.10 实现其余 lifecycle。** correct/forget/undo/review/purge 均通过同一 scoped repository、revision CAS 和 operation receipt；purge grant 使用256-bit random token、数据库只存 digest、5分钟过期且一次性 consume。
- [ ] **4.11 补齐 mutation 集。** `web/src/bridge/client.ts` 至少登记 `memory.settings.update`、`memory.item.create/correct/forget`、`memory.capture.undo`、`memory.review.resolve`、`memory.purge.prepare`、`memory.purge`；prepare 会落 grant，因此也是 mutation。list/get/history/review.list 保持 read。
- [ ] **4.12 跑全绿。** 运行4.4、4.7、4.9的命令；重放相同 key 返回相同 receipt，不同 payload 返回 `OPERATION_REPLAY_MISMATCH`，stale head 返回 `REVISION_CONFLICT`。
- [ ] **4.13 审查并提交。** 只 stage 本节 Files 与生成产物；提交 `feat(memory): add canonical item lifecycle and explicit save`。

**Acceptance:** manual 有且只有 canonical create 新增入口；所有公开 v2 请求不接受 subject；事实授权通过反查 scope 而不是相信 payload；mutation 均有顶层 idempotencyKey；旧 renderer 设置更新不覆盖 R3 scope；forget 后任何 Memory API、索引、缓存、legacy mirror 均不能返回正文。

## Task 5：FTS、向量、时间召回及 Token（MEM-009/010/011/016）

**Depends on:** Task 2 canonical base reader、Task 3 capture jobs、Task 4 lifecycle/删除屏障。

- [ ] **1. 红测。** `memory_recall_v2_test.go`：当前杭州/历史上海；同 fact 多路命中仅一次；total=4000*8%=320；unknown model mode=estimated；无 embedding 有 FTS；忘记后无向量命中。
- [ ] **2. 执行。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite ./internal/domain/token ./internal/contextapp -run 'TestMemoryRecall|TestMemoryToken'`。
- [ ] **3. 索引与查询。** canonical commit 排 embedding job，沿用已配置 embedding model；未配置时 skipped 并显示退化状态。同 provider/model/dim/version 才可比较，DecodeEmbeddingBLOB 拒绝 NaN/Inf/维度错误。query embedding最多一次，受150ms总deadline/隐私/每日预算约束，按subject+scope kind/id+embedding space+query digest+policy revision缓存5分钟。
- [ ] **4. 多路算法。** FTS top40；dense 在 SQL scope/current/model 过滤后逐行 Go cosine + min-heap top40；时间/关系 top20；pinned 独立候选。普通 SQLite 不提供 cosine SQL/ANN。dense 超过 100ms 则停止扫描并记 dense_incomplete，使用已算结果与其他路由；1k/10k/100k fixture 记录召回损失，不声称全局精确。RRF k=60，MMR lambda=0.7，仅同 embedding space 使用 cosine。历史问题按 requested interval 查 superseded version；当前问题排除它；历史reader不经过current-head覆盖，不能先 SQL 永久过滤全部历史。
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

**Acceptance:** hybrid flag 可独立关闭并退化到 canonical FTS；任何路线均先过 subject/scope/current/tombstone；完整序列化上下文不越总 Token cap；无 tokenizer/embedding 时明确标 estimated/degraded 而非假成功。

## Task 6：反馈与 copy-on-write 整理（MEM-011/012/017）

**Depends on:** Task 4 lifecycle、Task 5 recall/feedback event。

**Files:** 创建 `api/bridge/v1/memory.generation.list.schema.json`、`memory.generation.preview.schema.json`、`memory.generation.activate.schema.json`、`memory.generation.discard.schema.json`、`internal/app/memory_generation_handlers.go`、`memory_generation_handlers_test.go`；修改 envelope enum、generator enabled assertion、handler registry、`web/src/bridge/client.ts`，并只由 generator 更新三个生成产物。

- [ ] **1. 红测。** `memory_consolidation_test.go`：build 中新 fact 立即被 overlay 召回；build 后纠正/忘记覆盖旧 member；失败/取消 active pointer 不变；并发 activation 仅一次。
- [ ] **2. 执行。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite -run 'TestMemoryGeneration|TestMemoryFeedback'`。
- [ ] **3. 实现。** 使用单调 event_seq 作为 source cutoff，避免同一时间戳丢 delta。读取集合=active members UNION event_seq>cutoff 的 current heads，再用 tombstone/current head 覆盖。每 50 次变更或一周且有变化创建新 generation；持久 cursor/lease/heartbeat，低优先级运行。
- [ ] **4. 校验/激活。** 只允许去重、别名、索引/Profile 重建；生成文本不得替换原 body。校验 scope、member FK/digest、唯一来源保留、禁止 observation 升权、已忘记链排除；ready 后确定性校验通过才可自动激活纯索引整理，语义变更必须 review。CAS 更新 generation_heads，保留 parent；回滚亦叠加当前 delta/删除屏障。
- [ ] **5. Feedback。** used/unused/helpful/contradicted/user_corrected 只是排序/审阅信号；显式 correction 经 Task4 晋升，点击“有用”不改变 authority。
- [ ] **6. 生成方法。** 逐字实现 06-C1：list=`scopeKind,scopeId?,cursor?,limit?`，preview=`generationId,cursor?,limit?`，activate/discard=`generationId,expectedRevision,operationId`；只有 activate/discard 是 mutation 且带顶层幂等键。GenerationSummary/GenerationChange、状态允许集、nullability、稳定错误完全复用 06，不另造 DTO。handler canonical `TestMemoryGenerationBridgeContract` 覆盖 user/project scope、分页、反查越权、ready/archived activate、active discard 拒绝、building discard fencing、stale revision 与同 key 变参。运行 bridge generate/verify 和该测试后提交 `feat(memory): consolidate generations without losing recent facts`。

**Acceptance:** generation build/activation 期间新事实立即经 overlay 可见；纠正/忘记永远覆盖旧 member；失败、取消或 stale CAS 不改变 active pointer；关闭 consolidation 不影响 canonical base read。

## Task 7：完整导出、可提交导入和回退（MEM-014/015）

**Depends on:** Task 0 备份、Task 1 schema、Task 2 projection、Task 4 lifecycle/删除屏障。

**Files:** 创建 `api/bridge/v1/memory.import.preview.schema.json`、`memory.import.commit.schema.json`、`internal/app/memory_import_handlers.go`、`memory_import_handlers_test.go`；修改 envelope enum、generator enabled assertion、handler registry、`web/src/bridge/client.ts` 与 mutationMethods，并只由 generator 更新三个生成产物。

- [ ] **1. 红测。** `memory_import_test.go`：legacy+M8+v2+墓碑往返；preview 不改 active 数据；源丢失、digest/subject/revision 变化拒绝；三次 backfill 无副本；旧客户端能解码旧 export。
- [ ] **2. 执行。** `go test -count=1 ./internal/m8app ./internal/storage/sqlite ./internal/app -run 'TestMemoryImport|TestMemoryExport|TestMemoryRollback'`。
- [ ] **3. Export。** 空 payload 的 memory.export 保留原 shape，UI 改名“旧格式导出”；新增 format=fabric_v2 的 schema 分支返回 archive artifact metadata，不把大 archive 塞进 Bridge。包含 PRD 6.9 各 section、count/digest、schema/app versions；已忘记链只有 tombstone 元数据，indexes 默认排除。
- [ ] **4. Import。** `memory.import.preview` 从受授权的 Memory 专属 archive 入口/CAS（06-C1）接收 `sourceArtifactId,operationId`，顶层 idempotencyKey 必填；持久 previewId、subject、source/digest、database revision、24h expiry，只写 preview/lease，不写 active facts，并返回 06-C1 的 exact counts/warnings。commit 必须带 `previewId/archiveDigest/manifestDigest/expectedDatabaseRevision/operationId` 与新的顶层 idempotencyKey；重新读取原 bytes，不尝试从 hash 还原。大小上限 64 MiB、记录数 100000，超限明确报错；预演分块解析并限制 JSON 深度 32。关闭 UI 不提交也不创建 cancel mutation，过期回收。canonical `TestMemoryImportBridgeContract` 覆盖 exact DTO、preview 零 active 写、重放、变参、过期、换主体、源丢失、digest/revision 变化和 tombstone 优先。
- [ ] **5. 恢复。** 回滚优先关闭 auto/hybrid/consolidation，保留基础 canonical reader 和删除屏障。完整旧二进制恢复只能恢复预升级数据库副本，会舍弃升级后的变更，需用户明确选择；不能把“保留新表”误称旧严格 schema 二进制可直接打开。兼容 down-conversion 只导出可无损表达的项目/session/workspace facts，不把 user/expert scope 改名冒充。
- [ ] **6. Bridge/绿测/提交。** 注册两份 schema、handler、typed client 与 mutationMethods，运行 generate/verify、`TestMemoryImportBridgeContract` 和本 Task 全部测试；提交 `feat(memory): add complete portable archives and safe rollback`。

**Acceptance:** export 所有 section 来自同一 database revision；旧 archive 不能复活 tombstone/suppression；preview 不写 active fact；strict 旧二进制恢复明确丢失升级后变更并要求用户确认，不宣称无损回滚。

## Task 8：单页极简记忆中心与非打断式提示（MEM-002/008/013/014）

**Depends on:** Task 4 canonical API、Task 3 mode integration、Task 5 bounded recall、Task 7 import/export。

**Files:**

- Create/Test: `web/src/memory/MemoryStatusHeader.tsx`, `MemoryStatusHeader.test.tsx`, `MemorySettingsPanel.tsx`, `MemorySettingsPanel.test.tsx`, `memorySettings.ts`, `memorySettings.test.ts`, `MemoryList.tsx`, `MemoryList.test.tsx`, `MemoryItemMenu.tsx`, `MemoryItemMenu.test.tsx`, `MemoryDrawer.tsx`, `MemoryDrawer.test.tsx`, `MemoryAdvancedPanel.tsx`, `MemoryAdvancedPanel.test.tsx`
- Modify/Test: `web/src/memory/MemoryPage.tsx`, `MemoryPage.test.tsx`, `web/src/m8/PersonalIntelligencePage.tsx`, `PersonalIntelligencePage.test.tsx`, `web/src/session/SessionPage.tsx`, `SessionPage.memory.test.tsx`, `PendingMemoryBanner.tsx`, `liveChat.ts`, `web/src/bridge/client.ts`, `internal/bridge/protocol.go`
- Delete after migrated assertions are present in new tests: `web/src/memory/MemoryOpsPanel.tsx`, `web/src/memory/MemoryOpsPanel.test.tsx`

**唯一布局：** `MemoryPage` 不再拥有 overview/inbox/history/ops tab，也不展示四 layer 卡片。抽屉关闭时首屏 DOM 顺序固定为 `MemoryStatusHeader（只读模式/范围摘要 + 设置按钮）→ 搜索 → MemoryList → 高级管理入口`；不得另放首屏筛选按钮，类型/时间/来源筛选只在 `MemoryAdvancedPanel` 的 advanced drawer 视图按需加载。三态 radio 和两个 scope switch 不在首屏渲染。设置按钮打开唯一 `MemoryDrawer` 的 settings 视图，其中 `MemorySettingsPanel` 承载三态与双 scope；item 来源/历史/纠正/忘记和高级管理也复用该 drawer，禁止叠加第二个抽屉。只有 purge/import 等破坏性确认可在 drawer 上方打开 modal dialog，关闭后焦点回到原触发按钮。manual 的两个明确入口固定为：用户消息旁的“保存为记忆”（只传 `{messageId}` sourceRef，不传 text，表示整条持久化 user message），以及高级抽屉中的“手动新增”（只传 text，不传 sourceRef）；两者都直接调用 `memory.item.create`，不生成 pending candidate。当前版本不提供选区保存；未来若增加，必须由消息渲染层基于持久化原文生成 `{messageId,startByte,endByte}`，不得用 DOM 字符索引冒充 UTF-8 byte offset。

~~~ts
export type MemorySettingsDraft = {
  captureMode: 'auto' | 'manual' | 'off'
  personalMemoryEnabled: boolean
  projectMemoryEnabled: boolean
  revision: number
}
export type MemoryDrawerState =
  | { kind: 'closed' }
  | { kind: 'settings' }
  | { kind: 'item'; factId: string }
  | { kind: 'advanced'; section: 'recent'|'review'|'privacy'|'import-export'|'purge' }
~~~

`memorySettings.ts` 是唯一配置适配层：只接收/发送以上四字段和 mutation attempt；CAS 冲突保存 draft、回读 latest 后让用户重试。它不得导出或默认填充 `memoryEnabled/autoNominate/growthDays`。新 UI 调 `memory.settings.get({})` 和 R3 update 分支，不读取 identity 后自报 subject。

- [ ] **8.1 写首屏红测。** `MemoryPage.test.tsx` 断言抽屉关闭时有一个只读模式/范围摘要、一个“记忆设置”按钮、一个搜索框、一列有效 item 和一个高级入口；`queryByRole('radio')`、`queryByRole('switch')` 均为 0。点击“记忆设置”后，唯一 drawer 内恰有三个 mode choice 和两个完整命名 switch。overview/inbox/history/ops tab、四 layer 卡、memoryEnabled、autoNominate、growthDays 在所有普通 UI 中均不存在。
- [ ] **8.2 跑首屏红测。** `npm --prefix web test -- src/memory/MemoryPage.test.tsx`；预期现有四 tab/运营面板仍渲染而 FAIL。
- [ ] **8.3 抽取设置 adapter 红测。** `memorySettings.test.ts` 覆盖 get 空 payload、update 仅四字段+expectedRevision、刷新保持、CAS conflict 保留 draft、off 不清零两个 scope；预期 adapter 不存在而 FAIL。
- [ ] **8.4 实现状态头、设置面板和 adapter。** 建 `MemoryStatusHeader`、`MemorySettingsPanel` 和 `memorySettings.ts`，接 Task 4 typed client；状态头只显示生效模式/范围摘要及打开 drawer 的按钮，设置面板才用单选/segmented control 呈现模式，两个 switch 标签分别为“个人记忆”“项目记忆”。不保留第二个总开关。
- [ ] **8.5 跑设置绿测。** `npm --prefix web test -- src/memory/memorySettings.test.ts src/memory/MemoryStatusHeader.test.tsx src/memory/MemorySettingsPanel.test.tsx src/memory/MemoryDrawer.test.tsx`；预期 PASS。
- [ ] **8.6 实现唯一首屏。** `MemoryPage` 只组合 StatusHeader、搜索、MemoryList 和单一 MemoryDrawer；把旧 facts/traces/growth/review/import/export/purge 中仍属 R3 的能力迁入 `MemoryAdvancedPanel`，随后移除 `MemoryOpsPanel` import 和文件。`PersonalIntelligencePage` 负责 overview/memory 跳转，但不得重复渲染设置控件。
- [ ] **8.7 跑首屏绿测。** 重跑8.2命令；预期 PASS，并用查询 `getAllByRole('dialog')/getAllByRole('complementary')` 断言任一时刻 drawer 数≤1。
- [ ] **8.8 写提示行为红测。** `SessionPage.memory.test.tsx` 覆盖 auto 三条保存仅一个2s status toast、manual/off 永不请求或显示 memory pending banner、冲突仅一个非打断提示、24h undo 在 advanced recent 可达。
- [ ] **8.9 移除 manual 自动横幅。** SessionPage 不再为记忆轮询 `feedback.candidates(sessionId)`；`PendingMemoryBanner` 仅保留非记忆业务确有消费者的分支，否则删除组件。用户消息动作“保存为记忆”固定只传 `{messageId}` sourceRef，不传 text；Engine 据此重读整条已持久化 user text 并生成 canonical fact/evidence，高级抽屉“手动新增”固定只传 text。当前版本不实现局部选区保存。两者成功后刷新列表/显示 toast，off 时隐藏入口并对竞态返回的 `MEMORY_MODE_OFF` 显示稳定错误。
- [ ] **8.10 跑提示绿测。** `npm --prefix web test -- src/session/SessionPage.memory.test.tsx src/memory`；预期所有目标测试实际执行且 PASS。
- [ ] **8.11 做可用性回归。** 测 item menu 打开同一 drawer、关闭/Esc 回焦点、purge dialog 焦点圈、390×844与200% zoom无横向滚动、读屏 status；运行现有 axe 测试并要求 serious/critical=0。
- [ ] **8.12 typecheck/build。** 依次运行 `npm --prefix web run typecheck`、`npm --prefix web run build`，每条命令后检查 `$LASTEXITCODE`；预期 PASS。
- [ ] **8.13 审查并提交。** 只 stage 本节 Files；提交 `feat(memory): deliver one-page memory center and explicit save`。

**Acceptance:** 用户首屏只看到只读状态/范围摘要、设置按钮、搜索和单列列表；三态与两个 scope 只在单一抽屉的设置视图出现，所有其他低频动作也复用该抽屉；manual 只有显式保存、没有待确认横幅；off 不出现注入或保存提示；设置刷新/CAS 不丢 scope；旧死配置不再可见。

## Task 9：评测、发布门与交付证据（全部 MEM）

**Depends on:** Task 0–8 与 06-X0/X1 的记忆相关真实产品接线全部完成；不依赖 OCR 或媒体工作包。

- [ ] **1. 建 corpus。** negative≥300、sensitive≥150、positive≥300、temporal_conflict≥150、recall_queries≥300；固定 seed/digest，敏感值全部合成，不把用户历史放 Git。明确标注 explicit_stable 与 inference。
- [ ] **2. Evaluator。** 新建 memory_fabric_eval_test.go，读取真实 service 输出逐条计算误存/漏存/kind+scope/旧版注入/Hit@5/Token/延迟；模型未配置的 case 为 INCOMPLETE，不能假 pass。旧实现和新实现使用相同 corpus/config/model。
- [ ] **3. 运行器。** 新建 scripts/eval-memory-fabric-v2.ps1；自身生成 UTC timestamp + GUID run ID，创建独立输出目录；Go evaluator 将 `metrics.json` 输出到显式环境路径。`go test -json` 的逐行日志保存 `test-events.jsonl`，绝不能叫 results.json 并删除换行。脚本同时写 `run-manifest.json`，固定含 runID、testedSourceHead、sourceTrackedDiffDigest、命令、start/end、exitCode、testCount、outputDigest、dataset/config/model digest、OS/CPU/RAM/runtime 及本目录三个 artifact 的相对路径和 SHA-256；正式 P14 运行要求 source tracked diff 为空，任一字段缺失或模型实测未完成时 `complete=false`。该 manifest 只是 P14 输入，不能把 requirement 标为 `VERIFIED`。

~~~powershell
$runID = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ') + '-' + [guid]::NewGuid().ToString('N')
$outDir = Join-Path 'artifacts/memory-fabric-v2' $runID
New-Item -ItemType Directory -Path $outDir -ErrorAction Stop | Out-Null
$env:LUNITIDE_MEMORY_EVAL_DIR = (Resolve-Path $outDir).Path
go test -count=1 -json ./internal/m8app -run TestMemoryFabricEval |
    Out-File -Encoding utf8 (Join-Path $outDir 'test-events.jsonl')
if ($LASTEXITCODE -ne 0) { throw "Memory evaluation failed" }
~~~

- [ ] **4. Gate。** 敏感/跨 scope/固定问候即时查询误存为 0；explicit stable recall≥95%、kind+scope≥90%；当前纠正 100%、旧版当前注入 0；Hit@5≥旧基线，时间纠正子集达到min(100%,旧基线+10pp)；Token 不超预算且回答质量不降。缺指标或数据不完整为 INCOMPLETE。
- [ ] **5. 验证。** 逐条执行 PRD 13.4 命令并检查退出码；新/旧 DTO、真实 bootstrap、migration、隐私/恢复演练均覆盖。另单独记录 `TestMemoryModePolicy` 的数据库/最终 provider message 双重证据、`TestMemoryPreMigrationBackupContains0159` 的备份 digest，以及旧 renderer omission case 的前后两个 scope 值。运行脚本只能证明它实际测量的项目，DryRun 不能替代模型实测。
- [ ] **6. 提交/移交。** 校验 `artifacts/memory-fabric-v2/<runID>/run-manifest.json` 已把 requirements→test→commit→artifact 映射到同一次运行，并把该目录交给 11 P14 的 QA evidence builder；本任务不得创建或覆盖 `docs/design/jiyishengji/evidence/release/<VERSION>/memory-evidence.json`，其中 VERSION 逐字取仓库根 `VERSION`。提交 `test(memory): record reproducible fabric release gates`。数据迁移/基础 read gate 通过后才启用 auto，不再要求用户逐条选择记忆。

**Acceptance:** M-R01～M-R15、U-R04～U-R06、迁移前备份/恢复和真实 bootstrap 均有可复现 artifact；敏感/越权/遗忘复活任一非0即阻断；缺模型、缺样本或测试未执行一律 INCOMPLETE，不计入通过率。

## 依赖顺序与完成定义

Task0（含X0备份/基线） → Task1 → Task2 → Task4 → Task7数据保护 → Task3 → Task5 → Task6 → Task8 → **P14 内执行 X1 记忆集成** → Task9评测/证据输入。Task3 明确消费 Task4 的 `memory.item.create`，不得倒序把 manual 临时接回 candidate；corpus/旧基线从 Task0 开始冻结，避免事后调指标。OCR/媒体可并行推进，但不是 Memory Task9 的完成前置。

总计划阶段与记忆回归子集固定如下；阶段子集通过只关闭该阶段，不得提前宣称 M-R01～M-R15 全部完成：

| 11 阶段 | 本计划任务 | 本阶段必须执行的测试子集 |
|---|---|---|
| P02 | Task1 | canonical `TestMemoryV2Migration`；M-R03 的 storage scope-collision 子测；M-R14 的设置迁移/旧 payload omission/CAS 子测 |
| P03 | Task2、Task4 | M-R01、M-R02、M-R06；M-R09 的后端删除屏障子测；M-R15 的 canonical create/idempotency 服务子测。此阶段不执行 X1，也不要求 M-R01～M-R15 全表通过 |
| P04 | Task3 | M-R05、M-R11、M-R12；M-R13 的 capture/job/search/inject 后端门；M-R15 的 auto/manual/off 与 candidate 后端子测 |
| P05 | Task5、Task6、Task7 | M-R03 的检索/导出/审阅/缓存全路径、M-R04、M-R07、M-R08、M-R10 |
| P06 | Task8 | U-R04～U-R06，以及 M-R09/M-R11/M-R13/M-R15 的正式 UI、toast、范围开关和 manual 横幅子测 |
| P14 | 06-X1 后接 Task9 | 在同一 testedSourceHead、同一 baseline/release untracked manifest 下执行完整 M-R01～M-R15 表；只有此阶段可形成 canonical `memory-evidence.json` |

完成必须有真实发布装配、数据迁移和回滚证据；每个 gate 有命令、退出码、fixture/config revision。灰度严格按 `shadow write → canonical base read → lifecycle/删除屏障 → auto capture → hybrid → consolidation`，五个内部 flag 初始为0且只按测试 subject cohort 人工晋级。

软回滚先关 auto/hybrid/consolidation，保留 canonical base reader、tombstone 和 source suppression；已有 v2-only fact 后不得退到 legacy-only reader。旧二进制会把0160视为 unknown schema，严格回退只能停写并恢复 Task0 的升级前备份，且会舍弃升级后变更；执行前必须展示差异并取得用户确认。user/expert scope 不能无损降为 project，禁止改名伪装成功。

本文已决定采用原生方案，实施时按任务顺序推进，无需再询问架构路线。尚未运行的功能测试保持未勾选；任一目标测试不存在、未执行、恢复未实演或结果 INCOMPLETE，均不得声称 R3 或 5/5 完成。
