import React from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { OfficeStudioPage } from './OfficeStudioPage';
import { openOfficeHome, OFFICE_ARTIFACT_FOCUS_KEY, OFFICE_LAST_TASK_KEY } from './officeNavigation';
import { OFFICE_READ_TIMEOUT_MS } from './officeRead';
import type { OfficeStudioApi, OfficeTaskDetail } from './officeStudioApi';

const detail: OfficeTaskDetail = {
  task: { id: 'task-one', sessionId: 'session-one', projectId: 'project-one', title: '季度汇报', goal: '生成周报', revision: 1, status: 'draft', createdAt: '2026-09-08T00:00:00Z', updatedAt: '2026-09-08T00:00:00Z' },
  artifacts: [], sources: [], steps: [],
};
const fixture = () => ({ list: vi.fn(async () => ({ items: [detail.task] })), get: vi.fn(async () => detail), sync: vi.fn(async () => detail), listMetrics: vi.fn(async () => ({ items: [] })) } as unknown as OfficeStudioApi);
const conversation = () => <textarea aria-label="任务对话" defaultValue="未提交草稿" />;
afterEach(() => { cleanup(); localStorage.clear(); vi.useRealTimers(); });

it('opens the task list despite an obsolete last-task key and returns without deleting tasks', async () => {
  const api = fixture();
  localStorage.setItem(OFFICE_LAST_TASK_KEY, 'deleted-task');
  render(<OfficeStudioPage api={api} renderConversation={conversation} onOpenSession={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: /季度汇报/ }));
  await screen.findByLabelText('任务对话');
  expect(api.get).toHaveBeenCalledExactlyOnceWith({ taskId: detail.task.id });
  fireEvent.click(screen.getByRole('button', { name: '返回任务列表' }));
  expect(await screen.findByLabelText('想完成什么工作')).toBeVisible();
  fireEvent.click(await screen.findByRole('button', { name: /季度汇报/ }));
  await screen.findByLabelText('任务对话');
  act(() => openOfficeHome());
  expect(await screen.findByRole('heading', { name: '最近任务' })).toBeVisible();
});

it('still supports explicit entry from a chat artifact', async () => {
  const api = fixture();
  localStorage.setItem(OFFICE_ARTIFACT_FOCUS_KEY, JSON.stringify({ taskId: detail.task.id, path: 'report.docx' }));
  render(<OfficeStudioPage api={api} renderConversation={conversation} onOpenSession={vi.fn()} />);
  await screen.findByLabelText('任务对话');
  expect(api.get).toHaveBeenCalledWith({ taskId: detail.task.id });
});

it('does not let a late list response erase the task error and can retry', async () => {
  const api = fixture();
  let finishList!: (value: { items: typeof detail.task[] }) => void;
  vi.mocked(api.list).mockImplementation(() => new Promise(resolve => { finishList = resolve; }));
  vi.mocked(api.get).mockRejectedValueOnce(new Error('原任务不存在或不在当前组织范围'));
  render(<OfficeStudioPage initialTaskId={detail.task.id} api={api} renderConversation={conversation} onOpenSession={vi.fn()} />);
  await screen.findByText('原任务不存在或不在当前组织范围');
  await act(async () => finishList({ items: [detail.task] }));
  expect(screen.getByText('原任务不存在或不在当前组织范围')).toBeVisible();
  expect(screen.queryByText('正在读取任务对话…')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: '重新读取对话' }));
  await screen.findByLabelText('任务对话');
});

it('bounds stalled reads and ignores their late completion after returning home', async () => {
  vi.useFakeTimers();
  const api = fixture();
  let finish!: (value: OfficeTaskDetail) => void;
  vi.mocked(api.get).mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  render(<OfficeStudioPage initialTaskId={detail.task.id} api={api} renderConversation={conversation} onOpenSession={vi.fn()} />);
  await act(async () => { await vi.advanceTimersByTimeAsync(OFFICE_READ_TIMEOUT_MS + 1); });
  expect(screen.getByText('读取办公记录超时，请重试或返回任务列表。')).toBeVisible();
  fireEvent.click(screen.getAllByRole('button', { name: '返回任务列表' })[0]);
  await act(async () => finish(detail));
  expect(screen.getByLabelText('想完成什么工作')).toBeVisible();
  expect(screen.queryByLabelText('任务对话')).not.toBeInTheDocument();
  expect(api.sync).not.toHaveBeenCalled();
});

it('keeps the loaded conversation visible when artifact synchronization fails', async () => {
  const api = fixture();
  vi.mocked(api.sync).mockRejectedValue(new Error('文件同步失败'));
  render(<OfficeStudioPage initialTaskId={detail.task.id} api={api} renderConversation={conversation} onOpenSession={vi.fn()} />);
  await screen.findByLabelText('任务对话');
  await waitFor(() => expect(screen.getByText('文件同步失败')).toBeVisible());
  expect(screen.queryByText('正在读取任务对话…')).not.toBeInTheDocument();
});
