export function countAssistantMessages(items: ReadonlyArray<{ role: string }>): number {
  return items.filter(item => item.role === 'assistant').length
}

export type TurnHistorySettlement = {
  persisted: boolean
  fallbackNotice?: string
}

/** Decide whether a terminal turn landed in durable history after home(). */
export function turnHistorySettlement(
  beforeAssistantCount: number,
  afterAssistantCount: number,
  fallbackNotice?: string,
): TurnHistorySettlement {
  if (afterAssistantCount > beforeAssistantCount) {
    return { persisted: true }
  }
  return { persisted: false, fallbackNotice }
}
