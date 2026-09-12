import { afterEach, expect, it, vi } from 'vitest';
import {
  OFFICE_ARTIFACT_FOCUS_EVENT,
  OFFICE_STUDIO_OPEN_EVENT,
  focusOfficeArtifact,
  requestOfficeStudio,
  type OfficeArtifactFocus,
  type OfficeOpenRequest,
} from './officeNavigation';

afterEach(() => vi.useRealTimers());
it('focuses an artifact on the current office task without opening a new route', () => {
  let captured: OfficeArtifactFocus | undefined;
  const listener = (event: Event) => {
    captured = (event as CustomEvent<OfficeArtifactFocus>).detail;
  };
  window.addEventListener(OFFICE_ARTIFACT_FOCUS_EVENT, listener, { once: true });
  focusOfficeArtifact('01ARZ3NDEKTSV4RRFFQ69G5FA0', 'desktop/节奏图.pptx');
  expect(captured).toEqual({ taskId: '01ARZ3NDEKTSV4RRFFQ69G5FA0', path: 'desktop/节奏图.pptx' });
});

it('passes the exact session and artifact while loading no separate chat state', async () => {
  let captured: OfficeOpenRequest | undefined;
  const listener = (event: Event) => {
    captured = (event as CustomEvent<OfficeOpenRequest>).detail;
    captured.finish();
  };
  window.addEventListener(OFFICE_STUDIO_OPEN_EVENT, listener, { once: true });
  await requestOfficeStudio('session-original', 'desktop/报告.docx');
  expect(captured?.sessionId).toBe('session-original');
  expect(captured?.path).toBe('desktop/报告.docx');
});

it('returns a disabled feature response without requesting any replacement conversation', async () => {
  const listener = (event: Event) =>
    (event as CustomEvent<OfficeOpenRequest>).detail.finish('FEATURE_DISABLED: 办公工作台已关闭');
  window.addEventListener(OFFICE_STUDIO_OPEN_EVENT, listener, { once: true });
  await expect(requestOfficeStudio('session-original', 'report.docx')).rejects.toThrow('FEATURE_DISABLED');
});

it('marks expired navigation so a late bridge result cannot unexpectedly navigate away', async () => {
  vi.useFakeTimers();
  let captured: OfficeOpenRequest | undefined;
  window.addEventListener(
    OFFICE_STUDIO_OPEN_EVENT,
    (event) => {
      captured = (event as CustomEvent<OfficeOpenRequest>).detail;
    },
    { once: true },
  );
  const pending = requestOfficeStudio('session-original', 'report.docx');
  const rejected = expect(pending).rejects.toThrow('超时');
  await vi.advanceTimersByTimeAsync(35_000);
  await rejected;
  expect(captured?.cancelled()).toBe(true);
  captured?.finish();
});
