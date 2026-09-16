# 三角复核报告（R2历史审计）：需求、旧PRD、当时代码

审计日期：2026-09-15。本文原始审计基线为 `5970012dfe067ba7a6167417f896bc48650cefc0`，存在用户未提交修改；它保留用于解释 R2 的 24 项历史缺陷，**不再表示当前代码状态**。R3 当前基线、追加缺陷和最终评分见 [10 最终复核与五分整改](10-final-audit-and-five-point-remediation.md)，唯一执行映射见 [12 需求追踪矩阵](12-requirements-traceability.md)。

## 1. 结论

**原生Go/SQLite记忆升级、保留Windows OCR并选装Paddle、借鉴媒体连续体验的方向正确；旧文档还不能做到无需开发者猜测地执行。** 问题不是缺少技术名词，而是存在来源ID、权限、状态、生命周期和旧调用者接线缺口。

本文当时按[统一五分标准](07-acceptance-and-scorecard.md)记录旧版 3.4/5、R2 自评 4.4/5。R3 交叉审计发现新的代码漂移、UI 合同冲突和追踪缺口，故将整改前实施就绪度更正为 **3.6/5**；以 10 的复核为准。产品升级后的实际分数不能由文档推定；满分必须补真实可行性、恢复和效果证据。

## 2. 三个审计角度

| 角度 | 旧版优点 | 主要缺陷 | R2处理 |
|---|---|---|---|
| 你的需求 | 自动记忆、选装OCR、媒体/工具UI方向明确 | 最初15维和语音/办公/代码回归缺显式边界；“忘记”容易理解过强 | 08逐项映射，07端到端测量，06明确forget/音频焦点 |
| PRD自洽 | 状态、表、错误、性能、回滚已有框架 | 捕获时机/纠正/观察/旧读降级互相矛盾；批次预算不等于总成本 | 统一C1/C2；状态与版本有唯一合同，新增累计预算 |
| 当前代码 | 两记忆平面、现有OCR和媒体假成功识别基本正确 | 将scope扩展点、CAS、活动分页、SMTC能力当现成；遗漏真实消费者 | 真实入口复查；X0–X3补后端、Host、App和缓存接线 |

## 3. 缺陷台账与修正定位

P0：数据隔离、原数据保护或成功真实性不闭合，阻断上线。P1：核心功能无法按描述完成，阻断对应子功能。P2：质量/说明/维护改进。以下为文档/现码发现，不表示本轮已经修复产品代码。

| ID/级别 | 证据与问题 | R2决策/实施位置 |
|---|---|---|
| F01 P0 | [chat_run_stream.go:1511](E:/Trae-Work-Projects/lunitide/internal/app/chat_run_stream.go:1511)持久化 assistant，[chat_run_stream.go:1600](E:/Trae-Work-Projects/lunitide/internal/app/chat_run_stream.go:1600)再把该 messageID 传给记忆；[chat_memory.go:619](E:/Trae-Work-Projects/lunitide/internal/app/chat_memory.go:619)却拼 chat-user 来源 | C1真实user receipt+原文span；03 Task2、X1；历史无法唯一修复进入review |
| F02 P1 | [Message:31](E:/Trae-Work-Projects/lunitide/internal/domain/message/message.go:31)没有source_revision；旧PRD把它当可读取字段 | C1定义digest派生SourceRevision，不伪造现API |
| F03 P0 | [memory.go:133](E:/Trae-Work-Projects/lunitide/internal/m8app/memory.go:133)默认scope policy允许；bootstrap仅设置FTS；[data_scope.go:131](E:/Trae-Work-Projects/lunitide/internal/app/data_scope.go:131)未覆盖新factId等 | 注入真实policy/evidence；完整scope键与ID反查，X1 |
| F04 P0 | [chat.go:416](E:/Trae-Work-Projects/lunitide/internal/app/chat.go:416)、[assemble_envelope.go:510](E:/Trae-Work-Projects/lunitide/internal/contextapp/assemble_envelope.go:510)存在偏好/working指令通路；事实authority不等于系统权限 | C2自由文本低信任；Core只映射白名单，测试最终messages |
| F05 P0 | [chat.go:598](E:/Trae-Work-Projects/lunitide/internal/app/chat.go:598)/:636与归档另有summary注入；只清memory不能保证整个聊天上下文“忘掉” | C1明确记忆库forget边界、source suppression防重新捕获；不谎称删除聊天/摘要 |
| F06 P0 | 旧PRD迁移前要求v2 export，计划导出在Task7；[m10_memory_ops.go:402](E:/Trae-Work-Projects/lunitide/internal/storage/sqlite/m10_memory_ops.go:402)多查询非同read tx | X0使用[CreateBackup](E:/Trae-Work-Projects/lunitide/internal/storage/sqlite/backup.go:20)；迁移后snapshot archive |
| F07 P0 | archive仅SHA校验却命名imported_signed，可错误升权/复活忘记项 | imported_unverified→review；目标tombstone优先；C1/M-R07 |
| F08 P1 | 旧PRD252终态后、446终态前；301不冲突、290明确纠正；304观察可直接注入、计划审阅前禁止 | C1唯一时序与自动纠正规则；observation审阅前0注入 |
| F09 P1 | 旧计划129队列10000停止claim，消费者停了无法排空 | 停物化、保留cursor、继续消费；M-R05 |
| F10 P1 | get无历史参数，this_version无版本，undo不考虑后续修改；current overlay可能覆盖历史 | get version/asOf、history、targetVersion；undo原子冲突；历史独立查询；C1 |
| F11 P1 | embedding只model_id不足识别向量空间；只算注入/批次不能保证总费用 | embedding_space_id；C2每日预占/重试计量和全链成本 |
| F12 P0 | [OCR route.go:39](E:/Trae-Work-Projects/lunitide/internal/ocrapp/route.go:39)全局FileStore；[ocr_wire.go:38](E:/Trae-Work-Projects/lunitide/internal/app/ocr_wire.go:38)执行时重读routing，与主体策略矛盾 | C3 ScopedRoutingStore+ResolvedOCRRequest；身份切换不能串用凭据 |
| F13 P1 | Office直接[RecognizePDF:61](E:/Trae-Work-Projects/lunitide/internal/app/office_source_text.go:61)，缓存仅SHA+OCR revision；模型目录直接RecognizeImage | C3全部旧入口适配，缓存含主体/scope/pack/gate/pipeline；X2 |
| F14 P1 | [CAS:84](E:/Trae-Work-Projects/lunitide/internal/workspace/cas.go:84)仅整文件Get，无refcount/lease/GC；按OCR引用删全局CAS会伤其他域 | C4独立OCR root+adapter/ref/lease，禁止全局局部GC |
| F15 P1 | plan要求lease恢复/卸载，旧PRD表无lease/fence；pack ready与operation终态混用 | C4持久operation/version/artifact leases，旧fence拒绝，更新失败旧current仍ready |
| F16 P1 | [pdf_render_windows.go:110](E:/Trae-Work-Projects/lunitide/internal/doctext/pdf_render_windows.go:110)先读取页bytes、累计整批；worker限额保护不到Engine | C3逐页文件引用、分配前限额，X2 |
| F17 P1 | [IPC frame:10](E:/Trae-Work-Projects/lunitide/internal/ipc/frame.go:10)4MiB，旧run.get按页数限制但单页可8MiB | C4单响应512KiB/metadata only，artifact原始byte块≤65536 |
| F18 P0 | player.report无asset/epoch却要求识别当前轨道；旧ended可能推进新歌 | C5 assetId+playbackEpoch+eventSeq，ended一次幂等，A-R02 |
| F19 P1 | 当前 [App.tsx:108](E:/Trae-Work-Projects/lunitide/web/src/App.tsx:108) 的 `SessionPage` 随 project/session key 变更而卸载；若把 player 挂在 SessionPage 会中断跨页播放 | C5 App 当前三条 early return 外共同持久化 Media runtime，刷新与页面导航区分 |
| F20 P1 | [ttsPlayer.ts](E:/Trae-Work-Projects/lunitide/web/src/session/companion/ttsPlayer.ts)、speech已有独立音频；旧媒体计划未协调 | C5音频焦点，暂停核验再录音，恢复受token保护；V-R01～04 |
| F21 P1 | [operation.list schema](E:/Trae-Work-Projects/lunitide/api/bridge/v1/operation.list.schema.json)无scope/cursor/time；session快照不能恢复全部media历史 | C6新增activity.list聚合；C5 media.operation.get/list与root去重；X3 |
| F22 P1 | [media_session.go:3](E:/Trae-Work-Projects/lunitide/internal/winexec/media_session.go:3)无capability/timeline/sessionKey；Host Request无window身份 | C5显式扩展SMTC能力；Host代理player私有RPC，不能信Renderer owner |
| F23 P0 | [media_foreground.go:397](E:/Trae-Work-Projects/lunitide/internal/toolruntime/media_foreground.go:397)/:419仅发送媒体键仍passed=true | 05 Task0先修两条未核验fallback；A-R01；不是本次已修 |
| F24 P2 | UI仅抽象“更美观”、无音频/任务效率/总成本实测，5分容易自评 | C6主题/布局/焦点规范；07可用性/30例任务/语音计时与满分门 |

本表不声称是全仓库安全审计。准确范围为本次功能及其直接依赖；同一根因跨文档多处出现按一项计。

## 4. 保留的合理设计

- 不引入第三个记忆服务；本地canonical真相、版本化和按需召回符合现码。
- Windows OCR继续快速/灾备，Paddle可选完整pack，安装与自动路由分门。
- 明确Job Object不是网络/文件沙箱，不虚构离线安全边界。
- 媒体owned/external分型，权限与内容许可边界保留。
- 异步任务、幂等、CAS、snapshot恢复、未知状态不假成功，方向正确。

## 5. R2当时提出的整改方向（最终版本见10与11）

不能只按本页 24 项直接实施。先执行 11 的 X0 合同冻结和基线记录，再按 03–06 落地，并提交三组证据：①干净 Windows 完整模型包与真实媒体垂直链；②数据隔离/遗忘/备份恢复/崩溃 fencing 演练；③固定数据集质量、总 Token、语音/办公/代码任务及目标用户 UI 验收。满足 07 全部门且 12 的 P0/P1 行均为 `VERIFIED` 才标 5 分。

不以压缩排期、增加框架数量或追求“世界第一”作为加分项。更值得优先投入的是可追溯、不会误记/越权、失败能恢复、不会打断用户、结果可测量。
