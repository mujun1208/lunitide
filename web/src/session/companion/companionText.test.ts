// companionText.test.ts asserts the M9.5 speech cleaning and
// segmentation rules (T-9.5.3.1): markdown stripped, code fences
// replaced, URLs reduced to domains, emoji removed, sentence splits
// with the 500-char comma re-split and the 20-segment truncation
// notice.
import { describe, expect, test } from 'vitest'
import { MAX_SEGMENT_CHARS, MAX_SEGMENTS, cleanForSpeech, cleanUserTranscript, looksLikePlaybackEcho, looksIncompleteUtterance, mergeShortSegments, prepareSpeech, segmentForSpeech, takeSpeakableChunk } from './companionText'

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
  test('splits on terminal punctuation and newlines', () => {
    expect(segmentForSpeech('第一句。第二句？第三句！\n第四行')).toEqual(['第一句。', '第二句？', '第三句！', '第四行'])
  })

  test('caps each segment at 500 chars with comma re-split', () => {
    const long = `${'前'.repeat(300)}，${'后'.repeat(300)}。`
    const segments = segmentForSpeech(long)
    expect(segments.length).toBeGreaterThan(1)
    for (const segment of segments) expect(Array.from(segment).length).toBeLessThanOrEqual(MAX_SEGMENT_CHARS)
    expect(segments.join('')).toBe(long)
  })

  test('truncates at 20 segments and appends the notice', () => {
    const segments = segmentForSpeech(Array.from({ length: 30 }, (_, i) => `第${i}句。`).join(''))
    expect(segments.length).toBe(MAX_SEGMENTS)
    expect(segments[MAX_SEGMENTS - 1]).toContain('后续内容请看字幕')
  })
})

describe('prepareSpeech', () => {
  test('cleans before segmenting so code bodies are never read aloud', () => {
    const segments = prepareSpeech('```\nsecret()\n```\n好的。')
    expect(segments).toEqual(['代码已省略', '好的。'])
  })

  test('merges short acknowledgement segments for smoother playback', () => {
    expect(prepareSpeech('好的。然后我再给你建议。')).toEqual(['好的。然后我再给你建议。'])
  })
})

describe('takeSpeakableChunk', () => {
  test('starts the first utterance on a short comma clause so the voice does not wait for a period', () => {
    expect(takeSpeakableChunk('今晚是满月，', true)).toEqual({
      text: '今晚是满月，',
      consumed: '今晚是满月，'.length,
    })
  })

  test('starts the first utterance once enough unpunctuated text has arrived', () => {
    const pending = '你好'
    expect(takeSpeakableChunk(pending, true)).toEqual({
      text: '你好',
      consumed: 2,
    })
  })

  test('keeps later chunks as whole sentences so playback is not chopped', () => {
    expect(takeSpeakableChunk('然后我再', false)).toBeNull()
    expect(takeSpeakableChunk('然后我再给你一些建议。最后总结。', false)).toEqual({
      text: '然后我再给你一些建议。',
      consumed: '然后我再给你一些建议。'.length,
    })
  })

  test('force-flushes a stalled unpunctuated tail so later turns do not hang the voice', () => {
    expect(takeSpeakableChunk('然后我再', false, true)).toEqual({
      text: '然后我再',
      consumed: '然后我再'.length,
    })
  })
})

describe('looksIncompleteUtterance', () => {
  test('flags mid-command tails and short fragments', () => {
    expect(looksIncompleteUtterance('帮我在桌面儿')).toBe(true)
    expect(looksIncompleteUtterance('帮我打开桌面。')).toBe(false)
    expect(looksIncompleteUtterance('好的')).toBe(true)
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

  test('keeps meaningful content intact', () => {
    expect(cleanUserTranscript('帮我写一份项目计划')).toBe('帮我写一份项目计划')
  })
})

describe('looksLikePlaybackEcho', () => {
  test('treats a recognizer copy of the spoken reply as echo', () => {
    expect(looksLikePlaybackEcho('今晚是满月，适合抬头。', '今晚是满月，适合抬头。')).toBe(true)
    expect(looksLikePlaybackEcho('今晚是满月适合抬头', '今晚是满月，适合抬头。')).toBe(true)
  })

  test('does not treat a new question as echo of the previous reply', () => {
    expect(looksLikePlaybackEcho('帮我打开桌面协议', '今晚是满月，适合抬头。')).toBe(false)
    expect(looksLikePlaybackEcho('嗯', '今晚是满月，适合抬头。')).toBe(false)
  })
})
