import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ExpertBridge } from '../bridge/client'
import { PhaseExpertsBar } from './PhaseExpertsBar'

const AI = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const SESSION = '01ARZ3NDEKTSV4RRFFQ69G5FAX'
const PROJECT = '01ARZ3NDEKTSV4RRFFQ69G5FAY'

afterEach(cleanup)

function experts(overrides: Partial<ExpertBridge> = {}): ExpertBridge {
  return {
    list: vi.fn().mockResolvedValue({
      experts: [{ expertId: AI, name: 'AI 工程师', catalogItemId: 'dev-expert', state: 'enabled' }],
    }),
    sessionMountGet: vi.fn().mockResolvedValue({ expertIds: [AI] }),
    sessionMountSet: vi.fn().mockImplementation(async ({ expertIds }) => ({ expertIds })),
    mountingGet: vi.fn().mockResolvedValue({
      matrix: [{
        phaseKey: 'REQUIREMENT_DEFINITION',
        defaults: [],
        mountings: [{ expertId: AI, state: 'mounted' }],
      }],
    }),
    ...overrides,
  } as unknown as ExpertBridge
}

it('mirrors mounted experts as read-only chips without add or remove controls', async () => {
  render(<PhaseExpertsBar sessionId={SESSION} projectId={PROJECT} phaseLabel="需求架构规范" experts={experts()} />)
  expect(await screen.findByText('本阶段将使用')).toBeInTheDocument()
  expect(screen.getByText('AI 工程师')).toBeInTheDocument()
  expect(screen.queryByLabelText('添加本阶段专家')).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /移除/ })).not.toBeInTheDocument()
  expect(screen.queryByText('×')).not.toBeInTheDocument()
})

it('shows the composer-only empty copy', async () => {
  const bridge = experts({ sessionMountGet: vi.fn().mockResolvedValue({ expertIds: [] }) })
  render(<PhaseExpertsBar sessionId={SESSION} projectId={PROJECT} experts={bridge} />)
  expect(await screen.findByText('未挂载 · 在下方输入框添加')).toBeInTheDocument()
})

it('writes recommended mounts through session.experts.set as ULIDs', async () => {
  const set = vi.fn().mockResolvedValue({ expertIds: [AI] })
  const bridge = experts({
    sessionMountGet: vi.fn().mockResolvedValue({ expertIds: [] }),
    sessionMountSet: set,
  })
  render(<PhaseExpertsBar sessionId={SESSION} projectId={PROJECT} phaseLabel="需求架构规范" experts={bridge} />)
  fireEvent.click(await screen.findByRole('button', { name: '推荐' }))
  await waitFor(() => expect(set).toHaveBeenCalledWith({ sessionId: SESSION, expertIds: [AI] }))
})
