// Volc seed-asr in the renderer. Isolated from sherpa / localAsr.ts:
// this file only speaks voice.start { backend: 'volc', providerId }.
//
// The microphone stays here. The engine never opens an audio device.
// commit() drains and returns the last transcript; it must not finish
// the websocket. voice.stop is teardown only.

import { getVoiceBridge } from '../../../bridge/client'
import { int16ToBase64, TARGET_SAMPLE_RATE } from '../pcmFrames'
import { startPcmCapture, type PcmCaptureHandle } from '../pcmCapture'
import type { LocalAsrCallbacks, LocalAsrHandle } from '../localAsr'

/** Ceiling on audio waiting to be sent, in samples. Two seconds at 16 kHz. */
const MAX_PENDING_SAMPLES = TARGET_SAMPLE_RATE * 2

/**
 * voice.ValidFrame rejects payloads larger than ten 100 ms frames (1 s /
 * 32 000 bytes). Handshake backlog and a slow append must be split, not
 * merged into one illegal voice.append.
 */
const MAX_APPEND_SAMPLES = TARGET_SAMPLE_RATE

const DRAIN_ROUNDS = 40

/** Match the Go handshake budget so a hung provider.list falls back. */
export const VOLC_ASR_DECISION_MS = 3000

/** Meeting recorder taps can be late. If no PCM arrives, open our own mic so seed-asr is not deaf. */
export const VOLC_EXTERNAL_PCM_RESCUE_MS = 1600

/** Silence after a turn so Volc VAD can emit definite without closing the WS. */
const SILENCE_FLUSH_SAMPLES = Math.round(TARGET_SAMPLE_RATE * 0.4)

/** Ignore a late definite of the same sentence so it cannot become the next turn. */
const REPEAT_SUPPRESS_MS = 1500

const settled = () => new Promise<void>(resolve => window.setTimeout(resolve, 5))

export type VolcAsrCallbacks = LocalAsrCallbacks & { endWindowMs?: number }
export type VolcAsrHandle = LocalAsrHandle

export async function startVolcAsr(providerId: string, callbacks: VolcAsrCallbacks = {}): Promise<VolcAsrHandle> {
  const bridge = getVoiceBridge()
  let sessionId = ''
  let lastText = ''

  let closed = false
  let muted = false
  let swapping = false
  let inFlight = false
  let fed = false
  // Latches the first real external PCM frame. External always wins over the
  // deaf-rescue mic, so this gate guarantees we never run two capture sources.
  let externalSeen = false
  let capture: PcmCaptureHandle | undefined
  let suppressText = ''
  let suppressUntil = 0

  const startPayload = {
    language: 'zh-CN' as const,
    backend: 'volc' as const,
    providerId,
    ...(typeof callbacks.endWindowMs === 'number' ? { endWindowMs: callbacks.endWindowMs } : {}),
  }

  const stop = () => {
    closed = true
    capture?.stop()
    capture = undefined
  }

  const fail = (error: unknown) => {
    if (closed) return
    const dying = sessionId
    stop()
    if (dying) void bridge.stop({ sessionId: dying }).catch(() => {})
    callbacks.onError?.(error instanceof Error ? error : new Error(String(error)))
  }

  const retryableVoice = (error: unknown) => {
    if (!error || typeof error !== 'object') return false
    if ('retryable' in error && Boolean((error as { retryable?: unknown }).retryable)) return true
    const code = 'code' in error ? String((error as { code: unknown }).code) : ''
    return code === 'REQUEST_DEADLINE_EXCEEDED' || code === 'HOST_BUSY' || code === 'STORAGE_UNAVAILABLE'
  }

  const recoverSession = async () => {
    if (closed) return
    const dying = sessionId
    sessionId = ''
    fed = false
    lastText = ''
    suppressText = ''
    suppressUntil = 0
    if (dying) void bridge.stop({ sessionId: dying }).catch(() => {})
    let last: unknown
    for (let attempt = 0; attempt < 4; attempt++) {
      if (closed) return
      try {
        const opened = await bridge.start(startPayload)
        if (closed) {
          void bridge.stop({ sessionId: opened.sessionId }).catch(() => {})
          return
        }
        sessionId = opened.sessionId
        return
      } catch (error) {
        last = error
        await new Promise<void>(resolve => { window.setTimeout(resolve, 400 * (attempt + 1)) })
      }
    }
    throw last
  }

  let pending: { base64: string; samples: Int16Array }[] = []
  let pendingSamples = 0

  const takePending = (): string | undefined => {
    if (pending.length === 0) return undefined
    const first = pending[0]!
    if (pending.length === 1 && first.samples.length <= MAX_APPEND_SAMPLES) {
      pending = []
      pendingSamples = 0
      return first.base64
    }
    let take = 0
    let samples = 0
    while (take < pending.length && samples + pending[take]!.samples.length <= MAX_APPEND_SAMPLES) {
      samples += pending[take]!.samples.length
      take++
    }
    if (take === 0) {
      const head = first.samples.subarray(0, MAX_APPEND_SAMPLES)
      const rest = first.samples.subarray(MAX_APPEND_SAMPLES)
      pending[0] = { base64: int16ToBase64(rest), samples: rest }
      pendingSamples -= head.length
      return int16ToBase64(head)
    }
    if (take === pending.length && samples === pendingSamples && take === 1) {
      pending = []
      pendingSamples = 0
      return first.base64
    }
    const merged = new Int16Array(samples)
    let at = 0
    for (let i = 0; i < take; i++) {
      merged.set(pending[i]!.samples, at)
      at += pending[i]!.samples.length
    }
    pending = pending.slice(take)
    pendingSamples -= samples
    return int16ToBase64(merged)
  }

  const suppressRepeat = (text: string) => {
    if (!text) return
    suppressText = text
    suppressUntil = Date.now() + REPEAT_SUPPRESS_MS
  }

  const queueSilence = () => {
    if (closed || !sessionId) return
    const samples = new Int16Array(SILENCE_FLUSH_SAMPLES)
    pending.push({ base64: int16ToBase64(samples), samples })
    pendingSamples += samples.length
    pump()
  }

  const acceptTranscript = (owner: string, text: string, final: boolean) => {
    if (!text || owner !== sessionId) return
    if (suppressText && text === suppressText && Date.now() < suppressUntil) return
    lastText = text
    if (final) suppressRepeat(text)
    callbacks.onTranscript?.(text, final)
  }

  const pump = () => {
    if (closed || swapping || inFlight || !sessionId) return
    const pcm = takePending()
    if (!pcm) return
    const owner = sessionId
    inFlight = true
    fed = true
    bridge
      .append({ sessionId: owner, pcm })
      .then(result => {
        if (result.text) acceptTranscript(owner, result.text, result.final)
      })
      .catch(async error => {
        if (closed || owner !== sessionId) return
        if (retryableVoice(error)) {
          try {
            const result = await bridge.append({ sessionId: owner, pcm })
            if (result.text) acceptTranscript(owner, result.text, result.final)
            return
          } catch {
            try {
              swapping = true
              await recoverSession()
              return
            } catch (recoverErr) {
              fail(recoverErr)
              return
            } finally {
              swapping = false
            }
          }
        }
        fail(error)
      })
      .finally(() => {
        inFlight = false
        pump()
      })
  }

  const drain = async () => {
    for (let guard = 0; guard < DRAIN_ROUNDS && !closed; guard++) {
      if (!inFlight && pending.length === 0) return
      pump()
      await settled()
    }
  }

  const acceptFrame = (frame: { base64: string; samples: Int16Array; peak: number }) => {
    callbacks.onLevel?.(frame.peak)
    if (closed || muted) return
    pending.push({ base64: frame.base64, samples: frame.samples })
    pendingSamples += frame.samples.length
    while (pendingSamples > MAX_PENDING_SAMPLES && pending.length > 1) {
      pendingSamples -= pending.shift()!.samples.length
    }
    pump()
  }

  if (!callbacks.externalPcm) {
    capture = await startPcmCapture({
      extraStreams: callbacks.extraStreams,
      onFrame: acceptFrame,
      onError: fail,
    })
  } else {
    // Deaf-rescue: if the recorder tap never delivers PCM, open our own mic so
    // seed-asr is not silently deaf. externalSeen makes external PCM win in
    // every ordering — arriving before the timer, during the async mic open, or
    // after the mic opened — so the tap and the browser mic never run together.
    window.setTimeout(() => {
      if (closed || externalSeen || capture) return
      void startPcmCapture({
        extraStreams: callbacks.extraStreams,
        onFrame: acceptFrame,
        onError: fail,
      }).then(handle => {
        if (closed || externalSeen || capture) {
          handle.stop()
          return
        }
        capture = handle
      }).catch(fail)
    }, VOLC_EXTERNAL_PCM_RESCUE_MS)
  }

  try {
    sessionId = (await bridge.start(startPayload)).sessionId
  } catch (error) {
    stop()
    throw error
  }
  pump()

  return {
    finish: async () => {
      if (closed) return lastText
      stop()
      const dying = sessionId
      if (dying) void bridge.stop({ sessionId: dying }).catch(() => {})
      return lastText
    },
    cancel: () => {
      if (closed) return
      stop()
      if (sessionId) void bridge.stop({ sessionId }).catch(() => {})
    },
    commit: async () => {
      if (closed || swapping) return lastText
      capture?.flush()
      await drain()
      if (closed) return lastText
      if (!fed) return ''
      const out = lastText
      lastText = ''
      fed = false
      suppressRepeat(out)
      queueSilence()
      return out
    },
    setMuted: next => {
      if (muted === next) return
      muted = next
      if (next) {
        pending = []
        pendingSamples = 0
        queueSilence()
      }
    },
    restart: async () => {
      if (closed) return
      swapping = true
      try {
        await recoverSession()
      } catch (error) {
        fail(error)
      } finally {
        swapping = false
        pump()
      }
    },
    pushFrame: frame => {
      if (!callbacks.externalPcm || closed) return
      externalSeen = true
      if (capture) {
        // A late-recovering tap: external wins, so stop the rescue mic rather
        // than feed two audio sources into the same seed-asr session.
        capture.stop()
        capture = undefined
      }
      acceptFrame(frame)
    },
  }
}
