import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ExpertBridge, ProjectBridge } from '../bridge/client'
import { ExpertCenterPage } from './ExpertCenterPage'

afterEach(cleanup)
const now = '2026-01-01T00:00:00Z'
const expertId = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const expertList = {
  experts: [{
    expertId, subjectId: 'subj-1', name: '安全工程师',
    division: 'security', source: 'pack', semver: '1.0.0', state: 'enabled',
    versionCount: 1, mountedPhaseCount: 0, createdAt: now, updatedAt: now,
  }],
  total: 1,
}
const expertApi = (o: Partial<ExpertBridge> = {}): ExpertBridge => ({
  list: vi.fn().mockResolvedValue(expertList),
  detail: vi.fn(),
  create: vi.fn(), update: vi.fn(), toggle: vi.fn(), archive: vi.fn(), mount: vi.fn(),
  mountingGet: vi.fn().mockResolvedValue({
    matrix: [
      { phaseKey: 'INITIATION_BOUNDARY', defaults: [], mountings: [] },
      { phaseKey: 'RESEARCH_EVIDENCE', defaults: [], mountings: [] },
    ],
  }),
  scenarioCreate: vi.fn(), scenarioList: vi.fn().mockResolvedValue({ items: [] }), scenarioDelete: vi.fn(),
  ...o,
})
const projects: ProjectBridge = {
  list: vi.fn().mockResolvedValue({ items: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAZ', name: '在线电商', projectCode: 'ITM00001', type: 'implementation', createdAt: now, updatedAt: now, status: 'active', version: 1 }] }),
  create: vi.fn(), update: vi.fn(), publish: vi.fn(), close: vi.fn(), reopen: vi.fn(), advanceStatus: vi.fn(), delete: vi.fn(),
}

it('lists installed experts by name instead of an empty row', async () => {
  render(<ExpertCenterPage bridge={expertApi()} projects={projects} />)
  expect(await screen.findByText('安全工程师')).toBeInTheDocument()
  expect(screen.getByText(/安全 · v1\.0\.0/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '挂载' })).toBeInTheDocument()
  expect(screen.queryByText('九阶段挂载矩阵')).toBeNull()
  expect(screen.queryByRole('separator', { name: '调整详情栏宽度' })).toBeNull()
})

it('opens a mount dialog with the eight project steps', async () => {
  const bridge = expertApi()
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  await screen.findByText('安全工程师')
  fireEvent.click(screen.getByRole('button', { name: '挂载' }))
  expect(await screen.findByRole('dialog', { name: '挂载「安全工程师」' })).toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: /1 · 需求架构规范/ })).toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: /8 · 发布/ })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('checkbox', { name: /5 · 开发/ }))
  fireEvent.click(screen.getByRole('button', { name: '确认挂载' }))
  await waitFor(() => expect(bridge.mount).toHaveBeenCalled())
  expect(vi.mocked(bridge.mount).mock.calls[0][0]).toMatchObject({
    projectId: '01ARZ3NDEKTSV4RRFFQ69G5FAZ',
    phaseKey: 'ARCHITECTURE_PLAN',
    expertId,
    action: 'mount',
  })
})

it('opens create expert in a dialog from 手动填写', async () => {
  render(<ExpertCenterPage bridge={expertApi()} projects={projects} />)
  await screen.findByText('安全工程师')
  fireEvent.click(screen.getByRole('button', { name: '手动填写' }))
  expect(await screen.findByRole('dialog', { name: '创建专家向导' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '创建专家' })).toBeInTheDocument()
})
