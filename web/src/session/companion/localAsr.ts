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
  const { sessionId } = await bridge.start({ language: 'zh-CN' })

  let closed = false
  let inFlight = false
  let capture: PcmCaptureHandle | undefined

  const stop = () => {
    closed = true
    capture?.stop()
    capture = undefined
  }

  const fail = (error: unknown) => {
    if (closed) return
    stop()
    void bridge.stop({ sessionId }).catch(() => {})
    callbacks.onError?.(error instanceof Error ? error : new Error(String(error)))
  }

  try {
    capture = await startPcmCapture({
      onFrame: frame => {
        callbacks.onLevel?.(frame.peak)
        if (closed || inFlight) return
        inFlight = true
        bridge
          .append({ sessionId, pcm: frame.base64 })
          .then(result => {
            if (result.text) callbacks.onTranscript?.(result.text, result.final)
          })
          .catch(fail)
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
  }
}
