# 月伴第二种模式「星尘」进产品 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Do not commit unless the user asks.

**Goal:** 把已定稿的星尘仿真原样挂进月伴全屏舞台，成为可切换的第二种脸（默认仍是玉盘）。

**Architecture:** 不改粒子配方 / 着色器常量 / `engageMorph`。`CompanionSettings.visualSkin` 持久化（不升 `SETTINGS_REV`）。`CompanionStage` 按皮肤二选一挂载：玉盘走现网 `Aurora`+`MoonSphere`；星尘走 `ParticleMoonScene` + 透明热区。语音口令在 `beginUserTurn` 里拦截，短确认走 `TtsPlayer.enqueue`，不 `chat.start`。

**Tech Stack:** React + OGL 现依赖；Vitest；`localStorage` 键 `lunitide:companion`。

**Spec:** [docs/superpowers/specs/2026-09-05-companion-execute-followthrough-prd.md](../specs/2026-09-05-companion-execute-followthrough-prd.md) 第一部分 V1–V6（以复核补丁后的接线锁为准）。

## Global Constraints

- 不改 `MoonSphere.tsx` / `Orb.tsx` / `Strands.tsx` / `Aurora.tsx` / `moonVisual.ts` / `CompanionVisualPreview.tsx` 像素
- 不改 `RINGS`、着色器数字、`engageMorph` 时间轴、粒子预算、相机、行星半径
- 不升 `SETTINGS_REV`（保持 13）；缺 `visualSkin` 回 `'classic'`
- 不改 poison / TTS 音色 / 专家表 / VERSION / `useCompanionMachine`
- 不接执行收口（闲聊门 / 火山窗 / 预批准）
- 星尘态禁止再挂 `MoonSphere`（会叠玉盘 WebGL）
- 口播禁止走 `speakCompanionPad`（那是「嗯」）
- PowerShell 用 `;` 连接命令，不要用 `&&`
- 不 commit，除非用户明确要求

## File map

| File | Responsibility |
|---|---|
| `web/src/session/companion/particle/particleMoon.ts` | 口令解析、`nextCompanionSkin`、确认词、`consumeCompanionSkinCommand` |
| `web/src/session/companion/particle/particleMoon.test.ts` | 上表单测 |
| `web/src/session/companion/companionSettings.ts` | `visualSkin` 读写，不升 rev |
| `web/src/session/companion/companionSettings.test.ts` | 默认 / 非法 / 持久化 |
| `web/src/session/companion/CompanionSkinSwitch.tsx` | 玉盘\|星尘两段开关（设置页 + 顶栏复用） |
| `web/src/settings/SettingsPage.tsx` | 「启用月伴对话」下挂开关 |
| `web/src/session/companion/CompanionStage.tsx` | `data-skin`、分支挂载、拦截、enqueue |
| `web/src/session/companion/CompanionStage.a11y.test.tsx` | 星尘仍有 `.companion-moon`；口令不 `onSend` |
| `web/src/session/companion/particle/ParticleMoonScene.tsx` | **只**把 pointermove 改听 `.companion-stage`（接线，不改配方） |
| `web/src/session/companion/particle/ParticleMoonPreview.tsx` | 对照文案 |
| `web/src/styles.css` | `data-skin=particle` 藏玉盘 disc/halo；开关样式 |

不改：`particleGeometry.ts`、玉盘 visual 目录、`SessionPage.tsx` 布局、Go 引擎。

---

## Task 1: 纯函数 — 皮肤字段与口令

**Files:** `particleMoon.ts`, `particleMoon.test.ts`, `companionSettings.ts`, `companionSettings.test.ts`

- [ ] 打开 `particleMoon.test.ts`。在 `parseCompanionSkinCommand` describe 末尾追加：

```ts
    expect(parseCompanionSkinCommand('换成粒子月亮看看。')).toBe('particle')
    expect(parseCompanionSkinCommand('换成粒子月亮吧')).toBe('particle')
    expect(parseCompanionSkinCommand('帮我换成粒子月亮然后查天气')).toBeNull()
```

并在文件里追加：

```ts
describe('companion skin helpers', () => {
  test('toggle and confirm copy', () => {
    expect(nextCompanionSkin('classic', 'toggle')).toBe('particle')
    expect(nextCompanionSkin('particle', 'toggle')).toBe('classic')
    expect(nextCompanionSkin('classic', 'classic')).toBe('classic')
    expect(companionSkinConfirmSpeech('classic', 'particle')).toBe('好，换成星尘。')
    expect(companionSkinConfirmSpeech('particle', 'classic')).toBe('好，换回玉盘。')
    expect(companionSkinConfirmSpeech('particle', 'particle')).toBe('已经是星尘了。')
    expect(companionSkinConfirmSpeech('classic', 'classic')).toBe('已经是玉盘了。')
  })
  test('consume intercepts only exact skin lines', () => {
    expect(consumeCompanionSkinCommand('切换风格', 'classic')).toEqual({
      next: 'particle',
      speech: '好，换成星尘。',
    })
    expect(consumeCompanionSkinCommand('今天合肥的天气怎么样', 'classic')).toBeNull()
  })
})
```

从 `./particleMoon` 补 import：`nextCompanionSkin`, `companionSkinConfirmSpeech`, `consumeCompanionSkinCommand`。

- [ ] 打开 `companionSettings.test.ts` 追加：

```ts
it('defaults visualSkin to classic and rejects junk', () => {
  expect(loadCompanionSettings().visualSkin).toBe('classic')
  expect(defaultCompanionSettings().visualSkin).toBe('classic')
  saveCompanionSettings({ ...defaultCompanionSettings(), visualSkin: 'particle' })
  expect(loadCompanionSettings().visualSkin).toBe('particle')
  localStorage.setItem('lunitide:companion', JSON.stringify({ ...defaultCompanionSettings(), visualSkin: 'jarvis' }))
  expect(loadCompanionSettings().visualSkin).toBe('classic')
})
```

- [ ] 跑红测：

```powershell
cd web; npx vitest run src/session/companion/particle/particleMoon.test.ts src/session/companion/companionSettings.test.ts
```

预期：新断言 FAIL（函数不存在 / 字段不存在 / 「换成粒子月亮看看」为 null）。

- [ ] `particleMoon.ts`：把 `CompanionSkinCommand` 旁加上：

```ts
export type CompanionSkin = 'classic' | 'particle'

export function nextCompanionSkin(current: CompanionSkin, command: CompanionSkinCommand): CompanionSkin {
  if (command === 'toggle') return current === 'particle' ? 'classic' : 'particle'
  return command
}

export function companionSkinConfirmSpeech(from: CompanionSkin, to: CompanionSkin): string {
  if (from === to) return to === 'particle' ? '已经是星尘了。' : '已经是玉盘了。'
  return to === 'particle' ? '好，换成星尘。' : '好，换回玉盘。'
}

export function consumeCompanionSkinCommand(
  text: string,
  current: CompanionSkin,
): { next: CompanionSkin; speech: string } | null {
  const hit = parseCompanionSkinCommand(text)
  if (!hit) return null
  const next = nextCompanionSkin(current, hit)
  return { next, speech: companionSkinConfirmSpeech(current, next) }
}
```

改 `parseCompanionSkinCommand`：

```ts
export function parseCompanionSkinCommand(text: string): CompanionSkinCommand | null {
  const t = text
    .replace(/\s+/g, '')
    .replace(/[。.?？!！]+$/g, '')
    .replace(/(看看|吧|呀|啊)+$/g, '')
  if (!t) return null
  if (/^(切换风格|换个风格|换成另一套)$/.test(t)) return 'toggle'
  if (/^(星尘风格|粒子月亮|贾维斯风格|换成粒子|换成粒子月亮)$/.test(t)) return 'particle'
  if (/^(玉盘风格|普通月亮|原来的月亮|换回普通月亮)$/.test(t)) return 'classic'
  return null
}
```

- [ ] `companionSettings.ts`：

在 `CompanionSettings` 接口末尾（`omniPersonaId` 后）加：

```ts
  /** classic = 现网玉盘；particle = 定稿星尘。缺省 classic。 */
  visualSkin: 'classic' | 'particle'
```

`defaultCompanionSettings()` 加 `visualSkin: 'classic'`。

在 `loadCompanionSettings` 的 `next` 对象里加（和其他可选字段一样，**不要**改 `SETTINGS_REV`）：

```ts
      visualSkin: parsed.visualSkin === 'particle' ? 'particle' : 'classic',
```

`parsed` 类型已是 `Partial<CompanionSettings>`，够用。禁止把 `SETTINGS_REV` 改成 14。

- [ ] 同一 vitest 命令必须绿。
- [ ] Commit only if asked: `Persist companion visualSkin and lock skin voice phrases.`

---

## Task 2: 共用开关组件 + 设置页

**Files:** create `web/src/session/companion/CompanionSkinSwitch.tsx`, `web/src/settings/SettingsPage.tsx`, `web/src/styles.css`

- [ ] 新建 `CompanionSkinSwitch.tsx`：

```tsx
import type { CompanionSkin } from './particle/particleMoon'

export function CompanionSkinSwitch({
  value,
  onChange,
  compact = false,
  zh = true,
}: {
  value: CompanionSkin
  onChange: (next: CompanionSkin) => void
  compact?: boolean
  zh?: boolean
}): React.JSX.Element {
  const classic = zh ? '玉盘' : 'Jade'
  const particle = zh ? '星尘' : 'Stardust'
  return (
    <div
      className={`companion-skin-switch${compact ? ' is-compact' : ''}`}
      role="radiogroup"
      aria-label={zh ? '月伴外观' : 'Companion look'}
    >
      <button type="button" role="radio" aria-checked={value === 'classic'} aria-pressed={value === 'classic'} onClick={() => onChange('classic')}>
        {classic}
      </button>
      <button type="button" role="radio" aria-checked={value === 'particle'} aria-pressed={value === 'particle'} onClick={() => onChange('particle')}>
        {particle}
      </button>
    </div>
  )
}
```

- [ ] `styles.css` 在 `.companion-interrupt-key` 附近追加：

```css
.companion-skin-switch{display:inline-flex;border:1px solid rgba(143,170,216,.35);border-radius:999px;overflow:hidden;background:rgba(14,24,40,.9)}
.companion-skin-switch button{padding:7px 12px;border:0;background:transparent;color:inherit;font-size:12.5px;cursor:pointer}
.companion-skin-switch button[aria-pressed="true"]{background:rgba(63,214,255,.18)}
.companion-skin-switch.is-compact button{padding:6px 10px}
.setting-row .companion-skin-switch{flex:none}
```

星尘藏玉盘脸（Stage 热区会带 `.companion-moon` class）：

```css
.companion-stage[data-skin="particle"] .companion-moon-halo,
.companion-stage[data-skin="particle"] .companion-moon-halo-wave,
.companion-stage[data-skin="particle"] .companion-moon-disc,
.companion-stage[data-skin="particle"] .companion-moon-orb,
.companion-stage[data-skin="particle"] .companion-moon-strands{display:none!important}
```

- [ ] `SettingsPage.tsx`：在「启用月伴对话」那个 `<Toggle label="启用月伴对话"` 的闭合 `/>` **后面**插入（这一行很长，只插这一处）：

```tsx
<div className="setting-row"><div><div className="setting-label">月伴外观</div><div className="setting-desc">玉盘是现在的月亮；星尘是粒子星河。说「切换风格」也能换。</div></div><CompanionSkinSwitch value={companion.visualSkin==='particle'?'particle':'classic'} onChange={skin=>save({...companion,visualSkin:skin})} zh/></div>
```

文件顶部补 import：`import { CompanionSkinSwitch } from '../session/companion/CompanionSkinSwitch'`。

- [ ] 若有 `SettingsPage` 相关测会因缺 `visualSkin` 炸：`defaultCompanionSettings` 已带字段，不应炸。跑：

```powershell
cd web; npx vitest run src/session/companion/companionSettings.test.ts src/settings
```

- [ ] Commit only if asked: `Add companion skin switch on the voice settings page.`

---

## Task 3: 舞台分支 + 透明热区 + 顶栏开关

**Files:** `CompanionStage.tsx`, `ParticleMoonScene.tsx`, `CompanionStage.a11y.test.tsx`

- [ ] 在 `CompanionStage.a11y.test.tsx` 的 MC-06 describe **之后**加：

```ts
describe('companion visual skin', () => {
  test('classic stays the default face', async () => {
    const { container } = await renderStage()
    expect(stage(container).getAttribute('data-skin')).toBe('classic')
    expect(container.querySelector('.companion-moon')).not.toBeNull()
  })

  test('particle skin keeps a moon hit target and does not send chat', async () => {
    saveCompanionSettings({ ...defaultCompanionSettings(), visualSkin: 'particle' })
    const onSend = vi.fn()
    const { container } = await renderStage({ onSend })
    expect(stage(container).getAttribute('data-skin')).toBe('particle')
    expect(container.querySelector('.companion-moon')).not.toBeNull()
    expect(container.querySelector('.companion-moon-body')?.getAttribute('aria-label')).toBe('月亮：轻点开始说话')
  })
})
```

文件顶部补：`import { defaultCompanionSettings, saveCompanionSettings } from './companionSettings'`。`beforeEach` 已 `localStorage.clear()`，不用再改。

- [ ] 跑红测（预期 FAIL：没有 `data-skin`）：

```powershell
cd web; npx vitest run src/session/companion/CompanionStage.a11y.test.tsx
```

- [ ] `ParticleMoonScene.tsx` 的 `onMove` 监听目标改成舞台（现码绑在 host 上，产品里 host 在 `pointer-events:none` 的 aurora 里收不到鼠标）：

把

```ts
    window.addEventListener('resize', onResize)
    host.addEventListener('pointermove', onMove)
```

改成：

```ts
    const pointerRoot = host.closest('.companion-stage') ?? host
    window.addEventListener('resize', onResize)
    pointerRoot.addEventListener('pointermove', onMove)
```

cleanup 里 `host.removeEventListener('pointermove', onMove)` 改成 `pointerRoot.removeEventListener('pointermove', onMove)`。不要改着色器、相机、`RINGS`、行星。

- [ ] `CompanionStage.tsx`：

1. import：

```ts
import { ParticleMoonScene } from './particle/ParticleMoonScene'
import { CompanionSkinSwitch } from './CompanionSkinSwitch'
import type { CompanionSkin } from './particle/particleMoon'
```

2. 在 `settings` state 旁加（若已有 `useCompanionSettings` 就读 `settings.visualSkin`）：

舞台里已有 `settings` / `saveCompanionSettings`。加：

```ts
  const visualSkin: CompanionSkin = settings.visualSkin === 'particle' ? 'particle' : 'classic'
  const [dustHigh, setDustHigh] = useState(true)
  const lowFps = useRef(0)
  const applyVisualSkin = (next: CompanionSkin) => {
    const current = loadCompanionSettings()
    saveCompanionSettings({ ...current, visualSkin: next })
    setSettings(loadCompanionSettings())
  }
```

`setSettings` 现码已有（约 550 行 `refresh`）。沿用同一套。

3. 根 `div.companion-stage` 加 `data-skin={visualSkin}`。

4. 替换 aurora 槽：

```tsx
      <div className="companion-aurora" aria-hidden="true">
        {visualSkin === 'particle' ? (
          canUseCompanionWebgl() ? (
            <ParticleMoonScene
              state={surfaceState}
              gain={gain}
              levels={levels}
              burstToken={0}
              high={dustHigh}
              onFps={fps => {
                if (fps && fps < 40 && dustHigh) {
                  lowFps.current += 1
                  if (lowFps.current >= 8) setDustHigh(false)
                  return
                }
                lowFps.current = 0
              }}
            />
          ) : (
            <div className="stardust-sim-fallback" />
          )
        ) : canUseCompanionWebgl() ? (
          <Aurora colorStops={AURORA_STOPS} {...auroraForEnter(surfaceState, gain, enter)} />
        ) : (
          <div className="companion-aurora-fallback" />
        )}
      </div>
```

`surfaceState` / `gain` / `levels` / `enter` 在 return 里已经算好，aurora 现在在 return 里——保持这个顺序，不要提前用。

5. 月亮槽：`visualSkin==='classic'` 时现网 `<MoonSphere .../>` 一字不改。`particle` 时只画热区：

```tsx
      <div className="companion-moon-slot">
      {visualSkin === 'classic' ? (
      <MoonSphere
        state={surfaceState}
        gain={gain}
        levels={levels}
        enter={enter}
        interruptible={surfaceState !== 'listening' || audioLocked}
        onInterrupt={
          machine.state === 'speaking' || machine.state === 'thinking' || surfaceState === 'speaking' || surfaceState === 'thinking'
            ? interruptAssistant
            : surfaceState === 'listening'
              ? () => {
                  void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
                }
              : toggleMic
        }
      />
      ) : (
      <div className={`companion-moon state-${surfaceState}`} data-state={surfaceState} data-visual="css">
        <button
          type="button"
          className="companion-moon-body"
          tabIndex={-1}
          aria-label={
            surfaceState === 'idle'
              ? '月亮：轻点开始说话'
              : surfaceState === 'listening'
                ? '月亮正在聆听'
                : surfaceState === 'thinking'
                  ? '月亮正在回应'
                  : '月亮正在说话，点击打断朗读'
          }
          onClick={
            machine.state === 'speaking' || machine.state === 'thinking' || surfaceState === 'speaking' || surfaceState === 'thinking'
              ? interruptAssistant
              : surfaceState === 'listening'
                ? () => {
                    void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
                  }
                : toggleMic
          }
        />
      </div>
      )}
      </div>
```

6. `.companion-chrome` 里，「暂停」按钮和 `CompanionEntryLights` **之间**插入：

```tsx
        <CompanionSkinSwitch compact zh={zh} value={visualSkin} onChange={applyVisualSkin} />
```

- [ ] 同一 a11y vitest 必须绿（旧 MC-06 + 新 skin 两测）。
- [ ] Commit only if asked: `Mount locked stardust scene as a switchable companion skin.`

---

## Task 4: 语音拦截 + enqueue 确认

**Files:** `CompanionStage.tsx`, `CompanionStage.a11y.test.tsx`

- [ ] 在 Task 3 的 `companion visual skin` describe 再加：

```ts
  test('skin voice command does not call onSend and speaks confirm', async () => {
    const onSend = vi.fn()
    const { container, rerender } = await renderStage({ onSend })
    speech.start.mockResolvedValue(speech.handle())
    fireEvent.click(moonBody(container))
    await waitFor(() => expect(speech.start).toHaveBeenCalled())
    await act(async () => {
      speech.callbacks?.onFinal?.('切换风格')
    })
    rerender(<CompanionStage {...baseProps} onSend={onSend} />)
    expect(onSend).not.toHaveBeenCalled()
    expect(tts.enqueueCalls.some(call => call.segments.join('').includes('星尘'))).toBe(true)
    expect(loadCompanionSettings().visualSkin).toBe('particle')
  })

  test('weather still goes to onSend', async () => {
    const onSend = vi.fn()
    const { container } = await renderStage({ onSend })
    speech.start.mockResolvedValue(speech.handle())
    fireEvent.click(moonBody(container))
    await waitFor(() => expect(speech.start).toHaveBeenCalled())
    await act(async () => {
      speech.callbacks?.onFinal?.('今天合肥的天气怎么样')
    })
    await waitFor(() => expect(onSend).toHaveBeenCalledWith('今天合肥的天气怎么样'))
  })
```

补 import：`act` 已有；`loadCompanionSettings` 并进 companionSettings import。

- [ ] 跑红测（拦截未写，`切换风格` 会 `onSend`）。

- [ ] `CompanionStage.tsx` 的 `beginUserTurn`，在

```ts
      const text = clipCompanionPrompt(cleanUserTranscript(transcript))
```

**之后**立刻插入（`setRounds` / `onSend` / `RECOGNIZED_FINAL` / `speakCompanionPad` 之前）：

```ts
      const skinTurn = consumeCompanionSkinCommand(text, settingsRef.current.visualSkin === 'particle' ? 'particle' : 'classic')
      if (skinTurn) {
        const stored = loadCompanionSettings()
        saveCompanionSettings({ ...stored, visualSkin: skinTurn.next })
        setSettings(loadCompanionSettings())
        setInterimText('')
        if (settingsRef.current.autoSpeak && ttsAvailableRef.current !== false) {
          void unlockTtsAudio()
          ensurePlayer().enqueue([skinTurn.speech], { ...settingsRef.current, voiceId: activeVoiceId() }, {})
        } else {
          setEngineHint(skinTurn.speech)
        }
        return
      }
```

import `consumeCompanionSkinCommand`。`settingsRef` / `ensurePlayer` / `activeVoiceId` / `setEngineHint` 现码已有。不要调用 `speakCompanionPad`。不要 `applyEvent({ type: 'RECOGNIZED_FINAL' })`。

- [ ] 同一 a11y + particle + settings vitest 绿：

```powershell
cd web; npx vitest run src/session/companion/CompanionStage.a11y.test.tsx src/session/companion/particle src/session/companion/companionSettings.test.ts
```

若 `activeVoiceId` 在 `beginUserTurn` 的 `useCallback` 依赖里缺失，补进依赖数组。`ensurePlayer` 同样。

- [ ] Commit only if asked: `Intercept companion skin voice lines without starting a chat turn.`

---

## Task 5: 预览对照文案 + 手工

**Files:** `ParticleMoonPreview.tsx`

- [ ] 把 `LINES.speaking.text` 从「这是仿真页，还没进产品。」改成「这是定稿对照。产品里用玉盘 / 星尘切换。」
- [ ] 把 brand 段「独立仿真，不接产品。」改成「定稿对照；产品舞台用顶栏或设置切换。」
- [ ] 手工（本机）：

```powershell
cd web; npm run preview:companion-particle
```

对照仍是定稿星河。再：

```powershell
cd web; npm run dev
```

进月伴：默认玉盘；设置或顶栏切星尘，画面与预览同一构图；说「换成粒子月亮看看」只换脸、会话不出现这句用户回合；说「今天合肥的天气怎么样」仍进引擎（本轨不修空等）；无 WebGL 时星尘仍有圆。

- [ ] `cd web; npx vitest run src/session/companion` 绿。
- [ ] **停。** 不要改 `DefaultEndWindowMS`、不要改 `companionWantsTools`、不要改 VERSION。

---

## 规格覆盖（计划自检）

| 规格 | 任务 |
|---|---|
| V1 默认玉盘 / 非法回 classic | Task 1 |
| V1 三路切换同一设置 | Task 2–4 |
| V1 语音不进引擎 | Task 4 |
| V1 点月亮打断 / a11y `.companion-moon` | Task 3 |
| V1 不改玉盘像素 / 不改星尘配方 | 全局约束 |
| V2 画面数字 | 不改 particle 配方文件 |
| V3.1 不升 rev | Task 1 |
| V3.2 分支挂载 / 透明热区 / 视差听 stage | Task 3 |
| V3.3 顶栏开关 | Task 3 |
| V3.4 enqueue 确认词 / 看看后缀 | Task 1 + 4 |
| V3.5 预览对照文案 | Task 5 |
| V-P1–V-P8 | Task 5 手工 |

执行轨 §0–§10 **不在本计划**。走 [2026-09-05-companion-execute-followthrough.md](./2026-09-05-companion-execute-followthrough.md)，且 Task 6 按「有限恢复」落地。
