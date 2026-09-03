import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ExpertBridge, ProjectBridge, SkillBridge } from '../bridge/client'
import { ExpertCenterPage } from './ExpertCenterPage'

afterEach(cleanup)
const now = '2026-01-01T00:00:00Z'
const expertId = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const versionId = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
const digest = 'a'.repeat(64)
const expertList = {
  experts: [{
    expertId, subjectId: 'subj-1', name: '安全工程师',
    division: 'security', source: 'pack', semver: '1.0.0', state: 'enabled',
    versionCount: 1, mountedPhaseCount: 0, createdAt: now, updatedAt: now,
  }],
  total: 1,
}
const expertDetail = {
  expert: { expertId, name: '安全工程师', division: 'security', source: 'pack', state: 'enabled', semver: '1.0.0', currentVersionId: versionId },
  sixSection: { identity: '安全岗位', mission: '守住边界', rules: '最小权限', workflow: '评估-处置', deliverableTemplate: '报告', successMetrics: '零事故' },
  versions: [{ versionId, semver: '1.0.0', changeNote: '初版', sixSectionDigest: digest, createdAt: now }],
  mountings: [],
}
const expertApi = (o: Partial<ExpertBridge> = {}): ExpertBridge => ({
  list: vi.fn().mockResolvedValue(expertList),
  detail: vi.fn().mockResolvedValue(expertDetail),
  create: vi.fn(), update: vi.fn().mockResolvedValue({ expertId, versionId, semver: '1.0.1' }),
  toggle: vi.fn(), archive: vi.fn().mockResolvedValue({ expertId, state: 'archived', archivedVersions: 1 }),
  mount: vi.fn(),
  mountingGet: vi.fn().mockResolvedValue({
    matrix: [
      { phaseKey: 'INITIATION_BOUNDARY', defaults: [], mountings: [] },
      { phaseKey: 'RESEARCH_EVIDENCE', defaults: [], mountings: [] },
    ],
  }),
  scenarioCreate: vi.fn().mockResolvedValue({ scenarioCardId: '01ARZ3NDEKTSV4RRFFQ69G5FAX', expertId, title: '慢查询处置', phaseKey: 'DEVELOPMENT_CHANGE', digest }),
  scenarioList: vi.fn().mockResolvedValue({ items: [] }), scenarioDelete: vi.fn(),
  catalogList: vi.fn().mockResolvedValue({ items: [] }),
  ...o,
})
const projects: ProjectBridge = {
  list: vi.fn().mockResolvedValue({ items: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAZ', name: '在线电商', projectCode: 'ITM00001', type: 'implementation', createdAt: now, updatedAt: now, status: 'active', version: 1 }] }),
  create: vi.fn(), update: vi.fn(), publish: vi.fn(), close: vi.fn(), reopen: vi.fn(), advanceStatus: vi.fn(), delete: vi.fn(),
}

it('shows runtime tools and opens a conversation specialist as a colleague', async () => {
  const onOpenExpert = vi.fn()
  const ppt = {
    ...expertList.experts[0],
    name: 'PPT专家',
    division: 'product' as const,
  }
  const bridge = expertApi({
    list: vi.fn().mockResolvedValue({ experts: [ppt], total: 1 }),
    detail: vi.fn().mockResolvedValue({
      ...expertDetail,
      expert: { ...expertDetail.expert, name: 'PPT专家', division: 'product' },
    }),
  })
  const mcp = {
    presets: vi.fn().mockResolvedValue({ items: [{ id: 'playwright', name: 'Playwright', description: '浏览器自动化', transport: 'stdio', command: 'npx', args: [], needsArgs: false, category: '浏览器' }] }),
    list: vi.fn().mockResolvedValue({ endpoints: [] }),
    add: vi.fn(), toggle: vi.fn(), health: vi.fn(), marketSearch: vi.fn(),
  }
  render(<ExpertCenterPage bridge={bridge} projects={projects} mcp={mcp} onOpenExpert={onOpenExpert} />)
  expect((await screen.findAllByText('PPT专家')).length).toBeGreaterThanOrEqual(1)
  expect(await screen.findByLabelText('必备工具')).toHaveTextContent('pptx.gen')
  expect(await screen.findByLabelText('已授权 MCP')).toHaveTextContent('Playwright')
  fireEvent.click(screen.getByRole('button', { name: '打开同事' }))
  expect(onOpenExpert).toHaveBeenCalledWith(expect.objectContaining({ name: 'PPT专家' }))
})

it('offers open-workbench for an enabled aviation MRO colleague', async () => {
  const onOpenWorkbench = vi.fn()
  const mro = {
    ...expertList.experts[0],
    name: '航空机务维修专家',
    division: 'operations' as const,
    catalogItemId: 'mro-expert',
  }
  const bridge = expertApi({
    list: vi.fn().mockResolvedValue({ experts: [mro], total: 1 }),
    detail: vi.fn().mockResolvedValue({
      ...expertDetail,
      expert: { ...expertDetail.expert, name: '航空机务维修专家', division: 'operations', catalogItemId: 'mro-expert' },
    }),
  })
  render(<ExpertCenterPage bridge={bridge} projects={projects} onOpenWorkbench={onOpenWorkbench} />)
  expect((await screen.findAllByText('航空机务维修专家')).length).toBeGreaterThanOrEqual(1)
  fireEvent.click(screen.getByRole('button', { name: '打开工作台' }))
  expect(onOpenWorkbench).toHaveBeenCalledWith(expect.objectContaining({ name: '航空机务维修专家', catalogItemId: 'mro-expert' }))
})

it('offers use-in-current-session without opening a colleague thread', async () => {
  const onUseInSession = vi.fn()
  render(<ExpertCenterPage bridge={expertApi()} projects={projects} onOpenExpert={vi.fn()} onUseInSession={onUseInSession} />)
  expect((await screen.findAllByText('安全工程师')).length).toBeGreaterThanOrEqual(1)
  fireEvent.click(screen.getByRole('button', { name: '在当前会话使用' }))
  expect(onUseInSession).toHaveBeenCalledWith(expect.objectContaining({ name: '安全工程师' }))
})

it('shows a persona card and opens an enabled expert', async () => {
  const onOpenExpert = vi.fn()
  render(<ExpertCenterPage bridge={expertApi()} projects={projects} onOpenExpert={onOpenExpert} />)
  expect((await screen.findAllByText('安全工程师')).length).toBeGreaterThanOrEqual(1)
  expect(screen.getByRole('region', { name: '专家名片' })).toBeInTheDocument()
  expect(screen.getByText(/技能包 · 工程师 · 安全/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '打开专家' }))
  expect(onOpenExpert).toHaveBeenCalledWith(expect.objectContaining({ expertId, name: '安全工程师' }))
})

it('lists installed experts in a skill-center table with a resizable detail pane', async () => {
  render(<ExpertCenterPage bridge={expertApi()} projects={projects} />)
  expect((await screen.findAllByText('安全工程师')).length).toBeGreaterThanOrEqual(1)
  expect(screen.getAllByText('v1.0.0').length).toBeGreaterThanOrEqual(1)
  expect(screen.getByRole('button', { name: '挂载' })).toBeInTheDocument()
  expect(screen.getByRole('separator', { name: '调整详情栏宽度' })).toBeInTheDocument()
  expect(screen.queryByText('九阶段挂载矩阵')).toBeNull()
  expect(await screen.findByText('安全岗位')).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: /技能包/ })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: /同事专家/ })).toBeInTheDocument()
  expect(await screen.findByText('尚未挂技能，仅提示词。')).toBeInTheDocument()
  await waitFor(() => expect(screen.queryByRole('tab', { name: '专家市场' })).toBeNull())
})

it('opens a mount dialog with the eight project steps', async () => {
  const bridge = expertApi()
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  await screen.findAllByText('安全工程师')
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
  await screen.findAllByText('安全工程师')
  fireEvent.click(screen.getByRole('button', { name: '添加专家' }))
  fireEvent.click(screen.getByRole('button', { name: /手动填写/ }))
  expect(await screen.findByRole('dialog', { name: '创建专家向导' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '创建专家' })).toBeInTheDocument()
})

it('edits the six-section body from the detail pane', async () => {
  const bridge = expertApi()
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  expect(await screen.findByText('安全岗位')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '编辑专家' }))
  expect(await screen.findByRole('dialog', { name: '编辑 安全工程师' })).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('变更说明'), { target: { value: '刷新使命' } })
  fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
  await waitFor(() => expect(bridge.update).toHaveBeenCalled())
  expect(vi.mocked(bridge.update).mock.calls[0][0]).toMatchObject({
    expertId,
    expectedVersionId: versionId,
    changeNote: '刷新使命',
    sixSection: expect.objectContaining({ identity: '安全岗位', mission: '守住边界' }),
  })
})

it('archives an expert after confirm', async () => {
  const bridge = expertApi()
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  await screen.findAllByText('安全工程师')
  fireEvent.click(screen.getByRole('button', { name: '归档' }))
  fireEvent.click(await screen.findByRole('button', { name: '确认归档' }))
  await waitFor(() => expect(bridge.archive).toHaveBeenCalledOnce())
  const payload = vi.mocked(bridge.archive).mock.calls[0][0]
  expect(payload.expertId).toBe(expertId)
  expect(payload.confirmToken).toMatch(/^[0-9a-f]{64}$/)
})

it('creates a scenario card from the detail pane', async () => {
  const bridge = expertApi()
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  expect(await screen.findByText('安全岗位')).toBeInTheDocument()
  const title = screen.getByLabelText('场景标题')
  const summary = screen.getByLabelText('摘要')
  fireEvent.change(title, { target: { value: '慢查询处置' } })
  fireEvent.change(summary, { target: { value: '针对慢查询的索引与执行计划处置剧本' } })
  await waitFor(() => {
    expect(title).toHaveValue('慢查询处置')
    expect(summary).toHaveValue('针对慢查询的索引与执行计划处置剧本')
  })
  fireEvent.click(screen.getByRole('button', { name: '添加场景卡' }))
  await waitFor(() => expect(bridge.scenarioCreate).toHaveBeenCalled())
  expect(vi.mocked(bridge.scenarioCreate).mock.calls[0][0]).toMatchObject({
    expertId,
    title: '慢查询处置',
    summary: '针对慢查询的索引与执行计划处置剧本',
    scenario: { steps: ['定位', '处置', '验收'] },
  })
})

it('prompts to install missing preferred factory kits for a conversation specialist', async () => {
  const install = vi.fn().mockResolvedValue({ name: 'slide-builder' })
  const skills = {
    list: vi.fn().mockResolvedValue({ items: [] }),
    install,
  } as unknown as SkillBridge
  const skillsSet = vi.fn().mockResolvedValue({ expertId, skillKeys: [] })
  const ppt = { ...expertList.experts[0], name: 'PPT专家', division: 'product' as const }
  const bridge = expertApi({
    list: vi.fn().mockResolvedValue({ experts: [ppt], total: 1 }),
    detail: vi.fn().mockResolvedValue({
      ...expertDetail,
      expert: { ...expertDetail.expert, name: 'PPT专家', division: 'product' },
    }),
    skillsSet,
  })
  render(<ExpertCenterPage bridge={bridge} projects={projects} skills={skills} />)
  expect(await screen.findByText('岗位需要但技能库还没有：')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '去技能中心安装 slide-builder' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '去技能中心安装 web-researcher' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '去技能中心安装 slide-builder' }))
  await waitFor(() => expect(install).toHaveBeenCalledWith({ templateId: 'slide-builder' }))
})

it('lets an agent specialist pick a local Codex or Claude Code brain', async () => {
  const ppt = { ...expertList.experts[0], name: 'PPT专家', division: 'product' as const }
  const bridge = expertApi({
    list: vi.fn().mockResolvedValue({ experts: [ppt], total: 1 }),
    detail: vi.fn().mockResolvedValue({
      ...expertDetail,
      expert: { ...expertDetail.expert, name: 'PPT专家', division: 'product' },
    }),
    skillsSet: vi.fn().mockResolvedValue({ expertId, skillKeys: ['brain:codex'] }),
  })
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  expect(await screen.findByRole('group', { name: '大脑' })).toBeInTheDocument()
  expect(screen.getAllByRole('radio', { name: /月汐引擎/ })[0]).toBeChecked()
  fireEvent.click(screen.getAllByRole('radio', { name: /本机 Codex/ })[0])
  fireEvent.click(screen.getByRole('button', { name: '保存运行时绑定' }))
  await waitFor(() => expect(bridge.skillsSet).toHaveBeenCalled())
  expect(vi.mocked(bridge.skillsSet!).mock.calls[0][0].skillKeys).toContain('brain:codex')
})

it('saves expert skill bindings from the detail pane', async () => {
  const skillsSet = vi.fn().mockResolvedValue({ expertId, skillKeys: ['slide-builder'] })
  const skills = {
    list: vi.fn().mockResolvedValue({
      items: [{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FA1',
        name: 'tpl-slide-builder',
        displayName: '演示文稿',
        description: 'slides',
        version: '1.0.0',
        status: 'published',
        permissions: ['read_write'],
        entryPoint: 'builtin://slide-builder',
        manifestJson: '{}',
        category: 'writing',
        categorySource: 'keyword',
        createdAt: now,
        updatedAt: now,
      }],
    }),
  } as unknown as SkillBridge
  render(<ExpertCenterPage bridge={expertApi({ skillsSet })} projects={projects} skills={skills} />)
  expect(await screen.findByText('尚未挂技能，仅提示词。')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('checkbox', { name: /演示文稿/ }))
  fireEvent.click(screen.getByRole('button', { name: '保存运行时绑定' }))
  await waitFor(() => expect(skillsSet).toHaveBeenCalled())
  expect(skillsSet.mock.calls[0][0]).toMatchObject({ expertId, skillKeys: ['slide-builder'] })
})

it('states conversation specialists are same-engine colleagues, not independent processes', async () => {
  const catalogList = vi.fn().mockResolvedValue({
    items: [{
      id: 'ppt-expert', name: 'ppt-expert', displayName: 'PPT专家',
      description: '做演示文稿', category: '产品', version: '1.0.0', installed: false, featured: true,
    }],
  })
  render(<ExpertCenterPage bridge={expertApi({ catalogList })} projects={projects} />)
  fireEvent.click(await screen.findByRole('tab', { name: '专家市场' }))
  expect(await screen.findByText(/同一引擎，不是独立进程/)).toBeInTheDocument()
  expect(screen.queryByText(/独立同事/)).not.toBeInTheDocument()
})
