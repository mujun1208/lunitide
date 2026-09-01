# Lunitide 0.4.47

0.4.46 之后的累计包：月伴三卡级联 + 火山 `talk.*` 形状适配 + 首页唤醒拆除 + 空草稿回收 + 会议纪要复读。装本包后才能勾 F-*、会议复读、首页不再听。用 0.4.45 / 0.4.46 勾不算数。

合同：`docs/superpowers/specs/2026-09-01-companion-voice-upgrade.md`、`docs/superpowers/specs/2026-09-01-companion-voice-fluency-remediation.md`。

## 修复

- **会话上限误报**：个人项目仍硬顶 100。首页 / 月伴会留下空的「新对话」「月伴对话」占坑。撞上限时先回收这些空草稿再按同一幂等键重试，不抬上限。有正文的会话不会被回收。
- **首页唤醒拆除**：首页不再挂「正在听：说「你好月汐」进入月伴」。设置里去掉「首页语音唤醒」和「挡扬声器误唤醒」。旧设置 `rev < 12` 强制关 `wakeWord`。
- **会议复读**：Windows 听写 `continuous` 会把整段 `results[]` 再送一遍。听写按结果下标吸收，提交前折叠串联重复；会中行做前缀增量；摘要 / 补听在清洗时折叠重复句，不把 20 条相同字幕收成一条以免误删现场行。
- **三卡打断**：火山进舞台强制开自动插话；云端 / 本地强制关，按钮和热键仍能停。文案改成「打断用按钮」，不再写「不能打断」。
- **闲聊不调工具**：「今晚月色如何」「继续聊」「我随便说说」不再因目录非空就带工具 schema。要查天气、打开网页、填表仍开工具。
- **首声可流**：`tts.stream` / `tts.chunk` 进契约。Edge / 火山合成按片推，播放器不再整包等完才响。
- **打断只留已读**：取消月伴回合按舞台 `spokenUpTo` 落盘，未读半句不再按最后一个标点留下。
- **闪模型**：月伴 `chat.start` 静默挑已有 flash / air / lite / mini / haiku，不改设置页展示模型。
- **火山通话核（形状适配）**：`talk.start` / `append` / `cancel`。捕获 16 kHz 上采样到 24 kHz pcm16；转写模型写 `whisper-1`；默认不等 8 秒首片（测试可注入 `firstAudioMs`）。没有可用 realtime 模型时这轮退回用语模型，灯诚实。
- **本地克隆灯**：慢克隆改灯，不改 `E:\GPT-SoVITS\start-api-cpu.bat`、9880、盘符。流式默认仍关。
- **测试稳定**：全量 vitest 下 rethink 点选会偶发 30s 超时；产品逻辑改由 `fireEvent.click` 驱动，不再等 pointer-events。

## 不要指望本包做的

- 未装本包前官方目录仍按旧包装报（当前机子若还是 0.4.45 / 0.4.46，对打仍是 **2.9**）
- **对答如流未过**。没有 20 轮真机 W0，不改 FORCE_COMMIT / stall / 回声门
- 云端 / 本地不是 4.8；没有自动插话
- 火山没有可用 realtime 时不是 4.8；级联顶 **4.3**。talk 形状适配未在本机官方包上证过首声，不能报 4.6+
- 不是 5.0，不是三卡平均，主尺加权不是 4.80
- 不把火山聋静默切 sherpa
- 不复活 MiniCPM-o / `omni.*`
- 不改 SoVITS 启动脚本
- 不重写 `useCompanionMachine`

## 验证

- Quality 等价门禁连续三轮通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge` 无契约漂移、`typecheck`、`npm --prefix web test`（166 files / 1272 tests）、`npm --prefix web run build`
- 第 1 轮修了 `SessionPage.runtime` rethink 用例在全量并行下 30s 超时（孤立重跑 142ms 已绿；改为 `fireEvent`）
- 第 2、3 轮全绿，无新增修复
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）
- 未做桌面 WebView 真机点选；单测绿 ≠ 对打过门

## 安装包

- `release/out/Lunitide-Setup-0.4.47-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`31eab035b9ce951c6de719a07ef8be2b79569ee329d1c9011b1dfb777a46a589`
- `release/out/SHA256SUMS.txt`
- `Verify-Release.ps1` 对 0.4.47 stage + installer 通过
- `release/out` 只保留 0.4.47 安装包与 stage
- 从 0.4.46 升级；不要用 next 旁路覆盖官方目录
