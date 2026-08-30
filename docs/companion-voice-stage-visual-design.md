# 月伴舞台视觉改造 — 可替换落地设计

Status: **Phase 1 + Phase 2 已落地。** 本文是对照 React Bits 真机预览 + 抖音「3步 vibecoding 一个 AI 语音助手」+ 现有月伴舞台后的替换方案。只改本文列出的文件；不改免提状态机、ASR/TTS、电脑控制。

Reference:

- 参考站：[React Bits Orb](https://www.reactbits.dev/backgrounds/orb)、[Aurora](https://www.reactbits.dev/backgrounds/aurora)、[Strands](https://www.reactbits.dev/animations/strands)、[Iridescence](https://www.reactbits.dev/backgrounds/iridescence)
- 参考片：`https://www.douyin.com/video/7659809759737974052`（作者 AI充电官）
- 现有舞台：`web/src/session/companion/CompanionStage.tsx`、`MoonSphere.tsx`、`web/src/styles.css`（`.companion-*`）
- 现有验收：`CompanionStage.a11y.test.tsx`（纯月面、无输入条、无麦克风大按钮）

---

## 1. 结论（先读这段就能开工）

视频前端不是手写一套 UI 语言，而是 **React Bits 的 WebGL 组件拼装**：

| 视频里看到的 | React Bits 真机对应 | 用在月伴 |
|---|---|---|
| 全屏流动极光 | **Aurora**（simplex 噪声幕 + 三色停） | 第 1 层：换掉死黑星空 |
| 思考时油膜圆球、有体积高光 | **Orb** 环光 + **Strands `glass=true`** 球面透镜 | 第 2 层：月亮「思考」材质 |
| 说话时横条等离子光带 | **Strands `glass=false`**（加法光带 + bloom） | 第 2 层：月亮「回答」外形 |
| 空闲轮播问句 + 玻璃输入 | 落地页组合，不是组件库内置 | 第 3 层轮播 + `.companion-ask` 玻璃输入 |

真机核对（2026-08-30，reactbits.dev）：

- **Orb** 依赖 `ogl`。它是 **能量环**（shader 里 `innerRadius = 0.6`），不是实心玉盘。悬停用 `sin` 扭曲 UV，边缘会波动变色；`forceHoverState` 可在没有鼠标时强制进入「活」态。Hue / Hover Intensity / Rotate On Hover 都是运行时 uniform，适合绑月伴状态。
- **Aurora** 同样是 `ogl` 全屏三角 + 噪声高度场。`colorStops` / `amplitude` / `blend` / `speed` 可热更新。深色模式输出带 alpha 的极光，可叠在黑底上。
- **Strands** 也是 `ogl`。光带循环用双正弦叠加；`glass=true` 时先画光带到 FBO，再套一层 **球透镜**（Fresnel 边缘 + 左上高光 + 色散）。这正好解释视频里「同一团光：圆时像玻璃珠，说话时摊成光带」——**不必做 SDF 形变**，切换 `glass` + 少量参数即可。

落地策略：**vendor 拷贝这三个 MIT 组件进仓库**（不引入整个 React Bits、不上 Three.js），加依赖 `ogl`。`MoonSphere` 仍是唯一对外组件：props 不变（`state` / `gain` / `levels` / `interruptible` / `onInterrupt`）。WebGL 不可用或 `prefers-reduced-motion` 时退回今天的 CSS 月亮，jsdom 测试不崩。

不改：`useCompanionMachine`、听→答→再听、Esc 退出、空格麦、月亮点击打断、字幕条、退出/打断按钮。首页 `LaunchHome` 本轮不动。

---

## 2. 产品边界：改哪、不改哪

### 改

只改 **月伴全屏舞台**（`html.companion-active` 时的 `.companion-stage`）。这是语音产品的「脸」。

### 不改

| 表面 | 原因 |
|---|---|
| 首页「今天想聊什么？」+ `launch-composer` | 还要附件、技能、专家、项目；视频那种极简壳撑不住 |
| 普通文字会话页 | 不是语音舞台 |
| 侧栏月亮图标 `.real-moon.small` | 品牌识别，保持冷白玉盘 |
| 电脑控制 / BeeBEEP / 身份 | 无关 |

### 与现有验收的关系

`CompanionStage.a11y.test.tsx` 要求：

- 无 `.companion-controls` / `.companion-mic` / `.companion-typing` / TTS 开关
- 有 dialog、aria-live、退出按钮
- 装饰层 `aria-hidden`

Phase 1 继续满足。星空选择器改成 `.companion-aurora`（仍 `aria-hidden`）。Phase 2 才允许玻璃输入，并显式改那条测试。

---

## 3. 四状态视觉合同

现有机器：`idle | listening | thinking | speaking`。`companionSurfaceState()` 把「工具执行中」映射成 thinking。视觉必须跟 `data-state` 走，不能另起一套。

| 状态 | 用户感知 | 第 1 层 Aurora | 第 2 层月亮 | 第 3 层问句 |
|---|---|---|---|---|
| **idle** | 月汐在，还没开口 | 慢、暗、冷青紫 | 冷白月盘仍可见；外圈 Orb 低强度、不强制 hover | 显示轮播（进入后约 1.2s，无用户轮次时） |
| **listening** | 正在听 | 略提亮 | 月盘还在；Orb `forceHoverState=true`，`hoverIntensity = 0.4 + level*1.6` | 立刻隐藏 |
| **thinking** | 视频图 1 | 再暗一点，把对比让给球 | **隐藏月盘**；Strands `glass=true`，中等 speed，hue 慢转 | 隐藏 |
| **speaking** | 视频图 2 | 跟随 `--moon-gain` 微增 amplitude | **隐藏月盘**；Strands `glass=false`，`speed/amplitude/glow` 绑 `gain` | 隐藏 |

过渡：thinking ↔ speaking 只切 Strands 的 `glass` 和参数，**同一 canvas、同一 palette**，280ms 内 `opacity` 交叉不是两套无关 GIF。idle/listening → thinking：月盘 220ms 淡出，Strands 淡入。speaking → idle：光带收回，月盘淡回。

点击热区始终是 `.companion-moon-body`（圆形按钮，`tabIndex=-1`，现有 `aria-label` 表不变）。WebGL 在按钮后面、`pointer-events: none`。

---

## 4. 三层结构（可直接画进 DOM）

```
.companion-stage[data-state]
  .companion-aurora[aria-hidden]          ← 层1 Aurora 全屏
  .companion-moon.state-{idle|…}
      canvas/ogl（Orb 或 Strands）        ← 层2 装饰，pointer-events:none
      .companion-moon-halo…               ← CSS 气晕，WebGL 失败时才作为主外观
      button.companion-moon-body          ← 唯一可点
          .companion-moon-disc            ← idle/listening 可见；thinking/speaking 透明
  .companion-status                       ← 现有胶囊，文案仍中文
  .companion-prompts                      ← 层3，仅 idle
  .companion-subtitles                    ← 现有本轮字幕
  .companion-chrome                       ← 退出 / 打断
  .companion-ask                          ← 玻璃输入，走 beginUserTurn
```

z-index：aurora 0 → moon visual 1 → button 2 → status/chrome 5 → subtitles 4 → prompts 3。

---

## 5. 层 1 — Aurora 背景（替换星空）

### 从哪拷

Vendor：`web/src/session/companion/visual/Aurora.tsx`  
来源：React Bits `src/ts-tailwind/Backgrounds/Aurora/Aurora.tsx`（MIT，文件头保留版权）。  
依赖：`ogl` 的 `Renderer, Program, Mesh, Color, Triangle`。

### 接到舞台

`CompanionStage.tsx` 删掉 `COMPANION_STARS` 和 `.companion-stars`。改为：

```tsx
<div className="companion-aurora" aria-hidden="true">
  <Aurora
    colorStops={['#1b3a7a', '#5b2cff', '#3fd6ff']}
    amplitude={surfaceState === 'speaking' ? 0.85 + gain * 0.4 : 0.55}
    blend={0.55}
    speed={surfaceState === 'idle' ? 0.45 : surfaceState === 'listening' ? 0.8 : 0.6}
  />
</div>
```

色停刻意 **偏月汐冷青紫**，不用视频里的酸绿 `#7cff67`（那是演示站默认，装在月伴里会像另一款产品）。

CSS：`.companion-aurora{position:absolute;inset:0;pointer-events:none;z-index:0}`，canvas `width/height 100%`。舞台底仍 `#000`。

`prefers-reduced-motion` / WebGL 失败：不挂 Aurora，保留一层静态径向暗紫（CSS），不要空黑。

测试：`querySelector('.companion-aurora')[aria-hidden=true]` 替换原来的 `.companion-stars`。

---

## 6. 层 2 — 月亮：同一团光、两种外形

### 6.1 真机结论：不要做真正的网格 morph

Orb 源码里波动是：

```glsl
uv.x += hover * hoverIntensity * 0.1 * sin(uv.y * 10.0 + iTime);
uv.y += hover * hoverIntensity * 0.1 * sin(uv.x * 10.0 + iTime);
```

Strands 说话外形是水平包络：

```glsl
float env = pow(max(cos(uv.x * PI * 1.3), 0.0), uTaper);
float w = sin(...) * 0.60 + sin(...) * 0.40;
```

`glass` 通道用球方程 `z = sqrt(r*r - d*d)` 做透镜。视频图 1 ≈ Strands+玻璃球；图 2 ≈ 关掉玻璃的同一 shader。

因此 **MoonSphere 内部只维护一个 `visualMode: 'orb' | 'glass' | 'wave'`**：

| `state` | visualMode | 组件 |
|---|---|---|
| idle | `orb` | Orb，`forceHoverState=false`，hue≈210 |
| listening | `orb` | Orb，`forceHoverState=true`，intensity 跟 `levels` 均值 |
| thinking | `glass` | Strands，`glass=true`，`count=3`，`speed=0.55` |
| speaking | `wave` | Strands，`glass=false`，参数跟 `gain` |

idle 仍叠现有 `.companion-moon-disc`（冷白玉），Orb 画在盘外当气晕环，身份还在。thinking 起 disc `opacity:0`，只剩玻璃光球。

### 6.2 调参表（可当常量文件）

`web/src/session/companion/visual/moonVisual.ts`：

```ts
export const MOON_PALETTE = ['#7cffd4', '#7c3aed', '#3fd6ff'] // 月汐：薄荷 / 靛紫 / 潮汐青

export const STRANDS_THINKING = {
  colors: MOON_PALETTE,
  count: 3,
  speed: 0.55,
  amplitude: 0.85,
  waviness: 1,
  thickness: 0.75,
  glow: 2.4,
  glass: true,
  intensity: 0.7,
  scale: 1.15,
  glassSize: 1.05,
}

export function strandsSpeaking(gain: number) {
  const g = Math.max(0.18, Math.min(1, gain))
  return {
    ...STRANDS_THINKING,
    glass: false,
    speed: 0.7 + g * 0.9,
    amplitude: 0.9 + g * 0.7,
    glow: 2.2 + g * 1.4,
    intensity: 0.55 + g * 0.4,
    scale: 1.45,
    taper: 2.4,
  }
}

export function orbListening(level: number) {
  return {
    hue: 205,
    hoverIntensity: 0.45 + Math.min(1, level) * 1.5,
    rotateOnHover: true,
    forceHoverState: true,
    backgroundColor: 'transparent',
  }
}
```

Orb 的 `backgroundColor` 必须透明，否则会盖住舞台极光。需要在 vendor 的 Orb 里把默认 `#000000` 改成透明清屏（`gl.clearColor(0,0,0,0)` 已有，不要再把容器涂黑）。

### 6.3 `MoonSphere.tsx` 替换合同

**对外 props 零变化。** 内部：

1. `useWebglOk()`：一次探测，失败则 `data-visual="css"`，走今天的 halo/disc。
2. `usePrefersReducedMotion()`：true 则同样 CSS，且 Orb/Strands 不 `requestAnimationFrame`。
3. 两个绝对定位层：`.companion-moon-orb`（idle/listening）、`.companion-moon-strands`（thinking/speaking），交叉淡入，避免卸载 WebGL 上下文。
4. 按钮与 `MOON_CLICK_LABELS`、`data-state` 保持。

CSS 增量（仍写在 `styles.css` 的 companion 段）：

- `.companion-moon[data-visual="webgl"] .companion-moon-disc` 在 `state-thinking` / `state-speaking` 时 `opacity:0`
- `.companion-moon-orb, .companion-moon-strands { position:absolute; inset:-18%; pointer-events:none }`
- speaking 时 moon 容器可略拉宽：`width: 48vmin`（现 34vmin），用 `transform: scaleX` 200ms，热区仍圆形以免点偏

### 6.4 性能

月伴是全屏常驻 rAF。约束：

- 只跑 **两个** WebGL 上下文：Aurora 一个、月亮（Orb 与 Strands **复用同一个 canvas**，按 mode 换 Program，不要三套 Renderer）。
- `devicePixelRatio` 封顶 2。
- 舞台 `visibility:hidden`（Esc 退出卸载）时 `loseContext`，与现有 `companion-active` 卸载一致。
- 文档要求：低端核显若掉到 30fps 以下，下一版再加「静态月亮」设置项；本期用探测失败即 CSS。

---

## 7. 层 3 — 空闲轮播问句

默认：`surfaceState === 'idle'` 且 `rounds` 里还没有 user 轮次。开口或进入 thinking/speaking 立即卸载。

实现例外（自动开麦）：舞台入场会立刻 `listening`，严格 idle-only 几乎永远看不到问句。因此 **尚未听到声音、也没有 interim / user 轮次的 listening** 也显示问句；`voiceHeard` 后立刻隐藏。

文案（中文产品，写死本期即可）：

1. 今天天气怎么样？
2. 帮我看一下屏幕上在干什么
3. 读一下刚才那封邮件的要点
4. 把这页整理成待办

交互：按钮，`onClick` → 现有 `beginUserTurn(text)`（与识别 final 同一条路径）。`aria-label`：「试试问：…」。

样式：月亮下方、字幕条上方；半透明 pill，字 13px，4s 换一句，crossfade 400ms。`prefers-reduced-motion` 不自动轮播，四句横排可点。

组件：`web/src/session/companion/CompanionPrompts.tsx`。测：idle 可见；mock 进入 listening 后消失；点击调用 `onSend`。

玻璃输入：`.companion-ask`（不要复用 `.companion-typing` 选择器）。提交走 `beginUserTurn`；思考/说话中提交先 `cancelReply` 再发送。输入框聚焦时 Space 不抢麦（`closest('input')`）。不自动聚焦，以免入场就抢走空格开麦。

---

## 8. 文件级替换清单

| 动作 | 路径 |
|---|---|
| 新增依赖 | `web/package.json`：`"ogl":` 与 React Bits 当前 major 对齐（安装时锁版本） |
| 新增 vendor | `web/src/session/companion/visual/Aurora.tsx` |
| 新增 vendor | `web/src/session/companion/visual/Orb.tsx`（改默认背景透明） |
| 新增 vendor | `web/src/session/companion/visual/Strands.tsx` |
| 新增 | `web/src/session/companion/visual/moonVisual.ts` |
| 新增 | `web/src/session/companion/visual/webglSupport.ts` |
| 新增 | `web/src/session/companion/CompanionPrompts.tsx` |
| 新增 | `web/src/session/companion/CompanionAsk.tsx` |
| 新增测试 | `CompanionPrompts.test.tsx`、`MoonSphere.visual.test.tsx`（断言 data-visual / data-state，不测像素） |
| 改 | `MoonSphere.tsx`（外壳合同不变） |
| 改 | `CompanionStage.tsx`（星空→Aurora；idle 插 Prompts） |
| 改 | `styles.css` companion 段 |
| 改 | `CompanionStage.a11y.test.tsx`：`.companion-stars` → `.companion-aurora` |
| 版权 | 每个 vendor 文件头：`Portions from React Bits, MIT, https://github.com/DavidHDev/react-bits` |

不要改：`useCompanionMachine.ts`、`speech.ts`、`ttsPlayer.ts`、`companionText.ts`、SessionPage 里挂舞台的那一行 props。

---

## 9. Vendor 接入规则（避免以后难升级）

1. 源码复制进 `visual/`，不 `npm i react-bits`（该包是文档站，不是干净的 runtime）。
2. 去掉 Tailwind 类，改用我们已有的 companion class。
3. Orb/Strands/Aurora 的 canvas 由父级定高宽；月亮容器已是 `34vmin`。
4. 禁止在 vendor 里读 `window.chrome`；失败即 throw，由 `webglSupport` 抓住并 CSS 降级。
5. 升级 React Bits 时只 diff 这三个文件的 shader，不要手改业务绑定。

安装命令备忘（实现时用，不在文档里当产品步骤）：

```text
npx shadcn@latest add @react-bits/Aurora-TS-TW
npx shadcn@latest add @react-bits/Orb-JS-CSS
npx shadcn@latest add @react-bits/Strands-JS-CSS
```

仓库策略仍是 **拷贝进 visual/**，CLI 只作取源，不把 shadcn 注册表引进 Renderer。

---

## 10. 无障碍与减弱动态

- 装饰 WebGL：`aria-hidden="true"`。
- 状态仍靠 `.companion-status` 的中文 + `data-state` + 月亮 `aria-label`（现有四句不要改成 thinking/answering）。
- `prefers-reduced-motion: reduce`：停 rAF，Aurora 静态一帧，月亮 CSS 玉盘，问句不自动切。
- 对比度：字幕条、状态胶囊、退出按钮保持现有实底；不要把字叠在纯光带上。

---

## 11. 测试计划

| 用例 | 期望 |
|---|---|
| 现有 companion a11y / recognizer / headstart / task / broadcast | 全绿；只改星空选择器 |
| jsdom 无 WebGL | `data-visual="css"`，月亮按钮仍可点 |
| idle 有 prompts | 4 句之一可见 |
| 点 prompt | `onSend` 收到原文，prompts 消失 |
| `data-state=thinking` | disc 不可见（webgl 路径）或 CSS 思考态（降级） |
| `data-state=speaking` | 容器仍可点打断 |
| reduce-motion | 无无限动画 class（沿用现有 `.reduce-motion` 规则） |

不测：WebGL 截图像素、帧率。真机验收：进月伴看极光；说话看光带；思考看玻璃球。

---

## 12. 分相交付

**Phase 1（本文授权范围，一次 PR）**

1. `ogl` + 三个 vendor  
2. Aurora 换星空  
3. MoonSphere 四状态视觉  
4. idle 问句  
5. 测试与 a11y 选择器

**Phase 2（已落地）**

- 底部玻璃输入 `.companion-ask`
- 走 `beginUserTurn`；思考/说话中先打断再发送
- Space 在输入框聚焦时不抢麦（现有 `closest('input')` 已排除）

**明确不做**

- 首页换极光落地页  
- 英文状态词  
- Three.js / React Three Fiber  
- 把侧栏月亮也改成 Orb  

---

## 13. 实现时的替换顺序（防半残）

1. 加 `ogl`，vendor Aurora，舞台先只换背景，确认 CSS 月亮仍工作。  
2. vendor Orb，idle/listening 叠环，disc 还在。  
3. vendor Strands，thinking/speaking 切换 glass。  
4. 加 Prompts。  
5. 跑 `web` 的 companion 相关 vitest + `typecheck`。  
6. 真机进月伴走一轮听/想/说。

每一步都应可独立回滚：视觉文件可删，舞台回到星空+玉盘。

---

## 14. 验收标准（产品语言）

做完后，进月伴应感到：

1. 背景在缓慢流动，不再是死黑加点。  
2. 安静时仍认得是月亮，不是换成了另一个 App 的彩环。  
3. 一开口，环开始「活」。  
4. 模型思考时变成有体积、会游色的玻璃球（对视频图 1）。  
5. 朗读时摊成横条光带，音量越大越亮、越抖（对视频图 2）。  
6. 空闲能点示例问句；Esc / 打断 / 字幕 / 免提循环与现在一样。

不满足第 6 条则视觉 PR 不能合。
