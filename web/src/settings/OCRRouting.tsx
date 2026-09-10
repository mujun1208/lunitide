import React, { useEffect, useRef, useState } from 'react'
import { createMutationAttempt, getOCRRoutingBridge, getProviderBridge, type OCRRoutingBridge, type ProviderBridge } from '../bridge/client'
import type { ProviderDTO } from '../generated/bridge'
import { modelKind } from '../provider/modelKind'

type Option = { value: string; label: string; providerId: string; modelId: string }

function ocrUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

function ocrFailureClassLabel(value: string): string {
  switch (value) {
    case 'auth': return '鉴权失败'
    case 'rate': return '限流'
    case 'failed': return '识别失败'
    case 'unavailable': return '服务不可用'
    default: return '识别异常'
  }
}

function ocrFailureOperationLabel(value: string): string {
  switch (value) {
    case 'image-ocr': return '图片识别'
    case 'pdf-ocr': return 'PDF识别'
    default: return '识别'
  }
}

function visionOptions(providers: ProviderDTO[]): Option[] {
  const out: Option[] = []
  for (const p of providers) {
    for (const m of p.models) {
      const kind = modelKind(m)
      if (kind !== 'vision' && !(kind === 'llm' && m.supportsVision)) continue
      out.push({ value: `${p.id}\u0000${m.modelId}`, label: `${p.name} / ${m.displayName || m.modelId}`, providerId: p.id, modelId: m.modelId })
    }
  }
  return out
}

export function OCRRouting({ providers, ocr }: { providers?: ProviderBridge; ocr?: OCRRoutingBridge }): React.JSX.Element {
  const providerApi = providers ?? getProviderBridge()
  const ocrApi = ocr ?? getOCRRoutingBridge()
  const [items, setItems] = useState<ProviderDTO[]>([])
  const [providerId, setProviderId] = useState('')
  const [modelId, setModelId] = useState('')
  const [preferProvider, setPreferProvider] = useState(true)
  const [revision, setRevision] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [lastFailure, setLastFailure] = useState<{class: string; operation: string; until: string}>()
  const [localReady, setLocalReady] = useState<{pdf: boolean; image: boolean; backend: string}>()
  const saving = useRef(false)
  const generation = useRef(0)

  const load = async () => {
    const epoch = ++generation.current
    try {
      const [listed, got] = await Promise.all([providerApi.list(), ocrApi.get()])
      if (epoch !== generation.current) return
      setItems(listed.items)
      setProviderId(got.providerId ?? '')
      setModelId(got.modelId ?? '')
      setPreferProvider(got.preferProvider)
      setRevision(got.revision)
      setLastFailure(got.lastFailure)
      setLocalReady(got.localReady)
      setError('')
    } catch (e) {
      if (epoch === generation.current) setError(ocrUserError(e, 'OCR 路由载入失败'))
    }
  }

  useEffect(() => { void load(); return () => { generation.current++ } }, [providerApi, ocrApi])

  const save = async () => {
    if (saving.current || !revision) return
    saving.current = true
    const epoch = generation.current
    setBusy(true)
    setNotice('')
    setError('')
    try {
      const payload = {
        expectedRevision: revision,
        preferProvider,
        ...(providerId && modelId ? { providerId, modelId } : {}),
      }
      const saved = await ocrApi.set(payload, { attempt: createMutationAttempt('ocr.routing.set', payload) })
      if (epoch !== generation.current) return
      setRevision(saved.revision)
      setProviderId(saved.providerId ?? '')
      setModelId(saved.modelId ?? '')
      setPreferProvider(saved.preferProvider)
      setLastFailure(saved.lastFailure)
      setLocalReady(saved.localReady)
      setNotice('OCR 路由已保存，下一次识别生效')
    } catch (e) {
      if (epoch === generation.current) setError(ocrUserError(e, 'OCR 路由保存失败'))
    } finally {
      saving.current = false
      if (epoch === generation.current) setBusy(false)
    }
  }

  const opts = visionOptions(items)
  const value = providerId && modelId ? `${providerId}\u0000${modelId}` : ''

  return (
    <section className="capability-routing" aria-label="OCR 路由">
      <h3>OCR 路由</h3>
      <p className="setting-desc">已配置供应商时优先走云端识别；失败、未配置或停用后自动本机兜底。图片与扫描 PDF 共用本机 Windows OCR（需系统语言包），不是随包 PP-OCR。这不是第七个能力角色。保存后从下一次识别开始生效。</p>
      <label className="capability-role-row">
        <span>OCR 模型</span>
        <select
          aria-label="OCR 模型"
          value={value}
          disabled={busy}
          onChange={e => {
            const hit = opts.find(o => o.value === e.target.value)
            setProviderId(hit?.providerId ?? '')
            setModelId(hit?.modelId ?? '')
          }}
        >
          <option value="">未绑定（仅本机兜底）</option>
          {opts.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
        <small>空=不调用供应商，直接本机</small>
      </label>
      <label className="default">
        <input type="checkbox" checked={preferProvider} disabled={busy} onChange={e => setPreferProvider(e.target.checked)} />
        供应商可用时优先云端
      </label>
      {localReady && <p className="setting-desc">本机后端 {localReady.backend}{localReady.backend === 'windows-ocr' ? '（需系统语言包，不是随包 PP-OCR）' : '（当前平台未封装本地识别）'}；PDF {localReady.pdf ? '已封装' : '不可用'}，图片 {localReady.image ? '已封装' : '不可用'}</p>}
      {lastFailure && <p role="status">最近供应商失败：{ocrFailureClassLabel(lastFailure.class)} / {ocrFailureOperationLabel(lastFailure.operation)}，冷却至 {lastFailure.until}</p>}
      {error && <><p role="alert">{error}</p><button type="button" disabled={busy} onClick={() => void load()}>载入最新配置并替换草稿</button></>}
      {notice && <p role="status">{notice}</p>}
      <button type="button" className="primary" disabled={busy || !revision} onClick={() => void save()}>保存 OCR 路由</button>
    </section>
  )
}
