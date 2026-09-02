# Lunitide 0.4.54

三条语音对话通道（云端 / 本地 / 火山）统一整改：统一半双工回合模型 + 打断按钮、火山默认单声道（彻底消除双声），并移除本地语音对 E: 盘的硬依赖。

## 修复与优化

- **本地插话不再误打断 / 不再卡壳断链（P2）**：本地通道改为与云端一致的半双工——她思考与说话的整个回合麦克风静音，只能用舞台「打断」按钮或快捷键切回用户轮次。退役了客户端 VAD 语音打断（`companionVoiceBargeInEnabled` 现全通道返回 false），根除了「插话即打断 + 闪烁跳频 + 不识别 + 卡壳断链」的状态机抖动。旧的 `voiceBargeIn` 字段保留但已失效，设置里的「本地全双工打断（实验）」开关已移除。
- **火山不再有两个声音（P3）**：火山默认走稳健的单声道级联管线（seed-asr → 模型 → seed-tts），全程只有一个声音、说完再答、打断用按钮。端到端实时全双工「通话核」改为**显式опt-in**（设置里「实时全双工通话核（火山 · 实验）」，默认关闭），只有在开启且存在实时模型 + 会话时才建立 talk 连接。默认关闭即彻底杜绝 talk PCM 与级联 seed-tts 叠加造成的双声。
- **本地语音不再依赖 E: 盘（P1-A）**：GPT-SoVITS 参考音色包目录不再硬编码 `E:\AI电影漫剧\...`。改为按序解析：环境变量 `LUNITIDE_REF_PACK_DIR`/`LUNITIDE_REF_PACK_DIR_HOT` → `%LOCALAPPDATA%\Lunitide\gpt-sovits\refpacks\<角色扮演|热门音色>`（随按需引擎包安装）→ 仅当存在时的旧机路径 → LOCALAPPDATA 兜底。缺 E: 时本地通道自动回退晓晓朗读，不再报错。启动器探测（`refhost.go`）此前已把 `E:\GPT-SoVITS` 降为「仅存在才用」的末位兜底。
- **统一回合模型与文案（P4）**：三条通道统一「说完再答、打断用按钮」；舞台提示、语音灯标签、通道卡说明、设置提示全部对齐，不再宣称火山「可对着麦打断」（该能力仅在开启实时通话核后由服务端 VAD 提供）。

## 说明（本次未含）

- **本地 ONNX 离线 TTS 默认引擎（P1-B）延后**：将 GPT-SoVITS 降为可选克隆引擎、以 sherpa-onnx 离线 TTS（Piper/Kokoro-zh 级别 ONNX）作为「安装即用」的新默认本地嗓音，是既定方向，但需要（1）选定并托管一个 ONNX 语音模型 + sherpa-onnx TTS 运行时的下载清单，(2) 在真机上验证实际出声。二者无法在无头环境验证，贸然切换默认嗓音有致哑风险，故拆分到下一版单独落地并真机验证。P1-A 已先行消除 E: 硬依赖这一实际阻塞点。

## 验证

- `go vet ./...`、全量 `go test ./...`（0 失败）绿；`internal/tts` 构建 + 单测绿
- `golangci-lint run ./internal/tts/...` **0 issues**
- 前端 `tsc --noEmit` 通过；`vitest run` **171 files / 1307 tests** 全绿（一次并发抖动的 SessionPage 用例单跑 57/57 通过）；`generate-bridge --check` 无契约漂移
- 未做桌面 WebView 真机点选与真机出声验证

## 安装包

- `release/out/Lunitide-Setup-0.4.54-x64.exe`（真签名，Authenticode 已验、DigiCert 时间戳）
- `release/out/SHA256SUMS.txt`
- 从 0.4.53 升级
