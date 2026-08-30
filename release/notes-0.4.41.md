# Lunitide 0.4.41

## 修复与改进

- **火山 Agent Plan**：BASE URL 已带 `v1`/`v3` 时不再追加 `/v1`。`https://ark.cn-beijing.volces.com/api/plan/v3` 打到 `/chat/completions`，连接测试不再误报失败。
- **桌面 / 安装 / 卸载图标**：月亮 + 空隙 + 淡云线；ICO 改为 32 位 BMP（安装包和卸载程序不再黑底）。升级时先删再建快捷方式并刷新资源管理器缓存，避免一直显示旧的三朵蓝云。
- **月伴入口**：听 / 说 / 想三灯；模型或听路未齐不会空听。已选火山听写时失败不再改走系统识别。垫话去掉「嗯」。桌面忙碌时会说「桌面正忙，我稍后再试。」
- **会议纪要**：可指定听写路径和笔记用的 LLM（`meetings.summarize` 的 `modelId`）。
- **浏览器**：`browser.act` 增加滚动、后退、悬停、选择、按键、标签、等待、对话框（仍不自动处理登录墙 / 验证码 / 文件选择）。
- **清单页**：`html.gen` 增加 `checklist` 模板。
- **插件市场**：条目标明是内置名单开关，不会假装再装一份。归档的 GitHub / Puppeteer / SQLite / Git MCP 若仍留在参数里会提示。
- **其它**：微信/QQ 发消息打不进会话时如实失败；自动化「AI 早报」强制 `web.search` 带来源；中文报告优先 `docx.gen`。

## 验证

- Quality 等价门禁连续三轮通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（147 files / 1089 tests）、`npm --prefix web run build`、生成契约与工作区一致
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- `Verify-Release.ps1` 对 0.4.41 stage + installer 通过
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）

## 安装包

- `release/out/Lunitide-Setup-0.4.41-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，DigiCert RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：见打包完成后的 `release/out/SHA256SUMS.txt`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.41 安装包与 stage
