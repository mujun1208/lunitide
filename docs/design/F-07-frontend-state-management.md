# F-07 · 前端状态管理升级迁移计划

> **文档性质：迁移计划文档（Migration Plan），非实施。** 本文评估 Zustand vs Jotai、给出选型与分阶段迁移计划。
> **实施归属：阶段四 A-02。** 本轮不改任何 `.ts`/`.tsx`、不新增依赖、不动 `bridge/client.ts`。
> **验收目标：** PRD F-07「评估 Zustand/Jotai 引入方案，先出迁移计划文档」，评分 3.0 → 4.0。

---

## 1. 现状（真实证据）

调查对象：`web/src/App.tsx` 及 `web/src/bridge/client.ts`。统计用 PowerShell（`Select-String` 计数）。

### 1.1 规模证据

| 指标 | 实测值 | 采集方式 |
|------|--------|----------|
| `App.tsx` 行数 | **151** | `(Get-Content App.tsx).Count` |
| `useState` 出现次数 | **7 处调用点**（多为一行内解构多个 state） | `Select-String 'useState'` |
| `useReducer` | **0** | `Select-String 'useReducer'` |
| `useContext` | **0**（仅 `LanguageProvider` 一个 Context，见 `App.tsx:10,108`） | `Select-String 'useContext'` |
| 状态库依赖 | **无**（`package.json` 仅 `react ^19.1.1`、`react-dom ^19.1.1`，无 zustand/jotai/redux/valtio/recoil） | `web/package.json` |

`App.tsx:1` 显式只从 react 引入 `useEffect,useMemo,useRef,useState` —— **零状态管理库**，全部靠组件本地 `useState` + props 传递。

### 1.2 顶层 state 群（`App` 组件，`App.tsx:76-77`）

单行解构，共约 **21 个顶层 state**（真实变量名）：

`page`、`target`、`draftKey`、`drawer`、`sidebarCollapsed`、`settingsCategory`、`modelManagerOpen`、`providersRevision`、`localChats`、`deletedChatIds`、`draftSessionIds`、`peopleFocus`、`peoplePeerId`、`peoplePeerName`、`catalogFocus`、`mroEnabled`、`mroExpertId`、`opsExpertIds`、`mroInitialRail`（`App.tsx:76`）；`theme`、`language`、`companionNotice`（`App.tsx:77`）。

`LaunchSidebar` 另有约 15 个本地 state（`App.tsx:114`：`recent`/`menuId`/`renameTarget`/`searchOpen`/`searchQuery`/`searchHits`/`identity`/`peopleUnread` 等），`LaunchHome` 另有约 11 个（`App.tsx:138`：`text`/`providerItems`/`provider`/`model`/`mode`/`menu`/`files`/`dragging` 等）。

### 1.3 痛点（真实代码证据）

| 痛点 | 证据 |
|------|------|
| **Props drilling** | `sidebarProps` 对象（`App.tsx:107`）打包 ~18 个字段透传给 `LaunchSidebar`（`App.tsx:108,110,113`）；`bridge` 单例（`projects`/`sessions`/`messages`/…）从 `App` 一路作 prop 传到 `LaunchSidebar`/`LaunchHome`/`SessionPage`/`MroWorkbenchPage`（`App.tsx:108-110,137,150`） |
| **巨型 prop 列表** | `MroWorkbenchPage`（`App.tsx:110`）单个 JSX 元素传入 **50+ 个 `on*`/`*List` 回调**（`onUpsertAircraft`/`onBindStock`/`onRegisterManual`… 全在一行）——典型的 drilling 反模式 |
| **跨页共享 state** | `target`（当前对话）、`theme`、`language`、`mroEnabled`/`opsExpertIds`（专家开关）被首页/侧栏/会话页/设置页多处读写；`theme` 还与 `nativeTheme.set`（`App.tsx:88`）、localStorage（`THEME_KEY` `App.tsx:55`）耦合 |
| **重渲染面** | 任一顶层 state（如 `menuId`、`searchQuery`）变化触发整个 `App`/`LaunchSidebar` 子树重渲染；`sidebarProps`（`App.tsx:107`）每次渲染都新建对象，破坏子组件 memo |
| **bridge 数据进组件方式** | 通过 `useEffect` 逐组件拉取（`App.tsx:92` 拉 experts、`App.tsx:121` 拉 sessions、`App.tsx:125` 拉 identity/people、`App.tsx:141` 拉 providers），各自维护本地 loading/error/alive 标志，逻辑重复且无共享缓存 |

---

## 2. 选型对比：Zustand vs Jotai

| 维度 | Zustand | Jotai |
|------|---------|-------|
| 心智模型 | 单一/多个 **store**（集中式，类 Redux 但无样板） | 原子 **atom**（自下而上，类 Recoil） |
| 体积（min+gzip 量级） | ~1.2 KB | ~3–4 KB（含 utils） |
| 读取粒度 | selector 订阅，`useStore(s=>s.x)` 精确重渲染 | atom 粒度天然精确 |
| 与「命令式 bridge 单例」契合 | **高**：store 可直接持有 `bridge/client.ts` 单例并在 action 内调用 `sessions.list()` 等，把 §1.3 分散的 `useEffect` 收敛为 store action | 中：atom 更偏声明式派生，命令式副作用需 `atomWithObservable`/额外封装 |
| React 外调用 | `store.getState()`/`setState` 可在组件外（如 bridge 事件回调、`subscribeLiveChatRegistry` `App.tsx:124`）直接更新 | 需 `store` 实例或 Provider，组件外更新更绕 |
| TS 支持 | 优秀，`create<State>()` 全推导 | 优秀，atom 类型自动推导 |
| 迁移成本 | 低：可与现有 `useState` **并存增量迁移**，逐个 state 搬入 store | 中：atom 化改动更细碎，21 个 state 逐一 atom 化 |
| 现有代码适配 | `target`/`theme` 等「一处写多处读」的共享状态直接映射为 store 字段 | 同样可行但需为每个 state 建 atom + 组合 |

### 推荐：**Zustand**

理由：
1. **与 bridge 单例范式天然契合**——`web/src/bridge/client.ts` 已导出 `projectBridge`/`sessionBridge`/`messageBridge`/`providerBridge`/`attachmentBridge`/`uiThemeBridge` 等命令式单例（见 `client.ts:289/300/310/322/348`），Zustand 的 action 可直接把 §1.3 中散落各组件的 `useEffect` 拉取逻辑收编进 store，形成「store 调 bridge、组件订阅 store」的单向数据流。
2. **组件外更新**——`subscribeLiveChatRegistry`（`App.tsx:124`）、bridge 事件、`setInterval` 轮询（`App.tsx:125`）等场景需要在 React 生命周期外改状态，Zustand `setState`/`getState` 无 Provider 依赖，比 Jotai 顺手。
3. **增量迁移友好**——可与现存 `useState` 共存，按 §3 逐 store 迁移，不需一次性重写 21 个 state。
4. 体积最小，对桌面壳内嵌 web 的启动无明显负担。

---

## 3. 分阶段迁移计划（每步不破坏现有组件）

关键不变量：**组件对外 props 契约与 `bridge/client.ts` 单例导出不变**；每步只把一组 state 从 `useState` 搬进 store，组件改为 `useXxxStore(selector)` 读取，UI 行为逐步等价。

| 阶段 | 抽取的 store | 迁移的现状 state（`App.tsx`） | 收益 & 不破坏保证 |
|------|--------------|-------------------------------|---------------------|
| **A-02.1 themeStore** | `theme`、`language` + `toggleTheme`/`setLanguage` | `theme`/`language`（`App.tsx:77`），含 `THEME_KEY`/`LANGUAGE_KEY` localStorage 与 `nativeTheme.set`（`App.tsx:88-89`）副作用 | 最小、最独立、跨全应用共享；`LanguageProvider`（`App.tsx:108`）保留，store 作为其数据源 |
| **A-02.2 targetStore（导航/当前对话）** | `page`、`target`、`drawer`、`sidebarCollapsed`、`settingsCategory`、`catalogFocus` | `App.tsx:76` 对应字段 | 消除 `page`/`target` 在三处 return 分支（`App.tsx:108/109/110`）的透传；`fresh()`（`App.tsx:93`）改为 store action |
| **A-02.3 sessionStore（对话列表）** | `localChats`、`deletedChatIds`、`draftSessionIds` + `registerChat`/`handleSessionDeleted`/`setChatActivity` | `App.tsx:76`，逻辑现散在 `App.tsx:94-97,121-122` | 消灭 `sidebarProps`（`App.tsx:107`）大对象透传；侧栏拉取 `useEffect`（`App.tsx:121`）收编进 store action |
| **A-02.4 mroStore（专家/MRO 开关）** | `mroEnabled`、`mroExpertId`、`opsExpertIds`、`mroInitialRail` | `App.tsx:76`；数据源自 `experts.list` `useEffect`（`App.tsx:92`） | 把 experts 拉取收编；`MroWorkbenchPage`（`App.tsx:110`）50+ 回调可逐步改为组件内直接订阅 store + 调 `mroBridge`，大幅削减 drilling |
| **A-02.5 局部 store（可选）** | `LaunchSidebar` 搜索态（`searchOpen`/`searchQuery`/`searchHits`，`App.tsx:114`）、`LaunchHome` 草稿态（`App.tsx:138`） | 对应本地 state | 仅当确有跨组件共享需求才上；否则保留本地 `useState`，避免过度 store 化 |

每步收尾：`npm run build`/`tsc --noEmit` + 现有前端测试全绿；人工验证对应页交互（切主题/切页/新建对话/MRO 开关）等价。

---

## 4. 风险与回滚

| 风险 | 缓解 |
|------|------|
| store 与 localStorage/`nativeTheme` 副作用双写不一致 | themeStore 作为唯一写入点，副作用集中在 store 的 subscribe 或 action 内（收编 `App.tsx:88`） |
| 组件外更新（bridge 事件/轮询）与 React 渲染竞态 | Zustand `setState` 天然线程内串行；`alive` 标志（`App.tsx:92,121,125`）逻辑迁入 store 时保留 |
| 增量期 store 与残留 `useState` 双源真相 | 每个 state 一次只属一处：迁入 store 后立即删本地 `useState`，禁止双写 |
| 过度迁移（本不需共享的本地态） | A-02.5 明确标记「可选/按需」，纯本地 UI 态（如 `menuId` `dragging`）留在组件内 |
| **回滚** | 每个 A-02.x 是独立 PR；store 与旧 `useState` 在阶段内可共存，回滚即 revert 单个 PR，不影响其它 store |

---

## 5. 验收勾稽（映射回 PRD F-07）

| PRD F-07 要求 | 本文对应 |
|----------------|----------|
| 评估 Zustand/Jotai 引入方案 | §2 六维对比表 |
| 先出迁移计划文档（非立即实施） | 本文即迁移计划，§0 标注实施属阶段四 A-02 |
| 现状痛点（props drilling / 跨页共享 / 重渲染） | §1.3 四类痛点 + 真实行号证据 |
| 推荐选型与理由 | §2「推荐 Zustand」四点理由，落到 bridge 单例契合 |
| 分阶段迁移（先抽 session/target/theme/mro 等） | §3 五阶段：theme→target→session→mro→局部 |
| 每步不破坏现有组件 | §3 不变量：props 契约与 bridge 导出冻结，每步等价验证 |
| 风险与回滚 | §4 |

**实施归属：阶段四 A-02。本文不含任何代码变更、不新增依赖。**