/**
 * On-demand download of the bundled offline ONNX voice (sherpa-onnx + Kokoro).
 *
 * Nothing large ships in the desktop package; the ~21 MB runtime and ~333 MB
 * Kokoro model are pulled into %LOCALAPPDATA%\Lunitide on demand and verified
 * by digest, after which the local path speaks fully offline with no Python,
 * no server and no reference audio. Like RefEngineInstallRow this owns a poll
 * loop because the transfer is detached on the engine side.
 *
 * The probe is read-only: installOnnxEngine() (no args) only reports state, so
 * merely opening settings never triggers the 350 MB download. The button sends
 * installOnnxEngine({ start: true }) to actually begin it.
 */
import { useCallback, useEffect, useRef, useState } from 'react'

import { getTtsBridge, type TtsInstallOnnxEngineResult } from '../bridge/client'

const POLL_MS = 800

const megabytes = (bytes: number): string => `${(bytes / 1024 / 1024).toFixed(0)} MB`

export function OnnxEngineInstallRow(): React.JSX.Element | null {
  const [snapshot, setSnapshot] = useState<TtsInstallOnnxEngineResult>()
  const [probed, setProbed] = useState(false)
  const [busy, setBusy] = useState(false)
  const timer = useRef(0)
  const alive = useRef(true)

  const poll = useCallback((start: boolean) => {
    void getTtsBridge()
      .installOnnxEngine(start ? { start: true } : {})
      .then(result => {
        if (!alive.current) return
        setSnapshot(result)
        setProbed(true)
        if (result.state === 'downloading') {
          timer.current = window.setTimeout(() => poll(false), POLL_MS)
          return
        }
        setBusy(false)
      })
      .catch(() => {
        if (!alive.current) return
        setBusy(false)
        setProbed(true)
      })
  }, [])

  useEffect(() => {
    alive.current = true
    // Read-only probe on mount: never starts a download.
    poll(false)
    return () => {
      alive.current = false
      window.clearTimeout(timer.current)
    }
  }, [poll])

  const download = useCallback(() => {
    setBusy(true)
    poll(true)
  }, [poll])

  // Until the probe answers, stay hidden rather than flashing a control that
  // may immediately disappear (already installed).
  if (!probed || !snapshot) return null
  // Bundles already present: nothing to offer; the picker + 试听 cover the rest.
  if (snapshot.state === 'ready') return null

  const downloading = busy || snapshot.state === 'downloading'
  const failed = snapshot.state === 'failed'

  const desc = downloading
    ? snapshot.totalBytes > 0
      ? `正在下载本地语音（Kokoro）：${snapshot.percent}% · ${megabytes(snapshot.doneBytes)} / ${megabytes(snapshot.totalBytes)}`
      : '正在准备下载…'
    : failed
      ? `下载失败：${snapshot.lastError || '未知原因'}。可重试，已下载的部分会保留。`
      : '本地语音（Kokoro，约 354 MB）尚未安装。点击下载后自动拉取并校验，装好后 8 个中文音色可完全离线朗读。'

  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">本地语音引擎（Kokoro）</div>
        <div className="setting-desc">{desc}</div>
      </div>
      <button type="button" disabled={downloading} onClick={download} aria-label="下载并安装本地语音引擎 Kokoro">
        {downloading ? '下载中…' : failed ? '重试下载' : '下载安装'}
      </button>
    </div>
  )
}
