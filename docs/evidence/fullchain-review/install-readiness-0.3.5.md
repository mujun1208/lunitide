# 0.3.5 安装就绪收口证据（能力解锁三遗留收口）

生成时间：2026-08-16（UTC+8）· 平台：windows/amd64 · go1.26.5 · 结论：**PASS（-race 门禁首次本机全量跑通：WinLibs GCC 16.1.0 经 winget 就位）**

## 1. 本版本收口的三项遗留（capability-unlock-0.3.5 → P1）

| # | 遗留项 | 落地 | 结果 |
|---|--------|------|------|
| 1 | -race 依赖 gcc 环境缺失 | winget 已装 WinLibs GCC 16.1.0（BrechtSanders.WinLibs.POSIX.UCRT）；PATH 注入 `…WinGet\Packages\…\mingw64\bin` + `CGO_ENABLED=1` 后 `go test -race` 全量可执行 | 已收口 |
| 2 | MCP 工具 schema 仅 pass-through | `internal/mcp/remote.go` ListTools（GET {BaseURL}/tools 目录抓取）；`internal/mcp6/registry.go` DescribeFunc/refreshToolbox 缓存 + ReadyTool 携带真实 schema；`internal/app/chat.go` mcpToolDefinitions 优先采用 describe 缓存 schema（JSON object 校验，失败回退 pass-through）；`cmd/engine/mcpgateway.go` mcpGatewayDescribe 接线 | 已收口 |
| 3 | command-policy.json 无 UI | `api/bridge/v1/tools.commandPolicy.get/set.schema.json`（x-method 契约，生成器启用清单 + envelope 枚举同步）；`internal/app/tools_policy_handlers.go`（FEATURE_DISABLED/STORAGE_UNAVAILABLE/COMMAND_POLICY_INVALID 错误映射）；`internal/toolruntime` CommandPolicyJSON/SetCommandPolicyJSON（校验→临时文件→原子替换→RWMutex 热切换，Windows 语义显式 Remove+Rename）；web `ToolsPolicyBridge` + 设置"安全与治理"CommandPolicyPanel（增删改/前缀归一/maxArgs/超时编辑，保存即热生效提示） | 已收口 |

## 2. 0.3.5 发行构建验证

- 命令：`release/Build-Release.ps1 -AllowUnsignedDevelopment`（NSIS /WX 零警告）
- 产物：`release/out/Lunitide-Setup-0.3.5-x64.exe`（8,834,948 B）+ `release/out/Lunitide-0.3.5-x64/`
- 布局：Lunitide.exe（9,284,608 B）/ lunitide-engine.exe（16,781,312 B）/ purge-user-data.exe（2,036,736 B）/ WebView2Loader.dll（159,848 B，SHA-256 钉扎）/ web/dist / licenses / lunitide-icon.ico / SHA256SUMS.txt / verify+stop 脚本
- 版本冒烟：`Lunitide.exe -version` → `0.3.5` ✓
- 运行时验收（Test-Runtime.ps1，stage 目录直跑）：WebView2 初始文档加载 ✓、唯一 engine 子进程 ✓、WM_CLOSE 干净退出（exit 0）✓、无残留子进程 ✓；3 轮中 2 轮通过，首轮出现一次启动后自行退出的瞬时竞态（CloseMainWindow 时进程已退出），复跑 2 次均干净、无残留进程，判为环境瞬时抖动并留观
- 未签名披露：同 0.3.3/0.3.4，-AllowUnsignedDevelopment 测试候选，发布级 Authenticode 签名门禁不变

## 3. 全量回归（本 session 最终轮）

| 门禁 | 命令 | 结果 |
|------|------|------|
| 静态 | `go vet ./...` | 0 警告 |
| Go 单元/集成 | `go test ./... -count=1` | 全部 ok，fail=0（含新增 TestCommandPolicyJSONRoundTripAndHotApply、describe/listtools 契约组） |
| 竞态 | `go test -race ./...` | **全量通过，0 数据竞争**；storage/sqlite 首跑触默认 10min 超时（modernc 纯 Go 驱动 -race 放慢 ~14x），`-timeout=40m` 单包重跑 1389.9s 通过 |
| 契约 | `go test ./internal/contract/` | 通过（tools.commandPolicy.get/set 进三端生成物：bridge.ts / schema_generated.go / contract test） |
| 前端 | `npx vitest run` | 34 文件 / 255 测试全过（含 CommandPolicy.test.tsx 5 例） |
| 前端构建 | `npm run build`（generate:bridge + tsc + vite） | ✓ |

新增测试：Go — TestCommandPolicyJSONRoundTripAndHotApply（空文档回退/热生效/坏文档整体拒绝/重启持久化）；前端 — CommandPolicy.test.tsx（加载渲染/前缀归一提交/空行丢弃/拒绝原因可见且行可编辑/加载失败锁保存）。

## 4. 残留（显式边界，非阻塞）

1. Test-Runtime 首轮瞬时启动退出竞态：3 轮 2 过，未复现；后续版本如再现需排查单实例互斥与 WebView2 窗口消息时序。
2. 真机手动验收：月伴 SAPI 语音端到端（女声音质/免提循环/打断）、reduce-motion/200% 缩放/读屏实测（沿袭 0.3.4）。
3. 发布级 Authenticode 签名需 LUNITIDE_SIGN_COMMAND + LUNITIDE_SIGNER_THUMBPRINT。
4. gcc 仅本会话 PATH 注入可用；如需常态启用 -race，建议将 WinLibs mingw64\bin 写入用户 PATH（或 winget links）。
