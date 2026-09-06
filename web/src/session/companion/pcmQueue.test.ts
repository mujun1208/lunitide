import { expect, test } from 'vitest'
import { int16ToBase64 } from './pcmFrames'
import { MAX_VOICE_APPEND_SAMPLES, takePcmBatch } from './pcmQueue'

test.each([1, 16000, 24013, 720000])('PCM batches preserve all %i samples and stay within the backend limit', count => {
  const input = Int16Array.from({ length: count }, (_, i) => i % 32768)
  const pending = [{ samples: input, base64: int16ToBase64(input) }]
  const output: number[] = []
  for (let batch = takePcmBatch(pending); batch; batch = takePcmBatch(pending)) {
    expect(batch.sampleCount).toBeGreaterThan(0)
    expect(batch.sampleCount).toBeLessThanOrEqual(MAX_VOICE_APPEND_SAMPLES)
    expect(atob(batch.base64).length).toBe(batch.sampleCount * 2)
    output.push(...batch.samples)
  }
  expect(output).toEqual(Array.from(input))
})

test('PCM batches join frames and preserve a partial tail without padding', () => {
  const frames = Array.from({ length: 15 }, (_, i) => {
    const samples = new Int16Array(i === 14 ? 93 : 1600).fill(i)
    return { samples, base64: int16ToBase64(samples) }
  })
  const expected = frames.flatMap(frame => Array.from(frame.samples))
  const first = takePcmBatch(frames)!
  const second = takePcmBatch(frames)!
  expect(first.sampleCount).toBe(16000)
  expect(second.sampleCount).toBe(6493)
  expect([...first.samples, ...second.samples]).toEqual(expected)
  expect(takePcmBatch(frames)).toBeUndefined()
})
