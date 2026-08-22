export type WorkspaceToolActivityLike = {
  name: string
  status: string
  summary?: string
  artifact?: { kind: string; path: string; content?: string }
}

/** True when the user asked to see or open a page in the workspace browser. */
export function userWantsBrowserPanel(text: string): boolean {
  const t = text.trim()
  if (!t) return false
  if (/打开(?:一下|下)?(?:网页|网站|页面|链接|网址)/.test(t)) return true
  if (/在(?:工作区|右侧|浏览器).*?(?:打开|看|显示|展示)/.test(t)) return true
  if (/(?:看看|查看|显示|展示|浏览).*?(?:网页|网站|页面)/.test(t)) return true
  if (/(?:网页|网站|页面|链接).*?(?:打开|看看|显示|展示|浏览)/.test(t)) return true
  if (/打开\s*https?:\/\//.test(t)) return true
  if (/https?:\/\/.+/.test(t) && /打开|访问|看看|浏览|去/.test(t)) return true
  if (/网页/.test(t) && /生成|做|写|制作|做一个|帮我/.test(t)) return true
  return false
}

export type SearchHit = { title: string; url: string; snippet: string }

export const isBrowserAddress = (value: string): boolean => {
  const v = value.trim()
  if (/^file:\/\//i.test(v)) return v.length > 7
  try {
    const parsed = new URL(v)
    return (parsed.protocol === 'https:' || parsed.protocol === 'http:') && Boolean(parsed.hostname)
  } catch {
    return false
  }
}

const SEARCH_HREF = /<a href="([^"]+)">\d+\.\s*([^<]*)<\/a><small>([^<]*)<\/small><p>([^<]*)<\/p>/gi

export function parseSearchCards(html: string): { query: string; hits: SearchHit[] } {
  const query = /搜索结果 · ([^<]+)/.exec(html)?.[1]?.trim() ?? ''
  const hits: SearchHit[] = []
  const seen = new Set<string>()
  for (const match of html.matchAll(SEARCH_HREF)) {
    const url = decodeHtml(match[1] ?? '').trim()
    const title = decodeHtml(match[2] ?? '').trim()
    const snippet = decodeHtml(match[4] ?? '').trim()
    if (!isBrowserAddress(url) || !title || seen.has(url)) continue
    seen.add(url)
    hits.push({ title, url, snippet })
  }
  return { query, hits }
}

export function latestBrowserAddress(activities: readonly WorkspaceToolActivityLike[]): string {
  for (const activity of [...activities].reverse()) {
    if (activity.status !== 'tool_completed') continue
    const path = activity.artifact?.path?.trim() ?? ''
    if (path && isBrowserAddress(path)) return path
    const summary = activity.summary ?? ''
    if (activity.name === 'web.search') {
      const fromSummary = /results_url:\s*(\S+)/.exec(summary)?.[1]
      if (fromSummary && isBrowserAddress(fromSummary)) return fromSummary
      const q = /(?:搜索：|query:\s*)(.+)/.exec(summary)?.[1]?.trim()
      if (q) return `https://cn.bing.com/search?q=${encodeURIComponent(q)}`
    }
    if (activity.name === 'web.fetch' || activity.name.startsWith('browser.')) {
      const fromSummary = /^url:\s*(\S+)/m.exec(summary)?.[1]
      if (fromSummary && isBrowserAddress(fromSummary)) return fromSummary
    }
  }
  return ''
}

function decodeHtml(value: string): string {
  return value
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
}
