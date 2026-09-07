/**
 * Volc seed-asr wearing the same Web Speech handle as localSpeech.
 *
 * Endpointing stays in speech.ts. This file only swaps who transcribes.
 * Isolated from sherpa: do not import localAsr runtime, only the handle type.
 */
import { BridgeClientError } from '../../../bridge/client'
import { startVolcAsr, type VolcAsrHandle } from './volcAsr'
import { MOON_RING_BINS } from '../MoonSphere'
import {
  ECHO_GUARD_MS,
  TURN_END_SILENCE_MS,
  shouldDeferCommit,
  completeCaptionAtVoiceDeadline,
  shouldCommitHeardUtterance,
  shouldForceCommitUtterance,
  speechProfile,
  turnEndWindows,
  type CompanionSpeechHandle,
  type CompanionSpeechOptions,
} from '../speech'
import { looksIncompleteUtterance, looksLikeBargeInSpeech, looksLikePlaybackEcho } from '../companionText'
import { pickTranscriptRevision } from '../transcriptRevision'

/** Endpointing is evaluated on a timer because silence is not an event. */
const TICK_MS = 60

/**
 * Reached only if the engine never reports an endpoint. Sized just above the
 * incomplete hard ceiling so a stuck recognizer still commits, not mid-breath.
 */
export const ENDPOINT_BACKSTOP_MS = 2300

/** Peak that paints a full ring. Chosen so normal speech sits mid-scale. */
const FULL_SCALE_PEAK = 0.35

/**
 * Wait this long after she starts talking before a non-echo transcript is
 * treated as barge-in. Covers the pad syllable coming back through the mic
 * without waiting the full echo-guard used when unmuting after she stops.
 */
export const BARGE_IN_ARM_MS = 160

const silentBars = () => Array.from({ length: MOON_RING_BINS }, () => 0)

const asBridgeError = (error: unknown): BridgeClientError =>
  error instanceof BridgeClientError
    ? error
    : new BridgeClientError(
        error instanceof Error && error.message ? error.message : '火山语音识别中断',
        'SPEECH_RECOGNITION_UNAVAILABLE',
        false,
        'renderer',
      )

export async function startVolcCompanionSpeech(
  options: CompanionSpeechOptions,
  providerId: string,
): Promise<CompanionSpeechHandle> {
  const profile = speechProfile(options.environment)
  const windows = turnEndWindows(options.holdUtterance)
  const holdUtterance = options.holdUtterance === true
  const bars = silentBars()

  let closed = false
  let asr: VolcAsrHandle | undefined
  let text = ''
  let providerEnded = false
  let timestamped = false
  let lastTextAt = 0
  let textSince = 0
  let lastVoiceAt = 0
  let speechActive = false
  let announcedSpeech = false
  let playback = false
  let listeningThrough = false
  let bargedThisPlayback = false
  let playbackStartedAt = 0
  let guardUntil = 0
  let unmuteTimer = 0
  let commitPaused = false
  let recycling = false
  let pendingEvaluate = false
  let ticker = 0

  const resetUtterance = () => {
    text = ''
    providerEnded = false
    lastTextAt = 0
    lastVoiceAt = 0
    speechActive = false
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
  const recycle = async (emit: 'final' | false, readyAtVoiceDeadline = false) => {
    if (!asr || closed || recycling) return
    recycling = true
    const carried = text.trim()
    try {
      let settled = ''
      try {
        if (readyAtVoiceDeadline) {
          // Submit the complete streamed sentence now. Retire/flush the ASR
          // session concurrently; a slow finish/refiner cannot delay the model.
          const finishing = asr.commit({ useStreamed: true })
          resetUtterance()
          options.onFinal(carried)
          void finishing.catch(error => { if (!closed) fail(error) })
          return
        }
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
      if (closed) return
      const fresh = text.trim()
      if (emit === 'final') {
        resetUtterance()
        const final = pickTranscriptRevision(pickTranscriptRevision(carried, fresh), settled)
        if (!final) return
        options.onFinal(final)
        return
      }
      // Non-final recycle seals her echo window. The user often starts the
      // next turn while commit() is still in flight — clearing unconditionally
      // here left the caption on screen with an empty recognizer buffer, so
      // endpointing never fired and the stage sat on 聆听中 after round 3+.
      if (!fresh || fresh === carried) {
        resetUtterance()
      }
    } finally {
      recycling = false
      if (pendingEvaluate) {
        pendingEvaluate = false
        evaluate()
      } else if (!closed && !playback && !commitPaused && text.trim()) {
        evaluate()
      }
    }
  }

  /** Complete voice turns end after 1.2s of actual silence, without adding
   * a second text/provider wait. Meetings keep the longer hold. Without an
   * energy sample, provider finality and stable-text backstops still apply. */
  const evaluate = () => {
    if (closed || playback || commitPaused) return
    if (recycling) {
      pendingEvaluate = true
      return
    }
    if (Date.now() < guardUntil) return
    const trimmed = text.trim()
    if (!trimmed) return
    const now = Date.now()
    const incomplete = looksIncompleteUtterance(trimmed)
    const silentForMs = lastVoiceAt ? now - lastVoiceAt : undefined
    if (completeCaptionAtVoiceDeadline({ holdUtterance, silentForMs, incomplete })) {
      void recycle('final', true)
      return
    }
    // With a real energy clock, an ordinary breath is still this sentence.
    if (!holdUtterance && silentForMs !== undefined && !incomplete && silentForMs < TURN_END_SILENCE_MS) return
    if (shouldDeferCommit(trimmed, now - textSince)) return
    const textStableForMs = lastTextAt ? now - lastTextAt : 0
    // A known provider segment is still being revised. A previous sentence's
    // definite flag cannot end this one; retain a bounded stalled-ASR fallback.
    if (timestamped && !providerEnded && textStableForMs < ENDPOINT_BACKSTOP_MS) return
    if (
      !shouldCommitHeardUtterance({
        speechActive,
        silentForMs: lastVoiceAt ? now - lastVoiceAt : undefined,
        textStableForMs,
        incomplete: looksIncompleteUtterance(trimmed),
        holdUtterance,
        silenceMs: windows.silenceMs,
        incompleteSilenceMs: windows.incompleteSilenceMs,
      })
    ) {
      return
    }
    void recycle('final')
  }

  const considerBargeIn = (heard: string) => {
    if (!options.bargeIn?.() || !options.onBargeIn) return
    if (bargedThisPlayback) return
    if (Date.now() < playbackStartedAt + BARGE_IN_ARM_MS) return
    const trimmed = heard.trim()
    if (!looksLikeBargeInSpeech(trimmed, options.spokenText?.() ?? '')) return
    bargedThisPlayback = true
    options.onBargeIn(trimmed)
  }

  asr = await startVolcAsr(providerId, {
    extraStreams: options.externalPcm ? undefined : options.extraStreams,
    externalPcm: options.externalPcm,
    endWindowMs: options.endWindowMs,
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
    onTranscript: (next, final, hasPositions) => {
      if (closed) return
      const now = Date.now()
      // Audio captured during her reply is the speaker, not the user —
      // unless barge-in is on, in which case a non-echo transcript is the
      // user cutting in. Never commit() here: that would take the turn as a
      // normal final and skip the echo filter the stage already applies.
      if (playback || commitPaused) {
        if (looksLikePlaybackEcho(next, options.spokenText?.() ?? '')) {
          asr?.discardTranscript?.()
          return
        }
        if (playback) considerBargeIn(next)
        return
      }
      const trimmed = next.trim()
      if (!trimmed) return
      if (looksLikePlaybackEcho(trimmed, options.spokenText?.() ?? '')) {
        asr?.discardTranscript?.()
        resetUtterance()
        return
      }
      // volcAsr already owns the full-snapshot cursor. Every update here is a
      // replacement for the current product turn, including meeting captions.
      // Appending a revision is what multiplied whole paragraphs in recordings.
      const absorbed = pickTranscriptRevision(text, next)
      providerEnded = final
      timestamped = hasPositions === true
      if (absorbed !== text.trim()) {
        text = absorbed
        lastTextAt = now
        if (!textSince) textSince = now
        speechActive = true
        if (!announcedSpeech) {
          announcedSpeech = true
          options.onSpeechStart?.()
        }
        options.onInterim?.(text)
      }
      if (now < guardUntil) return
      if (final) {
        // Engine endpoint is not the product endpoint. Incomplete phrases stay
        // open through a micro-pause; complete ones may settle on silence.
        if (!looksIncompleteUtterance(text.trim())) {
          speechActive = false
        }
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
    resetSession: () => {
      resetUtterance()
    },
    setCommitPaused: paused => {
      commitPaused = paused
    },
    setAssistantPlayback: (active, echoGuardMs = ECHO_GUARD_MS) => {
      if (closed) return
      const listenThrough = active && options.bargeIn?.() === true
      if (active === playback && listenThrough === listeningThrough) return
      const starting = active && !playback
      playback = active
      listeningThrough = listenThrough
      guardUntil = Date.now() + echoGuardMs
      if (starting) {
        asr?.discardTranscript?.()
        playbackStartedAt = Date.now()
        bargedThisPlayback = false
      }
      if (!active) bargedThisPlayback = false
      // Keep the Volc websocket. Recycle used to commit/reopen on every TTS
      // boundary, which is the cold start after she speaks. Mute still drops
      // frames (or barge-in leaves them flowing). The utterance buffer is
      // cleared so her last words cannot become the next turn.
      window.clearTimeout(unmuteTimer)
      if (active) {
        asr?.setMuted(!listenThrough)
      } else {
        asr?.setMuted(true)
        unmuteTimer = window.setTimeout(() => {
          if (!closed) asr?.setMuted(false)
        }, echoGuardMs)
      }
      resetUtterance()
    },
    forceCommit: (fallback?: string) => {
      if (playback || commitPaused) return false
      const fromBuffer = text.trim()
      const trimmed = fromBuffer || (fallback ?? '').trim()
      if (!trimmed) return false
      if (!fromBuffer) {
        if (looksIncompleteUtterance(trimmed)) return false
        text = trimmed
        lastTextAt = Date.now()
        void recycle('final')
        return true
      }
      const now = Date.now()
      if (timestamped && !providerEnded && now - lastTextAt < ENDPOINT_BACKSTOP_MS) return false
      if (
        !shouldForceCommitUtterance({
          speechActive,
          silentForMs: lastVoiceAt ? now - lastVoiceAt : undefined,
          textStableForMs: lastTextAt ? now - lastTextAt : 0,
          incomplete: looksIncompleteUtterance(fromBuffer),
          silenceMs: windows.silenceMs,
          incompleteSilenceMs: windows.incompleteSilenceMs,
        })
      ) {
        return false
      }
      void recycle('final')
      return true
    },
    flush: () => recycle('final'),
    pulseRecognition: () => {
      if (closed || playback || commitPaused) return
      if (text.trim()) {
        void recycle('final')
        return
      }
      void asr?.restart()
    },
    resumeCapture: () => {
      if (closed) return
      if (options.bargeIn?.() === true) {
        asr?.setMuted(false)
        return
      }
      if (playback || commitPaused || Date.now() < guardUntil) return
      asr?.setMuted(false)
    },
    pushPcm: frame => {
      if (closed) return
      asr?.pushFrame?.(frame)
    },
  }
}
