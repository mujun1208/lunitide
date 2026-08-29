import { describe, expect, it, vi } from 'vitest'
import { BridgeClientError, type PeopleBridge } from '../bridge/client'
import { PEOPLE_STAGE_CHUNK, stageBrowserFile } from './peopleStage'

function file(name: string, bytes: Uint8Array): File {
  const blob = new File([bytes as BlobPart], name, { type: 'image/png' })
  Object.defineProperty(blob, 'arrayBuffer', { value: async () => bytes.buffer.slice(0) })
  return blob
}

describe('stageBrowserFile', () => {
  it('uploads chunks with progress and returns the staged path', async () => {
    const bytes = Uint8Array.from({ length: PEOPLE_STAGE_CHUNK + 1024 }, (_, i) => i % 251)
    const stages: Array<{ index: number; last: boolean }> = []
    const progress: Array<[number, number, number]> = []
    const people = {
      fileStage: vi.fn(async (payload: { index: number; last: boolean }) => {
        stages.push({ index: payload.index, last: payload.last })
        return payload.last ? { localPath: 'C:/stage/up-demo.png', ready: true, bytes: bytes.length } : { bytes: PEOPLE_STAGE_CHUNK, ready: false, localPath: '' }
      }),
    } as unknown as PeopleBridge
    const path = await stageBrowserFile(people, file('demo.png', bytes), bytes, (percent, chunk, total) => {
      progress.push([percent, chunk, total])
    })
    expect(path).toBe('C:/stage/up-demo.png')
    expect(stages).toEqual([{ index: 0, last: false }, { index: 1, last: true }])
    expect(progress.at(-1)).toEqual([100, 2, 2])
    expect(people.fileStage).toHaveBeenCalledTimes(2)
  })

  it('keeps each chunk base64 within the transport-safe bridge limit', async () => {
    const bytes = Uint8Array.from({ length: PEOPLE_STAGE_CHUNK * 3 + 512 }, (_, i) => i % 251)
    const payloads: Array<{ contentBase64: string; last: boolean }> = []
    const people = {
      fileStage: vi.fn(async (payload: { contentBase64: string; last: boolean }) => {
        payloads.push(payload)
        return payload.last ? { localPath: 'C:/stage/up-demo.png', ready: true, bytes: bytes.length } : { bytes: PEOPLE_STAGE_CHUNK, ready: false, localPath: '' }
      }),
    } as unknown as PeopleBridge
    await stageBrowserFile(people, file('飞算AI.png', bytes), bytes)
    expect(payloads).toHaveLength(4)
    expect(Math.max(...payloads.map(p => p.contentBase64.length))).toBeLessThanOrEqual(43692)
  })

  it('maps bridge timeout to a user-facing chunk error', async () => {
    const bytes = Uint8Array.from([1, 2, 3, 4])
    const people = {
      fileStage: vi.fn().mockRejectedValue(new BridgeClientError('Bridge 请求超时', 'REQUEST_DEADLINE_EXCEEDED', true, 'trace')),
    } as unknown as PeopleBridge
    await expect(stageBrowserFile(people, file('飞算AI.png', bytes), bytes)).rejects.toThrow(/分片上传超时（1\/1）/)
  })
})
