# Lunitide 0.4.66

工作台加号「添加上下文」走 Host 系统文件框，不再用 WebView2 隐藏 `<input type="file">` 当主路径。图片附件可以在工作区看见原图。发送失败不再伪造「核心引擎暂时不可用」。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方 / 会议听写 / 同事文件传输。Engine 不打开用户文件系统路径。

## 1. Host 选文件 / 选文件夹

- 新 Bridge：`desktop.files.pick`、`desktop.files.readChunk`。Host 写入进程内 10 分钟 allowlist，只读普通文件（无符号链接），分片上限 32768。
- Windows 先走 WinForms/PowerShell；失败再 `GetOpenFileNameW` / `SHBrowseForFolderW`。取消是成功，不是对话框坏了。
- 渲染进程拼 `File[]`，继续走现有 `ingestAttachments` / `attachment.upload.*`。不实现 `attachment.ingestFromPath`。
- 文件夹里没有白名单文件 → 「这个文件夹里没有可导入的文件。」跳过的 exe 等会点名。隐藏 input 取消保持静默。

## 2. 视觉图片预览

- png / jpeg / webp 解析成功（空文本，不注入聊天）。`attachment.get` 可选 `contentBase64`（上限 240000）。
- 工作区用原图预览，文案改为「图片 · 可供视觉分析」。删掉「工作区尚未取得原图字节」那句假话。

## 3. 加号列表与发送诚实

- @ / 技能 / 专家列表失败不再 `.catch(() => [])` 装成空菜单。
- `submitUserAsk` 失败用 `CHAT_ASK_FOLLOWUP_FAILED` / 「这次没发出去，请再试一次。」不再伪造 `ENGINE_UNAVAILABLE` 横幅。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；覆盖率闸 **50.7% ≥ 49%**。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **196 files / 1522 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.66-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.65 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
