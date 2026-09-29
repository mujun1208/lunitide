# Lunitide 0.4.64

把月伴第二种脸「星尘」和执行收口打进产品版本。玉盘像素与星尘配方都不改；不改 poison / 月伴 TTS 音色 / 专家表；`desktop.open` 成功仍只认前台；不伪造 `ENGINE_UNAVAILABLE`。

## 1. 星尘第二种月亮

- 默认仍是玉盘。设置与舞台顶栏共用「玉盘 | 星尘」开关，写入 `visualSkin`，不升设置 rev。
- 星尘挂定稿粒子仿真；月亮只留透明热区，不叠第二套玉盘 WebGL。
- 「切换风格 / 换成粒子月亮看看」只换脸并口播确认，不进引擎。天气句仍走 `chat.start`。
- 无 WebGL / 减动效时星尘仍画彩晕圆，不是黑屏。掉到 40fps 自动降档。

## 2. 执行收口

- 闲聊门改为针 ∪ R1–R4。「今晚天气 / 今天合肥的天气怎么样」挂 `web.search`；「你好 / 今晚月色如何」仍零工具。
- 已挂表却只说稍等：续轮催工具。三次仍不调则口播「无法执行：这一轮没有完成查询。」
- 搜索失败口播「无法执行：这次没有查到。」

## 3. 火山定稿与插话

- 火山等待窗 400ms → 1200ms，对齐 sherpa 量级。客户端 280/220 不动。
- 仅火山 cascade 说话中可对着麦打断；云端 / 本机仍只按钮。回声过滤保留。

## 4. 打字 = 语音桌面手

- 同一句路由与允许集同档。电脑控制开时预批 `desktop.type` / `computer.act`。月伴仍剥 `command.run`。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；`go test ./...` **全绿**（总覆盖率 **50.7% ≥ 49%** 闸）。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **194 files / 1488 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。

## 安装包

- `release/out/Lunitide-Setup-0.4.64-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.63 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
