# OCR Truthful Baseline and PaddleOCR-VL-1.6 Optional Pack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 先把真实 Windows OCR、作用域路由和“智能能力”两卡 UI 收敛成诚实基线；只有存在已验证 Windows runtime profile 时，用户才可安装完整 PaddleOCR-VL-1.6 复杂文档包。
**Architecture:** 复用 Settings 的 `personal` category 作为唯一“智能能力”入口，首页只有“自动记忆”和“文字识别”两张卡，OCR 不再重复出现在 `routing`。Go Engine 对每次识别固定 subject/scope/policy/provider/pack 快照；Windows OCR 用真实 WinRT probe，旧 PP-OCR 目录只保留为未接线登记；可选 Paddle pack 由签名 catalog、SQLite 状态、后台 runner 和受管 Python stdio worker组成，前端只消费可恢复 snapshot。
**Tech Stack:** 当前 Go/modernc SQLite、Windows Job Object、Python/Paddle 官方完整 pipeline、React/TypeScript、JSON Schema Bridge。
**Spec:** [完整 PRD](02-upgrade-prd.md)，OCR-001～012。
**Revision:** R3，2026-09-15。本文是开发计划；当前仓库只有真实 Windows OCR 执行链和 legacy PP-OCR 目录登记，没有 PaddleOCR-VL-1.6 runtime/catalog/installer。真实 pack 构建、Windows 性能与 200 页盲测均未通过。

**R3 执行依赖：** 必须同读[完整 PRD](02-upgrade-prd.md)、[跨模块合同](06-integration-contracts-plan.md) C1–C6 及 X0–X3、[新增回归和五分门](07-acceptance-and-scorecard.md)。下文只在 02/06 约束内细化 OCR 生产接线，不能覆盖二者；发现冲突时停止该项并在同一文档提交中同步修正 02/04/06/07/11/12 后再编码，不允许只做 mock 骨架。

## R3 OCR 模块冻结决策（受 02/06 约束）

1. 当前 `PP-OCR` 不是 PaddleOCR-VL-1.6，也不是可执行 OCR engine。旧目录只作为 legacy 登记迁移，状态固定为 `registered_unwired`、`Available=false`；发现 `ppocr.exe` 或 `ppocr.onnx` 也不得变成 `ready`。
2. 旧 `localEngine`/`packRoot` 只解码一个兼容发布周期。新写请求拒绝 `localEngine=ppocr` 执行偏好；后端可以保留本机目录用于迁移/诊断，但任何 Bridge response、日志和 activity DTO 都不得返回绝对路径。
3. Windows OCR 是否可用必须由 `Windows.Media.Ocr` 初始化、语言枚举和固定小图识别的真实 probe 决定，不能由 `runtime.GOOS == "windows"` 推断。
4. Settings 复用现有 `personal` category，显示名称改为“智能能力”。overview 恰好两张卡：“自动记忆”“文字识别”；点击文字识别进入唯一 OCR 详情。`routing` 只保留六类模型能力路由，不再挂载 OCR。
5. OCR 详情中的“文字识别：自动”是只读策略/运行状态，不是 checkbox、switch 或停用 OCR 的开关。首屏唯一可选能力是复杂文档增强；云端绑定、语言、pipeline、版本、回退和 legacy 登记均放入默认折叠的高级区。
6. OCR 核心 snapshot 与 provider catalog 使用独立加载状态。provider 仅在高级区展开后延迟加载；其失败不得隐藏 Windows 状态、Paddle gate 或本地识别说明。
7. 没有签名并通过 W0 的 `verifiedRuntimeProfileDigest` 时，Paddle 卡固定为 disabled，原因码 `NO_VERIFIED_RUNTIME_PROFILE`；点击不发 mutation，后端即使被旧客户端直接调用也以同码拒绝。
8. 早期 UI 探索曾出现“截图识别/选择文件”和静态结果，现行 09 Demo 与本期正式产品均已移除。本期不新增屏幕读取、文件选择或手工 `ocr.run.start` UI；生产 OCR 继续由现有上传、Office、KB、模型目录和工作区消费者触发。若以后纳入操作区，必须另立 Host 授权、scope、run 与 artifact 合同。

## Global Constraints

- 主程序不附带模型，不自动下载，用户明确安装后才启用；Windows OCR 为默认本地快速/灾备路径。
- engine IDs 固定 windows-ocr、paddleocr-vl-1.6、provider:<providerId>/<modelId>。
- 必须是 layout + VLM 完整 pipeline；只运行 VLM 不可标 full。confidence 上游没提供则 null。
- 首发只采用受管 Python stdio worker；不启动 HTTP 推理服务，不在用户机器临时 pip install，不依赖用户全局 Python/WSL/Docker。
- Job Object 限制进程/内存并回收，不是网络或文件系统沙箱。offline policy 指受信任一方代码只读本地模型和显式输入、无凭据/代理/自动下载；不得宣传 OS 级禁网。
- OCR logical migration `ocr_model_packs`（审计快照候选号 0163）必须排在 allocation 中最后一个 Memory logical migration 之后；最终文件名只取 P00 生成的 `evidence/migration-allocation.json`，并据此机械更新 store.go 的 manifest/checksum/expectedSchemaSQL/列清单及测试名。
- 旧 Result.Text/Method/Source/Pages/Coverage/Complete/Uncertain 保持；新 result 通过 RecognizeDocumentV2 提供，旧方法做显式映射。
- routing.* 保持现有 SHA revision 和 SETTINGS_VERSION_CONFLICT；新 pack aggregate 使用整数 revision + REVISION_CONFLICT。
- mutation 逐方法遵循 06-C4：`ocr.routing.set` 只有顶层 `idempotencyKey` 与 payload `scopeKind/scopeId?/policy/expectedRevision(SHA)`，scope 使用 user 禁 scopeId、project 必填 scopeId 的严格 `oneOf`，且不接受 operationId；install/uninstall 另带新 `operationId` 与 pack integer revision；cancel 只带目标 `operationId` 与其 integer revision且不新建 operation。request digest 均由服务端计算。
- 模型包物理安装为当前本地应用实例共享资源，仅本地用户设置页可改；OCR runs/results 按 subject/scope 隔离。
- ocr_paddle_pack_install/ocr_paddle_auto_route 在新 settings/gate 表持久化；默认 off。只有对应硬件实测通过才进入 auto。
- 安装 mutation 立即返回 operation receipt，下载/自测在 Engine context 后台运行。前端断开不自动取消。
- 完整流水线的“literalText”仍是识别文本，不是原件真值；所有生成 Markdown/HTML 按不可信内容渲染。
- **R3 渐进披露。** “智能能力”overview 只有自动记忆/文字识别两张卡；OCR 详情首屏只出现只读“文字识别：自动”和“PaddleOCR-VL-1.6 复杂文档增强”两个用户概念。文本层、Windows OCR、Paddle、provider 的实际选择由确定性路由、既有隐私授权和发布 gate 自动完成，不把 engine ID、阈值或 fallback 顺序变成日常配置。
- Windows probe、runtime profile、签名 manifest、许可证清单、路由解释、最近运行与故障诊断全部保留，但默认收进“高级与诊断”；安装确认仍须在执行前直接展示来自已验 manifest/preflight 的下载量、磁盘占用、许可和设备结论，不能因折叠高级信息而弱化知情同意。
- 活动列表不作为 OCR 设置页第三个常驻区块。安装、校验和长识别的进行中/失败状态投影到应用顶部统一状态入口；普通成功不弹窗打断，失败必须从该入口进入可恢复详情。
- 现有 `internal/doctext/image_ocr_windows.go` 与 `pdf_ocr_windows.go` 是真实 WinRT 执行路径，实施不得用 fake worker 替换它们；probe 与识别执行共享能力事实，但 probe 有独立15秒 deadline和5分钟缓存。
- legacy PP-OCR 登记与新 Paddle pack 使用不同类型、表、状态和 UI 文案。禁止把 `packRoot`、文件 marker 或 `LUNITIDE_PPOCR_ROOT` 映射为 `paddleocr-vl-1.6` 的安装、健康或 gate 证据。
- 新主体默认策略逐字为 `{"mode":"auto","complexDocumentEngine":"paddleocr-vl-1.6","fallbackOrder":["windows-ocr"],"sendToCloud":"never"}`；存量 `preferProvider=true` 仅在完整 provider/model 绑定存在时迁为 `provider_first/configured_only`，否则回该默认并记录 `LEGACY_PROVIDER_BINDING_REQUIRED`。启用“自动识别”本身绝不扩大云端授权。

## 命令、生成、提交约定

所有 PowerShell 原生命令逐条执行，并立即检查 $LASTEXITCODE。多条测试用分号拼接却只检查最后一条不合格。新方法要同步 envelope enum、generate-bridge.mjs enabled assertion、client mutation sets、Engine registry/scope guard：

~~~powershell
npm --prefix web run generate:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge generation failed" }
npm --prefix web run verify:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge verification failed" }
~~~

不手改 internal/bridge/schema_generated.go、internal/contract/schema_generated_test.go、web/src/generated/bridge.ts。所有任务只 stage 列出的本任务文件和生成物，不 stage 用户其他修改。

## 文件所有权

| 任务 | 文件 | 职责 |
|---|---|---|
| 0 | workers/paddleocr-vl-1.6/paddleocr_worker.py、protocol.schema.json、requirements.lock、pipeline.yaml；scripts/build-paddleocr-vl-pack.ps1、test-paddleocr-vl-pack.ps1 | 真实运行时、完整模型配置、可复现离线包 |
| 1 | migrations/0163_ocr_model_packs.sql（仅审计候选名，实际名取 allocation）；internal/storage/sqlite/store.go、upgrade_schema.go、ocr_model_packs.go、ocr_model_packs_test.go；internal/ocrapp/scope.go、scope_test.go | repository、scope基础类型、settings/gates、legacy登记、安装状态/操作、受信 NOTICE 索引、scoped runs/artifacts |
| 2 | internal/ocrapp/catalog.go、catalog_test.go、installer.go、installer_test.go；internal/bootstrap/ocr_paddle_composition.go、ocr_paddle_composition_test.go | manifest/签名/download/后台状态机；仅 P09 通过后装配 catalog/runner/launcher |
| 3 | internal/ocrapp/worker.go、worker_test.go、result.go；internal/doctext/ocr_probe_windows.go、ocr_probe_other.go、ocr_probe_windows_test.go、ocr_probe_integration_windows_test.go、testdata/ocr_probe.png；internal/app/capability_ready.go、capability_ready_test.go | stdio、资源、结果协议、真实 Windows probe/readiness |
| 4 | internal/ocrapp/request.go、request_test.go、route.go、route_test.go、recognize.go、recognize_test.go、health.go、health_test.go、pack.go、pack_test.go、ocrapp_test.go；internal/app/ocr_wire.go、ocr_wire_test.go、office_source_text.go、office_source_text_test.go、model_catalog.go、model_delivery_test.go、kb_search_handlers.go、kb_search_handlers_test.go、chat.go、chat_run_stream.go、robot_closed_loop_test.go；internal/toolruntime/office_studio.go、workspace_read.go、workspace_documents_test.go；internal/bootstrap/wire.go、ocr_composition_test.go | scoped request快照、legacy兼容、逐页路由、所有生产消费者 |
| 5 | internal/app/ocr_pack_handlers.go、ocr_pack_handlers_test.go、s1_bridge_handlers.go、s1_bridge_handlers_test.go、handlers_registry.go、org_lifecycle.go；api/bridge/v1/ocr.routing.get.schema.json、ocr.routing.set.schema.json、ocr.pack.get.schema.json、ocr.pack.install.schema.json、ocr.pack.cancel.schema.json、ocr.pack.uninstall.schema.json、ocr.pack.notice.list.schema.json、ocr.pack.notice.read.schema.json、ocr.run.list.schema.json、ocr.run.get.schema.json、ocr.artifact.read.schema.json；web/src/bridge/client.ts、web/src/generated/bridge.ts、internal/bridge/schema_generated.go、internal/contract/schema_generated_test.go | 公开合同 |
| 6 | 修改 web/src/settings/settingsNav.ts、settingsNav.test.ts、SettingsPage.tsx、SettingsPage.test.tsx、SmartCapabilitiesPanel.tsx、SmartCapabilitiesPanel.test.tsx、web/src/m8/PersonalIntelligencePage.tsx、PersonalIntelligencePage.test.tsx、web/src/app/navStore.ts、web/src/App.tsx、App.test.tsx、web/src/styles.css、managementLayout.css；创建 web/src/settings/OCRSettingsPanel.tsx、OCRSettingsPanel.test.tsx、OCRModelPackCard.tsx、OCRModelPackCard.test.tsx、OCRAdvancedPanel.tsx、OCRRecentRuns.tsx、OCRRecentRuns.test.tsx、ocrActivityAdapter.ts、ocrActivityAdapter.test.ts；迁移后删除 OCRRouting.tsx/OCRRouting.test.tsx | P06 已创建的两卡入口上融合 OCR、独立加载、纯黑响应式；只产出 OCR activity adapter，不拥有共享顶部活动组件 |
| 7 | internal/ocrapp/artifact_store.go、artifact_store_test.go；testdata/ocr-eval/manifest.json；scripts/ocr-eval.ps1；docs/design/jiyishengji/runbooks/paddleocr-vl-runbook.md | scoped 结果存储、评测、恢复 |

## Task 0：验证 runtime profile；验证前保持 Paddle 安装关闭（OCR-002/004/005/012）

- [ ] **1. 先实现 fail-closed 报告。** `scripts/test-paddleocr-vl-pack.ps1 -Scenario CleanOffline` 的输入是构建产物路径，并检查 manifest、runtime、worker、layout/VLM、NOTICE/SBOM。当前仓库没有该路径、runtime 或完整 pipeline，命令必须输出 `INCOMPLETE` 并非零退出；不得用 fake pipeline 生成 digest 或 PASS。Task 1 写入的初始 gate 必须是 `install_enabled=0, auto_route_enabled=0, verified_runtime_profile_digest=NULL, disabled_reason=NO_VERIFIED_RUNTIME_PROFILE`。
- [ ] **2. 构建源仅在官方组合可验证时加入。** 固定官方 PaddleOCR/PaddleX/PaddlePaddle 的已验证版本及 Windows x64 CPU wheel；构建时从官方发布元数据解析精确版本/URL/hash并写 `requirements.lock`，禁止 latest、浮动范围和无 hash dependency。CPython 使用可再分发受管 runtime，构建 CI 安装 wheel 到 pack runtime；不得假定 Paddle 可安装，也不得假定普通 embedded Python 无配置即可加载 Paddle DLL。若找不到可再分发且许可兼容的完整组合，记录 `INCOMPLETE`，保持 gate 关闭，Windows/UI 诚实基线仍可交付。
- [ ] **3. 配置。** 只在锁定 wheel 的 API 已由官方签名/示例验证后，从官方 PaddleOCR-VL-1.6 配置导出完整 pipeline YAML；模型路径全部改为 pack 内相对路径并禁用自动下载。不得臆造 `layout.Analyze`、VLM binding 或以单 VLM 冒充 full pipeline。
- [ ] **4. Python worker。** 入口只接受 `--config` 和 Engine 私有任务参数；配置文件属于签名 pack，不允许 renderer 传任意 YAML。固定本地 pipeline 初始化后从 stdin 接收 JSONL，调用锁定版本的官方 pipeline，逐页映射 block/文字/结构/警告，stdout 只发协议，日志去受控 stderr。首次自测必须真实执行 layout 和 VLM 两 stage。

~~~python
# protocol.schema.json 对应的协议头；不是模型识别的替代实现
PROTOCOL_VERSION = 1
PIPELINE_KIND = "paddleocr_vl_full"
ALLOWED_INPUT_TYPES = {"page_image"}
~~~

- [ ] **5. 真实验证。** `scripts/build-paddleocr-vl-pack.ps1 -OutputRoot ./artifacts/paddleocr-vl-1.6` 只构建唯一待测完整包；随后在干净 Windows x64 CPU、无系统 Python、无模型缓存、阻断网络的 CI/VM 中运行以下命令。报告记录 runtime/profile JSON、两个 stage/model identity、输出字段、冷启动、峰值 RSS、DLL 依赖和进程退出；路径不存在、跳过 stage 或网络命中均失败。

~~~powershell
./scripts/build-paddleocr-vl-pack.ps1 -OutputRoot ./artifacts/paddleocr-vl-1.6
if ($LASTEXITCODE -ne 0) { throw "Paddle pack build failed or is unsupported" }
./scripts/test-paddleocr-vl-pack.ps1 -PackPath ./artifacts/paddleocr-vl-1.6/pack -Scenario CleanOffline
if ($LASTEXITCODE -ne 0) { throw "Paddle clean-offline verification failed" }
~~~

- [ ] **6. 发布条件。** W0 通过后生成 immutable pack、逐文件 SHA-256、压缩/展开 bytes、SBOM/NOTICE，并以规范化 profile JSON 的 SHA-256 作为 `verifiedRuntimeProfileDigest`。catalog entry 的 digest 必须逐字等于本地 gate 中的 digest 才能 `enabled=true`。签名由发布流程完成，私钥不进 repo。未产生 digest 时 UI/handler 均返回 `NO_VERIFIED_RUNTIME_PROFILE`，下载、目录创建、operation insert 和后台 job 数全部为 0。
- [ ] **7. 提交。** 仅 worker 源、lock/config、脚本与脱敏报告；commit `build(ocr): validate the Windows offline Paddle pipeline pack`，不提交模型权重/临时 venv。

## Task 1：SQLite repository、设置及恢复真相（OCR-003/007）

- [ ] **1. 红测。** 在 `internal/storage/sqlite/ocr_model_packs_test.go` 精确新增与物理编号无关的 `TestOCRModelPacksMigration`、`TestOCRModelPacksMigrationIsIdempotent`、`TestOCRModelPacksMigrationChecksumMismatch`、`TestOCRPackRejectsSecondActiveMutation`、`TestOCRNoticeRetentionOffline`、`TestOCRRunReadRequiresOwnerScope`、`TestOCRLegacyPPOCRMigrationRegisteredUnwired` 和 `TestOCRGateDefaultsNoVerifiedRuntimeProfile`。NOTICE 测试必须覆盖签名校验前不可见、校验后 list/read、安装/卸载/catalog 换版后仍保留、断网零请求、未知 digest 拒绝和 NOTICE blob 篡改 fail-closed。前三个 migration 测试从 allocation 读取实际前置/目标编号，测试先证明新表/方法缺失，再实施；不得把候选 0163 写进函数名，也不得用只查表名的 smoke test 代替约束断言。
- [ ] **2. 迁移顺序。** 本任务只创建 logical migration `ocr_model_packs`；审计快照候选文件名是 `migrations/0163_ocr_model_packs.sql`，实际文件名、upgrade 分支和测试名必须从 P00 的 allocation artifact 机械生成/替换，并严格接在 allocation 中最后一个 Memory logical migration 之后。同步更新 `internal/storage/sqlite/store.go` 的 migration manifest、checksum、`expectedSchemaSQL` 和列清单及 `internal/storage/sqlite/upgrade_schema.go`；禁止硬编码未冻结候选号或运行时动态补列。
- [ ] **3. 表。** `ocr_model_packs` logical migration 一次创建以下实体；ID/enum/JSON/length/bytes 约束在 SQL 和 domain 双重执行。

| 表 | 真相/关键约束 |
|---|---|
| ocr_pack_state | pack_id PK；not_installed 也有行；current/previous version、active op、integer revision |
| ocr_pack_versions | (pack_id,version) PK；immutable manifest_digest/install_root_ref；verified/quarantined；与 legacy registration 的 root_ref 分列，禁止混用 |
| ocr_pack_operations | subject、operation/key/request digest、accepted manifest、phase/progress/cancel/result/revision |
| ocr_settings | `(owner_subject_id,scope_kind,scope_id)` PK；policy JSON、SHA policy_revision、integer revision、legacy_imported_at |
| ocr_pack_gates | `pack_id` PK；`install_enabled`/`auto_route_enabled` 默认 0；`verified_runtime_profile_digest` nullable；`disabled_reason` 默认 `NO_VERIFIED_RUNTIME_PROFILE`；integer revision/updated_at |
| ocr_legacy_registrations | `registration_id` PK、owner_subject_id/scope_kind/scope_id、engine_id=`ppocr`、root_ref（Engine 私有）、marker_detected、state=`registered_unwired`、available=0、imported_at/revision；`UNIQUE(owner_subject_id,scope_kind,scope_id,engine_id)`，CHECK 禁止 ready/available |
| ocr_document_runs | request_id、owner/scope、document_digest、policy/gate/mode/provider pair/pack manifest/runtime profile/pipeline/allow_remote 的 immutable snapshot、request_snapshot_digest、state/pages/actual engine evidence；`UNIQUE(run_id,request_snapshot_digest)`，credential ref 禁止落库 |
| ocr_page_results | (run_id,page) PK；request_snapshot_digest 外键、typed actual engine/version/pack/pipeline/source digest、complete/uncertain、layout/ref/warnings |
| ocr_version_leases / ocr_artifact_leases | 按06-C4增加版本运行和artifact读取租约；operation表增加lease_owner/until/heartbeat/attempt/fence |
| ocr_artifacts | artifact_id、owner/scope/run、CAS ref/hash/size/MIME、expiry |

活动 operation partial unique index 排除 `succeeded/failed/cancelled`。旧 `ocr-routing.json` 只在可确定 owner/scope 时导入一次；`preferProvider=true` 映射 `provider_first/configured_only`，`false` 映射 `auto/never`。旧 `localEngine=ppocr` 或 `packRoot` 只生成 `ocr_legacy_registrations` 行，不生成 pack version/gate，不把 Paddle 设为 ready；路由执行值规整为 `windows-ocr` 并记录稳定 warning `LEGACY_PPOCR_UNWIRED`。旧文件保留备份但 SQLite 成为新真相。`root_ref` 只供 Engine 私有诊断，Bridge/log/activity 永不序列化绝对路径。

- [ ] **4. repository API。** 先在 `internal/ocrapp/scope.go` 定义 `OCRScope{OwnerSubjectID,ScopeKind,ScopeID}` 与下列 store interfaces；SQLite 实现放在 `ocr_model_packs.go`，避免 storage package 反向依赖 service。

~~~go
type ScopedRoutingStore interface {
    Get(context.Context, OCRScope) (Routing, error)
    CompareAndSet(context.Context, OCRScope, string, Routing) (Routing, error)
}
type LegacyRegistrationStore interface {
    ImportPPOCR(context.Context, OCRScope, string) (LegacyRegistration, error) // string 仅为 Engine 私有 rootRef
    GetPPOCR(context.Context, OCRScope) (LegacyRegistration, error)
}
type PackGateStore interface {
    GetGate(context.Context, string) (PackGate, error)
    CompareAndSetGate(context.Context, string, int64, PackGate) (PackGate, error)
}
// 新 pack/run repository 方法均接 context；run/artifact 读取必须同时带 identity 和 OCRScope。
// LegacyRegistration 的公开 DTO 只含 registrationId/state/available/markerDetected，绝无 root_ref。
~~~

安装 mutation 在同事务检查 gate、key/digest、CAS、活动操作唯一性，再 insert operation；gate 无 digest 时在事务开始即返回 `NO_VERIFIED_RUNTIME_PROFILE`，不得 insert operation。重放返回原 operation。本任务只产出 repository/interfaces，不提前在 bootstrap 引用 Task2/3 尚未交付的 catalog、runner 或 launcher；统一装配在 Task4 完成消费者接线时进行。
- [ ] **5. 绿测/提交。** 逐条运行：

~~~powershell
go test -count=1 ./internal/storage/sqlite -run 'TestOCRModelPacksMigration|TestOCRPackRejectsSecondActiveMutation|TestOCRRunReadRequiresOwnerScope|TestOCRLegacyPPOCRMigrationRegisteredUnwired|TestOCRGateDefaultsNoVerifiedRuntimeProfile'
if ($LASTEXITCODE -ne 0) { throw "OCR storage tests failed" }
go test -count=1 ./internal/storage/sqlite
if ($LASTEXITCODE -ne 0) { throw "SQLite suite failed" }
~~~

提交 `feat(ocr): persist pack operations scoped runs and routing settings`。

## Task 2：签名 catalog 与异步安装（仅 P09/W0 通过后；OCR-002/003/007/011/012）

进入条件是 Task0 已生成并验证非空 `verifiedRuntimeProfileDigest`；未满足时本节所有实现步骤均保持 `BLOCKED/INCOMPLETE`，不得提前合入 downloader、可安装 catalog entry 或 runner 启动路径。

- [ ] **1. 红测。** 在 `internal/ocrapp/catalog_test.go` 精确新增一个 table-driven `TestOCRCatalog`，子测固定为 `tampered_manifest`、`unknown_expired_or_revoked_key`、`matching_verified_runtime_profile`；在 `installer_test.go` 新增 `TestOCRInstallRejectsNoVerifiedRuntimeProfileWithoutMutation`、`TestOCRInstallResumesHalfDownload`、`TestOCRInstallRecoversBeforeAndAfterActivation` 和 `TestOCRInstallReturnsExistingActiveOperation`。profile 子测同时覆盖 mismatch 拒绝与 exact match 接受；`TestOCRInstallRejectsNoVerifiedRuntimeProfileWithoutMutation` 断言 operation inserts、download calls、directory creates、runner starts 全为 0。禁止再建立三个同义 `TestOCRCatalogRejects*` 顶层函数。
- [ ] **2. 执行红测。**

~~~powershell
go test -count=1 ./internal/ocrapp -run 'TestOCRCatalog|TestOCRInstall'
if ($LASTEXITCODE -eq 0) { throw "Expected new OCR catalog/installer tests to fail before implementation" }
~~~

- [ ] **3. 签名与启用条件。** Ed25519 直接签 `manifest.json` 原始 UTF-8 bytes，签名在 `manifest.sig`；不重排 JSON。signature 使用 base64url 无 padding，`keyId` 查内置 active/retired/revoked 表；过期/撤销/未知 key 拒绝。publisher/client 共享固定测试向量。`catalogRevision=SHA256(catalog bytes)`，`acceptedManifestDigest=SHA256(manifest bytes)`。entry 只有在 `enabled=true`、manifest 中 `runtimeProfileDigest` 非空且与本机 gate 的 `verifiedRuntimeProfileDigest` 完全一致时可安装；不能用目录、marker、环境变量或 legacy PP-OCR 登记满足此条件。
- [ ] **4. 安装请求。** `packId/catalogRevision/acceptedManifestDigest/expectedRevision/operationId`；用户在 manifest 大小、许可、设备预检后点击安装即完成 consent。`Installer.Start` 的第一条分支读取 gate；digest 为空、entry disabled 或 digest 不匹配均返回 `NO_VERIFIED_RUNTIME_PROFILE`，不创建 receipt/operation/staging/job。通过 gate 后 Engine 写 requested receipt 并立即返回，runner 绑定 Engine lifecycle context；没有第二个悬空 `awaiting_consent` 状态。

~~~text
requested → preflighting → downloading → verifying → installing → self_testing → succeeded
任一步失败 → failed（旧 current 仍可用）
downloading + cancel → cancelled
verified version + self-test success → SQLite CAS current/previous → ready
pack availability独立：ready；卸载operation运行成功后pack为not_installed
~~~

- [ ] **5. 文件与崩溃顺序。** 专用 downloader 复用网络 URL/重定向 policy；仅 manifest allowlist；staging 路径从内部 ID 生成。拒绝 zip-slip、重复规范化名、NTFS ADS、symlink/reparse、解压炸弹，磁盘预检=下载剩余+展开+旧版保留+10%裕量。校验/展开/self-test 完成后原子 rename 到 immutable version，最后 SQLite CAS 切 current；current.json 只是可重建缓存。崩溃后 DB 未指向的新版本是 orphan，可验证后重试或清理；DB 指向损坏版本则隔离并事务退 previous。
- [ ] **6. 取消/重试。** 持久 cancel_requested，下载每块检查；原子切换不可取消返回 OCR_OPERATION_NOT_CANCELLABLE。启动 recovery 将 lease 过期 job 重新 claim，重放不重复切换。运行中版本持有 lease，卸载先停止新作业、取消/等待现有 lease 释放，再清受管 pack；保留 scoped OCR 结果以及 `ocr_pack_notices`/NOTICE CAS blob，只更新 `uninstalled_at`；卸载等待30s仍有lease则OCR_PACK_BUSY并保留current。旧fence runner禁止提交。
- [ ] **7. 装配、绿测、审计/提交。** 只有 P09 已通过时，`internal/bootstrap/ocr_paddle_composition.go` 才在唯一 composition root 新增 verified catalog、installer runner 与受管 worker launcher；P07 不预造 disabled/fake pack service。composition test 断言 runtime profile digest 精确匹配才构造这些依赖，失败仍是零 downloader/runner mutation；Task5 gate-first handler 的拒绝语义继续保留。审计使用 `m7_audit_events` 的开放 action 合同，写 operation/stage/digest/error，不改旧 `audit_events` 闭集；路径不得进审计。逐条运行：

~~~powershell
go test -count=1 ./internal/ocrapp -run 'TestOCRCatalog|TestOCRInstall'
if ($LASTEXITCODE -ne 0) { throw "OCR catalog/installer tests failed" }
go test -count=1 ./internal/ocrapp
if ($LASTEXITCODE -ne 0) { throw "OCR app suite failed" }
~~~

提交 `feat(ocr): install signed packs with resumable operations`。

## Task 3：真实 worker、资源和 Windows OCR probe（OCR-004/005/007/008）

- [ ] **1. 红测。** `internal/ocrapp/worker_test.go` 新增入口被改、URL/越权 input、坏 JSON/乱序/输出超限/挂死、缺 layout 等用例；`internal/doctext/ocr_probe_windows_test.go` 精确新增 canonical table-driven `TestWindowsOCRProbeStates`，子测固定为 `ready`、`unsupported_os`、`initialization_failed`、`language_unavailable`、`sample_failed`、`timed_out`；`ocr_probe_other_test.go` 另验证 other stub 始终返回 `unsupported_os`。子测名不是第二套顶层 wrapper，禁止再建立六个 `TestProbeWindowsOCR*` 同义函数。fake 只注入 WinRT launcher，不能把 `runtime.GOOS` 当 ready。
- [ ] **2. 执行红测。**

~~~powershell
go test -count=1 ./internal/ocrapp ./internal/doctext -run 'TestOCRWorker|TestWindowsOCRProbeStates'
if ($LASTEXITCODE -eq 0) { throw "Expected new worker/probe tests to fail before implementation" }
~~~
- [ ] **3. 启动。** 从已验 manifest 解出 runtime/python.exe、固定 -I worker/paddleocr_worker.py 和 pipeline config；再次校验 worker/runtime/DLL/config hashes。通过 SpawnIsolatedWithStderr 原语启动，OCR 自己的 gate 在调用前检查；不打开无关 MCP stdio gate。env allowlist 仅 SystemRoot、受管 TEMP/TMP、必要 DLL/runtime paths、本地模型配置和线程数，API keys/proxy/PYTHONPATH 不继承。
- [ ] **4. 协议。** 每帧 v/jobId/seq/type；input 只含 source digest、task-local page reference、page number；输出 header/block/page_done/failed。单帧≤1MiB，每页≤8MiB，任务≤64MiB，input≤100页/每页20MP；超过阈值明确 warning/error。stdout/stderr 分离，stderr 限 1MiB 环形脱敏缓冲。序号错/EOF 半 JSON/未知 type fail closed。
- [ ] **5. 资源。** 一次最多 1 worker/1推理任务；Job Object MaxProcs、MemoryCapBytes 来自已验证 runtime profile；wall watchdog/输出字节在 Engine 执行。CPU thread cap 通过已锁 Paddle runtime 设置并记录，不能把它当硬 CPU quota。临时盘定期检查阈值并取消；不是内核文件配额。profile 不提供合法非零 limits 则禁止安装。
- [ ] **6. Probe 接口与状态。** `internal/doctext/ocr_probe_windows.go` 复用现有 WinRT PowerShell 调用原语，实际初始化 `Windows.Media.Ocr`、枚举可创建 OCR engine 的语言，并识别 `//go:embed testdata/ocr_probe.png` 的固定文本；不访问网络/用户文件。15 秒 deadline 到期必须终止子进程。公开接口固定为：

~~~go
type WindowsOCRProbeState string
const (
    WindowsOCRReady             WindowsOCRProbeState = "ready"
    WindowsOCRUnsupportedOS     WindowsOCRProbeState = "unsupported_os"
    WindowsOCRInitializationErr WindowsOCRProbeState = "initialization_failed"
    WindowsOCRLanguageMissing   WindowsOCRProbeState = "language_unavailable"
    WindowsOCRSampleFailed      WindowsOCRProbeState = "sample_failed"
    WindowsOCRTimedOut          WindowsOCRProbeState = "timed_out"
)
type WindowsOCRProbe struct {
    State WindowsOCRProbeState `json:"state"`
    Available bool `json:"available"` // 仅 State==ready
    Languages []string `json:"languages"`
    ErrorCode string `json:"errorCode,omitempty"`
    CheckedAt time.Time `json:"checkedAt"`
}
func ProbeWindowsOCR(context.Context) WindowsOCRProbe
~~~

`internal/ocrapp/health.go` 以 5 分钟 TTL 缓存该值；`refreshProbe=true` 绕过缓存。删除/改写当前仅检查 `runtime.GOOS` 的 `LocalOCRReady` 语义，`internal/app/capability_ready.go`、所有路由和 UI 只认 probe 的 `Available`。非 Windows `ocr_probe_other.go` 返回 `unsupported_os/false`；`capability_ready_test.go` 增加 `TestOCRReadinessRequiresSuccessfulWindowsProbe`，证明 Windows OS + probe failure 仍非 ready。
- [ ] **7. 真实 Windows integration。** 在 `internal/doctext/ocr_probe_integration_windows_test.go` 增加 `//go:build windows && ocrintegration` 的 `TestProbeWindowsOCRIntegration`：必须走真实 WinRT，断言 `state=ready`、至少一门 language，且固定小图的规范化结果匹配 fixture。无语言包或初始化失败应让发布机测试失败并输出稳定状态，不得 `Skip` 或伪造 ready。逐条运行：

~~~powershell
go test -count=1 ./internal/ocrapp ./internal/doctext -run 'TestOCRWorker|TestWindowsOCRProbeStates'
if ($LASTEXITCODE -ne 0) { throw "OCR worker/probe unit tests failed" }
go test -count=1 ./internal/app -run '^TestOCRReadinessRequiresSuccessfulWindowsProbe$'
if ($LASTEXITCODE -ne 0) { throw "OCR readiness probe test failed" }
go test -count=1 -tags ocrintegration ./internal/doctext -run '^TestProbeWindowsOCRIntegration$'
if ($LASTEXITCODE -ne 0) { throw "Real Windows OCR probe failed" }
~~~

Task0 通过时再以真实 pack 跑 CleanOffline 确认两个 stage 与资源；否则只提交 Windows probe/worker 协议，保持 Paddle gate disabled。提交 `feat(ocr): run the verified local pipeline and probe Windows OCR`。

## Task 4：兼容结果、逐页路由与生产接线（OCR-001/006/007/008）

- [ ] **1. 先固定 scoped request 接口。** 在 `internal/ocrapp/request.go` 复用 Task1 `scope.go` 的 `OCRScope`，并定义下列 request 类型。`OwnerSubjectID` 必须由 Engine 的可信 identity/context 注入，`ScopeKind/ScopeID` 由调用点注入；Bridge payload 不得自报 owner。`ResolveRequest` 在一次 repository snapshot/事务内读取 policy、gate、probe、pack/version lease 和精确 provider binding，生成后不可变；`RecognizeDocumentV2` 校验输入 SHA-256 等于 `SourceDigest`，执行期间不得再读 routing/gate/catalog。

~~~go
type ProviderBinding struct { ProviderID, ModelID, CredentialRef string }
type ResolvedOCRRequest struct {
    RequestID string
    Scope OCRScope
    SourceDigest string
    PolicyRevision string
    GateRevision int64
    Mode string
    AllowRemote bool
    Provider *ProviderBinding
    PackManifestDigest string
    PackRuntimeProfileDigest string
    PipelineKind string
}
type ProviderFunc func(context.Context, ResolvedOCRRequest, []byte, string) (string, error)
func (s *Service) ResolveRequest(context.Context, OCRScope, string) (ResolvedOCRRequest, error)
func (s *Service) RecognizeDocumentV2(context.Context, ResolvedOCRRequest, string, []byte, string) (DocumentResult, error)
~~~

`CredentialRef` 是不含 secret 的内部 snapshot 字段且不进入 Bridge/result；provider lease 在调用时由该 ref 获取。`internal/app/ocr_wire.go::ocrProviderCall` 只使用传入 request 的 Provider，不再调用 `e.ocr.Routing()` 或从目录猜 model；`internal/ocrapp/health.go::tryProvider` 同样只消费该 snapshot。
- [ ] **2. 表驱动红测。** 在 `internal/ocrapp/recognize_test.go`/`route_test.go` 精确新增 `TestResolveOCRRequestIsScoped`、`TestResolvedOCRRequestDoesNotReloadRouting`、`TestResolvedOCRRequestDoesNotCrossSubjects`、`TestOCRDefaultNeverCallsCloud`、`TestLegacyPPOCRDecodesButNeverExecutes`、`TestSetRoutingRejectsLegacyPPOCRPreference` 及 `TestOCRRouteMatrix`。每个矩阵 case 断言 engine identity、fallback chain、cloud calls、complete/warnings；用会在第一次读取后报错的 store 证明整个 run 不二读。
- [ ] **3. 路由。** 文本层覆盖的页首先直用，不重复 OCR；其余按矩阵从 request snapshot 生成有序健康引擎列表，再去重尝试。

| mode / 条件 | 首选 | 后续 |
|---|---|---|
| local_fast | Windows | 不云端；Windows 失败返回 partial |
| local_document、pack ready | Paddle | Windows；仅 policy 明确允许时 provider |
| local_document、pack 缺失 | 不保存不可用设置；一次任务可 Windows | 提示安装，不自动下载 |
| auto、简单/auto gate off | Windows | policy 中允许且健康的 fallback |
| auto、复杂+ready+profile gate pass | Paddle | Windows，再按 policy 允许的 provider |
| provider_first、configured_only+健康绑定 | 精确 provider ID/model | policy 中本地 fallback |
| provider_first、sendToCloud=never | Windows 或经 gate 的本地 Paddle | cloud calls=0 |

`fallbackOrder` 仅能在许可/安装/健康过滤后的集合排序，不能绕过 `never`。旧 `preferProvider=true` 迁移 `provider_first/configured_only` 并保持原已有明确 provider 配置；`false` 迁移 `auto/never`。无已配置 provider 时不得由模型目录猜一个上传。

- [ ] **4. legacy 解码一个周期。** `internal/ocrapp/route.go` 的旧 JSON decoder 在 R3 只读 `preferProvider/localEngine/packRoot`；新 `SetRouting` 输入 `localEngine=ppocr` 返回 `OCR_LEGACY_ENGINE_UNWIRED`，新输出不含 `packRoot`。`internal/ocrapp/pack.go::DetectPPOcrPack` 即使发现 marker 也返回 `State=registered_unwired, Available=false`，且不得产生 `paddleocr-vl-1.6` identity/ready。R3 是唯一兼容发布；R4 删除旧 schema input properties/decoder。删除前以不含路径的 telemetry counter 证明旧写入已归零，并在 release notes 明示迁移终止。
- [ ] **5. 兼容与所有生产消费者。** 旧 `RecognizeDocument(ctx,name,raw,media)`、`RecognizePDF(ctx,raw)`、`RecognizeImage(ctx,raw)` 保留一个周期，但只能由 Engine 内部兼容 adapter 在可信默认 scope 下调用，并由 V2 映射原 `Result.Text/Method/Source/Pages/Coverage/Complete/Uncertain`；无可信 scope 返回 `OCR_SCOPE_REQUIRED`。以下调用点必须显式 resolve 一次并传同一 snapshot，不能各自回退到旧 global route：

  - `internal/app/kb_search_handlers.go::projectKBDocument`：`project/<CollectionID>`，并回归完整 coverage。
  - `internal/app/model_catalog.go::maybeDescribeImages`：`conversation/<sessionID>`；相应地把 sessionID 从 `chat.go` 和 `chat_run_stream.go` 传入。
  - `internal/app/office_source_text.go`：`office_task/<taskID>`；cache key 使用 request 的 `PolicyRevision+GateRevision+PackManifestDigest`。
  - `internal/toolruntime/office_studio.go::SetDocumentText` 和 `workspace_read.go::readWorkspace`：callback 增加可信 `session` 参数，映射 `workspace/<session>`；`internal/app/ocr_wire.go::workspaceDocumentText` 接收该值。
  - `internal/app/ocr_wire.go` 与 `internal/bootstrap/wire.go`：P07 只注入 SQLite repositories、独立 OCR CAS、scope resolver和真实 Windows probe/engine；删除运行中的 global FileStore 二读。Task5 的无 profile handler 先查持久 gate 并直接拒绝，不需要 pack service 空实现。`ocr_composition_test.go` 断言无 verified profile 时 catalog/downloader/runner/worker launcher 均未构造且不创建 `*FileStore`。P10 再由 Task2 的 `ocr_paddle_composition.go` 在同一 composition root 接入已验证 catalog/runner/launcher，不新建第二套 service。

- [ ] **6. 逐页持久化。** 输出按 `run+page` 幂等，mixed/partial 明确；成功页不因失败页重做。无 confidence 写 null。模型输出和 provider 错误正文不进 UI 错误。run 必须写 06-C3 冻结的 `request_id/owner/scope/document_digest/policy_revision/gate_revision/mode/provider pair/pack manifest/runtime profile/pipeline/allow_remote/request_snapshot_digest`，不持久化 CredentialRef；page 必须以 `(run_id,request_snapshot_digest)` 外键绑定同一不可变请求，并另记实际 engine/version/pack/pipeline/source digest/warnings。中途 fallback 只改实际证据，禁止改请求快照。
- [ ] **7. 绿测/提交。** 逐条运行：

~~~powershell
go test -count=1 ./internal/ocrapp -run 'TestResolveOCRRequest|TestResolvedOCRRequest|TestOCRDefaultNeverCallsCloud|TestLegacyPPOCR|TestSetRoutingRejectsLegacyPPOCRPreference|TestOCRRouteMatrix'
if ($LASTEXITCODE -ne 0) { throw "OCR scoped routing tests failed" }
go test -count=1 ./internal/app -run 'TestOCRProviderUsesResolvedRequest|TestProjectKBOCRScope|TestCatalogImageOCRScope|TestOfficeOCRScope'
if ($LASTEXITCODE -ne 0) { throw "OCR consumer wiring tests failed" }
go test -count=1 ./internal/toolruntime -run '^TestWorkspaceReadOCRScope$'
if ($LASTEXITCODE -ne 0) { throw "Workspace OCR scope test failed" }
go test -count=1 ./internal/bootstrap -run '^TestOCRCompositionUsesScopedSQLiteStore$'
if ($LASTEXITCODE -ne 0) { throw "OCR composition test failed" }
go test -count=1 ./internal/ocrapp ./internal/doctext ./internal/app ./internal/toolruntime ./internal/bootstrap
if ($LASTEXITCODE -ne 0) { throw "OCR routing/wiring suites failed" }
~~~

提交 `feat(ocr): route pages with scoped immutable requests`。

## Task 5：Bridge snapshot 与查询权限（OCR-003/009）

- [ ] **1. 精确 schema 文件。** 修改 `api/bridge/v1/ocr.routing.get.schema.json`、`ocr.routing.set.schema.json`；新增 `ocr.pack.get.schema.json`、`ocr.pack.install.schema.json`、`ocr.pack.cancel.schema.json`、`ocr.pack.uninstall.schema.json`、`ocr.pack.notice.list.schema.json`、`ocr.pack.notice.read.schema.json`、`ocr.run.list.schema.json`、`ocr.run.get.schema.json`、`ocr.artifact.read.schema.json`。随后只通过 generator 更新 `internal/bridge/schema_generated.go`、`internal/contract/schema_generated_test.go`、`web/src/generated/bridge.ts`。
- [ ] **2. 红测。** 在 `internal/app/ocr_pack_handlers_test.go` 精确新增 canonical table-driven `TestOCRPackBridge`，子测至少为 `get_exact_snapshot`、`install_receipt`、`cancel_target_operation`、`uninstall_receipt`、`stale_sha_revision`、`notice_list_stable_cursor`、`notice_bounded`、`notice_retained_after_uninstall_offline`、`notice_unavailable_zero_mutation`、`no_verified_profile_zero_mutation`、`no_absolute_path`；NOTICE unavailable 子测覆盖本机无受信记录、NOTICE blob 缺失/损坏及 digest 不匹配，均断言 `OCR_PACK_NOTICE_UNAVAILABLE` 且数据库/文件/网络 mutation=0；`release=null` 但存在 retained record 必须仍能读。这些只是 subtest title，不再建立 `TestOCRPackBridgeUsesValidStaleRevision`、`TestOCRPackInstallNoVerifiedProfileHasZeroMutation` 或 `TestOCRBridgeNeverReturnsAbsolutePackPath` 顶层 wrapper。stale 子测使用格式合法但过期的 64 位 SHA，不用非法字符串假测 CAS。在现有 `s1_bridge_handlers_test.go` 新增 canonical table-driven `TestOCRRoutingScopeContract`（user 正例/project 正例/user 带 scopeId/project 缺 scopeId/缺 scopeKind/越权/继承 revision CAS）、`TestOCRRoutingLegacyFieldsDecodeForOneRelease`、`TestOCRRoutingSetRejectsPPOCRExecutionPreference`、`TestOCRRunBridgeRejectsCrossScope` 和 `TestOCRArtifactReadIsBounded`；在 `internal/contract/schema_generated_test.go` 断言所有新 method enabled。
- [ ] **3. 核心 snapshot 与兼容边界。** `ocr.routing.get` 是 OCR 详情的轻量核心请求，只读 scoped policy、真实 probe 和请求 scope 的 legacy registration，不调用 pack catalog、`providerApi.list` 或 provider registry；payload 必须按 06-C3 显式给 user/project scope，可选 `refreshProbe=true` 绕过 probe 缓存。项目无 override 时只读继承 user policy；R3 result 固定包含 `requestedScope/policySource/policy/revision/windowsProbe/legacy`；Paddle 状态只来自独立 `ocr.pack.get`：

~~~json
{
  "requestedScope":{"scopeKind":"user","scopeId":null},
  "policySource":{"scopeKind":"user","scopeId":null,"inherited":false},
  "policy": {"mode":"auto","complexDocumentEngine":"paddleocr-vl-1.6","fallbackOrder":["windows-ocr"],"sendToCloud":"never"},
  "revision":"<sha256>",
  "windowsProbe":{"state":"ready","available":true,"languages":["zh-Hans"],"checkedAt":"..."},
  "legacy":{"engineId":"ppocr","registered":true,"state":"registered_unwired","available":false,"markerDetected":true}
}
~~~

`legacy` 未登记时必须为 JSON `null`，不得省略或改成 boolean。requestedScope/policySource、policy 的四个核心字段必填；providerId/modelId 同现同缺，`provider_first`/provider fallback 与 `sendToCloud=configured_only` 的 `oneOf`、fallback/Paddle 条件、枚举/default/additionalProperties=false 全部逐字采用 02-7.1/06-C3。响应不得含 subject、`packRoot/rootRef/contentRef/credentialRef` 或任何绝对路径。`ocr.routing.set` 在一个兼容周期仍解码旧 `preferProvider/localEngine/packRoot`；`packRoot` 仅写请求 scope 的 legacy registration，`localEngine=ppocr` 一律返回 `OCR_LEGACY_ENGINE_UNWIRED`，不更新执行偏好。新客户端只有顶层 `idempotencyKey` 和 payload `scopeKind+scopeId?+policy+expectedRevision(SHA)`，不接受 operationId。
- [ ] **4. 公共 methods。**

| 方法 | payload → result |
|---|---|
| ocr.routing.get | scopeKind,scopeId?,refreshProbe? → requestedScope/policySource/scoped policy/revision/Windows probe/请求scope的legacy registration；scope 严格 oneOf，无绝对路径 |
| ocr.routing.set | scopeKind,scopeId?,policy,expectedRevision → 同形新 routing snapshot；继承态 CAS 原子创建 project override；无 operationId |
| ocr.pack.get | packId → 06-C4 唯一四段 `pack/gate/operation/release` snapshot；字段不得省略，nullable 显式 null，nested objects 均 additionalProperties=false；release 内含 exact preflight/licenseSummary/NOTICE 摘要，无绝对路径 |
| ocr.pack.install | packId,catalogRevision,acceptedManifestDigest,expectedRevision(pack),operationId → `{requestId,operationId,activityId,accepted:true,packId,phase:"requested",packRevision,operationRevision}`，activityId逐字等于operationId |
| ocr.pack.cancel | operationId,expectedRevision(operation) → `{requestId,operationId,activityId,cancelRequested,phase,operationRevision}`；不创建新 operation，activityId 等于目标 operationId |
| ocr.pack.uninstall | packId,expectedRevision(pack),confirmed:true,operationId → 与 install 同形 accepted receipt，activityId 等于 operationId |
| ocr.pack.notice.list | packId,cursor?,limit(1..50,默认20) → `{items,nextCursor}`；按 verifiedAt DESC/manifestDigest ASC，item 为受信 NOTICE 摘要且不含路径 |
| ocr.pack.notice.read | packId,manifestDigest,offset>=0,limit(1..65536) → base64,nextOffset,eof,sha256,totalBytes；只读本机 retained trusted NOTICE；release 可为 null，未知/缺失/损坏记录才 `OCR_PACK_NOTICE_UNAVAILABLE`，零副作用 |
| ocr.run.list | scopeKind,scopeId?,cursor?,limit(1..100) → bounded summaries；scope 严格 oneOf |
| ocr.run.get | runId,pageCursor?,limit(1..20) → summary/page metadata,nextCursor |
| ocr.artifact.read | artifactId,offset,limit(1..65536)（原始字节偏移/长度）→ base64,nextOffset,eof,sha256,totalBytes |

`ocr.pack.get` 的 exact shape（所有对象 `additionalProperties=false`，字段不省略）为：

~~~text
pack:{packId:string,availability:not_installed|ready|quarantined,currentVersion:string|null,previousVersion:string|null,manifestDigest:string|null,engineVersion:string|null,deviceKind:cpu|gpu|null,lastHealthAt:date-time|null,lastErrorCode:string|null,revision:int>=1}
gate:{installAllowed:boolean,autoRouteAllowed:boolean,reasonCode:string|null,verifiedRuntimeProfileDigest:string|null,revision:int>=1}
operation:null|{operationId:string,action:install|uninstall,phase:requested|preflighting|downloading|verifying|installing|self_testing|succeeded|failed|cancelled,completedBytes:int>=0,totalBytes:int>=0,cancelRequested:boolean,terminal:boolean,cancelAllowed:boolean,errorCode:string|null,retryable:boolean,revision:int>=1,createdAt:date-time,updatedAt:date-time}
release:null|{version:string,catalogRevision:string,manifestDigest:string,runtimeProfileDigest:string,compressedBytes:int>=0,expandedBytes:int>=0,deviceProfile:cpu|gpu,preflight:{state:not_run|compatible|incompatible,reasonCode:string|null,requiredDiskBytes:int>=0,availableDiskBytes:null|int>=0,deviceKind:cpu|gpu},licenseSummary:{count:int>=0,spdxIds:string[],nonSpdxLicenseIds:string[]},noticeDigest:string,noticeBytes:int>=0}
~~~

operation `terminal/cancelAllowed` 只能由 phase 按 06-C4 派生；preflight 用 06-C4 的三分支 `oneOf` 并约束 deviceKind 等于 deviceProfile；licenseSummary 两数组 `uniqueItems=true,maxItems=256`、每项 1..128 bytes，count 等于元素总数。新增 ocr.artifact.read 为 Engine-owned、有 subject/scope 鉴权的受限读取；不提供 content_ref/路径直读。routing.get 的 Windows probe 采用 5 分钟缓存，显式 refreshProbe=true 可重测，仍受 deadline；pack progress poll 不触发固定小图识别。
`routing.get/set` 扩展 policy；旧字段仅在 `routing.set` input decoder 保留一个周期，result 不回绝对路径。修改 accepted fields 后先生成 schema 再编译 handler。

`ocr.pack.notice.list` item exact shape 为 `{packId,manifestDigest,version,noticeDigest,noticeBytes,verifiedAt,installedAt:null|date-time,uninstalledAt:null|date-time}`，所有字段必返、`additionalProperties=false`；cursor 绑定 packId 和稳定排序键。catalog verifier 在 release 可用于安装确认前必须先落完整 NOTICE bytes 和 trusted row；关于页只经 list→read 读取，所以断网、卸载或 catalog 换版后仍可查看。`ocr.pack.install` 在 handler 调用 installer 前再次检查 profile gate；失败固定为 `NO_VERIFIED_RUNTIME_PROFILE`、`retryable=false`，并断言 operation/download/staging/runner mutation 均为 0。`ocr.run.*`/`ocr.artifact.read` 的 owner 来自 request identity，payload 的 scope 只能进一步收窄；跨 owner/scope 统一返回 `OCR_SCOPE_FORBIDDEN`，避免泄露存在性。

- [ ] **5. 轮询。** 首发无 `ocr.pack.event` method；前台每 750ms，非活动页面 3s，窗口隐藏暂停，最多一个 in-flight 请求；终态停止，重开立即 get。不假设现有 x-method generator 自动建立 push。
- [ ] **6. 生成/绿测/提交。** 在 `internal/app/handlers_registry.go` 注册方法，在 `internal/app/org_lifecycle.go` 注册本地用户 scope policy；新错误 `REVISION_CONFLICT`，旧 routing 保持 `SETTINGS_VERSION_CONFLICT`。逐条运行：

~~~powershell
npm --prefix web run generate:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge generation failed" }
npm --prefix web run verify:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge verification failed" }
go test -count=1 ./internal/app ./internal/contract -run 'TestOCRRouting|TestOCRPack|TestOCRRun|TestOCRArtifact'
if ($LASTEXITCODE -ne 0) { throw "OCR Bridge tests failed" }
~~~

提交 `feat(ocr): expose recoverable typed pack and run snapshots`。

## Task 6：设置与活动 UI（OCR-009/011/012）

- [ ] **1. 先写导航/唯一性红测。** `web/src/settings/SettingsPage.test.tsx` 断言 `routing` 只渲染六类 `CapabilityRouting` 且不存在“OCR 路由”；`personal` overview 恰好两个可操作卡片“自动记忆”“文字识别”。`web/src/settings/settingsNav.test.ts` 断言 `personal` 中文名为“智能能力”，搜索“OCR/Windows OCR/文字识别”只命中 `personal`，`routing` keywords 不再含 OCR/PP-OCR。
- [ ] **2. 复用 personal route。** `web/src/settings/settingsNav.ts` 只改 `personal` label/keywords，不新增 category。`web/src/settings/SettingsPage.tsx` 的 `routing` 分支删除 `<OCRRouting>`，`personal` 分支把 `providers/ocr` bridge 传给 `PersonalIntelligencePage`。`web/src/m8/PersonalIntelligencePage.tsx` 定义 `type IntelligenceView='overview'|'memory'|'ocr'`：overview 只渲染 P06 已创建的 `<SmartCapabilitiesPanel>` 两卡，P08 只修改/消费它，不得再次创建同名组件；自动记忆进入 `<MemoryPage>`，文字识别进入 `<OCRSettingsPanel>`，详情头提供返回“智能能力”。迁移断言通过后删除 `OCRRouting.tsx` 及旧测试，OCR UI 在整个 Settings 只能由该分支渲染一次。
- [ ] **3. 深链状态与 adapter。** `web/src/app/navStore.ts` 新增 `settingsIntelligenceView`，默认 `overview`；`web/src/App.tsx` 进入 Settings/personal 时传入该值。创建 `web/src/settings/ocrActivityAdapter.ts`，只把 OCR operation/run 映射成 P13 可消费的 domain/activityId/phase/terminal/recovery target，并提供目标 `{settingsCategory:'personal',settingsIntelligenceView:'ocr'}`；其单测断言失败状态生成唯一 OCR 详情目标。P08 不创建、不修改也不测试 `activitySnapshot.ts`、`ActivityStatusButton.tsx`、`ActivityCenter.tsx`；P13/05 Task7 是共享顶部入口的唯一创建与集成阶段。
- [ ] **4. OCR 详情不是开关。** 新建 `web/src/settings/OCRSettingsPanel.tsx`，首行渲染静态 status 文案“文字识别：自动”和说明，使用 `role=status`/普通文本，禁止 `input[type=checkbox]`、`role=switch`、`onChange` 或“保存 OCR 路由”按钮。其同一首屏状态块显示 Windows probe 的真实中文摘要、检查时间和失败时的“重新检查”，但不展开语言列表/底层码。首屏第二项且唯一主动作区是 `OCRModelPackCard`；完整语言、provider、legacy、版本/回退/最近运行均在默认闭合的 `OCRAdvancedPanel`。
- [ ] **5. providers 独立延迟加载。** 全局 Settings 的 OCR detail mount 固定以 `{scopeKind:'user'}` 调用 `ocr.routing.get`（以及同域 `ocr.pack.get`），维护 `coreState/coreError/coreGeneration`；不得从最后打开的 project 猜 scope，也不得与 `providerApi.list()` 放在同一 `Promise.all`。`OCRAdvancedPanel` 第一次展开时才调用 `providerApi.list()`，维护独立的 `providerState/providerError/providerGeneration`，后续展开复用结果，显式“重试供应商列表”才重载。provider reject 时 Windows probe、Paddle 卡、自动状态继续显示且可操作；unmount/新请求通过各自 generation 丢弃迟到响应。
- [ ] **6. legacy 与 gate UI。** legacy 只在高级区显示“已登记，尚未接入”，永不显示“已安装/可用/ready”，不显示本机路径，也没有选择 PP-OCR 为引擎的控件。`packSnapshot.gate.installAllowed=false` 时安装按钮原生 `disabled`，旁边显示 `packSnapshot.gate.reasonCode=NO_VERIFIED_RUNTIME_PROFILE` 的用户文案“尚无经验证的 Windows 运行包”；点击卡片/键盘不得调用 `ocr.pack.install`、目录选择或其他 mutation。只有 release manifest、签名 catalog、preflight 全部满足后才允许确认安装。
- [ ] **7. 安装、许可与自动路由。** 允许安装时，确认层显示 manifest 的真实下载量、展开磁盘量、许可、设备结论和数据流向，确认后发送一次 mutation 并从 receipt 恢复轮询。设置高级区/关于页第一次打开许可证历史时调用 `ocr.pack.notice.list`，选中版本后按块调用 `ocr.pack.notice.read`；断网、卸载和 catalog 换版不得改走 URL，也不得因 `release=null` 隐藏 retained entries。pack ready 也只有 `ocr_paddle_auto_route` gate 通过才参与复杂文档；否则 UI 不宣称自动增强。cloud provider 仍受 `sendToCloud` 授权，不因只读“自动”状态上传。
- [ ] **8. Demo 边界。** 本期正式 OCR 详情不得出现“截图识别”“选择文件”、文件 input、屏幕权限请求、拖放区、静态 OCR 结果或手工 `ocr.run.start`。[09 UI交互演示说明](09-ui-demo-guide.md) 必须把此类元素标为 Demo/未来探索；现阶段运行历史来自生产消费者。测试以 `queryByText(/截图识别|选择文件/)===null` 和 `container.querySelector('input[type=file]')===null` 固化边界。
- [ ] **9. OCR adapter 与安全渲染。** `ocrActivityAdapter.ts` 对 06-C4 的 phase/terminal 做穷尽映射，未知值 fail closed 为不可宣称成功；P13 后续消费它实现顶部运行中/失败入口，普通成功不制造未读红点。`OCRRecentRuns.tsx` 仅按需读取 bounded artifact chunks；Markdown 走现有安全 renderer，禁外部资源/raw HTML/event 属性，preview 超过 1MiB 时分页/导出，不整本进 DOM。
- [ ] **10. 纯黑高端响应式。** 只在 `web/src/styles.css`、`managementLayout.css` 的 Settings/`pi-*`/`ocr-*` scoped selectors 修改：页面 `#000`，surface `#080808/#0d0d0d`，边框 `rgba(255,255,255,.10)`，正文 `#f5f5f5`、次要文字 `#a3a3a3`，focus ring `#fff`；删除这些 selector 下的蓝灰背景/蓝色 glow，保留语义错误色。overview 在 `>=900px` 为等宽两列、最大宽度 1120px；`<900px` 单列；`<680px` 的 `.settings-shell` 改为单列、nav 可横向滚动、内容无固定最小宽，按钮/折叠标题触控高度至少 44px；360px 宽不得横向溢出，并尊重 `prefers-reduced-motion`。
- [ ] **11. 精确前端测试。** 新增/修改测试：

  - `PersonalIntelligencePage.test.tsx::renders exactly two overview cards and one OCR detail`
  - `OCRSettingsPanel.test.tsx::renders automatic as read only and omits demo actions`
  - `OCRSettingsPanel.test.tsx::provider failure does not block core OCR snapshot`
  - `OCRSettingsPanel.test.tsx::loads providers only after advanced opens`
  - `OCRModelPackCard.test.tsx::no verified runtime disables install with zero mutations`
  - `OCRModelPackCard.test.tsx::TestOCRLicenseLink`（retained NOTICE 在卸载后断网仍可经 list/read 查看）
  - `SettingsPage.test.tsx::routing contains no OCR and personal owns it`
  - `App.test.tsx::OCR settings deep link reuses personal detail`
  - `ocrActivityAdapter.test.ts::OCR failure maps to the unique personal OCR detail`

逐条运行：

~~~powershell
npm --prefix web test -- src/settings/settingsNav.test.ts src/settings/SettingsPage.test.tsx src/settings/SmartCapabilitiesPanel.test.tsx src/m8/PersonalIntelligencePage.test.tsx src/settings/OCRSettingsPanel.test.tsx src/settings/OCRModelPackCard.test.tsx src/settings/OCRRecentRuns.test.tsx src/settings/ocrActivityAdapter.test.ts src/App.test.tsx
if ($LASTEXITCODE -ne 0) { throw "OCR settings tests failed" }
npm --prefix web run typecheck
if ($LASTEXITCODE -ne 0) { throw "Web typecheck failed" }
npm --prefix web run build
if ($LASTEXITCODE -ne 0) { throw "Web build failed" }
~~~

提交 `feat(ocr): unify truthful OCR under intelligence settings`。

## Task 7：artifact 生命周期、200 页实测与恢复门（全部 OCR）

- [ ] **1. Artifact 红测。** 相同 digest 不等于授权；他人 run 的 artifact.read 拒绝；删除/TTL 清理不破坏其他 run 引用；文件被改 hash 失败。
- [ ] **2. 存储。** 使用06-C4独立OCR CAS root和artifact_store.go保存每页text/Markdown/layout，ocr_artifacts 维护 ACL/引用/30天默认保留。run.get 不内联大正文；用户导出后可选择保留。到期清理run解除引用，OCR独立root仅在所有ocr_artifacts引用与ocr_artifact_leases均无有效记录时GC，禁止清理全局共享CAS。CAS 未提供加密即按当前应用数据目录 ACL 管理，文档不得宣称已加密。
- [ ] **3. Corpus。** ≥200 页，40 简单+40 扫描+60 复杂排版表格+30 公式图表印章+30 极端/空白。生成合成页或使用许可明确数据；manifest 固定源 digest/license/category/language/annotationRevision/split，两人标注仲裁。缺数据/许可/引擎/指标为 INCOMPLETE。
- [ ] **4. Evaluator。** `scripts/ocr-eval.ps1` 必须声明唯一 `-Scenario OCR200PageGate`，运行真实 Windows/Paddle 两引擎，同硬件/config/corpus，逐页输出 CER/WER、block/reading-order/table F1、幻觉/空白误报、cold/warm p50/p95、peak RSS/CPU/失败率；运行 manifest 将该 scenario 精确登记为 12 的 canonical PowerShell anchor。首版 gate 依 PRD 13.2；vendor 分数只作背景。
- [ ] **5. 故障场景。** scripts/test-paddleocr-vl-pack.ps1 必须实现 Scenario=TamperedManifest|WorkerTimeout|ActivationCrash|CleanOffline。每个场景真实注入对应故障并检查 SQLite/current/worker/fallback；DryRun 仅参数检查，不能写已通过故障演练。
- [ ] **6. 基线验证命令。** 逐条执行，报告保存版本、hash、命令、退出码、原始输出和遗漏项：

~~~powershell
go test -count=1 ./internal/ocrapp ./internal/doctext ./internal/storage/sqlite ./internal/app ./internal/toolruntime ./internal/bootstrap ./internal/contract
if ($LASTEXITCODE -ne 0) { throw "OCR Go suites failed" }
go test -count=1 -tags ocrintegration ./internal/doctext -run '^TestProbeWindowsOCRIntegration$'
if ($LASTEXITCODE -ne 0) { throw "Real Windows OCR integration failed" }
npm --prefix web run verify:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge verification failed" }
npm --prefix web test -- src/settings/settingsNav.test.ts src/settings/SettingsPage.test.tsx src/settings/SmartCapabilitiesPanel.test.tsx src/m8/PersonalIntelligencePage.test.tsx src/settings/OCRSettingsPanel.test.tsx src/settings/OCRModelPackCard.test.tsx src/settings/OCRRecentRuns.test.tsx src/settings/ocrActivityAdapter.test.ts src/App.test.tsx
if ($LASTEXITCODE -ne 0) { throw "OCR UI suites failed" }
npm --prefix web run typecheck
if ($LASTEXITCODE -ne 0) { throw "Web typecheck failed" }
npm --prefix web run build
if ($LASTEXITCODE -ne 0) { throw "Web build failed" }
~~~

若没有 `verifiedRuntimeProfileDigest`，到此只可验收“Windows OCR + disabled Paddle 卡”的诚实基线；pack corpus/故障脚本必须报告 `INCOMPLETE`，不能把未执行算 PASS。
- [ ] **7. Paddle 启用门（仅 Task0 已通过时）。** 逐条执行 CleanOffline、三种故障和 corpus；任一失败即不发布 catalog enabled entry，UI 回到 `NO_VERIFIED_RUNTIME_PROFILE`：

~~~powershell
./scripts/test-paddleocr-vl-pack.ps1 -PackPath ./artifacts/paddleocr-vl-1.6/pack -Scenario CleanOffline
if ($LASTEXITCODE -ne 0) { throw "CleanOffline failed" }
foreach ($scenario in @('TamperedManifest','WorkerTimeout','ActivationCrash')) {
  ./scripts/test-paddleocr-vl-pack.ps1 -PackPath ./artifacts/paddleocr-vl-1.6/pack -Scenario $scenario
  if ($LASTEXITCODE -ne 0) { throw "Paddle failure scenario failed: $scenario" }
}
./scripts/ocr-eval.ps1 -Scenario OCR200PageGate -Manifest ./testdata/ocr-eval/manifest.json -PackPath ./artifacts/paddleocr-vl-1.6/pack
if ($LASTEXITCODE -ne 0) { throw "OCR corpus gate failed" }
~~~

- [ ] **8. 交付/提交。** runbook 记录安装/取消/更新/卸载/恢复、数据位置、offline policy 实际边界、完整依赖 NOTICE，并实跑卸载后断网的设置/关于页 `notice.list→notice.read`。提交 `test(ocr): enforce real quality and recovery release gates`。只有 Task0/全部 P0 通过才能开 install；auto 必须单独通过硬件盲测。

## 完成顺序

冻结阶段为：`P07 = Task1 + Task3 的 Windows probe 部分 + Task4 + Task5 的 disabled handlers + Task7 的 artifact 基线`，随后 `P08 = Task6`；`P09 = Task0` 单独验证真实 runtime profile。只有 P09 产生已签且匹配的 `verifiedRuntimeProfileDigest` 后，才执行 `P10 = Task2 + Task3 的 Paddle worker 部分 + Task7 的 Paddle corpus/故障门`。Task2 不得在 W0/P09 之前以“先开发 fail-closed installer”为由进入完成态或合入可下载路径。P09 未成功不阻塞 Windows OCR 与两卡 UI 的诚实基线，但 OCR-002/004/005/012 的 Paddle 部分必须标 `INCOMPLETE`，绝不能宣称完成。保留 Windows OCR 是已确定产品决策，无需再询问替换方案。

## 视觉验收（必须附截图/录屏与 DOM 证据）

| 场景 | 通过条件 |
|---|---|
| 1440×900 / 1024×768 | “智能能力”overview 只出现两张等宽卡；OCR 不在“路由管理”；页面底色 computed style 为 `rgb(0,0,0)`，surface 仅 `#080808/#0d0d0d`，目标 selector 无蓝灰背景/蓝 glow |
| 899×768 | 两卡切为单列，标题/状态/主动作视觉层级不变；高级区闭合时 provider 请求数为 0 |
| 680×800 / 390×844 / 360×800 | Settings 单列，导航可触控/滚动，`document.documentElement.scrollWidth === clientWidth`；按钮与折叠标题高度 ≥44px，文字不截断、无重叠 |
| 键盘/辅助技术 | Tab 顺序为返回→只读状态→Paddle 主动作→高级区；“文字识别：自动”不是 switch；disabled 安装按钮带可感知原因；focus ring 对比清晰 |
| 故障注入 | provider list reject 后 Windows/Paddle 仍显示；WinRT probe 失败显示具体非 ready 状态；无 verified runtime 时安装 disabled 且 mutation spy=0 |
| 内容边界 | 正式详情无“截图识别/选择文件”、file input、拖放区、静态识别结果；legacy 只显示 `registered_unwired/不可用` 且 DOM/console/activity 无绝对路径 |

视觉证据至少包含上述五种 viewport 的 overview/OCR detail、键盘 focus、provider failure 和 disabled gate。截图只能证明布局；DOM 请求计数、computed style、无横向溢出和 mutation=0 必须由测试/DevTools 输出证明。

## 配套 PRD/合同一致性核对（编码前门）

1. `02-upgrade-prd.md` §7.7 必须持续保持“智能能力 overview 恰好两卡、OCR 唯一详情、自动为只读策略”，且 PP-OCR 仅为 `registered_unwired/Available=false`，Paddle 无 verified runtime 时固定 `NO_VERIFIED_RUNTIME_PROFILE`；不得恢复早期四卡/四常驻区。
2. `02-upgrade-prd.md` OCR-009/012 必须持续包含 provider 延迟加载隔离、Bridge 不返绝对路径、旧字段仅一个兼容发布周期及“无 runtime 时 mutation=0”。
3. `06-integration-contracts-plan.md` C3 是 `OCRScope/ResolvedOCRRequest/ProviderFunc` 的跨模块唯一合同；本文实现必须逐字段匹配，并明确 resolver 单次 snapshot、各 consumer 的 scope 来源与执行中禁止二读 routing。
4. `07-acceptance-and-scorecard.md` 必须持续覆盖唯一 UI、provider failure isolation、legacy unwired、真实 Windows integration、gate zero-mutation 和 360px 响应式测试；不得把 mock probe/空 corpus 计通过。
5. `09-ui-demo-guide.md` 与现行 Demo 必须持续不提供截图/选文件 OCR 工作台，并保持纯黑色值、两卡唯一入口和断点；原型不得被用作已接线证据。

若上述文档出现不一致，冲突项必须停止；按 11 的规范优先级先同步修正文档与验收，不能以本文覆盖 02/06，也不能让开发者临场选一种实现。

## 1–5 评分与五分门

当前代码审计分为 **1/5**：真实 WinRT 执行链存在，但 availability 仍由 OS 推断、PP-OCR marker 仍可制造 ready、routing 是全局且执行中二读、provider 加载拖垮 OCR UI、Settings 重复入口且非纯黑、Paddle runtime 与真实 gate 证据不存在。每满足一项得 1 分，基线发布必须 **5/5**，不得平均、豁免或以计划/原型代替实现：

1. 真实 Windows probe 在发布 Windows 机执行通过，失败/缺语言明确非 ready。
2. legacy PP-OCR 永远 `registered_unwired/Available=false`；无 verified Paddle runtime 时 UI disabled、handler `NO_VERIFIED_RUNTIME_PROFILE`、mutation=0、无路径泄露。
3. 所有生产消费者使用授权 `OCRScope` 与一次性 `ResolvedOCRRequest`；执行期 routing/provider/gate 二读为 0，默认 cloud calls=0。
4. Settings 只有“智能能力”两卡和唯一 OCR 详情；provider 延迟加载失败不拖核心；只读自动状态、Demo 边界及 1440→360 纯黑响应式全部通过。
5. allocation 冻结后的 OCR logical migration 升级/回滚恢复、Bridge generation、Go/UI/integration 命令与视觉证据均绿；缺项明确 `INCOMPLETE`。

达到基线 5/5 只允许发布 Windows OCR 与受 gate 保护的 Paddle 卡。要把 Paddle 安装从 disabled 改为 enabled，还必须额外满足 Task0 W0、签名 catalog digest 匹配及 Task7 Paddle corpus/故障门；否则维持 disabled 不影响基线评分。
