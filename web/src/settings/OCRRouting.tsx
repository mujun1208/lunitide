import React, { useEffect, useRef, useState } from 'react'
import { createMutationAttempt, getOCRRoutingBridge, getProviderBridge, type OCRRoutingBridge, type OCRRoutingSnapshot, type OCRRoutingUpdate, type ProviderBridge } from '../bridge/client'
import { agentHubApi } from '../agentHub/agentHubApi'
import type { ProviderDTO } from '../generated/bridge'
import { modelKind } from '../provider/modelKind'

type LocalEngine = 'windows-ocr' | 'ppocr'
type OCRPack = { available: boolean; status: string; backend: string }
type OCRRoutingView = OCRRoutingSnapshot & {
  localEngine?: LocalEngine
  packRoot?: string
  pack?: OCRPack
}
type OCRRoutingWrite = OCRRoutingUpdate & {
  localEngine?: LocalEngine
  packRoot?: string
}

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

function applySnapshot(
  got: OCRRoutingView,
  set: {
    setProviderId: (v: string) => void
    setModelId: (v: string) => void
    setPreferProvider: (v: boolean) => void
    setRevision: (v: string) => void
    setLastFailure: (v: {class: string; operation: string; until: string} | undefined) => void
    setLocalReady: (v: {pdf: boolean; image: boolean; backend: string} | undefined) => void
    setLocalEngine: (v: LocalEngine) => void
    setPackRoot: (v: string) => void
    setPack: (v: OCRPack | undefined) => void
  },
) {
  set.setProviderId(got.providerId ?? '')
  set.setModelId(got.modelId ?? '')
  set.setPreferProvider(got.preferProvider)
  set.setRevision(got.revision)
  set.setLastFailure(got.lastFailure)
  set.setLocalReady(got.localReady)
  set.setLocalEngine(got.localEngine === 'ppocr' ? 'ppocr' : 'windows-ocr')
  set.setPackRoot(got.packRoot ?? '')
  set.setPack(got.pack)
}

export function OCRRouting({
  providers,
  ocr,
  pickPackDir,
}: {
  providers?: ProviderBridge
  ocr?: OCRRoutingBridge
  pickPackDir?: () => Promise<{ canceled: boolean; path: string }>
}): React.JSX.Element {
  const providerApi = providers ?? getProviderBridge()
  const ocrApi = ocr ?? getOCRRoutingBridge()
  const pickDir = pickPackDir ?? (() => agentHubApi.pickDir())
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
  const [localEngine, setLocalEngine] = useState<LocalEngine>('windows-ocr')
  const [packRoot, setPackRoot] = useState('')
  const [pack, setPack] = useState<OCRPack>()
  const saving = useRef(false)
  const generation = useRef(0)
  const snapshotSetters = { setProviderId, setModelId, setPreferProvider, setRevision, setLastFailure, setLocalReady, setLocalEngine, setPackRoot, setPack }

  const load = async () => {
    const epoch = ++generation.current
    try {
      const [listed, got] = await Promise.all([providerApi.list(), ocrApi.get()])
      if (epoch !== generation.current) return
      setItems(listed.items)
      applySnapshot(got as OCRRoutingView, snapshotSetters)
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
      const payload: OCRRoutingWrite = {
        expectedRevision: revision,
        preferProvider,
        localEngine,
        ...(packRoot ? { packRoot } : {}),
        ...(providerId && modelId ? { providerId, modelId } : {}),
      }
      const saved = await ocrApi.set(payload as OCRRoutingUpdate, { attempt: createMutationAttempt('ocr.routing.set', payload) })
      if (epoch !== generation.current) return
      applySnapshot(saved as OCRRoutingView, snapshotSetters)
      setNotice(localEngine === 'ppocr'
        ? '已保存偏好。当前识别仍走 Windows OCR，PP-OCR 引擎尚未接入。'
        : 'OCR 路由已保存，下一次识别生效')
    } catch (e) {
      if (epoch === generation.current) setError(ocrUserError(e, 'OCR 路由保存失败'))
    } finally {
      saving.current = false
      if (epoch === generation.current) setBusy(false)
    }
  }

  const installPack = async () => {
    if (saving.current || !revision) return
    setBusy(true)
    setNotice('')
    setError('')
    try {
      const got = await pickDir()
      if (got.canceled || !got.path) return
      const payload: OCRRoutingWrite = {
        expectedRevision: revision,
        preferProvider,
        localEngine,
        packRoot: got.path,
        ...(providerId && modelId ? { providerId, modelId } : {}),
      }
      const saved = await ocrApi.set(payload as OCRRoutingUpdate, { attempt: createMutationAttempt('ocr.routing.set', payload) })
      applySnapshot(saved as OCRRoutingView, snapshotSetters)
      setNotice(saved && (saved as OCRRoutingView).pack?.available
        ? '已记录 PP-OCR 目录。当前识别仍走 Windows OCR，引擎尚未接入。'
        : '已记录目录，但未检测到 PP-OCR 可执行文件或模型。')
    } catch (e) {
      setError(ocrUserError(e, 'PP-OCR 安装失败'))
    } finally {
      setBusy(false)
    }
  }

  const opts = visionOptions(items)
  const value = providerId && modelId ? `${providerId}\u0000${modelId}` : ''
  const packReady = pack?.available === true
  const engineValue = localEngine === 'ppocr' && packReady ? 'ppocr' : 'windows-ocr'

  return (
    <section className="capability-routing" aria-label="OCR 路由">
      <h3>OCR 路由</h3>
      <p className="setting-desc">已配置供应商时优先走云端识别；失败、未配置或停用后自动本机兜底。本机识别目前只走内置 Windows OCR。可记录自备 PP-OCR 目录作为偏好，引擎尚未接入，不是随包提供。这不是第七个能力角色。保存后从下一次识别开始生效。</p>
      <label className="capability-role-row">
        <span>本机 OCR 引擎</span>
        <select
          aria-label="本机 OCR 引擎"
          value={engineValue}
          disabled={busy}
          onChange={e => setLocalEngine(e.target.value === 'ppocr' ? 'ppocr' : 'windows-ocr')}
        >
          <option value="windows-ocr">Windows OCR（内置）</option>
          <option value="ppocr" disabled={!packReady}>{packReady ? 'PP-OCR' : 'PP-OCR（未安装）'}</option>
        </select>
        <small>{packReady ? '目录已记录，识别仍走 Windows OCR' : '未安装，点下方安装'}</small>
      </label>
      <div className="capability-role-row">
        <span>PP-OCR</span>
        <button type="button" disabled={busy || !revision} onClick={() => void installPack()}>安装 PP-OCR</button>
        <small>选择含 ppocr.onnx 或 ppocr.exe 的解压目录。不是随包，接入前识别仍走 Windows OCR</small>
      </div>
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
