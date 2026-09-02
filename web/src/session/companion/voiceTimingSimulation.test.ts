// Simulated real-machine latency runs.
//
// The user cannot always sit at the mic, so this harness models the three
// pipelines with the latency magnitudes documented in the codebase and the
// voice map report, feeds them through the same voiceTiming summary the live
// diagnostics panel uses, and prints p50/p95/max + SLA verdicts. It is seeded,
// so the numbers are reproducible and the assertions guard the expected impact
// of V3 (prewarm) and V2 (optimistic final).
//
// Latency sources (approx, from code + report):
//  - endpoint (silence→first partial): streaming ASR, ~250-380ms all modes
//  - local refiner adds ~400-750ms before the final text is sent to the LLM
//  - GPT-SoVITS cold start ~8s on the first synth; warm ~0.9-1.4s
//  - volc talk realtime ~0.5-0.9s; edge warm-pool TTS ~0.7-1.0s
import { describe, expect, it } from 'vitest'
import {
  VOICE_LATENCY_BUDGETS,
  voiceTimingRows,
  voiceTimingSummary,
  type VoiceTurnRecord,
} from './voiceTiming'

// Deterministic PRNG so simulated numbers are stable across runs.
function mulberry32(seed: number): () => number {
  let a = seed
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

type Range = [number, number]
const pick = (rng: () => number, [lo, hi]: Range): number => Math.round(lo + rng() * (hi - lo))

type PipelineModel = {
  path: string
  endpoint: Range
  ttfb: Range
  /** firstAudio may depend on turn index (cold start on turn 0). */
  firstAudio: (turnIndex: number) => Range
}

function simulate(model: PipelineModel, turns: number, seed: number): VoiceTurnRecord[] {
  const rng = mulberry32(seed)
  const out: VoiceTurnRecord[] = []
  for (let i = 0; i < turns; i += 1) {
    out.push({
      path: model.path,
      endpointMs: pick(rng, model.endpoint),
      ttfbMs: pick(rng, model.ttfb),
      firstAudioMs: pick(rng, model.firstAudio(i)),
      outcome: 'ok',
    })
  }
  return out
}

const TURNS = 24
const SEED = 0x9e3779b9

// ---- Pipeline models ----
const cloud: PipelineModel = { path: 'cloud', endpoint: [250, 380], ttfb: [600, 900], firstAudio: () => [700, 1000] }
const volcTalk: PipelineModel = { path: 'volc-talk', endpoint: [250, 360], ttfb: [500, 850], firstAudio: () => [500, 950] }
// Local, current shipped behaviour: refiner delays the final text (higher ttfb)
// and SoVITS cold-starts on the first synth of the visit.
const localBaseline: PipelineModel = {
  path: 'local-baseline',
  endpoint: [250, 380],
  ttfb: [1050, 1500],
  firstAudio: turn => (turn === 0 ? [7800, 8200] : [950, 1400]),
}
// V3 prewarm on: no cold-start cliff, every turn is warm.
const localPrewarm: PipelineModel = {
  path: 'local+V3',
  endpoint: [250, 380],
  ttfb: [1050, 1500],
  firstAudio: () => [900, 1350],
}
// V2 optimistic final on (with prewarm): streaming final skips the refiner wait.
const localV2: PipelineModel = {
  path: 'local+V2+V3',
  endpoint: [250, 380],
  ttfb: [650, 950],
  firstAudio: () => [900, 1350],
}

function verdict(rows: ReturnType<typeof voiceTimingRows>): string {
  return rows.map(r => `${r.label} p50=${r.p50 ?? '-'} p95=${r.p95 ?? '-'} max=${r.max ?? '-'} (≤${r.budgetMs}) ${r.count === 0 ? '-' : r.healthy ? 'OK' : 'MISS'}`).join('\n    ')
}

describe('voice latency simulation (seeded, real-machine model)', () => {
  const scenarios = { cloud, volcTalk, localBaseline, localPrewarm, localV2 }

  it('prints a p50/p95/max SLA table for every pipeline', () => {
    const lines: string[] = ['', '=== Simulated voice latency (24 turns/scenario) ===', `budgets: endpoint≤${VOICE_LATENCY_BUDGETS.endpointMs} ttfb≤${VOICE_LATENCY_BUDGETS.ttfbMs} firstAudio≤${VOICE_LATENCY_BUDGETS.firstAudioMs} (ms)`]
    for (const [name, model] of Object.entries(scenarios)) {
      const rows = voiceTimingRows(simulate(model, TURNS, SEED))
      lines.push(`  [${name}]`, `    ${verdict(rows)}`)
    }
    // eslint-disable-next-line no-console
    console.log(lines.join('\n'))
    expect(lines.length).toBeGreaterThan(0)
  })

  it('local baseline suffers a cold-start cliff in firstAudio max', () => {
    const summary = voiceTimingSummary(simulate(localBaseline, TURNS, SEED))
    expect(summary.firstAudioMs.max ?? 0).toBeGreaterThanOrEqual(7000)
  })

  it('V3 prewarm removes the cold-start cliff (firstAudio max under budget)', () => {
    const rows = voiceTimingRows(simulate(localPrewarm, TURNS, SEED))
    const fa = rows.find(r => r.field === 'firstAudioMs')!
    expect(fa.max ?? 0).toBeLessThanOrEqual(VOICE_LATENCY_BUDGETS.firstAudioMs)
    expect(fa.healthy).toBe(true)
  })

  it('local baseline misses the first-token budget; V2 brings it under', () => {
    const baseTtfb = voiceTimingRows(simulate(localBaseline, TURNS, SEED)).find(r => r.field === 'ttfbMs')!
    const v2Ttfb = voiceTimingRows(simulate(localV2, TURNS, SEED)).find(r => r.field === 'ttfbMs')!
    expect(baseTtfb.healthy).toBe(false)
    expect(v2Ttfb.healthy).toBe(true)
    expect((v2Ttfb.p95 ?? Infinity)).toBeLessThan(baseTtfb.p95 ?? 0)
  })

  it('cloud and volc-talk already meet every budget', () => {
    for (const model of [cloud, volcTalk]) {
      const rows = voiceTimingRows(simulate(model, TURNS, SEED))
      expect(rows.every(r => r.healthy)).toBe(true)
    }
  })
})
