import React, { useEffect, useId, useRef, useState } from 'react'

let mermaidPromise: Promise<typeof import('mermaid').default> | undefined

async function loadMermaid() {
  if (!mermaidPromise) {
    mermaidPromise = import('mermaid').then(mod => {
      mod.default.initialize({
        startOnLoad: false,
        theme: 'dark',
        securityLevel: 'strict',
        fontFamily: 'Inter, "Noto Sans SC", ui-sans-serif, system-ui, sans-serif',
      })
      return mod.default
    })
  }
  return mermaidPromise
}

export function MermaidBlock({ source, onCopy }: { source: string; onCopy?: (value: string) => void | Promise<void> }) {
  const hostRef = useRef<HTMLDivElement>(null)
  const id = useId().replace(/:/g, '')
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    let cancelled = false
    const run = async () => {
      setError('')
      try {
        const mermaid = await loadMermaid()
        const { svg } = await mermaid.render(`mmd-${id}`, source.trim())
        if (cancelled || !hostRef.current) return
        hostRef.current.innerHTML = svg
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : '图表渲染失败')
      }
    }
    void run()
    return () => {
      cancelled = true
      if (hostRef.current) hostRef.current.innerHTML = ''
    }
  }, [id, source])

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
      ) : (
        <div ref={hostRef} className="mermaid-host" aria-label="Mermaid 图表" />
      )}
    </div>
  )
}
