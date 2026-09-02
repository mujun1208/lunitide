// companionSettings.ts persists the M9.5 Moon Companion settings under
// the existing lunitide:localStorage namespace (zero new tables, zero
// migrations). The engine family picks the synthesis route: "edge"
// (free Microsoft cloud neural voices, default), leftover "natural"
// (OneCore) / "sapi" (classic desktop, no longer offered), or "ref"
// (GPT-SoVITS local cloning), or "volc" (Ark Agent Plan seed-tts).
// Saved SAPI/OneCore installs move onto Edge; the local clone path stays.
// rev < 2 OneCore defaults are moved onto the cloud engine once.
import { useEffect, useState } from 'react'

import type { VoicePath } from './voicePersonas'
import { VOLC_DEFAULT_VOICE_ID, isVolcSpeakerId } from './volcVoices'

export type CompanionEngine = 'edge' | 'natural' | 'sapi' | 'ref' | 'volc'
/** Product-level voice channel. MiniCPM-o is not a TTS engine. */
export type { VoicePath }
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
  /** Retired home-page “你好月汐” listener. Forced off; enter companion from the card. */
  wakeWord: boolean
  /**
   * On the home wake mic, reject matches that look like speaker playback.
   * Off = any transcript hit enters (old behaviour).
   */
  wakeVad: boolean
  /** Keep mic open between turns; mute while assistant TTS plays. */
  fullDuplex: boolean
  /**
   * Speak a one-syllable pad («嗯») as soon as the user turn is sent, so
   * the first sound is not waiting on the model's first token. The real
   * reply interrupts the pad. Default off: the pad echoes back through
   * the recognizer and becomes another 嗯. Off = wait for the first
   * speakable chunk.
   */
  instantAck: boolean
  /**
   * Local sherpa only: keep listening while she talks and treat a non-echo
   * transcript as an interrupt. System speech recognition stays muted —
   * it captures the speaker independently. Default off: TVs and other
   * voices can false-trigger.
   */
  voiceBargeIn: boolean
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
  /** cloud = Edge ASR/TTS; local = GPT-SoVITS 50 人生. omni is leftover-only and migrates to cloud. */
  voicePath: VoicePath
  /**
   * 火山 only, opt-in (default off). When off, 火山 runs the robust single-voice
   * cascade pipeline (seed-asr → LLM → seed-tts) with 打断-button turn control.
   * When on, offer the realtime full-duplex talk core when a realtime model +
   * session exist. Default cascade guarantees no double-voice out of the box.
   */
  talkRealtime: boolean
  omniPersonaId: string
}

const STORAGE_KEY = 'lunitide:companion'
const SETTINGS_REV = 12

/** True only when the user (or a previous save) wrote an explicit voicePath. */
export function hasExplicitCompanionVoicePath(): boolean {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return false
    const parsed = JSON.parse(raw) as { voicePath?: unknown }
    return (
      parsed.voicePath === 'cloud' ||
      parsed.voicePath === 'local' ||
      parsed.voicePath === 'volc' ||
      parsed.voicePath === 'omni' ||
      parsed.voicePath === 'flm'
    )
  } catch {
    return false
  }
}

/** Reliable probe order when the primary engine fails.
 *
 *  'natural' is deliberately absent. Reaching the OneCore neural voices means
 *  mirroring their tokens into HKCU so classic SAPI enumerates them, and on
 *  the machines this was tested against SAPI does not merge that hive — the
 *  mirror is byte-for-byte complete, the engine CLSID is registered for both
 *  bitnesses, and GetVoices still returns only the HKLM tokens. Probing it
 *  costs a round trip to arrive back where 'sapi' already is.
 *  Classic SAPI is not a fallback either: it is the tinny desktop
 *  voice the picker used to offer as 本机语音, and it is gone. */
const ENGINE_PROBE_FALLBACK: CompanionEngine[] = ['edge']

/**
 * What this turn actually synthesizes. Local + SoVITS down may speak 晓晓
 * for this clip only — the caller must relabel the speak light. Never persist
 * that as the local card's engine.
 */
export function companionPlaybackSettings(
  settings: CompanionSettings,
  speakReady: boolean,
  preferEdge = false,
): CompanionSettings & { lockEngine?: boolean } {
  if (settings.voicePath === 'local') {
    if (speakReady && !preferEdge) {
      return { ...settings, engine: 'ref', lockEngine: true }
    }
    return { ...settings, engine: 'edge', voiceId: '', lockEngine: true }
  }
  if (settings.engine === 'volc') {
    return { ...settings, lockEngine: true }
  }
  return settings
}

export function companionEngineProbeOrder(primary: CompanionEngine): CompanionEngine[] {
  const start = primary === 'sapi' || primary === 'natural' ? 'edge' : primary
  if (start === 'volc') return ['volc']
  const order: CompanionEngine[] = [start]
  for (const engine of ENGINE_PROBE_FALLBACK) {
    if (!order.includes(engine)) order.push(engine)
  }
  return order
}

/** Cloud voice ids are invalid on OneCore/SAPI — drop them on engine switch. */
export function voiceIdForEngineSwitch(from: CompanionEngine, to: CompanionEngine, voiceId: string): string {
  if (!voiceId || from === to) return voiceId
  if (to === 'volc') return isVolcSpeakerId(voiceId) ? voiceId : ''
  if (from === 'volc') return ''
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
  wakeVad: true,
  fullDuplex: true,
  instantAck: false,
  voiceBargeIn: false,
  interruptHotkey: { ...DEFAULT_INTERRUPT_HOTKEY },
  speechEnvironment: 'normal',
  recognizer: 'auto',
  voiceId: '',
  rate: 2,
  volume: 88,
  engine: 'edge',
  refEndpoint: '',
  voicePath: 'cloud',
  talkRealtime: false,
  omniPersonaId: 'refpack:优质台湾腔.wav',
})

export function loadCompanionSettings(): CompanionSettings {
  const fallback = defaultCompanionSettings()
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as Omit<Partial<CompanionSettings>, 'voicePath'> & {
      rev?: number
      flmPersonaId?: string
      voicePath?: unknown
    }
    let engine: CompanionEngine = isEngine(parsed.engine) ? parsed.engine : fallback.engine
    let voiceId = typeof parsed.voiceId === 'string' ? parsed.voiceId : ''
    const rev = typeof parsed.rev === 'number' ? parsed.rev : 0
    // rev < 12 shipped home-page 「你好月汐」 listening. That kept the mic
    // open on launch and minted empty 月伴对话 sessions until the 100 cap.
    let wakeWord = typeof parsed.wakeWord === 'boolean' ? parsed.wakeWord : fallback.wakeWord
    if (rev < 12) {
      wakeWord = false
    }
    let persist = rev < SETTINGS_REV
    // rev < 11 shipped instantAck on. The pad echoed through the mic and
    // looped 嗯嗯嗯; force the product default off once, then honour later
    // explicit on/off choices at the current rev.
    let instantAck = typeof parsed.instantAck === 'boolean' ? parsed.instantAck : fallback.instantAck
    if (rev < 11) {
      instantAck = false
    }
    const voicePath = readVoicePath(parsed.voicePath, engine)
    if (parsed.voicePath === 'omni' || parsed.voicePath === 'flm') persist = true
    if (voicePath === 'local') {
      engine = 'ref'
    } else if (voicePath === 'volc') {
      if (engine !== 'volc') persist = true
      engine = 'volc'
      if (!isVolcSpeakerId(voiceId)) {
        voiceId = VOLC_DEFAULT_VOICE_ID
        persist = true
      }
    } else if (engine === 'natural' || engine === 'sapi') {
      // Classic SAPI / OneCore are no longer offered — they were the
      // tinny 本机语音 option. Move onto Edge rather than keep speaking it.
      engine = 'edge'
      if (!voiceId.startsWith('refpack:')) voiceId = ''
      persist = true
    }
    const omniPersonaId = readOmniPersona(parsed.omniPersonaId, parsed.flmPersonaId, fallback.omniPersonaId)
    const interruptHotkey = parseInterruptHotkey(
      (parsed as Partial<CompanionSettings> & { interruptHotkey?: unknown }).interruptHotkey,
    )
    const next: CompanionSettings = {
      enabled: typeof parsed.enabled === 'boolean' ? parsed.enabled : fallback.enabled,
      autoSpeak: typeof parsed.autoSpeak === 'boolean' ? parsed.autoSpeak : fallback.autoSpeak,
      wakeWord,
      wakeVad: typeof parsed.wakeVad === 'boolean' ? parsed.wakeVad : fallback.wakeVad,
      fullDuplex: typeof parsed.fullDuplex === 'boolean' ? parsed.fullDuplex : fallback.fullDuplex,
      instantAck,
      voiceBargeIn: typeof parsed.voiceBargeIn === 'boolean' ? parsed.voiceBargeIn : fallback.voiceBargeIn,
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
      voicePath,
      talkRealtime: typeof parsed.talkRealtime === 'boolean' ? parsed.talkRealtime : fallback.talkRealtime,
      omniPersonaId,
    }
    if (persist) saveCompanionSettings(next)
    return next
  } catch {
    return fallback
  }
}

let savingCompanionSettings = false

export function saveCompanionSettings(settings: CompanionSettings): void {
  try {
    const payload: Record<string, unknown> = { ...settings, rev: SETTINGS_REV }
    // MiniCPM-o is retired; cloud saves must not carry omni persona refs.
    if (settings.voicePath === 'cloud') delete payload.omniPersonaId
    localStorage.setItem(STORAGE_KEY, JSON.stringify(payload))
    if (savingCompanionSettings) return
    savingCompanionSettings = true
    try {
      window.dispatchEvent(new Event('lunitide:companion-settings'))
    } finally {
      savingCompanionSettings = false
    }
  } catch {
    // Storage unavailable (private mode etc.) — settings stay in-memory.
  }
}

function isEngine(value: unknown): value is CompanionEngine {
  return value === 'edge' || value === 'natural' || value === 'sapi' || value === 'ref' || value === 'volc'
}

function isVoicePath(value: unknown): value is VoicePath {
  return value === 'cloud' || value === 'local' || value === 'omni' || value === 'volc'
}

function readVoicePath(value: unknown, engine: CompanionEngine): VoicePath {
  // MiniCPM-o is no longer a selectable 月伴 path: leftover omni/flm saves
  // move onto 云端 so the stage uses Edge ASR/TTS instead of the duplex
  // engine that truncated captions and trapped 说话中.
  if (value === 'flm' || value === 'omni') return 'cloud'
  if (isVoicePath(value)) return value
  return engine === 'ref' ? 'local' : 'cloud'
}

function readOmniPersona(omniPersonaId: unknown, flmPersonaId: unknown, fallback: string): string {
  if (typeof omniPersonaId === 'string' && omniPersonaId) return omniPersonaId
  if (typeof flmPersonaId === 'string' && flmPersonaId) return flmPersonaId
  return fallback
}

/**
 * Whether the assistant keeps her microphone open while she speaks
 * (client-side, VAD-driven barge-in over the PCM/Web Speech recognizer).
 *
 * Unified turn model (all client-recognizer paths are half-duplex): her turn
 * is hers start-to-end and the only ways back to the user are the 打断
 * button/hotkey or her finishing. This retires client-side voice barge-in for
 * every mode, because an open mic during playback is a window her own voice
 * comes back through — the source of the 本地 "插话即打断 + 闪烁跳频 + 卡壳断链"
 * thrash and of accidental interrupts.
 *
 * - 云端 (cloud): half-duplex (Web Speech cannot be muted mid-flight anyway).
 * - 本地 (local): half-duplex, 打断 button only.
 * - 火山 cascade (default): half-duplex, 打断 button only.
 * - 火山 talk-realtime (opt-in): full-duplex is handled server-side (server VAD
 *   + AEC) over the independent talk uplink, not through this client flag.
 *
 * The legacy `voiceBargeIn` field is kept for storage compatibility but is now
 * inert; it no longer opens a live mic on any path.
 */
export function companionVoiceBargeInEnabled(_settings: Pick<CompanionSettings, 'voicePath' | 'voiceBargeIn'>): boolean {
  return false
}

export function applyVoicePath(settings: CompanionSettings, path: VoicePath, opts?: { volcTtsReady?: boolean }): CompanionSettings {
  if (path === 'local') {
    const voiceId = settings.voiceId.startsWith('refpack:') ? settings.voiceId : settings.omniPersonaId
    return { ...settings, voicePath: 'local', engine: 'ref', voiceId, recognizer: 'local' }
  }
  if (path === 'volc') {
    if (opts?.volcTtsReady === false) {
      const voiceId = settings.voiceId.startsWith('refpack:') || isVolcSpeakerId(settings.voiceId) ? '' : settings.voiceId
      return { ...settings, voicePath: 'volc', engine: 'edge', voiceId }
    }
    const voiceId = isVolcSpeakerId(settings.voiceId) ? settings.voiceId : VOLC_DEFAULT_VOICE_ID
    return { ...settings, voicePath: 'volc', engine: 'volc', voiceId }
  }
  const voiceId = settings.voiceId.startsWith('refpack:') || isVolcSpeakerId(settings.voiceId) ? '' : settings.voiceId
  return { ...settings, voicePath: 'cloud', engine: 'edge', voiceId }
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

export function useCompanionSettings(): CompanionSettings {
  const [settings, setSettings] = useState(loadCompanionSettings)
  useEffect(() => {
    const refresh = () => setSettings(loadCompanionSettings())
    const onStorage = (event: StorageEvent) => {
      if (event.key === STORAGE_KEY) refresh()
    }
    window.addEventListener('lunitide:companion-settings', refresh)
    window.addEventListener('storage', onStorage)
    return () => {
      window.removeEventListener('lunitide:companion-settings', refresh)
      window.removeEventListener('storage', onStorage)
    }
  }, [])
  return settings
}
