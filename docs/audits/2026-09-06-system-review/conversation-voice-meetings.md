# 对话、三条语音链路、会议纪要专项审计

审计基线：2026-09-06，HEAD `0997859`，项目版本 `v0.4.67`。本报告基于本次代码接线与隔离复现，不沿用历史 PRD 的结论。未改动产品代码；新增文件仅位于本审计目录。结论是“当前存在可复现的恢复、资源释放及数据一致性缺陷”，不能承诺现状无报错、无闪退或已达到 4.9 分。

## 1. 本次覆盖与证据边界

- 已读：实际页面、Bridge handler、会话和播放生命周期、ASR/TTS 接口、会议 SQLite 操作及迁移、长会补转写和纪要生成、聊天 checkpoint 与落库重试。
- 已做：5 个临时 SQLite 场景、3 个前端设备/Bridge mock 场景、1 个 Go overlay 聊天恢复场景，共 **9 个缺陷复现**。使用合成静音、临时目录、fake provider/adapter；没有录音、没有真实模型费用、没有修改用户数据库。
- 本文的审计测试刻意断言“当前缺陷仍能复现”；测试 PASS 表示复现成功，不是产品功能通过。整改时应另写验证正确行为的回归测试。
- 根审计另运行全量 Go/前端检查，此处不重复其结果。以下“代码确认”与“隔离复现”均不等于 WebView2、声卡、真实供应商、长会或打断体验实测。
- 尚未执行：真实麦克风/系统 loopback、弱网与设备拔插、真实 Edge/火山/ONNX/SoVITS 生成、Windows 睡眠恢复、崩溃重启录音、1–2 小时会议、真实供应商计费与权限变更、长时间资源曲线、真实对话质量/CER/首音延迟测量。

## 2. 真实链路地图

| 模块 | 入口与桥接 | 核心执行 | 数据落点及关键边界 |
|---|---|---|---|
| 打字对话 | `web/src/session/SessionPage.tsx:236` → `message.append` / `chat.start` | `internal/app/chat.go:131` → `contextapp` → provider lease/adapter → `chat_run_stream.go:100` → tools/MCP/专家等 | `messageapp.Service` 事务写消息、审计与幂等记录；`chat_turn.go:62` 写工作区 `.turns/<sessionId>.json`；前端 `liveChat.ts` 保留运行态 |
| 云端语音 cloud | `voicePersonas.ts:81`，`CompanionStage.tsx:1724` → `speech.ts:596` | Web Speech 系统/在线识别 → `beginUserTurn`/通用 chat → `TtsPlayer` → Edge TTS | 识别音频由系统浏览器服务接收；文本回复经通用聊天落库；不能注入系统 loopback 给 Web Speech；受 Windows 在线语音服务/网络/麦克风许可影响 |
| 本地语音 local | `CompanionStage.tsx:1729` → `localSpeech.ts` / `localAsr.ts` → `voice.start/append/finish/stop` | PCM 16 kHz mono → Sherpa streaming → refiner → 通用 chat → 默认 ONNX Kokoro TTS；`ref` 为可选 GPT-SoVITS | 模型资源在本机；“本地”主要指 ASR/TTS，LLM 仍取决于所选 provider；消息落点与通用聊天相同 |
| 火山语音 volc 默认级联 | `CompanionStage.tsx:1880` → `volcSpeech.ts` / `volcAsr.ts` → `voice.* {backend:volc}` | seed-asr WebSocket → 通用 chat → seed-tts（未配 TTS 的入口设置可选择 Edge） | `startVolcVoice` 使用 provider lease，限定 Volc speech host；语音需网络和对应凭据；默认级联复用聊天持久化 |
| 火山卡的可选 realtime 子链 | `talkRealtime === true` 且有 realtime 模型/会话：`companionTalk.ts:51` → `talk.start/append/cancel` | `talk_handlers.go:21` → OpenAI-shaped realtime WS，服务端 VAD；工具意图 handoff 再调用通用 chat | 这不是 seed-asr 默认链；当前普通 realtime transcript 仅在页面内存显示，见 CVM-08；须独立计入测试和发布资格 |
| 会议纪要 | `MeetingPage.tsx:566` 开始，`:642` 停止；`meeting_handlers.go` 的 `meetings.*` | 独立 PCM 录音 + WASAPI loopback/前端系统音频；实时 ASR 可选系统/本机/火山；停止 →本机 Sherpa 补转写→ `SummarizeLong`→分段/合并纪要 | SQLite `meetings` / `meeting_segments` / `meeting_docs`；独立 audio root 的 `<meetingId>/chunk_*.wav`（120 秒轮换）；补转写每 20 秒处理一段 |

三条**当前产品主路径**是 `cloud / local / volc`。`voicePersonas.ts:1` 的历史类型仍含 `omni`，但 `companionSettings.ts` 会将旧 `omni/flm` 设置迁到 cloud；不能把旧 omni 当成当前第三条主路径。`companionSettings.ts:255` 附近 rev 13 已将旧 ref 默认迁到 ONNX，入口卡片仍有旧 SoVITS 文案，详见 CVM-11。

## 3. 已实现的有效基础

- 通用消息有数据库事务、幂等 key + payload digest、重放一致性验证与审计；`messageapp/service.go:320` 比会议写入路径成熟。历史查询采用签名 cursor 与 snapshot/revision 验证，避免简单页码分页的部分漂移问题。
- 通用 chat 有流数量限制、序列化事件编号、终态认领与取消竞争处理；流失败可保留部分回答；落库失败有 `PersistFailed/PersistDraft`、用户重试入口。不能因 CVM-01 否定已有恢复设计，但必须修复新轮覆盖的边界。
- ASR 与 TTS 有独立抽象，PCM 格式统一；本地资源安装包含下载/摘要/解包校验；ASR 异常有重建会话、字幕丢失提示；火山 ASR 已实现最大 1 秒批次切分及有限队列。
- `CompanionStage` 有状态机、回声过滤、播放互斥、手动打断、启动 deadline、晚到 handle 清理、代际标记；默认关闭 realtime 的选择降低了级联/实时同时播音的概率。
- 会议录音与实时 ASR 分离，ASR 重启不应直接结束录音；音频按 WAV 轮换；长稿分块总结有作业 deadline；摘要失败保留逐字稿，并能回收重启遗留的 summarizing 状态。
- 会议导出 Markdown/HTML/TXT，HTML 做内容转义；DeleteMeeting 与 ReplaceDocs 各自有事务；这些基础适合定向升级，无需无依据地推倒重写。

## 4. 确定缺陷与整改验收

严重性口径：P1 为当前存在用户数据丢失、状态回退、麦克风释放失败或主链可用性故障的发布前阻断项；P2 为产品事实不一致等应修项。本文没有以未实测的潜在崩溃直接标 P0。

### CVM-01 / P1：下一轮覆盖上一轮未落库回答的唯一恢复副本

- 证据：`internal/app/chat.go:171–172` 忽略 `retrySessionPersistDraft` 的错误；`chat_turn.go:62–66` checkpoint 只有 session 维度；`chat_run_stream.go:117` 新建空 turn，`:249` 与 `:1154` 覆盖同一路径。`chat_turn_persist.go:38–58` 的重试失败没有额外持久队列。前端虽另有 localStorage 恢复副本，但 `persistRetry.ts:2,21` 也仅按 session 存一条，`SessionPage.tsx:242` 下一轮 completed 会将其替换为新草稿或清除，所以不能作为多轮故障保全机制。
- 触发：上一轮模型生成成功但消息落库失败，用户继续发下一轮，自动重试仍失败。无需同时开启两轮即可发生。
- 影响：上一轮完整答案没有进入消息库，checkpoint 又被下一轮草稿替换；刷新后无法依赖原有重试入口恢复旧回答。
- 隔离复现：`chat_reproduction_test.go.txt`，由 `go-overlay.json` 注入虚拟测试文件；fake adapter 与持续失败 writer 验证旧草稿被下一轮 `answer` 替换。
- 整改：建立按 `sessionId + turnId/streamId` 的 durable turn/outbox；未确认写入的草稿只能在事务确认后删除；新轮不复用旧轮恢复槽；checkpoint 写入错误必须可见。短期可阻止覆盖未修复草稿，但不得静默丢弃。
- 验收：连续三轮存储故障、恢复后任意顺序重试、重启恢复，各轮答案恰好一次入库；已接收内容不丢失；同一 stream 不会因正文变化触发幂等冲突。

### CVM-02 / P1：本地 ASR 缓存上限与服务端单帧上限冲突

- 证据：`localAsr.ts:19` 保留最多 2 秒 pending；`:220–236` 合并全部 pending；`:249` 一次 append。`internal/voice/voice.go:163–172` 只允许 10×100 ms，32000 bytes；`sherpa_backend.go:275–279` 拒绝超限。火山 `volcAsr.ts` 已有对应切片逻辑，本地未同步。
- 触发：模型冷启动/桥接慢调用期间累计大于 1 秒 PCM，随后 session 打开或 append 释放。
- 影响：正常采集产生的请求被自身后端拒绝；重复同一非法 payload 不会恢复。实际 Go handler 将写入错误标 retryable，本地会重复发送后重建会话并丢掉该批音频，表现为漏字、断句或识别重启。
- 隔离复现：15×100 ms 模拟启动缓存生成一个 48000-byte payload，超过上限 50%。
- 整改：提取共用 PCM 有界队列/切片实现，所有 `voice.append` ≤32000 bytes；按确认水位移除，保留尾音；溢出应明确计数和提示。
- 验收：冷启 0/0.5/1.5/3 秒、100 ms–2 秒 append 延迟、恢复后队列排空；全样本顺序守恒，超过容量时报告明确丢帧数；本地/火山使用同一契约测试。

### CVM-03 / P1：停止会议先等存储，麦克风最多额外保持 120 秒

- 证据：`meetingAudio.ts:76–98` drain 最多等 120000 ms；`:151–159` 先 drain，后置 closed 与 capture.stop。`MeetingPage.tsx:547–555` 卸载时同样调用该 stop。
- 触发：音频 append 长时间未返回，用户点停止或离开会议页。
- 影响：停止操作未及时释放麦克风；等待期间继续收音入 pending；截止后 pending 清空，且 append rejection 被 onError 吞掉，没有“哪些录音未写入”的稳定标记。
- 隔离复现：挂起 append 后调用 stop；虚拟时间 10 秒时 capture.stop 仍未调用，120 秒后才调用。
- 整改：先冻结采集并 flush 最后帧，立即停止/释放设备，再后台持久化已冻结队列；保存状态独立于录制状态；超时保留未确认批次或显式显示缺口，禁止无声清空。
- 验收：无论存储阻塞与否，用户停止后 500 ms 内设备 track 结束；30 秒写入阻塞不再采集新样本；恢复后尾音恰好一次写入；强退有可恢复批次清单。

### CVM-04 / P1：迟到 heartbeat 能把已停止会议恢复成 recording

- 证据：`meetings/service.go:315–325` Heartbeat 读整行再 UpdateMeeting；`:338–360` Stop 独立读改写；`storage/sqlite/meetings.go:19` UPDATE 将 status/ended_at/transcript 等整行覆盖且无版本条件。前端 `MeetingPage.tsx:455–460` 的已在途 heartbeat 不随清 interval 而取消。
- 触发：heartbeat 读到 recording → Stop 写成 transcribed → 旧 heartbeat 才落库。
- 影响：数据库回到 recording，endedAt/transcript 可能被旧快照清空；UI 停止和数据库状态分裂；新会议可能 ErrBusy。Append 的 `service.go:303–304` 也有同类整行回写风险。
- 隔离复现：真实 SQLite + 门控 UpdateMeeting，验证 transcribed→recording 且 endedAt 被清空。
- 整改：数据库状态转换使用条件 UPDATE/事务/版本号；heartbeat 只写 duration/updatedAt 且 `WHERE status='recording'`；片段追加不回写旧整行；每 meeting 的 lifecycle 明确序列化。
- 验收：heartbeat/append/audio/stop 的交错执行至少 1000 组；停止状态不可逆；晚到写请求被拒绝或安全忽略；任一时刻最多一个 recording，建议数据库部分唯一约束补强。

### CVM-05 / P1：补转写失败之前已删除原始带时间戳片段

- 证据：`meetings/catchup.go:126–142` 当 live 文本不足 80 字且接近音频尾部时，先 DeleteSegments，再判断 audio root/transcriber；成功译码之前已破坏原始 segments。
- 触发：短会议/稀疏字幕后补转写，本机识别缺失或解码失败。
- 影响：原始 timed segments 消失。已有扁平 `meeting.Transcript` 在本复现中仍保留，不能夸大成整段文字立即全部消失；但原始时间与分段证据已丢失，后续重建可能覆盖旧稿。
- 隔离复现：5 秒合成录音 + 一条“客户确认周五交付”；补转写失败后 segments=0，旧扁平 transcript 仍存在。
- 整改：保留 immutable live transcript/source segments；补转写写新版本/暂存结果，达到完整性条件再事务切换；失败保留可追溯源段。
- 验收：缺模型、全段失败、部分失败、取消、磁盘满后原 segments 与原音频哈希完全不变；重试可从原数据恢复，不靠字符串猜测旧内容。

### CVM-06 / P1：纪要完成会覆盖用户在生成期间保存的修订

- 证据：`service.go:436` Summarize 读取快照，`:490–498` 完成后整行写回；Update 仅禁止 recording（`:615`），允许 summarizing，`:631–635` 写用户新逐字稿；存储层无 revision/CAS。
- 触发：摘要生成中用户改正逐字稿或改标题/摘要，然后生成完成。
- 影响：已成功保存的人工内容被模型作业启动时的旧快照覆盖；摘要也可能基于旧稿而界面没有冲突提示。
- 隔离复现：阻塞 fake completer，保存“人工校正后的逐字稿”，释放 completer 后回到“原始逐字稿”。
- 整改：summary job 绑定 sourceRevision；完成只更新生成字段并 CAS；若源稿变更，保留旧 job 结果为待查看版本、提示需要重生成，不能覆盖用户编辑；Docs 与元数据版本一起提交。
- 验收：生成中改逐字稿/标题/摘要、重复生成、删除、失败重试均有明确冲突策略；每个摘要可定位输入修订；界面“保存成功”对应的内容不会静默回退。

### CVM-07 / P1：音频重试没有批次幂等，重放会重复写 WAV

- 证据：`MeetingPage.tsx:81–91` retryMeetingWrite 自动重试；`:410`/`:581` 用于 audioAppend；payload 只有 meetingId/pcm。`meeting_handlers.go:73–92` → `service.go:386–411` → `audio.go:146` 直接追加。
- 触发：后端已写入，但桥接回执丢失/超时；前端按 retryable 再发同一逻辑 batch。
- 影响：相同声音被写入两次、录音时长漂移；补转写时间轴与 live timestamps 错位。普通“两个相同 pcm”也可能是合法连续静音，所以不能用内容哈希单独去重。
- 隔离复现：同一合成 1 秒 batch 调用两次，返回 1000→2000 ms，确认服务没有重放保护。
- 整改：采集 sessionId + 单调 chunkSeq + 样本区间 + checksum；后端持久 ACK，重复同 seq 同 hash 返回旧 ACK，异 hash 拒绝；收到确认前前端保留批次。
- 验收：回执丢失、超时重试、乱序、重复、重启重传；逻辑音频只写一份，样本计数精确，时间轴连续或明确标缺口；不能把合法重复静音误判为重试。

### CVM-08 / P1（仅 opt-in realtime）：普通实时聊天没有进入会话历史

- 证据：`talk_handlers.go:220–233` 只 send transcript event；`CompanionStage.tsx:1789–1814` 只更新 React rounds；此链未见 message.append/AppendAssistant、durable turn/checkpoint 写入；tool handoff 路径例外，会转通用 chat。
- 触发：开启 realtime，用普通语音闲聊后退出/重进。
- 影响：对话历史、后续上下文、恢复和审计与默认级联链不一致；传入 sessionId 当前主要用于路由连接，不代表已入库。
- 证据级别：端到端代码接线确认，未连真实 realtime provider 验证。
- 整改：以 provider conversation/item/response ID 归一成通用 turn 事件；区分 assistant delta/final、播出部分、打断；用户和助手 final 各恰好一次持久化，注入后续上下文。
- 验收：20 轮 realtime 闲聊、重连、打断、退出重进，历史和上下文一致；handoff 不重复落用户消息；取消保留已说内容且不伪称完整回复。

### CVM-09 / P1（仅 opt-in realtime）：远端结束通话后麦克风 handle 丢失

- 证据：`companionTalk.ts:187–190` ended 仅 markFirst + onEnded；未调用同文件 stop。`CompanionStage.tsx:1841` onEnded 先将 talkHandleRef 清空，此后退出清理拿不到原捕获句柄；pump 仍可尝试 append，错误被吞。
- 触发：已经成功开始的 realtime 连接由远端/error 结束。
- 影响：界面已结束，但 capture 未停止，可能残留麦克风占用和无效发送；阻止后续链路顺畅重新获取设备。
- 隔离复现：fake talk 发 ended 后 onEnded 已执行，capture.stop 未执行；显式 handle.stop 才释放。
- 整改：让 talk session 自己拥有并保证 terminal→stop，统一 error/ended/start failure/cancel/unmount 的 exactly-once cleanup；外部回调在资源终态之后执行。
- 验收：远端 EOF、error、首音超时、主动取消、页面卸载、设备异常各路径 track 500 ms 内关闭、发送停止、引用归零；可立即重启且只存在一个捕获者。

### CVM-10 / P1：部分补转写失败被成功尾段掩盖，重试永远越过缺口

- 证据：`catchup.go:153–156` 解码失败只记 lastTranscribe 后继续；`:175` 仅全部未写且没旧文时才提示失败；`:80–101`/`:121–129` 仅使用最大 startedMS 与尾部时间差判断还要补什么。
- 触发：某一早期/中间 20 秒 span 解码失败，后续 span 成功且总文本达到 80 字，尾部距 audio 尾小于 15 秒。
- 影响：时间轴有真实缺口却无完整性错误；再次 CatchUp 不访问失败片段；后续纪要看起来成功但漏掉会议内容。
- 隔离复现：45 秒音频，第一段失败、后两段成功；第一次仅留下 20s/40s 起始段且 SummaryError 空；第二次重试 transcriber 调用数仍为 3。
- 整改：按音频 span 持久记录 pending/running/succeeded/failed、attempt/error/source hash；覆盖率按区间并集计算；失败段独立可重试；摘要必须显式标注缺口，不能因为尾段成功宣称全文完成。
- 验收：首段/中段/尾段失败矩阵；只补失败段、成功段不重复；用户可看到缺失区间和完整率；恢复后补齐所有段并按版本重新生成摘要。

### CVM-11 / P2：本地语音卡片与当前默认实现不一致

- 证据：`voicePersonas.ts:90–96` 写“离线克隆 / sherpa + GPT-SoVITS / 克隆 50 种人生”，而 `companionSettings.ts` rev 13 默认及迁移为 `onnx` + `onnx-zf-xiaoxiao`，SoVITS 仅显式 ref 选择。
- 影响：用户期望与实际安装资源/音色/故障排查目标错位。此处不评价哪种音色更好，也未测量真实克隆效果。
- 整改与验收：卡片从实际 runtime descriptor 派生；显示本次 ASR/LLM/TTS 实际 provider/engine 与降级状态；全新配置/旧 ref 迁移/显式 ref/故障降级四场景文案准确。

## 5. 尚需验证的代码疑点（不计为已复现缺陷）

1. `chat_turn.go:85–109` checkpoint 写固定 `.tmp`、先删原文件再 rename、吞写错误；后台和前台若同一 session 多 stream 并行可发生覆盖/临时文件竞争。普通 SessionPage 当前有排队/任务切换限制，不能仅凭 liveChat 多 turn 容器断言普通操作必然同时运行；需要覆盖其他入口、取消超时后重开与自动化入口。
2. `talk_runtime.go:21–39` 写持有 writeMu；shutdown 也先取 writeMu；`talk/conn.go:32` WriteMessage 没有写 deadline。远端停止读取使发送长时间阻塞时，取消用于关闭连接的路径可能等同一锁。应追加本地 socket/fake Conn 的阻塞写取消测试。
3. `voice_handlers.go:406–465` session map 没有显式总量/闲置 TTL；启动请求回执丢失时前端无法拿到 sessionId；voice session 生命周期未与 renderer connection 租约明确绑定。需要可控断桥测试，量测 goroutine/child/socket 回收。
4. `MeetingPage.tsx:389–434` 恢复录音过程在 `await startMeetingAudioRecorder` 后缺少紧邻的 alive 检查；卸载清理与异步设备获取交错可能晚建捕获。需要延迟 getUserMedia resolve + unmount 的页面测试。
5. 会议按“麦克风录音帧”驱动混音与落盘，前端离开会议页后停止 mic/ASR，而后端 recording/loopback 仍保留；产品是否要求后台持续录制必须明确。当前代码不能当作后台持续录音已经验收。
6. `Service.Start` 的 HasRecording→InsertMeeting 非事务，数据库未见 recording 部分唯一约束；需两次并发 start/崩溃后恢复的真实 handler 测试。
7. 摘要元数据 UpdateMeeting 与 ReplaceDocs 为两个提交；第二步失败会出现 ready 但导出快照旧/缺；需要存储故障注入核查读取使用实时字段与 docs 缓存的所有消费方。

## 6. 建议现状评分（供总审计统一权重）

评分衡量本次已读实现的工程完整度与证据成熟度，不是线上成功率，也不是主观音色评分。4.9 的前提是阻断缺陷清零并完成真实环境量化验收，不能由“单测多/编译通过”推得。

| 模块 | 建议 /5 | 依据 |
|---|---:|---|
| 打字对话 | 3.4 | 消息幂等/审计/流终态基础较好；CVM-01 恢复数据丢失；checkpoint 双存储一致性尚弱 |
| 云端语音默认链 | 3.4 | 状态机/字幕/回声/手动打断完整，复用通用聊天；继承 CVM-01；真实 Windows 在线识别、音频延迟与多轮体验未测 |
| 本地语音默认链 | 2.8 | 双 ASR + ONNX 默认可用架构存在；CVM-02 是主链输入契约错误；CVM-11 文案失配；部署/冷启/设备实测欠缺 |
| 火山默认级联 | 3.3 | 独立 SAUC 适配、host 限制、有限队列、1 秒切片、TTS 互斥；继承聊天恢复问题；真实授权、弱网、回声及打断未测 |
| 火山卡可选 realtime | 2.3 | 连接、VAD、PCM、工具 handoff 已接；CVM-08 历史缺失与 CVM-09 资源释放失败；应作为独立 feature gate |
| 会议纪要 | 2.6 | 录音/ASR 分离、滚动 WAV、长稿总结与导出基础齐；CVM-03–07/10 涉及停止、状态、原稿、缺口、重试完整性 |

若总表只允许“三条语音”而无 realtime 行，应在火山行同时展示默认级联 3.3、opt-in realtime 2.3；不要掩盖模式差异后只报一个高分。

## 7. 可执行 PRD 工作包（先修契约和数据，再优化体验）

| 顺序/ID | 边界与交付物 | 依赖 | 完成判据 |
|---|---|---|---|
| 0 / CVM-P00 | 固化 cloud/local/volc/realtime 与会议能力矩阵、接口样本、当前问题复现；把 9 个审计复现转换成正确行为回归用例 | 无 | 报告基线、测试命令、硬件/供应商测试矩阵可重跑 |
| 1 / CVM-P01 | 按 turn 持久恢复状态和未完成写入队列；数据库迁移、旧 checkpoint 导入、ACK 后清理；修 CVM-01 | P00 | 跨重启不丢多轮草稿，按 turn 幂等，故障可见；旧用户数据迁移成功 |
| 1 / CVM-P02 | 共享有界 PCM transport；统一 frame limit、样本计数、队列/ACK、尾音 drain；修 CVM-02 | P00 | local/volc 相同契约向量通过，无自生超限帧 |
| 1 / CVM-P03 | Mic lease/capture 生命周期组件，停止采集与持久保存分离；统一 terminal cleanup；修 CVM-03/09 | P00 | 所有退出/错误设备 500 ms 内释放，后台保存有真实状态 |
| 2 / CVM-P04 | 会议版本与条件状态更新、recording 唯一约束、局部心跳字段、摘要 revision 绑定、Docs 同版本提交；修 CVM-04/06 | P00 | 并发 stop/edit/summary/heartbeat 不回退、不覆盖用户新稿 |
| 2 / CVM-P05 | 持久音频 chunk manifest/ACK/幂等、区间覆盖率、原始 live segments 不可变、补转写版本化；修 CVM-05/07/10 | P02/P03/P04 | ACK 丢失不重音、部分失败可精确续传、原稿哈希不变、缺口可见 |
| 3 / CVM-P06 | realtime 转通用 turn/item 生命周期与数据库，工具 handoff 去重，已播部分/打断归档；修 CVM-08 | P01/P03 | 20 轮聊天、打断、重连历史一致；未达门槛保留默认关闭 |
| 3 / CVM-P07 | 运行时能力 descriptor 驱动卡片/状态，明确 ASR/LLM/TTS、资源路径、降级、在线/本机边界；修 CVM-11 | P02 | 文案和实际生效引擎逐项匹配，无旧默认误导 |
| 4 / CVM-P08 | Windows 真机矩阵、低资源/弱网/热拔插/睡眠、长时 soak、录音完整性和延迟质量验收；冻结 release gate | 前述全部 | 无未解决 P0/P1，量化指标与原始结果齐全，再决定是否达到 4.9 |

实现纪律：每个包绑定上述缺陷 ID 与具体函数/表，不顺便重写其他中心；迁移可备份、可回滚；旧接口采用版本兼容或增量字段；真实 provider 测试使用专用配置与明确成本上限。不能把缺口录音、被截断回复或 fallback 状态标为完整成功。

建议量化门槛（最终由总 PRD 固化）：三条默认语音各 100 轮连续成功，冷/热启动和重连独立统计；同句 repeat/改口/长停顿/播放回声/打断 ≥50 组；停止 track ≤500 ms，终态恢复 ≤3 秒；会议 2 小时 ×3 种 ASR，WAV 样本计数与 manifest 一致、无未标注缺口；600 次超时/重放/乱序/持久化故障用例不丢已确认数据；真实首音 P50/P95 按 ASR/模型/TTS 与机器分组记录，阈值以测量基线确定，不能先编造当前延迟。已知 bug 全部修完仍不能替代这些真机验收。

## 8. 独立复现材料与命令

目录：`docs/audits/2026-09-06-system-review/conversation-probes/`。

```powershell
# 5 个会议 SQLite 复现；临时目录与合成静音
go test -tags auditprobe ./docs/audits/2026-09-06-system-review/conversation-probes -count=1 -v

# 3 个前端 mock 复现；无设备和网络
node web/node_modules/vitest/vitest.mjs run --config docs/audits/2026-09-06-system-review/conversation-probes/vitest.audit.config.mjs

# 1 个聊天恢复 overlay 复现；只将审计文本虚拟映射到 app 包
go test '-overlay=E:/Trae-Work-Projects/lunitide/docs/audits/2026-09-06-system-review/conversation-probes/go-overlay.json' ./internal/app -run '^TestAuditReproduce' -count=1 -v
```

最后验证：5 个 Go 会议场景已用 `-tags auditprobe` 完整运行通过；3 个前端场景最终一次运行通过；1 个 overlay 场景通过。Go 会议探针使用 `//go:build auditprobe`，默认 `go test ./...` 不收集该审计包。再次强调，这 9 个 PASS 是 9 个缺陷被复现。Overlay JSON 使用当前审计工作区绝对路径，迁移工作区时需更新其映射。

2026-09-06 14:10（Asia/Shanghai）为固化交付证据按以上命令重新执行，未扩展测试范围。原始输出与尾部审计元数据已保存：

| 日志（均在本审计目录 `evidence/`） | 场景数 | 退出码 | 结果含义 |
|---|---:|---:|---|
| `conversation-meetings-probe.log` | 5 | 0 | 5 个会议缺陷成功复现 |
| `conversation-voice-probe.log` | 3 | 0 | 3 个语音/采集缺陷成功复现 |
| `conversation-chat-probe.log` | 1 | 0 | 1 个聊天恢复缺陷成功复现 |
