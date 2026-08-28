// Multi-round pass bar for 对话模式: listen → 1.2s silence → answer →
// next listen, eight times, on the shared contract used by 云端 Web Speech,
// 本地 sherpa, and MiniCPM-o captions. No live microphone — the same
// functions every path calls.
import { describe, expect, test } from 'vitest'
import {
  TURN_END_INCOMPLETE_SILENCE_MS,
  TURN_END_SILENCE_MS,
  TURN_END_TEXT_SETTLE_MS,
  STUCK_TRANSCRIPT_MS,
  shouldForceCommitUtterance,
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

  test('eight more rounds after a speak cycle still accept the next question', () => {
    let lastSpoken = '当然可以啦，你有什么问题'
    for (let round = 0; round < 8; round += 1) {
      const heard = cleanUserTranscript(`下一轮问题${round + 1}`)
      expect(shouldAcceptUserTranscript({
        state: 'listening',
        text: heard,
        lastSpoken,
        lastAssistant: lastSpoken,
      })).toBe(true)
      expect(turnEnded({
        speechActive: true,
        silentForMs: 0,
        msSinceLastResult: 1400,
        incomplete: false,
      })).toBe(false)
      expect(shouldForceCommitUtterance({
        speechActive: true,
        silentForMs: 0,
        textStableForMs: STUCK_TRANSCRIPT_MS,
        incomplete: false,
      })).toBe(true)
      lastSpoken = `好的，这是第${round + 1}轮回答。`
      expect(looksLikePlaybackEcho(lastSpoken, lastSpoken)).toBe(true)
      expect(shouldKeepHandsFreeLoop({ exited: false, userPausedMic: false })).toBe(true)
    }
  })

  test('「打开桌面上的协议文档」and 把开 garbles stay a desktop-open command', () => {
    expect(cleanUserTranscript('打开桌面上的协议文档')).toBe('打开桌面上的协议文档')
    expect(cleanUserTranscript('把开了我把它桌面上的协议文档')).toBe('打开桌面上的协议文档')
    expect(shouldAcceptUserTranscript({
      state: 'listening',
      text: cleanUserTranscript('把开了我把它桌面上的协议文档'),
      lastSpoken: '今晚是满月，适合抬头。',
      lastAssistant: '今晚是满月，适合抬头。',
    })).toBe(true)
  })
})
