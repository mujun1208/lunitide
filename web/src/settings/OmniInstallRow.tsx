import { useCallback, useEffect, useRef, useState, type JSX } from 'react'

import { getOmniBridge } from '../bridge/client'
import type { OmniInstallResult, OmniStatusResult } from '../bridge/client'

const POLL_MS = 700
const STATUS_POLL_MS = 2500
const PROBE_MS = 4_000

const gigabytes = (bytes: number) => `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`

export function OmniInstallRow({ onReady }: { onReady?: () => void } = {}): JSX.Element {
  const [status, setStatus] = useState<OmniStatusResult>()
  const [progress, setProgress] = useState<OmniInstallResult>()
  const [busy, setBusy] = useState(false)
  const [probeFailed, setProbeFailed] = useState(false)
  const timer = useRef(0)
  const alive = useRef(true)
  const inFlight = useRef(false)
  const statusRef = useRef<OmniStatusResult | undefined>(undefined)
  const readyNotified = useRef(false)

  const refresh = useCallback(() => {
    if (inFlight.current) return
    inFlight.current = true
    void getOmniBridge()
      .status()
      .then(result => {
        if (!alive.current) return
        statusRef.current = result
        setStatus(result)
        setProbeFailed(false)
      })
      .catch(() => {
        if (!alive.current) return
        setProbeFailed(true)
      })
      .finally(() => {
        inFlight.current = false
      })
  }, [])

  useEffect(() => {
    alive.current = true
    refresh()
    const poll = window.setInterval(refresh, STATUS_POLL_MS)
    const stuck = window.setTimeout(() => {
      if (!alive.current) return
      if (!statusRef.current) setProbeFailed(true)
    }, PROBE_MS)
    return () => {
      alive.current = false
      window.clearInterval(poll)
      window.clearTimeout(stuck)
      window.clearTimeout(timer.current)
    }
  }, [refresh])

  useEffect(() => {
    if (!status?.ready || !onReady || readyNotified.current) return
    readyNotified.current = true
    onReady()
  }, [onReady, status?.ready])

  const pump = useCallback(() => {
    void getOmniBridge()
      .install()
      .then(async result => {
        if (!alive.current) return
        setProgress(result)
        if (result.state === 'downloading') {
          timer.current = window.setTimeout(pump, POLL_MS)
          return
        }
        setBusy(false)
        refresh()
      })
      .catch(() => {
        if (!alive.current) return
        setBusy(false)
        setProgress({ state: 'failed', percent: 0, doneBytes: 0, totalBytes: 0, lastError: '下载启动失败' })
      })
  }, [refresh])

  const download = useCallback(() => {
    setBusy(true)
    setProgress(undefined)
    pump()
  }, [pump])

  const ready = status?.ready === true
  const installed = status?.installed === true
  const state = progress?.state
  const downloading = busy || state === 'downloading'
  const failed = state === 'failed' || (probeFailed && !status)
  const runtimeMissing = status?.supported === true && installed && !status.runtimeFound && !ready

  let desc = '正在检测 MiniCPM-o 4.5…'
  if (probeFailed && !status) desc = '检测 MiniCPM-o 失败。可重试；已下载的部分会保留。'
  else if (status) {
    if (ready) desc = '本机 MiniCPM-o 4.5 Q4 已就绪，全双工与音色克隆都在环回上推理，无需密钥。'
    else if (downloading && progress && progress.totalBytes > 0) {
      desc = `正在下载 MiniCPM-o 4.5：${progress.percent}% · ${gigabytes(progress.doneBytes)} / ${gigabytes(progress.totalBytes)}${progress.file ? ` · ${progress.file}` : ''}`
    } else if (downloading) desc = '正在准备下载…'
    else if (failed) desc = `下载失败：${progress?.lastError || '未知原因'}。可重试，已下载的部分会保留。`
    else if (runtimeMissing) desc = '此安装包未附带 MiniCPM-o 推理进程，本机推理进程未能展开。云端与本地模型仍可用。'
    else if (installed) desc = '模型已下载。进入月伴时会启动本机服务（首次加载约 10–60 秒）。'
    else if (!status.runtimeFound) desc = `本机 MiniCPM-o 4.5 Q4 未安装（约 ${gigabytes(status.downloadBytes)}）。此安装包未附带推理进程；点下载只会拉取权重。云端与本地模型仍可用。`
    else desc = `本机 MiniCPM-o 4.5 Q4 未安装（约 ${gigabytes(status.downloadBytes)}）。推理进程已随月汐安装；点下载后写入数据目录，安装包不内嵌权重。`
  }

  const label = ready
    ? '已就绪'
    : downloading
      ? '下载中…'
      : probeFailed && !status
        ? '重新检测'
        : failed
          ? '继续安装'
          : runtimeMissing
            ? '重试展开'
            : '下载安装'

  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">MiniCPM-o 4.5 Q4</div>
        <div className="setting-desc">{desc}</div>
      </div>
      <button
        type="button"
        className="btn"
        disabled={downloading || ready}
        onClick={probeFailed && !status ? refresh : download}
      >
        {label}
      </button>
    </div>
  )
}
