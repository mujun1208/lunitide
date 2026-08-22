// speech.test.ts pins Doubao-style utterance endpointing: commit after
// analyser silence or a stable Windows SR transcript, never on a short
// pause or on an empty buffer.
import { describe, expect, test } from 'vitest'
import {
  UTTERANCE_SILENCE_MS,
  UTTERANCE_STABLE_MS,
  INCOMPLETE_STABLE_MS,
  INCOMPLETE_SILENCE_MS,
  MIN_UTTERANCE_MS,
  BARGE_IN_HOLD_MS,
  ECHO_GUARD_MS,
  shouldCommitUtterance,
  shouldCommitStable,
  shouldBargeIn,
  shouldHoldRecognition,
  shouldDeferCommit,
  endpointingForText,
  isPermanentSpeechError,
  speechProfile,
} from './speech'
import { looksIncompleteUtterance } from './companionText'

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
    expect(shouldCommitUtterance(true, 620, 620)).toBe(true)
    expect(shouldCommitUtterance(true, 619, 620)).toBe(false)
  })
})

describe('endpointingForText', () => {
  test('extends windows for incomplete command tails', () => {
    expect(looksIncompleteUtterance('帮我在桌面儿')).toBe(true)
    expect(endpointingForText('帮我在桌面儿', speechProfile('normal'))).toEqual({
      stableMs: INCOMPLETE_STABLE_MS,
      silenceMs: INCOMPLETE_SILENCE_MS,
    })
    expect(endpointingForText('帮我打开桌面。', speechProfile('normal'))).toEqual({
      stableMs: UTTERANCE_STABLE_MS,
      silenceMs: UTTERANCE_SILENCE_MS,
    })
  })
})

describe('shouldDeferCommit', () => {
  test('waits briefly before accepting a non-terminal phrase', () => {
    expect(shouldDeferCommit('帮我在桌面', 200)).toBe(true)
    expect(shouldDeferCommit('帮我在桌面', MIN_UTTERANCE_MS)).toBe(false)
    expect(shouldDeferCommit('好的。', 100)).toBe(false)
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

describe('shouldHoldRecognition', () => {
  test('holds during playback unless playback barge-in is enabled', () => {
    expect(shouldHoldRecognition(true, 0, 1000)).toBe(true)
    expect(shouldHoldRecognition(true, 0, 1000, true)).toBe(false)
    expect(shouldHoldRecognition(false, 800, 700)).toBe(true)
    expect(shouldHoldRecognition(false, 800, 800)).toBe(false)
  })

  test('echo guard is long enough for speaker ring-out', () => {
    expect(ECHO_GUARD_MS).toBeGreaterThanOrEqual(500)
  })
})

describe('speechProfile', () => {
  test('noisy mode tightens mic gating without cutting utterances too early', () => {
    const noisy = speechProfile('noisy')
    const normal = speechProfile('normal')
    expect(noisy.voicePeak).toBeGreaterThan(normal.voicePeak)
    expect(noisy.minVoiceHoldMs).toBeGreaterThan(0)
    expect(normal.minVoiceHoldMs).toBeGreaterThan(0)
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
