import { afterEach, describe, expect, test, vi } from 'vitest'
import {
  absorbHeldTranscript,
  cleanMeetingTranscript,
  createMeetingLineBuffer,
  joinMeetingLines,
  collapseLiveTranscriptLines,
  meetingLineDelta,
  MEETING_MERGE_GAP_MS,
  pickMeetingFinalText,
  shouldMergeMeetingLines,
} from './meetingText'

describe('cleanMeetingTranscript', () => {
  test('strips oral fillers without dropping the meaning', () => {
    const raw = '我现在开始做那个会议纪要啊然后然后看看能不能把这个呃呃工作都彻底落实呃第一步应该先写 b r d 然后第二步'
    const got = cleanMeetingTranscript(raw)
    expect(got).toMatch(/BRD/)
    expect(got).toMatch(/会议纪要/)
    expect(got).toMatch(/落实/)
    expect(got).not.toMatch(/呃/)
    expect(got).not.toMatch(/啊/)
    expect(got).not.toMatch(/然后然后/)
    expect(got).not.toMatch(/b r d/i)
  })

  test('keeps sentence-final 好啊 and domain names from companion cleanup', () => {
    expect(cleanMeetingTranscript('好啊，那就先写 brd')).toMatch(/好啊/)
    expect(cleanMeetingTranscript('好啊，那就先写 brd')).toMatch(/BRD/)
    expect(cleanMeetingTranscript('你好岳西，打开店面文件')).toMatch(/月汐/)
    expect(cleanMeetingTranscript('你好岳西，打开店面文件')).toMatch(/桌面/)
  })

  test('drops lines that are only fillers', () => {
    expect(cleanMeetingTranscript('呃啊嗯')).toBe('')
    expect(cleanMeetingTranscript('然后然后')).toBe('')
  })
})

describe('absorbHeldTranscript', () => {
  test('glues a new sherpa segment onto the sealed clause', () => {
    expect(absorbHeldTranscript('第一步先写BRD', '第二步再做相关工作')).toBe('第一步先写BRD第二步再做相关工作')
  })

  test('keeps a growing revision of the same segment', () => {
    expect(absorbHeldTranscript('第一步', '第一步先写BRD')).toBe('第一步先写BRD')
  })

  test('deduplicates overlap when sherpa starts the next segment mid-word', () => {
    expect(absorbHeldTranscript('先把范围对齐', '范围对齐并且写BRD')).toBe('先把范围对齐并且写BRD')
  })
})

describe('pickMeetingFinalText', () => {
  test('prefers the held caption when commit returns only the last segment', () => {
    const held = '火焰已把火烧到天亮做一个叛逆的童年我把抽屉图晃晃悠悠和我心情往前走'
    expect(pickMeetingFinalText(held, '往前走')).toBe(held)
  })

  test('prefers a full refiner pass when it covers the held caption', () => {
    const held = '第一步应该先写BRD第二步再做相关工作。'
    const refined = '第一步应该先写BRD。第二步再做相关工作。'
    expect(pickMeetingFinalText(held, refined)).toBe(refined)
  })
})

describe('shouldMergeMeetingLines', () => {
  test('merges short chops that arrive close together', () => {
    expect(shouldMergeMeetingLines('先写', 'BRD', 200)).toBe(true)
    expect(shouldMergeMeetingLines('先对齐范围并且写完纪要。', '下一件事先放一放。', 200)).toBe(false)
    expect(shouldMergeMeetingLines('先写', 'BRD', MEETING_MERGE_GAP_MS + 1)).toBe(false)
  })
})

describe('joinMeetingLines', () => {
  test('glues CJK without spaces and Latin with a space', () => {
    expect(joinMeetingLines('先写', 'BRD')).toBe('先写BRD')
    expect(joinMeetingLines('write', 'BRD')).toBe('write BRD')
    expect(joinMeetingLines('先写BRD。', '第二步再做。')).toBe('先写BRD。第二步再做。')
  })
})

describe('meetingLineDelta', () => {
  test('keeps only the new clause when ASR repeats the history', () => {
    expect(meetingLineDelta('今天天气不错。', '今天天气不错。我们开会。')).toBe('我们开会。')
    expect(meetingLineDelta('今天天气不错。我们开会。', '今天天气不错。我们开会。')).toBe('')
    expect(meetingLineDelta('', '今天天气不错。')).toBe('今天天气不错。')
  })

  test('ignores the period cleanMeetingTranscript adds when ASR concatenates the same sentence', () => {
    const clause = '针对一个场景的业务逻辑设计要把项目和专家包对齐'
    expect(meetingLineDelta(`${clause}。`, clause + clause)).toBe('')
    expect(meetingLineDelta(`${clause}。`, `${clause}。${clause}。`)).toBe('')
  })
})

describe('collapseLiveTranscriptLines', () => {
  test('drops prefix replays so the transcript is not one growing paragraph', () => {
    const clause = '针对一个场景的业务逻辑设计要把项目和专家包对齐。'
    expect(collapseLiveTranscriptLines([clause, clause + clause, clause])).toEqual([clause])
  })
})

describe('createMeetingLineBuffer', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  test('emits a cleaned line and merges a short follow-up', () => {
    vi.useFakeTimers()
    const emit = vi.fn()
    const buffer = createMeetingLineBuffer(emit)
    buffer.push('呃第一步应该先写 b r d')
    expect(emit).not.toHaveBeenCalled()
    buffer.push('然后第二步再做')
    buffer.flush()
    expect(emit).toHaveBeenCalledOnce()
    const line = emit.mock.calls[0][0] as string
    expect(line).toMatch(/BRD/)
    expect(line).toMatch(/第二步/)
    expect(line).not.toMatch(/呃/)
  })

  test('does not emit the same spoken line twice when ASR repeats history', () => {
    vi.useFakeTimers()
    const emit = vi.fn()
    const buffer = createMeetingLineBuffer(emit)
    buffer.push('今天天气不错')
    buffer.flush()
    buffer.push('今天天气不错')
    buffer.flush()
    buffer.push('今天天气不错我们开会')
    buffer.flush()
    expect(emit.mock.calls.map(call => call[0]).join(' ')).not.toMatch(/今天天气不错.*今天天气不错/)
    expect(emit).toHaveBeenCalledTimes(2)
  })
})
