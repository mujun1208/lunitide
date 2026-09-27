export type BrowserTab = {
  id: string
  url: string
  title: string
  trail: string[]
  index: number
}

export function createBrowserTab(id: string, url = 'https://'): BrowserTab {
  const open = pageHost(url) !== ''
  return { id, url, title: '', trail: open ? [url] : [], index: open ? 0 : -1 }
}

export function browserTabLabel(tab: BrowserTab): string {
  const title = tab.title.trim()
  if (title) return title
  const host = pageHost(tab.url).replace(/^www\./, '')
  return host || '新标签页'
}

export function navigateBrowserTab(tab: BrowserTab, url: string): BrowserTab {
  const moved = pushTrail(tab.trail, tab.index, url)
  return { ...tab, url, title: '', trail: moved.trail, index: moved.index }
}

export function followBrowserTab(tab: BrowserTab, url: string): BrowserTab {
  if (tab.url === url) return tab
  return navigateBrowserTab(tab, url)
}

export function retitleBrowserTab(tab: BrowserTab, title: string): BrowserTab {
  return { ...tab, title: title.trim().slice(0, 80) }
}

export function moveBrowserTab(tab: BrowserTab, step: -1 | 1): BrowserTab {
  const index = tab.index + step
  const url = tab.trail[index]
  if (!url) return tab
  return { ...tab, url, title: '', index }
}

export function closeBrowserTab(tabs: readonly BrowserTab[], activeId: string, closingId: string, replacementId: string): { tabs: BrowserTab[]; activeId: string } {
  const index = tabs.findIndex(tab => tab.id === closingId)
  if (index < 0) return { tabs: [...tabs], activeId }
  const next = tabs.filter(tab => tab.id !== closingId)
  if (next.length === 0) {
    const blank = createBrowserTab(replacementId)
    return { tabs: [blank], activeId: blank.id }
  }
  if (activeId !== closingId) return { tabs: next, activeId }
  return { tabs: next, activeId: next[Math.min(index, next.length - 1)].id }
}

/** A new address joins the strip. A blank tab is filled instead of leaving an empty page beside it. */
export function placeBrowserAddress(tabs: readonly BrowserTab[], activeId: string, url: string, idForNew: string): { tabs: BrowserTab[]; activeId: string } {
  const active = tabs.find(tab => tab.id === activeId) ?? tabs[0]
  if (!active) return { tabs: [navigateBrowserTab(createBrowserTab(idForNew), url)], activeId: idForNew }
  if (active.url === url) return { tabs: [...tabs], activeId: active.id }
  if (pageHost(active.url) === '') {
    return { tabs: tabs.map(tab => tab.id === active.id ? navigateBrowserTab(tab, url) : tab), activeId: active.id }
  }
  const opened = navigateBrowserTab(createBrowserTab(idForNew), url)
  return { tabs: [...tabs, opened], activeId: opened.id }
}

function pageHost(url: string): string {
  try { return new URL(url).hostname } catch { return '' }
}

function pushTrail(trail: readonly string[], index: number, url: string): { trail: string[]; index: number } {
  if (trail[index] === url) return { trail: [...trail], index }
  const next = trail.slice(0, index + 1)
  next.push(url)
  return { trail: next, index: next.length - 1 }
}
