export type PendingMemoryItem = {
  candidateId: string
  confirmationToken: string
  content: string
  createdAt?: string
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
