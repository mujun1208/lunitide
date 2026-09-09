import { afterEach, expect, it, vi } from 'vitest';
import type { WebViewTransport } from '../bridge/client';

const U = '01ARZ3NDEKTSV4RRFFQ69G5FA0';
type Request = {
  id: string;
  method: string;
  payload: unknown;
  idempotencyKey?: string;
  traceId: string;
  deadlineMs: number;
};
async function harness() {
  vi.resetModules();
  const listeners = new Set<(event: MessageEvent) => void>();
  const sent: Request[] = [];
  const transport: WebViewTransport = {
    addEventListener: (_, listener) => {
      listeners.add(listener);
    },
    removeEventListener: (_, listener) => {
      listeners.delete(listener);
    },
    postMessage: (message) => {
      sent.push(message as Request);
    },
  };
  window.chrome = { webview: transport };
  const { requestOffice } = await import('../bridge/client');
  const reply = (index: number, error?: { retryable: boolean; code: string }) => {
    const data = {
      v: '1.0',
      kind: 'response',
      id: U,
      requestId: sent[index].id,
      ...(error
        ? { ok: false, error: { ...error, message: error.code, correlationId: U } }
        : { ok: true, payload: { items: [] } }),
    };
    for (const listener of listeners) listener(new MessageEvent('message', { data }));
  };
  return { requestOffice, sent, reply };
}
afterEach(() => {
  delete window.chrome;
  vi.useRealTimers();
});

it('reuses a mutation token after a retryable lost reply and resets it after success or rejection', async () => {
  const h = await harness(),
    payload = { title: '季度报告' };
  const first = h.requestOffice('office.task.create', payload).catch((error) => error);
  h.reply(0, { retryable: true, code: 'REQUEST_DEADLINE_EXCEEDED' });
  await first;
  const retry = h.requestOffice('office.task.create', { ...payload });
  expect(h.sent[1].idempotencyKey).toBe(h.sent[0].idempotencyKey);
  expect(h.sent[1].id).not.toBe(h.sent[0].id);
  h.reply(1);
  await retry;
  const next = h.requestOffice('office.task.create', payload).catch((error) => error);
  expect(h.sent[2].idempotencyKey).not.toBe(h.sent[1].idempotencyKey);
  h.reply(2, { retryable: false, code: 'INVALID_ARGUMENT' });
  await next;
  const afterRejected = h.requestOffice('office.task.create', payload);
  expect(h.sent[3].idempotencyKey).not.toBe(h.sent[2].idempotencyKey);
  h.reply(3);
  await afterRejected;
});

it('keeps the immutable payload and does not reuse a failed attempt for changed input', async () => {
  const h = await harness(),
    payload = { title: '最初的标题' };
  const first = h.requestOffice('office.task.create', payload).catch((error) => error);
  payload.title = '修改后的标题';
  expect(h.sent[0].payload).toEqual({ title: '最初的标题' });
  h.reply(0, { retryable: true, code: 'BRIDGE_UNAVAILABLE' });
  await first;
  const next = h.requestOffice('office.task.create', payload);
  expect(h.sent[1].idempotencyKey).not.toBe(h.sent[0].idempotencyKey);
  h.reply(1);
  await next;
});

it('does not let an older concurrent response discard a newer retry token', async () => {
  const h = await harness(),
    payload = { title: '同一标题' };
  const first = h.requestOffice('office.task.create', payload);
  const oldConcurrent = h.requestOffice('office.task.create', payload).catch((error) => error);
  expect(h.sent[1].idempotencyKey).toBe(h.sent[0].idempotencyKey);
  h.reply(0);
  await first;
  const newer = h.requestOffice('office.task.create', payload).catch((error) => error);
  h.reply(2, { retryable: true, code: 'BRIDGE_UNAVAILABLE' });
  await newer;
  h.reply(1, { retryable: false, code: 'CONFLICT' });
  await oldConcurrent;
  const retry = h.requestOffice('office.task.create', payload);
  const preserved = h.sent[3].idempotencyKey === h.sent[2].idempotencyKey;
  h.reply(3);
  await retry;
  expect(preserved).toBe(true);
});

it('keeps reads token-free and gives validation its bounded longer deadline', async () => {
  const h = await harness();
  const list = h.requestOffice('office.task.list', { sessionId: U });
  expect(h.sent[0].idempotencyKey).toBeUndefined();
  expect(h.sent[0].payload).toEqual({ sessionId: U });
  h.reply(0);
  await list;
  const checking = h.requestOffice('office.artifact.validate', { taskId: U, versionId: U });
  expect(h.sent[1].deadlineMs).toBe(120_000);
  expect(h.sent[1].idempotencyKey).toBeTruthy();
  h.reply(1);
  await checking;
});

it('keeps the action token after an actual client deadline expires', async () => {
  const h = await harness();
  vi.useFakeTimers();
  const first = h
    .requestOffice('office.bundle.create', { taskId: U, title: '交付包', versionIds: [U] })
    .catch((error) => error);
  await vi.advanceTimersByTimeAsync(30_251);
  expect(await first).toMatchObject({ code: 'REQUEST_DEADLINE_EXCEEDED' });
  const retry = h.requestOffice('office.bundle.create', { taskId: U, title: '交付包', versionIds: [U] });
  expect(h.sent[1].idempotencyKey).toBe(h.sent[0].idempotencyKey);
  h.reply(1);
  await retry;
});
