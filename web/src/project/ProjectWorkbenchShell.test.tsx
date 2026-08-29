import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { ChatBridge, MessageBridge, ProjectBridge, ProviderBridge, SessionBridge, StageBridge } from '../bridge/client'
import type { ProjectDTO, SessionDTO } from '../generated/bridge'
import { phaseSessionTitle } from './projectPhaseSession'
import { ProjectWorkbenchShell } from './ProjectWorkbenchShell'

vi.mock('../session/SessionPage', () => ({
  SessionPage: (props: {
    homeChat?: boolean
    initialSession?: { id: string; title: string }
    projectSidePanel?: unknown
    projectPhase?: number
  }) => (
    <div data-testid="workbench-chat">
      {props.homeChat ? 'home' : 'legacy'}:{props.initialSession?.id ?? 'none'}:{props.initialSession?.title ?? ''}:
      {props.projectPhase ?? 'no-phase'}:{props.projectSidePanel ? 'nested' : 'none'}
    </div>
  ),
}))
vi.mock('./DeliverablePanel', () => ({ DeliverablePanel: () => <div data-testid="deliverable-panel">交付物</div> }))
vi.mock('./RegistryPanel', () => ({ RegistryPanel: () => <div data-testid="registry-panel">注册表</div> }))
vi.mock('./ReleasePanel', () => ({ ReleasePanel: () => <div data-testid="release-panel">发布</div> }))

vi.mock('./phaseExperts', () => ({ applySessionPhaseExperts: vi.fn().mockResolvedValue([]) }))

beforeEach(() => {
  localStorage.removeItem(`lunitide:project-phase:${project.id}`)
})
afterEach(cleanup)
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
const phase1Session: SessionDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAA',
  projectId: project.id,
  title: phaseSessionTitle(1, '需求架构规范'),
  pinned: false,
  status: 'active',
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
}
const phase7Session: SessionDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAB',
  projectId: project.id,
  title: phaseSessionTitle(7, '集成'),
  pinned: false,
  status: 'active',
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
}

it('loads the phase-bound session for the active stage', async () => {
  const sessions = {
    list: vi.fn().mockResolvedValue({ items: [phase1Session, phase7Session] }),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  } as unknown as SessionBridge
  const stages = { list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn(), update: vi.fn() } as unknown as StageBridge
  render(
    <ProjectWorkbenchShell
      project={project}
      projects={{} as ProjectBridge}
      sessions={sessions}
      messages={{} as MessageBridge}
      stages={stages}
      chat={{} as ChatBridge}
      providers={{} as ProviderBridge}
      onBack={vi.fn()}
    />,
  )
  await waitFor(() =>
    expect(screen.getByTestId('workbench-chat')).toHaveTextContent(
      `home:${phase1Session.id}:${phase1Session.title}:1:nested`,
    ),
  )
  expect(sessions.create).not.toHaveBeenCalled()
})

it('switches chat sessions when the user selects another stage', async () => {
  const user = userEvent.setup()
  const sessions = {
    list: vi.fn().mockResolvedValue({ items: [phase1Session, phase7Session] }),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  } as unknown as SessionBridge
  const stages = { list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn(), update: vi.fn() } as unknown as StageBridge
  render(
    <ProjectWorkbenchShell
      project={project}
      projects={{} as ProjectBridge}
      sessions={sessions}
      messages={{} as MessageBridge}
      stages={stages}
      chat={{} as ChatBridge}
      providers={{} as ProviderBridge}
      onBack={vi.fn()}
    />,
  )
  await waitFor(() => expect(screen.getByTestId('workbench-chat')).toHaveTextContent(phase1Session.id))
  await user.click(screen.getByRole('button', { name: /集成/ }))
  await waitFor(() =>
    expect(screen.getByTestId('workbench-chat')).toHaveTextContent(
      `home:${phase7Session.id}:${phase7Session.title}:7:nested`,
    ),
  )
})

it('creates a phase session when none exist for the active stage', async () => {
  const created = { ...phase1Session, id: '01ARZ3NDEKTSV4RRFFQ69G5FAC' }
  const sessions = {
    list: vi.fn().mockResolvedValue({ items: [] }),
    create: vi.fn().mockResolvedValue(created),
    update: vi.fn(),
    delete: vi.fn(),
  } as unknown as SessionBridge
  const stages = { list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn(), update: vi.fn() } as unknown as StageBridge
  render(
    <ProjectWorkbenchShell
      project={project}
      projects={{} as ProjectBridge}
      sessions={sessions}
      messages={{} as MessageBridge}
      stages={stages}
      chat={{} as ChatBridge}
      providers={{} as ProviderBridge}
      onBack={vi.fn()}
    />,
  )
  await waitFor(() => expect(sessions.create).toHaveBeenCalledOnce())
  expect(await screen.findByTestId('workbench-chat')).toHaveTextContent(`home:${created.id}:${created.title}:1:nested`)
})
