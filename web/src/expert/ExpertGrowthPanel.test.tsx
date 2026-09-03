import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { ExpertGrowthPanel } from './ExpertGrowthPanel'

afterEach(cleanup)

const expertId = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

it('shows this expert path title and hides fake ladder when coverage is empty', async () => {
  const growthGet = vi.fn().mockResolvedValue({
    missionSnapshot: '持证人员的辅助检索顾问',
    ladder: [{ name: '知识积累', state: 'next' }],
    coverage: { docTypes: [], gaps: [] },
    scenarios: [],
  })
  render(<LanguageProvider value="zh-CN"><ExpertGrowthPanel expertId={expertId} growthGet={growthGet} /></LanguageProvider>)
  expect(await screen.findByText('这位专家的成长')).toBeInTheDocument()
  expect(screen.getByText(/持证人员的辅助检索顾问/)).toBeInTheDocument()
  expect(screen.queryByText('正在补')).toBeNull()
})

it('lists covered types when coverage is present', async () => {
  const growthGet = vi.fn().mockResolvedValue({
    missionSnapshot: 'mission',
    ladder: [{ name: '手册检索', state: 'have' }],
    coverage: { docTypes: ['AMM', 'MEL'], gaps: ['IPC'] },
    scenarios: [{ title: '手册问答', phaseKey: 'OPERATIONS_RETROSPECTIVE' }],
  })
  render(<LanguageProvider value="zh-CN"><ExpertGrowthPanel expertId={expertId} growthGet={growthGet} /></LanguageProvider>)
  expect(await screen.findByText('AMM')).toBeInTheDocument()
  expect(screen.getByText('IPC')).toBeInTheDocument()
  expect(screen.getByText('手册问答')).toBeInTheDocument()
})
