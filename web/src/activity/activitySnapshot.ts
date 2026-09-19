import type { ActivitySnapshotDTO } from '../generated/bridge'

export type ActivityLampTone = 'ready' | 'warn' | 'error'
export type ActivityLampDomain = ActivitySnapshotDTO['domain']

export function activityNeedsAttention(item: ActivitySnapshotDTO): boolean {
  if (item.phase === 'queued' || item.phase === 'awaiting_approval' || item.phase === 'running' || item.phase === 'verifying') return true
  return item.phase === 'failed' || item.phase === 'uncertain'
}

export function activityQuiet(items: ActivitySnapshotDTO[]): boolean {
  return !items.some(activityNeedsAttention)
}

/** Traffic-light tone. Running is healthy; unused domains stay green. */
export function activityLampTone(items: ActivitySnapshotDTO[], domain?: ActivityLampDomain): ActivityLampTone {
  const scoped = domain ? items.filter(item => item.domain === domain) : items
  if (scoped.some(item => item.phase === 'failed')) return 'error'
  if (scoped.some(item => item.phase === 'uncertain')) return 'warn'
  return 'ready'
}

export function activityLampWorst(items: ActivitySnapshotDTO[]): ActivityLampTone {
  const tones: ActivityLampTone[] = [
    activityLampTone(items, 'tool'),
    activityLampTone(items, 'ocr'),
    activityLampTone(items, 'media'),
  ]
  if (tones.includes('error')) return 'error'
  if (tones.includes('warn')) return 'warn'
  return 'ready'
}

export type DiagnosticLampItem = { id: string; state: string; code?: string }

export function foldLampTone(left: ActivityLampTone, right: ActivityLampTone): ActivityLampTone {
  if (left === 'error' || right === 'error') return 'error'
  if (left === 'warn' || right === 'warn') return 'warn'
  return 'ready'
}

/** Quarantine is yellow; other degraded diagnostics are red. Unused/not_configured stay green. */
export function diagnosticLampTone(item: DiagnosticLampItem): ActivityLampTone {
  if (item.state !== 'degraded') return 'ready'
  if (item.code === 'MCP_QUARANTINED') return 'warn'
  return 'error'
}

export function activityLampToneWithDiagnostics(
  items: ActivitySnapshotDTO[],
  domain: ActivityLampDomain,
  diagnostics: DiagnosticLampItem[] = [],
): ActivityLampTone {
  let tone = activityLampTone(items, domain)
  if (domain === 'tool') {
    for (const item of diagnostics) {
      if (item.id === 'mcp' || item.id === 'skills' || item.id === 'plugins') {
        tone = foldLampTone(tone, diagnosticLampTone(item))
      }
    }
  }
  return tone
}

export function activityLampWorstWithDiagnostics(
  items: ActivitySnapshotDTO[],
  diagnostics: DiagnosticLampItem[] = [],
): ActivityLampTone {
  let tone = activityLampWorst(items)
  for (const item of diagnostics) {
    tone = foldLampTone(tone, diagnosticLampTone(item))
  }
  return tone
}
