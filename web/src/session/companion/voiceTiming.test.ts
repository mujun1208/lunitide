import { afterEach, describe, expect, test } from 'vitest'
import { finishVoiceTurn, markVoiceTiming, peekVoiceTimings, resetVoiceTimings, startVoiceTurn } from './voiceTiming'

afterEach(() => {
  resetVoiceTimings()
})

describe('voiceTiming', () => {
  test('records first marks only and flushes a turn', () => {
    startVoiceTurn('volc')
    markVoiceTiming('endpoint')
    markVoiceTiming('ttfb')
    markVoiceTiming('ttfb')
    const record = finishVoiceTurn('ok')
    expect(record?.path).toBe('volc')
    expect(record?.endpointMs).toBeGreaterThanOrEqual(0)
    expect(record?.ttfbMs).toBeGreaterThanOrEqual(0)
    expect(peekVoiceTimings()).toHaveLength(1)
  })
})
