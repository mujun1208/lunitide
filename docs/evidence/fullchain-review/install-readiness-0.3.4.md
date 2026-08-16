# 0.3.4 安装就绪收口证据（UI 六项整改回归）

生成时间：2026-08-16（UTC+8）· 平台：windows/amd64 · go1.26.5 · 结论：**PASS（-race 沿用既有基线 skipped：本机无 gcc/cgo）**

## 1. 本版本包含的六项 UI/UX 整改

| # | 用户诉求 | 落地 | 结果 |
|---|---------|------|------|
| 1 | 资产管理点不进去 | AssetManagerPanel/ExpertCenterPage useEffect 同步 bridge 异常 try/catch 收口（jsdom/非 WebView2 环境不再崩溃） | 已修复 |
| 2 | 项目与对话可折叠 | LaunchSidebar 增加"对话/项目"可折叠分组（ARIA expand/collapse） | 已上线 |
| 3 | 项目清单→项目管理，按 UI 设计重做 | 迁移 0070（A-N 字段+生命周期 created→active→closed→reopened）、projectapp 服务、project.update/publish/close/reopen Bridge API、ProjectPage 七列清单+发布/关闭/打开门禁+幂等重试 | 已上线 |
| 4 | 项目工作台按设计重做 + 开发平台 | SessionPage 工作台重构，工作区"开发平台"视图（文件树+变更历史），移除 计划管理/记忆/本体/审批/运行/调研 前台页签 | 已上线 |
| 5 | 月伴对话改版 | 仅首页入口；全屏纯月亮舞台（无字幕栏无按钮）；忽闪白光 halo（idle/listening 常闪，speaking 音量驱动）；免提自动对话循环；ttsPlayer AudioContext 解锁修复无声；默认温柔女声（zh female 优先）；MoonSphere 四态可点 | 已上线 |
| 6 | 设置/侧边栏信息架构收口 | 技能/专家/资产/组织归入设置；记忆/本体/审批/计划管理为项目作用域工作台页签（设计文档口径）；移除"运行"（单 Agent 前台）与"调研与证据"前台；设置页去除"规划中"占位 | 已上线 |

## 2. 0.3.4 发行构建验证

- 命令：`release/Build-Release.ps1 -AllowUnsignedDevelopment`（NSIS 安装器含，/WX 零警告）
- 产物：`release/out/Lunitide-Setup-0.3.4-x64.exe`（8,818,143 B）+ `release/out/Lunitide-0.3.4-x64/`
- 布局：Lunitide.exe（9,284,096 B）/ lunitide-engine.exe（16,724,992 B）/ purge-user-data.exe（2,036,736 B）/ WebView2Loader.dll / web/dist / licenses / lunitide-icon.ico / SHA256SUMS.txt / verify+stop 脚本
- 版本冒烟：`Lunitide.exe -version` → `0.3.4` ✓
- 未签名披露：同 0.3.3，-AllowUnsignedDevelopment 测试候选，发布级 Authenticode 签名门禁不变

## 3. 全量回归（本 session 最终轮）

| 门禁 | 命令 | 结果 |
|------|------|------|
| 静态 | `go vet ./...` | 0 警告 |
| Go 单元/集成 | `go test ./... -count=1` | 全部 ok，fail=0 |
| 前端 | `npx vitest run` | 33 文件 / 250 测试全过 |
| 前端构建 | `npm run build`（tsc + vite） | ✓ |
| 竞态 | `go test -race ./...` | skipped：本机无 gcc，与全部既往基线一致 |

本 session 修复的回归缺陷：scope_seal_test.go 种子补 project_code；domain/project 测试种子补 ProjectCode+Type；ExpertCenterPage 同步 bridge 异常；client.test.ts / ProjectPage.tsx TS 类型（ProjectType 收窄、MutationAttempt 泛型参数）。

## 4. 残留（显式边界，非阻塞）

1. 真机手动验收：月伴 SAPI 语音端到端（女声音质/免提循环/打断）、reduce-motion/200% 缩放/读屏实测。
2. 发布级 Authenticode 签名需 LUNITIDE_SIGN_COMMAND + LUNITIDE_SIGNER_THUMBPRINT。
