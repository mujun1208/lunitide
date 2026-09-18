import { useEffect, useState } from 'react'
import type { OCRInstallSnapshot, OCRRoutingBridge } from '../bridge/client'
import { ocrUserError } from './ocrCopy'

const POLL_MS = 750

export function OCRRapidPackCard({
  ocr,
}: {
  ocr: OCRRoutingBridge
}): React.JSX.Element {
  const [snap, setSnap] = useState<OCRInstallSnapshot | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    void ocr.install({ probe: true }).then(got => {
      if (!cancelled) setSnap(got)
    }).catch(() => {})
    return () => { cancelled = true }
  }, [ocr])

  const start = async () => {
    setBusy(true)
    setError('')
    try {
      let got = await ocr.install({})
      setSnap(got)
      while (got.state === 'downloading') {
        await new Promise(resolve => setTimeout(resolve, POLL_MS))
        got = await ocr.install({})
        setSnap(got)
      }
      if (got.state === 'failed') {
        setError(got.lastError || 'RapidOCR 安装失败')
      }
    } catch (err) {
      setError(ocrUserError(err, 'RapidOCR 安装失败'))
    } finally {
      setBusy(false)
    }
  }

  const ready = snap?.state === 'ready'
  const downloading = busy || snap?.state === 'downloading'
  const label = ready ? '已安装 RapidOCR' : downloading ? '正在安装 RapidOCR' : '安装 RapidOCR'

  return (
    <section className="ocr-rapid" aria-labelledby="ocr-rapid-title">
      <h2 id="ocr-rapid-title">本机 RapidOCR</h2>
      <p className="ocr-muted">已配置的视觉模型优先；云端失败或未配置时走 RapidOCR，最后用 Windows OCR 兜底。</p>
      <button type="button" disabled={downloading || ready} onClick={() => void start()}>
        {label}
      </button>
      {snap?.state === 'downloading' ? <p>下载 {snap.percent}%</p> : null}
      {error ? <p role="alert">{error}</p> : null}
    </section>
  )
}
