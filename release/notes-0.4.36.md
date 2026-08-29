# Lunitide 0.4.36

## 修复与改进

- **会议停止**：一点「停止」立刻冻结计时器、列表改为「整理中」而不是继续「录制中」。补转写只在实时字幕过短或落后时钟时运行；十分钟量级、字幕完整的会直接进入纪要。
- **桌面操作**：在 Word/WPS 里填写不再误点「关闭」；「再打开刚才的文档」走上次打开的路径。QQ 不再抢走 QQ 音乐。播歌搜索结果，禁止点「我喜欢的音乐 / 收藏」。
- **消息通道**：设置 → 消息通道。飞书 / 企微 / 钉钉粘贴 https 群机器人 Webhook；微信 / QQ 使用本机已登录客户端。对月伴说「发给飞书…」走 `im.send`。
- **资产模版**：`0097` 让 `audit_events` 接受模版上传动作，上传不再在 100% 后因 CHECK 回滚。
- **桌面图标**：月亮、空隙、淡云线；透明底角。重新生成 png / ico / `lunitide.syso`。

## 验证

- Quality 等价门禁通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（130 files / 961 tests）、`npm --prefix web run build`、生成契约与工作区一致（提交后 `git diff --exit-code` 对三份 generated 文件为空）
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- `Verify-Release.ps1` 对 0.4.36 stage + installer 通过
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）

## 安装包

- `release/out/Lunitide-Setup-0.4.36-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，DigiCert RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`9ffc2656d7d46ed8a7044517a679e6d4b76db14fbaa78dc9f021e32d2a844fb7`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.36 安装包与 stage
