# 月伴对打 4.8 整改合同（复核后施工）

> **已覆盖：** 权威施工合同是  
> [`2026-09-01-companion-voice-upgrade.md`](./2026-09-01-companion-voice-upgrade.md)。  
> 产品裁定见 [`2026-09-01-companion-three-path-48.md`](./2026-09-01-companion-three-path-48.md)。  
> 本文 §5.7 / §5.8（云端换听写、云端自动插话）**作废**。

日期：2026-09-01  
产品：月汐 / Lunitide `VERSION` **0.4.47**（源码波次含级联 + talk 形状适配；官方目录未升本包前，真机仍按旧包装报）  
状态：**已被 three-path 合同覆盖**。通话核 `talk.*` 细节仍可参考本文 §5.1–5.4。  
级联合同 `2026-09-01-companion-voice-fluency-remediation.md` **不废**，是降级路径和工具轮的底座。  
画板：`companion-voice-48-remediation.canvas.tsx`

冻结仍以语音全链路 §0 / 主合同 §8 为准，本文只**书面放开一条**：闲聊允许走独立通话核。  
**不放开：** 对话 LLM 挂到现有 `volc_speech` SAUC / unidirectional TTS；复活 MiniCPM-o；改 SoVITS bat / 9880；重写 `useCompanionMachine` 整表；火山聋切 sherpa。

**不能报：** 级联再抠定时器就是 4.8、三卡都能 4.8、上了 Realtime 就是 5.0、主尺加权已是 4.80、正式发布、语音模块 4.5（旧 §7 未勾）。

目标：日常有通话核时对打 **4.8**（冲刺 4.9）。云端卡、本地卡另走 §5.7 / §5.8，目标约 **4.5**，**不报 4.8**。**不报 5.0。**

---

## 0. 复核结论（上一份通道稿哪里不够）

通道稿方向对：**4.3 是级联墙，4.8 要换核。** 按现码和契约复核后，必须改掉这些，否则会中断或卡死。

| # | 通道稿 | 现码 / 契约事实 | 本文怎么改 |
|---|---|---|---|
| 1 | 让用户先选 A/B/C，B 是新 `realtime` protocol | `ProviderProtocol` 只有 `openai_compatible` / `anthropic` / `volc_speech`。SQLite `CHECK` 和 schema 生成锁死。新协议 = 迁移 + `generate:bridge` + 全表测试 | **默认 A\***：不新增 protocol。通话核用现有 `openai_compatible` 上**已存在**的 realtime/live 模型 id。没有该模型就留级联，横幅诚实 |
| 2 | 事件名写成 `realtime.*` | 仓库已有死路径 `omni.start/append/stop`（MiniCPM-o，灯已折云端） | **禁止复用 `omni.*`**。新方法 `talk.start` / `talk.append` / `talk.cancel`，避免把本机 GPU 全双工当 4.8 |
| 3 | 「闲聊走通话核、工具再切级联」一句话 | 现码 `companionWantsTools` 是字面针（「打开」「周杰伦」「天气」）。通话核先出的是音频，字在旁路。误切会中断对打 | 专列 **W-RT-3 交接协议**：只根据**已稳定的用户转写**切；半句「帮我打开」不切；切之前先停通话核出声 |
| 4 | 没写双听 | 级联 ASR（火山/sherpa/Web Speech）和通话核都会占麦 | **通话核会话期间级联 ASR 必须停**。降级时先停 talk 再开 ASR。否则双轮风暴 |
| 5 | 没写回声 | 通话核边播边听，比级联更易把她的话当插话 | 通话核必须用会话自带的 AEC / barge 门；**禁止**把级联 `ECHO_GUARD` 收到 &lt;300ms |
| 6 | 没写唤醒、记忆、落盘 | 首页 `wakeWord` 仍走级联听写；`clipCancelledCompanionPersist` 只认标点 | 唤醒进舞台后**一次** `talk.start`；persist 用已读转写，不靠最后一个句号 |
| 7 | 没写 WebView2 出声 | 现 TTS 走引擎 `tts.synthesize`。通话核音频若只推到页面，可能被自动播放策略挡住 → 假卡死 | 引擎播 PCM **或** 页面 `AudioContext` 解锁（沿用 `unlockTtsAudio`）。8s 无首包必须诚实失败 |
| 8 | 没写无实时模型时的分数 | 现场常见是 `glm-4-air` / DeepSeek，**不是** Realtime | 没配 realtime 模型：**对打停 4.3**，禁止假装 4.8 |
| 9 | 工时低估 B | 新 protocol 不是 2 日 | B 从本波拿掉。若以后要独立供应商，另开合同 |
| 10 | 「4.8 以上」没定义 | 5.0 要三卡都像电话 | **4.8 门闩；4.9 冲刺；5.0 不报** |

级联合同里已落地、**不要退**的：F-1 舞台不得绕过 `forceCommit`；W1 完整句 220+280；W2 首声门闩 / 不复读；W3 `chat.start` 与 append 并行；W-STUCK SoVITS ≤10s；W5 舞台已读截句。  
级联还没做、**本波仍要做**（给工具轮和降级）：W4 流式首包；W-FLASH 月伴静默闪模型；W-PACK 级联包；W0 二十轮真数（没数不准改 12s stall / 回声门）。

---

## 1. 三把尺（报分只准用这些句子）

| 尺 | 现在 | 级联做完并勾 F1–F4 | 本文门闩 | 冲刺 | 不是 |
|---|---|---|---|---|---|
| 对打体感 | 2.9（未勾 F1–F4） | 4.1 / 级联顶 4.3 | **4.8**（通话核闲聊+工具交接，真机勾完） | **4.9**（再加 W4 + 闪模型 + 交接不回退） | 主尺 4.80 |
| 语音模块 | 旧 §7 未齐则不得报 4.5 | 4.5 封顶（主合同：不做 Hermes 全双工） | 模块仍 ≤4.5 | — | 对打 4.8 |
| 主尺门闩 / 加权 | 门闩可报 4.8；加权乘不满 4.80 | 不因本文升降 | 不升 | 不升 | 对打 |

三张卡（对打体感，不是模块齐）：

| 卡 | 现码墙 | 只做级联重叠 | 本文目标 | 上不去的原因 |
|---|---|---|---|---|
| 日常 + 通话核 | 2.9 | 4.3 | **4.8 / 冲刺 4.9** | — |
| 云端（Web Speech + 晓晓） | ~2.6 | **3.8** | **4.5**（必须换听写） | 不换 Web Speech 则不能插话，停 3.8 |
| 本地（sherpa + SoVITS CPU） | ~2.3 | **3.3** | **4.2 克隆 / 4.5 本机听·晓晓读** | 不改 bat 时克隆首包常 >1s；4.5 不标克隆 |

综合对打按**用户当前选的卡**报，不三卡平均，不把云端/本地报成 4.8。

---

## 2. 对标（对齐到可抄的形状，不抄运行时）

| 对标 | 对齐到月伴哪一层 | 落地 | 明确不对齐 |
|---|---|---|---|
| OpenAI Realtime / Gemini Live | 通话核会话 | `talk.*` 走供应商已有 realtime/live 模型；音频进音频出 | 不把月伴重写成浏览器 Demo；不写死未开通的模型名 |
| 豆包实时通话 | 同一通话核，另一家 URL | 仅当用户已配该模型 / 兼容网关 | **禁止**接到现有 SAUC 听写或 unidirectional TTS |
| Azure / 火山 seed-asr 当「云端听」 | 云端卡听写 | W-CLOUD：换掉 Web Speech，说仍晓晓 | 静默改成火山卡；不换灯 |
| Edge 流式首帧 | 云端/晓晓说 | W4 `edge_ws` 首块就播 | 凑齐整段 WAV |
| SoVITS api_v2 `streaming_mode` | 本地说 | 只改客户端请求，不改 bat | 未测 CPU 就开；失败仍标克隆成功 |
| LiveKit 假打断恢复 | 插话 + 已读 | W-RT-2；级联 W5 | 不上 SFU |
| TEN「说完/没说完」 | 级联收句 | 现 `looksIncompleteUtterance` | 不上 7B 分类器 |
| MiniCPM-o / Hermes 全双工 | — | 不复活 | `omni.*` 保持折云端 |

业界对照（不是本机埋点）：人类轮换 ~200ms；调好级联 500–800ms（=本尺 4.3）；语音原生闲聊 200–400ms（=本尺 4.6–4.8）。

---

## 3. 目标结构

```text
进月伴
  ├─ 已配 realtime/live 模型 ──► talk.start（一次会话，多轮）
  │     麦 PCM 16 kHz ──► talk.append
  │     talk.audio ──► 解锁后的播放器（8s 无首包 = 失败，切级联并横幅）
  │     talk.transcript ──► 字幕旁路
  │     插话 = talk.cancel 出声 + 会话继续听
  │     用户转写稳定且 companionWantsTools ──► 先停出声，再一次 chat.start
  │     工具 / UAC / 电脑控制走现运行时
  │     做完一句口语回灌（talk 或级联 TTS），回到听
  │
  └─ 未配 / talk 失败 ──► 现级联（火山听·晓晓/火山读 / 云端 / 本地）
        分数停在级联顶 4.3；灯诚实
```

通话核会话期间：**级联 `startCompanionSpeech` / volc / sherpa 必须是停的。**

---

## 4. 风险登记（中断 / 卡壳 / 切坏）

做 4.8 之前先看这些。P0 本波要堵。

### 4.1 会重开风暴（中断对打）

| ID | 风险 | 现码 / 通道稿 | 整改 |
|---|---|---|---|
| R-STORM-1…4 | 级联 FORCE_COMMIT / 投机双开 / 复读 / 退出再冲 | 级联合同已收 | 保持。通话核不重开这些口子 |
| **R-RT-STORM-5** | 级联 ASR 与 talk 同时听 | 通道稿未写 | 进 talk 先 `speechHandle.stop`；退出 talk 再 `startListening` |
| **R-RT-STORM-6** | 交接时 talk 还在说，又 `chat.start` | 会双声 + 两轮历史 | 交接：`talk.cancel` 出声 → 等 播完/300ms → **一次** `chat.start` |
| **R-RT-STORM-7** | 半句「帮我打开」当工具切走 | `companionWantsTools("打开")` 过宽 | 只认**稳定转写**；`looksIncompleteUtterance` 为真则不切（对齐 F3） |
| **R-RT-STORM-8** | 唤醒进舞台再 `talk.start` 一次、级联 seed 再开一轮 | `HomeWake` + `seedPrompt` | seed 只喂 talk 会话或只喂级联，**二选一** |
| **R-RT-STORM-9** | 复用 `omni.*` 拉起 MiniCPM | 现码仍有方法 | 新方法必须叫 `talk.*`。单测禁止 companion 闲聊调 `omni.start` |

### 4.2 会卡在「回应中 / 说话中 / 接通中」

| ID | 风险 | 整改 |
|---|---|---|
| R-STUCK-1…10 | 级联 12s stall / 工具 / UAC / SoVITS / 熔断 | 级联合同保持 |
| **R-RT-STUCK-11** | `talk.start` 一直「接通中」 | 连接预算 **8s**；超时横幅「通话核未就绪，这轮用语模型」，自动级联。禁止空听 |
| **R-RT-STUCK-12** | 有会话无喇叭（自动播放 / 未解锁） | 进舞台先 `unlockTtsAudio`；8s 无 `talk.audio` = 同 STUCK-11 |
| **R-RT-STUCK-13** | 会话空闲被服务端踢，下一句无声 | 空闲 60s 可静默续 `talk.start` 一次；再失败切级联。禁止假「说话中」 |
| **R-RT-STUCK-14** | 交接后 `chat.start` 走 12s stall | 工具轮沿用 W-STUCK：stall 不计、20s 口播、「要你点」 |
| **R-RT-STUCK-15** | 通话核烧钱一直开麦 | 退出舞台必须 `talk.cancel`；首页唤醒**不**开 talk（仍用语听写） |

### 4.3 会切坏准度（假快）

| ID | 风险 | 整改 |
|---|---|---|
| R-CUT-1…5 | 级联证件逗号 / 硬顶 / persist / 回声门 | 保持 |
| **R-RT-CUT-6** | 把自己的出声当插话 | 用会话 barge 策略；本机再加 ≥300ms 出声抑制。禁止为快拆门 |
| **R-RT-CUT-7** | 「打开心扉」当 desktop.open | 工具针沿用现列表，但必须完整句；闲聊歧义留给通话核说，不切 |
| **R-RT-CUT-8** | persist 整段实时转写 | 落盘 = 用户稳定句 + 助手**已读出**转写。未读尾巴丢掉 |
| **R-RT-CUT-9** | 灯写「火山听」实际已是通话核 | 灯：听/说 =「通话核」；想 = 供应商名。失败回级联时灯改回三卡 |

### 4.4 契约 / 冻结

| 诱惑 | 裁定 |
|---|---|
| 新增 `ProviderProtocol=realtime` | **本波不做**（SQLite CHECK） |
| 对话走 SAUC / unidirectional | 拒绝 |
| 复活 `omni.*` / MiniCPM-o | 拒绝 |
| 改 SoVITS bat | 拒绝 |
| 重写状态机整表 | 拒绝。talk 是旁路会话；工具轮仍走现表 |
| 云端卡承诺 4.8 | 拒绝 |
| 没配 realtime 模型仍报 4.8 | 拒绝 |
| 报 5.0 / 主尺 4.80 | 拒绝 |

---

## 5. 详细整改计划（一波不过不宣称下一波已齐）

```text
G0    级联收口（已写源码）+ W-PACK-C  可先装 0.4.47 勾 F1–F4     对打 4.1 / 顶 4.3
 │
 ├─► W4 流式首包（级联）              工具轮 / 降级 +0.1
 ├─► W-FLASH 月伴静默闪模型           无新设置
 │
 ▼
G1    W-TALK-0  契约 talk.*           P0
 ▼
G2    W-TALK-1  闲聊一次会话          P0   ← 有模型后可报 4.6
 ▼
G3    W-TALK-2  插话 + 已读 + 双听互斥 P0
 ▼
G4    W-TALK-3  工具交接              P0   ← 勾完才报 4.8
 ▼
G5    W-PACK-T  0.4.48+ 装包真机勾
 │
 ├─► W-CLOUD  云端换流式听写 + 晓晓首包   本卡目标 4.5；不换听写停 3.8
 └─► W-LOCAL  本机预热 + 可选流式克隆     克隆顶 4.2；4.5 只报「本机听·晓晓读」
```

顺序不得倒：没 `talk.*` 契约不许写舞台；没双听互斥不许宣传插话；没交接不许报 4.8。

### 5.1 G0 — 级联收口（底座，不换核）

| 步骤 | 文件 / 动作 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| G0-1 | 现源码 | 保持 F-1、W0–W3、W-STUCK、W5 | 定点单测保持绿 | 为通话核回退这些 |
| G0-2 | `internal/tts/volc.go` `edge_ws.go` `ttsPlayer.ts` + bridge | W4：`tts.chunk` 或可读流；火山按行 NDJSON | 契约与生成文件一起提交；首片 &lt;200ms | 只 generate 不提交；改 Resource-Id |
| G0-3 | `modelKind.ts` `App.tsx` `SessionPage.tsx` | W-FLASH：月伴从**当前供应商已有**模型挑 flash/air/lite/mini/haiku；没有则首页模型 | 单测：有 flash 用 flash；没有不发明 id | 新设置项；写死 `glm-4-flashx` |
| G0-4 | `VERSION` | 级联包建议 **0.4.47** | Quality 门禁 + Verify-Release | 用 0.4.46 勾本波 F1–F4 |
| G0-5 | 真机 | 旧合同 F-SAFE、F1–F8、V-TURN、V-VOLC-*、V-CLOUD、V-LOC-* | 勾完级联对打报 **4.1** | 没装包报 4.1 |

### 5.2 G1 — W-TALK-0 契约

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| T0-1 | `internal/bridge` schema | 方法：`talk.start` `{providerId,modelId,sessionId}`；`talk.append` `{sessionId,pcm}`；`talk.cancel` `{sessionId,mode:output\|all}`。事件：`talk.audio` `talk.transcript` `talk.tool` `talk.error` `talk.ended` | `verify:bridge --check` + 提交生成物 | 复用 `omni.*`；新增 protocol 枚举 |
| T0-2 | `internal/gateway` 新适配 | 只对接 **已列在该供应商 models[] 里**、id/显示名匹配 `(?i)realtime|live|realtime-preview` 的模型。PCM 16 kHz mono | 单测：模型不匹配 → 明确错误码 `TALK_MODEL_UNSUPPORTED` | 把 `glm-4-air` 当 realtime |
| T0-3 | `companionLights.ts` `VoicePathPicker.tsx` | 探测到可通话模型时多一行说明：「闲聊走通话核；没配则用语模型」。灯在会话中改「通话核」 | 设置页可见 | 静默换核 |
| T0-4 | 网络策略 | talk 的 wss/https 走现网关白名单扩展（若有） | 失败露出 URL 类原因 | 裸连绕过策略 |

### 5.3 G2 — W-TALK-1 闲聊一次会话

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| T1-1 | `CompanionStage.tsx` | 可通话且非工具：`talk.start` 一次；之后只 `talk.append`。闲聊 **0 次** `chat.start` | 单测：`你好` / `今晚月色如何` start 次数=0 | 每句重开会话 |
| T1-2 | 播放 | `talk.audio` → 已解锁播放器 | F-RT1：说完→第一声正经回答 **≤400ms**（本机联网） | 只播「嗯」；再走 `tts.synthesize` |
| T1-3 | 降级 | `TALK_MODEL_UNSUPPORTED` / 8s 无首包 / `talk.error` → 停 talk，开级联听写，横幅「通话核未就绪，这轮用语模型」 | 不空听 | 假成功 |
| T1-4 | 多轮 | 同一 `talk` 会话撑 20 轮闲聊 | F-RT2：无闪灯、无双用户句 | 退出 10s 内无自动 `chat.start`（沿用 V-TURN） |
| T1-5 | 人设 | talk 系统指令只要身份 + 短开口；桌面长文不进闲聊 | 闲聊请求无 desktop.open 说明书 | 闲聊挂全量 tools |

通过且真机勾 F-RT1/2：**4.6 / 5**（仅该核闲聊）。

### 5.4 G3 — W-TALK-2 插话、已读、双听

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| T2-1 | `CompanionStage` | 进 talk 停级联 ASR；出 talk / 降级再开 | 单测：talk 活跃时 `startCompanionSpeech` 不被调 | 两路麦 |
| T2-2 | `talk.cancel mode=output` | 插话 300ms 内无残声；会话继续听 | F-RT3 | 再开一条 TTS 盖 |
| T2-3 | persist / 舞台 rounds | 助手只留已读出转写（W5 语义，不靠最后标点） | 历史无未读后半句 | 只改引擎启发式、不改舞台 |
| T2-4 | 出声抑制 | 播放 talk.audio 期间忽略短于 2 字的本地回声 | 沿用 `looksLikeBargeInSpeech` | 回声门 &lt;300ms |

### 5.5 G4 — W-TALK-3 工具交接（4.8 门闩）

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| T3-1 | `talk.transcript` + `companionWantsTools` + `looksIncompleteUtterance` | 稳定且完整且要工具 → 停出声 → **一次** `chat.start`（messages 带本句） | 「帮我打开」+0.5s+「网易云」一轮；不先瞎执行 | 半句就切；通话核里重写 desktop |
| T3-2 | `toolruntime` 现有 | function call 若厂商支持：只当意图，执行仍走引擎工具 | F7 关电脑控制横幅；F8 UAC「要你点」 | 代点 UAC |
| T3-3 | 回灌 | 工具结束后一句口语：优先 talk 注入文本让它说；talk 已死则级联 TTS | 不复读已读（W2） | speakCloseout 整段 |
| T3-4 | 进度 | 工具 &gt;20s 口播「还在做」 | W-STUCK | 12s 自杀 |
| T3-5 | 天气/搜索 | 现 `companionWantsTools` 含「天气」会切级联——**保持**（要工具）。纯闲聊「今晚月色如何」不切 | 单测两条对立 | 把月色也当搜索 |

通过 F-RT4/5：**4.8 / 5**。

### 5.6 G5 — W-PACK-T 与冲刺 4.9

| 步骤 | 做 | 不要 |
|---|---|---|
| P-T1 | `VERSION` **0.4.48**（若 0.4.47 已是级联包） | 用 0.4.46/47 勾通话核 |
| P-T2 | 签名 + Verify-Release；`release/out` 只留本号 | next 旁路盖官方目录 |
| P-T3 | 官方目录升级后再勾 §6 | `Test-Install.ps1` |
| P-T4 | 未勾 F-RT1…5 仍按级联尺报 | 报 4.8 / 4.9 / 5.0 |
| 4.9 | W4 + W-FLASH 真机也勾；交接 20 轮工具不回退级联空听 | 把 4.9 写成 5.0 |
| W-CLOUD / W-LOCAL | 见 §5.7 / §5.8 | 云端/本地报 4.8 |

### 5.7 W-CLOUD — 云端卡冲 4.5

现码墙：**听 = WebView2 Web Speech**。它自己抓麦、TTS 时不能 mute、不能插话。级联重叠（W1–W5、W4 晓晓首包、闪模型）只能把云端从 ~2.6 抬到 **3.8**。再抠定时器不会到 4.5。

4.5 的必要条件：听写改成**引擎可控的流式 ASR**，说仍是晓晓。结构与火山级联对齐（可插话、可 mute），只是说走 Edge。

```text
云端卡（4.5）
  听  流式 ASR（优先：已配的 volc seed-asr；否则保持 Web Speech = 停 3.8）
  说  晓晓（W4：edge_ws 首块 MP3 就 scheduleBuffer）
  想  同一 chat.start / 闪模型
  灯  「听 云端流式 · 说 晓晓，可插话」
      无流式密钥时：「听 系统识别 · 说 晓晓，不能插话」
```

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| C-1 | `asrPath.ts` `CompanionStage.tsx` | 云端卡：若 `volc_speech` 已配，听走 seed-asr（现 `volcSpeech.ts`），**不改 voicePath=volc** | 灯不是「火山听·火山读」；说仍 `engine=edge` | 静默整卡改火山；火山聋再切 Web Speech 当成功 |
| C-2 | 同上 | 未配火山听写：仍 Web Speech，横幅保持「不能插话」 | F5 能走完；本卡报分 **3.8** | 没密钥也开插话 |
| C-3 | `edge_ws.go` `ttsPlayer.ts` | W4 晓晓：首帧 `Path:audio` 就交给播放器 | 本机联网首包 &lt;200ms | 等整段再播 |
| C-4 | `CompanionStage` | C-1 生效后 `voiceBargeIn` 对云端卡可用（与火山同一门：≥2 字、回声门 ≥300ms） | F-CL1：回答中插话 300ms 停声 | Web Speech 路上开插话 |
| C-5 | 设置文案 | 「云端：有语音模型密钥时听=流式、说=晓晓、可插话；否则听=系统识别、不能插话」 | 设置页可见 | 把晓晓写成火山音色 |

通过（有流式听写 + W4 + 级联 F1–F4 同构勾完）：云端卡 **4.5 / 5**。  
无流式听写：云端卡 **3.8 / 5**。  
有通话核模型时，云端闲聊也可走 talk（§5.2），那时按通话核报 4.6+，灯改「通话核」，不再报「云端 4.8」。

风险：

| ID | 风险 | 整改 |
|---|---|---|
| R-CL-STORM | 云端卡同时开 Web Speech + seed-asr | 只开一路。C-1 成功则停 Web Speech |
| R-CL-CUT | 用户以为选了免密钥云端，音频却进了火山 | 灯必须写「云端流式」；设置说明密钥用途 |
| R-CL-STUCK | seed-asr 聋，云端卡空听 | 2 次重启后停在云端，文案 VOICE-004 类；**不**暗切系统识别当 4.5 |

### 5.8 W-LOCAL — 本地卡：克隆 4.2，4.5 只走晓晓车道

现码墙：**说 = GPT-SoVITS CPU、`streaming_mode=false`、冷启动**。听已经是 sherpa，可插话。级联重叠 + W-STUCK 10s 只能到 **3.3**。  
冻结不改 `start-api-cpu.bat` / 9880 / 盘符。客户端把 `streaming_mode` 打开 **不算改 bat**，但必须先在本机测 CPU，卡顿就回退。

克隆音色在 CPU 上首句常常 1.5–4s。说完→第一声很难稳在 800ms。所以：

| 车道 | 听 | 说 | 目标 | 灯 |
|---|---|---|---|---|
| 克隆（默认本地卡） | sherpa | SoVITS | **4.2** | GPT-SoVITS |
| 速度（可选） | sherpa | 晓晓 | **4.5** | 「本机听 · 晓晓读」，**禁止**写克隆成功 |

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| L-1 | `CompanionStage` `refhost` | 进本地卡先 `tts.ensureRefEngine`；预热一句极短合成丢掉 | 第一轮用户说话前引擎已 online 或已诚实失败 | 假启动中超过 10s |
| L-2 | `refaudio.go` | 月伴轮尝试 `streaming_mode=true`；本机 CPU 占用导致 clip 间隔 &gt;200ms 则回退 false | 单测：两种模式都可解析。真机记 W0 | 未测就默认开；改 bat |
| L-3 | `ttsPlayer.ts` | 保持 ≤2 次 / 10s；失败灯「本机朗读未就绪」+ 这轮晓晓 | 已有 W-STUCK | 失败仍标 50 人生 |
| L-4 | `takeSpeakableChunk` | 本地卡第一段尽量短（已有 ≥8 字门闩） | 首段更早上 SoVITS | 证件逗号处出声 |
| L-5 | 设置 | 本地卡增加「朗读要快时用晓晓（仍是本机听写）」开关，默认关 | 打开后灯「本机听 · 晓晓读」 | 默认打开；用户没开却走晓晓还写克隆 |
| L-6 | 闪模型 | 本地卡同样 W-FLASH | 想不走推理大模型 | 为快打开推理 |

报分：

- L-1…L-4 勾完、克隆出声、可插话、不卡死：本地卡 **4.2 / 5**。
- L-5 打开且 F-L1（你好 ≤800ms）勾完：该车道 **4.5 / 5**，文案必须是本机听·晓晓读。
- 克隆车道 **不报 4.5**，除非 W0 证明本机 SoVITS 首包稳 &lt;300ms（CPU 机几乎不会）。

风险：

| ID | 风险 | 整改 |
|---|---|---|
| R-LOC-STUCK | `streaming_mode` 把 CPU 钉死，舞台假说话中 | 首轮超时 10s 回退非流；再失败晓晓 + 诚实灯 |
| R-LOC-CUT | 预热「啊」被当用户句 | 预热 mute 麦 / 回声门武装到预热结束 |
| R-LOC-STORM | 预热未完用户开说，pending 再冲 | 预热期间只上字幕，不 `beginUserTurn` |

---

## 6. 验收（装对应新包后勾；单测绿不加分）

### 6.1 级联包（0.4.47）

沿用级联合同 §5：F0、F-SAFE、F1–F8、V-TURN、V-VOLC-ASR/TTS/KEY、V-CLOUD、V-LOC-HOST、V-LIGHT。  
未勾前对打 **2.9**；勾完 **4.1**；含 W4 仍 **≤4.3**。

### 6.2 通话核包（0.4.48+）

环境：已登录；已配 **匹配 realtime/live 的模型**；官方目录已是本号。

| ID | 波 | 动作 | 通过 | 失败即停 |
|---|---|---|---|---|
| F-RT0 | T0 | 设置能看出有/无通话核 | 无模型时不空听，用语模型 | 灯写火山听却已是 talk |
| F-RT1 | T1 | 「你好」 | 第一声正经回答 ≤400ms | 只听嗯，或 ≥800ms |
| F-RT2 | T1 | 「今晚月色如何」×20 | 一轮问一轮答；无闪灯 | 每句 `chat.start` |
| F-RT3 | T2 | 回答中插话 | 300ms 内停声；历史无未读后半句 | 残声；双麦再开一轮 |
| F-RT4 | T3 | 「帮我打开」+0.5s+「网易云」 | 一轮；交接级联工具 | 半句就打开；通话核假装打开 |
| F-RT5 | T3 | 真 UAC | 「要你点，我不能代点是」 | 代点 |
| F-RT6 | T1 | 拔掉 realtime 模型再进 | 横幅降级，级联能走完 | 空听 / 假 4.8 |
| F-RT7 | T2 | talk 中看级联识别器 | 未 start | 双字幕双轮 |
| V-TURN | 对齐旧合同 | 退回打字 10s | 无自动重发 | 连发 |

### 6.3 云端卡 4.5（有流式听写时）

| ID | 动作 | 通过 | 失败即停 |
|---|---|---|---|
| F-CL0 | 看灯 | 听=云端流式，说=晓晓；不是火山读 | 灯写火山听·火山读 |
| F-CL1 | 「你好」 | 第一声正经回答 ≤800ms | 只听嗯；仍 Web Speech 却报 4.5 |
| F-CL2 | 回答中插话 | 300ms 停声 | 不能插话却报 4.5 |
| F-CL3 | 拔掉火山听写密钥 | 回系统识别 +「不能插话」；能走完 | 空听 |

无流式听写只勾旧 V-CLOUD：云端 **3.8**。

### 6.4 本地卡

| ID | 动作 | 通过 | 报分 |
|---|---|---|---|
| F-L0 | 进本地卡 10s 内 | 引擎 online 或诚实失败，无假启动中 | 底座 |
| F-L1 | 克隆开，「你好」 | 能走完、可插话；首声不承诺 800ms | **4.2** |
| F-L2 | 打开「朗读要快时用晓晓」 | 灯「本机听 · 晓晓读」；你好 ≤800ms | **4.5**（仅该车道） |
| F-L3 | SoVITS 坏 venv | ≤10s 失败或改晓晓，灯不写克隆成功 | 与 F6 同 |

---

## 7. 报分

- 未勾级联 F1–F4：对打 **2.9 / 5**。
- 级联 F1–F4 勾完：**4.1 / 5**。W4 再加约 0.1，**不超过 4.3 / 5**。
- 无 realtime 模型：即使代码合了 talk，**仍报级联尺**。
- F-RT1/2 勾完：**4.6 / 5**（该核闲聊）。
- F-RT3–5 勾完：**4.8 / 5**。
- F-RT1–7 + 级联 W4/闪模型都勾、交接不回退：**4.9 / 5**。
- 云端：仍 Web Speech → **3.8**；C-1…C-4 + F-CL1/2 → **4.5**。**不报 4.8**。
- 本地：克隆车道 → **4.2**；L-5 速度车道 + F-L2 → **4.5**。克隆车道无 W0 首包数 **不报 4.5**。
- **不报 5.0。**
- 主尺门闩 / 加权 4.80 / 语音模块 4.5：**不**因本文自动升降。
- 不上通话核：**不报 4.6+**（云端/本地 4.5 是卡自己的尺，不是日常综合 4.6）。

---

## 8. 工时（一人）

| 波 | 预估 | 依赖 |
|---|---|---|
| G0 W4 + W-FLASH | 1.5–2 日 | 级联源码已在 |
| G0 W-PACK-C | 0.5 日 | 至少 F-1+W1+W2+W-STUCK |
| W-TALK-0 | 2–3 日 | 契约；供应商已有 realtime 模型 |
| W-TALK-1 | 2–3 日 | T0 |
| W-TALK-2 | 1–1.5 日 | T1 |
| W-TALK-3 | 2–3 日 | T1；现工具运行时 |
| W-PACK-T | 0.5 日 | F-RT1…4 可勾 |
| W-CLOUD | 1–1.5 日 | 已配 volc 听写；与 W4 并行 |
| W-LOCAL | 1 日 | 不改 bat；streaming 须本机 CPU 抽样 |

合计到可勾日常 4.8：**约 10–14 日**（含级联包）。  
云端 4.5、本地 4.2/4.5 另计约 **2–2.5 日**，可与 talk 并行。  
供应商里没有 realtime/live 模型：通话核工时为 0，日常综合停 4.3；云端/本地仍可按 §5.7 / §5.8 单独抬。

---

## 9. 范围外（写进合同以免做歪）

- 会议纪要 / 人脉语音 / 首页唤醒改通话核
- 新增 `ProviderProtocol`
- 本地 GPU 全双工、Pipecat 运行时、LiveKit 房间
- 主尺寿命 / 桌面准度 / 正式发布
- 把云端卡、本地卡报成 4.8 / 5.0
- 不换 Web Speech 却报云端 4.5
- 克隆 CPU 未测首包却报本地 4.5

---

## 10. 开工顺序（落地默认，不再等 A/B/C）

1. 先 G0：W4、W-FLASH、级联 0.4.47、真机 F1–F4（对打到 4.1 / 顶 4.3）。  
2. 查当前供应商 `models[]` 有没有 realtime/live。没有就配一条**已开通**的，不发明 id。  
3. 再 G1–G5。没有模型禁止开工 talk 舞台。  
4. 云端 4.5：有火山听写密钥就做 W-CLOUD（听流式、说晓晓）；没有则诚实停 3.8。  
5. 本地：先 L-1…L-4 到 4.2；要 4.5 打开「本机听·晓晓读」，不要把克隆报成 4.5。
