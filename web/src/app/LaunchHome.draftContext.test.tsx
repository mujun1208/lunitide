import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LaunchHome } from './LaunchHome';
import { BridgeClientError, type AttachmentBridge, type ProjectBridge, type ProviderBridge, type SessionBridge } from '../bridge/client';
import { PERSONAL_CHAT_PROJECT_ID_KEY } from './appHelpers';
import { prepareAttachmentFiles } from '../session/attachments';
import { pickComposerFiles } from '../session/composerPlusPick';

vi.mock('../session/attachments', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../session/attachments')>();
  return { ...actual, prepareAttachmentFiles: vi.fn(actual.prepareAttachmentFiles) };
});
vi.mock('../session/composerPlusPick', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../session/composerPlusPick')>();
  return { ...actual, pickComposerFiles: vi.fn(actual.pickComposerFiles) };
});

afterEach(() => { cleanup(); localStorage.clear(); vi.mocked(prepareAttachmentFiles).mockReset(); vi.mocked(pickComposerFiles).mockReset(); });

const project = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', name: '个人对话', type: 'implementation', projectCode: 'ITM00001', status: 'active', createdAt: '', updatedAt: '', version: 1 };
const session = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', projectId: project.id, title: '新对话', status: 'active', createdAt: '', updatedAt: '', version: 1 };
const now = '2026-01-01T00:00:00Z';
const provider = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAC', name: 'Demo', protocol: 'openai_compatible', baseUrl: 'https://example.com', models: [{ modelId: 'chat-l', displayName: 'Chat', isDefault: true, kind: 'llm' }], status: 'enabled', credentialState: 'configured', credentialBackupCount: 0, createdAt: now, updatedAt: now, version: 1 };

function home(overrides: { projects?: ProjectBridge; sessions?: SessionBridge; providers?: ProviderBridge } = {}) {
  const providers = overrides.providers ?? { list: vi.fn(async () => ({ items: [provider] })) } as unknown as ProviderBridge;
  return render(<LaunchHome projects={overrides.projects ?? { list: vi.fn(async () => ({ items: [project] })), create: vi.fn(async () => project) } as unknown as ProjectBridge} providers={providers} sessions={overrides.sessions ?? { create: vi.fn(async () => session), list: vi.fn() } as unknown as SessionBridge} attachments={{} as AttachmentBridge} onCreated={vi.fn()} onDraft={vi.fn()} onSelect={vi.fn()} onOpenProjects={vi.fn()} onManageModels={vi.fn()} onCompanion={vi.fn()} companionNotice="" language="zh-CN"/>);
}

it('does not show raw English attachment or file-pick failures, and ignores OS cancel', async () => {
  vi.mocked(prepareAttachmentFiles).mockRejectedValue(new Error('Failed to fetch'));
  home();
  const input = document.querySelector('input[type="file"]:not([webkitdirectory])') as HTMLInputElement;
  fireEvent.change(input, { target: { files: [new File(['hi'], 'note.txt', { type: 'text/plain' })] } });
  expect(await screen.findByText('文件无法添加')).toBeInTheDocument();
  expect(screen.queryByText('Failed to fetch')).toBeNull();
  cleanup();
  vi.mocked(pickComposerFiles).mockResolvedValue({ kind: 'error', error: new BridgeClientError('Failed to fetch', 'ENGINE_UNAVAILABLE', true, 'engine') });
  home();
  fireEvent.click(screen.getByRole('button', { name: '添加上下文' }));
  fireEvent.click(screen.getByRole('button', { name: /附件 \/ 文件/ }));
  expect(await screen.findByText('文件无法添加')).toBeInTheDocument();
  expect(screen.queryByText('Failed to fetch')).toBeNull();
  cleanup();
  vi.mocked(pickComposerFiles).mockResolvedValue({ kind: 'canceled' });
  home();
  fireEvent.click(screen.getByRole('button', { name: '添加上下文' }));
  fireEvent.click(screen.getByRole('button', { name: /附件 \/ 文件/ }));
  await waitFor(() => expect(pickComposerFiles).toHaveBeenCalled());
  expect(screen.queryByText('Failed to fetch')).toBeNull();
  expect(screen.queryByText('文件无法添加')).toBeNull();
  expect(screen.queryByRole('alert')).toBeNull();
  cleanup();
  vi.mocked(pickComposerFiles).mockResolvedValue({ kind: 'error', error: new BridgeClientError('用户取消了选择', 'CANCELED', false, 'desktop') });
  home();
  fireEvent.click(screen.getByRole('button', { name: '添加上下文' }));
  fireEvent.click(screen.getByRole('button', { name: /附件 \/ 文件/ }));
  await waitFor(() => expect(pickComposerFiles).toHaveBeenCalled());
  expect(screen.queryByText('文件无法添加')).toBeNull();
  expect(screen.queryByText('用户取消了选择')).toBeNull();
  expect(screen.queryByRole('alert')).toBeNull();
});

it('does not show raw English session create or restore failures', async () => {
  localStorage.setItem(PERSONAL_CHAT_PROJECT_ID_KEY, project.id);
  const providers = { list: vi.fn(async () => ({ items: [provider] })) } as unknown as ProviderBridge;
  render(<LaunchHome projects={{ list: vi.fn(async () => ({ items: [project] })), create: vi.fn(async () => project) } as unknown as ProjectBridge} providers={providers} sessions={{ create: vi.fn(async () => { throw new Error('Failed to fetch') }), list: vi.fn() } as unknown as SessionBridge} attachments={{} as AttachmentBridge} onCreated={vi.fn()} onDraft={vi.fn()} onSelect={vi.fn()} onOpenProjects={vi.fn()} onManageModels={vi.fn()} onCompanion={vi.fn()} companionNotice="" language="zh-CN"/>);
  expect(await screen.findByText('Chat')).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText('输入消息'), { target: { value: '开始一轮对话' } });
  fireEvent.click(screen.getByRole('button', { name: '开始对话' }));
  expect(await screen.findByText('创建对话失败，请重试。')).toBeInTheDocument();
  expect(screen.queryByText('Failed to fetch')).toBeNull();
  cleanup();
  render(<LaunchHome projects={{ list: vi.fn(async () => { throw new Error('Failed to fetch') }), create: vi.fn() } as unknown as ProjectBridge} providers={providers} sessions={{ create: vi.fn(), list: vi.fn() } as unknown as SessionBridge} attachments={{} as AttachmentBridge} onCreated={vi.fn()} onDraft={vi.fn()} onSelect={vi.fn()} onOpenProjects={vi.fn()} onManageModels={vi.fn()} onCompanion={vi.fn()} companionNotice="" language="zh-CN"/>);
  fireEvent.click(screen.getByRole('button', { name: /恢复对话/ }));
  expect(await screen.findByText('恢复对话失败')).toBeInTheDocument();
  expect(screen.queryByText('Failed to fetch')).toBeNull();
});

it.each([{ button: '选技能', trigger: '/' }, { button: '选专家', trigger: 'expert' }])('preserves a long unsent home draft when opening $button', async ({ button, trigger }) => {
  const project = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', name: '个人对话', type: 'implementation', projectCode: 'ITM00001', status: 'active', createdAt: '', updatedAt: '', version: 1 };
  const session = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', projectId: project.id, title: '新对话', status: 'active', createdAt: '', updatedAt: '', version: 1 };
  const onSelect = vi.fn();
  localStorage.setItem(PERSONAL_CHAT_PROJECT_ID_KEY, project.id);
  render(<LaunchHome projects={{ list: vi.fn(async () => ({ items: [project] })), create: vi.fn(async () => project) } as unknown as ProjectBridge} providers={{ list: vi.fn(async () => ({ items: [] })) } as unknown as ProviderBridge} sessions={{ create: vi.fn(async () => session) } as unknown as SessionBridge} attachments={{} as AttachmentBridge} onCreated={vi.fn()} onDraft={vi.fn()} onSelect={onSelect} onOpenProjects={vi.fn()} onManageModels={vi.fn()} onCompanion={vi.fn()} companionNotice="" language="zh-CN"/>);
  const draft = '  请按以下完整需求创建专家。'.repeat(240);
  fireEvent.change(screen.getByLabelText('输入消息'), { target: { value: draft } });
  fireEvent.click(screen.getByRole('button', { name: '添加上下文' }));
  fireEvent.click(screen.getByRole('button', { name: new RegExp(button) }));
  await waitFor(() => expect(onSelect).toHaveBeenCalledOnce());
  expect(onSelect.mock.calls[0][0]).toMatchObject({ project, session, prompt: draft, noAutoSend: true, composerTrigger: trigger });
});
