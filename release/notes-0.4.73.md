# Lunitide 0.4.73

把 0.4.72 之后已验证的办公产物、专家成套、图表流式渲染和电脑控制补丁打进正式版本。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。

## 1. 专家成套与提问分寸

- 专家创建走完整档案：能力分析、装备匹配、两张对照表；`skillKeys` / `mcp:<id>` 写进绑定，不再一句「已创建，去专家中心」收场。
- `user.ask` 先想后问：键盘只在确有用户分叉时出牌；语音不弹决策卡，改成口播选项。

## 2. 办公产物与会话文件夹

- 会话产物承认 `md` / `txt`（及导出 `markdown` / `csv`），办公归档、打开、预览与 Bridge 契约对齐。
- 会话文件夹树、办公工作台同步和 Office MCP 预设（Excel `uvx`、PPT 分叉、MarkItDown 上限）按同一套本机工具走。
- 生成文件用系统关联应用打开；不嵌入 MinerU / Docling / Office COM。

## 3. 图表、电脑控制、MCP 进程

- Mermaid 等围栏闭合并稳定后再渲染；流式过程显示「图表生成中」，超时可重试，不再闪错误再摊源码。
- `computer.act` 按窗口精确名、点击阶梯、截图+坐标同一条路径；语音与键盘共用。
- MCP stdio 启动锁避免同进程抢启动。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；覆盖率闸 **57.8% ≥ 51%**。
- 前端：`npm audit` **0**（vitest **4.1.11**）；`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **265 files / 2016 tests** 全绿；`vite build` 通过。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.73-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.72 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
