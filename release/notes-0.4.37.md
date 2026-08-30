# Lunitide 0.4.37

## 修复与改进

- **月伴火山听写**：语音通道第三档「火山听写 · seed-asr」。火山只负责听；嘴仍是 Edge / 晓晓。不走 realtime/dialogue，不改 sherpa 本地识别。
- **配置融合**：供应商「语音模型」页协议「火山语音」；月伴设置三列通道（云端 / 火山 / 本地）。选火山会打开 barge-in，并丢掉 refpack 音色。
- **握手与安全**：生产拨号只连 `openspeech.bytedance.com`；握手 1.5s，async 路径先试 800ms 再回落 `bigmodel`。`voice.start` 的 volc 必须带 `providerId`。
- **会话稳定**：握手积压按 `ValidFrame`（最多 1 秒）分包，避免首包超限掉回系统识别；一轮结束后送 400ms 静音并抑制同句 definite 1.5s，不 `voice.finish` 重开 WebSocket。静音/打断仍保活连接。
- **舞台清理**：卸载时清掉 TTS 轮询与预热定时器；退出或卸载时丢掉尚未 adopt 的火山会话。供应商列表挂起 1.5s 后改用系统识别。

## 验证

- Quality 等价门禁通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（136 files / 1022 tests）、`npm --prefix web run build`、生成契约与工作区一致
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）

## 安装包

- `release/out/Lunitide-Setup-0.4.37-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，DigiCert RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`708bdd3107ba9d5600330ab22ed5fe7a81cc2aff398a6cee33ae2a6d55725b52`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.37 安装包与 stage
