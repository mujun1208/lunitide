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

/**
 * Which recognizer transcribes the microphone.
 *
 * `auto` prefers the local model once it is installed and falls back to the
 * system recognizer, which is the right default: the local one keeps audio on
 * the machine and works offline, but it is a large download nobody should be
 * made to wait for before speaking.
 *
 * The fallback really is a fallback, not an equal option. The system
 * recognizer captures its own audio inside the engine, so this side cannot
 * mute it while the companion speaks — it transcribed her replies off the
 * speakers and delivered them as the user's next question — and it cannot be
 * asked when a turn ended, which left that decision to a level meter. Both
 * are properties of the API, not bugs above it.
 */
export type SpeechRecognizer = 'auto' | 'cloud' | 'local'

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
  /** Local model vs system recognizer for speech-to-text. */
  recognizer: SpeechRecognizer
  voiceId: string
  rate: number
  volume: number
  engine: CompanionEngine
  refEndpoint: string
}

const STORAGE_KEY = 'lunitide:companion'
const SETTINGS_REV = 9

/** Reliable probe order when the primary engine fails.
 *
 *  'natural' is deliberately absent. Reaching the OneCore neural voices means
 *  mirroring their tokens into HKCU so classic SAPI enumerates them, and on
 *  the machines this was tested against SAPI does not merge that hive — the
 *  mirror is byte-for-byte complete, the engine CLSID is registered for both
 *  bitnesses, and GetVoices still returns only the HKLM tokens. Probing it
 *  costs a round trip to arrive back where 'sapi' already is. */
const ENGINE_PROBE_FALLBACK: CompanionEngine[] = ['edge', 'sapi']

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
  recognizer: 'auto',
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
    }
    // The OneCore engine is no longer offered: it could not be reached
    // through SAPI, so anyone left on it had a companion that showed
    // captions and never spoke. Moved rather than left broken, and the
    // stale voice id goes with it — its tokens do not exist for SAPI.
    if (engine === 'natural') {
      engine = 'edge'
      voiceId = ''
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
      // Absent for anyone who saved settings before the local recognizer
      // existed, which is exactly what the 'auto' default is for — no rev bump,
      // because bumping it would also reset unrelated fields.
      recognizer: isRecognizer(parsed.recognizer) ? parsed.recognizer : fallback.recognizer,
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

function isRecognizer(value: unknown): value is SpeechRecognizer {
  return value === 'auto' || value === 'cloud' || value === 'local'
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
