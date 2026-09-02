// Local voice-turn timing. No product thresholds change from these numbers.
// Flush is a console table plus an in-memory ring so 20 idle chats can be
// drawn without leaving the machine.

export type VoiceTimingField =
  | 'endpoint'
  | 'append'
  | 'startReturn'
  | 'ttfb'
  | 'firstSynth'
  | 'firstAudio'
  | 'echo'
  | 'tools'

export type VoiceTurnOutcome = 'ok' | 'stall'

export type VoiceTurnRecord = {
  path: string
  endpointMs?: number
  appendMs?: number
  startReturnMs?: number
  ttfbMs?: number
  firstSynthMs?: number
  firstAudioMs?: number
  echoMs?: number
  toolsMs?: number
  outcome: VoiceTurnOutcome
}

type OpenTurn = VoiceTurnRecord & { t0: number }

const RING = 40
const ring: VoiceTurnRecord[] = []
let current: OpenTurn | undefined

const now = () => (typeof performance !== 'undefined' ? performance.now() : Date.now())

export function startVoiceTurn(path: string): void {
  current = { path, outcome: 'ok', t0: now() }
}

export function currentVoiceTiming(field: VoiceTimingField): number | undefined {
  if (!current) return undefined
  return current[`${field}Ms`]
}

export function markVoiceTiming(field: VoiceTimingField, at = now()): void {
  const turn = current
  if (!turn) return
  const key = `${field}Ms` as const
  if (turn[key] != null) return
  turn[key] = Math.round(at - turn.t0)
}

export function finishVoiceTurn(outcome: VoiceTurnOutcome = 'ok'): VoiceTurnRecord | undefined {
  const turn = current
  if (!turn) return undefined
  current = undefined
  const { t0: _t0, ...record } = turn
  record.outcome = outcome
  ring.push(record)
  if (ring.length > RING) ring.shift()
  if (typeof console !== 'undefined' && console.debug) {
    console.debug(
      `[voiceTiming] path=${record.path} endpoint=${record.endpointMs ?? '-'} append=${record.appendMs ?? '-'} start=${record.startReturnMs ?? '-'} ttfb=${record.ttfbMs ?? '-'} synth=${record.firstSynthMs ?? '-'} audio=${record.firstAudioMs ?? '-'} echo=${record.echoMs ?? '-'} tools=${record.toolsMs ?? '-'} ${record.outcome}`,
    )
  }
  return record
}

export function peekVoiceTimings(): readonly VoiceTurnRecord[] {
  return ring
}

export function resetVoiceTimings(): void {
  ring.length = 0
  current = undefined
}

// Latency budgets in milliseconds. These are the "出字快 / 回答快 / 不卡壳"
// service levels a turn must meet to feel real-time. They are diagnostic only:
// nothing in the product path reads them, but a turn that breaches one is what
// a regression guard should catch before it ships.
export const VOICE_LATENCY_BUDGETS = {
  /** Silence detected → recognizer returns the first partial. */
  endpointMs: 1500,
  /** User stopped → first LLM token (time-to-first-byte of the reply). */
  ttfbMs: 1200,
  /** User stopped → first audible speech sample from the synthesizer. */
  firstAudioMs: 1500,
} as const

export type VoiceLatencyBudgetField = keyof typeof VOICE_LATENCY_BUDGETS

const BUDGET_TO_RECORD: Record<VoiceLatencyBudgetField, keyof VoiceTurnRecord> = {
  endpointMs: 'endpointMs',
  ttfbMs: 'ttfbMs',
  firstAudioMs: 'firstAudioMs',
}

export type VoiceLatencyBreach = {
  field: VoiceLatencyBudgetField
  actualMs: number
  budgetMs: number
}

/** Which SLA budgets a single turn blew past. A stall counts as a breach of
 * every measured budget so a hung turn never looks fast. */
export function voiceTurnBreaches(record: VoiceTurnRecord): VoiceLatencyBreach[] {
  const breaches: VoiceLatencyBreach[] = []
  for (const field of Object.keys(VOICE_LATENCY_BUDGETS) as VoiceLatencyBudgetField[]) {
    const budgetMs = VOICE_LATENCY_BUDGETS[field]
    const actual = record[BUDGET_TO_RECORD[field]]
    if (typeof actual !== 'number') continue
    if (record.outcome === 'stall' || actual > budgetMs) {
      breaches.push({ field, actualMs: actual, budgetMs })
    }
  }
  return breaches
}

export type VoiceLatencyStat = { p50?: number; p95?: number; max?: number; count: number }

function percentile(sorted: number[], p: number): number | undefined {
  if (sorted.length === 0) return undefined
  const rank = Math.ceil((p / 100) * sorted.length) - 1
  return sorted[Math.min(sorted.length - 1, Math.max(0, rank))]
}

/** p50/p95/max per latency field across the retained ring. Used by the
 * diagnostics panel and by tests that assert the pipeline stays within SLA. */
export function voiceTimingSummary(
  records: readonly VoiceTurnRecord[] = ring,
): Record<VoiceLatencyBudgetField, VoiceLatencyStat> {
  const out = {} as Record<VoiceLatencyBudgetField, VoiceLatencyStat>
  for (const field of Object.keys(VOICE_LATENCY_BUDGETS) as VoiceLatencyBudgetField[]) {
    const key = BUDGET_TO_RECORD[field]
    const values = records
      .map(r => r[key])
      .filter((v): v is number => typeof v === 'number')
      .sort((a, b) => a - b)
    out[field] = {
      p50: percentile(values, 50),
      p95: percentile(values, 95),
      max: values.length ? values[values.length - 1] : undefined,
      count: values.length,
    }
  }
  return out
}

// ---- Diagnostics panel view model (pure, so it can be unit tested) ----

export type VoiceTimingDisplayRow = {
  field: VoiceLatencyBudgetField
  label: string
  budgetMs: number
  p50?: number
  p95?: number
  max?: number
  count: number
  /** p95 within budget → healthy; over → the SLA is being missed. */
  healthy: boolean
}

const FIELD_LABELS: Record<VoiceLatencyBudgetField, string> = {
  endpointMs: '静音→首个识别',
  ttfbMs: '说完→首字回复',
  firstAudioMs: '说完→首个语音',
}

/** Turn the ring summary into display rows for the diagnostics panel. A field
 * with no samples yet is reported as healthy (nothing has missed its SLA). */
export function voiceTimingRows(records: readonly VoiceTurnRecord[] = ring): VoiceTimingDisplayRow[] {
  const summary = voiceTimingSummary(records)
  return (Object.keys(VOICE_LATENCY_BUDGETS) as VoiceLatencyBudgetField[]).map(field => {
    const stat = summary[field]
    const budgetMs = VOICE_LATENCY_BUDGETS[field]
    return {
      field,
      label: FIELD_LABELS[field],
      budgetMs,
      p50: stat.p50,
      p95: stat.p95,
      max: stat.max,
      count: stat.count,
      healthy: stat.p95 == null || stat.p95 <= budgetMs,
    }
  })
}

/** How many retained turns ended in a stall. */
export function voiceStallCount(records: readonly VoiceTurnRecord[] = ring): number {
  return records.reduce((n, r) => (r.outcome === 'stall' ? n + 1 : n), 0)
}
