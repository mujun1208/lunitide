export const REASONING_LEVELS = ['low', 'high', 'max'] as const

export type ReasoningLevel = (typeof REASONING_LEVELS)[number]

const STORAGE_KEY = 'lunitide:reasoning-level'

export const REASONING_LABELS: Record<ReasoningLevel, { zh: string; en: string }> = {
  low: { zh: '低', en: 'Low' },
  high: { zh: '高', en: 'High' },
  max: { zh: '极高', en: 'Max' },
}

export function reasoningLevelAt(index: number): ReasoningLevel {
  const i = Math.max(0, Math.min(REASONING_LEVELS.length - 1, Math.round(index)))
  return REASONING_LEVELS[i] ?? 'high'
}

export function reasoningLevelIndex(level: ReasoningLevel): number {
  const i = REASONING_LEVELS.indexOf(level)
  return i < 0 ? REASONING_LEVELS.indexOf('high') : i
}

export function loadReasoningLevel(): ReasoningLevel {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw && (REASONING_LEVELS as readonly string[]).includes(raw)) return raw as ReasoningLevel
  } catch {
    /* private mode */
  }
  return 'high'
}

export function saveReasoningLevel(level: ReasoningLevel): void {
  try {
    localStorage.setItem(STORAGE_KEY, level)
  } catch {
    /* private mode */
  }
}
