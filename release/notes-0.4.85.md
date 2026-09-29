# Lunitide 0.4.85

把 AgentHub 对话页收成「一家 Agent 一扇窗」：官方品牌标、统一语音/发送、安装与连接分开，进入时回到该 Agent 最近一次记忆。工作区文件树仍在右侧。覆盖升级仍走 GitHub `latest.json`。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把 AgentHub 焊进 SessionPage。

已装 0.4.84 可直接覆盖安装。`v0.4.85` 的 Setup + `latest.json` 由 `publish-update-feed` 挂到 GitHub Latest。不要覆盖 `v0.4.84` 的 digest。

## 1. AgentHub 对话

- 侧栏三家：Codex / Cursor / Kimi 官方标 + 灯 + 连接。历史任务进旧任务中心。
- 进入 AgentHub 或点某家，打开该家最近一次会话；首页发送也续写同一条记忆，不再默默再开一条。
- 未安装才给安装说明。「未连接」只请登录后点连接。探测失败只重试，不当成没装。
- 顶栏标题跟已打开会话的 CLI，不再在 Kimi 会话上写 Cursor。
- 输入条语音/发送收成一组小圆钮。路径芯片可点。
- 「写周报 Markdown」只出现在文档/其他，并写明不要生成 Office。Work 里「写周报」仍走 `office.generate`。

## 2. 工作区

- 文件页右侧是本机目录树，可拖宽、可折叠。
- 代码任务文件显示相对路径，两个同名文件能分开。

## 3. 测试与 CI

- Windows 超时杀进程树时，孙进程来不及写 pid 文件不再判失败。
- Windows CI 继续隔离 `internal/app` 覆盖率和 CGO race。

## 4. 不做

- 不做 Memory Fabric / Paddle 下载管线 / 媒体 PRD。
- 回滚仍是空操作。不做后台预下载、强制更新、WinSparkle。
- 「一键安装」仍是打开官方说明并复制命令，不会替你静默装 CLI。

## 产物

- `release/out/Lunitide-Setup-0.4.85-x64.exe`
- `release/out/latest.json`
- `release/out/SHA256SUMS.txt`
