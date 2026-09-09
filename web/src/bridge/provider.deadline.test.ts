import { afterEach, expect, it, vi } from 'vitest'
import { capBridgeDeadlineMs, createProviderBridge, PROVIDER_TEST_DEADLINE_MS, type WebViewTransport } from './client'

const U = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

function harness() {
  let listener: (event: MessageEvent) => void = () => {}
  const sent: Array<{ id: string; method: string; deadlineMs: number }> = []
  const transport: WebViewTransport = {
    addEventListener: (_type, next) => { listener = next },
    removeEventListener: vi.fn(),
    postMessage: message => { sent.push(message as typeof sent[number]) },
  }
  const reply = (payload: unknown) => listener(new MessageEvent('message', {
    data: { v: '1.0', kind: 'response', id: U, requestId: sent[0].id, ok: true, payload },
  }))
  return { sent, reply, bridge: createProviderBridge(transport, 600_000) }
}

afterEach(() => { vi.useRealTimers() })

it('keeps provider.test pending through a 135 second video job without resubmission', async () => {
  vi.useFakeTimers()
  const h = harness()
  const settled = vi.fn()
  const pending = h.bridge.test({ providerId: U, modelId: 'doubao-seedance-2-0-260128' })
  void pending.then(settled)
  expect(h.sent[0]).toMatchObject({ method: 'provider.test', deadlineMs: PROVIDER_TEST_DEADLINE_MS })
  await vi.advanceTimersByTimeAsync(135_000)
  expect(settled).not.toHaveBeenCalled()
  expect(h.sent).toHaveLength(1)
  h.reply({ status: 'passed', stage: 'response', latencyMs: 135_000, retryable: false, testedAt: new Date().toISOString() })
  await expect(pending).resolves.toMatchObject({ status: 'passed', latencyMs: 135_000 })
  expect(h.sent).toHaveLength(1)
})

it('times out once at the diagnostic ceiling and never retries a paid probe', async () => {
  vi.useFakeTimers()
  const h = harness()
  const pending = expect(h.bridge.test({ providerId: U })).rejects.toMatchObject({ code: 'REQUEST_DEADLINE_EXCEEDED' })
  await vi.advanceTimersByTimeAsync(PROVIDER_TEST_DEADLINE_MS + 251)
  await pending
  expect(h.sent).toHaveLength(1)
  h.reply({ status: 'passed', stage: 'response', latencyMs: 360_000, retryable: false, testedAt: new Date().toISOString() })
  await vi.runAllTimersAsync()
  expect(h.sent).toHaveLength(1)
})

it('retains ordinary provider and discovery caps', async () => {
  expect(PROVIDER_TEST_DEADLINE_MS).toBeGreaterThanOrEqual(300_000)
  expect(capBridgeDeadlineMs('provider.test', 900_000)).toBe(PROVIDER_TEST_DEADLINE_MS)
  expect(capBridgeDeadlineMs('provider.model.sync', 900_000)).toBe(30_000)
  expect(capBridgeDeadlineMs('system.health', 900_000)).toBe(30_000)
  const h = harness()
  const pending = h.bridge.list()
  expect(h.sent[0].deadlineMs).toBe(30_000)
  h.reply({ items: [] })
  await pending
})
