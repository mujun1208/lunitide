# Lunitide 0.4.79

把 Agent Hub V2.1 独立会话打进正式版本：外接对话留在模块内，不焊进月汐 `SessionPage`。顺带收齐办公预览滚动、周报 PDF 识别和「完全访问」不再二次开轮。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称 Pi / Claude / OpenCode / 三家 GUI 已接入，也不声称 Codex app-server 已探活。开关 `officeMenu.agentHub` 默认仍为 false。

## 1. 外接会话（开发层范围内无缺口）

- 工作台创建会话；写项目 / 改代码必须先选根。做 PPT 与自由可不选。
- Cursor / Kimi 走 ACP 多轮；Codex 先 `exec` 一把跑完，卡上写明不能中途提问。
- 打开 CLI 失败会把会话标 `faulted` 并留下原文（截断 200 字），不再留下沉默空会话。
- 权限：手动不过 permission；自动只放过改文件；完全访问放过工具权限。业务选项永远要人点。完全访问文案写清可能用到本机 MCP。
- 选项条可写「补充说明」。成功回合会记下工作区扫描文件和导出白名单拷贝。
- `thread.get` 带 `tokensUsed`；界面写「消耗的是该 CLI 自己的会员额度」。
- 右侧可下钻文件夹并回上一级。PPT 场景结束没有产出文稿时如实说「没有文稿」。
- 侧栏可重命名 / 删除本模块会话；外接态不走月汐对话搜索。

## 2. 办公与周报

- 切换任务或回首页会清预览滚动；PPT 长文稿可按页翻。
- 周报 PDF 必须看到 `%PDF` 才当 PDF 嵌；只有提示文字不再空白或误报。
- 对话已是「完全访问」时，周报全权不再二次 `decideTool` 开一轮。

## 3. 明确不做

- 后继 6 家适配器、三家官方 GUI、桌面 WebView2 联调、Codex app-server 探活、公共 `ConversationChrome` 抽取。
- 不接 API Key，不打月汐模型账本。

## 验证

- Go：覆盖率闸 **59.6% ≥ 51%**；`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 通过；`vitest run` **305 files / 2370 tests** 全绿；`vite build` 通过。生成文件已重跑并对齐。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。未对生产 `%LocalAppData%\Lunitide` 启引擎。

## 安装包

- `release/out/Lunitide-Setup-0.4.79-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.78 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
