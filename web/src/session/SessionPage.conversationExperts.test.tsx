import {cleanup, render, screen, waitFor} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {afterEach, expect, it, vi} from 'vitest'
import type {ExpertBridge, MessageBridge, ProviderBridge, SessionBridge, SkillBridge} from '../bridge/client'
import type {ProjectDTO, ProviderDTO, SessionDTO} from '../generated/bridge'
import {CONVERSATION_EXPERTS, conversationExpertDivision} from '../expert/conversationExperts'
import {SessionPage} from './SessionPage'
import {resetLiveChatForTests} from './liveChat'

afterEach(() => {
  cleanup()
  resetLiveChatForTests()
  localStorage.clear()
})

const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const S = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const NOW = '2025-01-01T00:00:00Z'
const project: ProjectDTO = {id: P, name: 'Runtime', projectCode: 'ITM00001', type: 'implementation', status: 'active', createdAt: NOW, updatedAt: NOW, version: 1}
const session: SessionDTO = {id: S, projectId: P, title: 'Session', pinned: false, status: 'active', createdAt: NOW, updatedAt: NOW, version: 1}
const sessionBridge: SessionBridge = {list: vi.fn().mockResolvedValue({items: [session]}), create: vi.fn(), update: vi.fn(), delete: vi.fn()}
const provider: ProviderDTO = {id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', name: 'Ready', protocol: 'openai_compatible', baseUrl: 'https://example.test', models: [{modelId: 'model', displayName: 'Model', isDefault: true}], status: 'enabled', credentialState: 'configured', credentialBackupCount: 0, createdAt: NOW, updatedAt: NOW, version: 1}
const providers = {list: vi.fn().mockResolvedValue({items: [provider]})} as unknown as ProviderBridge

it('lets the 对话 picker select 系统架构师专家 with the other conversation specialists', async () => {
  const roster = CONVERSATION_EXPERTS.map((item, index) => ({
    expertId: `01ARZ3NDEKTSV4RRFFQ69G5F${String(index).padStart(2, '0')}`,
    name: item.name,
    division: conversationExpertDivision(item.id),
    source: 'local' as const, semver: '1.0.0', state: 'enabled' as const,
    versionCount: 1, mountedPhaseCount: 0,
  }))
  const sessionMountSet = vi.fn().mockImplementation(async (payload: {expertIds: string[]}) => ({expertIds: payload.expertIds}))
  const experts = {
    list: vi.fn().mockResolvedValue({experts: roster}),
    sessionMountGet: vi.fn().mockResolvedValue({expertIds: []}),
    sessionMountSet, detail: vi.fn(), create: vi.fn(), update: vi.fn(), toggle: vi.fn(), archive: vi.fn(),
    mount: vi.fn(), mountingGet: vi.fn(), scenarioCreate: vi.fn(), scenarioList: vi.fn(), scenarioDelete: vi.fn(),
  } as unknown as ExpertBridge
  const user = userEvent.setup()
  const skills = {list: vi.fn().mockResolvedValue({items: []})} as unknown as SkillBridge
  render(<SessionPage project={project} bridge={sessionBridge} messages={{list: vi.fn().mockResolvedValue({items: [], hasMore: false, nextCursor: null, snapshotSequence: 0}), append: vi.fn()} as MessageBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} experts={experts} skills={skills} />)
  await user.click(await screen.findByText('Session'))
  await screen.findByText('还没有消息')
  await user.click(screen.getByRole('button', {name: '添加上下文'}))
  await user.click(screen.getByRole('button', {name: /选专家/}))
  expect(await screen.findByRole('listbox', {name: '专家候选'})).toBeInTheDocument()
  for (const item of roster) {
    expect(screen.getByRole('option', {name: (n) => n.startsWith(`${item.name} `)})).toBeInTheDocument()
  }
  // Composer caps mounts at 8; pick a job-split set that includes the three new cards.
  const mountNames = [
    '系统架构师专家', '数据库设计专家', '系统项目结构规范专家', '开发规范专家',
    'UI专家', '系统测试专家', '硬件配置专家', '开发专家',
  ]
  for (const name of mountNames) {
    await user.click(screen.getByRole('option', {name: (n) => n.startsWith(`${name} `)}))
  }
  const mounted = screen.getByLabelText('已挂载专家')
  for (const name of mountNames) {
    expect(mounted).toHaveTextContent(name)
  }
})

it('does not pin published skills onto the composer when 选专家 PPT专家', async () => {
  const sessionSolo = {...session, id: '01ARZ3NDEKTSV4RRFFQ69G5F99'}
  const ppt = {
    expertId: '01ARZ3NDEKTSV4RRFFQ69G5F00',
    name: 'PPT专家',
    division: 'product' as const,
    source: 'local' as const, semver: '1.0.0', state: 'enabled' as const,
    versionCount: 1, mountedPhaseCount: 0,
  }
  const skill = (id: string, name: string, displayName: string, entryPoint: string) => ({
    id, name, displayName, description: displayName,
    version: '1.0.0', status: 'published' as const, permissions: ['read_write' as const],
    entryPoint, manifestJson: '{}', category: 'writing' as const, categorySource: 'keyword' as const,
    createdAt: NOW, updatedAt: NOW,
  })
  const published = [
    skill('01ARZ3NDEKTSV4RRFFQ69G5F10', 'tpl-slide-builder', '演示文稿助手', 'builtin://slide-builder'),
    skill('01ARZ3NDEKTSV4RRFFQ69G5F11', 'tpl-web-researcher', '联网调研', 'builtin://web-researcher'),
    skill('01ARZ3NDEKTSV4RRFFQ69G5F12', 'tpl-mermaid-diagrams', 'Mermaid 结构图', 'builtin://mermaid-diagrams'),
  ]
  const skills = {list: vi.fn().mockResolvedValue({items: published})} as unknown as SkillBridge
  const experts = {
    list: vi.fn().mockResolvedValue({experts: [ppt]}),
    sessionMountGet: vi.fn().mockResolvedValue({expertIds: []}),
    sessionMountSet: vi.fn().mockResolvedValue({expertIds: [ppt.expertId]}),
    detail: vi.fn(), create: vi.fn(), update: vi.fn(), toggle: vi.fn(), archive: vi.fn(),
    mount: vi.fn(), mountingGet: vi.fn(), scenarioCreate: vi.fn(), scenarioList: vi.fn(), scenarioDelete: vi.fn(),
  } as unknown as ExpertBridge
  const user = userEvent.setup()
  render(<SessionPage project={project} bridge={sessionBridge} messages={{list: vi.fn().mockResolvedValue({items: [], hasMore: false, nextCursor: null, snapshotSequence: 0}), append: vi.fn()} as MessageBridge} onBack={vi.fn()} personal initialSession={sessionSolo} providers={providers} experts={experts} skills={skills} />)
  await user.click(await screen.findByText('Session'))
  await screen.findByText('还没有消息')
  await user.click(screen.getByRole('button', {name: '添加上下文'}))
  await user.click(screen.getByRole('button', {name: /选专家/}))
  await user.click(await screen.findByRole('option', {name: (n) => n.startsWith('PPT专家 ')}))
  expect(screen.getByLabelText('已挂载专家')).toHaveTextContent('PPT专家')
  await waitFor(() => expect(screen.queryByRole('list', {name: '已引用技能'})).toBeNull())
  expect(screen.queryByText('演示文稿助手')).toBeNull()
})
