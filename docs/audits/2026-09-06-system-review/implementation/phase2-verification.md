# 第二阶段代码交付与最终门禁

2026-09-06 最终状态：适用代码整改、完整未签名候选构建及独立制品核验已完成，**r2 六组统一门禁全部通过，代码输入摘要一致**。按本次约定完成逐模块交付评审，**工程交付等级为 4.9 / 5；系统实机评分留空、待用户验收**。4.9 是代码与自动化交付口径的评审等级，保留 24 模块、五维和原权重；它不由测试数量、通过率或覆盖率换算，也不代表已经测得全系统成功率或长期可靠性。

## 最终冻结源码与实际结果

数据来自 [最终机器可读汇总](evidence/phase2-verification-results.json)，生成于 2026-09-06 22:16:12（北京时间）；原始日志与六组回执位于 [r2 取证目录](evidence/final-gates-20260906-r2/)。汇总器只校验记录并计算实际计数，不自动赋分；JSON 中的评分空值与上述独立工程评审属于不同记录。

| 项目 | 最终实际结果 | 原始证据 |
| --- | --- | --- |
| Go 全量 | 4,766 个测试/子测试 PASS，其中 2,970 个顶层测试；143 个包 PASS，123 个包实际有 Test 事件；0 失败，28 个测试 SKIP，另有 2 个包 SKIP。 | [Go 日志](evidence/final-gates-20260906-r2/go-tests.jsonl)、[回执](evidence/final-gates-20260906-r2/go-receipt.json) |
| 覆盖率 | 53.2%，通过既定 51% 门槛；不是功能覆盖比例。 | [覆盖率汇总](evidence/final-gates-20260906-r2/coverage.txt) |
| 全库 race | 4,758 个测试/子测试 PASS，其中 2,962 个顶层测试；122 个包 PASS；0 失败，28 个测试 SKIP，另有 23 个包 SKIP；实际命令 exit 0。 | [race 日志](evidence/final-gates-20260906-r2/go-race.jsonl)、[回执](evidence/final-gates-20260906-r2/race-receipt.json) |
| 前端 | 219 个测试文件、1,668 个测试 PASS，0 失败、0 跳过；Bridge 契约、类型检查和生产构建均 exit 0。 | [测试 JSON](evidence/final-gates-20260906-r2/web-tests.json)、[回执](evidence/final-gates-20260906-r2/web-receipt.json) |
| 静态检查 | Go vet、build、golangci-lint 全部 exit 0。 | [静态回执](evidence/final-gates-20260906-r2/static-receipt.json) |
| 依赖扫描 | npm audit 总告警 0；govulncheck exit 0、调用可达告警 0，另有 1 条模块层提示，见下文。 | [依赖回执](evidence/final-gates-20260906-r2/security-receipt.json)、[npm 原始结果](evidence/final-gates-20260906-r2/npm-audit.json)、[Go 扫描原始结果](evidence/final-gates-20260906-r2/govulncheck.jsonl) |
| 发布工具 | Windows PowerShell 5.1、PowerShell 7 工具回归及打包排除回归均 exit 0。 | [发布工具回执](evidence/final-gates-20260906-r2/release-tools-receipt.json) |
| 完整候选 | 未签名 r2 NSIS 安装器 24,986,006 字节；四程序、PE、布局、清单及独立摘要核验通过，构建和独立验证均 exit 0。 | [候选记录](evidence/phase2-candidate.json)、[独立核验日志](evidence/phase2-candidate-verify-r2.log) |

基线 commit 为 `09978597dc026bfa40ab8d690d64f9bcaef476f3`，交付的是其上的冻结工作区变更。2,758 个实际代码输入文件的摘要为 `e578444c4485345e8aa55dcc2b71e551e2abda0d07627e23cc149e62d50fef17`；六组回执、当前代码和候选均一致。候选构建时包含文档的完整树摘要为 `e1e3db16e6cd4e8e7abd1623f70840a92b0e67da39f7864a5f008ad8375a1e22`。构建后补充的文档和日志单独交付，不能冒充候选原始文档快照。

## 跳过与提示的准确边界

普通和 race 各有同一组 28 个测试跳过，完整包名/测试名清单保存在机器汇总的 `go.skipped_tests` 与 `full_library_race.skipped_tests`，具体原因在上述原始日志。它们没有计入 PASS：

| 数量 | 本轮跳过范围及原因 |
| --- | --- |
| 6 | 隐藏浏览器生命周期/代理、独立图形 worker、3 项 WebView 原生集成未在默认套件启用专用路径/opt-in。这些功能此前的独立隐藏原生专项证据另列，不把本轮 SKIP 改写为 PASS。 |
| 5 | 4 项实际桌面准确性未启用 opt-in；窗口列表/焦点测试没有可用的带标题前台窗口。 |
| 3 | datadir、worker、workspace 的符号链接测试缺少 Windows 创建符号链接权限；此前实际 junction 和文件句柄边界专项另有证据。 |
| 2 | 实际 MySQL 驱动/服务未配置专用测试 DSN。 |
| 12 | 真实 TTS、语音识别/refiner、网络模型下载、火山握手所需服务开关/模型/凭据未配置；OneCore 场景不足 2 个已安装声音。 |

普通套件与 race 的 8 个顶层测试差值来自 `cmd/desktop/watchdog_test.go` 和 `singleton_windows_test.go` 的现有 `!race` 构建约束：它们在普通套件 PASS，在 race 构建中不收集，不能声明它们通过 race。包级 SKIP 与逐测试 SKIP 是不同统计，完整包级清单同样保存在最终 JSON。

Go 扫描保留 `GO-2026-5932` 对 `golang.org/x/crypto v0.56.0` 的模块层提示，本次扫描没有报告调用可达路径；不能据此写成“依赖绝无漏洞”。前端构建仍有大于 500 kB 的产物体积提示，构建 exit 0；[原始 stderr](evidence/final-gates-20260906-r2/web-build.stderr.log) 保留该提示。

## 历史失败与最终结果分开

首次统一验证发现一条旧 MRO 正向夹具把任意文本引用当作受控来源；已改为真实组织、知识文件和受控手册，并保留任意引用失败及其他约束不被清除的断言。首次 race 为同步该夹具修正而主动终止。旧失败/终止日志完整保留于 [r1 取证目录](evidence/final-gates-20260906/)，不计入最终 r2 的通过数量，也不把主动终止写成一次完整 race 通过。

更早的探索运行、第一阶段结果和原审计反例保持原记录。整改前 2.7 / 5、第一阶段历史暂评 3.6 / 5 保留各自量表和时间边界，不由本轮 4.9 工程交付等级改写。

## 模块专项与完整交付

- 打字、三语音、会议、独立图表 worker：[对话与会议](phase2-conversation.md)、[持久队列](phase2-queue.md)、[会议容量与来源验收](phase2-meetings-acceptance.md)。
- 电脑控制、真实计划运行、项目文件交付、资产和备份恢复：[项目与执行](phase2-projects.md)；MRO 大结果及分页：[MRO 分页](phase2-mro-pagination.md)。
- 权限 epoch、技能、能力包、MCP、专家历史：[扩展与专家](phase2-extensions.md)。
- 知识来源实际字节查新、当前版本和解析预算：[知识库](phase2-knowledge.md)。
- 同事送达、数据库写确认、配置版本、任务账本与诊断：[系统与数据](phase2-system.md)。
- 浏览器实际代理与文件边界：[执行边界](phase2-browser-execution.md)、[WebView 关闭](phase2-webview-close.md)。
- 完整未签名候选、四程序清单及源码绑定：[发布实施](phase2-release.md)；需求与实现的准确范围：[PRD 复核](phase2-prd-calibration.md)。

此前独立完成的 1,000 组真实 Service/SQLite 状态交错、600 组音频故障组合已经随本次冻结源码重新通过普通与 race 全量套件。它们是正式测试内的组合场景，包含更多子操作；不把这些子操作当作独立故障种类，也不在 4,766 / 4,758 个测试事件之外重复加总。

## 用户接手的真实环境验收

真实麦克风/系统声与三条默认语音供应商、真实模型/MCP/数据库和多设备同事连接、Win32/UIA/ConPTY 与设备矩阵、发布者签名及干净安装/更新/卸载、完整 72 小时混合业务负载，以及 14 天、至少 3,000 次核心有效操作的受控观察仍为 pending。短时工具观察仅用于调试，不能抵扣正式时长或业务样本。所需代码、隔离回归、候选和验收工具已经交付；未进行的实测不能由工程分推导为已通过。用户验收分工与范围以 [支持矩阵](support-matrix.csv) 和主 PRD 为准。
