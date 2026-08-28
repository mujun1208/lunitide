/**
 * Local recognition wearing the Web Speech handle.
 *
 * The stage drives one interface and never learns which recognizer is behind
 * it. What is deliberately *not* duplicated here is the endpointing: the tiered
 * rules live in speech.ts and are imported, so choosing a recognizer changes
 * who transcribes the audio, not how the companion decides a sentence ended.
 * Two copies of those rules would drift, and the drift would show up as the
 * companion interrupting on one engine but not the other.
 */
import { BridgeClientError } from '../../bridge/client'
import { startLocalAsr, type LocalAsrHandle } from './localAsr'
import { MOON_RING_BINS } from './MoonSphere'
import {
  ECHO_GUARD_MS,
  TURN_END_SILENCE_MS,
  shouldDeferCommit,
  shouldForceCommitUtterance,
  speechProfile,
  turnEndWindows,
  type CompanionSpeechHandle,
  type CompanionSpeechOptions,
} from './speech'
import { looksIncompleteUtterance, looksLikePlaybackEcho } from './companionText'
import { absorbHeldTranscript } from '../../meetings/meetingText'

/** Endpointing is evaluated on a timer because silence is not an event. */
const TICK_MS = 60

/**
 * Reached only if the engine never reports an endpoint. Sized to the
 * 1.2s turn-end window plus a little slack, not a multi-second stall.
 */
export const ENDPOINT_BACKSTOP_MS = 1400

/** Peak that paints a full ring. Chosen so normal speech sits mid-scale. */
const FULL_SCALE_PEAK = 0.35

const silentBars = () => Array.from({ length: MOON_RING_BINS }, () => 0)

const asBridgeError = (error: unknown): BridgeClientError =>
  error instanceof BridgeClientError
    ? error
    : new BridgeClientError(
        error instanceof Error && error.message ? error.message : '本地语音识别中断',
        'SPEECH_RECOGNITION_UNAVAILABLE',
        false,
        'renderer',
      )

export async function startLocalCompanionSpeech(options: CompanionSpeechOptions): Promise<CompanionSpeechHandle> {
  const profile = speechProfile(options.environment)
  const windows = turnEndWindows(options.holdUtterance)
  const holdUtterance = options.holdUtterance === true
  const bars = silentBars()

  let closed = false
  let asr: LocalAsrHandle | undefined
  let text = ''
  let sealed = ''
  let lastTextAt = 0
  let textSince = 0
  let lastVoiceAt = 0
  let speechActive = false
  let announcedSpeech = false
  let playback = false
  let guardUntil = 0
  let unmuteTimer = 0
  let commitPaused = false
  let recycling = false
  let ticker = 0

  const resetUtterance = () => {
    text = ''
    sealed = ''
    lastTextAt = 0
    textSince = 0
    announcedSpeech = false
  }

  const teardown = () => {
    closed = true
    window.clearInterval(ticker)
    window.clearTimeout(unmuteTimer)
    asr?.cancel()
    asr = undefined
    options.onLevels?.(silentBars())
  }

  const fail = (error: unknown) => {
    if (closed) return
    teardown()
    options.onError(asBridgeError(error))
  }

  /**
   * Closes the current utterance. `emit` is false when the text is known to be
   * the companion's own voice coming back through the microphone, which must
   * reset the recognizer without ever reaching the stage.
   */
  const recycle = async (emit: 'final' | false) => {
    if (!asr || closed || recycling) return
    recycling = true
    const carried = text.trim()
    try {
      let settled = ''
      try {
        settled = (
          await Promise.race([
            asr.commit(),
            new Promise<string>(resolve => {
              window.setTimeout(() => resolve(''), 4000)
            }),
          ])
        ).trim()
      } catch (error) {
        fail(error)
        return
      }
      resetUtterance()
      if (!emit) return
      const final = settled || carried
      if (!final) return
      options.onFinal(final)
    } finally {
      recycling = false
    }
  }

  /**
   * The engine's endpoint is what ends a turn. This is the backstop for a
   * recognizer that stops reporting them at all.
   *
   * Deliberately much longer than the engine's own window, so it stays a
   * backstop: the previous rules here decided turns from microphone energy
   * and how long the transcript had been unchanged, and both of those are
   * shorter than an ordinary pause mid-sentence. They ended turns half-said.
   */
  const evaluate = () => {
    if (closed || recycling || playback || commitPaused) return
    if (Date.now() < guardUntil) return
    const trimmed = text.trim()
    if (!trimmed) return
    const now = Date.now()
    if (shouldDeferCommit(trimmed, now - textSince)) return
    const textStableForMs = lastTextAt ? now - lastTextAt : 0
    if (
      !shouldForceCommitUtterance({
        speechActive,
        silentForMs: lastVoiceAt ? now - lastVoiceAt : undefined,
        textStableForMs,
        incomplete: looksIncompleteUtterance(trimmed),
        silenceMs: windows.silenceMs,
        incompleteSilenceMs: windows.incompleteSilenceMs,
      })
    ) {
      return
    }
    void recycle('final')
  }

  asr = await startLocalAsr({
    extraStreams: options.extraStreams,
    onLevel: peak => {
      if (closed) return
      bars.shift()
      bars.push(Math.min(1, Math.sqrt(Math.max(0, peak) / FULL_SCALE_PEAK)))
      options.onLevels?.([...bars])
      const now = Date.now()
      const textLocked = !!text.trim() && lastTextAt > 0 && now - lastTextAt >= TURN_END_SILENCE_MS
      if (peak >= profile.voicePeak) {
        if (!textLocked) {
          lastVoiceAt = now
          speechActive = true
        }
        if (!playback) options.onVoiceEnergy?.()
      } else if (lastVoiceAt && now - lastVoiceAt > profile.utteranceSilenceMs) {
        speechActive = false
      }
    },
    onTranscript: (next, final) => {
      if (closed) return
      const now = Date.now()
      // Audio captured during her reply is the speaker, not the user.
      if (playback || commitPaused) {
        return
      }
      const trimmed = next.trim()
      if (!trimmed) return
      if (looksLikePlaybackEcho(trimmed, options.spokenText?.() ?? '')) {
        resetUtterance()
        return
      }
      const absorbed = holdUtterance ? absorbHeldTranscript(sealed || text, next) : next
      if (absorbed !== text.trim()) {
        text = absorbed
        lastTextAt = now
        if (!textSince) textSince = now
        if (!announcedSpeech) {
          announcedSpeech = true
          options.onSpeechStart?.()
        }
        options.onInterim?.(text)
      }
      if (now < guardUntil) return
      if (final && holdUtterance) {
        sealed = text.trim()
        return
      }
      if (final) {
        speechActive = false
        // Engine endpoint is not the product endpoint. Wait 1.2s of true
        // silence after they stopped, complete phrase or not.
        evaluate()
      }
    },
    onTranscriptLost: () => {
      // The microphone is still live, so the repair is to say it again. Said
      // out loud because the alternative is a sentence that vanishes with no
      // explanation, which reads as the companion ignoring the user.
      resetUtterance()
      options.onEngineHint?.('刚才那句没听清，请再说一遍')
    },
    onError: fail,
  })

  ticker = window.setInterval(evaluate, TICK_MS)

  return {
    stop: teardown,
    setCommitPaused: paused => {
      commitPaused = paused
    },
    setAssistantPlayback: (active, echoGuardMs = ECHO_GUARD_MS) => {
      if (closed || active === playback) return
      playback = active
      guardUntil = Date.now() + echoGuardMs
      // Muted for the whole reply, not just the ramp.
      //
      // Leaving it open was how the user could cut in by talking, and the
      // cost of that was a decision no transcript can make reliably: two
      // characters that did not match the sentence currently playing ended
      // her turn, so a television, someone else in the room, or her own voice
      // recognized a beat late truncated the answer mid-word. Interrupting is
      // the 打断 button's job — it is unambiguous, it is what the setting
      // describing this behaviour already tells the user to reach for, and it
      // works while she is thinking too, where the microphone still does.
      //
      // Muting also removes echo as a category rather than guessing at it.
      window.clearTimeout(unmuteTimer)
      asr?.setMuted(true)
      if (!active) {
        unmuteTimer = window.setTimeout(() => {
          if (!closed) asr?.setMuted(false)
        }, echoGuardMs)
      }
      // Whatever landed either side of the boundary belongs to the turn that
      // just ended. Cleared here and now rather than when the commit below
      // comes back, so there is no window where the next turn can start on
      // top of the last one's words.
      resetUtterance()
      void recycle(false)
    },
    forceCommit: () => {
      const trimmed = text.trim()
      if (!trimmed || playback || commitPaused) return
      const now = Date.now()
      if (
        !shouldForceCommitUtterance({
          speechActive,
          silentForMs: lastVoiceAt ? now - lastVoiceAt : undefined,
          textStableForMs: lastTextAt ? now - lastTextAt : 0,
          incomplete: looksIncompleteUtterance(trimmed),
          silenceMs: windows.silenceMs,
          incompleteSilenceMs: windows.incompleteSilenceMs,
        })
      ) {
        return
      }
      void recycle('final')
    },
    flush: () => recycle('final'),
    pulseRecognition: () => {
      /* The cloud recognizer stops returning results while still claiming to
         listen, which is what pulsing repairs. The sidecar either answers or
         reports an error, so there is no silent stall to kick. */
    },
    resumeCapture: () => {
      if (closed) return
      asr?.setMuted(false)
    },
  }
}
