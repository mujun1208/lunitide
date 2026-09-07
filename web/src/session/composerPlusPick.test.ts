import {expect, test, vi} from 'vitest'
import {BridgeClientError} from '../bridge/client'
import {pickComposerFiles,readPickedFile, type DesktopFilesBridge} from './composerPlusPick'

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

test.each([
 {contentBase64:'',nextOffset:0,eof:false},
 {contentBase64:btoa('hi'),nextOffset:0,eof:false},
 {contentBase64:btoa('hi'),nextOffset:2,eof:true},
 {contentBase64:btoa('too long'),nextOffset:8,eof:true},
])('rejects stalled or corrupt native file progress %j',async chunk=>{
 const bridge={readChunk:vi.fn().mockResolvedValue(chunk)} as unknown as DesktopFilesBridge
 await expect(readPickedFile(bridge,{path:'C:/a',fileName:'a.txt',size:3,mime:'text/plain'})).rejects.toThrow('进度无效')
 expect(bridge.readChunk).toHaveBeenCalledOnce()
})
test('cancels a native read that never replies',async()=>{
 const controller=new AbortController(),bridge={readChunk:vi.fn().mockReturnValue(new Promise(()=>{}))} as unknown as DesktopFilesBridge
 const reading=readPickedFile(bridge,{path:'C:/a',fileName:'a.txt',size:3,mime:'text/plain'},controller.signal)
 controller.abort();await expect(reading).rejects.toThrow('取消')
})
test('keeps readable folder files after one native file fails',async()=>{
 const bridge={pick:vi.fn().mockResolvedValue({items:[{path:'bad',fileName:'bad.txt',size:3,mime:'text/plain'},{path:'good',fileName:'good.txt',size:2,mime:'text/plain'}]}),readChunk:vi.fn().mockRejectedValueOnce(new Error('文件已删除')).mockResolvedValue({contentBase64:btoa('ok'),nextOffset:2,eof:true})} as unknown as DesktopFilesBridge
 const result=await pickComposerFiles(bridge,true)
 expect(result.kind).toBe('files');if(result.kind==='files'){expect(result.files[0].name).toBe('good.txt');expect(result.skipped[0]).toContain('文件已删除')}
})
