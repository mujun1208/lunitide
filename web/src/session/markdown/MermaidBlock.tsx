import React, { useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { Dialog } from '../../ui/Dialog'
import {
  mermaidBudgetError,
  mermaidSourceReady,
  mountMermaidSvg,
  prepareMermaidSource,
  recoverMermaidSource,
  loadMermaidEngine,
  fitMermaidSvg,
} from './tideMermaid'

export { mermaidInitConfig, mermaidThemeVariables, mountMermaidSvg } from './tideMermaid'

function mermaidUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

const SETTLE_MS = 280

let mermaidQueue: Promise<unknown> = Promise.resolve()

function withMermaidLock<T>(work: () => Promise<T>): Promise<T> {
  const run = mermaidQueue.then(work, work)
  mermaidQueue = run.then(() => undefined, () => undefined)
  return run
}

export function MermaidBlock({
  source,
  onCopy,
  onLayout,
  wait = false,
}: {
  source: string
  onCopy?: (value: string) => void | Promise<void>
  onLayout?: () => void
  wait?: boolean
}) {
  const hostRef = useRef<HTMLDivElement>(null)
  const onLayoutRef = useRef(onLayout)
  onLayoutRef.current = onLayout
  const id = useId().replace(/:/g, '')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(true)
  const [hasSvg, setHasSvg] = useState(false)
  const hasSvgRef = useRef(false)
  const [copied, setCopied] = useState(false)
  const [open, setOpen] = useState(false)
  const [zoom, setZoom] = useState(1)
  const lightboxRef = useRef<HTMLDivElement>(null)
  const [themeEpoch, setThemeEpoch] = useState(0)

  useEffect(() => {
    if (typeof MutationObserver === 'undefined' || typeof document === 'undefined') return
    const obs = new MutationObserver(() => setThemeEpoch(n => n + 1))
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    return () => obs.disconnect()
  }, [])

  useEffect(() => {
    let cancelled = false
    const budgetError = mermaidBudgetError(source)
    if (budgetError) {
      setPending(false)
      setError(budgetError)
      return
    }
    if (wait) {
      setError('')
      if (!hasSvgRef.current) setPending(true)
      return
    }
    if (!mermaidSourceReady(source)) {
      setPending(false)
      setError('图表源码未闭合或未写完，源码仍保留')
      return
    }
    const timer = window.setTimeout(() => {
      void run()
    }, SETTLE_MS)
    const mount = async (src: string) => {
      if (cancelled) return
      const engine = await loadMermaidEngine()
      const { svg } = await engine.render(`tide-mermaid-${id}`, src)
      if (cancelled || !hostRef.current) return
      mountMermaidSvg(hostRef.current, svg)
      hasSvgRef.current = true
      setHasSvg(true)
      setPending(false)
      setError('')
      onLayoutRef.current?.()
    }
    const run = async () => {
      const prepared = prepareMermaidSource(source)
      if (!prepared) {
        if (!cancelled) {
          setPending(false)
          setError('空图表')
        }
        return
      }
      try {
        await withMermaidLock(async () => {
          try {
            await mount(prepared)
          } catch (first) {
            const recovered = prepareMermaidSource(recoverMermaidSource(source))
            if (!recovered || recovered === prepared) throw first
            await mount(recovered)
          }
        })
      } catch (e) {
        if (cancelled) return
        if (!hasSvgRef.current && hostRef.current) hostRef.current.replaceChildren()
        setPending(false)
        setError(mermaidUserError(e, '图表渲染失败'))
        onLayoutRef.current?.()
      }
    }
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [id, source, themeEpoch, wait])

  useEffect(() => {
    return () => {
      if (hostRef.current) hostRef.current.replaceChildren()
    }
  }, [])

  useLayoutEffect(() => {
    if (!open) return
    const sourceSvg = hostRef.current?.querySelector('svg')
    const host = lightboxRef.current
    if (!sourceSvg || !host) return
    let clone = host.querySelector('svg')
    if (!clone) {
      clone = sourceSvg.cloneNode(true) as SVGSVGElement
      host.replaceChildren(clone)
    }
    fitMermaidSvg(clone, { fill: true, zoom })
  }, [open, hasSvg, zoom])

  const copySource = async () => {
    if (!onCopy) return
    await onCopy(source.trim())
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }

  return (
    <div className="mermaid-block">
      <div className="rich-code-toolbar">
        <span className="rich-code-lang">Mermaid</span>
        {onCopy && (
          <button type="button" className="rich-code-copy" aria-label="复制 Mermaid 源码" onClick={() => void copySource()}>
            {copied ? '已复制' : '复制'}
          </button>
        )}
        {hasSvg ? (
          <button type="button" className="rich-code-copy" aria-label="放大查看" onClick={() => setOpen(true)}>
            放大查看
          </button>
        ) : null}
      </div>
      {error ? (
        <div className="mermaid-fallback">
          <p className="mermaid-error">图表未能渲染：{error}</p>
          <pre className="rich-code-pre">
            <code>{source.trim()}</code>
          </pre>
        </div>
      ) : null}
      {pending && !error && !hasSvg ? <p className="mermaid-pending">图表生成中…</p> : null}
      <button
        type="button"
        className="mermaid-preview"
        aria-label="放大查看图表"
        disabled={!hasSvg || !!error}
        onClick={() => setOpen(true)}
      >
        <div ref={hostRef} className="mermaid-host" hidden={!!error} aria-label="Mermaid 图表" />
      </button>
      <Dialog open={open} title="查看图表" onClose={() => { setOpen(false); setZoom(1) }} wide>
        <div ref={lightboxRef} className="mermaid-lightbox mermaid-skin" aria-label="放大后的 Mermaid 图表" />
        <div className="dialog-actions">
          <button type="button" aria-label="缩小" disabled={zoom <= 0.5} onClick={() => setZoom(value => Math.max(0.5, Math.round((value - 0.5) * 100) / 100))}>
            缩小
          </button>
          <button type="button" aria-label="适合宽度" onClick={() => setZoom(1)}>
            适合宽度
          </button>
          <button type="button" aria-label="放大" disabled={zoom >= 4} onClick={() => setZoom(value => Math.min(4, Math.round((value + 0.5) * 100) / 100))}>
            放大
          </button>
          <button type="button" onClick={() => { setOpen(false); setZoom(1) }}>
            关闭
          </button>
        </div>
      </Dialog>
    </div>
  )
}
