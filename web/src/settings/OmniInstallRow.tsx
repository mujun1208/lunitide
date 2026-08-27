import { useCallback, useEffect, useRef, useState, type JSX } from 'react'

import { getOmniBridge } from '../bridge/client'
import type { OmniInstallResult, OmniStatusResult } from '../bridge/client'

const POLL_MS = 700
const STATUS_POLL_MS = 2500

const gigabytes = (bytes: number) => `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`

export function OmniInstallRow(): JSX.Element {
  const [status, setStatus] = useState<OmniStatusResult>()
  const [progress, setProgress] = useState<OmniInstallResult>()
  const [busy, setBusy] = useState(false)
  const [probeFailed, setProbeFailed] = useState(false)
  const timer = useRef(0)
  const alive = useRef(true)

  const refresh = useCallback(() => {
    void getOmniBridge()
      .status()
      .then(result => {
        if (!alive.current) return
        setStatus(result)
        setProbeFailed(false)
      })
      .catch(() => {
        if (!alive.current) return
        setProbeFailed(true)
      })
  }, [])

  useEffect(() => {
    alive.current = true
    refresh()
    const poll = window.setInterval(refresh, STATUS_POLL_MS)
    return () => {
      alive.current = false
      window.clearInterval(poll)
      window.clearTimeout(timer.current)
    }
  }, [refresh])

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
    else if (runtimeMissing) desc = '模型已下载。还差本机推理进程 llama-omni-server（约 0.5 GB），点继续安装即可。'
    else if (installed) desc = '模型已下载。进入月伴时会启动本机服务（首次加载约 10–60 秒）。'
    else desc = `本机 MiniCPM-o 4.5 Q4 未安装（约 ${gigabytes(status.downloadBytes)}，含推理进程）。安装包不内嵌权重，点下载后写入月汐数据目录。`
  }

  const label = ready
    ? '已就绪'
    : downloading
      ? '下载中…'
      : probeFailed && !status
        ? '重新检测'
        : failed || runtimeMissing
          ? '继续安装'
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
