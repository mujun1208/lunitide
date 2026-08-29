import { expect, it, vi } from 'vitest'
import {
  capBridgeDeadlineMs,
  createPeopleBridge,
  PEOPLE_CAPTURE_DEADLINE_MS,
  PEOPLE_FILE_DEADLINE_MS,
  type WebViewTransport,
} from './client'

const U = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

function peopleHarness() {
  let listener: (e: MessageEvent) => void = () => {}
  const sent: Array<{ method: string; deadlineMs: number }> = []
  const transport: WebViewTransport = {
    addEventListener: (_t, l) => { listener = l as (e: MessageEvent) => void },
    removeEventListener: vi.fn(),
    postMessage: m => {
      const request = m as { id: string; method: string; deadlineMs: number }
      sent.push({ method: request.method, deadlineMs: request.deadlineMs })
      queueMicrotask(() => listener(new MessageEvent('message', {
        data: { v: '1.0', kind: 'response', id: U, requestId: request.id, ok: true, payload: { localPath: 'C:/tmp/x' } },
      })))
    },
  }
  return { sent, transport, bridge: createPeopleBridge(transport) }
}

it('allows a longer deadline for region screenshots', () => {
  expect(capBridgeDeadlineMs('people.screen.capture', PEOPLE_CAPTURE_DEADLINE_MS)).toBe(PEOPLE_CAPTURE_DEADLINE_MS)
  expect(capBridgeDeadlineMs('people.file.stage', PEOPLE_FILE_DEADLINE_MS)).toBe(PEOPLE_FILE_DEADLINE_MS)
  expect(capBridgeDeadlineMs('people.thread.send', PEOPLE_FILE_DEADLINE_MS)).toBe(PEOPLE_FILE_DEADLINE_MS)
  expect(capBridgeDeadlineMs('system.health', PEOPLE_FILE_DEADLINE_MS)).toBe(30_000)
})

it('requests people.file.stage with the extended deadline', async () => {
  const { sent, bridge } = peopleHarness()
  await bridge.fileStage({
    uploadId: U,
    fileName: 'a.png',
    fileMime: 'image/png',
    index: 0,
    last: true,
    contentBase64: 'AQID',
  })
  expect(sent[0]?.method).toBe('people.file.stage')
  expect(sent[0]?.deadlineMs).toBe(PEOPLE_FILE_DEADLINE_MS)
})

it('requests people.screen.capture with the snip deadline', async () => {
  const { sent, bridge } = peopleHarness()
  await bridge.screenCapture({})
  expect(sent[0]?.method).toBe('people.screen.capture')
  expect(sent[0]?.deadlineMs).toBe(PEOPLE_CAPTURE_DEADLINE_MS)
})
