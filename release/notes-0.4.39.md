# Lunitide 0.4.39

## 修复与改进

- **飞书 / 企微入站**：设置 → 消息通道可开入站（默认关，须白名单）。飞书走官方出站长连接，不监听公网端口；企微只收 `im.inbound.deliver`。钉钉 / 微信 / QQ 不能开入站。入站停到「月汐·普通对话」会话，前缀 `【飞书 · 发送者】`，自动跑一轮 chat。
- **同事文件打开**：收件箱已接收或本人发出的文件可「打开」，只允许收件 / 暂存目录内的本机文件。
- **全盘完全访问默认关**：缺少 `command-policy.json` 不再隐式 FullAccess；月伴只读现有开关，不再静默打开。要开仍走设置 → 命令策略。
- **电脑控制**：`computer.act` 统一动作；截图像素坐标必须带 `frameId`（含显示器拓扑）；窗口级截图；按键保持 / 点击修饰键；UI Automation 观察。不自动点 UAC 或文件打开/保存框。
- **会话检索**：`message.search` 用 FTS 搜本机会话正文。
- **对话工作台**：文件选择走 `user.ask`；意图裁剪工作流与 `structured.output`；嵌套 `AGENTS.md`；压缩提示条；子代理最多 8 步且只读。

## 验证

- Quality 等价门禁通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（142 files / 1046 tests）、`npm --prefix web run build`、生成契约与工作区一致
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- `Verify-Release.ps1` 对 0.4.39 stage + installer 通过
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）

## 安装包

- `release/out/Lunitide-Setup-0.4.39-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，DigiCert RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`ae043da55b5fea4199a3b327d8d535d20dbd758411dbd62f5b02510e209811dd`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.39 安装包与 stage
