/**
 * On-demand download of the local GPT-SoVITS engine.
 *
 * The desktop package ships nothing large; the engine + models are pulled
 * into %LOCALAPPDATA%\Lunitide\gpt-sovits on demand and verified by digest,
 * after which the launcher discovers them and the 50 preset voices work
 * offline. Like LocalAsrRow this owns a poll loop, because the transfer is
 * detached on the engine side and reports progress by snapshot.
 *
 * When no pack manifest is configured the engine reports "not_configured";
 * the row then explains how to point Lunitide at a hosted pack instead of
 * offering a download that has no source.
 */
import { useCallback, useEffect, useRef, useState } from 'react'

import { getTtsBridge, type TtsInstallRefEngineResult } from '../bridge/client'

const POLL_MS = 800

const megabytes = (bytes: number): string => `${(bytes / 1024 / 1024).toFixed(0)} MB`

export function RefEngineInstallRow(): React.JSX.Element | null {
  const [snapshot, setSnapshot] = useState<TtsInstallRefEngineResult>()
  const [probed, setProbed] = useState(false)
  const [busy, setBusy] = useState(false)
  const timer = useRef(0)
  const alive = useRef(true)

  const poll = useCallback((keepPumping: boolean) => {
    void getTtsBridge()
      .installRefEngine()
      .then(result => {
        if (!alive.current) return
        setSnapshot(result)
        setProbed(true)
        if (result.state === 'downloading') {
          timer.current = window.setTimeout(() => poll(true), POLL_MS)
          return
        }
        setBusy(false)
      })
      .catch(() => {
        if (!alive.current) return
        setBusy(false)
        setProbed(true)
        if (keepPumping) setSnapshot({ state: 'failed', percent: 0, doneBytes: 0, totalBytes: 0, lastError: '下载启动失败' })
      })
  }, [])

  useEffect(() => {
    alive.current = true
    // Probe once without starting a download: begin() is idempotent and only
    // launches when a pack is configured and not yet installed.
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
  // may immediately disappear (already installed / launcher present).
  if (!probed || !snapshot) return null
  // Engine already present (installed pack or a portable/legacy launcher):
  // nothing to offer here; the service-address row and 试听 cover the rest.
  if (snapshot.state === 'ready') return null

  const downloading = busy || snapshot.state === 'downloading'
  const notConfigured = snapshot.state === 'not_configured'
  const failed = snapshot.state === 'failed'

  const desc = notConfigured
    ? '本机未检测到语音引擎，也未配置可下载的引擎包。把一个 tar.bz2 引擎包托管到可访问地址后，通过环境变量 LUNITIDE_REF_ENGINE_PACK_URL（配 _SHA256/_BYTES）或在 %LOCALAPPDATA%\\Lunitide\\gpt-sovits\\pack.manifest.json 填入即可自动下载安装。'
    : downloading
      ? snapshot.totalBytes > 0
        ? `正在下载本地语音引擎：${snapshot.percent}% · ${megabytes(snapshot.doneBytes)} / ${megabytes(snapshot.totalBytes)}${snapshot.file ? ` · ${snapshot.file}` : ''}`
        : '正在准备下载…'
      : failed
        ? `下载失败：${snapshot.lastError || '未知原因'}。可重试，已下载的部分会保留。`
        : '本机语音引擎未安装。点击下载后自动拉取并校验，装好后 50 种音色可离线试听。'

  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">本地语音引擎</div>
        <div className="setting-desc">{desc}</div>
      </div>
      <button type="button" disabled={downloading || notConfigured} onClick={download} aria-label="下载并安装本地语音引擎">
        {downloading ? '下载中…' : failed ? '重试下载' : notConfigured ? '未配置' : '下载安装'}
      </button>
    </div>
  )
}
