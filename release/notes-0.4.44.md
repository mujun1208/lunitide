# Lunitide 0.4.44

## 修复与改进

- **火山听·读**：一张 `volc_speech` 供应商。基础 URL 只存 `https://openspeech.bytedance.com`；听写走 Agent Plan 双流 `…/plan/sauc/bigmodel_async`（Resource-Id `volc.seedasr.sauc.duration`），朗读走官方 HTTP 单流 `…/plan/tts/unidirectional`（Resource-Id `seed-tts-2.0`）。文档里的 wss/http 全路径、`seedtts-2.0`、`doubao-seed-asr-2.0` / `doubao-seed-tts-2.0` 保存时收成源站和正确 Resource-Id，不再误走 1.0。
- **官方音色**：朗读音色是 seed-tts 2.0 的 `*_uranus_bigtts`（默认小何）。不是本机 50 种人生，也不是豆包 App 角色库。
- **桌面寿命**：引擎死后先放互斥再 `--takeover` 拉起网关，避免杀引擎后空场。
- **月伴诚实**：电脑控制关着不代开、不说「完成了」；UAC 出 `user.ask` /「我不能代点是」；工具轨迹和待确认记忆横幅在会话 / 月伴 / 同事条都能看见。
- **浏览器**：Playwright 未就绪时 `browser.act` click 报 `BROWSER_MCP_NOT_READY`，不是空成功。MCP 市场「＋」会记住策展预置 id。
- **专家与库**：专家目录项 `catalogItemId`（迁移 0108）；模型 kind 区分听写 / 朗读（迁移 0109）。正式 0.4.43 引擎吃不下这两条迁移，本包引擎可以。

## 验证

- Quality 等价门禁连续三轮通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（164 files / 1217 tests）、`npm --prefix web run build`、生成契约与 `verify:bridge --check` 一致
- 第 1 轮修了供应商协议切到火山语音时 `ModelDTO.kind` 的 typecheck
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- `Verify-Release.ps1` 对 0.4.44 stage + installer 通过
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）
- 主尺门闩可报 4.8；拆分加权不是 4.80；13 个独立 Agent 不是本包

## 安装包

- `release/out/Lunitide-Setup-0.4.44-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`d2a14ffa95db474421e97c44abecea80673e692568848fb8d880bc038b3f1b6a`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.44 安装包与 stage
