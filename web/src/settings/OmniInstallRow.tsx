import { useCallback, useEffect, useRef, useState, type JSX } from 'react'

import { getOmniBridge } from '../bridge/client'
import type { OmniInstallResult, OmniStatusResult } from '../bridge/client'

const POLL_MS = 700

const gigabytes = (bytes: number) => `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`

export function OmniInstallRow(): JSX.Element {
  const [status, setStatus] = useState<OmniStatusResult>()
  const [progress, setProgress] = useState<OmniInstallResult>()
  const [busy, setBusy] = useState(false)
  const timer = useRef(0)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    void getOmniBridge()
      .status()
      .then(result => {
        if (alive.current) setStatus(result)
      })
      .catch(() => {
        if (alive.current) setStatus(undefined)
      })
    return () => {
      alive.current = false
      window.clearTimeout(timer.current)
    }
  }, [])

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
        const next = await getOmniBridge().status()
        if (alive.current) setStatus(next)
      })
      .catch(() => {
        if (!alive.current) return
        setBusy(false)
        setProgress({ state: 'failed', percent: 0, doneBytes: 0, totalBytes: 0, lastError: '下载启动失败' })
      })
  }, [])

  const download = useCallback(() => {
    setBusy(true)
    setProgress(undefined)
    pump()
  }, [pump])

  const ready = status?.ready === true
  const installed = status?.installed === true
  const state = progress?.state
  const downloading = busy || state === 'downloading'
  const failed = state === 'failed'
  const runtimeMissing = status?.supported === true && installed && !status.runtimeFound && !ready

  let desc = '正在检测 MiniCPM-o 4.5…'
  if (status) {
    if (ready) desc = '本机 MiniCPM-o 4.5 Q4 已就绪，全双工与音色克隆都在环回上推理，无需密钥。'
    else if (downloading && progress && progress.totalBytes > 0) {
      desc = `正在下载 MiniCPM-o 4.5 Q4：${progress.percent}% · ${gigabytes(progress.doneBytes)} / ${gigabytes(progress.totalBytes)}${progress.file ? ` · ${progress.file}` : ''}`
    } else if (downloading) desc = '正在准备下载…'
    else if (failed) desc = `下载失败：${progress?.lastError || '未知原因'}。可重试，已下载的部分会保留。`
    else if (runtimeMissing) desc = '模型已下载，但未找到 llama-omni-server。请把可执行文件放到月汐数据目录 omni/runtime/。'
    else if (installed) desc = '模型已下载。进入月伴时会启动本机服务（首次加载约 10–60 秒）。'
    else desc = `本机 MiniCPM-o 4.5 Q4 未安装（约 ${gigabytes(status.downloadBytes)}）。安装包不内嵌权重，点下载后写入月汐数据目录。`
  }

  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">MiniCPM-o 4.5 Q4</div>
        <div className="setting-desc">{desc}</div>
      </div>
      <button type="button" className="btn" disabled={downloading || installed} onClick={download}>
        {installed ? '已安装' : downloading ? '下载中…' : failed ? '重试下载' : '下载安装'}
      </button>
    </div>
  )
}
