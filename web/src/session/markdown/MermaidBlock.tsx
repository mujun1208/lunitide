import { getDiagramBridge } from '../../bridge/client'
import React, { useEffect, useId, useRef, useState } from 'react'
import {
  mermaidInitConfig,
  mermaidBudgetError,
  mermaidSourceReady,
  mermaidTransientError,
  mountMermaidSvg,
  prepareMermaidSource,
  recoverMermaidSource,
} from './tideMermaid'

export { mermaidInitConfig, mermaidThemeVariables, mountMermaidSvg } from './tideMermaid'

const SETTLE_MS = 480
const RETRIES = 3

let mermaidQueue: Promise<unknown> = Promise.resolve()

function withMermaidLock<T>(work: () => Promise<T>): Promise<T> {
  const run = mermaidQueue.then(work, work)
  mermaidQueue = run.then(() => undefined, () => undefined)
  return run
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => window.setTimeout(resolve, ms))
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
    if (wait || !mermaidSourceReady(source)) {
      setError('')
      if (!hasSvgRef.current) setPending(true)
      return
    }
    const timer = window.setTimeout(() => {
      void run()
    }, SETTLE_MS)
    const mount = async (src: string) => {
      if (cancelled) return
      const { svg } = await getDiagramBridge().render({ source: src, config: mermaidInitConfig() })
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
          let last: unknown
          for (let attempt = 0; attempt < RETRIES; attempt++) {
            if (cancelled) return
            try {
              try {
                await mount(prepared)
              } catch (first) {
                const recovered = prepareMermaidSource(recoverMermaidSource(source))
                if (!recovered || recovered === prepared) throw first
                await mount(recovered)
              }
              return
            } catch (err) {
              last = err
              const message = err instanceof Error ? err.message : '图表渲染失败'
              if (cancelled || attempt === RETRIES - 1 || !mermaidTransientError(message)) throw err
              await sleep(350 * (attempt + 1))
            }
          }
          throw last
        })
      } catch (e) {
        if (cancelled) return
        if (!hasSvgRef.current && hostRef.current) hostRef.current.replaceChildren()
        setPending(false)
        setError(e instanceof Error ? e.message : '图表渲染失败')
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
      <div ref={hostRef} className="mermaid-host" hidden={!!error} aria-label="Mermaid 图表" />
    </div>
  )
}
