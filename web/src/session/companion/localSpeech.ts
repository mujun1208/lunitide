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
  shouldDeferCommit,
  speechProfile,
  type CompanionSpeechHandle,
  type CompanionSpeechOptions,
} from './speech'

/** Endpointing is evaluated on a timer because silence is not an event. */
const TICK_MS = 60

/**
 * How long to wait on an engine that has stopped reporting endpoints.
 *
 * Only reached when the recognizer never says a turn ended, so it is sized to
 * be unmistakably longer than a pause someone leaves mid-sentence. A shorter
 * value here does not make the companion quicker — the engine answers first
 * in every healthy turn — it only makes it interrupt.
 */
export const ENDPOINT_BACKSTOP_MS = 3500

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
  const bars = silentBars()

  let closed = false
  let asr: LocalAsrHandle | undefined
  let text = ''
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
      const settled = (await asr.commit()).trim()
      resetUtterance()
      if (!emit) return
      // The streamed partial is the fallback: a commit that races the last
      // append can come back empty, and dropping the sentence is worse than
      // sending the slightly staler copy the user already saw as a caption.
      const final = settled || carried
      if (!final) return
      options.onFinal(final)
    } catch (error) {
      fail(error)
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
    const trimmed = text.trim()
    if (!trimmed) return
    const now = Date.now()
    if (shouldDeferCommit(trimmed, now - textSince)) return
    if (speechActive) return
    if (now - lastTextAt < ENDPOINT_BACKSTOP_MS) return
    void recycle('final')
  }

  asr = await startLocalAsr({
    onLevel: peak => {
      if (closed) return
      bars.shift()
      bars.push(Math.min(1, Math.sqrt(Math.max(0, peak) / FULL_SCALE_PEAK)))
      options.onLevels?.([...bars])
      const now = Date.now()
      if (peak >= profile.voicePeak) {
        lastVoiceAt = now
        speechActive = true
        if (!playback) options.onVoiceEnergy?.()
      } else if (lastVoiceAt && now - lastVoiceAt > profile.utteranceSilenceMs) {
        speechActive = false
      }
    },
    onTranscript: (next, final) => {
      if (closed) return
      const now = Date.now()
      // Audio captured before the guard closed is the speaker, not the user.
      if (now < guardUntil) return
      if (playback || commitPaused) {
        // Her turn, so this is her own voice off the speaker — dropped
        // before it is recorded, not after.
        //
        // Recording it first and refusing to act on it was not enough: the
        // buffer still held her sentence when her turn ended, so it surfaced
        // a beat later as a caption flashing her own last line back at the
        // user, and could be committed as though they had said it. Nothing
        // heard during her turn belongs to the user's next one.
        return
      }
      const trimmed = next.trim()
      if (!trimmed) return
      if (trimmed !== text.trim()) {
        text = next
        lastTextAt = now
        if (!textSince) textSince = now
        if (!announcedSpeech) {
          announcedSpeech = true
          options.onSpeechStart?.()
        }
        options.onInterim?.(next)
      }
      // The engine has decided the speaker stopped. It reaches that from the
      // decoder's own state and the trailing silence it measured, which is
      // the evidence this side of the bridge never had.
      if (final) void recycle('final')
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
      void recycle('final')
    },
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
