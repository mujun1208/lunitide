// Regression for the 火山 cascade "doubled voice / echo" bug (Issue 2).
//
// volc is the only cascade engine that always live-streams: playStreamingSegment
// decodes the growing WAV and schedules each newly-arrived *contiguous* slice on
// the shared timeline. Those slices are one continuous waveform, so — exactly
// like realtime talk PCM — they must be scheduled back-to-back (join=false).
// The original code used the default join=true, whose 140ms cross-fade replays
// the tail of every chunk: the same voice heard twice, offset (the echo). Only
// volc doubled because only volc streams its cascade output.
//
// Streaming buffers arrive faster than realtime, so the timeline runs ahead of
// the audio clock (modeled here as currentTime≈0) — the exact condition under
// which the join=true cross-fade fires.
import { describe, expect, it } from 'vitest'
import { timelineWindow } from './ttsPlayer'

const CHUNK = 0.2
const N = 5

describe('cascade streaming chunk scheduling (volc echo)', () => {
  it('join=false lays contiguous stream slices end-to-end with zero replay', () => {
    let end = 0
    for (let i = 0; i < N; i++) {
      const w = timelineWindow(end, 0, CHUNK, false)
      // Every chunk starts exactly where the previous ended — no rewind.
      expect(w.startAt).toBeCloseTo(end, 5)
      end = w.timelineEnd
    }
    // No overlap accumulated: the reading is exactly the sum of its chunks.
    expect(end).toBeCloseTo(N * CHUNK, 5)
  })

  it('join=true would replay 140ms per chunk (the doubled-voice bug it fixes)', () => {
    let end = 0
    for (let i = 0; i < N; i++) end = timelineWindow(end, 0, CHUNK, true).timelineEnd
    // Each join after the first rewinds 140ms, so the timeline is shorter than
    // the true audio — that swallowed span is the tail being replayed (echo).
    expect(end).toBeLessThan(N * CHUNK)
    expect(end).toBeCloseTo(N * CHUNK - (N - 1) * 0.14, 5)
  })
})
