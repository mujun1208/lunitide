// companionSettings.ts persists the M9.5 Moon Companion settings under
// the existing lunitide:localStorage namespace (zero new tables, zero
// migrations). The engine family picks the synthesis route: "edge"
// (free Microsoft cloud neural voices, default), "natural" (local
// OneCore), "sapi" (classic desktop voices) or "ref" (GPT-SoVITS local
// cloning). rev < 2 installs that used the old OneCore default are
// moved onto the cloud engine once; an explicit later choice is kept.
export type CompanionEngine = 'edge' | 'natural' | 'sapi' | 'ref'

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
const SETTINGS_REV = 2

export const defaultCompanionSettings = (): CompanionSettings => ({
  enabled: true,
  autoSpeak: true,
  wakeWord: true,
  voiceId: '',
  rate: 4,
  volume: 80,
  engine: 'edge',
  refEndpoint: '',
})

export function loadCompanionSettings(): CompanionSettings {
  const fallback = defaultCompanionSettings()
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as Partial<CompanionSettings> & { rev?: number }
    let engine: CompanionEngine = isEngine(parsed.engine) ? parsed.engine : fallback.engine
    let voiceId = typeof parsed.voiceId === 'string' ? parsed.voiceId : ''
    const rev = typeof parsed.rev === 'number' ? parsed.rev : 0
    if (rev < SETTINGS_REV && engine === 'natural') {
      engine = 'edge'
      if (voiceId.startsWith('HKEY_')) voiceId = ''
    }
    const next: CompanionSettings = {
      enabled: typeof parsed.enabled === 'boolean' ? parsed.enabled : fallback.enabled,
      autoSpeak: typeof parsed.autoSpeak === 'boolean' ? parsed.autoSpeak : fallback.autoSpeak,
      wakeWord: typeof parsed.wakeWord === 'boolean' ? parsed.wakeWord : fallback.wakeWord,
      voiceId,
      rate: clampInt(parsed.rate ?? fallback.rate, -10, 10),
      volume: clampInt(parsed.volume ?? fallback.volume, 0, 100),
      engine,
      refEndpoint: typeof parsed.refEndpoint === 'string' ? parsed.refEndpoint : '',
    }
    if (rev < SETTINGS_REV) saveCompanionSettings(next)
    return next
  } catch {
    return fallback
  }
}

export function saveCompanionSettings(settings: CompanionSettings): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ ...settings, rev: SETTINGS_REV }))
  } catch {
    // Storage unavailable (private mode etc.) — settings stay in-memory.
  }
}

function isEngine(value: unknown): value is CompanionEngine {
  return value === 'edge' || value === 'natural' || value === 'sapi' || value === 'ref'
}

function clampInt(value: unknown, lo: number, hi: number): number {
  const n = Math.round(Number(value))
  if (!Number.isFinite(n)) return lo
  return Math.min(hi, Math.max(lo, n))
}
