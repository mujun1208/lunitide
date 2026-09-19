import { expect, it } from 'vitest'
import { AGENT_HUB_DIR_PICK_MS, capBridgeDeadlineMs, createAgentHubBridge, createMediaBridge, type WebViewTransport } from './client'

const RID = '01ARZ3NDEKTSV4RRFFQ69G5FAZ'

function rejectThenAccept(deadlines: number[]): WebViewTransport {
  const listeners: Array<(event: MessageEvent) => void> = []
  return {
    postMessage(value: unknown) {
      const message = value as { id: string; deadlineMs: number; traceId: string }
      deadlines.push(message.deadlineMs)
      const ok = deadlines.length > 1
      queueMicrotask(() => {
        listeners.forEach(fn => fn({
          data: ok
            ? { v: '1.0', kind: 'response', id: RID, requestId: message.id, ok: true, payload: { ok: true } }
            : { v: '1.0', kind: 'response', id: RID, requestId: message.id, ok: false, error: { code: 'BRIDGE_SCHEMA_INVALID', message: '请求超时参数无效', retryable: false, correlationId: message.traceId } },
        } as MessageEvent))
      })
    },
    addEventListener(_type: 'message', listener: (event: MessageEvent) => void) { listeners.push(listener) },
    removeEventListener() {},
  }
}

it('lets agentHub.dir.pick and agentHub.inbox wait for native dialogs', () => {
  expect(capBridgeDeadlineMs('agentHub.dir.pick', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
  expect(capBridgeDeadlineMs('agentHub.inbox', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
  expect(capBridgeDeadlineMs('agentHub.detect', AGENT_HUB_DIR_PICK_MS)).toBe(30_000)
})

it('lets agentHub.install wait as long as a native dialog', () => {
  expect(capBridgeDeadlineMs('agentHub.install', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
})

it('lets agentHub.thread.prompt wait past the default 30s cap', () => {
  expect(capBridgeDeadlineMs('agentHub.thread.create', 180_000)).toBe(180_000)
  expect(capBridgeDeadlineMs('agentHub.thread.prompt', 180_000)).toBe(180_000)
  expect(capBridgeDeadlineMs('agentHub.thread.respond', 180_000)).toBe(180_000)
  expect(capBridgeDeadlineMs('agentHub.detect', 180_000)).toBe(30_000)
})

it('lets project.root.pick wait for the same native dialog cap', () => {
  expect(capBridgeDeadlineMs('project.root.pick', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
})

it('lets media.asset.pick wait for the native file dialog', () => {
  expect(capBridgeDeadlineMs('media.asset.pick', 120_000)).toBe(120_000)
  expect(capBridgeDeadlineMs('system.health', 120_000)).toBe(30_000)
})

it('lets chat.start use the same 120s cap as the engine', () => {
  expect(capBridgeDeadlineMs('chat.start', 120_000)).toBe(120_000)
})

it('retries a Cursor prompt at 30s when the host still rejects the 180s cap', async () => {
  const deadlines: number[] = []
  await createAgentHubBridge(rejectThenAccept(deadlines)).request('agentHub.thread.prompt', { threadId: RID, text: 'hi' })
  expect(deadlines).toEqual([180_000, 30_000])
})

it('retries media.asset.pick at 30s when the host still rejects the 120s cap', async () => {
  const deadlines: number[] = []
  await createMediaBridge(rejectThenAccept(deadlines)).pick({ scopeKind: 'user', multiple: true })
  expect(deadlines).toEqual([120_000, 30_000])
})
