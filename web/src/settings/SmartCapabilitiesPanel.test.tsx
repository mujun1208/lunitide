import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { IdentityBridge, MemoryOpsBridge } from '../bridge/client'
import { SmartCapabilitiesPanel } from './SmartCapabilitiesPanel'

afterEach(cleanup)

const now = new Date().toISOString()
const versionA = 'a'.repeat(64)
const versionB = 'b'.repeat(64)

function panel(overrides: {
  updateSettings?: MemoryOpsBridge['updateSettings']
  getSettings?: MemoryOpsBridge['getSettings']
  onOpenOCR?: () => void
  onOpenMemory?: () => void
} = {}) {
  const updateSettings = overrides.updateSettings ?? vi.fn().mockResolvedValue({
    subjectId: 'sub-1', memoryEnabled: false, autoNominate: false, captureMode: 'off', growthDays: 14, updatedAt: now, version: versionB, revision: 2, personalMemoryEnabled: true, projectMemoryEnabled: true,
  })
  const getSettings = overrides.getSettings ?? vi.fn().mockResolvedValue({
    subjectId: 'sub-1', memoryEnabled: true, autoNominate: false, captureMode: 'auto', growthDays: 14, updatedAt: now, version: versionA, revision: 1, personalMemoryEnabled: true, projectMemoryEnabled: true,
  })
  const memoryOps = { getSettings, updateSettings } as unknown as MemoryOpsBridge
  const identity = { get: vi.fn().mockResolvedValue({ subjectId: 'sub-1' }) } as unknown as IdentityBridge
  render(<SmartCapabilitiesPanel memoryOps={memoryOps} identity={identity} onOpenOCR={overrides.onOpenOCR} onOpenMemory={overrides.onOpenMemory} />)
  return { updateSettings, getSettings }
}

it('TestSmartCapabilitiesOverviewContract: shows exactly two cards and keeps Paddle disabled', async () => {
  const onOpenOCR = vi.fn()
  const user = userEvent.setup()
  panel({ onOpenOCR })
  expect(await screen.findByRole('heading', { name: '自动记忆' })).toBeInTheDocument()
  expect(screen.getAllByRole('heading', { name: '文字识别' })).toHaveLength(1)
  expect(screen.getByText(/已配置的视觉模型能用就先用，然后走本机 RapidOCR，最后用 Windows OCR 兜底/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '安装（不可用）' })).toBeNull()
  expect(document.querySelectorAll('.smart-cap-card')).toHaveLength(2)
  const cards = document.querySelector('.smart-cap-cards')
  expect(cards).not.toBeNull()
  expect(cards?.querySelectorAll('.smart-cap-card')).toHaveLength(2)
  expect(cards?.previousElementSibling?.classList.contains('smart-cap-intro')).toBe(true)
  await user.click(screen.getByRole('button', { name: '打开文字识别' }))
  expect(onOpenOCR).toHaveBeenCalledTimes(1)
})

it('saves captureMode=off from the memory card', async () => {
  const user = userEvent.setup()
  const { updateSettings } = panel()
  await screen.findByRole('radio', { name: /自动/ })
  await user.click(screen.getByRole('radio', { name: /关闭/ }))
  await waitFor(() => expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
    captureMode: 'off',
    memoryEnabled: false,
    expectedVersion: versionA,
  })))
  expect(await screen.findByRole('status')).toHaveTextContent('记忆设置已保存')
})

it('saves an explicit memory through memory.item.create', async () => {
  const user = userEvent.setup()
  const itemCreate = vi.fn().mockResolvedValue({
    item: { factId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', version: 1, revision: 1, scopeKind: 'user', kind: 'preference', forgotten: false, updatedAt: now },
    databaseRevision: 1,
    undoOperationId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    undoExpiresAt: now,
  })
  const memory = { itemCreate } as unknown as import('../bridge/client').MemoryBridge
  const updateSettings = vi.fn().mockResolvedValue({
    subjectId: 'sub-1', memoryEnabled: true, autoNominate: false, captureMode: 'auto', growthDays: 14, updatedAt: now, version: versionB, revision: 2, personalMemoryEnabled: true, projectMemoryEnabled: true,
  })
  const getSettings = vi.fn().mockResolvedValue({
    subjectId: 'sub-1', memoryEnabled: true, autoNominate: false, captureMode: 'auto', growthDays: 14, updatedAt: now, version: versionA, revision: 1, personalMemoryEnabled: true, projectMemoryEnabled: true,
  })
  const memoryOps = { getSettings, updateSettings } as unknown as MemoryOpsBridge
  const identity = { get: vi.fn().mockResolvedValue({ subjectId: 'sub-1' }) } as unknown as IdentityBridge
  render(<SmartCapabilitiesPanel memoryOps={memoryOps} identity={identity} memory={memory} />)
  await screen.findByRole('textbox', { name: '保存为记忆' })
  await waitFor(() => expect(identity.get).toHaveBeenCalled())
  await user.type(screen.getByRole('textbox', { name: '保存为记忆' }), '我喜欢简洁的回答')
  await user.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(itemCreate).toHaveBeenCalled())
  expect(itemCreate.mock.calls[0][0]).toEqual(expect.objectContaining({
    scopeKind: 'user',
    text: '我喜欢简洁的回答',
  }))
  expect(itemCreate.mock.calls[0][1]).toEqual(expect.objectContaining({
    attempt: expect.objectContaining({ method: 'memory.item.create' }),
  }))
  expect(await screen.findByRole('status')).toHaveTextContent('已保存为记忆')
})
