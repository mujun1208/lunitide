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
  VOICE_PEAK,
  companionRecognitionLang,
  idleMeterLevel,
  shouldRestartStalledRecognition,
  speechEngineHint,
  shouldShowSpeechSetupHint,
  STALL_RESTART_AFTER_MS,
  overlayTranscript,
  pickRecognitionTranscript,
  shouldCommitIncomplete,
  INCOMPLETE_HOLD_MS,
  INCOMPLETE_HARD_MS,
  shouldBargeInOverPlayback,
  BARGE_IN_MIN_CHARS,
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
    expect(looksIncompleteUtterance('帮我打开桌面')).toBe(false)
    expect(endpointingForText('帮我打开桌面', speechProfile('normal'))).toEqual({
      stableMs: UTTERANCE_STABLE_MS,
      silenceMs: UTTERANCE_SILENCE_MS,
    })
  })
})

describe('shouldDeferCommit', () => {
  test('waits briefly before accepting a non-terminal phrase', () => {
    expect(shouldDeferCommit('帮我在桌面', MIN_UTTERANCE_MS - 1)).toBe(true)
    expect(shouldDeferCommit('帮我在桌面', MIN_UTTERANCE_MS)).toBe(false)
    expect(shouldDeferCommit('好的。', 100)).toBe(false)
  })
})

describe('shouldCommitIncomplete', () => {
  test('does not commit「你可以」on a short pause or while speech is still active', () => {
    expect(shouldCommitIncomplete({
      silentForMs: INCOMPLETE_SILENCE_MS,
      silenceMs: INCOMPLETE_SILENCE_MS,
      msSinceLastResult: 700,
      speechActive: false,
    })).toBe(false)
    expect(shouldCommitIncomplete({
      silentForMs: 2000,
      silenceMs: INCOMPLETE_SILENCE_MS,
      msSinceLastResult: INCOMPLETE_HOLD_MS,
      speechActive: true,
    })).toBe(false)
  })

  test('commits a fragment only after tokens go stale and the mic is quiet', () => {
    expect(shouldCommitIncomplete({
      silentForMs: INCOMPLETE_SILENCE_MS,
      silenceMs: INCOMPLETE_SILENCE_MS,
      msSinceLastResult: INCOMPLETE_HOLD_MS,
      speechActive: false,
    })).toBe(true)
    expect(shouldCommitIncomplete({
      silentForMs: 200,
      silenceMs: INCOMPLETE_SILENCE_MS,
      msSinceLastResult: INCOMPLETE_HARD_MS,
      speechActive: true,
    })).toBe(true)
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

  test('echo guard balances fast re-listen with speaker ring-out', () => {
    expect(ECHO_GUARD_MS).toBeGreaterThanOrEqual(80)
    expect(ECHO_GUARD_MS).toBeLessThanOrEqual(300)
  })
})

describe('speechProfile', () => {
  test('noisy mode tightens mic gating without cutting utterances too early', () => {
    const noisy = speechProfile('noisy')
    const normal = speechProfile('normal')
    expect(noisy.voicePeak).toBeGreaterThan(normal.voicePeak)
    expect(noisy.minVoiceHoldMs).toBeGreaterThan(0)
    expect(normal.minVoiceHoldMs).toBe(0)
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

describe('companionRecognitionLang', () => {
  test('maps Chinese navigator tags to zh-CN for Windows Speech Runtime', () => {
    expect(companionRecognitionLang('zh-Hans-CN')).toBe('zh-CN')
    expect(companionRecognitionLang('zh')).toBe('zh-CN')
    expect(companionRecognitionLang('en-US')).toBe('en-US')
    expect(companionRecognitionLang('')).toBe('zh-CN')
  })
})

describe('idleMeterLevel', () => {
  test('stays below the voice peak so fake rings never look like speech', () => {
    for (let tick = 0; tick < 80; tick += 1) {
      for (let index = 0; index < 12; index += 1) {
        expect(idleMeterLevel(tick, index)).toBeLessThan(VOICE_PEAK)
      }
    }
  })
})

describe('shouldRestartStalledRecognition', () => {
  test('never aborts an in-flight utterance or a fresh session', () => {
    expect(shouldRestartStalledRecognition({
      speechActive: true, hasText: false, held: false, restarting: false, msSinceStart: 8000,
    })).toBe(false)
    expect(shouldRestartStalledRecognition({
      speechActive: false, hasText: false, held: false, restarting: false, msSinceStart: 400,
    })).toBe(false)
    expect(shouldRestartStalledRecognition({
      speechActive: false, hasText: true, held: false, restarting: false, msSinceStart: 8000,
    })).toBe(false)
    expect(shouldRestartStalledRecognition({
      speechActive: false, hasText: false, held: false, restarting: false, msSinceStart: 1800,
    })).toBe(false)
    expect(shouldRestartStalledRecognition({
      speechActive: false, hasText: false, held: false, restarting: false, msSinceStart: 8000,
    })).toBe(true)
    expect(STALL_RESTART_AFTER_MS).toBeGreaterThanOrEqual(6000)
    expect(shouldRestartStalledRecognition({
      speechActive: false, hasText: false, held: false, restarting: false, msSinceStart: 1200, minSessionMs: 1200,
    })).toBe(true)
  })
})

describe('speechEngineHint', () => {
  test('does not treat aborted as a user-facing failure', () => {
    expect(speechEngineHint('aborted')).toBe('')
    expect(speechEngineHint('audio-capture')).toMatch(/麦克风/)
  })
})

describe('shouldShowSpeechSetupHint', () => {
  test('hides Windows setup copy once this visit has already recognized speech', () => {
    expect(shouldShowSpeechSetupHint({
      listening: true, hasInterim: false, listenSeconds: 25, heardThisVisit: false, hasUserRound: false,
    })).toBe(true)
    expect(shouldShowSpeechSetupHint({
      listening: true, hasInterim: false, listenSeconds: 25, heardThisVisit: true, hasUserRound: false,
    })).toBe(false)
    expect(shouldShowSpeechSetupHint({
      listening: true, hasInterim: false, listenSeconds: 25, heardThisVisit: false, hasUserRound: true,
    })).toBe(false)
    expect(shouldShowSpeechSetupHint({
      listening: true, hasInterim: true, listenSeconds: 25, heardThisVisit: false, hasUserRound: false,
    })).toBe(false)
  })
})

describe('overlayTranscript', () => {
  test('lets a longer interim replace a prefix final instead of duplicating it', () => {
    expect(overlayTranscript('今天合肥', '今天合肥的天气怎么样')).toBe('今天合肥的天气怎么样')
    expect(overlayTranscript('打开桌面', '打开桌面')).toBe('打开桌面')
    expect(overlayTranscript('你好', '')).toBe('你好')
  })
})

describe('pickRecognitionTranscript', () => {
  test('picks the highest-confidence alternative', () => {
    expect(pickRecognitionTranscript({
      0: { transcript: '今天合肥的天气怎么养', confidence: 0.4 },
      1: { transcript: '今天合肥的天气怎么样', confidence: 0.91 },
      length: 2,
    })).toBe('今天合肥的天气怎么样')
  })
})

describe('shouldBargeInOverPlayback', () => {
  const spoken = '今天合肥多云，气温二十六度，出门记得带把伞。'

  test('lets the user cut in while she is speaking', () => {
    expect(shouldBargeInOverPlayback('等一下', spoken)).toBe(true)
    expect(shouldBargeInOverPlayback('换个话题', spoken)).toBe(true)
  })

  test('ignores her own voice coming back through the speaker', () => {
    expect(shouldBargeInOverPlayback('气温二十六度', spoken)).toBe(false)
    expect(shouldBargeInOverPlayback('出门记得带把伞', spoken)).toBe(false)
  })

  test('needs real words, not a one-character blip', () => {
    expect(shouldBargeInOverPlayback('嗯', spoken)).toBe(false)
    expect(shouldBargeInOverPlayback('', spoken)).toBe(false)
    expect(BARGE_IN_MIN_CHARS).toBe(2)
  })
})
