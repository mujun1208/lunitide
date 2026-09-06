import { int16ToBase64, TARGET_SAMPLE_RATE } from './pcmFrames'

/** Matches voice.ValidFrame: at most one second of 16 kHz, mono, s16 PCM. */
export const MAX_VOICE_APPEND_SAMPLES = TARGET_SAMPLE_RATE

export type QueuedPcm = { base64: string; samples: Int16Array }

/** Removes one legal batch, preserving the exact sample order and any tail. */
export function takePcmBatch(pending: QueuedPcm[]): (QueuedPcm & { sampleCount: number }) | undefined {
  while (pending[0]?.samples.length === 0) pending.shift()
  const first = pending[0]
  if (!first) return undefined
  const available = pending.reduce((count, frame) => count + frame.samples.length, 0)
  const sampleCount = Math.min(available, MAX_VOICE_APPEND_SAMPLES)
  if (first.samples.length === sampleCount) {
    pending.shift()
    return { ...first, sampleCount }
  }
  const samples = new Int16Array(sampleCount)
  let offset = 0
  while (offset < sampleCount) {
    const frame = pending[0]!
    const count = Math.min(frame.samples.length, sampleCount - offset)
    samples.set(frame.samples.subarray(0, count), offset)
    offset += count
    if (count === frame.samples.length) pending.shift()
    else {
      const tail = frame.samples.subarray(count)
      pending[0] = { samples: tail, base64: int16ToBase64(tail) }
    }
  }
  return { samples, base64: int16ToBase64(samples), sampleCount }
}
