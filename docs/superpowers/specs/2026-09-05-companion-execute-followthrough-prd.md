# 月伴可落地 PRD（第二种「星尘」模式 + 执行收口）

日期：2026-09-05  
版本：定稿重写。第一部分=第二种月伴视觉「星尘」（仿真已锁，进产品可切换）；第二部分=指令执行收口（编号 §0–§10 语义不变，计划不断链）。  
状态：规格已自检。**星尘画面已由用户点头定稿**（对照独立仿真 `npm run preview:companion-particle`）。第一部分下一步是把仿真挂进月伴舞台，不是再改画面。第二部分未获单独开工令前，不把执行改动和视觉改动混进同一提交。  
产品：月汐（Lunitide）Go Engine + React WebView2 + SQLite  
不改：`VERSION` / 现网安装包号（文档写稿时为 `v0.4.63`）。落地后另开版本。

**本文是月伴本波唯一事实源。** 前稿同路径文件作废，以下文为准。两轨独立：

| 轨 | 做什么 | 不做什么 |
|---|---|---|
| 第一部分 · 星尘 | 把已定稿仿真做成第二种月伴模式；玉盘 / 星尘可切换 | 不改玉盘像素；不重画星尘配方；不接执行收口 |
| 第二部分 · 执行 | 说了稍等必须跑完；火山认全句、可插话；打字=语音桌面手 | 不改 `MoonSphere` / `Aurora` / 星尘着色器 |

执行轨计划仍是 [../plans/2026-09-05-companion-execute-followthrough.md](../plans/2026-09-05-companion-execute-followthrough.md)，只覆盖第二部分。星尘进产品另开任务，按本文第一部分派工。

用户四条执行锁（2026-09-05）只约束第二部分：

1. 凡是要执行的指令，说了稍等就必须跑完再口播结果，不许空等。天气只是样例。
2. 火山：月汐说话中要能**临时插话打断**。云端 / 本机仍只按钮。
3. 火山：一句要认全；换气不得只定稿半句。话没说完不得抢答。根因是等待窗太短（400ms → 1200ms）。
4. **打字聊天的桌面执行能力，语音必须同等。** 不是两套手。

沿用冻结（两轨都守）：

- 不新 daemon；内核 SQLite。
- 不改 poison / 月伴 TTS 音色 / 专家表。
- `desktop.open` 成功 = 前置后前台命中。
- 电脑控制不自开；保护会话「月伴对话」不可删、不可改名。
- 不假成功 `ENGINE_UNAVAILABLE`。
- 不重写 `AnnotateCapture` / `assignNodeIDs`。
- 不接线 `companionOpeningAck`。
- **现网玉盘（`MoonSphere` / `Orb` / `Strands` / `Aurora` / `CompanionVisualPreview`）像素冻结。** 星尘是平行皮肤，不是改旧月亮。
- 星尘画面本身也冻结：以 `web/src/session/companion/particle/` 现码与本文 V2 数字为准，进产品只接线，不「顺便再调好看一点」。

「说完再答」冻结收窄为第二部分 §3。云端 / 本机 cascade 仍说完再答。

---

# 第一部分 · 第二种月伴模式「星尘」

## V0. 这一轨现在做到哪

独立仿真已经按用户逐轮改到定稿，**不是概念稿**：

| 项 | 现状 |
|---|---|
| 预览页 | `web/companion-particle-preview.html` + `npm run preview:companion-particle` |
| 渲染 | `ParticleMoonScene.tsx`（OGL / WebGL2） |
| 配方 | `particleMoon.ts` + `particleGeometry.ts` |
| 回归 | `npx vitest run src/session/companion/particle` 已绿 |
| 产品舞台 | `CompanionStage` 仍只挂玉盘：`Aurora` + `MoonSphere` |
| 设置 | `CompanionSettings` **没有** `visualSkin` |

用户原话（定稿）：把仿真「作为 PRD 的第一部分，做成第二种月伴模式，可以切换成这个效果」。

因此第一部分的产品目标不再是「先看效果再决定画什么」，而是：

> 月伴全屏舞台有两套脸：**玉盘**（现网，默认）和 **星尘**（本仿真原样）。用户能用按钮和语音来回切；切完记住；听写 / 模型 / 打断 / 字幕契约与玉盘相同。

贾维斯整仓、Three.js、R3F、MediaPipe、Tone.js、手势、新 WebSocket / 新 daemon：**否决，不再讨论。** 只许读粒子态机思路，不许当壳。

---

## V1. 产品锁（进系统后必须成立）

1. **两套皮肤，默认玉盘。**  
   `companionSettings.visualSkin = 'classic' | 'particle'`，缺省 `'classic'`。非法值、缺字段、旧存档一律回 `'classic'`。新用户第一次进月伴仍是玉盘，不会被星尘吓一跳。

2. **玉盘零像素改动。**  
   禁止改：`MoonSphere.tsx`、`visual/Orb.tsx`、`visual/Strands.tsx`、`visual/Aurora.tsx`、`visual/moonVisual.ts`、`CompanionVisualPreview.tsx`、玉盘相关 CSS 选择器的观感。现网预览页 `preview:companion` 继续只回归玉盘。

3. **星尘零配方改动。**  
   进产品时把仿真挂上去。禁止改 `RINGS`、着色器常量、`engageMorph` 时间轴、粒子预算、相机、行星半径，除非用户再点头。预览页留下来当对照，不删。

4. **切换三路，结果同一份设置。**

   | 入口 | 位置 | 行为 |
   |---|---|---|
   | 舞台按钮 | `CompanionStage` 顶栏 chrome，退出 / 打断旁边 | 两段开关「玉盘 \| 星尘」，当前态 `aria-pressed` |
   | 设置 | 设置 → 语音与麦克风，紧挨「启用月伴对话」 | 同一两段开关；改完立刻写盘，再进月伴就是这套 |
   | 语音 | 听写定稿进入 `beginUserTurn` **之前** | 整句命中口令表则拦截，不进引擎 |

5. **语音切皮肤不是用户回合。**  
   不 `chat.start`、不挂工具、不 `message.append`、不改会话标题、不写入字幕历史当问答。月汐用当前音色口播一句短确认，然后继续听。已经是目标皮肤时也口播「已经是星尘了。」/「已经是玉盘了。」，仍不进引擎。

6. **星尘只吃玉盘同一根状态线，不吃玉盘的点击 props。**  
   `ParticleMoonScene` 保持仿真契约：`state` / `gain` / `levels` / `high` / `onFps`。`state` 必须是 `companionSurfaceState(...)` 的结果，禁止星尘自己猜「在执行所以变思考」。`levels` 继续传入，**不得**再画频谱环（定稿没有）。`enter` / `interruptible` / `onInterrupt` **不要**加进 Scene——点击走覆盖热区，不进 WebGL。

7. **点月亮仍打断。**  
   星尘 canvas `aria-hidden`。**禁止**在星尘态再挂 `MoonSphere`（会再开一套 Orb/Strands WebGL，叠出玉盘脸）。热区是独立的透明 `.companion-moon` + `.companion-moon-body`，标签表与玉盘相同（`月亮：轻点开始说话` / `月亮正在聆听` / `月亮正在回应` / `月亮正在说话，点击打断朗读`）。星尘态必须用 CSS 藏掉 `.companion-moon-disc` / `.companion-moon-halo` / `.companion-moon-halo-wave`，否则会在彩球上叠一层白玉盘。点热区契约与现网 `MoonSphere` 的 `onInterrupt` 分支原样。

8. **Stage 只加分支，不重画玉盘 DOM。**  
   `data-state`、字幕、灯、退出/打断/暂停、横幅、批准卡、a11y 骨架（`CompanionStage.a11y.test.tsx`）保持。允许：根节点加 `data-skin="classic|particle"`；aurora 槽按皮肤二选一；月亮槽在星尘时改挂粒子场景 + 透明热区。禁止借机改字幕布局、加输入条、加麦克风大按钮。

9. **性能。**  
   WebView2 + 核显目标 60fps 量级。高档 `moon 20000 + nebula 22000 + stars 5600`；低档 `9000 + 9000 + 2400`。连续约 0.5s 窗口里 FPS&lt;40 满 8 次，自动降到低档，本访不再升回去（与仿真一致）。禁止再把预算加到几万以上。

10. **降级。**  
    `!canUseCompanionWebgl()` 或 `prefersCompanionStillVisual()`：星尘画静态彩晕圆（仿真已有 `.stardust-sim-disc`），禁止黑屏、禁止转、禁止形态切换。玉盘自己的 CSS 月亮路径不动。

11. **不改身份。**  
    不改 poison、TTS 音色、专家表、VERSION、听写卡、`useCompanionMachine` 转移表。

---

## V2. 视觉定稿（对照仿真，禁止再脑补）

本节数字全部来自 2026-09-05 用户点头的仿真。实现对照：`ParticleMoonScene.tsx`、`particleMoon.ts`、`particleGeometry.ts`。验收时以肉眼对照预览页为准；单测锁下面标了「测」的不变量。

### V2.1 构图（用户要看见什么）

全屏一张漆黑星空。中间一颗**嵌在星河圈里**的彩色粒子球（月伴本体）。球外面是七层不规则星河环，青 → 紫 → 酒红，慢速 **3D 环绕**（不是平面转盘）。更远处一颗**实体**小行星带着淡光晕环慢慢绕。底上有很淡的紫烟 / 青纱 / 白雾，不能把黑夜洗成紫灰墙。

禁止：仪轨刻度、假代码雨、钢铁侠红金、白色主粒子、粒子假行星、空心科技线框球。

### V2.2 相机与层

| 项 | 锁 |
|---|---|
| `clearColor` | `(0, 0, 0, 1)` |
| FOV | `40` |
| 相机位 | `(0, 2.15, 5.5)` 看向原点；随时间 `sin(t)*0.18` 微移，加鼠标视差 |
| 画序 | 1 全屏夜空幕 → 2 实体行星（开深度）→ 3 粒子月 → 4 星河/远星（加法，后画才能包住月） |
| 混合 | 粒子月、星河、光晕环：`SRC_ALPHA, ONE` |

### V2.3 夜空幕（`AURORA_FRAG`）

漆黑底 + 稀疏闪星 + **很淡**的体积纱。不是现网玉盘那条亮极光。

| 层 | 锁 |
|---|---|
| 闪星 | 网格 `260×148`，命中阈值 `0.9964`，冷白微闪 |
| 河床 | `mix((0.04,0.03,0.1),(0.03,0.08,0.16)) * wash`，`wash` 很弱（`*0.04` 再 `*0.35`） |
| 紫烟 | `purpleVeil * (0.24 + uAura*0.08)`，色 `(0.72,0.1,0.58)↔(0.28,0.06,0.52)` |
| 青纱 | cyan `* 0.16` |
| 白雾 | fog `* 0.12` |

云雾再加浓、背景再洗亮 = 违约。

### V2.4 星河圈（`buildNebulaField` + `NEBULA_VERT`）

七层不规则环，半径从里到外（测：尘点最近半径 ∈ (0.55, 1.1)，且 ≥70% 尘点落在下列带 ±0.55 内）：

| # | r | 色调 |
|---|---:|---|
| 1 | 0.98 | 电青 `[0.40, 0.98, 1.00]` |
| 2 | 1.48 | 蓝 `[0.32, 0.50, 1.00]` |
| 3 | 2.05 | 紫 `[0.52, 0.24, 0.96]` |
| 4 | 2.68 | 品红紫 `[0.72, 0.16, 0.88]` |
| 5 | 3.38 | 酒红 `[0.78, 0.12, 0.42]` |
| 6 | 4.12 | 深酒红 `[0.72, 0.08, 0.24]` |
| 7 | 4.88 | 暗酒红 `[0.58, 0.06, 0.18]` |

运动（要 3D 环绕，不要扁饼自转）：

- 先 `rotateX(0.66 + sin(t*0.055+ring)*0.07)`
- 再 `rotateY(t * orbit)`，`orbit ∈ [0.014, 0.024]`，远星更慢
- 再 `rotateZ(sin(t*0.04+ring)*0.06)`
- 轻微上下呼吸 `sin(t*0.16)*0.025`

远星：彩色（青 / 品红 / 金 / 绿），不是白点；分布在更大球壳上。

### V2.5 实体小行星

| 项 | 锁 |
|---|---|
| 几何 | OGL `Sphere`，半径 `0.16`，48×32 |
| 材质 | 实体漫反射 + 高光 + 冷蓝边缘光；**不是粒子球** |
| 光晕 | `buildHaloRing(0.17, 0.26)` 盘，倾斜 `0.72`，加法，alpha 约 `0.4` |
| 轨道 | 半径约 `3.55`，偏下、偏后，`y` 微摆；自转很慢 |

### V2.6 中心粒子月

每个粒子预计算五个休息位：球 / 五瓣莲 / 环面 / 双螺旋 / 星刺。GPU 里 `morphTo` 插值，禁止瞬移。

颜色（测：2400 点色相桶 &gt; 8）：

- 顶点 `color.x` 是种子色相，不是最终 RGB。
- 片元：`hsv2rgb(hue, 0.96, 0.88)`，`hue` 随时间慢漂。
- **必须是亮彩色霓虹，禁止改回白点。**
- 点大小约 `1.8–6.4`；核 `exp(-d*9.2)` + 晕 `exp(-d*2.2)*0.48`，要比早期稿更亮、更认得出是一颗球。

嵌套感：粒子月与星河共用同一套 `nestTilt` / 慢 `rotateY` / 微 `rotateZ`，看起来坐在圈里，不是浮在圈前的另一套世界。

### V2.7 四态（机器四态，画面三条）

状态机不改：`idle | listening | thinking | speaking`。  
**聆听和思考画面合并成一条联动**（`isEngageState`）。状态灯文案仍可写「聆听」「思考」，但粒子月走同一时间轴。工具执行中现网已映射成 thinking，星尘自动吃到同一条联动。

| 产品态 | 画面 | 关键数字（测） |
|---|---|---|
| **空闲** | 稍散的慢转彩球。这是用户锁定的「原来聆听那样」：球不要太大，相对分散，自转慢。 | `radius=0.52`，`scatter=1.34`，`form=0.58`，周期 `36s` |
| **聆听 / 思考** | 从当前形先**收成紧球**，停一下，再按生命形态来回变：莲花 → 环面 → 螺旋 → 星刺 → 再折回。全程继续慢 3D 自转。 | 见 V2.8 |
| **说话** | 从当前形态**展开回球**，球比空闲略大，核随 `gain` 一张一缩（跟 TTS 音量）。 | `radius=0.58`，`form=0.78`，`pulse=gain`，`coreScale=0.28+gain*0.72`，周期 `22s` |

增益：只在 speaking 驱动 `pulse`。聆听的 `levels[12]` 预览里有滑条，产品里可以接着传，但**不得**再做成一圈频谱环（定稿画面没有）。

### V2.8 聆听 / 思考时间轴（`engageMorph`）

进入聆听或思考（从非 engage 态进来）时：`thinkAge=0`，`fromShape=上一形态`。聆听↔思考互切 **不重置** 时间轴。

| 段 | 时长 | 形态 | 半径 |
|---|---:|---|---|
| 收紧 | `THINK_SHRINK_SEC = 1.15` | 上一形 → 球 | `0.50 → 0.30`（线性于 u） |
| 停住 | `THINK_HOLD_SEC = 0.70` | 球 | `0.28` |
| 每一形态槽 | `THINK_SLOT_SEC = 2.60`（其中先持住 `0.70` 再变 `1.90`） | `ENGAGE_SHAPES = [莲, 环, 螺旋, 星]` ping-pong | `0.38` |

缓动：`morphEase` = smootherstep。形态切换用 `morphTo`（方向插值 + 正弦抬升），禁止 lerp 穿心。

离开到说话：`speakFrom=当前形`，`speakMix` 从 0 ease 到 1，落到球。

空闲：落到球，半径 0.52。

### V2.9 粒子预算

```
high: { moon: 20000, nebula: 22000, stars: 5600 }
low:  { moon:  9000, nebula:  9000, stars: 2400 }
```

仿真里的自动降档逻辑进产品时原样保留。

### V2.10 减动效 / 无 WebGL2

静态彩晕圆，文案仍可切皮肤。禁止转、散、变形。jsdom 测试不得因 WebGL 缺失崩。

---

## V3. 进产品落点（可直接派工）

### V3.1 设置

| 项 | 锁 |
|---|---|
| 字段 | `CompanionSettings.visualSkin: 'classic' \| 'particle'` |
| 默认 | `'classic'` |
| rev | **不升 `SETTINGS_REV`（保持 13）。** 缺字段和 `recognizer` 一样回默认，禁止为加一个可选字段重写整份存档（`rev < SETTINGS_REV` 会 `persist=true` 把旧用户整包再存一遍）。 |
| 读写 | `loadCompanionSettings` / `saveCompanionSettings` / `defaultCompanionSettings` |
| 校验 | 只认两个字面量；其它一律 classic |
| UI | `SettingsPage` 语音段，「启用月伴对话」下面加两段开关。文案：「月伴外观。玉盘是现在的月亮；星尘是粒子星河。说『切换风格』也能换。」 |

测：默认 classic；写入 particle 再 load 仍是 particle；`"jarvis"` / `""` / 缺省回 classic；`lunitide:companion-settings` 事件仍会派发。

### V3.2 舞台分支

`CompanionStage.tsx` **只**改这两处挂载 + 顶栏一枚开关。禁止重排字幕 / 灯 / 横幅。

**经典（默认，现网）：**

```
.companion-aurora → Aurora（现网）
.companion-moon-slot → MoonSphere（现网，含可见热区）
```

**星尘：**

```
.companion-aurora → ParticleMoonScene（铺满 inset:0，aria-hidden）
.companion-moon-slot → 透明 .companion-moon + .companion-moon-body（无 disc/halo）
                   不渲染 MoonSphere / Orb / Strands / Aurora
```

现网 `.companion-aurora` 是 `pointer-events:none`。仿真里的鼠标视差必须改听 `.companion-stage`（或 `host.closest('.companion-stage')`），**禁止**给 aurora 开 `pointer-events`（会挡住顶栏/热区）。这是接线，不算改配方。

`ParticleMoonScene` 继续是同一文件：不要带预览 HUD。预览页继续包 `ParticleMoonPreview`。切皮肤必须卸载另一套 WebGL（`loseContext`），禁止玉盘+星尘两套 renderer 同时活着。`high` 变化会重建 canvas（现码 `[high]` 依赖）——本访最多降一档，接受一次闪。

根：`data-skin={visualSkin}`。a11y 旧断言继续绿：dialog、aurora `aria-hidden`、无 `.companion-mic` / `.companion-ask`、`core` 里仍有 `.companion-moon`、退出按钮文案不变。

### V3.3 顶栏开关

放在 `.companion-chrome` 里，退出 / 打断 / 暂停这一排，**不要**新开底栏。

- 两个 `role="button"`（或 `role="radio"` 一组 `radiogroup`「月伴外观」）。
- 当前皮肤 `aria-pressed="true"`。
- 中文：「玉盘」「星尘」；英文：「Jade」「Stardust」。
- 点击：立刻 `saveCompanionSettings`，舞台当帧切换，不重载会话、不取消当前听写（说话中切皮肤：TTS 继续，只换脸）。

### V3.4 语音口令

函数已有：`parseCompanionSkinCommand`。进产品时在 `beginUserTurn` 里：`clipCompanionPrompt(cleanUserTranscript(transcript))` **之后**、`setRounds` / `onSend` / `RECOGNIZED_FINAL` / `speakCompanionPad` **之前**拦截。命中则 `return`，机器态保持（聆听继续听，不要跳思考）。

`seedPrompt` 也走 `beginUserTurn`，同一拦截。`pendingSend` 冲刷路径（`chatReady` effect）**不**走 `beginUserTurn`——皮肤口令不得入队；拦截在 `beginUserTurn` 即可避免入队。

整句 trim，去空白，去句末 `。.?？!！`，再去句尾语气「看看 / 吧 / 呀 / 啊」后精确匹配：

| 句 | 结果 |
|---|---|
| 切换风格 / 换个风格 / 换成另一套 | `toggle` |
| 星尘风格 / 粒子月亮 / 贾维斯风格 / 换成粒子 / 换成粒子月亮 | `particle` |
| 玉盘风格 / 普通月亮 / 原来的月亮 / 换回普通月亮 | `classic` |

未命中（含「帮我换成粒子月亮然后查天气」这种任务句）→ 当普通用户句走现网 `chat.start`。  
命中 → `saveCompanionSettings` → 短口播 → `return`。工作台打字「切换风格」**不**切皮肤，只月伴舞台拦截。

口播必须走现成 `TtsPlayer.enqueue([词], settings, callbacks)`，**禁止**走 `speakCompanionPad`（那是「嗯」垫音，且受 `instantAck` 默认关）。`autoSpeak===false` 或 TTS 不可用：只改皮肤，可用 `setEngineHint` 闪同一句，**仍不写会话历史、不 `onSend`**。说话中点顶栏切皮肤：TTS 继续，只换脸。

口播词：

| 结果 | 词 |
|---|---|
| 切到星尘 | 好，换成星尘。 |
| 切到玉盘 | 好，换回玉盘。 |
| 已是星尘 | 已经是星尘了。 |
| 已是玉盘 | 已经是玉盘了。 |

测：上表 +「换成粒子月亮看看。」→ `particle`；「今天合肥的天气怎么样」「你好」→ `null`。新测：命中后 `onSend` 不被调用；`visualSkin` 已写盘。

### V3.5 预览页（对照，不进安装包主路径）

`preview:companion-particle` 保留。文案从「还没进产品」改成「定稿对照；产品里用玉盘 / 星尘切换」。不接 `chat.start` / 真麦。玉盘预览页不改。

---

## V4. 第一部分任务（按这个做，做完停）

1. 设置：`visualSkin`（**不**升 rev）+ `nextCompanionSkin` / `companionSkinConfirmSpeech` 单测。  
2. 设置页两段开关（抽 `CompanionSkinSwitch`，舞台顶栏复用）。  
3. `CompanionStage` 按皮肤挂 `ParticleMoonScene` 或玉盘；透明热区；`data-skin`。  
4. 顶栏两段开关。  
5. `beginUserTurn` 拦截口令 + 短口播；单测 `onSend` 不被叫。  
6. 预览页一句对照文案。  
7. `npx vitest run src/session/companion/particle src/session/companion/companionSettings.test.ts` 以及舞台 a11y / companion 相关测。  
8. 手工：玉盘默认进月伴；点「星尘」画面与预览一致；再说「换回普通月亮」回玉盘且无新用户回合；无 WebGL 时星尘仍有圆。  
9. **停。** 不改听写窗、不改闲聊门、不改 VERSION。

---

## V5. 第一部分明确不做

- 不 fork / 不嵌任何贾维斯整仓。
- 不加 Three.js、R3F、MediaPipe、Tone.js。
- 不做手势、不做第三套皮肤、不把星尘设成默认。
- 不改玉盘配方，不「顺便」改星尘配方。
- 不在星尘里接第二套状态机。
- 不把预览 HUD（四态按钮 / 增益滑条）带进产品舞台。
- 不把切皮肤写成工具调用或技能。

---

## V6. 第一部分验收（产品 4.9 口径）

| ID | 断言 |
|---|---|
| V-P1 | 新存档 / 缺字段 → 进月伴是玉盘；`preview:companion` 外观与现网一致 |
| V-P2 | 设置或顶栏切到星尘：全屏是定稿星河+彩球，不是玉盘极光月；再切回玉盘像素回到现网 |
| V-P3 | 星尘空闲=散开慢转彩球；说「你好」进入聆听后先缩紧再莲花再切形态；思考不另起一套；说话展开并跟 gain 一张一缩 |
| V-P4 | 点星尘热区：说话中打断；标签与现网月亮表一致；a11y 旧测绿 |
| V-P5 | 「切换风格」「粒子月亮」「换回普通月亮」不 `chat.start`、不写用户回合；口播确认；天气句仍进引擎 |
| V-P6 | 无 WebGL2 / 减动效：星尘仍有圆，不是黑屏 |
| V-P7 | 高档预算不超过 V2.9；掉到 40fps 自动低档 |
| V-P8 | 刷新 / 退出再进：皮肤仍是上次选的 |

剩 0.1：电影级体积光、真频谱 FFT、手势拨开。本轨不承诺。

---

# 第二部分 · 指令执行收口

以下 §0–§10 是执行轨。星尘进产品时 **不要** 把闲聊门 / 火山窗 / 预批准和皮肤分支搅在一个 PR 里。执行轨 **不要** 改 `CompanionStage.tsx` 的玉盘 DOM；星尘分支是第一部分的事。

## 0. 现状打分（满分 5，落地后目标 4.9）

| 面 | 现分 | 目标 | 为什么不是 5 |
|---|---:|---:|---|
| A 指令执行收口 | 1.4 | 4.9 | 说稍等就停；闲聊门把问句当闲聊；R0 不得误挂工具 |
| B 三种听写一致（收口） | 4.5 | 4.9 | 收口在引擎，三模式同一条 `chat.start`；要测三模式都过 |
| C 火山半句 / 抢答 | 1.8 | 4.9 | 服务端 VAD 400ms；半句 `final` 后当整句发出 |
| D 火山插话打断 | 1.5 | 4.9 | `companionVoiceBargeInEnabled` 恒 false |
| E 打字=语音工具面 | 2.2 | 4.9 | 工作台打字永远挂工具；月伴语音多闲聊门 + 点/填卡批准 |
| **综合** | **2.1** | **4.9** | 剩 0.1：真网搜索延迟、火山服务端 VAD 不能 100% 对齐 sherpa 解码器 |

未达标的 0.1 不靠再加提示词补。本 PRD 不承诺实时气象精度，只承诺：**有工具就必须跑完，开口必须是结果或「无法执行」。**

---

## 1. 现码复盘（禁止当缺口重做已绿的锁）

### 1.1 三种听写卡（不是三种引擎环）

| UI 卡 | `voicePath` | ASR | 定稿 | 现打断 |
|---|---|---|---|---|
| 云端 | `cloud` | Windows Web Speech | 客户端 280/220ms 完整句；不完整 1200/1500ms | 只按钮 / Tab / 点月亮 |
| 火山 | `volc` | seed-asr `volcsauc` | **服务端 `DefaultEndWindowMS=400`** 出 `final`，再走同一套客户端规则 | 只按钮（通话核 opt-in 才可对着麦打断） |
| 本机 | `local` | sherpa | **服务端 `TurnEndSilenceSeconds=1.2`** | 只按钮 |

入口：`VoicePathPicker.tsx` → `CompanionStage.startListening`。三条听写在 ASR `final` 之后都进 `beginUserTurn` → `sendAndChat` → `chat.start`。**执行收口与听写卡无关；火山抢话与听写卡有关。**

第四条「实时全双工通话核」是设置里 opt-in，不是第四张听写卡。本 PRD 不改通话核。

`CompanionStage` 已把 `bargeIn` / `listenThrough` 接到 `companionVoiceBargeInEnabled(...) && (local || volc)`。开关现码恒 false（`companionSettings.ts` 直接 `return false`），所以本机 `considerBargeIn` 虽在 `localSpeech.ts` 里，cascade 本机卡不会走到。打开开关时 **只许 `voicePath==='volc'`**，本机卡保持 false。

现码注释写「cascade 只按钮 / barge-in 已退役」。落地时改函数体，并改文件头注释，与 §3.3 对齐。不要为了改注释去动舞台视觉。

### 1.2 图1 空等：两条闸，不是 TTS 把回合掐死

用户：「今天合肥的天气怎么样」  
月汐：「……我手头没有实时数据……我可以帮你查一下，稍等。」→ 再无下文。

**闸 1 — 工具没挂上**

`chat.go` 约 200 行：`wantsTools := !p.Companion || mode == full-access || companionWantsTools(turnText)`。  
约 294–296 行：**只要 `p.Companion`，就用 `companionWantsToolsForTurn` 覆盖**，`full-access` 初值作废。

- **工作台「对话」打字**：`companion=false` → 永远挂工具 → `classifyTaskRoute` + `applyTaskRoute` 生效。
- **月伴里打字和语音**：都是 `companion=true`（`SessionPage` 的 `companion: companionOpen`）。同一道闲聊门。月伴打字「今天合肥的天气怎么样」**同样零工具**，不是「打字永远强」。
- 语音体感更弱的额外原因：火山半句（§1.3）+ 点/填卡批准（§1.4）。

`companionWantsTools` 针有「查一下」「查天气」，**没有**「天气怎么样」。  
`detectTaskRoute("今天合肥的天气怎么样")` 已是 **R1**（`infoQueryHints` 含「天气」），但 `applyTaskRoute` 只在 `wantsTools==true` 时运行。  
现测锁死错误行为：`TestCompanionWantsTools` 断言 `今晚天气` 不得挂工具；`TestCompanionFastPathCapsTokensAndKeepsVoice`（payload `今晚天气`）断言 `len(req.Tools)==0` 且系统指令不得含「不要 web.fetch」。

现码路由对照（落地后 A1 必须仍成立）：

| 句 | `detectTaskRoute` | 现 `companionWantsTools` | 落地后 |
|---|---|---|---|
| 你好 | **R0**（`autoToolProfile` minimal；`autoProfileChatHints` 含「你好」） | false | **仍 false**。禁止对 R0 开闸。R0 的 allow 表本身含 `web.search`，一旦误挂，问候会搜网。 |
| 今晚月色如何 | **unspecified**（无天气针，也不是 minimal） | false | 仍 false |
| 今晚天气 / 今天合肥的天气怎么样 | **R1** | false | **true**，挂 `web.search` |
| 帮我打开桌面道具 | **unspecified**（「打开」但「道具」不在 `namedLocalAppHints`） | **true**（针「打开」） | 仍 true。不是闸 1。 |
| 打开记事本 | **R2** | true | true；ccOff 无 `computer.act`，ccOn 有 |

**闸 2 — 说了稍等也不续轮**

- `runStream` 无 `ToolCalls` 且 `pickTurnContinueKind==""` → **立刻 break**。
- `leadin`：`companion && usedTools && isCompanionLeadInOnly`。没调工具 → 不可能 leadin。
- `isCompanionLeadInOnly` 只认整句等于「稍等」「好，我帮你查一下」等，或 ≤6 字且含「等/好」。图1 长承诺 **不是** lead-in-only。
- 空串也被当成 lead-in-only（现行为）。落地后：已挂表 + 空回复 / 短 lead-in / 长承诺，都走 `wait`。
- TTS 头句播放 **不会** 结束引擎。引擎已经结束，前端 `chatStatus==='done'` 后 `PLAYBACK_ENDED` → 空等。

三种听写都走这条引擎链 → **三模式都有图1。**

催工具续轮 **不能** 代替挂工具：续轮复用同一份 `req.Tools`。没挂上，催也调不成。`maxContinueNudges=3`。三次 `wait` 仍不调工具 → 必须口播「无法执行：这一轮没有完成查询。」再结束，禁止默默 break。

### 1.3 火山：插话、半句、抢答（两根因）

云端 / 本机不改这三把锁。

**① 临时插话打断（月汐正在说时）**

`companionVoiceBargeInEnabled` **恒 false**。`volcSpeech.ts` 的 `considerBargeIn` / `BARGE_IN_ARM_MS=160` 已写好。舞台 `listenThrough` / `bargeIn` 已接线。用户锁：火山 cascade **说话中** 对着麦插话必须打断 TTS 并听新句。

**② 我说一句，他只认出一半**

火山 `DefaultEndWindowMS=400`：换气约 0.4s 就 `final=true`。`enable_punc=true` 给半句打「。」，`looksIncompleteUtterance` 以为说完，`evaluate()` → `recycle('final')`。本机 sherpa 要 **1.2s**。

`internal/voice/volcsauc` **没有**断言 400 的现测；`web/.../volcSpeech.test.ts`「400ms 内定稿问候」是客户端 280/220，**禁止当服务端窗去改**。

**③ 话还没说完他就开始答 — 与 ② 同一根**

400ms 服务端窗 + `final` 后再 220/280ms 客户端完整句 → 换气到 `chat.start` 可以只有 **0.4–0.7s**。锁：`DefaultEndWindowMS` **400 → 1200**。`speech.ts` 的 280/220 **不动**。`final` 仍走 `evaluate()`，禁止 `final` 直接 `beginUserTurn`。嘈杂档 100/120ms **不动**。

### 1.4 图2 / 打字强语音弱（对照现码，上一稿判错）

不是两套桌面手。打字和语音最后都进同一个 `chat.start` → `runStream` → `desktop.open` / `computer.act`。

**上一稿错：** 把「帮我打开桌面道具」写成零工具闲聊。现码针「打开」**已经命中**，`companionWantsTools==true`。图2 更像：

1. 火山 400ms 把「帮我打开……桌面上的道具」拆成两轮：先「帮我打开」（有针），再「桌面道具」（无「打开」、路由 unspecified、`companionWantsTools==false`）→ 后半截零工具幻觉「找不到叫道具的图标」；
2. 或整句已挂表但模型只口头承诺、不调 `desktop.open`（闸 2）；
3. 语音点/填卡在 `ccStandingApprovedTool`（现仅 `desktop.open` / `media.play`）。

进门之后语音独有闸：

| 闸 | 现码 | 体感 |
|---|---|---|
| 闲聊门 | `companionWantsTools`（问句漏针；半句更漏） | 月伴问句零工具 |
| 预批准 | `ccStandingApprovedTool` 只有打开/播 | 语音 `computer.act` / `desktop.type` 卡批准 |
| 纯打开收场 | `companionGoalIsOpenOnly` 成功后停环 | 纯打开该停；「打开并写」已排除「写」，保持 |

不在本 PRD 放开：月伴 `command.run` / `im.send` / `cc.*`。`browser.act` **已经**在月伴工具表里（`TestCompanionAttachesFullToolset` 要求「打开网页」含它）；本 PRD 不新做浏览器点击运行时，也不删现有广告。

锁：同一句 `G`，`companion=true` 与 `companion=false` 的路由、允许工具名、**电脑控制开时的桌面动手**必须同档（§3.4）。火山半句修了之后，整句才能再打中闲聊门。

### 1.5 GUI 模型设置：只是点选兜底，不是电脑手（现网，禁止当缺口重做）

设置 → 供应商页「能力路由」有六席：对话缺省 / Flash / 视觉 / 向量 / Judge / **GUI**。UI 原文：`GUI · 只作兜底，模型看不见`（`CapabilityRouting.tsx`）。「看不见」= **不进对话、不看聊天记录**，只吃当前帧截图 + SoM 读号提示，回一个 JSON 目标。

电脑操作的主路径从来不是 GUI 席：

1. 会话 **对话模型** 调 `desktop.open` / `desktop.type` / `computer.act`（先 observe / 按 name、id 点，禁止盲点）。
2. 只有这步 **失败**（`desktop.type` 或 `computer.act` 出 `ok:false`，且 L0 没过）之后，引擎才走 `tryGUIFallback`（`chat_run_stream.go` → `gui_fallback.go`）。
3. 每回合最多一次。路由必须是 **R2 或 R3**。电脑控制必须开。子代理不走。UAC / 另存为 / 登录墙已停车则不走。`desktop.type` 已 L0 成功则不走。

| 条件 | 执行器 |
|---|---|
| 观察树有节点 + 目录里有 `kind=gui` | **GUI 模型** SoM：只回 `markId`（如 `B1`） |
| 有节点、无 GUI、有视觉席 | **视觉模型** SoM |
| 树空 + 有 GUI | GUI 出经 `frameId` 校验的归一化坐标 |
| 树空且无 GUI，或两席都空 | **不兜底** |

成功则再调一次 `computer.act click`。打字和语音共用 `tryGUIFallback`。本 PRD 不重写 `AnnotateCapture` / `assignNodeIDs` / `guiSomPickPrompt`。预批 `computer.act` 之后，兜底那一下 click 也不得再卡批准。

---

## 2. 方案选择（只留一条）

| 方案 | 做法 | 否决 |
|---|---|---|
| A 加天气针 | `companionWantsTools` 再塞「天气」 | 用户已否定「天气收口」；下一句「合肥气温多少」又漏 |
| B 语音永远挂全工具 | 对齐工作台 `wantsTools=true` | 闲聊 TTFT 回退；与「闲聊立刻回答」冲突 |
| C **路由即执行（采用）** | 针 **或** `detectTaskRoute∈{R1,R2,R3,R4}` → 挂工具；已挂表的承诺句续轮 `wait`；三次仍不调则「无法执行」 | 闲聊 / R0 / unspecified 无针仍零工具；执行句与工作台同路由 |

---

## 3. 产品锁（落地后 4.9 验收口径）

### 3.1 执行收口（三听写同一把锁）

对任意用户句 `G`：

1. 现 `companionWantsTools` 针命中 **或** `detectTaskRoute(G)` ∈ {**R1,R2,R3,R4**} **或** `companionWantsToolsForTurn` 的桌面/音乐跟句 → 本轮 **必须** 挂路由后的工具表 + `companionPersonaToolsInstruction`（含「不要只说等一下就停」）。
2. **R0 与 unspecified 且针不过 → 零工具。** 「你好」「今晚月色如何」「我随便说说」仍零工具。禁止 `detectTaskRoute != unspecified` 这种写法（会把 R0 问候挂上 `web.search`）。
3. 模型先出口头承诺，**无论句子长短**，在已挂表且尚未 `usedTools` 时续轮 `wait`：
   - 触发：`looksLikeCompanionWaitPromise` **或** `isCompanionLeadInOnly`（含空回复、短「稍等」「好，我来执行」）。
   - `looksLikeCompanionWaitPromise`：含「稍等」「等一下」「帮你查」「我去查」「我来做」「我来执行」，或同时含「手头没有」+「查」。排除「无法执行」「已经打开」「已经写入」。**不要**用「数字+气温」做排除（图1 也谈温度，会误杀）。
   - 催词：「立刻调用本轮已装备的工具执行。不要再承诺稍等。下一句必须是结果或无法执行。」
   - 已 `usedTools` 且正文仍是短 lead-in → 沿用 `leadin`。
4. 工具跑完：必须再有一句 **结果** 或 **无法执行+原因**，并送 TTS。禁止 `runStream` 在承诺句上默默 break。
5. `wait` 已用满 `maxContinueNudges`（3）仍无 tool_calls → 引擎补一句「无法执行：这一轮没有完成查询。」再结束（F-A7）。
6. 真网 / 工具失败：`companionToolResultSpeech` 对 `web.search` / `web.fetch` 失败若原文无「无法执行」，改为「无法执行：这次没有查到。」（F-A6）。不要假装有气温/已打开。
7. 不复活 `companionOpeningAck`。不改 poison。不改月伴 TTS 音色。

旧测必须改：`今晚天气` / `今天合肥的天气怎么样` **要挂** `web.search`。`TestCompanionFastPathCapsTokensAndKeepsVoice` 改为断言 **有** 工具天气条款。新测：`今晚月色如何` 仍不挂。

`companionWantsToolsForTurn` **不改逻辑**（已先走 `companionWantsTools`，再桌面/音乐跟句）。A1 自动流入。

### 3.2 火山定稿：半句 + 抢答一起修

- `volcsauc.DefaultEndWindowMS`：**400 → 1200**，与 `sherpa.TurnEndSilenceSeconds` 同量级。`endWindowMS()` 现逻辑：`<200` 回 default，`>3000` 封顶。零值走 1200。
- 客户端 `UTTERANCE_SILENCE_MS=280` 等 **不动**。
- `final` 仍走 `evaluate()`。
- 半句无句末标点：`looksIncompleteUtterance==true` 必须走 1.2s/1.5s 强制窗。
- 验收：连说「帮我打开桌面上的记事本」中间换气 &lt;1s → 字幕必须是整句，不得先定稿「帮我打开」再抢答。

### 3.3 火山插话打断（仅火山；①）

| 卡 | 聆听定稿 | 月汐说话中 |
|---|---|---|
| 云端 | 说完再答 | **只** 打断按钮 / Tab / 点月亮 |
| 本机 | 说完再答 | **只** 按钮 / Tab / 点月亮 |
| 火山 cascade | 说完再答（1200ms 窗） | **对着麦打断**（`companionVoiceBargeInEnabled` 仅 `voicePath==='volc'`） |
| 火山通话核 | 不改 | 已可打断，不改 |

云端播放期停 Web Speech **保持**。本机 `voicePath==='local'` 时 barge-in 保持 false（即使 `voiceBargeIn` 存储为 true）。

文案必须三处一致（只改文案文件；第一部分可以动 Stage 挂载，第二部分仍不改玉盘视觉）：

| 文件 | 火山 cascade |
|---|---|
| `companionLights.ts` | `火山 seed-asr · 说完再答 · 可对着麦打断` |
| `asrPath.ts` | `火山听写 · seed-asr · 说完再答 · 可对着麦打断` |
| `voicePersonas.ts` 火山卡 desc | 说完再答，说话中可对着麦打断；通话核仍是设置里的全双工 |
| `settings/VoicePathPicker.tsx` 顶栏说明 | 火山句改为可对着麦打断；云端/本机仍按钮 |

云端/本机仍「打断用按钮」。`voicePersonas.test.ts` 的云端卡断言保持。

### 3.4 打字 = 语音桌面执行（第四条锁）

同一句 `G`，`companion=true` 与 `companion=false`：

- `detectTaskRoute` 相同（分类器本就忽略 companion 参数）；
- `applyTaskRoute` 后的允许工具名相同（月伴仍剥 `command.run` / `im.send` / `cc.*`）；
- `ccOn` 两边读同一设置：关则都无 `computer.act`，都有 `desktop.open`；开则 R2 两边都有 `computer.act`。
- 电脑控制开：`ccStandingApprovedTool` **加上** `computer.act`、`desktop.type`。电脑控制关：两边都不得装成功点/填。
- 纯打开目标：打开成功仍停环。带「写/填/点/播」的目标必须继续。

`assembleRoutedTools` 是 `chat.start` 的镜像，F-E* 用它比再走一遍 HTTP 更稳。

手工对照：

| 句 | 必须发生 |
|---|---|
| 帮我打开记事本 | `desktop.open`，成功口播「已经打开了记事本。」 |
| 帮我打开桌面道具 | **整句** `desktop.open name=道具`（或用户原词）；找不到 → 「无法执行」+ 请说全名。禁止半句后零工具反问 |
| 打开记事本并写你好 | 电脑控制开：打开后 `desktop.type` / `computer.act`；关：打开后说明要开电脑控制 |

### 3.5 GUI 兜底（现网事实锁，本 PRD 不新做）

- 主手=对话模型 + 桌面工具。GUI 席=失败后 SoM 补一枪。
- 不改 `pickGUIFallback` / `shouldAttemptGUIFallback`。
- 未配置 GUI 且无视觉目录：不兜底，不得假装点中。

---

## 4. 落地改哪里（禁止平行实现）

| ID | 文件 | 改什么 |
|---|---|---|
| A1 | `internal/app/chat_companion_speech.go` | `companionWantsTools`：针失败后若 `detectTaskRoute` 为 R1–R4 则 true，然后才走专家匹配。不要对 R0/unspecified 开闸。不要加天气针。 |
| A2 | `internal/app/companion_context.go` | **只验证不改**：`companionWantsToolsForTurn` 已先走 A1。 |
| A3 | `internal/app/chat_intent.go` | 新增 `looksLikeCompanionWaitPromise`（§3.1.3）。`isCompanionLeadInOnly` 不放宽匹配表。 |
| A4 | `internal/app/chat_continue.go` | `pickTurnContinueKind` 末尾加 `toolsAttached bool`。`!usedTools && toolsAttached && (waitPromise \|\| leadInOnly) && nudges<max` → `"wait"`。放在 `leadin` 之前。 |
| A5 | `internal/app/chat_run_stream.go` | 传入 `len(req.Tools)>0`；`case "wait"` 催词见 §3.1.3；`continueKind==""` 且承诺未执行且已挂表且 nudges 已满 → 补「无法执行：这一轮没有完成查询。」 |
| A6 | `internal/app/chat_companion_fastpath_test.go` | 翻转 `今晚天气`；改 `TestCompanionFastPathCapsTokensAndKeepsVoice`（不是不存在的 `TestCompanionSkipsReasoningAndWorkflows`）。 |
| A7 | `internal/app/chat_companion_speech.go` | `companionToolResultSpeech`：`web.search`/`web.fetch` 失败无「无法执行」→「无法执行：这次没有查到。」 |
| C1 | `internal/voice/volcsauc/config.go` + **新测** | `DefaultEndWindowMS=1200`；协议 JSON `end_window_size==1200`。更新注释「产品默认 400」。 |
| D1 | `web/src/session/companion/companionSettings.ts` | `voicePath==='volc'` 时 barge-in true。更新文件头注释（现写「cascade 只按钮 / 恒 false」）。 |
| D2 | `companionLights.ts` + `asrPath.ts` + `voicePersonas.ts` + `settings/VoicePathPicker.tsx` + 对应测 | 火山文案按 §3.3。不改玉盘像素。 |
| E1 | 新 `internal/app/companion_parity_test.go` | `assembleRoutedTools`：同一 `G` 打字/语音允许集。 |
| E2 | `internal/app/approval_profile.go` | `ccStandingApprovedTool` 含 `computer.act`、`desktop.type`。改函数注释（现写「不能点/填」）。翻转 `approval_profile_test.go`。 |

不改：poison / TTS 音色 / `speech.ts` 共享常数 / sherpa 1.2s / `companionOpeningAck` 接线 / `gui_fallback.go` / `SessionPage.tsx`。  
`CompanionStage.tsx`：执行轨不改视觉；皮肤分支只走第一部分 V3.2。

会红、必须按本表改的旧测（不是回归失败）：

- `TestCompanionWantsTools`：`今晚天气` 改为要挂。
- `TestCompanionFastPathCapsTokensAndKeepsVoice`：改为要挂 `web.search` + 天气条款。
- `TestApprovalProfileDangerous`：ccOn 预批 `computer.act`/`desktop.type`；`cc.mouse_click` 仍 false。
- `companionSettings.test.ts`：volc barge-in true。
- `companionLights.test.ts` / `asrPath.test.ts`：火山文案。
- `chat_continue_test.go`：旧 `pickTurnContinueKind` 调用补最后一参。

`TestCompanionTaskWorkflowInjectionSkipsIdle` 含「今晚天气」「打开网页」——测的是办公室流水线字符串，**不随 A1 变**，不要误改。

---

## 5. 测试锁（先写红测再改）

| ID | 断言 |
|---|---|
| F-A1 | `companionWantsTools("今天合肥的天气怎么样")==true`；`今晚天气==true`；`今晚月色如何==false`；`你好==false`；`我随便说说==false` |
| F-A2 | companion `chat.start`「今晚天气」→ `req.Tools` 含 `web.search`；系统指令含「不要只说等一下就停」 |
| F-A3 | 长句「手头没有…帮你查一下，稍等。」+ 无 tool_calls + `toolsAttached` → `continueKind==wait` |
| F-A4 | 「稍等。」+ `toolsAttached==false` → `continueKind==""`（防闲聊死循环） |
| F-A5 | 工具已跑 + 「好，我帮你查一下」→ `leadin` |
| F-A6 | `companionToolResultSpeech("web.search", "ok:false")` 含「无法执行」 |
| F-A7 | `wait` 且 `nudges>=3` → kind `""`；`runStream` 在已挂表、未用工具、承诺句上结束时补「无法执行：这一轮没有完成查询。」 |
| F-C1 | `DefaultEndWindowMS==1200`；`fullClientRequest` 的 `end_window_size==1200` |
| F-C2 | volc `final` + 不完整句 → `shouldCommitHeardUtterance` 仍 false（**现测保持，不改**） |
| F-D1 | `companionVoiceBargeInEnabled({voicePath:'volc'})===true`；cloud/local ===false；local+`voiceBargeIn:true` 仍 false |
| F-D2 | 灯 / `asrPath`：volc 含「可对着麦打断」；cloud/local 含「打断用按钮」 |
| F-E1 | 「帮我打开桌面道具」companion 与非 companion 都允许 `desktop.open`；companion 无 `command.run` |
| F-E2 | 「打开记事本」+ ccOff：两边有 `desktop.open` 无 `computer.act`；ccOn：两边有 `computer.act` |
| F-E3 | ccOn：`companionToolPreapproved("computer.act")` 与 `desktop.type` 为 true；ccOff 为 false；`cc.mouse_click` 在 ccOn 仍 false |
| F-E4 | 「打开记事本并写你好」：`companionGoalIsOpenOnly==false`；纯「打开记事本」为 true |

禁止用 live 天气 URL 当门禁。用 stub completer。

---

## 6. 手工 15 分钟（装现网 0.4.63 **之后的下一构建**，不是现在手里的 Setup）

三听写各一遍（云端 / 火山 / 本机）：

1. 「今天合肥的天气怎么样」→ 先有一句开口，必须出现「网页搜索中」或等价 tool 活动，然后口播阴晴/气温或「无法执行：…」。禁止停在稍等。
2. 「帮我打开记事本」→ `desktop.open`，成功则「已经打开了记事本。」
3. 闲聊「今晚月色如何」→ 立刻闲聊，无搜索条。

只火山：

4. 连说两句中间换气不足 1s：「帮我打开……桌面上的那个」→ **不得** 在第一截就抢答。
5. 月汐正在播结果时对着麦说「停」→ 必须打断；云端/本机同一操作 **不得** 语音打断，要点打断。

打字 vs 语音同一句「帮我打开桌面道具」：两边都走打开，失败文案都是无法执行+请说全名。工作台打字与月伴语音对照一次「打开记事本」（电脑控制开/关各一）。

星尘若已进产品：本段在**玉盘**下勾一遍即可。皮肤不得改变工具是否挂上。

---

## 7. 明确不做

- 不把三种模式都改成通话核全双工。
- 不改云端/本机共享 `speech.ts` 280/220。
- 不加天气专用分支、不加第二套路由表。
- 不重写 GUI SoM 兜底、不把 GUI 席升成电脑主手、不重写 `AnnotateCapture` / `assignNodeIDs`。
- 不重打包进 0.4.63；落地后另开版本。
- 不在本机日常账号跑 `Test-Install.ps1`。
- 不删月伴已有的 `browser.act` 广告。
- 执行轨不改玉盘像素、不改 `SessionPage.tsx`。

---

## 8. 落地顺序

**视觉轨（第一部分，用户已点头）：** V4.1 → V4.8，单独提交。

**执行轨（第二部分）：**

1. F-A* 红测 → A1–A7（引擎收口，三种听写一起绿）
2. F-E* 红测 → 与 A 同一提交（工具面本就是 A）
3. F-C1 → C1
4. F-D* → D1–D2
5. 手工 §6

A 不依赖听写改动，可先合。C/D 只碰火山配置 + 灯/路径/人设文案。  
`companionSettings.ts` 若两轨都要改（`visualSkin` 与 barge-in），**分两次提交**，先合皮肤字段，再改 barge-in，避免一个函数里两套故事。

---

## 9. 复盘勘误（对照现码，已修进本文）

| 稿 | 错 | 现锁 |
|---|---|---|
| 第一部分仍写「Nova 月 / form 星云插值 / 白点 / 36k」 | 仿真已改成嵌套彩球 + 七环星河 + 实体行星 | 以 V2 现码数字为准 |
| 第一部分停在「未点头不写 visualSkin」 | 用户已定稿，要第二种模式 | V1 写字段，默认 classic |
| V1.6 要 Scene 吃 `onInterrupt` | Scene 现契约没有点击；再挂 `MoonSphere` 会叠玉盘 WebGL | Scene 只装饰；热区独立透明按钮 |
| `SETTINGS_REV` 13→14 | `rev < SETTINGS_REV` 会把旧存档整包再写 | **不升 rev**，缺字段回 classic |
| 口令只认「换成粒子」 | 仿真字幕就是「换成粒子月亮看看。」 | 去语气词；加「换成粒子月亮」 |
| 口播暗示走垫音 | `speakCompanionPad` 是「嗯」且默认关 | 必须 `TtsPlayer.enqueue` |
| 星尘挂进 aurora 指望自己收鼠标 | `.companion-aurora{pointer-events:none}` | 视差听 `.companion-stage` |
| D1 当「开关还没打开」 | 现码 `companionVoiceBargeInEnabled` **恒 false**，注释写已退役防回声 | 用户锁仍要火山可插话；落地是**有限恢复**（仅 volc + 现成 echo discard），不是把退役注释当废纸 |
| §3.1 写成 R0–R4 都挂工具 | `detectTaskRoute("你好")==R0`，R0 allow 含 `web.search` | 只 R1–R4 + 针。F-A1 `你好==false` |
| 计划写 `TestCompanionSkipsReasoningAndWorkflows` | **仓库无此测试** | 改 `TestCompanionFastPathCapsTokensAndKeepsVoice` |
| 图2=零工具 | 「帮我打开桌面道具」针「打开」已 true | 半句拆轮 / 不调工具 / 批准卡；整句靠 1200ms |
| 打字=`companion=false` | 月伴打字也是 `companion=true` | 工作台才永远挂工具；月伴打字与语音同门 |
| `wait` 认「手头没有」单独 | 「手头没有那本书」误催 | 须含稍等/等一下/帮你查/我去查/我来做/我来执行；或（手头没有 **且** 查） |
| `wait` 用「数字+气温」排除 | 图1 也说温度，会误杀 | 只排除无法执行/已经打开/已经写入 |
| 三次 wait 后默默 break | 用户仍停在稍等 | F-A7 补无法执行 |
| F-A6 指望现 `companionToolResultSpeech` | 失败常回「这次没有完成。」 | A7 改搜索失败文案 |
| A2 要改 `companion_context.go` | ForTurn 已先走 WantsTools | 只验证 |
| 火山客户端测「400ms 内定稿问候」 | 那是 `speech.ts` 280/220 | 只改 Go `DefaultEndWindowMS` |
| D2 只改灯 | `asrPath.ts` / `voicePersonas.ts` 仍写打断用按钮 | 三处文案一起改 |
| 语音 `executionMode=full-access` | `chat.go:200` 初值 true，`296` 覆盖 | 只改 WantsTools，不删覆盖 |
| 计划写「更新所有断言 400 的 volcsauc 测」 | **没有这类测** | 新写 F-C1 |
| 执行轨「绝对不碰 CompanionStage」 | 星尘必须在 Stage 加分支 | 执行轨仍不改玉盘 DOM；皮肤分支只走第一部分 |

GUI 兜底、poison、说完再答（云端/本机）、`companionOpeningAck` 仍冻结。

---

## 10. 规格自检（文档本身，落地前）

| 项 | 结果 |
|---|---|
| 占位符 TBD/TODO | 无 |
| 2026-09-05 晚复核 | 已补接线锁：不升 rev、Scene 不吃点击、enqueue 口播、口令去语气词、视差听 stage、插话=有限恢复 |
| 星尘画面 | 按现码锁死；与过期「Nova 白月」描述已切断 |
| 文件/函数名对照现码 | 已核对；`companionVoiceBargeInEnabled` 现恒 false，D1 要改的是这个函数 |
| 签名变更调用点 | `pickTurnContinueKind` 仅 `chat_run_stream.go` + `chat_continue_test.go` 4 处 |
| 与冻结冲突 | 无。玉盘像素冻结；星尘配方冻结；VERSION 不动 |
| 可独立测试 | V-P* 与 F-* 都有断言；天气不靠真网 |
| 两轨文件重叠 | 只有 `companionSettings.ts`：分提交 |
| 残留风险（计入 0.1） | 火山云端 VAD 实现细节；「你觉得这天气心情怎么样」会走 R1 搜网；核显首次进星尘可能先掉到低档 |

**文档分：4.9/5。** 落地后：星尘产品目标 4.9（V6），执行综合目标 4.9（§0），两把尺分开报，禁止用仿真页分数冒充产品已切。
