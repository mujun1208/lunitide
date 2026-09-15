# 五分制、测试矩阵与发布验收

日期：2026-09-15。适用[PRD](02-upgrade-prd.md)、[合同](06-integration-contracts-plan.md)与三个子计划。以下目标不是实测结果。

## 1. 评分对象与结果

评分对象是“需求到交付的实施就绪度”，不是模型智能排行榜，也不是现在产品体验的测评分。每维1分，按下列五项检查每项0.2；审查评分有判断成分，不伪装实验精度。

| 维度 | 五项检查 | 旧版 | R2文档交付时 |
|---|---|---:|---:|
| 需求闭环 | 自动记忆；OCR自选；媒体/工具；原始15维覆盖；边界不夸大 | 0.8 | 1.0 |
| 现码与可行性 | 真实入口；存储约束；消费者；平台接线；真实Paddle/媒体切片 | 0.6 | 0.8 |
| 合同确定性 | 来源/身份；状态/幂等；分页/字节；生命周期；跨文档一致 | 0.6 | 1.0 |
| 安全与恢复 | 作用域；低信任注入；遗忘边界；备份/降级；真实故障演练 | 0.8 | 0.8 |
| 效果与可验证性 | 固定样本；指标准确；总成本；UI/语音回归；实测全部门 | 0.6 | 0.8 |
| 合计 | 25项×0.2 | **3.4/5** | **4.4/5** |

R2未给5分的三个缺口：Windows完整Paddle与媒体切片尚未实现验证；升级/遗忘/故障恢复尚未实演；真实质量、总Token、语音和用户体验尚未实测。写再多文档也不能填补这0.6分。实现03–06且以下门全部通过后，才可在本次明确范围内验收5/5。

“5/5”不表示世界最先进、零未知漏洞、任意电脑都最快或用户从不需要操作。它表示本PRD范围、受支持设备和冻结测试下全部约定完成。尚未运行测试必须标NOT_RUN；缺模型/授权样本标INCOMPLETE，不计通过。

## 2. 阻断门与证据格式

- 任一越权、敏感内容向未授权远端发送、遗忘复活、错误资产播放、旧epoch误触发下一首、假verified成功、破坏原数据，都是阻断项；不可用平均分抵消。
- 每个结果记录requirementId/testId/commit/worktreeDiffDigest/OS/CPU/RAM/runtime/model/config/dataset digest/command/exitCode/result/outputArtifact。
- dirty工作树必须记录diff digest；仅写HEAD不足以复现实验。绝不能把用户其他修改认领成本方案成果。
- 报告同时保存逐例结果与汇总；失败、取消、超时也计入分母，不只统计成功样本。
- 基线和新版同硬件、同模型、同配置、同样本；冷启动与热启动分开，p50/p95/最大值和样本数齐全。

## 3. 记忆安全与功能新增回归（M-R）

| ID / 拟新增测试名 | 输入与操作 | 必须结果 |
|---|---|---|
| M-R01 TestMemorySourceAssistantIDRejected | 用appendAssistantTurn的ID伪装user source | SOURCE_INVALID；fact=0，无远端调用 |
| M-R02 TestMemorySourceUTF8Span | 中英混合、CRLF、中文边界、裁剪后的digest | 仅真实规范化原文合法边界通过；中间字节/裁剪hash拒绝 |
| M-R03 TestMemoryScopeKindCollision | 同scope ID分别project/expert、两主体 | 查询/索引/导出/审阅/缓存均不互通 |
| M-R04 TestMemoryPromptUntrustedSlots | working/import/偏好带忽略审批和伪role分隔符 | 最终provider messages无自由文本system升权；工具审批仍生效 |
| M-R05 TestMemoryQueueHighWatermarkDrains | 10000 queued且再有100个source | 消费继续、游标保留、低水位恢复；最终100%处理或明确拒绝 |
| M-R06 TestMemoryUndoAfterCorrection | 自动保存A后用户改为B，再撤销A | UNDO_CONFLICT且B不变，旧A不复活 |
| M-R07 TestMemoryImportCannotResurrectTombstone | 导出A→忘记A→导入旧archive | A不恢复active，保留冲突/墓碑报告 |
| M-R08 TestMemoryExportConcurrentSnapshot | 导出期间另一事务更正/删除 | archive所有section/count/digest对应同revision，能校验 |
| M-R09 TestMemoryForgetBoundaryWithCheckpoint | 原聊天/summary含canary，保存后forget | Memory各派生面无canary，不重新捕获；原聊天保留且UI明确边界 |
| M-R10 TestMemoryHistoricalRecallAcrossGeneration | 上海→杭州，建代/激活/回退各查过去及现在 | 当前杭州、当时上海，已忘记版本永不返回 |
| M-R11 TestMemoryModePolicy | off/manual/auto及remoteAssist关闭 | off无新记忆；manual无自动横幅；auto本地保存；未授权0外发 |
| M-R12 TestMemoryBudgetReservation | 并发提取/向量/重试、usage缺失、UTC跨日 | 事务预占不超日额，缺usage不记免费，新日重置独立账本 |

固定最小集沿用PRD：负例300、敏感150、明确正例300、时间冲突150、召回查询300。负例必须含引用文章、角色扮演、第三人事实、“以后不要保存我的位置”、临时任务与混合陈述，不仅“你好”。样本分开发集/冻结验收集，各自保留digest，不在同一验收集反复调规则后宣传泛化。

门槛：固定问候/查询/纯命令误存0；敏感与跨scope违规0；explicit stable捕获recall≥95%、kind+scope同时正确≥90%；明确纠正当前版本100%；召回Hit@5不低于旧基线，时间纠正子集目标min(100%,旧基线+10个百分点)。未知/无答案问题不得凭记忆编造，拒答正确率单列。样本0违规不等于真实世界0风险。

## 4. OCR新增回归（O-R）

| ID / 测试名 | 输入与操作 | 必须结果 |
|---|---|---|
| O-R01 TestOCRScopePolicySnapshot | 两主体不同绑定，识别中途切策略/组织 | 本次快照目标不变或明确取消；不借另一主体凭据 |
| O-R02 TestOfficeOCRCachePackRevision | 同SHA，安装/更新pack或gate变化 | 旧OCR缓存不命中，新结果带实际pipeline证据 |
| O-R03 TestPDFRenderBounded | 100页、超20MP压缩页、取消 | 同时页数≤2，分配前拒超限，无全部PNG常驻 |
| O-R04 TestOCRArtifactRangeAndGC | 同digest两run、读取中TTL到期、损坏blob | 有ref/lease不删；仅独立OCR root清理；坏blob明确错误 |
| O-R05 TestOCRFenceRejectsStaleRunner | runner A租约失效，B接管，A晚到激活 | A提交拒绝；current唯一，不丢旧可用版本 |
| O-R06 TestOCRBridgeSizeBudget | 单页8MiB结构、20页run.get | metadata JSON≤512KiB，正文ref分块，UTF-8重组正确 |
| O-R07 TestOCRLegacyConsumers | Image/PDF/Document/ReadDocument、Office/KB/modelcatalog | 全部scoped路由，旧shape可解码，超限正确partial |
| O-R08 TestOCRUpdateFailureKeepsReady | 旧版ready，新版自测失败/卸载busy | 旧current仍可用；operation failed不伪装整个pack坏了 |

真实包必须在无Python/无模型缓存的Windows x64 CPU干净机完成断网两阶段识别、取消、进程树回收和签名失败测试。记录真实ABI/指令集/系统/依赖版本；未通过则install入口disabled，不用fake worker、WSL或Docker冒充内置能力。

盲测200页：简单40、扫描40、复杂结构60、高难30、应失败/警告30；每页双人标注与仲裁。CER/WER/阅读顺序/表格单元格F1、漏页、空白误报、成功率、耗时、RSS齐全。

自动路由判定：简单页默认Windows；若要Paddle接管，CER相对改善≥5%且p95≤3×Windows。复杂页满足CER相对改善≥10%或可比较结构F1改善≥5个百分点，且成功率不下降。Windows不输出该结构时不可虚构F1=0：改用Paddle绝对结构F1≥0.85（首发目标）且CER不劣于Windows；记录该指标不属于直接对比。基线CER=0时禁止除0，要求保持0并依其他适用指标决策。未达门不妨碍手动高级识别，但不自动路由。

## 5. 媒体、活动与音频（A-R / V-R）

| ID | 场景 | 验收 |
|---|---|---|
| A-R01 | 只发送媒体键/未核验SMTC | passed=false/uncertain=true；中文不称已播放 |
| A-R02 | A轨道旧epoch ended在B播放后到达 | 不推进B，不产生多余next |
| A-R03 | 两窗口争lease、Renderer伪owner | 唯一owner，Host身份不可伪造；过期拒绝 |
| A-R04 | 切会话/设置/项目/办公页 | 媒体元素不卸载、进度连续、命令不重复 |
| A-R05 | 完整页面刷新/Engine重启 | 恢复paused，绝不自动出声；未核验命令uncertain |
| A-R06 | 100次切歌、30分钟合法视频、4GiB sparse资源 | 句柄回落；Range内存不随文件大小线性增长；sparse只测IO不当可解码视频 |
| A-R07 | ticket过期/错误asset/文件被替换/越scope | fail-closed，不联网兜底，不泄露路径/ticket |
| A-R08 | 活动>200条、同时间戳、多session、分页时更新 | 稳定cursor，无创建历史漏行/重复；状态可刷新 |
| A-R09 | media多次失败/重试映射同tool | 历史可get/list，root去重，一次动作一条主活动 |
| A-R10 | capability撤销/急停在accepted后dispatch前 | 不执行；already dispatched不声称撤回；不自动重试next |
| V-R01 | 播放音乐时TTS开始/被打断 | 真实pause后TTS；符合focus token条件才恢复 |
| V-R02 | 电影对白时启动麦克风 | owned先pause核验；失败不开始录音；external提示手动处理 |
| V-R03 | 录音期间手动切歌/暂停/换设备/退出 | 不恢复旧轨道，无隐藏自动发声 |
| V-R04 | 现有TTS/ASR配置、打断、重连 | 原模型/音色/凭据不改，无重复音频/串音 |

真实WebView2垂直切片必须早于美化：Host选择→登记→Range→audio/video播放→seek→取消/关闭释放。200/206/416、单range、多range拒绝、bounded IStream、导航时deferral结束全覆盖。播放成功证据只能来自实际播放事件或匹配目标SMTC回读。

## 6. 效率、Token、办公与写代码回归

- 端到端固定任务至少30例：10办公、10编码、10跨会话续接。记录任务成功、人工纠正次数、工具调用数、首token/首音/总耗时与全链模型tokens。
- 办公例覆盖扫描PDF提取→表格核对→文档引用；不以OCR返回文本当文档最终正确。编码例覆盖读取项目约束→修改→测试→续接，不把主观“更聪明”评分替代测试结果。
- full path成本含后台记忆、query embedding、重算、重试；warm/cold分开。只有成功率不降、质量可比且总成本确降，才能宣称节省；仅注入下降必须标“注入tokens下降”。
- 语音每配置30轮以上，记录speech stop→ASR final→首LLM token→首可听sample，区分网络与本地时延。现voiceTiming只有40条内存ring且标记基点未必等于实际speech stop，评测需加明确timestamp导出，不直接套注释里的1500ms当已实测结论。
- 回归门：固定无模型/本地case无新增远端调用；voice/TTS原功能测试全通过；新后台功能启用相对关闭的首音p95恶化≤100ms作为首发目标。超过门先关闭后台辅助，不能降低语音体验来换记忆。

## 7. UI五分目标的具体证据

同一用户任务至少5名目标用户，每人完成：查某条记忆/纠正/忘记；安装取消/失败恢复；本地视频播放/切页面/切歌；查看工具失败并恢复。先写任务与成功定义，再收结果。

目标：关键任务完成率≥90%，未出现因文案把发送误认为成功；满意度中位数≥4/5；新布局无输入遮挡。该小样本仅为首发可用性门，不宣称统计代表所有用户。审美不靠自评“满分”，保留匿名反馈与修改记录。

技术门：亮暗主题、680/960/1440宽度、100%/150%/200%缩放，键盘焦点返回、Esc/读屏、reduced motion；axe serious/critical为0，截图中无明显重叠/截断。字体颜色复用现有tokens，禁止局部第二主题。

## 8. 发布与降级顺序

1. X0基线与迁移前一致备份；独立修媒体假成功。
2. 0160–0164表及schema注册（若占号整体顺延），生产scope/source/lease装配。
3. canonical影子写→基础读→纠正/遗忘/导入导出闭环；通过后才auto capture。
4. hybrid/时间查询→generation，独立gate可关；保留canonical reader。
5. OCR Windows完整pack门通过才install；硬件profile盲测通过才auto route。
6. 媒体Windows切片→App生命周期/音频焦点→Activity分页→视觉验收。
7. 内部测试主体先开；无阻断缺陷后人工批准下一批，不能仅按定时器自动扩量。真实故障/备份恢复与全量测试通过后验收5分。

每一步提交有requirement→test→commit→artifact映射。新功能包尚未创建时不能运行其目录后把“no tests to run”当PASS；`-run`命令需检查目标测试确实存在且被执行。

## 9. 本轮实际验证记录

2026-09-15当前dirty工作树、HEAD 5970012d：

```powershell
go test -count=1 ./internal/memoryapp ./internal/m8app ./internal/contextapp ./internal/compactionapp ./internal/ocrapp ./internal/doctext ./internal/toolruntime ./internal/winexec ./internal/tts ./internal/voice
npm --prefix web run verify:bridge
```

均exit0，10个Go包通过，Bridge一致性通过。这只验证现有基线；上述M/O/A/V新增测试、模型盲测、用户测试、升级后性能均未执行。全量`go test ./...`和web build未在本轮运行，不声称全项目无失败。
