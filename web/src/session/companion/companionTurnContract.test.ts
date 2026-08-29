// companionTurnContract.test.ts is the CI-style bar for the voice turn
// the user asked for: 汉字准、说完 1.2s 才截句、字幕是用户原话、
// 指令结束不说「我做完了」、首 token 不垫思考、多轮不卡死。Copied from
// the existing speech.test.ts / companionText.test.ts assertion style.
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
  accumulateSpeakableCaption,
} from './companionText'
import { companionSurfaceState } from './useCompanionMachine'
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
    expect(cleanUserTranscript('打开网易云')).toBe('打开网易云音乐')
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

  test('commit is 1.2s after they stop, not 50–400ms and not 2.7–3.5s', () => {
    expect(turnEnded({ ...settled, silentForMs: 80 })).toBe(false)
    expect(turnEnded({ ...settled, silentForMs: 400 })).toBe(false)
    expect(turnEnded({ ...settled, silentForMs: 1100 })).toBe(false)
    expect(turnEnded({ ...settled, silentForMs: TURN_END_SILENCE_MS })).toBe(true)
    expect(TURN_END_SILENCE_MS).toBe(1200)
    expect(TURN_END_INCOMPLETE_SILENCE_MS).toBe(1500)
    expect(FORCE_COMMIT_MS).toBeGreaterThan(TURN_END_SILENCE_MS)
    expect(FORCE_COMMIT_MS).toBeLessThan(2700)
  })

  test('incomplete-looking phrases end at 1.2s of true silence, not a short breath', () => {
    const incomplete = { ...settled, incomplete: true }
    expect(turnEnded({ ...incomplete, silentForMs: 400 })).toBe(false)
    expect(turnEnded({ ...incomplete, silentForMs: 1100 })).toBe(false)
    expect(turnEnded({ ...incomplete, silentForMs: TURN_END_INCOMPLETE_SILENCE_MS })).toBe(true)
  })

  test('「打开网」is not a finished command after a 50–400ms pause', () => {
    expect(turnEnded({
      speechActive: false,
      silentForMs: 400,
      msSinceLastResult: TURN_END_TEXT_SETTLE_MS + 50,
      incomplete: true,
    })).toBe(false)
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

describe('多轮直到退出', () => {
  test('eight listen → 1.2s → answer → next-listen rounds stay fluent', () => {
    const lines = [
      '你好月汐',
      '今晚天气怎么样',
      '帮我打开桌面',
      '打开网易云音乐',
      '搜索周杰伦放一首',
      '下一句你好吗',
      '谢谢',
      '再见',
    ]
    let lastSpoken = ''
    for (let i = 0; i < lines.length; i += 1) {
      const heard = cleanUserTranscript(lines[i])
      expect(heard).toBe(lines[i])
      expect(turnEnded({
        speechActive: false,
        silentForMs: 400,
        msSinceLastResult: TURN_END_TEXT_SETTLE_MS + 50,
        incomplete: false,
      })).toBe(false)
      expect(turnEnded({
        speechActive: false,
        silentForMs: TURN_END_SILENCE_MS,
        msSinceLastResult: TURN_END_TEXT_SETTLE_MS + 50,
        incomplete: false,
      })).toBe(true)
      expect(shouldAcceptUserTranscript({
        state: 'listening',
        text: heard,
        lastSpoken,
        lastAssistant: lastSpoken,
      })).toBe(true)
      expect(shouldAcceptUserTranscript({
        state: 'listening',
        text: lastSpoken || '今晚是满月，适合抬头。',
        lastSpoken: lastSpoken || '今晚是满月，适合抬头。',
        lastAssistant: lastSpoken || '今晚是满月，适合抬头。',
      })).toBe(false)
      expect(stripTaskDonePhrases('我做完了')).toBe('')
      expect(companionSurfaceState('listening', true)).toBe('speaking')
      lastSpoken = `好的，${heard}`
    }
  })
})

describe('字幕与说话状态', () => {
  test('assistant caption accumulates streaming crumbs into one line', () => {
    let caption = ''
    for (const piece of ['在', '的。', '我在听，请说。']) {
      caption = accumulateSpeakableCaption(caption, piece)
    }
    expect(caption).toBe('在的。我在听，请说。')
    expect(caption).not.toBe('请说。')
  })

  test('assistant caption grows to the full sentence, including 问', () => {
    let caption = ''
    for (const piece of ['当', '然可以啦，你有什么问', '题']) {
      caption = accumulateSpeakableCaption(caption, piece)
    }
    expect(caption).toBe('当然可以啦，你有什么问题')
  })

  test('「打开桌面上的协议文档」survives ASR 把开 and stays a command', () => {
    expect(cleanUserTranscript('把开了我把它桌面上的协议文档')).toBe('打开桌面上的协议文档')
    expect(shouldAcceptUserTranscript({ ...listening, text: '打开桌面上的协议文档' })).toBe(true)
  })
})
