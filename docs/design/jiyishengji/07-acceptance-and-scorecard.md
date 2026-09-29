# 五分制、测试矩阵与发布验收

日期：2026-09-15。适用[PRD](02-upgrade-prd.md)、[合同](06-integration-contracts-plan.md)、[总实施计划](11-master-implementation-plan.md)与三个子计划。以下目标不是实测结果；逐项状态以[需求追踪矩阵](12-requirements-traceability.md)为准。

## 1. 评分对象与结果

评分对象是“需求到交付的实施就绪度”，不是模型智能排行榜，也不是现在产品体验的测评分。每维1分，按下列五项检查每项0.2；审查评分有判断成分，不伪装实验精度。

| 维度 | 五项检查 | 旧版 | R2原自评 | R3整改前复核 |
|---|---|---:|---:|---:|
| 需求闭环 | 自动记忆；OCR自选；媒体/工具；原始15维覆盖；逐项追踪 | 0.8 | 1.0 | 0.8 |
| 现码与可行性 | 真实入口；存储约束；消费者；平台接线；真实Paddle/媒体切片 | 0.6 | 0.8 | 0.6 |
| 合同确定性 | 来源/身份；状态/幂等；分页/字节；生命周期；跨文档/UI一致 | 0.6 | 1.0 | 0.6 |
| 安全与恢复 | 作用域；低信任注入；遗忘边界；备份/降级；真实故障演练 | 0.8 | 0.8 | 0.8 |
| 效果与可验证性 | 固定样本；指标准确；总成本；UI/语音回归；实测全部门 | 0.6 | 0.8 | 0.8 |
| 合计 | 25项×0.2 | **3.4/5** | **4.4/5** | **3.6/5** |

R3 复核下调的原因见 10：模式/scope、PP-OCR、Windows probe、UI信息架构、迁移分配和追踪矩阵在 R2 中并未闭合。文档修正可恢复“可执行性”，但不能替代实现证据；实现 03–06/11 且 12 的全部 P0/P1 行达到 `VERIFIED` 后，才可在本次明确范围内验收 5/5。

“5/5”不表示世界最先进、零未知漏洞、任意电脑都最快或用户从不需要操作。它表示本PRD范围、受支持设备和冻结测试下全部约定完成。尚未运行测试必须标NOT_RUN；缺模型/授权样本标INCOMPLETE，不计通过。

## 2. 阻断门与证据格式

- 任一越权、敏感内容向未授权远端发送、遗忘复活、错误资产播放、旧epoch误触发下一首、假verified成功、破坏原数据，都是阻断项；不可用平均分抵消。
- P00 `BASE-E` 只记录 11 P00/12 §1.2 冻结的基线字段：HEAD、trackedDiffDigest、untrackedManifestRelativePath/untrackedManifestDigest、migration allocation/digest、环境、采集命令、开始/结束时间、exitCode 与 outputDigest；它发生在业务实现前，不包含虚构的 requirement/test 结果。模块 run manifest 与 P14 五份 release evidence 才记录 requirementId/testId、`testedSourceHead`、sourceTrackedDiffDigest、release untracked manifest、OS/CPU/RAM/runtime、model/config/dataset digest、command/exitCode/testCount、逐例 result 与 outputArtifact digest。
- dirty工作树必须记录diff digest；仅写HEAD不足以复现实验。绝不能把用户其他修改认领成本方案成果。
- 报告同时保存逐例结果与汇总；失败、取消、超时也计入分母，不只统计成功样本。
- 基线和新版同硬件、同模型、同配置、同样本；冷启动与热启动分开，p50/p95/最大值和样本数齐全。

## 3. 记忆安全与功能新增回归（M-R）

`M-R/O-R/A-R/V-R/U-R` 是跨测试层的稳定 scenario ID，不是待创建的顶层 Go/Vitest symbol。斜线后的英文仅为助记名，也不得创建成 `Test...` 同义 wrapper。实现时把 ID 作为 table-driven subtest/case 的精确 title，挂在 12 对应 requirement 行列出的任一 canonical `path#symbol` 下；P14 同时核验 parent anchor 与 scenario ID 均实际 run/pass。

| ID / 场景助记名（非 symbol） | 输入与操作 | 必须结果 |
|---|---|---|
| M-R01 / source_assistant_id_rejected | 用appendAssistantTurn的ID伪装user source | SOURCE_INVALID；fact=0，无远端调用 |
| M-R02 / source_utf8_span | 中英混合、CRLF、中文边界、裁剪后的digest | 仅真实规范化原文合法边界通过；中间字节/裁剪hash拒绝 |
| M-R03 / scope_kind_collision | 同scope ID分别project/expert、两主体 | 查询/索引/导出/审阅/缓存均不互通 |
| M-R04 / prompt_untrusted_slots | working/import/偏好带忽略审批和伪role分隔符 | 最终provider messages无自由文本system升权；工具审批仍生效 |
| M-R05 / queue_high_watermark_drains | 10000 queued且再有100个source | 消费继续、游标保留、低水位恢复；最终100%处理或明确拒绝 |
| M-R06 / undo_after_correction | 自动保存A后用户改为B，再撤销A | UNDO_CONFLICT且B不变，旧A不复活 |
| M-R07 / import_cannot_resurrect_tombstone | 导出A→忘记A→导入旧archive | A不恢复active，保留冲突/墓碑报告 |
| M-R08 / export_concurrent_snapshot | 导出期间另一事务更正/删除 | archive所有section/count/digest对应同revision，能校验 |
| M-R09 / forget_boundary_with_checkpoint | 原聊天/summary含canary，保存后forget | Memory各派生面无canary，不重新捕获；原聊天保留且UI明确边界 |
| M-R10 / historical_recall_across_generation | 上海→杭州，建代/激活/回退各查过去及现在 | 当前杭州、当时上海，已忘记版本永不返回 |
| M-R11 / mode_policy | off/manual/auto及remoteAssist关闭 | off无新记忆；manual无自动横幅；auto本地保存；未授权0外发 |
| M-R12 / budget_reservation | 并发提取/向量/重试、usage缺失、UTC跨日 | 事务预占不超日额，缺usage不记免费，新日重置独立账本 |
| M-R13 / scope_toggles_gate_all_paths | 依次关闭个人、项目、全部；制造缓存和后台job | 对应 scope 的捕获/显式保存/working/job/search/recall/inject均为0，另一scope不受影响；缓存立即失效；管理/忘记仍可用 |
| M-R14 / legacy_settings_migration | 0159中组合memory_enabled/capture_mode/省略新字段旧payload | 0→off；其余保留auto/manual；scope默认true；pre-off模式可恢复；旧payload不覆盖scope；CAS冲突不丢草稿 |
| M-R15 / manual_canonical_create | manual下明确“记住”/主动新增、重复幂等键、off/scope关 | 只由memory.item.create写canonical；无自动candidate/banner；重放单写；不同payload冲突；禁用时稳定拒绝 |

固定最小集沿用PRD：负例300、敏感150、明确正例300、时间冲突150、召回查询300。负例必须含引用文章、角色扮演、第三人事实、“以后不要保存我的位置”、临时任务与混合陈述，不仅“你好”。样本分开发集/冻结验收集，各自保留digest，不在同一验收集反复调规则后宣传泛化。

门槛：固定问候/查询/纯命令误存0；敏感与跨scope违规0；explicit stable捕获recall≥95%、kind+scope同时正确≥90%；明确纠正当前版本100%；召回Hit@5不低于旧基线，时间纠正子集目标min(100%,旧基线+10个百分点)。未知/无答案问题不得凭记忆编造，拒答正确率单列。样本0违规不等于真实世界0风险。

## 4. OCR新增回归（O-R）

| ID / 场景助记名（非 symbol） | 输入与操作 | 必须结果 |
|---|---|---|
| O-R01 / scope_policy_snapshot | 两主体不同绑定，识别中途切策略/组织 | 本次快照目标不变或明确取消；不借另一主体凭据 |
| O-R02 / office_cache_pack_revision | 同SHA，安装/更新pack或gate变化 | 旧OCR缓存不命中，新结果带实际pipeline证据 |
| O-R03 / pdf_render_bounded | 100页、超20MP压缩页、取消 | 同时页数≤2，分配前拒超限，无全部PNG常驻 |
| O-R04 / artifact_range_and_gc | 同digest两run、读取中TTL到期、损坏blob | 有ref/lease不删；仅独立OCR root清理；坏blob明确错误 |
| O-R05 / fence_rejects_stale_runner | runner A租约失效，B接管，A晚到激活 | A提交拒绝；current唯一，不丢旧可用版本 |
| O-R06 / bridge_size_budget | 单页8MiB结构、20页run.get | metadata JSON≤512KiB，正文ref分块，UTF-8重组正确 |
| O-R07 / legacy_consumers | Image/PDF/Document/ReadDocument、Office/KB/modelcatalog | 全部scoped路由，旧shape可解码，超限正确partial |
| O-R08 / update_failure_keeps_ready | 旧版ready，新版自测失败/卸载busy | 旧current仍可用；operation failed不伪装整个pack坏了 |
| O-R09 / windows_probe_states | 有语言包、缺语言包、WinRT初始化失败、固定图失败、超时、非Windows | 分别为ready/language_unavailable/initialization_failed/sample_failed/timed_out/unsupported_os；仅ready可用，UI文字真实且不凭GOOS显示勾选 |
| O-R10 / legacy_ppocr_never_executes | 登记含ppocr.exe/onnx目录并保存旧localEngine=ppocr | 返回registered_unwired/available=false；effective engine为Windows；Paddle state不变；ppocr调用数0；Renderer无绝对路径 |
| O-R11 / provider_failure_isolation | provider.list失败但ocr.routing.get/ocr.pack.get成功 | 核心OCR状态正常呈现；只有高级provider区报错并可单独重试 |
| O-R12 / install_requires_verified_profile | 无catalog/profile、legacy marker存在、签名profile通过三种情况 | 前两者NO_VERIFIED_RUNTIME_PROFILE且mutation/operation=0；只有签名profile通过后进入preflight |

真实包必须在无Python/无模型缓存的Windows x64 CPU干净机完成断网两阶段识别、取消、进程树回收和签名失败测试。记录真实ABI/指令集/系统/依赖版本；未通过则install入口disabled，不用fake worker、WSL或Docker冒充内置能力。

盲测200页：简单40、扫描40、复杂结构60、高难30、应失败/警告30；每页双人标注与仲裁。CER/WER/阅读顺序/表格单元格F1、漏页、空白误报、成功率、耗时、RSS齐全。

自动路由判定：简单页默认Windows；若要Paddle接管，CER相对改善≥5%且p95≤3×Windows。复杂页满足CER相对改善≥10%或可比较结构F1改善≥5个百分点，且成功率不下降。Windows不输出该结构时不可虚构F1=0：改用Paddle绝对结构F1≥0.85（首发目标）且CER不劣于Windows；记录该指标不属于直接对比。基线CER=0时禁止除0，要求保持0并依其他适用指标决策。未达门不妨碍手动高级识别，但不自动路由。

## 5. 媒体、活动与音频（A-R / V-R）

| ID | 场景 | 验收 |
|---|---|---|
| A-R01 | 只发送媒体键/未核验SMTC | passed=false/uncertain=true；中文不称已播放 |
| A-R02 | A轨道旧epoch ended在B播放后到达 | 不推进B，不产生多余next |
| A-R03 | 两窗口争lease、Renderer伪owner | 唯一owner，Host身份不可伪造；过期拒绝 |
| A-R04 | 播放后切会话/设置/项目/办公页/媒体中心 | App根player/store不卸载；MediaCenter与MiniPlayer读取同一snapshot，进度连续、命令不重复 |
| A-R05 | 完整页面刷新/Engine重启 | 恢复paused，绝不自动出声；未核验命令uncertain |
| A-R06 | 100次切歌、30分钟合法视频、4GiB sparse资源 | 句柄回落；Range内存不随文件大小线性增长；sparse只测IO不当可解码视频 |
| A-R07 | ticket过期/错误asset/文件被替换/越scope | fail-closed，不联网兜底，不泄露路径/ticket |
| A-R08 | 活动>200条、同时间戳、多session、分页时更新 | 稳定cursor，无创建历史漏行/重复；状态可刷新 |
| A-R09 | media多次失败/重试映射同tool | 历史可get/list，root去重，一次动作一条主活动 |
| A-R10 | capability撤销/急停在accepted后dispatch前 | 不执行；already dispatched不声称撤回；不自动重试next |
| A-R11 | 无会话、播放、离开媒体中心、pause、返回媒体中心 | 无会话MiniPlayer隐藏；离开后显示；pause保留会话与MiniPlayer；媒体中心复用同一进度且不重复装载 |
| A-R12 | owned/external MiniPlayer 点击 close，stop/release 成功、失败或无法核验 | owned 成功后结束会话、释放 ticket/lease 并隐藏；owned 失败先安全暂停/撤销本产品输出，保留错误与恢复入口，不得只做 CSS 隐藏或继续由本产品发声。external 成功且 SMTC 回读停止后才隐藏；失败/uncertain 保留“未确认”与外部播放器处理提示，不宣称已停止，也不承诺 Lunitide 能强制外部应用静音 |
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

自动化UI合同：

| ID | 场景 | 必须结果 |
|---|---|---|
| U-R01 | 默认桌面首页截图与computed style | 主内容为纯黑、侧栏接近黑色；极光是首页单一主视觉，不出现大面积深蓝灰后台面板 |
| U-R02 | 依次打开首页、记忆、OCR、媒体中心 | 极光只存在首页与媒体中心主视觉；记忆/OCR使用纯黑、轻分层和现有theme tokens |
| U-R03 | 检查左侧办公分组与活动入口 | “媒体中心”和“办公工作台”同级；侧栏没有“活动中心”；顶部状态按钮可打开活动popover/详情 |
| U-R04 | 设置→智能能力→记忆，及聊天“打开记忆” | overview恰好两卡；记忆详情为状态+搜索+单列列表；设置抽屉仅有自动/仅手动/关闭与个人/项目开关；聊天深链直达详情；无五页签或底层参数 |
| U-R05 | 自动模式提交稳定偏好，再提交“你好”/天气查询 | 稳定偏好出现已记住toast并可撤销；问候/天气不新增记忆且不弹确认框 |
| U-R06 | 无冲突、制造冲突、打开记忆更多菜单 | 无冲突时无待审阅常驻区；冲突时仅轻提示；来源/纠正/忘记均可达且焦点可返回 |
| U-R07 | 打开智能能力与OCR高级信息 | routing页无OCR；overview OCR卡进入详情；默认只突出“文字识别·自动”只读状态和复杂文档增强；语言/provider/legacy/pipeline/版本/回退默认折叠 |
| U-R08 | Paddle gate未通过、通过后安装/取消/失败 | gate未通过安装禁用并说明原因；通过后状态真实可恢复，失败不影响Windows OCR，不用模拟完成冒充ready |
| U-R09 | 媒体中心播放后跨页、pause、close | 独立媒体中心呈现完整控制；跨页显示条件式MiniPlayer；pause保留、close结束；同一snapshot无跳变 |
| U-R10 | 390×844、1024×768、1440×900，100%/150%/200%缩放及reduced motion | 无横向滚动/遮挡/截断主操作；移动MiniPlayer不挡输入；焦点可见；极光与装饰动画按偏好减弱 |
| U-R11 | Windows probe 六种机器状态的用户文案 | 严格覆盖 O-R09 的 `ready/unsupported_os/initialization_failed/language_unavailable/sample_failed/timed_out`；仅 `ready` 显示可用，`language_unavailable` 给系统语言安装指引，初始化/样例/超时失败可重试，非 Windows 明确不可用；展示层可归组但不得改写 wire enum，也不得出现假勾选 |
| U-R12 | provider列表失败/恢复 | OCR核心状态和Paddle gate仍可见；错误只在高级provider区；独立重试不重置OCR草稿 |
| U-R13 | legacy PP-OCR目录存在 | 仅在高级区显示“已登记，尚未接线”；无PP-OCR选择/安装成功/ready；不显示绝对路径 |
| U-R14 | 无签名Paddle runtime profile | 增强按钮disabled并说明当前版本尚未发布已验证运行时；点击/键盘均不发送mutation；Demo只能写“示例/模拟” |
| U-R15 | 智能能力overview加载与焦点 | 未进入详情时project/memory/provider/OCR调用均0；两卡进入/返回焦点正确；<680px单列且设置导航可达 |

同一用户任务至少5名目标用户，每人完成：查某条记忆/纠正/忘记；确认自动记忆无需逐次询问且理解问候/天气不保存；查看OCR自动策略并安装取消/失败恢复；在独立媒体中心播放本地视频、切页面、pause、close；从顶部状态入口查看工具失败并恢复。先写任务与成功定义，再收结果。

目标：关键任务完成率≥90%，未出现因文案把发送误认为成功；满意度中位数≥4/5；首屏无需解释即可找到媒体中心、记忆模式和OCR增强入口；新布局无输入遮挡。该小样本仅为首发可用性门，不宣称统计代表所有用户。审美不靠自评“满分”，保留匿名反馈与修改记录。

技术门：亮暗主题、390×844/1024×768/1440×900视口、100%/150%/200%缩放，键盘焦点返回、Esc/读屏、reduced motion；axe serious/critical为0，截图中无明显重叠/截断。字体颜色复用现有tokens，禁止局部第二主题。截图回归必须至少覆盖首页纯黑+极光、记忆极简、OCR极简、媒体中心、跨页MiniPlayer、顶部活动popover六个状态。

## 8. 发布与降级顺序

1. X0 记录 HEAD/diff digest、生成唯一 migration allocation、完成迁移前一致备份；独立修媒体假成功。
2. 按 allocation 注册 Memory/OCR/Media 表及 schema，生产 scope/source/lease 装配；编号冻结后遇冲突直接阻断，不由子线顺延。
3. canonical影子写→基础读→纠正/遗忘/导入导出闭环；通过后才auto capture。
4. hybrid/时间查询→generation，独立gate可关；保留canonical reader。
5. OCR Windows完整pack门通过才install；硬件profile盲测通过才auto route。
6. 媒体Windows切片→App生命周期/音频焦点→Activity分页→视觉验收。
7. 内部测试主体先开；无阻断缺陷后人工批准下一批，不能仅按定时器自动扩量。真实故障/备份恢复与全量测试通过后验收5分。

每一步提交有requirement→test→commit→artifact映射。新功能包尚未创建时不能运行其目录后把“no tests to run”当PASS；`-run`命令需检查目标测试确实存在且被执行。

## 9. 历史验证记录与当前状态

以下命令曾在 2026-09-15 的旧 dirty 工作树、HEAD `5970012d` 执行；当前代码审计 HEAD 已为 `b1d58b0d`，所以结果状态是 `STALE`，不是当前通过证据：

```powershell
go test -count=1 ./internal/memoryapp ./internal/m8app ./internal/contextapp ./internal/compactionapp ./internal/ocrapp ./internal/doctext ./internal/toolruntime ./internal/winexec ./internal/tts ./internal/voice
npm --prefix web run verify:bridge
```

旧记录均 exit 0。X0 必须按 11 P00 重新生成 `evidence/baseline/<UTC>/manifest.json`：固定包含 HEAD、tracked diff digest、确定性 untracked manifest 的路径与 digest、migration allocation/digest、环境、采集命令、开始/结束时间、exitCode 和 outputDigest；不写尚未运行的 requirement/test/逐例结果。测试逐例证据只进入模块 run manifest 和 P14 release JSON。在此之前当前状态为 NOT_RUN。上述 M/O/A/V/U 新增测试、模型盲测、用户测试、升级后性能均未执行，不声称全项目无失败。

本轮文档收口在当次 HEAD `62711f3d` 的 dirty 工作树另做一次**非发布 sanity check**，开始/结束 HEAD 一致；它不是 X0/P00 证据：

```powershell
go test -count=1 ./internal/memoryapp ./internal/m8app ./internal/contextapp ./internal/compactionapp ./internal/ocrapp ./internal/doctext ./internal/toolruntime ./internal/winexec ./internal/tts ./internal/voice
npm --prefix web test -- src/settings/SettingsPage.test.tsx src/settings/OCRRouting.test.tsx src/m8/PersonalIntelligencePage.test.tsx src/memory/MemoryOpsPanel.test.tsx
npm --prefix web run verify:bridge
node docs/design/jiyishengji/ui-demo/demo.test.cjs
node docs/design/jiyishengji/ui-demo/browser-check.cjs
```

结果：10 个 Go 包通过；4 个现有前端测试文件、30 项测试通过；Bridge check 通过；Demo 13/13 通过；Edge 覆盖 5 个目的页、6 种宽度，无 JavaScript 错误或外部请求。该记录没有 P00 生成的 worktree digest、untracked manifest 和环境 artifact，因此 12 仍保持 0 `IMPLEMENTED`、0 `VERIFIED`。
