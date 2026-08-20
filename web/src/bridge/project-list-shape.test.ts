import { expect, it, vi } from 'vitest'
import { createProjectBridge, type WebViewTransport } from './client'

const U = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

function listTransport(payload: unknown): WebViewTransport {
  let listener: (e: MessageEvent) => void = () => {}
  return {
    addEventListener: (_t, l) => {
      listener = l
    },
    removeEventListener: vi.fn(),
    postMessage: (m) =>
      queueMicrotask(() =>
        listener(
          new MessageEvent('message', {
            data: { v: '1.0', kind: 'response', id: U, requestId: (m as { id: string }).id, ok: true, payload },
          }),
        ),
      ),
  }
}

it('accepts engine-shaped project lists with empty close fields and RFC3339Nano times', async () => {
  const dto = {
    id: U,
    name: 'Moon Tide',
    projectCode: 'ITM00001',
    type: 'implementation',
    description: '',
    summary: '',
    objective: '',
    client: '',
    contractNo: '',
    amount: 0,
    budget: 0,
    planStart: '',
    planEnd: '',
    remark: '',
    closeReason: '',
    statusBeforeClose: '',
    reopenReason: '',
    status: 'created',
    orgId: U,
    createdAt: '2026-08-20T09:01:12.1234567Z',
    updatedAt: '2026-08-20T09:01:12.1234567Z',
    version: 1,
  }
  await expect(createProjectBridge(listTransport({ items: [dto] })).list()).resolves.toEqual({ items: [dto] })
})
