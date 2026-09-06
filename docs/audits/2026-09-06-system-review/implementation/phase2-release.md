# Q04 发布工具实施与验收边界

2026-09-06 最终记录。完整未签名 r2 NSIS 候选已构建并独立验证，六组同源工程门禁全部通过。工程交付按约定逐模块评审为 4.9 / 5，依据见 [最终验证汇总](phase2-verification.md)；该等级不由测试数量折算。实际安装/卸载、发布者签名、外部发布、用户数据库维护及长期 GUI 业务工作负载仍未执行，系统实机评分保持空值。

## 实际完成

1. `Build-Release.ps1` 构建四个自有程序：Desktop、Engine、maintenance、purge helper；正式签名模式执行发布者签名检查，本次开发候选显式禁用签名命令。维护程序纳入布局白名单、SHA256 清单、安装完整性及发布者证书/可信时间戳检查。文档解析复用 Engine 的 `--doctext-worker`，不虚构第五个程序。
2. 每次构建使用 release 目录下全新的空输出目录；旧候选不删除。构建前后及打包后比较 Git commit 与实际源码文件摘要；生成 `SOURCE-CANDIDATE.json`，纳入制品 SHA256 清单。正式签名构建要求干净且已提交的源码。构建 renderer 时明确切入仓库根，支持从其他工作目录调用；仅 verify 已生成契约，不在候选构建期间运行生成器，以避免混入接口变更及 Windows 换行重写。签名命令通过环境变量接收文件名，路径内容不会被直接拼入 PowerShell 代码。
3. CI 在构建后单独记录候选安装包与源码摘要，安装测试后再次核对再上传。`Verify-Release.ps1` 必须提供此前独立记录的摘要，不能只核对可一起替换的文件和同目录清单；源码记录也重新计算内部摘要。未签名排练明确输出 rehearsal，不再称 complete release acceptance。
4. 修复 `verify-install-directory.ps1` 对已存在目标过早返回 33 的问题：现在先检查目标及内部重解析点，再允许升级。程序路径与实际用户数据目录相同、包含它或位于其中都拒绝，避免默认卸载误删数据库。安装/卸载检查拒绝目录链接；活动中的离线维护不会被强制终止。
5. 修复 `Test-Install.ps1` 递归删除调用者任意 `TestRoot` 的问题：改为在该父目录内创建一次性独占子目录，清理前检查绝对边界与目录链接。原有真实安装测试仍仅允许无现有安装/注册/数据的专用账户。后台安装程序隐藏控制台，保留产品交互窗口供实际验收。
6. `Test-AcceptanceMatrix.ps1` 使用制品的源码记录和独立摘要，不再把验收机器当前 checkout 的 HEAD 冒充制品 commit；记录签名/排练类型，拒绝覆盖旧证据。`manual-acceptance.template.json` 所有真机用例初始 pending、score=null。
7. 新增只读 `Test-Soak.ps1`：核对已安装清单后，对用户已启动的 Desktop/Engine 进程采样，记录 PID 变化/缺失、私有内存、工作集、句柄、线程和增长量。工具默认 8 小时只可用于调试观察；正式验收必须配置并完成 72 小时混合业务负载，另完成 14 天、至少 3,000 次核心有效操作的受控观察。它不生成业务负载、不控制桌面、不读对话/凭据；observed-stable 仅表示资源观察指标，不能代替上述业务验收。

## 正式隔离验证

- `release/Test-ReleaseTools.ps1`：全部 PowerShell 源语法、父目录越界、实际 NTFS 目录 junction（目标及内部）、用户数据目录重叠拒绝；临时 Git 仓库稳定摘要通过/源码改变拒绝/摘要篡改拒绝；四程序清单与 Engine worker 在 bootstrap 之前的入口；真实构建 maintenance AMD64 PE 并仅运行 `-h`，在准备数据目录前退出。
- `release/Test-OmniExcluded.ps1`：新增程序清单后，仍按正确原因拒绝 Omni/Comni payload，排除测试通过。
- 原始输出：[PowerShell 7 工具回归](../evidence/phase2-release-tools.log)、[Windows PowerShell 5.1 工具回归](../evidence/phase2-release-tools-windowsps.log)、[排除回归](../evidence/phase2-release-exclusions.log)。双宿主复核补齐 .NET Framework 的 NUL Split 重载差异，防止 5.1 将尾部空项当作源码根。这些是较早的专项日志，仅证明所列隔离测试；最终同源发布工具回执及整包构建证据见下一节，真实安装仍未执行。

## 最终 r2 候选与独立验证

[候选机器记录](evidence/phase2-candidate.json) 的状态为 `verified-development-candidate`；[完整构建日志](evidence/phase2-candidate-build-r2.log) 和 [独立核验日志](evidence/phase2-candidate-verify-r2.log) 均对应实际 exit 0。独立核验包括四程序 AMD64 PE、布局、清单、此前独立记录的安装包/源码摘要；没有运行安装器。

| 字段 | 实际记录 |
| --- | --- |
| 候选目录 | `release/out/upgrade-20260906-r2` |
| 安装器 | `Lunitide-Setup-0.4.67-x64.exe`，24,986,006 字节 |
| 安装器 SHA-256 | `b69b0e97c2d9c03e0e08e273103ffc02c8df21776ed965e2a38cc5994db6771d` |
| stage 清单 SHA-256 | `50779b3525a98a591c29beaf6d3b3e8ed4ef163abd6485d69b4d82a16fb079a8` |
| 基线 commit | `09978597dc026bfa40ab8d690d64f9bcaef476f3`，包含该基线上的冻结工作区改造 |
| 2,758 个代码输入的 SHA-256 | `e578444c4485345e8aa55dcc2b71e551e2abda0d07627e23cc149e62d50fef17`，与最终六组门禁一致 |
| 含构建时文档的完整树 SHA-256 | `e1e3db16e6cd4e8e7abd1623f70840a92b0e67da39f7864a5f008ad8375a1e22` |
| 工具链 | Go 1.26.6 windows/amd64；Node 24.16.0 |
| 签名与安装 | `unsigned-development`，签名命令显式禁用；`installed=false`，真实发布与机器验收 pending |

候选内 `SOURCE-CANDIDATE.json` 保存构建时文档快照；构建结束后补充的本报告、评分和日志单独交付。代码摘要一致不能被误写为后来编辑的文档也逐字等同于候选快照。

最终发布工具门禁：[Windows PowerShell 5.1](evidence/final-gates-20260906-r2/release-tools-ps5.log)、[PowerShell 7](evidence/final-gates-20260906-r2/release-tools-ps7.log)、[排除回归](evidence/final-gates-20260906-r2/release-exclusions.log)、[同源回执](evidence/final-gates-20260906-r2/release-tools-receipt.json)。其他五组结果及跳过明细以 [最终机器汇总](evidence/phase2-verification-results.json) 为准。远端 CI 配置已提供，本次未把本机通过说成远端流水线实际运行过。

r1 候选记录、r1 旧 MRO 夹具失败及主动停止的 race 原始日志继续保留，见 [旧候选记录](evidence/phase2-candidate-r1.json) 和 [旧门禁目录](evidence/final-gates-20260906/)。它们不计入最终 r2 通过统计，也不删除后改称从未失败。

## 文档更正与用户待验

本次实际验收记录使用 [已绑定候选的表单](user-acceptance.json)，其中长期观察已明确72小时混合负载、14天及至少3000次核心操作。仓库通用 `manual-acceptance.template.json` 的8小时描述与工具默认值只用于短时调试，不能替代本次正式验收门槛；表单本身也不自动授权发布。

| 旧表述/易混淆项 | 当前准确口径 |
| --- | --- |
| Q04 仅为未来发布流水线任务 | 候选绑定、maintenance 打包和安全检查已实现；完整未签名 r2 候选构建及独立验证 exit 0；真实发布者签名和四格安装矩阵仍待用户执行。 |
| 验收记录 commit 可取测试机器 HEAD | 必须来自被测候选的 SOURCE-CANDIDATE.json，且与独立记录的源码摘要和安装包摘要绑定。 |
| 无 NuGet verifier 时开发包警告后继续 | 实现会下载固定版本/固定 hash 的 nuget verifier；签名校验失败停止。release/README 已同步。 |
| PowerShell 验证脚本通过等于签名安装验收通过 | 脚本回归通过和完整未签名 NSIS 构建/独立验证分别有记录；真实签名链、Win10/Win11 与 WebView2 有/无安装矩阵仍待验。 |
| 短时观察或 observed-stable 等于正式长稳验收 | 短时运行仅用于工具调试；正式验收需完整 72 小时混合业务负载和 14 天、至少 3,000 次核心有效操作，核对业务成功率/延迟等主 PRD 门槛。工具标签不能代替业务证据或实机评分。 |
| 初始审计中的未实施描述 | 属于基线历史证据，保留原文；当前状态以 implementation 实施记录和正式回归为准，由主文档统一整合。 |

交付与用户验收顺序：冻结源码（正式签名需提交） → 在全新输出目录构建候选 → 独立保存安装包、源码和 stage 清单摘要 → Verify-Release → 在一次性 Windows 环境跑安装矩阵与实际业务链路 → 完成 72 小时混合业务负载 → 完成 14 天、至少 3,000 次核心有效操作的受控观察并核对退出清理 → 填写人工模板、评审失败项。短时工具观察仅用于调试，不抵扣正式时长或操作样本。具体参数见 release/README.md。真实签名、第三方模型/设备、安装与长时观察仍由用户负责，不能靠本文件自动授予发布资格。

源码前后摘要能识别构建期间的普通修改，但不是对受控构建环境的证明，也不是可复现构建认证；拥有本机修改权的构建进程及上游工具链仍需受发布流程约束。
