import React from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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

function chatFixture() {
  const props = fixtures();
  const draft = { ...skill, status: 'draft' as const, manifestJson: JSON.stringify({ originSessionId: S }) };
  const provider = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', name: 'Ready', protocol: 'openai_compatible', baseUrl: 'https://example.test', models: [{ modelId: 'model', displayName: 'Model', isDefault: true }], status: 'enabled', credentialState: 'configured', credentialBackupCount: 0, createdAt: now, updatedAt: now, version: 1 } as ProviderDTO;
  let event!: (value: StreamEvent) => void;
  const start = vi.fn(async (_payload, handleEvent) => { event = handleEvent; return { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel: vi.fn(), dispose: vi.fn() }; });
  const created: SkillDTO[] = [];
  const skills = {
    list: vi.fn(async (p) => ({ items: p.sourceSessionId ? [...created] : [skill] })),
    get: vi.fn(async () => draft), publish: vi.fn(),
    packageList: vi.fn(async () => ({ skillId: skill.id, revision: 'rev1', rootPath: 'C:/Lunitide/skills/report-writer', entries: [{ path: 'SKILL.md', kind: 'file' as const, size: 18 }, { path: 'scripts/check.py', kind: 'file' as const, size: 8 }] })),
    packageRead: vi.fn(async (p) => ({ skillId: skill.id, path: p.path, content: '# 实际技能正文', encoding: 'utf8' as const, size: 18, nextOffset: 18, eof: true, digest: 'digest', revision: 'rev1' })),
  } as unknown as SkillBridge;
  return { props: { ...props, skills, chat: { start, approve: vi.fn(), dispose: vi.fn() } as ChatBridge, providers: { list: vi.fn(async () => ({ items: [provider] })) } as unknown as ProviderBridge }, draft, created, start,
    emit: async (type: 'completed' | 'tool_completed', sequence: number) => act(async () => event({ v: '1.0', kind: 'event', id: `e${sequence}`, streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', sequence, type, ...(type === 'tool_completed' ? { tool: { callId: 'created', name: 'skill.manage', argsDigest: 'abc', summary: `已创建技能 id=${skill.id}` } } : {}) } as StreamEvent)) };
}

it('keeps a created skill in its original conversation, opens real package files and explicitly trials a draft', async () => {
  const { props, draft, created, start, emit } = chatFixture(), onCatalogCreated = vi.fn();
  render(<SessionPage {...props} onCatalogCreated={onCatalogCreated} />);
  await screen.findByRole('button', { name: '已配置模型' });
  fireEvent.change(input(), { target: { value: '创建报告技能' } }); fireEvent.click(screen.getByRole('button', { name: '↑ 发送并对话' }));
  await waitFor(() => expect(start).toHaveBeenCalledOnce()); created.push(draft);
  await emit('tool_completed', 1); await emit('completed', 2);
  expect(await screen.findByLabelText('技能包文件')).toHaveTextContent('C:/Lunitide/skills/report-writer');
  expect(await screen.findByText('# 实际技能正文')).toBeInTheDocument();
  const card = await screen.findByRole('region', { name: '本会话创建的技能' });
  expect(card).toHaveTextContent('草稿'); expect(onCatalogCreated).not.toHaveBeenCalled();
  fireEvent.click(within(card).getByRole('button', { name: '在当前对话试用' }));
  await screen.findByText('草稿试用');
  fireEvent.change(input(), { target: { value: '把今天的工作整理成周报' } }); fireEvent.click(screen.getByRole('button', { name: '↑ 发送并对话' }));
  await waitFor(() => expect(start).toHaveBeenCalledTimes(2));
  expect(start.mock.calls[1][0]).toMatchObject({ sessionId: S, trialSkillIds: [skill.id] });
  expect(props.messages.append).toHaveBeenLastCalledWith(expect.objectContaining({ text: expect.stringContaining(`[引用技能 报告助手|${skill.id}]`) }), expect.anything());
  expect(props.skills.publish).not.toHaveBeenCalled();
  await emit('completed', 3);
  fireEvent.click(screen.getByRole('button', { name: '移除技能引用 报告助手' }));
  fireEvent.change(input(), { target: { value: '普通对话' } }); fireEvent.click(screen.getByRole('button', { name: '↑ 发送并对话' }));
  await waitFor(() => expect(start).toHaveBeenCalledTimes(3)); expect(start.mock.calls[2][0]).not.toHaveProperty('trialSkillIds');
});

it('counts selected references in the full long description and never clips a valid creation request', async () => {
  const { props, start } = chatFixture();
  render(<SessionPage {...props} initialReferencedSkills={[skill]} />);
  await screen.findByRole('button', { name: '已配置模型' });
  const raw = '创建专家，完整能力要求。'.repeat(220);
  fireEvent.change(input(), { target: { value: raw } });
  expect(screen.getByText(/含引用.*字节/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: '↑ 发送并对话' }));
  await waitFor(() => expect(start).toHaveBeenCalledOnce());
  expect(vi.mocked(props.messages.append).mock.calls[0][0].text).toBe(`[引用技能 报告助手|${skill.id}]\n${raw}`);
});

it('disables oversized composed requests while preserving every character in the editor', async () => {
  const { props, start } = chatFixture();
  render(<SessionPage {...props} initialReferencedSkills={[skill]} />);
  await screen.findByRole('button', { name: '已配置模型' });
  const raw = '界'.repeat(32768); fireEvent.change(input(), { target: { value: raw } });
  expect(screen.getByRole('button', { name: '↑ 发送并对话' })).toBeDisabled();
  expect(input()).toHaveValue(raw); expect(start).not.toHaveBeenCalled();
});

it('refreshes a cached draft reference after publication and then uses the published skill normally', async () => {
  const { props, draft, start } = chatFixture();
  vi.mocked(props.skills.get).mockResolvedValue(skill);
  render(<SessionPage {...props} initialReferencedSkills={[draft]} />);
  await screen.findByRole('button', { name: '已配置模型' });
  fireEvent.change(input(), { target: { value: '继续使用已发布的报告助手' } });
  fireEvent.click(screen.getByRole('button', { name: '↑ 发送并对话' }));
  await waitFor(() => expect(start).toHaveBeenCalledOnce());
  expect(start.mock.calls[0][0]).not.toHaveProperty('trialSkillIds');
  expect(vi.mocked(props.messages.append).mock.calls[0][0].text).toContain(`[引用技能 报告助手|${skill.id}]`);
  expect(screen.queryByText('草稿试用')).not.toBeInTheDocument();
});

