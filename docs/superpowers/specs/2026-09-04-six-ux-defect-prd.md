# 六项体验缺陷 + 引擎断连 + 月伴三病 PRD · 可落地整改

> **已 superseded（2026-09-04 复核）。** 三次反馈的锁定规格、遗漏补丁、风险与分任务实施步骤，以新文档为准：  
> [2026-09-04-remediation-upgrade-prd.md](./2026-09-04-remediation-upgrade-prd.md)  
> 实施计划：[../plans/2026-09-04-ux-engine-companion-remediation.md](../plans/2026-09-04-ux-engine-companion-remediation.md)

日期：2026-09-04  
状态：已被升级 PRD 取代，仅作对照，勿按本文开工  
产品：月汐（Lunitide）Go Engine + React WebView2 + SQLite  
前序：[2026-09-04-six-surface-prd-v2.md](./2026-09-04-six-surface-prd-v2.md)、[2026-09-04-four-ops-workbench-ui-prd.md](./2026-09-04-four-ops-workbench-ui-prd.md)

本文件是「会议设置丢稿 / 机务导航 / 固定会话删除 / 说话嘴 / 双处挂专家 / 项目管理对话抖动与决策丢失 / **系统级引擎断连** / **月伴二次进入带旧稿、出字慢、软件操作连说无法执行**」的产品与技术事实源。沿用冻结原则：**不新开独立平台骨架；内核永远 SQLite；机务输出是辅助建议，不构成放行。**

> **2026-09-04 增补 1：** 用户在项目管理清单、技能市场同时看到「核心引擎暂时不可用 / ENGINE_UNAVAILABLE」。这不是页面崩溃，也不是对话页单独坏了——是宿主 WebView 还活着、引擎 RPC 已被毒死，而**拉起引擎的那条看门狗不看管道断开**。升为 **P0-E**，压过其余体验项。
>
> **2026-09-04 增补 2：** 月伴三项。用户澄清「无法执行」**不是做成了还喊失败**，而是**根本没做成**，还连说好几遍。对照现码：快捷方式已经点到了，但引擎故意不承认「打开」算完成，接着逼模型在**月伴自己的前台窗口**上 see→act→verify，点不到目标软件，只能靠说「无法执行」停循环。

---

## 0. 执行摘要

十项都是**已经上线的表面在真实路径上断了**，不是缺新功能。对照现码，根因全部可定位到具体文件与状态机，不需要新子系统。

| # | 用户现象 | 现码根因（一句话） | 现状 | 整改后目标 |
|---|---|---|---:|---:|
| 0 | 项目管理 / 技能市场整页「核心引擎暂时不可用」，点重试仍失败 | 宿主 `caller.Call` 失败一律映射 `ENGINE_UNAVAILABLE`；**自拉起引擎只等进程退出，不管 RPC poison** | 1.4 | **4.9** |
| 1 | 录制中点「听写与纪要设置」，返回后逐字稿全没了 | `setPage('settings')` 卸载 `MeetingPage`，卸载 effect **停麦停录** | 2.1 | **4.9** |
| 2 | 机务左栏十条扁平，不像专家域 | `mro-rail` 仍是平铺按钮，只有「工具」一行分组标题 | 3.3 | **4.9** |
| 3 | 「月伴对话」三点菜单能删 | `isOrdinarySidebarChat` 不过滤伴生会话，溢出菜单无保护 | 2.6 | **4.9** |
| 4 | 说话时「嘴」只是圈内一小条 | `strandsSpeaking` 关了 glass，波动半径远小于内圆 | 3.1 | **4.8** |
| 5 | 左侧「本阶段专家」与输入框专家不同步，且报参数无效 | 侧栏写 catalog slug，桥只收 ULID；两套 React state | 1.8 | **4.9** |
| 6 | 项目管理中间栏思考/滚动乱跳；选完决策后 ENGINE_UNAVAILABLE，选择没进对话框 | 思考增高触发 pin；`UserAsk` 用 `promptOverride` 直发，失败不回填 | 2.2 | **4.8** |
| 7 | 第二次进月伴仍显示上次没做完的对话 | 单例会话 + `historySeed` 把上一轮字幕灌进舞台；未完成轮也会复活 | 2.0 | **4.9** |
| 8 | 说完话要等三条链路，出字慢 | `companionOpeningAck` 是死代码；舞台停在 thinking 等到首句可朗读 | 2.4 | **4.9** |
| 9 | 让月伴开软件总失败，连说「无法执行」 | 「打开」被单测锁成未完成 → 逼 `computer.act` 截月伴自己 → 点控失败 → 「无法执行」是唯一停循环口令 | 1.6 | **4.9** |

**综合：现状约 2.1 / 5（引擎断连 + 月伴操作死循环把语音面打穿）。按本 PRD 落地后目标 4.9 / 5。**  
扣 0.1 的两项（说话嘴、引擎被杀）依赖本机 WebGL 与进程存活，无法用单测保证到 5.0。出字速度仍受对端 TTFT 上限，方案保证「说完立刻出字」，不保证云端首 token 本身变成 0ms。

推荐实施顺序（按用户可见伤害，不是按模块大小）：

1. **P0-E** — #0 引擎断连可自愈（进程内重连；看门狗补齐；壳层横幅）  
2. **P0** — #9 月伴打开软件必须做成、#8 说完立刻出字、#7 再进月伴空白、#6 决策不丢 + 不乱跳、#1 会议不丢稿、#5 专家单一写面  
3. **P1** — #3 固定会话不可删、#2 机务折叠导航  
4. **P2** — #4 说话嘴填满内圆（视觉锁升级）

---

## 1. 问题 1 · 录制中打开设置，逐字稿消失

### 1.1 用户路径

开始录制 → 中间栏出现逐字稿 → 点「听写与纪要设置」→ 进入设置 → 返回会议 → **稿空了，计时也可能被打断**。

用户可接受两种产品结果，本 PRD **锁死第一种**（第二种作为降级，不单独做）：

1. **可以点设置，返回时录制与稿都在**（采用）  
2. 录制中禁用设置（不采用：用户正在录，往往正是要改听写引擎）

### 1.2 根因

`App.tsx` 路由是互斥页：

```text
page==='meetings' ? <MeetingPage onOpenSettings={() => {
  setSettingsCategory('meetings')
  setPage('settings')   // 卸载 MeetingPage
}} /> : page==='settings' ? <SettingsPage ... />
```

`MeetingPage` 卸载时的 cleanup（`MeetingPage.tsx` 约 L536–553）会：

- `speechRef.stop()` 停听写  
- `audioRef.stop()` 停本机录音写入  
- `releaseMeetingCapture()` 放掉系统声  
- 清掉 heartbeat / stall / fallback 定时器  

前端 `liveLines` / `interim` 是 React state，随卸载一起没。后端 `meetings.start` 的 recording 行可能还在，但 **实时字幕会话已被拆掉**。返回时虽有 leftover 续录逻辑，也只能从 `meetings.get` 里已落库的 transcript 恢复——实时行若还没 append 完，用户看到的就是「全没了」。

### 1.3 方案（锁死）

**设置改成会议页上的覆盖层，禁止改 `page`。**

- `onOpenSettings` 不再 `setPage('settings')`。  
- `MeetingPage` 内打开 `SettingsPage` 的会议分类（已有 `initialCategory='meetings'`、`backLabel='返回会议'`），用现有 overlay 壳（同聊天页 `model-manager-overlay`），**父树保持挂载**。  
- 覆盖层打开期间：麦克风、系统声、heartbeat、逐字稿 state **继续跑**。  
- 录制中：听写引擎 / 音源两项 **只读 + 文案「本场结束后生效」**；字幕样式、热键、模型等可改。  
- 关闭覆盖层：焦点回到逐字稿区，计时连续，已有行一条不少。  
- 侧栏点「设置」仍走整页设置（用户主动离开会议）；仅会议页内的「听写与纪要设置」走 overlay。

### 1.4 验收

| ID | 断言 |
|---|---|
| M1 | 录制 30s 以上、稿有 ≥3 行时打开设置再关，行数与全文不变，计时单调递增 |
| M2 | overlay 打开期间 heartbeat 仍调用，`status==='recording'` |
| M3 | 录制中引擎/音源控件 `disabled`，aria 说明「本场结束后生效」 |
| M4 | 非录制状态打开设置，引擎/音源可改（回归） |
| M5 | 测试：`MeetingPage` 点设置 **不** 触发 `setPage`；`onOpenSettings` 可改为页内 callback |

### 1.5 主要文件

- `web/src/meetings/MeetingPage.tsx` / `MeetingPage.test.tsx`  
- `web/src/App.tsx`（`onOpenSettings` 不再切 page）  
- `web/src/settings/SettingsPage.tsx`（embedded / overlay 模式）

---

## 2. 问题 2 · 机务工作台按专家域折叠

### 2.1 用户意图

左栏不要十条平铺。按**最初四张运营专家 + 机务维修专家**做上级菜单。用户点名：

- **航材** 下：航材、计划  
- **机务维修** 下：排故  

其余轨按同一原则补齐，避免再留扁平孤儿。

### 2.2 现码

`MroWorkbenchPage.tsx` `mro-rail`：手册 / 排故 / 到期 / 工具化工品 / 航材 / 计划 + 分组标题「工具」+ 检查单 / 审计 / 机队。  
专家映射已在 `expertIds.ts`：`parts→航材`、`mx-planning→计划`、`tooling-chemical→工具化工品`、`uas→到期`、默认 `mro-expert→手册/排故`。

### 2.3 锁死信息架构

```text
手册                         ← 入口，mro-expert
机务维修 ▾                   ← mro-expert + uas-airworthiness
  排故
  到期
航材 ▾                       ← parts-expert + mx-planning-expert
  航材
  计划
工具化工品                    ← tooling-chemical-expert
工具 ▾                       ← 合规/机队（非专家卡）
  检查单
  审计
  机队
```

规则：

- 分组标题可点：展开/折叠；点叶子才切 `rail`。  
- 默认：当前 `rail` 所在组展开，其余折叠。记忆 `localStorage` 键 `lunitide:mro-rail-open`（JSON：组 id → bool）。  
- 从专家中心「打开工作台」仍走 `workbenchRailForCatalog`，并自动展开对应组。  
- 问月汐的 `askCatalogForRail` **不改**（仍按叶子轨选专家）。  
- 不做第三层、不加新 rail、不改台账 API。

### 2.4 验收

| ID | 断言 |
|---|---|
| R1 | 左栏可见 5 个一级：手册、机务维修、航材、工具化工品、工具 |
| R2 | 「航材」展开后可见 航材、计划；「机务维修」展开后可见 排故、到期 |
| R3 | 点「计划」：`rail==='plan'`，问月汐仍挂 `mx-planning-expert` |
| R4 | 刷新后展开状态按 localStorage 恢复；当前轨所在组强制展开 |
| R5 | 键盘：分组 `aria-expanded`，叶子 `aria-selected` |

### 2.5 主要文件

- `web/src/mro/MroWorkbenchPage.tsx` / `MroWorkbenchPage.test.tsx`  
- `web/src/mro/mroRailGroups.ts`（新建：组定义 + 展开状态纯函数）  
- `web/src/styles.css`（复用 office-group / org-nav 令牌，不新依赖）

---

## 3. 问题 3 · 固定会话三点菜单不可删

### 3.1 用户意图

「对话」「月伴对话」是系统位，三点菜单 **没有删除**。普通历史对话仍可删。

### 3.2 根因

- `isOrdinarySidebarChat` = 非「新对话」且非同事会话 → **月伴会进列表**。  
- `isCompanionChatTitle` 已存在，但侧栏溢出菜单与会话顶栏垃圾桶都没用它。  
- `ensureCompanionSession` 把月伴做成单例；删掉会再造一个空壳，用户感知是「系统对话被毁了」。

「对话」在侧栏是分组标题，本身没有三点。保护对象锁死为：

1. `isCompanionChatTitle(title)`（`月伴对话` / `Companion talk`）  
2. `isPlaceholderChatTitle(title)`（`新对话` / `New chat`）——空草稿走现有自动回收，不提供手动删除  
3. 标题 trim 后等于 `对话` 或 `Chat`（若将来出现系统位）

### 3.3 方案（锁死）

新增 `isProtectedSidebarChat(title)`，放进 `sessionTitle.ts`。

侧栏溢出菜单：

- 保护会话：**不渲染「删除」**；可保留置顶（若已有）。  
- 若保护会话菜单只剩 0 个动作：三点按钮也不渲染。  
- `remove()` 入口再挡一层：保护会话直接 return，不弹确认框。

会话页顶栏垃圾桶：保护会话不渲染。`session.delete` 后端不改契约（仍可删），UI 层禁止。

### 3.4 验收

| ID | 断言 |
|---|---|
| P1 | 列表里「月伴对话」无删除按钮、无删除确认 |
| P2 | 普通标题「上海天气」仍有删除 |
| P3 | 打开月伴后顶栏无垃圾桶 |
| P4 | 直接调 UI `remove` 保护会话，`sessions.delete` 不被调用 |

### 3.5 主要文件

- `web/src/session/sessionTitle.ts` / `sessionTitle.test.ts`  
- `web/src/App.tsx`（LaunchSidebar 菜单）  
- `web/src/session/SessionPage.tsx`（顶栏删除）  
- `web/src/App.test.tsx`

---

## 4. 问题 4 · 说话「嘴」要填满内圆

### 4.1 用户意图

前台说话时中间那条发光「嘴」波动太小，要和**内部圆形**一样大。不是再放大整颗月亮外框（46vmin 已经比 idle 大）。

### 4.2 根因

上一轮视觉锁保证了：

- speaking 走 glass 模式（`moonVisualMode`）  
- 外框 46vmin、strands CSS `scale(1.38)`  
- `strandsSpeaking.scale = 0.82`（uScale 越小球越大）

但 `strandsSpeaking` **把 `glass: false`**。Strands shader 在非 glass 时画的是水平纺锤波，半径由 `uAmplitude` + `uScale` + taper 决定，肉眼就是圈里一条窄嘴。用户圈出来的正是这条波，不是外圈 halo。

### 4.3 方案（锁死）

说话态必须同时满足：

1. **内圆被等离子体填满**（恢复 `glass: true`，与 thinking 同一渲染器）。  
2. **嘴的垂直振幅贴住内圆**：`gain=0.6` 时垂直峰 ≥ 内圆半径的 **92%**；`gain=1` 时允许略微溢出到晕（≤ 108%，禁止漂到圈外成一条细线）。  
3. 数值锁（写入 `moonVisual.test.ts`，禁止再靠截图口审）：

```text
strandsSpeaking(g).glass === true
strandsSpeaking(0.6).scale <= 0.62
strandsSpeaking(0.6).amplitude >= 1.45
STRANDS_THINKING.scale 仍为 1.15（thinking 不跟着改）
CSS: .companion-moon[data-visual="webgl"].state-speaking .companion-moon-strands.is-on
     transform: scale(1.55); transform-origin: center center
```

4. CSS 回退（无 WebGL）：`.companion-moon-halo-wave` 在 speaking 时 `inset` 收到与内盘同径（约 `-8%`～`0`），动画 `scale` 从 0.92 → 1.08，不再用 `inset:-92%` 的大空心圈冒充嘴。  
5. `prefers-reduced-motion`：静止填满内圆，不闪。

### 4.4 验收

| ID | 断言 |
|---|---|
| V1 | 上列数值锁全部进单测；改小嘴必红 |
| V2 | 预览页切 speaking，嘴与内圆同心，不再是偏左小纺锤 |
| V3 | idle / listening 外框与行为不变 |

### 4.5 主要文件

- `web/src/session/companion/visual/moonVisual.ts` / `moonVisual.test.ts`  
- `web/src/styles.css`  
- `web/src/session/companion/MoonSphere.tsx`（不改状态机，只消费新参数）

---

## 5. 问题 5 · 专家只保留对话上挂载

### 5.1 用户判断

两处挂载（左侧「本阶段专家」+ 输入框 chip）不同步；删对话里的，左侧还在。用户问：**是不是只留对话上挂就行。**

**锁死：是。对话输入框是唯一写入口。**

左侧不再提供「＋ 添加专家 / × 移除」。改为只读：「本阶段将使用」——与输入框同一份 `session.experts.get`。

### 5.2 根因（三层）

1. **ID 空间错了。** `PhaseExpertsBar` 的 `<option value={e.id}>` 用的是 `ppt-expert` 这类 catalog slug。`handleSessionExpertsSet` 要求 **canonical ULID**，于是红字 `session.experts.set 参数无效`。截图里「未挂载 0/4」+ 输入框已有「AI 工程师」，就是这次失败后侧栏回滚、对话 chip 仍是本地 state。  
2. **两套 React state。** 侧栏 `ids` 与 `SessionPage.referencedExperts` 各 load 一次，没有订阅。`persistMounted` 失败还 `.catch(()=>{})` 吞掉。  
3. **产品语义重复。** 阶段推荐本应是「播种」，不是第二套挂载台账。`applySessionPhaseExperts` 已按「已有 mounts 则不覆盖」写过，但 UI 又给了第二支笔。

### 5.3 方案（锁死）

**单一事实源：** `session.experts.get` / `session.experts.set`，payload 只含已安装专家的 **ULID**，最多 8（桥上限；工作台展示仍可显示 4 个推荐位）。

对话输入框（已有 chip + `@` / 专家菜单）：

- 增删只走 `persistMounted`。  
- set 失败必须把 chip 回滚并显示错误（禁止静默）。  
- catalog slug 必须先 `expert.list` → `findInstalledExpert` 再 set。

左侧「本阶段专家」改为只读条 `PhaseExpertsMirror`：

- 文案：`本阶段将使用` + chip（无 ×）。空态：`未挂载 · 在下方输入框添加`。  
- 「推荐」：把 `resolvePhaseExpertIds` 的 ULID **写入同一 session.experts.set**，成功后输入框 chip 立即变。  
- 不再渲染 `<select>` 添加。  
- 订阅：`sessionId` 变化或父级在 set 成功后传入 `revision`，mirror 重新 get。

阶段切换：继续用现有 `applySessionPhaseExperts`（有则不覆盖）。不引入第二张「阶段专家」表。

### 5.4 验收

| ID | 断言 |
|---|---|
| E1 | 输入框去掉「AI 工程师」后，左侧 mirror 同步为空（同一次 get） |
| E2 | 左侧不再出现「添加专家」select；点「推荐」后两侧 chip 一致且均为 ULID |
| E3 | 用 slug 调 set 的路径从 UI 消失；单测 `resolveInstalledExpertIds` 把 slug 译成 ULID，译不了则跳过并提示「专家未安装」 |
| E4 | 不再出现 `session.experts.set 参数无效`（slug 被拦在客户端） |
| E5 | 个人聊天（无阶段栏）行为不变 |

### 5.5 主要文件

- `web/src/project/PhaseExpertsBar.tsx` → 只读 mirror（或替换为 `PhaseExpertsMirror.tsx`）  
- `web/src/session/SessionPage.tsx`（失败回滚、暴露 revision）  
- `web/src/expert/expertIds.ts`（slug→ULID）  
- `web/src/project/phaseExperts.ts` / `phaseExperts.test.ts`  
- `web/src/session/SessionPage.conversationExperts.test.tsx`  
- `web/src/project/ProjectWorkbenchShell.test.tsx`

---

## 6. 问题 6 · 项目管理中间栏抖动 + 决策丢失 + ENGINE_UNAVAILABLE

本项是同一条路径上的两个症状，必须一起修，否则只修滚动用户仍会丢选择。

### 6.1 抖动

**现象：** 一开始思考、或上下拉动，中间栏就乱跳。

**根因：**

- `schedulePin` 的 effect 依赖 `thinkingOpen` / `toolActivities` / `chatStatus`；思考 token 增高时 `ResizeObserver` 再 pin 一次。  
- `pinConversationScroll` 注释写了「Thinking tokens must not call this」，但 RO + effect 仍会在思考增高时改 `scrollTop`。  
- 项目管理 `.project-chat-panel .message-list{flex:none}`，思考面板没有内部滚动上限，整列被顶高，再被 pin 拽回底部 → 抖动。  
- 用户上拉后，思考继续增高会改变 `scrollHeight`，`applyConversationUserScroll` 的 near-bottom 判定抖动，跟随时开时关。

**锁死方案：**

1. `pinIfFollowing` **忽略思考高度**：思考区 `contain:strict` + `max-height: 28vh` + 内部 `overflow-y:auto`；外层 RO 不因思考区子树触发 pin（RO 只观察 message-list 与 live assistant bubble，不观察 `.thinking-panel`）。  
2. effect 依赖去掉 `thinkingOpen`；思考 token **禁止** `schedulePin`。  
3. 用户一旦 `deltaY < 0` 或离开尾部 4px：锁定 `userFollowPaused`，直到真正贴底（`STREAM_RESUME_BOTTOM_PX`）才恢复。思考增高不得解除暂停。  
4. 「回到底部」仍走 `scrollBottom`，且是唯一主动平滑滚动。  
5. 项目管理与个人聊天共用同一套 `streamScroll.ts`，不写第二套。

### 6.2 决策提交后稿丢 + ENGINE_UNAVAILABLE

**现象：** 弹出选择 → 选完 → toast「核心引擎暂时不可用 代码 ENGINE_UNAVAILABLE」→ 选择没有进对话框，也没有后续思考。

**根因：**

- `UserAskWizard` 把答案格式化成 `【决策提交】…` 后交给 `submitUserAsk(followUp)`。  
- `submitUserAsk` 对工具决策走 `decideTool(..., followUp)`，否则 `sendAndChat(..., followUp)`——**`promptOverride` 不会 `setText`**。  
- `sendAndChat` 在 `chat.start` 失败时进 catch：置 `failed`、toast，**composer 仍为空**（override 从未写入）。  
- Wizard 是否消失取决于 `askTool` 是否被 `decideTool` 改掉；用户感知是「选完就没了」。  
- 对话里看到的 `ENGINE_UNAVAILABLE` **与项目管理清单、技能市场是同一条宿主故障**（见 §7），不是聊天页自己的错。产品层必须 **保稿 + 可重试**；宿主层必须 **把管道接回来**，否则重试只是再撞一次已毒死的 client。

**锁死方案（产品）：**

提交决策的顺序固定为：

```text
1. formatUserAskFollowUp → setText(followUp)     // 先看见
2. 尝试 sendAndChat(followUp)
3. 成功：清空输入框、收起 wizard（与现网一致）
4. 失败（含 ENGINE_UNAVAILABLE）：
     - 输入框保留 followUp
     - wizard 保持打开，已选项仍在
     - toast 可关，旁边「重试发送」= 再走 sendAndChat(当前输入框)
     - 禁止 decideTool 在 start 失败后把 ask 标成已完成
```

引擎侧（对话页，配合 §7）：

- `ENGINE_UNAVAILABLE` 保持 retryable。  
- `sendAndChat` 对 retryable 引擎错误 **自动重试 1 次**（500ms）；仍失败才 toast。  
- toast 文案改为：「核心引擎暂时不可用。你的选择已留在输入框，可点重试。」不再写「从最新页重试」。  
- 对话页的「重试发送」在宿主完成进程内重连后才会真正成功；§7 没落地前，点重试仍会失败。

### 6.3 验收

| ID | 断言 |
|---|---|
| J1 | 思考流 200 token：`scrollTop` 在用户上拉后变化 ≤ 2px（单测用假 box） |
| J2 | 跟随开启时，只因 assistant delta / 新消息 pin，不因 thinkingText |
| J3 | 项目管理与个人聊天同一套 pin 测试共用 |
| D1 | 提交决策后输入框立刻出现 `【决策提交】` 全文 |
| D2 | `chat.start` reject `ENGINE_UNAVAILABLE`：输入框仍有全文，wizard 仍在，chip 仍选中 |
| D3 | 点「重试发送」再次 `chat.start`，payload 含同一 followUp |
| D4 | 成功路径仍清空输入框并收起 wizard |

### 6.4 主要文件

- `web/src/session/streamScroll.ts` / `streamScroll.test.ts`  
- `web/src/session/SessionPage.tsx`  
- `web/src/session/UserAskWizard.tsx` / `userAsk.ts`  
- `web/src/session/MarkdownMessage.tsx`（思考区高度）  
- `web/src/styles.css`（`.thinking-panel` max-height）  
- `web/src/project/ProjectWorkbenchShell.send.test.tsx`  
- `web/src/project/ProjectWorkbenchShell.send.test.tsx`

---

## 7. 问题 0 · 系统级 ENGINE_UNAVAILABLE（P0-E）

### 7.1 用户刚复现的路径（2026-09-04）

两张新截图，同一时刻、两个无关页面：

1. **项目管理清单**：`project.list` 失败。红框「核心引擎暂时不可用 · 代码 ENGINE_UNAVAILABLE · 关联 `01M1N5GKD65V8G31AX7QRME4JZ` · 可重试」，底下有「重试」。壳层（侧栏、创建按钮、筛选）还在。  
2. **技能中心 · 技能市场**：`skill.catalog.list` 失败。顶栏同一句「核心引擎暂时不可用」；中间「市场加载失败 / 核心引擎暂时不可用」。已安装 0、捆绑目录 0。

这不是 React 崩了，也不是清单或市场各自的 bug。侧栏能点，说明 **WebView 宿主活着**。所有 `Owner: engine` 的 Bridge 方法（`project.list`、`skill.catalog.list`、`chat.start`、专家挂载……）都走同一条 `gateway.caller.Call`。Call 一失败，宿主**不论真实原因**都回同一句 `ENGINE_UNAVAILABLE`。

对话里选完决策后看到的那条 toast，和这两页是**同一次管道死亡的不同表面**。

### 7.2 根因（对照现码，不是猜）

```text
Renderer  --postMessage-->  Host Gateway.Handle
                              │
                              ├─ host 方法（browser.open / 主题 / 工作区）→ 仍可用
                              └─ engine 方法 → client.Call
                                    │
                                    ├─ client.broken != nil → 立刻 error
                                    ├─ 写管道失败 / 35s 写超时 → poison()，关连接
                                    └─ 读泵断开 → poison()
                              Call error → failureFor(..., "ENGINE_UNAVAILABLE", retryable=true)
```

三层缺口叠在一起，才会「整机像崩了、点重试没用」：

**缺口 A · 毒死即终态。**  
`engineclient.Client.poison`（`client_windows.go`）把 `broken` 钉死、关掉 named pipe、所有 pending 一起失败。之后每一次 `Call` 直接返回，**没有进程内重连**。

**缺口 B · 自拉起引擎的看门狗不管管道。**  
`cmd/desktop/main.go`：

- `command != nil`（本进程拉起的引擎，日常路径）：只 `Wait()` 进程退出。RPC 毒死但 **引擎 PID 还活着**（卡死、死锁、写超时）→ **不 relaunch**。  
- `command == nil`（重连到已有引擎）：2s 轮询 `rpcBroken || !pidAlive`，才会 `--takeover`。  

`engineWatchShouldRelaunch` 测例写了「broken RPC must relaunch」，但这条逻辑**只挂在重连分支**。用户现在卡的就是日常自拉起分支。

**缺口 C · 页面重试打的是同一具尸体。**  
`ProjectPage.load` / `SkillPage.loadCatalog` 的「重试」再调一次 `project.list` / `skill.catalog.list`。Gateway 没有 `ReplaceCaller`。点重试 = 再撞一次已 poison 的 client。技能市场连 correlationId 都丢掉，只显示 `error.message`。

关联 ID `01M1N5GKD65V8G31AX7QRME4JZ` 是这次 Bridge 请求的 ULID，不是引擎 PID。取证应看 `%LocalAppData%\Lunitide\logs\host-*.log` 里第一行 `engine RPC connection poisoned: …`。

### 7.3 方案（锁死）

不新开 daemon，不改方法归属。恢复顺序固定：

```text
1. 探测：client.Broken() != nil 或 Call 返回 error
2. 进程内重连（优先，不闪窗口）：
     Connect 稳定管 \\.\pipe\lunitide-gateway-<user>
     成功 → Gateway.ReplaceCaller(newClient)
          → 壳层横幅「核心引擎已恢复」2s 后消失
          → 当前页自动再 load 一次
3. 连不上且 PID 已死：拉起 lunitide-engine.exe，再走 2
4. 连不上且 PID 仍活：沿用现有 D11 --takeover 整桌面接管
5. 2/3/4 都失败：壳层横幅保持，按钮「重试连接」；禁止假装成功
```

**宿主：**

- 自拉起与重连两条路径**共用** `engineWatchShouldRelaunch`（`rpcBroken` 必须 relaunch/reconnect）。  
- `Gateway` 增加线程安全的 `ReplaceCaller`。  
- `ENGINE_UNAVAILABLE` 的 `details` 带上 poison 原因短句（写超时 / 管道断开 / 握手拒绝），`code` 不变。  
- 不把 `ENGINE_UNAVAILABLE` 改成假成功。

**壳层 UI（`App.tsx`，所有页共用一条）：**

- 任一 Bridge 调用返回 `ENGINE_UNAVAILABLE` → 顶栏一条：「核心引擎已断开，正在自动重连…」  
- 重连成功：当前页（项目清单、技能市场、对话）自动再拉一次，不要让用户自己猜「从最新页重试」。  
- 项目管理 / 技能市场：**保留页头和工具条**，错误落在列表区；不要整页只剩红框。  
- 技能市场 `marketError` 必须带上 `code` + `correlationId`（与 `ProjectPage.ErrorBox` 对齐）。

**对话页（§6.2 已锁）：** 选择先入输入框；宿主重连成功后「重试发送」才能过。

### 7.4 验收

| ID | 断言 |
|---|---|
| G0 | 单测：自拉起路径上 `rpcBroken=true` 且 PID 仍活 → `engineWatchShouldRelaunch==true` |
| G1 | `Client` 被 poison 后，`ReplaceCaller` + 新 `Connect` 的下一次 `project.list` 成功（gateway 单测用 fake caller） |
| G2 | 前端：连续两次 `ENGINE_UNAVAILABLE` 只出现一条壳层横幅，不在每个子页再堆一条 |
| G3 | 技能市场失败态显示 code + correlationId；点壳层「重试连接」后 `catalogList` 再被调用 |
| G4 | 项目管理清单失败后工具条仍在，「重试」在 ReplaceCaller 之后能列出项目 |
| G5 | 不把 Call error 映射成 `ok:true` |

### 7.5 主要文件

- `cmd/desktop/main.go` / `watchdog.go` / `watchdog_test.go`  
- `internal/hostbridge/gateway.go` / `gateway_test.go`（`ReplaceCaller`）  
- `internal/engineclient/client_windows.go`（不改 poison 语义；重连是新 client）  
- `web/src/App.tsx`（壳层横幅）  
- `web/src/bridge/client.ts`（把 `ENGINE_UNAVAILABLE` 冒泡给壳层）  
- `web/src/project/ProjectPage.tsx`  
- `web/src/skill/SkillPage.tsx`  
- `web/src/expert/ExpertCenterPage.tsx`（同一 catalog 失败态，一并对齐）

---

## 8. 问题 7 · 第二次进月伴带出上次对话

### 8.1 用户路径

第一次进月伴可以有欢迎/空舞台。**第二次再进**（同一次启动或下次启动都算）必须是全新一轮：舞台上什么都不显示。现在会把上次没执行完的对话（用户句 + 月伴句 + 工具轨迹残留）直接铺回来。

### 8.2 根因

月伴被做成**安装期内单例**（`companionSession.ts` 注释写死：from-install-to-uninstall 一份历史）。`ensureCompanionSession` 每次都指回同一条「月伴对话」。`SessionPage` 把 `items` 当 `historySeed` 传给 `CompanionStage`，`seedCompanionCaptionRounds` 取**最后一条用户 + 最后一条助手**灌进字幕。未完成轮（工具跑到一半、失败通知、`无法执行`）也会被当成上一轮字幕复活。

这和问题 3（侧栏固定位不可删）不冲突：侧栏仍是那一条「月伴对话」；变的是**每次打开舞台的可见面**。

### 8.3 方案（锁死）

- 会话单例、标题、置顶、不可删：**不动**。  
- `CompanionStage` 每次挂载：`historySeed` **不灌字幕**；`rounds` 从空开始。  
- 不自动续跑上次未完成流、不自动点「继续上次」。  
- 引擎上下文仍可用会话历史（月伴记得你是谁）；**舞台不展示**。  
- 若用户从侧栏当普通文字会话打开「月伴对话」，仍看得到历史（那是文字面，不是语音舞台）。

### 8.4 验收

| ID | 断言 |
|---|---|
| C7-1 | 进月伴说一句再退出，第二次进舞台 `rounds.length===0`，看不见上一句 |
| C7-2 | 上次以「无法执行」收尾再进，舞台仍空，不自动续跑 |
| C7-3 | 侧栏仍只有一条置顶「月伴对话」 |
| C7-4 | 单测：`seedCompanionCaptionRounds` 不再被 mount effect 调用；或 effect 显式忽略 seed |

### 8.5 主要文件

- `web/src/session/companion/CompanionStage.tsx`（去掉/短路 seed effect）  
- `web/src/session/SessionPage.tsx`（月伴打开不传 historySeed，或不 resume）  
- `web/src/session/companion/companionText.ts` / 对应 test  
- `web/src/session/SessionPage.companion.test.tsx`

---

## 9. 问题 8 · 说完话出字慢（只改速度）

### 9.1 用户意图

说完 → **立刻、不加思考地出字** → 再朗读回答。只关心反应速度。不改人设、不改听/说引擎、不改视觉、不改工具能力。

三条 pill（听 seed-asr / 说 火山·小何 / 想 glm-4）本身不是三串并行推理；慢在**串行等待**：听写定稿 → 模型首 token → 可朗读句子 → TTS 首包。用户体感是「每次都要等他先说话」。

### 9.2 是网络还是模型？

两边都有，但**产品路径先把自己的等待拆掉**：

| 段 | 预算（已有 `voiceTiming`） | 现码真实阻塞 |
|---|---|---|
| 听写定稿 | endpoint ≤ 1500ms | 火山 seed-asr，属网络/对端；本项不改 ASR |
| 说完→首字 | ttfb ≤ 1200ms | **`companionOpeningAck` 已写单测，从未接入 stream**。舞台停在 `thinking`，要等模型首句或首个可朗读 chunk |
| 说完→首声 | firstAudio ≤ 1500ms | TTS（火山·小何）等可朗读句；用户要的是**先出字再出声**，不要等声 |

想 pill 显示 glm-4：`pickCompanionFlashModel` 在当前模型已是 LLM 时**不会改到 flash**，所以「想」就是用户选的 glm-4。工具轮还要先带齐 desktop/computer schema，TTFT 更长。这些是对端/模型上限。方案保证：**定稿后 200ms 内舞台已有可见字**，不等 glm-4，也不等 TTS。

### 9.3 方案（锁死，只动速度）

1. **接线 `companionOpeningAck`**：`chat.start` 且 `companion=true` 时，在等上游之前立刻 `EventDelta` 一句短开场（已有文案：问候→「嗨，我在呢。」；问句→「嗯，」；其它→「嗯，」/「嗯，我听到了。」）。舞台收到首 delta 立刻离 `thinking`、开始出字。  
2. **先字后声**：首 delta 就上字幕；TTS 仍按现有 headstart 等第一句完整标点。禁止「等会说话才出字」。  
3. **禁止再等思考块**：companion 已 `DisableReasoning`；不要为了完整回复再垫 thinking。  
4. 不换 ASR、不换 TTS、不改 pill 文案、不改人设长指令（人设里「1 秒内开口」已经写了，缺的是引擎真的先吐字）。

### 9.4 验收

| ID | 断言 |
|---|---|
| C8-1 | 单测：companion `chat.start` 在 mock 上游返回前已发出非空 `EventDelta`（即 ack） |
| C8-2 | 舞台：`RECOGNIZED_FINAL` 后 200ms 内 `rounds` 有助手字，状态离开纯 thinking |
| C8-3 | ack 先上字幕；TTS 可晚于字幕（允许 firstAudio > ttfb） |
| C8-4 | 闲聊轮不因本项多调工具；打开/播放轮工具集不变 |

### 9.5 主要文件

- `internal/app/chat.go` / `chat_run_stream.go`（接入 ack）  
- `internal/app/chat_companion_speech.go`（已有函数，只接线）  
- `internal/app/chat_companion_fastpath_test.go`  
- `web/src/session/companion/CompanionStage.tsx`（首 delta 即出字）  
- `web/src/session/companion/CompanionStage.headstart.test.tsx`

---

## 10. 问题 9 · 软件操作根本做不成，连说「无法执行」

### 10.1 用户澄清（锁死事实）

**不是**「工具其实成功了、文案误报失败」。  
**是**操作对用户来说没做成（软件没到前台、没法用），月伴还把「无法执行」说很多遍。

现场（打开桌面汽水）：

- 工具轨迹：`desktop.open`「完成」`opened C:\Users\mujun\Desktop\汽水音乐.lnk`  
- 接着：`computer.act`「完成」`captured foreground window 1650x1080`  
- 字幕先「无法执行」，再说「已经打开啦」  
- 系统再叠：「无法执行。这次操作没成功，请再说具体一点让我重试。」

「完成」只表示**这一次工具调用返回了**，不表示用户目标达成。

### 10.2 到底为什么无法执行（因果链）

**第一刀：打开只点火，不验收。**  
`desktop.open` → `openWithDefaultApp` → `cmd /c start "" 汽水音乐.lnk`。`Start()` 成功就回报 `opened …lnk`。不查进程在不在、窗口到没到前台、用户看不看得见。月伴全屏盖住桌面时，播放器即使起来了也在月伴后面——用户判定「没打开」。

**第二刀：引擎故意不承认「打开了」算做完。**  
`desktopTurnSettled` **不认**「已经打开了」。单测写死：

```text
already opened 记事本 → must continue
opened an app is not the user goal
```

「打开 / 汽水」还被算进 `companionWantsDesktopControl`，走 24 步桌面环。打开成功后系统强制再塞：

```text
桌面操作还没做完。根据最新截图的 frameId 继续 see→act→verify
```

**第三刀：被迫截的是月伴自己。**  
`computer.act` screenshot 默认 `target=foreground`。月伴占着前台。`captured foreground window 1650x1080` 就是月伴窗口，不是汽水音乐。模型在月伴画面上找播放器、点按钮 → 点不到、校验失败。**这才是用户说的「根本操作不成功」。**

**第四刀：「无法执行」是这条死循环的逃生口令，所以会说很多遍。**

| 层 | 谁说 | 为什么会说 |
|---|---|---|
| 人设 | 模型 | 「做不到必须说无法执行」 |
| 停环 | 模型 | `desktopTurnSettled` 把「无法执行」当成**已结算**；说完循环才停。打开成功反而停不了 |
| 结轮通知 | 引擎 | `createTurnFailureNotice`：正文已含「无法」则空通知，`ToolFailed` 再落成「这次操作没成功」；流失败再走 `turnOutcomeNotice` → 又一条「无法执行。」 |
| 舞台朗读 | 前端 | `chatStatus==='failed'` 再 `companionCannotExecuteSpeech` 读一遍 |

所以不是网络偶发，也不是桌面上找不到汽水（快捷方式已经命中）。是：**打开被设计成未完成 → 在月伴自己的窗口上点控失败 → 用「无法执行」收场，而且每层再喊一次。**

### 10.3 方案（锁死）

目标是「打开汽水」时：**软件到前台、用户能看见、月伴只说一句结果、绝不说无法执行。**

1. **打开类目标：`desktop.open` 必须验窗。** `Start()` 之后按标题/进程找到目标窗并 `SetForeground`；验到才回报成功。超时未见到窗口才回「无法执行」+ 原因（没启动 / 在月伴后面拉不起来）。  
2. **打开成功即 settle。** 「已经打开了」列入 `desktopTurnSettled`。本轮用户目标是打开/启动时，禁止 desktop continue nudge，禁止再跟 `computer.act`。  
3. **月伴在前台时，screenshot 默认不要 foreground。** 打开后要看目标软件：按窗口标题找，或 `target=desktop`；禁止把月伴窗当操作面。  
4. **「无法执行」每轮最多一句，且必须带工具原文原因。** 打开已验窗成功：模型与引擎都禁止再写「无法执行」。前端 failed 且本轮已有成功 `desktop.open`：不朗读无法执行。  
5. 只改打开/点控结算与验窗。不改闲聊人设、不改听/说引擎、不改视觉。

### 10.4 验收

| ID | 断言 |
|---|---|
| C9-1 | 单测：`shouldContinueDesktopTurn("已经打开了汽水音乐。", 0)==false`（推翻「opened an app is not the user goal」） |
| C9-2 | 单测：目标=打开、`desktop.open` 成功、无后续失败工具 → `createTurnFailureNotice` 与 `turnOutcomeNotice` 都为空 |
| C9-3 | 单测：companion 打开轮在 `desktop.open` 成功后 `pickTurnContinueKind` 不再返回 `"desktop"` |
| C9-4 | 单测：同一轮助手正文里「无法执行」最多出现一次；成功打开的回合为 0 次 |
| C9-5 | 手工：月伴里说「打开桌面汽水」→ 汽水音乐到前台；字幕只有一句结果；不说无法执行 |
| C9-6 | 手工：桌面没有该软件 → 只说一次「无法执行」+「桌面/开始菜单没找到」 |

### 10.5 主要文件

- `internal/app/chat_continue.go` / `chat_continue_test.go`（settle + 停 nudge）  
- `internal/app/chat_turn_notice.go` / `chat_office_autogen_test.go`（成功打开不落失败通知）  
- `internal/app/chat_companion_speech.go`（人设：打开成功禁止说无法执行）  
- `internal/toolruntime/desktop.go` / `runtime.go`（打开后验窗）  
- `internal/ccapp/runhost.go`（截图不要默认月伴窗）  
- `web/src/session/companion/CompanionStage.tsx`（成功打开不朗读无法执行）

---

## 11. 非目标

- 不重做会议 ASR 引擎、不改火山/本机听写协议。  
- 不新增机务 rail、不改台账 Bridge。  
- 不把月伴从单例改成可删历史；不每次进月伴新建一条会话。  
- 不新做「阶段专家」存储表。  
- 不把 ENGINE_UNAVAILABLE 改成假成功。  
- 不新开第二套 daemon、不改 named pipe 安全模型。  
- 不引入新 npm 依赖、不新开窗口路由框架。  
- **#8 不换模型、不改 TTS 音色/人设长文、不改 pill 文案。**  
- **#9 不把「完成」改成假装成功；验不到窗就必须失败，且只说一次原因。**

---

## 12. 测试与发布门槛

每个问题必须有 **失败单测先写、再改实现**（TDD）。最低集：

- Go：`./internal/hostbridge`、`./cmd/desktop` 看门狗 + `./internal/app` 月伴 continue/notice/ack + 现有 `./internal/...` 全绿。  
- Web：上表 M / R / P / V / E / J / D / **G** / **C7–C9** 全部有 vitest。  
- `npx tsc --noEmit`、`npm run verify:bridge`。  
- 不升版本、不打安装包，直到 P0-E 与体验验收勾完且用户点头。

手工 15 分钟（实施后）：

0. **引擎：** 打开项目管理看到清单；若故意断开引擎，顶栏出横幅，恢复后清单和技能市场自动回来，不必关应用。  
1. 会议：录 20s → 开设置改无关项 → 关 → 稿还在。  
2. 机务：展开航材看到计划；展开机务维修看到排故。  
3. 侧栏：月伴三点无删除；普通对话能删。  
4. 月伴：说话时嘴填满内圆。  
5. 项目：输入框摘掉专家，左侧跟着空；推荐后两侧一致。  
6. 项目：思考时上拉不跳；提交决策失败后输入框仍有选择，重连后重试能继续。  
7. **月伴：进 → 说一句 → 退 → 再进，舞台空白。**  
8. **月伴：说完立刻出字，再听到朗读。**  
9. **月伴：打开桌面汽水，软件到前台，只说一句结果，不说无法执行。**

---

## 13. 评分（整改方案本身）

评分标准：根因是否钉到文件、方案是否唯一、有无验收 ID、有无非目标、能否按文件开工。满分 5。

| 维度 | 分 | 说明 |
|---|---:|---|
| 根因可复现 | 1.0 | 十条都对到具体函数；#9 对到 settle 单测 + foreground 截月伴 |
| 方案唯一且可落地 | 0.98 | 打开验窗 + 打开即 settle；ack 接线；舞台不灌 seed |
| 验收可测试 | 0.95 | G0–G5 + C7–C9 + 原 M/R/P/V/E/J/D |
| 范围克制 | 1.0 | #8 只动速度；#9 只动打开结算；无新 daemon |
| 实施顺序 | 0.97 | P0-E 仍最前，月伴三病进 P0 |
| **方案分** | **4.9** | 用户澄清「根本没做成」后，#9 从误报改写成死循环根因 |

落地后用户体验目标综合 **4.9**。剩余 0.1 = WebGL 机器差 + 引擎进程被杀且 takeover 也失败 + 云端 TTFT 本身的上限。

---

## 14. 实施时禁止再打开的分叉

写代码时不要回头讨论这些（已在上文锁死）：

| 题 | 不采用 | 采用 |
|---|---|---|
| 0 | 只改文案；或点重试再撞已死 client；或新开 daemon | 进程内 ReplaceCaller；看门狗两条路径都认 rpcBroken |
| 1 | 录制中禁用设置；或整页跳转后再从 SQLite 猜稿 | Overlay，页不卸载 |
| 2 | 只加两个组、其余仍扁平 | 五级 IA 一次做完 |
| 3 | 隐藏整个三点；或后端禁止 delete | UI 去掉删除，后端契约不动 |
| 4 | 只加大 vmin 外框 | 填满内圆 + 数值锁 |
| 5 | 两处互相同步的双写 | 只留对话写；左侧只读 |
| 6 | 失败只 toast；或自动重进「最新页」 | 先入输入框，失败保留，一键重试 |
| 7 | 每次进月伴新建会话；或清空 SQLite 历史 | 单例保留；舞台不灌 seed |
| 8 | 换模型 / 改 ASR / 等 TTS 再出字 | 接线 ack，先字后声 |
| 9 | 把轨迹「完成」当成功；或只改「无法执行」文案 | 打开验窗；打开即 settle；无法执行每轮一句 |
