// Multi-round pass bar for 对话模式: listen → 1.2s silence → answer →
// next listen, eight times, on the shared contract used by 云端 Web Speech,
// 本地 sherpa, and MiniCPM-o captions. No live microphone — the same
// functions every path calls.
import { describe, expect, test } from 'vitest'
import {
  TURN_END_INCOMPLETE_SILENCE_MS,
  TURN_END_SILENCE_MS,
  TURN_END_TEXT_SETTLE_MS,
  turnEnded,
} from './speech'
import {
  cleanUserTranscript,
  looksLikeOmniPersonaCaption,
  looksLikePlaybackEcho,
  shouldAcceptUserTranscript,
  shouldKeepHandsFreeLoop,
  stripTaskDonePhrases,
} from './companionText'

const ROUNDS = [
  '你好月汐',
  '今晚天气怎么样',
  '帮我打开桌面',
  '打开网易云音乐',
  '搜索周杰伦放一首',
  '下一句你好吗',
  '谢谢',
  '再见',
]

describe('对话模式多轮契约（云端 / 本地 / MiniCPM-o 共用）', () => {
  test('1.2s of true silence ends a turn; a 400ms breath does not', () => {
    const settled = {
      speechActive: false,
      msSinceLastResult: TURN_END_TEXT_SETTLE_MS + 50,
      incomplete: false,
    }
    expect(TURN_END_SILENCE_MS).toBe(1200)
    expect(TURN_END_INCOMPLETE_SILENCE_MS).toBe(1200)
    expect(turnEnded({ ...settled, silentForMs: 400 })).toBe(false)
    expect(turnEnded({ ...settled, silentForMs: 1200 })).toBe(true)
  })

  test('「打开网」is not committed until they finish or 1.2s of real silence', () => {
    expect(turnEnded({
      speechActive: false,
      silentForMs: 400,
      msSinceLastResult: TURN_END_TEXT_SETTLE_MS + 50,
      incomplete: true,
    })).toBe(false)
    expect(turnEnded({
      speechActive: true,
      silentForMs: 0,
      msSinceLastResult: 800,
      incomplete: true,
    })).toBe(false)
    expect(turnEnded({
      speechActive: false,
      silentForMs: 1200,
      msSinceLastResult: TURN_END_TEXT_SETTLE_MS + 50,
      incomplete: true,
    })).toBe(true)
  })

  test('eight rounds keep the subtitle as their words, ignore echo, and stay armed', () => {
    let lastSpoken = ''
    for (const raw of ROUNDS) {
      const heard = cleanUserTranscript(raw)
      expect(heard).toBe(raw)
      expect(shouldAcceptUserTranscript({
        state: 'listening',
        text: heard,
        lastSpoken,
        lastAssistant: lastSpoken,
      })).toBe(true)
      lastSpoken = `好的，${heard}`
      expect(looksLikePlaybackEcho(lastSpoken, lastSpoken)).toBe(true)
      expect(shouldAcceptUserTranscript({
        state: 'listening',
        text: lastSpoken,
        lastSpoken,
        lastAssistant: lastSpoken,
      })).toBe(false)
      expect(stripTaskDonePhrases('我做完了')).toBe('')
      expect(looksLikeOmniPersonaCaption('人生：优质台湾腔')).toBe(true)
      expect(shouldKeepHandsFreeLoop({ exited: false, userPausedMic: false })).toBe(true)
    }
    expect(shouldKeepHandsFreeLoop({ exited: true, userPausedMic: false })).toBe(false)
  })
})
