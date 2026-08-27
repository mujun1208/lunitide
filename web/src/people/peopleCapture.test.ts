import { afterEach, describe, expect, test, vi } from 'vitest'
import { CAPTURE_HINT, canvasToJpegFile, captureThisPcFrame } from './peopleCapture'

describe('peopleCapture', () => {
  const jpegUrl = 'data:image/jpeg;base64,QQ=='

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  test('throws a this-PC-only hint when display capture is unavailable', async () => {
    vi.stubGlobal('navigator', { mediaDevices: {} })
    await expect(captureThisPcFrame()).rejects.toThrow(CAPTURE_HINT)
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
