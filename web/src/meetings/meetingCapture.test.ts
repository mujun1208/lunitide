import { afterEach, describe, expect, test, vi } from 'vitest'
import { SYSTEM_AUDIO_HINT, captureThisPcSystemAudio, hasAudioTrack, isCaptureCanceled, stopMediaStream } from './meetingCapture'

describe('meetingCapture', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  test('throws a this-PC-only hint when display capture is unavailable', async () => {
    vi.stubGlobal('navigator', { mediaDevices: {} })
    await expect(captureThisPcSystemAudio()).rejects.toThrow(SYSTEM_AUDIO_HINT)
  })

  test('asks Chromium for this-PC system audio without treating it as a remote share', async () => {
    const stream = { getAudioTracks: () => [{ kind: 'audio' }] } as unknown as MediaStream
    const getDisplayMedia = vi.fn().mockResolvedValue(stream)
    await expect(captureThisPcSystemAudio({ getDisplayMedia })).resolves.toBe(stream)
    expect(getDisplayMedia).toHaveBeenCalledWith({ video: true, audio: true, systemAudio: 'include' })
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
  })
})
