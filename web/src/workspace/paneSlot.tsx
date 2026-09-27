import React, { useEffect, useRef } from 'react'

/** The hole the native side browser is positioned over. The page itself is a
 *  separate document, so this div stays empty on purpose. */
export function BrowserPane({ url, frameKey, onNavigate, onTitle, title }: {
  url: string
  frameKey: number
  onNavigate?: (url: string) => void
  onTitle?: (title: string, url?: string) => void
  title?: string
}): React.JSX.Element {
  const slot = useRef<HTMLDivElement>(null)
  const urlRef = useRef(url)
  const keyRef = useRef(frameKey)
  const navigateRef = useRef(onNavigate)
  const titleRef = useRef(onTitle)
  urlRef.current = url
  if (keyRef.current < frameKey) keyRef.current = frameKey
  navigateRef.current = onNavigate
  titleRef.current = onTitle
  useEffect(() => {
    const host = () => window.chrome?.webview
    const postShow = () => {
      const node = slot.current
      const view = host()
      if (!node || !view) return
      const rect = node.getBoundingClientRect()
      view.postMessage({ source: 'lunitide-pane', op: 'show', url: urlRef.current, key: keyRef.current, x: rect.left, y: rect.top, width: rect.width, height: rect.height })
    }
    const onMessage = (event: MessageEvent) => {
      const data = event.data as { source?: string; type?: string; text?: string; url?: string } | null
      if (!data || data.source !== 'lunitide-pane') return
      if (data.type === 'cite' && data.text) window.dispatchEvent(new CustomEvent('lunitide:preview-cite', { detail: { text: data.text } }))
      if (data.type === 'text' && typeof data.text === 'string') window.dispatchEvent(new CustomEvent('lunitide:preview-text', { detail: { text: data.text } }))
      if (data.type === 'url' && data.url) navigateRef.current?.(data.url)
      if (data.type === 'title' && data.text) titleRef.current?.(data.text, data.url)
    }
    const onAct = (event: Event) => {
      const text = String((event as CustomEvent<{ text?: string }>).detail?.text ?? '').trim()
      if (text) host()?.postMessage({ source: 'lunitide-pane', op: 'click', text })
    }
    const onRead = () => host()?.postMessage({ source: 'lunitide-pane', op: 'read' })
    host()?.addEventListener('message', onMessage)
    window.addEventListener('lunitide:preview-act', onAct)
    window.addEventListener('lunitide:preview-read', onRead)
    const onReload = () => {
      keyRef.current += 1
      postShow()
    }
    window.addEventListener('lunitide:preview-reload', onReload)
    postShow()
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(postShow) : null
    if (slot.current) observer?.observe(slot.current)
    window.addEventListener('resize', postShow)
    return () => {
      observer?.disconnect()
      window.removeEventListener('resize', postShow)
      host()?.removeEventListener('message', onMessage)
      window.removeEventListener('lunitide:preview-act', onAct)
      window.removeEventListener('lunitide:preview-read', onRead)
      window.removeEventListener('lunitide:preview-reload', onReload)
    }
  }, [url, frameKey])
  useEffect(() => () => { window.chrome?.webview?.postMessage({ source: 'lunitide-pane', op: 'hide' }) }, [])
  return <div ref={slot} className="workspace-browser-frame" data-lunitide-pane="browser" data-lunitide-page-url={url} title={title ?? `页面 ${url}`} style={{ height: '100%', width: '100%' }} />
}
