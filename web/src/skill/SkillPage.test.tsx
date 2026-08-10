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
  manifestJson: '{}', createdAt: now, updatedAt: now,
}
const api = (o: Partial<SkillBridge> = {}): SkillBridge => ({
  get: vi.fn(), list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn().mockResolvedValue(skill),
  update: vi.fn().mockResolvedValue(skill), delete: vi.fn().mockResolvedValue({ deleted: true }),
  match: vi.fn().mockResolvedValue({ items: [] }), publish: vi.fn().mockResolvedValue({ published: true }),
  deprecate: vi.fn().mockResolvedValue({ deprecated: true }), disable: vi.fn().mockResolvedValue({ disabled: true }), ...o,
})

it('renders empty state initially', async () => {
  const bridge = api()
  render(<SkillPage bridge={bridge} />)
  expect(await screen.findByText('暂无技能')).toBeInTheDocument()
  expect(bridge.list).toHaveBeenCalled()
})

it('renders skill items from bridge.list', async () => {
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [skill] }) })
  render(<SkillPage bridge={bridge} />)
  expect(await screen.findByText('代码审查')).toBeInTheDocument()
})

it('creates a skill via the create form', async () => {
  const create = vi.fn().mockResolvedValue(skill), bridge = api({ create })
  render(<SkillPage bridge={bridge} />)
  await screen.findByText('暂无技能')
  fireEvent.click(screen.getByText('新建技能'))
  fireEvent.change(screen.getByLabelText('技能名称'), { target: { value: 'new-skill' } })
  fireEvent.change(screen.getByLabelText('显示名称'), { target: { value: '新技能' } })
  fireEvent.change(screen.getByLabelText('入口'), { target: { value: 'skills/new/index.js' } })
  fireEvent.click(screen.getByRole('button', { name: '创建技能' }))
  await waitFor(() => expect(create).toHaveBeenCalledOnce())
  expect(create.mock.calls[0][0]).toMatchObject({ name: 'new-skill', displayName: '新技能', entryPoint: 'skills/new/index.js' })
})

it('deletes a skill from the list', async () => {
  const del = vi.fn().mockResolvedValue({ deleted: true })
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [skill] }), delete: del })
  render(<SkillPage bridge={bridge} />)
  await screen.findByText('代码审查')
  fireEvent.click(screen.getByRole('button', { name: '删除' }))
  await waitFor(() => expect(del).toHaveBeenCalledOnce())
  expect(del.mock.calls[0][0]).toEqual({ id: skill.id, expectedVersion: 1 })
})
