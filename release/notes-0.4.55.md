# Lunitide 0.4.55

本地语音「安装即用」落地（P1-B）：新增内置离线 ONNX 嗓音（sherpa-onnx + Kokoro 多语），成为「本地」通道的默认引擎，不再依赖 Python、常驻服务或参考音频；GPT-SoVITS 降级为可选的高级克隆引擎。这是 0.4.54 里明确延后的那一项。

## 新增

- **内置离线 Kokoro 本地嗓音（默认本地引擎）**：新增 `onnx` 合成引擎，用 sherpa-onnx 的 `offline-tts` 可执行文件驱动 Kokoro multi-lang v1.0 模型，纯离线、单次合成即一个短命子进程写出一段 WAV，无需 Python、无需常驻服务、无需参考音频。内置 8 个中文音色（晓晓/晓伊/晓北/晓妮/云希/云扬/云健/云夏）+ 2 个英文音色，开箱即用。
- **按需下载、摘要校验、安装即用**：运行时（sherpa-onnx，约 21 MB）与模型（Kokoro 多语，约 333 MB）两个包在设置中按需下载，落盘到 `%LOCALAPPDATA%\Lunitide`，用与 ASR 运行时同一套 `voice.Installer` 做 SHA-256 摘要校验。两个默认下载源均为上游 k2-fsa GitHub Release 资产，**摘要与字节数已逐一对拉校验一致**（runtime `57a4815…658c8` / 21210111 B；model `c133d26…be7046` / 349418188 B），可用 `LUNITIDE_ONNX_TTS_RUNTIME_URL/_SHA256/_BYTES`、`LUNITIDE_ONNX_TTS_MODEL_URL/_SHA256/_BYTES` 重定向到镜像。
- **新桥方法 `tts.installOnnxEngine`**：默认只读探测（不触发下载），`{ "start": true }` 才启动下载；两个包顺序安装并汇总为一条连续进度条。设置页新增「本地语音引擎（Kokoro）」下载行，`state==ready` 后自动隐藏。

## 变更

- **「本地」通道默认改为 Kokoro（onnx）**：设置里的「本地引擎」下拉默认「Kokoro 离线（推荐）」，GPT-SoVITS 克隆改为「GPT-SoVITS 克隆（高级）」可选项，选中后才显示其服务地址等宿主控件。
- **旧存档一次性迁移**：`rev < 13` 里把「本地」钉死在 GPT-SoVITS（模型曾放在硬编码外置盘）的存档，一次性迁移到内置 Kokoro 并归一化音色 id；显式重新选择 GPT-SoVITS 的用户其选择保留。引擎切换时不兼容的音色 id（`onnx-*` ↔ 其它）会被丢弃归一。
- **本地预热覆盖 onnx**：进入月伴时对 onnx 做静默预热，把 Kokoro 模型拉进操作系统页缓存，降低首句子进程的模型加载耗时。

## 验证

- Go：`go vet ./...` 绿；全量 `go test ./...` **0 失败**（含 `internal/tts` 的 onnx 单测：假 runner 驱动 argv/读盘/解码全路径，另有 env-gated 真机集成测试）；真机已用 Go `exec` 路径跑通中文 UTF-8 合成（`你好，我是月汐…` → 有效 RIFF WAV）
- Lint：`golangci-lint run ./internal/app/... ./internal/tts/...` **0 issues**
- 前端：`tsc --noEmit` 通过；`vitest run` **171 files / 1313 tests** 全绿；`generate-bridge --check` 无契约漂移；`vite build` 成功
- 摘要校验：runtime 与 model 两个上游资产已逐一下载并核对 SHA-256 与字节数，与代码内 pin 完全一致

## 安装包

- `release/out/Lunitide-Setup-0.4.55-x64.exe`（真签名，Authenticode 已验、DigiCert 时间戳）
- `release/out/SHA256SUMS.txt`
- 从 0.4.54 升级
