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
import { int16ToBase64, TARGET_SAMPLE_RATE } from './pcmFrames'
import { startPcmCapture, type PcmCaptureHandle } from './pcmCapture'

/** Ceiling on audio waiting to be sent, in samples. Two seconds at 16 kHz. */
const MAX_PENDING_SAMPLES = TARGET_SAMPLE_RATE * 2

/** Bound on how long a drain will wait for the engine before giving up. */
const DRAIN_ROUNDS = 40

/**
 * Companion 'auto' must not wait the full voice.status bridge deadline
 * (8s) before opening the system recognizer. If the sidecar is slow or
 * hung, talking has to work on Web Speech in this window.
 */
export const LOCAL_ASR_DECISION_MS = 400

/** Race a boolean probe against a deadline; hanging probes become false. */
export async function readyWithin(probe: Promise<boolean> | undefined, timeoutMs = LOCAL_ASR_DECISION_MS): Promise<boolean> {
  if (!probe) return false
  let timer = 0
  try {
    return await Promise.race([
      probe.catch(() => false),
      new Promise<boolean>(resolve => {
        timer = window.setTimeout(() => resolve(false), timeoutMs)
      }),
    ])
  } finally {
    window.clearTimeout(timer)
  }
}

/** Yields to the microtask queue and one macrotask, so replies can land. */
const settled = () => new Promise<void>(resolve => window.setTimeout(resolve, 5))

export interface LocalAsrCallbacks {
  /** Fires as words arrive, with `final` set once the recognizer settles. */
  onTranscript?: (text: string, final: boolean) => void
  /** Extra this-PC streams mixed into the microphone frames (meeting loopback). */
  extraStreams?: MediaStream[]
  /** Fires when the session ends for a reason the caller did not ask for. */
  onError?: (error: Error) => void
  /**
   * Fires when one utterance could not be transcribed but recognition is
   * still running. Separate from `onError` because the caller's response is
   * different: this loses a sentence, that loses the microphone.
   */
  onTranscriptLost?: (error: Error) => void
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
 * Points the caption recognizer at a different model.
 *
 * Retires the running one, so the next turn starts on the chosen weights
 * rather than the previous process finishing the conversation on the old
 * ones.
 */
export async function selectLocalAsrModel(modelId: string) {
  return getVoiceBridge().select({ modelId })
}

/**
 * Opens a recognition session and streams the microphone into it.
 *
 * The microphone is opened before the session rather than after it. Opening a
 * session can mean waiting for the engine to load a model, which takes
 * seconds; doing that first meant the user pressed the button, started
 * talking, and was recorded by nothing at all until it finished. Audio
 * captured in that window waits in the queue below and goes in as soon as
 * there is somewhere to put it.
 */
export async function startLocalAsr(callbacks: LocalAsrCallbacks = {}): Promise<LocalAsrHandle> {
  const bridge = getVoiceBridge()
  let sessionId = ''

  let closed = false
  let muted = false
  let swapping = false
  let inFlight = false
  /** Whether the open session has been given any audio since the last commit. */
  let fed = false
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
    if (dying) void bridge.stop({ sessionId: dying }).catch(() => {})
    const opened = await bridge.start({ language: 'zh-CN' })
    if (closed) {
      void bridge.stop({ sessionId: opened.sessionId }).catch(() => {})
      return
    }
    sessionId = opened.sessionId
  }

  /**
   * Audio captured while an append is in flight, waiting its turn.
   *
   * Frames used to be discarded in that window on the grounds that late audio
   * is transcribed after the user has stopped talking. That reasoning belongs
   * to a queue of *requests*; this is a queue of *samples*, and a sample that
   * is never sent is a sound the recognizer never hears. It came back as the
   * missing couple of characters in the middle of a sentence.
   */
  let pending: { base64: string; samples: Int16Array }[] = []
  let pendingSamples = 0

  /** Joins everything queued into one payload, reusing the frame when alone. */
  const takePending = (): string | undefined => {
    if (pending.length === 0) return undefined
    if (pending.length === 1) {
      const only = pending[0]!
      pending = []
      pendingSamples = 0
      return only.base64
    }
    const merged = new Int16Array(pendingSamples)
    let at = 0
    for (const frame of pending) {
      merged.set(frame.samples, at)
      at += frame.samples.length
    }
    pending = []
    pendingSamples = 0
    return int16ToBase64(merged)
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
        // A reply from a session that has since been retired describes an
        // utterance the caller has already been given.
        if (result.text && owner === sessionId) callbacks.onTranscript?.(result.text, result.final)
      })
      .catch(async error => {
        if (closed || owner !== sessionId) return
        if (retryableVoice(error)) {
          try {
            const result = await bridge.append({ sessionId: owner, pcm })
            if (result.text && owner === sessionId) callbacks.onTranscript?.(result.text, result.final)
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

  /** Resolves once the recognizer has been given everything captured so far. */
  const drain = async () => {
    for (let guard = 0; guard < DRAIN_ROUNDS && !closed; guard++) {
      if (!inFlight && pending.length === 0) return
      pump()
      await settled()
    }
  }

  capture = await startPcmCapture({
    extraStreams: callbacks.extraStreams,
    onFrame: frame => {
      // The level drives the meter, so it is reported even while muted —
      // otherwise the rings freeze every time the assistant speaks.
      callbacks.onLevel?.(frame.peak)
      if (closed || muted) return
      pending.push({ base64: frame.base64, samples: frame.samples })
      pendingSamples += frame.samples.length
      // An engine that has fallen behind for this long is not going to
      // catch up within the utterance, and an unbounded queue would trade
      // the missing characters for growing memory and a caption drifting
      // ever further behind the speaker.
      while (pendingSamples > MAX_PENDING_SAMPLES && pending.length > 1) {
        pendingSamples -= pending.shift()!.samples.length
      }
      pump()
    },
    onError: fail,
  })

  try {
    sessionId = (await bridge.start({ language: 'zh-CN' })).sessionId
  } catch (error) {
    // Nothing to retire on the engine side — the session never opened — but
    // the microphone is ours and is already running.
    stop()
    throw error
  }
  // Whatever was said while the engine was loading its model.
  pump()

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
      // The last fraction of a second of the sentence, which the accumulator
      // is holding back because it did not fill a whole frame. Nothing else
      // ever asks for it, so before this it was discarded at the end of every
      // single utterance — the final syllable, every time.
      capture?.flush()
      await drain()
      if (closed) return ''
      // A session that was never fed has nothing to transcribe and nothing to
      // reset, so the boundary costs nothing. This is the common case now that
      // the microphone is muted for the whole of the assistant's turn: without
      // it, every turn paid two round trips to retire a session that had heard
      // silence, and asked the recognizer to decode an empty utterance.
      if (!fed) return ''
      swapping = true
      const retiring = sessionId
      try {
        // Both at once. The new session does not need the old one's
        // transcript, and running them in order puts a whole round trip
        // between the user finishing a sentence and the microphone being
        // live again for the next one.
        const [transcript, opened] = await Promise.allSettled([
          bridge.finish({ sessionId: retiring }),
          bridge.start({ language: 'zh-CN' }),
        ])
        if (opened.status === 'rejected') {
          // Without a session there is nothing to append to and every frame
          // after this one fails identically, so this is the one outcome
          // worth ending recognition over.
          fail(opened.reason)
          return ''
        }
        sessionId = opened.value.sessionId
        fed = false
        if (transcript.status === 'rejected') {
          // One turn's words, not the microphone.
          //
          // This used to end the session, which meant a single bad commit —
          // a decode that timed out, a turn that ended with nothing to
          // recognize — left the user talking to something that had stopped
          // listening for the rest of the visit, with the first turn having
          // worked normally. Recognition is already running again on the new
          // session, so the cost is this sentence and not the conversation.
          callbacks.onTranscriptLost?.(
            transcript.reason instanceof Error ? transcript.reason : new Error(String(transcript.reason)),
          )
          return ''
        }
        return transcript.value.text
      } finally {
        swapping = false
        pump()
      }
    },
    setMuted: next => {
      if (muted === next) return
      muted = next
      // Audio queued behind the mute belongs to the assistant's turn: commit
      // has already drained everything the user said. Sending it would put
      // her own voice into the session listening for their next sentence.
      if (next) {
        pending = []
        pendingSamples = 0
      }
    },
  }
}
