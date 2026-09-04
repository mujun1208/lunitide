# 三次反馈全量复核 · 修复升级整改 PRD

日期：2026-09-04  
状态：待用户批准后按配套实施计划开工（先测后改，不升版本、不打安装包）  
产品：月汐（Lunitide）Go Engine + React WebView2 + SQLite  
前序对照：[2026-09-04-six-ux-defect-prd.md](./2026-09-04-six-ux-defect-prd.md)（已 superseded，勿按旧文开工）  
实施计划：[../plans/2026-09-04-ux-engine-companion-remediation.md](../plans/2026-09-04-ux-engine-companion-remediation.md)

本文件是三次用户反馈的**唯一事实源**：问题清单、复核缺口、锁死规格、风险与验收。沿用冻结原则：**不新开独立平台骨架；内核永远 SQLite；机务输出是辅助建议，不构成放行。**

---

## 0. 三次反馈对齐（不得漏项）

| 轮 | 用户原话要点 | 本 PRD 编号 |
|---|---|---|
| 第 1 轮 | 录制中点「听写与纪要设置」，返回稿没了 | #1 |
| 第 1 轮 | 机务工作台按专家域做成折叠菜单（航材→航材/计划，机务维修→排故） | #2 |
| 第 1 轮 | 固定「对话」「月伴对话」三点菜单没有删除 | #3 |
| 第 1 轮 | 说话时「嘴」波动太小，要和内圆一样大 | #4 |
| 第 1 轮 | 两处挂专家不同步，只留对话上挂 | #5 |
| 第 1 轮 | 项目管理中间栏思考/滚动乱跳；选完决策 ENGINE_UNAVAILABLE，选择没进对话框 | #6 |
| 第 2 轮 | 系统崩溃感：项目管理 / 技能市场整页「核心引擎暂时不可用」 | #0（P0-E） |
| 第 3 轮 | 第二次进月伴仍带出上次没做完的对话，应全新空白 | #7 |
| 第 3 轮 | 说完话三条链路太慢；立刻不加思考出字，再回答。只改速度 | #8 |
| 第 3 轮 | 让月伴做具体软件操作总失败，连说「无法执行」。澄清：**不是做成了还喊失败，是根本没做成** | #9 |

十项都是已上线表面断了，不新开子系统。

### 0.1 对照原话：这次能不能彻底解决

| # | 你提的路径 | 这次能否彻底 | 诚实边界 |
|---|---|---|---|
| 0 | 多页引擎不可用、点重试没用 | **产品路径能彻底**：重连 + 自动再拉，不必关应用 | 引擎进程被杀且 takeover 也失败时，只剩横幅，不装成功 |
| 1 | 录制中点「听写与纪要设置」再回来稿没了 | **能彻底**（就是这一颗按钮） | 侧栏「设置」仍整页离开，那是主动离开会议，不在你这条路径里 |
| 2 | 机务按专家域折叠 | **能彻底** | 到期归入机务维修，是补扁平孤儿，不是新功能 |
| 3 | 「对话」「月伴对话」三点没有删除 | **能彻底**（行内「月伴对话」） | 分组标题「对话」本来没有三点；顺带禁重命名，避免单例丢发现键 |
| 4 | 说话嘴和内圆一样大 | **能彻底**（数值锁 + WebGL） | 极差 GPU / 关 WebGL 走 CSS 回退，观感 4.8 |
| 5 | 只留对话上挂，两侧同步 | **能彻底** | — |
| 6 | 中间栏乱跳；选择没进框 | **能彻底**（保稿 + 不因思考 pin） | 点「重试发送」真正发出去，依赖 #0 管道已接上 |
| 7 | 第二次进月伴空白、全新开始 | **能彻底**（舞台空 + 不续跑 + 不把上次失败塞进本访首轮） | 侧栏当文字会话打开「月伴对话」仍看得到历史，那不是语音舞台 |
| 8 | 说完立刻出字再回答，只改速度 | **产品等待能彻底拆掉** | **不能**把火山听写、glm-4 首 token、工具执行变成 0ms；**禁止**用「嗯」假出字充数 |
| 9 | 开软件做成，不要连说无法执行 | **「打开汽水」这条能彻底**（验窗用 sodamusic 别名，前置成功才算打开） | 没装的软件、Windows 抢前台失败：只说一次原因，不装成功 |

上一版 PRD 有三处会让「彻底」落空，本文件已改（见 §1.1 G-E、G-F、G-G）。

---

## 1. 对旧 PRD 的复核：遗漏 / 不全面 / 会卡壳

旧文根因大体对，按它开工会在四处卡死。本文件把缺口锁死，旧结论作废处以下文为准。

### 1.1 会中断实施的缺口（必须补）

**G-A · #7 只关 `historySeed` 不够。**  
`SessionPage` 进月伴还会：

- `liveChatEntry(session.id)` 把未结束流的 `assistantText` / `toolActivities` 灌回舞台  
- `applyTurnBanners` 弹出「上次对话还没做完」  
- `persistDraft` 把失败稿写成助手字  

用户说的「上次没执行完的对话」就是这条复活链。只去掉 `seedCompanionCaptionRounds`，第二次仍会看到旧轮。

**锁死：** 每次挂载月伴舞台 = 空白。取消本会话未结束 companion 流；不灌 seed；不显示续跑条；不传上一轮工具轨迹。单例会话与 SQLite 历史保留，只给文字面和模型上下文。

**G-B · #9 若把「已经打开了」一律 settle，会打断「打开再填」。**  
旧单测 `opened an app is not the user goal` 是为「打开记事本然后写号码」写的。用户这轮是**纯打开**（「打开桌面汽水」）。

**锁死：** 新增 `companionGoalIsOpenOnly(userText)`。仅纯打开/启动（打开/启动/把开 + 软件名，且没有填写/播放/点/输入/搜索）才在 `desktop.open` 验窗成功后 settle。带后续动作的目标仍走桌面环。

**G-C · #9 验窗不要新写一套 Win32。**  
已有 `winexec.ActivateWindowMatching`、`ccapp.FocusWindow`，以及「companion 前台必须拉回上一非月伴窗」的测试。新开 EnumWindows 会和现网抢前台。

**锁死：** `desktop.open` 在 `Start()` 后复用 `ActivateWindowMatching`（查询名 = 用户原话核 / 快捷方式词干）。排除标题或进程为 `Lunitide` / `lunitide.exe` / `月伴` 的窗。验到并前置才算成功。

**G-D · #9 截图默认 foreground 会继续截到月伴。**  
键盘输入已会跳过 Lunitide；screenshot 没有。改全局默认会误伤「点当前对话框」。

**锁死：** 不改工具 schema 默认值。`computer.act` screenshot 若前台是自家窗，改截 `desktop` 或上一非月伴窗。打开类成功后禁止本轮再 `computer.act`。

**G-E · #8 不得用「嗯」假出字（上一版锁错了，作废）。**  
用户要的是：**说完 → 立刻不加思考地出回答的字 → 再朗读这句回答**。舞台其实已在每个 delta 上字幕。再接 `companionOpeningAck`（「嗯，」「嗨，我在呢。」）会：

- 违反人设「不要先垫嗯」  
- 违反「只改速度、其它不动」  
- 200ms 里出现的不是回答，用户仍要等 glm-4，体感更假  

`companionOpeningAck` **保持死代码，禁止接线。**

**锁死（只拆产品等待）：**

1. `RECOGNIZED_FINAL` 后立刻 `chat.start`，中间不垫 thinking 文案、不垫口头禅。  
2. 模型**第一个非空 token**立刻上字幕并离开纯 thinking；禁止等可朗读整句、禁止等 TTS 首包才出字。  
3. TTS 仍等第一句完整标点（先字后声）。这解决「每次都要等他先说话」里**产品自己造成的等**。  
4. 不换 ASR、不改「说完再答」、不换 glm-4、不改 pill。听写对端和模型 TTFT 是边界，不在本项装快。  
5. 打开类带工具的轮，出字仍会晚于闲聊——那是工具 schema + 执行，归 #9，不在 #8 换模型。

**G-F · #7 只清空舞台、仍把上次「无法执行」塞进本访首轮，不算全新开始。**  
单例历史若原样组装，模型会接着上轮失败说，用户以为又带出旧对话。

**锁死：** 新一次挂载设 `companionFreshVisit`。本访第一轮 `chat.start` **不带上一轮未完成/失败助手稿**（含「无法执行」、未结束 tool）。身份记忆与已结算闲聊可留在紧预算里。不续跑、不点「继续上次」。

**G-G · #9 复用 `ActivateWindowMatching` 仍会假成功。**  

- `SetForegroundWindow` 失败时函数现在 `return nil`（当成功）。  
- 汽水进程是 `sodamusic.exe` / `Soda Music.exe`，标题才是「汽水音乐」。用「汽水」去对进程名对不上。  

**锁死：** 验窗查询必须带 `CanonicalMusicApp` 的 `Processes` + `Aliases`（汽水 → sodamusic.exe、Soda Music…）。前置后读前台，标题或进程命中才算成功；`SetForeground` 失败且前台仍是 Lunitide → 未成功，继续轮询到 4s。禁止把 `ActivateWindowMatching` 的 nil 误差当打开成功。

### 1.2 旧文不完整但不会推翻结论

| 项 | 补全 |
|---|---|
| #0 | `engineWatchShouldRelaunch` 已认 `rpcBroken`，缺的是**自拉起分支没把 poison 喂进这个函数**。重连优先于 `--takeover`，避免误杀仍活的引擎。`ReplaceCaller` 必须带锁；重连窗口内的 Call 保持 `ENGINE_UNAVAILABLE` + retryable，禁止 `ok:true`。 |
| #1 | 侧栏「设置」仍整页离开（用户主动走）。只有会议页内「听写与纪要设置」走 overlay。overlay 打开时 `SettingsPage` **不得**调 `meetings.stop` / 停麦。录制中听写引擎与音源只读。 |
| #3 | 分组标题「对话」本身没有三点。保护对象是行内「月伴对话」。月伴 **禁止重命名**（标题是单例发现键）。可保留置顶。删除与重命名都不渲染。 |
| #5 | 月伴舞台不展示专家条，本项只改项目工作台。 |
| #6 | 与 #0 同一次管道死亡。产品保稿必须做；重试成功依赖 #0。思考区 `max-height:28vh` + 禁止因 thinking pin。 |
| #2 | 用户点名航材/计划、机务维修/排故；到期归入机务维修，避免扁平孤儿。不加新 rail。 |
| #4 | 只锁 speaking 内圆振幅；不改 idle/listening 外框。 |

### 1.3 风险与防卡壳

| 风险 | 若不锁会死在 | 防卡 |
|---|---|---|
| `SessionPage.tsx` / `CompanionStage.tsx` 被多任务同时改 | 合并冲突、半套 seed | 按波次串行：#0 → #9 → #8 → #7 → #6/#5/#1 → #3/#2/#4 |
| #0 takeover 误杀卡死但 PID 仍活的引擎 | 用户更崩 | 先 `Connect` 同管；失败且 PID 死才拉起；PID 活且重连失败才 `--takeover` |
| #8 用「嗯」假出字 | 口吻变了，真回答仍慢 | **不接线 ack**；只保证首模型 token 先于 TTS 上屏 |
| #9 `ActivateWindowMatching` 对 sodamusic.exe 对不上 / SetForeground 失败当成功 | 又报打开成功但你看不见 | 用 Processes 别名验窗；前置后读前台，命中才成功 |
| #9 慢启动 App（汽水冷启动 >3s） | 假「无法执行」 | 验窗轮询 4s（200ms×20）；超时才失败，原因写「启动了但窗口没到前台」 |
| #9 排除月伴后截全桌面把月伴 UI 也拍进去 | 模型又点月亮 | 打开类成功即停；点控类用上一非月伴窗 |
| #1 overlay 里改模型触发全局 provider reload | 会议 ASR 重连 | 录制中引擎/音源 disabled；其它设置可改但不停采集 |
| 现网 `chat_continue_test.go` 与 #9 打架 | 一改全红 | 先改测试语义（纯打开 settle，打开再填 continue），再改实现 |
| 不升版本抢发 | 半成品进安装包 | 十项验收勾完且用户点头前：不打 tag、不打安装包 |

### 1.4 没有问题、保持不动的

- 月伴会话单例、侧栏一条「月伴对话」、不可删。  
- `Client.poison` 语义（坏连接必须钉死）；恢复靠新 client + `ReplaceCaller`。  
- 机务台账 Bridge、会议 ASR 协议、TTS 音色、pill 文案。  
- 专家仍走 `session.experts.*` ULID，不新表。  
- named pipe 安全模型、不新 daemon、不新 npm 依赖。

---

## 2. 执行摘要与分数

| # | 用户现象 | 锁死后的根因（一句话） | 现状 | 目标 |
|---|---|---|---:|---:|
| 0 | 多页 ENGINE_UNAVAILABLE，点重试仍失败 | 毒死后无进程内重连；自拉起看门狗不喂 `rpcBroken` | 1.4 | **4.9** |
| 1 | 录制中开设置，稿没了 | `setPage('settings')` 卸载会议页并停麦 | 2.1 | **4.9** |
| 2 | 机务十条扁平 | `mro-rail` 平铺，只有「工具」分组标题 | 3.3 | **4.9** |
| 3 | 月伴三点能删 | `isOrdinarySidebarChat` 不过滤伴生会话 | 2.6 | **4.9** |
| 4 | 说话嘴太小 | `strandsSpeaking.glass===false`，纺锤波远小于内圆 | 3.1 | **4.8** |
| 5 | 两处挂专家不同步 | slug 当 ULID + 两套 state | 1.8 | **4.9** |
| 6 | 中间栏乱跳；决策丢失 | 思考增高触发 pin；`promptOverride` 不入输入框 | 2.2 | **4.8** |
| 7 | 再进月伴带旧稿 | seed + **liveChat 复活** + 续跑条 | 2.0 | **4.9** |
| 8 | 说完出字慢 | 等整句/TTS 才出字（产品等待）；不接线 ack，不装云端 0ms | 2.4 | **4.9** |
| 9 | 开软件失败，连说无法执行 | 打开不验窗；纯打开被逼截月伴；「无法执行」是停环口令 | 1.6 | **4.9** |

**综合现状约 2.1。按本文落地目标 4.9。**  
扣 0.1：WebGL 差机、引擎被杀且 takeover 失败、云端 TTFT 本身不能变成 0ms。方案保证「说完 200ms 内有字」，不保证 glm-4 首 token 为 0。

实施顺序（按伤害，兼顾文件所有权）：

1. **P0-E** #0 引擎自愈  
2. **P0** #9 打开必须做成 → #8 立刻出字 → #7 再进空白 → #6 保稿+不跳 → #1 会议不丢稿 → #5 专家单写  
3. **P1** #3 不可删 → #2 机务折叠  
4. **P2** #4 嘴填满内圆  

---

## 3. 问题 0 · 引擎断连可自愈（P0-E）

### 3.1 现象

同一时刻项目管理 `project.list`、技能市场 `skill.catalog.list`、对话 `chat.start` 都报 `ENGINE_UNAVAILABLE`。壳层还在，点页面「重试」无效。

### 3.2 根因

`caller.Call` 失败一律该码。`Client.poison` 钉死连接。自拉起路径只 `Wait()` 进程退出，RPC 断了但 PID 活着不 relaunch。页面重试打同一具尸体。

### 3.3 锁死方案

```text
1. Broken() 或 Call error
2. 进程内 Connect 同管 \\.\pipe\lunitide-gateway-<user>
   成功 → Gateway.ReplaceCaller(新 client)（带锁）
        → 壳层「核心引擎已恢复」2s
        → 当前页自动再 load
3. 连不上且 PID 死：拉起 lunitide-engine.exe，再走 2
4. 连不上且 PID 活：现有 D11 --takeover
5. 都失败：横幅保持 +「重试连接」；禁止 ok:true
```

自拉起与重连**都**把 `client.Broken()` 喂给 `engineWatchShouldRelaunch`。  
`ENGINE_UNAVAILABLE` 的 details 带短因（写超时 / 管道断开 / 握手拒绝），code 不变。  
壳层只一条横幅。技能市场失败态补 `code` + `correlationId`。专家中心同一 catalog 失败态对齐。

### 3.4 验收

| ID | 断言 |
|---|---|
| G0 | 自拉起路径 `rpcBroken=true` 且 PID 活 → 走重连/relaunch，不是干等 Wait |
| G1 | poison 后 `ReplaceCaller` + 新 Connect，下一次 `project.list` 成功（fake caller） |
| G2 | 连续两次该码只一条壳层横幅 |
| G3 | 技能市场显示 code + correlationId；壳层重试后 `catalogList` 再被调用 |
| G4 | 项目清单失败后工具条仍在；ReplaceCaller 后「重试」能列出 |
| G5 | Call error 不得映射 `ok:true` |
| G6 | `ReplaceCaller` 并发 Call 不 panic；失败仍 retryable |

### 3.5 文件

`cmd/desktop/main.go`、`watchdog.go`、`watchdog_test.go`  
`internal/hostbridge/gateway.go`、`gateway_test.go`  
`internal/engineclient/client_windows.go`（不改 poison 语义）  
`web/src/App.tsx`、`web/src/bridge/client.ts`  
`web/src/project/ProjectPage.tsx`、`web/src/skill/SkillPage.tsx`、`web/src/expert/ExpertCenterPage.tsx`

---

## 4. 问题 1 · 会议设置不丢稿

**锁死：** `onOpenSettings` 不再 `setPage`。会议页 overlay 打开已有 `SettingsPage`（`initialCategory='meetings'`）。父树不卸载，麦/系统声/heartbeat/逐字稿继续。录制中引擎与音源 disabled +「本场结束后生效」。侧栏设置仍整页。`SettingsPage` 在 `embedded` 且 `recording` 时不得调用停采集 API。

验收 M1–M5 同旧文，并加 **M6**：overlay 打开期间 `meetings.stop` / `speechRef.stop` 调用次数为 0。

文件：`MeetingPage.tsx`、`App.tsx`、`SettingsPage.tsx` 及测试。

---

## 5. 问题 2 · 机务按专家域折叠

```text
手册
机务维修 ▾   排故 / 到期
航材 ▾       航材 / 计划
工具化工品
工具 ▾       检查单 / 审计 / 机队
```

点组只展开，点叶子才切 `rail`。`localStorage` 键 `lunitide:mro-rail-open`。`askCatalogForRail` 不改。不加新 rail。

验收 R1–R5 同旧文。

文件：`MroWorkbenchPage.tsx`、新建 `mroRailGroups.ts`、`styles.css`。

---

## 6. 问题 3 · 固定会话不可删

新增 `isProtectedSidebarChat(title)`：`isCompanionChatTitle` ∪ `isPlaceholderChatTitle` ∪ 标题为 `对话`/`Chat`。

侧栏：保护会话不渲染删除、不渲染重命名（月伴标题是发现键）。可留置顶。若动作数为 0 则三点也不渲染。`remove()` 对保护会话直接 return。顶栏垃圾桶不渲染。后端 `session.delete` 契约不改。

验收 P1–P4，并加 **P5**：月伴菜单无「重命名」。

文件：`sessionTitle.ts`、`App.tsx`、`SessionPage.tsx`。

---

## 7. 问题 4 · 说话嘴填满内圆

`strandsSpeaking(g).glass === true`；`gain=0.6` 垂直峰 ≥ 内圆半径 92%；`scale <= 0.62`；`amplitude >= 1.45`。thinking 的 `STRANDS_THINKING.scale` 仍 1.15。CSS speaking strands `scale(1.55)`。无 WebGL 时 halo-wave inset 收到内盘。`prefers-reduced-motion` 静止填满。不改状态机。

验收 V1–V3。

文件：`moonVisual.ts`、`styles.css`、`MoonSphere.tsx`（只消费参数）。

---

## 8. 问题 5 · 专家只留对话写

输入框是唯一写入口。左侧改为只读 `PhaseExpertsMirror`（「本阶段将使用」）。「推荐」写入同一 `session.experts.set`（ULID）。slug 先 `findInstalledExpert`。set 失败回滚 chip 并报错。不新表。

验收 E1–E5。

文件：`PhaseExpertsBar.tsx` → mirror、`SessionPage.tsx`、`expertIds.ts`、`phaseExperts.ts`。

---

## 9. 问题 6 · 不乱跳 + 决策不丢

**抖动：** `pinIfFollowing` 忽略思考高度。思考区 `contain:strict; max-height:28vh; overflow-y:auto`。RO 不观察 `.thinking-panel`。effect 去掉 `thinkingOpen`。用户上拉锁定 `userFollowPaused`，思考增高不得解除。项目与个人共用 `streamScroll.ts`。

**决策：** 先 `setText(followUp)` 再 `sendAndChat`。失败：输入框保留、wizard 仍开、已选项仍在、toast「你的选择已留在输入框，可点重试」、禁止失败后 `decideTool` 标完成。retryable 引擎错误自动重试 1 次（500ms）。文案禁止「从最新页重试」。真正成功依赖 #0。

验收 J1–J3、D1–D4。

文件：`streamScroll.ts`、`SessionPage.tsx`、`UserAskWizard.tsx`、`userAsk.ts`、`MarkdownMessage.tsx`、`styles.css`。

---

## 10. 问题 7 · 再进月伴必须空白

每次 `CompanionStage` 挂载：

1. `rounds = []`，忽略 `historySeed`  
2. 若 `liveChatEntry(sessionId)` 未结束：`cancelLiveChatTurn`（companion 退出也要 cancel，禁止把半轮留在内存）  
3. `resumeBanner` 在 `initialCompanion` 时强制 false  
4. 不把上一轮 `toolActivities` / `persistDraft` 传进舞台  
5. 单例、标题、置顶不动  
6. **本访首轮 `companionFreshVisit=true`**：组装上下文时丢掉上一轮未完成/失败助手稿（含「无法执行」）。身份记忆保留。避免模型接着上轮失败说。  

验收 C7-1–C7-4，并加 **C7-5**：进月伴时若有未结束 live 流，测试断言 `cancel` 被调用且舞台无旧字幕。  
**C7-6：** 上轮助手为「无法执行…」时，本访第一轮 `chat.start` 的 messages 不含该句。

文件：`CompanionStage.tsx`、`SessionPage.tsx`、`liveChat.ts`、`SessionPage.companion.test.tsx`。

---

## 11. 问题 8 · 只改出字速度

用户原话：**说完 → 立刻不加思考地出字 → 再回答（朗读）**。只改速度。

舞台已在每个模型 delta 上字幕。慢在三处，只拆**产品自己造成的等**：

| 段 | 动不动 |
|---|---|
| 听写「说完再答」定稿 | **不动**（改它就是改交互，不是只改速度） |
| 定稿后还停在 thinking、等整句/等 TTS 才出字 | **拆掉** |
| glm-4 / 火山网络 / 工具执行 | **不换、不装快** |

**禁止**接线 `companionOpeningAck`。出的字必须是模型回答本身，不是「嗯，」。

锁死：

1. `RECOGNIZED_FINAL` → 立刻 `chat.start`。  
2. 第一个非空模型 token → 上字幕、离开纯 thinking。不准等 `takeSpeakableChunk`，不准等 TTS。  
3. TTS 仍等第一句完整标点（先字后声）。  
4. `DisableReasoning` 保持。不改 pill、不改人设长文、不改工具集。

验收：

| ID | 断言 |
|---|---|
| C8-1 | companion 流在上游返回前 **没有** 注入 ack delta（`嗯，` / `嗨，我在呢`） |
| C8-2 | 第一条模型 delta 到达后，字幕已有该回答的字，且不因 TTS 未开而仍只有 thinking |
| C8-3 | TTS 第一次 enqueue 是模型句，不是 ack；允许 firstAudio > 首字时间 |
| C8-4 | 闲聊轮工具集不变；打开/播放轮工具集不变 |

文件：`CompanionStage.tsx`、`CompanionStage.headstart.test.tsx`、`chat_run_stream.go`（确认无 ack 接线）。不改 `companionOpeningAck` 的生产调用。

---

## 12. 问题 9 · 打开软件必须做成，无法执行只说一次

**事实：** 快捷方式已命中；`Start()` 当成功；引擎不认打开为完成；逼 `computer.act` 截月伴自己；「无法执行」是停环口令。用户看到的是软件没到前台。

**锁死：**

1. `desktop.open`：`Start()` 后验窗。查询串 = 用户核 + `CanonicalMusicApp` 的 Aliases/Processes（汽水必须含 `sodamusic.exe`、`Soda Music`）。排除 Lunitide。轮询 4s。**前置后读前台，标题或进程命中才 `opened`。** `ActivateWindowMatching` 在 `SetForeground` 失败时返回 nil 不得当成功。  
2. `companionGoalIsOpenOnly`：纯打开成功 → settle，禁止 desktop nudge，禁止再 `computer.act`。打开再填 / 打开再播 → 继续桌面环。  
3. screenshot 前台若是自家窗 → desktop 或上一非月伴窗。  
4. 成功打开：模型、结轮通知、舞台朗读都禁止「无法执行」。失败：每轮最多一句，必须带工具原文。  
5. 不把轨迹「完成」当成用户成功。

验收 C9-1–C9-6，并加 **C9-7**：`companionGoalIsOpenOnly("打开记事本然后填身份证")==false` 且该轮 `desktop.open` 后仍可 continue。  
**C9-8：** 假前台为 Lunitide、已有 `sodamusic.exe` → 查询「汽水」验窗成功（Processes 别名），不得只拿中文词去对 exe 名。

文件：`chat_continue.go`、`chat_turn_notice.go`、`chat_companion_speech.go`、`desktop.go`、`desktop_launch.go`、`runtime.go`、`runhost.go`、`CompanionStage.tsx`。

---

## 13. 非目标

- 不重做会议 ASR / 火山协议。  
- 不新增机务 rail、不改台账 Bridge。  
- 不把月伴改成每次新建会话，不清 SQLite 历史。  
- 不新阶段专家表。  
- 不把 ENGINE_UNAVAILABLE 改成假成功。  
- 不新 daemon、不改 named pipe 安全模型、不新 npm 依赖、不新窗口路由。  
- #8 不换模型、不改 TTS/人设长文/pill、**不接线 opening ack**。  
- #9 验不到窗就必须失败，只说一次原因。

---

## 14. 依赖与文件所有权

```text
#0 hostbridge/desktop watchdog
        │
        ▼
#9 chat_continue + desktop.open 验窗
        │
        ▼
#8 首 token 出字（不接线 ack）
        │
        ▼
#7 CompanionStage 空白 + freshVisit     ← 与 #8 都碰 CompanionStage，必须 #8 先合再 #7
        │
        ├─ #6 SessionPage 滚动/决策  ← #0 之后，#7 之后（少冲突）
        ├─ #5 PhaseExperts + SessionPage persist
        └─ #1 Meeting overlay
#3 sessionTitle + App 侧栏
#2 mroRailGroups（独立）
#4 moonVisual（独立，最后）
```

`SessionPage.tsx` 只允许一波一个人改：顺序 #6 → #5 → #7 收尾（#7 只动 companion 分支）。  
`CompanionStage.tsx`：#8 再 #7 再 #9 前端朗读。

---

## 15. 测试与发布门槛

每个问题：**失败单测先写，再改实现。**

- Go：`./internal/hostbridge` `./cmd/desktop` `./internal/app` `./internal/toolruntime` `./internal/ccapp` + `go test ./...`  
- Web：M / R / P / V / E / J / D / G / C7–C9 全有 vitest；`npx tsc --noEmit`；`npm run verify:bridge`  
- 不升版本、不打安装包，直到本表勾完且用户点头  

手工 15 分钟：引擎横幅恢复、会议不丢稿、机务折叠、月伴不可删、嘴填满、专家单写、滚动/决策、再进空白、立刻出字、打开汽水到前台且不说无法执行。

---

## 16. 方案评分（对齐 4.9）

| 维度 | 分 | 说明 |
|---|---:|---|
| 根因可复现 | 1.00 | 十条对到函数；二次复核补上 ack 假出字、sodamusic 别名、freshVisit |
| 方案唯一可落地 | 0.98 | 每项一个采用/不采用；#8 明确不接线 ack |
| 验收可测试 | 0.96 | C7-6、C8-1 反 ack、C9-8 别名验窗 |
| 范围克制 | 0.98 | #8 只拆产品等待，不装云端 0ms |
| 实施顺序 | 0.98 | 文件所有权串行 |
| **方案分** | **4.9** | 对照原话能落地；#8/#0 的对端上限写进诚实边界，不装 5.0 |

落地体验目标 **4.9**。剩余 0.1 = 机器 WebGL + 进程被杀 + 云端 TTFT。

---

## 17. 禁止再打开的分叉

| 题 | 不采用 | 采用 |
|---|---|---|
| 0 | 只改文案；重试撞死 client；新 daemon；先 takeover 再重连 | ReplaceCaller；看门狗喂 rpcBroken；重连优先 |
| 1 | 录制禁用设置；跳转后再从 SQLite 猜稿 | Overlay，页不卸载 |
| 2 | 只加两个组、其余扁平 | 五级一次做完 |
| 3 | 隐藏整个三点；后端禁 delete | UI 去掉删除和重命名 |
| 4 | 只加大 vmin 外框 | glass + 振幅锁 |
| 5 | 两处双写同步 | 只留对话写 |
| 6 | 只 toast；从最新页重试 | 先入输入框，失败保留 |
| 7 | 每次新建会话；只关 seed 不管 live；首轮仍带上轮无法执行 | 单例 + 取消未结束流 + 舞台空 + freshVisit 丢掉失败稿 |
| 8 | 换模型；等 TTS 再出字；接线「嗯」ack 假出字 | 不接线 ack；首模型 token 先于 TTS 上屏 |
| 9 | 轨迹完成当成功；打开一律 settle；新写 Win32；「汽水」只对中文标题 | 纯打开才 settle；Processes 别名验窗；前台命中才成功；无法执行每轮一句 |
