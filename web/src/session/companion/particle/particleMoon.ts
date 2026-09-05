export type ParticleMoonState = 'idle' | 'listening' | 'thinking' | 'speaking'

export type CompanionSkinCommand = 'toggle' | 'particle' | 'classic'
export type CompanionSkin = 'classic' | 'particle'

export const SHAPE_SPHERE = 0
export const SHAPE_LOTUS = 1
export const SHAPE_TORUS = 2
export const SHAPE_HELIX = 3
export const SHAPE_STAR = 4

export const THINK_SHRINK_SEC = 1.15
export const THINK_HOLD_SEC = 0.7
export const THINK_STEP_SEC = 1.9
export const THINK_SLOT_SEC = THINK_HOLD_SEC + THINK_STEP_SEC
export const ENGAGE_SHAPES = [SHAPE_LOTUS, SHAPE_TORUS, SHAPE_HELIX, SHAPE_STAR] as const
export const THINK_SHAPES = ENGAGE_SHAPES

export type ParticleMoonTargets = {
  spinPeriodSec: number
  spinSpeed: number
  scatter: number
  form: number
  radius: number
  pulse: number
  coreScale: number
  inhale: number
  aura: number
  shape: number
}

const SPIN: Record<ParticleMoonState, number> = {
  idle: 36,
  listening: 30,
  thinking: 30,
  speaking: 22,
}

export function isEngageState(state: ParticleMoonState): boolean {
  return state === 'listening' || state === 'thinking'
}

export function clamp01(value: number): number {
  if (!Number.isFinite(value)) return 0
  if (value <= 0) return 0
  if (value >= 1) return 1
  return value
}

export function pingpongIndex(x: number, n: number): number {
  if (n <= 1) return 0
  const period = 2 * (n - 1)
  let m = x % period
  if (m < 0) m += period
  return m <= n - 1 ? m : period - m
}

export function morphEase(t: number): number {
  const x = clamp01(t)
  return x * x * x * (x * (x * 6 - 15) + 10)
}

export function engageMorph(
  elapsed: number,
  fromShape = SHAPE_SPHERE,
): { shapeA: number; shapeB: number; mix: number; radius: number } {
  const t = Number.isFinite(elapsed) ? Math.max(0, elapsed) : 0
  if (t < THINK_SHRINK_SEC) {
    const u = t / THINK_SHRINK_SEC
    return {
      shapeA: fromShape,
      shapeB: SHAPE_SPHERE,
      mix: u,
      radius: 0.5 - 0.2 * u,
    }
  }
  if (t < THINK_SHRINK_SEC + THINK_HOLD_SEC) {
    return { shapeA: SHAPE_SPHERE, shapeB: SHAPE_SPHERE, mix: 0, radius: 0.28 }
  }
  const u = pingpongIndex((t - THINK_SHRINK_SEC - THINK_HOLD_SEC) / THINK_SLOT_SEC, ENGAGE_SHAPES.length)
  const i0 = Math.min(ENGAGE_SHAPES.length - 1, Math.floor(u))
  const i1 = Math.min(ENGAGE_SHAPES.length - 1, i0 + 1)
  const frac = u - i0
  const hold = THINK_HOLD_SEC / THINK_SLOT_SEC
  const mix = frac <= hold ? 0 : (frac - hold) / (1 - hold)
  return {
    shapeA: ENGAGE_SHAPES[i0],
    shapeB: ENGAGE_SHAPES[i1],
    mix,
    radius: 0.38,
  }
}

export function thinkingMorph(elapsed: number, fromShape = SHAPE_SPHERE) {
  return engageMorph(elapsed, fromShape)
}

export function particleMoonTargets(state: ParticleMoonState, gain = 0): ParticleMoonTargets {
  const g = clamp01(gain)
  const engage = isEngageState(state)
  const spinPeriodSec = SPIN[state]
  return {
    spinPeriodSec,
    spinSpeed: 1 / spinPeriodSec,
    scatter: state === 'idle' ? 1.34 : state === 'speaking' ? 1.12 : 0.9,
    form: state === 'idle' ? 0.58 : state === 'speaking' ? 0.78 : 0.94,
    radius: state === 'idle' ? 0.52 : state === 'speaking' ? 0.58 : 0.28,
    pulse: state === 'speaking' ? g : 0,
    coreScale: state === 'speaking' ? 0.28 + g * 0.72 : 0.5,
    inhale: engage ? 0.12 : 0,
    aura: engage ? 0.7 : state === 'speaking' ? 0.5 + g * 0.28 : 0.5,
    shape: SHAPE_SPHERE,
  }
}

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

export function particleBudget(high: boolean): { moon: number; nebula: number; stars: number } {
  return high ? { moon: 20000, nebula: 22000, stars: 5600 } : { moon: 9000, nebula: 9000, stars: 2400 }
}
