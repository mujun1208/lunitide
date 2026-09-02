// Regression for the 火山 talk "doubled voice / echo" bug: continuous
// realtime PCM chunks were scheduled through the TTS clip-join path, which
// overlaps each buffer by 140ms. On streamed talk audio that replayed
// ~140ms of every chunk, so the same voice was heard twice, offset. Talk
// PCM (join=false) must be strictly contiguous; only discrete TTS sentence
// clips (join=true) get the cross-fade.
import { describe, expect, it } from 'vitest'
import { timelineWindow } from './ttsPlayer'

describe('timelineWindow', () => {
  it('overlaps joined TTS clips by 140ms so sentences cross-fade', () => {
    // A 1s clip is already scheduled to end at t=1; a second joined clip
    // starts 140ms early so the two readings blend into one take.
    const w = timelineWindow(1, 0, 0.5, true)
    expect(w.startAt).toBeCloseTo(0.86, 5)
    expect(w.timelineEnd).toBeCloseTo(1.36, 5)
  })

  it('never overlaps continuous realtime PCM (join=false)', () => {
    // Consecutive talk chunks must be back-to-back: the next chunk starts
    // exactly where the timeline ended, so no audio is ever replayed.
    const first = timelineWindow(0, 0, 0.2, false)
    expect(first.startAt).toBe(0)
    expect(first.timelineEnd).toBeCloseTo(0.2, 5)

    const second = timelineWindow(first.timelineEnd, 0.05, 0.2, false)
    expect(second.startAt).toBeCloseTo(0.2, 5) // no 140ms rewind
    expect(second.timelineEnd).toBeCloseTo(0.4, 5)

    // Ten chunks in a row accumulate no overlap: total duration is exact.
    let end = 0
    for (let i = 0; i < 10; i++) end = timelineWindow(end, i * 0.02, 0.2, false).timelineEnd
    expect(end).toBeCloseTo(2.0, 5)
  })

  it('starts at the live clock when the timeline has already drained', () => {
    // Timeline ended at t=1 but the clock is already at t=2: do not schedule
    // in the past, start now.
    const w = timelineWindow(1, 2, 0.3, false)
    expect(w.startAt).toBe(2)
    expect(w.timelineEnd).toBeCloseTo(2.3, 5)
  })
})
