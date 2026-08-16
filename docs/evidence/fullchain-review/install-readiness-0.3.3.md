# M0-M9 待办清零 + 0.3.3 安装就绪收口证据

生成时间：2026-08-16（UTC+8）· 平台：windows/amd64 · go1.26.5 · 结论：**PASS（-race 沿用既有基线 skipped：本机无 gcc/cgo）**

## 1. 待办清零矩阵（本 session 处理项）

| # | 事项 | 处理 | 结果 |
|---|------|------|------|
| 1 | M4 Scope Seal 未收口 | ADR-018（能力级最终形态）+ m4-closure bundle + M4/01+04 状态回写 | 已关闭 |
| 2 | M3 RC 退出证据未绑定 | m3-rc bundle（build digest、69 迁移 checksum、tokenizer 冻结身份、回归、fail-closed 对账、故障注入、go vet）+ M3/01+02+04 回写"正式退出" | 已关闭 |
| 3 | M9.5 MC-06 a11y 仅真机 | CompanionStage.a11y.test.tsx 9 用例（键盘零鼠标全流程/aria-live 双区域/四态可辨/焦点进出归还）；修复舞台级空格快捷键被 `[role]` 选择器吞掉的缺陷；M9.5/04 回写 | 自动化切片已落地；reduce-motion/200% 缩放/读屏实测留真机清单 |
| 4 | A 类文档状态滞后 | trace-writeback.ps1（修复 PowerShell 单元素数组解包缺陷后执行，applied=7）+ finalize-05.ps1（applied=3）：ADR-011 Accepted（v1.0.0 · 2026-08-16）、M0-M9.5 里程碑状态列、fusion trace 8 行、CloudRunner、M9-REQ-ADR-GATE-001 解封、Section 10 总体状态与 footer 口径 | 已回写 |
| 5 | 安装产物不含当日修复 | VERSION 0.3.2→0.3.3；Build-Release.ps1 -AllowUnsignedDevelopment 完整构建 | 已产出 |

## 2. 0.3.3 发行构建验证

- 命令：`release/Build-Release.ps1 -AllowUnsignedDevelopment`（NSIS 安装器含）
- 产物：`release/out/Lunitide-Setup-0.3.3-x64.exe`（8,806,302 B）+ `release/out/Lunitide-0.3.3-x64/`
- 布局：Lunitide.exe（9,284,096 B）/ lunitide-engine.exe（16,667,648 B）/ purge-user-data.exe（2,036,736 B）/ WebView2Loader.dll（SHA-256 pinned）/ web/dist（index + 970KB JS + 109KB CSS）/ licenses / lunitide-icon.ico / SHA256SUMS.txt
- 校验：Verify-PE（4 exe/dll AMD64 + 导出表）✓ · Verify-Layout（含 manifest 复核）✓ ×2 · NSIS /WX 编译零警告 ✓
- 版本冒烟：`Lunitide.exe -version` → `0.3.3` ✓
- 运行时验收：`Test-Runtime.ps1` → "Installed WebView2 runtime startup and clean shutdown acceptance passed"（WebView2 初始文档加载、唯一 engine 子进程、WM_CLOSE 干净退出 exit 0、无残留子进程）✓
- 未签名披露：本构建为 -AllowUnsignedDevelopment 测试候选（无 LUNITIDE_SIGN_COMMAND / 持钉证书），发布级签名门禁不变

## 3. 全量回归

| 门禁 | 命令 | 结果 |
|------|------|------|
| 静态 | `go vet ./...` | 0 警告 |
| Go 单元/集成 | `go test ./...` | 全部 ok，fail=0 |
| 前端 | `npx vitest run` | 34 文件 / 251 测试全过（含 CompanionStage.a11y 9 用例） |
| 前端构建 | `npm run build`（bridge 生成 + tsc + vite 334 模块） | ✓ |
| 竞态 | `go test -race ./...` | skipped：本机无 gcc（CGO 不可用），与 m3-rc/fullchain-review/m9/m9.5 全部既往基线一致，门禁未降低 |

## 4. 残留（显式边界，非阻塞）

1. 真机手动验收清单不变：M9.5 SAPI 真机 e2e（音质/P95/打断 ≤100ms/Windows N 降级演练）、reduce-motion/200% 缩放/读屏实测。
2. 正式外部验收与 released 状态未发生（05 追踪文档口径已同步）。
3. 发布级 Authenticode 签名需提供 LUNITIDE_SIGN_COMMAND + LUNITIDE_SIGNER_THUMBPRINT 后重跑构建。
