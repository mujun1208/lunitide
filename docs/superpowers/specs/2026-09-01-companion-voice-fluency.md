# 月伴语音对答提速提准方案（三条链路）

日期：2026-09-01  
产品：月汐 / Lunitide `VERSION` **0.4.47**  
状态：**分析底稿**。施工以 `2026-09-01-companion-voice-fluency-remediation.md` 为准（复核后已改波次：取消投机开流、纠正垫「嗯」默认、补 FORCE_COMMIT 残差与工具轮卡壳）。  
画板：`companion-voice-fluency-remediation.canvas.tsx`

本文只回答：三条卡已经「满」之后，为什么对话仍慢、不准，以及怎么把对打感拉到接近电话，而不是再补一张卡。

**不能报：** 语音已齐所以流畅、全云端就该 200ms、换成 OpenAI Realtime / 豆包全双工才算方案、主尺 4.80、正式发布。

---

## 0. 结论（先看这个）

三条卡都是 **听 + 想 + 说** 的级联管线，不是一条音频进、音频出的通话。  
「全部用云端」只换了供应商，**没有换结构**。慢的是串行等待，不是缺云。

| 尺 | 现码（0.4.46，能走完一轮） | 按本文做完 | 不换核的天花板 |
|---|---|---|---|
| 功能齐（听→想→说一轮） | **4.2** | 4.3 | 4.5 |
| 对答速度（说完到第一声可读） | **2.7** | **4.1** | 4.3 |
| 听写准确 / 不切句 | **3.2** | **4.0** | 4.4 |
| 打断自然（火山/本地） | **3.0** | **4.0** | 4.5 |
| **综合对打** | **2.9 / 5** | **4.1 / 5** | **4.3 / 5** |

云端卡单独：现码 **2.6**，Web Speech 不换引擎最多 **3.8**；火山卡现码 **3.0**，优化后 **4.2**。  
要到 4.6–4.8 必须上语音原生模型（Realtime / 豆包实时通话）。那会把对话 LLM 挂到音频通道上，**违反冻结：不在 openspeech 上挂对话 LLM**，也会丢掉桌面工具这条产品线。本文不走那条。

最佳接近方案：**留下三条卡的身份，把级联做成 Pipecat / LiveKit 那种可重叠管线**——语义收句、投机开想、流式首包朗读。不重写 `useCompanionMachine`，不换核。

---

## 1. 现码三条链路（对打视角）

权威拆卡仍是语音合同 §1。这里只写**用户感知延迟**从哪来。

```text
麦 PCM 16 kHz
    │
    ├─ 火山  听 seed-asr WS ── voice.append ── onFinal
    ├─ 本地  听 sherpa     ── voice.append ── onFinal
    └─ 云端  听 Web Speech ── onresult     ── onFinal     ← 不能插话
                │
                ▼
         beginUserTurn（同一句只一次 chat.start）
                │
                ├─ 可选垫「嗯」（instantAck，又走一遍云端 TTS）
                ▼
         sendAndChat → message.append → chat.start（glm-4-*，DisableReasoning）
                │
                ▼
         takeSpeakableChunk 等到 。？！ 或 800ms 停顿
                ▼
         tts.synthesize 整段 WAV（火山 HTTP 单向 / Edge WS / SoVITS 非流）
                ▼
         播完 + ECHO_GUARD 450ms + POST_SPEAK_ECHO 900ms → 再听
```

关键文件：

| 层 | 路径 | 对打作用 |
|---|---|---|
| 收句 | `web/src/session/companion/speech.ts` | `UTTERANCE_SILENCE_MS=280`，不完整句再等 900–2200ms，`FORCE_COMMIT_MS=1400` |
| 听路由 | `asrPath.ts` / `volcSpeech.ts` / `localSpeech.ts` / `speech.ts` | 三卡身份 |
| 回合 | `CompanionStage.tsx` `beginUserTurn` | 已堵连发；垫「嗯」；首句朗读门闩 |
| 切句朗读 | `companionText.ts` `takeSpeakableChunk` | **必须等到句号**，逗号不算 |
| 想 | `internal/app/chat.go` `companion:true` | 关推理、2048 token、闲聊可卸工具；人设很长 |
| 说 | `ttsPlayer.ts` + `internal/tts/{volc,edge,refaudio}.go` | 先合成完整 WAV 再播；预取下两段 |

已经做对、不要退回去的：

- 同一句只一次 `chat.start`；进行中同一句不 reset 再开流
- 火山聋不切 sherpa；本地克隆不假启动中；不把晓晓标成本地
- 月伴 `DisableReasoning`；闲聊不挂技能目录
- 人设要求「第一句 8–20 字并以。？！结尾」——**前端却要等这个句号才开 TTS**

---

## 2. 为什么「全云端」仍不流畅

人类轮换间隙中位大约 **200ms**。业界调好的级联（Pipecat / LiveKit + 流式 ASR/TTS）落在 **500–800ms**。再慢就会被当成「掉线」。

月伴闲聊、无工具、网络正常时的**串行账**（估算，不是本机埋点；W0 必须用真机数字替换）：

| 阶段 | 业界预算 | 月伴现码 | 为何更贵 |
|---|---|---|---|
| 收句 / 端点 | 100–300ms | 500–1800ms | Web Speech `isFinal` 常切半句，不完整句再等 0.9–2.2s；`FORCE_COMMIT` 1.4s 当日常 |
| ASR 定稿 | 150–300ms | 叠在上一行 | 火山有流式 partial，但 **onFinal 才开想**；云端几乎没有可用 partial 策略 |
| 持久化 + 装配 | 应与 TTFT 重叠 | 100–400ms 挡在前面 | `message.append` 后再 `chat.start`；装配最多 24 条 |
| LLM 首 token | 150–700ms | 400–1500ms | 人设/桌面工具说明很长；`glm-4-air` 不是闪模型 |
| TTS 首包 | 100–200ms | 300–800ms | 火山 `unidirectional`、Edge、SoVITS 都是 **整段 WAV**；`streaming_mode=false` |
| 首句门闩 | 与生成重叠 | 再加 0–800ms | `takeSpeakableChunk` 无句号不读；停顿 800ms 才 force |
| 垫「嗯」 | 不该走云 | 一次云端合成 | 后端禁止模型说「嗯」，前端自己合成「嗯」再打断 |
| 回声门 | 必要 | 450–900ms | 播完还要等，下一轮更「一顿一顿」 |

典型体感：**说完 → 2.5–4s 才听到正经回答**（「嗯」不算）。差时 5s+。  
这不是「云不够」，是五段延迟**相加**而不是重叠。

三条卡的天花板不一样：

| 卡 | 听 | 说 | 现码对打 | 本文做完 | 卡自己的顶 |
|---|---|---|---|---|---|
| **火山** | seed-asr 流式 | seed-tts 整段 HTTP | 3.0 | **4.2** | 4.3（级联） |
| **云端** | WebView2 Web Speech | Edge Neural | 2.6 | **3.6–3.8** | 3.8（不换听写引擎） |
| **本地** | sherpa | GPT-SoVITS `:9880` 非流、CPU | 2.3 | **3.2** | 3.3（本机合成墙） |

用户说「三种模式都比较满，主要反应慢」——满的是**功能面**。慢的是火山/云端也在走同一套「等终稿 → 等句号 → 等整段 WAV」。

---

## 3. GitHub 对标（同类实时语音，不是再做一个桌面助手）

2026-07 开源实时语音收敛成三套。月伴是 **Go 引擎 + WebView2**，不该把运行时换成 Python。要偷的是**管线形状**，不是框架本身。

| 项目 | 仓库 | 抽象 | 该偷 | 不该做 |
|---|---|---|---|---|
| **Pipecat** | [pipecat-ai/pipecat](https://github.com/pipecat-ai/pipecat) | 帧管线（STT→LLM→TTS 可重叠） | Smart Turn v3 语义收句；打断清下游缓冲；TTS 从首句边界开声 | 把月伴重写成 Python |
| **LiveKit Agents** | [livekit/agents](https://github.com/livekit/agents) | WebRTC 会话 + worker | 音频 turn-detector；自适应插话（最短时长/最少字、假打断恢复）；历史只留用户听到的字 | 上 SFU / 改成房间产品 |
| **TEN Framework** | [TEN-framework/ten-framework](https://github.com/TEN-framework/ten-framework) | C++ 图 + 多语言扩展 | 热路径原生；中英文本「说完/没说完/等」 | 引入 Agora RTC 图运行时 |
| 语音原生 | OpenAI Realtime / Gemini Live / 豆包实时通话 | 音频进音频出 | 理解 4.6+ 从哪来 | **禁止**把 `chat.start` 挂到 openspeech；桌面工具会变瞎 |

业界共识（2026 对比文）：

1. 实时语音里，**基础设施比模型档次更决定体感**。1.4s 的前沿模型输给 600ms 的小模型。
2. 端点错一次，代价是「假收句 + 再等一轮」，比多等 200ms 惨得多。
3. Whisper 式 30s 窗不该当实时听写。火山 SAUC / sherpa 流式是对的；Web Speech 是云端卡的墙。
4. 插话必须取消**在飞的 LLM 和已排队 TTS**，不能只静音。月伴 0.4.46 已能强制 `cancelled`，还缺「历史只留已读出的字」。

---

## 4. 最佳接近方案（裁定）

**默认干活卡：火山。** 听 seed-asr，说 seed-tts 或晓晓（密钥未齐时灯要诚实），想用**闪模型**且继续 `DisableReasoning`。  
云端卡继续当免密钥兜底，不要对它承诺电话感。  
本地卡继续当离线/克隆，不要当「最快」。

结构目标（级联近双工，不是 S2S）：

```text
用户还在说 ──► 流式 partial 上字幕
                 │ 语义收句（不是固定 1.4s）
                 ▼
            投机 chat.start（终稿若变了再改/取消）
                 │ 首 token
                 ▼
            首句/首逗号立刻 synthesize（最好 PCM 流）
                 │ 与后续 token 重叠
                 ▼
            连续朗读；插话取消流 + 清队列 + 历史截到已读
```

冻结（本文不得违反）：

- 不重写 `useCompanionMachine` 整表
- 不在 `volc_speech` / openspeech 上挂对话 LLM
- 选火山失败不静默切 sherpa / Web Speech
- 不改 `E:\GPT-SoVITS\start-api-cpu.bat`、9880、盘符
- 不把晓晓标成本地克隆
- 不把用量环当停止键

---

## 5. 波次（按体感，一次只改一层）

### W0 — 埋点（无产品行为变化）

每轮记下：收句耗时、`message.append`、`chat.start` 返回、首 token、首句合成、首包 PCM、回声门。  
写进会话诊断或 `%LocalAppData%\Lunitide\logs\`。没有数字不准调 `FORCE_COMMIT`。

通过：同一本机打 20 轮闲聊，能画出 §2 那张表的真数。

### W1 — 收句：日常不再靠 1.4s（速度 + 准）

**改：**

- 完整句（`looksIncompleteUtterance===false` 且已稳定 220ms）立刻 `onFinal`，不要再等 `FORCE_COMMIT_MS`
- `FORCE_COMMIT` 只当卡死兜底，不当默认端点
- 火山/本地：用流式 partial + 已有不完整句规则；可选接入 **Pipecat Smart Turn 类 ONNX**（本机 CPU，<100ms）判「说完了」
- 云端：Web Speech 的 `isFinal` 继续当不可靠；不完整句继续 hold，但完整问候/问句不要再加 1.6s

**不要：** 为了快把「帮我打开」在没宾语时提交。那是准的倒退。

通过：完整短句说完 → 开想 <400ms；「帮我打开」仍等宾语。

### W2 — 首声：句号前门闩去掉一半（速度，最大头）

现码自相矛盾：人设要「第一句 8–20 字 + 句号」，`takeSpeakableChunk` 又必须看到句号才读。模型稍慢出句号，用户就再干等 800ms。

**改：**

- 第一段：出现 `。？！` **或** 首逗号且 ≥8 字 **或** 已 8–20 字且人设句型完整 → 立刻 `enqueue`
- 停顿 force 从 800ms 降到 **350ms**（有 W0 数字再收）
- 垫「嗯」：默认关；要开就播 **本机 80ms PCM**，禁止再走云端 `tts.synthesize`
- 人设继续禁止模型自己说「嗯」

通过：首 token 后 **400ms 内**有可读声（闲聊）。字幕仍可超前于声音。

### W3 — 想：热路径不要先写库（速度）

**改：**

- 月伴 `chat.start` 用当前句 + 已在内存的上下文先开流；`message.append` / `home()` **异步**，失败走已有 persist 横幅
- 装配继续跳过 compaction / handoff（已跳）；人设桌面长文拆成「闲聊短指令 / 工具轮才注入」
- 月伴默认模型建议闪模型（`glm-4-flashx` / 同档）；`glm-4-air` 可留工作台
- 继续：闲聊不挂工具；`DisableReasoning`

通过：从 `onFinal` 到请求出网 <150ms；闲聊请求不带 tool schema。

### W4 — 说：流式首包，不要整段 WAV（速度）

**改：**

- 火山：在单向 HTTP 上按官方分片读 PCM，到就 `scheduleBuffer`，不要等整句
- Edge：WS 本来就有块，播放器改成「首块即可响」
- SoVITS：保持 `streaming_mode=false` 诚实；本地卡不承诺 4 分速度
- 保持预取下两段；打断继续 `interrupt` + 0.4.46 强制 cancelled

通过：合成开始 → 喇叭出声 <200ms（火山/晓晓）。

### W5 — 准：语义收句 + 已读历史（准，且防假快）

**改：**

- 插话取消后，写入会话的助手内容截到 **已经读出的字**（`spokenUpToRef`），不要整段生成
- 火山热词 / `dialog_ctx` 保持；数字、人名、桌面文件名进热词
- 关键槽位（证件号、路径）继续不完整句 hold，不准为了分数误提交
- 云端卡文案写清：不能插话、听写弱，是能力不是故障

通过：假打断不丢后半句；切半句打开桌面的比例下降（W0 对照）。

### W6 — 云端卡诚实加强（可选）

若用户坚持「云端也要电话感」：听写可升级为云端流式 ASR（Azure / 同一火山 seed-asr），**灯改成「听 云端流式 / 说 晓晓」**，仍禁止插话或单独开插话开关。  
这是产品改卡，要书面批准。未批准则云端保持 3.8 顶，日常引导去火山卡。

---

## 6. 不要做的「更快」

| 诱惑 | 为何不做 |
|---|---|
| 对话 LLM 改走豆包/OpenAI Realtime | 冻结；工具/桌面变瞎 |
| 火山聋了切 Web Speech | 换产品，不是提速 |
| 把 `FORCE_COMMIT` 收到 400ms 当唯一端点 | 切半句，准分崩 |
| 重写状态机 / 上 Pipecat 运行时 | 成本高，现核已能重叠 |
| 改 SoVITS bat / 把克隆失败改晓晓还标本地 | 冻结 |
| 声称做完就是 4.80 / 正式发布 | 加权尺和发布门闩不是本文 |

---

## 7. 验收（装包后勾，单测绿不加分）

闲聊 20 轮、火山卡、本机已登录：

| ID | 动作 | 通过 |
|---|---|---|
| F1 | 「你好」 | 说完到第一声可读 ≤800ms；不是只听「嗯」 |
| F2 | 「今晚月色如何」 | 一句问、一句答，无连发、无闪灯 |
| F3 | 「帮我打开」停 0.5s 再接「网易云」 | 只开一轮，不先对「帮我打开」瞎执行 |
| F4 | 回答中插话（火山） | 喇叭立刻停，新一轮开想；历史无未读出的后半句 |
| F5 | 云端卡同一句 | 能走完；不强求 ≤800ms；不能插话 |

未勾前综合对打仍按 **2.9** 报。勾完 F1–F4 才能报 **4.1**。
