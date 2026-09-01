# Lunitide 0.4.45

语音全链路整改包。装本包后才能勾语音专项 §7。用 0.4.44 勾语音不算数。

## 修复

- **回合只提交一次**：同一句听写不再连开 `chat.start`。FORCE_COMMIT 单次。月伴进行中同一句返回，不再 reset 再开流。退出月伴清掉排队。
- **火山听写**：只挑 ASR，不再误用默认 TTS speaker。`result` 支持对象 / 数组 / `payload_msg`。`result_type=full`，热词走官方 `dialog_ctx`。选火山失败仍停在火山，文案 VOICE-004，不切 sherpa。
- **本地克隆诚实**：启动脚本退出后不再假「启动中」。设置页会写出 `jieba_fast` / `dict.txt` 一类原因。进程死后试听可点并诚实失败。不改 `E:\GPT-SoVITS\start-api-cpu.bat`、9880、盘符，不往安装包塞字典。
- **云端文案**：听=系统识别，说=微软晓晓，不能插话。

## 验证

- 定点：`go test ./internal/voice/volcsauc ./internal/tts ./internal/app -count=1`
- 定点 Vitest：月伴回合、火山灯、设置试听、同事标题过滤
- 未跑 CI `windows-cgo-race`（本机不跑 90m）
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）
- 语音真机须装本包后再勾，未勾前真机仍按 2.4 报

## 安装包

- `release/out/Lunitide-Setup-0.4.45-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，RFC3161 时间戳。仅当前 Windows 账户信任该证书）
- SHA-256：`4b46d7c79be2a7161fa4ea32e02f6184f73091d9abee0764c38eabdbabedbf03`
- `release/out/SHA256SUMS.txt`
- `Verify-Release.ps1` 对 0.4.45 stage + installer 通过
- 从 0.4.44 升级；不要用 next 旁路覆盖官方目录
- 本机 2026-09-01 已从 0.4.44 升到 0.4.45：`DisplayVersion` / `--rpc-health` 均为 `0.4.45` 且 `engine=ready`；官方目录无 `*-next`
- 语音 §7 真机尚未勾
