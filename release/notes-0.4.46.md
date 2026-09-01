# Lunitide 0.4.46

0.4.44 之后的累计包：语音全链路（原 0.4.45）+ 0.4.45 真机残差 + 本轮 W1 操作修复。装本包后才能勾语音 §7 和「打断后灯稳」。用 0.4.44 / 0.4.45 勾不算数。

合同：`docs/superpowers/specs/2026-09-01-voice-pipeline-remediation.md`、`docs/superpowers/specs/2026-09-01-voice-residual-remediation.md`、`docs/superpowers/specs/2026-09-01-system-upgrade-and-remediation.md`。

## 修复

- **回合只提交一次**：同一句听写不再连开 `chat.start`。FORCE_COMMIT 单次。月伴进行中同一句返回，不再 reset 再开流。
- **火山听写**：只挑 ASR；`result` 支持对象 / 数组 / `payload_msg`；`result_type=full`；热词走官方 `dialog_ctx`。选火山失败仍停在火山，不切 sherpa。
- **本地克隆诚实**：启动脚本退出后不再假「启动中」。设置页会写出 `jieba_fast` / `dict.txt` 一类原因。不改 `E:\GPT-SoVITS\start-api-cpu.bat`、9880、盘符。
- **垫话只一次**：工具步不再反复注入「好，我帮你查一下」。字幕把相邻重复垫话收成一句。
- **退出留会话**：月伴说过话再退出，不再把空 `items` 当成空会话删掉。侧栏能找到第一句标题。
- **本地能出声**：SoVITS 未就绪时这一轮用晓晓读，灯改成「晓晓（本机朗读未就绪）」，不把本地卡存成 edge，不冒充克隆。
- **说完不冒字**：播放结束后回声窗内不提交新用户句；垫话原句不当作用户话。
- **天气一轮搜**：月伴查天气只 `web.search` 一次，没有用户网址时不再第二次 search / fetch。
- **打断真停**：取消流即使没有 stream 句柄或 `cancel` 挂住，也会本地收成 cancelled。输入框生成中露出带字的「停止」，不再把用量环当成停止键。
- **月伴打断不闪灯**：打断清掉排队句，不再回「上一句还在发送」；对答中「暂停」改成「停止」并真正取消这一轮。退出或卸载后 `flush` 不再误报播完、不再 `setState`。
- **MCP 市场卸载**：已安装卡上有「卸载」，不必先切到「已安装」页。
- **空对话侧栏**：没有普通对话时不再撑出一条可上下拖的假分隔线。

## 不要指望本包做的

- 不修本机 `jieba_fast\dict.txt`，不改 `E:\GPT-SoVITS\start-api-cpu.bat`
- 不把火山失败改切 sherpa
- 不把用量环变成停止键；停止是输入框旁带字的「停止」
- D12 仍须真实 UAC（装驱动 / 改系统），不能靠模型「触发 uc」
- 装包前不能勾桌面 V5-* / 「打断后灯稳」
- 主尺门闩可报 4.8；拆分加权不是 4.80；不是正式发布；13 个独立 Agent 不是本包

## 验证

- Quality 等价门禁连续三轮通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge` 无契约漂移、`typecheck`、`npm --prefix web test`（164 files / 1241 tests，无未处理异常）、`npm --prefix web run build`
- 第 1 轮修了月伴 TTS `flush` 在 `dispose`/`interrupt` 后仍回调 `onFinished`，以及卸载后 `setGain` 打到已拆掉的 `window`
- 第 3 轮修了已下架 MCP 先闪市场页：清单里有 leftover 时同步切到「已安装」才能看见卸载
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）

## 安装包

- `release/out/Lunitide-Setup-0.4.46-x64.exe`（打包后填写签名与 SHA-256）
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.46 安装包与 stage
