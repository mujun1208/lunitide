# 第二阶段：对话与会议代码收口

用户负责真机及现场验收。本阶段继续完成代码内剩余项；历史审计证据不改写，当前目录新增修改保留。

## C01：生成总预算与超大草稿恢复

- 新增同一轮共享生成预算：最多 512 KiB 输出（正文、推理、工具参数合计）、131072 provider 输出 token、10 分钟累计 provider 时间。主循环、补充总结、并行专家讨论共享同一预算；等待人工审批不占 provider 时长。本地脑输出使用同一字节边界。
- 达到预算后取消该 provider 请求，停止后续生成，发送明确 `TURN_GENERATION_BUDGET_EXCEEDED` 终态和可继续提示；已接收正文完整保存。预算不通过静默截断装作成功。
- 新增 `0126_chat_turn_checkpoint_parts.sql`：大 checkpoint 分成最多 256 KiB 的块，SHA-256/字节数/块数收据和所有块与 journal 在同一事务替换。读取使用同一快照并验证顺序和摘要。原 1 MiB 总长度拒绝已移除，旧版大文件可导入后恢复全文。
- 保存成功删除多余块；会话/项目删除级联清块；消息回退同一事务清块及引用并保留不可重导入的 journal 记录。
- 回归覆盖：跨多轮预算、未通过 delta 返回的工具参数、provider 超时取消、真实 runStream 保留预算前全文并发明确错误、并行专家共享预算；大于 1 MiB 的旧草稿导入→关闭数据库→重开→全部分片恢复→重试不重复且 usage 只记一次；两种删除和回退均不保留分块。
- 已验证：`go test ./internal/app ./internal/storage/sqlite -run '^Test(Generation|RunStreamBudget|LegacyCheckpointAbove|MessageRewindAbandons|DeleteRemovesChatTurn|UpgradeV26)' -count=1` 通过；并行共享预算另做定向/race 验证。实际磁盘故障/物理掉电由现场验收覆盖。

## C04：完整客户端 revision/CAS

- 新增 0129 独立 revision，心跳只更新时长和活性时间，不改变内容版本；内容/状态变更及删除在同一存储事务校验 revision，成功后递增。
- 五个修改桥接口 stop/catchup/summarize/update/delete 强制 expectedRevision，旧客户端省略时明确拒绝，无法覆盖新版本。公开 MeetingDTO 和前端调用均携带 revision。
- 前端保留本地编辑及其基准版本，冲突显示服务端最新内容，用户可采用新版本或明确以新版本再次保存；保存等待期间继续输入不会被 ACK 覆盖。会议切换、心跳/摘要轮询隔离迟到响应。
- 回归：真实 SQLite 的心跳/递增/旧更新/旧删除/旧摘要/旧补转写/五种省略 revision 全通过；MeetingPage 29 项通过，含版本冲突、保存期间输入、选择切换、录音启动晚于页面退出立即释放。

## C05：跨 renderer 退出的 PCM 持久恢复

- 新增无运行时依赖的 IndexedDB 队列，以 strict readwrite 事务持久保存收到的 PCM（包括不足一批的短尾）；帧原子转为不可变发送批次，序号、样本位置随同提交。
- 发送前必须完成队列提交；批次 SHA-256/身份与后端持久 ACK 全部匹配才事务删除。传输超时、ACK 丢失、应用重开均复用原 identity，后端既有不可变批次幂等与此闭合。多个队列连接原子领取同一批次，不能产生两个身份。
- 重新进入仍在录制的会议自动重放旧采集周期，再发送新音频；停止先释放设备，未成功保存会明确提示本机队列仍可恢复，保留重试入口。磁盘/浏览器存储失败停止继续采集，已提交队列保留，失败帧内存重试；物理故障发生在任何本地事务提交之前的最后采集帧无法宣称已持久保存。
- 测试仅增加 fake-indexeddb 开发依赖，生产不依赖它。目标回归：队列 3 项、音频录制 8 项、会议页面 29 项共 40 项通过，桥接口另 8 项通过。测试覆盖重开短尾、已提交但 ACK 丢失、错 ACK 不删除、会议隔离、双连接并发、停止超时重试和停止冻结尾部。

## B06：真实 Mermaid 渲染进程隔离

- 主界面不再加载/运行 Mermaid parser 和 render，只调用 host-only `diagram.render`，收到 SVG 后沿用现有安全挂载。
- Host 用现有 commandworker 启动自身 `desktop.exe --diagram-worker=<私有任务文件>`：Windows 子进程先 suspended，成功加入 kill-on-close Job Object 后才 resume。子进程独立 STA、WebView2 environment、临时 profile、隐藏窗口，使用新增离线 `diagram-worker.html` 及全量 Mermaid bundle，保留所有图种。
- 单任务 8 秒硬期限、并发最多 2 个、输入/结果预算；返回、取消或超时均终止任务进程树并清理私有任务目录。没有按进程名扫描或终止用户浏览器。修复复用路径的 AssignProcessToJobObject 失败后遗留 suspended 子进程问题，并增加真实失败注入回归。
- 实际隐藏 WebView2 自动化验证通过：flowchart 11776 字节 / 450.9 ms，sequence 23173 字节 / 430.1 ms，class 18450 字节 / 413.3 ms。独立 renderer 中执行真正 `for(;;){}`，8.059 秒后任务回收；主测试进程正常返回。不是 Promise 超时模拟。
- 原始日志：`evidence/phase2-diagram-native.log`（exit=0）；真实输出：`evidence/phase2-diagram-flowchart.svg`、`phase2-diagram-sequence.svg`、`phase2-diagram-class.svg`。
- 复现：将当前 `cmd/desktop` 构建为临时 desktop.exe，其旁复制已随产品分发的 WebView2Loader.dll；`node node_modules/vite/bin/vite.js build --outDir <临时assets>`（在 web 目录）。设 `LUNITIDE_DIAGRAM_TEST_EXECUTABLE`、`LUNITIDE_DIAGRAM_TEST_ASSETS`、可选 `LUNITIDE_DIAGRAM_TEST_EVIDENCE`，执行 `go test ./internal/diagramrender -run '^TestNativeIsolatedDiagram' -count=1 -v`。此测试默认跳过，只在显式提供独立 worker 路径时运行。

## S02/C06：长任务撤销、组织切换和最终语音记录

- chat.runStream、realtime talk、ASR 生命周期接入 AcquireCapability，释放发生在异步任务结束。ASR 封装串行调用、取消在途上下文、关闭识别器且拒绝停用后返回的 final；暖机也继承该生命周期。
- 会议后台工作继续与页面超时解耦，同时持有独立能力 scope；停用后忽略迟到摘要/补转写结果，保存可重试状态及已完成内容，不将迟到成功当成有效新版本。
- 普通通话断开时已读取的 final 仍在独立 5 秒期限内尝试落盘；重新获取能力 scope、当前真实 session/project 与组织读锁后才写。语音 final、assistant 分片、checkpoint 写入和恢复读入口均复核当前组织绑定；组织切换等待写锁时使用统一 TryRLock 冲突出口，避免嵌套恢复读锁死锁。
- user final 的 `talk-final:` identity 与 assistant 一样永久去重；保留实际消息回查，旧 24h 记录过期、数据库重开后仍不重复。
- 超过单消息上限的最终文本按原顺序完整分片，首条 ID 为 handoff 引用；各片携带相同全文摘要，防止同 identity 以相同首段追加不同尾段。全部保存后才发 final ACK。前端按实际 session/role/status/连续 sequence 重建并校验原文，再复用首条 ID，模型输入按各消息契约分片，不重写用户历史。
- 定向 race：语音停用取消、会议迟到摘要、取消时已读 final 保存通过；长 final 的 user/assistant 两种分片、重放不重复、全文摘要冲突和跨组织迟到写拒绝通过。

## 当前验证与剩余

- 当前代码目标套件原始证据：`evidence/phase2-conversation-go.json` 与 `evidence/phase2-conversation-web.json`，Go 5 包、50 个测试/子测试事件通过，前端 9 文件 / 106 项通过，两条执行 exit=0；前端类型检查通过。全库集成套件由根代理统一运行。
- 本批 C01/C04/C05/B06/S02/C06 代码里程碑已完成。下一批接手根代理指派的 E09 执行边界与 S01 浏览器设置实际生效/CAS，不宣称尚未实施项已完成。
- 真机录音、付费 provider、物理断电及用户现场验收由用户执行；独立隐藏图表 worker 已完成上述本机自动化证据。

后续补充已完成：600 组真实音频故障/重放组合、0141 摘要输入版本与持久来源快照、长稿分页阅读与局部编辑、完整分段导航；详细范围和新回归计数见 [phase2-meetings-acceptance.md](phase2-meetings-acceptance.md)。WebView 的 COM getter 所有权、创建期间取消和幂等关闭修复及隐藏原生证据见 [phase2-webview-close.md](phase2-webview-close.md)。以上记录追加本轮进展，不改写较早批次计数。
# 最后会议收口索引

摘要来源 0141、600 音频组合、会议长稿分页与完整导出、聚合容量及停止错误恢复的最后结果统一见 [会议验收补充](phase2-meetings-acceptance.md)。最后补充页面回归为 35 / 35，全局类型检查通过；此前 51 项分页组合和本轮重跑存在重叠，不累加为独立用例总数。源码已冻结，最终六组同源门禁已全部通过，完整统计见 [最终验证](phase2-verification.md)；专项数字不与整库数字相加。
