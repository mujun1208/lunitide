import type { CompanionState } from '../useCompanionMachine'

/** Same iridescent glass tones as the idle orb — pink / gold / mint / lilac. */
export const MOON_PALETTE = ['#ff7eb3', '#ffd36a', '#7dffc3', '#8eb6ff'] as const

export const AURORA_STOPS = ['#8eb6ff', '#7dffc3', '#ff7eb3'] as const

/** Official React Bits Aurora stops — homepage atmosphere only. */
export const LAUNCH_AURORA_DARK = ['#5227FF', '#7CFF67', '#5227FF'] as const
export const LAUNCH_AURORA_LIGHT = ['#00d8ff', '#7cff67', '#00d8ff'] as const

export function launchAuroraProps(theme: 'dark' | 'light') {
  return {
    colorStops: theme === 'light' ? LAUNCH_AURORA_LIGHT : LAUNCH_AURORA_DARK,
    lightMode: theme === 'light',
    amplitude: 1,
    blend: 0.5,
    speed: 0.55,
  }
}

export type MoonVisualMode = 'orb' | 'glass' | 'wave'

export function moonVisualMode(state: CompanionState): MoonVisualMode {
  if (state === 'thinking') return 'glass'
  if (state === 'speaking') return 'wave'
  return 'orb'
}

function mix(from: number, to: number, t: number): number {
  return from + (to - from) * Math.min(1, Math.max(0, t))
}

export function auroraSpeed(state: CompanionState): number {
  if (state === 'idle') return 0.42
  if (state === 'listening') return 0.58
  return 0.5
}

export function auroraAmplitude(state: CompanionState, gain: number): number {
  if (state === 'speaking') return 1.05 + Math.min(1, gain) * 0.2
  if (state === 'listening') return 1.0
  return 0.92
}

export function auroraForEnter(state: CompanionState, gain: number, enter: number) {
  return {
    amplitude: mix(0.08, auroraAmplitude(state, gain), enter),
    speed: mix(0.12, auroraSpeed(state), enter),
    blend: mix(0.38, 0.52, enter),
  }
}

export const STRANDS_THINKING = {
  colors: [...MOON_PALETTE],
  count: 3,
  speed: 0.55,
  amplitude: 0.85,
  waviness: 1,
  thickness: 0.75,
  glow: 2.4,
  taper: 3,
  spread: 1,
  hueShift: 0.12,
  intensity: 0.7,
  saturation: 1.35,
  opacity: 1,
  scale: 1.15,
  glass: true,
  refraction: 1,
  dispersion: 1,
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
    // Match thinking's scale so the orb does NOT shrink when she starts
    // speaking (Issue 5). A higher uScale zooms the plasma OUT (smaller ball);
    // 1.45 made it visibly smaller than the thinking orb. Liveliness stays in
    // the gain-driven speed/amplitude/glow above, not in a size jump.
    scale: 1.15,
    taper: 2.4,
  }
}

export function orbProps(state: CompanionState, level: number, enter = 1) {
  const listening = state === 'listening'
  const e = Math.min(1, Math.max(0, enter))
  return {
    hue: 0,
    hoverIntensity: listening ? 0.28 + Math.min(1, level) * 0.22 : mix(0.04, 0.22, e),
    rotateOnHover: false,
    forceHoverState: e > 0.28,
    backgroundColor: 'transparent',
    moonFill: 1 - e,
    ringGain: mix(0.15, 0.55, e),
    moonRadius: 0.8,
    idleDrift: mix(0.03, 0.1, e),
  }
}

export const COMPANION_PROMPTS_ZH = [
  '今天天气怎么样？',
  '帮我看一下屏幕上在干什么',
  '读一下刚才那封邮件的要点',
  '把这页整理成待办',
] as const

export const COMPANION_PROMPTS_EN = [
  'What’s the weather today?',
  'What is on my screen right now?',
  'Summarize that last email',
  'Turn this page into todos',
] as const
