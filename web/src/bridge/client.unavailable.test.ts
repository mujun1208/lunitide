// A bridge facade is reached from React render paths and effects. If it
// throws synchronously because WebView2 has not injected its host object
// yet, React unmounts the whole root and the window goes blank with no way
// back. These tests pin the contract: a bridge call always returns a
// promise, and a late WebView2 still connects the singletons that were
// built before it existed.
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { WebViewTransport } from './client'

const U = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

/** Echoes every request back as a valid empty-list response. */
function fakeHost(): WebViewTransport & { sent: unknown[] } {
  const listeners = new Set<(event: MessageEvent) => void>()
  const sent: unknown[] = []
  return {
    sent,
    addEventListener: (_type, listener) => listeners.add(listener as (event: MessageEvent) => void),
    removeEventListener: (_type, listener) => listeners.delete(listener as (event: MessageEvent) => void),
    postMessage: message => {
      sent.push(message)
      const data = { v: '1.0', kind: 'response', id: U, requestId: (message as { id: string }).id, ok: true, payload: { items: [] } }
      queueMicrotask(() => {
        for (const listener of [...listeners]) listener(new MessageEvent('message', { data }))
      })
    },
  }
}

beforeEach(() => {
  vi.resetModules()
  delete (window as { chrome?: unknown }).chrome
})

afterEach(() => {
  delete (window as { chrome?: unknown }).chrome
})

test('facades reject instead of throwing when WebView2 has not arrived', async () => {
  const client = await import('./client')
  // One from each historical shape: the hand-wrapped facades and the many
  // that reached the host object directly.
  const calls: Array<[string, () => Promise<unknown>]> = [
    ['providerBridge', () => client.providerBridge.list({})],
    ['projectBridge', () => client.projectBridge.list({})],
    ['sessionBridge', () => client.sessionBridge.list({ projectId: U })],
    ['skillBridge', () => client.skillBridge.list({})],
    ['mcpBridge', () => client.mcpBridge.list({})],
    ['memoryBridge', () => client.memoryBridge.list({ projectId: U })],
    ['pluginBridge', () => client.pluginBridge.list({})],
    ['uiThemeBridge', () => client.uiThemeBridge.set({ theme: 'dark' })],
  ]

  for (const [name, call] of calls) {
    let promise: Promise<unknown> | undefined
    expect(() => {
      promise = call()
    }, `${name} must not throw synchronously`).not.toThrow()
    await expect(promise, name).rejects.toMatchObject({ code: 'BRIDGE_UNAVAILABLE' })
  }
})

test('a singleton built before WebView2 existed still works once it arrives', async () => {
  const client = await import('./client')
  // Builds the singleton (and registers its response listener) with no host.
  await expect(client.providerBridge.list({})).rejects.toMatchObject({ code: 'BRIDGE_UNAVAILABLE' })

  const host = fakeHost()
  ;(window as { chrome?: { webview?: WebViewTransport } }).chrome = { webview: host }

  // The same singleton must now reach the host, and the listener it
  // registered early must receive the reply.
  await expect(client.providerBridge.list({})).resolves.toEqual({ items: [] })
  expect(host.sent).toHaveLength(1)
})
