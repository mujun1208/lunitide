import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { ChatBridge, MessageBridge, PlanBridge, ProjectBridge, ProviderBridge, ReviewBridge, SessionBridge, StageBridge } from '../bridge/client'
import type { ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { RootErrorBoundary } from '../RootErrorBoundary'
import { resetLiveChatForTests } from '../session/liveChat'
import { ProjectWorkbenchShell } from './ProjectWorkbenchShell'

vi.mock('./DeliverablePanel', () => ({ DeliverablePanel: () => <div data-testid="deliverable-panel">交付物</div> }))
vi.mock('./RegistryPanel', () => ({ RegistryPanel: () => <div data-testid="registry-panel">注册表</div> }))
vi.mock('./ReleasePanel', () => ({ ReleasePanel: () => <div data-testid="release-panel">发布</div> }))
vi.mock('./phaseExperts', () => ({ applySessionPhaseExperts: vi.fn().mockResolvedValue([]) }))

afterEach(() => {
  cleanup()
  resetLiveChatForTests()
})

const NOW = '2025-01-01T00:00:00Z'
const project: ProjectDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: '在线电商',
  projectCode: 'ITM00003',
  type: 'implementation',
  status: 'active',
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
}
const session: SessionDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAA',
  projectId: project.id,
  title: '在线电商',
  pinned: false,
  status: 'active',
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
}
const provider: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAB',
  name: 'Ready',
  protocol: 'openai_compatible',
  baseUrl: 'https://example.test',
  models: [{ modelId: 'model', displayName: 'Model', isDefault: true }],
  status: 'enabled',
  credentialState: 'configured',
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
}

it('sends from 项目管理 进入工作台 without throwing or replacing the shell', async () => {
  const start = vi.fn().mockResolvedValue({ cancel: vi.fn(), dispose: vi.fn() })
  const append = vi.fn().mockResolvedValue({})
  const sessions = {
    list: vi.fn().mockResolvedValue({ items: [session] }),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  } as unknown as SessionBridge
  const messages = {
    list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }),
    append,
  } as unknown as MessageBridge
  const stages = {
    list: vi.fn().mockResolvedValue({ items: [] }),
    create: vi.fn().mockRejectedValue(new Error('no seed')),
    update: vi.fn(),
  } as unknown as StageBridge
  const plans = { list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as PlanBridge
  const reviews = { list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ReviewBridge
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const user = userEvent.setup()
  render(
    <RootErrorBoundary>
      <ProjectWorkbenchShell
        project={project}
        projects={{} as ProjectBridge}
        sessions={sessions}
        messages={messages}
        stages={stages}
        chat={{ start, approve: vi.fn(), dispose: vi.fn() } as ChatBridge}
        providers={providers}
        plans={plans}
        reviews={reviews}
        onBack={vi.fn()}
      />
    </RootErrorBoundary>,
  )
  expect(await screen.findByLabelText('项目阶段导航')).toBeInTheDocument()
  await screen.findByText('还没有消息')
  await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), '你好')
  await expect(user.click(screen.getByRole('button', { name: '↑ 发送并对话' }))).resolves.toBeUndefined()
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  expect(start.mock.calls[0][0]).toMatchObject({
    sessionId: session.id,
    projectId: project.id,
    projectPhase: 1,
  })
  expect(screen.queryByText('界面遇到了一个错误')).toBeNull()
  expect(screen.getByLabelText('向月汐提问，或描述你想完成的任务…')).toBeInTheDocument()
})
