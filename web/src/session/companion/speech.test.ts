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
  ECHO_GUARD_MS,
  shouldCommitUtterance,
  shouldCommitStable,
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
  shouldReplaceSilentRecognizer,
  VOICE_WITHOUT_TEXT_MS,
  VOICE_RESTART_RESULT_MS,
  STALL_RESTART_GAP_MS,
  turnEnded,
  TURN_END_SILENCE_MS,
  TURN_END_INCOMPLETE_SILENCE_MS,
  TURN_END_TEXT_SETTLE_MS,
  FORCE_COMMIT_MS,
  STUCK_TRANSCRIPT_MS,
  shouldForceCommitUtterance,
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

describe('shouldHoldRecognition', () => {
  test('holds for the whole reply, and past it until the speaker rings out', () => {
    // Unconditional during playback: the microphone cannot end her turn while
    // she is speaking, only the 打断 button can.
    expect(shouldHoldRecognition(true, 0, 1000)).toBe(true)
    expect(shouldHoldRecognition(false, 800, 700)).toBe(true)
    expect(shouldHoldRecognition(false, 800, 800)).toBe(false)
  })

  test('echo guard balances fast re-listen with speaker ring-out', () => {
    expect(ECHO_GUARD_MS).toBeGreaterThanOrEqual(300)
    expect(ECHO_GUARD_MS).toBeLessThanOrEqual(600)
  })
})

describe('speechProfile', () => {
  test('noisy mode tightens mic gating without cutting utterances too early', () => {
    const noisy = speechProfile('noisy')
    const normal = speechProfile('normal')
    expect(noisy.voicePeak).toBeGreaterThan(normal.voicePeak)
    expect(noisy.utteranceSilenceMs).toBeLessThan(normal.utteranceSilenceMs)
    expect(noisy.utteranceStableMs).toBeLessThan(normal.utteranceStableMs)
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

  test('prefers a longer alternative that extends a higher-confidence fragment', () => {
    expect(pickRecognitionTranscript({
      0: { transcript: '文档联系', confidence: 0.92 },
      1: { transcript: '文档联系电话', confidence: 0.41 },
      length: 2,
    })).toBe('文档联系电话')
  })
})

describe('replacing a recognizer that is hearing speech but returning nothing', () => {
  const silent = {
    hasText: false,
    voiceForMs: VOICE_WITHOUT_TEXT_MS + 50,
    msSinceLastResult: VOICE_RESTART_RESULT_MS + 50,
    msSinceLastRestart: STALL_RESTART_GAP_MS + 50,
  }

  test('replaces it, whatever the session claims about being alive', () => {
    // The bug this exists for: the test used to require the session to have
    // already reported itself dead, so a Windows session that started,
    // reported audio and then never returned a token was left in place. The
    // user talked to it, saw nothing, and waited out an eight-second stall
    // timer before anything was repaired.
    expect(shouldReplaceSilentRecognizer(silent)).toBe(true)
  })

  test('leaves a recognizer that has produced anything at all', () => {
    // Restarting mid-utterance discards the rest of the sentence, which is
    // why an empty transcript is part of the condition rather than a detail.
    expect(shouldReplaceSilentRecognizer({ ...silent, hasText: true })).toBe(false)
  })

  test('gives a healthy engine time to return its first token', () => {
    expect(shouldReplaceSilentRecognizer({ ...silent, voiceForMs: 200 })).toBe(false)
    expect(shouldReplaceSilentRecognizer({ ...silent, msSinceLastResult: 300 })).toBe(false)
  })

  test('does not restart faster than the engine can survive', () => {
    expect(shouldReplaceSilentRecognizer({ ...silent, msSinceLastRestart: 100 })).toBe(false)
  })

  test('acts well before the stall timer it used to depend on', () => {
    expect(VOICE_RESTART_RESULT_MS).toBeLessThan(STALL_RESTART_AFTER_MS)
    expect(VOICE_WITHOUT_TEXT_MS).toBeLessThan(STALL_RESTART_AFTER_MS)
  })
})

describe('when the user has finished speaking', () => {
  const stopped = {
    speechActive: false,
    silentForMs: TURN_END_SILENCE_MS + 50,
    msSinceLastResult: TURN_END_TEXT_SETTLE_MS + 50,
    incomplete: false,
  }

  test('the product window is 1.2s after they stop talking', () => {
    expect(TURN_END_SILENCE_MS).toBe(1200)
    expect(TURN_END_INCOMPLETE_SILENCE_MS).toBe(1500)
    expect(TURN_END_SILENCE_MS).toBeLessThanOrEqual(TURN_END_INCOMPLETE_SILENCE_MS)
    expect(FORCE_COMMIT_MS).toBeGreaterThan(TURN_END_SILENCE_MS)
    expect(FORCE_COMMIT_MS).toBeLessThan(2700)
  })

  test('does not treat a 50–100ms pause as the end of the turn', () => {
    expect(turnEnded({ ...stopped, silentForMs: 50 })).toBe(false)
    expect(turnEnded({ ...stopped, silentForMs: 100 })).toBe(false)
    expect(turnEnded({ ...stopped, silentForMs: 400 })).toBe(false)
    expect(turnEnded({ ...stopped, silentForMs: 1100 })).toBe(false)
  })

  test('ends once the room is quiet and the transcript has settled', () => {
    expect(turnEnded(stopped)).toBe(true)
    expect(turnEnded({ ...stopped, silentForMs: TURN_END_SILENCE_MS })).toBe(true)
  })

  test('does not end while the speaker is still going', () => {
    // The failure this exists for: the recognizer stops emitting mid-sentence,
    // the old rules read that as the end of the turn, and 「你好月汐」 was
    // answered as 「你好」.
    expect(turnEnded({ ...stopped, speechActive: true })).toBe(false)
    expect(turnEnded({ ...stopped, silentForMs: 400 })).toBe(false)
  })

  test('does not end while the transcript is still growing', () => {
    expect(turnEnded({ ...stopped, msSinceLastResult: 100 })).toBe(false)
  })

  test('unfinished phrases still wait for a real stop, then end at 1.2s', () => {
    const incomplete = { ...stopped, incomplete: true, silentForMs: 400 }
    expect(turnEnded(incomplete)).toBe(false)
    expect(turnEnded({ ...incomplete, silentForMs: 1100 })).toBe(false)
    expect(turnEnded({ ...incomplete, silentForMs: TURN_END_INCOMPLETE_SILENCE_MS })).toBe(true)
  })

  test('still ends on a microphone whose level never reaches the speech gate', () => {
    // A quiet device, or noise suppression that flattens everything. Gating
    // the turn on evidence the hardware cannot produce would leave that user
    // waiting forever, so the transcript going quiet stands in for it.
    expect(turnEnded({ ...stopped, silentForMs: undefined })).toBe(false)
    expect(turnEnded({ ...stopped, silentForMs: undefined, msSinceLastResult: TURN_END_SILENCE_MS + 50 })).toBe(true)
  })

  test('meeting notes wait longer than companion turn-taking', () => {
    expect(turnEnded({ ...stopped, silentForMs: TURN_END_SILENCE_MS + 50 })).toBe(true)
    expect(turnEnded({
      ...stopped,
      silentForMs: TURN_END_SILENCE_MS + 50,
      silenceMs: 2000,
      incompleteSilenceMs: 2500,
    })).toBe(false)
    expect(turnEnded({
      ...stopped,
      silentForMs: 2050,
      silenceMs: 2000,
      incompleteSilenceMs: 2500,
    })).toBe(true)
  })

  test('does not treat「打开网」as finished after a short pause', () => {
    expect(looksIncompleteUtterance('你帮我打开网')).toBe(true)
    expect(looksIncompleteUtterance('打开网')).toBe(true)
    expect(turnEnded({ ...stopped, incomplete: true, silentForMs: 400 })).toBe(false)
    expect(shouldForceCommitUtterance({
      speechActive: false,
      silentForMs: 400,
      textStableForMs: 400,
      incomplete: true,
    })).toBe(false)
  })

  test('force-commits a stuck complete caption after 1.2s even if the analyser still hears room noise', () => {
    expect(shouldForceCommitUtterance({
      speechActive: true,
      silentForMs: 0,
      textStableForMs: 400,
      incomplete: false,
    })).toBe(false)
    expect(shouldForceCommitUtterance({
      speechActive: true,
      silentForMs: 0,
      textStableForMs: STUCK_TRANSCRIPT_MS,
      incomplete: false,
    })).toBe(true)
    expect(shouldForceCommitUtterance({
      speechActive: false,
      silentForMs: TURN_END_INCOMPLETE_SILENCE_MS,
      textStableForMs: STUCK_TRANSCRIPT_MS,
      incomplete: true,
    })).toBe(true)
  })

  test('incomplete endings wait 1.5s before commit', () => {
    expect(TURN_END_INCOMPLETE_SILENCE_MS).toBe(1500)
  })

  test('does not hard-commit an incomplete caption on a short breath', () => {
    expect(shouldForceCommitUtterance({
      speechActive: true,
      silentForMs: 0,
      textStableForMs: 400,
      incomplete: true,
    })).toBe(false)
    expect(shouldForceCommitUtterance({
      speechActive: true,
      silentForMs: 0,
      textStableForMs: STUCK_TRANSCRIPT_MS,
      incomplete: true,
    })).toBe(false)
    expect(shouldForceCommitUtterance({
      speechActive: true,
      silentForMs: 0,
      textStableForMs: INCOMPLETE_HARD_MS,
      incomplete: true,
    })).toBe(true)
    expect(shouldForceCommitUtterance({
      speechActive: false,
      silentForMs: TURN_END_INCOMPLETE_SILENCE_MS,
      textStableForMs: INCOMPLETE_HARD_MS,
      incomplete: true,
    })).toBe(true)
  })
})

describe('who may end her turn', () => {
  test('recognition is held for all of it, thinking and speaking alike', () => {
    // The two ways back to the user's turn are the 打断 button and her
    // finishing. Neither is a transcript, so recognition is simply held —
    // there is no threshold here to tune because there is no decision left
    // to make.
    expect(shouldHoldRecognition(true, 0, 1000)).toBe(true)
  })
})
