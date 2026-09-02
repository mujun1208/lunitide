import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import {
  PREWARM_TEXT,
  buildPrewarmPayload,
  setVoicePrewarmEnabled,
  shouldPrewarm,
  shouldPrewarmEngine,
  voicePrewarmEnabled,
  type PrewarmInput,
} from './voicePrewarm'

const base: PrewarmInput = { engine: 'ref', voiceId: 'refpack:x.wav', refEndpoint: 'http://127.0.0.1:9880', rate: 0, volume: 80 }

describe('voicePrewarm flag + gating', () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => localStorage.clear())

  it('defaults to off', () => {
    expect(voicePrewarmEnabled()).toBe(false)
    expect(shouldPrewarm(base)).toBe(false)
  })

  it('is only meaningful for cold-start engines', () => {
    expect(shouldPrewarmEngine('ref')).toBe(true)
    expect(shouldPrewarmEngine('volc')).toBe(true)
    expect(shouldPrewarmEngine('edge')).toBe(false)
    expect(shouldPrewarmEngine('sapi')).toBe(false)
  })

  it('enables only when flag is on AND engine is cold-start', () => {
    setVoicePrewarmEnabled(true)
    expect(shouldPrewarm(base)).toBe(true)
    expect(shouldPrewarm({ ...base, engine: 'edge' })).toBe(false)
    setVoicePrewarmEnabled(false)
    expect(shouldPrewarm(base)).toBe(false)
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
