import { describe, expect, it, vi } from 'vitest'
import { BridgeClientError, type TemplateBridge } from '../bridge/client'
import { TEMPLATE_STAGE_CHUNK, stageTemplateFile } from './assetStage'

function file(name: string, bytes: Uint8Array): File {
  const blob = new File([bytes as BlobPart], name, { type: 'application/octet-stream' })
  Object.defineProperty(blob, 'arrayBuffer', { value: async () => bytes.buffer.slice(0) })
  return blob
}

describe('stageTemplateFile', () => {
  it('uploads chunks with progress and returns the upload id', async () => {
    const bytes = Uint8Array.from({ length: TEMPLATE_STAGE_CHUNK + 1024 }, (_, i) => i % 251)
    const stages: Array<{ index: number; last: boolean }> = []
    const progress: Array<[number, number, number]> = []
    const templates = {
      fileStage: vi.fn(async (payload: { uploadId: string; index: number; last: boolean }) => {
        stages.push({ index: payload.index, last: payload.last })
        return payload.last
          ? { ready: true, uploadId: payload.uploadId, bytes: bytes.length }
          : { ready: false, uploadId: payload.uploadId, bytes: TEMPLATE_STAGE_CHUNK }
      }),
    } as unknown as TemplateBridge
    const uploadId = await stageTemplateFile(templates, file('blueprint.dot', bytes), bytes, (percent, chunk, total) => {
      progress.push([percent, chunk, total])
    })
    expect(uploadId).toHaveLength(26)
    expect(stages).toEqual([{ index: 0, last: false }, { index: 1, last: true }])
    expect(progress.at(-1)).toEqual([100, 2, 2])
    expect(templates.fileStage).toHaveBeenCalledTimes(2)
  })

  it('keeps each chunk base64 within the transport-safe bridge limit', async () => {
    const bytes = Uint8Array.from({ length: TEMPLATE_STAGE_CHUNK * 3 + 512 }, (_, i) => i % 251)
    const payloads: Array<{ contentBase64: string; last: boolean }> = []
    const templates = {
      fileStage: vi.fn(async (payload: { uploadId: string; contentBase64: string; last: boolean }) => {
        payloads.push(payload)
        return payload.last
          ? { ready: true, uploadId: payload.uploadId, bytes: bytes.length }
          : { ready: false, uploadId: payload.uploadId, bytes: TEMPLATE_STAGE_CHUNK }
      }),
    } as unknown as TemplateBridge
    await stageTemplateFile(templates, file('blueprint.dot', bytes), bytes)
    expect(payloads).toHaveLength(4)
    expect(Math.max(...payloads.map(p => p.contentBase64.length))).toBeLessThanOrEqual(43692)
  })

  it('maps bridge timeout to a user-facing chunk error', async () => {
    const bytes = Uint8Array.from([1, 2, 3, 4])
    const templates = {
      fileStage: vi.fn().mockRejectedValue(new BridgeClientError('Bridge 请求超时', 'REQUEST_DEADLINE_EXCEEDED', true, 'trace')),
    } as unknown as TemplateBridge
    await expect(stageTemplateFile(templates, file('blueprint.dot', bytes), bytes)).rejects.toThrow(/分片上传超时（1\/1）/)
  })
})
