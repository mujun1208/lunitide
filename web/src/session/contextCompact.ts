export const COMPACT_USAGE_THRESHOLD = 0.7

export function contextNeedsCompact(usage: number | undefined): boolean {
  return typeof usage === 'number' && Number.isFinite(usage) && usage >= COMPACT_USAGE_THRESHOLD
}

export function compactUsageLabel(usage: number, zh: boolean): string {
  const pct = Math.min(100, Math.max(0, Math.round(usage * 100)))
  return zh ? `压缩 ${pct}%` : `Compact ${pct}%`
}

export function compactPreviewDescription(humanSummary?: string, summaryPreview?: string): string {
  const text = (humanSummary ?? '').trim() || (summaryPreview ?? '').trim()
  if (text) return text.slice(0, 800)
  return '将把较早的消息收成摘要，腾出上下文窗口。压缩后无法逐字找回原文。'
}
