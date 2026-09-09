import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LaunchHome } from './LaunchHome';
import type { AttachmentBridge, ProjectBridge, ProviderBridge, SessionBridge } from '../bridge/client';
import { PERSONAL_CHAT_PROJECT_ID_KEY } from './appHelpers';

afterEach(() => { cleanup(); localStorage.clear(); });
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
