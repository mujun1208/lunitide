import { afterEach, expect, it, vi } from 'vitest'
import { readBoundedFile } from './readBoundedFile'

afterEach(() => vi.useRealTimers())

it('rejects a large selection before allocating or reading its contents', async () => {
  const arrayBuffer = vi.fn()
  await expect(readBoundedFile({ size: 11 * 1024 * 1024, arrayBuffer } as unknown as File, 10 * 1024 * 1024)).rejects.toThrow('10 MiB')
  expect(arrayBuffer).not.toHaveBeenCalled()
})

it('times out a stuck read and ignores late completion so another file remains readable', async () => {
  vi.useFakeTimers()
  let complete!: (value: ArrayBuffer) => void
  const pending = readBoundedFile({ size: 1, arrayBuffer: () => new Promise(resolve => { complete = resolve }) } as File, 32)
  const failed = expect(pending).rejects.toThrow('读取文件超时')
  await vi.advanceTimersByTimeAsync(10_000)
  await failed
  complete(new ArrayBuffer(1))
  await expect(readBoundedFile({ size: 2, arrayBuffer: async () => new ArrayBuffer(2) } as File, 32)).resolves.toHaveProperty('byteLength', 2)
})

it('retains FileReader compatibility when arrayBuffer is unavailable', async () => {
  const file = new File(['text'], '报告.txt')
  Object.defineProperty(file, 'arrayBuffer', { value: undefined })
  expect(Array.from(new Uint8Array(await readBoundedFile(file, 32)))).toEqual([116, 101, 120, 116])
})
