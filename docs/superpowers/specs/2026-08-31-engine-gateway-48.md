# 引擎本体 + 常驻模式：落地对照（主尺 4.8）

日期：2026-08-31  
状态：终稿，按波落地。每一波一个 PR，验收不过不开始下一波。  
画板摘要：`gateway-desktop-full-mark.canvas.tsx`（和本文冲突时以本文为准）。

作废（不要按下面施工）：

- `canvases/lunitide-vs-openclaw-plan.canvas.tsx` 里「Gateway 可选 / 改造后 4.2」
- `canvases/desktop-control-vs-openclaw.canvas.tsx` 里「无 frameId / 只有 IAccessible」
- 口头「这台 PC 助手改造后 4.9」「任意 Windows 程序都能点中 = 5.0」

## 0. 裁定（不要再选边）

| 层 | 是 | 不是 |
|---|---|---|
| 逻辑本体 | Go 引擎（`internal/app` + `ccapp` + `mcp6`） | Python Hermes、OpenClaw TypeScript Gateway |
| 常驻模式 | 托盘拥有引擎寿命 + 稳定本机管道 | 第二套 daemon、`0.0.0.0` |
| 工作台 | 可关的客户端 | 助手本体 |
| 调试 | 先 `go test` / 无窗口起引擎 | 必须先开 Gateway 再排错 |

偷 OpenClaw 的寿命，不偷它的网关实现。偷 Hermes 的「内核可单独跑」，不写成 Python、不自进化技能。

## 1. 三把尺子（落地后）

| 尺子 | 现在 | 第 0–4 波 | 第 5 波后 | 封顶原因 |
|---|---|---|---|---|
| 这台 Windows PC 助手（主尺） | 3.3 | 4.6 | **4.8** | UAC / 独占全屏游戏 / 纯位图远控点不了 |
| 对照 OpenClaw | 3.1 | 4.6 | **4.7** | 海外 20 渠道 / iOS 节点不做 |
| 四中心功能核 | 4.8 | 4.8 | **4.9** | 不是外部商店镜像 |

主尺加权：寿命 30% + 桌面准度 30% + 做完才停 20% + 记忆 10% + 四中心 10%。  
第 5 波后拆分目标：寿命 4.8 / 准度 4.8 / 做完 4.7 / 记忆 4.6 / 浏览器 4.6 / 四中心 4.9。

主尺 4.8 看 **K/G/D**。对照 4.7 另加 **I**。I 不过仍可报主尺 4.8，对照停在 4.6。

## 2. 现码对照（第 5 波后，不要按旧绑死点施工）

下面是**当前代码**，不是开工前的旧表。验收仍以 §6 的 K/G/D/I 为准；G1/G3/D1/D6/D11 必须在本机 Windows 桌面跑过才能报实测 4.8。

| 位置 | 事实 |
|---|---|
| `cmd/engine/main.go` | 已认证 RPC 结束只 `leave()`；仅握手 ACK 失败 `cancel()`。写 `engine.pid`。密钥经纪在引擎进程（DPAPI `secretlease.LocalClient`） |
| `AdmitClient` | owner = 拉起引擎的 PID；同用户其它 PID 自动配对（管道 DACL 已排除其它用户）。`SessionGate(8)` |
| `cmd/desktop/main.go` | 稳定管 `\\.\pipe\lunitide-gateway-<user>`；先重连再拉起；`--tray` 隐藏窗口；托盘「退出」按 `engine.pid` / pipe 对端 PID 停引擎（重连后 `command==nil` 也能停）。任务管理器杀窗口不杀引擎。`--rpc-health` 第二条客户端只做 `system.health` |
| `docs/adr/ADR-002-ipc-security.md` | 稳定名 + 多客户端配对；禁止非本机 / `0.0.0.0` |
| `internal/ccapp/service.go` | 命名点击阶梯：Invoke/MSAA → Win32 → 本机像素 + hit-test。`verifyAfter` 截图失败 = error。宽屏映射目的地是 `ScreenSize()`，不走 1280 缩图当桌面 |
| `internal/ccapp` `filterInput` | 有过截图后，x/y、id、name、光标点击都要当前 `frameId` |
| `internal/app/chat.go` | 月伴工具环 24 步（与桌面环相同）；电脑控制第一次要人在设置里开；`browser.act` 不落到 `media.play` |
| `internal/app/companion_capabilities.go` + `ensureCompanionCapabilities.ts` | 月伴（Go/Web）都不得静默 `Enabled=true`；第一次控电脑要人在设置里开 |
| `internal/app/chat_memory.go` | `memory.search` / `memory.get` 只打已确认事实和摘要；FTS5 + 注入 |
| `internal/imapp` + `im_inbound.go` | 飞书 / 企微 / 钉钉 Stream 入站；I4 结束后官方 API 回原会话 |
| `web/src/settings/SettingsPage.tsx` | 钉钉入站表单；微信/QQ 不能开入站；关窗口 ≠ 退出助手 |
| 同事 / 评议会 / 子代理 | `computer.act` 已拉黑，保持 |

## 3. 六波

第 0 波今天可改，不碰 UI。第 0 波没过 **K0/K2**，不要开始第 1 波。

### 第 0 波 — 内核可活

改：`cmd/engine/main.go` 去掉「认证会话结束就退出」。同一 `hostPID` 允许两条 RPC；断一条，另一条还能 `chat.start`。

不改：`ccapp`、UI、管道名、`AdmitClient` 的 owner 规则、密钥经纪。

验收：K0、K2。

### 第 1 波 — 托盘常驻

改：

- 稳定管：`\\.\pipe\lunitide-gateway-<user>`（仍只给当前用户 DACL）
- 托盘（可先 hidden desktop）拥有引擎；工作台改为连接已有管道，连不上再请托盘拉起
- `AdmitClient`：owner = 托盘 PID；同用户已配对客户端可连（本机第一次自动过）
- 密钥经纪迁到托盘/引擎，窗口不再是密钥根
- 更新 `docs/adr/ADR-002-ipc-security.md`：稳定名 + 多客户端配对，**仍然禁止**非本机 / `0.0.0.0`

不改：`computer.act` 语义。不要让 `go test ./internal/app` 必须先起托盘。

验收：K1、G1、G2、G3。

### 第 2 波 — 桌面准度底座

改：`internal/ccapp/service.go`（点击、`verifyAfter`、`filterInput`）；`internal/app/chat.go` 月伴步数与完成话术；`companion_capabilities.go`；去掉 `browser.act` 空 click → `media.play`；记事本/计算器夹具。

合同（本波先落地 1–6，阶梯第 4 层和高分图像素放到第 5 波）：

1. 命名点击 `InvokeUI` / ValuePattern 失败 = 结构化拒绝，不静默中心盲点  
2. 有过截图后，x/y、id、name、光标点击都要当前 `frameId`  
3. `verifyAfter` 截图失败必须 error（M10-CC-011）  
4. 校验区和上一张截图同范围  
5. 月伴解析 `STALE_FRAME` / refused / verify failed，不说「完成了」  
6. 桌面任务 24 步；电脑控制第一次要人在设置里开  
7. 网页只走 `browser.act`

不改：CUA Driver、新 CDP、劫持用户 Chrome。同事/评议会/子代理继续不能用 `computer.act`。

验收：D1–D9。

### 第 3 波 — 浏览器合同、记忆工具、国内入站

改：

- `browser_automation.go`：snapshot → act → snapshot 合同（本波先写死边界；完整快照环可在第 5 波收口）
- `chat_memory.go`：模型工具 `memory.search` / `memory.get`，只打已确认事实和摘要，不打原始流水账
- 钉钉官方 **Stream 模式**入站（本机出站长连接，对标飞书 `FetchFeishuEndpoint` + websocket）。文件：`internal/imapp/dingtalk.go`（新）、`inbound.go` 放开 `KindDingTalk`、`im_inbound.go` `loopDingTalkInbound`
- 飞书 / 企微 / 钉钉共用 `parkInboundMessage` / `kickInboundChat` / `turnEquipment`
- **I4**：自动跑结束后把助手可见回复推回原 IM 会话（官方发消息 API + 保存的 chat/sender id）。禁止只写工作台、禁止只靠模型「想起」`im.send`
- 长链成功仍只建议 `skill.create`，人装才上架

不改：WhatsApp / Telegram / Slack；个人微信非官方协议；自进化 SKILL.md；记忆改成 MD 文件。

要改掉的旧测试：`internal/app/im_handlers_test.go`「钉钉入站必须失败」；`imapp/inbound_test.go` `KindDingTalk` → `ErrInboundKind`。  
Bridge：`im.inbound.deliver` 的 `kind` 枚举加上 `dingtalk`（走 generate:bridge）。

验收：I1、I2、I4；记忆工具有单测。

### 第 4 波 — 诚实文案与工具剖面

改：`web/src/settings/SettingsPage.tsx`

- 钉钉入站表单（与飞书同形：App/Secret、连接并等待配对、白名单、自动跑）
- 微信 / QQ：**不能**开入站；文案写明「本机已登录客户端代发，不能收消息」
- 「助手在托盘运行」；退出助手 ≠ 关窗口
- 工具剖面：`minimal` / `coding` / `同事`（设置项，默认不改现网行为）

验收：I3；设置文案和进程对得上。

### 第 5 波 — 收到主尺 4.8

改：

- 点击五层阶梯 + 点后 hit-test（UIA 焦点/名称，或窗口文本，或 `ElementFromPoint`）
- 第 4 层：高分图像素，映射**不**走 1280 缩图；必须当前 `frameId`
- 夹具矩阵：记事本、计算器、资源管理器、一个 WPF、一个 Electron；断言窗口内容。目录建议 `internal/ccapp/accuracy/`（CI 可 skip，本机 Windows 必跑）
- 引擎死亡后托盘/工作台拉起引擎（D11）。关窗/杀托盘不杀引擎（G1），不要做成「杀托盘才重启引擎」
- 记忆 FTS（已确认事实 + 摘要）
- 浏览器完整快照环
- 活技能索引失败不再只剩 9 条；MCP 端点存预置 id

验收：D10–D12；四中心可报 4.9。

## 4. 桌面点击阶梯（第 2 波底座 + 第 5 波收口）

| 层 | 用于 | 失败则 |
|---|---|---|
| 1 UIA Invoke / ValuePattern / ElementFromPoint | Win32 / WPF / WinUI / 标准对话框 | 下一层 |
| 2 MSAA 默认动作 | 老控件、部分 Office | 下一层 |
| 3 Win32 控件消息（BN_CLICKED 等） | 原生按钮有 control id | 下一层 |
| 4 高分图像素 + 当前 frameId | Electron / 自绘 / 树空 | 点后 hit-test，对不上就失败 |
| 5 拒绝 | UAC、文件框要人、保护进程、校验失败 | error + `user.ask`，不说完成了 |

字面「任意 Windows 程序都准确点中」不验收。UAC 安全桌面、独占全屏游戏、纯位图远控必须失败关闭。

## 5. 国内通道（不是 20 渠道）

| 通道 | 现在 | 做到 |
|---|---|---|
| 飞书 | Webhook 出站 + 长连接入站 | 关窗仍收；回复推回原会话 |
| 企业微信 | 同上 | 同上 |
| 钉钉 | 仅 Webhook 出站 | 官方 Stream 入站 + 回复推回 |
| 微信 | 本机代发 | 保持；不能开入站 |
| QQ | 本机代发 | 保持；不能开入站 |

## 6. 硬验收清单

| # | 波 | 路径 | 通过 |
|---|---|---|---|
| K0 | 0 | 同一 owner 两条 RPC，断一条 | 另一条还能 `chat.start`；引擎不退出 |
| K2 | 0 | `go test ./internal/app` | 不依赖 named pipe / 托盘，仍绿 |
| K1 | 1 | 非 owner 工作台/CLI 经配对连上 | 第二条客户端 `chat.start` |
| G1 | 1 | 开工作台 → 关窗口 → 任务管理器 | 引擎仍在，PID 未换 |
| G2 | 1 | 关窗口后等 cron | 有 run 记录 |
| G3 | 1 | 再开工作台 | 连上同一引擎，会话还在 |
| D1 | 2 | 计算器观察「7」再 `click name=7` | Invoke 成功；审计不是 mouse_click 兜底 |
| D2 | 2 | 旧 frameId 点像素 | `COMPUTER_STALE_FRAME`，`executed=false` |
| D3 | 2 | 4K 点 24px 命名按钮 | 落在控件矩形内 |
| D4 | 2 | 月伴「在记事本写入三行」 | 不在第 10 步假完成 |
| D5 | 2 | 打开网页填表 | 只走 `browser.act` |
| D6 | 2 | 记事本/计算器夹具 | 断言窗口文本/值 |
| D7 | 2 | verify 截图失败 | 工具 error，月伴不说「完成了」 |
| D8 | 2 | 只传 name、不传 frameId | `COMPUTER_STALE_FRAME` |
| D9 | 2 | 月伴第一次要控电脑 | 设置里要人开 |
| I1 | 3 | 钉钉 Stream 白名单消息 | 入站会话出现；关工作台仍写入 |
| I2 | 3 | 三通道自动跑 | 同一 `chat.start` / `turnEquipment` |
| I4 | 3 | 入站自动跑结束 | 回复出现在原 IM 会话 |
| I3 | 4 | 设置里开微信入站 | 不能开 |
| D10 | 5 | Electron 树空高分图像素 | 点后 hit-test；打偏则 error |
| D11 | 5 | 引擎 PID/RPC 死亡 | 托盘或工作台拉起引擎，工作台能重连。关窗/杀托盘必须留引擎（G1） |
| D12 | 5 | UAC 点确认 | 拒绝 + `user.ask` |

## 7. 仍不做

TypeScript 重写、Hermes Python、OpenClaw+Hermes 串联、WhatsApp/Telegram/Slack/Signal 铺齐、iOS 节点、ClawHub、Cordis、自进化改 SKILL.md、200 张人设卡当运行时、劫持用户 Chrome、个人微信非官方协议、对公网开口、报任意程序 5.0。

## 8. 复核时补上的遗漏（相对上一份画板）

1. 画板「架构」页仍写四中心不进方案 / 五波后 4.8 → 已改：4.9 在第 5 波。  
2. `AdmitClient(hostPID)` 使「无托盘两个客户端」不能在第 0 波一次做完 → 拆成 K0（同 owner）/ K1（配对）。  
3. 入站自动跑不会回 IM → 加 I4。  
4. 稳定管道名与 ADR-002 冲突 → 第 1 波必须改 ADR，不能只改代码。  
5. 密钥经纪在桌面进程 → 第 1 波必须迁，否则关窗后模型/MCP 会断。  
6. 钉钉入站有反向单测 → 第 3 波要改测试和 bridge 枚举。  
7. I 项不应挡主尺 4.8。  
8. 第 2 波与第 5 波点击范围拆开，避免第 2 波范围爆炸。

## 9. 开工第一刀

`cmd/engine/main.go`：已认证会话结束只 `leave()`，不要 `cancel()`。加单测：同一 owner 两条连接，断第一条，第二条 `chat.start` 仍成功。不碰 UI。
