import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ExpertBridge, ProjectBridge } from '../bridge/client'
import { ExpertCenterPage } from './ExpertCenterPage'

afterEach(cleanup)
const now = '2026-01-01T00:00:00Z'
const expertList = {
  experts: [{
    expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', subjectId: 'subj-1', name: '数据库优化专家',
    division: 'engineering', source: 'local', semver: '1.0.0', state: 'enabled',
    versionCount: 1, mountedPhaseCount: 0, createdAt: now, updatedAt: now,
  }],
  total: 1,
}
const detail = {
  expert: { expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: '数据库优化专家', division: 'engineering', description: '索引与查询调优', semver: '1.0.0' },
  versions: [{ versionId: '01ARZ3NDEKTSV4RRFFQ69G5FAB', semver: '1.0.0', sixSectionDigest: 'a'.repeat(64), changeNote: '', createdAt: now }],
  sixSection: { identity: 'i', mission: 'm', rules: 'r', workflow: 'w', deliverableTemplate: 'd', successMetrics: 's' },
}
const scenarioCard = {
  scenarioCardId: '01ARZ3NDEKTSV4RRFFQ69G5FAX', expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  title: '数据库慢查询处置', summary: '针对慢查询的处置剧本', phaseKey: 'DEVELOPMENT_CHANGE',
  state: 'active', createdAt: now, updatedAt: now,
}
const expertApi = (o: Partial<ExpertBridge> = {}): ExpertBridge => ({
  list: vi.fn().mockResolvedValue(expertList),
  detail: vi.fn().mockResolvedValue(detail),
  create: vi.fn(), update: vi.fn(), toggle: vi.fn(), archive: vi.fn(), mount: vi.fn(),
  mountingGet: vi.fn().mockRejectedValue(new Error('unavailable')),
  scenarioCreate: vi.fn().mockResolvedValue({ scenarioCardId: '01ARZ3NDEKTSV4RRFFQ69G5FAY', expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', title: '新场景', phaseKey: 'ARCHITECTURE_PLAN', digest: 'b'.repeat(64) }),
  scenarioList: vi.fn().mockResolvedValue({ items: [scenarioCard] }),
  scenarioDelete: vi.fn().mockResolvedValue({ scenarioCardId: scenarioCard.scenarioCardId, state: 'archived' }),
  ...o,
})
const projects: ProjectBridge = { list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn(), update: vi.fn(), publish: vi.fn(), close: vi.fn(), reopen: vi.fn(), advanceStatus: vi.fn(), delete: vi.fn() }

it('opens create expert in a dialog instead of the side panel', async () => {
  const bridge = expertApi()
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  await screen.findAllByText('数据库优化专家')
  expect(screen.getByRole('separator', { name: '调整详情栏宽度' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '表单向导' }))
  expect(await screen.findByRole('dialog', { name: '创建专家向导' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '创建专家' })).toBeInTheDocument()
  expect(screen.getAllByText('数据库优化专家').length).toBeGreaterThanOrEqual(2)
})

it('renders scenario cards of the selected expert', async () => {
  const bridge = expertApi()
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  expect(await screen.findByText('数据库慢查询处置')).toBeInTheDocument()
  expect(bridge.scenarioList).toHaveBeenCalledWith({ expertId: expertList.experts[0].expertId, state: 'active' })
})

it('creates a scenario card through the form', async () => {
  const bridge = expertApi({ scenarioList: vi.fn().mockResolvedValue({ items: [] }) })
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  await screen.findByText('暂无活跃场景卡')
  fireEvent.change(screen.getByLabelText('标题'), { target: { value: '新场景' } })
  fireEvent.change(screen.getByLabelText('摘要'), { target: { value: '场景摘要' } })
  fireEvent.click(screen.getByRole('button', { name: '创建场景卡' }))
  await waitFor(() => expect(bridge.scenarioCreate).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.scenarioCreate).mock.calls[0][0]).toMatchObject({
    expertId: expertList.experts[0].expertId, title: '新场景', summary: '场景摘要',
    phaseKey: 'DEVELOPMENT_CHANGE', scenario: { steps: [] },
  })
})

it('rejects invalid scenario JSON before calling the bridge', async () => {
  const bridge = expertApi({ scenarioList: vi.fn().mockResolvedValue({ items: [] }) })
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  await screen.findByText('暂无活跃场景卡')
  fireEvent.change(screen.getByLabelText('标题'), { target: { value: '新场景' } })
  fireEvent.change(screen.getByLabelText('摘要'), { target: { value: '场景摘要' } })
  fireEvent.change(screen.getByLabelText('场景 JSON'), { target: { value: 'not-json' } })
  fireEvent.click(screen.getByRole('button', { name: '创建场景卡' }))
  expect(await screen.findByText('场景 JSON 需为合法对象')).toBeInTheDocument()
  expect(bridge.scenarioCreate).not.toHaveBeenCalled()
})

it('filters by state through the memory tabs and disables AgentPack import as concept', async () => {
  const bridge = expertApi()
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  expect(await screen.findByRole('tab', { name: /全部 · 1/ })).toBeInTheDocument()
  const pack = screen.getByRole('button', { name: '导入 AgentPack' })
  expect(pack).toBeDisabled()
  expect(pack.getAttribute('title')).toMatch(/概念预览/)
  fireEvent.click(screen.getByRole('tab', { name: /已停用 · 0/ }))
  await waitFor(() => expect(screen.getByText('暂无专家')).toBeInTheDocument())
})

it('renders the nine-phase mounting matrix table with defaults and limits', async () => {
  const bridge = expertApi({
    mountingGet: vi.fn().mockResolvedValue({
      matrix: [
        { phaseKey: 'INITIATION_BOUNDARY', defaults: [{ expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', division: 'engineering' }], mountings: [{ mountingId: '01ARZ3NDEKTSV4RRFFQ69G5FAC', expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', versionId: '01ARZ3NDEKTSV4RRFFQ69G5FAB', semver: '1.0.0', state: 'mounted', expertState: 'enabled' }] },
        { phaseKey: 'OPERATIONS_RETROSPECTIVE', defaults: [], mountings: [] },
      ],
    }),
  })
  const withProject: ProjectBridge = { ...projects, list: vi.fn().mockResolvedValue({ items: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAZ', name: '在线商城系统', createdAt: now, updatedAt: now, status: 'active' }] }) }
  render(<ExpertCenterPage bridge={bridge} projects={withProject} />)
  expect(await screen.findByText('默认推荐（M7 映射）')).toBeInTheDocument()
  expect(screen.getAllByText('数据库优化专家').length).toBeGreaterThanOrEqual(2)
  expect(screen.getByText('1 / 4')).toBeInTheDocument()
  expect(screen.getByText('同默认（0 名）')).toBeInTheDocument()
  expect(screen.getByText('只读派发 · 无权限载荷')).toBeInTheDocument()
})

it('archives a scenario card from the list', async () => {
  const bridge = expertApi()
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  await screen.findByText('数据库慢查询处置')
  fireEvent.click(screen.getByRole('button', { name: '归档此场景卡' }))
  await waitFor(() => expect(bridge.scenarioDelete).toHaveBeenCalledWith(
    { scenarioCardId: scenarioCard.scenarioCardId }, expect.objectContaining({ attempt: expect.any(Object) })))
})

it('switches between active and archived scenario tabs', async () => {
  const scenarioList = vi.fn().mockImplementation(async (_p: { state?: string }) =>
    ({ items: _p?.state === 'archived' ? [{ ...scenarioCard, state: 'archived' as const }] : [scenarioCard] }))
  const bridge = expertApi({ scenarioList })
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  await screen.findByText('数据库慢查询处置')
  fireEvent.click(screen.getByRole('tab', { name: '已归档' }))
  await waitFor(() => expect(scenarioList).toHaveBeenLastCalledWith({ expertId: expertList.experts[0].expertId, state: 'archived' }))
  expect(bridge.scenarioList).toHaveBeenCalled()
})
