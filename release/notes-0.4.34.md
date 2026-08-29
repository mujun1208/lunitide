# Lunitide 0.4.34

## 修复与改进

- **同事聊天框选截图**：📷 后增加「框选截图」。Windows 冻结桌面后拖选区域，完成/Enter 发送，Esc/右键取消。无原生框选时退回系统共享画面再裁切。取消不会误走全屏共享。
- **桌面 / 程序列表图标**：修正 PE 资源目录（`RT_ICON` + `RT_GROUP_ICON`，子目录偏移带 directory 位）。`lunitide.syso` 为 amd64/`IMAGE_REL_AMD64_ADDR32NB`，可被 Go 内链器链接。图标为透明底月亮 + 蓝云；快捷方式与「程序和功能」DisplayIcon 指向 `lunitide-icon.ico`。
- **中文产品名**：界面为中文时侧栏、工作台、关于页、对话图片显示「月汐」；默认英文仍为 Lunitide。安装包 DisplayName 仍为 Lunitide。
- **会议实时字幕**：录音一直持续到点停止。字幕若约 25 秒无更新则只重启识别，不结束 WAV。本地 sherpa 会话约每 45 秒轮换，避免超长一句被拒。
- **汽水音乐**：工作流说明改为 `sodamusic.exe` / Soda Music，不再写成网易云的 `cloudmusic.exe`。

## 验证

- Quality 等价门禁通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`、`npm --prefix web run build`、生成契约 `git diff --exit-code`
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- `go test ./cmd/gen-icon ./cmd/gen-syso ./internal/ccapp ./internal/app`
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）

## 安装包

- `release/out/Lunitide-Setup-0.4.34-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，DigiCert RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`b877cbd9576c49246e6f2f18e83b0297fe5420a840c32c6a699366d623cfde6c`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.34 安装包与 stage
