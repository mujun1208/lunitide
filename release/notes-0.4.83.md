# Lunitide 0.4.83

装好以后可以检查 GitHub 上的 `latest.json`，点提示就下载并覆盖安装，不必再卸载重装。本机已有更高版本的 Setup 时仍优先用本地包。AgentHub 改成对话页，Work / AgentHub 可切换；工作区顶栏收瘦；OCR 路由独立成设置页，PP-OCR 只记目录偏好。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称高端商用，也不声称 E / Q / D 已 100%。

## 1. 应用内覆盖升级

- 启动后先看本机 `%LOCALAPPDATA%\Lunitide\updates\latest.json`（或 `LUNITIDE_UPDATE_DIR`），再看 GitHub Releases 的 `latest.json`。更高版本会顶栏提示；点「立即升级」先核对摘要，再静默 `/S` 覆盖。
- 远程只允许 `github.com` / `*.githubusercontent.com`，或测试用的本机回环覆盖地址。摘要对不上或文件缺失则失败，不会空跑。
- 打包脚本仍把 Setup 和摘要写进上述目录，并清掉旧 Setup。`Build-Release.ps1 -Publish` 才会上传 GitHub Release；默认打包装包不再自动 `gh release create`。
- 当前已装的 0.4.82 及更早没有这套远程下载。这次升级请直接运行 `Lunitide-Setup-0.4.83-x64.exe`，不要先卸载。装上 0.4.83 之后，再打的新包才会弹窗。

## 2. AgentHub、工作区、路由

- AgentHub 首页与会话共用月汐对话框：语音是听写，打断走 `threadCancel`，「连接」只重新探测本机 CLI。任务类型是项目 / 代码 / PPT / 文档 / 其他。
- 左侧灯是历史任务。Hub 页不再放「新对话 / 历史任务」主按钮。附件 / 项目目录 / 产物目录选中后旁侧显示。
- 模板是「写周报 Markdown」，明确不要生成 Office。Work 对话里「写周报」仍走办公生成；用户写明「不要生成 Office」时不再按周报 Word 收窄。
- 工作区：浏览器只留一条地址栏；文件栏是名称加小下载；代码树可折叠。月伴 HiDPI 按设备像素画，不再糊。
- 设置「路由管理」管能力路由和 OCR。供应商页只做目录。Windows OCR 始终列出。PP-OCR 不是随包；目录可记为偏好，识别仍走 Windows OCR。

## 3. 明确不做

- 不把 AgentHub 焊进 SessionPage。不接 API Key，不打月汐模型账本。
- 不发明 Paddle 下载管线，不把空 `ppocr` 文件夹当成已安装。
- 不自动合并到 `main`。不把本版写成产品 100%。

## 验证

- Go：覆盖率闸 **59.8% ≥ 51%**；`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...`（toolchain go1.26.6）受影响漏洞 **0**。
- 前端：`tsc --noEmit` 通过；`vitest run` **321 files / 2452 tests** 全绿；生成 Bridge 已重跑。
- `npm --prefix web audit --audit-level=moderate` **0**；`./release/Test-OmniExcluded.ps1` 通过；`./release/Test-ReleaseTools.ps1` 通过。
- 本机未跑 CI 的 120 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality / Release candidate 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。未对生产 `%LocalAppData%\Lunitide` 启引擎。

## 安装包

- `release/out/Lunitide-Setup-0.4.83-x64.exe`（本机没有 Windows SDK `signtool.exe`，按 `-AllowUnsignedDevelopment` 打出的未签名候选，不是生产签名版）
- `release/out/SHA256SUMS.txt`
- `release/out/latest.json`（给本机更新目录和 GitHub overlay 用，不进 SHA256SUMS）
- 从 0.4.82 升级：直接运行 Setup，不必卸载。`release/out` 只保留本版本。
- GitHub `v0.4.83` 上传需要本机 `gh` 已登录且网络能连 github.com；这次推送和 Release 上传被 443 连不上挡住。
