# Media Session and Activity UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让本地音视频可在产品内持续播放，并准确展示自动工具的执行、核验、失败与恢复。
**Architecture:** Engine 的 SQLite MediaSession/Operation 为状态真相，Host 登记授权文件并提供 WebView2 Range 资源。UI 渲染统一 snapshot；外部播放器经原 ToolRuntime/SMTC 控制，自有播放器通过领取指令与报告真实 DOM 事件更新状态。
**Tech Stack:** Go、SQLite、Windows SMTC/WebView2、现有 ToolRuntime/Bridge/IPC、React/TypeScript/Vitest。
**Spec:** [完整 PRD](02-upgrade-prd.md)，TOOL-001～004、MEDIA-001～010、UI-001～004。
**Revision:** 2026-09-15。以下任务是实施要求和预期测试，不代表产品已具备新播放器。

**R3执行补充：** 必须同读[完整 PRD](02-upgrade-prd.md)、[跨模块合同](06-integration-contracts-plan.md)C1–C6及X0–X3、[新增回归和五分门](07-acceptance-and-scorecard.md)。下文只在 02/06 约束内细化媒体实现，不能覆盖二者；发现冲突时停止该项并同步修正文档与验收后再编码，不允许只做 mock 骨架。

## Global Constraints

- 保留 media.play 的命名歌曲 query、用户明确 URL、browser/foreground/app 参数兼容；这些走 origin=external，不导入 owned queue、不抓取媒体。
- 保留 ToolRuntime 的审批、capability、桌面串行锁、hook、审计与急停。新增 Bridge 入口调用共用治理 dispatcher，不直接绕到 winexec。
- global key、UIA、打开 URL/进程至多 command_dispatched；只有匹配目标的 SMTC 或受本产品控制的 audio/video 实际事件才 verified。
- owned 仅用户选择、授权 workspace 或已登记 artifact；媒体许可不随 MIT 播放器代码授权自动获得。
- Media logical migration `media_sessions`（审计快照候选文件名 `0164_media_sessions.sql`，最终编号只取 P00 的 `evidence/migration-allocation.json`），默认 media_session_v2/activity_center_v2=off；它必须排在 allocation 中 OCR logical migration 之后。SQLite 为元数据真相，无 UI localStorage 真相。
- 除合同声明的 lease/report/claim 内部协议外，所有业务 mutation 都在顶层 envelope 携带 `idempotencyKey`；只有创建用户可见 operation 的方法才带 `operationId`，只有修改既有 CAS 聚合的方法才带该聚合的 `expectedRevision/expectedQueueRevision`。读取、Host picker 和 Host-private player RPC 不虚构这些字段；各方法以 Task 5 的 exact 表为准。key+subject+method 唯一，重放变参返回 `OPERATION_REPLAY_MISMATCH`。
- 同一会话的 revision 单调；command 快速返回 accepted receipt，最终状态从 get/watch 得到。事件重放不执行播放命令。
- local file 通过 opaque assetId/ticket，renderer/log/聊天不出现绝对路径；不使用附件 Base64 chunk 传电影。
- 支持格式必须由 WebView2 实机 canPlayType/loadedmetadata/error 验证。无需预先承诺全格式、全编码。
- 未实现的新性能指标只能写验收预算，不写“已提升 X%”。
- **V2 信息架构。** 媒体作为一个独立“媒体中心”呈现，中心内仅分“音乐”和“视频”两个一级视图；完整播放器、媒体库/选择入口和队列留在中心，不把播放能力拆成多个侧栏菜单。
- Media Center 内展示完整播放界面时不重复显示 `MediaMiniPlayer`。用户离开媒体中心后，只有 session phase=playing/paused 的 owned/external 会话才在主内容区右下角出现；idle/stalled/stopped/ended/uncertain/failed 或尚未创建会话时隐藏，唯独正在 close 或 close 失败按 Task 6 的 `closing/close_error` 优先级保留。
- `MediaMiniPlayer` 的关闭按钮是明确的 stop/结束会话命令，不是只隐藏 DOM；最终 snapshot 到达 stopped/ended 后隐藏。命令失败或 external 无法核验时保留可见状态并显示“未确认”，不得营造已经停止的假象。
- ActivityCenter 不占左侧常驻导航。应用顶部统一状态入口呈现等待审批、运行中、失败和需处理状态，点击打开活动抽屉；普通成功静默沉淀到历史。
- UI 沿用 Lunitide 的纯黑基底、月白文字与青绿—蓝紫品牌色；品牌渐变集中用于媒体中心主舞台、封面氛围和播放进度，列表/抽屉保持低饱和，避免整页蓝灰卡片化和高信息密度。

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
| 1 | P00 allocation 为 logical `media_sessions` 指定的物理 migration 文件（审计候选 `0164_media_sessions.sql`）；internal/storage/sqlite/store.go、media_sessions.go、media_sessions_test.go；internal/domain/media/session.go | assets/session/operation/queue/settings/phase |
| 2 | internal/mediaapp/service.go、service_test.go；internal/toolruntime/media.go、runtime.go；internal/winexec/media_session_windows.go、对应 stub | governed dispatch、外部兼容和核验 |
| 3 | internal/desktopmedia/handler_windows.go、handler_other.go、handler_test.go；internal/app/media_private_handlers.go；cmd/desktop/main.go；internal/hostbridge/gateway.go | 原生选择、私有登记/resolve、Host 接线 |
| 4 | internal/webviewhost/media_resource_windows.go、media_resource_other.go、media_resource_test.go；web/index.html | ticket/Range/流式 IStream/CSP |
| 5 | internal/app/media_handlers.go、media_handlers_test.go、handlers_registry.go；internal/bootstrap/wire.go；internal/bridge/protocol.go；internal/engineclient 对应流路由；api/bridge/v1/media.*.schema.json | 请求/真实 watch stream/生产装配 |
| 6 | web/src/media/mediaSnapshot.ts、OwnedMediaPlayer.tsx、MediaCenterPage.tsx、MusicPlayerSurface.tsx、VideoPlayerSurface.tsx、MediaMiniPlayer.tsx、MediaQueueDrawer.tsx 及测试 | 唯一 snapshot、独立音乐/视频中心、条件式离页迷你播放器、实际音视频元素 |
| 7 | web/src/media/MediaOperationCard.tsx；web/src/activity/activitySnapshot.ts、ActivityStatusButton.tsx、ActivityCenter.tsx 及测试；web/src/app/appTypes.ts、navStore.ts、LaunchSidebar.tsx、LaunchSidebar.test.tsx；web/src/App.tsx、App.test.tsx、styles.css、web/src/session/SessionPage.tsx、ToolTrajectory.tsx、liveChat.ts；web/src/settings/ComputerPanel.tsx | 工具回执、顶部活动入口/抽屉、唯一 `media` Page/办公组入口、App 根播放器、AgentHub 避让、已有权限 UI |
| 8 | scripts/test-media-session.ps1；docs/design/jiyishengji/runbooks/media-session-runbook.md；`artifacts/media-session/<runID>/run-manifest.json` 及其日志/截图/录屏 | 并发、恢复、安全与实机播放的 P14 证据输入；本任务不创建 release evidence JSON |

## Task 0：修复只发送媒体键即报成功（MEDIA-002/TOOL-001）

- [ ] **1. 红测。** 在 media_foreground_test.go 加 TestMediaKeyWithoutReadbackIsUncertain：stub 发送键成功、无 SMTC，兼容 result 仍断言 passed=false/uncertain=true；新增 operation receipt 断言 phase=uncertain、verificationStatus=unconfirmed、verificationSource=none，中文不得称“已播放”。
- [ ] **2. 执行。** `go test -count=1 ./internal/toolruntime -run TestMediaKeyWithoutReadbackIsUncertain`，先确认业务断言 FAIL。
- [ ] **3. 实现。** 修改 genericPlaybackStarted fallback，不把 SendMediaKey 无错误等同播放成功；存已发送证据并返回 MEDIA_UNVERIFIED。
- [ ] **4. 绿测并回归。** `go test -count=1 ./internal/toolruntime ./internal/winexec`。
- [ ] **5. 提交。** `fix(media): keep key-only playback unverified`；此真实性修复不依赖 logical `media_sessions` migration，之后关闭 media flag 也不能回滚此修复。

## Task 1：持久状态、队列、设置与幂等（MEDIA-001/006/007）

- [ ] **1. 红测。** media_sessions_test：空库/从 allocation 中紧邻其前的 OCR migration 升级、再次打开、错误 checksum、cross-scope 读取、同 key 变参、双 next 仅一次、队列 CAS 冲突。
- [ ] **2. 执行。** `go test -count=1 ./internal/storage/sqlite -run TestMediaSession`。
- [ ] **3. 建表。**

| 表 | 字段/约束 |
|---|---|
| media_assets | asset_id PK；subject/scope；source_kind=user_selected/artifact/workspace；private source_ref；file identity/digest、MIME/kind/title/size、state/revision |
| media_sessions | id/subject/scope/origin、phase、verification_status（播放专用闭集）、verification_source、asset_id/playback_epoch/auto_advance、position/duration/volume/muted、external app/session key、queue_revision/revision |
| media_operations | id/session/parent、action、request_digest/idempotency_key、phase、verification_status（通用闭集）、verification_source、error/evidence、accepted/dispatched/completed时间 |
| media_queue_items | id/session/asset_id/order_index/state；UNIQUE(session,order_index) |
| media_bookmarks | subject/asset_id、position/duration/updated；不作为长期偏好 |
| media_settings | local singleton、media_session_v2/activity_center_v2、auto_advance 默认true、integer revision |
| media_player_leases | session_id/window_instance_id/navigation_epoch/lease_token_digest/lease_expires_at/generation；只持久化 token 的 SHA-256 digest，不持久化明文 leaseToken |
| media_audio_focus | singleton_id/owner_subject_id/media_session_id/playback_epoch/focus_token_digest/reason/revision/updated_at；全应用single-audible CAS |
| media_player_commands | operation_id PK、session/lease generation、asset_id/playback_epoch、absolute desired state、claimed_at/acknowledged_at/state |

revision≥1、volume 0..100、position/duration 非负毫秒、size 非负、JSON≤64KiB且合法；公开列表 limit≤100。cue/list title≤512 bytes，歌词不进 session row。File source ref 只在 Engine DB/Host private IPC 使用。对应 migration 更新 store.go manifest、expectedSchemaSQL、列清单。
- [ ] **4. 内部接口。** session snapshot/domain 为唯一类型；schema DTO 从它做明确映射。每个 mutating transaction 顺序：authorize→key/digest→CAS→persist intent→commit；外部动作在 commit 之后，不能在 DB lock 中发送按键。
- [ ] **5. 绿测/提交。** `feat(media): persist authorized sessions queues and operations`。

## Task 2：状态机、治理和 external 兼容（TOOL-001～004/MEDIA-001/005/009）

- [ ] **1. 红测。** service_test：审批拒绝不 dispatch、急停后不新派发、SMTC 目标不匹配 uncertain、外部无 seek capability 禁用；现有 query/URL/browser/foreground/app fixtures 保持可调用。
- [ ] **2. 执行。** `go test -count=1 ./internal/mediaapp ./internal/toolruntime ./internal/winexec`。
- [ ] **3. 状态。** 操作与播放状态分开：operation phase=requested/awaiting_approval/dispatching/verifying/succeeded/uncertain/failed/cancelled，verificationStatus=not_applicable/not_started/pending/confirmed/unconfirmed，verificationSource=none/process/window/uia/smtc/owned_runtime/artifact；session phase=idle/playing/paused/stalled/ended/uncertain/failed/stopped，verificationStatus=none/command_dispatched/verified_playing/verified_paused/verified_ended/verified_stopped，verificationSource=none/smtc/owned_runtime。receipt 不把 accepted 写 succeeded，也不把 command_dispatched 塞进 verificationSource。

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
- [ ] **3. Pick。** 新 host-owned `media.asset.pick {multiple?,scopeKind,scopeId?}`，scope 使用 user 禁 scopeId、project 必填 scopeId 的 strict oneOf；系统原生音视频文件框最多 20 文件，不扫描磁盘、不复用 100MiB 附件限制。Host 打开普通文件 handle，检查最终路径每个授权边界与 reparse，记录 volume/file ID、size/mtime；本地用户选取即 path 授权，但不等于 scope 授权。
- [ ] **4. 私有合同。** 沿用 authenticated named-pipe call 模式，新 internal.media.asset.register 只由 Host 携带私有 source path/fingerprint、Host window identity 与公开请求中的未信任 scope selector；Engine 从认证连接派生 subject、对 project 再授权并为 user 派生内部 scope_id 后登记，返回公开 asset metadata（user scopeId 为 null）。internal.media.asset.resolve 只给 Host 返回 ticket 对应 source ref/expected identity。这两个名称不进公共 schema enum，gateway 按来源拒绝从 WebMessage 转发。
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
| media.session.create | assetId,queueAssetIds?,scopeKind,scopeId?,operationId → owned idle snapshot；scope strict oneOf |
| media.session.get/list | mediaSessionId 或 scopeKind,scopeId?,cursor?,limit? → snapshots；list 必须显式 scopeKind |
| media.operation.get/list | operationId 或 scopeKind,scopeId?,cursor?,limit? → 操作历史/parent/rootOperationId；list 必须显式 scopeKind |
| activity.list | scopeKind,scopeId?,domains?,cursor?,limit? → items,nextCursor,snapshotAt,hasMore；scope strict oneOf |
| media.session.command | mediaSessionId,action,positionMs?,volume?,expectedRevision,operationId → receipt |
| media.queue.command | mediaSessionId,action,itemId?,beforeItemId?,expectedQueueRevision,operationId → queue/snapshot |
| media.asset.pick | multiple?,scopeKind,scopeId?（host）→ canceled,assets；Host透传selector，Engine鉴权 |
| media.asset.list | scopeKind,scopeId?,mediaSessionId?,sourceKind?,cursor?,limit? → authorized assets；sourceKind=`user_selected|artifact|workspace` |
| media.asset.open | assetId,mediaSessionId → playbackUrl,expiresAt |
| media.session.watch | scopeKind,scopeId → streamId，再推 typed media event |
公开表到 `media.session.watch` 为止。下面三项是 **Host-private RPC**，只注册到 authenticated private Engine pipe，禁止加入 Renderer Bridge schema、公开 method enum 或 `web/src/bridge/client.ts`：

| Host-private RPC | payload → result |
|---|---|
| internal.media.player.attach | mediaSessionId,operationId,windowInstanceId,navigationEpoch → leaseToken,generation,expiresAt |
| internal.media.player.next | mediaSessionId,leaseToken,generation,windowInstanceId,navigationEpoch → one pending absolute directive or null |
| internal.media.player.report | mediaSessionId,leaseToken,generation,windowInstanceId,navigationEpoch,assetId,playbackEpoch,operationId?,eventSeq,event,positionMs?,durationMs?,errorCode? → receipt；命令回执operationId必填 |

session ID wire 字段统一使用 mediaSessionId。action=play/pause/toggle/stop/previous/next/seek/set_volume/mute/unmute。seek仅position；volume仅set_volume。queue action=move/remove/clear/jump，move beforeItemId=null表示末尾；外部队列拒绝。private player.attach 的同 owner 调用续租；owner ID 必须来自可信 Host，不接受 Renderer 任意指定。Host-private 返回的 `leaseToken` 只在 Host 内存持有；SQLite `media_player_leases.lease_token_digest` 保存其 SHA-256，attach/next/report 均以恒定时间比较摘要。不得另建 `nonce_digest` 同义列。

- [ ] **4. Watch。** media event 是 bridge.Event payload，必须扩展 protocol.go/IPC encode/decode/client stream router；不建 x-method=media.event。每次只推已提交 snapshot revision invalidation/有界patch；sequence gap/重连调用 get/list，不执行 command。
- [ ] **5. Owned 指令闭环。** command 通过治理后写 media_player_commands 的“绝对目标状态”，例如 next 在 Engine CAS 中选定下一 asset，而非给 UI 重放 next。单个 Host window 获30s lease，每10s续约；attach/next/report 由 Host 经 authenticated private pipe 直接调用既有 `internalRuntimeHandlers`，并由 Host 附加 `windowInstanceId/navigationEpoch`，不使用公开 `x-owner` schema，也不能让 Renderer 自选（06-C5）。next 领取后不删除，直到对应 report ack；UI按operationId去重，reconnect只恢复 asset/position到paused，不未经用户意图自动出声。lease争夺/expiry使未完成动作 uncertain。player.report 只允许持有者、匹配generation、assetId/playbackEpoch、单调eventSeq和当前directive target，不接受任意客户端报告 verified flag。
- [ ] **6. 真实播放报告。** event枚举 playing/pause/ended/stalled/error/position；只有 playing/pause 来自实际元素事件才 verified，play() promise fulfilled 本身不够。位置每1s/暂停/seek/结束上报，Engine存snapshot。播放未在用户手势下获准返回 MEDIA_AUTOPLAY_BLOCKED，在UI给“点击播放”；不假报成功。
- [ ] **7. 生成/装配/提交。** bootstrap/wire.go + Host/client wiring + schema生成均绿；提交 `feat(media): connect typed sessions watches and owned player receipts`。

## Task 6：独立媒体中心、MediaMiniPlayer 和队列（MEDIA-004/007/UI-002/003）

- [ ] **1. 红测。** `OwnedMediaPlayer/MediaCenterPage/MediaMiniPlayer` 测试覆盖：音乐和视频分别进入对应中心视图；真实 playing 才报告；stalled/error；复挂不自动 play；重复 directive 不跳两首；queue conflict 先回读；媒体中心内 `MediaMiniPlayer` 不渲染；离开中心且 phase=playing/paused 时右下角出现；普通 idle/stalled/stopped/ended/uncertain/failed 时隐藏；点击 close 发送一次 stop，收到确认终态后隐藏；stop uncertain/failed 时不得先行消失。
- [ ] **2. 执行。** `npm --prefix web test -- src/media`。
- [ ] **3. 单一状态。** mediaSnapshot.ts 只有一个 Engine snapshot cache；旧revision忽略、gap回读。媒体元素本地currentTime可作动画，不作为业务真相。OwnedMediaPlayer只有lease owner挂载；source只来自asset.open，处理canPlayType/metadata/error。
- [ ] **4. 媒体中心。** 单一侧栏入口“媒体中心”，页内用“音乐 / 视频”二级切换。音乐视图提供当前曲目、播放/暂停/上下一首、进度、音量、队列与“选择本地媒体”；视频视图以真实画面为主，提供相同核心控制和返回音乐入口。两种视图复用同一 snapshot/queue，不建立各自状态源。无会话时用一块低信息密度空状态引导选择本地文件或从合法 artifact 打开，不陈列不存在的曲库、影片库或在线搜索。
- [ ] **5. MediaMiniPlayer。** 作为 App 根层 portal 固定在**主内容区右下角**，不覆盖左侧导航、输入框、系统窗口控制或活动抽屉；桌面建议宽 320～380px、高不超过 72px，窄窗退化为单行封面/标题/播放/关闭。只显示封面、单行标题、只读紧凑进度、playing/paused 文本+图标、播放/暂停和关闭；队列、音量、seek 和视频画面回到媒体中心，避免把它做成第二个完整播放器。点击主体回媒体中心；点击 close 发送一次绝对 stop，收到确认终态再隐藏。

  `MiniPlayerUIPhase` 是 `miniPlayerPhase` 字段的类型，只由 Engine session snapshot、当前 stop operation 和是否位于媒体中心派生，不持久化：`hidden|active|closing|close_error`。映射固定为：位于媒体中心、无 session，或 session 为 `idle|stalled|stopped|ended|failed|uncertain` 且没有正在处理的 stop operation → `hidden`；离开媒体中心、session 为 `playing|paused` 且无 stop operation → `active`；stop operation 为 `requested|awaiting_approval|dispatching|verifying` → `closing`，显示“正在结束”并禁用重复 close；stop operation 为 `failed|uncertain` 且 session 尚未 `stopped` → `close_error`，保留原卡并显示重试/强制结束入口；stop operation `cancelled` 后若 session 仍 playing/paused 则回 `active`；`succeeded` 必须与 session=`stopped`、ticket/lease 已释放同一已提交 snapshot 出现，否则视为合同错误并保持 `closing`、触发回读。不得另存 `miniPlayerVisible` 或用 CSS 先隐藏。
- [ ] **6. Queue。** move/remove/clear/jump走独立queue.command，带queue revision；清空当前项停止并撤销ticket；删除正在播项选择下一可用项且不自动出声，用户按next或自动ended才继续。external显示“队列由外部应用管理”，`MediaMiniPlayer` 不伪造外部队列。
- [ ] **7. 自动下一首。** owned ended report匹配当前asset/lease/playbackEpoch后，Engine依原会话autoAdvance设置（默认true）创建一次linked operation和绝对next directive；重复ended按session+epoch幂等。急停/权限关闭/窗口无lease时停止续播，不能无限重试。
- [ ] **8. 视觉与响应式。** 媒体中心采用纯黑内容底和大面积留白，月白为主体、青绿—蓝紫只作主舞台氛围/进度/焦点；普通列表不铺渐变。验证 1280/960/680px、200% 缩放和 reduced motion；`MediaMiniPlayer` 与聊天 composer、toast、右侧 drawer 建立确定避让层级，打开活动/队列抽屉时不遮挡主操作。
- [ ] **9. 绿测/typecheck/提交。** keyboard/焦点返回/中文 aria/close 状态/reduced motion/200%缩放，`npm --prefix web run typecheck`；提交 `feat(media): add media center and conditional mini player`。

## Task 7：工具卡片与活动中心（TOOL-001～004/UI-001～004）

本任务是 `web/src/activity/activitySnapshot.ts`、`ActivityStatusButton.tsx`、`ActivityCenter.tsx` 及其测试的唯一创建者。P08/OCR 只交付 `web/src/settings/ocrActivityAdapter.ts`、`ocrActivityAdapter.test.ts` 与 `OCRRecentRuns`，不得创建第二套共享 activity snapshot、顶部按钮或抽屉；本任务把 OCR adapter、tool journal 与 media operation 统一投影为下述 `ActivitySnapshot`。

- [ ] **1. 红测。** `MediaOperationCard/ActivityStatusButton/ActivityCenter` 测试：uncertain不能绿色成功；顶部入口在 running/awaiting_approval/failed/needs_attention 时可见且有文字/图标语义，普通成功不显示未读红点；点击入口打开 OCR+media+tool 统一抽屉；刷新恢复；retry 创建新 operation、不改旧记录；raw HTML 标题不能执行。`LaunchSidebar.test.tsx#TestMediaCenterOfficeNavigation` 与 `App.test.tsx` 另断言 `Page='media'`、办公组常驻入口/active/officeOpen、不受 officeMenu toggle 控制、route 渲染唯一 MediaCenter，跨 AgentHub replaceMainNav/44px header 时 MiniPlayer/Activity 不重挂或遮挡。
- [ ] **2. 执行。** `npm --prefix web test -- src/activity src/media src/app/LaunchSidebar.test.tsx src/App.test.tsx src/session/SessionPage.media.test.tsx src/settings/ComputerPanel.test.tsx`。
- [ ] **3. ActivitySnapshot。** 唯一类型和适配器位于 web/src/activity/activitySnapshot.ts：

~~~typescript
type ActivitySnapshot = {
  activityId: string
  domain: 'tool' | 'ocr' | 'media'
  kind: string
  phase: 'queued' | 'awaiting_approval' | 'running' | 'verifying' | 'succeeded' | 'uncertain' | 'failed' | 'cancelled'
  terminal: boolean
  verificationStatus: 'not_applicable' | 'not_started' | 'pending' | 'confirmed' | 'unconfirmed'
  verificationSource: 'none' | 'process' | 'window' | 'uia' | 'smtc' | 'owned_runtime' | 'artifact'
  title: string
  completedUnits: number | null
  totalUnits: number | null
  errorCode: string | null
  retryable: boolean
  recoveryAction: 'none' | 'retry' | 'cancel' | 'open_settings' | 'open_player' | 'select_asset'
  scopeKind: 'user' | 'project'
  scopeId: string | null
  createdAt: string | null
  updatedAt: string | null
  rootOperationId: string | null
}
~~~

数据由新增activity.list在后端聚合现有journal/OCR/media_operations，旧operation.list不具备此分页合同。request 的 domains 仅为省略或 unique `('tool'|'ocr'|'media')[]` 1..3 项；空数组/未知值拒绝。按06-C6的created_at复合cursor、snapshotAt和root去重实现；不增加第四套活动数据库。所有 DTO 字段 required，缺失旧值用 null；OCR未启用不阻塞media，limit 默认50、最大100并真实加载更多。
- [ ] **4. 顶部状态入口、路由与卡片。** 前端集成人在 `appTypes.ts` 的唯一 Page union 增加 `media`；`LaunchSidebar` 的 officeOpen/current 判定加入 media，并在办公列表放置不受 `officeMenuSettings` 控制的常驻“媒体中心”；`App.tsx` 主 route 接唯一 `MediaCenterPage`。AgentHub 继续使用独立 shell switch/replaceMainNav，不移回办公组。App 顶部 `ActivityStatusButton` 是唯一常驻活动入口，不新增左侧“活动中心”。无活跃/需处理事项时显示中性状态且不抢注意力；等待审批/运行中显示轻量文字或进度，失败/需处理才显示强调点。点击打开 `ActivityCenter`，按“进行中与需处理”优先、历史随后排列；目标→等待审批→发送→核验→已确认/无法确认/失败，显示证据来源、elapsed 和一个真实 recovery。非media仍用ToolTrajectory；App 当前三条 early return 共同包在稳定 Media runtime 下，只挂一套 player/store/`MediaMiniPlayer`，SessionPage仅订阅，离开页不重复派发；MiniPlayer/Activity 与 AgentHub 44px header、替换侧栏和抽屉有明确 inset/z-index/focus 避让。
- [ ] **5. 权限。** ComputerPanel直接读取现有配置/急停，不复制toggle。关闭电脑控制后external只读；owned手工/Agent/自动续播按06-C5权限矩阵运行，Agent发起动作仍经过ToolRuntime；不拿桌面锁长期锁住本地音乐。
- [ ] **6. 绿测/提交。** `feat(ui): unify truthful media receipts and recoverable activities`。

Task6/7必须同时完成06-X3的App根MediaProvider、单一owned发声、TTS/ASR音频焦点与后端activity.list；`MediaCenterPage`、条件式 `MediaMiniPlayer`、顶部 `ActivityStatusButton/ActivityCenter` 都消费同一份 snapshot，不得各自维护真假不一的本地状态。这些不是留给开发者自由选择的后续项。

## Task 8：实机、恢复和交付（所有 MEDIA/TOOL/UI）

- [ ] **1. 故障测试。** 并发同revision仅一个动作；重启时verifying→uncertain；SMTC丢失；无自动重发next；撤销ticket；恶意metadata/filename/URL；私有RPC从renderer拒绝。
- [ ] **2. 资源。** 4GiB sparse stream/合法影片seek、连续播30min、100次切换、WebView导航/退出回收IStream/handle；使用加速时钟验证24h状态寿命，再用真实Windows播放验证不能模拟的codec/COM/SMTC行为。
- [ ] **3. 回退。** 关activity flag隐藏UI不删数据；关media flag停止创建新v2操作、关闭owned player并保留metadata，external回既有路径且保留Task0真实性修复。不保证旧严格schema二进制能直接打开新DB；优先同二进制功能回退。
- [ ] **4. 执行。** 逐条运行 `go test -count=1 ./internal/mediaapp ./internal/toolruntime ./internal/winexec ./internal/desktopmedia ./internal/webviewhost ./internal/storage/sqlite ./internal/app ./internal/contract`；UI media/activity/session/settings测试；typecheck/build；PRD13.4全门。Windows-only测试需真实Windows runner，不用非Windows stub PASS代替。
- [ ] **5. Gate。** false verified/越权读取/路径ticket日志泄漏/重复next副作用均0；Range峰值内存不随文件长度线性增长；刷新/lease重建可恢复且不自动出声；媒体中心内不重复 `MediaMiniPlayer`，`playing/paused` 离页才出现，close 经 stop 终态后隐藏，uncertain/failed 不假隐藏；顶部活动入口可恢复需处理任务；核心流程键盘可操作。当前未安装 axe；本任务执行 `npm --prefix web install --save-dev axe-core`，提交 package.json 与 lockfile 的固定解析版本，在 Vitest DOM 测试中调用 axe.run 并要求 serious/critical 为0；另做真实 WebView2 键盘/读屏验收，不把 jsdom 结果冒充完整可访问性证明。
- [ ] **6. 证据输入与提交。** `scripts/test-media-session.ps1` 每次生成 `artifacts/media-session/<runID>/run-manifest.json`，固定记录 requirement/test、testedSourceHead、sourceTrackedDiffDigest、命令、start/end、exitCode、testCount、outputDigest、OS/CPU/RAM/runtime、fixture/config digest，以及日志/截图/录屏的相对路径和 SHA-256；正式 P14 运行要求 source tracked diff 为空，缺实机项时 `complete=false`。runbook说明owned/external能力差异、故障恢复和授权资产范围。本任务只把该运行目录交给 11 P14，禁止创建或覆盖 `docs/design/jiyishengji/evidence/release/<VERSION>/media-evidence.json`，其中 VERSION 逐字取 testedSourceHead 根 `VERSION`。提交 `test(media): verify desktop playback recovery and resource boundaries`。

## 执行顺序

Task0可先独立交付；Task1→Task2/3→Task4→Task5→Task6→Task7→Task8。每阶段有对应可运行测试和正式接线，不能仅以React页面出现视为播放器完成。路线已确定，后续按清单实施即可。
