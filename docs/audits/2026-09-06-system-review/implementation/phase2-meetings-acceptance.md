# 会议验收补充：音频组合、摘要来源与长稿界面

本记录补齐 PRD §6 / C04 / C05 中代码可验证的验收项。现场音频设备与真实 provider 验收仍由用户执行；以下测试不使用录音设备、付费模型或外网。

## 600 组音频故障与重放

正式测试：`internal/meetings/acceptance_audio_test.go` 的 `TestAudioAcceptance600FaultSchedules`。固定 seed 为 `20260906`；10 个故障族 × 三批音频的 6 种到达顺序 × 10 组不同样本长度/内容，共 **600 个独立 meeting 场景**。只有 1 个顶层测试、10 个故障子测试；没有把一次重复运行称作一种新的故障。

故障族：重复 ACK、提交后 ACK 丢失并重建服务、遗留未提交临时文件、真实文件 rename 失败、请求 PCM 与摘要不匹配、同 identity 不同内容、样本位置缺口、样本数不匹配、请求取消、已提交 PCM 损坏后明确拒绝。损坏测试由夹具恢复原始字节；产品不会自动修补无法验证的数据。

每组沿真实 Service、临时 SQLite、实际批次文件执行：乱序拒绝后补齐连续序号，核对持久 ACK 中的 capture / seq / sampleStart / sampleCount / digest / 累计时长；独立读取已提交文件检查真实 PCM、全局样本偏移和校验和；停止后重建 Service 再次重放旧 ACK，新音频明确拒绝。完整补转写读取全部 PCM，核对总字节数与整体 SHA-256，源分段 hash、内容 revision、终态均逐组校验。

一轮精确统计：2400 个已提交批次、800 次乱序拒绝、4560 次 ACK 重放、10 次实际 SQLite 关闭/重开、300 次补转写失败及明确缺口后的恢复。600 组全部丢弃 Service 内存状态重新读盘；这不是 600 次操作系统进程强杀或物理掉电测试。

- 普通：`go test ./internal/meetings -run '^TestAudioAcceptance600FaultSchedules$' -count=1 -v`，exit=0，测试用时 50.69 秒。
- race：相同命令增加 `-race`，exit=0，包用时 217.113 秒。
- 原始日志：`evidence/phase2-meetings-audio600.log`、`evidence/phase2-meetings-audio600-race.log`。此首次矩阵在 0141 合入前编译；合并后的最终门禁由根代理重跑。

1000 组 heartbeat / append / stop / summary / edit 屏障交错由根代理在 `acceptance_interleavings_test.go` 独立实现和记录。本测试不重复计入该 1000 组。其确定反例推动了心跳不制造内容 CAS 伪冲突、心跳时间不回退的 SQLite 修复；Stop 现在读回实际已提交行返回，包含并发心跳更新后的时长。

## 0141：当前摘要对应的真实输入

新增 `0141_meeting_summary_source.sql`，已登记 manifest、摘要和预期 schema 指纹，并完成旧库迁移回归。新增字段：`transcriptRevision`、`summarySourceRevision`、摘要源 SHA-256、生成前标题、完整清洗后原稿快照、人工编辑标记。

- 原稿内容真正改变时递增独立版本；心跳、摘要生成本身、规范化后相同原稿不递增。存储以旧值和新值对比维护版本。
- 生成成功的摘要、待办和对应输入版本 / 标题 / 原稿快照 / SHA 在同一个 meeting CAS 提交。记录的是窗口拆分前的完整清洗输入；后续 map/reduce 窗口由既有算法派生。
- 生成中人工改稿、模型失败、最终提交失败，都保留之前已提交摘要与它的来源。List 恢复中断状态前重新读取完整行，避免列表精简投影把来源快照清空。
- 修改原稿保留旧摘要，标记 `needs_summary`，界面与导出均说明旧摘要所用版本。手工改摘要/待办单独标明人工编辑，生成成功后重置标识。
- 旧库只给现存原稿建立当前版本；旧摘要来源为 0 / 空，明确显示未知，不把后来修改的原稿伪装成旧摘要输入。
- 当前表保留当前摘要及其来源，不宣称新增完整历史摘要库。已有旧摘要被重新生成覆盖后，其旧 digest 游标明确冲突，不能把不同来源页拼成一份。

源快照按需通过 `meetings.summary.source.get` 读取，每页最多 16,384 字，以 digest 固定版本；基本 get/list 不复制完整源正文。真实 SQLite 最大夹具包含 1,048,575 字（含非 BMP 字符和 JSON/HTML 需转义字符），64 页完整拼回相同原文，各 JSON 页小于 128 KiB，低于 4 MiB IPC 帧限制。

正式后端回归共 6 个顶层测试：服务 4 项、旧库迁移 1 项、完整 Engine 路由 / IPC 写帧 / 过期 digest 拒绝 1 项；服务及迁移的 5 项同时通过 race。日志为 `phase2-meetings-summary-source.log`、`phase2-meetings-summary-migration.log`、`phase2-meetings-summary-race.log`、`phase2-meetings-summary-engine.log`。

## 长稿阅读、局部编辑和来源查看

后端分页、Bridge 体积预算和导出修复由 computer_projects_assets 代理负责；前端由本代理接线。原先合法长稿 response 超过 4 MiB 的真实复现与最终后端证据见该代理交付。

- 长稿只按页显示和修改，编辑提交 `transcriptEdit`：原稿版本、Unicode 字符 offset、原页字符数和新页文本。不会把首屏预览作为全文提交；摘要单独修改的 payload 不含预览正文。
- 本页有改动时禁止翻页，必须等保存 ACK 后导航；超限文本完整留在编辑框并明确未保存，不截断。
- 版本冲突保留草稿，提供最新页只读比较；用户明确采用最新页后才换基准，不自动拿旧 offset 在新版本重放。
- 外层保存/导出先完成本页提交，再按新 revision 保存摘要/待办。长稿缩成一页时，即使摘要仍有未保存编辑，也使用提交后的新全文，不能恢复旧预览。
- 逐字稿明确人工修改后，重新生成摘要直接使用已保存版本，避免旧补转写 journal 冲突令重生成入口失效。实际补转写缺口仍阻止跳过缺失原文生成摘要。
- 原始分段独立分页，每页 10 个完整分段，以会议 revision 和读取上界固定一次浏览；能导航所有分段，不把固定前 10 条称为全集。
- 摘要来源显示当前/过期/未知及人工编辑标识，按需分页；会议切换和退出后的迟到来源、原稿、分段、保存回包不能污染新页面。

首轮完整分页前端定向验证：5 文件 / **51 项**通过，exit=0；全局类型检查通过。覆盖 MeetingPage 33 项、摘要来源组件 3 项、分页编辑组件 5 项、分段组件 2 项、会议 Bridge 8 项。原始日志：`evidence/phase2-meetings-pages-final.log`、`evidence/phase2-meetings-pages-typecheck.log`。全库集成由根代理统一执行。

## 最后容量边界与停止错误恢复

完整原稿以 **1 Mi 个 Unicode 字符**为支持上限，计数包括 NUL 和非 BMP 字符，不能以 SQLite 对 NUL 的文本长度行为漏计。实际 SQLite 写事务同时检查聚合容量；并发 append 不能各自通过检查后共同越界，超过上限明确返回 `MEETING_CAPACITY_LIMIT`，不裁去尾部或写入部分正文。

旧数据已经超过限制时，Stop 仍实际关闭录音并保存 `needs_summary` 和明确容量原因，不把停止失败伪装成继续录音。原始分段、音频保留，可分页查看；补转写可从音频起点重新构建。已经成功的跨度不重复执行，失败跨度保留为可重试状态；重开服务后再试仍保留完整 20,000 字识别结果，不按单段展示长度截去尾部。

正式 SQLite 回归：`TestMeetingAggregationExactLimitAndConcurrentAppendRefusesOverflow`、`TestMeetingLegacyOverflowPreservesRawDataAndCanRecoverFromAudio`、`TestMeetingCatchupDoesNotClipLongRecognizerResult`；另有 100,000 原始分段分页、最大合法 Unicode 原稿分页 / 局部编辑 / 完整 HTML 导出的真实 Handler 回归。后端定向最终记录：普通 meetings 9.855 秒、app 3.783 秒、SQLite 3.443 秒；race SQLite 42.965 秒、meetings 41.527 秒，均 exit=0。原始日志：`evidence/meeting-aggregation-tests.txt`、`evidence/meeting-aggregation-race.txt`。包耗时不是独立测试数量。

前端最后新增 2 项正式回归：容量错误后读取真实已提交的停止行，显示原稿与 `needs_summary`，不误触发摘要；页面退出后才到达的 Stop 失败，不再发起废弃页面的恢复请求。错误恢复请求、回包和 busy 清理使用页面代次 / meetingId / mounted 校验；已经请求的后端 Stop 仍可完成。原有 `adopt` 已有 meetingId 防护，本次补齐的是错误恢复和清理分支，不能把原有保护遗漏描述为新发现的数据覆盖事实。

最后页面单文件 **35 / 35** 通过，exit=0，7.51 秒；全局类型检查 exit=0。日志：`evidence/phase2-meetings-stop-recovery.log`、`evidence/phase2-meetings-stop-typecheck.log`。它是在前述 51 项后增加 2 项并重跑相关页面，不把两次的重叠测试累计成更多独立用例。最后统一门禁仍由根代理对冻结源码执行。
