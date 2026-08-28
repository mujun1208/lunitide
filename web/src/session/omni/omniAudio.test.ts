import { afterEach, beforeEach, expect, test, vi } from 'vitest'

const omni = vi.hoisted(() => ({
  status: vi.fn(),
  ensure: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
}))

vi.mock('../../bridge/client', () => ({
  getOmniBridge: () => omni,
}))

import { OMNI_MISSING_RUNTIME, OMNI_PROBE_MS, OMNI_APPEND_MS, omniChannelAvailable, omniStartBlock, probeOmniChannel } from './omniAudio'

beforeEach(() => {
  omni.status.mockReset()
})

afterEach(() => {
  vi.useRealTimers()
})

test('omniStartBlock treats missing model as a download, not a missing server', () => {
  expect(omniStartBlock({ hostState: 'missing_model' })).toBe('请先在设置里下载 MiniCPM-o 4.5 Q4')
  expect(omniStartBlock({ hostState: 'missing_runtime' })).toBe(OMNI_MISSING_RUNTIME)
  expect(OMNI_MISSING_RUNTIME).not.toMatch(/omni\/runtime/)
  expect(OMNI_MISSING_RUNTIME).not.toMatch(/放到/)
  expect(omniStartBlock({ hostState: 'idle' })).toBeUndefined()
})

test('omniChannelAvailable is false when the runtime or model is missing', () => {
  expect(omniChannelAvailable({ ready: false, installed: false, runtimeFound: false, hostState: 'missing_runtime' })).toBe(false)
  expect(omniChannelAvailable({ ready: false, installed: false, runtimeFound: false, hostState: 'missing_model' })).toBe(false)
  expect(omniChannelAvailable({ ready: false, installed: true, runtimeFound: false, hostState: 'missing_runtime' })).toBe(false)
  expect(omniChannelAvailable({ ready: true, hostState: 'ready', installed: true, runtimeFound: true })).toBe(true)
  expect(omniChannelAvailable({ ready: false, hostState: 'launching', installed: true, runtimeFound: true })).toBe(true)
  expect(omniChannelAvailable({ ready: false, hostState: 'idle', installed: true, runtimeFound: true })).toBe(true)
})

test('probeOmniChannel is false when status hangs past the budget', async () => {
  vi.useFakeTimers()
  omni.status.mockReturnValue(new Promise(() => {}))
  const pending = probeOmniChannel()
  await vi.advanceTimersByTimeAsync(OMNI_PROBE_MS)
  await expect(pending).resolves.toBe(false)
})

test('probeOmniChannel is false when the bridge rejects', async () => {
  omni.status.mockRejectedValue(new Error('timeout'))
  await expect(probeOmniChannel()).resolves.toBe(false)
})

test('a hung MiniCPM-o append is bounded so duplex cannot stall the conversation', () => {
  expect(OMNI_APPEND_MS).toBeGreaterThanOrEqual(4000)
  expect(OMNI_APPEND_MS).toBeLessThanOrEqual(12_000)
})
