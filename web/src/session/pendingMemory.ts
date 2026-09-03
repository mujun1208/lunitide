export type PendingKind = 'memory' | 'mro-uncontrolled' | 'mro-defect'

export type PendingMemoryItem = {
  candidateId: string
  confirmationToken: string
  content: string
  createdAt?: string
  kind?: PendingKind
}

export function locatorIsUncontrolled(locator: string): boolean {
  const raw = locator.trim()
  if (!raw) return false
  if (/[?&]status=uncontrolled\b/i.test(raw)) return true
  try {
    const loc = JSON.parse(raw) as { status?: string }
    return loc.status === 'uncontrolled'
  } catch {
    return raw.includes('"status":"uncontrolled"')
  }
}

export function pendingFromUncontrolledCites(
  cites: readonly { locator: string }[],
  candidateId = '01MROUNCONTROLLEDCONFIRM01',
): PendingMemoryItem | undefined {
  if (!cites.some(cite => locatorIsUncontrolled(cite.locator))) return undefined
  return {
    candidateId,
    confirmationToken: 'mro-uncontrolled',
    content: '待确认：将使用未受控手册回答',
    kind: 'mro-uncontrolled',
  }
}

export function pendingMroFromMessages(
  items: readonly { role: string; text: string; id?: string }[],
  dismissedId = '',
): PendingMemoryItem | undefined {
  for (let i = items.length - 1; i >= 0; i--) {
    if (items[i].role !== 'assistant') continue
    const match = /<!--mro-cite:([\s\S]*?)-->/.exec(items[i].text)
    if (!match) continue
    try {
      const raw = JSON.parse(match[1]) as { cites?: Array<{ locator?: string }> }
      const cites = (raw.cites ?? []).flatMap(row => typeof row.locator === 'string' ? [{ locator: row.locator }] : [])
      const pending = pendingFromUncontrolledCites(cites, items[i].id || '01MROUNCONTROLLEDCONFIRM01')
      if (pending && pending.candidateId !== dismissedId) return pending
    } catch {
      // keep scanning
    }
  }
  return undefined
}

export function pickLatestPending(
  items: readonly PendingMemoryItem[] | undefined,
  dismissedId = '',
): PendingMemoryItem | undefined {
  const next = (items ?? []).filter(item => item.candidateId && item.candidateId !== dismissedId)
  if (!next.length) return undefined
  return [...next].sort((a, b) => (b.createdAt ?? '').localeCompare(a.createdAt ?? ''))[0]
}

export function previewPendingMemory(content: string, max = 72): string {
  const text = content.replace(/\s+/g, ' ').trim()
  if (text.length <= max) return text
  return `${text.slice(0, max)}…`
}
