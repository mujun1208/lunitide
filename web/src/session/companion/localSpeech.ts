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
import { looksIncompleteUtterance } from './companionText'
import { startLocalAsr, type LocalAsrHandle } from './localAsr'
import { MOON_RING_BINS } from './MoonSphere'
import {
  BARGE_IN_GUARD_MS,
  BARGE_IN_SETTLE_MS,
  BARGE_IN_THINKING_MIN_CHARS,
  ECHO_GUARD_MS,
  endpointingForText,
  shouldBargeInOverPlayback,
  shouldCommitIncomplete,
  shouldCommitStable,
  shouldCommitUtterance,
  shouldDeferCommit,
  speechProfile,
  type CompanionSpeechHandle,
  type CompanionSpeechOptions,
} from './speech'

/** Endpointing is evaluated on a timer because silence is not an event. */
const TICK_MS = 60

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
  let lastCommittedAt = 0
  let ticker = 0

  const resetUtterance = () => {
    text = ''
    lastTextAt = 0
    textSince = 0
    announcedSpeech = false
  }

  const spoken = () => options.spokenText?.() ?? ''

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
  const recycle = async (emit: 'final' | 'barge-in' | false) => {
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
      lastCommittedAt = Date.now()
      if (emit === 'barge-in') {
        // The tail of the sentence that just fired keeps arriving for a moment
        // after it; without this it would immediately barge in on itself.
        guardUntil = lastCommittedAt + BARGE_IN_GUARD_MS
        options.onBargeIn?.(final)
      } else {
        options.onFinal(final)
      }
    } catch (error) {
      fail(error)
    } finally {
      recycling = false
    }
  }

  const evaluate = () => {
    if (closed || recycling || playback || commitPaused) return
    const trimmed = text.trim()
    if (!trimmed) return
    const now = Date.now()
    const sinceText = now - lastTextAt
    if (shouldDeferCommit(trimmed, now - textSince)) return
    const { stableMs, silenceMs } = endpointingForText(trimmed, profile)
    const silentForMs = lastVoiceAt ? now - lastVoiceAt : 0
    const ready = looksIncompleteUtterance(trimmed)
      ? shouldCommitIncomplete({ silentForMs, silenceMs, msSinceLastResult: sinceText, speechActive })
      : shouldCommitUtterance(true, silentForMs, silenceMs) || shouldCommitStable(true, sinceText, stableMs)
    if (ready) void recycle('final')
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
    onTranscript: next => {
      if (closed) return
      const now = Date.now()
      // Audio captured before the guard closed is the speaker, not the user.
      if (now < guardUntil) return
      const trimmed = next.trim()
      if (!trimmed || trimmed === text.trim()) return
      text = next
      lastTextAt = now
      if (!textSince) textSince = now
      if (!announcedSpeech) {
        announcedSpeech = true
        options.onSpeechStart?.()
      }
      if (playback) {
        // Mid-reply the only question worth asking is whether this is the user
        // talking over her or her own sentence echoing back.
        if (shouldBargeInOverPlayback(trimmed, spoken())) void recycle('barge-in')
        return
      }
      if (commitPaused) {
        // She is thinking, so nothing is coming out of the speakers and there
        // is no echo to reject. The risk is the opposite one: the late tail of
        // the sentence just sent restarting the turn it belongs to. That is
        // what the longer utterance and the settle window are for.
        if (
          Array.from(trimmed).length >= BARGE_IN_THINKING_MIN_CHARS &&
          now - lastCommittedAt >= BARGE_IN_SETTLE_MS
        ) {
          void recycle('barge-in')
        }
        return
      }
      options.onInterim?.(next)
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
      // The microphone stays open: muting it here is what makes a companion
      // impossible to interrupt. Only the feed into the recognizer pauses, and
      // only for the guard window, where the speaker ramp is loudest and echo
      // cancellation has not converged yet. The level meter keeps running.
      window.clearTimeout(unmuteTimer)
      asr?.setMuted(true)
      unmuteTimer = window.setTimeout(() => {
        if (!closed) asr?.setMuted(false)
      }, echoGuardMs)
      // Whatever landed either side of the boundary belongs to the turn that
      // just ended, so the recognizer restarts clean for the next one.
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
