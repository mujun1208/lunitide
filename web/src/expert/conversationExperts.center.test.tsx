import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ExpertBridge, ProjectBridge } from '../bridge/client'
import { CONVERSATION_EXPERTS, conversationExpertDivision } from './conversationExperts'
import { ExpertCenterPage } from './ExpertCenterPage'

afterEach(cleanup)

const now = '2026-01-01T00:00:00Z'
const expertId = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const versionId = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
const digest = 'a'.repeat(64)
const expertDetail = {
  expert: { expertId, name: 'PPT专家', division: 'product', source: 'local', state: 'enabled', semver: '1.0.0', currentVersionId: versionId },
  sixSection: { identity: '对话专家', mission: '交付', rules: '规则', workflow: '流程', deliverableTemplate: '产物', successMetrics: '指标' },
  versions: [{ versionId, semver: '1.0.0', changeNote: '初版', sixSectionDigest: digest, createdAt: now }],
  mountings: [],
}

const expertApi = (o: Partial<ExpertBridge> = {}): ExpertBridge => ({
  list: vi.fn().mockResolvedValue({experts: [], total: 0}),
  detail: vi.fn().mockResolvedValue(expertDetail),
  create: vi.fn(), update: vi.fn(),
  toggle: vi.fn(), archive: vi.fn(),
  mount: vi.fn(),
  mountingGet: vi.fn().mockResolvedValue({matrix: []}),
  scenarioCreate: vi.fn(), scenarioList: vi.fn().mockResolvedValue({items: []}), scenarioDelete: vi.fn(),
  catalogList: vi.fn().mockResolvedValue({items: []}),
  ...o,
})

const projects: ProjectBridge = {
  list: vi.fn().mockResolvedValue({items: []}),
  create: vi.fn(), update: vi.fn(), publish: vi.fn(), close: vi.fn(), reopen: vi.fn(), advanceStatus: vi.fn(), delete: vi.fn(),
}

it('shows 系统架构师专家 with the other conversation specialists in the library and 专家市场', async () => {
  const catalog = CONVERSATION_EXPERTS.map(item => ({
    id: item.id, name: item.name, displayName: item.name,
    description: `${item.name} 对话专家`, category: '创作交付',
    division: conversationExpertDivision(item.id),
    origin: 'lunitide', usage: 'both' as const, version: '1.0.0', installed: true, emoji: '专',
  }))
  const experts = CONVERSATION_EXPERTS.map((item, index) => ({
    expertId: `01ARZ3NDEKTSV4RRFFQ69G5F${String(index).padStart(2, '0')}`,
    name: item.name, division: catalog[index].division,
    source: 'local' as const, semver: '1.0.0', state: 'enabled' as const,
    versionCount: 1, mountedPhaseCount: 0,
  }))
  const bridge = expertApi({
    list: vi.fn().mockResolvedValue({experts, total: experts.length}),
    catalogList: vi.fn().mockResolvedValue({items: catalog}),
    detail: vi.fn().mockResolvedValue({
      ...expertDetail,
      expert: {...expertDetail.expert, expertId: experts[0].expertId, name: experts[0].name},
    }),
  })
  render(<ExpertCenterPage bridge={bridge} projects={projects} />)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'architect-expert' && item.name === '系统架构师专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'db-expert' && item.name === '数据库设计专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'repo-expert' && item.name === '系统项目结构规范专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'test-expert' && item.name === '系统测试专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'hardware-expert' && item.name === '硬件配置专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'dev-expert' && item.name === '开发专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'mro-expert' && item.name === '航空机务专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'standards-expert' && item.name === '开发规范专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'ui-designer' && item.name === 'UI专家')).toBe(true)
  for (const item of CONVERSATION_EXPERTS) {
    expect((await screen.findAllByText(item.name)).length).toBeGreaterThanOrEqual(1)
  }
  fireEvent.click(screen.getByRole('tab', {name: '专家市场'}))
  expect(await screen.findByLabelText('专家市场')).toBeInTheDocument()
  for (const item of CONVERSATION_EXPERTS) {
    expect(screen.getAllByText(item.name).length).toBeGreaterThanOrEqual(1)
  }
})
