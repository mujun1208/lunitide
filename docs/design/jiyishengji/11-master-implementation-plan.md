# Lunitide 记忆、OCR、媒体与融合 UI 总实施计划（R3）

> For agentic workers: 按本计划逐项实施；每个 Task 必须先写并运行指定红测，再做最小实现并运行绿测。不得把 Demo、计划新增测试、模拟安装或平台名称当成产品证据。

**Goal:** 在不破坏 Lunitide 现有聊天、办公、代码、语音和 OCR 能力的前提下，完成自动且可治理的 Memory Fabric v2、真实可探测的 Windows OCR、受硬门保护的 PaddleOCR-VL-1.6 可选能力、可信媒体会话，以及统一的“智能能力”极简 UI。

**Architecture:** Engine/SQLite 是身份、权限、设置、任务、操作和快照的唯一真相；Host 只承担 Windows/文件句柄/WebView2 等私有能力；Renderer 只消费有界 DTO 并发送命令。记忆采用 source → durable job → canonical version → retrieval → bounded injection；OCR 采用 scoped request snapshot → page routing → engine → artifact；媒体采用 asset/session/operation → root runtime → MediaCenter/MiniPlayer 单一 snapshot。所有写操作具有 scope、revision、idempotency、可审计终态。

**Tech Stack:** Go、modernc SQLite、JSON Schema Bridge、React 19、TypeScript、Zustand、Vitest/Testing Library、WebView2/WinRT、受管 Python stdio worker（仅通过 W0 后的 Paddle 包）。

**Spec:** [02-upgrade-prd.md](02-upgrade-prd.md)、[03-memory-implementation-plan.md](03-memory-implementation-plan.md)、[04-ocr-implementation-plan.md](04-ocr-implementation-plan.md)、[05-media-ui-implementation-plan.md](05-media-ui-implementation-plan.md)、[06-integration-contracts-plan.md](06-integration-contracts-plan.md)、[07-acceptance-and-scorecard.md](07-acceptance-and-scorecard.md)、[12-requirements-traceability.md](12-requirements-traceability.md)。

## 0. 使用规则与唯一决策

### 0.1 规范优先级

发生冲突时固定按下列顺序处理，不由开发者临场选择：

1. 02 的产品行为、范围与非功能要求。
2. 06 的唯一跨模块技术合同。
3. 03、04、05 的模块内精确实现步骤。
4. 本文件只冻结执行顺序、所有权和完成门，不能改写前三项。
5. 07 的验收口径；若验收与前述合同不一致，先修文档再测试。
6. 12 是唯一需求状态和证据映射源。
7. 09 的 Demo 只用于视觉和交互参考，不是业务实现；01 为历史复核记录。

发现仍有冲突时停止该冲突项，在同一个文档提交中修正 02/03/04/05/06/07/11/12 后再编码；不得在代码里偷偷选择第三套语义。

### 0.2 已冻结产品决策

| 主题 | 唯一实施决定 |
|---|---|
| 设置入口 | 内部 category id 保持 `personal`；用户可见名称为“智能能力”。 |
| 设置首页 | 首屏恰好两张卡：“自动记忆”“文字识别”。OCR 从模型路由页移除；`CapabilityRouting` 保留其他模型能力。 |
| 记忆模式 | `auto` 自动捕获并召回；`manual` 仅显式保存但可召回；`off` 不捕获、不搜、不召回、不注入，保留数据管理。 |
| 记忆范围 | wire/Go 只用 `personalMemoryEnabled/projectMemoryEnabled`；DB 只用 `personal_memory_enabled/project_memory_enabled`。关闭范围立即阻止该范围所有写、搜、召回和注入，但不删存量。 |
| 自动记忆 | 默认自动筛选稳定偏好、身份事实、长期目标、项目约束和明确决定；“你好”、寒暄、一次性天气/行情、低置信推断、秘密和临时执行噪声不进入长期记忆。普通候选不再逐条询问。 |
| 显式保存 | canonical mutation 固定为 `memory.item.create`；现有 `memory.create` 仅保留 legacy project/layer 语义。 |
| OCR 默认 | 保留 Windows.Media.Ocr；“文字识别 · 自动”是只读策略摘要，不是开关。可用性必须来自真实 WinRT/语言/固定图片 probe。 |
| 旧 PP-OCR | 目录 marker 只映射 `registered_unwired / available=false`；不能执行、不能暴露绝对路径、不能迁成 Paddle ready。 |
| PaddleOCR | PaddleOCR-VL-1.6 是完整 layout+VLM 可选本地包。没有已签名 catalog 和 `verifiedRuntimeProfileDigest` 时安装按钮禁用并返回 `NO_VERIFIED_RUNTIME_PROFILE`，mutation 数为 0。 |
| OCR 云端 | 只有现有明确授权才可用；provider 高级区首次展开才独立加载，失败不遮挡本地 OCR 状态。一次请求只解析一次 routing snapshot。 |
| 媒体 | 独立“媒体中心”，音乐/视频分面，只处理用户选定本地媒体或已授权 artifact；不内置曲库、片库、下载器或绕权能力。 |
| MiniPlayer | MediaCenter 内隐藏；`playing/paused` 会话离页显示；只含封面、标题、只读进度、播放/暂停、关闭。关闭是 stop/release，终态确认后才隐藏。 |
| UI | 纯黑/近黑基底、大留白、月白文字；青绿—蓝紫仅用于焦点和氛围。高级项渐进披露，错误必须有文字，不靠颜色。 |
| 成功语义 | dispatched/accepted/verified/failed/uncertain 分离；没有目标状态回读不得显示“成功”。 |

### 0.3 当前代码与目标的边界

截至审计 HEAD `b1d58b0d8e4867bd5c5efd7048ce82b5daf07c09`：

- 产品已有 memory 设置、M8 fact/working/growth、OCR routing、Windows OCR 实际调用和 PP-OCR 目录登记，但没有本计划所述完整 v2 数据面、Paddle worker/catalog/installer、media session 数据面或融合 UI。
- `captureMode=off` 尚不能覆盖全部 working/recall 路径；当前 recall 主要读取 `MemoryEnabled`。
- Windows OCR health 主要以 `runtime.GOOS` 判断，不等于 WinRT 初始化、语言和识别成功。
- `OCRRouting` 的 provider 与本地 OCR 联合加载，provider 失败可能遮挡本地状态。
- `PersonalIntelligencePage` 与 `MemoryOpsPanel` 仍是旧信息架构。
- 09/`ui-demo` 是无后端、无模型、无媒体文件的交互原型。

开发提交不得声称上述目标已经存在。每个能力只有在 12 对应行有证据并转为 `VERIFIED` 后才算完成。

## 1. 执行组织、文件所有权与依赖

### 1.1 分支与脏工作树规则

当前工作树含用户未提交源码。实施负责人按以下固定流程开工：

1. 运行 `git status --short`、`git rev-parse HEAD` 和 `git diff --binary | git hash-object --stdin`，只记录路径、HEAD、diff digest，不复制用户内容到证据。
2. 若本计划目标文件已有未提交改动，集成人先把这些改动保存为用户拥有的基线提交或受控 patch；实现者不得覆盖、reset、checkout 或自动格式化无关文件。
3. 从确认后的集成基线建立 `codex/memory-ocr-media-r3`；每个下表工作包使用独立短分支并以小提交合入。
4. 每次只 stage 当前 Task 的 Files；`git diff --check -- <paths>` 必须通过。

### 1.2 所有权

| 工作包 | 唯一主责 | 可改范围 | 禁止并行改动 |
|---|---|---|---|
| BASE | 集成人 | migrations manifest、store open、evidence、Bridge generation | 其他线不得自行占 migration 号 |
| MEM | 记忆开发 | 03 文件地图中的 domain/storage/m8/app/context/web memory | 不改 OCR/Media 状态机 |
| OCR | OCR 开发 | 04 文件地图中的 ocrapp/doctext/worker/storage/settings | 不把 PP marker 解释为可执行 |
| MEDIA | 媒体开发 | 05 文件地图中的 media/toolruntime/winexec/host/web media | 不建立第二播放器状态源 |
| UI | 前端集成人 | App/Settings/SmartCapabilities/Activity/shared styles | 不复制业务状态到 localStorage |
| QA | 验收负责人 | tests/evidence/traceability；测试 fixture | 不以修改阈值让失败“通过” |

共享文件 `internal/storage/sqlite/store.go`、`upgrade_schema.go`、`migrations/embed.go`、`internal/bootstrap/wire.go`、`internal/app/schema_registry.go`、`web/src/App.tsx`、生成文件只由集成人合并。子线先提供最小 patch，集成人按 P00 `migration-allocation.json` 冻结的 logical migration 顺序和 Bridge 字母序统一解决；`0160→0164` 仅是审计时候选，不得绕过 allocation 当物理文件名。

### 1.3 依赖图

~~~text
P00 baseline + migration allocation + raw backup
 ├─ P01 truthful current-state fixes
 ├─ P02 memory schema/settings ─ P03 canonical lifecycle ─ P04 capture ─ P05 recall ─ P06 memory UI
 ├─ P07 OCR scope/probe ─┬─ P08 OCR UI truthful baseline
 │                      └─ P09 Paddle W0 ─ P10 pack/worker/installer
 └─ P11 media truth ─ P12 media data/host ─ P13 player/UI/activity

P14 integrated regression/evaluation → P15 staged release/rollback
~~~

P09 W0 失败只阻止 P10 和 Paddle 自动路由；P07/P08 的 Windows OCR 真实性、旧 PP 兼容和融合 UI 必须继续交付。Memory、Media 不因 Paddle W0 失败停工。P08 复用 P06 已创建的 `SmartCapabilitiesPanel`，因此只在共享 UI shell 层面依赖 P06；该依赖不要求 OCR 后端等待 Memory 评测。

## 2. P00：冻结基线、迁移号和迁移前备份

**Files:** 先创建并单独提交 `scripts/capture-r3-baseline.ps1`；随后创建 `docs/design/jiyishengji/evidence/migration-allocation.json`、`docs/design/jiyishengji/evidence/baseline/<UTC>/untracked-manifest.json`、`docs/design/jiyishengji/evidence/baseline/<UTC>/manifest.json`；修改 `internal/storage/sqlite/store.go`、`backup.go`、`maintenance_backup.go`、`internal/bootstrap/wire.go`；新增 `internal/storage/sqlite/memory_upgrade_backup_test.go`。

**实施：**

- [ ] **先交付只读采集器。** `scripts/capture-r3-baseline.ps1` 只读 Git/文件元数据并支持 `-SelfTest`。自测在临时 Git 仓库覆盖中文/空格路径、CRLF/二进制、空清单、内容改变、reparse point 拒绝和重复运行目标已存在拒绝；先运行 `./scripts/capture-r3-baseline.ps1 -SelfTest`，成功后只提交该脚本，提交信息 `test(evidence): add deterministic r3 baseline capture`。完成此工具提交后才开始任何 R3 业务文件修改。
- [ ] **生成唯一 untracked manifest。** 正式模式在创建 evidence 目录前一次性读取 `git ls-files --others --exclude-standard -z`；路径转换为仓库相对 `/` 分隔并按 ordinal 排序。每个普通文件只写 `{path,bytes,sha256}`，SHA-256 对文件原始 bytes 计算并用小写 hex；发现仓库外解析、reparse point、读取期间 size/mtime 改变即非零退出。JSON 固定为 UTF-8 无 BOM、compact、末尾一个 LF；`untrackedManifestDigest` 是该文件最终 bytes 的 SHA-256。输出文件自身因目录在快照后才创建，不进入本次清单。
- [ ] **按以下命令采集，不手写时间目录。** 脚本还必须在开始/结束两次校验 HEAD 一致，计算 `git diff --binary --no-ext-diff | git hash-object --stdin` 的 `trackedDiffDigest`，扫描 migration 并生成 allocation；目标路径已存在时失败，禁止覆盖历史基线。

~~~powershell
$baselineUTC = [DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
./scripts/capture-r3-baseline.ps1 -RepositoryRoot (Resolve-Path .).Path -BaselineUtc $baselineUTC -LogicalMigrationNames @('memory_fabric','memory_retrieval','memory_generations','ocr_model_packs','media_sessions')
if ($LASTEXITCODE -ne 0) { throw "R3 baseline capture failed" }
~~~

- [ ] 扫描 `migrations/*.sql`、`migrations/embed.go`、store manifest、checksum、expected schema/columns 和发布 fixture；确认最大号后一次性分配连续区间。当前候选为 0160 Memory Fabric、0161 Memory Retrieval、0162 Memory Generations、0163 OCR、0164 Media。
- [ ] allocation JSON 固定包含 `logicalName/number/fileName/previous/maxObserved/head/trackedDiffDigest/untrackedManifestRelativePath/untrackedManifestDigest/baselineManifestRelativePath/allocatedAt`；`manifest.json` 固定包含相同 HEAD/digest/path、Git object format、PowerShell/Git/OS/CPU/RAM、命令、开始/结束时间和 allocation SHA-256。读取两个 JSON 回算所有 digest 后才继续；新增测试确保文档、文件、manifest 只有一套编号。
- [ ] 按 03 Task 0 实现 `openRaw → inspectPreMigration → BeforeMigrate → initialize → protect files → return Store`。
- [ ] 已有数据库且待执行列表包含 P00 allocation 中任一 R3 logical migration 时，未提供 hook 必须返回 `PRE_MIGRATION_BACKUP_REQUIRED`；全新空库不备份。
- [ ] 备份复用 SQLite 一致性实现，不用普通文件复制替代 WAL 快照。hook 失败时数据库 journal 与数据 digest 不变。
- [ ] 保存基线命令、开始/结束时间、退出码、输出 digest 和环境；秘密、绝对用户文档路径及完整 diff 不进 evidence。

**红测：**

~~~powershell
go test -count=1 ./internal/storage/sqlite -run 'TestMemoryPreMigrationBackupContains0159|TestMigrationAllocation'
~~~

首次必须因 hook/allocation 缺失失败，且目标测试数不为 0。

**绿测：**

~~~powershell
go test -count=1 ./internal/storage/sqlite -run 'TestMemoryPreMigrationBackup|TestBackup|TestMigrationAllocation'
go test -count=1 ./internal/storage/sqlite
~~~

**完成门：** allocation 已冻结；baseline manifest 可定位且验证唯一 `untracked-manifest.json`，回算 digest 完全一致；0159 fixture 的升级前备份仍停在 0159 且含 canary；hook 失败零迁移；用户脏改动未被覆盖。提交：`test(upgrade): freeze migrations and back up before r3`。

## 3. P01：先修真实性，不等待新数据面

### 3.1 媒体 false-success

按 05 Task 0 修改 `internal/toolruntime/media_foreground.go` 和测试：发送 global media key、打开 URL/进程最多返回 `command_dispatched/MEDIA_UNVERIFIED`；只有匹配目标的 SMTC 回读或 owned audio/video 事件可 verified。

~~~powershell
go test -count=1 ./internal/toolruntime ./internal/winexec
~~~

提交：`fix(media): keep key-only playback unverified`。该修复不跟随 media flag 回滚。

### 3.2 OCR 当前状态

修改 `internal/ocrapp/pack.go`、`health.go`、`recognize.go`、`route.go` 及测试：

- PP marker 始终 `registered_unwired/available=false`，执行计数 0；effective engine 回落到实际可用路径。
- 新写请求拒绝 `localEngine=ppocr`；旧字段只兼容解码一个发布周期。
- Renderer、Bridge、日志和 activity 不返回 `packRoot`。
- P07 的六态 `windowsProbe` 尚未落地前，旧兼容 health DTO 只能返回 `available=false,errorCode=OCR_PROBE_REQUIRED`，不得返回 ready；不要把 `unknown/probe_required` 加进最终六态 enum。P07 接线后仅保留 0.2 冻结的六态。

~~~powershell
go test -count=1 ./internal/ocrapp ./internal/doctext
~~~

提交：`fix(ocr): report only executable local capabilities`。

## 4. P02：Memory 0160–0162 与设置兼容

**Normative detail:** 严格执行 03 Task 1 和 06-C1；表名、约束、迁移矩阵不得简化。

**Files:** P00 allocation 为 logical `memory_fabric`、`memory_retrieval`、`memory_generations` 指定的三个物理 migration 文件（审计候选分别为 `0160_memory_fabric.sql`、`0161_memory_retrieval.sql`、`0162_memory_generations.sql`）；`internal/domain/m8core/memory_v2.go`、`internal/storage/sqlite/m8_memory_v2.go`、`memory_v2_migration_test.go`；修改 store/upgrade/memory ops。

**顺序：**

- [ ] 先写空库、0159 升级、重开、checksum、scope collision、非法 enum/interval/vector 红测。
- [ ] 先落 allocation 中的 logical `memory_fabric`（审计候选 0160），登记 manifest/checksum/expected schema/column/index/trigger。
- [ ] 按六组合表迁移 `memory_enabled/capture_mode`；历史 scope 置 true，保存不可变迁移原值和 `last_non_off_capture_mode`；五个内部 flag 默认 0。
- [ ] legacy get/update 保持 SHA `version/expectedVersion`；R3 内部 revision 为整数。两个 CAS 形状由 JSON Schema `oneOf` 区分，不得混用。
- [ ] 旧 payload 省略新 scope 时用字段存在性解码并在同一事务保留原值；R3 payload 省略 legacy 字段同理。
- [ ] `autoNominate/growthDays` 只保留兼容读写，没有生产消费者，不进入新 UI。
- [ ] 再按 allocation 顺序落 logical `memory_retrieval`/`memory_generations`（审计候选 0161/0162）、FTS external-content shadow 和触发器；忘记正文同步移除 shadow/index。

**验证：**

~~~powershell
go test -count=1 ./internal/storage/sqlite -run 'TestMemoryV2Migration|TestMemoryV2ScopeCollision|TestMemoryV2SettingsMigrationMatrix|TestMemoryV2SettingsLegacyOmission|TestMemoryV2SettingsCAS'
go test -count=1 ./internal/storage/sqlite
~~~

**完成门：** 新建、0159 升级和重开 schema dump 一致；并发同 revision 仅一个成功；旧 renderer 不会把 scope 清零。本阶段只关闭 canonical `TestMemoryV2Migration`、M-R03 的 storage scope-collision 子测和 M-R14 的迁移/omission/CAS 子测，不宣称完整 M-R 表通过。提交：`feat(memory): add scoped fabric schema and deterministic settings migration`。

## 5. P03：Memory canonical source、版本、纠正和遗忘

严格执行 03 Task 2、Task 4。**本阶段禁止提前执行 06-X1，也不得要求 M-R01～M-R15 全表通过**；X1 需要 Task3/5/8 的真实接线，只能在 P06 后由 P14 执行。

- [ ] 在 `internal/app/chat_memory.go`、`chat_run_stream.go`、`internal/m8app/memory_projection.go` 建立 source resolver；只接受当前 Engine 身份可读的 message/artifact，拒绝客户端提供主体。
- [ ] 移除记忆公开路径对 `local-user` 的安全回退；无可信身份返回稳定权限错误。
- [ ] 旧事实 backfill 可重入，以完整 `subject_id/scope_kind/scope_id/legacy_id` 做 map；不能把 user/expert 偷降为 project。
- [ ] canonical 写入使用 immutable body/version + CAS head + evidence span；更正产生新版本，不改原聊天。
- [ ] 新增精确 Bridge：`memory.item.list/get/history/create/correct/forget`、`memory.capture.undo`、`memory.review.list/resolve`、`memory.generation.list/preview/activate/discard`、`memory.import.preview/commit`、`memory.purge.prepare`；保留并切换现有 `memory.search/get/export/purge`。schema、handler、client 与 mutation/read 集合必须同批生成。
- [ ] `memory.item.create` payload 按 03 Task 4 冻结：顶层 `idempotencyKey`，payload 含目标 scope、`operationId`，正文严格 `oneOf` 为 text-only 或 sourceRef-only；sourceRef 分支的 canonical text 由 Engine 重读原消息/span 派生，客户端不得同时传正文。新建没有可比较的旧 head，因此不含 `expectedRevision`；subject 从 Engine identity 注入。
- [ ] forget 在一个事务内写 tombstone、source suppression、删正文/FTS/embedding、失效 cache；重复调用幂等。undo 只在限定窗口恢复允许内容。

~~~powershell
go test -count=1 ./internal/storage/sqlite ./internal/m8app ./internal/app -run 'TestMemoryProjection|TestMemoryItem|TestMemoryForget|TestMemorySource'
npm --prefix web run generate:bridge
npm --prefix web run verify:bridge
~~~

**完成门：** spoof、跨主体、跨 scope、stale revision、同 key 变参均拒绝；删除屏障同步生效；legacy 读与 canonical 基础读影子差异可解释。本阶段执行 M-R01/M-R02/M-R06、M-R09 后端删除屏障子测及 M-R15 canonical create/idempotency 服务子测；不执行依赖 capture/recall/UI 的其余 M-R。提交：`feat(memory): add canonical item lifecycle and evidence`。

## 6. P04：自动筛选、持久任务和模式/范围全路径门

严格执行 03 Task 3；这是解决“每次问要不要加入记忆”的核心工作包。

- [ ] 使用 durable `memory_capture_jobs/cursor` 替换容量 32 的易失 channel 作为真相；消息提交只原子 enqueue，不阻塞回答。
- [ ] worker lease/fence/heartbeat/retry；批量最多 4096 input tokens、768 output tokens、8 candidates；预算不足保留游标。
- [ ] 硬过滤先于模型：问候、感谢、一次性天气/行情、OTP/密码/token、纯工具日志、低置信推断不进模型；敏感内容默认拒绝长期保存。
- [ ] 评分只选择稳定、未来复用、明确来源且范围可确定的信息；模型输出必须 schema 校验并保留 evidence，模型不可用时 fast path 继续。
- [ ] auto 自动保存高置信条目，低置信进入安静候选；不逐条弹“要不要记忆”。只有冲突、敏感或不可推断 scope 才请求确认。
- [ ] manual 不创建自动 candidate/banner/job，只允许显式 `memory.item.create`；off 所有 capture/working/candidate/job/index/面向回答的 `memory.search`/recall/inject 为 0。授权管理用 `memory.item.list/get/history`、过滤、correct/forget/export/purge 仍可用，不得与注入检索共用放行判断。
- [ ] 两个 scope 开关覆盖 auto、manual、working、expert、candidate、index、search、recall、inject；关闭立即使 cache/snapshot 失效。
- [ ] 每个自动条目提供非打断 toast“已记住 1 条 / 查看 / 撤销”；同源重放不重复。

~~~powershell
go test -count=1 ./internal/m8app ./internal/app -run 'TestMemoryCapture|TestMemoryUsage|TestMemoryMode|TestMemoryScope'
go test -race -count=1 ./internal/m8app ./internal/app
~~~

**完成门：** 安全集假保存率为 0；weather/greeting 保存为 0；崩溃重启不丢 source cursor；回答首 token 延迟不被后台提取阻塞。本阶段执行 M-R05/M-R11/M-R12、M-R13 后端门及 M-R15 的 auto/manual/off/candidate 子测；UI 子测留给 P06。提交：`feat(memory): add durable value-based auto capture`。

## 7. P05：混合召回、Token 预算与整理

严格执行 03 Task 5、6、7。

- [ ] SQL 先按 identity/scope/时间/tombstone/敏感过滤，再并行 FTS top40、Go 精确 dense top40、时间/实体/关系 top20。
- [ ] 以 RRF 合并、MMR 去重；无 embedding 时退化到 FTS+temporal；deadline 返回 best-so-far 并记录 `dense_incomplete`。
- [ ] 注入器仅使用最终 top-k，逐条附 fact/version/evidence；预算超限先减记忆，不挤占用户消息和系统安全内容。
- [ ] 反馈事件 append-only；整理 copy-on-write 生成 generation，验证完成后原子切换 active pointer。
- [ ] 导出包含 canonical 当前/历史、来源、scope、tombstone 和版本；导入先 preview，再带 digest/revision commit；绝不把 preview 当提交。
- [ ] 按 06-C1 创建 generation 四方法与 import 两方法的六份 strict schema、canonical handler tests 和 typed client；generation activate/discard、import preview/commit 进入 mutationMethods，list/generation preview 保持 read，所有 revision/DTO/error 不得自拟别名。
- [ ] 建 frozen evaluation dataset，记录 baseline 与 v2 的 recall、precision、temporal、scope leak、token 和 p95。

~~~powershell
go test -count=1 ./internal/storage/sqlite ./internal/m8app ./internal/contextapp ./internal/compactionapp
go test -race -count=1 ./internal/m8app ./internal/contextapp
~~~

**完成门：** 本阶段执行 M-R03 的检索/导出/审阅/缓存全路径、M-R04/M-R07/M-R08/M-R10；scope leak=0；遗忘内容召回=0；Token 指标只报告实测，不预写节省百分比。M-R01～M-R15 全表只在 P14 的 X1 后执行，不能在 P05 提前写“全绿”。提交：`feat(memory): add scoped hybrid recall and safe consolidation`。

## 8. P06：融合记忆 UI

**Files:** 修改 `web/src/settings/SettingsPage.tsx`、`web/src/m8/PersonalIntelligencePage.tsx`、`web/src/memory/MemoryPage.tsx`；创建 `SmartCapabilitiesPanel.tsx`、`MemoryStatusHeader.tsx`、`MemoryDrawer.tsx`、`MemorySettingsPanel.tsx`、`MemoryList.tsx`、`MemoryItemMenu.tsx`、`MemoryAdvancedPanel.tsx` 及测试；迁移断言后删除 `MemoryOpsPanel.tsx` 及旧测试；共用 `web/src/styles.css`。

- [ ] `personal` overview 恰好渲染两张静态能力卡，不发 project、memory、OCR 或 provider 请求；只有进入 memory/ocr 详情后才加载对应有界 snapshot，高级区仍保持惰性。
- [ ] 自动记忆详情首屏：返回、只读状态与范围摘要、设置按钮、搜索、单一列表、每行 scope/更新时间/更多；不保留旧多 Tab，抽屉关闭时不渲染三态控件或 scope switch。
- [ ] 设置按钮打开唯一 `MemoryDrawer` 的 settings 视图，其中 `MemorySettingsPanel` 渲染 auto/manual/off + 个人/项目开关；item 和 advanced 也复用这个 drawer。高级统计、来源、导入导出按需展开后才加载。
- [ ] 页面无 project 时照常展示个人记忆，不以 `projectId` 作为整个页面前提。
- [ ] 列表和 drawer 只消费 Bridge query/mutation；不自行推断保存成功。冲突回读最新 revision。
- [ ] 纠正、忘记、撤销、显式保存均展示真实 accepted/verified/error；普通自动保存只用安静 toast。

~~~powershell
npm --prefix web test -- src/settings/SettingsPage.test.tsx src/settings/SmartCapabilitiesPanel.test.tsx src/m8/PersonalIntelligencePage.test.tsx src/memory/MemoryPage.test.tsx src/memory/MemoryStatusHeader.test.tsx src/memory/MemorySettingsPanel.test.tsx src/memory/MemoryDrawer.test.tsx
npm --prefix web run typecheck
~~~

**完成门：** 1440/1024/390、100/150/200% 无横向滚动；键盘可完成搜索、打开设置、更改模式、纠正和忘记；高级区关闭时对应请求数为 0。执行 U-R04～U-R06 及 M-R09/M-R11/M-R13/M-R15 的 UI、toast、范围开关和 manual 横幅子测；随后才具备 P14 执行 X1 的前置。提交：`feat(ui): unify memory under smart capabilities`。

## 9. P07：OCR 诚实基线、scope snapshot、生产消费者与真实 Windows probe

严格执行 04 Task 1、Task 3 的 Windows probe 部分、Task 4、Task 5、Task 7 的 Windows/Bridge/artifact 基线部分，以及 06-C3/C4/X2。P09 未通过也必须完成本工作包，给 P08 提供真实的 disabled snapshot。

P07 的 Task1 wiring 只注入已经存在的 SQLite scoped routing/gate/run/artifact repository 与真实 Windows probe。`catalog verifier`、Paddle `installer`、worker `runner/launcher` 的类型和生产实例在本阶段尚不存在，禁止在 `internal/bootstrap/wire.go` 引用、伪造或用空实现占位；无 profile 的 pack mutation 由 gate-first handler 直接以 `NO_VERIFIED_RUNTIME_PROFILE`、零副作用返回。P09 通过后由 P10 在同一 composition root 一次性接入真实 catalog/installer/runner/launcher，并移除仅因实现未到位而存在的临时 dispatch 分支；gate-first 的拒绝语义继续保留。

- [ ] 落 P00 allocation 指定的 logical `ocr_model_packs` 物理文件（审计候选 `0163_ocr_model_packs.sql`）和 typed repository：pack state/version/operation、retained trusted NOTICE、settings/gates、legacy registration、scoped runs/pages/artifacts 与 leases；默认 `install_enabled=0,auto_route_enabled=0,verified_runtime_profile_digest=NULL,disabled_reason=NO_VERIFIED_RUNTIME_PROFILE`。
- [ ] 实现 `ocr.routing.get/set`、`ocr.pack.get/install/cancel/uninstall`、`ocr.pack.notice.list/read`、`ocr.run.list/get`、`ocr.artifact.read` Bridge；routing/get/set/run.list 的 scope selector 固定为 user 禁 scopeId、project 必填 scopeId 的 strict oneOf，Engine 注入 subject 并授权 project，项目无 override 时按 06-C3 返回带 policySource 的继承 snapshot；未有 verified profile 时 install handler 在创建 operation、下载、目录或进程前稳定拒绝，mutation=0。P07 的 NOTICE list 返回空页，read 返回 `OCR_PACK_NOTICE_UNAVAILABLE`；两者均不接受路径、不产生 mutation，也不伪造许可内容。
- [ ] 新增 `OCRScope`、`ResolvedOCRRequest`、`ProviderFunc`；入口一次性解析 identity、scope、policy、provider、gate、engine availability、deadline 和 request id。
- [ ] `RecognizeDocumentV2` 之后禁止重新读取 routing/provider/gate；测试注入中途设置变化，当前请求结果必须保持 snapshot。
- [ ] KB、模型目录、Office/PDF 和 Bridge 消费者全部传授权 scope；无可信身份 fail closed。
- [ ] PDF 逐页有界渲染，页面独立路由；结果记录 run/page/method/coverage/complete/uncertain。旧 Result 只做显式兼容映射。
- [ ] Windows 实现 probe 六态：`ready/unsupported_os/initialization_failed/language_unavailable/sample_failed/timed_out`；仅 ready 可用，15 秒 timeout、成功/失败最多缓存 5 分钟，语言或设置改变失效。
- [ ] probe 必须真实创建 WinRT engine、确认语言、识别仓库固定小图并断言文本；非 Windows stub 只能证明 `unsupported_os`。
- [ ] Windows 识别结果使用 scoped run/page/artifact repository；Bridge 只返回有界摘要/ref，绝不返回内容存储路径。legacy Result 兼容映射不绕过 scope。
- [ ] 真 Windows integration 测试不得 Skip；CI 无对应 runner 时该 release gate 为 BLOCKED，不得由 stub 代替。

~~~powershell
go test -count=1 ./internal/storage/sqlite ./internal/doctext ./internal/ocrapp ./internal/m8app ./internal/app ./internal/officestudio
go test -count=1 ./internal/app ./internal/contract -run 'TestOCR|TestDocument'
npm --prefix web run generate:bridge
npm --prefix web run verify:bridge
~~~

**完成门：** allocation 指定的 OCR migration 在新建库、0159 升级库和重开场景通过；`ocr.pack.get` 能真实返回 disabled 原因；无 profile 的安装副作用为 0；默认 cloud calls=0；scope spoof=0；执行期二读 routing=0；WinRT 状态不假 ready；现有 Windows OCR 识别回归通过。提交：`feat(ocr): add scoped truthful baseline and windows probe`。

## 10. P08：融合 OCR UI 与 provider 故障隔离

**Depends on shared UI:** P06 已创建 `SmartCapabilitiesPanel.tsx` 及其测试；P08 只能修改/消费它，禁止再创建同名组件。

**Files:** 修改 `SettingsPage.tsx`、`PersonalIntelligencePage.tsx`、`SmartCapabilitiesPanel.tsx` 及现有测试；创建 `OCRSettingsPanel.tsx`、`OCRModelPackCard.tsx`、`OCRAdvancedPanel.tsx`、`OCRRecentRuns.tsx`、`web/src/settings/ocrActivityAdapter.ts`、`ocrActivityAdapter.test.ts` 及对应测试；迁移断言后删除 `OCRRouting.tsx` 及其旧测试。P08 不创建或修改 `web/src/activity/activitySnapshot.ts`、`ActivityStatusButton.tsx`、`ActivityCenter.tsx`；这些共享文件只由 P13 创建，OCR adapter 由 P13 消费。

- [ ] 从 routing 页面删除 OCR 卡，但保留其他 provider/model 路由能力。
- [ ] “智能能力”第二卡进入唯一 OCR 详情；全局 Settings 固定以 user scope 独立加载 `ocr.routing.get` 与 `ocr.pack.get`，不从最后项目猜 scope。
- [ ] 顶部“文字识别 · 自动”只读，禁止 switch/checkbox；下方显示 probe 的真实中文状态和恢复动作。
- [ ] Paddle 卡在无 W0 digest 时显示“当前不可安装”，disabled title/辅助文本含 `NO_VERIFIED_RUNTIME_PROFILE`；点击/键盘均不能发 mutation。
- [ ] provider、语言、legacy PP、版本、缓存、pipeline、fallback 收入高级区；`provider.list` 只在首次展开时请求，失败仅在高级区报错。
- [ ] 正式设置页不新增截图、选文件、OCR 结果工作台；09 中这些只属于历史交互探索。

~~~powershell
npm --prefix web test -- src/settings/SettingsPage.test.tsx src/settings/SmartCapabilitiesPanel.test.tsx src/settings/OCRSettingsPanel.test.tsx
npm --prefix web run typecheck
~~~

**完成门：** provider.list 失败时 Windows/Paddle 状态仍可见；legacy marker 不显示 ready；disabled 安装 mutation=0。提交：`feat(ui): add truthful ocr smart capability`。

## 11. P09：PaddleOCR-VL-1.6 W0 可行性门

该工作包是验证任务，不以写 mock worker 代替。

**Files:** 创建 `workers/paddleocr-vl-1.6/paddleocr_worker.py`、`protocol.schema.json`、`requirements.lock`、`pipeline.yaml`、`scripts/build-paddleocr-vl-pack.ps1`、`scripts/test-paddleocr-vl-pack.ps1` 和 `docs/design/jiyishengji/evidence/ocr-runtime/<digest>/`。

- [ ] 在干净 Windows x64 CPU、无全局 Python、无模型缓存环境，用锁定依赖构建自包含包。
- [ ] 断网启动完整 layout+VLM pipeline，识别固定图、复杂排版、表格和长 PDF；证明不是仅文本 detector/recognizer。
- [ ] 记录 OS/CPU/RAM、依赖与模型 digest、许可证/SBOM/NOTICE、启动时间、每页 p50/p95、RSS 峰值和失败码。
- [ ] worker stdout 只输出有界 JSONL 协议；日志走 stderr；请求/响应过 schema；输入只接受受管临时引用。
- [ ] 验证 cancellation、timeout、进程崩溃、损坏模型、断网和重复启动。

**判定：**

- 全部通过：签发 `verifiedRuntimeProfileDigest`，允许进入 P10。
- 任一完整性、许可、签名、资源或稳定性门失败：记录 `BLOCKED`，不创建假 digest；产品继续显示 disabled，P07/P08 正常发布。

**完成门：** 有可复现实机 evidence 或明确 BLOCKED 证据；“上游可运行”不能代替 Lunitide 包验证。提交：`test(ocr): qualify paddleocr vl runtime profile`。

## 12. P10：启用经验证的 Paddle pack、installer 与 worker

只在 P09 通过后执行 04 Task 2、Task 3 的 Paddle worker/资源部分和 Task 7 的 Paddle corpus/故障门。04 Task 1、4、5 以及 Windows/artifact 基线已在 P07 完成，绝不能因 P09 失败而跳过。

- [ ] catalog 固定签名公钥、允许版本、平台、URL、size、sha256、manifest/runtime profile digest；签名和 digest 不匹配零下载。
- [ ] installer 预检磁盘/OS/CPU/RAM，异步下载、校验、原子激活、取消、失败清理、重试、崩溃恢复；同 pack 最多一个 mutation。
- [ ] 首发只启动受管 Python stdio worker；不临时 pip、不用用户 Python/WSL/Docker、不启动 HTTP 服务。
- [ ] 用 Job Object 限制进程/内存/回收；文案只称资源治理，不宣称 OS 级网络沙箱。
- [ ] output artifact 写独立受控 CAS adapter；Bridge 只返回有界摘要/ref，不内联全文或绝对路径。
- [ ] `ocr.pack.install/cancel/uninstall/get`、`ocr.run.get/list` 使用各自冻结的 revision、idempotency 和 owner scope；catalog verifier 在暴露 release/安装确认前先把完整已验 NOTICE 写入受控 CAS 与 `ocr_pack_notices`，再把 P07 的 `ocr.pack.notice.list/read` 接到 retained trusted records。卸载/catalog 换版只更新时间不删 NOTICE；断网关于页 list→read、逐块 digest、无路径与有界读取测试全过。
- [ ] 自动路由初始仍 off；200 页盲测比较 Windows/Paddle 的准确性、版面/表格完整度、耗时和资源后再决定 cohort。
- [ ] 在 `internal/bootstrap/wire.go` 一次性注入 P10 已落盘的真实 catalog verifier、installer、worker runner/launcher；composition test 证明没有 P07 gate-only 分支、假 runner 或 `*FileStore` 并存。

~~~powershell
go test -count=1 ./internal/storage/sqlite ./internal/ocrapp ./internal/app
npm --prefix web run generate:bridge
npm --prefix web run verify:bridge
npm --prefix web test -- src/settings/OCRSettingsPanel.test.tsx
~~~

**完成门：** 07 的 O-R01～O-R12 全绿；干净机安装/取消/断网/重启/篡改证据齐全；未通过 corpus 的设备 profile 不发布。P09 失败时 P07/P08 可作为“Windows OCR + 诚实 disabled Paddle 卡”基线发布，但 Paddle 相关 requirement 保持 `BLOCKED/INCOMPLETE`，完整 PRD 不得签 5/5。提交：`feat(ocr): enable verified optional paddle pack`。

## 13. P11–P13：媒体数据面、播放器和活动 UI

### P11 真实性与 schema

先完成 P01 媒体修复，再执行 05 Task 1/2：

- [ ] 落 P00 allocation 指定的 logical `media_sessions` 物理文件（审计候选 `0164_media_sessions.sql`）：assets/sessions/operations/queue/bookmarks/settings/gates/player leases/commands/audio focus。
- [ ] 所有 mutation 使用 subject+method+idempotencyKey；queue/session revision CAS；同 key 变参拒绝。
- [ ] owned/external 明确分流；external 未核验显示 uncertain，不伪造进度/队列。

### P12 Host 私有文件与 Range

执行 05 Task 3/4/5：

- [ ] Host 原生选文件，只把 opaque asset/ticket 给 Renderer；Engine 私有保存绝对路径和 file identity。
- [ ] WebView2 broker 支持 GET/HEAD/单 byte Range、200/206/416；拒绝 multi-range、过期/跨 scope/tamper/traversal。
- [ ] 使用有界 seekable stream，单次 Read≤256KiB，不整文件读内存；测试 4GiB sparse 文件、导航取消和 handle 回收。
- [ ] 公开/Renderer 可消费方法逐字实现为 `media.session.create/get/list/command/watch`、`media.queue.command`、`media.asset.list/open`、`media.operation.get/list`、`activity.list`；所有 list/watch/create/pick 的 scope 使用 06-C5 的 user/project strict oneOf，`media.asset.list` 过滤名为 `sourceKind`。`media.asset.pick` 是唯一 `x-owner=host` 的公开 Host-owned 选择方法，但 Host 只透传 selector，Engine 才派生 subject/授权 project。播放器方法逐字为 `internal.media.player.attach/next/report`，仅由 Host 经 authenticated private pipe 调用既有 `internalRuntimeHandlers`，不进入 Renderer envelope/schema，也不使用公开 `x-owner`。绝对路径登记/解析同样只走 `internal.media.asset.register/resolve` 私有 Host RPC，不进入公开 schema 或 WebMessage 转发；不存在公开 `media.asset.register`。

### P13 单一播放器、MediaCenter、MiniPlayer 与 Activity

执行 05 Task 6/7/8 和 06-X3：

**Files:** 创建 `web/src/activity/activitySnapshot.ts`、`ActivityStatusButton.tsx`、`ActivityCenter.tsx` 及其测试，并由前端集成人修改 `web/src/app/appTypes.ts`、`navStore.ts`、`LaunchSidebar.tsx`、`LaunchSidebar.test.tsx`、`web/src/App.tsx`、`App.test.tsx`、`styles.css` 接入；这是唯一 `media` Page/办公组入口、三份共享 activity 文件和 App 根 Media runtime 的唯一集成阶段。消费 P08 的 `web/src/settings/ocrActivityAdapter.ts`、tool journal 与 media operation，禁止 OCR/Media 各建一套顶部入口或 snapshot。当前 AgentHub shell switch/replaceMainNav 保持独立，不回填 officeMenu。

- [ ] App 根部只挂一套无视觉 `MediaStore + OwnedMediaPlayer`，跨 route 使用稳定 key；`SessionPage` 不再创建第二 player。
- [ ] `Page` union 增加 `media`；`LaunchSidebar` 办公组加入常驻媒体中心并纳入 officeOpen/active；`App` 主 route 渲染唯一 MediaCenter。入口不受 officeMenu toggle 控制，AgentHub shell switch 仍独立；AgentHub header/replaceMainNav 下 MiniPlayer 与 Activity 不遮挡且不重挂。
- [ ] 音乐/视频各自表面复用同一 snapshot/queue；浏览器 `play/pause/timeupdate/ended/error/stalled` 才更新 owned 状态。
- [ ] 每次装载有 `playbackEpoch`，report 带 asset/epoch/eventSeq；旧 epoch 丢弃；重复 ended 只推进一次。
- [ ] 无 session 的 MediaCenter 显示低密度空状态；不得显示虚构歌单、片库或进度。
- [ ] MiniPlayer 合同按 0.2；失败/uncertain 时保留并给恢复动作。
- [ ] owned/TTS/ASR 共享 audio focus；抢占先暂停旧目标并核验，失败不启动新声源。
- [ ] 顶部 `ActivityStatusButton` 是唯一常驻活动入口；详情抽屉显示 operation 的 sent/verified/failed/uncertain 与证据，不新增左侧“活动中心”。

~~~powershell
go test -count=1 ./internal/storage/sqlite ./internal/mediaapp ./internal/toolruntime ./internal/winexec ./internal/webviewhost ./internal/app ./internal/tts ./internal/voice
npm --prefix web run generate:bridge
npm --prefix web run verify:bridge
npm --prefix web test -- src/media src/activity src/App.test.tsx
npm --prefix web run typecheck
~~~

**完成门：** A-R01～A-R12、V-R01～V-R04、U-R03/U-R09/U-R10 全绿；真实 WebView2 音视频、Range、刷新暂停恢复、100 次页面切换和 30 分钟播放有证据。提交顺序：`feat(media): add governed media sessions`、`feat(media): add private webview2 resource broker`、`feat(ui): add media center and truthful activity`。

## 14. P14：集成回归、视觉、性能与证据

**唯一主责与 Files:** QA/集成人先创建并单独提交 `scripts/run-r3-release-gates.ps1`、`scripts/build-r3-release-evidence.ps1`、`scripts/verify-r3-release-evidence.ps1`、`scripts/verify-r3-traceability.ps1` 与 `docs/design/jiyishengji/evidence/release-evidence.schema.json`；随后由 P14 独占创建 `docs/design/jiyishengji/evidence/release/<VERSION>/untracked-manifest.json`、`integration-evidence.json`、`memory-evidence.json`、`ocr-evidence.json`、`media-evidence.json`、`ui-evidence.json`。03 Task9、04 Task7、05 Task8 只产出被引用的运行 artifact；任何模块任务均不得创建或覆盖这五份 canonical release JSON。P15 只创建 `signoff.json`。

**Memory 收口前置:** P06 后先执行 06-X1，把真实 source/scope/lifecycle/capture/recall/UI 接到同一产品装配，再执行 03 Task9。P03～P06 的阶段子集不是 X1 替代品；只有在此处同一 `testedSourceHead` 运行完整 M-R01～M-R15，才可给 Memory release evidence 写通过。

### 14.1 自动化命令

`<VERSION>` 不是人工占位符：逐字取仓库根 `VERSION` 的 trim 后值，必须匹配 `^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`。QA 工具提交、X1、Task9 及所有待验产品、测试、schema、dataset、config 必须先形成一个 Git commit，记为 `testedSourceHead`。P14 只能在该 commit 的专用干净 worktree 运行：开始时 `git status --porcelain --untracked-files=no` 必须为空；任何受测文件变化都必须提交成新的 testedSourceHead 并重跑全部门。以下是 `run-r3-release-gates.ps1` 在同一 testedSourceHead 上必须顺序执行并逐条保存机器可读事件的固定命令，任一失败即停止晋级：

~~~powershell
go test -count=1 ./...
go test -race -count=1 ./internal/m8app ./internal/contextapp ./internal/ocrapp ./internal/mediaapp
npm --prefix web run verify:bridge
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
node docs/design/jiyishengji/ui-demo/demo.test.cjs
~~~

`run-r3-release-gates.ps1` 还必须调用 03 Task9、04 Task7、05 Task8 的脚本，把各自 `run-manifest.json` 与实机 artifact 登记进 `artifacts/r3-release/<VERSION>/<runID>/gate-manifest.json`。每条保存 command、start/end、exitCode、testCount、outputDigest、`testedSourceHead`、`sourceTrackedDiffDigest` 和环境；sourceTrackedDiffDigest 必须等于干净 worktree 空 diff 的规范 digest。Go 测试另存 `go test -json` 事件，Vitest 另存 JSON reporter 结果。目标测试数为 0、Skip 关键 Windows 测试、复用旧截图，均视为未执行。

唯一启动命令如下；脚本自行生成 UTC+GUID runID，成功时只在 stdout 最后一行输出 `gate-manifest.json` 的绝对路径，其他日志写入该 run 目录：

~~~powershell
$releaseVersion = (Get-Content -Raw VERSION).Trim()
if ($releaseVersion -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$') { throw "VERSION is not a release version" }
$testedSourceHead = (git rev-parse HEAD).Trim()
if (git status --porcelain --untracked-files=no) { throw "Tracked worktree must be clean before release gates" }
$gateManifestLines = @(& ./scripts/run-r3-release-gates.ps1 -Version $releaseVersion)
$gateRunnerExit = $LASTEXITCODE
if ($gateRunnerExit -ne 0) { throw "R3 release gates failed" }
if ($gateManifestLines.Count -ne 1) { throw "Runner stdout must contain exactly one gate-manifest path" }
$gateManifest = [string]$gateManifestLines[0]
if (-not (Test-Path -LiteralPath $gateManifest -PathType Leaf)) { throw "gate-manifest.json not found" }
~~~

### 14.2 实机矩阵

| 范围 | 必测环境与动作 | 必须证据 |
|---|---|---|
| UI | 1440×900、1024×768、390×844；100/150/200%；键盘、读屏、reduced motion | 截图/录屏、DOM 断言、无横向滚动、焦点顺序 |
| Memory | 新用户、legacy auto/manual/off、scope 组合、10k 事实、重启、离线模型失败 | M-R 全表、保存/泄漏/遗忘/Token/p95 |
| Windows OCR | 真 Windows，语言存在/缺失、初始化失败、固定图、超时 | probe 状态、原图 digest、识别结果和日志 digest |
| Paddle | 仅 verified profile；干净机/断网/取消/篡改/崩溃/200页 | W0 digest、O-R、资源和准确性报告 |
| Media | WebView2 本地音频/视频、4GiB sparse、seek/ended/close/刷新/SMTC/TTS | A-R/V-R、Range、焦点、30分钟与100切换 |
| 安全 | scope spoof、绝对路径扫描、Bridge 变参重放、删除屏障 | 违规调用数=0、敏感路径/正文泄漏=0 |

### 14.3 UI 五分验收

- 设置首页恰两卡；普通用户 30 秒内能找到记忆模式、个人/项目范围、OCR 状态和可选增强。
- 首页/设置/记忆/OCR/媒体均使用同一黑色设计 token；主内容不被蓝灰卡片铺满。
- 高级数据默认不加载；5 名目标用户完成核心任务，中位首次成功≤30 秒，误触后退≤1 次。
- MiniPlayer 不遮聊天输入、审批、toast、抽屉或系统窗口控件。
- Demo 截图只能证明视觉；产品截图必须来自构建产物并包含 HEAD。

### 14.4 canonical evidence 与 anchor linter

- [ ] `build-r3-release-evidence.ps1` 读取本次 `gate-manifest.json`、P00 allocation/baseline、模块 run manifests、P09 runtime evidence 和 14.2/14.3 实机 artifact；先确认它们都绑定逐字相同的 `testedSourceHead`、空 source tracked diff、dataset/config/model digest，或对不可同次生成的 runtime artifact具有明确 digest 父引用。输入缺失、exitCode 非0、testCount=0、关键 Skip 或 artifact digest 不匹配时只能写 `INCOMPLETE/BLOCKED`，不得生成 PASS。
- [ ] builder 在创建 `evidence/release/<VERSION>` 前，使用 P00 相同算法重新快照当前 untracked 普通文件，生成本 release 唯一 `untracked-manifest.json`；目标目录已存在即失败，不覆盖旧证据。五份 evidence JSON 都记录该文件的相对路径和 SHA-256，并固定含 `schemaVersion/releaseVersion/domain/testedSourceHead/sourceTrackedDiffDigest/untrackedManifestDigest/baselineManifestDigest/generatedAt/environment/requirements/commands/artifacts`。每个 requirement 记录 requirementId、status、精确 testId、逐例结果和 artifact digest；路径只用仓库相对路径。release JSON 不写尚不存在、会自引用的 attestation commit hash。
- [ ] `verify-r3-traceability.ps1` 确定性解析 12 中每个 ``计划新增: `path#symbol` `` anchor。Go anchor 要求该文件存在、精确顶层 `func Test...` 存在且本次 `go test -json` 有同名 run/pass；Vitest anchor 要求文件存在、精确同名 test title 在 JSON reporter 中 run/pass；PowerShell `#Scenario` anchor 要求脚本声明并在本次 run manifest 中执行同名 scenario。它还解析 07 的 58 个 `M-R/O-R/A-R/V-R/U-R` scenario ID：先要求每个 ID 至少被 12 §3～§7 的某条 PRD requirement 行逐字引用（区间端点不得代替中间 ID），再要求它在该行列出的至少一个 canonical anchor 下存在精确同名 table-driven subtest/case 事件并实际 run/pass。12 §2 的 `UR-*` 行只做聚合追踪，不参与 parent anchor 校验。07 的英文助记名不是 symbol，不得生成 `Test...` wrapper。basename 命中、`-run` 前缀命中、相似名称或未执行一律失败；一个行为只允许 12 的 canonical anchor，不另建同义 wrapper 测试。
- [ ] 先运行 anchor linter，再由 builder 一次生成五份 JSON，最后验证证据；`-RequireAllP0P1` 失败时 12 对应行保持 `TEST_DEFINED/IMPLEMENTED/BLOCKED`，不得手改成 `VERIFIED`。

~~~powershell
$releaseVersion = (Get-Content -Raw VERSION).Trim()
if (-not (Test-Path -LiteralPath $gateManifest -PathType Leaf)) { throw "Run 14.1 in this PowerShell session first" }
./scripts/verify-r3-traceability.ps1 -Version $releaseVersion -GateManifest $gateManifest
if ($LASTEXITCODE -ne 0) { throw "R3 traceability anchors failed" }
./scripts/build-r3-release-evidence.ps1 -Version $releaseVersion -GateManifest $gateManifest
if ($LASTEXITCODE -ne 0) { throw "R3 evidence build failed" }
./scripts/verify-r3-release-evidence.ps1 -Version $releaseVersion -TestedSourceHead $testedSourceHead -RequireAllP0P1
if ($LASTEXITCODE -ne 0) { throw "R3 evidence verification failed" }
~~~

CLI 与 CI 都必须直接使用 runner 本次 stdout 返回的路径，禁止从多个历史 run 猜选。上述验证通过后，QA 只允许 stage 本版本的 `untracked-manifest.json`、五份 canonical evidence JSON 与 `12-requirements-traceability.md` 状态/路径，提交为 testedSourceHead 的**直接子提交**，记为 `p14AttestationHead`。该提交与 testedSourceHead 的 diff allowlist 逐字为 `docs/design/jiyishengji/evidence/release/<VERSION>/{untracked-manifest.json,integration-evidence.json,memory-evidence.json,ocr-evidence.json,media-evidence.json,ui-evidence.json}` 和 `docs/design/jiyishengji/12-requirements-traceability.md`；出现任何源码、测试、schema、配置、dataset 或其他路径即失败并回到新 testedSourceHead 重跑。提交后 CI 执行 `verify-r3-release-evidence.ps1 -TestedSourceHead <source> -P14AttestationHead <HEAD> -RequireDirectParent -RequireP14Whitelist -RequireAllP0P1`，回算五份 evidence 与所有 artifact digest。这一步才完成 P14；不再要求证据提交后的 current HEAD 等于 testedSourceHead。

## 15. P15：灰度、回滚和五分签字

### 15.1 灰度顺序

1. 所有 gates 默认 off。
2. Memory：shadow write → canonical base read → lifecycle/删除屏障 → auto capture → hybrid → consolidation。
3. OCR：truthful Windows probe 先发布；Paddle 安装 gate 仅对 verified profile 开；手动路由稳定后再考虑 auto-route。
4. Media：false-success 修复常驻；schema/后端 → 内部用户 → MediaCenter → MiniPlayer → activity。
5. 每阶段观察 scope violation、错误率、p95、资源、回滚恢复和用户操作成功率；达到 07 阈值才晋级。

### 15.2 回滚

- Memory：先关 auto/hybrid/consolidation，保留 canonical base reader、tombstone 和 suppression。有 v2-only fact 后禁止回 legacy-only；严格降级只能恢复 P00 备份，并明确展示会丢失的升级后变更。
- OCR：关 auto-route/installer，停止 worker；保留 Windows OCR。激活失败回 previous verified version；无 verified version 则 absent。legacy PP 不参与回滚。
- Media：关 UI 和新 dispatch gate，先停止 owned 声音、释放 ticket/lease，再隐藏 UI；P01 false-success 修复不回滚。
- UI：可隐藏新入口，但不得恢复会假报 ready/success 的旧界面。

### 15.3 5/5 定义

“5/5”不是文档写完即获得，必须同时满足：

- [ ] 12 的全部 P0/P1 行状态为 `VERIFIED`；每行证据绑定同一 testedSourceHead，并能从 p14AttestationHead 打开，P14 diff 仅命中冻结 allowlist。
- [ ] 07 的安全、功能、性能、视觉、恢复门全部通过；关键实机测试无 Skip。
- [ ] 迁移新建/升级/重开/备份/回滚演练通过，无数据或 scope 泄漏。
- [ ] 不存在假 ready、假成功、模拟安装、Renderer 绝对路径或第二业务真相。
- [ ] Token、准确率、时延、资源数字来自冻结集实测，并注明设备与样本；不作行业第一或全设备更优承诺。
- [ ] Paddle W0 未过时，基线产品仍可凭 Windows OCR 真实性和 disabled 硬门达到本期“安全可发布”标准；但不得宣称 Paddle 功能已交付。若产品范围要求 Paddle 可安装，则该行保持 BLOCKED，整体不得签 5/5。
- [ ] P15 的唯一新增文件是 `docs/design/jiyishengji/evidence/release/<VERSION>/signoff.json`；`<VERSION>` 同样逐字取 testedSourceHead 根目录 `VERSION`。产品、设计、开发、QA 四方记录姓名/角色、`testedSourceHead`、`p14AttestationHead`、五份 canonical evidence digest、release untracked manifest digest、签字时间和例外项；例外项非空则不得签满分。将它提交为 p14AttestationHead 的直接子提交后得到 `releaseAttestationHead`；signoff 不自写尚不存在的 releaseAttestationHead。最终 verifier 要求 direct parent=p14AttestationHead，P15 diff 只有该 signoff 文件，并重新校验前两级 allowlist/所有 digest；release tag 只指向 releaseAttestationHead，发布二进制仍逐字绑定 testedSourceHead。

## 16. 开发交接清单

每个工作包合入前，PR 描述必须逐项填写：

- Requirement IDs：来自 12，不允许写“相关需求”。
- Current truth：改动前真实行为和红测失败。
- Contract：消费/产出的 DTO、scope、revision、idempotency、终态。
- Files：实际变更清单；共享文件由谁合并。
- Verification：命令、退出码、测试数、HEAD、evidence 路径。
- Privacy/security：身份来源、路径/正文是否可能进入 Renderer/日志、失败是否 fail closed。
- Rollback：关哪个 gate、保留哪些不可逆数据、如何验证声音/worker/lease 已停止。
- Traceability：同一提交更新 12 的状态和证据；没有证据只能是 `TEST_DEFINED` 或 `BLOCKED`，不能写 `VERIFIED`。

计划中不存在需要开发者自行决定的产品分支。唯一条件分支是 Paddle W0 的客观验证结果；失败路径已经固定为“保持禁用、继续发布真实 Windows OCR 和其他模块”，不得用模拟或弱化 pipeline 绕过。
