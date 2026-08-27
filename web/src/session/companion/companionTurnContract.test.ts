// companionTurnContract.test.ts is the CI-style bar for the voice turn
// the user asked for: 汉字准、说完 1.2–1.5s 才截句、字幕是用户原话、
// 指令结束不说「我做完了」、首 token 不垫思考。Copied from the existing
// speech.test.ts / companionText.test.ts assertion style — functions and
// windows, not a live microphone.
import { describe, expect, test } from 'vitest'
import {
  FORCE_COMMIT_MS,
  TURN_END_INCOMPLETE_SILENCE_MS,
  TURN_END_SILENCE_MS,
  TURN_END_TEXT_SETTLE_MS,
  turnEnded,
} from './speech'
import {
  COMPANION_FIRST_TOKEN_CONNECTING_MS,
  COMPANION_FIRST_TOKEN_STREAMING_MS,
  cleanUserTranscript,
  companionReplyStallMs,
  looksLikeOmniPersonaCaption,
  looksLikePlaybackEcho,
  shouldAcceptUserTranscript,
  stripTaskDonePhrases,
} from './companionText'
import { omniPersonaCaption } from './voicePersonas'

const listening = {
  state: 'listening' as const,
  lastSpoken: '今晚是满月，适合抬头。',
  lastAssistant: '今晚是满月，适合抬头。',
}

describe('汉字识别质量', () => {
  test('first-round and later-round homophones become the user’s words', () => {
    expect(cleanUserTranscript('你好岳西')).toBe('你好月汐')
    expect(cleanUserTranscript('帮我打开店面文件')).toBe('帮我打开桌面文件')
    expect(cleanUserTranscript('打开气水音乐')).toBe('打开汽水音乐')
    expect(cleanUserTranscript('下一句，打开悦溪的店面')).toBe('下一句，打开月汐的桌面')
  })

  test('her TTS coming back through the mic is not the next question', () => {
    expect(looksLikePlaybackEcho('今晚是满月适合抬头', listening.lastSpoken)).toBe(true)
    expect(shouldAcceptUserTranscript({ ...listening, text: '今晚是满月适合抬头' })).toBe(false)
    expect(shouldAcceptUserTranscript({ ...listening, text: '下一句' })).toBe(true)
  })

  test('clone labels never become a dialogue round', () => {
    expect(omniPersonaCaption('refpack:优质台湾腔.wav')).toBe('人生：优质台湾腔')
    expect(looksLikeOmniPersonaCaption('人生：优质台湾腔')).toBe(true)
    expect(shouldAcceptUserTranscript({ ...listening, text: '人生：优质台湾腔' })).toBe(false)
    expect(shouldAcceptUserTranscript({ ...listening, text: '月汐 / 人生：优质台湾腔' })).toBe(false)
  })
})

describe('识别速度', () => {
  const settled = {
    speechActive: false,
    msSinceLastResult: TURN_END_TEXT_SETTLE_MS + 50,
    incomplete: false,
  }

  test('commit sits in the 1.2–1.5s band, not 50–100ms and not 2.7–3.5s', () => {
    expect(turnEnded({ ...settled, silentForMs: 80 })).toBe(false)
    expect(turnEnded({ ...settled, silentForMs: 400 })).toBe(false)
    expect(turnEnded({ ...settled, silentForMs: 1100 })).toBe(false)
    expect(turnEnded({ ...settled, silentForMs: TURN_END_SILENCE_MS })).toBe(true)
    expect(TURN_END_SILENCE_MS).toBeGreaterThanOrEqual(1200)
    expect(TURN_END_INCOMPLETE_SILENCE_MS).toBeLessThanOrEqual(1500)
    expect(FORCE_COMMIT_MS).toBeGreaterThan(TURN_END_INCOMPLETE_SILENCE_MS)
    expect(FORCE_COMMIT_MS).toBeLessThan(2700)
  })

  test('incomplete-looking phrases still end by 1.5s of quiet', () => {
    const incomplete = { ...settled, incomplete: true }
    expect(turnEnded({ ...incomplete, silentForMs: TURN_END_SILENCE_MS })).toBe(false)
    expect(turnEnded({ ...incomplete, silentForMs: TURN_END_INCOMPLETE_SILENCE_MS })).toBe(true)
  })
})

describe('识别返回', () => {
  test('a new user sentence is accepted; her last line is dropped', () => {
    expect(shouldAcceptUserTranscript({ ...listening, text: '帮我打开桌面' })).toBe(true)
    expect(shouldAcceptUserTranscript({ ...listening, text: '适合抬头' })).toBe(false)
  })
})

describe('回答质量', () => {
  test('machine self-reports are stripped; a real result stays', () => {
    expect(stripTaskDonePhrases('我做完了')).toBe('')
    expect(stripTaskDonePhrases('我已经做完了。')).toBe('')
    expect(stripTaskDonePhrases('任务已完成')).toBe('')
    expect(stripTaskDonePhrases('文件夹建好了。我已经做完了。')).toBe('文件夹建好了。')
  })
})

describe('回答速度', () => {
  test('first-token watchdog stays on the live-stream path, not a thinking pad', () => {
    expect(companionReplyStallMs(true, false)).toBe(COMPANION_FIRST_TOKEN_STREAMING_MS)
    expect(companionReplyStallMs(false, false)).toBe(COMPANION_FIRST_TOKEN_CONNECTING_MS)
    expect(COMPANION_FIRST_TOKEN_STREAMING_MS).toBeGreaterThanOrEqual(8_000)
  })
})
