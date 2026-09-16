# R3 规范需求追踪矩阵

> 日期：2026-09-15
>
> 追踪基线：`00` 记录的 `b1d58b0d` 仅是 R3 文档形成时的审计快照；实际开工必须由 11 P00/06 X0 重新记录 HEAD、tracked diff digest、untracked manifest digest 与 migration allocation。
>
> 范围：8 项顶层用户需求、`02-upgrade-prd.md` §10 的全部 48 项 requirement、`03`～`07`、`11`、UI Demo 与计划证据。
>
> 当前事实：R3 目标代码、PaddleOCR-VL-1.6 完整包、媒体垂直切片和新增验收测试尚未实现或运行；本矩阵为 **0 `IMPLEMENTED`、0 `VERIFIED`**。

## 1. 使用规则

本文件是需求追踪的唯一索引，不替代产品和技术正文：

1. `08` 与下表 `UR-*` 固定用户意图。
2. `02 §10` 固定产品 requirement ID 和优先级。
3. `02` 固定产品行为，`06 C1～C6` 固定跨模块技术合同，`03`～`05` 负责模块步骤，`11` 负责总依赖顺序，`07` 负责发布门；任意两者真正冲突时，该行必须为 `BLOCKED`，不能靠文档顺序或开发者临场选择。尚未执行已明确排程的 P00/P09 本身不构成合同冲突。
4. `09/ui-demo` 仅是设计证据。Demo 对齐可以记录在 Demo 列，但不改变目标产品的实现/验证状态。
5. 自动测试列写“计划新增”表示文件或测试尚未存在/尚未作为本需求证据运行。即使测试名已在计划中出现，也不因此成为 PASS。
6. 每个 requirement 只有在目标代码、全部自动测试、必须的人工/实机门和证据 artifact 都绑定同一 `testedSourceHead`、空 source tracked diff digest 与 release untracked manifest digest，且 P14 attestation 是它的直接子提交、diff 仅含冻结 evidence/本矩阵 allowlist 后，才能改为 `VERIFIED`。P15 signoff 再作为第二个只含 signoff 的直接子提交；不得要求证据文件记录会因提交自身而改变的 current HEAD。
7. 每个 ``计划新增: `path#symbol` `` 是唯一 canonical 测试创建清单，而不是示例。07 的 `M-R/O-R/A-R/V-R/U-R` 是稳定 scenario ID，不是额外顶层 symbol；对 §3～§7 的 48 条 PRD requirement 行所引用的每个 scenario ID，开发者必须把它实现为该行至少一个 canonical symbol 内的精确同名 table-driven subtest/case。§2 的 `UR-*` 行只做顶层用户意图聚合追踪，不拥有 scenario parent anchor，也不参加逐行 parent 校验。模块计划中的英文描述仅是助记，禁止另建同义 wrapper。P14 的 deterministic anchor linter 必须同时证明 07 的 58 个 scenario ID 全部至少被一条 PRD requirement 行逐字引用、文件存在、精确 parent symbol/test title、精确 scenario ID 存在，且本次机器可读测试事件实际 run/pass；basename、区间端点、前缀或相似名称命中无效。

### 1.1 状态枚举

| 状态 | 含义 |
|---|---|
| `SPECIFIED` | 只有产品行为，尚无足够实施任务或测试。 |
| `PLANNED` | 已指定实施任务，但测试输入/预期仍不足。 |
| `TEST_DEFINED` | 已明确自动测试与必要门禁，但目标尚未实现或未验证。 |
| `DEMO_ALIGNED` | 仅 Demo 与设计一致；只作辅助标签，不作为下方产品 requirement 的主状态。 |
| `IMPLEMENTED` | 目标代码已存在，但发布证据不完整。 |
| `VERIFIED` | 同一冻结版本的自动、人工/实机和证据门全部通过。 |
| `BLOCKED` | 存在尚无处理路径的合同冲突、迁移分配冲突或安全缺口，不能诚实实施/验收。 |
| `STALE` | 曾有证据，但代码或工作树已变化，不能继续用于验收。 |

### 1.2 证据路径

以下路径是固定的计划交付位置；文件不存在或未包含完整元数据时，该需求不得标 `VERIFIED`。

| 简称 | 固定计划证据路径 | 唯一创建阶段/主责 |
|---|---|---|
| `BASE-E` | `docs/design/jiyishengji/evidence/baseline/<UTC>/manifest.json`、同目录 `untracked-manifest.json` 与 `docs/design/jiyishengji/evidence/migration-allocation.json` | P00 / 集成人；目标已存在时禁止覆盖 |
| `INT-E` | `docs/design/jiyishengji/evidence/release/<VERSION>/integration-evidence.json` | P14 / QA evidence builder |
| `MEM-E` | `docs/design/jiyishengji/evidence/release/<VERSION>/memory-evidence.json` | P14 / QA evidence builder；03 Task9 只产运行输入 |
| `OCR-E` | `docs/design/jiyishengji/evidence/ocr-runtime/<digest>/` 与 `docs/design/jiyishengji/evidence/release/<VERSION>/ocr-evidence.json` | runtime 目录由 P09/P10；release JSON 仅 P14 / QA evidence builder |
| `MEDIA-E` | `docs/design/jiyishengji/evidence/release/<VERSION>/media-evidence.json` | P14 / QA evidence builder；05 Task8 只产运行输入 |
| `UI-E` | `docs/design/jiyishengji/evidence/release/<VERSION>/ui-evidence.json` | P14 / QA evidence builder |
| `SIGNOFF-E` | `docs/design/jiyishengji/evidence/release/<VERSION>/signoff.json` | P15 / 产品、设计、开发、QA 四方；不得由 P14 预生成 |

`<VERSION>` 逐字取 testedSourceHead 仓库根 `VERSION` 的 trim 后值，必须满足 11 P14 的 release-version 正则，不由开发者另起目录名。BASE-E 记录其采集时 HEAD、tracked diff digest、untracked manifest 相对路径/digest、migration allocation/digest、环境与采集命令；它不含业务测试结果。五份 release evidence JSON 至少记录 requirementId、精确 testId、`testedSourceHead`、空 `sourceTrackedDiffDigest`、release untracked manifest digest、baseline manifest digest、OS、CPU、RAM、runtime、模型/pack/config/dataset digest、命令、exit code、test count、逐例结果和输出 artifact。P14 提交身份由 testedSourceHead 的直接子提交与冻结路径 allowlist 外部验证，JSON 不自引用 p14AttestationHead；P15 signoff 记录 testedSourceHead 与 p14AttestationHead，最终 release tag 指向只新增 signoff 的 releaseAttestationHead。上述均是计划路径；文件不存在、元数据不全、anchor linter 未通过、提交链/allowlist 不通过或结果未运行时只能保持 `TEST_DEFINED/IMPLEMENTED/BLOCKED`。

### 1.3 总计划交叉表

每个 requirement 行的“实施任务”给出直接模块 Task；同时按下表进入 11 的唯一依赖图：

| 领域 | 11 工作包 | 模块 Task 的含义 |
|---|---|---|
| 共同基线与真实性 | P00、P01 | X0 冻结基线/migration；修复假成功、legacy 假 ready 等现码真实性问题。 |
| Memory | P02～P06、P14、P15 | 设置/存储、来源/迁移、捕获、生命周期/召回、正式 UI、验证与发布证据。 |
| Windows OCR | P07、P08、P14、P15 | 候选 0163、默认 disabled gate、Bridge、真实 probe、legacy 隔离和正式 UI。 |
| PaddleOCR | P09、P10、P14、P15 | P09 只执行 W0；通过后 P10 才实现签名安装、完整 layout+VLM worker 和运行证据。 |
| Tool/Media | P11～P15 | 可信 runtime、正式 UI、垂直切片、验证与发布证据。 |
| 融合 UI | P06、P08、P12～P15 | 各详情页、overview 零请求、Activity 与跨模块正式验收。 |

## 2. 顶层用户需求

| 用户需求 ID | 不可改变的验收语义 | PRD 映射 | 合同与计划 | 自动测试 | 人工/实机门 | Demo | 证据 | 当前状态 |
|---|---|---|---|---|---|---|---|---|
| UR-MEM-01 | 自动保存明确、稳定、未来可复用的信息；不逐条询问。 | MEM-002、004、018 | C1/C2；03 Task3/8/9；11 P04/P06/P14 | M-R11、U-R05；计划新增: `internal/m8app/memory_capture_test.go#TestMemoryCaptureUsefulFacts`；计划新增: `internal/m8app/memory_fabric_eval_test.go#TestMemoryFabricEval` | 07 固定正例、跨会话续接和轻提示任务。 | `demo.test.cjs#Only the explicit valuable example is auto-saved once; greeting and weather are ignored`，仅设计模拟。 | MEM-E | `TEST_DEFINED` |
| UR-MEM-02 | “你好”、感谢、天气/价格/时间查询、一次性命令不进入长期记忆；敏感项不调用提取/embedding。 | MEM-003、018 | C1/C2；03 Task3/9；11 P04/P14 | U-R05；计划新增: `internal/m8app/memory_capture_test.go#TestMemoryCaptureLowValueDrops`；计划新增: `internal/m8app/memory_capture_test.go#TestMemoryDenyBeforeModel` | 300 负例、150 敏感例；逐类结果为 0，失败计入分母。 | 同一现有 Demo 回归只证明交互，不证明模型调用数。 | MEM-E | `TEST_DEFINED` |
| UR-MEM-03 | 用户面只有自动、仅手动、关闭三种互斥模式；manual 仍可明确保存和召回已有记忆，off 不捕获也不注入。 | MEM-002、013、016 | C1/C6；03 Task1/3/8；11 P02/P04/P06 | M-R11/M-R15；计划新增: `internal/app/chat_memory_mode_test.go#TestMemoryModePolicy`；计划新增: `web/src/memory/MemorySettingsPanel.test.tsx#TestMemoryModePolicy` | 刷新、重启、既有设置迁移；每种模式完成保存/召回任务。 | 抽屉展示三种模式；不作为后端门控证据。 | MEM-E、UI-E | `TEST_DEFINED` |
| UR-MEM-04 | 个人记忆、项目记忆两个开关分别控制对应范围的捕获和召回；两者关时零激活。 | MEM-006、013 | C1/C2/C6；03 Task1/3/5/8；11 P02/P04/P05/P06 | M-R03/M-R13；计划新增: `internal/app/chat_memory_mode_test.go#TestMemoryScopeTogglesGateAllPaths`；计划新增: `web/src/memory/MemorySettingsPanel.test.tsx#TestMemoryScopeSwitches`；`demo.test.cjs#The personal memory scope switch actually blocks personal auto-save` | 两主体、两项目和四种开关组合；检查缓存、索引与最终 provider messages。 | 个人 scope 已有可执行设计回归；项目 scope 仍只展示开关。 | MEM-E、INT-E、UI-E | `TEST_DEFINED` |
| UR-UI-01 | 全模块使用统一、简约、纯黑和渐进披露 UI；普通用户不面对底层阈值、路由或 Token。 | MEM-013、OCR-009、MEDIA-007、UI-001～003 | C6；03 Task8；04 Task6；05 Task6/7；11 P06/P08/P12～P14 | U-R01～U-R10/U-R15；计划新增: `web/src/App.r3-ui.test.tsx#TestR3InformationArchitecture` | 390/1024/1440、100/150/200% 缩放、键盘、读屏、reduced motion、5 名用户。 | 13 项 DOM 回归覆盖两卡、单抽屉、OCR、媒体、Activity 与焦点；仍非正式 UI。 | UI-E | `TEST_DEFINED` |
| UR-OCR-01 | Windows OCR 是否可用必须来自真实 WinRT、语言和固定图 probe；失败时显示真实原因，不假 ready。 | OCR-001、006、007、009 | C3/C4/C6；04 Task1/3～7；11 P01/P07/P08/P14 | O-R07/O-R09/O-R11、U-R11/U-R12；计划新增: `internal/doctext/ocr_probe_windows_test.go#TestWindowsOCRProbeStates`；计划新增: `web/src/settings/OCRSettingsPanel.test.tsx#TestWindowsOCRTruthfulStates` | 真 Windows 覆盖六态；非 Windows 只允许 `unsupported_os`。 | `demo.test.cjs#OCR is an automatic status, does not fake Windows readiness, and hard-disables Paddle install` 明示没有 WinRT probe。 | OCR-E、UI-E | `TEST_DEFINED` |
| UR-OCR-02 | PaddleOCR-VL-1.6 仅用户选装；必须为完整 layout+VLM、签名、受管、真实离线包；未过门不得 ready。 | OCR-002～012 | C3/C4/C6；04 Task0～7；11 P07/P09/P10/P14 | O-R01～O-R12、U-R07/U-R08/U-R14；计划新增: `internal/ocrapp/catalog_test.go#TestOCRCatalog`；计划新增: `internal/ocrapp/worker_test.go#TestOCRWorkerFullPipeline` | 干净 Windows x64 CPU、无 Python/缓存、断网两阶段、自测/篡改/取消/崩溃和 200 页盲测。 | 当前 Demo 显示 `NO_VERIFIED_RUNTIME_PROFILE`、安装 disabled 且点击不产生 operation。 | OCR-E | `TEST_DEFINED` |
| UR-MEDIA-01 | 已确认：独立媒体中心、音乐/视频分面、离页按需 MiniPlayer、pause 保留、close 结束、顶部运行状态；无第三方曲库且不假成功。 | TOOL-001～004、MEDIA-001～010、UI-001～004 | C5/C6；05 Task0～8；11 P11～P14 | A-R01～A-R12、V-R01～V-R04、U-R03/U-R09/U-R10；计划新增: `web/src/media/MediaCenterPage.test.tsx#TestNoSessionEmptyState`；计划新增: `web/src/media/VideoPlayerSurface.test.tsx#TestVideoCoreControls` | 真 WebView2 的选择、Range、audio/video、seek、close、恢复；SMTC 目标匹配；30 分钟和 100 次切换。 | Demo 已覆盖初始空态、按需 MiniPlayer、pause/close、音乐/视频切换和 sent/confirmed 区分。 | MEDIA-E、UI-E | `TEST_DEFINED` |

## 3. Memory Fabric v2：18 项

本节计划证据统一写入 `MEM-E`，跨作用域/上下文证据同时写入 `INT-E`。各行的 03 Task 按 §1.3 映射到 11 P02～P06，验证与发布证据统一进入 P14/P15；涉及 migration allocation 的行先经过 P00。

| PRD ID / P | 产品验收摘要 | 规范合同 | 实施任务 | 自动测试 ID / 计划文件 | 人工/实机门 | Demo | 证据 | 当前状态 |
|---|---|---|---|---|---|---|---|---|
| MEM-001 / P0 | canonical正文、版本及candidate/fact/source显式可追溯，无旧payload旁路。 | C1来源/时序；C2注入 | 03 Task1/2；X1 | M-R01/M-R02；计划新增: `internal/storage/sqlite/memory_v2_migration_test.go#TestMemoryV2Migration`；计划新增: `internal/m8app/memory_projection_test.go#TestMemoryProjection` | 随机抽取每类事实，单向追至真实user/tool receipt；改源必须拒绝。 | 不适用：后台数据合同。 | MEM-E、INT-E | `TEST_DEFINED` |
| MEM-002 / P0 | 默认静默自动保存；manual 明确保存只写 canonical；每回合至多一个可撤销 toast，无逐条确认。 | C1 时序；C6 提示 | 03 Task3/4/8；X1 | M-R11/M-R15、U-R05/U-R06；计划新增: `internal/m8app/memory_capture_test.go#TestMemoryCaptureUsefulFacts`；计划新增: `internal/app/memory_v2_handlers_test.go#TestMemoryItemCreateManual`；计划新增: `web/src/session/SessionPage.memory.test.tsx#TestMemoryCaptureToast` | 连续 30 轮普通对话，统计确认弹窗=0、toast≤1/回合、24h 撤销可达；manual 重放单写、auto 无重复 candidate/banner。 | “valuable example”现有 Demo 回归只证明交互选择，不证明正式捕获或 canonical 写入。 | MEM-E、UI-E | `TEST_DEFINED` |
| MEM-003 / P0 | 问候、感谢、即时查询、一次性命令、助手文本和敏感正文硬过滤。 | C1；C2 | 03 Task3/9；X1 | U-R05；计划新增: `internal/m8app/memory_capture_test.go#TestMemoryCaptureLowValueDrops`；计划新增: `internal/m8app/memory_fabric_eval_test.go#TestMemoryFabricEval` | 300 负例+150 敏感例；正文、向量、模型调用和事件 payload 逐项为 0。 | 同一现有 Demo 回归确认问候和天气不新增页面内存记录。 | MEM-E | `TEST_DEFINED` |
| MEM-004 / P0 | 稳定资料、偏好、目标、约束、纠正、流程可按正确 kind/scope 自动保存并去重；持久捕获队列在高水位下继续排空且不静默丢 source。 | C1 自动更新；C2 | 03 Task3/9；X1 | M-R05、U-R05；计划新增: `internal/m8app/memory_capture_test.go#TestMemoryCapturePositiveKinds`；计划新增: `internal/app/chat_memory_workers_test.go#TestMemoryQueueHighWatermarkDrains`；计划新增: `internal/m8app/memory_fabric_eval_test.go#TestMemoryFabricEval` | 300 明确正例；recall≥95%，kind+scope≥90%，重复不新增 current；10000 queued 后追加 100 source，消费继续、游标保留且逐项处理或稳定拒绝。 | Demo 只模拟一类个人稳定偏好，不证明持久队列。 | MEM-E | `TEST_DEFINED` |
| MEM-005 / P0 | 晋升前重读真实来源并重算revision/span digest。 | C1来源合同 | 03 Task2；X1 | M-R01/M-R02；计划新增: `internal/m8app/memory_projection_test.go#TestMemoryProjection`；计划新增: `internal/app/chat_memory_source_test.go#TestMemorySourceValidation` | 中文/CRLF/多part/源被改/源缺失/工具receipt逐例检查。 | 不适用：后台安全合同。 | MEM-E、INT-E | `TEST_DEFINED` |
| MEM-006 / P0 | subject 与 user/workspace/project/expert/session 在读写、索引、导出、UI、缓存一致隔离；个人/项目开关真实门控。 | C1；C2 | 03 Task1/2/5/8；X1；11 P02/P03/P05/P06 | M-R03/M-R13；计划新增: `internal/app/chat_memory_mode_test.go#TestMemoryScopeTogglesGateAllPaths`；计划新增: `internal/contextapp/memory_scope_test.go#TestMemoryScopeFinalMessages` | 两主体、同 ID 不同 kind、两项目、expert/session 及四种 UI 开关组合。 | 个人 scope 已在 Demo 中真实阻断演示自动保存；项目 scope 尚无独立捕获场景。 | MEM-E、INT-E、UI-E | `TEST_DEFINED` |
| MEM-007 / P0 | 用户纠正新建版本并正确关闭旧有效期；当前只注入新值，历史可查。 | C1 自动更新/历史 | 03 Task4/5；X1 | M-R06/M-R10；计划新增: `internal/m8app/memory_lifecycle_v2_test.go#TestMemoryLifecycleCorrection`；计划新增: `internal/m8app/memory_recall_v2_test.go#TestMemoryHistoricalRecall` | 上海→杭州及并发 CAS；当前/过去问题分别验证。 | Demo 只演示纠正文字，不演示 canonical 版本链。 | MEM-E | `TEST_DEFINED` |
| MEM-008 / P0 | 单条、版本、禁记和分范围 purge 均可验证；忘记后所有记忆派生面无正文且不复活。 | C1 遗忘/备份 | 03 Task4/7/8；X1 | M-R07/M-R09；计划新增: `internal/m8app/memory_lifecycle_v2_test.go#TestMemoryForgetAndPurge`；计划新增: `internal/app/m8_memory_handlers_test.go#TestMemoryPurgePrepareContract` | 原聊天边界、导入旧 archive、缓存、generation、legacy mirror；prepare mutation 的 scope/idempotency/operationId 与一次性服务端 grant。 | Demo 只演示列表移除和“原聊天保留”边界。 | MEM-E、INT-E | `TEST_DEFINED` |
| MEM-009 / P1 | FTS/dense/time/relation/pinned多路召回、统一融合去重并可降级。 | C2预算；C1历史 | 03 Task5；X1 | M-R10；计划新增: `internal/m8app/memory_recall_v2_test.go#TestMemoryRecallRoutes` | 1k/10k/100k规模、各route关闭、deadline和Hit@5基线。 | 不适用：后台检索合同。 | MEM-E | `TEST_DEFINED` |
| MEM-010 / P1 | 精确tokenizer可用才标exact，否则冻结估算×1.15；完整序列化不越预算。 | C2 | 03 Task5/9；X1 | M-R12；计划新增: `internal/m8app/memory_recall_v2_test.go#TestMemoryTokenBudget`；计划新增: `internal/domain/token/provider_tokenizer_test.go#TestProviderTokenizerMetadata` | 多provider、unknown model、超长后压缩；完整trace审计。 | 不适用：UI不得暴露底层Token参数。 | MEM-E | `TEST_DEFINED` |
| MEM-011 / P1 | 使用/纠正反馈只影响排序或审阅，不能自证或升权。 | C1权威；C2低信任 | 03 Task5/6 | 计划新增: `internal/m8app/memory_feedback_test.go#TestMemoryFeedbackCannotPromoteAuthority` | helpful/dismiss/contradicted/correction逐类检查authority与最终messages。 | 不适用：低频高级管理。 | MEM-E | `TEST_DEFINED` |
| MEM-012 / P1 | consolidation为copy-on-write generation；失败/取消不改active，激活CAS；四个公开方法有唯一 schema/DTO。 | C1历史/遗忘 | 03 Task6 | 计划新增: `internal/m8app/memory_consolidation_test.go#TestMemoryGeneration`；计划新增: `internal/app/memory_generation_handlers_test.go#TestMemoryGenerationBridgeContract` | 构建中新增、纠正、忘记、并发激活、回退、discard fencing、分页/越权/幂等和崩溃恢复。 | 不适用：高级管理仅展示preview。 | MEM-E、INT-E | `TEST_DEFINED` |
| MEM-013 / P1 | 记忆中心可搜索、查看来源、纠正、忘记、撤销、审阅和隐私设置，保持极简。 | C6；C1 | 03 Task8；X1；11 P06 | U-R04/U-R06/U-R10；计划新增: `web/src/memory/MemoryPage.test.tsx#TestMemoryCenterContract`；计划新增: `web/src/memory/MemorySettingsPanel.test.tsx#TestSingleMemoryDrawer` | 键盘、读屏、200% 缩放、刷新恢复、5 名用户任务。 | Demo 已对齐只读状态头、单列列表和单一设置抽屉；仅作设计证据。 | MEM-E、UI-E | `TEST_DEFINED` |
| MEM-014 / P0 | 完整v2导出、不可变预演、显式commit，round-trip保持current/删除语义；preview/commit 有唯一 schema/DTO。 | C1备份/导入导出 | 03 Task7/8；X0/X1 | M-R07/M-R08；计划新增: `internal/m8app/memory_import_test.go#TestMemoryImportExport`；计划新增: `internal/app/memory_import_handlers_test.go#TestMemoryImportBridgeContract`；计划新增: `internal/storage/sqlite/memory_export_test.go#TestMemoryExportConcurrentSnapshot` | legacy+M8+v2+墓碑、64MiB/100000行边界、受权artifact、preview零active写、幂等/过期/源丢失、恢复演练。 | 高级抽屉仅展示入口；不能作round-trip证据。 | MEM-E、INT-E | `TEST_DEFINED` |
| MEM-015 / P0 | 存量迁移可重入、审计、降级；v2-only 事实不丢，旧接口不复活墓碑。 | C1 备份/迁移 | 03 Task0/1/2/7/9；X0/X1；11 P00/P02/P03/P05/P14 | M-R14；计划新增: `internal/storage/sqlite/memory_v2_migration_test.go#TestMemoryV2Migration`；计划新增: `internal/storage/sqlite/memory_upgrade_backup_test.go#TestMemoryPreMigrationBackupContains0159`；计划新增: `internal/m8app/memory_import_test.go#TestMemoryRollback` | P00 扫描后冻结实际 migration allocation；真实生产库备份→升级→三次重入→功能降级→旧备份恢复。 | 不适用。候选号未在 P00 冻结是计划前置条件，不是当前合同冲突。 | BASE-E、MEM-E | `TEST_DEFINED` |
| MEM-016 / P1 | 无提取/embedding模型时fast path与FTS仍工作，未处理项明确deferred。 | C2 | 03 Task3/5/9 | M-R11/M-R12；计划新增: `internal/m8app/memory_offline_test.go#TestMemoryOfflineDegradation` | 断网、无模型、预算0、模型错误；聊天不阻断且无未授权远端调用。 | Demo不连接模型，只能说明概念。 | MEM-E | `TEST_DEFINED` |
| MEM-017 / P1 | 生成式整理不能改写权威来源；working/import/偏好等不可信正文只能进入 data slot，不能升权为 system/developer/tool 角色；generation 只引用输入版本。 | C1权威/历史；C2 注入 | 03 Task5/6/9 | M-R04；计划新增: `internal/m8app/memory_consolidation_test.go#TestGenerationCannotRewriteEvidence`；计划新增: `internal/contextapp/memory_injection_safety_test.go#TestMemoryInjectionSafety` | 恶意/错误摘要、伪 role 分隔符、忽略审批指令、discard、回退及 source digest 核对；最终 provider messages 的自由文本升权数为 0。 | 不适用。 | MEM-E、INT-E | `TEST_DEFINED` |
| MEM-018 / P1 | deny/禁记先于提取与embedding，命中时远端调用为0。 | C1时序；C2隐私 | 03 Task3/4/9；X1 | 计划新增: `internal/m8app/memory_capture_test.go#TestMemoryDenyBeforeModel` | “不要保存位置”、敏感值、引用注入、已有队列竞态。 | Demo只显示过滤文案。 | MEM-E、INT-E | `TEST_DEFINED` |

## 4. OCR：12 项

本节计划证据统一写入 `OCR-E`，跨作用域消费者证据同时写入 `INT-E`。Windows 真实性基线按 11 P07/P08 实施；P09 只执行 W0，只有通过后才进入 Paddle 专属 P10；P14/P15 统一收集验证与发布证据。

| PRD ID / P | 产品验收摘要 | 规范合同 | 实施任务 | 自动测试 ID / 计划文件 | 人工/实机门 | Demo | 证据 | 当前状态 |
|---|---|---|---|---|---|---|---|---|
| OCR-001 / P0 | 主程序不下载 Paddle；Windows OCR 仅在真实 probe 可用时作为快速/灾备，原路径不回归。 | C3/C4/C6 | 04 Task1/3/4/6/7；X2；11 P01/P07/P08 | O-R07/O-R09；计划新增: `internal/doctext/ocr_probe_windows_test.go#TestWindowsOCRProbeStates`；计划新增: `web/src/settings/OCRSettingsPanel.test.tsx#TestWindowsOCRTruthfulStates` | 真 Windows 覆盖语言、初始化、固定图和超时；全新安装确认无 Paddle 下载。 | Demo 明示没有 WinRT probe，未显示 Windows ready；Paddle 硬禁用。 | OCR-E、UI-E | `TEST_DEFINED` |
| OCR-002 / P0 | 只能从签名catalog安装，manifest和每个文件digest fail closed。 | C4 | 04 Task0/2/7 | 计划新增: `internal/ocrapp/catalog_test.go#TestOCRCatalog`；计划新增: `internal/ocrapp/installer_test.go#TestOCRInstallRejectsTampering` | 发布签名cross-vector、未知/撤销/过期key、篡改文件、zip-slip。 | Demo只应标“模拟”，不能证明签名。 | OCR-E | `TEST_DEFINED` |
| OCR-003 / P0 | verified runtime profile 是安装前硬门；通过后预检、异步进度、取消、失败清理、重试和重启恢复均持久化。 | C4/C6 | 04 Task1/2/5/6/7；11 P07/P08/P10 | O-R05/O-R08/O-R12、U-R08；计划新增: `internal/ocrapp/installer_test.go#TestOCRInstallRecovery`；计划新增: `internal/app/ocr_pack_handlers_test.go#TestOCRPackBridge` | 无 profile/legacy marker/签名 profile、下载中断、激活前后断电、取消边界、旧版本继续 ready；前两类 mutation=0。 | 当前 Demo 只覆盖 W0 未过时的硬禁用，不模拟可安装后的恢复生命周期。 | OCR-E、UI-E | `TEST_DEFINED` |
| OCR-004 / P0 | 必须运行完整layout+VLM并报告真实pipeline/version；缺一阶段不得full。 | C4 | 04 Task0/3/7 | 计划新增: `internal/ocrapp/worker_test.go#TestOCRWorkerFullPipeline`；计划新增: `scripts/test-paddleocr-vl-pack.ps1#CleanOffline` | 干净Windows断网两页，记录两个stage/model identity和输出。 | Demo不运行模型，不适用作证据。 | OCR-E | `TEST_DEFINED` |
| OCR-005 / P0 | 受信签名worker应用级offline；协议、PDF逐页渲染、资源和输出均有界；不谎称Job Object是网络/文件沙箱。 | C4 | 04 Task0/3/7 | O-R03；计划新增: `internal/ocrapp/worker_test.go#TestOCRWorkerIsolationLimits`；计划新增: `internal/doctext/pdf_render_stream_windows_test.go#TestPDFRenderBounded`；计划新增: `scripts/test-paddleocr-vl-pack.ps1#WorkerTimeout` | URL/越权引用、凭据/代理不继承、挂死、输出炸弹、100页/20MP/取消/同时页数≤2、进程树回收；报告明确无OS级egress保证。 | Demo CSP只证明原型不联网。 | OCR-E | `TEST_DEFINED` |
| OCR-006 / P0 | 文本层、显式 user/project scope、策略、安装健康和复杂度确定性路由；现有 PP-OCR 设置有明确迁移。 | C3/C4 | 04 Task1/4/5/7；X2；11 P07/P10 | O-R01/O-R02/O-R07/O-R10；计划新增: `internal/ocrapp/recognize_test.go#TestOCRRoute`；计划新增: `internal/app/s1_bridge_handlers_test.go#TestOCRRoutingScopeContract`；计划新增: `internal/ocrapp/routing_migration_test.go#TestLegacyPPOCRSettingsMigration` | 两主体切换、user/project oneOf、项目继承与 CAS、越权、策略中途变化、provider 隐私、PP-OCR 旧设置升级且调用数为 0。 | Demo 高级区把 legacy PP-OCR 标为“已登记但未接线”；不决定实际 scope/CAS/迁移结果。 | OCR-E、INT-E | `TEST_DEFINED` |
| OCR-007 / P0 | Paddle任一故障不破坏Windows/provider；旧current仍可用。 | C3/C4 | 04 Task2/3/4/7；X2 | O-R05/O-R08；计划新增: `internal/ocrapp/recognize_test.go#TestOCRFallbackFaults`；计划新增: `scripts/test-paddleocr-vl-pack.ps1#ActivationCrash` | 缺包、签名失败、启动失败、超时、崩溃逐项故障注入。 | Demo只展示“Windows不受影响”文案。 | OCR-E | `TEST_DEFINED` |
| OCR-008 / P1 | 纯文本、Markdown、layout、warning和confidence语义分离；兼容旧Result。 | C3/C4 | 04 Task3/4/5/7；X2 | O-R06/O-R07；计划新增: `internal/ocrapp/result_test.go#TestOCRResultCompatibility` | 表格/公式/印章/缺confidence/超1MiB legacy Text；逐页核验。 | 现行 Demo 不展示识别结果，不适用作 OCR-008 证据。 | OCR-E | `TEST_DEFINED` |
| OCR-009 / P1 | UI 只暴露简约策略和选装动作；probe 摘要来自真实 snapshot，语言/provider/legacy/pipeline/许可按需展开且 provider 故障隔离。 | C6/C4 | 04 Task5/6/7；11 P08/P10 | O-R11、U-R07/U-R08/U-R11/U-R12/U-R13/U-R14；计划新增: `web/src/settings/OCRSettingsPanel.test.tsx#TestOCRProgressiveDisclosure`；计划新增: `web/src/settings/OCRModelPackCard.test.tsx#TestOCRManifestConsent` | 键盘、读屏；provider reject/恢复不遮蔽核心；六态 probe；legacy PP-OCR 只显示“已登记，尚未接线”且不泄露路径；W0 disabled、安装、取消、失败、恢复；真实 manifest/preflight 数值。 | Demo 已对齐只读自动摘要、probe 未知、Paddle 硬禁用和高级诊断分层。 | OCR-E、UI-E | `TEST_DEFINED` |
| OCR-010 / P0 | 200页盲测决定auto route；厂商指标不替代，未过门仅手动且W0失败不得安装。 | C3/C4 | 04 Task0/7 | 计划新增: `scripts/ocr-eval.ps1#OCR200PageGate`；计划新增: `internal/ocrapp/ocr_eval_test.go#TestOCRAutoRouteGate` | 200页双人标注、同硬件配置、CER/WER/结构/成功率/p95/RSS完整。 | Demo不能证明准确率或auto gate。 | OCR-E | `TEST_DEFINED` |
| OCR-011 / P1 | 卸载等待运行 lease；忙则保留 current；模型删除但结果元数据/NOTICE 保留。 | C4 | 04 Task1/2/5/6/7；11 P10 | O-R04/O-R08；计划新增: `internal/ocrapp/installer_test.go#TestOCRUninstallLeases`；计划新增: `internal/storage/sqlite/ocr_model_packs_test.go#TestOCRNoticeRetentionOffline` | 运行中卸载、30 秒 busy、重启孤儿、artifact 读取 lease；卸载后断网 list/read retained NOTICE。 | 当前 Demo 没有卸载流程，不适用作 lease/NOTICE 证据。 | OCR-E | `TEST_DEFINED` |
| OCR-012 / P1 | catalog、安装目录、设置/关于页均可离线查看真实许可证和修改声明。 | C4/C6 | 04 Task0/1/2/5/6/7；11 P09/P10 | 计划新增: `internal/ocrapp/catalog_test.go#TestOCRLicenseManifest`；计划新增: `internal/storage/sqlite/ocr_model_packs_test.go#TestOCRNoticeRetentionOffline`；计划新增: `web/src/settings/OCRModelPackCard.test.tsx#TestOCRLicenseLink` | 断网查看 retained SBOM/NOTICE、依赖逐项许可、版本/来源一致；catalog 换版后历史受信 digest 仍可分页读取。 | W0 未过时不展示虚构许可详情；Demo 不能作为真实许可证据。 | OCR-E | `TEST_DEFINED` |

### 4.1 Paddle W0 状态规则

- 当前 P09/W0 未运行，所以 Paddle 相关行只能是 `TEST_DEFINED`，不能是 `IMPLEMENTED` 或 `VERIFIED`。
- P07/P08 必须先交付真实 Windows probe、legacy 隔离、`ocr.routing.get`、`ocr.pack.get` 和 `NO_VERIFIED_RUNTIME_PROFILE` 的零 mutation 基线；它们不依赖 W0 成功。
- P09 通过只代表允许进入 P10，仍需完成签名安装器、完整 layout+VLM worker、故障恢复、200 页门和同版本 evidence 后才能升级状态。
- P09 客观失败时，受影响 requirement 行转为 `BLOCKED`，P10 与“本期可安装 Paddle”的发布证据标 `INCOMPLETE`；Windows OCR、Memory 和 Media 继续推进，UI 保持 disabled、mutation/operation/download 为 0。

## 5. 自动工具：4 项

本节计划证据写入 `MEDIA-E`，跨域活动写入 `INT-E`。各行的 05 Task 按 §1.3 进入 11 P11～P13，验证与发布证据进入 P14/P15。

| PRD ID / P | 产品验收摘要 | 规范合同 | 实施任务 | 自动测试 ID / 计划文件 | 人工/实机门 | Demo | 证据 | 当前状态 |
|---|---|---|---|---|---|---|---|---|
| TOOL-001 / P0 | 回执区分接收、审批、派发、核验和终态；verificationStatus 与 verificationSource 分离，成功必有证据。 | C5/C6 | 05 Task0/2/7/8；X3 | A-R01；计划新增: `internal/toolruntime/media_foreground_test.go#TestMediaKeyWithoutReadbackIsUncertain`；计划新增: `web/src/media/MediaOperationCard.test.tsx#TestTruthfulOperationStates` | 真SMTC有/无回读、目标不符；逐条核成功文案与 source/status 枚举。 | 运行状态抽屉区分“待核验/已确认”，仅示例。 | MEDIA-E、INT-E | `TEST_DEFINED` |
| TOOL-002 / P0 | 审批、capability、急停、桌面串行、hook和审计不得被新入口绕过。 | C5 | 05 Task2/7/8；X3 | A-R10；计划新增: `internal/mediaapp/service_test.go#TestMediaGovernanceBeforeDispatch` | accepted后dispatch前撤权/急停、Agent与用户手动入口分别验证。 | Demo不执行工具，不适用作证据。 | MEDIA-E、INT-E | `TEST_DEFINED` |
| TOOL-003 / P1 | activity/media snapshot和event可重连，刷新不重复执行。 | C5/C6 | 05 Task5/7/8；X3 | A-R04/A-R05/A-R08/A-R09；计划新增: `internal/app/media_handlers_test.go#TestMediaWatchReconnect`；计划新增: `web/src/activity/activitySnapshot.test.ts#TestActivityReconnect` | WebView刷新、Engine重启、sequence gap和多窗口。 | Demo页面内存不持久，不能证明重连。 | MEDIA-E、INT-E | `TEST_DEFINED` |
| TOOL-004 / P1 | 失败卡片提供真实恢复；重试新建 operation 并关联父记录。 | C6 | 05 Task2/7/8；X3 | A-R09/A-R10；计划新增: `internal/app/activity_handlers_test.go#TestActivityRecoveryCreatesChildOperation`；计划新增: `web/src/activity/ActivityCenter.test.tsx#TestActivityRecoveryAction` | failed/uncertain、取消不可撤回、不可恢复四类任务。 | Demo 显示示例，没有可执行恢复闭环。 | MEDIA-E、INT-E | `TEST_DEFINED` |

## 6. Media：10 项

本节计划证据统一写入 `MEDIA-E`，视觉和交互证据同时写入 `UI-E`。各行的 05 Task 按 §1.3 进入 11 P11～P13，验证与发布证据进入 P14/P15。

| PRD ID / P | 产品验收摘要 | 规范合同 | 实施任务 | 自动测试 ID / 计划文件 | 人工/实机门 | Demo | 证据 | 当前状态 |
|---|---|---|---|---|---|---|---|---|
| MEDIA-001 / P0 | MediaSession统一owned/external，但能力和可信度分型。 | C5 | 05 Task1/2/5 | 计划新增: `internal/mediaapp/service_test.go#TestMediaOriginCapabilities`；计划新增: `web/src/media/mediaSnapshot.test.ts#TestOwnedExternalCapabilities` | owned/external各完成play/pause/unsupported seek/queue任务。 | Demo只展示owned视觉，不覆盖external。 | MEDIA-E | `TEST_DEFINED` |
| MEDIA-002 / P0 | 只发送媒体键不得成功；无SMTC回读为uncertain/MEDIA_UNVERIFIED。 | C5/C6 | 05 Task0/2/8 | A-R01；计划新增: `internal/toolruntime/media_foreground_test.go#TestMediaKeyWithoutReadbackIsUncertain` | 真Windows无SMTC和错误目标；中文不得写“已播放”。 | 运行状态有“已发送，待核验”示例。 | MEDIA-E | `TEST_DEFINED` |
| MEDIA-003 / P0 | owned只处理显式 user/project scope 下的用户选择、授权workspace或合法artifact；未知URL/路径/脚本元数据拒绝。 | C5 | 05 Task3/4/5/8 | A-R07；计划新增: `internal/desktopmedia/handler_test.go#TestMediaAssetAuthorization`；计划新增: `internal/app/media_private_handlers_test.go#TestPrivateMediaRPC`；计划新增: `internal/app/media_handlers_test.go#TestMediaScopePayloadContract` | user/project oneOf、picker 私有登记、UNC/reparse/ADS/越scope/文件替换/renderer调用私有RPC。 | Demo声明无第三方曲库；不读取文件或证明 scope 鉴权。 | MEDIA-E | `TEST_DEFINED` |
| MEDIA-004 / P1 | owned 播放、暂停、关闭、seek、音量、队列、自动下一首和断点；刷新只恢复 paused；与 TTS/ASR 共享可核验音频焦点。 | C5 | 05 Task1/5/6/8；X3；11 P11/P12/P13 | A-R02/A-R04/A-R05/A-R06/A-R11/A-R12、V-R01/V-R02/V-R03/V-R04；计划新增: `internal/mediaapp/service_test.go#TestOwnedMediaLifecycle`；计划新增: `internal/mediaapp/audio_focus_test.go#TestMediaAudioFocus`；计划新增: `web/src/media/OwnedMediaPlayer.test.tsx#TestOwnedPlaybackEvents` | 真 WebView2 音/视频、seek、ended、close、恢复、30 分钟、100 次切换；TTS、麦克风、手动切歌/换设备/退出逐项验证不串音、不隐藏恢复。 | Demo 分离音乐/视频界面，音乐与视频主按钮均可切换；不处理真实媒体字节或真实音频焦点。 | MEDIA-E | `TEST_DEFINED` |
| MEDIA-005 / P1 | external优先SMTC，媒体键/UIA仅低可信fallback；只开放实际capability。 | C5 | 05 Task2/5/8 | A-R01/A-R03/A-R10；计划新增: `internal/winexec/media_session_windows_test.go#TestSMTCTargetAndCapabilities` | 多app/multi-session、标题延迟、不匹配、退出、capability撤销。 | Demo不覆盖external。 | MEDIA-E | `TEST_DEFINED` |
| MEDIA-006 / P1 | command/queue/player report使用CAS、幂等、epoch和eventSeq；乱序不得重复副作用。 | C5 | 05 Task1/5/6/8 | A-R02/A-R08/A-R09；计划新增: `internal/mediaapp/service_test.go#TestMediaCASAndIdempotency`；计划新增: `internal/app/media_handlers_test.go#TestMediaPlayerEpoch` | 双击next、旧epoch ended、重复directive、lease接管。 | Demo直接改本地state，不能证明CAS。 | MEDIA-E | `TEST_DEFINED` |
| MEDIA-007 / P1 | 办公组常驻一级 MediaCenter、MediaMiniPlayer、Queue、OperationCard 共享唯一 snapshot；无 session 为空态，pause 保留，close 终态后隐藏。 | C5/C6 | 05 Task5/6/7/8；X3；11 P12/P13 | A-R04/A-R11/A-R12、U-R03/U-R09；计划新增: `web/src/app/LaunchSidebar.test.tsx#TestMediaCenterOfficeNavigation`；计划新增: `web/src/media/MediaCenterPage.test.tsx#TestNoSessionEmptyState`；计划新增: `web/src/media/MediaMiniPlayer.test.tsx#TestMediaMiniPlayerContract` | 跨 home/settings/project/office/AgentHub；officeMenu toggle 不隐藏媒体；AgentHub replaceMainNav/header 不遮挡；stop 失败或 uncertain 时不得先隐藏。 | Demo 已对齐办公组入口、初始空态、创建会话后按需 MiniPlayer、pause 保留和 close 隐藏；页面内存模拟不覆盖真实 App/AgentHub 挂载或等待后端终态。 | MEDIA-E、UI-E | `TEST_DEFINED` |
| MEDIA-008 / P1 | 单次播放不写长期偏好；derived observation仍需多证据和审阅。 | C1/C5 | 03 Task3/6；05 Task2/8 | 计划新增: `internal/mediaapp/service_test.go#TestPlaybackDoesNotCreateLongTermMemory`；计划新增: `internal/m8app/memory_capture_test.go#TestMediaObservationRequiresReview` | 单次/重复播放、历史关闭、记忆off、scope切换。 | Demo不接Memory后台。 | MEDIA-E、MEM-E | `TEST_DEFINED` |
| MEDIA-009 / P0 | 不抓取、下载、转码或绕过第三方受保护内容；无相应工具动作。 | C5 | 05 Task2/3/4/8 | A-R07；计划新增: `internal/mediaapp/service_test.go#TestProtectedRemoteMediaRejected`；计划新增: `internal/contract/media_schema_test.go#TestNoProtectedContentActions` | URL、DRM、会员、remote redirect和未知origin fail closed。 | Demo明确无曲库、下载、DRM。 | MEDIA-E | `TEST_DEFINED` |
| MEDIA-010 / P0 | 本地大文件走Host私有登记和WebView2单Range broker；Renderer无路径/ticket映射。 | C5 | 05 Task3/4/8 | A-R06/A-R07；计划新增: `internal/webviewhost/media_resource_test.go#TestMediaRange`；计划新增: `internal/mediaapp/service_test.go#TestMediaTicket` | GET/HEAD/200/206/416/multi-range、4GiB sparse、导航取消、handle回收。 | Demo无媒体字节，不适用作证据。 | MEDIA-E | `TEST_DEFINED` |

## 7. UI：4 项

本节计划证据统一写入 `UI-E`，活动真实状态同时写入 `INT-E`。各行按具体详情页进入 11 P06/P08/P12，跨模块垂直切片、验证和发布证据进入 P13～P15。

| PRD ID / P | 产品验收摘要 | 规范合同 | 实施任务 | 自动测试 ID / 计划文件 | 人工/实机门 | Demo | 证据 | 当前状态 |
|---|---|---|---|---|---|---|---|---|
| UI-001 / P1 | Activity 从顶部入口汇总 tool/OCR/media，以 exact filter/DTO 分页恢复，不占左侧主导航。 | C6 | 04 Task6；05 Task7/8；X3；11 P08/P12/P13 | A-R08/A-R09、U-R03；计划新增: `internal/app/activity_handlers_test.go#TestActivityListPagination`；计划新增: `internal/app/activity_handlers_test.go#TestActivityExactDTO`；计划新增: `web/src/activity/ActivityStatusButton.test.tsx#TestActivityTopEntry` | >200 条、同时间戳、domains unique/empty/unknown、多 scope、null 时间、source/status 映射、刷新、失败恢复。 | Demo 顶部运行状态入口与侧栏方向一致，区分 sent/pending 与 confirmed；仅静态数据。 | UI-E、INT-E | `TEST_DEFINED` |
| UI-002 / P1 | 状态使用文字、图标和 ARIA，不只靠颜色；智能能力 overview 零预取且焦点返回；缩放、读屏可用。 | C6 | 03 Task8；04 Task6；05 Task6/7/8；X3；11 P06/P08/P12～P14 | U-R10/U-R15；计划新增: `web/src/accessibility/r3_accessibility.test.tsx#TestR3StateAccessibility`；计划新增: `web/src/settings/SmartCapabilitiesPanel.test.tsx#TestSmartCapabilitiesOverviewContract` | axe serious/critical=0；overview 数据调用=0、两卡进出焦点；真 WebView2 键盘、读屏、高对比、200%。 | Demo 有 overview 零请求、ARIA、主题和 Esc 焦点回归，但不能替代正式 UI/读屏。 | UI-E | `TEST_DEFINED` |
| UI-003 / P1 | 复用现有 theme tokens、纯黑和中文规范，不建立第二套全局主题。 | C6 | 03 Task8；04 Task6；05 Task6/7/8；11 P06/P08/P12～P14 | U-R01/U-R02/U-R10；计划新增: `web/src/App.r3-ui.test.tsx#TestR3ThemeContract` | 亮暗主题、多视口截图、reduced motion、5 人可用性。 | Demo 视觉方向一致；正式组件尚未实现。 | UI-E | `TEST_DEFINED` |
| UI-004 / P1 | UI、Bridge 和日志不展示密钥、完整路径、ticket 映射或未消毒 HTML。 | C3/C4/C5/C6 | 03 Task8；04 Task6/7；05 Task3/4/7/8；X2/X3；11 P06/P08/P12～P14 | A-R07；计划新增: `web/src/security/r3_rendering.test.tsx#TestR3UntrustedContentRendering`；计划新增: `internal/app/r3_redaction_test.go#TestR3BridgeAndLogRedaction` | 恶意 memory/OCR/media 文本、路径和 URL；debug 导出需明确授权。 | Demo 有输入转义和 CSP 检查，只能作为设计辅助。 | UI-E、INT-E | `TEST_DEFINED` |

## 8. 完整性与当前状态汇总

| 分组 | Requirement 数 | `TEST_DEFINED` | `BLOCKED` | `IMPLEMENTED` | `VERIFIED` |
|---|---:|---:|---:|---:|---:|
| Memory | 18 | 18 | 0 | 0 | 0 |
| OCR | 12 | 12 | 0 | 0 | 0 |
| Tool | 4 | 4 | 0 | 0 | 0 |
| Media | 10 | 10 | 0 | 0 | 0 |
| UI | 4 | 4 | 0 | 0 | 0 |
| **PRD 合计** | **48** | **48** | **0** | **0** | **0** |

8 项顶层用户需求同样全部为 `TEST_DEFINED`，0 `IMPLEMENTED`、0 `VERIFIED`。这只说明产品行为、任务和测试门已定义，不说明目标代码已经存在。

当前没有开放的源合同 `BLOCKED` 行：先前的 scope、Memory UI、Windows 状态、legacy 迁移、OCR UI、媒体 Demo 与依赖顺序冲突均已在 R3 文档中收敛。P00 尚未生成 migration allocation、P09/W0 尚未运行属于计划前置门，因此保持 `TEST_DEFINED`；若执行时发现无法消解的占号，或 W0 客观失败而本期仍要求 Paddle 可安装，再把受影响 requirement 行转为 `BLOCKED`、发布证据标 `INCOMPLETE`，绝不能转为 `VERIFIED`。

## 9. 状态升级检查

维护者每次修改状态时必须同时完成：

1. 更新本行的产品/合同/Task/Test映射，不能只改状态文字。
2. `TEST_DEFINED → IMPLEMENTED`：目标迁移、代码、schema和正式接线已存在；相关计划新增测试已经落盘并实际失败后转绿。
3. `IMPLEMENTED → VERIFIED`：自动测试、人工/实机门和证据路径全部存在，且绑定同一 testedSourceHead、空 source tracked diff digest 与 release untracked manifest digest；p14AttestationHead 是其直接子提交且只含冻结 evidence/本矩阵 allowlist。
4. 任何相关代码、schema、dataset、模型pack或测试配置变化后，旧证据先降为 `STALE`，重跑后才能恢复 `VERIFIED`。
5. Demo 对齐只能增加设计信心；不得把 requirement 状态从 `TEST_DEFINED` 直接改成 `VERIFIED`。
6. `BLOCKED` 行必须先在 [10 最终审计](10-final-audit-and-five-point-remediation.md) 的缺陷台账记录原因；只有源合同和处理路径重新一致后才进入实现。
7. P14 只有在 `verify-r3-traceability.ps1` 对本文件全部 canonical anchors 验证“精确定位 + 本次 run/pass + canonical evidence 绑定”后，才可写五份 release evidence 并逐行升级状态；计划中的 prefix-only 命令不能替代该检查。

当本矩阵全部 P0/P1 行为 `VERIFIED`、10 的 P0/P1 全部关闭、且07发布门没有 `INCOMPLETE/NOT_RUN` 时，才具备本期5/5验收资格。
