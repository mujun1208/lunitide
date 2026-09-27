import { afterEach, expect, it, vi } from 'vitest'
import { capBridgeDeadlineMs, createProductHubBridge, PRODUCT_HUB_CHECK_DEADLINE_MS, type WebViewTransport } from './client'

const U = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const probeBudgetMs = (25 + 8 + 20 + 18 + 90) * 1000

function harness() {
  let listener: (event: MessageEvent) => void = () => {}
  const sent: Array<{ id: string; method: string; deadlineMs: number }> = []
  const transport: WebViewTransport = {
    addEventListener: (_type, next) => { listener = next },
    removeEventListener: vi.fn(),
    postMessage: message => { sent.push(message as typeof sent[number]) },
  }
  const reply = (payload: unknown) => listener(new MessageEvent('message', {
    data: { v: '1.0', kind: 'response', id: U, requestId: sent.at(-1)!.id, ok: true, payload },
  }))
  return { sent, reply, bridge: createProductHubBridge(transport) }
}

afterEach(() => { vi.useRealTimers() })

it('keeps the diagnostic page open through a full fresh check', async () => {
  vi.useFakeTimers()
  const h = harness()
  const settled = vi.fn()
  const pending = h.bridge.diagnostics({ sessionToken: 'session' })
  void pending.then(settled)
  expect(h.sent[0]).toMatchObject({ method: 'productHub.diagnostics', deadlineMs: PRODUCT_HUB_CHECK_DEADLINE_MS })
  expect(PRODUCT_HUB_CHECK_DEADLINE_MS).toBeGreaterThanOrEqual(probeBudgetMs)
  await vi.advanceTimersByTimeAsync(12_000)
  expect(settled).not.toHaveBeenCalled()
  expect(h.sent).toHaveLength(1)
  h.reply({ markdown: '# 诊断报告', html: '', findings: [] })
  await expect(pending).resolves.toMatchObject({ markdown: '# 诊断报告' })
})

it('gives refresh and apply the same ceiling and leaves overview short', () => {
  expect(capBridgeDeadlineMs('productHub.diagnostics', 900_000)).toBe(PRODUCT_HUB_CHECK_DEADLINE_MS)
  expect(capBridgeDeadlineMs('productHub.refresh', 900_000)).toBe(PRODUCT_HUB_CHECK_DEADLINE_MS)
  expect(capBridgeDeadlineMs('productHub.apply', 900_000)).toBe(PRODUCT_HUB_CHECK_DEADLINE_MS)
  expect(capBridgeDeadlineMs('productHub.overview', 900_000)).toBe(30_000)
  const h = harness()
  const token = { sessionToken: 'session' }
  void h.bridge.refresh(token)
  void h.bridge.apply(token)
  void h.bridge.overview(token)
  expect(h.sent.map(item => item.deadlineMs)).toEqual([
    PRODUCT_HUB_CHECK_DEADLINE_MS,
    PRODUCT_HUB_CHECK_DEADLINE_MS,
    12_000,
  ])
})
