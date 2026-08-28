import { startPcmCapture, type PcmCaptureHandle } from '../companion/pcmCapture'
import { getOmniBridge } from '../../bridge/client'

export interface OmniCompanionHandle {
  stop: () => void
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
 * Full-duplex MiniCPM-o 4.5 session. Capture stays in the renderer; the
 * engine talks to llama-omni-server on loopback. Does not call chat.start
 * or the companion TTS catalogue.
 */
export async function startOmniCompanion(options: OmniCompanionOptions): Promise<OmniCompanionHandle> {
  let stopped = false
  let capture: PcmCaptureHandle | undefined
  let sessionId = ''
  let playing = false
  let appendChain: Promise<void> = Promise.resolve()
  let appendFails = 0
  const queue: string[] = []
  let audio: HTMLAudioElement | undefined

  const stopPlayback = () => {
    queue.length = 0
    playing = false
    audio?.pause()
    if (audio?.src.startsWith('blob:')) URL.revokeObjectURL(audio.src)
    audio = undefined
    options.onSpeaking?.(false)
    capture?.setMuted(false)
  }

  const playNext = () => {
    if (stopped || playing) return
    const wav = queue.shift()
    if (!wav) {
      options.onSpeaking?.(false)
      capture?.setMuted(false)
      return
    }
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

  const stop = () => {
    if (stopped) return
    stopped = true
    stopPlayback()
    void capture?.stop()
    capture = undefined
    if (sessionId) {
      void getOmniBridge().stop({ sessionId }).catch(() => {})
      sessionId = ''
    }
  }

  await waitOmniReady()
  const started = await getOmniBridge().start({ personaId: options.personaId })
  sessionId = started.sessionId

  try {
    capture = await startPcmCapture({
      onFrame: frame => {
        if (stopped || !sessionId) return
        appendChain = appendChain.then(async () => {
          if (stopped || !sessionId) return
          try {
            const turn = await withTimeout(
              getOmniBridge().append({ sessionId, pcm: frame.base64 }),
              OMNI_APPEND_MS,
            )
            appendFails = 0
            if (stopped) return
            const text = turn.text.trim()
            if (text) options.onText(text)
            if (turn.listening) stopPlayback()
            for (const wav of turn.wavs) queue.push(wav)
            playNext()
          } catch (err) {
            appendFails += 1
            if (!stopped && appendFails >= OMNI_APPEND_FAILS_BEFORE_ERROR) {
              options.onError(err instanceof Error ? err.message : 'MiniCPM-o 推理失败')
            }
          }
        })
      },
      onError: error => {
        if (!stopped) options.onError(error.message)
      },
    })
  } catch (err) {
    stop()
    throw err
  }

  return { stop }
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
