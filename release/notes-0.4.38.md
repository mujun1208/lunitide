# Lunitide 0.4.38

## 修复与改进

- **月伴舞台视觉**：全屏极光背景；空闲/听写保留冷白月盘与能量环，思考为玻璃球，说话为等离子光带。底部玻璃输入「问问月汐」与轮播问句。无 WebGL 或减少动效时退回 CSS 月亮。免提状态机、ASR/TTS、电脑控制未改。
- **首页氛围**：仅启动首页叠极光，普通聊天与项目工作台仍用原 Atmosphere。
- **同事聊天截图**：📷 与 Alt+A 均为微信式框选截图（拖选后发送，Esc / 右键取消）。表情、发图片、本机文件、文件夹仍可用。
- **预览页**：`companion-preview.html` / `launch-preview.html` 仅本地预览，生产 `dist` 只打 `index.html`。

## 验证

- Quality 等价门禁通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（142 files / 1035 tests）、`npm --prefix web run build`、生成契约与工作区一致
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- `Verify-Release.ps1` 对 0.4.38 stage + installer 通过
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）

## 安装包

- `release/out/Lunitide-Setup-0.4.38-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，DigiCert RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`7dd22630d33399a0dd7212fe4e186ff664cbeb08b3f8a7b4ab9566d25399d7e4`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.38 安装包与 stage
