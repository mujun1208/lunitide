// companionText.test.ts asserts the M9.5 speech cleaning and
// segmentation rules (T-9.5.3.1): markdown stripped, code fences
// replaced, URLs reduced to domains, emoji removed, sentence splits
// with the 500-char comma re-split and the 20-segment truncation
// notice.
import { describe, expect, test } from 'vitest'
import { MAX_SEGMENT_CHARS, MAX_SEGMENTS, cleanForSpeech, prepareSpeech, segmentForSpeech, takeSpeakableChunk } from './companionText'

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
})

describe('takeSpeakableChunk', () => {
  test('waits for a complete sentence instead of speaking a short comma clause', () => {
    expect(takeSpeakableChunk('今晚是满月，', true)).toBeNull()
    expect(takeSpeakableChunk('今晚是满月，适合抬头。', true)).toEqual({
      text: '今晚是满月，适合抬头。',
      consumed: '今晚是满月，适合抬头。'.length,
    })
  })

  test('starts the first utterance once enough unpunctuated text has arrived', () => {
    const pending = '今晚天气怎么样啊'
    expect(Array.from(pending).length).toBeGreaterThanOrEqual(8)
    const taken = takeSpeakableChunk(pending, true)
    expect(taken).not.toBeNull()
    expect(Array.from(taken!.text).length).toBeGreaterThanOrEqual(8)
    expect(pending.slice(0, taken!.consumed)).toBe(taken!.text)
  })

  test('keeps later chunks as whole sentences so playback is not chopped', () => {
    expect(takeSpeakableChunk('然后我再给你建议', false)).toBeNull()
    expect(takeSpeakableChunk('然后我再给你建议。最后总结。', false)).toEqual({
      text: '然后我再给你建议。',
      consumed: '然后我再给你建议。'.length,
    })
  })
})
