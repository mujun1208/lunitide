import { afterEach, expect, it, vi } from 'vitest'
import { applyLiveChatEvent, listActiveTurns, resetLiveChatForTests, startLiveChat } from './liveChat'

afterEach(resetLiveChatForTests)

it('tracks multiple concurrent turns per session without cancelling siblings', () => {
  const a = startLiveChat('session-a', 'turn-1')
  const b = startLiveChat('session-a', 'turn-2')
  expect(listActiveTurns('session-a')).toHaveLength(2)
  expect(a.terminal).toBe(false)
  expect(b.terminal).toBe(false)
})

it('cancels one turn without retiring the other', async () => {
  const cancelA = vi.fn().mockResolvedValue(true)
  const cancelB = vi.fn().mockResolvedValue(true)
  const a = startLiveChat('session-b', 'turn-a')
  a.stream = { streamId: 's-a', cancel: cancelA, dispose: vi.fn() }
  const b = startLiveChat('session-b', 'turn-b')
  b.stream = { streamId: 's-b', cancel: cancelB, dispose: vi.fn() }
  await a.stream.cancel()
  expect(cancelA).toHaveBeenCalledOnce()
  expect(cancelB).not.toHaveBeenCalled()
  applyLiveChatEvent(a, { v: '1.0', kind: 'event', id: 'e1', streamId: 's-a', sequence: 1, type: 'cancelled' })
  expect(listActiveTurns('session-b')).toEqual([b])
})
