import React, { useCallback, useEffect, useRef, useState } from 'react'
import { createMutationAttempt, getOCRRoutingBridge, getProviderBridge, type OCRInstallSnapshot, type OCRRoutingBridge, type OCRRoutingSnapshot, type OCRRoutingUpdate, type ProviderBridge } from '../bridge/client'
import type { ProviderDTO } from '../generated/bridge'
import { modelKind } from '../provider/modelKind'

type LocalEngine = 'auto' | 'windows-ocr' | 'ppocr'
type OCRPack = { available: boolean; status: string; backend: string }
type OCRRoutingView = OCRRoutingSnapshot & {
  localEngine?: LocalEngine
  packRoot?: string
  pack?: OCRPack
  downloadBytes?: number
}
type OCRRoutingWrite = OCRRoutingUpdate & {
  localEngine?: LocalEngine
}

type Option = { value: string; label: string; providerId: string; modelId: string }

const POLL_MS = 700
const OCR_NAME_RE = /ocr|识别|document|paddleocr|rapidocr/i

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

function parseLocalEngine(value?: string): LocalEngine {
  if (value === 'ppocr' || value === 'windows-ocr') return value
  return 'auto'
}

function isOcrNamed(model: { modelId: string; displayName?: string }): boolean {
  return OCR_NAME_RE.test(`${model.modelId} ${model.displayName ?? ''}`)
}

function providerOcrOptions(providers: ProviderDTO[]): Option[] {
  const named: Option[] = []
  const vision: Option[] = []
  for (const p of providers) {
    for (const m of p.models) {
      const kind = modelKind(m)
      const namedOcr = isOcrNamed(m)
      const visionish = kind === 'vision' || (kind === 'llm' && m.supportsVision)
      if (!namedOcr && !visionish) continue
      const option: Option = {
        value: `${p.id}\u0000${m.modelId}`,
        label: `${p.name} / ${m.displayName || m.modelId}`,
        providerId: p.id,
        modelId: m.modelId,
      }
      if (namedOcr) named.push(option)
      else vision.push(option)
    }
  }
  return [...named, ...vision]
}

function megabytes(bytes: number): string {
  return `${(bytes / 1024 / 1024).toFixed(0)} MB`
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
    setPack: (v: OCRPack | undefined) => void
    setDownloadBytes: (v: number) => void
  },
) {
  set.setProviderId(got.providerId ?? '')
  set.setModelId(got.modelId ?? '')
  set.setPreferProvider(got.preferProvider)
  set.setRevision(got.revision)
  set.setLastFailure(got.lastFailure)
  set.setLocalReady(got.localReady)
  set.setLocalEngine(parseLocalEngine(got.localEngine))
  set.setPack(got.pack)
  set.setDownloadBytes(got.downloadBytes ?? 0)
}

export function OCRRouting({
  providers,
  ocr,
}: {
  providers?: ProviderBridge
  ocr?: OCRRoutingBridge
}): React.JSX.Element {
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
  const [localEngine, setLocalEngine] = useState<LocalEngine>('auto')
  const [pack, setPack] = useState<OCRPack>()
  const [downloadBytes, setDownloadBytes] = useState(0)
  const [progress, setProgress] = useState<OCRInstallSnapshot>()
  const saving = useRef(false)
  const generation = useRef(0)
  const timer = useRef(0)
  const alive = useRef(true)
  const snapshotSetters = { setProviderId, setModelId, setPreferProvider, setRevision, setLastFailure, setLocalReady, setLocalEngine, setPack, setDownloadBytes }

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

  useEffect(() => {
    alive.current = true
    void load()
    return () => {
      alive.current = false
      generation.current++
      window.clearTimeout(timer.current)
    }
  }, [providerApi, ocrApi])

  const write = async (next: { revision: string; preferProvider: boolean; localEngine: LocalEngine; providerId: string; modelId: string }) => {
    if (saving.current || !next.revision) return
    saving.current = true
    const epoch = generation.current
    setBusy(true)
    setNotice('')
    setError('')
    try {
      const payload: OCRRoutingWrite = {
        expectedRevision: next.revision,
        preferProvider: next.preferProvider,
        localEngine: next.localEngine,
        ...(next.providerId && next.modelId ? { providerId: next.providerId, modelId: next.modelId } : {}),
      }
      const saved = await ocrApi.set(payload as OCRRoutingUpdate, { attempt: createMutationAttempt('ocr.routing.set', payload) })
      if (epoch !== generation.current) return
      applySnapshot(saved as OCRRoutingView, snapshotSetters)
      setNotice('OCR 路由已保存，下一次识别生效')
    } catch (e) {
      if (epoch === generation.current) setError(ocrUserError(e, 'OCR 路由保存失败'))
    } finally {
      saving.current = false
      if (epoch === generation.current) setBusy(false)
    }
  }

  const save = () => write({ revision, preferProvider, localEngine, providerId, modelId })
  const writeRef = useRef(write)
  writeRef.current = write

  const pump = useCallback(() => {
    void ocrApi.install()
      .then(async result => {
        if (!alive.current) return
        setProgress(result)
        if (result.state === 'downloading') {
          timer.current = window.setTimeout(pump, POLL_MS)
          return
        }
        if (result.state === 'failed') {
          setBusy(false)
          setError(result.lastError || 'PP-OCR 下载失败')
          return
        }
        const got = await ocrApi.get() as OCRRoutingView
        if (!alive.current) return
        applySnapshot(got, snapshotSetters)
        await writeRef.current({
          revision: got.revision,
          preferProvider: got.preferProvider,
          localEngine: parseLocalEngine(got.localEngine),
          providerId: got.providerId ?? '',
          modelId: got.modelId ?? '',
        })
      })
      .catch(e => {
        if (!alive.current) return
        setBusy(false)
        setProgress({ state: 'failed', percent: 0, doneBytes: 0, totalBytes: 0, lastError: ocrUserError(e, 'PP-OCR 安装失败') })
        setError(ocrUserError(e, 'PP-OCR 安装失败'))
      })
  }, [ocrApi])

  const installPack = () => {
    if (busy || !revision) return
    setBusy(true)
    setNotice('')
    setError('')
    setProgress(undefined)
    pump()
  }

  const opts = providerOcrOptions(items)
  const value = providerId && modelId ? `${providerId}\u0000${modelId}` : ''
  const packReady = pack?.available === true
  const state = progress?.state
  const ready = packReady
  const incomplete = state === 'ready' && !packReady
  const failed = state === 'failed' || incomplete
  const downloading = (busy || state === 'downloading') && !ready && !failed
  const sizeLabel = downloadBytes > 0 ? megabytes(downloadBytes) : '28 MB'
  const packDesc = packReady
    ? '已就绪，选「自动」或「PP-OCR」后从下一次识别走本机引擎。'
    : downloading && progress && progress.totalBytes > 0
      ? `正在下载：${progress.percent}% · ${megabytes(progress.doneBytes)} / ${megabytes(progress.totalBytes)}${progress.file ? ` · ${progress.file}` : ''}`
      : failed
        ? `下载失败：${progress?.lastError || (incomplete ? '安装包还不能运行，请重试。' : '未知原因')}。可重试。`
        : `约 ${sizeLabel}，点按钮下载安装。不是随安装包附带。`

  const localHint = localReady
    ? localReady.backend === 'ppocr'
      ? '（已走本机 PP-OCR）'
      : localReady.backend === 'windows-ocr'
        ? '（Windows 兜底，需系统语言包）'
        : '（当前平台未封装本地识别）'
    : ''

  return (
    <section className="capability-routing" aria-label="OCR 路由">
      <h3>OCR 路由</h3>
      <p className="setting-desc">优先级：① 供应商 OCR / 视觉模型 → ② 已安装的 PP-OCR → ③ Windows OCR 兜底。供应商失败、未绑定或停用后自动落到本机。自动＝装好 PP-OCR 就用它，否则 Windows。这不是第七个能力角色。改引擎、装完或点保存后，从下一次识别生效。</p>
      <label className="capability-role-row">
        <span>本机 OCR 引擎</span>
        <select
          aria-label="本机 OCR 引擎"
          value={localEngine}
          disabled={busy}
          onChange={e => {
            const next = parseLocalEngine(e.target.value)
            setLocalEngine(next)
            void write({ revision, preferProvider, localEngine: next, providerId, modelId })
          }}
        >
          <option value="auto">自动（推荐）</option>
          <option value="ppocr" disabled={!packReady}>{packReady ? 'PP-OCR' : 'PP-OCR（未安装）'}</option>
          <option value="windows-ocr">Windows OCR（内置）</option>
        </select>
        <small>{packReady ? '已安装 PP-OCR，自动会优先用它' : '未安装时自动走 Windows 兜底'}</small>
      </label>
      <div className="capability-role-row">
        <span>PP-OCR</span>
        <button type="button" disabled={downloading || ready || !revision} onClick={installPack}>
          {ready ? '已安装' : downloading ? '下载中…' : failed ? '重试下载' : '下载安装'}
        </button>
        <small>{packDesc}</small>
      </div>
      <label className="capability-role-row">
        <span>OCR 模型</span>
        <select
          aria-label="OCR 模型"
          value={value}
          disabled={busy}
          onChange={e => {
            const hit = opts.find(o => o.value === e.target.value)
            const nextProvider = hit?.providerId ?? ''
            const nextModel = hit?.modelId ?? ''
            setProviderId(nextProvider)
            setModelId(nextModel)
            void write({ revision, preferProvider, localEngine, providerId: nextProvider, modelId: nextModel })
          }}
        >
          <optgroup label="① 供应商 OCR">
            {opts.length
              ? opts.map(o => <option key={o.value} value={o.value}>{o.label}</option>)
              : <option value="__none_provider__" disabled>未配置供应商 OCR / 视觉模型</option>}
          </optgroup>
          <optgroup label="② 本机 PP-OCR">
            <option value="__ppocr_local__" disabled>{packReady ? '已安装，由上方本机引擎选择' : '未安装，点下载安装'}</option>
          </optgroup>
          <optgroup label="③ Windows 兜底">
            <option value="">Windows OCR（系统语言包）</option>
          </optgroup>
        </select>
        <small>空＝不调用供应商，直接按本机引擎优先级</small>
      </label>
      <label className="default">
        <input type="checkbox" checked={preferProvider} disabled={busy} onChange={e => {
          const next = e.target.checked
          setPreferProvider(next)
          void write({ revision, preferProvider: next, localEngine, providerId, modelId })
        }} />
        供应商可用时优先云端
      </label>
      {localReady && <p className="setting-desc">本机后端 {localReady.backend}{localHint}；PDF {localReady.pdf ? '已封装' : '不可用'}，图片 {localReady.image ? '已封装' : '不可用'}</p>}
      {lastFailure && <p role="status">最近供应商失败：{ocrFailureClassLabel(lastFailure.class)} / {ocrFailureOperationLabel(lastFailure.operation)}，冷却至 {lastFailure.until}</p>}
      {error && <><p role="alert">{error}</p><button type="button" disabled={busy} onClick={() => void load()}>载入最新配置并替换草稿</button></>}
      {notice && <p role="status">{notice}</p>}
      <button type="button" className="primary" disabled={busy || !revision} onClick={() => void save()}>保存 OCR 路由</button>
    </section>
  )
}
