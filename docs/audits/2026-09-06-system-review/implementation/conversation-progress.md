# 对话音频与会议整改实施记录

基于 `PRD-system-upgrade.md` 的 C02–C05；产品修改不触及 chat、MCP、插件、电脑控制、同事模块。真实设备/付费服务尚未验证。历史审计日志保持不变；原缺陷存在性探针作为基线材料，新代码的验收以正式包内正确行为回归测试为准。

## 批次 1：PCM 契约与采集生命周期（已实现）

- C02 / CVM-02：新增共用 `pcmQueue.ts`，local/volc 共用最多 16000 samples 的切片，保留顺序及不足整帧尾部；本地 session 样本计数按实际批次累计。
- C03 / CVM-03：会议 stop 先 flush、冻结采集并立即调用 capture.stop，再等待冻结队列保存；重复 stop 共用 Promise；恢复设备迟到时关闭新句柄。
- C03 / CVM-09：realtime 远端 ended/error 通过统一终态清理释放 capture，并去重终态；启动期间迟到的 stream/capture 也会关闭。
- 正式回归新增：1.5 秒本地启动缓存分两次合法发送；单帧/跨帧/大输入切分样本守恒；阻塞写入下立即停麦且只保存停止前尾帧；远端 ended/error 重复事件恰好一次清理。
- 定向验证（2026-09-06 14:22）：5 文件、59 项通过：`pcmQueue.test.ts`、`localAsr.test.ts`、`volcAsr.test.ts`、`companionTalk.test.ts`、`meetingAudio.test.ts`。
- 剩余：PCM overflow 的持久丢帧证据、会议写入 timeout 后可靠保留/重传由 C05 批次处理；实际设备释放 500 ms 指标仍需真机测量。

## 批次 2：会议旧快照保护（已实现）

- 心跳及字幕写入后更新时间改为仅录制态局部更新，不再回写整行旧快照。
- 摘要开始、结束、失败收尾、人工修改和超时回收使用基于 updatedAt 的 CAS；时间生成器使用纳秒原子递增，避免同一毫秒生成同版本。
- 摘要完成时若会议已被人工编辑，保留最新标题/摘要/原稿，明确提示基于新内容重试；后台短写入串行化，模型等待不占锁。
- 正式回归：迟到 heartbeat 不复活 stopped；模型成功/失败两条路径均保留等待期间人工修改。会议短生命周期及原有包测试保持通过。
- 范围界限：当前源版本由 updatedAt 内部保护，未新增对外 expectedRevision API 或通用事务 outbox，不宣称完整 C04 所有架构目标已实现。

## 批次 3：补转写原稿及缺口（已实现）

- 不再删除 live segments，也不向原始时间段混写补转写结果；补转写独立 journal 保存每个区间的音频摘要、结果和错误。
- 失败区间保留原稿并显示缺口，成功尾段不隐藏前段失败；再次调用或 Service 重建后只重试失败/缺失区间。摘要入口与页面均阻止缺口状态被自动摘要掩盖。
- journal 写入采用同目录临时文件、文件 sync、原子替换；SQLite 原稿视图更新前写入 prepared transcript+revision，可协调数据库已提交而最终 journal 尚未落盘的进程中断。
- 人工修改期间的补转写结果因 CAS 冲突不覆盖人工内容；完整结果重复读取不改变 ready/transcribed 版本。
- 正式回归：原始时间段完整保留；45 秒录音首段失败后重开 Service 仅重试该段；摘要不掩盖缺口；最终 journal 提交中断后的重开协调。

## 批次 4：音频持久 ACK 与前端停止语义（已实现）

- 新 payload 一律携带 captureSessionId、chunkSeq、sampleStart、sampleCount、SHA-256 digest；响应回显并由前端校验。旧无 key payload 仍兼容，但旧调用自身不具有幂等承诺。
- 一个不可变 batch JSON 同时包含身份、PCM、混音后 checksum 和确认时长。临时文件 sync 后 rename 是提交点，无“WAV 已追加但 manifest 尚未提交”的窗口；重开索引只读取已提交文件，校验身份/样本连续性/内容摘要。
- 内存索引只存 metadata；读取时聚合为最多 20 秒 ASR 区间，旧 WAV 可以与新批次按顺序读取。无需数据库迁移。文件级提交覆盖进程中断恢复，不承诺机器断电/存储硬件故障时零损失。
- 原批次未确认前保留并重试，后续批次不会越过未确认序号；停止先冻结并释放采集，再排空保存。页面取消了原有的先 flush 等待；保存超过 120 秒明确失败并保留当前页面队列供再次停止重试，不继续生成完整纪要。
- 正式回归：真实关闭/重开 SQLite 与新 Service 后重放；同 identity 改内容拒绝；digest 不符拒绝；seq 缺口拒绝；停止后旧 ACK 可重放而新录音拒绝；legacy/new 混合读取与 ASR 批次上限；前端原批次重传、ACK 错配拒绝、保存期限后队列保留。
- 定向验证：2026-09-06 14:47–14:49，meetingAudio 8 项通过；会议页面原有 25 项通过；会议包全量及新增中断/幂等定向测试通过，最终汇总由主任务执行。
- 范围界限：未确认队列仍在当前渲染进程内；用户强制关页/崩溃前尚未提交的音频不承诺可恢复。长时磁盘容量和批次文件数量性能、真实设备停止时延仍需后续验收。

## 批次 5：realtime 正式历史与 handoff（已实现）

- C06 / CVM-08：协议解析区分临时 delta 与 provider completed/done，保留 item/response/event/content index 身份；仅 final 写入正式 messages，助手消息复用 canonical token ledger。缺少稳定身份、非法分段号、存储不可用时明确失败关闭。
- 稳定写入键绑定 session + talk + role + provider item + content index；单一身份不同文本冲突；提交响应丢失使用相同 key 重试。已收到 final 的写入使用独立 5 秒保存上下文，因此同步取消不会删除已确认历史；取消/断连不把 delta 拼成假 final。
- talk.start 在拨号前验证会话/项目可写及存储可用；写入前重新验证作用域；远端断连与保存失败采用真实 failed 终态，释放 socket 和 stream slot。
- 持久提交后才发带 messageId 的 final/handoff 事件；CompanionStage 透传给 SessionPage，父发送流程从当前 session 历史核对 ID、user role、completed 状态及文本，再复用已保存消息，避免 handoff 再追加。模型无法直接指定这一已保存 messageId。
- 正式回归：20 轮/40 条双向消息重放及重新打开读取；同身份改内容冲突；missing session 拒绝；收到 final 同时取消仍保存；只有 delta 时取消不保存；final/handoff ACK 前历史可查询；存储失败不 ACK、不继续处理；前端错误 session/role/text/id 拒绝；Stage 透传 ID；SessionPage 实际整合验证不重复 append 且 foreign ID 不触发 chat。
- 定向验证：15:00 前端新增整合及 talk3 文件20项通过；此前 related4文件36项通过（重叠套件不能相加）；15:02 后端存储失败边界单项通过。主任务统一执行最终全量检查、统计和生成 Bridge。
- 边界：确认过的消息进入正式历史；provider 尚未发 final 的字幕、完全无法提交存储的 final 不承诺跨崩溃恢复。没有把已有历史在 reconnect 时重新注入 provider 会话，也未承诺跨全新 provider conversation 复用旧 item identity。没有真实 WS/provider 或设备验证。

## 收口与复核

以上整改代码进入主任务全量检查；历史审计材料保持不变。下一步只读交叉复核 C01 journal 的恢复、迟到清理及 usage 边界，修复检查发现的本模块回归。

## 批次 6：C01 交叉复核后补修（已实现，主任务协调授权）

- 明确取消且未取得持久化资格时清除本轮 live draft、usage 和 persistFailed，不让下一轮恢复反向撤销用户取消；失败/未确认提交继续保留草稿。真实 runStream 取消后重开及恢复回归通过。
- 恢复检查点保存按 750 ms 或新增 4096 字节触发，避免累计 80 字后每个 delta 都重写整段 SQLite。最终回复仍走完整保存，时间/增长阈值回归通过。
- 会话/项目删除在同一事务删除相关 chat_turn_journal；PutChatTurn 原子验证实体 session 存在，迟到保存不能重建被删除草稿。两种删除及迟到写入回归通过。
- 生产 journal 增加只读实体 session 存在性查询；chat.turn.get、正常加载与旧版文件导入之前校验，即使旧 .turns 文件还在也不能读取或重导入已删除会话。无真实实体读取能力的旧测试/轻量引擎保持兼容行为。
- 正常助手最终写入与 journal 重试共用 appendAssistantTurn：第一片保留原 turn key，后续按 turn:part:N 稳定命名；逐片满足 16384 Unicode 字符/65536 字节限制，CRLF 全局归一化但不截断正文；provider usage 仅第一片记一次，各片正常记录 canonical tokens。中间片写失败保持完整 journal，重试不会重复已提交片。
- 恢复后的 pushInboundReply 收到完整 draft 一次，正常完成时收到完整 text 一次，分片 helper 不逐片重复发送、不叠加已有流式 delta；Completed.messageId 指向最后一片，历史可加载全部片段。
- 正式回归：33000 个 emoji 加尾文在第二片写入失败后重新打开 SQLite 恢复全部三片，连接后逐字相同、额度合法、provider token ledger 恰好一行 37 tokens、pending 清空。
- C01 24 小时后幂等过期导致重复的独立发现已交主任务，由主任务修复 messageapp/SQLite 永久 turn 去重并测试；本代理未改该部分。message.rewind 与 journal 草稿一致性由主任务负责。
- 剩余边界：journal 当前 1 MiB checkpoint 上限仍然存在，异常超大 metadata/无限输出需要明确更高层生成预算及恢复导出方案。该项没有被本次分片伪装为已满足。

## 最终定向验证记录

- 15:02：旧单参数 onSend 回归涉及的 a11y/headstart/recognizer/task 四文件 65 项通过；无 ID 保持单参，带 ID 使用二参。
- 15:08：C01 取消重开、保存节流、删除会话/项目及迟到写入通过。
- 15:15：C01 共 6 个目标测试通过，覆盖超长恢复、原 journal 重开/丢失 ACK、存储故障阻断、明确取消、节流、删除后旧文件访问。
- 重点 race：meetings、app、sqlite 的音频、补转写、final 历史、取消、删除目标通过；该次 regex 对 internal/talk 没有匹配测试，因此不把该包算作已验证 race 场景。
- 最终全量 Go、TypeScript、前端、lint、contract/build 结果以主任务统一日志为准；各次重叠定向测试不能简单累加为独立覆盖数量。
