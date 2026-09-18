import React, { useCallback, useEffect, useRef, useState } from 'react'
import { getProviderBridge, type OCRRunBridge, type ProviderBridge } from '../bridge/client'
import type { OcrRoutingGetResult, ProviderDTO } from '../generated/bridge'
import { OCRRecentRuns } from './OCRRecentRuns'
import { isLegacyUnwired, ocrUserError } from './ocrCopy'

export function OCRAdvancedPanel({
  open,
  onToggle,
  routing,
  providers,
  runs,
}: {
  open: boolean
  onToggle: () => void
  routing: OcrRoutingGetResult | null
  providers?: ProviderBridge
  runs?: OCRRunBridge
}): React.JSX.Element {
  const providerApi = providers ?? getProviderBridge()
  const [items, setItems] = useState<ProviderDTO[] | null>(null)
  const [providerError, setProviderError] = useState('')
  const loaded = useRef(false)
  const generation = useRef(0)

  const loadProviders = useCallback(async (force = false) => {
    if (!force && loaded.current) return
    const epoch = ++generation.current
    try {
      const listed = await providerApi.list()
      if (epoch !== generation.current) return
      setItems(listed.items)
      setProviderError('')
      loaded.current = true
    } catch (err) {
      if (epoch !== generation.current) return
      setProviderError(ocrUserError(err, '云端识别目录载入失败'))
    }
  }, [providerApi])

  useEffect(() => {
    if (open) void loadProviders(false)
  }, [open, loadProviders])

  const languages = routing?.windowsProbe.languages ?? []
  const policy = routing?.policy
  const legacy = routing?.legacy

  return (
    <div className="ocr-advanced">
      <button type="button" className="ocr-disclose" aria-expanded={open} onClick={onToggle}>
        高级
      </button>
      {open ? (
        <div className="ocr-advanced-body">
          <section aria-labelledby="ocr-lang-title">
            <h4 id="ocr-lang-title">已探测语言</h4>
            {languages.length ? <p>{languages.join('、')}</p> : <p className="ocr-muted">无语言列表</p>}
          </section>
          <section aria-labelledby="ocr-policy-title">
            <h4 id="ocr-policy-title">版本与回退</h4>
            <p>模式：自动</p>
            <p>复杂文档引擎：当前不可用</p>
            <p>回退顺序：视觉模型（能力路由） → RapidOCR → Windows OCR</p>
            <p>上传云端：仅在能力路由绑定了视觉模型时；未绑定不会上传页面</p>
          </section>
          <section aria-labelledby="ocr-legacy-title">
            <h4 id="ocr-legacy-title">本机 PP-OCR</h4>
            <p>{isLegacyUnwired(legacy) ? '已登记，尚未接入' : (policy?.fallbackOrder ?? []).includes('ppocr') ? 'RapidOCR 已纳入识别顺序' : '未登记本机 PP-OCR'}</p>
          </section>
          <section aria-labelledby="ocr-provider-title">
            <h4 id="ocr-provider-title">云端识别目录</h4>
            {providerError ? <p role="alert">{providerError}</p> : null}
            {providerError ? <button type="button" onClick={() => void loadProviders(true)}>重试供应商列表</button> : null}
            {items && !providerError ? (
              items.length ? (
                <ul>{items.map(item => <li key={item.id}>{item.name}</li>)}</ul>
              ) : <p className="ocr-muted">没有可用的云端识别模型</p>
            ) : null}
          </section>
          <OCRRecentRuns api={runs} />
        </div>
      ) : null}
    </div>
  )
}
