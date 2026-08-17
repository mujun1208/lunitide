// companionSettings.ts persists the M9.5 Moon Companion settings under
// the existing lunitide:localStorage namespace (zero new tables, zero
// migrations). The engine family picks the synthesis route: "natural"
// (local OneCore neural voices, default), "sapi" (classic desktop
// voices) or "ref" (GPT-SoVITS local service cloning the built-in 18
// character voices). Legacy "edge" values stored by pre-1.0 builds fall
// back to "natural". refEndpoint overrides the GPT-SoVITS api_v2
// address (empty = backend default http://127.0.0.1:9880).
export type CompanionEngine = 'natural' | 'sapi' | 'ref'

export interface CompanionSettings {
  enabled: boolean
  autoSpeak: boolean
  wakeWord: boolean
  voiceId: string
  rate: number
  volume: number
  engine: CompanionEngine
  refEndpoint: string
}

const STORAGE_KEY = 'lunitide:companion'

export const defaultCompanionSettings = (): CompanionSettings => ({
  enabled: true,
  autoSpeak: true,
  wakeWord: true,
  voiceId: '',
  rate: 4,
  volume: 80,
  engine: 'natural',
  refEndpoint: '',
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
      wakeWord: typeof parsed.wakeWord === 'boolean' ? parsed.wakeWord : fallback.wakeWord,
      voiceId: typeof parsed.voiceId === 'string' ? parsed.voiceId : '',
      rate: clampInt(parsed.rate ?? fallback.rate, -10, 10),
      volume: clampInt(parsed.volume ?? fallback.volume, 0, 100),
      engine: isEngine(parsed.engine) ? parsed.engine : fallback.engine,
      refEndpoint: typeof parsed.refEndpoint === 'string' ? parsed.refEndpoint : '',
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

function isEngine(value: unknown): value is CompanionEngine {
  return value === 'natural' || value === 'sapi' || value === 'ref'
}

function clampInt(value: unknown, lo: number, hi: number): number {
  const n = Math.round(Number(value))
  if (!Number.isFinite(n)) return lo
  return Math.min(hi, Math.max(lo, n))
}
