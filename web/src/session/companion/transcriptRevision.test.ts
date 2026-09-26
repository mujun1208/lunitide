import { describe, expect, test } from 'vitest'
import { commitRecognitionFinal } from './speech'
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

  test('a five-character fragment does not replace the news sentence', () => {
    expect(pickTranscriptRevision('打开第一个新闻链接', '打开第一条')).toBe('打开第一个新闻链接')
    expect(pickTranscriptRevision('打开第一个新闻链接', '第一条新闻')).toBe('打开第一个新闻链接')
  })

  test('keeps both packets when the recognizer splits one command', () => {
    expect(pickTranscriptRevision('打开', '第一个新闻链接')).toBe('打开第一个新闻链接')
    expect(commitRecognitionFinal('', '打开', '第一个新闻链接').finals).toBe('打开第一个新闻链接')
    expect(pickTranscriptRevision('打开第一', '条新闻链接')).toBe('打开第一条新闻链接')
    expect(commitRecognitionFinal('', '打开第一', '条新闻链接').finals).toBe('打开第一条新闻链接')
    expect(pickTranscriptRevision('打开', '第')).toBe('打开第')
    expect(pickTranscriptRevision('打开第一条', '新闻')).toBe('打开第一条新闻')
  })

  test('a finished sentence is not glued onto the next one', () => {
    expect(pickTranscriptRevision('今天天气很好', '我们出去走走')).toBe('我们出去走走')
    expect(pickTranscriptRevision('打开第一个新闻链接', '我')).toBe('打开第一个新闻链接')
  })

  test('a one- or two-character final does not erase the heard sentence', () => {
    expect(pickTranscriptRevision('打开第一个新闻链接', '我')).toBe('打开第一个新闻链接')
    expect(pickTranscriptRevision('打开第一个新闻链接', '第')).toBe('打开第一个新闻链接')
    expect(pickTranscriptRevision('打开第一个新闻链接', '一个')).toBe('打开第一个新闻链接')
  })

  test('keeps a heard sentence when a later final is only its tail', () => {
    const heard = '我说帮我打开桌面系统架构推荐 md文档'
    expect(pickTranscriptRevision(heard, 'md文档')).toBe(heard)
    expect(commitRecognitionFinal('', heard, 'md文档').finals).toBe(heard)
  })

  test('a same-length correction replaces the previous hypothesis', () => {
    expect(pickTranscriptRevision('今天合肥的天气怎么养', '今天合肥的天气怎么样')).toBe('今天合肥的天气怎么样')
    expect(pickTranscriptRevision('今天合肥市的天气很好，我们一起出去散步。', '今天合肥的天气很好，我们一起出去散步。')).toBe(
      '今天合肥的天气很好，我们一起出去散步。',
    )
  })

  test('keeps later terminal punctuation on the same spoken words', () => {
    expect(pickTranscriptRevision('你好', '你好。')).toBe('你好。')
    expect(pickTranscriptRevision('今天合肥的天气怎么样', '今天合肥的天气怎么样？')).toBe('今天合肥的天气怎么样？')
    expect(pickTranscriptRevision('你好。', '你好')).toBe('你好。')
  })
})
