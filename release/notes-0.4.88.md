# Lunitide 0.4.88

工作台深色底铺成纯黑，设置/技能/插件/专家/资产等同壳页面不再露出海军蓝；连接诊断不再在发出 HTTP 前因 UNIQUE 冲突失败；供应商可配 Responses API 与常用平台预填。覆盖升级仍走 GitHub `latest.json`。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把 AgentHub 焊进 SessionPage。

已装 0.4.87 可直接覆盖安装。`v0.4.88` 的 Setup + `latest.json` 挂到 GitHub Latest。不要覆盖 `v0.4.87` / `v0.4.86` / `v0.4.85` / `v0.4.84` 的 digest。

## 1. 模型调用

- 连接测试和对话在 AdmitCall 已写入 `model_call_attempts` 桩之后，再写同一条 UNIQUE 不再报冲突。此前会在约 3ms 内变成 `INTERNAL_ERROR` / `UPSTREAM_FAILED`，HTTP 根本没发出去。
- 本机 0.4.87 需要覆盖安装 0.4.88 后才会生效。

## 2. 供应商协议

- 协议增加 `openai_responses`（POST `/responses`），与 OpenAI-compatible、Anthropic、火山语音并列。
- 新建供应商可预填 DeepSeek、智谱 GLM、火山 Ark / Agent Plan / Coding Plan、阿里百炼、OpenAI、Anthropic。Agent Plan 和 Coding Plan 可以走 chat/completions 或 Responses，路径分别是 `/api/plan/v3` 和 `/api/coding/v3`。
- Responses 供应商只保留 llm / vision / gui，不会把图片生成模型改写成聊天模型。
- 连接探测和对话都写 `store:false`；深度模式只带 `reasoning.effort`，不带 Ark 的 `thinking`（OpenAI Responses 会 400）。
- 本版本不能替你在每个云厂商上做一次真实密钥联调。

## 3. 深色工作台

- Work 对话壳、顶栏、右侧工作区改成 `#000`。
- 设置中心：导航、关于、分组卡片、供应商表改成 `#000`；浅色主题覆盖保持白底。
- 同一套左侧入口里的技能中心、插件、MCP、专家中心、资产管理、项目管理、同事聊天内层面板改成 `#000`，避免和纯黑外壳对出海军蓝。
- 浅色不再用冰蓝天空和 `#f8fcff` 页面底。主按钮、选中态仍用青色 `--tide1`。

## 4. 工作台输入区

- 去掉「深度思考」「先搜索」快捷插入。
- AgentHub 组合框居中 720px；权限芯片和场景选择与 Work 同高、同透明底。
- 首页不再放模板芯片；侧栏只留「历史对话」，最近线程标题去历史页看。

## 5. 能力包

- 「调研工作包」「报告写作包」抓取改走内置门闸，不再预置 fetch MCP。
- 已卸载或失败残留遇到新清单时按新 spec 重装，不再报「能力包清单或操作已变化」。
- 市场里看到残留会带 `repair=true`。不删除目录里的这两个包。

## 6. 周报

- Work「写周报」仍走 `office.generate`。
- AgentHub 周报仍只出 Markdown，不生成 Office。首页不再放「写周报 Markdown」芯片，需要时在输入框写。

## 7. 不做

- 不做 Memory Fabric / Paddle 下载管线 / 媒体 PRD。
- 回滚仍是空操作。不做后台预下载、强制更新、WinSparkle。
- 不把 draw.io 装进产品。图仍走 Mermaid，文档仍走办公生成。
- 不重打 `v0.4.87` / `v0.4.86` / `v0.4.85` / `v0.4.84`。

## 产物

- `release/out/Lunitide-Setup-0.4.88-x64.exe`
- `release/out/latest.json`
- `release/out/SHA256SUMS.txt`
