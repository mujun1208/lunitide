import { webcrypto } from 'node:crypto'
import { afterEach, expect, it, vi } from 'vitest'
import type { SkillBridge } from '../bridge/client'
import type { SkillDTO } from '../generated/bridge'
import { uploadSkillPackage } from './skillPackageUpload'

const skill = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', status: 'draft' } as SkillDTO
const fixture = () => ({
  uploadBegin: vi.fn(async () => ({ uploadId: '01ARZ3NDEKTSV4RRFFQ69G5FAB', chunkSize: 65536 as const })),
  uploadChunk: vi.fn(async (p: {offset: number; dataBase64: string}) => ({ received: p.offset + atob(p.dataBase64).length })),
  uploadCommit: vi.fn(async () => ({ skill })), uploadAbort: vi.fn(async () => ({ aborted: true })),
} as unknown as SkillBridge)
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })

it('uploads exact ZIP binary and hashes complete bytes without parsing JSON', async () => {
  vi.stubGlobal('crypto', webcrypto)
  const bytes = new Uint8Array(131079); bytes.set([80, 75, 3, 4]); bytes[65536] = 255; bytes[131078] = 128
  const api = fixture(), result = await uploadSkillPackage(api, new File([bytes], '技能.zip'))
  expect(result).toBe(skill)
  expect(api.uploadBegin).toHaveBeenCalledWith({ name: '技能.zip', size: bytes.length, sha256: expect.stringMatching(/^[0-9a-f]{64}$/) })
  expect(vi.mocked(api.uploadChunk!).mock.calls.map(([p]) => p.offset)).toEqual([0, 65536, 131072])
  const combined = vi.mocked(api.uploadChunk!).mock.calls.flatMap(([p]) => Array.from(atob(p.dataBase64), char => char.charCodeAt(0)))
  expect(new Uint8Array(combined)).toEqual(bytes)
  expect(api.uploadAbort).not.toHaveBeenCalled()
})

it('retries the same committed upload after a lost receipt, without uploading or creating another package', async () => {
  vi.stubGlobal('crypto', webcrypto)
  const api = fixture(), file = new File(['{"name":"draft"}'], 'draft.json')
  vi.mocked(api.uploadCommit!).mockRejectedValueOnce(new Error('回执丢失'))
  await expect(uploadSkillPackage(api, file)).rejects.toThrow('回执丢失')
  expect(api.uploadAbort).not.toHaveBeenCalled()
  expect(await uploadSkillPackage(api, new File(['{"name":"draft"}'], 'draft.json'))).toBe(skill)
  expect(api.uploadBegin).toHaveBeenCalledTimes(1)
  expect(api.uploadChunk).toHaveBeenCalledTimes(1)
  expect(vi.mocked(api.uploadCommit!).mock.calls[0][0]).toEqual(vi.mocked(api.uploadCommit!).mock.calls[1][0])
})

it('aborts incomplete uploads when a receipt has a wrong byte offset', async () => {
  vi.stubGlobal('crypto', webcrypto)
  const api = fixture(); vi.mocked(api.uploadChunk!).mockResolvedValue({ received: 1 })
  await expect(uploadSkillPackage(api, new File(['abcd'], 'bad.zip'))).rejects.toThrow('进度不一致')
  expect(api.uploadAbort).toHaveBeenCalledOnce(); expect(api.uploadCommit).not.toHaveBeenCalled()
})

it('does not send unverified files or malformed size to the bridge', async () => {
  const api = fixture()
  vi.stubGlobal('crypto', {})
  await expect(uploadSkillPackage(api, new File(['abcd'], 'a.zip'))).rejects.toThrow('不支持文件校验')
  await expect(uploadSkillPackage(api, new File(['a'], 'a.zip'))).rejects.toThrow('2 字节至 8 MiB')
  expect(api.uploadBegin).not.toHaveBeenCalled()
})

it('cancels before commit and releases the server upload', async () => {
  vi.stubGlobal('crypto', webcrypto)
  const api = fixture(), controller = new AbortController()
  vi.mocked(api.uploadChunk!).mockImplementation(async () => { controller.abort(); return { received: 4 } })
  await expect(uploadSkillPackage(api, new File(['abcd'], 'cancel.zip'), undefined, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
  expect(api.uploadAbort).toHaveBeenCalledOnce(); expect(api.uploadCommit).not.toHaveBeenCalled()
})
