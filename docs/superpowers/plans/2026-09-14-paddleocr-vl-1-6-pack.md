# PaddleOCR-VL-1.6 Optional Model Pack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用户可自行安装完整 PaddleOCR-VL-1.6 复杂文档识别包，Windows OCR 和已有 provider OCR 继续可用。
**Architecture:** 受信任签名的 Windows x64 CPython/Paddle/models pack，由 Go Engine 启动 Python stdio worker。SQLite 管状态，后台 runner 管安装，UI 轮询 snapshot；按页路由并保留引擎/回退证据。
**Tech Stack:** 当前 Go/modernc SQLite、Windows Job Object、Python/Paddle 官方完整 pipeline、React/TypeScript、JSON Schema Bridge。
**Spec:** [完整 PRD](../specs/2026-09-14-memory-ocr-media-ui-upgrade-prd.md)，OCR-001～012。
**Revision:** 2026-09-15。本文是开发计划；真实 pack 构建、Windows 性能与 200 页盲测属于实施门禁，没有预先通过。

## Global Constraints

- 主程序不附带模型，不自动下载，用户明确安装后才启用；Windows OCR 为默认本地快速/灾备路径。
- engine IDs 固定 windows-ocr、paddleocr-vl-1.6、provider:<providerId>/<modelId>。
- 必须是 layout + VLM 完整 pipeline；只运行 VLM 不可标 full。confidence 上游没提供则 null。
- 首发只采用受管 Python stdio worker；不启动 HTTP 推理服务，不在用户机器临时 pip install，不依赖用户全局 Python/WSL/Docker。
- Job Object 限制进程/内存并回收，不是网络或文件系统沙箱。offline policy 指受信任一方代码只读本地模型和显式输入、无凭据/代理/自动下载；不得宣传 OS 级禁网。
- 0163 必须在已协调的 0162 后；更新 store.go 真正 manifest/checksum/expectedSchemaSQL/列清单。
- 旧 Result.Text/Method/Source/Pages/Coverage/Complete/Uncertain 保持；新 result 通过 RecognizeDocumentV2 提供，旧方法做显式映射。
- routing.* 保持现有 SHA revision 和 SETTINGS_VERSION_CONFLICT；新 pack aggregate 使用整数 revision + REVISION_CONFLICT。
- 顶层 envelope idempotencyKey，payload operationId/expectedRevision；request digest 服务端计算。
- 模型包物理安装为当前本地应用实例共享资源，仅本地用户设置页可改；OCR runs/results 按 subject/scope 隔离。
- ocr_paddle_pack_install/ocr_paddle_auto_route 在新 settings/gate 表持久化；默认 off。只有对应硬件实测通过才进入 auto。
- 安装 mutation 立即返回 operation receipt，下载/自测在 Engine context 后台运行。前端断开不自动取消。
- 完整流水线的“literalText”仍是识别文本，不是原件真值；所有生成 Markdown/HTML 按不可信内容渲染。

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
| 1 | migrations/0163_ocr_model_packs.sql；internal/storage/sqlite/store.go、ocr_model_packs.go、ocr_model_packs_test.go | repository、settings、安装状态/操作、scoped runs/artifacts |
| 2 | internal/ocrapp/catalog.go、catalog_test.go、installer.go、installer_test.go | manifest/签名/download/后台状态机 |
| 3 | internal/ocrapp/worker.go、worker_test.go、result.go；internal/doctext/ocr_probe_windows.go、ocr_probe_other.go | stdio、资源、结果协议、真实 Windows probe |
| 4 | internal/ocrapp/route.go、recognize.go、health.go 及测试；internal/app/ocr_wire.go；internal/bootstrap/wire.go | 兼容 adapter、路由、生产装配 |
| 5 | internal/app/ocr_pack_handlers.go、ocr_pack_handlers_test.go、s1_bridge_handlers.go、handlers_registry.go、org_lifecycle.go；api/bridge/v1/ocr.*.schema.json | 公开合同 |
| 6 | web/src/settings/OCRRouting.tsx、OCRModelPackCard.tsx、OCRRecentRuns.tsx 及 .test.tsx | 安装/隐私/最近运行 UI |
| 7 | internal/ocrapp/artifacts.go、artifacts_test.go；testdata/ocr-eval/manifest.json；scripts/ocr-eval.ps1；docs/ocr/paddleocr-vl-runbook.md | scoped 结果存储、评测、恢复 |

## Task 0：先做可发布的真实 Windows 离线 pack（OCR-002/004/005/012）

- [ ] **1. 建失败门。** scripts/test-paddleocr-vl-pack.ps1 -Scenario CleanOffline：输入是构建产物路径，检查 manifest、runtime、worker、layout/VLM、NOTICE/SBOM 是否齐全；当前无 pack，应明确返回 INCOMPLETE/非零，而不是用 fake pipeline PASS。
- [ ] **2. 构建源。** 固定官方 PaddleOCR/PaddleX/PaddlePaddle 的已验证版本及 Windows x64 CPU wheel；构建时从官方发布元数据解析精确版本/URL/hash并写 requirements.lock，禁止 latest、浮动范围和无 hash dependency。CPython 使用可再分发受管 runtime，构建 CI 安装 wheel 到 pack runtime；不假定普通 embedded Python 无配置即可加载 Paddle DLL。
- [ ] **3. 配置。** 先用官方 PaddleOCRVL 的 v1.6 默认配置导出完整 pipeline YAML，锁定到 Git；将所有模型路径改为 pack 内相对路径并禁用自动下载。具体 constructor 参数由已锁 wheel 的签名/官方示例验证后写入 worker，不臆造 Go layout.Analyze 或 VLM binding。
- [ ] **4. Python worker。** 入口明确只接受 --config 和 Engine 私有任务参数；配置文件属于签名 pack，不允许 renderer 传任意 YAML。固定本地 pipeline 初始化后从 stdin 接收 JSONL，调用官方 pipeline.predict，逐页映射 block/文字/结构/警告，stdout 只发协议，日志去受控 stderr。首次自测必须真实执行布局和 VLM 两 stage。

~~~python
# protocol.schema.json 对应的协议头；不是模型识别的替代实现
PROTOCOL_VERSION = 1
PIPELINE_KIND = "paddleocr_vl_full"
ALLOWED_INPUT_TYPES = {"page_image"}
~~~

- [ ] **5. 真实验证。** 新建构建脚本支持 -OutputRoot，并将唯一待测的完整包展开到 artifacts/paddleocr-vl-1.6/pack。构建完成后执行 `./scripts/test-paddleocr-vl-pack.ps1 -PackPath ./artifacts/paddleocr-vl-1.6/pack -Scenario CleanOffline`，立即检查退出码。验证必须在干净 Windows x64 CPU、无 Python、无模型缓存、阻断网络的 CI/VM 中跑两页表格/多栏；报告记录实际路径、两个 stage/model identity、输出字段、冷启动、峰值 RSS、DLL 依赖和进程退出。路径不存在必须失败。
- [ ] **6. 发布条件。** 生成 immutable pack、逐文件 SHA-256、压缩/展开 bytes、SBOM/NOTICE。签名由发布流程完成，私钥不进 repo。W0 未通过的 profile 不出现在 enabled catalog；此时继续可做 installer 单测，但不能上线安装入口。
- [ ] **7. 提交。** 仅 worker 源、lock/config、脚本与脱敏报告；commit `build(ocr): validate the Windows offline Paddle pipeline pack`，不提交模型权重/临时 venv。

## Task 1：SQLite repository、设置及恢复真相（OCR-003/007）

- [ ] **1. 红测。** ocr_model_packs_test.go：0159→0160→0161→0162→0163 升级、重复打开、checksum 错误、一个 pack 两个 active mutation 拒绝、跨主体 get/list 拒绝。
- [ ] **2. 执行。** `go test -count=1 ./internal/storage/sqlite -run TestOCRPack`。预期新表/方法缺失导致测试未通过；实现契约后再验证行为红测。
- [ ] **3. 表。** 0163 包含以下实体，精确字段遵循 PRD 7.6；全部 ID/enum/JSON/length/bytes 约束在 SQL 和 domain 双重执行。

| 表 | 真相/关键约束 |
|---|---|
| ocr_pack_state | pack_id PK；not_installed 也有行；current/previous version、active op、integer revision |
| ocr_pack_versions | (pack_id,version) PK；immutable manifest_digest/root_ref；verified/quarantined |
| ocr_pack_operations | subject、operation/key/request digest、accepted manifest、phase/progress/cancel/result/revision |
| ocr_settings | subject policy JSON、SHA policy_revision、integer settings revision；兼容 FileStore 导入标记 |
| ocr_pack_gates | local singleton，install/auto flags、integer revision、updated_at |
| ocr_document_runs | owner_subject_id/scope_kind/scope_id、源 digest、policy snapshot、state/pages/engine evidence |
| ocr_page_results | (run_id,page) PK；typed engine、complete/uncertain、layout/ref/warnings |
| ocr_artifacts | artifact_id、owner/scope/run、CAS ref/hash/size/MIME、expiry |

活动 operation partial unique index 排除 succeeded/failed/cancelled。settings 迁移一次读取旧 FileStore，在事务中固定旧 revision/原偏好；旧文件保留备份但新 SQLite 成为真相。需要把 Service.store 从 *FileStore 改为最小 RoutingStore interface，并将 health 持久化从 FileStore 私有方法分离为注入 HealthStore，不能只替换类型却遗漏 health.go。

- [ ] **4. repository API。**

~~~go
type RoutingStore interface {
    Get() (Routing, error)
    CompareAndSet(Routing, string) (Routing, error)
}
// 新 pack/run repository 方法均接 context，读取 run 必须包含 identity/scope。
// PackSnapshot/PackOperation/RunSummary 使用与 schema 一致的显式 json tags。
~~~

安装 mutation 在同事务检查 key/digest、CAS、活动操作唯一性，再 insert operation；重放返回原 operation。bootstrap/wire.go 注入 SQLite repo、CAS、catalog verifier、runner、launcher，测试正式 composition root。
- [ ] **5. 绿测/提交。** `feat(ocr): persist pack operations scoped runs and routing settings`。

## Task 2：签名 catalog 与异步安装（OCR-002/003/007/011/012）

- [ ] **1. 红测。** catalog_test/installer_test：签名/byte/hash/key/expiry 被改拒绝；下载 50% 重启可恢复；atomic commit 前后断电各有确定 current；同 pack 二次安装拒绝并返回 active op。
- [ ] **2. 执行。** `go test -count=1 ./internal/ocrapp -run 'TestOCRCatalog|TestOCRInstall'`。
- [ ] **3. 签名。** Ed25519 直接签 manifest.json 原始 UTF-8 bytes，签名在 manifest.sig；不重排 JSON。signature 使用 base64url 无 padding，keyId 查内置 active/retired/revoked 表；过期/撤销/未知 key 拒绝。publisher/client 共享固定测试向量。catalogRevision=SHA256(catalog bytes)，acceptedManifestDigest=SHA256(manifest bytes)。
- [ ] **4. 安装请求。** packId/catalogRevision/acceptedManifestDigest/expectedRevision/operationId；用户在 manifest 大小、许可、设备预检后点击安装即完成 consent。没有第二个悬空 awaiting_consent 后台状态。Engine 写 requested receipt 后立即返回，runner 绑定 Engine lifecycle context。

~~~text
requested → preflighting → downloading → verifying → installing → self_testing → ready
任一步失败 → failed（旧 current 仍可用）
downloading + cancel → cancelled
verified version + self-test success → SQLite CAS current/previous → ready
ready + uninstall → uninstalling → not_installed
~~~

- [ ] **5. 文件与崩溃顺序。** 专用 downloader 复用网络 URL/重定向 policy；仅 manifest allowlist；staging 路径从内部 ID 生成。拒绝 zip-slip、重复规范化名、NTFS ADS、symlink/reparse、解压炸弹，磁盘预检=下载剩余+展开+旧版保留+10%裕量。校验/展开/self-test 完成后原子 rename 到 immutable version，最后 SQLite CAS 切 current；current.json 只是可重建缓存。崩溃后 DB 未指向的新版本是 orphan，可验证后重试或清理；DB 指向损坏版本则隔离并事务退 previous。
- [ ] **6. 取消/重试。** 持久 cancel_requested，下载每块检查；原子切换不可取消返回 OCR_OPERATION_NOT_CANCELLABLE。启动 recovery 将 lease 过期 job 重新 claim，重放不重复切换。运行中版本持有 lease，卸载先停止新作业、取消/等待现有 lease 释放，再清受管 pack；保留 scoped OCR 结果与 NOTICE。
- [ ] **7. 审计/提交。** 使用 m7_audit_events 的开放 action 合同，写 operation/stage/digest/error，不改旧 audit_events 闭集；绿测后 `feat(ocr): install signed packs with resumable operations`。

## Task 3：真实 worker、资源和 Windows OCR probe（OCR-004/005/007/008）

- [ ] **1. 红测。** worker_test：入口被改拒绝、URL/越权 input 拒绝、坏 JSON/乱序/输出超限/挂死终止、缺 layout 不标 full；probe 测 WinRT 不可初始化/无语言包与真实 ready。
- [ ] **2. 执行。** `go test -count=1 ./internal/ocrapp ./internal/doctext -run 'TestOCRWorker|TestOCRProbe'`。
- [ ] **3. 启动。** 从已验 manifest 解出 runtime/python.exe、固定 -I worker/paddleocr_worker.py 和 pipeline config；再次校验 worker/runtime/DLL/config hashes。通过 SpawnIsolatedWithStderr 原语启动，OCR 自己的 gate 在调用前检查；不打开无关 MCP stdio gate。env allowlist 仅 SystemRoot、受管 TEMP/TMP、必要 DLL/runtime paths、本地模型配置和线程数，API keys/proxy/PYTHONPATH 不继承。
- [ ] **4. 协议。** 每帧 v/jobId/seq/type；input 只含 source digest、task-local page reference、page number；输出 header/block/page_done/failed。单帧≤1MiB，每页≤8MiB，任务≤64MiB，input≤100页/每页20MP；超过阈值明确 warning/error。stdout/stderr 分离，stderr 限 1MiB 环形脱敏缓冲。序号错/EOF 半 JSON/未知 type fail closed。
- [ ] **5. 资源。** 一次最多 1 worker/1推理任务；Job Object MaxProcs、MemoryCapBytes 来自已验证 runtime profile；wall watchdog/输出字节在 Engine 执行。CPU thread cap 通过已锁 Paddle runtime 设置并记录，不能把它当硬 CPU quota。临时盘定期检查阈值并取消；不是内核文件配额。profile 不提供合法非零 limits 则禁止安装。
- [ ] **6. Probe。** 新增 doctext.ProbeWindowsOCR，实际初始化 Windows.Media.Ocr、枚举语言并识别随应用的固定小图；超时 15s。返回 languages/available/errorCode/checkedAt，HealthSnapshot 显示真实结果，非 Windows stub 明确 unavailable。
- [ ] **7. 真实 integration/提交。** unit fake 只验证协议；Task0 CleanOffline 再跑真实产物确认两个 stage 与资源，提交 `feat(ocr): run the verified local pipeline and probe Windows OCR`。

## Task 4：兼容结果、逐页路由与生产接线（OCR-001/006/007/008）

- [ ] **1. 表驱动红测。** recognize_test 覆盖下面矩阵，每个 case 断言 engine identity、fallback chain、cloud calls、complete/warnings。
- [ ] **2. 执行。** `go test -count=1 ./internal/ocrapp ./internal/doctext ./internal/app -run 'TestOCRRoute|TestOCRCompatibility'`。
- [ ] **3. 路由。** 文本层覆盖的页首先直用，不重复 OCR；其余按矩阵生成有序健康引擎列表，再去重尝试。

| mode / 条件 | 首选 | 后续 |
|---|---|---|
| local_fast | Windows | 不云端；Windows 失败返回 partial |
| local_document、pack ready | Paddle | Windows；仅 policy 明确允许时 provider |
| local_document、pack 缺失 | 不保存不可用设置；一次任务可 Windows | 提示安装，不自动下载 |
| auto、简单/auto gate off | Windows | policy 中允许且健康的 fallback |
| auto、复杂+ready+profile gate pass | Paddle | Windows，再按 policy 允许的 provider |
| provider_first、configured_only+健康绑定 | 精确 provider ID/model | policy 中本地 fallback |
| provider_first、sendToCloud=never | Windows 或经 gate 的本地 Paddle | cloud calls=0 |

fallbackOrder 仅能在许可/安装/健康过滤后的集合排序，不能绕过 never。旧 preferProvider=true 迁移 provider_first 并保持原已有明确 provider 配置；false 迁移 auto/never。无已配置 provider 时不得由模型目录猜一个上传。

- [ ] **4. 兼容。** 新增 RecognizeDocumentV2(授权 scope + 输入) 返回 run/pages/artifact refs；旧 RecognizeDocument 保留 Result 字段，由 V2 映射 text/method/source/coverage。kb_search_handlers.go、model_catalog.go、office_source_text.go 加回归，不能静默改变他们期待的 Text。
- [ ] **5. 逐页持久化。** 输出按 run+page 幂等，mixed/partial 明确；成功页不因失败页重做。无 confidence 写 null。模型输出和 provider 错误正文不进 UI 错误。
- [ ] **6. 绿测/提交。** 正式 bootstrap 使用新 dependency graph 并保留 local functions；提交 `feat(ocr): route pages with compatibility and explicit fallbacks`。

## Task 5：Bridge snapshot 与查询权限（OCR-003/009）

- [ ] **1. 红测。** handler 测 stale 合法 revision、缺 key、未知 manifest、跨 scope run、无 runtime；旧 routing schema 继续解码。不能传 "stale" 这种非法 hash 来假测 CAS。
- [ ] **2. 执行。** `go test -count=1 ./internal/app ./internal/contract -run 'TestOCRPackBridge|TestOCRRunBridge|TestOCRRouting'`。
- [ ] **3. 公共 methods。**

| 方法 | payload → result |
|---|---|
| ocr.pack.get | packId → pack/operation/catalog/preflight/license/Windows probe snapshot |
| ocr.pack.install | packId,catalogRevision,acceptedManifestDigest,expectedRevision,operationId → accepted receipt |
| ocr.pack.cancel | operationId,expectedRevision → cancel requested/current terminal |
| ocr.pack.uninstall | packId,expectedRevision,confirmed,operationId → accepted receipt |
| ocr.run.list | scopeKind,scopeId,cursor?,limit(1..100) → bounded summaries |
| ocr.run.get | runId,pageCursor?,limit(1..20) → summary/page metadata,nextCursor |
| ocr.artifact.read | artifactId,offset,limit(1..65536) → bytes/text chunk,nextOffset,eof |

新增 ocr.artifact.read 为 Engine-owned、有 subject/scope 鉴权的受限读取；不提供 content_ref/路径直读。pack.get 的 Windows probe 采用 5 分钟缓存，显式 refreshProbe=true 可重测，仍受 deadline，不在每次 progress poll 识别小图。
routing.get/set 扩展 policy，旧字段保留；修改 accepted fields 后先生成 schema 再编译 handler。

- [ ] **4. 轮询。** 首发无 ocr.pack.event method；前台每 750ms，非活动页面 3s，窗口隐藏暂停，最多一个 in-flight 请求；终态停止，重开立即 get。不假设现有 x-method generator 自动建立 push。
- [ ] **5. 生成/绿测/提交。** 注册 mutation/scope policy；新错误 REVISION_CONFLICT，旧 routing SETTINGS_VERSION_CONFLICT；提交 `feat(ocr): expose recoverable typed pack and run snapshots`。

## Task 6：设置与活动 UI（OCR-009/011/012）

- [ ] **1. 红测。** OCRModelPackCard.test.tsx：未安装不下载、manifest bytes/许可可见、点击后返回 accepted、刷新恢复真实进度、不可取消阶段禁用按钮、无语言包不显示 ready。
- [ ] **2. 执行。** `npm --prefix web test -- src/settings/OCRRouting.test.tsx src/settings/OCRModelPackCard.test.tsx src/settings/OCRRecentRuns.test.tsx`。
- [ ] **3. UI。** 策略、Windows probe、可选包、最近运行四块；只显示实测 profile/manifest 大小，不编造 RAM/VRAM。复杂本地模式未安装时打开安装说明，不能保存无效配置；取消安装不会改 cloud preference。
- [ ] **4. 活动投影。** OCR 仅导出 pack/run snapshots；媒体计划建立的 web/src/activity/activitySnapshot.ts 把它适配为 domain=ocr，无第二套活动库，无依赖 media backend 才能运行的 OCR service。
- [ ] **5. 渲染。** Markdown 使用现有安全 renderer，拒绝外部资源、raw HTML/event 属性；有界加载 artifact chunks，preview 超过 1MiB 时提示导出/分页，不把整本书塞 DOM。
- [ ] **6. 绿测/typecheck/提交。** `npm --prefix web run typecheck`，提交 `feat(ocr): add optional pack settings and recovery UI`。

## Task 7：artifact 生命周期、200 页实测与恢复门（全部 OCR）

- [ ] **1. Artifact 红测。** 相同 digest 不等于授权；他人 run 的 artifact.read 拒绝；删除/TTL 清理不破坏其他 run 引用；文件被改 hash 失败。
- [ ] **2. 存储。** 复用 workspace.CASStore 保存每页 text/Markdown/layout 小对象，ocr_artifacts 维护 ACL/引用/30天默认保留。run.get 不内联大正文；用户导出后可选择保留。删除 run 解除引用，CAS 只在引用计数归零且没有读取 lease 时 GC。CAS 未提供加密即按当前应用数据目录 ACL 管理，文档不得宣称已加密。
- [ ] **3. Corpus。** ≥200 页，40 简单+40 扫描+60 复杂排版表格+30 公式图表印章+30 极端/空白。生成合成页或使用许可明确数据；manifest 固定源 digest/license/category/language/annotationRevision/split，两人标注仲裁。缺数据/许可/引擎/指标为 INCOMPLETE。
- [ ] **4. Evaluator。** scripts/ocr-eval.ps1 运行真实 Windows/Paddle 两引擎，同硬件/config/corpus，逐页输出 CER/WER、block/reading-order/table F1、幻觉/空白误报、cold/warm p50/p95、peak RSS/CPU/失败率。首版 gate 依 PRD 13.2；vendor 分数只作背景。
- [ ] **5. 故障场景。** scripts/test-paddleocr-vl-pack.ps1 必须实现 Scenario=TamperedManifest|WorkerTimeout|ActivationCrash|CleanOffline。每个场景真实注入对应故障并检查 SQLite/current/worker/fallback；DryRun 仅参数检查，不能写已通过故障演练。
- [ ] **6. 验证命令。** 逐条执行 `go test -count=1 ./internal/ocrapp ./internal/doctext ./internal/storage/sqlite ./internal/app ./internal/bootstrap`、UI 测试/typecheck、PRD 13.4 全门。报告包含版本、hash、命令、退出码、原始输出和遗漏项。
- [ ] **7. 交付/提交。** runbook 记录安装/取消/更新/卸载/恢复、数据位置、offline policy 实际边界、完整依赖 NOTICE。提交 `test(ocr): enforce real quality and recovery release gates`。只有 Task0/全部 P0 通过才能开 install；auto 必须单独通过硬件盲测。

## 完成顺序

Task0 → Task1 → Task2/3 → Task4 → Task5 → Task6 → Task7。Task0 未成功不妨碍无模型单测和接口开发，但该 profile 不可发布。保留 Windows OCR 是已确定产品决策，无需再询问替换方案。
