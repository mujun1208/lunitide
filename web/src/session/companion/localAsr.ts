// Local speech recognition in the renderer.
//
// The microphone stays here — the renderer captures it, resamples to the one
// format the recognizer accepts, and posts frames over the bridge. The engine
// never opens an audio device, which is what keeps a second process from
// fighting this one for the microphone.
//
// Each frame's reply carries the partial transcript, so a caption updates at
// the rate speech arrives without a separate event channel to order against.

import { getVoiceBridge, type VoiceStatusResult } from '../../bridge/client'
import { startPcmCapture, type PcmCaptureHandle } from './pcmCapture'

export interface LocalAsrCallbacks {
  /** Fires as words arrive, with `final` set once the recognizer settles. */
  onTranscript?: (text: string, final: boolean) => void
  /** Fires when the session ends for a reason the caller did not ask for. */
  onError?: (error: Error) => void
  /** Fires with the microphone's current peak, for the level meter. */
  onLevel?: (peak: number) => void
}

export interface LocalAsrHandle {
  /** Ends the utterance and resolves with the final transcript. */
  finish: () => Promise<string>
  /** Abandons the utterance without waiting for a result. */
  cancel: () => void
  /**
   * Closes the current utterance and opens the next one, keeping the
   * microphone open throughout.
   *
   * This is how utterance boundaries are drawn. The alternative — one long
   * session with the caller tracking which prefix it has already sent — falls
   * apart the moment the recognizer revises a word it had already emitted,
   * because the prefix the caller committed no longer matches the text it is
   * slicing. Recycling the session resets the decoder at exactly the boundary
   * the caller chose, and costs one bridge round trip.
   */
  commit: () => Promise<string>
  /**
   * Stops feeding audio without releasing the device. Used while the
   * assistant is speaking: reacquiring a microphone takes long enough to
   * clip the start of the user's next sentence.
   */
  setMuted: (muted: boolean) => void
}

/**
 * Reports whether local recognition can run right now.
 *
 * Distinguishes "this build has no recognizer" from "the model has not been
 * downloaded", because the first is a dead end and the second is a button.
 */
export async function localAsrStatus(): Promise<VoiceStatusResult | undefined> {
  try {
    return await getVoiceBridge().status()
  } catch {
    // An older engine without these methods, or a bridge that is not up.
    // Neither is worth surfacing: the caller falls back to Web Speech.
    return undefined
  }
}

/** Starts the download, returning the progress so far. Safe to call twice. */
export async function installLocalAsr(modelId?: string) {
  return getVoiceBridge().install(modelId ? { modelId } : {})
}

/**
 * Opens a recognition session and streams the microphone into it.
 *
 * Frames are dropped rather than queued when the engine falls behind. A
 * backlog would be the wrong repair: audio that arrives late is audio the
 * recognizer will transcribe after the user has stopped talking, and a queue
 * that grows during a stutter never drains within the utterance.
 */
export async function startLocalAsr(callbacks: LocalAsrCallbacks = {}): Promise<LocalAsrHandle> {
  const bridge = getVoiceBridge()
  let sessionId = (await bridge.start({ language: 'zh-CN' })).sessionId

  let closed = false
  let muted = false
  let swapping = false
  let inFlight = false
  let capture: PcmCaptureHandle | undefined

  const stop = () => {
    closed = true
    capture?.stop()
    capture = undefined
  }

  const fail = (error: unknown) => {
    if (closed) return
    const dying = sessionId
    stop()
    void bridge.stop({ sessionId: dying }).catch(() => {})
    callbacks.onError?.(error instanceof Error ? error : new Error(String(error)))
  }

  try {
    capture = await startPcmCapture({
      onFrame: frame => {
        // The level drives the meter, so it is reported even while muted —
        // otherwise the rings freeze every time the assistant speaks.
        callbacks.onLevel?.(frame.peak)
        if (closed || muted || swapping || inFlight) return
        const owner = sessionId
        inFlight = true
        bridge
          .append({ sessionId: owner, pcm: frame.base64 })
          .then(result => {
            // A reply from a session that has since been retired describes an
            // utterance the caller has already been given.
            if (result.text && owner === sessionId) callbacks.onTranscript?.(result.text, result.final)
          })
          .catch(error => {
            if (owner === sessionId) fail(error)
          })
          .finally(() => {
            inFlight = false
          })
      },
      onError: fail,
    })
  } catch (error) {
    // The session is already open on the engine side; leaving it there would
    // hold the recognizer for a turn that never happens.
    void bridge.stop({ sessionId }).catch(() => {})
    throw error
  }

  return {
    finish: async () => {
      if (closed) return ''
      stop()
      const { text } = await bridge.finish({ sessionId })
      return text
    },
    cancel: () => {
      if (closed) return
      stop()
      void bridge.stop({ sessionId }).catch(() => {})
    },
    commit: async () => {
      if (closed || swapping) return ''
      swapping = true
      const retiring = sessionId
      try {
        const [{ text }, next] = await Promise.all([
          bridge.finish({ sessionId: retiring }),
          bridge.start({ language: 'zh-CN' }),
        ])
        sessionId = next.sessionId
        return text
      } catch (error) {
        fail(error)
        return ''
      } finally {
        swapping = false
      }
    },
    setMuted: next => {
      muted = next
    },
  }
}
