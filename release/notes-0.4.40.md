# Lunitide 0.4.40

## 修复与改进

- **月伴听路**：HOST_BUSY 排队；火山超时后本机 sherpa，再云端；「嗯。」垫话不抢麦。火山只负责听，嘴仍是 Edge / GPT-SoVITS。
- **桌面手**：按意图选一把（专用工具 / 浏览器 / 打开 / 输入），不要四套里轮流赌。`media.play` / `desktop.open` 不再误绑全盘权限。
- **未完成工作继续**：卡在「请确认」、过期画面、未核验的播放时，本轮继续做完，而不是停住。
- **本轮约定**：会话里可见 `chat.guidance` 芯片（工作流 / 身份 / AGENTS / 技能），不再只写日志。
- **企微入站**：与飞书一样走官方出站长连接（Bot ID/Secret），不监听公网端口；`im.inbound.deliver` 仅作兜底。入站自动跑一轮最多 2 分钟。
- **会议**：每次写入都落盘；总结 LLM 不再被 20 秒持久化超时打断；完成后先 Complete 再 Stream。
- **同事**：人像只保留框选截图与 Alt+A，不再走摄像头。
- **电脑控制**：启用时可勾选 30 分钟后自动关闭；到点关闸，紧急停止同时清掉定时。
- **专家市场**：目录为空时隐藏该页签。研究子代理最多 16 步（仍只读）。

## 验证

- Quality 等价门禁通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（143 files / 1062 tests）、`npm --prefix web run build`、生成契约与工作区一致
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- `Verify-Release.ps1` 对 0.4.40 stage + installer 通过
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）

## 安装包

- `release/out/Lunitide-Setup-0.4.40-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，DigiCert RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：（打包后填入）
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.40 安装包与 stage
