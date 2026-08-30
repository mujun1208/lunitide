const ARCHIVED: Array<{ marker: string; label: string }> = [
  { marker: 'server-github', label: 'GitHub' },
  { marker: 'puppeteer', label: 'Puppeteer' },
  { marker: 'server-sqlite', label: 'SQLite' },
  { marker: 'server-git', label: 'Git' },
]

export function leftoverArchivedMcp(args: readonly string[] | undefined): string[] {
  const blob = (args ?? []).join(' ')
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
