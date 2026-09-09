import React, { useEffect } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type {
  ProjectBridge,
  SessionBridge,
  AttachmentBridge,
  MessageBridge,
  ProviderBridge,
  ExpertBridge,
  ChatBridge,
} from '../bridge/client';
import type { ProjectDTO, SessionDTO } from '../generated/bridge';
import { OfficeStudioRoute, resolveOfficeBinding } from './OfficeStudioRoute';
import type { OfficeStudioApi, OfficeTaskDetail } from './officeStudioApi';

const observed = vi.hoisted(() => ({ mounts: vi.fn(), prompts: vi.fn(), taskScopes: vi.fn() }));
vi.mock('../session/SessionPage', () => ({
  SessionPage: (props: {
    initialPrompt?: string;
    initialNoAutoSend?: boolean;
    initialSession?: SessionDTO;
    officeTaskId?: string;
  }) => {
    useEffect(() => {
      observed.mounts(props.initialSession?.id);
      observed.taskScopes(props.officeTaskId);
      if (props.initialPrompt && !props.initialNoAutoSend) observed.prompts(props.initialPrompt);
    }, []);
    return <textarea aria-label="共用会话输入" defaultValue={props.initialPrompt || ''} />;
  },
}));
const now = '2026-09-07T00:00:00Z';
const project: ProjectDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FA0',
  name: '\u2063月汐·普通对话',
  projectCode: 'ITM00001',
  type: 'implementation',
  status: 'active',
  createdAt: now,
  updatedAt: now,
  version: 1,
};
const session: SessionDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FA1',
  projectId: project.id,
  title: '报告制作',
  pinned: false,
  status: 'active',
  createdAt: now,
  updatedAt: now,
  version: 1,
};
const detail: OfficeTaskDetail = {
  task: {
    id: '01ARZ3NDEKTSV4RRFFQ69G5FA2',
    sessionId: session.id,
    projectId: project.id,
    title: '报告制作',
    goal: '生成经营报告',
    revision: 1,
    status: 'draft',
    createdAt: now,
    updatedAt: now,
  },
  artifacts: [],
  sources: [],
  steps: [],
};
function fixtures() {
  const projects = { list: vi.fn(async () => ({ items: [project] })), create: vi.fn() } as unknown as ProjectBridge;
  const sessions = {
    list: vi.fn(async () => ({ items: [session] })),
    create: vi.fn(),
    delete: vi.fn(),
  } as unknown as SessionBridge;
  const api = {
    list: vi.fn(async () => ({ items: [detail.task] })),
    get: vi.fn(async () => detail),
    create: vi.fn(async () => detail),
    sync: vi.fn(async () => detail),
    preview: vi.fn(),
    listMetrics: vi.fn(async () => ({ items: [] })),
  } as unknown as OfficeStudioApi;
  return {
    projects,
    sessions,
    api,
    messages: {} as MessageBridge,
    attachments: {} as AttachmentBridge,
    providers: {} as ProviderBridge,
    experts: {} as ExpertBridge,
    chat: {} as ChatBridge,
    providersRevision: 0,
    onOpenChat: vi.fn(),
    onManageModels: vi.fn(),
    onActivityChange: vi.fn(),
  };
}
afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.clearAllMocks();
});

it('opens exactly the bound project/session and never creates a replacement conversation', async () => {
  const props = fixtures();
  expect(await resolveOfficeBinding(detail.task, props.projects, props.sessions)).toEqual({
    project,
    session,
    personal: true,
  });
  expect(props.sessions.list).toHaveBeenCalledWith({ projectId: project.id });
  expect(props.projects.create).not.toHaveBeenCalled();
  expect(props.sessions.create).not.toHaveBeenCalled();
});

it('keeps files accessible when a bound session is gone rather than silently switching projects', async () => {
  const props = fixtures();
  vi.mocked(props.sessions.list).mockResolvedValue({ items: [] });
  await expect(resolveOfficeBinding(detail.task, props.projects, props.sessions)).rejects.toThrow('原对话当前不可用');
  expect(props.sessions.create).not.toHaveBeenCalled();
});

it('returns a project conversation to its project workbench instead of personal chat', async () => {
  const props = fixtures();
  const scopedProject = { ...project, name: '工程实施项目' };
  vi.mocked(props.projects.list).mockResolvedValue({ items: [scopedProject] });
  localStorage.setItem('lunitide:office-studio:last-task', detail.task.id);
  render(<OfficeStudioRoute {...props} initialTaskId={detail.task.id} />);
  await screen.findByLabelText('共用会话输入');
  fireEvent.click(screen.getByRole('button', { name: '返回原对话' }));
  await waitFor(() =>
    expect(props.onOpenChat).toHaveBeenCalledWith({ project: scopedProject, session, personal: false }),
  );
});

it('syncs files on entry and passes the existing conversation on return without autosending', async () => {
  const props = fixtures();
  localStorage.setItem('lunitide:office-studio:last-task', detail.task.id);
  render(<OfficeStudioRoute {...props} initialTaskId={detail.task.id} />);
  await screen.findByLabelText('共用会话输入');
  expect(props.api.sync).toHaveBeenCalledWith({ taskId: detail.task.id });
  expect(observed.prompts).not.toHaveBeenCalled();
  expect(observed.taskScopes).toHaveBeenCalledWith(detail.task.id);
  fireEvent.click(screen.getByRole('button', { name: '返回原对话' }));
  await waitFor(() =>
    expect(props.onOpenChat).toHaveBeenCalledWith({ project, session, personal: true, noAutoSend: true }),
  );
});

it('submits a new task goal once through the shared SessionPage and never repeats it when reopened', async () => {
  const props = fixtures();
  render(<OfficeStudioRoute {...props} />);
  await screen.findByRole('heading', { name: '从材料到可交付文件' });
  fireEvent.change(screen.getByLabelText('想完成什么工作'), { target: { value: '整理季度业绩并生成 Word 文件' } });
  fireEvent.click(screen.getByRole('button', { name: '开始工作 →' }));
  await screen.findByLabelText('共用会话输入');
  await waitFor(() => expect(observed.prompts).toHaveBeenCalledExactlyOnceWith('整理季度业绩并生成 Word 文件'));
  fireEvent.click(screen.getByRole('button', { name: '切换到检查' }));
  fireEvent.click(screen.getByRole('button', { name: '切换到对话' }));
  expect(observed.mounts).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole('button', { name: '办公工作台' }));
  await waitFor(() => {
    expect(screen.queryByLabelText('共用会话输入')).not.toBeInTheDocument();
  });
  await screen.findByRole('heading', { name: '从材料到可交付文件' });
  fireEvent.click(await screen.findByRole('button', { name: /报告制作/ }));
  await screen.findByLabelText('共用会话输入');
  await waitFor(() => expect(observed.mounts).toHaveBeenCalledTimes(2));
  expect(observed.prompts).toHaveBeenCalledOnce();
});
