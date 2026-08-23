// companionSettings.ts persists the M9.5 Moon Companion settings under
// the existing lunitide:localStorage namespace (zero new tables, zero
// migrations). The engine family picks the synthesis route: "edge"
// (free Microsoft cloud neural voices, default), "natural" (local
// OneCore), "sapi" (classic desktop voices) or "ref" (GPT-SoVITS local
// cloning). rev < 2 installs that used the old OneCore default are
// moved onto the cloud engine once; an explicit later choice is kept.
export type CompanionEngine = 'edge' | 'natural' | 'sapi' | 'ref'
/** normal = default endpointing; noisy = tighter mic gate for cafes / shared rooms. */
export type SpeechEnvironment = 'normal' | 'noisy'

export interface InterruptHotkey {
  /** KeyboardEvent.key, letters stored lowercase. */
  key: string
  ctrl: boolean
  alt: boolean
  shift: boolean
}

export interface CompanionSettings {
  enabled: boolean
  autoSpeak: boolean
  wakeWord: boolean
  /** Keep mic open between turns; mute while assistant TTS plays. */
  fullDuplex: boolean
  /** Manual interrupt shortcut while she is thinking or speaking. */
  interruptHotkey: InterruptHotkey
  /** Tighter voice gate + longer silence before commit in loud environments. */
  speechEnvironment: SpeechEnvironment
  voiceId: string
  rate: number
  volume: number
  engine: CompanionEngine
  refEndpoint: string
}

const STORAGE_KEY = 'lunitide:companion'
const SETTINGS_REV = 9

/** Reliable local-first probe order when the primary engine fails. */
const ENGINE_PROBE_FALLBACK: CompanionEngine[] = ['natural', 'edge', 'sapi']

export function companionEngineProbeOrder(primary: CompanionEngine): CompanionEngine[] {
  if (primary === 'ref') return ['ref', ...ENGINE_PROBE_FALLBACK]
  const order: CompanionEngine[] = [primary]
  for (const engine of ENGINE_PROBE_FALLBACK) {
    if (!order.includes(engine)) order.push(engine)
  }
  return order
}

/** Cloud voice ids are invalid on OneCore/SAPI — drop them on engine switch. */
export function voiceIdForEngineSwitch(from: CompanionEngine, to: CompanionEngine, voiceId: string): string {
  if (!voiceId || from === to) return voiceId
  if (from === 'edge' && (to === 'natural' || to === 'sapi')) {
    if (/Neural|::|^zh-CN-/i.test(voiceId)) return ''
  }
  if ((from === 'natural' || from === 'sapi') && to === 'edge' && voiceId.startsWith('HKEY_')) return ''
  return voiceId
}

export const DEFAULT_INTERRUPT_HOTKEY: InterruptHotkey = {
  key: 'Tab',
  ctrl: false,
  alt: false,
  shift: false,
}

export const defaultCompanionSettings = (): CompanionSettings => ({
  enabled: true,
  autoSpeak: true,
  wakeWord: false,
  fullDuplex: true,
  interruptHotkey: { ...DEFAULT_INTERRUPT_HOTKEY },
  speechEnvironment: 'normal',
  voiceId: '',
  rate: 2,
  volume: 88,
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
    let wakeWord = typeof parsed.wakeWord === 'boolean' ? parsed.wakeWord : fallback.wakeWord
    if (rev < SETTINGS_REV) {
      wakeWord = false
      if (engine === 'natural' && voiceId === '') {
        engine = 'edge'
      }
    }
    const interruptHotkey = parseInterruptHotkey(
      (parsed as Partial<CompanionSettings> & { interruptHotkey?: unknown }).interruptHotkey,
    )
    const next: CompanionSettings = {
      enabled: typeof parsed.enabled === 'boolean' ? parsed.enabled : fallback.enabled,
      autoSpeak: typeof parsed.autoSpeak === 'boolean' ? parsed.autoSpeak : fallback.autoSpeak,
      wakeWord,
      fullDuplex: typeof parsed.fullDuplex === 'boolean' ? parsed.fullDuplex : fallback.fullDuplex,
      interruptHotkey,
      speechEnvironment: parsed.speechEnvironment === 'noisy' ? 'noisy' : fallback.speechEnvironment,
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
    window.dispatchEvent(new Event('lunitide:companion-settings'))
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

export function parseInterruptHotkey(raw: unknown): InterruptHotkey {
  if (!raw || typeof raw !== 'object') return { ...DEFAULT_INTERRUPT_HOTKEY }
  const value = raw as Partial<InterruptHotkey>
  const key = typeof value.key === 'string' && value.key.trim() ? normalizeHotkeyKey(value.key) : DEFAULT_INTERRUPT_HOTKEY.key
  if (key === 'Escape') return { ...DEFAULT_INTERRUPT_HOTKEY }
  return {
    key,
    ctrl: value.ctrl === true,
    alt: value.alt === true,
    shift: value.shift === true,
  }
}

export function normalizeHotkeyKey(key: string): string {
  return key.length === 1 ? key.toLowerCase() : key
}

export function interruptHotkeyFromEvent(event: KeyboardEvent): InterruptHotkey | null {
  if (event.key === 'Control' || event.key === 'Shift' || event.key === 'Alt' || event.key === 'Meta') return null
  if (event.key === 'Escape') return null
  return {
    key: normalizeHotkeyKey(event.key),
    ctrl: event.ctrlKey,
    alt: event.altKey,
    shift: event.shiftKey,
  }
}

export function matchesInterruptHotkey(event: KeyboardEvent, hotkey: InterruptHotkey): boolean {
  if (event.repeat) return false
  if (!!event.ctrlKey !== hotkey.ctrl) return false
  if (!!event.altKey !== hotkey.alt) return false
  if (!!event.shiftKey !== hotkey.shift) return false
  return normalizeHotkeyKey(event.key) === normalizeHotkeyKey(hotkey.key)
}

export function formatInterruptHotkey(hotkey: InterruptHotkey): string {
  const parts: string[] = []
  if (hotkey.ctrl) parts.push('Ctrl')
  if (hotkey.alt) parts.push('Alt')
  if (hotkey.shift) parts.push('Shift')
  parts.push(displayHotkeyKey(hotkey.key))
  return parts.join('+')
}

function displayHotkeyKey(key: string): string {
  if (key === ' ') return '空格'
  if (key === 'Tab') return 'Tab'
  if (key === 'Enter') return 'Enter'
  if (key.length === 1) return key.toUpperCase()
  return key
}
