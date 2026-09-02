import { describe, expect, it, beforeEach } from 'vitest'
import {
  VOICE_LATENCY_BUDGETS,
  finishVoiceTurn,
  markVoiceTiming,
  peekVoiceTimings,
  resetVoiceTimings,
  startVoiceTurn,
  voiceStallCount,
  voiceTimingRows,
  voiceTimingSummary,
  voiceTurnBreaches,
  type VoiceTurnRecord,
} from './voiceTiming'

describe('voiceTiming SLA guard', () => {
  beforeEach(() => resetVoiceTimings())

  it('records the fields marked during a turn', () => {
    startVoiceTurn('local')
    markVoiceTiming('endpoint', performance.now())
    markVoiceTiming('ttfb', performance.now())
    const record = finishVoiceTurn('ok')
    expect(record?.path).toBe('local')
    expect(record?.endpointMs).toBeGreaterThanOrEqual(0)
    expect(record?.ttfbMs).toBeGreaterThanOrEqual(0)
    expect(peekVoiceTimings()).toHaveLength(1)
  })

  it('flags a turn that blows the first-audio budget', () => {
    const record: VoiceTurnRecord = {
      path: 'local',
      firstAudioMs: VOICE_LATENCY_BUDGETS.firstAudioMs + 500,
      ttfbMs: 200,
      outcome: 'ok',
    }
    const breaches = voiceTurnBreaches(record)
    expect(breaches.map(b => b.field)).toContain('firstAudioMs')
    expect(breaches.map(b => b.field)).not.toContain('ttfbMs')
  })

  it('treats a stall as a breach of every measured budget', () => {
    const record: VoiceTurnRecord = {
      path: 'volc',
      ttfbMs: 100,
      firstAudioMs: 100,
      outcome: 'stall',
    }
    const fields = voiceTurnBreaches(record).map(b => b.field)
    expect(fields).toContain('ttfbMs')
    expect(fields).toContain('firstAudioMs')
  })

  it('does not flag unmeasured fields', () => {
    const record: VoiceTurnRecord = { path: 'cloud', outcome: 'ok' }
    expect(voiceTurnBreaches(record)).toHaveLength(0)
  })

  it('summarizes p50/p95/max across the ring', () => {
    const records: VoiceTurnRecord[] = [100, 200, 300, 400, 900].map(ms => ({
      path: 'local',
      ttfbMs: ms,
      outcome: 'ok' as const,
    }))
    const summary = voiceTimingSummary(records)
    expect(summary.ttfbMs.count).toBe(5)
    expect(summary.ttfbMs.max).toBe(900)
    expect(summary.ttfbMs.p50).toBe(300)
    expect(summary.ttfbMs.p95).toBe(900)
  })

  it('reports empty rows as healthy with zero samples', () => {
    const rows = voiceTimingRows([])
    expect(rows).toHaveLength(3)
    expect(rows.every(r => r.count === 0 && r.healthy)).toBe(true)
    expect(rows.map(r => r.field)).toEqual(['endpointMs', 'ttfbMs', 'firstAudioMs'])
  })

  it('marks a field unhealthy once p95 exceeds its budget', () => {
    const overBudget = VOICE_LATENCY_BUDGETS.ttfbMs + 400
    const records: VoiceTurnRecord[] = [overBudget, overBudget, overBudget].map(ms => ({
      path: 'local',
      ttfbMs: ms,
      outcome: 'ok' as const,
    }))
    const ttfbRow = voiceTimingRows(records).find(r => r.field === 'ttfbMs')
    expect(ttfbRow?.healthy).toBe(false)
    expect(ttfbRow?.p95).toBe(overBudget)
  })

  it('counts stalled turns', () => {
    const records: VoiceTurnRecord[] = [
      { path: 'local', outcome: 'ok' },
      { path: 'volc', outcome: 'stall' },
      { path: 'local', outcome: 'stall' },
    ]
    expect(voiceStallCount(records)).toBe(2)
  })
})
