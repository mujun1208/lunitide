import { expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createMediaBridge, type WebViewTransport } from './client'

const U = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const TOKEN = 'abcdefghijklmnopqrst0123456789'
const VIRTUAL = `https://media.lunitide.local/v1/assets/${TOKEN}`
const EXPIRES = '2026-10-08T00:00:00Z'

// openAsset 走 createSimpleBridge（无结果 guard），响应直通后由
// rewriteRemotePlaybackUrl 在出口重写 playbackUrl。
function harness(playbackUrl: string) {
  let listener: (e: MessageEvent) => void = () => {}
  const transport: WebViewTransport = {
    addEventListener: (_t, l) => { listener = l },
    removeEventListener: vi.fn(),
    postMessage: m => {
      queueMicrotask(() => listener(new MessageEvent('message', { data: { v: '1.0', kind: 'response', id: U, requestId: (m as any).id, ok: true, payload: { playbackUrl, expiresAt: EXPIRES } } })))
    },
  }
  return createMediaBridge(transport)
}

beforeEach(() => localStorage.clear())
afterEach(() => localStorage.clear())

it('rewrites the virtual media origin to the gateway origin for remote wss sessions', async () => {
  localStorage.setItem('lunitide:remote-bridge', JSON.stringify({ wsUrl: 'wss://192.0.2.10:47651/bridge', token: 'tok' }))
  const result = await harness(VIRTUAL).openAsset({ assetId: U, mediaSessionId: U })
  expect(result.playbackUrl).toBe(`https://192.0.2.10:47651/media/assets/${TOKEN}`)
  expect(result.expiresAt).toBe(EXPIRES)
})

it('maps insecure ws sessions onto plain http gateway origins', async () => {
  localStorage.setItem('lunitide:remote-bridge', JSON.stringify({ wsUrl: 'ws://192.0.2.10:47651/bridge', token: 'tok' }))
  const result = await harness(VIRTUAL).openAsset({ assetId: U, mediaSessionId: U })
  expect(result.playbackUrl).toBe(`http://192.0.2.10:47651/media/assets/${TOKEN}`)
})

it('keeps the virtual origin untouched on desktop sessions without credentials', async () => {
  const result = await harness(VIRTUAL).openAsset({ assetId: U, mediaSessionId: U })
  expect(result.playbackUrl).toBe(VIRTUAL)
})

it('keeps the virtual origin when stored credentials are unusable', async () => {
  for (const raw of ['not-json', JSON.stringify({ wsUrl: 1 }), JSON.stringify({ wsUrl: '' }), JSON.stringify({}), JSON.stringify({ wsUrl: '192.0.2.10 nope' }), JSON.stringify({ wsUrl: 'ftp://host/x' })]) {
    localStorage.setItem('lunitide:remote-bridge', raw)
    const result = await harness(VIRTUAL).openAsset({ assetId: U, mediaSessionId: U })
    expect(result.playbackUrl).toBe(VIRTUAL)
  }
})

it('leaves non-virtual playback URLs alone even in remote sessions', async () => {
  localStorage.setItem('lunitide:remote-bridge', JSON.stringify({ wsUrl: 'wss://192.0.2.10:47651/bridge', token: 'tok' }))
  for (const url of ['https://example.test/v1/assets/t', 'https://media.lunitide.local/v2/assets/t', 'media.lunitide.local/v1/assets/t']) {
    const result = await harness(url).openAsset({ assetId: U, mediaSessionId: U })
    expect(result.playbackUrl).toBe(url)
  }
})
