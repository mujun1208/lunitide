# Media Session and Activity UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让本地音视频可在产品内持续播放，并准确展示自动工具的执行、核验、失败与恢复。
**Architecture:** Engine 的 SQLite MediaSession/Operation 为状态真相，Host 登记授权文件并提供 WebView2 Range 资源。UI 渲染统一 snapshot；外部播放器经原 ToolRuntime/SMTC 控制，自有播放器通过领取指令与报告真实 DOM 事件更新状态。
**Tech Stack:** Go、SQLite、Windows SMTC/WebView2、现有 ToolRuntime/Bridge/IPC、React/TypeScript/Vitest。
**Spec:** [完整 PRD](02-upgrade-prd.md)，TOOL-001～004、MEDIA-001～010、UI-001～004。
**Revision:** 2026-09-15。以下任务是实施要求和预期测试，不代表产品已具备新播放器。

**R2执行补充：** 必须同读[跨模块合同](06-integration-contracts-plan.md)C1–C6及X0–X3、[新增回归和五分门](07-acceptance-and-scorecard.md)。下文任务由这些合同补齐生产接线，不允许只做mock骨架。

## Global Constraints

- 保留 media.play 的命名歌曲 query、用户明确 URL、browser/foreground/app 参数兼容；这些走 origin=external，不导入 owned queue、不抓取媒体。
- 保留 ToolRuntime 的审批、capability、桌面串行锁、hook、审计与急停。新增 Bridge 入口调用共用治理 dispatcher，不直接绕到 winexec。
- global key、UIA、打开 URL/进程至多 command_dispatched；只有匹配目标的 SMTC 或受本产品控制的 audio/video 实际事件才 verified。
- owned 仅用户选择、授权 workspace 或已登记 artifact；媒体许可不随 MIT 播放器代码授权自动获得。
- 0164_media_sessions.sql，默认 media_session_v2/activity_center_v2=off；SQLite 为元数据真相，无 UI localStorage 真相。
- 除合同声明的lease/report/claim内部幂等协议外，所有业务mutation 顶层 envelope idempotencyKey，payload operationId/expectedRevision；key+subject+method 唯一，重放变参 OPERATION_REPLAY_MISMATCH。
- 同一会话的 revision 单调；command 快速返回 accepted receipt，最终状态从 get/watch 得到。事件重放不执行播放命令。
- local file 通过 opaque assetId/ticket，renderer/log/聊天不出现绝对路径；不使用附件 Base64 chunk 传电影。
- 支持格式必须由 WebView2 实机 canPlayType/loadedmetadata/error 验证。无需预先承诺全格式、全编码。
- 未实现的新性能指标只能写验收预算，不写“已提升 X%”。

## 提交和 Bridge 生成

每个任务先写行为红测、跑命令确认失败、实现、同命令绿测、检查 diff、只提交任务文件。所有 PowerShell 原生命令后立即检查 $LASTEXITCODE；不把多个命令用分号串起来只看最终退出码。

新增公开 schema 后同步 envelope.schema.json method enum、web/scripts/generate-bridge.mjs enabled assertion、client mutation sets、Engine/Host route metadata。执行：

~~~powershell
npm --prefix web run generate:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge generation failed" }
npm --prefix web run verify:bridge
if ($LASTEXITCODE -ne 0) { throw "Bridge verification failed" }
~~~

三个生成物 internal/bridge/schema_generated.go、internal/contract/schema_generated_test.go、web/src/generated/bridge.ts 不手改。internal.media.* 私有 RPC 不进入公开 renderer envelope。

## 文件地图

| Task | 文件 | 职责 |
|---|---|---|
| 0 | internal/toolruntime/media_foreground.go、media_foreground_test.go | 立即修正 false success |
| 1 | migrations/0164_media_sessions.sql；internal/storage/sqlite/store.go、media_sessions.go、media_sessions_test.go；internal/domain/media/session.go | assets/session/operation/queue/settings/state |
| 2 | internal/mediaapp/service.go、service_test.go；internal/toolruntime/media.go、runtime.go；internal/winexec/media_session_windows.go、对应 stub | governed dispatch、外部兼容和核验 |
| 3 | internal/desktopmedia/handler_windows.go、handler_other.go、handler_test.go；internal/app/media_private_handlers.go；cmd/desktop/main.go；internal/hostbridge/gateway.go | 原生选择、私有登记/resolve、Host 接线 |
| 4 | internal/webviewhost/media_resource_windows.go、media_resource_other.go、media_resource_test.go；web/index.html | ticket/Range/流式 IStream/CSP |
| 5 | internal/app/media_handlers.go、media_handlers_test.go、handlers_registry.go；internal/bootstrap/wire.go；internal/bridge/protocol.go；internal/engineclient 对应流路由；api/bridge/v1/media.*.schema.json | 请求/真实 watch stream/生产装配 |
| 6 | web/src/media/mediaSnapshot.ts、OwnedMediaPlayer.tsx、MediaTray.tsx、MediaQueueDrawer.tsx、NowPlayingBadge.tsx 及测试 | 唯一 snapshot、实际音视频元素、指令消费 |
| 7 | web/src/media/MediaOperationCard.tsx；web/src/activity/activitySnapshot.ts、ActivityCenter.tsx 及测试；web/src/session/SessionPage.tsx、ToolTrajectory.tsx、liveChat.ts；web/src/settings/ComputerPanel.tsx | 工具回执、活动中心、已有权限 UI |
| 8 | scripts/test-media-session.ps1；docs/design/jiyishengji/runbooks/media-session-runbook.md；docs/design/jiyishengji/evidence/media-session.md | 并发、恢复、安全、实机播放与发布证据 |

## Task 0：修复只发送媒体键即报成功（MEDIA-002/TOOL-001）

- [ ] **1. 红测。** 在 media_foreground_test.go 加 TestMediaKeyWithoutReadbackIsUncertain：stub 发送键成功、无 SMTC，断言 result.passed=false、uncertain=true、verification=command_dispatched，中文不得称“已播放”。
- [ ] **2. 执行。** `go test -count=1 ./internal/toolruntime -run TestMediaKeyWithoutReadbackIsUncertain`，先确认业务断言 FAIL。
- [ ] **3. 实现。** 修改 genericPlaybackStarted fallback，不把 SendMediaKey 无错误等同播放成功；存已发送证据并返回 MEDIA_UNVERIFIED。
- [ ] **4. 绿测并回归。** `go test -count=1 ./internal/toolruntime ./internal/winexec`。
- [ ] **5. 提交。** `fix(media): keep key-only playback unverified`；此真实性修复不依赖 0164，之后关闭 media flag 也不能回滚此修复。

## Task 1：持久状态、队列、设置与幂等（MEDIA-001/006/007）

- [ ] **1. 红测。** media_sessions_test：空库/0163升级、再次打开、错误 checksum、cross-scope 读取、同 key 变参、双 next 仅一次、队列 CAS 冲突。
- [ ] **2. 执行。** `go test -count=1 ./internal/storage/sqlite -run TestMediaSession`。
- [ ] **3. 建表。**

| 表 | 字段/约束 |
|---|---|
| media_assets | asset_id PK；subject/scope；source_kind=user_selected/artifact/workspace；private source_ref；file identity/digest、MIME/kind/title/size、state/revision |
| media_sessions | id/subject/scope/origin、phase/verification、asset_id/playback_epoch/auto_advance、position/duration/volume/muted、external app/session key、queue_revision/revision |
| media_operations | id/session/parent、action、request_digest/idempotency_key、phase/verification、error/evidence、accepted/dispatched/completed时间 |
| media_queue_items | id/session/asset_id/order_index/state；UNIQUE(session,order_index) |
| media_bookmarks | subject/asset_id、position/duration/updated；不作为长期偏好 |
| media_settings | local singleton、media_session_v2/activity_center_v2、auto_advance 默认true、integer revision |
| media_player_leases | session/window identity/nonce digest/expiry、generation |
| media_audio_focus | singleton_id/owner_subject_id/media_session_id/playback_epoch/focus_token_digest/reason/revision/updated_at；全应用single-audible CAS |
| media_player_commands | operation_id PK、session/lease generation、asset_id/playback_epoch、absolute desired state、claimed_at/acknowledged_at/state |

revision≥1、volume 0..100、position/duration 非负毫秒、size 非负、JSON≤64KiB且合法；公开列表 limit≤100。cue/list title≤512 bytes，歌词不进 session row。File source ref 只在 Engine DB/Host private IPC 使用。对应 migration 更新 store.go manifest、expectedSchemaSQL、列清单。
- [ ] **4. 内部接口。** session snapshot/domain 为唯一类型；schema DTO 从它做明确映射。每个 mutating transaction 顺序：authorize→key/digest→CAS→persist intent→commit；外部动作在 commit 之后，不能在 DB lock 中发送按键。
- [ ] **5. 绿测/提交。** `feat(media): persist authorized sessions queues and operations`。

## Task 2：状态机、治理和 external 兼容（TOOL-001～004/MEDIA-001/005/009）

- [ ] **1. 红测。** service_test：审批拒绝不 dispatch、急停后不新派发、SMTC 目标不匹配 uncertain、外部无 seek capability 禁用；现有 query/URL/browser/foreground/app fixtures 保持可调用。
- [ ] **2. 执行。** `go test -count=1 ./internal/mediaapp ./internal/toolruntime ./internal/winexec`。
- [ ] **3. 状态。** 操作与播放状态分开，操作 phase=requested/awaiting_approval/dispatching/verifying/succeeded/uncertain/failed/cancelled；session phase=idle/playing/paused/stalled/ended/uncertain/failed/stopped。receipt 不把 accepted 写 succeeded。

~~~text
persist intent → governance check/approval → dispatch
→ matching evidence → succeeded + verified state
→ command sent but evidence absent → uncertain
→ explicit error → failed
~~~

- [ ] **4. External。** 保留 buildMediaSearchURL/openHTTPURL 既有治理路径，query 或 URL 解析为 external operation；不可伪造 owned asset。SMTC 按明确 app/session identity、请求歌曲目标和后续 playback status 匹配；只要歧义存在就 uncertain。previous/next/seek/volume 控件仅按当前 provider capability 开放，external queue 只读。
- [ ] **5. 恢复。** 重启时已 dispatch 但未核验的操作设 uncertain 并重新读证据，绝不自动重发 toggle/next。重试由用户发起新 operationId 并关联 parent；取消仅保证未 dispatch 的动作停止，已发送外部按键不宣称撤回。
- [ ] **6. 绿测/提交。** `feat(media): govern media commands and preserve external playback compatibility`。

## Task 3：Host 文件选择与 Engine 私有登记（MEDIA-003/010/UI-004）

- [ ] **1. 红测。** handler_test/private handler test：选择后只返回 assetId/MIME/title/kind/size；renderer 调 internal 方法拒绝；UNC/reparse/ADS/目录拒绝；文件 identity 变化使 asset 失效；4GiB sparse file 只登记不全读。
- [ ] **2. 执行。** `go test -count=1 ./internal/desktopmedia ./internal/app ./internal/hostbridge -run 'TestMediaAsset|TestPrivateMedia'`。
- [ ] **3. Pick。** 新 host-owned media.asset.pick {multiple?}，系统原生音视频文件框最多 20 文件；不扫描磁盘、不复用 100MiB 附件限制。Host 打开普通文件 handle，检查最终路径每个授权边界与 reparse，记录 volume/file ID、size/mtime；本地用户选取即 path 授权。
- [ ] **4. 私有合同。** 沿用 authenticated named-pipe call 模式，新 internal.media.asset.register 只由 Host 携带私有 source path/fingerprint 与 scope context；Engine 用当前身份/授权 scope 登记，返回公开 asset metadata。internal.media.asset.resolve 只给 Host 返回 ticket 对应 source ref/expected identity。这两个名称不进公共 schema enum，gateway 按来源拒绝从 WebMessage 转发。
- [ ] **5. Artifact/workspace。** Agent 只可提供已授权 artifact/workspace ref；Engine 复核现有 grant，登记同一 media_assets。本地选择 source_ref 不因 ID 可猜而免检，resolve 每次再验证身份与授权。取消文件框返回 canceled=true，无 session/queue 副作用。
- [ ] **6. Wiring/提交。** cmd/desktop/main.go 装配 picker/private RPC client；Engine bootstrap 注册 service/private handler。绿测后 `feat(media): register local assets through the desktop host`。

## Task 4：opaque ticket 与 WebView2 Range 读取（MEDIA-010/UI-004）

- [ ] **1. 红测。** media_resource_test：GET/HEAD、bytes=0-9、suffix、open-ended、416、multi-range拒绝；无 ticket/过期/其他 scope/改文件/导航外站拒绝；大文件读取内存有界。
- [ ] **2. 执行。** `go test -count=1 ./internal/webviewhost ./internal/mediaapp -run 'TestMediaRange|TestMediaTicket'`。
- [ ] **3. Ticket。** media.asset.open {assetId,mediaSessionId} 检查 owned session，返回随机128-bit base64url ticket URL；初次60s，成功请求续30min空闲期，硬上限12h。Engine 内存保存绑定，stop/logout/session删除/退出撤销；应用重启 URL 全失效，UI 重新 open。ticket/source path 不进 logs/DB/聊天。
- [ ] **4. Resource broker。** 固定 origin https://media.lunitide.local/v1/assets/<ticket>，WebView2 AddWebResourceRequestedFilter 注册所有该 origin 请求；在 handler 中检查顶层页面是应用 origin、请求 context=media，再私有 resolve，重新打开并比较 file handle identity。失败直接返回拒绝响应，绝不回落真实 DNS/network。
- [ ] **5. Range。** CreateWebResourceResponse 返回文件支持的 IStream；实现受区间限制的 seekable COM IStream adapter，而不是 os.ReadFile/CreateMemoryStream 全量。每次 Read≤256KiB，每个媒体最多4个并发请求；HTTP支撑 GET/HEAD/单 byte Range，header 为 Content-Type、Content-Length、Accept-Ranges、Content-Range、nosniff/no-store。文件 handle 存活到 COM Release/请求终止，防止提前 close。WebView2 UI thread 使用 deferral，私有 IPC/file打开在 worker 执行，回 UI thread 完成 response，测试取消/navigation 时资源释放。

~~~text
GET full → 200
HEAD → 200/headers only
valid single Range → 206 + Content-Range
unsatisfiable or multiple Range → 416 + bytes */size
invalid/expired ticket → 403
asset missing or changed → 410
~~~

- [ ] **6. CSP。** 只增加 media-src 的 https://media.lunitide.local；script/connect/frame policy 保持原限制。该 origin 不接受 query/path参数、cookies、remote redirect；不暴露 file://。
- [ ] **7. Windows 实机/提交。** 真实 WebView2 音频和4GiB sparse/合法视频 seek；无全量内存增长，stop释放 handle。提交 `feat(media): stream authorized local media with bounded byte ranges`。

## Task 5：公开 Bridge、watch 与播放器指令通道（MEDIA-004/006/TOOL-003）

- [ ] **1. 红测。** media_handlers_test：首次 create、queue动作字段 fail closed、watch gap重连、多个窗口只有一个player lease、旧generation report拒绝、重复 directive不重复next。
- [ ] **2. 执行。** `go test -count=1 ./internal/app ./internal/contract ./internal/mediaapp -run 'TestMediaBridge|TestMediaPlayer|TestMediaWatch'`。
- [ ] **3. 请求合同。** 所有 method schema additionalProperties=false，严格 ULID/整数/枚举；idempotencyKey 仅顶层。

| 方法 | payload → result |
|---|---|
| media.session.create | assetId,queueAssetIds?,scopeKind,scopeId,operationId → owned idle snapshot |
| media.session.get/list | mediaSessionId 或 scopeKind,scopeId,cursor?,limit? → snapshots |
| media.operation.get/list | operationId 或 scopeKind,scopeId,cursor?,limit? → 操作历史/parent/rootOperationId |
| activity.list | scopeKind,scopeId,domains?,cursor?,limit? → items,nextCursor,snapshotAt,hasMore |
| media.session.command | mediaSessionId,action,positionMs?,volume?,expectedRevision,operationId → receipt |
| media.queue.command | mediaSessionId,action,itemId?,beforeItemId?,expectedQueueRevision,operationId → queue/snapshot |
| media.asset.pick | multiple?（host）→ canceled,assets |
| media.asset.list | scopeKind,scopeId,mediaSessionId?,origin?,cursor?,limit? → authorized assets |
| media.asset.open | assetId,mediaSessionId → playbackUrl,expiresAt |
| media.session.watch | scopeKind,scopeId → streamId，再推 typed media event |
| media.player.attach | mediaSessionId,operationId → leaseToken,generation,expiresAt |
| media.player.next | mediaSessionId,leaseToken,generation → one pending absolute directive or null |
| media.player.report | mediaSessionId,leaseToken,generation,assetId,playbackEpoch,operationId?,eventSeq,event,positionMs?,durationMs?,errorCode? → receipt；命令回执operationId必填 |

session ID wire 字段统一使用 mediaSessionId。action=play/pause/toggle/stop/previous/next/seek/set_volume/mute/unmute。seek仅position；volume仅set_volume。queue action=move/remove/clear/jump，move beforeItemId=null表示末尾；外部队列拒绝。player.attach 的同 owner 调用续租；owner ID 必须来自可信 Host，不接受 Renderer 任意指定。

- [ ] **4. Watch。** media event 是 bridge.Event payload，必须扩展 protocol.go/IPC encode/decode/client stream router；不建 x-method=media.event。每次只推已提交 snapshot revision invalidation/有界patch；sequence gap/重连调用 get/list，不执行 command。
- [ ] **5. Owned 指令闭环。** command 通过治理后写 media_player_commands 的“绝对目标状态”，例如 next 在 Engine CAS 中选定下一 asset，而非给 UI 重放 next。单个 Host window 获30s lease，每10s续约；attach/next/report由x-owner=host代理私有RPC附加windowInstanceId/navigationEpoch，不能让renderer自选（06-C5）。next 领取后不删除，直到对应 report ack；UI按operationId去重，reconnect只恢复 asset/position到paused，不未经用户意图自动出声。lease争夺/expiry使未完成动作 uncertain。player.report 只允许持有者、匹配generation、assetId/playbackEpoch、单调eventSeq和当前directive target，不接受任意客户端报告 verified flag。
- [ ] **6. 真实播放报告。** event枚举 playing/pause/ended/stalled/error/position；只有 playing/pause 来自实际元素事件才 verified，play() promise fulfilled 本身不够。位置每1s/暂停/seek/结束上报，Engine存snapshot。播放未在用户手势下获准返回 MEDIA_AUTOPLAY_BLOCKED，在UI给“点击播放”；不假报成功。
- [ ] **7. 生成/装配/提交。** bootstrap/wire.go + Host/client wiring + schema生成均绿；提交 `feat(media): connect typed sessions watches and owned player receipts`。

## Task 6：播放器、Tray 和队列（MEDIA-004/007/UI-002/003）

- [ ] **1. 红测。** OwnedMediaPlayer.test.tsx：audio/video分别渲染；真实playing才报告；stalled/error；复挂不自动play；重复directive不跳两首；queue conflict先回读。
- [ ] **2. 执行。** `npm --prefix web test -- src/media`。
- [ ] **3. 单一状态。** mediaSnapshot.ts 只有一个 Engine snapshot cache；旧revision忽略、gap回读。媒体元素本地currentTime可作动画，不作为业务真相。OwnedMediaPlayer只有lease owner挂载；source只来自asset.open，处理canPlayType/metadata/error。
- [ ] **4. 组件。** compact Tray 56px，expanded≤40% viewport；窄窗两行；title/artist/进度/播放/暂停/上下一首/音量/验证标签。video展开提供实际画面，audio保持轻条。封面无授权资源用占位；歌词用户本地文件仅安全纯文本，初版不实现在线歌词搜索。
- [ ] **5. Queue。** move/remove/clear/jump走独立queue.command，带queue revision；清空当前项停止并撤销ticket；删除正在播项选择下一可用项且不自动出声，用户按next或自动ended才继续。external显示“队列由外部应用管理”。
- [ ] **6. 自动下一首。** owned ended report匹配当前asset/lease/playbackEpoch后，Engine依原会话autoAdvance设置（默认true）创建一次linked operation和绝对next directive；重复ended按session+epoch幂等。急停/权限关闭/窗口无lease时停止续播，不能无限重试。
- [ ] **7. 绿测/typecheck/提交。** keyboard/焦点/中文aria/reduced motion/200%缩放，`npm --prefix web run typecheck`；提交 `feat(media): add persistent audio video controls and owned queues`。

## Task 7：工具卡片与活动中心（TOOL-001～004/UI-001～004）

- [ ] **1. 红测。** MediaOperationCard.test.tsx/ActivityCenter.test.tsx：uncertain不能绿色成功；OCR+media+tool筛选；刷新恢复；retry创建新operation，不改旧记录；raw HTML标题不能执行。
- [ ] **2. 执行。** `npm --prefix web test -- src/activity src/media/MediaOperationCard.test.tsx src/session/SessionPage.media.test.tsx src/settings/ComputerPanel.test.tsx`。
- [ ] **3. ActivitySnapshot。** 唯一类型和适配器位于 web/src/activity/activitySnapshot.ts：

~~~typescript
type ActivitySnapshot = {
  activityId: string
  domain: 'tool' | 'ocr' | 'media'
  kind: string
  phase: string
  terminal: boolean
  verification?: string
  title: string
  completedUnits?: number
  totalUnits?: number
  errorCode?: string
  retryable: boolean
  recoveryAction: 'none' | 'retry' | 'cancel' | 'open_settings' | 'open_player' | 'select_asset'
  scopeKind: string
  scopeId: string
  createdAt?: string
  updatedAt?: string
  rootOperationId?: string
}
~~~

数据由新增activity.list在后端聚合现有journal/OCR/media_operations，旧operation.list不具备此分页合同。按06-C6的created_at复合cursor、snapshotAt和root去重实现；不增加第四套活动数据库。OCR未启用不阻塞media；首版最近100项并真实加载更多。
- [ ] **4. 卡片。** 目标→等待审批→发送→核验→已确认/无法确认/失败；显示证据来源、elapsed和一个真实recovery。非media仍用ToolTrajectory；App共同根只挂一套player/store/tray，SessionPage仅订阅，离开页不重复派发。
- [ ] **5. 权限。** ComputerPanel直接读取现有配置/急停，不复制toggle。关闭电脑控制后external只读；owned手工/Agent/自动续播按06-C5权限矩阵运行，Agent发起动作仍经过ToolRuntime；不拿桌面锁长期锁住本地音乐。
- [ ] **6. 绿测/提交。** `feat(ui): unify truthful media receipts and recoverable activities`。

Task6/7必须同时完成06-X3的App根MediaProvider、单一owned发声、TTS/ASR音频焦点与后端activity.list；这些不是留给开发者自由选择的后续项。

## Task 8：实机、恢复和交付（所有 MEDIA/TOOL/UI）

- [ ] **1. 故障测试。** 并发同revision仅一个动作；重启时verifying→uncertain；SMTC丢失；无自动重发next；撤销ticket；恶意metadata/filename/URL；私有RPC从renderer拒绝。
- [ ] **2. 资源。** 4GiB sparse stream/合法影片seek、连续播30min、100次切换、WebView导航/退出回收IStream/handle；使用加速时钟验证24h状态寿命，再用真实Windows播放验证不能模拟的codec/COM/SMTC行为。
- [ ] **3. 回退。** 关activity flag隐藏UI不删数据；关media flag停止创建新v2操作、关闭owned player并保留metadata，external回既有路径且保留Task0真实性修复。不保证旧严格schema二进制能直接打开新DB；优先同二进制功能回退。
- [ ] **4. 执行。** 逐条运行 `go test -count=1 ./internal/mediaapp ./internal/toolruntime ./internal/winexec ./internal/desktopmedia ./internal/webviewhost ./internal/storage/sqlite ./internal/app ./internal/contract`；UI media/activity/session/settings测试；typecheck/build；PRD13.4全门。Windows-only测试需真实Windows runner，不用非Windows stub PASS代替。
- [ ] **5. Gate。** false verified/越权读取/路径ticket日志泄漏/重复next副作用均0；Range峰值内存不随文件长度线性增长；刷新/lease重建可恢复且不自动出声；核心流程键盘可操作。当前未安装 axe；本任务执行 `npm --prefix web install --save-dev axe-core`，提交 package.json 与 lockfile 的固定解析版本，在 Vitest DOM 测试中调用 axe.run 并要求 serious/critical 为0；另做真实 WebView2 键盘/读屏验收，不把 jsdom 结果冒充完整可访问性证明。
- [ ] **6. 证据与提交。** traceability记录requirement→test/实机视频→commit→report；runbook说明owned/external能力差异、故障恢复和授权资产范围。提交 `test(media): verify desktop playback recovery and resource boundaries`。

## 执行顺序

Task0可先独立交付；Task1→Task2/3→Task4→Task5→Task6→Task7→Task8。每阶段有对应可运行测试和正式接线，不能仅以React页面出现视为播放器完成。路线已确定，后续按清单实施即可。
