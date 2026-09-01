# 月汐语音全链路整改合同（火山 / 本地 / 云端）

日期：2026-09-01  
产品：月汐 / Lunitide `VERSION` **0.4.44**（已装官方包；本机数据根 `%LocalAppData%\Lunitide\`）  
状态：**语音专项落地合同**。和 `2026-09-01-system-upgrade-and-remediation.md` 冲突时：**波次授权以本文 W-VOICE 为准**，冻结表仍以主合同 §8 为准。  
画板：`voice-pipeline-remediation.canvas.tsx`（和本文冲突时以本文为准）。

本文只回答语音。上一份全局合同把 W1-VOICE 写成「几乎不改代码 / 装包后勾」——**2026-09-01 桌面已证伪**。现在三条卡都不能对话，必须先改代码再勾真机。

**不能报：** 语音已齐、克隆启动中只是在加载、VOICE-004 只是密钥问题、选火山失败会自动改用本机、已经可以试听 50 种音色。

---

## 0. 和上一份整改报告的关系

| 上一份（主合同） | 桌面事实（2026-09-01） | 本文修订 |
|---|---|---|
| §5.2 缺口 = 装包后复验 VOICE-004、克隆启动中 | 火山：麦有能量、握手能过、`voice.append` 不出字，停在 VOICE-004 | 不是「再勾一次」。**协议解析 + 模型挑选是代码缺陷** |
| R8「克隆启动中：代码已收，装包后勾」 | 设置页按钮永久「启动中…」，试听不可点 | 代码收的是「启动中禁止试听」。**宿主把已崩溃的进程仍报 launching**，UI 永远不离场 |
| W1-VOICE 通过标准 = 插话仍关；VOICE-004 不跳 sherpa；启动中文案含「启动中」 | 这三条现码都满足，用户仍然完全不能对话 | **旧验收写错了**。不跳 sherpa 是对的；聋了必须修火山本身。启动中必须能在进程死后变成诚实失败 |
| 语音月伴现码 4.3 | 三条卡都不能完成一轮「听→想→说」 | 真机 **2.4**。单测绿不加成 |
| 未单列回合风暴 | 月伴不答、画面乱跳；退回打字后同一句连发；会话里五条相同「收到请求回答」 | **三条卡共享的 P0**。不修这个，修哪条 ASR/TTS 都会再炸 |

上一份里仍然有效、本文不得推翻的：

- 选火山失败 **禁止** 静默切 sherpa / 系统识别（`companionListenFailover('volc', …) === 'volc'`）
- 不重写 `useCompanionMachine` 整表
- 不在 `volc_speech` / openspeech 上挂对话 LLM
- 不改 `E:\GPT-SoVITS\start-api-cpu.bat`、端口 9880、盘符
- 不静默回落晓晓冒充本地克隆
- 不给 ASR / TTS 各开供应商顶栏

---

## 1. 产品裁定（语音）

三张卡不是三条完整栈。每张卡都是 **听（ASR）+ 说（TTS）+ 想（对话 LLM）**。想永远走已选供应商（现场是 `glm-4-air`），与 openspeech 无关。

| 卡 | 听 | 说 | 想 | 失败关闭 |
|---|---|---|---|---|
| **火山** | 火山 seed-asr（SAUC WebSocket） | 火山 seed-tts 2.0（密钥未齐则故意用 Edge 晓晓，灯要诚实） | 同一 `chat.start` | 听失败停在火山，文案 VOICE-004，**不**切 sherpa / Web Speech |
| **本地** | 本机 sherpa | GPT-SoVITS `http://127.0.0.1:9880` | 同一 `chat.start` | 听失败停在本机。说失败必须露出宿主/日志原因，**不**假装还在加载 |
| **云端** | 系统 Web Speech（WebView2） | 微软 Edge Neural（晓晓等） | 同一 `chat.start` | 听失败可改本机（仅云端卡）。说失败露出网络/引擎，可改自然语音，**不**假装本地克隆 |

插话（barge-in）只允许本地 / 火山。云端 Web Speech 不能插话——这是能力，不是 bug。

---

## 2. 现码地图（落地时对照，不要重画）

```text
麦 PCM 16 kHz
    │
    ├─ 火山卡 ── voice.start backend=volc ── volcsauc WS
    │              wss://openspeech.bytedance.com/api/v3/plan/sauc/bigmodel_async
    │              → voice.append {text,final} → CompanionStage.onFinal
    │
    ├─ 本地卡 ── voice.start (sherpa) ── 本机 sidecar
    │              → voice.append → onFinal
    │
    └─ 云端卡 ── Web Speech (startCompanionSpeech)
                   → onresult → onFinal

onFinal / FORCE_COMMIT / seedPrompt
    → beginUserTurn
    → SessionPage.sendAndChat (companion:true)
    → message.append + chat.start (glm-4-*)
    → TtsPlayer.speak
         ├─ volc  → openspeech /api/v3/plan/tts/unidirectional
         ├─ ref   → GPT-SoVITS :9880  (RefHost 拉起 start-api-cpu.bat)
         └─ edge  → 微软云端 Neural
```

关键文件：

| 层 | 路径 |
|---|---|
| 选卡 | `web/src/session/companion/companionSettings.ts` `applyVoicePath`；`web/src/settings/VoicePathPicker.tsx` |
| 听路由 | `web/src/session/companion/asrPath.ts`；`CompanionStage.tsx` `startListening` |
| 火山听 | `web/src/session/companion/volc/volcSpeech.ts`；`internal/voice/volcsauc/{backend,protocol,config}.go`；`internal/app/voice_handlers.go` `startVolcVoice` |
| 本地听 | `web/src/session/companion/localSpeech.ts`；`internal/voice/sherpa*.go` |
| 云端听 | `web/src/session/companion/speech.ts` `startCompanionSpeech` |
| 说 | `web/src/session/companion/ttsPlayer.ts`；`internal/tts/{volc,refhost,refcatalog}.go`；`internal/app/m9_tts_handlers.go` |
| 想 / 回合 | `web/src/session/SessionPage.tsx` `sendAndChat`；`CompanionStage.tsx` `beginUserTurn` |
| 设置 / 试听 | `web/src/settings/SettingsPage.tsx`；`web/src/settings/refEnginePreview.ts` |
| 灯 | `web/src/session/companion/companionLights.ts` |

---

## 3. 现场证据（2026-09-01 本机 0.4.44）

### 3.1 火山卡：听得到，不出字

- 灯：听 火山 seed-asr / 说 火山 小向 / 思 glm-4-*。
- 字幕：正在听；「听到你的声音了，火山听写还没有出字」；VOICE-004。
- `onVoiceEnergy` 有峰值 → 麦链路通。
- `voice.start` 能成功（否则会走「连不上」而不是「转不出字」）。
- `voice.append` 回空 `text`。聋恢复 2 次后停在火山（`VOLC_DEAF_RESTART_LIMIT = 2`），**行为符合合同，但用户被关在死胡同**。

### 3.2 本地卡：永久「启动中」，不能试听

- 设置 → 语音与麦克风；地址 `http://127.0.0.1:9880`；按钮「启动中…」。
- 文案仍写「首次加载 30–90 秒」。页脚「设置已保存」。
- 宿主日志 `%LocalAppData%\Lunitide\logs\ref-engine-20260901-094638.log`：

```text
FileNotFoundError: ...\env\lib\site-packages\jieba_fast\dict.txt
```

`api_v2.py` 在 import `jieba_fast` 时立刻退出。`:9880` 永远不会起来。`/docs` 永远不是 200。

### 3.3 云端卡：不是这次点的卡，但同一回合核已炸

- 云端听 = WebView2 Web Speech；说 = Edge Neural。
- 用户退回打字后，会话里已有多条相同助手气泡「收到请求回答」。
- 「收到请求回答」**不是**产品文案（全库无此字符串）。它是 `glm-4-air` 对同一用户句的短应答。
- 云端卡若此时打开，听写即便成功，也会撞上同一 `sendAndChat` 风暴。

### 3.4 回合风暴（跨三卡）

- 月伴舞台不答、字幕/气泡乱跳。
- 退出月伴改打字：同一句用户话被连发。
- 会话右侧五条相同助手气泡，每条都有复制 / 分享 / 重试 / 删除。

---

## 4. 根因（按层，不要先改架构）

### 4.1 火山听写聋（P0）

现码握手能过、帧在走、字解析不出来，或解析到了前端不要。

| # | 缺陷 | 证据 | 为何致聋 |
|---|---|---|---|
| V1 | `startVolcVoice` / `handleProviderTest` 取 **第一个 `IsDefault`**，不看 `KindASR` | `voice_handlers.go` 408–417；`provider_diagnostics.go` 56–63 | 火山供应商里 TTS 音色（`zh_female_*` / `seed-tts-2.0`）常被标默认。`ResourceIDFromModel` 对非 `volc.seedasr.*` 会回落到 `volc.seedasr.sauc.duration`，握手仍可能 200，但模型/资源组合不是前端以为的 seed-asr |
| V2 | 前端 `pickDefaultVoice` 已按 ASR 挑；后端 `voice.start` **不用** 这个 modelId，只用 providerId | `modelKind.ts` 160–181 vs `startVolcVoice` | 设置页测通、月伴仍聋，两套挑选 |
| V3 | `result_type: "single"` | `backend.go` `fullClientRequest` | 官方默认偏 `full`（累计当前句）。`single` 只给最新一段，配合错误 context 时常见空 `result` |
| V4 | `corpus.context` 是热词换行明文（`月汐\n月伴…`） | `backend.go` 173–175；`DefaultHotwords` | 官方要的是 JSON 字符串 `{"context_type":"dialog_ctx","context_data":[{"text":"…"}]}`。非法 context 可以让套接字活着、一直不出字 |
| V5 | `TranscriptFromJSON` 只把 `result` 当 **对象** | `protocol.go` 159–193 | 官方表里 `result` 可以是 **列表**。数组来了 → unmarshal 失败 → `ok=false` → `Latest()` 空。也没有 `payload_msg` 外壳 |
| V6 | `readLoop` 遇到 `DecodeFrame` 错误 **直接 continue** | `backend.go` 307–310 | 握手后若后续是文本 JSON / 非标准二进制，全部扔掉，会话表现为永久聋 |
| V7 | 只设 `X-Api-Connect-Id`，不设 `X-Api-Request-Id` | `backend.go` 108 | 官方建议两套都带。不是唯一根因，但是握手/对账缺口 |
| V8 | 前端 `acceptTranscript` 只要 `result.text`；能量 ≠ 字 | `volcAsr.ts`（既有） | 后端空字时，灯会正确说「听得到但转不出字」——这是诚实，不是修复 |

**不要做的「修复」：** 把火山聋改成 sherpa / Web Speech。那是换产品，不是修火山。

### 4.2 本地克隆假启动中（P0）

| # | 缺陷 | 证据 | 为何永远启动中 |
|---|---|---|---|
| L1 | 启动脚本拉起的 Python **秒退**（缺 `jieba_fast/dict.txt`） | `ref-engine-20260901-094638.log` | `/docs` 永不 200。这是本机 GPT-SoVITS 环境坏了，**不是月汐冷启动慢** |
| L2 | `RefHost.Status` 在 `state==launching` 时，即使进程已死，仍报 `launching` | `refhost.go` 150–153 | 没有 `Wait()` / 退出回调。spawn 成功就把 `lastErr` 清空 |
| L3 | `host_last_err` **只在 offline / not_configured 时下发** | `refcatalog.go` 218–219 | launching 期间设置页永远看不到日志里的 `FileNotFoundError` |
| L4 | 设置页轮询 40×3s 后 **静默停**，不调用已写好的 `refLaunchPollStatus` | `SettingsPage.tsx` 556 vs `refEnginePreview.ts` 42–47 | 超时文案和 `host_last_err` 在单测里齐，生产设置页没用上 |
| L5 | `EnsureRunning` 把 launching 当「还在加载」最多 3 分钟，然后可能再 spawn | `refhost.go` 219–223 | 同一条坏脚本循环拉起，UI 一直「启动中」 |
| L6 | 试听按钮在 `host_state==launching` 时禁用 | `refPreviewReady` / `refPreviewButtonLabel` | 对真加载是对的；对已崩溃是陷阱 |
| L7 | `engineState` 只看 `voices.length>0`，**不看** `server_online` | `SettingsPage.tsx` 551 | 音色包目录常驻 50 条，服务没起来也会 `available`。现场按钮能禁用是因为 L6 看了 `host_state`；一旦 launching 误翻或其它面读 `engineState`，会假可用 |

**不要做的「修复」：** 改 `E:\GPT-SoVITS\start-api-cpu.bat`、改端口、改盘符、失败后静默改用晓晓还显示「本地克隆」。

月汐该做的：监视子进程退出、把日志最后一行（去路径敏感后）写入 `lastErr`、把状态打成 `offline`、设置页用 `refLaunchPollStatus`、试听解禁为「可点但诚实失败」。用户修 `jieba_fast` 字典是环境活，不是月汐改 bat。

### 4.3 云端卡（P1，但同一核）

| # | 事实 | 风险 |
|---|---|---|
| C1 | 听 = Web Speech。WebView2 上经常弱、无 `onresult`、或只给残句 | 单独选云端时，用户会觉得「没听清」 |
| C2 | 说 = Edge Neural，要联网。403 / 时间 / 网络已有文案 | 云端说失败不应被报成 M95-002 本地合成 |
| C3 | `companionListenFailover`：云端失败且 sherpa 就绪 → 改本机；火山失败 **不** 改云端 | 正确。云端不是火山的暗备份 |
| C4 | `omni` / `flm` 遗留存档会被 `applyVoicePath` 折到云端 | 不要再接 MiniCPM-o 全双工 |
| C5 | 云端不能插话 | 保持。不要为了对齐火山去假开插话 |
| C6 | FORCE_COMMIT / `sendAndChat` 风暴与卡无关 | **不修 4.4，云端也会乱跳连发** |
| C7 | 云端听写 **不能** 在 TTS 时关掉系统识别（Web Speech 无 mute） | `speech.ts` 只能靠回声门闩 | 外放时可能把她的话再听成用户句；保持门闩，不做假全双工 |

云端本波不是「做成最好的听写」，是：回合核修好之后，云端至少能 **听一句 → 回一句 → 读一句**，失败文案诚实。

### 4.4 回合风暴（P0，挡所有卡）

这是「完全不能对话」的直接原因，比单条 ASR 更致命。

| # | 缺陷 | 证据 | 用户看见什么 |
|---|---|---|---|
| T1 | `FORCE_COMMIT`：1.4s 后每 400ms `kick()`；`forceCommit===false` 时仍 `beginUserTurn(caption)` | `CompanionStage.tsx` 1679–1700；`FORCE_COMMIT_MS=1400` | 同一句 interim 被当成多轮用户话 |
| T2 | `sendAndChat` 在月伴且已有进行中回合时：`resetCompanionTurn()`（取消上一流）然后 **继续往下开新 `chat.start`**，没有 `return` | `SessionPage.tsx` 212 同行 | 舞台取消/重开，气泡乱跳；同一句反复 `message.append` |
| T3 | `onSend` 多数成功路径 `return true` / `undefined`，只有 busy 才 `false`。T2 让 busy 被 reset 掉 | 同上 | `beginUserTurn` 以为发送成功，不会停 |
| T4 | `pendingSendRef` 在 `chatReady` 变化时最多再冲 6 次 | `CompanionStage.tsx` 1200–1228 | 退出月伴后同一句继续进 `sendAndChat` |
| T5 | `seedPrompt` 进舞台就 `beginUserTurn` 一次 | `CompanionStage.tsx` 1192–1198 | 和 FORCE_COMMIT / 听写 final 叠在一起会双发 |
| T6 | 「收到请求回答」是模型短应答，不是 UI 占位 | 全库无此字符串 | 五条相同气泡 = 五次成功的 `chat.start` |
| T7 | `!chatReady` 时 `beginUserTurn` 只写 `pendingSendRef`，**不**发 `RECOGNIZED_FINAL` | `CompanionStage.tsx` 1171–1174 | 状态留在 `listening`，T1 的 400ms 定时器继续踢同一句 |

合同：同一句用户话在一个听写回合里只允许 **一次** `chat.start`。月伴进行中再来同一句：排队或丢掉，**禁止** 取消上一流再开一流。离开月伴必须拆掉 FORCE_COMMIT、pendingSend、听写 handle。

---

## 5. 整改方案（按优先级，一次做完）

原则：先堵回合核，再修火山字，再让本地宿主说真话，最后给云端一条能走完的路。每步都可单独验收。不重写状态机，不换核。

### 5.1 W-VOICE-1 — 回合只提交一次（P0，三卡共享）

**改：**

1. `CompanionStage` FORCE_COMMIT：`forceCommit` 失败则 **不再** `beginUserTurn`。只允许 handle 自己 `onFinal` 一次。定时器在 `thinking/speaking` 或已经 `sentThisUtterance` 后必须停。
2. `beginUserTurn`：对规范化后的 `text` 做「本回合已发送」门闩（ref）。同一句在会话未回到 idle 听之前不得再 `onSend`。
3. `sendAndChat`：`companionOpen && inFlight` 时 **只** `queue.enqueue` 或返回 `false`。**删除**「reset 再开新流」这条落空路径。
4. 离开月伴（`onExit` / unmount）：停听写、清 `pendingSendRef`、拆 FORCE_COMMIT、取消未完成的 seed 重试。
5. `seedPrompt` 与听写 final 去重。
6. `!chatReady` 入队后必须停 FORCE_COMMIT（或离开 `listening`），禁止边等供应商边每 400ms 补发。

**测：**

- 现有 `CompanionStage.headstart/a11y/task`：同一句 `onSend` 仍为 1。
- **新测：** FORCE_COMMIT 连续 `kick` 不得第二次 `onSend`。
- **新测：** `sendAndChat` 月伴 in-flight 返回 `false` / 入队，不 `resetCompanionTurn`。
- 桌面：说一句，只出现一轮用户气泡；退回打字不再连发。

**不要：** 重写 `useCompanionMachine`；不要靠加长 FORCE_COMMIT 掩盖。

### 5.2 W-VOICE-2 — 火山听写能出字（P0）

**改：**

1. `volcListenModelID(provider)`：只从 `EffectiveKind()==KindASR` 里挑（KindDefault → IsDefault → 第一个 ASR）。`startVolcVoice` 与 `handleProviderTest`（未指定 model 时）共用。
2. `TranscriptFromJSON`：`result` 对象 **或** 数组；可选 `payload_msg` 外壳。
3. `applyFrame`：`frame.JSON` 为空时尝试 `frame.Raw`；`readLoop` 对非二进制文本帧走 JSON，不再 `continue` 吞掉。
4. `result_type: "full"`。
5. `corpus.context` 改为官方 `dialog_ctx` JSON；热词放进 `context_data[].text`。非法则 **省略 corpus**，不要发明文换行。
6. 握手头加 `X-Api-Request-Id`（新 UUID，可与 Connect-Id 不同）。

**测：**

- `protocol_test.go`：数组 `result`、`payload_msg`、空 result。
- `voice_handlers` / provider.test：默认模型是 ASR 不是 TTS 音色。
- 桌面：火山卡说一句中文，字幕出字，且 **不** 出现 sherpa /「系统识别」。

**不要：** 改 Resource-Id 默认值；不要把 LLM Base URL 当语音源站；不要为了出字去切本机。

### 5.3 W-VOICE-3 — 本地克隆诚实失败（P0）

**改：**

1. `RefHost`：`cmd.Wait()` 在后台。进程退出且 `/docs` 未通 → `offline`，`lastErr` = 日志尾（截断、去掉用户主目录以外的绝对路径可留 GPT-SoVITS 路径，因产品已写死 `E:\GPT-SoVITS`）。
2. `launching` 超过等待预算（120s ensure / 3min adopt）必须离场到 `offline`，禁止无限 launching。
3. `RefPackMeta`：`launching` 也下发 `host_last_err`（若有）。
4. `SettingsPage` 轮询结束必须走 `refLaunchPollStatus`；`engineState` 对 `ref` 必须看 `server_online`（或 `host_state==online`），禁止只凭音色条数报 `available`。按钮在 `offline` 时恢复「试听」或「重新启动」，点了给诚实错误，不回到假启动中。
5. 月伴灯：`GPT-SoVITS 启动中` 只在 `startedAt` 后的宽限内；超时改「本地朗读未就绪」+ 原因。

**测：**

- `refhost_test.go`：假进程立刻退出 → 状态 offline + lastErr 非空。
- `refEnginePreview` + CompanionSection：超时文案出现。
- 桌面：当前缺 `dict.txt` 时，不再永久「启动中」；试听可点并看到失败原因。用户修好字典后，**不改 bat** 也能再探活。

**不要：** 改 bat / 9880 / 盘符；不要自动改用晓晓还标「本地」。

### 5.4 W-VOICE-4 — 云端走完一轮（P1，依赖 W-VOICE-1）

**改：**

1. Web Speech 同一句只 `onFinal` 一次（跟 W-VOICE-1 门闩）。
2. Edge 合成失败保持现有「需联网 / 改自然语音」文案；禁止落到「该段语音合成失败」而不说是云端。
3. 云端卡设置页：写清「听=系统识别，说=微软晓晓，不能插话」。
4. 不把云端当成火山/本地的静默备份。

**测：**

- 现有 `prepareCompanionEntry` / `asrPath` 单测保持：火山失败仍是火山。
- 桌面：选云端，说一句，出字、有一轮回答、晓晓能出声（需联网）。Web Speech 不可用时诚实报，不跳火山。

### 5.5 W-VOICE-5 — 桌面验收（无新功能）

装 **含 W-VOICE-1..4 的包**（不是现在的 0.4.44）后勾。单测绿 ≠ 勾。

勾进 `2026-08-31-p0-live-acceptance.md` 增表「语音三卡」。旧 W1-VOICE 三行作废。

---

## 6. 详细整改计划（施工顺序）

一波不过不开始下一波。W-VOICE-1 不过，禁止宣称任何一条卡已修好。

```text
W-VOICE-1  回合只提交一次
     │
     ├─► W-VOICE-2  火山出字
     │
     └─► W-VOICE-3  本地宿主诚实     } 1 过之后可并行
              │
              ▼
         W-VOICE-4  云端走完一轮
              │
              ▼
         W-VOICE-5  打包装包，三卡真机勾
```

W2 / W3 禁止并行到「同一天改 `useCompanionMachine`」——本来就不许改整表。并行只指 **不同文件**：`volcsauc/*` + `voice_handlers` 对 W2；`refhost` + 设置页对 W3。

### 6.1 任务表

| ID | 优先级 | 动作 | 文件 | 完成定义 | 不要 |
|---|---|---|---|---|---|
| V1-1 | P0 | FORCE_COMMIT 失败不再 `beginUserTurn`；提交后拆定时器 | `CompanionStage.tsx` | 连续 kick 只 1 次 `onSend` | 加长间隔当修复 |
| V1-2 | P0 | `beginUserTurn` 本回合去重 | `CompanionStage.tsx` | 同一规范化文本不二次发送 | 改状态表 |
| V1-3 | P0 | 月伴 in-flight 只入队 / 返回 false | `SessionPage.tsx` `sendAndChat` | 不再 `resetCompanionTurn` 后开新流 | 文字会话的 queue 行为不要一起重写 |
| V1-4 | P0 | 退出月伴清理 pending / 听写 / seed | `CompanionStage.tsx` `onExit` | 退回打字 10s 内无新的自动 `chat.start` | 清空已落盘的历史消息 |
| V1-5 | P0 | 单测补风暴 | `CompanionStage.*.test.tsx`；SessionPage companion 测 | CI 绿 | 用 e2e 代单测 |
| V2-1 | P0 | `volcListenModelID` | `voice_handlers.go`；`provider_diagnostics.go`；新小函数可放 `volc_speech_canonical.go` | 默认听模型 kind=asr | 用 TTS speaker 当 Resource-Id |
| V2-2 | P0 | 解析 object/array/`payload_msg`/Raw | `protocol.go` + `protocol_test.go` | 数组用例绿 | 猜二进制布局以外的私有协议 |
| V2-3 | P0 | `result_type=full`；合法 dialog_ctx 或省略 | `backend.go` + test | 请求体快照测 | 继续发明热词格式 |
| V2-4 | P1 | `X-Api-Request-Id` | `backend.go` | 头存在 | 把密钥写进日志 |
| V3-1 | P0 | 子进程 Wait → offline + lastErr | `refhost.go` + test | 假崩溃用例 | 改 bat |
| V3-2 | P0 | launching 超时离场 | `refhost.go` | 120s/3min 后不是 launching | 再 spawn 死循环不露原因 |
| V3-3 | P0 | launching 也下发 lastErr；设置页用 `refLaunchPollStatus` | `refcatalog.go`；`SettingsPage.tsx` | 超时可见 jieba/dict 一类原因 | 把完整环境路径打到公网遥测 |
| V3-4 | P0 | 试听在 offline 可点 | `refEnginePreview.ts`；Settings | 不再永久 disabled | 失败当成功 |
| V4-1 | P1 | 云端一轮 + 文案 | `speech.ts`；设置文案 | 桌面一轮 | 静默切火山 |
| V5-1 | P0 | 打含上述修复的包（建议 0.4.45），装官方目录 | `release/` | `--version` 新号；无 next 旁路 | 覆盖 0.4.44 当「已修」 |
| V5-2 | P0 | 三卡真机勾 | 本文 §7 | 全勾才能把语音月伴报回 4.5 | 用单测代勾 |

### 6.2 工时与人员（一个人按序做）

| 波 | 预估 | 说明 |
|---|---|---|
| W-VOICE-1 | 0.5–1 日 | 回合核。不过不要开 V2/V3 的「已修好」口 |
| W-VOICE-2 | 1 日 | 协议 + 挑选。需要本机火山密钥做桌面勾 |
| W-VOICE-3 | 0.5 日 | 宿主诚实。不修用户的 `jieba_fast` 字典 |
| W-VOICE-4 | 0.25 日 | 文案 + 去重已在 V1 |
| W-VOICE-5 | 0.5 日 | 签名包 + 三卡勾。陌生机仍归 W1-R |

### 6.3 给环境的一刀（月汐仓库外，必须写进验收说明）

本机 GPT-SoVITS 缺 `env\lib\site-packages\jieba_fast\dict.txt`。这 **不是** 月汐 bug，但设置页必须把这句话亮出来。用户可从同环境的 `jieba` / `jieba_fast` 包补回 `dict.txt`，或重装该 venv。**禁止** 把补文件写进月汐安装包，也禁止改 `start-api-cpu.bat`。

---

## 7. 真机验收（W-VOICE-5）

环境：已登录、已装 **含本波代码** 的官方包、通道按主合同 W0 配火山密钥。空格不得用单测代勾。

| ID | 卡 | 操作 | 通过 | 失败即停 |
|---|---|---|---|---|
| V-TURN | 任意 | 月伴说一句完整中文；再退回打字等 10s | 只有一轮用户+助手；打字框不再自动重发 | 出现第二条相同用户句 |
| V-VOLC-ASR | 火山 | 灯为火山听；说「打开记事本」 | 字幕出这句或明显同义；不出现 VOICE-004 聋 | 跳到本机/系统识别 |
| V-VOLC-TTS | 火山 | 等助手答完 | 有声（火山音色或灯已写明晓晓） | 无声且无失败文案 |
| V-VOLC-KEY | 火山 | 密钥坏一次 | 停在火山；VOICE-004；插话开关不变 | 改 sherpa |
| V-LOC-HOST | 本地 | 保持当前坏 venv | 不再永久启动中；可见 dict/jieba 一类原因；试听可点并失败 | 仍「启动中…」超过 3 分钟 |
| V-LOC-OK | 本地 | 用户补回 dict 后（或不在本机做，记「环境未复」） | `/docs` 200 后试听出声 | 月汐改了 bat |
| V-CLOUD | 云端 | 选云端，说一句 | 出字、一轮回答、晓晓出声（需联网） | 静默改火山 |
| V-LIGHT | 三卡 | 只看灯 | 听/说/想与所选卡一致 | 本地卡说写成晓晓却标克隆 |

旧 W1-VOICE「启动中试听不可点」**作废**。新标准：真加载 < 90s 可禁用试听；超时或进程死必须可点且诚实。

---

## 8. 报分（只准用这些句子）

- 语音月伴真机 **现在 2.4**（三卡都不能完成一轮）。单测/现码结构仍可报 4.3，但 **对用户必须报 2.4**。
- W-VOICE-1..5 全勾后：语音月伴目标 **4.5**（主合同封顶：不做 Hermes 全双工）。
- 主尺门闩 4.8 **不** 因本波自动升降。语音坏的是模块分，不是 G1 寿命门闩。
- 在 V-VOLC-ASR 勾之前，禁止写「火山听写已齐」。
- 在 V-LOC-HOST 勾之前，禁止写「本地 50 音色可用」。

---

## 9. 落地进度（2026-09-01 代码，未装新包）

| 波 | 代码 | 真机 |
|---|---|---|
| W-VOICE-1 | 已合：`sentThisUtterance`；FORCE_COMMIT 单次；同句 in-flight 返回 false | 须 W-VOICE-5 新包勾 V-TURN |
| W-VOICE-2 | 已合：只挑 ASR；`result` 对象/数组/`payload_msg`；`result_type=full`；dialog_ctx；`X-Api-Request-Id` | 须新包勾 V-VOLC-ASR，禁止用 0.4.44 报已齐 |
| W-VOICE-3 | 已合：Wait→offline+lastErr；3 分钟离场；设置页 `refEngineCaption`；offline 可点试听 | 须新包勾 V-LOC-HOST。不补 `jieba_fast/dict.txt` |
| W-VOICE-4 | 已合：云端文案「听=系统识别，说=晓晓，不能插话」 | 须新包勾 V-CLOUD |
| W-VOICE-5 | 包已打并已装官方目录 | 2026-09-01 mu：`DisplayVersion` 0.4.45；`--rpc-health` `{"engine":"ready","protocol":"1.0","version":"0.4.45"}`；桌面 15140 / 引擎 37916；无 next。§7 真机未勾 |

## 10. 给施工者的第一刀

1. 先做 W-VOICE-1（回合）。不先改协议。
2. 再并行 W-VOICE-2 / W-VOICE-3。
3. 不要 `generate:bridge`，不要改迁移，不要碰主合同冻结表。
4. 需要密钥时走 DPAPI 租约；日志禁止打 API Key。
5. 打完包再勾 §7。用现在的 0.4.44 勾语音 = 无效。
