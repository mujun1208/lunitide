// speech.test.ts pins Doubao-style utterance endpointing: commit after
// analyser silence or a stable Windows SR transcript, never on a short
// pause or on an empty buffer.
import { describe, expect, test } from 'vitest'
import { UTTERANCE_SILENCE_MS, UTTERANCE_STABLE_MS, BARGE_IN_HOLD_MS, shouldCommitUtterance, shouldCommitStable, shouldBargeIn, isPermanentSpeechError } from './speech'

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

describe('shouldCommitStable', () => {
  test('commits once transcript stops changing', () => {
    expect(shouldCommitStable(true, UTTERANCE_STABLE_MS - 1)).toBe(false)
    expect(shouldCommitStable(true, UTTERANCE_STABLE_MS)).toBe(true)
  })

  test('does not wait on analyser silence: Windows SR re-fires the same interim', () => {
    expect(shouldCommitUtterance(true, 0)).toBe(false)
    expect(shouldCommitStable(true, UTTERANCE_STABLE_MS)).toBe(true)
  })
})

describe('shouldBargeIn', () => {
  test('requires sustained voice once text is present', () => {
    expect(shouldBargeIn(true, 0)).toBe(false)
    expect(shouldBargeIn(true, BARGE_IN_HOLD_MS - 1)).toBe(false)
    expect(shouldBargeIn(true, BARGE_IN_HOLD_MS)).toBe(true)
  })

  test('never fires without assembled text', () => {
    expect(shouldBargeIn(false, BARGE_IN_HOLD_MS)).toBe(false)
  })
})

describe('isPermanentSpeechError', () => {
  test('treats permission and language failures as fatal', () => {
    expect(isPermanentSpeechError('not-allowed')).toBe(true)
    expect(isPermanentSpeechError('service-not-allowed')).toBe(true)
    expect(isPermanentSpeechError('language-not-supported')).toBe(true)
  })

  test('treats network and silence as restartable', () => {
    expect(isPermanentSpeechError('network')).toBe(false)
    expect(isPermanentSpeechError('no-speech')).toBe(false)
    expect(isPermanentSpeechError('aborted')).toBe(false)
    expect(isPermanentSpeechError('audio-capture')).toBe(false)
    expect(isPermanentSpeechError(undefined)).toBe(false)
  })
})
