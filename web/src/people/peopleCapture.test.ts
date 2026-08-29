import { afterEach, describe, expect, test, vi } from 'vitest'
import { BridgeClientError } from '../bridge/client'
import { CAPTURE_HINT, canvasToJpegFile, captureThisPcFrame } from './peopleCapture'

describe('peopleCapture', () => {
  const jpegUrl = 'data:image/jpeg;base64,QQ=='
  const pngBytes = Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='), c => c.charCodeAt(0))

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  test('throws a this-PC-only hint when display capture is unavailable', async () => {
    vi.stubGlobal('navigator', { mediaDevices: {} })
    await expect(captureThisPcFrame()).rejects.toThrow(CAPTURE_HINT)
  })

  test('prefers native engine capture without opening the browser share picker', async () => {
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue(jpegUrl)
    vi.stubGlobal('createImageBitmap', vi.fn(async (blob: Blob) => {
      const bitmap = document.createElement('canvas')
      bitmap.width = 1
      bitmap.height = 1
      Object.assign(bitmap, { close: vi.fn() })
      return bitmap
    }))
    const getDisplayMedia = vi.fn()
    const nativeCapture = vi.fn().mockResolvedValue({
      contentBase64: btoa(String.fromCharCode(...pngBytes)),
      mimeType: 'image/png',
    })
    const file = await captureThisPcFrame({
      maxBytes: 180 * 1024,
      getDisplayMedia,
      nativeCapture,
    })
    expect(nativeCapture).toHaveBeenCalledOnce()
    expect(getDisplayMedia).not.toHaveBeenCalled()
    expect(file.type).toBe('image/jpeg')
    expect(file.name).toMatch(/^screenshot-/)
  })

  test('falls back to display capture only when native capture is unsupported', async () => {
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue(jpegUrl)
    const stop = vi.fn()
    const stream = { getTracks: () => [{ stop }] } as unknown as MediaStream
    const canvas = document.createElement('canvas')
    canvas.width = 8
    canvas.height = 8
    const getDisplayMedia = vi.fn().mockResolvedValue(stream)
    const nativeCapture = vi.fn().mockRejectedValue(Object.assign(new Error('unsupported'), { code: 'PEOPLE_CAPTURE_UNSUPPORTED' }))
    const file = await captureThisPcFrame({
      maxBytes: 180 * 1024,
      getDisplayMedia,
      grabFrame: async () => canvas,
      nativeCapture,
    })
    expect(nativeCapture).toHaveBeenCalledOnce()
    expect(getDisplayMedia).toHaveBeenCalledWith({ video: true, audio: false })
    expect(stop).toHaveBeenCalledOnce()
    expect(file.type).toBe('image/jpeg')
  })

  test('falls back to display capture when native capture fails at runtime', async () => {
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue(jpegUrl)
    const stop = vi.fn()
    const stream = { getTracks: () => [{ stop }] } as unknown as MediaStream
    const canvas = document.createElement('canvas')
    canvas.width = 8
    canvas.height = 8
    const getDisplayMedia = vi.fn().mockResolvedValue(stream)
    const nativeCapture = vi.fn().mockRejectedValue(new BridgeClientError('无法截取本机画面', 'PEOPLE_CAPTURE_FAILED', false, 'trace'))
    const file = await captureThisPcFrame({
      maxBytes: 180 * 1024,
      getDisplayMedia,
      grabFrame: async () => canvas,
      nativeCapture,
    })
    expect(nativeCapture).toHaveBeenCalledOnce()
    expect(getDisplayMedia).toHaveBeenCalledWith({ video: true, audio: false })
    expect(file.type).toBe('image/jpeg')
  })

  test('stops display tracks after grabbing a frame', async () => {
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue(jpegUrl)
    const stop = vi.fn()
    const stream = { getTracks: () => [{ stop }] } as unknown as MediaStream
    const canvas = document.createElement('canvas')
    canvas.width = 8
    canvas.height = 8
    const getDisplayMedia = vi.fn().mockResolvedValue(stream)
    const file = await captureThisPcFrame({
      maxBytes: 180 * 1024,
      getDisplayMedia,
      preferNative: false,
      grabFrame: async () => canvas,
    })
    expect(getDisplayMedia).toHaveBeenCalledWith({ video: true, audio: false })
    expect(stop).toHaveBeenCalledOnce()
    expect(file.type).toBe('image/jpeg')
    expect(file.name).toMatch(/^screenshot-/)
    expect(file.size).toBeGreaterThan(0)
  })

  test('compresses a canvas under the send limit', async () => {
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue(jpegUrl)
    const canvas = document.createElement('canvas')
    canvas.width = 16
    canvas.height = 16
    const file = await canvasToJpegFile(canvas, 180 * 1024, 'shot.jpg')
    expect(file.size).toBeLessThanOrEqual(180 * 1024)
    expect(file.name).toBe('shot.jpg')
  })
})
