import { afterEach, describe, expect, test, vi } from 'vitest'
import {
  SYSTEM_AUDIO_HINT,
  captureLoopbackInputDevice,
  captureThisPcSystemAudio,
  displayMediaConstraints,
  fallbackDisplayMediaConstraints,
  hasAudioTrack,
  hasLiveAudioTrack,
  isCaptureCanceled,
  isLoopbackInputLabel,
  stopMediaStream,
  watchAudioTrackEnded,
} from './meetingCapture'

function audioStream(readyState: MediaStreamTrack['readyState'] | undefined = 'live'): MediaStream {
  const track = { kind: 'audio', readyState, stop: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn() }
  return { getAudioTracks: () => [track], getTracks: () => [track] } as unknown as MediaStream
}

describe('meetingCapture', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  test('throws a this-PC-only hint when display capture is unavailable', async () => {
    vi.stubGlobal('navigator', { mediaDevices: {} })
    await expect(captureThisPcSystemAudio()).rejects.toThrow(SYSTEM_AUDIO_HINT)
  })

  test('prefers entire-screen systemAudio so Feishu window share is not required', async () => {
    const stream = audioStream()
    const getDisplayMedia = vi.fn().mockResolvedValue(stream)
    await expect(captureThisPcSystemAudio({ getDisplayMedia, enumerateDevices: async () => [] })).resolves.toBe(stream)
    expect(getDisplayMedia).toHaveBeenCalledWith(displayMediaConstraints())
  })

  test('retries display media when the first pick has video but no live audio', async () => {
    const empty = { getAudioTracks: () => [], getTracks: () => [{ stop: vi.fn() }] } as unknown as MediaStream
    const mixed = audioStream()
    const getDisplayMedia = vi.fn().mockResolvedValueOnce(empty).mockResolvedValueOnce(mixed)
    await expect(captureThisPcSystemAudio({ getDisplayMedia, enumerateDevices: async () => [] })).resolves.toBe(mixed)
    expect(getDisplayMedia).toHaveBeenNthCalledWith(1, displayMediaConstraints())
    expect(getDisplayMedia).toHaveBeenNthCalledWith(2, fallbackDisplayMediaConstraints())
  })

  test('uses a Stereo Mix / loopback input without opening the picker', async () => {
    const stream = audioStream()
    const getUserMedia = vi.fn().mockResolvedValue(stream)
    const getDisplayMedia = vi.fn()
    await expect(captureThisPcSystemAudio({
      getDisplayMedia,
      getUserMedia,
      enumerateDevices: async () => [{ kind: 'audioinput', deviceId: 'mix', label: '立体声混音' } as MediaDeviceInfo],
    })).resolves.toBe(stream)
    expect(getDisplayMedia).not.toHaveBeenCalled()
    expect(getUserMedia).toHaveBeenCalled()
  })

  test('treats picker cancel as canceled, not as a silent mic-only start', () => {
    expect(isCaptureCanceled(new DOMException('Permission denied', 'NotAllowedError'))).toBe(true)
    expect(isCaptureCanceled(new DOMException('interrupted', 'AbortError'))).toBe(true)
    expect(isCaptureCanceled(new Error('无法启动'))).toBe(false)
  })

  test('stops every track, including the unused video track from the picker', () => {
    const stop = vi.fn()
    const stream = { getTracks: () => [{ stop }, { stop }] } as unknown as MediaStream
    stopMediaStream(stream)
    expect(stop).toHaveBeenCalledTimes(2)
    expect(hasAudioTrack({ getAudioTracks: () => [] } as unknown as MediaStream)).toBe(false)
    expect(hasAudioTrack({ getAudioTracks: () => [{}] } as unknown as MediaStream)).toBe(true)
    expect(hasLiveAudioTrack({ getAudioTracks: () => [{ readyState: 'ended' }] } as unknown as MediaStream)).toBe(false)
  })

  test('watchAudioTrackEnded restarts when the system-audio track dies', () => {
    const listeners = new Map<string, () => void>()
    const track = {
      kind: 'audio',
      readyState: 'live',
      addEventListener: (name: string, fn: () => void) => listeners.set(name, fn),
      removeEventListener: (name: string) => listeners.delete(name),
    }
    const stream = { getAudioTracks: () => [track] } as unknown as MediaStream
    const onEnded = vi.fn()
    const stop = watchAudioTrackEnded(stream, onEnded)
    listeners.get('ended')?.()
    expect(onEnded).toHaveBeenCalledOnce()
    stop()
    expect(listeners.has('ended')).toBe(false)
  })

  test('watchCaptureTracksEnded fires when the keep-alive video track dies', () => {
    const listeners = new Map<string, () => void>()
    const video = {
      kind: 'video',
      readyState: 'live',
      addEventListener: (name: string, fn: () => void) => listeners.set(`video:${name}`, fn),
      removeEventListener: (name: string) => listeners.delete(`video:${name}`),
    }
    const stream = { getAudioTracks: () => [], getVideoTracks: () => [video] } as unknown as MediaStream
    const onEnded = vi.fn()
    watchAudioTrackEnded(stream, onEnded)
    listeners.get('video:ended')?.()
    expect(onEnded).toHaveBeenCalledOnce()
  })

  test('display keep-alive video is large enough Chromium will not end capture at ~2 minutes', () => {
    const video = displayMediaConstraints().video as { width?: number; height?: number; frameRate?: number }
    expect(video.width ?? 0).toBeGreaterThanOrEqual(320)
    expect(video.height ?? 0).toBeGreaterThanOrEqual(180)
    expect(video.frameRate ?? 0).toBeGreaterThanOrEqual(2)
  })

  test('recognizes Windows loopback device labels', () => {
    expect(isLoopbackInputLabel('Stereo Mix (Realtek)')).toBe(true)
    expect(isLoopbackInputLabel('立体声混音')).toBe(true)
    expect(isLoopbackInputLabel('Microphone')).toBe(false)
  })

  test('non-interactive recover only tries the loopback device', async () => {
    const getDisplayMedia = vi.fn()
    await expect(captureLoopbackInputDevice({
      getDisplayMedia,
      enumerateDevices: async () => [{ kind: 'audioinput', deviceId: 'mic', label: 'Microphone' } as MediaDeviceInfo],
    })).resolves.toBeUndefined()
    expect(getDisplayMedia).not.toHaveBeenCalled()
  })
})
