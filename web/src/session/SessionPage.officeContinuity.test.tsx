import React from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import {
  BridgeClientError,
  runQueueBridge,
  type MessageBridge,
  type SessionBridge,
  type SkillBridge,
  type ExpertBridge,
  type ChatBridge,
  type ProviderBridge,
  type StreamEvent,
} from '../bridge/client';
import type { MessageDTO, ProjectDTO, SessionDTO, SkillDTO, ProviderDTO } from '../generated/bridge';
import { SessionPage } from './SessionPage';
import { readSessionComposerDraft, writeSessionComposerDraft } from './sessionComposerDraft';
import { resetLiveChatForTests } from './liveChat';

const P = '01ARZ3NDEKTSV4RRFFQ69G5FA0',
  S = '01ARZ3NDEKTSV4RRFFQ69G5FA1',
  now = '2026-09-07T00:00:00Z';
const project: ProjectDTO = {
  id: P,
  name: '办公任务',
  projectCode: 'ITM00001',
  type: 'implementation',
  status: 'active',
  createdAt: now,
  updatedAt: now,
  version: 1,
};
const session: SessionDTO = {
  id: S,
  projectId: P,
  title: '办公原对话',
  pinned: false,
  status: 'active',
  createdAt: now,
  updatedAt: now,
  version: 1,
};
const skill: SkillDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FA2',
  name: 'report-writer',
  displayName: '报告助手',
  description: '整理报告',
  version: '1.0.0',
  rev: 1,
  status: 'published',
  permissions: ['read_write'],
  entryPoint: 'builtin://report-writer',
  manifestJson: '{}',
  category: 'writing',
  categorySource: 'keyword',
  createdAt: now,
  updatedAt: now,
};
const empty = () => ({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 });
function fixtures() {
  return {
    project,
    initialSession: session,
    initialNoAutoSend: true,
    personal: true,
    bridge: {
      list: vi.fn(async () => ({ items: [session] })),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
    } as SessionBridge,
    messages: { list: vi.fn(async () => empty()), append: vi.fn(async () => ({})) } as unknown as MessageBridge,
    skills: { list: vi.fn(async () => ({ items: [skill] })) } as unknown as SkillBridge,
    experts: {
      list: vi.fn(async () => ({ experts: [] })),
      sessionMountGet: vi.fn(async () => ({ expertIds: [] })),
    } as unknown as ExpertBridge,
    onBack: vi.fn(),
  };
}
const input = () => screen.getByPlaceholderText('输入消息，或使用 / 触发技能，@ 引用产物...');
beforeEach(() => {
  vi.spyOn(runQueueBridge, 'list').mockResolvedValue({ items: [] });
  vi.spyOn(runQueueBridge, 'consume').mockResolvedValue({ count: 0, items: [] });
});
afterEach(() => {
  cleanup();
  resetLiveChatForTests();
  vi.restoreAllMocks();
  localStorage.clear();
});

it('keeps the original unsent text, skills and committed attachment cards across Office view remounts', async () => {
  const props = fixtures();
  writeSessionComposerDraft(props.messages, P, S, {
    text: '整理这份资料',
    referencedSkills: [skill],
    pendingAttachmentIds: ['attachment-1'],
    uploadProgress: [
      { key: 'file-1', status: 'complete', percent: 100, name: '资料.txt', size: 40, attachmentId: 'attachment-1' },
    ],
  });
  const first = render(<SessionPage {...props} />);
  await waitFor(() => expect(input()).toHaveProperty('value', '整理这份资料'));
  expect(screen.getByText('报告助手')).toBeTruthy();
  expect(screen.getByText('资料.txt')).toBeTruthy();
  fireEvent.change(input(), { target: { value: '尚未发送的修改要求' } });
  first.unmount();
  render(<SessionPage {...props} />);
  await waitFor(() => expect(input()).toHaveProperty('value', '尚未发送的修改要求'));
  expect(screen.getByText('报告助手')).toBeTruthy();
  expect(screen.getByText('资料.txt')).toBeTruthy();
  expect(props.messages.append).not.toHaveBeenCalled();
});

it('isolates drafts by project and session and does not delete a new session that has unsent work', async () => {
  const props = fixtures();
  const first = render(<SessionPage {...props} />);
  await screen.findByText('还没有消息');
  fireEvent.change(input(), { target: { value: '留在原会话的草稿' } });
  fireEvent.click(screen.getByRole('button', { name: /返回主页/ }));
  await waitFor(() => expect(props.onBack).toHaveBeenCalled());
  expect(props.bridge.delete).not.toHaveBeenCalled();
  first.unmount();
  render(<SessionPage {...props} initialSession={{ ...session, id: '01ARZ3NDEKTSV4RRFFQ69G5FA9' }} />);
  await waitFor(() => expect(input()).toHaveProperty('value', ''));
  expect(readSessionComposerDraft(props.messages, P, S)?.text).toBe('留在原会话的草稿');
  expect(readSessionComposerDraft(props.messages, 'other-project', S)).toBeUndefined();
});

it.each([{ keepEmptySession: true }, { officeTaskId: '01ARZ3NDEKTSV4RRFFQ69G5FA3' }])('preserves an empty session already owned by a task on return: %j', async scope => {
  const props = fixtures();
  render(<SessionPage {...props} {...scope} backLabel="返回执行记录" />);
  await screen.findByText('还没有消息');
  fireEvent.click(screen.getByRole('button', { name: /返回执行记录/ }));
  await waitFor(() => expect(props.onBack).toHaveBeenCalledOnce());
  expect(props.bridge.delete).not.toHaveBeenCalled();
});

it('retains an unsent draft after a failed send and clears it only after the successful receipt', async () => {
  const props = fixtures();
  vi.mocked(props.messages.append).mockRejectedValueOnce(
    new BridgeClientError('发送暂未完成', 'BRIDGE_UNAVAILABLE', true, 'test'),
  );
  let view = render(<SessionPage {...props} />);
  await screen.findByText('还没有消息');
  fireEvent.change(input(), { target: { value: '需要保留的发送内容' } });
  fireEvent.submit(input().closest('form')!);
  await screen.findByText('发送暂未完成');
  view.unmount();
  view = render(<SessionPage {...props} />);
  await waitFor(() => expect(input()).toHaveProperty('value', '需要保留的发送内容'));
  fireEvent.submit(input().closest('form')!);
  await waitFor(() => expect(input()).toHaveProperty('value', ''));
  view.unmount();
  render(<SessionPage {...props} />);
  await waitFor(() => expect(input()).toHaveProperty('value', ''));
  expect(props.messages.append).toHaveBeenCalledTimes(2);
});

it('handles a late successful send after leaving without replaying that text on return', async () => {
  const props = fixtures();
  let finish!: (value: MessageDTO) => void;
  vi.mocked(props.messages.append).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const view = render(<SessionPage {...props} />);
  await screen.findByText('还没有消息');
  fireEvent.change(input(), { target: { value: '已经发送，等待确认' } });
  fireEvent.submit(input().closest('form')!);
  view.unmount();
  await act(async () => {
    finish({} as MessageDTO);
  });
  render(<SessionPage {...props} />);
  await waitFor(() => expect(input()).toHaveProperty('value', ''));
  expect(props.messages.append).toHaveBeenCalledTimes(1);
});

it('binds each Office turn to the selected task without changing user text or leaking the task into ordinary chat', async () => {
  const props = fixtures();
  const provider: ProviderDTO = {
    id: '01ARZ3NDEKTSV4RRFFQ69G5FAB',
    name: 'Ready',
    protocol: 'openai_compatible',
    baseUrl: 'https://example.test',
    models: [{ modelId: 'model', displayName: 'Model', isDefault: true }],
    status: 'enabled',
    credentialState: 'configured',
    credentialBackupCount: 0,
    createdAt: now,
    updatedAt: now,
    version: 1,
  };
  let onEvent!: (event: StreamEvent) => void;
  const start = vi.fn(async (_payload, handleEvent) => {
    onEvent = handleEvent;
    return { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel: vi.fn(), dispose: vi.fn() };
  });
  const chat = { start, approve: vi.fn(), dispose: vi.fn() } as ChatBridge;
  const providers = { list: vi.fn(async () => ({ items: [provider] })) } as unknown as ProviderBridge;
  const tasks = ['01ARZ3NDEKTSV4RRFFQ69G5FBA', '01ARZ3NDEKTSV4RRFFQ69G5FBB', undefined];
  for (const [index, officeTaskId] of tasks.entries()) {
    const view = render(<SessionPage {...props} chat={chat} providers={providers} officeTaskId={officeTaskId} />);
    await waitFor(() => expect(screen.getByRole('button', { name: '已配置模型' })).toHaveTextContent('Model'));
    const text = `这是第 ${index + 1} 次原始请求`;
    fireEvent.change(input(), { target: { value: text } });
    fireEvent.click(screen.getByRole('button', { name: '↑ 发送并对话' }));
    await waitFor(() => expect(start).toHaveBeenCalledTimes(index + 1));
    expect(vi.mocked(props.messages.append).mock.calls[index][0].text).toBe(text);
    const payload = start.mock.calls[index][0];
    expect(payload.sessionId).toBe(S);
    if (officeTaskId) expect(payload.officeTaskId).toBe(officeTaskId);
    else expect(payload).not.toHaveProperty('officeTaskId');
    await act(async () =>
      onEvent({
        v: '1.0',
        kind: 'event',
        id: `event-${index}`,
        streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
        sequence: 1,
        type: 'completed',
      }),
    );
    view.unmount();
  }
});
