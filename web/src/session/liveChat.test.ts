import { afterEach, expect, it, vi } from 'vitest'
import { applyLiveChatEvent, cancelLiveChatTurn, listActiveSessionIds, listActiveTurns, resetLiveChatForTests, resolveLiveApproval, retireLiveChatTurn, startLiveChat, startLiveChatWatchdog, subscribeLiveChat, subscribeLiveChatRegistry } from './liveChat'

afterEach(resetLiveChatForTests)

it('retains cache accounting when a view reconnects to a live turn', () => {
  const entry = startLiveChat('usage-session', 'usage-turn')
  const usage = {inputTokens:100,outputTokens:20,totalTokens:120,cachedInputTokens:30,cacheWriteInputTokens:10,cacheUsageReported:false}
  applyLiveChatEvent(entry, {v:'1.0',kind:'event',id:'usage-event',streamId:'usage-stream',sequence:1,type:'usage',usage})
  expect(entry.state.usage).toEqual(usage)
})

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

it('records the auto-equip signal on the live turn state', () => {
  const entry = startLiveChat('session-equip', 'turn-1')
  applyLiveChatEvent(entry, { v: '1.0', kind: 'event', id: 'e3', streamId: 's-eq', sequence: 1, type: 'equip', equip: { experts: ['PPT专家'], skills: ['slide-builder'], missingMcp: ['playwright'] } })
  expect(entry.state.equip).toEqual({ experts: ['PPT专家'], skills: ['slide-builder'], missingMcp: ['playwright'] })
  applyLiveChatEvent(entry, { v: '1.0', kind: 'event', id: 'e4', streamId: 's-eq', sequence: 2, type: 'completed' })
})

it('records when the turn started so a remount can restore the elapsed timer', () => {
  const before = Date.now()
  const entry = startLiveChat('session-start', 'turn-1')
  expect(entry.state.startedAtMs).toBeGreaterThanOrEqual(before)
  expect(entry.state.startedAtMs).toBeLessThanOrEqual(Date.now())
  expect(entry.lastEventAt).toBe(entry.state.startedAtMs)
})

it('retires a zombie turn silently without notifying listeners', () => {
  const activity = vi.fn()
  const entry = startLiveChat('session-retire', 'turn-1', activity)
  const seen: string[] = []
  subscribeLiveChat('session-retire', event => { seen.push(event.type) }, 'turn-1')
  retireLiveChatTurn(entry)
  expect(entry.terminal).toBe(true)
  expect(listActiveTurns('session-retire')).toHaveLength(0)
  expect(seen).toEqual([])
  expect(activity).toHaveBeenCalledWith(false)
})

it('watchdog retires a silent zombie turn with a synthetic cancelled event', async () => {
  vi.useFakeTimers()
  const entry = startLiveChat('session-watchdog', 'turn-1')
  const seen: string[] = []
  subscribeLiveChat('session-watchdog', event => { seen.push(event.type) }, 'turn-1')
  entry.lastEventAt = Date.now() - 301_000
  startLiveChatWatchdog({ intervalMs: 1_000, silenceMs: 300_000 })
  await vi.advanceTimersByTimeAsync(1_000)
  expect(entry.terminal).toBe(true)
  expect(entry.state.chatStatus).toBe('cancelled')
  expect(seen).toContain('cancelled')
  expect(listActiveTurns('session-watchdog')).toHaveLength(0)
  vi.useRealTimers()
})

it('watchdog spares turns awaiting approval or running a tool', async () => {
  vi.useFakeTimers()
  const digest = 'a'.repeat(64)
  const pending = startLiveChat('session-watchdog-spare', 'turn-1')
  applyLiveChatEvent(pending, { v: '1.0', kind: 'event', id: 'wd-1', streamId: 's-wd', sequence: 1, type: 'approval_required', tool: { callId: 'c1', name: 'shell.exec', argsDigest: digest } })
  const running = startLiveChat('session-watchdog-spare', 'turn-2')
  applyLiveChatEvent(running, { v: '1.0', kind: 'event', id: 'wd-2', streamId: 's-wd', sequence: 1, type: 'tool_started', tool: { callId: 'c2', name: 'web.search', argsDigest: digest } })
  pending.lastEventAt = Date.now() - 301_000
  running.lastEventAt = Date.now() - 301_000
  startLiveChatWatchdog({ intervalMs: 1_000, silenceMs: 300_000 })
  await vi.advanceTimersByTimeAsync(2_000)
  expect(pending.terminal).toBe(false)
  expect(running.terminal).toBe(false)
  vi.useRealTimers()
})

it('watchdog leaves recently active turns alone and stays idempotent', async () => {
  vi.useFakeTimers()
  const entry = startLiveChat('session-watchdog-fresh', 'turn-1')
  startLiveChatWatchdog({ intervalMs: 1_000, silenceMs: 300_000 })
  startLiveChatWatchdog()
  await vi.advanceTimersByTimeAsync(5_000)
  expect(entry.terminal).toBe(false)
  vi.useRealTimers()
})

it('clears the approval exemption once a decision is consumed', async () => {
  vi.useFakeTimers()
  const digest = 'a'.repeat(64)
  const entry = startLiveChat('session-resolve', 'turn-1')
  applyLiveChatEvent(entry, { v: '1.0', kind: 'event', id: 'rs-1', streamId: 's-rs', sequence: 1, type: 'approval_required', tool: { callId: 'c1', name: 'shell.exec', argsDigest: digest } })
  entry.lastEventAt = Date.now() - 301_000
  startLiveChatWatchdog({ intervalMs: 500, silenceMs: 300_000 })
  await vi.advanceTimersByTimeAsync(500)
  expect(entry.terminal).toBe(false)
  resolveLiveApproval('session-resolve', 'c1', 'tool_completed')
  await vi.advanceTimersByTimeAsync(500)
  expect(entry.terminal).toBe(true)
  expect(entry.state.chatStatus).toBe('cancelled')
  vi.useRealTimers()
})
