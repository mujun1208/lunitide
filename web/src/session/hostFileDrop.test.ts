import { afterEach, expect, it, vi } from 'vitest'
import { claimDroppedFiles, hostDropCovers, rememberHostDrop, resolveHostFilePaths, subscribeHostFileDrop } from './hostFileDrop'

type Listener = (event: MessageEvent) => void

function fakeWebView(reply: (message: { kind?: string; token?: string }, objects: unknown[]) => unknown) {
  const listeners = new Set<Listener>()
  const view = {
    postMessage: vi.fn(),
    postMessageWithAdditionalObjects: vi.fn((message: { kind?: string; token?: string }, objects: unknown[]) => {
      const data = reply(message, objects)
      if (data !== undefined) queueMicrotask(() => listeners.forEach(listener => listener({ data } as MessageEvent)))
    }),
    addEventListener: (_: string, listener: Listener) => listeners.add(listener),
    removeEventListener: (_: string, listener: Listener) => listeners.delete(listener),
  }
  Object.defineProperty(window, 'chrome', { value: { webview: view }, configurable: true })
  return { view, listeners }
}

afterEach(() => { Reflect.deleteProperty(window, 'chrome') })

it('remembers only the files the host just registered', () => {
  const now = 1_000
  rememberHostDrop([{ fileName: '需求.docx' }], now)
  expect(hostDropCovers(['需求.docx'], now + 100)).toBe(true)
  expect(hostDropCovers(['其他.txt'], now + 100)).toBe(false)
  expect(hostDropCovers(['需求.docx'], now + 10_000)).toBe(false)
})

it('turns dropped files into host paths through the WebView file objects', async () => {
  const file = new File(['x'], '需求.docx')
  const { view, listeners } = fakeWebView((message, objects) => ({ source: 'lunitide-host', type: 'filesResolved', token: message.token, items: objects.length === 1 ? [{ path: 'C:/Users/me/Desktop/需求.docx', fileName: '需求.docx', mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', size: 1 }] : [] }))
  await expect(resolveHostFilePaths([file])).resolves.toEqual([{ path: 'C:/Users/me/Desktop/需求.docx', fileName: '需求.docx', mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', size: 1 }])
  expect(view.postMessageWithAdditionalObjects).toHaveBeenCalledWith({ kind: 'lunitide.files.resolve', token: expect.any(String) }, [file])
  expect(listeners.size).toBe(0)
})

it('falls back when the host resolves only part of the batch', async () => {
  fakeWebView(message => ({ source: 'lunitide-host', type: 'filesResolved', token: message.token, items: [{ path: 'C:/a.txt', fileName: 'a.txt', mime: 'text/plain', size: 1 }] }))
  await expect(resolveHostFilePaths([new File(['a'], 'a.txt'), new File(['b'], 'b.txt')])).resolves.toBeUndefined()
})

it('ignores replies for another request and gives up when the host stays silent', async () => {
  fakeWebView(() => ({ source: 'lunitide-host', type: 'filesResolved', token: 'someone-else', items: [{ path: 'C:/a.txt', fileName: 'a.txt', mime: 'text/plain', size: 1 }] }))
  await expect(resolveHostFilePaths([new File(['a'], 'a.txt')], 30)).resolves.toBeUndefined()
})

it('imports one drop once whether the host drop hook or path resolution lands first', async () => {
  const { listeners } = fakeWebView(message => ({ source: 'lunitide-host', type: 'filesResolved', token: message.token, items: [{ path: 'C:/方案.pdf', fileName: '方案.pdf', mime: 'application/pdf', size: 1 }] }))
  const hostDrops = vi.fn()
  const stop = subscribeHostFileDrop(hostDrops)
  await expect(claimDroppedFiles([new File(['p'], '方案.pdf')])).resolves.toEqual([{ path: 'C:/方案.pdf', fileName: '方案.pdf', mime: 'application/pdf', size: 1 }])
  listeners.forEach(listener => listener({ data: { source: 'lunitide-host', type: 'filesDropped', items: [{ path: 'C:/方案.pdf', fileName: '方案.pdf', mime: 'application/pdf', size: 1 }] } } as MessageEvent))
  expect(hostDrops).not.toHaveBeenCalled()
  listeners.forEach(listener => listener({ data: { source: 'lunitide-host', type: 'filesDropped', items: [{ path: 'C:/报价.xlsx', fileName: '报价.xlsx', mime: 'application/octet-stream', size: 1 }] } } as MessageEvent))
  expect(hostDrops).toHaveBeenCalledOnce()
  await expect(claimDroppedFiles([new File(['q'], '报价.xlsx')])).resolves.toBe('claimed')
  stop()
})

it('uses the upload path when the WebView cannot hand over file objects', async () => {
  Object.defineProperty(window, 'chrome', { value: { webview: { postMessage: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn() } }, configurable: true })
  await expect(resolveHostFilePaths([new File(['a'], 'a.txt')])).resolves.toBeUndefined()
})
