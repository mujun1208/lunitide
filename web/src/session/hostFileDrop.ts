export type HostDropItem = { path: string; fileName: string; mime: string; size: number }

const hostDropTTL = 4_000
let hostDropClaim: { names: string[]; at: number } | undefined

export function rememberHostDrop(items: readonly { fileName: string }[], now = Date.now()) {
  hostDropClaim = { names: items.map(item => item.fileName).filter(Boolean), at: now }
}

export function hostDropCovers(names: readonly string[], now = Date.now()) {
  if (!hostDropClaim || names.length === 0 || now - hostDropClaim.at > hostDropTTL) return false
  const claimed = new Set(hostDropClaim.names)
  return names.every(name => claimed.has(name))
}

export function publishHostDrop(items: readonly HostDropItem[], now = Date.now()) {
  rememberHostDrop(items, now)
  window.dispatchEvent(new CustomEvent('lunitide:files-dropped', { detail: items }))
}

export function subscribeHostFileDrop(onItems: (items: HostDropItem[]) => void): () => void {
  const view = window.chrome?.webview
  if (!view) return () => {}
  const onMessage = (event: MessageEvent) => {
    const data = event.data as { source?: string; type?: string; items?: HostDropItem[] } | null
    if (!data || data.source !== 'lunitide-host' || data.type !== 'filesDropped' || !Array.isArray(data.items)) return
    const items = data.items.filter(item => item && typeof item.path === 'string' && item.path !== '' && typeof item.fileName === 'string' && item.fileName !== '')
    if (!items.length) return
    publishHostDrop(items)
    onItems(items)
  }
  view.addEventListener('message', onMessage as (event: MessageEvent) => void)
  return () => view.removeEventListener('message', onMessage as (event: MessageEvent) => void)
}

export function preferHostDrop(names: readonly string[], waitMs = 400): Promise<boolean> {
  if (!window.chrome?.webview) return Promise.resolve(false)
  if (hostDropCovers(names)) return Promise.resolve(true)
  return new Promise(resolve => {
    const finish = (covered: boolean) => {
      window.clearTimeout(timer)
      window.removeEventListener('lunitide:files-dropped', onDrop)
      resolve(covered)
    }
    const onDrop = () => {
      if (hostDropCovers(names)) finish(true)
    }
    const timer = window.setTimeout(() => finish(hostDropCovers(names)), waitMs)
    window.addEventListener('lunitide:files-dropped', onDrop)
  })
}
