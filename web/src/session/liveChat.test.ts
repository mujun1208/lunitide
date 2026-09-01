import { afterEach, expect, it, vi } from 'vitest'
import { applyLiveChatEvent, cancelLiveChatTurn, listActiveSessionIds, listActiveTurns, resetLiveChatForTests, startLiveChat, subscribeLiveChat, subscribeLiveChatRegistry } from './liveChat'

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

it('retires a turn even when the stream handle is missing', async () => {
  const entry = startLiveChat('session-d', 'turn-1')
  const seen: string[] = []
  subscribeLiveChat('session-d', event => { seen.push(event.type) }, 'turn-1')
  await cancelLiveChatTurn('session-d')
  expect(entry.terminal).toBe(true)
  expect(listActiveTurns('session-d')).toHaveLength(0)
  expect(seen).toContain('cancelled')
})

it('retires a turn if stream.cancel hangs', async () => {
  vi.useFakeTimers()
  const entry = startLiveChat('session-e', 'turn-1')
  entry.stream = { streamId: 's-e', cancel: () => new Promise(() => {}), dispose: vi.fn() }
  const done = cancelLiveChatTurn('session-e')
  await vi.advanceTimersByTimeAsync(900)
  await done
  expect(entry.terminal).toBe(true)
  expect(listActiveTurns('session-e')).toHaveLength(0)
  vi.useRealTimers()
})

it('notifies the registry when a session starts or finishes generating', () => {
  const ticks: string[][] = []
  const stop = subscribeLiveChatRegistry(() => ticks.push(listActiveSessionIds()))
  const entry = startLiveChat('session-c', 'turn-1')
  expect(listActiveSessionIds()).toEqual(['session-c'])
  applyLiveChatEvent(entry, { v: '1.0', kind: 'event', id: 'e2', streamId: 's-c', sequence: 1, type: 'completed' })
  stop()
  expect(ticks.at(-1)).toEqual([])
})
