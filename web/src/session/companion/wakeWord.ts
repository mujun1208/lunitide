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
// - Interim transcripts are scanned too, so the wake feels instant.
// - Transient errors (no-speech / network / aborted / audio-capture) restart
//   with backoff — an idle timeout never disables the listener.
// - Permanent denials (not-allowed / service-not-allowed) stop cleanly.
import { useEffect, useRef, useState } from 'react'

export type WakeWordState = 'idle' | 'listening' | 'unsupported' | 'error'

export interface WakeWordMatch {
  hit: boolean
  prompt: string
}

const WAKE_PHRASES = ['你好月汐', '嗨月汐', '哈喽月汐', 'hello月汐', 'hi月汐', '你好月夕', '嗨月夕']

// Strip whitespace, punctuation and symbols, then lowercase so「你好，月汐！」
// and "Hello 月汐" both match. ASR transcripts mix full/half-width punctuation.
const normalize = (value: string) => value.replace(/[\s\p{P}\p{S}]+/gu, '').toLowerCase()

export function matchWakeWord(transcript: string): WakeWordMatch {
  const normalized = normalize(transcript)
  for (const phrase of WAKE_PHRASES) {
    const at = normalized.indexOf(phrase)
    if (at >= 0) return { hit: true, prompt: normalized.slice(at + phrase.length) }
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

/** Errors after which restarting recognition can never succeed. */
const PERMANENT_ERRORS = new Set(['not-allowed', 'service-not-allowed'])

/** Probe the microphone permission so a hard denial never spins recognition. */
async function microphoneDenied(): Promise<boolean> {
  try {
    if (!navigator.permissions?.query) return false
    const status = await navigator.permissions.query({ name: 'microphone' } as PermissionDescriptor)
    return status.state === 'denied'
  } catch {
    return false // Permission name unsupported on this engine — just try.
  }
}

// useWakeWord listens continuously while enabled and fires onWake(prompt) once
// per wake hit (listening stops after a hit; the companion stage owns the mic
// afterwards). onend and transient errors restart recognition with backoff so
// idle timeouts never disable the wake listener.
export function useWakeWord({ enabled, onWake }: { enabled: boolean; onWake: (prompt: string) => void }): WakeWordState {
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
    let restarts = 0
    const stopRecognition = () => {
      try {
        recognition?.stop()
      } catch {
        /* already stopped */
      }
    }
    const arm = () => {
      if (stopped) return
      try {
        recognition = new Recognition()
        recognition.lang = 'zh-CN'
        recognition.continuous = true
        // Interim results make the wake feel instant: the phrase usually
        // lands in an interim transcript long before the final one.
        recognition.interimResults = true
        recognition.onresult = event => {
          if (stopped) return
          for (let i = 0; i < event.results.length; i++) {
            const result = event.results[i]
            const match = matchWakeWord(result[0].transcript)
            if (match.hit) {
              stopped = true
              stopRecognition()
              onWakeRef.current(match.prompt)
              return
            }
          }
        }
        recognition.onerror = event => {
          if (stopped) return
          if (event?.error && PERMANENT_ERRORS.has(event.error)) {
            stopped = true
            setState('error')
          }
          // Transient errors (no-speech, network, aborted, audio-capture)
          // leave the restart to onend, which always follows.
        }
        recognition.onend = () => {
          if (stopped) return
          // Backoff restart: idle timeouts fire constantly on a quiet home
          // page, and a start() racing its own teardown throws
          // InvalidStateError — retry a bounded number of times.
          const delay = 200 + Math.min(restarts, 6) * 180
          restartTimer = window.setTimeout(() => {
            if (stopped) return
            restarts++
            if (restarts > 40) {
              setState('error')
              return
            }
            arm()
          }, delay)
        }
        recognition.start()
        restarts = 0
        setState('listening')
      } catch {
        setState('error')
      }
    }
    void microphoneDenied().then(denied => {
      if (stopped) return
      if (denied) {
        setState('error')
        return
      }
      arm()
    })
    return () => {
      stopped = true
      window.clearTimeout(restartTimer)
      stopRecognition()
    }
  }, [enabled])
  return state
}
