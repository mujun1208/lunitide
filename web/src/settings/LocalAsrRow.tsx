/**
 * Speech-to-text engine picker plus the local model's download.
 *
 * Kept out of SettingsPage because it owns a poll loop: the download is
 * detached on the engine side and reports progress by snapshot, so the row has
 * to ask rather than be told.
 */
import { useCallback, useEffect, useRef, useState } from 'react'

import type { VoiceInstallResult, VoiceStatusResult } from '../bridge/client'
import type { CompanionSettings, SpeechRecognizer } from '../session/companion/companionSettings'
import { installLocalAsr, localAsrStatus, selectLocalAsrModel, selectLocalAsrRefiner } from '../session/companion/localAsr'

const POLL_MS = 700

const megabytes = (bytes: number) => `${(bytes / 1024 / 1024).toFixed(0)} MB`

interface Props {
  companion: CompanionSettings
  save: (next: CompanionSettings) => void
}

export function LocalAsrRow({ companion, save }: Props): React.JSX.Element | null {
  const [status, setStatus] = useState<VoiceStatusResult>()
  const [progress, setProgress] = useState<VoiceInstallResult>()
  const [probed, setProbed] = useState(false)
  const [busy, setBusy] = useState(false)
  const timer = useRef(0)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    void localAsrStatus().then(result => {
      if (!alive.current) return
      setStatus(result)
      setProbed(true)
    })
    return () => {
      alive.current = false
      window.clearTimeout(timer.current)
    }
  }, [])

  const pump = useCallback((modelId?: string) => {
    void installLocalAsr(modelId)
      .then(async result => {
        if (!alive.current) return
        setProgress(result)
        if (result.state === 'downloading') {
          timer.current = window.setTimeout(() => pump(modelId), POLL_MS)
          return
        }
        setBusy(false)
        // Readiness is the recognizer's answer, not the downloader's: the
        // files can all be present and the runtime still refuse to start.
        const next = await localAsrStatus()
        if (alive.current) setStatus(next)
      })
      .catch(() => {
        if (!alive.current) return
        setBusy(false)
        setProgress({ state: 'failed', percent: 0, doneBytes: 0, totalBytes: 0, file: '', lastError: '下载启动失败' })
      })
  }, [])

  const download = useCallback(() => {
    setBusy(true)
    setProgress(undefined)
    pump()
  }, [pump])

  const chooseRefiner = useCallback((modelId: string) => {
    void selectLocalAsrRefiner(modelId)
      .then(async () => {
        const next = await localAsrStatus()
        if (!alive.current) return
        if (next) setStatus(next)
        const chosen = next?.refiners.find(item => item.id === modelId)
        if (chosen && !chosen.installed) {
          setBusy(true)
          setProgress(undefined)
          pump(modelId)
        }
      })
      .catch(() => {
        /* The row keeps showing the model the engine reports. */
      })
  }, [pump])

  const chooseModel = useCallback((modelId: string) => {
    // Told to the engine, not just remembered: it holds the running
    // recognizer, and a preference nothing acts on is worse than no
    // preference at all.
    void selectLocalAsrModel(modelId)
      .then(() => localAsrStatus())
      .then(next => {
        if (alive.current && next) setStatus(next)
      })
      .catch(() => {
        /* The row keeps showing the model the engine reports. */
      })
  }, [])

  // A build without the recognizer must not advertise it. Until the probe
  // answers, the row stays hidden rather than flashing a control that is about
  // to disappear.
  if (!probed || !status?.supported) return null

  const ready = status.ready
  const state = progress?.state
  const downloading = busy || state === 'downloading'
  const failed = state === 'failed'

  const desc = ready
    ? `${status.modelTitle || status.modelId} 已就绪，语音不出本机，断网也能用。`
    : downloading
      ? progress && progress.totalBytes > 0
        ? `正在下载 ${status.modelTitle || status.modelId}：${progress.percent}% · ${megabytes(progress.doneBytes)} / ${megabytes(progress.totalBytes)}${progress.file ? ` · ${progress.file}` : ''}`
        : '正在准备下载…'
      : failed
        ? `下载失败：${progress?.lastError || '未知原因'}。可重试，已下载的部分会保留。`
        : `本机识别模型未安装（约 ${megabytes(status.downloadBytes)}，含引擎与模型）。未安装时实际走系统识别（WebView 语音服务，音频会离开本机），不是本机识别。`

  return (
    <>
      <div className="setting-row">
        <div>
          <div className="setting-label">语音识别引擎</div>
          <div className="setting-desc">
            自动＝装好本机模型就用本机，否则用系统识别；本机识别不联网、不上传音频，系统识别依赖 WebView 语音服务。
          </div>
        </div>
        <select
          className="setting-input"
          aria-label="语音识别引擎"
          value={companion.recognizer}
          onChange={event => save({ ...companion, recognizer: event.target.value as SpeechRecognizer })}
        >
          <option value="auto">自动（推荐）</option>
          <option value="local">仅本机模型</option>
          <option value="cloud">仅系统识别</option>
        </select>
      </div>
      <div className="setting-row">
        <div>
          <div className="setting-label">本机识别模型</div>
          <div className="setting-desc">{desc}</div>
        </div>
        <button
          type="button"
          className="btn"
          disabled={downloading || ready}
          onClick={download}
        >
          {ready ? '已安装' : downloading ? '下载中…' : failed ? '重试下载' : '下载安装'}
        </button>
      </div>
      {status.refiners.length > 1 && (
        <div className="setting-row">
          <div>
            <div className="setting-label">听写模型</div>
            <div className="setting-desc">
              说完一句后重新识别、真正送进对话的那套。当前这套保持默认。选另一套时单独下载，不影响已经装好的模型。以后换模型也在这里换。
            </div>
          </div>
          <select
            className="setting-input"
            aria-label="听写模型"
            value={status.refinerId}
            disabled={downloading}
            onChange={event => chooseRefiner(event.target.value)}
          >
            {status.refiners.map(model => (
              <option key={model.id} value={model.id}>
                {model.title}（{megabytes(model.sizeBytes)}
                {model.installed ? ' · 已下载' : ' · 需下载'}）
              </option>
            ))}
          </select>
        </div>
      )}
      {status.models.length > 1 && (
        <div className="setting-row">
          <div>
            <div className="setting-label">字幕模型</div>
            <div className="setting-desc">
              说话时实时出字的那个。最终送给模型的文本由离线模型重新识别，所以这里选的是字幕的快慢与体积，不是最终准确度。
            </div>
          </div>
          <select
            className="setting-input"
            aria-label="字幕模型"
            value={status.modelId}
            disabled={downloading}
            onChange={event => chooseModel(event.target.value)}
          >
            {status.models.map(model => (
              <option key={model.id} value={model.id}>
                {model.title}（{megabytes(model.sizeBytes)}
                {model.installed ? ' · 已下载' : ' · 需下载'}）
              </option>
            ))}
          </select>
        </div>
      )}
    </>
  )
}
