# 对照整改（同事 / 火山语音 / 会议浅色 / 月伴 / 本地克隆试听）

日期：2026-08-31 夜（复核终稿，补图6）  
产品：月汐 / Lunitide `VERSION` **0.4.43**  
状态：**落地对照规格**。和现码冲突时以现码为准；和本波施工冲突时以本文为准。  
画板摘要：`voice-people-meeting-fix-plan.canvas.tsx`（和本文冲突时以本文为准）。

施工合同（K/G/D/I、主尺 4.8）：`docs/superpowers/specs/2026-08-31-engine-gateway-48.md`  
四家对标：`docs/superpowers/specs/2026-08-31-peer-alignment-and-remediation.md`

本文只回答：对着 0.4.43 真机图，下一刀改什么、不改什么、怎么验收。  
**本波不能报主尺 4.8。** 4.8 仍要签名桌面 e2e（文字写偏好 → 月伴续 → 同事 @专家「继续刚才的」）。

---

## 0. 文档权威

| 文档 | 职责 | 冲突时 |
|---|---|---|
| **本文** | 图1–6 根因、P0/P1/P2、文件清单、验收、冻结 | 覆盖当晚画板第一版 |
| `2026-08-31-engine-gateway-48.md` | 引擎常驻 + 桌面点击 + 国内 IM | 本波不扩 K/G/D/I |
| `2026-08-31-peer-alignment-and-remediation.md` | 四家对标与主尺 | 本波不改加权 |
| 现码 + 单测 | 事实 | 覆盖口头分 |

作废（不要按下面施工）：

- 画板第一版「点自己 → `setRail('me')`」（会拆掉通讯录树，用户还在通讯录里）
- 「选火山必须强开语音插话」
- 「火山听写失败就改用本机 sherpa」当作产品默认
- 「把 LLM 写进火山供应商类型下拉」
- 「本波完成后报 4.8」

---

## 1. 冻结（本波禁止碰）

和 `engine-gateway-48` / 对话回合契约同一条线：

| 禁止 | 原因 |
|---|---|
| 合并 `messages` / `people_messages` | 两库一契约 |
| 把 PeoplePage 嵌进 SessionPage | 同事不是会话页的一种 mode |
| 改 `PERSONAL_CHAT_PROJECT` 或 cmd/engine KB/Plugin/Handoff 的 `local-user` | 个人会话身份 |
| 把 `@` 菜单扩到全部已启用专家 | 只挂已装上的 |
| 把 `chat.go` 重写成新总线 | 回合契约已冻 |
| 整表统一 Go/TS mention 解析器 | 各面各用各的，只修分叉症状 |
| 开放「和自己开会话」/ note-to-self 线程 | 「我」是资料，不是对话对象 |
| 给 ASR / TTS 各开一个供应商顶栏页 | 密钥配两次 |
| 在 `volc_speech` / openspeech 上挂对话 LLM | 想走 `chat.prefer` |
| 重写 `useCompanionMachine` 状态表 | 只修非法事件映射 |
| 本波承诺豆包角色库「温柔桃子」 | 和 seed-tts HTTP 不是同一份目录 |
| 重写 GPT-SoVITS / 换端口 / 换音色包路径 | 本机 `E:\GPT-SoVITS` + `api_v2:9880` 已冻；只修启动中诚实 |

---

## 2. 复核补遗（第一版报告漏了或写错的）

这些是对照现码再走一遍之后必须写进合同的，否则落地会偏：

1. **图1 不要切 `rail=me`。** `openPeer(self)` 已经 `setCard` + return。通讯录树在 `rail === 'contacts'`。改成 `setRail('me')` 会换成「我」摘要栏，通讯录消失。正确：留在通讯录，右侧改画 ContactCard。
2. **1.5s poll 只在 `rail === 'me'` 停刷线程。** 通讯录看自己时 thread 仍后台刷新。这是对的：切回「聊天」还能看到上一通。不要为了藏标题去停 poll。
3. **浅色不只 `aside`。** `.launch-content main` 同一条暗玻璃。会议右栏是 `section.meeting-main`，所以看起来白；左栏是 `aside.meeting-list`，所以黑。浅色覆盖应打在 `.launch-content aside/main`，会议壳再加一层兜底。同事页已有 `html[data-theme="light"] .people-mid`。
4. **侧栏「同事 · Excel专家」不是图1。** 那是会话历史标题，出现在会议/设置也正常。本波不改 LaunchSidebar。
5. **火山失败有三条路，不是只有 onError。** `companionListenFailover('volc','volc',true) → 'local'`（聋识别循环 ~1629）；`fallbackVolcToWorkingListen` 握手失败开 sherpa；`onError` 中途再切一次。三条都要封。
6. **`applyVoicePath('local')` 同样强开 `voiceBargeIn`。** 火山卡和本地卡都要改成保留用户开关。
7. **已有测试在保护错误行为。** `asrPath.test.ts` 期望 volc→local；`companionSettings.test.ts` / `CompanionSection.test.tsx` / `prepareCompanionEntry.test.ts` 期望选火山/本地就开插话。改产品必须改这些测试，不能留绿当旧契约。
8. **P1 拆 `asr | tts` 会碰到 `pickDefaultVoice`。** 会议、唤醒、听灯、月伴开麦都用它当 **ASR**。旧 `voice` 必须读成 `asr`，`pickDefaultVoice` 只挑听写，否则会议麦会捡到朗读模型。
9. **图6 不是「本地模型坏了」。** 文案已是「语音引擎启动中」，试听却报 M95-002。`EnsureRunning` 超时返回 `ErrRefEngineStarting`，`refaudio.go` 用 `%w` 包成 `ErrSynthesisFailed`，桥上只剩「该段语音合成失败」。音色卡可点是因为 `tts.voices(engine=ref)` 永远返回 50 个预设，`engineState` 被当成 available。

---

## 3. 图对照

### 图1 · 通讯录点自己，顶栏仍是专家

**现象：** 通讯录选中 `mr.Mu（我）`，右侧 header / composer 仍是上一通「Excel表格制作专家」。

**现码：**

- `PeoplePage.openPeer`：`setCard(peer)` 后 `if (peer.self) return`，不 `threadOpen`。
- `showThread = rail !== 'me' && thread`。 leftover thread 继续 `threadHeading(thread)`。
- ContactCard 只在 `!showThread` 时出现。
- 现测 `'clicking self only opens the card'` 没有先打开专家会话，所以绿。

**改成：**

```
showThread = rail !== 'me' && thread && !card?.self
```

- 点自己：留在通讯录，右侧 ContactCard，`h2` = `displayName(person, true)`（如 `mu（我）`）。
- 禁止发消息按钮（ContactCard 已有 `!person.self`）。
- thread 留在 state，聊天轨还能打开上一通 DM。
- 不 `setRail('me')`。

**验收：** 先开 Excel/PPT 会话，再点自己 → heading 是自己，专家标题和 composer 消失；通讯录轨仍 `aria-current`。

### 图2–3 · 火山整条「听 · 想 · 说」

**现象：** 选火山卡（文案「火山听 · 晓晓读」）。图3 类型只有「语音模型」。用户要一张火山供应商同时挂听写和朗读；「想」继续是已配的云端 LLM（图5 `glm-4.3` 是对的）。

**现码：**

- `ModelKind = llm | vision | image | video | voice`；`volc_speech` + `kind: voice` = seed-asr。
- `applyVoicePath(..., 'volc')` 强制 `engine: 'edge'`（晓晓）且（旧码）`voiceBargeIn: true`。
- VoicePathPicker 诚实写了：火山只要 ASR，朗读仍是晓晓。

**P1（单独一波，不和 P0 绑一次提交）：**

| 做 | 不做 |
|---|---|
| 顶栏仍是「语音模型」一页 | 不加 ASR/TTS 两个顶栏页 |
| 编辑器类型 = 听写 ASR / 朗读 TTS | 类型里加 LLM |
| `kind` 扩 `asr \| tts`；旧 `voice` → `asr` | 把 LLM 写进 openspeech |
| 一个火山供应商两行模型 | 新开 daemon |
| `tts.synthesize` 加 `volc` 引擎，复用已有密钥 | 本波点名「温柔桃子」 |
| 卡面：配了 TTS 才写「火山听 · 火山读」；否则「火山听 · 晓晓读（未配朗读）」 | 假装整条都是火山 |
| 更新全部 `pickDefaultVoice` 调用方只挑 ASR：`MeetingPage`、`CompanionStage`、`wakeWord.ts`、`companionLights.ts`、`meetingAsr.ts` | 让会议麦捡到 TTS 模型 |

**想：** 继续 `chat.prefer` / 首页模型。图5 想灯 `glm-4.3` 保持。

### 图4 · 浅色会议左栏发黑

**现象：** 中间历史栏暗，侧栏和右栏浅。

**现码：**

```css
.launch-content aside, .launch-content main { background: rgba(9,16,28,.70); }
```

`MeetingPage` 左栏是 `<aside class="meeting-list">`。`.meeting-list { background: var(--bg2) }` 选择器负于 `.launch-content aside`。同事中栏后来补了浅色覆盖，会议没有。

**改成：**

```css
html[data-theme="light"] .launch-content aside,
html[data-theme="light"] .launch-content main { background: var(--bg2); backdrop-filter: none; }
html[data-theme="light"] .meeting-shell,
html[data-theme="light"] .meeting-list,
html[data-theme="light"] .meeting-main { background: var(--bg2); }
```

设置页已有自己的浅色覆盖，不要拆掉。不重做会议信息架构。

### 图5 · 第一句能说，其后卡在「对答中」

**现象：** 听灯「本机 sherpa」、说「晓晓」、想「glm-4.3」；状态停在对答中 / 正在回你…。图2 选的是火山。打断会跳。

**现码叠层：**

1. 火山失败 + sherpa 就绪 → 静默切本地（`CompanionStage` onError / `fallbackVolcToWorkingListen`）。聋识别循环用 `companionListenFailover('volc','volc',true) → 'local'`。
2. `applyVoicePath('volc'|'local')` 强开 `voiceBargeIn`，和 UI「语音插话」打架。
3. 图2 全双工 + 先应一声：垫音「嗯」可被听成下一句。默认必须保持关。
4. `speaking × REPLY_TERMINAL` 不在状态表里 → `COMPANION_STATE_INVALID`，机位卡住；`companionSurfaceState` 在 `assistantAloud` 时强显说话，看起来乱跳。
5. 打断 → idle → 免提 `MIC_ACTIVATE` + 迟到流事件。

**P0 改法（不改状态表）：**

| 点 | 改法 |
|---|---|
| 通道诚实 | 显式火山：灯必须是火山 seed-asr。失败 VOICE-004，**不**切 sherpa / 系统识别。三条路一起封。 |
| 插话开关 | `applyVoicePath` 保留 `voiceBargeIn`；load 时缺字段走产品默认 `false`，不再因 volc/local 隐式 true。 |
| 先应一声 | 默认关；本波不改默认。 |
| 非法事件 | `speaking` 收到 `REPLY_TERMINAL` 映射成 `PLAYBACK_ENDED` → idle。函数抽出可测，不改 transitionTable。 |
| 打断 | 已有 INTERRUPT → idle；停 TTS、取消流。排队句保留，不重放上一句。不重写打断。 |

`beginUserTurn` 已清 `handledReplyRef`。本波不另造复位总线。

**验收（签名桌面，真人模型，P0）：** 火山听 + 现有 LLM + 晓晓读，连续 8 轮免提每轮出声；Tab 打断后下一句仍能说。P1 接上火山 TTS 后再改说灯文案。

### 图6 · 本地卡「启动中」点试听报合成失败

**现象：** 语音通道选本地。50 种人生卡可点。地址 `http://127.0.0.1:9880`。状态「语音引擎启动中…（首次加载模型约 30-90 秒…）」。点试听 → `试听失败：该段语音合成失败`。

**现码（对照，不是猜）：**

1. `tts.voices(engine=ref)` 无条件返回内置 `RefVoices()`。设置页 `engineState` 只看 `voices.length` 和 `pack_exists`，**不看** `server_online` / `host_state`。试听按钮因此在启动中可点。
2. `hostLaunching = ref_meta.host_state === 'launching'` 只改说明文案，不锁按钮。
3. `refaudio.Synthesize` 连不上默认 `9880` 时 `EnsureRunning(..., 25s)`。冷启动 30–90s，25s 必超时，返回 `ErrRefEngineStarting`。
4. 下一行写成 `fmt.Errorf("%w: %v", ErrSynthesisFailed, hostErr)`。`errors.Is(..., ErrRefEngineStarting)` 为假。`ttsFailure` 走 M95-002「该段语音合成失败」，`retryable=false`。
5. 设置页 `catch` 一律 `试听失败：${e.message}`。月伴 `ttsPlayer` 只对 **M95-001 + 启动中** 重试，收到 002 就当段失败——本地卡第一句也会哑。
6. 设置页合成超时 40s；引擎这边先空等 25s 再失败，用户看到的是「合成失败」不是「还在加载」。

**不是：**

- 地址填错（`127.0.0.1:9880` 就是 `DefaultRefEndpoint`）
- 要换 GPT-SoVITS / 换 9874 WebUI
- 50 张卡本身坏了（目录在，服务还没起来）

**P0 改法：**

| 点 | 改法 |
|---|---|
| 错误家族 | `EnsureRunning` 仍在启动 → **原样**返回 `ErrRefEngineStarting`，禁止再包成 `ErrSynthesisFailed` |
| 试听门闩 | `host_state===launching` 或 `server_online===false` 时禁用试听；文案保持「启动中」 |
| 漏点兜底 | 若仍打到 synthesize：设置页把 M95-001+启动中写成「请稍候再试听」，不要「试听失败：该段…」 |
| 默认地址 | 空 / `http://127.0.0.1:9880` / 带尾斜杠 都当默认端点，才能走自动拉起 |
| 真失败 | 引擎已 `offline`、脚本没有、HTTP 非 WAV：仍报 M95-002，带 `host_last_err` / HTTP 正文摘要 |
| 启动中不空等 | 已在 launching 时立即回 `ErrRefEngineStarting`，首次拉起最多等 8s；设置页试听超时 120s 对齐引擎 |
| 说灯 | 本地卡 launching →「GPT-SoVITS 启动中」warn，不把启动中画成死灯 |

**不改：** `E:\GPT-SoVITS\start-api-cpu.bat`、音色包盘符、api_v2 协议、把试听改成晓晓偷偷顶上。

---

## 4. 波次与文件

### P0（本波立刻落地，不扩契约）

| ID | 文件 | 改动 |
|---|---|---|
| P0-1 | `web/src/people/peopleRoster.ts` | `peopleShowsOpenThread` |
| P0-1 | `web/src/people/PeoplePage.tsx` | `showThread` 用上面的函数 |
| P0-1 | `web/src/people/PeoplePage.test.tsx` | 先开专家会话再点自己 |
| P0-1 | `web/src/people/peopleRoster.test.ts` | 自己名片不露 leftover thread |
| P0-2 | `web/src/styles.css` | 浅色 `launch-content aside/main` + meeting 兜底 |
| P0-3 | `web/src/session/companion/asrPath.ts` | 显式火山 failover 停在 volc |
| P0-3 | `web/src/session/companion/asrPath.test.ts` | 改期望 |
| P0-3 | `web/src/session/companion/CompanionStage.tsx` | onError / fallbackVolc 不切 sherpa；`REPLY_TERMINAL` 映射 |
| P0-3 | `web/src/session/companion/useCompanionMachine.ts` | `companionEventForDispatch`：speaking×REPLY_TERMINAL→PLAYBACK_ENDED；idle/listening 迟到事件吞掉，不改状态表 |
| P0-3 | `web/src/session/companion/CompanionStage.tsx` | `applyEvent` 把 `null` 当成功；火山聋识别两次后 VOICE-004；`onSend===false` 退回 idle；上一轮字幕不当成本轮首 token |
| P0-3 | `web/src/session/companion/asrPath.ts` | `companionVolcDeafGiveUp` |
| P0-3 | `web/src/session/companion/companionText.ts` | `companionHasFreshAssistantText` |
| P0-3 | `web/src/session/companion/voicePersonas.ts` | 卡面「火山听 · 晓晓读（未配朗读）」——仓库里还没有火山 TTS，诚实写 |
| P0-3 | `web/src/session/companion/companionSettings.ts` | `applyVoicePath` / load 不强开插话 |
| P0-3 | 上表相关 `*.test.ts(x)` | 改成新契约 |
| P0-4 | `internal/tts/refaudio.go` | 启动中错误不再包成 `ErrSynthesisFailed` |
| P0-4 | `internal/tts/refcatalog.go` | `CanonicalRefEndpoint` / 默认端点判定 |
| P0-4 | `internal/app/m9_tts_handlers_test.go` | `ErrRefEngineStarting` → M95-001 可重试 |
| P0-4 | `web/src/settings/refEnginePreview.ts` | 试听门闩 + 启动中文案 |
| P0-4 | `web/src/settings/SettingsPage.tsx` | 启动中禁用试听；catch 走诚实文案 |
| P0-4 | `CompanionSection.test.tsx` + `refEnginePreview.test.ts` | 启动中按钮不可点 |

P0 **不**跑 `generate:bridge`，不改 Go schema。不重装 GPT-SoVITS。

### P1（火山听+读，单独提交）

- `api` / `internal` 模型 kind、`generate:bridge`
- `web/src/provider/modelKind.ts`：`asr | tts`，`voice` 兼容为 asr
- `pickDefaultVoice` 只返回 ASR；会议/唤醒/灯全部跟上
- `tts.synthesize` volc 引擎
- VoicePathPicker 卡面文案按是否已配 TTS

### P2（可选，不进本波承诺）

- 火山角色库音色目录
- 自我名片上「最近房间」误列全部含自己的会话（self 在每个 thread 里）——现码只给智能体卡画房间列表，先观察
- LaunchSidebar 历史标题「同事 · 某某」

---

## 5. 验收清单

### P0 单测（落地门）

- `peopleRoster` / `PeoplePage`：点自己不 `threadOpen`；先开专家再点自己，heading 是自己
- `asrPath`：`companionListenFailover('volc','volc',true) === 'volc'`
- `CompanionStage.recognizer`：火山握手失败且 sherpa 就绪，仍不调 local，文案含 VOICE-004
- `companionSettings` / `CompanionSection` / `prepareCompanionEntry`：选火山/本地不改写插话开关
- `useCompanionMachine`：`companionEventForDispatch('speaking', REPLY_TERMINAL)` 是 `PLAYBACK_ENDED`；idle/listening 迟到事件返回 `null`；状态表本身仍拒绝直接 `REPLY_TERMINAL`
- `asrPath`：`companionVolcDeafGiveUp('volc','volc',2) === true`
- `companionHasFreshAssistantText`：上一轮原文不算本轮首 token
- `CompanionStage.a11y`：listening 时 `onSend===false` 不卡在 thinking
- `wrapRefHostErr(ErrRefEngineStarting)` 仍是 starting；`tts.synthesize` 启动中 = M95-001 且 retryable
- 本地卡 `host_state=launching`：试听 disabled；文案含「启动中」，不含「该段语音合成失败」

### P0 桌面（装包后，浅色 + 月伴）

- [ ] 通讯录：Excel 会话 → 点自己 → 名片是自己，没有专家标题和输入框
- [ ] 浅色会议：左栏与右栏同为浅底，不再暗玻璃
- [ ] 月伴选火山：听灯是火山 seed-asr；密钥坏时 VOICE-004，听灯不跳 sherpa
- [ ] 语音插话关着选火山，开关仍关
- [ ] 连续多轮能出声；打断后下一轮仍听
- [ ] 本地卡：启动中试听不可点；就绪后「温暖御姐」能出声；引擎挂了才报合成失败（可带 last_err）

### P1 另开清单

- [ ] 图3 类型能选听写 / 朗读
- [ ] 同行火山供应商可同时挂 ASR + TTS
- [ ] 会议麦仍只命中 ASR
- [ ] 配了 TTS 后面包屑才写「火山读」

---

## 6. 诚实分

| 面 | 0.4.43 现码 | P0 后 | P1 后 | 封顶 |
|---|---|---|---|---|
| 同事标题跟选中项 | 分叉（图1） | 通讯录内一致 | 同左 | 不做 note-to-self |
| 火山听·想·说 | 听=火山，说=晓晓 | 同左，通道诚实 | 说也可火山 | 角色库另算 |
| 会议浅色 | 左栏被暗玻璃盖 | 浅底 | 同左 | 不重做 IA |
| 月伴连续回合 | 失败会偷切 + 非法事件 | 不偷切、迟到事件吞掉、火山聋识别有上限 | TTS 换引擎 | 4.8 另要跨面 e2e |
| 本地克隆试听 | 启动中报 002 | 启动中锁按钮 + 001 | 同左 | 不重写 SoVITS |

P0 绿只能报「图1/4/5/6 现码缺陷已按合同修」。不能报全面对齐、不能报 4.8。图2–3 仍是 P1。
