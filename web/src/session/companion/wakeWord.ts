// wakeWord.ts — companion voice wake on the launch home. A tiny pure matcher
// (wake phrases with punctuation/space-insensitive matching, trailing text
// becomes the companion prompt) plus a continuous-listening hook built on the
// same Windows online SpeechRecognition the chat composer uses. The hook
// degrades silently when recognition is unavailable (headless/tests) and
// always stops on unmount so it never leaks into chat pages.
//
// Robustness rules (the wake listener used to die on the first error):
// - Microphone permission is probed first; a hard 'denied' surfaces the
//   error state instead of a recognition that can never start.
// - getUserMedia uses the same microphone constraints as in-companion
//   speech so WebView2 actually grants the mic.
// - Continuous mode matches the companion stage (Edge still ends after
//   silence — onend restarts). If start() rejects continuous=true, fall
//   back to one-shot sessions with the same restart loop.
// - Transient errors (no-speech / network / aborted / audio-capture) restart
//   with backoff — an idle timeout never disables the listener.
// - Permanent denials (not-allowed / service-not-allowed) stop cleanly.
import { useEffect, useRef, useState } from 'react'
import { microphoneConstraints, saveMicrophoneId, selectedMicrophoneId } from '../../settings/microphone'
import { unlockTtsAudio } from './ttsPlayer'

export type WakeWordState = 'idle' | 'probing' | 'listening' | 'unsupported' | 'error'

export interface WakeWordMatch {
  hit: boolean
  prompt: string
}

// Greeting × name cross product covers the homophones Windows ASR most
// often returns for 「你好，月汐」 (月汐/月夕/月希/月西/月溪/月熙/月惜 and
// 悦汐/悦希 — the exact-fit phrase list used to miss transcribes entirely).
const WAKE_GREETINGS = ['你好', '您好', '嗨', '嘿', '哈喽', 'hello', 'hi']
const WAKE_NAMES = ['月汐', '月夕', '月希', '月西', '月溪', '月熙', '月惜', '月曦', '月溪', '悦汐', '悦希', '玥汐', '岳希', '月伴', 'yuxi']
const WAKE_FILLERS = ['', '我是', '我叫', '我是叫', '请', '帮我', '帮忙']
const WAKE_PHRASES = WAKE_GREETINGS.flatMap(g => WAKE_NAMES.map(n => g + n))
WAKE_PHRASES.push('进入月伴', '打开月伴', '进入月伴模式', '打开月伴模式', '进入月伴对话', '打开月伴对话')
WAKE_PHRASES.sort((a, b) => b.length - a.length)
const NAME_ONLY = [...WAKE_NAMES].sort((a, b) => b.length - a.length)

const normalize = (value: string) => value.replace(/[\s\p{P}\p{S}]+/gu, '').toLowerCase()

export function matchWakeWord(transcript: string): WakeWordMatch {
  const normalized = normalize(transcript)
  if (!normalized) return { hit: false, prompt: '' }
  for (const phrase of WAKE_PHRASES) {
    const at = normalized.indexOf(phrase)
    if (at >= 0) return { hit: true, prompt: normalized.slice(at + phrase.length) }
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
        if (ni >= 0 && ni <= 6) return { hit: true, prompt: afterFiller.slice(ni + name.length) }
      }
    }
  }
  for (const name of NAME_ONLY) {
    if (normalized === name) return { hit: true, prompt: '' }
    if (normalized.startsWith(name)) return { hit: true, prompt: normalized.slice(name.length) }
  }
  return { hit: false, prompt: '' }
}

type SpeechRecognitionEventLike = { results: ArrayLike<{ 0: { transcript: string }; isFinal: boolean }> }
type SpeechRecognitionLike = {
  lang: string
  continuous: boolean
  interimResults: boolean
  onresult: ((event: SpeechRecognitionEventLike) => void) | null
  onerror: ((event?: { error?: string }) => void) | null
  onend: (() => void) | null
  start: () => void
  stop: () => void
}

const speechRecognitionConstructor = () =>
  (window as typeof window & { SpeechRecognition?: new () => SpeechRecognitionLike; webkitSpeechRecognition?: new () => SpeechRecognitionLike }).SpeechRecognition ??
  (window as typeof window & { webkitSpeechRecognition?: new () => SpeechRecognitionLike }).webkitSpeechRecognition

const PERMANENT_ERRORS = new Set(['not-allowed', 'service-not-allowed', 'language-not-supported'])

async function microphoneDenied(): Promise<boolean> {
  try {
    if (!navigator.permissions?.query) return false
    const status = await navigator.permissions.query({ name: 'microphone' } as PermissionDescriptor)
    return status.state === 'denied'
  } catch {
    return false
  }
}

export function useWakeWord({ enabled, retry = 0, onWake }: { enabled: boolean; retry?: number; onWake: (prompt: string) => void }): WakeWordState {
  const [state, setState] = useState<WakeWordState>('idle')
  const onWakeRef = useRef(onWake)
  onWakeRef.current = onWake
  useEffect(() => {
    if (!enabled) {
      setState('idle')
      return
    }
    const Recognition = speechRecognitionConstructor()
    if (!Recognition || !navigator.mediaDevices?.getUserMedia) {
      setState('unsupported')
      return
    }
    let stopped = false
    let recognition: SpeechRecognitionLike | undefined
    let restartTimer = 0
    let failures = 0
    let armedAt = 0
    let sawResult = false
    let media: MediaStream | undefined
    const stopRecognition = () => {
      try {
        recognition?.stop()
      } catch {
        /* already stopped */
      }
    }
    const releaseMedia = () => {
      media?.getTracks().forEach(track => track.stop())
      media = undefined
    }
    const arm = () => {
      if (stopped) return
      try {
        recognition = new Recognition()
        recognition.lang = 'zh-CN'
        recognition.continuous = true
        recognition.interimResults = true
        recognition.onresult = event => {
          if (stopped) return
          sawResult = true
          setState('listening')
          let rolling = ''
          for (let i = 0; i < event.results.length; i++) {
            rolling += event.results[i][0].transcript
          }
          const match = matchWakeWord(rolling)
          if (match.hit) {
            stopped = true
            stopRecognition()
            onWakeRef.current(match.prompt)
          }
        }
        recognition.onerror = event => {
          if (stopped) return
          if (event?.error && PERMANENT_ERRORS.has(event.error)) {
            stopped = true
            setState('error')
          }
        }
        recognition.onend = () => {
          if (stopped) return
          if (sawResult || Date.now() - armedAt >= 3000) failures = 0
          else failures++
          const delay = 200 + Math.min(failures, 6) * 180
          restartTimer = window.setTimeout(() => {
            if (stopped) return
            if (failures > 8) {
              setState('error')
              return
            }
            arm()
          }, delay)
        }
        armedAt = Date.now()
        sawResult = false
        try {
          recognition.start()
        } catch {
          recognition.continuous = false
          recognition.start()
        }
        setState('listening')
      } catch {
        setState('error')
      }
    }
    setState('probing')
    void (async () => {
      if (stopped) return
      if (await microphoneDenied()) {
        setState('error')
        return
      }
      try {
        let constraints = microphoneConstraints()
        try {
          media = await navigator.mediaDevices.getUserMedia(constraints)
        } catch (error) {
          const name = error instanceof DOMException ? error.name : ''
          if (selectedMicrophoneId() && (name === 'NotFoundError' || name === 'DevicesNotFoundError' || name === 'OverconstrainedError')) {
            saveMicrophoneId('')
            constraints = { audio: true }
            media = await navigator.mediaDevices.getUserMedia(constraints)
          } else throw error
        }
      } catch {
        if (!stopped) setState('error')
        return
      }
      if (stopped) {
        releaseMedia()
        return
      }
      unlockTtsAudio()
      arm()
    })()
    return () => {
      stopped = true
      window.clearTimeout(restartTimer)
      stopRecognition()
      releaseMedia()
    }
  }, [enabled, retry])
  return state
}
