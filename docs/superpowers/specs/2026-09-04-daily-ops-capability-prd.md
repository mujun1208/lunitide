# 日常任务能力整改 PRD：五路路由 + 独立 GUI 兜底

日期：2026-09-04  
状态：**superseded。** 开工只认 [2026-09-04-daily-ops-executable-prd.md](./2026-09-04-daily-ops-executable-prd.md)。本文方向仍有效，锁死项（空树读号、画面不变立刻失败、R1 含 kb.search、P0 改 Key 池等）以下文为准。  
产品：月汐（Lunitide）Go Engine + React WebView2 + SQLite  
前序对照（作废处以下文为准）：

- [2026-09-04-capability-slots-design.md](./2026-09-04-capability-slots-design.md) — 角色 + 传感器方向保留；**「不新增 kind=gui」作废**
- [2026-09-04-remediation-upgrade-prd.md](./2026-09-04-remediation-upgrade-prd.md) — #9 前台验窗继续有效，本文件把它推广成 L0

本文件是日常操作能力的**唯一事实源**：现码缺口、五路路由、独立 GUI 类型、角色表、分期与验收。沿用冻结：

- 不新开 daemon；内核永远 SQLite  
- 不改 poison / 说完再答 / 月伴 TTS 音色 / 专家表  
- `desktop.open` 成功 = **前置后前台命中**，列表命中不算  
- 电脑控制仍走现网门（`cc.getConfig`、启用向导、`securityLevel`）；月伴**不得自开**  
- 保护会话「月伴对话 / 对话 / Chat」不可删、不可改名  
- 不把 `ENGINE_UNAVAILABLE` 改成假成功  

---

## 0. 对照原话与分析结论（不得漏项）

| 来源 | 原话 / 结论要点 | 本 PRD |
|---|---|---|
| 用户 | 按现有产品 + 前面不足 + 分析结论出完整整改 PRD | 全文 |
| 用户 | **可以配置 GUI 模型，单独出来一个类型，用来兜底** | §3.2、§5 #D、`kind=gui` |
| 用户 | **现在没有 GUI 模型**；要最好的替代，或可集成的开源 GUI 模型 | §3.2.1 无模型默认链、§3.2.2 开源接入 |
| 用户 | 深度分析现在电脑控制还缺什么，补进 PRD | §2.4 现码盘点、§5 #I–#O |
| 前序不足 | 只有 `llm/vision/image/video/asr/tts`；无 embedding / GUI 槽 | §2、§3 |
| 前序不足 | 记忆/专家库 FTS，`kb_chunks.embedding` 空列 | §5 #E |
| 前序不足 | `planVerifyPrompt` 喂同一聊天模型、只看执行日志 | §5 #C |
| 前序不足 | 一把 Key；429 整轮死 | §5 #F |
| 前序不足 | 模型自己挑工具，没有任务路由 | §5 #A |
| 市场结论 | 日常有 API 的任务：API 主干远好于纯视觉 CUA | §1、§4 R1 |
| 市场结论 | 五路还不够：缺结构化观察、动作后传感器、登录/支付人接管 | §4、§5 #B #G |
| 市场结论 | 纯视觉 CUA 覆盖面高、日常天气/音乐/文档成本差一个数量级 | GUI **只兜底**，禁止当默认手 |

### 0.1 这次能不能彻底

| # | 路径 | 这次能否彻底 | 诚实边界 |
|---|---|---|---|
| A | 查天气不再开浏览器乱点 | **产品路径能彻底**（路由锁死 R1 工具面） | 公开网页没有稳定 API 时仍走 `web.search` 摘要，不编温度 |
| B | 打开/播放/点网页不再假成功 | **L0 推广能彻底** | Windows 抢前台失败、播放器不暴露 Now Playing：只说一次原因 |
| C | 计划验证不再和主模型共嗨 | **能彻底**（先 L0，再独立 judge 角色） | 用户显式勾「验证用主模型」除外 |
| D | GUI 可配、单独类型、只兜底；**没配也能兜一层** | **能彻底**：无 GUI 时用 SoM + 已有视觉槽读编号；有 GUI 时走专用目录 | 视觉槽只能选编号，不能猜像素；未配视觉也未配 GUI 才停成「无法执行 + 观察」 |
| E | 同义检索 | 配了 embedding 能彻底；未配合法降级 FTS | 机务定位器继续吃关键词，FTS 不撤 |
| F | 主模型 429 换备用 Key | **能彻底**（只轮配额类） | 真配错的 401 不轮，避免掩盖 |
| G | 登录 / 支付 / UAC / 验证码 | **人接管能彻底**（停 + `user.ask`） | 不替用户输密码、不代付 |
| I | 点按钮不再盲像素 | **能彻底**（本轮先 observe；角标已有） | 自绘/游戏树空时仍要视觉读号或 GUI |
| J | 多个无名按钮点得准 | **能彻底**（只许点本帧 ID） | 同名控件仍可能撞，必须用 B1/B2 |
| K | 复杂窗口控件被 80 节点切掉 | **能提示再观察** | 不保证一次看完微信/Excel 功能区 |
| L | 点了但画面没变算失败 | **能彻底**（`verifyAfter` 不变则 `ok:false`） | 纯视觉动画闪烁可能误判，允许一次 `wait until=change` |
| M | 提权/网银/自绘 | **能停手** | 不能穿过 UIPI 去点已提权窗 |
| O | 中文进输入框 | **能彻底**（CJK 走 `paste`） | 个别只收按键的游戏除外 |

---

## 1. 为什么改、改哪一层

2026 年日常桌面 Agent 的主干不是「再接一个会点屏幕的模型」，而是：

1. **先分类任务，再打开对应工具面。** 现码只有 `autoToolProfile`（寒暄缩成 `minimal`，其余甩全套工具）。`complexity.decide` 只给长对话贴 `plan.run` 提示，**不是**日常路由。模型看见 `desktop.open`、`browser.act`、`computer.act` 会为「北京天气」去开网页。
2. **先结构化观察，再像素。** 现码已经有 Win32/UIA（`desktop.type`、`cc.observe_ui`）、CDP/Playwright 快照（`browser.act`）、前台验窗（`openedWindowConfirmed`）。缺的是：**失败之后才允许视觉**，以及视觉必须走独立目录，不能和「看图说话」抢同一个 `vision` 槽。
3. **动作后读世界，再让模型说话。** `desktop.open` 已锁死前台。`media.play` 的媒体键、`browser.act` 的 click、`computer.act` 的像素点击，仍可能「工具返回 ok、世界没变」。
4. **登录墙是人，不是模型。** `browser.act` 文案已写 Login/2FA/captcha/文件选择器停手，但没有结构化接管码，模型仍可能改去 `computer.act` 或（若混用）vision 盲点。

**采用：** 五路任务路由 + 角色绑定 + **两个新 kind（`embedding`、`gui`）** + **无 GUI 时的 SoM 读号替代**。  
**不采用：** 八个 kind 全上；嵌入 UI-TARS Desktop / 自启 vLLM；把视觉页当成 GUI 基座去猜坐标；安装包夹带 7B 权重；未配齐模型就报残缺。

`kind=gui` 页签仍保留（用户要单独类型）。**今天产品里没有 GUI 模型是默认态**，P0/P1 不得依赖用户先买/先下专用权重。无 GUI 时的默认手是 §3.2.1，不是空转失败。

---

## 2. 现码事实（整改必须站在这上面）

### 2.1 已有 kind 与目录

| Kind | 设置页签 | 谁在用 |
|---|---|---|
| `llm` | LLM | 会话 / 月伴（月伴另用 `pickCompanionFlashModel` 启发式 flash/lite） |
| `vision` | 视觉模型 | `VisionDescribeCatalog`：看图、转写、OCR 提示词 |
| `image` / `video` | 生图 / 生视频 | `image.generate` / `video.generate` |
| `asr` / `tts` | 语音模型 | 听写 / 朗读（火山一张供应商） |

`NormalizeKind` 未知值回落 `llm`。新 kind 必须进 `ValidKind`，否则存成 llm，GUI 会污染对话目录。

### 2.2 已有工具（路由必须点名，禁止发明平行工具）

| 路 | 现成工具 | 现成缺口 |
|---|---|---|
| 信息 | `web.search`、`web.fetch`、`memory.search`、`kb.search` | 无天气/车票结构化适配器；无路由禁止「为查事实开 GUI」 |
| 本地 | `desktop.open`（已有前台 L0）、`desktop.type`（UIA 编辑框）、`media.play`、`excel.parse`、Office `*.gen` | `media.play` 媒体键无播放态 L0；打开再填已用 `companionGoalIsOpenOnly` 区分 |
| 网站 | `browser.act`（snapshot ref，禁止猜 CSS） | 突变后没有强制「URL/快照变化」才算成功 |
| 生成 | `docx.gen` / `excel.gen` / `pptx.gen` / `pdf.gen` / `html.gen` / `workspace.write`；文案已禁止用 COM/`command.run` 拼 Office | 模型仍可能改去开 Word 点菜单 |
| 像素 | `computer.act`（展开到受治理 `cc.*`）；`cc.observe_ui` 已有 UIA 树（上限约 80 节点） | 模型可在 UIA 能点时直接截图点；无独立 GUI 目录 |
| 计划 | `plan.run` 最多 6 步 | 验证喂**同一** `model`，只看 log |
| 人 | `user.ask`（1–8 题，不可自动批准）；UAC / 另存为已会停成 ask | 缺 `login` / `pay` / `2fa` / `captcha` 原因码 |
| 画像 | `autoToolProfile` 寒暄→minimal | 不是五路；任务句仍甩全套含 `computer.act` |

电脑控制：`ccToolDefinitions` 仅在 `Enabled && !EmergencyStopped` 时挂上 `computer.act`。月伴默认拒绝裸 `cc.*`、`command.run`、`im.send`。子代理**永不**拿桌面控制。这些门继续有效；GUI 兜底**走同一扇门**，不另开后门。

### 2.3 已锁死、本整改不得回退

- `openedWindowConfirmed`：假前台是 Lunitide / 列表里有进程 ≠ 成功  
- `companionGoalIsOpenOnly`：纯打开 settle；打开再填继续桌面环  
- 成功打开禁止对用户说「无法执行」  
- `desktop.type` 找不到具名框必须 `无法执行`，禁止 Ctrl+F 蒙  
- Office 生成禁止 COM / Python / `command.run` 拼包（现工具描述已锁）  
- `browser.act` 禁止回落到 `media.play` 或桌面像素  

### 2.4 电脑控制现码盘点（对照 TRAE / Codex，只认月汐）

TRAE 会话环境缺「任意 Win32 看屏点选」——**那不是月汐的缺口**。月汐已经是本机桌面 Agent：`computer.act` 展开到受治理 `cc.*`，有截屏、`frameId`+DPI/多屏、UIA/`observe` 角标、具名点击阶梯、UAC/另存为停手、紧急停止、审计、节奏限制。对照应是「手已经有了，闭环和路由还没接完」，不是「去装 Computer-Use MCP」。

| 层 | 现码 | 还缺（本整改要锁） |
|---|---|---|
| 终端 | `command.run`（月伴默认拒绝） | 不补进月伴 |
| 文件 | `workspace.*` + Office `*.gen` | 不恢复 COM 生成 |
| 浏览器 | `browser.act` snapshot ref | 突变 L0；登录墙 reason（#B #G） |
| 打开/播放 | `desktop.open` 前台 L0；`media.play` | 播放态 L0（#B） |
| 填表 | `desktop.type` 具名 UIA | 回读框值（#B）；CJK 优先 paste（#O） |
| 看树 | `observe`：UIA→MSA，`assignNodeIDs`（B1/E1），`AnnotateCapture` 画角标，`rememberHits` | **会话环不强制先 observe**；Acc 路径丢掉真无名节点；默认 80 截断无 `truncated`（#I #J #K） |
| 点 | 具名：`InvokeUI` → Win32 → 像素+hit-test；`verifyAfter` 再截一张 | `verifyAfter` **画面不变仍返回成功**；`InvokeUI` 只按 Name，多个 `button (unnamed)` 会点错（#J #L） |
| 像素 | `screenshot` + x,y + `frameId` | 允许不观察就盲点；没有运行时「视觉只回 markId」（#I #D） |
| 滚/拖 | `MouseScroll`；拖只收坐标 | 拖不能 `id=`；虚拟列表不会 ScrollItem 进视口（#K #O） |
| 窗 | list/focus/window_action；保护 explorer/lunitide | 不观察未前置的窗；不跨虚拟桌面保证（诚实） |
| 门 | CC 关闭则工具表没有 `computer.act`；月伴不自开 | 失败文案必须指向启用向导，不装成功（#N） |
| 消息 | `im.send` 五通道；入站仅飞书/企微/钉钉+白名单 | 个人微信/QQ **不是**电脑控制缺口，不在本整改做协议（#N） |
| 语音 | 月伴 ASR/TTS/HomeWake | 不是桌面点选；不改音色 |
| 定时 | `automation.*` cron，跟引擎进程走 | 关掉月汐就停；不新开 daemon（#N） |

**对 Codex Windows Computer Use：** 他们是「截屏→视觉→鼠标」主干。月汐主干应是 **UIA/打开/CDP**，视觉只读角标。不要把产品改成 Codex 克隆。

**对 OpenClaw/Hermes 网关：** 月汐已是桌面常驻应用+三家入站，不是 TRAE 那种纯会话。缺的是个人微信协议和手机节点——**本整改非目标**。

---

## 3. 锁死架构

### 3.1 角色（设置「能力路由」）与 Kind

Role 描述**这次调用干什么**。Kind 描述**目录/API 形状**。用户可见：供应商页签按 Kind；「能力路由」六个下拉按 Role。

| Role | 绑到 Kind | 缺省 | 空配置 |
|---|---|---|---|
| `chat` | `llm` | 会话/首页当前模型 | 已有 `chat.prefer` → `kindDefault` |
| `flash` | `llm` | 把 `pickCompanionFlashModel` 显式化，可手选 | 回落 `chat` |
| `vision` | `vision` 或 `llm+supportsVision` | `VisionDescribeCatalog` | 已有顺序试；**绝不**纳入 `kind=gui` |
| `embed` | **新 `kind=embedding`** | 未配则只 FTS | 合法降级，设置不报残缺 |
| `judge` | `llm`（**不是**新 kind） | 默认 `flash` | L0 通过则不调用；禁止默认等于 `chat`，除非用户勾「验证用主模型」 |
| `gui` | **新 `kind=gui`** | 未配则走 §3.2.1（SoM + 已有 `vision` 读号） | 视觉也没有：`无法执行` + L0 观察，不报残缺 |
| `listen` / `speak` | `asr` / `tts` | 已有 | 已有 |
| `image` / `video` | `image` / `video` | 已有 Catalog | 已有 |

**新增 kind 仅两个：`embedding`、`gui`。**  
**不新增 kind：** `judge`、`ocr`、`rerank`、`guardrail`。OCR 继续并进 `vision` 提示词。Rerank P2 可选且默认关。Guardrail 保持执行模式 / hooks / 电脑控制策略，不模型化。

### 3.2 `kind=gui`（单独类型）与无模型替代

**配置面**

- 供应商页新增页签 **「GUI 模型」**，与「视觉模型」并列，文案分开。  
- 视觉页：看图、转写、OCR、月伴看屏；**额外**在 §3.2.1 里只允许「读 SoM 编号」。  
- GUI 页：专用接地模型（UI-TARS 类）。今天默认空。  
- 空态（锁死文案）：「未配置 GUI 模型时，无名按钮会先画编号，再用已有视觉模型读号。没有视觉模型则停止并说明原因。」不出现红字残缺。  
- 协议沿用 `openai_compatible`。本机权重走用户已有的 LM Studio / vLLM / 本机 OpenAI 兼容口，**引擎不自启推理进程**（MiniCPM-o 的 `omni.*` 通道不复用到 GUI，避免和第二套 llama 抢端口）。  
- `kindDefault`：GUI 目录自己的全局默认；`CatalogForKind(gui)` 默认再备用。  
- **禁止**把同一行同时标成 `vision` 和 `gui`。要两用就建两行（可以同一 `modelId`、不同 kind）。

**运行时降级链（无 GUI 是默认，不是残缺）**

```
R2/R3 目标
  → 结构化工具（UIA / desktop.* / media.play / browser.act snapshot）
  → L0 失败或树/DOM 缺失
  → L0.5 Set-of-Mark（编号来自本帧 UIA/DOM，不是模型猜框）
  → 选执行器（只选一个）
        1. CatalogForKind(gui) 非空 → 专用 GUI 模型
        2. 否则 VisionDescribeCatalog 非空 → 已有视觉槽只回答 {markId}
        3. 否则 无法执行 + L0 观察 + 提示可在「GUI 模型」接入开源权重
  → 运行时用 computer.act / browser.act 执行「点编号 N 对应的控件 id/ref」
  → 再读一次 L0
```

硬规则：

1. **禁止**在 R1 / R4 / 寒暄路径调用 GUI 或 SoM 读号。  
2. **禁止** UIA/CDP 还没试就截图。  
3. **禁止** `chat` 角色参与点选。视觉槽参与时**只输出 markId**（整数，且必须落在本帧编号表）；禁止输出 x,y / 百分比坐标。GUI 目录才允许在「编号表为空、纯自绘控件」时输出经 `frameId` 校验的归一化坐标。  
4. **禁止**把 `cc.window_list` / 进程列表 / 截图猜测当成功。  
5. 任何视觉输出必须落成现有受治理动作。禁止模型直接打鼠标驱动。  
6. 电脑控制关闭时：GUI 页可配，运行时与 `computer.act` 一起不可跑。  
7. 子代理、同事画像、机务专家：**永不**进入 GUI 或 SoM 读号。  
8. 登录墙 / 支付 / UAC / 2FA / 验证码 / 系统文件框：**禁止**视觉硬点，转 §5 #G。

#### 3.2.1 今天没有 GUI 模型：最好的替代（采用）

现码已经有 `cc.observe_ui`（UIA 树，约 80 节点）、`desktop.type` 具名框、`browser.act` snapshot ref、`VisionDescribeCatalog`。缺的是「树在、名字空」时的桥。

**采用 SoM 读号，不采用再接一个点选基座。**  
现码 `observe` **已经** `assignNodeIDs` + `AnnotateCapture`（Peekaboo 角标）+ `rememberHits`。缺的不是再画一遍编号，而是：会话环强制先 observe、无名节点只许点 ID、画面不变算失败、以及把已画角标的图交给视觉槽只问 markId（#I #J #L）。

| 方案 | 做法 | 为何不/采用 |
|---|---|---|
| A. 空 GUI 就停 | UIA 失败直接无法执行 | 否。用户现在没有 GUI 模型，日常无名按钮会全灭 |
| B. 把会话 LLM 当 CUA | 截图喂 chat，让它点像素 | 否。和主模型共故障域，且会把 R1/R4 带进乱点 |
| C. 嵌 UI-TARS Desktop / 自启 vLLM | 产品内再养一个 Electron 或 Python 推理 | 否。新 daemon，违背冻结；安装包已排除 Omni 权重同理 |
| D. OmniParser + YOLO | 第二套检测栈再让 LLM 选 | 否。多进程、多权重，收益小于 SoM |
| **E. SoM + 已有视觉槽读号** | UIA/DOM 画 1…N，vision 只回 markId，运行时点该节点 | **采用。零新模型，接地误差有界** |
| F. 可选 `kind=gui` | 用户把 UI-TARS 等接到 OpenAI 兼容口 | **采用为升级档**，不是默认依赖 |

E 的契约：

- 输入给视觉槽：本帧 SoM 截图 + JSON 编号表 `{id,name,controlType,bbox}` + 用户目标一句。  
- 输出：严格 JSON `{"markId":n}`。非法、越界、非 JSON → 当失败，不重试 chat。  
- 执行：用编号表里的 UIA `id` 或 browser snapshot `ref`，**不用**模型给的像素。  
- 这不是「vision 冒充 GUI 基座」，是「vision 做选择题」。和看图说话共用目录，但提示词/schema 隔离（`guiSomPickPrompt`，禁止走普通 describe）。

#### 3.2.2 可集成的开源 GUI 模型（升级档，不进安装包）

产品只认 **OpenAI 兼容 chat.completions + 图片**。引擎不下载、不加载 GGUF（与 MiniCPM-o 通道隔离）。GUI 页提供「推荐接入」说明，用户自己起服务或填云端兼容口。

| 优先级 | 模型 | 许可 | 怎么进月汐 | 诚实边界 |
|---|---|---|---|---|
| **首选接入** | [ByteDance-Seed/UI-TARS-1.5-7B](https://huggingface.co/ByteDance-Seed/UI-TARS-1.5-7B) | Apache-2.0 | 用户用 vLLM 打出 `/v1`，供应商协议 `openai_compatible`，kind=`gui` | 约 14–16GB 显存；OSWorld ~27%，只够兜底不够当主干 |
| 次选（文档全、有 GGUF） | [UI-TARS-7B-DPO](https://huggingface.co/bytedance-research/UI-TARS-7B-DPO) / [2B-SFT](https://huggingface.co/bytedance-research/UI-TARS-2B-SFT)（[官方 2B GGUF](https://huggingface.co/bytedance-research/UI-TARS-2B-gguf)） | Apache-2.0 | 同上；2B 可挂本机 LM Studio（现网已认 localhost + `lm-studio` 占位 Key） | 2B 接地明显弱于 7B，只适合低显存机器 |
| 接地专项 | [OS-Atlas-Base-7B](https://huggingface.co/OS-Copilot/OS-Atlas-Base-7B) | Apache-2.0 | 能打成 OpenAI 兼容再挂 gui 页；否则 **不**为它写 transformers 加载器 | 基础动作模型，不是完整 agent |
| 坐标无关（研究向） | [GUI-Actor-7B](https://huggingface.co/microsoft/GUI-Actor-7B-Qwen2.5-VL) | MIT | **P2 以后再评估**；现网加载器不是 OpenAI 兼容 | 高分屏接地好，集成税高 |
| 不采用进产品 | UI-TARS-2 全家桶、UI-TARS Desktop、ShowUI 当默认、OmniParser | — | 2 的完整能力未以可商用小权重开放到「接上就能跑」；Desktop 是平行应用 | 不嵌、不自启 |

**产品预置（P1 设置文案，不是自动安装）：**

1. 「用已有视觉模型读编号」——默认，无需新供应商。  
2. 「接入本机 UI-TARS（LM Studio / vLLM）」——一键填 `http://127.0.0.1:1234/v1` 或 `:8000/v1` 的草稿行，kind=`gui`，Key 填 `lm-studio`。  
3. 「接入云端 OpenAI 兼容 GUI」——用户自填 Base URL + 模型 ID。

安装包、`omni.install`、Quality 体积契约：**不**增加 GUI 权重。谁要 7B 谁在本机下。

### 3.3 五路任务路由（日常主干）

在 `chat.start` 组完工具表**之前**判定 `TaskRoute`，用路由收工具面。这是新层，挂在现有 `autoToolProfile` / companion deny / CC 门之外，不取代它们。

| Route | 用户意图（例） | 打开的工具 | 明确禁止 |
|---|---|---|---|
| **R1 信息查询** | 天气、股价、新闻、公开事实、手册问答 | `web.search`、`web.fetch`、`memory.*`、`kb.search`、`kb.cite`、`user.ask` | `desktop.*`、`media.play`、`browser.act`、`computer.act`、GUI |
| **R2 本地应用** | 打开汽水/记事本、播放周杰伦、在前台框填身份证 | `desktop.open`、`desktop.type`、`media.play`、`excel.parse`、Office `*.gen`（若同时要落盘）、`computer.act`（仅 CC 开且 UIA 已失败） | 为「查天气」打开天气网；未失败先 GUI |
| **R3 网站操作** | 打开某站并点击、填公开表单、读页面 | `browser.act`、`web.fetch`、`user.ask` | `media.play`、桌面像素、猜 CSS；登录墙改 GUI |
| **R4 内容生成** | 写报告/PPT/表格/小工具/生图生视频/攻略 | `docx.gen` `excel.gen` `pptx.gen` `pdf.gen` `html.gen` `workspace.*` `image.generate` `video.generate` `web.search`（调研） | 打开 Word/Excel **点菜单**来生成；GUI |
| **R0 寒暄** | 你好、今晚吃啥闲聊 | 现 `minimal`：`web.search/fetch`、`memory.*`、`user.ask` | 桌面 / 浏览器自动化 / GUI |

**判定顺序（锁死）：**

1. **规则优先**（零模型）：沿用并扩展 `autoProfileChatHints` / `autoProfileTaskHints`；纯打开走已有 `companionGoalIsOpenOnly`；含「天气/气温/股价/今日金价」等 → R1；含「打开/启动/播放」且指向本机应用 → R2；含「打开网站/登录后帮我点」→ R3；含「写一份/生成 PPT/画一张」→ R4。  
2. **冲突时按禁止表收口**：既像查询又像打开（「打开浏览器查天气」）→ **R1**（给摘要），除非用户点名某个已装 App（「用汽水播」→ R2）。  
3. **规则不确定** → 用 Role=`flash` 输出严格 JSON `{route,reason}`，枚举只许 `R0–R4`。失败或非法枚举 → 保持现网全工具面（不比今天窄，也不更宽）。  
4. 路由只收**广告给模型的工具表**。CC 关闭时 R2 里本来就没有 `computer.act`/GUI，行为与现网一致。  
5. `plan.run` 的每一步继承父轮 Route，禁止计划步骤把 R1 目标改写成 `computer.act`。

`complexity.decide` 继续只管「要不要提示 plan.run」，不管五路。禁止把两套路由揉成一个模型提示。

### 3.4 L0 传感器（先观察再裁判）

每个**会改变世界**的工具，在返回给模型之前附加 `l0` 对象。模型可以说完成，但运行时 `ok` 以 L0 为准。

| 工具 | L0 过线（采用） | 不算成功 |
|---|---|---|
| `desktop.open` | 已有：前置后标题或进程命中启动查询；排除自家窗 | 列表里有进程、Start() 未报错 |
| `media.play` target=foreground | 播放器窗在前台（复用启动查询 / CanonicalMusicApp）；若能读到 Now Playing 则对上 query | 只发送了媒体键 |
| `desktop.type` | UIA 再读该框，值包含本次提交文本（或提交后按钮状态变化） | 键发到月伴、或找不到框仍 `ok` |
| `browser.act` 突变（click/type/navigate） | 返回新鲜 snapshot；navigate 则 final URL 非空且不是约到登录墙却报成功 | `BROWSER_MCP_NOT_READY`、stale ref 当成功 |
| `computer.act` 像素动作 | 动作后前台仍是目标窗（非 Lunitide）；若带 `verifyAfter` / 具名控件则控件状态变化 | `action=list`、截图本身 |
| GUI 兜底点击 | 与上一行相同，**必须**再跑一轮 L0 | 模型句子「我点了」 |
| Office `*.gen` | 目标路径存在且大小 > 0（现网已接近） | 只口头交差 |
| `kb.cite` | 引用 id 落在本轮 `kb.search` 命中 | 模型编定位器 |

L0 通过 → 不调用 judge。  
L0 不确定（例如媒体键已发、Now Playing 读不到）→ Role=`judge` 只吃 **L0 观察 JSON + 目标**，禁止只吃「执行日志」。  
L0 明确失败 → 工具 `ok:false`，文案带原因；月伴每轮最多一句「无法执行」。

### 3.5 人接管（#G）

在现有 `user.ask` 上增加 `reason`（对模型可见、对 UI 可映射），枚举：

`login` | `2fa` | `captcha` | `pay` | `uac` | `file_picker` | `decision`

规则：

- `browser.act` 嗅到登录墙 / 支付页 / captcha → **停**，发 `user.ask`（reason 对应），禁止改 `computer.act` 或 GUI。  
- UAC、系统 Open/Save：继续现网停成 ask（已有测试）。  
- 接管期间舞台/会话进入 `awaiting_human`；用户做完点「继续」才恢复工具环。  
- 发送失败保向导 + 草稿（#6）继续有效。

### 3.6 检索（#E）与 Key 池（#F）

**检索：** FTS5 必留（机务 ATA/定位器）。配了 `embedding` 才：写入时给专家库 chunk、已确认记忆打向量；查询时 FTS ∥ 向量，`α·fts + β·dense + γ·recency`，再 MMR（λ≈0.7）。未配 embedding：行为不低于今天。

**Key 池：** 每个 Provider 从单 `CredentialRef` 改为有序 `CredentialRefs`。遇 `429` / `insufficient_quota` / 明确配额类错误换下一把；同一次用户动作最多轮 N=3。`401` 且不是轮转后的配额误报 → 停，标「需要重新输入凭据」。lease / Windows 凭据 / 不自动重发 的现网语义不变。UI：供应商编辑页「添加备用 Key」，不是新产品。

---

## 4. 日常场景对照（用现码工具写死）

| 用户说 | Route | 允许的第一跳 | 失败才 | 对用户成功句 |
|---|---|---|---|---|
| 北京明天天气 | R1 | `web.search`（已有示例 query） | `web.fetch` 打开结果页正文 | 引用摘要；不报假温度 |
| 打开汽水 | R2 | `desktop.open` + 前台 L0（已有 sodamusic 别名） | 不进 GUI | 「已经打开」且前台是播放器 |
| 打开汽水播周杰伦 | R2 | `media.play` foreground；必要时先 `desktop.open` | UIA 搜框失败 → SoM → GUI | 前台是汽水且在播（或 L0 不确定则诚实） |
| 打开记事本填身份证 | R2 | `desktop.open` 后继续环 + `desktop.type` after= | 无具名框 → `computer.act` / GUI | 框里读回号码 |
| 写一份半月财报到桌面 | R4 | `excel.gen`/`docx.gen` `desktop=true` | 禁止开 Excel 点「新建」 | 桌面文件存在 |
| 打开 12306 查余票（需登录） | R3 | `browser.act` navigate + snapshot | 登录墙 → `user.ask` reason=login | 不替登；登完用户点继续再 snapshot |
| 这个按钮 UIA 没有名字 | R2/R3 | `cc.observe_ui` / browser snapshot | SoM + **视觉读号**（无 GUI）或 GUI 模型点编号 | L0 再读控件/DOM |
| 画一张月亮 | R4 | `image.generate` | 不用 GUI 去开画图 | 工作区图片 |

---

## 5. 分问题锁死规格

### #A 任务路由（现码：模型看见全套工具）

**现象：** 查询类任务被做成开浏览器/开 GUI；生成类任务被做成操作 Office 菜单。  
**根因：** `autoToolProfile` 只区分寒暄，不区分五路。  
**锁死：** §3.3。新文件承担纯函数：`classifyTaskRoute(goal, companion, ccEnabled) (route, allow)`。单测用表驱动，不依赖网络。  
**验收 D-A1–A6**（§7）。

### #B L0 推广（现码：几乎只有 `desktop.open`）

**现象：** 播放、点击、填表工具自报成功。  
**根因：** 传感器没做成工具返回契约。  
**锁死：** §3.4。复用 `winexec.ForegroundWindow` / `ActivateWindowMatching` / `cc.observe_ui` / browser 返回的 snapshot，禁止新开 EnumWindows。截图若前台是自家窗，继续现网：改截桌面或上一非月伴窗。  
**验收 D-B1–B5。**

### #C Judge 角色（现码：`planVerifyPrompt` + 同一 model）

**现象：** 日志写「已打开」验证就变 true。  
**根因：** 验证器与执行器共故障域，且不看世界。  
**锁死：** `plan.run` 验证请求：`l0[]` 必须进 user 消息；模型 id = Role `judge`（默认 flash）。`verified=true` 当且仅当各突变步 L0 过或 judge 在**观察非空**时确认。无观察不得改口成功。  
**验收 D-C1–C3。**

### #D 独立 GUI 类型（现码：没有；旧设计绑 vision）

**现象：** 要视觉点选只能滥用视觉页或 chat 多模态；产品里也还没有 GUI 权重。  
**根因：** kind 表没有 `gui`；`computer.act` 把截图直接喂会话模型。  
**锁死：** §3.2 全节。无 GUI 走 SoM 读号；有 GUI 走专用目录。`ValidKind`/`NormalizeKind`/`MODEL_KINDS`/`persistKind` 同步加 `gui`。未知 kind **不得**再默默变 llm。会话选模型列表仍只暴露 `llm`。  
**验收 D-D1–D8。**

### #E Embedding + 混合检索（现码：空列）

**锁死：** 同 §3.6。机务 `kb.search` 关键词问不得被向量挤掉。  
**验收 D-E1–E3。**

### #F Key 池

**锁死：** 同 §3.6。只对配额类轮转。  
**验收 D-F1–F2。**

### #G 人接管

**锁死：** 同 §3.5。  
**验收 D-G1–G3。**

### #H 月伴与路由共存（现码：flash 启发式 + 桌面环）

**锁死：**

- 月伴仍用 Role `flash` 出字；路由判定可用同一 flash，**禁止**为分类再打一轮慢 `chat`。  
- 纯打开成功 settle、失败一句原因：保持 #9。  
- 路由不得让月伴重新获得 `command.run` / 裸 `cc.*` / `im.send`。  
- GUI 兜底若发生在月伴：仍算 desktop 环，受 `shouldContinueDesktopTurnGoal` 与 nudge 上限约束。  

**验收 D-H1–H3。**

### #I 本轮必须先观察（现码：角标画了，模型仍可盲点）

**现象：** `computer.act` 允许 `screenshot` 后直接 `click x,y`。`observe` 已返回 `nodes[].id` 和带角标的图，但聊天模型常忽略 JSON 去猜像素。  
**根因：** 运行时没有「本会话本轮未 observe 则拒绝裸坐标」。  
**锁死：**

1. 同一 `chat.start` 轮次内，裸 `x,y` / `drag` 必须带回本轮 `observe` 或 `screenshot` 的 `frameId`，且该帧之后至少成功执行过一次 `action=observe`（或 `desktop.type` 已命中具名框）。否则 `ok:false`，提示先 `observe`。  
2. `observe` 返回的带角标 PNG 才允许交给 §3.2.1 视觉读号；普通 `screenshot` 不得当 SoM。  
3. 不重写 `AnnotateCapture`。  
4. 纯打开（`companionGoalIsOpenOnly`）成功后本轮禁止再 `computer.act`（现网保持）。

**验收 D-I1–I3。**

### #J 无名 / 重名只许点本帧 ID

**现象：** UIA 无名节点叫 `button (unnamed)`；`InvokeUI` 按 Name 匹配，多个无名会点到第一个。Acc 回退路径直接丢掉无名节点。  
**根因：** ID 只在 `runhost` 观察后写入记忆；Invoke 层不用 AutomationId / 记忆 ID。  
**锁死：**

1. Acc 路径与 UIA 一样：无名也进树，Name 用 `role (unnamed)`，观察后必有 `B1`…。  
2. `click id=B1` 走 `rememberHits`；`InvokeUI` 优先 AutomationId 或记忆坐标，禁止用 `button (unnamed)` 当唯一键。  
3. 仅 `name=` 且命中多于一个 → `ok:false`，列出候选 id，要求调用方改 `id=`。  
4. 未先 observe 就 `id=B1` → 与 #I 相同失败。

**验收 D-J1–J3。**

### #K 树被截断要说出来

**现象：** 默认 80、硬顶 120；`FindAll` 先切 400。微信 / Excel 功能区大量控件看不见。  
**根因：** 返回里没有 `truncated`，模型以为看完了。  
**锁死：**

1. `observe` JSON 增加 `truncated:true|false`、`maxNodes`、`returned`。截断时文案提示 `scroll` 后再 `observe`。  
2. 不在 P0 把上限改到几千（提示词爆炸）。  
3. P1：对带 `ScrollPattern` 的容器，`scroll` 后同一 `frame` 允许再 observe；不新工具名。

**验收 D-K1–K2。**

### #L `verifyAfter` 画面不变不得当成功

**现象：** `verifyAfter` 已再截图，若哈希相同仍返回成功，只附「screen unchanged」。  
**根因：** 传感器当了旁注，不是 `ok`。  
**锁死：**

1. 突变类（具名 click / 像素 click / type / paste / menu / set_value）若后帧哈希与前帧相同，且调用方没有刚执行 `wait until=change`：**工具 `ok:false`**，摘要保留观察。  
2. `wait until=change` 仍按现网等变化；超时失败。  
3. 纯 `screenshot` / `observe` / `list` / `active` 不算突变。  
4. 与 #B 合并验收：像素动作前台是 Lunitide 仍失败。

**验收 D-L1–L2。**

### #M 提权 / 自绘 / 他窗（诚实边界）

**现象：** 提权后的窗、DirectX/游戏、大量自绘（部分微信区）UIA 为空。  
**锁死：**

1. UIPI / 拒绝附加 → `user.ask` reason=`uac`（或新 `elevated`，可并进 `uac`），禁止 GUI 硬点。  
2. `observe` 节点为 0 且不是敏感窗：才进入 §3.2.1 视觉读号或 GUI；仍必须 L0。  
3. 不承诺网银、杀毒、游戏内操作。

**验收 D-M1。**

### #N 门与入口（不是再造网关）

**锁死：**

1. CC 关闭：R2 可打开/播放/UIA 填表；`computer.act` / GUI / SoM 读号全部不可见。失败一句带「设置里打开电脑控制」，不自开。  
2. 入站飞书/企微/钉钉保持白名单；个人微信/QQ 入站、手机节点、Home Assistant：**本整改不做**。  
3. cron / 入站依赖引擎进程活着。不新 daemon。关掉应用就停，设置里可用一句话说明。

**验收 D-N1。**

### #O 滚、拖、中文输入

**锁死：**

1. 中文 / 超过 16 字的桌面输入：运行时改走 `paste`（已有工具），禁止逐键 `keyboard_type` 拆 IME。`desktop.type` 同样。  
2. `drag` 允许 `id`（起点）+ 终点坐标或第二个 `id`；P1。P0 可只测 CJK paste。  
3. `scroll` 保持现网 `MouseScroll`，不改成 click。

**验收 D-O1。**

---

## 6. 分期

### P0 — 不配新模型也变准（可先合，不升版本）

1. `TaskRoute` 纯函数 + 接入 `chat.start` 工具表（#A）。  
2. L0 接到 `media.play` / `desktop.type` / `browser.act` 突变 / `computer.act` 像素（#B）。  
3. `plan.verify` 带 L0；judge 角色默认 flash（#C）。  
4. `user.ask` 增加 reason；登录/支付/UAC 禁止转 GUI（#G 的门，GUI 页可以还没有）。  
5. FTS + 新近度 + MMR（此时可以没有向量）。  
6. 供应商备用 Key + 429 轮转（#F）。  
7. 电脑控制闭环：本轮先 observe（#I）；无名只点 ID（#J）；`observe.truncated`（#K）；`verifyAfter` 不变则失败（#L）；CJK 走 paste（#O）；CC 关则无 SoM（#N）。

### P1 — 两个新 kind 与设置面

1. `kind=gui` 页签 + SoM + **无 GUI 时视觉读号** + 有 GUI 时专用目录（#D）。设置里写清 UI-TARS-1.5-7B / 2B GGUF 的接入草稿，不下载权重。  
2. `kind=embedding` + 写入/混合检索（#E）。  
3. 设置「能力路由」六个下拉：chat / flash / vision / embed / judge / gui，空 = 自动。  
4. 能力路由与供应商页空态文案按 §3.1 / §3.2。  
5. `drag` 支持 id；`ScrollPattern` 后再 observe（#K #O）。  
6. `observe` 节点为 0 才视觉读号（#M + #D）。

### P2 — 痛点才加，默认关

1. 信息查询适配器（天气等）——仍是 R1，不是 GUI。  
2. 库 > 约 2 万 chunk 才可选 rerank。  
3. 扫描件 ingest 本机 OCR（不是设置 kind）。  
4. 可选文本 Guard 挂 hooks，航空默认关。

---

## 7. 验收 ID

先写失败单测，再改实现。编号前缀 `D-`（Daily ops），避免和 C7–C9 撞车。

| ID | 断言 |
|---|---|
| D-A1 | `classifyTaskRoute("北京明天天气", …)==R1`，allow 不含 `desktop.open`/`browser.act`/`computer.act` |
| D-A2 | `classifyTaskRoute("打开汽水", …)==R2`，allow 含 `desktop.open`，不含 GUI 入口标志 |
| D-A3 | `classifyTaskRoute("打开浏览器查天气", …)==R1`（冲突收口） |
| D-A4 | `classifyTaskRoute("写一份半月财报到桌面", …)==R4`，allow 含 `excel.gen`/`docx.gen`，不含 `computer.act` |
| D-A5 | `classifyTaskRoute("你好", …)==R0`，与现 `minimal` 兼容 |
| D-A6 | CC 关闭时 R2 allow 仍不含 `computer.act`；分类不得把 CC 打开 |
| D-B1 | `media.play` 前台未命中播放器 → `ok:false`（即使媒体键已发） |
| D-B2 | `desktop.type` 后再读框值不包含提交文本 → `ok:false` |
| D-B3 | `browser.act` click 在 `BROWSER_MCP_NOT_READY` 或 stale ref → 不得 `ok:true` |
| D-B4 | `computer.act` `action=list` 不得把桌面目标标 verified |
| D-B5 | 假前台 Lunitide：像素动作 L0 失败（对齐 C9-8 精神） |
| D-C1 | `plan.run` 验证请求的 model id ≠ chat model id（未勾「验证用主模型」） |
| D-C2 | 验证 user 消息含 `l0`；无观察时 `verified` 不得为 true |
| D-C3 | 各步 L0 全过：可不调用 judge，直接 `verified=true` |
| D-D1 | `ValidKind("gui")==true`；`NormalizeKind("gui")==gui`；不会变成 llm |
| D-D2 | `CatalogForKind(gui)` 不含 vision/llm 行；`VisionDescribeCatalog` 不含 gui 行 |
| D-D3 | 前端 `MODEL_KINDS` 含 `gui`，页签文案「GUI 模型」 |
| D-D4 | 会话/首页模型选择器仍只有 llm |
| D-D5 | R1 路径调用 GUI catalog → 运行时拒绝（单测打桩） |
| D-D6 | UIA 已能 `desktop.type` 的目标：不进入 GUI（顺序断言） |
| D-D7 | GUI 空 + 视觉目录有 + UIA 无名按钮：只调用 vision 的 `guiSomPickPrompt`，输出 markId 后点编号表里的 id；**不**把像素坐标当动作 |
| D-D8 | GUI 空 + 视觉也空 + UIA 失败 → `无法执行` 且含 L0 观察；不调用 `chat` 冒充点击 |
| D-E1 | 未配 embedding：`memory.search`/`kb.search` 行为不低于现网基线测试 |
| D-E2 | 配了 embedding：同义问能进向量支路；关键词问 FTS 分仍在融合里 |
| D-E3 | 向量写入失败不影响 FTS 可读 |
| D-F1 | 主 Key 429、备用 Key 可用：同一轮 `chat.start` 能继续 |
| D-F2 | 主 Key 401（非配额）：不轮转到备用，状态 `requires_reentry` |
| D-G1 | 登录墙 → `user.ask` reason=`login`，本轮无 `computer.act`/GUI |
| D-G2 | UAC 仍停成 ask（现网回归） |
| D-G3 | 支付页 reason=`pay`，禁止 GUI |
| D-H1 | 月伴 R2 纯打开：前台成功 settle，不进 GUI |
| D-H2 | 月伴工具表仍无 `command.run` / 裸 `cc.*` |
| D-H3 | 分类调用若需要模型，只用 flash 绑定，不另起 chat |
| D-I1 | 本轮无 observe 的 `click x,y` → `ok:false` |
| D-I2 | 同轮 observe 后 `click id=B1` 可用 `rememberHits` |
| D-I3 | 不重画角标：`AnnotateCapture` 仍是唯一画手 |
| D-J1 | 两个 `button (unnamed)`：`name=` 失败并列出 B1/B2；`id=B2` 成功 |
| D-J2 | Acc 无名节点观察后有 ID |
| D-J3 | `InvokeUI("button (unnamed)")` 不得当唯一成功路径 |
| D-K1 | 节点打满 maxNodes → JSON `truncated=true` |
| D-K2 | 未打满 → `truncated=false` |
| D-L1 | 具名 click 后哈希不变 → `ok:false`（除非刚 wait until=change 等到了） |
| D-L2 | `action=observe` 后哈希不变仍 `ok:true` |
| D-M1 | 附加提权窗失败 → ask/`uac`，无像素动作 |
| D-N1 | CC 关闭：工具表无 `computer.act`，SoM 读号不调用 |
| D-O1 | `desktop.type` / `computer.act type` 含汉字 → 走 paste 路径（单测打桩） |

手工 20 分钟（P1 齐套后）：**不配 GUI、只配视觉**时无名按钮经 observe 角标 + 读号能点且 L0 再读；不 observe 就xy 必须失败；点完画面不变必须失败；视觉也关掉则失败并带观察；CC 关闭不能 SoM；汉字填框走粘贴；再配 GUI 时走专用目录；查天气只搜索；打开汽水到前台；登录页弹出决策不盲点。不要求安装包内自带 GUI 权重。

---

## 8. 文件所有权（实施计划按此拆，本文不改代码）

```text
#A 路由
  新  internal/app/task_route.go (+ _test.go)
  改  internal/app/chat.go          （chat.start 收工具表）
  改  internal/app/chat_tool_profile.go
  改  internal/app/chat_plan.go      （步骤继承 Route）

#B L0
  改  internal/toolruntime/desktop_open_verify.go  （抽出可复用命中）
  改  internal/toolruntime/media.go / media_foreground.go
  改  internal/toolruntime/runtime.go              （desktop.type 回读）
  改  internal/brapp/* 或 browser 执行返回
  改  internal/ccapp / computer.act 映射
  改  internal/ccapp/runhost.go                    （#I 本轮 observe 门、#L 不变失败、#K truncated）
  改  internal/ccapp/host_windows_ui.go            （#J Acc 无名保留）
  改  internal/ccapp/host_windows_uia.go           （#J Invoke 认 ID/AutomationId）
  改  internal/ccapp/service.go                    （#L verifyAfter、#O CJK paste）
  改  internal/app/chat.go                         （工具结果附 l0）

#C Judge
  改  internal/app/chat_plan.go
  改  internal/app/model_catalog.go                （RoleBinding）

#D GUI kind
  改  internal/domain/provider/kind.go
  改  web/src/provider/modelKind.ts + test
  改  web/src/provider/ProviderApp.tsx             （页签 + 空态）
  改  bridge schema / generated 若 kind 枚举暴露
  新  internal/app/gui_fallback.go (+ _test.go)    （选 gui / SoM-vision / 停）
  改  internal/ccapp/host_windows_uia.go           （SoM 编号）
  改  internal/app/chat_tool_defs.go               （computer.act 描述：UIA 失败才视觉）
  改  web/src/provider/ProviderApp.tsx             （GUI 空态 + UI-TARS 接入草稿，不下载）

#E 检索
  改  internal/storage/sqlite/m8_kb.go / memory_storage.go
  改  internal/app/kb_search_handlers.go
  新  embedding 调用走现有 gateway，不新进程

#F Key 池
  改  internal/domain/provider/provider.go
  改  internal/secretlease / credentialsubmission
  改  ProviderApp 备用 Key UI

#G 接管
  改  internal/toolruntime/user_ask.go
  改  web/src/session/userAsk.ts
  改  browser.act 登录墙分支
  回归  SessionPage.companion.test UAC

设置「能力路由」
  新  web/src/settings/CapabilityRouting.tsx（或并入现设置页，不新窗口路由）
```

`SessionPage.tsx` / `CompanionStage.tsx` 本整改尽量不碰；#G 只加 reason 映射。舞台视觉未提交改动与本 PRD 无关，禁止夹带。

---

## 9. 非目标

- 不把月汐改成 OSWorld 纯视觉 CUA，不追求 OSWorld 分数。  
- 不新开 UI-TARS Desktop / vLLM / 向量库 / Mem0 进程；不把 GUI 权重打进 Setup（与 Omni 权重排除同契约）。  
- 不为 OS-Atlas / GUI-Actor 写专用 transformers 加载器。  
- 不新增 weather 商业 API 密钥产品（P2 适配器另批）。  
- 不恢复 Office COM 生成路径。  
- 不接线 `companionOpeningAck`，不改 poison。  
- 不移动 `v0.4.62` tag，不夹带 `.release-cache`。  
- 不把 Judge/OCR/Rerank 做成设置页签。  
- 不让子代理获得 GUI 或桌面控制。  
- 不做个人微信/QQ 入站协议、手机节点、Home Assistant、远程桌面。  
- 不把月汐改成 Codex 式「截屏即主干」。  
- 不重写已有 `AnnotateCapture` / `assignNodeIDs`。

---

## 10. 风险

| 风险 | 对策 |
|---|---|
| GUI 页一配，模型当主力手 | 路由 + `gui_fallback` 闸门；R1/R4 单测拒绝 |
| `NormalizeKind` 把 gui 吃成 llm | D-D1 先写；ValidKind 同步 |
| 视觉页与 GUI 页用户配同一模型 | 允许两行同 ID；目录按 kind 隔离 |
| flash 分类抖 | 规则优先；非法 JSON 回落现网全工具，不比今天窄 |
| Key 池轮 401 | 只认配额类错误码/正文 |
| SoM 编号与 UIA 节点错位 | 编号只来自本帧 `observe_ui` / snapshot；过期 frameId 失败闭 |
| 无 GUI 时 vision 开始猜坐标 | D-D7：schema 只收 markId；越界当失败 |
| 用户以为产品自带 UI-TARS | 空态写明要自备本机服务；安装包不含权重 |
| 按 TRAE 缺口去装 Computer-Use MCP | 月汐已有 `computer.act`；只闭环，不外挂平行手 |

---

## 11. 批准后怎么走

1. 用户批本文（可改 §6 分期。**无 GUI 时允许视觉读号，不允许把 GUI 页并回视觉页，也不允许 chat 点像素**）。  
2. 再写 `docs/superpowers/plans/2026-09-04-daily-ops-capability.md`（writing-plans，先测后改）。  
3. 按 P0 → P1 → P2 合入；P0 即可单独发布体验，不强制用户先配 GUI。  

未批准：不改 `kind.go`、不加页签、不接路由。
