# 月伴语音对答整改合同（复核后）

日期：2026-09-01  
产品：月汐 / Lunitide `VERSION` **0.4.47**（级联 + 操作残差已装箱；官方目录未升本包前用旧包装勾不算）  
状态：**流畅度施工合同**。覆盖上一份分析稿 `2026-09-01-companion-voice-fluency.md` 的波次表。  
画板：`companion-voice-fluency-remediation.canvas.tsx`

冻结仍以 `2026-09-01-voice-pipeline-remediation.md` §0 / 主合同 §8 为准。本文只改「能走完一轮之后仍然慢、卡、易断」的层。

**不能报：** 语音已齐所以流畅、全云端该 200ms、投机开第二路 `chat.start`、上 Realtime 才算方案、主尺 4.80、正式发布、4.5 语音模块（§7 未勾）。

---

## 0. 复核结论

上一份方案方向对：**慢在串行级联，不在缺云。** 最佳接近仍是把级联做成可重叠管线，不换核。

复核后必须改掉的四处：

| # | 上一份写错 / 写漏 | 现码事实 | 本文怎么改 |
|---|---|---|---|
| A | 把垫「嗯」写成日常延迟 | `instantAck` **默认关**（rev&lt;11 强制关，曾因回声「嗯嗯嗯」关掉） | 保持默认关；只修「用户打开后禁止再走云端合成」 |
| B | 结构图写「投机 chat.start」 | 与 W-VOICE-1「同一句只一次流」冲突，会重开回合风暴 | **禁止投机开流**。重叠只做：收句、落库、首句 TTS |
| C | 暗示 FORCE_COMMIT 已只是兜底 | `speech.ts` 的 `forceCommit` 会拒不完整句，但 `CompanionStage` 1.4s 后 **失败仍 `beginUserTurn(caption)`**（约 1744 行） | **P0 残差**：失败不得开想 |
| D | 只写闲聊账，没写工具轮 | 工具轮会 `AWAIT_MORE`、打断垫话、等 `user.ask`、SoVITS 5s×24；用户体感是「卡死」不是「慢 300ms」 | 单列 W-STUCK |

打分（对打体感，不是功能齐）**维持**：现码 **2.9**，做完 **4.1**，不换核顶 **4.3**。  
上一份的 2.5–4s 闲聊估算仍是估算，W0 未跑之前不准改数。  
语音合同里的真机 **2.4** 是「三卡走不完一轮」的旧句；0.4.46 源码已能走完，对打改报 **2.9**。未装 0.4.46 仍按旧包报。

---

## 1. 对标（对齐到可抄的形状，不抄运行时）

| 对标 | 对齐到月伴的哪一层 | 落地动作 | 明确不对齐 |
|---|---|---|---|
| Pipecat Smart Turn | 收句 | W1 用现有 `looksIncompleteUtterance`；不装 ONNX | 不上 Python 帧管线 |
| Pipecat 首句边界 TTS | 说 | W2 `takeSpeakableChunk` 放宽第一段 | 不在句号前读半个数字槽 |
| LiveKit 假打断恢复 | 插话 | W5 历史截到已读；最短 2 字才算插话（现码已有 echo/barge 门） | 不上 SFU |
| LiveKit「只留用户听到的」 | 落盘 | 取消时 persist 用 `spokenUpToRef` | 不在流还在写时改已落盘全文 |
| TEN 说完/没说完/等 | 收句文案 | 不完整句继续 hold；硬顶 2.2s 只当识别器死 | 不上 7B 文本分类器 |
| Realtime / 豆包实时 | 理解天花板 | 用来解释为何 4.6+ 做不到 | **禁止** LLM 走 openspeech |

业界数字只作对照：人类 ~200ms，调好级联 500–800ms。月伴闲聊目标 **F1 ≤800ms 第一声正经回答**。

---

## 2. 风险登记（中断 / 卡壳 / 倒退）

做快之前必须先看这些。带 **P0** 的本波要堵；其余写进对应波的「不要」。

### 2.1 会重开风暴（中断对打）

| ID | 风险 | 现码 | 整改 |
|---|---|---|---|
| R-STORM-1 | FORCE_COMMIT 失败仍 `beginUserTurn` | `CompanionStage.tsx` ~1735–1748 | **F-1**：`!committed` 只留字幕，禁止 `onSend` |
| R-STORM-2 | 投机 partial 开 `chat.start` | 上一份方案 | **禁止**。进行中同一句只入队 |
| R-STORM-3 | 工具轮 `speakCloseout` 再 `speak` 整段 | 流已读过又复读 | W2 关闭时 `spokenUpTo` 对账，禁止复读已读 |
| R-STORM-4 | 退出后 `pendingSend` 再冲 | 0.4.46 已在打断时清 | 保持；W0 测退出 10s 无自动 `chat.start` |

### 2.2 会卡在「回应中 / 说话中 / 启动中」

| ID | 风险 | 现码 | 整改 |
|---|---|---|---|
| R-STUCK-1 | 无首 token：`streaming` 干等 **12s** 才 `COMPANION_REPLY_STALL` | 故意比 DeepSeek TTFT 长 | W0 量 TTFT；闪模型后可收到 8s，**不得**收到 3s |
| R-STUCK-2 | 工具执行中 stall 定时器停掉 | `companionToolsExecuting` 则 return | 对：工具轮不要 12s 自杀。W-STUCK 要有「执行中」口播 + 超时文案 |
| R-STUCK-3 | `user.ask`（UAC 等）挂起 | `userAsk` 时 stall 不计 | 保持。灯/字幕必须说「要你点」，不能像死机 |
| R-STUCK-4 | SoVITS 段失败重试 **5s × 24 ≈ 2 分钟** 无声 | `ttsPlayer.ts` | W-STUCK：月伴轮最多 2 次/10s，然后诚实失败，不假启动中 |
| R-STUCK-5 | 火山 TTS `ReadAll` 整包 NDJSON，45s 超时 | `internal/tts/volc.go` | W4 才能流式；W4 前超时要可取消（打断已 cancel 流，合成要认 generation） |
| R-STUCK-6 | `AWAIT_MORE`：说话中播完但流未结束 → 回 thinking | 为了麦能听下一句 | 保持。禁止再加「thinking 时 commitPaused 导致永远听不到」而不 pulse |
| R-STUCK-7 | `chatReady===false` 只入队 | 供应商未齐 | 入口已拦；保持。禁止空听 |
| R-STUCK-8 | 火山聋 2 次重启后停在 VOICE-004 | 合同正确 | 不本波改切 sherpa。设置灯保持火山 |
| R-STUCK-9 | 电路熔断 3 段失败后无声 | `circuit-broken` | 要开口「朗读断了，看字幕」 |
| R-STUCK-10 | 免手环监听重启 0.4s→8s | `handsFreeRetryDelayMs` | 有字时不要当聋重启（现码 pulse 已偏此） |

### 2.3 会切坏准度（假快）

| ID | 风险 | 整改 |
|---|---|---|
| R-CUT-1 | 第一逗号就读，「好呀，我把身份证写成」切在号码前 | 第一段放宽 **不含** 证件/电话/路径槽（沿用 `INCOMPLETE_FIELD_WAIT`） |
| R-CUT-2 | 不完整句硬顶 2.2s 提交「打开网」 | 保持 hard 顶；W1 不把 hard 收到 &lt;1.6s |
| R-CUT-3 | 异步 `message.append` 后装配读不到本句 | `chat.start` 已带 `messages:[{user, prompt}]`，装配失败走 explicit。异步可以，**不得删 messages** |
| R-CUT-4 | 插话后落盘全文，用户只听到半句 | W5：取消时助手内容截到 `spokenUpToRef` |
| R-CUT-5 | 回声门 450–900ms 吞下一句 | 不为本波缩到 &lt;300ms（曾把她的话当用户） |

### 2.4 产品冻结冲突

| 诱惑 | 裁定 |
|---|---|
| 对话走 Realtime / 豆包全双工 | 拒绝 |
| 火山聋切 Web Speech | 拒绝 |
| 重写 `useCompanionMachine` | 拒绝。`AWAIT_MORE` 已够用 |
| 改 SoVITS bat / 9880 | 拒绝 |
| 云端也要插话 | 能力做不到。W6 才谈换听写引擎 |
| 装 Pipecat Smart Turn ONNX | **本波不做**（体积/签名/CPU）。W1 只用现规则 |

---

## 3. 目标结构（纠正后）

```text
用户还在说 ──► partial 上字幕（已有）
                 │ 完整句稳定 220ms → onFinal
                 │ 不完整句 hold；1.4s 舞台定时器不得绕过 forceCommit
                 ▼
            一次 chat.start（先出网；append 可异步，messages 必带本句）
                 │ DisableReasoning；闲聊不挂工具
                 ▼
            首段可读（句号 / 安全逗号 / 350ms 停顿）→ enqueue
                 │ 火山/Edge：W4 才改首块 PCM；此前仍整段 WAV
                 ▼
            连续读；打断 = 取消流 + 清队列 + 已读落盘
工具轮：口播「在做」→ 等工具（stall 不计时）→ 收尾一句；UAC 用人点
```

---

## 4. 详细整改计划（一波不过不宣称下一波已齐）

```text
F-1   堵 FORCE_COMMIT 绕过          P0  半天
  │
  ├─► W0   埋点（无行为）           P0  与 F-1 可同一天
  │
  ▼
W1   收句：完整句快、半句仍 hold    P0  0.5–1 日
  │
  ▼
W2   首声：句号门闩 + 复读对账      P0  1 日     ← 闲聊体感最大头
  │
  ├─► W3   想：异步落库 + 短人设    P1  0.5–1 日
  │
  └─► W-STUCK  工具/合成/熔断卡壳   P0  0.5 日   } W2 后可并行
              │
              ▼
         W4   流式首包（火山/Edge） P1  1–1.5 日  要新 bridge 事件
              │
              ▼
         W5   准：已读历史 + 热词   P1  0.5 日
              │
              ▼
         W-PACK  打包装包真机勾     P0
              │
              ▼
         W6   云端换流式听写        可选，书面批准
```

### 4.1 F-1 — FORCE_COMMIT 不得绕过（P0）

**为什么：** W-VOICE-1 合同写「forceCommit 失败不再 beginUserTurn」。现码失败仍开想。interim「帮我」停 1.4s 会误开一轮。

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| F-1-1 | `CompanionStage.tsx` 1735–1748 | `forceCommit===false` 时 **return**，禁止 `beginUserTurn` | 单测：不完整 interim 1.4s 后 `onSend` 次数=0 | 加长 1.4s 当修复 |
| F-1-2 | `CompanionStage.*.test.tsx` | 补「kick 失败不发送」 | CI 绿 | 用 e2e 代单测 |

通过：F3 的「帮我打开」停 0.5s 不再单独成轮（连同 W1）。

### 4.2 W0 — 埋点（无产品行为）

每轮写一条（本地日志即可，不打公网）：

`path, endpointMs, appendMs, startReturnMs, ttfbMs, firstSynthMs, firstAudioMs, echoMs, toolsMs, stall|ok`

| 步骤 | 文件 | 做 | 完成定义 |
|---|---|---|---|
| W0-1 | `companionText.ts` 或新 `voiceTiming.ts` | `performance.now()` 打点，会话结束 flush | 20 轮闲聊能画出表 |
| W0-2 | `CompanionStage` / `ttsPlayer` / `sendAndChat` | 在现有事件上挂钩子 | 不改收句/合成阈值 |

没有 20 轮数字，**禁止**改 `FORCE_COMMIT_MS` / stall 12s / 回声门。

### 4.3 W1 — 收句

对标：Pipecat「完整了就提交」，不是「一律 1.4s」。

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| W1-1 | `speech.ts` `volcSpeech.ts` `localSpeech.ts` | 完整句（`looksIncomplete===false`）稳定 220ms + 静音 280ms → `onFinal` | 「你好」开想 &lt;400ms | 完整句再等舞台 1.4s |
| W1-2 | 同上 | 不完整句继续 900/1600/2200 | 「打开网」0.5s 内不提交 | hard 顶收到 &lt;1600 |
| W1-3 | 舞台定时器 | 只调用 `forceCommit`；与 F-1 合一 | 无第二条路径 | 再写一套端点 |
| W1-4 | 单测 | 补完整问候 / 半句打开 | `speech.test` + volc/local 各 1 | 上 Smart Turn 二进制 |

### 4.4 W2 — 首声（闲聊最大头）

对标：Pipecat「第一句边界就合成」。

现码矛盾：人设要 8–20 字 + 句号；`takeSpeakableChunk` 无句号不读，再 800ms force。

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| W2-1 | `companionText.ts` | 第一段：`。？！` **或**（逗号且 ≥8 字且 **非** 字段等待）**或** force 且 ≥8 字 | 单测覆盖「好呀，今晚月色不错。」可读；「号码，」不可读 | 证件号中间出声 |
| W2-2 | `CompanionStage.tsx` | 停顿 force **350ms**（W0 有数再收） | 首 token 后 400ms 有 enqueue | 没 W0 就改 12s stall |
| W2-3 | `companionSettings.ts` | 默认 `instantAck=false` 保持 | 打开时走本机短 PCM，不走 `tts.synthesize` | 默认再打开嗯 |
| W2-4 | `CompanionStage` 收尾 | `spokenUpTo` 已覆盖的禁止 `speak()` 再读一遍 | headstart 测不复读 | 工具收尾复读整段 |
| W2-5 | 人设 | 保持「第一句带句号、禁止先嗯」 | 现测仍绿 | 为快删人设 |

### 4.5 W3 — 想（出网提前）

现码：`companionMode` **先 `await append` 再 `chat.start`**（`SessionPage.tsx` `sendAndChat`）。`messages` 已带本句。

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| W3-1 | `SessionPage.tsx` | 月伴：`chat.start` 与 append **并行**；start 失败仍保留 persist 语义 | `onFinal`→出网 &lt;150ms；append 失败走已有横幅 | 删 `messages` |
| W3-2 | `chat.go` `companionPersonaInstruction` | 闲聊短指令（身份+开口规则）；桌面长文仅 `companionWantsToolsForTurn` 时注入 | 闲聊请求无 desktop.open 说明书 | 闲聊挂全量 tools |
| W3-3 | 设置/默认模型 | 月伴优先用**当前供应商里已有的最快文本模型**（闪/air 由用户已配决定） | 文案写「月伴建议闪模型」 | 写死不存在的 `glm-4-flashx` |
| W3-4 | 保持 | `DisableReasoning`；闲聊不挂技能目录 | 现测仍绿 | 打开推理「更准」 |

### 4.6 W-STUCK — 卡壳专波（和 W3 并行）

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| S-1 | `ttsPlayer.ts` | 月伴轮 SoVITS starting 重试 ≤2 次或 ≤10s | 超时口播「本地朗读未就绪」 | 改 bat |
| S-2 | `CompanionStage` | `circuit-broken` / `engine-unavailable` 必须开口或字幕「朗读断了」 | 不静默回 idle | 假装在说 |
| S-3 | 工具轮 | 已有 `companionExecutingSpeech`；补：超过 20s 仍 `…中` 则重复一句进度，不触发 12s stall | 桌面任务不像死机 | 工具中取消模型 |
| S-4 | `userAsk` | 字幕固定「要你点，我不能代点是」 | 与 D12 合同一致 | 自动点 UAC |
| S-5 | 火山合成 | `interrupt` 必须让 in-flight `ReadAll` 丢结果（generation 已有） | 打断后 300ms 内无残声 | 新开第二条合成当修复 |

### 4.7 W4 — 流式首包（P1，要新契约）

现码：`volc.go` `io.ReadAll` 再 `parseVolcNDJSON`；Edge WS 也是凑齐再交播放器；SoVITS `streaming_mode=false`。

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| W4-1 | `internal/tts` + bridge schema | 新事件 `tts.chunk`（PCM/MP3 片）或播放器可读流 | 契约生成 + `verify:bridge --check` | 当施工步骤跑 `generate:bridge` 却不提交 |
| W4-2 | `volc.go` | 按行解析 NDJSON，`data` 到就 emit，不要等 `20000000` | 首片 &lt;200ms（本机联网） | 改 Resource-Id |
| W4-3 | `edge_ws.go` + `ttsPlayer` | 首块 `decodeAudioData` 即可 `scheduleBuffer` | 晓晓同样 &lt;200ms | 改 SoVITS 为 streaming 还不测 CPU 卡 |
| W4-4 | 本地卡 | 保持非流；灯诚实 | 不承诺 4 分速度 | 失败标克隆成功 |

W4 未合时，F1 仍可靠 W2（更早 enqueue）接近 800ms，不要假装已流式。

### 4.8 W5 — 准

| 步骤 | 文件 | 做 | 完成定义 | 不要 |
|---|---|---|---|---|
| W5-1 | 取消/打断 persist | 助手落盘截到已读出 | 插话后历史无未读半句 | 改用户句 |
| W5-2 | 火山 `dialog_ctx` | 保持；可加桌面文件名/常用人名词 | 不发明文换行热词 | 非法 context 硬塞 |
| W5-3 | 云端设置文案 | 「听=系统识别，说=晓晓，不能插话」 | 设置页可见 | 静默换火山听写 |

### 4.9 W-PACK — 发布与真机

| 步骤 | 做 | 不要 |
|---|---|---|
| P-1 | `VERSION` 新号（建议 0.4.47）；Quality 等价门禁 | 用 0.4.46 勾本波 |
| P-2 | 签名包 + `Verify-Release`；`release/out` 只留本号 | next 旁路盖官方目录 |
| P-3 | 本机 Setup 升级后再勾 §5 | `Test-Install.ps1`（本机已有安装） |
| P-4 | 未勾 F1–F4 综合对打仍报 **2.9** | 报 4.1 / 4.5 / 4.80 |

### 4.10 W6 — 可选改卡

云端听写改流式 ASR（Azure 或火山 seed-asr）须**书面批准**，灯改「听 云端流式 / 说 晓晓」。未批准则云端顶 3.8，日常引导火山卡。

---

## 5. 验收（装新包后勾，单测绿不加分）

环境：已登录、官方目录已是本波版本、火山密钥按 W0 配。闲聊 20 轮 + 下列定点。

| ID | 波 | 动作 | 通过 | 失败即停 |
|---|---|---|---|---|
| F0 | W0 | 打 20 轮「你好 / 今晚月色如何」 | 日志有 endpoint/ttfb/firstAudio | 凭感觉改定时器 |
| F-SAFE | F-1 | interim「帮我」停 1.4s | 不开 `chat.start` | 又开一轮「帮我」 |
| F1 | W2 | 「你好」 | 说完→第一声**正经回答** ≤800ms | 只听嗯、或 &gt;1.5s 无声 |
| F2 | W1+W2 | 「今晚月色如何」 | 一轮问一轮答；无闪灯连发 | 两条用户句 |
| F3 | W1 | 「帮我打开」+0.5s+「网易云」 | 一轮；不先瞎执行 | 先对「帮我打开」调桌面 |
| F4 | W5 | 火山卡回答中插话 | 300ms 内停声；历史无未读后半句 | 残声 / 整段落盘 |
| F5 | — | 云端同一句 | 能走完；不强求 800ms；不能插话 | 静默改火山 |
| F6 | W-STUCK | 本地卡坏 venv | ≤10s 诚实失败，不 2 分钟无声 | 假启动中 |
| F7 | W-STUCK | 关电脑控制后「打开文档」 | 横幅「不会自己打开」；不卡死 | 说完成了 |
| F8 | W-STUCK | 真 UAC | `user.ask` / 「不能代点是」 | 代点 |
| V-TURN | 对齐旧合同 | 退回打字 10s | 无自动重发 | 连发 |

对齐旧语音 §7：V-VOLC-ASR / V-LOC-HOST / V-CLOUD 仍要勾。本波不替代它们。未装 0.4.46+ 的人先升包。

---

## 6. 报分（只准用这些句子）

- 对打体感 **现在 2.9 / 5**（功能齐约 4.2；速度 2.7）。
- F-1 + W1 + W2 + W-STUCK + F1–F4 勾完：**4.1 / 5**。
- W4 流式首包再加约 0.1，仍 **不超过 4.3**（级联顶）。
- 不上 Realtime：**不报 4.6+**。
- 主尺门闩 4.8 **不**因本波升降；加权不是 4.80。
- 语音模块 4.5 仍要旧 §7 三卡勾齐；本波只抬对打速度尺。

---

## 7. 工时（一人顺序）

| 波 | 预估 | 依赖 |
|---|---|---|
| F-1 | 0.3 日 | 无 |
| W0 | 0.3 日 | 无 |
| W1 | 0.5–1 日 | F-1 |
| W2 | 1 日 | W1 |
| W3 + W-STUCK | 1 日 | W2（可并行） |
| W4 | 1–1.5 日 | 契约；可晚于真机 F1 |
| W5 | 0.5 日 | W2 |
| W-PACK | 0.5 日 | 至少 F-1+W1+W2+W-STUCK |

合计到可勾 F1–F4：**约 3.5–4.5 日**。W4/W6 另计。
