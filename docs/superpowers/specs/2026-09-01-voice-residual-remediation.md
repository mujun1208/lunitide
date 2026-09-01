# 月汐语音残差整改合同（0.4.45 真机）

日期：2026-09-01  
产品：月汐 / Lunitide `VERSION` **0.4.45**（已装官方包）→ 本波落地后打 **0.4.46**  
状态：**残差落地合同**。和 `2026-09-01-voice-pipeline-remediation.md` 冲突时：**本波以本文为准**；冻结表仍以主合同 §8 与语音合同 §0 为准。  
前波 W-VOICE-1..4 已装：链路通了。本文只修 0.4.45 桌面仍看见的四张图。

**不能报：** 语音 4.5、正式发布、本地 50 音色可用、火山听写已齐（火山听+说本波现场已通，仍不写「已齐」直到 §7 再勾）。

---

## 0. 冻结（沿用，不许破）

- 不重写 `useCompanionMachine` 整表
- 不在 openspeech 上挂对话 LLM
- 选火山失败 **禁止** 静默切 sherpa / Web Speech
- 不改 `E:\GPT-SoVITS\start-api-cpu.bat`、端口 9880、盘符
- 不静默回落晓晓还标「GPT-SoVITS / 本地克隆」
- 不把 `jieba_fast\dict.txt` 打进安装包；环境坏了灯要诚实
- 不 `generate:bridge` 当施工步；不改 migrations；不提交密钥

---

## 1. 现场四图（2026-09-01 ~11:07，0.4.45）

| 图 | 用户说 | 灯 / 字幕 | 根因（现码） |
|---|---|---|---|
| **1** | 本地模型模式：字出来了，没有声音 | 听=本机 sherpa，说=**晓晓**，想=glm-4-*；状态「正在回你」；字幕是用户 ASR | 「本地模型」多半是本地 LLM，不是本地语音卡。说灯已是晓晓却仍无声：首 token 慢，或 SoVITS 启动中把合成卡住，或回落晓晓后音频未解锁。本地卡 SoVITS 未就绪时现码只出字幕（`本机朗读未就绪，先听和看字幕`），**故意无声** |
| **2** | 退出后侧栏找不到刚才的说话会话 | 对话列表仍是旧线程「你可以听到我说话吗」 | `initialCompanion` 进场 **不** `home()`，`items` 一直 `[]`。`onExit`：`initialCompanion && !items.length && !chatActive` → `onEmpty()` **删会话**。`sendAndChat` 已 `markSessionEngaged` + 改标题，但退出条件没看 engaged |
| **3** | 火山能听能说；「好，我帮你查一下」说了两遍 | 听=seed-asr，说=小何；工具轨迹 2×search + fetch | `companionToolLeadIn` 在 **每一步** `assistantText.Len()==stepTextStart` 时注入。模型先说「我就帮你查…」后，后续空文本的 search/fetch **再各垫一次** |
| **4** | 用户标「云端」；说完后又多一排不是他说的字 | 灯实为 sherpa + **GPT-SoVITS 启动中**（本地卡）；回答里垫话出现三次 | ① 同图 3 的二次/三次垫话在 TTS 读完首句后才流进字幕，像「自己又出一排字」。② TTS 结束后麦重开，回声/垫话被当成新用户句。③ 两搜一抓把首句拖慢 |

整体慢：天气强制 `web.search`，模型再搜一次再 `web.fetch`；垫话重复朗读；SoVITS 启动中阻塞合成。

---

## 2. 产品裁定

| # | 裁定 |
|---|---|
| P1 | 同一轮工具垫话 **最多一句**。模型已经开口（含「我帮你查」）则 **不再** 注入 `好，我帮你查一下。` |
| P2 | 月伴说过至少一轮后退出：**禁止** 当空会话删除。侧栏必须能找到（标题=第一句用户话，或仍叫「月伴对话」也要留下） |
| P3 | 本地卡 SoVITS 未就绪：**可以** 用晓晓读出这一轮，但灯必须改成「晓晓（本机朗读未就绪）」，**禁止** 仍标 GPT-SoVITS。不把 `engine=edge` 写回本地卡存档 |
| P4 | 她说完后的回声窗口内，听写不得提交新用户回合。垫话原句不得当作用户话 |
| P5 | 月伴查天气：**一次** `web.search`，用摘要说气温阴晴；没有用户给的 URL 时 **不要** 第二次 search / `web.fetch` |

---

## 3. 整改波次（一次做完）

### W-VOICE-5a — 垫话只一次 + 字幕去重（图 3 / 图 4）

**改：** `shouldInjectCompanionToolLeadIn`：本轮已注入或 `assistantText` 非空 → 不注入。`companionCaptionFromStream` 合并相邻重复短句（含「帮你查一下」变体）。

**测：** Go：已有正文不再注入；第二次 tool 步不注入。Vitest：三连「好，我帮你查一下」塌成一句。

### W-VOICE-5b — 退出保留会话（图 2）

**改：** `companionShouldDiscardOnExit`：仅当 `initialCompanion && items==0 && !chatActive && !sessionEngaged`。`onExit` / `requestLeave` 共用。engaged 在 `sendAndChat` 已有。

**测：** 说过一句再点退出，不调用 `session.delete`。没说过话的空月伴仍可删。

### W-VOICE-5c — 本地能出声且灯诚实（图 1 / 图 4 启动中）

**改：** `companionPlaybackSettings`：本地 + SoVITS 就绪 → `lockEngine` 只走 ref；未就绪 → **本轮** Edge + 灯「晓晓（本机朗读未就绪）」，不 `saveCompanionSettings(engine=edge)`。预热探测不得把本地卡改成 edge。

**测：** 本地未就绪的 extras 是 edge+lock；就绪是 ref+lock。fallback 回调不把本地存档改成 edge。

### W-VOICE-5d — 说完不冒字（图 4）

**改：** `shouldAcceptUserTranscript` 拒绝垫话原句；`echoGuardActive` 为真时拒绝提交。播放结束后用 ≥900ms 的说完回声窗（不改 `ECHO_GUARD_MS` 的 300–600 合同）。

**测：** 听写到「好，我帮你查一下」为 false；echoGuardActive 为 false 接受新问。

### W-VOICE-5e — 天气一轮搜（慢）

**改：** 人设：「查天气只 search 一次，不要 fetch」。引擎：本轮已成功/已见 `web.search` 后，再来的 search/fetch（用户话里没有 http URL）直接跳过网络，只回「已有摘要，直接说」。

**测：** `companionRedundantWebSkip` 在已有 search 后对第二 search / fetch 为 true；用户带 `https://` 的 fetch 不跳。

---

## 4. 不要做

- 不把缺 `dict.txt` 修进 bat / 安装包
- 不把火山聋改回 sherpa
- 不重开 instantAck 默认开
- 不把图 4 的 sherpa+SoVITS 灯改报成「云端已齐」——那是本地卡
- 本波不做签名包以外的发布声明；装包后才能勾桌面

---

## 5. 桌面再勾（装 0.4.46 后，需人点）

| 行 | 动作 | 通过 |
|---|---|---|
| V5-LOCAL-SOUND | 本地卡，SoVITS 未就绪时说一句 | 有晓晓声，灯写未就绪，不标克隆 |
| V5-KEEP | 月伴问天气，退出 | 侧栏看得到这句标题 |
| V5-LEADIN | 火山问天气 | 「好，我帮你查一下」最多一声 |
| V5-ECHO | 等她说完 | 不出现不是你说的新用户行 |
| V5-SPEED | 同上天气 | 工具轨迹 **一次** search，无第二次 search/fetch |
