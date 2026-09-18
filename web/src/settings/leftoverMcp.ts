const ARCHIVED: Array<{ marker: string; label: string }> = [
  { marker: 'server-github', label: 'GitHub' },
  { marker: 'puppeteer', label: 'Puppeteer' },
  { marker: 'server-sqlite', label: 'SQLite' },
  { marker: 'server-git', label: 'Git' },
  { marker: 'server-gdrive', label: 'Google Drive' },
  { marker: 'mcp.linear.app', label: 'Linear' },
  { marker: 'lark-mcp', label: '飞书' },
]
// Playwright (@playwright/mcp) is the current browser.act backend, not leftover.

export function leftoverArchivedMcp(args: readonly string[] | undefined, url?: string): string[] {
  const blob = `${(args ?? []).join(' ')} ${url ?? ''}`
  const hits: string[] = []
  for (const item of ARCHIVED) {
    if (item.marker === 'server-git') {
      if (blob.includes('server-git') && !blob.includes('server-github')) hits.push(item.label)
      continue
    }
    if (blob.includes(item.marker)) hits.push(item.label)
  }
  return hits
}

export function leftoverArchivedNames(endpoints: ReadonlyArray<{ args?: readonly string[]; url?: string; state?: string }>): string[] {
  const names = new Set<string>()
  for (const ep of endpoints) {
    if (ep.state === 'revoked') continue
    leftoverArchivedMcp(ep.args, ep.url).forEach(name => names.add(name))
  }
  return [...names]
}

/** Market "已安装" only for a completed grant, not a failed first handshake. */
export function mcpCountsAsInstalled(item: { state?: string; enabled?: boolean }): boolean {
  if (!item.state || item.state === 'revoked' || item.state === 'probe') return false
  return item.state === 'ready' || Boolean(item.enabled)
}
