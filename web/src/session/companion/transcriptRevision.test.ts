import { describe, expect, test } from 'vitest'
import { pickTranscriptRevision } from './transcriptRevision'

describe('pickTranscriptRevision', () => {
  test('keeps the already-heard sentence when finish returns a long tail', () => {
    const heard = '播放没有成功，再点击播放一下。'
    expect(pickTranscriptRevision(heard, '成功，再点击播放一下。')).toBe(heard)
    expect(pickTranscriptRevision(heard, '再点击播放一下')).toBe(heard)
    expect(pickTranscriptRevision(heard, '播放一下')).toBe(heard)
  })

  test('keeps the already-heard sentence when finish returns a prefix', () => {
    expect(pickTranscriptRevision('打开网易云音乐', '打开网易云')).toBe('打开网易云音乐')
  })

  test('accepts a longer revision of the same utterance', () => {
    expect(pickTranscriptRevision('播放没有成功', '播放没有成功，再点击播放一下。')).toBe(
      '播放没有成功，再点击播放一下。',
    )
  })

  test('still replaces with a genuinely different shorter sentence', () => {
    expect(pickTranscriptRevision('打开汽水音乐', '暂停')).toBe('暂停')
  })

  test('keeps later terminal punctuation on the same spoken words', () => {
    expect(pickTranscriptRevision('你好', '你好。')).toBe('你好。')
    expect(pickTranscriptRevision('今天合肥的天气怎么样', '今天合肥的天气怎么样？')).toBe('今天合肥的天气怎么样？')
    expect(pickTranscriptRevision('你好。', '你好')).toBe('你好。')
  })
})
