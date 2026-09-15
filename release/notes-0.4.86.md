# Lunitide 0.4.86

AgentHub 按本机确认安装 CLI、浅色主题不再被强制成黑底，组合包不再因 MCP 预检整包失败，卡住的故障会话不会再自动顶上来。覆盖升级仍走 GitHub `latest.json`。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把 AgentHub 焊进 SessionPage。

已装 0.4.85 可直接覆盖安装。`v0.4.86` 的 Setup + `latest.json` 由 `publish-update-feed` 挂到 GitHub Latest。不要覆盖 `v0.4.85` / `v0.4.84` 的 digest。0.4.85 里「一键安装只打开说明页」已被本版本取代。

## 1. AgentHub 安装与连接

- 未装 Codex / Cursor CLI / Kimi 时，确认后在本机执行官方安装命令，再探测、必要时走该 CLI 自己的 `login`。不再打开文档页冒充安装。
- 登录用探测到的可执行文件路径，不依赖引擎进程当时的 PATH。
- 安装受请求超时约束；单条配方和登录不会把整次 Bridge 调用拖过上限。
- 灯变绿仍以探测 `available` 为准。厂商 CLI 登录弹它自己的浏览器时，那是登录，不是我们打开说明页。

## 2. 会话不再卡死

- 新对话、Ctrl+N、顶栏返回都开空白首页，不会自动跳回故障/失败会话。
- 首页发送始终新建会话，可选目录；已有会话也能改项目目录、产物目录和任务类型，并写回该会话。
- 界面只显示用户原句，隐藏重启通知和场景包装。
- 工作区不展示 `$null` 和扫描垃圾名。

## 3. 办公菜单、插件、组合包

- 设置 → 办公菜单不再出现 AgentHub。工作/AgentHub 切换不受该开关控制。
- 插件页标题是「插件」，组合包单独成节。内置组合包只能撤下；导入包可删除。Codex / Cursor / Kimi 不是组合包。
- 周报/调研组合包不再依赖本机 `uvx` 的 `fetch`/`time`。MCP 预检失败记为跳过，不整包失败。
- AgentHub「写周报 Markdown」仍只出 Markdown，不生成 Office。Work 里「写周报」仍走 `office.generate`。

## 4. 主题

- AgentHub 主区、侧栏、安装卡、输入条和消息跟应用浅色/深色走，不再整页锁黑。

## 5. 测试与 CI

- Windows Quality 的 race 与 coverage 在运行时 abort 时各重试一次。真实 DATA RACE 或断言失败不重试。

## 6. 不做

- 不做 Memory Fabric / Paddle 下载管线 / 媒体 PRD。
- 回滚仍是空操作。不做后台预下载、强制更新、WinSparkle。
- 不重打 `v0.4.85` / `v0.4.84`。

## 产物

- `release/out/Lunitide-Setup-0.4.86-x64.exe`
- `release/out/latest.json`
- `release/out/SHA256SUMS.txt`
