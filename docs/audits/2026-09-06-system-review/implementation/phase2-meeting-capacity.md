# 长会议通信容量整改

实证：临时 SQLite 写入合法 1,048,576 个中文字符和两份合法派生文档后，经 Engine.Handle(meetings.get) 序列化的响应为 9,438,055 字节；真实 ipc.WriteFrame 因 4,194,304 字节上限拒绝发送。原始大正文会使长会议无法打开。本轮没有增加 IPC 帧上限。

已实现：

- 列表从 SQL 起不取转写、摘要、待办或摘要来源快照正文。恢复过期 summarizing 状态后也不把完整正文重新带入列表。
- 详情用独立 Metadata/Detail 路径，不读取全量片段或两份文档。只返回前 16,384 个字符，并明确 transcriptComplete、transcriptTotalRunes 和 transcriptRevision；片段预览最多 10 条完整记录。
- meetings.transcript.get 按字符 offset 分页，每页 16,384 字符，绑定实际 transcriptRevision，版本变化明确冲突。页边界保留 Unicode，不按字节截断。
- meetings.segments.list 每页最多 10 条完整片段；SQL 在同一读事务校验会议 revision，seq 游标和首读 throughSeq 固定上界。新录音追加不会混入当前翻页快照；catchup/修改造成版本变化则拒绝旧页请求。
- meetings.update 支持 transcriptEdit 的版本、offset、deleteRunes、text 范围修改，读取持久全文后替换该范围，保留其他内容，并继续使用原有会议 CAS。长稿拒绝旧客户端用 transcript 预览替换全文；普通 transcript 请求本身也限 16,384 字符，避免合法输入超过传输帧。摘要/待办与页修改可在同一次 CAS 提交。
- 导出从完整权威原稿现场生成 txt/markdown/html。HTML 转义可能使合法原稿超过 meeting_docs 的既有 2 Mi 字符缓存限制，此时保留完整 Markdown 缓存，省略过大的派生 HTML 缓存；真实 HTML 导出仍完整，不截断原稿或导出文件。
- frontend 分页编辑与原有摘要来源组件由 conversation_voice_meetings 代理接线，相关结果统一归入其前端记录。本记录不代替它的最终测试退出结果。

实际正式回归（临时 SQLite/临时导出文件，无真实录音设备）：

- internal/app/meeting_frame_test.go：原 9.4 MB 超帧场景从 RED 转 GREEN；百万字符 `<中🙂&` 全 64 页逐字重建；最大合法摘要/待办与片段均经过真实 ipc.WriteFrame；局部修改前后其余原文不变；旧版本页及编辑拒绝；txt/markdown/html 实际文件均包含完整 Unicode 原稿；大正文列表不返回正文。
- 同文件：35 条最大长度片段逐页完整读取；首读后新增第 36 条不混入既有快照。
- internal/storage/sqlite/meeting_content_pages_test.go：真实 100,000 条片段，首尾分页准确，详情只取 10 条。普通定向通过 1.683s。
- 会议普通回归通过：app 2.931s、meetings 7.732s、SQLite 1.698s。原始记录 evidence/meeting-pages-tests.txt。
- 新增容量回归 race 通过：app 42.874s、SQLite 59.150s。原始记录 evidence/meeting-pages-race.txt。

内部聚合边界已继续完成代码整改：

- 以 1,048,576 个实际 Unicode 字符（包括段间换行）、最多 100,000 个片段作为单份原稿的明确容量。SQLite 逐行读取，达到容量即停止；不再先把所有片段装入内存。统计实际 rune，不使用会在 NUL 处提前结束的 SQLite length(text)，合法 JSON 空字符也不能绕过总量准入。
- Append 的累计容量检查、seq 分配、写入处于同一写事务；两个 Service 同时追加也不能突破预算。拒绝时同事务保存容量提示，保留此前片段；不会把被拒绝的新文字假装已保存。录音仍可继续保存原始音频，直到用户停止。
- Stop 的容量处理分支实际结束已开始的录音、关闭录音资源并清除本机录音占用。旧片段或已锁存状态超限时，提交 needs_summary 与明确的 MEETING_CAPACITY_LIMIT，保留原有 canonical 原稿、原始片段和已收音频，绝不把接受的前缀裁成“完整原稿”。分页读取原始片段仍可用。
- Summarize 在录音期间保持拒绝，不会因为容量错误私自终止录音；已标记超限的原稿不调用模型。未标记的历史超限聚合也会保存明确的待处理状态。显式全文修改超过上限同样拒绝，不能静默裁尾。
- Catchup 使用流式片段统计和有界音频跨度，最多保存 4,096 个跨度结果、合计 1 Mi 字符。超限时保留已成功跨度与持久化重试记录，不向权威原稿写入不完整前缀。重启后只重试未成功跨度；容量标记下重新发起完整音频恢复从音频开头开始，原始实时片段不删除。
- ASR 单次结果不再沿用 16,384 字符的实时片段裁剪。合法且总量允许的 20,000 字符结果及末尾完整保留；结果超出总量则明确失败，保留音频用于重试。此限制是可恢复容量错误，不代表任意长度会议都能在一份原稿内完成。
- 现有内部 Summarize 返回继续带有派生文档，保证原服务契约；对界面的 Bridge 仍不传输这些重复正文。

新增正式回归为 internal/storage/sqlite/meeting_content_capacity_test.go（临时 SQLite、临时静音 PCM/WAV，不操作录音设备）：

- 恰好 1 Mi 字符 Stop 保持逐字一致；差两个字符时两个 Service 并发追加只接受一个；加入嵌入 NUL 的合法片段仍准确计数。超限后 Summarize 不偷改录音状态，Stop 返回容量错误但下一场会议可以开始。
- 旧数据库原始片段超过上限时，Stop/Summarize/Get 明确处理；模型调用为零，片段分页和实际 WAV 文件仍存在。重建 Service 后从音频恢复成功且原始片段条数不变。
- 一个跨度成功、后续跨度识别输出超限时不提交原稿；重建 Service 后只重试后续跨度，成功跨度和 20,000 字符结果的末尾均完整保留。手工全文超限也返回容量错误。

最终专项验证已完成，两个命令均以退出码 0 结束：

- 普通回归：`go test ./internal/meetings ./internal/app ./internal/storage/sqlite -run 'TestMeeting|TestMeetings|TestCatchup|TestStartAppend' -count=1`。meetings 9.855s、app 3.783s、SQLite 3.443s 全部通过。原始证据：[meeting-aggregation-tests.txt](evidence/meeting-aggregation-tests.txt)。
- 并发检测：`go test -race ./internal/storage/sqlite ./internal/meetings -run 'TestMeeting(Aggregation|LegacyOverflow|CatchupDoesNotClip)|TestCatchup(Restart|Failure|Reconciles)' -count=1`。SQLite 42.965s、meetings 41.527s 全部通过。原始证据：[meeting-aggregation-race.txt](evidence/meeting-aggregation-race.txt)。

本专项的通信容量、聚合准入、停止收尾、原始数据保留和重启重试代码闭环已完成；源码已冻结并交给主任务统一候选构建和全量门禁。本记录仅声明上述自动化验证结果，现场长会音频、实际模型和设备验收由用户负责。
