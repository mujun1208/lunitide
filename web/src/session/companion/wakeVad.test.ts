import { describe, expect, test } from 'vitest'
import { classifyWakeEnergy, shouldAcceptWake } from './wakeVad'

function series(value: number, count: number): number[] {
  return Array.from({ length: count }, () => value)
}

describe('classifyWakeEnergy', () => {
  test('few frames stay quiet rather than guessing speech', () => {
    expect(classifyWakeEnergy([0.2, 0.1])).toEqual({
      speechLikely: false,
      playbackLikely: false,
      tooQuiet: true,
    })
  })

  test('flat loud energy looks like speaker playback', () => {
    const snap = classifyWakeEnergy(series(0.2, 25).map((v, i) => v + (i % 2 ? 0.008 : -0.008)))
    expect(snap.tooQuiet).toBe(false)
    expect(snap.playbackLikely).toBe(true)
    expect(snap.speechLikely).toBe(false)
  })

  test('syllable-scale swings look like live address', () => {
    const peaks = Array.from({ length: 25 }, (_, i) => (i % 4 < 2 ? 0.16 : 0.03))
    const snap = classifyWakeEnergy(peaks)
    expect(snap.tooQuiet).toBe(false)
    expect(snap.speechLikely).toBe(true)
    expect(snap.playbackLikely).toBe(false)
  })
})

describe('shouldAcceptWake', () => {
  const speech = { speechLikely: true, playbackLikely: false, tooQuiet: false }
  const playback = { speechLikely: false, playbackLikely: true, tooQuiet: false }
  const quiet = { speechLikely: false, playbackLikely: false, tooQuiet: true }

  test('vad off never blocks a real match', () => {
    expect(shouldAcceptWake('phrase', playback, false)).toBe(true)
    expect(shouldAcceptWake('name', quiet, false)).toBe(true)
    expect(shouldAcceptWake('none', speech, true)).toBe(false)
  })

  test('missing analyser fails open so tests and old WebView still wake', () => {
    expect(shouldAcceptWake('phrase', null, true)).toBe(true)
    expect(shouldAcceptWake('name', null, true)).toBe(true)
  })

  test('playback rejects both phrase and name hits', () => {
    expect(shouldAcceptWake('phrase', playback, true)).toBe(false)
    expect(shouldAcceptWake('name', playback, true)).toBe(false)
  })

  test('name-only needs live speech; a greeted phrase may pass on unknown quiet', () => {
    expect(shouldAcceptWake('name', quiet, true)).toBe(false)
    expect(shouldAcceptWake('name', speech, true)).toBe(true)
    expect(shouldAcceptWake('phrase', quiet, true)).toBe(true)
    expect(shouldAcceptWake('phrase', speech, true)).toBe(true)
  })
})
