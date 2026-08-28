import React, { useEffect, useId, useRef, useState } from 'react'
import { loadMermaidEngine, mountMermaidSvg, prepareMermaidSource } from './tideMermaid'

export { mermaidInitConfig, mermaidThemeVariables, mountMermaidSvg } from './tideMermaid'

let renderSeq = 0
let mermaidQueue: Promise<unknown> = Promise.resolve()

function withMermaidLock<T>(work: () => Promise<T>): Promise<T> {
  const run = mermaidQueue.then(work, work)
  mermaidQueue = run.then(() => undefined, () => undefined)
  return run
}

export function MermaidBlock({ source, onCopy }: { source: string; onCopy?: (value: string) => void | Promise<void> }) {
  const hostRef = useRef<HTMLDivElement>(null)
  const id = useId().replace(/:/g, '')
  const [error, setError] = useState('')
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
    const run = async () => {
      setError('')
      const prepared = prepareMermaidSource(source)
      if (!prepared) {
        setError('空图表')
        return
      }
      try {
        await withMermaidLock(async () => {
          const mermaid = await loadMermaidEngine()
          const { svg } = await mermaid.render(`mmd-${id}-${++renderSeq}`, prepared)
          if (cancelled || !hostRef.current) return
          mountMermaidSvg(hostRef.current, svg)
        })
      } catch (e) {
        if (cancelled) return
        if (hostRef.current) hostRef.current.replaceChildren()
        setError(e instanceof Error ? e.message : '图表渲染失败')
      }
    }
    void run()
    return () => {
      cancelled = true
      if (hostRef.current) hostRef.current.replaceChildren()
    }
  }, [id, source, themeEpoch])

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
      <div ref={hostRef} className="mermaid-host" hidden={!!error} aria-label="Mermaid 图表" />
    </div>
  )
}
