import React, { useEffect, useRef, useState } from 'react'
import { getOCRPackBridge, getOCRRoutingBridge, type OCRPackBridge, type OCRRoutingBridge, type OCRRunBridge, type ProviderBridge } from '../bridge/client'
import type { OcrPackGetResult, OcrRoutingGetResult } from '../generated/bridge'
import { OCRAdvancedPanel } from './OCRAdvancedPanel'
import { OCRModelPackCard } from './OCRModelPackCard'
import { OCRRapidPackCard } from './OCRRapidPackCard'
import { OCR_PACK_ID, formatCheckedAt, ocrUserError, windowsProbeSummary } from './ocrCopy'

export function OCRSettingsPanel({
  ocr = getOCRRoutingBridge(),
  packApi = getOCRPackBridge(),
  providers,
  runs,
}: {
  ocr?: OCRRoutingBridge
  packApi?: OCRPackBridge
  providers?: ProviderBridge
  runs?: OCRRunBridge
}): React.JSX.Element {
  const [routing, setRouting] = useState<OcrRoutingGetResult | null>(null)
  const [pack, setPack] = useState<OcrPackGetResult | null>(null)
  const [coreError, setCoreError] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const coreGeneration = useRef(0)

  const loadCore = async (refreshProbe = false) => {
    const epoch = ++coreGeneration.current
    const routingReq = ocr.get({ scopeKind: 'user', ...(refreshProbe ? { refreshProbe: true } : {}) })
    const packReq = packApi.get({ packId: OCR_PACK_ID })
    const [routingResult, packResult] = await Promise.allSettled([routingReq, packReq])
    if (epoch !== coreGeneration.current) return
    if (routingResult.status === 'fulfilled') {
      setRouting(routingResult.value)
      setCoreError('')
    } else {
      setCoreError(ocrUserError(routingResult.reason, 'OCR 状态载入失败'))
    }
    if (packResult.status === 'fulfilled') setPack(packResult.value)
  }

  useEffect(() => {
    void loadCore(false)
    return () => { coreGeneration.current++ }
  }, [ocr, packApi])

  const probe = routing?.windowsProbe
  const probeFailed = probe ? probe.state !== 'ready' : false

  return (
    <div className="ocr-settings">
      <p className="ocr-status" role="status">文字识别：自动</p>
      <p className="ocr-muted">已配置的视觉模型能用就先用（能力路由），然后走本机 RapidOCR，最后用 Windows OCR 兜底。没有绑定视觉模型时不会上传页面。</p>
      {coreError ? <p role="alert">{coreError}</p> : null}
      <section className="ocr-probe" aria-label="Windows OCR">
        <p>{windowsProbeSummary(probe?.state)}</p>
        {probe?.checkedAt ? <p className="ocr-muted">检查时间 {formatCheckedAt(probe.checkedAt)}</p> : null}
        {probeFailed ? <button type="button" onClick={() => void loadCore(true)}>重新检查</button> : null}
      </section>
      <OCRRapidPackCard ocr={ocr} />
      <OCRModelPackCard pack={pack} api={packApi} />
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
