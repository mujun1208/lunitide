# 本轮源码整改与复测交付回执

日期：2026-09-07。源码版本标识：0.4.70。**按用户要求，本轮不打包、不安装、不重启正在使用的 Lunitide**。当前安装实例不会自动获得这些源码修复；无需卸载或清空原数据。

## 交付内容与进度口径

本轮已完成可在当前工程中落实的故障修复、六类基础能力补强、自动测试和复测资料。聊天、三语音、会议、同事附件、记忆、电脑全局设置、管理页面/MCP、自动化及新增六类能力，统一收录在 **77 项 [用户复测清单](user-acceptance-checklist-20260907.md)** 中。

本轮中途报告的约 85% / 90% 是随追加范围调整的工作量估计，不是产品成功率。当前代码与自动检查收尾已完成；设备、账号、用户期望的内容质量仍需逐项验收。**无账号且永久免费可售票务报价、所有来源整片音画、任意复杂 Office 无损编辑等未实现范围仍明确保留，不能因此声称用户所有能力愿望或产品 4.9 分已经成立。**

- [六类能力实施方案](six-capability-upgrade-20260907.md)：结构化天气/检索、GUI、浏览器、文档内容、文件系统、调度的代码路径、免费条件、依赖与验收。
- [全部反馈整改台账](feedback-regressions-20260907.md)：本轮追加问题和组合链路修复。
- [独立复核](six-capability-independent-review-20260907.md)：未接通服务、未覆盖能力与费用条件。
- [PRD 1.11](../PRD-system-upgrade.md)：同步交付边界，历史 4.9 与旧版测试不充作当前证据。

## 最终验证结果

以下是本轮最终验证，不复用上一版本通过数字。Go 统计包括父测试与子测试，不能与前端数相加解释为功能数量。

| 检查 | 最终结果 | 证据与边界 |
| --- | --- | --- |
| Go 全量普通测试，CGO=0、并行4、每包25分钟上限 | **5,112 项测试/子测试通过，0失败，28跳过**；3,179个顶层通过，146包通过、2包无测试 | 2026-09-07 11:41:12–11:44:15（上海时区），进程退出0；[原始日志](evidence/feedback-20260907/feedback-go-verified.jsonl) |
| Go 语句覆盖率 | **54.9%**，通过原 **51%** 门槛 | [覆盖率文件](evidence/feedback-20260907/feedback-coverage-verified.out)；覆盖率不是正确率 |
| 前端全量 | **231 个文件，1,793项通过，0失败/跳过** | [测试报告](evidence/feedback-20260907/feedback-frontend-complete.json)；jsdom 不代替WebView2、真实音频和软件操作 |
| Bridge生成一致性、TypeScript、前端生产构建 | 通过 | [契约](evidence/feedback-20260907/feedback-bridge-final.log)、[类型与构建](evidence/feedback-20260907/feedback-web-build-final.log)；构建前端资源不是生成安装包 |
| 全量 Go vet、golangci-lint、Go build | 通过，lint **0 issues** | [vet](evidence/feedback-20260907/feedback-vet-final.log)、[lint](evidence/feedback-20260907/feedback-lint-verified.log)、[build](evidence/feedback-20260907/feedback-go-build-verified.log) |
| Go 可达漏洞扫描 | 退出0，未发现当前调用可达漏洞 | [扫描](evidence/feedback-20260907/feedback-govulncheck-verified.log)；另有1条所需模块中未被当前代码调用的漏洞记录，不能写成整个依赖生态无漏洞 |
| npm依赖扫描 | 总漏洞0，退出0 | [扫描](evidence/feedback-20260907/feedback-npm-audit-final.json) |
| 新改动并发/竞态测试 | 相关包/路径通过 | 各专项报告记录实际命令与日志；**本轮没有再跑整库90分钟race，因此不写整库race全通过** |
| 免费天气实际网络请求 | 合肥预报成功，约2,167ms，再次缓存命中 | [只读公共接口记录](evidence/feedback-20260907/weather-live-smoke.json)；这是单次环境实测，不是所有请求的延迟承诺。后续身份与304协议在隔离HTTP服务中验证 |
| 中文PDF实际生成、读取、渲染 | 2页、25,861 bytes，首末页显示及长文逐字核对通过 | [生成文件](evidence/documents-20260907/chinese-pdf-verification.pdf)、[第一页](evidence/documents-20260907/chinese-pdf-page-1.png)、[末页](evidence/documents-20260907/chinese-pdf-page-2.png) |
| 视频真实解码与聊天接线 | 本地合成短片通过真实FFmpeg解码，音轨、3帧、工具接线与拒图降级通过 | [视频报告](video-direct-content-20260907.md)；语音文字与模型使用隔离适配器，不代表真实ASR准确率已测 |
| Git差异空白检查 | 通过 | 不提交/推送用户工作树，不对未相关改动作归属声明 |

最终计数与28个跳过项的名称见 [机器可读汇总](evidence/feedback-20260907/final-verification-summary.json)。跳过项主要是实际浏览器/GUI、外部数据库、TTS/ASR/模型下载、公开MCP、Windows符号链接权限等，需要额外开关、依赖或账号；未偷偷记为通过。

前端生产主包约1,873kB（gzip约565kB），构建仍有体积提示，未发现构建错误。这不是首屏性能达标的证据，真实启动、内存与长时运行仍需观察。

## 专项并发证据

- [MCP连接](mcp-regressions-20260907.md)、[附件](attachments-regressions-20260907.md)、[子代理](subagent-parallel-progress-20260907.md)。
- [语音字幕](voice-transcript-regressions-20260907.md)、[任务装备/视频](task-routing-video-regressions-20260907.md)、[公开视频直链](video-direct-content-20260907.md)。
- [记忆/过程](memory-process-regressions-20260907.md)、[自动化故障](automation-regressions-20260907.md)、[取消/时区](automation-scheduling-capabilities-20260907.md)。
- [GUI与浏览器](gui-browser-capabilities-20260907.md)、[检索](search-reliability-20260907.md)、[天气HTTP](weather-http-compliance-20260907.md)、[文档与文件](document-filesystem-capabilities-20260907.md)。
- Tushare路径/查询凭据隔离、明文模板拒绝、无旧协议重试、重定向反射令牌不泄漏：[专项race输出](evidence/feedback-20260907/tushare-mcp-targeted.log)。未配置真实Tushare账号，不声称实际历史行情已经查得。

## 保留失败过程，说明复核后修改的依据

本轮第一次Go全量记录为5,100通过、9失败事件、28跳过，失败分布在app与SQLite两个包；[失败原始日志](evidence/feedback-20260907/feedback-go-complete.jsonl)保留。逐项核对后的修改包括：

1. 截图/电脑任务失败不能附加无关的播放建议，旧断言仍要求出现media.play；改为验证简短电脑失败且不含无关建议。
2. 最小工具集新加结构化天气，旧固定数量5不成立；改为精确验证六个允许工具，仍防额外写/控制能力混入。
3. 四种语音任务的旧断言要求与打字选择卡工具完全一致；现只排除语音user.ask，其余全部工具定义/schema仍严格相等，并核对天气能力。
4. 旧记忆测试要求回答结束前写session.last；现以channel阻塞后台worker，证明回答终态先到，释放后真实记忆仍保存，避免用sleep掩盖时序。
5. SQLite旧精确ID点击夹具只模拟按名称Invoke，没有新命中核对；改为两个同名元素选择B2实际坐标，错误命中时禁止点击。生产代码没有为测试放松目标验证。

前端全量早期也曾出现两处旧断言差异（语音选择卡、过程容器的size containment），修订到用户最终规则后才得到1,793项全通过。没有把早期失败运行删除或并入最终通过计数。

## 本轮测试事件必须保留

约10:54:37，原有HTML/Office测试曾直接写入并清理真实桌面四个固定名称：**点球大战.html、半年财报.xlsx、半年报告.docx、结构.pptx**。原夹具没有检查同名文件是否预先存在，也没有备份；事后四路径均不存在，**无法确认或排除此前的同名用户文件被覆盖**。没有可核实的恢复结果。

已改为每个测试独立临时桌面，生产的真实桌面功能保留；复跑核对没有再次改变真实桌面目录。完整事实、不确定性及后续核查范围见 [事件记录](desktop-test-isolation-20260907.md)。测试通过不能反向证明原文件不存在，也不能把隔离修复称作文件恢复。

## 未执行与仍需验证

- 未生成安装包、未安装升级、未执行真实联系人发消息、购票/支付、用户自动化重放或本机软件控制验收。系统声音录制、三语音实际准确率、首音频时间、字幕朗读同步、微信/汽水音乐等交由用户按清单验证。
- 上述Desktop夹具事件是已发生的例外，因此不概括成“本轮所有测试从未触及用户文件”。
- 字体许可已加入下次成品复制清单和必需布局门禁。**自动审批拒绝临时安装目录的许可证遗漏测试，未提供具体原因；该命令未执行。** 已做只读语法和清单核对，没有通过改换执行方式绕过拒绝，也没有因这项测试去打包。成品布局测试留到用户恢复打包阶段。
- 免费票务/行情边界、视频长片及分享页限制、扫描PDF/复杂文档保真等按实施报告保留，不能转写成已成功能力。

## 源码与证据追溯

核验时HEAD：`007aa2b783c32a378ea2c829e36c998e39672a48`。测试面向含未提交修改的当前工作树，不代表只测试该HEAD。

源码清单摘要：`cd901f61b74b06466df393736a3f429f0a67fe4102129941e99e3e509ea6cdaf`，覆盖2,854个源码、嵌入资源、契约、迁移、前端与发布脚本文件，不含文档和输出日志。详细范围与证据SHA-256见 [清单](evidence/feedback-20260907/source-evidence-manifest.json)。后续任何源码修改都应重新对应验证，不沿用本回执冒充新结果。
