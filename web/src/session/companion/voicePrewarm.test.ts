import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import {
  PREWARM_TEXT,
  buildPrewarmPayload,
  prewarmDefaultForEngine,
  setVoicePrewarmPref,
  shouldPrewarm,
  shouldPrewarmEngine,
  voicePrewarmEffective,
  voicePrewarmPref,
  type PrewarmInput,
} from './voicePrewarm'

const base: PrewarmInput = { engine: 'ref', voiceId: 'refpack:x.wav', refEndpoint: 'http://127.0.0.1:9880', rate: 0, volume: 80 }

describe('voicePrewarm flag + gating', () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => localStorage.clear())

  it('defaults to per-engine: local ref on, volc off', () => {
    expect(voicePrewarmPref()).toBe('default')
    expect(prewarmDefaultForEngine('ref')).toBe(true)
    expect(prewarmDefaultForEngine('volc')).toBe(false)
    // no explicit choice → follow the engine default
    expect(shouldPrewarm(base)).toBe(true)
    expect(shouldPrewarm({ ...base, engine: 'volc' })).toBe(false)
  })

  it('is only meaningful for cold-start engines', () => {
    expect(shouldPrewarmEngine('ref')).toBe(true)
    expect(shouldPrewarmEngine('volc')).toBe(true)
    expect(shouldPrewarmEngine('edge')).toBe(false)
    expect(shouldPrewarmEngine('sapi')).toBe(false)
  })

  it('explicit on/off overrides the per-engine default', () => {
    setVoicePrewarmPref('on')
    expect(voicePrewarmEffective('ref')).toBe(true)
    expect(voicePrewarmEffective('volc')).toBe(true)
    expect(shouldPrewarm(base)).toBe(true)
    expect(shouldPrewarm({ ...base, engine: 'volc' })).toBe(true)
    // still gated by engine
    expect(shouldPrewarm({ ...base, engine: 'edge' })).toBe(false)

    setVoicePrewarmPref('off')
    expect(voicePrewarmEffective('ref')).toBe(false)
    expect(shouldPrewarm(base)).toBe(false)

    // clearing the override restores the per-engine default
    setVoicePrewarmPref('default')
    expect(voicePrewarmPref()).toBe('default')
    expect(shouldPrewarm(base)).toBe(true)
  })

  it('builds a silent warmup payload that carries the ref endpoint only for ref', () => {
    const ref = buildPrewarmPayload(base)
    expect(ref.text).toBe(PREWARM_TEXT)
    expect(ref.volume).toBe(0)
    expect(ref.engine).toBe('ref')
    expect(ref.refEndpoint).toBe('http://127.0.0.1:9880')
    const volc = buildPrewarmPayload({ ...base, engine: 'volc' })
    expect(volc.refEndpoint).toBeUndefined()
  })
})
