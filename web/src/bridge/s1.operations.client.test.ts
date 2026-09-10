import {expect, it, vi} from 'vitest'
import {createFilesBridge, createMutationAttempt, createOperationBridge, type WebViewTransport} from './client'

const U = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

function harness(result: unknown) {
  let listener: (e: MessageEvent) => void = () => {}
  const sent: Array<Record<string, unknown>> = []
  const transport: WebViewTransport = {
    addEventListener: (_t, l) => { listener = l as (e: MessageEvent) => void },
    removeEventListener: vi.fn(),
    postMessage: m => {
      sent.push(m as Record<string, unknown>)
      queueMicrotask(() => listener(new MessageEvent('message', {
        data: {v: '1.0', kind: 'response', id: U, requestId: (m as {id: string}).id, ok: true, payload: result},
      })))
    },
  }
  return {sent, transport}
}

it('lists operations without inventing a resume execution', async () => {
  const listed = {items: [{
    id: U, toolName: 'files.apply', effectClass: 'write', state: 'succeeded',
    expectedVersion: 1, attempt: 1, resumeAction: 'show_existing',
  }]}
  const {sent, transport} = harness(listed)
  const out = await createOperationBridge(transport).list({sessionId: U, limit: 20})
  expect(sent[0]).toMatchObject({method: 'operation.list', payload: {sessionId: U, limit: 20}})
  expect(sent[0]).not.toHaveProperty('idempotencyKey')
  expect(out).toEqual(listed)
  expect(JSON.stringify(out)).not.toContain('executed')
})

it('sends cancel with idempotency and keeps resume verify-only', async () => {
  const receipt = {
    id: U, toolName: 'files.apply', effectClass: 'write', state: 'cancelled',
    expectedVersion: 2, attempt: 1, resumeAction: 'keep_stopped', executed: false,
  }
  const {sent, transport} = harness(receipt)
  const ops = createOperationBridge(transport)
  const cancelPayload = {sessionId: U, operationId: U, expectedVersion: 1}
  await ops.cancel(cancelPayload, {attempt: createMutationAttempt('operation.cancel', cancelPayload)})
  expect(sent[0]).toMatchObject({method: 'operation.cancel', idempotencyKey: expect.any(String)})
  const resumePayload = {sessionId: U, operationId: U, expectedVersion: 2}
  const resumed = await ops.resume(resumePayload, {attempt: createMutationAttempt('operation.resume', resumePayload)})
  expect(sent[1]).toMatchObject({method: 'operation.resume'})
  expect(resumed.executed).toBe(false)
})

it('sends files.status without mutation keys and plans with idempotency', async () => {
  const {sent, transport} = harness({planId: U, state: 'planned', steps: []})
  const files = createFilesBridge(transport)
  await files.status({sessionId: U, planId: U})
  expect(sent[0]).toMatchObject({method: 'files.status', payload: {sessionId: U, planId: U}})
  expect(sent[0]).not.toHaveProperty('idempotencyKey')
  const plan = {sessionId: U, recipe: 'weekly-report' as const}
  await files.plan(plan, {attempt: createMutationAttempt('files.plan', plan)})
  expect(sent[1]).toMatchObject({method: 'files.plan', idempotencyKey: expect.any(String)})
})
