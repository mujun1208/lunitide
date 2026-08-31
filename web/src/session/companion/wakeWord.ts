// wakeWord.ts — companion voice wake on the launch home. A tiny pure matcher
// (wake phrases with punctuation/space-insensitive matching, trailing text
// becomes the companion prompt) plus a continuous-listening hook on the
// same ASR path as 月伴 (火山 / sherpa / 系统识别). One microphone; no
// second getUserMedia VAD stream. Volc never falls through to Web Speech
// (VOICE-004). Energy without text surfaces as a deaf error, not fake listening.
import { useEffect, useRef, useState } from 'react'
import { getProviderBridge } from '../../bridge/client'
import { pickDefaultVoice } from '../../provider/modelKind'
import { companionListenKind, withDeadline } from './asrPath'
import { loadCompanionSettings } from './companionSettings'
import { cleanUserTranscript } from './companionText'
import { startLocalCompanionSpeech } from './localSpeech'
import { prepareCompanionEntry } from './prepareCompanionEntry'
import { startCompanionSpeech, type CompanionSpeechHandle, type CompanionSpeechOptions } from './speech'
import { unlockTtsAudio } from './ttsPlayer'
import { startVolcCompanionSpeech } from './volc/volcSpeech'
import { shouldAcceptWake, type WakeMatchKind, type WakeVadSnapshot } from './wakeVad'

export type WakeWordState = 'idle' | 'probing' | 'listening' | 'unsupported' | 'error'
export type { WakeMatchKind }

export interface WakeWordMatch {
  hit: boolean
  prompt: string
  kind: WakeMatchKind
}

// Greeting × name cross product covers the homophones Windows ASR most
// often returns for 「你好，月汐」 (月汐/月夕/月希/月西/月溪/月熙/月惜 and
// 悦汐/悦希 — the exact-fit phrase list used to miss transcribes entirely).
const WAKE_GREETINGS = ['你好', '您好', '嗨', '嘿', '哈喽', 'hello', 'hi']
const WAKE_NAMES = ['月汐', '月夕', '月希', '月西', '月溪', '月熙', '月惜', '月曦', '悦汐', '悦希', '玥汐', '岳希', '月昔', '月息', '月喜', '月兮', '月锡', '月伴', 'yuxi']
const WAKE_FILLERS = ['', '我是', '我叫', '我是叫', '请', '帮我', '帮忙']
const WAKE_PHRASES = WAKE_GREETINGS.flatMap(g => WAKE_NAMES.map(n => g + n))
WAKE_PHRASES.push('进入月伴', '打开月伴', '进入月伴模式', '打开月伴模式', '进入月伴对话', '打开月伴对话')
WAKE_PHRASES.sort((a, b) => b.length - a.length)
const NAME_ONLY = [...WAKE_NAMES].sort((a, b) => b.length - a.length)

const normalize = (value: string) => value.replace(/[\s\p{P}\p{S}]+/gu, '').toLowerCase()

export function matchWakeWord(transcript: string): WakeWordMatch {
  const normalized = normalize(transcript)
  if (!normalized) return { hit: false, prompt: '', kind: 'none' }
  for (const phrase of WAKE_PHRASES) {
    const at = normalized.indexOf(phrase)
    if (at >= 0) return { hit: true, prompt: normalized.slice(at + phrase.length), kind: 'phrase' }
  }
  for (const greeting of WAKE_GREETINGS) {
    const gi = normalized.indexOf(greeting)
    if (gi < 0) continue
    const rest = normalized.slice(gi + greeting.length)
    for (const filler of WAKE_FILLERS) {
      const afterFiller = filler ? (rest.startsWith(filler) ? rest.slice(filler.length) : '') : rest
      if (filler && !rest.startsWith(filler)) continue
      for (const name of NAME_ONLY) {
        const ni = afterFiller.indexOf(name)
        if (ni >= 0 && ni <= 6) {
          return { hit: true, prompt: afterFiller.slice(ni + name.length), kind: 'phrase' }
        }
      }
    }
  }
  for (const name of NAME_ONLY) {
    if (normalized === name) return { hit: true, prompt: '', kind: 'name' }
    if (normalized.startsWith(name)) return { hit: true, prompt: normalized.slice(name.length), kind: 'name' }
  }
  return { hit: false, prompt: '', kind: 'none' }
}

export const HOME_WAKE_DEAF_MS = 5000

export function useWakeWord({
  enabled,
  retry = 0,
  onWake,
  onHeard,
  onDeaf,
  vad = true,
  readVad,
}: {
  enabled: boolean
  retry?: number
  onWake: (prompt: string) => void
  onHeard?: (transcript: string) => void
  onDeaf?: () => void
  /** Live-voice gate. Off = any transcript match enters, including speaker bleed. */
  vad?: boolean
  /** Tests inject a snapshot; production treats a transcript as live speech unless this says playback. */
  readVad?: () => WakeVadSnapshot | null
}): WakeWordState {
  const [state, setState] = useState<WakeWordState>('idle')
  const onWakeRef = useRef(onWake)
  onWakeRef.current = onWake
  const onHeardRef = useRef(onHeard)
  onHeardRef.current = onHeard
  const onDeafRef = useRef(onDeaf)
  onDeafRef.current = onDeaf
  const readVadRef = useRef(readVad)
  readVadRef.current = readVad
  useEffect(() => {
    if (!enabled) {
      setState('idle')
      onHeardRef.current?.('')
      return
    }
    if (!navigator.mediaDevices?.getUserMedia) {
      setState('unsupported')
      return
    }
    const bag: { stopped: boolean; handle?: CompanionSpeechHandle } = { stopped: false }
    let deafTimer = 0
    let heardText = false
    const acceptMatch = (match: ReturnType<typeof matchWakeWord>) => {
      if (!match.hit) return false
      const snapshot = readVadRef.current?.() ?? (heardText
        ? { speechLikely: true, playbackLikely: false, tooQuiet: false }
        : null)
      return shouldAcceptWake(match.kind, snapshot, vad)
    }
    const consider = (raw: string) => {
      if (bag.stopped) return
      const text = cleanUserTranscript(raw)
      if (!text) return
      heardText = true
      window.clearTimeout(deafTimer)
      onHeardRef.current?.(text)
      setState('listening')
      const match = matchWakeWord(text)
      if (!acceptMatch(match)) return
      bag.stopped = true
      bag.handle?.stop()
      bag.handle = undefined
      unlockTtsAudio()
      onWakeRef.current(cleanUserTranscript(match.prompt))
    }
    const speechOptions: CompanionSpeechOptions = {
      duplex: true,
      onInterim: consider,
      onFinal: consider,
      onVoiceEnergy: () => {
        if (bag.stopped || heardText) return
        setState('listening')
        window.clearTimeout(deafTimer)
        deafTimer = window.setTimeout(() => {
          if (bag.stopped || heardText) return
          onDeafRef.current?.()
          setState('error')
        }, HOME_WAKE_DEAF_MS)
      },
      onError: () => {
        if (!bag.stopped) setState('error')
      },
      onEndWithoutFinal: () => {
        if (bag.stopped) return
        bag.handle?.resumeCapture()
      },
    }
    const adopt = (handle: CompanionSpeechHandle) => {
      bag.handle = handle
      if (bag.stopped) {
        handle.stop()
        return
      }
      setState('listening')
      handle.resumeCapture()
    }
    setState('probing')
    void (async () => {
      try {
        const prepared = await prepareCompanionEntry(loadCompanionSettings())
        if (bag.stopped) return
        const kind = companionListenKind(prepared.settings.voicePath, prepared.settings.recognizer)
        if (kind === 'volc') {
          if (!prepared.hasVolc) {
            setState('error')
            return
          }
          const listed = await getProviderBridge().list().catch(() => ({ items: [] }))
          const picked = pickDefaultVoice(listed.items)
          if (!picked) {
            setState('error')
            return
          }
          adopt(await withDeadline(startVolcCompanionSpeech(speechOptions, picked.provider.id), 4000))
          return
        }
        if (kind === 'local') {
          if (!prepared.listenReady) {
            setState('error')
            return
          }
          adopt(await withDeadline(startLocalCompanionSpeech(speechOptions), 4000))
          return
        }
        adopt(await startCompanionSpeech(speechOptions))
      } catch {
        if (bag.stopped) return
        const kind = companionListenKind(loadCompanionSettings().voicePath, loadCompanionSettings().recognizer)
        if (kind === 'volc' || kind === 'local') {
          setState('error')
          return
        }
        try {
          adopt(await startCompanionSpeech(speechOptions))
        } catch {
          if (!bag.stopped) setState('error')
        }
      }
    })()
    return () => {
      bag.stopped = true
      window.clearTimeout(deafTimer)
      bag.handle?.stop()
    }
  }, [enabled, retry, vad])
  return state
}
