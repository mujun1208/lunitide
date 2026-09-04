# Lunitide 0.4.62

对照锁定复盘规格，把十项主体收口到 4.9：引擎断开后能自愈，月伴再进不带失败稿，桌面打开只认前台，决策发送失败保稿。不接线 `companionOpeningAck`，不改 poison / 说完再答 / TTS 音色。

## 1. 核心引擎可自愈（#0）

- 壳层横幅在断开态每 2s 自动 `probe`；成功后改「核心引擎已恢复」约 2s，并广播 `ENGINE_RECOVERED`，当前页自动再 load。
- 看门狗 `ReplaceCaller` 成功后锁住新连接；不伪造 `ok:true`。

## 2. 月伴再进不带失败稿（#7 C7-6）

- `dropCompanionFailedTail` 在本访首轮 `chat.start` 抽掉空稿或含「无法执行」的助手，即使装配形态是 `[历史失败助手, 本访新用户话]`。
- 已结算闲聊保留。舞台再进空场，不弹「继续上次」。

## 3. 打开认前台（#9 G-G）

- `desktop.open` 成功只认当前前台标题/进程命中查询（含汽水 `sodamusic.exe` / `Soda Music`）。
- 列表里有目标、前台仍是 Lunitide → 继续轮询；4s 仍是自家窗 → `无法执行：启动了但窗口没到前台`。
- 纯打开 settle；打开再填仍 continue。`ActivateWindowMatching` 在 SetForeground 失败时仍返回 nil（`media.play` 依赖）。

## 4. 决策保稿（#6）与侧栏保护（#3）

- `user.ask` 批准后若 `chat.start` 失败：向导仍开、输入框留着已选项、toast「核心引擎暂时不可用。你的选择已留在输入框，可点重试。」
- `月伴对话` / 「对话」/「Chat」：侧栏无删无改名；倒带空会话也不会删掉受保护对话。

## 5. 其余已落地主体

- #1 听写设置 overlay 不卸载会议页、不 `meetings.stop`；录制中逐字稿仍在。
- #2 机务五级折叠。
- #4 speaking `glass` + `amplitude>=1.45`。
- #5 左侧只读专家镜像。
- #8 生产路径无假「嗯，」；首个模型 token 先上字幕，TTS 仍等第一句标点（C8-2）。
- Project / Skill / Expert 失败态带 `code · correlationId`。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；`go test ./...` **全绿**（总覆盖率 **49.9% ≥ 49%** 闸）。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **190 files / 1450 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。

## 安装包

- `release/out/Lunitide-Setup-0.4.62-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.61 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
