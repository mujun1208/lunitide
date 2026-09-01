# 月汐 0.4.44 全局复盘 + 可落地整改合同

日期：2026-09-01  
产品：月汐 / Lunitide `VERSION` **0.4.44**（tag `v0.4.44`，commit `38128a5`）  
状态：**落地合同终稿 + 2026-09-01 语音专项修订**。后续施工只准按本文波次做；语音代码波以 `2026-09-01-voice-pipeline-remediation.md` 为准。和现码冲突时以现码为准，和愿景冲突时以本文 + 下表权威文档为准。  
画板摘要：`system-global-upgrade-report.canvas.tsx`；语音专项：`voice-pipeline-remediation.canvas.tsx`（和本文冲突时以本文为准；和语音专项冲突时以语音专项为准）。

本文回答四件事：现在是什么、打多少分、缺什么、下一刀改什么 / 不改什么 / 怎么验收。

**不能报：** 已全面对齐四家、13 个独立 Agent 已落地、主尺加权算术已经 4.80、正式发布已完成、系统全无报错 / 无闪退 / 无宕机。

---

## 0. 文档权威

| 文档 | 职责 | 冲突时 |
|---|---|---|
| **本文** | 0.4.44 之后的整改顺序、文件清单、验收、漏项补遗、报分句子 | 覆盖 2026-09-01 画板与口头分 |
| `2026-08-31-engine-gateway-48.md` | K/G/D/I 合同定义 | 施工语义仍以它为准 |
| `2026-08-31-peer-alignment-and-remediation.md` | 四家对标、主尺加权算法、规格禁止项 | 算法与禁止项以它为准；「已收/未收」以现码 + 本文为准 |
| `2026-08-31-four-surface-remediation.md` | 图1–6（同事 / 火山 / 会议浅色 / 月伴 / 克隆） | 代码已收的项并入本文 W1 桌面复验；「本波不能报 4.8」作废 |
| **`2026-09-01-voice-pipeline-remediation.md`** | 火山 / 本地 / 云端听写朗读 + 回合风暴 | **覆盖** 本文 §5.2 旧缺口与旧 W1-VOICE 验收；语音施工只准按它的 W-VOICE |
| `2026-08-31-p0-live-acceptance.md` | 真机勾选表 | 装官方 0.4.44 后在该表复勾；语音三卡须装含 W-VOICE 的包后再勾 |
| `docs/adr/ADR-002-ipc-security.md` | 本机管道 | 禁止 `0.0.0.0` / 非本机 |
| `docs/adr/ADR-004-host-secrets-and-engine-leases.md` | 密钥 | 禁止明文入库 / 进仓库 |
| `docs/P0-P1-CLOSEOUT.md` | 外证（干净机 / Authenticode 政策 / race） | **仍开着**。本机 `CN=Yy.MJ` 不关闭它 |
| 现码 + 单测 | 事实 | 覆盖所有画板 |

作废（不要按下面施工或报分）：

- 口头「一次打到四家满分 / 已全面对齐 / 13 个独立 Agent 已落地」
- `four-centers-landed-score` 把目录功能写成独立运行时
- `lunitide-peer-benchmark` 把岗位观感写成主尺加权
- `now-honest-score` 的「0.4.43 + 脏树」基线
- `2026-08-31-four-surface-remediation.md` 里「本波不能报主尺 4.8」（门闩已可报；跨面 e2e 挡的是加权乘满，不是门闩）
- `docs/P0-P1-CLOSEOUT.md` 里「安装包未签名」——对 **0.4.44 本机包** 过时；对外证 / 陌生机仍有效
- 把 Playwright 托管浏览器的首次安装，写成「已经劫持用户 Chrome」

---

## 1. 产品裁定（不要再选边）

| 层 | 是 | 不是 |
|---|---|---|
| 逻辑本体 | Go 引擎（`internal/app` + `ccapp` + `mcp6`） | OpenClaw TypeScript Gateway、Hermes Python、Cordis |
| 常驻 | 托盘 + 稳定管 `\\.\pipe\lunitide-gateway-<user>` | `:18789`、`0.0.0.0`、第二套 daemon |
| 工作台 | 可关的 WebView2 客户端 | 助手本体 |
| 路由 | `App.tsx` 状态机（无 React Router） | 浏览器多页应用 |
| 电脑控制 | 本机 UIA/Win32/像素阶梯，设置里人开 | 远程节点、任意窗口 5.0 |
| 浏览器 | 托管 Playwright（`browser.act`） | 用户日常 Chrome 当默认控电脑 |
| 专家 | 人设 + 同一工具环 + 可绑技能/MCP | 13 个独立进程 / 独立记忆主体 |
| 对标 | 偷寿命、合同、失败关闭 | 做成四家合集 |

对齐 = 该偷的寿命 / 合同 / 失败关闭。  
不对齐 = 做成那四家。规格禁止项缺了也不算月汐故障。

---

## 2. 复核：上一份报告的遗漏与修订

上一份画板方向对，下面这些必须写进合同，否则落地会偏。

| # | 上一份 | 修订 |
|---|---|---|
| R1 | 十二模块未单列自动化 / 记忆 / 浏览器 / 审批 | 本文 §4 补独立行；不改变主尺加权 |
| R2 | 「MCP 禁自动装」写混 | **拆开**：Playwright 托管浏览器，`navigate` 可 `ensurePlaywrightMCP`；click/type 未就绪必须 `BROWSER_MCP_NOT_READY`。`chrome-devtools` / `browsermcp` **禁止**自动装进月伴默认路径 |
| R3 | G1/D9/D11 直接当 0.4.44 官方包装已证 | 勾选发生在 **0.4.43 安装包 + `Lunitide-next` 旁路**。0.4.44 代码含 takeover，**装官方包后必须复验** |
| R4 | 未写自动化存储 | cron 是 `%LocalAppData%\Lunitide\` 下 `jobs.json` + `runs.jsonl`，**不要**为「统一」迁进 SQLite UoW |
| R5 | 未写设置双存储 | 外观 / 月伴开关 / 会议偏好大量 `localStorage`；供应商、通道、CC、记忆在 SQLite + DPAPI |
| R6 | 未写未接线页面 | `OntologyPage` / `OrgAdminPage` / `ConnectorPage` 有代码，**不在** `App.tsx` 侧栏。不当缺口开新产品 |
| R7 | 「全无报错」语气 | 合同是 fail-closed，不是零错误。没勾的是陌生机、UAC、未开 IM、跨面 e2e |
| R8 | 四表面桌面勾选未列入下一刀 | 通讯录点自己、浅色会议：装包后勾。**火山不切 sherpa 单测齐，但桌面已证 seed-asr 聋（VOICE-004）——不是再勾，见语音专项。克隆「启动中」代码已收的是禁试听，桌面已证宿主假 launching，见 W-VOICE-3** |
| R9 | P0-P1 外证未挂 | 本机签名 ≠ 正式发布。`P0-P1-CLOSEOUT.md` 仍挡「正式版」 |
| R10 | 分数算法未写出乘式 | 见 §3。跨面 e2e 不过，记忆/做完不能再往上加 |

这些修订 **不改** 主尺门闩 4.8、加权 4.65。

---

## 3. 现在能打多少分（每项满分 5）

必须分开报，禁止一张表偷换。

### 3.1 主尺（月汐自己）

加权：**寿命 30% + 桌面准度 30% + 做完才停 20% + 记忆 10% + 四中心 10%**。

| 分项 | 现码+单测 | 真机 | 目标 | 说明 |
|---|---|---|---|---|
| 寿命 | 4.8 | G1/G2/G3/D11 已在 0.4.43/旁路勾 | 4.8 | 装 0.4.44 官方包后复验 D11 |
| 桌面准度 | 4.6 | D6a/b/c 已勾；D12 空 | 4.8 | D12 真机不过，准度停 4.6 |
| 做完才停 | 4.6 | 未做端到端桌面抽检 | 4.7 | 跨面 e2e + 可选记事本抽检 |
| 记忆 | 4.6 | 工具单测在；跨面未勾 | 4.6 | 跨面不过不加分 |
| 四中心 | 4.5 | 目录约 4.8；运行时 3.0 | 4.6 | 不把 4.8 乘进独立运行时 |
| **主尺加权** | **4.65** | **门闩可报 4.8** | **≈4.74** | 0.3×4.8+0.3×4.6+0.2×4.6+0.1×4.6+0.1×4.5=4.65。整改后约 4.8/4.7/4.7/4.6/4.6=4.74，**乘不满 4.80** |

I（国内 IM）不过仍可报主尺门闩 4.8，对照 OpenClaw 停 4.6。  
R0 不过不得称正式发布。

### 3.2 产品模块

| 模块 | 现在 | 整改后 | 封顶原因 |
|---|---|---|---|
| 对话 | 4.3 | 4.6 | 不重写 `chat.go`；轨迹不是 DSH fork |
| 语音月伴 | 4.3 结构 / **2.4 真机** | 4.5 | 不做 Hermes 全双工；真机分见语音专项。W-VOICE 未勾禁止报 4.5 |
| 同事聊天 | 4.2 | 4.4 | 不嵌 SessionPage；互@是 P2 |
| 会议纪要 | 4.0 | 4.3 | 不是飞书会中 Agent |
| 后台设置 | 4.2 | 4.5 | 不拆 ASR/TTS 两个顶栏 |
| 电脑控制 | 4.5 | 4.6 | UAC / 全屏游戏 / 纯位图 |
| 浏览器 | 4.3 | 4.6 | 不劫持用户 Chrome |
| 项目管理 | 4.0 | 4.3 | 不换 Codex 沙箱树 |
| 技能中心 | 4.6 | 4.8 | 禁止自进化 SKILL.md |
| 插件中心 | 3.6 | 4.0 | 禁止 Cordis |
| MCP | 4.4 | 4.6 | 策展预置，不是商店 |
| 专家中心（目录） | 4.4 | 4.7 | 独立运行时另计 3.0 |
| 资产管理 | 3.8 | 4.2 | 本机模板，不是云 DAM |
| 自动化 | 4.4 | 4.5 | 文件存储保持；G2 已勾 |
| 记忆 / 个人智能 | 4.4 | 4.6 | 无自进化；跨面须勾 |
| 审批 / 治理 | 4.1 | 4.3 | 危险工具每次问 |
| 子智能体 | 3.4 | 3.5 | 只读 spawn，无路径树 |
| 搜索 | 3.8 | 4.0 | Ctrl+K 本机会话，不做云搜 |
| 诊断 / 更新 | 3.6 | 4.0 | 本机诊断；更新通道保持诚实 |
| 本体论（未接线） | 1.5 | 1.5 | **不做**，除非另立产品 |

十二模块算术（用户点名的那十二行，不含自动化等补行）：**现在 4.2 / 整改后 4.5**。

### 3.3 工程素质

| 点 | 现在 | 整改后 | 可落下一刀 |
|---|---|---|---|
| 系统架构 | 4.5 | 4.6 | 保持冻结 |
| 功能完整性 | 4.1 | 4.4 | 勾跨面 + I；勿接本体论 |
| 引擎 | 4.6 | 4.7 | 官方包装复验 D11 |
| 数据库连接 | 4.7 | 4.7 | 单写 WAL；cron 不入库 |
| 数据更新 | 4.5 | 4.6 | CAS + 幂等；冲突只 refetch |
| 数据查询 | 4.4 | 4.5 | 两套消息表分开；FTS 保持 |
| 权限审计 | 4.2 | 4.4 | 危险工具继续问 |
| 链路贯通 | 4.2 | 4.5 | 跨面 e2e |
| 无闪退 / 宕机 | 4.3 | 4.5 | 陌生机 R0；不是「永远零错误」 |
| 清晰 / 复用 | 4.1 | 4.3 | 不统一 mention 解析器 |
| 逻辑缜密 | 4.4 | 4.5 | BYOA 回落前缀不可改写 |
| 代码质量 | 4.3 | 4.5 | 本机不跑 race，交给 CI |
| 健壮 | 4.2 | 4.5 | P0-R3 陌生机 |
| 稳定 | 4.3 | 4.5 | 单连接池 |

工程算术：**现在 4.3 / 整改后 4.6**。

### 3.4 对照岗位（用对方定义考月汐）

| 尺子 | 现在 | 整改后 | 永远到不了 |
|---|---|---|---|
| 岗位观感（这台 PC 助手） | 4.6 | 4.7 | 5.0 |
| 给熟人试用 | 4.4 | 4.6 | 官方商店应用 |
| 给陌生人当发布物 | 3.6 | 4.2（R0 过后） | SmartScreen 消失（需 OV/EV） |
| 对 OpenClaw 岗位 | 3.3 | 3.4 | 4.7（海外通道 / iOS 不做） |
| 对 Codex 岗位 | 2.9 | 3.0 | 4.8（不换核） |
| 对 DSH 岗位 | 2.1 | 2.1 | 4.7（Cordis 禁止） |
| 对 Hermes 岗位 | 3.1 | 3.2 | 4.6（自进化禁止） |

---

## 4. 系统地图（落地时对照，不要重画）

### 4.1 进程与管道

| 角色 | 入口 | 事实 |
|---|---|---|
| Host | `cmd/desktop` → `Lunitide.exe` | WebView2；`--tray` 同 PE；互斥 `Local\lunitide-gateway` |
| Engine | `cmd/engine` → `lunitide-engine.exe` | SQLite、RPC、领域；认证会话结束只 `leave()` |
| 管道 | `\\.\pipe\lunitide-gateway-<user>` | 当前用户 DACL；nonce 在 `gateway-session.nonce` |
| 配对 | `sameUserPairedPID` | 任意正 PID 即过（靠 DACL+nonce，PID 不当隔离） |
| 看门狗 | `cmd/desktop/watchdog.go` | 引擎死后先放互斥再 `--takeover` |
| 健康 | `system.health` | 启 UI 前先探；`--rpc-health` 只做探活 |

数据根：`%LocalAppData%\Lunitide\`（`lunitide.db`、wal/shm、nonce、`engine.pid`、`logs/`、附件、`jobs.json`、`runs.jsonl`、DPAPI 密文）。

### 4.2 三面两库一契约

| 面 | 入口 | 写库 | 启动回合 |
|---|---|---|---|
| 文字会话 | `SessionPage` | `messages` | `message.append` → `chat.start` |
| 月伴 | 同一 `SessionPage` + `companion:true` | 同一 `messages` | 同一 `chat.start` |
| 同事 | **独立** `PeoplePage` | `people_messages` | `people.thread.send` → 绑定会话再 `chat.start` |

禁止合并两张消息表。禁止把 `PeoplePage` 嵌进 `SessionPage`。

### 4.3 存储分工

| 存哪 | 存什么 | 不要改成 |
|---|---|---|
| SQLite（`OpenSecure`，`MaxOpenConns=1`，WAL，109 条迁移到 0109） | 项目、会话、消息、同事、会议、专家、技能、插件、MCP 端点、记忆、治理、CC 配置/审计、供应商元数据 | 多写者 / URI DSN |
| DPAPI（Host 持有，引擎租约） | API Key 明文只在提交窗口 | 写入仓库或 SQLite |
| `jobs.json` / `runs.jsonl` | 自动化 | 强迁进同一 UoW |
| `localStorage` | 主题、月伴开关、会议偏好、启动页 | 假装「全在库里」 |
| `/PURGE` | 整个 data root（含上述文件） | 只删 db 留下 cron |

### 4.4 用户能进的面（`App.tsx` `Page`）

`home`、`projects`、`providers`、`settings`、`skill`、`expert`、`mcp`、`plugins`、`assets`、`automation`、`meetings`、`people`。  
个人会话 / 项目工作台是 overlay，不是 `Page`。  
设置 15 类：`general` `appearance` `profile` `providers` `voice` `meetings` `personal` `security` `browser` `computer` `channels` `subagents` `collab` `diagnostics` `about`。

---

## 5. 模块合同（现码 / 缺口 / 文件 / 禁止）

每块只列**还能做的下一刀**。已齐的见 §7，不要重做。

### 5.1 对话

**现码：** 流式、工具环、`chat.prefer` / `chat.turn.get`、写入失败横幅 + `\u2063persist-retry`、`ToolTrajectory`（只读追加）、`PendingMemoryBanner`。  
**缺口：** 跨面 e2e 未勾；轨迹不是 DSH 回放叉。  
**关键文件：** `web/src/session/SessionPage.tsx`、`ToolTrajectory.tsx`、`persistRetry.ts`、`internal/app/chat.go`、`chat_turn_persist.go`、`message_handlers.go`。  
**下一刀：** W1-CROSS。  
**禁止：** 新总线；合并 `messages`。

### 5.2 语音月伴

**现码：** 路径 `cloud` / `local` / `volc`；一张 `volc_speech`；源站只存 `https://openspeech.bytedance.com`；听写 `volc.seedasr.sauc.duration`；朗读 `seed-tts-2.0`；选火山失败停在火山（`companionListenFailover('volc','volc')==='volc'`）；插话开关不因选路径被强开。  
**缺口（2026-09-01 桌面，覆盖旧「装包后勾」）：** 火山握手过、麦有能量、`voice.append` 空字（协议 `result` 只认对象、`result_type=single`、热词 context 非法、后端按 `IsDefault` 可能挑到 TTS）；本地 GPT-SoVITS 因 `jieba_fast/dict.txt` 缺失秒退，`RefHost` 仍报 launching，设置页永久「启动中」；`sendAndChat` 月伴 in-flight 会 `resetCompanionTurn` 再开新流 + FORCE_COMMIT 每 400ms 补发，退回打字同一句连发。云端（Web Speech + Edge）同一回合核已炸。  
**权威：** `2026-09-01-voice-pipeline-remediation.md`。  
**关键文件：** `web/src/session/companion/*`、`web/src/session/SessionPage.tsx`、`internal/voice/volcsauc/`、`internal/app/voice_handlers.go`、`internal/tts/refhost.go`、`web/src/settings/refEnginePreview.ts`。  
**下一刀：** W-VOICE-1 → 2/3 并行 → 4 → 5。旧 W1-VOICE 三行验收作废。  
**禁止：** 重写 `useCompanionMachine` 整表；LLM 挂 openspeech；静默切 sherpa；改 `E:\GPT-SoVITS\start-api-cpu.bat`；静默回落晓晓冒充克隆。

### 5.3 同事聊天

**现码：** 独立页；1:1 / 群 / `@` 两跳；绑定会话；`computer.act` 拉黑；点自己不出 leftover 专家标题（单测在）。  
**缺口：** 装包后勾通讯录点自己；智能体免配对偏软（文案，不改协议）。  
**关键文件：** `web/src/people/PeoplePage.tsx`、`peopleRoster.ts`、`internal/app/people_agent.go`、`internal/storage/sqlite/people.go`。  
**下一刀：** W1-PEOPLE。  
**禁止：** 嵌进 SessionPage；开放和自己开会话。

### 5.4 会议纪要

**现码：** 麦 / 系统声 + ASR；`pickDefaultVoice` 必须只挑 `asr`；诚实 `needs_summary`。  
**缺口：** 浅色左右栏装包后勾。  
**关键文件：** `web/src/meetings/MeetingPage.tsx`、`meetingAsr.ts`、`web/src/provider/modelKind.ts`。  
**下一刀：** W1-MEET。  
**禁止：** 做成飞书会中入会。

### 5.5 后台设置

**现码：** 15 类齐。  
**缺口：** 配对模式 / Chrome MCP / BYOA 回落文案保持可测。  
**关键文件：** `web/src/settings/SettingsPage.tsx`、`settingsNav.ts`。  
**下一刀：** W2-COPY。  
**禁止：** ASR/TTS 各开顶栏。

### 5.6 电脑控制

**现码：** 阶梯点击；`frameId`；`verifyAfter` 失败即错；关着月伴不代开（D9 旁路已勾）。  
**缺口：** D12 真机；官方包装复验 D9。  
**关键文件：** `internal/ccapp/`、`internal/app/m10_cc_handlers.go`。  
**下一刀：** W1-D12、W1-D9。  
**禁止：** 同事 / 评议会 / 子代理调用 `computer.act`；报任意窗口 5.0。

### 5.7 浏览器（从电脑控制拆开）

**现码：** `browser.act`；`navigate` 可安装 Playwright 预置；click/type/snapshot 未就绪 → `BROWSER_MCP_NOT_READY`（不是空成功）。  
**缺口：** B1 真机未勾。  
**关键文件：** `internal/app/browser_automation.go`、`web/src/mcp/McpPage.tsx`。  
**下一刀：** W1-B1。  
**禁止：** 自动装 `chrome-devtools` / `browsermcp`；引擎去抢用户 Chrome；空成功。

### 5.8 项目管理

**现码：** 项目 / 1–9 阶段 / 工作台壳 / 交付物门。`PlanPage` 在设置安全页，不在工作台侧栏。  
**缺口：** 不用为对标 Codex 重做。  
**禁止：** `apply_patch` 树 / 换核。

### 5.9 技能 / 插件 / MCP / 专家 / 资产

| 面 | 现码 | 下一刀 | 禁止 |
|---|---|---|---|
| 技能 | 人装；远程失败回捆绑目录 | W2-IDX 条数诚实 | 自进化 SKILL.md |
| 插件 | 能力包 = 技能+MCP+门闸；市场本地 | 对外禁止说 Cordis | 热替换运行时 |
| MCP | 策展预置；preset id 落盘 | W1-B1 | Chrome 类自动装 |
| 专家目录 | 两类 + `catalogItemId` | 对外说人设+工具环 | 13 进程（P2） |
| 资产 | 模板 / 脚手架 | 保持本机 | 云 DAM |

### 5.10 自动化

**现码：** 同一 `chat.start`；跟引擎活；G2 已勾；`jobs.json` / `runs.jsonl`。  
**禁止：** 第二执行核；为整齐迁库。

### 5.11 记忆 / 审批 / 子智能体 / 搜索 / 诊断

| 面 | 现码 | 下一刀 | 禁止 |
|---|---|---|---|
| 记忆 | FTS；确认台；三面横幅 | W1-CROSS | 改 SKILL.md |
| 审批 | 治理 + 命令白名单 + CC 审计 | 保持每次问 | 会话批准高危 |
| 子智能体 | 只读 spawn/join | 失败文案 | 路径寻址树 |
| 搜索 | Ctrl+K 本机 | 可选体验 | 云索引 |
| 诊断 | `system.health` / 导出 | 保持 | 对公网遥测默认开 |

---

## 6. 竞品：只偷匹配功能

| 匹配功能 | 该看谁 | 该偷 | 禁止写成 |
|---|---|---|---|
| 关窗不断助手 | OpenClaw / Hermes | 已对齐；复验官方包装 | TS Gateway / `:18789` |
| 本机点桌面 | 月汐自己 | D12 真机 | 远程节点、任意窗口 5.0 |
| 工具剖面 | OpenClaw | 已收三档；同事拉黑 CC | 20 通道 |
| 轨迹 | DSH 思想 | 已挂只读视图 | Cordis / fork 运行时 |
| 语音 / 会议 | 月汐自有 | 桌面复验 | Ashley 全双工、会中入会 |
| 记忆确认 | Hermes | 跨面勾 | 自进化 |
| 技能人装 | Hermes 技能面 | 索引诚实 | ClawHub |
| MCP | OpenClaw 货架思想 | 人装 + B1 | 用户 Chrome = 默认电脑控制 |
| 编码 | Codex | BYOA 回落可见 | 换核 / 沙箱树 |
| 插件运行时 | DSH | 不学 | Cordis |
| 多 Agent 办公室 | Codex / Hermes | 不立项则改口 | 目录分报成运行时 |

---

## 7. 已齐、不要再当缺口施工

- 引擎断 RPC 不退；稳定管；先重连再 spawn
- 0.4.44 死引擎先放互斥再 `--takeover`（**代码齐，官方包装待复验**）
- 电脑控制第一次要人开；过期 `frameId` 拒绝
- 同事 / 评议会 / 子代理不能 `computer.act`
- `browser.act` 未就绪报错（单测齐，B1 真机待勾）
- 飞书 / 钉钉 HTTP 回程代码在；企微走 `aibot_send_msg`
- cron 跟引擎；G2 已勾
- `memory.search` / `get` 只打已确认；FTS
- 技能挂专家，不自动钉输入框
- 入站路由与 MCP 预置 id 落盘
- BYOA `.last-reply.txt`；微信 / QQ 不能开入站
- 火山 URL / Resource-Id 保存时规范化
- 选火山不切 sherpa（单测齐；桌面已证不切，但听写本身聋——改协议，不是改这条）
- 点自己不露 leftover 线程标题（单测齐，桌面待勾）
- `generate:bridge` 与 asr/tts kind 已同步
- Quality 三轮：`go test ./...` + 164/1217 vitest（0.4.44 发布时）
- 本机签名安装包 + tag + SHA 已推（陌生机未证）

---

## 8. 架构冻结（任何波都不得碰）

1. 不合并 `messages` / `people_messages`
2. 不把 `PeoplePage` 嵌进 `SessionPage`
3. 不改 `PERSONAL_CHAT_PROJECT`（`⁣月汐·普通对话`）或引擎 KB/Plugin/Handoff 的 `local-user`
4. 不把 `@` 菜单扩到全部已启用专家
5. 不把 `chat.go` 重写成新总线
6. 不整表统一 Go/TS mention 解析器
7. 不开放「和自己开会话」
8. 不给 ASR / TTS 各开供应商顶栏
9. 不在 `volc_speech` / openspeech 上挂对话 LLM
10. 不重写 `useCompanionMachine` 状态表（只修非法事件映射）
11. 不改 GPT-SoVITS 启动脚本 / 端口 / 盘符，不静默回落晓晓
12. 不启动 P2「13 个独立 Agent 运行时」（除非产品书面立项）
13. 不把 API Key 写进仓库
14. 不对公网开口；不做 WhatsApp / Telegram / Slack / Signal / iOS 节点
15. 不自动把 `chrome-devtools` / `browsermcp` 装进默认电脑控制
16. 不把 cron 为了整齐迁进 SQLite
17. 不接线 `OntologyPage` 当本波功能
18. 不覆盖官方 `Lunitide.exe` / `lunitide-engine.exe`（只准用户跑 0.4.44 Setup）

---

## 9. 波次（一波不过不开始下一波）

原则：先装官方包，再勾真机，再动文案回归，最后才谈思想项。P2 默认跳过。

```text
W0  用户安装官方 0.4.44
        │
        ▼
W-VOICE  语音全链路代码（回合风暴 → 火山出字 / 本地诚实 → 云端一轮 → 新包装勾）
        │         与剩余 W1 真机可并行；W2 之前必须过 V-TURN + 至少一条听写通路
        │
        ├─ 并行 W1 其余真机（D12 / B1 / 跨面 / 同事 / 会议）
        ├─ 并行 W1-R 陌生机 R0（挡正式发布，不挡门闩 4.8）
        │
        ▼
W2  合同回归（文案 / BYOA / 索引条数 / 企微失败可见）——无回归不新开功能
        │
        ▼
W3  P1 剩余思想（误唤醒等，不新开语音产品）
        │
        ▼
W4  仅当书面确认「独立 Agent 办公室」——先改文案
```

### W0 — 安装（无代码）

2026-09-01 已收：官方目录无 next；`DisplayVersion` / `--version` / `--rpc-health` 均为 0.4.44 `engine=ready`。

| ID | 动作 | 验收 | 不要 |
|---|---|---|---|
| W0-1 | 关掉旁路 `Lunitide-next` / `lunitide-engine-next` | 任务管理器里没有 next | 不要杀官方目录里的正式 exe 来「替换」 |
| W0-2 | 跑 `E:\Trae-Work-Projects\lunitide\release\out\Lunitide-Setup-0.4.44-x64.exe` | 安装目录版本 0.4.44；`--rpc-health` `engine=ready` | 不要用未签名 / 旧 0.4.43 包 |
| W0-3 | 核 SHA-256 | `33bd063e62cd3bb4df1d066bc4dad95da3ec6389b27f504d6ae360de6191f63b`（含 W2/W3 的 0.4.44 包） | 不要改文件后再装 |

通道要测火山听/读时：设置 → 语音选「火山」；供应商基础 URL 只填 `https://openspeech.bytedance.com`；专属 API Key；听写 `volc.seedasr.sauc.duration`；朗读 `seed-tts-2.0`。

### W-VOICE — 语音全链路（必须改代码，2026-09-01 插入）

权威：`2026-09-01-voice-pipeline-remediation.md`。0.4.44 桌面已证：火山聋、本地假启动中、回合连发。旧 W1-VOICE「几乎不改代码」作废。

| ID | 动作 | 通过 |
|---|---|---|
| W-VOICE-1 | 同一句只一次 `chat.start`；月伴 in-flight 禁止 reset 再开流 | 代码已合；真机须新包勾 V-TURN |
| W-VOICE-2 | 火山：只挑 ASR；`result` 对象/数组；`result_type=full`；合法 dialog_ctx | 代码已合；真机须新包勾 V-VOLC-ASR |
| W-VOICE-3 | RefHost 进程死 → offline + lastErr；设置页超时文案 | 代码已合；真机须新包勾 V-LOC-HOST；不改 bat |
| W-VOICE-4 | 云端走完一轮（依赖 1） | 代码已合（文案+去重）；真机须新包勾 V-CLOUD |
| W-VOICE-5 | 打新包装勾专项 §7 | 包已打 0.4.46（签名 + Verify-Release 通过）。官方目录仍是 0.4.45 时须先跑 Setup。用 0.4.44 / 0.4.45 勾语音 / 「打断后灯稳」= 无效 |

### W1 — 真机（语音以外几乎不改代码）

只在 **已登录、已装 0.4.44 的 Windows** 勾（语音项除外，须装含 W-VOICE 的包）。单测绿 ≠ 勾。勾进 `2026-08-31-p0-live-acceptance.md`。

| ID | 合同 | 操作 | 通过标准 |
|---|---|---|---|
| W1-G1 | G1 | 开工作台 → 关窗口 → 任务管理器 | **已勾 2026-09-01**：引擎 27088 未换，health ready |
| W1-G3 | G3 | 再开窗口 | **已勾 2026-09-01**：仍是引擎 27088 |
| W1-D9 | D9 | 设置里关电脑控制 → 开月伴 | 横幅「不会自己打开」；`computer.act` → M10-CC-012 |
| W1-D11 | D11 | 只杀 `lunitide-engine.exe`，不杀托盘 | **已勾 2026-09-01**：27088 → 50708，health ready |
| W1-D12 | D12 | 诱发 UAC（装驱动 / 改系统一类） | 出现 `user.ask` /「我不能代点是」；自己点 |
| W1-B1 | B1 | 设置 → MCP 卸 Playwright → 对话或月伴 `browser.act` click | 报 `BROWSER_MCP_NOT_READY`；**不能空成功**。验完可再装回 |
| W1-CROSS | 跨面 | 文字「以后回答默认用中文」→ 三面点确认沉淀 → 月伴「继续刚才的」→ 同事 `@` 专家同一句 | 三面都接得上偏好 |
| W1-PEOPLE | 图1 | 先开专家会话 → 通讯录点自己 | 右侧是自己名片，无专家标题、无输入框；**不要**切 `rail=me` |
| W1-MEET | 浅色 | 浅色主题开会 | 左栏右栏同为浅底 |
| W1-VOICE | **作废，改走 W-VOICE** | 见 `2026-09-01-voice-pipeline-remediation.md` §7 | 旧标准（「启动中不可点」「VOICE-004 不跳 sherpa 即过」）**不再当作通过** |
| W1-DONE | 可选抽检 | 记事本写入可见字，中途截图 | 不说「完成了」 |
| W1-I1 | I1 可选 | 飞书**或**钉钉：白名单，关窗 | 再开工作台看得到入站。不过对照停 4.6 |
| W1-I4 | I4 可选 | 同上通道自动跑完 | **原 IM 私聊**看得到回复 |

W1 代码门（若桌面失败先查，不要先改架构）：

```text
go test ./internal/ccapp/accuracy -count=1
go test ./internal/app ./internal/imapp ./cmd/engine ./cmd/desktop -count=1
```

环境：`LUNITIDE_CC_ACCURACY=1`（D6 复跑时）。

### W1-R — 发布外证（与 W1 并行，挡住「正式版」）

| ID | 动作 | 验收 |
|---|---|---|
| W1-R1 | 已完成：干净树 + tag `v0.4.44` | commit `38128a5` |
| W1-R2 | 已完成：本机 Authenticode + RFC3161 | `CN=Yy.MJ`，仅当前账户信任 |
| W1-R3 | **未做：** 干净 Win10 **和** Win11：装 / 启动 / 升 / 卸后数据仍在 / `/PURGE` | 按 `release/Test-Install.ps1` + `P0-P1-CLOSEOUT.md` |
| W1-R4 | 未做：CI `windows-cgo-race` 对 tag 的证据留下 | 本机不跑 90m race |

W1-R3 不过：**可以**继续报门闩 4.8，**不可以**报正式发布。

### W2 — 合同回归（有失败才改代码）

目标：W1 若露出文案 / 回落 / 索引撒谎，只改该点。无失败则本波标完成、不扩功能。

2026-09-01 代码波已按合同先收（不扩功能、不改冻结表、不 `generate:bridge`）。真机仍走 W1。

| ID | 改什么 | 不要改成 | 验收 | 代码 |
|---|---|---|---|---|
| W2-COPY | IM 配对=第一人入白名单写清；Chrome MCP=人装、非默认电脑控制 | 恐吓式安全页 | 设置快照 / 文案单测 | 已收 |
| W2-F1 | BYOA 失败前缀「已回落月汐」不可被模型改写 | 用 Codex 换引擎 | 单测前缀 | 已收 |
| W2-IDX | 技能/插件市场标明捆绑目录，远程失败保留条数 | ClawHub | 失败仍能列出捆绑条数 | 已收 |
| W2-I4W | 企微无 writer：失败写入会话，禁止只打日志 | 恢复 `agentid=1` | 单测 | 已收（原有） |
| W2-B1 | click 走 `errBrowserMCPNotReady`，禁止空成功；**只有 navigate 可 `ensurePlaywrightMCP`** | 自动装 chrome-devtools | 单测无 Output | 已收 |

### W3 — P1 思想剩余（可选，不换核）

| ID | 从谁偷 | 改什么 | 不要改成 | 代码 |
|---|---|---|---|---|
| W3-VOICE | 月汐自有 | 误唤醒 / 麦权限按既有 RCA 收口 | Ashley 全双工重做 | 已收：光名+日常句不唤醒；麦权限 `denied` |
| W3-BYOA | Codex | 可选「本机 Codex 未在 PATH」 | 沙箱树 | 已收 |
| W3-MEM | Hermes | 专家跨会话摘要须人确认（已有则只复验） | 改 SKILL.md | 已收：个人智能页写明须确认 |

### W4 — P2（默认跳过）

只有产品书面确认要「独立 Agent 办公室」才开工。做就全做：

1. 每个 `ExpertKindAgent` 独立会话存储，记忆主体 = 专家
2. Agent 消息自己入队，不依赖用户再按发送
3. 任务锁可观测
4. `@` 跳数与预算进设置，默认仍封顶
5. 智能体免配对改为显式策略，默认收紧
6. 仍然禁止同事 `computer.act`；仍然禁止自进化 SKILL.md

不立项：专家中心对外永远说「人设 + 工具 + 技能」。

---

## 10. 报分规则（落地过程中只准用这些句子）

**可以写：**

- 月汐主尺：**K/G/D 门闩可报 4.8**。拆分加权 **4.65**。岗位观感 **4.6**。
- 语音月伴：**真机 2.4**（2026-09-01：三卡都不能对话）。结构分 4.3 不成交。W-VOICE 未勾禁止报「能对话 / 4.5」。
- 装完 0.4.44 并勾完 W1 后：加权约 **4.7**，仍不是 4.80。
- 对照 OpenClaw **思想**接近寿命/剖面；**岗位** 3.3；I 真机齐了对照最高 4.7。
- 对标 Codex / DSH / Hermes 岗位分 2.9 / 2.1 / 3.1；DSH 运行时和 Hermes 自进化 **故意不对齐**。
- 四中心目录约 4.8，独立运行时 3.0，主尺打折 4.5。
- Playwright 是托管浏览器，**navigate 可以首次预置**；click/type 未就绪报 `BROWSER_MCP_NOT_READY`。这不是劫持用户 Chrome。

**不可以写：**

- 已全面对齐四家 / 全没问题 / 全无报错。
- 13 个独立 Agent 已落地。
- 主尺加权算术已经 4.8。正式发布已经完成。
- 插件中心已经是 Cordis。
- 已经官方支持用户 Chrome 作为默认电脑控制。

---

## 11. 完成定义

**门闩保持 4.8：** W0 完成 + W1 中 G1/G3/D11 已在官方 0.4.44 复验通过。D9 仍要人点月伴横幅。D12/B1/跨面不过，门闩句子不变，加权不加。

**加权可报到约 4.7：** 上一条 + D12 + B1 + W1-CROSS 通过。

**可称正式发布：** W1-R3 陌生机装/升/卸/PURGE 通过。本机签名不够。

**不可称完成的：** P2、跨平台、OV/EV 证书、四家岗位拉齐。

---

## 12. 给施工者的第一刀

W2 / W3 **代码已收**（2026-09-01）。W0 已装。**下一刀是 W-VOICE**（语音完全不能对话），不是继续只勾旧 W1-VOICE。其余 W1 真机可并行。不要 `generate:bridge`，不要改迁移，不要碰冻结表，不要开 W4/P2，不要改 GPT-SoVITS bat。

1. 按 `2026-09-01-voice-pipeline-remediation.md`：先 W-VOICE-1（回合），再并行 2/3，再 4，再打包装勾。  
2. 其余 W1 真机仍按本表勾进 `p0-live-acceptance.md`（跳过已作废的 W1-VOICE）。  
3. 失败先对照语音专项根因表——是协议 / 宿主 / 回合，不是重做架构。  
4. 需要报分时只准用 §10 + 语音专项 §8 的句子。语音真机未勾禁止报「能对话」。
