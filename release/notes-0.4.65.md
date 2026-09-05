# Lunitide 0.4.65

把火山月伴一轮一句、任务说完就停、同事专家跟轮、会议三路听写打进产品版本。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方；`companionMaxTokens` 仍 2048；月伴火山默认等待窗仍 1200ms；不把 `result_type` 改成 `single`。

## 1. 月伴火山一轮一句

- 客户端用 `isolateCurrentUtterance` 把火山 `result_type=full` 切成本句。字幕和喂模型都只有这一轮。
- 月伴路径不再跨句 `absorbHeldTranscript`。插话只认最新一句；火山不排队「下一句已记下」。
- 处理中复用现有 lead-in（「好，我来播放。」等），不新造第三句。

## 2. 说完就停

- 字幕 + TTS + 停流同一把尺子：一轮最多 2 句或 80 字。
- `media.play` 一次定生死。第二次 skip。失败口播「没播成。没找到这首歌，请说出歌名或歌手。」
- 未核验播放只续 1 次；失败后不再桌面续跑、不再盲试暂停/播放。

## 3. 同事专家跟轮

- People `Complete` 带线程最近 8 条，去掉本轮 user 再拼一次。
- 只有对话模型目录为空才说「请先启用对话模型」。有模型但空文/失败走诚实错误。
- 「做个模拟计算表」且本轮没出表时补一次 `excel.gen`。`peopleAgentMaxTokens` 1600 → 2400。

## 4. 会议听写

- `voice.start` 增加可选 `endWindowMs`。会议传 400，月伴不传（默认 1200）。
- 相邻 tandem ≥8 字去重。`MEETING_MERGE_GAP_MS` 1000 → 400。
- 连接成功或出字后不再停在「正在连接火山听写…」。云端横幅写明系统声会在停止后补。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；`go test ./...` **全绿**（总覆盖率 **50.7% ≥ 49%** 闸）。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **194 files / 1502 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.65-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.64 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
