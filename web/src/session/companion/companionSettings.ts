// companionSettings.ts persists the M9.5 Moon Companion settings under
// the existing lunitide: localStorage namespace (zero new tables, zero
// migrations). Keys mirror the frozen companion defaults from the
// technical design: enabled=true, auto_speak=true, rate=0, volume=80.
export interface CompanionSettings {
  enabled: boolean
  autoSpeak: boolean
  voiceId: string
  rate: number
  volume: number
}

const STORAGE_KEY = 'lunitide:companion'

export const defaultCompanionSettings = (): CompanionSettings => ({
  enabled: true,
  autoSpeak: true,
  voiceId: '',
  rate: 0,
  volume: 80,
})

export function loadCompanionSettings(): CompanionSettings {
  const fallback = defaultCompanionSettings()
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as Partial<CompanionSettings>
    return {
      enabled: typeof parsed.enabled === 'boolean' ? parsed.enabled : fallback.enabled,
      autoSpeak: typeof parsed.autoSpeak === 'boolean' ? parsed.autoSpeak : fallback.autoSpeak,
      voiceId: typeof parsed.voiceId === 'string' ? parsed.voiceId : '',
      rate: clampInt(parsed.rate ?? fallback.rate, -10, 10),
      volume: clampInt(parsed.volume ?? fallback.volume, 0, 100),
    }
  } catch {
    return fallback
  }
}

export function saveCompanionSettings(settings: CompanionSettings): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(settings))
  } catch {
    // Storage unavailable (private mode etc.) — settings stay in-memory.
  }
}

function clampInt(value: unknown, lo: number, hi: number): number {
  const n = Math.round(Number(value))
  if (!Number.isFinite(n)) return lo
  return Math.min(hi, Math.max(lo, n))
}
