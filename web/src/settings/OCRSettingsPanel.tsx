import React, { useEffect, useRef, useState } from 'react'
import { getOCRRoutingBridge, type OCRPackBridge, type OCRRoutingBridge, type OCRRunBridge, type ProviderBridge } from '../bridge/client'
import type { OcrRoutingGetResult } from '../generated/bridge'
import { OCRAdvancedPanel } from './OCRAdvancedPanel'
import { OCRRapidPackCard } from './OCRRapidPackCard'
import { formatCheckedAt, ocrUserError, windowsProbeCanRepair, windowsProbeHint, windowsProbeSummary } from './ocrCopy'

export function OCRSettingsPanel({
  ocr = getOCRRoutingBridge(),
  packApi: _packApi,
  providers,
  runs,
}: {
  ocr?: OCRRoutingBridge
  packApi?: OCRPackBridge
  providers?: ProviderBridge
  runs?: OCRRunBridge
}): React.JSX.Element {
  const [routing, setRouting] = useState<OcrRoutingGetResult | null>(null)
  const [coreError, setCoreError] = useState('')
  const [busy, setBusy] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const coreGeneration = useRef(0)

  const loadCore = async (opts: { refreshProbe?: boolean; repairWindows?: boolean } = {}) => {
    const epoch = ++coreGeneration.current
    const repairing = opts.repairWindows === true
    setBusy(repairing ? 'repair' : opts.refreshProbe ? 'refresh' : '')
    const routingResult = await ocr.get({
      scopeKind: 'user',
      ...(opts.refreshProbe || repairing ? { refreshProbe: true } : {}),
      ...(repairing ? { repairWindows: true } : {}),
    }).then(value => ({ status: 'fulfilled' as const, value }), reason => ({ status: 'rejected' as const, reason }))
    if (epoch !== coreGeneration.current) return
    setBusy('')
    if (routingResult.status === 'fulfilled') {
      setRouting(routingResult.value)
      setCoreError('')
    } else {
      setCoreError(ocrUserError(routingResult.reason, repairing ? 'Windows OCR 修复失败' : 'OCR 状态载入失败'))
    }
  }

  useEffect(() => {
    void loadCore()
    return () => { coreGeneration.current++ }
  }, [ocr])

  const probe = routing?.windowsProbe
  const probeFailed = probe ? probe.state !== 'ready' : false
  const hint = windowsProbeHint(probe?.state)
  const canRepair = windowsProbeCanRepair(probe?.state)

  return (
    <div className="ocr-settings">
      <p className="ocr-status" role="status">文字识别：自动</p>
      <p className="ocr-muted">已配置的视觉模型能用就先用（能力路由），然后走本机 RapidOCR，最后用 Windows OCR 兜底。没有绑定视觉模型时不会上传页面。</p>
      {coreError ? <p role="alert">{coreError}</p> : null}
      <section className="ocr-probe" aria-label="Windows OCR">
        <p>{windowsProbeSummary(probe?.state)}</p>
        {hint ? <p className="ocr-muted">{hint}</p> : null}
        {probe?.checkedAt ? <p className="ocr-muted">检查时间 {formatCheckedAt(probe.checkedAt)}</p> : null}
        {probeFailed ? (
          <div className="ocr-probe-actions">
            <button type="button" disabled={busy !== ''} onClick={() => void loadCore({ refreshProbe: true })}>
              {busy === 'refresh' ? '正在检查…' : '重新检查'}
            </button>
            {canRepair ? (
              <button type="button" disabled={busy !== ''} onClick={() => void loadCore({ repairWindows: true })}>
                {busy === 'repair' ? '正在修复…' : '一键修复'}
              </button>
            ) : null}
          </div>
        ) : null}
      </section>
      <OCRRapidPackCard ocr={ocr} />
      <OCRAdvancedPanel
        open={advancedOpen}
        onToggle={() => setAdvancedOpen(value => !value)}
        routing={routing}
        providers={providers}
        runs={runs}
      />
    </div>
  )
}
