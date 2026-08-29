import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { ChatBridge, MessageBridge, PlanBridge, ProjectBridge, ProviderBridge, ReviewBridge, SessionBridge, StageBridge, StreamEvent } from '../bridge/client'
import type { MessageDTO, ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { RootErrorBoundary } from '../RootErrorBoundary'
import { resetLiveChatForTests } from '../session/liveChat'
import { phaseSessionTitle } from './projectPhaseSession'
import { ProjectWorkbenchShell } from './ProjectWorkbenchShell'

vi.mock('./DeliverablePanel', () => ({ DeliverablePanel: () => <div data-testid="deliverable-panel">交付物</div> }))
vi.mock('./RegistryPanel', () => ({ RegistryPanel: () => <div data-testid="registry-panel">注册表</div> }))
vi.mock('./ReleasePanel', () => ({ ReleasePanel: () => <div data-testid="release-panel">发布</div> }))
vi.mock('./phaseExperts', () => ({ applySessionPhaseExperts: vi.fn().mockResolvedValue([]) }))

afterEach(() => {
  cleanup()
  resetLiveChatForTests()
  localStorage.removeItem(`lunitide:project-phase:${project.id}`)
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
  title: phaseSessionTitle(1, '需求架构规范'),
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

const emptyStages = {
  list: vi.fn().mockResolvedValue({ items: [] }),
  create: vi.fn().mockRejectedValue(new Error('no seed')),
  update: vi.fn(),
} as unknown as StageBridge
const emptyPlans = { list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as PlanBridge
const emptyReviews = { list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ReviewBridge

it('pauses PM chat follow on wheel-up and does not scrollTo conversation or document while thinking', async () => {
  let onEvent!: (event: StreamEvent) => void
  const start = vi.fn().mockImplementation(async (_payload, onStreamEvent) => {
    onEvent = onStreamEvent
    return { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel: vi.fn(), dispose: vi.fn() }
  })
  const table = '| 主要分歧 | 立场 |\n| --- | --- |\n| 范围 | 待定 |\n| 进度 | 滞后 |'
  const items: MessageDTO[] = [
    { id: '01ARZ3NDEKTSV4RRFFQ69G5FAC', sessionId: session.id, role: 'user', text: '列出分歧', status: 'completed', sequence: 1, createdAt: NOW },
    { id: '01ARZ3NDEKTSV4RRFFQ69G5FAE', sessionId: session.id, role: 'assistant', text: table, status: 'completed', sequence: 2, createdAt: NOW },
  ]
  const sessions = {
    list: vi.fn().mockResolvedValue({ items: [session] }),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  } as unknown as SessionBridge
  const messages = {
    list: vi.fn().mockResolvedValue({ items, hasMore: false, nextCursor: null, snapshotSequence: 2 }),
    append: vi.fn().mockResolvedValue({}),
  } as unknown as MessageBridge
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const windowScrollTo = vi.fn()
  const htmlScrollTo = vi.fn()
  const bodyScrollTo = vi.fn()
  const prevWindow = window.scrollTo
  const htmlDesc = Object.getOwnPropertyDescriptor(document.documentElement, 'scrollTo')
  const bodyDesc = Object.getOwnPropertyDescriptor(document.body, 'scrollTo')
  Object.defineProperty(window, 'scrollTo', { configurable: true, value: windowScrollTo })
  Object.defineProperty(document.documentElement, 'scrollTo', { configurable: true, value: htmlScrollTo })
  Object.defineProperty(document.body, 'scrollTo', { configurable: true, value: bodyScrollTo })
  try {
    const user = userEvent.setup()
    render(
      <RootErrorBoundary>
        <div className="session-shell workbench-route">
          <ProjectWorkbenchShell
            project={project}
            projects={{} as ProjectBridge}
            sessions={sessions}
            messages={messages}
            stages={emptyStages}
            chat={{ start, approve: vi.fn(), dispose: vi.fn() } as ChatBridge}
            providers={providers}
            plans={emptyPlans}
            reviews={emptyReviews}
            onBack={vi.fn()}
          />
        </div>
      </RootErrorBoundary>,
    )
    expect(await screen.findByLabelText('项目阶段导航')).toBeInTheDocument()
    expect(await screen.findByText('主要分歧')).toBeInTheDocument()
    expect(document.querySelector('.project-chat-panel')).toBeTruthy()
    const box = document.querySelector('.conversation-scroll') as HTMLDivElement
    const scrollTo = vi.fn()
    Object.defineProperties(box, {
      scrollHeight: { configurable: true, value: 900 },
      clientHeight: { configurable: true, value: 300 },
      scrollTop: { configurable: true, writable: true, value: 400 },
    })
    Object.defineProperty(box, 'scrollTo', { configurable: true, value: scrollTo })
    await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), '继续')
    await user.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
    await waitFor(() => expect(start).toHaveBeenCalledOnce())
    await act(async () =>
      onEvent({
        v: '1.0',
        kind: 'event',
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAF',
        streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
        sequence: 1,
        type: 'delta',
        delta: { text: '卡片' },
      }),
    )
    await waitFor(() => expect(scrollTo).toHaveBeenCalledWith({ top: 600, behavior: 'auto' }))
    scrollTo.mockClear()
    windowScrollTo.mockClear()
    htmlScrollTo.mockClear()
    bodyScrollTo.mockClear()
    box.scrollTop = 200
    fireEvent.wheel(box, { deltaY: -48 })
    fireEvent.scroll(box)
    expect(screen.getByRole('button', { name: '回到最新消息' })).toBeInTheDocument()
    await act(async () =>
      onEvent({
        v: '1.0',
        kind: 'event',
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAG',
        streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
        sequence: 2,
        type: 'thinking',
        thinking: { text: '逐步推理'.repeat(20) },
      }),
    )
    await act(async () =>
      onEvent({
        v: '1.0',
        kind: 'event',
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAH',
        streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
        sequence: 3,
        type: 'delta',
        delta: { text: '续' },
      }),
    )
    expect(scrollTo).not.toHaveBeenCalled()
    expect(windowScrollTo).not.toHaveBeenCalled()
    expect(htmlScrollTo).not.toHaveBeenCalled()
    expect(bodyScrollTo).not.toHaveBeenCalled()
  } finally {
    Object.defineProperty(window, 'scrollTo', { configurable: true, value: prevWindow })
    if (htmlDesc) Object.defineProperty(document.documentElement, 'scrollTo', htmlDesc)
    else delete (document.documentElement as { scrollTo?: unknown }).scrollTo
    if (bodyDesc) Object.defineProperty(document.body, 'scrollTo', bodyDesc)
    else delete (document.body as { scrollTo?: unknown }).scrollTo
  }
})
