import type {MessageDTO} from '../generated/bridge'
import type {StreamEvent} from '../bridge/client'
import {formatTaskElapsed} from './MarkdownMessage'

export type TurnStatsValue = NonNullable<MessageDTO['turnStats']>
export type TurnUsageValue = Extract<StreamEvent, {type: 'usage'}>['usage']

const numberFmt = new Intl.NumberFormat('en-US')

/** One-line turn footer: elapsed time plus provider tokens, rendered right
 *  under the finished reply. Reuses the existing token-usage styling so the
 *  chat look stays untouched. */
export function turnStatsLine(durationMs: number | undefined, usage: {inputTokens: number; outputTokens: number; totalTokens: number} | undefined, zh: boolean): string {
  const parts: string[] = []
  if (durationMs !== undefined && durationMs >= 0) {
    parts.push(zh ? `本轮耗时 ${formatTaskElapsed(durationMs)}` : `Elapsed ${formatTaskElapsed(durationMs)}`)
  }
  if (usage && (usage.totalTokens > 0 || usage.inputTokens > 0 || usage.outputTokens > 0)) {
    const tokens = zh
      ? `输入 ${numberFmt.format(usage.inputTokens)} · 输出 ${numberFmt.format(usage.outputTokens)} · 合计 ${numberFmt.format(usage.totalTokens)} tokens`
      : `Input ${numberFmt.format(usage.inputTokens)} · Output ${numberFmt.format(usage.outputTokens)} · Total ${numberFmt.format(usage.totalTokens)} tokens`
    parts.push(tokens)
  }
  return parts.join(' · ')
}

export function TurnStatsLine({durationMs, usage, stats, zh}: {durationMs?: number; usage?: TurnUsageValue; stats?: TurnStatsValue; zh: boolean}) {
  const live = usage && !stats ? {durationMs: usage.durationMs ?? durationMs, inputTokens: usage.inputTokens, outputTokens: usage.outputTokens, totalTokens: usage.totalTokens} : stats
  const effectiveDuration = stats ? stats.durationMs : (live?.durationMs ?? durationMs)
  const effectiveUsage = live
  const line = turnStatsLine(effectiveDuration, effectiveUsage, zh)
  if (!line) return null
  return <div className="chat-usage token-usage token-usage-compact turn-stats-line" role="status" aria-label={zh ? '本轮耗时与 token 用量' : 'Turn elapsed and token usage'}><span>{line}</span></div>
}
