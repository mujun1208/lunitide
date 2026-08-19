// speech.test.ts pins Doubao-style utterance endpointing: commit only
// after ~400ms of silence once we already have text, never on a short
// pause or on an empty buffer.
import { describe, expect, test } from 'vitest'
import { UTTERANCE_SILENCE_MS, shouldCommitUtterance } from './speech'

describe('shouldCommitUtterance', () => {
  test('waits for the silence window once speech is present', () => {
    expect(shouldCommitUtterance(true, 0)).toBe(false)
    expect(shouldCommitUtterance(true, UTTERANCE_SILENCE_MS - 1)).toBe(false)
    expect(shouldCommitUtterance(true, UTTERANCE_SILENCE_MS)).toBe(true)
    expect(shouldCommitUtterance(true, 800)).toBe(true)
  })

  test('never commits silence without assembled text', () => {
    expect(shouldCommitUtterance(false, UTTERANCE_SILENCE_MS)).toBe(false)
    expect(shouldCommitUtterance(false, 2000)).toBe(false)
  })

  test('accepts a tighter window for mid-utterance checks', () => {
    expect(shouldCommitUtterance(true, 280, 280)).toBe(true)
    expect(shouldCommitUtterance(true, 279, 280)).toBe(false)
  })
})
