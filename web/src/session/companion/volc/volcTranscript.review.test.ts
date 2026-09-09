import { describe, expect, it } from 'vitest'
import { createVolcTranscriptCursor, type VolcUtterance } from './volcTranscript'

const part = (text: string, startMs: number, endMs: number, final = false): VolcUtterance => ({ text, startMs, endMs, final })

describe('independent audio watermark boundary review', () => {
  it('retains a short repeated command at a new audio position, including an exact adjacent boundary', () => {
    const cursor = createVolcTranscriptCursor()
    cursor.update('停。', true, [part('停。', 0, 500, true)])
    expect(cursor.commit()).toBe('停。')
    expect(cursor.update('停。', true, [part('停。', 500, 610, true)])).toEqual({ text: '停。', final: true })
    expect(cursor.commit()).toBe('停。')
  })

  it('ignores delayed old fragments without clearing an already visible next short sentence', () => {
    const cursor = createVolcTranscriptCursor()
    cursor.update('查一下合肥天气。', false, [part('查一下合肥天气。', 0, 2300)])
    cursor.commit()
    cursor.update('不。', false, [part('不。', 2400, 2600)])
    expect(cursor.update('气。', true, [part('气。', 1900, 2300, true)])).toEqual({ text: '不。', final: false })
    expect(cursor.commit()).toBe('不。')
  })

  it('retains distinct meeting clauses from a reordered full snapshot and does not repeat a committed chunk', () => {
    const cursor = createVolcTranscriptCursor()
    cursor.update('同意。不同意。', true, [part('不同意。', 300, 500, true), part('同意。', 0, 200, true)])
    expect(cursor.commit()).toBe('同意。不同意。')
    const snapshot = [part('同意。', 0, 200, true), part('不同意。', 300, 500, true), part('同意。', 550, 700, true)]
    expect(cursor.update('同意。不同意。同意。', true, snapshot).text).toBe('同意。')
    expect(cursor.commit()).toBe('同意。')
    expect(cursor.update('同意。不同意。同意。', true, snapshot).text).toBe('')
  })

  it('retains newly spoken audio when the provider extends a nonfinal segment after a product-side commit', () => {
    const cursor = createVolcTranscriptCursor()
    cursor.update('查天气。', false, [part('查天气。', 0, 2000)])
    expect(cursor.commit()).toBe('查天气。')
    expect(cursor.update('查天气。停。', true, [part('查天气。停。', 0, 3500, true)]).text).toBe('停。')
    expect(cursor.commit()).toBe('停。')
    expect(cursor.update('查天气。停。停。', true, [part('查天气。停。停。', 0, 4600, true)]).text).toBe('停。')
    expect(cursor.commit()).toBe('停。')
    expect(cursor.update('停。', true, [part('停。', 3200, 4600, true)]).text).toBe('')
  })

  it('does not treat a correction or punctuation-only extension of old audio as new speech', () => {
    const cursor = createVolcTranscriptCursor()
    cursor.update('查天气。', false, [part('查天气。', 0, 2000)])
    cursor.commit()
    expect(cursor.update('查天气！', true, [part('查天气！', 0, 2500, true)]).text).toBe('')
    expect(cursor.update('查天气。？', true, [part('查天气。？', 0, 2600, true)]).text).toBe('')
    expect(cursor.update('查合肥天气。', true, [part('查合肥天气。', 0, 2700, true)]).text).toBe('')
    // The next genuinely independent short phrase remains eligible.
    expect(cursor.update('好。', true, [part('好。', 3000, 3200, true)]).text).toBe('好。')
  })
})
