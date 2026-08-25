import { describe, expect, it } from 'vitest'
import {
  FRAME_SAMPLES,
  TARGET_SAMPLE_RATE,
  createFrameAccumulator,
  floatToInt16,
  framePeak,
  int16ToBase64,
  resampleTo16k,
} from './pcmFrames'

describe('resampleTo16k', () => {
  it('returns the input untouched when it already arrives at 16 kHz', () => {
    const input = new Float32Array([0.1, -0.2, 0.3])
    expect(resampleTo16k(input, TARGET_SAMPLE_RATE)).toBe(input)
  })

  it('produces a third of the samples from 48 kHz', () => {
    const input = new Float32Array(480)
    expect(resampleTo16k(input, 48000)).toHaveLength(160)
  })

  it('produces the right count from a rate that does not divide evenly', () => {
    const input = new Float32Array(441)
    // 44100 / 16000 = 2.75625, so 441 samples carry 160 whole output samples.
    expect(resampleTo16k(input, 44100)).toHaveLength(160)
  })

  it('follows a ramp rather than merely sampling it', () => {
    // A straight line is the case linear interpolation reproduces exactly, so
    // any error here is indexing rather than approximation.
    const input = Float32Array.from({ length: 480 }, (_, i) => i / 480)
    const out = resampleTo16k(input, 48000)
    expect(out[0]).toBeCloseTo(0, 5)
    expect(out[80]!).toBeCloseTo(240 / 480, 5)
    expect(out[159]!).toBeCloseTo(477 / 480, 5)
  })

  it('never reads past the end of the input', () => {
    const input = Float32Array.from({ length: 97 }, (_, i) => (i % 2 ? 1 : -1))
    for (const sample of resampleTo16k(input, 48000)) {
      expect(Number.isFinite(sample)).toBe(true)
    }
  })

  it('treats an unusable rate as no audio rather than dividing by it', () => {
    expect(resampleTo16k(new Float32Array([1, 2, 3]), 0)).toHaveLength(0)
    expect(resampleTo16k(new Float32Array([1, 2, 3]), Number.NaN)).toHaveLength(0)
  })

  it('survives an empty input', () => {
    expect(resampleTo16k(new Float32Array(0), 48000)).toHaveLength(0)
  })
})

describe('floatToInt16', () => {
  it('maps the ends of the range to the ends of the type', () => {
    const out = floatToInt16(new Float32Array([1, -1, 0]))
    expect(out[0]).toBe(32767)
    expect(out[1]).toBe(-32768)
    expect(out[2]).toBe(0)
  })

  it('clamps overshoot instead of letting it wrap polarity', () => {
    // Gain control and channel summing both push past 1.0. Wrapping would turn
    // a loud positive peak into a loud negative one, which is a click.
    const out = floatToInt16(new Float32Array([1.8, -2.5]))
    expect(out[0]).toBe(32767)
    expect(out[1]).toBe(-32768)
  })

  it('keeps quiet audio quiet', () => {
    const out = floatToInt16(new Float32Array([0.5, -0.5]))
    expect(out[0]).toBeGreaterThan(16000)
    expect(out[0]).toBeLessThan(16500)
    expect(out[1]).toBeLessThan(-16000)
  })
})

describe('createFrameAccumulator', () => {
  it('emits nothing until a whole frame has arrived', () => {
    const acc = createFrameAccumulator(160)
    expect(acc.push(new Float32Array(128))).toHaveLength(0)
    expect(acc.pendingSamples()).toBe(128)
    expect(acc.push(new Float32Array(32))).toHaveLength(1)
    expect(acc.pendingSamples()).toBe(0)
  })

  it('emits every frame a large push completed', () => {
    const acc = createFrameAccumulator(160)
    expect(acc.push(new Float32Array(500))).toHaveLength(3)
    expect(acc.pendingSamples()).toBe(20)
  })

  it('holds the remainder back rather than padding mid-utterance', () => {
    // Padding here would splice silence into the middle of a word every time
    // the block size and frame size failed to divide evenly.
    const acc = createFrameAccumulator(4)
    const frames = acc.push(Float32Array.from([1, 2, 3, 4, 5, 6]))
    expect(Array.from(frames[0]!)).toEqual([1, 2, 3, 4])
    expect(acc.pendingSamples()).toBe(2)
    const next = acc.push(Float32Array.from([7, 8]))
    expect(Array.from(next[0]!)).toEqual([5, 6, 7, 8])
  })

  it('preserves sample order across many small pushes', () => {
    const acc = createFrameAccumulator(4)
    const collected: number[] = []
    for (let i = 0; i < 10; i++) {
      for (const frame of acc.push(Float32Array.from([i]))) collected.push(...frame)
    }
    expect(collected).toEqual([0, 1, 2, 3, 4, 5, 6, 7])
  })

  it('pads only on flush, and only what was left over', () => {
    const acc = createFrameAccumulator(4)
    acc.push(Float32Array.from([9, 8]))
    expect(Array.from(acc.flush()!)).toEqual([9, 8, 0, 0])
    expect(acc.pendingSamples()).toBe(0)
  })

  it('flushes nothing when nothing was held', () => {
    const acc = createFrameAccumulator(4)
    expect(acc.flush()).toBeUndefined()
  })

  it('drops the tail on reset so a new turn cannot inherit the last one', () => {
    const acc = createFrameAccumulator(4)
    acc.push(Float32Array.from([1, 2]))
    acc.reset()
    expect(acc.pendingSamples()).toBe(0)
    expect(acc.flush()).toBeUndefined()
  })

  it('defaults to one frame of audio at the target rate', () => {
    const acc = createFrameAccumulator()
    expect(acc.push(new Float32Array(FRAME_SAMPLES))).toHaveLength(1)
  })
})

describe('int16ToBase64', () => {
  it('round-trips the bytes it was given', () => {
    const samples = Int16Array.from([0, 1, -1, 32767, -32768, 1234])
    const decoded = atob(int16ToBase64(samples))
    const bytes = new Uint8Array(decoded.length)
    for (let i = 0; i < decoded.length; i++) bytes[i] = decoded.charCodeAt(i)
    expect(Array.from(new Int16Array(bytes.buffer))).toEqual(Array.from(samples))
  })

  it('encodes a frame larger than one apply call could pass as arguments', () => {
    const samples = Int16Array.from({ length: 60000 }, (_, i) => (i % 2 ? 1000 : -1000))
    expect(() => int16ToBase64(samples)).not.toThrow()
    expect(atob(int16ToBase64(samples))).toHaveLength(samples.byteLength)
  })

  it('respects a view that does not start at the beginning of its buffer', () => {
    const backing = Int16Array.from([1111, 2222, 3333, 4444])
    const view = backing.subarray(2)
    const decoded = atob(int16ToBase64(view))
    const bytes = new Uint8Array(decoded.length)
    for (let i = 0; i < decoded.length; i++) bytes[i] = decoded.charCodeAt(i)
    expect(Array.from(new Int16Array(bytes.buffer))).toEqual([3333, 4444])
  })
})

describe('framePeak', () => {
  it('reports the loudest magnitude regardless of sign', () => {
    expect(framePeak(Float32Array.from([0.1, -0.7, 0.3]))).toBeCloseTo(0.7, 6)
  })

  it('is zero for silence and for nothing at all', () => {
    expect(framePeak(new Float32Array(64))).toBe(0)
    expect(framePeak(new Float32Array(0))).toBe(0)
  })

  it('does not report more than full scale', () => {
    expect(framePeak(Float32Array.from([3]))).toBe(1)
  })
})
