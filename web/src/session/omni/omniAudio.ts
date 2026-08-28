import { startPcmCapture, type PcmCaptureHandle } from '../companion/pcmCapture'
import { accumulateSpeakableCaption, looksLikeOmniPersonaCaption, stripTaskDonePhrases } from '../companion/companionText'
import { sharedTtsAudioContext, unlockTtsAudio } from '../companion/ttsPlayer'
import { getOmniBridge } from '../../bridge/client'

export interface OmniCompanionHandle {
  stop: () => void
  /**
   * Send the PCM buffered during the user's turn. Call after 1.2s of real
   * silence so MiniCPM-o answers one fluent utterance, not 100ms crumbs.
   */
  commitUserAudio: () => boolean
  /** Open the next listen: hold PCM again, drop leftover playback. */
  resumeListen: () => void
}

export interface OmniCompanionOptions {
  personaId: string
  onText: (text: string) => void
  onError: (message: string) => void
  onSpeaking?: (speaking: boolean) => void
}

const READY_MS = 90_000
const POLL_MS = 700
/** Status probe budget: MiniCPM-o is optional, so a hung bridge must not block talking. */
export const OMNI_PROBE_MS = 4_000
/** One hung append must not stall the whole duplex chain. */
export const OMNI_APPEND_MS = 8_000
const OMNI_APPEND_FAILS_BEFORE_ERROR = 3
/** 100ms frames × 300 = 30s of held user audio. */
const MAX_HELD_FRAMES = 300
const PLAYBACK_OVERLAP_S = 0.04

export interface OmniChannelSnapshot {
  ready?: boolean
  installed?: boolean
  runtimeFound?: boolean
  hostState?: string
  lastError?: string
}

export const OMNI_MISSING_MODEL = '请先在设置里下载 MiniCPM-o 4.5 Q4'
export const OMNI_MISSING_RUNTIME = '本机 MiniCPM-o 推理进程未能展开，请重装月汐后再试'

/** User-facing block before MiniCPM-o can start. Missing model is a download, not a missing server. */
export function omniStartBlock(snap: OmniChannelSnapshot): string | undefined {
  if (snap.hostState === 'missing_model') return OMNI_MISSING_MODEL
  if (snap.hostState === 'missing_runtime') return OMNI_MISSING_RUNTIME
  if (snap.hostState === 'failed') return snap.lastError || 'MiniCPM-o 启动失败'
  return undefined
}

/** Whether MiniCPM-o can start a duplex session right now. Missing runtime/model is not fatal. */
export function omniChannelAvailable(snap: OmniChannelSnapshot): boolean {
  if (snap.ready === true || snap.hostState === 'ready' || snap.hostState === 'launching') return true
  if (snap.hostState === 'missing_model' || snap.hostState === 'missing_runtime' || snap.hostState === 'failed') {
    return false
  }
  return snap.installed === true && snap.runtimeFound === true
}

/** Quick status check. Resolves false on missing files, bridge errors, or timeout. */
export async function probeOmniChannel(timeoutMs = OMNI_PROBE_MS): Promise<boolean> {
  let timer = 0
  const timeout = new Promise<boolean>(resolve => {
    timer = window.setTimeout(() => resolve(false), timeoutMs)
  })
  const probe = (async () => {
    try {
      const snap = await getOmniBridge().status()
      return omniChannelAvailable(snap)
    } catch {
      return false
    }
  })()
  try {
    return await Promise.race([probe, timeout])
  } finally {
    window.clearTimeout(timer)
  }
}

/**
 * MiniCPM-o 4.5 session. Capture stays in the renderer; the engine talks to
 * llama-omni-server on loopback. PCM is held until the user's turn ends so
 * the reply is one utterance, not a stream of 1s chops. Does not call
 * chat.start — the stage still sends the ASR transcript there so tools run.
 */
export async function startOmniCompanion(options: OmniCompanionOptions): Promise<OmniCompanionHandle> {
  let stopped = false
  let capture: PcmCaptureHandle | undefined
  let sessionId = ''
  let playing = false
  let appendChain: Promise<void> = Promise.resolve()
  let appendFails = 0
  let holding = true
  let held: string[] = []
  let caption = ''
  const wavQueue: string[] = []
  let audio: HTMLAudioElement | undefined
  let timelineEnd = 0
  const sources = new Set<AudioBufferSourceNode>()

  const paintCaption = (piece: string) => {
    const shown = stripTaskDonePhrases(piece)
    if (!shown || looksLikeOmniPersonaCaption(shown)) return
    caption = accumulateSpeakableCaption(caption, shown)
    if (caption) options.onText(caption)
  }

  const stopPlayback = () => {
    const wasPlaying = playing
    wavQueue.length = 0
    playing = false
    timelineEnd = 0
    for (const source of sources) {
      try {
        source.stop()
      } catch {
        /* already stopped */
      }
    }
    sources.clear()
    if (audio) {
      audio.pause()
      if (audio.src.startsWith('blob:')) URL.revokeObjectURL(audio.src)
      audio = undefined
    }
    // Only notify a true→false edge. CompanionStage's onSpeaking(false)
    // calls resumeListen → stopPlayback; an unconditional notify recurses
    // until Maximum call stack size exceeded (RootErrorBoundary on exit).
    if (wasPlaying) options.onSpeaking?.(false)
    capture?.setMuted(false)
  }

  const decodeWav = async (wav: string): Promise<AudioBuffer | null> => {
    const ctx = sharedTtsAudioContext()
    if (!ctx) return null
    const bytes = Uint8Array.from(atob(wav), c => c.charCodeAt(0))
    try {
      return await ctx.decodeAudioData(bytes.buffer.slice(0))
    } catch {
      return null
    }
  }

  const playViaElement = (wav: string) => {
    playing = true
    capture?.setMuted(true)
    options.onSpeaking?.(true)
    const bytes = Uint8Array.from(atob(wav), c => c.charCodeAt(0))
    const url = URL.createObjectURL(new Blob([bytes], { type: 'audio/wav' }))
    const el = new Audio(url)
    audio = el
    el.onended = () => {
      URL.revokeObjectURL(url)
      playing = false
      audio = undefined
      playNext()
    }
    el.onerror = () => {
      URL.revokeObjectURL(url)
      playing = false
      audio = undefined
      playNext()
    }
    void el.play().catch(() => {
      URL.revokeObjectURL(url)
      playing = false
      audio = undefined
      playNext()
    })
  }

  const playViaContext = async (wav: string): Promise<boolean> => {
    await unlockTtsAudio()
    const ctx = sharedTtsAudioContext()
    if (!ctx || ctx.state !== 'running') return false
    const buffer = await decodeWav(wav)
    if (!buffer) return false
    const source = ctx.createBufferSource()
    source.buffer = buffer
    source.connect(ctx.destination)
    const overlap = timelineEnd > ctx.currentTime + 0.08 ? PLAYBACK_OVERLAP_S : 0
    const startAt = timelineEnd > ctx.currentTime ? timelineEnd - overlap : ctx.currentTime
    timelineEnd = startAt + buffer.duration
    sources.add(source)
    playing = true
    capture?.setMuted(true)
    options.onSpeaking?.(true)
    source.onended = () => {
      sources.delete(source)
      if (sources.size === 0 && wavQueue.length === 0 && !audio) {
        playing = false
        options.onSpeaking?.(false)
        capture?.setMuted(false)
      }
    }
    try {
      source.start(startAt)
    } catch {
      sources.delete(source)
      return false
    }
    return true
  }

  const playNext = () => {
    if (stopped) return
    if (playing && audio) return
    const wav = wavQueue.shift()
    if (!wav) {
      if (sources.size === 0) {
        playing = false
        options.onSpeaking?.(false)
        capture?.setMuted(false)
      }
      return
    }
    void playViaContext(wav).then(ok => {
      if (stopped) return
      if (!ok) {
        playViaElement(wav)
        return
      }
      playNext()
    })
  }

  const enqueueWavs = (wavs: string[]) => {
    if (!wavs.length) return
    for (const wav of wavs) wavQueue.push(wav)
    playNext()
  }

  const appendFrames = (frames: string[]) => {
    if (stopped || !sessionId || !frames.length) return
    appendChain = appendChain.then(async () => {
      for (const pcm of frames) {
        if (stopped || !sessionId) return
        try {
          const turn = await withTimeout(
            getOmniBridge().append({ sessionId, pcm }),
            OMNI_APPEND_MS,
          )
          appendFails = 0
          if (stopped) return
          paintCaption(turn.text)
          enqueueWavs(turn.wavs)
        } catch (err) {
          appendFails += 1
          if (!stopped && appendFails >= OMNI_APPEND_FAILS_BEFORE_ERROR) {
            options.onError(err instanceof Error ? err.message : 'MiniCPM-o 推理失败')
            return
          }
        }
      }
    })
  }

  const resumeListen = () => {
    if (stopped) return
    holding = true
    caption = ''
    stopPlayback()
  }

  const commitUserAudio = () => {
    if (stopped || !sessionId) return false
    const frames = held
    held = []
    holding = false
    if (!frames.length) return false
    appendFrames(frames)
    return true
  }

  const stop = () => {
    if (stopped) return
    stopped = true
    held = []
    stopPlayback()
    void capture?.stop()
    capture = undefined
    if (sessionId) {
      void getOmniBridge().stop({ sessionId }).catch(() => {})
      sessionId = ''
    }
  }

  try {
    capture = await startPcmCapture({
      onFrame: frame => {
        if (stopped) return
        if (holding) {
          held.push(frame.base64)
          if (held.length > MAX_HELD_FRAMES) held.shift()
          return
        }
      },
      onError: error => {
        if (!stopped) options.onError(error.message)
      },
    })
  } catch (err) {
    stop()
    throw err
  }

  try {
    await waitOmniReady()
    const started = await getOmniBridge().start({ personaId: options.personaId })
    sessionId = started.sessionId
  } catch (err) {
    stop()
    throw err
  }

  return { stop, commitUserAudio, resumeListen }
}

async function waitOmniReady(): Promise<void> {
  const deadline = Date.now() + READY_MS
  while (Date.now() < deadline) {
    const snap = await getOmniBridge().ensure()
    if (snap.ready || snap.hostState === 'ready') return
    const blocked = omniStartBlock(snap)
    if (blocked) throw new Error(blocked)
    await sleep(POLL_MS)
  }
  throw new Error('MiniCPM-o 启动超时（模型加载约 10–60 秒）')
}

function withTimeout<T>(promise: Promise<T>, ms: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => reject(new Error('MiniCPM-o 推理超时')), ms)
    promise.then(
      value => {
        window.clearTimeout(timer)
        resolve(value)
      },
      err => {
        window.clearTimeout(timer)
        reject(err)
      },
    )
  })
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => window.setTimeout(resolve, ms))
}
