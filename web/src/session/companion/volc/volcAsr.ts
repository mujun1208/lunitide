// Volc seed-asr in the renderer. Isolated from sherpa / localAsr.ts:
// this file only speaks voice.start { backend: 'volc', providerId }.
//
// The microphone stays here. The engine never opens an audio device.
// commit() drains and returns the last transcript; it must not finish
// the websocket. voice.stop is teardown only.

import { getVoiceBridge } from '../../../bridge/client'
import { int16ToBase64, TARGET_SAMPLE_RATE } from '../pcmFrames'
import { startPcmCapture, type PcmCaptureHandle } from '../pcmCapture'
import { takePcmBatch } from '../pcmQueue'
import type { LocalAsrCallbacks, LocalAsrHandle } from '../localAsr'
import { createVolcTranscriptCursor, type VolcUtterance } from './volcTranscript'

/** Bound startup/backpressure buffering without silently dropping speech. */
const MAX_PENDING_SAMPLES = TARGET_SAMPLE_RATE * 15

const DRAIN_TIMEOUT_MS = 6000

/** Match the Go handshake budget so a hung provider.list falls back. */
export const VOLC_ASR_DECISION_MS = 3000

/** Meeting recorder taps can be late. If no PCM arrives, open our own mic so seed-asr is not deaf. */
export const VOLC_EXTERNAL_PCM_RESCUE_MS = 1600

/** Silence after a turn so Volc VAD can emit definite without closing the WS. */
const SILENCE_FLUSH_SAMPLES = Math.round(TARGET_SAMPLE_RATE * 0.4)

const settled = () => new Promise<void>(resolve => window.setTimeout(resolve, 5))

export type VolcAsrCallbacks = Omit<LocalAsrCallbacks, 'onTranscript'> & {
  endWindowMs?: number
  onTranscript?: (text: string, final: boolean, timestamped?: boolean) => void
}
export type VolcAsrHandle = LocalAsrHandle & { discardTranscript?: () => void }

export async function startVolcAsr(providerId: string, callbacks: VolcAsrCallbacks = {}): Promise<VolcAsrHandle> {
  const bridge = getVoiceBridge()
  let sessionId = ''
  let lastText = ''
  const cursor = createVolcTranscriptCursor()

  let closed = false
  let muted = false
  let swapping = false
  let inFlight = false
  let inFlightSamples = 0
  let completedSamples = 0
  let fed = false
  // Latches the first real external PCM frame. External always wins over the
  // deaf-rescue mic, so this gate guarantees we never run two capture sources.
  let externalSeen = false
  let capture: PcmCaptureHandle | undefined

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
    cursor.reset()
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

  const takePending = () => {
    const batch = takePcmBatch(pending)
    if (!batch) return undefined
    pendingSamples -= batch.sampleCount
    return batch
  }

  const queueSilence = () => {
    if (closed || !sessionId) return
    const samples = new Int16Array(SILENCE_FLUSH_SAMPLES)
    pending.push({ base64: int16ToBase64(samples), samples })
    pendingSamples += samples.length
    pump()
  }

  const acceptTranscript = (owner: string, text: string, final: boolean, utterances?: VolcUtterance[]) => {
    if (closed || !text || owner !== sessionId) return
    const next = cursor.update(text, final, utterances)
    if (!next.text) return
    lastText = next.text
    callbacks.onTranscript?.(next.text, next.final, cursor.current().timestamped)
  }

  const pump = () => {
    if (closed || swapping || inFlight || !sessionId) return
    const batch = takePending()
    if (!batch) return
    const pcm = batch.base64
    const owner = sessionId
    inFlight = true
    inFlightSamples = batch.sampleCount
    fed = true
    bridge
      .append({ sessionId: owner, pcm })
      .then(result => {
        if (result.text) acceptTranscript(owner, result.text, result.final, result.utterances)
      })
      .catch(async error => {
        if (closed || owner !== sessionId) return
        if (retryableVoice(error)) {
          try {
            const result = await bridge.append({ sessionId: owner, pcm })
            if (result.text) acceptTranscript(owner, result.text, result.final, result.utterances)
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
        completedSamples += batch.sampleCount
        inFlightSamples = 0
        inFlight = false
        pump()
      })
  }

  const drain = async () => {
    const boundary = completedSamples + inFlightSamples + pendingSamples
    const deadline = Date.now() + DRAIN_TIMEOUT_MS
    while (!closed && completedSamples < boundary) {
      if (Date.now() >= deadline) throw new Error('语音发送超时，本句尚未完整识别。')
      pump()
      await settled()
    }
  }

  const acceptFrame = (frame: { base64: string; samples: Int16Array; peak: number }) => {
    callbacks.onLevel?.(frame.peak)
    if (closed || muted) return
    pending.push({ base64: frame.base64, samples: frame.samples })
    pendingSamples += frame.samples.length
    if (pendingSamples > MAX_PENDING_SAMPLES) {
      fail(new Error('语音处理积压超过 15 秒，本句未完整识别，请稍后重试。'))
      return
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
    discardTranscript: () => {
      cursor.commit()
      lastText = ''
    },
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
    commit: async (options) => {
      if (closed || swapping) return lastText
      if (options?.useStreamed) {
        const out = cursor.commit() || lastText
        lastText = ''
        fed = false
        capture?.flush()
        queueSilence()
        return out
      }
      capture?.flush()
      await drain()
      if (closed) return lastText
      if (!fed) return ''
      const out = cursor.commit() || lastText
      lastText = ''
      fed = false
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
