import { afterEach, expect, it, vi } from 'vitest'
import { cancelLivePaint, scheduleLivePaint, streamingThinkingTail, type PaintSlot } from './livePaint'

afterEach(() => { vi.unstubAllGlobals() })

it('flushes a burst of stream paints on one frame', () => {
  const queued: FrameRequestCallback[] = []
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { queued.push(cb); return queued.length })
  vi.stubGlobal('cancelAnimationFrame', () => {})
  const slot: PaintSlot = { frame: 0 }
  let paints = 0
  const flush = () => { paints += 1 }
  scheduleLivePaint(slot, flush)
  scheduleLivePaint(slot, flush)
  expect(queued).toHaveLength(1)
  queued[0](0)
  expect(paints).toBe(1)
  expect(slot.frame).toBe(0)
})

it('drops a scheduled paint when the panel goes away', () => {
  const queued: FrameRequestCallback[] = []
  const cancelled: number[] = []
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { queued.push(cb); return 7 })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => { cancelled.push(id) })
  const slot: PaintSlot = { frame: 0 }
  scheduleLivePaint(slot, () => {})
  cancelLivePaint(slot)
  expect(cancelled).toEqual([7])
  expect(slot.frame).toBe(0)
  expect(queued).toHaveLength(1)
})

it('keeps only the latest stretch of a long thinking stream', () => {
  expect(streamingThinkingTail('短')).toBe('短')
  const text = `${'旧'.repeat(5000)}最新`
  const tail = streamingThinkingTail(text)
  expect(tail.startsWith('…\n')).toBe(true)
  expect(tail.endsWith('最新')).toBe(true)
  expect(tail.length).toBeLessThan(text.length)
})
