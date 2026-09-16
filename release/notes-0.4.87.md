# Lunitide 0.4.87

AgentHub 安装切页不中断、权限改成和工作台一样的芯片、默认进历史对话、浅色任务中心跟主题走；本机系统代理覆盖更新检查、网页抓取、天气和技能归档。覆盖升级仍走 GitHub `latest.json`。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把 AgentHub 焊进 SessionPage。

已装 0.4.86 可直接覆盖安装。`v0.4.87` 的 Setup + `latest.json` 挂到 GitHub Latest。不要覆盖 `v0.4.86` / `v0.4.85` / `v0.4.84` 的 digest。

## 1. AgentHub

- 确认安装后在本机继续跑完；切走首页或侧栏不会再开第二次安装。
- 权限是芯片菜单：手动审批 / 自动审批 / 完全访问，和工作台同一套说法。
- 只有打开会话时才进对话壳；全局「新对话」让给 Hub 自己的一颗。
- 默认「历史对话」列出各家线程。任务中心仍给旧 `agent_hub_tasks`，不再把空任务列表当成没会话。
- 浅色主题下任务中心统计卡、筛选和列表跟 `--hub-panel`，不再锁黑底。
- 探测测试不再读注册表 PATH，避免本机已装的 cursor-agent 污染夹具。

## 2. 系统代理与更新

- Windows Internet Options 代理抽到 `egressproxy`，语音安装继续用同一套。
- 更新 feed / 安装包、网页抓取、天气、技能归档和工具网页在设了代理时走系统代理，不再钉死直连 IP。
- 已有同名 GitHub Release 时只标 Latest，不再 `--clobber` 冲掉本机签过名的包。

## 3. 周报

- Work「写周报」仍走 `office.generate`。
- AgentHub「写周报 Markdown」仍只出 Markdown，不生成 Office。

## 4. 不做

- 不做 Memory Fabric / Paddle 下载管线 / 媒体 PRD。`docs/design/jiyishengji` 只是设计与交互原型，不是本版本产品能力。
- 回滚仍是空操作。不做后台预下载、强制更新、WinSparkle。
- 不重打 `v0.4.86` / `v0.4.85` / `v0.4.84`。

## 产物

- `release/out/Lunitide-Setup-0.4.87-x64.exe`
- `release/out/latest.json`
- `release/out/SHA256SUMS.txt`
