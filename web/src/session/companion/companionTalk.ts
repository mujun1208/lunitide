import { BridgeClientError, getProviderBridge, getTalkBridge, type TalkBridge, type TalkStreamHandle } from '../../bridge/client'
import type { ProviderDTO } from '../../generated/bridge'
import { pickTalkRealtimeModel } from '../../provider/modelKind'
import { looksIncompleteUtterance } from './companionText'
import { startPcmCapture, type PcmCaptureHandle } from './pcmCapture'
import { unlockTtsAudio } from './ttsPlayer'
import type { VoicePath } from './voicePersonas'

export const TALK_FIRST_AUDIO_MS = 8_000
export const TALK_FALLBACK_BANNER = '通话核未就绪，这轮用语模型'

// A single talk connection failure used to latch for the whole stage session,
// trapping the user on the slower cascade even after the network recovered.
// Instead, back off for a cooldown and re-offer talk; only a run of failures
// latches, so a flaky link is retried without thrashing the connect path.
export const TALK_MAX_FAILURES = 3
export const TALK_RETRY_COOLDOWN_MS = 20_000

export type TalkRetryState = { failures: number; lastFailAt: number }

export function newTalkRetryState(): TalkRetryState {
  return { failures: 0, lastFailAt: 0 }
}

/** Whether a fresh talk connection should be withheld right now. */
export function talkRetryBlocked(state: TalkRetryState, now: number): boolean {
  if (state.failures >= TALK_MAX_FAILURES) return true
  if (state.failures > 0 && now - state.lastFailAt < TALK_RETRY_COOLDOWN_MS) return true
  return false
}

/** Record a failed attempt and return the next state. */
export function noteTalkFailure(state: TalkRetryState, now: number): TalkRetryState {
  return { failures: state.failures + 1, lastFailAt: now }
}

/**
 * Talk-realtime is opt-in. Default 火山 runs the single-voice cascade pipeline;
 * only when the user explicitly enables the realtime talk core (optIn) and a
 * realtime model + session exist do we offer the full-duplex talk connection.
 * This keeps the out-of-the-box 火山 experience to exactly one voice.
 */
export function shouldOfferCompanionTalk(
  voicePath: VoicePath,
  hasTalkModel: boolean,
  sessionId?: string,
  optIn?: boolean,
): boolean {
  return optIn === true && voicePath === 'volc' && hasTalkModel && !!sessionId
}

/** Cascade TTS must stay off while talk owns the speaker. Tool handoff
 *  sets talkSuppressPlay so the tool-result cascade can talk after
 *  cancelOutput; a live talk session alone is not enough to unmute it. */
export function companionCascadeSpeechBlocked(input: {
  talkLive: boolean
  talkPending: boolean
  talkSuppressPlay?: boolean
}): boolean {
  if (input.talkSuppressPlay) return false
  return input.talkLive || input.talkPending
}

export function isCompanionIdleChat(text: string): boolean {
  const t = text.replace(/\s+/g, '')
  return /^(你好|你好月汐|今晚月色如何|继续聊|我随便说说|后面那个更好听)[。.?？!！]*$/.test(t)
}

export type CompanionTalkHandle = {
  talkId: string
  streamId: string
  cancelOutput: () => Promise<void>
  stop: () => Promise<void>
}

export type CompanionTalkCallbacks = {
  sessionId: string
  onAudio: (pcmBase64: string, mime: string) => void
  onUserTranscript: (text: string) => void
  onAssistantTranscript: (text: string) => void
  onBarge: () => void
  onToolHandoff: (text: string) => void
  onError: (error: BridgeClientError) => void
  onEnded: () => void
}

export type CompanionTalkDeps = {
  listProviders?: () => Promise<{ items: ProviderDTO[] }>
  talk?: Pick<TalkBridge, 'start'>
  capture?: typeof startPcmCapture
  firstAudioMs?: number
}

export async function startCompanionTalk(
  callbacks: CompanionTalkCallbacks,
  deps: CompanionTalkDeps = {},
): Promise<CompanionTalkHandle | undefined> {
  const listed = await (deps.listProviders ?? (() => getProviderBridge().list()))().catch(() => ({ items: [] as ProviderDTO[] }))
  const picked = pickTalkRealtimeModel(listed.items ?? [])
  if (!picked) return undefined

  await unlockTtsAudio()
  let firstAudio!: (heard: boolean) => void
  const firstAudioGate = new Promise<boolean>(resolve => {
    firstAudio = heard => resolve(heard)
  })
  let settledFirst = false
  const markFirst = (heard: boolean) => {
    if (settledFirst) return
    settledFirst = true
    firstAudio(heard)
  }

  let stream: TalkStreamHandle | undefined
  let capture: PcmCaptureHandle | undefined
  let stopped = false
  const stop = async () => {
    if (stopped) return
    stopped = true
    markFirst(false)
    await capture?.stop().catch(() => undefined)
    capture = undefined
    await stream?.cancel('all').catch(() => undefined)
  }

  try {
    const talk = deps.talk ?? getTalkBridge()
    stream = await talk.start(
      { providerId: picked.providerId, modelId: picked.modelId, sessionId: callbacks.sessionId },
      event => {
        if (stopped) return
        if (event.type === 'audio') {
          markFirst(true)
          callbacks.onAudio(event.audioBase64, event.mime)
          return
        }
        if (event.type === 'transcript') {
          if (event.role === 'user') callbacks.onUserTranscript(event.text)
          else callbacks.onAssistantTranscript(event.text)
          return
        }
        if (event.type === 'tool' && event.name === 'handoff' && event.text && !looksIncompleteUtterance(event.text)) {
          void stream?.cancel('output')
          callbacks.onToolHandoff(event.text)
          return
        }
        if (event.type === 'error' && event.code === 'TALK_BARGE') {
          callbacks.onBarge()
          return
        }
        if (event.type === 'error') {
          markFirst(false)
          callbacks.onError(new BridgeClientError(event.message, event.code, true, stream?.streamId ?? 'talk'))
          return
        }
        if (event.type === 'ended') {
          markFirst(false)
          callbacks.onEnded()
        }
      },
    )
  } catch (error) {
    await stop()
    if (error instanceof BridgeClientError) callbacks.onError(error)
    return undefined
  }

  try {
    capture = await (deps.capture ?? startPcmCapture)({
      onFrame: frame => {
        if (stopped || !stream) return
        void stream.append(frame.base64)
      },
      onError: error => {
        if (!stopped) callbacks.onError(error)
      },
    })
  } catch (error) {
    await stop()
    if (error instanceof BridgeClientError) callbacks.onError(error)
    return undefined
  }

  if (deps.firstAudioMs != null) {
    const heard = await Promise.race([
      firstAudioGate,
      new Promise<boolean>(resolve => {
        window.setTimeout(() => resolve(false), deps.firstAudioMs)
      }),
    ])
    if (!heard || stopped) {
      await stop()
      return undefined
    }
  }

  return {
    talkId: stream.talkId,
    streamId: stream.streamId,
    cancelOutput: () => stream?.cancel('output') ?? Promise.resolve(),
    stop,
  }
}
