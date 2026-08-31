import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { SkillBridge } from '../bridge/client'
import type { SkillDTO } from '../generated/bridge'
import { SkillPage } from './SkillPage'

afterEach(cleanup)
const now = '2025-01-01T00:00:00Z'
const skill: SkillDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', name: 'code-review', displayName: '代码审查', description: '审查代码',
  version: '1.0.0', status: 'draft', permissions: ['read_only'], entryPoint: 'skills/cr/index.js',
  manifestJson: '{}', category: 'development', categorySource: 'keyword', createdAt: now, updatedAt: now,
}
const api = (o: Partial<SkillBridge> = {}): SkillBridge => ({
  get: vi.fn(), list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn().mockResolvedValue(skill),
  update: vi.fn().mockResolvedValue(skill), delete: vi.fn().mockResolvedValue({ deleted: true }),
  match: vi.fn().mockResolvedValue({ items: [] }), publish: vi.fn().mockResolvedValue({ published: true }),
  deprecate: vi.fn().mockResolvedValue({ deprecated: true }), disable: vi.fn().mockResolvedValue({ disabled: true }), ...o,
})

const catalogEntry = {
  id: 'meeting-minutes', name: 'tpl-meeting-minutes', displayName: '会议纪要助手',
  description: '整理会话为结构化会议纪要', category: '办公协作', version: '1.0.0',
  permissions: ['read_write' as const], installed: false, featured: true, source: '月汐',
}

it('renders empty state initially', async () => {
  const bridge = api()
  render(<SkillPage bridge={bridge} />)
  fireEvent.click(screen.getByRole('tab', { name: '技能库' }))
  expect(await screen.findByText('暂无技能')).toBeInTheDocument()
  expect(bridge.list).toHaveBeenCalled()
})

it('renders skill items from bridge.list', async () => {
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [skill] }) })
  render(<SkillPage bridge={bridge} />)
  fireEvent.click(screen.getByRole('tab', { name: '技能库' }))
  expect((await screen.findAllByText('代码审查')).length).toBeGreaterThanOrEqual(1)
})

it('opens the library and highlights a newly created skill', async () => {
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [skill] }) })
  render(<SkillPage bridge={bridge} highlightId={skill.id} />)
  const row = await screen.findByRole('button', { name: /代码审查/ })
  expect(row.className).toContain('is-highlight')
})

it('starts skill creation through chat instead of an inline form', async () => {
  const onCreateInChat = vi.fn()
  render(<SkillPage bridge={api()} onCreateInChat={onCreateInChat} />)
  fireEvent.click(screen.getByRole('tab', { name: '技能库' }))
  await screen.findByText('暂无技能')
  fireEvent.click(screen.getByRole('button', { name: '添加技能' }))
  fireEvent.click(screen.getByRole('button', { name: /通过对话创建/ }))
  expect(onCreateInChat).toHaveBeenCalledOnce()
  expect(screen.queryByLabelText('技能名称')).not.toBeInTheDocument()
})

it('opens skill edit in a dialog and exposes a resizable detail pane', async () => {
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [skill] }) })
  render(<SkillPage bridge={bridge} />)
  fireEvent.click(screen.getByRole('tab', { name: '技能库' }))
  await screen.findAllByText('代码审查')
  expect(screen.getByRole('separator', { name: '调整详情栏宽度' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '编辑技能' }))
  expect(await screen.findByRole('dialog', { name: '编辑 代码审查' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '保存修改' })).toBeInTheDocument()
})

it('deletes a skill from the list', async () => {
  const del = vi.fn().mockResolvedValue({ deleted: true })
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [skill] }), delete: del })
  render(<SkillPage bridge={bridge} />)
  fireEvent.click(screen.getByRole('tab', { name: '技能库' }))
  await screen.findAllByText('代码审查')
  vi.spyOn(window,'confirm').mockReturnValue(true)
  fireEvent.click(screen.getByRole('button', { name: '删除' }))
  await waitFor(() => expect(del).toHaveBeenCalledOnce())
  expect(del.mock.calls[0][0]).toEqual({ id: skill.id, expectedVersion: 1 })
})

it('states the market is a bundled catalog not an online store', async () => {
  render(<SkillPage bridge={api({ catalogList: vi.fn().mockResolvedValue({ items: [catalogEntry] }) })} />)
  expect(await screen.findByText(/本机捆绑目录/)).toBeInTheDocument()
  expect(screen.getByText(/不是在线商店/)).toBeInTheDocument()
  expect(screen.getAllByText(/捆绑目录/).length).toBeGreaterThanOrEqual(2)
})

it('shows the market catalog and installs a template', async () => {
  const catalogList = vi.fn().mockResolvedValue({ items: [catalogEntry] })
  const install = vi.fn().mockResolvedValue({ skillId: '01ARZ3NDEKTSV4RRFFQ69G5FBB', name: 'tpl-meeting-minutes', status: 'published' })
  const list = vi.fn().mockResolvedValue({ items: [] })
  const bridge = api({ list, catalogList, install })
  render(<SkillPage bridge={bridge} />)
  expect(await screen.findByText('会议纪要助手')).toBeInTheDocument()
  expect(screen.getByRole('region', { name: '推荐技能' })).toBeInTheDocument()
  expect(catalogList).toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '安装 会议纪要助手' }))
  await waitFor(() => expect(install).toHaveBeenCalledWith({ templateId: 'meeting-minutes' }))
  await waitFor(() => expect(list).toHaveBeenCalled())
})

it('filters the market by category', async () => {
  const catalogList = vi.fn().mockResolvedValue({ items: [catalogEntry, { ...catalogEntry, id: 'go-reviewer', name: 'tpl-go-reviewer', displayName: 'Go 代码审查', category: '研发效能', featured: false }] })
  render(<SkillPage bridge={api({ catalogList, install: vi.fn() })} />)
  expect(await screen.findByText('会议纪要助手')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /研发效能/ }))
  expect(screen.getByText('Go 代码审查')).toBeInTheDocument()
  expect(screen.queryByText('会议纪要助手')).not.toBeInTheDocument()
})

it('marks installed templates in the market', async () => {
  const catalogList = vi.fn().mockResolvedValue({ items: [{ ...catalogEntry, installed: true }] })
  const install = vi.fn()
  render(<SkillPage bridge={api({ catalogList, install })} />)
  expect(await screen.findByText('已安装')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /安装/ })).not.toBeInTheDocument()
})
