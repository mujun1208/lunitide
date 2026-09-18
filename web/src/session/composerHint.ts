import { MESSAGE_MAX_BYTES, MESSAGE_MAX_CHARACTERS } from './messageLimits'

export function compactCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0'
  if (n >= 10000) {
    const wan = n / 10000
    const text = wan >= 10 ? String(Math.round(wan)) : wan.toFixed(1).replace(/\.0$/, '')
    return `${text}万`
  }
  return String(Math.round(n))
}

export function tokenUsageCompactLine(display: { inputTokens: number; outputTokens: number }): string {
  return `${compactCount(display.inputTokens)}/${compactCount(display.outputTokens)}`
}

export type HintOperation = { toolName: string; state: string }

function urgency(state: string): number {
  if (state === 'running') return 0
  if (state === 'failed') return 1
  if (state === 'pending' || state === 'intent' || state === 'unknown' || state === 'partial') return 2
  return 3
}

export function operationCompactSummary(
  items: HintOperation[],
  stateLabel: (state: string) => string,
): string {
  if (items.length === 0) return ''
  const top = [...items].sort((a, b) => urgency(a.state) - urgency(b.state))[0]
  const line = `${top.toolName} ${stateLabel(top.state)}`
  return items.length > 1 ? `${line} · +${items.length - 1}` : line
}

export function composerSizeHint(opts: {
  characters: number
  bytes: number
  outgoingCharacters: number
  outgoingBytes: number
  zh: boolean
}): string {
  const near = opts.characters / MESSAGE_MAX_CHARACTERS >= 0.8 || opts.bytes / MESSAGE_MAX_BYTES >= 0.8
  const referenced = opts.outgoingCharacters !== opts.characters || opts.outgoingBytes !== opts.bytes
  const parts: string[] = []
  if (near) parts.push(opts.zh ? `${opts.characters}字` : `${opts.characters}`)
  if (referenced) parts.push(opts.zh ? '+引用' : '+ref')
  return parts.join(' · ')
}
