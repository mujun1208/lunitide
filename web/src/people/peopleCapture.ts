import { BridgeClientError } from '../bridge/client'

const CAPTURE_HINT = '当前环境无法截取本机画面。可粘贴截图或选择图片。这不会把桌面共享给其他电脑。'
const NATIVE_FALLBACK_CODES = new Set([
  'PEOPLE_CAPTURE_UNSUPPORTED',
  'PEOPLE_CAPTURE_FAILED',
  'REQUEST_DEADLINE_EXCEEDED',
  'BRIDGE_UNAVAILABLE',
])

export type CaptureThisPcOptions = {
  maxBytes?: number
  getDisplayMedia?: MediaDevices['getDisplayMedia']
  grabFrame?: (stream: MediaStream) => Promise<HTMLCanvasElement>
  /** Prefer engine native capture (no browser share picker). Defaults to true. */
  preferNative?: boolean
  nativeCapture?: () => Promise<{ contentBase64: string; mimeType: string }>
}

function dataUrlToBlob(dataUrl: string): Blob {
  if (!dataUrl.includes(',')) throw new Error('无法编码截图')
  const [head, b64] = dataUrl.split(',')
  const mime = head.match(/:(.*?);/)?.[1] || 'image/jpeg'
  const binary = atob(b64 || '')
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return new Blob([bytes], { type: mime })
}

export async function canvasToJpegFile(canvas: HTMLCanvasElement, maxBytes: number, fileName: string): Promise<File> {
  let quality = 0.82
  let width = canvas.width || 1
  let height = canvas.height || 1
  let blob: Blob | null = null
  for (let i = 0; i < 8; i++) {
    const frame = document.createElement('canvas')
    frame.width = Math.max(1, Math.round(width))
    frame.height = Math.max(1, Math.round(height))
    const ctx = frame.getContext('2d')
    if (!ctx) throw new Error('无法绘制本机画面')
    ctx.drawImage(canvas, 0, 0, frame.width, frame.height)
    const dataUrl = typeof frame.toDataURL === 'function' ? frame.toDataURL('image/jpeg', quality) : ''
    if (!dataUrl || !dataUrl.includes(',')) throw new Error('无法编码截图')
    blob = dataUrlToBlob(dataUrl)
    if (blob.size <= maxBytes) break
    quality = Math.max(0.4, quality - 0.12)
    width *= 0.82
    height *= 0.82
  }
  if (!blob) throw new Error('无法编码截图')
  if (blob.size > maxBytes) throw new Error('截图过大，请改选窗口或粘贴较小的图片')
  return new File([blob], fileName, { type: 'image/jpeg' })
}

async function pngBase64ToJpegFile(contentBase64: string, maxBytes: number, fileName: string): Promise<File> {
  const binary = atob(contentBase64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  const blob = new Blob([bytes], { type: 'image/png' })
  if (typeof createImageBitmap !== 'function') throw new Error(CAPTURE_HINT)
  const bitmap = await createImageBitmap(blob)
  try {
    const canvas = document.createElement('canvas')
    canvas.width = bitmap.width || 1
    canvas.height = bitmap.height || 1
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('无法绘制本机画面')
    ctx.drawImage(bitmap, 0, 0)
    return canvasToJpegFile(canvas, maxBytes, fileName)
  } finally {
    bitmap.close()
  }
}

async function grabVideoFrame(stream: MediaStream): Promise<HTMLCanvasElement> {
  const video = document.createElement('video')
  video.muted = true
  video.playsInline = true
  video.srcObject = stream
  await video.play()
  await new Promise<void>(resolve => {
    if (video.readyState >= 2) {
      resolve()
      return
    }
    video.onloadeddata = () => resolve()
  })
  const canvas = document.createElement('canvas')
  canvas.width = video.videoWidth || 1
  canvas.height = video.videoHeight || 1
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('无法绘制本机画面')
  ctx.drawImage(video, 0, 0)
  return canvas
}

function shouldFallbackFromNative(err: unknown): boolean {
  if (err instanceof BridgeClientError) return NATIVE_FALLBACK_CODES.has(err.code)
  if (!err || typeof err !== 'object') return false
  const code = 'code' in err ? String((err as { code?: string }).code ?? '') : ''
  return NATIVE_FALLBACK_CODES.has(code)
}

async function captureViaDisplayMedia(options: CaptureThisPcOptions, maxBytes: number, fileName: string): Promise<File> {
  const native = navigator.mediaDevices?.getDisplayMedia
  const getDisplayMedia = options.getDisplayMedia ?? (native ? native.bind(navigator.mediaDevices) : undefined)
  if (!getDisplayMedia) throw new Error(CAPTURE_HINT)
  const stream = await getDisplayMedia.call(navigator.mediaDevices ?? {}, { video: true, audio: false })
  try {
    const canvas = await (options.grabFrame ?? grabVideoFrame)(stream)
    return canvasToJpegFile(canvas, maxBytes, fileName)
  } finally {
    stream.getTracks().forEach(track => track.stop())
  }
}

export async function captureThisPcFrame(options: CaptureThisPcOptions = {}): Promise<File> {
  const maxBytes = options.maxBytes ?? 512 * 1024
  const stamp = new Date().toISOString().replace(/[:.]/g, '').slice(0, 15)
  const fileName = `screenshot-${stamp}.jpg`
  const preferNative = options.preferNative !== false

  if (preferNative && options.nativeCapture) {
    try {
      const shot = await options.nativeCapture()
      return pngBase64ToJpegFile(shot.contentBase64, maxBytes, fileName)
    } catch (err) {
      if (!shouldFallbackFromNative(err)) throw err
    }
  }

  return captureViaDisplayMedia(options, maxBytes, fileName)
}

export { CAPTURE_HINT }
