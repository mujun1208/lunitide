# Lunitide 0.4.80

把 Agent Hub V2.1 未提交缺口收成可发布版本：Codex 能探到 app-server 就中途提问，探不到就诚实一把跑完；Cursor / Kimi 能 `session/load` 再回落 `session/new`。开关 `officeMenu.agentHub` 默认打开。顺带修掉自动化 Recover 测试在覆盖率下抢跑的竞态。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称 Pi / Claude / OpenCode / 三家 GUI 已接入。

## 1. 外接会话

- 对话页是月汐自己的界面，不嵌官方窗口；未接入的 CLI 不出现。检测未完成时不能执行。
- 写项目 / 改代码仍必须先选根。空目录拒绝并提示「请先选择项目目录」。
- Codex：`codex app-server` 握手成功则多轮提问；失败时先写通知再 `exec`，并按权限映射沙箱（完全访问 → `danger-full-access`）。探测失败不再先卡 20 秒握手。
- Cursor / Kimi：`session/load` 成功但没带回 `sessionId` 时保住已存 native id；`session/new` 没带回 id 则失败，不把空会话继续往下打。
- 应用重启会 `Close` 仍标为 running / waiting_user 的适配器，再把会话标 `faulted` 并取消未答选项。不声称能续上正在跑的原生进程。
- `tokensUsed` 取最新一条 usage，不累加。选项条提交失败用 `role=alert` 显示。

## 2. 开关与隔离

- `officeMenu.agentHub` 缺省为 true；用户显式存 false 仍尊重。
- 生产引擎若设置 `LUNITIDE_DATA_ROOT`，必须是绝对路径，否则直接失败；空值仍走正式资料目录。

## 3. 周报与自动化连带

- 办公预览 / 周报 PDF 必须见到 `%PDF`、完全访问不再二次审批：沿用 0.4.79，本版未改办公生成路径。
- 调度 Recover 用例在第一次跑完、`running` 标记清掉之前不再二次 `TriggerNow`，避免覆盖率下误报「任务已在运行」。

## 4. 明确不做

- 后继 6 家适配器、三家官方 GUI、用 WebView2 去点官方窗口、本机未装的 Cursor / Kimi 真机联调、Cursor 插件市场 Skills 对齐、公共 `ConversationChrome` 抽取。
- 不接 API Key，不打月汐模型账本。

## 验证

- Go：覆盖率闸 **59.7% ≥ 51%**；`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 通过；`vitest run` **305 files / 2373 tests** 全绿；`vite build` 通过。生成文件已重跑并对齐。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。未对生产 `%LocalAppData%\Lunitide` 启引擎。

## 安装包

- `release/out/Lunitide-Setup-0.4.80-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.79 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
- `release/out` 只保留本版本。
