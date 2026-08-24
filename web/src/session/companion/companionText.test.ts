// companionText.test.ts asserts the M9.5 speech cleaning and
// segmentation rules (T-9.5.3.1): markdown stripped, code fences
// replaced, URLs reduced to domains, emoji removed, sentence splits
// with the 500-char comma re-split and the 20-segment truncation
// notice.
import { describe, expect, test } from 'vitest'
import {
  COMPANION_AFTER_TOKEN_MS,
  COMPANION_FIRST_TOKEN_CONNECTING_MS,
  COMPANION_FIRST_TOKEN_STREAMING_MS,
  MAX_SEGMENT_CHARS,
  MAX_SEGMENTS,
  cleanForSpeech,
  cleanUserTranscript,
  companionInstantAck,
  companionReplyStallMs,
  looksLikePlaybackEcho,
  looksIncompleteUtterance,
  mergeShortSegments,
  prepareSpeech,
  segmentForSpeech,
  takeSpeakableChunk,
} from './companionText'

describe('cleanForSpeech', () => {
  test('replaces code fences and inline code with the spoken notice', () => {
    const out = cleanForSpeech('先看代码：\n```go\nfmt.Println("hi")\n```\n以及 `x := 1` 结束。')
    expect(out).not.toContain('fmt')
    expect(out).not.toContain('```')
    expect(out.match(/代码已省略/g)?.length).toBe(2)
  })

  test('reduces URLs to their domain', () => {
    expect(cleanForSpeech('参见 https://example.com/docs/page?x=1 说明。')).toBe('参见 example.com 说明。')
  })

  test('strips markdown emphasis, headings, list markers and links', () => {
    const out = cleanForSpeech('## 标题\n- **重点** __下划线__ ~~删除~~\n> 引用\n1. 第一条\n[链接](https://a.b/c) 文本')
    expect(out).not.toContain('##')
    expect(out).not.toContain('**')
    expect(out).not.toContain('~~')
    expect(out).not.toContain('](https')
    expect(out).toContain('重点')
    expect(out).toContain('链接')
  })

  test('removes emoji that SAPI cannot read', () => {
    const out = cleanForSpeech('完成了 🎉✅，继续 🚀。')
    expect(out).toBe('完成了，继续。')
  })
})

describe('segmentForSpeech', () => {
  test('keeps a conversational reply as one continuous clip', () => {
    expect(segmentForSpeech('第一句。第二句？第三句！\n第四行')).toEqual(['第一句。第二句？第三句！第四行'])
  })

  test('caps each segment at 1200 chars with comma re-split', () => {
    const long = `${'前'.repeat(700)}，${'后'.repeat(700)}。`
    const segments = segmentForSpeech(long)
    expect(segments.length).toBeGreaterThan(1)
    for (const segment of segments) expect(Array.from(segment).length).toBeLessThanOrEqual(MAX_SEGMENT_CHARS)
    expect(segments.join('')).toBe(long)
  })

  test('truncates at 20 segments and appends the notice', () => {
    const segments = segmentForSpeech(Array.from({ length: 30 }, () => `${'字'.repeat(80)}。`).join(''))
    expect(segments.length).toBe(MAX_SEGMENTS)
    expect(segments[MAX_SEGMENTS - 1]).toContain('后续内容请看字幕')
  })
})

describe('prepareSpeech', () => {
  test('cleans before segmenting so code bodies are never read aloud', () => {
    const segments = prepareSpeech('```\nsecret()\n```\n好的。')
    expect(segments).toEqual(['代码已省略好的。'])
  })

  test('merges short acknowledgement segments for smoother playback', () => {
    expect(prepareSpeech('好的。然后我再给你建议。')).toEqual(['好的。然后我再给你建议。'])
  })
})

describe('takeSpeakableChunk', () => {
  test('does not speak unpunctuated fragments until a sentence lands', () => {
    expect(takeSpeakableChunk('你', true)).toBeNull()
    expect(takeSpeakableChunk('你好', true)).toBeNull()
    expect(takeSpeakableChunk('你好月汐', true)).toBeNull()
    expect(takeSpeakableChunk('今晚，', true)).toBeNull()
    expect(takeSpeakableChunk('今晚月色真的很好，', true)).toBeNull()
  })

  test('speaks a whole sentence in one clip, including commas', () => {
    const sentence = '你好，我是月汐，你的私人助理。'
    expect(takeSpeakableChunk(sentence, true)).toEqual({
      text: sentence,
      consumed: sentence.length,
    })
    expect(takeSpeakableChunk('今晚是满月，适合抬头看看。', true)).toEqual({
      text: '今晚是满月，适合抬头看看。',
      consumed: '今晚是满月，适合抬头看看。'.length,
    })
    expect(takeSpeakableChunk('今晚月色真的很好，适合出门走走。', true)).toEqual({
      text: '今晚月色真的很好，适合出门走走。',
      consumed: '今晚月色真的很好，适合出门走走。'.length,
    })
  })

  test('keeps later chunks as whole sentences so playback is not chopped', () => {
    expect(takeSpeakableChunk('然后我', false)).toBeNull()
    expect(takeSpeakableChunk('然后我再给你一些建议。最后总结。', false)).toEqual({
      text: '然后我再给你一些建议。最后总结。',
      consumed: '然后我再给你一些建议。最后总结。'.length,
    })
  })

  test('force-flushes a stalled unpunctuated tail so later turns do not hang the voice', () => {
    expect(takeSpeakableChunk('你', true, true)).toBeNull()
    expect(takeSpeakableChunk('你好月汐', true, true)).toEqual({
      text: '你好月汐',
      consumed: '你好月汐'.length,
    })
    expect(takeSpeakableChunk('然后我再给你', false, true)).toEqual({
      text: '然后我再给你',
      consumed: '然后我再给你'.length,
    })
  })
})

describe('looksIncompleteUtterance', () => {
  test('flags mid-command tails and short fragments', () => {
    expect(looksIncompleteUtterance('帮我在桌面儿')).toBe(true)
    expect(looksIncompleteUtterance('帮我打开桌面。')).toBe(false)
    expect(looksIncompleteUtterance('好的')).toBe(false)
    expect(looksIncompleteUtterance('帮我打开桌面')).toBe(false)
    expect(looksIncompleteUtterance('你好月汐')).toBe(false)
    expect(looksIncompleteUtterance('你好')).toBe(false)
    expect(looksIncompleteUtterance('你可以')).toBe(true)
    expect(looksIncompleteUtterance('帮我')).toBe(true)
    expect(looksIncompleteUtterance('你能')).toBe(true)
    expect(looksIncompleteUtterance('你可以帮我')).toBe(true)
    expect(looksIncompleteUtterance('你能帮我')).toBe(true)
    expect(looksIncompleteUtterance('请你帮我')).toBe(true)
    expect(looksIncompleteUtterance('合肥的')).toBe(true)
    expect(looksIncompleteUtterance('合肥的天气怎么样')).toBe(false)
  })

  test('treats sentence-final particles and question words as whole turns', () => {
    // These used to wait out the 1.6–2.2s incomplete hold before sending.
    expect(looksIncompleteUtterance('我知道了')).toBe(false)
    expect(looksIncompleteUtterance('太好了')).toBe(false)
    expect(looksIncompleteUtterance('你在干什么')).toBe(false)
    expect(looksIncompleteUtterance('你现在在哪儿')).toBe(false)
    expect(looksIncompleteUtterance('这个多少钱')).toBe(false)
    expect(looksIncompleteUtterance('可以帮我看看吗')).toBe(false)
    // Real dangling words still wait for the rest of the sentence.
    expect(looksIncompleteUtterance('我想去')).toBe(true)
    expect(looksIncompleteUtterance('帮我查')).toBe(true)
    expect(looksIncompleteUtterance('明天的')).toBe(true)
  })
})

describe('mergeShortSegments', () => {
  test('merges tiny fragments into the next clause', () => {
    expect(mergeShortSegments(['好的。', '然后我再给你建议。'])).toEqual(['好的。然后我再给你建议。'])
  })
})

describe('cleanUserTranscript', () => {
  test('removes leading and trailing oral fillers', () => {
    expect(cleanUserTranscript('嗯啊，那个，帮我打开桌面')).toBe('帮我打开桌面')
    expect(cleanUserTranscript('然后就是说，今晚月色如何？')).toBe('今晚月色如何？')
    expect(cleanUserTranscript('好的，嗯')).toBe('好的')
  })

  test('corrects common speech-recognition homophones', () => {
    expect(cleanUserTranscript('帮我打开店面文件')).toBe('帮我打开桌面文件')
    expect(cleanUserTranscript('打开气水音乐')).toBe('打开汽水音乐')
    expect(cleanUserTranscript('你好岳西')).toBe('你好月汐')
    expect(cleanUserTranscript('岳西，岳西')).toBe('月汐，月汐')
    expect(cleanUserTranscript('你好月夕')).toBe('你好月汐')
  })
})

describe('looksLikePlaybackEcho', () => {
  test('treats a recognizer copy of the spoken reply as echo', () => {
    expect(looksLikePlaybackEcho('今晚是满月，适合抬头。', '今晚是满月，适合抬头。')).toBe(true)
    expect(looksLikePlaybackEcho('今晚是满月适合抬头', '今晚是满月，适合抬头。')).toBe(true)
    expect(looksLikePlaybackEcho('嗨我在呢', '嗨，我在呢。')).toBe(true)
    expect(looksLikePlaybackEcho('我在呢', '嗨，我在呢。')).toBe(true)
  })

  test('does not treat a new question as echo of the previous reply', () => {
    expect(looksLikePlaybackEcho('帮我打开桌面协议', '今晚是满月，适合抬头。')).toBe(false)
    expect(looksLikePlaybackEcho('嗯', '今晚是满月，适合抬头。')).toBe(false)
  })
})

describe('companionInstantAck', () => {
  test('returns a short spoken line for common voice turns', () => {
    expect(companionInstantAck('你好月汐')).toBe('嗨，我在呢。')
    expect(companionInstantAck('今晚月色如何？')).toBe('嗯，')
    expect(companionInstantAck('一场大雨淋湿了眼睛。')).toBe('嗯，我听到了。')
    expect(companionInstantAck('嗯')).toBe('嗯，')
  })
})

describe('companionReplyStallMs', () => {
  test('gives a live stream time for DeepSeek V4 first token', () => {
    expect(companionReplyStallMs(true, false)).toBe(COMPANION_FIRST_TOKEN_STREAMING_MS)
    expect(companionReplyStallMs(false, false)).toBe(COMPANION_FIRST_TOKEN_CONNECTING_MS)
    expect(companionReplyStallMs(true, true)).toBe(COMPANION_AFTER_TOKEN_MS)
    expect(COMPANION_FIRST_TOKEN_STREAMING_MS).toBeGreaterThanOrEqual(8_000)
  })
})
