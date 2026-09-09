import { describe, expect, it } from 'vitest'
import { createVolcTranscriptCursor, type VolcUtterance } from './volcTranscript'

const segment = (text: string, startMs: number, final = false, endMs = startMs + 900): VolcUtterance => ({ text, startMs, endMs, final })

describe('Volc full snapshot audio-time cursor', () => {
  it('does not treat a late re-segmented tail inside submitted audio as a new turn', () => {
    const cursor = createVolcTranscriptCursor()
    for (let round = 0; round < 12; round++) {
      const start = round * 5000
      const full = `第${round + 1}轮，请查询今天合肥的天气怎么样？`
      expect(cursor.update(full, false, [segment(full, start, false, start + 2300)]).text).toBe(full)
      expect(cursor.commit()).toBe(full)
      // SAUC may revise VAD segmentation after the product silence deadline.
      // The tail now has a different start, but contains no newly spoken audio.
      expect(cursor.update('呢？', true, [segment('呢？', start + 1900, true, start + 2300)]).text).toBe('')
      expect(cursor.commit()).toBe('')
    }
  })

  it('replaces dozens of corrected hypotheses in the same segment instead of making a repeated wall', () => {
    const cursor = createVolcTranscriptCursor()
    for (let i = 0; i < 60; i++) {
      const text = `今天${i % 2 ? '合肥' : '合肥市'}天气不错，我们准备出去散步。`
      expect(cursor.update(text, false, [segment(text, 0)]).text).toBe(text)
    }
    expect(cursor.commit()).toBe('今天合肥天气不错，我们准备出去散步。')
  })

  it('does not revive committed history even when the provider revises its wording or length', () => {
    const cursor = createVolcTranscriptCursor()
    cursor.update('我们要开会。', true, [segment('我们要开会。', 0, true)])
    expect(cursor.commit()).toBe('我们要开会。')
    expect(cursor.update('现在我们要开会议。今天看一下进度', false, [
      segment('现在我们要开会议。', 0, true), segment('今天看一下进度', 1100),
    ])).toEqual({ text: '今天看一下进度', final: false })
    // A late final for an older segment cannot replace the complete new turn.
    expect(cursor.update('会。', true, [segment('会。', 0, true)])).toEqual({ text: '今天看一下进度', final: false })
  })

  it('retains identical sentences spoken at distinct audio positions', () => {
    const cursor = createVolcTranscriptCursor()
    cursor.update('请再说一遍。', true, [segment('请再说一遍。', 0, true)])
    expect(cursor.commit()).toBe('请再说一遍。')
    cursor.update('请再说一遍。请再说一遍。', true, [segment('请再说一遍。', 0, true), segment('请再说一遍。', 1400, true)])
    expect(cursor.commit()).toBe('请再说一遍。')
    expect(cursor.update('请再说一遍。', true, [segment('请再说一遍。', 1400, true)]).text).toBe('')
  })

  it('does not shorten a complete caption to a repeated syllable or an older partial', () => {
    const cursor = createVolcTranscriptCursor()
    const complete = '现在你可以听到我说话吗？'
    cursor.update(complete, false, [segment(complete, 0, false, 2500)])
    expect(cursor.update('吗？', true, [segment('吗？', 0, true, 2500)]).text).toBe(complete)
    expect(cursor.update('现在', false, [segment('现在', 0, false, 400)]).text).toBe(complete)
    expect(cursor.commit()).toBe(complete)
  })

  it('resets audio positions when the websocket is replaced', () => {
    const cursor = createVolcTranscriptCursor()
    cursor.update('上一轮。', true, [segment('上一轮。', 40000, true)])
    cursor.commit()
    cursor.reset()
    expect(cursor.update('新连接第一句。', true, [segment('新连接第一句。', 0, true)]).text).toBe('新连接第一句。')
  })

  it('supports old text-only engines and isolates the committed prefix only once', () => {
    const cursor = createVolcTranscriptCursor()
    cursor.update('今天合肥天气怎么样', false)
    cursor.update('今天合肥市天气怎么样', true)
    expect(cursor.commit()).toBe('今天合肥市天气怎么样')
    expect(cursor.update('今天合肥市天气怎么样。算了放首歌', false).text).toBe('算了放首歌')
    expect(cursor.commit()).toBe('算了放首歌')
    expect(cursor.update('今天合肥市天气怎么样。算了放首歌', true).text).toBe('')
  })
})
