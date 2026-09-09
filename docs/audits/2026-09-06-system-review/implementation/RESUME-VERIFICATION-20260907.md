# 恢复开发与集中复核记录

更新：2026-09-08 10:06（北京时间）。基线 `7f5db68`，当前源码未提交、未打包，未替换正在运行的客户端。本记录持续追加真实结果，不以测试数量换算用户验收通过率。

## 已完成的本轮收口

- Office图片、四类PPT原生图表与内嵌工作簿、Excel本表图表及源范围编辑，已有版本继续可编辑。
- 选择性合并Word简单域和Excel公式缓存；按分节角色匹配页眉页脚，保留原公式、输入值、样式和非目标部件。成功另存新版本，原版本不动。
- 原生更新的旧版本冲突提前拒绝；丢回执重试不再次执行原生转换；检查记录中断可恢复；迟到取消回执不覆盖完成状态。
- 几何检查和最多两轮有限平移修复；旋转、组合、继承坐标及无法可靠解析的对象不盲修。
- 存储配额、容量预留/租约、固定批次清理和引用保护；字体声明与本机字体可用性检查。
- 技能三入口统一草稿、完整资源树、聊天内试用、技能中心发布及长描述创建专家。
- 黑白主题下的Office小窗口弹窗滚动、容量布局；实际PPT图表默认布局和背景可读性。
- 修正前端全量测试包装脚本误把 `run` 当作 Vitest 过滤词，以及共享生成预算在末次补齐扣减时的误判；据此完成 2026-09-08 的全量 Go/Vitest、类型和 Bridge 复跑。

## 已取得的测试证据

日志路径相对于仓库根目录。

| 范围 | 结果 | 证据 |
|---|---|---|
| 全量后端 | 130个包通过，23个包无测试，0失败 | `release/out/lunitide-resume-go-all.log` |
| 全量前端 | 257文件、1931项通过 | `release/out/lunitide-resume-web-all.log` |
| 后续Office前端回归 | 15文件、87项通过，包含新增迟到取消测试 | `release/out/lunitide-office-final-web-tests.log` |
| 最后内容/服务/转换器回归 | 三包通过 | `release/out/lunitide-office-final-content-tests.log` |
| SQLite全量 | 通过，124.352秒 | `release/out/lunitide-resume-storage-tests.log` |
| 原生发布与恢复 | 真实临时SQLite：检查中断、回执重试、冲突、取消及原版本保留通过 | `release/out/lunitide-native-publication-retry-tests.log` |
| 类型与Bridge生成 | 通过，Office当前30个方法 | `release/out/lunitide-office-final-typecheck.log`、`lunitide-office-final-bridge.log` |
| 技能生产组件浏览器 | 黑白×两窗口，4组合，0页面异常 | `release/out/skill-workflow-ui-evidence/report.json` |
| Office主体浏览器 | 黑白×四窗口，8组合，0页面异常 | `release/out/office-studio-ui-evidence/report.json` |
| Office新增弹窗浏览器 | 图表/Excel源范围/存储/图片×黑白×两窗口，16组合，0页面异常；实际底部按钮可到达 | `release/out/office-extensions-ui-evidence/report.json` |
| 实际LibreOffice图片/图表 | PPT四页、Excel图表打开/转PDF通过；PPT四页逐页查看 | `release/out/lunitide-office-native-objects-tests.log` |
| 实际Word目录缓存 | 原生更新、选择性合并、非目标保真通过 | `release/out/lunitide-office-native-word-tests.log` |
| 实际Excel重算 | SUM/跨表计算、公式错误分支和错误缓存拒绝通过 | `release/out/lunitide-office-resume-native-tests.log`（该早期日志中的Word失败已由上一行修复复测） |
| 长Word实际更新 | 一份30章合成稿生成32页；目录/章节/长编号全量文字核对，非目标部件保持；页1/2/3/17/32视觉检查 | `release/out/lunitide-native-long-document-tests.log`、`release/out/office-native-resume-evidence/long-toc-report.json` |
| 2026-09-08 前端全量复跑 | 258文件、1942项通过 | `release/out/lunitide-20260908-web-all-fixed.log` |
| 2026-09-08 前端类型与Bridge生成 | 通过 | `release/out/lunitide-20260908-web-typecheck.log`、`release/out/lunitide-20260908-web-bridge.log` |
| 2026-09-08 共享生成预算回归 | 并发预算末次补扣通过 | `release/out/lunitide-20260908-app-budget.log` |
| 2026-09-08 后端全量复跑 | 脱沙箱复核通过，包含 `internal/storage/sqlite`、`internal/toolruntime`、`internal/voice` 等尾段包 | `release/out/lunitide-20260908-go-all-unsandboxed.log` |
| 2026-09-08 实时语音真链路 | 打开 `LUNITIDE_VOICE_E2E=1` 后，两组真实识别/精修 E2E 通过 | `release/out/lunitide-20260908-voice-live-verbose.log` |
| 2026-09-08 Microsoft Office真机核对 | Word/PowerPoint/Excel 实际打开样例核对通过；当前机器未装 WPS | `docs/audits/2026-09-06-system-review/evidence/office-desktop-verification-20260908.md` |

浏览器测试运行生产组件但使用合成API，没有接真实用户数据库或云模型。原生文件测试使用独立LibreOffice profile，样例全部由测试生成。长文单次转换13.52秒，不等于P95或每种模板都达到相同时延。

## 2026-09-08 继续复核追加

- Microsoft Word：实际打开 `release/out/office-native-resume-evidence/long-toc-merged.docx`，窗口显示共32页，目录页可见 `Chapter-01` 至 `Chapter-30`；辅助文本还能读到末尾 `原文末尾标记-30`。
- Microsoft PowerPoint：实际打开 `release/out/office-native-resume-evidence/objects.pptx`，逐页核对4页图表；柱状、条形、折线、饼图均可见，折线页图例 `Revenue` 可见。
- Microsoft Excel：实际打开 `release/out/office-native-resume-evidence/objects.xlsx`，工作表可见 `Category / Value`、`Alpha=12.5`、`Beta=25`，图表标题 `Verified worksheet chart` 和图例 `Revenue` 可见。
- 当前机器未发现 WPS（注册表和常见安装目录均未找到 `wps.exe` / `et.exe` / `wpp.exe`），因此 WPS 兼容本次不能记为通过。
- 语音方面，这次不是跳过测试：显式设置 `LUNITIDE_VOICE_E2E=1` 并复用隔离目录后，真实云端识别和精修返回完整 turn 文本；四段样例的 finish 额外耗时为 293–489ms。

## 尚未全部通过的验收

1. 真实三语音整套麦克风/系统环回、口头或按钮打断、任务完成朗读及整轮体感时延。今天已补跑云端识别/精修 E2E，不等于 GUI 月伴整套真机验收。
2. 使用当前源码开发版本的真实模型、附件/截图、MCP连接、桌面应用任务和用户外部账号联调。
3. WPS以及复杂公司模板兼容；Microsoft Office 的 Word/PowerPoint/Excel 样例真机打开与内容核对已通过，但不能外推为任意模板或 WPS 兼容。
4. 完整复杂原稿黄金集、实际字体替换/字宽/文字溢出及完整性能分位数；一份32页合成文档不是30份复杂黄金模板。
5. 真实断电/物理满盘/不同文件系统属于环境故障验收；不会为测试破坏用户数据或机器。

此前尝试Microsoft Word真机验证时，用户按Esc停止了电脑操作，该次只到打开入口，未打开测试文档，不能记录为兼容通过。用户现已要求继续全部测试；2026-09-08 已补完 Microsoft Office 样例真机复核，后续结果继续在本记录追加。

不在本次执行范围：用户暂停的30项未安装社区技能，不自动安装；不打包、不清空旧数据、不覆盖或重启当前客户端。天气、票务、股票和第三方模型的账号/费用/来源限制以总复测清单为准，不承诺免费取得不存在的数据源。

总入口：[本轮全部问题与复测步骤](user-acceptance-checklist-20260907.md)。办公：[实施报告](../../../design/office-studio/IMPLEMENTATION-REPORT.md)、[支持矩阵](../../../design/office-studio/SUPPORT-MATRIX.md)、[O01–O40步骤](../../../design/office-studio/RETEST.md)。
