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
        appendChain = appendChain
          .then(async () => {
            if (stopped || !sessionId) return
            const turn = await getOmniBridge().append({ sessionId, pcm: frame.base64 })
            if (stopped) return
            const text = turn.text.trim()
            if (text) options.onText(text)
            if (turn.listening) stopPlayback()
            for (const wav of turn.wavs) queue.push(wav)
            playNext()
          })
          .catch(err => {
            if (!stopped) options.onError(err instanceof Error ? err.message : 'MiniCPM-o 推理失败')
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
    if (snap.hostState === 'missing_model') {
      throw new Error('请先在设置里下载 MiniCPM-o 4.5 Q4')
    }
    if (snap.hostState === 'missing_runtime') {
      throw new Error('未找到 llama-omni-server，请放到月汐数据目录 omni/runtime/')
    }
    if (snap.hostState === 'failed') {
      throw new Error(snap.lastError || 'MiniCPM-o 启动失败')
    }
    await sleep(POLL_MS)
  }
  throw new Error('MiniCPM-o 启动超时（模型加载约 10–60 秒）')
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => window.setTimeout(resolve, ms))
}
