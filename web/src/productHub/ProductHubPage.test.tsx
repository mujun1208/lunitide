import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ProductHubPage } from './ProductHubPage'
import { PRODUCT_HUB_TOKEN_KEY } from './productHubTypes'

const unlock = vi.fn()
const authStatus = vi.fn()
const overview = vi.fn()
const graph = vi.fn()
const diagnostics = vi.fn()
const refresh = vi.fn()
const changelog = vi.fn()
const apply = vi.fn()
const tagSet = vi.fn()

vi.mock('../bridge/client', () => ({
  getProductHubBridge: () => ({
    unlock,
    authStatus,
    overview,
    graph,
    diagnostics,
    refresh,
    changelog,
    apply,
    tagSet,
    featureCard: vi.fn(),
    changePassword: vi.fn(),
    exportDoc: vi.fn(),
  }),
}))

afterEach(() => {
  cleanup()
  sessionStorage.clear()
  localStorage.clear()
  vi.clearAllMocks()
})

it('keeps the hub behind an unlock form and does not list it as a public page', async () => {
  render(<ProductHubPage />)
  expect(screen.getByRole('heading', { name: '产品总览' })).toBeInTheDocument()
  expect(screen.getByLabelText('用户名')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '刷新' })).toBeNull()
  unlock.mockResolvedValue({ sessionToken: 'tok', username: 'mujun', unlocked: true })
  overview.mockResolvedValue({ product: 'Lunitide', editionId: 'e1', generatedAt: '', cardCount: 2, healthScore: 90, added: 1, updated: 0, removed: 0, probePassed: 2, probeTotal: 2, domains: [], tags: [] })
  graph.mockResolvedValue({ nodes: [{ id: 'f1', stable_key: 'feature.dialog.music.play', type: 'Feature', name: '放歌', domain: 'dialog' }], edges: [] })
  diagnostics.mockResolvedValue({ findings: [{ severity: 'info', error_code: 'PH_000', stable_key: 'product.lunitide', title: '本轮未发现阻断问题', evidence: '', root_cause: '', fix: '', verify: '', status: 'wont_fix' }], reportMarkdown: '# 册', reportHtml: '<h1>册</h1>' })
  changelog.mockResolvedValue({ changes: [] })
  apply.mockResolvedValue({ ok: true, applied: true, count: 1, status: 'applied', plan: '无需改代码' })
  fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'mujun' } })
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: '1234567890' } })
  fireEvent.click(screen.getByRole('button', { name: '进入总览' }))
  expect(await screen.findByRole('button', { name: '刷新' })).toBeInTheDocument()
  expect(sessionStorage.getItem(PRODUCT_HUB_TOKEN_KEY)).toBe('tok')
  expect(screen.getAllByRole('button', { name: /放歌/ }).length).toBeGreaterThan(0)
  expect(screen.getByText('按前台页面')).toBeInTheDocument()
  expect(screen.getByText('17 / 17 PAGES')).toBeInTheDocument()
  expect(screen.getByLabelText('对话')).toBeInTheDocument()
  expect(screen.getByLabelText('办公')).toBeInTheDocument()
  expect(screen.getAllByRole('button', { name: /主页/ }).length).toBeGreaterThan(0)
  fireEvent.click(screen.getByRole('button', { name: /媒体中心/ }))
  expect(screen.getByText(/本页功能/)).toBeInTheDocument()
  expect(screen.getByText(/播放、队列、打开音视频/)).toBeInTheDocument()
  expect(screen.getByText(/活源覆盖 2\/2/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /诊断报告/ }))
  expect(await screen.findByRole('button', { name: '执行全部净化' })).toBeInTheDocument()
  expect(screen.getByText(/新增 1/)).toBeInTheDocument()
  expect(screen.queryByText(/新增 2/)).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '执行全部净化' }))
  expect(apply).toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: /变更/ }))
  expect(screen.getByText(/这一轮没有该类变更/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '关闭' }))
  fireEvent.click(screen.getByLabelText('全部域'))
  expect(screen.getByRole('option', { name: /对话体验/ })).toBeInTheDocument()
  fireEvent.click(screen.getByLabelText('全部域'))
  fireEvent.change(screen.getByLabelText('搜索产品知识'), { target: { value: '放歌' } })
  expect(screen.getByRole('listbox', { name: '搜索结果' })).toBeInTheDocument()
  expect(screen.getByRole('option', { name: /放歌/ })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '知识图谱' }))
  expect(screen.getByRole('img', { name: '知识图谱' })).toBeInTheDocument()
  expect(screen.getByLabelText('全部类型')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '图景' }))
  expect(screen.getByText(/图景不计入健康分/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Cursor' }))
  fireEvent.click(screen.getByRole('button', { name: '执行对照' }))
  expect(screen.getByText(/Lunitide \/ 月汐/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '本机优先' })).toBeInTheDocument()
})
