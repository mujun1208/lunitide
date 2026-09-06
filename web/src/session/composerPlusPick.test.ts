import {expect, test, vi} from 'vitest'
import {BridgeClientError} from '../bridge/client'
import {pickComposerFiles, type DesktopFilesBridge} from './composerPlusPick'

test('falls back when no host bridge is wired', async () => {
  await expect(pickComposerFiles(undefined, false)).resolves.toEqual({kind: 'fallback'})
})

test('treats user cancel as silence', async () => {
  const bridge: DesktopFilesBridge = {
    pick: vi.fn().mockResolvedValue({canceled: true, items: []}),
    readChunk: vi.fn(),
  }
  await expect(pickComposerFiles(bridge, false)).resolves.toEqual({kind: 'canceled'})
  expect(bridge.readChunk).not.toHaveBeenCalled()
})

test('file pick with zero items is DESKTOP_PICK_FAILED', async () => {
  const bridge: DesktopFilesBridge = {
    pick: vi.fn().mockResolvedValue({canceled: false, items: []}),
    readChunk: vi.fn(),
  }
  const result = await pickComposerFiles(bridge, false)
  expect(result.kind).toBe('error')
  if (result.kind === 'error') expect(result.error.code).toBe('DESKTOP_PICK_FAILED')
})

test('folder pick with zero whitelist files is DESKTOP_FOLDER_EMPTY', async () => {
  const bridge: DesktopFilesBridge = {
    pick: vi.fn().mockResolvedValue({canceled: false, items: [], skipped: ['setup.exe']}),
    readChunk: vi.fn(),
  }
  const result = await pickComposerFiles(bridge, true)
  expect(result.kind).toBe('error')
  if (result.kind === 'error') {
    expect(result.error.code).toBe('DESKTOP_FOLDER_EMPTY')
    expect(result.error.message).toContain('没有可导入的文件')
    expect(result.error.message).toContain('setup.exe')
  }
})

test('folder pick keeps accepted files and names skipped exe', async () => {
  const bridge: DesktopFilesBridge = {
    pick: vi.fn().mockResolvedValue({canceled: false, items: [{path: 'C:/notes.txt', fileName: 'notes.txt', mime: 'text/plain', size: 2}], skipped: ['setup.exe']}),
    readChunk: vi.fn().mockResolvedValue({contentBase64: btoa('hi'), nextOffset: 2, eof: true}),
  }
  const result = await pickComposerFiles(bridge, true)
  expect(result.kind).toBe('files')
  if (result.kind === 'files') {
    expect(result.files[0].name).toBe('notes.txt')
    expect(result.skipped).toEqual(['setup.exe'])
  }
})

test('unavailable host falls back to the hidden input', async () => {
  const bridge: DesktopFilesBridge = {
    pick: vi.fn().mockRejectedValue(new BridgeClientError('系统没打开文件框，请再试一次。', 'DESKTOP_PICK_UNAVAILABLE', false, 'host')),
    readChunk: vi.fn(),
  }
  await expect(pickComposerFiles(bridge, false)).resolves.toEqual({kind: 'fallback'})
})

test('reads allowlisted chunks into a File', async () => {
  const bridge: DesktopFilesBridge = {
    pick: vi.fn().mockResolvedValue({canceled: false, items: [{path: 'C:/a.txt', fileName: 'a.txt', mime: 'text/plain', size: 5}]}),
    readChunk: vi.fn().mockResolvedValue({contentBase64: btoa('hello'), nextOffset: 5, eof: true}),
  }
  const result = await pickComposerFiles(bridge, false)
  expect(result.kind).toBe('files')
  if (result.kind === 'files') {
    expect(result.files[0].name).toBe('a.txt')
    expect(result.files[0].size).toBe(5)
  }
})
