import { afterEach, beforeEach, expect, test, vi } from 'vitest'

const omni = vi.hoisted(() => ({
  status: vi.fn(),
  ensure: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
  append: vi.fn(),
}))

const capture = vi.hoisted(() => ({
  onFrame: undefined as ((frame: { base64: string }) => void) | undefined,
  muted: false,
}))

vi.mock('../../bridge/client', () => ({
  getOmniBridge: () => omni,
}))

vi.mock('../companion/pcmCapture', () => ({
  startPcmCapture: async (options: { onFrame: (frame: { base64: string }) => void }) => {
    capture.onFrame = options.onFrame
    return {
      stop: async () => {},
      setMuted: (muted: boolean) => {
        capture.muted = muted
      },
      contextSampleRate: () => 16000,
      flush: () => {},
    }
  },
}))

vi.mock('../companion/ttsPlayer', () => ({
  unlockTtsAudio: () => Promise.resolve(),
  sharedTtsAudioContext: () => null,
}))

import { OMNI_MISSING_RUNTIME, OMNI_PROBE_MS, OMNI_APPEND_MS, omniChannelAvailable, omniStartBlock, probeOmniChannel, startOmniCompanion } from './omniAudio'

beforeEach(() => {
  omni.status.mockReset()
  omni.ensure.mockReset()
  omni.start.mockReset()
  omni.stop.mockReset().mockResolvedValue(undefined)
  omni.append.mockReset()
  capture.onFrame = undefined
  capture.muted = false
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

test('holds PCM until commitUserAudio so MiniCPM-o answers after the user turn, not mid-sentence', async () => {
  omni.ensure.mockResolvedValue({ ready: true, hostState: 'ready' })
  omni.start.mockResolvedValue({ sessionId: 'omni-1' })
  omni.append.mockResolvedValue({ text: '在的。我在听。', listening: false, wavs: [] })
  const onText = vi.fn()
  const handle = await startOmniCompanion({
    personaId: 'refpack:优质台湾腔.wav',
    onText,
    onError: vi.fn(),
  })
  capture.onFrame?.({ base64: 'AAAA' })
  capture.onFrame?.({ base64: 'BBBB' })
  expect(omni.append).not.toHaveBeenCalled()
  expect(handle.commitUserAudio()).toBe(true)
  await vi.waitFor(() => expect(omni.append).toHaveBeenCalledTimes(2))
  expect(omni.append.mock.calls[0][0]).toEqual({ sessionId: 'omni-1', pcm: 'AAAA' })
  await vi.waitFor(() => expect(onText).toHaveBeenCalled())
  expect(onText.mock.calls.at(-1)?.[0]).toContain('我在听')
  handle.stop()
})
