import { BridgeClientError, type TemplateBridge } from '../bridge/client'

/** Match attachment.upload.chunk transport-safe size (32 KiB raw → ≤43692 base64). */
export const TEMPLATE_STAGE_CHUNK = 32 * 1024
export const TEMPLATE_INLINE_MAX = 32 * 1024

export type TemplateStageProgress = (percent: number, chunk: number, totalChunks: number) => void

function bytesToB64(bytes: Uint8Array): string {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}

function newUploadId(): string {
  const alphabet = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'
  const extra = crypto.getRandomValues(new Uint8Array(10))
  let value = (BigInt(Date.now()) << 80n) | extra.reduce((n, x) => (n << 8n) | BigInt(x), 0n)
  let out = ''
  for (let i = 0; i < 26; i++) {
    out = alphabet[Number(value & 31n)] + out
    value >>= 5n
  }
  return out
}

function stageError(error: unknown, chunk: number, totalChunks: number): Error {
  if (error instanceof BridgeClientError) {
    if (error.code === 'REQUEST_DEADLINE_EXCEEDED') {
      return new Error(`分片上传超时（${chunk}/${totalChunks}），引擎未及时响应，请稍后重试`)
    }
    if (error.code === 'HOST_BUSY') {
      return new Error('桌面主机正忙，请稍后重试分片上传')
    }
    if (error.code === 'BRIDGE_REQUEST_TOO_LARGE') {
      return new Error('分片过大，无法通过 Bridge 发送，请换较小文件')
    }
    return new Error(error.message || '分片上传失败')
  }
  return error instanceof Error ? error : new Error('分片上传失败')
}

export async function stageTemplateFile(
  templates: TemplateBridge,
  file: File,
  ready: Uint8Array,
  onProgress?: TemplateStageProgress,
): Promise<string> {
  const buf = ready
  const uploadId = newUploadId()
  const totalChunks = Math.max(1, Math.ceil(buf.length / TEMPLATE_STAGE_CHUNK))
  let offset = 0
  let index = 0
  let stagedId = ''
  while (offset < buf.length) {
    const end = Math.min(offset + TEMPLATE_STAGE_CHUNK, buf.length)
    const chunkNo = index + 1
    onProgress?.(Math.min(99, Math.round((offset / Math.max(1, buf.length)) * 100)), chunkNo, totalChunks)
    try {
      const staged = await templates.fileStage({
        uploadId,
        fileName: file.name,
        fileMime: file.type,
        index,
        last: end === buf.length,
        contentBase64: bytesToB64(buf.subarray(offset, end)),
      })
      if (end === buf.length && !staged.ready) {
        throw new Error('文件分片上传未完成')
      }
      stagedId = staged.uploadId
    } catch (error) {
      throw stageError(error, chunkNo, totalChunks)
    }
    offset = end
    index += 1
  }
  if (!stagedId) throw new Error('文件分片上传未完成')
  onProgress?.(100, totalChunks, totalChunks)
  return stagedId
}

export function bytesToBase64(bytes: Uint8Array): string {
  return bytesToB64(bytes)
}
