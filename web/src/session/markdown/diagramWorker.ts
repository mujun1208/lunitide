import mermaid from 'mermaid'
import { mermaidBudgetError, prepareMermaidSource } from './tideMermaid'

const channel = (window as unknown as { chrome?: { webview?: { postMessage(value: string): void; addEventListener(type: 'message', handler: (event: { data: unknown }) => void): void } } }).chrome?.webview
let accepted = false
channel?.addEventListener('message', event => {
  if (accepted) return
  accepted = true
  void (async () => {
    try {
      const request = event.data as { source: string; config: Record<string, unknown> }
      const error = mermaidBudgetError(request.source)
      if (error) throw new Error(error)
      // Security fields cannot be overridden by the calling renderer or by
      // Mermaid init directives embedded in model output.
      mermaid.initialize({ ...request.config, securityLevel: 'strict', startOnLoad: false, suppressErrorRendering: true, maxTextSize: 32768 })
      const { svg } = await mermaid.render('isolated-diagram', prepareMermaidSource(request.source))
      if (new TextEncoder().encode(svg).length > 1_000_000) throw new Error('图表输出超过预算')
      channel?.postMessage(JSON.stringify({ svg }))
    } catch (error) { channel?.postMessage(JSON.stringify({ svg: '', error: error instanceof Error ? error.message : '图表渲染失败' })) }
  })()
})
channel?.postMessage(JSON.stringify({ ready: true }))
